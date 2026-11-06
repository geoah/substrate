package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// keyCatcher is the endpoint a redirect points at: it records every request
// that reaches it, with the credential headers it carried, and answers as a
// completion would on either wire, so a followed redirect also succeeds.
type keyCatcher struct {
	mu   sync.Mutex
	seen []string
	srv  *httptest.Server
}

func newKeyCatcher(t *testing.T) *keyCatcher {
	t.Helper()
	k := &keyCatcher{}
	k.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		k.mu.Lock()
		k.seen = append(k.seen, r.Header.Get("Authorization")+"|"+r.Header.Get("X-Api-Key")+"|"+r.Header.Get("X-Gateway-Token"))
		k.mu.Unlock()
		jsonBody(w, map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "followed"}}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
			"content": []any{map[string]any{"type": "text", "text": "followed"}},
			"role":    "assistant", "type": "message", "model": "m", "id": "msg_1", "stop_reason": "end_turn",
		})
	}))
	t.Cleanup(k.srv.Close)
	return k
}

func (k *keyCatcher) requests() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]string(nil), k.seen...)
}

// redirector answers every request with a 307 to target, which keeps the
// method and the body, so a client that follows it posts the completion,
// key and all, to target.
func redirector(t *testing.T, target string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A request carrying a key follows no redirect, on either wire: the call fails
// naming the redirect, and the place the redirect pointed at sees nothing. Go
// would follow a 307 with the body and every header but Authorization on
// another host, and with Authorization too on the same host name, so the key,
// the anthropic wire's x-api-key and a row's own headers would all arrive.
func TestCredentialRequestsRefuseRedirects(t *testing.T) {
	for _, wire := range []Wire{WireOpenAI, WireAnthropic} {
		t.Run(string(wire), func(t *testing.T) {
			catcher := newKeyCatcher(t)
			origin := redirector(t, catcher.srv.URL)
			client, err := New(wire, Config{
				BaseURL: origin.URL, APIKey: "sk-redirect-fixture",
				Headers: map[string]string{"X-Gateway-Token": "gateway-fixture"},
			})
			if err != nil {
				t.Fatalf("new: %v", err)
			}
			for _, stream := range []bool{false, true} {
				var onDelta func(Delta)
				if stream {
					onDelta = func(Delta) {}
				}
				_, err = client.Complete(context.Background(), Request{
					Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}},
				}, onDelta)
				if err == nil {
					t.Fatalf("stream=%v: a redirected completion succeeded", stream)
				}
				if !strings.Contains(err.Error(), "follows no redirect") {
					t.Fatalf("stream=%v: error = %v, want it to name the refused redirect", stream, err)
				}
			}
			if seen := catcher.requests(); len(seen) > 0 {
				t.Fatalf("the redirect target received %d requests carrying %q", len(seen), seen)
			}
		})
	}
}
