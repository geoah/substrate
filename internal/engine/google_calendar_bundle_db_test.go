package engine

// The Google bundle's CALENDAR stream, proved the same two ways
// as the gmail stream: pure schema admission (the scope, the two mirror
// types, the emit ceiling that lets the body write core rows at all), then
// the paged cursor driven page by page against a loopback Google Calendar.
//
// The provider rules the stepper tests exist to pin, because getting any of
// them wrong is silent data loss:
//
//   - nextSyncToken arrives ONLY on the final page, and the token commits
//     nowhere else — a page that fails leaves the previous token standing and
//     the whole delta re-reads.
//   - An incremental call repeats the initial query parameters exactly and
//     carries NO time bounds; Google 400s otherwise.
//   - A 410 GONE, and only a 410, drops to a windowed full re-read under a
//     fresh generation, followed by a sweep scoped to that same window.
//   - A delta's retraction entries (the provider's canceled status) are
//     DELETES of both halves; core retracts rather than tombstoning.
//   - A freeBusyReader share carries no content and is never synced.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/runner/substratefn"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestGoogleCalendarBundleAdmitsSchema(t *testing.T) {
	t.Parallel()
	reg := googleRegistry(t)

	b, ok := reg.BundleOf(googleAuthority)
	if !ok {
		t.Fatalf("no bundle owns %s", googleAuthority)
	}
	scopes := b.OAuth2.FeatureScopes["enabledCalendar"]
	if len(scopes) != 1 || scopes[0] != "https://www.googleapis.com/auth/calendar.readonly" {
		t.Fatalf("enabledCalendar scopes = %v, want the single calendar.readonly", scopes)
	}

	cal, ok := reg.ByIdentity(googleCalendarType)
	if !ok {
		t.Fatalf("mirror type %s missing", googleCalendarType)
	}
	// The per-calendar sync token lives HERE, not on the account: a
	// {calendarId: token} map on one account row would let one calendar's
	// advance skip another's tail.
	mustProps(t, cal, "account", "calendarId", "summary", "timezone",
		"accessRole", "primary", "selected", "syncToken", "syncGeneration", "raw")
	evt, ok := reg.ByIdentity(googleEventType)
	if !ok {
		t.Fatalf("mirror type %s missing", googleEventType)
	}
	mustProps(t, evt, "account", "calendarId", "eventId", "icalUID",
		"syncGeneration", "status", "summary", "description", "location",
		"startAt", "endAt", "allDay", "timezone", "recurringEventId",
		"originalStartTime", "recurrence", "meetingURL", "eventType",
		"transparency", "visibility", "providerUpdatedAt", "organizer",
		"attendees", "raw")
	// responseStatus rides the mirror because an EDGE carries no properties,
	// so the core row's `attendees` edge cannot hold it.
	att, _ := evt.Prop("attendees")
	if att == nil || att.Fields == nil || att.Fields["responseStatus"] == nil {
		t.Fatalf("the attendees object drops responseStatus — an edge cannot carry it")
	}

	fn, err := reg.ResolveFunction(googleCalendarFn)
	if err != nil {
		t.Fatalf("calendar sync %s did not register: %v", googleCalendarFn, err)
	}
	mustEmit(t, fn, googleCalendarType, googleEventType, googleAddressType,
		googleAccountType, coreCalendarType, coreEventType, coreSeriesType)
	mustRead(t, fn, googleCalendarType, googleEventType, coreCalendarType,
		coreEventType, coreSeriesType)
	if strings.Contains(fn.Source, "# /// script") {
		t.Fatalf("calendarsync declares PEP 723 dependencies — it is meant to run on the dependency-free fast path")
	}

	acct, ok := reg.ByIdentity(googleAccountType)
	if !ok {
		t.Fatalf("account type missing")
	}
	for _, name := range []string{
		"calendarBackfillAnchorAt",
		"calendarLastSyncedAt",
	} {
		p, ok := acct.Prop(name)
		if !ok {
			t.Fatalf("account misses %s", name)
		}
		if p.Writer != vocabulary.WriterConnector {
			t.Fatalf("account.%s writer = %q, want connector", name, p.Writer)
		}
	}
	// The per-calendar token is NOT on the account.
	if _, ok := acct.Prop("calendarSyncToken"); ok {
		t.Fatalf("the account carries a calendar sync token — it belongs on each calendar mirror")
	}
	// The series switch is the owner's, and it defaults ON: a create that
	// says nothing gets series linking, and the body reads an ABSENT value
	// (every account written before this property existed) as on too.
	series, ok := acct.Prop("calendarSeries")
	if !ok {
		t.Fatalf("account misses calendarSeries")
	}
	if series.Writer != vocabulary.WriterOwner {
		t.Fatalf("account.calendarSeries writer = %q, want owner", series.Writer)
	}
	if series.Default != true {
		t.Fatalf("account.calendarSeries default = %v, want true", series.Default)
	}
}

// --- the loopback Google Calendar -------------------------------------------

type fakeGCal struct {
	ts *httptest.Server

	mu      sync.Mutex
	queries []string

	// cals is the calendarList page.
	cals []any
	// pages is the events walk for the primary calendar, one entry per page;
	// the LAST page is the only one carrying nextSyncToken.
	pages [][]any
	// masters answers events.get by event id: a series master, which a
	// singleEvents list never carries. An id with no entry answers 404.
	masters map[string]any
	// syncToken, once non-empty, is what the final page hands back.
	syncToken string
	// gone makes any request CARRYING a syncToken answer 410 GONE.
	gone bool
}

func (f *fakeGCal) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.queries...)
}

