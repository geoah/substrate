package engine

// The snapshot: a copy of one stopped repository's directory into another
// root, verified before it is taken and after it is written, that records
// the committed point it holds in `snapshot.json` (changelogfile.Snapshot,
// decision 0065). It takes the repository's writer lock, so a running server
// refuses it, and it needs the credential key, because a snapshot promises
// that every sealed file it carries opens. It copies what a restore needs and
// nothing else: the manifest, every segment and sidecar, every committed
// sealed file, and the bytes of every `stored` blob manifest, each hashed
// against its digest on the way. The copy is built under a dot-prefixed
// temporary root beside the destination and renamed into place only once
// `snapshot.json` is durable, so a failure leaves nothing at the destination
// and the boot never lists a partial.
//
// Handed an earlier snapshot of the same repository as its base, it copies
// only what changed since. A finished segment the base holds under the same
// name, size and sidecar digest, and a blob the base's `snapshot.json` lists,
// is a hard link to the base's file: the same bytes, which the base's own
// checks read when it was written, so neither the source's file nor the
// base's is read again, and the table is compared with the files only past
// them. What the base does not hold is checked and copied as a full snapshot
// checks and copies it. The copy is still a whole directory: deleting the
// base leaves it intact (decision 0146).

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/blobbytes"
	"github.com/geoah/substrate/internal/changelogfile"
)

// SnapshotReport is what one snapshot wrote.
type SnapshotReport struct {
	Repository string `json:"repository"`
	// Directory is the copy: `<destination root>/repositories/<authority>`.
	Directory string `json:"directory"`
	// Head and HeadHash are the recovery point recorded in the copy.
	Head     int64  `json:"head"`
	HeadHash string `json:"headHash"`
	Segments int    `json:"segments"`
	// SealedFiles is how many sealed files the copy holds, every one opened
	// under the DEK before the copy was taken and again in the copy.
	SealedFiles int `json:"sealedFiles"`
	// BlobStore is the layout the copy's blob bytes are written for, which is
	// the one backend. Blobs and BlobBytes are the `stored` manifests' bytes
	// copied into the directory.
	BlobStore string `json:"blobStore"`
	Blobs     int    `json:"blobs"`
	BlobBytes int64  `json:"blobBytes"`
	// Base is the base's directory when the snapshot took one, and
	// LinkedSegments and LinkedBlobs are how many segments and blobs are
	// hard links to its files rather than copies; BlobBytes counts the
	// copied blobs' bytes alone.
	Base           string        `json:"base,omitempty"`
	LinkedSegments int           `json:"linkedSegments,omitempty"`
	LinkedBlobs    int           `json:"linkedBlobs,omitempty"`
	Took           time.Duration `json:"took"`
}

// SnapshotOptions tunes SnapshotRepositoryWith.
type SnapshotOptions struct {
	// Base is the destination root of an earlier snapshot of the same
	// repository: `<Base>/repositories/<authority>/snapshot.json` must be
	// there. It must be on the destination's filesystem for its files to be
	// linked; a file the filesystem refuses to link is copied from the
	// source and read back, as without a base.
	Base string
}

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
	// ErrSnapshotCopyDamaged is the refusal to keep a copy that does not
	// read back as the source: a segment whose checksums do not walk, a
	// sealed file that is not the source's or does not open, a blob whose
	// bytes do not hash to its digest. Nothing is left at the destination.
	ErrSnapshotCopyDamaged = errors.New("substrate/engine: the copy does not read back as the source, so it was discarded")
	// ErrSnapshotBase is the refusal of a base that is not a snapshot of
	// this repository: no `snapshot.json`, or a manifest of another
	// authority.
	ErrSnapshotBase = errors.New("substrate/engine: the base is not a snapshot of this repository")
)

// The stages the snapshot's test seam reports (export_test.go): after the
// changelog is copied, and after every file is copied and before the copy is
// read back.
const (
	snapshotAfterChangelog = "after-changelog"
	snapshotAfterCopy      = "after-copy"
)

