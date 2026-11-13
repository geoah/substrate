package engine

// THE HISTORY IS THE CHANGELOG TABLE (decision 0262). Every fold is folded
// from it: a rebuild, an import, a conversion. What comes INTO the table from
// outside comes through one path, importHistory, from a SOURCE: a
// repository's archive, or the directory a release before the archive wrote
// (a 0.x directory). The source's lines past the table's head are inserted,
// the history is converted to this binary's dialect, and the fold is rebuilt
// from the table, under the import-progress marker from the first insert to
// the last fold pass, so a boot cut short anywhere finishes the import before
// the repository is served.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/geoah/substrate/internal/archive"
	"github.com/geoah/substrate/internal/blobbytes"
	"github.com/geoah/substrate/internal/changelogfile"
)

// historyCursor reads a history's entries in seq order.
type historyCursor interface {
	Next(n int) ([]changelogfile.Entry, error)
	Close() error
}

// historySource is a history an import reads from.
type historySource interface {
	// Head is the last seq the source holds.
	Head() int64
	// Dialect is the changelog dialect the source's lines are in.
	Dialect() int
	// Cursor reads the entries after seq after.
	Cursor(ctx context.Context, after int64) (historyCursor, error)
	// Sealed is every sealed value the source holds.
	Sealed(ctx context.Context) ([]changelogfile.SealedRecord, error)
	// CopyFiles reads every blob the source holds that local lacks into it,
	// and hands every spooled webhook request it selects to spool. The
	// import's local is the server's store and its spool the repository's
	// directory; a take's admission reads the same objects into neither
	// (admitSource).
	CopyFiles(ctx context.Context, local blobbytes.Store, spool func(file string, data []byte) error) error
	// String names the source for a log line.
	String() string
}

// tableCursor reads the changelog table: the fold's one source.
type tableCursor struct {
	ctx   context.Context
	q     dbx
	after int64
}

func (c *tableCursor) Next(n int) ([]changelogfile.Entry, error) {
	page, err := scanChecksumPage(c.ctx, c.q, c.after, n)
	if err != nil {
		return nil, err
	}
	out := make([]changelogfile.Entry, len(page))
	for i, row := range page {
		out[i] = row.entry.fileEntry()
	}
	if len(out) > 0 {
		c.after = out[len(out)-1].Seq
	}
	return out, nil
}

func (c *tableCursor) Close() error { return nil }

// dirSource is a 0.x directory's segments and sealed files; its blobs are
// already where the server keeps them.
type dirSource struct {
	dir     string
	log     *changelogfile.Log
	dialect int
}

func (d dirSource) Head() int64 { return d.log.Head() }

func (d dirSource) Dialect() int { return max(d.dialect, 1) }

func (d dirSource) Cursor(_ context.Context, after int64) (historyCursor, error) {
	return d.log.Cursor(after), nil
}

func (d dirSource) Sealed(context.Context) ([]changelogfile.SealedRecord, error) {
	return changelogfile.ReadSealed(d.dir)
}

func (d dirSource) CopyFiles(context.Context, blobbytes.Store, func(string, []byte) error) error {
	return nil
}

func (d dirSource) String() string { return d.dir }

// archiveSource is one manifest of a repository's archive (decision 0279):
// the history its pieces hold, the sealed values and the spool it selects,
// and the blobs of its archive.
type archiveSource struct {
	store archive.Store
	// repository and archive name where it is; name is the manifest's own.
	repository, archive, name string
	manifest                  changelogfile.Manifest
	chain                     []changelogfile.Piece
	sums                      map[string]string
}

// errArchiveCovered is a restore's refusal of an archive a deletion mark
// covers (rule 7): the repository was deleted, and only an explicit import
// brings it back.
var errArchiveCovered = errors.New("substrate/engine: the repository's newest archive is covered by its deletion mark")

// errArchiveAmbiguous is a reader's refusal of an archive holding two
// manifests of its highest epoch: one of them is not this archive's, and
// which is not decided here (rule 8).
var errArchiveAmbiguous = errors.New("substrate/engine: the archive holds two manifests of its highest epoch")

// restorable is the manifest a restore with no rows takes (rule 8): the
// newest archive's newest manifest, when that archive is newer than every
// deletion mark. archive.ErrNotFound when no archive has a manifest.
func restorable(listed archiveListing) (archiveID, name string, err error) {
	newest := listed.newest()
	if newest == "" {
		return "", "", archive.ErrNotFound
	}
	if mark := listed.newestMark(); mark > newest {
		return "", "", fmt.Errorf("%w: mark %s, archive %s", errArchiveCovered, mark, newest)
	}
	tops := listed[newest].tops()
	if len(tops) != 1 {
		return "", "", fmt.Errorf("%w: archive %s, %s", errArchiveAmbiguous, newest, strings.Join(tops, ", "))
	}
	return newest, tops[0], nil
}

