package engine

// The agent loop's regressions, driven by an in-process fake
// OpenAI-compatible server with scripted tool-calling turns — deterministic,
// no live LLM (a real gateway is exercised in the live run,
// never in suites): trigger→agent dispatch end-to-end (function-tool patch
// held to the agent's emit, sub-agent child thread + parent edge, propose
// landing a recordpatchrequest, cost rolled up to the root thread), budget
// exhaustion terminating cleanly, the agentDepth cap refusing at 4 while
// causal depth stays untouched, and the chat round-trip (streaming events +
// thread continuation).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// --- the fake OpenAI-compatible server -------------------------------------------

type fakeCall struct {
	name string
	args string
}

type fakeTurn struct {
	content string
	calls   []fakeCall
	// status, when non-zero, fails this turn with the HTTP code — the LLM
	// transport error that makes a delivery retry.
	status int
	// arrived (closed on receipt) and release (blocks the response) build
	// concurrency barriers.
	arrived chan struct{}
	release <-chan struct{}
}

// fakeLLM scripts /chat/completions per MODEL: each request pops the next
// turn for its model, so one server drives a whole agent chain as long as
// every loop under test names its own model id.
type fakeLLM struct {
	mu       sync.Mutex
	scripts  map[string][]fakeTurn
	requests map[string][]map[string]any
	auths    []string // every Authorization header the server saw
	srv      *httptest.Server
}

func newFakeLLM(t *testing.T) *fakeLLM {
	t.Helper()
	f := &fakeLLM{scripts: map[string][]fakeTurn{}, requests: map[string][]map[string]any{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeLLM) authsSeen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.auths...)
}

func (f *fakeLLM) script(model string, turns ...fakeTurn) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scripts[model] = append(f.scripts[model], turns...)
}

func (f *fakeLLM) requestsOf(model string) []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.requests[model]...)
}

func (f *fakeLLM) handle(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/chat/completions" {
		http.NotFound(w, r)
		return
	}
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	model, _ := req["model"].(string)
	f.mu.Lock()
	f.auths = append(f.auths, r.Header.Get("Authorization"))
	f.requests[model] = append(f.requests[model], req)
	queue := f.scripts[model]
	if len(queue) == 0 {
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(w, `{"error":{"message":"script exhausted for model %s"}}`, model)
		return
	}
	turn := queue[0]
	f.scripts[model] = queue[1:]
	f.mu.Unlock()

	if turn.arrived != nil {
		close(turn.arrived)
	}
	if turn.release != nil {
		<-turn.release
	}
	if turn.status != 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(turn.status)
		_, _ = fmt.Fprintf(w, `{"error":{"message":"scripted failure %d"}}`, turn.status)
		return
	}

	usage := map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
	toolCalls := make([]map[string]any, 0, len(turn.calls))
	for i, c := range turn.calls {
		toolCalls = append(toolCalls, map[string]any{
			"index": i, "id": fmt.Sprintf("call_%d", i), "type": "function",
			"function": map[string]any{"name": c.name, "arguments": c.args},
		})
	}
	if stream, _ := req["stream"].(bool); !stream {
		msg := map[string]any{"role": "assistant", "content": turn.content}
		if len(toolCalls) > 0 {
			msg["tool_calls"] = toolCalls
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "cmpl", "object": "chat.completion",
			"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": "stop"}},
			"usage":   usage,
		})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	chunk := func(payload map[string]any) {
		buf, _ := json.Marshal(payload)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", buf)
	}
	// Content streams as two deltas so the collector sees real chunking.
	if turn.content != "" {
		half := len(turn.content) / 2
		for _, part := range []string{turn.content[:half], turn.content[half:]} {
			if part == "" {
				continue
			}
			chunk(map[string]any{"object": "chat.completion.chunk", "choices": []any{
				map[string]any{"index": 0, "delta": map[string]any{"content": part}},
			}})
		}
	}
	for _, tc := range toolCalls {
		chunk(map[string]any{"object": "chat.completion.chunk", "choices": []any{
			map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{tc}}},
		}})
	}
	chunk(map[string]any{"object": "chat.completion.chunk", "choices": []any{}, "usage": usage})
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}

// --- fixtures ---------------------------------------------------------------------

const crewAuthority = "crew.test.dev"

