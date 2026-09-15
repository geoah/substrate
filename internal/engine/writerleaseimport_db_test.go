package engine_test

// The boot import of a repository DIRECTORY takes the writer lease before it
// publishes the repository's row, the same way a creation does. The row is
// what another server would see, and at that moment the changelog table is
// still empty: a second process that got the lease first would find a
// repository with no entries and write a directory out of nothing, while this
// import aborted at the lease with the restored history still on disk.

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// leaseHeld takes a repository's lease key on a session of its own and hands
// back the release, so a test can be the process that got there first.
func leaseHeld(t *testing.T, dsn, repository string) (release func()) {
	t.Helper()
	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(`+engine.WriterLeaseKeySQL+`)`, repository).Scan(&got); err != nil {
		t.Fatalf("take the lease key: %v", err)
	}
	if !got {
		t.Fatalf("the lease key of %s is already held", repository)
	}
	released := false
	release = func() {
		if released {
			return
		}
		released = true
		// Ending the session is what releases it; the pool goes with it.
		_ = conn.Close()
		_ = db.Close()
	}
	t.Cleanup(release)
	return release
}

// repositoryRows is how many control-plane rows name an authority, read from
// the tamperer's seat: the control-plane table is outside row level security.
func repositoryRows(t *testing.T, dsn, authority string) int {
	t.Helper()
	var n int
	if err := rawDB(t, dsn).QueryRow(`SELECT count(*) FROM repositories WHERE id = $1`, authority).Scan(&n); err != nil {
		t.Fatalf("count the control-plane rows: %v", err)
	}
	return n
}

// leaseKeyIsFree probes a lease key from a session of its own and gives it
// straight back when it could take it.
func leaseKeyIsFree(t *testing.T, dsn, repository string) bool {
	t.Helper()
	ctx := context.Background()
	conn, err := rawDB(t, dsn).Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	var got bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(`+engine.WriterLeaseKeySQL+`)`, repository).Scan(&got); err != nil {
		t.Fatalf("probe the lease key: %v", err)
	}
	return got
}

// The import is REFUSED and PUBLISHES NOTHING while another process holds the
// lease. The row is the ordering proof: a lease taken after the insert would
// leave the repository published, and the other process free to write a
// directory from the empty changelog table this import had not filled yet.
func TestAnImportTakesTheLeaseBeforeItPublishesTheRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, _ := newDatasetWithDSN(t)
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "restored"}})
	before := foldOf(t, ds)
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	// The directory, copied into a fresh data root beside an empty database:
	// the shape a restore has.
	root2 := copyRepositoryDir(t, root, id)
	dsn2 := engine.MigratedDSN(t)
	release := leaseHeld(t, dsn2, id)

	if _, err := reopen(t, dsn2, root2); !errors.Is(err, engine.ErrRepositoryHasAnotherWriter) {
		t.Fatalf("an import ran while another process held the repository's lease: %v", err)
	}
	if n := repositoryRows(t, dsn2, id); n != 0 {
		t.Fatalf("the refused import published %d control-plane rows; the lease must come first, or another process claims a repository whose history is still on disk", n)
	}

	// With the lease free the import lands, whole.
	release()
	svc2 := mustReopen(t, dsn2, root2)
	if n := repositoryRows(t, dsn2, id); n != 1 {
		t.Fatalf("the import published %d rows, want 1", n)
	}
	ds2, err := svc2.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open the imported repository: %v", err)
	}
	if after := foldOf(t, ds2); string(after) != string(before) {
		t.Fatalf("the imported fold is not the original\n%s", firstDifference(before, after))
	}
}

// An import that FAILS hands the lease back: the boot refuses either way, but
// the key must not be left held by a process that imported nothing, or the
// next attempt against the same directory would be refused by its own
// predecessor.
func TestAFailedImportReleasesTheLease(t *testing.T) {
	t.Parallel()
	svc, ds, _ := newDatasetWithDSN(t)
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "half imported"}})
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	root2 := copyRepositoryDir(t, root, id)
	dsn2 := engine.MigratedDSN(t)
	if !leaseKeyIsFree(t, dsn2, id) {
		t.Fatal("the key is held before any import ran")
	}

	diskFull := errors.New("test: the import failed part way")
	_, err := engine.OpenForTest(t, context.Background(), dsn2,
		engine.WithKindsDir(engine.SeedKindsDir),
		engine.WithDataRoot(root2),
		engine.WithCredentialKey(engine.TestCredentialKey),
		engine.WithTestImportFault(0, func(string) error { return diskFull }))
	if !errors.Is(err, diskFull) {
		t.Fatalf("the import was expected to fail with the seam's error: %v", err)
	}
	if !leaseKeyIsFree(t, dsn2, id) {
		t.Fatal("a failed import kept the repository's writer lease")
	}
}