// openNewestArchive opens the manifest a restore with no rows would take
// (restorable).
func openNewestArchive(ctx context.Context, store archive.Store, id string, admit func(changelogfile.Manifest) error) (*archiveSource, error) {
	listed, err := listArchives(ctx, store, id)
	if err != nil {
		return nil, err
	}
	archiveID, name, err := restorable(listed)
	if err != nil {
		return nil, err
	}
	return openArchiveSource(ctx, store, id, archiveID, name, admit)
}

// openArchiveSource reads manifest name of archive archiveID of repository
// id, held to its name, its repository and its archive. A manifest that went
// while it was read is read again once, from a listing, before the read is
// refused (rule 8). admit, when set, is held to the manifest before anything
// else is read: a history this binary cannot read is refused by what it
// names, not by a piece it cannot find.
func openArchiveSource(ctx context.Context, store archive.Store, id, archiveID, name string, admit func(changelogfile.Manifest) error) (*archiveSource, error) {
	return openArchiveManifest(ctx, store, id, archiveID, name, true, admit)
}

// openArchiveManifest is openArchiveSource, reading again from a listing
// when again is set, and refusing a manifest that is gone when it is not.
func openArchiveManifest(ctx context.Context, store archive.Store, id, archiveID, name string, again bool, admit func(changelogfile.Manifest) error) (*archiveSource, error) {
	key := changelogfile.ArchiveManifestsPrefix(id, archiveID) + name
	data, err := archive.ReadAll(ctx, store, key)
	if again && errors.Is(err, archive.ErrNotFound) {
		listed, lerr := listArchives(ctx, store, id)
		if lerr != nil {
			return nil, lerr
		}
		if archiveID, name, err = restorable(listed); err != nil {
			return nil, err
		}
		key = changelogfile.ArchiveManifestsPrefix(id, archiveID) + name
		data, err = archive.ReadAll(ctx, store, key)
	}
	if errors.Is(err, archive.ErrNotFound) {
		return nil, fmt.Errorf("%w: %s", err, key)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errArchiveUnreadable, err)
	}
	if !changelogfile.ManifestNamesBytes(name, data) {
		return nil, fmt.Errorf("%w: %s is not the manifest its name says", ErrChangelogDiverged, key)
	}
	m, err := changelogfile.DecodeArchiveManifest(data, id)
	if err != nil {
		return nil, err
	}
	if m.Archive != archiveID {
		return nil, fmt.Errorf("%w: %s names archive %s", ErrChangelogDiverged, key, m.Archive)
	}
	if admit != nil {
		if err := admit(m); err != nil {
			return nil, err
		}
	}
	chain, err := m.Chain()
	if err != nil {
		return nil, err
	}
	prefix := changelogfile.ArchivePrefix(id, archiveID)
	sums := make(map[string]string, len(chain))
	for i := range chain {
		sums[prefix+chain[i].Key] = m.Pieces[i].Sum
		chain[i].Key = prefix + chain[i].Key
	}
	return &archiveSource{store: store, repository: id, archive: archiveID, name: name, manifest: m, chain: chain, sums: sums}, nil
}

func (a *archiveSource) Head() int64 { return a.manifest.Head }

func (a *archiveSource) Dialect() int { return max(a.manifest.ChangelogDialect, 1) }

func (a *archiveSource) String() string {
	return a.store.String() + "/" + changelogfile.ArchiveManifestsPrefix(a.repository, a.archive) + a.name
}

func (a *archiveSource) Cursor(ctx context.Context, after int64) (historyCursor, error) {
	return &pieceCursor{ctx: ctx, src: a, after: after}, nil
}

// objectKey is the key of an object the manifest names under its archive.
func (a *archiveSource) objectKey(name string) string {
	return changelogfile.ArchivePrefix(a.repository, a.archive) + name
}

