package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A hand that retries a waiting row between the walk's read and its hold
// leaves the row one attempt older and due later: the walk reads the row
// again under the hold and leaves it until then (decision 0266).
func TestTheWalkDoesNotRetryARowAHandRetriedMeanwhile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clock := &TestClock{}
	ds := openCursorDataset(t, WithTestRetryClock(clock.Now))
	const pkg = "widgets.test.dev/widgets"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.FunctionManifest(pkg, "mirror", map[string]any{
			"description": "mirrors widgets into tasks, while its provider answers",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"writes": []any{"samples.substrate.reamde.dev/tasks/task"}},
			"source":      "def main(input, host):\n    raise host.Temporary(\"the provider is down\")\n",
		}),
	}); err != nil {
		t.Fatalf("apply the failing body: %v", err)
	}
	triggerID := "on-mirror." + pkg
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: pkg + "/widget", ID: fid("w1"), Properties: map[string]any{"name": "w1"},
	}); err != nil {
		t.Fatalf("put widget: %v", err)
	}
	processOnce(t, ds)
	rows, err := ds.TriggerFailures(ctx, triggerID)
	if err != nil || len(rows) != 1 || rows[0].Attempts != 1 {
		t.Fatalf("parked = %+v %v, want one row at attempt 1", rows, err)
	}
	id := rows[0].ID

	// Due now. A hand retries it first, and it fails again: attempt 2, due
	// 30 seconds after the hand's retry.
	clock.Advance(15 * time.Second)
	if _, err := ds.RetryTriggerFailure(ctx, triggerID, id); !errors.Is(err, substrate.ErrParked) {
		t.Fatalf("the hand's retry = %v, want it parked again", err)
	}
	// The walk, holding a read from before the hand's retry, reaches the row.
	tr, _, err := ds.triggerByID(ctx, triggerID)
	if err != nil {
		t.Fatal(err)
	}
	ds.retryWaiting(ctx, tr, id)
	rows, err = ds.TriggerFailures(ctx, triggerID)
	if err != nil || len(rows) != 1 || rows[0].Attempts != 2 {
		t.Fatalf("after the walk reached the row the hand retried = %+v %v, want it left at attempt 2", rows, err)
	}
}
