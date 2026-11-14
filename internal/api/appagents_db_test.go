package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The agent verbs (decision 0195): `agents:` and `decide:` scopes reach the
// rows of the agents they name, one record at a time.

// otherAgent is an agent under another authority, which `agents:~/chat/*`
// never names.
const otherAgent = "other.example.com/tools/helper"

// appAgentsLLMProvider is the llm/provider row every agent of the fixture
// completes against.
const appAgentsLLMProvider = "fakellm"

// appAgentsEnv is the engine holding the agent machinery's rows for two
// agents: talked, in the repository's own namespace, which
// `agents:~/chat/*` names, and otherAgent. Each has a chat thread (tA, tB)
// with a user message (mA, mB), and a proposed change request (rA, rB)
// patching a person (pa, pb); talked has a second thread, tA2, of another
// mode. With loops, each agent has also run one real turn on the in-process
// provider (appAgentsLLM), on a thread of its own (lA, lB), which asked a
// question and called a function that needs approval: the agent loop alone
// lands an interaction (iA, iB) and an approval (aA, aB).
type appAgentsEnv struct {
	*dbEnv
	talked                         string
	tA, tB, mA, mB                 string
	lA, lB, iA, iB, aA, aB, rA, rB string
	llm                            *appAgentsProvider
}

// chatThreads is every chat thread of talked's.
func (e *appAgentsEnv) chatThreads() []string {
	if e.lA == "" {
		return []string{e.tA}
	}
	return []string{e.tA, e.lA}
}

func newAppAgentsEnv(t *testing.T, loops bool, opts ...dbOption) *appAgentsEnv {
	t.Helper()
	env := newDBEnv(t, append([]dbOption{
		withSamples("people"), withAppFlow(),
		// The second agent lives under another authority, which only the
		// insecure escape lets this repository declare.
		withEngine(engine.WithInsecureForeignPackages(true)),
	}, opts...)...)
	e := &appAgentsEnv{
		dbEnv: env, talked: "~/chat/assistant",
		tA: "tA", tB: "tB", mA: "mA", mB: "mB", rA: "rA", rB: "rB",
	}
	env.put(t, llmPackage+"/provider", appAgentsLLMProvider, map[string]any{
		"wire": "openai", "baseURL": "https://llm.example.com", "apiKey": "row-key",
	})
	chat := env.repository + "/chat"
	send := chat + "/send"
	agent := func(pkg, name string) map[string]any {
		return vocabulary.AgentManifest(pkg, name, map[string]any{
			"description": name + " under test", "prompt": "You are " + name + ".",
			"provider": appAgentsLLMProvider, "model": name,
			"tools":       []any{map[string]any{"function": vocabulary.HostFunctionAsk}, map[string]any{"function": send}},
			"permissions": map[string]any{"writes": []any{vocabulary.KindLLMInteraction, chat + "/sent"}},
		})
	}
	appAgentsApply(t, env,
		vocabulary.PackageManifest(chat, 1),
		vocabulary.KindManifest(chat, map[string]any{"singular": "sent"},
			map[string]any{"properties": map[string]any{"text": map[string]any{"type": "string"}}}),
		vocabulary.FunctionManifest(chat, "send", map[string]any{
			"description": "sends a text",
			"runtime":     vocabulary.RuntimePython,
			"effect":      vocabulary.FunctionEffectIrreversible,
			"approval":    vocabulary.FunctionApprovalRequired,
			"arguments":   []any{map[string]any{"name": "text", "type": "string", "required": true}},
			"permissions": map[string]any{"writes": []any{chat + "/sent"}},
			"source": `
def main(input, host):
    text = input["args"]["text"]
    return {"effects": [{"action": "put", "kind": "` + chat + `/sent", "id": host.ids.minted(text), "properties": {"text": text}}],
            "output": {"sent": text}}
`,
		}),
		agent(chat, "assistant"),
		vocabulary.PackageManifest("other.example.com/tools", 1), agent("other.example.com/tools", "helper"),
	)
	ref := func(kind, id string) map[string]any {
		return map[string]any{vocabulary.ReferenceValueKey: vocabulary.RecordPath(kind, id)}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	thread := func(id, agent, mode string) {
		env.put(t, kindThread, id, map[string]any{
			"agent": ref(kindAgent, agent), "subject": "first", "mode": mode, "status": "ok",
			"provider": appAgentsLLMProvider, "model": "assistant", "agentDepth": 0, "startedAt": now,
		})
	}
	for _, side := range []struct{ thread, message, request, agent, person, name string }{
		{e.tA, e.mA, e.rA, e.talked, "pa", "Ada"}, {e.tB, e.mB, e.rB, otherAgent, "pb", "Bob"},
	} {
		thread(side.thread, side.agent, "chat")
		env.put(t, kindMessage, side.message, map[string]any{
			"thread": ref(kindThread, side.thread), "role": "user", "content": "hello", "turn": 1,
		})
		env.put(t, personKind, side.person, map[string]any{"name": side.name})
		env.put(t, kindRequest, side.request, map[string]any{
			"thread": ref(kindThread, side.thread), "decision": "proposed", "op": "patch",
			"target": ref(personKind, side.person), "diff": map[string]any{"properties": map[string]any{"name": side.name + " L."}},
		})
	}
	thread("tA2", e.talked, "call")
	if loops {
		e.llm = appAgentsLLM(t, env)
		e.lA, e.iA, e.aA = e.askAndSend(t, e.talked, "assistant")
		e.lB, e.iB, e.aB = e.askAndSend(t, otherAgent, "helper")
	}
	return e
}

// askAndSend runs one turn of agent whose model asks a question and calls
// the send function, and answers the thread, the interaction and the
// approval the loop landed.
func (e *appAgentsEnv) askAndSend(t *testing.T, agent, model string) (thread, interaction, approval string) {
	t.Helper()
	ctx := context.Background()
	e.llm.script(model,
		appAgentsTurn{calls: [][2]string{
			{"ask", `{"questions":[{"id":"q","prompt":"which?","options":[{"value":"a","label":"A"}]}]}`},
			{"send", `{"text":"hi"}`},
		}},
		appAgentsTurn{content: "asked, and waiting for your approval"},
	)
	res, err := e.ds.ChatAgent(ctx, substrate.ActorPerson, agent, substrate.ChatInput{Message: "hello"}, func(substrate.AgentEvent) {})
	if err != nil {
		t.Fatalf("run %s: %v", agent, err)
	}
	on := func(kind string) string {
		page, err := e.ds.List(ctx, substrate.Query{Filter: substrate.Filter{
			Kinds: []string{kind}, Properties: map[string]substrate.Cond{"thread": {Eq: res.Thread}},
		}})
		if err != nil || len(page.Records) != 1 {
			t.Fatalf("%s's turn landed %v of %s, %v: want one", agent, page, kind, err)
		}
		return page.Records[0].ID
	}
	return res.Thread, on(kindInteraction), on(kindApproval)
}

// appAgentsUserMessage is the one user message a chat opened its thread
// with.
func appAgentsUserMessage(t *testing.T, e *appAgentsEnv, thread string) *substrate.Record {
	t.Helper()
	page, err := e.ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{
		Kinds: []string{kindMessage}, Properties: map[string]substrate.Cond{"thread": {Eq: thread}, "role": {Eq: "user"}},
	}})
	if err != nil || len(page.Records) != 1 {
		t.Fatalf("the user messages of thread %s = %v, %v: want one", thread, page, err)
	}
	return page.Records[0]
}

