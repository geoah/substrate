package engine

// A PUT THAT RESTORES A TOMBSTONE COMPLETES THE CONVERSIONS THE TOMBSTONE
// MISSED (decision 0144, amending 0066). A conversion rewrites live records
// alone (convert.go), so a record tombstoned before an apply renamed a
// property, respelled an enum value, made a property required with a default
// or dropped one keeps the shape it was deleted in, and a put onto it merges
// that shape back. The restoring put therefore rewrites the stored row
// against the kind as declared now, before the writer's properties merge in:
//
//   - a value under a name some property declares as its `renamedFrom` moves
//     to that property, with its manager row and vectors (rename.go);
//   - a value holding a spelling some enum value declares as its
//     `renamedFrom` takes the new spelling, in a scalar, a list or a keyed map
//     (remapValue);
//   - every other stored value is held to what a write of it would store
//     (admitRestored): kept in that stored form where the declaration admits
//     it, and removed with its manager row, vectors and sealed material where
//     it does not (an undeclared name, a retype, a removed value, a tightened
//     pattern or bound, a reference the pin no longer admits, a blob that is
//     gone), as a null step removes a live record's;
//   - a required property with a default the row holds no value for receives
//     the default, managed by the restoring hand, as a backfill fills a live
//     record lacking it.
//
// A name the writer's put names is left to the merge: the writer's value, or
// its null, is the answer there. The declaration is the only history read, so
// a name or a spelling it no longer mentions (a second rename over the
// first) is refused and removed rather than followed.
//
// Everything lands in the restoring put's one entry as values in its delta,
// so a rebuild replays the same row without reading a declaration, and the
// payload names each step under a conversion's keys (`renamed`, `remapped`,
// `backfilled`, `nulled`), which the values read pairs a rename by
// (changevalues.go). A tombstone is never rewritten while it stays one: a
// record nobody restores costs nothing and gains no history.

