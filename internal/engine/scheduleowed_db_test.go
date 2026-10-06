package engine

// A schedule trigger's parked fires never hold back its next due one: every
// pass dispatches what is due, whatever the trigger has parked. And a due
// occurrence is never invisible: a status read counts it as pending until a
// pass reaches it and as in flight while the pass delivers it, so a fire
// queued behind a slow pass does not read as a trigger that stopped.

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// scheduleStatus is one trigger's status row.
func scheduleStatus(t *testing.T, ds *dataset, triggerID string) (pending, inFlight, parked int64) {
	t.Helper()
	triggers, err := ds.TriggerStatuses(context.Background())
	if err != nil {
		t.Fatalf("trigger statuses: %v", err)
	}
	for _, st := range triggers {
		if st.ID == triggerID {
			return st.Pending, st.InFlight, st.Parked
		}
	}
	t.Fatalf("no status for %s", triggerID)
	return 0, 0, 0
}

func TestAScheduleFireRunsWhileOlderFiresAreParked(t *testing.T) {
	t.Parallel()
	// Two occurrences lie in the past: s and s+1h.
	s := nowUTC().Add(-90 * time.Minute).Truncate(time.Minute)
	s1 := s.Add(time.Hour)
	ds := supersedeDataset(t, s, "update")
	processOnce(t, ds)

	// The body fails every run: both occurrences park.
	rewindSchedule(t, ds, supersedeSync, s.Add(-time.Minute))
	if pending, _, _ := scheduleStatus(t, ds, supersedeSync); pending != 2 {
		t.Fatalf("pending = %d before the pass, want s and s+1h", pending)
	}
	processOnce(t, ds)
	if got, want := parkedFires(t, ds, supersedeSync), []string{fireID(s), fireID(s1)}; !slices.Equal(got, want) {
		t.Fatalf("parked %v, want %v", got, want)
	}

	// s+1h is due again with both parks standing: the pass runs it, and it
	// parks a second time beside them.
	rewindSchedule(t, ds, supersedeSync, s)
	if pending, inFlight, parked := scheduleStatus(t, ds, supersedeSync); pending != 1 || inFlight != 0 || parked != 2 {
		t.Fatalf("status = pending %d, in flight %d, parked %d, want 1, 0, 2", pending, inFlight, parked)
	}
	processOnce(t, ds)
	if got, want := parkedFires(t, ds, supersedeSync), []string{fireID(s), fireID(s1), fireID(s1)}; !slices.Equal(got, want) {
		t.Fatalf("parked %v, want s+1h dispatched past the parks: %v", got, want)
	}
	if pending, _, _ := scheduleStatus(t, ds, supersedeSync); pending != 0 {
		t.Fatalf("pending = %d after the pass, want 0", pending)
	}

	// The body recovers: s+1h runs once more, settles, and retires every
	// park at or before it.
	setJobSync(t, ds, supersedeOK)
	rewindSchedule(t, ds, supersedeSync, s)
	processOnce(t, ds)
	if got := parkedFires(t, ds, supersedeSync); len(got) != 0 {
		t.Fatalf("parked %v after s+1h settled, want none", got)
	}
	if runs := runRowsFor(t, ds, supersedeSync, runStatusOK); runs != 1 {
		t.Fatalf("wrote %d ok runs, want s+1h's", runs)
	}
}

func TestADueScheduleFireCountsAsInFlightWhileItRuns(t *testing.T) {
	t.Parallel()
	s := nowUTC().Add(-90 * time.Minute).Truncate(time.Minute)
	s1 := s.Add(time.Hour)
	ds := supersedeDataset(t, s, "update")
	processOnce(t, ds)
	rewindSchedule(t, ds, supersedeSync, s)

	// A function fire writes nothing until it settles: the dispatcher's mark
	// is what a status read sees while s+1h runs.
	done := ds.startFire(supersedeSync, fireID(s1))
	if pending, inFlight, _ := scheduleStatus(t, ds, supersedeSync); pending != 0 || inFlight != 1 {
		t.Fatalf("while s+1h runs: pending %d, in flight %d, want 0 and 1", pending, inFlight)
	}
	// Another trigger's occurrence of the same time is its own.
	rewindSchedule(t, ds, supersedeTool, s)
	if pending, inFlight, _ := scheduleStatus(t, ds, supersedeTool); pending != 1 || inFlight != 0 {
		t.Fatalf("%s: pending %d, in flight %d, want 1 and 0", supersedeTool, pending, inFlight)
	}
	done()
	if pending, inFlight, _ := scheduleStatus(t, ds, supersedeSync); pending != 1 || inFlight != 0 {
		t.Fatalf("after the mark ends: pending %d, in flight %d, want 1 and 0", pending, inFlight)
	}

	// A disabled trigger owes nothing the dispatcher is going to run.
	if _, err := ds.Patch(context.Background(), substrate.ActorAPI, typeTrigger, supersedeSync, substrate.PatchInput{
		Properties: map[string]any{"enabled": false},
	}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if pending, inFlight, _ := scheduleStatus(t, ds, supersedeSync); pending != 0 || inFlight != 0 {
		t.Fatalf("disabled: pending %d, in flight %d, want 0 and 0", pending, inFlight)
	}
}
