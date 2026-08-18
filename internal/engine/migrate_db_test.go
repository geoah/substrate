package engine_test

// A database migrated by a build from an unmerged branch: what it takes to
// open one, and what it takes to be refused. The case in hand is the 0005 a
// branch build applied before the merge added its last CHECK. 0007 adds that
// constraint wherever it is missing, and supersededSHA256 is what lets the
// runner get as far as applying it.

import (
	"context"
	"database/sql"
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
// and the constraint 0007 exists to add still missing.
func strand(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, q := range []string{
		`DELETE FROM schema_migrations WHERE version >= 7`,
		`UPDATE schema_migrations SET sha256 = '` + branch0005 + `' WHERE version = 5`,
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

	svc, err := engine.Open(context.Background(), dsn,
		engine.WithKindsDir("../../kinds/core.substrate.reamde.dev"),
		engine.WithCredentialKey(engine.TestCredentialKey))
	if err != nil {
		t.Fatalf("a database this repository's own branch build migrated was refused: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })

	if !constraintExists(t, db, "repositories_signed_from_positive") {
		t.Fatal("0007 did not add the constraint the superseded 0005 lacks")
	}
	// The recorded 0005 hash STAYS what ran: it is the record of which file
	// this database applied, and 0007 is what makes accepting it safe.
	if got := recordedHash(t, db, 5); got != branch0005 {
		t.Fatalf("the recorded 0005 hash is %q; the runner rewrote history", got)
	}
	if recordedHash(t, db, 7) == "" {
		t.Fatal("0007 is not recorded as applied")
	}
}

// The database that ran the LANDED 0005 already has the constraint, and 0007
// runs against it too: the conditional add has to be a no-op rather than a
// duplicate-object error.
func TestTheCatchUpMigrationIsANoOpWhereTheConstraintStands(t *testing.T) {
	t.Parallel()
	_, dsn := newService(t)
	db := rawDB(t, dsn)
	if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version >= 7`); err != nil {
		t.Fatalf("unrecord 0007: %v", err)
	}
	if !constraintExists(t, db, "repositories_signed_from_positive") {
		t.Fatal("the landed 0005 did not leave the constraint")
	}
	svc, err := engine.Open(context.Background(), dsn,
		engine.WithKindsDir("../../kinds/core.substrate.reamde.dev"),
		engine.WithCredentialKey(engine.TestCredentialKey))
	if err != nil {
		t.Fatalf("re-applying 0007 over an existing constraint failed: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
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
	_, err := engine.Open(context.Background(), dsn,
		engine.WithKindsDir("../../kinds/core.substrate.reamde.dev"),
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
