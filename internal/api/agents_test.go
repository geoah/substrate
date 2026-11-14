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

// stubSignals is a stub whose changelog never moves: an agent stream
// subscribes to it, to read its token again when it moves (decision 0272).
type stubSignals struct{ stubDataset }

func (stubSignals) WatchSignal(context.Context) <-chan int64 { return nil }

// chatFake answers a chat by emitting its events, then returning err.
type chatFake struct {
	stubSignals
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
	stubSignals
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
	stubSignals
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
	if len(lines) != 2 || lines[1] != `{"kind":"error","error":"internal error","code":"internal"}` {
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

// continueFake answers a continue by emitting its events, then returning err.
type continueFake struct {
	stubSignals
	events []substrate.AgentEvent
	err    error
	// thread is the thread the handler asked to continue.
	thread string
}

func (c *continueFake) ContinueAgentTurn(_ context.Context, _ substrate.Actor, thread string, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	c.thread = thread
	for _, ev := range c.events {
		emit(ev)
	}
	return nil, c.err
}

const threadContinuePath = "/api/v1/substrate.reamde.dev/llm/thread/th1/continue"

// A continue streams as a chat does and takes no body; its two refusals
// answer 409 with a reason a client acts on (decision 0312).
func TestThreadContinueStreamsOrRefusesWithAReason(t *testing.T) {
	res := &substrate.AgentResult{Thread: "th1", Status: "ok", Reply: "and the rest"}
	cont := &continueFake{events: []substrate.AgentEvent{
		{Kind: substrate.AgentEventThread, Thread: "th1"},
		{Kind: substrate.AgentEventDelta, Text: "and the rest", Turn: 4},
		{Kind: substrate.AgentEventDone, Result: res},
	}}
	env := newStubEnv(t, cont)
	rec := env.do(t, http.MethodPost, threadContinuePath, stubToken, nil)
	wantStatus(t, rec, http.StatusOK)
	if got := rec.Header().Get("Content-Type"); got != "application/x-ndjson" {
		t.Fatalf("a continue's Content-Type = %q, want the ndjson stream", got)
	}
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if cont.thread != "th1" || len(lines) != 3 || lines[0] != `{"kind":"thread","thread":"th1"}` || !strings.Contains(lines[2], `"kind":"done"`) {
		t.Fatalf("continue of %q streamed %s", cont.thread, rec.Body.String())
	}

	for _, tc := range []struct {
		err    error
		reason string
	}{
		{fmt.Errorf("%w: %w: thread th1", substrate.ErrConflict, substrate.ErrThreadBusy), substrate.ReasonThreadBusy},
		{fmt.Errorf("%w: %w: thread th1", substrate.ErrConflict, substrate.ErrNothingToContinue), substrate.ReasonNothingToContinue},
	} {
		cont.events, cont.err = nil, tc.err
		rec := env.do(t, http.MethodPost, threadContinuePath, stubToken, nil)
		wantErrorCode(t, rec, http.StatusConflict, codeConflict)
		var body substrate.ErrorEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Error.Reason != tc.reason {
			t.Fatalf("a continue refused with %v carries the reason %q, want %q", tc.err, body.Error.Reason, tc.reason)
		}
	}
}

// A chat body's messageId reaches the engine as it was sent, and a message
// into a running thread says why it was refused.
func TestChatCarriesItsMessageIDAndABusyThreadSaysSo(t *testing.T) {
	chat := &chatFake{events: []substrate.AgentEvent{{Kind: substrate.AgentEventThread, Thread: "th1"}, {Kind: substrate.AgentEventIdle, Thread: "th1", Status: "ok"}}}
	env := newStubEnv(t, chat)
	rec := env.do(t, http.MethodPost, helperChatPath, stubToken, map[string]any{"thread": "th1", "message": "hi", "messageId": "c41a7e9b02"})
	wantStatus(t, rec, http.StatusOK)
	if chat.got.MessageID != "c41a7e9b02" || chat.got.Thread != "th1" {
		t.Fatalf("the engine was handed %+v", chat.got)
	}
	// A repeated send's whole answer: the thread, then the one idle line.
	if got := strings.TrimSpace(rec.Body.String()); got != `{"kind":"thread","thread":"th1"}`+"\n"+`{"kind":"idle","thread":"th1","status":"ok"}` {
		t.Fatalf("a repeated send answered %s", got)
	}

	chat.events, chat.err = nil, fmt.Errorf("%w: %w: thread th1", substrate.ErrConflict, substrate.ErrThreadBusy)
	rec = env.do(t, http.MethodPost, helperChatPath, stubToken, map[string]any{"thread": "th1", "message": "hi"})
	wantErrorCode(t, rec, http.StatusConflict, codeConflict)
	if !strings.Contains(rec.Body.String(), `"reason":"thread_busy"`) {
		t.Fatalf("a message into a running thread answered %s", rec.Body.String())
	}
}

// A messageId that names another message is refused with one reason, whether
// or not the words say which message it is: the engine says that only to a
// hand that may continue the thread the id is on, and a client acts on the
// reason, never on the words.
func TestAMessageIDThatNamesAnotherMessageIsRefusedAsTaken(t *testing.T) {
	chat := &chatFake{}
	env := newStubEnv(t, chat)
	for _, words := range []string{
		`messageId "c41a7e9b02": send the message under a new id`,
		`messageId "c41a7e9b02" names a message with another text: send the message under a new id`,
	} {
		chat.err = fmt.Errorf("%w: %w: %s", substrate.ErrConflict, substrate.ErrMessageIDTaken, words)
		rec := env.do(t, http.MethodPost, helperChatPath, stubToken, map[string]any{"message": "hi", "messageId": "c41a7e9b02"})
		wantErrorCode(t, rec, http.StatusConflict, codeConflict)
		var body substrate.ErrorEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Error.Reason != substrate.ReasonMessageIDTaken || !strings.HasSuffix(body.Error.Message, words) {
			t.Fatalf("a reused messageId answered reason %q and %q, want message_id_taken and the engine's words", body.Error.Reason, body.Error.Message)
		}
	}
}

// callFake answers an agent call with err.
type callFake struct {
	stubSignals
	err error
}

func (c *callFake) CallAgent(context.Context, string, any) (*substrate.AgentResult, error) {
	return nil, c.err
}

func (c *callFake) ChatAgent(_ context.Context, _ substrate.Actor, _ string, _ substrate.ChatInput, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	emit(substrate.AgentEvent{Kind: substrate.AgentEventThread, Thread: "th1"})
	return nil, c.err
}

const helperCallPath = "/api/v1/substrate.reamde.dev/core/agent/helper/call"

// failedRun is a run that failed with cause, as the engine returns it.
func failedRun(cause string, retryAfter int, inner error) error {
	return &substrate.AgentFailure{Cause: cause, RetryAfter: retryAfter, Err: fmt.Errorf("agent helper: llm: %w", inner)}
}

// A provider's failure on the call route is the provider's, never the
// caller's input: `unavailable` with the cause as its reason and a
// Retry-After, the vendor's own where it named one. A request the vendor
// refuses as it stands, and one past the model's window, are `validation`
// with the reason. A failure of the server's own is `internal`, in the words
// the thread keeps (decision 0315).
func TestAFailedAgentCallAnswersByItsCause(t *testing.T) {
	vendor := errors.New("the vendor's words")
	for _, tc := range []struct {
		name       string
		err        error
		status     int
		code       string
		reason     string
		retryAfter string
	}{
		{"a rate limit with the vendor's wait", failedRun(substrate.CauseProviderRateLimited, 20, vendor), 503, codeUnavailable, substrate.ReasonProviderRateLimited, "20"},
		{"a rate limit with no wait named", failedRun(substrate.CauseProviderRateLimited, 0, vendor), 503, codeUnavailable, substrate.ReasonProviderRateLimited, "30"},
		{"an account out of credit", failedRun(substrate.CauseProviderBilling, 0, vendor), 503, codeUnavailable, substrate.ReasonProviderBilling, "30"},
		{"an overloaded vendor", failedRun(substrate.CauseProviderOverloaded, 0, vendor), 503, codeUnavailable, substrate.ReasonProviderOverloaded, "30"},
		{"a vendor fault", failedRun(substrate.CauseProviderUnavailable, 0, vendor), 503, codeUnavailable, substrate.ReasonProviderUnavailable, "30"},
		{"a model the key is not offered", failedRun(substrate.CauseModelUnavailable, 0, vendor), 503, codeUnavailable, substrate.ReasonModelUnavailable, "30"},
		{"a conversation past the window", failedRun(substrate.CauseContextTooLong, 0, vendor), 422, codeValidation, substrate.ReasonContextTooLong, ""},
		{"a request the vendor refuses", failedRun(substrate.CauseProviderRejected, 0, vendor), 422, codeValidation, substrate.ReasonProviderRejected, ""},
		{"a key the vendor refuses", failedRun(substrate.CauseProviderKeyRefused, 0, substrate.ErrKeyRefused), 503, codeUnavailable, substrate.ReasonKeyRefused, "60"},
		{"the operator's allowance", failedRun(substrate.CauseAllowanceSpent, 0, substrate.ErrAllowance), 403, codeGuard, substrate.ReasonQuota, ""},
		{"a write of the run that failed", failedRun(substrate.CauseWriteFailed, 0, errors.New("the insert was refused")), 500, codeInternal, "", ""},
		{"anything else", failedRun(substrate.CauseInternal, 0, errors.New("a fault")), 500, codeInternal, "", ""},
		// Refusals before the run started keep their status and say which.
		{"the person's running turns", substrate.WithReason(fmt.Errorf("%w: 4 chat turns are already running", substrate.ErrConflict), substrate.ReasonTurnCap), 409, codeConflict, substrate.ReasonTurnCap, ""},
		{"a spend cap", substrate.WithReason(fmt.Errorf("%w: spend cap reached", substrate.ErrGuard), substrate.ReasonSpendCap), 403, codeGuard, substrate.ReasonSpendCap, ""},
		{"a row with no key", substrate.WithReason(fmt.Errorf("%w: llm/provider row declares no apiKey", substrate.ErrValidation), substrate.ReasonProviderKeyMissing), 422, codeValidation, substrate.ReasonProviderKeyMissing, ""},
		{"a reason outside the closed set", substrate.WithReason(fmt.Errorf("%w: no", substrate.ErrConflict), "made_up"), 409, codeConflict, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newStubEnv(t, &callFake{err: tc.err})
			rec := env.do(t, http.MethodPost, helperCallPath, stubToken, map[string]any{"input": "go"})
			wantErrorCode(t, rec, tc.status, tc.code)
			var body substrate.ErrorEnvelope
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Error.Reason != tc.reason {
				t.Fatalf("reason = %q, want %q: %s", body.Error.Reason, tc.reason, rec.Body.String())
			}
			if got := rec.Header().Get("Retry-After"); got != tc.retryAfter {
				t.Fatalf("Retry-After = %q, want %q", got, tc.retryAfter)
			}
			if tc.status >= 500 && tc.reason == "" && !strings.Contains(body.Error.Message, "agent helper: llm:") {
				t.Fatalf("a failed run's words were masked: %q", body.Error.Message)
			}
		})
	}
}

// What ends a stream after its 200 says which of four things it is: a turn
// that failed carries the cause its thread settled with, and nothing else
// carries one.
func TestAStreamsErrorEventSaysWhatEnded(t *testing.T) {
	last := func(t *testing.T, err error) substrate.AgentEvent {
		t.Helper()
		env := newStubEnv(t, &callFake{err: err})
		rec := env.do(t, http.MethodPost, helperChatPath, stubToken, map[string]any{"message": "hi"})
		wantStatus(t, rec, http.StatusOK)
		lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
		var ev substrate.AgentEvent
		if err := json.Unmarshal([]byte(lines[len(lines)-1]), &ev); err != nil {
			t.Fatal(err)
		}
		if ev.Kind != substrate.AgentEventError {
			t.Fatalf("the stream ended with %s", rec.Body.String())
		}
		return ev
	}
	ev := last(t, failedRun(substrate.CauseProviderRateLimited, 20, errors.New("429 Too Many Requests")))
	if ev.Cause != substrate.CauseProviderRateLimited || ev.RetryAfter != 20 || ev.Code != codeUnavailable || !strings.Contains(ev.Error, "429 Too Many Requests") {
		t.Fatalf("a failed turn's error event = %+v", ev)
	}
	ev = last(t, failedRun(substrate.CauseWriteFailed, 0, errors.New("the insert was refused")))
	if ev.Cause != substrate.CauseWriteFailed || ev.RetryAfter != 0 || ev.Code != codeInternal {
		t.Fatalf("a failed write's error event = %+v", ev)
	}
	// A reader the hub dropped: attach again, and nothing about the turn.
	ev = last(t, fmt.Errorf("%w: stream fell behind; attach again", substrate.ErrUnavailable))
	if ev.Cause != "" || ev.Code != codeUnavailable {
		t.Fatalf("a reader that fell behind = %+v, want unavailable and no cause", ev)
	}
	// A fault of the server's own while it produced the stream.
	ev = last(t, errStreamPanicked)
	if ev.Cause != "" || ev.Code != codeInternal {
		t.Fatalf("a fault = %+v, want internal and no cause", ev)
	}
}
