package engine_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The dispatcher's acceptance gate: trigger-source matching, self-echo
// exclusion, coalescing, serial-per-record ordering, idempotent replay,
// park-and-advance, transitions via patch, emit enforcement, the
// causal-depth cap and cursor transactionality — all through TRIGGER
// records delivering inline Python bodies on the shared runner.

const (
	fnAuthority = "widgets.test.dev"
	fnActor     = substrate.Actor(fnAuthority)

	widgetType = fnAuthority + "/widget"
	gadgetType = fnAuthority + "/gadget"
	taskType   = "tasks.substrate.reamde.dev/task"
	runType    = "core.substrate.reamde.dev/run"
)

// fnOps is the engine's automation seam the API asserts at runtime; tests
// reach it the same way.
type fnOps interface {
	ProcessTriggers(ctx context.Context) (int, error)
	TriggerStatuses(ctx context.Context) ([]substrate.TriggerStatus, error)
	ReplayTrigger(ctx context.Context, id string, from int64) error
	RunTrigger(ctx context.Context, id, recordKind, recordID string) (int, error)
	WakeTrigger(ctx context.Context, id string) (int, error)
	TriggerFailures(ctx context.Context, id string) ([]substrate.TriggerFailure, error)
	RetryTriggerFailure(ctx context.Context, id string, failureID int64) (int, error)
	CallFunction(ctx context.Context, name string, args any) (any, int, error)
}

// fnDoc renders one function manifest into the test connector authority,
// defaulting the model-facing description the loader requires.
func fnDoc(name string, data map[string]any) map[string]any {
	if _, ok := data["description"]; !ok {
		data["description"] = "test function " + name
	}
	return vocabulary.FunctionManifest(fnAuthority, name, data)
}

// pyFn renders a python function manifest: the inline body as source, the write
// permission beside it, `data` carrying any further keys (timeout, arguments,
// and a `permissions:` object for the grants beyond writes).
func pyFn(name string, data map[string]any, writes []any, source string) map[string]any {
	data["runtime"] = vocabulary.RuntimePython
	data["source"] = source
	fnPermissions(data)["writes"] = writes
	return fnDoc(name, data)
}

// fnPermissions is a fixture's `permissions:` grant, minted on first use: the
// one object a declaration's grants live in.
func fnPermissions(data map[string]any) map[string]any {
	perms, ok := data["permissions"].(map[string]any)
	if !ok {
		perms = map[string]any{}
		data["permissions"] = perms
	}
	return perms
}

// trigID is the default trigger id a test function's subscription wears.
func trigID(fn string) string { return "on-" + fnAuthority + "/" + fn }

// trigOn renders one trigger record binding an record source to a test
// function: `record` carries types/ops/when/coalesce.
func trigOn(fn string, record map[string]any) enginetest.Trigger {
	return enginetest.Trigger{
		ID: trigID(fn),
		Properties: map[string]any{
			"enabled":  true,
			"source":   map[string]any{"record": record},
			"callable": vocabulary.RecordPath("core.substrate.reamde.dev/function", fnAuthority+"/"+fn),
		},
	}
}

// fnConnector assembles the test connector: the authority, its actor, two plain
// types, the functions under test and their default triggers.
func fnConnector(triggers []enginetest.Trigger, fns ...map[string]any) enginetest.Manifest {
	manifests := []map[string]any{
		vocabulary.AuthorityManifest(fnAuthority, 0),
		vocabulary.ActorManifest(fnAuthority, vocabulary.AuthorityActor(fnAuthority)),
		vocabulary.KindManifest(fnAuthority, map[string]any{"singular": "widget", "plural": "widgets"}, map[string]any{
			// Only `name` indexes: the scenario knobs must not pollute the
			// lexical arm the host-read search test asserts on.
			"properties": map[string]any{
				"name":     map[string]any{"type": "string"},
				"assignee": map[string]any{"type": "string", "fts": false},
				"count":    map[string]any{"type": "float"},
				"want":     map[string]any{"type": "string", "fts": false},
				"mode":     map[string]any{"type": "string", "fts": false},
				"target":   map[string]any{"type": "string", "fts": false},
				"match":    map[string]any{"type": "string", "fts": false},
				"q":        map[string]any{"type": "string", "fts": false},
				"calls":    map[string]any{"type": "float"},
				"op":       map[string]any{"type": "string", "fts": false},
				"winner":   map[string]any{"type": "string", "fts": false},
				"loser":    map[string]any{"type": "string", "fts": false},
				"record":   map[string]any{"type": "string", "fts": false},
			},
		}),
		vocabulary.KindManifest(fnAuthority, map[string]any{"singular": "gadget", "plural": "gadgets"}, map[string]any{
			"properties": map[string]any{
				"count":  map[string]any{"type": "float"},
				"wire":   map[string]any{"type": "string", "fts": false},
				"target": map[string]any{"type": "string", "fts": false},
				"widget": map[string]any{
					"type": "reference", "kind": widgetType, "mustExist": true,
					"description": "the widget this gadget belongs to",
				},
			},
		}),
	}
	manifests = append(manifests, fns...)
	return enginetest.Manifest{Name: "widgets", Authority: fnAuthority, Manifests: manifests, Triggers: triggers}
}

