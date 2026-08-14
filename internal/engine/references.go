package engine

import (
	"fmt"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// validateReferences is the existence gate for reference-typed properties
// , the twin of validateBlobRefs: coercion checked the SHAPE and
// left the value a record PATH, and this registry-aware pass —
// taken inside the write transaction — resolves the referent KIND, refuses an
// unknown one, refuses a pin mismatch, and rewrites the stored value to the
// canonical path. It does NOT require the referent RECORD
// to exist: a reference is a typed POINTER, not a graph edge, so it may name a
// row that is not present yet (a trigger's `callable` names a function the
// same batch installs; the trigger's OWN admission resolves the callable
// record separately). Mutating props here, before the row's property map is
// built, is what makes the stored value canonical on every write path.
//
// It reaches every POSITION a reference is declared at, not only a kind's own
// properties: inside an object, inside a repeated object's elements, inside a
// keyed map's values, to the declared depth. A position it did not reach would
// store whatever the writer sent — a bare local name where every reader expects
// a resolved identity.
func (t *txn) validateReferences(ty *vocabulary.Kind, props map[string]any) error {
	var problems []string
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
		return &substrate.ValidationError{Problems: problems}
	}
	return nil
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

// storedReferencePath reads a STORED reference value as its record path,
// tolerating the released dialect-1 {kind, id} pair.
//
// POST-RUNG THIS IS UNREACHABLE: the promotion rewrites every trigger holding a
// pair (canonicalizeTriggerCallables), and every released store passes through
// it at first open, so a dialect-2 store has none. It stays as stated defense,
// so a dispatcher reading a row mid-migration reads it correctly rather than
// skipping a live trigger — the failure mode a silent nil would cause here is
// delivery that stops without saying so.
//
// Nothing AUTHORS a pair: the write path refuses it by name (coerceReference).
func storedReferencePath(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		kind, _ := t["kind"].(string)
		id, _ := t["id"].(string)
		if kind == "" || id == "" {
			return ""
		}
		return vocabulary.RecordPath(kind, id)
	}
	return ""
}

// normalizeReference resolves one reference value's referent kind against the
// registry, checks the declaration's `kind:` pin, and returns the canonical
// RECORD PATH — "<kind>/<id>", one flat string. A bare local kind name resolves
// to its full identity here, so the stored path is spelled one way whatever the
// writer typed.
func (t *txn) normalizeReference(p *vocabulary.Property, v any) (any, error) {
	s, ok := v.(string)
	if !ok || s == "" {
		return nil, fmt.Errorf(`a reference is a "<kind>/<id>" path string`)
	}
	pinned := p.To != "" && p.To != vocabulary.ToAny
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
	// Both sides get named: the pin says one kind and the value spells another,
	// and a message carrying only one of them leaves the writer to guess which
	// end to change.
	if pinned && rt.Identity != p.To {
		return nil, fmt.Errorf("reference points at %s, but the declaration pins %s", rt.Identity, p.To)
	}
	return vocabulary.RecordPath(rt.Identity, id), nil
}
