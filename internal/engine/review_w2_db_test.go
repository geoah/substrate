package engine

// The wave-2 adversarial review's engine regressions, driven from inside the
// package: the run:+on: migration composition (#1), the atomic connector
// registration (#4), the create-only default trigger under the advisory lock
// (#5), the global effect-lock order (#7) and the traits/bundles rename
// conflict (#12).

import (
	"context"
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
	dsn := MigratedDSN(t)
	var last substrate.Service
	created := false
	imported := false
	open = func() *dataset {
		svc, err := OpenForTest(t, ctx, dsn,
			WithDataRoot(t.TempDir()),
			WithCredentialKey(TestCredentialKey), WithKindsDir(CoreKindsDir))
		if err != nil {
			t.Fatalf("open engine: %v", err)
		}
		last = svc
		if !created {
			if _, err := svc.CreateRepository(ctx, testdb.Username(t), testdb.Authority(t)); err != nil {
				t.Fatalf("create repository: %v", err)
			}
			created = true
		}
		d, err := svc.Dataset(ctx, testdb.Username(t))
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
	w2Package = "widgets.test.dev/widgets"
	w2Widget  = w2Package + "/widget"
	w2Mirror  = w2Package + "/mirror"
)

// w2Manifest is the widget connector: one type, one inline mirror function,
// and optionally the function's default trigger.
func w2Manifest(withTrigger bool) enginetest.Manifest {
	m := enginetest.Manifest{
		Name: "widgets", Authority: w2Package,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(w2Package, 0),
			vocabulary.ActorManifest(w2Package, vocabulary.PackageActor(w2Package)),
			vocabulary.KindManifest(w2Package, map[string]any{"singular": "widget"},
				map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
			vocabulary.FunctionManifest(w2Package, "mirror", map[string]any{
				"description": "mirrors widgets into tasks",
				"runtime":     vocabulary.RuntimePython,
				"permissions": map[string]any{"writes": []any{"samples.substrate.reamde.dev/tasks/task"}},
				"source": `
def main(input, host):
    env = input["envelope"]
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": "t-" + env["change"]["id"],
                         "properties": {"name": env["record"]["properties"]["name"]}}]}
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
				"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", w2Mirror),
			},
		}}
	}
	return m
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
	if _, err := ds.Delete(ctx, substrate.ActorAPI, typeTrigger, triggerID, substrate.DeleteInput{}); err != nil {
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
		`SELECT pg_advisory_xact_lock(`+advisoryKeySQL+`)`, ds.scope.lockKey("record|"+raceWidget+"|"+a.ID)); err != nil {
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
		`SELECT pg_try_advisory_xact_lock(`+advisoryKeySQL+`)`, ds.scope.lockKey("record|"+raceWidget+"|"+z.ID)).Scan(&free); err != nil {
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
