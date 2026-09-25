package engine

// A parked SCHEDULE fire can be retried by hand (`trigger retry` on a park
// whose row carries a fire id and no seq): the retry re-invokes the same
// occurrence under the same fire id, retires the row when the body now
// delivers, and, when the body still fails, re-parks the row one attempt
// older and answers a classified refusal — not a bare 500 "internal error"
// with the reason only in the server's log.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestAParkedScheduleFireRetriesByHand(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openCursorDataset(t)
	const pkg = "widgets.test.dev/widgets"
	install := func(source string) {
		t.Helper()
		if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
			vocabulary.FunctionManifest(pkg, "hourly", map[string]any{
				"description": "mints one task per fire",
				"runtime":     vocabulary.RuntimePython,
				"permissions": map[string]any{"writes": []any{"samples.substrate.reamde.dev/tasks/task"}},
				"source":      source,
			}),
		}); err != nil {
			t.Fatalf("install function: %v", err)
		}
	}
	install(`
def main(input, host):
    raise Exception("the upstream endpoint is dead")
`)
	// Half an hour back: exactly one occurrence lies between the anchor and
	// now once the fire state is rewound to it.
	startsAt := nowUTC().Add(-30 * time.Minute).Truncate(time.Minute)
	tr, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source": map[string]any{"schedule": map[string]any{
				"recurrence": "FREQ=HOURLY", "timezone": "UTC", "startsAt": startsAt.Format(time.RFC3339),
			}},
			"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", pkg+"/hourly"),
		},
	})
	if err != nil {
		t.Fatalf("put schedule trigger: %v", err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	// One occurrence overdue; it parks, fire-shaped.
	if _, err := ds.db.ExecContext(ctx, `
		UPDATE trigger_schedule SET fired_at = $2 WHERE trigger_id = $1`, tr.ID, startsAt.Add(-time.Minute)); err != nil {
		t.Fatalf("rewind: %v", err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}
	failures, err := ds.TriggerFailures(ctx, tr.ID)
	if err != nil || len(failures) != 1 {
		t.Fatalf("parked deliveries: %+v (%v)", failures, err)
	}
	parked := failures[0]
	if parked.FireID != fireID(startsAt) || parked.Seq != 0 {
		t.Fatalf("the park is not fire-shaped: %+v", parked)
	}

	// The body still fails: the row stays, one attempt older, carrying the
	// new error, and the retry answers ErrParked (409 on the wire) naming
	// the error's first line — the bare runner error it used to return
	// matched no sentinel and was answered 500 "internal error".
	_, rerr := ds.RetryTriggerFailure(ctx, tr.ID, parked.ID)
	if !errors.Is(rerr, substrate.ErrParked) {
		t.Fatalf("a retry of a body that still raises answered %v, want ErrParked", rerr)
	}
	if !strings.Contains(rerr.Error(), "the upstream endpoint is dead") || !strings.Contains(rerr.Error(), "attempt 4") {
		t.Fatalf("the refusal does not name the new error and the attempt: %v", rerr)
	}
	if strings.Contains(rerr.Error(), "Traceback") {
		t.Fatalf("the refusal carries the whole traceback, want its first line: %v", rerr)
	}
	again, err := ds.TriggerFailures(ctx, tr.ID)
	if err != nil || len(again) != 1 {
		t.Fatalf("parked deliveries after the failed retry: %+v (%v)", again, err)
	}
	if again[0].Attempts != parked.Attempts+1 || !strings.Contains(again[0].LastError, "the upstream endpoint is dead") {
		t.Fatalf("the failed retry did not re-park one attempt older: before %+v, after %+v", parked, again[0])
	}

	// The body is fixed: the retry delivers the SAME occurrence (the body
	// sees the parked fire id) and retires the row.
	install(`
def main(input, host):
    fire = input["envelope"]["fire"]
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": "fire-" + fire["id"],
                         "properties": {"name": input["mode"] + " " + fire["at"]}}]}
`)
	ran, err := ds.RetryTriggerFailure(ctx, tr.ID, parked.ID)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if ran != 1 {
		t.Fatalf("the retry applied %d deliveries, want 1", ran)
	}
	if left, err := ds.TriggerFailures(ctx, tr.ID); err != nil || len(left) != 0 {
		t.Fatalf("failures after the retry: %+v (%v)", left, err)
	}
	if _, err := ds.Get(ctx, "samples.substrate.reamde.dev/tasks/task", "fire-"+parked.FireID); err != nil {
		t.Fatalf("the retry did not deliver the parked occurrence under its fire id: %v", err)
	}
}
