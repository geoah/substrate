package engine_test

// Sources in the aggregate read (decision 0161): how a filter and an order
// name them, the order a source record keeps, and the streams with no about
// or no channel.

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// weighing writes one body weight from a source at an instant, held a
// minute; extra properties (about, channel) are merged in.
func (f aggFix) weighing(id, source, value string, at time.Time, extra map[string]any) {
	f.t.Helper()
	props := map[string]any{
		"metric": f.metricRef("health/body_mass"), "source": f.sourceRef(source), "form": "gauge",
		"value": quantity(value, "kg"), "at": at.Format(time.RFC3339), "hold": "PT1M", "utcOffset": 0,
	}
	for k, v := range extra {
		props[k] = v
	}
	f.mustPut(f.t, f.observation, id, props)
}

// A filter and a priority order name a source by its record path or by its
// bare id, which the read completes with the kind the selected kinds' source
// reference pins, exactly as a list filter does: `manual` is the source
// `manual`, never `googlefit/x/manual`, which also ends in `/manual`. A path
// no stream holds answers no rows and ranks nothing; a bare id where the
// reference pins no kind (about points at any kind) is a 422 naming it.
func TestAggregateSourcesResolveBareIDsExactly(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	at := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	f.weighing("w/manual", "manual", "70", at, nil)
	f.weighing("w/fit", "googlefit/x/manual", "80", at, nil)
	read := func(filter substrate.Filter, sources *substrate.AggregateSources) *substrate.AggregateResult {
		return f.agg(context.Background(), filter, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/body_mass")},
			Range:    rng(at.Add(-time.Hour), at.Add(time.Hour)),
			Reducers: reducers("max"),
			Sources:  sources,
		})
	}
	source := func(c substrate.Cond) substrate.Filter {
		return substrate.Filter{Properties: map[string]substrate.Cond{"source": c}}
	}
	for _, c := range []substrate.Cond{{Eq: "manual"}, {Eq: map[string]any{"ref": "manual"}}, {In: []any{"manual"}}, {Eq: f.sourcePath("manual")}} {
		res := read(source(c), nil)
		if len(res.Rows) != 1 || len(res.Sources) != 1 || res.Sources[0] != f.sourcePath("manual") {
			t.Errorf("source %+v: rows %+v sources %v, want the one row of %s", c, res.Rows, res.Sources, f.sourcePath("manual"))
		}
	}
	if res := read(source(substrate.Cond{Eq: "x/manual"}), nil); len(res.Rows) != 0 {
		t.Errorf("x/manual names no stream's source; the read answered %d rows", len(res.Rows))
	}
	for _, order := range [][]string{{"manual"}, {"no-such-source", "manual"}, {f.sourcePath("manual")}} {
		res := read(substrate.Filter{}, &substrate.AggregateSources{Policy: "priority", Order: order})
		if v, _ := val(res.Rows[0], "max"); len(res.Rows) != 1 || v != "70" {
			t.Errorf("priority %v: %+v, want manual's 70 ahead of googlefit/x/manual's 80", order, res.Rows)
		}
	}
	f.refusedAgg(substrate.AggregateQuery{
		Filter: substrate.Filter{Properties: map[string]substrate.Cond{"about": {Eq: "kitchen"}}},
		Aggregate: &substrate.Aggregate{
			Metric: &substrate.AggregateMetric{Ref: f.metricPath("health/body_mass")}, Range: rng(at.Add(-time.Hour), at.Add(time.Hour)),
			Reducers: reducers("max"),
		},
	}, "kitchen")
}

