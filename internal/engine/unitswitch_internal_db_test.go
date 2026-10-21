package engine

// The unit graph switch (decision 0186, step 3 of docs/plans/dynamic-units.md):
// with WithUnitGraph on, every quantity write resolves its unit through the
// repository's graph and stores the unit's id, a declaration's `unit`,
// `units`, `delta` and bounds are held against the graph, an observed value is
// held to its metric, and a metric to the values that point at it. Each test
// here runs with the switch on; the suite's other tests run with it off.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	thingsPackage = "units.test.dev/things"
	thingKind     = thingsPackage + "/thing"
	obsAuthority  = "samples.substrate.reamde.dev"
	obsKind       = obsAuthority + "/observations/observation"
	traceKind     = obsAuthority + "/observations/trace"
	metricKind    = obsAuthority + "/observations/metric"
)

// openSwitched opens a service with the unit graph switch on, a repository
// with the fixture graph (switchedGraph) and the thing kind, whose quantities
// declare the graph dialect.
func openSwitched(t *testing.T, opts ...Option) *dataset {
	t.Helper()
	ctx := context.Background()
	all := append([]Option{WithDataRoot(t.TempDir()), WithCredentialKey(TestCredentialKey), WithKindsDir(SeedKindsDir), WithUnitGraph(true)}, opts...)
	svc, err := OpenForTest(t, ctx, MigratedDSN(t), all...)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	repo := testdb.Repository(t)
	if _, err := svc.CreateRepository(ctx, repo); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	d, err := svc.Dataset(ctx, repo)
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	ds := d.(*dataset)
	switchedGraph(t, ds)
	installThings(t, ds)
	return ds
}

// switchedGraph writes the fixture graph: mass (kg, g, lb), temperature (K,
// Cel, degF), angle (deg, arcmin, with turns), fuel economy (mpg and L/100km,
// joined by an expression), and the six units the observations sample's
// metrics name.
func switchedGraph(t *testing.T, ds *dataset) {
	t.Helper()
	for id, props := range map[string]map[string]any{
		"kg":       {"name": "kilogram", "code": "kg"},
		"g":        {"name": "gram", "code": "g"},
		"lb":       {"name": "pound", "code": "[lb_av]", "aliases": []any{"pound"}},
		"K":        {"name": "kelvin", "code": "K"},
		"Cel":      {"name": "degree Celsius", "code": "Cel", "symbol": "°C", "aliases": []any{"°C"}},
		"degF":     {"name": "degree Fahrenheit", "code": "[degF]", "symbol": "°F"},
		"deg":      {"name": "degree", "code": "deg", "symbol": "°", "turn": "360"},
		"arcmin":   {"name": "minute of arc", "code": "'", "symbol": "′", "turn": "21600"},
		"mpg":      {"name": "miles per gallon", "code": "[mi_i]/[gal_us]"},
		"L/100km":  {"name": "liters per 100 km", "code": "L/(100.km)"},
		"percent":  {"name": "percent", "code": "%", "symbol": "%"},
		"kW.h":     {"name": "kilowatt hour", "code": "kW.h"},
		"W":        {"name": "watt", "code": "W"},
		"lx":       {"name": "lux", "code": "lx"},
		"hPa":      {"name": "hectopascal", "code": "hPa"},
		"steps":    {"name": "steps", "code": "{steps}"},
		"dB":       {"name": "decibel", "code": "dB", "scale": "level"},
		"cup":      {"name": "cup", "code": "[cup_us]"},
		"mL":       {"name": "milliliter", "code": "mL"},
		"stone_av": {"name": "stone", "code": "[stone_av]"},
	} {
		putUnit(t, ds, id, props)
	}
	putConversion(t, ds, "lb-g", "lb", "g", map[string]any{"factor": "453.59237"})
	putConversion(t, ds, "g-kg", "g", "kg", map[string]any{"factor": "0.001"})
	putConversion(t, ds, "Cel-K", "Cel", "K", map[string]any{"factor": "1", "offset": "273.15"})
	putConversion(t, ds, "degF-K", "degF", "K", map[string]any{"factor": "5/9", "offset": "2298.35/9"})
	putConversion(t, ds, "arcmin-deg", "arcmin", "deg", map[string]any{"factor": "1/60"})
	putConversion(t, ds, "mpg-L/100km", "mpg", "L/100km", map[string]any{
		"expr": "235.214583 / value", "inverse": "235.214583 / value",
	})
	putConversion(t, ds, "cup-mL", "cup", "mL", map[string]any{"factor": "236.5882365"})
}

// installThings declares the thing kind: quantities anchored by `unit`,
// narrowed by `units`, read as differences by `delta`, bounded, unanchored,
// in an object field, in a list and in a reference's link properties.
func installThings(t *testing.T, ds *dataset) {
	t.Helper()
	ctx := context.Background()
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "things", Authority: thingsPackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(thingsPackage, 0),
			vocabulary.ActorManifest(thingsPackage, vocabulary.PackageActor(thingsPackage)),
			vocabulary.KindManifest(thingsPackage, map[string]any{"singular": "thing"}, thingDeclaration()),
		},
	}); err != nil {
		t.Fatalf("install the thing kind: %v", err)
	}
}

func thingDeclaration() map[string]any {
	return map[string]any{
		"displayTemplate": "{weight}",
		"properties": map[string]any{
			"weight": map[string]any{"type": "quantity", "unit": "kg", "fts": true},
			"narrow": map[string]any{"type": "quantity", "unit": "kg", "units": []any{"lb"}},
			"change": map[string]any{
				"type": "quantity", "unit": "K", "delta": true,
				"max": map[string]any{"value": "10", "unit": "K"},
			},
			"heat": map[string]any{
				"type": "quantity", "unit": "K",
				"max": map[string]any{"value": "100", "unit": "Cel"},
			},
			"bounded": map[string]any{
				"type": "quantity", "unit": "kg",
				"min": map[string]any{"value": "0", "unit": "kg"}, "max": map[string]any{"value": "200", "unit": "lb"},
			},
			"loose":   map[string]any{"type": "quantity"},
			"angle":   map[string]any{"type": "quantity", "unit": "deg"},
			"economy": map[string]any{"type": "quantity", "unit": "mpg"},
			"parts":   map[string]any{"type": "object", "fields": map[string]any{"mass": map[string]any{"type": "quantity", "unit": "kg"}}},
			"samples": map[string]any{"type": "quantity", "unit": "kg", "repeated": true},
			"peer": map[string]any{
				"type": "reference", "kind": "any",
				"properties": map[string]any{"load": map[string]any{"type": "quantity", "unit": "kg"}},
			},
		},
	}
}

func putThingErr(ds *dataset, id string, props map[string]any) (*substrate.Record, error) {
	return ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{Kind: thingKind, ID: id, Properties: props})
}

func putThing(t *testing.T, ds *dataset, id string, props map[string]any) *substrate.Record {
	t.Helper()
	rec, err := putThingErr(ds, id, props)
	if err != nil {
		t.Fatalf("put thing %s: %v", id, err)
	}
	return rec
}

func storedUnit(t *testing.T, rec *substrate.Record, name string) string {
	t.Helper()
	q, _ := rec.Properties[name].(map[string]any)
	u, _ := q["unit"].(string)
	return u
}

// A written quantity stores its unit's id, whatever spelling the writer
// sent, at every depth a quantity sits.
func TestQuantityStoresTheUnitID(t *testing.T) {
	t.Parallel()
	ds := openSwitched(t)
	rec := putThing(t, ds, "a", map[string]any{
		"weight":  qty("1", "[lb_av]"),
		"parts":   map[string]any{"mass": qty("2", "pound")},
		"samples": []any{qty("3", "[lb_av]"), qty("4", "kg")},
		"peer":    map[string]any{"ref": thingKind + "/b", "load": qty("5", "[lb_av]")},
	})
	if u := storedUnit(t, rec, "weight"); u != "lb" {
		t.Errorf("weight stored %q, want lb", u)
	}
	if m := rec.Properties["parts"].(map[string]any)["mass"].(map[string]any); m["unit"] != "lb" {
		t.Errorf("parts.mass stored %v", m)
	}
	if s := rec.Properties["samples"].([]any); s[0].(map[string]any)["unit"] != "lb" || s[1].(map[string]any)["unit"] != "kg" {
		t.Errorf("samples stored %v", s)
	}
	if p := rec.Properties["peer"].(map[string]any); p["load"].(map[string]any)["unit"] != "lb" {
		t.Errorf("peer.load stored %v", p)
	}
}

