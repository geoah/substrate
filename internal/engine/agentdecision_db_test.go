package engine

// The decision loop's regressions (agentdecision.go): a propose stamps the
// proposing thread on the request; a dispatch's committed writes ride the
// tool row as engine-stamped `changes`; deciding a thread-borne request
// writes ONE `system` message into that thread — verdict, target, version,
// the decision's own changelog entries — and RESUMES it, the continuation
// replaying the system row to the model as user content.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// waitUntil polls until check passes or the deadline lapses — the decision
// resume is a post-commit goroutine, so its rows arrive shortly after the
// deciding write returns, never inside it.
func waitUntil(t *testing.T, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// changesOfRow reads a message row's engine-stamped `changes`.
func changesOfRow(m map[string]any) []map[string]any {
	items, _ := m["changes"].([]any)
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if entry, ok := item.(map[string]any); ok {
			out = append(out, entry)
		}
	}
	return out
}

func changesName(entries []map[string]any, kind string) (map[string]any, bool) {
	for _, e := range entries {
		if e["kind"] == kind {
			return e, true
		}
	}
	return nil, false
}

func onlyPatchRequest(t *testing.T, ds *dataset) *substrate.Record {
	t.Helper()
	var id string
	if err := ds.db.QueryRowContext(context.Background(), `
		SELECT id FROM records WHERE kind = $1 AND deleted_at IS NULL`,
		vocabulary.KindRecordPatchRequest).Scan(&id); err != nil {
		t.Fatalf("no request landed: %v", err)
	}
	e, err := ds.Get(context.Background(), vocabulary.KindRecordPatchRequest, id)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func systemMessages(t *testing.T, ds *dataset, threadID string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, m := range threadMessages(t, ds, threadID) {
		if m["role"] == msgRoleSystem {
			out = append(out, m)
		}
	}
	return out
}

func TestProposalDecisionReportsAndResumesThread(t *testing.T) {
	t.Parallel()
	// The whole loop: propose stamps the thread and the tool row's `changes`;
	// the owner's accept applies the diff, writes the system message into the
	// proposing thread in the SAME transaction, and resumes the thread, whose
	// continuation hands the envelope to the model as user content.
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewAuthority + "/widget", ID: "w-loop", Properties: map[string]any{"name": "raw"},
	}); err != nil {
		t.Fatalf("put widget: %v", err)
	}
	fake.script("root",
		fakeTurn{calls: []fakeCall{{"propose", `{"kind":"crew.test.dev/widget","target":"w-loop","diff":{"properties":{"name":"better"}},"rationale":"tidy"}`}}},
		fakeTurn{content: "proposed."},
		// The RESUME's completion: the loop replays the decision envelope and
		// the model acknowledges.
		fakeTurn{content: "acknowledged: the widget moved."},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/classifier", "tidy the widget")
	if err != nil {
		t.Fatalf("call: %v", err)
	}

	// The request carries the proposing thread, stamped by the loop.
	req := onlyPatchRequest(t, ds)
	if got := req.Properties["thread"]; got != vocabulary.RecordPath(typeThread, res.Thread) {
		t.Fatalf("request thread = %v, want the proposing thread", got)
	}
	// The propose tool row carries the request's changelog entry.
	tool := lastToolMessage(t, ds, res.Thread)
	entry, ok := changesName(changesOfRow(tool), vocabulary.KindRecordPatchRequest)
	if !ok {
		t.Fatalf("the propose row carries no request change: %+v", tool["changes"])
	}
	if entry["id"] != req.ID || entry["op"] != "put" {
		t.Fatalf("request change entry: %+v", entry)
	}
	if seq, _ := anyFloat(entry["seq"]); seq <= 0 {
		t.Fatalf("request change entry has no seq: %+v", entry)
	}

	// The owner accepts. The system message lands in the SAME transaction.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindRecordPatchRequest, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: &req.Version,
	}); err != nil {
		t.Fatalf("accept: %v", err)
	}
	widget, err := ds.Get(ctx, crewAuthority+"/widget", "w-loop")
	if err != nil || widget.Properties["name"] != "better" {
		t.Fatalf("the accepted diff did not land: %+v %v", widget, err)
	}
	system := systemMessages(t, ds, res.Thread)
	if len(system) != 1 {
		t.Fatalf("system messages: %d, want 1", len(system))
	}
	content, _ := system[0]["content"].(string)
	var env map[string]any
	if err := json.Unmarshal([]byte(content), &env); err != nil {
		t.Fatalf("the system message is not an envelope: %q", content)
	}
	if env["event"] != "proposalDecision" || env["decision"] != "accepted" || env["op"] != "patch" {
		t.Fatalf("envelope: %+v", env)
	}
	if env["request"] != vocabulary.RecordPath(vocabulary.KindRecordPatchRequest, req.ID) {
		t.Fatalf("envelope request: %v", env["request"])
	}
	if env["target"] != vocabulary.RecordPath(widget.Kind, widget.ID) {
		t.Fatalf("envelope target: %v", env["target"])
	}
	if v, _ := anyFloat(env["version"]); int64(v) != widget.Version {
		t.Fatalf("envelope version %v, widget version %d", env["version"], widget.Version)
	}
	// The decision's own entries ride the message: the request's patch AND the
	// target's apply.
	wrote := changesOfRow(system[0])
	if _, ok := changesName(wrote, vocabulary.KindRecordPatchRequest); !ok {
		t.Fatalf("the decision's changes miss the request patch: %+v", wrote)
	}
	if w, ok := changesName(wrote, crewAuthority+"/widget"); !ok || w["op"] != "patch" {
		t.Fatalf("the decision's changes miss the target apply: %+v", wrote)
	}

	// The thread RESUMES: the scripted acknowledgement lands as a fresh
	// assistant turn and the thread settles ok again.
	waitUntil(t, "the resume's acknowledgement", func() bool {
		for _, m := range threadMessages(t, ds, res.Thread) {
			if m["role"] == "assistant" && m["content"] == "acknowledged: the widget moved." {
				return true
			}
		}
		return false
	})
	waitUntil(t, "the resumed thread settling", func() bool {
		threads := agentThreadsOf(t, ds, "classifier")
		return len(threads) == 1 && threads[0]["status"] == threadOK
	})
	// No new user row rode the resume: the system row IS the turn.
	users := 0
	for _, m := range threadMessages(t, ds, res.Thread) {
		if m["role"] == "user" {
			users++
		}
	}
	if users != 1 {
		t.Fatalf("user rows after resume: %d, want the original alone", users)
	}
	// And the model SAW the envelope: the resume's completion request carries
	// it as user content (system rows replay as user — no wire admits a
	// mid-thread system role).
	requests := fake.requestsOf("root")
	last := requests[len(requests)-1]
	messages, _ := last["messages"].([]any)
	seen := false
	for _, m := range messages {
		msg, _ := m.(map[string]any)
		if msg["role"] == "user" {
			if s, _ := msg["content"].(string); strings.Contains(s, "proposalDecision") {
				seen = true
			}
		}
	}
	if !seen {
		t.Fatalf("the resume's completion never saw the envelope: %+v", messages)
	}
}

