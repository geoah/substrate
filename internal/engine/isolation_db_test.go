package engine_test

// Two repositories cannot see each other's rows even with the Go-side scoping
// deliberately dropped, and the policies, the FORCE flag and the two roles are
// asserted to exist, so the enforcement cannot quietly degrade into
// discipline. The second half is the principal: which token a write names, on
// the changelog entry and on every manager row it lands, through a split and
// through a rebuild.

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
	"github.com/geoah/substrate/internal/testdb"
)

// repositoryScopedTables is the list every repository-scoped table has to stay
// on: a new one that forgets its `repository` column, its policy or its FORCE
// flag fails here rather than in production.
var repositoryScopedTables = []string{
	"records", "refs", "former_ids", "annotations", "property_managers",
	"property_offers", "changelog", "embeddings", "embed_queue", "trigger_cursors",
	"trigger_failures", "trigger_schedule", "sealed", "oauth_flows",
	"paged_cursors", "vocabulary_dialect",
	"changelog_dialect", "import_progress", "idempotency_keys",
}

// pair is two registered users, each with one task written at the SAME
// (kind, id): the collision is the point, since one shared table now holds
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
	for _, name := range []string{"alpha.example.com", "beta.example.com"} {
		info, err := svc.CreateRepository(ctx, name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if name == "alpha.example.com" {
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
			Properties: map[string]any{"name": name + " only"},
		})
	}
	return p
}

// A repository's own `repository` record describes only itself: the ledger
// Dataset() and ExchangeOTP() resolve through is a table on the maintenance
// pool that substrate_app holds no grant on, so one repository can neither
// list nor delete another's row out from under it.
func TestARepositorySeesOnlyItsOwnDescription(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t)
	alpha, err := svc.CreateRepository(ctx, "alpha.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateRepository(ctx, "beta.example.com"); err != nil {
		t.Fatal(err)
	}
	ds, err := svc.Dataset(ctx, "alpha.example.com")
	if err != nil {
		t.Fatal(err)
	}
	page, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{Kinds: []string{"substrate.reamde.dev/core/repository"}}, First: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || page.Records[0].ID != alpha.ID {
		t.Fatalf("alpha sees %v, want only its own description", ids(page.Records))
	}
	if _, err := ds.Delete(ctx, owner, "substrate.reamde.dev/core/repository", alpha.ID, substrate.DeleteInput{}); err == nil {
		t.Fatal("deleting the repository's own description succeeded")
	}
	// beta stays reachable whatever alpha does with its own rows.
	if _, err := svc.Dataset(ctx, "beta.example.com"); err != nil {
		t.Fatalf("repository beta became unreachable: %v", err)
	}
}

const principalTask = "samples.substrate.reamde.dev/tasks/task"

// managerPrincipals reads the distinct principals the record's manager rows
// carry, so a test can say "every property this write landed names the token".
// The PRINCIPAL: the token id the door resolved from the bearer secret,
// stamped on every changelog entry a request appends and on every manager row
// it lands. The actor beside it is what the caller ASSERTED, so two tokens
// writing as `api` are one actor and two principals, and only the principal
// says which token wrote.
func managerPrincipals(t *testing.T, dsn, kind, id string) []string {
	t.Helper()
	rows, err := rawDB(t, dsn).Query(`
		SELECT DISTINCT principal FROM property_managers
		WHERE record_kind = $1 AND record_id = $2 ORDER BY principal`, kind, id)
	if err != nil {
		t.Fatalf("read manager principals: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatalf("scan manager principal: %v", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read manager principals: %v", err)
	}
	return out
}

func lastPrincipal(t *testing.T, dsn string) string {
	t.Helper()
	var p string
	if err := rawDB(t, dsn).QueryRow(`SELECT principal FROM changelog ORDER BY seq DESC LIMIT 1`).Scan(&p); err != nil {
		t.Fatalf("read the head entry's principal: %v", err)
	}
	return p
}

