package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/vocabulary"
)

// agentsThread writes a thread of agent at status, as the engine's own hand.
func agentsThread(t *testing.T, env *dbEnv, id, agent, status string) {
	t.Helper()
	env.put(t, kindThread, id, map[string]any{
		"agent":    map[string]any{vocabulary.ReferenceValueKey: vocabulary.RecordPath(kindAgent, agent)},
		"subject":  "first",
		"mode":     "chat",
		"status":   status,
		"provider": appAgentsLLMProvider, "model": "assistant", "agentDepth": 0,
		"startedAt": time.Now().UTC().Format(time.RFC3339),
	})
}

// Both live streams ask whatever proxies them to pass each line through as it
// is written: an nginx ingress buffers a response by default, and a compressing
// intermediary holds lines back, and either turns a streamed chat reply into
// one block at the end. The chat runs a real turn on the in-process provider
// (appAgentsLLM), so it cannot run in parallel.
func TestStreamsAskProxiesNotToBuffer(t *testing.T) {
	e := newAppAgentsEnv(t, false)
	appAgentsLLM(t, e.dbEnv)

	chat := e.do(t, http.MethodPost, "/api/v1/"+corePackage+"/agent/"+url.PathEscape(e.talked)+"/chat", e.token, map[string]any{"message": "hi"})
	wantStatus(t, chat, http.StatusOK)

	srv := httptest.NewServer(e.h)
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/changes?watch=1&from=0", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+e.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("watch status = %d", resp.StatusCode)
	}
	watch := resp.Header
	agentsThread(t, e.dbEnv, "th1", e.talked, "ok")
	events := e.do(t, http.MethodGet, "/api/v1/"+llmPackage+"/thread/th1/events", e.token, nil)
	wantStatus(t, events, http.StatusOK)

	for name, h := range map[string]http.Header{"chat": chat.Header(), "watch": watch, "events": events.Header()} {
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

// With nothing running on a thread, an attach answers one idle line naming
// the thread's status and ends; an unknown thread is a 404.
func TestThreadEventsAnswersIdleWhenNothingRuns(t *testing.T) {
	t.Parallel()
	e := newAppAgentsEnv(t, false)
	agentsThread(t, e.dbEnv, "th1", e.talked, "running")

	rec := e.do(t, http.MethodGet, "/api/v1/"+llmPackage+"/thread/th1/events", e.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := rec.Header().Get("Content-Type"); got != "application/x-ndjson" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"kind":"idle","thread":"th1","status":"running"}` {
		t.Fatalf("idle answer = %s", got)
	}

	rec = e.do(t, http.MethodGet, "/api/v1/"+llmPackage+"/thread/nope/events", e.token, nil)
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
}

// A stop with nothing running on the thread is a 409 that says so, and one of
// an unknown thread a 404; a running turn's stop answers its settled result
// (TestThreadStopAnswersTheSettledResult).
func TestThreadStopWithNothingRunningIsAConflict(t *testing.T) {
	t.Parallel()
	e := newAppAgentsEnv(t, false)
	agentsThread(t, e.dbEnv, "th1", e.talked, "ok")

	rec := e.do(t, http.MethodPost, "/api/v1/"+llmPackage+"/thread/th1/stop", e.token, nil)
	wantErrorCode(t, rec, http.StatusConflict, codeConflict)
	if !strings.Contains(rec.Body.String(), "thread th1 has no running chat turn") {
		t.Fatalf("the conflict does not say why: %s", rec.Body.String())
	}
	if got := e.get(t, kindThread, "th1"); got.Properties["status"] != "ok" {
		t.Fatalf("a refused stop moved the thread to %v", got.Properties["status"])
	}
	rec = e.do(t, http.MethodPost, "/api/v1/"+llmPackage+"/thread/nope/stop", e.token, nil)
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
}
