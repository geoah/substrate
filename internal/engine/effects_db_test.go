package engine

// The effect verbs: what decode refuses, what the ifAbsent mint
// serializes and what a replay repeats.
//
// ifAbsent+ifVersion is refused at decode rather than silently letting
// ifAbsent win, and a version keeps full int64 fidelity through the runner's
// UseNumber decode.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// TestPutIfAbsentIfVersionRejectedAtDecode: the two preconditions cannot
// combine — ifAbsent short-circuits ahead of the version check, so the pair
// silently drops the guard. Decode refuses it; a lone ifVersion or a lone
// ifAbsent still decodes.
func TestPutIfAbsentIfVersionRejectedAtDecode(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	ds := newTierDataset(t)
	fn := &vocabulary.Function{
		Name: "fixer", Package: tierPackage, Description: "test",
		Caps: vocabulary.FunctionCaps{Emit: []string{typeTierProfile}},
	}

	_, err := ds.decodeEffect(fn, map[string]any{
		"action": "put", "kind": typeTierProfile, "id": "x",
		"ifAbsent": true, "ifVersion": float64(3),
		"properties": map[string]any{"name": "v"},
	})
	if err == nil || !strings.Contains(err.Error(), "ifAbsent and ifVersion cannot combine") {
		t.Fatalf("ifAbsent+ifVersion: err = %v, want the combine refusal", err)
	}

	// A lone ifVersion decodes.
	if ef, err := ds.decodeEffect(fn, map[string]any{
		"action": "put", "kind": typeTierProfile, "id": "x", "ifVersion": float64(3),
	}); err != nil || ef.IfVersion == nil || *ef.IfVersion != 3 {
		t.Fatalf("lone ifVersion decode = %+v, %v", ef, err)
	}
	// A lone ifAbsent decodes.
	if ef, err := ds.decodeEffect(fn, map[string]any{
		"action": "put", "kind": typeTierProfile, "id": "x", "ifAbsent": true,
	}); err != nil || !ef.IfAbsent || ef.IfVersion != nil {
		t.Fatalf("lone ifAbsent decode = %+v, %v", ef, err)
	}
}

// TestIfVersionInt64Fidelity: a version past 2^53 survives asInt64 as itself
// when carried as json.Number (the runner's UseNumber decode), where the old
// float64 path would round it to a neighboring integer.
func TestIfVersionInt64Fidelity(t *testing.T) {
	t.Parallel()
	const big = int64(9007199254740993) // 2^53 + 1, unrepresentable in float64

	if got, ok := asInt64(json.Number("9007199254740993")); !ok || got != big {
		t.Fatalf("asInt64(json.Number) = %d, %v, want %d", got, ok, big)
	}
	// The float path cannot represent it — the very reason UseNumber is needed.
	if got, _ := asInt64(float64(big)); got == big {
		t.Fatalf("float64 unexpectedly preserved %d — the fidelity risk is gone?", big)
	}
	// A decoded effect carrying a json.Number ifVersion keeps the exact value.
	var ef effect
	if err := decodeIfVersion(map[string]any{"ifVersion": json.Number("9007199254740993")}, &ef); err != nil {
		t.Fatalf("decodeIfVersion(json.Number): %v", err)
	}
	if ef.IfVersion == nil || *ef.IfVersion != big {
		t.Fatalf("decoded ifVersion = %v, want %d", ef.IfVersion, big)
	}
}

