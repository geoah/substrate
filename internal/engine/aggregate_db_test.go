package engine_test

// The aggregate read against Postgres (decisions 0160 and 0161): every
// worked request of the observations proposal, each answer held against
// internal/observed computed over the raw points the index stores, and a
// rollup-served answer held against the raw-served one.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/observed"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// aggFix is a repository holding the observations and health samples, with
// the helpers the aggregate tests write and read through.
type aggFix struct {
	obsIndex
	t *testing.T
}

func openAgg(t *testing.T) aggFix {
	t.Helper()
	return aggFix{obsIndex: openObservedIndex(t), t: t}
}

func (f aggFix) metricPath(id string) string { return f.metric + "/" + id }

func (f aggFix) sourcePath(id string) string { return f.source + "/" + id }

// series writes one trace: points at `at` plus each offset (milliseconds),
// values or codes as given.
func (f aggFix) series(id string, props map[string]any, at, endsAt time.Time, offsets []int64, vals []any) {
	f.t.Helper()
	p := map[string]any{
		"at": at.Format(time.RFC3339Nano), "endsAt": endsAt.Format(time.RFC3339Nano),
		"offsets": anySlice(offsets), "revision": 1,
	}
	for k, v := range props {
		p[k] = v
	}
	key := "values"
	if _, ok := props["unit"]; !ok {
		key = "codes"
	}
	if _, ok := p[key]; !ok {
		p[key] = vals
	}
	f.mustPut(f.t, f.trace, id, p)
}

func anySlice[T any](in []T) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

// heartRate writes a day of heart rate from a source as four 6-hour traces,
// one reading every step, skipping [gapFrom, gapTo). Each value is a
// function of its instant and the source's bias, so two sources disagree.
func (f aggFix) heartRate(source string, day time.Time, step time.Duration, bias int, gapFrom, gapTo time.Time) {
	f.t.Helper()
	for q := range 4 {
		start := day.Add(time.Duration(q) * 6 * time.Hour)
		var offsets []int64
		var vals []any
		for at := start; at.Before(start.Add(6 * time.Hour)); at = at.Add(step) {
			if !at.Before(gapFrom) && at.Before(gapTo) {
				continue
			}
			offsets = append(offsets, at.Sub(start).Milliseconds())
			m := at.Hour()*60 + at.Minute()
			vals = append(vals, strconv.Itoa(55+bias+(m*7)%61))
		}
		if len(offsets) == 0 {
			continue
		}
		f.series(fmt.Sprintf("hr/%s/%s", source, start.Format("2006-01-02T15")), map[string]any{
			"metric": f.metricRef("health/heart_rate"), "source": f.sourceRef(source),
			"form": "gauge", "unit": "/min", "hold": "PT4M", "utcOffsetStart": 180, "utcOffsetEnd": 180,
		}, start, start.Add(6*time.Hour), offsets, vals)
	}
}

// agg runs one aggregate read.
func (f aggFix) agg(ctx context.Context, filter substrate.Filter, a substrate.Aggregate) *substrate.AggregateResult {
	f.t.Helper()
	res, err := f.ds.Aggregate(ctx, substrate.AggregateQuery{Filter: filter, Aggregate: &a})
	if err != nil {
		f.t.Fatalf("aggregate: %v", err)
	}
	return res
}

// refusedAgg asserts a 422 naming want.
func (f aggFix) refusedAgg(q substrate.AggregateQuery, want string) {
	f.t.Helper()
	_, err := f.ds.Aggregate(context.Background(), q)
	if !errors.Is(err, substrate.ErrValidation) {
		f.t.Fatalf("err = %v, want a validation refusal naming %q", err, want)
	}
	if !strings.Contains(err.Error(), want) {
		f.t.Errorf("err = %v, want it to name %q", err, want)
	}
}

// stream is the stored points of one stream of a metric, the oracle's input.
func (f aggFix) stream(metric, source string) []observed.Point {
	f.t.Helper()
	var out []observed.Point
	for _, sp := range storedPoints(f.t, f.db) {
		if sp.hold[0] == f.metricPath(metric) && (source == "" || sp.hold[2] == f.sourcePath(source)) {
			out = append(out, sp.point)
		}
	}
	return out
}

func rng(from, to time.Time) *substrate.AggregateRange {
	return &substrate.AggregateRange{Gte: from.Format(time.RFC3339), Lt: to.Format(time.RFC3339)}
}

func reducers(names ...string) []substrate.AggregateReducer {
	out := make([]substrate.AggregateReducer, len(names))
	for i, n := range names {
		out[i] = substrate.AggregateReducer{Name: n}
	}
	return out
}

// val is a row's value of a reducer (by its label), and whether it has one.
func val(row substrate.AggregateRow, reducer string) (string, bool) {
	for _, v := range row.Values {
		if v.Reducer.String() == reducer && v.Of == nil {
			if v.Value == nil {
				return "", false
			}
			return *v.Value, true
		}
	}
	return "", false
}

func num(t *testing.T, s string) float64 {
	t.Helper()
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		t.Fatalf("%q is not a number: %v", s, err)
	}
	return v
}

func close9(a, b float64) bool { return math.Abs(a-b) <= 1e-9*math.Max(1, math.Abs(b)) }

// wantNum asserts a row's value against the oracle's.
func wantNum(t *testing.T, label string, row substrate.AggregateRow, reducer string, want float64, ok bool) {
	t.Helper()
	got, has := val(row, reducer)
	if has != ok {
		t.Errorf("%s %s: has value %v, want %v (got %q)", label, reducer, has, ok, got)
		return
	}
	if ok && !close9(num(t, got), want) {
		t.Errorf("%s %s = %s, want %v", label, reducer, got, want)
	}
}

var athens = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Athens")
	if err != nil {
		panic(err)
	}
	return loc
}()

