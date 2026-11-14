package engine

// ERASURE (decision 0304): a permanent delete removes a record's values from
// the history, and "forget history" removes a live record's earlier values,
// keeping its current state.
//
// The intent is part of the history. A permanent delete, or a forget, writes
// an `erase` effect on its own line for every record whose history is to be
// erased, naming the record and the line it is erased up to, and no value.
// The fold notes it in compaction_erasures, a fold table, so a rebuild and a
// restore from the archive know what remains to erase, whatever happened to
// the process between the delete and the erasure.
//
// The erasure is a compaction, in a transaction of its own that follows the
// delete's commit in the same call: the line that releases a mapped target's
// copy is the delete's own, and only a compaction after the commit can put
// a target's state past it. It erases the record's history up to the line
// (a purged record's every effect becomes one state with neither row nor
// value, on its `gc` line; a live record's, one state that keeps its
// current values), takes the `erase` effect off its line and the row out of
// compaction_erasures, in the one transaction. The pass finishes what a
// call left: a crash between the two, a hold that ended.
//
// WHAT HOLDS AN ERASURE. A permanent delete's waits for an enabled record
// trigger whose source watches the kind's deletions, whose cursor stands
// past 0 and below the deletion's line, and which is delivering: a mirror's
// function may need the record's values to carry the deletion outward. A
// trigger with a parked failure, or whose cursor has not moved for
// erasureStallAfter, is passed, and meets the line with no value when it
// resumes. A forget waits for a line of the record a parked failure or a
// drain names, which a permanent delete settles first. Whoever may erase
// may finish an erasure now (finish), past every trigger.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// erasureStallAfter is how long a trigger's cursor may stand still before
// it no longer holds an erasure: a few dispatches, never the hours a stuck
// trigger could hold one for.
const erasureStallAfter = 5 * time.Minute

// applyErase notes an erasure's intent: the record, the line its history is
// erased up to, whether its current state stays, and the line that carries
// the intent, which the erasure takes it off.
func (t *txn) applyErase(op foldOp) error {
	if op.Ref == "" || op.ID == "" || op.Seq == nil {
		return fmt.Errorf("substrate/engine: an erase effect names no record or line")
	}
	line := t.lineSeq
	if line == 0 {
		line = t.maxSeq
	}
	if line == 0 {
		return fmt.Errorf("substrate/engine: an erase effect of %s %s joins no line", op.Ref, op.ID)
	}
	former, err := jsonList(op.FormerIDs)
	if err != nil {
		return err
	}
	_, err = t.exec(`
		INSERT INTO compaction_erasures (record_kind, record_id, upto, live, line, since, owned, on_behalf_of, former_ids)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)
		ON CONFLICT (repository, record_kind, record_id, line) DO UPDATE SET
			upto = EXCLUDED.upto, live = EXCLUDED.live, since = EXCLUDED.since,
			owned = EXCLUDED.owned, on_behalf_of = EXCLUDED.on_behalf_of, former_ids = EXCLUDED.former_ids`,
		op.Ref, op.ID, int64(*op.Seq), op.Live, line, t.now, op.Owned, op.OnBehalfOf, former)
	if op.Owned {
		t.closuresChecked = false
	}
	return err
}

// refuseWhileErasing refuses a create at an id whose permanent delete is
// still taking what the record owned, at any level of it: the batches find
// what it owned by its id and its former ids (ownedWhere), so a record
// created at one of them would lose what it owns to them. A replay judges
// nothing.
func (t *txn) refuseWhileErasing(ref eref) error {
	if t.replaying {
		return nil
	}
	if !t.closuresChecked {
		if err := t.row(`SELECT EXISTS (SELECT 1 FROM compaction_erasures WHERE owned)`).Scan(&t.closures); err != nil {
			return err
		}
		t.closuresChecked = true
	}
	if !t.closures {
		return nil
	}
	var owner string
	err := t.row(`
		SELECT record_id FROM compaction_erasures
		WHERE owned AND record_kind = $1 AND (record_id = $2 OR former_ids ? $2) LIMIT 1`, ref.Kind, ref.ID).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	deleted := eref{Kind: ref.Kind, ID: owner}
	left, err := t.ownedCount(deleted, t.ds.erasureBatchSize()+1)
	if err != nil {
		return err
	}
	return fmt.Errorf("%w: the permanent delete of %s still has at least %d of what it owned to erase; the compaction pass takes them from within a minute, so retry then",
		substrate.ErrErasing, vocabulary.RecordPath(deleted.Kind, deleted.ID), left)
}

// noteErasure writes the intent to erase ref's history up to upTo on the
// transaction's newest line, as an effect that joins it.
func (t *txn) noteErasure(ref eref, upTo int64, live bool) error {
	return t.noteErasureOf(ref, upTo, live, nil)
}

// noteErasureOf is noteErasure, with everything ref owns where it owns
// anything: ownsThrough is every id it owns through (ownership), and what
// it owns is erased in the pass's batches acting for the hand this
// transaction's line acts for.
func (t *txn) noteErasureOf(ref eref, upTo int64, live bool, ownsThrough []string) error {
	seq := foldInt(upTo)
	op := foldOp{Kind: foldErase, Ref: ref.Kind, ID: ref.ID, Seq: &seq, Live: live, Owned: len(ownsThrough) > 0}
	if op.Owned {
		op.OnBehalfOf = string(t.onBehalfOfKey(t.actor))
		for _, id := range ownsThrough {
			if id != ref.ID {
				op.FormerIDs = append(op.FormerIDs, id)
			}
		}
	}
	_, err := t.fold(op)
	return err
}

