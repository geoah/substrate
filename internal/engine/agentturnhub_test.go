package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

func TestTheReplayBufferIsBounded(t *testing.T) {
	t.Parallel()
	h := newTurnHub()
	h.limit = 10
	h.publish(substrate.AgentEvent{Kind: substrate.AgentEventThread, Thread: "th"})
	for _, text := range []string{"abcd", "efgh", "ijkl", "mnop"} {
		h.publish(substrate.AgentEvent{Kind: substrate.AgentEventDelta, Text: text, Turn: 1})
	}
	// Past the bound nothing more is retained, not even a small event.
	h.publish(substrate.AgentEvent{Kind: substrate.AgentEventDelta, Text: "q", Turn: 1})
	res := &substrate.AgentResult{Thread: "th", Status: threadOK}
	h.finish(res, nil)

	var got []string
	out, err := h.attach(context.Background(), func(ev substrate.AgentEvent) {
		got = append(got, ev.Kind+":"+ev.Text)
	})
	if err != nil || out != res {
		t.Fatalf("attach = %+v, %v", out, err)
	}
	want := "thread:,delta:abcd,delta:efgh,truncated:,done:"
	if strings.Join(got, ",") != want {
		t.Fatalf("attach events = %s, want %s", strings.Join(got, ","), want)
	}
}

func TestALiveSubscriberGetsEverythingPastTheBound(t *testing.T) {
	t.Parallel()
	h := newTurnHub()
	h.limit = 4
	h.publish(substrate.AgentEvent{Kind: substrate.AgentEventThread, Thread: "th"})
	attached := make(chan []string, 1)
	ready := make(chan struct{})
	go func() {
		var got []string
		_, _ = h.attach(context.Background(), func(ev substrate.AgentEvent) {
			got = append(got, ev.Kind+":"+ev.Text)
			if ev.Kind == substrate.AgentEventThread {
				close(ready)
			}
		})
		attached <- got
	}()
	<-ready
	for _, text := range []string{"abcd", "efgh"} {
		h.publish(substrate.AgentEvent{Kind: substrate.AgentEventDelta, Text: text})
	}
	h.finish(&substrate.AgentResult{Status: threadOK}, nil)
	if got := strings.Join(<-attached, ","); got != "thread:,delta:abcd,delta:efgh,done:" {
		t.Fatalf("live events = %s", got)
	}
}

func TestASlowSubscriberIsDroppedNotWaitedFor(t *testing.T) {
	t.Parallel()
	h := newTurnHub()
	h.publish(substrate.AgentEvent{Kind: substrate.AgentEventThread, Thread: "th"})
	block, ready := make(chan struct{}), make(chan struct{})
	ended := make(chan error, 1)
	go func() {
		first := true
		_, err := h.attach(context.Background(), func(substrate.AgentEvent) {
			if first {
				first = false
				close(ready)
			}
			<-block
		})
		ended <- err
	}()
	// Subscribed: the attach is emitting its replay.
	<-ready
	// The subscriber is stuck on its first event; publish must not block.
	for range agentTurnSubscriberBuffer + 10 {
		h.publish(substrate.AgentEvent{Kind: substrate.AgentEventDelta, Text: "x"})
	}
	close(block)
	if err := <-ended; !errors.Is(err, errAttachFellBehind) {
		t.Fatalf("a slow attach ended with %v, want errAttachFellBehind", err)
	}
}

func TestAStopBeforeTheRunContextTakesEffect(t *testing.T) {
	t.Parallel()
	h := newTurnHub()
	h.stop(substrate.ActorConsole)
	ctx, cancel := context.WithCancel(context.Background())
	h.setCancel(cancel)
	if ctx.Err() == nil {
		t.Fatal("a stop before setCancel did not cancel the run")
	}
	if h.stopper() != substrate.ActorConsole {
		t.Fatalf("stopper = %q", h.stopper())
	}
}
