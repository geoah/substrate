package llm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/geoah/substrate/internal/providersecret"
)

// The Anthropic-wire adapter. Three shapes differ from the OpenAI wire and
// they are the whole of this file: the system prompt is a top-level param and
// never a message; the API alternates roles, so consecutive same-side turns —
// tool answers, which are user-side content blocks here, and repeated user
// turns alike — fold into ONE turn carrying several blocks; and max_tokens is
// required on every request.

// anthropicDefaultMaxTokens is what a request carries when the agent names no
// maxTokens: the wire REQUIRES the field, so there is no "unset" to send. 8192
// is inside every current model's output cap; an older model with a lower one
// needs params.maxTokens on the agent, since the wire rejects a ceiling above
// what the model allows.
const anthropicDefaultMaxTokens = 8192

type anthropicClient struct {
	client anthropic.Client
	// apiKey is held only to keep it OUT of an error, the same reason the
	// openai adapter holds one: on a 401 the endpoint quotes the bearer it
	// refused, and scrubbed rebuilds any provider error without it.
	apiKey string
}

func newAnthropic(cfg Config) *anthropicClient {
	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	for k, v := range cfg.Headers {
		opts = append(opts, option.WithHeader(k, v))
	}
	return &anthropicClient{client: anthropic.NewClient(opts...), apiKey: cfg.APIKey}
}

// scrubbed rebuilds a provider error with the row's bearer taken back out,
// rebuilt rather than %w-wrapped so no unwrap can recover the key.
func (c *anthropicClient) scrubbed(err error) error {
	return errors.New(providersecret.Scrub(c.apiKey, err.Error()))
}

func (c *anthropicClient) Complete(ctx context.Context, req Request, onDelta func(string)) (*Result, error) {
	params := anthropic.MessageNewParams{
		Model:     req.Model,
		MaxTokens: int64(anthropicDefaultMaxTokens),
		Messages:  anthropicMessages(req.Messages),
	}
	if req.Params.MaxTokens > 0 {
		params.MaxTokens = int64(req.Params.MaxTokens)
	}
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}
	// Only when the caller asked for one: current models refuse a sampling
	// param they do not accept, and that refusal must reach the loop as an
	// error rather than be papered over with a default.
	if req.Params.Temperature != nil {
		params.Temperature = anthropic.Float(float64(*req.Params.Temperature))
	}
	for _, t := range req.Tools {
		params.Tools = append(params.Tools, anthropic.ToolUnionParam{OfTool: &anthropic.ToolParam{
			Name:        t.Name,
			Description: anthropic.String(t.Description),
			InputSchema: anthropicSchema(t.Parameters),
		}})
	}
	if onDelta == nil {
		msg, err := c.client.Messages.New(ctx, params)
		if err != nil {
			return nil, c.scrubbed(err)
		}
		return anthropicResult(msg), nil
	}
	stream := c.client.Messages.NewStreaming(ctx, params)
	// Only Close releases the response body; Next never does, so a streamed
	// turn leaks it without this.
	defer func() { _ = stream.Close() }()
	var msg anthropic.Message
	for stream.Next() {
		event := stream.Current()
		// The SDK's accumulator owns the reassembly, tool-call arguments
		// arriving as input_json_delta fragments included.
		if err := msg.Accumulate(event); err != nil {
			return nil, c.scrubbed(err)
		}
		if event.Type == "content_block_delta" && event.Delta.Type == "text_delta" && event.Delta.Text != "" {
			onDelta(event.Delta.Text)
		}
	}
	if err := stream.Err(); err != nil {
		return nil, c.scrubbed(err)
	}
	return anthropicResult(&msg), nil
}

