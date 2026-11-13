package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// windowEnv is a repository holding seedWindow's timeline, with the two
// kinds it declared.
func windowEnv(t *testing.T) (env *dbEnv, dose, meeting string) {
	t.Helper()
	env = newDBEnv(t)
	dose, meeting, err := seedWindow(env.ds)
	if err != nil {
		t.Fatalf("seed the window: %v", err)
	}
	return env, dose, meeting
}

// windowRecordPath is a record's path under the version prefix.
func windowRecordPath(kind, id string) string { return "/api/v1/" + vocabulary.RecordPath(kind, id) }

func TestWindowMergesRowsAndComputedOccurrences(t *testing.T) {
	t.Parallel()
	env, dose, _ := windowEnv(t)

	rec := env.do(t, http.MethodGet, windowPath("2026-07-01T00:00:00Z", "2026-07-06T00:00:00Z"), env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[windowPage](t, rec)

	var got []string
	for _, r := range page.Records {
		at, _ := r.Properties["at"].(string)
		got = append(got, r.ID+"@"+at+"@"+map[bool]string{true: "computed", false: "stored"}[r.Computed])
	}
	want := []string{
		"levo_20260701T060000Z@2026-07-01T06:00:00Z@computed", // the anchor
		// Order is by slot: the dentist (12:00 on the 2nd) comes after the
		// 06:00 dose of the 2nd.
		"levo_20260702T060000Z@2026-07-02T06:00:00Z@computed",
		"dentist@2026-07-02T12:00:00Z@stored",
		// the 3rd is exdated; the 4th's slot is claimed by the override,
		// which appears at its OWN time instead
		"levo_20260704T060000Z@2026-07-04T18:00:00Z@stored",
		"levo_20260705T060000Z@2026-07-05T06:00:00Z@computed",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("window =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if page.Cursor != "" {
		t.Fatalf("a complete window carried a cursor %q", page.Cursor)
	}
	for _, r := range page.Records {
		if r.ID == "levo" {
			t.Fatal("the series row itself is on the timeline only through its occurrences")
		}
	}

	// The computed envelope: the series' properties with the rule removed,
	// the slot under `at`, endsAt at the anchor's wall-clock duration, the
	// override pair filled, version 0.
	var first windowRow
	for _, r := range page.Records {
		if r.ID == "levo_20260702T060000Z" {
			first = r
		}
	}
	if first.Version != 0 || !first.Computed || first.Kind != dose {
		t.Fatalf("computed occurrence = %+v, want version 0, computed, of the series' kind", first)
	}
	for _, gone := range []string{"recurrence", "exdates", "rdates"} {
		if _, has := first.Properties[gone]; has {
			t.Fatalf("a computed occurrence still carries %s", gone)
		}
	}
	if first.Properties["endsAt"] != "2026-07-02T06:15:00Z" {
		t.Fatalf("endsAt = %v, want the anchor's fifteen minutes", first.Properties["endsAt"])
	}
	if first.Properties["originalAt"] != "2026-07-02T06:00:00Z" {
		t.Fatalf("originalAt = %v", first.Properties["originalAt"])
	}
	ref, _ := first.Properties["recurrenceOf"].(map[string]any)
	if ref["ref"] != dose+"/levo" {
		t.Fatalf("recurrenceOf = %v", first.Properties["recurrenceOf"])
	}
	if first.Properties["dose"] != "50mcg" {
		t.Fatalf("the series' own properties ride the occurrence: %v", first.Properties)
	}
}

func TestWindowPagesAcrossComputedAndStored(t *testing.T) {
	t.Parallel()
	env, _, _ := windowEnv(t)

	var ids []string
	path := windowPath("2026-07-01T00:00:00Z", "2026-07-06T00:00:00Z", "first=2")
	for i := range 5 {
		rec := env.do(t, http.MethodGet, path, env.token, nil)
		wantStatus(t, rec, http.StatusOK)
		page := decodeJSON[windowPage](t, rec)
		if len(page.Records) > 2 {
			t.Fatalf("page %d has %d records, first=2", i, len(page.Records))
		}
		for _, r := range page.Records {
			ids = append(ids, r.ID)
		}
		if page.Cursor == "" {
			break
		}
		path = windowPath("2026-07-01T00:00:00Z", "2026-07-06T00:00:00Z", "first=2", "after="+url.QueryEscape(page.Cursor))
	}
	want := "levo_20260701T060000Z levo_20260702T060000Z dentist levo_20260704T060000Z levo_20260705T060000Z"
	if strings.Join(ids, " ") != want {
		t.Fatalf("paged walk = %q, want %q", strings.Join(ids, " "), want)
	}

	// Descending is the same walk backwards.
	ids = nil
	path = windowPath("2026-07-01T00:00:00Z", "2026-07-06T00:00:00Z", "first=2", "orderBy=at:desc")
	for range 5 {
		rec := env.do(t, http.MethodGet, path, env.token, nil)
		wantStatus(t, rec, http.StatusOK)
		page := decodeJSON[windowPage](t, rec)
		for _, r := range page.Records {
			ids = append(ids, r.ID)
		}
		if page.Cursor == "" {
			break
		}
		path = windowPath("2026-07-01T00:00:00Z", "2026-07-06T00:00:00Z", "first=2", "orderBy=at:desc", "after="+url.QueryEscape(page.Cursor))
	}
	if strings.Join(ids, " ") != "levo_20260705T060000Z levo_20260704T060000Z dentist levo_20260702T060000Z levo_20260701T060000Z" {
		t.Fatalf("descending walk = %q", strings.Join(ids, " "))
	}
}

// A cursor from another filter is refused, not mis-seeked; and a dense rule
// is a named problem, never a silent absence, and the page stands without it.
// The refusals the door makes alone are TestWindowRefusals.
func TestWindowRefusesAForeignCursorAndNamesADenseRule(t *testing.T) {
	t.Parallel()
	env, dose, _ := windowEnv(t)

	first := decodeJSON[windowPage](t, env.do(t, http.MethodGet,
		windowPath("2026-07-01T00:00:00Z", "2026-07-06T00:00:00Z", "first=1"), env.token, nil))
	if first.Cursor == "" {
		t.Fatal("first=1 over five items must page")
	}
	rec := env.do(t, http.MethodGet, windowPath("2026-07-01T00:00:00Z", "2026-07-07T00:00:00Z", "first=1", "after="+url.QueryEscape(first.Cursor)), env.token, nil)
	wantStatus(t, rec, http.StatusUnprocessableEntity)

	env.put(t, dose, "dense", map[string]any{"recurrence": "FREQ=SECONDLY", "at": "2020-01-01T00:00:00Z"})
	page := decodeJSON[windowPage](t, env.do(t, http.MethodGet, windowPath("2026-07-01T00:00:00Z", "2026-07-06T00:00:00Z"), env.token, nil))
	if len(page.Problems) != 1 || page.Problems[0].ID != "dense" {
		t.Fatalf("problems = %+v, want the dense rule named", page.Problems)
	}
	if len(page.Records) != 5 {
		t.Fatalf("the page did not stand beside the problem: %d records", len(page.Records))
	}
}

// Date-only bounds read as UTC midnight, as they do on a plain list, so the
// same day asked either way answers the same page.
func TestWindowDateOnlyBounds(t *testing.T) {
	t.Parallel()
	env, _, _ := windowEnv(t)
	instant := decodeJSON[windowPage](t, env.do(t, http.MethodGet, windowPath("2026-07-01T00:00:00Z", "2026-07-06T00:00:00Z"), env.token, nil))
	rec := env.do(t, http.MethodGet, windowPath("2026-07-01", "2026-07-06"), env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	dated := decodeJSON[windowPage](t, rec)
	if len(dated.Records) == 0 || len(dated.Records) != len(instant.Records) {
		t.Fatalf("date-only bounds answered %d records, instants %d", len(dated.Records), len(instant.Records))
	}
	for i := range dated.Records {
		if dated.Records[i].ID != instant.Records[i].ID {
			t.Fatalf("record %d: %s, want %s", i, dated.Records[i].ID, instant.Records[i].ID)
		}
	}
}

// One bound alone is a plain list: the stored rows of the kinds that bind
// temporal, the series row among them, and nothing computed.
func TestOneSidedAtBoundIsAPlainList(t *testing.T) {
	t.Parallel()
	env, dose, meeting := windowEnv(t)
	filter := map[string]any{"implements": "substrate.reamde.dev/core/temporal", "properties": map[string]any{"at": map[string]any{"gte": "2026-07-01T00:00:00Z"}}}
	raw, _ := json.Marshal(filter)
	rec := env.do(t, http.MethodGet, "/api/v1/records?filter="+url.QueryEscape(string(raw)), env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[windowPage](t, rec)
	var got []string
	for _, r := range page.Records {
		if r.Computed {
			t.Fatalf("a one-sided bound computed an occurrence: %+v", r)
		}
		got = append(got, r.Kind+"/"+r.ID)
	}
	want := map[string]bool{dose + "/levo": true, dose + "/levo_20260704T060000Z": true, meeting + "/dentist": true}
	if len(got) != len(want) {
		t.Fatalf("plain list = %v, want the three stored rows of the temporal kinds", got)
	}
	for _, p := range got {
		if !want[p] {
			t.Fatalf("plain list = %v, want the three stored rows of the temporal kinds", got)
		}
	}
}

// GET at a computed id answers the envelope the window read would, so
// `get -o yaml | apply -f` reaches an override; a claimed slot and a slot the
// rule does not produce are not found.
func TestGetComputedOccurrence(t *testing.T) {
	t.Parallel()
	env, dose, _ := windowEnv(t)

	rec := env.do(t, http.MethodGet, windowRecordPath(dose, "levo_20260702T060000Z"), env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	got := decodeJSON[windowRow](t, rec)
	if !got.Computed || got.Properties["at"] != "2026-07-02T06:00:00Z" || got.Properties["originalAt"] != "2026-07-02T06:00:00Z" {
		t.Fatalf("computed GET = %+v", got)
	}
	// A query parameter is refused at a computed id as at a stored one (#335).
	wantErrorCode(t, env.do(t, http.MethodGet, windowRecordPath(dose, "levo_20260702T060000Z")+"?bogus=1", env.token, nil),
		http.StatusBadRequest, codeBadRequest)
	// The exdated slot, a slot off the rule and a series that does not exist.
	for _, id := range []string{"levo_20260703T060000Z", "levo_20260702T070000Z", "nosuch_20260702T060000Z"} {
		rec := env.do(t, http.MethodGet, windowRecordPath(dose, id), env.token, nil)
		wantStatus(t, rec, http.StatusNotFound)
	}
	// The overridden slot: its override is a stored row under its own id,
	// served by the ordinary path.
	rec = env.do(t, http.MethodGet, windowRecordPath(dose, "levo_20260704T060000Z"), env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := decodeJSON[windowRow](t, rec); got.Computed || got.Properties["at"] != "2026-07-04T18:00:00Z" {
		t.Fatalf("the stored override must win over the computed slot: %+v", got)
	}
}

// The wall-clock duration: an all-day series in a DST zone keeps local
// midnight to local midnight across the change.
func TestComputedEndsAtKeepsTheWallClock(t *testing.T) {
	t.Parallel()
	env, dose, _ := windowEnv(t)
	// Midnight to midnight London, anchored in March before the DST change.
	env.put(t, dose, "allday", map[string]any{
		"name": "All day", "recurrence": "FREQ=WEEKLY", "timezone": "Europe/London",
		"at": "2026-03-23T00:00:00Z", "endsAt": "2026-03-24T00:00:00Z",
	})
	// The week after the clocks go forward (29 March 2026): local midnight is
	// 23:00Z the evening before, and the end is the next local midnight.
	rec := env.do(t, http.MethodGet, windowRecordPath(dose, "allday_20260329T230000Z"), env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	got := decodeJSON[windowRow](t, rec)
	if got.Properties["at"] != "2026-03-29T23:00:00Z" || got.Properties["endsAt"] != "2026-03-30T23:00:00Z" {
		t.Fatalf("all-day occurrence = at %v endsAt %v, want local midnight to local midnight",
			got.Properties["at"], got.Properties["endsAt"])
	}
}

// The edges Codex's review named: a kind without `override` gets a read-only
// envelope (no pair a put would need); an RDATE after the rule's UNTIL still
// occurs; and an occurrence at another time of day keeps the anchor's
// duration rather than its end.
func TestWindowEdgesFromReview(t *testing.T) {
	t.Parallel()
	env, _, _ := windowEnv(t)
	mirror := env.repository + "/mirror/series"
	env.declare(t, fmt.Sprintf(`
kind: substrate.reamde.dev/core/package
metadata:
  id: %[1]s/mirror
data:
  authority: %[1]s
  package: mirror
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: %[1]s/mirror/series
data:
  authority: %[1]s
  package: mirror
  names:
    singular: series
  displayTemplate: "{name}"
  traits:
    - substrate.reamde.dev/core/temporal(range)
    - substrate.reamde.dev/core/recurring
  properties:
    name:
      type: string
    recurrence:
      type: recurrence
    rdates:
      type: datetime
      repeated: true
    exdates:
      type: datetime
      repeated: true
    timezone:
      type: timezone
`, env.repository))
	// Ended in June, but an RDATE lands in the window at 15:00 on a 09:00
	// series that runs an hour.
	env.put(t, mirror, "ended", map[string]any{
		"name": "Ended", "recurrence": "RRULE:FREQ=DAILY;UNTIL=20260601T090000Z",
		"at": "2026-05-01T09:00:00Z", "endsAt": "2026-05-01T10:00:00Z",
		"rdates": []any{"2026-07-02T15:00:00Z"},
	})
	rec := env.do(t, http.MethodGet, windowPath("2026-07-01T00:00:00Z", "2026-07-06T00:00:00Z"), env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[windowPage](t, rec)
	var extra *windowRow
	for i := range page.Records {
		if page.Records[i].ID == "ended_20260702T150000Z" {
			extra = &page.Records[i]
		}
	}
	if extra == nil {
		t.Fatalf("the RDATE after UNTIL was dropped: %v", page.Records)
	}
	if extra.Properties["endsAt"] != "2026-07-02T16:00:00Z" {
		t.Fatalf("endsAt = %v, want the anchor's hour applied to the 15:00 start", extra.Properties["endsAt"])
	}
	for _, gone := range []string{"recurrenceOf", "originalAt"} {
		if _, has := extra.Properties[gone]; has {
			t.Fatalf("a kind without override got %s in its computed envelope", gone)
		}
	}
	// The medication kind binds override, so its envelope still carries the
	// pair.
	for _, r := range page.Records {
		if r.ID == "levo_20260702T060000Z" && r.Properties["recurrenceOf"] == nil {
			t.Fatalf("the override-binding kind lost its pair: %v", r.Properties)
		}
	}
}