// openAgentDataset provisions a repository, points a set of llmprovider rows at the fake
// server (one model id per loop under test), and installs one authority carrying
// a widget type, the annotate function tool, and every agent the tests
// exercise.
func openAgentDataset(t *testing.T) (*dataset, *fakeLLM) {
	t.Helper()
	ctx := context.Background()
	ds := openInternalDataset(t)
	fake := newFakeLLM(t)
	for _, id := range []string{"rootllm", "subllm", "roguellm", "chainllm", "budgetllm", "chatllm", "wardenllm", "minionllm", "keepllm", "gqlllm", "mutllm", "judgellm", "justicellm", "arbiterllm", "libllm", "purellm", "stoicllm", "selfllm"} {
		model := strings.TrimSuffix(id, "llm")
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: typeProvider, ID: id,
			Properties: map[string]any{
				// A row-defined baseURL REQUIRES a row-defined apiKey (the
				// host gateway key never travels to a custom endpoint).
				"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-" + id,
				// pricing is a repeated object: one row per model, keyed by
				// the `model` field.
				"pricing": []any{map[string]any{"model": model, "inputPer1M": 1.0, "outputPer1M": 5.0}},
			},
		}); err != nil {
			t.Fatalf("put llmprovider row %s: %v", id, err)
		}
	}
	agent := func(name string, data map[string]any) map[string]any {
		data["description"] = name + " under test"
		data["prompt"] = "You are " + name + "."
		return vocabulary.AgentManifest(crewAuthority, name, data)
	}
	docs := []map[string]any{
		vocabulary.AuthorityManifest(crewAuthority, 0),
		vocabulary.KindManifest(crewAuthority, map[string]any{"singular": "widget", "plural": "widgets"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		vocabulary.FunctionManifest(crewAuthority, "annotate", map[string]any{
			"description": "writes one annotated task under the id you pass",
			"runtime":     vocabulary.RuntimePython,
			"arguments":   []any{map[string]any{"name": "id", "type": "string", "required": true}},
			"permissions": map[string]any{"writes": []any{"tasks.substrate.reamde.dev/task"}},
			"source": `
def main(input, host):
    tid = input["args"]["id"]
    return {"effects": [{"action": "put", "kind": "tasks.substrate.reamde.dev/task",
                         "id": tid, "properties": {"title": "annotated"}}],
            "output": {"ok": True}}
`,
		}),
		// A PURE FUNCTION: it declares no emit at all, returns an output and
		// stages nothing. Legal since `emit:` stopped being required.
		vocabulary.FunctionManifest(crewAuthority, "measure", map[string]any{
			"description": "measures a widget name and returns its length, writing nothing",
			"runtime":     vocabulary.RuntimePython,
			"arguments":   []any{map[string]any{"name": "name", "type": "string", "required": true}},
			"returns":     []any{map[string]any{"name": "length", "type": "int"}},
			"source": `
def main(input, host):
    return {"output": {"length": len(input["args"]["name"])}}
`,
		}),
		vocabulary.FunctionManifest(crewAuthority, "keyecho", map[string]any{
			"description": "writes one task carrying the invocation's idempotency key",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"writes": []any{"tasks.substrate.reamde.dev/task"}},
			"source": `
def main(input, host):
    key = input["idempotencyKey"]
    return {"effects": [{"action": "put", "kind": "tasks.substrate.reamde.dev/task",
                         "id": "t-idem", "properties": {"title": key}}],
            "output": {"key": key}}
`,
		}),
		agent("classifier", map[string]any{
			"provider": "rootllm", "model": "root",
			"tools": []any{
				map[string]any{"function": crewAuthority + "/annotate"},
				map[string]any{"function": vocabulary.HostFunctionPropose},
			},
			"agents": []any{crewAuthority + "/scribe"},
			"permissions": map[string]any{
				"writes": []any{"tasks.substrate.reamde.dev/task", vocabulary.KindRecordPatchRequest},
			},
		}),
		agent("scribe", map[string]any{"provider": "subllm", "model": "sub"}),
		// stoic proposes but never resumes: the resolution row lands, the
		// continuation is withheld — the declaration's own cost knob.
		agent("stoic", map[string]any{
			"provider": "stoicllm", "model": "stoic",
			"resume": "never",
			"tools":  []any{map[string]any{"function": vocabulary.HostFunctionPropose}},
			"permissions": map[string]any{
				"writes": []any{vocabulary.KindRecordPatchRequest},
			},
		}),
		// selfjudge can both propose and decide: the self-exclusion pair — a
		// thread's own agent resolving something never wakes that thread.
		agent("selfjudge", map[string]any{
			"provider": "selfllm", "model": "self",
			"tools": []any{
				map[string]any{"function": vocabulary.HostFunctionPropose},
				map[string]any{"function": vocabulary.HostFunctionMutate},
			},
			"permissions": map[string]any{
				"writes": []any{vocabulary.KindRecordPatchRequest, crewAuthority + "/widget"},
			},
		}),
		agent("rogue", map[string]any{
			"provider": "roguellm", "model": "rogue",
			"tools": []any{map[string]any{"function": crewAuthority + "/annotate"}},
			// annotate emits tasks, but THIS agent's emit does not allow them.
		}),
		agent("budgeter", map[string]any{
			"provider": "budgetllm", "model": "budget",
			"tools":       []any{map[string]any{"function": crewAuthority + "/annotate"}},
			"permissions": map[string]any{"writes": []any{"tasks.substrate.reamde.dev/task"}},
			"budgets":     map[string]any{"maxTurns": 2},
		}),
		agent("chatter", map[string]any{"provider": "chatllm", "model": "chat"}),
		// warden writes NOTHING (empty emit) but delegates to minion, whose
		// own emit could write tasks — the ceiling test pair.
		agent("warden", map[string]any{
			"provider": "wardenllm", "model": "warden", "agents": []any{crewAuthority + "/minion"},
		}),
		agent("minion", map[string]any{
			"provider": "minionllm", "model": "minion",
			"tools": []any{
				map[string]any{"function": crewAuthority + "/annotate"},
				map[string]any{"function": vocabulary.HostFunctionPropose},
			},
			"permissions": map[string]any{
				"writes": []any{"tasks.substrate.reamde.dev/task", vocabulary.KindRecordPatchRequest},
			},
		}),
		// keeper's keyecho tool records its idempotency key — the stable-key
		// retry test.
		agent("keeper", map[string]any{
			"provider": "keepllm", "model": "keep",
			"tools":       []any{map[string]any{"function": crewAuthority + "/keyecho"}},
			"permissions": map[string]any{"writes": []any{"tasks.substrate.reamde.dev/task"}},
		}),
		// archivist reads the whole graph through the graphql built-in and
		// writes nothing; editor holds both graphql tools, its mutate gated to
		// widgets alone.
		agent("archivist", map[string]any{
			"provider": "gqlllm", "model": "gql",
			"tools": []any{map[string]any{"function": vocabulary.HostFunctionGraphQL}},
		}),
		agent("editor", map[string]any{
			"provider": "mutllm", "model": "mut",
			"tools": []any{
				map[string]any{"function": vocabulary.HostFunctionGraphQL},
				map[string]any{"function": vocabulary.HostFunctionMutate},
			},
			"permissions": map[string]any{"writes": []any{crewAuthority + "/widget"}},
		}),
		// arbiter DECIDES change requests through the mutate tool: its emit
		// names the request kind (so it may write the decision) and widgets (so
		// the accept's transitive write is within its ceiling), and NOT tasks —
		// the confused-deputy half of the pair.
		agent("arbiter", map[string]any{
			"provider": "arbiterllm", "model": "arbiter",
			"tools": []any{map[string]any{"function": vocabulary.HostFunctionMutate}},
			"permissions": map[string]any{
				"writes": []any{vocabulary.KindRecordPatchRequest, crewAuthority + "/widget"},
			},
		}),
		// librarian holds the capability-scoped read: the query built-in named by
		// identity, with the `reads:` that grants it.
		agent("librarian", map[string]any{
			"provider": "libllm", "model": "lib",
			"tools": []any{map[string]any{"function": vocabulary.HostFunctionQuery}},
			"permissions": map[string]any{
				"reads": map[string]any{"kinds": []any{crewAuthority + "/widget"}},
			},
		}),
		// purist carries the PURE function as a tool: no emit anywhere, an output
		// the model reads.
		agent("purist", map[string]any{
			"provider": "purellm", "model": "pure",
			"tools": []any{map[string]any{"function": crewAuthority + "/measure"}},
		}),
		// judge is subagentOnly: off the chat surface, still a callable and
		// still justice's sub-agent.
		agent("judge", map[string]any{
			"provider": "judgellm", "model": "judge", "subagentOnly": true,
		}),
		agent("justice", map[string]any{
			"provider": "justicellm", "model": "justice",
			"agents": []any{crewAuthority + "/judge"},
		}),
		agent("e", map[string]any{"provider": "chainllm", "model": "chain"}),
		agent("d", map[string]any{"provider": "chainllm", "model": "chain", "agents": []any{crewAuthority + "/e"}}),
		agent("c", map[string]any{"provider": "chainllm", "model": "chain", "agents": []any{crewAuthority + "/d"}}),
		agent("b", map[string]any{"provider": "chainllm", "model": "chain", "agents": []any{crewAuthority + "/c"}}),
		agent("a", map[string]any{"provider": "chainllm", "model": "chain", "agents": []any{crewAuthority + "/b"}}),
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, docs); err != nil {
		t.Fatalf("install crew authority: %v", err)
	}
	return ds, fake
}

func agentThreadsOf(t *testing.T, ds *dataset, agent string) []map[string]any {
	t.Helper()
	rows, err := ds.db.QueryContext(context.Background(), `
		SELECT id, props FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND props->>'agent' = $2
		ORDER BY created_at, id`, typeThread, vocabulary.RecordPath(kindAgent, crewAuthority+"/"+agent))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []map[string]any
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			t.Fatal(err)
		}
		props := map[string]any{}
		_ = json.Unmarshal(raw, &props)
		props["__id"] = id
		out = append(out, props)
	}
	return out
}

