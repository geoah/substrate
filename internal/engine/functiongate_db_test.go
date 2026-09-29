package engine_test

// Gated function effects (issue #74, shape 1): a recordpatchpolicy whose
// `functions` arm names a function, or the function's own
// `confirmation: always`, holds each put, patch or delete the body returns
// as a recordpatchrequest the function's actor proposes. The target stays
// untouched until the owner accepts, and nothing escapes
// `permissions.writes`: an effect outside it is refused, never held.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const functionKind = "substrate.reamde.dev/core/function"

// gatePolicy writes one owner policy.
func gatePolicy(t *testing.T, ds substrate.Dataset, id string, props map[string]any) *substrate.Record {
	t.Helper()
	return mustPut(t, ds, owner, substrate.PutInput{
		Kind: vocabulary.KindRecordPatchPolicy, ID: id, Properties: props,
	})
}

// patchRequests lists the live change requests.
func patchRequests(t *testing.T, ds substrate.Dataset) []*substrate.Record {
	t.Helper()
	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{Kinds: []string{vocabulary.KindRecordPatchRequest}},
		First:  50,
	})
	if err != nil {
		t.Fatalf("list requests: %v", err)
	}
	return page.Records
}

// mustAccept decides one request as the owner (requestadmit_db_test.go
// accept), failing the test when the accept does.
func mustAccept(t *testing.T, ds substrate.Dataset, req *substrate.Record) {
	t.Helper()
	if err := accept(t, ds, req.ID); err != nil {
		t.Fatalf("accept %s: %v", req.ID, err)
	}
}

// heldBy asserts the one request is the held create of the task, proposed by
// the function's actor and stamped with the function.
func heldBy(t *testing.T, ds substrate.Dataset, fn, taskID string) *substrate.Record {
	t.Helper()
	reqs := patchRequests(t, ds)
	if len(reqs) != 1 {
		t.Fatalf("requests: %d, want 1", len(reqs))
	}
	req := reqs[0]
	if req.Properties["op"] != "create" || req.Properties["targetKind"] != taskType || req.Properties["targetId"] != taskID {
		t.Fatalf("request envelope: %+v", req.Properties)
	}
	if got, want := refPathValue(req, "function"), vocabulary.RecordPath(functionKind, fnPackage+"/"+fn); got != want {
		t.Fatalf("request function = %q, want %q", got, want)
	}
	// The function's own actor wrote the request, and nothing else.
	var wroteRequest bool
	for _, ch := range actorChanges(t, ds, fnPackage+"/"+fn) {
		switch ch.Kind {
		case vocabulary.KindRecordPatchRequest:
			wroteRequest = wroteRequest || ch.RecordID == req.ID
		case taskType:
			t.Fatalf("the function wrote the target itself: %+v", ch)
		}
	}
	if !wroteRequest {
		t.Fatalf("the request is not attributed to %s", fn)
	}
	return req
}

// TestPolicyFunctionsArmHoldsATriggeredWrite: a gate naming a function in
// `functions` holds what its trigger delivery returns. The task is not
// written, the request carries the proposed values, the delivery settles
// clean, and the owner's accept is what writes the task.
func TestPolicyFunctionsArmHoldsATriggeredWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newFnDataset(t,
		[]enginetest.Trigger{trigOn("mirror", map[string]any{"kinds": []any{widgetType}, "ops": []any{"create"}})},
		pyFn("mirror", map[string]any{}, []any{taskType}, mirrorSource))
	policy := gatePolicy(t, ds, "gate-mirror", map[string]any{
		"selector": map[string]any{"functions": []any{fnPackage + "/mirror"}},
		"action":   "gate",
	})

	w := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "held"}})
	process(t, ds)

	taskID := "t-" + w.ID
	if _, err := ds.Get(ctx, taskType, taskID); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the gated write landed: %v", err)
	}
	req := heldBy(t, ds, "mirror", taskID)
	if got := refPathValue(req, "policy"); got != vocabulary.RecordPath(vocabulary.KindRecordPatchPolicy, policy.ID) {
		t.Fatalf("request policy = %q", got)
	}
	diff, _ := req.Properties["diff"].(map[string]any)
	props, _ := diff["properties"].(map[string]any)
	if props["name"] != "held" {
		t.Fatalf("request diff: %+v", req.Properties["diff"])
	}
	parked, err := ds.TriggerFailures(ctx, trigID("mirror"))
	if err != nil || len(parked) != 0 {
		t.Fatalf("a held effect parked the delivery: %+v %v", parked, err)
	}
	if st := statusOf(t, ds, trigID("mirror")); st.Lag != 0 {
		t.Fatalf("the delivery did not settle: %+v", st)
	}
	if summary := runEffects(t, ds); summary["gate"] == nil || summary["put"] != nil {
		t.Fatalf("run effects: %+v, want one gate and no put", summary)
	}

	mustAccept(t, ds, req)
	if got := mustGet(t, ds, taskType, taskID); got.Title != "held" {
		t.Fatalf("the accepted request wrote %+v", got)
	}
}

