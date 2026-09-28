package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Both live streams ask whatever proxies them to pass each line through as it
// is written: an nginx ingress buffers a response by default, and a compressing
// intermediary holds lines back, and either turns a streamed chat reply into
// one block at the end.
func TestStreamsAskProxiesNotToBuffer(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)

	chat := env.do(t, http.MethodPost, "/api/v1/substrate.reamde.dev/core/agent/helper/chat", tok,
		map[string]any{"message": "hi"})
	wantStatus(t, chat, http.StatusOK)

	srv := httptest.NewServer(env.h)
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/changes?watch=1&from=0", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("watch status = %d", resp.StatusCode)
	}
	watch := resp.Header

	for name, h := range map[string]http.Header{"chat": chat.Header(), "watch": watch} {
		if got := h.Get("X-Accel-Buffering"); got != "no" {
			t.Errorf("%s: X-Accel-Buffering = %q, want no", name, got)
		}
		if got := h.Get("Cache-Control"); got != "no-store, no-transform" {
			t.Errorf("%s: Cache-Control = %q, want no-store, no-transform", name, got)
		}
		if got := h.Get("Content-Type"); got != "application/x-ndjson" {
			t.Errorf("%s: Content-Type = %q", name, got)
		}
	}
}
