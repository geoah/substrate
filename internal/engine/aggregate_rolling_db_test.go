package engine_test

// The aggregate read's rolling windows (decision 0161): a window reaches
// before the range, says how many buckets held a value, and may end at the
// bucket before its own.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// rolled is a row's rolling value of a reducer over the reducer of, and the
// buckets with a value in its window.
func rolled(t *testing.T, row substrate.AggregateRow, reducer, of string) (string, int) {
	t.Helper()
	for _, v := range row.Values {
		if v.Of != nil && v.Of.String() == of && v.Reducer.String() == reducer {
			n := -1
			if v.N != nil {
				n = *v.N
			}
			if v.Value == nil {
				return "", n
			}
			return *v.Value, n
		}
	}
	t.Fatalf("row %s holds no rolling %s of %s", row.Start.Format(time.RFC3339), reducer, of)
	return "", 0
}

// A rolling window over the first buckets of a range reads the buckets before
// it, so a day's 3-day mean is the same whatever day the range starts on, and
// those buckets are counted, not answered. Each rolling value says how many
// buckets in its window held a value. `through: previous` ends the window at
// the bucket before, a baseline the day itself does not move, and stddev
// rolls too. The rows a rolling read answers are the rows the same read
// answers without it, a range that cuts its first bucket included. One
// weighing a day at 07:00+03:00, 70 kg on 1 September rising a kilogram a
// day, none on the 7th.
func TestAggregateRollingReadsTheBucketsBeforeTheRange(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	plus3 := time.FixedZone("", 3*3600)
	first := time.Date(2026, 9, 1, 0, 0, 0, 0, plus3)
	for d := range 10 {
		if d == 6 {
			continue
		}
		f.mustPut(t, f.observation, fmt.Sprintf("w/%d", d), map[string]any{
			"metric": f.metricRef("health/body_mass"), "source": f.sourceRef("scale"), "form": "gauge",
			"value": quantity(fmt.Sprint(70+d), "kg"), "at": first.AddDate(0, 0, d).Add(7 * time.Hour).Format(time.RFC3339),
			"hold": "PT0S", "utcOffset": 180,
		})
	}
	read := func(from time.Time, bucket substrate.AggregateBucket, rolling *substrate.AggregateRolling) *substrate.AggregateResult {
		return f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/body_mass")},
			Range:    rng(from, first.AddDate(0, 0, 10)),
			Bucket:   &bucket,
			Reducers: reducers("avg"),
			Rolling:  rolling,
		})
	}
	three := &substrate.AggregateRolling{Buckets: 3, Reducers: reducers("avg", "stddev")}
	for _, bucket := range []substrate.AggregateBucket{
		{Calendar: "day", Timezone: "Europe/Athens"}, {Calendar: "day"}, {Every: "P1D", Origin: "2026-09-01T00:00:00+03:00"},
	} {
		label := bucket.Timezone + bucket.Every
		if label == "" {
			label = "recorded-local"
		}
		late := read(first.AddDate(0, 0, 5), bucket, three)
		if len(late.Rows) != 4 {
			t.Fatalf("%s: %d rows, want the four days from the 6th with a weighing", label, len(late.Rows))
		}
		if got := late.Rows[0].Start.Format(time.RFC3339); got != "2026-09-06T00:00:00+03:00" {
			t.Fatalf("%s: the first row starts %s, want the range's first day", label, got)
		}
		v, n := rolled(t, late.Rows[0], "avg", "avg")
		if v != "74" || n != 3 {
			t.Errorf("%s: the 6th's 3-day mean = %s over %d days, want 74 over 3", label, v, n)
		}
		sd, _ := rolled(t, late.Rows[0], "stddev", "avg")
		if !close9(num(t, sd), math.Sqrt(2.0/3)) {
			t.Errorf("%s: the 6th's 3-day stddev = %s, want %v", label, sd, math.Sqrt(2.0/3))
		}
		if v, n := rolled(t, late.Rows[1], "avg", "avg"); v != "76" || n != 2 {
			t.Errorf("%s: the 8th's 3-day mean = %s over %d days, want 76 over 2 (no weighing on the 7th)", label, v, n)
		}
		early := read(first, bucket, three)
		for _, row := range early.Rows {
			if row.Start.Equal(late.Rows[0].Start) {
				if v, _ := rolled(t, row, "avg", "avg"); v != "74" {
					t.Errorf("%s: read from the 1st, the 6th's 3-day mean = %s, want 74 as from the 6th", label, v)
				}
			}
		}
		previous := read(first.AddDate(0, 0, 5), bucket, &substrate.AggregateRolling{Buckets: 3, Through: "previous"})
		if v, n := rolled(t, previous.Rows[0], "avg", "avg"); v != "73" || n != 3 {
			t.Errorf("%s: the 6th's mean of the 3 days before = %s over %d, want 73 over 3", label, v, n)
		}
	}

	// A range starting at noon cuts its first day: the rows are the plain
	// read's, the cut day's own weighing (07:00) left out of it.
	noon := first.AddDate(0, 0, 4).Add(12 * time.Hour)
	bucket := substrate.AggregateBucket{Calendar: "day", Timezone: "Europe/Athens"}
	plain := read(noon, bucket, nil)
	withRolling := read(noon, bucket, three)
	if len(plain.Rows) != len(withRolling.Rows) {
		t.Fatalf("a cut range: %d rows plain, %d rolling", len(plain.Rows), len(withRolling.Rows))
	}
	for i := range plain.Rows {
		a, _ := json.Marshal(plain.Rows[i].Values)
		b, _ := json.Marshal(withRolling.Rows[i].Values[:len(plain.Rows[i].Values)])
		if string(a) != string(b) || !plain.Rows[i].Start.Equal(withRolling.Rows[i].Start) || plain.Rows[i].Count != withRolling.Rows[i].Count {
			t.Errorf("a cut range's row %d: plain %s %s, rolling %s %s", i, plain.Rows[i].Start, a, withRolling.Rows[i].Start, b)
		}
	}

	f.refusedAgg(substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
		Metric: &substrate.AggregateMetric{Ref: f.metricPath("health/body_mass")}, Range: rng(first, first.AddDate(0, 0, 10)),
		Reducers: reducers("avg"), Rolling: three,
	}}, "name a bucket")
}

