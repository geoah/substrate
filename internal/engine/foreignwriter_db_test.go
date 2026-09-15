package engine_test

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// Two servers on one database, each under a data root of its own, is the
// deployment docs/operations.md rules out, and the writer lock under one data
// root cannot see it. The rows the second one commits are history the first
// one's directory lacks, and the first one's next write used to meet the seq
// gap and latch ErrChangelogFileBehind until a restart, for rows it never
// wrote (#539). The write now appends the other's rows first and lands its
// own behind them, the directory verifies at the table's head, and the other
// server's directory catches up the same way at its next write.
//
// The writer lease refuses this arrangement at open now (decision 0083), so
// the second process here takes none: the repair below is what still stands
// behind a lease a restart or a dropped connection let slip, and it must keep
// working.
func TestAWriteAppendsTheRowsAnotherProcessCommitted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, dsn := newDatasetWithDSN(t)
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "first"}})
	dir := repoDirOf(t, svc, ds)

	other, err := engine.OpenForTest(t, ctx, dsn, engine.WithKindsDir(engine.SeedKindsDir),
		engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey),
		engine.WithTestSkipWriterLease())
	if err != nil {
		t.Fatalf("a second process on the same database: %v", err)
	}
	t.Cleanup(func() { _ = other.Close() })
	ds2, err := other.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("the second process opens the repository: %v", err)
	}
	// Two transactions the first process's directory never sees.
	mustPut(t, ds2, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "second"}})
	mustPut(t, ds2, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "third"}})
	tableHead := maxSeq(t, ds)
	ro, err := changelogfile.OpenReadOnly(changelogfile.ChangelogDir(dir))
	if err != nil {
		t.Fatal(err)
	}
	if ro.Head() >= tableHead {
		t.Fatalf("the first directory is at seq %d with the table at %d; the second process's rows should be missing from it", ro.Head(), tableHead)
	}

	// The write that latched: it appends the gap and lands behind it.
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "fourth"}})
	if got := maxSeq(t, ds); got <= tableHead {
		t.Fatalf("the write did not reach the table: head %d, was %d", got, tableHead)
	}
	report := mustVerify(t, svc, testdb.Repository(t))
	if !report.OK || report.FileHead != report.Head || report.Head != maxSeq(t, ds) {
		t.Fatalf("the first directory after the write: %+v", report)
	}
	// Reads and later writes go on as if nothing happened.
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "fifth"}})

	// The other side is the same case in the other direction.
	mustPut(t, ds2, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "sixth"}})
	report2 := mustVerify(t, other, testdb.Repository(t))
	if !report2.OK || report2.FileHead != report2.Head || report2.Head != maxSeq(t, ds2) {
		t.Fatalf("the second directory after its write: %+v", report2)
	}
}
