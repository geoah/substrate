package engine

// The chain backfill: entries written before the chain existed get their
// hashes at the repository's FIRST OPEN under this binary, before it serves a
// write — the same per-repository, lazy, idempotent shape as the shipped
// vocabulary upgrade, and for the same reason: it leaves a repository nobody
// opens untouched.
//
// ONE TRANSACTION, deliberately (adversarial review #1): the epoch that says
// "attested history begins at from_seq" must be exactly as durable as the
// hashes it describes. A chunked backfill had two crash windows — hashes
// without their epoch (backfilled rows indistinguishable from witnessed
// ones), and a resumed run recording the resumption point as the beginning —
// and both misstate provenance, which is the one thing the epoch exists to
// state. The reads still page (a transaction holds pages fine); only the
// COMMIT is whole.
//
// A backfilled hash attests forward from the moment of backfill, nothing
// more: if history was already tampered with, the backfill notarizes the
// tampered bytes. `verify` reports coverage from the epoch rather than
// pretending the past was witnessed.
//
// Deployment assumption, stated rather than engineered around: ONE writer
// process per database. An old binary appending unhashed rows after a new
// one backfilled would break the NULL-suffix invariant; the interior NULL
// check catches that state and refuses rather than notarizing around it.

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"fmt"
	"time"
)

// backfillBatch bounds one page of reads inside the backfill transaction.
const backfillBatch = 500

func (ds *dataset) backfillChain(ctx context.Context) error {
	var total, first int64
	err := ds.inRawTx(ctx, func(t *txn) error {
		if err := t.lockKey(changelogLockKey); err != nil {
			return err
		}
		var hashedHead int64
		if err := t.row(`SELECT coalesce(max(seq), 0) FROM changelog WHERE hash IS NOT NULL`).Scan(&hashedHead); err != nil {
			return err
		}
		// Hashed rows must be a prefix: the chain cannot be computed out of
		// order. An interior NULL means somebody wrote around the chain (a
		// second, older writer; a hand UPDATE), and stamping past it would
		// notarize the damage.
		var holes int64
		if err := t.row(`SELECT count(*) FROM changelog WHERE seq <= $1 AND hash IS NULL`, hashedHead).Scan(&holes); err != nil {
			return err
		}
		if holes > 0 {
			return fmt.Errorf("substrate/engine: chain backfill refuses: %d entries below the hashed head (seq %d) have no hash — history has been written around the chain; run `repository verify`", holes, hashedHead)
		}
		prev := zeroHash
		if hashedHead > 0 {
			var raw []byte
			if err := t.row(`SELECT hash FROM changelog WHERE seq = $1`, hashedHead).Scan(&raw); err != nil {
				return err
			}
			if len(raw) != 32 {
				return fmt.Errorf("substrate/engine: chain backfill: the hashed head (seq %d) carries a %d-byte hash", hashedHead, len(raw))
			}
			copy(prev[:], raw)
		}
		signing := ds.signing()
		after, expected := hashedHead, hashedHead+1
		for {
			entries, err := t.chainPage(after, backfillBatch)
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				break
			}
			for _, e := range entries {
				if e.Seq != expected {
					return fmt.Errorf("substrate/engine: chain backfill refuses: seq %d follows %d — the sequence has a gap", e.Seq, expected-1)
				}
				expected++
				h, err := entryHash(ds.scope.Repository, e, prev)
				if err != nil {
					return err
				}
				var sig []byte
				if signing.signedFrom > 0 && e.Seq >= signing.signedFrom {
					if signing.key == nil {
						return fmt.Errorf("substrate/engine: chain backfill: signing is active from seq %d but the signing key is unavailable", signing.signedFrom)
					}
					sig = ed25519.Sign(signing.key, h[:])
				}
				res, err := t.exec(`UPDATE changelog SET hash = $2, sig = $3 WHERE seq = $1`, e.Seq, h[:], sig)
				if err != nil {
					return err
				}
				if n, err := res.RowsAffected(); err != nil {
					return err
				} else if n != 1 {
					return fmt.Errorf("substrate/engine: chain backfill: stamping seq %d touched %d rows", e.Seq, n)
				}
				if first == 0 {
					first = e.Seq
				}
				total++
				prev = h
				after = e.Seq
			}
		}
		if total == 0 {
			return nil
		}
		// The provenance mark, in the SAME commit as the hashes it explains.
		ep := chainEpoch{
			At: t.now, Reason: epochBackfill, FromSeq: first, NewHead: prev[:],
		}
		if signing.signedFrom > 0 {
			ep.PublicKey = []byte(signing.public)
			ep.SignedFrom = signing.signedFrom
		}
		return t.recordEpoch(ep, signing.key)
	})
	if err != nil {
		return err
	}
	if total > 0 {
		ds.svc.log.Info("substrate: chain backfill complete",
			"repository", ds.scope.Repository, "entries", total, "fromSeq", first)
	}
	return nil
}

// chainPage reads one page of entries AS THE CHAIN sees them: every preimage
// column with the payload as stored text, plus nothing — the hash and sig
// columns have their own readers (verify).
func (t *txn) chainPage(after int64, limit int) ([]chainEntry, error) {
	rows, err := t.query(`
		SELECT seq, ts, actor, op, record_id, kind, payload::text, caused_by FROM changelog
		WHERE seq > $1 ORDER BY seq LIMIT $2`, after, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []chainEntry
	for rows.Next() {
		var e chainEntry
		var ts time.Time
		var causedBy sql.NullInt64
		if err := rows.Scan(&e.Seq, &ts, &e.Actor, &e.Op, &e.RecordID, &e.Kind, &e.PayloadText, &causedBy); err != nil {
			return nil, err
		}
		e.TS = ts.UTC()
		e.CausedBy, e.CausedByOK = causedBy.Int64, causedBy.Valid
		out = append(out, e)
	}
	return out, rows.Err()
}

// chainHead reads the head seq and its hash; a zero head means an empty
// changelog and a zero hash.
func (t *txn) chainHead() (int64, []byte, error) {
	var head int64
	if err := t.row(`SELECT coalesce(max(seq), 0) FROM changelog`).Scan(&head); err != nil {
		return 0, nil, err
	}
	if head == 0 {
		return 0, nil, nil
	}
	var raw []byte
	if err := t.row(`SELECT hash FROM changelog WHERE seq = $1`, head).Scan(&raw); err != nil {
		return 0, nil, err
	}
	if len(raw) != 32 {
		return head, nil, fmt.Errorf("substrate/engine: the head entry (seq %d) has no hash", head)
	}
	return head, raw, nil
}