// newFnDataset provisions a repository and installs the test connector.
func newFnDataset(t *testing.T, triggers []enginetest.Trigger, fns ...map[string]any) (substrate.Dataset, fnOps) {
	t.Helper()
	_, ds := newDataset(t)
	if err := enginetest.Install(context.Background(), ds, owner, fnConnector(triggers, fns...)); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	ops, ok := ds.(fnOps)
	if !ok {
		t.Fatal("dataset does not implement the automation seam")
	}
	return ds, ops
}

// TestPrepareBatchCountCap is finding #13: a single admission batch cannot warm
// an unbounded number of function bodies. A bundle of many stalling PEP 723
// bodies must not be able to hold the schema-write serialization for N × 120s;
// the count cap (maxPrepareBatch = 64) refuses the batch outright, BEFORE any
// body is warmed, so the deterministic-count path needs no uv to exercise.
func TestPrepareBatchCountCap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	sa := applier(t, ds)
	docs := []map[string]any{
		vocabulary.AuthorityManifest(fnAuthority, 0),
		vocabulary.ActorManifest(fnAuthority, vocabulary.AuthorityActor(fnAuthority)),
		vocabulary.KindManifest(fnAuthority, map[string]any{"singular": "widget", "plural": "widgets"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
	}
	// 65 bodies — one over the maxPrepareBatch=64 cap.
	for i := range 65 {
		name := fmt.Sprintf("cap%02d", i)
		docs = append(docs, pyFn(name, map[string]any{}, []any{widgetType},
			"def main(input, host):\n    return {}\n"))
	}
	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, docs); err == nil ||
		!strings.Contains(err.Error(), "at most 64 function bodies") {
		t.Fatalf("a batch over the prepare cap was admitted: %v", err)
	}
}

func process(t *testing.T, ops fnOps) int {
	t.Helper()
	n, err := ops.ProcessTriggers(context.Background())
	if err != nil {
		t.Fatalf("process triggers: %v", err)
	}
	return n
}

// actorChanges lists the changelog rows one function authored. The function
// is named by its IDENTITY, because so is its actor (record 0025).
func actorChanges(t *testing.T, ds substrate.Dataset, function string) []substrate.Change {
	t.Helper()
	authority, name := vocabulary.SplitKindRef(function)
	out, err := ds.Changes(context.Background(), 0, substrate.ChangeFilter{
		Actors: []substrate.Actor{substrate.FunctionActor(authority, name)},
	}, 500)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	return out
}

// dataSeq is the newest changelog seq EXCLUDING run-ledger rows: every
// settled delivery attempt writes a run record, so "the data went quiet" is
// asserted over everything but the ledger describing the attempts.
func dataSeq(t *testing.T, ds substrate.Dataset) int64 {
	t.Helper()
	out, err := ds.Changes(context.Background(), 0, substrate.ChangeFilter{
		ExcludeKinds: []string{runType},
	}, 100000)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	if len(out) == 0 {
		return 0
	}
	return out[len(out)-1].Seq
}

func statusOf(t *testing.T, ops fnOps, triggerID string) substrate.TriggerStatus {
	t.Helper()
	statuses, err := ops.TriggerStatuses(context.Background())
	if err != nil {
		t.Fatalf("statuses: %v", err)
	}
	for _, st := range statuses {
		if st.ID == triggerID {
			return st
		}
	}
	t.Fatalf("no status for %s", triggerID)
	return substrate.TriggerStatus{}
}

// mirrorSource puts a task titled after the widget's name — the workhorse
// body: a widget without a name raises, which is the parking fixture.
const mirrorSource = `
def main(input, host):
    env = input["envelope"]
    return {"effects": [{"action": "put", "kind": "tasks.substrate.reamde.dev/task",
                         "id": "t-" + env["change"]["id"],
                         "properties": {"name": env["record"]["properties"]["name"]}}]}
`

