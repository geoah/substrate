package vocabulary_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/vocabulary"
)

func instant(t *testing.T, s string) *time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return &ts
}

func q(value, unit string) map[string]any { return map[string]any{"value": value, "unit": unit} }

// single is a valid gauge observation; each case edits a copy.
func single(t *testing.T) vocabulary.ObservedRecord {
	return vocabulary.ObservedRecord{
		Variant: vocabulary.ObservedSingle,
		At:      instant(t, "2026-10-07T06:41:12+03:00"),
		Props: map[string]any{
			"metric": "ada.example.com/observations/metric/health/body_mass",
			"form":   "gauge", "value": q("72.40", "kg"), "utcOffset": int64(180), "revision": int64(1),
		},
	}
}

// packed is a valid hour of irregular heart rate.
func packed(t *testing.T) vocabulary.ObservedRecord {
	return vocabulary.ObservedRecord{
		Variant: vocabulary.ObservedPacked,
		At:      instant(t, "2026-10-07T06:00:00Z"),
		EndsAt:  instant(t, "2026-10-07T07:00:00Z"),
		Props: map[string]any{
			"metric": "ada.example.com/observations/metric/health/heart_rate",
			"form":   "gauge", "unit": "/min", "hold": "PT4M",
			"offsets":   []any{int64(0), int64(118000), int64(241000)},
			"values":    []any{"58", nil, "61"},
			"utcOffset": int64(180),
		},
	}
}

func edit(r vocabulary.ObservedRecord, set map[string]any) vocabulary.ObservedRecord {
	props := map[string]any{}
	for k, v := range r.Props {
		props[k] = v
	}
	for k, v := range set {
		if v == nil {
			delete(props, k)
			continue
		}
		props[k] = v
	}
	r.Props = props
	return r
}

func TestCheckObservedAdmitsTheWorkedExamples(t *testing.T) {
	for name, r := range map[string]vocabulary.ObservedRecord{
		"a weighing": single(t),
		"a door":     edit(single(t), map[string]any{"value": nil, "code": "open"}),
		"a vendor score": func() vocabulary.ObservedRecord {
			r := edit(single(t), map[string]any{
				"form": "summary", "statistic": "vendor", "period": "cycle", "value": q("67", "%"),
			})
			r.EndsAt = instant(t, "2026-10-07T22:38:00+03:00")
			return r
		}(),
		"a reading kept as received": edit(single(t), map[string]any{"value": nil, "sourceValue": "12.3"}),
		"an absent reading":          edit(single(t), map[string]any{"value": nil, "absentReason": "not-performed"}),
		// Without core's `result` trait, `text` is an ordinary property: a
		// note beside the value, not a second result shape.
		"a reading with a note named text": edit(single(t), map[string]any{"text": "after a run"}),
		"an hour of heart rate":            packed(t),
		"a regular trace": edit(packed(t), map[string]any{
			"offsets": nil, "every": "PT20M", "values": []any{"1", "2", "3"},
		}),
		"an energy meter": edit(packed(t), map[string]any{
			"form": "counter", "hold": nil, "resetRule": "monotonic", "unit": "kW.h",
			"values": []any{"18342.112", "18342.391", "0.204"},
		}),
		"a deviation": edit(single(t), map[string]any{"value": q("0.3", "Cel"), "delta": true}),
		"a sleep stage": func() vocabulary.ObservedRecord {
			r := edit(single(t), map[string]any{"form": "amount", "value": nil, "code": "rem"})
			r.EndsAt = instant(t, "2026-10-07T08:00:00+03:00")
			return r
		}(),
		"a stage total with its category": func() vocabulary.ObservedRecord {
			r := edit(single(t), map[string]any{"form": "amount", "value": q("94", "min"), "code": "rem"})
			r.EndsAt = instant(t, "2026-10-07T08:00:00+03:00")
			return r
		}(),
		"an amount trace with categories": edit(packed(t), map[string]any{
			"form": "amount", "hold": nil, "unit": "min",
			"values": []any{"20", nil, "35"}, "codes": []any{"light", "deep", "rem"},
		}),
	} {
		if problems := vocabulary.CheckObserved(r); len(problems) != 0 {
			t.Errorf("%s: refused: %v", name, problems)
		}
	}
}

