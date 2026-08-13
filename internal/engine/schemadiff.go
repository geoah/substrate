package engine

// Evolution-with-data. Schema admission classifies
// the definition diff of every touched kind against its CURRENTLY
// STORED definition and refuses a NARROWING change while live rows exist that
// the new definition would strand or contradict — the same posture as
// refuse-with-instances, extended from type-drop to the definition itself,
// with the count taken inside the batch transaction and one query per
// narrowed property. The refused classes:
//
//   - property dropped (a rename without machinery is a drop — a declared
//     `renamedFrom:` is recorded but NOT yet acted on, so it refuses the same
//     way, naming the reservation);
//   - property kind changed (repeated flips count: a list is not a scalar);
//   - enum value removed while rows hold it;
//   - state removed while rows occupy it (a state property dropped or turned
//     scalar counts as a kind change);
//   - required added while rows lack the property (`required:` stays a form
//     hint on writes, but adding it to a stored declaration makes existing
//     rows nonconforming, so it narrows).
//
// Additive changes — new type, new optional property, new enum value, new
// state, new transition, required removed, presentational keys — admit
// freely. Constraint refinements the ruling did not name (pattern, min/max)
// are not classified.

import (
	"encoding/json"
	"fmt"

	"github.com/geoah/substrate/internal/vocabulary"
)

// narrowing is one refused diff found by classification: the guard message
// (format carries one %d for the live-row count) and the count query that
// runs inside the batch transaction.
type narrowing struct {
	format string
	query  string
	args   []any
}

// countPropQuery counts live rows of a type carrying a value for a property
// (a nulled property's key is deleted from props, so presence is a value).
const countPropQuery = `SELECT count(*) FROM records WHERE kind = $1 AND deleted_at IS NULL AND props ? $2`

// countMissingPropQuery counts live rows of a type NOT carrying the property.
const countMissingPropQuery = `SELECT count(*) FROM records WHERE kind = $1 AND deleted_at IS NULL AND NOT props ? $2`

// countStateQuery counts live rows holding any state for a machine.
const countStateQuery = `SELECT count(*) FROM records WHERE kind = $1 AND deleted_at IS NULL AND states ? $2`

// countStateValuesQuery counts live rows whose machine sits in one of the
// given states ($3 is a JSON array of state names).
const countStateValuesQuery = `SELECT count(*) FROM records
	WHERE kind = $1 AND deleted_at IS NULL AND states ? $2 AND $3::jsonb @> (states->$2)`

// countPropValuesQuery counts live rows whose property holds one of the given
// values ($3 is a JSON array), covering both the scalar and the repeated
// stored shape.
const countPropValuesQuery = `SELECT count(*) FROM records
	WHERE kind = $1 AND deleted_at IS NULL AND props ? $2
	  AND CASE WHEN jsonb_typeof(props->$2) = 'array'
	       THEN EXISTS (SELECT 1 FROM jsonb_array_elements(props->$2) e WHERE $3::jsonb @> e.value)
	       ELSE $3::jsonb @> (props->$2) END`

// countObjectFieldQuery counts live rows whose object property ($2) carries a
// given field ($3), covering both the scalar-object and repeated-object stored
// shape: a dropped or kind-changed object field strands those rows.
const countObjectFieldQuery = `SELECT count(*) FROM records
	WHERE kind = $1 AND deleted_at IS NULL AND props ? $2
	  AND CASE WHEN jsonb_typeof(props->$2) = 'array'
	       THEN EXISTS (SELECT 1 FROM jsonb_array_elements(props->$2) e WHERE e ? $3)
	       ELSE props->$2 ? $3 END`

// countRefOutsideQuery counts live rows whose reference property ($2) points at
// a kind OTHER than the newly required target ($3, a full identity), covering
// both the scalar and repeated stored shape.
//
// It reads `kind` and nothing else. The query used to reconstruct the referent
// as `kind || '.' || authority`, which had matched a long-dead stored shape:
// normalizeReference writes {kind: <full identity>, id} and has never written
// an `authority` key (references.go), so every comparison was against
// `<identity>.` and the guard counted every live row or none — it was dead
// either way, and silently.
const countRefOutsideQuery = `SELECT count(*) FROM records
	WHERE kind = $1 AND deleted_at IS NULL AND props ? $2
	  AND CASE WHEN jsonb_typeof(props->$2) = 'array'
	       THEN EXISTS (SELECT 1 FROM jsonb_array_elements(props->$2) e
	                    WHERE COALESCE(e->>'kind','') <> $3)
	       ELSE COALESCE(props->$2->>'kind','') <> $3 END`

// classifyNarrowings walks every type present in BOTH the current and the
// candidate registry (dropped types are refuse-with-instances' whole-type
// count) across the touched authorities and returns the narrowing diffs. Pure
// classification — the counts run later, inside the batch transaction.
func classifyNarrowings(current, candidate *vocabulary.Registry, touched map[string]bool) []narrowing {
	return classifyNarrowingsExcept(current, candidate, touched, nil)
}

