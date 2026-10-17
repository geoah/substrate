package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"unsafe"

	"github.com/geoah/substrate/internal/substrate"
)

// A chat turn runs detached from the request that started it (decision
// 0169): its loop publishes into a turnHub, and any number of clients attach
// to the hub by thread id, each getting the turn's events so far and then
// its live ones. The hub lives exactly as long as the loop: it is reached
// through runningThreads, which the loop leaves when it returns, and nothing
// caches a finished turn.

// agentTurnReplayBytes bounds the memory a hub retains for a late attacher:
// each retained event's own size plus its text, arguments, output and error.
// Consecutive deltas of one turn are retained as one event, so a reply
// streamed a token at a time costs its text, not an event per token. Past
// the bound the hub keeps the prefix it has and retains nothing further;
// live subscribers still receive everything, and the thread's
// `llm/message` rows hold what the replay dropped.
const agentTurnReplayBytes = 4 << 20

// agentTurnSubscriberBuffer is how many events a subscriber may fall behind
// before the hub drops it. publish never blocks the loop on a slow client.
const agentTurnSubscriberBuffer = 256

// errAttachFellBehind ends an attach whose client read slower than the turn
// wrote: the hub dropped it rather than hold the loop.
var errAttachFellBehind = errors.New("stream fell behind; attach again")

type turnSubscriber struct {
	ch      chan substrate.AgentEvent
	dropped bool
}

// turnHub is one detached turn's fan-out.
type turnHub struct {
	mu sync.Mutex
	// thread is the opening event, kept apart from the replay so an attach
	// always starts with it whatever the buffer dropped.
	thread   *substrate.AgentEvent
	replay   []replayEntry
	bytes    int
	limit    int
	overflow bool
	subs     map[*turnSubscriber]struct{}

	// cancel ends the turn's run context; stop and the service shutdown
	// both call it, and stoppedBy is what tells the two apart.
	run       context.Context
	cancel    context.CancelFunc
	stoppedBy substrate.Actor

	finished bool
	result   *substrate.AgentResult
	err      error
	done     chan struct{}
}

func newTurnHub() *turnHub {
	return &turnHub{
		limit: agentTurnReplayBytes,
		subs:  map[*turnSubscriber]struct{}{},
		done:  make(chan struct{}),
	}
}

// setRun hands the hub the run context and its cancel. The loop is reachable
// through runningThreads before the run context exists, so a stop that came
// first takes effect here.
func (h *turnHub) setRun(run context.Context, cancel context.CancelFunc) {
	h.mu.Lock()
	h.run, h.cancel = run, cancel
	stopped := h.stoppedBy != ""
	h.mu.Unlock()
	if stopped {
		cancel()
	}
}

// replayEntry is one retained event. A delta's text grows in a builder as
// later deltas of the same turn merge into it.
type replayEntry struct {
	ev   substrate.AgentEvent
	text *strings.Builder
}

func (e replayEntry) event() substrate.AgentEvent {
	ev := e.ev
	if e.text != nil {
		ev.Text = e.text.String()
	}
	return ev
}

// agentEventSize is what one retained event costs before its strings.
var agentEventSize = int(unsafe.Sizeof(substrate.AgentEvent{})) + int(unsafe.Sizeof(replayEntry{}))

// eventBytes is what retaining an event costs the replay bound.
func eventBytes(ev substrate.AgentEvent) int {
	return agentEventSize + len(ev.Text) + len(ev.Args) + len(ev.Output) + len(ev.Error)
}

// retain adds ev to the replay, merging a delta into the delta before it
// when both belong to one turn. It reports false when the bound would be
// passed.
func (h *turnHub) retain(ev substrate.AgentEvent) bool {
	if n := len(h.replay); ev.Kind == substrate.AgentEventDelta && n > 0 {
		last := &h.replay[n-1]
		if last.text != nil && last.ev.Turn == ev.Turn {
			before := last.text.Cap()
			if h.bytes+max(before, last.text.Len()+len(ev.Text))-before > h.limit {
				return false
			}
			last.text.WriteString(ev.Text)
			h.bytes += last.text.Cap() - before
			return true
		}
	}
	n := eventBytes(ev)
	if h.bytes+n > h.limit {
		return false
	}
	h.bytes += n
	entry := replayEntry{ev: ev}
	if ev.Kind == substrate.AgentEventDelta {
		entry.text = &strings.Builder{}
		entry.text.WriteString(ev.Text)
		entry.ev.Text = ""
		h.bytes += entry.text.Cap() - len(ev.Text)
	}
	h.replay = append(h.replay, entry)
	return true
}