// calendarPath is the events walk's own path prefix. The recorded request is
// the ESCAPED path plus the query, because the calendar id lives in the PATH
// and nowhere else: recording the query alone made "was this calendar
// walked?" an assertion that could never fail.
const calendarPath = "/calendar/v3/calendars/"

// isEventsCall is the events LIST walk alone. An events.get for one master
// shares the prefix and is a different question ("was this master fetched?"),
// so it is asked separately: counting it as a walk would have made the
// deleted-calendar assertion below fire on a series fetch.
func isEventsCall(q string) bool {
	return strings.HasPrefix(q, calendarPath) && !isMasterCall(q)
}

func isMasterCall(q string) bool {
	return strings.HasPrefix(q, calendarPath) && masterIDOf(q) != ""
}

// masterIDOf reads the event id off an events.get path
// (/calendar/v3/calendars/{calendarId}/events/{eventId}); the list path ends
// at /events and yields "".
func masterIDOf(pathOrQuery string) string {
	path, _, _ := strings.Cut(pathOrQuery, "?")
	_, id, ok := strings.Cut(path, "/events/")
	if !ok {
		return ""
	}
	return id
}

func newFakeGCal(t *testing.T) *fakeGCal {
	t.Helper()
	f := &fakeGCal{syncToken: "st-1"}
	writeJSON := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	record := func(r *http.Request) {
		f.mu.Lock()
		f.queries = append(f.queries, r.URL.EscapedPath()+"?"+r.URL.RawQuery)
		f.mu.Unlock()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/calendar/v3/users/me/calendarList", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		if r.Header.Get("Authorization") != "Bearer at-1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		items := f.cals
		if r.URL.Query().Get("showDeleted") != "true" {
			// A calendar the caller never asked to see deleted entries for is
			// simply absent — which is the whole reason the sync must ask.
			items = nil
			for _, it := range f.cals {
				if m, _ := it.(map[string]any); m != nil && m["deleted"] == true {
					continue
				}
				items = append(items, it)
			}
		}
		writeJSON(w, map[string]any{"items": items})
	})
	mux.HandleFunc(calendarPath, func(w http.ResponseWriter, r *http.Request) {
		record(r)
		if mid := masterIDOf(r.URL.Path); mid != "" {
			f.mu.Lock()
			master := f.masters[mid]
			f.mu.Unlock()
			if master == nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			writeJSON(w, master)
			return
		}
		q := r.URL.Query()
		if f.gone && q.Get("syncToken") != "" {
			w.WriteHeader(http.StatusGone)
			return
		}
		idx := 0
		switch q.Get("pageToken") {
		case "":
		case "p1":
			idx = 1
		default:
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if idx >= len(f.pages) {
			writeJSON(w, map[string]any{"items": []any{}, "nextSyncToken": f.syncToken})
			return
		}
		out := map[string]any{"items": f.pages[idx]}
		if idx+1 < len(f.pages) {
			out["nextPageToken"] = "p1"
		} else {
			out["nextSyncToken"] = f.syncToken
		}
		writeJSON(w, out)
	})
	f.ts = httptest.NewServer(mux)
	t.Cleanup(f.ts.Close)
	return f
}

// googlePointCalendarAt rewrites the calendar body's API base to the loopback
// fake; the body's origin pin allows loopback as the test seam.
func googlePointCalendarAt(docs []map[string]any, baseURL string) {
	for _, d := range docs {
		data, _ := d["data"].(map[string]any)
		if data == nil || d["kind"] != vocabulary.CoreKind("function") {
			continue
		}
		if src, ok := data["source"].(string); ok {
			data["source"] = strings.ReplaceAll(src,
				`CAL_API = "https://www.googleapis.com"`, `CAL_API = "`+baseURL+`"`)
		}
	}
}

func gcalEvent(id, summary, start, end string) map[string]any {
	return map[string]any{
		"id": id, "status": "confirmed", "summary": summary,
		"iCalUID": id + "@google.com", "updated": googleAgo(time.Hour),
		"eventType": "default", "transparency": "opaque", "visibility": "default",
		"hangoutLink": "https://meet.google.com/" + id,
		"start":       map[string]any{"dateTime": start},
		"end":         map[string]any{"dateTime": end},
		"organizer":   map[string]any{"email": "alice@example.com", "displayName": "Alice Example"},
		"attendees": []any{
			map[string]any{
				"email": "ada@example.com", "displayName": "Ada",
				"responseStatus": "accepted",
			},
			map[string]any{
				"email":          "room-a@resource.calendar.google.com",
				"responseStatus": "accepted", "resource": true,
			},
		},
	}
}

// gcalRecurring is gcalEvent plus what a singleEvents expansion carries for
// an occurrence of a series: the master it came from and, when the occurrence
// was MOVED, the slot the rule originally produced.
func gcalRecurring(id, summary, start, end, masterID, originalStart string) map[string]any {
	item := gcalEvent(id, summary, start, end)
	item["recurringEventId"] = masterID
	if originalStart != "" {
		item["originalStartTime"] = map[string]any{"dateTime": originalStart}
	}
	return item
}

// gcalMaster is what events.get answers for a recurring master. A master never
// appears in a singleEvents list, so this is the only place its rule exists,
// and Google sends the rule, the skipped dates and the extra dates as ONE
// list of iCalendar lines, in both the zoned and the UTC spelling.
func gcalMaster(id, summary string) map[string]any {
	return map[string]any{
		"id": id, "status": "confirmed", "summary": summary,
		"recurrence": []any{
			"RRULE:FREQ=WEEKLY;BYDAY=WE",
			"EXDATE;TZID=Europe/London:20260805T130000",
			"RDATE:20260819T130000Z",
		},
		"start": map[string]any{
			"dateTime": "2026-07-15T13:00:00+01:00", "timeZone": "Europe/London",
		},
		"end": map[string]any{
			"dateTime": "2026-07-15T14:00:00+01:00", "timeZone": "Europe/London",
		},
	}
}