func threadMessages(t *testing.T, ds *dataset, threadID string) []map[string]any {
	t.Helper()
	rows, err := ds.db.QueryContext(context.Background(), `
		SELECT e.props FROM records e
		WHERE e.kind = $2 AND e.deleted_at IS NULL AND e.props->>'thread' = $1
		ORDER BY e.created_at, e.id`, vocabulary.RecordPath(typeThread, threadID), typeMessage)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []map[string]any
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		props := map[string]any{}
		_ = json.Unmarshal(raw, &props)
		out = append(out, props)
	}
	return out
}

// threadCountOf counts live threads by FULL agent identity (agentThreadsOf
// assumes the crew authority).
func threadCountOf(t *testing.T, ds *dataset, identity string) int {
	t.Helper()
	var n int
	if err := ds.db.QueryRowContext(context.Background(), `
		SELECT count(*) FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND props->>'agent' = $2`,
		typeThread, vocabulary.RecordPath(kindAgent, identity)).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func intProp(m map[string]any, key string) int {
	v, _ := anyFloat(m[key])
	return int(v)
}

// --- the tests ---------------------------------------------------------------------

func TestAgentTriggerDispatch(t *testing.T) {
	t.Parallel()
	// The end-to-end chain: a widget creation fires an agent-callable
	// trigger; the loop patches through a function tool (held to the agent's
	// emit), runs a sub-agent (child thread, parent edge, own budgets),
	// proposes a recordpatchrequest, and settles with a final reply — the
	// run row lands ok, the cursor advances, and the cost tally rolls up
	// onto the root thread.
	ctx := context.Background()
	ds, fake := openAgentDataset(t)

	// Admission: a trigger naming an unknown agent never lands.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source":   map[string]any{"record": map[string]any{"kinds": []any{crewAuthority + "/widget"}}},
			"callable": vocabulary.RecordPath("core.substrate.reamde.dev/agent", crewAuthority+"/ghost"),
		},
	}); err == nil {
		t.Fatal("a trigger naming an unknown agent landed")
	}
	tr, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source":   map[string]any{"record": map[string]any{"kinds": []any{crewAuthority + "/widget"}, "ops": []any{"create"}}},
			"callable": vocabulary.RecordPath("core.substrate.reamde.dev/agent", crewAuthority+"/classifier"),
		},
	})
	if err != nil {
		t.Fatalf("put agent trigger: %v", err)
	}

	fake.script("root",
		fakeTurn{calls: []fakeCall{{"annotate", `{"id":"t-classified"}`}}},
		fakeTurn{calls: []fakeCall{{"scribe", `{"input":"summarize the widget"}`}}},
		fakeTurn{calls: []fakeCall{{"propose", `{"kind":"crew.test.dev/widget","target":"w-fixed","diff":{"name":"better"},"rationale":"tidy"}`}}},
		fakeTurn{content: "classified."},
	)
	fake.script("sub", fakeTurn{content: "the summary"})

	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewAuthority + "/widget", ID: "w-fixed",
		Properties: map[string]any{"name": "raw"},
	}); err != nil {
		t.Fatalf("put widget: %v", err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}

	// The function tool's effect applied, under the AGENT's actor.
	task, err := ds.Get(ctx, "tasks.substrate.reamde.dev/task", "t-classified")
	if err != nil {
		t.Fatalf("the annotate tool's task: %v", err)
	}
	if task.Properties["title"] != "annotated" {
		t.Fatalf("task title %v", task.Properties["title"])
	}
	var taskActor string
	if err := ds.db.QueryRowContext(ctx, `
		SELECT actor FROM changelog WHERE record_id = 't-classified' ORDER BY seq LIMIT 1`).Scan(&taskActor); err != nil {
		t.Fatal(err)
	}
	if taskActor != "function:classifier" {
		t.Fatalf("the tool effect's actor is %q, not the agent's", taskActor)
	}

	// The propose built-in landed a recordpatchrequest targeting the widget.
	var reqID string
	if err := ds.db.QueryRowContext(ctx, `
		SELECT id FROM records WHERE kind = $1 AND deleted_at IS NULL
		  AND props->>'rationale' = 'tidy'`, vocabulary.KindRecordPatchRequest).Scan(&reqID); err != nil {
		t.Fatalf("the proposed recordpatchrequest: %v", err)
	}
	var target string
	if err := ds.db.QueryRowContext(ctx, `
		SELECT dst FROM edges WHERE rel = 'target' AND src = $1`, reqID).Scan(&target); err != nil {
		t.Fatal(err)
	}
	if target != "w-fixed" {
		t.Fatalf("proposal targets %q", target)
	}

	// Threads: one root (mode trigger), one child (mode subagent) carrying
	// the parent edge; both settled ok.
	roots := agentThreadsOf(t, ds, "classifier")
	if len(roots) != 1 {
		t.Fatalf("classifier threads: %d", len(roots))
	}
	root := roots[0]
	if root["mode"] != "trigger" || root["status"] != threadOK {
		t.Fatalf("root thread: mode %v status %v", root["mode"], root["status"])
	}
	children := agentThreadsOf(t, ds, "scribe")
	if len(children) != 1 {
		t.Fatalf("scribe threads: %d", len(children))
	}
	child := children[0]
	if child["mode"] != agentModeSubagent || child["status"] != threadOK || intProp(child, "agentDepth") != 1 {
		t.Fatalf("child thread: %+v", child)
	}
	var parent string
	if err := ds.db.QueryRowContext(ctx, `
		SELECT props->>'parent' FROM records WHERE kind = $2 AND id = $1`,
		child["__id"], typeThread).Scan(&parent); err != nil {
		t.Fatal(err)
	}
	if parent != vocabulary.RecordPath(typeThread, root["__id"].(string)) {
		t.Fatalf("child parent edge points at %q, root is %q", parent, root["__id"])
	}

	// Cost roll-up: the fake bills 10/5 per turn; the root ran 4 turns, the
	// sub-agent 1 — the ROOT thread carries all 5, the child only its own.
	if got := intProp(root, "promptTokens"); got != 50 {
		t.Fatalf("root promptTokens %d, want 50 (roll-up)", got)
	}
	if got := intProp(child, "promptTokens"); got != 10 {
		t.Fatalf("child promptTokens %d, want 10", got)
	}
	if cost, _ := anyFloat(root["costUSD"]); cost <= 0 {
		t.Fatalf("root costUSD %v", root["costUSD"])
	}

	// The transcript: user envelope + 4 assistant turns + 3 tool results.
	msgs := threadMessages(t, ds, root["__id"].(string))
	if len(msgs) != 8 {
		t.Fatalf("root messages: %d", len(msgs))
	}
	if msgs[0]["role"] != "user" || !strings.Contains(msgs[0]["content"].(string), "w-fixed") {
		t.Fatalf("the envelope message: %+v", msgs[0])
	}

	// The run ledger: ok, with the applied-effects summary.
	var effectsRaw []byte
	if err := ds.db.QueryRowContext(ctx, `
		SELECT props->'effects' FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND props->>'trigger' = $2 AND props->>'status' = 'ok'`,
		typeRun, tr.ID).Scan(&effectsRaw); err != nil {
		t.Fatalf("the ok run row: %v", err)
	}
	var summary map[string]any
	_ = json.Unmarshal(effectsRaw, &summary)
	if intPropAny(summary["put"]) != 1 || intPropAny(summary["propose"]) != 1 {
		t.Fatalf("run effects summary: %v", summary)
	}

	// causalDepth is the TRIGGER chain's counter, not the agent's: the child
	// thread's creation is one caused_by hop under the widget write —
	// agentDepth 1 never touched it.
	var childSeq int64
	if err := ds.db.QueryRowContext(ctx, `
		SELECT seq FROM changelog WHERE record_id = $1 ORDER BY seq LIMIT 1`, child["__id"]).Scan(&childSeq); err != nil {
		t.Fatal(err)
	}
	depth, err := ds.causalDepth(ctx, childSeq)
	if err != nil {
		t.Fatal(err)
	}
	if depth != 1 {
		t.Fatalf("causal depth of the sub-agent's thread write: %d, want 1", depth)
	}

	// Quiet redelivery: the cursor is past the widget; a second pass calls
	// no model (the script is exhausted — a delivery would error loudly).
	before := maxSeqOf(t, ds)
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if got := maxSeqOf(t, ds); got != before {
		t.Fatalf("a second pass redelivered: seq %d → %d", before, got)
	}
}

