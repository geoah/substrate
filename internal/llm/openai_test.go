package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The OpenAI-wire adapter against a server that speaks that wire: the request
// shape it puts on the wire, and the result it reads back — one-shot and
// streamed, text-only and with tool calls.

// openaiServer records the last request body and answers with a canned body.
type openaiServer struct {
	srv  *httptest.Server
	last map[string]any
	path string
	auth string
	hdr  http.Header
}

func newOpenAIServer(t *testing.T, respond func(w http.ResponseWriter, req map[string]any)) *openaiServer {
	t.Helper()
	s := &openaiServer{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.last, s.path, s.auth, s.hdr = req, r.URL.Path, r.Header.Get("Authorization"), r.Header.Clone()
		respond(w, req)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func jsonBody(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// An explicit temperature of 0 must REACH the wire. The SDK tags the field
// `omitempty`, so a true zero marshals away and the endpoint samples at its
// own default instead — the agent's declared determinism silently lost.
func TestOpenAISendsAnExplicitZeroTemperature(t *testing.T) {
	srv := newOpenAIServer(t, func(w http.ResponseWriter, _ map[string]any) {
		jsonBody(w, map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "ok"}}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
		})
	})
	client, err := New(WireOpenAI, Config{BaseURL: srv.srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	zero := float32(0)
	if _, err := client.Complete(context.Background(), Request{
		Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Params: Params{Temperature: &zero},
	}, nil); err != nil {
		t.Fatalf("complete: %v", err)
	}
	sent, ok := srv.last["temperature"].(float64)
	if !ok {
		t.Fatalf("temperature never reached the wire: %v", srv.last)
	}
	// Indistinguishable from 0 to the sampler; the point is only that the key
	// travels at all.
	if sent > 1e-30 {
		t.Fatalf("temperature = %v, want an effective zero", sent)
	}
	// A nil Temperature still sends nothing: "do not send one" is not zero.
	if _, err := client.Complete(context.Background(), Request{
		Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, nil); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, sent := srv.last["temperature"]; sent {
		t.Fatalf("temperature was sent unasked: %v", srv.last["temperature"])
	}
}

func TestOpenAIOneShotCarriesSystemToolsAndUsage(t *testing.T) {
	srv := newOpenAIServer(t, func(w http.ResponseWriter, _ map[string]any) {
		jsonBody(w, map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{
				"role": "assistant", "content": "hello back",
			}}},
			"usage": map[string]any{"prompt_tokens": 11, "completion_tokens": 7},
		})
	})
	client, err := New(WireOpenAI, Config{BaseURL: srv.srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	temp := float32(0.25)
	res, err := client.Complete(context.Background(), Request{
		Model: "m", System: "you are a test",
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
		Tools:    []Tool{{Name: "peek", Description: "look", Parameters: map[string]any{"type": "object"}}},
		Params:   Params{Temperature: &temp, MaxTokens: 64},
	}, nil)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if res.Content != "hello back" || len(res.ToolCalls) != 0 {
		t.Fatalf("result = %+v", res)
	}
	if res.Usage == nil || res.Usage.PromptTokens != 11 || res.Usage.CompletionTokens != 7 {
		t.Fatalf("usage = %+v", res.Usage)
	}
	if srv.auth != "Bearer k" {
		t.Fatalf("authorization = %q", srv.auth)
	}
	// The system prompt leads the messages on this wire.
	messages, _ := srv.last["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("messages = %v", srv.last["messages"])
	}
	first, _ := messages[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "you are a test" {
		t.Fatalf("first message = %v", first)
	}
	if srv.last["temperature"] != 0.25 || srv.last["max_completion_tokens"] != float64(64) {
		t.Fatalf("params = %v / %v", srv.last["temperature"], srv.last["max_completion_tokens"])
	}
	tools, _ := srv.last["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %v", srv.last["tools"])
	}
}

func TestOpenAIOneShotReadsToolCalls(t *testing.T) {
	srv := newOpenAIServer(t, func(w http.ResponseWriter, _ map[string]any) {
		jsonBody(w, map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{
				"role": "assistant",
				"tool_calls": []any{map[string]any{
					"id": "call_0", "type": "function",
					"function": map[string]any{"name": "peek", "arguments": `{"id":"x"}`},
				}},
			}}},
			"usage": map[string]any{"prompt_tokens": 3, "completion_tokens": 2},
		})
	})
	client, _ := New(WireOpenAI, Config{BaseURL: srv.srv.URL, APIKey: "k"})
	res, err := client.Complete(context.Background(), Request{
		Model: "m",
		Messages: []Message{
			{Role: RoleUser, Content: "go"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call_prev", Name: "peek", Arguments: "{}"}}},
			{Role: RoleTool, ToolCallID: "call_prev", ToolName: "peek", Content: "{}"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].ID != "call_0" || res.ToolCalls[0].Name != "peek" {
		t.Fatalf("tool calls = %+v", res.ToolCalls)
	}
	if res.ToolCalls[0].Arguments != `{"id":"x"}` {
		t.Fatalf("arguments = %q", res.ToolCalls[0].Arguments)
	}
	// The prior turns map onto the wire's roles 1:1, tool answers included.
	messages, _ := srv.last["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("messages = %v", srv.last["messages"])
	}
	toolMsg, _ := messages[2].(map[string]any)
	if toolMsg["role"] != "tool" || toolMsg["tool_call_id"] != "call_prev" {
		t.Fatalf("tool message = %v", toolMsg)
	}
}

func TestOpenAIStreamsDeltasAndAccumulatesToolCalls(t *testing.T) {
	chunks := []string{
		`{"choices":[{"delta":{"content":"he"}}]}`,
		`{"choices":[{"delta":{"content":"llo"}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_0","type":"function","function":{"name":"peek","arguments":"{\"id\":"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"x\"}"}}]}}]}`,
		`{"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":9}}`,
	}
	srv := newOpenAIServer(t, func(w http.ResponseWriter, _ map[string]any) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, c := range chunks {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", c)
			flusher.Flush()
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	})
	client, _ := New(WireOpenAI, Config{BaseURL: srv.srv.URL, APIKey: "k"})
	var deltas []string
	res, err := client.Complete(context.Background(), Request{
		Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, func(s string) { deltas = append(deltas, s) })
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if strings.Join(deltas, "") != "hello" || res.Content != "hello" {
		t.Fatalf("deltas = %v, content = %q", deltas, res.Content)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].Arguments != `{"id":"x"}` {
		t.Fatalf("tool calls = %+v", res.ToolCalls)
	}
	if res.Usage == nil || res.Usage.PromptTokens != 5 || res.Usage.CompletionTokens != 9 {
		t.Fatalf("usage = %+v", res.Usage)
	}
	// The usage tally only rides a stream that asked for it.
	opts, _ := srv.last["stream_options"].(map[string]any)
	if opts == nil || opts["include_usage"] != true {
		t.Fatalf("stream_options = %v", srv.last["stream_options"])
	}
}

func TestOpenAICustomHeadersRide(t *testing.T) {
	srv := newOpenAIServer(t, func(w http.ResponseWriter, _ map[string]any) {
		jsonBody(w, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}}}})
	})
	client, _ := New(WireOpenAI, Config{
		BaseURL: srv.srv.URL, APIKey: "k",
		Headers: map[string]string{"HTTP-Referer": "https://example.com", "X-Title": "substrate"},
	})
	if _, err := client.Complete(context.Background(), Request{Model: "m"}, nil); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if srv.hdr.Get("HTTP-Referer") != "https://example.com" || srv.hdr.Get("X-Title") != "substrate" {
		t.Fatalf("headers = %v", srv.hdr)
	}
	if srv.auth != "Bearer k" {
		t.Fatalf("the extra headers displaced the bearer: %q", srv.auth)
	}
}

// Azure is the same wire under a different client configuration: a
// deployment-shaped path, the api-key header, and an api-version query.
func TestAzureUsesTheAzureClientConfig(t *testing.T) {
	srv := newOpenAIServer(t, func(w http.ResponseWriter, _ map[string]any) {
		jsonBody(w, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}}}})
	})
	client, err := New(WireAzure, Config{BaseURL: srv.srv.URL, APIKey: "azure-key"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := client.Complete(context.Background(), Request{Model: "gpt-4o"}, nil); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if !strings.Contains(srv.path, "/deployments/gpt-4o/") {
		t.Fatalf("path = %q", srv.path)
	}
	if srv.hdr.Get("api-key") != "azure-key" {
		t.Fatalf("api-key = %q", srv.hdr.Get("api-key"))
	}
}

func TestUnknownWireRefuses(t *testing.T) {
	_, err := New(Wire("openrouter"), Config{})
	if err == nil || !strings.Contains(err.Error(), "openai, anthropic, azure") {
		t.Fatalf("err = %v", err)
	}
}
