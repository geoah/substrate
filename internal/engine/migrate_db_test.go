package engine_test

// The runner's bookkeeping against a real database: which recorded
// schema_migrations rows an open accepts, and which ones it refuses before
// any later boot step writes to the schema.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// An edited migration nobody sanctioned is still refused, and the refusal
// names the migration and both hashes rather than saying the schema is wrong.
func TestOpenRefusesAnUnknownEditedMigration(t *testing.T) {
	t.Parallel()
	_, dsn := newService(t)
	db := rawDB(t, dsn)
	if _, err := db.Exec(`UPDATE schema_migrations SET sha256 = 'not-a-hash-anybody-shipped' WHERE version = 1`); err != nil {
		t.Fatalf("edit the recorded hash: %v", err)
	}
	_, err := engine.OpenForTest(t, context.Background(), dsn,
		engine.WithDataRoot(t.TempDir()),
		engine.WithKindsDir(engine.SeedKindsDir),
		engine.WithCredentialKey(engine.TestCredentialKey))
	if err == nil {
		t.Fatal("a database whose 0001 nothing recognizes was opened")
	}
	for _, want := range []string{"0001_init", "not-a-hash-anybody-shipped", "dev:wipe"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not name %q: %v", want, err)
		}
	}
}

// migrationRows is the schema_migrations table as one string, so a test can
// hold it unchanged across an open that must write nothing.
func migrationRows(t *testing.T, db *sql.DB) string {
	t.Helper()
	var rows string
	if err := db.QueryRow(`
		SELECT coalesce(string_agg(version || ':' || name || ':' || sha256, ',' ORDER BY version), '')
		FROM schema_migrations`).Scan(&rows); err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	return rows
}

// indexNames is every index in the test's own schema, as one string.
func indexNames(t *testing.T, db *sql.DB) string {
	t.Helper()
	var names string
	if err := db.QueryRow(`
		SELECT coalesce(string_agg(indexname, ',' ORDER BY indexname), '')
		FROM pg_indexes WHERE schemaname = current_schema()`).Scan(&names); err != nil {
		t.Fatalf("read pg_indexes: %v", err)
	}
	return names
}

// A database a NEWER binary migrated: schema_migrations records a version this
// binary does not carry. The open refuses with ErrDatabaseNewer, naming the
// row, before anything after the runner writes. Both operator shapes refuse:
// the exclusive open `repository rebuild` rides runs the runner, and the
// read-only one `repository verify` rides reads the same rows (checkMigrated).
func TestOpenRefusesADatabaseANewerBinaryMigrated(t *testing.T) {
	t.Parallel()
	svc, dsn := newService(t)
	if err := svc.Close(); err != nil {
		t.Fatalf("close the first open: %v", err)
	}
	db := rawDB(t, dsn)
	if _, err := db.Exec(`INSERT INTO schema_migrations (version, name, sha256) VALUES (9999, '9999_from_the_future', 'unknown-to-this-binary')`); err != nil {
		t.Fatalf("record a future migration: %v", err)
	}
	// One of the declared indexes is dropped so that the boot's ensureIndices
	// has something observable to do: a refusal that still recreated it would
	// be a refusal after the writes, not before them.
	var dropped string
	if err := db.QueryRow(`SELECT indexname FROM pg_indexes WHERE schemaname = current_schema() AND indexname LIKE 'idx_%' ORDER BY 1 LIMIT 1`).Scan(&dropped); err != nil {
		t.Fatalf("find a declared index to drop: %v", err)
	}
	if _, err := db.Exec(`DROP INDEX ` + dropped); err != nil {
		t.Fatalf("drop %s: %v", dropped, err)
	}
	rowsBefore, indexesBefore := migrationRows(t, db), indexNames(t, db)

	for name, extra := range map[string][]engine.Option{
		"exclusive": nil,
		"read-only": {engine.WithDirectoryReadOnly()},
	} {
		opts := append([]engine.Option{
			engine.WithDataRoot(t.TempDir()),
			engine.WithKindsDir(engine.SeedKindsDir),
			engine.WithCredentialKey(engine.TestCredentialKey),
		}, extra...)
		_, err := engine.OpenForTest(t, context.Background(), dsn, opts...)
		if err == nil {
			t.Fatalf("%s open: a database recording migration 9999 was opened by a binary that does not carry it", name)
		}
		if !errors.Is(err, engine.ErrDatabaseNewer) {
			t.Fatalf("%s open: the refusal is not ErrDatabaseNewer: %v", name, err)
		}
		if !strings.Contains(err.Error(), "9999 (9999_from_the_future)") {
			t.Fatalf("%s open: the refusal does not name the recorded row: %v", name, err)
		}
	}
	if got := migrationRows(t, db); got != rowsBefore {
		t.Fatalf("schema_migrations changed under a refused open:\n before %s\n after  %s", rowsBefore, got)
	}
	if got := indexNames(t, db); got != indexesBefore {
		t.Fatalf("pg_indexes changed under a refused open (the boot wrote before refusing):\n before %s\n after  %s", indexesBefore, got)
	}
}

