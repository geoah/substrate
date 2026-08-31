package e2e

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The 600 block: recurring calendars, played the way Google Calendar delivers
// them. The substrate stores a recurrence rule and never expands it (decision
// 0039): occurrences are calendarevent rows a CONNECTOR explodes, pointing at
// their calendareventseries. These cases are that connector, note for note:
// the rules Google emits, the instance overrides, the cancellations, and the
// this-and-following split, each followed by the range queries a calendar
// client would make.
const (
	seriesCollection = "/api/v1/calendar.substrate.reamde.dev/calendareventseries"
	seriesKind       = "calendar.substrate.reamde.dev/calendareventseries"
)

func init() {
	registerCase(600, "CAL-01", "Every rule Google Calendar emits is stored and parseable",
		"The full RRULE matrix Google Calendar produces (daily, intervals, every weekday, Nth and last "+
			"weekday of the month, month days, yearly, COUNT, UNTIL in both spellings, WKST, the RRULE: "+
			"prefix) lands verbatim on a series and compiles under the engine's RFC 5545 parser, which now "+
			"guards the series gate too: garbage rules and multi-line RDATE blocks are refused by name, and "+
			"RDATE extras live in the declared `rdates` list beside `exdates` and the `startsAt` anchor.",
		xcCaseRuleMatrix)
	registerCase(610, "CAL-02", "A long weekday series with an override and a cancellation",
		"A connector explodes three weeks of an every-weekday standup; one instance is moved and retitled "+
			"in place (Google's single-instance override) and one is canceled (retracted, with the exdate "+
			"on the series); window queries return exactly the surviving instants, the moved one at its new "+
			"time only, and the canceled one only under deleted:true.",
		xcCaseWeekdayStandup)
	registerCase(620, "CAL-03", "Nth-weekday series resolve to the right instants",
		"Every-2nd-Tuesday and last-Friday series live side by side for four months; each month window "+
			"returns exactly one instance of each at the right date, the series records never appear in any "+
			"time window (they are definitions, not occurrences), and /incoming on a series lists exactly "+
			"its own occurrences.",
		xcCaseNthWeekday)
	registerCase(630, "CAL-04", "This-and-following: the split Google performs",
		"Changing a weekly series from occurrence five onward is a split: the old series gains UNTIL and "+
			"keeps occurrences one to four, a new series carries five to eight at the new time, the full "+
			"window shows the time change at the boundary, and each series' /incoming holds exactly its half.",
		xcCaseSeriesSplit)
	registerCase(640, "OCC-01", "The occurrences read: a daily-forever dose beside the calendar",
		"A medication taken every day forever is ONE schedule record whose RRULE the substrate stores and "+
			"never expands (decision 0039); GET /occurrences computes its instants in any window (decision "+
			"0043) beside the calendar's materialized rows, the connector-stamped series stay silent where "+
			"their rows answer, a logged dose annotates its slot without suppressing it, and a travel week "+
			"moves seven doses to another timezone with exdates plus rdates, the same mechanics a Google "+
			"instance override uses.",
		xoCaseMedicationWeek)
}

// xcMonday anchors the whole block: the most recent Monday, 09:30 UTC, at
// least two weeks back, so every case's arithmetic starts on a known weekday
// and never collides with another case's instants.
func xcMonday(r *run) time.Time {
	base := r.rep.Started.UTC().Truncate(24*time.Hour).AddDate(0, 0, -14)
	for base.Weekday() != time.Monday {
		base = base.AddDate(0, 0, -1)
	}
	return base.Add(9*time.Hour + 30*time.Minute)
}

// xcSeries writes one recurring definition the way a connector would: the
// rule, its `startsAt` anchor, and the [materializedFrom, materializedUntil)
// span these cases explode into rows themselves, so the occurrences read
// (decision 0043) stays silent here the way it stays silent over a synced
// Google window.
func xcSeries(c *C, id, summary, rule string, exdates []string) record {
	c.t.Helper()
	base := xcMonday(c.r)
	props := map[string]any{
		"summary": summary, "recurrence": rule, "timezone": "Europe/London",
		"startsAt":          base.Format(time.RFC3339),
		"materializedFrom":  base.AddDate(0, 0, -30).Format(time.RFC3339),
		"materializedUntil": base.AddDate(1, 0, 0).Format(time.RFC3339),
	}
	if len(exdates) > 0 {
		props["exdates"] = exdates
	}
	return c.putRec(seriesCollection, id, props, []edge{{Rel: "calendar", To: edgeTarget{ID: "work"}}})
}

