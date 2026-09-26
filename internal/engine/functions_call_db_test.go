package engine_test

// The callable contract: input/output schemas hold call-mode
// arguments to their shape, the invocation API applies effects under the
// function's actor with no cursor motion, a body's host Call is gated on
// `permissions.call` and lands its callee's effects in the CALLER's
// delivery transaction — and the run-ledger retention keeps failures while
// pruning successes.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
)

// A body that raises is a server-side execution fault, not invalid caller
// input: it surfaces as ErrFunctionFault (500 function_failed), while
// arguments that fail the declared input schema stay ErrValidation (422).
func TestBodyFaultIsNotValidation(t *testing.T) {
	t.Parallel()
	boom := pyFn("boom", map[string]any{
		"arguments": []any{
			map[string]any{"name": "title", "type": "string", "required": true},
		},
	}, []any{taskType}, `
def main(input, host):
    raise Exception("body exploded")
`)
	ds := newFnDataset(t, nil, boom)
	ctx := context.Background()

	// Valid input, faulting body: the run reached the body and it raised.
	_, _, err := ds.CallFunction(ctx, substrate.ActorAPI, fnPackage+"/boom", map[string]any{"title": "ok"})
	if err == nil {
		t.Fatal("a raising body returned no error")
	}
	if !errors.Is(err, substrate.ErrFunctionFault) {
		t.Fatalf("body fault is %v, want ErrFunctionFault", err)
	}
	if errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("body fault still matches ErrValidation: %v", err)
	}

	// Missing the required argument refuses BEFORE the body runs, as validation.
	_, _, err = ds.CallFunction(ctx, substrate.ActorAPI, fnPackage+"/boom", map[string]any{})
	if !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("missing required input is %v, want ErrValidation", err)
	}
	if errors.Is(err, substrate.ErrFunctionFault) {
		t.Fatalf("input validation leaked the function-fault code: %v", err)
	}
}

// adderFn is a pure callable with declared shapes: takes {title}, writes one
// task, answers {id}.
func adderFn() map[string]any {
	return pyFn("adder", map[string]any{
		"arguments": []any{
			map[string]any{"name": "title", "type": "string", "required": true},
		},
		"returns": []any{map[string]any{"name": "id", "type": "string"}},
	}, []any{taskType}, `
def main(input, host):
    title = input["args"]["title"]
    tid = "call-" + title.replace(" ", "-")
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": tid, "properties": {"name": title}}],
            "output": {"id": tid}}
`)
}

func TestCallModeValidatesAndApplies(t *testing.T) {
	t.Parallel()
	ds := newFnDataset(t, nil, adderFn())
	ctx := context.Background()

	// A valid call: input passes the schema, effects apply under the
	// function's actor, the output comes back shaped.
	out, effects, err := ds.CallFunction(ctx, substrate.ActorAPI, fnPackage+"/adder", map[string]any{"title": "from a call"})
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
	rows := actorChanges(t, ds, fnPackage+"/adder")
	if len(rows) != 1 || rows[0].RecordID != id {
		t.Fatalf("call attribution: %+v", rows)
	}
	// A direct call of a function without a network grant mints nothing on
	// the run ledger: its effects are its whole trace (decision record 0106).
	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{triggerRunType}}, First: 10})
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
		if _, _, err := ds.CallFunction(ctx, substrate.ActorAPI, fnPackage+"/adder", args); err == nil {
			t.Fatalf("%s: invalid input accepted", name)
		}
	}
}

// networkedFn is sender, a function that declares permissions.network and
// behaves by its input: {"title": t} writes one task and answers {"sent": t},
// {"big": n} answers n bytes and writes nothing, {"fail": true} raises.
func networkedFn() map[string]any {
	fn := pyFn("sender", map[string]any{}, []any{taskType}, `
def main(input, host):
    args = input["args"]
    if args.get("fail"):
        raise Exception("the provider refused the send")
    if "big" in args:
        return {"output": {"blob": "x" * args["big"]}}
    tid = "sent-" + args["title"]
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": tid, "properties": {"name": args["title"]}}],
            "output": {"sent": args["title"]}}
`)
	fnPermissions(fn["data"].(map[string]any))["network"] = []any{"api.example.com"}
	return fn
}

