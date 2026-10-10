package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// chatFake answers a chat by emitting its events, then returning err.
type chatFake struct {
	*fakeDataset
	events []substrate.AgentEvent
	err    error
}

func (c *chatFake) ChatAgent(_ context.Context, _ substrate.Actor, _, _, _ string, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	for _, ev := range c.events {
		emit(ev)
	}
	return nil, c.err
}

// A chat the engine refuses before its first event answers with the
// refusal's own status and code: an agent at its spend cap is 403 `guard`,
// not a 200 stream carrying an error event. A failure after the stream began
// is still an error event on the 200 stream.
func TestChatRefusedBeforeItsFirstEventAnswersItsStatus(t *testing.T) {
	env := newTestEnv(t)
	const capped = "spend cap reached: agent helper spent 512 of its 500 cents in the last 24 hours (budgets.spendCentsPerDay)"
	chat := &chatFake{
		fakeDataset: env.svc.datasets[fakeRepository],
		err:         fmt.Errorf("%w: %s", substrate.ErrGuard, capped),
	}
	env.svc.wrap = func(*fakeDataset) substrate.Dataset { return chat }
	tok := env.svc.token(fakeRepository)
	const path = "/api/v1/substrate.reamde.dev/core/agent/helper/chat"

	rec := env.do(t, http.MethodPost, path, tok, map[string]any{"message": "hi"})
	wantErrorCode(t, rec, http.StatusForbidden, codeGuard)
	if !strings.Contains(rec.Body.String(), capped) {
		t.Fatalf("the refusal does not carry the cap's text: %s", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got == "application/x-ndjson" {
		t.Fatalf("a refusal went out as a stream: Content-Type %q", got)
	}

	chat.events = []substrate.AgentEvent{{Kind: substrate.AgentEventThread, Thread: "th1"}}
	chat.err = errors.New("llm: the transport failed")
	rec = env.do(t, http.MethodPost, path, tok, map[string]any{"message": "hi"})
	wantStatus(t, rec, http.StatusOK)
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	var last substrate.AgentEvent
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || last.Kind != substrate.AgentEventError || !strings.Contains(last.Error, "transport failed") {
		t.Fatalf("a failure after the first event: %s", rec.Body.String())
	}
}

// Both live streams ask whatever proxies them to pass each line through as it
// is written: an nginx ingress buffers a response by default, and a compressing
// intermediary holds lines back, and either turns a streamed chat reply into
// one block at the end.
func TestStreamsAskProxiesNotToBuffer(t *testing.T) {
	env := newTestEnv(t)
	// A chat that runs: the stream's headers go out with its first event.
	callable := &callableFake{fakeDataset: env.svc.datasets[fakeRepository], keys: map[string]string{}}
	env.svc.wrap = func(*fakeDataset) substrate.Dataset { return callable }
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
