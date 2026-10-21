package engine

// The repository's unit graph (decision 0186, unitgraph.go): the write checks
// and the transaction's end, the use guard, the graph pinned to a read's
// snapshot, transaction-local until commit, and kept apart per repository.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	holdsPackage = "units.test.dev/holds"
	holdKind     = holdsPackage + "/hold"
)

// openUnitsDataset provisions a repository with the hold kind, whose
// quantities sit at the top level, in an object field, in a list and in a
// reference's link properties, and with the owner's small graph: kg, g and lb,
// joined lb to g to kg.
func openUnitsDataset(t *testing.T) *dataset {
	t.Helper()
	ctx := context.Background()
	svc, err := OpenForTest(t, ctx, MigratedDSN(t),
		WithDataRoot(t.TempDir()), WithCredentialKey(TestCredentialKey), WithKindsDir(SeedKindsDir))
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return createUnitsDataset(t, svc, testdb.Repository(t))
}

func createUnitsDataset(t *testing.T, svc substrate.Service, repo string) *dataset {
	t.Helper()
	ctx := context.Background()
	if _, err := svc.CreateRepository(ctx, repo); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	d, err := svc.Dataset(ctx, repo)
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	ds := d.(*dataset)
	quantity := map[string]any{"type": "quantity"}
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "holds", Authority: holdsPackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(holdsPackage, 0),
			vocabulary.ActorManifest(holdsPackage, vocabulary.PackageActor(holdsPackage)),
			vocabulary.KindManifest(holdsPackage, map[string]any{"singular": "hold"}, map[string]any{
				"properties": map[string]any{
					"weight":  quantity,
					"parts":   map[string]any{"type": "object", "fields": map[string]any{"mass": quantity}},
					"samples": map[string]any{"type": "quantity", "repeated": true},
					"peer": map[string]any{
						"type": "reference", "kind": "any",
						"properties": map[string]any{"load": quantity},
					},
					"span": map[string]any{"type": "quantity", "units": []any{"km"}},
				},
			}),
		},
	}); err != nil {
		t.Fatalf("install the hold kind: %v", err)
	}
	putUnit(t, ds, "kg", map[string]any{"name": "kilogram", "code": "kg"})
	putUnit(t, ds, "g", map[string]any{"name": "gram", "code": "g"})
	putUnit(t, ds, "lb", map[string]any{"name": "pound", "code": "[lb_av]"})
	putConversion(t, ds, "lb-g", "lb", "g", map[string]any{"factor": "453.59237"})
	putConversion(t, ds, "g-kg", "g", "kg", map[string]any{"factor": "0.001"})
	return ds
}

func unitRef(id string) map[string]any { return map[string]any{"ref": vocabulary.KindUnit + "/" + id} }

func putUnitErr(ds *dataset, id string, props map[string]any) error {
	_, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{Kind: vocabulary.KindUnit, ID: id, Properties: props})
	return err
}

func putUnit(t *testing.T, ds *dataset, id string, props map[string]any) {
	t.Helper()
	if err := putUnitErr(ds, id, props); err != nil {
		t.Fatalf("put unit %s: %v", id, err)
	}
}

func conversionProps(from, to string, form map[string]any) map[string]any {
	props := map[string]any{"from": unitRef(from), "to": unitRef(to)}
	for k, v := range form {
		props[k] = v
	}
	return props
}

func putConversionErr(ds *dataset, id, from, to string, form map[string]any) error {
	_, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{
		Kind: vocabulary.KindConversion, ID: id, Properties: conversionProps(from, to, form),
	})
	return err
}

func putConversion(t *testing.T, ds *dataset, id, from, to string, form map[string]any) {
	t.Helper()
	if err := putConversionErr(ds, id, from, to, form); err != nil {
		t.Fatalf("put conversion %s: %v", id, err)
	}
}

func putHold(t *testing.T, ds *dataset, id string, props map[string]any) {
	t.Helper()
	if _, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{Kind: holdKind, ID: id, Properties: props}); err != nil {
		t.Fatalf("put hold %s: %v", id, err)
	}
}

