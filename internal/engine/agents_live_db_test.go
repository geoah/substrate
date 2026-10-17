package engine

// The LIVE agent chain: one run of the real loop against two real wires at
// once — an Anthropic-backed root agent that calls a function tool, delegates
// to an OpenAI-backed sub-agent, and settles. Everything else in this package
// scripts a fake endpoint (agents_db_test.go); this is the one case that
// proves the whole path is wired to real providers.
//
// It costs real money, so it runs only when BOTH keys are in the environment
// and skips otherwise — before the database container is touched. Keys arrive
// from a gitignored .mise.local.toml; see docs/testing.md.
//
// What it asserts is DURABLE STATE, never prose: the thread rows, their token
// and cost tallies, the parent edge, and the tool transcript. A live model
// chooses its own words and the test must not care.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/llm/livespend"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	liveOpenAIModel    = "gpt-4.1-mini"
	liveAnthropicModel = "claude-haiku-4-5"
	// The openai wire has no endpoint of its own.
	liveOpenAIBaseURL = "https://api.openai.com/v1"
)

// The chain's budgets. A meter on every client the loop builds
// (dataset.wrapLLMClient) charges the ledger before each completion and
// records its usage after, so the request past liveChainMaxRequests, and every
// request after the booked tokens pass liveChainTokenCeiling, is refused and
// never sent. The agents' own budgets sit under that: the conductor takes at
// most liveConductorTurns completions and liveConductorToolCalls tool calls,
// and each tool call may be a speller run of liveSpellerTurns completions.
// Neither provider row declares a contextWindow, so compaction, the one
// completion outside a turn, never runs. liveChainMaxTokens caps each answer,
// so the completion that crosses the token ceiling overshoots it by at most
// its own prompt and that many output tokens.
const (
	liveConductorTurns     = 6
	liveConductorToolCalls = 4
	liveSpellerTurns       = 2
	liveChainMaxRequests   = liveConductorTurns + liveConductorToolCalls*liveSpellerTurns
	liveChainTokenCeiling  = 20_000
	liveChainMaxTokens     = 256
)

// liveSetting reads the same override variables internal/llm's live suite
// honors, so one export re-points both halves.
func liveSetting(env, fallback string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return fallback
}

// liveChainWire is one wire's side of the chain: the key and endpoint its
// provider row carries, and the model its agent asks.
type liveChainWire struct {
	key, baseURL, model string
}

// liveChainWires is both sides of the chain as the environment names them.
// An empty anthropic baseURL is that wire's own endpoint.
func liveChainWires(openaiKey, anthropicKey string) (openai, anthropic liveChainWire) {
	openai = liveChainWire{
		key:     openaiKey,
		baseURL: liveSetting("SUBSTRATE_TEST_OPENAI_BASE_URL", liveOpenAIBaseURL),
		model:   liveSetting("SUBSTRATE_TEST_OPENAI_MODEL", liveOpenAIModel),
	}
	anthropic = liveChainWire{
		key:     anthropicKey,
		baseURL: os.Getenv("SUBSTRATE_TEST_ANTHROPIC_BASE_URL"),
		model:   liveSetting("SUBSTRATE_TEST_ANTHROPIC_MODEL", liveAnthropicModel),
	}
	return openai, anthropic
}

// liveKeys gates the whole test: both wires or nothing. It runs BEFORE any
// fixture, so a machine without keys never starts a container for this.
func liveKeys(t *testing.T) (string, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("live agent test: skipped in -short mode")
	}
	openaiKey, anthropicKey := os.Getenv("OPENAI_API_KEY"), os.Getenv("ANTHROPIC_API_KEY")
	if openaiKey == "" || anthropicKey == "" {
		t.Skip("live agent test: OPENAI_API_KEY and ANTHROPIC_API_KEY must both be set — see docs/testing.md")
	}
	return openaiKey, anthropicKey
}

// chainMeter is internal/llm's live meter on the engine's side of the
// package boundary: it charges the ledger before every completion and records
// the usage after, so a refused completion never reaches the wire. It counts
// completions; the retries the anthropic adapter makes inside one happen
// below it.
type chainMeter struct {
	wire   string
	ledger *livespend.Ledger
	llm.Client
}