func intPropAny(v any) int {
	f, _ := anyFloat(v)
	return int(f)
}

func TestAgentEmitHoldsFunctionToolEffects(t *testing.T) {
	t.Parallel()
	// A function tool's effects are held to the AGENT's emit: annotate may
	// emit tasks, but the rogue agent may not — the tool call fails as a
	// result the model sees, and nothing lands.
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("rogue",
		fakeTurn{calls: []fakeCall{{"annotate", `{"id":"t-rogue"}`}}},
		fakeTurn{content: "blocked, giving up"},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/rogue", "go")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.Status != threadOK || res.Effects != 0 {
		t.Fatalf("rogue result: %+v", res)
	}
	if _, err := ds.Get(ctx, "tasks.substrate.reamde.dev/task", "t-rogue"); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the held-back task landed: %v", err)
	}
	msgs := threadMessages(t, ds, res.Thread)
	found := false
	for _, m := range msgs {
		if m["role"] == "tool" && strings.Contains(fmt.Sprint(m["content"]), "emit allowlist") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no tool result names the emit refusal: %+v", msgs)
	}
}

func TestAgentDepthCapRefusesAtFour(t *testing.T) {
	t.Parallel()
	// The chain a → b → c → d runs at agentDepths 0/1/2/3 — the deepest the
	// default cap (3) admits; d's call to e would sit at depth 4 and is
	// refused as a tool result d sees, no fifth loop ever opening. The
	// causal counter never moves: agentDepth is its own counter.
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("chain",
		fakeTurn{calls: []fakeCall{{"b", `{"input":"go"}`}}}, // a turn 1
		fakeTurn{calls: []fakeCall{{"c", `{"input":"go"}`}}}, // b turn 1
		fakeTurn{calls: []fakeCall{{"d", `{"input":"go"}`}}}, // c turn 1
		fakeTurn{calls: []fakeCall{{"e", `{"input":"go"}`}}}, // d turn 1 → REFUSED at depth 4
		fakeTurn{content: "d done"},                          // d turn 2
		fakeTurn{content: "c done"},                          // c turn 2
		fakeTurn{content: "b done"},                          // b turn 2
		fakeTurn{content: "a done"},                          // a turn 2
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/a", "start")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.Reply != "a done" {
		t.Fatalf("reply %q", res.Reply)
	}
	if got := len(agentThreadsOf(t, ds, "e")); got != 0 {
		t.Fatalf("e ran %d threads past the depth cap", got)
	}
	depths := agentThreadsOf(t, ds, "d")
	if len(depths) != 1 || intProp(depths[0], "agentDepth") != 3 {
		t.Fatalf("d threads: %+v", depths)
	}
	refused := false
	for _, m := range threadMessages(t, ds, depths[0]["__id"].(string)) {
		if m["role"] == "tool" && strings.Contains(fmt.Sprint(m["content"]), "agent depth cap") {
			refused = true
		}
	}
	if !refused {
		t.Fatal("d's transcript carries no depth-cap refusal")
	}
	// Direct invocations are causal ROOTS: nothing in the chain moved the
	// causal counter — every thread write's caused_by is NULL.
	var caused int
	if err := ds.db.QueryRowContext(ctx, `
		SELECT count(*) FROM changelog c JOIN records e ON e.id = c.record_id
		WHERE e.kind = $1 AND c.caused_by IS NOT NULL`, typeThread).Scan(&caused); err != nil {
		t.Fatal(err)
	}
	if caused != 0 {
		t.Fatalf("%d thread writes carry caused_by on a direct call", caused)
	}
}

func TestAgentBudgetExhaustionSettlesCleanly(t *testing.T) {
	t.Parallel()
	// budgeter caps maxTurns at 2; the script calls a tool on both turns, so
	// the loop ends over budget — cleanly: no error, the thread says
	// overbudget and why, and both applied effects stand.
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("budget",
		fakeTurn{calls: []fakeCall{{"annotate", `{"id":"t-b1"}`}}},
		fakeTurn{calls: []fakeCall{{"annotate", `{"id":"t-b2"}`}}},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/budgeter", "go")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.Status != threadOverBudget || !strings.Contains(res.Reason, "max turns") {
		t.Fatalf("result: %+v", res)
	}
	if res.Effects != 2 {
		t.Fatalf("effects %d", res.Effects)
	}
	threads := agentThreadsOf(t, ds, "budgeter")
	if len(threads) != 1 || threads[0]["status"] != threadOverBudget {
		t.Fatalf("threads: %+v", threads)
	}
	for _, id := range []string{"t-b1", "t-b2"} {
		if _, err := ds.Get(ctx, "tasks.substrate.reamde.dev/task", id); err != nil {
			t.Fatalf("task %s: %v", id, err)
		}
	}
}

// installGreeterBundle adds a bundled agent (greeter, in its own bundle
// authority) plus a host-authority caller that names it as a sub-agent — the
// lifecycle-gate fixtures.
func installGreeterBundle(t *testing.T, ds *dataset, fake *fakeLLM) {
	t.Helper()
	ctx := context.Background()
	const abAuthority = "abundle.bundles.substrate.reamde.dev"
	for _, id := range []string{"greetllm", "callerllm"} {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: typeProvider, ID: id,
			Properties: map[string]any{"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-" + id},
		}); err != nil {
			t.Fatalf("put llmprovider row %s: %v", id, err)
		}
	}
	greeter := vocabulary.AgentManifest(abAuthority, "greeter", map[string]any{
		"description": "greets", "prompt": "You greet.", "provider": "greetllm", "model": "greet",
	})
	docs := []map[string]any{
		vocabulary.AuthorityManifest(abAuthority, 0),
		vocabulary.BundleManifest(abAuthority, map[string]any{
			"description": "the agent bundle",
			"installs":    []any{abAuthority + "/abconfig", abAuthority + "/greeter"},
		}),
		vocabulary.KindManifest(abAuthority,
			map[string]any{"singular": "abconfig", "plural": "abconfigs"},
			map[string]any{"properties": map[string]any{
				"note": map[string]any{"type": "string"},
			}}),
		greeter,
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, docs); err != nil {
		t.Fatalf("install agent bundle: %v", err)
	}
	const hostAuthority = "hostcrew.test.dev"
	caller := vocabulary.AgentManifest(hostAuthority, "caller", map[string]any{
		"description": "delegates to greeter", "prompt": "You delegate.",
		"provider": "callerllm", "model": "caller", "agents": []any{abAuthority + "/greeter"},
	})
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.AuthorityManifest(hostAuthority, 0), caller,
	}); err != nil {
		t.Fatalf("install caller agent: %v", err)
	}
}