// callRuns lists the run ledger's call-mode rows, oldest first.
func callRuns(t *testing.T, ds substrate.Dataset) []*substrate.Record {
	t.Helper()
	page, err := ds.List(context.Background(), substrate.Query{
		Filter:  substrate.Filter{Kinds: []string{triggerRunType}},
		OrderBy: []substrate.Order{{Property: "createdAt"}},
		First:   50,
	})
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	var out []*substrate.Record
	for _, r := range page.Records {
		if r.Properties["mode"] == "call" {
			out = append(out, r)
		}
	}
	return out
}

// A direct call of a networked function leaves one run row (#645): the
// callable, the caller, the token, the time, the status and the output, in
// the commit that applies its effects. A failed body leaves a failed row, and
// an output past the cap lands as its size alone.
func TestNetworkedCallWritesRunRow(t *testing.T) {
	t.Parallel()
	// relay has no network grant of its own and calls sender, which does.
	relay := pyFn("relay", map[string]any{}, []any{taskType}, `
def main(input, host):
    return {"output": host.call("`+fnPackage+`/sender", input["args"])}
`)
	fnPermissions(relay["data"].(map[string]any))["call"] = []any{fnPackage + "/sender"}
	ds := newFnDataset(t, nil, networkedFn(), relay)
	ctx := substrate.WithPrincipal(context.Background(), "tok-645")
	name := fnPackage + "/sender"

	if _, n, err := ds.CallFunction(ctx, substrate.ActorConsole, name, map[string]any{"title": "hello"}); err != nil || n != 1 {
		t.Fatalf("call: %d %v", n, err)
	}
	runs := callRuns(t, ds)
	if len(runs) != 1 {
		t.Fatalf("call runs after one call: %d", len(runs))
	}
	p := runs[0].Properties
	want := map[string]any{
		"status": "ok", "caller": "console", "principal": "tok-645",
		"callable": name,
	}
	for k, v := range want {
		if p[k] != v {
			t.Fatalf("run %s = %v, want %v (%+v)", k, p[k], v, p)
		}
	}
	if ref, _ := p["callableRef"].(map[string]any); ref["ref"] != "substrate.reamde.dev/core/function/"+name {
		t.Fatalf("run callableRef: %+v", p["callableRef"])
	}
	if _, ok := p["trigger"]; ok {
		t.Fatalf("a call run names a trigger: %+v", p)
	}
	if out, _ := p["output"].(map[string]any); out["sent"] != "hello" {
		t.Fatalf("run output: %+v", p["output"])
	}
	if eff, _ := p["effects"].(map[string]any); fmt.Sprint(eff["put"]) != "1" {
		t.Fatalf("run effects: %+v", p["effects"])
	}
	if p["startedAt"] == nil || p["finishedAt"] == nil {
		t.Fatalf("run times: %+v", p)
	}

	// A large output keeps its size and not its value; no effects, one row.
	if _, _, err := ds.CallFunction(ctx, substrate.ActorAPI, name, map[string]any{"big": 5000}); err != nil {
		t.Fatalf("big call: %v", err)
	}
	// A body that raises: the call fails and its run row says so.
	if _, _, err := ds.CallFunction(ctx, substrate.ActorAPI, name, map[string]any{"fail": true}); !errors.Is(err, substrate.ErrFunctionFault) {
		t.Fatalf("failing call: %v", err)
	}
	// A call of relay reaches the network through sender: one row, relay's.
	if _, n, err := ds.CallFunction(ctx, substrate.ActorAPI, fnPackage+"/relay", map[string]any{"title": "via"}); err != nil || n != 1 {
		t.Fatalf("relay call: %d %v", n, err)
	}
	runs = callRuns(t, ds)
	if len(runs) != 4 {
		t.Fatalf("call runs after four calls: %d", len(runs))
	}
	if got := runs[3].Properties["callable"]; got != fnPackage+"/relay" {
		t.Fatalf("relay run callable: %v", got)
	}
	big, bad := runs[1].Properties, runs[2].Properties
	if _, kept := big["output"]; kept || big["status"] != "ok" {
		t.Fatalf("big run: %+v", big)
	}
	if n, err := strconv.Atoi(fmt.Sprint(big["outputBytes"])); err != nil || n <= 5000 {
		t.Fatalf("big run outputBytes: %v", big["outputBytes"])
	}
	reason, _ := bad["reason"].(string)
	if bad["status"] != "failed" || !strings.Contains(reason, "the provider refused the send") {
		t.Fatalf("failed run: %+v", bad)
	}
}

