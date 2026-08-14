package engine

import (
	"fmt"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// validateReferences is the existence gate for reference-typed properties
// , the twin of validateBlobRefs: coercion checked the SHAPE and
// left the value at most a {type, id} pair, and this registry-aware pass —
// taken inside the write transaction — resolves the referent TYPE, refuses an
// unknown one, refuses a `to:` mismatch, and rewrites the stored value to the
// canonical {authority, type, id} triple. It does NOT require the referent RECORD
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
// canonical pair, descending the declaration's containers (keyed map, list) and
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

// normalizeReference resolves one reference value's referent kind against the
// registry, checks a `to:` constraint, and returns the canonical {kind, id}
// pair — the record reference an edge target wears too. A bare local name
// resolves here.
func (t *txn) normalizeReference(p *vocabulary.Property, v any) (any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("a reference is a {kind, id} object")
	}
	kind, _ := m["kind"].(string)
	id, _ := m["id"].(string)
	if id == "" {
		return nil, fmt.Errorf("a reference needs an id")
	}
	if kind == "" {
		return nil, fmt.Errorf("a reference to any kind needs an explicit kind")
	}
	rt, err := t.ds.resolveType(kind)
	if err != nil {
		return nil, fmt.Errorf("referent kind %q is unknown", kind)
	}
	if p.To != "" && p.To != vocabulary.ToAny && rt.Identity != p.To {
		return nil, fmt.Errorf("reference points at %s, not %s", rt.Identity, p.To)
	}
	return map[string]any{"kind": rt.Identity, "id": id}, nil
}
