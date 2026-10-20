package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The Anthropic-wire adapter against a server that speaks that wire. What is
// checked here is exactly what differs from the OpenAI wire: the top-level
// system param, tool_result blocks grouped into one user turn, the required
// max_tokens, and a temperature that is sent only when asked for. The
// transport is this adapter's own too, so its endpoint, headers, retry policy
// and stream reader are checked at the end of the file.

type anthropicServer struct {
	srv  *httptest.Server
	last map[string]any
	key  string
	hdr  http.Header
}

func newAnthropicServer(t *testing.T, respond func(w http.ResponseWriter, req map[string]any)) *anthropicServer {
	t.Helper()
	s := &anthropicServer{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/messages") {
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
			return
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.last, s.key, s.hdr = req, r.Header.Get("X-Api-Key"), r.Header.Clone()
		respond(w, req)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *anthropicServer) client(t *testing.T, headers map[string]string) Client {
	t.Helper()
	c, err := New(WireAnthropic, Config{BaseURL: s.srv.URL, APIKey: "sk-test", Headers: headers})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return c
}

func TestAnthropicOneShotSystemMaxTokensAndUsage(t *testing.T) {
	srv := newAnthropicServer(t, func(w http.ResponseWriter, _ map[string]any) {
		jsonBody(w, map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-5",
			"content": []any{map[string]any{"type": "text", "text": "hello back"}},
			"usage":   map[string]any{"input_tokens": 11, "output_tokens": 7},
		})
	})
	res, err := srv.client(t, nil).Complete(context.Background(), Request{
		Model: "claude-opus-5", System: "you are a test",
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
		Tools:    []Tool{{Name: "peek", Description: "look", Parameters: map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}}, "required": []any{"id"}}}},
	}, nil)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if res.Content != "hello back" {
		t.Fatalf("content = %q", res.Content)
	}
	if res.Usage == nil || res.Usage.PromptTokens != 11 || res.Usage.CompletionTokens != 7 {
		t.Fatalf("usage = %+v", res.Usage)
	}
	if srv.key != "sk-test" {
		t.Fatalf("x-api-key = %q", srv.key)
	}
	// The system prompt is a top-level param, never a message.
	system, _ := srv.last["system"].([]any)
	if len(system) != 1 {
		t.Fatalf("system = %v", srv.last["system"])
	}
	if block, _ := system[0].(map[string]any); block["text"] != "you are a test" {
		t.Fatalf("system block = %v", system[0])
	}
	messages, _ := srv.last["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("messages = %v", srv.last["messages"])
	}
	// max_tokens is required by the wire, so an unset one still travels.
	if srv.last["max_tokens"] != float64(anthropicDefaultMaxTokens) {
		t.Fatalf("max_tokens = %v", srv.last["max_tokens"])
	}
	// A sampling param the caller did not ask for is never sent.
	if _, ok := srv.last["temperature"]; ok {
		t.Fatalf("temperature was sent unasked: %v", srv.last["temperature"])
	}
	tools, _ := srv.last["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %v", srv.last["tools"])
	}
	tool, _ := tools[0].(map[string]any)
	schema, _ := tool["input_schema"].(map[string]any)
	if tool["name"] != "peek" || schema["type"] != "object" || schema["properties"] == nil {
		t.Fatalf("tool = %v", tool)
	}
}

func TestAnthropicSendsAskedForParams(t *testing.T) {
	srv := newAnthropicServer(t, func(w http.ResponseWriter, _ map[string]any) {
		jsonBody(w, map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "model": "m",
			"content": []any{}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
		})
	})
	temp := float32(0.5)
	_, err := srv.client(t, map[string]string{"X-Title": "substrate"}).Complete(context.Background(), Request{
		Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Params: Params{Temperature: &temp, MaxTokens: 256},
	}, nil)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if srv.last["temperature"] != 0.5 || srv.last["max_tokens"] != float64(256) {
		t.Fatalf("params = %v / %v", srv.last["temperature"], srv.last["max_tokens"])
	}
	if srv.hdr.Get("X-Title") != "substrate" {
		t.Fatalf("headers = %v", srv.hdr)
	}
}

// The neutral reasoningEffort lands on output_config here, not on a top-level
// key: same declared word, each wire's own spelling.
func TestAnthropicSendsReasoningEffortOnOutputConfig(t *testing.T) {
	srv := newAnthropicServer(t, func(w http.ResponseWriter, _ map[string]any) {
		jsonBody(w, map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "model": "m",
			"content": []any{}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
		})
	})
	client := srv.client(t, nil)
	req := Request{
		Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Params: Params{ReasoningEffort: "high"},
	}
	if _, err := client.Complete(context.Background(), req, nil); err != nil {
		t.Fatalf("complete: %v", err)
	}
	cfg, _ := srv.last["output_config"].(map[string]any)
	if cfg["effort"] != "high" {
		t.Fatalf("output_config = %v", srv.last["output_config"])
	}
	req.Params = Params{}
	if _, err := client.Complete(context.Background(), req, nil); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, sent := srv.last["output_config"]; sent {
		t.Fatalf("output_config was sent unasked: %v", srv.last["output_config"])
	}
}