func TestPrincipalStampsTheEntryAndItsManagerRows(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newDatasetWithDSN(t)
	first := substrate.WithPrincipal(context.Background(), "tok_first")

	task, err := ds.Put(first, owner, substrate.PutInput{
		Kind:       principalTask,
		Properties: map[string]any{"name": "Ship it", "description": "with the token that wrote it"},
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if got := lastPrincipal(t, dsn); got != "tok_first" {
		t.Fatalf("the entry's principal is %q, want the writing token's id", got)
	}
	if got := managerPrincipals(t, dsn, task.Kind, task.ID); len(got) != 1 || got[0] != "tok_first" {
		t.Fatalf("manager principals are %v, want every row on tok_first", got)
	}

	// A second token, the same asserted actor: the entry and the manager row
	// both move, which is the whole point — the actor cannot tell them apart.
	second := substrate.WithPrincipal(context.Background(), "tok_second")
	if _, err := ds.Patch(second, owner, task.Kind, task.ID, substrate.PatchInput{
		Properties: map[string]any{"name": "Ship it now"},
	}); err != nil {
		t.Fatalf("patch: %v", err)
	}
	if got := lastPrincipal(t, dsn); got != "tok_second" {
		t.Fatalf("the second entry's principal is %q, want tok_second", got)
	}
	var namePrincipal string
	if err := rawDB(t, dsn).QueryRow(`
		SELECT principal FROM property_managers
		WHERE record_kind = $1 AND record_id = $2 AND property = 'name'`, task.Kind, task.ID).Scan(&namePrincipal); err != nil {
		t.Fatalf("read the name manager: %v", err)
	}
	if namePrincipal != "tok_second" {
		t.Fatalf("the name manager still names %q after tok_second wrote it", namePrincipal)
	}

	// The principal is covered by the checksum like every other column, so
	// the entries that now carry one have to still verify.
	if report := mustVerify(t, svc, testdb.Repository(t)); !report.OK {
		t.Fatalf("the checksums do not verify with principals stamped: %+v", report.Findings)
	}
}

// A write no token stands behind — the seed, the boot upgrade, a background
// worker — carries the empty principal. Nothing writes the 'invalid'
// placeholder: empty is the one spelling for "no token stood behind this
// write".
func TestPrincipalIsEmptyWhereNoTokenWrote(t *testing.T) {
	t.Parallel()
	_, ds, dsn := newDatasetWithDSN(t)

	task := mustPut(t, ds, owner, substrate.PutInput{
		Kind:       principalTask,
		Properties: map[string]any{"name": "no token here"},
	})
	if got := lastPrincipal(t, dsn); got != "" {
		t.Fatalf("an unauthenticated write stamped principal %q, want empty", got)
	}
	if got := managerPrincipals(t, dsn, task.Kind, task.ID); len(got) != 1 || got[0] != "" {
		t.Fatalf("manager principals are %v, want empty", got)
	}
	var placeholders int64
	if err := rawDB(t, dsn).QueryRow(`SELECT count(*) FROM changelog WHERE principal = 'invalid'`).Scan(&placeholders); err != nil {
		t.Fatalf("count placeholders: %v", err)
	}
	if placeholders != 0 {
		t.Fatalf("%d entries carry the 'invalid' placeholder; no write path may stamp it", placeholders)
	}
}

// A split puts back exactly what its merge moved. A manager row another token
// has written since names the same actor and tier and a DIFFERENT principal,
// and that row records a write the split must not erase.
func TestSplitKeepsAManagerRowAnotherTokenWroteSince(t *testing.T) {
	t.Parallel()
	_, ds, dsn := newDatasetWithDSN(t)
	first := substrate.WithPrincipal(context.Background(), "tok_first")

	winner, err := ds.Put(first, owner, substrate.PutInput{
		Kind: principalTask, Properties: map[string]any{"name": "Winner"},
	})
	if err != nil {
		t.Fatalf("put winner: %v", err)
	}
	loser, err := ds.Put(substrate.WithPrincipal(context.Background(), "tok_second"), owner, substrate.PutInput{
		Kind: principalTask, Properties: map[string]any{"name": "Loser", "url": "https://example.com/1"},
	})
	if err != nil {
		t.Fatalf("put loser: %v", err)
	}
	merge, err := ds.Merge(first, owner, substrate.MergeInput{Kind: principalTask, Winner: winner.ID, Loser: loser.ID})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	// A third token rewrites the migrated property under the same actor: the
	// manager row now stands on tok_third.
	third := substrate.WithPrincipal(context.Background(), "tok_third")
	if _, err := ds.Patch(third, owner, principalTask, winner.ID, substrate.PatchInput{
		Properties: map[string]any{"url": "https://example.com/2"},
	}); err != nil {
		t.Fatalf("patch url: %v", err)
	}

	if _, err := ds.Split(first, owner, substrate.SplitInput{Merge: merge.ID}); err != nil {
		t.Fatalf("split: %v", err)
	}
	var principal string
	err = rawDB(t, dsn).QueryRow(`
		SELECT principal FROM property_managers
		WHERE record_kind = $1 AND record_id = $2 AND property = 'url'`, winner.Kind, winner.ID).Scan(&principal)
	if err != nil {
		t.Fatalf("the split erased a manager row tok_third wrote after the merge: %v", err)
	}
	if principal != "tok_third" {
		t.Fatalf("the url manager names %q, want tok_third", principal)
	}
}

// The manager ledger is a fold of the changelog, principal included: replaying
// the history has to put the same token back on every row, through the
// per-property effect and through the merge's resync snapshot alike.
func TestRebuildReplaysTheManagerPrincipal(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newDatasetWithDSN(t)
	ctx := substrate.WithPrincipal(context.Background(), "tok_first")

	winner, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind:       principalTask,
		Properties: map[string]any{"name": "Winner", "description": "keeps its own"},
	})
	if err != nil {
		t.Fatalf("put winner: %v", err)
	}
	loser, err := ds.Put(substrate.WithPrincipal(context.Background(), "tok_second"), owner, substrate.PutInput{
		Kind:       principalTask,
		Properties: map[string]any{"name": "Loser", "url": "https://example.com/1"},
	})
	if err != nil {
		t.Fatalf("put loser: %v", err)
	}
	// The merge migrates the loser's manager rows where the winner has none,
	// and its resync snapshot is what a replay writes back.
	if _, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: principalTask, Winner: winner.ID, Loser: loser.ID}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if got := managerPrincipals(t, dsn, winner.Kind, winner.ID); len(got) != 2 {
		t.Fatalf("the merged winner's manager principals are %v, want both tokens", got)
	}

	before := foldOf(t, ds)
	rb, ok := svc.(rebuilder)
	if !ok {
		t.Fatal("the service cannot rebuild a repository")
	}
	if _, err := rb.RebuildRepository(context.Background(), testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after := foldOf(t, ds); string(after) != string(before) {
		t.Fatal("the rebuilt fold does not match the one the writes left; the manager principal did not replay")
	}
}

