package providertest

// The Google bundle's CALENDAR stream, proved the same two ways
// as the gmail stream: pure schema admission (the scope, the three mirror
// kinds, the emit ceiling that lets the body write its rows at all), then
// the paged cursor driven page by page against a loopback Google Calendar.
//
// The provider rules the stepper tests exist to pin, because getting any of
// them wrong is silent data loss:
//
//   - The walk is singleEvents=false: a recurring master is ONE item carrying
//     its rule and lands as a `series` row, a modified exception is an `event`
//     row pointing at it, a canceled exception is a slot the series spends,
//     and nothing is expanded — the records read computes the occurrences.
//   - nextSyncToken arrives ONLY on the final page, and the token commits
//     nowhere else — a page that fails leaves the previous token standing and
//     the whole delta re-reads.
//   - An incremental call repeats the initial query parameters exactly and
//     carries NO time bounds; Google 400s otherwise.
//   - A 410 GONE, and only a 410, drops to a full re-read under a fresh
//     generation, followed by a sweep of that one calendar.
//   - A delta's retraction entries (the provider's canceled status) are
//     DELETES; core retracts rather than tombstoning.
//   - A freeBusyReader share carries no content and is never synced.
//   - A token minted under the instance walk this one replaced reads as
//     absent, and that first run retracts every legacy instance row.

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const googleSeriesType = googlePackage + "/series"

// propCanceledSlots is the series property named the provider's way; spelled
// apart, because the US-locale misspell rule refuses the literal.
const propCanceledSlots = "cancel" + "ledSlots"

// The fixture's one recurring master and its two exceptions, FIXED rather than
// relative: a series row is swept by generation and never by date, and a slot
// is a spelling the assertions have to name. Europe/London in July is UTC+1.
const (
	masterStart = "2026-07-15T13:00:00+01:00"
	masterEnd   = "2026-07-15T14:00:00+01:00"
	masterAtUTC = "2026-07-15T12:00:00Z"
	// The EXDATE line below names 2026-08-05 13:00 London; the RDATE line is
	// spelled in UTC already.
	exdateUTC = "2026-08-05T12:00:00Z"
	rdateUTC  = "2026-08-19T12:00:00Z"
	// The moved exception replaces the 22 July slot and sits two hours later.
	movedOriginal = "2026-07-22T13:00:00+01:00"
	movedSlotUTC  = "2026-07-22T12:00:00Z"
	movedSlot     = "20260722T120000Z"
	movedStart    = "2026-07-22T15:00:00+01:00"
	movedEnd      = "2026-07-22T16:00:00+01:00"
	// The canceled exception spends the 29 July slot.
	canceledOriginal = "2026-07-29T13:00:00+01:00"
	canceledSlotUTC  = "2026-07-29T12:00:00Z"
	canceledSlot     = "20260729T120000Z"
)

var masterLines = []string{
	"RRULE:FREQ=WEEKLY;BYDAY=WE",
	"EXDATE;TZID=Europe/London:20260805T130000",
	"RDATE:" + strings.NewReplacer("-", "", ":", "").Replace(rdateUTC),
}

func TestGoogleCalendarBundleAdmitsSchema(t *testing.T) {
	t.Parallel()
	reg := bundleRegistry(t, googleDir)

	b, ok := reg.BundleOf(googlePackage)
	if !ok {
		t.Fatalf("no bundle owns %s", googlePackage)
	}
	scopes := b.OAuth2.FeatureScopes["enabledCalendar"]
	if len(scopes) != 1 || scopes[0] != "https://www.googleapis.com/auth/calendar.readonly" {
		t.Fatalf("enabledCalendar scopes = %v, want the single calendar.readonly", scopes)
	}

	cal := mustKind(t, reg, googleCalendarType)
	// The per-calendar sync token lives HERE, not on the account: a
	// {calendarId: token} map on one account row would let one calendar's
	// advance skip another's tail. `syncWalk` names the walk it was minted
	// under, so a token from the instance walk reads as absent.
	mustProps(t, cal, "account", "calendarId", "summary", "timezone",
		"accessRole", "primary", "selected", "syncToken", "syncWalk",
		"syncGeneration", "raw")

	// The master: core's `recurring` on a range, the rule and its dates
	// beside the verbatim lines and every slot a cancellation spent.
	series := mustKind(t, reg, googleSeriesType)
	mustProps(t, series, "account", "calendar", "calendarId", "eventId",
		"icalUID", "syncGeneration", "recurrence", "rdates", "exdates",
		"timezone", "recurrenceLines", propCanceledSlots, "status", "summary",
		"description", "location", "allDay", "meetingURL", "eventType",
		"transparency", "visibility", "providerUpdatedAt", "organizer",
		"attendees", "raw")
	mustBind(t, series, vocabulary.CoreKind("temporal"), vocabulary.CoreKind("recurring"))
	if !series.UsesHot("at") || !series.UsesHot("endsAt") {
		t.Fatalf("series binds temporal without the range's two columns: %v", series.HotColumns)
	}

	// The event: core's `override` on a range, pointing at the series and
	// naming the slot; the instance walk's three properties deprecated and
	// still declared, because live rows hold them.
	evt := mustKind(t, reg, googleEventType)
	mustProps(t, evt, "account", "calendar", "calendarId", "eventId", "icalUID",
		"syncGeneration", "status", "summary", "description", "location",
		"startAt", "endAt", "allDay", "timezone", "recurrenceOf", "originalAt",
		"recurringEventId", "originalStartTime", "recurrence", "meetingURL",
		"eventType", "transparency", "visibility", "providerUpdatedAt",
		"organizer", "attendees", "raw")
	mustBind(t, evt, vocabulary.CoreKind("temporal"), vocabulary.CoreKind("override"))
	for _, name := range []string{"startAt", "endAt", "recurrence"} {
		p, _ := evt.Prop(name)
		if !p.Deprecated {
			t.Fatalf("event.%s is not deprecated — the instants are at/endsAt and the rule is the series'", name)
		}
	}
	ref, _ := evt.Prop("recurrenceOf")
	if ref.To != googleSeriesType || ref.OnDelete != vocabulary.OnDeleteCascade || ref.MustExist {
		t.Fatalf("event.recurrenceOf = to %q onDelete %q mustExist %v; want the series, cascade, and no mustExist (an exception may land before its master)",
			ref.To, ref.OnDelete, ref.MustExist)
	}
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
	// Mirrors only: the ceiling names this package's own kinds and nothing
	// else (record 49).
	mustEmit(t, fn, googleCalendarType, googleSeriesType, googleEventType,
		googleAddressType, googleAccountType)
	mustRead(t, fn, googleCalendarType, googleSeriesType, googleEventType)
	for _, ident := range fn.Caps.Emit {
		if strings.HasPrefix(ident, enginetest.SampleAuthority+"/") {
			t.Fatalf("calendarsync may write %s, a kind this package does not own", ident)
		}
	}
	if strings.Contains(fn.Source, "# /// script") {
		t.Fatalf("calendarsync declares PEP 723 dependencies — it is meant to run on the dependency-free fast path")
	}

	acct := mustKind(t, reg, googleAccountType)
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
}

