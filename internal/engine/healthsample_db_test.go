package engine_test

// The health sample's own kinds on the write path, imported the way a
// repository imports them: the revision a session kind holds, a lab result's
// reference range, a workout's sport and energy, and the function that
// writes a lab result in its paired unit system.

import (
	"context"
	"fmt"
	"math/big"
	"os/exec"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
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

// THE HEALTH SAMPLE WRITES A LAB RESULT IN ITS PAIRED UNIT SYSTEM. A US lab's
// LDL in mg/dL and an HbA1c in NGSP % land, through the sample's own trigger,
// as derived results in mmol/L and IFCC mmol/mol: the value at one
// significant digit more than the input's, the range converted with it, and
// derivedFrom and conversion saying how. A derived result is never converted
// again, a summary keeps its statistic and period, a counter is never
// converted, a corrected input rewrites its derivation, an input written again
// after a delete never lowers its derivation's revision, and a deleted one
// takes it with it.
func TestLabResultIsWrittenInItsPairedUnitSystem(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not on PATH; the health sample's function cannot run")
	}
	r := openObserved(t)
	ctx := context.Background()
	drain := func() { t.Helper(); drainTriggers(t, r.ds) }
	derivedOf := func(input string) []*substrate.Record {
		t.Helper()
		page, err := r.ds.List(ctx, substrate.Query{Filter: substrate.Filter{
			Kinds:      []string{r.labresult},
			Properties: map[string]substrate.Cond{"derivedFrom": {Eq: r.labresult + "/" + input}},
		}, First: 10})
		if err != nil {
			t.Fatalf("list the derived results: %v", err)
		}
		return page.Records
	}
	result := func(metric, value, unit string, revision int, extra map[string]any) map[string]any {
		props := map[string]any{
			"metric": r.metricRef(metric), "form": "gauge", "hold": "PT0S",
			"value": quantity(value, unit), "at": "2026-09-14T08:05:00-04:00", "utcOffset": -240,
			"status": "final", "revision": revision,
		}
		for k, v := range extra {
			props[k] = v
		}
		return props
	}
	if _, err := r.put(t, r.labresult, "quest/2026-09-14/ldl", result("health/ldl_cholesterol_mass", "130", "mg/dL", 1,
		map[string]any{"referenceRanges": []any{map[string]any{"high": quantity("100", "mg/dL"), "text": "optimal"}}})); err != nil {
		t.Fatalf("put the LDL: %v", err)
	}
	if _, err := r.put(t, r.labresult, "quest/2026-09-14/a1c", result("health/hba1c", "5.2", "%", 1, nil)); err != nil {
		t.Fatalf("put the HbA1c: %v", err)
	}
	// A summary converts with the statistic and period its form requires; a
	// counter is a running total and never converts.
	if _, err := r.put(t, r.labresult, "home/2026-09-14/ldl-day", result("health/ldl_cholesterol_mass", "130", "mg/dL", 1, map[string]any{
		"form": "summary", "statistic": "avg", "period": "day", "hold": nil, "endsAt": "2026-09-15T00:00:00-04:00",
	})); err != nil {
		t.Fatalf("put the LDL summary: %v", err)
	}
	if _, err := r.put(t, r.labresult, "home/2026-09-14/ldl-total", result("health/ldl_cholesterol_mass", "130", "mg/dL", 1, map[string]any{
		"form": "counter", "hold": nil,
	})); err != nil {
		t.Fatalf("put the LDL counter: %v", err)
	}
	drain()
	day := r.get(t, r.labresult, "home/2026-09-14/ldl-day/as/health/ldl_cholesterol").Properties
	if day["form"] != "summary" || day["statistic"] != "avg" || day["period"] != "day" {
		t.Errorf("derived summary: form %v, statistic %v, period %v; want summary, avg, day", day["form"], day["statistic"], day["period"])
	}
	if n := len(derivedOf("home/2026-09-14/ldl-total")); n != 0 {
		t.Errorf("a counter was converted: %d derived results", n)
	}

	ldl := r.get(t, r.labresult, "quest/2026-09-14/ldl/as/health/ldl_cholesterol").Properties
	if v := ldl["value"].(map[string]any); v["value"] != "3.362" || v["unit"] != "mmol/L" {
		t.Errorf("derived LDL = %v, want 3.362 mmol/L (130 mg/dL over 386.65 g/mol)", v)
	}
	if got := storedPath(ldl["metric"]); got != r.metric+"/health/ldl_cholesterol" {
		t.Errorf("derived LDL metric = %v", got)
	}
	if from := ldl["derivedFrom"].([]any); len(from) != 1 || storedPath(from[0]) != r.labresult+"/quest/2026-09-14/ldl" {
		t.Errorf("derivedFrom = %v", from)
	}
	conv, _ := ldl["conversion"].(map[string]any)
	if conv["formula"] != "molar-mass" || conv["parameters"].(map[string]any)["molarMass"] != "386.65" {
		t.Errorf("conversion = %v, want molar-mass at 386.65", conv)
	}
	rng := ldl["referenceRanges"].([]any)[0].(map[string]any)
	if high := rng["high"].(map[string]any); high["value"] != "2.586" || high["unit"] != "mmol/L" || rng["text"] != "optimal" {
		t.Errorf("derived range = %v, want high 2.586 mmol/L, text kept", rng)
	}
	a1c := r.get(t, r.labresult, "quest/2026-09-14/a1c/as/health/hba1c_ifcc").Properties
	if v := a1c["value"].(map[string]any); v["value"] != "33.3" || v["unit"] != "mmol/mol" {
		t.Errorf("derived HbA1c = %v, want 33.3 mmol/mol ((5.2 - 2.15) x 10.929)", v)
	}
	if n := len(derivedOf("quest/2026-09-14/ldl/as/health/ldl_cholesterol")); n != 0 {
		t.Errorf("a derived result was converted again: %d derivations of it", n)
	}

	if _, err := r.put(t, r.labresult, "quest/2026-09-14/ldl", result("health/ldl_cholesterol_mass", "140", "mg/dL", 2, nil)); err != nil {
		t.Fatalf("correct the LDL: %v", err)
	}
	drain()
	ldl = r.get(t, r.labresult, "quest/2026-09-14/ldl/as/health/ldl_cholesterol").Properties
	if v := ldl["value"].(map[string]any); v["value"] != "3.621" || fmt.Sprint(ldl["revision"]) != "2" {
		t.Errorf("after the correction: value %v at revision %v; want 3.621 at 2", v, ldl["revision"])
	}

	// Deleted and written again before the triggers run, the input starts
	// over at revision 1 while its derivation still holds 2: the derivation
	// keeps 2 and takes the new value.
	if _, err := r.ds.Delete(ctx, substrate.ActorAPI, r.labresult, "quest/2026-09-14/ldl", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the LDL: %v", err)
	}
	if _, err := r.put(t, r.labresult, "quest/2026-09-14/ldl", result("health/ldl_cholesterol_mass", "150", "mg/dL", 1, nil)); err != nil {
		t.Fatalf("write the LDL again: %v", err)
	}
	drain()
	ldl = r.get(t, r.labresult, "quest/2026-09-14/ldl/as/health/ldl_cholesterol").Properties
	if v := ldl["value"].(map[string]any); v["value"] != "3.879" || fmt.Sprint(ldl["revision"]) != "2" {
		t.Errorf("after the input was written again: value %v at revision %v; want 3.879 at 2", v, ldl["revision"])
	}

	if _, err := r.ds.Delete(ctx, substrate.ActorAPI, r.labresult, "quest/2026-09-14/ldl", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the LDL: %v", err)
	}
	drain()
	if n := len(derivedOf("quest/2026-09-14/ldl")); n != 0 {
		t.Errorf("%d derived results outlive their deleted input", n)
	}
	if n := len(derivedOf("quest/2026-09-14/a1c")); n != 1 {
		t.Errorf("the HbA1c has %d derived results, want its one", n)
	}
}

