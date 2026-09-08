package engine

// The verification walk: the repository directory's changelog files, line by
// line and sidecar by sidecar (changelogfile.Verify), the changelog table row
// by row with every checksum recomputed from the stored columns, the two
// held to each other seq by seq, both heads, and the sealed files against the
// sealed rows. It MUTATES NOTHING: the files are opened read-only and the
// table is read inside one repeatable-read transaction, so a concurrent write
// cannot make it stitch two states into one report.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
)

// verifyFindingCap bounds the report: after this many findings the walk
// stops, because a corrupted store names every row and the first page is the
// story.
const verifyFindingCap = 20

// VerifyReport is what one verification saw.
type VerifyReport struct {
	Repository string `json:"repository"`
	Username   string `json:"username"`
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
	SealedRows  int           `json:"sealedRows"`
	SealedFiles int           `json:"sealedFiles"`
	Findings    []string      `json:"findings,omitempty"`
	Truncated   bool          `json:"truncated,omitempty"`
	OK          bool          `json:"ok"`
	Took        time.Duration `json:"took"`
}

// Verifier is the operator hat's verification seam, off substrate.Service
// like Resetter (auth.go) and asserted here for the same reason.
type Verifier interface {
	VerifyRepository(ctx context.Context, username string) (VerifyReport, error)
}

var _ Verifier = (*service)(nil)

// VerifyRepository walks one repository's changelog files and table. Findings
// land in the report, not in the error: the error is for "could not verify"
// (no such user, no connection), never for "verified and found damage".
func (s *service) VerifyRepository(ctx context.Context, username string) (VerifyReport, error) {
	started := time.Now()
	repo, err := s.repositoryByUsername(ctx, username)
	if err != nil {
		return VerifyReport{}, err
	}
	report := VerifyReport{Repository: repo.ID, Username: repo.Username}
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
			case txn != 0 && txn < row.entry.Seq:
				found(fmt.Sprintf("seq %d ends its transaction at %d, before itself", row.entry.Seq, txn))
				openTxn = 0
			case openTxn != 0 && txn != openTxn:
				found(fmt.Sprintf("seq %d carries txn %d inside the transaction ending at %d", row.entry.Seq, txn, openTxn))
				openTxn = 0
			case txn == 0 || txn == row.entry.Seq:
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
	report.OK = len(report.Findings) == 0
	report.Took = time.Since(started)
	return report, nil
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