// appAgentsApply admits vocabulary documents as the owner.
func appAgentsApply(t *testing.T, env *dbEnv, docs ...map[string]any) {
	t.Helper()
	if _, err := env.ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorPerson, docs); err != nil {
		t.Fatalf("apply vocabulary: %v", err)
	}
}

// appAgentsTurn is one scripted completion: tool calls, by name and JSON
// arguments, or a reply.
type appAgentsTurn struct {
	calls   [][2]string
	content string
}

// appAgentsProvider is an in-process server speaking the OpenAI wire: each
// completion pops the next turn scripted for its model, and one with nothing
// scripted replies "hi".
type appAgentsProvider struct {
	mu      sync.Mutex
	scripts map[string][]appAgentsTurn
}

func (p *appAgentsProvider) script(model string, turns ...appAgentsTurn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.scripts[model] = append(p.scripts[model], turns...)
}

func (p *appAgentsProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	_ = json.NewDecoder(r.Body).Decode(&req)
	model, _ := req["model"].(string)
	turn := appAgentsTurn{content: "hi"}
	p.mu.Lock()
	if queue := p.scripts[model]; len(queue) > 0 {
		turn, p.scripts[model] = queue[0], queue[1:]
	}
	p.mu.Unlock()
	calls := make([]map[string]any, len(turn.calls))
	for i, c := range turn.calls {
		calls[i] = map[string]any{
			"index": i, "id": fmt.Sprintf("call_%d", i), "type": "function",
			"function": map[string]any{"name": c[0], "arguments": c[1]},
		}
	}
	usage := map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
	finish := "stop"
	if len(calls) > 0 {
		finish = "tool_calls"
	}
	if stream, _ := req["stream"].(bool); !stream {
		msg := map[string]any{"role": "assistant", "content": turn.content}
		if len(calls) > 0 {
			msg["tool_calls"] = calls
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "c", "object": "chat.completion", "usage": usage,
			"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": finish}},
		})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	chunk := func(v map[string]any) {
		raw, _ := json.Marshal(v)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
	}
	delta := func(d map[string]any, finish any) {
		chunk(map[string]any{"object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": d, "finish_reason": finish}}})
	}
	if turn.content != "" {
		delta(map[string]any{"content": turn.content}, nil)
	}
	for _, c := range calls {
		delta(map[string]any{"tool_calls": []any{c}}, nil)
	}
	delta(map[string]any{}, finish)
	chunk(map[string]any{"object": "chat.completion.chunk", "choices": []any{}, "usage": usage})
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}

// appAgentsLLM points the fixture's provider row at an appAgentsProvider.
// The engine dials it on loopback, which its egress gate refuses unless the
// operator's escape lists it, so the test sets that variable and cannot run
// in parallel.
func appAgentsLLM(t *testing.T, env *dbEnv) *appAgentsProvider {
	t.Helper()
	t.Setenv("SUBSTRATE_EGRESS_ALLOW", "127.0.0.0/8,::1/128")
	p := &appAgentsProvider{scripts: map[string][]appAgentsTurn{}}
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)
	env.put(t, llmPackage+"/provider", appAgentsLLMProvider, map[string]any{
		"wire": "openai", "baseURL": srv.URL, "apiKey": "row-key",
	})
	return p
}

// appAgentsChatCalls records each chat the engine was handed and whether its
// context still carried the app's scope mark: the handler drops the mark
// before the turn runs (decision 0195), and nothing the turn writes shows
// whether it did, so the hand-off itself is read here, then passed on.
type appAgentsChatCalls struct {
	substrate.Dataset
	mu     *sync.Mutex
	calls  *int
	scoped *bool
}

func (d appAgentsChatCalls) ChatAgent(ctx context.Context, actor substrate.Actor, name string, in substrate.ChatInput, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	d.mu.Lock()
	*d.calls++
	_, *d.scoped = substrate.ScopesFrom(ctx)
	d.mu.Unlock()
	return d.Dataset.ChatAgent(ctx, actor, name, in, emit)
}

func listOf(kind, props string) string {
	f := `{"kinds":["` + kind + `"]`
	if props != "" {
		f += `,"properties":` + props
	}
	return "/api/v1/records?filter=" + url.QueryEscape(f+"}")
}

func recordIDs(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	wantStatus(t, rec, http.StatusOK)
	page := decodeJSON[substrate.Page](t, rec)
	ids := make([]string, len(page.Records))
	for i, r := range page.Records {
		ids[i] = r.ID
	}
	return strings.Join(ids, ",")
}

func recordPath(kind, id string) string {
	return "/api/v1/" + kind + "/" + url.PathEscape(id)
}

// read reads a path as bearer.
func (e *appAgentsEnv) read(t *testing.T, path, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	return appScopedDo(t, e.dbEnv, http.MethodGet, path, bearer, nil, "")
}

