package engine_test

// Where the aggregate read places a value in time (decision 0161): a value
// written without a UTC offset in the recorded-local calendar.

import (
	"context"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// A recorded-local day places each value by its own UTC offset, so a value
// written without one has no day: the read refuses it, naming the stream and
// bucket.fallbackTimezone, instead of placing it on its UTC date. With a
// fallback zone it lands where that zone's clock puts it, and the row counts
// it in offsetless. A calendar in a named zone places every value by that
// zone and is unchanged. Two weighings at 22:30Z on 1 October, one at +03:00
// and one with no offset: both are 2 October in Athens.
func TestAggregateRecordedLocalPlacesAnOffsetlessValueByTheFallbackZone(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	weighed := time.Date(2026, 10, 1, 22, 30, 0, 0, time.UTC)
	for _, x := range []struct {
		id     string
		value  string
		offset any
	}{{"w/known", "70", 180}, {"w/none", "71", nil}} {
		props := map[string]any{
			"metric": f.metricRef("health/body_mass"), "source": f.sourceRef("scale"), "form": "gauge",
			"value": quantity(x.value, "kg"), "at": weighed.Format(time.RFC3339), "hold": "PT0S",
		}
		if x.offset != nil {
			props["utcOffset"] = x.offset
		}
		f.mustPut(t, f.observation, x.id, props)
	}
	plus3 := time.FixedZone("", 3*3600)
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, plus3)
	read := func(bucket substrate.AggregateBucket) substrate.Aggregate {
		return substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/body_mass")},
			Range:    rng(day, day.AddDate(0, 0, 3)),
			Bucket:   &bucket,
			Reducers: reducers("count", "max"),
		}
	}
	f.refusedAgg(substrate.AggregateQuery{Aggregate: ptr(read(substrate.AggregateBucket{Calendar: "day"}))}, "bucket.fallbackTimezone")
	f.refusedAgg(substrate.AggregateQuery{Aggregate: ptr(read(substrate.AggregateBucket{Calendar: "day"}))}, f.sourcePath("scale"))
	f.refusedAgg(substrate.AggregateQuery{Aggregate: ptr(read(substrate.AggregateBucket{Calendar: "day", Timezone: "Europe/Athens", FallbackTimezone: "Europe/Athens"}))},
		"fallbackTimezone")

	res, _, _ := f.bothPaths(substrate.Filter{}, read(substrate.AggregateBucket{Calendar: "day", FallbackTimezone: "Europe/Athens"}))
	if len(res.Rows) != 1 {
		t.Fatalf("with a fallback zone: %d rows, want one day", len(res.Rows))
	}
	row := res.Rows[0]
	if got := row.Start.Format(time.RFC3339); got != "2026-10-02T00:00:00+03:00" {
		t.Errorf("the day starts %s, want 2 October at +03:00", got)
	}
	if c, _ := val(row, "count"); c != "2" || row.Offsetless != 1 {
		t.Errorf("the day counts %s points, %d of them offsetless; want 2 and 1", c, row.Offsetless)
	}
	if m, _ := val(row, "max"); m != "71" {
		t.Errorf("max = %s, want the offsetless 71", m)
	}

	named := f.agg(context.Background(), substrate.Filter{}, read(substrate.AggregateBucket{Calendar: "day", Timezone: "Europe/Athens"}))
	if len(named.Rows) != 1 || named.Rows[0].Offsetless != 0 {
		t.Fatalf("Athens days: %+v, want one day with nothing counted offsetless", named.Rows)
	}
	if c, _ := val(named.Rows[0], "count"); c != "2" {
		t.Errorf("the Athens day counts %s, want 2", c)
	}
}
