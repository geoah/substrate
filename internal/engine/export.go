package engine

// The owner's recovery export (decision 0066): one repository's directory as
// of one committed point, streamed as a tar laid out as a data root
// (`repositories/<authority>/...`) in the snapshot format `repository
// snapshot` writes (snapshot.go, decision 0065). The point is pinned under
// the dataset's writer mutex, which every commit holds from its prepare to
// its final newline, so under it the writer's head, the active segment's
// length, the sealed files and the `records` table describe one committed
// state. The mutex is then released and the files are streamed as of that
// state: a finished segment never changes, the active segment is cut at the
// length it had, the sealed records were read under the mutex, and a blob's
// bytes are content-addressed. Writes go on while a client downloads.

import (
	"archive/tar"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/geoah/substrate/internal/blobbytes"
	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
)

var _ substrate.Exporter = (*dataset)(nil)

// ErrExportNoWriter is the refusal of a dataset that is not the directory's
// writer: a read-only process cannot pin a point, because the writer it does
// not hold may be mid-transaction.
var ErrExportNoWriter = errors.New("substrate/engine: the export needs the repository's changelog writer, which a read-only process does not hold")

// export is one pinned export: what Export read under the writer mutex, and
// what WriteTo streams from it.
type export struct {
	ds  *dataset
	ctx context.Context
	// point is the pinned point; snapshot is the snapshot.json that records
	// it, written last.
	point    substrate.ExportPoint
	snapshot changelogfile.Snapshot
	// manifest is repository.json as the directory held it.
	manifest []byte
	// finished are the segments with a sidecar, in seq order; active is the
	// one the writer holds open, cut at activeBytes, "" when none is open.
	finished    []string
	active      string
	activeBytes int64
	sealed      []changelogfile.SealedRecord
	blobs       []storedBlob
}

// Export pins the repository's committed point (substrate.Exporter). It takes
// writerMu, which every commit holds around its prepare, its Postgres commit
// and its final newline, so the head, the active segment's committed length,
// the sealed files and the stored blob manifests it reads are one state. A
// directory latched behind the tables is refused: an export of it would miss
// committed writes.
func (ds *dataset) Export(ctx context.Context) (substrate.Export, error) {
	if ds.svc.readOnly || ds.writer == nil {
		return nil, ErrExportNoWriter
	}
	ds.writerMu.Lock()
	defer ds.writerMu.Unlock()
	if ds.fileErr != nil {
		return nil, ds.fileErr
	}
	e := &export{ds: ds, ctx: ctx}
	head := ds.writer.Head()
	var headHash [32]byte
	if head > 0 {
		var raw []byte
		if err := ds.db.QueryRowContext(ctx, `SELECT hash FROM changelog WHERE seq = $1`, head).Scan(&raw); err != nil {
			return nil, fmt.Errorf("substrate/engine: read the checksum of seq %d: %w", head, err)
		}
		if len(raw) != 32 {
			return nil, fmt.Errorf("substrate/engine: seq %d carries a %d-byte checksum, want 32", head, len(raw))
		}
		headHash = [32]byte(raw)
	}
	segments, err := changelogfile.Segments(changelogfile.ChangelogDir(ds.dir))
	if err != nil {
		return nil, err
	}
	activeName, activeBytes, hasActive := ds.writer.Active()
	for _, seg := range segments {
		if seg.Finished {
			e.finished = append(e.finished, seg.Name)
			continue
		}
		// The one unfinished segment is the writer's, or the directory is
		// not the writer's view of it and no point can be pinned.
		if !hasActive || seg.Name != activeName {
			return nil, fmt.Errorf("substrate/engine: %s has no sidecar and is not the segment the writer holds open", seg.Name)
		}
		e.active, e.activeBytes = activeName, activeBytes
	}
	if hasActive && e.active == "" {
		return nil, fmt.Errorf("substrate/engine: the writer holds %s open and the directory does not list it", activeName)
	}
	if e.manifest, err = os.ReadFile(filepath.Join(ds.dir, changelogfile.ManifestName)); err != nil {
		return nil, err
	}
	var m changelogfile.Manifest
	if err := json.Unmarshal(e.manifest, &m); err != nil {
		return nil, fmt.Errorf("substrate/engine: decode %s: %w", changelogfile.ManifestName, err)
	}
	if m.Authority != ds.scope.Repository {
		return nil, fmt.Errorf("substrate/engine: %s names authority %q, the repository is %q", changelogfile.ManifestName, m.Authority, ds.scope.Repository)
	}
	if e.sealed, err = changelogfile.ReadSealed(ds.dir); err != nil {
		return nil, err
	}
	if e.blobs, err = storedBlobs(ctx, ds.db); err != nil {
		return nil, err
	}
	digests := make([]string, 0, len(e.blobs))
	for _, b := range e.blobs {
		digests = append(digests, b.digest)
	}
	taken := nowUTC()
	// The bytes ride in the archive under blobs/ whatever store the server
	// runs, so the copy is laid out for fs and names no location.
	e.snapshot = changelogfile.Snapshot{
		Format: changelogfile.SnapshotFormat, TakenAt: taken,
		Head: head, HeadHash: headHash, SealedFiles: len(e.sealed),
		BlobStore: blobbytes.BackendFS, Blobs: digests,
	}
	e.point = substrate.ExportPoint{
		Authority: ds.scope.Repository, Head: head, TakenAt: taken,
		Segments: len(segments), SealedFiles: len(e.sealed), Blobs: len(e.blobs),
	}
	if head > 0 {
		e.point.HeadHash = hex.EncodeToString(headHash[:])
	}
	return e, nil
}