// Each agent's turn ran on the in-process provider (appAgentsLLM), so it
// sets the egress escape and cannot run in parallel.
func TestAnAgentsScopeReadsTheRowsOfItsAgentsAlone(t *testing.T) {
	e := newAppAgentsEnv(t, true)
	app := appScopedToken(t, e.dbEnv, "agents:"+e.repository+"/chat/*")

	wantStatus(t, e.read(t, recordPath(kindAgent, e.talked), app), http.StatusOK)
	wantStatus(t, e.read(t, recordPath(kindAgent, otherAgent), app), http.StatusNotFound)
	if got := recordIDs(t, e.read(t, listOf(kindAgent, ""), app)); got != e.talked {
		t.Fatalf("the agent list = %q, want the matching agent alone", got)
	}
	// The engine reads the matching agent alone: one row under first=1, no
	// cursor past it, and a count of one, where a list read whole and cut
	// afterwards would page and count the other agents too.
	one := e.read(t, listOf(kindAgent, "")+"&first=1&count=1", app)
	wantStatus(t, one, http.StatusOK)
	if page := decodeJSON[substrate.Page](t, one); len(page.Records) != 1 || page.Records[0].ID != e.talked ||
		page.Cursor != "" || page.Count == nil || *page.Count != 1 {
		t.Fatalf("the agent list's one page = %+v, want the matching agent alone, counted once", page)
	}

	wantStatus(t, e.read(t, recordPath(kindThread, e.tA), app), http.StatusOK)
	wantStatus(t, e.read(t, recordPath(kindThread, e.tB), app), http.StatusNotFound)
	// The app's own condition stands beside the agent narrowing: tA2 is the
	// matching agent's thread of another mode, the B threads another
	// agent's chats.
	if got := recordIDs(t, e.read(t, listOf(kindThread, `{"mode":{"eq":"chat"}}`), app)); !appAgentsSameIDs(got, e.chatThreads()...) {
		t.Fatalf("the thread list = %q, want the matching agent's chat threads %v alone", got, e.chatThreads())
	}
	wantStatus(t, e.read(t, listOf(kindThread, `{"agent":{"eq":"`+otherAgent+`"}}`), app), http.StatusNotFound)
	wantStatus(t, e.read(t, listOf(kindThread, `{"agent":{"exists":true}}`), app), http.StatusForbidden)

	// Messages and interactions narrow by thread, a bare id or a path; a
	// thread the token cannot read is answered as missing.
	for _, cond := range []string{`{"thread":{"eq":"` + e.tA + `"}}`, `{"thread":{"in":["` + kindThread + `/` + e.tA + `"]},"role":{"eq":"user"}}`} {
		if got := recordIDs(t, e.read(t, listOf(kindMessage, cond), app)); got != e.mA {
			t.Fatalf("messages under %s = %q, want the matching thread's alone", cond, got)
		}
	}
	wantStatus(t, e.read(t, listOf(kindMessage, `{"thread":{"in":["`+e.tA+`","`+e.tB+`"]}}`), app), http.StatusNotFound)
	if got := recordIDs(t, e.read(t, listOf(kindInteraction, `{"state":{"eq":"pending"},"thread":{"eq":"`+e.lA+`"}}`), app)); got != e.iA {
		t.Fatalf("pending interactions = %q, want the matching thread's alone", got)
	}
	if got := recordIDs(t, e.read(t, listOf(kindApproval, `{"state":{"eq":"pending"},"thread":{"eq":"`+e.lA+`"}}`), app)); got != e.aA {
		t.Fatalf("pending approvals = %q, want the matching thread's alone", got)
	}
	wantStatus(t, e.read(t, listOf(kindInteraction, `{"thread":{"eq":"`+e.lB+`"}}`), app), http.StatusNotFound)
	// A list that names no thread is refused before the engine pages: its
	// cursor would carry the last scanned row, another agent's included,
	// and its count every agent's rows.
	for _, kind := range []string{kindMessage, kindInteraction, kindApproval} {
		wantStatus(t, e.read(t, listOf(kind, ``), app), http.StatusForbidden)
		wantStatus(t, e.read(t, listOf(kind, `{"state":{"eq":"pending"}}`), app), http.StatusForbidden)
		wantStatus(t, e.read(t, listOf(kind, ``)+"&count=1", app), http.StatusForbidden)
	}
	counted := e.read(t, listOf(kindMessage, `{"thread":{"eq":"`+e.tA+`"}}`)+"&count=1", app)
	wantStatus(t, counted, http.StatusOK)
	if page := decodeJSON[substrate.Page](t, counted); page.Count == nil || *page.Count != 1 {
		t.Fatalf("a named count answered %+v, want the thread's one message counted", page.Count)
	}
	// A read scope that reaches the whole kind lists it without a thread.
	whole := appScopedToken(t, e.dbEnv, "agents:"+e.talked, "read:"+kindMessage)
	all, err := e.ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{Kinds: []string{kindMessage}}, First: 500})
	if err != nil {
		t.Fatal(err)
	}
	var every []string
	for _, r := range all.Records {
		every = append(every, r.ID)
	}
	if got := recordIDs(t, e.read(t, listOf(kindMessage, ``)+"&first=500", whole)); !appAgentsSameIDs(got, every...) || !strings.Contains(got, e.mB) {
		t.Fatalf("messages under a whole-kind read = %q, want every one: %v", got, every)
	}
	wantStatus(t, e.read(t, recordPath(kindMessage, e.mB), app), http.StatusNotFound)

	// A row-gated kind is listed on its own, and never searched.
	two := "/api/v1/records?filter=" + url.QueryEscape(`{"kinds":["`+kindThread+`","`+kindMessage+`"]}`)
	wantStatus(t, e.read(t, two, app), http.StatusForbidden)
	wantStatus(t, e.read(t, "/api/v1/records?q=hello&filter="+url.QueryEscape(`{"kinds":["`+kindMessage+`"]}`), app), http.StatusForbidden)
	// A list that names no kind lists what the token reads whole: nothing.
	if got := recordIDs(t, e.read(t, "/api/v1/records", app)); got != "" {
		t.Fatalf("a list naming no kind = %q, want nothing", got)
	}
	// The decide verb's kind is out of reach without it.
	wantStatus(t, e.read(t, recordPath(kindRequest, e.rA), app), http.StatusNotFound)
}

// appAgentsSameIDs reports whether a comma-joined id list holds exactly want,
// in any order.
func appAgentsSameIDs(got string, want ...string) bool {
	have := strings.Split(got, ",")
	if got == "" {
		have = nil
	}
	if len(have) != len(want) {
		return false
	}
	seen := map[string]int{}
	for _, id := range have {
		seen[id]++
	}
	for _, id := range want {
		if seen[id] == 0 {
			return false
		}
		seen[id]--
	}
	return true
}

