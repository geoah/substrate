package engine

// The table check: walk one repository's changelog start to head, hold the
// seqs gapless from 1, recompute every checksum from the stored row and
// compare it with the stamped `hash`. It MUTATES NOTHING and reads inside one
// read-only repeatable-read transaction, so a concurrent write cannot make it
// stitch two states into one report. Phase 2 of
// docs/plans/filesystem-changelog.md replaces this with the segment-file
// walk: every line's `sum`, every sidecar, both heads.

import (
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
	Entries    int64  `json:"entries"`
	Head       int64  `json:"head"`
	// HeadHash is the head entry's checksum, hex.
	HeadHash  string        `json:"headHash,omitempty"`
	Findings  []string      `json:"findings,omitempty"`
	Truncated bool          `json:"truncated,omitempty"`
	OK        bool          `json:"ok"`
	Took      time.Duration `json:"took"`
}

// Verifier is the operator hat's verification seam, off substrate.Service
// like Resetter (auth.go) and asserted here for the same reason.
type Verifier interface {
	VerifyRepository(ctx context.Context, username string) (VerifyReport, error)
}

var _ Verifier = (*service)(nil)

// VerifyRepository walks one repository's whole changelog table. Findings
// land in the report, not in the error: the error is for "could not verify"
// (no such user, no connection), never for "verified and found damage".
func (s *service) VerifyRepository(ctx context.Context, username string) (VerifyReport, error) {
	started := time.Now()
	repo, err := s.repositoryByUsername(ctx, username)
	if err != nil {
		return VerifyReport{}, err
	}
	report := VerifyReport{Repository: repo.ID, Username: repo.Username}
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

	found := func(f string) {
		if len(report.Findings) >= verifyFindingCap {
			report.Truncated = true
			return
		}
		report.Findings = append(report.Findings, f)
	}
	expected := int64(1)
	for {
		page, err := scanChecksumPage(ctx, tx, expected-1, rebuildBatch)
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
		}
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
		SELECT seq, ts, actor, principal, op, record_id, kind, payload::text, caused_by, hash FROM changelog
		WHERE seq > $1 ORDER BY seq LIMIT $2`, after, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []checksumRow
	for rows.Next() {
		var r checksumRow
		var ts time.Time
		var causedBy sql.NullInt64
		if err := rows.Scan(&r.entry.Seq, &ts, &r.entry.Actor, &r.entry.Principal, &r.entry.Op, &r.entry.RecordID,
			&r.entry.Kind, &r.entry.PayloadText, &causedBy, &r.hash); err != nil {
			return nil, err
		}
		r.entry.TS = ts.UTC()
		r.entry.CausedBy, r.entry.CausedByOK = causedBy.Int64, causedBy.Valid
		out = append(out, r)
	}
	return out, rows.Err()
}