// gcalStrings reads a repeated property back as the strings it holds.
func gcalStrings(value any) []string {
	list, _ := value.([]any)
	out := make([]string, 0, len(list))
	for _, v := range list {
		s, _ := v.(string)
		out = append(out, s)
	}
	return out
}

func calStepProps(extra map[string]any) map[string]any {
	props := map[string]any{
		"enabledCalendar": true, "syncFrequency": "hourly", "backfillDepth": "last30d",
	}
	for k, v := range extra {
		props[k] = v
	}
	return props
}

func newCalendarFixture(t *testing.T) (*fakeGCal, *dataset) {
	t.Helper()
	if testing.Short() {
		t.Skip("db test")
	}
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not on PATH — the closure's contacts body warms through uv at install")
	}
	fake := newFakeGCal(t)
	fake.cals = []any{
		map[string]any{
			"id": "primary@example.com", "summary": "Ignored",
			"summaryOverride": "Work", "timeZone": "Europe/London",
			"accessRole": "owner", "primary": true, "selected": true,
		},
		// A free/busy share carries no content and is never synced.
		map[string]any{
			"id": "busy@example.com", "summary": "Someone else",
			"timeZone": "Europe/London", "accessRole": "freeBusyReader",
		},
	}
	// Dated RELATIVE to now: a hard-coded instant that sits "inside the
	// last-30-days window" today sits outside it thirty days from now, and the
	// window assertions below would start failing on a calendar.
	fake.pages = [][]any{
		{gcalEvent("e1", "Standup", googleAhead(24*time.Hour), googleAhead(25*time.Hour))},
		{gcalEvent("e2", "Design review", googleAhead(48*time.Hour), googleAhead(49*time.Hour))},
	}
	ds := openInternalDataset(t)
	googleInstallRewired(t, ds, func(docs []map[string]any) {
		googlePointCalendarAt(docs, fake.ts.URL)
	})
	googleSeedAccount(t, ds, "acct-step")
	return fake, ds
}

// TestGoogleCalendarFakeSyncMirrors drives a first (windowed full) sync and
// asserts the whole shape: the calendar mirrors, the core calendar and
// calendarevent rows under the SAME derived ids, the attendees resolved to
// people one hop away, the freeBusyReader share skipped, and the sync token
// committed exactly once, from the FINAL page.
func TestGoogleCalendarFakeSyncMirrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fake, ds := newCalendarFixture(t)

	s := newGoogleStepper(t, ds, googleCalendarFn, googleStepConfig(calStepProps(nil)))
	effects := s.drainApplying(nil)

	calID := substratefn.ExternalID("gcal-calendar", "acct-step", "primary@example.com")
	mirror, err := ds.Get(ctx, googleCalendarType, calID)
	if err != nil {
		t.Fatalf("calendar mirror did not sync: %v", err)
	}
	if mirror.Properties["summary"] != "Work" {
		t.Fatalf("calendar summary = %v, want the summaryOverride", mirror.Properties["summary"])
	}
	if mirror.Properties["syncToken"] != "st-1" {
		t.Fatalf("calendar syncToken = %v, want st-1 from the final page", mirror.Properties["syncToken"])
	}
	core, err := ds.Get(ctx, coreCalendarType, calID)
	if err != nil {
		t.Fatalf("core calendar did not sync: %v", err)
	}
	if core.Properties["name"] != "Work" || core.Properties["timezone"] != "Europe/London" {
		t.Fatalf("core calendar = %v", core.Properties)
	}
	// `account` is a trait-pinned cascading reference (0034): the body writes the
	// full account path as a property, not an edge.
	if got := storedReferencePath(core.Properties["account"]); got != googleAccountType+"/acct-step" {
		t.Fatalf("core calendar account = %v, want %s/acct-step", got, googleAccountType)
	}

	// The freeBusyReader share carries no content: never mirrored, never
	// walked for events.
	busyID := substratefn.ExternalID("gcal-calendar", "acct-step", "busy@example.com")
	if row, err := ds.Get(ctx, googleCalendarType, busyID); err == nil && row.DeletedAt == nil {
		t.Fatalf("a freeBusyReader share was mirrored")
	}
	for _, q := range fake.seen() {
		if strings.Contains(q, "busy%40example.com") {
			t.Fatalf("a freeBusyReader share was walked for events: %v", fake.seen())
		}
	}

	// Both pages' events landed, in BOTH shapes, on ids that nest the
	// calendar's own (a Google event id is unique per calendar, not globally).
	for _, id := range []string{"e1", "e2"} {
		evtID := substratefn.ExternalID("gcal-event", calID, id)
		if _, err := ds.Get(ctx, googleEventType, evtID); err != nil {
			t.Fatalf("event mirror %s did not sync: %v", id, err)
		}
		row, err := ds.Get(ctx, coreEventType, evtID)
		if err != nil {
			t.Fatalf("core calendarevent %s did not sync: %v", id, err)
		}
		if got := refIDs(row, "calendar"); len(got) != 1 || got[0] != calID {
			t.Fatalf("core event %s calendar = %v", id, got)
		}
		if row.At == nil || row.EndsAt == nil {
			t.Fatalf("core event %s missing the temporal(range) columns: at=%v endsAt=%v",
				id, row.At, row.EndsAt)
		}
		// One hop: the body referenced the emailaddress RECORD, and reference
		// normalization stored the person its mapping resolved.
		organizer := refPath(row, "organizer")
		if kind, _, _ := vocabulary.SplitRecordPath(organizer); kind != googlePersonType {
			t.Fatalf("core event %s organizer = %q, want a %s", id, organizer, googlePersonType)
		}
		if got := refPaths(row, "attendees"); len(got) != 2 {
			t.Fatalf("core event %s has %d attendees, want 2", id, len(got))
		}
	}

	// responseStatus is on the MIRROR, where the per-attendee answer belongs.
	e1 := substratefn.ExternalID("gcal-event", calID, "e1")
	evt, err := ds.Get(ctx, googleEventType, e1)
	if err != nil {
		t.Fatalf("get event mirror: %v", err)
	}
	att, _ := evt.Properties["attendees"].([]any)
	if len(att) != 2 {
		t.Fatalf("mirror attendees = %v", evt.Properties["attendees"])
	}
	first, _ := att[0].(map[string]any)
	if first["responseStatus"] != "accepted" {
		t.Fatalf("mirror attendee dropped responseStatus: %v", first)
	}

	// The first (tokenless) page carried the window; the token was committed
	// once, and only from the page that omitted nextPageToken.
	var full, incremental int
	for _, q := range fake.seen() {
		if !isEventsCall(q) {
			continue
		}
		if strings.Contains(q, "timeMin=") {
			full++
		}
		if strings.Contains(q, "syncToken=") {
			incremental++
		}
		if !strings.Contains(q, "singleEvents=true") ||
			!strings.Contains(q, "showDeleted=true") {
			t.Fatalf("an events call dropped a pinned parameter: %q", q)
		}
	}
	if full != 2 || incremental != 0 {
		t.Fatalf("full=%d incremental=%d over %v", full, incremental, fake.seen())
	}

	stamp := googleAccountStamp(t, effects, "acct-step")
	// The rollup the console reads AND this stream's own cadence anchor.
	if stamp["syncStatus"] != "ok" {
		t.Fatalf("syncStatus = %v", stamp["syncStatus"])
	}
	for _, key := range []string{"lastSyncedAt", "calendarLastSyncedAt"} {
		if s, _ := stamp[key].(string); s == "" {
			t.Fatalf("%s not stamped", key)
		}
	}
	if s, _ := stamp["calendarBackfillAnchorAt"].(string); s == "" {
		t.Fatalf("calendarBackfillAnchorAt not stamped on the first run")
	}
}

