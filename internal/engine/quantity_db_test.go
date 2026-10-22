package engine

// The quantity datatype against Postgres, through the repository's unit
// graph (decision 0186): what a write stores and a read returns, the title a
// template renders from it, the filter that converts its operand exactly into
// every used unit of the operand's linear component, the comparator's
// half-line, the order by the value at its component's root, and the
// narrowings a declaration change may not make over live or deleted rows.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const quantityPackage = "quantity.example.substrate.reamde.dev/quantity"

const readingKind = quantityPackage + "/reading"

func qv(value, unit string, cmp ...string) map[string]any {
	m := map[string]any{"value": value, "unit": unit}
	if len(cmp) > 0 {
		m["comparator"] = cmp[0]
	}
	return m
}

// readingManifest declares the one test kind; overrides replace the default
// declaration of a named property.
func readingManifest(overrides map[string]any) map[string]any {
	props := map[string]any{
		"name":   map[string]any{"type": "string"},
		"weight": map[string]any{"type": "quantity", "unit": "kg"},
		"temp":   map[string]any{"type": "quantity", "unit": "K"},
		"result": map[string]any{"type": "quantity", "unit": "g/L", "comparator": true},
		"pulse":  map[string]any{"type": "quantity"},
		"height": map[string]any{"type": "quantity", "unit": "m", "fts": true},
	}
	for k, v := range overrides {
		props[k] = v
	}
	return vocabulary.KindManifest(quantityPackage,
		map[string]any{"singular": "reading"},
		map[string]any{
			"displayTemplate": "{name}: {weight}",
			"properties":      props,
		})
}

// readingUnits are the units whose linear components the reading kind's
// tests write in: mass, temperature, mass concentration, rate, length,
// acidity and the levels of a power.
var readingUnits = []string{"kg", "K", "g/L", "1/min", "m", "pH", "dBm"}

// openReadingsDataset opens a dataset holding the shipped units of
// readingUnits' components.
func openReadingsDataset(t *testing.T) *dataset {
	t.Helper()
	ds := openInternalDataset(t)
	if err := enginetest.PutShippedUnits(context.Background(), ds, readingUnits...); err != nil {
		t.Fatalf("put the shipped units: %v", err)
	}
	return ds
}

func installReadings(t *testing.T, rows map[string]map[string]any) *dataset {
	t.Helper()
	return installReadingsWith(t, nil, rows)
}

// installReadingsWith is installReadings with more units of the owner's,
// written by units before the kind lands.
func installReadingsWith(t *testing.T, units func(*dataset), rows map[string]map[string]any) *dataset {
	t.Helper()
	return installReadingsDeclared(t, nil, units, rows)
}

// installReadingsDeclared is installReadingsWith over a reading kind whose
// named properties overrides replaces.
func installReadingsDeclared(t *testing.T, overrides map[string]any, units func(*dataset), rows map[string]map[string]any) *dataset {
	t.Helper()
	ctx := context.Background()
	ds := openReadingsDataset(t)
	if units != nil {
		units(ds)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(quantityPackage, 0),
		readingManifest(overrides),
	}); err != nil {
		t.Fatalf("install the reading kind: %v", err)
	}
	for name, props := range rows {
		in := map[string]any{"name": name}
		for k, v := range props {
			in[k] = v
		}
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: readingKind, ID: name, Properties: in}); err != nil {
			t.Fatalf("put %s: %v", name, err)
		}
	}
	return ds
}

func readingNames(t *testing.T, ds *dataset, q substrate.Query) []string {
	t.Helper()
	q.Filter.Kinds = []string{readingKind}
	if q.First == 0 {
		q.First = 50
	}
	if q.OrderBy == nil {
		q.OrderBy = []substrate.Order{{Property: "name"}}
	}
	page, err := ds.List(context.Background(), q)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	out := make([]string, 0, len(page.Records))
	for _, r := range page.Records {
		out = append(out, r.Properties["name"].(string))
	}
	return out
}

func TestQuantityStoresReadsAndTitles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installReadings(t, nil)
	rec, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: readingKind, Properties: map[string]any{
		"name": "scale", "weight": qv("72.50", "kg"), "temp": qv("98.6", "°F"), "pulse": qv("62", "bpm"),
		"result": qv("0.5", "ng/mL", "<"),
	}})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := ds.Get(ctx, rec.Kind, rec.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	for name, want := range map[string]map[string]any{
		"weight": qv("72.50", "kg"), "temp": qv("98.6", "degF"), "pulse": qv("62", "1/min"),
		"result": qv("0.5", "ng/mL", "<"),
	} {
		v, _ := got.Properties[name].(map[string]any)
		if len(v) != len(want) {
			t.Fatalf("%s read back %#v, want %#v", name, got.Properties[name], want)
		}
		for k, w := range want {
			if v[k] != w {
				t.Fatalf("%s read back %#v, want %#v", name, got.Properties[name], want)
			}
		}
	}
	if got.Title != "scale: 72.50 kg" {
		t.Fatalf("title = %q, want %q", got.Title, "scale: 72.50 kg")
	}
	// A quantity declared `fts: true` indexes its value text, number and
	// unit, so a search finds the record by either.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: readingKind, ID: "tall", Properties: map[string]any{
		"name": "tall", "height": qv("1.93", "m"),
	}}); err != nil {
		t.Fatalf("put: %v", err)
	}
	for _, q := range []string{"1.93", "1.93 m"} {
		if got := readingNames(t, ds, substrate.Query{Filter: substrate.Filter{Search: q}}); strings.Join(got, ",") != "tall" {
			t.Fatalf("search %q found %v, want the tall reading", q, got)
		}
	}
	for name, bad := range map[string]map[string]any{
		"a JSON number":            {"weight": map[string]any{"value": 72.5, "unit": "kg"}},
		"a unit outside the mass":  {"weight": qv("1", "km")},
		"an unknown unit":          {"weight": qv("1", "kgs")},
		"an undeclared comparator": {"weight": qv("1", "kg", "<")},
		"an ambiguous unit":        {"pulse": qv("1", "J/s")},
		"a stray member":           {"weight": map[string]any{"value": "1", "unit": "kg", "scale": "2"}},
		"more digits than numeric": {"pulse": qv("1."+strings.Repeat("1", 16384), "m")},
	} {
		props := map[string]any{"name": "bad"}
		for k, v := range bad {
			props[k] = v
		}
		_, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: readingKind, Properties: props})
		if !errors.Is(err, substrate.ErrValidation) {
			t.Fatalf("%s: err = %v, want a validation error", name, err)
		}
	}
}

