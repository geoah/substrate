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
//   - required added while rows lack the property (the write path enforces
//     `required` on the merged row, and a declared `default` does not
//     backfill, so the rows that lack it now would be nonconforming and
//     unpatchable);
//   - a reference repointing its pin, gaining `mustExist:`, or narrowing one of
//     its declared link properties — each counted over the refs index
//     (referenceNarrowings), which reads every value shape and every depth.
//
// Additive changes — new type, new optional property, new enum value, new
// state, new transition, required removed, presentational keys — admit
// freely. Constraint refinements the ruling did not name (pattern, min/max)
// are not classified.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/geoah/substrate/internal/substrate"
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

// emptyJSONValues is the SQL spelling of emptyValue (write.go): the stored
// values that hold nothing, and so do not satisfy `required`. THE TWO MUST
// AGREE. The write path refuses a record whose value is one of these, so a
// guard that counted only the missing KEY would admit `required` onto a kind
// whose rows already hold "" and lock every later write to them out, with no
// way to migrate them under a declaration that refuses them.
const emptyJSONValues = `('null'::jsonb, '""'::jsonb, '[]'::jsonb, '{}'::jsonb)`

// countMissingPropQuery counts live rows of a type holding no value for the
// property: the key absent, or a value emptyJSONValues names.
const countMissingPropQuery = `SELECT count(*) FROM records
	WHERE kind = $1 AND deleted_at IS NULL
	AND (NOT props ? $2 OR props->$2 IN ` + emptyJSONValues + `)`

// hotColumns is the trait-bound property that occupies each own column, and the
// column it occupies. A hot property is NEVER a key in `props` (splitProps
// moves it out before the row is written), so counting the jsonb key would call
// every live row missing and refuse `required` on a temporal property forever,
// however faithfully the rows carry the instant.
var hotColumns = map[string]string{
	substrate.PropAt:     "at",
	substrate.PropEndsAt: "ends_at",
	substrate.PropDueAt:  "due_at",
}

// missingValueCount answers the live-row count for a property turning required:
// the jsonb key for an ordinary property, the trait-bound COLUMN for a hot one.
// It is the classification half of checkRequiredProps, which reads exactly the
// same two places (hotColumnOf, in write.go).
//
// NO DECLARATION REACHES THE HOT ARM TODAY, and it is here so the two halves
// cannot drift if one ever does: a kind that binds the trait may not declare the
// property at all ("`at` is the temporal trait's"), and a trait variant declares
// `name: datatype`, which has no room for `required`. Counting the jsonb key
// instead would call every live row missing and refuse the narrowing forever,
// however faithfully the rows carry the instant.
//
// The column is interpolated rather than bound because a column name cannot be
// a placeholder; it comes from the closed map above and never from a
// declaration.
func missingValueCount(ty *vocabulary.Kind, ident, pname string) (string, []any) {
	if col, hot := hotColumns[pname]; hot && ty.UsesHot(pname) {
		return fmt.Sprintf(
				"SELECT count(*) FROM records WHERE kind = $1 AND deleted_at IS NULL AND %s IS NULL", col),
			[]any{ident}
	}
	return countMissingPropQuery, []any{ident, pname}
}

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

