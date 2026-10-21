package engine

// QUANTITY VALUES: the write coercion's shape check, the filter's entry point
// and the narrowing guard, for `type: quantity` (vocabulary/quantity.go says
// what a quantity value is, vocabulary/unitgraph.go how a unit converts, and
// quantitygraph.go and quantitywrite.go read and write values through the
// repository's unit graph).
//
// Every comparison is exact. SQL never rounds a converted value: a filter
// converts its operand into each stored unit in Go, as a rational n/d, and asks
// SQL `value * d - n`, whose sign is the sign of the stored value minus the
// operand however the two units relate, because every linear conversion is
// increasing. An ordering reads the value converted to its component's root
// to orderPlaces decimal places, where the one division rounds; an order is a
// key, and nothing compares against it.

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// coerceQuantity holds one quantity value to its shape: exactly the three
// members, the value exact digits written as a string, a unit spelling, and a
// comparator only where the property declares `comparator: true`. The unit is
// the repository's to read: the write transaction resolves it and holds it to
// the declaration and its bounds (quantitywrite.go resolveQuantities).
func (c coercer) quantity(p *vocabulary.Property, v any) (any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf(`a quantity value is an object {%s: "72.5", %s: "kg"}`,
			vocabulary.QuantityValue, vocabulary.QuantityUnit)
	}
	for k := range m {
		switch k {
		case vocabulary.QuantityValue, vocabulary.QuantityUnit, vocabulary.QuantityComparator:
		default:
			return nil, fmt.Errorf("a quantity value holds %s, %s and %s, and %q is none of them",
				vocabulary.QuantityValue, vocabulary.QuantityUnit, vocabulary.QuantityComparator, k)
		}
	}
	raw, held := m[vocabulary.QuantityValue]
	if !held || raw == nil {
		return nil, fmt.Errorf(`a quantity value needs %s, its digits as a string ("72.5")`, vocabulary.QuantityValue)
	}
	s, isString := raw.(string)
	if !isString {
		return nil, fmt.Errorf(`%s is written as a string ("72.5"): a bare JSON number rides float64 and may already be rounded`,
			vocabulary.QuantityValue)
	}
	digits, err := vocabulary.CanonicalDecimal(s)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", vocabulary.QuantityValue, err)
	}
	unit, _ := m[vocabulary.QuantityUnit].(string)
	if unit == "" {
		return nil, fmt.Errorf("a quantity value needs %s, a unit of this repository (kg, Cel, mmol/L)", vocabulary.QuantityUnit)
	}
	out := map[string]any{vocabulary.QuantityValue: digits, vocabulary.QuantityUnit: unit}
	if c, held := m[vocabulary.QuantityComparator]; held && c != nil {
		if p.Quantity == nil || !p.Quantity.Comparator {
			return nil, fmt.Errorf("%s: this property states values exactly; it admits a comparator only where it declares `comparator: true`",
				vocabulary.QuantityComparator)
		}
		cs, _ := c.(string)
		if !slices.Contains(vocabulary.QuantityComparators, cs) {
			return nil, fmt.Errorf("%s is one of %s", vocabulary.QuantityComparator, strings.Join(vocabulary.QuantityComparators, ", "))
		}
		out[vocabulary.QuantityComparator] = cs
	}
	return out, nil
}