// A comparison converts its operand into each stored unit exactly: 155 pounds
// is 70.30681735 kg to the last digit, so a row stored in grams at that weight
// is equal to it, and nothing rounds on the way.
func TestFilterByQuantityConvertsExactly(t *testing.T) {
	t.Parallel()
	ds := installReadings(t, map[string]map[string]any{
		"a72":    {"weight": qv("72.4", "kg")},
		"b155lb": {"weight": qv("155", "[lb_av]")},
		"c155g":  {"weight": qv("70306.81735", "g")},
		"d80":    {"weight": qv("80", "kg")},
		"e100":   {"weight": qv("0.1", "t")},
		"f12st":  {"weight": qv("12", "[stone_av]")},
	})
	for _, tc := range []struct {
		name string
		cond substrate.Cond
		want []string
	}{
		{"lt in pounds", substrate.Cond{Lt: qv("160", "[lb_av]")}, []string{"a72", "b155lb", "c155g"}},
		{"eq across three units", substrate.Cond{Eq: qv("155", "[lb_av]")}, []string{"b155lb", "c155g"}},
		{"eq in kilograms", substrate.Cond{Eq: qv("70.30681735", "kg")}, []string{"b155lb", "c155g"}},
		{"gt excludes the equal ones", substrate.Cond{Gt: qv("70.30681735", "kg")}, []string{"a72", "d80", "e100", "f12st"}},
		{"gte includes them", substrate.Cond{Gte: qv("70306.81735", "g")}, []string{"a72", "b155lb", "c155g", "d80", "e100", "f12st"}},
		{"a range", substrate.Cond{Gt: qv("71", "kg"), Lte: qv("12", "[stone_av]")}, []string{"a72", "f12st"}},
		{"in", substrate.Cond{In: []any{qv("80", "kg"), qv("100000", "g")}}, []string{"d80", "e100"}},
		{"an alias operand", substrate.Cond{Gte: qv("100000000000", "mcg")}, []string{"e100"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := readingNames(t, ds, substrate.Query{Filter: substrate.Filter{
				Properties: map[string]substrate.Cond{"weight": tc.cond},
			}})
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("filtered to %v, want %v", got, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		name string
		cond substrate.Cond
		want string
	}{
		{"another component", substrate.Cond{Lt: qv("1", "m")}, "m and kg do not convert"},
		{"a bare number", substrate.Cond{Lt: "72"}, "is an object"},
		{"a number value", substrate.Cond{Lt: map[string]any{"value": 72, "unit": "kg"}}, "written as a string"},
		{"a comparator on the operand", substrate.Cond{Lt: qv("72", "kg", "<")}, "an operand is one value"},
		{"prefix", substrate.Cond{Prefix: "7"}, "is a quantity"},
		{"match", substrate.Cond{Match: "kg"}, "match needs a text property"},
		{"contains", substrate.Cond{Contains: qv("1", "kg")}, "contains is for a list"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{
				Kinds:      []string{readingKind},
				Properties: map[string]substrate.Cond{"weight": tc.cond},
			}})
			if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want a validation error naming %q", err, tc.want)
			}
		})
	}
}

// A comparison that no stored unit converts into selects nothing and answers
// an empty page: a property no row holds yet, an ids filter naming no row, an
// operand of another measure than every stored unit.
func TestQuantityFilterNoStoredUnitConvertsIsEmpty(t *testing.T) {
	t.Parallel()
	ds := installReadings(t, map[string]map[string]any{
		"heavy": {"weight": qv("80", "kg")},
	})
	if _, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{Kind: readingKind, ID: "rested", Properties: map[string]any{
		"name": "rested", "pulse": qv("58", "/min"),
	}}); err != nil {
		t.Fatalf("put: %v", err)
	}
	for name, f := range map[string]substrate.Filter{
		"ids naming no row": {IDs: []string{"no-such-id"}, Properties: map[string]substrate.Cond{
			"pulse": {Gt: qv("50", "/min")},
		}},
		"an operand no stored unit converts into": {Properties: map[string]substrate.Cond{
			"pulse": {Gt: qv("1", "kg")},
		}},
		"in, the same": {Properties: map[string]substrate.Cond{
			"pulse": {In: []any{qv("1", "kg")}},
		}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := readingNames(t, ds, substrate.Query{Filter: f}); len(got) != 0 {
				t.Fatalf("filtered to %v, want nothing", got)
			}
		})
	}
}

// A level anchored by `unit` needs no `units:` list: its component is the
// set, and a filter compares in it. `[pH]` under `unit: pH` matches itself
// and a range around it.
func TestFilterByALevelWithoutUnits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openReadingsDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(quantityPackage, 0),
		readingManifest(map[string]any{"ph": map[string]any{"type": "quantity", "unit": "pH"}}),
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	for name, ph := range map[string]string{"neutral": "7", "acid": "4.5"} {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: readingKind, ID: name, Properties: map[string]any{
			"name": name, "ph": qv(ph, "[pH]"),
		}}); err != nil {
			t.Fatalf("put %s: %v", name, err)
		}
	}
	for name, tc := range map[string]struct {
		cond substrate.Cond
		want []string
	}{
		"equal":   {substrate.Cond{Eq: qv("7", "[pH]")}, []string{"neutral"}},
		"in":      {substrate.Cond{In: []any{qv("4.5", "[pH]")}}, []string{"acid"}},
		"a range": {substrate.Cond{Gte: qv("4", "[pH]"), Lt: qv("7", "[pH]")}, []string{"acid"}},
		"above":   {substrate.Cond{Gt: qv("3", "[pH]")}, []string{"acid", "neutral"}},
	} {
		got := readingNames(t, ds, substrate.Query{Filter: substrate.Filter{Properties: map[string]substrate.Cond{"ph": tc.cond}}})
		if !slices.Equal(got, tc.want) {
			t.Fatalf("%s: filtered to %v, want %v", name, got, tc.want)
		}
	}
}