func TestTriggerSourceMatchingAndGlob(t *testing.T) {
	t.Parallel()
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{
			// Exact source, create only.
			trigOn("exact", map[string]any{"kinds": []any{widgetType}, "ops": []any{"create"}}),
			// Authority glob, all ops.
			trigOn("glob", map[string]any{"kinds": []any{fnAuthority + "/*"}}),
		},
		pyFn("exact", map[string]any{}, []any{taskType}, `
def main(input, host):
    c = input["envelope"]["change"]
    return {"effects": [{"action": "put", "kind": "tasks.substrate.reamde.dev/task",
                         "id": "e-" + c["id"], "properties": {"name": c["kind"]}}]}
`),
		pyFn("glob", map[string]any{}, []any{taskType}, `
def main(input, host):
    c = input["envelope"]["change"]
    return {"effects": [{"action": "put", "kind": "tasks.substrate.reamde.dev/task",
                         "id": "g-" + c["id"], "properties": {"name": c["kind"] + "/" + c["op"]}}]}
`),
	)
	ctx := context.Background()

	w := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "one"}})
	g := mustPut(t, ds, fnActor, substrate.PutInput{Kind: gadgetType})
	process(t, ops)

	// Both fire on the widget; only the glob fires on the gadget.
	if got := mustGet(t, ds, taskType, "e-"+w.ID); got.Title != widgetType {
		t.Fatalf("exact task title: %q", got.Title)
	}
	if got := mustGet(t, ds, taskType, "g-"+w.ID); got.Title != widgetType+"/create" {
		t.Fatalf("glob task title: %q", got.Title)
	}
	if _, err := ds.Get(ctx, taskType, "e-"+g.ID); err == nil {
		t.Fatal("exact source fired on a gadget")
	}
	if got := mustGet(t, ds, taskType, "g-"+g.ID); got.Title != gadgetType+"/create" {
		t.Fatalf("glob gadget title: %q", got.Title)
	}

	// The ops filter: an update re-fires the glob (op in title changes) but
	// never the create-only trigger.
	exactRows := len(actorChanges(t, ds, fnAuthority+"/exact"))
	mustPatch(t, ds, fnActor, w.Kind, w.ID, substrate.PatchInput{Properties: map[string]any{"name": "two"}})
	process(t, ops)
	if got := mustGet(t, ds, taskType, "g-"+w.ID); got.Title != widgetType+"/update" {
		t.Fatalf("glob after update: %q", got.Title)
	}
	if got := len(actorChanges(t, ds, fnAuthority+"/exact")); got != exactRows {
		t.Fatalf("create-only trigger fired on an update: %d rows, had %d", got, exactRows)
	}

	// The callable's writes carry its own actor and normal attribution.
	rows := actorChanges(t, ds, fnAuthority+"/exact")
	if len(rows) != 1 || rows[0].RecordID != "e-"+w.ID || rows[0].Op != substrate.OpPut {
		t.Fatalf("exact's changelog rows: %+v", rows)
	}
}

func TestTriggerWhenGuard(t *testing.T) {
	t.Parallel()
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{trigOn("guarded", map[string]any{
			"kinds": []any{widgetType},
			"when":  `record != null && record.properties.assignee == repository.owner`,
		})},
		pyFn("guarded", map[string]any{}, []any{taskType}, mirrorSource))
	ctx := context.Background()

	other := mustPut(t, ds, fnActor, substrate.PutInput{
		Kind:       widgetType,
		Properties: map[string]any{"name": "not mine", "assignee": "somebody"},
	})
	mine := mustPut(t, ds, fnActor, substrate.PutInput{
		Kind:       widgetType,
		Properties: map[string]any{"name": "mine", "assignee": "geoah"},
	})
	process(t, ops)

	if _, err := ds.Get(ctx, taskType, "t-"+other.ID); err == nil {
		t.Fatal("guard false must skip")
	}
	if got := mustGet(t, ds, taskType, "t-"+mine.ID); got.Title != "mine" {
		t.Fatalf("guarded task title: %q", got.Title)
	}
	// A skipped change still advances the cursor — false is normal, not lag.
	if st := statusOf(t, ops, trigID("guarded")); st.Lag != 0 || st.Parked != 0 {
		t.Fatalf("status after skip: %+v", st)
	}
	// And the skip is a settled attempt in the run ledger.
	skips := runRowsOf(t, ds, trigID("guarded"), "skipped")
	if len(skips) != 1 || skips[0].Properties["record"] != other.ID {
		t.Fatalf("skip runs: %+v", skips)
	}
}

// runRowsOf lists a trigger's live run records with the given status.
func runRowsOf(t *testing.T, ds substrate.Dataset, triggerID, status string) []*substrate.Record {
	t.Helper()
	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{
			Kinds: []string{runType},
			Properties: map[string]substrate.Cond{
				"trigger": {Eq: vocabulary.RecordPath("core.substrate.reamde.dev/trigger", triggerID)},
				"status":  {Eq: status},
			},
		},
		First: 500,
	})
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	return page.Records
}

func TestTriggerSelfEchoExclusion(t *testing.T) {
	t.Parallel()
	// The trigger watches the very type its callable writes: without
	// exclusion by the CALLABLE's actor this loops forever, with it the
	// marker settles after one run.
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{trigOn("echo", map[string]any{"kinds": []any{taskType}})},
		pyFn("echo", map[string]any{}, []any{taskType}, `
def main(input, host):
    c = input["envelope"]["change"]
    return {"effects": [{"action": "patch", "kind": "tasks.substrate.reamde.dev/task",
                         "id": c["id"], "properties": {"description": "seen-" + str(c["seq"])}}]}
`))

	task := mustPut(t, ds, owner, substrate.PutInput{Kind: taskType, Properties: map[string]any{"name": "t"}})
	process(t, ops)
	marked := mustGet(t, ds, task.Kind, task.ID)
	desc, _ := marked.Properties["description"].(string)
	if !strings.HasPrefix(desc, "seen-") {
		t.Fatalf("marker missing: %q", desc)
	}

	// The callable's own patch is now in the changelog past the cursor; more
	// passes must not re-fire it (the marker would change with the new seq).
	process(t, ops)
	process(t, ops)
	if got := mustGet(t, ds, task.Kind, task.ID); got.Properties["description"] != desc {
		t.Fatalf("trigger saw its callable's own write: %q → %q", desc, got.Properties["description"])
	}
	if rows := actorChanges(t, ds, fnAuthority+"/echo"); len(rows) != 1 {
		t.Fatalf("expected exactly one function-authored row, got %d", len(rows))
	}
	if st := statusOf(t, ops, trigID("echo")); st.Lag != 0 {
		t.Fatalf("lag after settling: %+v", st)
	}
}