func TestProposalRejectionReportsBack(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewAuthority + "/widget", ID: "w-kept", Properties: map[string]any{"name": "raw"},
	}); err != nil {
		t.Fatalf("put widget: %v", err)
	}
	fake.script("root",
		fakeTurn{calls: []fakeCall{{"propose", `{"kind":"crew.test.dev/widget","target":"w-kept","diff":{"properties":{"name":"worse"}},"rationale":"meh"}`}}},
		fakeTurn{content: "proposed."},
		fakeTurn{content: "understood, dropping it."},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/classifier", "change the widget")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	req := onlyPatchRequest(t, ds)
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindRecordPatchRequest, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "rejected"}, IfVersion: &req.Version,
	}); err != nil {
		t.Fatalf("reject: %v", err)
	}
	// Nothing applied.
	if got, err := ds.Get(ctx, crewAuthority+"/widget", "w-kept"); err != nil || got.Properties["name"] != "raw" {
		t.Fatalf("the rejected diff applied: %+v %v", got, err)
	}
	system := systemMessages(t, ds, res.Thread)
	if len(system) != 1 {
		t.Fatalf("system messages: %d, want 1", len(system))
	}
	content, _ := system[0]["content"].(string)
	var env map[string]any
	if err := json.Unmarshal([]byte(content), &env); err != nil {
		t.Fatalf("the system message is not an envelope: %q", content)
	}
	if env["decision"] != "rejected" || env["version"] != nil {
		t.Fatalf("envelope: %+v", env)
	}
	// The rejection's changes carry the request's own patch and nothing of the
	// target.
	wrote := changesOfRow(system[0])
	if _, ok := changesName(wrote, crewAuthority+"/widget"); ok {
		t.Fatalf("a rejection wrote the target: %+v", wrote)
	}
	if _, ok := changesName(wrote, vocabulary.KindRecordPatchRequest); !ok {
		t.Fatalf("the rejection's changes miss the request patch: %+v", wrote)
	}
	waitUntil(t, "the resume after rejection", func() bool {
		for _, m := range threadMessages(t, ds, res.Thread) {
			if m["role"] == "assistant" && m["content"] == "understood, dropping it." {
				return true
			}
		}
		return false
	})
}