// snapshotFault runs the test seam at one of the snapshot's steps with the
// partial directory; a nil hook is the production path.
func (s *service) snapshotFault(stage, dir string) error {
	if s.testSnapshotFault == nil {
		return nil
	}
	return s.testSnapshotFault(stage, dir)
}

// snapshotPartialPrefix names the temporary root a snapshot is built under,
// beside the destination's `repositories/`. It is dot-prefixed, so a partial
// a process died on is skipped by every directory listing and imported by
// nothing.
const snapshotPartialPrefix = ".incoming-snapshot-"

// SnapshotRepository writes a verified copy of one repository's directory
// under destRoot, laid out as a data root (`repositories/<authority>/`) so a
// restore copies it straight into a server's root. It opens the repository as
// its writer, so a running server refuses it with ErrChangelogLocked; then it
// runs the whole verification and refuses on any finding; then it builds the
// copy under a temporary root beside the destination (the changelog, the
// sealed files, the blob bytes and the manifest), reads the copy back the way
// the source was verified, writes `snapshot.json`, and renames the directory
// into place. A failure anywhere removes the temporary root, so the
// destination holds either the finished copy or nothing.
func (s *service) SnapshotRepository(ctx context.Context, repository, destRoot string) (SnapshotReport, error) {
	return s.SnapshotRepositoryWith(ctx, repository, destRoot, SnapshotOptions{})
}