// Point is the pinned point (substrate.Export).
func (e *export) Point() substrate.ExportPoint { return e.point }

// WriteTo streams the tar (substrate.Export). The order is the order a
// restore needs and the order the operator's snapshot copies: the manifest,
// the changelog, the sealed files, the blob bytes, and snapshot.json last, so
// an archive that ends early lacks the file that vouches for it.
func (e *export) WriteTo(w io.Writer) (int64, error) {
	cw := &countingWriter{w: w}
	tw := tar.NewWriter(cw)
	err := e.write(tw)
	if err == nil {
		err = tw.Close()
	}
	return cw.n, err
}

func (e *export) write(tw *tar.Writer) error {
	root := path.Join(changelogfile.RepositoriesDir, e.point.Authority)
	changelog := path.Join(root, changelogfile.ChangelogSubdir)
	blobs := path.Join(root, changelogfile.BlobsSubdir)
	sealed := path.Join(root, changelogfile.SealedSubdir)
	// The directories first, with the modes the server creates them with:
	// the changelog is every record in the clear, and `tar -x` keeps what
	// the header says.
	for _, dir := range []string{changelogfile.RepositoriesDir, root, changelog, blobs, sealed} {
		if err := tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeDir, Name: dir + "/", Mode: 0o700, ModTime: e.point.TakenAt, Format: tar.FormatPAX,
		}); err != nil {
			return err
		}
	}
	if err := e.writeBytes(tw, path.Join(root, changelogfile.ManifestName), e.manifest); err != nil {
		return err
	}
	srcChangelog := changelogfile.ChangelogDir(e.ds.dir)
	for _, name := range e.finished {
		for _, file := range []string{name, changelogfile.SidecarName(name)} {
			if err := e.writeFile(tw, path.Join(changelog, file), filepath.Join(srcChangelog, file), -1); err != nil {
				return err
			}
		}
	}
	if e.active != "" {
		if err := e.writeFile(tw, path.Join(changelog, e.active), filepath.Join(srcChangelog, e.active), e.activeBytes); err != nil {
			return err
		}
	}
	for _, rec := range e.sealed {
		// The same bytes WriteSealed puts in the directory.
		data, err := json.Marshal(rec)
		if err != nil {
			return err
		}
		if err := e.writeBytes(tw, path.Join(sealed, changelogfile.SealedFileName(rec.Ref)), append(data, '\n')); err != nil {
			return err
		}
	}
	if len(e.blobs) > 0 {
		store, err := e.ds.blobBytes()
		if err != nil {
			return err
		}
		for _, b := range e.blobs {
			// Whole, as the snapshot copies them: a blob is at most the upload
			// cap, and the digest is checked before the bytes enter the
			// archive, so a damaged store is a refusal and never a copy of
			// the damage.
			data, err := readStoredBlob(e.ctx, store, b)
			if err != nil {
				return fmt.Errorf("substrate/engine: read blob %s: %w", b.digest, err)
			}
			if got := blobDigest(data); got != b.digest {
				return fmt.Errorf("substrate/engine: blob %s read back as %s", b.digest, got)
			}
			if err := e.writeBytes(tw, path.Join(blobs, b.digest), data); err != nil {
				return err
			}
		}
	}
	snapshot, err := changelogfile.EncodeSnapshot(e.snapshot)
	if err != nil {
		return err
	}
	return e.writeBytes(tw, path.Join(root, changelogfile.SnapshotName), snapshot)
}

// writeBytes adds one regular file of known content.
func (e *export) writeBytes(tw *tar.Writer, name string, data []byte) error {
	if err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg, Name: name, Mode: 0o600, Size: int64(len(data)),
		ModTime: e.point.TakenAt, Format: tar.FormatPAX,
	}); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

// writeFile adds one regular file from disk: the whole file when size is
// negative, else exactly size bytes of it, which is the active segment cut at
// the point. A file shorter than the point claims is damage, not a shorter
// copy.
func (e *export) writeFile(tw *tar.Writer, name, src string, size int64) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if size < 0 {
		size = info.Size()
	} else if info.Size() < size {
		return fmt.Errorf("substrate/engine: %s is %d bytes, the point needs %d", filepath.Base(src), info.Size(), size)
	}
	if err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg, Name: name, Mode: 0o600, Size: size,
		ModTime: e.point.TakenAt, Format: tar.FormatPAX,
	}); err != nil {
		return err
	}
	_, err = io.CopyN(tw, f, size)
	return err
}

// readStoredBlob reads one stored blob whole, held to the size its manifest
// declares when it declares one.
func readStoredBlob(ctx context.Context, store blobbytes.Store, b storedBlob) ([]byte, error) {
	if b.size >= 0 {
		return blobbytes.ReadAll(ctx, store, b.digest, b.size)
	}
	return readBlob(ctx, store, b.digest)
}

// countingWriter counts the bytes that reached w, so a caller can tell an
// export that failed before its first byte (a refusal it can still answer)
// from one that failed mid-stream.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}
