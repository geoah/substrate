package engine

// A sub-agent call's tool row says what the child chain wrote: the engine
// stamps `subagentWrites` ({thread, records, kinds, moreKinds}) on the
// parent's row when the child's run returns, counting the grandchildren's writes
// too, while each write's changelog entry stays on the child row that made
// it.

import (
	"context"
	"reflect"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const rollupPackage = "rollup.test.dev/rollup"

var (
	rollupTask = rollupPackage + "/task"
	rollupNote = rollupPackage + "/note"
	rollupMemo = rollupPackage + "/memo"
)

// provisionRollupAgents installs three kinds and a chain of three agents:
// `lead` holds no tools and delegates to `worker`, which writes tasks and
// notes and delegates to `helper`, which writes memos. Every hop's emit
// covers the hops below it, since the ceiling intersects at each one.
func provisionRollupAgents(t *testing.T) (*dataset, *fakeLLM) {
	t.Helper()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeProvider, ID: "rollupllm",
		Properties: map[string]any{
			"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-rollupllm",
			"pricing": []any{
				map[string]any{"model": "rlead", "inputPer1M": "1", "outputPer1M": "5"},
				map[string]any{"model": "rworker", "inputPer1M": "1", "outputPer1M": "5"},
				map[string]any{"model": "rhelper", "inputPer1M": "1", "outputPer1M": "5"},
			},
		},
	}); err != nil {
		t.Fatalf("put llm/provider row rollupllm: %v", err)
	}
	agent := func(name string, data map[string]any) map[string]any {
		data["description"] = name + " under test"
		data["prompt"] = "You are " + name + "."
		return vocabulary.AgentManifest(rollupPackage, name, data)
	}
	kind := func(name string) map[string]any {
		return vocabulary.KindManifest(rollupPackage, map[string]any{"singular": name},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}})
	}
	all := []any{rollupTask, rollupNote, rollupMemo}
	docs := []map[string]any{
		vocabulary.PackageManifest(rollupPackage, 0),
		kind("task"), kind("note"), kind("memo"),
		agent("lead", map[string]any{
			"provider": "rollupllm", "model": "rlead",
			"subagents":   []any{rollupPackage + "/worker"},
			"permissions": map[string]any{"writes": all},
		}),
		agent("worker", map[string]any{
			"provider": "rollupllm", "model": "rworker",
			"tools":       []any{map[string]any{"function": vocabulary.HostFunctionWrite}},
			"subagents":   []any{rollupPackage + "/helper"},
			"permissions": map[string]any{"writes": all},
		}),
		agent("helper", map[string]any{
			"provider": "rollupllm", "model": "rhelper",
			"tools":       []any{map[string]any{"function": vocabulary.HostFunctionWrite}},
			"permissions": map[string]any{"writes": []any{rollupMemo}},
		}),
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, docs); err != nil {
		t.Fatalf("install rollup package: %v", err)
	}
	return ds, fake
}

// toolRowsOf returns a thread's tool rows in the order they landed.
func toolRowsOf(t *testing.T, ds *dataset, threadID string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, m := range threadMessages(t, ds, threadID) {
		if m["role"] == "tool" {
			out = append(out, m)
		}
	}
	return out
}

// toolRowNamed returns the one tool row answering the named tool.
func toolRowNamed(t *testing.T, rows []map[string]any, name string) map[string]any {
	t.Helper()
	var found map[string]any
	for _, r := range rows {
		if r["name"] != name {
			continue
		}
		if found != nil {
			t.Fatalf("two tool rows answer %s", name)
		}
		found = r
	}
	if found == nil {
		t.Fatalf("no tool row answers %s", name)
	}
	return found
}

// writesOf reads a tool row's `subagentWrites` as stored: the child thread's
// id out of the reference, the count, the kinds and the overflow count.
func writesOf(t *testing.T, row map[string]any) (thread string, records int, kinds []string, more int) {
	t.Helper()
	w, ok := row["subagentWrites"].(map[string]any)
	if !ok {
		t.Fatalf("tool row %s carries no subagentWrites: %v", row["name"], row)
	}
	path := referencePathOf(w["thread"])
	kind, id, split := vocabulary.SplitRecordPath(path)
	if !split || kind != typeThread {
		t.Fatalf("subagentWrites.thread = %v, want a %s reference", w["thread"], typeThread)
	}
	listed, _ := w["kinds"].([]any)
	for _, k := range listed {
		kinds = append(kinds, k.(string))
	}
	return id, intProp(w, "records"), kinds, intProp(w, "moreKinds")
}

