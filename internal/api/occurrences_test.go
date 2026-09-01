package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// The occurrences read (decision 0043): the computed slots of every recurring
// record in a window — rules expanded, logs annotating, materialized spans
// silent, problems named instead of dropped.

const (
	kindMedSchedule = "health.substrate.reamde.dev/medicationschedule"
	kindMedLog      = "health.substrate.reamde.dev/medicationschedulelog"
	kindSeries      = "calendar.substrate.reamde.dev/calendareventseries"
)

func seedRecurring(ds *fakeDataset) {
	ds.traits[traitRecurring] = []string{kindMedSchedule, kindSeries}
	ds.traits[traitOccurrencelog] = []string{kindMedLog}
	ds.records["meds"] = &substrate.Record{
		ID: "meds", Kind: kindMedSchedule, Title: "Levothyroxine daily",
		Properties: map[string]any{
			"recurrence": "RRULE:FREQ=DAILY",
			"timezone":   "Europe/Athens",
			"at":         "2026-07-01T06:00:00Z", // 09:00 Athens
		},
	}
	// The series' first two days are materialized as rows elsewhere, so the
	// read must stay silent there and speak only after the stamp.
	ds.records["standup"] = &substrate.Record{
		ID: "standup", Kind: kindSeries, Title: "Standup",
		Properties: map[string]any{
			"recurrence":        "FREQ=DAILY",
			"startsAt":          "2026-07-01T07:00:00Z",
			"materializedUntil": "2026-07-03T00:00:00Z",
		},
	}
	ds.records["log1"] = &substrate.Record{
		ID: "log1", Kind: kindMedLog,
		// The log names the recurring record it marks with a reference
		// property of its own name (`schedule` here, `routine` or `task`
		// elsewhere), stored as the object holding the full record path
		// under `ref` (0044).
		Properties: map[string]any{
			"scheduledAt": "2026-07-02T06:00:00Z", "status": "done",
			"schedule": map[string]any{"ref": kindMedSchedule + "/meds"},
		},
	}
}

func TestOccurrencesComputeRulesLogsAndStamps(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]
	seedRecurring(ds)
	// A rule the budget refuses is a named problem, never a silent absence.
	ds.records["dense"] = &substrate.Record{
		ID: "dense", Kind: kindMedSchedule,
		Properties: map[string]any{"recurrence": "FREQ=SECONDLY", "at": "2020-01-01T00:00:00Z"},
	}
	// An as-needed schedule (no rule, no rdates) simply has no occurrences.
	ds.records["asneeded"] = &substrate.Record{
		ID: "asneeded", Kind: kindMedSchedule,
		Properties: map[string]any{"doseUnit": "tablet"},
	}

	rec := env.do(t, http.MethodGet,
		"/api/v1/occurrences?from=2026-07-01T00:00:00Z&to=2026-07-04T00:00:00Z", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	out := decodeJSON[substrate.OccurrenceList](t, rec)

	type slot struct{ id, at, logStatus string }
	var got []slot
	for _, o := range out.Occurrences {
		s := slot{id: o.ID, at: o.At.UTC().Format(time.RFC3339)}
		if o.Log != nil {
			s.logStatus = o.Log.Status
		}
		got = append(got, s)
	}
	want := []slot{
		{"meds", "2026-07-01T06:00:00Z", ""},
		{"meds", "2026-07-02T06:00:00Z", "done"},
		{"meds", "2026-07-03T06:00:00Z", ""},
		{"standup", "2026-07-03T07:00:00Z", ""},
	}
	if len(got) != len(want) {
		t.Fatalf("occurrences = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("occurrence %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if out.Truncated {
		t.Fatal("nothing was cut, Truncated must be false")
	}
	if len(out.Problems) != 1 || out.Problems[0].ID != "dense" || out.Problems[0].Message == "" {
		t.Fatalf("problems = %+v, want the dense rule named once", out.Problems)
	}
	if out.Occurrences[0].Title != "Levothyroxine daily" {
		t.Fatalf("title = %q, want the record's title carried through", out.Occurrences[0].Title)
	}
}

func TestOccurrencesTruncateAtTheLimit(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	seedRecurring(env.svc.datasets["geoah"])

	rec := env.do(t, http.MethodGet,
		"/api/v1/occurrences?from=2026-07-01T00:00:00Z&to=2026-07-04T00:00:00Z&limit=2", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	out := decodeJSON[substrate.OccurrenceList](t, rec)
	if len(out.Occurrences) != 2 || !out.Truncated {
		t.Fatalf("limit=2 must answer 2 occurrences with Truncated, got %d (truncated %v)",
			len(out.Occurrences), out.Truncated)
	}
	// Ascending: the cut keeps the window's earliest, never a random subset.
	if out.Occurrences[0].At.After(out.Occurrences[1].At) {
		t.Fatal("occurrences must sort ascending by at")
	}
}

// A repository that never imported the scheduling vocabulary answers empty,
// not 422: no implementors means nothing recurs.
func TestOccurrencesWithoutTheTraitAnswerEmpty(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")

	rec := env.do(t, http.MethodGet,
		"/api/v1/occurrences?from=2026-07-01T00:00:00Z&to=2026-07-04T00:00:00Z", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	out := decodeJSON[substrate.OccurrenceList](t, rec)
	if len(out.Occurrences) != 0 || out.Truncated || len(out.Problems) != 0 {
		t.Fatalf("want an empty answer, got %+v", out)
	}
}

func TestOccurrencesRefuseBadWindows(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	seedRecurring(env.svc.datasets["geoah"])

	for _, path := range []string{
		"/api/v1/occurrences",                                                   // both bounds missing
		"/api/v1/occurrences?from=2026-07-01T00:00:00Z",                         // to missing
		"/api/v1/occurrences?from=yesterday&to=2026-07-04T00:00:00Z",            // not an instant
		"/api/v1/occurrences?from=2026-07-04T00:00:00Z&to=2026-07-01T00:00:00Z", // empty window
		"/api/v1/occurrences?from=2026-07-01T00:00:00Z&to=2026-07-04T00:00:00Z&limit=0",
		"/api/v1/occurrences?from=2026-07-01T00:00:00Z&to=2026-07-04T00:00:00Z&first=5", // wrong key
	} {
		rec := env.do(t, http.MethodGet, path, tok, nil)
		wantErrorCode(t, rec, http.StatusBadRequest, codeBadRequest)
	}

	rec := env.do(t, http.MethodGet,
		"/api/v1/occurrences?from=2026-07-01T00:00:00Z&to=2026-07-04T00:00:00Z", "", nil)
	wantStatus(t, rec, http.StatusUnauthorized)
}
