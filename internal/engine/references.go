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
func (t *txn) validateReferences(ty *vocabulary.Kind, props map[string]any) error {
	var problems []string
	for _, name := range sortedKeys(props) {
		p, ok := ty.Prop(name)
		if !ok || p.Datatype != vocabulary.DatatypeReference {
			continue
		}
		v := props[name]
		if v == nil {
			continue
		}
		if p.Repeated {
			list, ok := v.([]any)
			if !ok {
				problems = append(problems, fmt.Sprintf("props.%s: expected a list of references", name))
				continue
			}
			for i := range list {
				nv, err := t.normalizeReference(p, list[i])
				if err != nil {
					problems = append(problems, fmt.Sprintf("props.%s[%d]: %v", name, i, err))
					continue
				}
				list[i] = nv
			}
		} else {
			nv, err := t.normalizeReference(p, v)
			if err != nil {
				problems = append(problems, fmt.Sprintf("props.%s: %v", name, err))
				continue
			}
			props[name] = nv
		}
	}
	if len(problems) > 0 {
		return &substrate.ValidationError{Problems: problems}
	}
	return nil
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
