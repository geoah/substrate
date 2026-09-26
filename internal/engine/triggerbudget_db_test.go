package engine

// A SLOW TRIGGER DOES NOT HOLD THE PASS (#638). A record trigger whose
// deliveries each take seconds, sitting on a backlog, used to drain to head
// inside one dispatcher pass, so every trigger after it, schedules included,
// waited for the whole backlog. The pass now gives each trigger a wall-clock
// budget and fires the due schedules before any record trigger runs: one pass
// fires the schedule and leaves the slow trigger's backlog part-drained, and
// later passes finish it with every row delivered once and the cursor moving
// only past delivered rows.

import (
	"context"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// withTriggerPassBudget lowers the per-trigger pass budget for one test.
// Package-level, like withScheduleDrain: a caller MUST NOT call t.Parallel.
func withTriggerPassBudget(d time.Duration) func() {
	prev := triggerPassBudget
	triggerPassBudget = d
	return func() { triggerPassBudget = prev }
}

func TestASlowRecordTriggerDoesNotDelayADueSchedule(t *testing.T) {
	const (
		pkg     = "widgets.test.dev/widgets"
		widgets = 10
	)
	defer withTriggerPassBudget(300 * time.Millisecond)()
	ctx := context.Background()
	ds := openCursorDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.FunctionManifest(pkg, "slow", map[string]any{
			"description": "one slow fetch per widget",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"writes": []any{"samples.substrate.reamde.dev/tasks/task"}},
			"source": `
import time

def main(input, host):
    time.sleep(0.2)
    env = input["envelope"]
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": "slow-" + env["change"]["id"],
                         "properties": {"name": "slow"}}]}
`,
		}),
		vocabulary.FunctionManifest(pkg, "hourly", map[string]any{
			"description": "mints one task per fire",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"writes": []any{"samples.substrate.reamde.dev/tasks/task"}},
			"source": `
def main(input, host):
    fire = input["envelope"]["fire"]
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": "fire-" + fire["id"],
                         "properties": {"name": "fire"}}]}
`,
		}),
	}); err != nil {
		t.Fatalf("install functions: %v", err)
	}
	// Ids chosen so the slow trigger sorts first: the old walk reached the
	// schedule only once the slow trigger's drain returned.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger, ID: "a-slow",
		Properties: map[string]any{
			"source":   map[string]any{"record": map[string]any{"kinds": []any{pkg + "/widget"}}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", pkg+"/slow"),
		},
	}); err != nil {
		t.Fatalf("put slow trigger: %v", err)
	}
	startsAt := nowUTC().Add(-30 * time.Minute).Truncate(time.Minute)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger, ID: "b-hourly",
		Properties: map[string]any{
			"source": map[string]any{"schedule": map[string]any{
				"recurrence": "FREQ=HOURLY", "timezone": "UTC", "startsAt": startsAt.Format(time.RFC3339),
			}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", pkg+"/hourly"),
		},
	}); err != nil {
		t.Fatalf("put schedule trigger: %v", err)
	}
	for i := range widgets {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: pkg + "/widget", Properties: map[string]any{"name": "w" + string(rune('a'+i))},
		}); err != nil {
			t.Fatalf("put widget: %v", err)
		}
	}
	// One occurrence overdue.
	if _, err := ds.db.ExecContext(ctx, `
		UPDATE trigger_schedule SET fired_at = $2 WHERE trigger_id = $1`, "b-hourly", startsAt.Add(-time.Minute)); err != nil {
		t.Fatalf("rewind: %v", err)
	}

	countPrefixed := func(prefix string) int {
		t.Helper()
		var n int
		if err := ds.db.QueryRowContext(ctx, `
			SELECT count(*) FROM records WHERE kind = $1 AND deleted_at IS NULL AND id LIKE $2`,
			"samples.substrate.reamde.dev/tasks/task", prefix+"%").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	cursorOf := func(id string) int64 {
		t.Helper()
		var seq int64
		if err := ds.db.QueryRowContext(ctx, `SELECT seq FROM trigger_cursors WHERE trigger_id = $1`, id).Scan(&seq); err != nil {
			t.Fatal(err)
		}
		return seq
	}

	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	if n := countPrefixed("fire-"); n != 1 {
		t.Fatalf("the due schedule fired %d times in the first pass, want 1", n)
	}
	slow := countPrefixed("slow-")
	if slow == 0 || slow >= widgets {
		t.Fatalf("the slow trigger delivered %d of %d widgets in one pass: the pass did not move on at its budget", slow, widgets)
	}
	// The schedule fired before the slow trigger's first delivery, although
	// the slow trigger sorts first: schedules go ahead of record triggers.
	var fireSeq, firstSlowSeq int64
	if err := ds.db.QueryRowContext(ctx, `
		SELECT
		  (SELECT min(seq) FROM changelog WHERE kind = $1 AND record_id LIKE 'fire-%'),
		  (SELECT min(seq) FROM changelog WHERE kind = $1 AND record_id LIKE 'slow-%')`,
		"samples.substrate.reamde.dev/tasks/task").Scan(&fireSeq, &firstSlowSeq); err != nil {
		t.Fatal(err)
	}
	if fireSeq > firstSlowSeq {
		t.Fatalf("the schedule's fire landed at seq %d, after the slow trigger's first delivery at %d: the due schedule waited behind the record trigger", fireSeq, firstSlowSeq)
	}
	if head := maxSeqOf(t, ds); cursorOf("a-slow") >= head {
		t.Fatalf("the slow trigger's cursor is at head %d after a part-drained pass", head)
	}

	// Later passes finish the backlog: every widget delivered, none parked,
	// and the cursor at head.
	for range 4 * widgets {
		if countPrefixed("slow-") == widgets {
			break
		}
		if _, err := ds.ProcessTriggers(ctx); err != nil {
			t.Fatalf("process: %v", err)
		}
	}
	if n := countPrefixed("slow-"); n != widgets {
		t.Fatalf("the slow trigger delivered %d of %d widgets over its passes", n, widgets)
	}
	if failures, err := ds.TriggerFailures(ctx, "a-slow"); err != nil || len(failures) != 0 {
		t.Fatalf("parked deliveries: %+v (%v)", failures, err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	if head, got := maxSeqOf(t, ds), cursorOf("a-slow"); got != head {
		t.Fatalf("the slow trigger's cursor is %d after its backlog drained, want head %d", got, head)
	}
	if n := countPrefixed("fire-"); n != 1 {
		t.Fatalf("the schedule fired %d times over every pass, want its one due occurrence", n)
	}
}
