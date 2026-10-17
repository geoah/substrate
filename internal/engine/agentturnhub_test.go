package engine

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

func TestTheReplayBufferIsBounded(t *testing.T) {
	t.Parallel()
	h := newTurnHub()
	// Room for two retained deltas, each its event and an 8-byte buffer.
	h.limit = 2 * (agentEventSize + 8)
	h.publish(substrate.AgentEvent{Kind: substrate.AgentEventThread, Thread: "th"})
	for i, text := range []string{"abcd", "efgh", "ijkl"} {
		h.publish(substrate.AgentEvent{Kind: substrate.AgentEventDelta, Text: text, Turn: i + 1})
	}
	// Past the bound nothing more is retained, not even a small event.
	h.publish(substrate.AgentEvent{Kind: substrate.AgentEventDelta, Text: "q", Turn: 2})
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

// Consecutive deltas of one turn are one retained event, and the bound
// counts what the hub holds: 200,000 four-byte deltas over 40 turns are 40
// events of 20,000 bytes each, not 200,000 events.
func TestTheReplayMergesDeltasAndCountsWhatItHolds(t *testing.T) {
	h := newTurnHub()
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	const n = 200_000
	for i := range n {
		h.publish(substrate.AgentEvent{Kind: substrate.AgentEventDelta, Text: "tok ", Turn: 1 + i/5000})
	}
	h.publish(substrate.AgentEvent{Kind: substrate.AgentEventToolStarted, ID: "c", Turn: 41})
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if len(h.replay) != 41 || h.overflow {
		t.Fatalf("retained %d events, overflow %v; want 41 and no overflow", len(h.replay), h.overflow)
	}
	if first := h.replay[0].event(); first.Text != strings.Repeat("tok ", 5000) || first.Turn != 1 {
		t.Fatalf("the first merged delta is %d bytes at turn %d", len(first.Text), first.Turn)
	}
	if h.bytes < 4*n || h.bytes > 2*4*n+41*agentEventSize {
		t.Fatalf("counted %d bytes for %d bytes of text", h.bytes, 4*n)
	}
	if grew := int64(after.HeapAlloc) - int64(before.HeapAlloc); grew > 3*int64(h.bytes) {
		t.Fatalf("the heap grew %d bytes against %d counted", grew, h.bytes)
	}
	runtime.KeepAlive(h)
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
	h.setRun(ctx, cancel)
	if ctx.Err() == nil {
		t.Fatal("a stop before setCancel did not cancel the run")
	}
	if h.stopper() != substrate.ActorConsole {
		t.Fatalf("stopper = %q", h.stopper())
	}
}
