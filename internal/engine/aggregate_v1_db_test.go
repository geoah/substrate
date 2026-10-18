package engine_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

// A gauge's avg is its time-weighted mean (decision 0158), on the rollup path
// and from raw points alike, worked by hand:
//
//   - an on-change thermometer, 20 at 00:00, 22 at 06:00 and 18 at 18:00,
//     each holding until the next: the day is 20 for 6 hours, 22 for 12 and
//     18 for 6, 20.5, where the mean of the three readings is 20;
//   - a heart rate read every 30 minutes and every minute in a workout: 60
//     at 08:00, 120 each minute from 08:30 to 08:39, 60 at 08:40, the hour
//     is 60 for 50 minutes and 120 for 10, 70, where the mean of the twelve
//     readings is 110;
//   - weigh-ins on Monday, Wednesday and Friday, 80, 81 and 79.5, each held
//     a minute (a scale) or no time at all (typed in, PT0S): the week's avg
//     is the mean of the weigh-ins, 80.1666666666667, for either hold;
//   - a bucket mixing the two: a reading of 10 held an hour and one of 100
//     held no time. Where any value holds time the mean is weighed by time,
//     so the PT0S reading weighs nothing and the hour's avg is 10; the mean
//     of the readings is used only where none holds any time.
func TestAggregateGaugeAvgIsTimeWeighted(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i, x := range []struct {
		at    time.Duration
		value string
	}{{0, "20"}, {6 * time.Hour, "22"}, {18 * time.Hour, "18"}} {
		f.mustPut(t, f.observation, fmt.Sprintf("room/%d", i), map[string]any{
			"metric": f.metricRef("air_temperature"), "source": f.sourceRef("thermometer"), "form": "gauge",
			"value": quantity(x.value, "Cel"), "at": at(day.Add(x.at)), "utcOffset": 0,
		})
	}
	room, _, rows := f.bothPaths(substrate.Filter{}, substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("air_temperature")},
		Range:    rng(day, day.AddDate(0, 0, 1)),
		Bucket:   &substrate.AggregateBucket{Calendar: "day", Timezone: "UTC"},
		Reducers: reducers("avg", "count"),
	})
	if rows == 0 {
		t.Errorf("the thermometer's day read no rollup row; rollups serve a gauge's avg")
	}
	if len(room.Rows) != 1 {
		t.Fatalf("the thermometer's day: %d rows, want one", len(room.Rows))
	}
	if v, _ := val(room.Rows[0], "avg"); v != "20.5" {
		t.Errorf("the thermometer's day avg = %s, want 20.5 (20 for 6h, 22 for 12h, 18 for 6h)", v)
	}

	workout := day.Add(8 * time.Hour)
	offsets := []int64{0}
	vals := []any{"60"}
	for m := 30; m < 40; m++ {
		offsets = append(offsets, int64(m)*time.Minute.Milliseconds())
		vals = append(vals, "120")
	}
	offsets = append(offsets, (40 * time.Minute).Milliseconds(), time.Hour.Milliseconds())
	vals = append(vals, "60", "60")
	f.series("hr/workout", map[string]any{
		"metric": f.metricRef("health/heart_rate"), "source": f.sourceRef("strap"), "form": "gauge", "unit": "/min",
	}, workout, workout.Add(time.Hour+time.Minute), offsets, vals)
	hr, _, rows := f.bothPaths(substrate.Filter{}, substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/heart_rate")},
		Range:    rng(workout, workout.Add(time.Hour)),
		Bucket:   &substrate.AggregateBucket{Every: "PT1H"},
		Reducers: reducers("avg", "count"),
	})
	if rows == 0 {
		t.Errorf("the workout hour read no rollup row")
	}
	if len(hr.Rows) != 1 {
		t.Fatalf("the workout hour: %d rows, want one", len(hr.Rows))
	}
	if v, _ := val(hr.Rows[0], "avg"); v != "70" {
		t.Errorf("the workout hour avg = %s, want 70 (60 for 50 minutes, 120 for 10), not the readings' mean 110", v)
	}
	if c, _ := val(hr.Rows[0], "count"); c != "12" {
		t.Errorf("the workout hour count = %s, want its 12 readings", c)
	}

	monday := time.Date(2026, 9, 7, 7, 0, 0, 0, time.UTC)
	for _, hold := range []string{"PT1M", "PT0S"} {
		source := map[string]string{"PT1M": "scale", "PT0S": "typed"}[hold]
		for i, x := range []struct {
			day   int
			value string
		}{{0, "80"}, {2, "81"}, {4, "79.5"}} {
			f.mustPut(t, f.observation, fmt.Sprintf("weigh/%s/%d", source, i), map[string]any{
				"metric": f.metricRef("health/body_mass"), "source": f.sourceRef(source), "form": "gauge",
				"value": quantity(x.value, "kg"), "at": at(monday.AddDate(0, 0, x.day)), "hold": hold, "utcOffset": 0,
			})
		}
	}
	week, _, _ := f.bothPaths(substrate.Filter{}, substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/body_mass")},
		Range:    rng(monday.Add(-7*time.Hour), monday.Add(-7*time.Hour).AddDate(0, 0, 7)),
		Bucket:   &substrate.AggregateBucket{Calendar: "week", Timezone: "UTC"},
		GroupBy:  []string{"source"},
		Reducers: reducers("avg", "count"),
	})
	if len(week.Rows) != 2 {
		t.Fatalf("the week: %d rows, want one per source", len(week.Rows))
	}
	for _, row := range week.Rows {
		if v, _ := val(row, "avg"); v != "80.1666666666667" {
			t.Errorf("%s: the week's avg = %s, want the mean of the three weigh-ins, 80.1666666666667", row.Group.Source, v)
		}
	}

	mixed := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	for i, x := range []struct{ value, hold string }{{"10", "PT1H"}, {"100", "PT0S"}} {
		f.mustPut(t, f.observation, fmt.Sprintf("mixed/%d", i), map[string]any{
			"metric": f.metricRef("air_temperature"), "source": f.sourceRef("mixed"), "form": "gauge",
			"value": quantity(x.value, "Cel"), "at": at(mixed.Add(time.Duration(i) * 10 * time.Minute)), "hold": x.hold, "utcOffset": 0,
		})
	}
	both, _, _ := f.bothPaths(substrate.Filter{Properties: map[string]substrate.Cond{"source": {Eq: f.sourcePath("mixed")}}}, substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("air_temperature")},
		Range:    rng(mixed, mixed.Add(time.Hour)),
		Reducers: reducers("avg", "count"),
	})
	if len(both.Rows) != 1 {
		t.Fatalf("the mixed hour: %d rows, want one", len(both.Rows))
	}
	if v, _ := val(both.Rows[0], "avg"); v != "10" {
		t.Errorf("the mixed hour's avg = %s, want 10: the PT0S reading holds no time and weighs nothing", v)
	}
}