// xcOccurrence explodes one instant of a series into a concrete event.
func xcOccurrence(c *C, id, seriesID, summary string, at time.Time, length time.Duration) record {
	c.t.Helper()
	return c.putRec(eventCollection, id, map[string]any{
		"summary": summary,
		"at":      at.Format(time.RFC3339),
		"endsAt":  at.Add(length).Format(time.RFC3339),
	}, []edge{
		{Rel: "calendar", To: edgeTarget{ID: "work"}},
		{Rel: "series", To: edgeTarget{Kind: seriesKind, ID: seriesID}},
	})
}

// xcWindow is the calendar client's read: live events in [from, to), by time.
func xcWindow(c *C, from, to time.Time) []record {
	c.t.Helper()
	filter := fmt.Sprintf(`{"properties":{"at":{"gte":%q,"lt":%q}}}`,
		from.Format(time.RFC3339), to.Format(time.RFC3339))
	var page struct {
		Records []record `json:"records"`
	}
	status, raw := c.do(http.MethodGet,
		eventCollection+"?filter="+url.QueryEscape(filter)+"&orderBy=at&first=200&withEdges=1", nil, &page)
	c.requiref(status == http.StatusOK, "the window query answered %d: %s", status, raw)
	return page.Records
}

// xcWindowIDs are the window's record ids, in at order.
func xcWindowIDs(c *C, from, to time.Time) []string {
	c.t.Helper()
	recs := xcWindow(c, from, to)
	ids := make([]string, 0, len(recs))
	for _, rec := range recs {
		ids = append(ids, rec.ID)
	}
	return ids
}

// --- CAL-01 ---------------------------------------------------------------

// xcGoogleRules is the matrix Google Calendar actually produces: the UI's
// presets, its custom builder, and the two UNTIL spellings (datetime for
// timed events, bare date for all-day ones). Every entry must parse under
// rrule-go, the engine's own RFC 5545 parser, via a schedule trigger's
// write-time compile.
var xcGoogleRules = []struct {
	name string
	rule string
}{
	{"daily", "RRULE:FREQ=DAILY"},
	{"every 2 days", "RRULE:FREQ=DAILY;INTERVAL=2"},
	{"every weekday", "RRULE:FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"},
	{"weekly on Tuesday", "RRULE:FREQ=WEEKLY;BYDAY=TU"},
	{"every 2 weeks on Tuesday and Thursday", "RRULE:FREQ=WEEKLY;INTERVAL=2;BYDAY=TU,TH"},
	{"monthly on the 2nd Tuesday", "RRULE:FREQ=MONTHLY;BYDAY=2TU"},
	{"monthly on the last Friday", "RRULE:FREQ=MONTHLY;BYDAY=-1FR"},
	{"monthly on day 15", "RRULE:FREQ=MONTHLY;BYMONTHDAY=15"},
	{"yearly", "RRULE:FREQ=YEARLY"},
	{"yearly on Aug 26", "RRULE:FREQ=YEARLY;BYMONTH=8;BYMONTHDAY=26"},
	{"ends after 10", "RRULE:FREQ=WEEKLY;BYDAY=WE;COUNT=10"},
	{"ends on a datetime", "RRULE:FREQ=DAILY;UNTIL=20270901T093000Z"},
	{"ends on a date (all-day form)", "RRULE:FREQ=DAILY;UNTIL=20270901"},
	{"week starts Sunday", "RRULE:FREQ=WEEKLY;WKST=SU;BYDAY=TU"},
	{"bare spelling, no prefix", "FREQ=WEEKLY;BYDAY=MO"},
}

