package engine_test

// Where the aggregate read places a value in time (decision 0161): a value
// written without a UTC offset in the recorded-local calendar.

import (
	"context"
	"fmt"
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

// bucket.place "end" places a value with a span (an amount, a summary) in
// the bucket its last instant lies in, so a night lands on the day it ends:
// four nights by Athens day put two nights on 3 September and none on the
// 2nd by start, and one night on each day by end, the first starting before
// the range. A gauge's or a counter's points are instants, so place end is
// refused for them, and in the records mode.
func TestAggregatePlacesANightByItsEnd(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	plus3 := time.FixedZone("", 3*3600)
	nights := []struct{ start, end time.Time }{
		{time.Date(2026, 9, 1, 22, 0, 0, 0, plus3), time.Date(2026, 9, 2, 6, 0, 0, 0, plus3)},
		{time.Date(2026, 9, 3, 0, 30, 0, 0, plus3), time.Date(2026, 9, 3, 8, 0, 0, 0, plus3)},
		{time.Date(2026, 9, 3, 23, 0, 0, 0, plus3), time.Date(2026, 9, 4, 7, 0, 0, 0, plus3)},
		{time.Date(2026, 9, 5, 1, 0, 0, 0, plus3), time.Date(2026, 9, 5, 9, 0, 0, 0, plus3)},
	}
	for i, n := range nights {
		f.summary(fmt.Sprintf("hrv/%d", i), "health/hrv_rmssd", "ring", "avg", fmt.Sprint(60+i), "ms", n.start, n.end.Sub(n.start))
	}
	f.mustPut(t, f.observation, "steps/night", map[string]any{
		"metric": f.metricRef("health/steps"), "source": f.sourceRef("ring"), "form": "amount",
		"value": quantity("100", "{steps}"), "utcOffset": 180,
		"at": time.Date(2026, 9, 2, 23, 0, 0, 0, plus3).Format(time.RFC3339), "endsAt": time.Date(2026, 9, 3, 1, 0, 0, 0, plus3).Format(time.RFC3339),
	})
	day := time.Date(2026, 9, 2, 0, 0, 0, 0, plus3)
	read := func(metric, from string, bucket substrate.AggregateBucket, names ...string) substrate.Aggregate {
		return substrate.Aggregate{
			Metric: &substrate.AggregateMetric{Ref: f.metricPath(metric)}, From: from,
			Range: rng(day, day.AddDate(0, 0, 4)), Bucket: &bucket, Reducers: reducers(names...),
		}
	}
	days := func(res *substrate.AggregateResult, reducer string) string {
		var out []string
		for _, row := range res.Rows {
			v, _ := val(row, reducer)
			out = append(out, row.Start.Format("01-02")+"="+v)
		}
		return fmt.Sprint(out)
	}
	for _, bucket := range []substrate.AggregateBucket{{Calendar: "day", Timezone: "Europe/Athens"}, {Calendar: "day"}} {
		byStart := f.agg(context.Background(), substrate.Filter{}, read("health/hrv_rmssd", "summaries", bucket, "count"))
		if got, want := days(byStart, "count"), "[09-03=2 09-05=1]"; got != want {
			t.Errorf("%s nights by start: %s, want %s", bucket.Timezone, got, want)
		}
		bucket.Place = "end"
		byEnd := f.agg(context.Background(), substrate.Filter{}, read("health/hrv_rmssd", "summaries", bucket, "last"))
		if got, want := days(byEnd, "last"), "[09-02=60 09-03=61 09-04=62 09-05=63]"; got != want {
			t.Errorf("%s nights by end: %s, want %s", bucket.Timezone, got, want)
		}
		steps, _, _ := f.bothPaths(substrate.Filter{}, read("health/steps", "", bucket, "sum"))
		if got, want := days(steps, "sum"), "[09-03=100]"; got != want {
			t.Errorf("%s steps by end: %s, want %s", bucket.Timezone, got, want)
		}
	}
	gauge := read("health/body_mass", "", substrate.AggregateBucket{Calendar: "day", Place: "end"}, "count")
	f.weighing("w/1", "ring", "70", day.Add(time.Hour), nil)
	f.refusedAgg(substrate.AggregateQuery{Aggregate: &gauge}, "place")
	records := read("health/hrv_rmssd", "summaries", substrate.AggregateBucket{
		Records: &substrate.AggregateRecords{Filter: substrate.Filter{Kinds: []string{f.observation}}}, Place: "end",
	}, "count")
	f.refusedAgg(substrate.AggregateQuery{Aggregate: &records}, "place")
}