// changeKinds lists the kinds of a row's stamped `changes`.
func changeKinds(row map[string]any) []string {
	var out []string
	for _, ch := range objectRows(row["changes"]) {
		kind, _ := ch["kind"].(string)
		out = append(out, kind)
	}
	return out
}

// childThread loads a thread row and holds it to the agent that ran it and
// the thread that called it.
func childThread(t *testing.T, ds *dataset, id, agent, parent string) {
	t.Helper()
	row, err := ds.Get(context.Background(), typeThread, id)
	if err != nil {
		t.Fatalf("load thread %s: %v", id, err)
	}
	if got := referenceID(row.Properties["agent"]); got != rollupPackage+"/"+agent {
		t.Fatalf("thread %s ran %q, want %s", id, got, agent)
	}
	if got := referenceID(row.Properties[threadRelPare]); got != parent {
		t.Fatalf("thread %s names parent %q, want %s", id, got, parent)
	}
}

func TestSubagentWritesRollUpTheWholeChildChain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := provisionRollupAgents(t)
	fake.script("rlead",
		fakeTurn{calls: []fakeCall{{"worker", toolArgs(t, map[string]any{"input": "file the work"})}}},
		fakeTurn{content: "filed"},
	)
	fake.script("rworker",
		fakeTurn{calls: []fakeCall{
			{"write", writeArgs(t, "put", rollupTask, "t-1", map[string]any{"name": "one"})},
			{"write", writeArgs(t, "put", rollupNote, "n-1", map[string]any{"name": "one"})},
		}},
		// The same task again: a second entry, not a second record.
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "patch", rollupTask, "t-1", map[string]any{"name": "uno"})}}},
		fakeTurn{calls: []fakeCall{{"helper", toolArgs(t, map[string]any{"input": "leave a memo"})}}},
		fakeTurn{content: "worked"},
	)
	fake.script("rhelper",
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", rollupMemo, "m-1", map[string]any{"name": "one"})}}},
		fakeTurn{content: "memo left"},
	)
	res, err := ds.CallAgent(ctx, rollupPackage+"/lead", "go")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.Status != threadOK {
		t.Fatalf("result: %+v", res)
	}

	// The parent's row: the whole chain, grandchild included, and none of the
	// chain's entries copied onto it.
	leadRow := toolRowNamed(t, toolRowsOf(t, ds, res.Thread), "worker")
	if leadRow["ok"] != true {
		t.Fatalf("the sub-agent call failed: %v", leadRow["content"])
	}
	workerThread, records, kinds, more := writesOf(t, leadRow)
	if records != 3 || more != 0 {
		t.Fatalf("lead's row: records %d, moreKinds %d; want 3 and 0", records, more)
	}
	if want := []string{rollupTask, rollupNote, rollupMemo}; !reflect.DeepEqual(kinds, want) {
		t.Fatalf("lead's row kinds %v, want %v", kinds, want)
	}
	if _, held := leadRow["changes"]; held {
		t.Fatalf("the child chain's entries were copied onto the parent row: %v", leadRow["changes"])
	}
	childThread(t, ds, workerThread, "worker", res.Thread)

	// The child's own row for the grandchild: the grandchild's write alone.
	workerRows := toolRowsOf(t, ds, workerThread)
	helperThread, records, kinds, more := writesOf(t, toolRowNamed(t, workerRows, "helper"))
	if records != 1 || more != 0 || !reflect.DeepEqual(kinds, []string{rollupMemo}) {
		t.Fatalf("worker's row: records %d, kinds %v, moreKinds %d; want 1, [%s], 0", records, kinds, more, rollupMemo)
	}
	childThread(t, ds, helperThread, "helper", workerThread)

	// Each write's entry stays on the row that made it.
	var written []string
	for _, r := range workerRows {
		if r["name"] == "write" {
			written = append(written, changeKinds(r)...)
		}
	}
	if want := []string{rollupTask, rollupNote, rollupTask}; !reflect.DeepEqual(written, want) {
		t.Fatalf("worker's write rows stamp %v, want %v", written, want)
	}
	if got := changeKinds(toolRowNamed(t, toolRowsOf(t, ds, helperThread), "write")); !reflect.DeepEqual(got, []string{rollupMemo}) {
		t.Fatalf("helper's write row stamps %v, want [%s]", got, rollupMemo)
	}
	// A write row is not a sub-agent call, so it carries no summary.
	for _, r := range workerRows {
		if _, held := r["subagentWrites"]; held && r["name"] != "helper" {
			t.Fatalf("tool row %s carries a subagentWrites", r["name"])
		}
	}
}

