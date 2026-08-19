package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A provider row's baseURL is repository-chosen, so an openai-wire client built
// from one must refuse a dial to a private or loopback address (issue #241).
// The allowlist is cleared for this test, so the gate blocks loopback exactly as
// a real deployment does; the completion errors at DIAL, before the fake server
// is ever reached. It fails against a client not built on egress.Transport,
// which would connect to the loopback fake and return its canned body.
func TestOpenAIDialConfinedToPublic(t *testing.T) {
	t.Setenv("SUBSTRATE_EGRESS_ALLOW", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		jsonBody(w, map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "leaked"}}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
		})
	}))
	defer srv.Close()

	client, err := New(WireOpenAI, Config{BaseURL: srv.URL, APIKey: "sk-test-241-secret"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	_, err = client.Complete(context.Background(), Request{
		Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, nil)
	if err == nil {
		t.Fatal("a completion against a loopback baseURL succeeded: the dial was not gated")
	}
	if !strings.Contains(err.Error(), "egress blocked") {
		t.Fatalf("error = %v, want it to name egress blocked", err)
	}
}

// The same confinement holds on the anthropic wire, whose client is built
// through a different SDK option.
func TestAnthropicDialConfinedToPublic(t *testing.T) {
	t.Setenv("SUBSTRATE_EGRESS_ALLOW", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		jsonBody(w, map[string]any{
			"content":       []any{map[string]any{"type": "text", "text": "leaked"}},
			"usage":         map[string]any{"input_tokens": 1, "output_tokens": 1},
			"stop_reason":   "end_turn",
			"role":          "assistant",
			"type":          "message",
			"model":         "m",
			"id":            "msg_1",
			"stop_sequence": nil,
		})
	}))
	defer srv.Close()

	client, err := New(WireAnthropic, Config{BaseURL: srv.URL, APIKey: "sk-test-241-secret"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	_, err = client.Complete(context.Background(), Request{
		Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, nil)
	if err == nil {
		t.Fatal("a completion against a loopback baseURL succeeded: the dial was not gated")
	}
	if !strings.Contains(err.Error(), "egress blocked") {
		t.Fatalf("error = %v, want it to name egress blocked", err)
	}
}
