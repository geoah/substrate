package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	// cacheKey and cacheBreakpoints are the row's Config.CacheKey and
	// Config.CacheBreakpoints.
	cacheKey, cacheBreakpoints bool
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
	oc.HTTPClient = &bodyDoer{inner: oc.HTTPClient}
	return &openaiClient{
		client: openai.NewClientWithConfig(oc), apiKey: cfg.APIKey,
		cacheKey: cfg.CacheKey, cacheBreakpoints: cfg.CacheBreakpoints,
	}
}

// scrubbed rebuilds a provider error with the row's bearer taken back out. The
// error is REBUILT, not %w-wrapped: wrapping would carry the original, key and
// all, to anything downstream that unwraps it, and the leak this closes ends
// in a stored record and a host log. A context overflow is classified on the
// scrubbed text (providerError).
func (c *openaiClient) scrubbed(err error) error {
	return providerError(providersecret.Scrub(c.apiKey, err.Error()))
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

// maxStreamLine bounds one event-stream line, the anthropic adapter's bound
// on one event.
const maxStreamLine = 4 << 20

// maxToolCalls bounds the tool calls one streamed reply may open: each is
// accumulated by its index, and the index is the endpoint's to choose.
const maxToolCalls = 512

// lineLimitReader fails a read once a line runs past limit bytes without a
// newline, so a reader that buffers whole lines cannot be made to buffer
// without end.
type lineLimitReader struct {
	io.ReadCloser
	limit int
	run   int
}

func (r *lineLimitReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	for _, b := range p[:n] {
		if b == '\n' {
			r.run = 0
			continue
		}
		r.run++
		if r.run > r.limit {
			return 0, fmt.Errorf("openai: a stream line exceeds %d bytes", r.limit)
		}
	}
	return n, err
}

// bodyPatch is what one call adds to the JSON the SDK writes and reads past
// what the SDK parses, carried from Complete to bodyDoer in the call's
// context. The SDK's chat types have no key for a thinking request or for
// `thinking_blocks`, and LiteLLM speaks both.
type bodyPatch struct {
	// extra are top-level request keys; a key the SDK wrote stays as written.
	extra map[string]any
	// messages are keys added to the request message at each index, and
	// tools the keys added to the tool definition at each index.
	messages map[int]map[string]json.RawMessage
	tools    map[int]map[string]json.RawMessage
	// response, when non-nil, receives a one-shot reply's body, read whole
	// up to maxResponseBytes; a stream's body is left to the SDK.
	response *[]byte
}

type bodyPatchKey struct{}

// bodyDoer applies the context's bodyPatch to one request and its reply.
type bodyDoer struct {
	inner openai.HTTPDoer
}

func (d *bodyDoer) Do(req *http.Request) (*http.Response, error) {
	patch, _ := req.Context().Value(bodyPatchKey{}).(*bodyPatch)
	if patch == nil {
		patch = &bodyPatch{}
	}
	if (len(patch.extra) > 0 || len(patch.messages) > 0 || len(patch.tools) > 0) && req.Body != nil {
		if err := patchRequest(req, patch); err != nil {
			return nil, err
		}
	}
	res, err := d.inner.Do(req)
	if err != nil {
		return res, err
	}
	if strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		// The SDK reads each event line whole before anything here can
		// count it, so the line itself is bounded underneath it.
		res.Body = &lineLimitReader{ReadCloser: res.Body, limit: maxStreamLine}
		return res, nil
	}
	if patch.response == nil || res.StatusCode >= http.StatusBadRequest {
		return res, nil
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes+1))
	_ = res.Body.Close()
	if err != nil {
		return nil, err
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("openai: response body exceeds %d bytes", maxResponseBytes)
	}
	*patch.response = body
	res.Body = io.NopCloser(bytes.NewReader(body))
	return res, nil
}

func patchRequest(req *http.Request, patch *bodyPatch) error {
	raw, err := io.ReadAll(req.Body)
	_ = req.Body.Close()
	if err != nil {
		return err
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		return fmt.Errorf("openai: request body: %w", err)
	}
	for k, v := range patch.extra {
		if _, set := body[k]; set {
			continue
		}
		if body[k], err = json.Marshal(v); err != nil {
			return err
		}
	}
	for _, list := range []struct {
		key   string
		patch map[int]map[string]json.RawMessage
	}{{"messages", patch.messages}, {"tools", patch.tools}} {
		if len(list.patch) == 0 {
			continue
		}
		var items []map[string]json.RawMessage
		if err := json.Unmarshal(body[list.key], &items); err != nil {
			return fmt.Errorf("openai: request %s: %w", list.key, err)
		}
		for i, keys := range list.patch {
			if i < 0 || i >= len(items) {
				continue
			}
			for k, v := range keys {
				items[i][k] = v
			}
		}
		if body[list.key], err = json.Marshal(items); err != nil {
			return err
		}
	}
	if raw, err = json.Marshal(body); err != nil {
		return err
	}
	req.Body = io.NopCloser(bytes.NewReader(raw))
	req.ContentLength = int64(len(raw))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil }
	return nil
}

