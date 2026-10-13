package engine_test

// The health sample's own kinds on the write path, imported the way a
// repository imports them: the revision a session kind holds, a lab result's
// reference range, and a workout's sport and energy.

import (
	"testing"

	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A kind binding core's `revised` holds its `revision` the way an observed
// kind does (decision 0162): a stale delivery of a night's sleep is refused
// rather than reverting the night to an older upstream version, an equal
// revision is admitted, and a stored one is never removed.
func TestRevisedKindRefusesAFallingRevision(t *testing.T) {
	t.Parallel()
	r := openObserved(t)
	sleep := testdb.Repository(t) + "/health/sleep"
	night := map[string]any{
		"at": "2026-10-09T22:40:00+03:00", "endsAt": "2026-10-10T06:55:00+03:00",
		"timeAsleep": "PT7H20M", "revision": 5,
	}
	if _, err := r.put(t, sleep, "n", night); err != nil {
		t.Fatalf("put: %v", err)
	}
	night["timeAsleep"] = "PT7H25M"
	if _, err := r.put(t, sleep, "n", night); err != nil {
		t.Errorf("an equal revision: %v", err)
	}
	night["revision"] = 3
	night["timeAsleep"] = "PT3H"
	_, err := r.put(t, sleep, "n", night)
	refused(t, "a lower revision", err, "props.revision: 3 is below the stored revision 5")
	if got := r.get(t, sleep, "n").Properties["timeAsleep"]; got != "PT7H25M" {
		t.Errorf("the stale write landed: timeAsleep %v", got)
	}
	_, err = r.patch(t, sleep, "n", map[string]any{"revision": nil})
	refused(t, "removing the revision", err, "props.revision: the stored revision 5 is never removed")
}

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

// A workout's sport is one code set every adapter maps onto, with the
// provider's own word in sportSource, and its energy is active or total,
// never one property meaning either.
func TestWorkoutSportIsOneCodeSetAndEnergyIsSplit(t *testing.T) {
	t.Parallel()
	r := openObserved(t)
	workout := testdb.Repository(t) + "/health/workout"
	session := func(props map[string]any) map[string]any {
		out := map[string]any{"at": "2026-10-09T07:00:00+03:00", "endsAt": "2026-10-09T07:50:00+03:00", "revision": 1}
		for k, v := range props {
			out[k] = v
		}
		return out
	}
	if _, err := r.put(t, workout, "w", session(map[string]any{
		"sport": "strength", "sportSource": "weightlifting",
		"totalEnergy": quantity("1960", "kJ"),
	})); err != nil {
		t.Fatalf("a mapped sport with its source word and a total energy: %v", err)
	}
	if _, err := r.put(t, workout, "a", session(map[string]any{
		"sport": "other", "sportSource": "INDOOR_ROWING_MACHINE", "activeEnergy": quantity("312", "kcal"),
	})); err != nil {
		t.Errorf("an unmapped sport as other with an active energy: %v", err)
	}
	_, err := r.put(t, workout, "x", session(map[string]any{"sport": "weightlifting"}))
	refused(t, "a provider's own word as the sport", err, "props.sport")
	_, err = r.put(t, workout, "y", session(map[string]any{"sport": "running", "energy": quantity("400", "kcal")}))
	refused(t, "energy that says neither active nor total", err, "energy")
}
