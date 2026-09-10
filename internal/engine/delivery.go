package engine

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// THE DELIVERY LEDGER.
//
// A trigger's bookkeeping is described by the changelog, not held in Postgres
// alone: the cursor a record trigger has reached, the occurrence a schedule
// trigger last fired, the failures it parked and the resume row of a paged
// drain must survive a repository directory imported into an empty database.
//
// Every motion of those four tables is a FOLD EFFECT
// (fold.go: cursor, schedule, park, unpark, page, unpage, forget), applied
// here through the same foldOne a replay drives, and recorded on a `delivery`
// entry (substrate.OpDelivery) appended in the transaction that commits the
// effects the motion acknowledges. A rebuild or an import clears the four
// tables with the rest of the fold and folds them back from the entries. The
// entry is internal: every public read skips it (query.go Changes,
// changefeed.go ChangesBefore), a trigger's own read hides it from the source
// match (functions.go matchChanges), and it is never on the wire.
//
// The public webhook door writes the ledger too: a request it admits is a
// parked failure carrying pendingWebhookError from before the 202 until its
// fire settles, and the fire is that row's retry (webhooks.go admitWebhook,
// fireWebhook, resumeWebhooks; decision 0068).
//
// One position is deliberately not in the ledger: the SCAN position a record
// trigger moves past rows that did not match its source (functions.go
// advanceCursor). It acknowledges nothing, and recording it would append one
// entry per trigger per pass with new rows, which every other trigger would
// then scan past and record in turn. It is written to the table alone, always
// at or above the acknowledged position and never past an undelivered match,
// so a restore resumes from the last acknowledged delivery and re-reads rows
// that deliver nothing.
//
// Decision 0064 records the design.

// deliveryTables are the ledger's four tables, cleared and replayed with the
// fold (rebuild.go foldTables). oauth_flows stays runtime state: a consent
// flow interrupted by a restore is started again.
var deliveryTables = []string{"trigger_cursors", "trigger_schedule", "trigger_failures", "paged_cursors"}