func TestCheckObservedRefusals(t *testing.T) {
	labresult := func(set map[string]any) vocabulary.ObservedRecord {
		r := edit(single(t), set)
		r.Result = true
		return r
	}
	for name, c := range map[string]struct {
		r    vocabulary.ObservedRecord
		want string
	}{
		"no form":      {edit(single(t), map[string]any{"form": nil}), "props.form: an observed value needs a form"},
		"a fifth form": {edit(single(t), map[string]any{"form": "state"}), `props.form: "state" is not a form`},
		"an amount's categories miscounted": {
			edit(packed(t), map[string]any{"form": "amount", "codes": []any{"rem", "deep"}}),
			"props.codes: 2 codes for 3 values",
		},
		"value and code":      {edit(single(t), map[string]any{"code": "open"}), "props.value: an observed value is a number or a code, never both"},
		"neither":             {edit(single(t), map[string]any{"value": nil}), "props.value: an observed value needs value or code"},
		"a value and why not": {edit(single(t), map[string]any{"absentReason": "error"}), "props.absentReason: says why there is no value"},
		"an amount at an instant": {
			edit(single(t), map[string]any{"form": "amount"}),
			"props.endsAt: an amount covers [at, endsAt)",
		},
		"a summary without its statistic": {func() vocabulary.ObservedRecord {
			r := edit(single(t), map[string]any{"form": "summary", "period": "day"})
			r.EndsAt = instant(t, "2026-10-08T00:00:00+03:00")
			return r
		}(), "props.statistic: a summary names the statistic"},
		"a statistic on a gauge":  {edit(single(t), map[string]any{"statistic": "avg"}), "props.statistic: only a summary states a statistic"},
		"a held counter":          {edit(single(t), map[string]any{"form": "counter", "hold": "PT1H"}), "props.hold: only a gauge holds its value; a counter does not"},
		"a resetting gauge":       {edit(single(t), map[string]any{"resetRule": "monotonic"}), "props.resetRule: only a counter resets"},
		"an unknown contract":     {edit(single(t), map[string]any{"form": "counter", "resetRule": "decrease"}), `props.resetRule: "decrease" is not a counter contract`},
		"a reset with no rule":    {edit(single(t), map[string]any{"form": "counter", "resetAt": "2026-10-01T00:00:00Z"}), "props.resetAt: names when a counter's accumulation began"},
		"a wrap with no modulus":  {edit(single(t), map[string]any{"form": "counter", "resetRule": "wrap"}), "props.wrapAt: a wrapping counter names the value it wraps at"},
		"a modulus with no wrap":  {edit(single(t), map[string]any{"form": "counter", "resetRule": "monotonic", "wrapAt": "256"}), "props.wrapAt: is the value a wrapping counter returns to zero at"},
		"a coded counter":         {edit(single(t), map[string]any{"form": "counter", "value": nil, "code": "on"}), "props.code: a counter is a running total"},
		"a delta in kilograms":    {edit(single(t), map[string]any{"delta": true}), "props.delta: marks a temperature difference, and kg measures mass"},
		"a circular mass":         {edit(single(t), map[string]any{"circular": true}), "props.circular: averages an angle on the circle, and kg measures mass"},
		"an offset past 18 hours": {edit(single(t), map[string]any{"utcOffset": int64(1200)}), "props.utcOffset: minutes east of UTC, between -1080 and 1080"},
		"a result in two shapes": {
			labresult(map[string]any{"ratio": map[string]any{"numerator": "1", "denominator": "80"}}),
			"props.value: a result holds exactly one of value, codedValue, ratio, text or absentReason; this record holds value and ratio",
		},
		"a result with a value and text": {
			labresult(map[string]any{"text": "high"}),
			"props.value: a result holds exactly one of value, codedValue, ratio, text or absentReason; this record holds value and text",
		},
		"a result in no shape": {
			labresult(map[string]any{"value": nil}),
			"props.value: a result holds exactly one of value, codedValue, ratio, text or absentReason (or sourceValue alone, as received); this record holds none",
		},
		"a summary trace":   {edit(packed(t), map[string]any{"form": "summary"}), "props.form: a summary is one value per record"},
		"values and codes":  {edit(packed(t), map[string]any{"codes": []any{"a", "b", "c"}}), "props.values: a trace holds values or codes, never both"},
		"every and offsets": {edit(packed(t), map[string]any{"every": "PT1M"}), "props.every: a trace spaces its points by every or places them by offsets, never both"},
		"short offsets":     {edit(packed(t), map[string]any{"offsets": []any{int64(0), int64(1)}}), "props.offsets: 2 offsets for 3 values"},
		"a falling offset":  {edit(packed(t), map[string]any{"offsets": []any{int64(0), int64(5000), int64(4000)}}), "props.offsets[2]: 4000 ms comes before the offset ahead of it"},
		"an offset at the end": {
			edit(packed(t), map[string]any{"offsets": []any{int64(0), int64(1), int64(3600000)}}),
			"props.offsets[2]: 3600000 ms is outside the period [0, 3600000)",
		},
		"too many for every": {edit(packed(t), map[string]any{"offsets": nil, "every": "PT30M"}), "props.every: 3 points every PT30M do not fit the period PT1H"},
		"a unitless trace":   {edit(packed(t), map[string]any{"unit": nil}), "props.unit: a trace of values names the unit"},
		"a coded trace with a unit": {
			edit(packed(t), map[string]any{"values": nil, "codes": []any{"a", nil, "b"}}),
			"props.unit: a trace of codes carries no unit",
		},
		"no points": {edit(packed(t), map[string]any{"offsets": []any{}, "values": []any{}}), "props.values: a trace holds at least one point"},
		"an empty code in a trace": {
			edit(packed(t), map[string]any{"values": nil, "unit": nil, "codes": []any{"open", "", "closed"}}),
			"props.codes[1]: a code is not empty",
		},
		"an empty category in an amount trace": {
			edit(packed(t), map[string]any{"form": "amount", "hold": nil, "unit": "min", "codes": []any{"light", "deep", ""}}),
			"props.codes[2]: a code is not empty",
		},
		"an empty code": {edit(single(t), map[string]any{"value": nil, "code": ""}), "props.code: a code is not empty"},
		// The observed index reduces in float64 and weighs a value by the
		// milliseconds it holds: a value past 1e280 could overflow that sum,
		// so it is refused, not stored and left out of every aggregate.
		"a value past float64": {
			edit(single(t), map[string]any{"value": q("1"+strings.Repeat("0", 400), "kg")}),
			"props.value: the magnitude is past 1e280",
		},
		"a trace point past float64": {
			edit(packed(t), map[string]any{"values": []any{"60", "-1" + strings.Repeat("0", 400), "62"}}),
			"props.values[1]: the magnitude is past 1e280",
		},
		"a value past the weighing bound": {
			edit(single(t), map[string]any{"value": q("1"+strings.Repeat("0", 300), "kg")}),
			"props.value: the magnitude is past 1e280",
		},
		// A trace's unit is read as a quantity's is, so a refused spelling
		// gets its own guidance, not the grammar's parse error.
		"a trace in a bare count": {
			edit(packed(t), map[string]any{"unit": "count"}),
			`props.unit: "count": a bare count names nothing counted`,
		},
	} {
		problems := vocabulary.CheckObserved(c.r)
		found := false
		for _, p := range problems {
			found = found || strings.HasPrefix(p, c.want)
		}
		if !found {
			t.Errorf("%s: problems %q, want one starting %q", name, problems, c.want)
		}
	}
}

