package engine

// The verification walk: the changelog table row by row (its seqs, its
// transaction frames, and with VerifyOptions.Recanonicalize every row's
// checksum recomputed from its stored columns, in the frame of the line it
// was written as, since a verify opened read-only runs no conversion and may
// meet a history the boot has not converted, which is a finding of its own);
// the archive, where the
// process has one, its manifest and every line of its pieces held to the
// table's stamped checksums; the sealed rows against the records that should
// hold their refs and, under the credential key, opened; and every stored
// blob's bytes against its digest. It MUTATES NOTHING: the table is read
// inside one repeatable-read transaction, so a concurrent write cannot make
// it stitch two states into one report.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/archive"
	"github.com/geoah/substrate/internal/blobbytes"
	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// verifyFindingCap bounds the report: after this many findings the walk
// stops, because a corrupted store names every row and the first page is the
// story.
const verifyFindingCap = 20

// VerifyReport is what one verification saw.
type VerifyReport struct {
	Repository string `json:"repository"`
	// Entries and Head are the table's: how many rows, and the highest seq.
	Entries int64 `json:"entries"`
	Head    int64 `json:"head"`
	// HeadHash is the head entry's checksum, hex.
	HeadHash string `json:"headHash,omitempty"`
	// Archive names the store the archive was read from, empty when the
	// process has none; ArchiveHead is its manifest's head, and
	// ArchivePieces the pieces of the chain to it, every line held to the
	// table.
	Archive       string `json:"archive,omitempty"`
	ArchiveHead   int64  `json:"archiveHead"`
	ArchivePieces int    `json:"archivePieces"`
	// ArchiveBlobs is how many blobs were read back out of the archive and
	// held to the digests that name them.
	ArchiveBlobs int `json:"archiveBlobs"`
	// ArchiveUnverified is why the archive's lines were not all held to the
	// table, empty when they were: no copy published to the store yet, a
	// store that could not be read, a manifest gone, history in an earlier
	// dialect than the table's. Every reason but the first is a finding too.
	ArchiveUnverified string `json:"archiveUnverified,omitempty"`
	// ArchiveID is the archive the record names, and ArchiveUnaccounted how
	// many objects the store holds under it, manifests included, that the
	// record does not: a put applied late, or a manifest a database
	// restored from a backup forgot it put. Nothing reads them. It is -1
	// from the moment the archive is looked at until a whole count is made,
	// so a verify that stopped short of the count (a store that would not
	// list or read, a manifest gone) says unknown, which is a finding too,
	// and never zero.
	ArchiveID          string `json:"archiveId,omitempty"`
	ArchiveUnaccounted int    `json:"archiveUnaccounted"`
	// SealedRows counts the sealed table.
	SealedRows int `json:"sealedRows"`
	// SealedOpened is how many sealed rows opened under the repository's
	// DEK: every one, or the walk found the rest; 0 with no credential key,
	// when nothing is opened.
	SealedOpened int `json:"sealedOpened"`
	// SealedOrphans is how many sealed rows no live or tombstoned record
	// holds (credentials.go sealedUnheld). Each is a finding, and the next
	// writable open or GC sweep erases them.
	SealedOrphans int `json:"sealedOrphans"`
	// SecretRefs is how many secret references live records hold, each held
	// to a sealed row.
	SecretRefs int `json:"secretRefs"`
	// Blobs and BlobBytes are the `stored` blob manifests, each one's bytes
	// read from the store and hashed against its digest.
	Blobs     int   `json:"blobs"`
	BlobBytes int64 `json:"blobBytes"`
	// Recanonicalized is whether every table row's checksum was recomputed
	// from its stored columns (VerifyOptions.Recanonicalize).
	Recanonicalized bool          `json:"recanonicalized,omitempty"`
	Findings        []string      `json:"findings,omitempty"`
	Truncated       bool          `json:"truncated,omitempty"`
	OK              bool          `json:"ok"`
	Took            time.Duration `json:"took"`
}

// VerifyOptions tunes VerifyRepositoryWith.
type VerifyOptions struct {
	// Recanonicalize recomputes every table row's checksum from its stored
	// columns, the payload canonicalized, and holds it to the row's stamped
	// hash. Without it the table pass reads no payload, and the rows are held
	// to their stamps only where the archive holds the line: the archive's
	// lines carry their own sums, verified against their bytes. A row whose
	// columns were edited in place with its hash left alone is therefore
	// found only with it. It reads and canonicalizes every payload the table
	// holds, which on a history of millions of entries is hours (issue 761).
	Recanonicalize bool
}

