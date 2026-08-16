package engine

// The migration guard, without a database: what the runner accepts as a
// recorded hash, and what it refuses. The rule it holds is that a landed
// migration is never edited, and the one sanctioned exception is a hash named
// in supersededSHA256 with a later migration closing the gap.

import (
	"strings"
	"testing"
)

func TestCheckRecordedAcceptsTheMatchingAndTheUnrecorded(t *testing.T) {
	t.Parallel()
	migrations := []migration{
		{Version: 1, Name: "0001_init", SHA256: "aaa"},
		{Version: 2, Name: "0002_next", SHA256: "bbb"},
	}
	// 1 matches, 2 has never been applied, and an empty recorded hash is the
	// pre-hash bootstrap row rather than a divergence.
	if err := checkRecorded(migrations, map[int]string{1: "aaa"}); err != nil {
		t.Fatalf("a matching hash was refused: %v", err)
	}
	if err := checkRecorded(migrations, map[int]string{1: "", 2: ""}); err != nil {
		t.Fatalf("an empty recorded hash was refused: %v", err)
	}
}

func TestCheckRecordedNamesEveryDivergenceAtOnce(t *testing.T) {
	t.Parallel()
	migrations := []migration{
		{Version: 1, Name: "0001_init", SHA256: "aaa"},
		{Version: 2, Name: "0002_next", SHA256: "bbb"},
		{Version: 3, Name: "0003_third", SHA256: "ccc"},
	}
	err := checkRecorded(migrations, map[int]string{1: "aaa", 2: "was-edited", 3: "also-edited"})
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
	if err := checkRecorded(migrations, map[int]string{5: branchHash}); err != nil {
		t.Fatalf("the superseded 0005 hash was refused: %v", err)
	}
	// The exception is per version: the same hash under another version is
	// still an edited migration.
	migrations = []migration{{Version: 6, Name: "0006_manager_principal", SHA256: "current"}}
	if err := checkRecorded(migrations, map[int]string{6: branchHash}); err == nil {
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