func xcCaseRuleMatrix(c *C) {
	// Storage: every rule lands verbatim on a series and reads back.
	for i, tc := range xcGoogleRules {
		id := fmt.Sprintf("x-ser-matrix-%d", i)
		rec := xcSeries(c, id, "Matrix: "+tc.name, tc.rule, nil)
		c.requiref(rec.prop("recurrence") == tc.rule,
			"%s: stored %q, want the verbatim %q", tc.name, rec.prop("recurrence"), tc.rule)
	}
	c.stepf("all %d Google-shaped rules stored verbatim on calendareventseries records", len(xcGoogleRules))

	// Parseability: the engine's ONE real RRULE parser is the schedule
	// trigger's write-time compile (rrule-go), so admitting each rule there
	// proves the engine can actually expand it, not merely store it. The
	// probe trigger is disabled and deleted after; its callable must merely
	// resolve.
	probe := func(rule string) (int, string) {
		c.t.Helper()
		status, raw := c.do(http.MethodPut, triggerCollection+"/x-rrule-probe", map[string]any{
			"properties": map[string]any{
				"enabled":  false,
				"source":   map[string]any{"schedule": map[string]any{"recurrence": rule, "timezone": "Europe/London"}},
				"callable": "core.substrate.reamde.dev/function/" + storyAuthority + "/resolveattendees",
			},
		}, nil)
		return status, string(raw)
	}
	for _, tc := range xcGoogleRules {
		rule := strings.TrimPrefix(tc.rule, "RRULE:")
		status, raw := probe(rule)
		c.requiref(status == http.StatusOK || status == http.StatusCreated,
			"%s: the engine's RFC 5545 parser refused %q: %d %s", tc.name, rule, status, raw)
	}
	c.stepf("all %d rules compile under the engine's RFC 5545 parser (a schedule trigger's write-time gate)", len(xcGoogleRules))

	// The gates agree: a rule no parser accepts is refused on a series and on
	// a schedule alike, naming RFC 5545 and the parser's own reason.
	garbage := "FREQ=NONSENSE;BYDAY=99XX"
	badStatus, badRaw := c.do(http.MethodPut, seriesCollection+"/x-ser-garbage", map[string]any{
		"properties": map[string]any{"summary": "Matrix: garbage no parser accepts", "recurrence": garbage},
		"edges":      []edge{{Rel: "calendar", To: edgeTarget{ID: "work"}}},
	}, nil)
	c.requiref(badStatus == http.StatusUnprocessableEntity && strings.Contains(string(badRaw), "RFC 5545"),
		"a garbage rule answered %d on the series, want the 422 naming RFC 5545: %s", badStatus, badRaw)
	status, raw := probe(garbage)
	c.requiref(status == http.StatusUnprocessableEntity,
		"the schedule gate admitted a rule no parser accepts: %d %s", status, raw)
	c.stepf("`%s` is refused 422 on the series AND on a schedule: one parser guards both gates", garbage)

	// A rule that is not an RRULE at all is refused on the series too.
	notStatus, notRaw := c.do(http.MethodPut, seriesCollection+"/x-ser-notarule", map[string]any{
		"properties": map[string]any{"summary": "no rule", "recurrence": "every tuesday"},
		"edges":      []edge{{Rel: "calendar", To: edgeTarget{ID: "work"}}},
	}, nil)
	c.requiref(notStatus == http.StatusUnprocessableEntity && strings.Contains(string(notRaw), "RFC 5545"),
		"a non-RRULE string answered %d, want the 422 naming RFC 5545: %s", notStatus, notRaw)

	// Google's recurrence field is a LIST of lines. The rule string holds
	// exactly one RRULE: a joined RRULE+RDATE block is refused, because RDATE
	// extras have their own declared home now, the `rdates` datetime list
	// beside `exdates`, and the rule's anchor lives in `startsAt`.
	block := "RRULE:FREQ=WEEKLY;BYDAY=MO\nRDATE:20260915T093000Z"
	blockStatus, blockRaw := c.do(http.MethodPut, seriesCollection+"/x-ser-rdate", map[string]any{
		"properties": map[string]any{"summary": "Matrix: an RDATE block", "recurrence": block},
		"edges":      []edge{{Rel: "calendar", To: edgeTarget{ID: "work"}}},
	}, nil)
	c.requiref(blockStatus == http.StatusUnprocessableEntity,
		"a multi-line RRULE+RDATE block answered %d on the series, want 422: %s", blockStatus, blockRaw)
	withExtras := c.putRec(seriesCollection, "x-ser-extras", map[string]any{
		"summary":    "Matrix: rule plus RDATE extras and an anchor",
		"recurrence": "RRULE:FREQ=WEEKLY;BYDAY=MO",
		"rdates":     []string{"2026-09-15T09:30:00Z"},
		"exdates":    []string{"2026-09-21T09:30:00Z"},
		"startsAt":   "2026-08-31T09:30:00Z",
	}, []edge{{Rel: "calendar", To: edgeTarget{ID: "work"}}})
	rdates, _ := withExtras.Properties["rdates"].([]any)
	c.requiref(len(rdates) == 1 && withExtras.prop("startsAt") != "",
		"the declared homes did not round-trip: rdates %v, startsAt %q", rdates, withExtras.prop("startsAt"))
	c.stepf("a multi-line block is refused; RDATE extras live in `rdates`, skips in `exdates`, and the DTSTART anchor in `startsAt`, all round-tripping")

	// The probe trigger leaves with the case.
	status, _ = c.do(http.MethodDelete, triggerCollection+"/x-rrule-probe", nil, nil)
	c.requiref(status == http.StatusOK, "deleting the probe trigger answered %d", status)
}

