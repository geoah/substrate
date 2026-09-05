package engine

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// THE FOLD.
//
// The changelog is the truth and the records table is a fold of it. This file is
// that fold, and it is the ONLY way anything in the engine writes the folded
// tables: `foldOne` applies one effect, `foldEntry` applies a whole changelog
// entry's worth of them, and every write path reaches the tables through
// `txn.fold`, which applies the effect AND records it so the entry the write
// appends carries exactly what it did.
//
// That is what makes the payload replayable. A v0 entry named the properties
// that moved and nothing else, so a rebuild could learn WHAT changed and never
// WHAT IT BECAME; an entry now carries the delta WITH ITS VALUES, and
// `rebuild-repository` clears the fold and drives the same `foldOne` from the
// changelog. There is no second implementation of "what a change does to the records
// table" to drift from this one.
//
// The fold is deliberately SCHEMA-FREE about data: every decision the schema
// makes — coercion, defaults, display templates, mapping, sealing — happened
// at write time and its RESULT is in the delta. The registry is consulted for
// exactly one thing, the weighted search bands, because `fts` is an INDEX over
// the folded row rather than part of it (foldFTS).

// foldKind names one kind of effect. The values are wire values: they land in
// the changelog's payload and a rebuild reads them back.
type foldKind string

const (
	// foldRecord applies a record delta — the create/update half of the fold.
	foldRecord foldKind = "record"
	// foldTombstone soft-deletes a record (delete, gc, the merge loser).
	foldTombstone foldKind = "tombstone"
	// foldPurge hard-deletes a record and everything hanging off it (gc).
	foldPurge foldKind = "purge"
	// foldBump moves a record's version without touching its columns: an
	// annotation-only write, whose change is beside the row.
	foldBump foldKind = "bump"

	foldAnnotation foldKind = "annotation"
	foldManager    foldKind = "manager"
	foldFormerID   foldKind = "former"

	// foldResync re-states every side-store row hanging off a named set of
	// records: merge's and split's effect, the one snapshot among the deltas
	// (see "the resync effect" below).
	foldResync foldKind = "resync"
)

// rowDelta is one record's change as VALUES: what its columns became, against
// what they were. Properties travel as a set/delete pair because a record's
// property map is the big one; the small columns travel whole, because a delta
// of a five-key states map costs more to describe than to carry.
//
// The delta is derived mechanically (diffRow) from the row the write path
// loaded and the row it produced, so `applyTo(before) == after` holds by
// construction — the fold cannot reconstruct something the writer did not
// mean.
type rowDelta struct {
	// Created marks the entry that brought the record into being, Restored the
	// one that lifted a tombstone, Force an upsert that must touch the row even
	// when no column moved (its change was an annotation).
	Created  bool `json:"created,omitempty"`
	Restored bool `json:"restored,omitempty"`
	Force    bool `json:"force,omitempty"`

	Set map[string]any `json:"set,omitempty"`
	Del []string       `json:"del,omitempty"`

	Title *string `json:"title,omitempty"`
	Body  *string `json:"body,omitempty"`
	// The three capability-backed time columns. A pointer to the empty string
	// is the cleared form — JSON has one null and it already means "absent".
	At     *string `json:"at,omitempty"`
	EndsAt *string `json:"endsAt,omitempty"`
	DueAt  *string `json:"dueAt,omitempty"`

	States     map[string]string `json:"states,omitempty"`
	Labels     map[string]any    `json:"labels,omitempty"`
	Finalizers *[]string         `json:"finalizers,omitempty"`
}

// foldOp is one effect: a fold kind, the record REFERENCE it lands on, and
// the fields that fold kind reads. It is a union on purpose — the changelog carries these by the
// thousand, and `omitempty` on a flat struct is the cheapest honest encoding.
type foldOp struct {
	Kind foldKind `json:"kind"`
	Ref  string   `json:"ref,omitempty"`
	ID   string   `json:"id,omitempty"`

	Delta *rowDelta `json:"delta,omitempty"`

	// Finalizer is the hold a tombstone adds as it lands (merge's).
	Finalizer string `json:"finalizer,omitempty"`

	// Key/Value carry an annotation. Value is a POINTER because an annotation
	// may legitimately be `false`, `0` or `""`: an absent value has to mean the
	// deletion and nothing else, and `omitempty` on a bare `any` would swallow
	// those three into it.
	Key   string `json:"key,omitempty"`
	Value *any   `json:"value,omitempty"`

	// Property/Actor/Tier/Principal carry a manager row; an empty Actor is the
	// release. Principal is the token id behind the write, empty where no
	// token stood behind it and on every effect written before #102.
	Property  string `json:"property,omitempty"`
	Actor     string `json:"actor,omitempty"`
	Tier      string `json:"tier,omitempty"`
	Principal string `json:"principal,omitempty"`

	FormerID string `json:"formerId,omitempty"`

	// Scope and Rows carry a resync: the records whose side-store rows the
	// effect re-states wholly, and the rows they hold once it has.
	Scope []foldRef `json:"scope,omitempty"`
	Rows  *foldRows `json:"rows,omitempty"`
}