func deleteRecord(ds *dataset, kind, id string, purge bool) error {
	_, err := ds.Delete(context.Background(), substrate.ActorAPI, kind, id, substrate.DeleteInput{Purge: purge})
	return err
}

func qty(value, unit string) map[string]any { return map[string]any{"value": value, "unit": unit} }

// lbInKg is the factor the graph a read takes converts a pound into
// kilograms by.
func lbInKg(t *testing.T, at *unitGraphAt) string {
	t.Helper()
	f, _, err := at.graph.Linear("lb", "kg", false)
	if err != nil {
		t.Fatalf("lb to kg: %v", err)
	}
	return vocabulary.FormatRational(f)
}

func wantConflict(t *testing.T, what string, err error, naming string) {
	t.Helper()
	if !errors.Is(err, substrate.ErrConflict) {
		t.Errorf("%s: err = %v, want a conflict", what, err)
		return
	}
	if !strings.Contains(err.Error(), naming) {
		t.Errorf("%s: err = %v, want it to name %q", what, err, naming)
	}
}

func wantValidation(t *testing.T, what string, err error, naming string) {
	t.Helper()
	if !errors.Is(err, substrate.ErrValidation) {
		t.Errorf("%s: err = %v, want a validation refusal", what, err)
		return
	}
	if !strings.Contains(err.Error(), naming) {
		t.Errorf("%s: err = %v, want it to name %q", what, err, naming)
	}
}

// Each record is held to its shape on write.
func TestUnitRecordsAreHeldToTheirShape(t *testing.T) {
	t.Parallel()
	ds := openUnitsDataset(t)
	wantValidation(t, "a numeric factor", putConversionErr(ds, "g-lb", "g", "lb", map[string]any{"factor": 0.5}), "factor")
	wantValidation(t, "a factor and an expression", putConversionErr(ds, "g-lb", "g", "lb",
		map[string]any{"factor": "1", "expr": "value * 2.0", "inverse": "value / 2.0"}), "never both")
	wantValidation(t, "an expression returning an int", putConversionErr(ds, "g-lb", "g", "lb",
		map[string]any{"expr": "1", "inverse": "value"}), "double")
	wantValidation(t, "a conversion from a unit to itself", putConversionErr(ds, "g-g", "g", "g", map[string]any{"factor": "1"}), "twice")
	wantValidation(t, "a turn of zero", putUnitErr(ds, "deg", map[string]any{"name": "degree", "turn": "0"}), "props.turn")
}

// A cycle of linear conversions must compose to the identity: stone to pound
// to kilogram beside a direct stone to kilogram of 6.35 is refused, and the
// exact 6.35029318 is admitted.
func TestUnitGraphRefusesAnInconsistentCycle(t *testing.T) {
	t.Parallel()
	ds := openUnitsDataset(t)
	putUnit(t, ds, "st", map[string]any{"name": "stone", "code": "[stone_av]"})
	putConversion(t, ds, "st-lb", "st", "lb", map[string]any{"factor": "14"})
	wantValidation(t, "an inconsistent cycle", putConversionErr(ds, "st-kg", "st", "kg", map[string]any{"factor": "6.35"}), "st-kg")
	if err := putConversionErr(ds, "st-kg", "st", "kg", map[string]any{"factor": "6.35029318"}); err != nil {
		t.Fatalf("a consistent cycle: %v", err)
	}
}

// Two units may not answer one spelling, normalized forms included.
func TestUnitSpellingsAreUniqueInARepository(t *testing.T) {
	t.Parallel()
	ds := openUnitsDataset(t)
	wantValidation(t, "an alias another unit's code is", putUnitErr(ds, "pound", map[string]any{"name": "pound", "aliases": []any{"[lb_av]"}}), "[lb_av]")
	wantValidation(t, "a code another unit's id is", putUnitErr(ds, "gram", map[string]any{"name": "gram", "code": "g"}), "g")
	putUnit(t, ds, "ug", map[string]any{"name": "microgram", "code": "ug"})
	wantValidation(t, "a code that normalizes to another's", putUnitErr(ds, "mcg", map[string]any{"name": "microgram", "code": "µg"}), `the spelling "ug"`)
}