// verifyTableBatch is the table pass's page when it reads no payload: a row
// is a seq, a txn and 32 bytes, so a page is a round trip that moves little.
const verifyTableBatch = 10000

// VerifyRepository walks one repository's history. Findings land in the
// report, not in the error: the error is for "could not verify" (no such
// repository, no connection), never for "verified and found damage".
func (s *service) VerifyRepository(ctx context.Context, repository string) (VerifyReport, error) {
	return s.VerifyRepositoryWith(ctx, repository, VerifyOptions{})
}

// VerifyRepositoryWith is VerifyRepository with its options.
func (s *service) VerifyRepositoryWith(ctx context.Context, repository string, opts VerifyOptions) (VerifyReport, error) {
	started := time.Now()
	repo, err := s.repositoryByKey(ctx, repository)
	if err != nil {
		return VerifyReport{}, err
	}
	// The open's refusal (engine.go open): a repository still keyed by its
	// name is read under its id only once a server that writes converted it.
	if !vocabulary.ValidRepositoryID(repo.ID) {
		return VerifyReport{}, fmt.Errorf("%w: repository %s", ErrRepositoryNotKeyed, repo.Authority)
	}
	report := VerifyReport{Repository: repo.ID, Recanonicalized: opts.Recanonicalize}
	found := func(f string) {
		if len(report.Findings) >= verifyFindingCap {
			report.Truncated = true
			return
		}
		report.Findings = append(report.Findings, f)
	}
	// A bare scoped pool: the RLS-bound shape every request rides, with none
	// of the open ladder's writes.
	db, err := s.scopedDB(repo.scope())
	if err != nil {
		return VerifyReport{}, err
	}
	defer func() { _ = db.Close() }()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return VerifyReport{}, err
	}
	defer func() { _ = tx.Rollback() }()

	// The import-progress marker (repodir.go) first: a repository whose
	// import began and did not complete has a fold that is not its
	// history's, so a report with no finding would be a lie an operator acts
	// on.
	markedHead, incomplete, err := importIncomplete(ctx, tx)
	if err != nil {
		return report, err
	}
	if incomplete {
		found(fmt.Sprintf("import: the import of the repository's history has not completed (marked at head %d): the fold is not the history's until the server's next boot finishes it", markedHead))
	}
	// A history below this binary's dialect is one the boot has not
	// converted yet (an upgrade stopped before the conversion committed, or a
	// repository set aside at it). Its rows are whole in their own frame, and
	// the fold is not this binary's reading of them until the boot converts.
	stamped, err := readChangelogDialect(ctx, tx)
	if err != nil {
		return report, err
	}
	if pending := pendingConversions(s.conversion, stamped); len(pending) > 0 {
		head, err := tableChangelogHead(ctx, tx)
		if err != nil {
			return report, err
		}
		if head > 0 {
			found(fmt.Sprintf("conversion: the history is in changelog dialect %d, and this binary's conversion to %d (%s) has not run: the server's next boot converts it before the repository opens",
				max(stamped, 1), pending[len(pending)-1].Dialect, conversionNames(pending)))
		}
	}

	// The table, row by row: the seq sequence, the transaction frame, and
	// under Recanonicalize each row's checksum recomputed from its columns.
	expected := int64(1)
	prog := s.progress("verifying the changelog table", "repository", repo.ID)
	var openTxn int64
	for {
		var page []checksumRow
		if opts.Recanonicalize {
			page, err = scanChecksumPage(ctx, tx, expected-1, rebuildBatch)
		} else {
			page, err = scanStampPage(ctx, tx, expected-1, verifyTableBatch)
		}
		if err != nil {
			return report, err
		}
		if len(page) == 0 {
			break
		}
		for _, row := range page {
			if row.entry.Seq != expected {
				found(fmt.Sprintf("seq %d follows %d: the sequence has a gap", row.entry.Seq, expected-1))
				expected = row.entry.Seq
			}
			expected++
			switch txn := row.entry.Txn; {
			case txn == 0:
				found(fmt.Sprintf("seq %d carries no txn", row.entry.Seq))
				openTxn = 0
			case txn < row.entry.Seq:
				found(fmt.Sprintf("seq %d ends its transaction at %d, before itself", row.entry.Seq, txn))
				openTxn = 0
			case openTxn != 0 && txn != openTxn:
				found(fmt.Sprintf("seq %d carries txn %d inside the transaction ending at %d", row.entry.Seq, txn, openTxn))
				openTxn = 0
			case txn == row.entry.Seq:
				openTxn = 0
			default:
				openTxn = txn
			}
			report.Entries++
			report.Head = row.entry.Seq
			report.HeadHash = ""
			switch {
			case row.hash == nil:
				found(fmt.Sprintf("seq %d: no checksum", row.entry.Seq))
				continue
			case len(row.hash) != 32:
				found(fmt.Sprintf("seq %d: checksum is %d bytes, want 32", row.entry.Seq, len(row.hash)))
				continue
			}
			report.HeadHash = hex.EncodeToString(row.hash)
			if opts.Recanonicalize {
				if _, _, err := changelogfile.Encode(row.entry.fileEntry()); err != nil {
					found(fmt.Sprintf("seq %d: payload does not canonicalize: %v", row.entry.Seq, err))
					continue
				}
				// A row is held to the frame its checksum was computed in:
				// a history the boot has not converted yet holds whole rows
				// in an earlier frame (framedLine).
				switch _, frame := framedLine(row); {
				case frame == frameNone:
					found(fmt.Sprintf("seq %d: checksum mismatch, the stored row is not what was stamped", row.entry.Seq))
				case frame == framePrincipal && stamped >= tokenFrameDialect:
					found(fmt.Sprintf("seq %d: the row is in the frame of changelog dialect %d or earlier, and the history is stamped %d: a conversion passed it over", row.entry.Seq, tokenFrameDialect-1, stamped))
				}
			}
		}
		prog.tick(changelogfile.Position{Seq: report.Head})
	}
	if openTxn != 0 {
		found(fmt.Sprintf("the table ends inside the transaction ending at seq %d", openTxn))
	}

	if s.archive != nil {
		if err := s.verifyArchive(ctx, tx, repo, &report, found); err != nil {
			return report, err
		}
	}

	rows, err := readSealedTable(ctx, tx)
	if err != nil {
		return report, err
	}
	report.SealedRows = len(rows)
	if err := verifyUnheldSealed(ctx, tx, rows, &report, found); err != nil {
		return report, err
	}
	// The side stores against the fold: a `stored` manifest whose bytes are
	// missing or not its digest's, and a live secret reference with no
	// sealed row.
	if err := s.verifyBlobs(ctx, tx, repo, &report, found); err != nil {
		return report, err
	}
	if err := s.verifySecretRefs(ctx, tx, db, repo, rows, &report, found); err != nil {
		return report, err
	}
	// Under the credential key, every sealed row opened: the bytes being
	// there says nothing about whether they are ciphertext the DEK opens.
	s.verifySealedOpen(repo, rows, &report, found)

	report.OK = len(report.Findings) == 0
	report.Took = time.Since(started)
	return report, nil
}