// A database one migration BEHIND this binary, the one an older server runs
// on while a newer substratectl opens it read-only beside it. Applying the
// migration would move the server's database past its release and close its
// rollback, so the read-only open refuses with ErrDatabaseOlder, names the
// migration, and leaves schema_migrations as it found it.
func TestReadOnlyOpenRefusesADatabaseItWouldMigrate(t *testing.T) {
	t.Parallel()
	svc, dsn := newService(t)
	root := engine.DataRootOf(svc)
	if err := svc.Close(); err != nil {
		t.Fatalf("close the first open: %v", err)
	}
	db := rawDB(t, dsn)
	var version int
	var name string
	if err := db.QueryRow(`SELECT version, name FROM schema_migrations ORDER BY version DESC LIMIT 1`).Scan(&version, &name); err != nil {
		t.Fatalf("read the newest recorded migration: %v", err)
	}
	// The newest row goes and its DDL stays: the refusal comes before
	// anything reads the schema the migration built.
	if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version = $1`, version); err != nil {
		t.Fatalf("unrecord migration %d: %v", version, err)
	}
	before := migrationRows(t, db)

	_, err := engine.OpenForTest(t, context.Background(), dsn,
		engine.WithDataRoot(root), engine.WithDirectoryReadOnly())
	if !errors.Is(err, engine.ErrDatabaseOlder) {
		t.Fatalf("a read-only open of a database missing migration %d = %v, want ErrDatabaseOlder", version, err)
	}
	if want := fmt.Sprintf("%d (%s)", version, name); !strings.Contains(err.Error(), want) {
		t.Fatalf("the refusal does not name %s: %v", want, err)
	}
	if got := migrationRows(t, db); got != before {
		t.Fatalf("a read-only open applied a migration:\n before %s\n after  %s", before, got)
	}
}

// A database at this binary's migrations opens read-only beside its running
// writer, in both registry shapes: substratectl's empty one and the shipped
// one. Both operator commands that ride the open work (verify, and reembed
// through the repository's embeddings row), and the open changes no schema:
// schema_migrations stays as it was, and a declared index missing from the
// schema stays missing, because building it would lock the shared records
// table under the server.
func TestReadOnlyOpenServesAnUpToDateDatabaseBesideTheWriter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, dsn := newDatasetWithDSN(t)
	installEmbedProvider(t, ds, "vectors", newFakeEmbedServer(t).srv.URL, "text-embedding-3-small")
	db := rawDB(t, dsn)
	var dropped string
	if err := db.QueryRow(`SELECT indexname FROM pg_indexes WHERE schemaname = current_schema() AND indexname LIKE 'idx_%' ORDER BY 1 LIMIT 1`).Scan(&dropped); err != nil {
		t.Fatalf("find a declared index to drop: %v", err)
	}
	if _, err := db.Exec(`DROP INDEX ` + dropped); err != nil {
		t.Fatalf("drop %s: %v", dropped, err)
	}
	rowsBefore, indexesBefore := migrationRows(t, db), indexNames(t, db)

	for name, extra := range map[string][]engine.Option{
		"substratectl": {engine.WithRegistry(vocabulary.NewRegistry())},
		"shipped":      nil,
	} {
		ro := reopenWith(t, dsn, engine.DataRootOf(svc), append([]engine.Option{engine.WithDirectoryReadOnly()}, extra...)...)
		if report := mustVerify(t, ro, testdb.Repository(t)); !report.OK {
			t.Fatalf("%s: verify through a read-only open failed: %+v", name, report)
		}
		rods, err := ro.Dataset(ctx, testdb.Repository(t))
		if err != nil {
			t.Fatalf("%s: open the repository read-only: %v", name, err)
		}
		report, err := rods.Reembed(ctx, false)
		if err != nil || report.Provider != "vectors" {
			t.Fatalf("%s: reembed through a read-only open = %+v, %v", name, report, err)
		}
		if err := ro.Close(); err != nil {
			t.Fatalf("%s: close the read-only open: %v", name, err)
		}
	}
	if got := migrationRows(t, db); got != rowsBefore {
		t.Fatalf("schema_migrations changed under a read-only open:\n before %s\n after  %s", rowsBefore, got)
	}
	if got := indexNames(t, db); got != indexesBefore {
		t.Fatalf("a read-only open built an index:\n before %s\n after  %s", indexesBefore, got)
	}
}
