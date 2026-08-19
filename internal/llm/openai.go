package llm

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"

	"github.com/sashabaranov/go-openai"

	"github.com/geoah/substrate/internal/egress"
	"github.com/geoah/substrate/internal/providersecret"
)

// The OpenAI-wire adapter — the one every gateway that copied that wire uses,
// and Azure's variant, which differs only in how the client is configured
// (deployment-shaped paths and an api-key header, both the SDK's business).

type openaiClient struct {
	client *openai.Client
	// apiKey is held only to keep it OUT of an error. On a 401 the endpoint
	// quotes the bearer it refused; scrubbed rebuilds any provider error with
	// providersecret.Scrub so the key cannot reach the agent loop's thread
	// record or the sweep log.
	apiKey string
}

func newOpenAI(cfg Config, azure bool) *openaiClient {
	var oc openai.ClientConfig
	if azure {
		oc = openai.DefaultAzureConfig(cfg.APIKey, cfg.BaseURL)
	} else {
		oc = openai.DefaultConfig(cfg.APIKey)
		if cfg.BaseURL != "" {
			oc.BaseURL = cfg.BaseURL
		}
	}
	// The base URL is a repository-chosen address, so the dial is confined to
	// public destinations (issue #241); a row's extra headers ride over the
	// gated client, never around it.
	oc.HTTPClient = &http.Client{Transport: egress.Transport()}
	if len(cfg.Headers) > 0 {
		oc.HTTPClient = &headerDoer{inner: oc.HTTPClient, headers: cfg.Headers}
	}
	return &openaiClient{client: openai.NewClientWithConfig(oc), apiKey: cfg.APIKey}
}

// scrubbed rebuilds a provider error with the row's bearer taken back out. The
// error is REBUILT, not %w-wrapped: wrapping would carry the original, key and
// all, to anything downstream that unwraps it, and the leak this closes ends
// in a stored record and a host log.
func (c *openaiClient) scrubbed(err error) error {
	return errors.New(providersecret.Scrub(c.apiKey, err.Error()))
}

// headerDoer injects the provider row's extra headers on every request. The
// SDK's own headers — auth included — always win: a row's headers ride
// beside them, never over them, so a colliding key cannot break the wire.
type headerDoer struct {
	inner   openai.HTTPDoer
	headers map[string]string
}

func (d *headerDoer) Do(req *http.Request) (*http.Response, error) {
	for k, v := range d.headers {
		if req.Header.Get(k) == "" {
			req.Header.Set(k, v)
		}
	}
	inner := d.inner
	if inner == nil {
		inner = http.DefaultClient
	}
	return inner.Do(req)
}

func (c *openaiClient) Complete(ctx context.Context, req Request, onDelta func(string)) (*Result, error) {
	oreq := openai.ChatCompletionRequest{Model: req.Model, Messages: openaiMessages(req)}
	if t := req.Params.Temperature; t != nil {
		// The SDK tags Temperature `omitempty`, so a true zero never reaches
		// the wire and the endpoint silently samples at its own default. The
		// documented workaround: send the smallest representable float
		// instead, which is indistinguishable from 0 to the sampler and does
		// survive the marshal.
		oreq.Temperature = *t
		if *t == 0 {
			oreq.Temperature = math.SmallestNonzeroFloat32
		}
	}
	if req.Params.MaxTokens > 0 {
		// max_completion_tokens, not max_tokens: the reasoning models refuse
		// the older key outright, and a gateway that predates it ignores an
		// unknown field rather than failing the call.
		oreq.MaxCompletionTokens = req.Params.MaxTokens
	}
	for _, t := range req.Tools {
		oreq.Tools = append(oreq.Tools, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name: t.Name, Description: t.Description, Parameters: t.Parameters,
			},
		})
	}
	if onDelta == nil {
		return c.oneShot(ctx, oreq)
	}
	return c.stream(ctx, oreq, onDelta)
}