// Every spelling the graph reads resolves to the unit's id: the id, the
// code, an alias, and each normalized.
func TestOldCodesAndAliasesResolveToTheID(t *testing.T) {
	t.Parallel()
	ds := openSwitched(t)
	for spelling, want := range map[string]string{
		"lb": "lb", "[lb_av]": "lb", "pound": "lb", " kg ": "kg",
		"°C": "Cel", "Cel": "Cel", "%": "percent", "{steps}": "steps",
	} {
		rec, err := putThingErr(ds, "s", map[string]any{"loose": qty("1", spelling)})
		if err != nil {
			t.Errorf("%q: %v", spelling, err)
			continue
		}
		if u := storedUnit(t, rec, "loose"); u != want {
			t.Errorf("%q stored %q, want %q", spelling, u, want)
		}
	}
}

// A spelling no unit of the repository answers is refused by name, even one
// the engine's table reads.
func TestUnknownUnitIsRefused(t *testing.T) {
	t.Parallel()
	ds := openSwitched(t)
	_, err := putThingErr(ds, "u", map[string]any{"loose": qty("1", "mg")})
	wantValidation(t, "mg, which the table reads and the graph does not", err, `props.loose.unit: "mg" names no unit of this repository`)
	_, err = putThingErr(ds, "u", map[string]any{"samples": []any{qty("1", "kg"), qty("1", "stone")}})
	wantValidation(t, "a list item", err, `props.samples[1].unit`)
}

// `unit` admits every unit of its linear component and refuses the rest,
// an expression route included.
func TestPropertyUnitAdmitsItsComponent(t *testing.T) {
	t.Parallel()
	ds := openSwitched(t)
	for _, u := range []string{"kg", "g", "lb"} {
		if _, err := putThingErr(ds, "w", map[string]any{"weight": qty("1", u)}); err != nil {
			t.Errorf("weight in %s: %v", u, err)
		}
	}
	_, err := putThingErr(ds, "w", map[string]any{"weight": qty("1", "K")})
	wantValidation(t, "a temperature under a mass anchor", err, "K does not convert to kg")
	_, err = putThingErr(ds, "w", map[string]any{"economy": qty("1", "L/100km")})
	wantValidation(t, "an expression route", err, "expression conversion")
}

// `units` narrows the component, the anchor always admitted.
func TestPropertyUnitsNarrows(t *testing.T) {
	t.Parallel()
	ds := openSwitched(t)
	for _, u := range []string{"kg", "lb", "[lb_av]"} {
		if _, err := putThingErr(ds, "n", map[string]any{"narrow": qty("1", u)}); err != nil {
			t.Errorf("narrow in %s: %v", u, err)
		}
	}
	_, err := putThingErr(ds, "n", map[string]any{"narrow": qty("1", "g")})
	wantValidation(t, "g, of the component but not listed", err, "g is not one of the declared units kg, lb")
	err = enginetest.Install(context.Background(), ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "things", Authority: thingsPackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(thingsPackage, 0),
			vocabulary.ActorManifest(thingsPackage, vocabulary.PackageActor(thingsPackage)),
			vocabulary.KindManifest(thingsPackage, map[string]any{"singular": "thing"}, withProp(thingDeclaration(), "apart",
				map[string]any{"type": "quantity", "unit": "kg", "units": []any{"K"}})),
		},
	})
	wantValidation(t, "a listed unit outside the anchor's component", err, "the listed unit K does not convert to the declaration's unit kg")
}

// A bound compares the value converted exactly into the bound's unit, as a
// difference where the property declares `delta: true`.
func TestPropertyDeltaReadsDifferences(t *testing.T) {
	t.Parallel()
	ds := openSwitched(t)
	// A difference of 10 K is 10 Cel and 18 degF: the offset never applies.
	if _, err := putThingErr(ds, "d", map[string]any{"change": qty("18", "degF")}); err != nil {
		t.Fatalf("18 degF as a difference: %v", err)
	}
	_, err := putThingErr(ds, "d", map[string]any{"change": qty("10.5", "Cel")})
	wantValidation(t, "10.5 Cel as a difference past 10 K", err, "must be <= 10 K")
	// As an absolute temperature, 300 K is 26.85 Cel, under 100 Cel.
	if _, err := putThingErr(ds, "h", map[string]any{"heat": qty("300", "K")}); err != nil {
		t.Fatalf("300 K under 100 Cel: %v", err)
	}
	_, err = putThingErr(ds, "h", map[string]any{"heat": qty("374", "K")})
	wantValidation(t, "374 K past 100 Cel", err, "must be <= 100 °C")
}

// Bounds compare exactly across units, and a value whose unit cannot be
// compared with a bound is refused rather than admitted.
func TestBoundRefusesAValueItCannotCompare(t *testing.T) {
	t.Parallel()
	ds := openSwitched(t)
	if _, err := putThingErr(ds, "b", map[string]any{"bounded": qty("90.718474", "kg")}); err != nil {
		t.Fatalf("exactly 200 lb: %v", err)
	}
	_, err := putThingErr(ds, "b", map[string]any{"bounded": qty("90.7184741", "kg")})
	wantValidation(t, "past 200 lb by 0.1 mg", err, "must be <= 200 lb")
	_, err = putThingErr(ds, "b", map[string]any{"bounded": qty("-1", "g")})
	wantValidation(t, "below 0 kg", err, "must be >= 0 kg")
	// A bound no admitted unit reaches would refuse every value: refused at
	// the apply.
	if err := enginetest.Install(context.Background(), ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "things", Authority: thingsPackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(thingsPackage, 0),
			vocabulary.ActorManifest(thingsPackage, vocabulary.PackageActor(thingsPackage)),
			vocabulary.KindManifest(thingsPackage, map[string]any{"singular": "thing"}, withProp(thingDeclaration(), "listed",
				map[string]any{"type": "quantity", "unit": "K", "max": map[string]any{"value": "1", "unit": "kg"}})),
		},
	}); err == nil || !strings.Contains(err.Error(), "the admitted unit K does not convert to the bound's unit kg") {
		t.Fatalf("a bound a listed unit cannot reach: err = %v", err)
	}
}

func withProp(decl map[string]any, name string, p map[string]any) map[string]any {
	props := decl["properties"].(map[string]any)
	props[name] = p
	return decl
}

// A declaration naming a spelling the repository's graph does not hold is
// refused at the apply, naming the spelling.
func TestDeclarationNamingAnUnknownUnitIsRefused(t *testing.T) {
	t.Parallel()
	ds := openSwitched(t)
	err := enginetest.Install(context.Background(), ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "things", Authority: thingsPackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(thingsPackage, 0),
			vocabulary.ActorManifest(thingsPackage, vocabulary.PackageActor(thingsPackage)),
			vocabulary.KindManifest(thingsPackage, map[string]any{"singular": "thing"}, withProp(thingDeclaration(), "odd",
				map[string]any{"type": "quantity", "unit": "furlong"})),
		},
	})
	if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), `"furlong" names no unit of this repository`) {
		t.Fatalf("a declared unit the graph lacks: err = %v", err)
	}
}