// verifyArchive holds the repository's archive to the table and to the
// record (rule 8): the archive the record names is the one a pass would go
// on in, its published manifest is there, every object it selects is there
// with its bytes, and every line of its pieces carries the table's stamped
// checksum. An archive behind the table is not a finding, because it is
// copied after the commits. What was not proven is said (ArchiveUnverified),
// and is a finding wherever a copy was published: a store that cannot be
// read, a manifest gone, an object that went while it was read and is gone
// when read again, or lines in an earlier dialect than the table's, which
// nothing here holds to the table's. It proves what the store holds when it
// runs, and cannot prove that the store hides no other manifest.
func (s *service) verifyArchive(ctx context.Context, tx dbx, repo Repository, report *VerifyReport, found func(string)) error {
	report.Archive = s.archive.String()
	unverified := func(reason string, finding bool) {
		report.ArchiveUnverified = reason
		if finding {
			found("archive: not verified: " + reason)
		}
	}
	rec, ok, err := readArchiveRecord(ctx, tx)
	if err != nil {
		return err
	}
	if !ok || (rec.published == "" && rec.pending == nil) {
		unverified("no pass has published a copy of the repository to this store yet", false)
		return nil
	}
	report.ArchiveID, report.ArchiveUnaccounted = rec.archive, -1
	if rec.stopped != "" {
		found("archive: its passes stopped, and write nothing until an operator resolves it (`substratectl archive report`): " + rec.stopped)
	}
	listed, err := listArchives(ctx, s.archive, repo.ID)
	if err != nil {
		unverified(fmt.Sprintf("the store could not be read: %v", err), true)
		return nil
	}
	if mark := listed.newestMark(); mark >= rec.archive {
		found(fmt.Sprintf("archive: the repository's deletion mark %s covers archive %s, and no boot would restore it", mark, rec.archive))
	}
	if newest := listed.newest(); newest > rec.archive {
		found(fmt.Sprintf("archive: the store holds archive %s, newer than archive %s the database writes, and a boot with no rows would restore that one", newest, rec.archive))
	}
	if tops := listed[rec.archive].tops(); len(tops) > 1 {
		found(fmt.Sprintf("archive: archive %s holds %d manifests of its highest epoch, and no boot chooses between them", rec.archive, len(tops)))
	} else if len(tops) == 1 && tops[0] != rec.published {
		found(fmt.Sprintf("archive: archive %s's newest manifest is %s, which the database did not publish", rec.archive, tops[0]))
	}
	if rec.published == "" {
		unverified("the first manifest of archive "+rec.archive+" is still to be published", true)
		return nil
	}
	src, err := openArchiveManifest(ctx, s.archive, repo.ID, rec.archive, rec.published, false, nil)
	switch {
	case errors.Is(err, archive.ErrNotFound) && len(listed) == 0 && s.storeHoldsNothingOf(ctx, repo.ID):
		unverified(fmt.Sprintf("the store holds nothing of the repository, where a pass published manifest %s of archive %s: it was emptied or replaced, or the server now writes to another store; the next pass starts a new archive in it", rec.published, rec.archive), true)
		return nil
	case errors.Is(err, archive.ErrNotFound):
		unverified(fmt.Sprintf("the store holds no manifest %s of archive %s, which a pass published: the copy is gone, and a boot would restore nothing from it", rec.published, rec.archive), true)
		return nil
	case err != nil:
		unverified(fmt.Sprintf("the published manifest does not read: %v", err), true)
		return nil
	}
	report.ArchiveHead, report.ArchivePieces = src.Head(), len(src.chain)
	if src.Head() > report.Head {
		found(fmt.Sprintf("archive: its head is seq %d, past the table's %d: it holds history the table does not", src.Head(), report.Head))
		report.ArchiveUnverified = "it holds history the table does not"
		return nil
	}
	// Every object the manifest selects, read again once when it went
	// while it was read.
	read := func(key string) ([]byte, error) {
		data, err := archive.ReadAll(ctx, s.archive, key)
		if errors.Is(err, archive.ErrNotFound) {
			data, err = archive.ReadAll(ctx, s.archive, key)
		}
		return data, err
	}
	for _, ref := range sortedKeys(src.manifest.Sealed) {
		name := src.manifest.Sealed[ref]
		sum, _, ok := changelogfile.ParseSealedObjectName(ref, name)
		err := fmt.Errorf("%w: %q", changelogfile.ErrManifestFormat, name)
		if ok {
			var data []byte
			if data, err = read(src.objectKey(name)); err == nil {
				_, err = changelogfile.DecodeSealedVersion(ref, sum, data)
			}
		}
		if err != nil {
			found(fmt.Sprintf("archive: the sealed value %s the manifest selects: %v", ref, err))
		}
	}
	for _, name := range src.manifest.Spool {
		if _, err := read(src.objectKey(name)); err != nil {
			found(fmt.Sprintf("archive: the spooled request %s the manifest selects: %v", name, err))
		}
	}
	if missing := src.manifest.MissingBlobs; len(missing) > 0 {
		found(fmt.Sprintf("archive: %d blob(s) the records hold are not in it, their bytes lost on this server: %s",
			len(missing), strings.Join(missing[:min(len(missing), 5)], ", ")))
	}
	if pending := src.manifest.PendingBlobs; len(pending) > 0 {
		found(fmt.Sprintf("archive: %d blob(s) the records hold are not copied yet; the next passes copy them", len(pending)))
	}
	if err := s.verifyArchivedBlobs(ctx, tx, src, report, found); err != nil {
		return err
	}
	if err := s.countUnaccounted(ctx, tx, src, report); err != nil {
		unverified(fmt.Sprintf("the objects the database has no record of could not be counted: %v", err), true)
		return nil
	}
	// Lines of an archive in an earlier dialect than the table's are not the
	// table's lines, and nothing here converts them to compare: the next
	// pass writes every piece again in the table's.
	if dialect, err := readChangelogDialect(ctx, tx); err != nil {
		return err
	} else if max(dialect, 1) != src.Dialect() {
		unverified(fmt.Sprintf("its history is in changelog dialect %d and the table's in %d, so no line of it was held to the table; the next archive pass writes it again in dialect %d",
			src.Dialect(), max(dialect, 1), max(dialect, 1)), true)
		return nil
	}
	prog := s.progress("verifying the archive against the table", "repository", repo.ID)
	var stamps []checksumRow
	for _, p := range src.chain {
		body, err := read(p.Key)
		if err != nil {
			found(fmt.Sprintf("archive: %s: %v", p.Key, err))
			report.ArchiveUnverified = "a piece of its history could not be read"
			return nil
		}
		if changelogfile.SumText(sha256.Sum256(body)) != src.sums[p.Key] {
			found(fmt.Sprintf("archive: %s is not the piece its manifest selects", p.Key))
			report.ArchiveUnverified = "a piece of its history is not the one its manifest selects"
			return nil
		}
		err = changelogfile.ReadPiece(bytes.NewReader(body), p.First, p.Last, func(e changelogfile.Entry, _ []byte, sum [32]byte) error {
			for len(stamps) == 0 || stamps[0].entry.Seq < e.Seq {
				if len(stamps) > 0 {
					stamps = stamps[1:]
					continue
				}
				page, err := scanStampPage(ctx, tx, e.Seq-1, verifyTableBatch)
				if err != nil {
					return err
				}
				if len(page) == 0 {
					return fmt.Errorf("seq %d is in the archive and not in the table", e.Seq)
				}
				stamps = page
			}
			if stamps[0].entry.Seq != e.Seq {
				return fmt.Errorf("seq %d is in the archive and not in the table", e.Seq)
			}
			if !bytes.Equal(stamps[0].hash, sum[:]) {
				return fmt.Errorf("seq %d: the archive's line is not the table's", e.Seq)
			}
			return nil
		})
		if err != nil {
			found(fmt.Sprintf("archive: %s: %v", p.Key, err))
			report.ArchiveUnverified = "a line of its history is not the table's"
			return nil
		}
		prog.tick(changelogfile.Position{Seq: p.Last})
	}
	return nil
}