// The four isolation cases only read (the one write they attempt is refused),
// so they share the one pair of repositories.
func TestRepositoryIsolation(t *testing.T) {
	t.Parallel()
	repos := twoRepositories(t)

	// The engine never writes `repository = $1` into a query — the
	// column default supplies it on insert and the row level security policy
	// supplies it on read — so "dropping the Go-side predicate" here means issuing
	// the rawest possible SQL, with no predicate at all, on a pool scoped to one
	// repository. It must still see one repository's rows and no others.
	t.Run("a dropped predicate still sees one repository alone", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		dsn, alpha, beta := repos.dsn, repos.alpha, repos.beta

		raw, err := engine.OpenScopedDB(dsn, alpha, engine.RoleApp)
		if err != nil {
			t.Fatalf("open a scoped pool: %v", err)
		}
		t.Cleanup(func() { _ = raw.Close() })

		// The unqualified read: one row, alpha's, even though beta wrote the same
		// (kind, id) pair into the same table.
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
		if title != "alpha.example.com only" {
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
			`INSERT INTO records (repository, kind, id, title) VALUES ($1, 'samples.substrate.reamde.dev/tasks/task', 'forged', 'planted')`, beta)
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
		if title != "beta.example.com only" {
			t.Fatalf("beta's title = %q", title)
		}
	})

	// A connection that carries no repository at all reads nothing and writes
	// nothing: the policy's missing_ok current_setting fails closed, and the
	// column default raises rather than inventing a repository.
	t.Run("an unscoped connection is blind and mute", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		dsn := repos.dsn

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
	})

	// The policies and the FORCE flag exist on every repository-scoped table, and
	// the two roles exist with the right bypass attribute. It is what stops the
	// engine's degraded fallback (no roles, owner-bound only) from shipping.
	t.Run("row level security is declared on every scoped table", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		dsn := repos.dsn
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
	})

	// The write lock is per repository: one lock id for the whole database
	// meant a held changelog lock stalled every write on the box. Holding
	// alpha's must leave beta's writes alone.
	t.Run("advisory locks are per repository", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
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
			`SELECT pg_advisory_xact_lock(`+engine.AdvisoryKeySQL+`)`, alpha+"|changelog"); err != nil {
			t.Fatal(err)
		}

		beta, err := svc.Dataset(ctx, "beta.example.com")
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() {
			_, err := beta.Put(ctx, owner, substrate.PutInput{
				Kind: "task", Properties: map[string]any{"name": "while alpha's changelog is locked"},
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
	})
}