// A heart rate read served from rollups equals the one served from raw
// points and the observed package's reducers over the stored points, bucket
// by bucket, a time-weighted mean across the buckets' boundaries included.
func TestAggregateRollupEqualsRaw(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for d := range 3 {
		f.heartRate("garmin", day.AddDate(0, 0, d), 7*time.Minute, 0, time.Time{}, time.Time{})
	}
	a := substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/heart_rate")},
		Range:    rng(day.Add(6*time.Hour), day.AddDate(0, 0, 3)),
		Bucket:   &substrate.AggregateBucket{Every: "PT6H"},
		Reducers: reducers("count", "avg", "twa", "min", "max", "first", "last", "stddev"),
	}
	ctx, trace := engine.AggregateTraced(context.Background())
	fromRollups := f.agg(ctx, substrate.Filter{}, a)
	if trace.RollupRows() == 0 || trace.RawPoints() != 0 {
		t.Fatalf("the read took %d rollup rows and %d raw points; it should be served from rollups alone", trace.RollupRows(), trace.RawPoints())
	}
	rawCtx, rawTrace := engine.AggregateTraced(engine.AggregateRaw(context.Background()))
	fromRaw := f.agg(rawCtx, substrate.Filter{}, a)
	if rawTrace.RawPoints() == 0 {
		t.Fatal("the raw read took no raw points")
	}
	if len(fromRollups.Rows) != 11 || len(fromRaw.Rows) != 11 {
		t.Fatalf("rows: %d from rollups, %d raw; want 11 six-hour buckets", len(fromRollups.Rows), len(fromRaw.Rows))
	}
	points := f.stream("health/heart_rate", "garmin")
	for i, row := range fromRollups.Rows {
		raw := fromRaw.Rows[i]
		w := observed.Window{From: row.Start, To: row.End}
		label := row.Start.Format(time.RFC3339)
		if c, _ := val(row, "count"); c != strconv.FormatInt(observed.Count(points, w), 10) {
			t.Errorf("%s count = %s, want %d", label, c, observed.Count(points, w))
		}
		avg, ok := observed.Avg(points, w)
		wantNum(t, label, row, "avg", avg, ok)
		wantNum(t, label+" raw", raw, "avg", avg, ok)
		twa, ok := observed.TWA(points, w)
		wantNum(t, label, row, "twa", twa, ok)
		wantNum(t, label+" raw", raw, "twa", twa, ok)
		sd, ok := observed.StdDev(points, w)
		wantNum(t, label, row, "stddev", sd, ok)
		lo, ok := observed.Min(points, w)
		wantNum(t, label, row, "min", lo, ok)
		first, _ := observed.First(points, w)
		last, _ := observed.Last(points, w)
		if v, _ := val(row, "first"); v != first.Exact {
			t.Errorf("%s first = %s, want %s", label, v, first.Exact)
		}
		if v, _ := val(raw, "last"); v != last.Exact {
			t.Errorf("%s raw last = %s, want %s", label, v, last.Exact)
		}
		if row.Coverage != raw.Coverage {
			t.Errorf("%s coverage %s from rollups, %s raw", label, row.Coverage, raw.Coverage)
		}
		if row.Form != "gauge" || row.From != "raw" || len(row.Sources) != 1 || row.Sources[0] != f.sourcePath("garmin") {
			t.Errorf("%s row = %+v", label, row)
		}
	}
	if fromRollups.Exact {
		t.Error("an avg is a float reduction; the read said it was exact")
	}
	if fromRollups.Head.Changelog == 0 || fromRollups.Head.Generation == "" {
		t.Errorf("head = %+v", fromRollups.Head)
	}
}

// steps writes hourly-ish step amounts for one local day at +03:00 from a
// source: one point every step from the day's start, `hours` hours of them.
func (f aggFix) steps(source string, day time.Time, step time.Duration, hours, rev, scale int) {
	f.t.Helper()
	start := day
	var offsets []int64
	var vals []any
	for at := start; at.Before(start.Add(time.Duration(hours) * time.Hour)); at = at.Add(step) {
		offsets = append(offsets, at.Sub(start).Milliseconds())
		vals = append(vals, strconv.Itoa(scale*(10+(at.Hour()*13+at.Minute())%97)))
	}
	f.series(fmt.Sprintf("steps/%s/%s", source, day.Format("2006-01-02")), map[string]any{
		"metric": f.metricRef("health/steps"), "source": f.sourceRef(source),
		"form": "amount", "unit": "{steps}", "utcOffsetStart": 180, "utcOffsetEnd": 180, "revision": rev,
	}, start, start.Add(time.Duration(hours)*time.Hour), offsets, vals)
}

// A read whose range and buckets cut rollup rows merges the whole rows with
// raw points at the cut ends, and equals the raw read and the specification:
// as written, after a correction at a higher revision, after a deletion and
// after a rebuild.
func TestAggregateRollupEqualsRawAfterCorrectionDeletionAndRebuild(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.FixedZone("", 3*3600))
	for d := range 3 {
		f.steps("garmin", day.AddDate(0, 0, d), 5*time.Minute, 24, 1, 1)
	}
	a := substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/steps")},
		Range:    rng(day.Add(67*time.Minute), day.AddDate(0, 0, 3).Add(-8*time.Minute)),
		Bucket:   &substrate.AggregateBucket{Every: "PT6H", Origin: day.UTC().Format(time.RFC3339)},
		Reducers: reducers("count", "sum", "avg", "min", "max", "first", "last"),
		Unit:     "{steps}",
	}
	check := func(stage string) {
		t.Helper()
		ctx, trace := engine.AggregateTraced(context.Background())
		fromRollups := f.agg(ctx, substrate.Filter{}, a)
		if trace.RollupRows() == 0 || trace.RawPoints() == 0 {
			t.Fatalf("%s: the read took %d rollup rows and %d raw points; the cut ends should be raw and the rest rollups",
				stage, trace.RollupRows(), trace.RawPoints())
		}
		fromRaw := f.agg(engine.AggregateRaw(context.Background()), substrate.Filter{}, a)
		if len(fromRollups.Rows) != len(fromRaw.Rows) || len(fromRollups.Rows) == 0 {
			t.Fatalf("%s: %d rows from rollups, %d raw", stage, len(fromRollups.Rows), len(fromRaw.Rows))
		}
		points := f.stream("health/steps", "garmin")
		for i, row := range fromRollups.Rows {
			w := observed.Window{From: row.Start, To: row.End}
			label := stage + " " + row.Start.Format(time.RFC3339)
			for _, x := range []substrate.AggregateRow{row, fromRaw.Rows[i]} {
				if c, _ := val(x, "count"); c != strconv.FormatInt(observed.Count(points, w), 10) {
					t.Errorf("%s count = %s, want %d", label, c, observed.Count(points, w))
				}
				sum, ok := observed.Sum(points, w)
				wantNum(t, label, x, "sum", sum, ok)
				avg, ok := observed.Avg(points, w)
				wantNum(t, label, x, "avg", avg, ok)
				hi, ok := observed.Max(points, w)
				wantNum(t, label, x, "max", hi, ok)
				first, _ := observed.First(points, w)
				last, _ := observed.Last(points, w)
				if v, _ := val(x, "first"); v != first.Exact {
					t.Errorf("%s first = %s, want %s", label, v, first.Exact)
				}
				if v, _ := val(x, "last"); v != last.Exact {
					t.Errorf("%s last = %s, want %s", label, v, last.Exact)
				}
			}
			if row.Coverage != fromRaw.Rows[i].Coverage {
				t.Errorf("%s coverage %s from rollups, %s raw", label, row.Coverage, fromRaw.Rows[i].Coverage)
			}
		}
	}
	check("as written")
	f.steps("garmin", day.AddDate(0, 0, 1), 5*time.Minute, 24, 2, 3)
	check("after a correction")
	if _, err := f.ds.Delete(context.Background(), owner, f.trace, "steps/garmin/"+day.Format("2006-01-02"), substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	check("after a deletion")
	if _, err := f.svc.(rebuilder).RebuildRepository(context.Background(), testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	check("after a rebuild")
}

// A maxGap below a bucket's largest gap means its rows cannot say what the
// request's holds were, so the read takes raw points and still answers what
// the specification answers under that maxGap.
func TestAggregateTwaFallsBackToRawUnderATightMaxGap(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	// Held for an hour, readings every 7 minutes with a 3-hour gap.
	for d := range 2 {
		f.heartRate("strap", day.AddDate(0, 0, d), 7*time.Minute, 0, day.Add(9*time.Hour), day.Add(12*time.Hour))
	}
	if _, err := f.patch(t, f.trace, "hr/strap/2026-09-01T06", map[string]any{"hold": "PT1H"}); err != nil {
		t.Fatalf("patch the hold: %v", err)
	}
	points := f.stream("health/heart_rate", "strap")
	for _, maxGap := range []string{"", "PT5M", "PT30M"} {
		a := substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/heart_rate")},
			Range:    rng(day, day.AddDate(0, 0, 2)),
			Bucket:   &substrate.AggregateBucket{Calendar: "day", Timezone: "UTC"},
			Reducers: reducers("twa", "count"),
			MaxGap:   maxGap,
		}
		ctx, trace := engine.AggregateTraced(context.Background())
		res := f.agg(ctx, substrate.Filter{}, a)
		if maxGap == "PT5M" && trace.RawPoints() == 0 {
			t.Errorf("maxGap %s is below the rows' gaps, yet the read took no raw point", maxGap)
		}
		if maxGap == "" && trace.RawPoints() != 0 {
			t.Errorf("with no maxGap the rows answer, yet the read took %d raw points", trace.RawPoints())
		}
		var gap time.Duration
		if maxGap != "" {
			gap, _ = time.ParseDuration(strings.ToLower(strings.TrimPrefix(maxGap, "PT")))
		}
		if len(res.Rows) != 2 {
			t.Fatalf("maxGap %q: %d rows", maxGap, len(res.Rows))
		}
		for _, row := range res.Rows {
			twa, ok := observed.TWA(points, observed.Window{From: row.Start, To: row.End, MaxGap: gap})
			wantNum(t, "maxGap "+maxGap+" "+row.Start.Format(time.RFC3339), row, "twa", twa, ok)
			held := observed.HeldMs(observed.Spans(points, row.Start, row.End, gap))
			if row.Coverage != vocabularyDuration(held) {
				t.Errorf("maxGap %q %s coverage = %s, want %s", maxGap, row.Start, row.Coverage, vocabularyDuration(held))
			}
		}
	}
}