func TestBundledAgentLifecycleGate(t *testing.T) {
	t.Parallel()
	// A disabled or uninstalled bundle's agents refuse EVERY direct entry:
	// the call API, chat, and sub-agent dispatch — not just trigger loading.
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	installGreeterBundle(t, ds, fake)
	const greeter = "abundle.bundles.substrate.reamde.dev/greeter"

	// Live bundle: the agent answers.
	fake.script("greet", fakeTurn{content: "hello"})
	if res, err := ds.CallAgent(ctx, greeter, "hi"); err != nil || res.Reply != "hello" {
		t.Fatalf("live call: %v %+v", err, res)
	}

	if err := ds.DisableBundle(ctx, "abundle.bundles.substrate.reamde.dev"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := ds.CallAgent(ctx, greeter, "hi"); !errors.Is(err, substrate.ErrGuard) {
		t.Fatalf("disabled bundle's agent answered a call: %v", err)
	}
	if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, greeter, "", "hi", nil); !errors.Is(err, substrate.ErrGuard) {
		t.Fatalf("disabled bundle's agent answered a chat: %v", err)
	}
	// Sub-agent dispatch: the caller's hop is refused as a tool result — the
	// child never opens a thread or touches its model.
	fake.script("caller",
		fakeTurn{calls: []fakeCall{{"greeter", `{"input":"say hi"}`}}},
		fakeTurn{content: "gave up"},
	)
	res, err := ds.CallAgent(ctx, "hostcrew.test.dev/caller", "go")
	if err != nil {
		t.Fatalf("caller: %v", err)
	}
	if res.Reply != "gave up" {
		t.Fatalf("caller reply: %q", res.Reply)
	}
	if got := threadCountOf(t, ds, greeter); got != 1 {
		t.Fatalf("greeter threads after the refused hop: %d, want 1 (the live call only)", got)
	}
	refused := false
	for _, m := range threadMessages(t, ds, res.Thread) {
		if m["role"] == "tool" && strings.Contains(fmt.Sprint(m["content"]), "disabled") {
			refused = true
		}
	}
	if !refused {
		t.Fatal("the caller's transcript carries no lifecycle refusal")
	}

	// Uninstalled: the agent is gone entirely — uninstall tears the owned authority
	// down, so the agent no longer resolves and every entry 404s
	// rather than refusing with a lifecycle guard.
	if err := ds.EnableBundle(ctx, "abundle.bundles.substrate.reamde.dev"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if err := ds.UninstallBundle(ctx, "abundle.bundles.substrate.reamde.dev"); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := ds.CallAgent(ctx, greeter, "hi"); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("uninstalled bundle's agent answered a call: %v", err)
	}
	if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, greeter, "", "hi", nil); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("uninstalled bundle's agent answered a chat: %v", err)
	}
}

func TestSubAgentEmitCeiling(t *testing.T) {
	t.Parallel()
	// A parent with EMPTY emit delegates to a write-capable child: the
	// child's effective emit is the intersection — empty — so neither its
	// function tool's effects nor its propose may land. The same child
	// invoked DIRECTLY (no ceiling) still writes.
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewAuthority + "/widget", ID: "w-ceiling", Properties: map[string]any{"name": "raw"},
	}); err != nil {
		t.Fatalf("put widget: %v", err)
	}

	fake.script("warden",
		fakeTurn{calls: []fakeCall{{"minion", `{"input":"write the task"}`}}},
		fakeTurn{content: "warden done"},
	)
	fake.script("minion",
		fakeTurn{calls: []fakeCall{{"annotate", `{"id":"t-ceiling"}`}}},
		fakeTurn{calls: []fakeCall{{"propose", `{"kind":"crew.test.dev/widget","target":"w-ceiling","diff":{"name":"better"}}`}}},
		fakeTurn{content: "minion blocked"},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/warden", "go")
	if err != nil {
		t.Fatalf("warden: %v", err)
	}
	if res.Reply != "warden done" || res.Effects != 0 {
		t.Fatalf("warden result: %+v", res)
	}
	if _, err := ds.Get(ctx, "tasks.substrate.reamde.dev/task", "t-ceiling"); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the ceilinged function-tool effect landed: %v", err)
	}
	var proposals int
	if err := ds.db.QueryRowContext(ctx, `
		SELECT count(*) FROM records WHERE kind = $1 AND deleted_at IS NULL`,
		vocabulary.KindRecordPatchRequest).Scan(&proposals); err != nil {
		t.Fatal(err)
	}
	if proposals != 0 {
		t.Fatalf("the ceilinged propose landed %d requests", proposals)
	}
	minions := agentThreadsOf(t, ds, "minion")
	if len(minions) != 1 {
		t.Fatalf("minion threads: %d", len(minions))
	}
	refusals := 0
	for _, m := range threadMessages(t, ds, minions[0]["__id"].(string)) {
		if m["role"] == "tool" && strings.Contains(fmt.Sprint(m["content"]), "effective emit") {
			refusals++
		}
	}
	if refusals != 2 {
		t.Fatalf("the minion transcript carries %d ceiling refusals, want 2", refusals)
	}

	// Control: the SAME agent called directly (no ceiling) writes fine.
	fake.script("minion",
		fakeTurn{calls: []fakeCall{{"annotate", `{"id":"t-direct"}`}}},
		fakeTurn{content: "minion wrote"},
	)
	direct, err := ds.CallAgent(ctx, crewAuthority+"/minion", "go")
	if err != nil {
		t.Fatalf("direct minion: %v", err)
	}
	if direct.Effects != 1 {
		t.Fatalf("direct minion effects: %d", direct.Effects)
	}
	if _, err := ds.Get(ctx, "tasks.substrate.reamde.dev/task", "t-direct"); err != nil {
		t.Fatalf("the direct write is missing: %v", err)
	}
}