// A title prints the symbol the graph holds; the search index reads the
// stored id, so editing the symbol moves neither the stored title nor a
// search result.
func TestTitlePrintsTheSymbolAndSearchTheID(t *testing.T) {
	t.Parallel()
	ds := openSwitched(t)
	putUnit(t, ds, "kg", map[string]any{"name": "kilogram", "code": "kg", "symbol": "kilo"})
	rec := putThing(t, ds, "t", map[string]any{"weight": qty("72.5", "kg")})
	if rec.Title != "72.5 kilo" {
		t.Fatalf("title = %q, want %q", rec.Title, "72.5 kilo")
	}
	putUnit(t, ds, "kg", map[string]any{"name": "kilogram", "code": "kg", "symbol": "KG"})
	if got, err := ds.Get(context.Background(), thingKind, "t"); err != nil || got.Title != "72.5 kilo" {
		t.Fatalf("after a symbol edit: title = %q, %v", got.Title, err)
	}
	fts := func() string {
		var out string
		if err := ds.db.QueryRowContext(context.Background(), `SELECT fts::text FROM records WHERE kind = $1 AND id = 't'`, thingKind).Scan(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	// The title band (A) is the stored title; the property's own text (B) is
	// the digits and the id.
	before := fts()
	if !regexp.MustCompile(`'kg':[0-9,]*B`).MatchString(before) || regexp.MustCompile(`'kilo':[0-9,]*B`).MatchString(before) {
		t.Fatalf("fts = %s, want the property indexed by its id and never its symbol", before)
	}
	putUnit(t, ds, "kg", map[string]any{"name": "kilogram", "code": "kg", "symbol": "Kilogramm"})
	if after := fts(); after != before {
		t.Fatalf("a symbol edit moved the search text: %s, was %s", after, before)
	}
}

// --- the metric rules ---------------------------------------------------------

// openSwitchedObserved is openSwitched with the observations sample imported:
// its six metrics resolve through the fixture graph.
func openSwitchedObserved(t *testing.T) *dataset {
	t.Helper()
	ds := openSwitched(t)
	if err := enginetest.ImportVocabulary(context.Background(), ds, "observations"); err != nil {
		t.Fatalf("import observations: %v", err)
	}
	return ds
}

// putInOneTx writes every input in one transaction, in order.
func putInOneTx(ds *dataset, ins ...substrate.PutInput) error {
	return ds.inTx(context.Background(), substrate.ActorAPI, false, func(tx *txn) error {
		for _, in := range ins {
			if _, err := tx.put(in); err != nil {
				return err
			}
		}
		return nil
	})
}

func metricPath(id string) string { return metricKind + "/" + id }

func putMetricErr(ds *dataset, id string, props map[string]any) error {
	base := map[string]any{"name": id, "form": "gauge"}
	for k, v := range props {
		base[k] = v
	}
	_, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{Kind: metricKind, ID: id, Properties: base})
	return err
}

func observation(metric string, value map[string]any) map[string]any {
	return map[string]any{
		"metric": map[string]any{"ref": metricPath(metric)},
		"form":   "gauge", "value": value,
		"at": "2026-10-07T06:41:12Z", "revision": 1,
	}
}

func putObservationErr(ds *dataset, id string, props map[string]any) error {
	_, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{Kind: obsKind, ID: id, Properties: props})
	return err
}

// An observed value names a live metric, judged against the transaction's
// final state: a metric deleted after its value in one transaction leaves
// the value naming nothing. (The sample's `mustExist` refuses a value of a
// metric that does not exist when it is written, before the end.)
func TestObservedValueWithoutItsMetricIsRefused(t *testing.T) {
	t.Parallel()
	ds := openSwitchedObserved(t)
	if err := putMetricErr(ds, "body_mass", map[string]any{"unit": "kg"}); err != nil {
		t.Fatal(err)
	}
	if err := putObservationErr(ds, "o1", observation("body_mass", qty("72.4", "kg"))); err != nil {
		t.Fatalf("a value of a live metric: %v", err)
	}
	if err := putMetricErr(ds, "brief", map[string]any{"unit": "kg"}); err != nil {
		t.Fatal(err)
	}
	err := ds.inTx(context.Background(), substrate.ActorAPI, false, func(tx *txn) error {
		if _, err := tx.put(substrate.PutInput{Kind: obsKind, ID: "o2", Properties: observation("brief", qty("1", "kg"))}); err != nil {
			return err
		}
		_, err := tx.softDeleteIf(eref{Kind: metricKind, ID: "brief"}, nil)
		return err
	})
	wantValidation(t, "a value whose metric the transaction deletes", err, "is no live metric record")
	// The value is judged against the metric the transaction leaves: one
	// in K, refused by the kg metric as it stood, is admitted once the same
	// transaction moves the metric to K.
	if err := putInOneTx(ds,
		substrate.PutInput{Kind: obsKind, ID: "o3", Properties: observation("brief", qty("1", "K"))},
		substrate.PutInput{Kind: metricKind, ID: "brief", Properties: map[string]any{"name": "brief", "form": "gauge", "unit": "K"}},
	); err != nil {
		t.Fatalf("a value its metric admits once the transaction ends: %v", err)
	}
}

// A metric admits its unit, its listed units, and with none listed every
// unit that converts to its unit through linear conversions.
func TestObservedValueOutsideItsMetricsUnitsIsRefused(t *testing.T) {
	t.Parallel()
	ds := openSwitchedObserved(t)
	if err := putMetricErr(ds, "mass", map[string]any{"unit": "kg"}); err != nil {
		t.Fatal(err)
	}
	if err := putMetricErr(ds, "narrow", map[string]any{"unit": "kg", "units": []any{map[string]any{"ref": vocabulary.KindUnit + "/lb"}}}); err != nil {
		t.Fatal(err)
	}
	if err := putObservationErr(ds, "a", observation("mass", qty("1", "[lb_av]"))); err != nil {
		t.Fatalf("lb under a kg metric: %v", err)
	}
	wantValidation(t, "K under a kg metric", putObservationErr(ds, "b", observation("mass", qty("1", "K"))), "K does not convert to the metric's unit kg")
	if err := putObservationErr(ds, "c", observation("narrow", qty("1", "lb"))); err != nil {
		t.Fatalf("a listed unit: %v", err)
	}
	wantValidation(t, "g under a metric listing lb", putObservationErr(ds, "d", observation("narrow", qty("1", "g"))), "g is not one of the metric's units kg, lb")
	wantValidation(t, "a metric listing a unit apart from its own",
		putMetricErr(ds, "odd", map[string]any{"unit": "kg", "units": []any{map[string]any{"ref": vocabulary.KindUnit + "/K"}}}), "K does not convert to the metric's unit kg")
}

// A metric may not be created, edited, restored or deleted so that a stored
// value pointing at it, live or tombstoned, is no longer admitted.
func TestMetricEditThatStrandsValuesIsRefused(t *testing.T) {
	t.Parallel()
	ds := openSwitchedObserved(t)
	if err := putMetricErr(ds, "m", map[string]any{"unit": "kg"}); err != nil {
		t.Fatal(err)
	}
	if err := putObservationErr(ds, "live", observation("m", qty("1", "lb"))); err != nil {
		t.Fatal(err)
	}
	if err := putObservationErr(ds, "gone", observation("m", qty("1", "g"))); err != nil {
		t.Fatal(err)
	}
	if err := deleteRecord(ds, obsKind, "gone", false); err != nil {
		t.Fatal(err)
	}
	wantConflict(t, "an edit leaving lb out", putMetricErr(ds, "m", map[string]any{"unit": "kg", "units": []any{"g"}}), "a stored value in lb")
	wantConflict(t, "an edit leaving the tombstone's g out", putMetricErr(ds, "m", map[string]any{"unit": "kg", "units": []any{"lb"}}), "a stored value in g")
	wantConflict(t, "an edit moving the unit to another component", putMetricErr(ds, "m", map[string]any{"unit": "K"}), "a stored value in")
	if err := putMetricErr(ds, "m", map[string]any{"unit": "g"}); err != nil {
		t.Fatalf("an edit every value still converts under: %v", err)
	}
	wantConflict(t, "deleting a metric values point at", deleteRecord(ds, metricKind, "m", false), "cannot be deleted")
	// Purging the values, the tombstone included, frees the metric.
	if err := deleteRecord(ds, obsKind, "live", true); err != nil {
		t.Fatal(err)
	}
	wantConflict(t, "deleting a metric a tombstone points at", deleteRecord(ds, metricKind, "m", false), "cannot be deleted")
	if err := deleteRecord(ds, obsKind, "gone", true); err != nil {
		t.Fatal(err)
	}
	if err := deleteRecord(ds, metricKind, "m", false); err != nil {
		t.Fatalf("deleting a metric nothing points at: %v", err)
	}
	// A restore is held like a create: a value of the deleted metric is
	// written again only beside it, and the restored metric admits it.
	if err := putMetricErr(ds, "m", map[string]any{"unit": "K"}); err != nil {
		t.Fatalf("restoring the metric: %v", err)
	}
	wantValidation(t, "a value the restored metric refuses", putObservationErr(ds, "again", observation("m", qty("1", "kg"))), "kg does not convert to the metric's unit K")
	// A value and a metric edit in one transaction: the edit is judged
	// against the value it writes too.
	err := putInOneTx(ds,
		substrate.PutInput{Kind: obsKind, ID: "k1", Properties: observation("m", qty("1", "K"))},
		substrate.PutInput{Kind: metricKind, ID: "m", Properties: map[string]any{"name": "m", "form": "gauge", "unit": "kg"}},
	)
	wantValidation(t, "a value the metric the transaction leaves refuses", err, "K does not convert to the metric's unit kg")
}

