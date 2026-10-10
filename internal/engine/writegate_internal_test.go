package engine

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// gateState reads the write gate's counters under its mutex.
func gateState(g *writeGate) (admitted bool, queued, owners int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.admitted, len(g.queue), g.owners
}

// waitGate polls the gate until cond holds, or fails after a bound.
func waitGate(t *testing.T, g *writeGate, what string, cond func(admitted bool, queued, owners int) bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		a, q, o := gateState(g)
		if cond(a, q, o) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: the gate stayed admitted=%v queued=%d owners=%d", what, a, q, o)
		}
		time.Sleep(time.Millisecond)
	}
}

// waitGateIdle waits for no admitted background writer, no queued one and no
// owner write on its way: nothing a finished or canceled writer left behind.
func waitGateIdle(t *testing.T, g *writeGate) {
	t.Helper()
	waitGate(t, g, "idle", func(a bool, q, o int) bool { return !a && q == 0 && o == 0 })
}

type gateEntry struct {
	pass func()
	err  error
}

// enterAsync enters the gate on a goroutine of its own and delivers the
// outcome.
func enterAsync(ctx context.Context, g *writeGate, owner bool) <-chan gateEntry {
	out := make(chan gateEntry, 1)
	go func() {
		pass, err := g.enter(ctx, owner)
		out <- gateEntry{pass, err}
	}()
	return out
}

// enterNow enters the gate and fails if that waits.
func enterNow(t *testing.T, g *writeGate, owner bool) func() {
	t.Helper()
	select {
	case e := <-enterAsync(context.Background(), g, owner):
		if e.err != nil {
			t.Fatal(e.err)
		}
		return e.pass
	case <-time.After(5 * time.Second):
		t.Fatalf("an entry (owner=%v) waited at the gate", owner)
		return nil
	}
}

func admitted(t *testing.T, ch <-chan gateEntry, what string) func() {
	t.Helper()
	select {
	case e := <-ch:
		if e.err != nil {
			t.Fatalf("%s: %v", what, e.err)
		}
		return e.pass
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: not admitted", what)
		return nil
	}
}