func (m chainMeter) Complete(ctx context.Context, req llm.Request, onDelta func(llm.Delta)) (*llm.Result, error) {
	if err := m.ledger.Charge(m.wire); err != nil {
		return nil, err
	}
	res, err := m.Client.Complete(ctx, req, onDelta)
	if res != nil && res.Usage != nil {
		m.ledger.Record(m.wire, res.Usage.PromptTokens, res.Usage.CompletionTokens)
	}
	return res, err
}

// meterAgents puts every client the dataset's agent loops build, sub-agents
// included, behind the ledger, booked under its wire's name.
func meterAgents(ds *dataset, ledger *livespend.Ledger) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.wrapLLMClient = func(wire llm.Wire, c llm.Client) llm.Client {
		return chainMeter{string(wire), ledger, c}
	}
}

// liveChainSpend prints the ledger's summary for the CI run summary and fails
// the test past either ceiling.
func liveChainSpend(t *testing.T, ledger *livespend.Ledger) {
	t.Helper()
	fmt.Print(ledger.Summary())
	if err := ledger.Err(); err != nil {
		t.Error(err)
	}
}

// The meter, driven through a scripted endpoint: once a completion books more
// tokens than the ceiling, the loop's next completion is refused before it is
// sent, and the run ends on that refusal.
func TestAgentChainMeterRefusesTheCompletionPastTheTokenCeiling(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	ledger := livespend.New("test", 10, 100)
	meterAgents(ds, ledger)
	// The first turn books 205 tokens against a ceiling of 100 and calls a
	// tool, so the loop wants a second turn; scripted, it would settle the
	// thread ok.
	fake.script("budget",
		fakeTurn{calls: []fakeCall{{"annotate", `{"id":"t-m1"}`}}, promptTokens: 200},
		fakeTurn{content: "done"},
	)
	_, err := ds.CallAgent(ctx, crewPackage+"/budgeter", "go")
	if !errors.Is(err, livespend.ErrOverBudget) {
		t.Fatalf("call past the token ceiling: %v, want ErrOverBudget", err)
	}
	if got := len(fake.requestsOf("budget")); got != 1 {
		t.Fatalf("the endpoint saw %d completions, want 1: the refused one was sent", got)
	}
	threads := agentThreadsOf(t, ds, "budgeter")
	if len(threads) != 1 || threads[0]["status"] != threadError {
		t.Fatalf("threads: %+v", threads)
	}
	if err := ledger.Err(); !errors.Is(err, livespend.ErrOverBudget) {
		t.Fatalf("ledger: %v, want ErrOverBudget", err)
	}
}

// The chain's override variables, read without a key or a database: CI points
// both wires at a gateway through them, and a misspelled one would send that
// gateway's key to the provider's own endpoint.
func TestChainWiresReadTheLiveOverrides(t *testing.T) {
	t.Setenv("SUBSTRATE_TEST_OPENAI_BASE_URL", "https://gateway.example.com/api/v1")
	t.Setenv("SUBSTRATE_TEST_ANTHROPIC_BASE_URL", "https://gateway.example.com/api")
	t.Setenv("SUBSTRATE_TEST_OPENAI_MODEL", "openai/gpt-4.1-mini")
	t.Setenv("SUBSTRATE_TEST_ANTHROPIC_MODEL", "anthropic/claude-haiku-4.5")
	openai, anthropic := liveChainWires("ko", "ka")
	if want := (liveChainWire{"ko", "https://gateway.example.com/api/v1", "openai/gpt-4.1-mini"}); openai != want {
		t.Fatalf("openai side %+v, want %+v", openai, want)
	}
	if want := (liveChainWire{"ka", "https://gateway.example.com/api", "anthropic/claude-haiku-4.5"}); anthropic != want {
		t.Fatalf("anthropic side %+v, want %+v", anthropic, want)
	}

	for _, env := range []string{"SUBSTRATE_TEST_OPENAI_BASE_URL", "SUBSTRATE_TEST_ANTHROPIC_BASE_URL", "SUBSTRATE_TEST_OPENAI_MODEL", "SUBSTRATE_TEST_ANTHROPIC_MODEL"} {
		t.Setenv(env, "")
	}
	openai, anthropic = liveChainWires("ko", "ka")
	if want := (liveChainWire{"ko", "https://api.openai.com/v1", "gpt-4.1-mini"}); openai != want {
		t.Fatalf("openai side unset %+v, want %+v", openai, want)
	}
	if want := (liveChainWire{"ka", "", "claude-haiku-4-5"}); anthropic != want {
		t.Fatalf("anthropic side unset %+v, want %+v", anthropic, want)
	}
}

