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
