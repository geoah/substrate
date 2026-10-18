package engine_test

// The observed contracts on the write path: an observed record's measure is
// the engine's, stamped from its unit when it is written; and a metric's and a
// source's records are held to core's `metric` and `source` traits.

import (
	"testing"
)

// The write path stamps an observed record's `measure` from its unit (a
// difference of temperatures is a temperature-difference), drops one a
// writer sends, leaves none on a code, and the observed index converts in the
// stamped one.
func TestObservedMeasureIsStampedFromTheUnit(t *testing.T) {
	t.Parallel()
	r := openObservedIndex(t)
	for _, c := range []struct {
		kind, id string
		props    map[string]any
		want     any
	}{
		{r.observation, "w", map[string]any{
			"metric": r.metricRef("health/body_mass"), "form": "gauge", "value": quantity("72.4", "kg"),
			"at": "2026-10-07T06:41:12Z", "measure": "length",
		}, "mass"},
		{r.observation, "dev", map[string]any{
			"metric": r.metricRef("health/skin_temperature_deviation"), "form": "gauge", "value": quantity("0.3", "Cel"), "delta": true,
			"at": "2026-10-07T06:41:12Z",
		}, "temperature-difference"},
		{r.observation, "door", map[string]any{
			"metric": r.metricRef("air_temperature"), "form": "gauge", "code": "open", "at": "2026-10-07T06:41:12Z",
		}, nil},
		{r.trace, "hr", map[string]any{
			"metric": r.metricRef("health/heart_rate"), "form": "gauge", "unit": "bpm", "measure": "mass",
			"at": "2026-10-07T06:00:00Z", "endsAt": "2026-10-07T07:00:00Z", "every": "PT30M", "values": []any{"58", "61"},
		}, "rate"},
	} {
		rec, err := r.put(t, c.kind, c.id, c.props)
		if err != nil {
			t.Fatalf("put %s: %v", c.id, err)
		}
		if got := rec.Properties["measure"]; got != c.want {
			t.Errorf("%s: measure = %v, want %v", c.id, got, c.want)
		}
	}
	// A patch that names a measure changes nothing; one that changes the unit
	// restamps it.
	rec, err := r.patch(t, r.observation, "w", map[string]any{"measure": "length"})
	if err != nil || rec.Properties["measure"] != "mass" {
		t.Fatalf("a patch naming a measure: %v, measure %v", err, rec.Properties["measure"])
	}
	if got := r.points(t, r.trace, "hr"); len(got) != 2 || got[0].base.Float64 != 58.0/60 {
		t.Errorf("the trace's points convert in its stamped measure: %+v", got)
	}
	r.rebuildMatches(t)
}

// A metric's record is held to the `metric` trait's rules on every write: a
// measure the unit table names, a unit of it, and rollup tiers from the four
// widths, finest first.
func TestMetricWritesAreHeldToTheMetricTrait(t *testing.T) {
	t.Parallel()
	r := openObserved(t)
	if _, err := r.put(t, r.metric, "room/co2", map[string]any{
		"name": "CO2", "measures": "fraction", "unit": "[ppm]", "form": "gauge", "rollups": []any{"5m", "1h"},
	}); err != nil {
		t.Fatalf("a metric with its tiers: %v", err)
	}
	_, err := r.patch(t, r.metric, "room/co2", map[string]any{"rollups": []any{"1h", "5m"}})
	refused(t, "tiers out of order", err, "props.rollups[1]: 5m comes after the coarser 1h")
	_, err = r.patch(t, r.metric, "room/co2", map[string]any{"rollups": []any{"10m"}})
	refused(t, "an unknown tier", err, `props.rollups[0]: "10m" is not a rollup tier: 5m, 15m, 1h or 1d`)
	_, err = r.patch(t, r.metric, "room/co2", map[string]any{"unit": "kg"})
	refused(t, "a unit outside the measure", err, "props.unit: kg is not a unit of fraction")
	_, err = r.patch(t, r.metric, "room/co2", map[string]any{"measures": "airiness"})
	refused(t, "an unknown measure", err, `props.measures: "airiness" is not a measure of the unit table`)
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
