package vocabulary_test

import (
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/vocabulary"
)

// Under the unit graph switch (decision 0186) the observed rules read units
// through the repository's graph: `delta` marks a difference in any unit,
// `circular` needs a unit whose circle the graph knows, and a trace's unit
// is never read through the engine's table.
func TestObservedRulesReadTheGraph(t *testing.T) {
	g, problems := vocabulary.BuildUnitGraph([]vocabulary.UnitRecord{
		{ID: "deg", Turn: big.NewRat(360, 1)},
		{ID: "kg"},
		{ID: "steps"},
	}, nil)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	at := time.Date(2026, 10, 7, 6, 0, 0, 0, time.UTC)
	single := func(unit string, flags map[string]any) []string {
		props := map[string]any{
			"metric": map[string]any{"ref": "x.test/o/metric/m"}, "form": "gauge",
			"value": map[string]any{"value": "1", "unit": unit},
		}
		for k, v := range flags {
			props[k] = v
		}
		return vocabulary.CheckObserved(vocabulary.ObservedRecord{Variant: vocabulary.ObservedSingle, At: &at, Props: props, Units: g})
	}
	if p := single("kg", map[string]any{"delta": true}); len(p) > 0 {
		t.Errorf("a difference in kg: %v", p)
	}
	if p := single("deg", map[string]any{"circular": true}); len(p) > 0 {
		t.Errorf("a circular value in deg: %v", p)
	}
	if p := single("kg", map[string]any{"circular": true}); len(p) != 1 || !strings.Contains(p[0], "kg declares no turn") {
		t.Errorf("a circular value in kg: %v", p)
	}
	end := at.Add(time.Minute)
	trace := vocabulary.CheckObserved(vocabulary.ObservedRecord{Variant: vocabulary.ObservedPacked, At: &at, EndsAt: &end, Units: g, Props: map[string]any{
		"metric": map[string]any{"ref": "x.test/o/metric/m"}, "form": "amount",
		"unit": "steps", "values": []any{"10"}, "every": "PT1M",
	}})
	if len(trace) > 0 {
		t.Errorf("a trace in a unit the table does not read: %v", trace)
	}
}

// A metric's unit and listed units resolve through the graph, as an id or a
// reference, and each listed unit converts to the unit.
func TestCheckMetricInReadsTheGraph(t *testing.T) {
	g, _ := vocabulary.BuildUnitGraph([]vocabulary.UnitRecord{{ID: "kg"}, {ID: "lb"}, {ID: "K"}},
		[]vocabulary.ConversionRecord{{ID: "lb-kg", From: "lb", To: "kg", Factor: big.NewRat(45359237, 100000000)}})
	ref := func(id string) map[string]any { return map[string]any{"ref": vocabulary.KindUnit + "/" + id} }
	if p := vocabulary.CheckMetricIn(g, map[string]any{"unit": ref("kg"), "units": []any{ref("lb")}}); len(p) > 0 {
		t.Errorf("kg and lb: %v", p)
	}
	if p := vocabulary.CheckMetricIn(g, map[string]any{"unit": "kg", "units": []any{"K"}}); len(p) != 1 || !strings.Contains(p[0], "K does not convert to the metric's unit kg") {
		t.Errorf("kg listing K: %v", p)
	}
	if p := vocabulary.CheckMetricIn(g, map[string]any{"unit": "stone"}); len(p) != 1 || !strings.Contains(p[0], `"stone" names no unit`) {
		t.Errorf("an unknown unit: %v", p)
	}
	if p := vocabulary.CheckMetricIn(g, map[string]any{"unit": ref("stone")}); len(p) != 1 || !strings.Contains(p[0], "stone names no unit") {
		t.Errorf("a reference to no unit: %v", p)
	}
	if p := vocabulary.CheckMetricIn(g, map[string]any{"measures": "nonsense", "unit": "kg"}); len(p) > 0 {
		t.Errorf("the measure is not read under the graph: %v", p)
	}
}
