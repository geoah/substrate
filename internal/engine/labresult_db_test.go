package engine_test

import (
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// A lab result's reference range is two quantities, each held to the unit
// table like the result's own value: a range written in a unit the table
// cannot read is refused, and a stored one keeps its unit beside each bound,
// so a reader can convert the range to the value's unit before it flags it.
func TestLabResultReferenceRangeIsQuantities(t *testing.T) {
	t.Parallel()
	r := openObserved(t)
	ldl := func(rng map[string]any) map[string]any {
		return map[string]any{
			"metric": r.metricRef("health/ldl_cholesterol"), "form": "gauge", "hold": "PT0S",
			"value": quantity("3.4", "mmol/L"), "at": "2026-09-14T08:05:00+01:00",
			"referenceRanges": []any{rng},
		}
	}
	if _, err := r.put(t, r.labresult, "ldl", ldl(map[string]any{
		"low": quantity("0", "mmol/L"), "high": quantity("116", "mg/dL"), "text": "optimal",
	})); err != nil {
		t.Fatalf("a range in two units of one measure: %v", err)
	}
	rng := r.get(t, r.labresult, "ldl").Properties["referenceRanges"].([]any)[0].(map[string]any)
	high, _ := rng["high"].(map[string]any)
	if high[vocabulary.QuantityUnit] != "mg/dL" || high[vocabulary.QuantityValue] != "116" {
		t.Errorf("high = %v, want 116 mg/dL kept with its unit", rng["high"])
	}
	_, err := r.put(t, r.labresult, "ldl", ldl(map[string]any{"high": quantity("3.0", "mgdl")}))
	refused(t, "a range in a unit the table cannot read", err, "referenceRanges")
}
