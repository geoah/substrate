package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// The window read: a records list whose filter bounds `at` on both ends
// answers with the rows in the window AND the occurrences computed from every
// series among the kinds in play, one page ordered by slot, exdates and
// overridden slots removed, the series row itself absent, a computed
// occurrence served in the record envelope with `computed: true`.

const (
	kindDose     = "samples.substrate.reamde.dev/health/medicationschedule"
	kindMeeting  = "samples.substrate.reamde.dev/calendar/calendarevent"
	doseKindPath = "/api/v1/samples.substrate.reamde.dev/health/medicationschedule/"
)

func seedWindow(ds *fakeDataset) {
	ds.types = append(ds.types,
		substrate.KindInfo{Identity: kindDose, Name: "medicationschedule", Authority: "samples.substrate.reamde.dev", Package: "health"},
		substrate.KindInfo{Identity: kindMeeting, Name: "calendarevent", Authority: "samples.substrate.reamde.dev", Package: "calendar"},
	)
	// The dose kind binds all three traits: it holds its own overrides. The
	// meeting kind is temporal alone.
	ds.seedWindowKinds([]string{kindDose, kindMeeting}, []string{kindDose}, []string{kindDose})
	// A daily 09:00 Athens dose from July 1st, one slot exdated (the 3rd).
	ds.records["levo"] = &substrate.Record{
		ID: "levo", Kind: kindDose, Title: "Levothyroxine",
		Properties: map[string]any{
			"title":      "Levothyroxine",
			"recurrence": "RRULE:FREQ=DAILY",
			"timezone":   "Europe/Athens",
			"at":         "2026-07-01T06:00:00Z",
			"endsAt":     "2026-07-01T06:15:00Z",
			"exdates":    []any{"2026-07-03T06:00:00Z"},
			"dose":       "50mcg",
		},
	}
	// The 4th was moved to the evening: an override of the same kind.
	ds.records["levo_20260704T060000Z"] = &substrate.Record{
		ID: "levo_20260704T060000Z", Kind: kindDose, Title: "Levothyroxine",
		Properties: map[string]any{
			"title":        "Levothyroxine",
			"at":           "2026-07-04T18:00:00Z",
			"recurrenceOf": map[string]any{"ref": kindDose + "/levo"},
			"originalAt":   "2026-07-04T06:00:00Z",
			"dose":         "50mcg",
		},
	}
	// A plain meeting on the 2nd.
	ds.records["dentist"] = &substrate.Record{
		ID: "dentist", Kind: kindMeeting, Title: "Dentist",
		Properties: map[string]any{"title": "Dentist", "at": "2026-07-02T12:00:00Z", "endsAt": "2026-07-02T13:00:00Z"},
	}
}

