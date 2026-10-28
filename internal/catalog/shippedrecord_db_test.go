package catalog_test

// A closure's shipped data records other than its record views (decision
// records 0166 and 0191, issue #950): the health catalogue's metric records
// take the same rule through a re-install (record 0192). A metric record any
// other hand edited, trimmed or deleted is left as that hand left it, and one
// only the closure wrote takes the shipped one again.

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

func TestReinstallKeepsAnEditedMetricRecord(t *testing.T) {
	ds := newDataset(t)
	c := loadCatalog(t)
	ctx := context.Background()
	importVocabulary(t, c, ds, healthID)

	metric := func(id string) *substrate.Record {
		t.Helper()
		r, err := ds.Get(ctx, metricKind, id)
		if err != nil {
			t.Fatalf("read the %s metric: %v", id, err)
		}
		return r
	}
	patch := func(actor substrate.Actor, id string, props map[string]any) {
		t.Helper()
		if _, err := ds.Patch(ctx, actor, metricKind, id, substrate.PatchInput{Properties: props}); err != nil {
			t.Fatalf("patch the %s metric as %s: %v", id, actor, err)
		}
	}
	shippedSpO2 := metric("health/spo2").Properties["description"]
	if shippedSpO2 == nil {
		t.Fatal("the shipped health/spo2 metric carries no description to trim")
	}

	patch(substrate.ActorConsole, "health/heart_rate", map[string]any{"name": "Pulse"})
	// A removal is the owner's edit too, though it leaves no value behind.
	patch(substrate.ActorAPI, "health/spo2", map[string]any{"description": nil})
	if _, err := ds.Delete(ctx, substrate.ActorCLI, metricKind, "health/resting_heart_rate", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the resting heart rate metric: %v", err)
	}
	// Every hand but the closure's own holds a record: a door the caller
	// names, an agent's and another bundle's.
	patch(substrate.Actor("my-import-script"), "health/steps", map[string]any{"name": "Steps (script)"})
	patch(substrate.AgentActor(homeAuthority, "llm", "substrate"), "health/distance", map[string]any{"name": "Distance (agent)"})
	patch(substrate.BundleActor("substrate.reamde.dev", "labs"), "health/blood_glucose", map[string]any{"name": "Glucose (labs)"})
	// The closure's own write stands in for an older shipped value: no other
	// hand touched the record, so the re-install refreshes it.
	patch(substrate.BundleActor("substrate.reamde.dev", "health"), "health/hrv_rmssd", map[string]any{"name": "stale"})

	importVocabulary(t, c, ds, healthID)

	if got := metric("health/heart_rate").Properties["name"]; got != "Pulse" {
		t.Errorf("a re-install overwrote the owner's metric: name = %v, want Pulse", got)
	}
	if got, ok := metric("health/spo2").Properties["description"]; ok {
		t.Errorf("a re-install restored the description the owner removed: %v", got)
	}
	if r, err := ds.Get(ctx, metricKind, "health/resting_heart_rate"); err == nil && r.DeletedAt == nil {
		t.Errorf("a re-install recreated the metric the owner deleted: name = %v", r.Properties["name"])
	}
	for id, want := range map[string]string{
		"health/steps":         "Steps (script)",
		"health/distance":      "Distance (agent)",
		"health/blood_glucose": "Glucose (labs)",
	} {
		if got := metric(id).Properties["name"]; got != want {
			t.Errorf("a re-install overwrote the %s metric another hand wrote: name = %v, want %s", id, got, want)
		}
	}
	if got := metric("health/hrv_rmssd").Properties["name"]; got == "stale" {
		t.Error("a re-install left a metric only the closure wrote at its older value")
	}
}
