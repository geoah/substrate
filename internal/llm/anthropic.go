package llm

import (
	"context"
	"encoding/json"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// The Anthropic-wire adapter. Three shapes differ from the OpenAI wire and
// they are the whole of this file: the system prompt is a top-level param and
// never a message; a tool result is a content block inside a USER turn, so
// consecutive tool answers must be grouped into ONE turn (the API alternates
// roles); and max_tokens is required on every request.

// anthropicDefaultMaxTokens is what a request carries when the agent names no
// maxTokens: the wire REQUIRES the field, so there is no "unset" to send.
const anthropicDefaultMaxTokens = 8192

type anthropicClient struct {
	client anthropic.Client
}

func newAnthropic(cfg Config) (*anthropicClient, error) {
	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	for k, v := range cfg.Headers {
		opts = append(opts, option.WithHeader(k, v))
	}
	return &anthropicClient{client: anthropic.NewClient(opts...)}, nil
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
			return nil, err
		}
		return anthropicResult(msg), nil
	}
	stream := c.client.Messages.NewStreaming(ctx, params)
	var msg anthropic.Message
	for stream.Next() {
		event := stream.Current()
		// The SDK's accumulator owns the reassembly, tool-call arguments
		// arriving as input_json_delta fragments included.
		if err := msg.Accumulate(event); err != nil {
			return nil, err
		}
		if event.Type == "content_block_delta" && event.Delta.Type == "text_delta" && event.Delta.Text != "" {
			onDelta(event.Delta.Text)
		}
	}
	if err := stream.Err(); err != nil {
		return nil, err
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
			for _, rv := range anySlice(v) {
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

func anySlice(v any) []any {
	switch s := v.(type) {
	case []any:
		return s
	case []string:
		out := make([]any, 0, len(s))
		for _, e := range s {
			out = append(out, e)
		}
		return out
	default:
		return nil
	}
}

// anthropicMessages maps the neutral turns onto the wire's alternating roles.
// A run of tool answers becomes ONE user turn carrying every tool_result
// block, because the API refuses two user turns in a row.
func anthropicMessages(messages []Message) []anthropic.MessageParam {
	var out []anthropic.MessageParam
	var pending []anthropic.ContentBlockParamUnion // tool results awaiting their user turn
	flush := func() {
		if len(pending) > 0 {
			out = append(out, anthropic.NewUserMessage(pending...))
			pending = nil
		}
	}
	for _, m := range messages {
		switch m.Role {
		case RoleTool:
			pending = append(pending, anthropic.NewToolResultBlock(m.ToolCallID, m.Content, false))
		case RoleAssistant:
			flush()
			var blocks []anthropic.ContentBlockParamUnion
			if m.Content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(m.Content))
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, toolInput(tc.Arguments), tc.Name))
			}
			if len(blocks) > 0 {
				out = append(out, anthropic.NewAssistantMessage(blocks...))
			}
		default:
			flush()
			out = append(out, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		}
	}
	flush()
	return out
}

// toolInput carries the call's arguments through as the JSON the model wrote;
// the wire wants an object, so an empty or unparseable string travels as one.
func toolInput(arguments string) any {
	raw := json.RawMessage(arguments)
	if !json.Valid(raw) {
		return json.RawMessage(`{}`)
	}
	return raw
}

func anthropicResult(msg *anthropic.Message) *Result {
	res := &Result{Usage: &Usage{
		PromptTokens:     int(msg.Usage.InputTokens),
		CompletionTokens: int(msg.Usage.OutputTokens),
	}}
	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			res.Content += block.Text
		case "tool_use":
			res.ToolCalls = append(res.ToolCalls, ToolCall{
				ID: block.ID, Name: block.Name, Arguments: string(block.Input),
			})
		}
	}
	return res
}
