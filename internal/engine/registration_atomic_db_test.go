package engine

// A registration that fails — or crashes — must leave nothing durable behind.
// The repository's own rows commit FIRST and the control-plane row LAST, so
// the guarantees are: a RETURNED failure erases the scope (transactionally, on
// a context that outlives the request), and a CRASH in the commit window is
// reclaimed by a boot sweep of rows whose repository has no control-plane row.
// These INTERNAL tests drive those seams directly.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

func openBareService(t *testing.T) (*service, string) {
	t.Helper()
	ctx := context.Background()
	dsn := MigratedDSN(t)
	svcIface, err := OpenForTest(t, ctx, dsn, WithDataRoot(t.TempDir()), WithCredentialKey(TestCredentialKey), WithKindsFS(SeedKinds))
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
		secretRefPrefix+"orphan", bindingProviderKind, "orphan-provider", []byte("secret")); err != nil {
		t.Fatalf("plant orphan sealed: %v", err)
	}
	if _, err := raw.Exec(
		`INSERT INTO changelog (seq, actor, op, record_id, kind, principal) VALUES (1, $1, 'put', $2, $3, '')`,
		string(substrate.ActorSystem), "orphan-rec", "samples.substrate.reamde.dev/tasks/task"); err != nil {
		t.Fatalf("plant orphan changelog line: %v", err)
	}
}

// TestFailedRegistrationLeavesNoDurableRows forces a failure in the commit
// window (after the seed, before the control-plane row) and asserts nothing
// durable survives, and that a retry with the SAME name then succeeds.
func TestFailedRegistrationLeavesNoDurableRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := openBareService(t)
	s.testFailAfterSeed = func() error { return errors.New("boom after the seed committed") }

	if _, err := s.CreateRepository(ctx, "alice.example.com"); err == nil {
		t.Fatal("a registration that failed after the seed reported success")
	}
	if n := orphanRowCount(t, s); n != 0 {
		t.Fatalf("the failed registration left %d durable rows; it must create nothing", n)
	}

	// The retry lands cleanly: the orphan carried no control-plane row, so the
	// name was never taken.
	s.testFailAfterSeed = nil
	if _, err := s.CreateRepository(ctx, "alice.example.com"); err != nil {
		t.Fatalf("retry with the same name: %v", err)
	}
	if _, err := s.repositoryByName(ctx, "alice.example.com"); err != nil {
		t.Fatalf("alice is not registered after the retry: %v", err)
	}
	if n := orphanRowCount(t, s); n != 0 {
		t.Fatalf("after a clean retry there are still %d orphan rows", n)
	}
}

// A registration that fails after its row and its person were written takes
// the person in the erase's own transaction: a crash once the erase has
// committed leaves no name held by no repository, and the name registers
// again.
func TestAFailedRegistrationLeavesNoPersonEvenIfItsCleanupIsCutShort(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := openBareService(t)
	name := testdb.Repository(t)
	s.testFailAfterRow = func() error { return errors.New("the directory cannot be written") }
	type crash struct{}
	s.testAfterErase = func(string) { panic(crash{}) }
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("the cleanup was never cut short")
			} else if _, ok := r.(crash); !ok {
				panic(r)
			}
		}()
		_, _ = s.Register(ctx, substrate.RegisterInput{Identity: name, Password: "a-password-that-registers", SkipTOTP: true})
	}()
	if _, err := s.repositoryByName(ctx, name); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the failed registration's repository: %v, want none", err)
	}
	if _, err := s.identityByName(ctx, s.maint, name); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the failed registration's person: %v, want none", err)
	}

	s.testFailAfterRow, s.testAfterErase = nil, nil
	if _, err := s.Register(ctx, substrate.RegisterInput{Identity: name, Password: "a-password-that-registers", SkipTOTP: true}); err != nil {
		t.Fatalf("the name does not register again: %v", err)
	}
}

