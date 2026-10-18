package engine_test

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// tierHours writes a sensor's hourly traces from `from` to `to`, a reading
// every ten minutes, each hour at the UTC offset offsetOf names for its
// start: a home sensor across a clock change, or one that travels.
func (f aggFix) tierHours(source, metric, form, unit string, from, to time.Time, offsetOf func(time.Time) int) {
	f.t.Helper()
	total := 0
	for start := from; start.Before(to); start = start.Add(time.Hour) {
		var offsets []int64
		var vals []any
		for i := range 6 {
			at := start.Add(time.Duration(i) * 10 * time.Minute)
			offsets = append(offsets, at.Sub(start).Milliseconds())
			n := start.Hour()*6 + i
			switch form {
			case "counter":
				total += 3 + n%5
				if start.Hour() == 3 && i == 0 {
					total = 1
				}
				vals = append(vals, strconv.Itoa(total))
			default:
				vals = append(vals, fmt.Sprintf("%d.%d", 15+n%11, n%10))
			}
		}
		props := map[string]any{
			"metric": f.metricRef(metric), "source": f.sourceRef(source), "form": form, "unit": unit,
			"utcOffset": offsetOf(start),
		}
		switch form {
		case "gauge":
			props["hold"] = "PT20M"
		case "counter":
			props["resetRule"] = "monotonic"
		}
		f.series(fmt.Sprintf("%s/%s/%s", metric, source, start.UTC().Format("2006-01-02T15")), props,
			start, start.Add(time.Hour), offsets, vals)
	}
}

// The tiers serve each bucket from the coarsest rows that fit it and answer
// what the raw points do. Ten days of a home sensor in Berlin straddle the
// clock change of 2026-10-25, so its day rows are local days at +02:00
// before it and +01:00 after: a Berlin calendar day is one day row where
// the sensor kept one offset, and the hour rows on the days around the change,
// where the two days' rows overlap; the recorded-local calendar lands each
// day row in its own day; a UTC day takes the hour rows, since no day row is
// a UTC day; and a quarter-hour read takes the quarter-hour rows. A gauge, an
// amount and a counter each read the same through the rows and the points.
func TestAggregateTiersServeTheCoarsestRowsThatFit(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 10, 20, 0, 0, 0, 0, berlin)
	to := time.Date(2026, 10, 30, 0, 0, 0, 0, berlin)
	offsetOf := func(at time.Time) int {
		_, off := at.In(berlin).Zone()
		return off / 60
	}
	f.tierHours("attic", "air_temperature", "gauge", "Cel", from, to, offsetOf)
	f.tierHours("phone", "health/steps", "amount", "{steps}", from, to, offsetOf)
	f.tierHours("meter", "energy", "counter", "kW.h", from, to, offsetOf)
	reads := []struct {
		name     string
		metric   string
		reducers []string
		bucket   substrate.AggregateBucket
		// most is the most rollup rows the read may take; least the fewest.
		least, most int64
	}{
		{
			"Berlin days of a gauge", "air_temperature",
			[]string{"avg", "min", "max", "count", "first", "last"},
			substrate.AggregateBucket{Calendar: "day", Timezone: "Europe/Berlin"},
			10, 7 + 3*24,
		},
		{
			"local days of a gauge", "air_temperature",
			[]string{"avg", "min", "max"},
			substrate.AggregateBucket{Calendar: "day"},
			10, 7 + 3*24,
		},
		{
			"UTC days of an amount", "health/steps",
			[]string{"sum", "count"},
			substrate.AggregateBucket{Every: "P1D"},
			24, 11 * 24,
		},
		{
			"Berlin days of a counter", "energy",
			[]string{"increase", "rate"},
			substrate.AggregateBucket{Calendar: "day", Timezone: "Europe/Berlin"},
			10, 7 + 3*24,
		},
		{
			"quarter hours of a gauge", "air_temperature",
			[]string{"avg", "max"},
			substrate.AggregateBucket{Every: "PT15M"},
			4 * 24, 10 * 25 * 4,
		},
		{
			"Berlin hours of an amount", "health/steps",
			[]string{"sum"},
			substrate.AggregateBucket{Calendar: "hour", Timezone: "Europe/Berlin"},
			24, 10 * 25,
		},
	}
	for _, x := range reads {
		_, _, rows := f.bothPaths(substrate.Filter{}, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath(x.metric)},
			Range:    rng(from, to),
			Bucket:   &x.bucket,
			Reducers: reducers(x.reducers...),
		})
		t.Logf("%s: %d rollup rows", x.name, rows)
		if rows < x.least || rows > x.most {
			t.Errorf("%s: took %d rollup rows, want %d to %d", x.name, rows, x.least, x.most)
		}
	}
}

// A metric that keeps its hours alone serves a quarter-hour read of an
// amount from the hour rows where they fit and from the points in the
// quarters they do not, and a gauge's quarter hours from the points whole;
// a metric that keeps no rollups reads every bucket from the points.
func TestAggregateTiersNamedByTheMetric(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	f.rollups("health/steps", "1h")
	f.rollups("air_temperature")
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(30 * time.Hour)
	utc := func(time.Time) int { return 0 }
	f.tierHours("phone", "health/steps", "amount", "{steps}", from, to, utc)
	f.tierHours("attic", "air_temperature", "gauge", "Cel", from, to, utc)
	for _, x := range []struct {
		metric string
		bucket substrate.AggregateBucket
		rng    *substrate.AggregateRange
		rows   int64
	}{
		{"health/steps", substrate.AggregateBucket{Every: "PT15M"}, rng(from, to), 0},
		{"health/steps", substrate.AggregateBucket{Every: "PT2H"}, rng(from.Add(15*time.Minute), to), 29},
		{"air_temperature", substrate.AggregateBucket{Every: "PT1H"}, rng(from, to), 0},
	} {
		red := "sum"
		if x.metric == "air_temperature" {
			red = "avg"
		}
		_, _, rows := f.bothPaths(substrate.Filter{}, substrate.Aggregate{
			Metric: &substrate.AggregateMetric{Ref: f.metricPath(x.metric)}, Range: x.rng, Bucket: &x.bucket,
			Reducers: reducers(red),
		})
		if rows != x.rows {
			t.Errorf("%s %s: took %d rollup rows, want %d", x.metric, x.bucket.Every, rows, x.rows)
		}
	}
}
