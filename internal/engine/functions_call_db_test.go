package engine_test

// The callable contract: input/output schemas hold call-mode
// arguments to their shape, the invocation API applies effects under the
// function's actor with no cursor motion, a body's host Call is gated on
// `capabilities.call` and lands its callee's effects in the CALLER's
// delivery transaction — and the run-ledger retention keeps failures while
// pruning successes.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
)

// adderFn is a pure callable with declared shapes: takes {title}, writes one
// task, answers {id}.
func adderFn() map[string]any {
	return pyFn("adder", map[string]any{
		"input": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title": map[string]any{"type": "string"},
			},
			"required": []any{"title"},
		},
		"output": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "string"},
			},
		},
	}, []any{taskType}, `
def main(input, host):
    title = input["args"]["title"]
    tid = "call-" + title.replace(" ", "-")
    return {"effects": [{"action": "put", "kind": "tasks.substrate.reamde.dev/task",
                         "id": tid, "properties": {"title": title}}],
            "output": {"id": tid}}
`)
}

func TestCallModeValidatesAndApplies(t *testing.T) {
	ds, ops := newFnDataset(t, nil, adderFn())
	ctx := context.Background()

	// A valid call: input passes the schema, effects apply under the
	// function's actor, the output comes back shaped.
	out, effects, err := ops.CallFunction(ctx, fnAuthority+"/adder", map[string]any{"title": "from a call"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if effects != 1 {
		t.Fatalf("effects applied: %d", effects)
	}
	id, _ := out.(map[string]any)["id"].(string)
	if got := mustGet(t, ds, taskType, id); got.Title != "from a call" {
		t.Fatalf("call effect: %+v", got)
	}
	rows := actorChanges(t, ds, fnAuthority+"/adder")
	if len(rows) != 1 || rows[0].RecordID != id {
		t.Fatalf("call attribution: %+v", rows)
	}
	// Direct invocations mint nothing on the run ledger.
	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{runType}}, First: 10})
	if err != nil || len(page.Records) != 0 {
		t.Fatalf("a call minted run rows: %+v %v", page.Records, err)
	}

	// Input outside the declared shape refuses BEFORE the body runs.
	for name, args := range map[string]any{
		"missing required": map[string]any{},
		"mistyped":         map[string]any{"title": 4.0},
		"undeclared key":   map[string]any{"title": "x", "extra": true},
		"not an object":    "just a string",
	} {
		if _, _, err := ops.CallFunction(ctx, fnAuthority+"/adder", args); err == nil {
			t.Fatalf("%s: invalid input accepted", name)
		}
	}
}

func TestHostCallGatingAndCallerTransaction(t *testing.T) {
	// outer Calls adder during a trigger delivery: the grant is
	// capabilities.call, the callee's effects ride the CALLER's delivery
	// transaction (they land and roll back with it), and an ungranted
	// caller parks deterministically.
	outer := func(name string, granted bool, ownEffect string) map[string]any {
		data := map[string]any{}
		if granted {
			data["capabilities"] = map[string]any{"call": []any{fnAuthority + "/adder"}}
		}
		return pyFn(name, data, []any{taskType}, `
def main(input, host):
    e = input["envelope"]["record"]
    out = host.call("`+fnAuthority+`/adder", {"title": e["properties"]["name"]})
    return {"effects": [`+ownEffect+`], "output": out}
`)
	}
	ownOK := `{"action": "put", "kind": "tasks.substrate.reamde.dev/task",
               "id": "own-" + e["id"], "properties": {"title": "own " + out["id"]}}`
	ownBroken := `{"action": "patch", "kind": "tasks.substrate.reamde.dev/task",
                   "id": "missing-" + e["id"], "properties": {"title": "x"}}`

	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{
			trigOn("granted", map[string]any{"kinds": []any{widgetType}, "ops": []any{"create"}, "when": `record != null && record.properties.assignee == "granted"`}),
			trigOn("broken", map[string]any{"kinds": []any{widgetType}, "ops": []any{"create"}, "when": `record != null && record.properties.assignee == "broken"`}),
			trigOn("ungranted", map[string]any{"kinds": []any{widgetType}, "ops": []any{"create"}, "when": `record != null && record.properties.assignee == "ungranted"`}),
		},
		adderFn(),
		outer("granted", true, ownOK),
		outer("broken", true, ownBroken),
		outer("ungranted", false, ownOK),
	)
	ctx := context.Background()

	// Granted: one delivery applies the callee's put AND the caller's own.
	w := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "wired", "assignee": "granted"}})
	process(t, ops)
	if got := mustGet(t, ds, taskType, "call-wired"); got.Title != "wired" {
		t.Fatalf("callee effect missing: %+v", got)
	}
	if got := mustGet(t, ds, taskType, "own-"+w.ID); !strings.HasPrefix(got.Title, "own call-") {
		t.Fatalf("caller effect missing: %+v", got)
	}
	// Both landed in the CALLER's delivery: one transaction, one causal
	// parent — the callee wrote no delivery of its own.
	if parked, err := ops.TriggerFailures(ctx, trigID("granted")); err != nil || len(parked) != 0 {
		t.Fatalf("granted parked: %+v %v", parked, err)
	}

	// Broken caller: its OWN effect fails, so the callee's put must roll
	// back with the delivery — sub-call effects are not a separate commit.
	mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "halfway", "assignee": "broken"}})
	process(t, ops)
	if _, err := ds.Get(ctx, taskType, "call-halfway"); err == nil {
		t.Fatal("a sub-call effect survived its caller's rollback")
	}
	if parked, err := ops.TriggerFailures(ctx, trigID("broken")); err != nil || len(parked) != 1 {
		t.Fatalf("broken parked: %+v %v", parked, err)
	}

	// Ungranted: the call trips the allowlist, deterministically — one
	// attempt, parked, nothing applied.
	mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "sneaky", "assignee": "ungranted"}})
	process(t, ops)
	parked, err := ops.TriggerFailures(ctx, trigID("ungranted"))
	if err != nil || len(parked) != 1 {
		t.Fatalf("ungranted parked: %+v %v", parked, err)
	}
	if parked[0].Attempts != 1 || !strings.Contains(parked[0].LastError, "call allowlist") {
		t.Fatalf("ungranted park: %+v", parked[0])
	}
	if _, err := ds.Get(ctx, taskType, "call-sneaky"); err == nil {
		t.Fatal("an ungranted call applied effects")
	}
}