// Sealed is the version of each sealed value the manifest selects, and no
// other: a version beside it is another pass's.
func (a *archiveSource) Sealed(ctx context.Context) ([]changelogfile.SealedRecord, error) {
	out := make([]changelogfile.SealedRecord, 0, len(a.manifest.Sealed))
	for _, ref := range sortedKeys(a.manifest.Sealed) {
		name := a.manifest.Sealed[ref]
		sum, _, ok := changelogfile.ParseSealedObjectName(ref, name)
		if !ok {
			return nil, fmt.Errorf("%w: sealed %s names %q", changelogfile.ErrManifestFormat, ref, name)
		}
		data, err := archive.ReadAll(ctx, a.store, a.objectKey(name))
		if err != nil {
			return nil, fmt.Errorf("%w: the sealed value %s the manifest selects, %s: %w", errArchiveUnreadable, ref, a.objectKey(name), err)
		}
		rec, err := changelogfile.DecodeSealedVersion(ref, sum, data)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

// CopyFiles brings in the spool the manifest selects and the bytes of every
// blob the archive holds, one object per digest, each held to its digest as
// the blob store takes it.
func (a *archiveSource) CopyFiles(ctx context.Context, local blobbytes.Store, spool func(file string, data []byte) error) error {
	for _, name := range a.manifest.Spool {
		file, _, ok := changelogfile.ParseSpoolObjectName(name)
		if !ok {
			return fmt.Errorf("%w: spool %q", changelogfile.ErrManifestFormat, name)
		}
		data, err := archive.ReadAll(ctx, a.store, a.objectKey(name))
		if err != nil {
			return fmt.Errorf("%w: the spooled request %s the manifest selects: %w", errArchiveUnreadable, file, err)
		}
		if err := spool(file, data); err != nil {
			return err
		}
	}
	listed, err := a.store.List(ctx, a.objectKey(changelogfile.BlobsSubdir+"/"))
	if err != nil {
		return fmt.Errorf("%w: %w", errArchiveUnreadable, err)
	}
	prefix := changelogfile.ArchivePrefix(a.repository, a.archive)
	for _, o := range listed {
		digest, _, ok := changelogfile.ParseBlobObjectName(strings.TrimPrefix(o.Key, prefix))
		if !ok {
			continue
		}
		if ok, err := local.Exists(ctx, digest); err != nil || ok {
			if err != nil {
				return err
			}
			continue
		}
		rc, err := a.store.Get(ctx, o.Key)
		if errors.Is(err, archive.ErrNotFound) {
			// Another object of the digest may hold it; one that went is a
			// delete of an object no manifest selects.
			continue
		}
		if err != nil {
			return err
		}
		err = local.Put(ctx, digest, o.Size, rc)
		_ = rc.Close()
		if err != nil {
			return fmt.Errorf("substrate/engine: copy the archive's blob %s: %w", digest, err)
		}
	}
	return nil
}

// pieceCursor reads an archive's chain one piece at a time.
type pieceCursor struct {
	ctx     context.Context
	src     *archiveSource
	after   int64
	pending []changelogfile.Entry
}

func (c *pieceCursor) Next(n int) ([]changelogfile.Entry, error) {
	for len(c.pending) == 0 {
		var piece *changelogfile.Piece
		for i, p := range c.src.chain {
			if p.First <= c.after+1 && c.after+1 <= p.Last {
				piece = &c.src.chain[i]
				break
			}
		}
		if piece == nil {
			return nil, nil
		}
		body, err := archive.ReadAll(c.ctx, c.src.store, piece.Key)
		if err != nil {
			return nil, err
		}
		if changelogfile.SumText(sha256.Sum256(body)) != c.src.sums[piece.Key] {
			return nil, fmt.Errorf("%w: %s is not the piece its manifest selects", ErrChangelogDiverged, piece.Key)
		}
		if err := changelogfile.ReadPiece(bytes.NewReader(body), piece.First, piece.Last, func(e changelogfile.Entry, _ []byte, _ [32]byte) error {
			if e.Seq > c.after {
				c.pending = append(c.pending, e)
			}
			return nil
		}); err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrChangelogDiverged, piece.Key, err)
		}
	}
	n = min(n, len(c.pending))
	out := c.pending[:n:n]
	c.pending = c.pending[n:]
	c.after = out[n-1].Seq
	return out, nil
}

func (c *pieceCursor) Close() error { return nil }

// entryAt is the source's line at seq.
func entryAt(ctx context.Context, src historySource, seq int64) (changelogfile.Entry, error) {
	cur, err := src.Cursor(ctx, seq-1)
	if err != nil {
		return changelogfile.Entry{}, err
	}
	defer func() { _ = cur.Close() }()
	entries, err := cur.Next(1)
	if err != nil {
		return changelogfile.Entry{}, err
	}
	if len(entries) != 1 || entries[0].Seq != seq {
		return changelogfile.Entry{}, fmt.Errorf("%w: %s holds no seq %d", ErrChangelogDiverged, src, seq)
	}
	return entries[0], nil
}