// thinkingBlocks reads the `thinking_blocks` LiteLLM puts beside an
// assistant message or a stream delta.
type thinkingBlocks struct {
	Choices []struct {
		Message struct {
			ThinkingBlocks []json.RawMessage `json:"thinking_blocks"`
		} `json:"message"`
		Delta struct {
			ThinkingBlocks []json.RawMessage `json:"thinking_blocks"`
		} `json:"delta"`
	} `json:"choices"`
}

// signedBlocks keeps the blocks that can be echoed: a thinking block with its
// signature, a redacted one with its data. A stream sends each thinking
// block in unsigned pieces first and then once whole with the signature, so
// the pieces drop here.
func signedBlocks(blocks []json.RawMessage) []json.RawMessage {
	var out []json.RawMessage
	for _, b := range blocks {
		var head struct {
			Type      string `json:"type"`
			Signature string `json:"signature"`
			Data      string `json:"data"`
		}
		if json.Unmarshal(b, &head) != nil {
			continue
		}
		if (head.Type == "thinking" && head.Signature != "") || (head.Type == "redacted_thinking" && head.Data != "") {
			out = append(out, b)
		}
	}
	return out
}

func rawArray(blocks []json.RawMessage) json.RawMessage {
	if len(blocks) == 0 {
		return nil
	}
	raw, _ := json.Marshal(blocks)
	return raw
}

func (c *openaiClient) Complete(ctx context.Context, req Request, onDelta func(Delta)) (*Result, error) {
	messages, echoed := openaiMessages(req)
	oreq := openai.ChatCompletionRequest{Model: req.Model, Messages: messages}
	patch := &bodyPatch{messages: echoed}
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
	// Sent only when the row or the agent named one: a gateway that predates
	// the key ignores it, but an endpoint that knows it treats an explicit
	// value as a decision, and "none" is the only way to put tools on a
	// gpt-5.6 model here — that family reasons by default and refuses
	// function tools for any effort but none on /v1/chat/completions.
	oreq.ReasoningEffort = req.Params.ReasoningEffort
	if req.Params.ReasoningSummary {
		patch.extra = map[string]any{"thinking": thinkingSummarized()}
	}
	for _, t := range req.Tools {
		oreq.Tools = append(oreq.Tools, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name: t.Name, Description: t.Description, Parameters: t.Parameters,
			},
		})
	}
	c.patchCache(patch, req, len(messages), len(oreq.Tools))
	if onDelta == nil {
		var body []byte
		patch.response = &body
		res, err := c.oneShot(context.WithValue(ctx, bodyPatchKey{}, patch), oreq, &body)
		if res != nil {
			var tb thinkingBlocks
			if json.Unmarshal(body, &tb) == nil && len(tb.Choices) > 0 {
				res.ReasoningBlocks = rawArray(signedBlocks(tb.Choices[0].Message.ThinkingBlocks))
			}
		}
		return res, err
	}
	return c.stream(context.WithValue(ctx, bodyPatchKey{}, patch), oreq, onDelta)
}

// patchCache adds what the row opted into for a request that asks for a
// cache. prompt_cache_key is OpenAI's routing hint: requests carrying one key
// land where that prefix is already cached. The breakpoints are Anthropic's
// cache_control at the keys LiteLLM reads: on the last tool, and on the
// system message and the last message as a whole, which LiteLLM moves onto
// the message's content (litellm_core_utils/prompt_templates/factory.py,
// llms/anthropic/chat/transformation.py). Nothing here reaches a row that did
// not opt in, so an endpoint that refuses unknown keys never sees one.
func (c *openaiClient) patchCache(patch *bodyPatch, req Request, messages, tools int) {
	if req.Cache == nil {
		return
	}
	if c.cacheKey && req.Cache.Key != "" {
		if patch.extra == nil {
			patch.extra = map[string]any{}
		}
		patch.extra["prompt_cache_key"] = req.Cache.Key
	}
	if !c.cacheBreakpoints {
		return
	}
	cc, _ := json.Marshal(newCacheControl(req.Cache))
	if tools > 0 {
		patch.tools = map[int]map[string]json.RawMessage{tools - 1: {"cache_control": cc}}
	}
	mark := func(i int) {
		if patch.messages == nil {
			patch.messages = map[int]map[string]json.RawMessage{}
		}
		if patch.messages[i] == nil {
			patch.messages[i] = map[string]json.RawMessage{}
		}
		patch.messages[i]["cache_control"] = cc
	}
	if req.System != "" {
		mark(0)
	}
	if messages > 0 {
		mark(messages - 1)
	}
}

