package engine

// The snapshot: a copy of one stopped repository's directory into another
// root, verified before it is taken and after it is written, that records
// the committed point it holds in `snapshot.json` (changelogfile.Snapshot,
// decision 0065). It takes the repository's writer lock, so a running server
// refuses it, and it needs the credential key, because a snapshot promises
// that every sealed file it carries opens. It copies what a restore needs and
// nothing else: the manifest, every segment and sidecar, every committed
// sealed file, and under the fs blob store the bytes of every `stored`
// manifest, each hashed against its digest on the way. Under a store that
// keeps the bytes elsewhere (s3) the snapshot lists the objects instead.

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/blobbytes"
	"github.com/geoah/substrate/internal/changelogfile"
)

// SnapshotReport is what one snapshot wrote.
type SnapshotReport struct {
	Repository string `json:"repository"`
	Username   string `json:"username"`
	// Directory is the copy: `<destination root>/repositories/<authority>`.
	Directory string `json:"directory"`
	// Head and HeadHash are the recovery point recorded in the copy.
	Head     int64  `json:"head"`
	HeadHash string `json:"headHash"`
	Segments int    `json:"segments"`
	// SealedFiles is how many sealed files the copy holds, every one opened
	// under the DEK before the copy was taken.
	SealedFiles int `json:"sealedFiles"`
	// BlobStore is the source's blob backend. Blobs and BlobBytes are the
	// `stored` manifests' bytes copied into the directory, or under a store
	// that keeps them elsewhere, listed in the snapshot at BlobLocation.
	BlobStore    string        `json:"blobStore"`
	BlobLocation string        `json:"blobLocation,omitempty"`
	Blobs        int           `json:"blobs"`
	BlobBytes    int64         `json:"blobBytes"`
	Took         time.Duration `json:"took"`
}

// Snapshotter is the operator hat's snapshot seam, off substrate.Service like
// Rebuilder and asserted here for the same reason.
type Snapshotter interface {
	SnapshotRepository(ctx context.Context, username, destRoot string) (SnapshotReport, error)
}

var _ Snapshotter = (*service)(nil)

var (
	// ErrSnapshotExists is the refusal to write a snapshot where a
	// repository directory already is: a snapshot is a fresh copy, never a
	// merge over an older one.
	ErrSnapshotExists = errors.New("substrate/engine: the destination already holds a directory for this repository; a snapshot is written into an empty place")
	// ErrSnapshotNeedsKey is the refusal to snapshot without a credential
	// key: the snapshot promises every sealed file opens, and only the key
	// can prove it.
	ErrSnapshotNeedsKey = errors.New("substrate/engine: a snapshot needs SUBSTRATE_CREDENTIAL_KEY, because it proves every sealed file opens before it copies")
	// ErrSnapshotUnverified is the refusal to copy a repository that does
	// not verify: the findings are the error's text.
	ErrSnapshotUnverified = errors.New("substrate/engine: the repository does not verify, so no snapshot was taken")
)

