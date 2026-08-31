package occurrence_test

import (
	"errors"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/occurrence"
)

func mustExpand(t *testing.T, r occurrence.Rule, from, to time.Time, cap int) occurrence.Expansion {
	t.Helper()
	exp, err := occurrence.Expand(r, from, to, cap)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	return exp
}

func wantTimes(t *testing.T, got []time.Time, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d occurrences %v, want %d", len(got), got, len(want))
	}
	for i, w := range want {
		exp, err := time.Parse(time.RFC3339, w)
		if err != nil {
			t.Fatalf("bad want %q: %v", w, err)
		}
		if !got[i].Equal(exp) {
			t.Fatalf("occurrence %d = %s, want %s", i, got[i].Format(time.RFC3339), w)
		}
		if got[i].Location() != time.UTC {
			t.Fatalf("occurrence %d is in %s, want UTC", i, got[i].Location())
		}
	}
}

func utc(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// A daily rule keeps its wall clock in its zone: 09:00 Athens is 06:00Z in
// summer, and the RRULE: prefix is accepted the way validation accepts it.
func TestDailyKeepsWallClock(t *testing.T) {
	exp := mustExpand(t, occurrence.Rule{
		Recurrence: "RRULE:FREQ=DAILY",
		StartsAt:   utc("2026-07-06T06:00:00Z"),
		Timezone:   "Europe/Athens",
	}, utc("2026-07-06T00:00:00Z"), utc("2026-07-09T00:00:00Z"), 0)
	wantTimes(t, exp.Times,
		"2026-07-06T06:00:00Z", "2026-07-07T06:00:00Z", "2026-07-08T06:00:00Z")
}

// The US springs forward on 2026-03-08: a daily 09:00 New York rule moves
// from 14:00Z to 13:00Z and never drifts to 08:00 or 10:00 local.
func TestDailyAcrossDSTBoundary(t *testing.T) {
	exp := mustExpand(t, occurrence.Rule{
		Recurrence: "FREQ=DAILY",
		StartsAt:   utc("2026-03-06T14:00:00Z"), // 09:00 EST
		Timezone:   "America/New_York",
	}, utc("2026-03-06T00:00:00Z"), utc("2026-03-11T00:00:00Z"), 0)
	wantTimes(t, exp.Times,
		"2026-03-06T14:00:00Z", "2026-03-07T14:00:00Z",
		"2026-03-08T13:00:00Z", "2026-03-09T13:00:00Z", "2026-03-10T13:00:00Z")
}

func TestSecondTuesdayOfTheMonth(t *testing.T) {
	exp := mustExpand(t, occurrence.Rule{
		Recurrence: "FREQ=MONTHLY;BYDAY=2TU",
		StartsAt:   utc("2026-09-08T10:00:00Z"),
	}, utc("2026-09-01T00:00:00Z"), utc("2026-12-01T00:00:00Z"), 0)
	wantTimes(t, exp.Times,
		"2026-09-08T10:00:00Z", "2026-10-13T10:00:00Z", "2026-11-10T10:00:00Z")
}

func TestEveryWeekday(t *testing.T) {
	exp := mustExpand(t, occurrence.Rule{
		Recurrence: "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR",
		StartsAt:   utc("2026-07-06T08:00:00Z"),
	}, utc("2026-07-06T00:00:00Z"), utc("2026-07-13T00:00:00Z"), 0)
	wantTimes(t, exp.Times,
		"2026-07-06T08:00:00Z", "2026-07-07T08:00:00Z", "2026-07-08T08:00:00Z",
		"2026-07-09T08:00:00Z", "2026-07-10T08:00:00Z")
}

// UNTIL and COUNT close a rule inside the window rather than at its edge.
func TestUntilAndCountClose(t *testing.T) {
	exp := mustExpand(t, occurrence.Rule{
		Recurrence: "FREQ=DAILY;UNTIL=20260708T060000Z",
		StartsAt:   utc("2026-07-06T06:00:00Z"),
	}, utc("2026-07-01T00:00:00Z"), utc("2026-08-01T00:00:00Z"), 0)
	wantTimes(t, exp.Times,
		"2026-07-06T06:00:00Z", "2026-07-07T06:00:00Z", "2026-07-08T06:00:00Z")

	exp = mustExpand(t, occurrence.Rule{
		Recurrence: "FREQ=DAILY;COUNT=2",
		StartsAt:   utc("2026-07-06T06:00:00Z"),
	}, utc("2026-07-01T00:00:00Z"), utc("2026-08-01T00:00:00Z"), 0)
	wantTimes(t, exp.Times, "2026-07-06T06:00:00Z", "2026-07-07T06:00:00Z")
}

// exdates subtract after the rule, rdates add beside it, an exdate silences
// an rdate too, and an rdate that duplicates a rule slot stays one slot.
func TestExdatesAndRdates(t *testing.T) {
	exp := mustExpand(t, occurrence.Rule{
		Recurrence: "FREQ=DAILY",
		StartsAt:   utc("2026-07-06T06:00:00Z"),
		ExDates:    []time.Time{utc("2026-07-07T06:00:00Z"), utc("2026-07-09T18:00:00Z")},
		RDates: []time.Time{
			utc("2026-07-07T20:00:00Z"), // the moved slot
			utc("2026-07-09T18:00:00Z"), // silenced by the exdate
			utc("2026-07-08T06:00:00Z"), // duplicates a rule slot
		},
	}, utc("2026-07-06T00:00:00Z"), utc("2026-07-09T00:00:00Z"), 0)
	wantTimes(t, exp.Times,
		"2026-07-06T06:00:00Z", "2026-07-07T20:00:00Z", "2026-07-08T06:00:00Z")
}

// A schedule may be pure rdates: no rule, no anchor, still occurrences.
func TestRdatesWithoutARule(t *testing.T) {
	exp := mustExpand(t, occurrence.Rule{
		RDates: []time.Time{utc("2026-07-07T20:00:00Z"), utc("2026-07-06T09:00:00Z")},
	}, utc("2026-07-06T00:00:00Z"), utc("2026-07-09T00:00:00Z"), 0)
	wantTimes(t, exp.Times, "2026-07-06T09:00:00Z", "2026-07-07T20:00:00Z")
}

// Inside [materializedFrom, materializedUntil) the rows are the truth and the
// expander stays silent; until is exclusive, Google's timeMax convention.
func TestMaterializedSpanSuppresses(t *testing.T) {
	r := occurrence.Rule{
		Recurrence:        "FREQ=DAILY",
		StartsAt:          utc("2026-07-01T06:00:00Z"),
		MaterializedFrom:  utc("2026-07-03T00:00:00Z"),
		MaterializedUntil: utc("2026-07-05T06:00:00Z"),
	}
	exp := mustExpand(t, r, utc("2026-07-01T00:00:00Z"), utc("2026-07-07T00:00:00Z"), 0)
	wantTimes(t, exp.Times,
		"2026-07-01T06:00:00Z", "2026-07-02T06:00:00Z", // before the span
		"2026-07-05T06:00:00Z", "2026-07-06T06:00:00Z") // at and after until

	// A zero from leaves the span open at the past end.
	r.MaterializedFrom = time.Time{}
	exp = mustExpand(t, r, utc("2026-07-01T00:00:00Z"), utc("2026-07-07T00:00:00Z"), 0)
	wantTimes(t, exp.Times, "2026-07-05T06:00:00Z", "2026-07-06T06:00:00Z")
}

// The window is [from, to): a slot at from is in, a slot at to is out.
func TestWindowIsHalfOpen(t *testing.T) {
	exp := mustExpand(t, occurrence.Rule{
		Recurrence: "FREQ=DAILY",
		StartsAt:   utc("2026-07-06T06:00:00Z"),
	}, utc("2026-07-06T06:00:00Z"), utc("2026-07-08T06:00:00Z"), 0)
	wantTimes(t, exp.Times, "2026-07-06T06:00:00Z", "2026-07-07T06:00:00Z")
}

func TestSlotCapTruncates(t *testing.T) {
	exp := mustExpand(t, occurrence.Rule{
		Recurrence: "FREQ=DAILY",
		StartsAt:   utc("2026-07-06T06:00:00Z"),
	}, utc("2026-07-06T00:00:00Z"), utc("2026-08-06T00:00:00Z"), 3)
	if !exp.Truncated {
		t.Fatal("cap 3 over a month of daily slots must report Truncated")
	}
	wantTimes(t, exp.Times,
		"2026-07-06T06:00:00Z", "2026-07-07T06:00:00Z", "2026-07-08T06:00:00Z")
}

func TestRefusals(t *testing.T) {
	from, to := utc("2026-07-06T00:00:00Z"), utc("2026-08-06T00:00:00Z")
	if _, err := occurrence.Expand(occurrence.Rule{Recurrence: "FREQ=DAILY", StartsAt: utc("2026-07-06T06:00:00Z")}, to, from, 0); err == nil {
		t.Fatal("an empty window must refuse")
	}
	if _, err := occurrence.Expand(occurrence.Rule{Recurrence: "FREQ=DAILY", StartsAt: utc("2026-07-06T06:00:00Z"), Timezone: "Mars/Olympus"}, from, to, 0); err == nil {
		t.Fatal("an unknown timezone must refuse")
	}
	if _, err := occurrence.Expand(occurrence.Rule{Recurrence: "FREQ=DAILY"}, from, to, 0); err == nil {
		t.Fatal("a rule without an anchor must refuse")
	}
	if _, err := occurrence.Expand(occurrence.Rule{Recurrence: "FREQ=NONSENSE", StartsAt: from}, from, to, 0); err == nil {
		t.Fatal("a malformed rule must refuse")
	}
	_, err := occurrence.Expand(occurrence.Rule{Recurrence: "FREQ=SECONDLY", StartsAt: from}, from, to, 0)
	if !errors.Is(err, occurrence.ErrTooDense) {
		t.Fatalf("a second-by-second rule over a month must blow the budget, got %v", err)
	}
}