// Under the priority and summaries policies a read that names no order ranks
// the selected sources by each source record's priorities for the metric's
// record path, 1 first, an unranked source last; an order the request names
// wins.
// Three scales weigh at one instant: aardvark (unranked) 60, alpha (ranked
// 2) 70 and zeta (ranked 1) 80; by path alone aardvark would win.
func TestAggregatePriorityTakesTheSourcesOwnRanks(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	at := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	for _, x := range []struct {
		id, value string
		rank      any
	}{{"aardvark", "60", nil}, {"alpha", "70", 2}, {"zeta", "80", 1}} {
		props := map[string]any{"name": x.id}
		if x.rank != nil {
			props["priorities"] = map[string]any{f.metricPath("health/body_mass"): x.rank, f.metricPath("health/heart_rate"): 9}
		}
		f.mustPut(t, f.source, x.id, props)
		f.weighing("w/"+x.id, x.id, x.value, at, nil)
		f.mustPut(t, f.observation, "day/"+x.id, map[string]any{
			"metric": f.metricRef("health/body_mass"), "source": f.sourceRef(x.id), "form": "summary",
			"statistic": "avg", "period": "day", "value": quantity(x.value, "kg"),
			"at": at.Format(time.RFC3339), "endsAt": at.Add(time.Hour).Format(time.RFC3339), "utcOffset": 0,
		})
	}
	read := func(sources substrate.AggregateSources, from string) string {
		res := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/body_mass")},
			Range:    rng(at.Add(-time.Hour), at.Add(time.Hour)),
			Reducers: reducers("max"),
			Sources:  &sources,
			From:     from,
		})
		if len(res.Rows) != 1 {
			t.Fatalf("%+v: %d rows, want one", sources, len(res.Rows))
		}
		v, _ := val(res.Rows[0], "max")
		return v
	}
	for _, x := range []struct {
		sources    substrate.AggregateSources
		from, want string
	}{
		{substrate.AggregateSources{Policy: "priority"}, "", "80"},
		{substrate.AggregateSources{Policy: "priority"}, "summaries", "80"},
		{substrate.AggregateSources{Policy: "priority", Order: []string{"alpha"}}, "", "70"},
		{substrate.AggregateSources{Policy: "priority", Order: []string{"aardvark", "zeta"}}, "summaries", "60"},
	} {
		if got := read(x.sources, x.from); got != x.want {
			t.Errorf("%+v from %q answers %s, want %s", x.sources, x.from, got, x.want)
		}
	}
}

// A metric read's filter selects the streams with no about, no channel or
// no source by `exists: false` (and the ones with one by `exists: true`),
// and `in` takes null for the absent key, so one read asks for "no channel
// or workout". exists beside eq or in admits what both admit. A source
// weighs 60 with no channel, 61 on its workout channel and 62 about a
// kitchen; a weighing with no source weighs 63.
func TestAggregateFilterSelectsAbsentStreamKeys(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	at := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	f.weighing("w/plain", "scale", "60", at, nil)
	f.weighing("w/workout", "scale", "61", at, map[string]any{"channel": "workout"})
	f.weighing("w/kitchen", "scale", "62", at, map[string]any{"about": map[string]any{"ref": f.sourcePath("kitchen")}})
	f.mustPut(t, f.observation, "w/nosource", map[string]any{
		"metric": f.metricRef("health/body_mass"), "form": "gauge", "value": quantity("63", "kg"),
		"at": at.Format(time.RFC3339), "hold": "PT1M", "utcOffset": 0,
	})
	read := func(props map[string]substrate.Cond) []string {
		res := f.agg(context.Background(), substrate.Filter{Properties: props}, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/body_mass")},
			Range:    rng(at.Add(-time.Hour), at.Add(time.Hour)),
			GroupBy:  []string{"source", "about", "channel"},
			Reducers: reducers("max"),
		})
		var out []string
		for _, row := range res.Rows {
			v, _ := val(row, "max")
			out = append(out, v)
		}
		return out
	}
	yes, no := true, false
	for _, x := range []struct {
		label string
		props map[string]substrate.Cond
		want  string
	}{
		{"no channel", map[string]substrate.Cond{"channel": {Exists: &no}}, "[63 60 62]"},
		{"a channel", map[string]substrate.Cond{"channel": {Exists: &yes}}, "[61]"},
		{"no channel or workout", map[string]substrate.Cond{"channel": {In: []any{nil, "workout"}}, "about": {Exists: &no}, "source": {Exists: &yes}}, "[60 61]"},
		{"about something", map[string]substrate.Cond{"about": {Exists: &yes}}, "[62]"},
		{"no source", map[string]substrate.Cond{"source": {Exists: &no}}, "[63]"},
		{"no channel and workout", map[string]substrate.Cond{"channel": {Exists: &no, Eq: "workout"}}, "[]"},
	} {
		if got := fmt.Sprint(read(x.props)); got != x.want {
			t.Errorf("%s: %s, want %s", x.label, got, x.want)
		}
	}
	f.refusedAgg(substrate.AggregateQuery{
		Filter: substrate.Filter{Properties: map[string]substrate.Cond{"channel": {Eq: ""}}},
		Aggregate: &substrate.Aggregate{
			Metric: &substrate.AggregateMetric{Ref: f.metricPath("health/body_mass")}, Range: rng(at.Add(-time.Hour), at.Add(time.Hour)),
			Reducers: reducers("max"),
		},
	}, "exists")
}

