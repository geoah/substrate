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

	// The next boot clears dead reservations; a thread-bound one is not dead
	// to it. A fresh turn is scripted so a second run WOULD succeed if one
	// were allowed.
	if err := ds.clearDeadReservations(ctx); err != nil {
		t.Fatalf("boot clear: %v", err)
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

// The key is consumed at the entry: the loop's mutate tool creates records
// on a context without it, so two creates under one agent call are two
// records and not a create-key collision.
func TestIdempotencyKeyDoesNotLeakIntoTheAgentsWrites(t *testing.T) {
	t.Parallel()
	ds, fake := openAgentDataset(t)
	ctx := context.Background()
	kctx := substrate.WithIdempotencyKey(ctx, "editor-1")
	put := func(id, name string) string {
		return gqlToolArgs(t, map[string]any{
			"query": `mutation { put(input: {kind: "crew.test.dev/crew/widget", id: "` + id + `", properties: {name: "` + name + `"}}) { id } }`,
		})
	}
	fake.script("mut",
		fakeTurn{calls: []fakeCall{{"mutate", put("w-first", "first")}}},
		fakeTurn{calls: []fakeCall{{"mutate", put("w-second", "second")}}},
		fakeTurn{content: "made both"},
	)
	res, err := ds.CallAgent(kctx, crewPackage+"/editor", "make two widgets")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.Status != threadOK || res.Effects != 2 {
		t.Fatalf("result: %+v", res)
	}
	for _, id := range []string{"w-first", "w-second"} {
		if _, err := ds.Get(ctx, crewPackage+"/widget", id); err != nil {
			t.Fatalf("widget %s was not created: %v", id, err)
		}
	}
	for _, m := range threadMessages(t, ds, res.Thread) {
		if m["role"] == "tool" && m["ok"] != true {
			t.Fatalf("a mutate failed under the call's key: %v", m["content"])
		}
	}
	if n := keyRows(t, ds, "editor-1"); n != 1 {
		t.Fatalf("the key landed %d rows, want the agent call's one", n)
	}
}

// Every write to a reservation is conditional on the attempt's owner token:
// an attempt whose lease lapsed can neither drop nor overwrite the successor
// that took the row over.
func TestIdempotencyKeyStaleAttemptCannotReleaseOrSettle(t *testing.T) {
	t.Parallel()
	ds := openInternalDataset(t)
	ctx := context.Background()
	reserve := func(owner string) *idempotentCall {
		c := &idempotentCall{ds: ds, op: idemFunctionCall, key: "stale", fingerprint: "fp", owner: owner}
		owned, err := c.reserve(ctx)
		if err != nil || !owned {
			t.Fatalf("%s reserve: owned=%v err=%v", owner, owned, err)
		}
		return c
	}
	ownerOf := func() string {
		var owner string
		if err := ds.db.QueryRowContext(ctx, `SELECT owner FROM idempotency_keys WHERE key = 'stale'`).Scan(&owner); err != nil {
			t.Fatalf("read owner: %v", err)
		}
		return owner
	}
	a := reserve("attempt-a")
	// A's lease lapses; B takes the row over.
	if _, err := ds.db.ExecContext(ctx, `UPDATE idempotency_keys SET expires_at = $1 WHERE key = 'stale'`, nowUTC().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	b := reserve("attempt-b")
	if got := ownerOf(); got != "attempt-b" {
		t.Fatalf("owner after the takeover: %s", got)
	}

	// A fails late: its release must not drop B's live reservation, and its
	// settle must not claim the row.
	a.release(ctx)
	if n := keyRows(t, ds, "stale"); n != 1 || ownerOf() != "attempt-b" {
		t.Fatalf("a stale release touched the successor's row: rows=%d owner=%s", n, ownerOf())
	}
	if err := a.settle(ctx, "from a"); !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("a stale settle: %v, want ErrConflict", err)
	}
	if row, _ := idempotencyRead(ctx, ds.db, idemFunctionCall, "stale", nowUTC()); row == nil || row.settled {
		t.Fatalf("a stale settle changed the row: %+v", row)
	}
	if err := a.extendLease(ctx, nowUTC().Add(time.Hour)); !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("a stale lease extension: %v, want ErrConflict", err)
	}

	// B settles; a stale settle afterwards does not overwrite the outcome.
	if err := b.settle(ctx, "from b"); err != nil {
		t.Fatalf("b settle: %v", err)
	}
	if err := a.settle(ctx, "from a, later"); !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("a stale settle over a settled row: %v, want ErrConflict", err)
	}
	row, err := idempotencyRead(ctx, ds.db, idemFunctionCall, "stale", nowUTC())
	if err != nil || row == nil {
		t.Fatalf("read after settle: %+v %v", row, err)
	}
	var outcome string
	if err := row.replay("stale", "fp", &outcome); err != nil || outcome != "from b" {
		t.Fatalf("outcome after a stale settle: %q %v", outcome, err)
	}
}