// --- filters, orders and cursors ----------------------------------------------

func listThings(ds *dataset, q substrate.Query) (*substrate.Page, error) {
	if len(q.Filter.Kinds) == 0 {
		q.Filter.Kinds = []string{thingKind}
	}
	return ds.List(context.Background(), q)
}

func pageIDs(p *substrate.Page) []string {
	out := make([]string, len(p.Records))
	for i, r := range p.Records {
		out[i] = r.ID
	}
	return out
}

func wantIDs(t *testing.T, what string, p *substrate.Page, err error, want ...string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	got := pageIDs(p)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s: got %v, want %v", what, got, want)
	}
}

func weightFilter(op, value, unit string) substrate.Filter {
	c := substrate.Cond{}
	q := qty(value, unit)
	switch op {
	case "gt":
		c.Gt = q
	case "lt":
		c.Lt = q
	case "eq":
		c.Eq = q
	}
	return substrate.Filter{Properties: map[string]substrate.Cond{"weight": c}}
}

// A filter converts its operand into every used unit of its linear
// component, exactly: 1 lb is 453.59237 g, so a value of 453.59237 g equals
// it and 453.5924 g is above it.
func TestFilterConvertsThroughTheLinearComponent(t *testing.T) {
	t.Parallel()
	ds := openSwitched(t)
	putThing(t, ds, "a", map[string]any{"weight": qty("453.59237", "g")})
	putThing(t, ds, "b", map[string]any{"weight": qty("453.5924", "g")})
	putThing(t, ds, "c", map[string]any{"weight": qty("0.5", "kg")})
	putThing(t, ds, "d", map[string]any{"weight": qty("1", "[lb_av]")})
	order := []substrate.Order{{Property: "id"}}
	p, err := listThings(ds, substrate.Query{Filter: weightFilter("eq", "1", "lb"), OrderBy: order})
	wantIDs(t, "equal to 1 lb", p, err, "a", "d")
	p, err = listThings(ds, substrate.Query{Filter: weightFilter("gt", "1", "pound"), OrderBy: order})
	wantIDs(t, "above 1 lb", p, err, "b", "c")
	p, err = listThings(ds, substrate.Query{Filter: weightFilter("lt", "0.4536", "kg"), OrderBy: order})
	wantIDs(t, "below 0.4536 kg", p, err, "a", "b", "d")
	_, err = listThings(ds, substrate.Query{Filter: weightFilter("gt", "1", "K")})
	wantValidation(t, "an operand of another component under a kg anchor", err, "K and kg do not convert")
	_, err = listThings(ds, substrate.Query{Filter: weightFilter("gt", "1", "stone")})
	wantValidation(t, "an operand in no unit", err, `"stone" names no unit of this repository`)
}

// The arms a filter compiles are the spellings stored values use, never every
// unit of the component.
func TestUnusedUnitsNeverWidenAFilter(t *testing.T) {
	t.Parallel()
	ds := openSwitched(t)
	putThing(t, ds, "a", map[string]any{"weight": qty("1", "kg")})
	ctx := context.Background()
	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	ty, _ := ds.registry().ByIdentity(thingKind)
	b := &builder{overRecords: true}
	if err := ds.condProp(ctx, tx, b, []*vocabulary.Kind{ty}, "weight", substrate.Cond{Gt: qty("1", "lb")}); err != nil {
		t.Fatal(err)
	}
	var arms []string
	for _, a := range b.args {
		if s, ok := a.(string); ok {
			arms = append(arms, s)
		}
	}
	if strings.Join(arms, ",") != "kg" {
		t.Fatalf("the filter compiled arms for %v, want kg alone", arms)
	}
	if b.units == nil {
		t.Fatal("the filter converted and did not say so")
	}
}

// A conversion through an expression converts one value, never a
// comparison.
func TestExpressionConversionIsRefusedInAFilter(t *testing.T) {
	t.Parallel()
	ds := openSwitched(t)
	putThing(t, ds, "a", map[string]any{"economy": qty("30", "mpg")})
	_, err := listThings(ds, substrate.Query{Filter: substrate.Filter{Properties: map[string]substrate.Cond{
		"economy": {Lt: qty("8", "L/100km")},
	}}})
	wantValidation(t, "an operand across an expression", err, "only through an expression conversion")
}

// An order needs every candidate declaration anchored in one linear
// component; it then keys each value at the component's root.
func TestOrderWithoutAnAnchorIsRefused(t *testing.T) {
	t.Parallel()
	ds := openSwitched(t)
	putThing(t, ds, "a", map[string]any{"weight": qty("1", "lb"), "loose": qty("1", "kg")})
	putThing(t, ds, "b", map[string]any{"weight": qty("0.5", "kg"), "loose": qty("1", "K")})
	putThing(t, ds, "c", map[string]any{"weight": qty("400", "g")})
	p, err := listThings(ds, substrate.Query{OrderBy: []substrate.Order{{Property: "weight"}}})
	wantIDs(t, "ordered by weight", p, err, "c", "a", "b")
	_, err = listThings(ds, substrate.Query{OrderBy: []substrate.Order{{Property: "loose"}}})
	wantValidation(t, "an order by an unanchored quantity", err, "declares no unit")
	// A second kind anchoring the name in another component is refused
	// beside the first.
	installOther(t, ds, map[string]any{"type": "quantity", "unit": "K"})
	_, err = ds.List(context.Background(), substrate.Query{
		Filter:  substrate.Filter{Kinds: []string{thingKind, otherKind}},
		OrderBy: []substrate.Order{{Property: "weight"}},
	})
	wantValidation(t, "an order across two components", err, "do not convert to one another")
}

const otherKind = thingsPackage + "/other"

func installOther(t *testing.T, ds *dataset, weight map[string]any) {
	t.Helper()
	if err := enginetest.Install(context.Background(), ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "things", Authority: thingsPackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(thingsPackage, 0),
			vocabulary.ActorManifest(thingsPackage, vocabulary.PackageActor(thingsPackage)),
			vocabulary.KindManifest(thingsPackage, map[string]any{"singular": "thing"}, thingDeclaration()),
			vocabulary.KindManifest(thingsPackage, map[string]any{"singular": "other"}, map[string]any{
				"properties": map[string]any{"weight": weight},
			}),
		},
	}); err != nil {
		t.Fatalf("install the other kind: %v", err)
	}
}

