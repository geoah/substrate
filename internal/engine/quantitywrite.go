package engine

// QUANTITY WRITES THROUGH THE REPOSITORY'S UNIT GRAPH (decision 0186), the
// write half of the unit graph switch (WithUnitGraph). Coercion holds a
// written quantity to its shape alone (coercer.quantity); here, inside the
// write's transaction, every written quantity leaf (a top-level value, a
// list item, a keyed map's value, an object field at any depth, a reference's
// link property) has its unit resolved through the transaction's graph and
// stored as the unit's id, and is held to its declaration: the `unit` whose
// linear component it must belong to, the listed `units`, and the bounds,
// each compared exactly after converting the value into the bound's unit, as
// a difference where the declaration says `delta: true`. A value already
// stored is never checked again by a write that does not carry it.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// resolveQuantities resolves and checks every quantity the write carries.
// It does nothing while the switch is off.
func (t *txn) resolveQuantities(sp *applySpec) error {
	if !t.ds.svc.unitGraphOn || len(sp.props) == 0 {
		return nil
	}
	var g *vocabulary.UnitGraph
	var problems []string
	rowDelta := observedRowDelta(sp)
	for _, name := range sortedKeys(sp.props) {
		v := sp.props[name]
		p, ok := sp.ty.Prop(name)
		if !ok || v == nil || !holdsQuantity(p) {
			continue
		}
		if g == nil {
			at, err := t.unitGraph()
			if err != nil {
				return err
			}
			g = at.graph
		}
		delta := rowDelta && observedValueProp(sp.ty, name)
		problems = append(problems, resolveQuantityValues(g, p, v, "props."+name, delta)...)
	}
	if len(problems) > 0 {
		return &substrate.ValidationError{Problems: problems}
	}
	return nil
}

// resolveRestored resolves and checks one property's value a restoring put
// keeps (restore.go admitRestored), false where the graph refuses it. It
// admits everything while the switch is off.
func (t *txn) resolveRestored(sp *applySpec, name string, p *vocabulary.Property, v any) (bool, error) {
	if !t.ds.svc.unitGraphOn || v == nil || !holdsQuantity(p) {
		return true, nil
	}
	at, err := t.unitGraph()
	if err != nil {
		return false, err
	}
	delta := observedRowDelta(sp) && observedValueProp(sp.ty, name)
	return len(resolveQuantityValues(at.graph, p, v, "props."+name, delta)) == 0, nil
}

// observedRowDelta is an observed record's `delta` as the write leaves it:
// the written one, else the stored one.
func observedRowDelta(sp *applySpec) bool {
	if v, held := sp.props["delta"]; held {
		b, _ := v.(bool)
		return b
	}
	if sp.existing != nil && sp.existing.DeletedAt == nil {
		b, _ := sp.existing.Props["delta"].(bool)
		return b
	}
	return false
}

// observedValueProp reports whether name is the `value` of a kind binding
// the observed trait's single variant, whose row-level `delta` makes it a
// difference.
func observedValueProp(ty *vocabulary.Kind, name string) bool {
	b, ok := observedBinding(ty)
	return ok && b.Variant == vocabulary.ObservedSingle && name == "value"
}

// holdsQuantity reports whether a property's value may hold a quantity
// anywhere: a quantity itself, an object with one among its fields at any
// depth, or a reference with one among its link properties.
func holdsQuantity(p *vocabulary.Property) bool {
	switch p.Datatype {
	case vocabulary.DatatypeQuantity:
		return true
	case vocabulary.DatatypeObject:
		for _, f := range p.Fields {
			if holdsQuantity(f) {
				return true
			}
		}
	case vocabulary.DatatypeReference:
		for _, lp := range p.Properties {
			if holdsQuantity(lp) {
				return true
			}
		}
	}
	return false
}

// resolveQuantityValues walks one coerced value under its declaration,
// resolving each quantity leaf's unit to its id in place and answering every
// problem, each at its path.
func resolveQuantityValues(g *vocabulary.UnitGraph, p *vocabulary.Property, v any, path string, delta bool) []string {
	if v == nil {
		return nil
	}
	switch {
	case p.Keyed:
		m, _ := v.(map[string]any)
		var problems []string
		for _, k := range sortedKeys(m) {
			problems = append(problems, resolveQuantityOne(g, p, m[k], path+"."+k, delta)...)
		}
		return problems
	case p.Repeated:
		list, _ := v.([]any)
		var problems []string
		for i, item := range list {
			problems = append(problems, resolveQuantityOne(g, p, item, fmt.Sprintf("%s[%d]", path, i), delta)...)
		}
		return problems
	}
	return resolveQuantityOne(g, p, v, path, delta)
}

