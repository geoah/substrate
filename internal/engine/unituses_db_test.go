package engine_test

// What a stored value names (unitgraph.go rowUnitUses): a declaration that
// adds or removes the binding the derivation reads re-derives the kind's
// rows, tombstones included, behind the commit, the guard refuses a unit
// change until that finishes, and the live fold then equals a rebuild.

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	gaugesPackage = "units.test.dev/gauges"
	gaugeKind     = gaugesPackage + "/gauge"
)

// installGauges declares the gauge kind with every property of core's
// `metric` contract, binding the trait or not.
func installGauges(t *testing.T, ds substrate.Dataset, metric bool) {
	t.Helper()
	text := map[string]any{"type": "string"}
	data := map[string]any{
		"properties": map[string]any{
			"name": text, "measures": text, "unit": text, "form": text, "category": text, "bucketPlace": text,
			"codeSet":          map[string]any{"type": "string", "repeated": true},
			"rollups":          map[string]any{"type": "string", "repeated": true},
			"displayPrecision": map[string]any{"type": "int"},
		},
	}
	if metric {
		data["traits"] = []any{vocabulary.TraitMetricCore}
	}
	if err := enginetest.Install(context.Background(), ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "gauges", Authority: gaugesPackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(gaugesPackage, 0),
			vocabulary.ActorManifest(gaugesPackage, vocabulary.PackageActor(gaugesPackage)),
			vocabulary.KindManifest(gaugesPackage, map[string]any{"singular": "gauge"}, data),
		},
	}); err != nil {
		t.Fatalf("install the gauge kind (metric %v): %v", metric, err)
	}
}

// unitUsesOf reads the unit_uses rows of one kind out of a fold snapshot, as
// "record_id unit", sorted.
func unitUsesOf(t *testing.T, fold []byte, kind string) []string {
	t.Helper()
	var snap struct {
		UnitUses []struct {
			Kind     string `json:"kind"`
			RecordID string `json:"record_id"`
			Unit     string `json:"unit"`
		} `json:"unit_uses"`
	}
	if err := json.Unmarshal(fold, &snap); err != nil {
		t.Fatalf("read the fold snapshot: %v", err)
	}
	out := []string{}
	for _, u := range snap.UnitUses {
		if u.Kind == kind {
			out = append(out, u.RecordID+" "+u.Unit)
		}
	}
	sort.Strings(out)
	return out
}

// rebuiltEqualsLive rebuilds the repository and fails unless the fold comes
// back byte for byte.
func rebuiltEqualsLive(t *testing.T, svc substrate.Service, ds substrate.Dataset, when string) []byte {
	t.Helper()
	before := foldOf(t, ds)
	if _, err := svc.(rebuilder).RebuildRepository(context.Background(), testdb.Repository(t)); err != nil {
		t.Fatalf("%s: rebuild: %v", when, err)
	}
	if after := foldOf(t, ds); string(after) != string(before) {
		t.Fatalf("%s: the rebuilt fold differs from the live one\n%s", when, firstDifference(before, after))
	}
	return before
}

// Binding `metric` to a kind whose rows already hold a `unit` makes those
// rows uses, a tombstone's too; unbinding it again makes them none. Each
// change re-derives the rows behind the commit, the guard refuses splitting
// lb from kg until then, and the live fold equals a rebuild after each.
func TestABindingChangeRederivesTheUnitUses(t *testing.T) {
	t.Parallel()
	var blocking atomic.Bool
	gate := make(chan struct{})
	svc, _ := newService(t, engine.WithUnitSeed(true), engine.WithTestReprojectionHook(func(ctx context.Context, kind string) error {
		if kind != gaugeKind || !blocking.Load() {
			return nil
		}
		select {
		case <-gate:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}))
	ctx := context.Background()
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	installGauges(t, ds, false)
	for _, g := range []struct{ id, unit string }{{"live", "kg"}, {"gone", "lb"}} {
		mustPut(t, ds, substrate.ActorAPI, substrate.PutInput{Kind: gaugeKind, ID: g.id, Properties: map[string]any{
			"name": g.id, "measures": "mass", "unit": g.unit, "form": "gauge",
		}})
	}
	if _, err := ds.Delete(ctx, substrate.ActorAPI, gaugeKind, "gone", substrate.DeleteInput{}); err != nil {
		t.Fatalf("tombstone a gauge: %v", err)
	}
	if got := unitUsesOf(t, foldOf(t, ds), gaugeKind); len(got) != 0 {
		t.Fatalf("an unbound gauge's unit counts as a use: %v", got)
	}

	blocking.Store(true)
	installGauges(t, ds, true)
	_, err = ds.Delete(ctx, substrate.ActorAPI, vocabulary.KindConversion, "lb-kg", substrate.DeleteInput{})
	if !errors.Is(err, substrate.ErrConflict) || !strings.Contains(err.Error(), "still being derived") {
		t.Errorf("split lb from kg while the uses are owed: err = %v, want a conflict naming the derivation", err)
	}
	blocking.Store(false)
	close(gate)
	fold := rebuiltEqualsLive(t, svc, ds, "after binding metric")
	if got, want := unitUsesOf(t, fold, gaugeKind), []string{"gone lb", "live kg"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the bound gauges' uses = %v, want %v", got, want)
	}
	_, err = ds.Delete(ctx, substrate.ActorAPI, vocabulary.KindConversion, "lb-kg", substrate.DeleteInput{})
	if !errors.Is(err, substrate.ErrConflict) || !strings.Contains(err.Error(), gaugeKind) {
		t.Errorf("split lb from kg once the uses are derived: err = %v, want a conflict naming %s", err, gaugeKind)
	}

	installGauges(t, ds, false)
	fold = rebuiltEqualsLive(t, svc, ds, "after unbinding metric")
	if got := unitUsesOf(t, fold, gaugeKind); len(got) != 0 {
		t.Errorf("the unbound gauges' uses = %v, want none", got)
	}
	if _, err := ds.Delete(ctx, substrate.ActorAPI, vocabulary.KindConversion, "lb-kg", substrate.DeleteInput{}); err != nil {
		t.Errorf("split lb from kg once nothing uses them: %v", err)
	}
}