// A filter naming no kind compares a quantity as a filter naming its kinds
// does: over every kind that declares the name, where all of them declare one
// quantity. Where another kind declares the name as something else, a
// quantity operand is refused instead of compared as text.
func TestFilterByQuantityWithoutKinds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installReadings(t, map[string]map[string]any{
		"grams":     {"weight": qv("1000", "g")},
		"kilograms": {"weight": qv("1", "kg")},
		"heavier":   {"weight": qv("2", "kg")},
	})
	list := func(c substrate.Cond) ([]string, error) {
		page, err := ds.List(ctx, substrate.Query{
			First:   50,
			Filter:  substrate.Filter{Properties: map[string]substrate.Cond{"weight": c}},
			OrderBy: []substrate.Order{{Property: "name"}},
		})
		if err != nil {
			return nil, err
		}
		var out []string
		for _, r := range page.Records {
			out = append(out, r.Properties["name"].(string))
		}
		return out, nil
	}
	for name, tc := range map[string]struct {
		cond substrate.Cond
		want []string
	}{
		"equal across units": {substrate.Cond{Eq: qv("1", "kg")}, []string{"grams", "kilograms"}},
		"in":                 {substrate.Cond{In: []any{qv("2000", "g")}}, []string{"heavier"}},
		"a range":            {substrate.Cond{Gt: qv("1500", "g")}, []string{"heavier"}},
	} {
		got, err := list(tc.cond)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !slices.Equal(got, tc.want) {
			t.Fatalf("%s: filtered to %v, want %v", name, got, tc.want)
		}
	}
	// A second kind declaring the name as a quantity anchored in another
	// component takes no part in a comparison its component does not hold:
	// it neither refuses the operand nor matches. An operand no kind's
	// component holds is refused.
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.KindManifest(quantityPackage, map[string]any{"singular": "span"}, map[string]any{
			"displayTemplate": "{name}",
			"properties": map[string]any{
				"name":   map[string]any{"type": "string"},
				"weight": map[string]any{"type": "quantity", "unit": "m"},
			},
		}),
	}); err != nil {
		t.Fatalf("declare weight as a length on a second kind: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: quantityPackage + "/span", ID: "ruler", Properties: map[string]any{"name": "ruler", "weight": qv("1", "m")},
	}); err != nil {
		t.Fatalf("put a span: %v", err)
	}
	for name, tc := range map[string]struct {
		cond substrate.Cond
		want []string
	}{
		"a mass beside a length":    {substrate.Cond{Eq: qv("1", "kg")}, []string{"grams", "kilograms"}},
		"a length beside a mass":    {substrate.Cond{Lt: qv("2", "m")}, []string{"ruler"}},
		"in, one of each measure":   {substrate.Cond{In: []any{qv("2", "kg"), qv("100", "cm")}}, []string{"heavier", "ruler"}},
		"a range in another's unit": {substrate.Cond{Gt: qv("1500", "g")}, []string{"heavier"}},
	} {
		got, err := list(tc.cond)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !slices.Equal(got, tc.want) {
			t.Fatalf("%s: filtered to %v, want %v", name, got, tc.want)
		}
	}
	if _, err := list(substrate.Cond{Eq: qv("1", "s")}); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("an operand no declared component holds: err = %v, want a validation refusal", err)
	}
	// A kind declaring the name as a state machine disagrees as a string does:
	// the state branch would compare the operand as text and match nothing.
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.KindManifest(quantityPackage, map[string]any{"singular": "kiln"}, map[string]any{
			"displayTemplate": "{name}",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"temp": map[string]any{
					"type": "state", "states": []any{"cold", "hot"}, "initial": "cold",
					"transitions": []any{map[string]any{"from": "cold", "to": "hot"}},
				},
			},
		}),
	}); err != nil {
		t.Fatalf("declare temp as a state on a second kind: %v", err)
	}
	if _, err := ds.List(ctx, substrate.Query{
		First:  50,
		Filter: substrate.Filter{Properties: map[string]substrate.Cond{"temp": {Eq: qv("37", "Cel")}}},
	}); !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "name the kinds to compare in filter.kinds") {
		t.Fatalf("a quantity operand over a quantity and a state: err = %v, want the refusal", err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.KindManifest(quantityPackage, map[string]any{"singular": "crate"}, map[string]any{
			"displayTemplate": "{weight}",
			"properties":      map[string]any{"weight": map[string]any{"type": "string"}},
		}),
	}); err != nil {
		t.Fatalf("declare weight as a string on a third kind: %v", err)
	}
	if _, err := list(substrate.Cond{Eq: qv("1", "kg")}); !errors.Is(err, substrate.ErrValidation) ||
		!strings.Contains(err.Error(), "name the kinds to compare in filter.kinds") {
		t.Fatalf("a quantity operand over disagreeing kinds: err = %v, want the refusal", err)
	}
}

// An absolute temperature compares after its offset: 37 Cel, 98.6 [degF] and
// 310.15 K are one temperature.
func TestFilterByTemperatureAppliesTheOffset(t *testing.T) {
	t.Parallel()
	ds := installReadings(t, map[string]map[string]any{
		"celsius":    {"temp": qv("37", "Cel")},
		"fahrenheit": {"temp": qv("98.6", "[degF]")},
		"kelvin":     {"temp": qv("310.15", "K")},
		"cooler":     {"temp": qv("36", "Cel")},
	})
	for _, tc := range []struct {
		name string
		cond substrate.Cond
		want []string
	}{
		{"eq in Celsius", substrate.Cond{Eq: qv("37", "Cel")}, []string{"celsius", "fahrenheit", "kelvin"}},
		{"lt in Fahrenheit", substrate.Cond{Lt: qv("98", "[degF]")}, []string{"cooler"}},
		{"gte in kelvin", substrate.Cond{Gte: qv("310", "K")}, []string{"celsius", "fahrenheit", "kelvin"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := readingNames(t, ds, substrate.Query{Filter: substrate.Filter{
				Properties: map[string]substrate.Cond{"temp": tc.cond},
			}})
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("filtered to %v, want %v", got, tc.want)
			}
		})
	}
}

// Units no conversion joins stay apart: the owner's beats per minute reach
// their beats per second through the owner's conversion, and never 1/min or
// steps per minute, which UCUM would call equal.
func TestFilterByAnnotatedRateKeepsTheAnnotation(t *testing.T) {
	t.Parallel()
	ds := installReadingsWith(t, func(ds *dataset) {
		putUnit(t, ds, "beats/min", map[string]any{"name": "beats per minute", "code": "{beats}/min"})
		putUnit(t, ds, "beats/s", map[string]any{"name": "beats per second", "code": "{beats}/s"})
		putUnit(t, ds, "steps/min", map[string]any{"name": "steps per minute", "code": "{steps}/min"})
		putConversion(t, ds, "beats/s-beats/min", "beats/s", "beats/min", map[string]any{"factor": "60"})
	}, map[string]map[string]any{
		"beats62":   {"pulse": qv("62", "{beats}/min")},
		"beats1hz":  {"pulse": qv("1", "{beats}/s")},
		"bare70":    {"pulse": qv("70", "/min")},
		"steps5":    {"pulse": qv("5", "{steps}/min")},
		"kilograms": {"pulse": qv("70", "kg")},
	})
	for _, tc := range []struct {
		name string
		cond substrate.Cond
		want []string
	}{
		{"gt in beats per minute", substrate.Cond{Gt: qv("61", "{beats}/min")}, []string{"beats62"}},
		{"lt in beats per second", substrate.Cond{Lt: qv("2", "{beats}/s")}, []string{"beats1hz", "beats62"}},
		{"bpm is the bare rate", substrate.Cond{Eq: qv("70", "bpm")}, []string{"bare70"}},
		{"beats per minute is its code", substrate.Cond{Eq: qv("60", "{beats}/min")}, []string{"beats1hz"}},
		{"a bare rate reaches the bare row alone", substrate.Cond{Gt: qv("0", "/min")}, []string{"bare70"}},
		{"another measure in an undeclared property", substrate.Cond{Gt: qv("0", "g")}, []string{"kilograms"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := readingNames(t, ds, substrate.Query{Filter: substrate.Filter{
				Properties: map[string]substrate.Cond{"pulse": tc.cond},
			}})
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("filtered to %v, want %v", got, tc.want)
			}
		})
	}
}