func TestAgentRetryKeepsIdempotencyKeys(t *testing.T) {
	t.Parallel()
	// A tool effect commits, then the delivery fails at the NEXT model turn
	// and retries on a fresh thread: the retried tool call must present the
	// SAME idempotency key — keys derive from the delivery identity
	// (repository/trigger/seq), the call path and the ordinal, never from the
	// thread id.
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	tr, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source":   map[string]any{"record": map[string]any{"kinds": []any{crewAuthority + "/widget"}, "ops": []any{"create"}}},
			"callable": vocabulary.RecordPath("core.substrate.reamde.dev/agent", crewAuthority+"/keeper"),
		},
	})
	if err != nil {
		t.Fatalf("put trigger: %v", err)
	}
	fake.script("keep",
		fakeTurn{calls: []fakeCall{{"keyecho", `{}`}}}, // attempt 1: the tool effect commits
		fakeTurn{status: 500},                          // attempt 1: then the transport dies
		fakeTurn{calls: []fakeCall{{"keyecho", `{}`}}}, // attempt 2: the same logical call
		fakeTurn{content: "kept"},                      // attempt 2: settles
	)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewAuthority + "/widget", ID: "w-idem", Properties: map[string]any{"name": "raw"},
	}); err != nil {
		t.Fatalf("put widget: %v", err)
	}
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}

	threads := agentThreadsOf(t, ds, "keeper")
	if len(threads) != 2 {
		t.Fatalf("keeper threads: %d, want 2 (the failed attempt and the retry)", len(threads))
	}
	keyOf := func(threadID string) string {
		t.Helper()
		for _, m := range threadMessages(t, ds, threadID) {
			if m["role"] != "tool" {
				continue
			}
			var out struct {
				Output struct {
					Key string `json:"key"`
				} `json:"output"`
			}
			if err := json.Unmarshal([]byte(fmt.Sprint(m["content"])), &out); err != nil {
				t.Fatalf("tool result: %v", err)
			}
			return out.Output.Key
		}
		t.Fatalf("thread %s has no tool result", threadID)
		return ""
	}
	k1 := keyOf(threads[0]["__id"].(string))
	k2 := keyOf(threads[1]["__id"].(string))
	if k1 == "" || k1 != k2 {
		t.Fatalf("retry minted a different idempotency key: %q vs %q", k1, k2)
	}
	// The key names the delivery, never the (per-attempt) thread.
	if !strings.Contains(k1, tr.ID) {
		t.Fatalf("key %q does not carry the trigger delivery identity", k1)
	}
	for _, th := range threads {
		if strings.Contains(k1, th["__id"].(string)) {
			t.Fatalf("key %q leaks thread id %v", k1, th["__id"])
		}
	}
	// No double-apply: one task stands, and the run ledger shows the retry
	// settling ok.
	var tasks int
	if err := ds.db.QueryRowContext(ctx, `
		SELECT count(*) FROM records WHERE id = 't-idem' AND deleted_at IS NULL`).Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	if tasks != 1 {
		t.Fatalf("t-idem rows: %d", tasks)
	}
	var okRuns int
	if err := ds.db.QueryRowContext(ctx, `
		SELECT count(*) FROM records WHERE kind = $1 AND deleted_at IS NULL
		  AND props->>'trigger' = $2 AND props->>'status' = 'ok'`, typeRun, tr.ID).Scan(&okRuns); err != nil {
		t.Fatal(err)
	}
	if okRuns != 1 {
		t.Fatalf("ok runs: %d", okRuns)
	}
}

func TestChatThreadSingleActiveTurn(t *testing.T) {
	t.Parallel()
	// One chat thread admits ONE active continuation: a second concurrent
	// turn is rejected as a conflict while the first holds the lease, and
	// the thread's tally never loses an increment.
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("chat", fakeTurn{content: "first"})
	res, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewAuthority+"/chatter", "", "hi", nil)
	if err != nil {
		t.Fatalf("open thread: %v", err)
	}

	arrived := make(chan struct{})
	release := make(chan struct{})
	fake.script("chat", fakeTurn{content: "second", arrived: arrived, release: release})
	done := make(chan error, 1)
	go func() {
		_, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewAuthority+"/chatter", res.Thread, "more", nil)
		done <- err
	}()
	<-arrived // the first continuation holds the lease, parked at the model

	if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewAuthority+"/chatter", res.Thread, "sneak", nil); !errors.Is(err, substrate.ErrConflict) {
		close(release)
		t.Fatalf("a second active turn was admitted: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("first continuation: %v", err)
	}

	// The rejected turn left NOTHING on the thread; the tally holds both
	// settled turns exactly.
	threads := agentThreadsOf(t, ds, "chatter")
	if len(threads) != 1 {
		t.Fatalf("threads: %d", len(threads))
	}
	th := threads[0]
	if th["status"] != threadOK || intProp(th, "turns") != 2 || intProp(th, "promptTokens") != 20 {
		t.Fatalf("thread tally: status %v turns %v promptTokens %v", th["status"], th["turns"], th["promptTokens"])
	}
	msgs := threadMessages(t, ds, res.Thread)
	if len(msgs) != 4 { // hi, first, more, second — never "sneak"
		t.Fatalf("messages: %d (%+v)", len(msgs), msgs)
	}

	// The settled thread admits the next turn.
	fake.script("chat", fakeTurn{content: "third"})
	if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewAuthority+"/chatter", res.Thread, "again", nil); err != nil {
		t.Fatalf("post-settle turn: %v", err)
	}
}

func TestCustomLLMEndpointNeverReceivesHostKey(t *testing.T) {
	t.Parallel()
	// The P0 boundary: the host's SUBSTRATE_LLM_API_KEY may travel ONLY to the
	// host's own gateway URL. A row that selects its own baseURL must carry
	// its own apiKey — resolving the two independently would hand the
	// host-wide bearer to an arbitrary repository-chosen endpoint.
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	ds.svc.llmBaseURL = "https://gateway.example.com"
	ds.svc.llmAPIKey = "host-secret-key"

	// A custom baseURL without a row apiKey refuses to resolve at all.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeProvider, ID: "leaky",
		Properties: map[string]any{"wire": "openai", "baseURL": fake.srv.URL},
	}); err != nil {
		t.Fatalf("put leaky llmprovider row: %v", err)
	}
	if _, err := ds.resolveProvider(ctx, "leaky"); err == nil || !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("a row-defined baseURL resolved without a row-defined apiKey: %v", err)
	}
	// The anthropic wire has no host fallback at all: the host's gateway key
	// belongs to the host's gateway, never to Anthropic.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeProvider, ID: "keyless-anthropic",
		Properties: map[string]any{"wire": "anthropic"},
	}); err != nil {
		t.Fatalf("put keyless anthropic row: %v", err)
	}
	if _, err := ds.resolveProvider(ctx, "keyless-anthropic"); err == nil || !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("an anthropic row resolved without its own apiKey: %v", err)
	}

	// A row with both uses ITS key; a row with neither uses the host pair.
	keyed, err := ds.resolveProvider(ctx, "rootllm")
	if err != nil {
		t.Fatalf("resolve rootllm: %v", err)
	}
	if keyed.cfg.APIKey != "row-key-rootllm" || keyed.cfg.BaseURL != fake.srv.URL {
		t.Fatalf("row-defined provider resolved to %q @ %q", keyed.cfg.APIKey, keyed.cfg.BaseURL)
	}
	// A row with no baseURL and no key of its own falls back to the host
	// gateway. Nothing seeds one — the test writes it, like an owner would.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeProvider, ID: "hosted",
		Properties: map[string]any{"name": "hosted", "wire": "openai"},
	}); err != nil {
		t.Fatalf("put the hosted provider row: %v", err)
	}
	hosted, err := ds.resolveProvider(ctx, "hosted")
	if err != nil {
		t.Fatalf("resolve hosted: %v", err)
	}
	if hosted.cfg.BaseURL != "https://gateway.example.com" || hosted.cfg.APIKey != "host-secret-key" {
		t.Fatalf("host fallback pair: %q @ %q", hosted.cfg.APIKey, hosted.cfg.BaseURL)
	}

	// End to end: a chat against a custom-endpoint agent — the fake server
	// must never see the host key as a bearer.
	fake.script("chat", fakeTurn{content: "done"})
	if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewAuthority+"/chatter", "", "hi", func(substrate.AgentEvent) {}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	auths := fake.authsSeen()
	if len(auths) == 0 {
		t.Fatal("the fake endpoint saw no requests")
	}
	for _, a := range auths {
		if strings.Contains(a, "host-secret-key") {
			t.Fatalf("the custom endpoint received the host gateway key: %q", a)
		}
		if a != "Bearer row-key-chatllm" {
			t.Fatalf("unexpected bearer at the custom endpoint: %q", a)
		}
	}
}

