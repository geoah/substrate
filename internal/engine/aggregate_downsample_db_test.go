package engine_test

// The downsample's instants (decision 0161): each column's first, last, min
// and max say when they were taken, so a chart draws them where they lie.

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/observed"
	"github.com/geoah/substrate/internal/substrate"
)

// valueAt is a row's value of a reducer and the instant it carries, "" for
// none.
func valueAt(row substrate.AggregateRow, reducer string) (value, at string) {
	for _, v := range row.Values {
		if v.Reducer.String() != reducer {
			continue
		}
		if v.Value != nil {
			value = *v.Value
		}
		if v.At != nil {
			at = v.At.Format(time.RFC3339)
		}
	}
	return value, at
}

// earliest is the first point of the window, in the order every reader
// takes points in, whose stored digits are want.
func earliest(points []observed.Point, w observed.Window, want string) string {
	var best *observed.Point
	for i := range points {
		p := points[i]
		if p.At.Before(w.From) || !p.At.Before(w.To) || !p.HasValue || p.Exact != want {
			continue
		}
		if best == nil || observed.Before(p, *best) {
			best = &p
		}
	}
	if best == nil {
		return ""
	}
	return best.At.UTC().Format(time.RFC3339)
}

// A downsample's first, last, min and max carry the instant they were
// taken at, the earliest point holding an extreme where several do, the
// same from rollup rows as from raw points. A day of heart rate is served
// from whole rows alone; steps every five minutes read from 00:07 take the
// cut end [00:07, 00:15) from raw points beside the rows: its 00:10 holds
// the minimum, 3, which 01:00 ties in a row, and the maximum, 9, lies at
// 00:35 and 00:40 in one row and at 01:20 in another. A bucketed read
// carries no instant.
func TestAggregateDownsampleSaysWhereItsValuesLie(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	f.heartRate("garmin", day, 7*time.Minute, 0, time.Time{}, time.Time{})
	for at := day.Add(5 * time.Minute); at.Before(day.Add(2 * time.Hour)); at = at.Add(5 * time.Minute) {
		value := "5"
		switch at.Sub(day) {
		case 10 * time.Minute, time.Hour:
			value = "3"
		case 35 * time.Minute, 40 * time.Minute, 80 * time.Minute:
			value = "9"
		}
		f.mustPut(t, f.observation, "steps/"+at.Format("1504"), map[string]any{
			"metric": f.metricRef("health/steps"), "source": f.sourceRef("phone"), "form": "amount",
			"value": quantity(value, "{steps}"), "at": at.Format(time.RFC3339), "endsAt": at.Add(time.Minute).Format(time.RFC3339), "utcOffset": 0,
		})
	}
	downsample := func(ctx context.Context, metric string, from, to time.Time, points int) *substrate.AggregateResult {
		t.Helper()
		res, err := f.ds.Aggregate(ctx, substrate.AggregateQuery{
			Aggregate:  &substrate.Aggregate{Metric: &substrate.AggregateMetric{Ref: f.metricPath(metric)}, Range: rng(from, to)},
			Downsample: &substrate.Downsample{Points: points},
		})
		if err != nil {
			t.Fatalf("downsample %s: %v", metric, err)
		}
		return res
	}
	type column map[string][2]string
	columns := func(res *substrate.AggregateResult) []column {
		var out []column
		for _, row := range res.Rows {
			c := column{}
			for _, r := range []string{"first", "last", "min", "max"} {
				v, at := valueAt(row, r)
				c[r] = [2]string{v, at}
			}
			out = append(out, c)
		}
		return out
	}

	ctx, trace := engine.AggregateTraced(context.Background())
	hr := downsample(ctx, "health/heart_rate", day, day.AddDate(0, 0, 1), 10)
	if trace.RollupRows() == 0 || trace.RawPoints() != 0 {
		t.Fatalf("the heart rate took %d rollup rows and %d raw points; it should be served from rows alone", trace.RollupRows(), trace.RawPoints())
	}
	points := f.stream("health/heart_rate", "garmin")
	for i, row := range hr.Rows {
		w := observed.Window{From: row.Start, To: row.End}
		first, _ := observed.First(points, w)
		last, _ := observed.Last(points, w)
		lo, _ := observed.MinPoint(points, w)
		hi, _ := observed.MaxPoint(points, w)
		want := column{
			"first": {first.Exact, first.At.UTC().Format(time.RFC3339)}, "last": {last.Exact, last.At.UTC().Format(time.RFC3339)},
			"min": {lo.Exact, earliest(points, w, lo.Exact)}, "max": {hi.Exact, earliest(points, w, hi.Exact)},
		}
		if got := columns(hr)[i]; fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("heart rate column %s: %v, want %v", row.Start.Format(time.RFC3339), got, want)
		}
	}
	raw := downsample(engine.AggregateRaw(context.Background()), "health/heart_rate", day, day.AddDate(0, 0, 1), 10)
	if got, want := fmt.Sprint(columns(raw)), fmt.Sprint(columns(hr)); got != want {
		t.Errorf("heart rate from raw points:\n%s\nfrom rollup rows:\n%s", got, want)
	}

	ctx, trace = engine.AggregateTraced(context.Background())
	steps := downsample(ctx, "health/steps", day.Add(7*time.Minute), day.Add(2*time.Hour), 1)
	if trace.RollupRows() == 0 || trace.RawPoints() == 0 {
		t.Fatalf("the steps took %d rollup rows and %d raw points; want rows and the cut end's points", trace.RollupRows(), trace.RawPoints())
	}
	want := fmt.Sprint([]column{{
		"first": {"3", "2026-09-01T00:10:00Z"}, "last": {"5", "2026-09-01T01:55:00Z"},
		"min": {"3", "2026-09-01T00:10:00Z"}, "max": {"9", "2026-09-01T00:35:00Z"},
	}})
	if got := fmt.Sprint(columns(steps)); got != want {
		t.Errorf("steps: %s, want %s", got, want)
	}
	raw = downsample(engine.AggregateRaw(context.Background()), "health/steps", day.Add(7*time.Minute), day.Add(2*time.Hour), 1)
	if got := fmt.Sprint(columns(raw)); got != want {
		t.Errorf("steps from raw points: %s, want %s", got, want)
	}

	bucketed := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
		Metric: &substrate.AggregateMetric{Ref: f.metricPath("health/heart_rate")}, Range: rng(day, day.AddDate(0, 0, 1)),
		Reducers: reducers("first", "min"),
	})
	for _, v := range bucketed.Rows[0].Values {
		if v.At != nil {
			t.Errorf("a bucketed read's %s carries at %s", v.Reducer.String(), v.At)
		}
	}
}