func stillWaiting(t *testing.T, ch <-chan gateEntry, what string) {
	t.Helper()
	select {
	case e := <-ch:
		t.Fatalf("%s: admitted early (err %v)", what, e.err)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestWriteGateAdmitsBackgroundWritersOneAtATime(t *testing.T) {
	t.Parallel()
	g := &writeGate{}
	first := enterNow(t, g, false)
	second := enterAsync(context.Background(), g, false)
	waitGate(t, g, "the second writer queues", func(_ bool, q, _ int) bool { return q == 1 })
	stillWaiting(t, second, "a second background writer while the first is admitted")
	first()
	first() // idempotent: the deferred pass after the explicit one
	pass := admitted(t, second, "the second background writer once the first holds the lock")
	pass()
	waitGateIdle(t, g)
}

func TestWriteGateHoldsBackgroundWritersWhileAnOwnerWriteIsOnItsWay(t *testing.T) {
	t.Parallel()
	g := &writeGate{maxWait: time.Hour}
	background := enterNow(t, g, false)
	// An owner write does not wait for the admitted background writer, nor
	// for another owner write.
	owner := enterNow(t, g, true)
	owner2 := enterNow(t, g, true)
	queued := enterAsync(context.Background(), g, false)
	waitGate(t, g, "the background writer queues", func(_ bool, q, _ int) bool { return q == 1 })
	background()
	stillWaiting(t, queued, "a background writer while two owner writes are on their way")
	owner()
	stillWaiting(t, queued, "a background writer while one owner write is on its way")
	owner2()
	admitted(t, queued, "the background writer once the owner writes hold the lock")()
	waitGateIdle(t, g)
}

func TestWriteGateAdmitsABackgroundWriterAfterTheOwnerPassBound(t *testing.T) {
	t.Parallel()
	g := &writeGate{maxPasses: 3, maxWait: time.Hour}
	held := enterNow(t, g, true)
	queued := enterAsync(context.Background(), g, false)
	waitGate(t, g, "the background writer queues", func(_ bool, q, _ int) bool { return q == 1 })
	for i := range 2 {
		enterNow(t, g, true)()
		stillWaiting(t, queued, fmt.Sprintf("a background writer passed by %d owner writes of 3", i+1))
	}
	third := enterNow(t, g, true)
	admitted(t, queued, "the background writer once the third owner write entered")()
	third()
	held()
	waitGateIdle(t, g)
}

func TestWriteGateAdmitsABackgroundWriterAfterTheWaitBound(t *testing.T) {
	t.Parallel()
	const bound = 50 * time.Millisecond
	g := &writeGate{maxPasses: 1000, maxWait: bound}
	held := enterNow(t, g, true)
	started := time.Now()
	pass := admitted(t, enterAsync(context.Background(), g, false), "the background writer after its wait bound")
	if waited := time.Since(started); waited < bound {
		t.Fatalf("admitted after %s with an owner write on its way, under the %s bound", waited, bound)
	}
	pass()
	held()
	waitGateIdle(t, g)
}

func TestWriteGateCanceledWaiterLeavesItClean(t *testing.T) {
	t.Parallel()
	g := &writeGate{maxWait: time.Hour}

	// A canceled waiter in the middle of the queue leaves it, and the next
	// admission goes to the writer behind it.
	first := enterNow(t, g, false)
	ctx, cancel := context.WithCancel(context.Background())
	canceled := enterAsync(ctx, g, false)
	waitGate(t, g, "the first waiter queues", func(_ bool, q, _ int) bool { return q == 1 })
	behind := enterAsync(context.Background(), g, false)
	waitGate(t, g, "the second waiter queues", func(_ bool, q, _ int) bool { return q == 2 })
	cancel()
	if e := <-canceled; !errors.Is(e.err, context.Canceled) {
		t.Fatalf("a canceled waiter returned %v, want context.Canceled", e.err)
	}
	waitGate(t, g, "the canceled waiter leaves the queue", func(_ bool, q, _ int) bool { return q == 1 })
	first()
	admitted(t, behind, "the writer behind the canceled one")()
	waitGateIdle(t, g)

	// A canceled head of the queue, held back by an owner write, hands the
	// head to the next writer, which the owner's pass then admits.
	owner := enterNow(t, g, true)
	ctx, cancel = context.WithCancel(context.Background())
	head := enterAsync(ctx, g, false)
	waitGate(t, g, "the head queues", func(_ bool, q, _ int) bool { return q == 1 })
	next := enterAsync(context.Background(), g, false)
	waitGate(t, g, "the next queues", func(_ bool, q, _ int) bool { return q == 2 })
	cancel()
	if e := <-head; !errors.Is(e.err, context.Canceled) {
		t.Fatalf("a canceled head returned %v, want context.Canceled", e.err)
	}
	stillWaiting(t, next, "the next writer while the owner write is on its way")
	owner()
	admitted(t, next, "the next writer once the owner write holds the lock")()
	waitGateIdle(t, g)

	// A context that ended before the entry is refused at once and counts
	// nothing, owner or background.
	for _, isOwner := range []bool{true, false} {
		if _, err := g.enter(ctx, isOwner); !errors.Is(err, context.Canceled) {
			t.Fatalf("an entry (owner=%v) on an ended context returned %v", isOwner, err)
		}
	}
	waitGateIdle(t, g)
}

// releaseHeld gives back the admitted background writer's admission by hand,
// as its pass does. The caller holds g.mu.
func releaseHeld(g *writeGate) {
	g.admitted = false
	g.admitNext()
}

// settleAtCancel checks a waiter whose context ended around its admission:
// it went on with the admission and gives it back, or it returns
// context.Canceled. Either way the gate ends idle and admits the next writer
// at once. It reports whether the waiter went on.
func settleAtCancel(t *testing.T, g *writeGate, e gateEntry) bool {
	t.Helper()
	went := e.err == nil
	if went {
		e.pass()
	} else if !errors.Is(e.err, context.Canceled) {
		t.Fatalf("a waiter canceled at its admission returned %v, want context.Canceled", e.err)
	}
	waitGateIdle(t, g)
	enterNow(t, g, false)()
	return went
}

// A waiter admitted at the moment its context ends either goes on with the
// admission or hands it to the next writer; neither leaves the gate held.
// The admission and the cancel land together under the gate's mutex. A
// waiter parked in its select takes the case of the channel closed first, so
// cancel-then-admit hands the admission on and admit-then-cancel goes on;
// both branches must run.
func TestWriteGateHandsOnAnAdmissionItsWaiterGaveUp(t *testing.T) {
	t.Parallel()
	g := &writeGate{maxWait: time.Hour}
	var went, gaveUp int
	for i := range 200 {
		// The holder's pass is releaseHeld below.
		_ = enterNow(t, g, false)
		ctx, cancel := context.WithCancel(context.Background())
		waiter := enterAsync(ctx, g, false)
		waitGate(t, g, "the waiter queues", func(_ bool, q, _ int) bool { return q == 1 })
		g.mu.Lock()
		if i%2 == 0 {
			cancel()
			releaseHeld(g)
		} else {
			releaseHeld(g)
			cancel()
		}
		g.mu.Unlock()
		if settleAtCancel(t, g, <-waiter) {
			went++
		} else {
			gaveUp++
		}
	}
	t.Logf("admitted at their cancel: %d went on, %d handed the admission on", went, gaveUp)
	if went == 0 || gaveUp == 0 {
		t.Fatalf("of 200 waiters admitted at their cancel, %d went on and %d handed the admission on: one branch never ran", went, gaveUp)
	}
}

// The overdue timer, an admission and a cancel landing together leave the
// gate as clean as any one of them. The gate's mutex is held past the
// waiter's wait bound, so the timer fires and its branch waits for the mutex
// while the cancel lands. Even iterations admit the waiter by the holder's
// release; odd ones leave it held back by an owner write until the timer's
// branch, running after the cancel, admits it as overdue. Either way the
// waiter then meets a closed admission and an ended context together, and
// its select picks one at random.
func TestWriteGateSettlesAWaiterOverdueAdmittedAndCanceledAtOnce(t *testing.T) {
	t.Parallel()
	const wait = 10 * time.Millisecond
	g := &writeGate{maxPasses: 1000, maxWait: wait}
	var went, gaveUp int
	for i := range 60 {
		byRelease := i%2 == 0
		var owner func()
		if byRelease {
			// The holder's pass is releaseHeld below.
			_ = enterNow(t, g, false)
		} else {
			owner = enterNow(t, g, true)
		}
		ctx, cancel := context.WithCancel(context.Background())
		waiter := enterAsync(ctx, g, false)
		waitGate(t, g, "the waiter queues", func(_ bool, q, _ int) bool { return q == 1 })
		g.mu.Lock()
		time.Sleep(3 * wait)
		if byRelease {
			releaseHeld(g)
		}
		cancel()
		g.mu.Unlock()
		e := <-waiter
		if owner != nil {
			owner()
		}
		if settleAtCancel(t, g, e) {
			went++
		} else {
			gaveUp++
		}
	}
	t.Logf("overdue, admitted and canceled at once: %d went on, %d handed the admission on", went, gaveUp)
	if went == 0 || gaveUp == 0 {
		t.Fatalf("of 60 waiters overdue, admitted and canceled at once, %d went on and %d handed the admission on: one branch never ran", went, gaveUp)
	}
}
