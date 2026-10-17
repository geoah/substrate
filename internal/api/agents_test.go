package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	if _, err := callable.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: threadKind, ID: "th1", Properties: map[string]any{"status": "ok"},
	}); err != nil {
		t.Fatal(err)
	}
	events := env.do(t, http.MethodGet, "/api/v1/substrate.reamde.dev/llm/thread/th1/events", tok, nil)
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

// heldChat opens a stream with its thread event and then holds it until the
// client leaves, as a detached turn still running does.
type heldChat struct {
	*fakeDataset
	attach func(emit func(substrate.AgentEvent)) (*substrate.AgentResult, error)
	stop   func() (*substrate.AgentResult, error)
}

func (c *heldChat) ChatAgent(ctx context.Context, _ substrate.Actor, _, _, _ string, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	emit(substrate.AgentEvent{Kind: substrate.AgentEventThread, Thread: "th1"})
	<-ctx.Done()
	return nil, ctx.Err()
}

func (c *heldChat) AttachAgentTurn(ctx context.Context, id string, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	if c.attach == nil {
		return c.fakeDataset.AttachAgentTurn(ctx, id, emit)
	}
	return c.attach(emit)
}

func (c *heldChat) StopAgentTurn(ctx context.Context, actor substrate.Actor, id string) (*substrate.AgentResult, error) {
	if c.stop == nil {
		return c.fakeDataset.StopAgentTurn(ctx, actor, id)
	}
	return c.stop()
}

const threadKind = "substrate.reamde.dev/llm/thread"

func putThread(t *testing.T, ds *fakeDataset, id, status string) {
	t.Helper()
	if _, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{
		Kind: threadKind, ID: id, Properties: map[string]any{"status": status},
	}); err != nil {
		t.Fatal(err)
	}
}

