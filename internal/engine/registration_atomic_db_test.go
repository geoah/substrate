package engine

// A registration that fails — or crashes — must leave nothing durable behind.
// The repository's own rows commit FIRST and the control-plane row LAST, so
// the guarantees are: a RETURNED failure erases the scope (transactionally, on
// a context that outlives the request), and a CRASH in the commit window is
// reclaimed by a boot sweep of rows whose repository has no control-plane row.
// These INTERNAL tests drive those seams directly.

import (
	"context"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/testdb"
)

func openBareService(t *testing.T) (*service, string) {
	t.Helper()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	svcIface, err := Open(ctx, dsn, WithDataRoot(t.TempDir()), WithCredentialKey(TestCredentialKey), WithKindsDir("../../kinds/substrate.reamde.dev/core"))
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svcIface.Close() })
	return svcIface.(*service), dsn
}

// orphanRowCount is how many repository-scoped rows belong to a repository with
// no control-plane row — the durable residue a failed or crashed registration
// must never leave.
func orphanRowCount(t *testing.T, s *service) int {
	t.Helper()
	ctx := context.Background()
	total := 0
	for _, table := range repositoryScopedTables {
		var n int
		if err := s.maint.QueryRowContext(ctx,
			`SELECT count(*) FROM `+table+` WHERE repository NOT IN (SELECT id FROM repositories)`).Scan(&n); err != nil {
			t.Fatalf("count orphans in %s: %v", table, err)
		}
		total += n
	}
	return total
}

// plantOrphan writes repository-scoped rows under an id that has NO
// control-plane row — exactly what a registration crash between the scoped
// commit and the control-plane insert leaves. It writes on a scoped app pool,
// so the rows land through the same policy a real registration's do.
func plantOrphan(t *testing.T, dsn, id string) {
	t.Helper()
	raw, err := OpenScopedDB(dsn, id, roleApp)
	if err != nil {
		t.Fatalf("open scoped pool: %v", err)
	}
	defer func() { _ = raw.Close() }()
	if _, err := raw.Exec(
		`INSERT INTO records (kind, id, title) VALUES ($1, $2, $3)`,
		"samples.substrate.reamde.dev/tasks/task", "orphan-rec", "nowhere"); err != nil {
		t.Fatalf("plant orphan record: %v", err)
	}
	if _, err := raw.Exec(
		`INSERT INTO sealed (ref, record_kind, record_id, payload, updated_at) VALUES ($1, $2, $3, $4, now())`,
		"auth:password:orphan", "substrate.reamde.dev/core/credential", "self", []byte("secret")); err != nil {
		t.Fatalf("plant orphan sealed: %v", err)
	}
}

// TestFailedRegistrationLeavesNoDurableRows forces a failure in the commit
// window (after the seed, before the control-plane row) and asserts nothing
// durable survives — and that a retry with the SAME username then succeeds.
func TestFailedRegistrationLeavesNoDurableRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := openBareService(t)
	s.testFailAfterSeed = func() error { return errors.New("boom after the seed committed") }

	if _, err := s.CreateRepository(ctx, "alice", "alice.example.com"); err == nil {
		t.Fatal("a registration that failed after the seed reported success")
	}
	if n := orphanRowCount(t, s); n != 0 {
		t.Fatalf("the failed registration left %d durable rows; it must create nothing", n)
	}

	// The retry lands cleanly: the orphan carried no control-plane row, so the
	// username was never taken.
	s.testFailAfterSeed = nil
	if _, err := s.CreateRepository(ctx, "alice", "alice.example.com"); err != nil {
		t.Fatalf("retry with the same username: %v", err)
	}
	if _, err := s.repositoryByUsername(ctx, "alice"); err != nil {
		t.Fatalf("alice is not registered after the retry: %v", err)
	}
	if n := orphanRowCount(t, s); n != 0 {
		t.Fatalf("after a clean retry there are still %d orphan rows", n)
	}
}

// TestEraseRepositorySurvivesRequestCancellation: a registration usually fails
// BECAUSE the client went away, so the cleanup must run on a context decoupled
// from the request's. Erasing under an already-canceled context must still
// wipe the scope.
func TestEraseRepositorySurvivesRequestCancellation(t *testing.T) {
	t.Parallel()
	s, dsn := openBareService(t)
	plantOrphan(t, dsn, "orphanrepoidcancel")
	if orphanRowCount(t, s) == 0 {
		t.Fatal("setup: no orphan rows planted")
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel() // the request that triggered the failure is already gone
	if err := s.eraseRepository(canceled, "orphanrepoidcancel"); err != nil {
		t.Fatalf("erase under a canceled context: %v", err)
	}
	if n := orphanRowCount(t, s); n != 0 {
		t.Fatalf("erase under a canceled context left %d rows; the cleanup must outlive the request", n)
	}
}

// TestBootSweepReclaimsOrphanedRows: the sweep that runs at Open reclaims a
// crashed registration's orphan rows and leaves a legitimate repository's rows
// untouched.
func TestBootSweepReclaimsOrphanedRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, dsn := openBareService(t)

	if _, err := s.CreateRepository(ctx, "legit", "legit.example.com"); err != nil {
		t.Fatalf("create legit repository: %v", err)
	}
	legit, err := s.repositoryByUsername(ctx, "legit")
	if err != nil {
		t.Fatal(err)
	}
	var legitBefore int
	if err := s.maint.QueryRowContext(ctx,
		`SELECT count(*) FROM records WHERE repository = $1`, legit.ID).Scan(&legitBefore); err != nil {
		t.Fatal(err)
	}
	if legitBefore == 0 {
		t.Fatal("setup: the legit repository has no records")
	}

	plantOrphan(t, dsn, "orphanrepoidsweep")
	if orphanRowCount(t, s) == 0 {
		t.Fatal("setup: no orphan rows planted")
	}

	if err := s.sweepOrphans(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n := orphanRowCount(t, s); n != 0 {
		t.Fatalf("the sweep left %d orphan rows", n)
	}
	var legitAfter int
	if err := s.maint.QueryRowContext(ctx,
		`SELECT count(*) FROM records WHERE repository = $1`, legit.ID).Scan(&legitAfter); err != nil {
		t.Fatal(err)
	}
	if legitAfter != legitBefore {
		t.Fatalf("the sweep touched a legitimate repository: %d records, was %d", legitAfter, legitBefore)
	}
}
