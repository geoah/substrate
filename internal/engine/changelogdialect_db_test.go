package engine_test

// The changelog dialect gate: the transaction that appends a repository's
// first entries claims the dialect they are written in, and a binary that
// cannot replay a stored changelog refuses the open, before it appends
// anything of its own, and as a service condition rather than an auth
// failure.

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

func TestChangelogDialectGate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	open := func() substrate.Service {
		svc, err := engine.Open(ctx, dsn, engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey),
			engine.WithKindsDir("../../kinds/substrate.reamde.dev/core"))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		return svc
	}
	svc := open()
	_, _, secret := registerUser(t, svc, "geoah")
	_ = svc.Close()

	db, err := engine.OpenScopedDB(dsn, testdb.RepositoryID(t, dsn, "geoah"), engine.RoleApp)
	if err != nil {
		t.Fatalf("open repository schema: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Registration appended the repository's first entries, so their claim
	// committed with them: history no stamp covers is exactly what this gate
	// exists to prevent.
	if got := storedChangelogDialect(t, db); got != engine.MaxChangelogDialect() {
		t.Fatalf("stamped changelog dialect = %d, want %d", got, engine.MaxChangelogDialect())
	}

	// A reopen at the same dialect is silent.
	svc2 := open()
	if _, err := svc2.Dataset(ctx, "geoah"); err != nil {
		t.Fatalf("reopen at the stamped dialect: %v", err)
	}
	_ = svc2.Close()

	// Simulate a changelog written by a NEWER binary: entries spelled in a
	// dialect this one cannot fold. The open must refuse: this is the failure the
	// gate exists to move off the rebuild, which is where it used to land.
	head := changelogHead(t, db)
	if _, err := db.ExecContext(ctx, `UPDATE changelog_dialect SET dialect = $1`,
		engine.MaxChangelogDialect()+1); err != nil {
		t.Fatalf("bump the stored changelog dialect: %v", err)
	}
	svc3 := open()
	defer func() { _ = svc3.Close() }()
	_, err = svc3.Dataset(ctx, "geoah")
	if err == nil {
		t.Fatal("a changelog written in a newer dialect must refuse the open")
	}
	if !errors.Is(err, engine.ErrChangelogDialectNewer) {
		t.Fatalf("expected ErrChangelogDialectNewer, got %v", err)
	}
	if errors.Is(err, substrate.ErrAuth) || errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the refusal must not read as an auth failure or a missing repository: %v", err)
	}
	if !strings.Contains(err.Error(), "upgrade the substrate") {
		t.Fatalf("the refusal must be legible: %v", err)
	}

	// A valid token gets the same refusal, un-wrapped: the API turns anything
	// that is not ErrAuth into `503 repository temporarily unavailable`, and a
	// bricked open must never read as a bad credential.
	if _, _, err := svc3.Authenticate(ctx, secret); !errors.Is(err, engine.ErrChangelogDialectNewer) ||
		errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("authenticate against a refused repository = %v", err)
	}

	// The refusal came BEFORE the open's own writes. That ordering is the
	// guarantee: the stamp is never behind the entries the stamping binary
	// appended, so a refused binary leaves no entry of its own behind.
	if got := changelogHead(t, db); got != head {
		t.Fatalf("the refused open appended entries: head moved %d -> %d", head, got)
	}

	// The refusal is stateless: winding the store back reopens the repository.
	if _, err := db.ExecContext(ctx, `UPDATE changelog_dialect SET dialect = $1`,
		engine.MaxChangelogDialect()); err != nil {
		t.Fatalf("restore the stored changelog dialect: %v", err)
	}
	svc4 := open()
	defer func() { _ = svc4.Close() }()
	if _, err := svc4.Dataset(ctx, "geoah"); err != nil {
		t.Fatalf("reopen after restoring the dialect: %v", err)
	}
}

