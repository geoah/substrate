package engine

// The verification walk: the repository directory's changelog files, line by
// line and sidecar by sidecar (changelogfile.VerifyDir), the changelog table
// row by row, its stamped checksums held to the files' seq by seq (and, with
// VerifyOptions.Recanonicalize, recomputed from the stored columns), both
// heads, the sealed files against the sealed rows, every sealed row and file
// against the record that should hold its ref, the recorded recovery point
// against the files, every stored blob's bytes against its digest, every live
// secret reference against the sealed files and, under the credential key,
// every sealed file opened. It MUTATES NOTHING: the files
// are opened read-only and the table is read inside one repeatable-read
// transaction, so a concurrent write cannot make it stitch two states into
// one report.

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
	"os"
	"strings"
	"time"

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
	// FileHead, Segments, TruncatedBytes and TruncatedEntries are the
	// changelog files': the last seq of the last complete transaction, the
	// number of segment files, and the incomplete tail left on the active
	// segment (its bytes and the complete lines among them), which the next
	// Open cuts.
	FileHead         int64 `json:"fileHead"`
	Segments         int   `json:"segments"`
	TruncatedBytes   int64 `json:"truncatedBytes,omitempty"`
	TruncatedEntries int64 `json:"truncatedEntries,omitempty"`
	// SealedRows and SealedFiles count the sealed table and its mirror.
	SealedRows  int `json:"sealedRows"`
	SealedFiles int `json:"sealedFiles"`
	// SealedOpened is how many sealed files opened under the repository's
	// DEK: every one, or the walk found the rest; 0 with no credential key,
	// when nothing is opened.
	SealedOpened int `json:"sealedOpened"`
	// SealedOrphans is how many sealed refs, rows and files alike, no live or
	// tombstoned record holds (credentials.go sealedUnheld). Each is a
	// finding, and the GC sweep erases the rows among them.
	SealedOrphans int `json:"sealedOrphans"`
	// SecretRefs is how many secret references live records hold, each held
	// to a sealed file.
	SecretRefs int `json:"secretRefs"`
	// Blobs and BlobBytes are the `stored` blob manifests, each one's bytes
	// read from the store and hashed against its digest.
	Blobs     int   `json:"blobs"`
	BlobBytes int64 `json:"blobBytes"`
	// Snapshot is the recovery point `snapshot.json` records when the
	// directory is a snapshot or was restored from one; nil otherwise.
	Snapshot *RecoveryPoint `json:"snapshot,omitempty"`
	// Recanonicalized is whether every table row's checksum was recomputed
	// from its stored columns (VerifyOptions.Recanonicalize).
	Recanonicalized bool `json:"recanonicalized,omitempty"`
	// KnownHead is a snapshot's: the seq ending the run of finished segments
	// its base holds as this directory does. Those segments were read at
	// their first and last lines, and the table was compared with the files
	// at KnownHead and above it; Entries counts the rows above it.
	KnownHead int64         `json:"knownHead,omitempty"`
	Findings  []string      `json:"findings,omitempty"`
	Truncated bool          `json:"truncated,omitempty"`
	OK        bool          `json:"ok"`
	Took      time.Duration `json:"took"`
}

// VerifyOptions tunes VerifyRepositoryWith.
type VerifyOptions struct {
	// Recanonicalize recomputes every table row's checksum from its stored
	// columns, the payload canonicalized, and holds it to the row's stamped
	// hash. Without it the table pass reads no payload: it holds each row's
	// stamped hash to the sum its line carries, which the file pass verified
	// against the line's bytes. A row whose columns were edited in place with
	// its hash left alone is therefore found only with it. It reads and
	// canonicalizes every payload the table holds, which on a history of
	// millions of entries is hours (issue 761).
	Recanonicalize bool

	// known and knownBlobs are a snapshot's base (snapshot.go): the finished
	// segments it holds as this directory does, read at their first and
	// last lines alone, and the blobs it holds bytes for, not read at all.
	// The table is then compared with the files only above the run of known
	// segments, at the seq that ends it and past it. A verify of its own
	// sets neither.
	known      map[string]changelogfile.KnownSegment
	knownBlobs map[string]bool
}

// verifyTableBatch is the table pass's page when it reads no payload: a row
// is a seq, a txn and 32 bytes, so a page is a round trip that moves little.
const verifyTableBatch = 10000

// RecoveryPoint is the committed point a snapshot recorded: the head seq, its
// checksum in hex, and when the snapshot was taken.
type RecoveryPoint struct {
	Head     int64     `json:"head"`
	HeadHash string    `json:"headHash"`
	TakenAt  time.Time `json:"takenAt"`
}