// Six months of heart rate zones, weekly in Europe/Athens, with the chest
// strap preferred over the watch: each week's five durations are the
// specification's durationIn over the priority-resolved holds, in minutes,
// and sum to the week's coverage where every reading falls in a zone.
func TestAggregateHeartRateZonesWeekly(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	monday := time.Date(2026, 4, 6, 0, 0, 0, 0, athens)
	for w := range 26 {
		run := monday.AddDate(0, 0, 7*w+2).Add(7 * time.Hour)
		for _, src := range []struct {
			id         string
			bias       int
			from, till time.Duration
		}{{"strap", 40, 20 * time.Minute, 70 * time.Minute}, {"watch", 30, 0, 90 * time.Minute}} {
			var offsets []int64
			var vals []any
			for at := src.from; at < src.till; at += time.Minute {
				offsets = append(offsets, (at - src.from).Milliseconds())
				vals = append(vals, strconv.Itoa(95+src.bias+int(at/time.Minute)%50))
			}
			start := run.Add(src.from)
			f.series(fmt.Sprintf("hr/%s/%s", src.id, start.UTC().Format("2006-01-02T15:04")), map[string]any{
				"metric": f.metricRef("health/heart_rate"), "source": f.sourceRef(src.id),
				"form": "gauge", "unit": "/min", "hold": "PT2M", "utcOffsetStart": 180, "utcOffsetEnd": 180,
			}, start, run.Add(src.till), offsets, vals)
		}
	}
	zones := [][2]float64{{95, 114}, {114, 133}, {133, 152}, {152, 171}, {171, 220}}
	var reds []substrate.AggregateReducer
	for _, z := range zones {
		reds = append(reds, substrate.AggregateReducer{Name: "durationIn", Between: []float64{z[0], z[1]}})
	}
	res := f.agg(context.Background(), substrate.Filter{Kinds: []string{f.trace}}, substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/heart_rate")},
		From:     "raw",
		Range:    rng(monday, monday.AddDate(0, 0, 7*26)),
		Bucket:   &substrate.AggregateBucket{Calendar: "week", WeekStart: "monday", Timezone: "Europe/Athens"},
		Reducers: reds,
		MaxGap:   "PT5M",
		Sources:  &substrate.AggregateSources{Policy: "priority", Order: []string{"strap", "watch"}},
		Unit:     "min",
		Budget:   &substrate.AggregateBudget{MaxSamples: 5000000},
	})
	if len(res.Rows) != 26 {
		t.Fatalf("%d rows, want 26 weeks", len(res.Rows))
	}
	strap, watch := f.stream("health/heart_rate", "strap"), f.stream("health/heart_rate", "watch")
	for _, row := range res.Rows {
		if row.Start.Weekday() != time.Monday || row.Start.Hour() != 0 {
			t.Errorf("a week starts %s", row.Start)
		}
		resolved, err := observed.Prioritize(context.Background(), [][]observed.Span{
			observed.Spans(strap, row.Start, row.End, 5*time.Minute), observed.Spans(watch, row.Start, row.End, 5*time.Minute),
		})
		if err != nil {
			t.Fatal(err)
		}
		all := append(append([]observed.Span(nil), resolved[0]...), resolved[1]...)
		held := time.Duration(observed.HeldMs(all)) * time.Millisecond
		var total float64
		for i, z := range zones {
			want := observed.DurationInOf(all, z[0], z[1]).Minutes()
			got := num(t, *row.Values[i].Value)
			total += got
			if !close9(got, want) || row.Values[i].Unit != "min" {
				t.Errorf("%s zone %v = %v %s, want %v min", row.Start.Format("2006-01-02"), z, got, row.Values[i].Unit, want)
			}
		}
		if !close9(total, held.Minutes()) || held < 90*time.Minute {
			t.Errorf("%s zones add to %v min; every reading is in a zone, so they are the %v the resolved holds cover", row.Start.Format("2006-01-02"), total, held)
		}
		if len(row.Sources) != 2 {
			t.Errorf("%s sources = %v, want both", row.Start.Format("2006-01-02"), row.Sources)
		}
	}
}

// One total at five-minute buckets and at a daily bucket: the priority is
// resolved per stored interval before the range is cut, so the held time and
// a zone's duration do not change with the bucket width.
func TestAggregatePriorityResolvesBeforeBucketing(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	f.heartRate("strap", day, 7*time.Minute, 20, day.Add(3*time.Hour), day.Add(6*time.Hour))
	f.heartRate("watch", day, 3*time.Minute, 0, time.Time{}, time.Time{})
	totals := map[string][2]float64{}
	for _, every := range []string{"PT5M", "P1D"} {
		res := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/heart_rate")},
			Range:    rng(day, day.AddDate(0, 0, 1)),
			Bucket:   &substrate.AggregateBucket{Every: every},
			Reducers: []substrate.AggregateReducer{{Name: "durationIn", Between: []float64{90, 120}}, {Name: "count"}},
			Sources:  &substrate.AggregateSources{Policy: "priority", Order: []string{"strap"}},
		})
		var dur, count float64
		for _, row := range res.Rows {
			dur += num(t, *row.Values[0].Value)
			count += num(t, *row.Values[1].Value)
		}
		totals[every] = [2]float64{dur, count}
	}
	if totals["PT5M"] != totals["P1D"] || totals["P1D"][0] == 0 {
		t.Errorf("five-minute buckets add to %v, the day is %v", totals["PT5M"], totals["P1D"])
	}
	// The watch fills only what the strap does not hold: separate counts both.
	sep := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/heart_rate")},
		Range:    rng(day, day.AddDate(0, 0, 1)),
		Reducers: reducers("count"),
	})
	if len(sep.Rows) != 2 {
		t.Fatalf("separate: %d rows, want one per source", len(sep.Rows))
	}
	both := num(t, *sep.Rows[0].Values[0].Value) + num(t, *sep.Rows[1].Values[0].Value)
	if !(totals["P1D"][1] < both) {
		t.Errorf("priority counted %v points, separate %v; the strap's hours should drop the watch's", totals["P1D"][1], both)
	}
}