// In the recorded-local calendar a value in the range at an offset west of
// the range's can land on the day before the range's first, which the
// rolling lookback also reads: that day is one place in a window, taken
// whole (its values from both sides of the range's start), so it weighs once
// and n never exceeds the window. One weighing a day at 07:00+03:00, 70 kg on
// 1 September rising a kilogram a day, and 90 kg at 18:00-04:00 on the 5th,
// which is 01:00 on the 6th at +03:00 and so inside a range from the 6th.
func TestAggregateRollingTakesASplitLocalDayOnce(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	plus3 := time.FixedZone("", 3*3600)
	first := time.Date(2026, 9, 1, 0, 0, 0, 0, plus3)
	for d := range 9 {
		f.mustPut(t, f.observation, fmt.Sprintf("w/%d", d), map[string]any{
			"metric": f.metricRef("health/body_mass"), "source": f.sourceRef("scale"), "form": "gauge",
			"value": quantity(fmt.Sprint(70+d), "kg"), "at": first.AddDate(0, 0, d).Add(7 * time.Hour).Format(time.RFC3339),
			"hold": "PT0S", "utcOffset": 180,
		})
	}
	f.mustPut(t, f.observation, "w/west", map[string]any{
		"metric": f.metricRef("health/body_mass"), "source": f.sourceRef("scale"), "form": "gauge",
		"value": quantity("90", "kg"), "at": time.Date(2026, 9, 5, 18, 0, 0, 0, time.FixedZone("", -4*3600)).Format(time.RFC3339),
		"hold": "PT0S", "utcOffset": -240,
	})
	read := func(rolling *substrate.AggregateRolling) *substrate.AggregateResult {
		res, _, _ := f.bothPaths(substrate.Filter{}, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/body_mass")},
			Range:    rng(first.AddDate(0, 0, 5), first.AddDate(0, 0, 9)),
			Bucket:   &substrate.AggregateBucket{Calendar: "day"},
			Reducers: reducers("avg"),
			Rolling:  rolling,
		})
		return res
	}
	plain := read(nil)
	two := read(&substrate.AggregateRolling{Buckets: 2})
	if len(plain.Rows) != 5 || len(two.Rows) != 5 {
		t.Fatalf("%d rows plain, %d rolling; want the 5th's evening at -04:00 and the 6th to the 9th", len(plain.Rows), len(two.Rows))
	}
	for i := range plain.Rows {
		a, _ := val(plain.Rows[i], "avg")
		b, _ := val(two.Rows[i], "avg")
		if a != b || !plain.Rows[i].Start.Equal(two.Rows[i].Start) {
			t.Errorf("row %d: plain %s avg %s, rolling %s avg %s", i, plain.Rows[i].Start, a, two.Rows[i].Start, b)
		}
		if _, n := rolled(t, two.Rows[i], "avg", "avg"); n > 2 {
			t.Errorf("row %s: a 2-day window holds %d values", two.Rows[i].Start.Format(time.RFC3339), n)
		}
	}
	if got := two.Rows[0].Start.Format(time.RFC3339); got != "2026-09-05T17:00:00-04:00" {
		t.Fatalf("the first row starts %s, want the 5th from the range's start, 17:00-04:00", got)
	}
	// The 5th is 74 and 90 whole, a mean of 82; the 6th is 75.
	if v, n := rolled(t, two.Rows[1], "avg", "avg"); v != "78.5" || n != 2 {
		t.Errorf("the 6th's 2-day mean = %s over %d places, want 78.5 over 2", v, n)
	}
	if v, n := rolled(t, two.Rows[0], "avg", "avg"); v != "82" || n != 1 {
		t.Errorf("the 5th's 2-day mean = %s over %d places, want its whole day's 82 over 1 (the 4th is not read)", v, n)
	}
	previous := read(&substrate.AggregateRolling{Buckets: 2, Through: "previous"})
	if v, n := rolled(t, previous.Rows[1], "avg", "avg"); v != "77.5" || n != 2 {
		t.Errorf("the 6th's mean of the 2 days before = %s over %d places, want 77.5 (73 and 82) over 2", v, n)
	}
}