// The live chain's own rows and crew under a gateway's model slugs, which
// carry a slash: the provider row's pricing table, the agent manifests, the
// pricing lookup and the thread row all take a slug as written. Each agent
// runs one turn against a scripted endpoint laid out like OpenRouter: the
// speller on the OpenAI wire under /api/v1, the conductor on the Anthropic
// wire under /api.
func TestAgentChainCrewTakesGatewayModelSlugs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t)
	fake := newFakeLLM(t)
	var (
		mu       sync.Mutex
		messages []gatewayMessagesRequest
	)
	gateway := http.NewServeMux()
	gateway.Handle("/api/v1/chat/completions", http.StripPrefix("/api/v1", http.HandlerFunc(fake.handle)))
	gateway.HandleFunc("/api/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		model, _ := req["model"].(string)
		mu.Lock()
		messages = append(messages, gatewayMessagesRequest{model, r.Header.Get("X-Api-Key")})
		mu.Unlock()
		// No tool call, so the conductor settles on its first turn.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "model": model,
			"stop_reason": "end_turn",
			"content":     []any{map[string]any{"type": "text", "text": "DONE five"}},
			"usage":       map[string]any{"input_tokens": 10, "output_tokens": 5},
		})
	})
	srv := httptest.NewServer(gateway)
	t.Cleanup(srv.Close)

	openai := liveChainWire{key: "row-key-openai", baseURL: srv.URL + "/api/v1", model: "openai/gpt-4.1-mini"}
	anthropic := liveChainWire{key: "row-key-anthropic", baseURL: srv.URL + "/api", model: "anthropic/claude-haiku-4.5"}
	installLiveChain(t, ds, openai, anthropic)

	for id, w := range map[string]liveChainWire{"liveopenai": openai, "liveanthropic": anthropic} {
		pc, err := ds.resolveProvider(ctx, id)
		if err != nil {
			t.Fatalf("resolve %s: %v", id, err)
		}
		if pc.cfg.BaseURL != w.baseURL {
			t.Fatalf("%s base URL %q, want %q", id, pc.cfg.BaseURL, w.baseURL)
		}
		if price, ok := pc.pricing[w.model]; !ok || price.inPer1M <= 0 {
			t.Fatalf("%s pricing %+v has no price for %q", id, pc.pricing, w.model)
		}
	}

	fake.script(openai.model, fakeTurn{content: "five"})
	for _, c := range []struct {
		agent, input string
		w            liveChainWire
	}{
		{"speller", "5", openai},
		{"conductor", "Run the procedure.", anthropic},
	} {
		res, err := ds.CallAgent(ctx, crewPackage+"/"+c.agent, c.input)
		if err != nil {
			t.Fatalf("call the %s: %v", c.agent, err)
		}
		if res.Status != threadOK || res.CostUSD <= 0 {
			t.Fatalf("%s: status %q (%s), cost %v", c.agent, res.Status, res.Reason, res.CostUSD)
		}
		threads := agentThreadsOf(t, ds, c.agent)
		if len(threads) != 1 || threads[0]["model"] != c.w.model {
			t.Fatalf("%s threads %+v, want one on %q", c.agent, threads, c.w.model)
		}
	}
	if got := len(fake.requestsOf(openai.model)); got != 1 {
		t.Fatalf("the endpoint saw %d completions for %q, want 1", got, openai.model)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := []gatewayMessagesRequest{{anthropic.model, anthropic.key}}; !slices.Equal(messages, want) {
		t.Fatalf("the Anthropic wire saw %+v, want %+v", messages, want)
	}
}

