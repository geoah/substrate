package engine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The shared runner's acceptance gate: the Go ingestion path (compile at
// registration, cached binary, same protocol), the capability-scoped host
// reads with their allowlist and budget parking immediately, per-invocation
// timeouts riding the retries, the upgraded effect vocabulary — ifAbsent,
// former-id resolution, link/unlink, capability-gated merge/split — and the
// callable contract: input/output schemas, mode call, host Call with its
// gates.

func onlyParked(t *testing.T, ops fnOps, triggerID string) substrate.TriggerFailure {
	t.Helper()
	parked, err := ops.TriggerFailures(context.Background(), triggerID)
	if err != nil {
		t.Fatalf("failures: %v", err)
	}
	if len(parked) != 1 {
		t.Fatalf("parked rows: %+v", parked)
	}
	return parked[0]
}

// goFn renders a `runtime: go` function manifest.
func goFn(name string, data map[string]any, writes []any, source string) map[string]any {
	data["runtime"] = vocabulary.RuntimeGo
	data["source"] = source
	fnPermissions(data)["writes"] = writes
	return fnDoc(name, data)
}

func TestTriggerGoRuntime(t *testing.T) {
	t.Parallel()
	// One inline Go body: compiled at registration against the embedded
	// substratefn SDK, run as a supervised subprocess on the same protocol —
	// effects, logs and errors ride exactly the python path.
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{trigOn("gomirror", map[string]any{"kinds": []any{widgetType}})},
		goFn("gomirror", map[string]any{}, []any{taskType}, `
import "substratefn.local/substratefn"

func Main(in *substratefn.Input, host *substratefn.Host) (*substratefn.Result, error) {
	c := in.Envelope.Change
	name, _ := in.Envelope.Record.Properties["name"].(string)
	host.Logf("mirroring %s", c.ID)
	return &substratefn.Result{Effects: []substratefn.Effect{{
		Action: "put", Kind: "tasks.substrate.reamde.dev/task", ID: "t-" + c.ID,
		Properties: map[string]any{"name": name},
	}}}, nil
}
`))

	w := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "compiled"}})
	process(t, ops)
	if got := mustGet(t, ds, taskType, "t-"+w.ID); got.Title != "compiled" {
		t.Fatalf("go task title: %q", got.Title)
	}
	// Idempotent replay holds for the compiled arm too.
	before := dataSeq(t, ds)
	if err := ops.ReplayTrigger(context.Background(), trigID("gomirror"), 0); err != nil {
		t.Fatalf("replay: %v", err)
	}
	process(t, ops)
	if after := dataSeq(t, ds); after != before {
		t.Fatalf("replay wrote data: seq %d → %d", before, after)
	}
}

// readerSource exercises every host read; the widget's own properties select
// the scenario.
const readerSource = `
def main(input, host):
    e = input["envelope"]["record"]
    p = e["properties"]
    mode = p.get("mode")
    if mode == "get":
        got = host.get("widgets.test.dev/widget", p["target"])
        title = got["kind"] if got else "missing"
    elif mode == "list":
        page = host.list(filter={"kinds": ["widgets.test.dev/widget"],
                                 "properties": {"name": {"eq": p["match"]}}}, first=10)
        title = str(len(page.get("records") or []))
    elif mode == "search":
        hits = host.search(q=p["q"], mode="lexical", kinds=["widgets.test.dev/widget"], k=5)
        title = str(len(hits))
    elif mode == "forbidden":
        host.list(filter={"kinds": ["tasks.substrate.reamde.dev/task"]})
        title = "never"
    elif mode == "burn":
        for i in range(int(p["calls"])):
            host.get("widgets.test.dev/widget", "nothing-" + str(i))
        title = "never"
    else:
        return {}
    return {"effects": [{"action": "put", "kind": "tasks.substrate.reamde.dev/task",
                         "id": "t-" + e["id"], "properties": {"name": title}}]}
`

func readerFn(name string, tweak func(map[string]any)) map[string]any {
	data := map[string]any{
		"permissions": map[string]any{"reads": map[string]any{"kinds": []any{widgetType}}},
	}
	if tweak != nil {
		tweak(data)
	}
	return pyFn(name, data, []any{taskType}, readerSource)
}

func readerTrigger(name string) enginetest.Trigger {
	return trigOn(name, map[string]any{"kinds": []any{widgetType}, "ops": []any{"create", "update"}})
}