// applyDelivery applies one ledger effect to its table. It is the fold's
// door to the four tables: a live motion below reaches them through
// txn.fold, a replay through foldEntry, and nothing else writes them except
// the scan position (functions.go advanceCursor).
func (t *txn) applyDelivery(op foldOp) (bool, error) {
	switch op.Kind {
	case foldCursor:
		if op.Seq == nil {
			return false, fmt.Errorf("substrate/engine: a cursor effect on %s carries no seq", op.ID)
		}
		_, err := t.exec(`
			INSERT INTO trigger_cursors (trigger_id, seq, updated_at) VALUES ($1, $2, $3)
			ON CONFLICT (repository, trigger_id) DO UPDATE SET seq = EXCLUDED.seq, updated_at = EXCLUDED.updated_at`,
			op.ID, int64(*op.Seq), t.now)
		return true, err
	case foldSchedule:
		if op.At == nil {
			return false, fmt.Errorf("substrate/engine: a schedule effect on %s carries no occurrence", op.ID)
		}
		_, err := t.exec(`
			INSERT INTO trigger_schedule (trigger_id, fired_at, updated_at) VALUES ($1, $2, $3)
			ON CONFLICT (repository, trigger_id) DO UPDATE SET fired_at = EXCLUDED.fired_at, updated_at = EXCLUDED.updated_at`,
			op.ID, op.At.UTC(), t.now)
		return true, err
	case foldPark:
		if op.Failure == nil {
			return false, fmt.Errorf("substrate/engine: a park effect on %s carries no failure", op.ID)
		}
		f := op.Failure
		_, err := t.exec(`
			INSERT INTO trigger_failures (id, trigger_id, seq, fire_id, record_id, attempts, last_error, parked_at, payload)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)
			ON CONFLICT (repository, id) DO UPDATE
			SET trigger_id = EXCLUDED.trigger_id, seq = EXCLUDED.seq, fire_id = EXCLUDED.fire_id,
			    record_id = EXCLUDED.record_id, attempts = EXCLUDED.attempts, last_error = EXCLUDED.last_error,
			    parked_at = EXCLUDED.parked_at, payload = EXCLUDED.payload`,
			int64(f.ID), op.ID, int64(f.Seq), f.FireID, f.RecordID, int64(f.Attempts), f.LastError,
			f.ParkedAt.UTC(), rawOrSQLNull(f.Payload))
		return true, err
	case foldUnpark:
		if op.Failure == nil {
			return false, fmt.Errorf("substrate/engine: an unpark effect on %s names no failure", op.ID)
		}
		return t.rowsChanged(`DELETE FROM trigger_failures WHERE id = $1 AND trigger_id = $2`, int64(op.Failure.ID), op.ID)
	case foldPage:
		if op.Page == nil {
			return false, fmt.Errorf("substrate/engine: a page effect on %s carries no row", op.ID)
		}
		p := op.Page
		_, err := t.exec(`
			INSERT INTO paged_cursors (chain, cursor, pages, version, effects, bytes, started_at, trigger_id, kind, identity, updated_at)
			VALUES ($1, $2::jsonb, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT (repository, chain) DO UPDATE
			SET cursor = EXCLUDED.cursor, pages = EXCLUDED.pages, version = EXCLUDED.version,
			    effects = EXCLUDED.effects, bytes = EXCLUDED.bytes, started_at = EXCLUDED.started_at,
			    trigger_id = EXCLUDED.trigger_id, kind = EXCLUDED.kind, identity = EXCLUDED.identity,
			    updated_at = EXCLUDED.updated_at`,
			p.Chain, rawOrNull(p.Cursor), int64(p.Pages), int64(p.Version), int64(p.Effects), int64(p.Bytes),
			p.StartedAt.UTC(), op.ID, p.Kind, p.Identity, t.now)
		return true, err
	case foldUnpage:
		if op.Page == nil {
			return false, fmt.Errorf("substrate/engine: an unpage effect on %s names no chain", op.ID)
		}
		return t.rowsChanged(`DELETE FROM paged_cursors WHERE chain = $1`, op.Page.Chain)
	case foldForget:
		changed := false
		for _, table := range deliveryTables {
			n, err := t.rowsChanged(`DELETE FROM `+table+` WHERE trigger_id = $1`, op.ID)
			if err != nil {
				return false, err
			}
			changed = changed || n
		}
		return changed, nil
	}
	return false, fmt.Errorf("substrate/engine: %q is not a delivery effect", op.Kind)
}

// rowsChanged runs a statement and reports whether it touched a row.
func (t *txn) rowsChanged(q string, args ...any) (bool, error) {
	res, err := t.exec(q, args...)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// rawOrSQLNull is a jsonb parameter for an optional raw JSON value: SQL NULL
// when the ledger carried none.
func rawOrSQLNull(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}

// --- the live motions ---
//
// Each one is the compare-and-swap the dispatcher relies on, then the folded
// effect. The swap is a read under a row lock rather than a conditional
// UPDATE, so the write itself is the fold's and the same on both paths; a
// swap that misses is errCursorMoved, and the transaction rolls back whole.

// advanceCursorTx moves a record trigger's acknowledged cursor from `from` to
// `to` inside the transaction that commits what it acknowledges. The row is
// read FOR UPDATE, so two dispatchers on one trigger serialize here and the
// loser reads the winner's committed value. The changelog lock comes first,
// as before every row lock (rows.go changelogLockKey).
func (t *txn) advanceCursorTx(triggerID string, from, to int64) error {
	if err := t.lockChangelog(); err != nil {
		return err
	}
	var seq int64
	err := t.row(`SELECT seq FROM trigger_cursors WHERE trigger_id = $1 FOR UPDATE`, triggerID).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return errCursorMoved
	}
	if err != nil {
		return err
	}
	if seq != from {
		return errCursorMoved
	}
	_, err = t.fold(foldOp{Kind: foldCursor, Ref: typeTrigger, ID: triggerID, Seq: ptrTo(foldInt(to))})
	return err
}