// drainTriggers runs the trigger chain until it is idle, failing on a parked
// delivery.
func drainTriggers(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	ctx := context.Background()
	for range 20 {
		n, err := ds.ProcessTriggers(ctx)
		if err != nil {
			t.Fatalf("process triggers: %v", err)
		}
		if n == 0 {
			statuses, err := ds.TriggerStatuses(ctx)
			if err != nil {
				t.Fatalf("trigger statuses: %v", err)
			}
			for _, st := range statuses {
				if st.Parked > 0 {
					t.Fatalf("%s parked %d: %s", st.ID, st.Parked, st.LastParkedError)
				}
			}
			return
		}
	}
	t.Fatal("the trigger chain did not drain")
}

// convertlab converts the values themselves through the repository's graph,
// so a conversion with an offset moves a result by the offset once: 2.000
// assay is 12 g/L (assay * 1 + 10), 6 mol/L over a molar mass of 2, and 6005
// amtx (mol/L * 1000 + 5). Scaling by what one unit converts to would read
// 2 assay as 22 g/L and write 11055 amtx. The reference range moves the same
// way, each bound on its own.
func TestConvertLabConvertsThroughAffineConversions(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not on PATH; the health sample's function cannot run")
	}
	r := openObserved(t)
	ds := r.ds
	putUnitRecord(t, ds, "assay", map[string]any{"name": "assay unit"})
	putUnitRecord(t, ds, "amtx", map[string]any{"name": "amount unit"})
	putConversionRecord(t, ds, "assay-gl", "assay", "g/L", map[string]any{"factor": "1", "offset": "10"})
	putConversionRecord(t, ds, "moll-amtx", "mol/L", "amtx", map[string]any{"factor": "1000", "offset": "5"})
	metric := func(id, name, unit string) {
		t.Helper()
		if _, err := r.put(t, r.metric, id, map[string]any{
			"name": name, "unit": map[string]any{"ref": vocabulary.KindUnit + "/" + unit},
			"form": "gauge", "hold": "PT0S", "reducers": []any{"last"},
		}); err != nil {
			t.Fatalf("put the metric %s: %v", id, err)
		}
	}
	metric("test/assay_mass", "assay (mass)", "assay")
	metric("test/assay_amount", "assay (amount)", "amtx")
	home := testdb.Repository(t)
	if _, err := r.put(t, home+"/health/analyte", "test/assay", map[string]any{
		"metric":    r.metricRef("test/assay_mass"),
		"molarMass": "2",
		"conversions": []any{map[string]any{
			"target": r.metricRef("test/assay_amount"), "formula": "molar-mass", "version": "1",
		}},
	}); err != nil {
		t.Fatalf("put the analyte: %v", err)
	}
	if _, err := r.put(t, r.labresult, "lab/assay", map[string]any{
		"metric": r.metricRef("test/assay_mass"), "form": "gauge", "hold": "PT0S",
		"value": quantity("2.000", "assay"), "at": "2026-09-14T08:05:00-04:00", "utcOffset": -240,
		"status": "final", "revision": 1,
		"referenceRanges": []any{map[string]any{"low": quantity("1.000", "assay"), "high": quantity("4.000", "assay")}},
	}); err != nil {
		t.Fatalf("put the result: %v", err)
	}
	drainTriggers(t, ds)
	got := r.get(t, r.labresult, "lab/assay/as/test/assay_amount").Properties
	equal := func(what string, q any, want string) {
		t.Helper()
		m, _ := q.(map[string]any)
		v, _ := m["value"].(string)
		have, ok := new(big.Rat).SetString(v)
		if !ok || have.Cmp(mustRat(t, want)) != 0 || m["unit"] != "amtx" {
			t.Errorf("%s = %v, want %s amtx", what, q, want)
		}
	}
	equal("the derived value", got["value"], "6005")
	rng, _ := got["referenceRanges"].([]any)
	if len(rng) != 1 {
		t.Fatalf("derived ranges = %v, want one", got["referenceRanges"])
	}
	bounds := rng[0].(map[string]any)
	equal("the derived low bound", bounds["low"], "5505")
	equal("the derived high bound", bounds["high"], "7005")
}

func mustRat(t *testing.T, s string) *big.Rat {
	t.Helper()
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		t.Fatalf("not a number: %q", s)
	}
	return r
}

// storedPath is a stored reference's record path, in either spelling.
func storedPath(v any) string {
	if m, ok := v.(map[string]any); ok {
		v = m["ref"]
	}
	s, _ := v.(string)
	return s
}
