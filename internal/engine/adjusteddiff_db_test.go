package engine_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The owner may adjust a change request's values on the accepting write
// (decision 0106): `adjustedDiff` is admitted like a proposed diff, applied
// instead of it under the same re-validation, and frozen afterwards, while
// `diff` keeps what was proposed.

// acceptAdjusted decides a request the owner's way with adjusted values.
func acceptAdjusted(t *testing.T, ds substrate.Dataset, id string, adjusted any) error {
	t.Helper()
	v := requestVersion(t, ds, id)
	_, err := ds.Patch(context.Background(), owner, requestKind, id, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted", "adjustedDiff": adjusted}, IfVersion: &v,
	})
	return err
}

// proposePatch lands a patch request against a fresh task and returns both.
func proposePatch(t *testing.T, ds substrate.Dataset, diff map[string]any) (*substrate.Record, *substrate.Record) {
	t.Helper()
	task := mustPut(t, ds, owner, substrate.PutInput{
		Kind: taskType, Properties: map[string]any{"name": "draft"},
	})
	req := mustPut(t, ds, engram, substrate.PutInput{
		Kind: requestKind, Properties: map[string]any{
			"target": vocabulary.RecordPath(taskType, task.ID),
			"diff":   diff,
		},
	})
	return task, req
}

func diffProps(t *testing.T, v any) map[string]any {
	t.Helper()
	d, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("diff is %T", v)
	}
	props, ok := d["properties"].(map[string]any)
	if !ok {
		t.Fatalf("diff has no properties: %+v", d)
	}
	return props
}

func TestAdjustedAcceptAppliesTheOwnersValues(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	task, req := proposePatch(t, ds, map[string]any{"properties": map[string]any{
		"name": "urgent draft", "description": "due friday",
	}})

	// The bare form lands wrapped, exactly as a proposed diff does.
	if err := acceptAdjusted(t, ds, req.ID, map[string]any{
		"name": "urgent draft", "description": "due thursday",
	}); err != nil {
		t.Fatalf("adjusted accept: %v", err)
	}
	got := mustGet(t, ds, taskType, task.ID)
	if got.Properties["description"] != "due thursday" || got.Properties["name"] != "urgent draft" {
		t.Fatalf("the adjusted values did not apply: %+v", got.Properties)
	}
	after := mustGet(t, ds, requestKind, req.ID)
	if after.Properties["decision"] != "accepted" {
		t.Fatalf("decision = %v", after.Properties["decision"])
	}
	if p := diffProps(t, after.Properties["diff"]); p["description"] != "due friday" {
		t.Fatalf("the proposal was overwritten: %+v", p)
	}
	if p := diffProps(t, after.Properties["adjustedDiff"]); p["description"] != "due thursday" {
		t.Fatalf("the adjustment was not kept: %+v", p)
	}

	// Frozen with the decision: the applied values cannot be rewritten later.
	_, err := ds.Patch(context.Background(), owner, requestKind, req.ID, substrate.PatchInput{
		Properties: map[string]any{"adjustedDiff": map[string]any{"description": "due monday"}},
	})
	wantRefusal(t, err, substrate.ErrForbidden, "adjustedDiff is immutable")
}

// A create request's adjustment is what the accept mints.
func TestAdjustedAcceptMintsTheOwnersCreate(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	req := mustPut(t, ds, engram, substrate.PutInput{
		Kind: requestKind, Properties: map[string]any{
			"op": "create", "targetKind": taskType, "targetId": "t-minted",
			"diff": map[string]any{"properties": map[string]any{"name": "buy milk"}},
		},
	})
	if err := acceptAdjusted(t, ds, req.ID, map[string]any{"properties": map[string]any{"name": "buy oat milk"}}); err != nil {
		t.Fatalf("adjusted accept: %v", err)
	}
	if got := mustGet(t, ds, taskType, "t-minted"); got.Properties["name"] != "buy oat milk" {
		t.Fatalf("minted %+v", got.Properties)
	}
}

// The adjustment gets the accept's re-validation: a target that moved since
// the proposal fails the accept, and nothing of the adjustment is stored.
func TestAdjustedAcceptOnAMovedTargetConflicts(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	task, req := proposePatch(t, ds, map[string]any{"description": "proposed"})
	mustPatch(t, ds, owner, taskType, task.ID, substrate.PatchInput{
		Properties: map[string]any{"name": "moved"},
	})
	err := acceptAdjusted(t, ds, req.ID, map[string]any{"description": "adjusted"})
	wantErr(t, err, substrate.ErrConflict, "adjusted accept on a moved target")
	after := mustGet(t, ds, requestKind, req.ID)
	if after.Properties["decision"] != "proposed" || after.Properties["adjustedDiff"] != nil {
		t.Fatalf("a failed adjusted accept left %+v", after.Properties)
	}
	if got := mustGet(t, ds, taskType, task.ID); got.Properties["description"] != nil {
		t.Fatalf("the failed accept applied: %+v", got.Properties)
	}
}

