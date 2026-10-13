package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// The live cases' override variables, driven through a fake laid out like
// OpenRouter: the OpenAI wire under /api/v1, the Anthropic wire under /api.
// Each wire's endpoint and model come from the same liveEndpoint the live
// cases build from, and the gateway's model slug, slash included, reaches the
// wire as written.
func TestOverridesPointTheLiveWiresAtAGateway(t *testing.T) {
	var (
		mu    sync.Mutex
		model = map[string]string{} // request path -> model sent
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		model[r.URL.Path], _ = req["model"].(string)
		mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/chat/completions":
			jsonBody(w, map[string]any{
				"id": "cmpl", "object": "chat.completion",
				"choices": []any{map[string]any{
					"index": 0, "finish_reason": "stop",
					"message": map[string]any{"role": "assistant", "content": "blue"},
				}},
				"usage": map[string]any{"prompt_tokens": 3, "completion_tokens": 1},
			})
		case "/api/v1/messages":
			jsonBody(w, map[string]any{
				"id": "msg_1", "type": "message", "role": "assistant", "model": req["model"],
				"stop_reason": "end_turn",
				"content":     []any{map[string]any{"type": "text", "text": "blue"}},
				"usage":       map[string]any{"input_tokens": 3, "output_tokens": 1},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	t.Setenv(liveOpenAIBaseURLEnv, srv.URL+"/api/v1")
	t.Setenv(liveAnthropicBaseURLEnv, srv.URL+"/api")
	t.Setenv(liveOpenAIModelEnv, "openai/gpt-4.1-mini")
	t.Setenv(liveAnthropicModelEnv, "anthropic/claude-haiku-4.5")

	for _, c := range []struct {
		endpoint liveEndpoint
		path     string
		model    string
	}{
		{liveOpenAI, "/api/v1/chat/completions", "openai/gpt-4.1-mini"},
		{liveAnthropic, "/api/v1/messages", "anthropic/claude-haiku-4.5"},
	} {
		t.Run(string(c.endpoint.wire), func(t *testing.T) {
			cfg := c.endpoint.config("sk-test")
			// Checked before the request: a client that ignored the override
			// would send it to the provider's real endpoint.
			if !strings.HasPrefix(cfg.BaseURL, srv.URL+"/") {
				t.Fatalf("base URL %q, want the override under %s", cfg.BaseURL, srv.URL)
			}
			if got := c.endpoint.modelName(); got != c.model {
				t.Fatalf("model %q, want the override %q", got, c.model)
			}
			client, err := New(c.endpoint.wire, cfg)
			if err != nil {
				t.Fatalf("new: %v", err)
			}
			if _, err := client.Complete(context.Background(), Request{
				Model:    c.endpoint.modelName(),
				Messages: []Message{{Role: RoleUser, Content: liveOneWordUser}},
			}, nil); err != nil {
				t.Fatalf("complete: %v", err)
			}
			mu.Lock()
			defer mu.Unlock()
			if got, ok := model[c.path]; !ok || got != c.model {
				t.Fatalf("the gateway saw %v, want %q at %s", model, c.model, c.path)
			}
		})
	}
}

// With no override set, each wire buys from the endpoint it always did:
// OpenAI's own, and the anthropic adapter's default.
func TestDefaultLiveEndpointsAreTheProvidersOwn(t *testing.T) {
	for _, env := range []string{liveOpenAIBaseURLEnv, liveAnthropicBaseURLEnv, liveOpenAIModelEnv, liveAnthropicModelEnv} {
		t.Setenv(env, "")
	}
	if got := liveOpenAI.config("k").BaseURL; got != "https://api.openai.com/v1" {
		t.Fatalf("openai base URL %q", got)
	}
	if got := liveAnthropic.config("k").BaseURL; got != "" {
		t.Fatalf("anthropic base URL %q, want empty", got)
	}
	if got := liveOpenAI.modelName(); got != "gpt-4.1-mini" {
		t.Fatalf("openai model %q", got)
	}
	if got := liveAnthropic.modelName(); got != "claude-haiku-4-5" {
		t.Fatalf("anthropic model %q", got)
	}
}
