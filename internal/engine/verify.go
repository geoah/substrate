package engine

// The verification walk: the repository directory's changelog files, line by
// line and sidecar by sidecar (changelogfile.Verify), the changelog table row
// by row with every checksum recomputed from the stored columns, the two
// held to each other seq by seq, both heads, the sealed files against the
// sealed rows, the recorded recovery point against the files, every stored
// blob's bytes against its digest, every live secret reference against the
// sealed files and, under the credential key, every sealed file opened. It
// MUTATES NOTHING: the files are opened read-only and the table is read
// inside one repeatable-read transaction, so a concurrent write cannot make
// it stitch two states into one report.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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
	// SecretRefs is how many secret references live records hold, each held
	// to a sealed file.
	SecretRefs int `json:"secretRefs"`
	// Blobs and BlobBytes are the `stored` blob manifests, each one's bytes
	// read from the store and hashed against its digest.
	Blobs     int   `json:"blobs"`
	BlobBytes int64 `json:"blobBytes"`
	// Snapshot is the recovery point `snapshot.json` records when the
	// directory is a snapshot or was restored from one; nil otherwise.
	Snapshot  *RecoveryPoint `json:"snapshot,omitempty"`
	Findings  []string       `json:"findings,omitempty"`
	Truncated bool           `json:"truncated,omitempty"`
	OK        bool           `json:"ok"`
	Took      time.Duration  `json:"took"`
}

// RecoveryPoint is the committed point a snapshot recorded: the head seq, its
// checksum in hex, and when the snapshot was taken.
type RecoveryPoint struct {
	Head     int64     `json:"head"`
	HeadHash string    `json:"headHash"`
	TakenAt  time.Time `json:"takenAt"`
}

// Verifier is the operator hat's verification seam, off substrate.Service
// like Resetter (auth.go) and asserted here for the same reason.
type Verifier interface {
	VerifyRepository(ctx context.Context, repository string) (VerifyReport, error)
}

var _ Verifier = (*service)(nil)

