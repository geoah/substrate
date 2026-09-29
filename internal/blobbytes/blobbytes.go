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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
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

// ErrDigestMismatch is what a read reports when the bytes stored under a
// digest are not that digest's blob: they hash to another digest, or they are
// shorter or longer than the size the manifest declares. The message names the
// digest. The engine maps it to substrate.ErrCorrupt.
var ErrDigestMismatch = errors.New("blobbytes: the stored bytes do not match their digest")

// digestPrefix is substrate.BlobDigestPrefix, spelled here because a read
// rebuilds the digest from the hash it computed and this package does not
// import the contract.
const digestPrefix = "blob-sha256-"

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
	// Open returns the stored bytes as they are, or ErrNotStored. It does not
	// hash them: a caller that serves the bytes reads through OpenVerified or
	// ReadAll. `repository verify` reads them raw, because it reports the
	// digest the bytes do hash to.
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

// ReadAll reads a stored object whole through OpenVerified, so it returns the
// bytes of digest or an error: ErrDigestMismatch when the stored bytes hash to
// another digest or are not size bytes long. It is what the engine's
// non-streaming GetBlob uses, and size is what the manifest declares: an
// object that outgrew its manifest is a corrupt store, not a bigger blob, and
// reading it whole into memory is how a 64 MiB cap gets exceeded from the
// outside.
func ReadAll(ctx context.Context, s Store, digest string, size int64) ([]byte, error) {
	rc, err := OpenVerified(ctx, s, digest, size)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// OpenVerified opens the bytes stored under digest for a streaming read that
// hashes as it goes. It never hands out more than size bytes, and it holds
// back the chunk that reaches size until the object is known to end there and
// the SHA-256 of everything read is the digest. A mismatch is therefore
// reported before the reader gives out the blob's last byte: a caller copying
// it into a response or an archive has written a strict prefix at most when
// the error (ErrDigestMismatch, naming the digest) arrives, and cuts the
// stream on it.
func OpenVerified(ctx context.Context, s Store, digest string, size int64) (io.ReadCloser, error) {
	if size < 0 {
		return nil, fmt.Errorf("blobbytes: %s: a verified read needs the size the manifest declares, got %d", digest, size)
	}
	rc, err := s.Open(ctx, digest)
	if err != nil {
		return nil, err
	}
	return &verifiedReader{rc: rc, digest: digest, size: size, h: sha256.New()}, nil
}

// verifiedReader is OpenVerified's reader. err is sticky: io.EOF once the
// last chunk is out, the mismatch or the store's error otherwise.
type verifiedReader struct {
	rc     io.ReadCloser
	digest string
	size   int64
	n      int64
	h      hash.Hash
	err    error
}

func (v *verifiedReader) Read(p []byte) (int, error) {
	if v.err != nil {
		return 0, v.err
	}
	if v.n == v.size {
		// Only an empty blob arrives here: every other one is finished by the
		// chunk that reaches its size, below.
		if v.err = v.finish(); v.err == nil {
			v.err = io.EOF
		}
		return 0, v.err
	}
	if rest := v.size - v.n; int64(len(p)) > rest {
		p = p[:rest]
	}
	k, err := v.rc.Read(p)
	_, _ = v.h.Write(p[:k])
	v.n += int64(k)
	switch {
	case v.n == v.size:
		// The chunk that completes the blob is handed out only once the
		// whole blob checks, so on a mismatch the caller never holds it.
		if v.err = v.finish(); v.err != nil {
			return 0, v.err
		}
		v.err = io.EOF
		return k, nil
	case errors.Is(err, io.EOF):
		v.err = fmt.Errorf("%w: %s holds %d bytes, its manifest declares %d", ErrDigestMismatch, v.digest, v.n, v.size)
		return 0, v.err
	case err != nil:
		v.err = err
		return k, err
	}
	return k, nil
}

// finish runs once size bytes are read: the object must end there, and what
// was read must hash to the digest.
func (v *verifiedReader) finish() error {
	var one [1]byte
	k, err := io.ReadFull(v.rc, one[:])
	if k > 0 {
		return fmt.Errorf("%w: %s holds more than the %d bytes its manifest declares", ErrDigestMismatch, v.digest, v.size)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if got := digestPrefix + hex.EncodeToString(v.h.Sum(nil)); got != v.digest {
		return fmt.Errorf("%w: %s hashes to %s", ErrDigestMismatch, v.digest, got)
	}
	return nil
}

func (v *verifiedReader) Close() error { return v.rc.Close() }