// A chain that writes nothing still stamps its summary, `records: 0` and no
// kinds, so a reader tells "wrote nothing" from a row written before the
// stamp existed; the summary is also where the child thread is named.
func TestSubagentWritesStampsZeroForAChainThatWroteNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := provisionRollupAgents(t)
	fake.script("rlead",
		fakeTurn{calls: []fakeCall{{"worker", toolArgs(t, map[string]any{"input": "just think"})}}},
		fakeTurn{content: "thought"},
	)
	fake.script("rworker", fakeTurn{content: "nothing to write"})
	res, err := ds.CallAgent(ctx, rollupPackage+"/lead", "go")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	row := toolRowNamed(t, toolRowsOf(t, ds, res.Thread), "worker")
	w, _ := row["subagentWrites"].(map[string]any)
	if _, held := w["kinds"]; held {
		t.Fatalf("an empty chain lists kinds: %v", w)
	}
	thread, records, _, more := writesOf(t, row)
	if records != 0 || more != 0 {
		t.Fatalf("records %d, moreKinds %d; want 0 and 0", records, more)
	}
	childThread(t, ds, thread, "worker", res.Thread)
}

// A grandchild whose model fails after a write still counts: the write
// committed, so the child's failed call and the lead's row both report it.
func TestSubagentWritesCountAFailedGrandchildsWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := provisionRollupAgents(t)
	fake.script("rlead",
		fakeTurn{calls: []fakeCall{{"worker", toolArgs(t, map[string]any{"input": "delegate"})}}},
		fakeTurn{content: "done"},
	)
	fake.script("rworker",
		fakeTurn{calls: []fakeCall{{"helper", toolArgs(t, map[string]any{"input": "leave a memo"})}}},
		fakeTurn{content: "helper broke"},
	)
	fake.script("rhelper",
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", rollupMemo, "m-late", map[string]any{"name": "late"})}}},
		fakeTurn{status: 500},
	)
	res, err := ds.CallAgent(ctx, rollupPackage+"/lead", "go")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if _, err := ds.Get(ctx, rollupMemo, "m-late"); err != nil {
		t.Fatalf("the grandchild's write did not land: %v", err)
	}
	leadRow := toolRowNamed(t, toolRowsOf(t, ds, res.Thread), "worker")
	workerThread, records, kinds, _ := writesOf(t, leadRow)
	if records != 1 || !reflect.DeepEqual(kinds, []string{rollupMemo}) {
		t.Fatalf("lead's row: records %d, kinds %v; want 1, [%s]", records, kinds, rollupMemo)
	}
	helperRow := toolRowNamed(t, toolRowsOf(t, ds, workerThread), "helper")
	if helperRow["ok"] != false {
		t.Fatalf("the failed grandchild reported ok: %v", helperRow["content"])
	}
	helperThread, records, kinds, _ := writesOf(t, helperRow)
	if records != 1 || !reflect.DeepEqual(kinds, []string{rollupMemo}) {
		t.Fatalf("worker's row: records %d, kinds %v; want 1, [%s]", records, kinds, rollupMemo)
	}
	childThread(t, ds, helperThread, "helper", workerThread)
}

// A call refused before the child runs opens no thread, writes nothing and
// stamps no summary.
func TestSubagentWritesAbsentWhenTheChildNeverRan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := provisionRollupAgents(t)
	fake.script("rlead",
		fakeTurn{calls: []fakeCall{{"worker", toolArgs(t, map[string]any{})}}},
		fakeTurn{content: "gave up"},
	)
	res, err := ds.CallAgent(ctx, rollupPackage+"/lead", "go")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	row := toolRowNamed(t, toolRowsOf(t, ds, res.Thread), "worker")
	if row["ok"] != false {
		t.Fatalf("a call without input ran: %v", row["content"])
	}
	if _, held := row["subagentWrites"]; held {
		t.Fatalf("a child that never ran stamped a summary: %v", row["subagentWrites"])
	}
}
