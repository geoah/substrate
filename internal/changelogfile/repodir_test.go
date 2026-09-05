package changelogfile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepoDir(t *testing.T) {
	root := t.TempDir()
	got, err := RepoDir(root, "k3j9x2m41pfq")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "repositories", "k3j9x2m41pfq"); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	if ChangelogDir(got) != filepath.Join(got, "changelog") || BlobsDir(got) != filepath.Join(got, "blobs") || SealedDir(got) != filepath.Join(got, "sealed") {
		t.Fatal("subdirectory names")
	}
	for _, id := range []string{"a", "A-b_c.d", strings.Repeat("x", 128)} {
		if _, err := RepoDir(root, id); err != nil {
			t.Errorf("%q: %v", id, err)
		}
	}
	bad := []string{"", ".", "..", "/", "a/b", "/abs", "a\\b", "a b", "a:b", "é", strings.Repeat("x", 129), "../../etc"}
	for _, id := range bad {
		if _, err := RepoDir(root, id); !errors.Is(err, ErrRepositoryID) {
			t.Errorf("RepoDir(%q): err = %v, want ErrRepositoryID", id, err)
		}
		if _, err := EnsureRepoDir(root, id); !errors.Is(err, ErrRepositoryID) {
			t.Errorf("EnsureRepoDir(%q): err = %v, want ErrRepositoryID", id, err)
		}
	}
	for _, rel := range []string{"", "relative", "./x"} {
		if _, err := RepoDir(rel, "abc"); err == nil {
			t.Errorf("RepoDir(%q, abc) accepted a relative root", rel)
		}
		if _, err := ListRepositoryDirs(rel); err == nil {
			t.Errorf("ListRepositoryDirs(%q) accepted a relative root", rel)
		}
	}
}

func TestEnsureRepoDirAndList(t *testing.T) {
	root := t.TempDir()
	ids, err := ListRepositoryDirs(root)
	if err != nil || len(ids) != 0 {
		t.Fatalf("fresh root lists %v, %v", ids, err)
	}
	for _, id := range []string{"zeta", "alpha"} {
		dir, err := EnsureRepoDir(root, id)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range []string{dir, ChangelogDir(dir), BlobsDir(dir), SealedDir(dir)} {
			info, err := os.Stat(d)
			if err != nil {
				t.Fatal(err)
			}
			if !info.IsDir() || info.Mode().Perm() != dirMode {
				t.Fatalf("%s: mode %v", d, info.Mode().Perm())
			}
		}
		// A second call is a no-op.
		if _, err := EnsureRepoDir(root, id); err != nil {
			t.Fatal(err)
		}
	}
	// A file under repositories/ is ignored.
	if err := os.WriteFile(filepath.Join(root, RepositoriesDir, "notes.txt"), []byte("x"), fileMode); err != nil {
		t.Fatal(err)
	}
	ids, err = ListRepositoryDirs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "alpha" || ids[1] != "zeta" {
		t.Fatalf("ids = %v", ids)
	}
	// A dot-prefixed directory is a filesystem's or a tool's, never a
	// repository: skipped, so a `.snapshot` under the root does not refuse
	// the boot.
	if err := os.Mkdir(filepath.Join(root, RepositoriesDir, ".snapshot"), dirMode); err != nil {
		t.Fatal(err)
	}
	ids, err = ListRepositoryDirs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "alpha" || ids[1] != "zeta" {
		t.Fatalf("ids with a .snapshot beside them = %v", ids)
	}
	// A directory that is not a repository id is refused, not skipped.
	if err := os.Mkdir(filepath.Join(root, RepositoriesDir, "not an id"), dirMode); err != nil {
		t.Fatal(err)
	}
	if _, err := ListRepositoryDirs(root); !errors.Is(err, ErrRepositoryID) {
		t.Fatalf("err = %v, want ErrRepositoryID", err)
	}
}
