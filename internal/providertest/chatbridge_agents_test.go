package providertest

// The shipped policies through a real agent loop: the owner's assistant
// reacts, takes a reaction back and marks a chat read unasked through the
// package's tools, and every other agent write of a chataction waits as a
// change request. The model is a scripted server on the OpenAI wire, which
// the seeded `anthropic` row is pointed at.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const cbMoss = "substrate.reamde.dev/assistant/moss"

// cbCall is one tool call a scripted turn makes.
type cbCall struct {
	name string
	args map[string]any
}

// cbTurn is one scripted answer: its tool calls, or its text when it makes
// none.
type cbTurn struct {
	calls []cbCall
	text  string
}

// cbModel answers /chat/completions from a script, one turn per request that
// offers tools. A request offering none (a thread's title) is answered with
// text and takes no turn, so the script is the loop's alone.
type cbModel struct {
	mu    sync.Mutex
	turns []cbTurn
	srv   *httptest.Server
}

// cbAssistant points the seeded `anthropic` row, which Moss names, at a
// scripted model.
func cbAssistant(t *testing.T, ds substrate.Dataset) *cbModel {
	t.Helper()
	m := &cbModel{}
	m.srv = httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(m.srv.Close)
	if _, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{
		Kind: vocabulary.PackageLLM + "/provider", ID: "anthropic",
		Properties: map[string]any{"wire": "openai", "baseURL": m.srv.URL, "apiKey": "row-key-chatbridge"},
	}); err != nil {
		t.Fatalf("point the anthropic row at the scripted model: %v", err)
	}
	return m
}

func (m *cbModel) left() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.turns)
}

func (m *cbModel) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/chat/completions" {
		http.NotFound(w, r)
		return
	}
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	turn := cbTurn{text: "A chat"}
	if tools, _ := req["tools"].([]any); len(tools) > 0 {
		m.mu.Lock()
		if len(m.turns) == 0 {
			m.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprint(w, `{"error":{"message":"the script is spent"}}`)
			return
		}
		turn, m.turns = m.turns[0], m.turns[1:]
		m.mu.Unlock()
	}
	calls := make([]any, 0, len(turn.calls))
	for i, c := range turn.calls {
		args, _ := json.Marshal(c.args)
		calls = append(calls, map[string]any{
			"index": i, "id": fmt.Sprintf("call_%d", i), "type": "function",
			"function": map[string]any{"name": c.name, "arguments": string(args)},
		})
	}
	usage := map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
	if stream, _ := req["stream"].(bool); !stream {
		msg := map[string]any{"role": "assistant", "content": turn.text}
		if len(calls) > 0 {
			msg["tool_calls"] = calls
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "cmpl", "object": "chat.completion", "usage": usage,
			"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": "stop"}},
		})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	chunk := func(payload map[string]any) {
		buf, _ := json.Marshal(payload)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", buf)
	}
	delta := func(d map[string]any, finish any) {
		chunk(map[string]any{"object": "chat.completion.chunk", "choices": []any{
			map[string]any{"index": 0, "delta": d, "finish_reason": finish},
		}})
	}
	if turn.text != "" {
		delta(map[string]any{"content": turn.text}, nil)
	}
	for _, c := range calls {
		delta(map[string]any{"tool_calls": []any{c}}, nil)
	}
	delta(map[string]any{}, "stop")
	chunk(map[string]any{"object": "chat.completion.chunk", "choices": []any{}, "usage": usage})
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}

// ask has an agent make one tool call and then answer, and reports the
// chatactions and change requests the run added.
func (m *cbModel) ask(t *testing.T, ds substrate.Dataset, agent string, call cbCall) (actions, requests []*substrate.Record) {
	t.Helper()
	acts, reqs := cbLive(t, ds, cbAction), cbLive(t, ds, vocabulary.KindRecordPatchRequest)
	m.mu.Lock()
	m.turns = append(m.turns, cbTurn{calls: []cbCall{call}}, cbTurn{text: "done"})
	m.mu.Unlock()
	res, err := ds.CallAgent(context.Background(), agent, "do what the chat needs")
	if err != nil {
		t.Fatalf("%s calling %s: %v", agent, call.name, err)
	}
	if res.Status != "ok" || m.left() != 0 {
		t.Fatalf("%s calling %s settled %s (%s) with %d scripted turns unread",
			agent, call.name, res.Status, res.Reason, m.left())
	}
	return cbAdded(t, ds, cbAction, acts), cbAdded(t, ds, vocabulary.KindRecordPatchRequest, reqs)
}