// A stream that names no source (a weight typed in by hand) ranks after
// every named source under the priority and summaries policies: where no
// order is named, after the sources the records rank and those they do not,
// and where one is, after the sources it does not name too. A scale with no
// priorities weighs 60 and the typed weight 63 at one instant, each beside a
// day summary of the same; by path alone the typed weight, whose source is
// "", would sort first.
func TestAggregatePriorityRanksAStreamWithNoSourceLast(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	at := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	f.weighing("w/scale", "scale", "60", at, nil)
	f.mustPut(t, f.observation, "w/typed", map[string]any{
		"metric": f.metricRef("health/body_mass"), "form": "gauge", "value": quantity("63", "kg"),
		"at": at.Format(time.RFC3339), "hold": "PT1M", "utcOffset": 0,
	})
	for id, x := range map[string]map[string]any{"day/scale": {"source": f.sourceRef("scale"), "value": quantity("60", "kg")}, "day/typed": {"value": quantity("63", "kg")}} {
		props := map[string]any{
			"metric": f.metricRef("health/body_mass"), "form": "summary", "statistic": "avg", "period": "day",
			"at": at.Format(time.RFC3339), "endsAt": at.Add(time.Hour).Format(time.RFC3339), "utcOffset": 0,
		}
		for k, v := range x {
			props[k] = v
		}
		f.mustPut(t, f.observation, id, props)
	}
	for _, x := range []struct {
		sources substrate.AggregateSources
		from    string
	}{
		{substrate.AggregateSources{Policy: "priority"}, ""},
		{substrate.AggregateSources{Policy: "priority"}, "summaries"},
		{substrate.AggregateSources{Policy: "priority", Order: []string{"no-such-scale"}}, ""},
		{substrate.AggregateSources{Policy: "priority", Order: []string{"no-such-scale"}}, "summaries"},
	} {
		sources := x.sources
		res := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/body_mass")},
			Range:    rng(at.Add(-time.Hour), at.Add(time.Hour)),
			Reducers: reducers("max"),
			Sources:  &sources,
			From:     x.from,
		})
		if len(res.Rows) != 1 {
			t.Fatalf("%+v: %d rows, want one", sources, len(res.Rows))
		}
		if v, _ := val(res.Rows[0], "max"); v != "60" {
			t.Errorf("%+v answers %s, want the scale's 60 ahead of the typed 63", sources, v)
		}
	}
}