// syncStart runs in its own transaction after the envelope was read, so the
// record may be deleted or collected in between. Either way the stamp is
// skipped, not a delivery error, and the tombstone is left as it was (#633).
func TestSyncStartSkipsADeletedRecord(t *testing.T) {
	t.Parallel()
	ds := newRaceDataset(t)
	ctx := context.Background()
	w := racePut(t, ds, map[string]any{"name": "a"})
	if _, err := ds.Delete(ctx, substrate.ActorAPI, raceWidget, w.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	gone, err := ds.Get(ctx, raceWidget, w.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	stamp := &syncStamp{ref: eref{Kind: raceWidget, ID: w.ID}, actor: raceActor, started: nowUTC()}
	if err := ds.syncStart(ctx, stamp, 0); err != nil {
		t.Fatalf("sync start on a tombstone: %v", err)
	}
	if got, err := ds.Get(ctx, raceWidget, w.ID); err != nil || got.Version != gone.Version || got.DeletedAt == nil {
		t.Fatalf("sync start wrote the tombstone: %+v %v", got, err)
	}
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc: %v", err)
	}
	if err := ds.syncStart(ctx, stamp, 0); err != nil {
		t.Fatalf("sync start on a collected record: %v", err)
	}
}

func TestEffectIfAbsentMintsSerialize(t *testing.T) {
	t.Parallel()
	// Two concurrent ifAbsent mints of one absent id. SELECT
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

// TestGuardedEffectYieldsForExactlyOneRacer: two invocations staging the same
// guarded write over one record, concurrently, from the version they both
// read. One commits; the other gets errConflictYield and rolls back whole. The
// record moves exactly once — a lost race is never a lost write.
func TestGuardedEffectYieldsForExactlyOneRacer(t *testing.T) {
	ds := newRaceDataset(t)
	ctx := context.Background()
	rec := racePut(t, ds, map[string]any{"name": "cursor-0"})
	read := rec.Version

	apply := func(val string) error {
		return ds.inTx(ctx, raceActor, false, func(tx *txn) error {
			return tx.applyEffects([]string{raceWidget}, []effect{{
				Action: effectPatch, Type: raceWidget, ID: rec.ID,
				IfVersion: &read, OnConflict: conflictYield,
				Properties: map[string]any{"name": val},
			}})
		})
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, v := range []string{"cursor-a", "cursor-b"} {
		wg.Add(1)
		go func(v string) {
			defer wg.Done()
			errs <- apply(v)
		}(v)
	}
	wg.Wait()
	close(errs)
	won, yielded := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			won++
		case errors.Is(err, errConflictYield):
			yielded++
		default:
			t.Fatalf("racer: %v", err)
		}
	}
	if won != 1 || yielded != 1 {
		t.Fatalf("%d advanced and %d yielded, want one of each", won, yielded)
	}

	e, err := ds.Get(ctx, raceWidget, rec.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if e.Version != read+1 {
		t.Fatalf("the record sits at version %d, want %d — both racers wrote", e.Version, read+1)
	}
	if name := e.Properties["name"]; name != "cursor-a" && name != "cursor-b" {
		t.Fatalf("advanced to: %v", name)
	}
	// A yield still matches ErrConflict, so the direct-call door — which has a
	// caller to tell — answers the same 409 it always did.
	if !errors.Is(errConflictYield, substrate.ErrConflict) {
		t.Fatal("a yielded conflict stopped reading as a version conflict")
	}
}

// TestOnConflictDecode: the policy is a closed set, and it is meaningless
// without a precondition. A plausible-but-wrong word read as park would turn a
// body's declared tolerance into a parked delivery every scheduled pass, so
// decode refuses both mistakes.
func TestOnConflictDecode(t *testing.T) {
	t.Parallel()
	ds := newRaceDataset(t)
	fn := &vocabulary.Function{
		Name: "m", Package: racePackage,
		Caps: vocabulary.FunctionCaps{Emit: []string{raceWidget}},
	}
	base := func(extra map[string]any) map[string]any {
		m := map[string]any{"action": "patch", "kind": raceWidget, "id": "x"}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}

	ef, err := ds.decodeEffect(fn, base(map[string]any{
		"ifVersion": float64(4), "onConflict": "yield",
	}))
	if err != nil || ef.OnConflict != conflictYield {
		t.Fatalf("yield decode = %+v, %v", ef, err)
	}
	// The default is spellable, and it is what an absent key means.
	if ef, err := ds.decodeEffect(fn, base(map[string]any{
		"ifVersion": float64(4), "onConflict": "park",
	})); err != nil || ef.OnConflict != conflictPark {
		t.Fatalf("park decode = %+v, %v", ef, err)
	}
	if ef, err := ds.decodeEffect(fn, base(map[string]any{"ifVersion": float64(4)})); err != nil || ef.OnConflict != "" {
		t.Fatalf("absent onConflict decode = %+v, %v", ef, err)
	}

	if _, err := ds.decodeEffect(fn, base(map[string]any{"onConflict": "yield"})); err == nil ||
		!strings.Contains(err.Error(), "needs ifVersion") {
		t.Fatalf("onConflict without ifVersion: %v", err)
	}
	if _, err := ds.decodeEffect(fn, base(map[string]any{
		"ifVersion": float64(4), "onConflict": "skip",
	})); err == nil || !strings.Contains(err.Error(), "onConflict is") {
		t.Fatalf("an unknown policy decoded: %v", err)
	}
	if _, err := ds.decodeEffect(fn, base(map[string]any{
		"ifVersion": float64(4), "onConflict": true,
	})); err == nil || !strings.Contains(err.Error(), "onConflict is a string") {
		t.Fatalf("a non-string policy decoded: %v", err)
	}
	// delete carries no precondition, so the key is not in its closed set.
	if _, err := ds.decodeEffect(fn, map[string]any{
		"action": "delete", "kind": raceWidget, "id": "x", "onConflict": "yield",
	}); err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Fatalf("delete accepted onConflict: %v", err)
	}
}

func TestEffectDecodeRejectsNonBooleanIfAbsent(t *testing.T) {
	t.Parallel()
	// A typo'd ifAbsent must fail loudly, never silently become a
	// destructive upsert.
	ds := newRaceDataset(t)
	fn := &vocabulary.Function{
		Name: "m", Package: racePackage,
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
	// Replaying a merge whose loser is already former-to-the-
	// winner is a VERIFIED no-op returning the record; replaying a split
	// whose merge record is already tombstoned is a verified no-op returning
	// the split record — never a second mutation, never a park.
	ds := newRaceDataset(t)
	ctx := context.Background()
	a := racePut(t, ds, map[string]any{"name": "a"})
	b := racePut(t, ds, map[string]any{"name": "b"})

	rec, err := ds.Merge(ctx, substrate.ActorAPI, substrate.MergeInput{Kind: a.Kind, Winner: a.ID, Loser: b.ID})
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
	again, err := ds.Merge(ctx, substrate.ActorAPI, substrate.MergeInput{Kind: a.Kind, Winner: a.ID, Loser: b.ID})
	if err != nil {
		t.Fatalf("surface merge replay: %v", err)
	}
	if again.ID != rec.ID {
		t.Fatalf("surface replay returned %s, want the record %s", again.ID, rec.ID)
	}

	// Split, then replay the split effect.
	if _, err := ds.Split(ctx, substrate.ActorAPI, substrate.SplitInput{Merge: rec.ID}); err != nil {
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