// pendingErasure is what remains to erase of one record (compaction_erasures).
type pendingErasure struct {
	Kind, ID string
	UpTo     int64
	Live     bool
	Since    time.Time
	Lines    []int64
	Waits    []substrate.ErasureWait
	// Held names what else holds it: a line a parked failure or a drain
	// names, or a merge's resync it shares with a live record, or a line
	// that does not verify, whose seq Damaged is.
	Held    string
	Damaged int64
	// Owned says everything the record owns goes with it, in batches
	// acting for OnBehalfOf.
	Owned      bool
	OnBehalfOf substrate.Actor
}

// EraseReport is what one erasure call did: the records whose erasure it
// finished, those that wait and on what, the lines it removed, and the
// records a batch purged because a record that went owned them.
type EraseReport struct {
	Erased  []eref
	Waiting []pendingErasure
	Removed int
	Purged  []eref
}

// pendingErasures reads compaction_erasures, by record, for the refs only
// names, every record where only is nil.
func (ds *dataset) pendingErasures(ctx context.Context, q dbx, only []eref) ([]pendingErasure, error) {
	var kinds, ids []string
	for _, r := range only {
		kinds, ids = append(kinds, r.Kind), append(ids, r.ID)
	}
	rows, err := q.QueryContext(ctx, `
		SELECT record_kind, record_id, upto, live, line, since, owned, on_behalf_of FROM compaction_erasures
		WHERE $1 OR (record_kind, record_id) IN (SELECT * FROM unnest($2::text[], $3::text[]))
		ORDER BY record_kind, record_id, line`, only == nil, kinds, ids)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	want := map[eref]bool{}
	for _, r := range only {
		want[r] = true
	}
	var out []pendingErasure
	for rows.Next() {
		var e pendingErasure
		var upTo, line int64
		var live, owned bool
		var since time.Time
		var onBehalfOf string
		if err := rows.Scan(&e.Kind, &e.ID, &upTo, &live, &line, &since, &owned, &onBehalfOf); err != nil {
			return nil, err
		}
		if only != nil && !want[eref{Kind: e.Kind, ID: e.ID}] {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Kind == e.Kind && out[n-1].ID == e.ID {
			last := &out[n-1]
			last.UpTo, last.Live = max(last.UpTo, upTo), last.Live && live
			last.Owned = last.Owned || owned
			if owned && onBehalfOf != "" {
				last.OnBehalfOf = substrate.Actor(onBehalfOf)
			}
			last.Lines = append(last.Lines, line)
			if since.Before(last.Since) {
				last.Since = since.UTC()
			}
			continue
		}
		e.UpTo, e.Live, e.Lines, e.Since = upTo, live, []int64{line}, since.UTC()
		e.Owned, e.OnBehalfOf = owned, substrate.Actor(onBehalfOf)
		out = append(out, e)
	}
	return out, rows.Err()
}

// erasureWaits is every trigger a permanent erasure of a kind's record up to
// upTo waits for (the file's header).
func (ds *dataset) erasureWaits(ctx context.Context, kind string, upTo int64) ([]substrate.ErasureWait, error) {
	triggers, err := ds.loadTriggers(ctx)
	if err != nil {
		return nil, err
	}
	var watching []string
	for _, tr := range triggers {
		if tr.Err != nil || !tr.Enabled || tr.Record == nil {
			continue
		}
		if tr.Record.matches(kind, string(substrate.OpDelete)) || tr.Record.matches(kind, string(substrate.OpGC)) {
			watching = append(watching, tr.ID)
		}
	}
	if len(watching) == 0 {
		return nil, nil
	}
	rows, err := ds.db.QueryContext(ctx, `
		SELECT c.trigger_id, c.seq, c.updated_at FROM trigger_cursors c
		WHERE c.trigger_id = ANY($1) AND c.seq > 0 AND c.seq < $2
		  AND c.updated_at > $3
		  AND NOT EXISTS (SELECT 1 FROM trigger_failures f WHERE f.trigger_id = c.trigger_id)
		ORDER BY c.trigger_id`, watching, upTo, nowUTC().Add(-erasureStallAfter))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []substrate.ErasureWait
	for rows.Next() {
		var w substrate.ErasureWait
		if err := rows.Scan(&w.Trigger, &w.Cursor, &w.Since); err != nil {
			return nil, err
		}
		w.Since = w.Since.UTC()
		out = append(out, w)
	}
	return out, rows.Err()
}

// eraseNow does what compaction_erasures holds for the refs only names (every
// record where only is nil), each in a transaction of its own; finish passes
// every trigger it would wait for. What it leaves is in the report, with
// what it waits on.
func (ds *dataset) eraseNow(ctx context.Context, only []eref, finish bool, batches int, pace func(context.Context, time.Duration) error) (EraseReport, error) {
	var report EraseReport
	// A note whose row was written back goes first (damage.go).
	if _, err := ds.lookAtDamage(ctx); err != nil {
		return report, err
	}
	pending, err := ds.pendingErasures(ctx, ds.db, only)
	if err != nil {
		return report, err
	}
	for i := 0; i < len(pending); i++ {
		e := pending[i]
		if !finish && !e.Live {
			waits, err := ds.erasureWaits(ctx, e.Kind, e.UpTo)
			if err != nil {
				return report, err
			}
			if len(waits) > 0 {
				e.Waits = waits
				report.Waiting = append(report.Waiting, e)
				continue
			}
		}
		var done CompactReport
		var held string
		if err := ds.withCompactionTx(ctx, true, func(t *txn) error {
			var err error
			done, held, err = t.eraseOne(e)
			return err
		}); err != nil {
			return report, fmt.Errorf("substrate/engine: erase %s %s: %w", e.Kind, e.ID, err)
		}
		ds.compacted(done)
		if held != "" {
			e.Held, e.Damaged = held, done.Damaged
			report.Waiting = append(report.Waiting, e)
			continue
		}
		report.Removed += done.Removed
		if !e.Owned {
			report.Erased = append(report.Erased, eref{Kind: e.Kind, ID: e.ID})
			continue
		}
		// What it owns, a batch a transaction, as many as the caller's
		// budget allows (batches < 0: until the context ends); the
		// records a batch purges have their own histories erased in this
		// call, and what they own waits for a later batch.
		ref, closed, short := eref{Kind: e.Kind, ID: e.ID}, false, false
		for ctx.Err() == nil && (batches != 0 || short) {
			if batches != 0 {
				batches--
				started := time.Now()
				// A batch is writes: the write path's own transaction,
				// which stamps its lines, under the changelog lock.
				var purged, follow []eref
				met := 0
				if err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
					var err error
					purged, follow, met, err = t.eraseOwnedBatch(ref, e.OnBehalfOf, ds.erasureBatchSize())
					return err
				}); err != nil {
					return report, fmt.Errorf("substrate/engine: erase what %s %s owns: %w", e.Kind, e.ID, err)
				}
				if met > 0 {
					report.Purged = append(report.Purged, purged...)
					if err := ds.compactionFault(erasureBatchDone); err != nil {
						return report, err
					}
					more, err := ds.pendingErasures(ctx, ds.db, follow)
					if err != nil {
						return report, err
					}
					pending = append(pending, more...)
					// A short batch took the last it found: one more look,
					// a query, finishes the erasure where nothing is left.
					short = met < ds.erasureBatchSize()
					if pace != nil {
						if err := pace(ctx, time.Since(started)); err != nil {
							break
						}
					}
					continue
				}
			}
			// Nothing left to purge, or a look past a short batch: the
			// intent goes once the record owns nothing.
			var finished CompactReport
			left := false
			if err := ds.withCompactionTx(ctx, true, func(t *txn) error {
				n, err := t.ownedCount(ref, 1)
				if err != nil {
					return err
				}
				if left = n > 0; left {
					// What is left that no batch may take, a finalizer holds.
					takeable, err := t.ownedPage(ref, 1)
					if err == nil && len(takeable) == 0 {
						held = finalizerHold
					}
					return err
				}
				if damaged, err := t.linesDamage(e.Lines); err != nil {
					return err
				} else if damaged > 0 {
					finished.Damaged, held = damaged, damagedHold(damaged)
					return t.noteDamage(damaged, "erasure")
				}
				finished, held, err = t.finishErasure(ref, e.Lines, finished)
				return err
			}); err != nil {
				return report, fmt.Errorf("substrate/engine: erase %s %s: %w", e.Kind, e.ID, err)
			}
			ds.compacted(finished)
			closed = !left && held == ""
			break
		}
		if closed {
			report.Erased = append(report.Erased, ref)
			continue
		}
		e.Held = held
		if e.Held == "" {
			e.Held = ownedHold(ds.erasureBatchSize())
		}
		report.Waiting = append(report.Waiting, e)
		ds.kickCompactor()
	}
	return report, nil
}

