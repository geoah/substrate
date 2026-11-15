package engine

// THE `referencedBy` ARM (decision record 0322): a list narrowed to the
// records a record of one other kind points at, or to the records none does.
// It is `referencing` the other way round, and it exists because a fact about
// a row is sometimes kept on another record: a person's mark of a thread
// says whether they archived it, and a list of threads has to leave those
// out before it pages, not after.
//
// The pointing records are found through the refs index from each listed
// row, so a row the read passes over costs one probe of refs_dst_idx and one
// read of each record that points at it through the named property, and the
// read never starts from the records that point. The property is a single
// reference: its refs row sits at one known site (path "", ord 0).
//
// Only a list takes the arm. Every other read that compiles a filter (the
// window read, both record aggregates, a mapping's where) refuses it by
// name here, and the doors that hold a caller to an allowlist of kinds
// refuse it before they reach here (readList), since the arm names a kind
// beside the filter's own.

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const filterReferencedBy = "filter.referencedBy"

// errReferencedByList is the refusal of the arm on a read that is not the
// records list.
func errReferencedByList() error {
	return fmt.Errorf("%w: %s narrows a list of records alone: this read does not take it", substrate.ErrValidation, filterReferencedBy)
}

// condReferencedBy adds the arm's predicate to a list. types are the kinds
// the filter admits, none for every kind.
func (ds *dataset) condReferencedBy(ctx context.Context, x dbx, b *builder, types []*vocabulary.Kind, rb *substrate.ReferencedBy) error {
	if !b.list {
		return errReferencedByList()
	}
	if rb.Kind == "" || rb.Property == "" {
		return fmt.Errorf("%w: %s names the kind that points and the reference property of it that points: {\"kind\": \"<kind>\", \"property\": \"<name>\"}",
			substrate.ErrValidation, filterReferencedBy)
	}
	reg := ds.registry()
	by, err := reg.Resolve(rb.Kind)
	if err != nil {
		return fmt.Errorf("%w: %s.kind: %w", substrate.ErrValidation, filterReferencedBy, err)
	}
	p, ok := by.Prop(rb.Property)
	if !ok || !scalarReference(p) {
		return fmt.Errorf("%w: %s.property: %s is not a single reference %s declares; the single references it declares are %s",
			substrate.ErrValidation, filterReferencedBy, rb.Property, by.Identity, wordsOr(singleReferences(by), "none"))
	}
	// A pair that can match nothing is named, as `implements` beside `kinds`
	// is: a property pinned to one kind never points at a row of another.
	if len(types) > 0 && !slices.ContainsFunc(types, func(t *vocabulary.Kind) bool { return referenceAdmits(reg, p, t) }) {
		return fmt.Errorf("%w: %s: %s.%s points at no kind in filter.kinds", substrate.ErrValidation, filterReferencedBy, by.Identity, rb.Property)
	}
	pointing := []*vocabulary.Kind{by}
	for _, name := range sortedKeys(rb.Properties) {
		// A name the kind does not hold would match no record, and under
		// `exists: false` that is every row: a misspelled condition must not
		// read as "none of them".
		if !ds.filterable(by, name) {
			return fmt.Errorf("%w: %s.properties: %s declares no %s", substrate.ErrValidation, filterReferencedBy, by.Identity, name)
		}
	}
	f, err := ds.resolveMe(ctx, pointing, substrate.Filter{Properties: rb.Properties})
	if err != nil {
		return err
	}
	conds, err := b.clauses(func() error {
		for _, name := range sortedKeys(f.Properties) {
			if err := ds.condProp(ctx, x, b, pointing, name, f.Properties[name]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	// The pointing record, from inside a subquery whose own FROM is the
	// records table: the conditions above are rendered against that name, as
	// every condition of a list is, so here they read the record that points
	// and never the row being listed.
	live := strings.Join(append([]string{`deleted_at IS NULL`}, conds...), ` AND `)
	kind := b.arg(by.Identity)

	// EVERY SUBQUERY BELOW ENDS IN `OFFSET 0`, which keeps Postgres from
	// turning it into a join: each stays a subplan run once for the row being
	// listed, walking refs_dst_idx from that row to the few records that
	// point at it. Left to join them, the planner prices "the records that
	// point, and meet the conditions" from its estimate of a jsonb condition,
	// which is one row whatever the truth; it then put that set on the inner
	// side of a nested loop and computed it again for every listed row: 3,000
	// threads against 2,700 archived marks did not answer in five minutes.
	// The subplan costs the same per row whatever the statistics say.
	pending, err := refsPending(ctx, x, []string{by.Identity}, rb.Property)
	if err != nil {
		return err
	}
	var pointed string
	if pending {
		// The refs rows may still follow the property's previous declaration
		// (reprojection.go): only the pointing record's own value says where
		// it points. `o` carries the listed row into the subquery, whose
		// FROM hides it.
		pointed = `EXISTS (SELECT 1 FROM (SELECT records.kind AS k, records.id AS i OFFSET 0) o WHERE EXISTS (` +
			`SELECT 1 FROM records WHERE kind = ` + kind + ` AND ` + live +
			` AND ` + referencePathSQL("props", rb.Property) + ` = ANY(ARRAY[o.k || '/' || o.i] || ARRAY(` +
			`SELECT o.k || '/' || f.former_id FROM former_ids f WHERE f.record_kind = o.k AND f.record_id = o.i)) OFFSET 0) OFFSET 0)`
	} else {
		site := `x.src_kind = ` + kind + ` AND x.property = ` + b.arg(rb.Property) + ` AND x.path = '' AND x.ord = 0` +
			` AND EXISTS (SELECT 1 FROM records WHERE kind = x.src_kind AND id = x.src AND ` + live + ` OFFSET 0)`
		// A pointer keeps the id it was written with, so one written before
		// a merge names the row by a former id (idsOf).
		pointed = `(EXISTS (SELECT 1 FROM refs x WHERE x.dst_kind = records.kind AND x.dst = records.id AND ` + site + ` OFFSET 0)` +
			` OR EXISTS (SELECT 1 FROM former_ids f JOIN refs x ON x.dst_kind = f.record_kind AND x.dst = f.former_id` +
			` WHERE f.record_kind = records.kind AND f.record_id = records.id AND ` + site + ` OFFSET 0))`
	}
	if rb.Exists != nil && !*rb.Exists {
		pointed = `NOT ` + pointed
	}
	b.add(pointed)
	return nil
}

// filterable reports whether a condition may name name on a record of kind:
// a property or a state it declares, a link property of one of its
// references, or a column every record carries.
func (ds *dataset) filterable(kind *vocabulary.Kind, name string) bool {
	if col, err := columnFor(name); err != nil || col != "" {
		return true
	}
	if states, _ := ds.stateKinds([]*vocabulary.Kind{kind}, name); len(states) > 0 {
		return true
	}
	head, _, _ := strings.Cut(name, ".")
	_, ok := kind.Prop(head)
	return ok
}

// singleReferences are the names of the single references a kind declares,
// sorted.
func singleReferences(kind *vocabulary.Kind) []string {
	var out []string
	for name, p := range kind.Props {
		if scalarReference(p) {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// wordsOr joins words with commas, or answers none for an empty list.
func wordsOr(words []string, none string) string {
	if len(words) == 0 {
		return none
	}
	return strings.Join(words, ", ")
}
