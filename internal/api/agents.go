package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// The agent surfaces: the call API's agent half and chat — the same loop
// with a live client attached. Chat streams ndjson
// (one AgentEvent per line, the changes feed's conventions); the thread it
// writes is ordinary data any client re-reads through the record API.

// postAgentCall is the callable invocation API's agent half: arbitrary
// input becomes the first user message, and the response carries the final
// reply plus the thread id — the durable trace.
func (h *handler) postAgentCall(w http.ResponseWriter, r *http.Request) {
	ds := DatasetFrom(r.Context())
	var req callRequest
	if !readJSON(w, r, &req, jsonBody) {
		return
	}
	res, err := ds.CallAgent(idempotentContext(r), pathParam(r, "name"), req.Input)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// postAgentChat opens or continues a thread against an agent with one user
// message and streams the run: `Content-Type: application/x-ndjson`, one
// AgentEvent object per line: thread first, deltas and tool lifecycle as
// they happen, one done event carrying the settled AgentResult. Mode chat:
// no trigger, no cursor; mid-run state is the same loop machinery, and the
// transcript persists as thread/message records either way. The body is a
// substrate.ChatInput: `timezone` and `context` shape the turn context the
// engine renders after the message (decision 0196). The turn runs
// detached from this request (decision 0169): a client that leaves loses
// the stream, not the turn, and attaches again by thread id.
func (h *handler) postAgentChat(w http.ResponseWriter, r *http.Request) {
	ds := DatasetFrom(r.Context())
	var req substrate.ChatInput
	if !readJSON(w, r, &req, jsonBody) {
		return
	}
	actor, name := ActorFrom(r.Context()), pathParam(r, "name")
	ok, err := h.streamAgentEvents(w, r, func(ctx context.Context, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
		return ds.ChatAgent(ctx, actor, name, req, emit)
	})
	if !ok {
		writeSubstrateError(w, r, err)
	}
}

// getThreadEvents attaches to the detached chat turn running on a thread:
// the events it emitted so far, then live ones, ending with done. With no
// turn running the answer is one idle line carrying the thread row's status,
// and the stream ends; a status of `running` there is a turn this process
// does not stream (a triggered run), which the client reads as records.
func (h *handler) getThreadEvents(w http.ResponseWriter, r *http.Request) {
	ds := DatasetFrom(r.Context())
	id := pathParam(r, "id")
	ok, err := h.streamAgentEvents(w, r, func(ctx context.Context, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
		return ds.AttachAgentTurn(ctx, id, emit)
	})
	if ok {
		return
	}
	if !errors.Is(err, substrate.ErrNoRunningTurn) {
		writeSubstrateError(w, r, err)
		return
	}
	rec, err := ds.Get(r.Context(), llmPackage+"/thread", id)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	status, _ := rec.Properties["status"].(string)
	setStreamHeaders(w)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(substrate.AgentEvent{Kind: substrate.AgentEventIdle, Thread: rec.ID, Status: status})
}

// postThreadStop stops the detached chat turn running on a thread and
// answers its settled AgentResult once the settle committed.
func (h *handler) postThreadStop(w http.ResponseWriter, r *http.Request) {
	ds := DatasetFrom(r.Context())
	res, err := ds.StopAgentTurn(r.Context(), ActorFrom(r.Context()), pathParam(r, "id"))
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// errStreamPanicked ends a stream whose producer panicked. It maps to no
// sentinel, so it answers 500 `internal` before the first event and an
// `internal error` event after it.
var errStreamPanicked = errors.New("internal error")

// streamAgentEvents runs start and writes each event it emits as one ndjson
// line. The status line waits for the first event: a refusal before it (a
// spend cap, a hidden agent, a running turn, nothing to attach to) comes
// back as (false, err) for the caller to answer with its own status, which a
// 200 sent up front would turn into an error event a client cannot
// classify. Once started, one writer interleaves the events with the idle
// heartbeat, a failure travels as an error event, and a server drain ends
// the stream (the turn itself runs on).
func (h *handler) streamAgentEvents(w http.ResponseWriter, r *http.Request,
	start func(ctx context.Context, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error),
) (bool, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return false, errors.New("streaming unsupported")
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	events := make(chan substrate.AgentEvent)
	ended := make(chan error, 1)
	go func() {
		// This goroutine carries no request as far as chi's recoverer is
		// concerned: a panic here would kill the server, so it ends the
		// stream as an internal error instead.
		defer func() {
			if p := recover(); p != nil {
				slog.Error("agent stream panicked and was contained",
					append(requestLogAttrs(r), "panic", fmt.Sprint(p), "stack", string(debug.Stack()))...)
				ended <- errStreamPanicked
			}
		}()
		_, err := start(ctx, func(ev substrate.AgentEvent) {
			select {
			case events <- ev:
			case <-ctx.Done():
			}
		})
		ended <- err
	}()
	rc := http.NewResponseController(w)
	frame := func() { _ = rc.SetWriteDeadline(time.Now().Add(streamWriteDeadline)) }
	enc := json.NewEncoder(w)
	started := false
	// A drain before the first event waits for it: the refusal or the
	// stream it opens is still this request's answer.
	draining := h.draining
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case ev := <-events:
			if !started {
				started, draining = true, h.draining
				setStreamHeaders(w)
				w.WriteHeader(http.StatusOK)
			} else if leaseEnded(ctx, enc, flusher, frame) {
				// The turn runs on; the token that watched it no longer stands.
				return true, nil
			}
			frame()
			if err := enc.Encode(ev); err != nil {
				return true, nil
			}
			flusher.Flush()
		case err := <-ended:
			// emit hands each event over before start returns, so every
			// event is written by now.
			if !started {
				if err == nil {
					return false, errors.New("the stream ended before its first event")
				}
				return false, err
			}
			if err != nil && r.Context().Err() == nil {
				// The 200 status line is already gone, so the failure
				// travels as its own error event: never a done with only
				// text, which a client cannot tell from a successful (if
				// empty) settle.
				frame()
				if enc.Encode(substrate.AgentEvent{Kind: substrate.AgentEventError, Error: err.Error()}) == nil {
					flusher.Flush()
				}
			}
			return true, nil
		case <-ticker.C:
			if !started {
				continue
			}
			if leaseEnded(ctx, enc, flusher, frame) {
				return true, nil
			}
			frame()
			if _, err := w.Write([]byte("{}\n")); err != nil {
				return true, nil
			}
			flusher.Flush()
		case <-draining:
			if started {
				flusher.Flush()
				return true, nil
			}
			draining = nil
		case <-r.Context().Done():
			return true, nil
		}
	}
}

// leaseEnded ends an app token's agent stream once the token no longer
// stands (tokenlease.go), with an error event a client reads as the sign-in
// ending. The record is read at most every leaseAgentEvery.
func leaseEnded(ctx context.Context, enc *json.Encoder, flusher http.Flusher, frame func()) bool {
	err := leaseFrom(ctx).alive(ctx, leaseAgentEvery)
	if err == nil {
		return false
	}
	frame()
	if enc.Encode(substrate.AgentEvent{Kind: substrate.AgentEventError, Error: err.Error()}) == nil {
		flusher.Flush()
	}
	return true
}
