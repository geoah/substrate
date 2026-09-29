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
// alone: the cursor a record trigger has reached (`trigger_cursors`), the
// occurrence a schedule trigger last fired (`trigger_schedule`), the failures
// it parked (`trigger_failures`) and the resume row of a paged drain
// (`paged_cursors`) must survive a repository directory imported into an
// empty database.
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
// One column is carried whole only where a restore needs it: a paged drain's
// cursor (decision 0141). A provider cursor runs to hundreds of kilobytes and
// a drain writes one per page, so a middle page's `page` effect names the
// cursor by its SHA-256 and byte size and the bytes stay in `paged_cursors`.
// A park re-states the chain's row with the cursor whole (parkTx), because a
// parked failure is the handle a retry resumes from and a park is rare where
// pages are not. A replay therefore brings a parked drain back at its last
// committed page. A drain that stopped between pages without parking (a
// crash, a shutdown) comes back at its page from a rebuild, which keeps the
// cursor the table held when its digest is the one the entry names, and with
// no position from an import into an empty database: its next delivery starts
// the body over from its first page under a fresh budget.
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
		return true, t.upsertPage(op.ID, op.Page)
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

// pageUpsert writes a resume row whole; %s is the expression the cursor
// column takes.
const pageUpsert = `
	INSERT INTO paged_cursors (chain, cursor, pages, version, effects, bytes, started_at, trigger_id, kind, identity, updated_at)
	VALUES ($1, %s, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	ON CONFLICT (repository, chain) DO UPDATE
	SET cursor = EXCLUDED.cursor, pages = EXCLUDED.pages, version = EXCLUDED.version,
	    effects = EXCLUDED.effects, bytes = EXCLUDED.bytes, started_at = EXCLUDED.started_at,
	    trigger_id = EXCLUDED.trigger_id, kind = EXCLUDED.kind, identity = EXCLUDED.identity,
	    updated_at = EXCLUDED.updated_at`

// cursorDigestSQL is the digest a page entry names a cursor by (decision
// 0141): the hex SHA-256 of the stored jsonb as Postgres prints it, so the
// entry can be checked against the table with SQL alone and a jsonb value
// read back names itself the same way. cursorLengthSQL is that text's length
// in bytes.
func cursorDigestSQL(column string) string {
	return `encode(sha256(convert_to(` + column + `::text, 'UTF8')), 'hex')`
}

func cursorLengthSQL(column string) string {
	return `octet_length(convert_to(` + column + `::text, 'UTF8'))`
}

