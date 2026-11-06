package runner

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// A connect gate that never receives its listener usually means the sandbox
// stub refused the body and said why on stderr before exiting. That reason has
// to reach the caller; a bare "listener never arrived" says nothing a person
// can act on.
func TestAGateFailureCarriesTheStubsReason(t *testing.T) {
	stderr := &capBuf{cap: stderrCap}
	_, _ = stderr.Write([]byte("set filter: operation not permitted (a network body needs its socket filter)\n"))
	p := &proc{stderr: stderr, waited: make(chan struct{})}
	close(p.waited)

	err := gateFailure(p, errors.New("receive listener: EOF"), time.Second)
	for _, want := range []string{"connect gate", "receive listener: EOF", "a network body needs its socket filter"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

// A reap that does not finish must not hold the caller past the bound.
func TestAGateFailureDoesNotWaitPastItsBound(t *testing.T) {
	p := &proc{stderr: &capBuf{cap: stderrCap}, waited: make(chan struct{})}
	start := time.Now()
	err := gateFailure(p, errors.New("receive listener: timeout"), 50*time.Millisecond)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("gateFailure waited %v for a reap that never came", elapsed)
	}
	if !strings.Contains(err.Error(), "receive listener: timeout") {
		t.Fatalf("error %q lost the gate's own reason", err)
	}
}