func TestAnthropicGroupsConsecutiveToolResultsIntoOneUserTurn(t *testing.T) {
	srv := newAnthropicServer(t, func(w http.ResponseWriter, _ map[string]any) {
		jsonBody(w, map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "model": "m",
			"content": []any{map[string]any{
				"type": "tool_use", "id": "toolu_2", "name": "peek", "input": map[string]any{"id": "y"},
			}},
			"usage": map[string]any{"input_tokens": 4, "output_tokens": 6},
		})
	})
	res, err := srv.client(t, nil).Complete(context.Background(), Request{
		Model: "m",
		Messages: []Message{
			{Role: RoleUser, Content: "go"},
			{Role: RoleAssistant, Content: "working", ToolCalls: []ToolCall{
				{ID: "toolu_0", Name: "peek", Arguments: `{"id":"a"}`},
				{ID: "toolu_1", Name: "peek", Arguments: `{"id":"b"}`},
			}},
			{Role: RoleTool, ToolCallID: "toolu_0", ToolName: "peek", Content: "first"},
			{Role: RoleTool, ToolCallID: "toolu_1", ToolName: "peek", Content: "second"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].ID != "toolu_2" || res.ToolCalls[0].Arguments != `{"id":"y"}` {
		t.Fatalf("tool calls = %+v", res.ToolCalls)
	}
	messages, _ := srv.last["messages"].([]any)
	// user / assistant / ONE user turn carrying both tool results — the wire
	// alternates roles, so two user turns in a row would be refused.
	if len(messages) != 3 {
		t.Fatalf("messages = %s", mustJSON(srv.last["messages"]))
	}
	assistant, _ := messages[1].(map[string]any)
	assistantBlocks, _ := assistant["content"].([]any)
	if assistant["role"] != "assistant" || len(assistantBlocks) != 3 {
		t.Fatalf("assistant turn = %s", mustJSON(assistant))
	}
	toolUse, _ := assistantBlocks[1].(map[string]any)
	if toolUse["type"] != "tool_use" || toolUse["id"] != "toolu_0" {
		t.Fatalf("tool_use block = %s", mustJSON(toolUse))
	}
	if input, _ := toolUse["input"].(map[string]any); input["id"] != "a" {
		t.Fatalf("tool_use input = %s", mustJSON(toolUse["input"]))
	}
	results, _ := messages[2].(map[string]any)
	blocks, _ := results["content"].([]any)
	if results["role"] != "user" || len(blocks) != 2 {
		t.Fatalf("tool result turn = %s", mustJSON(results))
	}
	for i, want := range []string{"toolu_0", "toolu_1"} {
		block, _ := blocks[i].(map[string]any)
		if block["type"] != "tool_result" || block["tool_use_id"] != want {
			t.Fatalf("block %d = %s", i, mustJSON(block))
		}
	}
}

// The wire alternates roles, and a CONTINUED thread's replayed history
// legally holds two user turns in a row: loadHistory drops assistant turns
// that carried tool calls, and a thread that settled on an error stored no
// assistant reply at all, so the new user message lands straight after an
// older one. Such a continuation must not 400 forever.
func TestAnthropicFoldsConsecutiveSameRoleTurns(t *testing.T) {
	for _, tc := range []struct {
		name     string
		messages []Message
		// wantRoles is the wire's messages, in order.
		wantRoles []string
		// wantBlocks is each wire message's content-block count.
		wantBlocks []int
	}{
		{
			name: "user then user",
			messages: []Message{
				{Role: RoleUser, Content: "first"},
				{Role: RoleUser, Content: "second"},
			},
			wantRoles:  []string{"user"},
			wantBlocks: []int{2},
		},
		{
			name: "user, an orphan tool answer, then user",
			messages: []Message{
				{Role: RoleUser, Content: "first"},
				{Role: RoleTool, ToolCallID: "toolu_0", ToolName: "peek", Content: "result"},
				{Role: RoleUser, Content: "second"},
			},
			wantRoles:  []string{"user"},
			wantBlocks: []int{3},
		},
		{
			name: "an assistant turn still separates them",
			messages: []Message{
				{Role: RoleUser, Content: "first"},
				{Role: RoleAssistant, Content: "reply"},
				{Role: RoleUser, Content: "second"},
				{Role: RoleUser, Content: "third"},
			},
			wantRoles:  []string{"user", "assistant", "user"},
			wantBlocks: []int{1, 1, 2},
		},
		{
			name: "an empty turn never splits a run",
			messages: []Message{
				{Role: RoleUser, Content: "first"},
				{Role: RoleAssistant, Content: ""},
				{Role: RoleUser, Content: "second"},
			},
			wantRoles:  []string{"user"},
			wantBlocks: []int{2},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newAnthropicServer(t, func(w http.ResponseWriter, _ map[string]any) {
				jsonBody(w, map[string]any{
					"id": "msg_1", "type": "message", "role": "assistant", "model": "m",
					"content": []any{}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
				})
			})
			if _, err := srv.client(t, nil).Complete(context.Background(), Request{
				Model: "m", Messages: tc.messages,
			}, nil); err != nil {
				t.Fatalf("complete: %v", err)
			}
			messages, _ := srv.last["messages"].([]any)
			if len(messages) != len(tc.wantRoles) {
				t.Fatalf("messages = %s", mustJSON(srv.last["messages"]))
			}
			for i, wantRole := range tc.wantRoles {
				m, _ := messages[i].(map[string]any)
				blocks, _ := m["content"].([]any)
				if m["role"] != wantRole || len(blocks) != tc.wantBlocks[i] {
					t.Fatalf("message %d = %s, want role %q with %d blocks", i, mustJSON(m), wantRole, tc.wantBlocks[i])
				}
			}
		})
	}
}

// tool_use.input must be an OBJECT: `null`, `[]` and `5` are all valid JSON
// that 400s the turn echoing the call back, so anything else travels as `{}`.
func TestAnthropicToolInputIsAlwaysAnObject(t *testing.T) {
	for _, tc := range []struct{ arguments, want string }{
		{"null", "{}"},
		{"", "{}"},
		{"[]", "{}"},
		{"5", "{}"},
		{`"a string"`, "{}"},
		{"{not json", "{}"},
		{`{"id":"x"}`, `{"id":"x"}`},
	} {
		t.Run(tc.arguments, func(t *testing.T) {
			got := mustJSON(toolInput(tc.arguments))
			if got != tc.want {
				t.Fatalf("toolInput(%q) = %s, want %s", tc.arguments, got, tc.want)
			}
		})
	}
}

// The same, on the wire: a null-argument call echoed back carries an object.
func TestAnthropicSendsNullToolArgumentsAsAnObject(t *testing.T) {
	srv := newAnthropicServer(t, func(w http.ResponseWriter, _ map[string]any) {
		jsonBody(w, map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "model": "m",
			"content": []any{}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
		})
	})
	if _, err := srv.client(t, nil).Complete(context.Background(), Request{
		Model: "m",
		Messages: []Message{
			{Role: RoleUser, Content: "go"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "toolu_0", Name: "peek", Arguments: "null"}}},
			{Role: RoleTool, ToolCallID: "toolu_0", ToolName: "peek", Content: "done"},
		},
	}, nil); err != nil {
		t.Fatalf("complete: %v", err)
	}
	messages, _ := srv.last["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("messages = %s", mustJSON(srv.last["messages"]))
	}
	assistant, _ := messages[1].(map[string]any)
	blocks, _ := assistant["content"].([]any)
	block, _ := blocks[0].(map[string]any)
	if _, ok := block["input"].(map[string]any); !ok {
		t.Fatalf("tool_use input is not an object: %s", mustJSON(block))
	}
}

func TestAnthropicStreamsDeltasAndAccumulatesToolInput(t *testing.T) {
	events := []string{
		`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[],"usage":{"input_tokens":5,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"he"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"llo"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_0","name":"peek","input":{}}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"id\":"}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"x\"}"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":9}}`,
		`{"type":"message_stop"}`,
	}
	srv := newAnthropicServer(t, func(w http.ResponseWriter, _ map[string]any) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, e := range events {
			var typed struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal([]byte(e), &typed)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typed.Type, e)
			flusher.Flush()
		}
	})
	var deltas []string
	res, err := srv.client(t, nil).Complete(context.Background(), Request{
		Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, func(d Delta) { deltas = append(deltas, d.Text) })
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if strings.Join(deltas, "") != "hello" || res.Content != "hello" {
		t.Fatalf("deltas = %v, content = %q", deltas, res.Content)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].Name != "peek" || res.ToolCalls[0].Arguments != `{"id":"x"}` {
		t.Fatalf("tool calls = %+v", res.ToolCalls)
	}
	if res.Usage == nil || res.Usage.PromptTokens != 5 || res.Usage.CompletionTokens != 9 {
		t.Fatalf("usage = %+v", res.Usage)
	}
	// The accumulated message's stop_reason is the streamed turn's.
	if res.Stop != StopToolCall {
		t.Fatalf("stop = %q", res.Stop)
	}
	if srv.last["stream"] != true {
		t.Fatalf("stream = %v", srv.last["stream"])
	}
}