func (op foldOp) ref() eref { return eref{Kind: op.Ref, ID: op.ID} }

// foldResult is what applying one effect produced: whether it moved anything
// (a no-op effect is never recorded, so an entry never claims a change it did
// not make) and, for a record effect, the row that landed.
type foldResult struct {
	changed bool
	created bool
	row     *erow
}

// fold applies one effect and records it on the transaction. Every write path
// in the engine goes through here; `appendChange` drains what accumulated into
// the entry it writes, so the entry and the tables can never disagree.
func (t *txn) fold(op foldOp) (foldResult, error) {
	res, err := t.foldOne(op)
	if err != nil || !res.changed {
		return res, err
	}
	t.folded = append(t.folded, op)
	return res, nil
}

// settleFold closes the transaction's books: an effect applied AFTER the last
// entry was appended (GC hard-deletes after it logs the collection, merge
// records its former-id trail after it logs the merge) still belongs to that
// entry, or the changelog would describe a fold it did not produce. It joins the
// last entry's effects, in order, before the commit.
//
// Effects with no entry at all are the one case that cannot be repaired here —
// there is nothing to attach them to — so they are reported rather than
// swallowed: the fold and the changelog must not diverge quietly.
func (t *txn) settleFold() error {
	if len(t.folded) == 0 {
		return nil
	}
	ops := t.folded
	t.folded = nil
	if t.maxSeq == 0 {
		// A fold with no entry to attach it to cannot be repaired — and it is a
		// rebuild-divergence crack, not a curiosity: the records table now holds
		// a change the changelog will never reproduce. Every legitimate write folds AND
		// appends in the same transaction (linkSubject, the put path, recompute's
		// patch, GC and merge all changelog the change they fold), so this is a bug in a
		// new write path, and the transaction ROLLS BACK rather than committing a
		// fold the rebuild would silently drop.
		return fmt.Errorf("substrate/engine: transaction changed the fold (%d effects) without appending a changelog entry — the rebuild would not reproduce it (repository %s)",
			len(ops), t.ds.scope.Repository)
	}
	raw, err := json.Marshal(ops)
	if err != nil {
		return err
	}
	// RETURNING payload::text replaces the pending entry's payload with the
	// merged one AS STORED: the checksum stamped at settleChecksums must
	// cover what this update just made the entry say.
	var stored []byte
	if err := t.row(`
		UPDATE changelog
		SET payload = jsonb_set(payload, '{`+foldPayloadKey+`}',
			coalesce(payload->'`+foldPayloadKey+`', '[]'::jsonb) || $2::jsonb)
		WHERE seq = $1
		RETURNING payload::text`, t.maxSeq, raw).Scan(&stored); err != nil {
		return err
	}
	if n := len(t.pending); n == 0 || t.pending[n-1].Seq != t.maxSeq {
		// Only the transaction's LAST appended entry may take late effects; a
		// settle that would touch anything else is a bug in a write path, and
		// it rolls back rather than committing a hash over the wrong bytes.
		return fmt.Errorf("substrate/engine: settleFold touched seq %d, which is not this transaction's last appended entry", t.maxSeq)
	}
	t.pending[len(t.pending)-1].PayloadText = stored
	return nil
}

