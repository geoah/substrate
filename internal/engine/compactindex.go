package engine

// THE FOREIGN-EFFECTS INDEX (decision 0300). A line is addressed to one
// record, and it carries the effects of every record its transaction wrote
// since the line before it: a merge's loser, a resync's scope, an effect a
// transaction settled onto its last line. changelog_foreign holds one row
// for each record a line carries an effect on and is not addressed to, so a
// record's whole history is its own lines (changelog_record_idx) and these,
// read without scanning the history. A compaction of a window of history
// (compactpass.go) reads a record's history that way; a whole-history
// compaction (compact.go) needs no index.
//
// The write path records a line's rows in the transaction that appends it
// (settleForeign), a compaction rewrites the rows of the lines it changes,
// and a history that came in another way (the upgrade to the release that
// brought the index, a restore, an import) is indexed by the pass before it
// compacts anything (compaction_state.foreign_upto). The index is derived
// from the changelog alone: `repository rebuild` leaves it.

import (
	"context"
	"slices"
)

// foreignOf is every record the effects carry an effect on, other than addr,
// in first-touch order: a resync names each record of its scope, every
// other effect the record it lands on.
func foreignOf(ops []foldOp, addr eref) []eref {
	var out []eref
	add := func(r eref) {
		if r.Kind == "" || r.ID == "" || r == addr || slices.Contains(out, r) {
			return
		}
		out = append(out, r)
	}
	for _, op := range ops {
		if op.Kind == foldResync {
			for _, r := range op.Scope {
				add(eref(r))
			}
			continue
		}
		add(op.ref())
	}
	return out
}

// mergeRefs is a with every ref of b it does not hold, in order.
func mergeRefs(a, b []eref) []eref {
	for _, r := range b {
		if !slices.Contains(a, r) {
			a = append(a, r)
		}
	}
	return a
}

// settleForeign writes the index rows of every line the transaction
// appended, at its commit, once each line's effects are final (settleFold).
func (t *txn) settleForeign() error {
	var kinds, ids []string
	var seqs []int64
	for _, e := range t.pending {
		for _, r := range e.Foreign {
			kinds, ids, seqs = append(kinds, r.Kind), append(ids, r.ID), append(seqs, e.Seq)
		}
	}
	if len(seqs) == 0 {
		return nil
	}
	_, err := t.exec(`
		INSERT INTO changelog_foreign (record_kind, record_id, seq)
		SELECT * FROM unnest($1::text[], $2::text[], $3::bigint[])
		ON CONFLICT DO NOTHING`, kinds, ids, seqs)
	return err
}

// reindexForeign writes the index rows of the lines at seqs again from what
// they now carry: a compaction's rewrite removed effects from them, or the
// lines themselves.
func (t *txn) reindexForeign(seqs []int64) error {
	if len(seqs) == 0 {
		return nil
	}
	if _, err := t.exec(`DELETE FROM changelog_foreign WHERE seq = ANY($1)`, seqs); err != nil {
		return err
	}
	_, err := t.indexForeignOf(`WHERE seq = ANY($1) ORDER BY seq`, seqs)
	return err
}

// indexForeignOf derives the index rows of the lines tail selects, in order
// of seq, up to the first that does not verify, whose seq it reports: the
// index trusts nothing of such a line (damage.go).
func (t *txn) indexForeignOf(tail string, args ...any) (int64, error) {
	rows, err := queryChecksumRows(t.ctx, t.tx, tail, args...)
	if err != nil {
		return 0, err
	}
	var kinds, ids []string
	var seqs []int64
	damaged := int64(0)
	for _, r := range rows {
		if !r.verifies() {
			damaged = r.entry.Seq
			break
		}
		ch, err := changeOfEntry(r.entry.fileEntry())
		if err != nil {
			return 0, err
		}
		ops, err := foldOpsOf(ch)
		if err != nil {
			return 0, err
		}
		for _, ref := range foreignOf(ops, eref{Kind: ch.Kind, ID: ch.RecordID}) {
			kinds, ids, seqs = append(kinds, ref.Kind), append(ids, ref.ID), append(seqs, ch.Seq)
		}
	}
	if len(seqs) == 0 {
		return damaged, nil
	}
	_, err = t.exec(`
		INSERT INTO changelog_foreign (record_kind, record_id, seq)
		SELECT * FROM unnest($1::text[], $2::text[], $3::bigint[])
		ON CONFLICT DO NOTHING`, kinds, ids, seqs)
	return damaged, err
}

// foreignBackfillBatch bounds one transaction of the index's derivation.
const foreignBackfillBatch = 2000

// backfillForeign derives the index for the history it does not cover yet,
// a batch of lines per transaction, until it reaches the head or the
// context ends. It reports whether the index covers the whole history: the
// pass compacts nothing until it does. The write path indexes every line it
// appends, so lines past the head the derivation saw are indexed already,
// and a line both index is written once (ON CONFLICT). It stops below a line
// that does not verify and notes it on a commit of its own, so the index and
// the cut agree (damage.go): nothing above that line is compacted, and the
// index covers what is below it.
func (ds *dataset) backfillForeign(ctx context.Context, pace func(context.Context) error) (bool, error) {
	for {
		var done bool
		var damaged int64
		err := ds.withCompactionTx(ctx, false, func(t *txn) error {
			var upto int64
			if err := t.row(`
				INSERT INTO compaction_state (foreign_upto) VALUES (0)
				ON CONFLICT (repository) DO UPDATE SET foreign_upto = compaction_state.foreign_upto
				RETURNING foreign_upto, foreign_done`).Scan(&upto, &done); err != nil {
				return err
			}
			if done {
				return nil
			}
			head, err := tableChangelogHead(t.ctx, t.tx)
			if err != nil {
				return err
			}
			to := min(upto+foreignBackfillBatch, head)
			if damaged, err = t.indexForeignOf(`WHERE seq > $1 AND seq <= $2 ORDER BY seq`, upto, to); err != nil {
				return err
			}
			if damaged > 0 {
				to = damaged - 1
			}
			done = to >= head
			if _, err := t.exec(`UPDATE compaction_state SET foreign_upto = $1, foreign_done = $2, updated_at = now()`, to, done); err != nil {
				return err
			}
			return ds.compactionFault(compactionInIndex)
		})
		// The derivation stops below the line, and only the note lets the
		// pass go on below it: one that cannot be noted is a derivation
		// that did not finish.
		if damaged > 0 && err == nil {
			err = ds.noteDamageAlone(ctx, damaged, "index")
		}
		if err != nil || done {
			return done && err == nil, err
		}
		if damaged > 0 {
			return true, nil
		}
		if err := pace(ctx); err != nil {
			return false, err
		}
	}
}

// markForeignIndexed records that the index covers the whole history: a
// creation, whose every line the write path indexed as it appended it. An
// erasure reads a record's lines by the index and waits for its derivation
// otherwise (compact.go).
func (t *txn) markForeignIndexed() error {
	_, err := t.exec(`
		INSERT INTO compaction_state (foreign_upto, foreign_done) VALUES (0, true)
		ON CONFLICT (repository) DO UPDATE SET foreign_done = true, updated_at = now()`)
	return err
}

// resetForeignIndex makes the pass derive the index again from the start:
// history came in by a path that does not index it (an import).
func (t *txn) resetForeignIndex() error {
	_, err := t.exec(`UPDATE compaction_state SET foreign_upto = 0, foreign_done = false, updated_at = now()`)
	return err
}