// --- CAL-02 ---------------------------------------------------------------

func xcCaseWeekdayStandup(c *C) {
	r := c.r
	monday := xcMonday(r)

	// The definition: every weekday at 09:30 for a year, as Google emits it.
	until := monday.AddDate(1, 0, 0).Format("20060102T150405Z")
	xcSeries(c, "x-ser-standup", "Engineering standup",
		"RRULE:FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR;UNTIL="+until, nil)

	// The connector explodes three weeks: 15 weekday occurrences.
	ids := make([]string, 0, 15)
	for week := range 3 {
		for day := range 5 {
			at := monday.AddDate(0, 0, week*7+day)
			id := fmt.Sprintf("x-cal-standup-%s", at.Format("20060102"))
			xcOccurrence(c, id, "x-ser-standup", "Engineering standup", at, 15*time.Minute)
			ids = append(ids, id)
		}
	}
	c.stepf("exploded 3 weeks of the every-weekday series: 15 occurrences at 09:30, all edged to `x-ser-standup`")

	// Google's single-instance override: week 2 Wednesday moves 2 hours later
	// and gains a title, SAME instance id, everything else untouched.
	movedID := fmt.Sprintf("x-cal-standup-%s", monday.AddDate(0, 0, 9).Format("20060102"))
	movedAt := monday.AddDate(0, 0, 9).Add(2 * time.Hour)
	moved := c.putRec(eventCollection, movedID, map[string]any{
		"summary":         "Engineering standup (moved for the all-hands)",
		"at":              movedAt.Format(time.RFC3339),
		"endsAt":          movedAt.Add(15 * time.Minute).Format(time.RFC3339),
		"originalStartAt": monday.AddDate(0, 0, 9).Format(time.RFC3339),
	}, nil)
	c.requiref(sameIDs(edgeIDs(moved, "series"), "x-ser-standup"),
		"the override lost its series edge: %v (an update must merge, never prune)", edgeIDs(moved, "series"))
	c.requiref(moved.prop("originalStartAt") != "",
		"the override does not record the slot it replaced (originalStartAt)")
	c.stepf("overrode one instance in place: same id `%s`, new time +2h, new title, `originalStartAt` naming the slot it replaced; the series edge survived the update", movedID)

	// Google's cancellation: the week 3 Friday instance is retracted (a
	// canceled event is DELETED, never flagged) and the series gains the
	// exdate.
	canceledAt := monday.AddDate(0, 0, 18)
	canceledID := fmt.Sprintf("x-cal-standup-%s", canceledAt.Format("20060102"))
	status, raw := c.do(http.MethodDelete, eventCollection+"/"+canceledID, nil, nil)
	c.requiref(status == http.StatusOK, "retracting the canceled instance answered %d: %s", status, raw)
	series := c.putRec(seriesCollection, "x-ser-standup", map[string]any{
		"exdates": []string{canceledAt.Format(time.RFC3339)},
	}, nil)
	exdates, _ := series.Properties["exdates"].([]any)
	c.requiref(len(exdates) == 1, "the series carries %d exdates, want 1", len(exdates))
	c.stepf("canceled one instance Google-style: the occurrence `%s` is retracted and the series carries its exdate", canceledID)

	// The client's reads. Week 2 [Mon, Sat): five instances, the moved one at
	// its new time, ordered by at.
	week2 := xcWindowIDs(c, monday.AddDate(0, 0, 7), monday.AddDate(0, 0, 12))
	c.requiref(len(week2) == 5 && week2[2] == movedID,
		"week 2 window: %v (want 5 rows with the moved instance third by time)", week2)

	// The moved instance answers at its NEW hour and is gone from the old.
	oldSlot := xcWindowIDs(c, monday.AddDate(0, 0, 9), monday.AddDate(0, 0, 9).Add(time.Hour))
	newSlot := xcWindowIDs(c, movedAt, movedAt.Add(time.Hour))
	c.requiref(len(oldSlot) == 0 && len(newSlot) == 1 && newSlot[0] == movedID,
		"the override still answers at the old slot (%v) or not at the new one (%v)", oldSlot, newSlot)

	// The canceled day is empty live, and the tombstone shows under
	// deleted:true only.
	day := xcWindowIDs(c, canceledAt.Truncate(24*time.Hour), canceledAt.Truncate(24*time.Hour).AddDate(0, 0, 1))
	c.requiref(len(day) == 0, "the canceled day still answers: %v", day)
	filter := fmt.Sprintf(`{"deleted":true,"properties":{"at":{"gte":%q,"lt":%q}}}`,
		canceledAt.Truncate(24*time.Hour).Format(time.RFC3339),
		canceledAt.Truncate(24*time.Hour).AddDate(0, 0, 1).Format(time.RFC3339))
	var tomb struct {
		Records []record `json:"records"`
	}
	status, raw = c.do(http.MethodGet, eventCollection+"?filter="+url.QueryEscape(filter), nil, &tomb)
	c.requiref(status == http.StatusOK && len(tomb.Records) == 1 && tomb.Records[0].ID == canceledID,
		"deleted:true over the canceled day answered %d with %d rows: %s", status, len(tomb.Records), raw)

	// Whole horizon: 14 live occurrences (15 minus the cancellation), never
	// the series (a definition has no timeline instant to answer with). The
	// window is shared with other cases' events, so the count is over this
	// series' ids alone.
	standups := 0
	for _, id := range xcWindowIDs(c, monday.AddDate(0, 0, -1), monday.AddDate(0, 0, 21)) {
		if strings.HasPrefix(id, "x-cal-standup-") {
			standups++
		}
	}
	c.requiref(standups == 14, "the full window holds %d live standups, want 14", standups)
	c.stepf("window queries: week 2 has 5 (moved one at its new hour, third by time), the old slot is empty, the canceled day is empty live and one tombstone under deleted:true, the horizon holds 14")

	_ = ids
}

