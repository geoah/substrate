package engine_test

// Sources in the aggregate read (decision 0161): how a filter and an order
// name them, the order a source record keeps, and the streams with no about
// or no channel.

import (
	"context"
	"fmt"
	"slices"
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
// id, 1 first, an unranked source last; an order the request names wins.
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
			props["priorities"] = map[string]any{"health/body_mass": x.rank, "health/heart_rate": 9}
		}
		f.mustPut(t, f.source, x.id, props)
		f.weighing("w/"+x.id, x.id, x.value, at, nil)
		f.mustPut(t, f.observation, "day/"+x.id, map[string]any{
			"metric": f.metricRef("health/body_mass"), "source": f.sourceRef(x.id), "form": "summary",
			"statistic": "avg", "period": "day", "value": quantity(x.value, "kg"),
			"at": at.Format(time.RFC3339), "endsAt": at.Add(time.Hour).Format(time.RFC3339), "utcOffset": 0,
		})
	}
	read := func(sources substrate.AggregateSources) string {
		res := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/body_mass")},
			Range:    rng(at.Add(-time.Hour), at.Add(time.Hour)),
			Reducers: reducers("max"),
			Sources:  &sources,
		})
		if len(res.Rows) != 1 {
			t.Fatalf("%+v: %d rows, want one", sources, len(res.Rows))
		}
		v, _ := val(res.Rows[0], "max")
		return v
	}
	for _, x := range []struct {
		sources substrate.AggregateSources
		want    string
	}{
		{substrate.AggregateSources{Policy: "priority"}, "80"},
		{substrate.AggregateSources{Policy: "summaries"}, "80"},
		{substrate.AggregateSources{Policy: "priority", Order: []string{"alpha"}}, "70"},
		{substrate.AggregateSources{Policy: "summaries", Order: []string{"aardvark", "zeta"}}, "60"},
	} {
		if got := read(x.sources); got != x.want {
			t.Errorf("%+v answers %s, want %s", x.sources, got, x.want)
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
	for _, sources := range []substrate.AggregateSources{
		{Policy: "priority"},
		{Policy: "summaries"},
		{Policy: "priority", Order: []string{"no-such-scale"}},
		{Policy: "summaries", Order: []string{"no-such-scale"}},
	} {
		res := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
			Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/body_mass")},
			Range:    rng(at.Add(-time.Hour), at.Add(time.Hour)),
			Reducers: reducers("max"),
			Sources:  &sources,
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
