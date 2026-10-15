package changelogfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Spooled requests list in arrival order, only under their own names: a
// half-written file, a file set aside and a stray are skipped, a removed one
// is gone, and a removal of a file already gone is not an error.
func TestSpoolFilesListInArrivalOrder(t *testing.T) {
	repoDir := filepath.Join(t.TempDir(), "repositories", "ada.example.com")
	if files, err := SpoolFiles(repoDir); err != nil || len(files) != 0 {
		t.Fatalf("a missing spool lists %v, %v", files, err)
	}
	later, earlier, aside := SpoolFileName(2000, "hook-b"), SpoolFileName(1000, "hook-a"), SpoolFileName(3000, "hook-c")
	for _, name := range []string{later, earlier, aside} {
		if err := WriteSpoolFile(repoDir, name, []byte(name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := SetSpoolFileAside(repoDir, aside); err != nil {
		t.Fatal(err)
	}
	for _, stray := range []string{tmpPrefix + "x", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(SpoolDir(repoDir), stray), []byte("x"), fileMode); err != nil {
			t.Fatal(err)
		}
	}
	files, err := SpoolFiles(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Name != earlier || files[1].Name != later || files[0].Size != int64(len(earlier)) {
		t.Fatalf("spool lists %+v, want %s then %s", files, earlier, later)
	}
	ordinal, fid, err := ParseSpoolFileName(earlier)
	if err != nil || ordinal != 1000 || fid != "hook-a" {
		t.Fatalf("parse %s = %d, %q, %v", earlier, ordinal, fid, err)
	}
	if err := RemoveSpoolTemps(repoDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(SpoolDir(repoDir), tmpPrefix+"x")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the half-written file survived: %v", err)
	}
	for range 2 {
		if err := RemoveSpoolFile(repoDir, earlier); err != nil {
			t.Fatal(err)
		}
	}
	if files, _ := SpoolFiles(repoDir); len(files) != 1 || files[0].Name != later {
		t.Fatalf("after the removal the spool lists %+v", files)
	}
	if _, err := ReadSpoolFile(repoDir, "../escape.webhook"); !errors.Is(err, ErrSpoolName) {
		t.Fatalf("a name outside the grammar = %v", err)
	}
}