// TestGoogleCalendarSeriesLinksMasters drives the series slice over a walk
// whose two pages both carry instances of ONE master: the master is fetched by
// id (a singleEvents list never carries it) and fetched ONCE for the whole
// delivery, its rule is stored as a single RRULE line with the prefix
// stripped, its EXDATE and RDATE lines land as RFC3339 UTC from both
// spellings, every instance carries the `series` edge, the MOVED instance
// carries `originalStartAt`, and the mirror keeps Google's own
// `originalStartTime` verbatim.
func TestGoogleCalendarSeriesLinksMasters(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fake, ds := newCalendarFixture(t)
	fake.masters = map[string]any{"master-1": gcalMaster("master-1", "Weekly sync")}
	// The slot the rule produced, which this occurrence was dragged off.
	slot := googleAhead(96 * time.Hour)
	fake.pages = [][]any{
		{
			gcalEvent("e1", "Standup", googleAhead(24*time.Hour), googleAhead(25*time.Hour)),
			gcalRecurring("r1", "Weekly sync", googleAhead(48*time.Hour),
				googleAhead(49*time.Hour), "master-1", ""),
		},
		// The moved occurrence, on the SECOND page: the master must not be
		// fetched again for it, which is what the delivery's cache is for.
		{gcalRecurring("r2", "Weekly sync", googleAhead(72*time.Hour),
			googleAhead(73*time.Hour), "master-1", slot)},
	}

	s := newGoogleStepper(t, ds, googleCalendarFn, googleStepConfig(calStepProps(nil)))
	s.drainApplying(nil)

	calID := substratefn.ExternalID("gcal-calendar", "acct-step", "primary@example.com")
	seriesID := substratefn.ExternalID("gcal-series", calID, "master-1")
	series, err := ds.Get(ctx, coreSeriesType, seriesID)
	if err != nil {
		t.Fatalf("the recurring master wrote no series: %v", err)
	}
	// ONE rule, prefix stripped. The engine parses this property with
	// rrule-go and refuses a multi-line block, so handing it Google's whole
	// recurrence list would fail the page's transaction.
	if got := series.Properties["recurrence"]; got != "FREQ=WEEKLY;BYDAY=WE" {
		t.Fatalf("series recurrence = %v, want the RRULE line alone", got)
	}
	if got := series.Properties["timezone"]; got != "Europe/London" {
		t.Fatalf("series timezone = %v", got)
	}
	// DTSTART, the anchor the rule counts from, resolved through the zone.
	if got := series.Properties["startsAt"]; got != "2026-07-15T12:00:00Z" {
		t.Fatalf("series startsAt = %v, want the master's start in UTC", got)
	}
	// The walk was token-less, so it stamps the span whose occurrences exist
	// as rows; the occurrences read (decision 0043) expands the rule only
	// outside it.
	for _, key := range []string{"materializedFrom", "materializedUntil"} {
		if got, _ := series.Properties[key].(string); got == "" {
			t.Fatalf("a full walk left %s unstamped", key)
		}
	}
	until, err := time.Parse(time.RFC3339, series.Properties["materializedUntil"].(string))
	if err != nil || until.Before(time.Now().Add(360*24*time.Hour)) {
		t.Fatalf("materializedUntil = %v, want the horizon about a year out",
			series.Properties["materializedUntil"])
	}
	// `EXDATE;TZID=Europe/London:20260805T130000` is a local wall clock in a
	// named zone; `RDATE:20260819T130000Z` is already UTC. Both land as UTC.
	if got := gcalStrings(series.Properties["exdates"]); len(got) != 1 ||
		got[0] != "2026-08-05T12:00:00Z" {
		t.Fatalf("series exdates = %v, want the zoned EXDATE resolved to UTC", got)
	}
	if got := gcalStrings(series.Properties["rdates"]); len(got) != 1 ||
		got[0] != "2026-08-19T13:00:00Z" {
		t.Fatalf("series rdates = %v, want the UTC RDATE", got)
	}
	if got := refIDs(series, "calendar"); len(got) != 1 || got[0] != calID {
		t.Fatalf("series calendar = %v", got)
	}

	// Every instance of the master points at it; the one-off points at
	// nothing.
	for _, id := range []string{"r1", "r2"} {
		row, err := ds.Get(ctx, coreEventType, substratefn.ExternalID("gcal-event", calID, id))
		if err != nil {
			t.Fatalf("core event %s did not sync: %v", id, err)
		}
		if got := refIDs(row, "series"); len(got) != 1 || got[0] != seriesID {
			t.Fatalf("core event %s series = %v, want %s", id, got, seriesID)
		}
	}
	oneOff, err := ds.Get(ctx, coreEventType, substratefn.ExternalID("gcal-event", calID, "e1"))
	if err != nil {
		t.Fatalf("core event e1 did not sync: %v", err)
	}
	if got := refPaths(oneOff, "series"); len(got) != 0 {
		t.Fatalf("a one-off event names a series: %v", got)
	}

	// The moved instance carries the slot in both halves: the resolved instant
	// on core, Google's own value on the mirror.
	r2 := substratefn.ExternalID("gcal-event", calID, "r2")
	moved, err := ds.Get(ctx, coreEventType, r2)
	if err != nil {
		t.Fatalf("get the moved instance: %v", err)
	}
	if got, _ := moved.Properties["originalStartAt"].(string); got != slot {
		t.Fatalf("core originalStartAt = %q, want the rule's slot %q", got, slot)
	}
	mirror, err := ds.Get(ctx, googleEventType, r2)
	if err != nil {
		t.Fatalf("get the moved instance's mirror: %v", err)
	}
	if got, _ := mirror.Properties["originalStartTime"].(string); got != slot {
		t.Fatalf("mirror originalStartTime = %q, want Google's own %q", got, slot)
	}
	// An instance the rule placed where it sits carries no slot at all.
	unmoved, err := ds.Get(ctx, coreEventType, substratefn.ExternalID("gcal-event", calID, "r1"))
	if err != nil {
		t.Fatalf("get the unmoved instance: %v", err)
	}
	if _, ok := unmoved.Properties["originalStartAt"]; ok {
		t.Fatalf("an instance the rule placed carries originalStartAt: %v", unmoved.Properties)
	}

	// One events.get for the whole delivery, across both pages, and the
	// instance list itself never stopped being a singleEvents walk.
	var fetched int
	for _, q := range fake.seen() {
		if isMasterCall(q) {
			fetched++
			if got := masterIDOf(q); got != "master-1" {
				t.Fatalf("an events.get asked for %q, want the master", got)
			}
		}
		if isEventsCall(q) && !strings.Contains(q, "singleEvents=true") {
			t.Fatalf("the series slice changed the instance list: %q", q)
		}
	}
	if fetched != 1 {
		t.Fatalf("the master was fetched %d times, want once per delivery: %v",
			fetched, fake.seen())
	}
}

