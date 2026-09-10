// Package blobbytes holds a repository's blob bytes, and nothing else. The
// blob manifest is a record in Postgres and stays the truth; only the bytes
// live here.
//
// There is one backend, `fs`: the bytes sit at
// <root>/repositories/<repository>/blobs/<digest>, inside the repository
// directory that is the backup unit, so one copy of the directory is a whole
// backup. Nothing selects anything else and there is no variable to set.
//
// A Store is bound to ONE repository before a caller can reach it. The
// repository is half of every key and no method takes one, so a caller holding
// a digest cannot address another repository's bytes: it is the bound key
// prefix and the digest grammar checked here.
package blobbytes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/geoah/substrate/internal/vocabulary"
)

// BackendFS names the one backend. It is what a snapshot and an export record
// in `snapshot.json` as the layout their blob bytes are written for.
const BackendFS = "fs"

// ErrNotStored is what a read or an open reports when the store holds no bytes
// under that digest. The engine maps it to a not-found.
var ErrNotStored = errors.New("blobbytes: no bytes are stored under this digest")

// reDigest matches a blob digest: the fixed prefix plus a sha-256 in lowercase
// hex. It is checked HERE as well as in the engine because the digest is a
// path segment, and one that could hold `/` or `..` would address bytes
// outside the repository it was handed to.
var reDigest = regexp.MustCompile(`^blob-sha256-[0-9a-f]{64}$`)

// checkDigest refuses anything that is not a blob digest.
func checkDigest(digest string) error {
	if !reDigest.MatchString(digest) {
		return fmt.Errorf("blobbytes: %q is not a blob digest", digest)
	}
	return nil
}

// checkRepository refuses anything that is not a repository id, which is the
// repository's authority (vocabulary.ValidRepositoryAuthority: lowercase DNS
// labels with at least one dot). The id is a path segment for the same reason
// the digest is, and the authority grammar admits neither `/` nor `.` alone,
// so a checked id names exactly one repository inside the store.
func checkRepository(repository string) error {
	if !vocabulary.ValidRepositoryAuthority(repository) {
		return fmt.Errorf("blobbytes: %q is not a repository id (an authority such as ada.example.com)", repository)
	}
	return nil
}

// Object is one stored object, as a listing reports it. The time is what the
// unreferenced-upload grace and the orphan sweep read.
type Object struct {
	Digest string
	Size   int64
	At     time.Time
}

// Store is one repository's blob bytes. Every method is idempotent: putting
// bytes that are already there and deleting bytes that are not there both
// succeed, so a sweep can run again without special cases.
//
// Put takes an io.Reader and Open returns an io.ReadCloser so that a later
// streaming read path (range requests, a cap above 64 MiB) is a change to the
// callers rather than to the backend.
type Store interface {
	// Put writes exactly size bytes read from r under digest. The caller has
	// already hashed the bytes: the digest is the key, not a claim this store
	// re-checks.
	Put(ctx context.Context, digest string, size int64, r io.Reader) error
	// Open returns the stored bytes, or ErrNotStored.
	Open(ctx context.Context, digest string) (io.ReadCloser, error)
	// Exists reports whether the bytes are durable. It is the probe behind the
	// engine's "a manifest is stored only once its bytes exist" invariant.
	Exists(ctx context.Context, digest string) (bool, error)
	// Delete removes the bytes. An absent object is not an error.
	Delete(ctx context.Context, digest string) error
	// List reports the objects this repository holds whose digest sorts after
	// `after`, in ascending digest order, at most limit of them (limit <= 0 is
	// every object). The order and the cursor are what let the orphan sweep
	// walk a store larger than one batch without starving its tail.
	List(ctx context.Context, after string, limit int) ([]Object, error)
}

// Backend is the byte store before it is bound to a repository. One Backend
// serves every repository the process opens. *FS is the only implementation;
// the interface stays because the engine takes it as an option and a test
// substitutes a store that refuses.
type Backend interface {
	// Repository binds the backend to one repository.
	Repository(repository string) (Store, error)
}

// ReadAll reads a stored object whole, refusing one longer than size. It is
// what the engine's non-streaming GetBlob uses, and size is what the manifest
// declares: an object that outgrew its manifest is a corrupt store, not a
// bigger blob, and reading it whole into memory is how a 64 MiB cap gets
// exceeded from the outside.
func ReadAll(ctx context.Context, s Store, digest string, size int64) ([]byte, error) {
	rc, err := s.Open(ctx, digest)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(io.LimitReader(rc, size+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != size {
		return nil, fmt.Errorf("blobbytes: %s holds %d bytes or more, its manifest says %d", digest, len(data), size)
	}
	return data, nil
}
