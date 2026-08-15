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
	"os"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	liveOpenAIModel    = "gpt-4.1-mini"
	liveAnthropicModel = "claude-haiku-4-5"
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

func TestLiveAgentChainAcrossWires(t *testing.T) {
	t.Parallel()
	openaiKey, anthropicKey := liveKeys(t)
	ctx := context.Background()

	openaiModel := liveAgentModel("SUBSTRATE_LLM_TEST_OPENAI_MODEL", liveOpenAIModel)
	anthropicModel := liveAgentModel("SUBSTRATE_LLM_TEST_ANTHROPIC_MODEL", liveAnthropicModel)

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
			t.Fatalf("put llmprovider %s: %v", p.id, err)
		}
	}

	// One deterministic function tool and the two agents. The root's prompt is
	// as prescriptive as it gets: the test is about the wiring, so the model
	// is given no room to be creative about the procedure.
	docs := []map[string]any{
		vocabulary.AuthorityManifest(crewAuthority, 0),
		vocabulary.FunctionManifest(crewAuthority, "add", map[string]any{
			"description": "Adds two integers. Always use this for arithmetic.",
			"runtime":     vocabulary.RuntimePython,
			// The envelope is required even for a function that writes
			// nothing; this one returns output and emits no effects at all.
			"permissions": map[string]any{"writes": []any{"tasks.substrate.reamde.dev/task"}},
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
		vocabulary.AgentManifest(crewAuthority, "speller", map[string]any{
			"provider":    "liveopenai",
			"model":       openaiModel,
			"description": "Spells a number in English words.",
			"prompt":      "You are given a number. Reply with only that number spelled in English words. Nothing else.",
			"budgets":     map[string]any{"maxTurns": 2, "deadlineSeconds": 60},
		}),
		vocabulary.AgentManifest(crewAuthority, "conductor", map[string]any{
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
			"tools":   []any{map[string]any{"function": crewAuthority + "/add"}},
			"agents":  []any{crewAuthority + "/speller"},
			"budgets": map[string]any{"maxTurns": 6, "maxToolCalls": 4, "depth": 3, "deadlineSeconds": 120},
		}),
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, docs); err != nil {
		t.Fatalf("install the live crew: %v", err)
	}

	res, err := ds.CallAgent(ctx, crewAuthority+"/conductor", "Run the procedure.")
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
		if m["role"] == "tool" && m["tool"] == "add" {
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
		SELECT props->>'parent' FROM records WHERE kind = $2 AND id = $1`,
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