// A bounded value matches a comparison only where the comparison holds for
// every value the bound admits: <5 is below 5 and below 10, never below 3 and
// never equal to anything; <=5 is below 10 but not below 5.
func TestFilterByComparatorReadsTheHalfLine(t *testing.T) {
	t.Parallel()
	ds := installReadings(t, map[string]map[string]any{
		"lt5":    {"result": qv("5", "mg/dL", "<")},
		"lte5":   {"result": qv("5", "mg/dL", "<=")},
		"is7":    {"result": qv("7", "mg/dL")},
		"gt10":   {"result": qv("10", "mg/dL", ">")},
		"gte10":  {"result": qv("0.1", "g/L", ">=")},
		"is4gpl": {"result": qv("0.04", "g/L")},
	})
	for _, tc := range []struct {
		name string
		cond substrate.Cond
		want []string
	}{
		{"lt 5", substrate.Cond{Lt: qv("5", "mg/dL")}, []string{"is4gpl", "lt5"}},
		{"lt 10", substrate.Cond{Lt: qv("10", "mg/dL")}, []string{"is4gpl", "is7", "lt5", "lte5"}},
		{"lt 3", substrate.Cond{Lt: qv("3", "mg/dL")}, nil},
		{"lte 5", substrate.Cond{Lte: qv("5", "mg/dL")}, []string{"is4gpl", "lt5", "lte5"}},
		{"eq 5 matches no bound", substrate.Cond{Eq: qv("5", "mg/dL")}, nil},
		{"eq 7", substrate.Cond{Eq: qv("0.07", "g/L")}, []string{"is7"}},
		{"gt 10", substrate.Cond{Gt: qv("10", "mg/dL")}, []string{"gt10"}},
		{"gt 9", substrate.Cond{Gt: qv("9", "mg/dL")}, []string{"gt10", "gte10"}},
		{"gte 10", substrate.Cond{Gte: qv("10", "mg/dL")}, []string{"gt10", "gte10"}},
		{"gt 11", substrate.Cond{Gt: qv("11", "mg/dL")}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := readingNames(t, ds, substrate.Query{Filter: substrate.Filter{
				Properties: map[string]substrate.Cond{"result": tc.cond},
			}})
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("filtered to %v, want %v", got, tc.want)
			}
		})
	}
	_, err := ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{
		Kinds:      []string{readingKind},
		Properties: map[string]substrate.Cond{"result": {In: []any{qv("7", "mg/dL")}}},
	}})
	if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "bounded value") {
		t.Fatalf("in on a bounded property: err = %v, want it refused", err)
	}
}

// An order is resolved over the kinds the filter names: another kind that
// declares the same name differently (a string weight, a secret value, a text
// score) cannot turn a quantity order into a text sort or refuse it. With no
// kind named, the order spans every kind, and a disagreement keeps the text
// ordering it has always had.
func TestOrderIsResolvedOverTheReadsKinds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openReadingsDataset(t)
	labelKind := quantityPackage + "/label"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(quantityPackage, 0),
		readingManifest(map[string]any{
			"value": map[string]any{"type": "quantity", "unit": "kg"},
			"score": map[string]any{"type": "int"},
		}),
		vocabulary.KindManifest(quantityPackage, map[string]any{"singular": "label"}, map[string]any{
			"properties": map[string]any{
				"weight": map[string]any{"type": "string"},
				"score":  map[string]any{"type": "string"},
			},
		}),
	}); err != nil {
		t.Fatalf("install the kinds: %v", err)
	}
	for name, props := range map[string]map[string]any{
		"grams": {"weight": qv("500", "g"), "value": qv("500", "g"), "score": 9},
		"nine":  {"weight": qv("9", "kg"), "value": qv("9", "kg"), "score": 10},
		"ten":   {"weight": qv("10", "kg"), "value": qv("10", "kg"), "score": 100},
	} {
		props["name"] = name
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: readingKind, ID: name, Properties: props}); err != nil {
			t.Fatalf("put %s: %v", name, err)
		}
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: labelKind, ID: "tag", Properties: map[string]any{
		"weight": "heavy", "score": "high",
	}}); err != nil {
		t.Fatalf("put the label: %v", err)
	}
	for _, property := range []string{"weight", "value", "score"} {
		t.Run(property, func(t *testing.T) {
			got := readingNames(t, ds, substrate.Query{OrderBy: []substrate.Order{{Property: property}}})
			if want := "grams,nine,ten"; strings.Join(got, ",") != want {
				t.Fatalf("ordered by %s: %v, want %s", property, got, want)
			}
		})
	}
	// The readings' names in the order a read answers, whatever else it holds.
	ordered := func(q substrate.Query) []string {
		t.Helper()
		q.First = 50
		page, err := ds.List(ctx, q)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		var out []string
		for _, r := range page.Records {
			if r.Kind == readingKind {
				out = append(out, r.Properties["name"].(string))
			}
		}
		return out
	}
	// Two named kinds that disagree (an int score and a string one) keep the
	// text order: 10, 100, 9.
	if got := ordered(substrate.Query{
		OrderBy: []substrate.Order{{Property: "score"}},
		Filter:  substrate.Filter{Kinds: []string{readingKind, labelKind}},
	}); strings.Join(got, ",") != "nine,ten,grams" {
		t.Fatalf("across kinds that disagree, ordered %v, want the text order nine,ten,grams", got)
	}
	// A read that names no kind resolves the order over every kind, so the
	// label's string weight turns the quantity order into the text order of
	// the stored objects: unit g before kg, then value "10" before "9".
	if got := ordered(substrate.Query{OrderBy: []substrate.Order{{Property: "weight"}}}); strings.Join(got, ",") != "grams,ten,nine" {
		t.Fatalf("with no kind named, ordered by weight %v, want the text order grams,ten,nine", got)
	}
}