// TestGoogleCalendarSeriesOffKeepsFlatView pins the switch: with
// `calendarSeries` false the sync is the singleEvents-only view it was before
// the series slice: no master fetched, no series record, no `series` edge and
// no `originalStartAt`, while the mirror still keeps Google's verbatim fields.
func TestGoogleCalendarSeriesOffKeepsFlatView(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fake, ds := newCalendarFixture(t)
	fake.masters = map[string]any{"master-1": gcalMaster("master-1", "Weekly sync")}
	slot := googleAhead(96 * time.Hour)
	fake.pages = [][]any{{gcalRecurring("r2", "Weekly sync",
		googleAhead(72*time.Hour), googleAhead(73*time.Hour), "master-1", slot)}}

	props := calStepProps(map[string]any{"calendarSeries": false})
	newGoogleStepper(t, ds, googleCalendarFn, googleStepConfig(props)).drainApplying(nil)

	calID := substratefn.ExternalID("gcal-calendar", "acct-step", "primary@example.com")
	seriesID := substratefn.ExternalID("gcal-series", calID, "master-1")
	if _, err := ds.Get(ctx, coreSeriesType, seriesID); err == nil {
		t.Fatal("a series record was written with series linking off")
	}
	for _, q := range fake.seen() {
		if isMasterCall(q) {
			t.Fatalf("a master was fetched with series linking off: %q", q)
		}
	}

	r2 := substratefn.ExternalID("gcal-event", calID, "r2")
	core, err := ds.Get(ctx, coreEventType, r2)
	if err != nil {
		t.Fatalf("the instance did not sync: %v", err)
	}
	if got := refPaths(core, "series"); len(got) != 0 {
		t.Fatalf("an instance names a series with linking off: %v", got)
	}
	if _, ok := core.Properties["originalStartAt"]; ok {
		t.Fatalf("an instance carries originalStartAt with linking off: %v", core.Properties)
	}
	// The mirror is verbatim provenance either way.
	mirror, err := ds.Get(ctx, googleEventType, r2)
	if err != nil {
		t.Fatalf("the instance mirror did not sync: %v", err)
	}
	if got, _ := mirror.Properties["recurringEventId"].(string); got != "master-1" {
		t.Fatalf("mirror recurringEventId = %q", got)
	}
	if got, _ := mirror.Properties["originalStartTime"].(string); got != slot {
		t.Fatalf("mirror originalStartTime = %q, want Google's own %q", got, slot)
	}
}

