package engine_test

// Summaries in the aggregate read (decision 0161): a row reads one
// statistic, which the filter or a groupBy picks where a group holds two.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// summary writes one summary observation of a metric from a source over
// [at, at+span), at +03:00.
func (f aggFix) summary(id, metric, source, stat, value, unit string, at time.Time, span time.Duration) {
	f.t.Helper()
	f.mustPut(f.t, f.observation, id, map[string]any{
		"metric": f.metricRef(metric), "source": f.sourceRef(source), "form": "summary",
		"statistic": stat, "period": "night", "value": quantity(value, unit),
		"at": at.Format(time.RFC3339), "endsAt": at.Add(span).Format(time.RFC3339), "utcOffset": 180,
	})
}

// A source that keeps a night's average and maximum HRV as two summaries is
// read one statistic at a time. A read that would pool them in a row is
// refused, naming both statistics and the two ways to pick one; the filter
// narrows to one, and first, last and count then read that statistic alone;
// groupBy statistic splits them. avg, min and max reduce a single statistic's
// values whatever it is, vendor included, while sum adds only sums and
// counts.
func TestAggregateSummariesReadOneStatistic(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	night := time.Date(2026, 9, 1, 22, 0, 0, 0, time.UTC)
	avgs, maxes := []string{"60", "70", "65"}, []string{"90", "84", "88"}
	for n := range 3 {
		at := night.AddDate(0, 0, n)
		f.summary(fmt.Sprintf("hrv/%d/avg", n), "health/hrv_rmssd", "garmin", "avg", avgs[n], "ms", at, 8*time.Hour)
		f.summary(fmt.Sprintf("hrv/%d/max", n), "health/hrv_rmssd", "garmin", "max", maxes[n], "ms", at, 8*time.Hour)
		f.summary(fmt.Sprintf("rhr/%d", n), "health/resting_heart_rate", "whoop", "vendor", []string{"50", "54", "49"}[n], "/min", at, 8*time.Hour)
	}
	hrv := f.metricPath("health/hrv_rmssd")
	read := func(filter substrate.Filter, metric string, groupBy []string, names ...string) substrate.AggregateQuery {
		return substrate.AggregateQuery{Filter: filter, Aggregate: &substrate.Aggregate{
			Metric: &substrate.AggregateMetric{Ref: metric}, From: substrate.AggregateFromSummaries,
			Range: rng(night.AddDate(0, 0, -1), night.AddDate(0, 0, 4)), GroupBy: groupBy, Reducers: reducers(names...),
		}}
	}
	stat := func(c substrate.Cond) substrate.Filter {
		return substrate.Filter{Properties: map[string]substrate.Cond{"statistic": c}}
	}

	f.refusedAgg(read(substrate.Filter{}, hrv, nil, "count", "last"), "avg and max")
	f.refusedAgg(read(substrate.Filter{}, hrv, nil, "last"), "filter.properties.statistic")
	f.refusedAgg(read(substrate.Filter{}, hrv, nil, "avg"), "groupBy statistic")
	f.refusedAgg(read(stat(substrate.Cond{In: []any{"avg", "max"}}), hrv, nil, "count"), "avg and max")

	q := read(stat(substrate.Cond{Eq: "avg"}), hrv, nil, "count", "first", "last", "avg", "min", "max")
	res := f.agg(context.Background(), q.Filter, *q.Aggregate)
	if len(res.Rows) != 1 {
		t.Fatalf("the avg statistic: %d rows, want one", len(res.Rows))
	}
	row := res.Rows[0]
	for _, x := range []struct{ reducer, want string }{
		{"count", "3"}, {"first", "60"}, {"last", "65"}, {"min", "60"}, {"max", "70"},
	} {
		if v, _ := val(row, x.reducer); v != x.want {
			t.Errorf("the avg statistic's %s = %q, want %s", x.reducer, v, x.want)
		}
	}
	wantNum(t, "the avg statistic", row, "avg", 65, true)

	q = read(substrate.Filter{}, hrv, []string{"statistic"}, "count", "last")
	res = f.agg(context.Background(), q.Filter, *q.Aggregate)
	if len(res.Rows) != 2 {
		t.Fatalf("grouped by statistic: %d rows, want avg and max", len(res.Rows))
	}
	for i, want := range []struct{ stat, last string }{{"avg", "65"}, {"max", "88"}} {
		r := res.Rows[i]
		if r.Group == nil || r.Group.Statistic != want.stat {
			t.Fatalf("row %d groups %+v, want statistic %s", i, r.Group, want.stat)
		}
		if c, _ := val(r, "count"); c != "3" {
			t.Errorf("%s count = %s, want 3", want.stat, c)
		}
		if l, _ := val(r, "last"); l != want.last {
			t.Errorf("%s last = %s, want %s", want.stat, l, want.last)
		}
	}

	rhr := f.metricPath("health/resting_heart_rate")
	q = read(substrate.Filter{}, rhr, nil, "avg", "min", "max", "last", "count")
	res = f.agg(context.Background(), q.Filter, *q.Aggregate)
	if len(res.Rows) != 1 {
		t.Fatalf("vendor resting heart rate: %d rows, want one", len(res.Rows))
	}
	wantNum(t, "vendor", res.Rows[0], "avg", 51, true)
	for _, x := range []struct{ reducer, want string }{{"min", "49"}, {"max", "54"}, {"last", "49"}, {"count", "3"}} {
		if v, _ := val(res.Rows[0], x.reducer); v != x.want {
			t.Errorf("vendor %s = %q, want %s", x.reducer, v, x.want)
		}
	}
	f.refusedAgg(read(substrate.Filter{}, rhr, nil, "sum"), "vendor")
	f.refusedAgg(read(stat(substrate.Cond{Eq: "avg"}), hrv, nil, "sum"), "sum adds")

	raw := read(substrate.Filter{}, hrv, []string{"statistic"}, "count")
	raw.Aggregate.From = substrate.AggregateFromRaw
	f.refusedAgg(raw, "groupBy statistic")
}
