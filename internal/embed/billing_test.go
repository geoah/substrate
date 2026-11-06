package embed

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/egress"
)

// A caller paying per request (the host's ledger) settles on what these hold:
// an answer the client refuses after the endpoint sent it still reports the
// usage it carried, and a failure is marked egress.ErrUnprocessed only where
// the endpoint cannot have done the work.
func TestEmbedFailuresSayWhatTheEndpointBilled(t *testing.T) {
	vector := func(width int) string { return strings.TrimSuffix(strings.Repeat("0.1,", width), ",") }
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()
	cases := []struct {
		name        string
		serve       http.HandlerFunc
		unprocessed bool
		tokens      int
	}{
		{"a 401", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"error":{"message":"bad key"}}`, http.StatusUnauthorized)
		}, true, 0},
		{"a 400", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"error":{"message":"bad input"}}`, http.StatusBadRequest)
		}, true, 0},
		{"nothing listening", nil, true, 0},
		{"a 500", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"error":{"message":"down"}}`, http.StatusInternalServerError)
		}, false, 0},
		{"an answer cut off midway", func(w http.ResponseWriter, _ *http.Request) {
			conn, buf, err := w.(http.Hijacker).Hijack()
			if err != nil {
				return
			}
			_, _ = fmt.Fprint(buf, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 5000\r\n\r\n{\"data\":[{\"index\":0,")
			_ = buf.Flush()
			_ = conn.Close()
		}, false, 0},
		{"an answer of the wrong width", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"data":[{"index":0,"embedding":[%s]}],"usage":{"prompt_tokens":7,"total_tokens":7}}`, vector(8))
		}, false, 7},
		{"an answer one vector short", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"data":[],"usage":{"prompt_tokens":7,"total_tokens":7}}`)
		}, false, 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			base := closedURL
			if tc.serve != nil {
				srv := httptest.NewServer(tc.serve)
				t.Cleanup(srv.Close)
				base = srv.URL
			}
			client, err := New(Config{BaseURL: base, APIKey: "sk-embed-billing-fixture", Model: DefaultModel})
			if err != nil {
				t.Fatalf("new: %v", err)
			}
			_, usage, err := client.EmbedUsage(context.Background(), []string{"hi"})
			if err == nil {
				t.Fatal("the request succeeded")
			}
			if got := errors.Is(err, egress.ErrUnprocessed); got != tc.unprocessed {
				t.Fatalf("unprocessed = %v, want %v: %v", got, tc.unprocessed, err)
			}
			if usage.PromptTokens != tc.tokens {
				t.Fatalf("usage = %+v, want %d prompt tokens", usage, tc.tokens)
			}
		})
	}
}