func TestTriggerHostReads(t *testing.T) {
	t.Parallel()
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{readerTrigger("reader")},
		readerFn("reader", nil))

	base := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "anchor"}})
	process(t, ops)

	// Get resolves through the ordinary read surface; absence is None.
	found := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"mode": "get", "target": base.ID}})
	missing := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"mode": "get", "target": "nope"}})
	// List rides the filter grammar; Search rides the lexical arm.
	listed := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"mode": "list", "match": "anchor"}})
	searched := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"mode": "search", "q": "anchor"}})
	process(t, ops)

	for id, want := range map[string]string{
		"t-" + found.ID:    widgetType,
		"t-" + missing.ID:  "missing",
		"t-" + listed.ID:   "1",
		"t-" + searched.ID: "1",
	} {
		if got := mustGet(t, ds, taskType, id); got.Title != want {
			t.Fatalf("%s: %q, want %q", id, got.Title, want)
		}
	}
}

func TestTriggerReadAllowlistViolationParksImmediately(t *testing.T) {
	t.Parallel()
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{readerTrigger("fenced")},
		readerFn("fenced", nil))
	ctx := context.Background()

	s := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"mode": "forbidden"}})
	process(t, ops)

	failure := onlyParked(t, ops, trigID("fenced"))
	if failure.RecordID != s.ID || !strings.Contains(failure.LastError, "allowlist") {
		t.Fatalf("parked: %+v", failure)
	}
	// Deterministic: one attempt, no retries burned (the errCausalDepth
	// precedent).
	if failure.Attempts != 1 {
		t.Fatalf("attempts: %d", failure.Attempts)
	}
	if _, err := ds.Get(ctx, taskType, "t-"+s.ID); err == nil {
		t.Fatal("effects applied after a violation")
	}
	if st := statusOf(t, ops, trigID("fenced")); st.Lag != 0 {
		t.Fatalf("park did not advance: %+v", st)
	}
}

func TestTriggerReadBudgetTripParksImmediately(t *testing.T) {
	t.Parallel()
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{readerTrigger("thrifty")},
		readerFn("thrifty", func(data map[string]any) {
			fnPermissions(data)["reads"].(map[string]any)["budgets"] = map[string]any{"calls": 2}
		}))

	mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"mode": "burn", "calls": float64(5)}})
	process(t, ops)

	failure := onlyParked(t, ops, trigID("thrifty"))
	if failure.Attempts != 1 || !strings.Contains(failure.LastError, "budget") {
		t.Fatalf("parked: %+v", failure)
	}
}

func TestTriggerTimeoutRetriesThenParks(t *testing.T) {
	t.Parallel()
	// A stuck body blows the manifest timeout; the runner kills and restarts
	// the child, and the delivery rides the full attempts before parking —
	// a wall timeout is not deterministic (load could clear it).
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{trigOn("stuck", map[string]any{"kinds": []any{widgetType}})},
		pyFn("stuck", map[string]any{"timeoutMs": 250}, []any{taskType}, `
import time
def main(input, host):
    time.sleep(30)
    return {}
`))

	mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType})
	process(t, ops)

	failure := onlyParked(t, ops, trigID("stuck"))
	if failure.Attempts != 3 || !strings.Contains(failure.LastError, "exceeded") {
		t.Fatalf("parked: %+v", failure)
	}
	// The killed host restarted and re-registered: a healthy neighbor of the
	// same runtime still delivers.
	if st := statusOf(t, ops, trigID("stuck")); st.Lag != 0 {
		t.Fatalf("park did not advance: %+v", st)
	}
}

// mintSource is the put-if-absent fixture: it (re-)mints a task for every
// widget write, create-only.
const mintSource = `
def main(input, host):
    c = input["envelope"]["change"]
    return {"effects": [{"action": "put", "ifAbsent": True, "kind": "tasks.substrate.reamde.dev/task",
                         "id": "mint-" + c["id"], "properties": {"name": "pending"}}]}
`