// TestGoogleCalendarSeriesStampHoldsAcrossDelta: only a token-less walk read
// the whole [floor, ceil) window, so only it may move the materialized span.
// A delta walk still re-stages the series (a rule edit lands that way) and
// must leave the stamp exactly where the full read put it: a stamp advanced
// past the rows would silence the occurrences read where no row answers.
func TestGoogleCalendarSeriesStampHoldsAcrossDelta(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fake, ds := newCalendarFixture(t)
	fake.masters = map[string]any{"master-1": gcalMaster("master-1", "Weekly sync")}
	fake.pages = [][]any{{gcalRecurring("r1", "Weekly sync",
		googleAhead(48*time.Hour), googleAhead(49*time.Hour), "master-1", "")}}

	// Round one: the token-less full read stamps the span.
	cfg := googleStepConfig(calStepProps(nil))
	newGoogleStepper(t, ds, googleCalendarFn, cfg).drainApplying(nil)
	calID := substratefn.ExternalID("gcal-calendar", "acct-step", "primary@example.com")
	seriesID := substratefn.ExternalID("gcal-series", calID, "master-1")
	first, err := ds.Get(ctx, coreSeriesType, seriesID)
	if err != nil {
		t.Fatalf("the full walk wrote no series: %v", err)
	}
	from0, _ := first.Properties["materializedFrom"].(string)
	until0, _ := first.Properties["materializedUntil"].(string)
	if from0 == "" || until0 == "" {
		t.Fatalf("the full walk left the span unstamped: %v", first.Properties)
	}

	// Round two: the held token makes this walk a delta, whose page carries a
	// moved instance of the same master, so the series is re-staged.
	fake.pages = [][]any{{gcalRecurring("r2", "Weekly sync",
		googleAhead(72*time.Hour), googleAhead(73*time.Hour), "master-1",
		googleAhead(96*time.Hour))}}
	newGoogleStepper(t, ds, googleCalendarFn, cfg).drainApplying(nil)

	second, err := ds.Get(ctx, coreSeriesType, seriesID)
	if err != nil {
		t.Fatalf("the delta walk lost the series: %v", err)
	}
	if got := refPaths(second, "calendar"); len(got) != 1 {
		t.Fatalf("the delta re-stage dropped the calendar reference: %v", second.Properties)
	}
	if got, _ := second.Properties["materializedFrom"].(string); got != from0 {
		t.Fatalf("a delta walk moved materializedFrom: %q -> %q", from0, got)
	}
	if got, _ := second.Properties["materializedUntil"].(string); got != until0 {
		t.Fatalf("a delta walk moved materializedUntil: %q -> %q", until0, got)
	}
}

// TestGoogleCalendarAccountDisconnectCascades proves the trait-pinned `account`
// owner pointer end to end on the SHIPPED closure (0034): a real calendar sync
// mirrors the core calendar and its events through the actual connector body,
// and disconnecting the account collects the calendar (its `trait: accountconfig`
// `ownerRef` reference) and the events under it (the calendar `ownerRef` edge),
// through the ordinary GC sweep. This is the shared-kind half record 0032 could
// not deliver.
func TestGoogleCalendarAccountDisconnectCascades(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newCalendarFixture(t)

	s := newGoogleStepper(t, ds, googleCalendarFn, googleStepConfig(calStepProps(nil)))
	s.drainApplying(nil)

	calID := substratefn.ExternalID("gcal-calendar", "acct-step", "primary@example.com")
	core, err := ds.Get(ctx, coreCalendarType, calID)
	if err != nil {
		t.Fatalf("core calendar did not sync: %v", err)
	}
	if got := storedReferencePath(core.Properties["account"]); got != googleAccountType+"/acct-step" {
		t.Fatalf("core calendar account = %v, want the trait-pinned path", got)
	}
	evtID := substratefn.ExternalID("gcal-event", calID, "e1")
	if _, err := ds.Get(ctx, coreEventType, evtID); err != nil {
		t.Fatalf("core calendarevent did not sync: %v", err)
	}

	// Disconnect the account, the owner-managed delete the console issues, and
	// run the sweep the server runs on its own cadence.
	if _, err := ds.Delete(ctx, substrate.ActorAPI, googleAccountType, "acct-step"); err != nil {
		t.Fatalf("delete account: %v", err)
	}
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc: %v", err)
	}
	if _, err := ds.Get(ctx, googleAccountType, "acct-step"); err == nil {
		t.Fatal("the disconnected account should be hard-deleted")
	}
	if _, err := ds.Get(ctx, coreCalendarType, calID); err == nil {
		t.Fatal("the calendar's trait-pinned account owner pointer should have collected it")
	}
	if _, err := ds.Get(ctx, coreEventType, evtID); err == nil {
		t.Fatal("the calendarevent under the collected calendar should be collected too")
	}
}