func TestAgentChatRoundTrip(t *testing.T) {
	t.Parallel()
	// Chat is the same loop with a live client attached: the first message
	// opens a thread and streams deltas, the second CONTINUES it — the model
	// sees the prior turns, and the thread row keeps one running tally.
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("chat", fakeTurn{content: "hello there"})

	var events []substrate.AgentEvent
	res, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewAuthority+"/chatter", "", "hi", func(ev substrate.AgentEvent) {
		events = append(events, ev)
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if res.Reply != "hello there" || res.Thread == "" {
		t.Fatalf("result: %+v", res)
	}
	if len(events) < 3 || events[0].Kind != substrate.AgentEventThread || events[len(events)-1].Kind != substrate.AgentEventDone {
		t.Fatalf("events: %+v", events)
	}
	deltas := ""
	for _, ev := range events {
		if ev.Kind == substrate.AgentEventDelta {
			deltas += ev.Text
		}
	}
	if deltas != "hello there" {
		t.Fatalf("streamed deltas %q", deltas)
	}

	fake.script("chat", fakeTurn{content: "again"})
	res2, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewAuthority+"/chatter", res.Thread, "more", func(substrate.AgentEvent) {})
	if err != nil {
		t.Fatalf("continue: %v", err)
	}
	if res2.Thread != res.Thread || res2.Reply != "again" {
		t.Fatalf("continuation: %+v", res2)
	}
	// The second request replays the history: system + hi + hello there + more.
	reqs := fake.requestsOf("chat")
	if len(reqs) != 2 {
		t.Fatalf("model calls: %d", len(reqs))
	}
	msgs, _ := reqs[1]["messages"].([]any)
	if len(msgs) != 4 {
		t.Fatalf("continuation history: %d messages (%v)", len(msgs), msgs)
	}
	last, _ := msgs[3].(map[string]any)
	if last["role"] != "user" || last["content"] != "more" {
		t.Fatalf("last history message: %v", last)
	}
	prior, _ := msgs[2].(map[string]any)
	if prior["role"] != "assistant" || prior["content"] != "hello there" {
		t.Fatalf("replayed assistant turn: %v", prior)
	}

	threads := agentThreadsOf(t, ds, "chatter")
	if len(threads) != 1 {
		t.Fatalf("threads: %d", len(threads))
	}
	th := threads[0]
	if th["mode"] != agentModeChat || th["status"] != threadOK {
		t.Fatalf("thread: %+v", th)
	}
	if intProp(th, "turns") != 2 || intProp(th, "promptTokens") != 20 {
		t.Fatalf("accumulated tally: turns %v promptTokens %v", th["turns"], th["promptTokens"])
	}
	msgsRows := threadMessages(t, ds, res.Thread)
	if len(msgsRows) != 4 {
		t.Fatalf("persisted messages: %d", len(msgsRows))
	}
}

// patchRequestCount counts live recordpatchrequests — the inbox size.
func patchRequestCount(t *testing.T, ds *dataset) int {
	t.Helper()
	var n int
	if err := ds.db.QueryRowContext(context.Background(), `
		SELECT count(*) FROM records WHERE kind = $1 AND deleted_at IS NULL`,
		vocabulary.KindRecordPatchRequest).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// toolResultsContain reports whether any tool-result message on the thread
// carries the substring — how the model saw a tool error.
func toolResultsContain(t *testing.T, ds *dataset, thread, want string) bool {
	t.Helper()
	for _, m := range threadMessages(t, ds, thread) {
		if m["role"] == "tool" && strings.Contains(fmt.Sprint(m["content"]), want) {
			return true
		}
	}
	return false
}

func TestProposeRejectsMalformedDiffAtProposeTime(t *testing.T) {
	t.Parallel()
	// Issue 004: the propose tool validates/normalises the diff at PROPOSE
	// time. A wrapper-less diff naming an immutable key (`type`) and an unknown
	// key is a tool error the model sees, and NO recordpatchrequest reaches the
	// inbox — a malformed proposal never becomes a silent no-op approval.
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewAuthority + "/widget", ID: "w-mal", Properties: map[string]any{"name": "raw"},
	}); err != nil {
		t.Fatalf("put widget: %v", err)
	}
	fake.script("root",
		fakeTurn{calls: []fakeCall{{"propose", `{"kind":"crew.test.dev/widget","target":"w-mal","diff":{"type":"article","bogus":true}}`}}},
		fakeTurn{content: "understood, giving up"},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/classifier", "go")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if n := patchRequestCount(t, ds); n != 0 {
		t.Fatalf("a malformed proposal landed %d requests", n)
	}
	if !toolResultsContain(t, ds, res.Thread, "immutable") && !toolResultsContain(t, ds, res.Thread, "not a property") {
		t.Fatalf("no tool result names the shape refusal: %+v", threadMessages(t, ds, res.Thread))
	}
}

func TestProposeDeleteRefusesADiff(t *testing.T) {
	t.Parallel()
	// A delete proposes no VALUES. The tool used to drop a supplied diff
	// silently, leaving the model believing a change it wrote was under review;
	// now it says so, and nothing lands — the same rule the engine's admission
	// and the function SDK hold.
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewAuthority + "/widget", ID: "w-doomed", Properties: map[string]any{"name": "raw"},
	}); err != nil {
		t.Fatalf("put widget: %v", err)
	}
	fake.script("root",
		fakeTurn{calls: []fakeCall{{"propose", `{"op":"delete","kind":"crew.test.dev/widget","target":"w-doomed","diff":{"name":"why"},"rationale":"tidy"}`}}},
		// An EMPTY diff is a claim about the proposal too: presence, not content.
		fakeTurn{calls: []fakeCall{{"propose", `{"op":"delete","kind":"crew.test.dev/widget","target":"w-doomed","diff":{},"rationale":"tidy"}`}}},
		fakeTurn{calls: []fakeCall{{"propose", `{"op":"delete","kind":"crew.test.dev/widget","target":"w-doomed","rationale":"tidy"}`}}},
		fakeTurn{content: "proposed the delete alone"},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/classifier", "go")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var refusals, ok int
	for _, m := range threadMessages(t, ds, res.Thread) {
		if m["role"] != "tool" {
			continue
		}
		if m["ok"] == true {
			ok++
			continue
		}
		if !strings.Contains(fmt.Sprint(m["content"]), "proposes no values") {
			t.Fatalf("a propose failed for another reason: %v", m["content"])
		}
		refusals++
	}
	if refusals != 2 || ok != 1 {
		t.Fatalf("propose results: %d refused, %d ok — want 2 refused (a diff and an empty diff), 1 ok",
			refusals, ok)
	}
	// Only the diffless delete landed.
	if n := patchRequestCount(t, ds); n != 1 {
		t.Fatalf("landed %d requests, want 1 (the diffless delete)", n)
	}
}

