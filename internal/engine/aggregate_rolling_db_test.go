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