// runEffects reads the applied-effects summary of the one run a test's
// deliveries left on the ledger.
func runEffects(t *testing.T, ds substrate.Dataset) map[string]any {
	t.Helper()
	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{Kinds: []string{triggerRunType}}, First: 10,
	})
	if err != nil || len(page.Records) != 1 {
		t.Fatalf("runs: %d %v, want 1", len(page.Records), err)
	}
	summary, _ := page.Records[0].Properties["effects"].(map[string]any)
	return summary
}

// TestHeldCreateOnlyPutOfALiveTargetHoldsNothing: an `ifAbsent` put is a
// no-op when its target is live, and a gate over it stays one. No request is
// written, the record keeps its values, and the run counts the put it was,
// never a `gate` with no request behind it.
func TestHeldCreateOnlyPutOfALiveTargetHoldsNothing(t *testing.T) {
	t.Parallel()
	ds := newFnDataset(t,
		[]enginetest.Trigger{trigOn("minter", map[string]any{"kinds": []any{widgetType}, "ops": []any{"create"}})},
		pyFn("minter", map[string]any{}, []any{taskType}, `
def main(input, host):
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": "t-live", "ifAbsent": True, "properties": {"name": "minted"}}]}
`))
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskType, ID: "t-live", Properties: map[string]any{"name": "mine"}})
	gatePolicy(t, ds, "gate-minter", map[string]any{
		"selector": map[string]any{"functions": []any{fnPackage + "/minter"}},
		"action":   "gate",
	})
	mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType})
	process(t, ds)

	if reqs := patchRequests(t, ds); len(reqs) != 0 {
		t.Fatalf("a create-only put of a live target was held: %+v", reqs[0].Properties)
	}
	if got := mustGet(t, ds, taskType, "t-live"); got.Title != "mine" {
		t.Fatalf("the live task moved: %+v", got)
	}
	if summary := runEffects(t, ds); summary["put"] == nil || summary["gate"] != nil {
		t.Fatalf("run effects: %+v, want the put and no gate", summary)
	}
}

// TestPolicyWithoutFunctionsArmLeavesTriggeredWritesAlone: a gate that names
// no function speaks for agent writes only, as it did before `functions`
// existed, so a trigger's write under it still lands. This is the half that
// keeps a `{}` gate from holding every sync in the repository.
func TestPolicyWithoutFunctionsArmLeavesTriggeredWritesAlone(t *testing.T) {
	t.Parallel()
	ds := newFnDataset(t,
		[]enginetest.Trigger{trigOn("mirror", map[string]any{"kinds": []any{widgetType}, "ops": []any{"create"}})},
		pyFn("mirror", map[string]any{}, []any{taskType}, mirrorSource))
	gatePolicy(t, ds, "gate-tasks", map[string]any{
		"selector": map[string]any{"kinds": []any{taskType}},
		"action":   "gate",
	})
	w := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "free"}})
	process(t, ds)
	if got := mustGet(t, ds, taskType, "t-"+w.ID); got.Title != "free" {
		t.Fatalf("task: %+v", got)
	}
	if reqs := patchRequests(t, ds); len(reqs) != 0 {
		t.Fatalf("an agent-only gate held a trigger's write: %+v", reqs[0].Properties)
	}
}

// TestConfirmationAlwaysHoldsATriggeredWrite: the function's own floor holds
// its effects with no policy at all, cites no policy, and keeps installed
// code from deciding the request: a second function whose grants cover both
// the request and the task cannot accept it, the owner can.
func TestConfirmationAlwaysHoldsATriggeredWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newFnDataset(t,
		[]enginetest.Trigger{
			trigOn("careful", map[string]any{"kinds": []any{widgetType}, "ops": []any{"create"}}),
			trigOn("rubberstamp", map[string]any{"kinds": []any{vocabulary.KindRecordPatchRequest}, "ops": []any{"create"}}),
		},
		pyFn("careful", map[string]any{"confirmation": "always"}, []any{taskType}, mirrorSource),
		pyFn("rubberstamp", map[string]any{}, []any{vocabulary.KindRecordPatchRequest, taskType}, `
def main(input, host):
    c = input["envelope"]["change"]
    return {"effects": [{"action": "patch", "kind": "substrate.reamde.dev/core/recordpatchrequest",
                         "id": c["id"], "properties": {"decision": "accepted"}}]}
`))

	w := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "floored"}})
	process(t, ds)
	process(t, ds)

	taskID := "t-" + w.ID
	if _, err := ds.Get(ctx, taskType, taskID); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("a floored write landed: %v", err)
	}
	req := heldBy(t, ds, "careful", taskID)
	if req.Properties["policy"] != nil {
		t.Fatalf("a floor cited a policy: %v", req.Properties["policy"])
	}
	if req.Properties["decision"] != "proposed" {
		t.Fatalf("installed code decided a held request: %v", req.Properties["decision"])
	}
	parked, err := ds.TriggerFailures(ctx, trigID("rubberstamp"))
	if err != nil || len(parked) != 1 || !strings.Contains(parked[0].LastError, "the owner decides it") {
		t.Fatalf("the rubber stamp was not refused: %+v %v", parked, err)
	}

	mustAccept(t, ds, req)
	if got := mustGet(t, ds, taskType, taskID); got.Title != "floored" {
		t.Fatalf("the accepted request wrote %+v", got)
	}
}