// Retention is enforced by the reads, not the sweep: a settled row past its
// window is dead, and the next attempt under the key takes it over even
// before the sweep reclaims it.
func TestIdempotencyKeyExpiredRowIsDeadBeforeTheSweep(t *testing.T) {
	t.Parallel()
	ds := openInternalDataset(t)
	ctx := context.Background()
	const task = "samples.substrate.reamde.dev/tasks/task"
	first, err := ds.Put(substrate.WithIdempotencyKey(ctx, "stale-1"), substrate.ActorAPI,
		substrate.PutInput{Kind: task, Properties: map[string]any{"name": "first"}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := ds.db.ExecContext(ctx, `UPDATE idempotency_keys SET expires_at = $1 WHERE key = 'stale-1'`, nowUTC().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	// Live, this body would be a fingerprint conflict; dead, the key is free.
	second, err := ds.Put(substrate.WithIdempotencyKey(ctx, "stale-1"), substrate.ActorAPI,
		substrate.PutInput{Kind: task, Properties: map[string]any{"name": "second"}})
	if err != nil {
		t.Fatalf("create under an expired key: %v", err)
	}
	if second.ID == first.ID {
		t.Fatal("an expired key still answered the first attempt")
	}
	if n := keyRows(t, ds, "stale-1"); n != 1 {
		t.Fatalf("the takeover left %d rows", n)
	}
	again, err := ds.Put(substrate.WithIdempotencyKey(ctx, "stale-1"), substrate.ActorAPI,
		substrate.PutInput{Kind: task, Properties: map[string]any{"name": "second"}})
	if err != nil || again.ID != second.ID {
		t.Fatalf("the taken-over key does not answer its new attempt: %+v %v", again, err)
	}
}

// One process writes a repository at a time, so a reservation without a
// thread is dead when the repository opens; a thread-bound one is kept.
func TestIdempotencyKeyDeadReservationsClearAtOpen(t *testing.T) {
	t.Parallel()
	ds := openInternalDataset(t)
	ctx := context.Background()
	for _, key := range []string{"dead-plain", "dead-thread"} {
		c := &idempotentCall{ds: ds, op: idemAgentCall, key: key, fingerprint: "fp", owner: "gone-" + key}
		if owned, err := c.reserve(ctx); err != nil || !owned {
			t.Fatalf("reserve %s: %v %v", key, owned, err)
		}
	}
	if _, err := ds.db.ExecContext(ctx, `UPDATE idempotency_keys SET thread = 'th-1' WHERE key = 'dead-thread'`); err != nil {
		t.Fatal(err)
	}
	if err := ds.clearDeadReservations(ctx); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if n := keyRows(t, ds, "dead-plain"); n != 0 {
		t.Fatalf("a dead reservation survived the open: %d rows", n)
	}
	if n := keyRows(t, ds, "dead-thread"); n != 1 {
		t.Fatalf("a thread-bound reservation was cleared: %d rows", n)
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

// An outcome past the cap is not stored, and the repeat's refusal names the
// record the first attempt wrote so the client can read it. Serial: it
// lowers the package-level cap.
func TestIdempotencyKeyOverCapCreateNamesTheRecord(t *testing.T) {
	ds := openInternalDataset(t)
	ctx := context.Background()
	const task = "samples.substrate.reamde.dev/tasks/task"
	cap := idempotencyOutcomeCap
	idempotencyOutcomeCap = 1
	t.Cleanup(func() { idempotencyOutcomeCap = cap })

	in := substrate.PutInput{Kind: task, Properties: map[string]any{"name": "too big to keep"}}
	first, err := ds.Put(substrate.WithIdempotencyKey(ctx, "big-1"), substrate.ActorAPI, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = ds.Put(substrate.WithIdempotencyKey(ctx, "big-1"), substrate.ActorAPI, in)
	if !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("repeat of an over-cap create: %v, want ErrConflict", err)
	}
	if want := task + "/" + first.ID; !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "read the record") {
		t.Fatalf("the refusal does not name the record %s: %v", want, err)
	}
	if n := keyRows(t, ds, "big-1"); n != 1 {
		t.Fatalf("the over-cap create left %d rows", n)
	}
}
