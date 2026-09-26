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

// A BOOT THAT HAS NOT REACHED THE LISTENER SAYS SO, on a timer. The migrations
// and the boot check's import log nothing while they work, and .mise/dev.sh
// stops a server whose log has been quiet for 30 s, so without the heartbeat
// a first boot that imports a long data root is killed mid-import every time
// (issue 568). Once the boot is done the heartbeat stops.
func TestABootLogsAHeartbeatUntilItIsDone(t *testing.T) {
	var logs syncWriter
	log := slog.New(slog.NewTextHandler(&logs, nil))
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		bootHeartbeat(log, done, time.Millisecond)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for strings.Count(logs.String(), "still booting") < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("no heartbeat while booting: %s", logs.String())
		}
		time.Sleep(time.Millisecond)
	}
	close(done)
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("the heartbeat kept running after the boot finished")
	}
	if out := logs.String(); !strings.Contains(out, "elapsed=") {
		t.Fatalf("the heartbeat does not say how long the boot has run: %s", out)
	}
}