// setCursorTx sets a record trigger's cursor without a swap: the position a
// trigger starts at (initTriggerBookkeeping, ensureCursor) and the reset a
// replay asks for (ReplayTrigger).
func (t *txn) setCursorTx(triggerID string, seq int64) error {
	_, err := t.fold(foldOp{Kind: foldCursor, Ref: typeTrigger, ID: triggerID, Seq: ptrTo(foldInt(seq))})
	return err
}

// pinTriggerCursor records a record trigger's current cursor, scan position
// included, in the transaction that edits the trigger. The scan position
// past rows that matched nothing is otherwise Postgres-only (functions.go
// advanceCursor), and a replay that recovered only the last acknowledged
// delivery would apply the EDITED source to rows the old source scanned past:
// a trigger widened to a kind it excluded would deliver, after a restore, a
// change it never delivered live. Pinned at the edit, the replay applies the
// new definition only from the edit on. A trigger with no cursor row (a
// schedule or a webhook) pins nothing.
func (t *txn) pinTriggerCursor(triggerID string) error {
	var seq int64
	err := t.row(`SELECT seq FROM trigger_cursors WHERE trigger_id = $1`, triggerID).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := t.setCursorTx(triggerID, seq); err != nil {
		return err
	}
	return t.settleDelivery(triggerID)
}

// advanceScheduleTx is the fire-state motion, compare-and-swap on the
// occurrence the pass read: the schedule twin of advanceCursorTx.
func (t *txn) advanceScheduleTx(triggerID string, from, to time.Time) error {
	if err := t.lockChangelog(); err != nil {
		return err
	}
	var at time.Time
	err := t.row(`SELECT fired_at FROM trigger_schedule WHERE trigger_id = $1 FOR UPDATE`, triggerID).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return errCursorMoved
	}
	if err != nil {
		return err
	}
	if !at.Equal(from) {
		return errCursorMoved
	}
	return t.setScheduleTx(triggerID, to)
}

// setScheduleTx sets a schedule trigger's fire state without a swap: the
// occurrence a trigger starts at (initTriggerBookkeeping, ensureScheduleState).
func (t *txn) setScheduleTx(triggerID string, at time.Time) error {
	at = at.UTC()
	_, err := t.fold(foldOp{Kind: foldSchedule, Ref: typeTrigger, ID: triggerID, At: &at})
	return err
}

// parkTx records a parked failure, whole. A fresh park takes its id from
// reserveSeq, the seq of the delivery entry the caller appends next; a retry
// that fails again writes the same id with the new attempt count and error.
func (t *txn) parkTx(triggerID string, f foldFailure) error {
	_, err := t.fold(foldOp{Kind: foldPark, Ref: typeTrigger, ID: triggerID, Failure: &f})
	return err
}

// inFlightError is the error a claim carries (settlement.claim): a parked
// failure row that stands while an agent delivery runs, and that outlives a
// crash as the record of an interrupted delivery, retried by hand.
const inFlightError = "delivery in flight: an agent run a restart interrupted stays here, retried by hand"

// errClaimedElsewhere is a dispatch finding a claim it did not write on the
// delivery it is about to run: another dispatch (a replayed pass under a
// running loop, a second dispatcher) holds it. The finder skips the delivery
// and moves its cursor past it rather than running the loop twice.
var errClaimedElsewhere = errors.New("substrate/engine: the delivery is claimed by another dispatch")