// A difference compares and orders by the factor alone, and a restore keeps
// a difference its bound admits as one.
func TestPropertyDeltaFiltersOrdersAndRestores(t *testing.T) {
	t.Parallel()
	ds := openSwitched(t)
	putThing(t, ds, "f", map[string]any{"change": qty("18", "degF")}) // 10 K
	putThing(t, ds, "c", map[string]any{"change": qty("9.5", "Cel")}) // 9.5 K
	putThing(t, ds, "k", map[string]any{"change": qty("2", "K")})
	p, err := listThings(ds, substrate.Query{Filter: substrate.Filter{Properties: map[string]substrate.Cond{
		"change": {Gt: qty("9", "K")},
	}}, OrderBy: []substrate.Order{{Property: "change"}}})
	wantIDs(t, "differences above 9 K, ascending", p, err, "c", "f")
	if err := deleteRecord(ds, thingKind, "f", false); err != nil {
		t.Fatal(err)
	}
	rec := putThing(t, ds, "f", map[string]any{"weight": qty("1", "kg")})
	if q, _ := rec.Properties["change"].(map[string]any); q["value"] != "18" || q["unit"] != "degF" {
		t.Fatalf("the restore dropped the difference: %v", rec.Properties)
	}
}

// A list's cursor carries the graph revision whenever its filter converts,
// even ordered by a column: a conversion edited between two pages refuses
// the second rather than seeking under another predicate.
func TestCursorAfterAConversionChangeIsRefused(t *testing.T) {
	t.Parallel()
	ds := openSwitched(t)
	for i, w := range []string{"1", "2", "3"} {
		putThing(t, ds, string(rune('a'+i)), map[string]any{"weight": qty(w, "lb")})
	}
	q := substrate.Query{Filter: weightFilter("gt", "100", "g"), OrderBy: []substrate.Order{{Property: "createdAt"}}, First: 1}
	first, err := listThings(ds, q)
	if err != nil || len(first.Records) != 1 || first.Cursor == "" {
		t.Fatalf("first page: %v %+v", err, first)
	}
	q.After = first.Cursor
	if _, err := listThings(ds, q); err != nil {
		t.Fatalf("a second page under the same graph: %v", err)
	}
	putConversion(t, ds, "lb-g", "lb", "g", map[string]any{"factor": "453.6"})
	_, err = listThings(ds, q)
	wantValidation(t, "a second page after a conversion changed", err, "another revision of the unit graph")
	// An order by a column alone converts nothing and keeps its cursor.
	plain := substrate.Query{OrderBy: []substrate.Order{{Property: "createdAt"}}, First: 1}
	p1, err := listThings(ds, plain)
	if err != nil {
		t.Fatal(err)
	}
	putConversion(t, ds, "lb-g", "lb", "g", map[string]any{"factor": "453.59237"})
	plain.After = p1.Cursor
	if _, err := listThings(ds, plain); err != nil {
		t.Fatalf("a cursor that converts nothing across a graph change: %v", err)
	}
}

// Under the switch an index on a quantity is refused: it would serve every
// repository holding the kind through one repository's graph.
func TestQuantityIndexIsRefused(t *testing.T) {
	t.Parallel()
	ds := openSwitched(t)
	decl := thingDeclaration()
	decl["indices"] = []any{map[string]any{"properties": []any{"weight"}}}
	err := enginetest.Install(context.Background(), ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "things", Authority: thingsPackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(thingsPackage, 0),
			vocabulary.ActorManifest(thingsPackage, vocabulary.PackageActor(thingsPackage)),
			vocabulary.KindManifest(thingsPackage, map[string]any{"singular": "thing"}, decl),
		},
	})
	wantValidation(t, "an index on a quantity", err, "an index on one is refused")
}

// No write of a unit or a conversion rewrites a stored value: a changed
// factor moves every later answer and no digit.
func TestChangedConversionMovesNoStoredValue(t *testing.T) {
	t.Parallel()
	ds := openSwitched(t)
	putThing(t, ds, "a", map[string]any{"weight": qty("1", "lb")})
	before, _ := ds.Get(context.Background(), thingKind, "a")
	p, err := listThings(ds, substrate.Query{Filter: weightFilter("gt", "453.6", "g")})
	wantIDs(t, "above 453.6 g before", p, err)
	putConversion(t, ds, "lb-g", "lb", "g", map[string]any{"factor": "454"})
	after, _ := ds.Get(context.Background(), thingKind, "a")
	if after.Version != before.Version || storedUnit(t, after, "weight") != "lb" {
		t.Fatalf("a conversion edit rewrote a stored value: %v -> %v", before.Properties, after.Properties)
	}
	p, err = listThings(ds, substrate.Query{Filter: weightFilter("gt", "453.6", "g")})
	wantIDs(t, "above 453.6 g after", p, err, "a")
}

// --- parity with the engine's table --------------------------------------------

const tableKind = thingsPackage + "/reading"

// openParity opens one repository with the shipped units seeded and the
// switch as asked, holding the reading kind, declared in the table's dialect
// so both paths read it.
func openParity(t *testing.T, on bool, repo string) *dataset {
	t.Helper()
	ctx := context.Background()
	svc, err := OpenForTest(t, ctx, MigratedDSN(t), WithDataRoot(t.TempDir()), WithCredentialKey(TestCredentialKey),
		WithKindsDir(SeedKindsDir), WithUnitGraph(on), WithUnitSeed(true))
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if _, err := svc.CreateRepository(ctx, repo); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	d, err := svc.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	ds := d.(*dataset)
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "things", Authority: thingsPackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(thingsPackage, 0),
			vocabulary.ActorManifest(thingsPackage, vocabulary.PackageActor(thingsPackage)),
			vocabulary.KindManifest(thingsPackage, map[string]any{"singular": "reading"}, map[string]any{
				"properties": map[string]any{
					"mass": map[string]any{"type": "quantity", "measures": "mass"},
					"temp": map[string]any{"type": "quantity", "measures": "temperature"},
					"rate": map[string]any{"type": "quantity", "measures": "rate"},
				},
			}),
		},
	}); err != nil {
		t.Fatalf("install the reading kind: %v", err)
	}
	return ds
}

// parityReadings are the same values, written in the table's codes, into
// both repositories.
var parityReadings = map[string]map[string]any{
	"r1": {"mass": qty("1", "[lb_av]"), "temp": qty("98.6", "[degF]"), "rate": qty("60", "/min")},
	"r2": {"mass": qty("453.59237", "g"), "temp": qty("37", "Cel"), "rate": qty("60", "/h")},
	"r3": {"mass": qty("0.5", "kg"), "temp": qty("310", "K"), "rate": qty("72", "/min")},
	"r4": {"mass": qty("16", "[oz_av]"), "temp": qty("-40", "[degF]"), "rate": qty("0.5", "/s")},
	"r5": {"mass": qty("1", "[stone_av]"), "temp": qty("-40", "Cel")},
}