func TestTriggerCoalescingAndSerialOrder(t *testing.T) {
	t.Parallel()
	// Both triggers stamp the seq they ran against; the coalesced one runs
	// once per batch, the serial one once per change, in changelog order.
	body := func(prefix string) string {
		return fmt.Sprintf(`
def main(input, host):
    c = input["envelope"]["change"]
    return {"effects": [{"action": "put", "kind": "tasks.substrate.reamde.dev/task",
                         "id": "%s-" + c["id"], "properties": {"name": "seen-" + str(c["seq"])}}]}
`, prefix)
	}
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{
			trigOn("collapsed", map[string]any{"kinds": []any{widgetType}, "coalesce": true}),
			trigOn("serial", map[string]any{"kinds": []any{widgetType}}),
		},
		pyFn("collapsed", map[string]any{}, []any{taskType}, body("c")),
		pyFn("serial", map[string]any{}, []any{taskType}, body("s")),
	)

	w := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "a"}})
	mustPatch(t, ds, fnActor, w.Kind, w.ID, substrate.PatchInput{Properties: map[string]any{"name": "b"}})
	mustPatch(t, ds, fnActor, w.Kind, w.ID, substrate.PatchInput{Properties: map[string]any{"name": "c"}})
	lastSeq := maxSeq(t, ds)
	process(t, ops)

	// Coalesced: three pending changes to one record, one run, cursor past
	// all three.
	if rows := actorChanges(t, ds, fnAuthority+"/collapsed"); len(rows) != 1 {
		t.Fatalf("coalesced runs: %d rows", len(rows))
	}
	want := fmt.Sprintf("seen-%d", lastSeq)
	if got := mustGet(t, ds, taskType, "c-"+w.ID); got.Title != want {
		t.Fatalf("coalesced ran against %q, want %q", got.Title, want)
	}

	// Serial: one run per change, applied in seq order — the title walks the
	// seqs upward and lands on the last.
	rows := actorChanges(t, ds, fnAuthority+"/serial")
	if len(rows) != 3 {
		t.Fatalf("serial runs: %d rows", len(rows))
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].Seq <= rows[i-1].Seq {
			t.Fatalf("serial rows out of order: %+v", rows)
		}
	}
	if got := mustGet(t, ds, taskType, "s-"+w.ID); got.Title != want {
		t.Fatalf("serial final title %q, want %q", got.Title, want)
	}
}

func TestTriggerReplayFromZeroIsIdempotent(t *testing.T) {
	t.Parallel()
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{trigOn("mirror", map[string]any{"kinds": []any{widgetType}})},
		pyFn("mirror", map[string]any{}, []any{taskType}, mirrorSource))
	ctx := context.Background()

	w := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "roof"}})
	process(t, ops)
	if got := mustGet(t, ds, taskType, "t-"+w.ID); got.Title != "roof" {
		t.Fatalf("task title: %q", got.Title)
	}

	// Replay from zero re-delivers everything; the identical put is a no-op
	// under suppression, so the DATA does not move — only the run ledger
	// records the re-delivery.
	before := dataSeq(t, ds)
	if err := ops.ReplayTrigger(ctx, trigID("mirror"), 0); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if st := statusOf(t, ops, trigID("mirror")); st.Cursor != 0 {
		t.Fatalf("cursor after replay: %+v", st)
	}
	process(t, ops)
	if after := dataSeq(t, ds); after != before {
		t.Fatalf("replay wrote data: seq %d → %d", before, after)
	}
	if st := statusOf(t, ops, trigID("mirror")); st.Lag != 0 {
		t.Fatalf("lag after replay: %+v", st)
	}
}