func TestCheckObservedRefusesAnOversizedTrace(t *testing.T) {
	n := vocabulary.MaxTracePoints + 1
	values := make([]any, n)
	for i := range values {
		values[i] = "60"
	}
	r := edit(packed(t), map[string]any{"offsets": nil, "every": "PT0.1S", "values": values})
	problems := strings.Join(vocabulary.CheckObserved(r), "\n")
	if !strings.Contains(problems, "props.values: 20001 points; a trace holds at most 20000") {
		t.Errorf("problems %q name no point limit", problems)
	}
	values = make([]any, 9000)
	for i := range values {
		values[i] = strings.Repeat("9", 120)
	}
	r = edit(packed(t), map[string]any{"offsets": nil, "every": "PT0.1S", "values": values})
	problems = strings.Join(vocabulary.CheckObserved(r), "\n")
	if !strings.Contains(problems, "a trace holds at most 1048576 (1 MiB)") {
		t.Errorf("problems %q name no byte limit", problems)
	}
}

// The byte limit counts a trace as it is stored: with the stats its points
// compute, never the stale stats a rewrite's merged row still carries. A
// code the stats repeat as `first` and `last` takes three times its length.
func TestCheckObservedSizesATraceWithItsComputedStats(t *testing.T) {
	coded := func(code string) vocabulary.ObservedRecord {
		return edit(packed(t), map[string]any{
			"unit": nil, "hold": nil, "values": nil, "offsets": []any{int64(0)}, "codes": []any{code},
		})
	}
	withStats := func(r vocabulary.ObservedRecord) vocabulary.ObservedRecord {
		stats, err := vocabulary.TraceStats(*r.At, r.Props)
		if err != nil {
			t.Fatalf("stats: %v", err)
		}
		return edit(r, map[string]any{"stats": stats})
	}
	// 400 KiB stored three times is past the limit: refused on its create,
	// before any stats are stamped.
	big := coded(strings.Repeat("x", 400<<10))
	if problems := strings.Join(vocabulary.CheckObserved(big), "\n"); !strings.Contains(problems, "a trace holds at most 1048576 (1 MiB)") {
		t.Errorf("a create whose stamped stats pass the limit: problems %q", problems)
	}
	// 300 KiB three times fits: admitted on its create and on its identical
	// rewrite, which carries the stored stats.
	fits := coded(strings.Repeat("x", 300<<10))
	for name, r := range map[string]vocabulary.ObservedRecord{"create": fits, "rewrite": withStats(fits)} {
		if problems := vocabulary.CheckObserved(r); len(problems) != 0 {
			t.Errorf("%s of a trace that fits with its stats: %v", name, problems)
		}
	}
}

