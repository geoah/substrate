package engine_test

// The observed contracts on the write path: an observed record stores its
// unit's id and no measure; a metric's and a source's records are held to
// core's `metric` and `source` traits; and the aggregate read takes a
// source's priorities by the metric's record path.

import (
	"context"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// Since the unit graph switch (decision 0186) the write path stamps no
// `measure`, which the observations kinds retire: a written one is refused, every
// unit is stored as its unit record's id, and the observed index keeps no
// base value.
func TestObservedRecordsStoreTheUnitsIDAndNoMeasure(t *testing.T) {
	t.Parallel()
	r := openObservedIndex(t)
	for _, c := range []struct {
		kind, id string
		props    map[string]any
		unit     func(map[string]any) any
		want     string
	}{
		{r.observation, "w", map[string]any{
			"metric": r.metricRef("health/body_mass"), "form": "gauge", "value": quantity("160", "[lb_av]"),
			"at": "2026-10-07T06:41:12Z",
		}, func(p map[string]any) any { return p["value"].(map[string]any)["unit"] }, "lb"},
		{r.trace, "hr", map[string]any{
			"metric": r.metricRef("health/heart_rate"), "form": "gauge", "unit": "bpm",
			"at": "2026-10-07T06:00:00Z", "endsAt": "2026-10-07T07:00:00Z", "every": "PT30M", "values": []any{"58", "61"},
		}, func(p map[string]any) any { return p["unit"] }, "1/min"},
	} {
		rec, err := r.put(t, c.kind, c.id, c.props)
		if err != nil {
			t.Fatalf("put %s: %v", c.id, err)
		}
		if got, held := rec.Properties["measure"]; held {
			t.Errorf("%s: measure = %v, want none", c.id, got)
		}
		if got := c.unit(rec.Properties); got != c.want {
			t.Errorf("%s: unit = %v, want the unit's id %s", c.id, got, c.want)
		}
	}
	_, err := r.patch(t, r.observation, "w", map[string]any{"measure": "mass"})
	refused(t, "a written measure", err, "props.measure: not declared")
	r.rebuildMatches(t)
}

// A metric's record is held to the `metric` trait's rules on every write: a
// unit and listed units that name unit records of the repository, each
// listed unit converting to the unit, and rollup tiers from the four widths,
// finest first.
func TestMetricWritesAreHeldToTheMetricTrait(t *testing.T) {
	t.Parallel()
	r := openObserved(t)
	putShippedUnits(t, r.ds, "ppm")
	if _, err := r.put(t, r.metric, "room/co2", map[string]any{
		"name": "CO2", "unit": unitRef("ppm"), "form": "gauge", "rollups": []any{"5m", "1h"},
	}); err != nil {
		t.Fatalf("a metric with its tiers: %v", err)
	}
	_, err := r.patch(t, r.metric, "room/co2", map[string]any{"rollups": []any{"1h", "5m"}})
	refused(t, "tiers out of order", err, "props.rollups[1]: 5m comes after the coarser 1h")
	_, err = r.patch(t, r.metric, "room/co2", map[string]any{"rollups": []any{"10m"}})
	refused(t, "an unknown tier", err, `props.rollups[0]: "10m" is not a rollup tier: 5m, 15m, 1h or 1d`)
	_, err = r.patch(t, r.metric, "room/co2", map[string]any{"units": []any{unitRef("kg")}})
	refused(t, "a listed unit outside the unit's component", err, "props.units[0]: kg does not convert to the metric's unit ppm")
	_, err = r.patch(t, r.metric, "room/co2", map[string]any{"unit": unitRef("furlong")})
	refused(t, "a unit the repository does not hold", err, "props.unit: furlong names no unit of this repository")
	_, err = r.patch(t, r.metric, "room/co2", map[string]any{"measures": "fraction"})
	refused(t, "the table's measures", err, "props.measures: not declared")
	if _, err := r.patch(t, r.metric, "room/co2", map[string]any{"units": []any{unitRef("percent")}}); err != nil {
		t.Errorf("a listed unit of the unit's component: %v", err)
	}
	if _, err := r.patch(t, r.metric, "room/co2", map[string]any{"rollups": []any{}}); err != nil {
		t.Errorf("an empty tier list: %v", err)
	}
}

// A source's priorities are keyed by a metric's full record path; a bare
// metric id, a record of a kind that is not a metric and a rank below 1 are
// refused.
func TestSourcePrioritiesAreKeyedByTheMetricsPath(t *testing.T) {
	t.Parallel()
	r := openObserved(t)
	hr := r.metric + "/health/heart_rate"
	if _, err := r.put(t, r.source, "strap", map[string]any{"name": "Strap", "priorities": map[string]any{hr: 1}}); err != nil {
		t.Fatalf("a source ranked for a metric: %v", err)
	}
	_, err := r.patch(t, r.source, "strap", map[string]any{"priorities": map[string]any{"health/heart_rate": 1}})
	refused(t, "a bare metric id", err, "props.priorities.health/heart_rate: a priority is keyed by the metric's full record path")
	_, err = r.patch(t, r.source, "strap", map[string]any{"priorities": map[string]any{r.source + "/strap": 1}})
	refused(t, "a source as a metric", err, "does not bind substrate.reamde.dev/core/metric")
}

// A metric read names its metric by record path, and the source ranks it
// takes come from the record whose kind binds `source`, keyed by that path:
// the watch ranked first for heart rate answers, and the strap ranked first
// only under a bare id the write path never admits stays unranked.
func TestAggregatePriorityReadsTheSourceTrait(t *testing.T) {
	t.Parallel()
	f := openAgg(t)
	at := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	f.mustPut(t, f.source, "watch", map[string]any{"name": "Watch", "priorities": map[string]any{f.metricPath("health/body_mass"): 1}})
	f.mustPut(t, f.source, "aardvark", map[string]any{"name": "Aardvark"})
	f.weighing("w/watch", "watch", "71", at, nil)
	f.weighing("w/aardvark", "aardvark", "70", at, nil)
	res := f.agg(context.Background(), substrate.Filter{}, substrate.Aggregate{
		Metric:   &substrate.AggregateMetric{Ref: f.metricPath("health/body_mass")},
		Range:    rng(at.Add(-time.Hour), at.Add(time.Hour)),
		Reducers: reducers("max"),
		Sources:  &substrate.AggregateSources{Policy: "priority"},
	})
	if len(res.Rows) != 1 {
		t.Fatalf("%d rows, want one", len(res.Rows))
	}
	if v, _ := val(res.Rows[0], "max"); v != "71" {
		t.Errorf("the ranked watch's weight is %s, want 71", v)
	}
}