// mustBind asserts a kind binds every named trait identity.
func mustBind(t *testing.T, kind *vocabulary.Kind, idents ...string) {
	t.Helper()
	for _, want := range idents {
		found := false
		for _, b := range kind.Traits {
			if b.Identity == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s does not bind %s: %+v", kind.Identity, want, kind.Traits)
		}
	}
}

// --- the loopback Google Calendar -------------------------------------------

type fakeGCal struct {
	fakeAPI

	// cals is the calendarList page.
	cals []any
	// pages is the events walk for the primary calendar, one entry per page;
	// the LAST page is the only one carrying nextSyncToken. With
	// singleEvents=false a page carries masters, exceptions and singles alike.
	pages [][]any
	// syncToken, once non-empty, is what the final page hands back.
	syncToken string
	// gone makes any request CARRYING a syncToken answer 410 GONE.
	gone bool
}

// calendarPath is the events walk's own path prefix. The recorded request is
// the ESCAPED path plus the query, because the calendar id lives in the PATH
// and nowhere else: recording the query alone made "was this calendar
// walked?" an assertion that could never fail.
const calendarPath = "/calendar/v3/calendars/"

// isEventsCall is the events LIST walk.
func isEventsCall(q string) bool {
	return strings.HasPrefix(q, calendarPath)
}

func newFakeGCal(t *testing.T) *fakeGCal {
	t.Helper()
	f := &fakeGCal{syncToken: "st-1"}
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
		q := r.URL.Query()
		// A master only appears in a list that did NOT ask for the expansion;
		// a walk that still asks for singleEvents=true would get instances
		// and no rule, so the fake refuses it outright.
		if q.Get("singleEvents") != "false" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
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
	f.serve(t, mux)
	return f
}