// foldOne applies ONE effect to the fold tables. This is the fold: a live
// write reaches it through `fold` above, a rebuild through `foldEntry` below,
// and there is no third caller.
func (t *txn) foldOne(op foldOp) (foldResult, error) {
	switch op.Kind {
	case foldRecord:
		return t.foldRecordOp(op)
	case foldTombstone:
		changed, err := t.applyTombstone(op.ref(), op.Finalizer)
		return foldResult{changed: changed}, err
	case foldPurge:
		if err := t.applyPurge(op.ref()); err != nil {
			return foldResult{}, err
		}
		return foldResult{changed: true}, nil
	case foldBump:
		changed, err := t.applyBump(op.ref())
		return foldResult{changed: changed}, err
	case foldAnnotation:
		var value any
		if op.Value != nil {
			value = *op.Value
		}
		changed, err := t.applyAnnotation(op.ref(), op.Key, value)
		return foldResult{changed: changed}, err
	case foldManager:
		changed, err := t.applyManager(op.ref(), op.Property, substrate.Actor(op.Actor), substrate.Tier(op.Tier), op.Principal)
		return foldResult{changed: changed}, err
	case foldFormerID:
		if err := t.applyFormerID(op.Ref, op.FormerID, op.ID); err != nil {
			return foldResult{}, err
		}
		return foldResult{changed: true}, nil
	case foldResync:
		if err := t.applyResync(op); err != nil {
			return foldResult{}, err
		}
		return foldResult{changed: true}, nil
	}
	return foldResult{}, fmt.Errorf("substrate/engine: the fold does not know the effect %q", op.Kind)
}

// foldRow folds the difference between the row a write loaded and the row it
// produced. It is the write path's door to the records table: nothing else may
// name it, so every record change is described before it is made.
func (t *txn) foldRow(before, after *erow, force, resurrect bool) (foldResult, error) {
	d := diffRow(before, after)
	d.Force = force
	d.Restored = resurrect
	return t.fold(foldOp{Kind: foldRecord, Ref: after.Kind, ID: after.ID, Delta: d})
}

// foldRecordOp is the create/update half: read what the record is, apply the
// delta's values, write what it became. Reading first is what makes the fold a
// FOLD — the same delta on the same prior state gives the same row, whether it
// is being written now or replayed later.
func (t *txn) foldRecordOp(op foldOp) (foldResult, error) {
	if op.Delta == nil {
		return foldResult{}, fmt.Errorf("substrate/engine: a record effect on %s %s carries no delta", op.Kind, op.ID)
	}
	ref := op.ref()
	row, err := t.loadRow(ref, false)
	if err != nil {
		return foldResult{}, err
	}
	if row == nil {
		row = &erow{
			ID: ref.ID, Kind: ref.Kind,
			States: map[string]string{}, Props: map[string]any{}, Labels: map[string]any{},
			CreatedAt: t.now, UpdatedAt: t.now,
		}
	}
	op.Delta.applyTo(row)
	changed, created, version, err := t.upsertRecord(row, t.foldFTS(row), op.Delta.Force, op.Delta.Restored)
	if err != nil {
		return foldResult{}, err
	}
	row.Version = version
	if changed {
		row.UpdatedAt = t.now
		if op.Delta.Restored {
			row.DeletedAt = nil
		}
		// The refs index is a PROJECTION of the row that just landed, like
		// `fts` and unlike everything else here: derived from the folded
		// properties and the kind's declaration (refs.go), never from an effect
		// of its own. Deriving it HERE is what makes a rebuild reproduce it
		// byte for byte — every column is a function of those two inputs, and
		// no column of it reads the clock.
		//
		// A KIND WITH NO REFERENCE SITE IS SKIPPED. It derives no rows from any
		// value, so the delete-and-re-derive would be two statements per write
		// that can only ever find nothing — paid on the hot sync paths (mail,
		// calendar), whose kinds point at nothing. A kind this binary no longer
		// DECLARES is not skipped: its stored rows project a declaration that is
		// gone, and syncRefs is what removes them. A declaration that stops
		// carrying a reference is covered by reprojectRefs, which re-derives
		// every record of the kinds whose reference shape moved.
		ty, _ := t.declarations().ByIdentity(row.Kind)
		if ty == nil || declaresReference(ty) {
			if err := t.syncRefs(ref, ty, row.Props); err != nil {
				return foldResult{}, err
			}
		}
	}
	return foldResult{changed: changed, created: created, row: row}, nil
}

