package engine_test

// Sources in the aggregate read (decision 0161): how a filter and an order
// name them, the order a source record keeps, and the streams with no about
// or no channel.

import (
	"context"
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
