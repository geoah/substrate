package llm

// The LIVE adapter suite: every case here buys real completions from a real
// endpoint, so it runs only when the wire's key is in the environment and
// SKIPS otherwise — the default suites stay hermetic and CI, which holds no
// keys, never notices. Keys arrive from a gitignored .mise.local.toml; see
// docs/testing.md.
//
// Every name starts with TestLive so `-run '^TestLive'` selects exactly this
// set, and the prompts and token budgets are deliberately tiny: a whole pass
// costs a fraction of a cent. What is asserted is the ADAPTER's contract —
// content, usage, tool-call round trip, streamed deltas — never the model's
// prose, which is nobody's to promise.

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// The cheapest model on each wire that still calls tools reliably. Both are
// overridable, so a key without one of them is one env var away from green.
const (
	liveOpenAIModel    = "gpt-4.1-mini"
	liveAnthropicModel = "claude-haiku-4-5"

	liveOpenAIModelEnv    = "SUBSTRATE_TEST_OPENAI_MODEL"
	liveAnthropicModelEnv = "SUBSTRATE_TEST_ANTHROPIC_MODEL"
)

// liveMaxTokens caps every live request. These tests spend real money; nothing
// here needs more than a sentence back.
const liveMaxTokens = 256

// liveKey is the gate: no key, no test. It skips rather than fails, naming the
// variable it wanted, and skips under -short too so `mise run test:short`
// never buys anything on a laptop that happens to hold keys.
func liveKey(t *testing.T, name string) string {
	t.Helper()
	if testing.Short() {
		t.Skip("live LLM test: skipped in -short mode")
	}
	key := os.Getenv(name)
	if key == "" {
		t.Skipf("live LLM test: %s is not set — see docs/testing.md", name)
	}
	return key
}

func liveModel(env, fallback string) string {
	if m := os.Getenv(env); m != "" {
		return m
	}
	return fallback
}

func liveContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// liveWire is one real endpoint under test: the client, and the model to ask.
// The cases below run once per entry, each skipping on its own missing key.
//
// Every client is wrapped in a liveMeter, so no case can reach a real endpoint
// without the pass ledger booking it first — see livespend_test.go for the
// ceilings that wrapping enforces.
type liveWire struct {
	name  string
	build func(t *testing.T) (Client, string)
}

func liveWires() []liveWire {
	return []liveWire{
		{"openai", func(t *testing.T) (Client, string) {
			t.Helper()
			key := liveKey(t, "OPENAI_API_KEY")
			// The canonical endpoint, spelled out: this wire is spoken by a
			// dozen gateways and the test means OpenAI's own.
			c, err := New(WireOpenAI, Config{BaseURL: "https://api.openai.com/v1", APIKey: key})
			if err != nil {
				t.Fatalf("build openai client: %v", err)
			}
			return liveMeter{"openai", c}, liveModel(liveOpenAIModelEnv, liveOpenAIModel)
		}},
		{"anthropic", func(t *testing.T) (Client, string) {
			t.Helper()
			key := liveKey(t, "ANTHROPIC_API_KEY")
			// No BaseURL: the SDK's own default endpoint.
			c, err := New(WireAnthropic, Config{APIKey: key})
			if err != nil {
				t.Fatalf("build anthropic client: %v", err)
			}
			return liveMeter{"anthropic", c}, liveModel(liveAnthropicModelEnv, liveAnthropicModel)
		}},
	}
}

// The one-shot prompt every case that only needs text reuses.
const (
	liveOneWordSystem = "Answer with exactly one word."
	liveOneWordUser   = "What color is the sky on a clear day? One word."
)

// liveAddTool is the arithmetic tool the round trip hangs on: a strict
// two-integer object, described so the model has no excuse to answer from
// memory.
func liveAddTool() Tool {
	return Tool{
		Name:        "add",
		Description: "Adds two integers. Always use this for arithmetic.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"a": map[string]any{"type": "integer", "description": "the first addend"},
				"b": map[string]any{"type": "integer", "description": "the second addend"},
			},
			"required":             []any{"a", "b"},
			"additionalProperties": false,
		},
	}
}

func TestLiveCompletion(t *testing.T) {
	for _, w := range liveWires() {
		t.Run(w.name, func(t *testing.T) {
			client, model := w.build(t)
			res, err := client.Complete(liveContext(t), Request{
				Model:    model,
				System:   liveOneWordSystem,
				Messages: []Message{{Role: RoleUser, Content: liveOneWordUser}},
				Params:   Params{MaxTokens: liveMaxTokens},
			}, nil)
			if err != nil {
				t.Fatalf("complete: %v", err)
			}
			if strings.TrimSpace(res.Content) == "" {
				t.Fatalf("empty content: %+v", res)
			}
			if res.Usage == nil {
				t.Fatal("no usage: the loop's cost accounting depends on it")
			}
			if res.Usage.PromptTokens <= 0 || res.Usage.CompletionTokens <= 0 {
				t.Fatalf("usage %+v", *res.Usage)
			}
			t.Logf("%s/%s: %q (%d prompt, %d completion)",
				w.name, model, strings.TrimSpace(res.Content), res.Usage.PromptTokens, res.Usage.CompletionTokens)
		})
	}
}

