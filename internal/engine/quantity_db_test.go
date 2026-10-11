package engine

// The quantity datatype against Postgres: what a write stores and a read
// returns, the title a template renders from it, the filter that converts its
// operand exactly into every stored unit of the operand's measure, the
// comparator's half-line, the order by the value at its measure's base and the
// index that holds that order, and the narrowings a declaration change may not
// make over live rows.

import (
	"context"
	"errors"
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
		"weight": qv("72.50", "kg"), "temp": qv("98.6", "[degF]"), "pulse": qv("62", "{beats}/min"),
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
		{"bpm is its code", substrate.Cond{Eq: qv("60", "bpm")}, []string{"beats1hz"}},
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
