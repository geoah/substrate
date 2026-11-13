package engine_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

// An unclaimed changelog holds what a history from before the stamp holds,
// dialect 1. A binary that carries a conversion step converts it at its boot
// check and claims the chain's dialect there, whether or not a step changed
// a line, so a history converts once; the open claims nothing more, the first
// append finds the claim in place, and the archive's next pass carries the
// claim into its manifest. The claim is held by its value and by when it was
// written: the stamp is an upsert, so a second claim leaves one row and moves
// its time. (TestChangelogDialectAdoptsAnUnstampedStore holds the binary that
// carries no step, whose first append is the claim.)
func TestAnUnclaimedHistoryIsClaimedByTheBootsConversion(t *testing.T) {
	t.Parallel()
	if engine.MaxChangelogDialect() < 2 {
		t.Fatal("the binary carries no conversion step, so its boot converts nothing; this test holds one that carries a step")
	}
	ctx := context.Background()
	svc, dsn := newService(t)
	registerUser(t, svc, "ada.example.com")
	root := engine.DataRootOf(svc)
	id := repositoryIDByName(t, svc, "ada.example.com")
	_ = svc.Close()
	db, err := engine.OpenScopedDB(dsn, id, engine.RoleApp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// The unclaim runs as MAINT: the application role may raise a stamp and
	// not erase one.
	maint, err := engine.OpenScopedDB(dsn, id, engine.RoleMaint)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := maint.ExecContext(ctx, `DELETE FROM changelog_dialect`); err != nil {
		t.Fatalf("unclaim the changelog: %v", err)
	}
	_ = maint.Close()

	svc2 := mustReopen(t, dsn, root)
	claimed := changelogDialectClaim(t, db)
	if claimed.dialect != engine.MaxChangelogDialect() {
		t.Fatalf("after the boot the stamp is %d, want the chain's %d", claimed.dialect, engine.MaxChangelogDialect())
	}
	ds, err := svc2.Dataset(ctx, "ada.example.com")
	if err != nil {
		t.Fatalf("open the converted history: %v", err)
	}
	if n := changelogDialectRows(t, db); n != 1 {
		t.Fatalf("the open left %d claim rows, want 1", n)
	}
	if now := changelogDialectClaim(t, db); now != claimed {
		t.Fatalf("the open moved the claim: %+v after the boot, %+v after the open", claimed, now)
	}
	if _, _, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: "test"}); err != nil {
		t.Fatalf("mint a token: %v", err)
	}
	if now := changelogDialectClaim(t, db); now != claimed {
		t.Fatalf("the first append claimed again: %+v after the boot, %+v after the append", claimed, now)
	}
	// The stop's last pass leaves the archive at the head, in the claim.
	_ = svc2.Close()
	archived, err := archivedRepoDir(root, id)
	if err != nil {
		t.Fatal(err)
	}
	m, _, err := changelogfile.ReadArchiveManifestDir(archived)
	if err != nil {
		t.Fatal(err)
	}
	if m.ChangelogDialect != engine.MaxChangelogDialect() {
		t.Fatalf("the archive's manifest says changelog dialect %d, want %d", m.ChangelogDialect, engine.MaxChangelogDialect())
	}
}

// dialectClaim is the changelog's claim row: the dialect and when it was
// last written.
type dialectClaim struct {
	dialect int
	at      time.Time
}

func changelogDialectClaim(t *testing.T, db *sql.DB) dialectClaim {
	t.Helper()
	var c dialectClaim
	if err := db.QueryRowContext(context.Background(),
		`SELECT dialect, updated_at FROM changelog_dialect`).Scan(&c.dialect, &c.at); err != nil {
		t.Fatalf("read the changelog's claim: %v", err)
	}
	return c
}
