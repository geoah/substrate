package engine

// The Idempotency-Key store's engine-side halves: an agent call settles its
// key in the thread's settling transaction and a repeat replays without a
// second thread; a failed attempt releases its reservation; the GC sweep
// deletes a key past its retention window.

import (
	"context"
	"errors"
	"strings"
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

// An attempt that fails before it opens a thread stores nothing: the
// reservation goes with the failure, and the retry under the same key runs.
func TestIdempotencyKeyFailedAttemptReleasesTheKey(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	ctx := substrate.WithIdempotencyKey(context.Background(), "agent-fail")

	if _, err := ds.CallAgent(ctx, crewPackage+"/nobody", "try"); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("an unknown agent: %v, want ErrNotFound", err)
	}
	if n := keyRows(t, ds, "agent-fail"); n != 0 {
		t.Fatalf("a refused attempt left %d key rows", n)
	}

	fake.script("pure", fakeTurn{content: "second time lucky"})
	res, err := ds.CallAgent(ctx, crewPackage+"/purist", "try")
	if err != nil {
		t.Fatalf("retry under the released key: %v", err)
	}
	if res.Reply != "second time lucky" {
		t.Fatalf("the retry did not run: %+v", res)
	}
	if n := keyRows(t, ds, "agent-fail"); n != 1 {
		t.Fatalf("the settled retry left %d key rows, want 1", n)
	}
}

// Once a thread exists the key is bound to it: a tool effect committed before
// the run failed (or the process died) must not run again, so a retry, even
// after the reservation's lease lapsed, opens no second thread and is pointed
// at the first.
func TestIdempotencyKeyBindsToTheThreadItOpened(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	ctx := context.Background()
	kctx := substrate.WithIdempotencyKey(ctx, "agent-thread")
	const task = "samples.substrate.reamde.dev/tasks/task"

	// Turn one commits a tool effect; turn two dies, so the run never settles.
	fake.script("root",
		fakeTurn{calls: []fakeCall{{"annotate", `{"id":"idem-annot"}`}}},
		fakeTurn{status: 500},
	)
	if _, err := ds.CallAgent(kctx, crewPackage+"/classifier", "annotate it"); err == nil {
		t.Fatal("a run whose model died answered a result")
	}
	first, err := ds.Get(ctx, task, "idem-annot")
	if err != nil {
		t.Fatalf("the tool effect did not commit: %v", err)
	}
	if n := keyRows(t, ds, "agent-thread"); n != 1 {
		t.Fatalf("the reservation of a run that opened a thread was released: %d rows", n)
	}

	// The lease lapses, as it would under a dead process. A fresh turn is
	// scripted so a second run WOULD succeed if one were allowed.
	if _, err := ds.db.ExecContext(ctx, `UPDATE idempotency_keys SET expires_at = $1 WHERE key = $2`,
		nowUTC().Add(-time.Minute), "agent-thread"); err != nil {
		t.Fatalf("lapse the lease: %v", err)
	}
	fake.script("root", fakeTurn{calls: []fakeCall{{"annotate", `{"id":"idem-annot"}`}}}, fakeTurn{content: "done"})
	_, err = ds.CallAgent(kctx, crewPackage+"/classifier", "annotate it")
	if !errors.Is(err, substrate.ErrConflict) || !strings.Contains(err.Error(), "opened agent thread") {
		t.Fatalf("retry under a thread-bound key: %v, want ErrConflict naming the thread", err)
	}
	if n := threadCountOf(t, ds, crewPackage+"/classifier"); n != 1 {
		t.Fatalf("one key opened %d threads", n)
	}
	if again := mustGetInternal(t, ds, task, "idem-annot"); again.Version != first.Version {
		t.Fatalf("the tool effect ran again: version %d -> %d", first.Version, again.Version)
	}
	if n := len(fake.requestsOf("root")); n != 2 {
		t.Fatalf("the retry reached the model: %d requests, want the first run's 2", n)
	}
	if n := keyRows(t, ds, "agent-thread"); n != 1 {
		t.Fatalf("the thread-bound reservation was taken over or released: %d rows", n)
	}
}

// The key is looked up before admission: a repeat answers the stored outcome
// after the agent's bundle was disabled, where a fresh call is refused.
func TestIdempotencyKeyAnswersAfterTheAgentIsDisabled(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	installGreeterBundle(t, ds, fake)
	const (
		abPackage = "abundle.bundles.substrate.reamde.dev/abundle"
		greeter   = abPackage + "/greeter"
	)
	ctx := context.Background()
	kctx := substrate.WithIdempotencyKey(ctx, "greet-1")

	fake.script("greet", fakeTurn{content: "hello"})
	first, err := ds.CallAgent(kctx, greeter, "hi")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if err := ds.DisableBundle(ctx, abPackage); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := ds.CallAgent(ctx, greeter, "hi"); !errors.Is(err, substrate.ErrGuard) {
		t.Fatalf("a fresh call to the disabled agent: %v, want ErrGuard", err)
	}
	again, err := ds.CallAgent(kctx, greeter, "hi")
	if err != nil {
		t.Fatalf("repeat after the disable: %v", err)
	}
	if again.Thread != first.Thread || again.Reply != "hello" {
		t.Fatalf("repeat answered %+v, want the stored %+v", again, first)
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