// The byte limit counts the `measure` the write path stamps beside the stats.
// A create carries neither and an identical rewrite carries both, so a check
// that leaves the measure out admits a create its own rewrite then refuses.
func TestCheckObservedSizesATraceWithItsMeasure(t *testing.T) {
	base := edit(packed(t), map[string]any{"unit": "kg"})
	stored := func(r vocabulary.ObservedRecord) vocabulary.ObservedRecord {
		stats, err := vocabulary.TraceStats(*r.At, r.Props)
		if err != nil {
			t.Fatalf("stats: %v", err)
		}
		return edit(r, map[string]any{"stats": stats, vocabulary.ObservedMeasure: "mass"})
	}
	size := func(r vocabulary.ObservedRecord) int {
		raw, err := json.Marshal(r.Props)
		if err != nil {
			t.Fatal(err)
		}
		return len(raw)
	}
	padded := func(n int) vocabulary.ObservedRecord {
		return edit(base, map[string]any{"adapter": strings.Repeat("x", n)})
	}
	// The padding that puts the stored trace at exactly the limit.
	exact := vocabulary.MaxTraceBytes - (size(stored(padded(0))) - size(padded(0))) - size(padded(0))
	if got := size(stored(padded(exact))); got != vocabulary.MaxTraceBytes {
		t.Fatalf("the stored trace is %d bytes, want %d", got, vocabulary.MaxTraceBytes)
	}
	for name, r := range map[string]vocabulary.ObservedRecord{"create": padded(exact), "rewrite": stored(padded(exact))} {
		if problems := vocabulary.CheckObserved(r); len(problems) != 0 {
			t.Errorf("%s of a trace stored at exactly the limit: %v", name, problems)
		}
	}
	for name, r := range map[string]vocabulary.ObservedRecord{"create": padded(exact + 1), "rewrite": stored(padded(exact + 1))} {
		if problems := strings.Join(vocabulary.CheckObserved(r), "\n"); !strings.Contains(problems, "a trace holds at most 1048576 (1 MiB)") {
			t.Errorf("%s of a trace stored one byte past the limit: problems %q", name, problems)
		}
	}
}

// A zero spacing is refused as a problem, never a panic: the stats the size
// check computes place points by a spacing the rules have just refused, and
// a trace with no offsets would index an absent list.
func TestCheckObservedRefusesAZeroSpacing(t *testing.T) {
	r := edit(packed(t), map[string]any{"unit": "kg", "offsets": nil, "every": "PT0S", "values": []any{"1"}})
	problems := strings.Join(vocabulary.CheckObserved(r), "\n")
	if !strings.Contains(problems, "props.every: the spacing between points is longer than zero") {
		t.Errorf("problems %q name no spacing", problems)
	}
	if _, err := vocabulary.TracePoints(*r.At, r.Props); err == nil || !strings.Contains(err.Error(), "not longer than zero") {
		t.Errorf("TracePoints with a zero spacing: %v, want a refusal", err)
	}
}

// Each point of a trace may be in range while their spread is not: the stats
// are refused as a validation problem, never stored as an infinity a JSON
// encoder cannot write.
func TestTraceStatsRefuseASpreadPastFloat64(t *testing.T) {
	r := edit(packed(t), map[string]any{"values": []any{"1" + strings.Repeat("0", 159), nil, "0"}})
	if problems := vocabulary.CheckObserved(r); len(problems) > 0 {
		t.Fatalf("each point is in range, yet CheckObserved refuses: %q", problems)
	}
	if stats, err := vocabulary.TraceStats(*r.At, r.Props); err == nil || !strings.Contains(err.Error(), "past 1.8e308") {
		t.Errorf("TraceStats = %v, %v; want a refusal naming the range", stats, err)
	}
	r = edit(packed(t), map[string]any{"values": []any{"1" + strings.Repeat("0", 150), nil, "0"}})
	if _, err := vocabulary.TraceStats(*r.At, r.Props); err != nil {
		t.Errorf("a spread of 1e150 fits a float64: %v", err)
	}
}

