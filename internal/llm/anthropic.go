package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/egress"
	"github.com/geoah/substrate/internal/providersecret"
)

// The Anthropic-wire adapter. Three shapes differ from the OpenAI wire and
// they are the whole of the mapping: the system prompt is a top-level param and
// never a message; the API alternates roles, so consecutive same-side turns —
// tool answers, which are user-side content blocks here, and repeated user
// turns alike — fold into ONE turn carrying several blocks; and max_tokens is
// required on every request.
//
// The client is net/http against POST {base}/v1/messages, with the request
// body, the event-stream reader and the retry policy written out below.

// anthropicDefaultMaxTokens is what a request carries when the agent names no
// maxTokens: the wire REQUIRES the field, so there is no "unset" to send. 8192
// is inside every current model's output cap; an older model with a lower one
// needs params.maxTokens on the agent, since the wire rejects a ceiling above
// what the model allows.
const anthropicDefaultMaxTokens = 8192

// anthropicDefaultBaseURL is the endpoint of a row that names none, and
// anthropicVersion the API version every request pins.
const (
	anthropicDefaultBaseURL = "https://api.anthropic.com/"
	anthropicVersion        = "2023-06-01"
)

// The retry policy is the one anthropic-sdk-go v1.78.0 applied (its
// internal/requestconfig), kept so callers see the same retries without the
// SDK: an attempt that got no response, or got a 408, 409, 429 or 5xx, is
// retried up to twice, and an x-should-retry header overrides the status
// either way. The wait is retry-after-ms, else retry-after (seconds or an HTTP
// date), else 0.5 s doubling per retry up to 8 s, less up to a quarter of
// jitter. A retry-after is honored uncapped, as the SDK did; the caller's
// context bounds the wait.
const (
	anthropicMaxRetries  = 2
	anthropicBaseBackoff = 500 * time.Millisecond
	anthropicMaxBackoff  = 8 * time.Second
)

// A one-shot attempt gets ten minutes, and a one-shot request whose
// max_tokens could need longer is refused before it is sent. Both are the
// SDK's non-streaming rule (CalculateNonStreamingTimeout), which budgets
// 128,000 output tokens an hour and holds the models in
// anthropicOneShotModelMaxTokens lower still.
const (
	anthropicOneShotTimeout   = 10 * time.Minute
	anthropicOneShotMaxTokens = 128_000 * 10 / 60
)

// anthropicOneShotModelMaxTokens is the SDK's constant.ModelNonStreamingTokens:
// the model ids whose one-shot max_tokens it refused above 8192.
var anthropicOneShotModelMaxTokens = map[string]int{
	"claude-opus-4-20250514":                  8192,
	"claude-4-opus-20250514":                  8192,
	"claude-opus-4-0":                         8192,
	"anthropic.claude-opus-4-20250514-v1:0":   8192,
	"claude-opus-4@20250514":                  8192,
	"anthropic.claude-opus-4-1-20250805-v1:0": 8192,
	"claude-opus-4-1@20250805":                8192,
}

// What one call holds in memory. The base URL is repository-chosen, so the
// endpoint on the other end is not trusted to stop sending.
const (
	// anthropicMaxEvent bounds one event-stream line and one event's data.
	anthropicMaxEvent = 4 << 20
	// anthropicMaxMessage bounds a one-shot response body, and the text,
	// tool input and block starts a stream accumulates.
	anthropicMaxMessage = maxResponseBytes
	// anthropicMaxErrorBody is how much of a refusal's body, or of a stream's
	// error event, the call's error quotes.
	anthropicMaxErrorBody = 64 << 10
)

type anthropicClient struct {
	// doer sends every attempt: the egress-gated client under the row's extra
	// headers. headerDoer is the openai adapter's rule, so on both wires a row
	// header is set only where the adapter set none, and a row header named
	// X-Api-Key cannot replace the row's key.
	doer *headerDoer
	// endpoint is the Messages URL. endpointErr is why the row's base URL did
	// not parse; Complete returns it, because New is total.
	endpoint    string
	endpointErr error
	// apiKey is held only to keep it OUT of an error, the same reason the
	// openai adapter holds one: on a 401 the endpoint quotes the bearer it
	// refused, and scrubbed rebuilds any provider error without it.
	apiKey string
}

