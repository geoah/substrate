package engine_test

// A PARKED DELIVERY ALWAYS HAS A WAY OUT (issue #579). Twenty deliveries of
// one trigger parked in Mneme v6 under a body that has since been fixed;
// `trigger retry` answered "retried" for every one of them, twice, and every
// row was still there afterwards with its attempt count untouched. Nothing
// could remove them, because the automation API had list and retry and
// nothing else — and a scenario that asserts `parked == 0` cannot pass on a
// repository that has ever parked.
//
// Two answers, and they are different verbs on purpose. A retry RUNS the
// delivery, and it settles the row whatever the delivery then does — effects,
// or a guard that no longer matches, which is a settled delivery and not a
// silent success. A forget runs NOTHING: it is for the delivery that can
// never be made again, whose callable or record is gone, which is exactly the
// row a retry cannot clear.

import (
	"context"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// raisingSource is the parking fixture: a body that always fails.
const raisingSource = `
def main(input, host):
    raise Exception("the endpoint is dead")
`

// parkOne stands a guarded trigger up on a raising body and parks exactly one
// delivery of it, answering the trigger id and the parked failure.
func parkOne(t *testing.T, ds substrate.Dataset) (string, substrate.TriggerFailure) {
	t.Helper()
	mustPut(t, ds, fnActor, substrate.PutInput{
		Kind: widgetType, ID: "w1", Properties: map[string]any{"name": "w1", "mode": "go"},
	})
	process(t, ds)
	failures, err := ds.TriggerFailures(context.Background(), trigID("boom"))
	if err != nil {
		t.Fatalf("parked deliveries: %v", err)
	}
	if len(failures) != 1 {
		t.Fatalf("parked %d deliveries, want 1: %+v", len(failures), failures)
	}
	return trigID("boom"), failures[0]
}

// boomDataset is that fixture's repository: one trigger, guarded on the
// widget's `mode`, bound to a body that raises.
func boomDataset(t *testing.T) substrate.Dataset {
	t.Helper()
	return newFnDataset(t,
		[]enginetest.Trigger{trigOn("boom", map[string]any{
			"kinds": []any{widgetType},
			"when":  `record != null && record.properties.mode == "go"`,
		})},
		pyFn("boom", map[string]any{}, []any{taskType}, raisingSource))
}

// A retry whose guard no longer matches RETIRES the row. The delivery was
// settled — there is nothing left to deliver — so answering "retried" and
// leaving the row parked at the same attempt count is the report's exact
// complaint.
func TestARetryTheGuardNowSkipsRetiresTheParkedRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := boomDataset(t)
	triggerID, parked := parkOne(t, ds)

	// The work the delivery carried has been done another way, so the guard
	// is false now — which is the shape the twenty rows were in: a sync
	// request satisfied by a later run.
	mustPatch(t, ds, fnActor, widgetType, "w1", substrate.PatchInput{
		Properties: map[string]any{"mode": "done"},
	})

	ran, err := ds.RetryTriggerFailure(ctx, triggerID, parked.ID)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if ran != 0 {
		t.Fatalf("the retry applied %d deliveries; the guard says no", ran)
	}
	left, err := ds.TriggerFailures(ctx, triggerID)
	if err != nil {
		t.Fatalf("parked deliveries: %v", err)
	}
	if len(left) != 0 {
		t.Fatalf("the retry reported success and left %d rows parked: %+v", len(left), left)
	}
}

// And the row nothing can ever deliver: the callable is gone, so a retry
// REFUSES — correctly, it has nothing to run — and forget is what clears it.
func TestAParkedDeliveryCanBeForgotten(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := boomDataset(t)
	triggerID, parked := parkOne(t, ds)

	// The package is torn down under the parked row, the state a purged or
	// uninstalled provider leaves behind.
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(fnPackage, 0),
		vocabulary.ActorManifest(fnPackage, vocabulary.PackageActor(fnPackage)),
		vocabulary.KindManifest(fnPackage, map[string]any{"singular": "widget"},
			map[string]any{"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"mode": map[string]any{"type": "string", "fts": false},
			}}),
	}); err != nil {
		t.Fatalf("tear the function down: %v", err)
	}
	if _, err := ds.RetryTriggerFailure(ctx, triggerID, parked.ID); err == nil {
		t.Fatal("a retry of a delivery whose callable is gone reported success")
	}

	if err := ds.ForgetTriggerFailure(ctx, triggerID, parked.ID); err != nil {
		t.Fatalf("forget: %v", err)
	}
	left, err := ds.TriggerFailures(ctx, triggerID)
	if err != nil {
		t.Fatalf("parked deliveries: %v", err)
	}
	if len(left) != 0 {
		t.Fatalf("forget left %d rows parked: %+v", len(left), left)
	}
	// Forgetting it twice is a not-found, not a second silent success: the
	// row is the thing addressed.
	if err := ds.ForgetTriggerFailure(ctx, triggerID, parked.ID); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("forgetting a row that is gone answered %v, want not found", err)
	}
}

// fixedSource is the raising body after the fix: it writes a task for the
// widget, so the retry's effect says which body ran.
const fixedSource = `
def main(input, host):
    env = input["envelope"]
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": "t-" + env["change"]["id"],
                         "properties": {"name": "fixed"}}]}
`