// checkDeclaredDefaults holds every `default:` the touched authorities declare
// to the coercion a WRITE puts a value through, and answers one problem per
// default that would not survive it. The loader has already checked the
// literal's shape (parseDefault); what is left is the value's own rules (a
// pattern, a bound, an instant's range), which live with the write path. A kind
// whose default no create could store is refused here, once, instead of at
// every create of it.
func checkDeclaredDefaults(candidate *vocabulary.Registry, touched map[string]bool) []string {
	var problems []string
	for aname := range touched {
		a, ok := candidate.AuthorityByName(aname)
		if !ok || a == nil {
			continue
		}
		for _, tn := range a.KindOrder {
			ty := a.Kinds[tn]
			for _, pname := range ty.PropOrder {
				p := ty.Props[pname]
				if p.Default == nil {
					continue
				}
				if _, err := coerceValue(p, p.Default); err != nil {
					problems = append(problems, fmt.Sprintf("kind %s: property %q: default %v: %v",
						ty.Identity, pname, p.Default, err))
				}
			}
		}
	}
	sort.Strings(problems)
	return problems
}

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
				// A string retype to `enum` strands only the rows whose stored
				// value is outside the declared set, for the same reason: a set
				// the engine already held its writes to (a run's status, a
				// thread's mode) can be declared after the fact, the values
				// leading and the declaration following them.
				if stringToEnum(curP, candP) {
					q, args := valuesOutsidePath(ident, containerPath(nil, curP, pname), candP.ValueStrings())
					out = append(out, narrowing{
						format: fmt.Sprintf("type %s: property %q changes kind %s → %s while %%d live records hold a value outside %s; rewrite them first",
							ident, pname, from, to, quotedList(candP.ValueStrings())),
						query: q, args: args,
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
				out = append(out, narrowing{
					format: fmt.Sprintf("type %s: reference %q narrows its target to %s while %%d live records point elsewhere — repoint them first",
						ident, pname, candP.To),
					query: countRefOffTargetQuery, args: []any{ident, pname, candP.To},
				})
			}
			if curP.Datatype == vocabulary.DatatypeReference {
				out = append(out, referenceNarrowings(ident, pname, curP, candP)...)
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
				q, args := missingValueCount(candT, ident, pname)
				out = append(out, narrowing{
					format: fmt.Sprintf("type %s: property %q becomes required while %%d live records lack it — backfill them first", ident, pname),
					query:  q, args: args,
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
	for _, pname := range candT.PropOrder {
		candP := candT.Props[pname]
		if !candP.Required || candP.IsState() {
			continue
		}
		if _, existed := curT.Props[pname]; existed {
			continue // the `becomes required` case above owns it
		}
		q, args := missingValueCount(candT, ident, pname)
		out = append(out, narrowing{
			format: fmt.Sprintf("type %s: property %q is added as required while %%d live records lack it — backfill or delete them first",
				ident, pname),
			query: q, args: args,
		})
	}
	return out
}

// LINK COUNTS run over the `refs` index joined back to the SOURCE record, so a
// stranded value is counted the same way a stranded property value is: once per
// live record that holds it. The index is the faithful projection of the stored
// reference values (refs.go), which is what lets a count reach INSIDE a
// reference — at its link data, at what it points at — without a jsonb walk per
// declaration shape.
//
// The join names `repository` on both sides because the vocabulary door is also
// reachable on the maintenance role, which bypasses row level security; every
// other predicate here would then be right and the count would still be wrong.
//
// `path = ”` narrows to a kind's OWN reference, which is the only place link
// data may be declared (the loader refuses it inside an object).
const linkFrom = `FROM refs r
	JOIN records x ON x.repository = r.repository AND x.kind = r.src_kind AND x.id = r.src
	WHERE r.src_kind = $1 AND r.property = $2 AND r.path = '' AND x.deleted_at IS NULL`

// countLinkPropQuery counts live references carrying a value for one declared
// link property.
const countLinkPropQuery = `SELECT count(*) ` + linkFrom + ` AND r.props ? $3`

// countMissingLinkPropQuery counts live references NOT carrying it.
const countMissingLinkPropQuery = `SELECT count(*) ` + linkFrom + ` AND NOT r.props ? $3`

// countLinkPropValuesQuery counts live references whose value for one link
// property is in a set ($4 is a JSON array of the values).
const countLinkPropValuesQuery = `SELECT count(*) ` + linkFrom + `
	AND r.props ? $3 AND $4::jsonb @> jsonb_build_array(r.props->$3)`

// countLinkPropOutsideValuesQuery is its complement: the references holding a
// value the candidate's set does NOT admit.
const countLinkPropOutsideValuesQuery = `SELECT count(*) ` + linkFrom + `
	AND r.props ? $3 AND NOT ($4::jsonb @> jsonb_build_array(r.props->$3))`

// countLinkPropNonIntQuery counts live references whose value for a link
// property is not an integral number — the ones a retype to `int` strands.
const countLinkPropNonIntQuery = `SELECT count(*) ` + linkFrom + `
	AND r.props ? $3
	AND (jsonb_typeof(r.props->$3) <> 'number' OR (r.props->>$3)::numeric <> floor((r.props->>$3)::numeric))`

// countDanglingRefQuery counts live records whose reference names a record that
// is not there — exactly the rows `mustExist: true` would refuse from then on.
// It does NOT filter on `path`, because mustExist is declarable at every shape
// and depth a reference is.
// countRefOffTargetQuery counts the live records whose reference names a kind
// the candidate pin does not admit. It reads the refs INDEX rather than probing
// `props`, which is what lets one query answer for both value shapes (a bare
// path and the object a link-data reference stores) and for every depth: the
// index is the derivation of all of them.
const countRefOffTargetQuery = `SELECT count(DISTINCT r.src) FROM refs r
	JOIN records x ON x.repository = r.repository AND x.kind = r.src_kind AND x.id = r.src
	WHERE r.src_kind = $1 AND r.property = $2 AND x.deleted_at IS NULL
	  AND r.dst_kind <> $3`

const countDanglingRefQuery = `SELECT count(*) FROM refs r
	JOIN records x ON x.repository = r.repository AND x.kind = r.src_kind AND x.id = r.src
	WHERE r.src_kind = $1 AND r.property = $2 AND x.deleted_at IS NULL
	  AND NOT EXISTS (
		SELECT 1 FROM records t
		WHERE t.repository = r.repository AND t.kind = r.dst_kind AND t.id = r.dst)`

// referenceNarrowings classifies the diff of one reference's OWN keys: the link
// data it declares, and `mustExist`.
//
// `onDelete:` is deliberately absent. Adding, dropping or changing it strands
// nothing: it says what a future GC sweep does with the record, not what values
// are admissible, so every stored row stays exactly as writable as it was.
//
// Dropping the reference, taking `repeated:` away and repointing `kind:` are
// classified by the ordinary property loop this is called from: a reference is
// a property, and a container flip is a container flip.
func referenceNarrowings(ident, pname string, curP, candP *vocabulary.Property) []narrowing {
	var out []narrowing
	out = append(out, linkPropNarrowings(ident, pname, curP, candP)...)
	// `mustExist` added is a narrowing with a real count behind it: the
	// declaration admits only pointers at records that exist, and a stored
	// pointer at a record that never arrived (or was purged) is one no later
	// write to that row could re-assert.
	if !curP.MustExist && candP.MustExist {
		out = append(out, narrowing{
			format: fmt.Sprintf("type %s: reference %q requires its target to exist while %%d live references name a record that does not — repoint or clear them first",
				ident, pname),
			query: countDanglingRefQuery, args: []any{ident, pname},
		})
	}
	return out
}

// linkPropNarrowings classifies the declared `properties:` diff of one
// reference. A link property is a flat single value by construction (the
// loader's linkProp grammar), so this is the property loop without the
// containers, the states and the object recursion: dropped, retyped, an enum
// value removed, required added.
//
// The two retypes the record loop counts BY VALUE rather than by presence are
// counted by value here too, and for the same reason: a backfill can rewrite the
// stored values first and the declaration then follows them. A link property
// held to a stricter rule than the identical record property would be a
// difference with nothing behind it.
func linkPropNarrowings(ident, pname string, curP, candP *vocabulary.Property) []narrowing {
	var out []narrowing
	for _, lname := range curP.PropertyOrder {
		curL, candL := curP.Properties[lname], candP.Properties[lname]
		if candL == nil {
			out = append(out, narrowing{
				format: fmt.Sprintf("type %s: reference %q drops link property %q while %%d live references still carry it — null it on them first",
					ident, pname, lname),
				query: countLinkPropQuery, args: []any{ident, pname, lname},
			})
			continue
		}
		if curL.Datatype != candL.Datatype {
			switch {
			case candL.Datatype == vocabulary.DatatypeInt:
				out = append(out, narrowing{
					format: fmt.Sprintf("type %s: reference %q changes link property %q from %s to int while %%d live references hold values that are not integers — migrate them first",
						ident, pname, lname, curL.Datatype),
					query: countLinkPropNonIntQuery, args: []any{ident, pname, lname},
				})
			case stringToEnum(curL, candL):
				out = append(out, narrowing{
					format: fmt.Sprintf("type %s: reference %q changes link property %q from string to enum while %%d live references hold a value outside %s; rewrite them first",
						ident, pname, lname, quotedList(candL.ValueStrings())),
					query: countLinkPropOutsideValuesQuery,
					args:  []any{ident, pname, lname, jsonArray(candL.ValueStrings())},
				})
			default:
				out = append(out, narrowing{
					format: fmt.Sprintf("type %s: reference %q changes link property %q from %s to %s while %%d live references hold values of the old kind — migrate them first",
						ident, pname, lname, curL.Datatype, candL.Datatype),
					query: countLinkPropQuery, args: []any{ident, pname, lname},
				})
			}
			continue
		}
		if removed := removedStrings(curL.ValueStrings(), candL.ValueStrings()); len(removed) > 0 {
			out = append(out, narrowing{
				format: fmt.Sprintf("type %s: reference %q removes value(s) %s from link property %q while %%d live references hold one — rewrite them first",
					ident, pname, quotedList(removed), lname),
				query: countLinkPropValuesQuery, args: []any{ident, pname, lname, jsonArray(removed)},
			})
		}
		if !curL.Required && candL.Required {
			out = append(out, narrowing{
				format: fmt.Sprintf("type %s: reference %q makes link property %q required while %%d live references lack it — backfill them first",
					ident, pname, lname),
				query: countMissingLinkPropQuery, args: []any{ident, pname, lname},
			})
		}
	}
	// A link property the candidate ADDS as required strands every stored value,
	// for the same reason it does on a record: a write is refused without it and
	// no live value can carry a name no declaration had. The loop above walks the
	// CURRENT declaration, so a new name never reaches it.
	for _, lname := range candP.PropertyOrder {
		if !candP.Properties[lname].Required {
			continue
		}
		if _, existed := curP.Properties[lname]; existed {
			continue
		}
		out = append(out, narrowing{
			format: fmt.Sprintf("type %s: reference %q adds link property %q as required while %%d live references lack it — clear them or drop the requirement",
				ident, pname, lname),
			query: countMissingLinkPropQuery, args: []any{ident, pname, lname},
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
			// A LIST THAT IS NOT THERE HAS NO ELEMENTS, so an absent field and
			// a stored JSON null both expand to nothing. Without that arm
			// `jsonb_build_array(NULL)` boxes them as `[null]`, and a
			// complement predicate (valuesOutsidePath) counts that JSON null
			// as a value outside the declared set: a string→enum retype then
			// strands every row that left the field out, which for
			// recordpatchpolicy's `selector.ops` is the declared "empty means
			// all three". The keyed arm below cannot have the bug, because
			// jsonb_each of `{}` yields no rows. A stored scalar still boxes:
			// a value that IS there is what the box is for.
			//
			// One discriminant, so the arms cannot come to judge different
			// expressions.
			return fmt.Sprintf(
				"EXISTS (SELECT 1 FROM jsonb_array_elements("+
					"CASE coalesce(jsonb_typeof(%s), 'null') "+
					"WHEN 'array' THEN %s WHEN 'null' THEN '[]'::jsonb "+
					"ELSE jsonb_build_array(%s) END) %s WHERE %s)",
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

// valuesOutsidePath counts the live rows holding a value OUTSIDE the given set
// at the path: the rows a string retyped to enum actually strands. The
// complement of valuesAtPath, over the same container walk, so a repeated
// string is checked element by element and a keyed one value by value. A
// stored non-string is outside any set and counts too.
func valuesOutsidePath(ident string, path []fieldStep, values []string) (string, []any) {
	return countAtPath(ident, path, func(expr string, a *sqlArgs) string {
		return fmt.Sprintf("NOT (%s::jsonb @> %s)", a.add(jsonArray(values)), expr)
	})
}

// stringToEnum reports the one datatype flip the guards count by VALUE rather
// than by presence: a plain string becoming an enum in the same container.
// Every other flip changes what a stored value IS; this one only closes the
// set it may come from.
func stringToEnum(cur, cand *vocabulary.Property) bool {
	return cur.Datatype == vocabulary.DatatypeString && cand.Datatype == vocabulary.DatatypeEnum &&
		cur.Repeated == cand.Repeated && cur.Keyed == cand.Keyed
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
			// The string-to-enum tolerance a property gets, at depth: only the
			// rows holding a value outside the declared set are stranded.
			if stringToEnum(curF, candF) {
				q, args := valuesOutsidePath(ident, containerPath(path, curF, fname), candF.ValueStrings())
				out = append(out, narrowing{
					format: fmt.Sprintf("type %s: object %q field %q changes kind %s → %s while %%d live records hold a value outside %s; rewrite them first",
						ident, label, fname, kindShape(curF), kindShape(candF), quotedList(candF.ValueStrings())),
					query: q, args: args,
				})
				continue
			}
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
		if !curF.Required && candF.Required {
			q, args := fieldEmpty(ident, path, fname)
			out = append(out, narrowing{
				format: fmt.Sprintf("type %s: object %q field %q becomes required while %%d live records hold an object without a value for it; backfill them first",
					ident, label, fname),
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
	// A field the candidate ADDS as required strands every stored object at this
	// path at once: none of them can carry a name no declaration had. The loop
	// above walks the CURRENT fields, so this is the one shape it cannot see.
	for _, fname := range candP.FieldOrder {
		if !candP.Fields[fname].Required {
			continue
		}
		if _, existed := curP.Fields[fname]; existed {
			continue // the `becomes required` case above owns it
		}
		q, args := fieldEmpty(ident, path, fname)
		out = append(out, narrowing{
			format: fmt.Sprintf("type %s: object %q adds field %q as required while %%d live records hold an object without it; backfill or clear them first",
				ident, label, fname),
			query: q, args: args,
		})
	}
	return out
}

// fieldEmpty counts the live rows whose object at the path holds NO VALUE for
// the field: the key absent, or a value emptyJSONValues names. It is
// fieldPresence's complement and the depth-wise twin of countMissingPropQuery,
// so a field turning required is refused on exactly the rows the write path
// would then refuse.
//
// A row that does not carry the object at all is not counted: whether the
// object itself must be there is the enclosing property's own `required`, and
// counting an absent optional object here would refuse every field that ever
// turns required on one.
func fieldEmpty(ident string, path []fieldStep, key string) (string, []any) {
	return countAtPath(ident, path, func(expr string, a *sqlArgs) string {
		k := a.add(key)
		return fmt.Sprintf(
			"jsonb_typeof(%s) = 'object' AND (NOT %s ? %s OR %s->%s IN %s)",
			expr, expr, k, expr, k, emptyJSONValues)
	})
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