func gcalEvent(id, summary, start, end string) map[string]any {
	return map[string]any{
		"id": id, "status": "confirmed", "summary": summary,
		"iCalUID": id + "@google.com", "updated": ago(time.Hour),
		"location":  "Room 1",
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

// gcalMaster is a recurring master as a singleEvents=false list carries it:
// the rule, the skipped dates and the extra dates as ONE list of iCalendar
// lines, in both the zoned and the UTC spelling, and its own DTSTART/DTEND in
// its own zone.
func gcalMaster(id, summary string) map[string]any {
	item := gcalEvent(id, summary, masterStart, masterEnd)
	item["start"] = map[string]any{"dateTime": masterStart, "timeZone": "Europe/London"}
	item["end"] = map[string]any{"dateTime": masterEnd, "timeZone": "Europe/London"}
	lines := make([]any, len(masterLines))
	for i, l := range masterLines {
		lines[i] = l
	}
	item["recurrence"] = lines
	return item
}

// gcalException is one MODIFIED occurrence of a master as Google lists it:
// its own item, the master it came from and the slot the rule produced. Its
// id and UID are the master's, the way Google spells them.
func gcalException(masterID, summary, start, end, originalStart, slot string) map[string]any {
	item := gcalEvent(masterID+"_"+slot, summary, start, end)
	item["iCalUID"] = masterID + "@google.com"
	item["recurringEventId"] = masterID
	item["originalStartTime"] = map[string]any{"dateTime": originalStart, "timeZone": "Europe/London"}
	return item
}

// gcalCanceledException is what a delta (or showDeleted=true) carries for a
// canceled occurrence: the tombstone, the master and the slot, and nothing
// else. Spelled the provider's way.
func gcalCanceledException(masterID, originalStart, slot string) map[string]any {
	return map[string]any{
		"id": masterID + "_" + slot, "status": "cancel" + "led",
		"recurringEventId":  masterID,
		"originalStartTime": map[string]any{"dateTime": originalStart},
	}
}

// gcalCanceled is a deleted single event or a deleted master: Google's
// tombstone does not say which.
func gcalCanceled(id string) map[string]any {
	return map[string]any{"id": id, "status": "cancel" + "led"}
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

// instantOf reads a hot column back as its RFC 3339 UTC spelling, "" when
// unset.
func instantOf(ts *time.Time) string {
	if ts == nil {
		return ""
	}
	return ts.UTC().Format(time.RFC3339)
}

func calStepProps(extra map[string]any) map[string]any {
	return syncProps(extra, "enabledCalendar")
}

// calStepConfig is the calendar account a stepped invocation walks.
func calStepConfig(props map[string]any) map[string]any {
	return stepConfig(googleAccountType, googleAccountID, props)
}

// The fixture's derived ids: the primary calendar's mirror, the master's
// series row, and the moved exception's row at `<series>_<slot>`.
func fixtureIDs() (calID, seriesID, movedID string) {
	calID = runner.ExternalID("gcal-calendar", "acct-step", "primary@example.com")
	seriesID = runner.ExternalID("gcal-series", calID, "master-1")
	return calID, seriesID, seriesID + "_" + movedSlot
}

func newCalendarFixture(t *testing.T) (*fakeGCal, substrate.Dataset) {
	t.Helper()
	requireUV(t)
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
	// The singles are dated RELATIVE to now: a hard-coded instant that sits
	// "inside the last-30-days window" today sits outside it thirty days from
	// now, and the sweep assertions would start failing on a calendar. The
	// master's exceptions land on the FIRST page and the master on the
	// second, so the walk proves a cancellation that arrives before its
	// master is kept in a shell row the master's own write completes.
	fake.pages = [][]any{
		{
			gcalEvent("e1", "Standup", ahead(24*time.Hour), ahead(25*time.Hour)),
			gcalException("master-1", "Weekly sync (moved)", movedStart, movedEnd, movedOriginal, movedSlot),
			gcalCanceledException("master-1", canceledOriginal, canceledSlot),
		},
		{
			gcalMaster("master-1", "Weekly sync"),
			gcalEvent("e2", "Design review", ahead(48*time.Hour), ahead(49*time.Hour)),
		},
	}
	_, ds := newDataset(t)
	googleInstall(t, ds, calendarAPIAt(fake.ts.URL))
	googleSeedAccount(t, ds, googleAccountID)
	return fake, ds
}

// TestGoogleCalendarFakeSyncMirrors drives a first (full) sync and asserts
// the calendar's shape: the calendar mirror, the plain events on ids that
// nest the calendar's, the attendees as emailaddress mirrors with an empty
// subject slot, the freeBusyReader share skipped, the pinned walk parameters
// on every call, and the sync token committed exactly once, from the FINAL
// page, with the walk stamped beside it.
func TestGoogleCalendarFakeSyncMirrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fake, ds := newCalendarFixture(t)

	s := newStepper(t, ds, googleCalendarFn, calStepConfig(calStepProps(nil)))
	effects := s.drainApplying(nil)

	calID, _, _ := fixtureIDs()
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
	if mirror.Properties["syncWalk"] != "masters" {
		t.Fatalf("calendar syncWalk = %v, want the walk stamped once the sweep completed", mirror.Properties["syncWalk"])
	}
	if mirror.Properties["timezone"] != "Europe/London" {
		t.Fatalf("calendar mirror timezone = %v", mirror.Properties["timezone"])
	}
	// MIRRORS ONLY (record 49): the shared calendar kinds belong to a package
	// this one does not own, and nothing here writes one.
	if _, err := ds.Get(ctx, coreCalendarType, calID); err == nil {
		t.Fatalf("the sync wrote %s, a kind this package does not own", coreCalendarType)
	}

	// The freeBusyReader share carries no content: never mirrored, never
	// walked for events.
	busyID := runner.ExternalID("gcal-calendar", "acct-step", "busy@example.com")
	if row, err := ds.Get(ctx, googleCalendarType, busyID); err == nil && row.DeletedAt == nil {
		t.Fatalf("a freeBusyReader share was mirrored")
	}
	for _, q := range fake.seen() {
		if strings.Contains(q, "busy%40example.com") {
			t.Fatalf("a freeBusyReader share was walked for events: %v", fake.seen())
		}
	}

	// Both pages' singles landed on ids that nest the calendar's own (a
	// Google event id is unique per calendar, not globally), with their
	// instants in the temporal trait's columns and the instance walk's
	// deprecated pair never written.
	for _, id := range []string{"e1", "e2"} {
		evtID := runner.ExternalID("gcal-event", calID, id)
		row, err := ds.Get(ctx, googleEventType, evtID)
		if err != nil {
			t.Fatalf("event mirror %s did not sync: %v", id, err)
		}
		if row.Properties["calendarId"] != "primary@example.com" {
			t.Fatalf("event mirror %s calendarId = %v", id, row.Properties["calendarId"])
		}
		if row.At == nil || row.EndsAt == nil {
			t.Fatalf("event mirror %s missing its instants: at=%v endsAt=%v", id, row.At, row.EndsAt)
		}
		for _, dead := range []string{"startAt", "endAt", "recurrence", "recurrenceOf", "originalAt"} {
			if _, ok := row.Properties[dead]; ok {
				t.Fatalf("event mirror %s carries %s = %v; a single event carries neither the deprecated pair nor an override's", id, dead, row.Properties[dead])
			}
		}
		if got := refIDs(row, "calendar"); len(got) != 1 || got[0] != calID {
			t.Fatalf("event mirror %s calendar = %v, want the calendar mirror %s", id, got, calID)
		}
		if _, err := ds.Get(ctx, coreEventType, evtID); err == nil {
			t.Fatalf("the sync wrote %s, a kind this package does not own", coreEventType)
		}
		// Every attendee address lands as an emailaddress mirror with an
		// EMPTY subject slot: what a person is belongs to the repository.
		addrID := runner.ExternalID("google-address", "acct-step", "alice@example.com")
		addr, err := ds.Get(ctx, googleAddressType, addrID)
		if err != nil {
			t.Fatalf("emailaddress mirror did not sync: %v", err)
		}
		if got := refIDs(addr, "person"); len(got) != 0 {
			t.Fatalf("the address row filled its subject slot with %v, and no mapping is declared", got)
		}
	}

	// responseStatus is on the MIRROR, where the per-attendee answer belongs.
	e1 := runner.ExternalID("gcal-event", calID, "e1")
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

	// The tokenless pages carried the floor and NO ceiling (a master is one
	// row, so nothing pages forever and no horizon exists); every call
	// carried the pinned parameters; the token was committed once, and only
	// from the page that omitted nextPageToken.
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
		if strings.Contains(q, "timeMax=") {
			t.Fatalf("an events call carried a ceiling — the walk has no horizon: %q", q)
		}
		if !strings.Contains(q, "singleEvents=false") ||
			!strings.Contains(q, "showDeleted=true") ||
			!strings.Contains(q, "maxResults=250") {
			t.Fatalf("an events call dropped a pinned parameter: %q", q)
		}
	}
	if full != 2 || incremental != 0 {
		t.Fatalf("full=%d incremental=%d over %v", full, incremental, fake.seen())
	}

	stamp := accountStamp(t, effects, googleAccountType, googleAccountID)
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

// TestGoogleCalendarSeriesMirrorsTheMaster: a recurring master is ONE
// `series` row binding core's `recurring` — its DTSTART/DTEND in at/endsAt,
// the RRULE line as its rule, the EXDATE and RDATE lines resolved in the
// series' one zone, the verbatim lines kept — a modified exception is an
// `event` row at `<series>_<slot>` pointing at it, and a canceled exception
// is a slot the series spends (in canceledSlots and so in exdates), never a
// row. The cancellation and the moved exception arrive a page BEFORE the
// master, so the master's write completes a shell.
func TestGoogleCalendarSeriesMirrorsTheMaster(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newCalendarFixture(t)

	newStepper(t, ds, googleCalendarFn, calStepConfig(calStepProps(nil))).
		drainApplying(nil)

	calID, seriesID, movedID := fixtureIDs()
	series, err := ds.Get(ctx, googleSeriesType, seriesID)
	if err != nil {
		t.Fatalf("the series row did not sync: %v", err)
	}
	if got := instantOf(series.At); got != masterAtUTC {
		t.Fatalf("series at = %q, want the master's DTSTART %s", got, masterAtUTC)
	}
	if got := instantOf(series.EndsAt); got != "2026-07-15T13:00:00Z" {
		t.Fatalf("series endsAt = %q, want the master's DTEND", got)
	}
	if got, _ := series.Properties["recurrence"].(string); got != masterLines[0] {
		t.Fatalf("series recurrence = %q, want the RRULE line verbatim", got)
	}
	if got := gcalStrings(series.Properties["recurrenceLines"]); !slices.Equal(got, masterLines) {
		t.Fatalf("series recurrenceLines = %v, want Google's array verbatim", got)
	}
	if got := series.Properties["timezone"]; got != "Europe/London" {
		t.Fatalf("series timezone = %v, want the master's own zone", got)
	}
	if got := gcalStrings(series.Properties["rdates"]); !slices.Equal(got, []string{rdateUTC}) {
		t.Fatalf("series rdates = %v, want the RDATE line's instant", got)
	}
	// exdates = the EXDATE line's instant ∪ the canceled slot; the slot the
	// moved exception claims is NOT here — the read suppresses it through
	// the override row itself.
	if got := gcalStrings(series.Properties["exdates"]); !slices.Equal(got, []string{canceledSlotUTC, exdateUTC}) {
		t.Fatalf("series exdates = %v, want the EXDATE instant and the canceled slot", got)
	}
	if got := gcalStrings(series.Properties[propCanceledSlots]); !slices.Equal(got, []string{canceledSlotUTC}) {
		t.Fatalf("series canceledSlots = %v, want the one canceled slot", got)
	}
	if series.Properties["summary"] != "Weekly sync" || series.Properties["eventId"] != "master-1" {
		t.Fatalf("series carries the wrong master: %v", series.Properties)
	}
	if got := refIDs(series, "calendar"); len(got) != 1 || got[0] != calID {
		t.Fatalf("series calendar = %v, want %s", got, calID)
	}
	if s, _ := series.Properties["syncGeneration"].(string); s == "" {
		t.Fatalf("the series was not stamped with the full read's generation")
	}

	// The moved exception: its own instants, the series and the slot it
	// replaces, resolved in the series' zone; Google's two verbatim beside.
	moved, err := ds.Get(ctx, googleEventType, movedID)
	if err != nil {
		t.Fatalf("the exception did not land at <series>_<slot> %s: %v", movedID, err)
	}
	if got := refIDs(moved, "recurrenceOf"); len(got) != 1 || got[0] != seriesID {
		t.Fatalf("exception recurrenceOf = %v, want the series %s", got, seriesID)
	}
	if got := moved.Properties["originalAt"]; got != movedSlotUTC {
		t.Fatalf("exception originalAt = %v, want the slot %s", got, movedSlotUTC)
	}
	if got := instantOf(moved.At); got != "2026-07-22T14:00:00Z" {
		t.Fatalf("exception at = %q, want its own moved start", got)
	}
	if moved.Properties["recurringEventId"] != "master-1" || moved.Properties["originalStartTime"] != movedOriginal {
		t.Fatalf("exception dropped Google's verbatim pair: %v", moved.Properties)
	}
	if got := refIDs(moved, "calendar"); len(got) != 1 || got[0] != calID {
		t.Fatalf("exception calendar = %v, want %s", got, calID)
	}
	for _, dead := range []string{"startAt", "endAt", "recurrence"} {
		if _, ok := moved.Properties[dead]; ok {
			t.Fatalf("exception carries the deprecated %s", dead)
		}
	}

	// The canceled exception is a spent slot and never a row.
	if row, err := ds.Get(ctx, googleEventType, seriesID+"_"+canceledSlot); err == nil && row.DeletedAt == nil {
		t.Fatalf("a canceled exception landed as a live row: %v", row.Properties)
	}
	// No shared-kind series row (record 49).
	if _, err := ds.Get(ctx, coreSeriesType, seriesID); err == nil {
		t.Fatalf("the sync wrote %s, a kind this package does not own", coreSeriesType)
	}
	if got := countLive(t, ds, googleSeriesType); got != 1 {
		t.Fatalf("%d series rows, want the one master", got)
	}
}

// TestGoogleCalendarDeltaClearsAndCancels runs an incremental delta over the
// first read: a single whose location Google cleared loses it on the mirror
// (a put merges, so only an explicit null clears), a second canceled
// exception joins the stored series' spent slots without losing the first,
// and a canceled master takes its series row and every exception pointing
// at it in the same delivery.
func TestGoogleCalendarDeltaClearsAndCancels(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fake, ds := newCalendarFixture(t)
	cfg := calStepConfig(calStepProps(nil))
	newStepper(t, ds, googleCalendarFn, cfg).drainApplying(nil)
	calID, seriesID, movedID := fixtureIDs()
	e1 := runner.ExternalID("gcal-event", calID, "e1")
	if row := mustGet(t, ds, googleEventType, e1); row.Properties["location"] != "Room 1" {
		t.Fatalf("round one dropped the location: %v", row.Properties)
	}

	// Delta one: e1 without a location, and one more canceled slot.
	cleared := gcalEvent("e1", "Standup", ahead(24*time.Hour), ahead(25*time.Hour))
	delete(cleared, "location")
	const secondSlot = "2026-08-12T12:00:00Z"
	fake.syncToken = "st-2"
	fake.pages = [][]any{{
		cleared,
		gcalCanceledException("master-1", "2026-08-12T13:00:00+01:00", "20260812T120000Z"),
	}}
	before := len(fake.seen())
	newStepper(t, ds, googleCalendarFn, cfg).drainApplying(nil)
	for _, q := range fake.seen()[before:] {
		if isEventsCall(q) && !strings.Contains(q, "syncToken=st-1") {
			t.Fatalf("the delta did not ride the stored token: %q", q)
		}
	}
	row := mustGet(t, ds, googleEventType, e1)
	if _, ok := row.Properties["location"]; ok {
		t.Fatalf("a location Google cleared survived on the mirror: %v", row.Properties["location"])
	}
	if row.Properties["summary"] != "Standup" {
		t.Fatalf("the delta lost the summary: %v", row.Properties)
	}
	series := mustGet(t, ds, googleSeriesType, seriesID)
	if got := gcalStrings(series.Properties[propCanceledSlots]); !slices.Equal(got, []string{canceledSlotUTC, secondSlot}) {
		t.Fatalf("series canceledSlots = %v, want both canceled slots — the list only grows", got)
	}
	if got := gcalStrings(series.Properties["exdates"]); !slices.Equal(got, []string{canceledSlotUTC, exdateUTC, secondSlot}) {
		t.Fatalf("series exdates = %v, want the EXDATE line and both canceled slots", got)
	}
	if got, _ := series.Properties["recurrence"].(string); got != masterLines[0] {
		t.Fatalf("the shell put over a stored master lost its rule: %q", got)
	}

	// Delta two: the master itself is canceled. Google's tombstone is a bare
	// id and status; the series goes, and so does the exception pointing at
	// it, explicitly, without waiting for the cascade's GC.
	fake.syncToken = "st-3"
	fake.pages = [][]any{{gcalCanceled("master-1")}}
	newStepper(t, ds, googleCalendarFn, cfg).drainApplying(nil)
	for _, ref := range []struct{ kind, id, what string }{
		{googleSeriesType, seriesID, "the series row"},
		{googleEventType, movedID, "the exception pointing at it"},
	} {
		row, err := ds.Get(ctx, ref.kind, ref.id)
		if err != nil {
			t.Fatalf("get %s: %v", ref.what, err)
		}
		if row.DeletedAt == nil {
			t.Fatalf("%s survived the master's cancellation", ref.what)
		}
	}
	for _, id := range []string{"e1", "e2"} {
		if row := mustGet(t, ds, googleEventType, runner.ExternalID("gcal-event", calID, id)); row.DeletedAt != nil {
			t.Fatalf("the master's cancellation took the unrelated single %s", id)
		}
	}
	if mirror := mustGet(t, ds, googleCalendarType, calID); mirror.Properties["syncToken"] != "st-3" {
		t.Fatalf("calendar syncToken = %v, want the last delta's st-3", mirror.Properties["syncToken"])
	}
}

// TestGoogleCalendarAccountDisconnectCascades proves the cascading `account`
// owner pointer end to end on the SHIPPED closure (0032): a real calendar sync
// mirrors the calendar, its series and its events through the actual
// connector body, and disconnecting the account collects all of them,
// through the ordinary GC sweep.
func TestGoogleCalendarAccountDisconnectCascades(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newCalendarFixture(t)

	s := newStepper(t, ds, googleCalendarFn, calStepConfig(calStepProps(nil)))
	s.drainApplying(nil)

	calID, seriesID, movedID := fixtureIDs()
	evtID := runner.ExternalID("gcal-event", calID, "e1")
	for _, ref := range []struct{ kind, id string }{
		{googleCalendarType, calID},
		{googleSeriesType, seriesID},
		{googleEventType, evtID},
		{googleEventType, movedID},
	} {
		if _, err := ds.Get(ctx, ref.kind, ref.id); err != nil {
			t.Fatalf("%s %s did not sync: %v", ref.kind, ref.id, err)
		}
	}

	// Disconnect the account, the owner-managed delete the console issues, and
	// run the sweep the server runs on its own cadence.
	if _, err := ds.Delete(ctx, substrate.ActorAPI, googleAccountType, "acct-step", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete account: %v", err)
	}
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc: %v", err)
	}
	if _, err := ds.Get(ctx, googleAccountType, "acct-step"); err == nil {
		t.Fatal("the disconnected account should be hard-deleted")
	}
	for _, ref := range []struct{ kind, id, what string }{
		{googleCalendarType, calID, "the calendar mirror"},
		{googleSeriesType, seriesID, "the series row"},
		{googleEventType, evtID, "the single event"},
		{googleEventType, movedID, "the exception"},
	} {
		if _, err := ds.Get(ctx, ref.kind, ref.id); err == nil {
			t.Fatalf("%s should have been collected through the account's cascade", ref.what)
		}
	}
}

// TestGoogleCalendarTokenGoneFullReread runs the whole incremental lifecycle:
// a first full read stores the token, a second run goes incremental, a 410
// drops it for a full re-read under a fresh generation, a retraction entry
// takes its row, and the sweep that follows retracts what the generation did
// not stamp: event rows at or above the floor (and no ceiling: the read has
// none), EVERY series row whatever its dates, and nothing below the floor.
func TestGoogleCalendarTokenGoneFullReread(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fake, ds := newCalendarFixture(t)
	cfg := calStepConfig(calStepProps(nil))

	// Round one: the full read stores st-1 on the calendar mirror.
	newStepper(t, ds, googleCalendarFn, cfg).drainApplying(nil)
	calID, seriesID, movedID := fixtureIDs()

	// Rows a previous run left behind under a stale generation, one per edge
	// of the sweep: a single inside the floor, one deep in the archive below
	// it, one far ahead (the old walk's horizon spared it; this one has no
	// horizon and re-reads it), and a series row anchored years ago.
	inside := runner.ExternalID("gcal-event", calID, "stale-inside")
	archived := runner.ExternalID("gcal-event", calID, "archived")
	beyond := runner.ExternalID("gcal-event", calID, "beyond-horizon")
	for id, at := range map[string]string{
		inside:   ago(24 * time.Hour),
		archived: ago(7 * 365 * 24 * time.Hour),
		beyond:   ahead(400 * 24 * time.Hour),
	} {
		mustPut(t, ds, substrate.PutInput{
			Kind: googleEventType, ID: id,
			Properties: map[string]any{
				"account": "acct-step", "calendarId": "primary@example.com",
				"eventId": id, "syncGeneration": "an-older-generation",
				"at": at, "status": "confirmed",
			},
		})
	}
	staleSeries := runner.ExternalID("gcal-series", calID, "stale-master")
	mustPut(t, ds, substrate.PutInput{
		Kind: googleSeriesType, ID: staleSeries,
		Properties: map[string]any{
			"account": "acct-step", "calendarId": "primary@example.com",
			"eventId": "stale-master", "syncGeneration": "an-older-generation",
			"at": ago(7 * 365 * 24 * time.Hour), "recurrence": "RRULE:FREQ=DAILY;COUNT=3",
		},
	})

	// Round two: the stored token is GONE. The re-read carries the singles
	// and NOT the master, so the master is what a full read cannot tombstone
	// and the generation sweep must catch.
	fake.gone = true
	fake.syncToken = "st-2"
	fake.pages = [][]any{{
		gcalCanceled("e1"),
		gcalEvent("e2", "Design review", ahead(48*time.Hour), ahead(49*time.Hour)),
	}}
	before := len(fake.seen())
	effects := newStepper(t, ds, googleCalendarFn, cfg).drainApplying(nil)

	// A delta ALWAYS carries cancellations: they are the provider's ordinary
	// housekeeping, not a failure. Counting them as "skipped" made every
	// healthy calendar sync read as partially broken on the console.
	if stamp := accountStamp(t, effects, googleAccountType, googleAccountID); stamp["syncStatus"] != "ok" {
		t.Fatalf("syncStatus = %v — a canceled event is not a skipped one",
			stamp["syncStatus"])
	}

	// The incremental attempt happened (and 410'd), then the full re-read ran
	// with the floor and no token.
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

	// The retracted event is gone.
	e1 := runner.ExternalID("gcal-event", calID, "e1")
	if row := mustGet(t, ds, googleEventType, e1); row.DeletedAt == nil {
		t.Fatalf("a retracted event survived as a live %s row", googleEventType)
	}

	// The sweep: the in-window stale row is gone, the far-ahead one too (no
	// horizon spares it any more: the re-read reaches it and did not carry
	// it), the archived one below the floor is untouched.
	if row := mustGet(t, ds, googleEventType, inside); row.DeletedAt == nil {
		t.Fatalf("the in-window stale row survived the sweep")
	}
	if row := mustGet(t, ds, googleEventType, beyond); row.DeletedAt == nil {
		t.Fatalf("a stale row far ahead survived the sweep — the walk has no ceiling, so the re-read would have carried it")
	}
	if row := mustGet(t, ds, googleEventType, archived); row.DeletedAt != nil {
		t.Fatalf("the sweep deleted an event BELOW the re-read's floor")
	}
	// Series rows are swept whatever their dates: a master is one row, and a
	// master the re-read did not carry is a deleted series (spike 2's
	// fallback). Its exception goes with it through the cascade at GC.
	for _, ref := range []struct{ id, what string }{
		{staleSeries, "the stale series anchored years ago"},
		{seriesID, "the master the re-read did not carry"},
	} {
		if row := mustGet(t, ds, googleSeriesType, ref.id); row.DeletedAt == nil {
			t.Fatalf("%s survived the sweep", ref.what)
		}
	}
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc: %v", err)
	}
	if _, err := ds.Get(ctx, googleEventType, movedID); err == nil {
		t.Fatalf("the swept series' exception was not collected through recurrenceOf's cascade")
	}

	// The fresh token committed, once, from the final page.
	if mirror := mustGet(t, ds, googleCalendarType, calID); mirror.Properties["syncToken"] != "st-2" {
		t.Fatalf("calendar syncToken = %v, want the re-read's st-2", mirror.Properties["syncToken"])
	}
}