// foldFTS computes the row's weighted search bands. `fts` is an INDEX over the
// folded row, not fold data: it is the one place the fold consults the
// registry, and a record whose kind the binary no longer declares still
// indexes its title and body rather than failing the replay.
func (t *txn) foldFTS(row *erow) [3]string {
	if t.writeReg != nil {
		if ty, ok := t.writeReg.ByIdentity(row.Kind); ok {
			return ftsBands(ty, row)
		}
	}
	if ty, ok := t.ds.registry().ByIdentity(row.Kind); ok {
		return ftsBands(ty, row)
	}
	// No declaration to consult (a kind this binary no longer declares), so no
	// `body` property `fts` flag to read. Index the stored body: it is the safe
	// default for an unknown row, and it keeps a legacy body searchable rather
	// than dropping it out of the index on a binary that forgot its kind (#68).
	return [3]string{row.Title, "", row.Body}
}

// --- the resync effect (merge and split) ---
//
// Merge and split do not move one row at a time. They REWRITE the side stores
// around the pair they join — every annotation, manager row and former-id trail
// hanging off either — with set-shaped statements, and the entry they wrote
// carried the moved SETS, which exist for split's undo and describe the
// operation in reverse. A fold cannot replay a reverse set, so a repository
// that had ever merged used to rebuild to a refusal.
//
// The resync effect closes that. Instead of describing each row it moved, a
// merge names a SCOPE of records and carries the side-store rows that hold
// AFTER the rewrite. Replaying it deletes every row keyed on a scope record —
// the same statements a purge uses — and writes the recorded ones back, so the
// stores land exactly where the merge left them whatever route the live
// statements took to get there.
//
// THE SCOPE IS THE CONTRACT. Every row the rewrite touches must be keyed on a
// record in the scope, or the replay will neither delete nor restate it.
//
// The refs index is NOT in it, and needs no effect of its own: it is derived
// from the source records' own properties (refs.go), which travel as deltas, so
// a replay that reproduces the properties reproduces the index.
//
// It is the ONE effect that is a snapshot rather than a delta, and it is the
// only one RECORDED WITHOUT BEING APPLIED: the capture reads the tables, so
// the live path already holds what it describes, and applying it there would
// delete and rewrite rows to reach the state they are in. It stays bounded
// because merge is a manual verb on two records, not a sweep.

// foldRef names one record a resync re-states: an `eref` with a wire shape, and
// convertible from one — the two must keep the same fields for that to hold.
type foldRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// foldRows is the after-state a resync writes back: every side-store row, with
// every column, so a replay reproduces the rows and not merely their shape.
type foldRows struct {
	Annotations []foldAnnotationRow `json:"annotations,omitempty"`
	Managers    []foldManagerRow    `json:"managers,omitempty"`
	FormerIDs   []foldFormerRow     `json:"formerIds,omitempty"`
}

