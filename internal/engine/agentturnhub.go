package engine

import (
	"context"
	"errors"
	"sync"

	"github.com/geoah/substrate/internal/substrate"
)

// A chat turn runs detached from the request that started it (decision
// 0169): its loop publishes into a turnHub, and any number of clients attach
// to the hub by thread id, each getting the turn's events so far and then
// its live ones. The hub lives exactly as long as the loop: it is reached
// through runningThreads, which the loop leaves when it returns, and nothing
// caches a finished turn.

// agentTurnReplayBytes bounds what a hub retains for a late attacher,
// summed over each event's text, arguments, output and error. Past it the
// hub keeps the prefix it has and retains nothing further; live subscribers
// still receive everything, and the thread's `llm/message` rows hold what
// the replay dropped.
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
	replay   []substrate.AgentEvent
	bytes    int
	limit    int
	overflow bool
	subs     map[*turnSubscriber]struct{}

	// cancel ends the turn's run context; stop and the service shutdown
	// both call it, and stoppedBy is what tells the two apart.
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

// setCancel hands the hub the run context's cancel. The loop is reachable
// through runningThreads before the run context exists, so a stop that came
// first takes effect here.
func (h *turnHub) setCancel(cancel context.CancelFunc) {
	h.mu.Lock()
	h.cancel = cancel
	stopped := h.stoppedBy != ""
	h.mu.Unlock()
	if stopped {
		cancel()
	}
}

// eventBytes is what an event costs the replay bound.
func eventBytes(ev substrate.AgentEvent) int {
	return len(ev.Text) + len(ev.Args) + len(ev.Output) + len(ev.Error)
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
		n := eventBytes(ev)
		if h.bytes+n > h.limit {
			h.overflow = true
		} else {
			h.bytes += n
			h.replay = append(h.replay, ev)
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
	snapshot = append(snapshot, h.replay...)
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
