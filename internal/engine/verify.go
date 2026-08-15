package engine

// The chain verifier: walk one repository's changelog start to head,
// recompute every hash from the stored bytes, check every signature the
// signing state requires, and report either the verified head or every
// finding by seq and name. READ-ONLY BY CONSTRUCTION: it opens a bare scoped
// pool rather than the dataset ladder, so it can never backfill, repair or
// otherwise touch what it is about to judge — a verifier that silently fixes
// the store certifies its own work, not the history.
//
// The findings are a closed taxonomy, not library errors: an operator acting
// on "hash mismatch at seq 41230" and one acting on "unexpected EOF" are in
// different professions.

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

// verifyFindingCap bounds the report: after this many findings the walk
// stops, because a corrupted store names every row and the first page is the
// story.
const verifyFindingCap = 20

// VerifyReport is what one verification saw.
type VerifyReport struct {
	Repository string        `json:"repository"`
	Username   string        `json:"username"`
	Entries    int64         `json:"entries"`
	Head       int64         `json:"head"`
	HeadHash   string        `json:"headHash,omitempty"`
	SignedFrom int64         `json:"signedFrom,omitempty"`
	PublicKey  string        `json:"publicKey,omitempty"`
	Epochs     []EpochInfo   `json:"epochs,omitempty"`
	Findings   []string      `json:"findings,omitempty"`
	Truncated  bool          `json:"truncated,omitempty"`
	OK         bool          `json:"ok"`
	Took       time.Duration `json:"took"`
}

// EpochInfo is one chain epoch as the report renders it.
type EpochInfo struct {
	At         time.Time `json:"at"`
	Reason     string    `json:"reason"`
	FromSeq    int64     `json:"fromSeq"`
	OldHead    string    `json:"oldHead,omitempty"`
	NewHead    string    `json:"newHead,omitempty"`
	PublicKey  string    `json:"publicKey,omitempty"`
	SignedFrom int64     `json:"signedFrom,omitempty"`
	Signed     bool      `json:"signed"`
	SigOK      *bool     `json:"sigOk,omitempty"`
}

// VerifyRepository walks one repository's whole chain. Findings land in the
// report, not in the error: the error is for "could not verify" (no such
// user, no connection), never for "verified and found tampering".
func (s *service) VerifyRepository(ctx context.Context, username string) (VerifyReport, error) {
	started := time.Now()
	repo, err := s.repositoryByUsername(ctx, username)
	if err != nil {
		return VerifyReport{}, err
	}
	signing, err := s.loadSigningState(ctx, repo.ID)
	if err != nil {
		return VerifyReport{}, err
	}
	report := VerifyReport{
		Repository: repo.ID, Username: repo.Username,
		SignedFrom: signing.signedFrom,
	}
	if len(signing.public) > 0 {
		report.PublicKey = hex.EncodeToString(signing.public)
	}
	// A bare scoped pool: the RLS-bound shape every request rides, with none
	// of the open ladder's writes.
	db, err := openScoped(s.dsn, repo.scope(), s.appRole)
	if err != nil {
		return VerifyReport{}, err
	}
	defer func() { _ = db.Close() }()

	found := func(f string) {
		if len(report.Findings) >= verifyFindingCap {
			report.Truncated = true
			return
		}
		report.Findings = append(report.Findings, f)
	}
	prev := zeroHash
	expected := int64(1)
	for !report.Truncated {
		page, err := scanChainPageWithMarks(ctx, db, expected-1, rebuildBatch)
		if err != nil {
			return report, err
		}
		if len(page) == 0 {
			break
		}
		for _, row := range page {
			if report.Truncated {
				break
			}
			if row.entry.Seq != expected {
				found(fmt.Sprintf("seq %d follows %d: the sequence has a gap", row.entry.Seq, expected-1))
				expected = row.entry.Seq
			}
			expected++
			report.Entries++
			report.Head = row.entry.Seq
			switch {
			case row.hash == nil:
				found(fmt.Sprintf("seq %d: no hash (the backfill has not run, or history was written around the chain)", row.entry.Seq))
				continue
			case len(row.hash) != 32:
				found(fmt.Sprintf("seq %d: hash is %d bytes, want 32", row.entry.Seq, len(row.hash)))
				continue
			}
			want, err := entryHash(repo.ID, row.entry, prev)
			if err != nil {
				found(fmt.Sprintf("seq %d: payload does not canonicalize: %v", row.entry.Seq, err))
				copy(prev[:], row.hash)
				continue
			}
			if !bytesEqual32(want, row.hash) {
				found(fmt.Sprintf("seq %d: hash mismatch — the entry's stored content is not what was hashed", row.entry.Seq))
			}
			// The next entry chains off the STORED hash, so one bad entry is
			// one finding, not a cascade.
			copy(prev[:], row.hash)
			report.HeadHash = hex.EncodeToString(row.hash)

			mustSign := signing.signedFrom > 0 && row.entry.Seq >= signing.signedFrom
			switch {
			case row.sig == nil && mustSign:
				found(fmt.Sprintf("seq %d: no signature, but signing is active from seq %d", row.entry.Seq, signing.signedFrom))
			case row.sig != nil && len(row.sig) != ed25519.SignatureSize:
				found(fmt.Sprintf("seq %d: signature is %d bytes, want %d", row.entry.Seq, len(row.sig), ed25519.SignatureSize))
			case row.sig != nil && len(signing.public) == 0:
				found(fmt.Sprintf("seq %d: a signature with no public key on the repository", row.entry.Seq))
			case row.sig != nil && !ed25519.Verify(signing.public, row.hash, row.sig):
				found(fmt.Sprintf("seq %d: signature does not verify against the repository's public key", row.entry.Seq))
			}
		}
	}
	if signing.signedFrom > 0 && len(signing.public) == 0 {
		found("signing is active but the repository stores no public key")
	}
	epochs, err := loadEpochs(ctx, db, repo.ID, signing.public)
	if err != nil {
		return report, err
	}
	report.Epochs = epochs
	if signing.signedFrom > 0 {
		activated := false
		for _, ep := range epochs {
			if ep.Reason == epochActivate {
				activated = true
			}
		}
		if !activated {
			// Informational, not a chain failure: the durable mark is the
			// guarantee and it is present; the attestation of the moment is
			// what is missing (a crash between the two).
			found(fmt.Sprintf("signing is active from seq %d but no activation epoch is recorded", signing.signedFrom))
		}
	}
	report.OK = len(report.Findings) == 0
	report.Took = time.Since(started)
	return report, nil
}

