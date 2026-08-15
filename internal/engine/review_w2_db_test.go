package engine

// The wave-2 adversarial review's engine regressions, driven from inside the
// package: the run:+on: migration composition (#1), the atomic connector
// registration (#4), the create-only default trigger under the advisory lock
// (#5), the global effect-lock order (#7) and the traits/bundles rename
// conflict (#12).

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// w2Opener returns an open() that reopens the same repository schema — the
// repository-open migration harness.
func w2Opener(t *testing.T) (open func() *dataset, closeSvc func()) {
	t.Helper()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	var last substrate.Service
	created := false
	imported := false
	open = func() *dataset {
		svc, err := Open(ctx, dsn,
			WithKindsDir("../../kinds/core.substrate.reamde.dev"))
		if err != nil {
			t.Fatalf("open engine: %v", err)
		}
		last = svc
		if !created {
			if _, err := svc.CreateRepository(ctx, "geoah"); err != nil {
				t.Fatalf("create repository: %v", err)
			}
			created = true
		}
		d, err := svc.Dataset(ctx, "geoah")
		if err != nil {
			t.Fatalf("open dataset: %v", err)
		}
		if !imported {
			importVocabulary(t, d)
			imported = true
		}
		return d.(*dataset)
	}
	closeSvc = func() {
		if last != nil {
			_ = last.Close()
			last = nil
		}
	}
	t.Cleanup(closeSvc)
	return open, closeSvc
}

const (
	w2Group  = "widgets.test.dev"
	w2Widget = w2Group + "/widget"
	w2Mirror = w2Group + "/mirror"
)

// w2Manifest is the widget connector: one type, one inline mirror function,
// and optionally the function's default trigger.
func w2Manifest(withTrigger bool) enginetest.Manifest {
	m := enginetest.Manifest{
		Name: "widgets", Authority: w2Group,
		Manifests: []map[string]any{
			vocabulary.AuthorityManifest(w2Group, 0),
			vocabulary.ActorManifest(w2Group, vocabulary.AuthorityActor(w2Group)),
			vocabulary.KindManifest(w2Group, map[string]any{"singular": "widget", "plural": "widgets"},
				map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
			vocabulary.FunctionManifest(w2Group, "mirror", map[string]any{
				"description": "mirrors widgets into tasks",
				"runtime":     vocabulary.RuntimePython,
				"permissions": map[string]any{"writes": []any{"tasks.substrate.reamde.dev/task"}},
				"source": `
def main(input, host):
    env = input["envelope"]
    return {"effects": [{"action": "put", "kind": "tasks.substrate.reamde.dev/task",
                         "id": "t-" + env["change"]["id"],
                         "properties": {"title": env["record"]["properties"]["name"]}}]}
`,
			}),
		},
	}
	if withTrigger {
		m.Triggers = []enginetest.Trigger{{
			ID: "on-" + w2Mirror,
			Properties: map[string]any{
				"enabled":  true,
				"source":   map[string]any{"record": map[string]any{"kinds": []any{w2Widget}, "ops": []any{"create", "update"}}},
				"callable": vocabulary.RecordPath("core.substrate.reamde.dev/function", w2Mirror),
			},
		}}
	}
	return m
}

// windBackDialect erases the repository's schema-dialect stamp, simulating a
// store written by a pre-dialect binary. A legacy-shaped row and a
// max-dialect stamp cannot coexist in the wild (the stamp says the ladder
// already ran), so a test that hand-plants legacy content must wind the stamp
// back for the reopen to re-run the promotion steps.
func windBackDialect(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, q := range []string{`DELETE FROM vocabulary_dialect`, `DELETE FROM vocabulary_promotions`} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("wind back the schema dialect: %v", err)
		}
	}
}

