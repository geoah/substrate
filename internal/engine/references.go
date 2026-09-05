package engine

import (
	"errors"
	"fmt"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// validateReferences is the registry-aware gate for reference-typed
// properties, the twin of validateBlobRefs: coercion checked the SHAPE and left
// the value a record PATH, and this pass — taken inside the write transaction —
// resolves the referent KIND, refuses an unknown one, refuses a pin mismatch,
// resolves the one admitted hop through a mapping subject, refuses a repeated
// reference that names one record twice, enforces `mustExist:`, and rewrites the
// stored value to the canonical path. Mutating props here, before the row's
// property map is built, is what makes the stored value canonical on every write
// path.
//
// EXISTENCE IS OPT-IN. Without `mustExist: true` a reference is a plain pointer
// and may name a row that is not present yet: a trigger's `callable` names a
// function the same batch installs, and the trigger's OWN admission resolves
// that record separately. With it, the referent must be there — a tombstone
// counts, because the record exists and a split can bring it back.
//
// It reaches every POSITION a reference is declared at, not only a kind's own
// properties: inside an object, inside a repeated object's elements, inside a
// keyed map's values, to the declared depth. A position it did not reach would
// store whatever the writer sent — a bare local name where every reader expects
// a resolved identity.
func (t *txn) validateReferences(ty *vocabulary.Kind, props map[string]any) error {
	var problems []string
	t.refMissing = nil
	defer func() { t.refMissing = nil }()
	for _, name := range sortedKeys(props) {
		p, ok := ty.Prop(name)
		if !ok || !holdsReference(p) {
			continue
		}
		v := props[name]
		if v == nil {
			continue
		}
		nv, probs := t.normalizeReferencesIn(p, v, "props."+name)
		problems = append(problems, probs...)
		props[name] = nv
	}
	if len(problems) > 0 {
		// A `mustExist:` miss is a NOT FOUND, not a shape problem, and it
		// carries that sentinel out whole: it is the same refusal an addressed
		// read of the referent would give, which is what the door in front of
		// this maps to a 404. A problem list would flatten it to a 422 and the
		// caller would have to read the prose to tell the two apart.
		if len(t.refMissing) > 0 {
			return t.refMissing[0]
		}
		return &substrate.ValidationError{Problems: problems}
	}
	return nil
}

// declaresReference reports whether a KIND has a reference site anywhere: one
// walk over its properties, each to the depth holdsReference reaches. A kind
// that has none derives no rows from any value, which is what lets the write
// path skip the index entirely.
func declaresReference(ty *vocabulary.Kind) bool {
	if ty == nil {
		return false
	}
	for _, name := range ty.PropOrder {
		if holdsReference(ty.Props[name]) {
			return true
		}
	}
	return false
}

// holdsReference reports whether a declaration carries a reference anywhere
// inside it, so a plain object property is not walked at all.
func holdsReference(p *vocabulary.Property) bool {
	if p.Datatype == vocabulary.DatatypeReference {
		return true
	}
	for _, f := range p.Fields {
		if holdsReference(f) {
			return true
		}
	}
	return false
}

// normalizeReferencesIn rewrites every reference inside one coerced value to its
// canonical path, descending the declaration's containers (keyed map, list) and
// then its declared fields. It returns the rewritten value: a keyed map's values
// and an object's fields are replaced in place, so the caller stores what came
// back and nothing depends on which container aliased which map.
func (t *txn) normalizeReferencesIn(p *vocabulary.Property, v any, where string) (any, []string) {
	switch {
	case p.Keyed:
		m, ok := v.(map[string]any)
		if !ok {
			return v, []string{fmt.Sprintf("%s: expected a keyed map of references", where)}
		}
		var problems []string
		for _, key := range sortedKeys(m) {
			nv, probs := t.normalizeReferenceValue(p, m[key], fmt.Sprintf("%s.%s", where, key))
			problems = append(problems, probs...)
			m[key] = nv
		}
		return m, problems
	case p.Repeated:
		list, ok := v.([]any)
		if !ok {
			return v, []string{fmt.Sprintf("%s: expected a list of references", where)}
		}
		var problems []string
		for i := range list {
			nv, probs := t.normalizeReferenceValue(p, list[i], fmt.Sprintf("%s[%d]", where, i))
			problems = append(problems, probs...)
			list[i] = nv
		}
		// A repeated reference is an ORDERED SET of targets: naming one record
		// twice says nothing a single entry does not, and the refs index — whose
		// key is (property, path, ord) — would carry the same pointer under two
		// ordinals, so a reverse read would report it twice. Refused at the
		// write, where the author can see which value to drop.
		if p.Datatype == vocabulary.DatatypeReference {
			dups, err := t.duplicateRefs(list, where)
			if err != nil {
				return list, append(problems, fmt.Sprintf("%s: %v", where, err))
			}
			problems = append(problems, dups...)
		}
		return list, problems
	}
	return t.normalizeReferenceValue(p, v, where)
}

// normalizeReferenceValue handles ONE value of a declaration: the reference
// itself, or an object whose declared fields are walked in turn.
func (t *txn) normalizeReferenceValue(p *vocabulary.Property, v any, where string) (any, []string) {
	if v == nil {
		return nil, nil
	}
	if p.Datatype == vocabulary.DatatypeReference {
		nv, err := t.normalizeReference(p, v)
		if err != nil {
			if errors.Is(err, substrate.ErrNotFound) {
				t.refMissing = append(t.refMissing, fmt.Errorf("%s: %w", where, err))
			}
			return v, []string{fmt.Sprintf("%s: %v", where, err)}
		}
		return nv, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return v, nil
	}
	var problems []string
	for _, fname := range sortedKeys(m) {
		f, declared := p.Fields[fname]
		if !declared || !holdsReference(f) || m[fname] == nil {
			continue
		}
		nv, probs := t.normalizeReferencesIn(f, m[fname], where+"."+fname)
		problems = append(problems, probs...)
		m[fname] = nv
	}
	return m, problems
}

// storedReferencePath reads a STORED reference value as its record path: the
// object every write stores, the bare string a pre-0044 row still holds, or the
// retired dialect-1 {kind, id} pair.
//
// THE OBJECT ARM IS THE LIVE ONE. A reference is stored as `{ref: "<kind>/<id>",
// …}` whatever its declaration says (decision 0044), so a reader that only knew
// the string went blind the moment the one-shape rule landed: a dispatcher
// skipping a live trigger, delivery that stops without saying so. The string arm
// stays for the same reason splitReferenceValue keeps its: a READER never picks
// its parse from the declaration.
//
// NO STORE HOLDS A DIALECT-1 PAIR: no release ever wrote one and the write path
// refuses it by name (coerceReference). It stays as stated defense.
func storedReferencePath(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		if s, ok := t[vocabulary.ReferenceValueKey].(string); ok {
			return s
		}
		kind, _ := t["kind"].(string)
		id, _ := t["id"].(string)
		if kind == "" || id == "" {
			return ""
		}
		return vocabulary.RecordPath(kind, id)
	}
	return ""
}

