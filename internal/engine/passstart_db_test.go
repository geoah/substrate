package engine

// An idle dispatcher pass costs the same whatever the number of triggers: it
// reads the changelog head, every record trigger's cursor and every schedule
// trigger's fire state once (readPassStart), and a trigger with nothing owed
// sends nothing of its own. Each record trigger once read its cursor and the
// head, and each schedule trigger its fire state, on every pass of every
// repository.

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestAnIdlePassSendsNoStatementPerTrigger(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	log := &statementLog{}
	ds := openCursorDataset(t, WithTestQueryTracer(log))
	mirror := vocabulary.RecordPath(kindFunction, "widgets.test.dev/widgets/mirror")
	startsAt := time.Now().UTC().Add(-time.Minute)
	added := 0
	grow := func(n int) {
		t.Helper()
		for ; added < n; added++ {
			for _, src := range []map[string]any{
				{"record": map[string]any{"kinds": []any{"widgets.test.dev/widgets/widget"}}},
				{"schedule": map[string]any{"recurrence": "FREQ=DAILY", "timezone": "UTC", "startsAt": startsAt.Format(time.RFC3339)}},
			} {
				arm := "record"
				if src["schedule"] != nil {
					arm = "schedule"
				}
				if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
					Kind: typeTrigger, ID: fmt.Sprintf("idle-%s-%d", arm, added),
					Properties: map[string]any{"source": src, "callable": mirror},
				}); err != nil {
					t.Fatalf("put %s trigger: %v", arm, err)
				}
			}
		}
	}
	// idlePass runs passes until one finds nothing to scan past, and
	// answers the per-trigger reads of the pass after it.
	idlePass := func() (cursors, fireStates, heads int) {
		t.Helper()
		for range 2 {
			if _, err := ds.ProcessTriggers(ctx); err != nil {
				t.Fatalf("pass: %v", err)
			}
		}
		log.take()
		if _, err := ds.ProcessTriggers(ctx); err != nil {
			t.Fatalf("pass: %v", err)
		}
		for _, q := range log.take() {
			switch {
			case strings.Contains(q, "FROM trigger_cursors WHERE trigger_id"):
				cursors++
			case strings.Contains(q, "FROM trigger_schedule WHERE trigger_id"):
				fireStates++
			case strings.Contains(strings.ToLower(q), "max(seq)"):
				heads++
			}
		}
		return cursors, fireStates, heads
	}
	for _, n := range []int{2, 8} {
		grow(n)
		cursors, fireStates, heads := idlePass()
		if cursors != 0 || fireStates != 0 || heads != 1 {
			t.Fatalf("an idle pass over %d record and %d schedule triggers read %d cursors, %d fire states and the head %d times, want none, none and once",
				n+1, n, cursors, fireStates, heads)
		}
	}
}

