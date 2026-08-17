package blobbytes_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/blobbytes"
)

func newFS(t *testing.T) *blobbytes.FS {
	t.Helper()
	b, err := blobbytes.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("open the fs backend: %v", err)
	}
	return b
}

func TestFSStore(t *testing.T) {
	t.Parallel()
	b := newFS(t)
	conformance(t, func(t *testing.T, repository string) blobbytes.Store {
		t.Helper()
		s, err := b.Repository(repository, nil)
		if err != nil {
			t.Fatalf("bind %s: %v", repository, err)
		}
		return s
	})
}

func TestFSRepositoryIsolation(t *testing.T) {
	t.Parallel()
	b := newFS(t)
	repositoryIsolation(t, func(t *testing.T, repository string) blobbytes.Store {
		t.Helper()
		s, err := b.Repository(repository, nil)
		if err != nil {
			t.Fatalf("bind %s: %v", repository, err)
		}
		return s
	})
	refuseBadRepository(t, b, nil)
}

func TestFSRefusesARelativeRoot(t *testing.T) {
	t.Parallel()
	// A relative root follows the process's working directory, so a server
	// restarted from elsewhere would look for its bytes somewhere else.
	if _, err := blobbytes.NewFS("blobs"); err == nil {
		t.Fatal("the fs backend accepted a relative root")
	}
	if _, err := blobbytes.NewFS(""); err == nil {
		t.Fatal("the fs backend accepted an empty root")
	}
}

func TestFSKeyShapeAndModes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	b, err := blobbytes.NewFS(root)
	if err != nil {
		t.Fatalf("open the fs backend: %v", err)
	}
	s, err := b.Repository("repokeys", nil)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	data := []byte("bytes at a known path")
	digest := put(t, s, data)

	// <root>/<repository>/<digest>, which is the key shape an operator backs
	// up and an s3 bucket mirrors.
	path := filepath.Join(root, "repokeys", digest)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the object is not at %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("object mode is %o, want 600 — the bytes are stored in the clear", got)
	}
	dir, err := os.Stat(filepath.Join(root, "repokeys"))
	if err != nil {
		t.Fatalf("stat the repository directory: %v", err)
	}
	if got := dir.Mode().Perm(); got != 0o700 {
		t.Fatalf("repository directory mode is %o, want 700", got)
	}
}

func TestFSListSkipsWhatIsNotABlob(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	b, err := blobbytes.NewFS(root)
	if err != nil {
		t.Fatalf("open the fs backend: %v", err)
	}
	s, err := b.Repository("repostray", nil)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	put(t, s, []byte("a real blob"))
	// A half-written upload and something an operator dropped in. Neither is
	// an object, and the sweep must not offer either as one to delete.
	for _, name := range []string{".incoming-1234", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(root, "repostray", name), []byte("x"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	objects, err := s.List(context.Background(), "", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(objects) != 1 {
		t.Fatalf("list returned %d entries, want the one blob", len(objects))
	}
	if !strings.HasPrefix(objects[0].Digest, "blob-sha256-") {
		t.Fatalf("list returned %q, which is not a digest", objects[0].Digest)
	}
}