// w2SeedLegacy plants the pre-wave-1 shape onto an installed repository: the
// mirror function's definition regains the given legacy keys, its cursor is
// keyed by the FUNCTION identity at the given seq, and one parked failure
// rides the same key.
func w2SeedLegacy(t *testing.T, ds *dataset, defKeys map[string]any, seq int64) {
	t.Helper()
	ctx := context.Background()
	w2SeedLegacyBlob(t, ds, w2Mirror, defKeys)
	if _, err := ds.db.ExecContext(ctx, `
		INSERT INTO trigger_cursors (trigger_id, seq, updated_at) VALUES ($1, $2, $3)
		ON CONFLICT (repository, trigger_id) DO UPDATE SET seq = EXCLUDED.seq`,
		w2Mirror, seq, time.Now().UTC()); err != nil {
		t.Fatalf("seed cursor: %v", err)
	}
	if _, err := ds.db.ExecContext(ctx, `
		INSERT INTO trigger_failures (trigger_id, seq, fire_id, record_id, attempts, last_error, parked_at)
		VALUES ($1, 1, '', 'somerecord', 3, 'old park', $2)`,
		w2Mirror, time.Now().UTC()); err != nil {
		t.Fatalf("seed failure: %v", err)
	}
}

// w2SeedLegacyBlob merges legacy keys into one declaration row's `definition`
// blob, in place. It is the LEGACY-CONTENT half of the recipe above, apart from
// it because a dialect-1 grammar fixture needs exactly this and none of the
// trigger bookkeeping: a row planted in the dialect-1 shape
// (PlantDeclarationRow) holds the declaration as this binary would write it,
// and what a fixture wants is the spelling an OLDER binary stored, which no
// write path in this tree produces.
func w2SeedLegacyBlob(t *testing.T, ds *dataset, id string, defKeys map[string]any) {
	t.Helper()
	ctx := context.Background()
	var raw []byte
	if err := ds.db.QueryRowContext(ctx,
		`SELECT props->'definition' FROM records WHERE id = $1`, id).Scan(&raw); err != nil {
		t.Fatalf("read definition: %v", err)
	}
	var def map[string]any
	if err := json.Unmarshal(raw, &def); err != nil {
		t.Fatal(err)
	}
	for k, v := range defKeys {
		def[k] = v
	}
	newRaw, err := json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.db.ExecContext(ctx, `
		UPDATE records SET props = jsonb_set(props, '{definition}', $2::jsonb) WHERE id = $1`,
		id, string(newRaw)); err != nil {
		t.Fatalf("seed old definition: %v", err)
	}
}

// w2AssertExactResume asserts the trigger owns the legacy position exactly:
// cursor at seq under the trigger id, nothing orphaned under the function
// identity, the failure re-keyed — and dispatch delivers ONLY the change
// past the stored cursor.
func w2AssertExactResume(t *testing.T, ds *dataset, seq int64, pendingID, processedID string) {
	t.Helper()
	ctx := context.Background()
	triggerID := "on-" + w2Mirror
	var got int64
	if err := ds.db.QueryRowContext(ctx,
		`SELECT seq FROM trigger_cursors WHERE trigger_id = $1`, triggerID).Scan(&got); err != nil {
		t.Fatalf("trigger cursor: %v", err)
	}
	if got != seq {
		t.Fatalf("cursor = %d, want the stored %d", got, seq)
	}
	var stale int
	if err := ds.db.QueryRowContext(ctx,
		`SELECT count(*) FROM trigger_cursors WHERE trigger_id = $1`, w2Mirror).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if stale != 0 {
		t.Fatal("the function-keyed cursor row is orphaned")
	}
	failures, err := ds.TriggerFailures(ctx, triggerID)
	if err != nil || len(failures) != 1 || failures[0].LastError != "old park" {
		t.Fatalf("failures did not re-key: %+v %v", failures, err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	if _, err := ds.Get(ctx, "tasks.substrate.reamde.dev/task", "t-"+pendingID); err != nil {
		t.Fatalf("the pending change was skipped: %v", err)
	}
	if _, err := ds.Get(ctx, "tasks.substrate.reamde.dev/task", "t-"+processedID); err == nil {
		t.Fatal("the already-processed change was redelivered")
	}
}

// Review W2 #5: create-only means TOMBSTONES count as existing and an
// owner's edit stands — a re-registration never resurrects a deliberately
// deleted default trigger and never rewires an edited one.
func TestDefaultTriggerCreateOnlyHonorsOwnerState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	open, _ := w2Opener(t)
	ds := open()
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, w2Manifest(true)); err != nil {
		t.Fatalf("register: %v", err)
	}
	triggerID := "on-" + w2Mirror

	// The owner disables the trigger; re-registration must not re-enable it.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, typeTrigger, triggerID, substrate.PatchInput{
		Properties: map[string]any{"enabled": false},
	}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, w2Manifest(true)); err != nil {
		t.Fatalf("re-register: %v", err)
	}
	tr, _, err := ds.triggerByID(ctx, triggerID)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Enabled {
		t.Fatal("re-registration re-enabled an owner-edited default trigger")
	}

	// The owner deletes it; re-registration must not resurrect the tombstone.
	if _, err := ds.Delete(ctx, substrate.ActorAPI, typeTrigger, triggerID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, w2Manifest(true)); err != nil {
		t.Fatalf("re-register: %v", err)
	}
	row, err := ds.loadRowDB(ctx, eref{Kind: typeTrigger, ID: triggerID})
	if err != nil {
		t.Fatal(err)
	}
	if row == nil || row.DeletedAt == nil {
		t.Fatalf("re-registration resurrected a tombstoned default trigger: %+v", row)
	}
}

