package archive

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// Dir is a store in a directory: a key is a path under its root. It is the
// archive's default, under the data root.
type Dir struct {
	root string
}

// NewDir returns the store rooted at root, which must be an absolute path. The
// directory is created at the first put.
func NewDir(root string) (*Dir, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("archive: the directory %q is not an absolute path", root)
	}
	return &Dir{root: filepath.Clean(root)}, nil
}

// Root is the directory the store is rooted at.
func (d *Dir) Root() string { return d.root }

func (d *Dir) String() string { return "dir:" + d.root }

// tempMark names a file a put is writing: `~` is outside the key alphabet, so
// no listing reports one and no key reaches one.
const tempMark = ".~put-"

func (d *Dir) path(key string) (string, error) {
	if err := CheckKey(key); err != nil {
		return "", err
	}
	return filepath.Join(d.root, filepath.FromSlash(key)), nil
}

// Put writes the body to a temporary file beside the key's, flushes it and
// renames it into place, then flushes the directory, so a crash leaves the
// old object or the new one and never a part.
func (d *Dir) Put(ctx context.Context, key string, body io.ReadSeeker, size int64) error {
	path, err := d.path(key)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.place(path, func(f *os.File) error {
		n, err := io.Copy(f, body)
		if err != nil {
			return err
		}
		if size >= 0 && n != size {
			return fmt.Errorf("archive: %s: the body held %d bytes, the put declared %d", key, n, size)
		}
		return nil
	})
}

// Link puts the file at path under key by a hard link, so a blob the server
// holds costs no second copy in an archive on the same file system. Across
// file systems it copies.
func (d *Dir) Link(ctx context.Context, key, src string) error {
	path, err := d.path(key)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := makeDirs(dir); err != nil {
		return err
	}
	tmp := filepath.Join(dir, filepath.Base(path)+tempMark+randomSuffix())
	if err := os.Link(src, tmp); err != nil {
		if !errors.Is(err, syscall.EXDEV) && !errors.Is(err, syscall.EPERM) && !errors.Is(err, syscall.ENOTSUP) {
			return err
		}
		f, err := os.Open(src)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		return d.Put(ctx, key, f, -1)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return syncDir(dir)
}

func (d *Dir) place(path string, write func(*os.File) error) error {
	dir := filepath.Dir(path)
	if err := makeDirs(dir); err != nil {
		return err
	}
	tmp := filepath.Join(dir, filepath.Base(path)+tempMark+randomSuffix())
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			_ = f.Close()
			_ = os.Remove(tmp)
		}
	}()
	if err := write(f); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	done = true
	return syncDir(dir)
}

func (d *Dir) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	path, err := d.path(key)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	if err != nil {
		return nil, err
	}
	if st, err := f.Stat(); err == nil && st.IsDir() {
		_ = f.Close()
		return nil, fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	return f, nil
}

// List walks the deepest directory the prefix names whole and reports every
// file under it whose key starts with the prefix. A file whose name is not a
// key (a put's temporary file, anything an operator left) is not reported.
func (d *Dir) List(ctx context.Context, prefix string) ([]Object, error) {
	if err := checkPrefix(prefix); err != nil {
		return nil, err
	}
	start := d.root
	if i := strings.LastIndex(prefix, "/"); i >= 0 {
		start = filepath.Join(d.root, filepath.FromSlash(prefix[:i]))
	}
	var out []Object
	err := filepath.WalkDir(start, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(d.root, path)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if !strings.HasPrefix(key, prefix) || CheckKey(key) != nil || !e.Type().IsRegular() {
			return nil
		}
		info, err := e.Info()
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		out = append(out, Object{Key: key, Size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (d *Dir) Delete(ctx context.Context, key string) error {
	path, err := d.path(key)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	// The removal is durable before Delete returns: a delete the caller
	// builds on (a repository's archive deleted before its rows) must not
	// come back after a power loss. A file already gone may be gone from
	// the page cache alone, so its directory is flushed too.
	if err := syncDir(filepath.Dir(path)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// makeDirs creates dir and every parent it lacks, and flushes the parent of
// each one it created, so the path to an object survives a crash with it.
func makeDirs(dir string) error {
	var created []string
	for p := dir; ; p = filepath.Dir(p) {
		if _, err := os.Stat(p); err == nil {
			break
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		created = append(created, p)
		if filepath.Dir(p) == p {
			break
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for i := len(created) - 1; i >= 0; i-- {
		if err := syncDir(filepath.Dir(created[i])); err != nil {
			return err
		}
	}
	return nil
}

func randomSuffix() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// syncHook, when set (tests only), sees every directory syncDir flushes.
var syncHook func(dir string)

// syncDir flushes a directory's entries, so a rename into it survives a crash.
func syncDir(dir string) error {
	if syncHook != nil {
		syncHook(dir)
	}
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return f.Sync()
}
