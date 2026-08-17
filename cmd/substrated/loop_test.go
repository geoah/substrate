package main

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// A PANIC IN ONE PASS MUST NOT TAKE THE PROCESS DOWN. A pass reaches the same
// agent machinery a request does: the resolution sweep continues a thread
// (SweepResolutions -> continueThread), the dispatcher delivers a trigger.
// Nothing above the loop recovers, so without the recover in pass a panic
// there ends the process for every request in flight.
func TestAPassPanicIsContainedAndTheLoopKeepsTicking(t *testing.T) {
	var logs syncWriter
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	passes := make(chan int, 4)
	n := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		loop(ctx, "resolution sweep", time.Millisecond, func(context.Context) {
			n++
			passes <- n
			panic("the sweep came apart")
		})
	}()

	// Two passes: the first proves the panic was contained, the second proves
	// the loop is still running after it.
	for range 2 {
		select {
		case <-passes:
		case <-time.After(5 * time.Second):
			t.Fatal("the loop stopped ticking after a pass panicked")
		}
	}
	cancel()
	<-done

	out := logs.String()
	for _, want := range []string{"panicked", "loop=\"resolution sweep\"", "the sweep came apart"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the panic log does not mention %q: %s", want, out)
		}
	}
}

// syncWriter is a log sink a test can read while the loop still writes.
type syncWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *syncWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}
