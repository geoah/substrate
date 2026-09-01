package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// eref is one record's full storage identity: the (type, id) pair every
// table keys on since the v1 re-key. An id alone names nothing.
type eref struct {
	Kind string
	ID   string
}

func (r eref) key() string { return r.Kind + "|" + r.ID }

// less orders erefs by (type, id) — the ONE ascending order every
// multi-record lock path uses. Within a type it
// is the ascending id order merge always took.
func (r eref) less(o eref) bool {
	if r.Kind != o.Kind {
		return r.Kind < o.Kind
	}
	return r.ID < o.ID
}

// erow is one stored records row.
type erow struct {
	ID         string
	Kind       string
	Title      string
	Body       string
	States     map[string]string
	At         *time.Time
	EndsAt     *time.Time
	DueAt      *time.Time
	Props      map[string]any
	Labels     map[string]any
	Version    int64
	CreatedAt  time.Time
	UpdatedAt  time.Time
	DeletedAt  *time.Time
	Finalizers []string
}

func (r *erow) ref() eref { return eref{Kind: r.Kind, ID: r.ID} }

// clone copies the row deeply enough to diff against: the write path mutates
// the row it loaded in place, so the delta the changelog carries is measured against
// this snapshot of what was stored (fold.go diffRow). Property VALUES are
// shared, never mutated in place — a write replaces them.
func (r *erow) clone() *erow {
	if r == nil {
		return nil
	}
	c := *r
	c.States = map[string]string{}
	for k, v := range r.States {
		c.States[k] = v
	}
	c.Props = map[string]any{}
	for k, v := range r.Props {
		c.Props[k] = v
	}
	c.Labels = map[string]any{}
	for k, v := range r.Labels {
		c.Labels[k] = v
	}
	c.Finalizers = append([]string{}, r.Finalizers...)
	return &c
}

const recordCols = `id, kind, title, body, states, at, ends_at, due_at, props, labels,
	version, created_at, updated_at, deleted_at, to_jsonb(finalizers)`

type scanner interface {
	Scan(dest ...any) error
}

// recordScan holds the scan destinations for one records row across the
// recordCols projection. It is factored out of scanRecord so a caller that
// SELECTs recordCols PLUS trailing columns (the keyset list appends the
// order-key expressions) can scan the whole row in one pass: dests() returns
// the record destinations, the caller appends its own, and finish() decodes.
type recordScan struct {
	r          erow
	states     []byte
	props      []byte
	labels     []byte
	finalizers []byte
}

func (es *recordScan) dests() []any {
	return []any{
		&es.r.ID, &es.r.Kind, &es.r.Title, &es.r.Body, &es.states, &es.r.At, &es.r.EndsAt, &es.r.DueAt,
		&es.props, &es.labels, &es.r.Version, &es.r.CreatedAt, &es.r.UpdatedAt, &es.r.DeletedAt, &es.finalizers,
	}
}

func (es *recordScan) finish() *erow {
	r := &es.r
	r.States = map[string]string{}
	r.Props = map[string]any{}
	r.Labels = map[string]any{}
	if len(es.states) > 0 {
		_ = json.Unmarshal(es.states, &r.States)
	}
	if len(es.props) > 0 {
		_ = json.Unmarshal(es.props, &r.Props)
	}
	if len(es.labels) > 0 {
		_ = json.Unmarshal(es.labels, &r.Labels)
	}
	r.Finalizers = append(r.Finalizers, jsonStrings(es.finalizers)...)
	r.CreatedAt = r.CreatedAt.UTC()
	r.UpdatedAt = r.UpdatedAt.UTC()
	return r
}

func scanRecord(sc scanner) (*erow, error) {
	var es recordScan
	if err := sc.Scan(es.dests()...); err != nil {
		return nil, err
	}
	return es.finish(), nil
}

