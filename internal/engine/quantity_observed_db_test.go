package engine_test

// A quantity filter and order over the observed trait's `value`, which reads
// a temperature as a difference where the row says `delta: true`, as the
// observed index and the aggregate read do.

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
	if got := ids(substrate.Query{OrderBy: []substrate.Order{{Property: "value"}}}); strings.Join(got, ",") != "cooler,warm,warmer,surface" {
		t.Fatalf("ordered by value: %v, want the differences by their size, then the absolute reading", got)
	}
}

// HbA1c's NGSP % never reads in IFCC mmol/mol, and a vendor score never reads
// as a percent: the aggregate read refuses either unit, where it once
// answered 52 mmol/mol for 5.2 % and 1700 % for a strain of 17. A lab that
// writes its IFCC result under the NGSP metric makes the read refuse, where
// it once pooled 48 mmol/mol as 4.8 %.
func TestAggregateRefusesAUnitOutsideTheMetricsMeasure(t *testing.T) {
	t.Parallel()
	r := openObserved(t)
	ctx := context.Background()
	for id, props := range map[string]map[string]any{
		"lab/hba1c": {
			"metric": r.metricRef("health/hba1c"), "form": "gauge", "value": quantity("5.2", "%"),
			"at": "2026-10-07T06:41:12+03:00", "utcOffset": 180, "revision": 1,
		},
		"whoop/strain": {
			"metric": r.metricRef("health/whoop_strain"), "form": "summary", "statistic": "vendor", "period": "cycle",
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
		{"health/whoop_strain", "%", "summaries"},
	} {
		refused(t, c.metric+" in "+c.unit, read(c.metric, c.unit, c.from), c.unit)
	}
	if _, err := r.put(t, r.observation, "lab/hba1c-ifcc", map[string]any{
		"metric": r.metricRef("health/hba1c"), "form": "gauge", "value": quantity("48", "mmol/mol"),
		"at": "2026-10-08T06:41:12+03:00", "utcOffset": 180, "revision": 1,
	}); err != nil {
		t.Fatalf("put an IFCC result under the NGSP metric: %v", err)
	}
	for _, unit := range []string{"%", ""} {
		refused(t, "hba1c over a mmol/mol stream, unit "+unit, read("health/hba1c", unit, ""), "mmol/mol")
	}
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