func TestAnthropicSurfacesAnAPIError(t *testing.T) {
	srv := newAnthropicServer(t, func(w http.ResponseWriter, _ map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"type":"error","error":{"type":"invalid_request_error","message":"temperature is not supported"}}`)
	})
	temp := float32(0.5)
	_, err := srv.client(t, nil).Complete(context.Background(), Request{
		Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Params: Params{Temperature: &temp},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "temperature is not supported") {
		t.Fatalf("err = %v", err)
	}
}

// The wire's stop_reason maps onto the neutral set, and the prompt adds the
// cache reads and writes this wire reports beside input_tokens, each also
// carried apart so the cost stamp prices it at its own rate.
func TestAnthropicMapsStopReasonAndCacheCounts(t *testing.T) {
	for reason, want := range map[string]string{
		"end_turn":      StopEnd,
		"stop_sequence": StopEnd,
		"max_tokens":    StopLength,
		"tool_use":      StopToolCall,
		"refusal":       StopOther,
		"pause_turn":    StopOther,
	} {
		t.Run(reason, func(t *testing.T) {
			srv := newAnthropicServer(t, func(w http.ResponseWriter, _ map[string]any) {
				jsonBody(w, map[string]any{
					"id": "msg_1", "type": "message", "role": "assistant", "model": "m",
					"content": []any{map[string]any{"type": "text", "text": "x"}}, "stop_reason": reason,
					"usage": map[string]any{
						"input_tokens": 10, "output_tokens": 2,
						"cache_read_input_tokens": 300, "cache_creation_input_tokens": 40,
					},
				})
			})
			res, err := srv.client(t, nil).Complete(context.Background(), Request{
				Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}},
			}, nil)
			if err != nil {
				t.Fatalf("complete: %v", err)
			}
			if res.Stop != want {
				t.Fatalf("stop = %q, want %q", res.Stop, want)
			}
			if *res.Usage != (Usage{PromptTokens: 350, CompletionTokens: 2, CacheReadTokens: 300, CacheWriteTokens: 40}) {
				t.Fatalf("usage = %+v", res.Usage)
			}
		})
	}
}

// A context overflow is the one provider failure the loop can act on, so it
// wraps ErrContextTooLong; a rate-limit refusal that shares its words does
// not, and neither does any other error. The key is scrubbed first.
func TestAnthropicClassifiesAContextOverflow(t *testing.T) {
	for name, tc := range map[string]struct {
		status   int
		message  string
		overflow bool
	}{
		"prompt too long":   {400, "prompt is too long: 210000 tokens > 200000 maximum", true},
		"a count with 429":  {400, "prompt is too long: 214290 tokens > 200000 maximum", true},
		"request too large": {413, "Request too large for this model", true},
		"rate limit":        {429, "Number of request tokens has exceeded your rate limit: too many tokens", false},
		"unrelated":         {400, "temperature is not supported", false},
	} {
		t.Run(name, func(t *testing.T) {
			srv := newAnthropicServer(t, func(w http.ResponseWriter, _ map[string]any) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprintf(w, `{"type":"error","error":{"type":"invalid_request_error","message":%q}}`,
					tc.message+" (key sk-test)")
			})
			_, err := srv.client(t, nil).Complete(context.Background(), Request{
				Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}},
			}, nil)
			if err == nil {
				t.Fatal("no error")
			}
			if got := errors.Is(err, ErrContextTooLong); got != tc.overflow {
				t.Fatalf("errors.Is(ErrContextTooLong) = %v, want %v: %v", got, tc.overflow, err)
			}
			if strings.Contains(err.Error(), "sk-test") {
				t.Fatalf("the error carries the key: %v", err)
			}
		})
	}
}

func mustJSON(v any) string {
	out, _ := json.Marshal(v)
	return string(out)
}

// The host's Anthropic environment never reaches a request: a bearer token
// there would go to whatever base URL the provider row names, and a header
// there would ride on every repository's calls. Not parallel: t.Setenv.
func TestAnthropicIgnoresTheHostEnvironment(t *testing.T) {
	ok := func(w http.ResponseWriter, _ map[string]any) {
		jsonBody(w, map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "model": "m",
			"content": []any{map[string]any{"type": "text", "text": "ok"}},
			"usage":   map[string]any{"input_tokens": 1, "output_tokens": 1},
		})
	}
	call := func(t *testing.T) http.Header {
		t.Helper()
		s := newAnthropicServer(t, ok)
		if _, err := s.client(t, nil).Complete(context.Background(), Request{
			Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}},
		}, nil); err != nil {
			t.Fatalf("complete: %v", err)
		}
		if s.key != "sk-test" {
			t.Fatalf("X-Api-Key = %q, want the row's key", s.key)
		}
		return s.hdr
	}
	t.Run("auth token", func(t *testing.T) {
		t.Setenv("ANTHROPIC_API_KEY", "")
		t.Setenv("ANTHROPIC_AUTH_TOKEN", "host-bearer")
		if got := call(t).Get("Authorization"); got != "" {
			t.Fatalf("Authorization = %q, want none: the host's token reached the request", got)
		}
	})
	t.Run("custom headers", func(t *testing.T) {
		t.Setenv("ANTHROPIC_API_KEY", "")
		t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
		t.Setenv("ANTHROPIC_PROFILE", "")
		t.Setenv("ANTHROPIC_CONFIG_DIR", t.TempDir())
		t.Setenv("ANTHROPIC_CUSTOM_HEADERS", "X-Host-Header: leaked")
		if got := call(t).Get("X-Host-Header"); got != "" {
			t.Fatalf("X-Host-Header = %q, want none: the host's header reached the request", got)
		}
	})
}

// anthropicMessageBody is a minimal one-shot answer.
func anthropicMessageBody(w http.ResponseWriter) {
	jsonBody(w, map[string]any{
		"id": "msg_1", "type": "message", "role": "assistant", "model": "m",
		"content": []any{map[string]any{"type": "text", "text": "ok"}}, "stop_reason": "end_turn",
		"usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
	})
}

// anthropicSSE writes data lines as one event-stream response, each event
// named for its type.
func anthropicSSE(w http.ResponseWriter, events ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = fmt.Fprint(w, anthropicEventStream(events...))
}

// anthropicEventStream is the event-stream text anthropicSSE writes.
func anthropicEventStream(events ...string) string {
	var out strings.Builder
	for _, e := range events {
		var typed struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal([]byte(e), &typed)
		fmt.Fprintf(&out, "event: %s\ndata: %s\n\n", typed.Type, e)
	}
	return out.String()
}

// anthropicStreamOK is a whole streamed turn answering "ok".
var anthropicStreamOK = []string{
	`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}`,
	`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
	`{"type":"content_block_stop","index":0}`,
	`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
	`{"type":"message_stop"}`,
}

func anthropicClientAt(t *testing.T, baseURL string) Client {
	t.Helper()
	c, err := New(WireAnthropic, Config{BaseURL: baseURL, APIKey: "sk-test"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return c
}

func anthropicHi() Request {
	return Request{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}}
}

// One rule on both wires: the adapter's own auth and protocol headers win,
// and a row header lands only where the adapter set none.
func TestAnthropicRowHeadersNeverReplaceTheClientsOwn(t *testing.T) {
	srv := newAnthropicServer(t, func(w http.ResponseWriter, _ map[string]any) { anthropicMessageBody(w) })
	_, err := srv.client(t, map[string]string{
		"X-Api-Key": "row-key", "anthropic-version": "1999-01-01", "X-Title": "substrate",
	}).Complete(context.Background(), anthropicHi(), nil)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if srv.key != "sk-test" {
		t.Fatalf("X-Api-Key = %q: a row header replaced the row's key", srv.key)
	}
	if got := srv.hdr.Get("Anthropic-Version"); got != anthropicVersion {
		t.Fatalf("anthropic-version = %q, want %q", got, anthropicVersion)
	}
	if got := srv.hdr.Get("X-Title"); got != "substrate" {
		t.Fatalf("X-Title = %q: a row header the adapter does not set was dropped", got)
	}
}

// input_schema is required on this wire, so a tool that takes no arguments
// still carries {"type": "object"}.
func TestAnthropicSendsAnInputSchemaForEveryTool(t *testing.T) {
	srv := newAnthropicServer(t, func(w http.ResponseWriter, _ map[string]any) { anthropicMessageBody(w) })
	req := anthropicHi()
	req.Tools = []Tool{
		{Name: "now", Description: "the time", Parameters: map[string]any{"type": "object"}},
		{Name: "bare", Description: "no schema at all"},
	}
	if _, err := srv.client(t, nil).Complete(context.Background(), req, nil); err != nil {
		t.Fatalf("complete: %v", err)
	}
	tools, _ := srv.last["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("tools = %s", mustJSON(srv.last["tools"]))
	}
	for _, tool := range tools {
		if got := mustJSON(tool.(map[string]any)["input_schema"]); got != `{"type":"object"}` {
			t.Fatalf("input_schema = %s, want {\"type\":\"object\"}: %s", got, mustJSON(tool))
		}
	}
}

// The Messages path resolves against the row's base URL: a path on the base
// is kept whether or not it ends in a slash, and no base is Anthropic's own.
func TestAnthropicPostsUnderTheBaseURLPath(t *testing.T) {
	for base, want := range map[string]string{
		"":                           "https://api.anthropic.com/v1/messages",
		"https://api.anthropic.com":  "https://api.anthropic.com/v1/messages",
		"https://api.anthropic.com/": "https://api.anthropic.com/v1/messages",
		"https://openrouter.ai/api":  "https://openrouter.ai/api/v1/messages",
		"https://openrouter.ai/api/": "https://openrouter.ai/api/v1/messages",
	} {
		if got, err := anthropicEndpoint(base); err != nil || got != want {
			t.Errorf("anthropicEndpoint(%q) = %q, %v, want %q", base, got, err, want)
		}
	}
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.Method + " " + r.URL.Path
		anthropicMessageBody(w)
	}))
	defer srv.Close()
	for _, base := range []string{srv.URL + "/api", srv.URL + "/api/"} {
		if _, err := anthropicClientAt(t, base).Complete(context.Background(), anthropicHi(), nil); err != nil {
			t.Fatalf("complete against %s: %v", base, err)
		}
		if path != "POST /api/v1/messages" {
			t.Fatalf("base %s posted to %q, want POST /api/v1/messages", base, path)
		}
	}
}

// The retry policy anthropic-sdk-go applied, kept by hand: a 408, 409, 429 or
// 5xx is retried twice, anything else is not, and x-should-retry overrides
// the status either way. Every refusal here asks for no wait.
func TestAnthropicRetriesTheStatusesTheSDKRetried(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		shouldRetry string
		attempts    int32
	}{
		{"408", http.StatusRequestTimeout, "", 3},
		{"409", http.StatusConflict, "", 3},
		{"429", http.StatusTooManyRequests, "", 3},
		{"500", http.StatusInternalServerError, "", 3},
		{"529", 529, "", 3},
		{"400", http.StatusBadRequest, "", 1},
		{"401", http.StatusUnauthorized, "", 1},
		{"413", http.StatusRequestEntityTooLarge, "", 1},
		{"x-should-retry false on a 503", http.StatusServiceUnavailable, "false", 1},
		{"x-should-retry true on a 400", http.StatusBadRequest, "true", 3},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s stream=%v", tc.name, stream), func(t *testing.T) {
				var attempts atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					attempts.Add(1)
					w.Header().Set("Retry-After-Ms", "0")
					if tc.shouldRetry != "" {
						w.Header().Set("X-Should-Retry", tc.shouldRetry)
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(tc.status)
					_, _ = fmt.Fprint(w, `{"type":"error","error":{"type":"api_error","message":"refused"}}`)
				}))
				defer srv.Close()
				var onDelta func(Delta)
				if stream {
					onDelta = func(Delta) {}
				}
				_, err := anthropicClientAt(t, srv.URL).Complete(context.Background(), anthropicHi(), onDelta)
				if err == nil || !strings.Contains(err.Error(), fmt.Sprintf(": %d ", tc.status)) {
					t.Fatalf("err = %v, want it to quote the %d", err, tc.status)
				}
				if got := attempts.Load(); got != tc.attempts {
					t.Fatalf("%d attempts, want %d", got, tc.attempts)
				}
			})
		}
	}
}

// A failed dial and a one-shot body dropped mid-read are connection errors
// and retried; the third attempt answers.
func TestAnthropicRetriesAConnectionErrorAndADroppedBody(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch attempts.Add(1) {
		case 1:
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
		case 2:
			conn, buf, err := w.(http.Hijacker).Hijack()
			if err != nil {
				return
			}
			_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nRetry-After-Ms: 0\r\nContent-Length: 1000\r\n\r\n{\"id\":")
			_ = buf.Flush()
			_ = conn.Close()
		default:
			anthropicMessageBody(w)
		}
	}))
	defer srv.Close()
	res, err := anthropicClientAt(t, srv.URL).Complete(context.Background(), anthropicHi(), nil)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if res.Content != "ok" || attempts.Load() != 3 {
		t.Fatalf("content %q after %d attempts, want ok after 3", res.Content, attempts.Load())
	}
}

// A refused stream is retried like a refused one-shot; a stream that started
// is not.
func TestAnthropicRetriesARefusedStream(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After-Ms", "0")
			w.WriteHeader(529)
			return
		}
		anthropicSSE(w, anthropicStreamOK...)
	}))
	defer srv.Close()
	var deltas strings.Builder
	res, err := anthropicClientAt(t, srv.URL).Complete(context.Background(), anthropicHi(), func(d Delta) { deltas.WriteString(d.Text) })
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if res.Content != "ok" || deltas.String() != "ok" || attempts.Load() != 2 {
		t.Fatalf("content %q, deltas %q after %d attempts", res.Content, deltas.String(), attempts.Load())
	}
}

// The wait: retry-after-ms, else retry-after in seconds or as a date, else
// 0.5 s doubling per retry up to 8 s, less up to a quarter of jitter.
func TestAnthropicRetryDelay(t *testing.T) {
	with := func(kv ...string) http.Header {
		h := http.Header{}
		for i := 0; i < len(kv); i += 2 {
			h.Set(kv[i], kv[i+1])
		}
		return h
	}
	for name, tc := range map[string]struct {
		header http.Header
		want   time.Duration
	}{
		"retry-after-ms":                {with("Retry-After-Ms", "1500"), 1500 * time.Millisecond},
		"retry-after seconds":           {with("Retry-After", "3"), 3 * time.Second},
		"retry-after-ms wins":           {with("Retry-After-Ms", "20", "Retry-After", "3"), 20 * time.Millisecond},
		"an unparseable ms falls back":  {with("Retry-After-Ms", "soon", "Retry-After", "2"), 2 * time.Second},
		"a date already past waits 0":   {with("Retry-After", "Mon, 02 Jan 2006 15:04:05 GMT"), 0},
		"a negative wait is no wait":    {with("Retry-After-Ms", "-5"), 0},
		"an uncapped retry-after stays": {with("Retry-After", "120"), 120 * time.Second},
	} {
		if got := anthropicRetryDelay(tc.header, 0); got != tc.want {
			t.Errorf("%s: delay = %v, want %v", name, got, tc.want)
		}
	}
	future := time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)
	if got := anthropicRetryDelay(with("Retry-After", future), 0); got < 59*time.Minute || got > time.Hour {
		t.Errorf("retry-after %s: delay = %v, want about an hour", future, got)
	}
	for retry, ceiling := range []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second} {
		for _, header := range []http.Header{nil, with()} {
			got := anthropicRetryDelay(header, retry)
			if got <= ceiling*3/4 || got > ceiling {
				t.Errorf("retry %d: delay = %v, want in (%v, %v]", retry, got, ceiling*3/4, ceiling)
			}
		}
	}
}

// Pings, unknown events and nameless data are skipped; a tool input cut off
// mid-JSON settles to {} when its block stops.
func TestAnthropicStreamSkipsWhatItDoesNotUse(t *testing.T) {
	srv := newAnthropicServer(t, func(w http.ResponseWriter, _ map[string]any) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w,
			"event: message_start\ndata: "+anthropicStreamOK[0]+"\n\n",
			"event: ping\ndata: {\"type\": \"ping\"}\n\n",
			": a comment\n\n",
			"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
			"event: something_new\ndata: {\"type\":\"something_new\"}\n\n",
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_0\",\"name\":\"peek\",\"input\":{}}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"id\\\":\"}}\n\n",
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"max_tokens\"},\"usage\":{\"output_tokens\":4,\"cache_read_input_tokens\":30}}\n\n",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		)
	})
	res, err := srv.client(t, nil).Complete(context.Background(), anthropicHi(), func(Delta) {})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].Arguments != "{}" {
		t.Fatalf("tool calls = %+v, want one with arguments {}", res.ToolCalls)
	}
	if res.Stop != StopLength || res.Usage.CompletionTokens != 4 || res.Usage.PromptTokens != 31 || res.Usage.CacheReadTokens != 30 {
		t.Fatalf("stop %q, usage %+v", res.Stop, res.Usage)
	}
}

// An error event mid-stream ends the call with the event's data, after the
// text already delivered.
func TestAnthropicStreamSurfacesAnErrorEvent(t *testing.T) {
	srv := newAnthropicServer(t, func(w http.ResponseWriter, _ map[string]any) {
		anthropicSSE(w, append(anthropicStreamOK[:3:3],
			`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded (key sk-test)"}}`)...)
	})
	var deltas strings.Builder
	_, err := srv.client(t, nil).Complete(context.Background(), anthropicHi(), func(d Delta) { deltas.WriteString(d.Text) })
	if err == nil || !strings.Contains(err.Error(), "overloaded_error") || strings.Contains(err.Error(), "sk-test") {
		t.Fatalf("err = %v, want the scrubbed error event", err)
	}
	if deltas.String() != "ok" {
		t.Fatalf("deltas = %q", deltas.String())
	}
}

