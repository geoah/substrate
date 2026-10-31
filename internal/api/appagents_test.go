package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The agent verbs (decision 0195): `agents:` and `decide:` scopes reach the
// rows of the agents they name, one record at a time.

const (
	talkedAgent = fakeRepository + "/chat/assistant"
	otherAgent  = "other.example.com/tools/helper"
	appActor    = "app:notes.example.org"
)

// chatRecord is what the fake's chat was asked, and what the context it was
// asked under carried.
type chatRecord struct {
	mu       sync.Mutex
	calls    int
	actor    substrate.Actor
	name     string
	thread   string
	scoped   bool
	decision string
}

// agentDataset runs a chat as far as its first event and remembers the
// patch's decision mark, which the fake's own methods ignore.
type agentDataset struct {
	*mintingDataset
	rec *chatRecord
}

func (d *agentDataset) ChatAgent(ctx context.Context, actor substrate.Actor, name, thread, _ string, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	d.rec.mu.Lock()
	d.rec.calls++
	d.rec.actor, d.rec.name, d.rec.thread = actor, name, thread
	_, d.rec.scoped = substrate.ScopesFrom(ctx)
	d.rec.mu.Unlock()
	emit(substrate.AgentEvent{Kind: substrate.AgentEventThread, Thread: "tA"})
	res := &substrate.AgentResult{Thread: "tA", Status: "ok", Reply: "hi"}
	emit(substrate.AgentEvent{Kind: substrate.AgentEventDone, Result: res})
	return res, nil
}

func (d *agentDataset) Patch(ctx context.Context, actor substrate.Actor, typ, id string, in substrate.PatchInput) (*substrate.Record, error) {
	d.rec.mu.Lock()
	d.rec.decision = substrate.DecisionFrom(ctx)
	d.rec.mu.Unlock()
	return d.mintingDataset.Patch(ctx, actor, typ, id, in)
}

func ref(kind, id string) map[string]any {
	return map[string]any{vocabulary.ReferenceValueKey: vocabulary.RecordPath(kind, id)}
}

func agentKinds() []substrate.KindInfo {
	internal := func(identity, name, pkg string, props map[string]any) substrate.KindInfo {
		return substrate.KindInfo{
			Identity: identity, Name: name, Authority: coreAuthorityName, Package: pkg, Version: 1, Source: "builtin",
			Definition: map[string]any{"purpose": vocabulary.PurposeInternal, "properties": props},
		}
	}
	threadRef := map[string]any{"type": "reference", "kind": kindThread}
	return []substrate.KindInfo{
		internal(kindAgent, "agent", "core", map[string]any{
			"hiddenFromChat": map[string]any{"type": "bool"},
			"provider":       map[string]any{"type": "reference", "kind": llmPackage + "/provider"},
		}),
		internal(kindThread, "thread", "llm", map[string]any{
			"subject": map[string]any{"type": "string"},
			"status":  map[string]any{"type": "enum"},
			"mode":    map[string]any{"type": "enum"},
			"agent":   map[string]any{"type": "reference", "kind": kindAgent},
			"parent":  threadRef,
		}),
		internal(kindMessage, "message", "llm", map[string]any{
			"role": map[string]any{"type": "enum"}, "content": map[string]any{"type": "markdown"}, "thread": threadRef,
		}),
		internal(kindInteraction, "interaction", "llm", map[string]any{
			"state": map[string]any{"type": "state"}, "answers": map[string]any{"type": "object"}, "thread": threadRef,
		}),
		internal(kindRequest, "recordpatchrequest", "core", map[string]any{
			"decision": map[string]any{"type": "state"}, "adjustedDiff": map[string]any{"type": "json"},
			"diff": map[string]any{"type": "json"}, "thread": threadRef,
			"target": map[string]any{"type": "reference"},
		}),
	}
}