func newAnthropic(cfg Config) *anthropicClient {
	endpoint, err := anthropicEndpoint(cfg.BaseURL)
	return &anthropicClient{
		// Everything the client sends comes from the provider row, never from
		// the host's environment: a host token or header would ride along to
		// a repository-chosen base URL. That base URL is also why the dial is
		// confined to public destinations (issue #241).
		doer:        &headerDoer{inner: &http.Client{Transport: egress.Transport()}, headers: cfg.Headers},
		endpoint:    endpoint,
		endpointErr: err,
		apiKey:      cfg.APIKey,
	}
}

// anthropicEndpoint resolves the Messages path against a base URL the way the
// SDK's option.WithBaseURL did: a base with a path gains a trailing slash
// first, so https://openrouter.ai/api and https://openrouter.ai/api/ both post
// to https://openrouter.ai/api/v1/messages.
func anthropicEndpoint(base string) (string, error) {
	if base == "" {
		base = anthropicDefaultBaseURL
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("anthropic: base URL: %w", err)
	}
	if u.Path != "" && !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	endpoint, err := u.Parse("v1/messages")
	if err != nil {
		return "", fmt.Errorf("anthropic: base URL: %w", err)
	}
	return endpoint.String(), nil
}

// scrubbed rebuilds a provider error with the row's bearer taken back out,
// rebuilt rather than %w-wrapped so no unwrap can recover the key. A context
// overflow is classified on the scrubbed text (providerError).
func (c *anthropicClient) scrubbed(err error) error {
	return providerError(providersecret.Scrub(c.apiKey, err.Error()))
}

func (c *anthropicClient) Complete(ctx context.Context, req Request, onDelta func(Delta)) (*Result, error) {
	msg, err := c.complete(ctx, anthropicParams(req), onDelta)
	if err != nil {
		return nil, c.scrubbed(err)
	}
	return anthropicResult(msg), nil
}

func (c *anthropicClient) complete(ctx context.Context, params anthropicRequest, onDelta func(Delta)) (*anthropicMessage, error) {
	params.Stream = onDelta != nil
	if !params.Stream {
		ceiling := anthropicOneShotMaxTokens
		if limit, ok := anthropicOneShotModelMaxTokens[params.Model]; ok {
			ceiling = min(ceiling, limit)
		}
		if params.MaxTokens > ceiling {
			return nil, errors.New("anthropic: streaming is required for operations that may take longer than 10 minutes")
		}
	}
	body, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	res, payload, err := c.post(ctx, body, params.Stream)
	if err != nil {
		return nil, err
	}
	if !params.Stream {
		return anthropicDecode(res, payload)
	}
	defer func() { _ = res.Body.Close() }()
	return c.readStream(res, onDelta)
}

// anthropicRequest is the Messages request body, each key spelled as the wire
// spells it.
type anthropicRequest struct {
	Model        string                 `json:"model,omitempty"`
	MaxTokens    int                    `json:"max_tokens"`
	System       []anthropicText        `json:"system,omitempty"`
	Messages     []anthropicTurn        `json:"messages,omitempty"`
	Tools        []anthropicTool        `json:"tools,omitempty"`
	Temperature  *float64               `json:"temperature,omitempty"`
	OutputConfig *anthropicOutputConfig `json:"output_config,omitempty"`
	Thinking     map[string]any         `json:"thinking,omitempty"`
	Stream       bool                   `json:"stream,omitempty"`
}

type anthropicOutputConfig struct {
	Effort string `json:"effort"`
}