func TestTraceStatsAreComputedFromThePoints(t *testing.T) {
	r := packed(t)
	got, err := vocabulary.TraceStats(*r.At, r.Props)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"count": int64(2), "min": "58", "max": "61", "mean": 59.5, "m2": 4.5,
		"first":    map[string]any{"at": "2026-10-07T06:00:00Z", "value": "58"},
		"last":     map[string]any{"at": "2026-10-07T06:04:01Z", "value": "61"},
		"coverage": "PT4M1S",
	}
	if !reflect.DeepEqual(got, want) {
		gj, _ := json.Marshal(got)
		wj, _ := json.Marshal(want)
		t.Errorf("stats = %s, want %s", gj, wj)
	}
	// Codes carry the count and the ends, with the code as each value.
	coded := edit(packed(t), map[string]any{"unit": nil, "values": nil, "codes": []any{nil, "open", "closed"}})
	got, err = vocabulary.TraceStats(*coded.At, coded.Props)
	if err != nil {
		t.Fatal(err)
	}
	want = map[string]any{
		"count":    int64(2),
		"first":    map[string]any{"at": "2026-10-07T06:01:58Z", "value": "open"},
		"last":     map[string]any{"at": "2026-10-07T06:04:01Z", "value": "closed"},
		"coverage": "PT2M3S",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("coded stats = %v, want %v", got, want)
	}
	// An empty code a row stored before the write refused it is no code: the
	// point is unavailable, the one reading the index's tables give it too.
	empty := edit(packed(t), map[string]any{"unit": nil, "values": nil, "codes": []any{"open", "", "closed"}})
	got, err = vocabulary.TraceStats(*empty.At, empty.Props)
	if err != nil {
		t.Fatal(err)
	}
	if got["count"] != int64(2) {
		t.Errorf("an empty code counted: stats = %v, want a count of 2", got)
	}
	points, _ := vocabulary.TracePoints(*empty.At, empty.Props)
	if len(points) != 3 || !points[1].Unavailable || points[0].Unavailable {
		t.Errorf("an empty code's point is not the unavailable one: %+v", points)
	}
	for _, c := range []struct {
		hasValue bool
		code     string
		want     bool
	}{{true, "", true}, {true, "rem", true}, {false, "open", true}, {false, "", false}} {
		if got := vocabulary.ObservedPointAvailable(c.hasValue, c.code); got != c.want {
			t.Errorf("ObservedPointAvailable(%v, %q) = %v, want %v", c.hasValue, c.code, got, c.want)
		}
	}
	// A trace whose every point is unavailable counts nothing.
	gone := edit(packed(t), map[string]any{"values": []any{nil, nil, nil}})
	got, _ = vocabulary.TraceStats(*gone.At, gone.Props)
	if !reflect.DeepEqual(got, map[string]any{"count": int64(0)}) {
		t.Errorf("all-unavailable stats = %v", got)
	}
}

