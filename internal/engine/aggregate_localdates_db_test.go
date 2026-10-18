package engine_test

import (
	"context"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// A range of whole recorded-local dates (range.recordedLocal) holds every
// value lived on those dates at whatever offset, and nothing from the dates
// around them: 1 October at +14:00 and 3 October at -10:00 are in [1 Oct,
// 4 Oct), 30 September at -05:00 and 4 October at +03:00 are not, though
// the instants of a +03:00 reader's three days hold the first of those and
// miss the 3 October one. Only a recorded-local day read takes the flag, and
// its bounds are dates or midnights.
func TestAggregateRecordedLocalDatesHoldEveryOffset(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	for _, x := range []struct {
		id     string
		at     string
		offset int
	}{
		{"w/kiritimati", "2026-10-01T09:30:00Z", 14 * 60},
		{"w/honolulu", "2026-10-04T08:00:00Z", -10 * 60},
		{"w/athens", "2026-10-03T22:00:00Z", 3 * 60},
		{"w/lima", "2026-10-01T04:00:00Z", -5 * 60},
	} {
		f.mustPut(t, f.observation, x.id, map[string]any{
			"metric": f.metricRef("health/body_mass"), "source": f.sourceRef("scale"), "form": "gauge",
			"value": quantity("70", "kg"), "at": x.at, "utcOffset": x.offset, "hold": "PT0S",
		})
	}
	read := func(r substrate.AggregateRange, bucket substrate.AggregateBucket) substrate.Aggregate {
		return substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/body_mass")},
			Range:    &r,
			Bucket:   &bucket,
			Reducers: reducers("count"),
		}
	}
	days := func(res *substrate.AggregateResult) []string {
		var out []string
		for _, row := range res.Rows {
			out = append(out, row.Start.Format(time.DateOnly))
		}
		return out
	}
	day := substrate.AggregateBucket{Calendar: "day"}

	dates := f.agg(context.Background(), substrate.Filter{},
		read(substrate.AggregateRange{Gte: "2026-10-01", Lt: "2026-10-04", RecordedLocal: true}, day))
	if got := days(dates); len(got) != 2 || got[0] != "2026-10-01" || got[1] != "2026-10-03" {
		t.Errorf("recorded-local dates answer days %v, want 2026-10-01 and 2026-10-03", got)
	}
	midnights := f.agg(context.Background(), substrate.Filter{},
		read(substrate.AggregateRange{Gte: "2026-10-01T00:00:00+03:00", Lt: "2026-10-04T00:00:00+03:00", RecordedLocal: true}, day))
	if got, want := days(midnights), days(dates); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("midnight bounds answer days %v, want the dates' %v", got, want)
	}
	instants := f.agg(context.Background(), substrate.Filter{},
		read(substrate.AggregateRange{Gte: "2026-10-01T00:00:00+03:00", Lt: "2026-10-04T00:00:00+03:00"}, day))
	if got := days(instants); len(got) != 2 || got[0] != "2026-09-30" || got[1] != "2026-10-01" {
		t.Errorf("the reader's instants answer days %v, want 2026-09-30 and 2026-10-01", got)
	}

	for _, x := range []struct {
		r      substrate.AggregateRange
		bucket substrate.AggregateBucket
		says   string
	}{
		{
			substrate.AggregateRange{Gte: "2026-10-01", Lt: "2026-10-04", RecordedLocal: true},
			substrate.AggregateBucket{Calendar: "day", Timezone: "Europe/Athens"},
			"range.recordedLocal",
		},
		{
			substrate.AggregateRange{Gte: "2026-10-01", Lt: "2026-10-04", RecordedLocal: true},
			substrate.AggregateBucket{Calendar: "week"},
			"range.recordedLocal",
		},
		{substrate.AggregateRange{Gte: "2026-10-01T06:00:00Z", Lt: "2026-10-04", RecordedLocal: true}, day, "range.gte"},
		{substrate.AggregateRange{Gte: "2026-10-04", Lt: "2026-10-01", RecordedLocal: true}, day, "aggregate.range"},
	} {
		f.refusedAgg(substrate.AggregateQuery{Aggregate: ptr(read(x.r, x.bucket))}, x.says)
	}
}

// A range of recorded-local dates reads the instants around its dates, and
// the days those instants also reach are never answered, so they count
// against no budget: one date under maxBuckets 1 answers its day, with a
// fill, and with a value lived on the day before and the day after, whose
// days the read makes and drops. Two dates under maxBuckets 1 are refused.
func TestAggregateRecordedLocalDatesCountOnlyTheirDays(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	for _, x := range []struct {
		id     string
		at     string
		offset int
	}{
		{"w/before", "2026-09-30T20:00:00Z", 0},
		{"w/on", "2026-10-01T08:00:00Z", 180},
		{"w/after", "2026-10-02T04:00:00Z", 0},
	} {
		f.mustPut(t, f.observation, x.id, map[string]any{
			"metric": f.metricRef("health/body_mass"), "source": f.sourceRef("scale"), "form": "gauge",
			"value": quantity("70", "kg"), "at": x.at, "utcOffset": x.offset, "hold": "PT0S",
		})
	}
	read := func(lt, fill string) substrate.AggregateQuery {
		return substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/body_mass")},
			Range:    &substrate.AggregateRange{Gte: "2026-10-01", Lt: lt, RecordedLocal: true},
			Bucket:   &substrate.AggregateBucket{Calendar: "day"},
			Reducers: reducers("count"),
			Fill:     fill,
			Budget:   &substrate.AggregateBudget{MaxBuckets: 1},
		}}
	}
	for _, fill := range []string{"", "zero"} {
		res, err := f.ds.Aggregate(context.Background(), read("2026-10-02", fill))
		if err != nil {
			t.Fatalf("fill %q: one date under maxBuckets 1: %v", fill, err)
		}
		if len(res.Rows) != 1 || res.Rows[0].Start.Format(time.DateOnly) != "2026-10-01" || res.Rows[0].Count != 1 {
			t.Errorf("fill %q: %+v, want 1 October's one weighing", fill, res.Rows)
		}
	}
	f.refusedAgg(read("2026-10-03", ""), "aggregate.budget.maxBuckets")
}