// ownedProgress is how far the erasure of what ref owns stands: at least
// how many it owns directly, counted no further than one batch and one, and
// how many went.
func (ds *dataset) ownedProgress(ctx context.Context, ref eref) (substrate.ErasureOwned, error) {
	out := substrate.ErasureOwned{Batch: ds.erasureBatchSize()}
	err := ds.withCompactionTx(ctx, false, func(t *txn) error {
		var err error
		if out.Remaining, err = t.ownedCount(ref, out.Batch+1); err != nil {
			return err
		}
		return t.row(`
			SELECT coalesce((SELECT purged FROM compaction_closures WHERE record_kind = $1 AND record_id = $2), 0)`,
			ref.Kind, ref.ID).Scan(&out.Purged)
	})
	return out, err
}

// finalizerHold is what an erasure whose closure waits on a finalizer says.
const finalizerHold = "a finalizer holds a record it owns: that record is tombstoned, and the compaction pass erases it once the finalizer is released"

// ownedHold is what an erasure that has records it owns still to go says.
func ownedHold(batch int) string {
	return fmt.Sprintf("what it owns is still to go: those records stay as they are until the compaction pass reaches them, in batches of %d, each one transaction, and erases them with it; until then a create at this id answers erasing", batch)
}

// eraseOne erases one record's history as compaction_erasures says, takes
// its intents off their lines and its rows out, all in the caller's
// transaction. It reports what holds it instead where something does.
func (t *txn) eraseOne(e pendingErasure) (CompactReport, string, error) {
	ref := eref{Kind: e.Kind, ID: e.ID}
	var report CompactReport
	// What remains is read again under the changelog lock: another call
	// may have finished it, or a later intent widened it, since it was
	// read.
	fresh, err := t.ds.pendingErasures(t.ctx, t.tx, []eref{ref})
	if err != nil || len(fresh) == 0 {
		return report, "", err
	}
	e.UpTo, e.Live, e.Lines = fresh[0].UpTo, fresh[0].Live, fresh[0].Lines
	merges, err := t.mergeLines(ref, e.UpTo)
	if err != nil {
		return report, "", err
	}
	// Every line the erasure edits besides the record's own history (the
	// intents, the merges' copies) is held to its checksum before any edit,
	// and so is its transaction: an erasure is whole or held.
	if damaged, err := t.linesDamage(append(slices.Clone(e.Lines), merges...)); err != nil {
		return report, "", err
	} else if damaged > 0 {
		report.Damaged = damaged
		return report, damagedHold(damaged), t.noteDamage(damaged, "erasure")
	}
	t.erasing = true
	if e.Live {
		report, err = t.compactRecords(e.UpTo, []eref{ref})
	} else {
		report, err = t.forgetRecords(e.UpTo, []eref{ref})
	}
	t.erasing = false
	switch {
	case err != nil:
		return report, "", err
	case report.Damaged > 0:
		return report, damagedHold(report.Damaged), nil
	case report.unindexed:
		return report, "the history's index is still being derived: the next pass derives it and finishes the erasure", nil
	case e.Live && slices.Contains(report.held, ref):
		return report, "a line of the record a parked failure or a drain names", nil
	case !e.Live && report.heldBack > 0:
		return report, "an effect of the record a compaction keeps", nil
	}
	stripped, err := t.stripMergeCopies(merges)
	if err != nil {
		return report, "", err
	}
	if err := t.eraseOutcomesOf(ref); err != nil {
		return report, "", err
	}
	report.touched = append(report.touched, stripped.touched...)
	if len(stripped.touched) > 0 {
		if err := t.markArchiveRewrite(stripped.touched); err != nil {
			return report, "", err
		}
	}
	// What the record owns is still to go: its intent stays until a batch
	// finds nothing left (eraseNow).
	if e.Owned {
		return report, "", nil
	}
	return t.finishErasure(ref, e.Lines, report)
}

