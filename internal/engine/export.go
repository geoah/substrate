package engine

// The owner's export is the repository's archive as one file (decision 0278):
// a tar holding one archive of its own (decision 0279), an id minted at the
// export under `repositories/<id>/`, with the blobs, the sealed values, the
// webhook spool, the history in pieces and the manifest last, as of one
// committed point. The manifest carries the sign-in of the person who took
// it and nobody else's (archiveidentities.go). Put in a store, it is the newest archive there, so a
// boot with no rows for the repository restores from it. The point is a read snapshot of
// the table; the lines up to its head never change, so they are read after
// it, while writes go on. A blob's bytes are held against collection from
// before the snapshot until the stream ends (holdExportBlobs), so every blob
// the point holds is still there to stream. An archive ends with its
// manifest, so a stream cut short has none, and nothing imports it.

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"
	"path"
	"strconv"
	"sync"
	"time"

	"github.com/geoah/substrate/internal/archive"
	"github.com/geoah/substrate/internal/blobbytes"
	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
)

var (
	// ErrExportNoWriter is the refusal of a dataset that is not the
	// repository's writer: the blobs a point holds are held against the
	// collection of the process that runs it, which a read-only process is
	// not. Unavailable, not a fault: the same repository exports from the
	// process that writes it.
	ErrExportNoWriter = fmt.Errorf("%w: the export runs in the process that writes the repository, which a read-only process is not", substrate.ErrUnavailable)
	// ErrExportInProgress is the refusal of a second export while one
	// streams: an export holds blobs and a tar half written for as long as
	// the slowest client takes, so one per repository bounds what a
	// repository's exports can hold at once.
	ErrExportInProgress = fmt.Errorf("%w: an export of this repository is already streaming; wait for it to finish", substrate.ErrConflict)
)

// export is one pinned export: what Export read at the point, and what
// WriteTo streams from it.
type export struct {
	ds  *dataset
	ctx context.Context
	// released lets go of the export slot and the blob hold once, whether
	// WriteTo or Close gets there first: a second release would clear the
	// slot of an export started since.
	released sync.Once
	point    substrate.ExportPoint
	manifest changelogfile.Manifest
	sealed   map[string]changelogfile.SealedRecord
	blobs    []storedBlob
	spool    []spoolCopy
}

// Export pins the repository's committed point (substrate.Exporter).
func (ds *dataset) Export(ctx context.Context) (substrate.Export, error) {
	if ds.svc.readOnly {
		return nil, ErrExportNoWriter
	}
	if !ds.exporting.CompareAndSwap(false, true) {
		return nil, ErrExportInProgress
	}
	e, err := ds.pinExport(ctx)
	if err != nil {
		ds.holdExportBlobs(nil, false)
		ds.exporting.Store(false)
		return nil, err
	}
	return e, nil
}

func (ds *dataset) pinExport(ctx context.Context) (*export, error) {
	// Every blob is held from BEFORE the snapshot: a collection whose
	// tombstone the snapshot does not see deletes its bytes only after it, and
	// meets the hold. The hold narrows to the point's blobs once they are read.
	ds.holdExportBlobs(nil, true)
	e := &export{ds: ds, ctx: ctx}
	// The spool is read before the snapshot, for the reason the archive
	// pass reads it first (archive.go).
	var err error
	if e.spool, err = spoolFilesForCopy(ds.dir); err != nil {
		return nil, err
	}
	tx, err := ds.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	head, err := tableChangelogHead(ctx, tx)
	if err != nil {
		return nil, err
	}
	repo, err := ds.svc.repositoryByID(ctx, ds.info.ID)
	if err != nil {
		return nil, err
	}
	if e.manifest, err = ds.svc.manifestOf(ctx, repo, tx); err != nil {
		return nil, err
	}
	// The person who takes it, and nobody else: the other people's sign-ins
	// are theirs, not the export's (archiveidentities.go).
	if taker := substrate.IdentityFrom(ctx); taker != "" {
		if e.manifest.Identities, err = ds.svc.manifestIdentities(ctx, ds.info.ID, taker); err != nil {
			return nil, err
		}
	}
	e.manifest.Head, e.manifest.Epoch = head, 1
	// An archive of its own, past the one the database writes, so it is the
	// newest wherever it is put; and a pass of its own.
	var after []string
	if rec, ok, err := readArchiveRecord(ctx, tx); err != nil {
		return nil, err
	} else if ok {
		after = append(after, rec.archive)
	}
	if e.manifest.Archive, err = changelogfile.NewArchiveID(nowUTC(), after...); err != nil {
		return nil, err
	}
	if e.manifest.Pass, err = changelogfile.NewPassID(); err != nil {
		return nil, err
	}
	if head > 0 {
		if e.manifest.HeadSum, err = tableLineSum(ctx, tx, head); err != nil {
			return nil, err
		}
	}
	if e.sealed, err = readSealedTable(ctx, tx); err != nil {
		return nil, err
	}
	if e.blobs, err = storedBlobs(ctx, tx); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	held := make(map[string]bool, len(e.blobs))
	for _, b := range e.blobs {
		held[b.digest] = true
	}
	ds.holdExportBlobs(held, false)
	e.point = substrate.ExportPoint{
		ID: ds.scope.Repository, Authority: ds.name(), Head: head, TakenAt: nowUTC(),
		SealedFiles: len(e.sealed), Blobs: len(e.blobs),
	}
	if head > 0 {
		e.point.HeadHash = e.manifest.HeadSum[len("sha256:"):]
	}
	return e, nil
}

