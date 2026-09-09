package engine_test

// A database migrated by a build from an unmerged branch: what it takes to
// open one, and what it takes to be refused. The case in hand is the 0005 a
// branch build applied before the merge added its last CHECK. 0007 adds that
// constraint wherever it is missing, and supersededSHA256 is what lets the
// runner get as far as applying it. 0014 drops the constraint again with the
// rest of the signing state, so a fully migrated schema no longer carries it;
// what these hold is the runner's bookkeeping.

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
)

const branch0005 = "63fd9e709feefca7bd5ab040d268988d8f6f24c740f0384759f125f7f8adcc40"

func constraintExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow(`
		SELECT count(*) FROM pg_constraint
		WHERE conrelid = 'repositories'::regclass AND conname = $1`, name).Scan(&n); err != nil {
		t.Fatalf("read pg_constraint: %v", err)
	}
	return n > 0
}

func recordedHash(t *testing.T, db *sql.DB, version int) string {
	t.Helper()
	var sum string
	switch err := db.QueryRow(`SELECT sha256 FROM schema_migrations WHERE version = $1`, version).Scan(&sum); {
	case err == sql.ErrNoRows:
		return ""
	case err != nil:
		t.Fatalf("read schema_migrations: %v", err)
	}
	return sum
}

// strand rewrites a fully migrated schema into the one a pre-merge build of
// PR #89 left: 0005 recorded under the hash that branch's file had, no 0007,
// and the constraint 0007 exists to add still missing. Two columns come back
// with it, because a database stranded before 0007 still has both and the
// replayed migrations read them: `signed_from_seq`, which 0005 added and 0014
// dropped, and `username`, which 0001 added, 0013 reads and 0024 dropped.
func strand(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, q := range []string{
		`DELETE FROM schema_migrations WHERE version >= 7`,
		`UPDATE schema_migrations SET sha256 = '` + branch0005 + `' WHERE version = 5`,
		`ALTER TABLE repositories ADD COLUMN IF NOT EXISTS signed_from_seq bigint`,
		`ALTER TABLE repositories ADD COLUMN IF NOT EXISTS username text`,
		`ALTER TABLE repositories DROP CONSTRAINT IF EXISTS repositories_signed_from_positive`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("strand the schema (%s): %v", q, err)
		}
	}
}

func TestOpenHealsADatabaseTheBranchBuildMigrated(t *testing.T) {
	t.Parallel()
	_, dsn := newService(t)
	db := rawDB(t, dsn)
	strand(t, db)
	if constraintExists(t, db, "repositories_signed_from_positive") {
		t.Fatal("the stranded schema still carries the constraint; the test proves nothing")
	}

	svc, err := engine.OpenForTest(t, context.Background(), dsn,
		engine.WithDataRoot(t.TempDir()),
		engine.WithKindsDir(engine.CoreKindsDir),
		engine.WithCredentialKey(engine.TestCredentialKey))
	if err != nil {
		t.Fatalf("a database this repository's own branch build migrated was refused: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })

	if constraintExists(t, db, "repositories_signed_from_positive") {
		t.Fatal("0014 left the constraint 0007 added; the signing state was not dropped")
	}
	// The recorded 0005 hash STAYS what ran: it is the record of which file
	// this database applied, and 0007 is what makes accepting it safe.
	if got := recordedHash(t, db, 5); got != branch0005 {
		t.Fatalf("the recorded 0005 hash is %q; the runner rewrote history", got)
	}
	if recordedHash(t, db, 7) == "" {
		t.Fatal("0007 is not recorded as applied")
	}
	if recordedHash(t, db, 14) == "" {
		t.Fatal("0014 is not recorded as applied")
	}
}

// Re-running 0014 over a schema it already ran on: every drop is IF EXISTS,
// so a replay is a no-op rather than an undefined-object error.
func TestDroppingTheSigningStateIsANoOpWhereItIsAlreadyGone(t *testing.T) {
	t.Parallel()
	_, dsn := newService(t)
	db := rawDB(t, dsn)
	if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version >= 14`); err != nil {
		t.Fatalf("unrecord 0014: %v", err)
	}
	if constraintExists(t, db, "repositories_signed_from_positive") {
		t.Fatal("the landed 0014 did not drop the constraint")
	}
	svc, err := engine.OpenForTest(t, context.Background(), dsn,
		engine.WithDataRoot(t.TempDir()),
		engine.WithKindsDir(engine.CoreKindsDir),
		engine.WithCredentialKey(engine.TestCredentialKey))
	if err != nil {
		t.Fatalf("re-applying 0014 over a migrated schema failed: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if recordedHash(t, db, 14) == "" {
		t.Fatal("0014 is not recorded as applied after the replay")
	}
}

// An edited migration nobody sanctioned is still refused, and the refusal
// names the migration and both hashes rather than saying the schema is wrong.
func TestOpenRefusesAnUnknownEditedMigration(t *testing.T) {
	t.Parallel()
	_, dsn := newService(t)
	db := rawDB(t, dsn)
	if _, err := db.Exec(`UPDATE schema_migrations SET sha256 = 'not-a-hash-anybody-shipped' WHERE version = 5`); err != nil {
		t.Fatalf("edit the recorded hash: %v", err)
	}
	_, err := engine.OpenForTest(t, context.Background(), dsn,
		engine.WithDataRoot(t.TempDir()),
		engine.WithKindsDir(engine.CoreKindsDir),
		engine.WithCredentialKey(engine.TestCredentialKey))
	if err == nil {
		t.Fatal("a database whose 0005 nothing recognizes was opened")
	}
	for _, want := range []string{"0005_changelog_integrity", "not-a-hash-anybody-shipped", "dev:wipe"} {
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
