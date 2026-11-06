package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/providersecret"
)

// A 401 or a 403 is the endpoint refusing the key, and both wires mark it
// with its status through the scrubbed rebuild, one-shot and streamed alike,
// so the engine can tell a refused key from any other failure. No other
// status carries the mark, and the mark carries no key.
func TestBothWiresMarkARefusedKeyWithItsStatus(t *testing.T) {
	t.Parallel()
	const key = "zq-refused-key-fixture"
	for _, wire := range []Wire{WireOpenAI, WireAnthropic} {
		for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusBadRequest, http.StatusTooManyRequests, http.StatusInternalServerError} {
			for _, streamed := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%d/streamed=%v", wire, status, streamed), func(t *testing.T) {
					t.Parallel()
					srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						// No retry: the anthropic adapter retries a 429 and a
						// 5xx unless the endpoint says not to.
						w.Header().Set("X-Should-Retry", "false")
						w.WriteHeader(status)
						_, _ = fmt.Fprintf(w, `{"type":"error","error":{"type":"authentication_error","message":"refused %s"}}`, key)
					}))
					t.Cleanup(srv.Close)
					c, err := New(wire, Config{BaseURL: srv.URL + "/", APIKey: key})
					if err != nil {
						t.Fatal(err)
					}
					var onDelta func(Delta)
					if streamed {
						onDelta = func(Delta) {}
					}
					_, err = c.Complete(context.Background(), Request{
						Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}, Params: Params{MaxTokens: 16},
					}, onDelta)
					if err == nil {
						t.Fatal("no error")
					}
					want := 0
					if status == http.StatusUnauthorized || status == http.StatusForbidden {
						want = status
					}
					if got := providersecret.KeyRefusal(err); got != want {
						t.Fatalf("KeyRefusal = %d, want %d: %v", got, want, err)
					}
					if strings.Contains(err.Error(), key) {
						t.Fatalf("the error carries the key: %v", err)
					}
				})
			}
		}
	}
}