// publish is the loop's emit. The terminal done event is not published: finish
// carries the result, and each attach writes its own done from it.
func (h *turnHub) publish(ev substrate.AgentEvent) {
	if ev.Kind == substrate.AgentEventDone {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.finished {
		return
	}
	switch {
	case ev.Kind == substrate.AgentEventThread:
		h.thread = &ev
	case h.overflow:
	default:
		if !h.retain(ev) {
			h.overflow = true
		}
	}
	for sub := range h.subs {
		select {
		case sub.ch <- ev:
		default:
			sub.dropped = true
			close(sub.ch)
			delete(h.subs, sub)
		}
	}
}

// finish settles the hub with the loop's outcome and ends every attach.
func (h *turnHub) finish(res *substrate.AgentResult, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.finished {
		return
	}
	h.finished, h.result, h.err = true, res, err
	for sub := range h.subs {
		close(sub.ch)
		delete(h.subs, sub)
	}
	close(h.done)
}

// stop records who asked and cancels the turn. The first stopper is the one
// the reason names.
func (h *turnHub) stop(actor substrate.Actor) {
	h.mu.Lock()
	if h.stoppedBy == "" {
		h.stoppedBy = actor
	}
	cancel := h.cancel
	h.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// ended reports whether the turn's run context ended: a stop or the
// shutdown, never the loop's own deadline, which sits below it.
func (h *turnHub) ended() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.run != nil && h.run.Err() != nil
}

func (h *turnHub) stopper() substrate.Actor {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stoppedBy
}

// wait blocks until the turn settled or ctx ended.
func (h *turnHub) wait(ctx context.Context) (*substrate.AgentResult, error) {
	select {
	case <-h.done:
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.result, h.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// attach streams the turn through emit: the thread event, the retained
// replay, a truncated frame when the replay overflowed, then live events,
// then done. It returns the settled result, the loop's error, ctx.Err() when
// the client left first, or errAttachFellBehind.
func (h *turnHub) attach(ctx context.Context, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	if emit == nil {
		emit = func(substrate.AgentEvent) {}
	}
	h.mu.Lock()
	var snapshot []substrate.AgentEvent
	if h.thread != nil {
		snapshot = append(snapshot, *h.thread)
	}
	for _, e := range h.replay {
		snapshot = append(snapshot, e.event())
	}
	if h.overflow {
		snapshot = append(snapshot, substrate.AgentEvent{Kind: substrate.AgentEventTruncated})
	}
	var sub *turnSubscriber
	if !h.finished {
		sub = &turnSubscriber{ch: make(chan substrate.AgentEvent, agentTurnSubscriberBuffer)}
		h.subs[sub] = struct{}{}
	}
	h.mu.Unlock()
	if sub != nil {
		defer h.unsubscribe(sub)
	}
	for _, ev := range snapshot {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		emit(ev)
	}
	if sub != nil {
	live:
		for {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case ev, open := <-sub.ch:
				if !open {
					break live
				}
				emit(ev)
			}
		}
		h.mu.Lock()
		dropped := sub.dropped
		h.mu.Unlock()
		if dropped {
			return nil, errAttachFellBehind
		}
	}
	res, err := h.wait(ctx)
	if err != nil {
		return nil, err
	}
	if res != nil {
		emit(substrate.AgentEvent{Kind: substrate.AgentEventDone, Result: res})
	}
	return res, nil
}

func (h *turnHub) unsubscribe(sub *turnSubscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[sub]; ok {
		delete(h.subs, sub)
		close(sub.ch)
	}
}