// A stream that names no source feeds a row and adds no path to its sources
// or the result's, which list record paths, as the latest read does.
func TestAggregateSourcesListNoPathForAStreamWithNoSource(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	at := time.Date(2026, 10, 4, 8, 0, 0, 0, time.UTC)
	f.mustPut(t, f.observation, "typed", map[string]any{
		"metric": f.metricRef("health/heart_rate"), "form": "gauge", "value": quantity("61", "/min"),
		"at": at.Format(time.RFC3339), "hold": "PT1M", "utcOffset": 0,
	})
	f.weighing("w", "scale", "70", at, nil)
	for _, metric := range []string{"health/heart_rate", "health/body_mass"} {
		for _, bucket := range []*substrate.AggregateBucket{nil, {Calendar: "day", Timezone: "UTC"}} {
			res := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
				Metric: &substrate.AggregateMetric{Ref: f.metricPath(metric)}, Range: rng(at.Add(-time.Hour), at.Add(time.Hour)),
				Bucket: bucket, Reducers: reducers("avg"),
			})
			want := []string(nil)
			if metric == "health/body_mass" {
				want = []string{f.sourcePath("scale")}
			}
			if len(res.Rows) != 1 || !slices.Equal(res.Rows[0].Sources, want) || !slices.Equal(res.Sources, want) {
				t.Errorf("%s bucket %+v: rows %+v, sources %q; want one row, sources %q on it and the result", metric, bucket, res.Rows, res.Sources, want)
			}
		}
	}
}

// A gauge held for no time (hold PT0S, as the catalogue's blood pressures
// are) has no held interval, so priority keeps one reading per instant by
// the instant itself: a cuff read by two apps at one instant is one reading,
// the higher source's. A reading seconds apart is another instant and stays.
func TestAggregatePriorityDropsALowerReadingAtTheSameInstant(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	at := time.Date(2026, 9, 27, 5, 51, 0, 0, time.UTC)
	pressure := func(id, source, value string, at time.Time) {
		f.mustPut(t, f.observation, id, map[string]any{
			"metric": f.metricRef("health/systolic_blood_pressure"), "source": f.sourceRef(source), "form": "gauge",
			"value": quantity(value, "mm[Hg]"), "at": at.Format(time.RFC3339), "hold": "PT0S", "utcOffset": 0,
		})
	}
	pressure("bp/garmin", "garmin", "118", at)
	pressure("bp/fit", "fit", "121", at)
	pressure("bp/apple", "apple", "119", at.Add(30*time.Second))
	read := func(order ...string) substrate.AggregateRow {
		res := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/systolic_blood_pressure")},
			Range:    rng(at.Add(-time.Hour), at.Add(time.Hour)),
			Reducers: reducers("count", "min", "max"),
			Sources:  &substrate.AggregateSources{Policy: "priority", Order: order},
		})
		if len(res.Rows) != 1 {
			t.Fatalf("priority %v: %d rows, want one", order, len(res.Rows))
		}
		return res.Rows[0]
	}
	for _, x := range []struct {
		order               []string
		count, lo, hi, from string
	}{
		{[]string{"garmin", "fit"}, "2", "118", "119", "garmin"},
		{[]string{"fit", "garmin"}, "2", "119", "121", "fit"},
		{[]string{"apple", "fit", "garmin"}, "2", "119", "121", "fit"},
	} {
		row := read(x.order...)
		count, _ := val(row, "count")
		lo, _ := val(row, "min")
		hi, _ := val(row, "max")
		if count != x.count || lo != x.lo || hi != x.hi || !slices.Contains(row.Sources, f.sourcePath(x.from)) || len(row.Sources) != 2 {
			t.Errorf("priority %v: count %s, min %s, max %s, sources %v; want %s, %s, %s from %s", x.order, count, lo, hi, row.Sources, x.count, x.lo, x.hi, x.from)
		}
	}
}