// TestEraseRepositorySurvivesRequestCancellation: a registration usually fails
// BECAUSE the client went away, so the cleanup must run on a context decoupled
// from the request's. Erasing under an already-canceled context must still
// wipe the scope.
func TestEraseRepositorySurvivesRequestCancellation(t *testing.T) {
	t.Parallel()
	s, dsn := openBareService(t)
	plantOrphan(t, dsn, "orphanrepoidcancel.example.com")
	if orphanRowCount(t, s) == 0 {
		t.Fatal("setup: no orphan rows planted")
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel() // the request that triggered the failure is already gone
	if err := s.eraseRepository(canceled, "orphanrepoidcancel.example.com"); err != nil {
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

	if _, err := s.CreateRepository(ctx, "legit.example.com"); err != nil {
		t.Fatalf("create legit repository: %v", err)
	}
	legit, err := s.repositoryByName(ctx, "legit.example.com")
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

	plantOrphan(t, dsn, "orphanrepoidsweep.example.com")
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

// The sweep finds which repositories each table holds through an index the
// table leads with its repository column (orphanKeysSQL), so every table it
// sweeps must have one: without it each probe reads the table, once per
// repository, which is worse than the anti-join it replaced. A partial index
// does not count, since the planner cannot answer a probe that names no
// predicate from it, and neither does one that is invalid or not yet ready.
func TestEveryRepositoryScopedTableHasAnIndexLedByItsRepository(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := openBareService(t)
	for _, table := range repositoryScopedTables {
		var led bool
		if err := s.maint.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_index i
				JOIN pg_class c ON c.oid = i.indrelid
				JOIN pg_namespace n ON n.oid = c.relnamespace
				JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = i.indkey[0]
				WHERE n.nspname = current_schema() AND c.relname = $1 AND a.attname = 'repository'
				  AND i.indpred IS NULL AND i.indisvalid AND i.indisready)`, table).Scan(&led); err != nil {
			t.Fatalf("%s: %v", table, err)
		}
		if !led {
			t.Errorf("%s has no index led by its repository column: the start's orphan sweep would read it whole", table)
		}
	}
}

// What the sweep reads of a table does not grow with the table (#1107): a
// changelog of 20,000 lines and then of 100,000, under one repository and
// with no orphan, is read the same handful of index entries each time, and
// never scanned whole. The plan is the sweep's own statement, run under
// EXPLAIN ANALYZE.
func TestTheOrphanSweepReadsNoMoreOfALargerHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, _ := openBareService(t)
	if _, err := s.CreateRepository(ctx, "legit.example.com"); err != nil {
		t.Fatal(err)
	}
	legit, err := s.repositoryByName(ctx, "legit.example.com")
	if err != nil {
		t.Fatal(err)
	}
	grow := func(from, n int) {
		t.Helper()
		if _, err := s.maint.ExecContext(ctx, `
			INSERT INTO changelog (repository, seq, actor, op, record_id, kind, principal)
			SELECT $1, $2 + g, 'act-test', 'put', 'r' || g, 'samples.substrate.reamde.dev/tasks/task', ''
			FROM generate_series(1, $3) g`, legit.ID, 1_000_000+from, n); err != nil {
			t.Fatalf("grow the changelog: %v", err)
		}
		if _, err := s.maint.ExecContext(ctx, `ANALYZE changelog`); err != nil {
			t.Fatal(err)
		}
	}
	read := func() int {
		t.Helper()
		var plan string
		if err := s.maint.QueryRowContext(ctx, `EXPLAIN (ANALYZE, FORMAT JSON) `+orphanKeysSQL("changelog")).Scan(&plan); err != nil {
			t.Fatalf("explain the sweep's read: %v", err)
		}
		var root []struct {
			Plan map[string]any `json:"Plan"`
		}
		if err := json.Unmarshal([]byte(plan), &root); err != nil || len(root) != 1 {
			t.Fatalf("decode the plan: %v", err)
		}
		rows := 0
		var walk func(node map[string]any)
		walk = func(node map[string]any) {
			if node["Relation Name"] == "changelog" {
				if node["Node Type"] == "Seq Scan" {
					t.Errorf("the sweep scans the changelog whole:\n%s", plan)
				}
				num := func(k string) int { f, _ := node[k].(float64); return int(f) }
				rows += (num("Actual Rows") + num("Rows Removed by Filter")) * max(num("Actual Loops"), 1)
			}
			children, _ := node["Plans"].([]any)
			for _, c := range children {
				if child, ok := c.(map[string]any); ok {
					walk(child)
				}
			}
		}
		walk(root[0].Plan)
		return rows
	}
	grow(0, 20_000)
	small := read()
	grow(20_000, 80_000)
	large := read()
	t.Logf("the sweep read %d changelog entries of 20,000 and %d of 100,000", small, large)
	if small > 8 || large != small {
		t.Fatalf("the sweep read %d changelog entries of 20,000 and %d of 100,000, want the same handful", small, large)
	}
	if err := s.sweepOrphans(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	var n int
	if err := s.maint.QueryRowContext(ctx, `SELECT count(*) FROM changelog WHERE repository = $1`, legit.ID).Scan(&n); err != nil || n < 100_000 {
		t.Fatalf("the sweep touched the repository's history: %d lines left, %v", n, err)
	}
}