// TestHeldEffectOutsideWritesIsRefused: a gate never widens the grant. An
// effect on a kind outside `permissions.writes` fails the delivery before the
// door is read, so it parks and no request is queued, under a policy naming
// the function and under the function's own floor alike.
func TestHeldEffectOutsideWritesIsRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const project = "samples.substrate.reamde.dev/tasks/project"
	ds := newFnDataset(t,
		[]enginetest.Trigger{trigOn("reach", map[string]any{"kinds": []any{widgetType}})},
		pyFn("reach", map[string]any{"confirmation": "always"}, []any{taskType}, `
def main(input, host):
    c = input["envelope"]["change"]
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/project",
                         "id": "p-" + c["id"], "properties": {"name": "reached"}}]}
`))
	gatePolicy(t, ds, "gate-reach", map[string]any{
		"selector": map[string]any{"functions": []any{fnPackage + "/reach"}},
		"action":   "gate",
	})

	w := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType})
	process(t, ds)

	if _, err := ds.Get(ctx, project, "p-"+w.ID); err == nil {
		t.Fatal("an effect outside permissions.writes was applied")
	}
	if reqs := patchRequests(t, ds); len(reqs) != 0 {
		t.Fatalf("an effect outside permissions.writes was queued: %+v", reqs[0].Properties)
	}
	parked, err := ds.TriggerFailures(ctx, trigID("reach"))
	if err != nil || len(parked) != 1 || !strings.Contains(parked[0].LastError, "emit") {
		t.Fatalf("parked rows: %+v %v", parked, err)
	}
}

// TestPolicyFunctionsArmOnADirectCall: the arm covers a direct call too. A
// gate holds the effect and the reply counts no applied effect; a refuse
// fails the call as forbidden with nothing written.
func TestPolicyFunctionsArmOnADirectCall(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newFnDataset(t, nil, adderFn())
	gatePolicy(t, ds, "gate-adder", map[string]any{
		"selector": map[string]any{"functions": []any{fnPackage + "/adder"}},
		"action":   "gate",
	})
	out, applied, err := ds.CallFunction(ctx, owner, fnPackage+"/adder", map[string]any{"title": "held call"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	id, _ := out.(map[string]any)["id"].(string)
	if applied != 0 {
		t.Fatalf("a held effect counted as applied: %d", applied)
	}
	if _, err := ds.Get(ctx, taskType, id); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the gated call wrote its task: %v", err)
	}
	mustAccept(t, ds, heldBy(t, ds, "adder", id))
	if got := mustGet(t, ds, taskType, id); got.Title != "held call" {
		t.Fatalf("the accepted request wrote %+v", got)
	}

	gatePolicy(t, ds, "refuse-adder", map[string]any{
		"selector": map[string]any{"functions": []any{fnPackage + "/adder"}},
		"action":   "refuse",
	})
	if _, _, err := ds.CallFunction(ctx, owner, fnPackage+"/adder", map[string]any{"title": "refused call"}); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("a refused call: %v, want forbidden", err)
	}
	if _, err := ds.Get(ctx, taskType, "call-refused-call"); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("a refused call wrote its task: %v", err)
	}
}

// TestPolicyFunctionsArmNamesADeclaredFunction: the write door refuses a
// `functions` entry that can never match, a bare name, an undeclared
// reference or a host function, because such a gate reads as closed while
// holding nothing.
func TestPolicyFunctionsArmNamesADeclaredFunction(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newFnDataset(t, nil, adderFn())
	for name, fn := range map[string]string{
		"bare":       "adder",
		"undeclared": fnPackage + "/missing",
		"host":       vocabulary.HostFunctionWrite,
	} {
		if _, err := ds.Put(ctx, owner, substrate.PutInput{
			Kind: vocabulary.KindRecordPatchPolicy, ID: "gate-" + name,
			Properties: map[string]any{
				"selector": map[string]any{"functions": []any{fn}},
				"action":   "gate",
			},
		}); !errors.Is(err, substrate.ErrValidation) {
			t.Fatalf("%s: admitted %q: %v", name, fn, err)
		}
	}
}