// A cursor carries a digest of the order it was minted under, not the order's
// text: a quantity order is a CASE over every unit of its measure, which a
// cursor in a URL would otherwise repeat for every page. The walk still
// visits every row once, and a cursor from another filter is still refused.
func TestOrderByQuantityMintsAShortCursor(t *testing.T) {
	t.Parallel()
	ds := installReadings(t, map[string]map[string]any{
		"a": {"weight": qv("70", "kg")}, "b": {"weight": qv("150", "[lb_av]")},
		"c": {"weight": qv("65000", "g")}, "d": {"weight": qv("11", "[stone_av]")},
	})
	ctx := context.Background()
	q := substrate.Query{Filter: substrate.Filter{Kinds: []string{readingKind}}, OrderBy: []substrate.Order{{Property: "weight"}}, First: 1}
	var names []string
	for range 5 {
		page, err := ds.List(ctx, q)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, r := range page.Records {
			names = append(names, r.Properties["name"].(string))
		}
		if page.Cursor == "" {
			break
		}
		if len(page.Cursor) > 300 {
			t.Fatalf("the cursor is %d characters, want a digest of the order, not its text", len(page.Cursor))
		}
		q.After = page.Cursor
	}
	if got := strings.Join(names, ","); got != "c,b,d,a" {
		t.Errorf("the walk by weight visited %s, want c,b,d,a", got)
	}
	first, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{readingKind}}, OrderBy: []substrate.Order{{Property: "weight"}}, First: 1})
	if err != nil {
		t.Fatal(err)
	}
	other := substrate.Query{
		Filter:  substrate.Filter{Kinds: []string{readingKind}, Properties: map[string]substrate.Cond{"name": {Eq: "a"}}},
		OrderBy: []substrate.Order{{Property: "weight"}}, After: first.Cursor,
	}
	if _, err := ds.List(ctx, other); !errors.Is(err, substrate.ErrValidation) {
		t.Errorf("a cursor replayed under another filter: %v, want a validation refusal", err)
	}
}

// An order key keeps the value at its component's root to forty decimal
// places, so two rates that differ at the twenty-second significant digit
// sort apart rather than tie and fall to the id.
func TestOrderByQuantityKeepsFortyPlaces(t *testing.T) {
	t.Parallel()
	anchored := map[string]any{"pulse": map[string]any{"type": "quantity", "unit": "1/min"}}
	ds := installReadingsDeclared(t, anchored, nil, map[string]map[string]any{
		"aa": {"pulse": qv("7200.000000000000001", "/h")},
		"bb": {"pulse": qv("2", "/s")},
	})
	for _, desc := range []bool{false, true} {
		want := "bb,aa"
		if desc {
			want = "aa,bb"
		}
		got := readingNames(t, ds, substrate.Query{OrderBy: []substrate.Order{{Property: "pulse", Desc: desc}}})
		if strings.Join(got, ",") != want {
			t.Fatalf("desc %v ordered %v, want %s", desc, got, want)
		}
	}
}

// An order across two kinds that read one unit two ways keys each kind's
// rows its own way, at the component's root (Cel, the smallest id): a 10 Cel
// temperature keys at 10, and a 20 K difference (`delta: true`) at 20, not at
// the -253.15 an absolute 20 K would key at, whichever kind the read names
// first. The 10 Cel difference ties the 10 Cel temperature and falls to the
// id.
func TestOrderByQuantityKeysEachKindInItsOwnMeasure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openReadingsDataset(t)
	shiftKind := quantityPackage + "/shift"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(quantityPackage, 0),
		readingManifest(map[string]any{"change": map[string]any{"type": "quantity", "unit": "K"}}),
		vocabulary.KindManifest(quantityPackage, map[string]any{"singular": "shift"}, map[string]any{
			"properties": map[string]any{
				"name":   map[string]any{"type": "string"},
				"change": map[string]any{"type": "quantity", "unit": "K", "delta": true},
			},
		}),
	}); err != nil {
		t.Fatalf("install the kinds: %v", err)
	}
	for _, row := range []struct {
		kind, name string
		change     map[string]any
	}{
		{readingKind, "absolute", qv("10", "Cel")},
		{shiftKind, "small", qv("10", "Cel")},
		{shiftKind, "large", qv("20", "K")},
	} {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: row.kind, ID: row.name, Properties: map[string]any{
			"name": row.name, "change": row.change,
		}}); err != nil {
			t.Fatalf("put %s: %v", row.name, err)
		}
	}
	for _, kinds := range [][]string{{readingKind, shiftKind}, {shiftKind, readingKind}} {
		page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: kinds}, OrderBy: []substrate.Order{{Property: "change"}}, First: 10})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		var got []string
		for _, r := range page.Records {
			got = append(got, r.Properties["name"].(string))
		}
		if strings.Join(got, ",") != "absolute,small,large" {
			t.Fatalf("kinds %v ordered %v, want absolute,small,large", kinds, got)
		}
	}
}

// An order sorts by the value at its component's root, so 155 pounds sits
// between 50 kg and 71000 g; one-row pages put a cursor at every boundary.
func TestOrderByQuantityReadsTheBase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installReadings(t, map[string]map[string]any{
		"light":  {"weight": qv("50", "kg")},
		"pounds": {"weight": qv("155", "[lb_av]")},
		"grams":  {"weight": qv("71000", "g")},
		"heavy":  {"weight": qv("0.1", "t")},
		"stone":  {"weight": qv("12", "[stone_av]")},
		"none":   {},
	})
	want := []string{"light", "pounds", "grams", "stone", "heavy", "none"}
	var got []string
	after := ""
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("walk did not terminate")
		}
		page, err := ds.List(ctx, substrate.Query{
			Filter:  substrate.Filter{Kinds: []string{readingKind}},
			OrderBy: []substrate.Order{{Property: "weight"}},
			First:   1,
			After:   after,
		})
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		for _, r := range page.Records {
			got = append(got, r.Properties["name"].(string))
		}
		if page.Cursor == "" {
			break
		}
		after = page.Cursor
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ordered %v, want %v", got, want)
	}
	no, yes := false, true
	if got := readingNames(t, ds, substrate.Query{Filter: substrate.Filter{
		Properties: map[string]substrate.Cond{"weight": {Exists: &no}},
	}}); strings.Join(got, ",") != "none" {
		t.Fatalf("exists: false = %v, want the one reading without a weight", got)
	}
	if got := readingNames(t, ds, substrate.Query{Filter: substrate.Filter{
		Properties: map[string]substrate.Cond{"weight": {Exists: &yes}},
	}}); len(got) != 5 {
		t.Fatalf("exists: true = %v, want the five weighed readings", got)
	}
}

