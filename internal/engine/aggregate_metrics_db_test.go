package engine_test

// Several metrics in one aggregate read (decision 0161): one range, bucket,
// groupBy and sources policy, each metric with its own unit, reducers, from
// and statistic, held to the read's budgets together.

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

// todayFix writes one recorded-local day, 2026-09-24, of four metrics: heart
// rate read in London (+01:00) at 07:00 and 20:00, steps walked in Athens
// (+03:00) at 00:30 and 21:00, a weighing at noon UTC, and a night's resting
// heart rate kept as two statistics, an average and a maximum.
func todayFix(t *testing.T) aggFix {
	t.Helper()
	f := openAgg(t)
	london, athensOff := time.FixedZone("", 3600), time.FixedZone("", 3*3600)
	for i, x := range []struct {
		at    time.Time
		value string
	}{{time.Date(2026, 9, 24, 7, 0, 0, 0, london), "60"}, {time.Date(2026, 9, 24, 20, 0, 0, 0, london), "70"}} {
		f.mustPut(t, f.observation, fmt.Sprintf("hr/%d", i), map[string]any{
			"metric": f.metricRef("health/heart_rate"), "source": f.sourceRef("watch"), "form": "gauge",
			"value": quantity(x.value, "/min"), "at": at(x.at), "hold": "PT1M", "utcOffset": 60,
		})
	}
	for i, x := range []struct {
		at    time.Time
		value string
	}{{time.Date(2026, 9, 24, 0, 30, 0, 0, athensOff), "40"}, {time.Date(2026, 9, 24, 21, 0, 0, 0, athensOff), "60"}} {
		f.mustPut(t, f.observation, fmt.Sprintf("steps/%d", i), map[string]any{
			"metric": f.metricRef("health/steps"), "source": f.sourceRef("phone"), "form": "amount",
			"value": quantity(x.value, "{steps}"), "at": at(x.at), "endsAt": at(x.at.Add(10 * time.Minute)), "utcOffset": 180,
		})
	}
	f.weighing("w/1", "scale", "70.5", time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC), nil)
	night := time.Date(2026, 9, 24, 1, 0, 0, 0, athensOff)
	f.summary("rhr/avg", "health/resting_heart_rate", "watch", "avg", "52", "/min", night, 6*time.Hour)
	f.summary("rhr/max", "health/resting_heart_rate", "watch", "max", "58", "/min", night, 6*time.Hour)
	return f
}

// todayRange is three recorded-local days around 2026-09-24, read in UTC.
func todayRange() *substrate.AggregateRange {
	return rng(time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC), time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC))
}

