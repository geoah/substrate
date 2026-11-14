// Package archive holds the stores a repository's archive is written to: a
// directory, or a bucket of an S3-compatible service (decision 0262). A store
// has four operations, put, get, list and delete, because that is what every
// object store offers: nothing appends, renames or locks, so the archive's
// layout is made of objects that are written whole and never changed in place
// (docs/stored-format.md).
//
// A store knows nothing of repositories. The engine's archiver decides what
// goes where; a store only holds bytes under keys.
package archive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// ErrNotFound is what Get reports for a key the store holds nothing under.
var ErrNotFound = errors.New("archive: the store holds no object under this key")

// Object is one stored object, as a listing reports it.
type Object struct {
	Key  string
	Size int64
}

// Store is where an archive is written.
//
// Put replaces whatever the key held, and a reader never sees a part of it:
// before the put returns the key holds the old object or none, after it the
// new one. A listing that follows a put lists the key. Delete of a key that
// holds nothing succeeds. The archiver relies on nothing more.
type Store interface {
	Put(ctx context.Context, key string, body io.ReadSeeker, size int64) error
	// Get opens the object under key, or reports ErrNotFound.
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// List reports every object whose key starts with prefix, in key order.
	List(ctx context.Context, prefix string) ([]Object, error)
	Delete(ctx context.Context, key string) error
	// String names the store for a log line and for the mark: where it is,
	// never a credential. Two stores with one name hold the same objects.
	String() string
}

// Linker is a store that can take a local file without copying its bytes: a
// directory store on the file system the file is on links it. A blob's bytes
// are written once and never change, so a link is a copy that costs nothing.
type Linker interface {
	// Link puts the file at path under key, as Put would.
	Link(ctx context.Context, key, path string) error
}

// maxKey bounds a key so a directory store's path stays well inside PATH_MAX
// and an S3 key inside its 1,024 bytes once a prefix is added.
const maxKey = 512

// reSegment is one segment of a key: the record id alphabet and the dot, so a
// key is the same path on every file system and needs no escaping in a URL.
var reSegment = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// CheckKey refuses a key that is not segments of the key alphabet joined by
// `/`: an empty segment, a `.` or `..` segment, a leading or trailing `/`.
// Every store checks every key it is handed, because a key is a path in a
// directory store and one that climbed out of it would reach other files.
func CheckKey(key string) error {
	if key == "" || len(key) > maxKey {
		return fmt.Errorf("archive: %q is not a key", key)
	}
	for _, seg := range strings.Split(key, "/") {
		if !reSegment.MatchString(seg) || seg == "." || seg == ".." {
			return fmt.Errorf("archive: %q is not a key", key)
		}
	}
	return nil
}

// checkPrefix refuses a listing prefix that no key could start with in a way
// that escapes the store: the empty prefix lists everything, and any other is
// a key, or a key followed by `/`.
func checkPrefix(prefix string) error {
	if prefix == "" {
		return nil
	}
	return CheckKey(strings.TrimSuffix(prefix, "/"))
}

// ReadAll reads the whole object under key.
func ReadAll(ctx context.Context, s Store, key string) ([]byte, error) {
	rc, err := s.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}