// prefer-raw sums the step chunks where a local day is covered and takes the
// vendor's daily total on a thin day, saying which per row; a bucket never
// mixes the two, and from raw and from summaries read one each.
func TestAggregateNeverMixesRawAndSummariesInABucket(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.FixedZone("", 3*3600))
	f.steps("garmin", day, time.Hour, 24, 1, 1)
	f.steps("garmin", day.AddDate(0, 0, 1), time.Hour, 3, 1, 1)
	f.steps("garmin", day.AddDate(0, 0, 2), time.Hour, 24, 1, 1)
	totals := []string{"9001", "8002", "7003", "6004"}
	for d, total := range totals {
		at := day.AddDate(0, 0, d)
		f.mustPut(t, f.observation, "garmin/daily/"+at.Format("2006-01-02"), map[string]any{
			"metric": f.metricRef("health/steps"), "source": f.sourceRef("garmin"), "form": "summary",
			"statistic": "sum", "period": "day", "value": quantity(total, "{steps}"),
			"at": at.Format(time.RFC3339), "endsAt": at.AddDate(0, 0, 1).Format(time.RFC3339), "utcOffset": 180,
		})
	}
	read := func(from string) *substrate.AggregateResult {
		return f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/steps")},
			From:     from,
			Range:    rng(day, day.AddDate(0, 0, 4)),
			Bucket:   &substrate.AggregateBucket{Calendar: "day"},
			Reducers: reducers("sum"),
			Unit:     "{steps}",
		})
	}
	points := f.stream("health/steps", "garmin")
	rawSum := func(d int) float64 {
		s, _ := observed.Sum(points, observed.Window{From: day.AddDate(0, 0, d), To: day.AddDate(0, 0, d+1)})
		return s
	}
	res := read("prefer-raw")
	if len(res.Rows) != 4 {
		t.Fatalf("prefer-raw: %d rows, want 4 days", len(res.Rows))
	}
	for d, want := range []struct {
		from, form string
		sum        float64
	}{{"raw", "amount", rawSum(0)}, {"summaries", "summary", 8002}, {"raw", "amount", rawSum(2)}, {"summaries", "summary", 6004}} {
		row := res.Rows[d]
		if row.From != want.from || row.Form != want.form {
			t.Errorf("day %d: from %s form %s, want %s %s", d, row.From, row.Form, want.from, want.form)
		}
		wantNum(t, fmt.Sprintf("day %d", d), row, "sum", want.sum, true)
		if got := row.Start.Format(time.RFC3339); got != day.AddDate(0, 0, d).Format(time.RFC3339) {
			t.Errorf("day %d starts %s", d, got)
		}
	}
	raw := read("raw")
	if len(raw.Rows) != 3 || raw.Rows[1].From != "raw" {
		t.Fatalf("from raw: %+v", raw.Rows)
	}
	wantNum(t, "raw thin day", raw.Rows[1], "sum", rawSum(1), true)
	sums := read("summaries")
	if len(sums.Rows) != 4 {
		t.Fatalf("from summaries: %d rows", len(sums.Rows))
	}
	for d, total := range totals {
		if v, _ := val(sums.Rows[d], "sum"); v != total || sums.Rows[d].From != "summaries" {
			t.Errorf("summaries day %d = %s from %s, want %s", d, v, sums.Rows[d].From, total)
		}
	}
}

// The current value of every stream, grouped by metric and source, comes from
// observed_latest: the newest weighing per stream with its count, the start
// at its own offset, a bounded lab value counted as bounded, and a unit
// converted on the way out.
func TestAggregateLatestRead(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	day := time.Date(2026, 10, 1, 7, 0, 0, 0, time.FixedZone("", 3*3600))
	for d := range 5 {
		at := day.AddDate(0, 0, d)
		f.mustPut(t, f.observation, "withings/"+at.Format("2006-01-02")+"/weight", map[string]any{
			"metric": f.metricRef("health/body_mass"), "source": f.sourceRef("withings"), "form": "gauge",
			"value": quantity(fmt.Sprintf("72.%d0", d), "kg"), "at": at.Format(time.RFC3339), "utcOffset": 180,
		})
		f.mustPut(t, f.observation, "withings/"+at.Format("2006-01-02")+"/fat", map[string]any{
			"metric": f.metricRef("health/body_fat_fraction"), "source": f.sourceRef("withings"), "form": "gauge",
			"value": quantity(fmt.Sprintf("18.%d", d), "%"), "at": at.Format(time.RFC3339), "utcOffset": 180,
		})
	}
	f.mustPut(t, f.labresult, "lab/tsh", map[string]any{
		"metric": f.metricRef("health/tsh"), "form": "gauge", "hold": "PT0S",
		"value": map[string]any{"value": "0.01", "unit": "m[IU]/L", "comparator": "<"},
		"at":    "2026-09-14T08:05:00+01:00", "utcOffset": 60,
	})
	res := f.agg(context.Background(), substrate.Filter{Implements: "substrate.reamde.dev/core/observed"},
		substrate.Aggregate{GroupBy: []string{"metric", "source"}, Reducers: reducers("last")})
	if len(res.Rows) != 3 {
		t.Fatalf("%d rows, want three streams", len(res.Rows))
	}
	byMetric := map[string]substrate.AggregateRow{}
	for _, row := range res.Rows {
		byMetric[row.Group.Metric] = row
	}
	weight := byMetric[f.metricPath("health/body_mass")]
	if v, _ := val(weight, "last"); v != "72.40" || weight.Values[0].Unit != "kg" || weight.Count != 5 ||
		weight.Start.Format(time.RFC3339) != "2026-10-05T07:00:00+03:00" || weight.Group.Source != f.sourcePath("withings") {
		t.Errorf("the weight row = %+v", weight)
	}
	if tsh := byMetric[f.metricPath("health/tsh")]; tsh.Bounded != 1 || tsh.Group.Source != "" {
		t.Errorf("the lab row = %+v", tsh)
	}
	lb := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
		Metric:  &substrate.AggregateMetric{Ref: f.metricPath("health/body_mass")},
		GroupBy: []string{"source"}, Reducers: reducers("last"), Unit: "[lb_av]",
	})
	if len(lb.Rows) != 1 || lb.Rows[0].Values[0].Unit != "[lb_av]" {
		t.Fatalf("in pounds: %+v", lb.Rows)
	}
	if v, _ := val(lb.Rows[0], "last"); !close9(num(t, v), 72.40/0.45359237) {
		t.Errorf("72.40 kg is %s [lb_av]", v)
	}
	// A bucketed read over the lab metric counts the bounded value as bounded.
	labs := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
		Metric: &substrate.AggregateMetric{Ref: f.metricPath("health/tsh")},
		Range:  rng(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)),
		Bucket: &substrate.AggregateBucket{Calendar: "year", Timezone: "UTC"}, Reducers: reducers("count"),
	})
	if len(labs.Rows) != 1 || labs.Rows[0].Bounded != 1 || labs.Rows[0].Count != 1 {
		t.Errorf("the yearly lab row = %+v", labs.Rows)
	}
}