// duplicateRefs names every record a repeated reference lists more than once,
// in the order the duplicates appear.
//
// The comparison is CANONICAL. A merge repoints no stored value, so one record
// answers to its own id and to every id it was merged from, and two entries
// spelling it both ways are the same pointer twice — which is what the rule
// forbids. The path REPORTED is the one the author wrote, because that is the
// entry they have to find and drop.
func (t *txn) duplicateRefs(list []any, where string) ([]string, error) {
	seen := map[eref]bool{}
	reported := map[eref]bool{}
	var problems []string
	for _, item := range list {
		path := referencePathOf(item)
		if path == "" {
			continue
		}
		kind, id, ok := vocabulary.SplitRecordPath(path)
		if !ok {
			continue
		}
		canon, err := t.canonicalOf(eref{Kind: kind, ID: id})
		if err != nil {
			return nil, err
		}
		if seen[canon] && !reported[canon] {
			reported[canon] = true
			problems = append(problems, fmt.Sprintf("%s: names %s twice — a repeated reference holds each record once", where, path))
		}
		seen[canon] = true
	}
	return problems, nil
}

// normalizeReference resolves ONE reference value: its referent kind against the
// registry, the declaration's pin, the one admitted mapping hop, and
// `mustExist:`. It returns the value in THE stored shape: the object, with the
// canonical record path "<authority>/<package>/<kind>/<id>" under `ref` and any link data
// beside it, whether or not the declaration declares link properties (0044).
// Coercion already produced a qualified path (a full path, or a pin-completed
// bare id) inside that object, so the stored path is spelled one way whatever
// the writer typed.
//
// The rewrite lands on the `ref` KEY, never on the value as a whole: the link
// data was coerced with it and only the pointer moves.
func (t *txn) normalizeReference(p *vocabulary.Property, v any) (any, error) {
	s := referencePathOf(v)
	if s == "" {
		return nil, fmt.Errorf(`a reference is a "<kind>/<id>" path string`)
	}
	// This pass RESOLVES; it does not re-decide what the writer meant. Coercion
	// already turned the authored value into a path — refusing the ambiguous
	// spellings at that one door — and reading it a second time as an authored
	// form would call a canonical value ambiguous and hide the real problem
	// (a `{kind, id}` pair naming an unknown kind reported "ambiguous" instead).
	kind, id, ok := vocabulary.SplitRecordPath(s)
	if !ok {
		return nil, fmt.Errorf(`a reference is a "<kind>/<id>" path string, and %q is not one`, s)
	}
	rt, err := t.ds.resolveType(kind)
	if err != nil {
		return nil, fmt.Errorf("referent kind %q is unknown", kind)
	}
	target := eref{Kind: rt.Identity, ID: id}
	if !referenceAdmits(t.ds.registry(), p, rt) {
		hopped, err := t.subjectHop(p, target, rt)
		if err != nil {
			return nil, err
		}
		target = hopped
	}
	if p.MustExist {
		row, err := t.loadRow(target, false)
		if err != nil {
			return nil, err
		}
		// A TOMBSTONE counts as existing. The record is there, a split can bring
		// it back, and refusing a pointer at one would make deleting a referent
		// silently break every later write to the records naming it.
		if row == nil {
			return nil, fmt.Errorf("%w: reference names %s, which does not exist",
				substrate.ErrNotFound, vocabulary.RecordPath(target.Kind, target.ID))
		}
	}
	path := vocabulary.RecordPath(target.Kind, target.ID)
	out := map[string]any{}
	if m, ok := v.(map[string]any); ok {
		for k, kv := range m {
			out[k] = kv
		}
	}
	out[vocabulary.ReferenceValueKey] = path
	return out, nil
}