// Point is the pinned point (substrate.Export).
func (e *export) Point() substrate.ExportPoint { return e.point }

// Close lets go of the repository's export slot and the blob hold
// (substrate.Export).
func (e *export) Close() {
	e.released.Do(func() {
		e.ds.holdExportBlobs(nil, false)
		e.ds.exporting.Store(false)
	})
}

// WriteTo streams the tar (substrate.Export) and releases the repository's
// export slot when it returns, however it returns. The order is an archive
// pass's: what the history names first, the history, the manifest last.
func (e *export) WriteTo(w io.Writer) (int64, error) {
	defer e.Close()
	cw := &countingWriter{w: w}
	tw := tar.NewWriter(cw)
	if err := e.write(tw); err != nil {
		return cw.n, err
	}
	if err := tw.Close(); err != nil {
		return cw.n, err
	}
	return cw.n, nil
}

func (e *export) write(tw *tar.Writer) error {
	id, m := e.point.ID, &e.manifest
	prefix := changelogfile.ArchivePrefix(id, m.Archive)
	t := &tarStore{tw: tw, at: e.point.TakenAt}
	for _, dir := range []string{
		changelogfile.RepositoriesDir + "/", changelogfile.RepositoryPrefix(id), prefix,
		prefix + changelogfile.BlobsSubdir + "/", prefix + changelogfile.SealedSubdir + "/", prefix + changelogfile.SpoolSubdir + "/",
		prefix + changelogfile.ChangelogSubdir + "/", prefix + changelogfile.ChangelogSubdir + "/" + strconv.Itoa(m.ChangelogDialect) + "/",
		changelogfile.ManifestsPrefix(id), changelogfile.ArchiveManifestsPrefix(id, m.Archive),
	} {
		if err := t.dir(dir); err != nil {
			return err
		}
	}
	local, err := e.ds.blobBytes()
	if err != nil {
		return err
	}
	for _, b := range e.blobs {
		if err := e.writeBlob(t, prefix+changelogfile.BlobObjectName(b.digest, m.Pass), local, b); err != nil {
			return err
		}
	}
	m.Sealed = make(map[string]string, len(e.sealed))
	for _, ref := range sortedKeys(e.sealed) {
		data, sum, err := changelogfile.EncodeSealedVersion(e.sealed[ref])
		if err != nil {
			return err
		}
		name := changelogfile.SealedObjectName(ref, sum, m.Pass)
		m.Sealed[ref] = name
		if err := t.Put(e.ctx, prefix+name, bytes.NewReader(data), int64(len(data))); err != nil {
			return err
		}
	}
	for _, f := range e.spool {
		name := changelogfile.SpoolObjectName(f.name, m.Pass)
		m.Spool = append(m.Spool, name)
		if err := t.Put(e.ctx, prefix+name, bytes.NewReader(f.data), int64(len(f.data))); err != nil {
			return err
		}
	}
	if head := m.Head; head > 0 {
		written, _, err := writePieces(e.ctx, e.ds.db, 0, head, e.ds.svc.archivePieceBytes, nil, func(first, last int64, data []byte) (changelogfile.ArchivePiece, error) {
			sum := sha256.Sum256(data)
			name := changelogfile.PieceObjectName(m.ChangelogDialect, first, last, m.Pass, sum)
			if err := t.Put(e.ctx, prefix+name, bytes.NewReader(data), int64(len(data))); err != nil {
				return changelogfile.ArchivePiece{}, err
			}
			return changelogfile.ArchivePiece{Name: name, Sum: changelogfile.SumText(sum), Size: int64(len(data))}, nil
		})
		if err != nil {
			return err
		}
		m.Pieces = written
	}
	e.point.Pieces = len(m.Pieces)
	data, err := changelogfile.EncodeArchiveManifest(*m)
	if err != nil {
		return err
	}
	return t.Put(e.ctx, changelogfile.ArchiveManifestsPrefix(id, m.Archive)+changelogfile.ArchiveManifestName(m.Epoch, data), bytes.NewReader(data), int64(len(data)))
}

