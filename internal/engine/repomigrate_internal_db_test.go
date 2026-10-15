package engine

import (
	"context"
	"slices"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A repository migration folds before the stored vocabulary loads, under a
// registry that need not hold the kind of a row it rewrites. An observation
// it rewrites keeps its observed point, latest row and rollup rows as they
// stand rather than losing them as a kind's that is gone; its kind is owed a
// re-derivation, and the pass behind the open re-derives it under the loaded
// declarations, so the live store ends equal to a rebuild of it.
func TestARepositoryMigrationLeavesAnUndeclaredKindsProjectionsToThePass(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t)
	importVocabulary(t, ds, "observations")
	const home = "samples.substrate.reamde.dev"
	obs := home + "/observations/observation"
	put := func(kind, id string, props map[string]any) {
		t.Helper()
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: kind, ID: id, Properties: props}); err != nil {
			t.Fatalf("put %s %s: %v", kind, id, err)
		}
	}
	put(home+"/observations/metric", "m1", map[string]any{"name": "M1", "form": "gauge", "measures": "rate", "unit": "/min"})
	put(home+"/observations/source", "s1", map[string]any{"name": "S1"})
	put(obs, "o1", map[string]any{
		"metric": map[string]any{"ref": home + "/observations/metric/m1"}, "source": map[string]any{"ref": home + "/observations/source/s1"},
		"form": "gauge", "at": "2026-10-07T08:00:00Z", "hold": "PT1M", "value": map[string]any{"value": "70", "unit": "/min"},
	})
	db := rawDB(t, ds)
	read := func(q string) string {
		t.Helper()
		var s string
		if err := db.QueryRowContext(ctx, q).Scan(&s); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return s
	}
	const points = `SELECT count(*) || ':' || coalesce(max(value_exact)::text, '') FROM observed_points WHERE record_id = 'o1'`
	const latest = `SELECT count(*)::text FROM observed_latest`
	const rollups = `SELECT count(*)::text FROM observed_rollups`
	if got := read(points); got != "1:70" {
		t.Fatalf("before the migration the index holds %s, want one point of 70", got)
	}

	// What the open holds while runRepositoryMigrations runs: no registry.
	ds.mu.Lock()
	prevReg, prevParked := ds.reg, ds.parked
	ds.reg, ds.parked = vocabulary.NewRegistry(), nil
	ds.mu.Unlock()
	err := ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
		tx.migrating = true
		row, err := tx.loadRow(eref{Kind: obs, ID: "o1"}, true)
		if err != nil {
			return err
		}
		before := row.clone()
		row.Props["value"] = map[string]any{"value": "72", "unit": "/min"}
		if _, err := tx.foldRow(before, row, false, false); err != nil {
			return err
		}
		if err := tx.appendChange(substrate.ActorSystem, substrate.OpPatch, "o1", obs, map[string]any{"properties": []string{"value"}}); err != nil {
			return err
		}
		return tx.requestUnprojected()
	})
	ds.mu.Lock()
	ds.reg, ds.parked = prevReg, prevParked
	ds.mu.Unlock()
	if err != nil {
		t.Fatalf("a migration-shaped fold: %v", err)
	}
	if got := read(points); got != "1:70" || read(latest) != "1" || read(rollups) == "0" {
		t.Fatalf("after the migration's fold the index holds point %s, %s latest and %s rollup rows; want the stored point, latest and rollups left as they stand",
			got, read(latest), read(rollups))
	}
	if pending := pendingReprojectionsOf(t, ds); !slices.Contains(pending, obs) {
		t.Fatalf("the migration left %v owed, want %s", pending, obs)
	}

	if err := ds.checkIndexReprojections(ctx); err != nil {
		t.Fatal(err)
	}
	ds.startIndexReprojection()
	waitReprojection(t, ds)
	if got := read(points); got != "1:72" {
		t.Fatalf("after the pass the index holds %s, want the rewritten 72", got)
	}
	if pending := pendingReprojectionsOf(t, ds); len(pending) != 0 {
		t.Errorf("the pass left %v owed", pending)
	}
	live := foldSnapshotOf(t, ds)
	if _, err := ds.svc.RebuildRepository(ctx, ds.info.ID); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if rebuilt := foldSnapshotOf(t, ds); live != rebuilt {
		t.Fatalf("the rebuilt fold is not the live one\n%s", firstDifferenceOf([]byte(live), []byte(rebuilt)))
	}
}
