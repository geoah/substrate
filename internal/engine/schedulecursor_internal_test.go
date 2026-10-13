package engine

import (
	"context"
	"math/rand"
	"testing"
	"time"

	// The zoned rules below must not depend on the host's zoneinfo.
	_ "time/tzdata"
)

// freshDueFires is the walk a schedule cursor replaces: the rule walked from
// its anchor on every call. It is the oracle the cursor is held to.
func freshDueFires(t *testing.T, s *scheduleSource, createdAt, after, now time.Time, limit int) []time.Time {
	t.Helper()
	r, err := s.rule(createdAt)
	if err != nil {
		t.Fatal(err)
	}
	next := r.Iterator()
	var out []time.Time
	for len(out) < limit {
		at, ok := next()
		if !ok || at.After(now) {
			break
		}
		if !at.After(after) {
			continue
		}
		out = append(out, at.UTC())
	}
	return out
}

// scheduleCursorRules are the rules the cursor is held to a fresh walk on:
// every frequency the shipped triggers use, the BYxxx parts, a COUNT that
// runs out mid-test, and two zoned rules whose walk crosses DST changes.
// step bounds how far each step moves the clock, sized so the walk covers
// several periods of the rule without the oracle's walk from the anchor
// costing seconds.
var scheduleCursorRules = []struct {
	recurrence, timezone string
	step                 time.Duration
	steps                int
}{
	{"FREQ=MINUTELY", "", 5 * time.Minute, 600},
	{"FREQ=MINUTELY;INTERVAL=15", "", time.Hour, 1500},
	{"FREQ=HOURLY;BYMINUTE=5,35", "Europe/London", 6 * time.Hour, 1500},
	{"FREQ=DAILY;BYHOUR=9;BYMINUTE=0", "Europe/London", 3 * 24 * time.Hour, 1500},
	{"FREQ=WEEKLY;BYDAY=MO,WE;BYHOUR=8", "America/New_York", 10 * 24 * time.Hour, 1000},
	{"FREQ=MINUTELY;COUNT=500", "", 5 * time.Minute, 600},
}

// A cursor answers exactly what a walk from the anchor answers, over
// randomized passes: the clock moves on, a pass fires some of what is due
// and moves the fire state to one of them, a status read asks with an older
// fire state, a restore moves the fire state back, and an edit changes the
// rule. Every answer is compared, every step.
func TestScheduleCursorMatchesAFreshWalk(t *testing.T) {
	t.Parallel()
	for _, rule := range scheduleCursorRules {
		t.Run(rule.recurrence, func(t *testing.T) {
			t.Parallel()
			rng := rand.New(rand.NewSource(int64(len(rule.recurrence)) + 874))
			anchor := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			src := &scheduleSource{Recurrence: rule.recurrence, Timezone: rule.timezone}
			ds := &dataset{}
			after, now := anchor.Add(-time.Hour), anchor
			var seen []time.Time
			check := func(step int, what string, after time.Time, limit int) []time.Time {
				t.Helper()
				want := freshDueFires(t, src, anchor, after, now, limit)
				got, err := ds.dueFires("t", src, anchor, after, now, limit)
				if err != nil {
					t.Fatalf("step %d (%s): %v", step, what, err)
				}
				if len(got) != len(want) {
					t.Fatalf("step %d (%s): after %s now %s limit %d: %d fires, want %d",
						step, what, after, now, limit, len(got), len(want))
				}
				for i := range got {
					if !got[i].Equal(want[i]) || got[i].Location() != time.UTC {
						t.Fatalf("step %d (%s): fire %d is %s, want %s", step, what, i, got[i], want[i])
					}
				}
				return got
			}
			for step := range rule.steps {
				now = now.Add(time.Duration(rng.Int63n(int64(rule.step))))
				switch roll := rng.Intn(100); {
				case roll < 2:
					// An edit of the trigger: the rule changes and changes
					// back.
					src.Recurrence = rule.recurrence + ";INTERVAL=2"
					check(step, "edit", after, 1+rng.Intn(100))
					src.Recurrence = rule.recurrence
				case roll < 7 && len(seen) > 0:
					// A restore: the fire state is back at an earlier fire.
					after = seen[rng.Intn(len(seen))]
					check(step, "restore", after, 1+rng.Intn(100))
				case roll < 15 && len(seen) > 0:
					// A status read that read the fire state before the
					// pass moved it.
					check(step, "stale read", seen[rng.Intn(len(seen))], scheduleOwedCap)
				case roll < 25 && len(seen) > 0:
					// A look of the pass asks whether anything is due, with
					// the fire state the pass began with and with the one
					// the row holds now.
					for _, at := range []time.Time{seen[rng.Intn(len(seen))], after} {
						owes, err := ds.owesAfter("t", src, anchor, at, now)
						if err != nil {
							t.Fatal(err)
						}
						if want := len(freshDueFires(t, src, anchor, at, now, 1)) > 0; owes != want {
							t.Fatalf("step %d: owes after %s at %s = %v, want %v", step, at, now, owes, want)
						}
					}
				}
				got := check(step, "pass", after, 1+rng.Intn(100))
				if len(got) > 0 && rng.Intn(4) > 0 {
					after = got[rng.Intn(len(got))]
					seen = append(seen, after)
				}
			}
		})
	}
}