// quantityText is a quantity property's value as the search index reads it:
// its digits and unit id (vocabulary.QuantitySearchText), or a list's values
// joined.
func quantityText(v any) string {
	format := vocabulary.QuantitySearchText
	if list, ok := v.([]any); ok {
		var parts []string
		for _, item := range list {
			if s := format(item); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	}
	return format(v)
}

// --- filters ---

// quantityDecl is one candidate kind's declaration of a quantity name.
type quantityDecl struct {
	kind string
	spec *vocabulary.QuantitySpec
	// delta says the name is the `value` of a kind binding the observed
	// trait's single variant, whose row's `delta: true` makes its value a
	// difference.
	delta bool
}

// deltaSQL is true on a row whose `delta` says its value is a difference.
const deltaSQL = `(props->>'delta') = 'true'`

// condQuantity filters one quantity property through the repository's unit
// graph (quantitygraph.go). A comparison's operand is itself a quantity
// value, and it holds only inside the operand's linear component: `lt`
// {155, lb} is the values below 155 pounds, a stored 72.4 kg among them, and
// a stored value in a unit no linear conversion reaches never matches. `in` is any of
// several such equalities. A bounded value (`<5`) matches a comparison only
// where it certainly holds for every value the bound admits, so `<5` matches
// `lt 5` and `lt 10`, never `lt 3` and never `eq`; and `in` is refused where a
// value may be bounded, because no set certainly contains a half-line. A
// quantity has no words and no text prefix, and `contains` is for a list.
func (ds *dataset) condQuantity(ctx context.Context, x dbx, b *builder, types []*vocabulary.Kind, key string, c substrate.Cond) error {
	switch {
	case c.Prefix != "", c.Match != "":
		return fmt.Errorf("%w: %s is a quantity: filter it with eq, in or the comparison operators", substrate.ErrValidation, key)
	case c.Contains != nil:
		return fmt.Errorf("%w: %s is one quantity, and contains is for a list", substrate.ErrValidation, key)
	}
	decls := quantityDecls(ds.readKinds(types), key)
	bounded := slices.ContainsFunc(decls, func(d quantityDecl) bool { return d.spec != nil && d.spec.Comparator })
	if len(c.In) > 0 && bounded {
		return fmt.Errorf("%w: %s may hold a bounded value (<0.5), which no set of values certainly contains: compare it with eq or the comparison operators",
			substrate.ErrValidation, key)
	}
	return ds.condQuantityGraph(ctx, x, b, types, key, c, decls, bounded)
}

// quantityDecls are the candidate kinds declaring the name as one quantity.
func quantityDecls(types []*vocabulary.Kind, name string) []quantityDecl {
	var out []quantityDecl
	for _, t := range types {
		p, ok := t.Prop(name)
		if !ok || p.Datatype != vocabulary.DatatypeQuantity || p.Repeated || p.Keyed {
			continue
		}
		d := quantityDecl{kind: t.Identity, spec: p.Quantity}
		if b, ok := observedBinding(t); ok && b.Variant == vocabulary.ObservedSingle && name == "value" {
			d.delta = true
		}
		out = append(out, d)
	}
	return out
}

// maxQuantityUnits bounds the distinct spellings one filter or order reads
// for one quantity: each is an arm of the read's SQL.
const maxQuantityUnits = 256

// quantityValueSQL is the stored value's digits as numeric, or NULL where the
// value at `v` is not a quantity's digits: a name one kind declares a quantity
// may be another kind's object of another shape, and the cast must not fail
// that kind's rows. Digits past vocabulary.MaxDecimalDigits read NULL too,
// because past numeric's own bound the cast fails the whole query.
func quantityValueSQL(v string) string {
	value := `(` + v + `->>'` + vocabulary.QuantityValue + `')`
	return `(CASE WHEN length(` + value + `) <= ` + strconv.Itoa(vocabulary.MaxDecimalDigits+2) +
		` AND ` + value + ` ~ '^-?[0-9]+(\.[0-9]+)?$' THEN ` + value + `::numeric END)`
}

// orNull is an expression, or SQL's NULL where there is none.
func orNull(expr string) string {
	if expr == "" {
		return `NULL::numeric`
	}
	return expr
}

// --- ordering and the index expression ---

// quantityProp reports the declarations of a name when EVERY candidate kind
// (readKinds) that declares it declares one quantity, under numericProp's
// every-not-any rule.
func (ds *dataset) quantityProp(types []*vocabulary.Kind, name string) ([]quantityDecl, bool) {
	kinds := ds.readKinds(types)
	for _, t := range kinds {
		if p, ok := t.Prop(name); ok && (p.Repeated || p.Keyed || p.Datatype != vocabulary.DatatypeQuantity) {
			return nil, false
		}
	}
	decls := quantityDecls(kinds, name)
	return decls, len(decls) > 0
}

// quantityArm is one spelling's arm of an order key's CASE: the spelling it
// keys and its expression over valuePlaceholder.
type quantityArm struct{ code, expr string }

// valuePlaceholder stands for the value expression in a cached arm.
const valuePlaceholder = "\x00value\x00"

// orderPlaces is the decimal places of the root value an order key keeps.
// Postgres numeric division keeps as many places as its operands carry, at
// least 16 significant digits, so the numerator is given these places first:
// two values that agree to 40 places of the root unit tie and fall to the
// (kind, id) tiebreak, and any two that differ sooner sort apart.
const orderPlaces = 40

// linearSQL renders value x f + o with integer arithmetic and one division,
// which is where numeric rounds, at orderPlaces decimal places.
func linearSQL(value string, f, o *big.Rat) string {
	// value x fn/fd + on/od = (value x fn x od + on x fd) / (fd x od)
	num := new(big.Int).Mul(f.Num(), o.Denom())
	add := new(big.Int).Mul(o.Num(), f.Denom())
	den := new(big.Int).Mul(f.Denom(), o.Denom())
	expr := value
	if num.Cmp(big.NewInt(1)) != 0 {
		expr = `(` + expr + ` * ` + num.String() + `)`
	}
	if add.Sign() != 0 {
		expr = `(` + expr + ` + (` + add.String() + `))`
	}
	if den.Cmp(big.NewInt(1)) != 0 {
		expr = `(round(` + expr + `, ` + strconv.Itoa(orderPlaces) + `) / ` + den.String() + `)`
	}
	return expr
}

// --- the narrowing guard ---

// quantityNarrows reports whether a changed quantity declaration could refuse
// or reread a value the current one admits: an anchor named or moved, a
// flipped `delta`, a unit list added or shortened, a comparator turned off, a
// bound added or moved. A move of a bound in either direction counts: across
// units the direction is a conversion away, and the count is what decides.
func quantityNarrows(cur, cand *vocabulary.QuantitySpec) bool {
	if cand == nil {
		cand = &vocabulary.QuantitySpec{}
	}
	if cur == nil {
		cur = &vocabulary.QuantitySpec{}
	}
	switch {
	// A named or moved anchor may refuse a value, and a flipped `delta`
	// rereads every one (decision 0186).
	case cand.Unit != "" && cand.Unit != cur.Unit, cand.Delta != cur.Delta:
		return true
	case len(cand.Units) > 0 && (len(cur.Units) == 0 || slices.ContainsFunc(cur.Units, func(u string) bool { return !slices.Contains(cand.Units, u) })):
		return true
	case cur.Comparator && !cand.Comparator:
		return true
	case cand.Min != nil && (cur.Min == nil || *cand.Min != *cur.Min):
		return true
	case cand.Max != nil && (cur.Max == nil || *cand.Max != *cur.Max):
		return true
	}
	return false
}

// quantityRefusesIn judges one stored root value for a quantity narrowing:
// true when a quantity under the declared path is one a write through the
// graph refuses under the candidate declaration (quantitywrite.go), so the
// count and the next write cannot disagree.
func quantityRefusesIn(path []fieldStep, cand *vocabulary.Property) func(*vocabulary.UnitGraph) func([]byte) bool {
	return func(g *vocabulary.UnitGraph) func([]byte) bool {
		return quantityLeaves(path, func(leaf map[string]any) bool {
			coerced, err := coerceQuantity(cand, leaf)
			if err != nil {
				return true
			}
			return resolveQuantityLeaf(g, cand.Quantity, coerced.(map[string]any), cand.Quantity != nil && cand.Quantity.Delta) != nil
		})
	}
}

// quantityDeltaFlips reports whether the candidate reads a value as a
// difference where the current declaration reads it as an absolute value, or
// the reverse (decision 0186).
func quantityDeltaFlips(cur, cand *vocabulary.QuantitySpec) bool {
	return (cur != nil && cur.Delta) != (cand != nil && cand.Delta)
}

// quantityDeltaRereadsIn judges one stored root value for a flipped `delta`:
// true when a quantity under the declared path has a unit
// whose linear component holds two units an offset apart, where a difference
// and an absolute value convert to different numbers (10 Cel is 10 K as a
// difference and 283.15 K as a temperature). A spelling the graph does not
// resolve counts too: no graph can say how it would be reread.
func quantityDeltaRereadsIn(path []fieldStep) func(*vocabulary.UnitGraph) func([]byte) bool {
	return func(g *vocabulary.UnitGraph) func([]byte) bool {
		return quantityLeaves(path, func(leaf map[string]any) bool {
			written, _ := leaf[vocabulary.QuantityUnit].(string)
			id, ok := g.Resolve(written)
			return !ok || affineComponent(g, id)
		})
	}
}

// affineComponent reports whether two units of id's linear component map to
// its root with different offsets.
func affineComponent(g *vocabulary.UnitGraph, id string) bool {
	_, off, ok := g.ToRoot(id)
	if !ok {
		return true
	}
	for _, other := range g.Component(id) {
		if _, o, ok := g.ToRoot(other); ok && o.Cmp(off) != 0 {
			return true
		}
	}
	return false
}

// quantityLeaves is true when judge holds for any quantity-shaped value under
// the declared path of one stored root value.
func quantityLeaves(path []fieldStep, judge func(map[string]any) bool) func([]byte) bool {
	return func(raw []byte) bool {
		var root any
		if err := json.Unmarshal(raw, &root); err != nil {
			return false
		}
		for _, leaf := range leavesAtPath(root, path) {
			if m, ok := leaf.(map[string]any); ok && judge(m) {
				return true
			}
		}
		return false
	}
}

// quantityChange names what a narrowing quantity declaration changed, for the
// guard's message.
func quantityChange(cur, cand *vocabulary.QuantitySpec) string {
	if cur == nil {
		cur = &vocabulary.QuantitySpec{}
	}
	if cand == nil {
		cand = &vocabulary.QuantitySpec{}
	}
	var parts []string
	if !slices.Equal(cand.Units, cur.Units) && len(cand.Units) > 0 {
		parts = append(parts, "units "+strings.Join(cand.Units, ", "))
	}
	if cur.Comparator && !cand.Comparator {
		parts = append(parts, "no comparator")
	}
	if cand.Min != nil && (cur.Min == nil || *cand.Min != *cur.Min) {
		parts = append(parts, "min "+cand.Min.String())
	}
	if cand.Max != nil && (cur.Max == nil || *cand.Max != *cur.Max) {
		parts = append(parts, "max "+cand.Max.String())
	}
	if cand.Unit != "" && cand.Unit != cur.Unit {
		parts = append(parts, "unit "+cand.Unit)
	}
	if cand.Delta != cur.Delta {
		parts = append(parts, "delta "+strconv.FormatBool(cand.Delta))
	}
	return strings.Join(parts, ", ")
}
