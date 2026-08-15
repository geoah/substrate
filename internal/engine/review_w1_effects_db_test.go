package engine

// The wave-1 adversarial review's write-path regressions, driven from inside
// the package so the barriers land exactly where the reviewer's interleavings
// need them: effect addressing racing a merge (#2), the ifAbsent
// check-then-act (#3), synchronous compile-at-registration (#8) and
// merge/split replay idempotence (#11).

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	raceAuthority = "race.test.dev"
	raceWidget    = raceAuthority + "/widget"
	raceActor     = substrate.Actor("function.racer." + raceAuthority)
)

// newRaceDataset provisions a repository with one plain widget type and hands
// back the INTERNAL dataset, so tests can drive txn-level paths directly.
func newRaceDataset(t *testing.T) *dataset {
	t.Helper()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	svc, err := Open(ctx, dsn,
		WithKindsDir("../../kinds/core.substrate.reamde.dev"))
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if _, err := svc.CreateRepository(ctx, "geoah"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	d, err := svc.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	ds, ok := d.(*dataset)
	if !ok {
		t.Fatalf("dataset is a %T", d)
	}
	importVocabulary(t, ds)
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "race", Authority: raceAuthority,
		Manifests: []map[string]any{
			vocabulary.AuthorityManifest(raceAuthority, 0),
			vocabulary.ActorManifest(raceAuthority, vocabulary.AuthorityActor(raceAuthority)),
			vocabulary.KindManifest(raceAuthority, map[string]any{"singular": "widget", "plural": "widgets"},
				map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		},
	}); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	return ds
}

func racePut(t *testing.T, ds *dataset, props map[string]any) *substrate.Record {
	t.Helper()
	e, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{
		Kind: raceWidget, Properties: props,
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	return e
}

func TestEffectAddressingSerializesWithMerge(t *testing.T) {
	t.Parallel()
	// Review W1 #2, the reviewer's exact interleaving: an effect addressed
	// at the loser must not resolve BEFORE a concurrent merge commits, wait
	// out the merge on the row lock, and then resurrect the tombstoned
	// loser. The fix takes the per-record advisory lock before resolving;
	// this test parks a merge mid-flight on a held row lock (the merge holds
	// its advisory locks by then), proves the effect queues BEHIND the
	// advisory lock instead of resolving stale, and asserts the invariant
	// the old code violated.
	ds := newRaceDataset(t)
	ctx := context.Background()
	w := racePut(t, ds, map[string]any{"name": "winner"})
	l := racePut(t, ds, map[string]any{"name": "loser"})

	// The barrier: a raw transaction holds the loser's ROW lock, so the
	// merge (advisory locks acquired first) parks inside loadRow FOR UPDATE.
	barrier, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("barrier: %v", err)
	}
	if _, err := barrier.ExecContext(ctx,
		`SELECT id FROM records WHERE id = $1 FOR UPDATE`, l.ID); err != nil {
		t.Fatalf("barrier lock: %v", err)
	}

	mergeDone := make(chan error, 1)
	go func() {
		_, err := ds.Merge(ctx, substrate.ActorAPI, w.Kind, w.ID, l.ID)
		mergeDone <- err
	}()
	time.Sleep(300 * time.Millisecond) // the merge now holds the advisory locks

	effectDone := make(chan error, 1)
	go func() {
		effectDone <- ds.inTx(ctx, raceActor, false, func(tx *txn) error {
			return tx.applyEffect(effect{
				Action: effectPut, Type: raceWidget, ID: l.ID,
				Properties: map[string]any{"name": "from-effect"},
			})
		})
	}()
	time.Sleep(300 * time.Millisecond) // the effect must queue behind the advisory lock

	select {
	case err := <-effectDone:
		t.Fatalf("the effect did not serialize behind the merge: %v", err)
	default:
	}
	_ = barrier.Rollback()

	if err := <-mergeDone; err != nil {
		t.Fatalf("merge: %v", err)
	}
	if err := <-effectDone; err != nil {
		t.Fatalf("effect: %v", err)
	}

	// The invariant: the loser is tombstoned AND its former-id trail names
	// the winner — never a live loser behind a trail (the resurrection).
	var deletedAt sql.NullTime
	if err := ds.db.QueryRowContext(ctx,
		`SELECT deleted_at FROM records WHERE id = $1`, l.ID).Scan(&deletedAt); err != nil {
		t.Fatalf("loser row: %v", err)
	}
	if !deletedAt.Valid {
		t.Fatal("the merge's loser is live again — the effect resurrected it")
	}
	var target string
	if err := ds.db.QueryRowContext(ctx,
		`SELECT record_id FROM former_ids WHERE former_id = $1`, l.ID).Scan(&target); err != nil {
		t.Fatalf("former id: %v", err)
	}
	if target != w.ID {
		t.Fatalf("former trail points at %s, want %s", target, w.ID)
	}
	// The effect landed on the canonical winner.
	winner, err := ds.Get(ctx, w.Kind, w.ID)
	if err != nil {
		t.Fatalf("get winner: %v", err)
	}
	if winner.Properties["name"] != "from-effect" {
		t.Fatalf("the effect's write is lost: %v", winner.Properties)
	}
}

