package engine_test

// What a stored value names (unitgraph.go rowUnitUses): a declaration that
// adds or removes the binding the derivation reads re-derives the kind's
// rows, tombstones included, behind the commit, the guard refuses a unit
// change until that finishes, and the live fold then equals a rebuild.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
// packed `observed` contract, binding the trait or not.
func installGauges(t *testing.T, ds substrate.Dataset, packed bool) {
	t.Helper()
	typed := func(dt string) map[string]any { return map[string]any{"type": dt} }
	listed := func(dt string) map[string]any { return map[string]any{"type": dt, "repeated": true} }
	ref := map[string]any{"type": "reference", "kind": "any"}
	data := map[string]any{
		"properties": map[string]any{
			"metric": map[string]any{"type": "reference", "trait": vocabulary.TraitMetricCore},
			"source": ref, "about": ref, "origins": map[string]any{"type": "reference", "kind": "any", "repeated": true},
			"channel": typed("string"), "form": typed("string"), "unit": typed("string"), "every": typed("duration"),
			"offsets": listed("int"), "values": listed("decimal"), "codes": listed("string"),
			"hold": typed("duration"), "resetRule": typed("string"), "resetAt": typed("datetime"),
			"delta": typed("bool"), "circular": typed("bool"), "wrapAt": typed("decimal"), "utcOffset": typed("int"),
			"revision": typed("int"), "stats": map[string]any{"type": "json", "managed": true},
		},
	}
	data["traits"] = []any{"substrate.reamde.dev/core/temporal(range)"}
	if packed {
		data["traits"] = append(data["traits"].([]any), vocabulary.TraitObservedCore+"(packed)")
	}
	if err := enginetest.Install(context.Background(), ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "gauges", Authority: gaugesPackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(gaugesPackage, 0),
			vocabulary.ActorManifest(gaugesPackage, vocabulary.PackageActor(gaugesPackage)),
			vocabulary.KindManifest(gaugesPackage, map[string]any{"singular": "gauge"}, data),
		},
	}); err != nil {
		t.Fatalf("install the gauge kind (observed %v): %v", packed, err)
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

// Binding packed `observed` to a kind whose rows already hold a `unit` makes
// those rows uses, a tombstone's too; unbinding it again makes them none. Each
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
			"unit": g.unit, "form": "gauge",
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
	fold := rebuiltEqualsLive(t, svc, ds, "after binding observed")
	if got, want := unitUsesOf(t, fold, gaugeKind), []string{"gone lb", "live kg"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the bound gauges' uses = %v, want %v", got, want)
	}
	_, err = ds.Delete(ctx, substrate.ActorAPI, vocabulary.KindConversion, "lb-kg", substrate.DeleteInput{})
	if !errors.Is(err, substrate.ErrConflict) || !strings.Contains(err.Error(), gaugeKind) {
		t.Errorf("split lb from kg once the uses are derived: err = %v, want a conflict naming %s", err, gaugeKind)
	}

	installGauges(t, ds, false)
	fold = rebuiltEqualsLive(t, svc, ds, "after unbinding observed")
	if got := unitUsesOf(t, fold, gaugeKind); len(got) != 0 {
		t.Errorf("the unbound gauges' uses = %v, want none", got)
	}
	if _, err := ds.Delete(ctx, substrate.ActorAPI, vocabulary.KindConversion, "lb-kg", substrate.DeleteInput{}); err != nil {
		t.Errorf("split lb from kg once nothing uses them: %v", err)
	}
}