// upsertPage writes a page effect's resume row. The cursor is the bytes a
// live middle page staged, else the cursor the entry carries whole, else, on
// a replay that kept the table's cursors (rebuild.go keepPagedCursors), the
// kept cursor of the chain whose digest the entry names, else JSON null.
//
// A live middle page reads the digest and length of what the table stored
// back onto the row, and p is the row of the effect fold records, so the
// entry names exactly the stored cursor.
func (t *txn) upsertPage(triggerID string, p *foldPageRow) error {
	args := []any{
		p.Chain, nil, int64(p.Pages), int64(p.Version), int64(p.Effects), int64(p.Bytes),
		p.StartedAt.UTC(), triggerID, p.Kind, p.Identity, t.now,
	}
	switch {
	case len(p.staged) > 0:
		args[1] = []byte(p.staged)
		var n int64
		if err := t.row(fmt.Sprintf(pageUpsert, `$2::jsonb`)+`
			RETURNING `+cursorDigestSQL("paged_cursors.cursor")+`, `+cursorLengthSQL("paged_cursors.cursor"), args...).
			Scan(&p.CursorSHA256, &n); err != nil {
			return err
		}
		p.CursorBytes = foldInt(n)
		return nil
	case len(p.Cursor) > 0:
		args[1] = []byte(p.Cursor)
	case t.keptCursors && p.CursorSHA256 != "":
		args[1] = p.CursorSHA256
		_, err := t.exec(fmt.Sprintf(pageUpsert, `coalesce(
			(SELECT k.cursor FROM pg_temp.substrate_kept_cursors k WHERE k.chain = $1 AND k.digest = $2),
			'null'::jsonb)`), args...)
		return err
	default:
		args[1] = []byte(`null`)
	}
	_, err := t.exec(fmt.Sprintf(pageUpsert, `$2::jsonb`), args...)
	return err
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
// The same entry re-states the resume row of the paged chain the failure
// names, cursor included (checkpointPagedCursor).
func (t *txn) parkTx(triggerID string, f foldFailure) error {
	if _, err := t.fold(foldOp{Kind: foldPark, Ref: typeTrigger, ID: triggerID, Failure: &f}); err != nil {
		return err
	}
	return t.checkpointPagedCursor(triggerID, f)
}

// checkpointPagedCursor re-states, cursor whole, the resume row of the paged
// chain a parked failure names. A middle page names its cursor by hash alone
// (pageTx), so without this a restore would bring a parked drain back with
// no position and its retry would start the body over. A failure whose
// delivery holds no resume row re-states nothing, which is every park but a
// paged drain's.
func (t *txn) checkpointPagedCursor(triggerID string, f foldFailure) error {
	var chain string
	switch {
	case f.FireID != "":
		chain = t.ds.fireChainKey(triggerID, f.FireID)
	case f.Seq > 0:
		chain = t.ds.recordChainKey(triggerID, int64(f.Seq))
	default:
		return nil
	}
	if err := t.lockChangelog(); err != nil {
		return err
	}
	row := foldPageRow{Chain: chain}
	var cursor []byte
	var version, pages, effects, bytes int64
	err := t.row(`
		SELECT cursor, version, pages, effects, bytes, started_at, kind, identity
		FROM paged_cursors WHERE chain = $1 AND trigger_id = $2 FOR UPDATE`, chain, triggerID).
		Scan(&cursor, &version, &pages, &effects, &bytes, &row.StartedAt, &row.Kind, &row.Identity)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	row.Cursor = cursor
	row.Version, row.Pages, row.Effects, row.Bytes = foldInt(version), foldInt(pages), foldInt(effects), foldInt(bytes)
	row.StartedAt = row.StartedAt.UTC()
	_, err = t.fold(foldOp{Kind: foldPage, Ref: typeTrigger, ID: triggerID, Page: &row})
	return err
}

// inFlightError is the error a claim carries (settlement.claim): a parked
// failure row that stands while an agent delivery runs. A claim a stopped
// process left behind is rewritten to interruptedAgentError at the next open
// (settleInterruptedAgentRuns), so a stored claim is a run this writer
// started; one this process no longer holds in runningClaims reads as
// interrupted in the listing (TriggerFailures).
const inFlightError = "delivery in flight: an agent run is running it now"

// legacyInFlightError is the text a claim carried before inFlightError was
// reworded. Only the open-time sweep reads it: a claim written by an earlier
// binary is an interrupted run by construction, and the sweep rewrites it.
const legacyInFlightError = "delivery in flight: an agent run a restart interrupted stays here, retried by hand"

// interruptedAgentError is the error an agent delivery's claim carries once
// its run is known to be dead. Nothing reruns it by itself (decision 0064):
// the run may have spent tokens and written records, so a person decides.
const interruptedAgentError = "interrupted: the server stopped during this agent run, and nothing reruns it by itself; " +
	"read its thread, then retry this delivery to run the agent again, or forget it"

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
// startedAt is the drain's: the row's own, except for a chain the drain
// started over (functions.go loadPagedProgress), whose budget restarts.
func (t *txn) advancePagedCursor(chain string, version int64, cursor any, pages, effects, bytes int64, startedAt time.Time) error {
	owner, have, err := t.lockPagedCursor(chain)
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
	owner, have, err := t.lockPagedCursor(chain)
	if err != nil {
		return err
	}
	if have != version {
		return errCursorMoved
	}
	return t.unpageTx(owner.triggerID, chain)
}

// lockPagedCursor reads a chain's row FOR UPDATE: its owner and version. An
// absent row is errCursorMoved, since every caller owns a row.
func (t *txn) lockPagedCursor(chain string) (pagedOwner, int64, error) {
	var owner pagedOwner
	var version int64
	if err := t.lockChangelog(); err != nil {
		return owner, 0, err
	}
	err := t.row(`
		SELECT trigger_id, kind, identity, version FROM paged_cursors WHERE chain = $1 FOR UPDATE`, chain).
		Scan(&owner.triggerID, &owner.kind, &owner.identity, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return owner, 0, errCursorMoved
	}
	if err != nil {
		return owner, 0, err
	}
	return owner, version, nil
}

// pageTx records a middle page's resume row: whole in paged_cursors, and on
// the delivery entry with the cursor named by the SHA-256 and byte length of
// the stored value rather than carried (decision 0141, upsertPage). The entry
// stays a few hundred bytes however large the cursor grows.
func (t *txn) pageTx(owner pagedOwner, chain string, cursor any, version, pages, effects, bytes int64, startedAt time.Time) error {
	raw, err := json.Marshal(cursor)
	if err != nil {
		return fmt.Errorf("paged cursor: %w", err)
	}
	_, err = t.fold(foldOp{Kind: foldPage, Ref: typeTrigger, ID: owner.triggerID, Page: &foldPageRow{
		Chain: chain, staged: raw,
		Version: foldInt(version), Pages: foldInt(pages),
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
