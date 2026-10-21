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
	"regexp"
	"strings"
	"testing"

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