// A value written with a comparator (a lab's `<0.01`) has no known number:
// every reducer that needs it answers null for its row, count counts it and
// the row says how many it held, first and last answer it with its
// comparator, and min and max answer with one where the bounds decide. A
// rollup row keeps how many and not which, so the row is read from raw
// points, and both paths answer alike.
func TestAggregateCensoredValuesKeepTheirComparator(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	month := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i, x := range []struct {
		value, comparator string
	}{{"0.01", "<"}, {"0.5", ""}, {"2.0", ""}} {
		value := map[string]any{"value": x.value, "unit": "m[IU]/L"}
		if x.comparator != "" {
			value["comparator"] = x.comparator
		}
		f.mustPut(t, f.labresult, fmt.Sprintf("lab/tsh/%d", i), map[string]any{
			"metric": f.metricRef("health/tsh"), "form": "gauge", "hold": "PT0S", "value": value,
			"at": at(month.AddDate(0, 0, 7*i+2)), "utcOffset": 0,
		})
	}
	res, _, _ := f.bothPaths(substrate.Filter{}, substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/tsh")},
		Range:    rng(month, month.AddDate(0, 1, 0)),
		Bucket:   &substrate.AggregateBucket{Calendar: "month", Timezone: "UTC"},
		Reducers: reducers("count", "avg", "stddev", "min", "max", "first", "last"),
	})
	if len(res.Rows) != 1 {
		t.Fatalf("%d rows, want the month", len(res.Rows))
	}
	row := res.Rows[0]
	if row.Count != 3 || row.Bounded != 1 {
		t.Errorf("count %d, bounded %d; want 3 and 1", row.Count, row.Bounded)
	}
	got := map[string]string{}
	for _, v := range row.Values {
		s := "null"
		if v.Value != nil {
			s = v.Comparator + *v.Value
		}
		got[v.Reducer.Name] = s
	}
	for reducer, want := range map[string]string{
		"count": "3", "avg": "null", "stddev": "null", "min": "<0.01", "max": "2.0", "first": "<0.01", "last": "2.0",
	} {
		if got[reducer] != want {
			t.Errorf("%s = %s, want %s", reducer, got[reducer], want)
		}
	}
	// A row without the censored value answers as any other.
	later := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/tsh")},
		Range:    rng(month.AddDate(0, 0, 5), month.AddDate(0, 1, 0)),
		Reducers: reducers("avg"),
	})
	if len(later.Rows) != 1 || later.Rows[0].Bounded != 0 {
		t.Fatalf("the exact values alone: %+v", later.Rows)
	}
	if v, _ := val(later.Rows[0], "avg"); v != "1.25" {
		t.Errorf("the exact values' avg = %s, want 1.25", v)
	}
}