// TestGoogleCalendarLegacyInstancesRetracted is the first run after the
// upgrade from the instance walk: the calendar mirror holds a sync token and
// no `syncWalk`, and the old walk's rows (startAt, no at) sit at every date.
// The token reads as absent, so the run is a full read — no call carries it
// — and its sweep, once, retracts every legacy instance (recurringEventId, no
// recurrenceOf) at ANY date and moves every legacy plain row the read never
// re-fetches onto at/endsAt, before stamping the walk.
func TestGoogleCalendarLegacyInstancesRetracted(t *testing.T) {
	t.Parallel()
	fake, ds := newCalendarFixture(t)
	calID, _, _ := fixtureIDs()

	// What the instance walk left: the mirror with its token, an instance
	// row seven years back and one inside the window, both carrying the
	// master's id and the deprecated instants and no `at`, a plain row
	// inside the window the re-read carries again, and a plain row seven
	// years back that no read reaches.
	mustPut(t, ds, substrate.PutInput{
		Kind: googleCalendarType, ID: calID,
		Properties: map[string]any{
			"account": "acct-step", "calendarId": "primary@example.com",
			"syncToken": "st-legacy", "syncGeneration": "the-instance-walk",
		},
	})
	oldInstance := runner.ExternalID("gcal-event", calID, "master-1_20190717T120000Z")
	recentInstance := runner.ExternalID("gcal-event", calID, "master-1_recent")
	for id, startAt := range map[string]string{
		oldInstance:    ago(7 * 365 * 24 * time.Hour),
		recentInstance: ago(24 * time.Hour),
	} {
		mustPut(t, ds, substrate.PutInput{
			Kind: googleEventType, ID: id,
			Properties: map[string]any{
				"account": "acct-step", "calendarId": "primary@example.com",
				"eventId": id, "syncGeneration": "the-instance-walk",
				"startAt": startAt, "status": "confirmed",
				"recurringEventId": "master-1", "recurrence": []any{masterLines[0]},
			},
		})
	}
	e1 := runner.ExternalID("gcal-event", calID, "e1")
	mustPut(t, ds, substrate.PutInput{
		Kind: googleEventType, ID: e1,
		Properties: map[string]any{
			"account": "acct-step", "calendarId": "primary@example.com",
			"eventId": "e1", "syncGeneration": "the-instance-walk",
			"startAt": ahead(24 * time.Hour), "endAt": ahead(25 * time.Hour),
			"status": "confirmed",
		},
	})
	archivedPlain := runner.ExternalID("gcal-event", calID, "archived-plain")
	archivedStart, archivedEnd := ago(7*365*24*time.Hour), ago(7*365*24*time.Hour-time.Hour)
	mustPut(t, ds, substrate.PutInput{
		Kind: googleEventType, ID: archivedPlain,
		Properties: map[string]any{
			"account": "acct-step", "calendarId": "primary@example.com",
			"eventId": "archived-plain", "syncGeneration": "the-instance-walk",
			"startAt": archivedStart, "endAt": archivedEnd, "status": "confirmed",
		},
	})

	newStepper(t, ds, googleCalendarFn, calStepConfig(calStepProps(nil))).drainApplying(nil)

	for _, q := range fake.seen() {
		if isEventsCall(q) && strings.Contains(q, "syncToken=") {
			t.Fatalf("a token minted under the instance walk was reused: %q", q)
		}
	}
	for _, ref := range []struct{ id, what string }{
		{oldInstance, "the legacy instance seven years back"},
		{recentInstance, "the legacy instance inside the window"},
	} {
		if row := mustGet(t, ds, googleEventType, ref.id); row.DeletedAt == nil {
			t.Fatalf("%s survived the first run under the masters walk", ref.what)
		}
	}
	// The plain row the re-read carried moved to the trait's columns and
	// dropped the instance walk's pair.
	row := mustGet(t, ds, googleEventType, e1)
	if row.DeletedAt != nil {
		t.Fatalf("the legacy single the re-read carried was retracted")
	}
	if row.At == nil || row.EndsAt == nil {
		t.Fatalf("the re-read did not move the legacy single onto at/endsAt: %v", row.Properties)
	}
	for _, dead := range []string{"startAt", "endAt"} {
		if _, ok := row.Properties[dead]; ok {
			t.Fatalf("the re-put single still carries the deprecated %s", dead)
		}
	}
	// The plain row below the floor, which no read re-fetches, was moved onto
	// the trait's columns in place: same instants, deprecated pair gone, row
	// live — the archive enters a window read from now on.
	archived := mustGet(t, ds, googleEventType, archivedPlain)
	if archived.DeletedAt != nil {
		t.Fatalf("the legacy plain row below the floor was retracted; only instances are")
	}
	if got := instantOf(archived.At); got != archivedStart {
		t.Fatalf("archived plain row at = %q, want its startAt %s", got, archivedStart)
	}
	if got := instantOf(archived.EndsAt); got != archivedEnd {
		t.Fatalf("archived plain row endsAt = %q, want its endAt %s", got, archivedEnd)
	}
	for _, dead := range []string{"startAt", "endAt"} {
		if _, ok := archived.Properties[dead]; ok {
			t.Fatalf("the archived plain row still carries the deprecated %s", dead)
		}
	}
	mirror := mustGet(t, ds, googleCalendarType, calID)
	if mirror.Properties["syncWalk"] != "masters" {
		t.Fatalf("syncWalk = %v, want the walk stamped once the sweep completed", mirror.Properties["syncWalk"])
	}
	if mirror.Properties["syncToken"] != "st-1" {
		t.Fatalf("syncToken = %v, want the full read's fresh st-1", mirror.Properties["syncToken"])
	}
	if mirror.Properties["syncGeneration"] == "the-instance-walk" {
		t.Fatalf("the full read did not mint a fresh generation")
	}
}