// SnapshotRepository writes a verified copy of one repository's directory
// under destRoot, laid out as a data root (`repositories/<authority>/`) so a
// restore copies it straight into a server's root. It opens the repository as
// its writer, so a running server refuses it with ErrChangelogLocked; then it
// runs the whole verification and refuses on any finding; then it copies the
// changelog, the sealed files, the blob bytes and the manifest, verifies the
// copy's changelog and sealed files, and writes `snapshot.json` last, so a
// directory carrying one is a copy that finished.
func (s *service) SnapshotRepository(ctx context.Context, username, destRoot string) (SnapshotReport, error) {
	started := time.Now()
	if s.readOnly {
		return SnapshotReport{}, ErrDirectoryReadOnly
	}
	if len(s.credKey) == 0 {
		return SnapshotReport{}, ErrSnapshotNeedsKey
	}
	repo, err := s.repositoryByUsername(ctx, username)
	if err != nil {
		return SnapshotReport{}, err
	}
	report := SnapshotReport{Repository: repo.ID, Username: repo.Username, BlobStore: s.blobs.Name()}
	// The destination before the lock: a snapshot that would land on an
	// older copy is refused before it opens anything.
	dst, err := changelogfile.RepoDir(destRoot, repo.ID)
	if err != nil {
		return report, err
	}
	report.Directory = dst
	if _, err := os.Lstat(dst); err == nil {
		return report, fmt.Errorf("%w: %s", ErrSnapshotExists, dst)
	} else if !errors.Is(err, os.ErrNotExist) {
		return report, err
	}
	ds, err := s.open(ctx, repo)
	if err != nil {
		return report, err
	}
	if err := ds.directoryErr(); err != nil {
		return report, err
	}

	// The whole verification, side stores and sealed files opened included:
	// a copy of a repository that does not verify is a copy of the damage.
	verified, err := s.VerifyRepository(ctx, username)
	if err != nil {
		return report, err
	}
	if !verified.OK {
		return report, fmt.Errorf("%w: %s", ErrSnapshotUnverified, strings.Join(verified.Findings, "; "))
	}
	report.Head, report.HeadHash = verified.Head, verified.HeadHash
	var headHash [32]byte
	if verified.HeadHash != "" {
		sum, err := hex.DecodeString(verified.HeadHash)
		if err != nil || len(sum) != 32 {
			return report, fmt.Errorf("substrate/engine: the head checksum %q is not 32 bytes of hex", verified.HeadHash)
		}
		headHash = [32]byte(sum)
	}

	if _, err := changelogfile.EnsureRepoDir(destRoot, repo.ID); err != nil {
		return report, err
	}
	src := ds.dir
	if report.Segments, err = changelogfile.CopyChangelog(src, dst); err != nil {
		return report, fmt.Errorf("substrate/engine: copy the changelog: %w", err)
	}
	files, err := changelogfile.ReadSealed(src)
	if err != nil {
		return report, err
	}
	for _, f := range files {
		if err := changelogfile.WriteSealed(dst, f); err != nil {
			return report, fmt.Errorf("substrate/engine: copy sealed/%s: %w", changelogfile.SealedFileName(f.Ref), err)
		}
	}
	report.SealedFiles = len(files)

	blobs, err := storedBlobs(ctx, ds.db)
	if err != nil {
		return report, err
	}
	digests := make([]string, 0, len(blobs))
	for _, b := range blobs {
		digests = append(digests, b.digest)
	}
	report.Blobs = len(blobs)
	if loc, ok := s.blobs.(blobbytes.Locator); ok {
		// The bytes live outside the directory: the snapshot names where,
		// and the restore copies the listed objects there.
		if report.BlobLocation, err = loc.Location(repo.ID); err != nil {
			return report, err
		}
	} else if report.BlobBytes, err = s.copyBlobs(ctx, ds, repo, destRoot, digests); err != nil {
		return report, err
	}

	m, err := changelogfile.ReadManifest(src)
	if err != nil {
		return report, err
	}
	if err := changelogfile.WriteManifest(dst, m); err != nil {
		return report, fmt.Errorf("substrate/engine: copy the manifest: %w", err)
	}

	// The copy, read back the way a restore reads it, before the file that
	// vouches for it is written.
	copied, err := changelogfile.Verify(changelogfile.ChangelogDir(dst))
	if err != nil {
		return report, fmt.Errorf("substrate/engine: the copied changelog does not verify: %w", err)
	}
	if copied.Head != report.Head || copied.TruncatedBytes != 0 {
		return report, fmt.Errorf("substrate/engine: the copied changelog ends at %d with %d uncommitted bytes, the source at %d", copied.Head, copied.TruncatedBytes, report.Head)
	}
	if copiedSealed, err := changelogfile.ReadSealed(dst); err != nil {
		return report, fmt.Errorf("substrate/engine: the copied sealed files do not read: %w", err)
	} else if len(copiedSealed) != len(files) {
		return report, fmt.Errorf("substrate/engine: the copy holds %d sealed files, the source %d", len(copiedSealed), len(files))
	}
	if err := changelogfile.WriteSnapshot(dst, changelogfile.Snapshot{
		Format: changelogfile.SnapshotFormat, TakenAt: nowUTC(),
		Head: report.Head, HeadHash: headHash, SealedFiles: len(files),
		BlobStore: report.BlobStore, BlobLocation: report.BlobLocation, Blobs: digests,
	}); err != nil {
		return report, err
	}
	report.Took = time.Since(started)
	return report, nil
}

// copyBlobs reads every listed blob out of the repository's store, checks
// that its bytes hash to the digest that names it, and writes it into the
// destination root's fs store, which is `<dst>/blobs/<digest>`. A blob is at
// most the upload cap, so it is read whole; the store's Put writes it
// atomically and fsynced.
func (s *service) copyBlobs(ctx context.Context, ds *dataset, repo Repository, destRoot string, digests []string) (int64, error) {
	if len(digests) == 0 {
		return 0, nil
	}
	store, err := s.blobs.Repository(repo.ID, ds.db)
	if err != nil {
		return 0, err
	}
	destFS, err := blobbytes.NewFS(destRoot)
	if err != nil {
		return 0, err
	}
	dest, err := destFS.Repository(repo.ID, nil)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, digest := range digests {
		data, err := readBlob(ctx, store, digest)
		if err != nil {
			return total, fmt.Errorf("substrate/engine: read blob %s: %w", digest, err)
		}
		if got := blobDigest(data); got != digest {
			return total, fmt.Errorf("substrate/engine: blob %s read back as %s", digest, got)
		}
		if err := dest.Put(ctx, digest, int64(len(data)), bytes.NewReader(data)); err != nil {
			return total, fmt.Errorf("substrate/engine: copy blob %s: %w", digest, err)
		}
		total += int64(len(data))
	}
	return total, nil
}

// readBlob reads one blob's bytes whole out of the store.
func readBlob(ctx context.Context, store blobbytes.Store, digest string) ([]byte, error) {
	rc, err := store.Open(ctx, digest)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}