// A censored value carried into a row by its hold weighs in that row's held
// time though it is no member of it: `<0.01` at 09:00 held three hours and
// 0.5 at 10:30 make 10:00 to 11:00 half an unknown number, so the hour's
// time-weighted avg (which rollups serve) and its durationIn (which raw
// points serve) answer null, both paths alike, while count and max read the
// members alone. The next hour carries 0.5 and answers it.
func TestAggregateCensoredCarryNullsTheHeldReducers(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	nine := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	for i, x := range []struct {
		value, comparator string
		at                time.Time
	}{{"0.01", "<", nine}, {"0.5", "", nine.Add(90 * time.Minute)}} {
		value := map[string]any{"value": x.value, "unit": "m[IU]/L"}
		if x.comparator != "" {
			value["comparator"] = x.comparator
		}
		f.mustPut(t, f.labresult, fmt.Sprintf("lab/tsh/held/%d", i), map[string]any{
			"metric": f.metricRef("health/tsh"), "form": "gauge", "hold": "PT3H", "value": value,
			"at": at(x.at), "utcOffset": 0,
		})
	}
	read := func(from time.Time, reds ...substrate.AggregateReducer) (substrate.AggregateRow, int64) {
		t.Helper()
		res, _, rows := f.bothPaths(substrate.Filter{}, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/tsh")},
			Range:    rng(from, from.Add(time.Hour)),
			Bucket:   &substrate.AggregateBucket{Every: "PT1H"},
			Reducers: reds,
		})
		if len(res.Rows) != 1 {
			t.Fatalf("from %s: %d rows, want one", from.Format(time.Kitchen), len(res.Rows))
		}
		return res.Rows[0], rows
	}
	held := substrate.AggregateReducer{Name: "durationIn", Between: []float64{0, 1}}
	ten, _ := read(nine.Add(time.Hour), reducers("avg", "count", "max")...)
	if v, ok := val(ten, "avg"); ok {
		t.Errorf("10:00 avg = %s, want null: the carried <0.01 holds half the hour", v)
	}
	if v, _ := val(ten, "count"); v != "1" {
		t.Errorf("10:00 count = %s, want the one member", v)
	}
	if v, _ := val(ten, "max"); v != "0.5" {
		t.Errorf("10:00 max = %s, want the member's 0.5", v)
	}
	if ten, _ := read(nine.Add(time.Hour), held); len(ten.Values) != 1 || ten.Values[0].Value != nil {
		t.Errorf("10:00 durationIn = %+v, want null: the carried <0.01 holds half the hour", ten.Values)
	}
	eleven, _ := read(nine.Add(2*time.Hour), reducers("avg")...)
	if v, _ := val(eleven, "avg"); v != "0.5" {
		t.Errorf("11:00 avg = %s, want the carried 0.5", v)
	}
	if eleven, _ := read(nine.Add(2*time.Hour), held); len(eleven.Values) != 1 || eleven.Values[0].Value == nil {
		t.Errorf("11:00 durationIn = %+v, want the hour 0.5 holds", eleven.Values)
	}
}

