package commands

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// seedAlerts seeds one open and one resolved alert, the shapes the engine
// writes for a trigger whose deliveries park.
func seedAlerts(h *harness) {
	h.fake.seed(&substrate.Record{
		ID: "trigger.parked/syncgmail", Kind: alertKind, UpdatedAt: testNow,
		Properties: map[string]any{
			"key":         "trigger.parked/syncgmail",
			"level":       "error",
			"state":       "open",
			"count":       float64(3),
			"summary":     "Trigger syncgmail has parked deliveries to function providers.substrate.reamde.dev/google/syncgmail",
			"detail":      "egress blocked\nTraceback (most recent call last)",
			"firstSeenAt": "2026-08-02T10:00:00Z",
			"lastSeenAt":  "2026-08-02T11:55:00Z",
		},
	})
	h.fake.seed(&substrate.Record{
		ID: "trigger.parked/titler", Kind: alertKind, UpdatedAt: testNow,
		Properties: map[string]any{
			"key":         "trigger.parked/titler",
			"level":       "error",
			"state":       "resolved",
			"count":       float64(0),
			"summary":     "Trigger titler has parked deliveries to agent ada.example.com/notes/titler",
			"firstSeenAt": "2026-08-01T12:00:00Z",
			"lastSeenAt":  "2026-08-01T13:00:00Z",
		},
	})
}

// `alerts` lists the open alerts through the records route, the kind and the
// open state inside the filter, one line each: key, level, state, count, when
// first and last seen, and the summary.
func TestAlertsListsTheOpenOnes(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedAlerts(h)

	out, _ := h.mustRun("alerts")
	if got := h.lastRequest(); got != listOf(alertKind) {
		t.Fatalf("last request = %q, want %q", got, listOf(alertKind))
	}
	var f substrate.Filter
	if err := json.Unmarshal([]byte(h.fake.lastQuery.Get("filter")), &f); err != nil {
		t.Fatalf("filter is not JSON: %v", err)
	}
	if len(f.Kinds) != 1 || f.Kinds[0] != alertKind || f.Properties["state"].Eq != "open" {
		t.Fatalf("filter = %+v, want the alert kind and state eq open", f)
	}
	if !strings.Contains(out, "KEY") || !strings.Contains(out, "LAST SEEN") || !strings.Contains(out, "SUMMARY") {
		t.Fatalf("no header: %q", out)
	}
	for _, want := range []string{"trigger.parked/syncgmail", "error", "open", " 3 ", "2h", "5m", "Trigger syncgmail has parked deliveries"} {
		if !strings.Contains(out, want) {
			t.Errorf("alerts output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "titler") {
		t.Errorf("a resolved alert is listed without --all:\n%s", out)
	}
}

// --all drops the state predicate, so the resolved alerts list too.
func TestAlertsAllListsTheResolvedOnesToo(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedAlerts(h)

	out, _ := h.mustRun("alerts", "--all")
	var f substrate.Filter
	if err := json.Unmarshal([]byte(h.fake.lastQuery.Get("filter")), &f); err != nil {
		t.Fatalf("filter is not JSON: %v", err)
	}
	if _, has := f.Properties["state"]; has {
		t.Fatalf("--all still filters on state: %+v", f)
	}
	for _, want := range []string{"trigger.parked/syncgmail", "trigger.parked/titler", "resolved"} {
		if !strings.Contains(out, want) {
			t.Errorf("alerts --all output lacks %q:\n%s", want, out)
		}
	}
}

// -o json is one JSON array of apply-able documents, so a script reads it
// with one decode and the owner can resolve one by editing and applying it.
func TestAlertsJSONIsOneArrayOfDocuments(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedAlerts(h)

	out, _ := h.mustRun("alerts", "-o", "json")
	var docs []struct {
		Kind     string `json:"kind"`
		Metadata struct {
			ID string `json:"id"`
		} `json:"metadata"`
		Data struct {
			Properties map[string]any `json:"properties"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &docs); err != nil {
		t.Fatalf("alerts -o json is not one JSON array: %v\n%s", err, out)
	}
	if len(docs) != 1 || docs[0].Kind != alertKind || docs[0].Metadata.ID != "trigger.parked/syncgmail" ||
		docs[0].Data.Properties["state"] != "open" {
		t.Fatalf("alerts -o json = %+v, want the one open alert as a document", docs)
	}
}

// No open alert prints an empty array under -o json, so a script's decode
// never meets an empty string, and -o yaml prints the same rows as YAML.
func TestAlertsPrintsAListUnderEveryFormat(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()

	out, _ := h.mustRun("alerts", "-o", "json")
	if strings.TrimSpace(out) != "[]" {
		t.Fatalf("alerts -o json with no alert = %q, want []", out)
	}
	seedAlerts(h)
	out, _ = h.mustRun("alerts", "-o", "yaml")
	if !strings.Contains(out, "kind: "+alertKind) || !strings.Contains(out, "id: trigger.parked/syncgmail") ||
		strings.Contains(out, "titler") {
		t.Fatalf("alerts -o yaml lacks the one open alert:\n%s", out)
	}
}