// Review W2 #2: A calls B, B calls C (which succeeds and emits an effect),
// then B fails — target error, effect decode error or output validation
// error. A CATCHES the failure and returns success. C's effects must have
// been truncated with B's failure: only A's own effect applies.
func TestFailedCalleeLeavesNoDescendantEffects(t *testing.T) {
	leaf := pyFn("leaf", map[string]any{}, []any{taskType}, `
def main(input, host):
    return {"effects": [{"action": "put", "kind": "tasks.substrate.reamde.dev/task",
                         "id": "c-effect", "properties": {"title": "from c"}}]}
`)
	mid := func(name, after string) map[string]any {
		return pyFn(name, map[string]any{
			"capabilities": map[string]any{"call": []any{fnAuthority + "/leaf"}},
			"output": map[string]any{
				"type":       "object",
				"properties": map[string]any{"ok": map[string]any{"type": "boolean"}},
			},
		}, []any{taskType}, `
def main(input, host):
    host.call("`+fnAuthority+`/leaf", None)
    `+after+`
`)
	}
	caller := pyFn("catcher", map[string]any{
		"capabilities": map[string]any{"call": []any{
			fnAuthority + "/raiser", fnAuthority + "/badeffect", fnAuthority + "/badoutput",
		}},
		"input": map[string]any{
			"type":       "object",
			"properties": map[string]any{"mid": map[string]any{"type": "string"}},
		},
	}, []any{taskType}, `
def main(input, host):
    mid = input["args"]["mid"]
    try:
        host.call("`+fnAuthority+`/" + mid, None)
    except Exception:
        pass
    return {"effects": [{"action": "put", "kind": "tasks.substrate.reamde.dev/task",
                         "id": "a-" + mid, "properties": {"title": "a survived " + mid}}]}
`)
	ds, ops := newFnDataset(t, nil,
		leaf,
		mid("raiser", `raise Exception("b explodes")`),
		mid("badeffect", `return {"effects": [{"action": "conjure", "kind": "tasks.substrate.reamde.dev/task", "id": "x"}], "output": {"ok": True}}`),
		mid("badoutput", `return {"output": "not the declared object"}`),
		caller,
	)
	ctx := context.Background()

	for _, mid := range []string{"raiser", "badeffect", "badoutput"} {
		if _, _, err := ops.CallFunction(ctx, fnAuthority+"/catcher", map[string]any{"mid": mid}); err != nil {
			t.Fatalf("%s: the catching caller failed: %v", mid, err)
		}
		if _, err := ds.Get(ctx, taskType, "a-"+mid); err != nil {
			t.Fatalf("%s: the caller's own effect is lost: %v", mid, err)
		}
		if _, err := ds.Get(ctx, taskType, "c-effect"); err == nil {
			t.Fatalf("%s: the failed callee's descendant effect leaked into the caller", mid)
		}
	}
}

