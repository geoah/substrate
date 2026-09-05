package blobbytes_test

import (
	"context"
	"errors"
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
	// Binding creates nothing: a repository that never stores a blob leaves
	// no directory behind.
	if _, err := os.Stat(filepath.Join(root, "repositories", "repokeys")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Repository() created the directory before any Put: %v", err)
	}
	data := []byte("bytes at a known path")
	digest := put(t, s, data)

	// <root>/repositories/<repository>/blobs/<digest>: the bytes sit inside
	// the repository directory, beside its changelog and sealed store, so a
	// copy of that one directory is the whole backup.
	path := filepath.Join(root, "repositories", "repokeys", "blobs", digest)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the object is not at %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("object mode is %o, want 600 — the bytes are stored in the clear", got)
	}
	for _, dir := range []string{
		filepath.Join(root, "repositories", "repokeys"),
		filepath.Join(root, "repositories", "repokeys", "blobs"),
	} {
		st, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat %s: %v", dir, err)
		}
		if got := st.Mode().Perm(); got != 0o700 {
			t.Fatalf("%s mode is %o, want 700", dir, got)
		}
	}
}

// List reads the blobs directory in digest order, honors the cursor and the
// limit, and reports an unmade directory as empty rather than as an error.
func TestFSList(t *testing.T) {
	t.Parallel()
	b := newFS(t)
	s, err := b.Repository("repolist", nil)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	ctx := context.Background()
	empty, err := s.List(ctx, "", 0)
	if err != nil {
		t.Fatalf("list a repository with no blobs directory: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("an unmade repository lists %d objects", len(empty))
	}

	bodies := []string{"alpha", "beta", "gamma", "delta"}
	sizes := map[string]int64{}
	for _, body := range bodies {
		sizes[put(t, s, []byte(body))] = int64(len(body))
	}
	all, err := s.List(ctx, "", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != len(bodies) {
		t.Fatalf("list returned %d objects, stored %d", len(all), len(bodies))
	}
	for i, o := range all {
		if i > 0 && all[i-1].Digest >= o.Digest {
			t.Fatalf("list is not in ascending digest order: %q before %q", all[i-1].Digest, o.Digest)
		}
		if sizes[o.Digest] != o.Size {
			t.Fatalf("%s lists as %d bytes, stored %d", o.Digest, o.Size, sizes[o.Digest])
		}
		if o.At.IsZero() {
			t.Fatalf("%s lists with no time", o.Digest)
		}
	}
	// The cursor and the limit are what let a sweep page through a store
	// larger than one batch: everything after the second digest, two at most.
	page, err := s.List(ctx, all[1].Digest, 2)
	if err != nil {
		t.Fatalf("list after a cursor: %v", err)
	}
	if len(page) != 2 || page[0].Digest != all[2].Digest || page[1].Digest != all[3].Digest {
		t.Fatalf("list after %s with limit 2 returned %+v, want the third and fourth", all[1].Digest, page)
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
		if err := os.WriteFile(filepath.Join(root, "repositories", "repostray", "blobs", name), []byte("x"), 0o600); err != nil {
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