// Sleep stages per night: coded amounts summed by code inside each sleep
// record's window, the record's reference and version on the row, the rows
// marked non-additive, and the whoop's nights preferred over the garmin's.
func TestAggregateSleepStagesByRecordWindows(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	home := strings.TrimSuffix(f.observation, "/observations/observation")
	sleepKind := home + "/health/sleep"
	night := time.Date(2026, 9, 1, 23, 0, 0, 0, time.FixedZone("", 3*3600))
	stages := []struct {
		code string
		min  int
	}{{"light", 60}, {"deep", 45}, {"rem", 30}, {"awake", 15}, {"light", 90}}
	for n := range 3 {
		start := night.AddDate(0, 0, n)
		end := start.Add(4 * time.Hour)
		f.mustPut(t, sleepKind, fmt.Sprintf("whoop/%d", n), map[string]any{
			"at": start.Format(time.RFC3339), "endsAt": end.Format(time.RFC3339),
		})
		for _, src := range []string{"whoop", "garmin"} {
			at := start
			for i, s := range stages {
				f.mustPut(t, f.observation, fmt.Sprintf("%s/%d/stage/%d", src, n, i), map[string]any{
					"metric": f.metricRef("health/sleep_stage"), "source": f.sourceRef(src), "form": "amount",
					"value": quantity(strconv.Itoa(s.min), "min"), "code": s.code,
					"at": at.Format(time.RFC3339), "endsAt": at.Add(time.Duration(s.min) * time.Minute).Format(time.RFC3339),
					"utcOffset": 180,
				})
				at = at.Add(time.Duration(s.min) * time.Minute)
			}
		}
	}
	res := f.agg(context.Background(), substrate.Filter{Kinds: []string{f.observation}}, substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/sleep_stage")},
		From:     "raw",
		Range:    rng(night.Add(-time.Hour), night.AddDate(0, 0, 3)),
		Bucket:   &substrate.AggregateBucket{Records: &substrate.AggregateRecords{Filter: substrate.Filter{Kinds: []string{sleepKind}}}},
		GroupBy:  []string{"code"},
		Reducers: reducers("sum"),
		Sources:  &substrate.AggregateSources{Policy: "priority", Order: []string{"whoop", "garmin"}},
		Unit:     "min",
	})
	if len(res.Rows) != 12 {
		t.Fatalf("%d rows, want four codes on three nights", len(res.Rows))
	}
	want := map[string]string{"light": "150", "deep": "45", "rem": "30", "awake": "15"}
	for _, row := range res.Rows {
		if row.Record == nil || !strings.HasPrefix(row.Record.Ref, sleepKind+"/whoop/") || row.Record.Version != 1 || !row.NonAdditive {
			t.Errorf("row %+v", row)
			continue
		}
		if v, _ := val(row, "sum"); v != want[row.Group.Code] || row.Values[0].Unit != "min" {
			t.Errorf("%s %s = %s %s, want %s min", row.Record.Ref, row.Group.Code, v, row.Values[0].Unit, want[row.Group.Code])
		}
		if len(row.Sources) != 1 || row.Sources[0] != f.sourcePath("whoop") {
			t.Errorf("%s %s sources = %v, want the whoop alone", row.Record.Ref, row.Group.Code, row.Sources)
		}
	}
	// Without the priority both devices' nights add.
	sep := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
		Metric:  &substrate.AggregateMetric{Ref: f.metricPath("health/sleep_stage")},
		Range:   rng(night.Add(-time.Hour), night.AddDate(0, 0, 3)),
		Bucket:  &substrate.AggregateBucket{Records: &substrate.AggregateRecords{Filter: substrate.Filter{Kinds: []string{sleepKind}}}},
		GroupBy: []string{"code"}, Reducers: reducers("sum"), Unit: "h",
	})
	if len(sep.Rows) != 24 {
		t.Errorf("separate: %d rows, want one per code, night and source", len(sep.Rows))
	}
}

// kWh per day from a meter that resets: the increase steps across the reset
// and across the day's boundary, and the rate is that increase over the time its
// steps span.
func TestAggregateCounterIncreaseAndRateAcrossAReset(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, athens)
	vals := []any{"18342.655", "18343.100", "18344.000", "0.250", "1.000", "1.500", "2.125", "3.000"}
	var offsets []int64
	for i := range vals {
		offsets = append(offsets, int64(i)*int64(6*time.Hour/time.Millisecond))
	}
	f.series("meter/1", map[string]any{
		"metric": f.metricRef("energy"), "source": f.sourceRef("meter"), "form": "counter", "unit": "kW.h",
		"resetRule": "monotonic", "utcOffsetStart": 180, "utcOffsetEnd": 180,
	}, start, start.Add(48*time.Hour), offsets, vals)
	points := f.stream("energy", "meter")
	for _, unit := range []string{"kW.h", "W.h"} {
		res := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("energy")},
			Range:    rng(start, start.AddDate(0, 0, 2)),
			Bucket:   &substrate.AggregateBucket{Calendar: "day", Timezone: "Europe/Athens"},
			Reducers: reducers("increase", "rate", "first", "last"),
			Unit:     unit,
		})
		if len(res.Rows) != 2 {
			t.Fatalf("%s: %d rows", unit, len(res.Rows))
		}
		for _, row := range res.Rows {
			inc, _, covered, _ := observed.Increase(points, observed.Window{From: row.Start, To: row.End})
			want, _ := inc.Float64()
			if unit == "W.h" {
				want *= 1000
			}
			wantNum(t, unit+" "+row.Start.Format("01-02"), row, "increase", want, true)
			wantNum(t, unit+" "+row.Start.Format("01-02"), row, "rate", want/covered.Seconds(), true)
			if row.Form != "counter" {
				t.Errorf("form %s", row.Form)
			}
		}
		if v, _ := val(res.Rows[0], "increase"); unit == "kW.h" && v != "1.595" {
			t.Errorf("day one's increase = %s kW.h, want 1.595 (0.445, 0.9 and the 0.25 after the reset)", v)
		}
	}
}