func TestTriggerParkAndAdvanceThenRetry(t *testing.T) {
	t.Parallel()
	// The body raises on the widget that carries no name: a body error at
	// fire time is a parked failure, never a crash — and the cursor moves on
	// so the healthy neighbor still processes.
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{trigOn("mirror", map[string]any{"kinds": []any{widgetType}})},
		pyFn("mirror", map[string]any{}, []any{taskType}, mirrorSource))
	ctx := context.Background()

	poisoned := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType})
	healthy := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "fine"}})
	process(t, ops)

	if got := mustGet(t, ds, taskType, "t-"+healthy.ID); got.Title != "fine" {
		t.Fatalf("healthy record blocked: %q", got.Title)
	}
	st := statusOf(t, ops, trigID("mirror"))
	if st.Lag != 0 || st.Parked != 1 {
		t.Fatalf("status after park: %+v", st)
	}
	parked, err := ops.TriggerFailures(ctx, trigID("mirror"))
	if err != nil {
		t.Fatalf("failures: %v", err)
	}
	if len(parked) != 1 || parked[0].RecordID != poisoned.ID || parked[0].Attempts != 3 {
		t.Fatalf("parked rows: %+v", parked)
	}
	if !strings.Contains(parked[0].LastError, "KeyError") {
		t.Fatalf("the body's error must surface: %q", parked[0].LastError)
	}
	// The parked attempt is on the run ledger too, and it survives pruning.
	if runs := runRowsOf(t, ds, trigID("mirror"), "parked"); len(runs) != 1 {
		t.Fatalf("parked runs: %+v", runs)
	}

	// A retry runs against CURRENT state: fix the record, retry by hand, the
	// row clears.
	mustPatch(t, ds, fnActor, poisoned.Kind, poisoned.ID, substrate.PatchInput{Properties: map[string]any{"name": "fixed"}})
	if _, err := ops.RetryTriggerFailure(ctx, trigID("mirror"), parked[0].ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := mustGet(t, ds, taskType, "t-"+poisoned.ID); got.Title != "fixed" {
		t.Fatalf("retried task title: %q", got.Title)
	}
	parked, err = ops.TriggerFailures(ctx, trigID("mirror"))
	if err != nil {
		t.Fatalf("failures: %v", err)
	}
	if len(parked) != 0 {
		t.Fatalf("failure row survived a successful retry: %+v", parked)
	}
}

func TestTriggerTransitionViaPatch(t *testing.T) {
	t.Parallel()
	// A patch effect naming a state value is a transition and obeys the
	// machine: open → done is declared (and stamps completedAt), done →
	// abandoned is not and parks.
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{trigOn("closer", map[string]any{
			"kinds": []any{widgetType}, "ops": []any{"update"},
			"when": `record != null && "want" in record.properties`,
		})},
		pyFn("closer", map[string]any{}, []any{taskType}, `
def main(input, host):
    env = input["envelope"]
    return {"effects": [{"action": "patch", "kind": "tasks.substrate.reamde.dev/task",
                         "id": "t-" + env["change"]["id"],
                         "properties": {"status": env["record"]["properties"]["want"]}}]}
`))
	ctx := context.Background()

	w := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "w"}})
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskType, ID: "t-" + w.ID, Properties: map[string]any{"name": "w"}})
	process(t, ops)

	mustPatch(t, ds, fnActor, w.Kind, w.ID, substrate.PatchInput{Properties: map[string]any{"want": "done"}})
	process(t, ops)
	task := mustGet(t, ds, taskType, "t-"+w.ID)
	if task.Properties["status"] != "done" {
		t.Fatalf("status: %v", task.Properties["status"])
	}
	if _, ok := task.Properties["completedAt"]; !ok {
		t.Fatal("the transition's stamp is missing")
	}

	// The illegal transition is a normal per-effect error: retried, parked,
	// the task untouched.
	mustPatch(t, ds, fnActor, w.Kind, w.ID, substrate.PatchInput{Properties: map[string]any{"want": "abandoned"}})
	process(t, ops)
	if got := mustGet(t, ds, taskType, "t-"+w.ID); got.Properties["status"] != "done" {
		t.Fatalf("illegal transition applied: %v", got.Properties["status"])
	}
	parked, err := ops.TriggerFailures(ctx, trigID("closer"))
	if err != nil {
		t.Fatalf("failures: %v", err)
	}
	if len(parked) != 1 || !strings.Contains(parked[0].LastError, "no transition") {
		t.Fatalf("parked rows: %+v", parked)
	}
}

func TestTriggerEmitViolationParks(t *testing.T) {
	t.Parallel()
	// tasks.substrate.reamde.dev/project exists but is not in the allowlist: the effect
	// is rejected at apply time and the delivery parks.
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{trigOn("wild", map[string]any{"kinds": []any{widgetType}})},
		pyFn("wild", map[string]any{}, []any{taskType}, `
def main(input, host):
    c = input["envelope"]["change"]
    return {"effects": [{"action": "put", "kind": "tasks.substrate.reamde.dev/project", "id": "p-" + c["id"]}]}
`))
	ctx := context.Background()

	w := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType})
	process(t, ops)

	if _, err := ds.Get(ctx, "tasks.substrate.reamde.dev/project", "p-"+w.ID); err == nil {
		t.Fatal("an effect outside emit was applied")
	}
	parked, err := ops.TriggerFailures(ctx, trigID("wild"))
	if err != nil {
		t.Fatalf("failures: %v", err)
	}
	if len(parked) != 1 || !strings.Contains(parked[0].LastError, "emit") {
		t.Fatalf("parked rows: %+v", parked)
	}
	if st := statusOf(t, ops, trigID("wild")); st.Lag != 0 {
		t.Fatalf("park did not advance: %+v", st)
	}
}