func cbLive(t *testing.T, ds substrate.Dataset, kind string) map[string]bool {
	t.Helper()
	ids := map[string]bool{}
	for _, r := range listLive(t, ds, kind) {
		ids[r.ID] = true
	}
	return ids
}

func cbAdded(t *testing.T, ds substrate.Dataset, kind string, before map[string]bool) []*substrate.Record {
	t.Helper()
	var out []*substrate.Record
	for _, r := range listLive(t, ds, kind) {
		if !before[r.ID] {
			out = append(out, r)
		}
	}
	return out
}

func cbTool(fn string) string { return vocabulary.OfferedToolName(chatBridgePkg, fn) }

// cbUnasked holds a run to one queued chataction of the action named, and
// nothing held.
func cbUnasked(t *testing.T, what, action string, actions, requests []*substrate.Record) {
	t.Helper()
	if len(requests) != 0 || len(actions) != 1 || actions[0].Properties["action"] != action ||
		actions[0].Properties["state"] != "queued" {
		t.Fatalf("%s: wrote %d chatactions and %d change requests, want one queued %s and nothing held (%v)",
			what, len(actions), len(requests), action, cbProps(actions))
	}
}

// cbHeld holds a run to one change request for a chataction of the action
// named, converted by the shipped gate, and no chataction. fn is the tool
// whose effect the gate held, empty for an agent's direct write.
func cbHeld(t *testing.T, what, action, fn string, actions, requests []*substrate.Record) {
	t.Helper()
	if len(actions) != 0 || len(requests) != 1 {
		t.Fatalf("%s: wrote %d chatactions and %d change requests, want one request and no action (%v)",
			what, len(actions), len(requests), cbProps(actions))
	}
	req := requests[0]
	diff, _ := req.Properties["diff"].(map[string]any)
	props, _ := diff["properties"].(map[string]any)
	policy := refIDs(req, "policy")
	function := storedRefPath(req.Properties["function"])
	if req.Properties["targetKind"] != cbAction || len(policy) != 1 || policy[0] != cbPolicy ||
		props["action"] != action || (fn == "") != (function == "") ||
		(fn != "" && !strings.HasSuffix(function, "/"+chatBridgePkg+"/"+fn)) {
		t.Fatalf("%s: the change request is %v", what, req.Properties)
	}
}

func cbProps(rows []*substrate.Record) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Properties)
	}
	return out
}