// gatedReprojection is a reprojection hook that holds the pass at the given
// kind's pages until release is called, and the option that installs it.
func gatedReprojection(kind string) (engine.Option, func()) {
	gate := make(chan struct{})
	var released atomic.Bool
	opt := engine.WithTestReprojectionHook(func(ctx context.Context, k string) error {
		if k != kind || released.Load() {
			return nil
		}
		select {
		case <-gate:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	return opt, func() {
		if released.CompareAndSwap(false, true) {
			close(gate)
		}
	}
}

// forgetUnitUses puts the repository's unit uses where a store upgraded
// through migration 0016, or under a binary of an older rule, has them: no
// rows and no rule version.
func forgetUnitUses(t *testing.T, svc substrate.Service) {
	t.Helper()
	db, err := engine.SharedScopedDB(svc, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`DELETE FROM unit_uses`, `DELETE FROM unit_uses_index`} {
		if _, err := db.ExecContext(context.Background(), q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

// wantSplitRefused deletes a conversion and wants the guard's conflict,
// naming what it must.
func wantSplitRefused(t *testing.T, ds substrate.Dataset, conversion, naming string) {
	t.Helper()
	_, err := ds.Delete(context.Background(), substrate.ActorAPI, vocabulary.KindConversion, conversion, substrate.DeleteInput{})
	if !errors.Is(err, substrate.ErrConflict) || !strings.Contains(err.Error(), naming) {
		t.Errorf("delete %s: err = %v, want a conflict naming %q", conversion, err, naming)
	}
}

// The whole open of a repository whose rows predate the unit tables, with the
// seed on: the open seeds every shipped record over values that already name
// units, queues every row's uses for the pass behind it, and the guard
// refuses a split until the pass has derived them, a tombstone's included.
// Then the guard reads the uses, the live fold equals a rebuild, and the next
// open writes nothing.
func TestAnOldRepositoryOpensWithTheSeedAndTheBackfill(t *testing.T) {
	t.Parallel()
	// Written before the unit kinds: values hold the old table's codes. No
	// write stores a spelling the graph does not resolve any more, so the
	// rows are planted as the old binary stored them.
	svc, ds, dsn, root := unitSeededRepository(t, false)
	hold := installHolds(t, ds)
	ctx := context.Background()
	for _, h := range []struct{ id, value, unit string }{{"pounds", "150", "[lb_av]"}, {"kilos", "70", "kg"}, {"ounces", "3", "[oz_av]"}} {
		if err := planter(t, ds).PlantRecord(ctx, hold, h.id, map[string]any{
			"weight": map[string]any{"value": h.value, "unit": h.unit},
		}); err != nil {
			t.Fatalf("plant %s: %v", h.id, err)
		}
	}
	if _, err := ds.Delete(ctx, substrate.ActorAPI, hold, "ounces", substrate.DeleteInput{}); err != nil {
		t.Fatalf("tombstone a hold: %v", err)
	}
	if tab := unitsOf(t, ds); len(tab.Units) != 0 {
		t.Fatalf("the old repository holds %d units", len(tab.Units))
	}
	forgetUnitUses(t, svc)
	_ = svc.Close()

	gate, release := gatedReprojection(hold)
	defer release()
	svc = reopenWith(t, dsn, root, engine.WithUnitSeed(true), gate)
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open the old repository: %v", err)
	}
	units, convs := shippedCounts()
	tab := unitsOf(t, ds)
	if len(tab.Units) != units || len(tab.Conversions) != convs || len(tab.Unseeded) != 0 {
		t.Fatalf("the open seeded %d units and %d conversions, skipped %v; want %d and %d", len(tab.Units), len(tab.Conversions), tab.Unseeded, units, convs)
	}
	wantSplitRefused(t, ds, "lb-kg", "still being derived")
	// A filter whose arms unit_uses would choose answers no partial page
	// while the uses are owed: 150 [lb_av] has no use yet, so it would match
	// no arm and drop out of an answer that looked complete.
	heavier := substrate.Query{Filter: substrate.Filter{Kinds: []string{hold}, Properties: map[string]substrate.Cond{
		"weight": {Gt: map[string]any{"value": "1", "unit": "kg"}},
	}}, OrderBy: []substrate.Order{{Property: "createdAt"}}}
	if p, err := ds.List(ctx, heavier); !errors.Is(err, substrate.ErrUnavailable) || !strings.Contains(fmt.Sprint(err), "still being derived") {
		t.Errorf("a quantity filter while the uses are owed: page %v, err = %v, want ErrUnavailable", p, err)
	}
	release()
	engine.DrainIndexReprojection(t, ds)
	p, err := ds.List(ctx, heavier)
	if err != nil {
		t.Fatalf("a quantity filter once the uses are derived: %v", err)
	}
	var ids []string
	for _, r := range p.Records {
		ids = append(ids, r.ID)
	}
	if got := strings.Join(ids, ","); got != "pounds,kilos" {
		t.Errorf("a quantity filter once the uses are derived = %s, want pounds,kilos", got)
	}
	fold := rebuiltEqualsLive(t, svc, ds, "after the backfill")
	if got, want := unitUsesOf(t, fold, hold), []string{"kilos kg", "ounces [oz_av]", "pounds [lb_av]"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the holds' uses = %v, want %v", got, want)
	}
	wantSplitRefused(t, ds, "lb-kg", hold)
	wantSplitRefused(t, ds, "oz-kg", hold)

	head := maxSeq(t, ds)
	_, ds = reopenSeeded(t, svc, dsn, root)
	if got := maxSeq(t, ds); got != head {
		t.Errorf("the next open moved the head from %d to %d", head, got)
	}
	if tab := unitsOf(t, ds); len(tab.Unseeded) != 0 {
		t.Errorf("the next open left unseeded %v", tab.Unseeded)
	}
}

// An open whose stored uses are behind the rule queues them before the seed
// plans, so the seed does not judge a refresh by the uses it lacks: the
// owner deleted lb-kg, another writer retargeted oz-kg to join oz to lb, and
// values name both. Restoring oz-kg would leave lb apart from oz; it is held
// back while the uses are owed, and refused by them once they are derived.
func TestTheSeedWaitsForTheUnitUses(t *testing.T) {
	t.Parallel()
	svc, ds, dsn, root := unitSeededRepository(t, true)
	hold := installHolds(t, ds)
	ctx := context.Background()
	if _, err := ds.Delete(ctx, substrate.ActorAPI, vocabulary.KindConversion, "lb-kg", substrate.DeleteInput{}); err != nil {
		t.Fatalf("the owner deletes lb-kg: %v", err)
	}
	mustPut(t, ds, substrate.ActorSystem, substrate.PutInput{Kind: vocabulary.KindConversion, ID: "oz-kg", Properties: map[string]any{
		"to": map[string]any{"ref": vocabulary.KindUnit + "/lb"}, "factor": "0.0625",
	}})
	for _, h := range []struct{ id, unit string }{{"pounds", "[lb_av]"}, {"ounces", "[oz_av]"}} {
		mustPut(t, ds, substrate.ActorAPI, substrate.PutInput{Kind: hold, ID: h.id, Properties: map[string]any{
			"weight": map[string]any{"value": "1", "unit": h.unit},
		}})
	}
	forgetUnitUses(t, svc)
	_ = svc.Close()

	gate, release := gatedReprojection(hold)
	defer release()
	svc = reopenWith(t, dsn, root, engine.WithUnitSeed(true), gate)
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	wantHeldBack := func(when, reason string) {
		t.Helper()
		tab := unitsOf(t, ds)
		if c, _ := conversionByID(tab, "oz-kg"); c.To != "lb" {
			t.Errorf("%s: oz-kg = %+v, want it left joining oz to lb", when, c)
		}
		if len(tab.Unseeded) != 1 || tab.Unseeded[0].ID != "oz-kg" || !strings.Contains(tab.Unseeded[0].Reason, reason) {
			t.Errorf("%s: unseeded = %+v, want oz-kg alone, naming %q", when, tab.Unseeded, reason)
		}
	}
	wantHeldBack("while the uses are owed", "still being derived")
	release()
	engine.DrainIndexReprojection(t, ds)
	_, ds = reopenSeeded(t, svc, dsn, root)
	wantHeldBack("once the uses are derived", "apart")
}