// newAgentEnv is an app environment whose repository declares the agent
// machinery and holds two agents: one under the repository's own authority,
// which `agents:~/chat/*` names, and one under another. Each has a thread, a
// message, a pending interaction and a proposed change request.
func newAgentEnv(t *testing.T) (*appEnv, *chatRecord) {
	t.Helper()
	e := newAppEnv(t)
	rec := &chatRecord{}
	e.svc.wrap = func(d *fakeDataset) substrate.Dataset {
		return &agentDataset{mintingDataset: &mintingDataset{fakeDataset: d, svc: e.svc}, rec: rec}
	}
	ds := e.svc.datasets[fakeRepository]
	ds.types = append(ds.types, agentKinds()...)
	e.seed(substrate.Record{ID: talkedAgent, Kind: kindAgent, Version: 1}, nil)
	e.seed(substrate.Record{ID: otherAgent, Kind: kindAgent, Version: 1}, nil)
	for _, side := range []struct{ suffix, agent string }{{"A", talkedAgent}, {"B", otherAgent}} {
		thread := "t" + side.suffix
		e.seed(substrate.Record{ID: thread, Kind: kindThread, Version: 1, Properties: map[string]any{
			"agent": ref(kindAgent, side.agent), "subject": "first", "mode": "chat", "status": "ok",
		}}, nil)
		e.seed(substrate.Record{ID: "m" + side.suffix, Kind: kindMessage, Version: 1, Properties: map[string]any{
			"thread": ref(kindThread, thread), "role": "user", "content": "hello",
		}}, nil)
		e.seed(substrate.Record{ID: "i" + side.suffix, Kind: kindInteraction, Version: 1, Properties: map[string]any{
			"thread": ref(kindThread, thread), "state": "pending",
		}}, nil)
		e.seed(substrate.Record{ID: "r" + side.suffix, Kind: kindRequest, Version: 1, Properties: map[string]any{
			"thread": ref(kindThread, thread), "decision": "proposed",
		}}, nil)
	}
	return e, rec
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

func TestAnAgentsScopeReadsTheRowsOfItsAgentsAlone(t *testing.T) {
	e, _ := newAgentEnv(t)
	app := e.grant("agents:~/chat/*", []string{"agents:" + fakeRepository + "/chat/*"})
	ds := e.svc.datasets[fakeRepository]

	wantStatus(t, e.do(http.MethodGet, recordPath(kindAgent, talkedAgent), app, nil, ""), http.StatusOK)
	wantStatus(t, e.do(http.MethodGet, recordPath(kindAgent, otherAgent), app, nil, ""), http.StatusNotFound)
	if got := recordIDs(t, e.do(http.MethodGet, listOf(kindAgent, ""), app, nil, "")); got != talkedAgent {
		t.Fatalf("the agent list = %q, want the matching agent alone", got)
	}
	if ids := ds.lastQuery.Filter.IDs; len(ids) != 1 || ids[0] != talkedAgent {
		t.Fatalf("the agent list was narrowed to %v, want the engine to read the matching agent alone", ids)
	}

	wantStatus(t, e.do(http.MethodGet, recordPath(kindThread, "tA"), app, nil, ""), http.StatusOK)
	wantStatus(t, e.do(http.MethodGet, recordPath(kindThread, "tB"), app, nil, ""), http.StatusNotFound)
	if got := recordIDs(t, e.do(http.MethodGet, listOf(kindThread, `{"mode":{"eq":"chat"}}`), app, nil, "")); got != "tA" {
		t.Fatalf("the thread list = %q, want the matching agent's thread alone", got)
	}
	c := ds.lastQuery.Filter.Properties
	if in := c["agent"].In; len(in) != 1 || in[0] != vocabulary.RecordPath(kindAgent, talkedAgent) || c["mode"].Eq != "chat" {
		t.Fatalf("the thread list's conditions = %+v, want the app's beside the agent narrowing", c)
	}
	wantStatus(t, e.do(http.MethodGet, listOf(kindThread, `{"agent":{"eq":"`+otherAgent+`"}}`), app, nil, ""), http.StatusNotFound)
	wantStatus(t, e.do(http.MethodGet, listOf(kindThread, `{"agent":{"exists":true}}`), app, nil, ""), http.StatusForbidden)

	// Messages and interactions narrow by thread, a bare id or a path; a
	// thread the token cannot read is answered as missing.
	for _, cond := range []string{`{"thread":{"eq":"tA"}}`, `{"thread":{"in":["` + kindThread + `/tA"]},"role":{"eq":"user"}}`} {
		if got := recordIDs(t, e.do(http.MethodGet, listOf(kindMessage, cond), app, nil, "")); got != "mA" {
			t.Fatalf("messages under %s = %q, want the matching thread's alone", cond, got)
		}
	}
	wantStatus(t, e.do(http.MethodGet, listOf(kindMessage, `{"thread":{"in":["tA","tB"]}}`), app, nil, ""), http.StatusNotFound)
	if got := recordIDs(t, e.do(http.MethodGet, listOf(kindInteraction, `{"state":{"eq":"pending"},"thread":{"eq":"tA"}}`), app, nil, "")); got != "iA" {
		t.Fatalf("pending interactions = %q, want the matching thread's alone", got)
	}
	// A list that names no thread is refused before the engine pages: its
	// cursor would carry the last scanned row, another agent's included.
	for _, kind := range []string{kindMessage, kindInteraction} {
		wantStatus(t, e.do(http.MethodGet, listOf(kind, ``), app, nil, ""), http.StatusForbidden)
		wantStatus(t, e.do(http.MethodGet, listOf(kind, `{"state":{"eq":"pending"}}`), app, nil, ""), http.StatusForbidden)
	}
	// A read scope that reaches the whole kind lists it without a thread.
	whole := e.grant("agents:~/chat/assistant read:"+kindMessage, []string{"agents:" + talkedAgent, "read:" + kindMessage})
	if got := recordIDs(t, e.do(http.MethodGet, listOf(kindMessage, ``), whole, nil, "")); got != "mA,mB" && got != "mB,mA" {
		t.Fatalf("messages under a whole-kind read = %q, want every one", got)
	}
	wantStatus(t, e.do(http.MethodGet, recordPath(kindMessage, "mB"), app, nil, ""), http.StatusNotFound)

	// A row-gated kind is listed on its own, and never searched.
	two := "/api/v1/records?filter=" + url.QueryEscape(`{"kinds":["`+kindThread+`","`+kindMessage+`"]}`)
	wantStatus(t, e.do(http.MethodGet, two, app, nil, ""), http.StatusForbidden)
	wantStatus(t, e.do(http.MethodGet, "/api/v1/records?q=hello&filter="+url.QueryEscape(`{"kinds":["`+kindMessage+`"]}`), app, nil, ""), http.StatusForbidden)
	// A list that names no kind lists what the token reads whole: nothing.
	if got := recordIDs(t, e.do(http.MethodGet, "/api/v1/records", app, nil, "")); got != "" {
		t.Fatalf("a list naming no kind = %q, want nothing", got)
	}
	// The decide verb's kind is out of reach without it.
	wantStatus(t, e.do(http.MethodGet, recordPath(kindRequest, "rA"), app, nil, ""), http.StatusNotFound)
}

func TestAnAgentsScopeWritesARenameAndAnAnswerAndNothingElse(t *testing.T) {
	e, _ := newAgentEnv(t)
	app := e.grant("agents:~/chat/*", []string{"agents:" + fakeRepository + "/chat/*"})
	ds := e.svc.datasets[fakeRepository]
	patch := func(kind, id, body string) *httptest.ResponseRecorder {
		return e.do(http.MethodPatch, recordPath(kind, id), app, []byte(body), "application/json")
	}

	wantStatus(t, patch(kindThread, "tA", `{"properties":{"subject":"Renamed"}}`), http.StatusOK)
	if ds.lastActor != appActor {
		t.Fatalf("the rename was written as %q, want the app's actor", ds.lastActor)
	}
	wantStatus(t, patch(kindInteraction, "iA", `{"properties":{"state":"answered","answers":[{"question":"q","selected":["a"]}]},"ifVersion":1}`), http.StatusOK)
	if ds.lastPatch.IfVersion == nil || *ds.lastPatch.IfVersion != 1 {
		t.Fatalf("the answer reached the engine without its ifVersion: %+v", ds.lastPatch)
	}

	for _, c := range []struct{ kind, id, body string }{
		{kindThread, "tA", `{"properties":{"status":"stopped"}}`},
		{kindThread, "tA", `{"properties":{"subject":"x"},"labels":{"app:notes.example.org/x":"y"}}`},
		{kindInteraction, "iA", `{"properties":{"state":"dismissed","thread":"tB"}}`},
		{kindMessage, "mA", `{"properties":{"content":"x"}}`},
		{kindAgent, talkedAgent, `{"properties":{"hiddenFromChat":true}}`},
	} {
		rec := patch(c.kind, c.id, c.body)
		wantStatus(t, rec, http.StatusForbidden)
		if wa := rec.Header().Get("WWW-Authenticate"); !strings.Contains(wa, "insufficient_scope") {
			t.Fatalf("PATCH %s %s answered WWW-Authenticate %q", c.kind, c.body, wa)
		}
	}
	wantStatus(t, patch(kindThread, "tB", `{"properties":{"subject":"x"}}`), http.StatusNotFound)
	wantStatus(t, e.do(http.MethodPut, recordPath(kindThread, "tC"), app, []byte(`{"properties":{"subject":"x"}}`), "application/json"), http.StatusForbidden)
	wantStatus(t, e.do(http.MethodDelete, recordPath(kindThread, "tA"), app, nil, ""), http.StatusForbidden)
	for _, path := range []string{
		"/api/v1/" + corePackage + "/agent/" + url.PathEscape(talkedAgent) + "/call",
		"/api/v1/" + corePackage + "/function/x/call",
	} {
		wantStatus(t, e.do(http.MethodPost, path, app, []byte(`{"input":"x"}`), "application/json"), http.StatusForbidden)
	}
}

// A read scope over the thread kind reads every thread, and the rename stays
// the agent verb's: its own agents' threads, and a refusal, not a miss, for
// the rest.
func TestARenameUnderAReadScopeOverThreadsIsStillTheAgentVerbs(t *testing.T) {
	e, _ := newAgentEnv(t)
	app := e.grant("agents:~/chat/* read:"+kindThread, []string{"agents:" + fakeRepository + "/chat/*", "read:" + kindThread})
	wantStatus(t, e.do(http.MethodGet, recordPath(kindThread, "tB"), app, nil, ""), http.StatusOK)
	wantStatus(t, e.do(http.MethodPatch, recordPath(kindThread, "tA"), app, []byte(`{"properties":{"subject":"x"}}`), "application/json"), http.StatusOK)
	wantStatus(t, e.do(http.MethodPatch, recordPath(kindThread, "tB"), app, []byte(`{"properties":{"subject":"x"}}`), "application/json"), http.StatusForbidden)
}

func TestAnAppChatsWithTheAgentsItsScopesNameAsItself(t *testing.T) {
	e, rec := newAgentEnv(t)
	app := e.grant("agents:~/chat/*", []string{"agents:" + fakeRepository + "/chat/*"})
	chat := func(agent, body string) *httptest.ResponseRecorder {
		return e.do(http.MethodPost, "/api/v1/"+corePackage+"/agent/"+url.PathEscape(agent)+"/chat", app, []byte(body), "application/json")
	}

	got := chat(talkedAgent, `{"message":"hi"}`)
	wantStatus(t, got, http.StatusOK)
	if !strings.Contains(got.Body.String(), `"done"`) {
		t.Fatalf("the chat streamed %s, want its events", got.Body.String())
	}
	if rec.actor != appActor || rec.name != talkedAgent || rec.scoped {
		t.Fatalf("the turn ran as %q for %q, scoped %v: want the app's actor and the agent unmarked", rec.actor, rec.name, rec.scoped)
	}
	wantStatus(t, chat(talkedAgent, `{"message":"again","thread":"tA"}`), http.StatusOK)
	if rec.thread != "tA" {
		t.Fatalf("the continued chat named thread %q", rec.thread)
	}
	calls := rec.calls
	wantStatus(t, chat(otherAgent, `{"message":"hi"}`), http.StatusNotFound)
	wantStatus(t, chat(talkedAgent, `{"message":"hi","thread":"tB"}`), http.StatusNotFound)
	wantStatus(t, chat("assistant", `{"message":"hi"}`), http.StatusUnprocessableEntity)
	if rec.calls != calls {
		t.Fatal("a refused chat reached the engine")
	}

	// Attaching and stopping reach the token's threads alone.
	events := e.do(http.MethodGet, "/api/v1/"+llmPackage+"/thread/tA/events", app, nil, "")
	wantStatus(t, events, http.StatusOK)
	if !strings.Contains(events.Body.String(), `"idle"`) {
		t.Fatalf("attaching to an idle thread answered %s", events.Body.String())
	}
	wantStatus(t, e.do(http.MethodGet, "/api/v1/"+llmPackage+"/thread/tB/events", app, nil, ""), http.StatusNotFound)
	if stop := e.do(http.MethodPost, "/api/v1/"+llmPackage+"/thread/tA/stop", app, nil, ""); stop.Code == http.StatusNotFound || stop.Code == http.StatusForbidden {
		t.Fatalf("stopping the token's own thread answered %d %s", stop.Code, stop.Body.String())
	}
	wantStatus(t, e.do(http.MethodPost, "/api/v1/"+llmPackage+"/thread/tB/stop", app, nil, ""), http.StatusNotFound)

	// A token without an agents scope reaches none of it.
	reader := e.grant("read:"+personKind, []string{"read:" + personKind})
	wantStatus(t, e.do(http.MethodPost, "/api/v1/"+corePackage+"/agent/"+url.PathEscape(talkedAgent)+"/chat", reader, []byte(`{"message":"hi"}`), "application/json"), http.StatusForbidden)
	wantStatus(t, e.do(http.MethodGet, "/api/v1/"+llmPackage+"/thread/tA/events", reader, nil, ""), http.StatusForbidden)
}

func TestADecideScopeDecidesWhatItsAgentsPropose(t *testing.T) {
	e, rec := newAgentEnv(t)
	app := e.grant("decide:~/chat/assistant", []string{"decide:" + talkedAgent})
	ds := e.svc.datasets[fakeRepository]

	wantStatus(t, e.do(http.MethodGet, recordPath(kindRequest, "rA"), app, nil, ""), http.StatusOK)
	wantStatus(t, e.do(http.MethodGet, recordPath(kindRequest, "rB"), app, nil, ""), http.StatusNotFound)
	if got := recordIDs(t, e.do(http.MethodGet, listOf(kindRequest, `{"decision":{"eq":"proposed"},"thread":{"eq":"tA"}}`), app, nil, "")); got != "rA" {
		t.Fatalf("proposed requests = %q, want the matching agent's alone", got)
	}
	wantStatus(t, e.do(http.MethodGet, listOf(kindRequest, `{"decision":{"eq":"proposed"}}`), app, nil, ""), http.StatusForbidden)

	body := `{"properties":{"decision":"accepted","adjustedDiff":{"properties":{"name":"x"}}},"annotations":{"` + appActor + `/note":"ok"},"ifVersion":1}`
	wantStatus(t, e.do(http.MethodPatch, recordPath(kindRequest, "rA"), app, []byte(body), "application/json"), http.StatusOK)
	if rec.decision != vocabulary.RecordPath(kindRequest, "rA") || ds.lastActor != appActor {
		t.Fatalf("the decision reached the engine marked %q as %q, want the request's path as the app", rec.decision, ds.lastActor)
	}
	wantStatus(t, e.do(http.MethodPatch, recordPath(kindRequest, "rA"), app, []byte(`{"properties":{"diff":{}}}`), "application/json"), http.StatusForbidden)
	wantStatus(t, e.do(http.MethodPatch, recordPath(kindRequest, "rB"), app, []byte(`{"properties":{"decision":"rejected"},"ifVersion":1}`), "application/json"), http.StatusNotFound)

	// Deciding is not talking: the threads and the chat stay out of reach.
	wantStatus(t, e.do(http.MethodGet, recordPath(kindThread, "tA"), app, nil, ""), http.StatusNotFound)
	wantStatus(t, e.do(http.MethodPost, "/api/v1/"+corePackage+"/agent/"+url.PathEscape(talkedAgent)+"/chat", app, []byte(`{"message":"hi"}`), "application/json"), http.StatusForbidden)
}

func TestTheApprovalPageNamesTheAgentsAnAgentScopeMatches(t *testing.T) {
	e, _ := newAgentEnv(t)
	id := e.park(appRequest("agents:~/chat/* decide:* read:" + personKind))
	view := e.show(id)
	wantStatus(t, view, http.StatusOK)
	got := decodeJSON[substrate.AuthorizationRequest](t, view)
	if len(got.Scopes) != 3 {
		t.Fatalf("scopes = %+v", got.Scopes)
	}
	agents, decide, read := got.Scopes[0], got.Scopes[1], got.Scopes[2]
	if agents.Verb != "agents" || agents.Scope != "agents:"+fakeRepository+"/chat/*" || agents.Requested != "agents:~/chat/*" ||
		!agents.Glob || agents.Write || len(agents.Kinds) != 0 || len(agents.Agents) != 1 || agents.Agents[0].ID != talkedAgent {
		t.Fatalf("the agents scope = %+v", agents)
	}
	if decide.Verb != "decide" || len(decide.Agents) != 2 {
		t.Fatalf("the decide scope = %+v", decide)
	}
	if read.Verb != "read" || len(read.Agents) != 0 || len(read.Kinds) != 1 {
		t.Fatalf("the read scope = %+v", read)
	}
	rec := e.approveShown(id, []string{agents.Scope, decide.Scope}, 30)
	wantStatus(t, rec, http.StatusOK)
	tok := decodeJSON[tokenResponse](t, e.exchange(tokenForm(codeFrom(t, decodeJSON[substrate.AuthorizationAnswer](t, rec).Redirect))))
	if tok.Scope != agents.Scope+" decide:*" {
		t.Fatalf("the token's scope = %q", tok.Scope)
	}
}

func TestAnAppTailsTheChangeFeedHeldToWhatItReads(t *testing.T) {
	e, _ := newAgentEnv(t)
	app := e.grant("read:"+personKind+" agents:~/chat/*", []string{"read:" + personKind, "agents:" + fakeRepository + "/chat/*"})
	ds := e.svc.datasets[fakeRepository]

	for _, q := range []string{"", "?from=0", "?before=10", "?runs=1", "?watch=1&q=ada", "?watch=1&values=1"} {
		rec := e.do(http.MethodGet, "/api/v1/changes"+q, app, nil, "")
		wantStatus(t, rec, http.StatusForbidden)
		if wa := rec.Header().Get("WWW-Authenticate"); !strings.Contains(wa, "insufficient_scope") {
			t.Fatalf("changes%s answered WWW-Authenticate %q", q, wa)
		}
	}
	wantStatus(t, e.do(http.MethodGet, "/api/v1/changes?watch=1&kinds="+taskKind, app, nil, ""), http.StatusNotFound)

	srv := httptest.NewServer(e.h)
	defer srv.Close()
	br, stop := startWatch(t, srv, "/api/v1/changes?watch=1", app)
	defer stop()
	bookmark := readLine(t, br)
	if _, ok := bookmark["bookmark"]; !ok || bookmark["generation"] == "" {
		t.Fatalf("the first line = %v, want the bookmark and its generation", bookmark)
	}

	put := func(kind, id string, affected ...substrate.AffectedRecord) {
		ds.commit(substrate.Change{
			TS: time.Unix(1, 0).UTC(), Actor: substrate.ActorAPI, Op: substrate.OpPut, Kind: kind, RecordID: id,
			Payload: map[string]any{"properties": map[string]any{"name": "secret"}}, Hash: "abc", Affected: affected,
		})
	}
	// A batch of rows the token may not see ahead of one it may: the tail
	// moves past them rather than reading them again.
	for range changeBatch + 10 {
		put(taskKind, "t1")
	}
	put(personKind, "p1",
		substrate.AffectedRecord{Kind: personKind, ID: "p1", Version: 2},
		substrate.AffectedRecord{Kind: taskKind, ID: "t1", Version: 3})
	put(kindMessage, "mB")
	put(kindMessage, "mA")
	put(kindAgent, otherAgent)
	put(kindThread, "tA")

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
	e, _ := newAgentEnv(t)
	app := e.grant("agents:~/chat/*", []string{"agents:" + fakeRepository + "/chat/*"})
	ds := e.svc.datasets[fakeRepository]
	srv := httptest.NewServer(e.h)
	defer srv.Close()
	br, stop := startWatch(t, srv, "/api/v1/changes?watch=1", app)
	defer stop()
	if line := readLine(t, br); line["bookmark"] == nil {
		t.Fatalf("the first line = %v, want the bookmark", line)
	}

	form := url.Values{"token": {app}}
	wantStatus(t, e.do(http.MethodPost, "/oauth/revoke", "", []byte(form.Encode()), "application/x-www-form-urlencoded"), http.StatusOK)
	ds.commit(substrate.Change{TS: time.Unix(1, 0).UTC(), Actor: substrate.ActorAPI, Op: substrate.OpPut, Kind: kindThread, RecordID: "tA"})

	line := readLine(t, br)
	if line["recordId"] != nil {
		t.Fatalf("a revoked token's watch carried a row: %v", line)
	}
	problem, _ := line["error"].(map[string]any)
	if problem["code"] != codeAuth {
		t.Fatalf("the line after the revoke = %v, want the terminal auth frame", line)
	}
}

func TestATokenLeaseEndsAtItsExpiryAndOnARefusedRead(t *testing.T) {
	now := time.Unix(1000, 0)
	clock := func() time.Time { return now }
	expires := now.Add(time.Minute)
	reads := 0
	refuse := false
	lease := &tokenLease{expires: &expires, now: clock, checked: now, recheck: func(context.Context) error {
		reads++
		if refuse {
			return substrate.ErrAuth
		}
		return nil
	}}
	ctx := context.Background()

	if err := lease.alive(ctx, 2*time.Second); err != nil || reads != 0 {
		t.Fatalf("a fresh lease = %v after %d reads, want standing without a read", err, reads)
	}
	now = now.Add(3 * time.Second)
	if err := lease.alive(ctx, 2*time.Second); err != nil || reads != 1 {
		t.Fatalf("a stale lease = %v after %d reads, want one read", err, reads)
	}
	if err := lease.alive(ctx, 0); err != nil || reads != 2 {
		t.Fatalf("every=0 = %v after %d reads, want a read every call", err, reads)
	}
	refuse = true
	if err := lease.alive(ctx, 0); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("a refused read = %v, want ErrAuth", err)
	}
	refuse = false
	now = expires
	if err := lease.alive(ctx, time.Hour); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("at its expiry the lease = %v, want ErrAuth without waiting for a read", err)
	}
	var none *tokenLease
	if err := none.alive(ctx, 0); err != nil {
		t.Fatalf("a full-access token's absent lease = %v", err)
	}
}