// Hours the door was open per week: the intervals are built from every
// transition before the code filters them, so `closed` ends the `open` before
// it, and a door shut over a weekend is not open until Monday.
func TestAggregateTimeInStateBuildsIntervalsBeforeFiltering(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	monday := time.Date(2026, 7, 6, 0, 0, 0, 0, athens)
	var points []struct {
		at   time.Time
		code string
	}
	for d := range 14 {
		day := monday.AddDate(0, 0, d)
		points = append(points,
			struct {
				at   time.Time
				code string
			}{day.Add(7 * time.Hour), "closed"},
			struct {
				at   time.Time
				code string
			}{day.Add(8*time.Hour + 13*time.Minute), "open"},
			struct {
				at   time.Time
				code string
			}{day.Add(8*time.Hour + 41*time.Minute), "closed"})
	}
	for i, p := range points {
		f.mustPut(t, f.observation, fmt.Sprintf("door/%d", i), map[string]any{
			"metric": f.metricRef("air_temperature"), "about": map[string]any{"ref": f.sourcePath("front-door")},
			"source": f.sourceRef("contact"), "form": "gauge", "code": p.code,
			"at": p.at.Format(time.RFC3339), "utcOffset": 180,
		})
	}
	stream := f.stream("air_temperature", "contact")
	for _, filter := range []substrate.Filter{{}, {Properties: map[string]substrate.Cond{"code": {Eq: "open"}}}} {
		res := f.agg(context.Background(), filter, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("air_temperature")},
			Range:    rng(monday, monday.AddDate(0, 0, 14)),
			Bucket:   &substrate.AggregateBucket{Calendar: "week", WeekStart: "monday", Timezone: "Europe/Athens"},
			Reducers: []substrate.AggregateReducer{{Name: "timeInState", States: []string{"open"}}, {Name: "count"}},
			MaxGap:   "P1D",
			Unit:     "h",
		})
		if len(res.Rows) != 2 {
			t.Fatalf("%d rows", len(res.Rows))
		}
		for _, row := range res.Rows {
			want := observed.TimeInState(stream, observed.Window{From: row.Start, To: row.End, MaxGap: 24 * time.Hour})["open"]
			if got := num(t, *row.Values[0].Value); !close9(got, want.Hours()) || row.Values[0].Unit != "h" || row.Values[0].Code != "open" {
				t.Errorf("%s open = %v %s, want %v h", row.Start.Format("01-02"), got, row.Values[0].Unit, want.Hours())
			}
			if want.Hours() != 7*28.0/60 {
				t.Errorf("the specification says %v h open; 28 minutes a day is %v", want.Hours(), 7*28.0/60)
			}
		}
	}
	// Every state the stream holds, one value each, from the bare word.
	all := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("air_temperature")},
		Range:    rng(monday, monday.AddDate(0, 0, 7)),
		Bucket:   &substrate.AggregateBucket{Calendar: "week", Timezone: "Europe/Athens"},
		Reducers: reducers("timeInState"), MaxGap: "P1D", Unit: "min",
	})
	if len(all.Rows) != 1 || len(all.Rows[0].Values) != 2 || all.Rows[0].Values[0].Code != "closed" || *all.Rows[0].Values[1].Value != "196" {
		t.Errorf("every state: %+v", all.Rows)
	}
}

// The circular mean of 350 and 10 degrees is north, not 180.
func TestAggregateCircularMeanAcrossNorth(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	f.mustPut(t, f.metric, "wind_direction", map[string]any{"name": "Wind direction", "measures": "angle", "unit": "deg", "form": "gauge"})
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	for i, v := range []string{"350", "10", "355", "5"} {
		f.mustPut(t, f.observation, fmt.Sprintf("wind/%d", i), map[string]any{
			"metric": f.metricRef("wind_direction"), "source": f.sourceRef("vane"), "form": "gauge", "circular": true,
			"value": quantity(v, "deg"), "at": at.Add(time.Duration(i) * time.Minute).Format(time.RFC3339),
		})
	}
	for _, ctx := range []context.Context{context.Background(), engine.AggregateRaw(context.Background())} {
		res := f.agg(ctx, substrate.Filter{}, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("wind_direction")},
			Range:    rng(at.Add(-time.Hour), at.Add(time.Hour)),
			Reducers: reducers("circularMean", "avg"),
		})
		if len(res.Rows) != 1 {
			t.Fatalf("%d rows", len(res.Rows))
		}
		v, _ := val(res.Rows[0], "circularMean")
		if got := num(t, v); math.Min(got, 360-got) > 1e-9 || res.Rows[0].Values[0].Unit != "deg" {
			t.Errorf("circular mean = %s %s, want north", v, res.Rows[0].Values[0].Unit)
		}
		if avg, _ := val(res.Rows[0], "avg"); avg != "180" {
			t.Errorf("the arithmetic mean is %s; it should be the 180 the circular mean avoids", avg)
		}
	}
}

// The recorded-local day: a traveller's points are placed by their own
// offsets, so the day of the flight is 31 hours from +03:00 to -04:00 and its
// start and end say so; the counts partition the points.
func TestAggregateRecordedLocalDayAcrossAnOffsetChange(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	athensDay := time.Date(2026, 9, 1, 0, 0, 0, 0, time.FixedZone("", 3*3600))
	put := func(i int, at time.Time, off int) {
		f.mustPut(t, f.observation, fmt.Sprintf("travel/%d", i), map[string]any{
			"metric": f.metricRef("health/body_mass"), "source": f.sourceRef("scale"), "form": "gauge",
			"value": quantity(strconv.Itoa(70+i), "kg"), "at": at.Format(time.RFC3339), "utcOffset": off, "hold": "PT0S",
		})
	}
	put(0, athensDay.Add(1*time.Hour), 180)  // Sep 1 01:00 Athens
	put(1, athensDay.Add(10*time.Hour), 180) // Sep 1 10:00 Athens, before the flight
	// After landing in New York: 2026-09-02T02:00Z is Sep 1 22:00 at -04:00.
	put(2, time.Date(2026, 9, 2, 2, 0, 0, 0, time.UTC), -240)
	put(3, time.Date(2026, 9, 2, 14, 0, 0, 0, time.UTC), -240) // Sep 2 10:00 New York
	res := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/body_mass")},
		Range:    rng(athensDay, athensDay.AddDate(0, 0, 3)),
		Bucket:   &substrate.AggregateBucket{Calendar: "day"},
		Reducers: reducers("count", "first", "last"),
	})
	if len(res.Rows) != 2 {
		t.Fatalf("%d rows: %+v", len(res.Rows), res.Rows)
	}
	first, second := res.Rows[0], res.Rows[1]
	if c, _ := val(first, "count"); c != "3" || first.Start.Format(time.RFC3339) != "2026-09-01T00:00:00+03:00" ||
		first.End.Format(time.RFC3339) != "2026-09-02T00:00:00-04:00" {
		t.Errorf("the flight day: count %s from %s to %s", c, first.Start.Format(time.RFC3339), first.End.Format(time.RFC3339))
	}
	if h := first.End.Sub(first.Start).Hours(); h != 31 {
		t.Errorf("the flight day is %v hours, want 31", h)
	}
	if c, _ := val(second, "count"); c != "1" || second.Start.Format(time.RFC3339) != "2026-09-02T00:00:00-04:00" {
		t.Errorf("the next day: count %s from %s", c, second.Start.Format(time.RFC3339))
	}
}