// classifyNarrowingsExcept is classifyNarrowings with the kind identities the
// caller is NOT rewriting. The boot upgrade needs it: it re-projects per
// DECLARATION, leaving any whose stored version is the same or newer exactly as
// it stands (seed.go), and a kind it will not touch must never refuse the boot.
func classifyNarrowingsExcept(
	current, candidate *vocabulary.Registry,
	touched map[string]bool,
	skip map[string]bool,
) []narrowing {
	var out []narrowing
	for _, aname := range sortedKeys(touched) {
		cur, _ := current.AuthorityByName(aname)
		cand, _ := candidate.AuthorityByName(aname)
		if cur == nil || cand == nil {
			continue
		}
		for _, tn := range cur.KindOrder {
			if candT := cand.Kinds[tn]; candT != nil {
				if skip[candT.Identity] {
					continue
				}
				out = append(out, typeNarrowings(cur.Kinds[tn], candT)...)
			}
		}
	}
	return out
}

// typeNarrowings classifies one type's property-level diff.
func typeNarrowings(curT, candT *vocabulary.Kind) []narrowing {
	var out []narrowing
	ident := curT.Identity
	for _, pname := range curT.PropOrder {
		curP := curT.Props[pname]
		candP := candT.Props[pname]
		switch {
		case candP == nil:
			// Dropped — or renamed away, which without the (reserved) rewrite
			// is the same narrowing wearing a declared intent.
			if to := renamedTo(candT, pname); to != "" {
				out = append(out, narrowing{
					format: fmt.Sprintf("type %s: property %q renamed to %q, but renamedFrom is reserved and not yet acted on — %%d live records still carry %q; migrate them first",
						ident, pname, to, pname),
					query: countPropQuery, args: []any{ident, pname},
				})
				continue
			}
			if curP.IsState() {
				out = append(out, narrowing{
					format: fmt.Sprintf("type %s: state property %q dropped while %%d live records hold a state — resolve them first", ident, pname),
					query:  countStateQuery, args: []any{ident, pname},
				})
				continue
			}
			out = append(out, narrowing{
				format: fmt.Sprintf("type %s: property %q dropped while %%d live records still carry it — null it on them first", ident, pname),
				query:  countPropQuery, args: []any{ident, pname},
			})
		case curP.IsState() != candP.IsState():
			// A machine turned value (or a value turned machine) is a kind
			// change; the stranded side is wherever the old shape lives.
			q, what := countPropQuery, "a value"
			if curP.IsState() {
				q, what = countStateQuery, "a state"
			}
			out = append(out, narrowing{
				format: fmt.Sprintf("type %s: property %q changes kind (state and value do not convert) while %%d live records hold %s — migrate them first",
					ident, pname, what),
				query: q, args: []any{ident, pname},
			})
		case curP.IsState():
			if removed := removedStrings(curP.Machine.States, candP.Machine.States); len(removed) > 0 {
				out = append(out, narrowing{
					format: fmt.Sprintf("type %s: state property %q removes state(s) %s while %%d live records occupy one — transition them first",
						ident, pname, quotedList(removed)),
					query: countStateValuesQuery, args: []any{ident, pname, jsonArray(removed)},
				})
			}
		default:
			if curP.Datatype != candP.Datatype || curP.Repeated != candP.Repeated {
				from, to := kindShape(curP), kindShape(candP)
				out = append(out, narrowing{
					format: fmt.Sprintf("type %s: property %q changes kind %s → %s while %%d live records hold values of the old kind — migrate them first",
						ident, pname, from, to),
					query: countPropQuery, args: []any{ident, pname},
				})
				continue
			}
			if removed := removedStrings(curP.ValueStrings(), candP.ValueStrings()); len(removed) > 0 {
				out = append(out, narrowing{
					format: fmt.Sprintf("type %s: property %q removes value(s) %s while %%d live records hold one — rewrite them first",
						ident, pname, quotedList(removed)),
					query: countPropValuesQuery, args: []any{ident, pname, jsonArray(removed)},
				})
			}
			// A reference that narrows its `to:` target (unconstrained → a type,
			// or one type → another) strands stored references pointing elsewhere
			//.
			if curP.Datatype == vocabulary.DatatypeReference && refTargetNarrows(curP.To, candP.To) {
				out = append(out, narrowing{
					format: fmt.Sprintf("type %s: reference %q narrows its target to %s while %%d live records point elsewhere — repoint them first",
						ident, pname, candP.To),
					query: countRefOutsideQuery, args: []any{ident, pname, candP.To},
				})
			}
			// An object property that drops or kind-changes a declared field
			// strands rows holding that field.
			if curP.Datatype == vocabulary.DatatypeObject {
				out = append(out, objectFieldNarrowings(ident, pname, curP, candP)...)
			}
			if !curP.Required && candP.Required {
				out = append(out, narrowing{
					format: fmt.Sprintf("type %s: property %q becomes required while %%d live records lack it — backfill them first", ident, pname),
					query:  countMissingPropQuery, args: []any{ident, pname},
				})
			}
		}
	}
	// A property the candidate ADDS as required is the same stranding as one
	// that becomes required, and was the one shape of it nothing classified: the
	// loop above walks the CURRENT type's properties, so a name that did not
	// exist before never reached it. Live rows cannot carry a property no
	// declaration had, so every one of them is missing it the moment it is
	// declared required.
	//
	// This is how a relationship that MOVES — an edge becoming a required
	// reference — announces itself. Dropping the edge is unguarded (edges are
	// not diffed at all) and the reference is a new name, so without this the
	// declaration lands quietly and every existing row is left pointing the old
	// way, invisible to every read written against the new one.
	for _, pname := range candT.PropOrder {
		candP := candT.Props[pname]
		if !candP.Required || candP.IsState() {
			continue
		}
		if _, existed := curT.Props[pname]; existed {
			continue // the `becomes required` case above owns it
		}
		out = append(out, narrowing{
			format: fmt.Sprintf("type %s: property %q is added as required while %%d live records lack it — backfill or delete them first",
				ident, pname),
			query: countMissingPropQuery, args: []any{ident, pname},
		})
	}
	return out
}