// The edges of a networked call's run row (#645): a call refused on its input
// never ran and writes none, an output outside `returns:` writes a failed row,
// and a keyed call replayed under the same Idempotency-Key writes one row.
func TestNetworkedCallRunRowEdges(t *testing.T) {
	t.Parallel()
	shaped := []any{map[string]any{"name": "sent", "type": "string", "required": true}}
	fn := pyFn("shaped", map[string]any{
		"arguments": []any{map[string]any{"name": "title", "type": "string", "required": true}},
		"returns":   shaped,
	}, []any{taskType}, `
def main(input, host):
    title = input["args"]["title"]
    if title == "wrong":
        return {"output": {"sent": 5}}
    return {"output": {"sent": title}}
`)
	fnPermissions(fn["data"].(map[string]any))["network"] = []any{"api.example.com"}
	ds := newFnDataset(t, nil, fn)
	name := fnPackage + "/shaped"

	if _, _, err := ds.CallFunction(context.Background(), substrate.ActorAPI, name, map[string]any{"title": 5}); err == nil {
		t.Fatal("a call with input outside `arguments:` ran")
	}
	if runs := callRuns(t, ds); len(runs) != 0 {
		t.Fatalf("a call refused on its input wrote a run row: %+v", runs)
	}

	if _, _, err := ds.CallFunction(context.Background(), substrate.ActorAPI, name, map[string]any{"title": "wrong"}); err == nil {
		t.Fatal("an output outside `returns:` passed")
	}
	runs := callRuns(t, ds)
	if len(runs) != 1 || runs[0].Properties["status"] != "failed" {
		t.Fatalf("an output outside `returns:` left no failed row: %+v", runs)
	}

	ctx := keyed("call-645-replay")
	for i := range 2 {
		out, _, err := ds.CallFunction(ctx, substrate.ActorAPI, name, map[string]any{"title": "once"})
		if err != nil {
			t.Fatalf("keyed call %d: %v", i, err)
		}
		if got, _ := out.(map[string]any); got["sent"] != "once" {
			t.Fatalf("keyed call %d output: %+v", i, out)
		}
	}
	if runs := callRuns(t, ds); len(runs) != 2 {
		t.Fatalf("a replayed keyed call wrote another row: %d call runs, want 2", len(runs))
	}

	// An output no row stores (a NUL) still settles: the row keeps its size.
	out, _, err := ds.CallFunction(context.Background(), substrate.ActorAPI, name, map[string]any{"title": "a\x00b"})
	if err != nil {
		t.Fatalf("a call whose output carries a NUL failed: %v", err)
	}
	if got, _ := out.(map[string]any); got["sent"] != "a\x00b" {
		t.Fatalf("NUL call output: %+v", out)
	}
	runs = callRuns(t, ds)
	if len(runs) != 3 {
		t.Fatalf("the NUL call wrote %d call runs in all, want 3", len(runs))
	}
	var bare []map[string]any
	for _, r := range runs {
		if _, kept := r.Properties["output"]; !kept && r.Properties["status"] == "ok" {
			bare = append(bare, r.Properties)
		}
	}
	if len(bare) != 1 || bare[0]["outputBytes"] == nil {
		t.Fatalf("want one ok row with outputBytes and no output (the NUL call's), got %+v", bare)
	}
}

func TestHostCallGatingAndCallerTransaction(t *testing.T) {
	t.Parallel()
	// outer Calls adder during a trigger delivery: the grant is
	// permissions.call, the callee's effects ride the CALLER's delivery
	// transaction (they land and roll back with it), and an ungranted
	// caller parks deterministically.
	outer := func(name string, granted bool, ownEffect string) map[string]any {
		data := map[string]any{}
		if granted {
			fnPermissions(data)["call"] = []any{fnPackage + "/adder"}
		}
		return pyFn(name, data, []any{taskType}, `
def main(input, host):
    e = input["envelope"]["record"]
    out = host.call("`+fnPackage+`/adder", {"title": e["properties"]["name"]})
    return {"effects": [`+ownEffect+`], "output": out}
`)
	}
	ownOK := `{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
               "id": "own-" + e["id"], "properties": {"name": "own " + out["id"]}}`
	ownBroken := `{"action": "patch", "kind": "samples.substrate.reamde.dev/tasks/task",
                   "id": "missing-" + e["id"], "properties": {"name": "x"}}`

	ds := newFnDataset(t,
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
	process(t, ds)
	if got := mustGet(t, ds, taskType, "call-wired"); got.Title != "wired" {
		t.Fatalf("callee effect missing: %+v", got)
	}
	if got := mustGet(t, ds, taskType, "own-"+w.ID); !strings.HasPrefix(got.Title, "own call-") {
		t.Fatalf("caller effect missing: %+v", got)
	}
	// Both landed in the CALLER's delivery: one transaction, one causal
	// parent — the callee wrote no delivery of its own.
	if parked, err := ds.TriggerFailures(ctx, trigID("granted")); err != nil || len(parked) != 0 {
		t.Fatalf("granted parked: %+v %v", parked, err)
	}

	// Broken caller: its OWN effect fails, so the callee's put must roll
	// back with the delivery — sub-call effects are not a separate commit.
	mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "halfway", "assignee": "broken"}})
	process(t, ds)
	if _, err := ds.Get(ctx, taskType, "call-halfway"); err == nil {
		t.Fatal("a sub-call effect survived its caller's rollback")
	}
	if parked, err := ds.TriggerFailures(ctx, trigID("broken")); err != nil || len(parked) != 1 {
		t.Fatalf("broken parked: %+v %v", parked, err)
	}

	// Ungranted: the call trips the allowlist, deterministically — one
	// attempt, parked, nothing applied.
	mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "sneaky", "assignee": "ungranted"}})
	process(t, ds)
	parked, err := ds.TriggerFailures(ctx, trigID("ungranted"))
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