// --- CAL-03 ---------------------------------------------------------------

// xcNthWeekday computes the nth (1-based, or -1 for last) weekday of a month,
// which is this case's independent oracle for what Google would explode.
func xcNthWeekday(year int, month time.Month, weekday time.Weekday, n int) time.Time {
	if n > 0 {
		d := time.Date(year, month, 1, 13, 0, 0, 0, time.UTC)
		for d.Weekday() != weekday {
			d = d.AddDate(0, 0, 1)
		}
		return d.AddDate(0, 0, 7*(n-1))
	}
	d := time.Date(year, month, 1, 13, 0, 0, 0, time.UTC).AddDate(0, 1, -1)
	for d.Weekday() != weekday {
		d = d.AddDate(0, 0, -1)
	}
	return d
}

func xcCaseNthWeekday(c *C) {
	r := c.r
	start := xcMonday(r)

	xcSeries(c, "x-ser-2tu", "Platform review (2nd Tuesday)", "RRULE:FREQ=MONTHLY;BYDAY=2TU", nil)
	xcSeries(c, "x-ser-lastfri", "Retro (last Friday)", "RRULE:FREQ=MONTHLY;BYDAY=-1FR", nil)

	// Four months of each, materialized off the case's own oracle.
	type expect struct{ tu, fr string }
	months := make([]expect, 0, 4)
	for m := range 4 {
		anchor := start.AddDate(0, m, 0)
		tu := xcNthWeekday(anchor.Year(), anchor.Month(), time.Tuesday, 2)
		fr := xcNthWeekday(anchor.Year(), anchor.Month(), time.Friday, -1)
		tuID := "x-cal-2tu-" + tu.Format("20060102")
		frID := "x-cal-lastfri-" + fr.Format("20060102")
		xcOccurrence(c, tuID, "x-ser-2tu", "Platform review", tu, time.Hour)
		xcOccurrence(c, frID, "x-ser-lastfri", "Retro", fr, time.Hour)
		months = append(months, expect{tuID, frID})
	}
	c.stepf("exploded 4 months of `every 2nd Tuesday` and `last Friday`, dates computed by the case's own weekday oracle")

	// Each calendar month window returns exactly one of each, on its date.
	for m, want := range months {
		anchor := start.AddDate(0, m, 0)
		from := time.Date(anchor.Year(), anchor.Month(), 1, 0, 0, 0, 0, time.UTC)
		got := xcWindowIDs(c, from, from.AddDate(0, 1, 0))
		var tu, fr int
		for _, id := range got {
			if id == want.tu {
				tu++
			}
			if id == want.fr {
				fr++
			}
		}
		c.requiref(tu == 1 && fr == 1,
			"month %s window: %v misses the expected 2nd-Tuesday (%s) or last-Friday (%s) instance",
			anchor.Format("2006-01"), got, want.tu, want.fr)
	}
	c.stepf("each month window answers exactly one 2nd-Tuesday and one last-Friday instance, on the oracle's dates")

	// The definitions are never in a window: no temporal trait, no instant.
	var page struct {
		Records []record `json:"records"`
	}
	status, raw := c.do(http.MethodGet, seriesCollection+"?first=200", nil, &page)
	c.requiref(status == http.StatusOK, "listing series answered %d: %s", status, raw)
	for _, rec := range page.Records {
		c.requiref(rec.Properties["at"] == nil, "series %s carries a timeline instant %v", rec.ID, rec.Properties["at"])
	}

	// The back-reference a calendar UI walks: /incoming on the series, rel
	// `series`, is exactly its occurrences.
	var incoming struct {
		Total int `json:"total"`
	}
	status, _ = c.do(http.MethodGet, seriesCollection+"/x-ser-2tu/incoming?rel=series", nil, &incoming)
	c.requiref(status == http.StatusOK && incoming.Total == 4,
		"incoming on the 2nd-Tuesday series holds %d occurrences, want 4", incoming.Total)
	c.stepf("the series never sits in a time window, and /incoming on it lists exactly its 4 occurrences")
}

