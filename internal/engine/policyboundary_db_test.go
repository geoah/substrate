package engine_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Installed code never writes a policy, by any path (refuseInstalledPolicyWrite):
// a function granted the request kind and, explicitly, the policy kind still
// cannot propose a change to a policy and accept it itself, whether the
// request creates, patches or deletes one. Its effects stop at the write the
// accept materializes, and the delivery parks. The owner accepting a request a
// function proposed is the owner's hand, and lands.
func TestAFunctionCannotWriteAPolicyByAcceptingItsOwnRequest(t *testing.T) {
	t.Parallel()
	const request = vocabulary.KindRecordPatchRequest
	policy := vocabulary.KindRecordPatchPolicy
	closed, kept := vocabulary.RecordPath(policy, "closed"), vocabulary.RecordPath(policy, "kept")
	selfAccept := func(name, props string) string {
		return `
def main(input, host):
    c = input["envelope"]["change"]
    rid = "req-` + name + `-" + c["id"]
    return {"effects": [
        {"action": "put", "kind": "` + request + `", "id": rid, "properties": ` + props + `},
        {"action": "patch", "kind": "` + request + `", "id": rid, "properties": {"decision": "accepted"}}
    ]}
`
	}
	grants := []any{request, policy}
	ds := newFnDataset(t,
		[]enginetest.Trigger{
			trigOn("creates", map[string]any{"kinds": []any{widgetType}, "ops": []any{"create"}}),
			trigOn("patches", map[string]any{"kinds": []any{widgetType}, "ops": []any{"create"}}),
			trigOn("deletes", map[string]any{"kinds": []any{widgetType}, "ops": []any{"create"}}),
			trigOn("proposes", map[string]any{"kinds": []any{widgetType}, "ops": []any{"create"}}),
		},
		pyFn("creates", map[string]any{}, grants, selfAccept("creates", fmt.Sprintf(
			`{"op": "create", "targetKind": "%s", "targetId": "opened-" + c["id"], "diff": {"properties": {"selector": {"kinds": ["%s"]}, "action": "allow"}}}`,
			policy, taskType))),
		pyFn("patches", map[string]any{}, grants, selfAccept("patches", fmt.Sprintf(
			`{"op": "patch", "target": "%s", "diff": {"properties": {"action": "allow"}}}`, closed))),
		pyFn("deletes", map[string]any{}, grants, selfAccept("deletes", fmt.Sprintf(
			`{"op": "delete", "target": "%s"}`, kept))),
		pyFn("proposes", map[string]any{}, grants, fmt.Sprintf(`
def main(input, host):
    c = input["envelope"]["change"]
    return {"effects": [
        {"action": "put", "kind": "%s", "id": "proposed-" + c["id"],
         "properties": {"op": "create", "targetKind": "%s", "targetId": "owners-" + c["id"],
                        "diff": {"properties": {"selector": {"kinds": ["%s"]}, "action": "refuse"}}}}
    ]}
`, request, policy, taskType)),
	)
	ctx := context.Background()
	for _, id := range []string{"closed", "kept"} {
		if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: policy, ID: id, Properties: map[string]any{
			"selector": map[string]any{"kinds": []any{taskType}}, "action": "refuse",
		}}); err != nil {
			t.Fatalf("the owner writes a policy: %v", err)
		}
	}

	w := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "w"}})
	process(t, ds)

	if _, err := ds.Get(ctx, policy, "opened-"+w.ID); !errors.Is(err, substrate.ErrNotFound) {
		t.Errorf("a self-accepted create wrote a policy: %v", err)
	}
	if got := mustGet(t, ds, policy, "closed"); got.Properties["action"] != "refuse" {
		t.Errorf("a self-accepted patch opened the owner's policy: action %v", got.Properties["action"])
	}
	if got := mustGet(t, ds, policy, "kept"); got.DeletedAt != nil {
		t.Errorf("a self-accepted delete removed the owner's policy at %v", got.DeletedAt)
	}
	for _, name := range []string{"creates", "patches", "deletes"} {
		parked, err := ds.TriggerFailures(ctx, trigID(name))
		if err != nil || len(parked) == 0 {
			t.Errorf("%s: its delivery should have parked: parked %d, err %v", name, len(parked), err)
			continue
		}
		if !strings.Contains(parked[0].LastError, "installed code never writes the door's own rules") {
			t.Errorf("%s parked for another reason: %s", name, parked[0].LastError)
		}
	}

	// The owner accepts what a function proposed: that is the owner's hand.
	proposed := mustGet(t, ds, request, "proposed-"+w.ID)
	if _, err := ds.Patch(ctx, substrate.ActorPerson, request, proposed.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: ptr(proposed.Version),
	}); err != nil {
		t.Fatalf("the owner accepts a function's proposal to add a policy: %v", err)
	}
	if got := mustGet(t, ds, policy, "owners-"+w.ID); got.Properties["action"] != "refuse" {
		t.Errorf("the owner's accept did not land the policy: %+v", got.Properties)
	}
}
