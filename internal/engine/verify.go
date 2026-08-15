package engine

// The chain verifier: walk one repository's changelog start to head,
// recompute every hash from the stored bytes, check every signature the
// signing state requires, and report either the verified head or every
// finding by seq and name. It MUTATES NOTHING in the repository it judges —
// no backfill, no repair — and reads everything inside one read-only
// repeatable-read transaction, so a concurrent write cannot make it stitch
// two states into one report.
//
// WHAT ANCHORS IT. Everything the verifier reads — entries, signatures, the
// public key, the activation mark, the epochs — lives in the same mutable
// database. Unpinned, a database-only attacker can rewrite all of it
// together and present a self-consistent forgery; the signed activation
// epoch raises that bar (forging one needs the key), and the PINS close it:
// an operator who passes the pinned (public key, signed-from, head receipt)
// makes the comparison the design depends on enforceable instead of manual.
// The findings are a closed taxonomy, not library errors.

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

// VerifyPins is what the caller KNOWS from outside the database: the pair
// logged at activation, and a head receipt written down earlier. Zero values
// mean "not pinned"; a pinned value that disagrees with the store is a
// finding, which is the entire point of pinning.
type VerifyPins struct {
	// PublicKey is the hex Ed25519 public key logged at activation.
	PublicKey string `json:"publicKey,omitempty"`
	// SignedFrom is the activation seq logged beside it.
	SignedFrom int64 `json:"signedFrom,omitempty"`
	// HeadSeq/HeadHash are a remembered receipt: the entry AT HeadSeq must
	// still carry exactly HeadHash. Later entries may exist (history grows);
	// an earlier head or a different hash at that seq is a finding — this is
	// what catches a truncated or rewritten tail.
	HeadSeq  int64  `json:"headSeq,omitempty"`
	HeadHash string `json:"headHash,omitempty"`
}

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

// VerifyRepository walks one repository's whole chain with nothing pinned.
func (s *service) VerifyRepository(ctx context.Context, username string) (VerifyReport, error) {
	return s.VerifyRepositoryPinned(ctx, username, VerifyPins{})
}

// VerifyRepositoryPinned walks the chain and additionally holds the store to
// what the caller pinned outside it. Findings land in the report, not in the
// error: the error is for "could not verify" (no such user, no connection),
// never for "verified and found tampering".
func (s *service) VerifyRepositoryPinned(ctx context.Context, username string, pins VerifyPins) (VerifyReport, error) {
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
	// of the open ladder's writes. One read-only repeatable-read transaction
	// holds the walk and the epoch read to a single database state.
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
	res, err := verifyChainCorePinned(ctx, tx, repo.ID, signing.signedFrom, signing.public, pins.HeadSeq, found)
	if err != nil {
		return report, err
	}
	report.Entries, report.Head = res.entries, res.head
	if res.headHash != nil {
		report.HeadHash = hex.EncodeToString(res.headHash)
	}

	if signing.signedFrom > 0 && len(signing.public) == 0 {
		found("signing is active but the repository stores no public key")
	}
	epochs, err := loadEpochs(ctx, tx, repo.ID, signing.public)
	if err != nil {
		return report, err
	}
	report.Epochs = epochs
	verifyEpochs(epochs, signing, found)

	// The pins: the caller's out-of-band knowledge, enforced.
	if pins.PublicKey != "" && pins.PublicKey != report.PublicKey {
		found(fmt.Sprintf("pinned public key %s does not match the repository's %s — the key has been replaced", pins.PublicKey, report.PublicKey))
	}
	if pins.SignedFrom > 0 && pins.SignedFrom != signing.signedFrom {
		found(fmt.Sprintf("pinned signed-from seq %d does not match the repository's %d — the activation mark has moved", pins.SignedFrom, signing.signedFrom))
	}
	if pins.HeadSeq > 0 {
		switch got := res.hashAt(pins.HeadSeq); {
		case pins.HeadSeq > res.head:
			found(fmt.Sprintf("pinned head seq %d is beyond the stored head %d — the tail has been truncated", pins.HeadSeq, res.head))
		case got == "":
			found(fmt.Sprintf("pinned head seq %d carries no hash", pins.HeadSeq))
		case got != pins.HeadHash:
			explained := false
			for _, ep := range epochs {
				if ep.Reason == epochReseal && ep.OldHead == pins.HeadHash {
					explained = true
				}
			}
			if explained {
				// A sanctioned rewrite: the epoch names the old head the pin
				// remembers. Reported for the operator to re-pin, not a
				// failure.
				found(fmt.Sprintf("pinned head at seq %d matches a reseal epoch's old head: history was resealed since the pin; re-pin from this report", pins.HeadSeq))
			} else {
				found(fmt.Sprintf("pinned head at seq %d does not match the stored hash and no epoch explains it", pins.HeadSeq))
			}
		}
	}
	report.OK = len(report.Findings) == 0
	report.Took = time.Since(started)
	return report, nil
}

// chainWalkResult is what one full-chain walk measured.
type chainWalkResult struct {
	entries  int64
	head     int64
	headHash []byte
	// pinnedSeq/pinnedHash carry the one extra hash a caller asked about.
	pinnedSeq  int64
	pinnedHash string
}