// The pass's start reads a record trigger as idle only until a delivery of
// the pass starts: what a delivery writes reaches the triggers after it in
// id order in the same pass, as it did when each trigger read the head
// itself.
func TestADeliveryReachesTheTriggersAfterItInTheSamePass(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openCursorDataset(t)
	const pkg = "widgets.test.dev/widgets"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.FunctionManifest(pkg, "tally", map[string]any{
			"description": "tallies every task the mirror writes",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"writes": []any{taskKind}},
			"source": `
def main(input, host):
    env = input["envelope"]
    return {"effects": [{"action": "put", "kind": "` + taskKind + `",
                         "id": "tally-" + env["change"]["id"], "properties": {"name": "tally"}}]}
`,
		}),
	}); err != nil {
		t.Fatalf("install the tally: %v", err)
	}
	// After the mirror's trigger (on-mirror.…) in id order.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger, ID: "on-tally",
		Properties: map[string]any{
			"source":   map[string]any{"record": map[string]any{"kinds": []any{taskKind}}},
			"callable": vocabulary.RecordPath(kindFunction, pkg+"/tally"),
		},
	}); err != nil {
		t.Fatalf("put the tally trigger: %v", err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("pass: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: pkg + "/widget", ID: "w1", Properties: map[string]any{"name": "w1"},
	}); err != nil {
		t.Fatalf("put widget: %v", err)
	}
	// The tally scans past the widget, which it does not match, so the pass
	// begins with it idle and only the mirror's delivery gives it work.
	if _, err := ds.WakeTrigger(ctx, "on-tally"); err != nil {
		t.Fatalf("wake the tally: %v", err)
	}
	start, err := ds.readPassStart(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !start.idleRecord("on-tally") || start.idleRecord("on-mirror."+pkg) {
		t.Fatal("the pass would not begin with the tally idle and the mirror owed the widget")
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("pass: %v", err)
	}
	if _, err := ds.Get(ctx, taskKind, "t-w1"); err != nil {
		t.Fatalf("the mirror's task: %v", err)
	}
	if _, err := ds.Get(ctx, taskKind, "tally-t-w1"); err != nil {
		t.Fatalf("the tally of the mirror's task, in the pass that wrote it: %v", err)
	}
}

// hourlyFunction installs a schedule function that mints one task per fire,
// and puts an hourly trigger on it whose fire state is rewound so that
// `owed` occurrences are due.
func hourlyFunction(t *testing.T, ds *dataset, owed int) loadedTrigger {
	t.Helper()
	ctx := context.Background()
	const pkg = "widgets.test.dev/widgets"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.FunctionManifest(pkg, "hourly", map[string]any{
			"description": "mints one task per fire",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"writes": []any{taskKind}},
			"source": `
def main(input, host):
    fire = input["envelope"]["fire"]
    return {"effects": [{"action": "put", "kind": "` + taskKind + `",
                         "id": "fire-" + fire["id"], "properties": {"name": fire["at"]}}]}
`,
		}),
	}); err != nil {
		t.Fatalf("install the hourly function: %v", err)
	}
	startsAt := nowUTC().Add(-time.Duration(owed)*time.Hour + 30*time.Minute).Truncate(time.Minute)
	rec, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source": map[string]any{"schedule": map[string]any{
				"recurrence": "FREQ=HOURLY", "timezone": "UTC", "startsAt": startsAt.Format(time.RFC3339),
			}},
			"callable": vocabulary.RecordPath(kindFunction, pkg+"/hourly"),
		},
	})
	if err != nil {
		t.Fatalf("put the hourly trigger: %v", err)
	}
	if _, err := ds.db.ExecContext(ctx, `
		UPDATE trigger_schedule SET fired_at = $2 WHERE trigger_id = $1`, rec.ID, startsAt.Add(-time.Minute)); err != nil {
		t.Fatalf("rewind the fire state: %v", err)
	}
	tr, createdAt, err := ds.triggerByID(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	return loadedTrigger{trigger: tr, CreatedAt: createdAt}
}

