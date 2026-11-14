package engine_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/testdb"
)

// identityColumns are the columns that hold a record's kind or id: records'
// own, and those of every table keyed or indexed by one record, and the
// control plane's repository id. Each sorts COLLATE "C", byte order, so the
// index and every page over it order as Go compares strings, whatever the
// host's locale library (migration 0021). Every `repository` column is held
// the same way without being listed.
var identityColumns = [][2]string{
	{"records", "kind"},
	{"records", "id"},
	{"refs", "src_kind"},
	{"refs", "src"},
	{"refs", "dst_kind"},
	{"refs", "dst"},
	{"former_ids", "record_kind"},
	{"former_ids", "record_id"},
	{"former_ids", "former_id"},
	{"annotations", "record_kind"},
	{"annotations", "record_id"},
	{"property_marks", "record_kind"},
	{"property_marks", "record_id"},
	// The view that gives the landed repository migrations the old rows
	// (migration 0026) keeps the marks' order.
	{"property_managers", "record_kind"},
	{"property_managers", "record_id"},
	{"property_offers", "record_kind"},
	{"property_offers", "record_id"},
	{"embeddings", "record_kind"},
	{"embeddings", "record_id"},
	{"embed_queue", "record_kind"},
	{"embed_queue", "record_id"},
	{"sealed", "record_kind"},
	{"sealed", "record_id"},
	{"oauth_flows", "record_kind"},
	{"oauth_flows", "record_id"},
	{"trigger_failures", "record_id"},
	{"trigger_failures", "trigger_id"},
	{"trigger_cursors", "trigger_id"},
	{"trigger_schedule", "trigger_id"},
	{"paged_cursors", "trigger_id"},
	{"changelog", "kind"},
	{"changelog", "record_id"},
	{"changelog_foreign", "record_kind"},
	{"changelog_foreign", "record_id"},
	{"compaction_marks", "kind"},
	{"compaction_marks", "held_after"},
	{"compaction_held", "record_kind"},
	{"compaction_held", "record_id"},
	{"compaction_erasures", "record_kind"},
	{"compaction_erasures", "record_id"},
	{"compaction_erasures", "on_behalf_of"},
	{"compaction_closures", "record_kind"},
	{"compaction_closures", "record_id"},
	{"observed_segments", "kind"},
	{"observed_segments", "record_id"},
	{"observed_latest", "record_id"},
	{"unit_uses", "kind"},
	{"unit_uses", "record_id"},
	{"index_reprojections", "kind"},
	{"index_reprojections", "after_id"},
	{"repositories", "id"},
}