// sourceAgrees holds the source to the table at the table's head: the line
// both hold there is one line, in whichever dialect each is in, so the
// source's lines past it continue the table's history and do not replace it.
func (ds *dataset) sourceAgrees(ctx context.Context, src historySource, tableHead int64) error {
	if tableHead == 0 {
		return nil
	}
	if src.Head() < tableHead {
		return nil
	}
	e, err := entryAt(ctx, src, tableHead)
	if err != nil {
		return err
	}
	same, err := ds.sameLineAsTable(ctx, ds.db, e, src.Dialect())
	if err != nil {
		return fmt.Errorf("%w: %s at seq %d: %w", ErrChangelogDiverged, src, tableHead, err)
	}
	if !same {
		return fmt.Errorf("%w: %s and the table hold different lines at seq %d", ErrChangelogDiverged, src, tableHead)
	}
	return nil
}

// changelogDialectClaimEmpty sets the claim of a history that holds no line
// yet to the dialect its first lines are in, whichever way that moves it:
// changelogDialectStamp only moves a claim up, which over an empty table
// would leave older lines claimed as converted. The table is checked empty
// in the statement itself.
const changelogDialectClaimEmpty = `
	INSERT INTO changelog_dialect (dialect) SELECT $1::int WHERE NOT EXISTS (SELECT 1 FROM changelog)
	ON CONFLICT (repository) DO UPDATE SET dialect = EXCLUDED.dialect, updated_at = now()`

// meetDialect holds the table to the source's changelog dialect before the
// source's lines past its head are put in it, so the table never holds lines
// of two dialects and no line is compared across a conversion: an empty
// table takes the source's dialect, down as well as up, and the import
// converts the whole of it after; a table that holds history in another
// dialect is refused.
func (ds *dataset) meetDialect(ctx context.Context, src historySource, tableHead int64) error {
	tableDialect, err := readChangelogDialect(ctx, ds.db)
	if err != nil {
		return err
	}
	if max(tableDialect, 1) == src.Dialect() {
		return nil
	}
	if tableHead == 0 {
		_, err := ds.db.ExecContext(ctx, changelogDialectClaimEmpty, src.Dialect())
		return err
	}
	return fmt.Errorf("%w: %s holds lines of changelog dialect %d past the table's head, and the table's are in dialect %d: lines of two dialects are never put in one table",
		ErrChangelogDiverged, src, src.Dialect(), max(tableDialect, 1))
}

// importHistory brings the source's lines past the table's head into the
// table, then converts and refolds, and reports how many lines it inserted.
// A source at or below the table's head brings nothing, and the caller's
// marker, if set, is finished all the same.
func (ds *dataset) importHistory(ctx context.Context, src historySource) (int64, error) {
	n, err := ds.takeHistory(ctx, src)
	if err != nil {
		return n, err
	}
	return n, ds.finishImport(ctx)
}

// takeHistory is importHistory up to the conversion: the source's lines past
// the table's head inserted, in the source's dialect, under the import
// marker, with its sealed values and files. A directory a release before the
// archive wrote is set aside between this and the conversion (rule 6).
func (ds *dataset) takeHistory(ctx context.Context, src historySource) (int64, error) {
	tableHead, err := tableChangelogHead(ctx, ds.db)
	if err != nil {
		return 0, err
	}
	if src == nil || src.Head() <= tableHead {
		return 0, nil
	}
	if err := ds.meetDialect(ctx, src, tableHead); err != nil {
		return 0, err
	}
	if err := ds.sourceAgrees(ctx, src, tableHead); err != nil {
		return 0, err
	}
	// The obligation first: from here the repository's sealed values and
	// files are the source's and its lines not yet, which the marker says
	// until the import finishes.
	if err := markImportIncomplete(ctx, ds.db, src.Head()); err != nil {
		return 0, err
	}
	local, err := ds.blobBytes()
	if err != nil {
		return 0, err
	}
	dir, err := ds.svc.repositoryDir(ds.info.ID)
	if err != nil {
		return 0, err
	}
	if err := src.CopyFiles(ctx, local, func(file string, data []byte) error {
		return changelogfile.WriteSpoolFile(dir, file, data)
	}); err != nil {
		return 0, err
	}
	sealed, err := src.Sealed(ctx)
	if err != nil {
		return 0, err
	}
	if err := loadSealed(ctx, ds.db, sealed); err != nil {
		return 0, err
	}
	if err := ds.importFault(importAfterSealed); err != nil {
		return 0, err
	}
	return ds.insertFrom(ctx, src, tableHead)
}

