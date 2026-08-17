package embed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The queue drains on a server loop and reports through the HOST's log, so an
// error carrying the row's bearer would put one repository's secret where the
// whole deployment can read it.
//
// The two shapes are both real and only one is obvious. A short key comes back
// whole; a real one comes back MASKED, because OpenAI stars out the middle
// itself and returns the leading and trailing fragments. An exact-match scrub
// removes nothing from the masked form, so the fragments reach the log.
func TestEmbedErrorDoesNotCarryTheKey(t *testing.T) {
	t.Parallel()
	// The masked body is the live 401's own shape: prefix, asterisks, the last
	// four characters. Written as fragments of one key so the assertions below
	// are about that key and not about a literal.
	const key = "sk-proj-notarealkey000000000000000000000000000cdef"
	prefix, suffix := key[:8], key[len(key)-4:]
	for name, quoted := range map[string]string{
		"quoted whole": key,
		"self-masked":  prefix + strings.Repeat("*", 32) + suffix,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided: ` + quoted +
					`. You can find your API key at https://platform.openai.com/account/api-keys.","type":"invalid_request_error"}}`))
			}))
			t.Cleanup(srv.Close)

			c, err := New(Config{BaseURL: srv.URL, APIKey: key, Model: DefaultModel})
			if err != nil {
				t.Fatalf("new: %v", err)
			}
			_, err = c.Embed(context.Background(), []string{"anything"})
			if err == nil {
				t.Fatal("a 401 was not reported as an error")
			}
			got := err.Error()
			if strings.Contains(got, key) {
				t.Fatalf("the error carries the row's key whole: %v", got)
			}
			// The fragments are the point: a masked key still hands a reader
			// both ends of the secret.
			if strings.Contains(got, prefix) || strings.Contains(got, suffix) {
				t.Fatalf("the error carries a fragment of the row's key: %v", got)
			}
			if !strings.Contains(got, "<redacted>") {
				t.Fatalf("the error dropped the endpoint's message instead of scrubbing it: %v", got)
			}
			// The rest of the endpoint's words survive, or the scrub has cost
			// the reader the reason for the failure.
			if !strings.Contains(got, "Incorrect API key provided") {
				t.Fatalf("the scrub ate the endpoint's message: %v", got)
			}
		})
	}
}

// The width is asserted against the model's declared one, so an endpoint
// serving something else under a known model name is refused here rather than
// by Postgres, where the model would not be in the message.
func TestEmbedRefusesAWrongWidthAnswer(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[0.1,0.2,0.3]}]}`))
	}))
	t.Cleanup(srv.Close)

	c, err := New(Config{BaseURL: srv.URL, APIKey: "k", Model: DefaultModel})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	_, err = c.Embed(context.Background(), []string{"anything"})
	if err == nil || !strings.Contains(err.Error(), "3-wide") {
		t.Fatalf("a 3-wide answer was accepted under a 1536-wide model: %v", err)
	}
}

// New refuses what a repository that named no provider would hand it, and
// refuses a model whose width the stored column cannot hold (decision record
// 0026).
func TestNewRefusesAnIncompleteRow(t *testing.T) {
	t.Parallel()
	for name, cfg := range map[string]Config{
		"no baseURL": {APIKey: "k", Model: DefaultModel},
		"no apiKey":  {BaseURL: "http://localhost:1", Model: DefaultModel},
		"no model":   {BaseURL: "http://localhost:1", APIKey: "k"},
		"wrong width": {
			BaseURL: "http://localhost:1", APIKey: "k",
			Model: "text-embedding-3-large",
		},
		"unknown model": {
			BaseURL: "http://localhost:1", APIKey: "k",
			Model: "some-local-embedder",
		},
	} {
		if _, err := New(cfg); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
	// The gateway prefix is stripped before the width lookup, so a row naming
	// a routed alias still resolves.
	if _, err := New(Config{
		BaseURL: "http://localhost:1", APIKey: "k",
		Model: "openai/text-embedding-3-small",
	}); err != nil {
		t.Fatalf("a routed alias was refused: %v", err)
	}
}
