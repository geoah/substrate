package changelogfile

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// SnapshotName is the file at the top of a repository directory that records
// the committed recovery point a copy holds. The server never writes it: a
// snapshot (the engine's SnapshotRepository) does, last, so a directory that
// carries one is a complete copy taken at that point, and a directory
// without one is a live root or a copy nothing vouches for. The boot ignores
// the file, so a restored directory keeps it as the record of where it came
// from.
const SnapshotName = "snapshot.json"

// SnapshotFormat is the snapshot format this package writes and the only one
// it reads.
const SnapshotFormat = 1

// ErrSnapshotFormat is returned for a snapshot whose format this package does
// not read, and by WriteSnapshot for one that is not SnapshotFormat.
var ErrSnapshotFormat = errors.New("changelogfile: snapshot format is not one this package handles")

// Snapshot is `snapshot.json`: the point a copy of a repository directory
// represents, and what beside the directory the copy needs. The point is the
// head of the committed changelog and that entry's checksum: a property of
// the files alone, checkable in the copy without the database that wrote it,
// and the checksum names the exact entry, so a copy of another history that
// happens to reach the same seq is told apart. The history generation
// (decision 0056) is not recorded because an import mints a new one.
type Snapshot struct {
	Format  int
	TakenAt time.Time
	// Head is the seq of the last committed entry the copy holds, and
	// HeadHash that entry's checksum: the line's sum in the file and the
	// row's `hash` in the table.
	Head     int64
	HeadHash [32]byte
	// SealedFiles is how many files under sealed/ the copy holds, every one
	// of them opened under the repository's DEK when the snapshot was taken.
	SealedFiles int
	// BlobStore names the blob backend the source ran (fs or s3), and
	// BlobLocation where the repository's objects live when they are not in
	// the directory: empty under fs, where they are `blobs/<digest>`, and the
	// object prefix under s3 (`s3://<bucket>/<prefix><authority>/`), where
	// each object is that prefix plus the digest.
	BlobStore    string
	BlobLocation string
	// Blobs is every digest a `stored` blob manifest named at Head, sorted:
	// the bytes the copy holds under fs, and the objects a restore copies
	// under s3. A blob the fold does not name is not listed and not needed.
	Blobs []string
}

// snapshotWire is the JSON form. TakenAt is written in TSFormat and read as
// any RFC 3339 time; the checksum is hex.
type snapshotWire struct {
	Format       int      `json:"format"`
	TakenAt      string   `json:"takenAt"`
	Head         int64    `json:"head"`
	HeadHash     string   `json:"headHash"`
	SealedFiles  int      `json:"sealedFiles"`
	BlobStore    string   `json:"blobStore"`
	BlobLocation string   `json:"blobLocation"`
	Blobs        []string `json:"blobs"`
}

// MarshalJSON renders the snapshot in its file form.
func (s Snapshot) MarshalJSON() ([]byte, error) {
	blobs := s.Blobs
	if blobs == nil {
		blobs = []string{}
	}
	w := snapshotWire{
		Format: s.Format, Head: s.Head, HeadHash: hex.EncodeToString(s.HeadHash[:]),
		SealedFiles: s.SealedFiles, BlobStore: s.BlobStore, BlobLocation: s.BlobLocation, Blobs: blobs,
	}
	if !s.TakenAt.IsZero() {
		w.TakenAt = s.TakenAt.UTC().Format(TSFormat)
	}
	return json.Marshal(w)
}

// UnmarshalJSON parses the file form. The key set is closed, as the
// manifest's is: a format is exactly its keys, and a later one announces
// itself in `format`.
func (s *Snapshot) UnmarshalJSON(data []byte) error {
	var probe struct {
		Format int `json:"format"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	if probe.Format != SnapshotFormat {
		return fmt.Errorf("%w: got %d", ErrSnapshotFormat, probe.Format)
	}
	var w snapshotWire
	if err := decodeClosed(data, &w); err != nil {
		return err
	}
	taken, err := parseManifestTime(w.TakenAt)
	if err != nil {
		return fmt.Errorf("takenAt: %w", err)
	}
	sum, err := hex.DecodeString(w.HeadHash)
	if err != nil || len(sum) != 32 {
		return errors.New("headHash: not 32 bytes of hex")
	}
	*s = Snapshot{
		Format: w.Format, TakenAt: taken, Head: w.Head, HeadHash: [32]byte(sum),
		SealedFiles: w.SealedFiles, BlobStore: w.BlobStore, BlobLocation: w.BlobLocation, Blobs: w.Blobs,
	}
	return nil
}

// ReadSnapshot reads the snapshot file of the repository directory. A
// missing file is reported with an error that matches os.ErrNotExist, so a
// reader can tell a directory that is not a snapshot from a damaged one.
func ReadSnapshot(repoDir string) (Snapshot, error) {
	raw, err := os.ReadFile(filepath.Join(repoDir, SnapshotName))
	if err != nil {
		return Snapshot{}, err
	}
	var s Snapshot
	if err := json.Unmarshal(raw, &s); err != nil {
		return Snapshot{}, fmt.Errorf("changelogfile: decode %s: %w", SnapshotName, err)
	}
	return s, nil
}

// WriteSnapshot writes the snapshot atomically into the repository
// directory, replacing any there.
func WriteSnapshot(repoDir string, s Snapshot) error {
	if s.Format != SnapshotFormat {
		return fmt.Errorf("%w: writing format %d, this package writes %d", ErrSnapshotFormat, s.Format, SnapshotFormat)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(repoDir, SnapshotName, append(data, '\n'))
}

// CopyChangelog copies the changelog of the repository directory src into the
// repository directory dst: every segment and every finished segment's
// sidecar, each written through a temporary file and fsynced. The lock file
// and anything half-written are not files of the changelog and are not
// copied. It returns the number of segments copied. The caller holds the
// source's writer lock or knows its writer is stopped: a segment copied while
// its writer appends is a copy of an unknown point.
func CopyChangelog(src, dst string) (int, error) {
	srcDir, dstDir := ChangelogDir(src), ChangelogDir(dst)
	segments, err := Segments(srcDir)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(dstDir, dirMode); err != nil {
		return 0, err
	}
	for _, seg := range segments {
		if err := copyFile(srcDir, dstDir, seg.Name); err != nil {
			return 0, err
		}
		if seg.Finished {
			if err := copyFile(srcDir, dstDir, sidecarName(seg.Name)); err != nil {
				return 0, err
			}
		}
	}
	return len(segments), nil
}

// copyFile streams <srcDir>/<name> to <dstDir>/<name> the way writeFileAtomic
// writes: a temporary file, an fsync, a rename, a directory fsync.
func copyFile(srcDir, dstDir, name string) error {
	in, err := os.Open(filepath.Join(srcDir, name))
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	tmp, err := os.CreateTemp(dstDir, tmpPrefix+"*")
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
	if _, err := io.Copy(tmp, in); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, filepath.Join(dstDir, name)); err != nil {
		return err
	}
	return syncDir(dstDir)
}
