package engine_test

// Summaries in the aggregate read (decision 0161): a row reads one
// statistic, which the filter or a groupBy picks where a group holds two.

import (
	"context"
	"fmt"
	"strings"
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

	// The latest read groups by the statistic too: the newest night's
	// average and its maximum, a row each.
	latest := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
		Metric: &substrate.AggregateMetric{Ref: hrv}, GroupBy: []string{"statistic"}, Reducers: reducers("last"),
	})
	var got []string
	for _, r := range latest.Rows {
		l, _ := val(r, "last")
		if r.Group != nil {
			got = append(got, r.Group.Statistic+"="+l)
		}
	}
	if fmt.Sprint(got) != "[avg=65 max=88]" {
		t.Errorf("the latest read by statistic answers %v, want [avg=65 max=88]", got)
	}
}

// Under prefer-raw a bucket raw points cover reads them, so a sum over steps
// answers the raw total even where the source also keeps a daily total it
// labels `vendor`: 24 hourly chunks of 100 steps on 2 September answer 2400
// from raw. A bucket that falls to such a total answers sum null, because a
// vendor value does not add, and only a read from summaries refuses the sum.
// Two statistics in one group are refused under prefer-raw as from
// summaries, and the refusal names the statistic filter alone, since
// groupBy statistic is a read from summaries' word.
func TestAggregatePreferRawSumsRawStepsBesideAVendorTotal(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	plus3 := time.FixedZone("", 3*3600)
	day := time.Date(2026, 9, 2, 0, 0, 0, 0, plus3)
	for h := range 24 {
		at := day.Add(time.Duration(h) * time.Hour)
		f.mustPut(t, f.observation, fmt.Sprintf("steps/%02d", h), map[string]any{
			"metric": f.metricRef("health/steps"), "source": f.sourceRef("garmin"), "form": "amount",
			"value": quantity("100", "{steps}"), "utcOffset": 180,
			"at": at.Format(time.RFC3339), "endsAt": at.Add(time.Hour).Format(time.RFC3339),
		})
	}
	for d, total := range []string{"2400", "3100"} {
		at := day.AddDate(0, 0, d)
		f.mustPut(t, f.observation, fmt.Sprintf("steps/total/%d", d), map[string]any{
			"metric": f.metricRef("health/steps"), "source": f.sourceRef("garmin"), "form": "summary",
			"statistic": "vendor", "period": "day", "value": quantity(total, "{steps}"), "utcOffset": 180,
			"at": at.Format(time.RFC3339), "endsAt": at.AddDate(0, 0, 1).Format(time.RFC3339),
		})
	}
	read := func(from string) substrate.Aggregate {
		return substrate.Aggregate{
			Metric: &substrate.AggregateMetric{Ref: f.metricPath("health/steps")}, From: from,
			Range: rng(day, day.AddDate(0, 0, 2)), Bucket: &substrate.AggregateBucket{Calendar: "day"},
			Reducers: reducers("sum", "count"),
		}
	}
	res := f.agg(context.Background(), substrate.Filter{}, read(substrate.AggregateFromPreferRaw))
	if len(res.Rows) != 2 {
		t.Fatalf("prefer-raw steps: %d rows, want the 2nd from raw and the 3rd from its total", len(res.Rows))
	}
	if s, _ := val(res.Rows[0], "sum"); res.Rows[0].From != substrate.AggregateFromRaw || s != "2400" {
		t.Errorf("the 2nd: sum %q from %s, want 2400 from raw", s, res.Rows[0].From)
	}
	if s, ok := val(res.Rows[1], "sum"); res.Rows[1].From != substrate.AggregateFromSummaries || ok {
		t.Errorf("the 3rd: sum %q from %s, want null from summaries (a vendor total does not add)", s, res.Rows[1].From)
	}
	if c, _ := val(res.Rows[1], "count"); c != "1" {
		t.Errorf("the 3rd counts %s summaries, want 1", c)
	}
	summaries := read(substrate.AggregateFromSummaries)
	f.refusedAgg(substrate.AggregateQuery{Aggregate: &summaries}, "sum adds")

	night := time.Date(2026, 9, 1, 22, 0, 0, 0, time.UTC)
	f.summary("hrv/avg", "health/hrv_rmssd", "garmin", "avg", "60", "ms", night, 8*time.Hour)
	f.summary("hrv/max", "health/hrv_rmssd", "garmin", "max", "90", "ms", night, 8*time.Hour)
	hrv := substrate.Aggregate{
		Metric: &substrate.AggregateMetric{Ref: f.metricPath("health/hrv_rmssd")}, From: substrate.AggregateFromPreferRaw,
		Range: rng(day.AddDate(0, 0, -1), day.AddDate(0, 0, 2)), Reducers: reducers("last"),
	}
	_, err := f.ds.Aggregate(context.Background(), substrate.AggregateQuery{Aggregate: &hrv})
	if err == nil || !strings.Contains(err.Error(), "filter.properties.statistic") || strings.Contains(err.Error(), "groupBy statistic") {
		t.Errorf("prefer-raw over avg and max: err = %v, want a refusal naming filter.properties.statistic and not groupBy statistic", err)
	}
}
