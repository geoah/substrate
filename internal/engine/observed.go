package engine

// THE OBSERVED TRAIT ON THE WRITE PATH (decisions 0154 to 0158, 0162).
//
// A kind that binds core's `observed` is held, on every write that leaves a
// live row (a create, a put, a patch, a decided change request, a function's
// effect), to the rules its property list cannot state; the rules themselves
// are vocabulary.CheckObserved, a function of the merged row alone. Three
// things only the write path can do live here: a packed record's `unit` is
// stored as its table code whatever alias the writer sent, its `stats` is
// computed from its values and a writer's own is dropped, and a `revision`
// is compared with the stored one so a stale delivery never overwrites a
// newer result.

import (
	"fmt"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// observedBinding is the kind's binding of core's `observed`, if it binds it.
func observedBinding(ty *vocabulary.Kind) (vocabulary.TraitBinding, bool) {
	return ty.Binding(vocabulary.TraitObservedCore)
}

// prepareObserved shapes a packed record's write before the merge: a written
// `stats` is DROPPED, not refused as a managed property's would be, because
// the engine computes it from the values and a function that writes back a
// trace it read carries the stored one; and a written `unit` is rewritten to
// its table code (`bpm` is stored as `{beats}/min`), the way a quantity's
// unit is. A unit the table cannot read is left for CheckObserved to refuse
// by name.
func prepareObserved(sp *applySpec) {
	b, ok := observedBinding(sp.ty)
	if !ok || b.Variant != vocabulary.ObservedPacked {
		return
	}
	delete(sp.props, vocabulary.ObservedStats)
	if u, ok := sp.props["unit"].(string); ok && u != "" {
		if code, err := vocabulary.CanonicalUnit(u); err == nil {
			sp.props["unit"] = code
		}
	}
}

// checkObserved holds the merged row of an observed kind to the trait's
// rules, refuses a revision below the stored one, and stamps a packed
// record's stats. A write that leaves a tombstone (a finalizer release) is
// not held: it changes no observed value.
func (t *txn) checkObserved(sp *applySpec, before, row *erow, accepted *[]string) error {
	b, ok := observedBinding(sp.ty)
	if !ok || (row.DeletedAt != nil && !sp.resurrect) {
		return nil
	}
	problems := vocabulary.CheckObserved(vocabulary.ObservedRecord{
		Variant: b.Variant, At: row.At, EndsAt: row.EndsAt, Props: row.Props,
		Declares: func(name string) bool {
			_, declared := sp.ty.Prop(name)
			return declared
		},
	})
	// A revision is compared with the live row it rewrites. A create and a
	// put onto a tombstone start a record afresh, so neither has one to fall
	// below.
	if before != nil && before.DeletedAt == nil && !sp.resurrect {
		if stored, had := vocabulary.ObservedRevisionOf(before.Props); had {
			written, has := vocabulary.ObservedRevisionOf(row.Props)
			switch {
			case !has:
				problems = append(problems, fmt.Sprintf(
					"props.revision: the stored revision %d is never removed; a rewrite carries one at least as high", stored))
			case written < stored:
				problems = append(problems, fmt.Sprintf(
					"props.revision: %d is below the stored revision %d; a rewrite never lowers it, so this is a stale delivery", written, stored))
			}
		}
	}
	if len(problems) > 0 {
		return &substrate.ValidationError{Problems: problems}
	}
	if b.Variant != vocabulary.ObservedPacked || row.At == nil {
		return nil
	}
	stats, err := vocabulary.TraceStats(*row.At, row.Props)
	if err != nil {
		return fmt.Errorf("%w: props.stats: %w", substrate.ErrValidation, err)
	}
	if !jsonEqual(row.Props[vocabulary.ObservedStats], stats) {
		row.Props[vocabulary.ObservedStats] = stats
		*accepted = append(*accepted, vocabulary.ObservedStats)
	}
	return nil
}

// coerceProp coerces one declared property's written value, as coerceValue
// does, except that a packed observed record's `values` and `codes` keep a
// null item: unavailable from that point on (decision 0158). Every other list
// refuses a null, as it always has.
func coerceProp(ty *vocabulary.Kind, name string, p *vocabulary.Property, v any) (any, error) {
	if !p.Repeated || !vocabulary.ObservedNullable(ty, name) {
		return coerceValue(p, v)
	}
	items, ok := v.([]any)
	if !ok {
		return coerceValue(p, v)
	}
	out := make([]any, len(items))
	for i, item := range items {
		if item == nil {
			continue
		}
		cv, err := coerceScalar(p, item)
		if err != nil {
			return nil, fmt.Errorf("[%d]: %w", i, err)
		}
		out[i] = cv
	}
	return out, nil
}
