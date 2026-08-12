package engine_test

// B1's done-when, as tests: two repositories cannot see each other's rows even
// with the Go-side scoping deliberately dropped, and the policies, the FORCE
// flag and the two roles are asserted to exist so the enforcement cannot
// quietly degrade into discipline.

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

// repositoryScopedTables is the list every repository-scoped table has to stay
// on: a new one that forgets its `repository` column, its policy or its FORCE
// flag fails here rather than in production.
var repositoryScopedTables = []string{
	"records", "edges", "former_ids", "annotations", "property_managers",
	"property_offers", "changelog", "embeddings", "embed_queue", "trigger_cursors",
	"trigger_failures", "trigger_schedule", "sealed", "oauth_flows",
	"paged_cursors", "blobs", "vocabulary_dialect", "vocabulary_promotions",
}

// pair is two registered users, each with one task written at the SAME
// (type, id): the collision is the point, since one shared table now holds
// both rows.
type pair struct {
	svc         substrate.Service
	dsn         string
	alpha, beta string // repository ids
}

func twoRepositories(t *testing.T) pair {
	t.Helper()
	ctx := context.Background()
	svc, dsn := newService(t)
	p := pair{svc: svc, dsn: dsn}
	for _, name := range []string{"alpha", "beta"} {
		info, err := svc.CreateRepository(ctx, name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if name == "alpha" {
			p.alpha = info.ID
		} else {
			p.beta = info.ID
		}
		ds, err := svc.Dataset(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		importVocabulary(t, ds, "tasks")
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: "task", ID: "shared-id",
			Properties: map[string]any{"title": name + " only"},
		})
	}
	return p
}