// A calls B, B calls C (which succeeds and emits an effect),
// then B fails — target error, effect decode error or output validation
// error. A CATCHES the failure and returns success. C's effects must have
// been truncated with B's failure: only A's own effect applies.
func TestFailedCalleeLeavesNoDescendantEffects(t *testing.T) {
	t.Parallel()
	leaf := pyFn("leaf", map[string]any{}, []any{taskType}, `
def main(input, host):
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": "c-effect", "properties": {"name": "from c"}}]}
`)
	mid := func(name, after string) map[string]any {
		return pyFn(name, map[string]any{
			"permissions": map[string]any{"call": []any{fnPackage + "/leaf"}},
			"returns":     []any{map[string]any{"name": "ok", "type": "bool"}},
		}, []any{taskType}, `
def main(input, host):
    host.call("`+fnPackage+`/leaf", None)
    `+after+`
`)
	}
	caller := pyFn("catcher", map[string]any{
		"permissions": map[string]any{"call": []any{
			fnPackage + "/raiser", fnPackage + "/badeffect", fnPackage + "/badoutput",
		}},
		"arguments": []any{map[string]any{"name": "mid", "type": "string"}},
	}, []any{taskType}, `
def main(input, host):
    mid = input["args"]["mid"]
    try:
        host.call("`+fnPackage+`/" + mid, None)
    except Exception:
        pass
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": "a-" + mid, "properties": {"name": "a survived " + mid}}]}
`)
	ds := newFnDataset(t, nil,
		leaf,
		mid("raiser", `raise Exception("b explodes")`),
		mid("badeffect", `return {"effects": [{"action": "conjure", "kind": "samples.substrate.reamde.dev/tasks/task", "id": "x"}], "output": {"ok": True}}`),
		mid("badoutput", `return {"output": "not the declared object"}`),
		caller,
	)
	ctx := context.Background()

	for _, mid := range []string{"raiser", "badeffect", "badoutput"} {
		if _, _, err := ds.CallFunction(ctx, substrate.ActorAPI, fnPackage+"/catcher", map[string]any{"mid": mid}); err != nil {
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

// A declared `returns:` validates even an omitted or null
// answer, on BOTH call paths; a function that declares no result side stays
// open to nil.
func TestDeclaredOutputRefusesNil(t *testing.T) {
	t.Parallel()
	shaped := []any{map[string]any{"name": "id", "type": "string", "required": true}}
	silent := pyFn("silent", map[string]any{"returns": shaped}, []any{taskType}, `
def main(input, host):
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": "silent-effect", "properties": {"name": "x"}}]}
`)
	nuller := pyFn("nuller", map[string]any{"returns": shaped}, []any{taskType}, `
def main(input, host):
    return {"output": None}
`)
	// An UNDECLARED result side constrains nothing, which is what stays open to
	// nil now that every declared one is a named argument list.
	anyOut := pyFn("anyout", map[string]any{}, []any{taskType}, `
def main(input, host):
    return {}
`)
	nestCaller := pyFn("nestcaller", map[string]any{
		"permissions": map[string]any{"call": []any{fnPackage + "/silent"}},
	}, []any{taskType}, `
def main(input, host):
    out = host.call("`+fnPackage+`/silent", None)
    return {"output": out}
`)
	ds := newFnDataset(t, nil, silent, nuller, anyOut, nestCaller)
	ctx := context.Background()

	// Top level: omitted and explicit-null both refuse, and no effects land.
	if _, _, err := ds.CallFunction(ctx, substrate.ActorAPI, fnPackage+"/silent", nil); err == nil ||
		!strings.Contains(err.Error(), "output") {
		t.Fatalf("an omitted output passed the declared shape: %v", err)
	}
	if _, err := ds.Get(ctx, taskType, "silent-effect"); err == nil {
		t.Fatal("effects applied under a refused output")
	}
	if _, _, err := ds.CallFunction(ctx, substrate.ActorAPI, fnPackage+"/nuller", nil); err == nil ||
		!strings.Contains(err.Error(), "output") {
		t.Fatalf("an explicit null passed the declared shape: %v", err)
	}
	// An undeclared result side stays open.
	if _, _, err := ds.CallFunction(ctx, substrate.ActorAPI, fnPackage+"/anyout", nil); err != nil {
		t.Fatalf("any refused nil: %v", err)
	}
	// Nested: the host Call surfaces the same violation to the caller.
	if _, _, err := ds.CallFunction(ctx, substrate.ActorAPI, fnPackage+"/nestcaller", nil); err == nil ||
		!strings.Contains(err.Error(), "output") {
		t.Fatalf("a nested omitted output passed: %v", err)
	}
	if _, err := ds.Get(ctx, taskType, "silent-effect"); err == nil {
		t.Fatal("the nested callee's effects leaked past its output violation")
	}
}

// Two calls to the SAME callee in one root invocation carry
// two distinct idempotency keys — the caller's stack path plus a
// per-invocation call ordinal — so an external deduper never folds them.
func TestRepeatedSubCallsGetDistinctIdempotencyKeys(t *testing.T) {
	t.Parallel()
	echo := pyFn("echo", map[string]any{}, []any{taskType}, `
def main(input, host):
    return {"output": input["idempotencyKey"]}
`)
	twice := pyFn("twice", map[string]any{
		"permissions": map[string]any{"call": []any{fnPackage + "/echo"}},
	}, []any{taskType}, `
def main(input, host):
    k1 = host.call("`+fnPackage+`/echo", {"n": 1})
    k2 = host.call("`+fnPackage+`/echo", {"n": 2})
    return {"output": [k1, k2]}
`)
	ds := newFnDataset(t, nil, echo, twice)
	out, _, err := ds.CallFunction(context.Background(), substrate.ActorAPI, fnPackage+"/twice", nil)
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
	if !strings.Contains(k1, "/call/1/"+fnPackage+"/echo") || !strings.Contains(k2, "/call/2/"+fnPackage+"/echo") {
		t.Fatalf("keys lack the stack path + ordinal: %q %q", k1, k2)
	}
}

func TestRunRetentionKeepsFailuresPrunesSuccesses(t *testing.T) {
	t.Parallel()
	// The ledger's cleanup policy: parked runs are kept, ok/skipped runs
	// prune to the newest runRetention (20) per trigger.
	ds := newFnDataset(t,
		[]enginetest.Trigger{trigOn("mirror", map[string]any{"kinds": []any{widgetType}})},
		pyFn("mirror", map[string]any{}, []any{taskType}, mirrorSource))
	ctx := context.Background()

	// One poisoned delivery parks (kept forever)…
	poisoned := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType})
	process(t, ds)

	// …then 24 healthy deliveries, each an ok run.
	w := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "v0"}})
	process(t, ds)
	for i := 1; i < 23; i++ {
		mustPatch(t, ds, fnActor, w.Kind, w.ID, substrate.PatchInput{Properties: map[string]any{"name": "v" + string(rune('a'+i))}})
		process(t, ds)
	}

	oks := runRowsOf(t, ds, trigID("mirror"), "ok")
	if len(oks) != 20 {
		t.Fatalf("ok runs after 24 deliveries: %d, want the newest 20", len(oks))
	}
	parked := runRowsOf(t, ds, trigID("mirror"), "parked")
	if len(parked) != 1 {
		t.Fatalf("parked runs: %d — failures are kept", len(parked))
	}
	wantRecord := poisoned.ID
	if parked[0].Properties["record"] != wantRecord {
		t.Fatalf("parked run names %v, want %s", parked[0].Properties["record"], wantRecord)
	}
	if _, err := ds.TriggerFailures(ctx, trigID("mirror")); err != nil {
		t.Fatalf("failures: %v", err)
	}
}
