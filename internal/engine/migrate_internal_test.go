package engine

// The migration guard, without a database: what the runner accepts as a
// recorded row, and what it refuses. The rules it holds are that a landed
// migration is never edited (the one sanctioned exception is a hash named in
// supersededSHA256 with a later migration closing the gap), that a database a
// newer binary migrated is never opened, and that a pending migration never
// lands below one already recorded.

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// rows spells a schema_migrations table as version: hash, with the name
// derived, for the tests where the name does not matter.
func rows(hashes map[int]string) map[int]recorded {
	out := map[int]recorded{}
	for v, sum := range hashes {
		out[v] = recorded{Name: fmt.Sprintf("%04d_recorded", v), SHA256: sum}
	}
	return out
}

func TestCheckRecordedAcceptsTheMatchingAndTheUnrecorded(t *testing.T) {
	t.Parallel()
	migrations := []migration{
		{Version: 1, Name: "0001_init", SHA256: "aaa"},
		{Version: 2, Name: "0002_next", SHA256: "bbb"},
	}
	// 1 matches, 2 has never been applied, and an empty recorded hash is the
	// pre-hash bootstrap row rather than a divergence.
	if err := checkRecorded(migrations, rows(map[int]string{1: "aaa"})); err != nil {
		t.Fatalf("a matching hash was refused: %v", err)
	}
	if err := checkRecorded(migrations, rows(map[int]string{1: "", 2: ""})); err != nil {
		t.Fatalf("an empty recorded hash was refused: %v", err)
	}
	if err := checkRecorded(migrations, map[int]recorded{}); err != nil {
		t.Fatalf("an empty database was refused: %v", err)
	}
}

// A recorded version the binary does not embed is a newer binary's migration:
// the older binary refuses, names the row so the operator can tell which
// release wrote it, and says to roll forward or restore. Nothing else about
// the database is wrong, so the migrations that match are not named.
func TestCheckRecordedRefusesAVersionTheBinaryLacks(t *testing.T) {
	t.Parallel()
	migrations := []migration{
		{Version: 1, Name: "0001_init", SHA256: "aaa"},
		{Version: 2, Name: "0002_next", SHA256: "bbb"},
	}
	err := checkRecorded(migrations, map[int]recorded{
		1: {Name: "0001_init", SHA256: "aaa"},
		2: {Name: "0002_next", SHA256: "bbb"},
		3: {Name: "0003_from_the_future", SHA256: "ccc"},
	})
	if err == nil {
		t.Fatal("a database that recorded migration 3 was accepted by a binary carrying two")
	}
	if !errors.Is(err, ErrDatabaseNewer) {
		t.Fatalf("the refusal is not ErrDatabaseNewer: %v", err)
	}
	for _, want := range []string{"3 (0003_from_the_future)", "up to 2", "restore"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error does not say %q: %v", want, err)
		}
	}
	for _, unwanted := range []string{"0001_init", "0002_next"} {
		if strings.Contains(err.Error(), unwanted) {
			t.Fatalf("the error names a migration that matches (%s): %v", unwanted, err)
		}
	}
}

// The runner applies in order, so a pending migration BELOW one the database
// already recorded would land on a schema its successors already changed.
// Pending migrations above the highest recorded one are the ordinary upgrade.
func TestCheckRecordedRefusesAGapBelowTheHighestRecorded(t *testing.T) {
	t.Parallel()
	migrations := []migration{
		{Version: 1, Name: "0001_init", SHA256: "aaa"},
		{Version: 2, Name: "0002_next", SHA256: "bbb"},
		{Version: 3, Name: "0003_third", SHA256: "ccc"},
		{Version: 4, Name: "0004_fourth", SHA256: "ddd"},
	}
	if err := checkRecorded(migrations, rows(map[int]string{1: "aaa", 2: "bbb"})); err != nil {
		t.Fatalf("two pending migrations at the top were refused: %v", err)
	}
	err := checkRecorded(migrations, rows(map[int]string{1: "aaa", 2: "bbb", 4: "ddd"}))
	if err == nil {
		t.Fatal("migration 3 pending under a recorded 4 was accepted")
	}
	if errors.Is(err, ErrDatabaseNewer) {
		t.Fatalf("a gap was reported as a newer database: %v", err)
	}
	if !strings.Contains(err.Error(), "3 (0003_third)") || !strings.Contains(err.Error(), "recorded (4)") {
		t.Fatalf("the error does not name the gap and the highest recorded version: %v", err)
	}
}

func TestCheckRecordedNamesEveryDivergenceAtOnce(t *testing.T) {
	t.Parallel()
	migrations := []migration{
		{Version: 1, Name: "0001_init", SHA256: "aaa"},
		{Version: 2, Name: "0002_next", SHA256: "bbb"},
		{Version: 3, Name: "0003_third", SHA256: "ccc"},
	}
	err := checkRecorded(migrations, rows(map[int]string{1: "aaa", 2: "was-edited", 3: "also-edited"}))
	if err == nil {
		t.Fatal("two edited migrations were accepted")
	}
	// Reporting the first alone makes a tree behind by several edits learn
	// about them one boot at a time.
	for _, want := range []string{"0002_next", "was-edited", "0003_third", "also-edited"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error does not name %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "0001_init") {
		t.Fatalf("the error names a migration that matches: %v", err)
	}
}

func TestCheckRecordedAcceptsASupersededHash(t *testing.T) {
	t.Parallel()
	migrations := []migration{{Version: 5, Name: "0005_changelog_integrity", SHA256: "current"}}
	const branchHash = "63fd9e709feefca7bd5ab040d268988d8f6f24c740f0384759f125f7f8adcc40"
	if err := checkRecorded(migrations, rows(map[int]string{5: branchHash})); err != nil {
		t.Fatalf("the superseded 0005 hash was refused: %v", err)
	}
	// The exception is per version: the same hash under another version is
	// still an edited migration.
	migrations = []migration{{Version: 6, Name: "0006_manager_principal", SHA256: "current"}}
	if err := checkRecorded(migrations, rows(map[int]string{6: branchHash})); err == nil {
		t.Fatal("a superseded hash was accepted under the wrong version")
	}
}

// supersededSHA256 is written by hand, so it rots by hand: an entry whose
// version no longer exists, or whose hash is what the file hashes to today,
// is a line that silently stops meaning anything.
func TestSupersededHashesNameALiveMigrationAndAnOldFile(t *testing.T) {
	t.Parallel()
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	byVersion := map[int]migration{}
	for _, m := range migrations {
		byVersion[m.Version] = m
	}
	for version, hashes := range supersededSHA256 {
		m, ok := byVersion[version]
		if !ok {
			t.Fatalf("supersededSHA256 names migration %d, which no longer exists", version)
		}
		for _, h := range hashes {
			if h == m.SHA256 {
				t.Fatalf("migration %d (%s) lists its CURRENT hash as superseded", version, m.Name)
			}
		}
		// The catch-up has to exist, or accepting the old hash accepts a
		// schema nothing brings up to date.
		if version >= len(migrations) {
			t.Fatalf("migration %d is superseded but nothing later closes the gap", version)
		}
	}
}