// The graph's filters answer what the table's answer, over the shipped
// units, for every operand below: the seed is the table as records, so a
// filter through it may not move a row in or out.
func TestGraphFiltersMatchTheTable(t *testing.T) {
	t.Parallel()
	off := openParity(t, false, testdb.Repository(t))
	on := openParity(t, true, testdb.RepositoryLabel(t)+"2.example.com")
	for _, ds := range []*dataset{off, on} {
		for id, props := range parityReadings {
			if _, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{Kind: tableKind, ID: id, Properties: props}); err != nil {
				t.Fatalf("put %s: %v", id, err)
			}
		}
	}
	type probe struct {
		prop string
		cond substrate.Cond
	}
	var probes []probe
	for _, o := range []map[string]any{
		qty("1", "[lb_av]"), qty("453.59237", "g"), qty("0.45", "kg"), qty("15.9", "[oz_av]"), qty("6", "kg"),
	} {
		probes = append(probes, probe{"mass", substrate.Cond{Eq: o}}, probe{"mass", substrate.Cond{Gt: o}}, probe{"mass", substrate.Cond{Lte: o}})
	}
	for _, o := range []map[string]any{qty("37", "Cel"), qty("98.6", "[degF]"), qty("233.15", "K"), qty("0", "Cel")} {
		probes = append(probes, probe{"temp", substrate.Cond{Eq: o}}, probe{"temp", substrate.Cond{Gt: o}}, probe{"temp", substrate.Cond{Lt: o}})
	}
	for _, o := range []map[string]any{qty("1", "/min"), qty("60", "/min"), qty("3600", "/h")} {
		probes = append(probes, probe{"rate", substrate.Cond{Eq: o}}, probe{"rate", substrate.Cond{Gte: o}})
	}
	matched := 0
	for _, p := range probes {
		q := substrate.Query{
			Filter:  substrate.Filter{Kinds: []string{tableKind}, Properties: map[string]substrate.Cond{p.prop: p.cond}},
			OrderBy: []substrate.Order{{Property: "id"}},
		}
		a, err := off.List(context.Background(), q)
		if err != nil {
			t.Fatalf("%s %+v through the table: %v", p.prop, p.cond, err)
		}
		b, err := on.List(context.Background(), q)
		if err != nil {
			t.Fatalf("%s %+v through the graph: %v", p.prop, p.cond, err)
		}
		if got, want := strings.Join(pageIDs(b), ","), strings.Join(pageIDs(a), ","); got != want {
			t.Errorf("%s %+v: the graph answers [%s], the table [%s]", p.prop, p.cond, got, want)
		}
		if len(a.Records) > 0 && len(a.Records) < len(parityReadings) {
			matched++
		}
	}
	// The probes must split the readings, or agreeing proves nothing.
	if matched < len(probes)/2 {
		t.Fatalf("only %d of %d probes split the readings", matched, len(probes))
	}
}

// --- the aggregate read -----------------------------------------------------------

var aggDay = time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)

func putAt(ds *dataset, id, metric, form string, value map[string]any, at time.Time, extra map[string]any) error {
	props := map[string]any{
		"metric": map[string]any{"ref": metricPath(metric)}, "form": form, "value": value,
		"at": at.Format(time.RFC3339), "revision": 1,
	}
	if form == "amount" {
		props["endsAt"] = at.Add(time.Minute).Format(time.RFC3339)
	}
	for k, v := range extra {
		props[k] = v
	}
	return putObservationErr(ds, id, props)
}

// aggregateDay reads one metric over aggDay in one day bucket, raw where
// asked, and answers the row's values by reducer.
func aggregateDay(ds *dataset, raw bool, metric, unit string, reducers ...string) (map[string]string, string, error) {
	ctx := context.Background()
	if raw {
		ctx = AggregateRaw(ctx)
	}
	var reds []substrate.AggregateReducer
	for _, r := range reducers {
		reds = append(reds, substrate.AggregateReducer{Name: r})
	}
	res, err := ds.Aggregate(ctx, substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
		Metric: &substrate.AggregateMetric{Ref: metricPath(metric)}, Unit: unit,
		Range:    &substrate.AggregateRange{Gte: aggDay.Format(time.RFC3339), Lt: aggDay.Add(24 * time.Hour).Format(time.RFC3339)},
		Bucket:   &substrate.AggregateBucket{Every: "P1D"},
		Reducers: reds,
	}})
	if err != nil {
		return nil, "", err
	}
	out := map[string]string{}
	for _, row := range res.Rows {
		for _, v := range row.Values {
			if v.Value != nil {
				out[v.Reducer.Name] = *v.Value
			}
		}
	}
	return out, res.Unit, nil
}

func near(a, b string, tol float64) bool {
	x, err1 := strconv.ParseFloat(a, 64)
	y, err2 := strconv.ParseFloat(b, 64)
	return err1 == nil && err2 == nil && math.Abs(x-y) <= tol
}

// A read answers in the request's unit, else the metric's, converting each
// stored value and each rollup row through the composed affine map: a sum
// in Cel of values in degF crosses two edges (degF to K to Cel) and moves
// by the count times the offset, and the raw answer and the rollup's agree.
func TestAffineSumsAcrossTwoEdges(t *testing.T) {
	t.Parallel()
	ds := openSwitchedObserved(t)
	if err := putMetricErr(ds, "temp", map[string]any{"unit": "K"}); err != nil {
		t.Fatal(err)
	}
	for i, v := range []map[string]any{qty("10", "Cel"), qty("50", "[degF]"), qty("0", "Cel"), qty("32", "degF")} {
		if err := putAt(ds, fmt.Sprintf("t%d", i), "temp", "amount", v, aggDay.Add(time.Duration(i+1)*time.Hour), nil); err != nil {
			t.Fatalf("put t%d: %v", i, err)
		}
	}
	for _, c := range []struct{ unit, sum, avg, min, max string }{
		{"", "1112.6", "278.15", "273.15", "283.15"},
		{"Cel", "20", "5", "0", "10"},
		{"degF", "164", "41", "32", "50"},
	} {
		for _, raw := range []bool{true, false} {
			got, unit, err := aggregateDay(ds, raw, "temp", c.unit, "sum", "avg", "min", "max")
			if err != nil {
				t.Fatalf("in %q (raw %v): %v", c.unit, raw, err)
			}
			want := c.unit
			if want == "" {
				want = "K"
			}
			if unit != want {
				t.Errorf("in %q: answered in %s", c.unit, unit)
			}
			for red, w := range map[string]string{"sum": c.sum, "avg": c.avg, "min": c.min, "max": c.max} {
				if !near(got[red], w, 1e-9) {
					t.Errorf("%s in %q (raw %v) = %s, want %s", red, c.unit, raw, got[red], w)
				}
			}
		}
	}
	_, _, err := aggregateDay(ds, false, "temp", "kg", "avg")
	wantValidation(t, "a unit outside the metric's component", err, "kg does not convert to K")
}