type anthropicTool struct {
	Name string `json:"name"`
	// Description is sent even when empty.
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// anthropicTurn is one wire message. Content holds anthropicText,
// anthropicToolUse and anthropicToolResult blocks.
type anthropicTurn struct {
	Role    string `json:"role"`
	Content []any  `json:"content"`
}

type anthropicText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicToolUse struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type anthropicToolResult struct {
	Type      string          `json:"type"`
	ToolUseID string          `json:"tool_use_id"`
	Content   []anthropicText `json:"content"`
	IsError   bool            `json:"is_error"`
}

func newAnthropicText(text string) anthropicText {
	return anthropicText{Type: "text", Text: text}
}

func anthropicParams(req Request) anthropicRequest {
	params := anthropicRequest{
		Model:     req.Model,
		MaxTokens: anthropicDefaultMaxTokens,
		Messages:  anthropicMessages(req.Messages),
	}
	if req.Params.MaxTokens > 0 {
		params.MaxTokens = req.Params.MaxTokens
	}
	if req.System != "" {
		params.System = []anthropicText{newAnthropicText(req.System)}
	}
	// Only when the caller asked for one: current models refuse a sampling
	// param they do not accept, and that refusal must reach the loop as an
	// error rather than be papered over with a default.
	if req.Params.Temperature != nil {
		t := float64(*req.Params.Temperature)
		params.Temperature = &t
	}
	// The neutral word lands on output_config here, not on a top-level key, and
	// this wire's set is its own: "none" and "minimal" are openai's spellings
	// and this endpoint refuses them, which is the refusal the loop should see.
	if req.Params.ReasoningEffort != "" {
		params.OutputConfig = &anthropicOutputConfig{Effort: req.Params.ReasoningEffort}
	}
	// Adaptive is the only thinking mode current models accept, and an older
	// model that takes only a budget refuses it: that refusal reaches the loop
	// the way a refused effort does.
	if req.Params.ReasoningSummary {
		params.Thinking = thinkingSummarized()
	}
	for _, t := range req.Tools {
		params.Tools = append(params.Tools, anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: anthropicSchema(t.Parameters),
		})
	}
	return params
}