func TestTriggerEffectsAndCursorAreOneTransaction(t *testing.T) {
	t.Parallel()
	// The second effect fails inside the transaction (its target does not
	// exist), so the first effect must roll back with it: effects and cursor
	// move together or not at all — a parked delivery leaves no half-applied
	// write behind.
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{trigOn("pair", map[string]any{"kinds": []any{widgetType}})},
		pyFn("pair", map[string]any{}, []any{taskType}, `
def main(input, host):
    c = input["envelope"]["change"]
    return {"effects": [
        {"action": "put", "kind": "tasks.substrate.reamde.dev/task", "id": "ok-" + c["id"], "properties": {"name": "half"}},
        {"action": "patch", "kind": "tasks.substrate.reamde.dev/task", "id": "missing-" + c["id"], "properties": {"name": "x"}},
    ]}
`))
	ctx := context.Background()

	w := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType})
	process(t, ops)

	if _, err := ds.Get(ctx, taskType, "ok-"+w.ID); err == nil {
		t.Fatal("the first effect survived its sibling's failure")
	}
	parked, err := ops.TriggerFailures(ctx, trigID("pair"))
	if err != nil {
		t.Fatalf("failures: %v", err)
	}
	if len(parked) != 1 {
		t.Fatalf("parked rows: %+v", parked)
	}
	if st := statusOf(t, ops, trigID("pair")); st.Lag != 0 {
		t.Fatalf("cursor after rollback: %+v", st)
	}
	// And nothing the rolled-back transaction touched reached the changelog.
	if rows := actorChanges(t, ds, fnAuthority+"/pair"); len(rows) != 0 {
		t.Fatalf("rolled-back writes logged: %+v", rows)
	}
}

func TestTriggerCausalDepthCap(t *testing.T) {
	t.Parallel()
	// A→B→A: each write records the seq that caused it, and a chain deeper
	// than the cap parks with a distinct error instead of ping-ponging
	// forever.
	bounce := func(target string) string {
		return fmt.Sprintf(`
def main(input, host):
    props = input["envelope"]["record"]["properties"]
    return {"effects": [{"action": "put", "kind": "%s",
                         "id": "%s", "properties": {"count": props["count"] + 1.0}}]}
`, target, target[:1])
	}
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{
			trigOn("ping", map[string]any{"kinds": []any{widgetType}}),
			trigOn("pong", map[string]any{"kinds": []any{gadgetType}}),
		},
		pyFn("ping", map[string]any{}, []any{gadgetType}, bounce(gadgetType)),
		pyFn("pong", map[string]any{}, []any{widgetType}, bounce(widgetType)),
	)
	ctx := context.Background()

	mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, ID: "w", Properties: map[string]any{"count": float64(0)}})
	var parked []substrate.TriggerFailure
	for range 30 {
		process(t, ops)
		for _, name := range []string{"ping", "pong"} {
			rows, err := ops.TriggerFailures(ctx, trigID(name))
			if err != nil {
				t.Fatalf("failures: %v", err)
			}
			parked = append(parked, rows...)
		}
		if len(parked) > 0 {
			break
		}
	}
	if len(parked) == 0 {
		t.Fatal("the loop never parked")
	}
	if !strings.Contains(parked[0].LastError, "causal depth") {
		t.Fatalf("park reason: %q", parked[0].LastError)
	}
	// The cap is per chain: the chain wrote cap-many times and stopped.
	total := len(actorChanges(t, ds, fnAuthority+"/ping")) + len(actorChanges(t, ds, fnAuthority+"/pong"))
	if total > 17 {
		t.Fatalf("the chain kept going: %d function-authored rows", total)
	}
}

func TestTriggerManualRunLeavesCursorAlone(t *testing.T) {
	t.Parallel()
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{trigOn("mirror", map[string]any{"kinds": []any{widgetType}})},
		pyFn("mirror", map[string]any{}, []any{taskType}, mirrorSource))
	ctx := context.Background()

	w := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "manual"}})

	// One synthesized delivery, no cursor motion: the lag stays.
	ran, err := ops.RunTrigger(ctx, trigID("mirror"), w.Kind, w.ID)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if ran != 1 {
		t.Fatalf("ran: %d", ran)
	}
	if got := mustGet(t, ds, taskType, "t-"+w.ID); got.Title != "manual" {
		t.Fatalf("task title: %q", got.Title)
	}
	if st := statusOf(t, ops, trigID("mirror")); st.Lag == 0 {
		t.Fatalf("manual run moved the cursor: %+v", st)
	}
	// Direct invocations mint nothing on the run ledger.
	if runs := runRowsOf(t, ds, trigID("mirror"), "ok"); len(runs) != 0 {
		t.Fatalf("manual run minted run rows: %+v", runs)
	}

	// The ordinary dispatch that follows is a no-op in the data: same id,
	// same value.
	before := dataSeq(t, ds)
	process(t, ops)
	if after := dataSeq(t, ds); after != before {
		t.Fatalf("dispatch after manual run wrote data: %d → %d", before, after)
	}
	if st := statusOf(t, ops, trigID("mirror")); st.Lag != 0 {
		t.Fatalf("lag after dispatch: %+v", st)
	}
}

