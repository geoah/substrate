package engine_test

// The writer lease: a repository has one writer process, and a second is
// refused when it opens it (decision 0083). Two servers on one database, each
// under a data root of its own, is the arrangement the per-directory flock
// cannot see, and it is the one this refusal closes.

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// The second process refuses to BOOT: the database's writer lock is taken
// first in the start, before any repository's lease or migration, so the
// refusal arrives before the second server has read a declaration, run an
// upgrade or opened a directory.
func TestASecondWriterOfARepositoryIsRefusedAtOpen(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, dsn := newDatasetWithDSN(t)
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "the first writer's"}})

	_, err := engine.OpenForTest(t, ctx, dsn,
		engine.WithKindsFS(engine.SeedKinds),
		engine.WithDataRoot(t.TempDir()),
		engine.WithCredentialKey(engine.TestCredentialKey))
	if err == nil {
		t.Fatal("a second writer opened the database the first one is writing")
	}
	// The first writer holds the database's writer lock, which the second
	// start meets before any repository's lease (decision record 0325). The
	// refusal is the lease's error too, so every caller that tells a running
	// server from a fault still reads it as one.
	if !errors.Is(err, engine.ErrDatabaseHasAnotherWriter) || !errors.Is(err, engine.ErrRepositoryHasAnotherWriter) {
		t.Fatalf("the refusal must be the database's writer lock's: %v", err)
	}
	// It names the lock and its holder and says what the rule is, because the
	// person reading it is looking at a server that will not start.
	if !strings.Contains(err.Error(), "(hashtext(current_schema()), hashtext('substrate|writer'))") {
		t.Fatalf("the refusal does not name the database's writer lock: %v", err)
	}
	if !strings.Contains(err.Error(), "substrate-writer-") {
		t.Fatalf("the refusal does not name the first writer's session: %v", err)
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
		engine.WithKindsFS(engine.SeedKinds),
		engine.WithDataRoot(t.TempDir()),
		engine.WithCredentialKey(engine.TestCredentialKey))
	if err != nil {
		t.Fatalf("open the second process over an empty database: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	// The second process holds the database's writer lock, so no start of
	// this release registers beside it (decision record 0325). A server of
	// a release before the lock still can, and serves what it registers
	// under the repository's lease: the first process here registers through
	// the seam that opens a second writer on purpose, and a session of its
	// own holds the new repository's lease as that server would.
	first, err := engine.OpenForTest(t, ctx, dsn,
		engine.WithKindsFS(engine.SeedKinds),
		engine.WithDataRoot(t.TempDir()),
		engine.WithCredentialKey(engine.TestCredentialKey),
		engine.WithTestSkipWriterLease())
	if err != nil {
		t.Fatalf("open the first process: %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })
	info, err := first.CreateRepository(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("create repository: %v", err)
	}
	holdRepositoryLease(t, dsn, info.ID)

	if _, err := second.Dataset(ctx, testdb.Repository(t)); !errors.Is(err, engine.ErrRepositoryHasAnotherWriter) {
		t.Fatalf("the second process opened a repository another process registered and holds: %v", err)
	}
}

// holdRepositoryLease holds id's writer lease on a session of its own for
// the rest of the test, as a server of a release before the database's
// writer lock holds a repository it serves.
func holdRepositoryLease(t *testing.T, dsn, id string) {
	t.Helper()
	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtext(current_schema() || '|writer|' || $1)::bigint)`, id); err != nil {
		t.Fatal(err)
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