// claimedFailure finds the failure row that CLAIMS a delivery, a record
// change's by its seq or a fire's by its id: the row carrying inFlightError.
// An older park of the same delivery is not a claim; a replay that reaches a
// parked change parks it again, cursor motion included, and moves on.
func (t *txn) claimedFailure(triggerID string, seq int64, fireID string) (int64, bool, error) {
	var id int64
	err := t.row(`SELECT id FROM trigger_failures WHERE trigger_id = $1 AND seq = $2 AND fire_id = $3 AND last_error = $4`,
		triggerID, seq, fireID, inFlightError).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

// errFailureRetired is lockFailure finding the row gone: another hand retired
// it, so its delivery landed. It wraps ErrNotFound for the API and is named
// so a fire that meets it stops (functions.go deliverFire) rather than
// running the callable again and failing to park.
var errFailureRetired = errors.New("substrate/engine: the parked failure was retired")

// lockFailure holds a parked failure's row FOR UPDATE, after the changelog
// lock, for a retry about to retire or rewrite it. A row that is gone was
// retired by another hand: the retry that finds it gone writes nothing and
// answers not found (errFailureRetired), so two retries of one failure
// cannot both repeat its effects on the tables, and a delivered failure
// never comes back.
func (t *txn) lockFailure(triggerID string, id int64) error {
	if err := t.lockChangelog(); err != nil {
		return err
	}
	var one int
	err := t.row(`SELECT 1 FROM trigger_failures WHERE trigger_id = $1 AND id = $2 FOR UPDATE`, triggerID, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %w: trigger %s has no parked failure %d", errFailureRetired, substrate.ErrNotFound, triggerID, id)
	}
	return err
}

// unparkTx retires a parked failure: a retry that delivered.
func (t *txn) unparkTx(triggerID string, id int64) error {
	_, err := t.fold(foldOp{Kind: foldUnpark, Ref: typeTrigger, ID: triggerID, Failure: &foldFailure{ID: foldInt(id)}})
	return err
}

// forgetTx drops every delivery row a trigger owns: the tombstone's motion.
func (t *txn) forgetTx(triggerID string) error {
	_, err := t.fold(foldOp{Kind: foldForget, Ref: typeTrigger, ID: triggerID})
	return err
}

// claimPagedCursor claims an ABSENT chain for a fresh drain's first middle
// page. The check runs under the changelog lock, which every delivery
// transaction takes before it commits, so two fresh drains of one chain
// serialize here and the second reads the first's committed row:
// errCursorMoved rolls its page back. The claimed row starts at version 1.
func (t *txn) claimPagedCursor(chain string, owner pagedOwner, cursor any, pages, effects, bytes int64, startedAt time.Time) error {
	if err := t.lockChangelog(); err != nil {
		return err
	}
	var one int
	err := t.row(`SELECT 1 FROM paged_cursors WHERE chain = $1`, chain).Scan(&one)
	if err == nil {
		return errCursorMoved
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return t.pageTx(owner, chain, cursor, 1, pages, effects, bytes, startedAt)
}

// advancePagedCursor moves an OWNED chain to the next page under its version
// swap: the row is read FOR UPDATE, matched to the exact version this drain
// last saw, and rewritten one version up. A missed swap is errCursorMoved.
func (t *txn) advancePagedCursor(chain string, version int64, cursor any, pages, effects, bytes int64) error {
	owner, have, startedAt, err := t.lockPagedCursor(chain)
	if err != nil {
		return err
	}
	if have != version {
		return errCursorMoved
	}
	return t.pageTx(owner, chain, cursor, version+1, pages, effects, bytes, startedAt)
}

// clearPagedCursorCAS drops a drained chain's row, the final page, requiring
// the version this drain owns, so a chain a concurrent dispatcher advanced is
// never cleared under it.
func (t *txn) clearPagedCursorCAS(chain string, version int64) error {
	owner, have, _, err := t.lockPagedCursor(chain)
	if err != nil {
		return err
	}
	if have != version {
		return errCursorMoved
	}
	return t.unpageTx(owner.triggerID, chain)
}

// lockPagedCursor reads a chain's row FOR UPDATE: its owner, version and
// start. An absent row is errCursorMoved, since every caller owns a row.
func (t *txn) lockPagedCursor(chain string) (pagedOwner, int64, time.Time, error) {
	var owner pagedOwner
	var version int64
	var startedAt time.Time
	if err := t.lockChangelog(); err != nil {
		return owner, 0, time.Time{}, err
	}
	err := t.row(`
		SELECT trigger_id, kind, identity, version, started_at FROM paged_cursors WHERE chain = $1 FOR UPDATE`, chain).
		Scan(&owner.triggerID, &owner.kind, &owner.identity, &version, &startedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return owner, 0, time.Time{}, errCursorMoved
	}
	if err != nil {
		return owner, 0, time.Time{}, err
	}
	return owner, version, startedAt.UTC(), nil
}

// pageTx records a paged drain's resume row, whole.
func (t *txn) pageTx(owner pagedOwner, chain string, cursor any, version, pages, effects, bytes int64, startedAt time.Time) error {
	raw, err := json.Marshal(cursor)
	if err != nil {
		return fmt.Errorf("paged cursor: %w", err)
	}
	_, err = t.fold(foldOp{Kind: foldPage, Ref: typeTrigger, ID: owner.triggerID, Page: &foldPageRow{
		Chain: chain, Cursor: raw, Version: foldInt(version), Pages: foldInt(pages),
		Effects: foldInt(effects), Bytes: foldInt(bytes), StartedAt: startedAt.UTC(),
		Kind: owner.kind, Identity: owner.identity,
	}})
	return err
}

// unpageTx drops a paged drain's resume row.
func (t *txn) unpageTx(triggerID, chain string) error {
	_, err := t.fold(foldOp{Kind: foldUnpage, Ref: typeTrigger, ID: triggerID, Page: &foldPageRow{Chain: chain}})
	return err
}

// --- the entry ---

// lockChangelog holds the changelog's ordering lock, the first key of the
// global lock order (rows.go changelogLockKey). inTx takes it before the
// transaction does anything else, so inside one this is a no-op; appendChange
// and the motions that serialize against other deliveries (a paged claim,
// reserveSeq) still ask, so a transaction built outside inTx is ordered too.
func (t *txn) lockChangelog() error {
	if t.seqLocked {
		return nil
	}
	if err := t.lockKey(changelogLockKey); err != nil {
		return err
	}
	t.seqLocked = true
	return nil
}

// reserveSeq reports the seq the transaction's next append lands at, under
// the changelog lock so nothing can take it first. A fresh park's failure id
// is that seq (parkTx), folded before the delivery entry that carries it is
// appended; appendDeliveryAt then holds the entry to it.
func (t *txn) reserveSeq() (int64, error) {
	if err := t.lockChangelog(); err != nil {
		return 0, err
	}
	var head int64
	if err := t.row(`SELECT coalesce(max(seq), 0) FROM changelog`).Scan(&head); err != nil {
		return 0, err
	}
	return head + 1, nil
}

// settleDelivery appends the transaction's delivery entry when it has folded
// ledger effects not yet written into an entry: op `delivery`, addressed to
// the trigger, under the system actor. A transaction whose motions already
// rode an entry appends nothing.
func (t *txn) settleDelivery(triggerID string) error {
	if len(t.folded) == 0 {
		return nil
	}
	return t.appendChange(substrate.ActorSystem, substrate.OpDelivery, triggerID, typeTrigger, nil)
}

// appendDeliveryAt is settleDelivery for a transaction that reserved the
// entry's seq (reserveSeq): the entry must land there, or the failure id the
// park folded names an entry that does not exist.
func (t *txn) appendDeliveryAt(triggerID string, seq int64) error {
	if len(t.folded) == 0 {
		return fmt.Errorf("substrate/engine: delivery entry %d for %s has no effects to carry", seq, triggerID)
	}
	if err := t.settleDelivery(triggerID); err != nil {
		return err
	}
	if t.maxSeq != seq {
		return fmt.Errorf("substrate/engine: delivery entry for %s landed at %d, reserved %d", triggerID, t.maxSeq, seq)
	}
	return nil
}