// chainRow is one stored entry with its integrity marks.
type chainRow struct {
	entry chainEntry
	hash  []byte
	sig   []byte
}

// scanChainPageWithMarks reads one page of entries with their stored hash and
// signature, over any pool.
func scanChainPageWithMarks(ctx context.Context, db dbx, after int64, limit int) ([]chainRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT seq, ts, actor, op, record_id, kind, payload::text, caused_by, hash, sig FROM changelog
		WHERE seq > $1 ORDER BY seq LIMIT $2`, after, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []chainRow
	for rows.Next() {
		var r chainRow
		var ts time.Time
		var causedBy sql.NullInt64
		if err := rows.Scan(&r.entry.Seq, &ts, &r.entry.Actor, &r.entry.Op, &r.entry.RecordID,
			&r.entry.Kind, &r.entry.PayloadText, &causedBy, &r.hash, &r.sig); err != nil {
			return nil, err
		}
		r.entry.TS = ts.UTC()
		r.entry.CausedBy, r.entry.CausedByOK = causedBy.Int64, causedBy.Valid
		out = append(out, r)
	}
	return out, rows.Err()
}

// loadEpochs reads the chain epochs in order, verifying each signed one
// against the repository's public key.
func loadEpochs(ctx context.Context, db dbx, repository string, public ed25519.PublicKey) ([]EpochInfo, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT at, reason, from_seq, old_head, new_head, public_key, signed_from, sig
		FROM chain_epochs ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []EpochInfo
	for rows.Next() {
		var ep chainEpoch
		var at time.Time
		var signedFrom sql.NullInt64
		var sig []byte
		if err := rows.Scan(&at, &ep.Reason, &ep.FromSeq, &ep.OldHead, &ep.NewHead,
			&ep.PublicKey, &signedFrom, &sig); err != nil {
			return nil, err
		}
		ep.At = at.UTC()
		if signedFrom.Valid {
			ep.SignedFrom = signedFrom.Int64
		}
		info := EpochInfo{
			At: ep.At, Reason: ep.Reason, FromSeq: ep.FromSeq,
			SignedFrom: ep.SignedFrom, Signed: sig != nil,
		}
		if ep.OldHead != nil {
			info.OldHead = hex.EncodeToString(ep.OldHead)
		}
		if ep.NewHead != nil {
			info.NewHead = hex.EncodeToString(ep.NewHead)
		}
		if ep.PublicKey != nil {
			info.PublicKey = hex.EncodeToString(ep.PublicKey)
		}
		if sig != nil && len(public) > 0 {
			h := epochHash(repository, ep)
			ok := ed25519.Verify(public, h[:], sig)
			info.SigOK = &ok
		}
		out = append(out, info)
	}
	return out, rows.Err()
}

func bytesEqual32(a [32]byte, b []byte) bool {
	if len(b) != 32 {
		return false
	}
	return a == [32]byte(b)
}