func TestAdjustedDiffAdmission(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	task, req := proposePatch(t, ds, map[string]any{"description": "proposed"})

	// A proposal carries its values in diff; adjustedDiff is the owner's.
	_, err := ds.Put(ctx, engram, substrate.PutInput{
		Kind: requestKind, Properties: map[string]any{
			"target":       vocabulary.RecordPath(taskType, task.ID),
			"diff":         map[string]any{"description": "a"},
			"adjustedDiff": map[string]any{"description": "b"},
		},
	})
	wantRefusal(t, err, substrate.ErrValidation, "written with the accept")

	// Only on the accepting write: not alone, not with a rejection.
	_, err = ds.Patch(ctx, owner, requestKind, req.ID, substrate.PatchInput{
		Properties: map[string]any{"adjustedDiff": map[string]any{"description": "b"}},
	})
	wantRefusal(t, err, substrate.ErrValidation, "written only with the accept")
	v := requestVersion(t, ds, req.ID)
	_, err = ds.Patch(ctx, owner, requestKind, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "rejected", "adjustedDiff": map[string]any{"description": "b"}},
		IfVersion:  &v,
	})
	wantRefusal(t, err, substrate.ErrValidation, "written only with the accept")

	// Admitted like a proposal: an undeclared property is refused at the write.
	err = acceptAdjusted(t, ds, req.ID, map[string]any{"bogus": "x"})
	wantRefusal(t, err, substrate.ErrValidation, "is not a property of")
	err = acceptAdjusted(t, ds, req.ID, "description: x")
	wantRefusal(t, err, substrate.ErrValidation, "adjustedDiff must be an object")
	if after := mustGet(t, ds, requestKind, req.ID); after.Properties["decision"] != "proposed" {
		t.Fatalf("a refused adjustment decided the request: %+v", after.Properties)
	}

	// A delete proposes no values, so there is nothing to adjust.
	del := mustPut(t, ds, engram, substrate.PutInput{
		Kind: requestKind, Properties: map[string]any{
			"op": "delete", "target": vocabulary.RecordPath(taskType, task.ID),
		},
	})
	err = acceptAdjusted(t, ds, del.ID, map[string]any{"description": "x"})
	wantRefusal(t, err, substrate.ErrValidation, "nothing to adjust")
}

// Installed code decides what was proposed and never adjusts it, even where
// its emit set would let it accept its own voluntary request.
func TestFunctionMayNotAdjust(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newFnDataset(t, nil, pyFn("adjuster", map[string]any{}, []any{requestKind, taskType}, `
def main(input, host):
    return {"effects": [
        {"action": "patch", "kind": "substrate.reamde.dev/core/recordpatchrequest",
         "id": input["args"]["request"],
         "properties": {"decision": "accepted",
                        "adjustedDiff": {"description": "the function's own"}}}
    ]}
`))
	task, req := proposePatch(t, ds, map[string]any{"description": "proposed"})
	_, _, err := ds.CallFunction(ctx, fnPackage+"/adjuster", map[string]any{"request": req.ID})
	wantRefusal(t, err, substrate.ErrForbidden, "only the owner adjusts")
	if got := mustGet(t, ds, taskType, task.ID); got.Properties["description"] != nil {
		t.Fatalf("the function's adjustment applied: %+v", got.Properties)
	}
}

// The adjustment replaces the proposal whole: a proposed property it does not
// name is not applied.
func TestAdjustedDiffReplacesTheProposal(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	task, req := proposePatch(t, ds, map[string]any{"properties": map[string]any{
		"name": "renamed", "description": "proposed",
	}})
	if err := acceptAdjusted(t, ds, req.ID, map[string]any{"description": "adjusted"}); err != nil {
		t.Fatalf("adjusted accept: %v", err)
	}
	got := mustGet(t, ds, taskType, task.ID)
	if got.Properties["description"] != "adjusted" || got.Properties["name"] != "draft" {
		t.Fatalf("the adjustment did not replace the proposal: %+v", got.Properties)
	}
}

// An adjustment that changes nothing fails the accept with a reason that names
// the owner's values, not the proposer's.
func TestAdjustedNoOpNamesTheAdjustment(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	_, req := proposePatch(t, ds, map[string]any{"description": "proposed"})
	err := acceptAdjusted(t, ds, req.ID, map[string]any{"name": "draft"})
	var ace *substrate.AcceptConflictError
	if !errors.As(err, &ace) || !strings.Contains(ace.Reason, "the owner's adjusted values") {
		t.Fatalf("want an accept conflict naming the adjustment, got %v", err)
	}
}

// A put onto a tombstoned request resurrects it without running the accept,
// so it may not store an adjustment that was never applied.
func TestResurrectingPutMayNotAdjust(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	task, req := proposePatch(t, ds, map[string]any{"description": "proposed"})
	if _, err := ds.Delete(ctx, owner, requestKind, req.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete request: %v", err)
	}
	_, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: requestKind, ID: req.ID, Properties: map[string]any{
			"target":       vocabulary.RecordPath(taskType, task.ID),
			"diff":         req.Properties["diff"],
			"decision":     "accepted",
			"adjustedDiff": map[string]any{"description": "never applied"},
		},
	})
	wantRefusal(t, err, substrate.ErrValidation, "written only with the accept")
}