// With nothing running on a thread, an attach answers one idle line naming
// the thread's status and ends; an unknown thread is a 404.
func TestThreadEventsAnswersIdleWhenNothingRuns(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	putThread(t, env.svc.datasets[fakeRepository], "th1", "running")

	rec := env.do(t, http.MethodGet, "/api/v1/substrate.reamde.dev/llm/thread/th1/events", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := rec.Header().Get("Content-Type"); got != "application/x-ndjson" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"kind":"idle","thread":"th1","status":"running"}` {
		t.Fatalf("idle answer = %s", got)
	}

	rec = env.do(t, http.MethodGet, "/api/v1/substrate.reamde.dev/llm/thread/nope/events", tok, nil)
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
}

// A running turn's attach streams its events, replay and live alike, to done.
func TestThreadEventsStreamsARunningTurn(t *testing.T) {
	env := newTestEnv(t)
	res := &substrate.AgentResult{Thread: "th1", Status: "ok", Reply: "hi"}
	chat := &heldChat{fakeDataset: env.svc.datasets[fakeRepository], attach: func(emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
		emit(substrate.AgentEvent{Kind: substrate.AgentEventThread, Thread: "th1"})
		emit(substrate.AgentEvent{Kind: substrate.AgentEventDelta, Text: "hi", Turn: 3})
		emit(substrate.AgentEvent{Kind: substrate.AgentEventDone, Result: res})
		return res, nil
	}}
	env.svc.wrap = func(*fakeDataset) substrate.Dataset { return chat }
	rec := env.do(t, http.MethodGet, "/api/v1/substrate.reamde.dev/llm/thread/th1/events", env.svc.token(fakeRepository), nil)
	wantStatus(t, rec, http.StatusOK)
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if len(lines) != 3 || lines[1] != `{"kind":"delta","text":"hi","turn":3}` || !strings.Contains(lines[2], `"kind":"done"`) {
		t.Fatalf("attach stream = %s", rec.Body.String())
	}
}

// A stop answers the settled result; with nothing running it is a 409, and an
// unknown thread a 404.
func TestThreadStopAnswersTheSettledResult(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	putThread(t, env.svc.datasets[fakeRepository], "th1", "ok")
	const path = "/api/v1/substrate.reamde.dev/llm/thread/th1/stop"

	rec := env.do(t, http.MethodPost, path, tok, nil)
	wantErrorCode(t, rec, http.StatusConflict, codeConflict)
	if !strings.Contains(rec.Body.String(), "thread th1 has no running chat turn") {
		t.Fatalf("the conflict does not say why: %s", rec.Body.String())
	}
	rec = env.do(t, http.MethodPost, "/api/v1/substrate.reamde.dev/llm/thread/nope/stop", tok, nil)
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)

	chat := &heldChat{fakeDataset: env.svc.datasets[fakeRepository], stop: func() (*substrate.AgentResult, error) {
		return &substrate.AgentResult{Thread: "th1", Status: "stopped", Reason: "stopped by console", Reply: "part"}, nil
	}}
	env.svc.wrap = func(*fakeDataset) substrate.Dataset { return chat }
	rec = env.do(t, http.MethodPost, path, tok, nil)
	wantStatus(t, rec, http.StatusOK)
	var got substrate.AgentResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "stopped" || got.Reason != "stopped by console" || got.Reply != "part" {
		t.Fatalf("stop answer = %+v", got)
	}
}

// A chat stream held open past the heartbeat interval carries the idle `{}`
// line, and a server drain ends it cleanly while the turn runs on.
func TestAChatStreamHeartbeatsAndEndsOnDrain(t *testing.T) {
	prev := heartbeatInterval
	heartbeatInterval = 20 * time.Millisecond
	t.Cleanup(func() { heartbeatInterval = prev })

	svc, draining, srv := drainEnv(t)
	chat := &heldChat{fakeDataset: svc.datasets[fakeRepository]}
	svc.wrap = func(*fakeDataset) substrate.Dataset { return chat }
	body, _ := json.Marshal(map[string]any{"message": "hi"})
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/substrate.reamde.dev/core/agent/helper/chat", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+svc.token(fakeRepository))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	br := bufio.NewReader(resp.Body)
	first, err := br.ReadString('\n')
	if err != nil || !strings.Contains(first, `"kind":"thread"`) {
		t.Fatalf("first line = %q, %v", first, err)
	}
	beat, err := br.ReadString('\n')
	if err != nil || beat != "{}\n" {
		t.Fatalf("heartbeat line = %q, %v", beat, err)
	}
	close(draining)
	readToEOF(t, br, 5*time.Second)
}

// panicChat panics inside the stream's producer, after emitting events.
type panicChat struct {
	*fakeDataset
	events []substrate.AgentEvent
}

func (c *panicChat) ChatAgent(_ context.Context, _ substrate.Actor, _, _, _ string, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	for _, ev := range c.events {
		emit(ev)
	}
	panic("the dataset broke")
}

// A panic in the goroutine that runs the turn's stream is contained: the
// server keeps serving, and the request ends as an internal error, a 500
// before the first event and an error event after it.
func TestAPanickingChatStreamEndsAsAnInternalError(t *testing.T) {
	env := newTestEnv(t)
	chat := &panicChat{fakeDataset: env.svc.datasets[fakeRepository]}
	env.svc.wrap = func(*fakeDataset) substrate.Dataset { return chat }
	tok := env.svc.token(fakeRepository)
	const path = "/api/v1/substrate.reamde.dev/core/agent/helper/chat"

	rec := env.do(t, http.MethodPost, path, tok, map[string]any{"message": "hi"})
	wantErrorCode(t, rec, http.StatusInternalServerError, codeInternal)

	chat.events = []substrate.AgentEvent{{Kind: substrate.AgentEventThread, Thread: "th1"}}
	rec = env.do(t, http.MethodPost, path, tok, map[string]any{"message": "hi"})
	wantStatus(t, rec, http.StatusOK)
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if len(lines) != 2 || lines[1] != `{"kind":"error","error":"internal error"}` {
		t.Fatalf("stream after a panic = %s", rec.Body.String())
	}
}