// Under priority a lower source's amount is dropped where it overlaps a
// higher source's, and the higher one may lie outside the read's windows:
// A's 100 steps from 23:30 to 00:30 drop B's 40 from 00:00 to 00:15 whatever
// day the range starts on. So one day read alone, read after an earlier
// range start, and read with a rolling window answer the same row, B's 10
// from 10:00 alone, whether the order ranks A first or neither source is
// ranked and the stream order breaks the tie, and after A writes a gauge the
// next day. In the records mode a window that A's amount does not meet
// answers the same with or without the window that takes A's amount.
func TestAggregatePriorityWeighsAHigherPointOutsideTheWindows(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	steps := func(id, source, value string, at time.Time, span time.Duration) {
		f.mustPut(t, f.observation, id, map[string]any{
			"metric": f.metricRef("health/steps"), "source": f.sourceRef(source), "form": "amount",
			"value": quantity(value, "{steps}"), "at": at.Format(time.RFC3339), "endsAt": at.Add(span).Format(time.RFC3339), "utcOffset": 0,
		})
	}
	day := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	steps("a/night", "a", "100", day.Add(-30*time.Minute), time.Hour)
	steps("b/midnight", "b", "40", day, 15*time.Minute)
	steps("b/morning", "b", "10", day.Add(10*time.Hour), 15*time.Minute)
	priority := &substrate.AggregateSources{Policy: "priority", Order: []string{"a", "b"}}
	read := func(sources *substrate.AggregateSources, from time.Time, rolling *substrate.AggregateRolling) map[string]string {
		res := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
			Metric: &substrate.AggregateMetric{Ref: f.metricPath("health/steps")},
			Range:  rng(from, day.AddDate(0, 0, 1)), Bucket: &substrate.AggregateBucket{Calendar: "day", Timezone: "UTC"},
			Reducers: reducers("sum"), Sources: sources, Rolling: rolling,
		})
		out := map[string]string{}
		for _, row := range res.Rows {
			v, _ := val(row, "sum")
			out[row.Start.Format("01-02")] = v
		}
		return out
	}
	for _, sources := range []*substrate.AggregateSources{priority, {Policy: "priority"}} {
		for _, x := range []struct {
			label   string
			from    time.Time
			rolling *substrate.AggregateRolling
		}{
			{"the day alone", day, nil},
			{"from the day before", day.AddDate(0, 0, -1), nil},
			{"with a rolling window", day, &substrate.AggregateRolling{Buckets: 2, Reducers: reducers("sum")}},
		} {
			if got := read(sources, x.from, x.rolling)["10-06"]; got != "10" {
				t.Errorf("order %v, %s: 10-06 sums %s, want B's 10 from 10:00 alone", sources.Order, x.label, got)
			}
		}
	}

	// A gauge A writes the day after leaves A's amount weighed: a stream's
	// newest form says nothing of the forms before it.
	f.mustPut(t, f.observation, "a/gauge", map[string]any{
		"metric": f.metricRef("health/steps"), "source": f.sourceRef("a"), "form": "gauge",
		"value": quantity("5", "{steps}"), "at": day.AddDate(0, 0, 1).Format(time.RFC3339), "utcOffset": 0,
	})
	for _, sources := range []*substrate.AggregateSources{priority, {Policy: "priority"}} {
		if got := read(sources, day, nil)["10-06"]; got != "10" {
			t.Errorf("order %v, after A's gauge: 10-06 sums %s, want B's 10 from 10:00 alone", sources.Order, got)
		}
	}

	sleep := f.sleepKind()
	f.mustPut(t, sleep, "v", map[string]any{"at": at(day.Add(-20 * time.Minute)), "endsAt": at(day.Add(-10 * time.Minute))})
	f.mustPut(t, sleep, "w", map[string]any{"at": at(day.Add(31 * time.Minute)), "endsAt": at(day.Add(40 * time.Minute))})
	steps("b/late", "b", "30", day.Add(25*time.Minute), 15*time.Minute)
	windows := func(ids ...string) map[string]string {
		res := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/steps")},
			Range:    rng(day.Add(-time.Hour), day.Add(time.Hour)),
			Bucket:   &substrate.AggregateBucket{Records: &substrate.AggregateRecords{Filter: substrate.Filter{Kinds: []string{sleep}, IDs: ids}}},
			Reducers: reducers("sum"), Sources: priority,
		})
		out := map[string]string{}
		for _, row := range res.Rows {
			v, _ := val(row, "sum")
			out[strings.TrimPrefix(row.Record.Ref, sleep+"/")] = v
		}
		return out
	}
	alone, beside := windows("w"), windows("v", "w")
	if alone["w"] != beside["w"] || alone["w"] != "" {
		t.Errorf("window w alone answers %q, beside v %q; want no row either way: A's amount drops B's", alone["w"], beside["w"])
	}
}