// --- CAL-04 ---------------------------------------------------------------

func xcCaseSeriesSplit(c *C) {
	r := c.r
	// A weekly Wednesday sync at 14:00, eight occurrences ahead.
	wednesday := xcMonday(r).AddDate(0, 0, 2).Add(4*time.Hour + 30*time.Minute) // 14:00
	xcSeries(c, "x-ser-sync-a", "Design sync", "RRULE:FREQ=WEEKLY;BYDAY=WE", nil)
	for week := range 8 {
		at := wednesday.AddDate(0, 0, 7*week)
		xcOccurrence(c, "x-cal-sync-"+at.Format("20060102"), "x-ser-sync-a", "Design sync", at, 30*time.Minute)
	}

	// "From this event on, 15:00": Google splits. The old master gains UNTIL
	// just before occurrence five; a NEW master carries the new time; the
	// connector retracts the old instances five to eight and explodes the new
	// ones. The instance ids change with the master, exactly as Google's
	// master_yyyymmdd ids do.
	splitAt := wednesday.AddDate(0, 0, 7*4)
	until := splitAt.Add(-time.Second).Format("20060102T150405Z")
	seriesA := c.putRec(seriesCollection, "x-ser-sync-a", map[string]any{
		"recurrence": "RRULE:FREQ=WEEKLY;BYDAY=WE;UNTIL=" + until,
	}, nil)
	c.requiref(strings.Contains(seriesA.prop("recurrence"), "UNTIL="+until),
		"the truncated series does not carry the UNTIL: %q", seriesA.prop("recurrence"))
	xcSeries(c, "x-ser-sync-b", "Design sync", "RRULE:FREQ=WEEKLY;BYDAY=WE", nil)
	for week := 4; week < 8; week++ {
		oldAt := wednesday.AddDate(0, 0, 7*week)
		status, raw := c.do(http.MethodDelete, eventCollection+"/x-cal-sync-"+oldAt.Format("20060102"), nil, nil)
		c.requiref(status == http.StatusOK, "retracting old instance week %d answered %d: %s", week, status, raw)
		newAt := oldAt.Add(time.Hour) // 15:00
		xcOccurrence(c, "x-cal-syncb-"+newAt.Format("20060102"), "x-ser-sync-b", "Design sync", newAt, 30*time.Minute)
	}
	c.stepf("split from occurrence five: series A truncated with UNTIL=%s, series B minted, four old instances retracted, four new ones at 15:00", until)

	// The whole window: eight live rows, 14:00 before the boundary edged to
	// A, 15:00 after it edged to B, in one ordered read.
	all := xcWindow(c, wednesday.AddDate(0, 0, -1), wednesday.AddDate(0, 0, 7*8))
	var sync []record
	for _, rec := range all {
		if strings.HasPrefix(rec.ID, "x-cal-sync") {
			sync = append(sync, rec)
		}
	}
	c.requiref(len(sync) == 8, "the split series answers %d live occurrences, want 8", len(sync))
	for i, rec := range sync {
		at, err := time.Parse(time.RFC3339, rec.prop("at"))
		c.requiref(err == nil, "occurrence %s carries an unparsable at: %q", rec.ID, rec.prop("at"))
		wantAt, wantSeries := wednesday.AddDate(0, 0, 7*i), "x-ser-sync-a"
		if i >= 4 {
			wantAt, wantSeries = wantAt.Add(time.Hour), "x-ser-sync-b"
		}
		c.requiref(at.Equal(wantAt),
			"occurrence %d (%s) sits at %s, want %s (the hour must move exactly at the split)", i, rec.ID, at, wantAt)
		c.requiref(sameIDs(edgeIDs(rec, "series"), wantSeries),
			"occurrence %d (%s) is edged to %v, want %s", i, rec.ID, edgeIDs(rec, "series"), wantSeries)
	}
	c.stepf("one ordered window shows the boundary: four at the old hour on series A, then four an hour later on series B")

	// Each series' incoming holds exactly its half.
	for _, tc := range []struct {
		id   string
		want int
	}{{"x-ser-sync-a", 4}, {"x-ser-sync-b", 4}} {
		var incoming struct {
			Total int `json:"total"`
		}
		status, _ := c.do(http.MethodGet, seriesCollection+"/"+tc.id+"/incoming?rel=series", nil, &incoming)
		c.requiref(status == http.StatusOK && incoming.Total == tc.want,
			"incoming on %s holds %d, want %d", tc.id, incoming.Total, tc.want)
	}
	c.stepf("each half of the split accounts for exactly its four occurrences through /incoming")
}