func (r *chainWalkResult) hashAt(seq int64) string {
	if seq > 0 && seq == r.pinnedSeq {
		return r.pinnedHash
	}
	return ""
}

// verifyChainCore walks the WHOLE chain over any pool or transaction,
// recomputes every hash from the stored bytes, checks every signature the
// signing state requires, and hands each finding to found. It is the one
// implementation: VerifyRepository, rebuild's pre-fold check and reseal's
// pre-rewrite check all run through here, so "verifies" means the same
// thing on all three doors.
func verifyChainCore(ctx context.Context, db dbx, repository string, signedFrom int64, public ed25519.PublicKey, found func(string)) (chainWalkResult, error) {
	return verifyChainCorePinned(ctx, db, repository, signedFrom, public, 0, found)
}

func verifyChainCorePinned(ctx context.Context, db dbx, repository string, signedFrom int64, public ed25519.PublicKey, pinSeq int64, found func(string)) (chainWalkResult, error) {
	var res chainWalkResult
	res.pinnedSeq = pinSeq
	prev := zeroHash
	expected := int64(1)
	for {
		page, err := scanChainPageWithMarks(ctx, db, expected-1, rebuildBatch)
		if err != nil {
			return res, err
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
			res.entries++
			res.head = row.entry.Seq
			switch {
			case row.hash == nil:
				found(fmt.Sprintf("seq %d: no hash (the backfill has not run, or history was written around the chain)", row.entry.Seq))
				continue
			case len(row.hash) != 32:
				found(fmt.Sprintf("seq %d: hash is %d bytes, want 32", row.entry.Seq, len(row.hash)))
				continue
			}
			want, err := entryHash(repository, row.entry, prev)
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
			res.headHash = row.hash
			if pinSeq > 0 && row.entry.Seq == pinSeq {
				res.pinnedHash = hex.EncodeToString(row.hash)
			}

			mustSign := signedFrom > 0 && row.entry.Seq >= signedFrom
			switch {
			case row.sig == nil && mustSign:
				found(fmt.Sprintf("seq %d: no signature, but signing is active from seq %d", row.entry.Seq, signedFrom))
			case row.sig != nil && len(row.sig) != ed25519.SignatureSize:
				found(fmt.Sprintf("seq %d: signature is %d bytes, want %d", row.entry.Seq, len(row.sig), ed25519.SignatureSize))
			case row.sig != nil && len(public) == 0:
				found(fmt.Sprintf("seq %d: a signature with no public key on the repository", row.entry.Seq))
			case row.sig != nil && !ed25519.Verify(public, row.hash, row.sig):
				found(fmt.Sprintf("seq %d: signature does not verify against the repository's public key", row.entry.Seq))
			}
		}
	}
	return res, nil
}

// verifyEpochs holds the epoch rows to what they claim to be (adversarial
// review #2: an epoch nobody checks is scenery). An invalid signature on ANY
// epoch is a finding; when signing is active, the activation epoch must
// exist, be signed by the repository's key, and agree with the durable mark
// — an unsigned or disagreeing `activate` row is exactly what a forgery
// looks like. Epoch DELETION remains detectable only through a pinned head
// or receipt: epochs are statements, and the signed activation epoch plus
// the pins are what make the statements checkable.
func verifyEpochs(epochs []EpochInfo, signing signingState, found func(string)) {
	publicHex := ""
	if len(signing.public) > 0 {
		publicHex = hex.EncodeToString(signing.public)
	}
	activated := false
	for _, ep := range epochs {
		if ep.SigOK != nil && !*ep.SigOK {
			found(fmt.Sprintf("epoch (%s, from seq %d): signature does not verify", ep.Reason, ep.FromSeq))
		}
		// On a signed repository, a transition that touches the signed range
		// is signed by construction (the engine holds the key whenever it
		// writes one), so an unsigned epoch claiming to explain signed
		// history is exactly what a forged explanation looks like.
		if signing.signedFrom > 0 && ep.FromSeq >= signing.signedFrom && !ep.Signed {
			found(fmt.Sprintf("epoch (%s, from seq %d): unsigned, but it claims a transition inside signed history", ep.Reason, ep.FromSeq))
		}
		if ep.Reason != epochActivate {
			continue
		}
		switch {
		case !ep.Signed || ep.SigOK == nil || !*ep.SigOK:
			found(fmt.Sprintf("epoch (activate, from seq %d): not signed by the repository's key — an activation is signed by construction", ep.FromSeq))
		case ep.PublicKey != publicHex:
			found(fmt.Sprintf("epoch (activate, from seq %d): names a different public key than the repository holds", ep.FromSeq))
		case ep.SignedFrom != signing.signedFrom || ep.FromSeq != signing.signedFrom:
			found(fmt.Sprintf("epoch (activate, from seq %d, signed from %d): disagrees with the repository's activation mark (%d)", ep.FromSeq, ep.SignedFrom, signing.signedFrom))
		default:
			activated = true
		}
	}
	if signing.signedFrom > 0 && !activated {
		found(fmt.Sprintf("signing is active from seq %d but no valid activation epoch is recorded", signing.signedFrom))
	}
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
