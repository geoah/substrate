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
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/llm/livespend"
)

// The cheapest model on each wire that still calls tools reliably, and the
// endpoint each wire buys from. Every one is overridable: a key without one of
// the models is one env var away from green, and CI runs both wires through a
// gateway that speaks them (docs/testing.md).
const (
	liveOpenAIModel    = "gpt-4.1-mini"
	liveAnthropicModel = "claude-haiku-4-5"
	// OpenAI's own endpoint, spelled out: the openai wire has no default of
	// its own. The anthropic wire's default is empty, the wire's own endpoint.
	liveOpenAIBaseURL = "https://api.openai.com/v1"

	// The thinking case needs a model that takes adaptive thinking, which
	// the default Anthropic model does not.
	liveAnthropicThinkingModel = "claude-haiku-5-5"

	liveOpenAIModelEnv            = "SUBSTRATE_TEST_OPENAI_MODEL"
	liveAnthropicThinkingModelEnv = "SUBSTRATE_TEST_ANTHROPIC_THINKING_MODEL"
	liveAnthropicModelEnv         = "SUBSTRATE_TEST_ANTHROPIC_MODEL"
	liveOpenAIBaseURLEnv          = "SUBSTRATE_TEST_OPENAI_BASE_URL"
	liveAnthropicBaseURLEnv       = "SUBSTRATE_TEST_ANTHROPIC_BASE_URL"
)

// liveMaxTokens caps every live request. These tests spend real money; nothing
// here needs more than a sentence back.
const liveMaxTokens = 256

// The pass's ceilings, across every wire. The suite makes about two dozen
// requests; the cache case's two calls each carry a prefix of some 6,000
// tokens, long enough to cache, and the replay cases send an earlier run's
// exchange again. Both leave room for a case being added and none for a loop.
// Raising either is a deliberate edit.
const (
	liveMaxRequests  = 36
	liveTokenCeiling = 40_000
)

// liveLedger books every live request this binary makes; TestMain prints it
// and fails the run past a ceiling. Package-level because what it bounds is
// the pass: a per-test budget cannot see a second test spending the same money.
var liveLedger = livespend.New("adapter suite (internal/llm)", liveMaxRequests, liveTokenCeiling)

// liveMeter is a Client that charges the ledger before every completion and
// records the usage after, so no case reaches a real endpoint unbooked. It
// counts completions: the retries the anthropic adapter makes inside one,
// after a connection error or a 408, 409, 429 or 5xx, happen below it and are
// not counted.
type liveMeter struct {
	wire   string
	ledger *livespend.Ledger
	Client
}

func (m liveMeter) Complete(ctx context.Context, req Request, onDelta func(Delta)) (*Result, error) {
	if err := m.ledger.Charge(m.wire); err != nil {
		return nil, err
	}
	res, err := m.Client.Complete(ctx, req, onDelta)
	if res != nil && res.Usage != nil {
		m.ledger.Record(m.wire, res.Usage.PromptTokens, res.Usage.CompletionTokens)
	}
	return res, err
}

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

func liveSetting(env, fallback string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return fallback
}

// liveEndpoint is one wire's live target: the key that pays, and the endpoint
// and model its override variables name.
type liveEndpoint struct {
	wire       Wire
	keyEnv     string
	baseURLEnv string
	baseURL    string
	modelEnv   string
	model      string
}

var (
	liveOpenAI = liveEndpoint{
		wire: WireOpenAI, keyEnv: "OPENAI_API_KEY",
		baseURLEnv: liveOpenAIBaseURLEnv, baseURL: liveOpenAIBaseURL,
		modelEnv: liveOpenAIModelEnv, model: liveOpenAIModel,
	}
	liveAnthropic = liveEndpoint{
		wire: WireAnthropic, keyEnv: "ANTHROPIC_API_KEY",
		baseURLEnv: liveAnthropicBaseURLEnv,
		modelEnv:   liveAnthropicModelEnv, model: liveAnthropicModel,
	}
)