// A replayed trace arrives with its numbers as a changelog segment spells
// them, every one with an exponent: its points are the ones the write saw.
func TestTracePointsReadAReplayedTrace(t *testing.T) {
	at := *instant(t, "2026-10-07T06:00:00Z")
	written := map[string]any{"offsets": []any{int64(0), int64(118000), int64(3599000)}, "values": []any{"58", nil, "61"}}
	replayed := map[string]any{
		"offsets": []any{json.Number("0"), json.Number("1.18E5"), json.Number("3.599E6")}, "values": []any{"58", nil, "61"},
	}
	want, err := vocabulary.TracePoints(at, written)
	if err != nil {
		t.Fatal(err)
	}
	got, err := vocabulary.TracePoints(at, replayed)
	if err != nil {
		t.Fatalf("a replayed trace: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("replayed points %+v, written %+v", got, want)
	}
	if rev, ok := vocabulary.ObservedRevisionOf(map[string]any{"revision": json.Number("1.2E1")}); !ok || rev != 12 {
		t.Errorf("a replayed revision reads %d (%v), want 12", rev, ok)
	}
	if _, err := vocabulary.TracePoints(at, map[string]any{
		"offsets": []any{json.Number("1.5E0")}, "values": []any{"1"},
	}); err == nil {
		t.Error("a fractional offset was read as a point")
	}
}

// observationsDeclarations reads the shipped observations sample's
// declarations, the kinds that bind `observed` among them, as maps a case may
// edit before it builds them.
func observationsDeclarations(t *testing.T) []map[string]any {
	t.Helper()
	entries, err := os.ReadDir("../../samples/observations")
	if err != nil {
		t.Fatalf("read the observations sample: %v", err)
	}
	var out []map[string]any
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		body, err := os.ReadFile(filepath.Join("../../samples/observations", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		dec := yaml.NewDecoder(bytes.NewReader(body))
		for {
			var m map[string]any
			if err := dec.Decode(&m); errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				t.Fatalf("%s: %v", e.Name(), err)
			}
			kind, _ := m["kind"].(string)
			if len(m) > 0 && vocabulary.KindPackage(kind) == vocabulary.PackageCore &&
				vocabulary.VocabularyDocumentKind(vocabulary.KindName(kind)) {
				out = append(out, m)
			}
		}
	}
	return out
}

// installObservations builds the documents and installs them on the seed.
func installObservations(t *testing.T, raw []map[string]any) error {
	t.Helper()
	reg, err := vocabulary.LoadFS(os.DirFS("../../kinds/substrate.reamde.dev"))
	if err != nil {
		t.Fatalf("load the seed: %v", err)
	}
	return installOn(t, reg, raw)
}

// installOn builds the documents and installs them on reg.
func installOn(t *testing.T, reg *vocabulary.Registry, raw []map[string]any) error {
	t.Helper()
	var docs []vocabulary.Document
	for _, m := range raw {
		d, err := vocabulary.DocumentFromMap(m)
		if err != nil {
			t.Fatalf("read a document: %v", err)
		}
		docs = append(docs, d)
	}
	packages, err := vocabulary.BuildPackages(docs, vocabulary.SourceInstalled)
	if err != nil {
		return err
	}
	return reg.InstallAll(packages)
}

// THE OBSERVED CONTRACT HOLDS EACH PROPERTY'S SHAPE, not its datatype alone.
// A trace's `offsets`, `values` and `codes` are lists and everything else is
// one value: a kind that declares a list `metric` has every write admitted and
// indexed as nothing, because the index reads one reference path, and a kind
// that declares one `values` refuses every write.
func TestObservedBindingHoldsListAndSingleShapes(t *testing.T) {
	if err := installObservations(t, observationsDeclarations(t)); err != nil {
		t.Fatalf("the shipped observations sample is refused: %v", err)
	}
	for _, c := range []struct {
		kind, prop string
		set        map[string]any
		want       string
	}{
		{"trace", "values", map[string]any{"repeated": false}, "trait substrate.reamde.dev/core/observed requires values to be a list"},
		{"trace", "offsets", map[string]any{"repeated": nil}, "requires offsets to be a list"},
		{"trace", "metric", map[string]any{"repeated": true}, "requires metric to hold one value, not a list"},
		{"observation", "metric", map[string]any{"repeated": true}, "requires metric to hold one value, not a list"},
		{"observation", "code", map[string]any{"repeated": true}, "requires code to hold one value"},
		{"observation", "channel", map[string]any{"keyed": true}, "requires channel unkeyed, not a keyed map"},
		{"trace", "origins", map[string]any{"repeated": nil}, "requires origins to be a list"},
		{"observation", "origin", map[string]any{"repeated": true}, "requires origin to hold one value"},
		{"metric", "rollups", map[string]any{"repeated": nil}, "trait substrate.reamde.dev/core/metric requires rollups to be a list"},
		{"metric", "unit", map[string]any{"repeated": true}, "requires unit to hold one value"},
		{"metric", "units", map[string]any{"repeated": nil}, "requires units to be a list"},
		// A metric's units are unit records (decision 0186).
		{"metric", "unit", map[string]any{"kind": "any"}, "requires unit to pin kind substrate.reamde.dev/core/unit"},
		{"metric", "units", map[string]any{"kind": "samples.substrate.reamde.dev/observations/source"}, "requires units to pin kind substrate.reamde.dev/core/unit"},
		{"source", "priorities", map[string]any{"keyed": nil}, "trait substrate.reamde.dev/core/source requires priorities to be a keyed map"},
		// The metric a value names carries the metric contract, or the
		// write rules and the aggregate read have no unit to read.
		{"observation", "metric", map[string]any{"kind": "any"}, "pins no kind; an observed value's metric pins a kind that binds substrate.reamde.dev/core/metric"},
		{"trace", "metric", map[string]any{"kind": "samples.substrate.reamde.dev/observations/source"}, "which does not bind substrate.reamde.dev/core/metric"},
		{"trace", "metric", map[string]any{"kind": nil, "trait": "substrate.reamde.dev/core/source"}, "pins trait substrate.reamde.dev/core/source"},
	} {
		docs := observationsDeclarations(t)
		found := false
		for _, d := range docs {
			meta, _ := d["metadata"].(map[string]any)
			if meta["id"] != "samples.substrate.reamde.dev/observations/"+c.kind {
				continue
			}
			props := d["data"].(map[string]any)["properties"].(map[string]any)
			p, _ := props[c.prop].(map[string]any)
			if p == nil {
				t.Fatalf("observations/%s declares no %s", c.kind, c.prop)
			}
			for k, v := range c.set {
				if v == nil {
					delete(p, k)
				} else {
					p[k] = v
				}
			}
			found = true
		}
		if !found {
			t.Fatalf("no observations/%s declaration", c.kind)
		}
		err := installObservations(t, docs)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s.%s with %v: %v, want a refusal saying %q", c.kind, c.prop, c.set, err, c.want)
		}
	}
}

