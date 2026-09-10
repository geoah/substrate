package engine

import (
	"sync"
	"time"
)

// The seams a test outside this package drives: the acceptance drill in
// internal/testenv opens the engine the way substrated does and has to kill a
// boot import at a batch boundary and move the TOTP clock, which a test-only
// file (export_test.go) could not give it. Each installs a hook the
// production path already carries; none changes what the engine does when
// the hook is absent.
//
// The stepper in steptest.go is the second kind of seam and stays in its own
// file: it does not install a hook, it DRIVES a production path (one
// invocation of a callable, then the dispatcher's own effect commit,
// txn.applyEffects) with the dispatcher's bookkeeping left off. The
// distinction is what each one promises — a hook here promises the engine
// behaves as if it were absent, the stepper there promises only the write
// half it shares with production.

// WithTestTOTPClock is the clock the TOTP verifier reads (auth.go totpVerify
// callers), and nothing else: the record timestamps stay on the wall clock.
// A test that has spent one window's codes advances it one step instead of
// sleeping through a real 30 second window.
func WithTestTOTPClock(now func() time.Time) Option {
	return func(o *options) { o.now = now }
}

// TOTPPeriod is the verifier's step, for a test that moves its clock one.
const TOTPPeriod = totpPeriod

// TestClock is a TOTP clock a test hands WithTestTOTPClock: the wall clock
// plus what Advance has added, so a code spent in one window is followed by
// the next window's code without a real 30 second wait.
type TestClock struct {
	mu     sync.Mutex
	offset time.Duration
}

// Now is the wall clock plus the advance.
func (c *TestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().Add(c.offset).UTC()
}

// Advance moves the clock forward by d.
func (c *TestClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offset += d
}

// WithTestImportFault runs fn at each durable step of a boot import
// (repodir.go importEntries): after every batch of changelog rows commits,
// with ImportAfterBatch, and after the first fold pass commits, with
// ImportAfterFirstFold. An error from fn ends the boot there, which is the
// shape of a process dying at that step. A batch above zero replaces
// rebuildBatch for the import's row batches, so a short history spans
// several.
func WithTestImportFault(batch int, fn func(stage string) error) Option {
	return func(o *options) { o.importFault, o.importBatch = fn, batch }
}

// The import stages WithTestImportFault reports.
const (
	ImportAfterBatch     = importAfterBatch
	ImportAfterFirstFold = importAfterFirstFold
)

// WithTestInvokeHook runs fn with a function's identity as the runner is
// about to invoke its body (runner.go runCallableRaw): the moment a test
// that must act mid-fire (stop the server, retry it by hand) can wait for.
func WithTestInvokeHook(fn func(function string)) Option {
	return func(o *options) { o.invokeHook = fn }
}