// anthropicSchema hands the tool's JSON schema through as input_schema, whose
// type is always object on this wire. The key is sent even when the schema
// declares nothing but its type: the wire requires it, and a tool that takes
// no arguments is exactly {"type": "object"}.
func anthropicSchema(params any) map[string]any {
	out := map[string]any{"type": "object"}
	raw, ok := params.(map[string]any)
	if !ok {
		return out
	}
	for k, v := range raw {
		switch k {
		case "type":
			// The wire's input schema is always an object.
		case "properties":
			if v != nil {
				out[k] = v
			}
		case "required":
			var names []string
			req, _ := v.([]any)
			for _, rv := range req {
				if s, ok := rv.(string); ok {
					names = append(names, s)
				}
			}
			if len(names) > 0 {
				out[k] = names
			}
		default:
			out[k] = v
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
func anthropicMessages(messages []Message) []anthropicTurn {
	var out []anthropicTurn
	var side string // the side the pending blocks belong to; "" while none
	var pending []any
	flush := func() {
		if len(pending) == 0 {
			return
		}
		role := RoleUser
		if side == RoleAssistant {
			role = RoleAssistant
		}
		out = append(out, anthropicTurn{Role: role, Content: pending})
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
func anthropicBlocks(m Message) (string, []any) {
	switch m.Role {
	case RoleTool:
		return RoleUser, []any{anthropicToolResult{
			Type:      "tool_result",
			ToolUseID: m.ToolCallID,
			Content:   []anthropicText{newAnthropicText(m.Content)},
		}}
	case RoleAssistant:
		var blocks []any
		// The signed thinking goes back first and unchanged: the API refuses
		// a tool result whose assistant turn dropped or edited it.
		var reasoning []json.RawMessage
		if len(m.ReasoningBlocks) > 0 && json.Unmarshal(m.ReasoningBlocks, &reasoning) == nil {
			for _, b := range reasoning {
				blocks = append(blocks, b)
			}
		}
		if m.Content != "" {
			blocks = append(blocks, newAnthropicText(m.Content))
		}
		for _, tc := range m.ToolCalls {
			blocks = append(blocks, anthropicToolUse{
				Type: "tool_use", ID: tc.ID, Name: tc.Name, Input: toolInput(tc.Arguments),
			})
		}
		return RoleAssistant, blocks
	default:
		if m.Content == "" {
			return RoleUser, nil
		}
		return RoleUser, []any{newAnthropicText(m.Content)}
	}
}

// toolInput carries the call's arguments through as the JSON the model wrote.
// The Messages API requires tool_use.input to be an OBJECT, and validity is
// not enough: `null`, `[]` and `5` all parse, and all 400 the request that
// echoes the call back on the next turn. Anything that is not an object — an
// empty or unparseable string included — travels as one.
func toolInput(arguments string) json.RawMessage {
	var obj map[string]any
	if err := json.Unmarshal([]byte(arguments), &obj); err != nil || obj == nil {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(arguments)
}

// post sends one Messages request under the retry policy and returns the
// response that ended the call. A refusal comes back as an error quoting its
// status. A one-shot response's body is read inside its attempt, so a
// connection dropped mid-body is retried like a failed dial, and comes back
// as payload; a stream's body is the caller's to read and close.
func (c *anthropicClient) post(ctx context.Context, body []byte, stream bool) (*http.Response, []byte, error) {
	if c.endpointErr != nil {
		return nil, nil, c.endpointErr
	}
	for retry := 0; ; retry++ {
		last := retry == anthropicMaxRetries
		res, payload, again, err := c.attempt(ctx, body, stream, last)
		// Once the caller's context is done there is nothing left to retry,
		// and its error is the call's.
		if ctxErr := ctx.Err(); ctxErr != nil {
			if res != nil {
				_ = res.Body.Close()
			}
			return nil, nil, ctxErr
		}
		if last || !again {
			switch {
			case err != nil:
				return nil, nil, err
			case res.StatusCode >= http.StatusBadRequest:
				return nil, nil, c.statusError(res, payload)
			}
			return res, payload, nil
		}
		var header http.Header
		if res != nil {
			header = res.Header
			_ = res.Body.Close()
		}
		wait := time.NewTimer(anthropicRetryDelay(header, retry))
		select {
		case <-ctx.Done():
			wait.Stop()
			return nil, nil, ctx.Err()
		case <-wait.C:
		}
	}
}

// attempt sends the request once and reports whether the policy retries what
// came back. It reads the body only of a response that ends the call, and
// only where post must: every refusal, and a one-shot answer.
func (c *anthropicClient) attempt(ctx context.Context, body []byte, stream, last bool) (*http.Response, []byte, bool, error) {
	actx := ctx
	if !stream {
		if deadline, ok := ctx.Deadline(); !ok || time.Now().Add(anthropicOneShotTimeout).Before(deadline) {
			var cancel context.CancelFunc
			actx, cancel = context.WithTimeout(ctx, anthropicOneShotTimeout)
			defer cancel()
		}
	}
	req, err := http.NewRequestWithContext(actx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, nil, false, err
	}
	// The SDK also sent its User-Agent and X-Stainless-* platform and
	// retry-count headers. This client does not, so Go's default User-Agent
	// goes out unless a row header names one.
	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("Anthropic-Version", anthropicVersion)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := c.doer.Do(req)
	if err != nil {
		return nil, nil, true, err
	}
	again := anthropicShouldRetry(res)
	refused := res.StatusCode >= http.StatusBadRequest
	if (!last && again) || (stream && !refused) {
		return res, nil, again, nil
	}
	// A refusal is read the key's length past what its error quotes, so a key
	// that straddles the cut reaches statusError whole.
	limit := int64(anthropicMaxMessage) + 1
	if refused {
		limit = anthropicMaxErrorBody + int64(max(1, len(c.apiKey)))
	}
	payload, err := io.ReadAll(io.LimitReader(res.Body, limit))
	_ = res.Body.Close()
	switch {
	case err != nil && actx.Err() != nil && ctx.Err() == nil:
		// Only this attempt's ten minutes ran out: a failed attempt like a
		// failed dial, retried.
		return nil, nil, true, actx.Err()
	case err != nil:
		// A dropped body is a connection error whatever the status said, but
		// an x-should-retry: false still stands.
		return res, nil, res.Header.Get("X-Should-Retry") != "false", fmt.Errorf("error reading response body: %w", err)
	case !refused && len(payload) > anthropicMaxMessage:
		return res, nil, false, fmt.Errorf("anthropic: response body exceeds %d bytes", anthropicMaxMessage)
	}
	return res, payload, again, nil
}

// anthropicShouldRetry is the policy's verdict on a response.
func anthropicShouldRetry(res *http.Response) bool {
	switch res.Header.Get("X-Should-Retry") {
	case "true":
		return true
	case "false":
		return false
	}
	switch res.StatusCode {
	case http.StatusRequestTimeout, http.StatusConflict, http.StatusTooManyRequests:
		return true
	}
	return res.StatusCode >= http.StatusInternalServerError
}

// anthropicRetryDelay is how long to wait before retry number retry (0 for
// the first), given the headers of the response retried; none after a failed
// dial.
func anthropicRetryDelay(header http.Header, retry int) time.Duration {
	if d, ok := anthropicRetryAfter(header); ok {
		return max(0, d)
	}
	delay := anthropicBaseBackoff
	for range retry {
		delay = min(2*delay, anthropicMaxBackoff)
	}
	return delay - time.Duration(rand.Int64N(int64(delay/4))) //nolint:gosec // G404: retry jitter, not a secret
}

// anthropicRetryAfter reads the wait a response asks for: retry-after-ms,
// else retry-after in seconds or as an HTTP date.
func anthropicRetryAfter(header http.Header) (time.Duration, bool) {
	if v := header.Get("Retry-After-Ms"); v != "" {
		if ms, err := strconv.ParseFloat(v, 64); err == nil {
			return time.Duration(ms * float64(time.Millisecond)), true
		}
	}
	if v := header.Get("Retry-After"); v != "" {
		if s, err := strconv.ParseFloat(v, 64); err == nil {
			return time.Duration(s * float64(time.Second)), true
		}
		if t, err := time.Parse(time.RFC1123, v); err == nil {
			return time.Until(t), true
		}
	}
	return 0, false
}

// statusError is the error for a refused request or a stream's error event:
// method, URL, status, request ids, then the body the endpoint sent, cut to
// anthropicMaxErrorBody bytes without splitting a key scrubbed must remove.
// The status code must stay in the text, because providerError tells a rate
// limit from a context overflow by its 429.
func (c *anthropicClient) statusError(res *http.Response, body []byte) error {
	body = providersecret.Truncate(c.apiKey, body, anthropicMaxErrorBody)
	var msg strings.Builder
	fmt.Fprintf(&msg, "%s %q: %d %s", http.MethodPost, c.endpoint, res.StatusCode, http.StatusText(res.StatusCode))
	if id := res.Header.Get("Request-Id"); id != "" {
		fmt.Fprintf(&msg, " (Request-ID: %s)", id)
	}
	if id := res.Header.Get("Anthropic-Workspace-Id"); id != "" {
		fmt.Fprintf(&msg, " (Workspace-ID: %s)", id)
	}
	if body = bytes.TrimSpace(body); len(body) > 0 {
		msg.WriteByte(' ')
		msg.Write(body)
	}
	return errors.New(msg.String())
}

// anthropicMessage is the Messages response, one-shot or folded from a
// stream: only the keys the result reads.
type anthropicMessage struct {
	Content    []anthropicContent `json:"content"`
	StopReason string             `json:"stop_reason"`
	Usage      anthropicUsage     `json:"usage"`
}

// anthropicContent is one response block. A thinking block carries Thinking
// (empty when the model's display is omitted, the default on current models)
// and the Signature that would let it be echoed back; a redacted_thinking
// block carries only the opaque Data. No request here echoes either back, so
// both are kept for the result alone.
type anthropicContent struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	Signature string          `json:"signature"`
	Data      string          `json:"data"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
}

type anthropicUsage struct {
	InputTokens              anthropicCount `json:"input_tokens"`
	OutputTokens             anthropicCount `json:"output_tokens"`
	CacheReadInputTokens     anthropicCount `json:"cache_read_input_tokens"`
	CacheCreationInputTokens anthropicCount `json:"cache_creation_input_tokens"`
}

// anthropicCount is a token count read as leniently as the SDK read one: a
// number is truncated to an integer, a numeric string is read as its number,
// and anything else is 0. A gateway's odd usage must not discard an answer
// already paid for.
type anthropicCount int64

func (n *anthropicCount) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*n = 0
	switch v := v.(type) {
	case float64:
		*n = anthropicCount(v)
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && !math.IsInf(f, 0) && !math.IsNaN(f) {
			*n = anthropicCount(f)
		}
	}
	return nil
}

func anthropicDecode(res *http.Response, payload []byte) (*anthropicMessage, error) {
	contentType := res.Header.Get("Content-Type")
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if !strings.Contains(mediaType, "application/json") && !strings.HasSuffix(mediaType, "+json") {
		return nil, fmt.Errorf("anthropic: response content-type %q is not JSON", contentType)
	}
	var msg anthropicMessage
	if err := json.NewDecoder(bytes.NewReader(payload)).Decode(&msg); err != nil {
		return nil, fmt.Errorf("error parsing response json: %w", err)
	}
	return &msg, nil
}

// anthropicEvent is one stream event's data. Which keys it carries depends on
// its type; Delta serves content_block_delta and message_delta alike.
type anthropicEvent struct {
	Type         string           `json:"type"`
	Index        int              `json:"index"`
	Message      anthropicMessage `json:"message"`
	ContentBlock anthropicContent `json:"content_block"`
	Delta        struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		Signature   string `json:"signature"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	// Usage on message_delta is cumulative. output_tokens is always its final
	// word; the input counts overwrite only when sent and not null.
	Usage struct {
		OutputTokens             anthropicCount  `json:"output_tokens"`
		InputTokens              *anthropicCount `json:"input_tokens"`
		CacheReadInputTokens     *anthropicCount `json:"cache_read_input_tokens"`
		CacheCreationInputTokens *anthropicCount `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

// anthropicStream folds a stream's events into the message they describe.
// Each block's text grows in text, beside msg.Content, so a long reply is
// not copied once per delta; a thinking block's thinking grows there too.
type anthropicStream struct {
	msg  anthropicMessage
	text [][]byte
	// size is what the fold holds: every block start's event, every text
	// and tool-input fragment. It is bounded by anthropicMaxMessage.
	size int
}

// readStream reads the event stream of res. Events are dispatched on their
// SSE event name; ping and any name the adapter has no use for are skipped,
// and an error event ends the call with that event's data as its error.
func (c *anthropicClient) readStream(res *http.Response, onDelta func(Delta)) (*anthropicMessage, error) {
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 0, 64<<10), anthropicMaxEvent)
	var s anthropicStream
	var name string
	var data []byte
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			switch name {
			case "error":
				return nil, c.statusError(res, data)
			case "message_start", "message_delta", "message_stop",
				"content_block_start", "content_block_delta", "content_block_stop":
				if err := s.fold(data, onDelta); err != nil {
					return nil, err
				}
			}
			name, data = "", data[:0]
			continue
		}
		field, value, _ := bytes.Cut(line, []byte(":"))
		value = bytes.TrimPrefix(value, []byte(" "))
		switch string(field) {
		case "event":
			name = string(value)
		case "data":
			if len(data)+len(value) >= anthropicMaxEvent {
				return nil, fmt.Errorf("anthropic: a stream event exceeds %d bytes", anthropicMaxEvent)
			}
			data = append(append(data, value...), '\n')
		}
	}
	if err := sc.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return nil, fmt.Errorf("anthropic: a stream line exceeds %d bytes", anthropicMaxEvent)
		}
		return nil, err
	}
	for i := range s.msg.Content {
		if s.msg.Content[i].Type == "thinking" {
			s.msg.Content[i].Thinking = string(s.text[i])
			continue
		}
		s.msg.Content[i].Text = string(s.text[i])
	}
	return &s.msg, nil
}

// fold applies one event to the message. Blocks start in index order with
// no gaps, and a delta or a stop names a block already started; anything
// else is a broken stream and an error.
func (s *anthropicStream) fold(data []byte, onDelta func(Delta)) error {
	var ev anthropicEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return err
	}
	switch ev.Type {
	case "message_start":
		s.msg, s.text, s.size = ev.Message, nil, 0
		for _, block := range s.msg.Content {
			s.text = append(s.text, []byte(block.Text+block.Thinking))
		}
	case "message_delta":
		s.msg.StopReason = ev.Delta.StopReason
		u := &s.msg.Usage
		u.OutputTokens = ev.Usage.OutputTokens
		if ev.Usage.InputTokens != nil {
			u.InputTokens = *ev.Usage.InputTokens
		}
		if ev.Usage.CacheReadInputTokens != nil {
			u.CacheReadInputTokens = *ev.Usage.CacheReadInputTokens
		}
		if ev.Usage.CacheCreationInputTokens != nil {
			u.CacheCreationInputTokens = *ev.Usage.CacheCreationInputTokens
		}
	case "content_block_start":
		if ev.Index != len(s.msg.Content) {
			return fmt.Errorf("received event of type %s for content block at index %d, expected index %d",
				ev.Type, ev.Index, len(s.msg.Content))
		}
		if err := s.grow(len(data)); err != nil {
			return err
		}
		s.msg.Content = append(s.msg.Content, ev.ContentBlock)
		s.text = append(s.text, []byte(ev.ContentBlock.Text+ev.ContentBlock.Thinking))
	case "content_block_delta":
		if err := s.started(ev); err != nil {
			return err
		}
		block := &s.msg.Content[ev.Index]
		switch ev.Delta.Type {
		case "text_delta":
			if err := s.grow(len(ev.Delta.Text)); err != nil {
				return err
			}
			s.text[ev.Index] = append(s.text[ev.Index], ev.Delta.Text...)
			if ev.Delta.Text != "" {
				onDelta(Delta{Text: ev.Delta.Text})
			}
		case "thinking_delta":
			if err := s.grow(len(ev.Delta.Thinking)); err != nil {
				return err
			}
			s.text[ev.Index] = append(s.text[ev.Index], ev.Delta.Thinking...)
			if ev.Delta.Thinking != "" {
				onDelta(Delta{Reasoning: ev.Delta.Thinking})
			}
		case "signature_delta":
			if err := s.grow(len(ev.Delta.Signature)); err != nil {
				return err
			}
			block.Signature += ev.Delta.Signature
		case "input_json_delta":
			if ev.Delta.PartialJSON == "" {
				break
			}
			if err := s.grow(len(ev.Delta.PartialJSON)); err != nil {
				return err
			}
			// A tool_use block starts with input {}, or null or none from a
			// gateway, which the first fragment replaces rather than extends.
			switch string(block.Input) {
			case "{}", "null":
				block.Input = json.RawMessage(ev.Delta.PartialJSON)
			default:
				block.Input = append(block.Input, ev.Delta.PartialJSON...)
			}
		}
	case "content_block_stop":
		if err := s.started(ev); err != nil {
			return err
		}
		settleInput(&s.msg.Content[ev.Index])
	case "message_stop":
		for i := range s.msg.Content {
			settleInput(&s.msg.Content[i])
		}
	}
	return nil
}

func (s *anthropicStream) started(ev anthropicEvent) error {
	if ev.Index < 0 || ev.Index >= len(s.msg.Content) {
		return fmt.Errorf("received event of type %s for content block at index %d but there are only %d content blocks",
			ev.Type, ev.Index, len(s.msg.Content))
	}
	return nil
}

func (s *anthropicStream) grow(n int) error {
	s.size += n
	if s.size > anthropicMaxMessage {
		return fmt.Errorf("anthropic: a streamed message exceeds %d bytes", anthropicMaxMessage)
	}
	return nil
}

// settleInput empties a tool input a cut-off call left as unparseable JSON,
// once its block or the message has stopped.
func settleInput(block *anthropicContent) {
	if len(block.Input) > 0 && !json.Valid(block.Input) {
		block.Input = json.RawMessage(`{}`)
	}
}

func anthropicResult(msg *anthropicMessage) *Result {
	// This wire reports cache reads and writes beside input_tokens, never
	// inside it, and the window holds all three. A negative count reads as
	// none, as on the openai wire: the thread's tallies refuse a value below 0.
	input := max(0, int(msg.Usage.InputTokens))
	read := max(0, int(msg.Usage.CacheReadInputTokens))
	write := max(0, int(msg.Usage.CacheCreationInputTokens))
	res := &Result{
		Usage: &Usage{
			PromptTokens:     input + read + write,
			CompletionTokens: int(msg.Usage.OutputTokens),
			CacheReadTokens:  read,
			CacheWriteTokens: write,
		},
		Stop: anthropicStop(msg.StopReason),
	}
	var content, reasoning strings.Builder
	var blocks []any
	for _, block := range msg.Content {
		switch block.Type {
		case "redacted_thinking":
			blocks = append(blocks, map[string]any{"type": block.Type, "data": block.Data})
		case "thinking":
			reasoning.WriteString(block.Thinking)
			blocks = append(blocks, map[string]any{"type": block.Type, "thinking": block.Thinking, "signature": block.Signature})
		case "text":
			content.WriteString(block.Text)
		case "tool_use":
			res.ToolCalls = append(res.ToolCalls, ToolCall{
				ID: block.ID, Name: block.Name, Arguments: string(block.Input),
			})
		}
	}
	res.Content = content.String()
	res.Reasoning = reasoning.String()
	if len(blocks) > 0 {
		res.ReasoningBlocks, _ = json.Marshal(blocks)
	}
	return res
}

// anthropicStop maps the wire's stop_reason onto the neutral set. An empty
// reason stays empty: a stream cut before its message_delta said nothing.
func anthropicStop(reason string) string {
	switch reason {
	case "":
		return ""
	case "end_turn", "stop_sequence":
		return StopEnd
	case "max_tokens":
		return StopLength
	case "tool_use":
		return StopToolCall
	default:
		return StopOther
	}
}
