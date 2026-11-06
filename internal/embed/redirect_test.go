package embed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// An embeddings request carries the key too, so it follows no redirect: the
// call fails naming it, and the place the redirect pointed at sees nothing.
func TestEmbedRefusesRedirects(t *testing.T) {
	var reached atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()

	client, err := New(Config{BaseURL: origin.URL, APIKey: "sk-redirect-fixture", Model: DefaultModel, Headers: map[string]string{"X-Gateway-Token": "g"}})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	_, err = client.Embed(context.Background(), []string{"hi"})
	if err == nil || !strings.Contains(err.Error(), "follows no redirect") {
		t.Fatalf("error = %v, want the refused redirect", err)
	}
	if n := reached.Load(); n > 0 {
		t.Fatalf("the redirect target received %d requests", n)
	}
}

// The usage the endpoint reports reaches the caller, so an operator-paid
// request settles to what it cost.
func TestEmbedUsageReportsPromptTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		vec := strings.Repeat("0.1,", 1535) + "0.1"
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[` + vec + `]}],"usage":{"prompt_tokens":7,"total_tokens":7}}`))
	}))
	defer srv.Close()
	client, err := New(Config{BaseURL: srv.URL, APIKey: "k", Model: DefaultModel})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	vecs, usage, err := client.EmbedUsage(context.Background(), []string{"hi"})
	if err != nil || len(vecs) != 1 {
		t.Fatalf("embed = %d vectors, %v", len(vecs), err)
	}
	if usage.PromptTokens != 7 {
		t.Fatalf("usage = %+v, want 7 prompt tokens", usage)
	}
}