// finishErasure takes ref's intents off their lines and its rows out, the
// last step of its erasure.
func (t *txn) finishErasure(ref eref, lines []int64, report CompactReport) (CompactReport, string, error) {
	// Nothing at or above a line known not to verify is edited (damage.go),
	// the cut read under the changelog lock the caller holds.
	if cut, err := t.damageCut(); err != nil {
		return report, "", err
	} else if cut > 0 && slices.ContainsFunc(lines, func(l int64) bool { return l >= cut }) {
		report.Damaged = cut
		return report, damagedHold(cut), nil
	}
	removed, damaged, err := t.removeIntents(ref, lines)
	if err != nil {
		return report, "", err
	}
	if damaged > 0 {
		report.Damaged = damaged
		return report, damagedHold(damaged), t.noteDamage(damaged, "erasure")
	}
	report.Removed += removed.Removed
	report.touched = append(report.touched, removed.touched...)
	if _, err := t.exec(`
		DELETE FROM compaction_erasures WHERE record_kind = $1 AND record_id = $2 AND line = ANY($3)`,
		ref.Kind, ref.ID, lines); err != nil {
		return report, "", err
	}
	if _, err := t.exec(`DELETE FROM compaction_closures WHERE record_kind = $1 AND record_id = $2`, ref.Kind, ref.ID); err != nil {
		return report, "", err
	}
	if len(removed.touched) > 0 {
		if err := t.markArchiveRewrite(removed.touched); err != nil {
			return report, "", err
		}
	}
	return report, "", nil
}

// mergeLines is every merge or split line up to upTo whose payload names
// ref as its winner or its loser: what the line moved (labels,
// annotations, marks) is a copy of the pair's values outside the fold.
func (t *txn) mergeLines(ref eref, upTo int64) ([]int64, error) {
	rows, err := t.query(`
		SELECT seq FROM changelog WHERE op = ANY($1) AND kind = $2 AND seq <= $3
		  AND (payload->>'winner' = $4 OR payload->>'loser' = $4) ORDER BY seq`,
		[]string{string(substrate.OpMerge), string(substrate.OpSplit)}, ref.Kind, upTo, ref.ID)
	if err != nil {
		return nil, err
	}
	var out []int64
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, seq)
	}
	return out, errors.Join(rows.Err(), rows.Close())
}

// linesDamage is the first of seqs that does not verify, or the lowest line
// that does not verify in the transaction of one, 0 for none.
func (t *txn) linesDamage(seqs []int64) (int64, error) {
	for _, seq := range seqs {
		row, err := queryChecksumRows(t.ctx, t.tx, `WHERE seq = $1`, seq)
		if err != nil {
			return 0, err
		}
		if len(row) == 0 {
			continue
		}
		if !row[0].verifies() {
			return seq, nil
		}
		if txn := row[0].entry.Txn; txn > 0 {
			if damaged, err := t.transactionDamage(txn); err != nil || damaged > 0 {
				return damaged, err
			}
		}
	}
	return 0, nil
}

// stripMergeCopies takes the copies of the pair's values off the merge and
// split lines at seqs, keeping their fold and the ids they name.
func (t *txn) stripMergeCopies(seqs []int64) (CompactReport, error) {
	var report CompactReport
	if len(seqs) == 0 {
		return report, nil
	}
	head, err := tableChangelogHead(t.ctx, t.tx)
	if err != nil {
		return report, err
	}
	edits := map[int64]*compactEdit{}
	known := map[int64]compactLine{}
	for _, seq := range seqs {
		var txn sql.NullInt64
		var copies bool
		err := t.row(`SELECT txn, payload ? 'moved' OR payload ? 'skipped' FROM changelog WHERE seq = $1`, seq).Scan(&txn, &copies)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return report, err
		}
		if !copies {
			continue
		}
		edits[seq] = &compactEdit{remove: map[int]bool{}, replace: map[int]foldOp{}, drop: []string{"moved", "skipped"}}
		known[seq] = compactLine{seq: seq, txn: txn.Int64}
	}
	if len(edits) == 0 {
		return report, nil
	}
	return report, t.applyCompactEdits(edits, known, head, &report)
}

// eraseOutcomesOf drops the stored Idempotency-Key outcome of every key that
// answered with ref (idempotency.go): the key store never enters the
// changelog, so no compaction reaches it. The row stays, so its effect stays
// run-once, and a retry is told the outcome went with the record's history.
func (t *txn) eraseOutcomesOf(ref eref) error {
	_, err := t.exec(`
		UPDATE idempotency_keys SET outcome = NULL, erased = true, locator = $3
		WHERE outcome IS NOT NULL
		  AND position(convert_to($1, 'UTF8') in outcome) > 0
		  AND position(convert_to($2, 'UTF8') in outcome) > 0`,
		`"kind":"`+ref.Kind+`"`, `"id":"`+ref.ID+`"`, vocabulary.RecordPath(ref.Kind, ref.ID))
	return err
}

// damagedHold is what an erasure held on a line that does not verify says.
func damagedHold(seq int64) string {
	return fmt.Sprintf("seq %d does not verify: write its row back from a whole copy, then the next pass or a forget finishes the erasure", seq)
}