// Without bucket.place, a calendar read places a metric's values by its
// record's bucketPlace: sleep stages declare `end`, so a stage from 23:00
// to 07:00 lands on the day it ends, and the answer's bucket says so. A
// place the request names wins.
func TestAggregatePlaceDefaultsToTheMetricsBucketPlace(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	evening := time.Date(2026, 9, 1, 23, 0, 0, 0, time.UTC)
	f.stageAt("night/light", "whoop", "light", evening, 8*time.Hour)
	read := func(place string) *substrate.AggregateResult {
		t.Helper()
		return f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/sleep_stage")},
			Range:    rng(evening.Add(-23*time.Hour), evening.Add(25*time.Hour)),
			Bucket:   &substrate.AggregateBucket{Calendar: "day", Timezone: "UTC", Place: place},
			Reducers: reducers("sum"),
		})
	}
	for _, x := range []struct {
		place, day, echo string
	}{{"", "2026-09-02", "end"}, {"end", "2026-09-02", "end"}, {"start", "2026-09-01", "start"}} {
		res := read(x.place)
		if len(res.Rows) != 1 || res.Rows[0].Start.Format(time.DateOnly) != x.day {
			t.Errorf("place %q: %+v, want the night on %s", x.place, res.Rows, x.day)
		}
		if res.Bucket == nil || res.Bucket.Place != x.echo {
			t.Errorf("place %q: the answer's bucket %+v, want place %s", x.place, res.Bucket, x.echo)
		}
	}
}

// A point a records-mode read places in several windows is a copy in each,
// and every copy past the first counts against maxSamples: ten steps inside
// a night and a nap within it are twenty placements, and the read is
// refused, naming the copies, until the budget holds them.
func TestAggregateWindowCopiesCountAgainstMaxSamples(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	night := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	f.mustPut(t, f.sleepKind(), "night", map[string]any{"at": at(night), "endsAt": at(night.Add(4 * time.Hour))})
	f.mustPut(t, f.sleepKind(), "nap", map[string]any{"at": at(night.Add(time.Hour)), "endsAt": at(night.Add(2 * time.Hour))})
	for i := range 10 {
		from := night.Add(70*time.Minute + time.Duration(i)*time.Minute)
		f.mustPut(t, f.observation, fmt.Sprintf("steps/%d", i), map[string]any{
			"metric": f.metricRef("health/steps"), "source": f.sourceRef("phone"), "form": "amount",
			"value": quantity("10", "{steps}"), "at": at(from), "endsAt": at(from.Add(30 * time.Second)),
		})
	}
	read := func(maxSamples int64) (*substrate.AggregateResult, error) {
		return f.ds.Aggregate(context.Background(), substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/steps")},
			Range:    rng(night, night.Add(24*time.Hour)),
			Bucket:   &substrate.AggregateBucket{Records: &substrate.AggregateRecords{Filter: substrate.Filter{Kinds: []string{f.sleepKind()}}}},
			Reducers: reducers("sum"),
			Budget:   &substrate.AggregateBudget{MaxSamples: maxSamples},
		}})
	}
	var last error
	for n := int64(1); n <= 200; n++ {
		res, err := read(n)
		if err == nil {
			if len(res.Rows) != 2 {
				t.Fatalf("maxSamples %d: %+v, want the night and the nap", n, res.Rows)
			}
			for _, row := range res.Rows {
				if v, _ := val(row, "sum"); v != "100" {
					t.Errorf("%s: sum %s, want the ten steps' 100", row.Record.Ref, v)
				}
			}
			if last == nil || !strings.Contains(last.Error(), "copies of a point") {
				t.Errorf("under maxSamples %d: %v; want the last refusal to name the copies", n-1, last)
			}
			return
		}
		last = err
	}
	t.Fatalf("no budget up to 200 answers: %v", last)
}