func TestEveryIdentityColumnSortsInByteOrder(t *testing.T) {
	t.Parallel()
	db := rawDB(t, engine.MigratedDSN(t))
	collation := map[[2]string]string{}
	rows, err := db.Query(`SELECT table_name, column_name, COALESCE(collation_name, '')
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND data_type = 'text'`)
	if err != nil {
		t.Fatalf("read the columns: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var table, column, coll string
		if err := rows.Scan(&table, &column, &coll); err != nil {
			t.Fatal(err)
		}
		collation[[2]string{table, column}] = coll
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	listed := map[[2]string]bool{}
	for _, col := range identityColumns {
		listed[col] = true
		got, ok := collation[col]
		if !ok {
			t.Errorf("%s.%s is not a text column of the schema", col[0], col[1])
			continue
		}
		if got != "C" {
			t.Errorf("%s.%s sorts by collation %q, want \"C\": its order depends on the host's locale", col[0], col[1], got)
		}
	}
	// A column a new table names a record by is held too: one named for a
	// record's kind or id, or a trigger's, and missing from the list fails
	// until it is listed, in byte order.
	for col, got := range collation {
		switch col[1] {
		case "record_kind", "record_id", "trigger_id":
			if !listed[col] {
				t.Errorf("%s.%s names a record and is not in identityColumns", col[0], col[1])
			}
		case "repository":
			if got != "C" {
				t.Errorf("%s.repository sorts by collation %q, want \"C\": every key over it depends on the host's locale", col[0], got)
			}
		}
	}

	// So no primary key leads with a column in the locale's order.
	rows, err = db.Query(`SELECT c.relname, a.attname, COALESCE(co.collname, '')
		FROM pg_index ix
		JOIN pg_class c ON c.oid = ix.indrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = ix.indkey[0]
		LEFT JOIN pg_collation co ON co.oid = a.attcollation
		WHERE ix.indisprimary AND n.nspname = current_schema() AND a.atttypid = 'text'::regtype`)
	if err != nil {
		t.Fatalf("read the primary keys: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var table, column, coll string
		if err := rows.Scan(&table, &column, &coll); err != nil {
			t.Fatal(err)
		}
		if coll != "C" {
			t.Errorf("the primary key of %s leads with %s, which sorts by collation %q, want \"C\"", table, column, coll)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

// The byte-order migration changes the columns it names and nothing a
// database's owner added beside them: a table of their own with a
// `repository` column of a text domain keeps its domain, a view over it stays,
// and a policy that reads one of the changed columns from another table, or
// from a table the migration does not change, is dropped before the first
// change and comes back as it was after the last, as every shipped policy
// does. A database only this server wrote holds none of these; the shipped
// policies each read their own table's column.
func TestTheByteOrderMigrationLeavesWhatItDoesNotOwn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	if err := engine.MigrateThrough(ctx, dsn, 20); err != nil {
		t.Fatalf("stage the schema at 0020: %v", err)
	}
	db := rawDB(t, dsn)
	for _, stmt := range []string{
		`CREATE DOMAIN repo_name AS text CHECK (VALUE <> '')`,
		`CREATE TABLE extra (repository repo_name NOT NULL)`,
		`CREATE VIEW extra_view AS SELECT repository FROM extra`,
		`CREATE POLICY records_extra ON records AS RESTRICTIVE FOR SELECT
			USING (EXISTS (SELECT 1 FROM annotations a WHERE a.repository = records.repository))`,
		`CREATE TABLE untargeted (n int)`,
		`ALTER TABLE untargeted ENABLE ROW LEVEL SECURITY`,
		`CREATE POLICY untargeted_p ON untargeted USING (EXISTS (SELECT 1 FROM refs r WHERE r.src = 'x'))`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	policies, security := policySnapshot(t, db), securitySnapshot(t, db)

	if err := engine.MigrateThrough(ctx, dsn, 21); err != nil {
		t.Fatalf("the migration refused a schema with objects it does not own: %v", err)
	}
	if got := policySnapshot(t, db); got != policies {
		t.Errorf("the policies did not come back as they were:\n before %s\n after  %s", policies, got)
	}
	if got := securitySnapshot(t, db); got != security {
		t.Errorf("row security changed:\n before %s\n after  %s", security, got)
	}
	var domain sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT domain_name FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'extra' AND column_name = 'repository'`).Scan(&domain); err != nil {
		t.Fatal(err)
	}
	if domain.String != "repo_name" {
		t.Errorf("extra.repository is of domain %q after the migration, want repo_name: a table it does not own was changed", domain.String)
	}
	var views int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_views WHERE schemaname = current_schema() AND viewname = 'extra_view'`).Scan(&views); err != nil || views != 1 {
		t.Errorf("the view over a table the migration does not own: %d, %v", views, err)
	}
	for _, col := range [][2]string{{"records", "repository"}, {"annotations", "repository"}, {"refs", "src"}} {
		var coll string
		if err := db.QueryRowContext(ctx, `SELECT COALESCE(collation_name, '') FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2`, col[0], col[1]).Scan(&coll); err != nil {
			t.Fatal(err)
		}
		if coll != "C" {
			t.Errorf("%s.%s sorts by %q after the migration, want \"C\"", col[0], col[1], coll)
		}
	}
}

// Changing a column's type drops the planner's statistics for it. The byte
// order migration analyzes every column it changed, so a populated, analyzed
// database keeps its statistics across the upgrade.
func TestTheByteOrderMigrationKeepsThePlannersStatistics(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	if err := engine.MigrateThrough(ctx, dsn, 20); err != nil {
		t.Fatalf("stage the schema at 0020: %v", err)
	}
	db := rawDB(t, dsn)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO embed_queue (repository, record_kind, record_id, property)
		SELECT 'rnd-' || (n % 3), 'ada.example.com/notes/note', 'n' || n, 'text'
		FROM generate_series(1, 2000) AS n`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `ANALYZE embed_queue`); err != nil {
		t.Fatal(err)
	}
	stats := func() string {
		return snapshotRows(t, db, `SELECT attname, n_distinct::text FROM pg_stats
			WHERE schemaname = current_schema() AND tablename = 'embed_queue'
			  AND attname IN ('repository', 'record_kind', 'record_id') ORDER BY attname`)
	}
	before := stats()
	if strings.Count(before, "\n") != 3 {
		t.Fatalf("the staged table holds statistics for %q, want its three identity columns", before)
	}
	if err := engine.MigrateThrough(ctx, dsn, 21); err != nil {
		t.Fatal(err)
	}
	if after := stats(); after != before {
		t.Errorf("the planner's statistics after the migration:\n %s want them as before:\n %s", after, before)
	}
}

// policySnapshot is every row security policy of the schema, as pg_policies
// spells it.
func policySnapshot(t *testing.T, db *sql.DB) string {
	t.Helper()
	return snapshotRows(t, db, `SELECT tablename, policyname, permissive, cmd, roles::text,
		COALESCE(qual, ''), COALESCE(with_check, '')
		FROM pg_policies WHERE schemaname = current_schema() ORDER BY tablename, policyname`)
}

// securitySnapshot is each table's ENABLE and FORCE row security flags.
func securitySnapshot(t *testing.T, db *sql.DB) string {
	t.Helper()
	return snapshotRows(t, db, `SELECT c.relname, c.relrowsecurity::text, c.relforcerowsecurity::text
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema() AND c.relkind = 'r' ORDER BY c.relname`)
}

func snapshotRows(t *testing.T, db *sql.DB, query string) string {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	defer func() { _ = rows.Close() }()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for rows.Next() {
		vals := make([]string, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		b.WriteString(strings.Join(vals, "|"))
		b.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return b.String()
}