// A declaration may not narrow a quantity over the rows that hold it: a unit
// dropped from the list, the anchor moved to another component, a bound
// raised, a comparator turned off. Each count is the write's own verdict
// over each row, through the unit graph.
func TestQuantityNarrowingsAreRefusedOverLiveRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installReadings(t, map[string]map[string]any{
		"kilos":  {"weight": qv("50", "kg"), "result": qv("5", "mg/dL", "<")},
		"pounds": {"weight": qv("155", "[lb_av]")},
	})
	apply := func(override map[string]any) error {
		_, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{readingManifest(override)})
		return err
	}
	for _, tc := range []struct {
		name     string
		override map[string]any
		want     string
	}{
		{
			"a unit dropped",
			map[string]any{"weight": map[string]any{"type": "quantity", "unit": "kg", "units": []any{"g"}}},
			"while 1 live records hold a value it refuses",
		},
		{
			"the anchor moved to another component",
			map[string]any{"weight": map[string]any{"type": "quantity", "unit": "m"}},
			"while 2 live records hold a value it refuses",
		},
		{
			"a min raised",
			map[string]any{"weight": map[string]any{
				"type": "quantity", "unit": "kg",
				"min": map[string]any{"value": "60", "unit": "kg"},
			}},
			"while 1 live records hold a value it refuses",
		},
		{
			"the comparator off",
			map[string]any{"result": map[string]any{"type": "quantity", "unit": "g/L"}},
			"declares no comparator while 1 live records hold a value it refuses",
		},
	} {
		if err := apply(tc.override); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
	for _, tc := range []struct {
		name     string
		override map[string]any
	}{
		{"the anchor's component with a unit listed", map[string]any{"weight": map[string]any{"type": "quantity", "unit": "kg", "units": []any{"lb"}}}},
		{"a min every row meets, in grams", map[string]any{"weight": map[string]any{
			"type": "quantity", "unit": "kg",
			"min": map[string]any{"value": "50000", "unit": "g"},
		}}},
		{"the anchor dropped", map[string]any{"weight": map[string]any{"type": "quantity"}}},
	} {
		if err := apply(tc.override); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
}

// Dropping `delta: true` narrows: a 0.3 Cel difference would be read as the
// absolute 0.3 Cel. Dropping an anchor narrows nothing, since a quantity
// with neither `unit` nor `units` admits every unit. The guard counts the
// rows that hold a difference, live and deleted, and only those.
func TestDroppingAQuantityDeltaIsANarrowing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	declared := map[string]any{
		"output": map[string]any{"type": "quantity", "unit": "W"},
		"drift":  map[string]any{"type": "quantity", "unit": "K", "delta": true},
	}
	ds := openReadingsDataset(t)
	if err := enginetest.PutShippedUnits(ctx, ds, "W"); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(quantityPackage, 0), readingManifest(declared),
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	for name, props := range map[string]map[string]any{
		"watts":  {"output": qv("5", "W")},
		"warmer": {"drift": qv("0.3", "Cel")},
	} {
		props["name"] = name
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: readingKind, ID: name, Properties: props}); err != nil {
			t.Fatalf("put %s: %v", name, err)
		}
	}
	undelta := readingManifest(map[string]any{"drift": map[string]any{"type": "quantity", "unit": "K"}, "output": declared["output"]})
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{undelta}); err == nil ||
		!strings.Contains(err.Error(), `property "drift"`) || !strings.Contains(err.Error(), "1 live records") {
		t.Fatalf("dropping delta over a live difference: err = %v", err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		readingManifest(map[string]any{"output": map[string]any{"type": "quantity"}, "drift": declared["drift"]}),
	}); err != nil {
		t.Fatalf("dropping the output's anchor: %v", err)
	}
	// Deleting the difference does not clear the way: a restore would read
	// it again. Clearing the value does.
	if _, err := ds.Delete(ctx, substrate.ActorAPI, readingKind, "warmer", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{undelta}); err == nil ||
		!strings.Contains(err.Error(), `property "drift"`) || !strings.Contains(err.Error(), "1 deleted records") {
		t.Fatalf("dropping delta over a deleted difference: err = %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: readingKind, ID: "warmer", Properties: map[string]any{"name": "warmer", "drift": nil}}); err != nil {
		t.Fatalf("restore and clear: %v", err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{undelta}); err != nil {
		t.Fatalf("dropping delta once no record holds a difference: %v", err)
	}
}

// The tombstone guard reads what a restore would keep, through changes that
// each look harmless alone. A property renamed with `renamedFrom` leaves its
// tombstones under the old name, which a restore moves to the new one, so
// dropping `delta` a step later is counted against the old name's values,
// for a property and for a reference's link property alike. A candidate that
// refuses a deleted value for a removable constraint (a `min`, a `units:`
// list, an anchor in another component) does not shelter it, because a
// later declaration that drops the constraint rereads it.
func TestQuantityDeltaGuardFollowsTheRestore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	drift := map[string]any{"type": "quantity", "unit": "K", "delta": true}
	bare := map[string]any{"type": "quantity", "unit": "K"}
	renamed := func(spec map[string]any) map[string]any {
		out := map[string]any{"renamedFrom": "drift"}
		for k, v := range spec {
			out[k] = v
		}
		return out
	}
	reference := func(name, from string, link map[string]any) map[string]any {
		ref := map[string]any{
			"type": "reference", "kind": readingKind, "repeated": true,
			"properties": map[string]any{"drift": link},
		}
		if from != "" {
			ref["renamedFrom"] = from
		}
		return map[string]any{name: ref}
	}
	apply := func(ds *dataset, props map[string]any) error {
		_, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{readingManifest(props)})
		return err
	}
	seed := func(t *testing.T) *dataset {
		t.Helper()
		ds := openReadingsDataset(t)
		props := reference("peer", "", drift)
		props["drift"] = drift
		if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
			vocabulary.PackageManifest(quantityPackage, 0), readingManifest(props),
		}); err != nil {
			t.Fatalf("install: %v", err)
		}
		for id, p := range map[string]map[string]any{
			"target": {},
			"warmer": {"drift": qv("0.3", "Cel")},
			"linked": {"peer": []any{map[string]any{vocabulary.ReferenceValueKey: "target", "drift": qv("0.3", "Cel")}}},
		} {
			p["name"] = id
			if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: readingKind, ID: id, Properties: p}); err != nil {
				t.Fatalf("put %s: %v", id, err)
			}
		}
		for _, id := range []string{"warmer", "linked"} {
			if _, err := ds.Delete(ctx, substrate.ActorAPI, readingKind, id, substrate.DeleteInput{}); err != nil {
				t.Fatalf("delete %s: %v", id, err)
			}
		}
		return ds
	}
	const refused = "1 deleted records"

	t.Run("a renamed property", func(t *testing.T) {
		ds := seed(t)
		props := reference("peer", "", drift)
		props["delta"] = renamed(drift)
		if err := apply(ds, props); err != nil {
			t.Fatalf("the rename: %v", err)
		}
		props["delta"] = renamed(bare)
		if err := apply(ds, props); err == nil || !strings.Contains(err.Error(), `property "delta"`) || !strings.Contains(err.Error(), refused) {
			t.Fatalf("dropping delta after the rename: err = %v", err)
		}
	})
	t.Run("a renamed reference", func(t *testing.T) {
		ds := seed(t)
		props := reference("peers", "peer", drift)
		props["drift"] = drift
		if err := apply(ds, props); err != nil {
			t.Fatalf("the rename: %v", err)
		}
		props = reference("peers", "peer", bare)
		props["drift"] = drift
		if err := apply(ds, props); err == nil || !strings.Contains(err.Error(), `link property "drift"`) || !strings.Contains(err.Error(), refused) {
			t.Fatalf("dropping the link's delta after the rename: err = %v", err)
		}
	})
	for name, spec := range map[string]map[string]any{
		"a min":                           {"type": "quantity", "unit": "K", "min": map[string]any{"value": "1", "unit": "Cel"}},
		"a units list":                    {"type": "quantity", "unit": "K", "units": []any{"Cel"}},
		"an anchor the value is apart of": {"type": "quantity", "unit": "kg"},
	} {
		t.Run(name, func(t *testing.T) {
			ds := seed(t)
			props := reference("peer", "", drift)
			props["drift"] = spec
			if err := apply(ds, props); err == nil || !strings.Contains(err.Error(), `property "drift"`) || !strings.Contains(err.Error(), refused) {
				t.Fatalf("dropping delta with %s: err = %v", name, err)
			}
			props = reference("peer", "", spec)
			props["drift"] = drift
			if err := apply(ds, props); err == nil || !strings.Contains(err.Error(), `link property "drift"`) || !strings.Contains(err.Error(), refused) {
				t.Fatalf("dropping the link's delta with %s: err = %v", name, err)
			}
		})
	}
}

