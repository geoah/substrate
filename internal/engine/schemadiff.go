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
//   - property kind changed (container flips count: a list is not a scalar and
//     a keyed map is neither), at every declared level of an object's fields;
//   - a keyed map's key contract tightened while rows hold the map;
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
	"database/sql"
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

// countNonIntPropQuery counts live rows whose value for a property is not an
// integral number — the rows a scalar retype to `int` actually strands.
const countNonIntPropQuery = `SELECT count(*) FROM records
	WHERE kind = $1 AND deleted_at IS NULL AND props ? $2
	AND (jsonb_typeof(props->$2) <> 'number' OR (props->>$2)::numeric <> floor((props->>$2)::numeric))`

// countStateQuery counts live rows holding any state for a machine.
const countStateQuery = `SELECT count(*) FROM records WHERE kind = $1 AND deleted_at IS NULL AND states ? $2`

// countStateValuesQuery counts live rows whose machine sits in one of the
// given states ($3 is a JSON array of state names).
const countStateValuesQuery = `SELECT count(*) FROM records
	WHERE kind = $1 AND deleted_at IS NULL AND states ? $2 AND $3::jsonb @> (states->$2)`

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
			// A container flip is a kind change: a map is not a list and neither
			// is a scalar, and no stored value converts between them.
			if curP.Datatype != candP.Datatype || curP.Repeated != candP.Repeated || curP.Keyed != candP.Keyed {
				from, to := kindShape(curP), kindShape(candP)
				// A scalar retype to `int` strands only the rows whose stored
				// value is not already an integral number: a backfill can
				// rewrite the values first and the declaration then follows
				// them (the declaration-version migration is the one that
				// needed this). Every other flip keeps the presence count —
				// nothing converts a list into a map or a string into a bool.
				if candP.Datatype == vocabulary.DatatypeInt &&
					!curP.Repeated && !curP.Keyed && !candP.Repeated && !candP.Keyed {
					out = append(out, narrowing{
						format: fmt.Sprintf("type %s: property %q changes kind %s → %s while %%d live records hold values that are not integers — migrate them first",
							ident, pname, from, to),
						query: countNonIntPropQuery, args: []any{ident, pname},
					})
					continue
				}
				out = append(out, narrowing{
					format: fmt.Sprintf("type %s: property %q changes kind %s → %s while %%d live records hold values of the old kind — migrate them first",
						ident, pname, from, to),
					query: countPropQuery, args: []any{ident, pname},
				})
				continue
			}
			// Every value count below walks the property's own container, so a
			// keyed enum and a repeated one are counted in their own shape rather
			// than compared as whole containers against a value list.
			if removed := removedStrings(curP.ValueStrings(), candP.ValueStrings()); len(removed) > 0 {
				q, args := valuesAtPath(ident, containerPath(nil, curP, pname), removed)
				out = append(out, narrowing{
					format: fmt.Sprintf("type %s: property %q removes value(s) %s while %%d live records hold one — rewrite them first",
						ident, pname, quotedList(removed)),
					query: q, args: args,
				})
			}
			// A reference that narrows its `kind:` pin (unconstrained → a kind,
			// or one type → another) strands stored references pointing elsewhere
			//.
			if curP.Datatype == vocabulary.DatatypeReference && refTargetNarrows(curP.To, candP.To) {
				q, args := refOutsidePath(ident, containerPath(nil, curP, pname), candP.To)
				out = append(out, narrowing{
					format: fmt.Sprintf("type %s: reference %q narrows its target to %s while %%d live records point elsewhere — repoint them first",
						ident, pname, candP.To),
					query: q, args: args,
				})
			}
			if keyPatternTightens(curP, candP) {
				q, args := keysOutsidePattern(ident, mapPath(nil, pname),
					vocabulary.KeyPatternRegexp(candP.KeyPattern))
				out = append(out, narrowing{
					format: fmt.Sprintf("type %s: property %q tightens its keys to %s while %%d live records hold a key it refuses — rekey them first",
						ident, pname, candP.KeyPattern),
					query: q, args: args,
				})
			}
			// An object property that drops or kind-changes a declared field
			// strands rows holding that field, at every declared level.
			if curP.Datatype == vocabulary.DatatypeObject {
				out = append(out, objectFieldNarrowings(ident,
					[]fieldStep{{key: pname, repeated: curP.Repeated, keyed: curP.Keyed}}, curP, candP)...)
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

// sqlReader is the read surface the refuse-breakage counts run over: the
// batch transaction on the apply door, the bare pool on the read-only upgrade
// preview (PlanBundleUpgrade). Same queries either way; a count only one
// door could run would let the preview and the refusal disagree.
type sqlReader interface {
	row(sqlText string, args ...any) *sql.Row
	query(sqlText string, args ...any) (*sql.Rows, error)
}

// narrowingGuards runs each narrowing's count and renders a guard line for
// every one that strands live rows.
func narrowingGuards(q sqlReader, narrowings []narrowing) ([]string, error) {
	var guards []string
	for _, n := range narrowings {
		var count int64
		if err := q.row(n.query, n.args...).Scan(&count); err != nil {
			return nil, err
		}
		if count > 0 {
			guards = append(guards, fmt.Sprintf(n.format, count))
		}
	}
	return guards, nil
}

// droppedTypeGuards renders refuse-with-instances: a kind the candidate stops
// declaring, counted while live rows exist.
func droppedTypeGuards(q sqlReader, droppedTypes []string) ([]string, error) {
	var guards []string
	for _, ident := range droppedTypes {
		var n int64
		if err := q.row(`SELECT count(*) FROM records WHERE kind = $1 AND deleted_at IS NULL`, ident).Scan(&n); err != nil {
			return nil, err
		}
		if n > 0 {
			guards = append(guards, fmt.Sprintf("type %s has %d live records — delete or migrate them first (identities are never reused)",
				ident, n))
		}
	}
	return guards, nil
}

// refTargetNarrows reports whether a reference's `kind:` pin moved to a
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

// fieldStep is one step down a declared path: the key, and the CONTAINER the
// value under it is stored in. The container is what a count has to walk — a
// repeated object is a jsonb array of members, a keyed map a jsonb object of
// them — so the path carries the declared shape, never a guess from the data.
type fieldStep struct {
	key      string
	repeated bool
	keyed    bool
}

// pathLabel renders a path for a guard message: the keys, dotted. A one-step
// path reads as the property's own name, which is what the level-1 guards said
// before they could recurse.
func pathLabel(path []fieldStep) string {
	out := ""
	for i, s := range path {
		if i > 0 {
			out += "."
		}
		out += s.key
	}
	return out
}

// sqlArgs numbers a generated query's placeholders. The keys it binds are
// loader-validated camelCase, but they are bound rather than interpolated all
// the same: a declaration is data from an install, and one query builder that
// interpolates is one habit away from a query that matters.
type sqlArgs struct{ args []any }

func (a *sqlArgs) add(v any) string {
	a.args = append(a.args, v)
	return fmt.Sprintf("$%d", len(a.args))
}

// countAtPath renders a live-row count over a declared path: it descends the
// path's containers with one jsonb notch per level and applies `final` to the
// jsonb expression addressing the value at the end of it.
//
// This is the recursive form of the level-1 query it replaces, which read
// `CASE WHEN jsonb_typeof(props->$2) = 'array' THEN EXISTS(...) ELSE ... END`.
// The tolerance survives the generalization: a repeated level accepts a stored
// SCALAR object too, because a repeated-flip narrowing is refused by counting
// exactly the rows that still hold the other shape.
func countAtPath(ident string, path []fieldStep, final func(expr string, a *sqlArgs) string) (string, []any) {
	a := &sqlArgs{}
	kind := a.add(ident)
	head := a.add(path[0].key)
	pred := descendPath("(props->"+head+")", path, 0, a, final)
	return fmt.Sprintf(
		"SELECT count(*) FROM records WHERE kind = %s AND deleted_at IS NULL AND props ? %s AND %s",
		kind, head, pred), a.args
}

// descendPath renders the predicate over the value at path[i], addressed by
// expr: the container is expanded to its members, then either the next key is
// taken from a member or `final` closes over it.
//
// Every value expression it hands out is PARENTHESIZED. Postgres gives `->` and
// `@>` the same precedence and left-associates them, so `$3::jsonb @> props->$2`
// parses as `($3::jsonb @> props) -> $2` — a boolean indexed by a key, which is
// the error the enum count failed with the first time.
func descendPath(expr string, path []fieldStep, i int, a *sqlArgs, final func(string, *sqlArgs) string) string {
	step := path[i]
	member, wrap := expr, func(inner string) string { return inner }
	switch {
	case step.repeated:
		alias := fmt.Sprintf("e%d", i)
		member = alias
		wrap = func(inner string) string {
			return fmt.Sprintf(
				"EXISTS (SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(%s) = 'array' THEN %s ELSE jsonb_build_array(%s) END) %s WHERE %s)",
				expr, expr, expr, alias, inner)
		}
	case step.keyed:
		alias := fmt.Sprintf("k%d", i)
		member = alias + ".value"
		wrap = func(inner string) string {
			return fmt.Sprintf(
				"EXISTS (SELECT 1 FROM jsonb_each(CASE WHEN jsonb_typeof(%s) = 'object' THEN %s ELSE '{}'::jsonb END) %s WHERE %s)",
				expr, expr, alias, inner)
		}
	}
	if i == len(path)-1 {
		return wrap(final(member, a))
	}
	return wrap(descendPath(fmt.Sprintf("(%s->%s)", member, a.add(path[i+1].key)), path, i+1, a, final))
}

// fieldPresence counts the live rows carrying `key` inside the container the
// path names — the rows a dropped, retyped or re-boxed field strands.
func fieldPresence(ident string, path []fieldStep, key string) (string, []any) {
	return countAtPath(ident, path, func(expr string, a *sqlArgs) string {
		return fmt.Sprintf("jsonb_typeof(%s) = 'object' AND %s ? %s", expr, expr, a.add(key))
	})
}

// refOutsidePath counts the live rows whose reference at the end of the path
// points at a kind OTHER than the newly required target. The reference's own
// container is the path's last step, so a repeated or keyed reference is walked
// element by element — a top-level keyed reference read as one value would
// compare the whole MAP's absent `kind` and count every populated row.
//
// The value must BE a canonical reference before its kind is compared. An
// absent optional reference inside a present object is not a row pointing
// elsewhere, and counting it as one refused the legal `kind: any` → concrete
// evolution for every row that simply left the field out. That is what the
// string test buys: a missing value is jsonb NULL, whose jsonb_typeof is not
// 'string', so it is not counted.
//
// A stored reference is ONE flat path ("<kind>/<id>", references.go), so
// "points at the target" is a PREFIX: the value begins with the target kind and
// a slash. Compared with `left(…)` rather than LIKE because a kind reference is
// data here — no pattern of the target's can leak into the operator.
func refOutsidePath(ident string, path []fieldStep, target string) (string, []any) {
	return countAtPath(ident, path, func(expr string, a *sqlArgs) string {
		arg := a.add(target)
		return fmt.Sprintf("jsonb_typeof(%s) = 'string' AND left(%s #>> '{}', length(%s) + 1) <> %s || '/'",
			expr, expr, arg, arg)
	})
}

// valuesAtPath counts the live rows holding one of the given values at the path
// — the rows a removed enum value strands, wherever the enum is declared.
//
// The path's last step carries the enum's own container, so a list is compared
// element by element and a keyed map value by value. The query this replaces
// dispatched on the STORED type instead, because it had no declared shape to
// walk, and it knew only two shapes: a keyed map of enums compared as one object
// against the removed-values array, matched nothing, and the removal was
// admitted with every stranded row invisible.
func valuesAtPath(ident string, path []fieldStep, values []string) (string, []any) {
	return countAtPath(ident, path, func(expr string, a *sqlArgs) string {
		return fmt.Sprintf("%s::jsonb @> %s", a.add(jsonArray(values)), expr)
	})
}

// keysOutsidePattern counts the live rows whose keyed map at the path holds a
// key the CANDIDATE contract refuses — not every row that holds a map. The
// pattern is the loader's own grammar, handed over by vocabulary.KeyPatternRegexp
// so the count and CheckKey cannot disagree about what a legal key is.
func keysOutsidePattern(ident string, path []fieldStep, re string) (string, []any) {
	return countAtPath(ident, path, func(expr string, a *sqlArgs) string {
		return fmt.Sprintf(
			"jsonb_typeof(%s) = 'object' AND EXISTS (SELECT 1 FROM jsonb_object_keys(%s) k WHERE k !~ %s)",
			expr, expr, a.add(re))
	})
}

// containerPath appends the leaf step a VALUE count needs: the key, plus the
// declared container to expand, so the count reaches each element of a list and
// each value of a map.
func containerPath(path []fieldStep, p *vocabulary.Property, key string) []fieldStep {
	return append(append([]fieldStep(nil), path...), fieldStep{key: key, repeated: p.Repeated, keyed: p.Keyed})
}

// mapPath appends the leaf step a KEY count needs: the map itself, unexpanded,
// because the keys are what is being examined.
func mapPath(path []fieldStep, key string) []fieldStep {
	return append(append([]fieldStep(nil), path...), fieldStep{key: key})
}

// objectFieldNarrowings classifies one object level's field diff, recursing to
// the declared depth: a dropped field, a field whose kind, container or key
// contract changed, and a reference field that narrows its target — each
// stranding the rows that carry that field, counted where it actually sits.
func objectFieldNarrowings(ident string, path []fieldStep, curP, candP *vocabulary.Property) []narrowing {
	var out []narrowing
	label := pathLabel(path)
	for _, fname := range curP.FieldOrder {
		curF := curP.Fields[fname]
		candF := candP.Fields[fname]
		if candF == nil {
			q, args := fieldPresence(ident, path, fname)
			out = append(out, narrowing{
				format: fmt.Sprintf("type %s: object %q drops field %q while %%d live records still carry it — null it on them first",
					ident, label, fname),
				query: q, args: args,
			})
			continue
		}
		if curF.Datatype != candF.Datatype || curF.Repeated != candF.Repeated || curF.Keyed != candF.Keyed {
			q, args := fieldPresence(ident, path, fname)
			out = append(out, narrowing{
				format: fmt.Sprintf("type %s: object %q field %q changes kind %s → %s while %%d live records hold the old kind — migrate them first",
					ident, label, fname, kindShape(curF), kindShape(candF)),
				query: q, args: args,
			})
			continue
		}
		if keyPatternTightens(curF, candF) {
			q, args := keysOutsidePattern(ident, mapPath(path, fname),
				vocabulary.KeyPatternRegexp(candF.KeyPattern))
			out = append(out, narrowing{
				format: fmt.Sprintf("type %s: object %q field %q tightens its keys to %s while %%d live records hold a key it refuses — rekey them first",
					ident, label, fname, candF.KeyPattern),
				query: q, args: args,
			})
		}
		// A field's enum set narrows exactly as a property's does, and nothing
		// classified it: a value removed from a field at any depth used to land
		// with every row still holding it, in any container.
		if removed := removedStrings(curF.ValueStrings(), candF.ValueStrings()); len(removed) > 0 {
			q, args := valuesAtPath(ident, containerPath(path, curF, fname), removed)
			out = append(out, narrowing{
				format: fmt.Sprintf("type %s: object %q field %q removes value(s) %s while %%d live records hold one — rewrite them first",
					ident, label, fname, quotedList(removed)),
				query: q, args: args,
			})
		}
		next := containerPath(path, curF, fname)
		if curF.Datatype == vocabulary.DatatypeReference && refTargetNarrows(curF.To, candF.To) {
			q, args := refOutsidePath(ident, next, candF.To)
			out = append(out, narrowing{
				format: fmt.Sprintf("type %s: object %q reference %q narrows its target to %s while %%d live records point elsewhere — repoint them first",
					ident, label, fname, candF.To),
				query: q, args: args,
			})
		}
		if curF.Datatype == vocabulary.DatatypeObject {
			out = append(out, objectFieldNarrowings(ident, next, curF, candF)...)
		}
	}
	return out
}

// keyPatternTightens reports whether a keyed map's declared key contract moved
// to a STRICTER one: none → a pattern, or one pattern replaced by another. A key
// is not rewritable in place (the whole map has to be rewritten), so a stored key
// the new contract refuses strands its row. Dropping the contract admits
// everything the old one did and cannot narrow.
//
// Tightening is only a narrowing for the rows that actually hold a refused KEY,
// which is what the count asks: a map whose every key already conforms is not
// stranded by the declaration catching up with it.
func keyPatternTightens(curP, candP *vocabulary.Property) bool {
	if !candP.Keyed || candP.KeyPattern == "" {
		return false
	}
	return curP.KeyPattern != candP.KeyPattern
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

// kindShape renders a property's kind for a guard message, container-aware: the
// container is part of the kind, because nothing converts a list into a map or
// either into a scalar.
func kindShape(p *vocabulary.Property) string {
	switch {
	case p.Repeated:
		return "repeated " + string(p.Datatype)
	case p.Keyed:
		return "keyed " + string(p.Datatype)
	}
	return string(p.Datatype)
}