// The groups and series of a range of recorded-local dates are the ones
// answering a row on its dates: scale A's weighing on 30 September, read
// only to find the dates' values, makes no group and no series. So 1 October
// grouped by source under maxBuckets 1, or under maxGroups 1, answers B's
// weighing alone, with a fill too, where counting A refused the read; a
// property read of workouts grouped by source answers the same. A second
// source on 1 October is refused under either budget.
func TestAggregateRecordedLocalDatesCountOnlyTheirSources(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	for _, x := range []struct {
		id, source, at string
	}{
		{"w/a", "a", "2026-09-30T20:00:00Z"},
		{"w/b", "b", "2026-10-01T08:00:00Z"},
	} {
		f.mustPut(t, f.observation, x.id, map[string]any{
			"metric": f.metricRef("health/body_mass"), "source": f.sourceRef(x.source), "form": "gauge",
			"value": quantity("70", "kg"), "at": x.at, "utcOffset": 0, "hold": "PT0S",
		})
	}
	plus3 := time.FixedZone("", 3*3600)
	f.workout("run/a", "a", "running", time.Date(2026, 9, 30, 23, 0, 0, 0, plus3), 30*time.Minute, "5")
	f.workout("run/b", "b", "running", time.Date(2026, 10, 1, 10, 0, 0, 0, plus3), 30*time.Minute, "5")
	weighings := func(budget substrate.AggregateBudget, fill string) substrate.AggregateQuery {
		return substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/body_mass")},
			Range:    &substrate.AggregateRange{Gte: "2026-10-01", Lt: "2026-10-02", RecordedLocal: true},
			Bucket:   &substrate.AggregateBucket{Calendar: "day"},
			GroupBy:  []string{"source"},
			Reducers: reducers("count"),
			Fill:     fill,
			Budget:   &budget,
		}}
	}
	runs := func(budget substrate.AggregateBudget, fill string) substrate.AggregateQuery {
		return substrate.AggregateQuery{Filter: substrate.Filter{Kinds: []string{f.workoutKind()}}, Aggregate: &substrate.Aggregate{
			Property: &substrate.AggregateProperty{Name: "distance", GroupBy: []string{"source"}, UTCOffset: "utcOffsetStart"},
			Range:    &substrate.AggregateRange{Gte: "2026-10-01", Lt: "2026-10-02", RecordedLocal: true},
			Bucket:   &substrate.AggregateBucket{Calendar: "day"},
			Reducers: reducers("count"),
			Fill:     fill,
			Budget:   &budget,
		}}
	}
	budgets := []substrate.AggregateBudget{{MaxBuckets: 1}, {MaxGroups: 1}}
	for _, budget := range budgets {
		for _, fill := range []string{"", "zero"} {
			res, err := f.ds.Aggregate(context.Background(), weighings(budget, fill))
			if err != nil {
				t.Errorf("weighings, %+v, fill %q: %v", budget, fill, err)
			} else if len(res.Rows) != 1 || res.Rows[0].Group == nil || res.Rows[0].Group.Source != f.sourcePath("b") || res.Rows[0].Count != 1 {
				t.Errorf("weighings, %+v, fill %q: %+v, want B's one weighing on 1 October", budget, fill, res.Rows)
			}
			res, err = f.ds.Aggregate(context.Background(), runs(budget, fill))
			if err != nil {
				t.Errorf("runs, %+v, fill %q: %v", budget, fill, err)
			} else if len(res.Rows) != 1 || res.Rows[0].Group == nil || res.Rows[0].Group.Properties["source"] != f.sourcePath("b") || res.Rows[0].Count != 1 {
				t.Errorf("runs, %+v, fill %q: %+v, want B's one run on 1 October", budget, fill, res.Rows)
			}
		}
	}

	f.mustPut(t, f.observation, "w/c", map[string]any{
		"metric": f.metricRef("health/body_mass"), "source": f.sourceRef("c"), "form": "gauge",
		"value": quantity("71", "kg"), "at": "2026-10-01T09:00:00Z", "utcOffset": 0, "hold": "PT0S",
	})
	f.workout("run/c", "c", "running", time.Date(2026, 10, 1, 12, 0, 0, 0, plus3), 30*time.Minute, "5")
	f.refusedAgg(weighings(budgets[0], ""), "aggregate.budget.maxBuckets")
	f.refusedAgg(weighings(budgets[1], ""), "aggregate.budget.maxGroups")
	f.refusedAgg(runs(budgets[0], ""), "aggregate.budget.maxBuckets")
	f.refusedAgg(runs(budgets[1], ""), "aggregate.budget.maxGroups")
}