// storeHoldsNothingOf reports whether the store lists no object of the
// repository at all, under any archive.
func (s *service) storeHoldsNothingOf(ctx context.Context, id string) bool {
	everything, err := s.archive.List(ctx, changelogfile.RepositoryPrefix(id))
	return err == nil && len(everything) == 0
}

// verifyArchivedBlobs reads every blob the archive holds back out of it and
// hashes it against the digest that names it, and, when the archive is at the
// table's head, holds it to every stored blob the records there hold: each
// under some object of its digest, or named missing or pending by the
// manifest. An archive behind the table is held only to what it holds,
// because the records at its head are not the table's now. An object that
// went while it was read is a drop a pass made, and is read past.
func (s *service) verifyArchivedBlobs(ctx context.Context, tx dbx, src *archiveSource, report *VerifyReport, found func(string)) error {
	prefix := changelogfile.ArchivePrefix(src.repository, src.archive)
	listed, err := s.archive.List(ctx, prefix+changelogfile.BlobsSubdir+"/")
	if err != nil {
		found(fmt.Sprintf("archive: not verified: the blobs cannot be listed: %v", err))
		report.ArchiveUnverified = "its blobs could not be listed"
		return nil
	}
	held := make(map[string]bool, len(listed))
	for _, o := range listed {
		digest, _, ok := changelogfile.ParseBlobObjectName(strings.TrimPrefix(o.Key, prefix))
		if !ok {
			continue
		}
		rc, err := s.archive.Get(ctx, o.Key)
		if errors.Is(err, archive.ErrNotFound) {
			continue
		}
		if err != nil {
			found(fmt.Sprintf("archive: blob %s: %v", o.Key, err))
			continue
		}
		h := sha256.New()
		_, err = io.Copy(h, rc)
		_ = rc.Close()
		switch {
		case err != nil:
			found(fmt.Sprintf("archive: blob %s does not read: %v", o.Key, err))
		case substrate.BlobDigestPrefix+hex.EncodeToString(h.Sum(nil)) != digest:
			found(fmt.Sprintf("archive: blob %s holds other bytes than its digest names", o.Key))
		default:
			if !held[digest] {
				report.ArchiveBlobs++
			}
			held[digest] = true
		}
	}
	if src.Head() != report.Head {
		return nil
	}
	stored, err := storedBlobs(ctx, tx)
	if err != nil {
		return err
	}
	named := make(map[string]bool, len(src.manifest.MissingBlobs)+len(src.manifest.PendingBlobs))
	for _, digest := range append(slices.Clone(src.manifest.MissingBlobs), src.manifest.PendingBlobs...) {
		named[digest] = true
	}
	for _, b := range stored {
		if !held[b.digest] && !named[b.digest] {
			found(fmt.Sprintf("archive: blob %s the records hold is not in it, and its manifest does not name it missing or pending", b.digest))
		}
	}
	return nil
}