// gatewayMessagesRequest is what the scripted Anthropic wire records of one
// request: the model it names and the key it pays with.
type gatewayMessagesRequest struct {
	model, key string
}

// installLiveChain puts the chain's two provider rows and installs its crew:
// the add function, the OpenAI-backed speller and the Anthropic-backed
// conductor.
func installLiveChain(t *testing.T, ds *dataset, openai, anthropic liveChainWire) {
	t.Helper()
	ctx := context.Background()

	// Two provider rows, put like any other record. Pricing is what turns the
	// token tally into costUSD, so both rows carry the model under test.
	for _, p := range []struct {
		id, wire string
		w        liveChainWire
		in, out  string
	}{
		{"liveopenai", "openai", openai, "0.4", "1.6"},
		{"liveanthropic", "anthropic", anthropic, "1", "5"},
	} {
		props := map[string]any{
			"wire": p.wire, "apiKey": p.w.key,
			"pricing": []any{map[string]any{"model": p.w.model, "inputPer1M": p.in, "outputPer1M": p.out}},
		}
		// A url property refuses the empty string, and an anthropic row
		// without one buys from the wire's own endpoint.
		if p.w.baseURL != "" {
			props["baseURL"] = p.w.baseURL
		}
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: typeProvider, ID: p.id, Properties: props,
		}); err != nil {
			t.Fatalf("put llm/provider %s: %v", p.id, err)
		}
	}

	// One deterministic function tool and the two agents. The root's prompt is
	// as prescriptive as it gets: the test is about the wiring, so the model
	// is given no room to be creative about the procedure.
	docs := []map[string]any{
		vocabulary.PackageManifest(crewPackage, 0),
		vocabulary.FunctionManifest(crewPackage, "add", map[string]any{
			"description": "Adds two integers. Always use this for arithmetic.",
			"runtime":     vocabulary.RuntimePython,
			// The envelope is required even for a function that writes
			// nothing; this one returns output and emits no effects at all.
			"permissions": map[string]any{"writes": []any{"samples.substrate.reamde.dev/tasks/task"}},
			// The argument list is BOTH the function's input contract and the
			// model-facing tool card, which is what the flat spelling buys.
			"arguments": []any{
				map[string]any{"name": "a", "type": "float", "required": true, "description": "the first addend"},
				map[string]any{"name": "b", "type": "float", "required": true, "description": "the second addend"},
			},
			"source": `
def main(input, host):
    args = input["args"]
    return {"output": {"sum": args["a"] + args["b"]}}
`,
		}),
		vocabulary.AgentManifest(crewPackage, "speller", map[string]any{
			"provider":    "liveopenai",
			"model":       openai.model,
			"description": "Spells a number in English words.",
			"prompt":      "You are given a number. Reply with only that number spelled in English words. Nothing else.",
			"params":      map[string]any{"maxTokens": liveChainMaxTokens},
			"budgets":     map[string]any{"maxTurns": liveSpellerTurns, "deadlineSeconds": 60},
		}),
		vocabulary.AgentManifest(crewPackage, "conductor", map[string]any{
			"provider":    "liveanthropic",
			"model":       anthropic.model,
			"description": "Adds two numbers and has the result spelled out.",
			"prompt": strings.Join([]string{
				"Follow these steps exactly, in order, using the tools you are given.",
				"Step 1: call the add tool with a=2 and b=3.",
				"Step 2: call the speller tool, passing input set to the number the add tool returned.",
				"Step 3: reply with DONE followed by the speller tool's answer, and nothing else.",
				"Never do the arithmetic or the spelling yourself.",
			}, "\n"),
			"tools":     []any{map[string]any{"function": crewPackage + "/add"}},
			"subagents": []any{crewPackage + "/speller"},
			"params":    map[string]any{"maxTokens": liveChainMaxTokens},
			"budgets": map[string]any{
				"maxTurns": liveConductorTurns, "maxToolCalls": liveConductorToolCalls,
				"depth": 3, "deadlineSeconds": 120,
			},
		}),
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, docs); err != nil {
		t.Fatalf("install the live crew: %v", err)
	}
}

