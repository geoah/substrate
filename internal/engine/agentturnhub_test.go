package engine

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

func TestTheReplayBufferIsBounded(t *testing.T) {
	t.Parallel()
	h := newTurnHub()
	// Room for two retained deltas, each its event and its 4-byte text.
	h.limit = 2 * (agentEventSize + 4)
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
	var last substrate.AgentEvent
	go func() {
		first := true
		res, err := h.attach(context.Background(), func(ev substrate.AgentEvent) {
			if first {
				first = false
				close(ready)
			}
			<-block
			last = ev
		})
		if res != nil {
			err = errors.New("a dropped attach answered a result")
		}
		ended <- err
	}()
	// Subscribed: the attach is emitting its replay.
	<-ready
	// The subscriber is stuck on its first event; publish must not block.
	for range agentTurnSubscriberBuffer + 10 {
		h.publish(substrate.AgentEvent{Kind: substrate.AgentEventDelta, Text: "x"})
	}
	close(block)
	// Falling behind is no failure of the turn: the attach ends with its own
	// event, naming the thread to attach to again, and no error.
	if err := <-ended; err != nil {
		t.Fatalf("a slow attach ended with %v, want no error", err)
	}
	if last.Kind != substrate.AgentEventLagged || last.Thread != "th" {
		t.Fatalf("a slow attach ended on %+v, want the lagged event", last)
	}
}

// at is a position for an event.
func at(n int) *int { return &n }

// applyAt writes a piece into text at its position, counted in code points,
// as a client does: over what is there, never emptying it first.
func applyAt(text []rune, ev substrate.AgentEvent) []rune {
	piece := []rune(ev.Text)
	end := *ev.At + len(piece)
	for len(text) < end {
		text = append(text, ' ')
	}
	copy(text[*ev.At:], piece)
	return text
}

// A replay holds a turn's consecutive pieces as one event at the first
// piece's position, so a client that already shows part of the turn writes
// the replay over it and the live pieces after it, and reads the same text
// as one that saw every piece.
func TestAReplayedTurnIsPlacedByPosition(t *testing.T) {
	t.Parallel()
	h := newTurnHub()
	h.publish(substrate.AgentEvent{Kind: substrate.AgentEventThread, Thread: "th"})
	pieces := []string{"The balcony ", "plants 🌿 need ", "wa", "ter twice a week."}
	var live []substrate.AgentEvent
	pos := 0
	for _, p := range pieces {
		ev := substrate.AgentEvent{Kind: substrate.AgentEventDelta, Turn: 3, Text: p, At: at(pos)}
		pos += len([]rune(p))
		live = append(live, ev)
	}
	// A reasoning piece between two runs of text counts on its own.
	h.publish(substrate.AgentEvent{Kind: substrate.AgentEventReasoning, Turn: 3, Text: "thinking", At: at(0)})
	for _, ev := range live[:3] {
		h.publish(ev)
	}
	// A client that saw the first two pieces attaches again mid-turn.
	shown := applyAt(applyAt(nil, live[0]), live[1])
	done := make(chan []substrate.AgentEvent, 1)
	go func() {
		var got []substrate.AgentEvent
		_, _ = h.attach(context.Background(), func(ev substrate.AgentEvent) { got = append(got, ev) })
		done <- got
	}()
	waitSubscribed(t, h)
	h.publish(live[3])
	h.finish(&substrate.AgentResult{Thread: "th"}, nil)
	var replayed int
	for _, ev := range <-done {
		if ev.Kind != substrate.AgentEventDelta {
			continue
		}
		if ev.At == nil || ev.Turn != 3 {
			t.Fatalf("a delta carries no position: %+v", ev)
		}
		replayed++
		shown = applyAt(shown, ev)
	}
	// The three retained pieces came back as one event at 0, then the live one.
	if replayed != 2 {
		t.Fatalf("the attach saw %d delta events, want the merged replay and one live piece", replayed)
	}
	if want := "The balcony plants 🌿 need water twice a week."; string(shown) != want {
		t.Fatalf("placed by position the turn reads %q, want %q", string(shown), want)
	}
}

// waitSubscribed blocks until an attach is subscribed to h.
func waitSubscribed(t *testing.T, h *turnHub) {
	t.Helper()
	for range 2000 {
		h.mu.Lock()
		n := len(h.subs)
		h.mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no attach subscribed")
}

func TestAStopBeforeTheRunContextTakesEffect(t *testing.T) {
	t.Parallel()
	h := newTurnHub()
	h.stop(substrate.ActorPerson)
	ctx, cancel := context.WithCancel(context.Background())
	h.setRun(ctx, cancel)
	if ctx.Err() == nil {
		t.Fatal("a stop before setCancel did not cancel the run")
	}
	if h.stopper() != substrate.ActorPerson {
		t.Fatalf("stopper = %q", h.stopper())
	}
}

// Reasoning pieces merge like deltas, each kind on its own: a turn that
// thinks, answers, then thinks again replays as three events, in order.
func TestTheReplayMergesReasoningApartFromTheReply(t *testing.T) {
	t.Parallel()
	h := newTurnHub()
	for _, ev := range []substrate.AgentEvent{
		{Kind: substrate.AgentEventReasoning, Text: "think ", Turn: 1},
		{Kind: substrate.AgentEventReasoning, Text: "more", Turn: 1},
		{Kind: substrate.AgentEventDelta, Text: "say ", Turn: 1},
		{Kind: substrate.AgentEventDelta, Text: "it", Turn: 1},
		{Kind: substrate.AgentEventReasoning, Text: "again", Turn: 1},
		{Kind: substrate.AgentEventReasoning, Text: "next turn", Turn: 3},
	} {
		h.publish(ev)
	}
	var got []string
	for _, e := range h.replay {
		ev := e.event()
		got = append(got, fmt.Sprintf("%s:%d:%s", ev.Kind, ev.Turn, ev.Text))
	}
	want := "reasoning:1:think more,delta:1:say it,reasoning:1:again,reasoning:3:next turn"
	if strings.Join(got, ",") != want {
		t.Fatalf("replay = %s, want %s", strings.Join(got, ","), want)
	}
}

// Merging a delta grows its buffer to twice its size and more: the bound is
// checked against the grown buffer, so the hub never holds past its limit.
func TestAMergedDeltaNeverGrowsPastTheBound(t *testing.T) {
	t.Parallel()
	h := newTurnHub()
	h.limit = agentEventSize + 100
	for range 200 {
		h.publish(substrate.AgentEvent{Kind: substrate.AgentEventDelta, Text: "abc", Turn: 1})
		if h.bytes > h.limit {
			t.Fatalf("the hub holds %d bytes against a limit of %d", h.bytes, h.limit)
		}
	}
	if !h.overflow {
		t.Fatal("600 bytes of text fit a 100-byte bound")
	}
	if got := len(h.replay[0].event().Text); got == 0 || got > 100 {
		t.Fatalf("retained %d bytes of text", got)
	}
}
