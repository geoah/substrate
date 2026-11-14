package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The window read: a records list whose filter bounds `at` on both ends
// answers with the rows in the window AND the occurrences computed from every
// series among the kinds in play, one page ordered by slot, exdates and
// overridden slots removed, the series row itself absent, a computed
// occurrence served in the record envelope with `computed: true`.

// kindDose and kindMeeting name a medication schedule and a calendar event
// under the samples authority, for a test of what a token's grant over them
// reads; seedWindow declares its own kinds in the repository's own namespace.
const (
	kindDose    = "samples.substrate.reamde.dev/health/medicationschedule"
	kindMeeting = "samples.substrate.reamde.dev/calendar/calendarevent"
)

// seedWindow declares, in the repository's own namespace, `~`, a medication
// kind binding temporal, recurring and override (it holds its own overrides)
// and a meeting kind binding temporal alone, and writes, as the engine's own
// hand so the ids read as written: a daily 09:00 Athens dose from July 1st
// with one slot exdated (the 3rd), the 4th moved to the evening by an
// override of the same kind, and a plain meeting on the 2nd. It answers the
// two kind references.
func seedWindow(ds substrate.Dataset) (dose, meeting string, err error) {
	ctx := context.Background()
	pkg := "~/timeline"
	dose, meeting = pkg+"/medication", pkg+"/meeting"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(pkg, 1),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "medication"}, map[string]any{
			"traits": []any{
				"substrate.reamde.dev/core/temporal(range)", "substrate.reamde.dev/core/recurring", "substrate.reamde.dev/core/override",
			},
			"displayTemplate": "{name}",
			"properties": map[string]any{
				"name":         map[string]any{"type": "string"},
				"dose":         map[string]any{"type": "string"},
				"recurrence":   map[string]any{"type": "recurrence"},
				"rdates":       map[string]any{"type": "datetime", "repeated": true},
				"exdates":      map[string]any{"type": "datetime", "repeated": true},
				"timezone":     map[string]any{"type": "timezone"},
				"recurrenceOf": map[string]any{"type": "reference", "kind": dose},
				"originalAt":   map[string]any{"type": "datetime"},
			},
		}),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "meeting"}, map[string]any{
			"traits":          []any{"substrate.reamde.dev/core/temporal(range)"},
			"displayTemplate": "{name}",
			"properties":      map[string]any{"name": map[string]any{"type": "string"}},
		}),
	}); err != nil {
		return "", "", fmt.Errorf("declare the timeline kinds: %w", err)
	}
	for _, r := range []struct {
		kind, id string
		props    map[string]any
	}{
		{dose, "levo", map[string]any{
			"name":       "Levothyroxine",
			"recurrence": "RRULE:FREQ=DAILY",
			"timezone":   "Europe/Athens",
			"at":         "2026-07-01T06:00:00Z",
			"endsAt":     "2026-07-01T06:15:00Z",
			"exdates":    []any{"2026-07-03T06:00:00Z"},
			"dose":       "50mcg",
		}},
		{dose, "levo_20260704T060000Z", map[string]any{
			"name":         "Levothyroxine",
			"at":           "2026-07-04T18:00:00Z",
			"recurrenceOf": map[string]any{"ref": dose + "/levo"},
			"originalAt":   "2026-07-04T06:00:00Z",
			"dose":         "50mcg",
		}},
		{meeting, "dentist", map[string]any{"name": "Dentist", "at": "2026-07-02T12:00:00Z", "endsAt": "2026-07-02T13:00:00Z"}},
	} {
		if _, err := ds.Put(ctx, substrate.ActorSystem, substrate.PutInput{Kind: r.kind, ID: r.id, Properties: r.props}); err != nil {
			return "", "", fmt.Errorf("put %s: %w", r.id, err)
		}
	}
	return dose, meeting, nil
}

func windowPath(from, to string, extra ...string) string {
	filter := map[string]any{
		"implements": "substrate.reamde.dev/core/temporal",
		"properties": map[string]any{"at": map[string]any{"gte": from, "lt": to}},
	}
	raw, _ := json.Marshal(filter)
	path := "/api/v1/records?filter=" + url.QueryEscape(string(raw))
	for _, e := range extra {
		path += "&" + e
	}
	return path
}

type windowRow struct {
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	Properties map[string]any `json:"properties"`
	Version    int64          `json:"version"`
	Computed   bool           `json:"computed"`
}

type windowPage struct {
	Records  []windowRow                   `json:"records"`
	Cursor   string                        `json:"cursor"`
	Problems []substrate.OccurrenceProblem `json:"problems"`
}

// A window read refuses, at the door, what its merged page cannot answer:
// another order than at, a numbered page, a count and a cursor that does
// not decode. The stub has no Window, so a read that reached the dataset
// would be a 500.
func TestWindowRefusals(t *testing.T) {
	env := newStubEnv(t, nil)

	// Another order than at: a bad request naming the rule.
	rec := env.do(t, http.MethodGet, windowPath("2026-07-01T00:00:00Z", "2026-07-06T00:00:00Z", "orderBy=createdAt"), stubToken, nil)
	wantStatus(t, rec, http.StatusBadRequest)
	if !strings.Contains(rec.Body.String(), "orders by at alone") {
		t.Fatalf("refusal does not name the rule: %s", rec.Body.String())
	}

	// A numbered page has no meaning here: the page merges computed
	// occurrences into the stored rows, so a row count to skip would address
	// the rows alone and drop the occurrences between them.
	rec = env.do(t, http.MethodGet, windowPath("2026-07-01T00:00:00Z", "2026-07-06T00:00:00Z", "offset=2"), stubToken, nil)
	wantStatus(t, rec, http.StatusBadRequest)
	if !strings.Contains(rec.Body.String(), "offset is not supported on a window read") {
		t.Fatalf("refusal does not name offset: %s", rec.Body.String())
	}

	// Nor a count: occurrences are computed per slot and are not rows, so a
	// count of the stored rows would disagree with the page beside it.
	rec = env.do(t, http.MethodGet, windowPath("2026-07-01T00:00:00Z", "2026-07-06T00:00:00Z", "count=1"), stubToken, nil)
	wantStatus(t, rec, http.StatusBadRequest)
	if !strings.Contains(rec.Body.String(), "count is not supported on a window read") {
		t.Fatalf("refusal does not name count: %s", rec.Body.String())
	}

	// A cursor that does not decode is a bad request.
	rec = env.do(t, http.MethodGet, windowPath("2026-07-01T00:00:00Z", "2026-07-06T00:00:00Z", "after=not-a-cursor"), stubToken, nil)
	wantStatus(t, rec, http.StatusBadRequest)
}