// Each agent's turn ran on the in-process provider (appAgentsLLM), so it
// sets the egress escape and cannot run in parallel.
func TestAnAgentsScopeWritesARenameAndAnAnswerAndNothingElse(t *testing.T) {
	e := newAppAgentsEnv(t, true)
	app := appScopedToken(t, e.dbEnv, "agents:"+e.repository+"/chat/*")
	patch := func(kind, id, body string) *httptest.ResponseRecorder {
		return appScopedDo(t, e.dbEnv, http.MethodPatch, recordPath(kind, id), app, []byte(body), "application/json")
	}

	wantStatus(t, patch(kindThread, "tA", `{"properties":{"subject":"Renamed"}}`), http.StatusOK)
	if got := e.get(t, kindThread, "tA"); got.Properties["subject"] != "Renamed" {
		t.Fatalf("the rename stored %v", got.Properties["subject"])
	}
	if actor := appScopedLastActor(t, e.dbEnv, kindThread, "tA"); actor != appActor {
		t.Fatalf("the rename was written as %q, want the app's actor", actor)
	}
	// The answer carries its precondition to the engine: a stale one is the
	// engine's conflict and answers nothing.
	v := e.get(t, kindInteraction, e.iA).Version
	answer := func(ifVersion int64) *httptest.ResponseRecorder {
		return patch(kindInteraction, e.iA, fmt.Sprintf(`{"properties":{"state":"answered","answers":[{"question":"q","selected":["a"]}]},"ifVersion":%d}`, ifVersion))
	}
	wantErrorCode(t, answer(v+1), http.StatusConflict, codeConflict)
	if got := e.get(t, kindInteraction, e.iA); got.Properties["state"] != "pending" {
		t.Fatalf("a stale answer moved the interaction to %v", got.Properties["state"])
	}
	wantStatus(t, answer(v), http.StatusOK)
	if got := e.get(t, kindInteraction, e.iA); got.Properties["state"] != "answered" {
		t.Fatalf("the answer left the interaction %v", got.Properties["state"])
	}
	// An approval is approved or declined by its state alone (decision 0254).
	av := e.get(t, kindApproval, e.aA).Version
	wantStatus(t, patch(kindApproval, e.aA, fmt.Sprintf(`{"properties":{"state":"approved"},"ifVersion":%d}`, av)), http.StatusOK)
	if got := e.get(t, kindApproval, e.aA); got.Properties["state"] == "pending" {
		t.Fatalf("the approval stayed pending: %+v", got.Properties)
	}

	for _, c := range []struct{ kind, id, body string }{
		{kindThread, "tA", `{"properties":{"status":"stopped"}}`},
		{kindThread, "tA", `{"properties":{"subject":"x"},"labels":{"app:notes.example.org/x":"y"}}`},
		{kindInteraction, e.iA, `{"properties":{"state":"dismissed","thread":"tB"}}`},
		{kindApproval, e.aA, `{"properties":{"state":"approved","arguments":{"text":"changed"}}}`},
		{kindMessage, "mA", `{"properties":{"content":"x"}}`},
		{kindAgent, e.talked, `{"properties":{"hiddenFromChat":true}}`},
	} {
		rec := patch(c.kind, c.id, c.body)
		wantStatus(t, rec, http.StatusForbidden)
		if wa := rec.Header().Get("WWW-Authenticate"); !strings.Contains(wa, "insufficient_scope") {
			t.Fatalf("PATCH %s %s answered WWW-Authenticate %q", c.kind, c.body, wa)
		}
	}
	if got := e.get(t, kindThread, "tA"); got.Properties["subject"] != "Renamed" || got.Properties["status"] != "ok" {
		t.Fatalf("a refused patch changed the thread: %+v", got.Properties)
	}
	if got := e.get(t, kindMessage, "mA"); got.Properties["content"] != "hello" {
		t.Fatalf("a refused patch changed the message: %+v", got.Properties)
	}
	wantStatus(t, patch(kindThread, "tB", `{"properties":{"subject":"x"}}`), http.StatusNotFound)
	wantStatus(t, appScopedDo(t, e.dbEnv, http.MethodPut, recordPath(kindThread, mintedID("tC")), app, []byte(`{"properties":{"subject":"x"}}`), "application/json"), http.StatusForbidden)
	wantStatus(t, appScopedDo(t, e.dbEnv, http.MethodDelete, recordPath(kindThread, "tA"), app, nil, ""), http.StatusForbidden)
	if got := e.get(t, kindThread, "tA"); got == nil || got.DeletedAt != nil {
		t.Fatalf("a refused delete took the thread: %+v", got)
	}
	for _, path := range []string{
		"/api/v1/" + corePackage + "/agent/" + url.PathEscape(e.talked) + "/call",
		"/api/v1/" + corePackage + "/function/x/call",
	} {
		wantStatus(t, appScopedDo(t, e.dbEnv, http.MethodPost, path, app, []byte(`{"input":"x"}`), "application/json"), http.StatusForbidden)
	}
}

// A read scope over the thread kind reads every thread, and the rename stays
// the agent verb's: its own agents' threads, and a refusal, not a miss, for
// the rest.
func TestARenameUnderAReadScopeOverThreadsIsStillTheAgentVerbs(t *testing.T) {
	t.Parallel()
	e := newAppAgentsEnv(t, false)
	app := appScopedToken(t, e.dbEnv, "agents:"+e.repository+"/chat/*", "read:"+kindThread)
	rename := func(id string) *httptest.ResponseRecorder {
		return appScopedDo(t, e.dbEnv, http.MethodPatch, recordPath(kindThread, id), app, []byte(`{"properties":{"subject":"x"}}`), "application/json")
	}
	wantStatus(t, e.read(t, recordPath(kindThread, "tB"), app), http.StatusOK)
	wantStatus(t, rename("tA"), http.StatusOK)
	wantStatus(t, rename("tB"), http.StatusForbidden)
	if got := e.get(t, kindThread, "tB"); got.Properties["subject"] != "first" {
		t.Fatalf("the refused rename stored %v", got.Properties["subject"])
	}
}