// insertFrom inserts the source's lines after `after`, in batches, each its
// own transaction: the marker set before the first is what tells a boot that
// the rows are not all in.
func (ds *dataset) insertFrom(ctx context.Context, src historySource, after int64) (int64, error) {
	batch := rebuildBatch
	if ds.svc.testImportBatch > 0 {
		batch = ds.svc.testImportBatch
	}
	// Said before the work: the rows and the two fold passes below log
	// nothing until the import is done, and a long history takes minutes.
	ds.svc.log.Info("substrate: importing a repository's history",
		"repository", ds.scope.Repository, "from", src.String(), "rows", src.Head()-after, "head", src.Head())
	prog := ds.svc.progress("importing the changelog rows", "repository", ds.scope.Repository, "head", src.Head())
	return readHistory(ctx, src, after, batch, func(entries []changelogfile.Entry) error {
		if err := ds.insertEntries(ctx, entries); err != nil {
			return err
		}
		prog.tick(changelogfile.Position{Seq: entries[len(entries)-1].Seq})
		return ds.importFault(importAfterBatch)
	})
}

// readHistory reads src's lines after `after`, batch at a time, each checked
// as its cursor reads it (an archive's piece against its hash, every line
// against its own sum and the run the piece's name says), and hands each
// batch to put. It is the import's one reading of a source's history, and a
// take's admission runs it too, with a put that keeps nothing
// (admitSource), so what it refuses is refused before a take removes
// anything. It reports how many lines it read.
func readHistory(ctx context.Context, src historySource, after int64, batch int, put func([]changelogfile.Entry) error) (int64, error) {
	cur, err := src.Cursor(ctx, after)
	if err != nil {
		return 0, err
	}
	defer func() { _ = cur.Close() }()
	var n int64
	for {
		entries, err := cur.Next(batch)
		if err != nil {
			return n, fmt.Errorf("%w: %w", ErrChangelogDiverged, err)
		}
		if len(entries) == 0 {
			return n, nil
		}
		if err := put(entries); err != nil {
			return n, err
		}
		n += int64(len(entries))
	}
}

// finishImport converts the history and rebuilds the fold when the marker
// says the fold is not the table's: an import, a conversion, or a boot that
// one of them was cut short in. A marker whose head the table has not reached
// is an import whose source is still to be read: the caller reads it first.
func (ds *dataset) finishImport(ctx context.Context) error {
	if _, err := ds.convertTable(ctx); err != nil {
		return err
	}
	marked, incomplete, err := importIncomplete(ctx, ds.db)
	if err != nil || !incomplete {
		return err
	}
	tableHead, err := tableChangelogHead(ctx, ds.db)
	if err != nil {
		return err
	}
	if tableHead < marked {
		return importIncompleteErr(ds.info.ID, marked)
	}
	return ds.refold(ctx)
}

// loadSealed writes the source's sealed values into the table, replacing
// what the table holds under the same ref: the import's direction is the
// source's.
func loadSealed(ctx context.Context, q dbx, recs []changelogfile.SealedRecord) error {
	for _, rec := range recs {
		var expires any
		if rec.ExpiresAt != nil {
			expires = rec.ExpiresAt.UTC()
		}
		if _, err := q.ExecContext(ctx, `
			INSERT INTO sealed (ref, record_kind, record_id, payload, expires_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (repository, ref) DO UPDATE
			    SET record_kind = EXCLUDED.record_kind, record_id = EXCLUDED.record_id, payload = EXCLUDED.payload,
			        expires_at = EXCLUDED.expires_at, updated_at = EXCLUDED.updated_at`,
			rec.Ref, rec.RecordKind, rec.RecordID, rec.Payload, expires, rec.UpdatedAt.UTC()); err != nil {
			return fmt.Errorf("substrate/engine: import sealed %s: %w", rec.Ref, err)
		}
	}
	return nil
}

// errArchiveUnreadable marks a failure to read the archive's store, which
// may pass, apart from what was read and refused, which does not.
var errArchiveUnreadable = errors.New("substrate/engine: the archive cannot be read")

// errNoArchive is what a lookup of a repository's archive reports when the
// server writes none.
var errNoArchive = errors.New("substrate/engine: this server writes no archive")