func (c *openaiClient) oneShot(ctx context.Context, oreq openai.ChatCompletionRequest) (*Result, error) {
	resp, err := c.client.CreateChatCompletion(ctx, oreq)
	if err != nil {
		return nil, c.scrubbed(err)
	}
	usage := &Usage{PromptTokens: resp.Usage.PromptTokens, CompletionTokens: resp.Usage.CompletionTokens}
	if len(resp.Choices) == 0 {
		return &Result{Usage: usage}, errors.New("empty response: no choices")
	}
	msg := resp.Choices[0].Message
	return &Result{Content: msg.Content, ToolCalls: neutralToolCalls(msg.ToolCalls), Usage: usage}, nil
}

func (c *openaiClient) stream(ctx context.Context, oreq openai.ChatCompletionRequest, onDelta func(string)) (*Result, error) {
	// The usage tally only rides a stream when it is asked for, and the loop's
	// cost accounting depends on it.
	oreq.StreamOptions = &openai.StreamOptions{IncludeUsage: true}
	stream, err := c.client.CreateChatCompletionStream(ctx, oreq)
	if err != nil {
		return nil, c.scrubbed(err)
	}
	defer func() { _ = stream.Close() }()
	var content strings.Builder
	var usage *Usage
	acc := &toolCallAccumulator{byIndex: map[int]*openai.ToolCall{}}
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, c.scrubbed(err)
		}
		if chunk.Usage != nil {
			usage = &Usage{PromptTokens: chunk.Usage.PromptTokens, CompletionTokens: chunk.Usage.CompletionTokens}
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		if delta.Content != "" {
			content.WriteString(delta.Content)
			onDelta(delta.Content)
		}
		acc.absorb(delta.ToolCalls)
	}
	return &Result{Content: content.String(), ToolCalls: neutralToolCalls(acc.finalize()), Usage: usage}, nil
}

// openaiMessages maps the neutral turns onto the wire's roles 1:1. The system
// prompt leads, because on this wire that is what a system prompt is.
func openaiMessages(req Request) []openai.ChatCompletionMessage {
	out := make([]openai.ChatCompletionMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		out = append(out, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleSystem, Content: req.System})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case RoleAssistant:
			om := openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: m.Content}
			for _, tc := range m.ToolCalls {
				om.ToolCalls = append(om.ToolCalls, openai.ToolCall{
					ID: tc.ID, Type: openai.ToolTypeFunction,
					Function: openai.FunctionCall{Name: tc.Name, Arguments: tc.Arguments},
				})
			}
			out = append(out, om)
		case RoleTool:
			out = append(out, openai.ChatCompletionMessage{
				Role: openai.ChatMessageRoleTool, ToolCallID: m.ToolCallID,
				Name: m.ToolName, Content: m.Content,
			})
		default:
			out = append(out, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: m.Content})
		}
	}
	return out
}

func neutralToolCalls(calls []openai.ToolCall) []ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]ToolCall, 0, len(calls))
	for _, tc := range calls {
		out = append(out, ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments})
	}
	return out
}

// toolCallAccumulator collects streamed tool-call fragments: each chunk
// carries a partial (index, optional name, argument fragment); the full
// arguments string is the per-index concatenation.
type toolCallAccumulator struct {
	byIndex map[int]*openai.ToolCall
	order   []int
}

func (a *toolCallAccumulator) absorb(deltas []openai.ToolCall) {
	for _, d := range deltas {
		i := 0
		if d.Index != nil {
			i = *d.Index
		}
		tc, ok := a.byIndex[i]
		if !ok {
			tc = &openai.ToolCall{Type: openai.ToolTypeFunction}
			a.byIndex[i] = tc
			a.order = append(a.order, i)
		}
		if d.ID != "" {
			tc.ID = d.ID
		}
		if d.Function.Name != "" {
			tc.Function.Name = d.Function.Name
		}
		if d.Function.Arguments != "" {
			tc.Function.Arguments += d.Function.Arguments
		}
	}
}

func (a *toolCallAccumulator) finalize() []openai.ToolCall {
	out := make([]openai.ToolCall, 0, len(a.order))
	for _, i := range a.order {
		out = append(out, *a.byIndex[i])
	}
	return out
}