// A day in Europe/Athens across the end of summer time is 25 hours, and the
// row's start and end carry the two offsets.
func TestAggregateAthensDayAcrossTheClockChange(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	day := time.Date(2026, 10, 25, 0, 0, 0, 0, athens)
	f.steps("garmin", day, time.Hour, 24, 1, 1)
	res := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/steps")},
		Range:    rng(day, time.Date(2026, 10, 27, 0, 0, 0, 0, athens)),
		Bucket:   &substrate.AggregateBucket{Calendar: "day", Timezone: "Europe/Athens"},
		Reducers: reducers("count", "sum"), Fill: "zero",
	})
	if len(res.Rows) != 2 {
		t.Fatalf("%d rows", len(res.Rows))
	}
	row := res.Rows[0]
	if row.Start.Format(time.RFC3339) != "2026-10-25T00:00:00+03:00" || row.End.Format(time.RFC3339) != "2026-10-26T00:00:00+02:00" {
		t.Errorf("the day runs %s to %s", row.Start.Format(time.RFC3339), row.End.Format(time.RFC3339))
	}
	if h := row.End.Sub(row.Start).Hours(); h != 25 {
		t.Errorf("the day is %v hours, want 25", h)
	}
	points := f.stream("health/steps", "garmin")
	if c, _ := val(row, "count"); c != strconv.FormatInt(observed.Count(points, observed.Window{From: row.Start, To: row.End}), 10) {
		t.Errorf("count = %s", c)
	}
	if !res.Rows[1].Synthetic {
		t.Errorf("the empty day was not filled: %+v", res.Rows[1])
	}
}

// fill makes the empty days rows marked synthetic (null, zero or the last
// value carried), and rolling averages over buckets, filled ones included.
func TestAggregateFillAndRolling(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	day := time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC)
	weights := map[int]string{0: "70", 1: "71", 3: "73", 4: "74"}
	for d, w := range weights {
		f.mustPut(t, f.observation, fmt.Sprintf("w/%d", d), map[string]any{
			"metric": f.metricRef("health/body_mass"), "source": f.sourceRef("scale"), "form": "gauge",
			"value": quantity(w, "kg"), "at": day.AddDate(0, 0, d).Format(time.RFC3339), "hold": "PT0S",
		})
	}
	read := func(fill string) *substrate.AggregateResult {
		return f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/body_mass")},
			Range:    rng(day.Add(-7*time.Hour), day.AddDate(0, 0, 5).Add(-7*time.Hour)),
			Bucket:   &substrate.AggregateBucket{Calendar: "day", Timezone: "UTC"},
			Reducers: reducers("avg"), Fill: fill,
			Rolling: &substrate.AggregateRolling{Buckets: 2, Reducers: reducers("avg")},
		})
	}
	if res := read(""); len(res.Rows) != 4 {
		t.Errorf("unfilled: %d rows, want the four days with a weighing", len(res.Rows))
	}
	for _, x := range []struct {
		fill        string
		gap         string
		gapNull     bool
		rollingNext string
	}{{"null", "", true, "73"}, {"zero", "0", false, "36.5"}, {"locf", "71", false, "72"}} {
		res := read(x.fill)
		if len(res.Rows) != 5 {
			t.Fatalf("fill %s: %d rows", x.fill, len(res.Rows))
		}
		gap := res.Rows[2]
		v, has := val(gap, "avg")
		if !gap.Synthetic || has == x.gapNull || (has && v != x.gap) {
			t.Errorf("fill %s: the empty day = %+v", x.fill, gap)
		}
		var rolled string
		for _, rv := range res.Rows[3].Values {
			if rv.Of != nil && rv.Rolling == 2 && rv.Value != nil {
				rolled = *rv.Value
			}
		}
		if rolled != x.rollingNext {
			t.Errorf("fill %s: the rolling mean after the gap = %q, want %s", x.fill, rolled, x.rollingNext)
		}
	}
}

// A downsample cuts the range into at most `points` columns, each answering
// first, last, min and max, the columns a run of whole rollup rows.
func TestAggregateDownsample(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	f.heartRate("garmin", day, 7*time.Minute, 0, time.Time{}, time.Time{})
	ctx, trace := engine.AggregateTraced(context.Background())
	res, err := f.ds.Aggregate(ctx, substrate.AggregateQuery{
		Aggregate: &substrate.Aggregate{
			Metric: &substrate.AggregateMetric{Ref: f.metricPath("health/heart_rate")},
			Range:  rng(day, day.AddDate(0, 0, 1)),
		},
		Downsample: &substrate.Downsample{Points: 10, Method: "m4"},
	})
	if err != nil {
		t.Fatalf("downsample: %v", err)
	}
	if len(res.Rows) == 0 || len(res.Rows) > 10 || trace.RawPoints() != 0 {
		t.Fatalf("%d columns, %d raw points read", len(res.Rows), trace.RawPoints())
	}
	if res.Bucket == nil || res.Bucket.Every != "PT2H30M" {
		t.Errorf("bucket = %+v, want columns of whole rollup rows", res.Bucket)
	}
	points := f.stream("health/heart_rate", "garmin")
	for _, row := range res.Rows {
		w := observed.Window{From: row.Start, To: row.End}
		lo, _ := observed.Min(points, w)
		hi, _ := observed.Max(points, w)
		first, _ := observed.First(points, w)
		last, _ := observed.Last(points, w)
		if v, _ := val(row, "first"); v != first.Exact {
			t.Errorf("%s first %s, want %s", row.Start, v, first.Exact)
		}
		if v, _ := val(row, "last"); v != last.Exact {
			t.Errorf("%s last %s, want %s", row.Start, v, last.Exact)
		}
		wantNum(t, row.Start.String(), row, "min", lo, true)
		wantNum(t, row.Start.String(), row, "max", hi, true)
	}
}

// A money property per month: summed exactly per currency, the anchor a
// declared datetime.
func TestAggregatePropertyOverMoney(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), owner, []map[string]any{
		vocabularyPackage(moneyPackage),
		spendManifest(),
	}); err != nil {
		t.Fatalf("install the spend kind: %v", err)
	}
	for i, x := range []struct {
		amount   int64
		currency string
		at       string
	}{
		{1005, "EUR", "2026-09-03T10:00:00Z"},
		{1999, "EUR", "2026-09-20T10:00:00Z"},
		{-250, "EUR", "2026-10-01T10:00:00Z"},
		{9500, "KWD", "2026-09-05T10:00:00Z"},
		{2, "JPY", "2026-10-02T10:00:00Z"},
	} {
		mustPut(t, ds, owner, substrate.PutInput{Kind: moneyPackage + "/spend", ID: strconv.Itoa(i), Properties: map[string]any{
			"price": moneyValue(x.amount, x.currency), "paidAt": x.at,
		}})
	}
	res, err := ds.Aggregate(context.Background(), substrate.AggregateQuery{
		Filter: substrate.Filter{Kinds: []string{moneyPackage + "/spend"}},
		Aggregate: &substrate.Aggregate{
			Property: &substrate.AggregateProperty{Name: "price", Anchor: "paidAt"},
			Range:    rng(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)),
			Bucket:   &substrate.AggregateBucket{Calendar: "month", Timezone: "UTC"},
			Reducers: reducers("sum", "count", "avg", "max"),
		},
	})
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	got := map[string]string{}
	for _, row := range res.Rows {
		sum, _ := val(row, "sum")
		cnt, _ := val(row, "count")
		avg, _ := val(row, "avg")
		got[row.Start.Format("2006-01")+" "+row.Group.Currency] = sum + "/" + cnt + "/" + avg + " " + row.Values[0].Unit
	}
	want := map[string]string{
		"2026-09 EUR": "30.04/2/15.02 EUR", "2026-09 KWD": "9.5/1/9.5 KWD",
		"2026-10 EUR": "-2.5/1/-2.5 EUR", "2026-10 JPY": "2/1/2 JPY",
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %q, want %q", k, got[k], w)
		}
	}
	if len(got) != len(want) || !res.Exact || res.Property != "price" {
		t.Errorf("rows %v, exact %v, property %q", got, res.Exact, res.Property)
	}
}