// subjectSlots reports whether a kind declares a `subject: true` reference at
// all: a mirror waiting for a mapping, as against a kind that simply is not
// what the pin admits.
func subjectSlots(ty *vocabulary.Kind) bool {
	for _, name := range ty.PropOrder {
		if ty.Props[name].Subject {
			return true
		}
	}
	return false
}

// referenceAdmits reports whether a kind satisfies a reference's pin: no pin
// admits everything, a `kind:` pin names the kind, a `trait:` pin names a
// contract the kind implements.
func referenceAdmits(reg *vocabulary.Registry, p *vocabulary.Property, rt *vocabulary.Kind) bool {
	if p.ToTrait != "" {
		return rt.Implements(p.ToTrait)
	}
	if p.To == "" || p.To == vocabulary.ToAny {
		return true
	}
	return p.To == rt.Identity
}

// subjectHop is the ONE hop a pinned reference is allowed, and it exists because
// a sync body holds a MIRROR and not the thing: google writes an emailaddress
// path into a person-pinned reference, linear writes a user mirror into its
// assignee. Both mirrors are recordmapping SOURCES whose mapping targets the
// pinned kind, so the record the writer means is the source's own subject, and
// resolving it here is what lets a connector name what it actually has.
//
// ONE HOP, never a chain: the value's kind must itself be a mapping source for
// the pin, and the subject it resolves to is stored as written. A source record
// with no subject yet gets one here (matchOrMint, the same probe-or-mint every
// source write runs), and the subject recomputes from it — the source row is
// already stored, because this only ever runs inside somebody else's write.
func (t *txn) subjectHop(p *vocabulary.Property, target eref, rt *vocabulary.Kind) (eref, error) {
	mismatch := func() error {
		// A MIRROR WITH NO MAPPING is the one shape whose fix is not "write a
		// different path": the value names exactly the record the writer
		// means, and what is missing is the declaration saying which kind it
		// describes. Since record 49 that declaration is the target owner's,
		// so the message says whose it is rather than repeating the pin.
		if subjectSlots(rt) && len(t.ds.registry().MappingsFrom(rt.Identity)) == 0 {
			pin := p.To
			if pin == "" {
				pin = "trait " + p.ToTrait
			}
			return fmt.Errorf(
				"%w: reference points at %s, and no mapping is declared from %s onto %s: declare one in the package that owns %s",
				substrate.ErrValidation, rt.Identity, rt.Identity, pin, pin)
		}
		if p.ToTrait != "" {
			return fmt.Errorf("reference points at %s, which does not implement the pinned trait %s", rt.Identity, p.ToTrait)
		}
		// BOTH READINGS get named. The value parsed as a path at a kind the pin
		// does not admit, and it could equally have been meant as a bare id the
		// pin completes; coercion carried it here rather than guessing, and a
		// message naming one reading would leave the writer to find the other.
		return fmt.Errorf(
			"reference points at %s, but the declaration pins %s — write a %s path, or the bare id %q if that is what was meant",
			rt.Identity, p.To, p.To, vocabulary.RecordPath(target.Kind, target.ID))
	}
	// A source kind may carry a mapping per subject property (record 49), so
	// the hop collects the candidates the pin admits. TWO of them is not a
	// choice the engine may make for the writer: a `trait:` pin admitting both
	// of a mirror's targets means the value names one record and the
	// declaration means either, so it is refused naming both mappings rather
	// than resolved by load order.
	var admitted []*vocabulary.Mapping
	for _, cand := range t.ds.registry().MappingsFrom(rt.Identity) {
		to, known := t.ds.registry().ByIdentity(cand.To)
		if known && referenceAdmits(t.ds.registry(), p, to) {
			admitted = append(admitted, cand)
		}
	}
	if len(admitted) == 0 {
		return eref{}, mismatch()
	}
	if len(admitted) > 1 {
		return eref{}, fmt.Errorf(
			"%w: reference points at %s, and mappings %s and %s both reach a kind this declaration admits: name the subject record itself",
			substrate.ErrValidation, rt.Identity, admitted[0].Identity(), admitted[1].Identity())
	}
	m := admitted[0]
	// The hop reads a record, so the source has to be there whatever `mustExist`
	// says: there is no subject to resolve on a row that does not exist.
	row, err := t.loadRow(target, false)
	if err != nil {
		return eref{}, err
	}
	if row == nil {
		return eref{}, fmt.Errorf("%w: reference names %s, which does not exist",
			substrate.ErrNotFound, vocabulary.RecordPath(target.Kind, target.ID))
	}
	id, err := t.subjectOf(row, rt, m)
	if err != nil {
		return eref{}, err
	}
	return eref{Kind: m.To, ID: id}, nil
}
