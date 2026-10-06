package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The Anthropic-wire adapter against a server that speaks that wire. What is
// checked here is exactly what differs from the OpenAI wire: the top-level
// system param, tool_result blocks grouped into one user turn, the required
// max_tokens, and a temperature that is sent only when asked for.

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
	}, func(s string) { deltas = append(deltas, s) })
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

// The wire's stop_reason maps onto the neutral set, and the context size adds
// the cache reads and writes this wire reports beside input_tokens, while the
// billed prompt stays input_tokens alone.
func TestAnthropicMapsStopReasonAndContextTokens(t *testing.T) {
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
			if res.Usage.PromptTokens != 10 || res.Usage.ContextTokens != 350 {
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
