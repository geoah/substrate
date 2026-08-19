package embed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The embeddings baseURL is repository-chosen too, and it drains on a background
// ticker rather than only on user dispatch, so the dial must be confined to
// public destinations (issue #241). The allowlist is cleared here, so the gate
// blocks the loopback fake and Embed errors at DIAL. It fails against a client
// not built on egress.Transport, which would reach the fake.
func TestEmbedDialConfinedToPublic(t *testing.T) {
	t.Setenv("SUBSTRATE_EGRESS_ALLOW", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[0.1]}]}`))
	}))
	defer srv.Close()

	client, err := New(Config{BaseURL: srv.URL, APIKey: "sk-test-241-secret", Model: DefaultModel})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	_, err = client.Embed(context.Background(), []string{"hi"})
	if err == nil {
		t.Fatal("an embeddings call against a loopback baseURL succeeded: the dial was not gated")
	}
	if !strings.Contains(err.Error(), "egress blocked") {
		t.Fatalf("error = %v, want it to name egress blocked", err)
	}
}
