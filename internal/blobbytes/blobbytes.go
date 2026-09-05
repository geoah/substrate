// Package blobbytes holds a repository's blob bytes, and nothing else. The
// blob manifest is a record in Postgres and stays the truth; only the bytes
// live here.
//
// Two backends are runtime choices: `fs` (the default) keeps the bytes at
// <root>/repositories/<repository>/blobs/<digest>, inside the repository
// directory that is the backup unit, and `s3` keeps them at
// <prefix><repository>/<digest> under a bucket. A third, `postgres`, reads
// the `blobs` bytea column the bytes used to live in; it is a MIGRATION
// SOURCE for `substratectl blobs migrate --from postgres` and nothing else,
// and the engine refuses to boot while the column still holds rows.
//
// A Store is bound to ONE repository before a caller can reach it. The
// repository is half of every key and no method takes one, so a caller holding
// a digest cannot address another repository's bytes: for postgres that is row
// level security, for fs and s3 it is the bound key prefix and the digest
// grammar checked here.
package blobbytes

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"
)

// The backend names, which are also the SUBSTRATE_BLOB_STORE values and what
// the backend-switch guard compares.
const (
	BackendPostgres = "postgres"
	BackendFS       = "fs"
	BackendS3       = "s3"
)

// ErrNotStored is what a read or an open reports when the store holds no bytes
// under that digest. The engine maps it to a not-found.
var ErrNotStored = errors.New("blobbytes: no bytes are stored under this digest")

// reDigest matches a blob digest: the fixed prefix plus a sha-256 in lowercase
// hex. It is checked HERE as well as in the engine because for fs and s3 the
// digest is a path segment, and a digest that could hold `/` or `..` would
// address bytes outside the repository it was handed to.
var reDigest = regexp.MustCompile(`^blob-sha256-[0-9a-f]{64}$`)

// reRepository matches a repository id. The id is a path segment for the same
// reason, and the engine mints it from a restricted alphabet.
var reRepository = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

// checkDigest refuses anything that is not a blob digest.
func checkDigest(digest string) error {
	if !reDigest.MatchString(digest) {
		return fmt.Errorf("blobbytes: %q is not a blob digest", digest)
	}
	return nil
}

// checkRepository refuses anything that is not a repository id. `.` and `..`
// match the character class and are refused by name: either one would address
// the store's root or its parent rather than one repository inside it.
func checkRepository(repository string) error {
	if !reRepository.MatchString(repository) || repository == "." || repository == ".." {
		return fmt.Errorf("blobbytes: %q is not a repository id", repository)
	}
	return nil
}

// Object is one stored object, as a listing reports it. The time is what the
// unreferenced-upload grace and the orphan sweep read; the size is what a
// migration passes to the store it is copying into.
type Object struct {
	Digest string
	Size   int64
	At     time.Time
}

// Store is one repository's blob bytes. Every method is idempotent: putting
// bytes that are already there and deleting bytes that are not there both
// succeed, so a sweep or a resumed migration can run again without special
// cases.
//
// Put takes an io.Reader and Open returns an io.ReadCloser so that a later
// streaming read path (range requests, a cap above 64 MiB) is a change to the
// callers rather than to the backends.
type Store interface {
	// Backend names the backend this store belongs to: one of BackendPostgres,
	// BackendFS, BackendS3.
	Backend() string
	// Put writes exactly size bytes read from r under digest. The bytes must
	// hash to digest: the s3 backend signs the request with that hash, so a
	// reader that disagrees with its digest is refused by the endpoint.
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

// DB is the subset of *sql.DB and *sql.Tx the postgres backend runs on. Only
// that backend uses it; fs and s3 ignore the handle they are given.
type DB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Backend is the configured store, before it is bound to a repository. One
// Backend serves every repository the process opens.
type Backend interface {
	// Name is the backend's name, the same value its stores report.
	Name() string
	// Repository binds the backend to one repository. db is that repository's
	// row-level-security-scoped pool: the postgres backend runs its statements
	// on it, and the other two ignore it.
	Repository(repository string, db DB) (Store, error)
}

// InTransaction is implemented by a backend whose bytes settle inside the
// caller's database transaction, which is the postgres backend and only the
// postgres backend. The engine keeps the one-transaction settle wherever it is
// offered: bytes and manifest commit together, so no reader ever sees a stored
// manifest whose bytes are missing and no crash can leave an orphan. Where it
// is not offered the engine writes the bytes between two transactions instead
// (engine/blobs.go).
type InTransaction interface {
	Store
	// PutTx inserts the bytes inside the caller's transaction. It carries the
	// name and mime type because this backend's own table has held those
	// columns since before the manifest did; a read reports the manifest's.
	PutTx(ctx context.Context, tx DB, b Blob) error
	// ExistsTx is Exists inside the caller's transaction, so it sees a PutTx
	// the same transaction has not committed yet.
	ExistsTx(ctx context.Context, tx DB, digest string) (bool, error)
	// DeleteTx is Delete inside the caller's transaction, so the bytes and the
	// manifest tombstone go together.
	DeleteTx(ctx context.Context, tx DB, digest string) error
}

// Blob is one row of the postgres backend's own table.
type Blob struct {
	Digest    string
	Name      string
	MediaType string
	Size      int64
	Bytes     []byte
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
