package providertest

// The acceptance test the plan names for the Google mirror: from one
// calendar's rows alone — no call to Google — an iCalendar text is
// regenerated, one VEVENT per plain event, one per series with its verbatim
// lines and its EXDATEs from `exdates`, one per exception with RECURRENCE-ID
// = `originalAt`, and it is the calendar the fake served. Fixture in, same
// calendar out: the mirror is a copy of the calendar, not a reading of it.

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

func TestGoogleCalendarMirrorRegeneratesTheCalendar(t *testing.T) {
	t.Parallel()
	fake, ds := newCalendarFixture(t)
	newStepper(t, ds, googleCalendarFn, calStepConfig(calStepProps(nil))).
		drainApplying(nil)

	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{
			Kinds:      []string{googleSeriesType, googleEventType},
			Properties: map[string]substrate.Cond{"calendarId": {Eq: "primary@example.com"}},
		},
		First: 500,
	})
	if err != nil {
		t.Fatalf("list the calendar's rows: %v", err)
	}
	if page.Cursor != "" {
		t.Fatalf("the fixture's rows did not fit one page")
	}

	// What the fake served, spelled as the calendar it is. The singles are
	// dated relative to now, so their instants are read back off the pages.
	e1 := fake.pages[0][0].(map[string]any)
	e2 := fake.pages[1][1].(map[string]any)
	want := strings.Join([]string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//substrate//google mirror//EN",
		"BEGIN:VEVENT",
		"UID:e1@google.com",
		"DTSTART:" + basicOf(e1, "start"),
		"DTEND:" + basicOf(e1, "end"),
		"SUMMARY:Standup",
		"END:VEVENT",
		"BEGIN:VEVENT",
		"UID:e2@google.com",
		"DTSTART:" + basicOf(e2, "start"),
		"DTEND:" + basicOf(e2, "end"),
		"SUMMARY:Design review",
		"END:VEVENT",
		"BEGIN:VEVENT",
		"UID:master-1@google.com",
		"DTSTART:" + basic(masterAtUTC),
		"DTEND:20260715T130000Z",
		masterLines[0],
		masterLines[2],
		"EXDATE:" + basic(canceledSlotUTC),
		"EXDATE:" + basic(exdateUTC),
		"SUMMARY:Weekly sync",
		"END:VEVENT",
		"BEGIN:VEVENT",
		"UID:master-1@google.com",
		"RECURRENCE-ID:" + movedSlot,
		"DTSTART:20260722T140000Z",
		"DTEND:20260722T150000Z",
		"SUMMARY:Weekly sync (moved)",
		"END:VEVENT",
		"END:VCALENDAR",
	}, "\n") + "\n"

	if got := icalendar(page.Records); got != want {
		t.Fatalf("the mirror does not regenerate the calendar the fake served\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

// icalendar emits one calendar's mirror rows as iCalendar text. A series is
// its verbatim recurrence lines minus the EXDATE ones, which are re-emitted
// from `exdates` so a canceled slot appears beside the lines' own; an
// exception carries RECURRENCE-ID; every instant is UTC basic format. The
// order is by UID, then RECURRENCE-ID (the master first), then DTSTART, so
// the text is a function of the rows and nothing else.
func icalendar(rows []*substrate.Record) string {
	type vevent struct {
		uid, recurrenceID, dtstart string
		lines                      []string
	}
	var events []vevent
	for _, r := range rows {
		uid, _ := r.Properties["icalUID"].(string)
		v := vevent{uid: uid, dtstart: basic(instantOf(r.At))}
		v.lines = append(v.lines, "UID:"+uid)
		if original, ok := r.Properties["originalAt"].(string); ok && original != "" {
			v.recurrenceID = basic(original)
			v.lines = append(v.lines, "RECURRENCE-ID:"+v.recurrenceID)
		}
		v.lines = append(v.lines, "DTSTART:"+v.dtstart, "DTEND:"+basic(instantOf(r.EndsAt)))
		if r.Kind == googleSeriesType {
			for _, line := range gcalStrings(r.Properties["recurrenceLines"]) {
				if strings.HasPrefix(strings.ToUpper(line), "EXDATE") {
					continue
				}
				v.lines = append(v.lines, line)
			}
			for _, x := range gcalStrings(r.Properties["exdates"]) {
				v.lines = append(v.lines, "EXDATE:"+basic(x))
			}
		}
		if s, _ := r.Properties["summary"].(string); s != "" {
			v.lines = append(v.lines, "SUMMARY:"+s)
		}
		events = append(events, v)
	}
	sort.Slice(events, func(i, j int) bool {
		a, b := events[i], events[j]
		if a.uid != b.uid {
			return a.uid < b.uid
		}
		if a.recurrenceID != b.recurrenceID {
			return a.recurrenceID < b.recurrenceID
		}
		return a.dtstart < b.dtstart
	})
	var sb strings.Builder
	sb.WriteString("BEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//substrate//google mirror//EN\n")
	for _, v := range events {
		sb.WriteString("BEGIN:VEVENT\n")
		for _, line := range v.lines {
			sb.WriteString(line)
			sb.WriteString("\n")
		}
		sb.WriteString("END:VEVENT\n")
	}
	sb.WriteString("END:VCALENDAR\n")
	return sb.String()
}

// basic spells an RFC 3339 UTC instant in iCalendar's basic UTC form.
func basic(rfc3339 string) string {
	return strings.NewReplacer("-", "", ":", "").Replace(rfc3339)
}

// basicOf reads one of a fixture item's instants (its `start` or `end`) in
// basic UTC form.
func basicOf(item map[string]any, key string) string {
	value, _ := item[key].(map[string]any)
	s, _ := value["dateTime"].(string)
	return basic(s)
}