func TestTriggerDeleteSource(t *testing.T) {
	t.Parallel()
	// record is null after delete; the effect removes the mirrored task.
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{trigOn("sweeper", map[string]any{
			"kinds": []any{widgetType}, "ops": []any{"delete"},
			"when": `record == null`,
		})},
		pyFn("sweeper", map[string]any{}, []any{taskType}, `
def main(input, host):
    c = input["envelope"]["change"]
    return {"effects": [{"action": "delete", "kind": "tasks.substrate.reamde.dev/task", "id": "t-" + c["id"]}]}
`))
	ctx := context.Background()

	w := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType})
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskType, ID: "t-" + w.ID, Properties: map[string]any{"name": "w"}})
	process(t, ops)

	if _, err := ds.Delete(ctx, fnActor, w.Kind, w.ID); err != nil {
		t.Fatalf("delete widget: %v", err)
	}
	process(t, ops)
	got := mustGet(t, ds, taskType, "t-"+w.ID)
	if got.DeletedAt == nil {
		t.Fatal("the mirrored task was not tombstoned")
	}
}

func TestTriggerSameStatePatchIsNoOp(t *testing.T) {
	t.Parallel()
	// The wrinkle the github loop rests on: a function without a read
	// capability cannot ask the task's state, so it re-asserts `status: done`
	// on every delivery of a closed item. A patch naming the CURRENT state
	// must be a no-op — no transition error, no parked failure, and above all
	// no data changelog row, because the row is what would feed the next
	// trigger.
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{trigOn("closer", map[string]any{
			"kinds": []any{widgetType}, "ops": []any{"update"},
		})},
		pyFn("closer", map[string]any{}, []any{taskType}, `
def main(input, host):
    c = input["envelope"]["change"]
    return {"effects": [{"action": "patch", "kind": "tasks.substrate.reamde.dev/task",
                         "id": "t-" + c["id"], "properties": {"status": "done"}}]}
`))
	ctx := context.Background()

	w := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "w"}})
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskType, ID: "t-" + w.ID, Properties: map[string]any{"name": "w"}})
	process(t, ops)

	// First delivery moves open → done and stamps completedAt.
	mustPatch(t, ds, fnActor, w.Kind, w.ID, substrate.PatchInput{Properties: map[string]any{"name": "w2"}})
	process(t, ops)
	task := mustGet(t, ds, taskType, "t-"+w.ID)
	if task.Properties["status"] != "done" {
		t.Fatalf("status: %v", task.Properties["status"])
	}
	stamped := task.Properties["completedAt"]

	// Every later delivery re-asserts done against done: silence in the data.
	mustPatch(t, ds, fnActor, w.Kind, w.ID, substrate.PatchInput{Properties: map[string]any{"name": "w3"}})
	before := dataSeq(t, ds)
	process(t, ops)
	if after := dataSeq(t, ds); after != before {
		t.Fatalf("a done → done patch wrote data: seq %d → %d", before, after)
	}
	if parked, err := ops.TriggerFailures(ctx, trigID("closer")); err != nil || len(parked) != 0 {
		t.Fatalf("parked = %v, err = %v; a same-state patch is not a failure", parked, err)
	}
	if got := mustGet(t, ds, taskType, "t-"+w.ID); got.Properties["completedAt"] != stamped {
		t.Fatalf("completedAt restamped: %v → %v", stamped, got.Properties["completedAt"])
	}
	if st := statusOf(t, ops, trigID("closer")); st.Lag != 0 {
		t.Fatalf("lag: %+v", st)
	}
}

func TestTriggerDisabledStandsStill(t *testing.T) {
	t.Parallel()
	// enabled: false stops delivery without losing the cursor; re-enabling
	// resumes from where it stood — the interim change delivers late, never
	// lost.
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{trigOn("mirror", map[string]any{"kinds": []any{widgetType}})},
		pyFn("mirror", map[string]any{}, []any{taskType}, mirrorSource))
	ctx := context.Background()

	if _, err := ds.Patch(ctx, owner, "core.substrate.reamde.dev/trigger", trigID("mirror"), substrate.PatchInput{
		Properties: map[string]any{"enabled": false},
	}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	w := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "late"}})
	process(t, ops)
	if _, err := ds.Get(ctx, taskType, "t-"+w.ID); err == nil {
		t.Fatal("a disabled trigger delivered")
	}
	if st := statusOf(t, ops, trigID("mirror")); st.Enabled || st.Lag == 0 {
		t.Fatalf("disabled status: %+v", st)
	}

	if _, err := ds.Patch(ctx, owner, "core.substrate.reamde.dev/trigger", trigID("mirror"), substrate.PatchInput{
		Properties: map[string]any{"enabled": true},
	}); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	process(t, ops)
	if got := mustGet(t, ds, taskType, "t-"+w.ID); got.Title != "late" {
		t.Fatalf("the backlog did not deliver after re-enable: %q", got.Title)
	}
}