// narrowingGuards runs each narrowing's count inside the batch transaction
// and renders a guard line for every one that strands live rows.
func (t *txn) narrowingGuards(narrowings []narrowing) ([]string, error) {
	var guards []string
	for _, n := range narrowings {
		var count int64
		if err := t.row(n.query, n.args...).Scan(&count); err != nil {
			return nil, err
		}
		if count > 0 {
			guards = append(guards, fmt.Sprintf(n.format, count))
		}
	}
	return guards, nil
}

// refTargetNarrows reports whether a reference's `to:` target moved to a
// STRICTER constraint: an unconstrained target (empty or "any") pinned to a
// type, or one type replaced by a different one. Widening (→ any) or an
// unchanged target does not narrow.
func refTargetNarrows(cur, cand string) bool {
	if cand == "" || cand == vocabulary.ToAny {
		return false
	}
	if cur == "" || cur == vocabulary.ToAny {
		return true
	}
	return cur != cand
}

// objectFieldNarrowings classifies an object property's field-level diff: a
// dropped field, or a field whose kind/repetition changed, each stranding the
// rows that carry that field.
func objectFieldNarrowings(ident, pname string, curP, candP *vocabulary.Property) []narrowing {
	var out []narrowing
	for _, fname := range curP.FieldOrder {
		curF := curP.Fields[fname]
		candF := candP.Fields[fname]
		switch {
		case candF == nil:
			out = append(out, narrowing{
				format: fmt.Sprintf("type %s: object %q drops field %q while %%d live records still carry it — null it on them first",
					ident, pname, fname),
				query: countObjectFieldQuery, args: []any{ident, pname, fname},
			})
		case curF.Datatype != candF.Datatype || curF.Repeated != candF.Repeated:
			out = append(out, narrowing{
				format: fmt.Sprintf("type %s: object %q field %q changes kind %s → %s while %%d live records hold the old kind — migrate them first",
					ident, pname, fname, kindShape(curF), kindShape(candF)),
				query: countObjectFieldQuery, args: []any{ident, pname, fname},
			})
		}
	}
	return out
}

// renamedTo reports the candidate property (if any) that declares the given
// name as its renamedFrom.
func renamedTo(candT *vocabulary.Kind, from string) string {
	for _, pname := range candT.PropOrder {
		if candT.Props[pname].RenamedFrom == from {
			return pname
		}
	}
	return ""
}

// removedStrings lists the members of cur that cand no longer carries, in
// cur's order.
func removedStrings(cur, cand []string) []string {
	keep := make(map[string]bool, len(cand))
	for _, s := range cand {
		keep[s] = true
	}
	var out []string
	for _, s := range cur {
		if !keep[s] {
			out = append(out, s)
		}
	}
	return out
}

// jsonArray renders values as a JSON array literal for the ::jsonb casts.
func jsonArray(values []string) string {
	raw, _ := json.Marshal(values)
	return string(raw)
}

// quotedList renders values for a guard message.
func quotedList(values []string) string {
	out := ""
	for i, v := range values {
		if i > 0 {
			out += ", "
		}
		out += fmt.Sprintf("%q", v)
	}
	return out
}

// kindShape renders a property's kind for a guard message, list-aware.
func kindShape(p *vocabulary.Property) string {
	if p.Repeated {
		return "repeated " + string(p.Datatype)
	}
	return string(p.Datatype)
}