// A higher source's older value can be longer than any stream's newest, and
// a chain of them can reach further than one span times the ranks: each
// link is bounded by the values ending where the link before it can start.
// Under [a, b], A's twelve-hour amount ending at 02:00 drops B's 01:00 one,
// read from the day or from the day before. Under [a, b, c], B's six-hour
// amount over midnight would drop C's, but A's 33-hour one, starting before
// six hours times three ranks reach, drops B's first, so C's stands.
func TestAggregatePriorityReachesAnOlderLongerSpan(t *testing.T) {
	t.Parallel()
	day := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	for _, x := range []struct {
		label  string
		order  []string
		points map[string][3]any // id: source, value, [start, span]
		want   string            // 10-06's sum, "" for no row
	}{
		{"two ranks", []string{"a", "b"}, map[string][3]any{
			"a/long": {"a", "100", [2]time.Duration{-10 * time.Hour, 12 * time.Hour}},
			"b/one":  {"b", "40", [2]time.Duration{time.Hour, 15 * time.Minute}},
		}, ""},
		{"three ranks", []string{"a", "b", "c"}, map[string][3]any{
			"a/longest": {"a", "100", [2]time.Duration{-36 * time.Hour, 33 * time.Hour}},
			"b/long":    {"b", "70", [2]time.Duration{-4 * time.Hour, 6 * time.Hour}},
			"c/one":     {"c", "40", [2]time.Duration{time.Hour, 15 * time.Minute}},
		}, "40"},
	} {
		t.Run(x.label, func(t *testing.T) {
			t.Parallel()
			f := openAgg(t)
			steps := func(id, source, value string, at time.Time, span time.Duration) {
				f.mustPut(t, f.observation, id, map[string]any{
					"metric": f.metricRef("health/steps"), "source": f.sourceRef(source), "form": "amount",
					"value": quantity(value, "{steps}"), "at": at.Format(time.RFC3339), "endsAt": at.Add(span).Format(time.RFC3339), "utcOffset": 0,
				})
			}
			for id, pt := range x.points {
				when := pt[2].([2]time.Duration)
				steps(id, pt[0].(string), pt[1].(string), day.Add(when[0]), when[1])
			}
			// Each stream's newest value is a quarter hour long.
			for i, src := range x.order {
				steps(src+"/new", src, "5", day.AddDate(0, 0, 4).Add(time.Duration(i)*time.Hour), 15*time.Minute)
			}
			for _, read := range []struct {
				from    time.Time
				rolling *substrate.AggregateRolling
			}{
				{day, nil},
				{day.AddDate(0, 0, -1), nil},
				{day.AddDate(0, 0, -3), nil},
				{day, &substrate.AggregateRolling{Buckets: 2, Reducers: reducers("sum")}},
			} {
				res := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
					Metric: &substrate.AggregateMetric{Ref: f.metricPath("health/steps")},
					Range:  rng(read.from, day.AddDate(0, 0, 1)), Bucket: &substrate.AggregateBucket{Calendar: "day", Timezone: "UTC"},
					Reducers: reducers("sum"), Sources: &substrate.AggregateSources{Policy: "priority", Order: x.order},
					Rolling: read.rolling,
				})
				got := ""
				for _, row := range res.Rows {
					if row.Start.Format("01-02") == "10-06" {
						got, _ = val(row, "sum")
					}
				}
				if got != x.want {
					t.Errorf("from %s (rolling %v): 10-06 sums %q, want %q", read.from.Format("01-02"), read.rolling != nil, got, x.want)
				}
			}
		})
	}
}

