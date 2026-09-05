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
	got, err := RepoDir(root, "ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "repositories", "ada.example.com"); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	if ChangelogDir(got) != filepath.Join(got, "changelog") || BlobsDir(got) != filepath.Join(got, "blobs") || SealedDir(got) != filepath.Join(got, "sealed") {
		t.Fatal("subdirectory names")
	}
	// The grammar is the repository authority's (vocabulary.ValidRepositoryAuthority):
	// lowercase DNS labels, at least one dot, up to 253 bytes.
	for _, id := range []string{"a.b", "ada-1.example.com", longAuthority(200)} {
		if _, err := RepoDir(root, id); err != nil {
			t.Errorf("%q: %v", id, err)
		}
	}
	bad := []string{
		"", ".", "..", "/", "a/b", "/abs", "a\\b", "a b", "a:b", "é", "../../etc",
		// The old random id: no dot.
		"k3j9x2m41pfq",
		// A capital letter, a leading dot, an underscore, an over-long label,
		// and one byte over the DNS limit.
		"Ada.example.com", ".example.com", "ada_1.example.com",
		strings.Repeat("x", 64) + ".example.com", longAuthority(254),
	}
	for _, id := range bad {
		if _, err := RepoDir(root, id); !errors.Is(err, ErrRepositoryAuthority) {
			t.Errorf("RepoDir(%q): err = %v, want ErrRepositoryAuthority", id, err)
		}
		if _, err := EnsureRepoDir(root, id); !errors.Is(err, ErrRepositoryAuthority) {
			t.Errorf("EnsureRepoDir(%q): err = %v, want ErrRepositoryAuthority", id, err)
		}
	}
	for _, rel := range []string{"", "relative", "./x"} {
		if _, err := RepoDir(rel, "ada.example.com"); err == nil {
			t.Errorf("RepoDir(%q, ada.example.com) accepted a relative root", rel)
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
	for _, id := range []string{"zeta.example.com", "alpha.example.com"} {
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
	if len(ids) != 2 || ids[0] != "alpha.example.com" || ids[1] != "zeta.example.com" {
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
	if len(ids) != 2 || ids[0] != "alpha.example.com" || ids[1] != "zeta.example.com" {
		t.Fatalf("ids with a .snapshot beside them = %v", ids)
	}
	// A directory that is not an authority is refused, not skipped: a
	// pre-authority id first, which the legacy list finds instead.
	if err := os.Mkdir(filepath.Join(root, RepositoriesDir, "k3j9x2m41pfq"), dirMode); err != nil {
		t.Fatal(err)
	}
	if _, err := ListRepositoryDirs(root); !errors.Is(err, ErrRepositoryAuthority) {
		t.Fatalf("err = %v, want ErrRepositoryAuthority", err)
	}
	legacy, err := ListLegacyRepositoryDirs(root)
	if err != nil || len(legacy) != 1 || legacy[0] != "k3j9x2m41pfq" {
		t.Fatalf("legacy dirs = %v, %v", legacy, err)
	}
	// Then a name neither grammar admits, which both lists refuse.
	if err := os.Mkdir(filepath.Join(root, RepositoriesDir, "not an id"), dirMode); err != nil {
		t.Fatal(err)
	}
	if _, err := ListRepositoryDirs(root); !errors.Is(err, ErrRepositoryAuthority) {
		t.Fatalf("err = %v, want ErrRepositoryAuthority", err)
	}
	if _, err := ListLegacyRepositoryDirs(root); !errors.Is(err, ErrLegacyRepositoryDir) {
		t.Fatalf("legacy list err = %v, want ErrLegacyRepositoryDir", err)
	}
}

// longAuthority builds a valid authority of exactly n bytes out of 63-byte
// labels, for the length rule.
func longAuthority(n int) string {
	var labels []string
	for n > 0 {
		l := min(n, 63)
		labels = append(labels, strings.Repeat("a", l))
		n -= l + 1
	}
	return strings.Join(labels, ".")
}

// RenameRepoDir moves a pre-authority directory under its authority, once:
// the old name must be of the old grammar, the new one an authority, and an
// occupied target is refused.
func TestRenameRepoDir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, RepositoriesDir, "k3j9x2m41pfq", "changelog"), dirMode); err != nil {
		t.Fatal(err)
	}
	dst, err := RenameRepoDir(root, "k3j9x2m41pfq", "ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, RepositoriesDir, "ada.example.com"); dst != want {
		t.Fatalf("renamed to %s, want %s", dst, want)
	}
	if _, err := os.Stat(filepath.Join(dst, "changelog")); err != nil {
		t.Fatalf("the contents did not move: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, RepositoriesDir, "k3j9x2m41pfq")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the old directory is still there: %v", err)
	}
	legacy, err := ListLegacyRepositoryDirs(root)
	if err != nil || len(legacy) != 0 {
		t.Fatalf("legacy dirs after the rename = %v, %v", legacy, err)
	}
	// The target is taken.
	if err := os.MkdirAll(filepath.Join(root, RepositoriesDir, "other12345ab"), dirMode); err != nil {
		t.Fatal(err)
	}
	if _, err := RenameRepoDir(root, "other12345ab", "ada.example.com"); err == nil {
		t.Fatal("renamed onto an existing directory")
	}
	// Neither side may be of the wrong grammar.
	if _, err := RenameRepoDir(root, "other12345ab", "k3j9x2m41pfq"); !errors.Is(err, ErrRepositoryAuthority) {
		t.Fatalf("a non-authority target: err = %v", err)
	}
	if _, err := RenameRepoDir(root, "../etc", "grace.example.com"); !errors.Is(err, ErrLegacyRepositoryDir) {
		t.Fatalf("a path-escaping source: err = %v", err)
	}
}
