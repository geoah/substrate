// Package llm is the neutral completion contract the agent loop speaks: one
// Request in, one Result out, and one adapter per WIRE PROTOCOL.
//
// A wire is a protocol, never a company. OpenRouter, LiteLLM, Together,
// Groq and a local Ollama all speak OpenAI's wire, so all of them are
// WireOpenAI with a different base URL — configuration, not code. New code
// belongs here only when a new wire appears.
//
// The package is a leaf: it knows nothing of records, repositories or the
// engine, so the loop's transport can be tested without a database.
package llm

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Wire is the protocol an adapter speaks — never a company.
type Wire string

const (
	WireOpenAI    Wire = "openai"
	WireAnthropic Wire = "anthropic"
	WireAzure     Wire = "azure"
)

// WirePolicy states what a wire needs from a provider row before an adapter
// can be built. The facts live here, beside the adapters that make them true,
// so adding a wire declares its own rules instead of widening a caller's
// switch. The caller owns the error wording.
//
// There is no host-gateway fallback on any wire: a row carries its own
// endpoint and its own key, so no host-wide bearer can travel to a
// repository-chosen endpoint.
type WirePolicy struct {
	// RequiresBaseURL: the wire has no endpoint of its own, so the row must
	// name one — an azure deployment IS its URL, and an openai-wire gateway is
	// whichever one the row points at.
	RequiresBaseURL bool
	// RequiresAPIKey: the row must hold a key of its own.
	RequiresAPIKey bool
	// Embeddings: the wire has an embeddings endpoint, so a row on it may name
	// an embedModel. Only openai's wire does; anthropic sells no embeddings at
	// all, and the azure one is a per-deployment path this adapter does not
	// build.
	Embeddings bool
}

// wirePolicies is the valid wire set and what each one needs, in the order an
// error names them. One table: New's switch maps a wire to code, this maps it
// to its facts, and nothing else enumerates wires.
var wirePolicies = []struct {
	wire   Wire
	policy WirePolicy
}{
	{WireOpenAI, WirePolicy{RequiresBaseURL: true, RequiresAPIKey: true, Embeddings: true}},
	{WireAnthropic, WirePolicy{RequiresAPIKey: true}},
	{WireAzure, WirePolicy{RequiresBaseURL: true, RequiresAPIKey: true}},
}

// Policy reports what a wire needs from a provider row. The second result is
// false for exactly the wires New refuses.
func (w Wire) Policy() (WirePolicy, bool) {
	for _, e := range wirePolicies {
		if e.wire == w {
			return e.policy, true
		}
	}
	return WirePolicy{}, false
}

// WireNames renders the valid set for an error message.
func WireNames() string {
	out := make([]string, 0, len(wirePolicies))
	for _, e := range wirePolicies {
		out = append(out, string(e.wire))
	}
	return strings.Join(out, ", ")
}

// The message roles the contract carries. There is no system role: a system
// prompt rides Request.System, because that is where half the wires want it
// and the other half can prepend it themselves.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Config is where completions are bought: the endpoint, the bearer, and any
// extra headers the endpoint wants (a gateway's attribution headers, say). An
// empty BaseURL means the adapter's own default endpoint.
type Config struct {
	BaseURL string
	APIKey  string
	Headers map[string]string
}

// Message is one turn. An assistant turn may carry ToolCalls; a tool turn
// answers exactly one of them, naming it by ToolCallID.
type Message struct {
	Role       string
	Content    string
	ToolCalls  []ToolCall
	ToolCallID string
	ToolName   string
}

// Tool is one model-facing tool card. Parameters is a JSON schema.
type Tool struct {
	Name        string
	Description string
	Parameters  any
}

// ToolCall is one call the model asked for; Arguments is the raw JSON text,
// never parsed here — the caller owns the schema.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// Usage is the turn's token tally.
type Usage struct {
	// PromptTokens is the input the wire bills at the input price: on the
	// anthropic wire that excludes cache reads and writes, which it reports
	// apart, and the cost stamp prices exactly this number.
	PromptTokens     int
	CompletionTokens int
	// ContextTokens is the whole input the model saw, cache included: what
	// the context window holds, and so what compaction measures against it.
	// The openai wire's prompt_tokens already counts cached tokens, so there
	// it equals PromptTokens.
	ContextTokens int
}