// Review W2 #8: a declared `output:` validates even an omitted or null
// answer, on BOTH call paths; `any` alone stays open to nil.
func TestDeclaredOutputRefusesNil(t *testing.T) {
	shaped := map[string]any{
		"type":       "object",
		"properties": map[string]any{"id": map[string]any{"type": "string"}},
	}
	silent := pyFn("silent", map[string]any{"output": shaped}, []any{taskType}, `
def main(input, host):
    return {"effects": [{"action": "put", "kind": "tasks.substrate.reamde.dev/task",
                         "id": "silent-effect", "properties": {"title": "x"}}]}
`)
	nuller := pyFn("nuller", map[string]any{"output": shaped}, []any{taskType}, `
def main(input, host):
    return {"output": None}
`)
	anyOut := pyFn("anyout", map[string]any{"output": map[string]any{"type": "any"}}, []any{taskType}, `
def main(input, host):
    return {}
`)
	nestCaller := pyFn("nestcaller", map[string]any{
		"capabilities": map[string]any{"call": []any{fnAuthority + "/silent"}},
	}, []any{taskType}, `
def main(input, host):
    out = host.call("`+fnAuthority+`/silent", None)
    return {"output": out}
`)
	ds, ops := newFnDataset(t, nil, silent, nuller, anyOut, nestCaller)
	ctx := context.Background()

	// Top level: omitted and explicit-null both refuse, and no effects land.
	if _, _, err := ops.CallFunction(ctx, fnAuthority+"/silent", nil); err == nil ||
		!strings.Contains(err.Error(), "output") {
		t.Fatalf("an omitted output passed the declared shape: %v", err)
	}
	if _, err := ds.Get(ctx, taskType, "silent-effect"); err == nil {
		t.Fatal("effects applied under a refused output")
	}
	if _, _, err := ops.CallFunction(ctx, fnAuthority+"/nuller", nil); err == nil ||
		!strings.Contains(err.Error(), "output") {
		t.Fatalf("an explicit null passed the declared shape: %v", err)
	}
	// `any` stays open.
	if _, _, err := ops.CallFunction(ctx, fnAuthority+"/anyout", nil); err != nil {
		t.Fatalf("any refused nil: %v", err)
	}
	// Nested: the host Call surfaces the same violation to the caller.
	if _, _, err := ops.CallFunction(ctx, fnAuthority+"/nestcaller", nil); err == nil ||
		!strings.Contains(err.Error(), "output") {
		t.Fatalf("a nested omitted output passed: %v", err)
	}
	if _, err := ds.Get(ctx, taskType, "silent-effect"); err == nil {
		t.Fatal("the nested callee's effects leaked past its output violation")
	}
}

// Review W2 #9: two calls to the SAME callee in one root invocation carry
// two distinct idempotency keys — the caller's stack path plus a
// per-invocation call ordinal — so an external deduper never folds them.
func TestRepeatedSubCallsGetDistinctIdempotencyKeys(t *testing.T) {
	echo := pyFn("echo", map[string]any{}, []any{taskType}, `
def main(input, host):
    return {"output": input["idempotencyKey"]}
`)
	twice := pyFn("twice", map[string]any{
		"capabilities": map[string]any{"call": []any{fnAuthority + "/echo"}},
	}, []any{taskType}, `
def main(input, host):
    k1 = host.call("`+fnAuthority+`/echo", {"n": 1})
    k2 = host.call("`+fnAuthority+`/echo", {"n": 2})
    return {"output": [k1, k2]}
`)
	_, ops := newFnDataset(t, nil, echo, twice)
	out, _, err := ops.CallFunction(context.Background(), fnAuthority+"/twice", nil)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	keys, _ := out.([]any)
	if len(keys) != 2 {
		t.Fatalf("output: %+v", out)
	}
	k1, _ := keys[0].(string)
	k2, _ := keys[1].(string)
	if k1 == k2 {
		t.Fatalf("two calls to one callee share the key %q", k1)
	}
	if !strings.Contains(k1, "/call/1/"+fnAuthority+"/echo") || !strings.Contains(k2, "/call/2/"+fnAuthority+"/echo") {
		t.Fatalf("keys lack the stack path + ordinal: %q %q", k1, k2)
	}
}

func TestRunRetentionKeepsFailuresPrunesSuccesses(t *testing.T) {
	// The ledger's cleanup policy: parked runs are kept, ok/skipped runs
	// prune to the newest runRetention (20) per trigger.
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{trigOn("mirror", map[string]any{"kinds": []any{widgetType}})},
		pyFn("mirror", map[string]any{}, []any{taskType}, mirrorSource))
	ctx := context.Background()

	// One poisoned delivery parks (kept forever)…
	poisoned := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType})
	process(t, ops)

	// …then 24 healthy deliveries, each an ok run.
	w := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "v0"}})
	process(t, ops)
	for i := 1; i < 23; i++ {
		mustPatch(t, ds, fnActor, w.Kind, w.ID, substrate.PatchInput{Properties: map[string]any{"name": "v" + string(rune('a'+i))}})
		process(t, ops)
	}

	oks := runRowsOf(t, ds, trigID("mirror"), "ok")
	if len(oks) != 20 {
		t.Fatalf("ok runs after 24 deliveries: %d, want the newest 20", len(oks))
	}
	parked := runRowsOf(t, ds, trigID("mirror"), "parked")
	if len(parked) != 1 {
		t.Fatalf("parked runs: %d — failures are kept", len(parked))
	}
	if parked[0].Properties["record"] != poisoned.ID {
		t.Fatalf("parked run names %v, want %s", parked[0].Properties["record"], poisoned.ID)
	}
	if _, err := ops.TriggerFailures(ctx, trigID("mirror")); err != nil {
		t.Fatalf("failures: %v", err)
	}
}