func TestTriggerEffectPutIfAbsent(t *testing.T) {
	t.Parallel()
	// The prototype's clobber finding: a minting function must never reset
	// state owned by later stages — with ifAbsent, re-mention and replay are
	// no-ops.
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{trigOn("mint", map[string]any{"kinds": []any{widgetType}})},
		pyFn("mint", map[string]any{}, []any{taskType}, mintSource))
	ctx := context.Background()

	w := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "a"}})
	process(t, ops)
	if got := mustGet(t, ds, taskType, "mint-"+w.ID); got.Title != "pending" {
		t.Fatalf("minted title: %q", got.Title)
	}
	// A later stage takes the task over.
	mustPatch(t, ds, owner, taskType, "mint-"+w.ID, substrate.PatchInput{Properties: map[string]any{"name": "owned downstream"}})

	// Re-mention: the widget changes again, the mint re-fires, the task is
	// untouched and nothing lands in the changelog under the function.
	rows := len(actorChanges(t, ds, fnAuthority+"/mint"))
	mustPatch(t, ds, fnActor, w.Kind, w.ID, substrate.PatchInput{Properties: map[string]any{"name": "b"}})
	process(t, ops)
	if got := mustGet(t, ds, taskType, "mint-"+w.ID); got.Title != "owned downstream" {
		t.Fatalf("ifAbsent put reset downstream state: %q", got.Title)
	}
	if got := len(actorChanges(t, ds, fnAuthority+"/mint")); got != rows {
		t.Fatalf("ifAbsent no-op wrote: %d rows, had %d", got, rows)
	}

	// Replay from zero is quiet the same way.
	before := dataSeq(t, ds)
	if err := ops.ReplayTrigger(ctx, trigID("mint"), 0); err != nil {
		t.Fatalf("replay: %v", err)
	}
	process(t, ops)
	if after := dataSeq(t, ds); after != before {
		t.Fatalf("replay wrote data: seq %d → %d", before, after)
	}
}

func TestTriggerEffectResolvesFormerID(t *testing.T) {
	t.Parallel()
	// The deterministic-id parking trap: after the owner merges the task a
	// function addresses by composed id, the function's next put must land on
	// the canonical winner — not park on "ids are never reused".
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{trigOn("mirror", map[string]any{"kinds": []any{widgetType}})},
		pyFn("mirror", map[string]any{}, []any{taskType}, mirrorSource))
	ctx := context.Background()

	w := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "before"}})
	process(t, ops)

	// The owner merges the function's task away into their own.
	winner := mustPut(t, ds, owner, substrate.PutInput{Kind: taskType, Properties: map[string]any{"name": "the real task"}})
	if _, err := ds.Merge(ctx, owner, winner.Kind, winner.ID, "t-"+w.ID); err != nil {
		t.Fatalf("merge: %v", err)
	}

	// The next delivery writes through the former id onto the winner.
	mustPatch(t, ds, fnActor, w.Kind, w.ID, substrate.PatchInput{Properties: map[string]any{"name": "after"}})
	process(t, ops)
	if parked, err := ops.TriggerFailures(ctx, trigID("mirror")); err != nil || len(parked) != 0 {
		t.Fatalf("former-id put parked: %v %v", parked, err)
	}
	if got := mustGet(t, ds, winner.Kind, winner.ID); got.Title != "after" {
		t.Fatalf("winner title: %q — the put did not resolve to the canonical record", got.Title)
	}
}

func TestTriggerEffectLinkUnlink(t *testing.T) {
	t.Parallel()
	// The gadget declares a `widget` edge; the function wires and unwires it
	// by effect, driven by the gadget's own properties.
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{trigOn("wirer", map[string]any{
			"kinds": []any{gadgetType},
			"when":  `record != null && "wire" in record.properties`,
		})},
		pyFn("wirer", map[string]any{}, []any{gadgetType}, `
def main(input, host):
    e = input["envelope"]["record"]
    action = e["properties"]["wire"]
    return {"effects": [{"action": action, "kind": "widgets.test.dev/gadget",
                         "id": e["id"], "rel": "widget",
                         "to": e["properties"]["target"]}]}
`))
	ctx := context.Background()

	w := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "w"}})
	g := mustPut(t, ds, fnActor, substrate.PutInput{Kind: gadgetType})

	mustPatch(t, ds, fnActor, g.Kind, g.ID, substrate.PatchInput{Properties: map[string]any{"wire": "link", "target": w.ID}})
	process(t, ops)
	if got := mustGet(t, ds, g.Kind, g.ID); len(got.Edges["widget"]) != 1 || got.Edges["widget"][0].ID != w.ID {
		t.Fatalf("link effect did not land: %v", got.Edges)
	}

	mustPatch(t, ds, fnActor, g.Kind, g.ID, substrate.PatchInput{Properties: map[string]any{"wire": "unlink"}})
	process(t, ops)
	if got := mustGet(t, ds, g.Kind, g.ID); len(got.Edges["widget"]) != 0 {
		t.Fatalf("unlink effect did not land: %v", got.Edges)
	}
	if parked, err := ops.TriggerFailures(ctx, trigID("wirer")); err != nil || len(parked) != 0 {
		t.Fatalf("parked: %v %v", parked, err)
	}
}

