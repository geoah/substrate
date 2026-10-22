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

// prepareObserved shapes an observed record's write before the merge: a
// written `measure`, and on a packed record a written `stats`, are DROPPED,
// not refused as a managed property's would be, because the engine computes
// both and a function that writes back a record it read carries the stored
// ones; and a packed record's written `unit` is rewritten to its table code
// (`bpm` is stored as `/min`), the way a quantity's unit is. A unit the table
// cannot read is left for CheckObserved to refuse by name.
//
// Under the unit graph switch a packed record's `unit` is resolved through
// the transaction's graph and stored as the unit's id, and a spelling the
// graph does not hold is refused here.
func (t *txn) prepareObserved(sp *applySpec) error {
	b, ok := observedBinding(sp.ty)
	if !ok {
		return nil
	}
	delete(sp.props, vocabulary.ObservedMeasure)
	if b.Variant != vocabulary.ObservedPacked {
		return nil
	}
	delete(sp.props, vocabulary.ObservedStats)
	u, ok := sp.props["unit"].(string)
	if !ok || u == "" {
		return nil
	}
	if !t.ds.svc.unitGraphOn {
		if code, err := vocabulary.CanonicalUnit(u); err == nil {
			sp.props["unit"] = code
		}
		return nil
	}
	at, err := t.unitGraph()
	if err != nil {
		return err
	}
	id, ok := at.graph.Resolve(u)
	if !ok {
		return &substrate.ValidationError{Problems: []string{fmt.Sprintf("props.unit: %q names no unit of this repository", u)}}
	}
	sp.props["unit"] = id
	return nil
}

// checkObserved holds the merged row of an observed kind to the trait's
// rules, refuses a revision below the stored one on a kind that binds
// `observed` or `revised`, holds a metric's and a source's record to the
// `metric` and `source` traits' rules, and stamps an observed record's
// measure and a packed record's stats. A write that leaves a tombstone (a
// finalizer release) is not held: it changes no observed value.
func (t *txn) checkObserved(sp *applySpec, before, row *erow, accepted *[]string) error {
	b, observed := observedBinding(sp.ty)
	_, revised := sp.ty.Binding(vocabulary.TraitRevisedCore)
	_, metric := sp.ty.Binding(vocabulary.TraitMetricCore)
	_, source := sp.ty.Binding(vocabulary.TraitSourceCore)
	if (!observed && !revised && !metric && !source) || (row.DeletedAt != nil && !sp.resurrect) {
		return nil
	}
	var problems []string
	// Under the unit graph switch the units are read through the
	// transaction's graph (decision 0186) and no measure is stamped.
	var units *vocabulary.UnitGraph
	if t.ds.svc.unitGraphOn && (observed || metric) {
		at, err := t.unitGraph()
		if err != nil {
			return err
		}
		units = at.graph
	}
	if observed {
		_, result := sp.ty.Binding(vocabulary.TraitResultCore)
		problems = vocabulary.CheckObserved(vocabulary.ObservedRecord{
			Variant: b.Variant, At: row.At, EndsAt: row.EndsAt, Props: row.Props, Result: result, Units: units,
		})
	}
	switch {
	case metric && units != nil:
		problems = append(problems, vocabulary.CheckMetricIn(units, row.Props)...)
	case metric:
		problems = append(problems, vocabulary.CheckMetric(row.Props)...)
	}
	if source {
		reg := t.declarations()
		problems = append(problems, vocabulary.CheckSourcePriorities(row.Props, func(kind string) bool {
			k, ok := reg.ByIdentity(kind)
			return ok && k.Implements(vocabulary.TraitMetricCore)
		})...)
	}
	// A revision is compared with the live row it rewrites. A create and a
	// put onto a tombstone start a record afresh, so neither has one to fall
	// below.
	if (observed || revised) && before != nil && before.DeletedAt == nil && !sp.resurrect {
		problems = append(problems, vocabulary.RevisionProblems(before.Props, row.Props)...)
	}
	if len(problems) > 0 {
		return &substrate.ValidationError{Problems: problems}
	}
	if !observed {
		return nil
	}
	measure := ""
	if units == nil {
		measure = observedMeasure(sp.ty, b.Variant, row.Props)
	}
	stampManaged(row, vocabulary.ObservedMeasure, measure, accepted)
	if b.Variant != vocabulary.ObservedPacked || row.At == nil {
		return nil
	}
	stats, err := vocabulary.TraceStats(*row.At, row.Props)
	if err != nil {
		return fmt.Errorf("%w: props.stats: %w", substrate.ErrValidation, err)
	}
	stampManaged(row, vocabulary.ObservedStats, stats, accepted)
	return nil
}

// stampManaged sets a managed property the engine computed, or removes it
// where the engine computed none, and records the change among the write's
// accepted properties.
func stampManaged(row *erow, name string, v any, accepted *[]string) {
	if s, isString := v.(string); isString && s == "" {
		v = nil
	}
	if jsonEqual(row.Props[name], v) {
		return
	}
	if v == nil {
		delete(row.Props, name)
	} else {
		row.Props[name] = v
	}
	*accepted = append(*accepted, name)
}

// observedMeasure is the measure an observed record's unit resolves to under
// its kind's declaration (decision 0153): a single value's unit under the
// `value` property's `measures:`, a trace's unit under the table alone, and
// `temperature-difference` for a temperature written as a difference. ""
// where the record holds codes, or a unit the table cannot place (which the
// rules refuse before this runs).
func observedMeasure(ty *vocabulary.Kind, variant string, props map[string]any) string {
	delta, _ := props["delta"].(bool)
	if variant == vocabulary.ObservedPacked {
		return measureFor(stringProp(props, "unit"), "", delta)
	}
	q, ok := props["value"].(map[string]any)
	if !ok {
		return ""
	}
	unit, _ := q[vocabulary.QuantityUnit].(string)
	return measureFor(unit, declaredMeasure(ty), delta)
}

// coerceProp coerces one declared property's written value, as coerceValue
// does, except that a packed observed record's `values` and `codes` keep a
// null item: unavailable from that point on (decision 0158). Every other list
// refuses a null, as it always has.
func (c coercer) prop(ty *vocabulary.Kind, name string, p *vocabulary.Property, v any) (any, error) {
	if !p.Repeated || !vocabulary.ObservedNullable(ty, name) {
		return c.value(p, v)
	}
	items, ok := v.([]any)
	if !ok {
		return c.value(p, v)
	}
	out := make([]any, len(items))
	for i, item := range items {
		if item == nil {
			continue
		}
		cv, err := c.scalar(p, item)
		if err != nil {
			return nil, fmt.Errorf("[%d]: %w", i, err)
		}
		out[i] = cv
	}
	return out, nil
}
