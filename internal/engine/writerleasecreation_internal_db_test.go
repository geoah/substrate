package engine

// A CREATION takes the repository's writer lease before it seeds anything, and
// so before the control-plane row that publishes the repository exists. That
// row is the creation's commit point and the directory is written after it, so
// a lease taken only at the directory step left a window in which another
// server could see the row, take the lease and open the repository — which
// this creation's failure would then erase out from under it.

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

// leaseKeyFree reports whether the repository's lease key can be taken from a
// session of its own, and gives it straight back when it could. A probe, not a
// claim: it must not leave the key held either way.
func leaseKeyFree(t *testing.T, conn *sql.Conn, repository string) bool {
	t.Helper()
	ctx := context.Background()
	var got bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(`+writerLeaseKeySQL+`)`, repository).Scan(&got); err != nil {
		t.Fatalf("probe the writer lease key: %v", err)
	}
	if got {
		if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_unlock(`+writerLeaseKeySQL+`)`, repository); err != nil {
			t.Fatalf("give the probed key back: %v", err)
		}
	}
	return got
}

// leaseProbe is a session of its own on the same schema: the key is composed
// with current_schema(), so the probe has to ride the same DSN.
func leaseProbe(t *testing.T, dsn string) *sql.Conn {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func repositoryRowExists(t *testing.T, conn *sql.Conn, authority string) bool {
	t.Helper()
	var n int
	if err := conn.QueryRowContext(context.Background(),
		`SELECT count(*) FROM repositories WHERE id = $1`, authority).Scan(&n); err != nil {
		t.Fatalf("read the control-plane row: %v", err)
	}
	return n > 0
}

// The lease is held DURING the creation, before the row, and a creation that
// fails hands it back.
func TestACreationLeasesItsRepositoryBeforeThatRowExists(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, dsn := openBareService(t)
	probe := leaseProbe(t, dsn)
	const authority = "alice.example.com"

	if !leaseKeyFree(t, probe, authority) {
		t.Fatal("the key is held before any creation ran")
	}

	boom := errors.New("test: the creation failed after the seed committed")
	var freeDuring, rowDuring bool
	// The seam fires after the seed and BEFORE the control-plane row, which is
	// exactly the window another process used to be able to claim.
	s.testFailAfterSeed = func() error {
		freeDuring = leaseKeyFree(t, probe, authority)
		rowDuring = repositoryRowExists(t, probe, authority)
		return boom
	}
	if _, err := s.CreateRepository(ctx, authority); !errors.Is(err, boom) {
		t.Fatalf("the seeded creation was expected to fail with the seam's error: %v", err)
	}
	if rowDuring {
		t.Fatal("the control-plane row already existed at the seam; this test is not watching the window it means to")
	}
	if freeDuring {
		t.Fatal("a creation in flight did not hold its repository's writer lease, so another process could claim it and be erased")
	}
	// The repository does not exist, so this process is not its writer.
	if !leaseKeyFree(t, probe, authority) {
		t.Fatal("a creation that failed kept its repository's writer lease")
	}

	// And the retry lands, which is the same assertion from the other side: a
	// creation cannot be refused by the lease its own failed attempt left.
	s.testFailAfterSeed = nil
	if _, err := s.CreateRepository(ctx, authority); err != nil {
		t.Fatalf("retry after the released lease: %v", err)
	}
	if leaseKeyFree(t, probe, authority) {
		t.Fatal("a repository that exists is not leased by the process that created it")
	}
}

// A second process cannot open a repository whose creation is in flight: the
// lease is the whole of the answer, and it is held before the row the other
// process would find.
func TestASecondProcessCannotClaimARepositoryMidCreation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, dsn := openBareService(t)
	const authority = "bob.example.com"

	// The second process: its own service on the same database, booted while
	// there is nothing to lease.
	secondIface, err := OpenForTest(t, ctx, dsn, WithDataRoot(t.TempDir()), WithCredentialKey(TestCredentialKey), WithKindsDir(SeedKindsDir))
	if err != nil {
		t.Fatalf("open the second process: %v", err)
	}
	t.Cleanup(func() { _ = secondIface.Close() })
	second := secondIface.(*service)

	boom := errors.New("test: the creation failed after the seed committed")
	var claim error
	s.testFailAfterSeed = func() error {
		// Mid-creation the second process asks for the lease the way its own
		// open would, and must be refused.
		claim = second.lease.acquire(ctx, authority)
		return boom
	}
	if _, err := s.CreateRepository(ctx, authority); !errors.Is(err, boom) {
		t.Fatalf("the seeded creation was expected to fail with the seam's error: %v", err)
	}
	if !errors.Is(claim, ErrRepositoryHasAnotherWriter) {
		t.Fatalf("the second process claimed a repository mid-creation: %v", claim)
	}
}
