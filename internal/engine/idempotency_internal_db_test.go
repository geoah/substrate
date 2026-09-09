package engine

// The Idempotency-Key store's engine-side halves: an agent call settles its
// key in the thread's settling transaction and a repeat replays without a
// second thread; a failed attempt releases its reservation; the GC sweep
// deletes a key past its retention window.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

func keyRows(t *testing.T, ds *dataset, key string) int {
	t.Helper()
	var n int
	if err := ds.db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM idempotency_keys WHERE key = $1`, key).Scan(&n); err != nil {
		t.Fatalf("count key rows: %v", err)
	}
	return n
}

func TestIdempotencyKeyAgentCallRunsOnce(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	// One scripted turn: a second run would find the script exhausted and
	// fail, so a repeat that answers is a repeat that did not run.
	fake.script("pure", fakeTurn{content: "measured"})
	ctx := substrate.WithIdempotencyKey(context.Background(), "agent-1")

	first, err := ds.CallAgent(ctx, crewPackage+"/purist", "how long is this")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	second, err := ds.CallAgent(ctx, crewPackage+"/purist", "how long is this")
	if err != nil {
		t.Fatalf("repeat: %v", err)
	}
	if second.Thread != first.Thread || second.Reply != first.Reply || second.Status != first.Status {
		t.Fatalf("repeat answered %+v, want the first run's %+v", second, first)
	}
	if n := threadCountOf(t, ds, crewPackage+"/purist"); n != 1 {
		t.Fatalf("two calls under one key opened %d threads", n)
	}
	if n := len(fake.requestsOf("pure")); n != 1 {
		t.Fatalf("the repeat reached the model: %d requests", n)
	}
	_, err = ds.CallAgent(ctx, crewPackage+"/purist", "something else")
	if !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("same key, different input: %v, want ErrConflict", err)
	}
}

// A failed attempt stores nothing: the reservation goes with the failure, and
// the retry under the same key runs the operation again.
func TestIdempotencyKeyFailedAttemptReleasesTheKey(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	ctx := substrate.WithIdempotencyKey(context.Background(), "agent-fail")

	fake.script("pure", fakeTurn{status: 500})
	if _, err := ds.CallAgent(ctx, crewPackage+"/purist", "try"); err == nil {
		t.Fatal("a failed model turn answered a result")
	}
	if n := keyRows(t, ds, "agent-fail"); n != 0 {
		t.Fatalf("a failed attempt left %d key rows", n)
	}

	fake.script("pure", fakeTurn{content: "second time lucky"})
	res, err := ds.CallAgent(ctx, crewPackage+"/purist", "try")
	if err != nil {
		t.Fatalf("retry after a failure: %v", err)
	}
	if res.Reply != "second time lucky" {
		t.Fatalf("the retry did not run: %+v", res)
	}
	if n := keyRows(t, ds, "agent-fail"); n != 1 {
		t.Fatalf("the settled retry left %d key rows, want 1", n)
	}
}

func TestIdempotencyKeyExpiredIsSwept(t *testing.T) {
	t.Parallel()
	ds := openInternalDataset(t)
	ctx := context.Background()
	in := substrate.PutInput{Kind: "samples.substrate.reamde.dev/tasks/task", Properties: map[string]any{"name": "aging"}}
	first, err := ds.Put(substrate.WithIdempotencyKey(ctx, "old-1"), substrate.ActorAPI, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// A live key is not the sweep's: the retention window has not passed.
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc: %v", err)
	}
	if n := keyRows(t, ds, "old-1"); n != 1 {
		t.Fatalf("the sweep took a live key: %d rows", n)
	}

	// Age the row past its window, as the clock would.
	if _, err := ds.db.ExecContext(ctx, `UPDATE idempotency_keys SET expires_at = $1 WHERE key = $2`,
		nowUTC().Add(-time.Minute), "old-1"); err != nil {
		t.Fatalf("age the key: %v", err)
	}
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc: %v", err)
	}
	if n := keyRows(t, ds, "old-1"); n != 0 {
		t.Fatalf("the sweep left an expired key: %d rows", n)
	}
	// A swept key is a fresh key: the same body creates again.
	again, err := ds.Put(substrate.WithIdempotencyKey(ctx, "old-1"), substrate.ActorAPI, in)
	if err != nil {
		t.Fatalf("create after the sweep: %v", err)
	}
	if again.ID == first.ID {
		t.Fatal("a swept key still answered the first attempt")
	}
}