// anthropicSchema hands the tool's JSON schema through: the SDK models the
// object's own keys, and anything else the schema declares rides in extras.
func anthropicSchema(params any) anthropic.ToolInputSchemaParam {
	out := anthropic.ToolInputSchemaParam{}
	raw, ok := params.(map[string]any)
	if !ok {
		return out
	}
	for k, v := range raw {
		switch k {
		case "type":
			// The wire's input schema is always an object.
		case "properties":
			out.Properties = v
		case "required":
			req, _ := v.([]any)
			for _, rv := range req {
				if s, ok := rv.(string); ok {
					out.Required = append(out.Required, s)
				}
			}
		default:
			if out.ExtraFields == nil {
				out.ExtraFields = map[string]any{}
			}
			out.ExtraFields[k] = v
		}
	}
	return out
}

// anthropicMessages maps the neutral turns onto the wire's ALTERNATING roles:
// consecutive same-side turns fold into one message carrying several content
// blocks. A run of tool answers is the obvious case, but not the only one — a
// continued thread's replayed history legally holds two user turns in a row
// (loadHistory drops assistant turns that carried tool calls, and a thread
// that settled on an error stored no assistant reply at all), and openThread
// then appends the new user message after it. Without the fold such a
// continuation 400s on every attempt, forever.
func anthropicMessages(messages []Message) []anthropic.MessageParam {
	var out []anthropic.MessageParam
	var side string // the side the pending blocks belong to; "" while none
	var pending []anthropic.ContentBlockParamUnion
	flush := func() {
		if len(pending) == 0 {
			return
		}
		if side == RoleAssistant {
			out = append(out, anthropic.NewAssistantMessage(pending...))
		} else {
			out = append(out, anthropic.NewUserMessage(pending...))
		}
		pending = nil
	}
	for _, m := range messages {
		mside, blocks := anthropicBlocks(m)
		// A turn that renders no blocks is skipped entirely rather than
		// flushed: an empty message would otherwise split a run and put two
		// same-side messages back on the wire.
		if len(blocks) == 0 {
			continue
		}
		if mside != side {
			flush()
			side = mside
		}
		pending = append(pending, blocks...)
	}
	flush()
	return out
}

// anthropicBlocks renders one neutral turn as wire content blocks, and the
// SIDE they belong to: a tool answer is a user-side block on this wire, never
// a turn of its own.
func anthropicBlocks(m Message) (string, []anthropic.ContentBlockParamUnion) {
	switch m.Role {
	case RoleTool:
		return RoleUser, []anthropic.ContentBlockParamUnion{
			anthropic.NewToolResultBlock(m.ToolCallID, m.Content, false),
		}
	case RoleAssistant:
		var blocks []anthropic.ContentBlockParamUnion
		if m.Content != "" {
			blocks = append(blocks, anthropic.NewTextBlock(m.Content))
		}
		for _, tc := range m.ToolCalls {
			blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, toolInput(tc.Arguments), tc.Name))
		}
		return RoleAssistant, blocks
	default:
		if m.Content == "" {
			return RoleUser, nil
		}
		return RoleUser, []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock(m.Content)}
	}
}

// toolInput carries the call's arguments through as the JSON the model wrote.
// The Messages API requires tool_use.input to be an OBJECT, and validity is
// not enough: `null`, `[]` and `5` all parse, and all 400 the request that
// echoes the call back on the next turn. Anything that is not an object — an
// empty or unparseable string included — travels as one.
func toolInput(arguments string) any {
	var obj map[string]any
	if err := json.Unmarshal([]byte(arguments), &obj); err != nil || obj == nil {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(arguments)
}

func anthropicResult(msg *anthropic.Message) *Result {
	res := &Result{Usage: &Usage{
		PromptTokens:     int(msg.Usage.InputTokens),
		CompletionTokens: int(msg.Usage.OutputTokens),
	}}
	var content strings.Builder
	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			content.WriteString(block.Text)
		case "tool_use":
			res.ToolCalls = append(res.ToolCalls, ToolCall{
				ID: block.ID, Name: block.Name, Arguments: string(block.Input),
			})
		}
	}
	res.Content = content.String()
	return res
}