// --- OCC-01 ----------------------------------------------------------------

const (
	medCollection         = "/api/v1/health.substrate.reamde.dev/medication"
	medScheduleCollection = "/api/v1/health.substrate.reamde.dev/medicationschedule"
	medScheduleKind       = "health.substrate.reamde.dev/medicationschedule"
	medLogCollection      = "/api/v1/health.substrate.reamde.dev/medicationschedulelog"
)

// xoList mirrors substrate.OccurrenceList, the computed half of an agenda.
type xoList struct {
	Occurrences []struct {
		Kind  string `json:"kind"`
		ID    string `json:"id"`
		Title string `json:"title"`
		At    string `json:"at"`
		Log   *struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"log"`
	} `json:"occurrences"`
	Truncated bool `json:"truncated"`
	Problems  []struct {
		Kind    string `json:"kind"`
		ID      string `json:"id"`
		Message string `json:"message"`
	} `json:"problems"`
}

// xoRead is the agenda's computed half: what the stored rules name in the
// window, which no row query can answer.
func xoRead(c *C, from, to time.Time) xoList {
	c.t.Helper()
	var out xoList
	status, raw := c.do(http.MethodGet, "/api/v1/occurrences?from="+
		url.QueryEscape(from.Format(time.RFC3339))+"&to="+
		url.QueryEscape(to.Format(time.RFC3339)), nil, &out)
	c.requiref(status == http.StatusOK, "the occurrences read answered %d: %s", status, raw)
	c.requiref(!out.Truncated, "the window is a few weeks; nothing may truncate")
	c.requiref(len(out.Problems) == 0,
		"every stored rule must expand cleanly, got problems %v", out.Problems)
	return out
}

// xoDoses filters one schedule's slots out of the computed answer, in order.
func xoDoses(list xoList, id string) []string {
	var ats []string
	for _, o := range list.Occurrences {
		if o.Kind == medScheduleKind && o.ID == id {
			ats = append(ats, o.At)
		}
	}
	return ats
}

func xoCaseMedicationWeek(c *C) {
	c.xvInstall("health.substrate.reamde.dev/health")
	base := xcMonday(c.r).Truncate(24 * time.Hour) // the block's Monday, 00:00Z
	dose := base.Add(6 * time.Hour)                // 09:00 Athens in summer
	week2, week3, week4 := base.AddDate(0, 0, 7), base.AddDate(0, 0, 14), base.AddDate(0, 0, 21)

	c.putRec(medCollection, "levothyroxine", map[string]any{"name": "Levothyroxine"}, nil)
	c.putRec(medScheduleCollection, "levothyroxine-daily", map[string]any{
		"doseAmount": 1, "doseUnit": "tablet",
		"recurrence": "RRULE:FREQ=DAILY",
		"timezone":   "Europe/Athens",
		"at":         dose.Format(time.RFC3339), // the anchor: temporal(range)'s own start
	}, []edge{{Rel: "medication", To: edgeTarget{ID: "levothyroxine"}}})
	c.stepf("one schedule record holds the forever-daily rule, anchored %s", dose.Format(time.RFC3339))

	// The agenda is two reads and a merge: rows in the window (the calendar
	// events a connector materialized), and the computed occurrences beside
	// them. A concrete event proves the row half answers the same window.
	xcOccurrence(c, "x-occ-review", "x-ser-standup", "Quarterly review",
		base.AddDate(0, 0, 1).Add(15*time.Hour), time.Hour)
	rows := xcWindow(c, base, week2)
	found := false
	for _, rec := range rows {
		found = found || rec.ID == "x-occ-review"
	}
	c.requiref(found, "the row half of the agenda lost the concrete event: %v", rows)

	occs := xoRead(c, base, week2)
	ats := xoDoses(occs, "levothyroxine-daily")
	c.requiref(len(ats) == 7, "a daily rule names 7 instants in a week, got %v", ats)
	for i, at := range ats {
		want := dose.AddDate(0, 0, i).Format(time.RFC3339)
		c.requiref(at == want, "dose %d computed at %s, want %s", i, at, want)
	}
	for _, o := range occs.Occurrences {
		c.requiref(o.Kind != seriesKind,
			"series %s leaked into the computed answer: its rows are the truth inside its stamp", o.ID)
	}
	c.stepf("the week answers 7 computed doses beside the calendar's rows, and no stamped series leaks a twin")

	// A taken dose is an occurrencelog; it annotates the slot, never hides it.
	tue := dose.AddDate(0, 0, 1)
	c.putRec(medLogCollection, "x-occ-dose-tue", map[string]any{
		"at":          tue.Add(20 * time.Minute).Format(time.RFC3339),
		"scheduledAt": tue.Format(time.RFC3339),
	}, []edge{{Rel: "schedule", To: edgeTarget{ID: "levothyroxine-daily"}}})
	occs = xoRead(c, base, week2)
	marked := 0
	for _, o := range occs.Occurrences {
		if o.ID != "levothyroxine-daily" {
			continue
		}
		if o.At == tue.Format(time.RFC3339) {
			c.requiref(o.Log != nil && o.Log.ID == "x-occ-dose-tue" && o.Log.Status == "done",
				"Tuesday's slot must carry its log, got %+v", o.Log)
			marked++
		} else {
			c.requiref(o.Log == nil, "an unlogged slot at %s grew a log", o.At)
		}
	}
	c.requiref(marked == 1, "exactly one slot is logged, got %d", marked)
	c.stepf("Tuesday's dose reads done; the other six stay bare, and absence still means missed")

	// The travel week: home slots out via exdates, the moved instants in via
	// rdates — 09:00 America/New_York written as instants, exactly how a
	// Google override lands.
	exdates, rdates := []string{}, []string{}
	for i := range 7 {
		exdates = append(exdates, dose.AddDate(0, 0, 7+i).Format(time.RFC3339))
		rdates = append(rdates, base.AddDate(0, 0, 7+i).Add(13*time.Hour).Format(time.RFC3339))
	}
	status, raw := c.do(http.MethodPatch, medScheduleCollection+"/levothyroxine-daily",
		map[string]any{"properties": map[string]any{"exdates": exdates, "rdates": rdates}}, nil)
	c.requiref(status == http.StatusOK, "the travel-week override answered %d: %s", status, raw)

	moved := xoDoses(xoRead(c, week2, week3), "levothyroxine-daily")
	c.requiref(len(moved) == 7, "the travel week still doses daily, got %v", moved)
	for i, at := range moved {
		want := base.AddDate(0, 0, 7+i).Add(13 * time.Hour).Format(time.RFC3339)
		c.requiref(at == want, "travel dose %d computed at %s, want %s (the moved slot)", i, at, want)
	}
	home := xoDoses(xoRead(c, week3, week4), "levothyroxine-daily")
	c.requiref(len(home) == 7 && home[0] == dose.AddDate(0, 0, 14).Format(time.RFC3339),
		"the week after the trip must dose at home time again, got %v", home)
	c.stepf("the travel week reads 7 moved doses and week three is home time again — the rule itself never changed")
}
