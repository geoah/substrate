package engine

import (
	"context"
	"database/sql"
	"time"
)

// The seams a test outside this package drives: the acceptance drill in
// internal/testenv opens the engine the way substrated does and has to kill a
// boot import at a batch boundary and move the TOTP clock, which a test-only
// file (export_test.go) could not give it. Each installs a hook the
// production path already carries; none changes what the engine does when
// the hook is absent.

// WithTestTOTPClock is the clock the TOTP verifier reads (auth.go totpVerify
// callers), and nothing else: the record timestamps stay on the wall clock.
// A test that has spent one window's codes advances it one step instead of
// sleeping through a real 30 second window.
func WithTestTOTPClock(now func() time.Time) Option {
	return func(o *options) { o.now = now }
}

// TOTPPeriod is the verifier's step, for a test that moves its clock one.
const TOTPPeriod = totpPeriod

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

// ImportIncomplete reports whether the repository's import-progress marker
// is set, read through the caller's own connection.
func ImportIncomplete(ctx context.Context, db *sql.DB) (bool, error) {
	_, incomplete, err := importIncomplete(ctx, db)
	return incomplete, err
}
