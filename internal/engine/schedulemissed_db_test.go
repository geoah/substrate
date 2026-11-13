package engine

// A schedule trigger says what it does with the occurrences it missed
// (decision 0267): `latest`, the default, fires the newest due one alone and
// moves the fire state over the rest; `all` fires each, oldest first, as
// every schedule did before. The catch-up of `all` is held by
// TestScheduleFiresMissedOccurrencesInOrder and the schedule lane's tests,
// which state it.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const missedPkg = "widgets.test.dev/widgets"

// missedTrigger installs the fire-counting function and an hourly schedule
// trigger on it, anchored two weeks back, with the given `missed` (none when
// empty). It runs the pass that initializes the fire state at now.
func missedTrigger(t *testing.T, missed string) (*dataset, string) {
	t.Helper()
	ctx := context.Background()
	ds := openCursorDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.FunctionManifest(missedPkg, "hourly", map[string]any{
			"description": "mints one task per fire",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"writes": []any{"samples.substrate.reamde.dev/tasks/task"}},
			"source": `
def main(input, host):
    fire = input["envelope"]["fire"]
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": "fire-" + fire["id"], "properties": {"name": fire["at"]}}]}
`,
		}),
	}); err != nil {
		t.Fatalf("install function: %v", err)
	}
	schedule := map[string]any{
		"recurrence": "FREQ=HOURLY", "timezone": "UTC",
		"startsAt": nowUTC().Add(-14 * 24 * time.Hour).Truncate(time.Hour).Format(time.RFC3339),
	}
	if missed != "" {
		schedule["missed"] = missed
	}
	tr, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source":   map[string]any{"schedule": schedule},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", missedPkg+"/hourly"),
		},
	})
	if err != nil {
		t.Fatalf("put schedule trigger: %v", err)
	}
	processOnce(t, ds)
	if n := countLiveOf(t, ds, "samples.substrate.reamde.dev/tasks/task"); n != 0 {
		t.Fatalf("a fresh schedule backfilled: %d tasks", n)
	}
	return ds, tr.ID
}

// THE DONE-WHEN: a week offline runs one sync. The fire state is set back a
// week, as a server that was off for one leaves it; one pass fires the newest
// hourly occurrence alone, the fire state lands on it, and no later pass
// fires the 167 before it.
func TestAWeekOfflineRunsOneSync(t *testing.T) {
	t.Parallel()
	ds, triggerID := missedTrigger(t, "")
	now := nowUTC()
	rewindSchedule(t, ds, triggerID, now.Add(-7*24*time.Hour))
	latest := now.Truncate(time.Hour)

	if st := triggerStatusOf(t, ds, triggerID); st.Pending != 1 {
		t.Fatalf("the status counts %d occurrences pending, want the latest alone", st.Pending)
	}
	processOnce(t, ds)
	processOnce(t, ds)
	if n := countLiveOf(t, ds, "samples.substrate.reamde.dev/tasks/task"); n != 1 {
		t.Fatalf("a week offline fired %d occurrences, want 1", n)
	}
	// A pass that crossed the hour fired the occurrence it began in.
	got := firedAtOf(t, ds, triggerID)
	if !got.Equal(latest) && !got.Equal(nowUTC().Truncate(time.Hour)) {
		t.Fatalf("fire state %s, want the latest occurrence %s", got, latest)
	}
	if _, err := ds.Get(context.Background(), "samples.substrate.reamde.dev/tasks/task", "fire-"+fireID(got)); err != nil {
		t.Fatalf("the one fire is not the latest occurrence %s: %v", fireID(got), err)
	}
	if st := triggerStatusOf(t, ds, triggerID); st.Pending != 0 {
		t.Fatalf("the status still counts %d occurrences pending", st.Pending)
	}
}

// `all` keeps every occurrence: the same week fires one pass's bound now and
// the rest at the passes after.
func TestAScheduleThatStatesAllFiresEachMissedOccurrence(t *testing.T) {
	t.Parallel()
	ds, triggerID := missedTrigger(t, "all")
	rewindSchedule(t, ds, triggerID, nowUTC().Add(-7*24*time.Hour))
	processOnce(t, ds)
	if n := countLiveOf(t, ds, "samples.substrate.reamde.dev/tasks/task"); n != scheduleDrainPerPass {
		t.Fatalf("one pass fired %d occurrences, want the bound of %d", n, scheduleDrainPerPass)
	}
}

// A value outside the two is refused when the trigger is written, naming
// both.
func TestAScheduleRefusesAnUnknownMissed(t *testing.T) {
	t.Parallel()
	ds, _ := missedTrigger(t, "")
	_, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source": map[string]any{"schedule": map[string]any{
				"recurrence": "FREQ=HOURLY", "timezone": "UTC", "missed": "none",
			}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", missedPkg+"/hourly"),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "missed") || !strings.Contains(err.Error(), "latest") {
		t.Fatalf("a schedule naming missed: none was admitted or refused without a reason: %v", err)
	}
}

// A fire still running when a newer occurrence falls due stays in flight:
// the status counts it beside the latest, which the next pass fires.
func TestARunningFireStaysInFlightWhenANewerOccurrenceIsDue(t *testing.T) {
	t.Parallel()
	ds, triggerID := missedTrigger(t, "")
	latest := nowUTC().Truncate(time.Hour)
	older := latest.Add(-time.Hour)
	rewindSchedule(t, ds, triggerID, older.Add(-time.Minute))
	done := ds.startFire(triggerID, fireID(older))
	if st := triggerStatusOf(t, ds, triggerID); st.InFlight != 1 || st.Pending != 1 {
		t.Fatalf("while %s runs and %s is due: in flight %d, pending %d; want 1 and 1", fireID(older), fireID(latest), st.InFlight, st.Pending)
	}
	done()
	if st := triggerStatusOf(t, ds, triggerID); st.InFlight != 0 || st.Pending != 1 {
		t.Fatalf("after %s ended: in flight %d, pending %d; want 0 and 1", fireID(older), st.InFlight, st.Pending)
	}
}