import (
	"errors"
	"fmt"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// restoreShape is what a restoring put rewrote on the stored row before the
// merge, step by step.
type restoreShape struct {
	// renamed is old name to new, as a conversion entry carries it.
	renamed map[string]string
	// remapped is property to old spelling to new.
	remapped   map[string]map[string]string
	backfilled []string
	// coerced names the kept values the declaration's coercion stores in
	// another form than the tombstone held (a string retyped to a datetime
	// normalizes to UTC). No step key names them; `properties` does.
	coerced []string
	// nulled is each removed property with the value the tombstone held, so
	// the sealed material behind a secret's ref goes with it.
	nulled map[string]any
}

// names is every property the shape moved, sorted: a rename's two names, and
// every remapped, backfilled, coerced and removed one.
func (s restoreShape) names() []string {
	seen := map[string]bool{}
	for from, to := range s.renamed {
		seen[from], seen[to] = true, true
	}
	for name := range s.remapped {
		seen[name] = true
	}
	for _, name := range s.backfilled {
		seen[name] = true
	}
	for _, name := range s.coerced {
		seen[name] = true
	}
	for name := range s.nulled {
		seen[name] = true
	}
	return sortedKeys(seen)
}

// reshapeRestored rewrites a tombstoned row, in place, into the shape the
// kind declares now, leaving every name the write names to the merge.
func (t *txn) reshapeRestored(sp *applySpec, row *erow) (restoreShape, error) {
	var s restoreShape
	ty := sp.ty
	named := func(name string) bool {
		_, ok := sp.props[name]
		return ok
	}
	for _, name := range ty.PropOrder {
		from := ty.Props[name].RenamedFrom
		if from == "" || named(from) || named(name) {
			continue
		}
		if _, declared := ty.Props[from]; declared {
			continue
		}
		v, held := row.Props[from]
		if !held {
			continue
		}
		// A value already under the new name stands, as a live rename never
		// replaces one (0063); the old name's value is then undeclared and is
		// removed below.
		if _, taken := row.Props[name]; taken {
			continue
		}
		row.Props[name] = v
		delete(row.Props, from)
		if s.renamed == nil {
			s.renamed = map[string]string{}
		}
		s.renamed[from] = name
	}
	for _, name := range sortedKeys(row.Props) {
		if named(name) {
			continue
		}
		held := row.Props[name]
		if p, declared := ty.Props[name]; declared && !p.IsState() {
			v := held
			var moved map[string]string
			for _, ev := range p.Values {
				if ev.RenamedFrom == "" {
					continue
				}
				if next, ok := remapValue(v, ev.RenamedFrom, ev.Value); ok {
					v = next
					if moved == nil {
						moved = map[string]string{}
					}
					moved[ev.RenamedFrom] = ev.Value
				}
			}
			kept, ok, err := t.admitRestored(sp, name, p, v)
			if err != nil {
				return s, err
			}
			if ok {
				if moved != nil {
					if s.remapped == nil {
						s.remapped = map[string]map[string]string{}
					}
					s.remapped[name] = moved
				} else if !jsonEqual(kept, held) {
					s.coerced = append(s.coerced, name)
				}
				row.Props[name] = kept
				continue
			}
		}
		delete(row.Props, name)
		if s.nulled == nil {
			s.nulled = map[string]any{}
		}
		// A renamed value the new property refuses (a rename that also
		// retyped) never lands under the new name: what leaves the record is
		// the old name, and nothing moved.
		gone := name
		for from, to := range s.renamed {
			if to == name {
				gone = from
				delete(s.renamed, from)
				break
			}
		}
		s.nulled[gone] = held
	}
	filled := map[string]any{}
	for _, name := range ty.PropOrder {
		p := ty.Props[name]
		if !p.Required || p.IsState() || !backfillable(ty, p) || named(name) || !emptyValue(row.Props[name]) {
			continue
		}
		// Coerced and validated as backfillValues does for a live record, so
		// the restored row holds what a create falling back to it would.
		value, err := coerceValue(p, p.Default)
		if err != nil {
			return s, fmt.Errorf("%w: restore %s: the default of %q: %w", substrate.ErrValidation, sp.id, name, err)
		}
		filled[name] = value
	}
	if len(filled) > 0 {
		if err := t.validateReferences(ty, filled); err != nil {
			return s, err
		}
		for _, name := range sortedKeys(filled) {
			row.Props[name] = filled[name]
			s.backfilled = append(s.backfilled, name)
		}
	}
	return s, nil
}

// admitRestored answers the value a restored row keeps for one declared
// property, remapped already: what a write naming that value would store, or
// false where the declaration in force refuses it. The checks are the write
// path's own, the pure coercion and the transaction's reference and blob
// gates, so the restored record's read applies back unchanged.
func (t *txn) admitRestored(sp *applySpec, name string, p *vocabulary.Property, v any) (any, bool, error) {
	// A sealed ref this record owns is a secret's value and nothing else's: a
	// property retyped away from secret would read the ref as text and orphan
	// the material behind it.
	if s, isText := v.(string); isText && strings.HasPrefix(s, secretRefPrefix) {
		owned, err := t.sealedRefOf(s, sp.ref())
		if err != nil {
			return nil, false, err
		}
		if owned {
			return v, p.Secret(), nil
		}
	}
	if p.Secret() {
		// Anything but an owned ref under a secret is material nobody sealed
		// (a string retyped to secret), and no write stores material in the
		// row. The empty value is the one a cleared secret leaves.
		s, isText := v.(string)
		return v, isText && s == "", nil
	}
	kept, err := coerceValue(p, v)
	if err != nil {
		return nil, false, nil
	}
	gate := map[string]any{name: kept}
	switch {
	case holdsReference(p):
		err = t.validateReferences(sp.ty, gate)
	case p.Datatype == vocabulary.DatatypeBlobRef:
		err = t.validateBlobRefs(sp.ty, gate)
	}
	if errors.Is(err, substrate.ErrValidation) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return gate[name], true, nil
}

// settleRestoreShape moves the rows beside the record that follow its values,
// once the restoring entry's record effect is folded: what a conversion does
// for the same step on a live record (convert.go convertRecord).
func (t *txn) settleRestoreShape(ref eref, sp *applySpec, s restoreShape) error {
	ty := sp.ty
	for _, from := range sortedKeys(s.renamed) {
		to := s.renamed[from]
		if err := t.moveManager(ref, from, to); err != nil {
			return err
		}
		if err := t.moveEmbeddings(ref, from, to, ty.Props[to].Embed); err != nil {
			return err
		}
	}
	for _, name := range append(sortedKeys(s.remapped), s.coerced...) {
		if ty.Props[name].Embed {
			if err := t.enqueueEmbed(ref, name); err != nil {
				return err
			}
		}
	}
	for _, name := range s.backfilled {
		if err := t.setManager(ref, name, t.actor, t.tier); err != nil {
			return err
		}
		if ty.Props[name].Embed {
			if err := t.enqueueEmbed(ref, name); err != nil {
				return err
			}
		}
	}
	for _, name := range sortedKeys(s.nulled) {
		if err := t.deleteManager(ref, name); err != nil {
			return err
		}
		if err := t.dropEmbeddings(ref, name); err != nil {
			return err
		}
		// Only a ref this record owns is erased: the value's declaration may
		// be gone, so its shape and the sealed row's owner are what say it was
		// a secret of this record.
		old, _ := s.nulled[name].(string)
		if !strings.HasPrefix(old, secretRefPrefix) {
			continue
		}
		owned, err := t.sealedRefOf(old, ref)
		if err != nil {
			return err
		}
		if !owned {
			continue
		}
		if _, err := t.exec(`DELETE FROM sealed WHERE ref = $1`, old); err != nil {
			return err
		}
		t.mirrorSealedDelete(old)
	}
	return nil
}

// annotate writes the shape's step keys onto the restoring entry's payload,
// beside the states the restore removed (write.go undeclaredStates), and
// widens `properties` to every name the entry moved.
func (s restoreShape) annotate(payload map[string]any, accepted, undeclaredStates []string) {
	gone := map[string]bool{}
	for _, name := range undeclaredStates {
		gone[name] = true
	}
	for name := range s.nulled {
		gone[name] = true
	}
	if nulled := sortedKeys(gone); len(nulled) > 0 {
		payload[payloadNulled] = nulled
	}
	if len(s.renamed) > 0 {
		payload[payloadRenamed] = s.renamed
	}
	if len(s.remapped) > 0 {
		payload["remapped"] = s.remapped
	}
	if len(s.backfilled) > 0 {
		payload["backfilled"] = s.backfilled
	}
	properties := append([]string{}, accepted...)
	for _, name := range s.names() {
		if !containsString(properties, name) {
			properties = append(properties, name)
		}
	}
	if len(properties) > 0 {
		payload["properties"] = properties
	}
}