func TestTriggerWriteAdmission(t *testing.T) {
	t.Parallel()
	// A trigger row that cannot dispatch never lands: bad guards, unknown
	// callables, zero or two source arms, bad recurrences all refuse at
	// write time.
	ds, _ := newFnDataset(t,
		[]enginetest.Trigger{},
		pyFn("mirror", map[string]any{}, []any{taskType}, mirrorSource))
	ctx := context.Background()

	callable := vocabulary.RecordPath("core.substrate.reamde.dev/function", fnAuthority+"/mirror")
	cases := map[string]map[string]any{
		"no source": {
			"callable": callable,
		},
		"two source arms": {
			"source": map[string]any{
				"record":  map[string]any{"kinds": []any{widgetType}},
				"webhook": map[string]any{},
			},
			"callable": callable,
		},
		"uncompilable when": {
			"source": map[string]any{"record": map[string]any{
				"kinds": []any{widgetType}, "when": "record.properties.name ==",
			}},
			"callable": callable,
		},
		"bad op": {
			"source": map[string]any{"record": map[string]any{
				"kinds": []any{widgetType}, "ops": []any{"upsert"},
			}},
			"callable": callable,
		},
		"bad glob": {
			"source": map[string]any{"record": map[string]any{
				"kinds": []any{"wid*get"},
			}},
			"callable": callable,
		},
		"unknown callable": {
			"source":   map[string]any{"record": map[string]any{"kinds": []any{widgetType}}},
			"callable": vocabulary.RecordPath("core.substrate.reamde.dev/function", fnAuthority+"/nothing"),
		},
		"undispatchable kind": {
			"source":   map[string]any{"record": map[string]any{"kinds": []any{widgetType}}},
			"callable": vocabulary.RecordPath("core.substrate.reamde.dev/agent", fnAuthority+"/mirror"),
		},
		"bad recurrence": {
			"source":   map[string]any{"schedule": map[string]any{"recurrence": "EVERY=DAY"}},
			"callable": callable,
		},
		"bad timezone": {
			"source":   map[string]any{"schedule": map[string]any{"recurrence": "FREQ=DAILY", "timezone": "Mars/Olympus"}},
			"callable": callable,
		},
	}
	for name, props := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ds.Put(ctx, owner, substrate.PutInput{
				Kind: "core.substrate.reamde.dev/trigger", Properties: props,
			})
			if err == nil {
				t.Fatal("an undispatchable trigger landed")
			}
		})
	}

	// The healthy shape lands, and a patch that would break it refuses while
	// the row keeps working.
	tr, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "core.substrate.reamde.dev/trigger",
		Properties: map[string]any{
			"source":   map[string]any{"record": map[string]any{"kinds": []any{widgetType}}},
			"callable": callable,
		},
	})
	if err != nil {
		t.Fatalf("valid trigger refused: %v", err)
	}
	if _, err := ds.Patch(ctx, owner, tr.Kind, tr.ID, substrate.PatchInput{
		Properties: map[string]any{"callable": vocabulary.RecordPath("core.substrate.reamde.dev/function", fnAuthority+"/nothing")},
	}); err == nil {
		t.Fatal("a breaking patch landed")
	}
}

// TestTriggerDispatchIsPerRepository: a trigger's cursor is a position in ITS
// repository's changelog, and a dispatcher pass is one repository's pass. Two users
// install the same bundle; one writes; the other's trigger has nothing to do
// and its records stay empty. Without this, the loop in main.go is a shared
// dispatcher wearing a per-repository name.
func TestTriggerDispatchIsPerRepository(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	install := func(username string) (substrate.Dataset, fnOps) {
		t.Helper()
		if _, err := svc.CreateRepository(ctx, username); err != nil {
			t.Fatalf("create repository %s: %v", username, err)
		}
		ds, err := svc.Dataset(ctx, username)
		if err != nil {
			t.Fatalf("open %s: %v", username, err)
		}
		importVocabulary(t, ds, "tasks")
		err = enginetest.Install(ctx, ds, owner, fnConnector(
			[]enginetest.Trigger{trigOn("mirror", map[string]any{"kinds": []any{widgetType}})},
			pyFn("mirror", map[string]any{}, []any{taskType}, `
def main(input, host):
    c = input["envelope"]["change"]
    return {"effects": [{"action": "put", "kind": "tasks.substrate.reamde.dev/task",
                         "id": "m-" + c["id"], "properties": {"name": input["envelope"]["repository"]["owner"]}}]}
`)))
		if err != nil {
			t.Fatalf("install into %s: %v", username, err)
		}
		ops, ok := ds.(fnOps)
		if !ok {
			t.Fatal("dataset does not implement the automation seam")
		}
		return ds, ops
	}
	one, opsOne := install("geoah")
	two, opsTwo := install("ada")

	mustPut(t, one, owner, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "only here"}})
	if n := process(t, opsOne); n != 1 {
		t.Fatalf("the writing repository ran %d deliveries, want 1", n)
	}
	if n := process(t, opsTwo); n != 0 {
		t.Fatalf("the quiet repository ran %d deliveries off another repository's changelog", n)
	}

	tasksIn := func(ds substrate.Dataset) int {
		t.Helper()
		page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{taskType}}, First: 100})
		if err != nil {
			t.Fatalf("list tasks: %v", err)
		}
		return len(page.Records)
	}
	if got := tasksIn(one); got != 1 {
		t.Fatalf("the writing repository holds %d tasks, want 1", got)
	}
	if got := tasksIn(two); got != 0 {
		t.Fatalf("the quiet repository holds %d tasks off another repository's delivery", got)
	}
	// And the cursors moved independently: the quiet repository's trigger is
	// still at its own head, not the other's.
	if st := statusOf(t, opsTwo, trigID("mirror")); st.Lag != 0 {
		t.Fatalf("the quiet repository's trigger carries lag %d", st.Lag)
	}
}