// The neutral stop reasons a Result carries. Each wire maps its own words
// onto these, and anything it adds later reads as StopOther rather than as a
// clean end.
const (
	// StopEnd is a natural end: the model finished, or hit a stop sequence.
	StopEnd = "end"
	// StopLength is a reply cut off by the output ceiling. Its content is a
	// prefix of what the model meant to write.
	StopLength = "length"
	// StopToolCall is a turn that ended to call tools.
	StopToolCall = "toolCall"
	// StopOther is every other reason, a refusal or a filter among them.
	StopOther = "other"
)

// ErrContextTooLong marks a provider error that says the request did not fit
// the model's context window. The adapters rebuild every provider error as a
// scrubbed string, so this sentinel is the one fact about the failure a
// caller can still test for with errors.Is: it is what lets the agent loop
// compact and retry instead of failing the thread.
var ErrContextTooLong = errors.New("llm: context too long")

// contextTooLongMarkers are the phrasings the wires and the gateways that
// copy them use for a request past the window, matched in lower case.
var contextTooLongMarkers = []string{
	"prompt is too long",
	"prompt too long",
	"context_length_exceeded",
	"context length exceeded",
	"exceeds the context window",
	"maximum context length",
	"too many tokens",
	"input is too long",
	"request too large",
}

// rateLimitMarkers veto the classification: "too many tokens" and "request
// too large" also appear in rate-limit refusals, and compacting a thread
// because a minute's quota ran out would throw its history away for nothing.
var rateLimitMarkers = []string{"rate limit", "rate_limit", "too many requests", "throttl"}

// rateLimitStatus is the 429 status as a whole number: an overflow message
// quotes token counts, and a count such as 214290 must not veto it.
var rateLimitStatus = regexp.MustCompile(`\b429\b`)

// providerError is the error an adapter returns for a provider failure whose
// text is already scrubbed of the row's key. It wraps ErrContextTooLong when
// the text says the context overflowed, and is a plain error otherwise. The
// text is passed in scrubbed, so nothing here can reach the key.
func providerError(scrubbed string) error {
	lower := strings.ToLower(scrubbed)
	if rateLimitStatus.MatchString(lower) {
		return errors.New(scrubbed)
	}
	for _, m := range rateLimitMarkers {
		if strings.Contains(lower, m) {
			return errors.New(scrubbed)
		}
	}
	for _, m := range contextTooLongMarkers {
		if strings.Contains(lower, m) {
			return fmt.Errorf("%w: %s", ErrContextTooLong, scrubbed)
		}
	}
	return errors.New(scrubbed)
}

// Params are the request knobs the contract carries, parsed once from the
// merged provider/agent maps. Temperature is a pointer because current models
// differ on whether a sampling param is even accepted: nil means "do not send
// one".
type Params struct {
	Temperature *float32
	MaxTokens   int
	// ReasoningEffort is how hard a reasoning model thinks, as the word the
	// declaration carries: `reasoning_effort` on the openai and azure wires,
	// `output_config.effort` on the anthropic one. Empty sends nothing, which
	// is NOT the same as "none" — the gpt-5.6 family applies an effort of its
	// own to a request that names none and then refuses function tools for it,
	// so an agent with tools on one of those models needs the word said out
	// loud. Wires accept different sets (no "none" or "minimal" on anthropic,
	// no "max" on openai), so the value travels verbatim and a wire that does
	// not know it refuses the call, the way it refuses a temperature it does
	// not accept.
	ReasoningEffort string
}

// Request is one completion.
type Request struct {
	Model    string
	System   string
	Messages []Message
	Tools    []Tool
	Params   Params
}

// Result is one settled completion. Usage is nil when the wire returned none.
type Result struct {
	Content   string
	ToolCalls []ToolCall
	Usage     *Usage
	// Stop is why the turn ended, one of the Stop constants; empty when the
	// wire said nothing.
	Stop string
}

// Client is one configured place to buy completions from.
type Client interface {
	// Complete runs one turn. onDelta nil is a one-shot request; non-nil
	// streams text deltas as they arrive and still returns the whole result.
	Complete(ctx context.Context, req Request, onDelta func(string)) (*Result, error)
}

// New builds the adapter for a wire. An unknown wire is the only way this
// fails: every adapter's constructor is total, so the error lives here alone.
func New(w Wire, cfg Config) (Client, error) {
	switch w {
	case WireOpenAI:
		return newOpenAI(cfg, false), nil
	case WireAzure:
		return newOpenAI(cfg, true), nil
	case WireAnthropic:
		return newAnthropic(cfg), nil
	default:
		return nil, fmt.Errorf("llm: unknown wire %q — one of %s", w, WireNames())
	}
}