// Runs a real turn on the in-process provider, so it sets the egress escape
// (appAgentsLLM) and cannot run in parallel.
func TestAnAppChatsWithTheAgentsItsScopesNameAsItself(t *testing.T) {
	e := newAppAgentsEnv(t, false)
	appAgentsLLM(t, e.dbEnv)
	var mu sync.Mutex
	calls, scoped := 0, false
	e.wrap = func(ds substrate.Dataset) substrate.Dataset {
		return appAgentsChatCalls{Dataset: ds, mu: &mu, calls: &calls, scoped: &scoped}
	}
	app := appScopedToken(t, e.dbEnv, "agents:"+e.repository+"/chat/*")
	chat := func(agent, body string) *httptest.ResponseRecorder {
		return appScopedDo(t, e.dbEnv, http.MethodPost, "/api/v1/"+corePackage+"/agent/"+url.PathEscape(agent)+"/chat", app, []byte(body), "application/json")
	}

	before := appScopedCount(t, e.dbEnv, kindThread)
	got := chat(e.talked, `{"message":"hi"}`)
	wantStatus(t, got, http.StatusOK)
	if !strings.Contains(got.Body.String(), `"done"`) {
		t.Fatalf("the chat streamed %s, want its events", got.Body.String())
	}
	var first substrate.AgentEvent
	if err := json.Unmarshal([]byte(strings.SplitN(got.Body.String(), "\n", 2)[0]), &first); err != nil || first.Thread == "" {
		t.Fatalf("the chat's first line = %s, want the thread", got.Body.String())
	}
	thread := e.get(t, kindThread, first.Thread)
	if thread == nil || appScopedRefPath(thread.Properties["agent"]) != vocabulary.RecordPath(kindAgent, e.talked) {
		t.Fatalf("the turn opened %+v, want a thread of the agent the chat named", thread)
	}
	// The turn's user message is the app's own write, under its token.
	if user := appAgentsUserMessage(t, e, first.Thread); appScopedActors(t, e.dbEnv, kindMessage, user.ID)[0] != appActor {
		t.Fatalf("the turn's message was written as %v, want the app's actor", appScopedActors(t, e.dbEnv, kindMessage, user.ID))
	}
	mu.Lock()
	if calls != 1 || scoped {
		t.Fatalf("the engine was handed %d chats, scoped %v: want one, the agent unmarked", calls, scoped)
	}
	mu.Unlock()
	messages := func(thread string) int {
		page, err := e.ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{
			Kinds: []string{kindMessage}, Properties: map[string]substrate.Cond{"thread": {Eq: thread}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		return len(page.Records)
	}
	onTA := messages("tA")
	wantStatus(t, chat(e.talked, `{"message":"again","thread":"tA"}`), http.StatusOK)
	if n := messages("tA"); n <= onTA {
		t.Fatalf("the continued chat left thread tA with %d messages, had %d", n, onTA)
	}
	threads := appScopedCount(t, e.dbEnv, kindThread)
	if threads != before+1 {
		t.Fatalf("two chats, one continued, left %d threads from %d", threads, before)
	}
	mu.Lock()
	handed := calls
	mu.Unlock()
	onTB := messages("tB")
	wantStatus(t, chat(otherAgent, `{"message":"hi"}`), http.StatusNotFound)
	wantStatus(t, chat(e.talked, `{"message":"hi","thread":"tB"}`), http.StatusNotFound)
	wantStatus(t, chat("assistant", `{"message":"hi"}`), http.StatusUnprocessableEntity)
	mu.Lock()
	if calls != handed {
		t.Fatal("a refused chat reached the engine")
	}
	mu.Unlock()
	if appScopedCount(t, e.dbEnv, kindThread) != threads || messages("tB") != onTB {
		t.Fatal("a refused chat wrote a thread or a message")
	}

	// Attaching and stopping reach the token's threads alone.
	events := e.read(t, "/api/v1/"+llmPackage+"/thread/tA/events", app)
	wantStatus(t, events, http.StatusOK)
	if !strings.Contains(events.Body.String(), `"idle"`) {
		t.Fatalf("attaching to an idle thread answered %s", events.Body.String())
	}
	wantStatus(t, e.read(t, "/api/v1/"+llmPackage+"/thread/tB/events", app), http.StatusNotFound)
	if stop := appScopedDo(t, e.dbEnv, http.MethodPost, "/api/v1/"+llmPackage+"/thread/tA/stop", app, nil, ""); stop.Code == http.StatusNotFound || stop.Code == http.StatusForbidden {
		t.Fatalf("stopping the token's own thread answered %d %s", stop.Code, stop.Body.String())
	}
	wantStatus(t, appScopedDo(t, e.dbEnv, http.MethodPost, "/api/v1/"+llmPackage+"/thread/tB/stop", app, nil, ""), http.StatusNotFound)

	// A token without an agents scope reaches none of it.
	reader := appScopedToken(t, e.dbEnv, "read:"+personKind)
	wantStatus(t, appScopedDo(t, e.dbEnv, http.MethodPost, "/api/v1/"+corePackage+"/agent/"+url.PathEscape(e.talked)+"/chat", reader, []byte(`{"message":"hi"}`), "application/json"), http.StatusForbidden)
	wantStatus(t, e.read(t, "/api/v1/"+llmPackage+"/thread/tA/events", reader), http.StatusForbidden)
}

// A chat's context is read under the agent's grant, so an app's token names
// only records it reads itself, and one it does not is answered as a scoped
// read answers it, before the engine runs. It runs real turns on the
// in-process provider (appAgentsLLM), so it cannot run in parallel.
func TestAnAppsChatContextNamesOnlyRecordsTheTokenReads(t *testing.T) {
	e := newAppAgentsEnv(t, false)
	appAgentsLLM(t, e.dbEnv)
	// A second agent that reads people, so a turn may be shown one.
	librarian := e.repository + "/chat/librarian"
	appAgentsApply(t, e.dbEnv, vocabulary.AgentManifest(e.repository+"/chat", "librarian", map[string]any{
		"description": "librarian under test", "prompt": "You are librarian.",
		"provider": appAgentsLLMProvider, "model": "librarian",
		"permissions": map[string]any{"reads": map[string]any{"kinds": []any{personKind}}},
	}))
	var mu sync.Mutex
	calls, scoped := 0, false
	e.wrap = func(ds substrate.Dataset) substrate.Dataset {
		return appAgentsChatCalls{Dataset: ds, mu: &mu, calls: &calls, scoped: &scoped}
	}
	e.put(t, personKind, "ada", map[string]any{"name": "Ada"})
	chat := func(tok, agent, body string) *httptest.ResponseRecorder {
		return appScopedDo(t, e.dbEnv, http.MethodPost, "/api/v1/"+corePackage+"/agent/"+url.PathEscape(agent)+"/chat", tok, []byte(body), "application/json")
	}
	handed := func() int {
		mu.Lock()
		defer mu.Unlock()
		return calls
	}
	ada := vocabulary.RecordPath(personKind, "ada")
	threads := appScopedCount(t, e.dbEnv, kindThread)

	talker := appScopedToken(t, e.dbEnv, "agents:"+e.repository+"/chat/*")
	about := func(path string) *httptest.ResponseRecorder {
		kind, id, _ := vocabulary.SplitRecordPath(path)
		read := e.read(t, recordPath(kind, id), talker)
		got := chat(talker, e.talked, `{"message":"hi","context":["`+path+`"]}`)
		wantStatus(t, got, http.StatusNotFound)
		if gotCode, readCode := decodeJSON[substrate.ErrorEnvelope](t, got).Error.Code, decodeJSON[substrate.ErrorEnvelope](t, read).Error.Code; got.Code != read.Code || gotCode != readCode {
			t.Fatalf("a chat about %s answered %d %s, want what a read answers: %d %s", path, got.Code, gotCode, read.Code, readCode)
		}
		return got
	}
	if exists, missing := about(ada).Body.String(), about(vocabulary.RecordPath(personKind, "nobody")).Body.String(); exists != missing {
		t.Fatalf("a chat about an unread kind tells a record that exists (%s) from one that does not (%s)", exists, missing)
	}
	about(vocabulary.RecordPath(kindThread, "tB"))
	wantStatus(t, chat(talker, e.talked, `{"message":"hi","context":["ada"]}`), http.StatusUnprocessableEntity)
	if handed() != 0 {
		t.Fatal("a chat about a record the token does not read reached the engine")
	}
	if n := appScopedCount(t, e.dbEnv, kindThread); n != threads {
		t.Fatalf("a refused chat opened a thread: %d from %d", n, threads)
	}

	// A thread the token talks to is a record it reads, so the chat about it
	// reaches the engine, which shows a turn only what its agent reads: this
	// agent reads nothing.
	got := chat(talker, e.talked, `{"message":"hi","context":["`+vocabulary.RecordPath(kindThread, "tA")+`"]}`)
	wantErrorCode(t, got, http.StatusUnprocessableEntity, codeValidation)
	if !strings.Contains(got.Body.String(), "declares no reads") || handed() != 1 {
		t.Fatalf("a chat about a readable thread answered %s after %d hand-offs, want the engine's refusal", got.Body.String(), handed())
	}
	reader := appScopedToken(t, e.dbEnv, "agents:"+e.repository+"/chat/*", "read:"+personKind)
	got = chat(reader, librarian, `{"message":"hi","context":["`+ada+`"]}`)
	wantStatus(t, got, http.StatusOK)
	if handed() != 2 {
		t.Fatalf("the engine was handed %d chats, want 2", handed())
	}
	// The engine read the record the chat named into the turn's user message.
	var first substrate.AgentEvent
	if err := json.Unmarshal([]byte(strings.SplitN(got.Body.String(), "\n", 2)[0]), &first); err != nil || first.Thread == "" {
		t.Fatalf("the chat's first line = %s, want the thread", got.Body.String())
	}
	if raw, _ := json.Marshal(appAgentsUserMessage(t, e, first.Thread).Properties); !strings.Contains(string(raw), ada) {
		t.Fatalf("the turn's user message carries no context record %s: %s", ada, raw)
	}
}

func TestADecideScopeDecidesWhatItsAgentsPropose(t *testing.T) {
	t.Parallel()
	e := newAppAgentsEnv(t, false)
	app := appScopedToken(t, e.dbEnv, "decide:"+e.talked)

	wantStatus(t, e.read(t, recordPath(kindRequest, "rA"), app), http.StatusOK)
	wantStatus(t, e.read(t, recordPath(kindRequest, "rB"), app), http.StatusNotFound)
	if got := recordIDs(t, e.read(t, listOf(kindRequest, `{"decision":{"eq":"proposed"},"thread":{"eq":"tA"}}`), app)); got != "rA" {
		t.Fatalf("proposed requests = %q, want the matching agent's alone", got)
	}
	wantStatus(t, e.read(t, listOf(kindRequest, `{"decision":{"eq":"proposed"}}`), app), http.StatusForbidden)

	v := e.get(t, kindRequest, "rA").Version
	body := fmt.Sprintf(`{"properties":{"decision":"accepted","adjustedDiff":{"properties":{"name":"x"}}},"annotations":{"%s/note":"ok"},"ifVersion":%d}`, appActor, v)
	wantStatus(t, appScopedDo(t, e.dbEnv, http.MethodPatch, recordPath(kindRequest, "rA"), app, []byte(body), "application/json"), http.StatusOK)
	// The engine admitted the decision the request's path marked, as the app,
	// and the accept applied the owner's adjusted values to the target.
	if got := e.get(t, kindRequest, "rA"); got.Properties["decision"] != "accepted" {
		t.Fatalf("the request stayed %v", got.Properties["decision"])
	}
	if actor := appScopedLastActor(t, e.dbEnv, kindRequest, "rA"); actor != appActor {
		t.Fatalf("the decision was written as %q, want the app", actor)
	}
	if got := e.get(t, personKind, "pa"); got.Properties["name"] != "x" {
		t.Fatalf("the accept left the target %v, want the adjusted name", got.Properties["name"])
	}
	wantStatus(t, appScopedDo(t, e.dbEnv, http.MethodPatch, recordPath(kindRequest, "rA"), app, []byte(`{"properties":{"diff":{}}}`), "application/json"), http.StatusForbidden)
	bv := e.get(t, kindRequest, "rB").Version
	wantStatus(t, appScopedDo(t, e.dbEnv, http.MethodPatch, recordPath(kindRequest, "rB"), app, []byte(fmt.Sprintf(`{"properties":{"decision":"rejected"},"ifVersion":%d}`, bv)), "application/json"), http.StatusNotFound)
	if got := e.get(t, kindRequest, "rB"); got.Properties["decision"] != "proposed" {
		t.Fatalf("another agent's request moved to %v", got.Properties["decision"])
	}

	// Deciding is not talking: the threads and the chat stay out of reach.
	wantStatus(t, e.read(t, recordPath(kindThread, "tA"), app), http.StatusNotFound)
	wantStatus(t, appScopedDo(t, e.dbEnv, http.MethodPost, "/api/v1/"+corePackage+"/agent/"+url.PathEscape(e.talked)+"/chat", app, []byte(`{"message":"hi"}`), "application/json"), http.StatusForbidden)
}

func TestTheApprovalPageNamesTheAgentsAnAgentScopeMatches(t *testing.T) {
	t.Parallel()
	e := newAppAgentsEnv(t, false)
	id := appScopedPark(t, e.dbEnv, appRequest("agents:~/chat/* decide:* read:"+personKind))
	view := appScopedShow(t, e.dbEnv, id)
	wantStatus(t, view, http.StatusOK)
	got := decodeJSON[substrate.AuthorizationRequest](t, view)
	if len(got.Scopes) != 3 {
		t.Fatalf("scopes = %+v", got.Scopes)
	}
	agents, decide, read := got.Scopes[0], got.Scopes[1], got.Scopes[2]
	if agents.Verb != "agents" || agents.Scope != "agents:~/chat/*" || agents.Requested != "agents:~/chat/*" ||
		!agents.Glob || agents.Write || len(agents.Kinds) != 0 || len(agents.Agents) != 1 || agents.Agents[0].ID != e.talked {
		t.Fatalf("the agents scope = %+v", agents)
	}
	// decide:* matches every agent the repository declares: the two the
	// fixture declares among them.
	matched := map[string]bool{}
	for _, a := range decide.Agents {
		matched[a.ID] = true
	}
	if decide.Verb != "decide" || !matched[e.talked] || !matched[otherAgent] {
		t.Fatalf("the decide scope = %+v", decide)
	}
	if read.Verb != "read" || len(read.Agents) != 0 || len(read.Kinds) != 1 {
		t.Fatalf("the read scope = %+v", read)
	}
	rec := appScopedApprove(t, e.dbEnv, id, []string{agents.Scope, decide.Scope}, 30)
	wantStatus(t, rec, http.StatusOK)
	tok := decodeJSON[tokenResponse](t, appScopedExchange(t, e.dbEnv, tokenForm(codeFrom(t, decodeJSON[substrate.AuthorizationAnswer](t, rec).Redirect))))
	if tok.Scope != agents.Scope+" decide:*" {
		t.Fatalf("the token's scope = %q", tok.Scope)
	}
}

// appAgentsWideFeed adds a record of another kind to the affected list of
// one entry the feed reads. The change event names every record an entry
// moved (decision 0061), and an entry the engine writes today moves records
// of its own kind alone, a merge's two included, so an entry that also moved
// a record the token does not read is put on the feed here.
type appAgentsWideFeed struct {
	substrate.Dataset
	kind, id string
	also     substrate.AffectedRecord
}

func (d appAgentsWideFeed) widen(changes []substrate.Change) []substrate.Change {
	for i, c := range changes {
		if c.Kind == d.kind && c.RecordID == d.id {
			changes[i].Affected = append(append([]substrate.AffectedRecord(nil), c.Affected...), d.also)
		}
	}
	return changes
}

func (d appAgentsWideFeed) Changes(ctx context.Context, after int64, f substrate.ChangeFilter, limit int) ([]substrate.Change, error) {
	changes, err := d.Dataset.Changes(ctx, after, f, limit)
	return d.widen(changes), err
}

func (d appAgentsWideFeed) ChangesBefore(ctx context.Context, before int64, f substrate.ChangeFilter, limit int) ([]substrate.Change, error) {
	changes, err := d.Dataset.ChangesBefore(ctx, before, f, limit)
	return d.widen(changes), err
}

func TestAnAppTailsTheChangeFeedHeldToWhatItReads(t *testing.T) {
	t.Parallel()
	e := newAppAgentsEnv(t, false)
	// A tag is a kind the token does not read.
	tag := e.repository + "/feed/tag"
	appAgentsApply(t, e.dbEnv,
		vocabulary.PackageManifest(e.repository+"/feed", 1),
		vocabulary.KindManifest(e.repository+"/feed", map[string]any{"singular": "tag"}, map[string]any{"properties": map[string]any{
			"label": map[string]any{"type": "string"},
		}}),
	)
	e.wrap = func(ds substrate.Dataset) substrate.Dataset {
		return appAgentsWideFeed{Dataset: ds, kind: personKind, id: "p1", also: substrate.AffectedRecord{Kind: tag, ID: "n0", Version: 3}}
	}
	app := appScopedToken(t, e.dbEnv, "read:"+personKind, "agents:"+e.repository+"/chat/*")

	// A text search and the values read across kinds and into payloads, on
	// every feed route; the history itself is open (decision 0219,
	// TestAnAppReadsTheFeedsHistoryCutToWhatItReads).
	for _, q := range []string{"?q=ada", "?values=1", "?before=0&q=ada", "?runs=1&values=1", "?watch=1&q=ada", "?watch=1&values=1"} {
		rec := e.read(t, "/api/v1/changes"+q, app)
		wantStatus(t, rec, http.StatusForbidden)
		if wa := rec.Header().Get("WWW-Authenticate"); !strings.Contains(wa, "insufficient_scope") {
			t.Fatalf("changes%s answered WWW-Authenticate %q", q, wa)
		}
	}
	wantStatus(t, e.read(t, "/api/v1/changes?watch=1&kinds="+tag, app), http.StatusNotFound)

	srv := httptest.NewServer(e.h)
	defer srv.Close()
	br, stop := startWatch(t, srv, "/api/v1/changes?watch=1", app)
	defer stop()
	bookmark := readLine(t, br)
	if _, ok := bookmark["bookmark"]; !ok || bookmark["generation"] == "" {
		t.Fatalf("the first line = %v, want the bookmark and its generation", bookmark)
	}

	// A batch of rows the token may not see ahead of one it may: the tail
	// moves past them rather than reading them again.
	for i := range changeBatch + 10 {
		e.put(t, tag, fmt.Sprintf("n%d", i), map[string]any{"label": "secret"})
	}
	e.put(t, personKind, "p1", map[string]any{"name": "Ada"})
	patch := func(kind, id string, props map[string]any) {
		if _, err := e.ds.Patch(context.Background(), substrate.ActorSystem, kind, id, substrate.PatchInput{Properties: props}); err != nil {
			t.Fatalf("patch %s/%s: %v", kind, id, err)
		}
	}
	patch(kindMessage, "mB", map[string]any{"content": "secret"})
	patch(kindMessage, "mA", map[string]any{"content": "secret"})
	appAgentsApply(t, e.dbEnv, vocabulary.AgentManifest("other.example.com/tools", "helper", map[string]any{
		"description": "helper, rewritten", "prompt": "You are helper.", "provider": appAgentsLLMProvider, "model": "helper",
	}))
	patch(kindThread, "tA", map[string]any{"subject": "secret"})

	want := []struct{ kind, id string }{{personKind, "p1"}, {kindMessage, "mA"}, {kindThread, "tA"}}
	for i, w := range want {
		line := readLine(t, br)
		if line["kind"] != w.kind || line["recordId"] != w.id {
			t.Fatalf("row %d = %v, want %s %s", i, line, w.kind, w.id)
		}
		for _, key := range []string{"payload", "hash", "triggers"} {
			if _, has := line[key]; has {
				t.Fatalf("row %d carries %s: %v", i, key, line)
			}
		}
		for _, key := range []string{"seq", "ts", "op", "actor"} {
			if _, has := line[key]; !has {
				t.Fatalf("row %d lacks %s: %v", i, key, line)
			}
		}
		if i == 0 {
			affected, _ := line["affected"].([]any)
			if len(affected) != 1 {
				t.Fatalf("the person row's affected = %v, want the person alone", line["affected"])
			}
			if a := affected[0].(map[string]any); a["kind"] != personKind || a["properties"] != nil {
				t.Fatalf("affected = %v", a)
			}
		}
	}
}

// A watch an app's token opened ends once the token is revoked: the next
// row it would carry is a terminal auth frame instead, never the row.
func TestARevokedAppTokensWatchEndsBeforeItsNextRow(t *testing.T) {
	t.Parallel()
	e := newAppAgentsEnv(t, false)
	app := appScopedToken(t, e.dbEnv, "agents:"+e.repository+"/chat/*")
	srv := httptest.NewServer(e.h)
	defer srv.Close()
	br, stop := startWatch(t, srv, "/api/v1/changes?watch=1", app)
	defer stop()
	if line := readLine(t, br); line["bookmark"] == nil {
		t.Fatalf("the first line = %v, want the bookmark", line)
	}

	form := url.Values{"token": {app}}
	wantStatus(t, appScopedDo(t, e.dbEnv, http.MethodPost, "/oauth/revoke", "", []byte(form.Encode()), "application/x-www-form-urlencoded"), http.StatusOK)
	if _, err := e.ds.Patch(context.Background(), substrate.ActorSystem, kindThread, "tA", substrate.PatchInput{Properties: map[string]any{"subject": "after"}}); err != nil {
		t.Fatal(err)
	}

	line := readLine(t, br)
	if line["recordId"] != nil {
		t.Fatalf("a revoked token's watch carried a row: %v", line)
	}
	problem, _ := line["error"].(map[string]any)
	if problem["code"] != codeAuth {
		t.Fatalf("the line after the revoke = %v, want the terminal auth frame", line)
	}
}

// keyRefusal is the engine's refusal of a model call whose key the vendor
// refused: plain text under substrate.ErrKeyRefused.
type keyRefusal string

func (e keyRefusal) Error() string        { return string(e) }
func (e keyRefusal) Is(target error) bool { return target == substrate.ErrKeyRefused }

// appAgentsRefusingChat answers every chat with err before its first event.
// The engine refuses a chat for its key only after the stream's first event,
// once the turn met the vendor, and for an operator's allowance only where
// an operator's defaults serve the agent, which this engine has none of; the
// handler's answer to either before the first event is what is under test.
type appAgentsRefusingChat struct {
	substrate.Dataset
	err *error
}

func (d appAgentsRefusingChat) ChatAgent(context.Context, substrate.Actor, string, substrate.ChatInput, func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	return nil, *d.err
}

// A chat the token may hold that fails because the vendor refused the key,
// or because the operator's allowance is spent, is no scope failure: a key
// refusal answers 503 `unavailable` with the reason `key_refused` and a
// Retry-After, the allowance 403 `guard`, and neither carries
// `WWW-Authenticate: insufficient_scope`, which would send the app to ask
// for scopes that change nothing.
func TestAScopedChatRefusedForTheKeyOrTheAllowanceIsNoScopeFailure(t *testing.T) {
	t.Parallel()
	e := newAppAgentsEnv(t, false)
	app := appScopedToken(t, e.dbEnv, "agents:"+e.repository+"/chat/*")
	var refusal error
	e.wrap = func(ds substrate.Dataset) substrate.Dataset { return appAgentsRefusingChat{Dataset: ds, err: &refusal} }
	chat := func() *httptest.ResponseRecorder {
		return appScopedDo(t, e.dbEnv, http.MethodPost, "/api/v1/"+corePackage+"/agent/"+url.PathEscape(e.talked)+"/chat", app, []byte(`{"message":"hi"}`), "application/json")
	}

	const text = "OpenAI refused this key (401). Add a working key on the OpenAI card under Providers."
	refusal = keyRefusal(text)
	got := chat()
	wantStatus(t, got, http.StatusServiceUnavailable)
	p := decodeJSON[substrate.ErrorEnvelope](t, got).Error
	if p.Code != codeUnavailable || p.Reason != substrate.ReasonKeyRefused || p.Message != text {
		t.Fatalf("the refusal answered %+v, want unavailable/key_refused with the plain text", p)
	}
	if ra := got.Header().Get("Retry-After"); ra != "60" {
		t.Fatalf("Retry-After = %q, want 60", ra)
	}
	if wa := got.Header().Get("WWW-Authenticate"); wa != "" {
		t.Fatalf("a refused key answered WWW-Authenticate %q", wa)
	}

	refusal = fmt.Errorf("%w: today's allowance from Example is used up", substrate.ErrAllowance)
	got = chat()
	wantStatus(t, got, http.StatusForbidden)
	if p := decodeJSON[substrate.ErrorEnvelope](t, got).Error; p.Code != codeGuard || p.Reason != substrate.ReasonQuota {
		t.Fatalf("the allowance answered %+v, want guard/quota", p)
	}
	if wa := got.Header().Get("WWW-Authenticate"); wa != "" {
		t.Fatalf("the allowance answered WWW-Authenticate %q", wa)
	}
}