// The graph is judged at the transaction's end: a conversion written before
// its units in one transaction is admitted, and one whose unit never arrives
// is refused at commit.
func TestConversionWrittenBeforeItsUnitsIsJudgedAtCommit(t *testing.T) {
	t.Parallel()
	ds := openUnitsDataset(t)
	ctx := context.Background()
	err := ds.inTx(ctx, substrate.ActorAPI, false, func(tx *txn) error {
		for _, in := range []substrate.PutInput{
			{Kind: vocabulary.KindConversion, ID: "cup-mL", Properties: conversionProps("cup", "mL", map[string]any{"factor": "236.5882365"})},
			{Kind: vocabulary.KindUnit, ID: "cup", Properties: map[string]any{"name": "cup", "code": "[cup_us]"}},
			{Kind: vocabulary.KindUnit, ID: "mL", Properties: map[string]any{"name": "milliliter"}},
		} {
			if _, err := tx.put(in); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the conversion before its units: %v", err)
	}
	wantValidation(t, "a conversion naming no unit", putConversionErr(ds, "floz-mL", "floz", "mL", map[string]any{"factor": "29.5735295625"}), "floz")
}

// A read pins the revision in its snapshot: one that began before a
// conversion committed converts with the graph it began under, even once the
// cache has let that graph go, and a read after the commit sees the new one.
func TestReadKeepsItsSnapshotsGraph(t *testing.T) {
	t.Parallel()
	ds := openUnitsDataset(t)
	ctx := context.Background()
	tx, err := ds.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	was, err := ds.unitGraph(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	putConversion(t, ds, "lb-g", "lb", "g", map[string]any{"factor": "453.6"})
	ds.svc.unitGraphs.forget(ds.scope.Repository)
	again, err := ds.unitGraph(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	if again.revision != was.revision || lbInKg(t, again) != "0.45359237" {
		t.Errorf("the old read moved: revision %d -> %d, factor %s", was.revision, again.revision, lbInKg(t, again))
	}
	now, err := ds.Units(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if now.Revision != was.revision+1 {
		t.Errorf("revision after the commit = %d, want %d", now.Revision, was.revision+1)
	}
	for _, u := range now.Units {
		if u.ID == "lb" && (u.Root != "g" || u.Factor != "453.6") {
			t.Errorf("lb after the commit = %+v, want root g and factor 453.6", u)
		}
	}
}

// A transaction reads its own graph while it is open, and a rollback
// publishes nothing: the committed graph and the cache stay where they were.
func TestRolledBackGraphIsNeverPublished(t *testing.T) {
	t.Parallel()
	ds := openUnitsDataset(t)
	ctx := context.Background()
	before, err := ds.Units(ctx)
	if err != nil {
		t.Fatal(err)
	}
	errRollBack := errors.New("roll back")
	err = ds.inTx(ctx, substrate.ActorAPI, false, func(tx *txn) error {
		if _, err := tx.put(substrate.PutInput{
			Kind: vocabulary.KindConversion, ID: "lb-g",
			Properties: conversionProps("lb", "g", map[string]any{"factor": "500"}),
		}); err != nil {
			return err
		}
		inside, err := tx.unitGraph()
		if err != nil {
			return err
		}
		if inside.revision != before.Revision+1 || lbInKg(t, inside) != "0.5" {
			t.Errorf("inside the transaction: revision %d, factor %s, want %d and 0.5", inside.revision, lbInKg(t, inside), before.Revision+1)
		}
		return errRollBack
	})
	if !errors.Is(err, errRollBack) {
		t.Fatalf("the transaction ended with %v", err)
	}
	if at := ds.svc.unitGraphs.get(ds.scope.Repository, before.Revision+1); at != nil {
		t.Errorf("the rolled-back graph was published at revision %d", at.revision)
	}
	after, err := ds.Units(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision {
		t.Errorf("revision after the rollback = %d, want %d", after.Revision, before.Revision)
	}
	tx, err := ds.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	at, err := ds.unitGraph(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	if got := lbInKg(t, at); got != "0.45359237" {
		t.Errorf("factor after the rollback = %s", got)
	}
}

// Two repositories at the same revision keep their own graphs.
func TestUnitGraphsOfTwoRepositoriesStayApart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, err := OpenForTest(t, ctx, MigratedDSN(t),
		WithDataRoot(t.TempDir()), WithCredentialKey(TestCredentialKey), WithKindsDir(SeedKindsDir))
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	a := createUnitsDataset(t, svc, "a"+testdb.Repository(t))
	b := createUnitsDataset(t, svc, "b"+testdb.Repository(t))
	putUnit(t, a, "cup", map[string]any{"name": "cup"})
	putUnit(t, b, "mug", map[string]any{"name": "mug"})
	for round := range 2 {
		ua, err := a.Units(ctx)
		if err != nil {
			t.Fatal(err)
		}
		ub, err := b.Units(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if ua.Revision != ub.Revision {
			t.Fatalf("round %d: the fixture wants one revision, got %d and %d", round, ua.Revision, ub.Revision)
		}
		has := func(tab substrate.UnitTable, id string) bool {
			for _, u := range tab.Units {
				if u.ID == id {
					return true
				}
			}
			return false
		}
		if !has(ua, "cup") || has(ua, "mug") || !has(ub, "mug") || has(ub, "cup") {
			t.Errorf("round %d: a holds cup %v mug %v, b holds cup %v mug %v", round, has(ua, "cup"), has(ua, "mug"), has(ub, "cup"), has(ub, "mug"))
		}
	}
}

// A unit a stored value names cannot be deleted, wherever the value sits: a
// quantity in an object field, in a list, in a link property, a declaration's
// listed units, and a tombstone, until the tombstone is purged. An unused
// unit deletes.
func TestDeletingAUsedUnitIsRefused(t *testing.T) {
	t.Parallel()
	ds := openUnitsDataset(t)
	for _, u := range []struct{ id, code string }{
		{"oz", "[oz_av]"}, {"m", "m"}, {"mg", "mg"}, {"km", "km"}, {"L", "L"}, {"s", "s"},
	} {
		putUnit(t, ds, u.id, map[string]any{"name": u.id, "code": u.code})
	}
	putHold(t, ds, "nested", map[string]any{"parts": map[string]any{"mass": qty("3", "g")}})
	putHold(t, ds, "listed", map[string]any{"samples": []any{qty("1", "kg"), qty("2", "[oz_av]")}})
	putHold(t, ds, "linked", map[string]any{"peer": map[string]any{"ref": holdKind + "/nested", "load": qty("4", "m")}})
	putHold(t, ds, "gone", map[string]any{"weight": qty("5", "mg")})
	if err := deleteRecord(ds, holdKind, "gone", false); err != nil {
		t.Fatalf("tombstone the hold: %v", err)
	}
	// g sits between lb and kg, so its conversions go with it in each case
	// below; what refuses is the use alone.
	for _, c := range []struct{ what, unit, naming string }{
		{"an object field", "g", "a stored value of " + holdKind},
		{"a list", "oz", "(as [oz_av])"},
		{"a link property", "m", holdKind},
		{"a declaration's units", "km", "the declaration of " + holdKind + " span"},
		{"a tombstone", "mg", holdKind},
	} {
		err := ds.inTx(context.Background(), substrate.ActorAPI, false, func(tx *txn) error {
			for _, ref := range []eref{{vocabulary.KindConversion, "lb-g"}, {vocabulary.KindConversion, "g-kg"}, {vocabulary.KindUnit, c.unit}} {
				if c.unit != "g" && ref.Kind == vocabulary.KindConversion {
					continue
				}
				if _, err := tx.softDelete(ref); err != nil {
					return err
				}
			}
			return nil
		})
		wantConflict(t, c.what, err, c.naming)
	}
	if err := deleteRecord(ds, vocabulary.KindUnit, "L", false); err != nil {
		t.Errorf("delete an unused unit: %v", err)
	}
	if err := deleteRecord(ds, holdKind, "gone", true); err != nil {
		t.Fatalf("purge the hold: %v", err)
	}
	if err := deleteRecord(ds, vocabulary.KindUnit, "mg", false); err != nil {
		t.Errorf("delete a unit whose last use was purged: %v", err)
	}
}

// The observed kinds' own places a unit is named: a trace's `unit` and a
// metric's.
func TestATraceAndAMetricUseTheirUnit(t *testing.T) {
	t.Parallel()
	ds := openUnitsDataset(t)
	importVocabulary(t, ds, "observations")
	var metric, trace string
	for _, k := range ds.registry().Kinds() {
		switch {
		case strings.HasSuffix(k.Identity, "/observations/metric"):
			metric = k.Identity
		case strings.HasSuffix(k.Identity, "/observations/trace"):
			trace = k.Identity
		}
	}
	if metric == "" || trace == "" {
		t.Fatalf("the observations sample brought metric %q and trace %q", metric, trace)
	}
	putUnit(t, ds, "degF", map[string]any{"name": "degree Fahrenheit", "code": "[degF]"})
	ctx := context.Background()
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: metric, ID: "test_mass", Properties: map[string]any{
		"name": "Test mass", "measures": "mass", "unit": "[lb_av]", "form": "gauge",
	}}); err != nil {
		t.Fatalf("put the metric: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: trace, ID: "t1", Properties: map[string]any{
		"metric": map[string]any{"ref": metric + "/test_mass"}, "form": "gauge", "unit": "[degF]",
		"at": "2026-10-07T06:00:00Z", "endsAt": "2026-10-07T07:00:00Z", "utcOffset": 0,
		"offsets": []any{0, 60000}, "values": []any{"20", "21"}, "revision": 1,
	}}); err != nil {
		t.Fatalf("put the trace: %v", err)
	}
	wantConflict(t, "a trace's unit", deleteRecord(ds, vocabulary.KindUnit, "degF", false), "a stored value of "+trace)
	err := ds.inTx(ctx, substrate.ActorAPI, false, func(tx *txn) error {
		if _, err := tx.softDelete(eref{vocabulary.KindConversion, "lb-g"}); err != nil {
			return err
		}
		_, err := tx.softDelete(eref{vocabulary.KindUnit, "lb"})
		return err
	})
	wantConflict(t, "a metric's unit", err, "a stored value of "+metric)
}

// Two used units of one linear component stay joined: deleting the
// conversion between them, deleting the unused unit that joined them,
// retargeting the conversion and replacing its linear form with an expression
// are each refused, and a transaction that removes one route and writes
// another is judged by the graph it leaves.
func TestDisconnectingUsedUnitsIsRefused(t *testing.T) {
	t.Parallel()
	ds := openUnitsDataset(t)
	ctx := context.Background()
	putUnit(t, ds, "oz", map[string]any{"name": "ounce", "code": "[oz_av]"})
	putHold(t, ds, "pounds", map[string]any{"weight": qty("150", "[lb_av]")})
	putHold(t, ds, "kilos", map[string]any{"weight": qty("70", "kg")})

	wantConflict(t, "a deleted conversion", deleteRecord(ds, vocabulary.KindConversion, "lb-g", false), "leaves")
	wantConflict(t, "a deleted intermediate unit", ds.inTx(ctx, substrate.ActorAPI, false, func(tx *txn) error {
		for _, ref := range []eref{{vocabulary.KindConversion, "lb-g"}, {vocabulary.KindConversion, "g-kg"}, {vocabulary.KindUnit, "g"}} {
			if _, err := tx.softDelete(ref); err != nil {
				return err
			}
		}
		return nil
	}), "leaves")
	wantConflict(t, "a retarget", putConversionErr(ds, "lb-g", "lb", "oz", map[string]any{"factor": "16"}), "leaves")
	_, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindConversion, "lb-g", substrate.PatchInput{Properties: map[string]any{
		"factor": nil, "expr": "value * 453.59237", "inverse": "value / 453.59237",
	}})
	wantConflict(t, "a linear form replaced by an expression", err, "leaves")

	if err := ds.inTx(ctx, substrate.ActorAPI, false, func(tx *txn) error {
		if _, err := tx.softDelete(eref{vocabulary.KindConversion, "lb-g"}); err != nil {
			return err
		}
		_, err := tx.put(substrate.PutInput{
			Kind: vocabulary.KindConversion, ID: "lb-kg",
			Properties: conversionProps("lb", "kg", map[string]any{"factor": "0.45359237"}),
		})
		return err
	}); err != nil {
		t.Errorf("a route removed and another written in one transaction: %v", err)
	}
}

// A changed factor is admitted: every later read converts through it and no
// stored value moves.
func TestChangingAFactorIsAdmitted(t *testing.T) {
	t.Parallel()
	ds := openUnitsDataset(t)
	putHold(t, ds, "pounds", map[string]any{"weight": qty("150", "[lb_av]")})
	putHold(t, ds, "kilos", map[string]any{"weight": qty("70", "kg")})
	putConversion(t, ds, "lb-g", "lb", "g", map[string]any{"factor": "454"})
	tab, err := ds.Units(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range tab.Units {
		if u.ID == "lb" && u.Factor != "454" {
			t.Errorf("lb = %+v, want factor 454 to g", u)
		}
	}
	rec, err := ds.Get(context.Background(), holdKind, "pounds")
	if err != nil {
		t.Fatal(err)
	}
	if w := rec.Properties["weight"].(map[string]any); w["value"] != "150" || w["unit"] != "[lb_av]" {
		t.Errorf("the stored value moved: %v", w)
	}
}

// Reads that run while conversions commit each see one revision whole: the
// conversion a read lists and the map it gives lb agree.
func TestConcurrentReadsSeeOneRevision(t *testing.T) {
	t.Parallel()
	ds := openUnitsDataset(t)
	ctx := context.Background()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan string, 64)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				tab, err := ds.Units(ctx)
				if err != nil {
					errs <- err.Error()
					return
				}
				var conv, unit string
				for _, c := range tab.Conversions {
					if c.ID == "lb-g" {
						conv = c.Factor
					}
				}
				for _, u := range tab.Units {
					if u.ID == "lb" {
						unit = u.Factor
					}
				}
				if conv != unit {
					errs <- fmt.Sprintf("revision %d: lb-g %s beside lb %s", tab.Revision, conv, unit)
					return
				}
			}
		}()
	}
	for i := range 10 {
		factor := "453.59237"
		if i%2 == 0 {
			factor = "454"
		}
		putConversion(t, ds, "lb-g", "lb", "g", map[string]any{"factor": factor})
	}
	close(stop)
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// A repository whose rows predate unit_uses has every row's uses derived
// behind the open, and until that finishes the guard refuses what a missing
// use could have refused.
func TestUnitUsesAreDerivedBehindTheOpen(t *testing.T) {
	t.Parallel()
	ds := openUnitsDataset(t)
	ctx := context.Background()
	putHold(t, ds, "pounds", map[string]any{"weight": qty("150", "[lb_av]")})
	putHold(t, ds, "kilos", map[string]any{"weight": qty("70", "kg")})
	// The state a store upgraded through 0016 is in: rows, no uses, no version.
	for _, q := range []string{`DELETE FROM unit_uses`, `DELETE FROM unit_uses_index`} {
		if _, err := ds.db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	ds.stopIndexReprojection(false)
	if err := ds.checkUnitUses(ctx); err != nil {
		t.Fatalf("check the unit uses: %v", err)
	}
	wantConflict(t, "a split while the uses are owed", deleteRecord(ds, vocabulary.KindConversion, "lb-g", false), "still being derived")
	if err := ds.checkIndexReprojections(ctx); err != nil {
		t.Fatal(err)
	}
	ds.startIndexReprojection()
	DrainIndexReprojection(t, ds)
	var n int
	if err := ds.db.QueryRowContext(ctx, `SELECT count(*) FROM unit_uses WHERE kind = $1`, holdKind).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("the pass derived %d unit uses of the holds, want 2", n)
	}
	wantConflict(t, "a split once the uses are derived", deleteRecord(ds, vocabulary.KindConversion, "lb-g", false), "leaves")
}
