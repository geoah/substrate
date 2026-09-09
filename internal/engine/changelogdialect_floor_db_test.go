package engine_test

// A store stamped below the dialect floor refuses to OPEN. Every step that
// used to bring such a store forward — the edge translation, the `txn` frame,
// the `kindVersion` and `updatedAt` stamps, the delivery ledger's adoption of
// the trigger tables — was retired together, so opening one would serve a
// repository whose fold this binary cannot complete and whose rebuild would
// quietly drop what the step carried. The refusal names the release that
// still adopted it, because booting that one once is the whole remedy.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

func TestOpenRefusesAStoreBelowTheDialectFloor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)
	open := func() substrate.Service {
		svc, err := engine.OpenForTest(t, ctx, dsn, engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey),
			engine.WithKindsDir(engine.CoreKindsDir))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		return svc
	}
	svc := open()
	if _, err := svc.CreateRepository(ctx, testdb.Username(t), testdb.Authority(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	_ = svc.Close()

	maint, err := engine.OpenScopedDB(dsn, testdb.RepositoryID(t, dsn, testdb.Username(t)), engine.RoleMaint)
	if err != nil {
		t.Fatalf("open the maintenance pool: %v", err)
	}
	defer func() { _ = maint.Close() }()

	// A store an earlier release wrote, as an in-place upgrade would find it.
	// The application role may raise a stamp but not wind one back, so this
	// runs as maint.
	for stored := 1; stored < engine.MaxChangelogDialect(); stored++ {
		if _, err := maint.ExecContext(ctx, `UPDATE changelog_dialect SET dialect = $1`, stored); err != nil {
			t.Fatalf("wind the stamp back to %d: %v", stored, err)
		}
		svc2 := open()
		_, err := svc2.Dataset(ctx, testdb.Username(t))
		_ = svc2.Close()
		if !errors.Is(err, engine.ErrChangelogDialectRetired) {
			t.Fatalf("a store stamped %d opened: err = %v, want ErrChangelogDialectRetired", stored, err)
		}
		for _, want := range []string{"no longer adopts", "then upgrade"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("a store stamped %d: err = %q, want it to say %q", stored, err, want)
			}
		}
	}

	// A store at the maximum opens, which is every repository this binary
	// wrote.
	if _, err := maint.ExecContext(ctx, `UPDATE changelog_dialect SET dialect = $1`, engine.MaxChangelogDialect()); err != nil {
		t.Fatalf("restore the stamp: %v", err)
	}
	svc3 := open()
	defer func() { _ = svc3.Close() }()
	if _, err := svc3.Dataset(ctx, testdb.Username(t)); err != nil {
		t.Fatalf("a store at the maximum must open: %v", err)
	}
}

// An UNSTAMPED store is two things, and the open tells them apart by asking
// the changelog: nothing in it is a fresh repository, which opens; entries in
// it are a history written before the stamp existed, which is refused like
// any other store below the floor. Neither answer may depend on the stamp
// alone, because 0 is what both look like in the column.
func TestAnUnstampedStoreIsJudgedByItsEntries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := engine.MigratedDSN(t)
	open := func() substrate.Service {
		svc, err := engine.OpenForTest(t, ctx, dsn, engine.WithDataRoot(t.TempDir()), engine.WithCredentialKey(engine.TestCredentialKey),
			engine.WithKindsDir(engine.CoreKindsDir))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		return svc
	}
	svc := open()
	if _, err := svc.CreateRepository(ctx, testdb.Username(t), testdb.Authority(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	_ = svc.Close()

	maint, err := engine.OpenScopedDB(dsn, testdb.RepositoryID(t, dsn, testdb.Username(t)), engine.RoleMaint)
	if err != nil {
		t.Fatalf("open the maintenance pool: %v", err)
	}
	defer func() { _ = maint.Close() }()

	// Entries and no stamp: the seed's own history, as a binary from before
	// the stamp left it.
	if _, err := maint.ExecContext(ctx, `DELETE FROM changelog_dialect`); err != nil {
		t.Fatalf("drop the stamp: %v", err)
	}
	svc2 := open()
	_, err = svc2.Dataset(ctx, testdb.Username(t))
	_ = svc2.Close()
	if !errors.Is(err, engine.ErrChangelogDialectRetired) {
		t.Fatalf("an unstamped store holding entries opened: err = %v", err)
	}

	// Nothing and no stamp: a fresh repository, which is what every creation
	// looks like between its row and its first append. It opens, the open
	// claims nothing — a claim over history this binary did not write is what
	// would bar a rollback for no reason — and the first append is the claim,
	// committing with the entry.
	if _, err := maint.ExecContext(ctx, `DELETE FROM changelog`); err != nil {
		t.Fatalf("empty the changelog: %v", err)
	}
	svc3 := open()
	defer func() { _ = svc3.Close() }()
	ds, err := svc3.Dataset(ctx, testdb.Username(t))
	if err != nil {
		t.Fatalf("an unstamped, empty store must open: %v", err)
	}
	if n := changelogDialectRows(t, maint); n != 0 {
		t.Fatalf("the open stamped a dialect it had not written to: %d rows", n)
	}
	if _, _, err := ds.MintToken(ctx, "test", nil); err != nil {
		t.Fatalf("mint a token: %v", err)
	}
	if got := storedChangelogDialect(t, maint); got != engine.MaxChangelogDialect() {
		t.Fatalf("stamped changelog dialect = %d after the first append, want %d", got, engine.MaxChangelogDialect())
	}
}
