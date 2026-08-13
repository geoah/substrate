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
	"fmt"
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
// switch. The caller owns the host-gateway pairing and the error wording.
type WirePolicy struct {
	// HostGatewayFallback: on this wire an empty baseURL MEANS the host's
	// configured gateway. The URL and the key fall back as ONE unit — a row
	// naming its own baseURL never inherits the host's key, or a host-wide
	// gateway bearer would travel to a repository-chosen endpoint.
	HostGatewayFallback bool
	// RequiresBaseURL: the wire has neither an endpoint of its own nor a
	// fallback, so the row must name one — an azure deployment IS its URL.
	RequiresBaseURL bool
	// RequiresAPIKey: the row must end up holding a key, its own or (only
	// under HostGatewayFallback) the host's.
	RequiresAPIKey bool
}

// wirePolicies is the valid wire set and what each one needs, in the order an
// error names them. One table: New's switch maps a wire to code, this maps it
// to its facts, and nothing else enumerates wires.
var wirePolicies = []struct {
	wire   Wire
	policy WirePolicy
}{
	{WireOpenAI, WirePolicy{HostGatewayFallback: true, RequiresAPIKey: true}},
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
	PromptTokens     int
	CompletionTokens int
}

// Params are the request knobs the contract carries, parsed once from the
// merged provider/agent maps. Temperature is a pointer because current models
// differ on whether a sampling param is even accepted: nil means "do not send
// one".
type Params struct {
	Temperature *float32
	MaxTokens   int
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
