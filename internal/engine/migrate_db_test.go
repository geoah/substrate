package engine_test

// The runner's bookkeeping against a real database: which recorded
// schema_migrations rows an open accepts, and which ones it refuses before
// any later boot step writes to the schema.

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
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
		engine.WithKindsDir(engine.CoreKindsDir),
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
// row, before anything after the runner writes. Both operator shapes refuse,
// the exclusive open `repository rebuild` rides and the read-only one
// `repository verify` rides, because both run the runner.
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
			engine.WithKindsDir(engine.CoreKindsDir),
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