func (e liveEndpoint) config(key string) Config {
	return Config{BaseURL: liveSetting(e.baseURLEnv, e.baseURL), APIKey: key}
}

func (e liveEndpoint) modelName() string {
	return liveSetting(e.modelEnv, e.model)
}

// client gates on the wire's key, then builds its client behind the pass's
// meter.
func (e liveEndpoint) client(t *testing.T) (Client, string) {
	t.Helper()
	key := liveKey(t, e.keyEnv)
	c, err := New(e.wire, e.config(key))
	if err != nil {
		t.Fatalf("build %s client: %v", e.wire, err)
	}
	return liveMeter{string(e.wire), liveLedger, c}, e.modelName()
}

func liveContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// liveWire is one real endpoint under test: the client, metered, and the
// model to ask. The cases below run once per entry, each skipping on its own
// missing key.
type liveWire struct {
	name  string
	build func(t *testing.T) (Client, string)
}

func liveWires() []liveWire {
	return []liveWire{
		{"openai", liveOpenAI.client},
		{"anthropic", liveAnthropic.client},
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
			}, func(d Delta) { acc.WriteString(d.Text) })
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
	client, model := liveAnthropic.client(t)
	res, err := client.Complete(liveContext(t), Request{
		Model:  model,
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

// liveCacheSystem is a system prompt past the longest minimum cacheable
// prefix (4096 tokens, Haiku 4.5's): a shorter one is never cached, and the
// case would prove nothing.
func liveCacheSystem() string {
	var b strings.Builder
	b.WriteString("Answer with exactly one word. The numbered notes below are reference material; never quote them.\n")
	for i := range 220 {
		fmt.Fprintf(&b, "Note %d: a record carries a kind, an id and its declared properties, and note %d is not note %d.\n", i, i, i+1)
	}
	return b.String()
}

func TestLiveAnthropicReadsTheRunsPrefixFromTheCache(t *testing.T) {
	// The second call of a run repeats the first's tools, system prompt and
	// conversation, so it reads them back from the cache the first call's
	// marks wrote. Five minutes, the cheaper write.
	client, model := liveAnthropic.client(t)
	first := Request{
		Model: model, System: liveCacheSystem(),
		Messages: []Message{{Role: RoleUser, Content: liveOneWordUser}},
		Tools:    []Tool{liveAddTool()},
		Params:   Params{MaxTokens: liveMaxTokens},
		Cache:    &Cache{TTL: CacheTTL5m},
	}
	a, err := client.Complete(liveContext(t), first, nil)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if a.Usage == nil || a.Usage.CacheReadTokens+a.Usage.CacheWriteTokens == 0 {
		t.Fatalf("the first call neither wrote nor read a cache: %+v", a.Usage)
	}
	second := first
	second.Messages = append(append([]Message(nil), first.Messages...),
		Message{Role: RoleAssistant, Content: a.Content},
		Message{Role: RoleUser, Content: "And at night? One word."},
	)
	b, err := client.Complete(liveContext(t), second, nil)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if b.Usage == nil || b.Usage.CacheReadTokens < 4096 {
		t.Fatalf("the second call read %+v from the cache, want the whole prefix", b.Usage)
	}
}

func TestLiveOpenAIAcceptsThePromptCacheKey(t *testing.T) {
	// prompt_cache_key is sent only where a row opts in; OpenAI's own
	// endpoint, which the seeded row names, must take it.
	key := liveKey(t, liveOpenAI.keyEnv)
	cfg := liveOpenAI.config(key)
	cfg.CacheKey = true
	c, err := New(WireOpenAI, cfg)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	client := liveMeter{string(WireOpenAI), liveLedger, c}
	res, err := client.Complete(liveContext(t), Request{
		Model: liveOpenAI.modelName(), System: liveOneWordSystem,
		Messages: []Message{{Role: RoleUser, Content: liveOneWordUser}},
		Params:   Params{MaxTokens: liveMaxTokens},
		Cache:    &Cache{Key: "substrate-live-test", TTL: CacheTTL5m},
	}, nil)
	if err != nil {
		t.Fatalf("a request carrying prompt_cache_key: %v", err)
	}
	if strings.TrimSpace(res.Content) == "" {
		t.Fatalf("empty content: %+v", res)
	}
}

// The replay of an earlier run's tool exchange (engine decision 0201): a
// continued thread sends the call and its stored result again, without the
// signed thinking that rode the run that made it. Each wire must accept that
// history twice over: after the run's own prose reply, and as the thread's
// last turn before a new user message (a run that ended inside its tool loop),
// which the anthropic wire folds into one user turn with the tool result.
func TestLiveReplayedToolExchange(t *testing.T) {
	for _, w := range liveWires() {
		t.Run(w.name, func(t *testing.T) {
			client, model := w.build(t)
			liveReplay(t, client, model, Params{MaxTokens: liveMaxTokens})
		})
	}
	// Thinking on, where the signed blocks are what an earlier turn would
	// otherwise have to carry. Only on Anthropic's own endpoint: a gateway's
	// spelling of the thinking model is not this suite's to guess.
	t.Run("anthropic thinking", func(t *testing.T) {
		if os.Getenv(liveAnthropicBaseURLEnv) != "" {
			t.Skipf("the thinking case runs against Anthropic's own endpoint; %s is set", liveAnthropicBaseURLEnv)
		}
		client, _ := liveAnthropic.client(t)
		model := liveSetting(liveAnthropicThinkingModelEnv, liveAnthropicThinkingModel)
		liveReplay(t, client, model, Params{MaxTokens: 2048, ReasoningEffort: "low", ReasoningSummary: true})
	})
}

func liveReplay(t *testing.T, client Client, model string, params Params) {
	t.Helper()
	ctx := liveContext(t)
	question := Message{Role: RoleUser, Content: "Use the add tool to add 2 and 3, then reply with just the number."}
	req := Request{
		Model:    model,
		System:   "You are a calculator. Use the tools you are given.",
		Messages: []Message{question},
		Tools:    []Tool{liveAddTool()},
		Params:   params,
	}
	first, err := client.Complete(ctx, req, nil)
	if err != nil {
		t.Fatalf("first complete: %v", err)
	}
	if len(first.ToolCalls) == 0 {
		t.Fatalf("no tool call: content %q", first.Content)
	}
	// The run itself: its own thinking rides back beside the results.
	answers := make([]Message, 0, len(first.ToolCalls))
	for _, tc := range first.ToolCalls {
		answers = append(answers, Message{Role: RoleTool, ToolCallID: tc.ID, ToolName: tc.Name, Content: `{"sum":5}`})
	}
	req.Messages = append([]Message{question, {
		Role: RoleAssistant, Content: first.Content, ToolCalls: first.ToolCalls, ReasoningBlocks: first.ReasoningBlocks,
	}}, answers...)
	second, err := client.Complete(ctx, req, nil)
	if err != nil {
		t.Fatalf("in-run answer: %v", err)
	}
	if strings.TrimSpace(second.Content) == "" {
		t.Fatalf("in-run answer is empty: %+v", second)
	}

	// The next run: the same exchange as the thread stored it, thinking gone.
	replayed := append([]Message{question, {Role: RoleAssistant, Content: first.Content, ToolCalls: first.ToolCalls}}, answers...)
	followUp := Message{Role: RoleUser, Content: "What did the add tool return? Reply with just the number."}
	for name, history := range map[string][]Message{
		"after the reply":  append(append(append([]Message(nil), replayed...), Message{Role: RoleAssistant, Content: second.Content}), followUp),
		"as the last turn": append(append([]Message(nil), replayed...), followUp),
	} {
		req.Messages = history
		res, err := client.Complete(ctx, req, nil)
		if err != nil {
			t.Fatalf("replayed %s: %v", name, err)
		}
		if !strings.Contains(res.Content, "5") && len(res.ToolCalls) == 0 {
			t.Fatalf("replayed %s: content %q carries no 5", name, res.Content)
		}
	}
}