func resolveQuantityOne(g *vocabulary.UnitGraph, p *vocabulary.Property, v any, path string, delta bool) []string {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	switch p.Datatype {
	case vocabulary.DatatypeQuantity:
		if err := resolveQuantityLeaf(g, p.Quantity, m, delta); err != nil {
			return []string{path + "." + err.Error()}
		}
	case vocabulary.DatatypeObject:
		var problems []string
		for _, name := range sortedKeys(p.Fields) {
			problems = append(problems, resolveQuantityValues(g, p.Fields[name], m[name], path+"."+name, false)...)
		}
		return problems
	case vocabulary.DatatypeReference:
		var problems []string
		for _, name := range sortedKeys(p.Properties) {
			problems = append(problems, resolveQuantityValues(g, p.Properties[name], m[name], path+"."+name, false)...)
		}
		return problems
	}
	return nil
}

// resolveQuantityLeaf resolves one quantity value's unit to its id, in
// place, and holds it to the declaration. The error names the member it is
// about (`unit: …`, `value: …`), for the caller to prefix with the path.
func resolveQuantityLeaf(g *vocabulary.UnitGraph, spec *vocabulary.QuantitySpec, leaf map[string]any, rowDelta bool) error {
	written, _ := leaf[vocabulary.QuantityUnit].(string)
	id, ok := g.Resolve(written)
	if !ok {
		return fmt.Errorf("%s: %q names no unit of this repository", vocabulary.QuantityUnit, written)
	}
	leaf[vocabulary.QuantityUnit] = id
	digits, _ := leaf[vocabulary.QuantityValue].(string)
	value, ok := new(big.Rat).SetString(digits)
	if !ok {
		return fmt.Errorf("%s: %q is not a decimal", vocabulary.QuantityValue, digits)
	}
	return checkGraphQuantity(g, spec, id, value, rowDelta || (spec != nil && spec.Delta))
}

// checkGraphQuantity holds one value, its unit resolved to id, to a
// declaration: the anchor `unit` (the value's unit converts to it through
// linear conversions), the listed `units` (the value's unit is one of them,
// or the anchor), and the bounds, the value converted exactly into each
// bound's unit and refused where it cannot be. A difference converts by the
// factor alone.
func checkGraphQuantity(g *vocabulary.UnitGraph, spec *vocabulary.QuantitySpec, id string, value *big.Rat, delta bool) error {
	if spec == nil {
		return nil
	}
	anchor := ""
	if spec.Unit != "" {
		a, ok := g.Resolve(spec.Unit)
		if !ok {
			return fmt.Errorf("%s: the declaration's unit %q names no unit of this repository", vocabulary.QuantityUnit, spec.Unit)
		}
		anchor = a
		if _, _, err := g.Linear(id, anchor, false); err != nil {
			return fmt.Errorf("%s: %s does not convert to %s, the declaration's unit: %w", vocabulary.QuantityUnit, id, anchor, err)
		}
	}
	if len(spec.Units) > 0 && id != anchor {
		admitted := false
		var listed []string
		for _, s := range spec.Units {
			u, ok := g.Resolve(s)
			if !ok {
				continue
			}
			listed = append(listed, u)
			if u == id {
				admitted = true
			}
		}
		if !admitted {
			if anchor != "" {
				listed = append([]string{anchor}, listed...)
			}
			return fmt.Errorf("%s: %s is not one of the declared units %s", vocabulary.QuantityUnit, id, strings.Join(listed, ", "))
		}
	}
	for _, b := range []struct {
		bound *vocabulary.QuantityBound
		name  string
		below bool
	}{{spec.Min, "min", true}, {spec.Max, "max", false}} {
		if b.bound == nil {
			continue
		}
		in, err := convertExact(g, value, id, b.bound.Unit, delta)
		if err != nil {
			return fmt.Errorf("%s: cannot compare with %s %s: %w", vocabulary.QuantityValue, b.name, graphQuantityText(g, b.bound.Value, b.bound.Unit), err)
		}
		cmp := in.Cmp(b.bound.Rat())
		if b.below && cmp < 0 {
			return fmt.Errorf("%s: must be >= %s", vocabulary.QuantityValue, graphQuantityText(g, b.bound.Value, b.bound.Unit))
		}
		if !b.below && cmp > 0 {
			return fmt.Errorf("%s: must be <= %s", vocabulary.QuantityValue, graphQuantityText(g, b.bound.Value, b.bound.Unit))
		}
	}
	return nil
}

// convertExact converts an exact value between two units of one linear
// component, the target spelled any way the graph reads.
func convertExact(g *vocabulary.UnitGraph, value *big.Rat, from, to string, delta bool) (*big.Rat, error) {
	toID, ok := g.Resolve(to)
	if !ok {
		return nil, fmt.Errorf("%q names no unit of this repository", to)
	}
	factor, offset, err := g.Linear(from, toID, delta)
	if err != nil {
		return nil, err
	}
	out := new(big.Rat).Mul(value, factor)
	return out.Add(out, offset), nil
}

// graphQuantityText is a value and a unit spelling as a title prints them
// under the graph: "40 kg", "90°".
func graphQuantityText(g *vocabulary.UnitGraph, value, unit string) string {
	if id, ok := g.Resolve(unit); ok {
		unit = id
	}
	return vocabulary.FormatQuantityIn(g, map[string]any{vocabulary.QuantityValue: value, vocabulary.QuantityUnit: unit})
}