// One scale reaching the repository through its maker's app (withings,
// ranked 1) and through Apple Health (apple, ranked 2) is two sources, and
// Apple Health stamps the 07:00 weigh-in two seconds early, outside the
// higher source's hold, so the priority policy alone counts it twice. A
// record of the observations sample's `sourcelink` kind, binding core's
// `sourcelink`, says the two are one device (decision 0187): under priority
// the device answers from its highest-ranked source alone, so the weigh-in
// counts once, and an order naming apple first makes apple that source. A
// gym scale no link names keeps its own reading, a weighing about someone
// else is a stream of its own (read beside the owner's, grouped by about),
// `separate` ignores the link, two links sharing a source join, their four
// references are charged to maxSamples, and deleting the links counts the
// weigh-in twice again.
func TestAggregatePriorityTakesALinkedDeviceFromOneSource(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := openAgg(t)
	link := f.source + "link"
	at := time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC)
	for id, rank := range map[string]any{"withings": 1, "apple": 2, "gym": nil, "healthconnect": nil} {
		props := map[string]any{"name": id}
		if rank != nil {
			props["priorities"] = map[string]any{f.metricPath("health/body_mass"): rank}
		}
		f.mustPut(t, f.source, id, props)
	}
	f.weighing("w/withings", "withings", "72.4", at, nil)
	f.weighing("w/apple", "apple", "72.4", at.Add(-2*time.Second), nil)
	f.weighing("w/gym", "gym", "73.1", at.Add(11*time.Hour), nil)
	bob := map[string]any{"about": map[string]any{"ref": f.sourcePath("bob")}}
	f.weighing("w/bob", "apple", "81", at.Add(time.Hour), bob)
	read := func(about bool, sources substrate.AggregateSources) *substrate.AggregateResult {
		cond := substrate.Cond{Exists: &about}
		return f.agg(ctx, substrate.Filter{Properties: map[string]substrate.Cond{"about": cond}}, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/body_mass")},
			Range:    rng(at.Add(-time.Hour), at.Add(23*time.Hour)),
			Reducers: reducers("count"),
			Sources:  &sources,
		})
	}
	priority := func(order ...string) substrate.AggregateSources {
		return substrate.AggregateSources{Policy: "priority", Order: order}
	}
	check := func(label string, res *substrate.AggregateResult, count string, sources ...string) {
		t.Helper()
		var want []string
		for _, s := range sources {
			want = append(want, f.sourcePath(s))
		}
		slices.Sort(want)
		if len(res.Rows) != 1 {
			t.Fatalf("%s: %d rows, want one: %+v", label, len(res.Rows), res.Rows)
		}
		got, _ := val(res.Rows[0], "count")
		if got != count || !slices.Equal(res.Sources, want) {
			t.Errorf("%s: count %s from %v, want %s from %v", label, got, res.Sources, count, want)
		}
	}
	check("unlinked", read(false, priority()), "3", "apple", "gym", "withings")

	f.mustPut(t, link, "scale", map[string]any{
		"name": "Withings Body+", "sources": []any{f.sourceRef("withings"), f.sourceRef("apple")},
	})
	check("linked", read(false, priority()), "2", "gym", "withings")
	check("linked, apple ordered first", read(false, priority("apple")), "2", "apple", "gym")
	bySubject := f.agg(ctx, substrate.Filter{}, substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/body_mass")},
		Range:    rng(at.Add(-time.Hour), at.Add(23*time.Hour)),
		GroupBy:  []string{"about"},
		Reducers: reducers("count"),
		Sources:  &substrate.AggregateSources{Policy: "priority"},
	})
	counts := map[string]string{}
	for _, row := range bySubject.Rows {
		counts[row.Group.About], _ = val(row, "count")
	}
	if len(counts) != 2 || counts[""] != "2" || counts[f.sourcePath("bob")] != "1" {
		t.Errorf("grouped by about: %v, want the owner's 2 (gym, withings) and bob's 1 from apple, which withings does not weigh for", counts)
	}
	if res := read(false, substrate.AggregateSources{Policy: "separate"}); len(res.Rows) != 3 {
		t.Errorf("separate: %d rows, want one per source, the link ignored", len(res.Rows))
	}

	if _, err := f.ds.Delete(ctx, substrate.ActorAPI, link, "scale", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the link: %v", err)
	}
	f.mustPut(t, link, "a", map[string]any{"name": "a", "sources": []any{f.sourceRef("withings"), f.sourceRef("healthconnect")}})
	f.mustPut(t, link, "b", map[string]any{"name": "b", "sources": []any{f.sourceRef("healthconnect"), f.sourceRef("apple")}})
	check("linked through healthconnect", read(false, priority()), "2", "gym", "withings")
	f.refusedAgg(substrate.AggregateQuery{Aggregate: &substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/body_mass")},
		Range:    rng(at.Add(-time.Hour), at.Add(23*time.Hour)),
		Reducers: reducers("count"),
		Sources:  &substrate.AggregateSources{Policy: "priority"},
		Budget:   &substrate.AggregateBudget{MaxSamples: 3},
	}}, "the source links the priority policy reads")

	for _, id := range []string{"a", "b"} {
		if _, err := f.ds.Delete(ctx, substrate.ActorAPI, link, id, substrate.DeleteInput{}); err != nil {
			t.Fatalf("delete link %s: %v", id, err)
		}
	}
	check("links deleted", read(false, priority()), "3", "apple", "gym", "withings")
}

