package engine

// The detached-task supervisor (background.go). These hold the three
// guarantees a panic in an agent turn used to break: the process survives it
// and the log names it, shutdown waits for work that is still running, and
// work offered after shutdown started is refused rather than waited for.
// None of them needs a database — a service with a logger and a background is
// the whole subject.

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// backgroundService is a service with nothing but what spawn touches.
func backgroundService(logs *syncBuffer) *service {
	return &service{log: slog.New(slog.NewTextHandler(logs, nil)), bg: newBackground()}
}

// A PANIC IN A DETACHED TASK MUST NOT KILL THE PROCESS. These goroutines carry
// no request, so chi's Recoverer never sees them: without the recover in spawn
// a panicking judge takes down every request in flight. The log has to name the
// task, or the crash becomes an unexplained gap in the work instead.
func TestSpawnContainsAPanicAndLogsIt(t *testing.T) {
	var logs syncBuffer
	s := backgroundService(&logs)

	if !s.spawn("judge", "repo1", func(context.Context) {
		panic("the judge came apart")
	}) {
		t.Fatal("spawn refused a task on a live service")
	}
	// The drain is what makes the panic observable: it returns only once the
	// goroutine has finished unwinding through the recover.
	s.stopBackground(5 * time.Second)

	out := logs.String()
	for _, want := range []string{"panicked", "task=judge", "repository=repo1", "the judge came apart"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the panic log does not mention %q: %s", want, out)
		}
	}
}

// SHUTDOWN WAITS. Close pulls every pool down, so a task still inside a
// transaction when the wait ends is a torn write on the way out.
func TestStopBackgroundWaitsForATaskInFlight(t *testing.T) {
	var logs syncBuffer
	s := backgroundService(&logs)

	started := make(chan struct{})
	finished := make(chan struct{})
	s.spawn("resume notified thread", "repo1", func(context.Context) {
		close(started)
		time.Sleep(50 * time.Millisecond)
		close(finished)
	})
	<-started
	s.stopBackground(5 * time.Second)
	select {
	case <-finished:
	default:
		t.Fatal("stopBackground returned while a task was still running")
	}
	if strings.Contains(logs.String(), "shutdown budget") {
		t.Fatalf("a task that finished in time reported a timeout: %s", logs.String())
	}
}

// A TASK'S CONTEXT IS CANCELED AT SHUTDOWN, which is what lets the wait
// finish long before its budget: the task's own database and provider calls
// return on the cancellation.
func TestStopBackgroundCancelsTheTaskContext(t *testing.T) {
	var logs syncBuffer
	s := backgroundService(&logs)

	done := make(chan struct{})
	s.spawn("judge", "repo1", func(ctx context.Context) {
		<-ctx.Done()
		close(done)
	})
	s.stopBackground(5 * time.Second)
	select {
	case <-done:
	default:
		t.Fatal("the task's context was not canceled by stopBackground")
	}
}

// WORK OFFERED AFTER SHUTDOWN STARTED IS REFUSED. A task admitted after the
// drain decided what it was waiting for would run against closing pools; the
// caller's recovery path (the resolution sweep, the next open) picks it up.
func TestSpawnRefusesAfterShutdownStarted(t *testing.T) {
	var logs syncBuffer
	s := backgroundService(&logs)
	s.stopBackground(5 * time.Second)

	var ran bool
	var mu sync.Mutex
	if s.spawn("warm functions", "repo1", func(context.Context) {
		mu.Lock()
		ran = true
		mu.Unlock()
	}) {
		t.Fatal("spawn admitted a task after shutdown started")
	}
	// A refusal must not leave a count behind: a second drain with a budget
	// far under the wait it would need reports no timeout only if the counter
	// is where it was.
	s.stopBackground(2 * time.Second)
	if strings.Contains(logs.String(), "shutdown budget") {
		t.Fatalf("the refused task left a count behind: %s", logs.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if ran {
		t.Fatal("a refused task ran anyway")
	}
	if !strings.Contains(logs.String(), "refused") {
		t.Fatalf("the refusal was not logged: %s", logs.String())
	}
}

// THE WAIT IS BOUNDED. A task that ignores its canceled context must not hold
// the process past the orchestrator's grace period, and the give-up is logged
// so the shutdown is not silently torn.
func TestStopBackgroundGivesUpAfterItsBudget(t *testing.T) {
	var logs syncBuffer
	s := backgroundService(&logs)

	release := make(chan struct{})
	s.spawn("judge", "repo1", func(context.Context) { <-release })
	defer close(release)

	start := time.Now()
	s.stopBackground(50 * time.Millisecond)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("stopBackground waited %s past its 50ms budget", elapsed)
	}
	if !strings.Contains(logs.String(), "shutdown budget") {
		t.Fatalf("the exhausted budget was not logged: %s", logs.String())
	}
}