// checkDeclaredUnits holds, under the switch, the quantity declarations a
// transaction publishes to the graph it leaves: every unit spelling a
// changed or new kind names (`unit`, `units`, a bound, a default) names a
// unit of the repository, and every unit a property admits by name (its
// `unit` and listed `units`) converts to each of its bounds. A kind whose
// declaration the transaction left alone is not judged again, so a graph
// edit is the use guard's (unitgraph.go) and an unrelated apply is never
// refused for an older kind.
func (t *txn) checkDeclaredUnits() error {
	if !t.ds.svc.unitGraphOn || t.writeReg == nil || t.migrating || t.replaying {
		return nil
	}
	live := t.ds.registry()
	var g *vocabulary.UnitGraph
	var problems []string
	for _, k := range t.writeReg.Kinds() {
		if was, ok := live.ByIdentity(k.Identity); ok && jsonEqual(was.Definition, k.Definition) {
			continue
		}
		for _, name := range sortedKeys(k.Props) {
			if !holdsQuantity(k.Props[name]) {
				continue
			}
			if g == nil {
				at, err := t.unitGraph()
				if err != nil {
					return err
				}
				g = at.graph
			}
			problems = append(problems, declaredUnitProblems(g, k.Identity+" data.properties."+name, k.Props[name])...)
		}
	}
	if len(problems) > 0 {
		return &substrate.ValidationError{Problems: problems}
	}
	return nil
}

// declaredUnitProblems judges one property's quantity declarations at any
// depth against the graph.
func declaredUnitProblems(g *vocabulary.UnitGraph, where string, p *vocabulary.Property) []string {
	var out []string
	for _, name := range sortedKeys(p.Fields) {
		out = append(out, declaredUnitProblems(g, where+".fields."+name, p.Fields[name])...)
	}
	for _, name := range sortedKeys(p.Properties) {
		out = append(out, declaredUnitProblems(g, where+".properties."+name, p.Properties[name])...)
	}
	q := p.Quantity
	if p.Datatype != vocabulary.DatatypeQuantity || q == nil {
		return out
	}
	resolve := func(key, s string) (string, bool) {
		id, ok := g.Resolve(s)
		if !ok {
			out = append(out, fmt.Sprintf("%s.%s: %q names no unit of this repository", where, key, s))
		}
		return id, ok
	}
	var named []string
	anchor := ""
	if q.Unit != "" {
		if id, ok := resolve("unit", q.Unit); ok {
			named = append(named, id)
			anchor = id
		}
	}
	for _, s := range q.Units {
		id, ok := resolve("units", s)
		if !ok {
			continue
		}
		if anchor != "" {
			if _, _, err := g.Linear(id, anchor, false); err != nil {
				out = append(out, fmt.Sprintf("%s.units: the listed unit %s does not convert to the declaration's unit %s, so no value in it is admitted: %v",
					where, id, anchor, err))
				continue
			}
		}
		named = append(named, id)
	}
	for _, b := range []struct {
		key   string
		bound *vocabulary.QuantityBound
	}{{"min", q.Min}, {"max", q.Max}} {
		if b.bound == nil {
			continue
		}
		bid, ok := resolve(b.key, b.bound.Unit)
		if !ok {
			continue
		}
		for _, u := range named {
			if _, _, err := g.Linear(u, bid, q.Delta); err != nil {
				out = append(out, fmt.Sprintf("%s.%s: the admitted unit %s does not convert to the bound's unit %s, so the bound would refuse every value in it: %v",
					where, b.key, u, bid, err))
			}
		}
	}
	return append(out, graphDefaultProblems(g, where, p)...)
}

// graphDefaultProblems holds a declared default to everything a create
// that omits the property holds the value to: its shape, then every
// quantity in it resolved and held to its declaration through the graph
// (admitted units, bounds, `delta`). Graph-mode coercion checks the shape
// alone, so a default only this check refuses is one every such create
// would.
func graphDefaultProblems(g *vocabulary.UnitGraph, where string, p *vocabulary.Property) []string {
	if p.Default == nil || !holdsQuantity(p) {
		return nil
	}
	raw, err := json.Marshal(p.Default)
	if err != nil {
		return []string{fmt.Sprintf("%s.default: %v", where, err)}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return []string{fmt.Sprintf("%s.default: %v", where, err)}
	}
	coerced, err := coercer{graphUnits: true}.value(p, v)
	if err != nil {
		return []string{fmt.Sprintf("%s.default: %v", where, err)}
	}
	return resolveQuantityValues(g, p, coerced, where+".default", false)
}

// resolveDefault resolves the units of a coerced default a conversion or a
// restore stores, under the switch, as a written value's are.
func (t *txn) resolveDefault(p *vocabulary.Property, v any) error {
	if !t.ds.svc.unitGraphOn || !holdsQuantity(p) {
		return nil
	}
	at, err := t.unitGraph()
	if err != nil {
		return err
	}
	if problems := resolveQuantityValues(at.graph, p, v, "default", false); len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}