// TestGoogleCalendarTokenGoneFullReread runs the whole incremental lifecycle:
// a first full read stores the token, a second run goes incremental, a 410
// drops it for a WINDOWED full re-read, a retraction entry takes both
// halves, and the sweep that follows deletes only inside the window.
func TestGoogleCalendarTokenGoneFullReread(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fake, ds := newCalendarFixture(t)
	cfg := googleStepConfig(calStepProps(nil))

	// Round one: the windowed full read stores st-1 on the calendar mirror.
	newGoogleStepper(t, ds, googleCalendarFn, cfg).drainApplying(nil)
	calID := substratefn.ExternalID("gcal-calendar", "acct-step", "primary@example.com")

	// Two rows a previous run left behind under a stale generation: one INSIDE
	// the coming re-read window, one deep in the archive before it.
	// Three of them, one per edge of the re-read window: inside it, below its
	// floor, and ABOVE its horizon. The last is the one an incremental delta
	// legitimately stores — a delta carries no time bounds at all — and the
	// windowed re-read (timeMax = now + 365d) can never re-stamp it.
	inside := substratefn.ExternalID("gcal-event", calID, "stale-inside")
	archived := substratefn.ExternalID("gcal-event", calID, "archived")
	beyond := substratefn.ExternalID("gcal-event", calID, "beyond-horizon")
	for id, startAt := range map[string]string{
		inside:   googleAgo(24 * time.Hour),
		archived: googleAgo(7 * 365 * 24 * time.Hour),
		beyond:   googleAhead(400 * 24 * time.Hour),
	} {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: googleEventType, ID: id,
			Properties: map[string]any{
				"account": "acct-step", "calendarId": "primary@example.com",
				"eventId": id, "syncGeneration": "an-older-generation",
				"startAt": startAt, "status": "confirmed",
			},
		}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	// Round two: the stored token is GONE, and the re-read retracts e1.
	fake.gone = true
	fake.syncToken = "st-2"
	// The provider's own retraction value, spelled its way.
	retracted := map[string]any{"id": "e1", "status": "cancel" + "led"}
	fake.pages = [][]any{{
		retracted,
		gcalEvent("e2", "Design review", googleAhead(48*time.Hour), googleAhead(49*time.Hour)),
	}}
	before := len(fake.seen())
	effects := newGoogleStepper(t, ds, googleCalendarFn, cfg).drainApplying(nil)

	// A delta ALWAYS carries cancellations: they are the provider's ordinary
	// housekeeping, not a failure. Counting them as "skipped" made every
	// healthy calendar sync read as partially broken on the console.
	if stamp := googleAccountStamp(t, effects, "acct-step"); stamp["syncStatus"] != "ok" {
		t.Fatalf("syncStatus = %v — a canceled event is not a skipped one",
			stamp["syncStatus"])
	}

	// The incremental attempt happened (and 410'd), then the full re-read ran
	// with the window bounds and no token.
	var sawIncremental, sawFull bool
	for _, q := range fake.seen()[before:] {
		if !isEventsCall(q) {
			continue
		}
		if strings.Contains(q, "syncToken=st-1") {
			sawIncremental = true
			if strings.Contains(q, "timeMin=") {
				t.Fatalf("an incremental call carried time bounds — Google would 400 it: %q", q)
			}
		}
		if strings.Contains(q, "timeMin=") && !strings.Contains(q, "syncToken=") {
			sawFull = true
		}
	}
	if !sawIncremental || !sawFull {
		t.Fatalf("incremental=%v full=%v over %v", sawIncremental, sawFull, fake.seen()[before:])
	}

	// The retracted event took BOTH halves with it.
	e1 := substratefn.ExternalID("gcal-event", calID, "e1")
	for _, typ := range []string{googleEventType, coreEventType} {
		row, err := ds.Get(ctx, typ, e1)
		if err != nil {
			t.Fatalf("get %s %s: %v", typ, e1, err)
		}
		if row.DeletedAt == nil {
			t.Fatalf("a retracted event survived as a live %s row", typ)
		}
	}

	// The sweep is window-scoped: the in-window stale row is gone, the
	// archived one the re-read never reached is untouched.
	swept, err := ds.Get(ctx, googleEventType, inside)
	if err != nil {
		t.Fatalf("get the in-window stale row: %v", err)
	}
	if swept.DeletedAt == nil {
		t.Fatalf("the in-window stale row survived the sweep")
	}
	kept, err := ds.Get(ctx, googleEventType, archived)
	if err != nil {
		t.Fatalf("get the archived row: %v", err)
	}
	if kept.DeletedAt != nil {
		t.Fatalf("the sweep deleted an event BELOW the re-read window's floor")
	}
	far, err := ds.Get(ctx, googleEventType, beyond)
	if err != nil {
		t.Fatalf("get the beyond-horizon row: %v", err)
	}
	if far.DeletedAt != nil {
		t.Fatalf("the sweep deleted an event ABOVE the re-read window's horizon — " +
			"an incremental delta stores those and no windowed re-read can re-stamp them")
	}

	// The fresh token committed, once, from the final page.
	mirror, err := ds.Get(ctx, googleCalendarType, calID)
	if err != nil {
		t.Fatalf("get calendar mirror: %v", err)
	}
	if mirror.Properties["syncToken"] != "st-2" {
		t.Fatalf("calendar syncToken = %v, want the re-read's st-2", mirror.Properties["syncToken"])
	}
}