func vocabularyPackage(pkg string) map[string]any { return vocabulary.PackageManifest(pkg, 0) }

// spendManifest is a kind with a money property, a datetime and a date to
// place it, and a string that places nothing.
func spendManifest() map[string]any {
	return vocabulary.KindManifest(moneyPackage, map[string]any{"singular": "spend"}, map[string]any{
		"properties": map[string]any{
			"price":  map[string]any{"type": "money"},
			"paidAt": map[string]any{"type": "datetime"},
			"paidOn": map[string]any{"type": "date"},
			"note":   map[string]any{"type": "string"},
		},
	})
}

// A property read's anchor is a time: a record column that holds none
// (version, title, id, body, deletedAt) or a declared string is a 422 naming
// it, before any SQL compares it with the range. A declared date places a
// record at the start of its day in the calendar's zone: a spend paid on
// 2026-09-03 is in New York's 2026-09-03, not in 2026-09-02, where UTC
// midnight falls there.
func TestAggregatePropertyAnchorIsATime(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), owner, []map[string]any{
		vocabularyPackage(moneyPackage),
		spendManifest(),
	}); err != nil {
		t.Fatalf("install the spend kind: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: moneyPackage + "/spend", ID: "1", Properties: map[string]any{
		"price": moneyValue(1005, "EUR"), "paidAt": "2026-09-03T10:00:00Z", "paidOn": "2026-09-03", "note": "lunch",
	}})
	read := func(anchor string) (*substrate.AggregateResult, error) {
		return ds.Aggregate(context.Background(), substrate.AggregateQuery{
			Filter: substrate.Filter{Kinds: []string{moneyPackage + "/spend"}},
			Aggregate: &substrate.Aggregate{
				Property: &substrate.AggregateProperty{Name: "price", Anchor: anchor},
				Range:    rng(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)),
				Bucket:   &substrate.AggregateBucket{Calendar: "day", Timezone: "America/New_York"},
				Reducers: reducers("count"),
			},
		})
	}
	for _, anchor := range []string{"version", "title", "id", "body", "deletedAt", "note"} {
		_, err := read(anchor)
		if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "aggregate.property.anchor") {
			t.Errorf("anchor %s: %v, want a 422 naming the anchor", anchor, err)
		}
	}
	res, err := read("paidOn")
	if err != nil {
		t.Fatalf("anchor paidOn: %v", err)
	}
	if len(res.Rows) != 1 || res.Rows[0].Start.Format(time.DateOnly) != "2026-09-03" {
		t.Errorf("anchor paidOn: %+v, want the one row of 2026-09-03", res.Rows)
	}
}

// A request is refused before it reads a point or a rollup row: over a
// budget, a reducer its form does not take, a unit outside the measure, two
// selectors, a downsample beside reducers, a filter arm the index cannot
// test.
func TestAggregateRefusesOverBudgetBeforeRunning(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	f.heartRate("garmin", day, 7*time.Minute, 0, time.Time{}, time.Time{})
	f.steps("garmin", day, time.Hour, 24, 1, 1)
	hr := &substrate.AggregateMetric{Ref: f.metricPath("health/heart_rate")}
	base := func(mut func(a *substrate.Aggregate)) substrate.AggregateQuery {
		a := substrate.Aggregate{Metric: hr, Range: rng(day, day.AddDate(0, 0, 1)), Reducers: reducers("avg")}
		mut(&a)
		return substrate.AggregateQuery{Aggregate: &a}
	}
	for _, x := range []struct {
		name string
		q    substrate.AggregateQuery
		want string
	}{
		{"maxBuckets", base(func(a *substrate.Aggregate) { a.Bucket = &substrate.AggregateBucket{Every: "PT30S"} }), "aggregate.budget.maxBuckets"},
		{"maxBuckets cap", base(func(a *substrate.Aggregate) { a.Budget = &substrate.AggregateBudget{MaxBuckets: 10001} }), "cap of 10000"},
		{"maxSamples", base(func(a *substrate.Aggregate) {
			a.Reducers = []substrate.AggregateReducer{{Name: "percentile", Percentile: ptr(0.05)}}
			a.Budget = &substrate.AggregateBudget{MaxSamples: 10}
		}), "aggregate.budget.maxSamples"},
		{"sum on a gauge", base(func(a *substrate.Aggregate) { a.Reducers = reducers("sum") }), "does not apply to a gauge"},
		{"avg on a coded amount", base(func(a *substrate.Aggregate) {
			a.Metric = &substrate.AggregateMetric{Ref: f.metricPath("health/steps")}
			a.Reducers = reducers("increase")
		}), "does not apply to an amount"},
		{"unit outside the measure", base(func(a *substrate.Aggregate) { a.Unit = "kg" }), "is not a unit of rate"},
		{"a negative offset", base(func(a *substrate.Aggregate) {
			a.Bucket = &substrate.AggregateBucket{Calendar: "day", Timezone: "UTC", Offset: "-PT1H"}
		}), "never earlier; -PT1H is not one"},
		{"two selectors", base(func(a *substrate.Aggregate) { a.Property = &substrate.AggregateProperty{Name: "x"} }), "name one of metric, metrics and property"},
		{"no range", base(func(a *substrate.Aggregate) { a.Range = nil }), "aggregate.range"},
		{"an unknown reducer", base(func(a *substrate.Aggregate) { a.Reducers = reducers("median") }), `"median" is not a reducer`},
		{"downsample beside reducers", substrate.AggregateQuery{
			Aggregate:  &substrate.Aggregate{Metric: hr, Range: rng(day, day.AddDate(0, 0, 1)), Reducers: reducers("avg")},
			Downsample: &substrate.Downsample{Points: 800},
		}, "refused beside aggregate.reducers"},
		{"a filter arm", substrate.AggregateQuery{
			Filter:    substrate.Filter{Search: "run"},
			Aggregate: &substrate.Aggregate{Metric: hr, Range: rng(day, day.AddDate(0, 0, 1)), Reducers: reducers("avg")},
		}, "filter.search is not supported with aggregate"},
	} {
		t.Run(x.name, func(t *testing.T) {
			ctx, trace := engine.AggregateTraced(context.Background())
			_, err := f.ds.Aggregate(ctx, x.q)
			if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), x.want) {
				t.Fatalf("err = %v, want a refusal naming %q", err, x.want)
			}
			if trace.RawPoints() != 0 || trace.RollupRows() != 0 {
				t.Errorf("the refused read took %d raw points and %d rollup rows", trace.RawPoints(), trace.RollupRows())
			}
		})
	}
}

func vocabularyDuration(ms int64) string {
	return vocabulary.FormatISODuration(time.Duration(ms) * time.Millisecond)
}