// A day's heart rate, steps, weight and resting heart rate are one read:
// each metric answers in its own unit with its own reducers, from and
// statistic, every row names its metric, units names each metric's unit,
// and each metric's rows hold what the metric's own read answers. In the
// recorded-local calendar every row of 2026-09-24 carries the day's one
// start and end, the offsets of its first point over every metric (the
// Athens steps at 00:30) and its last (the London heart rate at 20:00),
// where each metric read alone writes its own.
func TestAggregateSeveralMetricsShareOneRead(t *testing.T) {
	t.Parallel()
	f := todayFix(t)
	hr, steps, mass, rhr := f.metricPath("health/heart_rate"), f.metricPath("health/steps"), f.metricPath("health/body_mass"), f.metricPath("health/resting_heart_rate")
	bucket := &substrate.AggregateBucket{Calendar: "day"}
	metrics := []substrate.AggregateMetricRead{
		{Ref: hr, Reducers: reducers("avg", "max")},
		{Ref: steps, Reducers: reducers("sum")},
		{Ref: mass, Unit: "g"},
		{Ref: rhr, From: substrate.AggregateFromSummaries, Statistic: "max"},
	}
	res := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
		Metrics: metrics, Range: todayRange(), Bucket: bucket, Reducers: reducers("last"),
	})
	if res.Metric != "" || res.Unit != "" || res.From != "" {
		t.Errorf("metric %q unit %q from %q, want none of them on a several-metric read", res.Metric, res.Unit, res.From)
	}
	if got := fmt.Sprint(res.Units); got != fmt.Sprint(map[string]string{hr: "/min", steps: "{steps}", mass: "g", rhr: "/min"}) {
		t.Errorf("units = %s", got)
	}
	type shown struct{ start, end, values string }
	show := func(row substrate.AggregateRow) shown {
		var vals []string
		for _, v := range row.Values {
			vals = append(vals, v.Reducer.String()+"="+*v.Value+v.Unit)
		}
		return shown{row.Start.Format(time.RFC3339), row.End.Format(time.RFC3339), fmt.Sprint(vals)}
	}
	day := shown{start: "2026-09-24T00:00:00+03:00", end: "2026-09-25T00:00:00+01:00"}
	want := []struct {
		metric string
		alone  shown
		values string
	}{
		{mass, shown{"2026-09-24T00:00:00Z", "2026-09-25T00:00:00Z", ""}, "[last=70500g]"},
		{hr, shown{"2026-09-24T00:00:00+01:00", "2026-09-25T00:00:00+01:00", ""}, "[avg=65/min max=70/min]"},
		{rhr, shown{"2026-09-24T00:00:00+03:00", "2026-09-25T00:00:00+03:00", ""}, "[last=58/min]"},
		{steps, shown{"2026-09-24T00:00:00+03:00", "2026-09-25T00:00:00+03:00", ""}, "[sum=100{steps}]"},
	}
	if len(res.Rows) != len(want) {
		t.Fatalf("%d rows, want one per metric on 2026-09-24: %+v", len(res.Rows), res.Rows)
	}
	for i, w := range want {
		row := res.Rows[i]
		if row.Group == nil || row.Group.Metric != w.metric {
			t.Fatalf("row %d group %+v, want metric %s (rows by bucket, then metric)", i, row.Group, w.metric)
		}
		if got, wantRow := show(row), (shown{day.start, day.end, w.values}); got != wantRow {
			t.Errorf("%s: %+v, want %+v", w.metric, got, wantRow)
		}
		m := metrics[slices.IndexFunc(metrics, func(m substrate.AggregateMetricRead) bool { return m.Ref == w.metric })]
		a := substrate.Aggregate{
			Metric: &substrate.AggregateMetric{Ref: m.Ref}, Unit: m.Unit, From: m.From, Range: todayRange(), Bucket: bucket, Reducers: m.Reducers,
		}
		if a.Reducers == nil {
			a.Reducers = reducers("last")
		}
		var filter substrate.Filter
		if m.Statistic != "" {
			filter.Properties = map[string]substrate.Cond{"statistic": {Eq: m.Statistic}}
		}
		alone := f.agg(context.Background(), filter, a)
		if len(alone.Rows) != 1 {
			t.Fatalf("%s alone: %+v", w.metric, alone.Rows)
		}
		if got, wantAlone := show(alone.Rows[0]), (shown{w.alone.start, w.alone.end, w.values}); got != wantAlone {
			t.Errorf("%s alone: %+v, want %+v", w.metric, got, wantAlone)
		}
	}
}

