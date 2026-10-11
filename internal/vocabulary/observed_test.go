package vocabulary_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

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
		Declares: func(name string) bool { return name == "sourceValue" || name == "absentReason" },
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
			"offsets":        []any{int64(0), int64(118000), int64(241000)},
			"values":         []any{"58", nil, "61"},
			"utcOffsetStart": int64(180), "utcOffsetEnd": int64(180),
		},
		Declares: func(string) bool { return false },
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
		"an hour of heart rate":      packed(t),
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
		r.Declares = func(name string) bool {
			switch name {
			case "codedValue", "ratio", "text", "absentReason", "sourceValue":
				return true
			}
			return false
		}
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
	// A trace whose every point is unavailable counts nothing.
	gone := edit(packed(t), map[string]any{"values": []any{nil, nil, nil}})
	got, _ = vocabulary.TraceStats(*gone.At, gone.Props)
	if !reflect.DeepEqual(got, map[string]any{"count": int64(0)}) {
		t.Errorf("all-unavailable stats = %v", got)
	}
}
