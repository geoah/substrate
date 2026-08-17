package blobbytes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// dirMode and fileMode keep the store readable by the substrate's own user and
// nobody else. The bytes are stored in the clear, so on a shared box the mode
// is the only thing between another local account and every repository's
// attachments.
const (
	dirMode  os.FileMode = 0o700
	fileMode os.FileMode = 0o600
)

// tmpPrefix marks a half-written object. A name carrying it can never be
// mistaken for a digest, so List skips it and a reader never opens one.
const tmpPrefix = ".incoming-"

// FS keeps blob bytes in a directory, one subdirectory per repository:
// <root>/<repository>/<digest>. It is the single-box and compose answer.
//
// What it gives up against the postgres backend, both deliberately:
// a database dump is no longer a whole backup (the root is the second half,
// and docs/operations.md says so), and row level security no longer reaches
// the bytes — the repository is a directory, and anything that can read the
// root can read every repository.
type FS struct{ root string }

// NewFS opens the filesystem backend rooted at root, creating it if it is not
// there. The root must be an absolute path: a relative one would follow the
// process's working directory, and a store that moves when the server is
// restarted from another directory is a store that has lost its bytes.
func NewFS(root string) (*FS, error) {
	if root == "" {
		return nil, errors.New("blobbytes: the fs backend needs a root directory")
	}
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("blobbytes: the fs root %q must be an absolute path", root)
	}
	if err := os.MkdirAll(root, dirMode); err != nil {
		return nil, fmt.Errorf("blobbytes: create the fs root: %w", err)
	}
	return &FS{root: filepath.Clean(root)}, nil
}

// Name is BackendFS.
func (*FS) Name() string { return BackendFS }

// Repository binds the backend to one repository's directory. The id is
// checked against the repository grammar first, so the directory is always
// exactly one segment below the root.
func (f *FS) Repository(repository string, _ DB) (Store, error) {
	if err := checkRepository(repository); err != nil {
		return nil, err
	}
	return &fsStore{dir: filepath.Join(f.root, repository)}, nil
}

type fsStore struct{ dir string }

func (*fsStore) Backend() string { return BackendFS }

// path is the one place a key is built. The digest is checked against the
// digest grammar, so it is one path segment and cannot escape the directory.
func (f *fsStore) path(digest string) (string, error) {
	if err := checkDigest(digest); err != nil {
		return "", err
	}
	return filepath.Join(f.dir, digest), nil
}

// Put writes the bytes through a temporary file and renames it into place.
// Rename within a directory is atomic, so a reader sees either no object or
// the whole object, never a prefix of one. The file and then the directory are
// fsynced, so a machine that loses power after Put returns still has the bytes
// the manifest is about to claim.
func (f *fsStore) Put(ctx context.Context, digest string, size int64, r io.Reader) error {
	final, err := f.path(digest)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(f.dir, dirMode); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(f.dir, tmpPrefix+"*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()
	if err := tmp.Chmod(fileMode); err != nil {
		return err
	}
	n, err := io.Copy(tmp, r)
	if err != nil {
		return err
	}
	if n != size {
		return fmt.Errorf("blobbytes: got %d bytes, the caller promised %d", n, size)
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, final); err != nil {
		return err
	}
	return syncDir(f.dir)
}

func (f *fsStore) Open(_ context.Context, digest string) (io.ReadCloser, error) {
	name, err := f.path(digest)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrNotStored, digest)
	}
	if err != nil {
		return nil, err
	}
	return file, nil
}

func (f *fsStore) Exists(_ context.Context, digest string) (bool, error) {
	name, err := f.path(digest)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(name)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (f *fsStore) Delete(_ context.Context, digest string) error {
	name, err := f.path(digest)
	if err != nil {
		return err
	}
	if err := os.Remove(name); err != nil {
		// Bytes that are not there are already deleted, and the directory may
		// not exist at all: a repository that never stored a blob has none.
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	// The unlink itself is a directory change, so it is flushed like the
	// rename: a crash must not resurrect a collected blob.
	return syncDir(f.dir)
}

// List walks the repository's directory. os.ReadDir sorts by filename, and a
// digest is its filename, so the entries come back in the order the sweep's
// cursor expects. Anything that is not a digest — a half-written temporary,
// something an operator dropped in — is skipped rather than reported as a blob.
func (f *fsStore) List(_ context.Context, after string, limit int) ([]Object, error) {
	entries, err := os.ReadDir(f.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Object
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, tmpPrefix) || !reDigest.MatchString(name) || name <= after {
			continue
		}
		info, err := e.Info()
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, Object{Digest: name, Size: info.Size(), At: info.ModTime().UTC()})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// syncDir flushes a directory entry (the rename, the unlink) to disk. Without
// it the file's contents are durable but the name pointing at them may not be.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	return d.Sync()
}