// SnapshotRepositoryWith is SnapshotRepository with its options: with a
// Base, what the base holds is linked rather than read and copied.
func (s *service) SnapshotRepositoryWith(ctx context.Context, repository, destRoot string, opts SnapshotOptions) (SnapshotReport, error) {
	started := time.Now()
	if s.readOnly {
		return SnapshotReport{}, ErrDirectoryReadOnly
	}
	if len(s.credKey) == 0 {
		return SnapshotReport{}, ErrSnapshotNeedsKey
	}
	repo, err := s.repositoryByID(ctx, repository)
	if err != nil {
		return SnapshotReport{}, err
	}
	report := SnapshotReport{Repository: repo.ID, BlobStore: blobbytes.BackendFS}
	// The destination before the lock: a snapshot that would land on an
	// older copy is refused before it opens anything.
	dst, err := changelogfile.RepoDir(destRoot, repo.ID)
	if err != nil {
		return report, err
	}
	report.Directory = dst
	if err := refuseExisting(dst); err != nil {
		return report, err
	}
	var base string
	if opts.Base != "" {
		if base, err = snapshotBase(opts.Base, repo.ID); err != nil {
			return report, err
		}
		report.Base = base
	}
	ds, err := s.open(ctx, repo)
	if err != nil {
		return report, err
	}
	if err := ds.directoryErr(); err != nil {
		return report, err
	}
	shared, err := baseShares(ctx, ds, base)
	if err != nil {
		return report, err
	}

	// The whole verification, side stores and sealed files opened included:
	// a copy of a repository that does not verify is a copy of the damage.
	// What the base holds was verified when the base was taken, and is read
	// at its first and last lines alone. The changelog Log it verified is
	// what the copy is held to below.
	verified, srcLog, err := s.verifyRepository(ctx, repository, VerifyOptions{known: shared.segments, knownBlobs: shared.blobs})
	if err != nil {
		return report, err
	}
	if !verified.OK || srcLog == nil {
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

	// The temporary root sits inside the destination root, so the final
	// rename is within one filesystem.
	if err := os.MkdirAll(filepath.Join(destRoot, changelogfile.RepositoriesDir), 0o700); err != nil {
		return report, err
	}
	tmpRoot, err := os.MkdirTemp(destRoot, snapshotPartialPrefix)
	if err != nil {
		return report, err
	}
	defer func() { _ = os.RemoveAll(tmpRoot) }()
	partial, err := s.buildSnapshot(ctx, ds, repo, tmpRoot, srcLog, headHash, shared, &report)
	if err != nil {
		return report, err
	}
	if err := refuseExisting(dst); err != nil {
		return report, err
	}
	if err := os.Rename(partial, dst); err != nil {
		return report, err
	}
	if err := syncDirectory(filepath.Dir(dst)); err != nil {
		return report, err
	}
	report.Took = time.Since(started)
	return report, nil
}

// refuseExisting is the destination check: a repository directory already
// there is ErrSnapshotExists, anything but its absence is the error.
func refuseExisting(dst string) error {
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("%w: %s", ErrSnapshotExists, dst)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// buildSnapshot writes the copy under tmpRoot, at `repositories/<authority>`,
// reads it back and writes `snapshot.json` last. It returns the directory it
// built; the caller renames it into place or removes the root.
func (s *service) buildSnapshot(ctx context.Context, ds *dataset, repo Repository, tmpRoot string, srcLog *changelogfile.Log, headHash [32]byte, shared baseShare, report *SnapshotReport) (string, error) {
	partial, err := changelogfile.EnsureRepoDir(tmpRoot, repo.ID)
	if err != nil {
		return "", err
	}
	src := ds.dir
	copied, err := changelogfile.CopyChangelogFrom(src, partial, shared.dir, shared.segments)
	if err != nil {
		return "", fmt.Errorf("substrate/engine: copy the changelog: %w", err)
	}
	report.Segments, report.LinkedSegments = copied.Segments, len(copied.Linked)
	// The read-back takes a linked segment as the base's: its bytes are the
	// base's file. A segment the filesystem would not link was copied and is
	// digested like any other.
	linked := make(map[string]changelogfile.KnownSegment, len(copied.Linked))
	for name := range copied.Linked {
		linked[name] = shared.segments[name]
	}
	if err := s.snapshotFault(snapshotAfterChangelog, partial); err != nil {
		return "", err
	}
	files, err := changelogfile.ReadSealed(src)
	if err != nil {
		return "", err
	}
	for _, f := range files {
		if err := changelogfile.WriteSealed(partial, f); err != nil {
			return "", fmt.Errorf("substrate/engine: copy sealed/%s: %w", changelogfile.SealedFileName(f.Ref), err)
		}
	}
	report.SealedFiles = len(files)

	blobs, err := storedBlobs(ctx, ds.db)
	if err != nil {
		return "", err
	}
	digests := make([]string, 0, len(blobs))
	for _, b := range blobs {
		digests = append(digests, b.digest)
	}
	report.Blobs = len(blobs)
	linkedBlobs, err := linkBlobs(shared, partial, digests)
	if err != nil {
		return "", err
	}
	report.LinkedBlobs = len(linkedBlobs)
	toCopy := make([]string, 0, len(digests)-len(linkedBlobs))
	for _, d := range digests {
		if !linkedBlobs[d] {
			toCopy = append(toCopy, d)
		}
	}
	if report.BlobBytes, err = s.copyBlobs(ctx, ds, repo, tmpRoot, toCopy); err != nil {
		return "", err
	}

	m, err := changelogfile.ReadManifest(src)
	if err != nil {
		return "", err
	}
	if err := changelogfile.WriteManifest(partial, m); err != nil {
		return "", fmt.Errorf("substrate/engine: copy the manifest: %w", err)
	}
	if err := s.snapshotFault(snapshotAfterCopy, partial); err != nil {
		return "", err
	}

	// The copy, read back before the file that vouches for it is written:
	// every copied finished segment hashed against its copied sidecar and
	// held to the digest the source's verification took, the active
	// segment's lines checked, every sealed file the source's and opened
	// under the DEK, every copied blob hashed. A finished segment whose
	// bytes are the source's needs no walk of its lines: the verification
	// walked the source's (issue 761). A linked segment or blob is the
	// base's file and is not read.
	copiedLog, err := changelogfile.OpenWith(changelogfile.ChangelogDir(partial), changelogfile.OpenOptions{
		ReadOnly: true,
		Known:    linked,
		Progress: s.checkProgress(checkSnapshotCopy, repo.ID),
	})
	if err != nil {
		return "", fmt.Errorf("%w: the changelog: %w", ErrSnapshotCopyDamaged, err)
	}
	if err := copiedLog.SameAs(srcLog); err != nil {
		return "", fmt.Errorf("%w: the changelog: %w", ErrSnapshotCopyDamaged, err)
	}
	if copiedLog.Head() != report.Head || copiedLog.TruncatedBytes != 0 {
		return "", fmt.Errorf("%w: the changelog ends at %d with %d uncommitted bytes, the source at %d", ErrSnapshotCopyDamaged, copiedLog.Head(), copiedLog.TruncatedBytes, report.Head)
	}
	if err := s.checkCopiedSealed(ds, partial, files); err != nil {
		return "", err
	}
	if err := checkCopiedBlobs(ctx, tmpRoot, repo, toCopy); err != nil {
		return "", err
	}
	if err := changelogfile.WriteSnapshot(partial, changelogfile.Snapshot{
		Format: changelogfile.SnapshotFormat, TakenAt: nowUTC(),
		Head: report.Head, HeadHash: headHash, SealedFiles: len(files),
		BlobStore: report.BlobStore, Blobs: digests,
	}); err != nil {
		return "", err
	}
	return partial, nil
}

// checkCopiedSealed reads every sealed file of the copy and holds each to the
// source's record byte for byte and to the DEK: a copied ciphertext damaged
// on the way is still valid JSON and base64, and only opening it tells.
func (s *service) checkCopiedSealed(ds *dataset, partial string, files []changelogfile.SealedRecord) error {
	copied, err := changelogfile.ReadSealed(partial)
	if err != nil {
		return fmt.Errorf("%w: the sealed files: %w", ErrSnapshotCopyDamaged, err)
	}
	if len(copied) != len(files) {
		return fmt.Errorf("%w: the copy holds %d sealed files, the source %d", ErrSnapshotCopyDamaged, len(copied), len(files))
	}
	byRef := make(map[string]changelogfile.SealedRecord, len(files))
	for _, f := range files {
		byRef[f.Ref] = f
	}
	for _, f := range copied {
		name := changelogfile.SealedFileName(f.Ref)
		if want, ok := byRef[f.Ref]; !ok || !sealedRecordsEqual(f, want) {
			return fmt.Errorf("%w: sealed/%s is not the source's record", ErrSnapshotCopyDamaged, name)
		}
		if _, err := openRepoPayload(f.Payload, ds.dek, sealedAAD(f.Ref, f.RecordKind, f.RecordID)); err != nil {
			return fmt.Errorf("%w: sealed/%s does not open under the DEK: %w", ErrSnapshotCopyDamaged, name, err)
		}
	}
	return nil
}

// checkCopiedBlobs reads every copied blob back out of the copy's fs store
// and hashes it against the digest that names it.
func checkCopiedBlobs(ctx context.Context, tmpRoot string, repo Repository, digests []string) error {
	if len(digests) == 0 {
		return nil
	}
	dest, err := destBlobStore(tmpRoot, repo)
	if err != nil {
		return err
	}
	for _, digest := range digests {
		_, got, err := hashBlob(ctx, dest, digest)
		if err != nil {
			return fmt.Errorf("%w: blob %s: %w", ErrSnapshotCopyDamaged, digest, err)
		}
		if got != digest {
			return fmt.Errorf("%w: blob %s hashes to %s in the copy", ErrSnapshotCopyDamaged, digest, got)
		}
	}
	return nil
}

// destBlobStore is the copy's blob store, `<root>/repositories/<authority>/blobs`.
func destBlobStore(root string, repo Repository) (blobbytes.Store, error) {
	destFS, err := blobbytes.NewFS(root)
	if err != nil {
		return nil, err
	}
	return destFS.Repository(repo.ID)
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
	store, err := s.blobs.Repository(repo.ID)
	if err != nil {
		return 0, err
	}
	dest, err := destBlobStore(destRoot, repo)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, digest := range digests {
		// Read with no size: the snapshot copies by digest, and the hash is
		// the check that matters for a copy.
		data, err := blobbytes.ReadAll(ctx, store, digest, -1)
		if err != nil {
			return total, fmt.Errorf("substrate/engine: read blob %s: %w", digest, err)
		}
		if err := dest.Put(ctx, digest, int64(len(data)), bytes.NewReader(data)); err != nil {
			return total, fmt.Errorf("substrate/engine: copy blob %s: %w", digest, err)
		}
		total += int64(len(data))
	}
	return total, nil
}

// syncDirectory flushes a directory entry (the snapshot's rename) to disk.
func syncDirectory(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	return d.Sync()
}

// snapshotBase resolves an earlier snapshot root to the repository's
// directory in it, refusing one that holds no finished snapshot of the
// repository.
func snapshotBase(root, repository string) (string, error) {
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("%w: %q is not an absolute path", ErrSnapshotBase, root)
	}
	dir, err := changelogfile.RepoDir(root, repository)
	if err != nil {
		return "", err
	}
	if _, err := changelogfile.ReadSnapshot(dir); err != nil {
		return "", fmt.Errorf("%w: %s: %w", ErrSnapshotBase, dir, err)
	}
	if _, err := changelogfile.ReadManifest(dir); err != nil {
		return "", fmt.Errorf("%w: %s: %w", ErrSnapshotBase, dir, err)
	}
	return dir, nil
}

// baseShare is what a snapshot's base holds as the repository does: its
// directory, the run of finished segments with the same name, size and
// sidecar digest (changelogfile.SharedFinished), and the stored blobs its
// snapshot.json lists whose file there has the size the manifest declares.
// The zero value is no base.
type baseShare struct {
	dir      string
	segments map[string]changelogfile.KnownSegment
	blobs    map[string]bool
}

// baseShares reads what the base at dir holds as ds's directory does, from
// sidecars, file sizes and the base's snapshot.json: no segment or blob byte.
func baseShares(ctx context.Context, ds *dataset, dir string) (baseShare, error) {
	if dir == "" {
		return baseShare{}, nil
	}
	// The repository's own directory is never its base, whatever it
	// carries: a restored one keeps the snapshot.json it came back with,
	// and what it wrote since nothing checked.
	if a, err := os.Stat(dir); err == nil {
		if b, err := os.Stat(ds.dir); err == nil && os.SameFile(a, b) {
			return baseShare{}, fmt.Errorf("%w: %s is the repository's own directory", ErrSnapshotBase, dir)
		}
	}
	snap, err := changelogfile.ReadSnapshot(dir)
	if err != nil {
		return baseShare{}, err
	}
	segments, err := changelogfile.SharedFinished(changelogfile.ChangelogDir(ds.dir), changelogfile.ChangelogDir(dir), snap.Head)
	if err != nil {
		return baseShare{}, fmt.Errorf("substrate/engine: compare the changelog with the base: %w", err)
	}
	listed := make(map[string]bool, len(snap.Blobs))
	for _, d := range snap.Blobs {
		listed[d] = true
	}
	stored, err := storedBlobs(ctx, ds.db)
	if err != nil {
		return baseShare{}, err
	}
	blobs := map[string]bool{}
	for _, b := range stored {
		if !listed[b.digest] {
			continue
		}
		info, err := os.Stat(filepath.Join(changelogfile.BlobsDir(dir), b.digest))
		if err != nil || !info.Mode().IsRegular() || (b.size >= 0 && info.Size() != b.size) {
			continue
		}
		blobs[b.digest] = true
	}
	return baseShare{dir: dir, segments: segments, blobs: blobs}, nil
}

// linkBlobs hard-links every listed digest the base holds into the copy's
// blobs/, and returns the ones it linked: a blob the filesystem refuses to
// link is left for copyBlobs to copy and checkCopiedBlobs to hash.
func linkBlobs(shared baseShare, partial string, digests []string) (map[string]bool, error) {
	linked := map[string]bool{}
	if len(shared.blobs) == 0 {
		return linked, nil
	}
	dst := changelogfile.BlobsDir(partial)
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return nil, err
	}
	for _, d := range digests {
		if !shared.blobs[d] {
			continue
		}
		if os.Link(filepath.Join(changelogfile.BlobsDir(shared.dir), d), filepath.Join(dst, d)) == nil {
			linked[d] = true
		}
	}
	if len(linked) > 0 {
		if err := syncDirectory(dst); err != nil {
			return nil, err
		}
	}
	return linked, nil
}