// A package that drops core's `metric` from its metric kind is refused while
// an observed kind of ANOTHER package still pins that kind. The install only
// rebuilds the package it touches, so a check of that package alone admits
// the change and the other package is quarantined at the next open.
func TestObservedMetricPinIsHeldAcrossPackages(t *testing.T) {
	const metricKind = "samples.substrate.reamde.dev/observations/metric"
	vitals := func() []map[string]any {
		var reading map[string]any
		for _, d := range observationsDeclarations(t) {
			if meta, _ := d["metadata"].(map[string]any); meta["id"] == "samples.substrate.reamde.dev/observations/observation" {
				reading = d
			}
		}
		if reading == nil {
			t.Fatal("no observations/observation declaration")
		}
		reading["metadata"].(map[string]any)["id"] = "samples.substrate.reamde.dev/vitals/observation"
		data := reading["data"].(map[string]any)
		data["package"] = "vitals"
		props := data["properties"].(map[string]any)
		props["source"].(map[string]any)["kind"] = "samples.substrate.reamde.dev/observations/source"
		return []map[string]any{{
			"kind":     "substrate.reamde.dev/core/package",
			"metadata": map[string]any{"id": "samples.substrate.reamde.dev/vitals"},
			"data":     map[string]any{"authority": "samples.substrate.reamde.dev", "package": "vitals", "version": 1},
		}, reading}
	}
	// The observations package with its metric kind unbound, its own values
	// pinning the trait so the package alone still admits.
	unbound := func() []map[string]any {
		docs := observationsDeclarations(t)
		for _, d := range docs {
			meta, _ := d["metadata"].(map[string]any)
			data, _ := d["data"].(map[string]any)
			switch meta["id"] {
			case metricKind:
				data["traits"] = []any{}
			case "samples.substrate.reamde.dev/observations/observation", "samples.substrate.reamde.dev/observations/trace":
				p := data["properties"].(map[string]any)["metric"].(map[string]any)
				delete(p, "kind")
				p["trait"] = vocabulary.TraitMetricCore
			}
		}
		return docs
	}
	seed, err := vocabulary.LoadFS(os.DirFS("../../kinds/substrate.reamde.dev"))
	if err != nil {
		t.Fatalf("load the seed: %v", err)
	}
	if err := installOn(t, seed.Clone(), unbound()); err != nil {
		t.Fatalf("the unbound observations package alone is refused: %v", err)
	}
	reg := seed.Clone()
	if err := installOn(t, reg, append(observationsDeclarations(t), vitals()...)); err != nil {
		t.Fatalf("observations and vitals are refused: %v", err)
	}
	// The engine's write: the touched package removed, its rebuild installed.
	reg.Remove("samples.substrate.reamde.dev/observations")
	err = installOn(t, reg, unbound())
	want := "samples.substrate.reamde.dev/vitals/observation: data.properties.metric: pins " + metricKind + ", which does not bind"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("dropping the metric binding under another package's value: %v, want a refusal saying %q", err, want)
	}
	// Deleting the metric kind outright is refused the same way. Only the
	// rebuilt package resolves its pins, so without this the deletion lands
	// and the vitals package fails `unknown referent kind` at the next open.
	deleted := func() []map[string]any {
		var out []map[string]any
		for _, d := range unbound() {
			meta, _ := d["metadata"].(map[string]any)
			if meta["id"] == metricKind {
				continue
			}
			if data, _ := d["data"].(map[string]any); data != nil {
				if installs, ok := data["installs"].([]any); ok {
					data["installs"] = slices.DeleteFunc(slices.Clone(installs), func(v any) bool { return v == metricKind })
				}
			}
			if meta["id"] == "samples.substrate.reamde.dev/observations/observation" {
				props := d["data"].(map[string]any)["properties"].(map[string]any)
				fields := props["components"].(map[string]any)["fields"].(map[string]any)
				delete(fields["metric"].(map[string]any), "kind")
			}
			out = append(out, d)
		}
		return out
	}
	if err := installOn(t, seed.Clone(), deleted()); err != nil {
		t.Fatalf("the observations package without its metric kind is refused alone: %v", err)
	}
	reg = seed.Clone()
	if err := installOn(t, reg, append(observationsDeclarations(t), vitals()...)); err != nil {
		t.Fatalf("observations and vitals are refused: %v", err)
	}
	reg.Remove("samples.substrate.reamde.dev/observations")
	err = installOn(t, reg, deleted())
	want = "samples.substrate.reamde.dev/vitals/observation: data.properties.metric: pins " + metricKind + ", which no package declares"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("deleting the metric kind under another package's value: %v, want a refusal saying %q", err, want)
	}
}