// The timestamp travels because it is the row: an annotation's `updated_at` is
// what the live write stamped, and a replay under its own clock would reproduce
// the value and not the row.
type foldAnnotationRow struct {
	Kind      string          `json:"kind"`
	ID        string          `json:"id"`
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value,omitempty"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

type foldManagerRow struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Property string `json:"property"`
	Actor    string `json:"actor"`
	Tier     string `json:"tier"`
	// Principal is the token id the row's write stood behind, empty where
	// none did and on every payload written before #102 — the same empty the
	// column's migration stamped on the rows it found, so replaying old
	// history reproduces them.
	Principal string    `json:"principal,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type foldFormerRow struct {
	Kind      string    `json:"kind"`
	FormerID  string    `json:"formerId"`
	RecordID  string    `json:"recordId"`
	CreatedAt time.Time `json:"createdAt"`
}

// recordResync captures the side-store rows the scope holds NOW and records
// the effect on the transaction without applying it. Its one caller pair is
// merge and split, immediately before they append their entry, so the effect
// rides on the entry whose operation it describes.
func (t *txn) recordResync(scope []eref) error {
	op, err := t.resyncOf(scope)
	if err != nil {
		return err
	}
	t.folded = append(t.folded, op)
	return nil
}

// resyncOf reads the scope's whole side-store state into one effect. Rows are
// read in a fixed order and de-duplicated, because two scope records share the
// rows between them and a former-id row can match both ends.
func (t *txn) resyncOf(scope []eref) (foldOp, error) {
	op := foldOp{Kind: foldResync, Rows: &foldRows{}}
	seen := map[eref]bool{}
	refs := make([]eref, 0, len(scope))
	for _, ref := range scope {
		if ref.Kind == "" || ref.ID == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		refs = append(refs, ref)
		op.Scope = append(op.Scope, foldRef(ref))
	}
	formerSeen := map[[2]string]bool{}
	for _, ref := range refs {
		if err := t.scanInto(`
			SELECT key, value, updated_at FROM annotations
			WHERE record_kind = $1 AND record_id = $2 ORDER BY key`, ref, func(rows *sql.Rows) error {
			a := foldAnnotationRow{Kind: ref.Kind, ID: ref.ID}
			var value []byte
			if err := rows.Scan(&a.Key, &value, &a.UpdatedAt); err != nil {
				return err
			}
			a.Value, a.UpdatedAt = json.RawMessage(value), a.UpdatedAt.UTC()
			op.Rows.Annotations = append(op.Rows.Annotations, a)
			return nil
		}); err != nil {
			return foldOp{}, err
		}
		if err := t.scanInto(`
			SELECT property, actor, tier, principal, updated_at FROM property_managers
			WHERE record_kind = $1 AND record_id = $2 ORDER BY property`, ref, func(rows *sql.Rows) error {
			m := foldManagerRow{Kind: ref.Kind, ID: ref.ID}
			if err := rows.Scan(&m.Property, &m.Actor, &m.Tier, &m.Principal, &m.UpdatedAt); err != nil {
				return err
			}
			m.UpdatedAt = m.UpdatedAt.UTC()
			op.Rows.Managers = append(op.Rows.Managers, m)
			return nil
		}); err != nil {
			return foldOp{}, err
		}
		if err := t.scanInto(`
			SELECT former_id, record_id, created_at FROM former_ids
			WHERE record_kind = $1 AND (record_id = $2 OR former_id = $2) ORDER BY former_id`, ref,
			func(rows *sql.Rows) error {
				f := foldFormerRow{Kind: ref.Kind}
				if err := rows.Scan(&f.FormerID, &f.RecordID, &f.CreatedAt); err != nil {
					return err
				}
				key := [2]string{f.Kind, f.FormerID}
				if formerSeen[key] {
					return nil
				}
				formerSeen[key] = true
				f.CreatedAt = f.CreatedAt.UTC()
				op.Rows.FormerIDs = append(op.Rows.FormerIDs, f)
				return nil
			}); err != nil {
			return foldOp{}, err
		}
	}
	return op, nil
}

// scanInto runs one (kind, id)-keyed read and hands each row to fn — the shape
// every capture query above shares.
func (t *txn) scanInto(query string, ref eref, fn func(*sql.Rows) error) error {
	rows, err := t.query(query, ref.Kind, ref.ID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// applyResync replays one: clear every side-store row keyed on a scope record,
// then write the recorded rows back. The clear runs over the WHOLE scope before
// the first insert, so a row that moved between two scope records cannot meet
// its own old copy on the way past.
func (t *txn) applyResync(op foldOp) error {
	for _, ref := range op.Scope {
		for _, q := range []string{
			`DELETE FROM annotations WHERE record_kind = $1 AND record_id = $2`,
			`DELETE FROM property_managers WHERE record_kind = $1 AND record_id = $2`,
			`DELETE FROM former_ids WHERE record_kind = $1 AND (record_id = $2 OR former_id = $2)`,
		} {
			if _, err := t.exec(q, ref.Kind, ref.ID); err != nil {
				return fmt.Errorf("substrate/engine: resync %s %s: %w", ref.Kind, ref.ID, err)
			}
		}
	}
	if op.Rows == nil {
		return nil
	}
	for _, a := range op.Rows.Annotations {
		if _, err := t.exec(`
			INSERT INTO annotations (record_kind, record_id, key, value, updated_at)
			VALUES ($1, $2, $3, $4::jsonb, $5)`,
			a.Kind, a.ID, a.Key, rawOrNull(a.Value), a.UpdatedAt); err != nil {
			return fmt.Errorf("substrate/engine: resync annotation %s %s %s: %w", a.Kind, a.ID, a.Key, err)
		}
	}
	for _, m := range op.Rows.Managers {
		if _, err := t.exec(`
			INSERT INTO property_managers (record_kind, record_id, property, actor, tier, principal, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			m.Kind, m.ID, m.Property, m.Actor, m.Tier, m.Principal, m.UpdatedAt); err != nil {
			return fmt.Errorf("substrate/engine: resync manager %s %s %s: %w", m.Kind, m.ID, m.Property, err)
		}
	}
	for _, f := range op.Rows.FormerIDs {
		if _, err := t.exec(`
			INSERT INTO former_ids (record_kind, former_id, record_id, created_at) VALUES ($1, $2, $3, $4)`,
			f.Kind, f.FormerID, f.RecordID, f.CreatedAt); err != nil {
			return fmt.Errorf("substrate/engine: resync former id %s %s: %w", f.Kind, f.FormerID, err)
		}
	}
	return nil
}