// removeIntents takes ref's `erase` effects off the lines that carry them.
// It edits no line that does not verify and re-frames no transaction that
// holds one, and reports the first it meets instead (damage.go).
func (t *txn) removeIntents(ref eref, lines []int64) (CompactReport, int64, error) {
	var report CompactReport
	head, err := tableChangelogHead(t.ctx, t.tx)
	if err != nil {
		return report, 0, err
	}
	edits := map[int64]*compactEdit{}
	known := map[int64]compactLine{}
	for _, seq := range lines {
		row, err := queryChecksumRows(t.ctx, t.tx, `WHERE seq = $1`, seq)
		if err != nil {
			return report, 0, err
		}
		if len(row) == 0 {
			continue
		}
		if !row[0].verifies() {
			return report, seq, nil
		}
		txn := row[0].entry.Txn
		if txn > 0 {
			if damaged, err := t.transactionDamage(txn); err != nil || damaged > 0 {
				return report, damaged, err
			}
		}
		payload, err := decodeNumberPreserving(row[0].entry.PayloadText)
		if err != nil {
			return report, 0, err
		}
		effects, _ := payload[foldPayloadKey].([]any)
		for i, raw := range effects {
			eff, _ := raw.(map[string]any)
			if eff["kind"] == string(foldErase) && eff["ref"] == ref.Kind && eff["id"] == ref.ID {
				ed := edits[seq]
				if ed == nil {
					ed = &compactEdit{remove: map[int]bool{}, replace: map[int]foldOp{}}
					edits[seq] = ed
				}
				ed.remove[i] = true
			}
		}
		known[seq] = compactLine{seq: seq, txn: txn}
	}
	if len(edits) == 0 {
		return report, 0, nil
	}
	return report, 0, t.applyCompactEdits(edits, known, head, &report)
}

// The core kinds whose records ask for a change to another record: a change
// request names its target by a typed link, and goes with it.
const (
	kindPatchRequest = "substrate.reamde.dev/core/recordpatchrequest"
	kindMergeRequest = "substrate.reamde.dev/core/recordmergerequest"
)

// erasureScope is what a permanent delete of a record erases in the
// transaction that purges it (decision 0305), found before its tombstone
// releases what a mapping copied and its purge takes its former ids: the
// records merged into it, the change requests and the merge and split
// records that name it, every record a mapping copied a value of it into,
// and the ids it owns through where it owns anything. What it owns goes
// after, in batches (eraseOwnedBatch), from the intent that names it.
type erasureScope struct {
	losers, requests, copies []eref
	owns                     []string
}

// scopeOfErasure reads the erasureScope of ref.
func (t *txn) scopeOfErasure(ref eref) (erasureScope, error) {
	var s erasureScope
	seen := map[eref]bool{ref: true}
	add := func(into *[]eref, refs []eref) {
		for _, r := range refs {
			if !seen[r] {
				seen[r] = true
				*into = append(*into, r)
			}
		}
	}
	losers, err := t.refsOf(`
		SELECT f.record_kind, f.former_id FROM former_ids f
		JOIN records r ON r.kind = f.record_kind AND r.id = f.former_id
		WHERE f.record_kind = $1 AND f.record_id = $2 ORDER BY f.former_id`, ref.Kind, ref.ID)
	if err != nil {
		return s, err
	}
	add(&s.losers, losers)
	requests, err := t.refsOf(`
		SELECT DISTINCT src_kind, src FROM refs WHERE dst_kind = $1 AND dst = $2 AND src_kind = ANY($3)
		UNION
		SELECT kind, id FROM records WHERE kind = $4 AND props->>'targetKind' = $1 AND props->>'targetId' = $2
		ORDER BY 1, 2`, ref.Kind, ref.ID, []string{kindPatchRequest, kindMergeRequest, kindRecordMerge}, kindPatchRequest)
	if err != nil {
		return s, err
	}
	add(&s.requests, requests)
	copies, err := t.copiesOf(ref)
	if err != nil {
		return s, err
	}
	add(&s.copies, copies)
	// A split's own record names its merge's, and keeps what the split
	// moved back: it goes with the merge record.
	for _, r := range slices.Clone(s.requests) {
		if r.Kind != kindRecordMerge {
			continue
		}
		splits, err := t.refsOf(`
			SELECT DISTINCT src_kind, src FROM refs WHERE dst_kind = $1 AND dst = $2 AND src_kind = $3
			ORDER BY 1, 2`, r.Kind, r.ID, kindRecordSplit)
		if err != nil {
			return s, err
		}
		add(&s.requests, splits)
	}
	s.owns, err = t.ownership(ref)
	return s, err
}

// erasureBatch bounds what one transaction of an erasure takes of what a
// record owns: the request that deletes the record runs one batch, and the
// compaction pass runs the rest, each one transaction under the changelog
// lock, sleeping as long as each took (docs/operations.md). It is no
// setting: a smaller one leaves a large closure more passes, and a create at
// its id waiting longer; a larger one holds the lock longer.
const erasureBatch = 100

// erasureBatchSize is erasureBatch, or what a test set.
func (ds *dataset) erasureBatchSize() int {
	if ds.svc.erasureBatch > 0 {
		return ds.svc.erasureBatch
	}
	return erasureBatch
}

// cascadingRefsIn is every kind and top-level property of reg whose
// reference declares `onDelete: cascade`: a record whose such reference
// names another is owned by it (gc.go cascadeChildren).
func cascadingRefsIn(reg *vocabulary.Registry) (kinds, props []string) {
	for _, k := range reg.Kinds() {
		for name, p := range k.Props {
			if p.Cascades() {
				kinds, props = append(kinds, k.Identity), append(props, name)
			}
		}
	}
	return kinds, props
}

func (t *txn) cascadingRefs() (kinds, props []string) { return cascadingRefsIn(t.declarations()) }