// TestRepositoryIsolationSurvivesADroppedPredicate is the ticket's own
// standard. The engine never writes `repository = $1` into a query — the
// column default supplies it on insert and the row level security policy
// supplies it on read — so "dropping the Go-side predicate" here means issuing
// the rawest possible SQL, with no predicate at all, on a pool scoped to one
// repository. It must still see one repository's rows and no others.
func TestRepositoryIsolationSurvivesADroppedPredicate(t *testing.T) {
	ctx := context.Background()
	repos := twoRepositories(t)
	dsn, alpha, beta := repos.dsn, repos.alpha, repos.beta

	raw, err := engine.OpenScopedDB(dsn, alpha, engine.RoleApp)
	if err != nil {
		t.Fatalf("open a scoped pool: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })

	// The unqualified read: one row, alpha's, even though beta wrote the same
	// (type, id) pair into the same table.
	var n int
	if err := raw.QueryRowContext(ctx,
		`SELECT count(*) FROM records WHERE id = 'shared-id'`).Scan(&n); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if n != 1 {
		t.Fatalf("an unqualified count saw %d rows at the shared id, want 1", n)
	}
	var title string
	if err := raw.QueryRowContext(ctx,
		`SELECT title FROM records WHERE id = 'shared-id'`).Scan(&title); err != nil {
		t.Fatalf("read the task: %v", err)
	}
	if title != "alpha only" {
		t.Fatalf("title = %q, want alpha's", title)
	}

	// Naming the other repository explicitly is worse than useless: the policy
	// filters it out before the predicate ever runs.
	for _, q := range []string{
		`SELECT count(*) FROM records WHERE repository = $1`,
		`SELECT count(*) FROM changelog WHERE repository = $1`,
	} {
		if err := raw.QueryRowContext(ctx, q, beta).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		if n != 0 {
			t.Fatalf("%s saw %d of another repository's rows", q, n)
		}
	}

	// Writes are bound the same way, in both directions: an UPDATE aimed at
	// another repository matches nothing...
	res, err := raw.ExecContext(ctx,
		`UPDATE records SET title = 'stolen' WHERE repository = $1`, beta)
	if err != nil {
		t.Fatalf("cross-repository update: %v", err)
	}
	if got, _ := res.RowsAffected(); got != 0 {
		t.Fatalf("a cross-repository update touched %d rows", got)
	}
	if res, err = raw.ExecContext(ctx, `DELETE FROM records WHERE repository = $1`, beta); err != nil {
		t.Fatalf("cross-repository delete: %v", err)
	}
	if got, _ := res.RowsAffected(); got != 0 {
		t.Fatalf("a cross-repository delete removed %d rows", got)
	}

	// ...and an INSERT that forges the column is refused by the WITH CHECK,
	// loudly, rather than landing in somebody else's repository.
	_, err = raw.ExecContext(ctx,
		`INSERT INTO records (repository, kind, id, title) VALUES ($1, 'tasks.substrate.reamde.dev/task', 'forged', 'planted')`, beta)
	if err == nil {
		t.Fatal("a forged repository column was accepted")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("forged insert failed with %v, want a row-security violation (42501)", err)
	}

	// The other repository is untouched by all of it.
	other, err := engine.OpenScopedDB(dsn, beta, engine.RoleApp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Close() })
	if err := other.QueryRowContext(ctx,
		`SELECT title FROM records WHERE id = 'shared-id'`).Scan(&title); err != nil {
		t.Fatalf("read beta's task: %v", err)
	}
	if title != "beta only" {
		t.Fatalf("beta's title = %q", title)
	}
}

// TestUnscopedConnectionIsBlindAndMute pins the other half: a connection that
// carries no repository at all reads nothing and writes nothing. The policy's
// missing_ok current_setting fails closed, and the column default raises
// rather than inventing a repository.
func TestUnscopedConnectionIsBlindAndMute(t *testing.T) {
	ctx := context.Background()
	dsn := twoRepositories(t).dsn

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, `SET ROLE `+engine.RoleApp); err != nil {
		t.Skipf("the cluster has no %s role: %v", engine.RoleApp, err)
	}
	// Same connection, so the SET ROLE above still holds.
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, `SET ROLE `+engine.RoleApp); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM records`).Scan(&n); err != nil {
		t.Fatalf("unscoped count: %v", err)
	}
	if n != 0 {
		t.Fatalf("an unscoped connection saw %d rows", n)
	}
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO records (kind, id, title) VALUES ('task', 'orphan', 'nowhere')`); err == nil {
		t.Fatal("an unscoped insert was accepted")
	}
	// The control-plane table is maintenance's alone: substrate_app has no
	// grant on it at all, so the repository ledger is not even readable.
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM repositories`).Scan(&n); err == nil {
		t.Fatalf("substrate_app read the control-plane table (%d rows)", n)
	}
}

// TestRowLevelSecurityIsDeclared is the CI predicate test for isolation:
// the policies and the FORCE flag exist on every repository-scoped table, and
// the two roles exist with the right bypass attribute. It is what stops the
// engine's degraded fallback (no roles, owner-bound only) from shipping.
func TestRowLevelSecurityIsDeclared(t *testing.T) {
	ctx := context.Background()
	dsn := twoRepositories(t).dsn
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	for _, table := range repositoryScopedTables {
		var enabled, forced bool
		if err := db.QueryRowContext(ctx, `
			SELECT c.relrowsecurity, c.relforcerowsecurity
			FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = current_schema() AND c.relname = $1`, table).Scan(&enabled, &forced); err != nil {
			t.Fatalf("%s: %v", table, err)
		}
		if !enabled || !forced {
			t.Errorf("%s: row security enabled=%v forced=%v, want both", table, enabled, forced)
		}
		var qual, check string
		if err := db.QueryRowContext(ctx, `
			SELECT coalesce(qual, ''), coalesce(with_check, '')
			FROM pg_policies
			WHERE schemaname = current_schema() AND tablename = $1`, table).Scan(&qual, &check); err != nil {
			t.Fatalf("%s policy: %v", table, err)
		}
		for _, expr := range []string{qual, check} {
			if !strings.Contains(expr, "substrate.repository") || !strings.Contains(expr, "repository") {
				t.Errorf("%s policy does not key on the repository setting: %q", table, expr)
			}
		}
		// The column the policy reads must exist and be NOT NULL with the
		// setting-derived default, or an insert could bypass the whole scheme.
		var def sql.NullString
		var notNull bool
		if err := db.QueryRowContext(ctx, `
			SELECT a.attnotnull, pg_get_expr(d.adbin, d.adrelid)
			FROM pg_attribute a
			JOIN pg_class c ON c.oid = a.attrelid
			JOIN pg_namespace n ON n.oid = c.relnamespace
			LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
			WHERE n.nspname = current_schema() AND c.relname = $1 AND a.attname = 'repository'`,
			table).Scan(&notNull, &def); err != nil {
			t.Fatalf("%s repository column: %v", table, err)
		}
		if !notNull || !strings.Contains(def.String, "substrate.repository") {
			t.Errorf("%s repository column: notNull=%v default=%q", table, notNull, def.String)
		}
	}

	for role, wantBypass := range map[string]bool{engine.RoleApp: false, engine.RoleMaint: true} {
		var bypass bool
		if err := db.QueryRowContext(ctx,
			`SELECT rolbypassrls FROM pg_roles WHERE rolname = $1`, role).Scan(&bypass); err != nil {
			t.Fatalf("role %s: %v", role, err)
		}
		if bypass != wantBypass {
			t.Errorf("role %s: bypassrls=%v, want %v", role, bypass, wantBypass)
		}
	}
}

// TestAdvisoryLocksArePerRepository pins the fix for the global write lock:
// v0 took one lock id for the whole database, so a held changelog lock stalled every
// write on the box. Holding alpha's must leave beta's writes alone.
func TestAdvisoryLocksArePerRepository(t *testing.T) {
	ctx := context.Background()
	repos := twoRepositories(t)
	svc, dsn, alpha := repos.svc, repos.dsn, repos.alpha

	holder, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Close() }()
	tx, err := holder.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	// The exact key rows.go composes for alpha's changelog.
	if _, err := tx.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`, alpha+"|changelog"); err != nil {
		t.Fatal(err)
	}

	beta, err := svc.Dataset(ctx, "beta")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := beta.Put(ctx, owner, substrate.PutInput{
			Kind: "task", Properties: map[string]any{"title": "while alpha's changelog is locked"},
		})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("beta's write failed: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("beta's write blocked on alpha's changelog lock — the lock is still global")
	}
}
