package engine_test

// The writer lease: a repository has one writer process, and a second is
// refused when it opens it (decision 0083). Two servers on one database, each
// under a data root of its own, is the arrangement the per-directory flock
// cannot see, and it is the one this refusal closes.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// The second process refuses to BOOT: the lease is taken by the boot check,
// over every repository with a row, so the refusal arrives before the second
// server has read a declaration, run an upgrade or opened a directory.
func TestASecondWriterOfARepositoryIsRefusedAtOpen(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, dsn := newDatasetWithDSN(t)
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "the first writer's"}})

	_, err := engine.OpenForTest(t, ctx, dsn,
		engine.WithKindsDir(engine.SeedKindsDir),
		engine.WithDataRoot(t.TempDir()),
		engine.WithCredentialKey(engine.TestCredentialKey))
	if err == nil {
		t.Fatal("a second writer opened the database the first one is writing")
	}
	if !errors.Is(err, engine.ErrRepositoryHasAnotherWriter) {
		t.Fatalf("the refusal must be the lease's: %v", err)
	}
	// The refusal NAMES the repository and says what the rule is, because the
	// person reading it is looking at a server that will not start.
	if !strings.Contains(err.Error(), testdb.Repository(t)) {
		t.Fatalf("the refusal does not name the repository: %v", err)
	}
	if !strings.Contains(err.Error(), "One server per database") {
		t.Fatalf("the refusal does not say the rule: %v", err)
	}
	// The first process was not disturbed by the refusal.
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "still writing"}})
}

// Closing the first process releases the lease, so the second takes it: this
// is an ordinary restart, and it must not need the pool to be torn down or a
// timeout to pass. The release is explicit (pg_advisory_unlock_all on the
// pinned connection) precisely because returning that connection to the pool
// would otherwise leave the lease held by nobody.
func TestClosingTheFirstWriterReleasesTheLease(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, dsn := newDatasetWithDSN(t)
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "before the restart"}})
	root := engine.DataRootOf(svc)
	if err := svc.Close(); err != nil {
		t.Fatalf("close the first writer: %v", err)
	}

	second := mustReopen(t, dsn, root)
	ds2, err := second.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("the second writer could not open the repository the first one released: %v", err)
	}
	mustPut(t, ds2, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "after the restart"}})
}

// A repository registered AFTER the boot check is leased too: the boot took
// what had a row then, and every later open takes its own. Without this a
// second server that booted first against an empty database would write
// every repository the first one registered.
func TestARepositoryRegisteredAfterTheBootIsLeasedAtItsOpen(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)
	// The second process boots FIRST, over a database with no repositories,
	// so its boot check leases nothing at all.
	second, err := engine.OpenForTest(t, ctx, dsn,
		engine.WithKindsDir(engine.SeedKindsDir),
		engine.WithDataRoot(t.TempDir()),
		engine.WithCredentialKey(engine.TestCredentialKey))
	if err != nil {
		t.Fatalf("open the second process over an empty database: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	first, err := engine.OpenForTest(t, ctx, dsn,
		engine.WithKindsDir(engine.SeedKindsDir),
		engine.WithDataRoot(t.TempDir()),
		engine.WithCredentialKey(engine.TestCredentialKey))
	if err != nil {
		t.Fatalf("open the first process: %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })
	if _, err := first.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}

	if _, err := second.Dataset(ctx, testdb.Repository(t)); !errors.Is(err, engine.ErrRepositoryHasAnotherWriter) {
		t.Fatalf("the second process opened a repository the first one registered and holds: %v", err)
	}
}

// A READ-ONLY process takes no lease: it exists to run beside the writer
// (`repository verify`, `reembed`), and a refusal there would take the one
// operator command that is safe against a live server away.
func TestAReadOnlyProcessNeedsNoLease(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newDatasetWithDSN(t)
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "verified beside the server"}})
	readOnly := reopenWith(t, dsn, engine.DataRootOf(svc), engine.WithDirectoryReadOnly())
	if report := mustVerify(t, readOnly, testdb.Repository(t)); !report.OK {
		t.Fatalf("a read-only process beside the writer could not verify: %+v", report)
	}
}