// The winner of a linked device is chosen among the streams that hold a value
// in the read's range and `from`, so a higher-ranked source with nothing
// there hides nothing: withings (ranked 1) weighs on 2 September and apple
// (ranked 2) on 1 September, so a read of 1 September answers apple's 70; and
// withings writes only raw weighings on 1 September while apple writes a
// daily average, so a read from summaries answers apple's 71.
func TestAggregatePriorityChoosesALinkedWinnerInTheRead(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for id, rank := range map[string]int{"withings": 1, "apple": 2} {
		f.mustPut(t, f.source, id, map[string]any{"name": id, "priorities": map[string]any{f.metricPath("health/body_mass"): rank}})
	}
	f.mustPut(t, f.source+"link", "scale", map[string]any{
		"name": "scale", "sources": []any{f.sourceRef("withings"), f.sourceRef("apple")},
	})
	f.weighing("w/withings/02", "withings", "72", day.Add(31*time.Hour), nil)
	f.weighing("w/withings/01", "withings", "73", day.Add(20*time.Hour), nil)
	f.weighing("w/apple/01", "apple", "70", day.Add(7*time.Hour), nil)
	f.mustPut(t, f.observation, "day/apple/01", map[string]any{
		"metric": f.metricRef("health/body_mass"), "source": f.sourceRef("apple"), "form": "summary",
		"statistic": "avg", "period": "day", "value": quantity("71", "kg"),
		"at": day.Format(time.RFC3339), "endsAt": day.Add(24 * time.Hour).Format(time.RFC3339), "utcOffset": 0,
	})
	read := func(from string, lt time.Time) []string {
		res := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/body_mass")},
			Range:    rng(day, lt),
			Reducers: reducers("min"),
			Sources:  &substrate.AggregateSources{Policy: "priority"},
			From:     from,
		})
		var out []string
		for _, row := range res.Rows {
			v, _ := val(row, "min")
			out = append(out, v)
		}
		return out
	}
	if got := read("", day.Add(12*time.Hour)); !slices.Equal(got, []string{"70"}) {
		t.Errorf("1 September's morning, raw: %v, want apple's 70; withings has no value in the range", got)
	}
	if got := read("summaries", day.Add(24*time.Hour)); !slices.Equal(got, []string{"71"}) {
		t.Errorf("1 September from summaries: %v, want apple's 71; withings has no summary", got)
	}
	if got := read("", day.Add(24*time.Hour)); !slices.Equal(got, []string{"73"}) {
		t.Errorf("1 September, raw: %v, want withings' 73 alone, the device's winner in the range", got)
	}
}
