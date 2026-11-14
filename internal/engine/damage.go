package engine

// KNOWN DAMAGE. While a line of the history is known not to verify, nothing
// is compacted or erased at or above it, and the repair comes first.
//
// A reader that finds a line whose columns are not what its stored checksum
// was stamped over (checksumRow.verifies) notes its seq in changelog_damage:
// a compaction (the door's whole-history run, the pass's batches),
// backfillForeign, an erasure, and the archive pass where it refuses a row. A batch
// finds a record's lines by the very columns damage may change, so it can
// miss a line a whole-history reader meets; the note makes what one reader
// found hold for all of them. Every compaction reads the lowest noted seq
// under the changelog lock and stays below it (compactOver); an erasure is
// whole or held on it, intent kept (erase.go). A note outlives
// the failure that found it: the archive pass and the backfill commit theirs
// on their own. `repository verify` notes nothing: it reads and writes
// nothing, beside a running server.
//
// A note goes once its row verifies again or is gone: each pass looks at
// every noted row once (lookAtDamage). Damage no reader has met is unknown
// to all of them: until one meets it, a batch can place a state above it
// without the effect it was written with, and an erasure can leave a value
// in it. `repository verify
// --recanonicalize` reads every line and names it.

import (
	"context"
	"errors"
	"slices"
)

// damagedLineError is a reader's refusal of a line that does not verify,
// carrying its seq for the note.
type damagedLineError struct {
	seq int64
	err error
}

func (e *damagedLineError) Error() string { return e.err.Error() }
func (e *damagedLineError) Unwrap() error { return e.err }

// damagedLineOf is the seq a refusal names, 0 where err is no such refusal.
func damagedLineOf(err error) int64 {
	var d *damagedLineError
	if errors.As(err, &d) {
		return d.seq
	}
	return 0
}

// noteDamage notes a line that does not verify in the caller's transaction.
func (t *txn) noteDamage(seq int64, by string) error {
	_, err := t.exec(`INSERT INTO changelog_damage (seq, noted_by) VALUES ($1, $2) ON CONFLICT DO NOTHING`, seq, by)
	return err
}

// noteDamageAlone notes a line that does not verify on a commit of its own,
// for a reader whose own work the refusal rolls back.
func (ds *dataset) noteDamageAlone(ctx context.Context, seq int64, by string) error {
	_, err := ds.db.ExecContext(context.WithoutCancel(ctx), `
		INSERT INTO changelog_damage (seq, noted_by) VALUES ($1, $2) ON CONFLICT DO NOTHING`, seq, by)
	return err
}

// knownDamage is the lowest line known not to verify, 0 for none, read
// outside a compaction: a bound for a window, never the cut itself.
func (ds *dataset) knownDamage(ctx context.Context) (int64, error) {
	var cut int64
	err := ds.db.QueryRowContext(ctx, `SELECT coalesce(min(seq), 0) FROM changelog_damage`).Scan(&cut)
	return cut, err
}

// damageNoter names the reader a compaction's note is from.
func (t *txn) damageNoter() string {
	if t.erasing {
		return "erasure"
	}
	return "compaction"
}

// damageCut is the lowest line known not to verify, 0 for none: a
// compaction reads it under the changelog lock and stays below it.
func (t *txn) damageCut() (int64, error) {
	var cut int64
	err := t.row(`SELECT coalesce(min(seq), 0) FROM changelog_damage`).Scan(&cut)
	return cut, err
}

// lookAtDamage drops every note whose row verifies again or is gone, one
// row read for each, and returns the seqs still noted, lowest first.
func (ds *dataset) lookAtDamage(ctx context.Context) ([]int64, error) {
	rows, err := ds.db.QueryContext(ctx, `SELECT seq FROM changelog_damage ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	var noted []int64
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			_ = rows.Close()
			return nil, err
		}
		noted = append(noted, seq)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	var still []int64
	for _, seq := range noted {
		line, err := queryChecksumRows(ctx, ds.db, `WHERE seq = $1`, seq)
		if err != nil {
			return nil, err
		}
		if len(line) == 1 && !line[0].verifies() {
			still = append(still, seq)
			continue
		}
		if _, err := ds.db.ExecContext(ctx, `DELETE FROM changelog_damage WHERE seq = $1`, seq); err != nil {
			return nil, err
		}
	}
	return still, nil
}

// noticeDamage logs, once a pass, the lines that hold it back.
func (ds *dataset) noticeDamage(still []int64) {
	if len(still) == 0 {
		return
	}
	ds.svc.log.Warn("substrate: a line of the history does not verify: nothing at or above it is compacted or erased until its row is written back from a whole copy",
		"repository", ds.info.ID, "below", slices.Min(still), "seqs", still)
}
