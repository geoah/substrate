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

// The agent streams' framing: what a chat or an attach writes for the
// events, the refusals and the failures a dataset hands it. What a chat runs
// is the engine's (agents_db_test.go).

const helperChatPath = "/api/v1/substrate.reamde.dev/core/agent/helper/chat"

// chatFake answers a chat by emitting its events, then returning err.
type chatFake struct {
	stubDataset
	events []substrate.AgentEvent
	err    error
	// got is the turn the handler decoded.
	got substrate.ChatInput
}

func (c *chatFake) ChatAgent(_ context.Context, _ substrate.Actor, _ string, in substrate.ChatInput, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	c.got = in
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
	const capped = "spend cap reached: agent helper spent 512 of its 500 cents in the last 24 hours (budgets.spendCentsPerDay)"
	chat := &chatFake{err: fmt.Errorf("%w: %s", substrate.ErrGuard, capped)}
	env := newStubEnv(t, chat)

	rec := env.do(t, http.MethodPost, helperChatPath, stubToken, map[string]any{"message": "hi"})
	wantErrorCode(t, rec, http.StatusForbidden, codeGuard)
	if !strings.Contains(rec.Body.String(), capped) {
		t.Fatalf("the refusal does not carry the cap's text: %s", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got == "application/x-ndjson" {
		t.Fatalf("a refusal went out as a stream: Content-Type %q", got)
	}

	chat.events = []substrate.AgentEvent{{Kind: substrate.AgentEventThread, Thread: "th1"}}
	chat.err = errors.New("llm: the transport failed")
	rec = env.do(t, http.MethodPost, helperChatPath, stubToken, map[string]any{"message": "hi"})
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

// heldChat opens a stream with its thread event and then holds it until the
// client leaves, as a detached turn still running does.
type heldChat struct {
	stubDataset
	attach func(emit func(substrate.AgentEvent)) (*substrate.AgentResult, error)
	stop   func() (*substrate.AgentResult, error)
}

func (c *heldChat) ChatAgent(ctx context.Context, _ substrate.Actor, _ string, _ substrate.ChatInput, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	emit(substrate.AgentEvent{Kind: substrate.AgentEventThread, Thread: "th1"})
	<-ctx.Done()
	return nil, ctx.Err()
}

func (c *heldChat) AttachAgentTurn(_ context.Context, _ string, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	return c.attach(emit)
}

func (c *heldChat) StopAgentTurn(context.Context, substrate.Actor, string) (*substrate.AgentResult, error) {
	return c.stop()
}

// A running turn's attach streams its events, replay and live alike, to done.
func TestThreadEventsStreamsARunningTurn(t *testing.T) {
	res := &substrate.AgentResult{Thread: "th1", Status: "ok", Reply: "hi"}
	chat := &heldChat{attach: func(emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
		emit(substrate.AgentEvent{Kind: substrate.AgentEventThread, Thread: "th1"})
		emit(substrate.AgentEvent{Kind: substrate.AgentEventDelta, Text: "hi", Turn: 3})
		emit(substrate.AgentEvent{Kind: substrate.AgentEventDone, Result: res})
		return res, nil
	}}
	env := newStubEnv(t, chat)
	rec := env.do(t, http.MethodGet, "/api/v1/substrate.reamde.dev/llm/thread/th1/events", stubToken, nil)
	wantStatus(t, rec, http.StatusOK)
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if len(lines) != 3 || lines[1] != `{"kind":"delta","text":"hi","turn":3}` || !strings.Contains(lines[2], `"kind":"done"`) {
		t.Fatalf("attach stream = %s", rec.Body.String())
	}
}

// A stop answers the settled result, whole. With nothing running the
// engine's conflict is the answer (TestThreadStopWithNothingRunningIsAConflict).
func TestThreadStopAnswersTheSettledResult(t *testing.T) {
	chat := &heldChat{stop: func() (*substrate.AgentResult, error) {
		return &substrate.AgentResult{Thread: "th1", Status: "stopped", Reason: "stopped by console", Reply: "part"}, nil
	}}
	env := newStubEnv(t, chat)
	rec := env.do(t, http.MethodPost, "/api/v1/substrate.reamde.dev/llm/thread/th1/stop", stubToken, nil)
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
// line, and a server drain ends it cleanly while the turn runs on. It shortens
// the package's heartbeatInterval, so it cannot run beside another test.
func TestAChatStreamHeartbeatsAndEndsOnDrain(t *testing.T) {
	prev := heartbeatInterval
	heartbeatInterval = 20 * time.Millisecond
	t.Cleanup(func() { heartbeatInterval = prev })

	draining := make(chan struct{})
	env := newStubEnv(t, &heldChat{}, func(c *Config) { c.Draining = draining })
	srv := httptest.NewServer(env.h)
	t.Cleanup(srv.Close)
	body, _ := json.Marshal(map[string]any{"message": "hi"})
	req, err := http.NewRequest(http.MethodPost, srv.URL+helperChatPath, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+stubToken)
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
	stubDataset
	events []substrate.AgentEvent
}

func (c *panicChat) ChatAgent(_ context.Context, _ substrate.Actor, _ string, _ substrate.ChatInput, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	for _, ev := range c.events {
		emit(ev)
	}
	panic("the dataset broke")
}

// A panic in the goroutine that runs the turn's stream is contained: the
// server keeps serving, and the request ends as an internal error, a 500
// before the first event and an error event after it.
func TestAPanickingChatStreamEndsAsAnInternalError(t *testing.T) {
	chat := &panicChat{}
	env := newStubEnv(t, chat)

	rec := env.do(t, http.MethodPost, helperChatPath, stubToken, map[string]any{"message": "hi"})
	wantErrorCode(t, rec, http.StatusInternalServerError, codeInternal)

	chat.events = []substrate.AgentEvent{{Kind: substrate.AgentEventThread, Thread: "th1"}}
	rec = env.do(t, http.MethodPost, helperChatPath, stubToken, map[string]any{"message": "hi"})
	wantStatus(t, rec, http.StatusOK)
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if len(lines) != 2 || lines[1] != `{"kind":"error","error":"internal error"}` {
		t.Fatalf("stream after a panic = %s", rec.Body.String())
	}
}

// A chat body's turn context fields reach the engine as the client sent
// them: the zone and the record paths, beside the thread and the message
// (decision 0196). The engine validates both.
func TestChatCarriesTheTimezoneAndTheContextRecords(t *testing.T) {
	chat := &chatFake{events: []substrate.AgentEvent{{Kind: substrate.AgentEventThread, Thread: "th1"}}}
	env := newStubEnv(t, chat)
	paths := []string{"ada.example.com/tasks/task/q4-budget", "ada.example.com/people/person/grace"}
	rec := env.do(t, http.MethodPost, helperChatPath, stubToken, map[string]any{
		"thread": "th1", "message": "what is due?", "timezone": "Europe/London", "context": paths,
	})
	wantStatus(t, rec, http.StatusOK)
	want := substrate.ChatInput{Thread: "th1", Message: "what is due?", Timezone: "Europe/London", Context: paths}
	if chat.got.Thread != want.Thread || chat.got.Message != want.Message || chat.got.Timezone != want.Timezone ||
		strings.Join(chat.got.Context, " ") != strings.Join(want.Context, " ") {
		t.Fatalf("the engine got %+v, want %+v", chat.got, want)
	}
}