// countUnaccounted counts what the store holds under the archive the record
// names that the record does not: no row, and not the published manifest.
// The count is set only once every listing answered whole; a listing that
// fails is an error, never a count of zero.
func (s *service) countUnaccounted(ctx context.Context, tx dbx, src *archiveSource, report *VerifyReport) error {
	rows, err := readArchiveObjects(ctx, tx, src.archive)
	if err != nil {
		return err
	}
	n := 0
	for _, prefix := range []string{changelogfile.ArchivePrefix(src.repository, src.archive), changelogfile.ArchiveManifestsPrefix(src.repository, src.archive)} {
		listed, err := s.archive.List(ctx, prefix)
		if err != nil {
			return fmt.Errorf("%w: %w", errArchiveUnreadable, err)
		}
		for _, o := range listed {
			name := strings.TrimPrefix(o.Key, prefix)
			if _, ok := rows[name]; !ok && name != src.name && name != changelogfile.DeletedMark {
				n++
			}
		}
	}
	report.ArchiveUnaccounted = n
	return nil
}

// storedBlob is one `stored` blob manifest: the digest that is its id and
// the size its properties claim, -1 when they claim none.
type storedBlob struct {
	digest string
	size   int64
}

// storedBlobs lists the live `stored` blob manifests, by digest. Only that
// status promises bytes (0030): a `pending` manifest is an upload that did
// not finish, and the sweep's to collect.
func storedBlobs(ctx context.Context, q dbx) ([]storedBlob, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id, COALESCE((props ->> $1)::bigint, -1) FROM records
		WHERE kind = $2 AND deleted_at IS NULL AND states ->> $3 = $4 ORDER BY id`,
		blobPropSize, kindBlob, blobStateStatus, string(substrate.BlobStored))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []storedBlob
	for rows.Next() {
		var b storedBlob
		if err := rows.Scan(&b.digest, &b.size); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// verifyBlobs reads every `stored` blob's bytes out of the store and hashes
// them against the digest that names them. A store that cannot be reached at
// all is one finding and ends the walk, not one finding per blob.
func (s *service) verifyBlobs(ctx context.Context, tx dbx, repo Repository, report *VerifyReport, found func(string)) error {
	blobs, err := storedBlobs(ctx, tx)
	if err != nil {
		return err
	}
	if len(blobs) == 0 {
		return nil
	}
	store, err := s.blobs.Repository(repo.ID)
	if err != nil {
		return err
	}
	for _, b := range blobs {
		report.Blobs++
		n, digest, err := hashBlob(ctx, store, b.digest)
		switch {
		case errors.Is(err, blobbytes.ErrNotStored):
			found(fmt.Sprintf("blob %s: the manifest is stored and the blob store holds no bytes", b.digest))
			continue
		case err != nil:
			found(fmt.Sprintf("blob %s: reading the blob store: %v", b.digest, err))
			return nil
		}
		report.BlobBytes += n
		if digest != b.digest {
			found(fmt.Sprintf("blob %s: the stored bytes hash to %s", b.digest, digest))
			continue
		}
		if b.size >= 0 && n != b.size {
			found(fmt.Sprintf("blob %s: %d bytes stored, the manifest says %d", b.digest, n, b.size))
		}
	}
	return nil
}

// hashBlob streams one blob out of the store and returns its length and the
// digest of what it read.
func hashBlob(ctx context.Context, store blobbytes.Store, digest string) (int64, string, error) {
	rc, err := store.Open(ctx, digest)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = rc.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, rc)
	if err != nil {
		return 0, "", err
	}
	return n, substrate.BlobDigestPrefix + hex.EncodeToString(h.Sum(nil)), nil
}

// verifySecretRefs walks every secret-typed property of every live record
// and holds each reference it finds to the sealed rows. The kinds come from
// the repository's own declaration rows, as a replay loads them, so a
// property the repository declares secret is checked whatever package it is
// in. A value that is not an engine-minted ref (`secret:`, `auth:`) is a
// plaintext, which references nothing. References only in historical payloads
// are not walked: rotation deletes their rows and files on purpose.
func (s *service) verifySecretRefs(ctx context.Context, tx dbx, db *sql.DB, repo Repository, sealed map[string]changelogfile.SealedRecord, report *VerifyReport, found func(string)) error {
	bare := s.bareDataset(repo, db)
	// Through the verify's own transaction: a read on the pool while it
	// holds a connection needs a second one, and the declarations belong to
	// the same snapshot the rows are checked in.
	if err := bare.loadDeclarationsForReplay(ctx, tx); err != nil {
		return err
	}
	for _, ty := range bare.registry().Kinds() {
		for _, name := range ty.PropOrder {
			p := ty.Props[name]
			if p.Datatype != vocabulary.DatatypeSecret {
				continue
			}
			q := `SELECT id, props ->> $1 FROM records
			       WHERE kind = $2 AND deleted_at IS NULL AND jsonb_typeof(props -> $1) = 'string' ORDER BY id`
			if p.Repeated {
				q = `SELECT id, jsonb_array_elements_text(props -> $1) FROM records
				     WHERE kind = $2 AND deleted_at IS NULL AND jsonb_typeof(props -> $1) = 'array' ORDER BY id`
			}
			rows, err := tx.QueryContext(ctx, q, name, ty.Identity)
			if err != nil {
				return err
			}
			for rows.Next() {
				var id, value string
				if err := rows.Scan(&id, &value); err != nil {
					_ = rows.Close()
					return err
				}
				if !strings.HasPrefix(value, secretRefPrefix) && !strings.HasPrefix(value, sealedAuthPrefix) {
					continue
				}
				report.SecretRefs++
				if _, ok := sealed[value]; !ok {
					found(fmt.Sprintf("secret %s: %s %s names it in %s and the sealed table has no row for it", value, ty.Identity, id, name))
				}
			}
			if err := rows.Err(); err != nil {
				_ = rows.Close()
				return err
			}
			_ = rows.Close()
		}
	}
	return nil
}

// verifyUnheldSealed names every sealed row that no live or tombstoned
// record holds (credentials.go sealedUnheld): the material nothing reads,
// which a purge before #236 left and the writable open and the GC sweep
// erase.
func verifyUnheldSealed(ctx context.Context, tx dbx, byRef map[string]changelogfile.SealedRecord, report *VerifyReport, found func(string)) error {
	if len(byRef) == 0 {
		return nil
	}
	type candidate struct {
		Ref        string `json:"ref"`
		RecordKind string `json:"record_kind"`
		RecordID   string `json:"record_id"`
	}
	candidates := make([]candidate, 0, len(byRef))
	for _, ref := range sortedKeys(byRef) {
		candidates = append(candidates, candidate{Ref: ref, RecordKind: byRef[ref].RecordKind, RecordID: byRef[ref].RecordID})
	}
	raw, err := json.Marshal(candidates)
	if err != nil {
		return err
	}
	unheld, err := tx.QueryContext(ctx, `
		SELECT s.ref, s.record_kind, s.record_id
		FROM jsonb_to_recordset($1::jsonb) AS s(ref text, record_kind text, record_id text)
		WHERE `+sealedUnheld+` ORDER BY s.ref`, raw)
	if err != nil {
		return err
	}
	defer func() { _ = unheld.Close() }()
	for unheld.Next() {
		var ref, kind, id string
		if err := unheld.Scan(&ref, &kind, &id); err != nil {
			return err
		}
		report.SealedOrphans++
		found(fmt.Sprintf("sealed %s (%s %s): an orphan: no live or tombstoned record holds the ref, so nothing reads the material",
			ref, kind, id))
	}
	return unheld.Err()
}

// verifySealedOpen opens every sealed row under the repository's DEK, the
// way a read of the record would, and names each that does not open. It runs
// only under a credential key, since the DEK is wrapped under it; a
// repository with no DEK yet (never opened by a keyed server) has nothing to
// open under.
func (s *service) verifySealedOpen(repo Repository, rows map[string]changelogfile.SealedRecord, report *VerifyReport, found func(string)) {
	if len(s.credKey) == 0 || len(repo.DEK) == 0 || len(rows) == 0 {
		return
	}
	dek, err := s.unwrapDEK(repo.DEK, repo.ID, repo.DEKKeyID)
	if err != nil {
		found(fmt.Sprintf("sealed: the repository's DEK does not open, so no sealed file can: %v", err))
		return
	}
	for _, ref := range sortedKeys(rows) {
		f := rows[ref]
		if _, err := openRepoPayload(f.Payload, dek, sealedAAD(f.Ref, f.RecordKind, f.RecordID)); err != nil {
			found(fmt.Sprintf("sealed %s (%s %s): does not open under the DEK: %v", f.Ref, f.RecordKind, f.RecordID, err))
			continue
		}
		report.SealedOpened++
	}
}

// checksumRow is one stored entry with its stamped checksum.
type checksumRow struct {
	entry pendingEntry
	hash  []byte
}

// scanChecksumPage reads one page of entries with their stored checksum, over
// any pool: every column the checksum covers, with the payload as stored
// text.
func scanChecksumPage(ctx context.Context, db dbx, after int64, limit int) ([]checksumRow, error) {
	return queryChecksumRows(ctx, db, `WHERE seq > $1 ORDER BY seq LIMIT $2`, after, limit)
}

// queryChecksumRows reads the changelog rows tail selects, each as its line.
func queryChecksumRows(ctx context.Context, db dbx, tail string, args ...any) ([]checksumRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT seq, ts, actor, on_behalf_of, principal, op, record_id, kind, payload::text, caused_by, txn, hash FROM changelog `+tail, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []checksumRow
	for rows.Next() {
		var r checksumRow
		var ts time.Time
		var causedBy, txn sql.NullInt64
		if err := rows.Scan(&r.entry.Seq, &ts, &r.entry.Actor, &r.entry.OnBehalfOf, &r.entry.Token, &r.entry.Op, &r.entry.RecordID,
			&r.entry.Kind, &r.entry.PayloadText, &causedBy, &txn, &r.hash); err != nil {
			return nil, err
		}
		r.entry.TS = ts.UTC()
		r.entry.CausedBy, r.entry.CausedByOK = causedBy.Int64, causedBy.Valid
		// NULL on a row v0.46.0 or v0.47.0 stamped: no boundary was recorded,
		// and the line is written without one (changelogfile.LineFormat).
		r.entry.Txn = txn.Int64
		out = append(out, r)
	}
	return out, rows.Err()
}

// scanStampPage is scanChecksumPage for a reader of the stamps alone: each
// row's seq, txn and stamped checksum, and no payload, which is most of what
// a row weighs.
func scanStampPage(ctx context.Context, db dbx, after int64, limit int) ([]checksumRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT seq, txn, hash FROM changelog WHERE seq > $1 ORDER BY seq LIMIT $2`, after, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []checksumRow
	for rows.Next() {
		var r checksumRow
		var txn sql.NullInt64
		if err := rows.Scan(&r.entry.Seq, &txn, &r.hash); err != nil {
			return nil, err
		}
		r.entry.Txn = txn.Int64
		out = append(out, r)
	}
	return out, rows.Err()
}