// oneShot reads body, the reply the bodyDoer captured, for the cache counts
// the SDK's Usage leaves out (cache_creation_tokens and the two top-level
// fields; it parses cached_tokens, but the raw reply carries all four).
func (c *openaiClient) oneShot(ctx context.Context, oreq openai.ChatCompletionRequest, body *[]byte) (*Result, error) {
	resp, err := c.client.CreateChatCompletion(ctx, oreq)
	if err != nil {
		return nil, c.scrubbed(err)
	}
	usage := openaiUsage(resp.Usage.PromptTokens, resp.Usage.CompletionTokens, *body)
	if len(resp.Choices) == 0 {
		return &Result{Usage: usage}, errors.New("empty response: no choices")
	}
	msg := resp.Choices[0].Message
	return &Result{
		Content: msg.Content, ToolCalls: neutralToolCalls(msg.ToolCalls), Usage: usage,
		Stop: openaiStop(resp.Choices[0].FinishReason), Reasoning: msg.ReasoningContent,
	}, nil
}

func (c *openaiClient) stream(ctx context.Context, oreq openai.ChatCompletionRequest, onDelta func(Delta)) (*Result, error) {
	// The usage tally only rides a stream when it is asked for, and the loop's
	// cost accounting depends on it.
	oreq.StreamOptions = &openai.StreamOptions{IncludeUsage: true}
	stream, err := c.client.CreateChatCompletionStream(ctx, oreq)
	if err != nil {
		return nil, c.scrubbed(err)
	}
	defer func() { _ = stream.Close() }()
	var content, reasoning strings.Builder
	var blocks []json.RawMessage
	size := 0
	grow := func(n int) error {
		size += n
		if size > maxResponseBytes {
			return fmt.Errorf("openai: a streamed response exceeds %d bytes", maxResponseBytes)
		}
		return nil
	}
	var usage *Usage
	// The finish reason rides one chunk near the end, and the usage chunk
	// after it carries none, so the last one seen is the turn's.
	var finish openai.FinishReason
	acc := &toolCallAccumulator{byIndex: map[int]*openai.ToolCall{}}
	for {
		raw, err := stream.RecvRaw()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, c.scrubbed(err)
		}
		var chunk openai.ChatCompletionStreamResponse
		if err := json.Unmarshal(raw, &chunk); err != nil {
			return nil, c.scrubbed(err)
		}
		var tb thinkingBlocks
		if json.Unmarshal(raw, &tb) == nil && len(tb.Choices) > 0 {
			signed := signedBlocks(tb.Choices[0].Delta.ThinkingBlocks)
			for _, b := range signed {
				if err := grow(len(b)); err != nil {
					return nil, err
				}
			}
			blocks = append(blocks, signed...)
		}
		if chunk.Usage != nil {
			usage = openaiUsage(chunk.Usage.PromptTokens, chunk.Usage.CompletionTokens, raw)
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		if fr := chunk.Choices[0].FinishReason; fr != "" {
			finish = fr
		}
		delta := chunk.Choices[0].Delta
		if err := grow(len(delta.ReasoningContent) + len(delta.Content)); err != nil {
			return nil, err
		}
		for _, tc := range delta.ToolCalls {
			if err := grow(len(tc.ID) + len(tc.Function.Name) + len(tc.Function.Arguments)); err != nil {
				return nil, err
			}
		}
		if err := acc.admit(delta.ToolCalls); err != nil {
			return nil, err
		}
		// reasoning_content is the thinking text LiteLLM and the other
		// gateways stream ahead of the content; thinking_blocks beside it
		// repeat the text with the signature, kept above for the echo.
		if delta.ReasoningContent != "" {
			reasoning.WriteString(delta.ReasoningContent)
			onDelta(Delta{Reasoning: delta.ReasoningContent})
		}
		if delta.Content != "" {
			content.WriteString(delta.Content)
			onDelta(Delta{Text: delta.Content})
		}
		acc.absorb(delta.ToolCalls)
	}
	return &Result{
		Content: content.String(), ToolCalls: neutralToolCalls(acc.finalize()), Usage: usage,
		Stop: openaiStop(finish), Reasoning: reasoning.String(),
		ReasoningBlocks: rawArray(blocks),
	}, nil
}