// VerifyRepository walks one repository's changelog files and table. Findings
// land in the report, not in the error: the error is for "could not verify"
// (no such user, no connection), never for "verified and found damage".
func (s *service) VerifyRepository(ctx context.Context, repository string) (VerifyReport, error) {
	return s.VerifyRepositoryWith(ctx, repository, VerifyOptions{})
}

// VerifyRepositoryWith is VerifyRepository with its options.
func (s *service) VerifyRepositoryWith(ctx context.Context, repository string, opts VerifyOptions) (VerifyReport, error) {
	report, _, err := s.verifyRepository(ctx, repository, opts)
	return report, err
}

// verifyRepository is the verification, returning beside the report the
// changelog files' Log it verified: nil when the files did not verify. A
// snapshot holds its copy to that Log (snapshot.go).
func (s *service) verifyRepository(ctx context.Context, repository string, opts VerifyOptions) (VerifyReport, *changelogfile.Log, error) {
	started := time.Now()
	repo, err := s.repositoryByID(ctx, repository)
	if err != nil {
		return VerifyReport{}, nil, err
	}
	report := VerifyReport{Repository: repo.ID, Recanonicalized: opts.Recanonicalize}
	found := func(f string) {
		if len(report.Findings) >= verifyFindingCap {
			report.Truncated = true
			return
		}
		report.Findings = append(report.Findings, f)
	}
	dir, err := changelogfile.RepoDir(s.dataRoot, repo.ID)
	if err != nil {
		return report, nil, err
	}
	// A bare scoped pool: the RLS-bound shape every request rides, with none
	// of the open ladder's writes.
	db, err := s.scopedDB(repo.scope())
	if err != nil {
		return VerifyReport{}, nil, err
	}
	defer func() { _ = db.Close() }()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return VerifyReport{}, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// The import-progress marker (repodir.go) first: a repository whose
	// import began and did not complete has files and rows that agree, and a
	// fold that is not theirs, so a report with no finding would be a lie an
	// operator acts on.
	markedHead, incomplete, err := importIncomplete(ctx, tx)
	if err != nil {
		return report, nil, err
	}
	if incomplete {
		found(fmt.Sprintf("import: the import of the repository directory has not completed (marked at file head %d): the fold is not the changelog's until the server's next boot resumes it", markedHead))
	}

	// The files first, whole, each segment read once: its digest against
	// its sidecar and every line's sum, the seq sequence and the transaction
	// frame, in parallel across segments. The Log it opens is what the table
	// pass below reads again rather than opening the directory a second time.
	// A directory that does not verify is one finding, and the table is
	// still walked so the report says what the table holds.
	prog := s.progress("substrate: verifying the changelog files", "repository", repo.ID)
	log, fileReport, fileErr := changelogfile.VerifyDir(changelogfile.ChangelogDir(dir), changelogfile.VerifyOptions{Progress: prog.tick, Known: opts.known})
	report.FileHead, report.Segments = fileReport.Head, fileReport.Segments
	report.TruncatedBytes, report.TruncatedEntries = fileReport.TruncatedBytes, fileReport.TruncatedEntries
	if fileErr != nil {
		found(fmt.Sprintf("file: %v", fileErr))
		log = nil
	}
	// A finding, not a refusal: beside a live server the tail can be a
	// transaction the writer is still writing, and verify repairs nothing.
	if report.TruncatedBytes > 0 {
		found(fmt.Sprintf("file: the active segment ends in an incomplete transaction: %d bytes past the last complete one, %d complete line(s) among them",
			report.TruncatedBytes, report.TruncatedEntries))
	}

	// The table, row by row, its stamped checksum held to the sum the file's
	// line carries where the file has the seq, and recomputed from the
	// stored columns under Recanonicalize; and the transaction frame, so a
	// `txn` the boot's writer would refuse is named here first. The file is
	// read through one cursor across every page, which reads each line's sum
	// without decoding it: the file pass above checked every line against
	// its sum already.
	//
	// Under a snapshot's base the rows at or below the run of known segments
	// are not read: the base was compared with the table when it was taken,
	// and the row at the seq that ends the run is held to that line's sum,
	// so the rows above it continue the history the base holds.
	expected := int64(1)
	var knownSum [32]byte
	if log != nil {
		report.KnownHead, knownSum = log.KnownHead()
	}
	if report.KnownHead > 0 {
		var hash []byte
		err := tx.QueryRowContext(ctx, `SELECT hash FROM changelog WHERE seq = $1`, report.KnownHead).Scan(&hash)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			found(fmt.Sprintf("seq %d: in the file and not in the table", report.KnownHead))
		case err != nil:
			return report, nil, err
		case !bytes.Equal(hash, knownSum[:]):
			found(fmt.Sprintf("seq %d: the file's checksum is not the table's", report.KnownHead))
		}
		expected = report.KnownHead + 1
		report.Head = report.KnownHead
		report.HeadHash = hex.EncodeToString(knownSum[:])
	}
	var sums *changelogfile.SumCursor
	if log != nil {
		c := log.Sums(expected - 1)
		defer func() { _ = c.Close() }()
		sums = c
	}
	fileSeq := expected - 1
	var fileSum [32]byte
	prog = s.progress("substrate: verifying the changelog table against the files", "repository", repo.ID)
	var openTxn int64
	for {
		var page []checksumRow
		if opts.Recanonicalize {
			page, err = scanChecksumPage(ctx, tx, expected-1, rebuildBatch)
		} else {
			page, err = scanStampPage(ctx, tx, expected-1, verifyTableBatch)
		}
		if err != nil {
			return report, nil, err
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
				_, want, err := changelogfile.Encode(row.entry.fileEntry())
				if err != nil {
					found(fmt.Sprintf("seq %d: payload does not canonicalize: %v", row.entry.Seq, err))
					continue
				}
				if want != [32]byte(row.hash) {
					found(fmt.Sprintf("seq %d: checksum mismatch, the stored row is not what was stamped", row.entry.Seq))
				}
			}
			if log == nil || row.entry.Seq > log.Head() {
				continue
			}
			// The cursor reads forward past any seq the table skips.
			for sums != nil && fileSeq < row.entry.Seq {
				if fileSeq, fileSum, err = sums.Next(); err != nil {
					found(fmt.Sprintf("file: reading seq %d: %v", row.entry.Seq, err))
					log, sums = nil, nil
				}
			}
			switch {
			case sums == nil:
			case fileSeq != row.entry.Seq:
				found(fmt.Sprintf("seq %d: in the table and not in the file", row.entry.Seq))
			case !bytes.Equal(fileSum[:], row.hash):
				found(fmt.Sprintf("seq %d: the file's checksum is not the table's", row.entry.Seq))
			}
		}
		pos := changelogfile.Position{Seq: report.Head}
		if sums != nil {
			pos = sums.Position()
		}
		prog.tick(pos)
	}
	if openTxn != 0 {
		found(fmt.Sprintf("the table ends inside the transaction ending at seq %d", openTxn))
	}
	if log != nil && report.FileHead != report.Head {
		found(fmt.Sprintf("the table's head is %d and the file's is %d", report.Head, report.FileHead))
	}

	// The sealed mirror against the sealed table, by ref.
	rows, err := readSealedTable(ctx, tx)
	if err != nil {
		return report, nil, err
	}
	files, err := changelogfile.ReadSealed(dir)
	if err != nil {
		found(fmt.Sprintf("sealed: %v", err))
	}
	report.SealedRows, report.SealedFiles = len(rows), len(files)
	seen := map[string]bool{}
	for _, rec := range files {
		seen[rec.Ref] = true
		row, ok := rows[rec.Ref]
		switch {
		case !ok:
			found(fmt.Sprintf("sealed %s: a file with no row", rec.Ref))
		case !sealedRecordsEqual(rec, row):
			found(fmt.Sprintf("sealed %s: the file is not the row", rec.Ref))
		}
	}
	for _, ref := range sortedKeys(rows) {
		if !seen[ref] {
			found(fmt.Sprintf("sealed %s: a row with no file", ref))
		}
	}
	// A pending file is a staged write that had not committed when the
	// directory was read: beside a live server, one in flight; in a copy,
	// one the import ignores and the boot removes.
	pending, err := changelogfile.PendingSealed(dir)
	if err != nil {
		found(fmt.Sprintf("sealed: %v", err))
	}
	for _, name := range pending {
		found(fmt.Sprintf("sealed/%s: a staged write that has not committed", name))
	}
	if err := verifyUnheldSealed(ctx, tx, rows, files, &report, found); err != nil {
		return report, nil, err
	}

	// The recorded recovery point, when the directory is a snapshot or was
	// restored from one: the entry it names must be in the files, as recorded.
	verifySnapshotPoint(dir, log, &report, found)

	// The side stores against the fold: a `stored` manifest whose bytes are
	// missing or not its digest's, and a live secret reference with no
	// sealed file, are what a copy that missed a file looks like after an
	// import, which upserts whatever files it finds.
	if err := s.verifyBlobs(ctx, tx, db, repo, opts, &report, found); err != nil {
		return report, nil, err
	}
	if err := s.verifySecretRefs(ctx, tx, db, repo, dir, seen, &report, found); err != nil {
		return report, nil, err
	}
	// Under the credential key, every sealed file opened: the table and the
	// file agreeing byte for byte says nothing about whether the bytes are
	// ciphertext the DEK opens, and an import loads what it finds.
	s.verifySealedOpen(repo, files, &report, found)

	report.OK = len(report.Findings) == 0
	report.Took = time.Since(started)
	return report, log, nil
}