// A downsample answers its values when finding its extremes' instants
// would pass maxSamples: two hours of heart rate at 1 Hz are eight rollup
// rows of 900 points, and finding each column's min and max in them reads
// up to 14400 points, so under maxSamples 1000 the read answers from the
// eight rows, its min and max carrying no instant and its first and last
// theirs, and under the default budget every value carries one. A min held
// both by a row the look-up skips (steps at 00:20 the next day) and by a
// later point of the cut end [00:30, 00:37) read raw (00:33) carries no
// instant either, since the row's is the earlier.
func TestAggregateDownsampleAnswersWhereItsInstantsDoNotFit(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	var offsets []int64
	var vals []any
	for s := range 2 * 3600 {
		offsets = append(offsets, int64(s)*1000)
		vals = append(vals, strconv.Itoa(55+(s/60*7)%61))
	}
	f.series("hr/dense", map[string]any{
		"metric": f.metricRef("health/heart_rate"), "source": f.sourceRef("garmin"),
		"form": "gauge", "unit": "/min", "hold": "PT4M", "utcOffsetStart": 0, "utcOffsetEnd": 0,
	}, start, start.Add(2*time.Hour), offsets, vals)
	next := start.AddDate(0, 0, 1)
	for m := range 37 {
		at, value := next.Add(time.Duration(m)*time.Minute), "5"
		switch m {
		case 5:
			value = "9"
		case 20, 33:
			value = "3"
		}
		f.mustPut(t, f.observation, "steps/"+at.Format("1504"), map[string]any{
			"metric": f.metricRef("health/steps"), "source": f.sourceRef("phone"), "form": "amount",
			"value": quantity(value, "{steps}"), "at": at.Format(time.RFC3339), "endsAt": at.Add(time.Minute).Format(time.RFC3339), "utcOffset": 0,
		})
	}
	downsample := func(metric string, from, to time.Time, points int, budget int64) (*substrate.AggregateResult, engine.AggregateTrace) {
		t.Helper()
		a := &substrate.Aggregate{Metric: &substrate.AggregateMetric{Ref: f.metricPath(metric)}, Range: rng(from, to)}
		if budget > 0 {
			a.Budget = &substrate.AggregateBudget{MaxSamples: budget}
		}
		ctx, trace := engine.AggregateTraced(context.Background())
		res, err := f.ds.Aggregate(ctx, substrate.AggregateQuery{Aggregate: a, Downsample: &substrate.Downsample{Points: points}})
		if err != nil {
			t.Fatalf("downsample under maxSamples %d: %v", budget, err)
		}
		return res, trace
	}

	full, _ := downsample("health/heart_rate", start, start.Add(2*time.Hour), 8, 0)
	tight, trace := downsample("health/heart_rate", start, start.Add(2*time.Hour), 8, 1000)
	if trace.RollupRows() != 8 || trace.RawPoints() != 0 {
		t.Fatalf("the tight read took %d rollup rows and %d raw points; want the eight rows alone", trace.RollupRows(), trace.RawPoints())
	}
	if len(tight.Rows) != 8 || len(full.Rows) != 8 {
		t.Fatalf("columns: %d under maxSamples 1000, %d under the default budget; want 8 each", len(tight.Rows), len(full.Rows))
	}
	for i, row := range tight.Rows {
		for _, r := range []string{"first", "last", "min", "max"} {
			got, gotAt := valueAt(row, r)
			want, wantAt := valueAt(full.Rows[i], r)
			if got != want {
				t.Errorf("column %d %s = %s under maxSamples 1000, %s under the default budget", i, r, got, want)
			}
			if wantAt == "" {
				t.Errorf("column %d %s carries no instant under the default budget", i, r)
			}
			switch r {
			case "min", "max":
				if gotAt != "" {
					t.Errorf("column %d %s carries %s under maxSamples 1000; the look-up does not fit, so it carries none", i, r, gotAt)
				}
			default:
				if gotAt != wantAt {
					t.Errorf("column %d %s at %q under maxSamples 1000, want %s", i, r, gotAt, wantAt)
				}
			}
		}
	}

	for _, c := range []struct {
		budget int64
		min    [2]string
		max    [2]string
	}{
		{0, [2]string{"3", "2026-09-02T00:20:00Z"}, [2]string{"9", "2026-09-02T00:05:00Z"}},
		{25, [2]string{"3", ""}, [2]string{"9", ""}},
	} {
		res, trace := downsample("health/steps", next, next.Add(37*time.Minute), 1, c.budget)
		if trace.RollupRows() == 0 || trace.RawPoints() == 0 {
			t.Fatalf("maxSamples %d: the tie took %d rollup rows and %d raw points; want rows and the cut end's points", c.budget, trace.RollupRows(), trace.RawPoints())
		}
		lo, loAt := valueAt(res.Rows[0], "min")
		hi, hiAt := valueAt(res.Rows[0], "max")
		if got := [2][2]string{{lo, loAt}, {hi, hiAt}}; got != [2][2]string{c.min, c.max} {
			t.Errorf("maxSamples %d: min and max %v, want %v and %v", c.budget, got, c.min, c.max)
		}
	}
}
