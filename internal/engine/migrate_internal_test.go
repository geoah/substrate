package engine

// The migration guard, without a database: what the runner accepts as a
// recorded row, and what it refuses. The rules it holds are that a landed
// migration is never edited, that a database a newer binary migrated is never
// opened, and that a pending migration never lands below one already
// recorded.

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
	// The rows are listed in version order, numerically: 10 after 9.
	err = checkRecorded(migrations, rows(map[int]string{1: "aaa", 2: "bbb", 9: "i", 10: "j"}))
	if err == nil {
		t.Fatal("two future migrations were accepted")
	}
	if nine, ten := strings.Index(err.Error(), "  9 ("), strings.Index(err.Error(), "  10 ("); nine < 0 || ten < 0 || ten < nine {
		t.Fatalf("the rows are not in numeric order: %v", err)
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

// A read-only open applies nothing, so what the runner would apply is what it
// refuses: every migration the binary carries and the database has not
// recorded, by version and name, and none of the ones it has.
func TestCheckPendingNamesEveryMigrationTheDatabaseLacks(t *testing.T) {
	t.Parallel()
	migrations := []migration{
		{Version: 1, Name: "0001_init", SHA256: "aaa"},
		{Version: 2, Name: "0002_next", SHA256: "bbb"},
		{Version: 3, Name: "0003_third", SHA256: "ccc"},
	}
	if err := checkPending(migrations, rows(map[int]string{1: "aaa", 2: "bbb", 3: "ccc"})); err != nil {
		t.Fatalf("a database with every migration applied was refused: %v", err)
	}
	err := checkPending(migrations, rows(map[int]string{1: "aaa"}))
	if !errors.Is(err, ErrDatabaseOlder) {
		t.Fatalf("two pending migrations were not refused as ErrDatabaseOlder: %v", err)
	}
	for _, want := range []string{"2 (0002_next)", "3 (0003_third)", "read-only"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not say %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "0001_init") {
		t.Fatalf("the refusal names a migration the database applied: %v", err)
	}
	// A database no binary migrated has recorded nothing, not even the table.
	err = checkPending(migrations, map[int]recorded{})
	if !errors.Is(err, ErrDatabaseOlder) || !strings.Contains(err.Error(), "1 (0001_init)") {
		t.Fatalf("an empty database was not refused naming 0001: %v", err)
	}
}

// pgvector reports `major.minor.patch`, and some builds `major.minor`. The
// floor is 0.7.0; anything below it, or anything unreadable, is refused and
// the refusal names the version, the floor and the command that upgrades.
func TestVectorFloorComparesPgvectorVersions(t *testing.T) {
	t.Parallel()
	for version, ok := range map[string]bool{
		"":        true, // no pgvector on the server: 0001 refuses with Postgres's own error
		"0.7.0":   true,
		"0.7":     true,
		"0.8.1":   true,
		"0.10.0":  true,
		"1.0.0":   true,
		"0.6.2":   false,
		"0.6":     false,
		"0.5.1":   false,
		"0":       false,
		"0.x.0":   false,
		"0.7.0.1": false,
	} {
		err := checkVectorFloor(version)
		if ok {
			if err != nil {
				t.Errorf("pgvector %q was refused: %v", version, err)
			}
			continue
		}
		if !errors.Is(err, ErrVectorTooOld) {
			t.Errorf("pgvector %q: want ErrVectorTooOld, got %v", version, err)
			continue
		}
		for _, want := range []string{"pgvector " + version + " is installed", "0.7.0", "ALTER EXTENSION vector UPDATE", "Nothing was applied"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("pgvector %q: the refusal does not say %q: %v", version, want, err)
			}
		}
	}
}

// The floor is read only while 0015 is still to apply: a database that
// recorded it already passed, and a run stopped below it never reaches it.
func TestVectorFloorAppliesOnlyWhile0015IsPending(t *testing.T) {
	t.Parallel()
	migrations := []migration{{Version: 14}, {Version: vectorFloorMigration}, {Version: 16}}
	if !pendingThrough(migrations, rows(map[int]string{14: ""}), 1<<30, vectorFloorMigration) {
		t.Error("0015 pending was not seen as pending")
	}
	if pendingThrough(migrations, rows(map[int]string{14: "", 15: ""}), 1<<30, vectorFloorMigration) {
		t.Error("0015 recorded was seen as pending")
	}
	if pendingThrough(migrations, map[int]recorded{}, 14, vectorFloorMigration) {
		t.Error("a run stopped at 0014 was seen as reaching 0015")
	}
}