func TestMutateStampsChangesOnToolRow(t *testing.T) {
	t.Parallel()
	// A mutate dispatch's committed writes ride the tool row: one entry per
	// changelog row, the seq addressing the delta. A thread-less decision
	// (putRequest writes no thread) stamps and resumes nothing — the quiet
	// half is TestAgentMutateDecidesRequestsWithinEmit, untouched.
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("mut",
		fakeTurn{calls: []fakeCall{{"mutate", gqlToolArgs(t, map[string]any{
			"query": `mutation { put(input: {kind: "crew.test.dev/widget", id: "w-stamped", properties: {name: "made"}}) { id } }`,
		})}}},
		fakeTurn{content: "made it"},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/editor", "make a widget")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	tool := lastToolMessage(t, ds, res.Thread)
	if tool["ok"] != true {
		t.Fatalf("mutate failed: %v", tool["content"])
	}
	entries := changesOfRow(tool)
	if len(entries) != 1 {
		t.Fatalf("changes entries: %d, want 1 (%+v)", len(entries), entries)
	}
	e := entries[0]
	if e["kind"] != crewAuthority+"/widget" || e["id"] != "w-stamped" || e["op"] != "put" {
		t.Fatalf("entry: %+v", e)
	}
	seq, _ := anyFloat(e["seq"])
	// The seq addresses the real entry: the changelog row at that seq is the
	// widget's put.
	changes, err := ds.Changes(ctx, int64(seq)-1, substrate.ChangeFilter{RecordID: "w-stamped"}, 1)
	if err != nil || len(changes) != 1 || changes[0].Seq != int64(seq) {
		t.Fatalf("the stamped seq does not address the entry: %v %v", changes, err)
	}
}

func TestFailedDispatchStampsNoChanges(t *testing.T) {
	t.Parallel()
	// A refused mutate rolls its transaction back, so the tool row carries no
	// `changes` — the sink flushes after commit, never before.
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("mut",
		fakeTurn{calls: []fakeCall{{"mutate", gqlToolArgs(t, map[string]any{
			"query": `mutation { put(input: {kind: "tasks.substrate.reamde.dev/task", id: "t-refused", properties: {title: "no"}}) { id } }`,
		})}}},
		fakeTurn{content: "refused"},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/editor", "write a task")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	tool := lastToolMessage(t, ds, res.Thread)
	if tool["ok"] != false {
		t.Fatalf("an out-of-emit mutate reported ok: %v", tool["content"])
	}
	if tool["changes"] != nil {
		t.Fatalf("a refused dispatch stamped changes: %+v", tool["changes"])
	}
}