// The endpoint is repository-chosen, so what one call buffers is bounded: one
// line, one event however many lines carry it, the streamed message's text,
// tool input and block starts, and a one-shot body. Each case trips its own
// bound and no other.
func TestAnthropicBoundsWhatItBuffers(t *testing.T) {
	huge := strings.Repeat("x", anthropicMaxEvent)
	fragment := strings.Repeat("y", 1<<20)
	fragments := func(event string) []string {
		var out []string
		for range anthropicMaxMessage/len(fragment) + 1 {
			out = append(out, event)
		}
		return out
	}
	for name, tc := range map[string]struct {
		stream  bool
		respond func(w http.ResponseWriter)
		want    string
	}{
		"one line": {true, func(w http.ResponseWriter) {
			anthropicSSE(w, anthropicStreamOK[0], anthropicStreamOK[1],
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"`+huge+`"}}`)
		}, "a stream line exceeds"},
		"one event over many lines": {true, func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, anthropicEventStream(anthropicStreamOK[0]), "event: content_block_delta\n")
			for range anthropicMaxEvent/len(fragment) + 1 {
				_, _ = fmt.Fprint(w, "data: ", fragment, "\n")
			}
			_, _ = fmt.Fprint(w, "\n")
		}, "a stream event exceeds"},
		"the streamed text": {true, func(w http.ResponseWriter) {
			anthropicSSE(w, append([]string{anthropicStreamOK[0], anthropicStreamOK[1]}, fragments(
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"`+fragment+`"}}`)...)...)
		}, "a streamed message exceeds"},
		"the streamed tool input": {true, func(w http.ResponseWriter) {
			anthropicSSE(w, append([]string{
				anthropicStreamOK[0],
				`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_0","name":"peek","input":{}}}`,
			}, fragments(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"`+fragment+`"}}`)...)...)
		}, "a streamed message exceeds"},
		"the streamed block starts": {true, func(w http.ResponseWriter) {
			text := strings.Repeat("z", 1<<10)
			events := []string{anthropicStreamOK[0]}
			for i := range anthropicMaxMessage/len(text) + 1 {
				events = append(events, fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":{"type":"text","text":"%s"}}`, i, text))
			}
			anthropicSSE(w, events...)
		}, "a streamed message exceeds"},
		"a one-shot body": {false, func(w http.ResponseWriter) {
			jsonBody(w, map[string]any{
				"type": "message", "content": []any{map[string]any{"type": "text", "text": huge + huge + huge + huge + huge}},
			})
		}, "response body exceeds"},
	} {
		t.Run(name, func(t *testing.T) {
			srv := newAnthropicServer(t, func(w http.ResponseWriter, _ map[string]any) { tc.respond(w) })
			var onDelta func(Delta)
			if tc.stream {
				onDelta = func(Delta) {}
			}
			_, err := srv.client(t, nil).Complete(context.Background(), anthropicHi(), onDelta)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

// A one-shot request whose max_tokens could take past the ten minutes an
// attempt gets is refused before it is sent, and so is one above the lower
// ceiling the SDK held some models to; streamed, each goes.
func TestAnthropicRefusesALongOneShotUnsent(t *testing.T) {
	for _, tc := range []struct {
		model     string
		maxTokens int
		refused   bool
	}{
		{"m", anthropicOneShotMaxTokens, false},
		{"m", anthropicOneShotMaxTokens + 1, true},
		{"claude-opus-4-0", 8192, false},
		{"claude-opus-4-0", 8193, true},
		{"claude-opus-4-1@20250805", 8193, true},
		{"claude-opus-4-1", 8193, false},
	} {
		t.Run(fmt.Sprintf("%s %d", tc.model, tc.maxTokens), func(t *testing.T) {
			srv := newAnthropicServer(t, func(w http.ResponseWriter, req map[string]any) {
				if req["stream"] == true {
					anthropicSSE(w, anthropicStreamOK...)
					return
				}
				anthropicMessageBody(w)
			})
			req := anthropicHi()
			req.Model, req.Params.MaxTokens = tc.model, tc.maxTokens
			_, err := srv.client(t, nil).Complete(context.Background(), req, nil)
			switch {
			case tc.refused && (err == nil || !strings.Contains(err.Error(), "streaming is required")):
				t.Fatalf("err = %v, want the one-shot refused", err)
			case tc.refused && srv.last != nil:
				t.Fatalf("the refused one-shot reached the endpoint: %s", mustJSON(srv.last))
			case !tc.refused && err != nil:
				t.Fatalf("one-shot: %v", err)
			}
			if _, err := srv.client(t, nil).Complete(context.Background(), req, func(Delta) {}); err != nil {
				t.Fatalf("streamed: %v", err)
			}
		})
	}
}

// The whole request body, against what anthropic-sdk-go v1.78.0 sent for the
// same requests (captured from the SDK adapter): every field the adapter
// maps, optional fields left out when unset, a zero temperature still sent.
// The one difference is the input_schema of a tool whose schema declares
// nothing past its type, which the SDK dropped although the wire requires it.
func TestAnthropicSendsTheSDKsRequestBody(t *testing.T) {
	half, zero := float32(0.5), float32(0)
	rich := Request{
		Model: "claude-x", System: "be terse",
		Messages: []Message{
			{Role: RoleUser, Content: "go"},
			{Role: RoleAssistant, Content: "working", ToolCalls: []ToolCall{
				{ID: "toolu_0", Name: "peek", Arguments: `{"id":"a","n":1.5,"deep":{"x":[1,2]}}`},
				{ID: "toolu_1", Name: "peek", Arguments: "null"},
			}},
			{Role: RoleTool, ToolCallID: "toolu_0", ToolName: "peek", Content: "first <b>&</b>"},
			{Role: RoleTool, ToolCallID: "toolu_1", ToolName: "peek", Content: ""},
			{Role: RoleUser, Content: "and then"},
			{Role: RoleAssistant, Content: ""},
			{Role: RoleUser, Content: "again"},
		},
		Tools: []Tool{
			{Name: "peek", Description: "look", Parameters: map[string]any{
				"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}},
				"required": []any{"id", 5}, "additionalProperties": false,
				"$defs": map[string]any{"a": map[string]any{"type": "integer"}},
			}},
			{Name: "bare", Description: ""},
			{Name: "empty", Description: "e", Parameters: map[string]any{}},
		},
		Params: Params{Temperature: &half, MaxTokens: 256, ReasoningEffort: "high"},
	}
	for name, tc := range map[string]struct {
		req  Request
		want string
	}{
		"every field": {rich, `{
			"model": "claude-x", "max_tokens": 256,
			"system": [{"type": "text", "text": "be terse"}],
			"messages": [
				{"role": "user", "content": [{"type": "text", "text": "go"}]},
				{"role": "assistant", "content": [
					{"type": "text", "text": "working"},
					{"type": "tool_use", "id": "toolu_0", "name": "peek", "input": {"id": "a", "n": 1.5, "deep": {"x": [1, 2]}}},
					{"type": "tool_use", "id": "toolu_1", "name": "peek", "input": {}}
				]},
				{"role": "user", "content": [
					{"type": "tool_result", "tool_use_id": "toolu_0", "content": [{"type": "text", "text": "first <b>&</b>"}], "is_error": false},
					{"type": "tool_result", "tool_use_id": "toolu_1", "content": [{"type": "text", "text": ""}], "is_error": false},
					{"type": "text", "text": "and then"},
					{"type": "text", "text": "again"}
				]}
			],
			"tools": [
				{"name": "peek", "description": "look", "input_schema": {
					"type": "object", "properties": {"id": {"type": "string"}}, "required": ["id"],
					"additionalProperties": false, "$defs": {"a": {"type": "integer"}}
				}},
				{"name": "bare", "description": "", "input_schema": {"type": "object"}},
				{"name": "empty", "description": "e", "input_schema": {"type": "object"}}
			],
			"temperature": 0.5,
			"output_config": {"effort": "high"}
		}`},
		"nothing optional": {anthropicHi(), `{
			"model": "m", "max_tokens": 8192,
			"messages": [{"role": "user", "content": [{"type": "text", "text": "hi"}]}]
		}`},
		"a zero temperature": {Request{Model: "m", Messages: anthropicHi().Messages, Params: Params{Temperature: &zero}}, `{
			"model": "m", "max_tokens": 8192, "temperature": 0,
			"messages": [{"role": "user", "content": [{"type": "text", "text": "hi"}]}]
		}`},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s stream=%v", name, stream), func(t *testing.T) {
				srv := newAnthropicServer(t, func(w http.ResponseWriter, req map[string]any) {
					if req["stream"] == true {
						anthropicSSE(w, anthropicStreamOK...)
						return
					}
					anthropicMessageBody(w)
				})
				var onDelta func(Delta)
				var want map[string]any
				if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
					t.Fatalf("want: %v", err)
				}
				if stream {
					onDelta, want["stream"] = func(Delta) {}, true
				}
				if _, err := srv.client(t, nil).Complete(context.Background(), tc.req, onDelta); err != nil {
					t.Fatalf("complete: %v", err)
				}
				if got, want := mustJSON(srv.last), mustJSON(want); got != want {
					t.Fatalf("body =\n%s\nwant\n%s", got, want)
				}
			})
		}
	}
}