// rawOrNull keeps the jsonb column honest when an effect arrives with the
// field omitted.
func rawOrNull(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte(`null`)
	}
	return raw
}

// --- the delta ---

// diffRow describes what turned `before` into `after`. A nil `before` is a
// creation. The result is exactly what `applyTo` needs to do it again.
func diffRow(before, after *erow) *rowDelta {
	d := &rowDelta{}
	if before == nil {
		before = &erow{States: map[string]string{}, Props: map[string]any{}, Labels: map[string]any{}}
		d.Created = true
	}
	for _, name := range sortedKeys(after.Props) {
		cur, had := before.Props[name]
		if had && jsonEqual(cur, after.Props[name]) {
			continue
		}
		if d.Set == nil {
			d.Set = map[string]any{}
		}
		d.Set[name] = after.Props[name]
	}
	for _, name := range sortedKeys(before.Props) {
		if _, still := after.Props[name]; !still {
			d.Del = append(d.Del, name)
		}
	}
	if before.Title != after.Title {
		d.Title = ptrTo(after.Title)
	}
	if before.Body != after.Body {
		d.Body = ptrTo(after.Body)
	}
	for _, hot := range []struct {
		was, is *time.Time
		dst     **string
	}{
		{before.At, after.At, &d.At},
		{before.EndsAt, after.EndsAt, &d.EndsAt},
		{before.DueAt, after.DueAt, &d.DueAt},
	} {
		if timeString(hot.was) != timeString(hot.is) {
			*hot.dst = ptrTo(timeString(hot.is))
		}
	}
	if !sameStates(before.States, after.States) {
		d.States = map[string]string{}
		for k, v := range after.States {
			d.States[k] = v
		}
	}
	if !jsonEqual(nonNilMap(before.Labels), nonNilMap(after.Labels)) {
		d.Labels = map[string]any{}
		for k, v := range after.Labels {
			d.Labels[k] = v
		}
	}
	if !jsonEqual(nonNilStrings(before.Finalizers), nonNilStrings(after.Finalizers)) {
		fin := append([]string{}, after.Finalizers...)
		d.Finalizers = &fin
	}
	return d
}

// applyTo replays the delta onto a row: the fold's half of diffRow.
func (d *rowDelta) applyTo(row *erow) {
	if row.Props == nil {
		row.Props = map[string]any{}
	}
	if row.Labels == nil {
		row.Labels = map[string]any{}
	}
	if row.States == nil {
		row.States = map[string]string{}
	}
	for _, name := range sortedKeys(d.Set) {
		row.Props[name] = d.Set[name]
	}
	for _, name := range d.Del {
		delete(row.Props, name)
	}
	if d.Title != nil {
		row.Title = *d.Title
	}
	if d.Body != nil {
		row.Body = *d.Body
	}
	for _, hot := range []struct {
		val *string
		dst **time.Time
	}{
		{d.At, &row.At},
		{d.EndsAt, &row.EndsAt},
		{d.DueAt, &row.DueAt},
	} {
		if hot.val == nil {
			continue
		}
		*hot.dst = parseTimeValue(*hot.val)
	}
	if d.States != nil {
		row.States = map[string]string{}
		for k, v := range d.States {
			row.States[k] = v
		}
	}
	if d.Labels != nil {
		row.Labels = map[string]any{}
		for k, v := range d.Labels {
			row.Labels[k] = v
		}
	}
	if d.Finalizers != nil {
		row.Finalizers = append([]string{}, (*d.Finalizers)...)
	}
}

func ptrTo[T any](v T) *T { return &v }

func timeString(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTimeValue(s string) *time.Time {
	if s == "" {
		return nil
	}
	v, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return nil
	}
	v = v.UTC()
	return &v
}

