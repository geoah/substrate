package engine

// The quantity datatype against Postgres: what a write stores and a read
// returns, the title a template renders from it, the filter that converts its
// operand exactly into every stored unit of the operand's measure, the
// comparator's half-line, the order by the value at its measure's base and the
// index that holds that order, and the narrowings a declaration change may not
// make over live rows.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

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
		"weight": map[string]any{"type": "quantity", "measures": "mass"},
		"temp":   map[string]any{"type": "quantity", "measures": "temperature"},
		"result": map[string]any{"type": "quantity", "measures": "mass-concentration", "comparator": true},
		"pulse":  map[string]any{"type": "quantity"},
		"height": map[string]any{"type": "quantity", "measures": "length", "fts": true},
	}
	for k, v := range overrides {
		props[k] = v
	}
	return vocabulary.KindManifest(quantityPackage,
		map[string]any{"singular": "reading"},
		map[string]any{
			"displayTemplate": "{name}: {weight}",
			"properties":      props,
			"indices":         []any{map[string]any{"properties": []any{"weight"}}},
		})
}

func installReadings(t *testing.T, rows map[string]map[string]any) *dataset {
	t.Helper()
	ctx := context.Background()
	ds := openInternalDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(quantityPackage, 0),
		readingManifest(nil),
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
		"weight": qv("72.50", "kg"), "temp": qv("98.6", "[degF]"), "pulse": qv("62", "/min"),
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
		{"another measure", substrate.Cond{Lt: qv("1", "m")}, "not mass"},
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

// A level's measure closes its unit set without a `units:` list: its listed
// codes are the set, and a filter compares in them. `[pH]` under `measures:
// acidity` matches itself and a range around it.
func TestFilterByALevelWithoutUnits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(quantityPackage, 0),
		readingManifest(map[string]any{"ph": map[string]any{"type": "quantity", "measures": "acidity"}}),
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
	// A second kind declaring the name as a quantity of another measure
	// takes no part in a comparison its measure does not hold: it neither
	// refuses the operand nor matches. An operand no kind's measure holds is
	// refused.
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.KindManifest(quantityPackage, map[string]any{"singular": "span"}, map[string]any{
			"displayTemplate": "{name}",
			"properties": map[string]any{
				"name":   map[string]any{"type": "string"},
				"weight": map[string]any{"type": "quantity", "measures": "length"},
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
		t.Fatalf("an operand no declared measure holds: err = %v, want a validation refusal", err)
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

// An annotation is part of a unit: an operand in {beats}/min reaches stored
// {beats}/s and never /min or {steps}/min, which UCUM would call equal.
func TestFilterByAnnotatedRateKeepsTheAnnotation(t *testing.T) {
	t.Parallel()
	ds := installReadings(t, map[string]map[string]any{
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
	ds := openInternalDataset(t)
	labelKind := quantityPackage + "/label"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(quantityPackage, 0),
		readingManifest(map[string]any{
			"value": map[string]any{"type": "quantity", "measures": "mass"},
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

// An order key keeps the base value to forty decimal places, so two rates
// that differ at the twenty-second significant digit sort apart rather than
// tie and fall to the id.
func TestOrderByQuantityKeepsFortyPlaces(t *testing.T) {
	t.Parallel()
	ds := installReadings(t, map[string]map[string]any{
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

// An order across two kinds that read one code in two measures keys each
// kind's rows in its own: a 10 Cel temperature is 283.15 K, a 10 Cel
// difference is 10 K, whichever kind the read names first.
func TestOrderByQuantityKeysEachKindInItsOwnMeasure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t)
	shiftKind := quantityPackage + "/shift"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(quantityPackage, 0),
		readingManifest(map[string]any{"change": map[string]any{"type": "quantity", "measures": "temperature"}}),
		vocabulary.KindManifest(quantityPackage, map[string]any{"singular": "shift"}, map[string]any{
			"properties": map[string]any{
				"name":   map[string]any{"type": "string"},
				"change": map[string]any{"type": "quantity", "measures": "temperature-difference"},
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
		if strings.Join(got, ",") != "small,large,absolute" {
			t.Fatalf("kinds %v ordered %v, want small,large,absolute", kinds, got)
		}
	}
}

// An order sorts by the value at its measure's base, so 155 pounds sits
// between 50 kg and 71000 g; one-row pages put a cursor at every boundary.
// The declared index on the property holds the same expression, and Postgres
// built it, which an expression that was not immutable would have failed.
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
	var def string
	if err := ds.db.QueryRowContext(ctx, `SELECT indexdef FROM pg_indexes WHERE indexname = $1`,
		"idx_"+derivedID(readingKind, "0")).Scan(&def); err != nil {
		t.Fatalf("the declared weight index: %v", err)
	}
	if !strings.Contains(def, "regexp_replace") || !strings.Contains(def, "CASE") {
		t.Fatalf("the weight index is not the base-value expression: %s", def)
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
// dropped from the list, a measure changed, a bound raised, a comparator
// turned off. Each count is the write coercion's own verdict over each row.
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
			map[string]any{"weight": map[string]any{"type": "quantity", "measures": "mass", "units": []any{"kg"}}},
			"declares units kg while 1 live records hold a value it refuses",
		},
		{
			"a measure changed",
			map[string]any{"weight": map[string]any{"type": "quantity", "measures": "length"}},
			"declares measures length while 2 live records hold a value it refuses",
		},
		{
			"a min raised",
			map[string]any{"weight": map[string]any{
				"type": "quantity", "measures": "mass",
				"min": map[string]any{"value": "60", "unit": "kg"},
			}},
			"declares min 60 kg while 1 live records hold a value it refuses",
		},
		{
			"the comparator off",
			map[string]any{"result": map[string]any{"type": "quantity", "measures": "mass-concentration"}},
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
		{"both units listed", map[string]any{"weight": map[string]any{"type": "quantity", "measures": "mass", "units": []any{"kg", "[lb_av]"}}}},
		{"a min every row meets, in grams", map[string]any{"weight": map[string]any{
			"type": "quantity", "measures": "mass",
			"min": map[string]any{"value": "50000", "unit": "g"},
		}}},
		{"the measure dropped for a unit list", map[string]any{"weight": map[string]any{"type": "quantity", "units": []any{"kg", "[lb_av]"}}}},
	} {
		if err := apply(tc.override); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
	// The index on weight orders by the base, which a property naming neither
	// a measure nor its units leaves unknown.
	err := apply(map[string]any{"weight": map[string]any{"type": "quantity"}})
	if err == nil || !strings.Contains(err.Error(), "declares neither measures nor units") {
		t.Fatalf("an indexed quantity of no measure: err = %v, want it refused", err)
	}
}

// Dropping `measures:` narrows too: a code the declared measure admitted may
// be ambiguous undeclared (J/s is a power or an apparent power), and a
// temperature stored as a difference would be read as an absolute one. The
// guard counts those rows and only those.
func TestDroppingAQuantityMeasureIsANarrowing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	declared := map[string]any{
		"output": map[string]any{"type": "quantity", "measures": "power"},
		"drift":  map[string]any{"type": "quantity", "measures": "temperature-difference"},
	}
	ds := openInternalDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(quantityPackage, 0), readingManifest(declared),
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	for name, props := range map[string]map[string]any{
		"joules": {"output": qv("5", "J/s")},
		"watts":  {"output": qv("5", "W")},
		"warmer": {"drift": qv("0.3", "Cel")},
	} {
		props["name"] = name
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: readingKind, ID: name, Properties: props}); err != nil {
			t.Fatalf("put %s: %v", name, err)
		}
	}
	for name, tc := range map[string]struct {
		override map[string]any
		want     string
	}{
		"an ambiguous code": {
			map[string]any{"output": map[string]any{"type": "quantity"}, "drift": declared["drift"]},
			"declares no measures while 1 live records hold a value it refuses",
		},
		"a difference read as a temperature": {
			map[string]any{"drift": map[string]any{"type": "quantity"}, "output": declared["output"]},
			"declares no measures while 1 live records hold a value it reads in another measure",
		},
	} {
		_, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{readingManifest(tc.override)})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
	// Deleting the J/s record does not clear the way: a restore under the
	// measureless declaration drops the ambiguous code, but a later
	// declaration naming another measure would read it again. Clearing the
	// value does.
	if _, err := ds.Delete(ctx, substrate.ActorAPI, readingKind, "joules", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	dropped := readingManifest(map[string]any{"output": map[string]any{"type": "quantity"}, "drift": declared["drift"]})
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{dropped}); err == nil ||
		!strings.Contains(err.Error(), "declares no measures while 1 deleted records hold a value written in another measure") {
		t.Fatalf("dropping the measure over a deleted J/s value: err = %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: readingKind, ID: "joules", Properties: map[string]any{"name": "joules", "output": nil}}); err != nil {
		t.Fatalf("restore and clear: %v", err)
	}
	if _, err := ds.Delete(ctx, substrate.ActorAPI, readingKind, "joules", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete again: %v", err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{dropped}); err != nil {
		t.Fatalf("dropping the measure with only W rows left: %v", err)
	}
}

// The tombstone guard reads what a restore would keep, through two changes
// that each looked harmless before. A property renamed with `renamedFrom`
// leaves its tombstones under the old name, which a restore moves to the new
// one, so dropping the measure a step later is counted against the old
// name's values. A candidate that refuses a deleted value for a removable
// constraint (a `min`, a `units:` list) or for another measure does not
// shelter it, because a later declaration that drops the constraint
// rereads it with no measure change left to guard.
func TestQuantityMeasureGuardFollowsTheRestore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	drift := map[string]any{"type": "quantity", "measures": "temperature-difference"}
	bare := map[string]any{"type": "quantity"}
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
		ds := openInternalDataset(t)
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
	const refused = "deleted records hold a value written in another measure"

	t.Run("a renamed property", func(t *testing.T) {
		ds := seed(t)
		props := reference("peer", "", drift)
		props["delta"] = renamed(drift)
		if err := apply(ds, props); err != nil {
			t.Fatalf("the rename: %v", err)
		}
		props["delta"] = renamed(bare)
		if err := apply(ds, props); err == nil || !strings.Contains(err.Error(), `property "delta" declares no measures while 1 `+refused) {
			t.Fatalf("dropping the measure after the rename: err = %v", err)
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
		if err := apply(ds, props); err == nil || !strings.Contains(err.Error(), `link property "drift" declares no measures while 1 `+refused) {
			t.Fatalf("dropping the link measure after the rename: err = %v", err)
		}
	})
	for name, spec := range map[string]map[string]any{
		"a min":                         {"type": "quantity", "measures": "temperature", "min": map[string]any{"value": "1", "unit": "Cel"}},
		"a units list":                  {"type": "quantity", "measures": "temperature", "units": []any{"K"}},
		"a measure the value is not in": {"type": "quantity", "measures": "mass"},
	} {
		t.Run(name, func(t *testing.T) {
			ds := seed(t)
			props := reference("peer", "", drift)
			props["drift"] = spec
			if err := apply(ds, props); err == nil || !strings.Contains(err.Error(), `property "drift" declares measures `) || !strings.Contains(err.Error(), refused) {
				t.Fatalf("a measure change with %s: err = %v", name, err)
			}
			props = reference("peer", "", spec)
			props["drift"] = drift
			if err := apply(ds, props); err == nil || !strings.Contains(err.Error(), `link property "drift" declares measures `) || !strings.Contains(err.Error(), refused) {
				t.Fatalf("a link measure change with %s: err = %v", name, err)
			}
		})
	}
}

// A restoring put keeps every value the declaration in force admits, so a
// measure change that rereads a deleted record's value is refused as it is
// over a live one: 0.3 Cel stored as a temperature difference (0.3 K) would
// otherwise come back as an absolute temperature (273.45 K). The same holds
// for a link property, read out of the tombstone's stored reference.
func TestQuantityMeasureChangeCountsDeletedRecords(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	drift := map[string]any{"type": "quantity", "measures": "temperature-difference"}
	declare := func(field, link map[string]any) map[string]any {
		return readingManifest(map[string]any{
			"drift": field,
			"peer": map[string]any{
				"type": "reference", "kind": readingKind, "repeated": true,
				"properties": map[string]any{"drift": link},
			},
		})
	}
	ds := openInternalDataset(t)
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
	bare := map[string]any{"type": "quantity"}
	for name, tc := range map[string]struct {
		doc  map[string]any
		want string
	}{
		"a property": {
			declare(bare, drift),
			`property "drift" declares no measures while 1 deleted records hold a value written in another measure`,
		},
		"a link property": {
			declare(drift, bare),
			`link property "drift" declares no measures while 1 deleted records hold a value written in another measure`,
		},
	} {
		_, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{tc.doc})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
	// The remedy the refusal names: restore, clear the value, delete again.
	put("warmer", map[string]any{"drift": nil})
	put("linked", map[string]any{"peer": nil})
	del("warmer")
	del("linked")
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{declare(bare, bare)}); err != nil {
		t.Fatalf("dropping the measure once no record holds a difference: %v", err)
	}
}

// A declared default is a quantity the write path admits, stored on a create
// that leaves the property out; one no write could store is refused.
func TestQuantityDefaultFillsACreate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(quantityPackage, 0),
		readingManifest(map[string]any{"weight": map[string]any{
			"type": "quantity", "measures": "mass",
			"default": map[string]any{"value": "0", "unit": "kg"},
		}}),
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	rec, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: readingKind, Properties: map[string]any{"name": "empty"}})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if s := vocabulary.FormatQuantity(rec.Properties["weight"]); s != "0 kg" {
		t.Fatalf("weight = %#v, want the default 0 kg", rec.Properties["weight"])
	}
	_, err = ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		readingManifest(map[string]any{"weight": map[string]any{
			"type": "quantity", "measures": "mass",
			"default": map[string]any{"value": "1", "unit": "m"},
		}}),
	})
	if err == nil || !strings.Contains(err.Error(), "not mass") {
		t.Fatalf("err = %v, want a default no write could store refused", err)
	}
}

// A property type refines a quantity through the apply door: core's
// propertytype declaration admits `quantity` as a base and its three keys,
// and a property using the refinement holds values to its measure.
func TestAQuantityPropertyTypeAppliesAndHoldsItsMeasure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t)
	authority, pkg := vocabulary.SplitPackageRef(quantityPackage)
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(quantityPackage, 0),
		{
			"kind":     vocabulary.CoreKind(vocabulary.DocPropertyType),
			"metadata": map[string]any{"id": quantityPackage + "/airtemperature"},
			"data": map[string]any{
				"authority": authority, "package": pkg, "base": "quantity",
				"measures": "temperature", "units": []any{"Cel", "[degF]"}, "comparator": true,
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

// A quantity orders by its value at the measure's base in whatever unit the
// grammar admitted it: 1 Mg is 1000 kg, between 500 kg and 2000 kg, though
// the mass table lists no Mg, and no accepted number keys NULL. A unit first
// written between two pages leaves the walk's cursor valid and lands where
// its value puts it.
func TestOrderByQuantityKeysEveryAdmittedUnit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installReadings(t, map[string]map[string]any{
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

// countingDB counts the statements a read sends through it.
type countingDB struct {
	dbx
	queries []string
}

func (c *countingDB) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	c.queries = append(c.queries, q)
	return c.dbx.QueryContext(ctx, q, args...)
}

// A quantity filter converts its operand into every unit the rows it reads
// may hold. Where the declaration lists `units:` that set is closed and no
// row is read for it; otherwise one read collects the units of the filtered
// kinds' rows on the side the read selects, live or tombstoned, and never
// another kind's rows that happen to share the property's name.
func TestQuantityFilterReadsOnlyTheUnitsItNeeds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installReadings(t, map[string]map[string]any{
		"decagram": {"weight": qv("5", "dag")},
		"megagram": {"weight": qv("1", "Mg")},
	})
	if _, err := ds.Delete(ctx, substrate.ActorAPI, readingKind, "megagram", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	scale := quantityPackage + "/scale"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.KindManifest(quantityPackage, map[string]any{"singular": "scale"}, map[string]any{
			"properties": map[string]any{
				"weight": map[string]any{"type": "quantity", "measures": "mass"},
				"grams":  map[string]any{"type": "quantity", "measures": "mass", "units": []any{"g", "kg"}},
			},
		}),
	}); err != nil {
		t.Fatalf("declare the scale: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: scale, ID: "s1", Properties: map[string]any{
		"weight": qv("7", "cg"), "grams": qv("3", "g"),
	}}); err != nil {
		t.Fatalf("put the scale: %v", err)
	}
	kind := func(ident string) *vocabulary.Kind {
		k, err := ds.registry().Resolve(ident)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	read := func(tombstones bool, decls []quantityDecl, key string) ([]string, int) {
		t.Helper()
		db := &countingDB{dbx: tx}
		units, err := ds.storedQuantityUnits(ctx, db, &builder{overRecords: true, tombstones: tombstones}, decls, key)
		if err != nil {
			t.Fatalf("read the units: %v", err)
		}
		for _, q := range db.queries {
			if !strings.Contains(q, "kind = ANY(") || !strings.Contains(q, "deleted_at IS") {
				t.Errorf("the units read %q does not carry the kind filter and the tombstone side", q)
			}
		}
		return units, len(db.queries)
	}
	readings := quantityDecls([]*vocabulary.Kind{kind(readingKind)}, "weight")
	live, n := read(false, readings, "weight")
	if n != 1 || !slices.Contains(live, "dag") || slices.Contains(live, "Mg") || slices.Contains(live, "cg") {
		t.Fatalf("a live read collected %v in %d reads, want dag and neither the tombstone's Mg nor the scale's cg", live, n)
	}
	if dead, _ := read(true, readings, "weight"); !slices.Contains(dead, "Mg") || slices.Contains(dead, "dag") {
		t.Fatalf("a read of tombstones collected %v, want the tombstone's Mg and not the live dag", dead)
	}
	closed, n := read(false, quantityDecls([]*vocabulary.Kind{kind(scale)}, "grams"), "grams")
	if n != 0 || strings.Join(closed, ",") != "g,kg" {
		t.Fatalf("a declaration listing its units read the rows %d times and collected %v, want none and g,kg", n, closed)
	}
	// The filter itself still finds the decagram by an operand in kilograms.
	if got := readingNames(t, ds, substrate.Query{Filter: substrate.Filter{
		Properties: map[string]substrate.Cond{"weight": {Lt: qv("1", "kg")}},
	}}); strings.Join(got, ",") != "decagram" {
		t.Fatalf("weight below 1 kg = %v, want the decagram", got)
	}
}

// The units one read collects are bounded: past maxQuantityUnits spellings in
// the rows a filter covers, the read is refused and names the declaration
// that would close the set, rather than growing its SQL by one arm a spelling.
func TestQuantityFilterRefusesUnboundedUnitSpellings(t *testing.T) {
	t.Parallel()
	rows := map[string]map[string]any{}
	for i := range maxQuantityUnits + 1 {
		rows[fmt.Sprintf("r%03d", i)] = map[string]any{"weight": qv("1", fmt.Sprintf("%d.g", i+2))}
	}
	ds := installReadings(t, rows)
	_, err := ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{
		Kinds:      []string{readingKind},
		Properties: map[string]substrate.Cond{"weight": {Lt: qv("1", "kg")}},
	}})
	if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "more than 256 distinct units") ||
		!strings.Contains(err.Error(), "declare `units:`") {
		t.Fatalf("err = %v, want a refusal naming the bound and `units:`", err)
	}
}

// The read of the rows' units carries the rest of the request's filter: 257
// spellings on rows the filter leaves out do not count against the bound, for
// a filter on the quantity (whose condition sorts before the one that
// narrows) and for an order by it alike.
func TestQuantityUnitsReadUnderTheWholeFilter(t *testing.T) {
	t.Parallel()
	rows := map[string]map[string]any{
		"a": {"height": qv("1", "m")},
		"b": {"height": qv("2000", "mm")},
	}
	for i := range maxQuantityUnits + 1 {
		rows[fmt.Sprintf("noise%03d", i)] = map[string]any{"height": qv("1", fmt.Sprintf("%d.m", i+2))}
	}
	ds := installReadings(t, rows)
	narrow := map[string]substrate.Cond{"name": {In: []any{"a", "b"}}}
	filtered := map[string]substrate.Cond{"name": narrow["name"], "height": {Lt: qv("3", "m")}}
	if got := readingNames(t, ds, substrate.Query{Filter: substrate.Filter{Properties: filtered}}); strings.Join(got, ",") != "a,b" {
		t.Errorf("height below 3 m among a and b = %v, want a,b", got)
	}
	if got := readingNames(t, ds, substrate.Query{
		Filter:  substrate.Filter{Properties: narrow},
		OrderBy: []substrate.Order{{Property: "height", Desc: true}},
	}); strings.Join(got, ",") != "b,a" {
		t.Errorf("a and b by height, tallest first = %v, want b,a", got)
	}
}

// A unit the grammar refuses is a validation error on every engine door, the
// write and the vocabulary apply, and never a panic: each case is a code the
// parser once crashed or misread on.
func TestQuantityRefusesHostileUnits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installReadings(t, nil)
	for _, c := range []struct{ code, want string }{
		{"m/0", "a factor is a positive integer"},
		{"/0", "a factor is a positive integer"},
		{"(m/0)", "a factor is a positive integer"},
		{"m/(2.0)", "a factor is a positive integer"},
		{"m9", "beyond the bound of ±8"},
		{"m5.m5", "beyond the bound of ±8"},
		{"10*1000000000", "beyond the bound of ±24"},
		{"1234567890123.m", "beyond the bound of 12 digits"},
		{strings.Repeat("(", 9) + "m" + strings.Repeat(")", 9), "beyond the bound of 8"},
		{strings.Repeat("2.", 32) + "m", "beyond the bound of 32"},
	} {
		_, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: readingKind, Properties: map[string]any{
			"name": "hostile", "pulse": qv("1", c.code),
		}})
		if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("a write in %s: err = %v, want a validation error naming %q", c.code, err, c.want)
		}
		_, err = ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{readingManifest(map[string]any{
			"pulse": map[string]any{"type": "quantity", "units": []any{c.code}},
		})})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("a declaration listing %s: err = %v, want a refusal naming %q", c.code, err, c.want)
		}
	}
}
