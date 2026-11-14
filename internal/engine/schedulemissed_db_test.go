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
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const missedPkg = "widgets.test.dev/widgets"

// missedTrigger installs the fire-counting function and an hourly schedule
// trigger on it, anchored two weeks back, with the given `missed` (none when
// empty). It runs the pass that initializes the fire state at now.
func missedTrigger(t *testing.T, missed string, opts ...Option) (*dataset, string) {
	t.Helper()
	ctx := context.Background()
	ds := openCursorDataset(t, opts...)
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
// fires the 167 before it. The schedule is read on a clock held half past an
// hour already gone, so no pass crosses into the next occurrence.
func TestAWeekOfflineRunsOneSync(t *testing.T) {
	t.Parallel()
	now := nowUTC().Truncate(time.Hour).Add(-30 * time.Minute)
	ds, triggerID := missedTrigger(t, "", WithTestScheduleClock(func() time.Time { return now }))
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
	got := firedAtOf(t, ds, triggerID)
	if !got.Equal(latest) {
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

// A trigger an edit makes a schedule takes its fire state at the edit, as a
// created one does, and a disabled one too: without it the repository's
// next open would give it one, and every occurrence between the edit and
// that open would be owed by nobody. The fire state rides the ledger, so a
// rebuild keeps it.
func TestATriggerAnEditMakesAScheduleTakesItsFireStateAtTheEdit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _ := missedTrigger(t, "")
	tr, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"enabled":  false,
			"source":   map[string]any{"record": map[string]any{"kinds": []any{"widgets.test.dev/widgets/widget"}}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", missedPkg+"/hourly"),
		},
	})
	if err != nil {
		t.Fatalf("put a record trigger: %v", err)
	}
	before := nowUTC()
	if _, err := ds.Patch(ctx, substrate.ActorAPI, typeTrigger, tr.ID, substrate.PatchInput{
		Properties: map[string]any{"source": map[string]any{"schedule": map[string]any{
			"recurrence": "FREQ=HOURLY", "timezone": "UTC", "missed": "all",
		}}},
	}); err != nil {
		t.Fatalf("make it a schedule: %v", err)
	}
	at := firedAtOf(t, ds, tr.ID)
	if at.Before(before.Add(-time.Second)) || at.After(nowUTC()) {
		t.Fatalf("fire state %s, want the edit's time (after %s)", at, before)
	}
	if _, err := ds.svc.RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if got := firedAtOf(t, ds, tr.ID); !got.Equal(at) {
		t.Fatalf("fire state %s after a rebuild, want %s", got, at)
	}
}
