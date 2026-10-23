package engine_test

// A quantity filter over the observed trait's `value`, which reads a
// temperature as a difference where the row says `delta: true`, as the
// observed index and the aggregate read do. An order by `value` is refused:
// the observation kind anchors no unit, and the metric decides each row's.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

func TestQuantityFilterAndOrderReadADeltaAsADifference(t *testing.T) {
	t.Parallel()
	r := openObserved(t)
	ctx := context.Background()
	reading := func(metric, value, unit string, delta bool) map[string]any {
		p := map[string]any{
			"metric": r.metricRef(metric), "form": "gauge", "value": quantity(value, unit),
			"at": "2026-10-07T06:41:12+03:00", "utcOffset": 180, "revision": 1,
		}
		if delta {
			p["delta"] = true
		}
		return p
	}
	for id, props := range map[string]map[string]any{
		"warmer":  reading("health/skin_temperature_deviation", "0.5", "K", true),
		"warm":    reading("health/skin_temperature_deviation", "0.2", "Cel", true),
		"cooler":  reading("health/skin_temperature_deviation", "-0.1", "[degF]", true),
		"surface": reading("health/skin_temperature", "33.5", "Cel", false),
	} {
		if _, err := r.put(t, r.observation, id, props); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
	}
	ids := func(q substrate.Query) []string {
		t.Helper()
		q.Filter.Kinds = []string{r.observation}
		q.First = 50
		page, err := r.ds.List(ctx, q)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		var out []string
		for _, rec := range page.Records {
			out = append(out, rec.ID)
		}
		return out
	}
	byID := []substrate.Order{{Property: "id"}}
	for _, tc := range []struct {
		name string
		cond substrate.Cond
		want string
	}{
		{"a difference above 0.3 Cel, and the absolute reading", substrate.Cond{Gt: quantity("0.3", "Cel")}, "surface,warmer"},
		{"a difference below 0.3 K", substrate.Cond{Lt: quantity("0.3", "K")}, "cooler,warm"},
		{"a difference equal to 0.36 [degF]", substrate.Cond{Eq: quantity("0.36", "[degF]")}, "warm"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ids(substrate.Query{OrderBy: byID, Filter: substrate.Filter{
				Properties: map[string]substrate.Cond{"value": tc.cond},
			}})
			if strings.Join(got, ",") != tc.want {
				t.Fatalf("filtered to %v, want %s", got, tc.want)
			}
		})
	}
	_, err := r.ds.List(ctx, substrate.Query{
		Filter:  substrate.Filter{Kinds: []string{r.observation}},
		OrderBy: []substrate.Order{{Property: "value"}},
	})
	refused(t, "an order by value", err, "declares no unit")
}

// HbA1c's NGSP percent never reads in IFCC mmol/mol, and a vendor score
// never reads as a percent: the aggregate read refuses either unit, where it
// once answered 52 mmol/mol for 5.2 % and 1700 % for a strain of 17. A lab
// that writes its IFCC result under the NGSP metric is refused at the write
// (decision 0186), where it once pooled 48 mmol/mol as 4.8 %.
func TestAggregateRefusesAUnitOutsideTheMetricsComponent(t *testing.T) {
	t.Parallel()
	r := openObserved(t)
	ctx := context.Background()
	putShippedUnits(t, r.ds, "score")
	if _, err := r.put(t, r.metric, "vendor/strain", map[string]any{
		"name": "A vendor's strain", "unit": unitRef("score"), "form": "summary",
	}); err != nil {
		t.Fatalf("put the strain metric: %v", err)
	}
	for id, props := range map[string]map[string]any{
		"lab/hba1c": {
			"metric": r.metricRef("health/hba1c"), "form": "gauge", "value": quantity("5.2", "%"),
			"at": "2026-10-07T06:41:12+03:00", "utcOffset": 180, "revision": 1,
		},
		"vendor/strain": {
			"metric": r.metricRef("vendor/strain"), "form": "summary", "statistic": "vendor", "period": "cycle",
			"value": quantity("17", "{score}"), "at": "2026-10-07T00:00:00+03:00", "endsAt": "2026-10-08T00:00:00+03:00",
			"utcOffset": 180, "revision": 1,
		},
	} {
		if _, err := r.put(t, r.observation, id, props); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
	}
	read := func(metric, unit, from string) error {
		_, err := r.ds.Aggregate(ctx, substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
			Metric: &substrate.AggregateMetric{Ref: r.metric + "/" + metric},
			From:   from,
			Range:  &substrate.AggregateRange{Gte: "2026-10-01T00:00:00Z", Lt: "2026-10-10T00:00:00Z"},
			Bucket: &substrate.AggregateBucket{Calendar: "month"}, Unit: unit,
			Reducers: []substrate.AggregateReducer{{Name: "last"}},
		}})
		return err
	}
	if err := read("health/hba1c", "%", ""); err != nil {
		t.Fatalf("hba1c in %%: %v", err)
	}
	for _, c := range []struct{ metric, unit, from string }{
		{"health/hba1c", "mmol/mol", ""},
		{"vendor/strain", "percent", "summaries"},
	} {
		refused(t, c.metric+" in "+c.unit, read(c.metric, c.unit, c.from), c.unit)
	}
	_, err := r.put(t, r.observation, "lab/hba1c-ifcc", map[string]any{
		"metric": r.metricRef("health/hba1c"), "form": "gauge", "value": quantity("48", "mmol/mol"),
		"at": "2026-10-08T06:41:12+03:00", "utcOffset": 180, "revision": 1,
	})
	refused(t, "an IFCC result under the NGSP metric", err, "mmol/mol does not convert to the metric's unit percent")
}