// anthropicInputDelta is one input_json_delta event for block index.
func anthropicInputDelta(index int, partial string) string {
	return fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"input_json_delta","partial_json":%s}}`,
		index, mustJSON(partial))
}

// Tool input accumulates per block index, so two tool blocks open at once,
// their fragments interleaved, each assemble their own arguments; a delta for
// a block that never started is a broken stream.
func TestAnthropicStreamAccumulatesInterleavedToolBlocks(t *testing.T) {
	start := func(index int, id string) string {
		return fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":{"type":"tool_use","id":%q,"name":"peek","input":{}}}`, index, id)
	}
	stop := func(index int) string { return fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, index) }
	srv := newAnthropicServer(t, func(w http.ResponseWriter, _ map[string]any) {
		anthropicSSE(w, anthropicStreamOK[0], start(0, "toolu_a"), start(1, "toolu_b"),
			anthropicInputDelta(0, `{"id":`), anthropicInputDelta(1, `{"id":`),
			anthropicInputDelta(1, `"b"}`), anthropicInputDelta(0, `"a"}`),
			stop(1), stop(0),
			`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":9}}`,
			`{"type":"message_stop"}`)
	})
	res, err := srv.client(t, nil).Complete(context.Background(), anthropicHi(), func(Delta) {})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if got := mustJSON(res.ToolCalls); got != `[{"ID":"toolu_a","Name":"peek","Arguments":"{\"id\":\"a\"}"},{"ID":"toolu_b","Name":"peek","Arguments":"{\"id\":\"b\"}"}]` {
		t.Fatalf("tool calls = %s", got)
	}

	broken := newAnthropicServer(t, func(w http.ResponseWriter, _ map[string]any) {
		anthropicSSE(w, anthropicStreamOK[0], start(0, "toolu_a"), anthropicInputDelta(1, `{}`))
	})
	if _, err := broken.client(t, nil).Complete(context.Background(), anthropicHi(), func(Delta) {}); err == nil ||
		!strings.Contains(err.Error(), "content block at index 1") {
		t.Fatalf("err = %v, want the unstarted block named", err)
	}
}