// A restoring put keeps every value the declaration in force admits, so a
// `delta` flip that rereads a deleted record's value is refused as it is over
// a live one: 0.3 Cel stored as a difference (0.3 K) would otherwise come
// back as an absolute temperature (273.45 K). The same holds for a link
// property, read out of the tombstone's stored reference.
func TestQuantityDeltaChangeCountsDeletedRecords(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	drift := map[string]any{"type": "quantity", "unit": "K", "delta": true}
	declare := func(field, link map[string]any) map[string]any {
		return readingManifest(map[string]any{
			"drift": field,
			"peer": map[string]any{
				"type": "reference", "kind": readingKind, "repeated": true,
				"properties": map[string]any{"drift": link},
			},
		})
	}
	ds := openReadingsDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(quantityPackage, 0), declare(drift, drift),
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	put := func(id string, props map[string]any) {
		t.Helper()
		props["name"] = id
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: readingKind, ID: id, Properties: props}); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
	}
	del := func(id string) {
		t.Helper()
		if _, err := ds.Delete(ctx, substrate.ActorAPI, readingKind, id, substrate.DeleteInput{}); err != nil {
			t.Fatalf("delete %s: %v", id, err)
		}
	}
	put("target", map[string]any{})
	put("warmer", map[string]any{"drift": qv("0.3", "Cel")})
	put("linked", map[string]any{"peer": []any{map[string]any{vocabulary.ReferenceValueKey: "target", "drift": qv("0.3", "Cel")}}})
	del("warmer")
	del("linked")
	bare := map[string]any{"type": "quantity", "unit": "K"}
	for name, tc := range map[string]struct {
		doc  map[string]any
		want string
	}{
		"a property":      {declare(bare, drift), `property "drift"`},
		"a link property": {declare(drift, bare), `link property "drift"`},
	} {
		_, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{tc.doc})
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "1 deleted records") {
			t.Fatalf("%s: err = %v, want a refusal naming %s and the deleted record", name, err, tc.want)
		}
	}
	// The remedy the refusal names: restore, clear the value, delete again.
	put("warmer", map[string]any{"drift": nil})
	put("linked", map[string]any{"peer": nil})
	del("warmer")
	del("linked")
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{declare(bare, bare)}); err != nil {
		t.Fatalf("dropping delta once no record holds a difference: %v", err)
	}
}

// A declared default is a quantity the write path admits, stored on a create
// that leaves the property out; one no write could store is refused.
func TestQuantityDefaultFillsACreate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openReadingsDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(quantityPackage, 0),
		readingManifest(map[string]any{"weight": map[string]any{
			"type": "quantity", "unit": "kg",
			"default": map[string]any{"value": "0", "unit": "kg"},
		}}),
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	rec, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: readingKind, Properties: map[string]any{"name": "empty"}})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if s := vocabulary.FormatQuantityIn(nil, rec.Properties["weight"]); s != "0 kg" {
		t.Fatalf("weight = %#v, want the default 0 kg", rec.Properties["weight"])
	}
	_, err = ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		readingManifest(map[string]any{"weight": map[string]any{
			"type": "quantity", "unit": "kg",
			"default": map[string]any{"value": "1", "unit": "m"},
		}}),
	})
	if err == nil || !strings.Contains(err.Error(), "m does not convert to kg") {
		t.Fatalf("err = %v, want a default no write could store refused", err)
	}
}

// A property type refines a quantity through the apply door: core's
// propertytype declaration admits `quantity` as a base and its four keys,
// and a property using the refinement holds values to its units.
func TestAQuantityPropertyTypeAppliesAndHoldsItsMeasure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openReadingsDataset(t)
	authority, pkg := vocabulary.SplitPackageRef(quantityPackage)
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(quantityPackage, 0),
		{
			"kind":     vocabulary.CoreKind(vocabulary.DocPropertyType),
			"metadata": map[string]any{"id": quantityPackage + "/airtemperature"},
			"data": map[string]any{
				"authority": authority, "package": pkg, "base": "quantity",
				"unit": "Cel", "units": []any{"degF"}, "comparator": true,
				"description": "an air temperature",
			},
		},
		vocabulary.KindManifest(quantityPackage, map[string]any{"singular": "station"}, map[string]any{
			"displayTemplate": "{name}: {air}",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"air":  map[string]any{"type": "airtemperature"},
			},
		}),
	}); err != nil {
		t.Fatalf("apply the refinement: %v", err)
	}
	rec, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: quantityPackage + "/station", Properties: map[string]any{
		"name": "roof", "air": qv("-3.5", "°C", ">"),
	}})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if rec.Title != "roof: >-3.5 °C" {
		t.Fatalf("title = %q", rec.Title)
	}
	_, err = ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: quantityPackage + "/station", Properties: map[string]any{
		"name": "roof", "air": qv("270", "K"),
	}})
	if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "not one of the declared units") {
		t.Fatalf("a unit outside the refinement's list: err = %v", err)
	}
}