// An expression conversion converts one value, never an aggregate.
func TestExpressionConversionIsRefusedInAnAggregate(t *testing.T) {
	t.Parallel()
	ds := openSwitchedObserved(t)
	if err := putMetricErr(ds, "economy", map[string]any{"unit": "mpg"}); err != nil {
		t.Fatal(err)
	}
	if err := putAt(ds, "e1", "economy", "gauge", qty("30", "mpg"), aggDay.Add(time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	_, _, err := aggregateDay(ds, false, "economy", "L/100km", "avg")
	wantValidation(t, "an aggregate across an expression", err, "only through an expression conversion")
}

// A factor past the range of a float is refused by name, never answered as
// null.
func TestOverflowingFactorIsRefused(t *testing.T) {
	t.Parallel()
	ds := openSwitchedObserved(t)
	huge := "1" + strings.Repeat("0", 60)
	for i := range 7 {
		putUnit(t, ds, fmt.Sprintf("u%d", i), map[string]any{"name": fmt.Sprintf("u%d", i)})
	}
	for i := range 6 {
		putConversion(t, ds, fmt.Sprintf("u%d-u%d", i, i+1), fmt.Sprintf("u%d", i), fmt.Sprintf("u%d", i+1), map[string]any{"factor": huge})
	}
	if err := putMetricErr(ds, "vast", map[string]any{"unit": "u0"}); err != nil {
		t.Fatal(err)
	}
	if err := putAt(ds, "v1", "vast", "gauge", qty("1", "u0"), aggDay.Add(time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	_, _, err := aggregateDay(ds, true, "vast", "u6", "avg")
	wantValidation(t, "a factor of 1e360", err, "past the range of a float")
}

// A circular value's angle is its value over its unit's turn, so the raw
// read and the rollup rows agree in every unit of the component, and after a
// turn edit the rollups are derived again before a read answers: the read
// is refused while they are, and agrees with the raw answer after, and
// after the service restarts.
func TestCircularRawAndRollupAgreeAfterATurnChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	open := func() (substrate.Service, *dataset) {
		svc, err := OpenForTest(t, ctx, MigratedDSN(t), WithDataRoot(root), WithCredentialKey(TestCredentialKey), WithKindsDir(SeedKindsDir), WithUnitGraph(true))
		if err != nil {
			t.Fatal(err)
		}
		d, err := svc.Dataset(ctx, testdb.Repository(t))
		if err != nil {
			t.Fatal(err)
		}
		return svc, d.(*dataset)
	}
	svc, err := OpenForTest(t, ctx, MigratedDSN(t), WithDataRoot(root), WithCredentialKey(TestCredentialKey), WithKindsDir(SeedKindsDir), WithUnitGraph(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatal(err)
	}
	d, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	ds := d.(*dataset)
	switchedGraph(t, ds)
	putUnit(t, ds, "arcmin", map[string]any{"name": "minute of arc", "code": "'", "symbol": "′", "turn": nil})
	if err := enginetest.ImportVocabulary(ctx, ds, "observations"); err != nil {
		t.Fatal(err)
	}
	if err := putMetricErr(ds, "wind", map[string]any{"unit": "deg"}); err != nil {
		t.Fatal(err)
	}
	circular := map[string]any{"circular": true}
	for i, v := range []map[string]any{qty("350", "deg"), qty("600", "arcmin"), qty("30", "deg")} {
		if err := putAt(ds, fmt.Sprintf("w%d", i), "wind", "gauge", v, aggDay.Add(time.Duration(i+1)*time.Hour), circular); err != nil {
			t.Fatalf("put w%d: %v", i, err)
		}
	}
	agree := func(ds *dataset, unit, want string) {
		t.Helper()
		raw, _, err := aggregateDay(ds, true, "wind", unit, "circularMean")
		if err != nil {
			t.Fatalf("raw circular mean in %s: %v", unit, err)
		}
		rolled, _, err := aggregateDay(ds, false, "wind", unit, "circularMean")
		if err != nil {
			t.Fatalf("rolled-up circular mean in %s: %v", unit, err)
		}
		if !near(raw["circularMean"], want, 1e-6) || !near(rolled["circularMean"], want, 1e-6) {
			t.Fatalf("circular mean in %s: raw %s, rollup %s, want %s", unit, raw["circularMean"], rolled["circularMean"], want)
		}
	}
	DrainIndexReprojection(t, ds)
	// 350, 10 and 30 degrees average to 10 degrees, 600 minutes of arc.
	agree(ds, "", "10")
	agree(ds, "arcmin", "600")
	// A circle of 720 degrees: every stored value keeps its number and its
	// unit and now names another direction, minutes of arc with it.
	putUnit(t, ds, "deg", map[string]any{"name": "degree", "code": "deg", "symbol": "°", "turn": "720"})
	if _, _, err := aggregateDay(ds, false, "wind", "", "circularMean"); !errors.Is(err, substrate.ErrObservedRederiving) {
		t.Fatalf("a read while the rollups are derived again: %v", err)
	}
	DrainIndexReprojection(t, ds)
	agree(ds, "", circularMeanDeg(720, 350, 10, 30))
	if err := putAt(ds, "w3", "wind", "gauge", qty("200", "deg"), aggDay.Add(4*time.Hour), circular); err != nil {
		t.Fatal(err)
	}
	want := circularMeanDeg(720, 350, 10, 30, 200)
	agree(ds, "", want)
	_ = svc.Close()
	svc2, ds2 := open()
	defer func() { _ = svc2.Close() }()
	DrainIndexReprojection(t, ds2)
	agree(ds2, "", want)
}

// circularMeanDeg is the circular mean of values on a circle of turn units,
// in those units.
func circularMeanDeg(turn float64, values ...float64) string {
	var s, c float64
	for _, v := range values {
		a := v * 2 * math.Pi / turn
		s += math.Sin(a)
		c += math.Cos(a)
	}
	m := math.Atan2(s, c) * turn / (2 * math.Pi)
	if m < 0 {
		m += turn
	}
	return strconv.FormatFloat(m, 'f', -1, 64)
}

// The graph's aggregate answers match the table's over the shipped units:
// the same values written in the table's codes, read raw and from rollups in
// several units of their measure.
func TestGraphAggregatesMatchTheTable(t *testing.T) {
	t.Parallel()
	off := openParity(t, false, testdb.Repository(t))
	on := openParity(t, true, testdb.RepositoryLabel(t)+"2.example.com")
	for _, ds := range []*dataset{off, on} {
		if err := enginetest.ImportVocabulary(context.Background(), ds, "observations"); err != nil {
			t.Fatalf("import observations: %v", err)
		}
		for id, unit := range map[string]string{"mass": "kg", "temp": "Cel"} {
			if err := putMetricErr(ds, id, map[string]any{"unit": unit}); err != nil {
				t.Fatal(err)
			}
		}
		for i, v := range []map[string]any{qty("1", "[lb_av]"), qty("500", "g"), qty("0.75", "kg"), qty("16", "[oz_av]")} {
			if err := putAt(ds, fmt.Sprintf("m%d", i), "mass", "amount", v, aggDay.Add(time.Duration(i+1)*time.Hour), nil); err != nil {
				t.Fatal(err)
			}
		}
		for i, v := range []map[string]any{qty("98.6", "[degF]"), qty("36.6", "Cel"), qty("310.15", "K")} {
			if err := putAt(ds, fmt.Sprintf("c%d", i), "temp", "gauge", v, aggDay.Add(time.Duration(i+1)*time.Hour), map[string]any{"hold": "PT1H"}); err != nil {
				t.Fatal(err)
			}
		}
		DrainIndexReprojection(t, ds)
	}
	for _, c := range []struct {
		metric   string
		units    []string
		reducers []string
	}{
		{"mass", []string{"", "g", "[lb_av]", "[oz_av]"}, []string{"sum", "avg", "min", "max", "count"}},
		{"temp", []string{"", "K", "[degF]"}, []string{"avg", "min", "max", "last"}},
	} {
		for _, unit := range c.units {
			for _, raw := range []bool{true, false} {
				want, _, err := aggregateDay(off, raw, c.metric, unit, c.reducers...)
				if err != nil {
					t.Fatalf("%s in %q through the table: %v", c.metric, unit, err)
				}
				got, _, err := aggregateDay(on, raw, c.metric, unit, c.reducers...)
				if err != nil {
					t.Fatalf("%s in %q through the graph: %v", c.metric, unit, err)
				}
				if len(want) != len(c.reducers) {
					t.Fatalf("%s in %q: the table answered %v", c.metric, unit, want)
				}
				for _, red := range c.reducers {
					if !near(got[red], want[red], 1e-9) {
						t.Errorf("%s %s in %q (raw %v): the graph answers %s, the table %s", c.metric, red, unit, raw, got[red], want[red])
					}
				}
			}
		}
	}
}

// A property read converts each record's quantity exactly into the request's
// unit, else the declaration's `unit`, and refuses a unit outside it.
func TestPropertyAggregateConvertsThroughTheGraph(t *testing.T) {
	t.Parallel()
	ds := openSwitched(t)
	putThing(t, ds, "a", map[string]any{"weight": qty("1", "lb")})
	putThing(t, ds, "b", map[string]any{"weight": qty("500", "g")})
	putThing(t, ds, "c", map[string]any{"weight": qty("0.25", "kg")})
	now := time.Now().UTC()
	read := func(unit string) (map[string]string, string, error) {
		res, err := ds.Aggregate(context.Background(), substrate.AggregateQuery{
			Filter: substrate.Filter{Kinds: []string{thingKind}},
			Aggregate: &substrate.Aggregate{
				Property: &substrate.AggregateProperty{Name: "weight", Anchor: "createdAt"},
				Range:    &substrate.AggregateRange{Gte: now.Add(-time.Hour).Format(time.RFC3339), Lt: now.Add(time.Hour).Format(time.RFC3339)},
				Bucket:   &substrate.AggregateBucket{Every: "PT2H", Origin: now.Add(-time.Hour).Format(time.RFC3339)},
				Reducers: []substrate.AggregateReducer{{Name: "sum"}, {Name: "max"}},
				Unit:     unit,
			},
		})
		if err != nil {
			return nil, "", err
		}
		out := map[string]string{}
		for _, row := range res.Rows {
			for _, v := range row.Values {
				if v.Value != nil {
					out[v.Reducer.Name] = *v.Value
				}
			}
		}
		return out, res.Unit, nil
	}
	got, unit, err := read("")
	if err != nil {
		t.Fatal(err)
	}
	if unit != "kg" || !near(got["sum"], "1.20359237", 1e-12) || !near(got["max"], "0.5", 1e-12) {
		t.Fatalf("in the declaration's unit: %v in %s", got, unit)
	}
	if got, _, err = read("g"); err != nil || !near(got["sum"], "1203.59237", 1e-9) {
		t.Fatalf("in g: %v %v", got, err)
	}
	_, _, err = read("K")
	wantValidation(t, "a property read in another component", err, "K does not convert to kg")
}

// --- review fixes ------------------------------------------------------------

// thingDocs is the thing kind's batch with decl as its declaration.
func thingDocs(decl map[string]any) []map[string]any {
	return []map[string]any{
		vocabulary.PackageManifest(thingsPackage, 0),
		vocabulary.ActorManifest(thingsPackage, vocabulary.PackageActor(thingsPackage)),
		vocabulary.KindManifest(thingsPackage, map[string]any{"singular": "thing"}, decl),
	}
}

// A preview reads the unit graph and the rows it judges in one snapshot: a
// conversion committed between the preview's revision read and its row read
// neither changes the preview's answer nor lands in the cache under the
// revision the preview read.
func TestDeclarationPreviewReadsTheGraphInOneSnapshot(t *testing.T) {
	t.Parallel()
	ds := openSwitched(t)
	ctx := context.Background()
	putThing(t, ds, "a", map[string]any{"weight": qty("1", "lb")})
	rev, err := readUnitRevision(ctx, ds.db)
	if err != nil {
		t.Fatal(err)
	}
	// 1 lb is 0.45359237 kg, under a 1 kg max; at 2000 g to the pound it
	// is 2 kg, past it.
	docs := thingDocs(withProp(thingDeclaration(), "weight", map[string]any{
		"type": "quantity", "unit": "kg", "max": map[string]any{"value": "1", "unit": "kg"},
	}))
	ds.svc.unitGraphs.forget(ds.scope.Repository)
	var fired atomic.Bool
	ds.svc.unitGraphLoadHook = func() {
		if fired.Swap(true) {
			return
		}
		if err := putConversionErr(ds, "lb-g", "lb", "g", map[string]any{"factor": "2000"}); err != nil {
			t.Errorf("the concurrent conversion: %v", err)
		}
		// The write cached the revision it read; dropping it leaves the
		// preview's load as the one that publishes rev.
		ds.svc.unitGraphs.forget(ds.scope.Repository)
	}
	plan, err := ds.PlanVocabularyApply(ctx, substrate.ActorAPI, docs)
	ds.svc.unitGraphLoadHook = nil
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if !fired.Load() {
		t.Fatal("the preview loaded no graph: the narrowing did not read it")
	}
	if len(plan.Blockers) != 0 {
		t.Fatalf("the preview judged a value through a graph committed after its snapshot: %v", plan.Blockers)
	}
	at := ds.svc.unitGraphs.get(ds.scope.Repository, rev)
	if at == nil {
		t.Fatalf("no graph cached at revision %d", rev)
	}
	f, _, err := at.graph.Linear("lb", "kg", false)
	if err != nil || f.RatString() != "45359237/100000000" {
		t.Fatalf("the graph cached at revision %d converts lb to kg by %v (%v), want 0.45359237", rev, f, err)
	}
	// After the commit the preview reads the new graph and refuses.
	if plan, err = ds.PlanVocabularyApply(ctx, substrate.ActorAPI, docs); err != nil || len(plan.Blockers) != 1 {
		t.Fatalf("the preview after the commit: %v %v", plan.Blockers, err)
	}
}

// applyThing declares the thing kind with decl.
func applyThing(ds *dataset, decl map[string]any) error {
	_, err := ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorAPI, thingDocs(decl))
	return err
}

// deltaThing is the thing declaration with a `delta` quantity at the top,
// in an object field and in a link property, and a mass difference.
func deltaThing(delta bool) map[string]any {
	decl := thingDeclaration()
	props := decl["properties"].(map[string]any)
	props["rise"] = map[string]any{"type": "quantity", "unit": "K", "delta": delta}
	props["gain"] = map[string]any{"type": "quantity", "unit": "kg", "delta": delta}
	props["parts"] = map[string]any{"type": "object", "fields": map[string]any{
		"mass": map[string]any{"type": "quantity", "unit": "kg"},
		"rise": map[string]any{"type": "quantity", "unit": "K", "delta": delta},
	}}
	props["peer"] = map[string]any{
		"type": "reference", "kind": "any",
		"properties": map[string]any{
			"load": map[string]any{"type": "quantity", "unit": "kg"},
			"rise": map[string]any{"type": "quantity", "unit": "K", "delta": delta},
		},
	}
	return decl
}

// Flipping `delta` rereads every stored value in a unit an offset away from
// another (10 Cel is 10 K as a difference and 283.15 K as a temperature), so
// it is refused while a live record or a tombstone holds one, at the top, in
// an object field and in a link property. A unit of a component with no
// offset reads the same either way and admits the flip.
func TestFlippedDeltaThatRereadsAValueIsRefused(t *testing.T) {
	t.Parallel()
	ds := openSwitched(t)
	if err := applyThing(ds, deltaThing(true)); err != nil {
		t.Fatalf("declare the differences: %v", err)
	}
	putThing(t, ds, "g", map[string]any{"gain": qty("2", "lb")})
	flip := func(name string) error {
		decl := deltaThing(true)
		switch name {
		case "rise", "gain":
			decl["properties"].(map[string]any)[name].(map[string]any)["delta"] = false
		case "parts.rise":
			decl["properties"].(map[string]any)["parts"].(map[string]any)["fields"].(map[string]any)["rise"].(map[string]any)["delta"] = false
		case "peer.rise":
			decl["properties"].(map[string]any)["peer"].(map[string]any)["properties"].(map[string]any)["rise"].(map[string]any)["delta"] = false
		}
		return applyThing(ds, decl)
	}
	if err := flip("gain"); err != nil {
		t.Fatalf("a mass difference read as a mass: %v", err)
	}
	if err := applyThing(ds, deltaThing(true)); err != nil {
		t.Fatalf("restore the declaration: %v", err)
	}
	for _, c := range []struct {
		name, id string
		props    map[string]any
		want     string
	}{
		{"rise", "r", map[string]any{"rise": qty("10", "Cel")}, `property "rise" declares`},
		{"parts.rise", "p", map[string]any{"parts": map[string]any{"rise": qty("1", "degF")}}, `object "parts" field "rise" declares`},
		{"peer.rise", "l", map[string]any{"peer": map[string]any{"ref": thingKind + "/g", "rise": qty("1", "K")}}, `link property "rise" declares`},
	} {
		putThing(t, ds, c.id, c.props)
		err := flip(c.name)
		if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "live") || !strings.Contains(err.Error(), "reread") {
			t.Errorf("%s flipped over a live value: err = %v", c.name, err)
		}
		if err := deleteRecord(ds, thingKind, c.id, false); err != nil {
			t.Fatal(err)
		}
		err = flip(c.name)
		if err == nil || !strings.Contains(err.Error(), "deleted records hold a value it would reread") {
			t.Errorf("%s flipped over a tombstone: err = %v", c.name, err)
		}
		if err := deleteRecord(ds, thingKind, c.id, true); err != nil {
			t.Fatal(err)
		}
		if err := flip(c.name); err != nil {
			t.Errorf("%s flipped once nothing holds a value: %v", c.name, err)
		}
		if err := applyThing(ds, deltaThing(true)); err != nil {
			t.Fatalf("restore the declaration: %v", err)
		}
	}
}