// Anthropic starts a tool_use block with input {}; a gateway may send null or
// leave the key out. The first fragment replaces any of the three.
func TestAnthropicStreamAssemblesToolInputFromAnyStart(t *testing.T) {
	for name, input := range map[string]string{"{}": `"input":{},`, "null": `"input":null,`, "absent": ``} {
		t.Run(name, func(t *testing.T) {
			srv := newAnthropicServer(t, func(w http.ResponseWriter, _ map[string]any) {
				anthropicSSE(w, anthropicStreamOK[0],
					`{"type":"content_block_start","index":0,"content_block":{`+input+`"type":"tool_use","id":"toolu_0","name":"peek"}}`,
					anthropicInputDelta(0, `{"id": `), anthropicInputDelta(0, `"a"}`),
					`{"type":"content_block_stop","index":0}`, `{"type":"message_stop"}`)
			})
			res, err := srv.client(t, nil).Complete(context.Background(), anthropicHi(), func(Delta) {})
			if err != nil {
				t.Fatalf("complete: %v", err)
			}
			if len(res.ToolCalls) != 1 || res.ToolCalls[0].Arguments != `{"id": "a"}` {
				t.Fatalf("tool calls = %+v", res.ToolCalls)
			}
		})
	}
}

// A token count is read as leniently as the SDK read one, so a gateway that
// writes 12.0 or "12" still gets its paid-for answer through: a whole number
// either way, and 0 for what is no number at all.
func TestAnthropicReadsTokenCountsLeniently(t *testing.T) {
	oneShot := newAnthropicServer(t, func(w http.ResponseWriter, _ map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"content":[{"type":"text","text":"a"}],"usage":{"input_tokens":12.0,"output_tokens":"3",`+
			`"cache_read_input_tokens":null,"cache_creation_input_tokens":"many"}}`)
	})
	res, err := oneShot.client(t, nil).Complete(context.Background(), anthropicHi(), nil)
	if err != nil {
		t.Fatalf("one-shot: %v", err)
	}
	if res.Content != "a" || *res.Usage != (Usage{PromptTokens: 12, CompletionTokens: 3}) {
		t.Fatalf("content %q, usage %+v", res.Content, res.Usage)
	}

	streamed := newAnthropicServer(t, func(w http.ResponseWriter, _ map[string]any) {
		anthropicSSE(w,
			`{"type":"message_start","message":{"content":[],"usage":{"input_tokens":10.0,"output_tokens":0}}}`,
			anthropicStreamOK[1], anthropicStreamOK[2], anthropicStreamOK[3],
			`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":"4","input_tokens":"11","cache_read_input_tokens":2.0}}`,
			`{"type":"message_stop"}`)
	})
	res, err = streamed.client(t, nil).Complete(context.Background(), anthropicHi(), func(Delta) {})
	if err != nil {
		t.Fatalf("streamed: %v", err)
	}
	if res.Content != "ok" || *res.Usage != (Usage{PromptTokens: 13, CompletionTokens: 4, CacheReadTokens: 2}) {
		t.Fatalf("content %q, usage %+v", res.Content, res.Usage)
	}
}