// writeBlob streams one blob's bytes, held to its digest and to the size its
// manifest declares, so a damaged blob ends the stream rather than reaching
// the archive. A `stored` manifest that declares no size is read whole first,
// because a tar header needs the size before the bytes.
func (e *export) writeBlob(t *tarStore, key string, store blobbytes.Store, b storedBlob) error {
	if b.size < 0 {
		data, err := blobbytes.ReadAll(e.ctx, store, b.digest, -1)
		if err != nil {
			return fmt.Errorf("substrate/engine: read blob %s: %w", b.digest, err)
		}
		return t.Put(e.ctx, key, bytes.NewReader(data), int64(len(data)))
	}
	rc, err := blobbytes.OpenVerified(e.ctx, store, b.digest, b.size)
	if err != nil {
		return fmt.Errorf("substrate/engine: open blob %s: %w", b.digest, err)
	}
	defer func() { _ = rc.Close() }()
	return t.stream(key, rc, b.size)
}

// tarStore is an archive store that is a tar being written: a put is an
// entry. Nothing is ever read back or deleted from it.
type tarStore struct {
	tw *tar.Writer
	at time.Time
}

func (t *tarStore) String() string { return "export" }

func (t *tarStore) dir(name string) error {
	return t.tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: name, Mode: 0o700, ModTime: t.at, Format: tar.FormatPAX})
}

func (t *tarStore) Put(_ context.Context, key string, body io.ReadSeeker, size int64) error {
	if size < 0 {
		end, err := body.Seek(0, io.SeekEnd)
		if err != nil {
			return err
		}
		if _, err := body.Seek(0, io.SeekStart); err != nil {
			return err
		}
		size = end
	}
	return t.stream(key, body, size)
}

func (t *tarStore) stream(key string, body io.Reader, size int64) error {
	if err := archive.CheckKey(key); err != nil {
		return err
	}
	if err := t.tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg, Name: path.Clean(key), Mode: 0o600, Size: size, ModTime: t.at, Format: tar.FormatPAX,
	}); err != nil {
		return err
	}
	n, err := io.Copy(t.tw, body)
	if err != nil {
		return err
	}
	if n != size {
		return fmt.Errorf("substrate/engine: %s: %d bytes where %d were declared", key, n, size)
	}
	return nil
}

func (t *tarStore) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, fmt.Errorf("substrate/engine: an export is written, never read")
}

func (t *tarStore) List(context.Context, string) ([]archive.Object, error) {
	return nil, fmt.Errorf("substrate/engine: an export is written, never listed")
}

func (t *tarStore) Delete(context.Context, string) error {
	return fmt.Errorf("substrate/engine: an export is written, never deleted from")
}

// holdExportBlobs replaces the set of digests the streaming export holds
// against byte deletion, or holds every digest while all is set; nil and
// false release them.
func (ds *dataset) holdExportBlobs(held map[string]bool, all bool) {
	ds.exportHeldMu.Lock()
	ds.exportHeld, ds.exportHoldAll = held, all
	ds.exportHeldMu.Unlock()
}

// holdArchiveBlobs is holdExportBlobs for the archive pass: the digests it
// is copying, or every digest while all is set.
func (ds *dataset) holdArchiveBlobs(held map[string]bool, all bool) {
	ds.exportHeldMu.Lock()
	ds.archiveHeld, ds.archiveHoldAll = held, all
	ds.exportHeldMu.Unlock()
}

// blobHeld reports whether the streaming export or the archive pass holds
// digest.
func (ds *dataset) blobHeld(digest string) bool {
	ds.exportHeldMu.Lock()
	defer ds.exportHeldMu.Unlock()
	return ds.exportHoldAll || ds.exportHeld[digest] || ds.archiveHoldAll || ds.archiveHeld[digest]
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