func TestLiveToolRoundTrip(t *testing.T) {
	// The whole shape of a tool turn against a real endpoint: the model asks
	// for the tool with parseable arguments, we answer with a RoleTool turn,
	// and the second completion carries the answer and no further calls.
	for _, w := range liveWires() {
		t.Run(w.name, func(t *testing.T) {
			client, model := w.build(t)
			ctx := liveContext(t)
			messages := []Message{{
				Role:    RoleUser,
				Content: "Use the add tool to add 2 and 3, then reply with just the number.",
			}}
			req := Request{
				Model:    model,
				System:   "You are a calculator. Use the tools you are given.",
				Messages: messages,
				Tools:    []Tool{liveAddTool()},
				Params:   Params{MaxTokens: liveMaxTokens},
			}
			first, err := client.Complete(ctx, req, nil)
			if err != nil {
				t.Fatalf("first complete: %v", err)
			}
			if len(first.ToolCalls) == 0 {
				t.Fatalf("no tool call: content %q", first.Content)
			}
			call := first.ToolCalls[0]
			if call.Name != "add" {
				t.Fatalf("tool call names %q, want add", call.Name)
			}
			var args map[string]any
			if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
				t.Fatalf("arguments %q are not JSON: %v", call.Arguments, err)
			}
			for _, k := range []string{"a", "b"} {
				if _, ok := args[k]; !ok {
					t.Fatalf("arguments %v name no %q", args, k)
				}
			}

			// Answer EVERY call the turn asked for: a wire that alternates
			// roles rejects a turn that leaves one unanswered.
			messages = append(messages, Message{Role: RoleAssistant, Content: first.Content, ToolCalls: first.ToolCalls})
			for _, tc := range first.ToolCalls {
				messages = append(messages, Message{
					Role: RoleTool, ToolCallID: tc.ID, ToolName: tc.Name, Content: "5",
				})
			}
			req.Messages = messages
			second, err := client.Complete(ctx, req, nil)
			if err != nil {
				t.Fatalf("second complete: %v", err)
			}
			if len(second.ToolCalls) != 0 {
				t.Fatalf("the answered turn asked for %d more tools", len(second.ToolCalls))
			}
			if !strings.Contains(second.Content, "5") {
				t.Fatalf("final content %q carries no 5", second.Content)
			}
		})
	}
}

func TestLiveStreaming(t *testing.T) {
	// The deltas ARE the content: whatever onDelta saw, concatenated, is what
	// the settled result holds — and usage still arrives (on the OpenAI wire
	// only because the adapter asks for it).
	for _, w := range liveWires() {
		t.Run(w.name, func(t *testing.T) {
			client, model := w.build(t)
			var acc strings.Builder
			res, err := client.Complete(liveContext(t), Request{
				Model:    model,
				System:   liveOneWordSystem,
				Messages: []Message{{Role: RoleUser, Content: liveOneWordUser}},
				Params:   Params{MaxTokens: liveMaxTokens},
			}, func(delta string) { acc.WriteString(delta) })
			if err != nil {
				t.Fatalf("stream: %v", err)
			}
			if strings.TrimSpace(res.Content) == "" {
				t.Fatalf("empty content: %+v", res)
			}
			if acc.String() != res.Content {
				t.Fatalf("deltas %q, result %q", acc.String(), res.Content)
			}
			if res.Usage == nil || res.Usage.CompletionTokens <= 0 {
				t.Fatalf("streamed usage %+v", res.Usage)
			}
		})
	}
}

func TestLiveAnthropicConsecutiveUserTurns(t *testing.T) {
	// The role-alternation fold, against the wire that enforces it: a replayed
	// history legally holds two user turns in a row, and anthropicMessages
	// merges them into one. Without the fold this is a 400, forever — which is
	// why it is worth one live request.
	key := liveKey(t, "ANTHROPIC_API_KEY")
	c, err := New(WireAnthropic, Config{APIKey: key})
	if err != nil {
		t.Fatalf("build anthropic client: %v", err)
	}
	client := liveMeter{"anthropic", c}
	res, err := client.Complete(liveContext(t), Request{
		Model:  liveModel(liveAnthropicModelEnv, liveAnthropicModel),
		System: "Answer with exactly one word.",
		Messages: []Message{
			{Role: RoleUser, Content: "Remember the number 7."},
			{Role: RoleUser, Content: "What number did I ask you to remember? Reply with just the digit."},
		},
		Params: Params{MaxTokens: liveMaxTokens},
	}, nil)
	if err != nil {
		t.Fatalf("two user turns in a row: %v", err)
	}
	if strings.TrimSpace(res.Content) == "" {
		t.Fatalf("empty content: %+v", res)
	}
	if !strings.Contains(res.Content, "7") {
		t.Fatalf("content %q — both user turns should have reached the model", res.Content)
	}
}