// TestGoogleCalendarOriginPinRefusal: pointed at a non-loopback, non-Google
// origin the body refuses to send the token, stamps erroring, and completes.
func TestGoogleCalendarOriginPinRefusal(t *testing.T) {
	t.Parallel()
	requireUV(t)
	_, ds := newDataset(t)
	googleInstall(t, ds, calendarAPIAt("https://intercepted.example"))
	googleSeedAccount(t, ds, googleAccountID)

	s := newStepper(t, ds, googleCalendarFn, calStepConfig(calStepProps(nil)))
	effects, _, cur := s.step(nil)
	if cur != nil {
		t.Fatalf("the refusal did not end the chain")
	}
	stamp := accountStamp(t, effects, googleAccountType, googleAccountID)
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
// never comes and the sweep that 410 drives never runs. Its mirror, its
// series and every one of its events would stay live and stale forever. The
// calendarList walk therefore asks for deleted entries and retracts the tree
// explicitly; the `calendar` reference's cascade is belt and braces.
func TestGoogleCalendarDeletedCalendarRetracted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fake, ds := newCalendarFixture(t)

	// Round one: the ordinary sync mirrors the calendar, its series and its
	// events.
	cfg := calStepConfig(calStepProps(nil))
	newStepper(t, ds, googleCalendarFn, cfg).drainApplying(nil)
	calID, seriesID, movedID := fixtureIDs()
	e1 := runner.ExternalID("gcal-event", calID, "e1")
	for _, ref := range []struct{ kind, id string }{
		{googleEventType, e1}, {googleSeriesType, seriesID}, {googleEventType, movedID},
	} {
		if _, err := ds.Get(ctx, ref.kind, ref.id); err != nil {
			t.Fatalf("round one did not sync %s %s: %v", ref.kind, ref.id, err)
		}
	}

	// Round two: Google reports the calendar as deleted.
	fake.cals = []any{map[string]any{
		"id": "primary@example.com", "summaryOverride": "Work",
		"accessRole": "owner", "primary": true, "deleted": true,
	}}
	before := len(fake.seen())
	newStepper(t, ds, googleCalendarFn, cfg).drainApplying(nil)

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
		{googleEventType, movedID, "the exception"},
		{googleSeriesType, seriesID, "the series row"},
		{googleCalendarType, calID, "the calendar mirror"},
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
	requireUV(t)
	_, ds := newDataset(t)
	googleInstall(t, ds, calendarAPIAt("https://intercepted.example"))
	googleSeedAccount(t, ds, googleAccountID)

	// The gmail stream failed first and owns the rollup.
	props := calStepProps(map[string]any{
		"syncStatus":      "erroring: gmail returned HTTP 500",
		"gmailSyncStatus": "erroring: gmail returned HTTP 500",
	})
	s := newStepper(t, ds, googleCalendarFn, calStepConfig(props))
	effects, _, _ := s.step(nil)

	stamp := accountStamp(t, effects, googleAccountType, googleAccountID)
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