func windowPath(from, to string, extra ...string) string {
	filter := map[string]any{
		"implements": "temporal",
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

func TestWindowMergesRowsAndComputedOccurrences(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	seedWindow(ds)

	rec := env.do(t, http.MethodGet, windowPath("2026-07-01T00:00:00Z", "2026-07-06T00:00:00Z"), tok, nil)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[windowPage](t, rec)

	var got []string
	for _, r := range page.Records {
		at, _ := r.Properties["at"].(string)
		got = append(got, r.ID+"@"+at+"@"+map[bool]string{true: "computed", false: "stored"}[r.Computed])
	}
	want := []string{
		"levo_20260701T060000Z@2026-07-01T06:00:00Z@computed", // the anchor
		"dentist@2026-07-02T12:00:00Z@stored",
		"levo_20260702T060000Z@2026-07-02T06:00:00Z@computed",
		// the 3rd is exdated; the 4th's slot is claimed by the override,
		// which appears at its OWN time instead
		"levo_20260704T060000Z@2026-07-04T18:00:00Z@stored",
		"levo_20260705T060000Z@2026-07-05T06:00:00Z@computed",
	}
	// Order is by slot: the dentist (12:00 on the 2nd) comes after the 06:00
	// dose of the 2nd.
	want[1], want[2] = want[2], want[1]
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
	if first.Version != 0 || !first.Computed {
		t.Fatalf("computed occurrence = %+v, want version 0 and computed", first)
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
	if ref["ref"] != kindDose+"/levo" {
		t.Fatalf("recurrenceOf = %v", first.Properties["recurrenceOf"])
	}
	if first.Properties["dose"] != "50mcg" {
		t.Fatalf("the series' own properties ride the occurrence: %v", first.Properties)
	}
}

func TestWindowPagesAcrossComputedAndStored(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	seedWindow(env.svc.datasets[fakeRepository])

	var ids []string
	path := windowPath("2026-07-01T00:00:00Z", "2026-07-06T00:00:00Z", "first=2")
	for i := range 5 {
		rec := env.do(t, http.MethodGet, path, tok, nil)
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
		rec := env.do(t, http.MethodGet, path, tok, nil)
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

func TestWindowRefusals(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	seedWindow(env.svc.datasets[fakeRepository])

	// Another order than at: a bad request naming the rule.
	rec := env.do(t, http.MethodGet, windowPath("2026-07-01T00:00:00Z", "2026-07-06T00:00:00Z", "orderBy=createdAt"), tok, nil)
	wantStatus(t, rec, http.StatusBadRequest)
	if !strings.Contains(rec.Body.String(), "orders by at alone") {
		t.Fatalf("refusal does not name the rule: %s", rec.Body.String())
	}

	// A cursor from another filter is refused, not mis-seeked.
	first := decodeJSON[windowPage](t, env.do(t, http.MethodGet,
		windowPath("2026-07-01T00:00:00Z", "2026-07-06T00:00:00Z", "first=1"), tok, nil))
	if first.Cursor == "" {
		t.Fatal("first=1 over five items must page")
	}
	rec = env.do(t, http.MethodGet, windowPath("2026-07-01T00:00:00Z", "2026-07-07T00:00:00Z", "first=1", "after="+url.QueryEscape(first.Cursor)), tok, nil)
	wantStatus(t, rec, http.StatusUnprocessableEntity)

	// A dense rule is a named problem, never a silent absence, and the page
	// stands without it.
	ds := env.svc.datasets[fakeRepository]
	ds.records["dense"] = &substrate.Record{
		ID: "dense", Kind: kindDose,
		Properties: map[string]any{"recurrence": "FREQ=SECONDLY", "at": "2020-01-01T00:00:00Z"},
	}
	page := decodeJSON[windowPage](t, env.do(t, http.MethodGet, windowPath("2026-07-01T00:00:00Z", "2026-07-06T00:00:00Z"), tok, nil))
	if len(page.Problems) != 1 || page.Problems[0].ID != "dense" {
		t.Fatalf("problems = %+v, want the dense rule named", page.Problems)
	}
	if len(page.Records) != 5 {
		t.Fatalf("the page did not stand beside the problem: %d records", len(page.Records))
	}
}

// One bound alone is a plain list: rows filtered as today, nothing computed.
func TestOneSidedAtBoundIsAPlainList(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	seedWindow(ds)
	filter := map[string]any{"implements": "temporal", "properties": map[string]any{"at": map[string]any{"gte": "2026-07-01T00:00:00Z"}}}
	raw, _ := json.Marshal(filter)
	rec := env.do(t, http.MethodGet, "/api/v1/records?filter="+url.QueryEscape(string(raw)), tok, nil)
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[windowPage](t, rec)
	for _, r := range page.Records {
		if r.Computed {
			t.Fatalf("a one-sided bound computed an occurrence: %+v", r)
		}
	}
	if q := ds.lastQuery; q.Filter.Implements != "temporal" {
		t.Fatalf("the plain list did not reach List: %+v", q)
	}
}

// GET at a computed id answers the envelope the window read would, so
// `get -o yaml | apply -f` reaches an override; a claimed slot and a slot the
// rule does not produce are not found.
func TestGetComputedOccurrence(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	seedWindow(env.svc.datasets[fakeRepository])

	rec := env.do(t, http.MethodGet, doseKindPath+"levo_20260702T060000Z", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	got := decodeJSON[windowRow](t, rec)
	if !got.Computed || got.Properties["at"] != "2026-07-02T06:00:00Z" || got.Properties["originalAt"] != "2026-07-02T06:00:00Z" {
		t.Fatalf("computed GET = %+v", got)
	}
	// The exdated slot, the overridden slot (its override is a stored row
	// under its own id, served by the ordinary path) and a slot off the rule.
	for _, id := range []string{"levo_20260703T060000Z", "levo_20260702T070000Z", "nosuch_20260702T060000Z"} {
		rec := env.do(t, http.MethodGet, doseKindPath+id, tok, nil)
		wantStatus(t, rec, http.StatusNotFound)
	}
	rec = env.do(t, http.MethodGet, doseKindPath+"levo_20260704T060000Z", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := decodeJSON[windowRow](t, rec); got.Computed || got.Properties["at"] != "2026-07-04T18:00:00Z" {
		t.Fatalf("the stored override must win over the computed slot: %+v", got)
	}
}

// The wall-clock duration: an all-day series in a DST zone keeps local
// midnight to local midnight across the change.
func TestComputedEndsAtKeepsTheWallClock(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	seedWindow(ds)
	// Midnight to midnight London, anchored in March before the DST change.
	ds.records["allday"] = &substrate.Record{
		ID: "allday", Kind: kindDose, Title: "All day",
		Properties: map[string]any{
			"recurrence": "FREQ=WEEKLY", "timezone": "Europe/London",
			"at": "2026-03-23T00:00:00Z", "endsAt": "2026-03-24T00:00:00Z",
		},
	}
	// The week after the clocks go forward (29 March 2026): local midnight is
	// 23:00Z the evening before, and the end is the next local midnight.
	rec := env.do(t, http.MethodGet, doseKindPath+"allday_20260329T230000Z", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	got := decodeJSON[windowRow](t, rec)
	if got.Properties["at"] != "2026-03-29T23:00:00Z" || got.Properties["endsAt"] != "2026-03-30T23:00:00Z" {
		t.Fatalf("all-day occurrence = at %v endsAt %v, want local midnight to local midnight",
			got.Properties["at"], got.Properties["endsAt"])
	}
	_ = time.Second
}

// The edges Codex's review named: a kind without `override` gets a read-only
// envelope (no pair a put would need); an RDATE after the rule's UNTIL still
// occurs; and an occurrence at another time of day keeps the anchor's
// duration rather than its end.
func TestWindowEdgesFromReview(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	seedWindow(ds)
	const kindMirror = "providers.example.com/mirror/series"
	ds.types = append(ds.types, substrate.KindInfo{Identity: kindMirror, Name: "series", Authority: "providers.example.com", Package: "mirror"})
	ds.seedWindowKinds([]string{kindMirror}, []string{kindMirror}, nil)
	// Ended in June, but an RDATE lands in the window at 15:00 on a 09:00
	// series that runs an hour.
	ds.records["ended"] = &substrate.Record{
		ID: "ended", Kind: kindMirror, Title: "Ended",
		Properties: map[string]any{
			"title": "Ended", "recurrence": "RRULE:FREQ=DAILY;UNTIL=20260601T090000Z",
			"at": "2026-05-01T09:00:00Z", "endsAt": "2026-05-01T10:00:00Z",
			"rdates": []any{"2026-07-02T15:00:00Z"},
		},
	}
	rec := env.do(t, http.MethodGet, windowPath("2026-07-01T00:00:00Z", "2026-07-06T00:00:00Z"), tok, nil)
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
	// The dose kind binds override, so its envelope still carries the pair.
	for _, r := range page.Records {
		if r.ID == "levo_20260702T060000Z" && r.Properties["recurrenceOf"] == nil {
			t.Fatalf("the override-binding kind lost its pair: %v", r.Properties)
		}
	}
}
