package engine

import (
	"context"
	"errors"
	"strings"
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
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
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
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{
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

// The walk checks the callable it runs. One the walk read before an apply
// let the function call another is not the one a record delivery runs, which
// it reads again as it starts: that one is checked, and the row is set aside
// for a person rather than run unattended (decision 0266).
func TestTheWalkChecksTheCallableItRuns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clock := &TestClock{}
	ds := openCursorDataset(t, WithTestRetryClock(clock.Now))
	const pkg = "widgets.test.dev/widgets"
	mirror := func(perms map[string]any) map[string]any {
		perms["writes"] = []any{"samples.substrate.reamde.dev/tasks/task"}
		return vocabulary.FunctionManifest(pkg, "mirror", map[string]any{
			"description": "mirrors widgets into tasks, while its provider answers",
			"runtime":     vocabulary.RuntimePython,
			"permissions": perms,
			"source":      "def main(input, host):\n    raise host.Temporary(\"the provider is down\")\n",
		})
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{mirror(map[string]any{})}); err != nil {
		t.Fatalf("apply the failing body: %v", err)
	}
	triggerID := "on-mirror." + pkg
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{
		Kind: pkg + "/widget", ID: fid("w1"), Properties: map[string]any{"name": "w1"},
	}); err != nil {
		t.Fatalf("put widget: %v", err)
	}
	processOnce(t, ds)
	rows, err := ds.TriggerFailures(ctx, triggerID)
	if err != nil || len(rows) != 1 || rows[0].Attempts != 1 || !strings.HasPrefix(rows[0].LastError, temporaryRetryMark) {
		t.Fatalf("parked = %+v %v, want one waiting row at attempt 1", rows, err)
	}
	id := rows[0].ID

	// The walk reads the trigger, and its callable passes the check...
	tr, _, err := ds.triggerByID(ctx, triggerID)
	if err != nil || !autoRetryable(tr) {
		t.Fatalf("the walk's read: %v, retryable %v", err, err == nil && autoRetryable(tr))
	}
	// ...then an apply lets the function call another before the walk runs it.
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.FunctionManifest(pkg, "helper", map[string]any{
			"description": "helps", "runtime": vocabulary.RuntimePython,
			"source": "def main(input, host):\n    return {}\n",
		}),
		mirror(map[string]any{"call": []any{pkg + "/helper"}}),
	}); err != nil {
		t.Fatalf("grant the function a call: %v", err)
	}
	clock.Advance(15 * time.Second)
	ds.retryWaiting(ctx, tr, id)

	rows, err = ds.TriggerFailures(ctx, triggerID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("after the walk = %+v %v, want the one row", rows, err)
	}
	if rows[0].Attempts != 1 || strings.HasPrefix(rows[0].LastError, temporaryRetryMark) ||
		!strings.Contains(rows[0].LastError, "the provider is down") {
		t.Fatalf("the row reads %+v: the walk ran a callable that may call another, want it set aside at attempt 1", rows[0])
	}
}