// Review W2 #7: before ANY effect applies, the whole list's statically
// addressed records lock in one global ascending order. The barrier holds
// the SMALLEST id and proves neither concurrent effect list has touched the
// larger one — pre-fix, the list-order patch would already hold it, and the
// two merges would deadlock across the transactions.
func TestEffectListLocksInGlobalOrder(t *testing.T) {
	t.Parallel()
	ds := newRaceDataset(t)
	ctx := context.Background()
	a, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: raceWidget, ID: "aaa", Properties: map[string]any{"name": "a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	z, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: raceWidget, ID: "zzz", Properties: map[string]any{"name": "z"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// The barrier: hold the smallest id's advisory lock, so both effect
	// transactions must park at the FIRST lock of the global order.
	barrier, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := barrier.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`, ds.scope.lockKey("record|"+raceWidget+"|"+a.ID)); err != nil {
		t.Fatal(err)
	}

	apply := func(effects []effect) error {
		return ds.inTx(ctx, raceActor, false, func(tx *txn) error {
			if err := tx.lockEffectTargets(effects); err != nil {
				return err
			}
			for _, ef := range effects {
				if err := tx.applyEffect(ef); err != nil {
					return err
				}
			}
			return nil
		})
	}
	// The reviewer's interleaving: one list patches z FIRST then merges the
	// pair; the other patches a first then performs the same merge.
	calleeFirst := []effect{
		{Action: effectPatch, Type: raceWidget, ID: z.ID, Properties: map[string]any{"name": "z2"}},
		{Action: effectMerge, Type: raceWidget, ID: z.ID, Loser: a.ID},
	}
	callerFirst := []effect{
		{Action: effectPatch, Type: raceWidget, ID: a.ID, Properties: map[string]any{"name": "a2"}},
		{Action: effectMerge, Type: raceWidget, ID: z.ID, Loser: a.ID},
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, effects := range [][]effect{calleeFirst, callerFirst} {
		wg.Add(1)
		go func(effects []effect) {
			defer wg.Done()
			errs <- apply(effects)
		}(effects)
	}
	// Both must be parked at the barrier — and neither may hold the LARGER
	// id yet: the probe's try-lock on z succeeds only if both transactions
	// queued at a first, in the global order.
	time.Sleep(400 * time.Millisecond)
	var free bool
	probe, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := probe.QueryRowContext(ctx,
		`SELECT pg_try_advisory_xact_lock(hashtext($1)::bigint)`, ds.scope.lockKey("record|"+raceWidget+"|"+z.ID)).Scan(&free); err != nil {
		t.Fatal(err)
	}
	_ = probe.Rollback()
	if !free {
		t.Fatal("an effect transaction locked the larger id before the global order let it — the deadlock ordering is back")
	}
	select {
	case err := <-errs:
		t.Fatalf("an effect transaction did not park at the barrier: %v", err)
	default:
	}

	_ = barrier.Rollback()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("effect transaction: %v", err)
		}
	}
	// The pair merged exactly once; the second merge replayed as a verified
	// no-op — and no transaction was aborted by the deadlock detector.
	merged, err := ds.Get(ctx, a.Kind, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if merged.CanonicalID != z.ID {
		t.Fatalf("the merge did not land: %+v", merged)
	}
}