// A several-metric read is checked whole before it runs: each metric's
// reducers, unit and statistics on their own, named by the metric's place in
// metrics (a refusal at a read-wide option or budget opens with the entry
// and its reference), and the metrics' series times the buckets against
// maxBuckets together (four metrics of one group each over three days are
// twelve, so eleven is refused before any rollup row or point is read).
func TestAggregateSeveralMetricsAreCheckedBeforeTheyRun(t *testing.T) {
	t.Parallel()
	f := todayFix(t)
	hr, steps, mass, rhr := f.metricPath("health/heart_rate"), f.metricPath("health/steps"), f.metricPath("health/body_mass"), f.metricPath("health/resting_heart_rate")
	read := func(a substrate.Aggregate) substrate.AggregateQuery {
		if a.Range == nil {
			a.Range = todayRange()
		}
		if a.Bucket == nil {
			a.Bucket = &substrate.AggregateBucket{Calendar: "day"}
		}
		return substrate.AggregateQuery{Aggregate: &a}
	}
	four := []substrate.AggregateMetricRead{
		{Ref: hr}, {Ref: steps, Reducers: reducers("sum")}, {Ref: mass}, {Ref: rhr, From: substrate.AggregateFromSummaries, Statistic: "avg"},
	}
	for _, x := range []struct {
		label string
		q     substrate.AggregateQuery
		want  string
	}{
		{"a reducer the metric's form refuses", read(substrate.Aggregate{Metrics: []substrate.AggregateMetricRead{
			{Ref: hr}, {Ref: steps, Reducers: reducers("durationIn")},
		}, Reducers: reducers("avg")}), "aggregate.metrics[1].reducers[0]: durationIn takes [lo, hi)"},
		{"the read's reducer one metric's form refuses", read(substrate.Aggregate{Metrics: []substrate.AggregateMetricRead{
			{Ref: steps}, {Ref: hr},
		}, Reducers: reducers("sum")}), "aggregate.reducers[0]: in aggregate.metrics[1] (" + hr + "), sum does not apply to a gauge"},
		{"a unit outside the metric's measure", read(substrate.Aggregate{Metrics: []substrate.AggregateMetricRead{
			{Ref: mass}, {Ref: hr, Unit: "kg"},
		}, Reducers: reducers("last")}), "aggregate.metrics[1].unit: kg is not a unit of rate"},
		{"summaries holding two statistics", read(substrate.Aggregate{Metrics: []substrate.AggregateMetricRead{
			{Ref: hr}, {Ref: rhr, From: substrate.AggregateFromSummaries},
		}, Reducers: reducers("last")}), "aggregate.metrics[1]: the summaries from " + f.sourcePath("watch") +
			" hold the statistics avg and max, and a row reads one; name it with aggregate.metrics[1].statistic"},
		{"a statistic on a metric read raw", read(substrate.Aggregate{Metrics: []substrate.AggregateMetricRead{
			{Ref: hr}, {Ref: rhr, Statistic: "avg"},
		}, Reducers: reducers("last")}), "aggregate.metrics[1].statistic: a statistic narrows the metric's summaries, and from raw reads none"},
		{
			"groupBy statistic beside a metric read raw", read(substrate.Aggregate{Metrics: []substrate.AggregateMetricRead{
				{Ref: rhr, From: substrate.AggregateFromSummaries}, {Ref: hr},
			}, GroupBy: []string{"statistic"}, Reducers: reducers("last")}),
			"aggregate.groupBy: in aggregate.metrics[1] (" + hr + "), groupBy statistic splits summaries, and the metric is from raw; read it from summaries with aggregate.metrics[1].from",
		},
		{
			"one metric over maxBuckets", read(substrate.Aggregate{Metrics: []substrate.AggregateMetricRead{
				{Ref: rhr, From: substrate.AggregateFromSummaries},
			}, GroupBy: []string{"statistic"}, Reducers: reducers("last"), Budget: &substrate.AggregateBudget{MaxBuckets: 3}}),
			"aggregate.budget.maxBuckets: in aggregate.metrics[0] (" + rhr + "), 2 series (groups, and codes where grouped by code) times 3 buckets is 6, over 3",
		},
		{
			"one metric over maxGroups", read(substrate.Aggregate{Metrics: []substrate.AggregateMetricRead{
				{Ref: rhr, From: substrate.AggregateFromSummaries},
			}, GroupBy: []string{"statistic"}, Reducers: reducers("last"), Budget: &substrate.AggregateBudget{MaxGroups: 1}}),
			"aggregate.budget.maxGroups: in aggregate.metrics[0] (" + rhr + "), the metric makes 2 groups, over 1",
		},
		{"a metric named twice", read(substrate.Aggregate{Metrics: []substrate.AggregateMetricRead{
			{Ref: hr}, {Ref: hr, Unit: "/s"},
		}, Reducers: reducers("last")}), "aggregate.metrics[1].ref"},
		{"a metric with no reducer", read(substrate.Aggregate{Metrics: []substrate.AggregateMetricRead{
			{Ref: hr, Reducers: reducers("avg")}, {Ref: steps},
		}}), "aggregate.metrics[1].reducers"},
		{"a unit for every metric", read(substrate.Aggregate{Metrics: four, Unit: "kg", Reducers: reducers("last")}), "aggregate.unit"},
		{"no metric", read(substrate.Aggregate{Metrics: []substrate.AggregateMetricRead{}, Reducers: reducers("last")}), "aggregate.metrics: name at least one metric"},
		{
			"metric beside metrics", read(substrate.Aggregate{Metric: &substrate.AggregateMetric{Ref: hr}, Metrics: four, Reducers: reducers("last")}),
			"name one of metric, metrics and property",
		},
		{"no range", substrate.AggregateQuery{Aggregate: &substrate.Aggregate{Metrics: four, Reducers: reducers("last")}}, "several-metric read is bucketed"},
		{
			"a downsample",
			substrate.AggregateQuery{Aggregate: &substrate.Aggregate{Metrics: four, Range: todayRange()}, Downsample: &substrate.Downsample{Points: 10}},
			"a downsample reads one metric",
		},
		{"seventeen metrics", read(substrate.Aggregate{Metrics: make([]substrate.AggregateMetricRead, 17), Reducers: reducers("last")}), "at most 16 metrics"},
	} {
		f.refusedAgg(x.q, x.want)
	}

	budget := read(substrate.Aggregate{Metrics: four, Reducers: reducers("last"), Budget: &substrate.AggregateBudget{MaxBuckets: 11}})
	ctx, trace := engine.AggregateTraced(context.Background())
	_, err := f.ds.Aggregate(ctx, budget)
	if err == nil || !strings.Contains(err.Error(), "aggregate.budget.maxBuckets: the 4 metrics make 4 series") {
		t.Fatalf("maxBuckets 11: err = %v, want a refusal counting the four metrics' series", err)
	}
	if trace.RollupRows() != 0 || trace.RawPoints() != 0 {
		t.Errorf("the refused read took %d rollup rows and %d raw points", trace.RollupRows(), trace.RawPoints())
	}
	budget.Aggregate.Budget.MaxBuckets = 12
	if res, err := f.ds.Aggregate(context.Background(), budget); err != nil || len(res.Rows) != 4 {
		t.Fatalf("maxBuckets 12: %v, %+v", err, res)
	}
	groups := read(substrate.Aggregate{Metrics: four, Reducers: reducers("last"), Budget: &substrate.AggregateBudget{MaxGroups: 3}})
	f.refusedAgg(groups, "aggregate.budget.maxGroups: the 4 metrics make 4 groups")
}