// TestChangelogDialectAdoptsAnUnstampedStore covers the store every existing
// repository is on the day this ships: a changelog written before anything
// stamped one. It is dialect 1 by construction (no binary that could write
// anything else has run), so it opens, and the next write claims it.
func TestChangelogDialectAdoptsAnUnstampedStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := testdb.NewSchema(t)
	open := func() substrate.Service {
		svc, err := engine.Open(ctx, dsn, engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey),
			engine.WithKindsDir("../../kinds/substrate.reamde.dev/core"))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		return svc
	}
	svc := open()
	if _, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	if _, err := svc.Dataset(ctx, "geoah"); err != nil {
		t.Fatal(err)
	}
	_ = svc.Close()

	db, err := engine.OpenScopedDB(dsn, testdb.RepositoryID(t, dsn, "geoah"), engine.RoleApp)
	if err != nil {
		t.Fatalf("open repository schema: %v", err)
	}
	defer func() { _ = db.Close() }()
	// The wind-back runs as MAINT: the application role may stamp but not
	// erase a stamp, so a pre-gate store has to be simulated from the side
	// erasing a repository runs on.
	maint, err := engine.OpenScopedDB(dsn, testdb.RepositoryID(t, dsn, "geoah"), engine.RoleMaint)
	if err != nil {
		t.Fatalf("open repository schema as maint: %v", err)
	}
	defer func() { _ = maint.Close() }()
	if _, err := maint.ExecContext(ctx, `DELETE FROM changelog_dialect`); err != nil {
		t.Fatalf("wind back the changelog dialect: %v", err)
	}

	svc2 := open()
	defer func() { _ = svc2.Close() }()
	ds, err := svc2.Dataset(ctx, "geoah")
	if err != nil {
		t.Fatalf("an unstamped changelog must open: %v", err)
	}
	// The open claimed nothing: it may never append, and a claim over history
	// this binary did not write is what bars a rollback for no reason.
	if n := changelogDialectRows(t, db); n != 0 {
		t.Fatalf("the open stamped a dialect it had not written to: %d rows", n)
	}
	// The first append is the claim, and it commits with the entry.
	if _, _, err := ds.MintToken(ctx, "test", nil); err != nil {
		t.Fatalf("mint a token: %v", err)
	}
	if got := storedChangelogDialect(t, db); got != engine.MaxChangelogDialect() {
		t.Fatalf("stamped changelog dialect = %d, want %d", got, engine.MaxChangelogDialect())
	}
}

func changelogDialectRows(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM changelog_dialect`).Scan(&n); err != nil {
		t.Fatalf("count changelog_dialect rows: %v", err)
	}
	return n
}

// TestRebuildRefusesANewerChangelogDialect covers the window the open-time gate
// cannot see: a process holding an ALREADY OPEN dataset while another raises
// the stamp. A rebuild is the operation whose whole job is to interpret
// history, so it re-reads the stamp rather than trusting the open's.
func TestRebuildRefusesANewerChangelogDialect(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, dsn := newService(t)
	if _, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	// Opened and cached BEFORE the stamp moves: from here the gate in the open
	// path never runs for this repository again in this process.
	if _, err := svc.Dataset(ctx, "geoah"); err != nil {
		t.Fatal(err)
	}
	rb := svc.(rebuilder)
	if _, err := rb.RebuildRepository(ctx, "geoah"); err != nil {
		t.Fatalf("rebuild at the stamped dialect: %v", err)
	}

	db, err := engine.OpenScopedDB(dsn, testdb.RepositoryID(t, dsn, "geoah"), engine.RoleApp)
	if err != nil {
		t.Fatalf("open repository schema: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, `UPDATE changelog_dialect SET dialect = $1`,
		engine.MaxChangelogDialect()+1); err != nil {
		t.Fatalf("bump the stored changelog dialect: %v", err)
	}

	before := recordCount(t, db)
	_, err = rb.RebuildRepository(ctx, "geoah")
	if !errors.Is(err, engine.ErrChangelogDialectNewer) {
		t.Fatalf("rebuild against a newer changelog dialect = %v", err)
	}
	// It refused BEFORE clearing the fold: a refusal that emptied the records
	// table would be the outage the gate exists to avoid.
	if got := recordCount(t, db); got != before {
		t.Fatalf("the refused rebuild disturbed the fold: %d records, want %d", got, before)
	}
}

func recordCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM records`).Scan(&n); err != nil {
		t.Fatalf("count records: %v", err)
	}
	return n
}

func storedChangelogDialect(t *testing.T, db *sql.DB) int {
	t.Helper()
	var d int
	if err := db.QueryRowContext(context.Background(),
		`SELECT dialect FROM changelog_dialect`).Scan(&d); err != nil {
		t.Fatalf("read the stamped changelog dialect: %v", err)
	}
	return d
}

func changelogHead(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var head int64
	if err := db.QueryRowContext(context.Background(),
		`SELECT coalesce(max(seq), 0) FROM changelog`).Scan(&head); err != nil {
		t.Fatalf("read the changelog head: %v", err)
	}
	return head
}
