package engine

// THE MARK ON A PROPERTY (decision 0284).
//
// Every property a write sets carries a mark beside its value: whether sync
// may replace the value (pinned), and where the value came from (setBy,
// onBehalfOf, derivedFrom, setAt). The two are stored apart because they
// answer different questions: a person may pin what sync wrote, and a
// function may write what nobody pinned.
//
// PINNED IS STORED WHEN THE VALUE IS WRITTEN, from the type of the hand that
// wrote it (pinsWrite), and never worked out again: not from a declaration,
// not from the hand as it stands later. `pin` and `release` are the only
// other ways it moves, each an entry of its own in history.
//
// A mark lives in `property_marks`, a fold table the `mark` and `unmark`
// effects write (fold.go), so a rebuild replays it. A link a mapping fills
// carries none: its declaration already says which mapping fills it.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// mark is one property's stored mark. SetBy and OnBehalfOf are actor record
// ids, DerivedFrom the record paths of the source records behind the value,
// sorted.
type mark struct {
	Pinned      bool
	SetBy       string
	OnBehalfOf  string
	DerivedFrom []string
	SetAt       time.Time
}

// equal reports whether two marks hold the same facts.
func (m mark) equal(o mark) bool {
	return m.Pinned == o.Pinned && m.SetBy == o.SetBy && m.OnBehalfOf == o.OnBehalfOf &&
		slices.Equal(m.DerivedFrom, o.DerivedFrom) && m.SetAt.Equal(o.SetAt)
}

// mark is the mark a `mark` effect states, its time the transaction's clock
// where the effect names none.
func (op foldOp) mark(now time.Time) mark {
	at := now
	if op.SetAt != nil {
		at = *op.SetAt
	}
	return mark{Pinned: op.Pinned, SetBy: op.SetBy, OnBehalfOf: op.OnBehalfOf, DerivedFrom: op.DerivedFrom, SetAt: at}
}

func (r foldMarkRow) mark() mark {
	return mark{Pinned: r.Pinned, SetBy: r.SetBy, OnBehalfOf: r.OnBehalfOf, DerivedFrom: r.DerivedFrom, SetAt: r.SetAt}
}

// pinsWrite reports whether a direct write by the hand pins what it writes: a
// person's, an app's, a function's and an agent's do, a package's and the
// engine's do not. A hand of no known type pins, as a stranger's write holds
// like the person's and never like the machinery.
func pinsWrite(actor substrate.Actor) bool {
	switch substrate.ActorTypeOf(actor) {
	case substrate.ActorTypePackage, substrate.ActorTypeSystem:
		return false
	}
	return true
}

// directMark is the mark a direct write by the hand leaves on what it sets:
// pinned by its type, set by it, for whomever it acts for, now.
func (t *txn) directMark(actor substrate.Actor) mark {
	links := t.linksOf(actor)
	return mark{Pinned: pinsWrite(actor), SetBy: links.actor, OnBehalfOf: links.onBehalfOf, SetAt: t.now}
}

// setMark writes one property's whole mark through the fold. The effect
// names its time only where it is not the transaction's clock.
func (t *txn) setMark(ref eref, property string, m mark) error {
	op := foldOp{
		Kind: foldMark, Ref: ref.Kind, ID: ref.ID, Property: property,
		Pinned: m.Pinned, SetBy: m.SetBy, OnBehalfOf: m.OnBehalfOf, DerivedFrom: m.DerivedFrom,
	}
	if !m.SetAt.Equal(t.now) {
		at := m.SetAt.UTC()
		op.SetAt = &at
	}
	_, err := t.fold(op)
	return err
}

// unmark drops one property's mark: a deleted property is nobody's, so the
// next recompute may refill it, and a link a mapping fills carries none.
func (t *txn) unmark(ref eref, property string) error {
	_, err := t.fold(foldOp{Kind: foldUnmark, Ref: ref.Kind, ID: ref.ID, Property: property})
	return err
}