// fuserSource merges or splits tasks, driven by the widget's properties.
const fuserSource = `
def main(input, host):
    p = input["envelope"]["record"]["properties"]
    if p.get("op") == "merge":
        return {"effects": [{"action": "merge", "kind": "tasks.substrate.reamde.dev/task",
                             "id": p["winner"], "loser": p["loser"]}]}
    if p.get("op") == "split":
        return {"effects": [{"action": "split", "kind": "tasks.substrate.reamde.dev/task",
                             "merge": p["record"]}]}
    return {}
`

func TestTriggerEffectMergeSplitNeedGrant(t *testing.T) {
	t.Parallel()
	// Same body, no mutations grant: the merge effect is refused at decode
	// and the delivery parks — the polite default stays the *request path.
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{trigOn("ungranted", map[string]any{
			"kinds": []any{widgetType}, "ops": []any{"update"},
		})},
		pyFn("ungranted", map[string]any{}, []any{taskType}, fuserSource))

	a := mustPut(t, ds, owner, substrate.PutInput{Kind: taskType, Properties: map[string]any{"name": "a"}})
	b := mustPut(t, ds, owner, substrate.PutInput{Kind: taskType, Properties: map[string]any{"name": "b"}})
	w := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType})
	mustPatch(t, ds, fnActor, w.Kind, w.ID, substrate.PatchInput{Properties: map[string]any{
		"op": "merge", "winner": a.ID, "loser": b.ID,
	}})
	process(t, ops)

	failure := onlyParked(t, ops, trigID("ungranted"))
	if !strings.Contains(failure.LastError, "mutations grant") {
		t.Fatalf("parked: %+v", failure)
	}
	if got := mustGet(t, ds, b.Kind, b.ID); got.DeletedAt != nil {
		t.Fatal("an ungranted merge was applied")
	}
}

func TestTriggerEffectMergeAndSplit(t *testing.T) {
	t.Parallel()
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{trigOn("fuser", map[string]any{
			"kinds": []any{widgetType}, "ops": []any{"update"},
		})},
		pyFn("fuser", map[string]any{
			"permissions": map[string]any{"mutations": []any{"merge", "split"}},
		}, []any{taskType}, fuserSource))
	ctx := context.Background()

	a := mustPut(t, ds, owner, substrate.PutInput{Kind: taskType, Properties: map[string]any{"name": "a"}})
	b := mustPut(t, ds, owner, substrate.PutInput{Kind: taskType, Properties: map[string]any{"name": "b"}})
	w := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType})

	// Merge by effect: the loser tombstones and its id resolves to the winner.
	mustPatch(t, ds, fnActor, w.Kind, w.ID, substrate.PatchInput{Properties: map[string]any{
		"op": "merge", "winner": a.ID, "loser": b.ID,
	}})
	process(t, ops)
	if parked, err := ops.TriggerFailures(ctx, trigID("fuser")); err != nil || len(parked) != 0 {
		t.Fatalf("merge parked: %v %v", parked, err)
	}
	merged := mustGet(t, ds, b.Kind, b.ID)
	if merged.CanonicalID != a.ID {
		t.Fatalf("loser did not resolve to the winner: %+v", merged)
	}

	// The merge record is the function's own write, found via the changelog.
	var recordID string
	for _, ch := range actorChanges(t, ds, fnAuthority+"/fuser") {
		if ch.Kind == "core.substrate.reamde.dev/recordmerge" {
			recordID = ch.RecordID
		}
	}
	if recordID == "" {
		t.Fatal("no merge record in the function's changelog rows")
	}

	// Split by effect: the loser answers to its own id again.
	mustPatch(t, ds, fnActor, w.Kind, w.ID, substrate.PatchInput{Properties: map[string]any{
		"op": "split", "record": recordID,
	}})
	process(t, ops)
	if parked, err := ops.TriggerFailures(ctx, trigID("fuser")); err != nil || len(parked) != 0 {
		t.Fatalf("split parked: %v %v", parked, err)
	}
	restored := mustGet(t, ds, b.Kind, b.ID)
	if restored.CanonicalID != "" || restored.DeletedAt != nil || restored.Title != "b" {
		t.Fatalf("split did not restore the loser: %+v", restored)
	}
}