// verifySnapshotPoint holds the files to the recovery point `snapshot.json`
// recorded, when there is one: the entry at the recorded head must be in the
// files and carry the recorded checksum. A head past the point is not a
// finding, because a restored repository keeps writing.
func verifySnapshotPoint(dir string, log *changelogfile.Log, report *VerifyReport, found func(string)) {
	snap, err := changelogfile.ReadSnapshot(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return
	case err != nil:
		found(fmt.Sprintf("snapshot: %v", err))
		return
	}
	report.Snapshot = &RecoveryPoint{Head: snap.Head, HeadHash: hex.EncodeToString(snap.HeadHash[:]), TakenAt: snap.TakenAt}
	if log == nil || snap.Head == 0 {
		return
	}
	if snap.Head > log.Head() {
		found(fmt.Sprintf("snapshot: records head %d and the files end at %d: the copy is short of the point it claims", snap.Head, log.Head()))
		return
	}
	entries, err := log.Read(snap.Head-1, 1)
	if err != nil || len(entries) != 1 || entries[0].Seq != snap.Head {
		found(fmt.Sprintf("snapshot: reading the recorded head %d: %v", snap.Head, err))
		return
	}
	if _, sum, err := changelogfile.Encode(entries[0]); err != nil || sum != snap.HeadHash {
		found(fmt.Sprintf("snapshot: the entry at seq %d is not the one recorded: the files are another history than the snapshot's", snap.Head))
	}
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
func (s *service) verifyBlobs(ctx context.Context, tx dbx, db *sql.DB, repo Repository, opts VerifyOptions, report *VerifyReport, found func(string)) error {
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
		if opts.knownBlobs[b.digest] {
			continue
		}
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
// and holds each reference it finds to the sealed files. The kinds come from
// the repository's own declaration rows, as a replay loads them, so a
// property the repository declares secret is checked whatever package it is
// in. A value that is not an engine-minted ref (`secret:`, `auth:`) is a
// plaintext, which references nothing. References only in historical payloads
// are not walked: rotation deletes their rows and files on purpose.
func (s *service) verifySecretRefs(ctx context.Context, tx dbx, db *sql.DB, repo Repository, dir string, fileRefs map[string]bool, report *VerifyReport, found func(string)) error {
	bare := s.bareDataset(repo, db, dir)
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
				if !fileRefs[value] {
					found(fmt.Sprintf("secret %s: %s %s names it in %s and sealed/ has no file for it", value, ty.Identity, id, name))
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

// verifyUnheldSealed names every sealed ref, in the table or under sealed/,
// that no live or tombstoned record holds (credentials.go sealedUnheld): the
// material nothing reads, which a purge before #236 left and the GC sweep
// erases. A ref with a row is judged by the row's owner and a file with no
// row by the owner its file names, so a file that is not its row is not
// judged twice.
func verifyUnheldSealed(ctx context.Context, tx dbx, rows map[string]changelogfile.SealedRecord, files []changelogfile.SealedRecord, report *VerifyReport, found func(string)) error {
	byRef := make(map[string]changelogfile.SealedRecord, len(rows)+len(files))
	for _, f := range files {
		byRef[f.Ref] = f
	}
	for ref, r := range rows {
		byRef[ref] = r
	}
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

// verifySealedOpen opens every sealed file under the repository's DEK, the
// way a read of the record would, and names each that does not open. It runs
// only under a credential key, since the DEK is wrapped under it; a
// repository with no DEK yet (never opened by a keyed server) has nothing to
// open under.
func (s *service) verifySealedOpen(repo Repository, files []changelogfile.SealedRecord, report *VerifyReport, found func(string)) {
	if len(s.credKey) == 0 || len(repo.DEK) == 0 || len(files) == 0 {
		return
	}
	dek, err := s.unwrapDEK(repo.DEK, repo.ID, repo.DEKKeyID)
	if err != nil {
		found(fmt.Sprintf("sealed: the repository's DEK does not open, so no sealed file can: %v", err))
		return
	}
	for _, f := range files {
		if _, err := openRepoPayload(f.Payload, dek, sealedAAD(f.Ref, f.RecordKind, f.RecordID)); err != nil {
			found(fmt.Sprintf("sealed/%s (%s %s): does not open under the DEK: %v",
				changelogfile.SealedFileName(f.Ref), f.RecordKind, f.RecordID, err))
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
	rows, err := db.QueryContext(ctx, `
		SELECT seq, ts, actor, principal, op, record_id, kind, payload::text, caused_by, txn, hash FROM changelog
		WHERE seq > $1 ORDER BY seq LIMIT $2`, after, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []checksumRow
	for rows.Next() {
		var r checksumRow
		var ts time.Time
		var causedBy, txn sql.NullInt64
		if err := rows.Scan(&r.entry.Seq, &ts, &r.entry.Actor, &r.entry.Principal, &r.entry.Op, &r.entry.RecordID,
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