// A value's metric may pin the `metric` trait itself instead of one kind:
// any record whose kind binds it is a metric.
func TestObservedMetricMayPinTheMetricTrait(t *testing.T) {
	docs := observationsDeclarations(t)
	for _, d := range docs {
		meta, _ := d["metadata"].(map[string]any)
		if meta["id"] != "samples.substrate.reamde.dev/observations/trace" {
			continue
		}
		props := d["data"].(map[string]any)["properties"].(map[string]any)
		p := props["metric"].(map[string]any)
		delete(p, "kind")
		p["trait"] = vocabulary.TraitMetricCore
	}
	if err := installObservations(t, docs); err != nil {
		t.Fatalf("a trace whose metric pins the metric trait is refused: %v", err)
	}
}

// A kind that binds `result` declares its object properties with the fields
// the result-shape rule and the console read; a trait contracts the datatype
// (`json`, which an object meets) and the loader holds the fields.
func TestResultBindingHoldsItsObjectShapes(t *testing.T) {
	body, err := os.ReadFile("../../samples/health/labresult.yaml")
	if err != nil {
		t.Fatal(err)
	}
	lab := func() map[string]any {
		var m map[string]any
		if err := yaml.Unmarshal(body, &m); err != nil {
			t.Fatal(err)
		}
		// Moved into the observations package, so it installs beside the
		// kinds it points at; its report lives in health.
		m["metadata"].(map[string]any)["id"] = "samples.substrate.reamde.dev/observations/labresult"
		data := m["data"].(map[string]any)
		data["package"] = "observations"
		delete(data["properties"].(map[string]any), "report")
		return m
	}
	withLab := func(m map[string]any) []map[string]any {
		docs := observationsDeclarations(t)
		for _, d := range docs {
			if d["kind"] == "substrate.reamde.dev/core/bundle" {
				data := d["data"].(map[string]any)
				data["installs"] = append(data["installs"].([]any), "samples.substrate.reamde.dev/observations/labresult")
			}
		}
		return append(docs, m)
	}
	if err := installObservations(t, withLab(lab())); err != nil {
		t.Fatalf("the lab result is refused: %v", err)
	}
	for _, c := range []struct {
		prop string
		edit func(p map[string]any)
		want string
	}{
		{
			"ratio", func(p map[string]any) { delete(p["fields"].(map[string]any), "denominator") },
			"trait substrate.reamde.dev/core/result requires field denominator: decimal",
		},
		{"codedValue", func(p map[string]any) {
			p["fields"].(map[string]any)["code"] = map[string]any{"type": "int", "description": "the code"}
		}, "data.properties.codedValue.fields.code: trait substrate.reamde.dev/core/result requires one string"},
		{"referenceRanges", func(p map[string]any) { delete(p, "repeated") }, "requires referenceRanges to be a list"},
		{"text", func(p map[string]any) { p["type"] = "string" }, "trait substrate.reamde.dev/core/result requires type text, got string"},
		{"interpretation", func(p map[string]any) {
			for k := range p {
				delete(p, k)
			}
			p["type"] = "string"
			p["repeated"] = true
			p["description"] = "the lab's flags"
		}, "requires interpretation to be an object with fields code, display, system"},
	} {
		m := lab()
		c.edit(m["data"].(map[string]any)["properties"].(map[string]any)[c.prop].(map[string]any))
		err := installObservations(t, withLab(m))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want a refusal saying %q", c.prop, err, c.want)
		}
	}
}