// VerifyRepository walks one repository's changelog files and table. Findings
// land in the report, not in the error: the error is for "could not verify"
// (no such user, no connection), never for "verified and found damage".
func (s *service) VerifyRepository(ctx context.Context, repository string) (VerifyReport, error) {
	started := time.Now()
	repo, err := s.repositoryByID(ctx, repository)
	if err != nil {
		return VerifyReport{}, err
	}
	report := VerifyReport{Repository: repo.ID}
	found := func(f string) {
		if len(report.Findings) >= verifyFindingCap {
			report.Truncated = true
			return
		}
		report.Findings = append(report.Findings, f)
	}
	dir, err := changelogfile.RepoDir(s.dataRoot, repo.ID)
	if err != nil {
		return report, err
	}
	// A bare scoped pool: the RLS-bound shape every request rides, with none
	// of the open ladder's writes.
	db, err := openScoped(s.dsn, repo.scope(), s.appRole)
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
	// import began and did not complete has files and rows that agree, and a
	// fold that is not theirs, so a report with no finding would be a lie an
	// operator acts on.
	markedHead, incomplete, err := importIncomplete(ctx, tx)
	if err != nil {
		return report, err
	}
	if incomplete {
		found(fmt.Sprintf("import: the import of the repository directory has not completed (marked at file head %d): the fold is not the changelog's until the server's next boot resumes it", markedHead))
	}

	// The files first, whole: every line's sum, every sidecar, the seq
	// sequence. A directory that does not open is one finding, and the table
	// is still walked so the report says what the table holds.
	fileReport, fileErr := changelogfile.Verify(changelogfile.ChangelogDir(dir))
	report.FileHead, report.Segments = fileReport.Head, fileReport.Segments
	report.TruncatedBytes, report.TruncatedEntries = fileReport.TruncatedBytes, fileReport.TruncatedEntries
	if fileErr != nil {
		found(fmt.Sprintf("file: %v", fileErr))
	}
	// A finding, not a refusal: beside a live server the tail can be a
	// transaction the writer is still writing, and verify repairs nothing.
	if report.TruncatedBytes > 0 {
		found(fmt.Sprintf("file: the active segment ends in an incomplete transaction: %d bytes past the last complete one, %d complete line(s) among them",
			report.TruncatedBytes, report.TruncatedEntries))
	}
	var log *changelogfile.Log
	if fileErr == nil {
		if log, err = changelogfile.OpenReadOnly(changelogfile.ChangelogDir(dir)); err != nil {
			found(fmt.Sprintf("file: %v", err))
			log = nil
		}
	}

	// The table, row by row, each checksum recomputed and, where the file has
	// the seq, compared with the line's; and the transaction frame, so a
	// `txn` the boot's writer would refuse is named here first.
	expected := int64(1)
	var openTxn int64
	for {
		page, err := scanChecksumPage(ctx, tx, expected-1, rebuildBatch)
		if err != nil {
			return report, err
		}
		if len(page) == 0 {
			break
		}
		fileSums := map[int64][32]byte{}
		if log != nil {
			first, last := page[0].entry.Seq, page[len(page)-1].entry.Seq
			entries, err := log.Read(first-1, int(last-first+1))
			if err != nil {
				found(fmt.Sprintf("file: reading seq %d..%d: %v", first, last, err))
				log = nil
			}
			for _, e := range entries {
				if _, sum, err := changelogfile.Encode(e); err == nil {
					fileSums[e.Seq] = sum
				}
			}
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
			_, want, err := changelogfile.Encode(row.entry.fileEntry())
			if err != nil {
				found(fmt.Sprintf("seq %d: payload does not canonicalize: %v", row.entry.Seq, err))
				continue
			}
			if want != [32]byte(row.hash) {
				found(fmt.Sprintf("seq %d: checksum mismatch, the stored row is not what was stamped", row.entry.Seq))
			}
			if log == nil || row.entry.Seq > log.Head() {
				continue
			}
			fileSum, ok := fileSums[row.entry.Seq]
			switch {
			case !ok:
				found(fmt.Sprintf("seq %d: in the table and not in the file", row.entry.Seq))
			case !bytes.Equal(fileSum[:], row.hash):
				found(fmt.Sprintf("seq %d: the file's checksum is not the table's", row.entry.Seq))
			}
		}
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
		return report, err
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

	// The recorded recovery point, when the directory is a snapshot or was
	// restored from one: the entry it names must be in the files, as recorded.
	verifySnapshotPoint(dir, log, &report, found)

	// The side stores against the fold: a `stored` manifest whose bytes are
	// missing or not its digest's, and a live secret reference with no
	// sealed file, are what a copy that missed a file looks like after an
	// import, which upserts whatever files it finds.
	if err := s.verifyBlobs(ctx, tx, db, repo, &report, found); err != nil {
		return report, err
	}
	if err := s.verifySecretRefs(ctx, tx, db, repo, dir, seen, &report, found); err != nil {
		return report, err
	}
	// Under the credential key, every sealed file opened: the table and the
	// file agreeing byte for byte says nothing about whether the bytes are
	// ciphertext the DEK opens, and an import loads what it finds.
	s.verifySealedOpen(repo, files, &report, found)

	report.OK = len(report.Findings) == 0
	report.Took = time.Since(started)
	return report, nil
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
func (s *service) verifyBlobs(ctx context.Context, tx dbx, db *sql.DB, repo Repository, report *VerifyReport, found func(string)) error {
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
			found(fmt.Sprintf("blob %s: the manifest is stored and the %s store holds no bytes", b.digest, store.Backend()))
			continue
		case err != nil:
			found(fmt.Sprintf("blob %s: reading the %s store: %v", b.digest, store.Backend(), err))
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
	if err := bare.loadDeclarationsForReplay(ctx); err != nil {
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
