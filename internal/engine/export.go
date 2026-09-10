package engine

// The owner's recovery export (decision 0069): one repository's directory as
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
	"crypto/sha256"
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

var (
	// ErrExportNoWriter is the refusal of a dataset that is not the
	// directory's writer: a read-only process cannot pin a point, because
	// the writer it does not hold may be mid-transaction. Unavailable, not a
	// fault: the same repository exports from the process that writes it.
	ErrExportNoWriter = fmt.Errorf("%w: the export needs the repository's changelog writer, which a read-only process does not hold", substrate.ErrUnavailable)
	// ErrExportInProgress is the refusal of a second export while one
	// streams: an export holds a blob open and a tar half written for as long
	// as the slowest client takes, so one per repository bounds what a
	// repository's exports can hold at once.
	ErrExportInProgress = fmt.Errorf("%w: an export of this repository is already streaming; wait for it to finish", substrate.ErrConflict)
)

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
//
// The pool connection the pin reads through is taken BEFORE the mutex. Every
// write holds a connection from inTx to commitAndMirror, where it takes the
// mutex, and the writers behind it wait on the changelog advisory lock inside
// their own transactions, connections held; with the pool full, an export
// that took the mutex first and then asked for a connection would wait for
// one that no writer can release until the mutex is free.
func (ds *dataset) Export(ctx context.Context) (substrate.Export, error) {
	if ds.svc.readOnly || ds.writer == nil {
		return nil, ErrExportNoWriter
	}
	if !ds.exporting.CompareAndSwap(false, true) {
		return nil, ErrExportInProgress
	}
	e, err := ds.pinExport(ctx)
	if err != nil {
		ds.exporting.Store(false)
		return nil, err
	}
	return e, nil
}

func (ds *dataset) pinExport(ctx context.Context) (*export, error) {
	conn, err := ds.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	ds.writerMu.Lock()
	defer ds.writerMu.Unlock()
	if ds.fileErr != nil {
		return nil, fmt.Errorf("%w: %w", substrate.ErrUnavailable, ds.fileErr)
	}
	e := &export{ds: ds, ctx: ctx}
	head := ds.writer.Head()
	var headHash [32]byte
	if head > 0 {
		var raw []byte
		if err := conn.QueryRowContext(ctx, `SELECT hash FROM changelog WHERE seq = $1`, head).Scan(&raw); err != nil {
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
	if e.blobs, err = storedBlobs(ctx, conn); err != nil {
		return nil, err
	}
	digests := make([]string, 0, len(e.blobs))
	for _, b := range e.blobs {
		digests = append(digests, b.digest)
	}
	taken := nowUTC()
	// The bytes ride in the archive under blobs/, so the copy is laid out the
	// way a repository directory is.
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

// WriteTo streams the tar (substrate.Export) and releases the repository's
// export slot when it returns, however it returns. The order is the order a
// restore needs and the order the operator's snapshot copies: the manifest,
// the changelog, the sealed files, the blob bytes, and snapshot.json last, so
// an archive that ends early lacks the file that vouches for it.
func (e *export) WriteTo(w io.Writer) (int64, error) {
	defer e.ds.exporting.Store(false)
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
			if err := e.writeBlob(tw, path.Join(blobs, b.digest), store, b); err != nil {
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
	if err := e.header(tw, name, int64(len(data))); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

// header adds one regular file's header.
func (e *export) header(tw *tar.Writer, name string, size int64) error {
	return tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg, Name: name, Mode: 0o600, Size: size,
		ModTime: e.point.TakenAt, Format: tar.FormatPAX,
	})
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
	if err := e.header(tw, name, size); err != nil {
		return err
	}
	_, err = io.CopyN(tw, f, size)
	return err
}

// writeBlob streams one stored blob out of the store into the archive,
// hashing it on the way: the header takes the size the manifest declares, so
// nothing is held in memory but the copy buffer, and the digest is checked
// once the bytes are through. A store that answers other bytes fails the
// export here, which ends the stream, so a restore never holds a blob that is
// not its digest's; the archive is already committed to the entry by then, so
// the failure is the export's and not a shorter archive.
//
// A `stored` manifest that declares no size (verify passes one: it hashes
// the bytes and compares the size only when the manifest claims one) is read
// whole instead, as the operator's snapshot reads every blob, so the header
// is sized from the bytes; the upload cap bounds what that holds.
func (e *export) writeBlob(tw *tar.Writer, name string, store blobbytes.Store, b storedBlob) error {
	if b.size < 0 {
		data, err := readBlob(e.ctx, store, b.digest)
		if err != nil {
			return fmt.Errorf("substrate/engine: read blob %s: %w", b.digest, err)
		}
		if got := blobDigest(data); got != b.digest {
			return fmt.Errorf("substrate/engine: blob %s read back as %s", b.digest, got)
		}
		return e.writeBytes(tw, name, data)
	}
	rc, err := store.Open(e.ctx, b.digest)
	if err != nil {
		return fmt.Errorf("substrate/engine: read blob %s: %w", b.digest, err)
	}
	defer func() { _ = rc.Close() }()
	if err := e.header(tw, name, b.size); err != nil {
		return err
	}
	h := sha256.New()
	// One byte past the declared size tells a longer object from an exact
	// one; the tar writer refuses the overrun, and the message names the blob.
	n, err := io.CopyN(tw, io.TeeReader(rc, h), b.size)
	if err != nil {
		return fmt.Errorf("substrate/engine: blob %s: %d of %d bytes copied: %w", b.digest, n, b.size, err)
	}
	if extra, err := io.ReadFull(rc, make([]byte, 1)); extra > 0 || (err != nil && !errors.Is(err, io.EOF)) {
		return fmt.Errorf("substrate/engine: blob %s holds more than the %d bytes its manifest declares", b.digest, b.size)
	}
	if got := substrate.BlobDigestPrefix + hex.EncodeToString(h.Sum(nil)); got != b.digest {
		return fmt.Errorf("substrate/engine: blob %s read back as %s", b.digest, got)
	}
	return nil
}

// countingWriter counts the bytes that reached w.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}