// A rolling window reads its buckets' values per row, and that work counts
// against maxSamples beside the points: ten days of heart rate rolled over 7
// days are 70 values, and the read is refused naming the rolling windows
// until the budget holds them.
func TestAggregateRollingWorkCountsAgainstMaxSamples(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for d := range 10 {
		f.weighing(fmt.Sprintf("w/%d", d), "scale", "70", day.AddDate(0, 0, d).Add(7*time.Hour), nil)
	}
	read := func(maxSamples int64) (*substrate.AggregateResult, error) {
		return f.ds.Aggregate(context.Background(), substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/body_mass")},
			Range:    rng(day.AddDate(0, 0, 6), day.AddDate(0, 0, 10)),
			Bucket:   &substrate.AggregateBucket{Calendar: "day", Timezone: "UTC"},
			Reducers: reducers("max"),
			Rolling:  &substrate.AggregateRolling{Buckets: 7, Reducers: reducers("avg")},
			Budget:   &substrate.AggregateBudget{MaxSamples: maxSamples},
		}})
	}
	var last error
	for n := int64(1); n <= 200; n++ {
		_, err := read(n)
		if err != nil {
			last = err
			continue
		}
		if last == nil || !strings.Contains(last.Error(), "the rolling windows read 28 values") {
			t.Errorf("under maxSamples %d: %v; want the last refusal to name the rolling windows' 28 values (4 rows times 7)", n-1, last)
		}
		return
	}
	t.Fatalf("no budget up to 200 answers: %v", last)
}

// The value a read looks up before a window (a gauge's carry, a counter's
// previous point) derives its whole record, and those points count against
// maxSamples like any it reads: a 60-point trace from 08:00 to 09:00 sits
// wholly before a read of 12:00 to 13:00, its hold long ended. The gauge's
// form discovery and the counter's raw read each derive it to find the value
// before 12:00, so each read is refused under maxSamples 59 and answers
// under 100.
func TestAggregateCarryLookupsCountAgainstMaxSamples(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	eight := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	var offsets []int64
	var hr, meter []any
	for m := range 60 {
		offsets = append(offsets, int64(m)*time.Minute.Milliseconds())
		hr = append(hr, "60")
		meter = append(meter, fmt.Sprint(100+m))
	}
	f.series("hr/morning", map[string]any{
		"metric": f.metricRef("health/heart_rate"), "source": f.sourceRef("strap"), "form": "gauge", "unit": "/min",
		"hold": "PT4M", "utcOffset": 0,
	}, eight, eight.Add(time.Hour), offsets, hr)
	f.series("meter/morning", map[string]any{
		"metric": f.metricRef("energy"), "source": f.sourceRef("meter"), "form": "counter", "unit": "kW.h",
		"resetRule": "monotonic", "utcOffset": 0,
	}, eight, eight.Add(time.Hour), offsets, meter)
	noon := eight.Add(4 * time.Hour)
	f.series("meter/noon", map[string]any{
		"metric": f.metricRef("energy"), "source": f.sourceRef("meter"), "form": "counter", "unit": "kW.h",
		"resetRule": "monotonic", "utcOffset": 0,
	}, noon.Add(30*time.Minute), noon.Add(31*time.Minute), []int64{0}, []any{"200"})
	for _, x := range []struct {
		metric, reducer string
	}{{"health/heart_rate", "avg"}, {"energy", "increase"}} {
		read := func(maxSamples int64) (*substrate.AggregateResult, error) {
			return f.ds.Aggregate(engine.AggregateRaw(context.Background()), substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
				Metric:   &substrate.AggregateMetric{Ref: f.metricPath(x.metric)},
				Range:    rng(noon, noon.Add(time.Hour)),
				Bucket:   &substrate.AggregateBucket{Every: "PT1H"},
				Reducers: reducers(x.reducer),
				Budget:   &substrate.AggregateBudget{MaxSamples: maxSamples},
			}})
		}
		if _, err := read(59); err == nil || !strings.Contains(err.Error(), "aggregate.budget.maxSamples") {
			t.Errorf("%s under maxSamples 59: %v, want a refusal naming maxSamples", x.metric, err)
		}
		if _, err := read(100); err != nil {
			t.Errorf("%s under maxSamples 100: %v", x.metric, err)
		}
	}
}