// An error quotes at most anthropicMaxErrorBody bytes of what the endpoint
// sent, a refusal's body and a stream's error event alike, and the cut never
// keeps part of the key: the scrub removes only a whole key, so a fragment
// left at the cut would reach the thread's reason.
func TestAnthropicErrorQuotesNoFragmentOfTheKey(t *testing.T) {
	const key = "zq-straddled-secret"
	head := `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key`
	for name, tc := range map[string]struct {
		stream bool
		send   func(w http.ResponseWriter, body string)
	}{
		"a refusal": {false, func(w http.ResponseWriter, body string) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = fmt.Fprint(w, body)
		}},
		"an error event": {true, func(w http.ResponseWriter, body string) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, anthropicEventStream(anthropicStreamOK[0]), "event: error\ndata: ", body, "\n\n")
		}},
	} {
		t.Run(name, func(t *testing.T) {
			var body atomic.Value
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				tc.send(w, body.Load().(string))
			}))
			defer srv.Close()
			c, err := New(WireAnthropic, Config{BaseURL: srv.URL, APIKey: key})
			if err != nil {
				t.Fatalf("new: %v", err)
			}
			var onDelta func(Delta)
			if tc.stream {
				onDelta = func(Delta) {}
			}
			call := func(b string) string {
				t.Helper()
				body.Store(b)
				_, err := c.Complete(context.Background(), anthropicHi(), onDelta)
				if err == nil {
					t.Fatal("no error")
				}
				return err.Error()
			}
			for at := anthropicMaxErrorBody - len(key) + 1; at < anthropicMaxErrorBody; at++ {
				got := call(head + strings.Repeat(" ", at-len(head)) + key + `"}}`)
				if strings.Contains(got, "zq") {
					t.Fatalf("key at byte %d: a fragment of it survived the cut: ...%s", at, got[max(0, len(got)-64):])
				}
				if !strings.Contains(got, "authentication_error") {
					t.Fatalf("key at byte %d: the words before it were dropped: %.200s", at, got)
				}
			}
			long := head + " " + strings.Repeat("long ", 3<<20/5) + `"}}`
			if got := call(long); len(got) > anthropicMaxErrorBody+512 {
				t.Fatalf("a %d-byte refusal is quoted at %d bytes", len(long), len(got))
			}
		})
	}
}