func TestEffectIfAbsentMintsSerialize(t *testing.T) {
	t.Parallel()
	// Review W1 #3: two concurrent ifAbsent mints of one absent id. SELECT
	// FOR UPDATE cannot lock an absent row, so before the fix both saw nil
	// and the second's ON CONFLICT UPDATE overwrote the first. Under the
	// advisory lock the mints serialize: exactly one create, zero
	// overwrites.
	ds := newRaceDataset(t)
	ctx := context.Background()
	const id = "mint-target"

	apply := func(val string) error {
		return ds.inTx(ctx, raceActor, false, func(tx *txn) error {
			return tx.applyEffect(effect{
				Action: effectPut, Type: raceWidget, ID: id, IfAbsent: true,
				Properties: map[string]any{"name": val},
			})
		})
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, v := range []string{"alpha", "beta"} {
		wg.Add(1)
		go func(v string) {
			defer wg.Done()
			errs <- apply(v)
		}(v)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
	}
	e, err := ds.Get(ctx, raceWidget, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if name := e.Properties["name"]; name != "alpha" && name != "beta" {
		t.Fatalf("minted name: %v", name)
	}
	// One changelog row: the create. A second row would be the loser's
	// destructive upsert.
	var rows int
	if err := ds.db.QueryRowContext(ctx,
		`SELECT count(*) FROM changelog WHERE record_id = $1`, id).Scan(&rows); err != nil {
		t.Fatalf("changelog: %v", err)
	}
	if rows != 1 {
		t.Fatalf("the losing mint wrote: %d changelog rows", rows)
	}
}

func TestEffectDecodeRejectsNonBooleanIfAbsent(t *testing.T) {
	t.Parallel()
	// Review W1 #3's decode half: a typo'd ifAbsent must fail loudly, never
	// silently become a destructive upsert.
	ds := newRaceDataset(t)
	fn := &vocabulary.Function{
		Name: "m", Authority: raceAuthority,
		Caps: vocabulary.FunctionCaps{Emit: []string{raceWidget}},
	}
	_, err := ds.decodeEffects(fn, []any{map[string]any{
		"action": "put", "kind": raceWidget, "id": "x", "ifAbsent": "yes",
	}})
	if err == nil || !strings.Contains(err.Error(), "boolean") {
		t.Fatalf("a non-boolean ifAbsent decoded: %v", err)
	}
}

func TestMergeSplitReplayIdempotent(t *testing.T) {
	t.Parallel()
	// Review W1 #11: replaying a merge whose loser is already former-to-the-
	// winner is a VERIFIED no-op returning the record; replaying a split
	// whose merge record is already tombstoned is a verified no-op returning
	// the split record — never a second mutation, never a park.
	ds := newRaceDataset(t)
	ctx := context.Background()
	a := racePut(t, ds, map[string]any{"name": "a"})
	b := racePut(t, ds, map[string]any{"name": "b"})

	rec, err := ds.Merge(ctx, substrate.ActorAPI, a.Kind, a.ID, b.ID)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	countLive := func(typ string) int {
		var n int
		if err := ds.db.QueryRowContext(ctx,
			`SELECT count(*) FROM records WHERE kind = $1 AND deleted_at IS NULL`, typ).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", typ, err)
		}
		return n
	}

	// Replay the merge effect.
	if err := ds.inTx(ctx, raceActor, false, func(tx *txn) error {
		return tx.applyEffect(effect{Action: effectMerge, Type: raceWidget, ID: a.ID, Loser: b.ID})
	}); err != nil {
		t.Fatalf("merge replay parked: %v", err)
	}
	if n := countLive(kindRecordMerge); n != 1 {
		t.Fatalf("merge replay minted a record: %d live merge records", n)
	}
	// And through the surface verb too.
	again, err := ds.Merge(ctx, substrate.ActorAPI, a.Kind, a.ID, b.ID)
	if err != nil {
		t.Fatalf("surface merge replay: %v", err)
	}
	if again.ID != rec.ID {
		t.Fatalf("surface replay returned %s, want the record %s", again.ID, rec.ID)
	}

	// Split, then replay the split effect.
	if _, err := ds.Split(ctx, substrate.ActorAPI, rec.ID); err != nil {
		t.Fatalf("split: %v", err)
	}
	if err := ds.inTx(ctx, raceActor, false, func(tx *txn) error {
		return tx.applyEffect(effect{Action: effectSplit, Type: raceWidget, MergeID: rec.ID})
	}); err != nil {
		t.Fatalf("split replay parked: %v", err)
	}
	if n := countLive(kindRecordSplit); n != 1 {
		t.Fatalf("split replay repeated the mutation: %d live split records", n)
	}
	restored, err := ds.Get(ctx, b.Kind, b.ID)
	if err != nil {
		t.Fatalf("get loser: %v", err)
	}
	if restored.DeletedAt != nil || restored.Title != "b" && restored.Properties["name"] != "b" {
		t.Fatalf("the replayed split disturbed the restored loser: %+v", restored)
	}
}

func TestSchemaApplyRejectsUnpreparableBody(t *testing.T) {
	t.Parallel()
	// Review W1 #8: schema apply prepares every added or changed body BEFORE
	// activation — invalid python fails admission with the register error,
	// and nothing activates.
	ds := newRaceDataset(t)
	ctx := context.Background()
	const badAuthority = "broken.test.dev"
	manifest := func(source string) enginetest.Manifest {
		return enginetest.Manifest{
			Name: "broken", Authority: badAuthority,
			Manifests: []map[string]any{
				vocabulary.AuthorityManifest(badAuthority, 0),
				vocabulary.ActorManifest(badAuthority, vocabulary.AuthorityActor(badAuthority)),
				vocabulary.FunctionManifest(badAuthority, "mangle", map[string]any{
					"description": "a body that must compile at registration",
					"runtime":     vocabulary.RuntimePython,
					"source":      source,
					"permissions": map[string]any{"writes": []any{raceWidget}},
				}),
			},
		}
	}
	err := enginetest.Install(ctx, ds, substrate.ActorAPI,
		manifest("def main(input, host)\n    return {}\n")) // missing colon
	if err == nil || !strings.Contains(err.Error(), "failed to prepare") {
		t.Fatalf("invalid source admitted: %v", err)
	}
	if _, ok := ds.registry().AuthorityByName(badAuthority); ok {
		t.Fatal("the failed batch activated")
	}
	// The corrected body admits.
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI,
		manifest("def main(input, host):\n    return {}\n")); err != nil {
		t.Fatalf("valid source refused: %v", err)
	}
	if _, ok := ds.registry().AuthorityByName(badAuthority); !ok {
		t.Fatal("the corrected batch did not activate")
	}
}