// applyMark folds one mark. It reports a change only where a fact moved.
func (t *txn) applyMark(ref eref, property string, m mark) (bool, error) {
	// A bulk run's record holds no mark, so the INSERT below would add one
	// (bulkwrite.go).
	if t.queueMark(ref, property, m) {
		return true, nil
	}
	derived, err := jsonList(m.DerivedFrom)
	if err != nil {
		return false, err
	}
	res, err := t.exec(`
		INSERT INTO property_marks (record_kind, record_id, property, pinned, set_by, on_behalf_of, derived_from, set_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8)
		ON CONFLICT (repository, record_kind, record_id, property) DO UPDATE SET
			pinned = EXCLUDED.pinned, set_by = EXCLUDED.set_by, on_behalf_of = EXCLUDED.on_behalf_of,
			derived_from = EXCLUDED.derived_from, set_at = EXCLUDED.set_at
		WHERE property_marks.pinned       IS DISTINCT FROM EXCLUDED.pinned
		   OR property_marks.set_by       IS DISTINCT FROM EXCLUDED.set_by
		   OR property_marks.on_behalf_of IS DISTINCT FROM EXCLUDED.on_behalf_of
		   OR property_marks.derived_from IS DISTINCT FROM EXCLUDED.derived_from
		   OR property_marks.set_at       IS DISTINCT FROM EXCLUDED.set_at`,
		ref.Kind, ref.ID, property, m.Pinned, m.SetBy, m.OnBehalfOf, derived, m.SetAt)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// insertMark writes one mark row a resync restates, over a scope it cleared.
func (t *txn) insertMark(ref eref, property string, m mark) error {
	derived, err := jsonList(m.DerivedFrom)
	if err != nil {
		return err
	}
	_, err = t.exec(`
		INSERT INTO property_marks (record_kind, record_id, property, pinned, set_by, on_behalf_of, derived_from, set_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8)`,
		ref.Kind, ref.ID, property, m.Pinned, m.SetBy, m.OnBehalfOf, derived, m.SetAt)
	return err
}

func (t *txn) applyUnmark(ref eref, property string) (bool, error) {
	res, err := t.exec(`DELETE FROM property_marks WHERE record_kind = $1 AND record_id = $2 AND property = $3`,
		ref.Kind, ref.ID, property)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// jsonList is a list of record paths as the derived_from column holds it:
// a JSON array, never null.
func jsonList(paths []string) ([]byte, error) {
	if paths == nil {
		paths = []string{}
	}
	return json.Marshal(paths)
}

// pathList scans the derived_from column into a list of record paths.
type pathList struct{ out *[]string }

func (p pathList) Scan(src any) error {
	var raw []byte
	switch v := src.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	case nil:
		*p.out = nil
		return nil
	default:
		return fmt.Errorf("substrate/engine: derived_from is %T, want a JSON list", src)
	}
	var paths []string
	if err := json.Unmarshal(raw, &paths); err != nil {
		return fmt.Errorf("substrate/engine: derived_from: %w", err)
	}
	if len(paths) == 0 {
		paths = nil
	}
	*p.out = paths
	return nil
}

// marksOf reads one record's marks, property → mark.
func (t *txn) marksOf(ref eref) (map[string]mark, error) {
	rows, err := t.query(`
		SELECT property, pinned, set_by, on_behalf_of, derived_from, set_at FROM property_marks
		WHERE record_kind = $1 AND record_id = $2`, ref.Kind, ref.ID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanMarks(rows)
}

func scanMarks(rows *sql.Rows) (map[string]mark, error) {
	out := map[string]mark{}
	for rows.Next() {
		var property string
		var m mark
		if err := rows.Scan(&property, &m.Pinned, &m.SetBy, &m.OnBehalfOf, pathList{&m.DerivedFrom}, &m.SetAt); err != nil {
			return nil, err
		}
		m.SetAt = m.SetAt.UTC()
		out[property] = m
	}
	return out, rows.Err()
}

// --- pin and release ---

// markAction is one of the two explicit actions on a mark.
type markAction string

const (
	actionPin     markAction = "pin"
	actionRelease markAction = "release"
)

// Pin keeps each named property's current value against sync.
func (ds *dataset) Pin(ctx context.Context, actor substrate.Actor, typ, id string, in substrate.MarkInput) (*substrate.Record, error) {
	return ds.markAction(ctx, actor, actionPin, typ, id, in)
}

// Release hands each named property back to sync.
func (ds *dataset) Release(ctx context.Context, actor substrate.Actor, typ, id string, in substrate.MarkInput) (*substrate.Record, error) {
	return ds.markAction(ctx, actor, actionRelease, typ, id, in)
}

func (ds *dataset) markAction(ctx context.Context, actor substrate.Actor, action markAction, typ, id string, in substrate.MarkInput) (*substrate.Record, error) {
	if len(in.Properties) == 0 {
		return nil, fmt.Errorf("%w: a %s names no property", substrate.ErrValidation, action)
	}
	var out *substrate.Record
	err := ds.inTx(ctx, actor, false, func(t *txn) error {
		rec, err := t.markAction(action, typ, id, in)
		out = rec
		return err
	})
	if err != nil {
		return nil, err
	}
	return ds.Get(ctx, out.Kind, out.ID)
}

// markAction pins or releases the named properties of one live record, in
// one history entry, held to each property's `writer:` as a write is: the record's version moves, as an annotation's change
// moves it, and the entry's payload names the properties. A property that
// holds no value has nothing to pin or release and is refused, as is one the
// kind does not declare. A pin of a pinned property, or a release of one
// that is not, changes nothing and writes nothing. A release recomputes the
// record afterwards, so sync picks among the live offers in its own entry.
func (t *txn) markAction(action markAction, typ, id string, in substrate.MarkInput) (*substrate.Record, error) {
	ty, err := t.resolveType(typ)
	if err != nil {
		return nil, err
	}
	ref := eref{Kind: ty.Identity, ID: id}
	row, err := t.loadRow(ref, true)
	if err != nil {
		return nil, err
	}
	if row == nil || row.DeletedAt != nil {
		return nil, fmt.Errorf("%w: %s %s", substrate.ErrNotFound, ty.Identity, id)
	}
	ref = row.ref()
	// The record's own rules hold as they hold a patch: a declaration row is
	// no record a mark sits on, the substrate's own kinds take no edit, and
	// a version precondition fails the action whole.
	if _, isVocabulary := vocabularyRecordKinds[ty.Identity]; isVocabulary {
		return nil, fmt.Errorf("%w: %s is a declaration, and a declaration carries no marks", substrate.ErrValidation, ty.Identity)
	}
	if err := t.forbidSystemKind(ty, substrate.OpPatch); err != nil {
		return nil, err
	}
	if err := checkCAS(row, in.IfVersion); err != nil {
		return nil, err
	}
	marks, err := t.marksOf(ref)
	if err != nil {
		return nil, err
	}
	names := slices.Clone(in.Properties)
	sort.Strings(names)
	names = slices.Compact(names)
	// A mark is the property's as much as its value is: a hand its `writer:`
	// leaves out may no more pin or release it than write it, or it could
	// lift a pin orphan detection reads. Held for every named property
	// before any mark moves, so the action is refused whole.
	if err := t.checkPropertyOwnership(ty, names); err != nil {
		return nil, err
	}
	var moved []string
	for _, name := range names {
		p, ok := ty.Prop(name)
		if !ok {
			return nil, fmt.Errorf("%w: %s declares no property %q to %s", substrate.ErrValidation, ty.Identity, name, action)
		}
		if p.MappedBy != "" {
			return nil, fmt.Errorf("%w: %s.%s is the link mapping %s fills, and carries no mark to %s",
				substrate.ErrValidation, ty.Identity, name, p.MappedBy, action)
		}
		if _, held := row.Props[name]; !held && hotValue(row, name) == nil {
			return nil, fmt.Errorf("%w: %s %s holds no %s to %s", substrate.ErrValidation, ref.Kind, ref.ID, name, action)
		}
		m, marked := marks[name]
		if !marked {
			// A value from before marks, or one a mapping link filled: the
			// pin stands behind what is there, said by no hand.
			m = mark{SetAt: row.UpdatedAt.UTC()}
		}
		want := action == actionPin
		if marked && m.Pinned == want {
			continue
		}
		m.Pinned = want
		if err := t.setMark(ref, name, m); err != nil {
			return nil, err
		}
		moved = append(moved, name)
	}
	if len(moved) == 0 {
		return t.recordOf(ref)
	}
	// The entry: the record's version moves (a touch, as an annotation's
	// change does), and the payload says what the action did to which
	// properties.
	if _, err := t.fold(foldOp{Kind: foldRecord, Ref: ref.Kind, ID: ref.ID, Delta: &rowDelta{Force: true}}); err != nil {
		return nil, err
	}
	key := "pinned"
	if action == actionRelease {
		key = "released"
	}
	if err := t.appendChange(t.actor, substrate.OpPatch, ref.ID, ref.Kind, map[string]any{key: moved}); err != nil {
		return nil, err
	}
	if len(t.declarations().MappingsTo(ref.Kind)) > 0 {
		// A release hands the value to sync at once; a pin makes a target
		// whose sources have gone no orphan (orphans.go).
		if action == actionRelease {
			err = t.recompute(ref)
		} else {
			err = t.syncOrphaned(ref)
		}
		if err != nil {
			return nil, err
		}
	}
	return t.recordOf(ref)
}

// --- the read ---

// propertyMarks assembles one record's marks: the stored ones, and beside
// each the live offers whose value differs (JSON equality) from the stored
// one, the ALTERNATIVES a reader shows beside a pinned value. An offer is
// keyed by the source record that offers it, so two accounts of one
// provider are two. The offer row is derived, so nothing here is a second
// copy of the changelog. A link a mapping fills carries no mark, here as in
// storage.
func (ds *dataset) propertyMarks(ctx context.Context, e *substrate.Record) (map[string]substrate.PropertyMark, error) {
	ty, tyErr := ds.resolveType(e.Kind)
	out := map[string]substrate.PropertyMark{}
	rows, err := ds.db.QueryContext(ctx, `
		SELECT property, pinned, set_by, on_behalf_of, derived_from, set_at FROM property_marks
		WHERE record_kind = $1 AND record_id = $2`, e.Kind, e.ID)
	if err != nil {
		return nil, err
	}
	stored, err := scanMarks(rows)
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	for name, m := range stored {
		if tyErr == nil && ty != nil {
			if p, ok := ty.Prop(name); ok && p.MappedBy != "" {
				continue
			}
		}
		out[name] = markOnTheWire(m)
	}
	rows, err = ds.db.QueryContext(ctx, `
		SELECT property, source, value, updated_at FROM property_offers
		WHERE record_kind = $1 AND record_id = $2 ORDER BY property, updated_at DESC, source`, e.Kind, e.ID)
	if err != nil {
		return nil, err
	}
	// The offers are read whole before any is compared: a reference offer
	// is compared through further reads (comparableReference), which must not
	// need a second pool connection while this cursor holds the first.
	type offerRow struct {
		property, source string
		raw              []byte
		at               time.Time
	}
	var offers []offerRow
	for rows.Next() {
		var o offerRow
		if err := rows.Scan(&o.property, &o.source, &o.raw, &o.at); err != nil {
			_ = rows.Close()
			return nil, err
		}
		offers = append(offers, o)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	// Which mapped properties are unions: a source whose items the stored
	// union holds backs it, and is not an alternative to it.
	union := map[string]bool{}
	for _, m := range ds.registry().MappingsTo(e.Kind) {
		for name, rule := range m.Map {
			if rule.Merge == vocabulary.MergeUnion {
				union[name] = true
			}
		}
	}
	for _, o := range offers {
		var value any
		if len(o.raw) > 0 {
			_ = json.Unmarshal(o.raw, &value)
		}
		// An offer's stored value bypasses recordOf, so a property that is
		// sensitive TODAY redacts here too: a stale offer minted before a
		// re-type must not hand out what the main value hides. Unresolvable
		// kinds fail closed the same way.
		storedValue, compared := e.Properties[o.property], value
		if tyErr != nil || ty == nil {
			value, compared = Redacted, Redacted
		} else if p, ok := ty.Prop(o.property); ok && p.Sensitive() {
			value, compared = Redacted, Redacted
		} else if ok && p.Datatype == vocabulary.DatatypeReference {
			// An offer spells a reference as its source wrote it, a mirror
			// included; the stored value holds what the pin resolved it to.
			// Both sides are read through the mirror's subject and the
			// former-id trail before they are compared (#580).
			if compared, err = ds.comparableReference(ctx, p, value); err != nil {
				return nil, err
			}
			if storedValue, err = ds.comparableReference(ctx, p, storedValue); err != nil {
				return nil, err
			}
		}
		if offerBacks(storedValue, compared, union[o.property]) {
			continue
		}
		m := out[o.property]
		m.Alternatives = append(m.Alternatives, substrate.PropertyAlternative{
			DerivedFrom: substrate.LinkTo(o.source), Value: value, UpdatedAt: o.at.UTC(),
		})
		out[o.property] = m
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// markOnTheWire is a stored mark as a read serves it: each id and path a
// link to its record.
func markOnTheWire(m mark) substrate.PropertyMark {
	w := substrate.PropertyMark{Pinned: m.Pinned, SetAt: m.SetAt}
	if m.SetBy != "" {
		w.SetBy = substrate.LinkTo(vocabulary.RecordPath(kindActor, m.SetBy))
	}
	if m.OnBehalfOf != "" {
		w.OnBehalfOf = substrate.LinkTo(vocabulary.RecordPath(kindActor, m.OnBehalfOf))
	}
	for _, path := range m.DerivedFrom {
		w.DerivedFrom = append(w.DerivedFrom, substrate.RecordLink{Ref: path})
	}
	return w
}

// --- the landed repository migrations' words ---

// setManager is the mark write the landed repository migration 0006 spells
// in the words of the manager row it was written against, kept as it landed
// (frozen:check): a write by the hand, pinned unless it held at the machine
// tier. Nothing else calls it.
func (t *txn) setManager(ref eref, property string, actor substrate.Actor, tier substrate.Tier) error {
	m := t.directMark(actor)
	m.Pinned = tier != substrate.TierMachine
	return t.setMark(ref, property, m)
}