// ownedWhere is the refs a record owner owns through, live or tombstoned:
// $1 its kind, $2 its ids, $3 and $4 the cascading kinds and properties.
const ownedWhere = `FROM refs r
	JOIN records x ON x.kind = r.src_kind AND x.id = r.src
	WHERE r.dst_kind = $1 AND r.dst = ANY($2) AND r.path = ''
	  AND (r.src_kind, r.property) IN (SELECT * FROM unnest($3::text[], $4::text[]))`

// ownerIDs is every id owner owns through: its own, its former ids while
// it stands, and those its standing intent kept once its purge took them.
func (t *txn) ownerIDs(owner eref) ([]string, error) {
	ids, err := t.idsOf(owner)
	if err != nil {
		return nil, err
	}
	rows, err := t.query(`
		SELECT DISTINCT jsonb_array_elements_text(former_ids) FROM compaction_erasures
		WHERE owned AND record_kind = $1 AND record_id = $2`, owner.Kind, owner.ID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}

// ownership is every id ref owns through where it owns anything, nil where
// it owns nothing: read before its purge, which takes its former ids.
func (t *txn) ownership(ref eref) ([]string, error) {
	n, err := t.ownedCount(ref, 1)
	if err != nil || n == 0 {
		return nil, err
	}
	return t.ownerIDs(ref)
}

// ownedPage is up to limit of the records owner owns directly that a batch
// may take, in kind and id order: live, or tombstoned and held by nothing.
// One a finalizer holds is tombstoned by the batch that meets it and waits
// for its release (eraseOwnedBatch).
func (t *txn) ownedPage(owner eref, limit int) ([]eref, error) {
	ids, err := t.ownerIDs(owner)
	if err != nil {
		return nil, err
	}
	kinds, props := t.cascadingRefs()
	if len(kinds) == 0 {
		return nil, nil
	}
	return t.refsOf(`SELECT DISTINCT r.src_kind, r.src `+ownedWhere+`
		AND (x.deleted_at IS NULL OR cardinality(x.finalizers) = 0) ORDER BY 1, 2 LIMIT $5`,
		owner.Kind, ids, kinds, props, limit)
}

// ownedCount is how many records owner owns directly, live or tombstoned,
// held or not, counted no further than limit: what a refusal and a reply
// say is "at least", and the count holds the changelog lock.
func (t *txn) ownedCount(owner eref, limit int) (int64, error) {
	ids, err := t.ownerIDs(owner)
	if err != nil {
		return 0, err
	}
	kinds, props := t.cascadingRefs()
	if len(kinds) == 0 {
		return 0, nil
	}
	var n int64
	err = t.row(`SELECT count(*) FROM (SELECT DISTINCT r.src_kind, r.src `+ownedWhere+` LIMIT $5) o`,
		owner.Kind, ids, kinds, props, limit).Scan(&n)
	return n, err
}

// eraseOwnedBatch takes up to limit of the records owner owns, in this one
// transaction, under the engine's hand acting for the hand the owner's
// deletion acted for, so their lines name who asked. Each goes the one path
// a record the request was asked to delete goes (purgeForErasure), its own
// intent taking what it owns in turn; one a finalizer holds is left
// tombstoned for a later batch. It reports what it purged, what their
// erasure follows with, and how many records it met.
func (t *txn) eraseOwnedBatch(owner eref, onBehalfOf substrate.Actor, limit int) (purged, follow []eref, met int, err error) {
	page, err := t.ownedPage(owner, limit)
	if err != nil || len(page) == 0 {
		return nil, nil, 0, err
	}
	t.onBehalfOf = onBehalfOf
	for _, r := range page {
		_, erased, copies, err := t.purgeForErasure(r, nil)
		var held purgeHeld
		if errors.As(err, &held) {
			continue
		}
		if err != nil {
			return nil, nil, 0, err
		}
		purged = append(purged, erased...)
		follow = append(append(follow, erased...), copies...)
	}
	if len(purged) > 0 {
		if _, err := t.exec(`
			INSERT INTO compaction_closures (record_kind, record_id, purged) VALUES ($1, $2, $3)
			ON CONFLICT (repository, record_kind, record_id) DO UPDATE SET
				purged = compaction_closures.purged + EXCLUDED.purged, updated_at = now()`,
			owner.Kind, owner.ID, len(purged)); err != nil {
			return nil, nil, 0, err
		}
	}
	return purged, follow, len(page), nil
}

// purgeHeld is a purge a finalizer holds: the request answers it 409 and
// rolls its tombstone back, and a batch keeps the tombstone and leaves the
// record to a later batch, once it is released.
type purgeHeld struct {
	id         string
	finalizers []string
}

func (e purgeHeld) Error() string {
	return fmt.Sprintf("%v: record %s is held by finalizers %v; delete it without purge, wait for them to release, then purge",
		substrate.ErrConflict, e.id, e.finalizers)
}

func (e purgeHeld) Unwrap() error { return substrate.ErrConflict }

// purgeForErasure is the one path a permanently deleted record goes
// (decision 0305): the request's for the record it was asked to delete, and
// a batch's for each record a closure reaches, so neither erases less. What
// the tombstone and the purge take away is read first (scopeOfErasure); the
// tombstone goes through the mapping's lifecycle (softDeleteIf); then the
// purge and the intents (purgeToErase). What the record owns in turn waits
// for its own intent's batches. It reports the record and the records whose
// erasure follows the commit: those it purged, and the mapped copies.
func (t *txn) purgeForErasure(ref eref, ifVersion *int64) (*substrate.Record, []eref, []eref, error) {
	scope, err := t.scopeOfErasure(ref)
	if err != nil {
		return nil, nil, nil, err
	}
	e, err := t.softDeleteIf(ref, ifVersion)
	if err != nil {
		return nil, nil, nil, err
	}
	// A former id resolves to the merge winner, and a purge cannot be
	// undone: purging through a loser's id would hard-delete the winner.
	// Refuse it as a put through a former id is refused, naming the
	// canonical id, and roll the tombstone back with it.
	if e.ID != ref.ID {
		return nil, nil, nil, fmt.Errorf("%w: %s is a former id of %s: purge the canonical id", substrate.ErrConflict, ref.ID, e.ID)
	}
	if len(e.Finalizers) > 0 {
		return e, nil, nil, purgeHeld{id: ref.ID, finalizers: e.Finalizers}
	}
	erased, copies, err := t.purgeToErase(ref, scope)
	return e, erased, copies, err
}

// purgeToErase purges a record and every record its erasureScope names,
// settles the parked failures and drains on their lines, and writes the
// intent to erase each, and every mapped copy, on the transaction's newest
// line (the file's header). It reports the records it purged and the
// mapped copies, whose erasures follow the commit.
func (t *txn) purgeToErase(ref eref, scope erasureScope) (erased, copies []eref, err error) {
	losers, requests, targets := scope.losers, scope.requests, scope.copies
	// What it owns goes after, in batches, from the intent that names it.
	t.noCascade = true
	defer func() { t.noCascade = false }()
	// The ids each purged record owns through, before its purge takes them.
	owns := map[eref][]string{ref: scope.owns}
	for _, r := range append(slices.Clone(requests), losers...) {
		if owns[r], err = t.ownership(r); err != nil {
			return nil, nil, err
		}
	}
	first := len(t.entries)
	if err := t.purgeNow(ref, nil); err != nil {
		return nil, nil, err
	}
	erased = []eref{ref}
	for _, r := range requests {
		// A merge's or a split's own record is the substrate's to keep, so
		// no delete takes it: it goes as a merge's loser does.
		if r.Kind == kindRecordMerge || r.Kind == kindRecordSplit {
			if row, err := t.loadRow(r, true); err != nil {
				return nil, nil, err
			} else if row != nil {
				if err := t.hardDelete(r); err != nil {
					return nil, nil, err
				}
				if err := t.appendChange(t.actor, substrate.OpGC, r.ID, r.Kind, map[string]any{"reason": "purged"}); err != nil {
					return nil, nil, err
				}
			}
			erased = append(erased, r)
			continue
		}
		if row, err := t.loadRow(r, false); err != nil {
			return nil, nil, err
		} else if row != nil && row.DeletedAt == nil {
			if _, err := t.softDelete(r); err != nil {
				return nil, nil, err
			}
		}
		if err := t.purgeIfThere(r); err != nil {
			return nil, nil, err
		}
		erased = append(erased, r)
	}
	// A loser stays tombstoned under its merge's finalizer, for a split that
	// can no longer come: it goes with the winner.
	for _, l := range losers {
		if err := t.hardDelete(l); err != nil {
			return nil, nil, err
		}
		if err := t.appendChange(t.actor, substrate.OpGC, l.ID, l.Kind, map[string]any{"reason": "purged"}); err != nil {
			return nil, nil, err
		}
		erased = append(erased, l)
	}
	if err := t.settleHoldsOn(erased); err != nil {
		return nil, nil, err
	}
	newest := map[eref]int64{}
	for _, e := range t.entries[first:] {
		newest[eref{Kind: e.kind, ID: e.id}] = e.seq
	}
	for _, r := range erased {
		if seq := newest[r]; seq > 0 {
			if err := t.noteErasureOf(r, seq, false, owns[r]); err != nil {
				return nil, nil, err
			}
		}
	}
	// A target's release may ride another record's line, as an effect the
	// transaction settled there: then it is erased up to the transaction's
	// newest line, which is at or past the release.
	for _, target := range targets {
		if slices.Contains(erased, target) || slices.Contains(copies, target) {
			continue
		}
		seq := newest[target]
		if seq == 0 {
			seq = t.maxSeq
		}
		if err := t.noteErasure(target, seq, true); err != nil {
			return nil, nil, err
		}
		copies = append(copies, target)
	}
	// The intents join the newest line now, the line compaction_erasures
	// names for them, before a batch's next record appends another that a
	// late effect would ride.
	return erased, copies, t.settleFold()
}

// purgeIfThere purges a record a cascade tombstoned, or a request, where it
// still has a row: its own owned records go with it, and its gc line.
func (t *txn) purgeIfThere(ref eref) error {
	row, err := t.loadRow(ref, false)
	if err != nil || row == nil {
		return err
	}
	return t.purgeNow(ref, row.Finalizers)
}

// copiesOf is every record a mapping copied a value of ref into: its marks
// name ref as a source (derivedFrom), or ref's mapping points at it as its
// subject. Read before ref's purge, which takes both away.
func (t *txn) copiesOf(ref eref) ([]eref, error) {
	out, err := t.refsOf(`
		SELECT DISTINCT record_kind, record_id FROM property_marks
		WHERE derived_from ? $1 ORDER BY 1, 2`, vocabulary.RecordPath(ref.Kind, ref.ID))
	if err != nil {
		return nil, err
	}
	// The subject each of its mappings points it at: a tombstone before
	// the purge released the marks, and the pointer is still on its row.
	reg := t.declarations()
	ty, ok := reg.ByIdentity(ref.Kind)
	if !ok {
		return out, nil
	}
	for _, m := range reg.MappingsFrom(ty.Identity) {
		target, err := t.subjectTargetOf(ref, m.Property)
		if err != nil {
			return nil, err
		}
		if target.ID != "" && !slices.Contains(out, target) {
			out = append(out, target)
		}
	}
	return out, nil
}

// refsOf runs a query that returns kind and id pairs.
func (t *txn) refsOf(query string, args ...any) ([]eref, error) {
	rows, err := t.query(query, args...)
	if err != nil {
		return nil, err
	}
	var out []eref
	for rows.Next() {
		var r eref
		if err := rows.Scan(&r.Kind, &r.ID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, r)
	}
	return out, errors.Join(rows.Err(), rows.Close())
}

// settleHoldsOn forgets every parked failure whose line is one of refs',
// and drops every record drain whose chain names one: a retry of a change to
// a record erased has nothing to deliver, and its line is no longer held.
func (t *txn) settleHoldsOn(refs []eref) error {
	for _, r := range refs {
		rows, err := t.query(`
			SELECT f.trigger_id, f.id FROM trigger_failures f
			JOIN changelog c ON c.seq = f.seq
			WHERE c.kind = $1 AND c.record_id = $2 ORDER BY f.trigger_id, f.id`, r.Kind, r.ID)
		if err != nil {
			return err
		}
		type parked struct {
			trigger string
			id      int64
		}
		var found []parked
		for rows.Next() {
			var p parked
			if err := rows.Scan(&p.trigger, &p.id); err != nil {
				_ = rows.Close()
				return err
			}
			found = append(found, p)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		for _, p := range found {
			if err := t.lockFailure(p.trigger, p.id); err != nil {
				return err
			}
			if err := t.unparkTx(p.trigger, p.id); err != nil {
				return err
			}
			if err := t.settleDelivery(p.trigger); err != nil {
				return err
			}
			if err := t.settleParkedAlert(p.trigger); err != nil {
				return err
			}
		}
		chains, err := t.query(`
			SELECT p.trigger_id, p.chain FROM paged_cursors p
			WHERE p.kind = $3 AND EXISTS (
				SELECT 1 FROM changelog c WHERE c.kind = $1 AND c.record_id = $2
				  AND p.chain LIKE '%/' || c.seq::text)
			ORDER BY p.trigger_id, p.chain`, r.Kind, r.ID, pagedKindRecord)
		if err != nil {
			return err
		}
		type drain struct{ trigger, chain string }
		var drains []drain
		for chains.Next() {
			var d drain
			if err := chains.Scan(&d.trigger, &d.chain); err != nil {
				_ = chains.Close()
				return err
			}
			drains = append(drains, d)
		}
		if err := errors.Join(chains.Err(), chains.Close()); err != nil {
			return err
		}
		for _, d := range drains {
			if err := t.unpageTx(d.trigger, d.chain); err != nil {
				return err
			}
			if err := t.settleDelivery(d.trigger); err != nil {
				return err
			}
		}
	}
	return nil
}

// erasureOf is where erasing ref stands, from compaction_erasures and what
// a call just left of it: the reply and the record's status read the table,
// so they say the same after a restart.
func (ds *dataset) erasureOf(ctx context.Context, ref eref, live bool, report EraseReport) (*substrate.Erasure, error) {
	out := &substrate.Erasure{Kind: ref.Kind, ID: ref.ID, Live: live}
	pending, err := ds.pendingErasures(ctx, ds.db, []eref{ref})
	if err != nil || len(pending) == 0 {
		return out, err
	}
	p := pending[0]
	since := p.Since
	out.Pending, out.Live, out.Since = true, p.Live, &since
	for _, w := range report.Waiting {
		if w.Kind == ref.Kind && w.ID == ref.ID {
			out.Waits, out.Held, out.Damaged = w.Waits, w.Held, w.Damaged
		}
	}
	if p.Owned && !p.Live {
		owned, err := ds.ownedProgress(ctx, ref)
		if err != nil {
			return nil, err
		}
		out.Owned = &owned
		if out.Held == "" {
			out.Held = ownedHold(owned.Batch)
		}
	}
	if out.Held == "" {
		cut, err := ds.knownDamage(ctx)
		if err != nil {
			return nil, err
		}
		if cut > 0 && cut <= p.UpTo {
			out.Held, out.Damaged = damagedHold(cut), cut
		}
	}
	if !p.Live && out.Waits == nil && out.Held == "" {
		if out.Waits, err = ds.erasureWaits(ctx, ref.Kind, p.UpTo); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Forget is a record's forget door (decision 0306). On a live record it
// erases the history before its current state, and every copy a mapping
// made of its values in another record's; on a record whose erasure waits,
// it finishes it, past every trigger where in.Finish says so. The owner's
// alone: the class the check reads is the owner's hand (ownerHand), which
// the roles make an admin's.
func (ds *dataset) Forget(ctx context.Context, actor substrate.Actor, typ, id string, in substrate.ForgetInput) (*substrate.Erasure, error) {
	ref := eref{Kind: typ, ID: id}
	var follow []eref
	live := false
	err := ds.inTx(ctx, actor, true, func(t *txn) error {
		if !t.ownerHand() {
			return fmt.Errorf("%w: only the owner may forget a record's history", substrate.ErrForbidden)
		}
		row, err := t.loadRow(ref, true)
		if err != nil {
			return err
		}
		if row == nil || row.DeletedAt != nil {
			return nil
		}
		live = true
		targets, err := t.copiesOf(ref)
		if err != nil {
			return err
		}
		if err := t.appendChange(t.actor, substrate.OpPatch, id, typ, map[string]any{"reason": "forget"}); err != nil {
			return err
		}
		upTo := t.maxSeq
		follow = append(follow, ref)
		if err := t.noteErasure(ref, upTo, true); err != nil {
			return err
		}
		for _, target := range targets {
			if err := t.noteErasure(target, upTo, true); err != nil {
				return err
			}
			follow = append(follow, target)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if follow == nil {
		pending, err := ds.pendingErasures(ctx, ds.db, []eref{ref})
		if err != nil {
			return nil, err
		}
		if len(pending) == 0 {
			return nil, fmt.Errorf("%w: record %s, and no erasure of it waits", substrate.ErrNotFound, id)
		}
		follow = []eref{ref}
	}
	if live {
		if err := ds.compactionFault(erasureBetween); err != nil {
			return nil, err
		}
	}
	report, err := ds.eraseNow(ctx, follow, in.Finish, 1, nil)
	if err != nil {
		return nil, err
	}
	return ds.erasureOf(ctx, ref, live, report)
}
