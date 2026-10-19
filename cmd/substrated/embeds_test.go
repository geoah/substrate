package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// fakeEmbedQueue applies up to a batch of its left jobs per call, or fails.
type fakeEmbedQueue struct {
	id     string
	left   int
	err    error
	calls  int
	onCall func()
}

func (f *fakeEmbedQueue) ProcessEmbedQueue(_ context.Context, batch int) (int, error) {
	f.calls++
	if f.onCall != nil {
		f.onCall()
	}
	if f.err != nil {
		return 0, f.err
	}
	n := min(batch, f.left)
	f.left -= n
	return n, nil
}

func (f *fakeEmbedQueue) Repository() substrate.RepositoryInfo {
	return substrate.RepositoryInfo{ID: f.id}
}

// One tick keeps taking batches, in turn over the repositories, until each
// applied nothing in its last batch: a backlog drains at the endpoint's speed
// rather than one batch a minute, a small queue finishes beside it, and a
// queue whose batch failed is not asked again until the next tick.
func TestTheEmbedDrainTakesBatchesUntilTheQueuesAreEmpty(t *testing.T) {
	t.Parallel()
	backlog := &fakeEmbedQueue{id: "backlog", left: 3*embedBatch + 8}
	small := &fakeEmbedQueue{id: "small", left: 3}
	broken := &fakeEmbedQueue{id: "broken", err: errors.New("the endpoint is down")}
	idle := &fakeEmbedQueue{id: "idle"}
	drainEmbedQueues(context.Background(), []embedQueue{backlog, small, broken, idle},
		time.Now().Add(time.Hour), time.Now)
	if backlog.left != 0 || small.left != 0 {
		t.Fatalf("left behind: backlog %d, small %d; want both drained", backlog.left, small.left)
	}
	// The backlog takes four full or partial batches and a fifth that
	// applies nothing; the small queue one and an empty one.
	for _, c := range []struct {
		q    *fakeEmbedQueue
		want int
	}{{backlog, 5}, {small, 2}, {broken, 1}, {idle, 1}} {
		if c.q.calls != c.want {
			t.Fatalf("%s was asked %d times, want %d", c.q.id, c.q.calls, c.want)
		}
	}
}

// The deadline ends the tick even with work left, so a tick never runs into
// the next one by more than the batch in flight.
func TestTheEmbedDrainStopsAtTheDeadline(t *testing.T) {
	t.Parallel()
	clock := time.Unix(0, 0)
	backlog := &fakeEmbedQueue{id: "backlog", left: 100 * embedBatch}
	backlog.onCall = func() { clock = clock.Add(20 * time.Second) }
	drainEmbedQueues(context.Background(), []embedQueue{backlog},
		clock.Add(embedTickBudget), func() time.Time { return clock })
	// Batches start at 0 s, 20 s and 40 s; at 60 s the deadline has passed.
	if backlog.calls != 3 {
		t.Fatalf("the drain took %d batches in a %s tick of 20 s batches, want 3", backlog.calls, embedTickBudget)
	}
}