// loadRow reads a record by its full (type, id) identity, optionally taking
// the row lock the write path serializes on.
func (t *txn) loadRow(ref eref, forUpdate bool) (*erow, error) {
	q := `SELECT ` + recordCols + ` FROM records WHERE kind = $1 AND id = $2`
	if forUpdate {
		q += ` FOR UPDATE`
	}
	r, err := scanRecord(t.row(q, ref.Kind, ref.ID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

func jsonb(v any) ([]byte, error) {
	if v == nil {
		return []byte("null"), nil
	}
	return json.Marshal(v)
}

// upsertRecord writes the row and reports whether anything actually changed.
// The DO UPDATE carries a WHERE so an identical re-write does not touch the
// tuple: no version bump, no updated_at motion, and (because the caller only
// logs when this returns true) no changelog row.
//
// Its ONE caller is the fold (fold.go foldRecordOp): a write path reaches the
// records table by describing what changed, never by writing the table.
func (t *txn) upsertRecord(r *erow, fts [3]string, force, resurrect bool) (changed, created bool, version int64, err error) {
	states, err := jsonb(r.States)
	if err != nil {
		return false, false, 0, err
	}
	props, err := jsonb(r.Props)
	if err != nil {
		return false, false, 0, err
	}
	labels, err := jsonb(r.Labels)
	if err != nil {
		return false, false, 0, err
	}
	finalizers, err := jsonb(nonNilStrings(r.Finalizers))
	if err != nil {
		return false, false, 0, err
	}
	row := t.row(`
		INSERT INTO records (
			id, kind, title, body, states, at, ends_at, due_at, props, labels,
			finalizers, fts, version, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9::jsonb, $10::jsonb,
			ARRAY(SELECT jsonb_array_elements_text($11::jsonb)),
			setweight(to_tsvector('english', $12), 'A') ||
			setweight(to_tsvector('english', $13), 'B') ||
			setweight(to_tsvector('english', $14), 'C'),
			1, $15, $15
		)
		ON CONFLICT (repository, kind, id) DO UPDATE SET
			title      = EXCLUDED.title,
			body       = EXCLUDED.body,
			states     = EXCLUDED.states,
			at         = EXCLUDED.at,
			ends_at    = EXCLUDED.ends_at,
			due_at     = EXCLUDED.due_at,
			props      = EXCLUDED.props,
			labels     = EXCLUDED.labels,
			finalizers = EXCLUDED.finalizers,
			fts        = EXCLUDED.fts,
			deleted_at = CASE WHEN $16::bool THEN NULL ELSE records.deleted_at END,
			version    = records.version + 1,
			updated_at = $15
		WHERE $17::bool
		   OR ($16::bool AND records.deleted_at IS NOT NULL)
		   OR records.title      IS DISTINCT FROM EXCLUDED.title
		   OR records.body       IS DISTINCT FROM EXCLUDED.body
		   OR records.states     IS DISTINCT FROM EXCLUDED.states
		   OR records.at         IS DISTINCT FROM EXCLUDED.at
		   OR records.ends_at    IS DISTINCT FROM EXCLUDED.ends_at
		   OR records.due_at     IS DISTINCT FROM EXCLUDED.due_at
		   OR records.props      IS DISTINCT FROM EXCLUDED.props
		   OR records.labels     IS DISTINCT FROM EXCLUDED.labels
		   OR records.finalizers IS DISTINCT FROM EXCLUDED.finalizers
		RETURNING version, (xmax = 0)`,
		r.ID, r.Kind, r.Title, r.Body, states, r.At, r.EndsAt, r.DueAt, props, labels,
		finalizers, fts[0], fts[1], fts[2], t.now, resurrect, force)
	err = row.Scan(&version, &created)
	if errors.Is(err, sql.ErrNoRows) {
		// The conflict fired and the WHERE excluded it: the stored row is
		// already exactly what we wanted to write.
		return false, false, r.Version, nil
	}
	if err != nil {
		return false, false, 0, err
	}
	return true, created, version, nil
}

func nonNilStrings(ss []string) []string {
	if ss == nil {
		return []string{}
	}
	return ss
}

// appendChange writes the one changelog row a committed change earns.
//
// changelogLockKey serializes seq allocation against commit order, PER REPOSITORY —
// v0 held one lock for the whole database, so every write in every repository
// queued behind every other. A sequence alone does not order commits either:
// two transactions can take seq 8 and 9 and commit in the other order, so a
// cursor-resumable reader that has seen 9 skips 8 forever. Holding one
// transaction-scoped lock from the first append until commit makes seq order
// and visibility order the same — the guarantee consumers resume on — and,
// because the lock is per repository, it also makes `seq` a per-repository
// gapless counter rather than a shared one with holes.
const changelogLockKey = "changelog"

// changeEntry is one appended changelog row's ADDRESS: the seq addresses the
// delta, kind and id address the record it moved. It is what an llmmessage's
// `changes` property stores per entry, so it carries no payload.
type changeEntry struct {
	seq  int64
	op   substrate.Op
	kind string
	id   string
}

// changeProps renders entries as the llmmessage `changes` property stores
// them (kinds/core.substrate.reamde.dev/llmmessage.yaml).
func changeProps(entries []changeEntry) []any {
	out := make([]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]any{
			"seq": e.seq, "op": string(e.op), "kind": e.kind, "id": e.id,
		})
	}
	return out
}

func (t *txn) appendChange(actor substrate.Actor, op substrate.Op, recordID, typ string, payload map[string]any) error {
	// The entry takes the effects the transaction folded since the previous
	// one: the payload IS the delta, and a rebuild replays it (fold.go). A
	// transaction that appends more than one entry slices its effect stream at
	// each append, so the effects stay in the order they happened even where an
	// inner write (a mapping shell, a recompute) borrows an outer one's.
	if len(t.folded) > 0 {
		if payload == nil {
			payload = map[string]any{}
		}
		payload[foldPayloadKey] = t.folded
		t.folded = nil
	}
	raw, err := jsonb(payload)
	if err != nil {
		return err
	}
	if !t.seqLocked {
		if err := t.lockKey(changelogLockKey); err != nil {
			return err
		}
		t.seqLocked = true
	}
	// caused_by is NULL on every write a function did not author, so the
	// causal-depth walk terminates on the first direct write.
	var causedBy sql.NullInt64
	if t.causedBy > 0 {
		causedBy = sql.NullInt64{Int64: t.causedBy, Valid: true}
	}
	// RETURNING payload::text hands back the payload AS STORED: jsonb
	// re-renders what Go sent (key order, number lexemes), and the chain
	// hashes what a verifier will read later, never the bytes that went in.
	var seq int64
	var stored []byte
	// The INSERT carries the all-zero signature and the transaction's
	// principal — the token id the door verified, empty where no token stands
	// behind the write. The zero never survives the transaction: settleChain
	// signs every pending entry at commit or refuses, and the store's
	// `changelog_sig_needs_hash` CHECK requires exactly this value while
	// `hash` is still NULL. The pending entry and the row must agree on every
	// hashed field, so both sides of this append stamp the same principal.
	if err := t.row(`
		INSERT INTO changelog (seq, ts, actor, principal, op, record_id, kind, payload, caused_by, sig)
		VALUES ((SELECT coalesce(max(seq), 0) + 1 FROM changelog), $1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9)
		RETURNING seq, payload::text`,
		t.now, string(actor), t.principal, string(op), recordID, typ, raw, causedBy, unsignedSig).Scan(&seq, &stored); err != nil {
		return err
	}
	if seq > t.maxSeq {
		t.maxSeq = seq
	}
	t.entries = append(t.entries, changeEntry{seq: seq, op: op, kind: typ, id: recordID})
	t.pending = append(t.pending, chainEntry{
		Seq: seq, TS: t.now, Actor: string(actor), Principal: t.principal,
		Op: string(op), RecordID: recordID, Kind: typ,
		CausedBy: causedBy.Int64, CausedByOK: causedBy.Valid,
		PayloadText: stored,
	})
	return nil
}

// tombstone soft-deletes a record: the delete verb's effect, GC's cascade and
// the merge loser's, which also lands a finalizer that holds GC off until a
// split puts it back.
func (t *txn) tombstone(ref eref, finalizer string) (bool, error) {
	res, err := t.fold(foldOp{Kind: foldTombstone, Ref: ref.Kind, ID: ref.ID, Finalizer: finalizer})
	return res.changed, err
}

func (t *txn) applyTombstone(ref eref, finalizer string) (bool, error) {
	q := `UPDATE records SET deleted_at = $3, version = version + 1, updated_at = $3`
	args := []any{ref.Kind, ref.ID, t.now}
	if finalizer != "" {
		q += `, finalizers = ARRAY(SELECT DISTINCT unnest(finalizers || ARRAY[$4::text]))`
		args = append(args, finalizer)
	}
	q += ` WHERE kind = $1 AND id = $2 AND deleted_at IS NULL`
	res, err := t.exec(q, args...)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (t *txn) applyBump(ref eref) (bool, error) {
	res, err := t.exec(`UPDATE records SET version = version + 1, updated_at = $3 WHERE kind = $1 AND id = $2`,
		ref.Kind, ref.ID, t.now)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// --- former ids (merge trails, proposal §6.3) ---
//
// A former id resolves WITHIN ITS TYPE: merge only ever joins two records of
// one type, so the trail row carries that type and a lookup names it.

// formerTarget returns the record a former id now denotes within one type,
// "" when the id is nobody's former id there.
func (t *txn) formerTarget(typ, formerID string) (string, error) {
	var id string
	err := t.row(`SELECT record_id FROM former_ids WHERE record_kind = $1 AND former_id = $2`,
		typ, formerID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// recordFormerID writes one merge trail entry, flattened: whatever the loser
// already carried is re-pointed at the winner by moveFormerIDs first, so this
// row is always a single hop.
func (t *txn) recordFormerID(typ, formerID, recordID string) error {
	_, err := t.fold(foldOp{Kind: foldFormerID, Ref: typ, ID: recordID, FormerID: formerID})
	return err
}

func (t *txn) applyFormerID(typ, formerID, recordID string) error {
	_, err := t.exec(`
		INSERT INTO former_ids (record_kind, former_id, record_id, created_at) VALUES ($1, $2, $3, $4)
		ON CONFLICT (repository, record_kind, former_id) DO UPDATE SET record_id = EXCLUDED.record_id`,
		typ, formerID, recordID, t.now)
	return err
}

// moveFormerIDs re-points the loser's own trail at the winner and reports what
// moved, so a split can put it back. Loser and winner share one type — merge
// refuses anything else.
func (t *txn) moveFormerIDs(typ, loserID, winnerID string) ([]string, error) {
	rows, err := t.query(`SELECT former_id FROM former_ids WHERE record_kind = $1 AND record_id = $2 ORDER BY former_id`,
		typ, loserID)
	if err != nil {
		return nil, err
	}
	var moved []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		moved = append(moved, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	if len(moved) == 0 {
		return nil, nil
	}
	_, err = t.exec(`UPDATE former_ids SET record_id = $3 WHERE record_kind = $1 AND record_id = $2`,
		typ, loserID, winnerID)
	return moved, err
}

// loadFormerIDs reads the ids merges fused into this record.
func loadFormerIDs(ctx context.Context, x dbx, ref eref) ([]string, error) {
	rows, err := x.QueryContext(ctx,
		`SELECT former_id FROM former_ids WHERE record_kind = $1 AND record_id = $2 ORDER BY former_id`,
		ref.Kind, ref.ID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var former string
		if err := rows.Scan(&former); err != nil {
			return nil, err
		}
		out = append(out, former)
	}
	return out, rows.Err()
}

// --- annotations ---

func (t *txn) putAnnotation(ref eref, key string, value any) (bool, error) {
	op := foldOp{Kind: foldAnnotation, Ref: ref.Kind, ID: ref.ID, Key: key}
	if value != nil {
		op.Value = &value
	}
	res, err := t.fold(op)
	return res.changed, err
}

func (t *txn) applyAnnotation(ref eref, key string, value any) (bool, error) {
	if value == nil {
		res, err := t.exec(`DELETE FROM annotations WHERE record_kind = $1 AND record_id = $2 AND key = $3`,
			ref.Kind, ref.ID, key)
		if err != nil {
			return false, err
		}
		n, _ := res.RowsAffected()
		return n > 0, nil
	}
	raw, err := jsonb(value)
	if err != nil {
		return false, err
	}
	var one int
	err = t.row(`
		INSERT INTO annotations (record_kind, record_id, key, value, updated_at)
		VALUES ($1, $2, $3, $4::jsonb, $5)
		ON CONFLICT (repository, record_kind, record_id, key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at
		WHERE annotations.value IS DISTINCT FROM EXCLUDED.value
		RETURNING 1`, ref.Kind, ref.ID, key, raw, t.now).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// --- property managers ---
//
// The ledger records which actor last had a change ACCEPTED on each property,
// at which TIER that write stood (primitives README §6), and which PRINCIPAL
// — the token id the door verified — stood behind it. It is
// attribution on every direct write, and it is load-bearing:
// mapping recompute yields to any manager row above the machine tier, which
// is how a hand edit — the owner's or a function's — survives a sync,
// visibly. Nothing else reads it to decide who may write — anyone still
// overwrites anything.

// actorTier resolves an actor's manager tier from DATA, never from the
// actor's spelling: the three human DOORS — api, console, substratectl — are
// the owner tier; the engine's own actor is machine; and every actor the
// registry answers for carries its declared tier — an authority-declared
// `bundle:<authority>` actor defaults machine, a registered function's or
// agent's own actor is bundle.
//
// An actor the registry does not answer for splits two ways. A RESERVED name
// (the `substrate` namespace, `bundle:`, `function:`, `agent:`, and the
// retired `connector:` spelling every repository written before record 0025
// still carries) is one of
// the substrate's own writing hands, which a request may never claim
// (api/auth.go) — an undeclared one is a hand whose declaration is gone, a
// facility like `substrate.oauth`, or a dispatch mid-uninstall, and none of
// those is the owner's edit, so it holds at the machine tier and recompute
// may replace it. Anything else is a name a REQUEST asserted at the door, and
// a token has full access to its repository: that write stands exactly where
// the token stands, the owner tier, whatever door name it chose. Nothing here
// escalates — the same caller can send `api` — and the entry's principal
// records which token it was. The split is FORWARD-ONLY: a manager row an
// undeclared reserved actor already holds at the owner tier keeps it, here
// and through a rebuild, because the fold replays the tier the write
// recorded, not the tier this function would resolve today.
//
// Direct writes read their tier off the transaction (txn.tier,
// set once at inTx and set to bundle explicitly by function/agent
// dispatch); recompute's own rows are always machine, whatever actor they
// credit (mapping.go). This function is also the facility seams'
// (StartOAuth's owner-gate) resolution, outside any transaction.
func (ds *dataset) actorTier(actor substrate.Actor) substrate.Tier {
	switch {
	case substrate.HumanActors[actor]:
		return substrate.TierOwner
	// The engine's own hand is resolved before the registry, so a declaration
	// cannot move it: `tier:` is data an installed bundle writes.
	case actor == substrate.ActorSystem:
		return substrate.TierMachine
	}
	if tier, ok := ds.registry().ActorTier(string(actor)); ok {
		return tier
	}
	if substrate.ReservedActor(actor) {
		return substrate.TierMachine
	}
	return substrate.TierOwner
}

func (t *txn) setManager(ref eref, property string, actor substrate.Actor, tier substrate.Tier) error {
	_, err := t.fold(foldOp{
		Kind: foldManager, Ref: ref.Kind, ID: ref.ID,
		Property: property, Actor: string(actor), Tier: string(tier),
		// The effect carries the principal so a replay reproduces the row
		// without reading the entry around it — the fold describes what the
		// write did, in full.
		Principal: t.principal,
	})
	return err
}

// deleteManager clears one property's manager row: a deleted property is
// nobody's, so the next recompute may refill it. An
// empty actor is the release on the wire too.
func (t *txn) deleteManager(ref eref, property string) error {
	_, err := t.fold(foldOp{Kind: foldManager, Ref: ref.Kind, ID: ref.ID, Property: property})
	return err
}

func (t *txn) applyManager(ref eref, property string, actor substrate.Actor, tier substrate.Tier, principal string) (bool, error) {
	if actor == "" {
		res, err := t.exec(`DELETE FROM property_managers WHERE record_kind = $1 AND record_id = $2 AND property = $3`,
			ref.Kind, ref.ID, property)
		if err != nil {
			return false, err
		}
		n, err := res.RowsAffected()
		return n > 0, err
	}
	// A different principal is a different write, so it moves the row: the
	// ledger names the token behind the last accepted change, not merely the
	// door it came through.
	res, err := t.exec(`
		INSERT INTO property_managers (record_kind, record_id, property, actor, tier, principal, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (repository, record_kind, record_id, property) DO UPDATE SET
			actor = EXCLUDED.actor, tier = EXCLUDED.tier,
			principal = EXCLUDED.principal, updated_at = EXCLUDED.updated_at
		WHERE property_managers.actor     IS DISTINCT FROM EXCLUDED.actor
		   OR property_managers.tier      IS DISTINCT FROM EXCLUDED.tier
		   OR property_managers.principal IS DISTINCT FROM EXCLUDED.principal`,
		ref.Kind, ref.ID, property, string(actor), string(tier), principal, t.now)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// --- embed queue ---

func (t *txn) enqueueEmbed(ref eref, property string) error {
	// A re-enqueue BUMPS the generation rather than doing nothing: the property's
	// text changed, so a worker already embedding the previous text must not be
	// allowed to write its now-stale vectors and drop the row (search.go
	// ProcessEmbedQueue). The bump is the signal it lost the race.
	_, err := t.exec(`
		INSERT INTO embed_queue (record_kind, record_id, property, generation, enqueued_at)
		VALUES ($1, $2, $3, 1, $4)
		ON CONFLICT (repository, record_kind, record_id, property) DO UPDATE
		    SET generation = embed_queue.generation + 1, enqueued_at = EXCLUDED.enqueued_at`,
		ref.Kind, ref.ID, property, t.now)
	return err
}

// hardDelete removes a record and every record that hangs off it.
func (t *txn) hardDelete(ref eref) error {
	_, err := t.fold(foldOp{Kind: foldPurge, Ref: ref.Kind, ID: ref.ID})
	return err
}

func (t *txn) applyPurge(ref eref) error {
	for _, q := range []string{
		// The record's OWN rows, and only those. The index is derived from the
		// SOURCE record's properties, so deleting by `dst` would erase rows
		// belonging to records that still name this one — and a rebuild, which
		// re-derives from those properties, would put them straight back. A
		// pointer at a purged record dangles; that is what an absent
		// `onDelete:` means.
		`DELETE FROM refs WHERE src_kind = $1 AND src = $2`,
		`DELETE FROM former_ids WHERE record_kind = $1 AND (record_id = $2 OR former_id = $2)`,
		`DELETE FROM annotations WHERE record_kind = $1 AND record_id = $2`,
		`DELETE FROM property_managers WHERE record_kind = $1 AND record_id = $2`,
		`DELETE FROM property_offers WHERE record_kind = $1 AND record_id = $2`,
		`DELETE FROM embeddings WHERE record_kind = $1 AND record_id = $2`,
		`DELETE FROM embed_queue WHERE record_kind = $1 AND record_id = $2`,
		`DELETE FROM records WHERE kind = $1 AND id = $2`,
	} {
		if _, err := t.exec(q, ref.Kind, ref.ID); err != nil {
			return fmt.Errorf("substrate/engine: hard delete %s %s: %w", ref.Kind, ref.ID, err)
		}
	}
	return nil
}
