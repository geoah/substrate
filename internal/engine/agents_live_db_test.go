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
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/llm/livespend"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	liveOpenAIModel    = "gpt-4.1-mini"
	liveAnthropicModel = "claude-haiku-4-5"
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

// liveAgentModel reads the same override variables internal/llm's live suite
// honors, so one export re-points both halves.
func liveAgentModel(env, fallback string) string {
	if m := os.Getenv(env); m != "" {
		return m
	}
	return fallback
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
// completions; the retries the Anthropic SDK makes inside one happen below it.
type chainMeter struct {
	wire   string
	ledger *livespend.Ledger
	llm.Client
}

func (m chainMeter) Complete(ctx context.Context, req llm.Request, onDelta func(string)) (*llm.Result, error) {
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

func TestLiveAgentChainAcrossWires(t *testing.T) {
	t.Parallel()
	openaiKey, anthropicKey := liveKeys(t)
	ctx := context.Background()

	openaiModel := liveAgentModel("SUBSTRATE_TEST_OPENAI_MODEL", liveOpenAIModel)
	anthropicModel := liveAgentModel("SUBSTRATE_TEST_ANTHROPIC_MODEL", liveAnthropicModel)

	ds := openInternalDataset(t)

	// Two provider rows, put like any other record. Pricing is what turns the
	// token tally into costUSD, so both rows carry the model under test.
	for _, p := range []struct {
		id      string
		props   map[string]any
		pricing []any
	}{
		{"liveopenai", map[string]any{
			"wire": "openai", "baseURL": "https://api.openai.com/v1", "apiKey": openaiKey,
		}, []any{map[string]any{"model": openaiModel, "inputPer1M": "0.4", "outputPer1M": "1.6"}}},
		{"liveanthropic", map[string]any{
			// No baseURL: the anthropic wire has its own endpoint.
			"wire": "anthropic", "apiKey": anthropicKey,
		}, []any{map[string]any{"model": anthropicModel, "inputPer1M": "1", "outputPer1M": "5"}}},
	} {
		p.props["pricing"] = p.pricing
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: typeProvider, ID: p.id, Properties: p.props,
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
			"model":       openaiModel,
			"description": "Spells a number in English words.",
			"prompt":      "You are given a number. Reply with only that number spelled in English words. Nothing else.",
			"params":      map[string]any{"maxTokens": liveChainMaxTokens},
			"budgets":     map[string]any{"maxTurns": liveSpellerTurns, "deadlineSeconds": 60},
		}),
		vocabulary.AgentManifest(crewPackage, "conductor", map[string]any{
			"provider":    "liveanthropic",
			"model":       anthropicModel,
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