func TestLiveAgentChainAcrossWires(t *testing.T) {
	t.Parallel()
	openaiKey, anthropicKey := liveKeys(t)
	ctx := context.Background()

	ds := openInternalDataset(t)
	openai, anthropic := liveChainWires(openaiKey, anthropicKey)
	installLiveChain(t, ds, openai, anthropic)

	ledger := livespend.New("agent chain (internal/engine)", liveChainMaxRequests, liveChainTokenCeiling)
	meterAgents(ds, ledger)
	res, err := ds.CallAgent(ctx, crewPackage+"/conductor", "Run the procedure.")
	// Before any assertion, so a failing chain still prints what it spent.
	liveChainSpend(t, ledger)
	if err != nil {
		t.Fatalf("call the conductor: %v", err)
	}
	if res.Status != threadOK {
		t.Fatalf("status %q (%s): reply %q", res.Status, res.Reason, res.Reply)
	}
	t.Logf("reply: %q (%d turns, %d tool calls, %d prompt + %d completion tokens, $%.6f)",
		res.Reply, res.Turns, res.ToolCalls, res.PromptTokens, res.CompletionTokens, res.CostUSD)

	// The root thread: settled, with a real tally rolled up onto it.
	roots := agentThreadsOf(t, ds, "conductor")
	if len(roots) != 1 {
		t.Fatalf("conductor threads: %d", len(roots))
	}
	root := roots[0]
	if root["status"] != threadOK {
		t.Fatalf("root thread status %v", root["status"])
	}
	rootTokens := intProp(root, "totalTokens")
	if rootTokens <= 0 {
		t.Fatalf("root totalTokens %d", rootTokens)
	}
	if cost, _ := anyFloat(root["costUSD"]); cost <= 0 {
		t.Fatalf("root costUSD %v — the provider row's pricing never applied", root["costUSD"])
	}

	// The function tool ran and its result reached the transcript: the add
	// tool's answer is the sum, in a role-tool message the model then read.
	msgs := threadMessages(t, ds, root["__id"].(string))
	var addResult string
	for _, m := range msgs {
		if m["role"] == "tool" && m["name"] == "add" {
			addResult, _ = m["content"].(string)
			break
		}
	}
	if addResult == "" {
		t.Fatalf("no role-tool message for add in %d messages", len(msgs))
	}
	if !strings.Contains(addResult, "5") {
		t.Fatalf("the add tool answered %q", addResult)
	}

	// The sub-agent ran on the OTHER wire: its own thread, its own tokens, the
	// parent edge back to the root, and its spend included in the root's.
	children := agentThreadsOf(t, ds, "speller")
	if len(children) != 1 {
		t.Fatalf("speller threads: %d", len(children))
	}
	child := children[0]
	if child["mode"] != agentModeSubagent || child["status"] != threadOK {
		t.Fatalf("child thread: mode %v status %v", child["mode"], child["status"])
	}
	var parent string
	if err := ds.db.QueryRowContext(ctx, `
		SELECT `+referencePathSQL("props", "parent")+` FROM records WHERE kind = $2 AND id = $1`,
		child["__id"], typeThread).Scan(&parent); err != nil {
		t.Fatalf("the child's parent: %v", err)
	}
	// The parent is a reference: one flat "<kind>/<id>" path.
	if want := vocabulary.RecordPath(typeThread, root["__id"].(string)); parent != want {
		t.Fatalf("child parent points at %q, want %q", parent, want)
	}
	childTokens := intProp(child, "totalTokens")
	if childTokens <= 0 {
		t.Fatalf("child totalTokens %d", childTokens)
	}
	if rootTokens < childTokens {
		t.Fatalf("root tally %d < child %d — the roll-up did not happen", rootTokens, childTokens)
	}

	// The prose assertion, and the only one: the sub-agent's answer survived
	// into the root's reply. Held leniently on purpose — everything above is
	// what this test is actually for.
	if !strings.Contains(strings.ToLower(res.Reply), "five") {
		t.Fatalf("reply %q does not carry the sub-agent's spelling", res.Reply)
	}
}