func sameStates(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func nonNilMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// --- replay ---

// foldEntry applies one changelog entry to the fold tables. The entry's timestamp
// becomes the transaction's clock, because `created_at`, `updated_at` and
// `deleted_at` are all the writing transaction's `now` — replaying under the
// replay's own clock would reproduce the values and not the row.
func (t *txn) foldEntry(ch substrate.Change) error {
	t.now = ch.TS.UTC()
	ops, err := foldOpsOf(ch)
	if err != nil {
		return err
	}
	for _, op := range ops {
		if _, err := t.foldOne(op); err != nil {
			return fmt.Errorf("substrate/engine: fold seq %d (%s %s %s): %w", ch.Seq, ch.Op, ch.Kind, ch.RecordID, err)
		}
	}
	return nil
}

// foldOpsOf reads an entry's effects back out of its payload.
func foldOpsOf(ch substrate.Change) ([]foldOp, error) {
	raw, ok := ch.Payload[foldPayloadKey]
	if !ok {
		return nil, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	// UseNumber: scanChange hands the payload over with its numbers spelled as
	// stored, and this second decode must not flatten them through float64 on
	// the way into the effects' open maps (Set, Labels, Props, Value).
	dec := json.NewDecoder(bytes.NewReader(encoded))
	dec.UseNumber()
	var ops []foldOp
	if err := dec.Decode(&ops); err != nil {
		return nil, fmt.Errorf("substrate/engine: seq %d carries an unreadable fold: %w", ch.Seq, err)
	}
	return ops, nil
}

// foldPayloadKey is where an entry's effects live in its payload.
const foldPayloadKey = "fold"

// forEachRecordDeltaSet walks a DECODED payload's record-delta effects and
// hands each one's `set` map to fn, with the kind reference and record id it
// lands on. It is the one place the raw payload shape (fold, op.kind==record,
// delta.set) is spelled outside the typed foldOp structs, so the change
// feed's redaction and any later payload walk share one reading of it.
func forEachRecordDeltaSet(payload map[string]any, fn func(kindRef, recordID string, set map[string]any)) {
	effects, ok := payload[foldPayloadKey].([]any)
	if !ok {
		return
	}
	for _, e := range effects {
		op, ok := e.(map[string]any)
		if !ok || op["kind"] != string(foldRecord) {
			continue
		}
		ref, _ := op["ref"].(string)
		id, _ := op["id"].(string)
		delta, ok := op["delta"].(map[string]any)
		if !ok {
			continue
		}
		set, ok := delta["set"].(map[string]any)
		if !ok {
			continue
		}
		fn(ref, id, set)
	}
}

// foldRefuses reports an entry the fold cannot faithfully replay, so a rebuild
// stops instead of producing a fold that is quietly not the changelog's.
//
// Two entries are refused.
//
// A MERGE OR SPLIT WITHOUT A RESYNC EFFECT. Merge and split rewrite the side
// stores around the pair they join, and an entry that describes the moved sets
// for split's undo carries nothing the fold can act on. It is refused rather
// than replayed into a silent difference.
//
// AN ENTRY FROM BEFORE REFERENCES ABSORBED THE EDGE (decision 0044, changelog
// dialect 1). `link` and `unlink` were ops and `edge`/`unedge`/`edge1` were
// fold effects; both are gone, and their meaning lives in the source record's
// own properties now, which no such entry carries. There is no translation and
// no migration path: the rebuild refuses the entry by name, so the operator
// reads which spelling stopped it instead of watching a replay reconstruct a
// record with no pointers on it.
//
// An effect the fold does not know at all is refused by foldOne, one layer
// down, where the same rule holds for every operation.
func foldRefuses(ch substrate.Change) bool {
	switch string(ch.Op) {
	case opLinkRetired, opUnlinkRetired:
		return true
	}
	ops, err := foldOpsOf(ch)
	if err != nil {
		return true
	}
	for _, op := range ops {
		switch string(op.Kind) {
		case foldEdgePutRetired, foldEdgeDelRetired, foldEdgeOnlyRetired:
			return true
		}
	}
	switch ch.Op {
	case substrate.OpMerge, substrate.OpSplit:
		for _, op := range ops {
			if op.Kind == foldResync {
				return false
			}
		}
		return true
	}
	return false
}

// The dialect-1 spellings this binary refuses. They are named as constants and
// not as bare literals so the refusal is greppable from the words a stored
// payload actually holds.
const (
	opLinkRetired   = "link"
	opUnlinkRetired = "unlink"

	foldEdgePutRetired  = "edge"
	foldEdgeDelRetired  = "unedge"
	foldEdgeOnlyRetired = "edge1"
)

// decodeNumberPreserving decodes stored JSONB without flattening numbers to
// float64: a rewritten payload must re-marshal every untouched value
// byte-faithfully, concurrency tokens included.
func decodeNumberPreserving(raw []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var out map[string]any
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}