func TestProposeCoercesBareDiffAndCreates(t *testing.T) {
	t.Parallel()
	// Issue 004 + 005: a BARE property map (a real model's shape) is coerced to
	// the wrapper and lands a patch request; a create proposal names type + id
	// and lands an op=create request with NO target edge (the record does not
	// exist yet); on accept it mints exactly once.
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewAuthority + "/widget", ID: "w-good", Properties: map[string]any{"name": "raw"},
	}); err != nil {
		t.Fatalf("put widget: %v", err)
	}
	fake.script("root",
		// A bare diff (no `properties` wrapper) coerces and lands.
		fakeTurn{calls: []fakeCall{{"propose", `{"kind":"crew.test.dev/widget","target":"w-good","diff":{"name":"tidier"},"rationale":"cleanup"}`}}},
		// A create proposal for a brand-new widget.
		fakeTurn{calls: []fakeCall{{"propose", `{"op":"create","kind":"crew.test.dev/widget","id":"w-minted","diff":{"properties":{"name":"born"}},"rationale":"the note asks for it"}`}}},
		// A create proposal with a bad shape is refused at propose.
		fakeTurn{calls: []fakeCall{{"propose", `{"op":"create","kind":"crew.test.dev/widget","id":"w-bad","diff":{"nope":1}}`}}},
		fakeTurn{content: "proposed."},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/classifier", "go")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	// Two requests landed (the bare patch + the create); the bad-shape create did not.
	if n := patchRequestCount(t, ds); n != 2 {
		t.Fatalf("landed %d requests, want 2 (bare patch + create; bad create refused)", n)
	}
	if !toolResultsContain(t, ds, res.Thread, "not a property") {
		t.Fatalf("the bad-shape create was not refused at propose: %+v", threadMessages(t, ds, res.Thread))
	}
	// Find the create request and assert its shape: op=create, no target edge.
	var createID string
	if err := ds.db.QueryRowContext(ctx, `
		SELECT id FROM records WHERE kind = $1 AND deleted_at IS NULL
		  AND props->>'op' = 'create'`, vocabulary.KindRecordPatchRequest).Scan(&createID); err != nil {
		t.Fatalf("no op=create request landed: %v", err)
	}
	var targetEdges int
	if err := ds.db.QueryRowContext(ctx, `
		SELECT count(*) FROM edges WHERE rel = 'target' AND src = $1`, createID).Scan(&targetEdges); err != nil {
		t.Fatal(err)
	}
	if targetEdges != 0 {
		t.Fatalf("a create request carries a target edge")
	}
	// Accept the create request: the widget is minted exactly once.
	createEnt, err := ds.Get(ctx, vocabulary.KindRecordPatchRequest, createID)
	if err != nil {
		t.Fatalf("read create request: %v", err)
	}
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindRecordPatchRequest, createID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: &createEnt.Version,
	}); err != nil {
		t.Fatalf("accept create: %v", err)
	}
	minted, err := ds.Get(ctx, crewAuthority+"/widget", "w-minted")
	if err != nil {
		t.Fatalf("minted widget: %v", err)
	}
	if minted.Properties["name"] != "born" {
		t.Fatalf("minted widget wrong: %+v", minted.Properties)
	}
}

// review-p0 #4 + #5: a proposed diff is fully validated at PROPOSE time — a
// secret property is refused (its raw value must never sit in the request's
// json diff), and value coercion, nested-field and edge declaration checks run
// in a non-writing pass so a malformed proposal never reaches the inbox.
func TestProposeDiffValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t)
	// A gauge kind carries what the checks need in one place: a secret, an
	// object property with declared fields, and no edges.
	const gaugeAuthority = "gauge.example.com"
	docs := []map[string]any{
		vocabulary.AuthorityManifest(gaugeAuthority, 0),
		vocabulary.KindManifest(gaugeAuthority, map[string]any{"singular": "gauge", "plural": "gauges"},
			map[string]any{"properties": map[string]any{
				"model":  map[string]any{"type": "string"},
				"apiKey": map[string]any{"type": "secret"},
				"pricing": map[string]any{"type": "object", "fields": map[string]any{
					"inputPer1M": "float", "outputPer1M": "float",
				}},
			}}),
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, docs); err != nil {
		t.Fatalf("install gauge authority: %v", err)
	}
	gauge, err := ds.resolveType(gaugeAuthority + "/gauge")
	if err != nil {
		t.Fatalf("resolve gauge kind: %v", err)
	}
	refused := func(what string, diff map[string]any, op string) {
		t.Helper()
		_, err := normalizeDiff(gauge, diff, op)
		if err == nil {
			t.Fatalf("%s: not refused", what)
		}
		if !errors.Is(err, substrate.ErrValidation) {
			t.Fatalf("%s: want ErrValidation, got %v", what, err)
		}
	}

	// #4: a secret-typed property is refused — on both create and patch —
	// because the request diff is a non-secret json column.
	for _, op := range []string{opCreate, opPatch} {
		diff := map[string]any{"properties": map[string]any{"apiKey": "sk-super-secret"}}
		if _, err := normalizeDiff(gauge, diff, op); err == nil {
			t.Fatalf("op %s: proposing a secret apiKey was not refused", op)
		} else if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "sensitive") {
			t.Fatalf("op %s: secret refusal unclear: %v", op, err)
		}
	}

	// #5a: a wrong-typed value — a string where the schema declares an object —
	// is refused by ordinary coercion, not left for accept time.
	refused("wrong-typed value", map[string]any{"properties": map[string]any{"pricing": "cheap"}}, opPatch)

	// #5b: an undeclared nested field inside an object property is refused.
	refused("undeclared nested field", map[string]any{"properties": map[string]any{"pricing": map[string]any{"bogusField": 1}}}, opPatch)

	// #5c: a create diff naming an edge the type does not declare is refused.
	refused("undeclared create edge", map[string]any{
		"properties": map[string]any{"model": "gpt"},
		"edges":      []any{map[string]any{"rel": "bogus", "to": map[string]any{"id": "x"}}},
	}, opCreate)

	// #5d: an UNDECLARED property set to null slips no longer — declaration is
	// checked before the null-cleanup exception.
	refused("undeclared null property", map[string]any{"properties": map[string]any{"bogus": nil}}, opPatch)

	// A well-formed diff still normalises.
	if _, err := normalizeDiff(gauge, map[string]any{
		"properties": map[string]any{"model": "claude-opus-5"},
	}, opPatch); err != nil {
		t.Fatalf("a well-formed diff was refused: %v", err)
	}
}

// Nothing seeds an llmprovider. A repository holds none until its owner writes
// one, because a row with an openai wire, no endpoint and no key answers
// nothing — it only postpones the failure to the first dispatch, wearing the
// name `default` as if it were configured.
func TestFreshRepositoryHoldsNoProvider(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t)

	page, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{Kinds: []string{typeProvider}},
	})
	if err != nil {
		t.Fatalf("list providers: %v", err)
	}
	if len(page.Records) != 0 {
		t.Fatalf("a fresh repository holds %d llmprovider rows, want none", len(page.Records))
	}

	// And an agent naming one it does not have says so, in terms an owner can
	// act on rather than "does not resolve".
	_, err = ds.resolveProvider(ctx, "default")
	if err == nil {
		t.Fatal("resolving an absent provider must fail")
	}
	if !strings.Contains(err.Error(), "create it") {
		t.Fatalf("the refusal must say what to do, got: %v", err)
	}
}