// Editing only a quantity propertytype changes what every kind using it
// resolves, so the edit is held in each of them as a change of the kind's
// own declaration is: an unknown unit and a listed unit the anchor cannot reach
// are refused though the kind holds no record and its own document is
// unchanged, and a unit the repository declares is admitted.
func TestAQuantityPropertyTypeEditIsHeldInEveryKindThatUsesIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openReadingsDataset(t)
	authority, pkg := vocabulary.SplitPackageRef(quantityPackage)
	refinement := func(extra map[string]any) map[string]any {
		data := map[string]any{"authority": authority, "package": pkg, "base": "quantity", "description": "a load"}
		for k, v := range extra {
			data[k] = v
		}
		return map[string]any{
			"kind":     vocabulary.CoreKind(vocabulary.DocPropertyType),
			"metadata": map[string]any{"id": quantityPackage + "/load"},
			"data":     data,
		}
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(quantityPackage, 0),
		refinement(map[string]any{"unit": "kg"}),
		vocabulary.KindManifest(quantityPackage, map[string]any{"singular": "crate"}, map[string]any{
			"properties": map[string]any{"load": map[string]any{"type": "load"}},
		}),
	}); err != nil {
		t.Fatalf("apply the refinement and its kind: %v", err)
	}
	for _, c := range []struct {
		what  string
		extra map[string]any
		want  string
	}{
		{"an unknown unit", map[string]any{"unit": "nonesuch"}, `crate data.properties.load.unit: "nonesuch" names no unit`},
		{"a listed unit the anchor cannot reach", map[string]any{"unit": "kg", "units": []any{"K"}}, "crate data.properties.load.units: the listed unit K does not convert"},
	} {
		_, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{refinement(c.extra)})
		if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s through the propertytype alone: err = %v, want a refusal naming %q", c.what, err, c.want)
		}
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{refinement(map[string]any{"unit": "g"})}); err != nil {
		t.Fatalf("anchor the propertytype to g: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: quantityPackage + "/crate", Properties: map[string]any{
		"load": qv("2", "kg"),
	}}); err != nil {
		t.Fatalf("a crate in kg under the g anchor: %v", err)
	}
}

// A quantity orders by its value at its component's root in every unit of
// the component, the owner's own included: 1 Mg is 1000 kg, between 500 kg
// and 2000 kg, though the shipped units hold no Mg, and no accepted number
// keys NULL. A row in a unit no row used before, written between two pages,
// leaves the walk's cursor valid and lands where its value puts it.
func TestOrderByQuantityKeysEveryAdmittedUnit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installReadingsWith(t, func(ds *dataset) {
		putUnit(t, ds, "Mg", map[string]any{"name": "megagram", "code": "Mg"})
		putUnit(t, ds, "hg", map[string]any{"name": "hectogram", "code": "hg"})
		putConversion(t, ds, "Mg-kg", "Mg", "kg", map[string]any{"factor": "1000"})
		putConversion(t, ds, "hg-kg", "hg", "kg", map[string]any{"factor": "0.1"})
	}, map[string]map[string]any{
		"light":    {"weight": qv("500", "kg")},
		"megagram": {"weight": qv("1", "Mg")},
		"heavy":    {"weight": qv("2000", "kg")},
	})
	var got []string
	after := ""
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("walk did not terminate")
		}
		page, err := ds.List(ctx, substrate.Query{
			Filter:  substrate.Filter{Kinds: []string{readingKind}},
			OrderBy: []substrate.Order{{Property: "weight"}},
			First:   1,
			After:   after,
		})
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		for _, r := range page.Records {
			got = append(got, r.Properties["name"].(string))
		}
		if pages == 0 {
			if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: readingKind, ID: "hecto", Properties: map[string]any{
				"name": "hecto", "weight": qv("30000", "hg"),
			}}); err != nil {
				t.Fatalf("put: %v", err)
			}
		}
		if page.Cursor == "" {
			break
		}
		after = page.Cursor
	}
	if want := "light,megagram,heavy,hecto"; strings.Join(got, ",") != want {
		t.Fatalf("ordered %v, want %s", got, want)
	}
	page, err := ds.List(ctx, substrate.Query{
		Filter:  substrate.Filter{Kinds: []string{readingKind}},
		OrderBy: []substrate.Order{{Property: "weight", Desc: true}},
		First:   1,
	})
	if err != nil || len(page.Records) != 1 || page.Records[0].Properties["name"] != "hecto" {
		t.Fatalf("the heaviest first is %v (%v), want hecto", page, err)
	}
}

// The spellings one filter compares in are bounded: past maxQuantityUnits
// used spellings of the operand's linear component the read is refused,
// rather than growing its SQL by one arm a spelling.
func TestQuantityFilterRefusesUnboundedUnitSpellings(t *testing.T) {
	t.Parallel()
	ds := installReadings(t, nil)
	if err := ds.inTx(context.Background(), substrate.ActorAPI, false, func(tx *txn) error {
		for i := range maxQuantityUnits + 1 {
			id := fmt.Sprintf("g%d", i+2)
			if _, err := tx.put(substrate.PutInput{Kind: vocabulary.KindUnit, ID: id, Properties: map[string]any{"name": id}}); err != nil {
				return err
			}
			if _, err := tx.put(substrate.PutInput{Kind: vocabulary.KindConversion, ID: id + "-g", Properties: conversionProps(id, "g", map[string]any{"factor": fmt.Sprint(i + 2)})}); err != nil {
				return err
			}
			if _, err := tx.put(substrate.PutInput{Kind: readingKind, ID: fmt.Sprintf("r%03d", i), Properties: map[string]any{
				"name": fmt.Sprintf("r%03d", i), "weight": qv("1", id),
			}}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("write the spellings: %v", err)
	}
	_, err := ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{
		Kinds:      []string{readingKind},
		Properties: map[string]substrate.Cond{"weight": {Lt: qv("1", "kg")}},
	}})
	if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "more than 256 spellings of one linear component") {
		t.Fatalf("err = %v, want a refusal naming the bound", err)
	}
}

// A spelling no unit answers is a validation error on every engine door, the
// write and the vocabulary apply, and never a panic: each case is a code the
// table's grammar once crashed or misread on.
func TestQuantityRefusesHostileUnits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installReadings(t, nil)
	for _, code := range []string{
		"m/0", "/0", "(m/0)", "m/(2.0)", "m9", "m5.m5", "10*1000000000", "1234567890123.m",
		strings.Repeat("(", 9) + "m" + strings.Repeat(")", 9), strings.Repeat("2.", 32) + "m",
	} {
		_, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: readingKind, Properties: map[string]any{
			"name": "hostile", "pulse": qv("1", code),
		}})
		if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "names no unit of this repository") {
			t.Fatalf("a write in %s: err = %v, want a validation error naming no unit", code, err)
		}
		_, err = ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{readingManifest(map[string]any{
			"pulse": map[string]any{"type": "quantity", "units": []any{code}},
		})})
		if err == nil || !strings.Contains(err.Error(), "names no unit of this repository") {
			t.Fatalf("a declaration listing %s: err = %v, want a refusal naming no unit", code, err)
		}
	}
}