// What a hand's wake delivered after the pass began is not delivered again
// by the pass: the pass's cursors and fire states only say whether a
// trigger has work, and a trigger with work delivers from its row as it
// stands. A function body that ran again would repeat its outbound calls
// before its settlement lost the cursor's swap.
func TestAPassDoesNotRedeliverWhatAWakeDeliveredSinceItBegan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var mirrored, fired atomic.Int32
	ds := openCursorDataset(t, WithTestInvokeHook(func(fn string) {
		switch fn {
		case "widgets.test.dev/widgets/mirror":
			mirrored.Add(1)
		case "widgets.test.dev/widgets/hourly":
			fired.Add(1)
		}
	}))
	const mirrorID = "on-mirror.widgets.test.dev/widgets"
	hourly := hourlyFunction(t, ds, 2)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: "widgets.test.dev/widgets/widget", ID: "w1", Properties: map[string]any{"name": "w1"},
	}); err != nil {
		t.Fatalf("put widget: %v", err)
	}

	pass, err := ds.readPassStart(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if pass.idleRecord(mirrorID) {
		t.Fatal("the pass read the mirror as idle with a widget owed")
	}
	for _, id := range []string{mirrorID, hourly.ID} {
		if _, err := ds.WakeTrigger(ctx, id); err != nil {
			t.Fatalf("wake %s: %v", id, err)
		}
	}
	if mirrored.Load() != 1 || fired.Load() != 2 {
		t.Fatalf("the wakes ran the mirror %d times and the hourly %d, want 1 and 2", mirrored.Load(), fired.Load())
	}

	mirror, _, err := ds.triggerByID(ctx, mirrorID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.processRecordTrigger(ctx, mirror, newPassDeadline(), pass); err != nil {
		t.Fatalf("the pass's mirror turn: %v", err)
	}
	if _, _, err := ds.processScheduleTrigger(ctx, hourly, newPassDeadline(), scheduleDrainPerPass, pass); err != nil {
		t.Fatalf("the pass's hourly fire: %v", err)
	}
	if mirrored.Load() != 1 || fired.Load() != 2 {
		t.Fatalf("after the pass the mirror ran %d times and the hourly %d, want 1 and 2: the pass delivered over the wakes",
			mirrored.Load(), fired.Load())
	}
}

// A schedule fire marks the pass before its body runs, as a record delivery
// does, so no record trigger whose turn starts after it is read as idle, and
// one fed by what the fire wrote receives it in the same pass.
func TestAScheduleFireMarksThePassBeforeItsBodyRuns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var pass atomic.Pointer[passStart]
	var marked atomic.Int32
	ds := openCursorDataset(t, WithTestInvokeHook(func(fn string) {
		if p := pass.Load(); p != nil && fn == "widgets.test.dev/widgets/hourly" && p.delivering.Load() {
			marked.Add(1)
		}
	}))
	hourly := hourlyFunction(t, ds, 1)
	start, err := ds.readPassStart(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pass.Store(start)
	if _, fired, err := ds.processScheduleTrigger(ctx, hourly, newPassDeadline(), scheduleDrainPerPass, start); err != nil || fired != 1 {
		t.Fatalf("the fire: %d fired (%v), want 1", fired, err)
	}
	if marked.Load() != 1 {
		t.Fatal("the fire's body ran before the pass was marked: a record turn starting meanwhile is read as idle")
	}
}

// A trigger the pass's start reads as having work reads the head when its
// turn comes, not the head the pass began with: a coalescing trigger whose
// record changed again meanwhile delivers the record once, for both changes.
func TestATurnWithWorkReadsTheHeadItsTurnBeganWith(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var mirrored atomic.Int32
	ds := openCursorDataset(t, WithTestInvokeHook(func(fn string) {
		if fn == "widgets.test.dev/widgets/mirror" {
			mirrored.Add(1)
		}
	}))
	rec, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger, ID: "on-coalesce",
		Properties: map[string]any{
			"source":   map[string]any{"record": map[string]any{"kinds": []any{"widgets.test.dev/widgets/widget"}, "coalesce": true}},
			"callable": vocabulary.RecordPath(kindFunction, "widgets.test.dev/widgets/mirror"),
		},
	})
	if err != nil {
		t.Fatalf("put the coalescing trigger: %v", err)
	}
	put := func(name string) {
		t.Helper()
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: "widgets.test.dev/widgets/widget", ID: "w1", Properties: map[string]any{"name": name},
		}); err != nil {
			t.Fatalf("put widget: %v", err)
		}
	}
	put("first")
	pass, err := ds.readPassStart(ctx)
	if err != nil {
		t.Fatal(err)
	}
	put("second")
	tr, _, err := ds.triggerByID(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.processRecordTrigger(ctx, tr, newPassDeadline(), pass); err != nil {
		t.Fatalf("the coalescing turn: %v", err)
	}
	if got := mirrored.Load(); got != 1 {
		t.Fatalf("the coalescing trigger ran the mirror %d times for two changes to one widget, want once", got)
	}
}