// Form discovery reads the segments of every pair of a stream and a window,
// as many pairs as the streams times the windows, so it sends them a batch at
// a time and answers alike whatever the batch: three sources' heart rate in
// three naps are nine pairs, read two at a time.
func TestAggregateFormDiscoveryReadsPairsInBatches(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i, source := range []string{"strap", "watch", "ring"} {
		f.heartRate(source, day, 30*time.Minute, i, time.Time{}, time.Time{})
	}
	for i, h := range []int{1, 9, 15} {
		f.mustPut(t, f.sleepKind(), fmt.Sprintf("nap/%d", i), map[string]any{
			"at": at(day.Add(time.Duration(h) * time.Hour)), "endsAt": at(day.Add(time.Duration(h+1) * time.Hour)),
		})
	}
	a := substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/heart_rate")},
		Range:    rng(day, day.AddDate(0, 0, 1)),
		Bucket:   &substrate.AggregateBucket{Records: &substrate.AggregateRecords{Filter: substrate.Filter{Kinds: []string{f.sleepKind()}}}},
		GroupBy:  []string{"source"},
		Reducers: reducers("avg", "count"),
	}
	whole := f.agg(context.Background(), substrate.Filter{}, a)
	if len(whole.Rows) != 9 {
		t.Fatalf("%d rows, want three naps times three sources", len(whole.Rows))
	}
	ctx, trace := engine.AggregateTraced(context.Background())
	trace.DiscoverBatch(2)
	sameRows(t, whole, f.agg(ctx, substrate.Filter{}, a))
	if n := trace.DiscoverPairs(); n != 2 {
		t.Errorf("one discovery statement read %d pairs, want the batch of 2", n)
	}
}

// Form discovery holds a record crossing a window's start or end once per
// window it crosses, and each hold past the first counts against maxSamples
// as a copy, as the reduction counts a point it places in several windows:
// four day-long amounts crossing twenty naps are eighty holds of one point.
// Under maxSamples 60 discovery refuses before it holds them all, where
// counting each record once let it hold every copy and left the refusal to
// the reduction; under 1000 the read answers every nap.
func TestAggregateFormDiscoveryCountsEveryWindowARecordCrosses(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for h := range 20 {
		from := day.Add(time.Duration(h)*time.Hour + 5*time.Minute)
		f.mustPut(t, f.sleepKind(), fmt.Sprintf("nap/%d", h), map[string]any{"at": at(from), "endsAt": at(from.Add(10 * time.Minute))})
	}
	for i := range 4 {
		f.mustPut(t, f.observation, fmt.Sprintf("steps/%d", i), map[string]any{
			"metric": f.metricRef("health/steps"), "source": f.sourceRef("phone"), "form": "amount",
			"value": quantity("1320", "{steps}"), "at": at(day.Add(-time.Hour)), "endsAt": at(day.Add(21 * time.Hour)),
		})
	}
	read := func(maxSamples int64) (*substrate.AggregateResult, error) {
		return f.ds.Aggregate(context.Background(), substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/steps")},
			Range:    rng(day, day.AddDate(0, 0, 1)),
			Bucket:   &substrate.AggregateBucket{Records: &substrate.AggregateRecords{Filter: substrate.Filter{Kinds: []string{f.sleepKind()}}}},
			Reducers: reducers("sum"),
			Budget:   &substrate.AggregateBudget{MaxSamples: maxSamples},
		}})
	}
	if _, err := read(60); err == nil || !strings.Contains(err.Error(), "finding the forms the range holds") {
		t.Errorf("under maxSamples 60: %v, want form discovery's refusal", err)
	}
	res, err := read(1000)
	if err != nil {
		t.Fatalf("under maxSamples 1000: %v", err)
	}
	if len(res.Rows) != 20 {
		t.Fatalf("%d rows, want one per nap", len(res.Rows))
	}
	for _, row := range res.Rows {
		if v, _ := val(row, "sum"); v != "40" {
			t.Errorf("%s: sum %s, want ten minutes of each of four amounts at one step a minute", row.Record.Ref, v)
		}
	}
}

// A read that runs past its own deadline is a refusal naming the limit,
// not an internal error; a caller's own earlier deadline stays the
// caller's. It shortens the process-wide deadline, so it runs alone.
func TestAggregateRefusesAReadPastItsDeadline(t *testing.T) {
	f := openAgg(t)
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	f.weighing("w", "scale", "70", day.Add(7*time.Hour), nil)
	engine.SetAggregateDeadline(t, time.Nanosecond)
	f.refusedAgg(substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/body_mass")},
		Range:    rng(day, day.AddDate(0, 0, 1)),
		Reducers: reducers("max"),
	}}, "the read ran past its 1ns limit")
}