// bodyCounter counts the response bodies its transport hands out and how
// many of them were closed.
type bodyCounter struct {
	inner          http.RoundTripper
	opened, closed atomic.Int32
}

func (b *bodyCounter) RoundTrip(r *http.Request) (*http.Response, error) {
	res, err := b.inner.RoundTrip(r)
	if err == nil {
		b.opened.Add(1)
		res.Body = &countedBody{ReadCloser: res.Body, closed: &b.closed}
	}
	return res, err
}

type countedBody struct {
	io.ReadCloser
	once   sync.Once
	closed *atomic.Int32
}

func (b *countedBody) Close() error {
	b.once.Do(func() { b.closed.Add(1) })
	return b.ReadCloser.Close()
}

// Every response body the adapter receives is closed, whichever way the call
// ends: answered, refused, retried, streamed, or ended by an error event.
func TestAnthropicClosesEveryResponseBody(t *testing.T) {
	refuse := func(w http.ResponseWriter, status int) {
		w.Header().Set("Retry-After-Ms", "0")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, `{"type":"error","error":{"type":"api_error","message":"refused"}}`)
	}
	for name, tc := range map[string]struct {
		stream  bool
		respond func(attempt int32, w http.ResponseWriter)
	}{
		"a one-shot answer": {false, func(_ int32, w http.ResponseWriter) { anthropicMessageBody(w) }},
		"a refusal":         {false, func(_ int32, w http.ResponseWriter) { refuse(w, http.StatusBadRequest) }},
		"retried, then answered": {false, func(n int32, w http.ResponseWriter) {
			if n < 3 {
				refuse(w, 529)
				return
			}
			anthropicMessageBody(w)
		}},
		"retries spent":          {false, func(_ int32, w http.ResponseWriter) { refuse(w, 529) }},
		"a streamed answer":      {true, func(_ int32, w http.ResponseWriter) { anthropicSSE(w, anthropicStreamOK...) }},
		"a stream retries spent": {true, func(_ int32, w http.ResponseWriter) { refuse(w, 529) }},
		"a stream retried, then answered": {true, func(n int32, w http.ResponseWriter) {
			if n < 2 {
				refuse(w, 529)
				return
			}
			anthropicSSE(w, anthropicStreamOK...)
		}},
		"a stream's error event": {true, func(_ int32, w http.ResponseWriter) {
			anthropicSSE(w, anthropicStreamOK[0], `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)
		}},
	} {
		t.Run(name, func(t *testing.T) {
			var attempts atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				tc.respond(attempts.Add(1), w)
			}))
			defer srv.Close()
			c := newAnthropic(Config{BaseURL: srv.URL, APIKey: "sk-test"})
			hc, _ := c.doer.inner.(*http.Client)
			bodies := &bodyCounter{inner: hc.Transport}
			hc.Transport = bodies
			var onDelta func(Delta)
			if tc.stream {
				onDelta = func(Delta) {}
			}
			_, _ = c.Complete(context.Background(), anthropicHi(), onDelta)
			if opened, closed := bodies.opened.Load(), bodies.closed.Load(); opened == 0 || closed != opened {
				t.Fatalf("%d of %d response bodies closed", closed, opened)
			}
		})
	}
}

// The caller's context bounds the whole call: a retry-after wait, a slow
// one-shot body and a slow stream each end when it does, with no attempt
// after it.
func TestAnthropicStopsWhenTheContextEnds(t *testing.T) {
	for name, tc := range map[string]struct {
		stream  bool
		respond func(w http.ResponseWriter, r *http.Request)
	}{
		"a retry-after wait": {false, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
		}},
		"a slow one-shot body": {false, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"content":[`)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}},
		"a slow stream": {true, func(w http.ResponseWriter, r *http.Request) {
			anthropicSSE(w, anthropicStreamOK[:3]...)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}},
	} {
		t.Run(name, func(t *testing.T) {
			var attempts atomic.Int32
			answered := make(chan struct{}, 3)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				go func() { time.Sleep(100 * time.Millisecond); answered <- struct{}{} }()
				tc.respond(w, r)
			}))
			defer srv.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() { <-answered; cancel() }()
			var onDelta func(Delta)
			if tc.stream {
				onDelta = func(Delta) {}
			}
			began := time.Now()
			_, err := anthropicClientAt(t, srv.URL).Complete(ctx, anthropicHi(), onDelta)
			if err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
				t.Fatalf("err = %v, want the context's", err)
			}
			if took := time.Since(began); took > 10*time.Second {
				t.Fatalf("the call took %v, long past its context's end", took)
			}
			if got := attempts.Load(); got != 1 {
				t.Fatalf("%d attempts, want 1", got)
			}
		})
	}
}

// A stream that breaks after it started is not retried: what it delivered
// already reached onDelta, and the call ends with the break.
func TestAnthropicNeverReplaysAStartedStream(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		chunk := anthropicEventStream(anthropicStreamOK[:3]...)
		_, _ = fmt.Fprintf(buf, "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nRetry-After-Ms: 0\r\n"+
			"Transfer-Encoding: chunked\r\n\r\n%x\r\n%s\r\n", len(chunk), chunk)
		_ = buf.Flush()
		_ = conn.Close()
	}))
	defer srv.Close()
	var deltas strings.Builder
	_, err := anthropicClientAt(t, srv.URL).Complete(context.Background(), anthropicHi(), func(d Delta) { deltas.WriteString(d.Text) })
	if err == nil {
		t.Fatal("a broken stream ended without an error")
	}
	if deltas.String() != "ok" || attempts.Load() != 1 {
		t.Fatalf("deltas %q after %d attempts, want ok after 1", deltas.String(), attempts.Load())
	}
}