// openaiCacheUsage is a reply's cache counts, read from the raw reply. The
// SDK parses cached_tokens but not cache_creation_tokens or the two
// top-level fields, so all four are read here in one place. LiteLLM writes each cache count twice: inside prompt_tokens_details
// (cached_tokens, cache_creation_tokens) and at the top level
// (cache_read_input_tokens, cache_creation_input_tokens), the same two
// numbers (calculate_usage in litellm/llms/anthropic/chat/transformation.py).
// OpenAI writes cached_tokens alone and reports no cache write.
type openaiCacheUsage struct {
	Usage *struct {
		Details *struct {
			CachedTokens        *anthropicCount `json:"cached_tokens"`
			CacheCreationTokens *anthropicCount `json:"cache_creation_tokens"`
		} `json:"prompt_tokens_details"`
		CacheReadInputTokens     *anthropicCount `json:"cache_read_input_tokens"`
		CacheCreationInputTokens *anthropicCount `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

// openaiUsage builds the neutral tally from the SDK's two counts and the raw
// reply (one-shot body or the stream's usage chunk). This wire's
// prompt_tokens already includes the cached and the written tokens on both
// LiteLLM and OpenAI, so the cache counts are parts of it, never added to
// it. The details form wins over the top-level form and the two are never
// summed. A reply that does not parse leaves both cache counts at 0, so an
// odd gateway prices its input at the full rate and never loses the answer.
// A reply whose read plus write exceeds prompt_tokens cannot be counting them
// inside it, so there the counts are added to prompt_tokens, as on the
// anthropic wire.
func openaiUsage(prompt, completion int, raw []byte) *Usage {
	u := &Usage{PromptTokens: prompt, CompletionTokens: completion}
	var doc openaiCacheUsage
	if json.Unmarshal(raw, &doc) != nil || doc.Usage == nil {
		return u
	}
	pick := func(detail, top *anthropicCount) int {
		if detail != nil && *detail > 0 {
			return int(*detail)
		}
		if top != nil && *top > 0 {
			return int(*top)
		}
		return 0
	}
	var dRead, dWrite *anthropicCount
	if d := doc.Usage.Details; d != nil {
		dRead, dWrite = d.CachedTokens, d.CacheCreationTokens
	}
	read := pick(dRead, doc.Usage.CacheReadInputTokens)
	write := pick(dWrite, doc.Usage.CacheCreationInputTokens)
	if read+write > prompt {
		u.PromptTokens = max(0, prompt) + read + write
	}
	u.CacheReadTokens, u.CacheWriteTokens = read, write
	return u
}

// openaiStop maps the wire's finish_reason onto the neutral set. An empty
// reason stays empty: a gateway that sends none said nothing.
func openaiStop(reason openai.FinishReason) string {
	switch reason {
	case "":
		return ""
	case openai.FinishReasonStop:
		return StopEnd
	case openai.FinishReasonLength:
		return StopLength
	case openai.FinishReasonToolCalls, openai.FinishReasonFunctionCall:
		return StopToolCall
	default:
		return StopOther
	}
}

// openaiMessages maps the neutral turns onto the wire's roles 1:1. The system
// prompt leads, because on this wire that is what a system prompt is.
//
// The second result is the `thinking_blocks` to add to each assistant message
// that carries them, by index: LiteLLM passes them back to an Anthropic model,
// which refuses a tool result whose assistant turn lost its signed thinking.
func openaiMessages(req Request) ([]openai.ChatCompletionMessage, map[int]map[string]json.RawMessage) {
	out := make([]openai.ChatCompletionMessage, 0, len(req.Messages)+1)
	var echoed map[int]map[string]json.RawMessage
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
			if len(m.ReasoningBlocks) > 0 {
				if echoed == nil {
					echoed = map[int]map[string]json.RawMessage{}
				}
				echoed[len(out)] = map[string]json.RawMessage{"thinking_blocks": m.ReasoningBlocks}
			}
			out = append(out, om)
		case RoleTool:
			out = append(out, openai.ChatCompletionMessage{
				Role: openai.ChatMessageRoleTool, ToolCallID: m.ToolCallID,
				Name: m.ToolName, Content: m.Content,
			})
		default:
			out = append(out, openaiUserMessage(m))
		}
	}
	return out, echoed
}

// openaiUserMessage is a user turn: a plain string, or two text parts when
// the turn carries a context, so the context stays a block of its own.
func openaiUserMessage(m Message) openai.ChatCompletionMessage {
	switch {
	case m.Context == "":
		return openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: m.Content}
	case m.Content == "":
		return openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: m.Context}
	}
	return openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, MultiContent: []openai.ChatMessagePart{
		{Type: openai.ChatMessagePartTypeText, Text: m.Content},
		{Type: openai.ChatMessagePartTypeText, Text: m.Context},
	}}
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

// admit refuses deltas that would open more than maxToolCalls calls.
func (a *toolCallAccumulator) admit(deltas []openai.ToolCall) error {
	opened := 0
	for _, d := range deltas {
		i := 0
		if d.Index != nil {
			i = *d.Index
		}
		if _, ok := a.byIndex[i]; !ok {
			opened++
		}
	}
	if len(a.byIndex)+opened > maxToolCalls {
		return fmt.Errorf("openai: a streamed reply opens more than %d tool calls", maxToolCalls)
	}
	return nil
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