// A recorded-local day is written at the offsets of the values the read
// takes, on a several-metric read as on a metric's own: heart rate read in
// London (+01:00) covers two minutes of 2026-09-24, so prefer-raw takes the
// night's summary, kept at +03:00, and the day's row starts and ends at
// +03:00, whether the metric is read alone or as the one entry of metrics.
// The two minutes prefer-raw passes over write neither bound.
func TestAggregateSeveralMetricsWriteADayAtTheValuesTaken(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	london := time.FixedZone("", 3600)
	for i, x := range []struct {
		at    time.Time
		value string
	}{{time.Date(2026, 9, 24, 7, 0, 0, 0, london), "60"}, {time.Date(2026, 9, 24, 20, 0, 0, 0, london), "70"}} {
		f.mustPut(t, f.observation, fmt.Sprintf("hr/%d", i), map[string]any{
			"metric": f.metricRef("health/heart_rate"), "source": f.sourceRef("watch"), "form": "gauge",
			"value": quantity(x.value, "/min"), "at": at(x.at), "hold": "PT1M", "utcOffset": 60,
		})
	}
	night := time.Date(2026, 9, 24, 1, 0, 0, 0, time.FixedZone("", 3*3600))
	f.summary("hr/night", "health/heart_rate", "watch", "avg", "52", "/min", night, 6*time.Hour)
	hr := f.metricPath("health/heart_rate")
	bucket := &substrate.AggregateBucket{Calendar: "day"}

	alone := f.agg(context.Background(), substrate.Filter{Properties: map[string]substrate.Cond{"statistic": {Eq: "avg"}}}, substrate.Aggregate{
		Metric: &substrate.AggregateMetric{Ref: hr}, From: substrate.AggregateFromPreferRaw, Range: todayRange(), Bucket: bucket, Reducers: reducers("avg"),
	})
	shared := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
		Metrics: []substrate.AggregateMetricRead{{Ref: hr, From: substrate.AggregateFromPreferRaw, Statistic: "avg"}},
		Range:   todayRange(), Bucket: bucket, Reducers: reducers("avg"),
	})
	want := "2026-09-24T00:00:00+03:00 2026-09-25T00:00:00+03:00 summaries [avg=52/min]"
	for label, res := range map[string]*substrate.AggregateResult{"alone": alone, "in metrics": shared} {
		if len(res.Rows) != 1 {
			t.Fatalf("%s: %d rows, want 2026-09-24's: %+v", label, len(res.Rows), res.Rows)
		}
		row := res.Rows[0]
		var vals []string
		for _, v := range row.Values {
			vals = append(vals, v.Reducer.String()+"="+*v.Value+v.Unit)
		}
		if got := fmt.Sprint(row.Start.Format(time.RFC3339), " ", row.End.Format(time.RFC3339), " ", row.From, " ", vals); got != want {
			t.Errorf("%s: %s, want %s", label, got, want)
		}
	}
}