// fixBoom applies the fixed body over the raising one, under the same
// function identity: the report's "the body was fixed".
func fixBoom(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), owner, []map[string]any{
		pyFn("boom", map[string]any{}, []any{taskType}, fixedSource),
	}); err != nil {
		t.Fatalf("apply the fixed body: %v", err)
	}
}

// setBoomEnabled flips the trigger record's `enabled`, the owner's switch.
func setBoomEnabled(t *testing.T, ds substrate.Dataset, enabled bool) {
	t.Helper()
	if _, err := ds.Patch(context.Background(), owner, "substrate.reamde.dev/core/trigger", trigID("boom"), substrate.PatchInput{
		Properties: map[string]any{"enabled": enabled},
	}); err != nil {
		t.Fatalf("set enabled=%v: %v", enabled, err)
	}
}

// requireNoneParked asserts the trigger lists no parked delivery and its
// status counts none, the `parked == 0` the report could never reach.
func requireNoneParked(t *testing.T, ds substrate.Dataset, triggerID string) {
	t.Helper()
	left, err := ds.TriggerFailures(context.Background(), triggerID)
	if err != nil {
		t.Fatalf("parked deliveries: %v", err)
	}
	if len(left) != 0 {
		t.Fatalf("%d rows still parked: %+v", len(left), left)
	}
	if st := statusOf(t, ds, triggerID); st.Parked != 0 || st.Pending != 0 {
		t.Fatalf("status still counts parked=%d pending=%d", st.Parked, st.Pending)
	}
}

// THE REPORT'S SHAPE, on a RECORD trigger whose guard still matches: the
// retry before the fix fails and says so (409 parked, one attempt older),
// the retry after the fix runs the body the fix applied, with no dispatcher
// pass between the apply and the retry (the stale-body half, #576), and
// retires the row. A third retry addresses a row that is gone and answers
// not found, never a second "retried".
func TestARetryUnderAFixedBodyRetiresARecordTriggerDelivery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := boomDataset(t)
	triggerID, parked := parkOne(t, ds)

	if _, err := ds.RetryTriggerFailure(ctx, triggerID, parked.ID); !errors.Is(err, substrate.ErrParked) {
		t.Fatalf("a retry under the still-raising body answered %v, want parked", err)
	}
	again, err := ds.TriggerFailures(ctx, triggerID)
	if err != nil {
		t.Fatalf("parked deliveries: %v", err)
	}
	if len(again) != 1 || again[0].Attempts != parked.Attempts+1 {
		t.Fatalf("a failed retry left %+v, want the one row one attempt older than %d", again, parked.Attempts)
	}

	fixBoom(t, ds)
	ran, err := ds.RetryTriggerFailure(ctx, triggerID, parked.ID)
	if err != nil {
		t.Fatalf("retry under the fixed body: %v", err)
	}
	if ran != 1 {
		t.Fatalf("the retry applied %d deliveries, want 1", ran)
	}
	if got := stampedTask(t, ds, "w1"); got != "fixed" {
		t.Fatalf("the retry wrote %q, want the fixed body's task", got)
	}
	requireNoneParked(t, ds, triggerID)

	if _, err := ds.RetryTriggerFailure(ctx, triggerID, parked.ID); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("retrying a retired row answered %v, want not found", err)
	}
}

// The operator's sequence in the report: fix the body, disable the trigger,
// re-enable it, retry. Neither the toggle nor the dispatcher passes between
// them touch the parked row, and the retry retires it.
func TestARetryAfterADisableAndReEnableRetiresTheParkedRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := boomDataset(t)
	triggerID, parked := parkOne(t, ds)

	fixBoom(t, ds)
	setBoomEnabled(t, ds, false)
	process(t, ds)
	setBoomEnabled(t, ds, true)
	process(t, ds)
	if left, err := ds.TriggerFailures(ctx, triggerID); err != nil || len(left) != 1 || left[0].Attempts != parked.Attempts {
		t.Fatalf("the toggle and two passes moved the parked row: %+v (%v)", left, err)
	}

	ran, err := ds.RetryTriggerFailure(ctx, triggerID, parked.ID)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if ran != 1 || stampedTask(t, ds, "w1") != "fixed" {
		t.Fatalf("the retry applied %d deliveries; the fixed body's task is missing", ran)
	}
	requireNoneParked(t, ds, triggerID)
}

// A retry is the owner's hand, and a disabled trigger does not refuse it: the
// delivery runs, its effects land and the row retires. What it must never do
// is answer success having run nothing, which is the report's complaint.
func TestARetryOnADisabledTriggerRunsTheDelivery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := boomDataset(t)
	triggerID, parked := parkOne(t, ds)

	fixBoom(t, ds)
	setBoomEnabled(t, ds, false)
	ran, err := ds.RetryTriggerFailure(ctx, triggerID, parked.ID)
	if err != nil {
		t.Fatalf("retry on a disabled trigger: %v", err)
	}
	if ran != 1 || stampedTask(t, ds, "w1") != "fixed" {
		t.Fatalf("the retry applied %d deliveries; the fixed body's task is missing", ran)
	}
	requireNoneParked(t, ds, triggerID)
}