// A trace's values are decimals, which have no length bound, so the observed
// check holds each to vocabulary.MaxDecimalDigits digits as a quantity value
// is held: a point Postgres numeric cannot hold is refused at write, where it
// once failed the observed index's insert.
func TestATracePointPastTheDigitBoundIsRefused(t *testing.T) {
	t.Parallel()
	r := openObserved(t)
	_, err := r.put(t, r.trace, "hand/heart_rate/2026-10-07T06", map[string]any{
		"metric": r.metricRef("health/heart_rate"), "form": "gauge", "unit": "/min",
		"at": "2026-10-07T06:00:00Z", "endsAt": "2026-10-07T06:01:00Z",
		"utcOffset": 180,
		"offsets":   []any{0}, "values": []any{"1." + strings.Repeat("1", 16384)}, "revision": 1,
	})
	refused(t, "a 16385-digit point", err, "at most 1000 digits")
}

// A meter's glucose in mg/dL and one in mmol/L are one series: the health
// sample's analyte-annotated units convert through glucose's molar mass, so
// `health/blood_glucose` admits both and an aggregate reads both, in the
// metric's mmol/L or in the unit the read names. Each value keeps the digits
// it was written with, and plain mg/dL, which no conversion joins to mmol/L,
// is refused.
func TestBloodGlucoseInEitherUnitReadsAsOneSeries(t *testing.T) {
	t.Parallel()
	r := openObserved(t)
	ctx := context.Background()
	reading := func(value, unit, at string) map[string]any {
		return map[string]any{
			"metric": r.metricRef("health/blood_glucose"), "form": "gauge", "hold": "PT15M",
			"value": quantity(value, unit), "at": at, "utcOffset": 180, "revision": 1,
		}
	}
	for id, props := range map[string]map[string]any{
		"meter/athens": reading("5.4", "mmol/L{glucose}", "2026-10-07T07:20:00+03:00"),
		"meter/boston": reading("97", "mg/dL{glucose}", "2026-10-08T07:22:00+03:00"),
	} {
		if _, err := r.put(t, r.observation, id, props); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
	}
	if q, _ := r.get(t, r.observation, "meter/boston").Properties["value"].(map[string]any); q["value"] != "97" || q["unit"] != "mg/dL_glucose" {
		t.Errorf("the mg/dL reading is stored as %v, want its own digits in mg/dL_glucose", q)
	}
	read := func(unit string) map[string]float64 {
		t.Helper()
		res, err := r.ds.Aggregate(ctx, substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: r.metric + "/health/blood_glucose"},
			Range:    &substrate.AggregateRange{Gte: "2026-10-01T00:00:00Z", Lt: "2026-11-01T00:00:00Z"},
			Bucket:   &substrate.AggregateBucket{Calendar: "month", Timezone: "Europe/Athens"},
			Unit:     unit,
			Reducers: []substrate.AggregateReducer{{Name: "count"}, {Name: "min"}, {Name: "max"}},
		}})
		if err != nil {
			t.Fatalf("aggregate in %q: %v", unit, err)
		}
		if len(res.Rows) != 1 {
			t.Fatalf("aggregate in %q: %d rows, want the month's one", unit, len(res.Rows))
		}
		out := map[string]float64{}
		for _, name := range []string{"count", "min", "max"} {
			v, ok := val(res.Rows[0], name)
			if !ok {
				t.Fatalf("aggregate in %q: no %s", unit, name)
			}
			out[name] = num(t, v)
		}
		return out
	}
	near := func(got, want float64) bool { return got > want-1e-6 && got < want+1e-6 }
	// 97 mg/dL is 97 / 18.0156 mmol/L; 5.4 mmol/L is 5.4 x 18.0156 mg/dL.
	if got := read(""); got["count"] != 2 || !near(got["min"], 97/18.0156) || !near(got["max"], 5.4) {
		t.Errorf("in the metric's mmol/L: %v, want count 2, min %.6f, max 5.4", got, 97/18.0156)
	}
	if got := read("mg/dL{glucose}"); got["count"] != 2 || !near(got["min"], 97) || !near(got["max"], 5.4*18.0156) {
		t.Errorf("in mg/dL{glucose}: %v, want count 2, min 97, max %.5f", got, 5.4*18.0156)
	}
	for _, unit := range []string{"mg/dL", "mmol/L"} {
		_, err := r.put(t, r.observation, "meter/plain", reading("101", unit, "2026-10-09T07:21:00+03:00"))
		refused(t, "a glucose reading in plain "+unit, err, "does not convert to the metric's unit")
	}
}