// TestGoogleCalendarOriginPinRefusal: pointed at a non-loopback, non-Google
// origin the body refuses to send the token, stamps erroring, and completes.
func TestGoogleCalendarOriginPinRefusal(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not on PATH — the closure's contacts body warms through uv at install")
	}
	ds := openInternalDataset(t)
	googleInstallRewired(t, ds, func(docs []map[string]any) {
		googlePointCalendarAt(docs, "https://intercepted.example")
	})
	googleSeedAccount(t, ds, "acct-step")

	s := newGoogleStepper(t, ds, googleCalendarFn, googleStepConfig(calStepProps(nil)))
	effects, _, cur := s.step(nil)
	if cur != nil {
		t.Fatalf("the refusal did not end the chain")
	}
	stamp := googleAccountStamp(t, effects, "acct-step")
	status, _ := stamp["syncStatus"].(string)
	if !strings.HasPrefix(status, "erroring: ") ||
		!strings.Contains(status, "refusing to send credentials") {
		t.Fatalf("syncStatus = %q, want an erroring refusal", status)
	}
	if !strings.Contains(status, "intercepted.example") {
		t.Fatalf("the refusal does not name the refused origin: %q", status)
	}
	for _, key := range []string{"lastSyncedAt", "calendarLastSyncedAt"} {
		if _, ok := stamp[key]; ok {
			t.Fatalf("a refused run stamped %s", key)
		}
	}
}

// TestGoogleCalendarDeletedCalendarRetracted: a calendar the owner
// unsubscribed from or deleted stops being walked, so its per-calendar 410
// never comes and the sweep that 410 drives never runs. Its mirror, its core
// row and every one of its events would stay live and stale forever. The
// calendarList walk therefore asks for deleted entries and retracts the tree.
func TestGoogleCalendarDeletedCalendarRetracted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fake, ds := newCalendarFixture(t)

	// Round one: the ordinary sync mirrors the calendar and its events.
	cfg := googleStepConfig(calStepProps(nil))
	newGoogleStepper(t, ds, googleCalendarFn, cfg).drainApplying(nil)
	calID := substratefn.ExternalID("gcal-calendar", "acct-step", "primary@example.com")
	e1 := substratefn.ExternalID("gcal-event", calID, "e1")
	if _, err := ds.Get(ctx, googleEventType, e1); err != nil {
		t.Fatalf("round one synced no events: %v", err)
	}

	// Round two: Google reports the calendar as deleted.
	fake.cals = []any{map[string]any{
		"id": "primary@example.com", "summaryOverride": "Work",
		"accessRole": "owner", "primary": true, "deleted": true,
	}}
	before := len(fake.seen())
	newGoogleStepper(t, ds, googleCalendarFn, cfg).drainApplying(nil)

	// The sync ASKED for deleted entries — without that the entry is simply
	// absent and nothing below can ever happen.
	var asked bool
	for _, q := range fake.seen()[before:] {
		if strings.Contains(q, "calendarList") && strings.Contains(q, "showDeleted=true") {
			asked = true
		}
		if isEventsCall(q) {
			t.Fatalf("a deleted calendar was still walked for events: %q", q)
		}
	}
	if !asked {
		t.Fatalf("the calendarList walk never requested deleted entries: %v",
			fake.seen()[before:])
	}

	for _, ref := range []struct{ typ, id, what string }{
		{googleEventType, e1, "the event mirror"},
		{coreEventType, e1, "the core event"},
		{googleCalendarType, calID, "the calendar mirror"},
		{coreCalendarType, calID, "the core calendar"},
	} {
		row, err := ds.Get(ctx, ref.typ, ref.id)
		if err != nil {
			t.Fatalf("get %s: %v", ref.typ, err)
		}
		if row.DeletedAt == nil {
			t.Fatalf("%s survived the calendar's deletion", ref.what)
		}
	}
}

// TestGoogleCalendarErrorRecordedBesideGmailError: the account-level rollup
// holds ONE string, so the "already erroring, leave it alone" guard that
// keeps a stamp from re-firing the on-connect trigger also made a gmail
// failure swallow a calendar one entirely — the calendar breakage was
// recorded nowhere at all. Each stream now records its OWN outcome and only
// RAISES the shared rollup.
func TestGoogleCalendarErrorRecordedBesideGmailError(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("db test")
	}
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not on PATH — the closure's contacts body warms through uv at install")
	}
	ds := openInternalDataset(t)
	googleInstallRewired(t, ds, func(docs []map[string]any) {
		googlePointCalendarAt(docs, "https://intercepted.example")
	})
	googleSeedAccount(t, ds, "acct-step")

	// The gmail stream failed first and owns the rollup.
	props := calStepProps(map[string]any{
		"syncStatus":      "erroring: gmail returned HTTP 500",
		"gmailSyncStatus": "erroring: gmail returned HTTP 500",
	})
	s := newGoogleStepper(t, ds, googleCalendarFn, googleStepConfig(props))
	effects, _, _ := s.step(nil)

	stamp := googleAccountStamp(t, effects, "acct-step")
	status, _ := stamp["calendarSyncStatus"].(string)
	if !strings.HasPrefix(status, "erroring: ") ||
		!strings.Contains(status, "refusing to send credentials") {
		t.Fatalf("calendarSyncStatus = %q — the calendar failure went unrecorded "+
			"because gmail was already erroring", status)
	}
	// The rollup already names a failure: re-stamping it would let the
	// account's own update ping-pong the two streams' on-connect triggers.
	if _, ok := stamp["syncStatus"]; ok {
		t.Fatalf("the rollup was re-stamped over another stream's live failure: %v", stamp)
	}
	for _, key := range []string{"lastSyncedAt", "calendarLastSyncedAt"} {
		if _, ok := stamp[key]; ok {
			t.Fatalf("a refused run stamped %s", key)
		}
	}
}