// The owner's assistant reacts, takes a reaction back and marks a chat read
// unasked, through the tools the shipped allows name; its sends, edits and
// deletes through the tools wait, as does its direct write of a reaction and
// another agent's reaction through the same tool. The owner's own writes are
// never held, and the owner's edit of a shipped allow survives an install.
func TestChatBridgeAssistantReactsAndMarksReadUnasked(t *testing.T) {
	ds := newChatBridge(t)
	ctx := context.Background()
	s := &cbStream{}
	cbOpen(t, ds, s, cbDirect)
	cbDeliver(t, ds, cbBatch(
		s.message(cbDirect, "theirs", map[string]any{"text": "hi"}),
		s.message(cbDirect, "mine", map[string]any{"sender": cbAda, "fromOwner": true, "text": "hey"}),
	), nil, "2026-11-09T12:00:00Z")
	direct := cbChatID(cbDirect)
	theirs, mine := cbMessageID(cbDirect, "theirs"), cbMessageID(cbDirect, "mine")
	model := cbAssistant(t, ds)

	for _, tc := range []struct {
		fn   string
		args map[string]any
	}{
		{"react", map[string]any{"message": theirs, "reactionKey": "👍"}},
		{"unreact", map[string]any{"message": theirs, "reactionKey": "👍"}},
		{"markread", map[string]any{"chat": direct, "upTo": theirs}},
	} {
		acts, reqs := model.ask(t, ds, cbMoss, cbCall{cbTool(tc.fn), tc.args})
		cbUnasked(t, "the assistant's "+tc.fn, tc.fn, acts, reqs)
	}
	for _, tc := range []struct {
		fn   string
		args map[string]any
	}{
		{"send", map[string]any{"chat": direct, "text": "on my way"}},
		{"edit", map[string]any{"message": mine, "text": "hey there"}},
		{"delete", map[string]any{"message": mine, "forEveryone": true}},
	} {
		acts, reqs := model.ask(t, ds, cbMoss, cbCall{cbTool(tc.fn), tc.args})
		cbHeld(t, "the assistant's "+tc.fn, tc.fn, tc.fn, acts, reqs)
	}

	// The allow names the tool: the same reaction written directly waits.
	acts, reqs := model.ask(t, ds, cbMoss, cbCall{"write", map[string]any{
		"op": "put", "kind": cbAction, "input": map[string]any{"properties": map[string]any{
			"action": "react", "chat": cbPath(cbChat, direct),
			"target": cbPath(cbMessage, theirs), "reactionKey": "👍",
		}},
	}})
	cbHeld(t, "the assistant's direct write of a reaction", "react", "", acts, reqs)

	// The allow names the assistant: another agent's reaction waits.
	pkg := testdb.Repository(t) + "/helpers"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.AgentManifest(pkg, "scribe", map[string]any{
			"description": "another agent the owner runs", "prompt": "You help with chats.",
			"provider": "anthropic", "model": "claude-opus-5-5",
			"toolsFrom": []any{map[string]any{"functions": "*"}},
			"permissions": map[string]any{
				"reads": map[string]any{"kinds": []any{"*"}}, "writes": []any{"*"},
			},
		}),
	}); err != nil {
		t.Fatalf("declare another agent: %v", err)
	}
	acts, reqs = model.ask(t, ds, pkg+"/scribe", cbCall{cbTool("react"), map[string]any{"message": theirs, "reactionKey": "🎉"}})
	cbHeld(t, "another agent's react", "react", "react", acts, reqs)

	// The owner's own writes, a tool call and a put, are never held.
	before, held := cbLive(t, ds, cbAction), cbLive(t, ds, vocabulary.KindRecordPatchRequest)
	if _, _, err := ds.CallFunction(ctx, substrate.ActorAPI, chatBridgePkg+"/send",
		map[string]any{"chat": direct, "text": "running late"}); err != nil {
		t.Fatalf("the owner's send: %v", err)
	}
	cbOwnerAction(t, ds, "owner-delete", map[string]any{
		"action": "delete", "chat": cbPath(cbChat, direct), "target": cbPath(cbMessage, mine), "forEveryone": true,
	})
	if got := cbAdded(t, ds, cbAction, before); len(got) != 2 ||
		len(cbAdded(t, ds, vocabulary.KindRecordPatchRequest, held)) != 0 {
		t.Fatalf("the owner's writes: %d chatactions landed, want both and nothing held", len(got))
	}

	// The owner turns the reaction allow off; installing again keeps it off,
	// and the assistant's reaction waits again.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindRecordPatchPolicy, "chatbridge-assistant-react",
		substrate.PatchInput{Properties: map[string]any{"disabled": true}}); err != nil {
		t.Fatalf("turn the allow off: %v", err)
	}
	installShipped(t, ds, chatBridgePkg)
	if got := mustGet(t, ds, vocabulary.KindRecordPatchPolicy, "chatbridge-assistant-react"); got.Properties["disabled"] != true {
		t.Fatalf("installing again undid the owner's edit of the allow: %v", got.Properties)
	}
	acts, reqs = model.ask(t, ds, cbMoss, cbCall{cbTool("react"), map[string]any{"message": theirs, "reactionKey": "👀"}})
	cbHeld(t, "the assistant's react with the allow off", "react", "react", acts, reqs)
	acts, reqs = model.ask(t, ds, cbMoss, cbCall{cbTool("markread"), map[string]any{"chat": direct}})
	cbUnasked(t, "the assistant's markread beside it", "markread", acts, reqs)
}