// The second walk resumes where the first stopped: a year-old minutely rule
// asked again one minute later steps the iterator a handful of times, not
// half a million. A fire state behind the cursor walks again from the
// anchor and still answers right.
func TestScheduleCursorResumesFromTheFireState(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 13, 12, 0, 30, 0, time.UTC)
	anchor := now.Add(-365 * 24 * time.Hour)
	src := &scheduleSource{Recurrence: "FREQ=MINUTELY"}
	ds := &dataset{}
	after := now.Add(-90 * time.Second)
	if _, err := ds.dueFires("minutely", src, anchor, after, now, scheduleOwedCap); err != nil {
		t.Fatal(err)
	}
	c := ds.scheduleCursorOf("minutely")
	steps := 0
	inner := c.next
	c.next = func() (time.Time, bool) {
		steps++
		return inner()
	}

	after, now = after.Add(time.Minute), now.Add(time.Minute)
	got, err := ds.dueFires("minutely", src, anchor, after, now, scheduleOwedCap)
	if err != nil {
		t.Fatal(err)
	}
	if want := freshDueFires(t, src, anchor, after, now, scheduleOwedCap); len(got) != len(want) || !got[0].Equal(want[0]) {
		t.Fatalf("the resumed walk answered %v, want %v", got, want)
	}
	if steps == 0 || steps > 3 {
		t.Fatalf("the resumed walk stepped the iterator %d times, want 1 to 3: it walked from the anchor", steps)
	}

	// A look with the fire state the pass began with, which the cursor has
	// stepped past, is owed something and walks nothing again: the
	// iterator stepped next is still the one the first walk built.
	if owes, err := ds.owesAfter("minutely", src, anchor, after.Add(-time.Hour), now); err != nil || !owes {
		t.Fatalf("owes after an hour back = %v (%v), want true", owes, err)
	}
	steps = 0
	after, now = after.Add(time.Minute), now.Add(time.Minute)
	if _, err := ds.dueFires("minutely", src, anchor, after, now, scheduleOwedCap); err != nil {
		t.Fatal(err)
	}
	if steps == 0 {
		t.Fatal("owesAfter with an older fire state walked again from the anchor")
	}

	steps = 0
	back := anchor.Add(30 * 24 * time.Hour)
	got, err = ds.dueFires("minutely", src, anchor, back, now, 5)
	if err != nil {
		t.Fatal(err)
	}
	if steps != 0 {
		t.Fatalf("a fire state behind the cursor stepped the old iterator %d times, want a new walk from the anchor", steps)
	}
	want := freshDueFires(t, src, anchor, back, now, 5)
	for i := range want {
		if i >= len(got) || !got[i].Equal(want[i]) {
			t.Fatalf("after a restore the walk answered %v, want %v", got, want)
		}
	}
}

// Both readers of a schedule's due occurrences go through the trigger's
// cursor: the schedule lane's fire (processScheduleTrigger) and the status
// read's count (owedFires).
func TestScheduleReadersWalkTheTriggersCursor(t *testing.T) {
	t.Parallel()
	anchor := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	src := &scheduleSource{Recurrence: "FREQ=HOURLY", StartsAt: &anchor}
	lt := loadedTrigger{trigger: &trigger{ID: "hourly", Enabled: true, Schedule: src}, CreatedAt: anchor}
	walked := func(ds *dataset) bool {
		ds.scheduleCursorsMu.Lock()
		defer ds.scheduleCursorsMu.Unlock()
		c := ds.scheduleCursors[lt.ID]
		return c != nil && c.next != nil
	}

	ds := &dataset{}
	if _, _, err := ds.owedFires(lt, nowUTC().Add(-3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if !walked(ds) {
		t.Fatal("owedFires walked the rule without the trigger's cursor")
	}

	// A pass that read a fire state at now owes nothing, so the fire reads
	// no row and runs no delivery.
	ds = &dataset{}
	pass := &passStart{fired: map[string]time.Time{lt.ID: nowUTC()}}
	if _, fired, err := ds.processScheduleTrigger(context.Background(), lt, passDeadline{}, 10, pass); err != nil || fired != 0 {
		t.Fatalf("a fire state at now fired %d (%v), want none", fired, err)
	}
	if !walked(ds) {
		t.Fatal("processScheduleTrigger walked the rule without the trigger's cursor")
	}
}

// A pass drops the cursor of a trigger that is gone or no longer carries a
// schedule source, and keeps the others'.
func TestPruneActivityDropsTheCursorsOfRemovedTriggers(t *testing.T) {
	t.Parallel()
	anchor := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	src := &scheduleSource{Recurrence: "FREQ=HOURLY", StartsAt: &anchor}
	ds := &dataset{}
	for _, id := range []string{"kept", "gone", "now-a-record"} {
		if _, err := ds.dueFires(id, src, anchor, anchor, anchor.Add(time.Hour), 1); err != nil {
			t.Fatal(err)
		}
	}
	ds.pruneActivity([]loadedTrigger{
		{trigger: &trigger{ID: "kept", Schedule: src}},
		{trigger: &trigger{ID: "now-a-record", Record: &recordSource{}}},
	})
	ds.scheduleCursorsMu.Lock()
	defer ds.scheduleCursorsMu.Unlock()
	if len(ds.scheduleCursors) != 1 || ds.scheduleCursors["kept"] == nil {
		t.Fatalf("cursors after the prune: %v, want only kept's", ds.scheduleCursors)
	}
}
