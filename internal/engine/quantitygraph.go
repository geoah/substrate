package engine

// QUANTITY FILTERS AND ORDERS THROUGH THE REPOSITORY'S UNIT GRAPH (decision
// 0186), the read half of the unit graph switch (WithUnitGraph).
//
// A filter resolves its operand through the graph and converts it exactly
// into every spelling of a unit of the operand's linear component that a
// filtered kind's stored values use (unit_uses, one index probe per spelling
// and kind), so units nobody uses never reach the SQL; each arm keeps the
// sign test `value * d - n` quantity.go's header explains, which holds
// because every linear conversion is increasing. A difference (a property
// declaring `delta: true`, or an observed value marked `delta`) compares by
// the factor alone. Only linear conversions are crossed: an operand whose
// unit reaches a declaration's `unit` only through an expression is refused.
//
// An order keys every row by its value converted to the root of the one
// linear component every candidate declaration anchors to, at orderPlaces,
// with an arm for every spelling of every unit of that component, so a
// row's key is a function of the row and the graph alone. A property some
// candidate declaration does not anchor with `unit` or `units`, or whose
// anchors lie in two components, is refused.
//
// Either one makes the list's cursor carry the graph revision it read
// (List), so a page after a conversion changed is refused rather than seeking
// under a different predicate.

import (
	"context"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// builderUnits is the graph a read's builder converts through, read once per
// builder inside the read's snapshot. Reading it marks the builder: the
// read's cursor then carries the revision.
func (ds *dataset) builderUnits(ctx context.Context, x dbx, b *builder) (*unitGraphAt, error) {
	if b.units != nil {
		return b.units, nil
	}
	var at *unitGraphAt
	var err error
	switch {
	case b.tx != nil:
		at, err = b.tx.unitGraph()
	case x != nil:
		at, err = ds.unitGraph(ctx, x)
	default:
		tx, terr := ds.db.BeginTx(ctx, nil)
		if terr != nil {
			return nil, terr
		}
		defer func() { _ = tx.Rollback() }()
		at, err = ds.unitGraph(ctx, tx)
	}
	if err != nil {
		return nil, err
	}
	b.units = at
	return at, nil
}

// quantityReading is how a group of kinds reads a stored value: as is, as a
// difference (a property declaring `delta: true`), or as a difference only
// on a row whose `delta` says so (an observed single value).
type quantityReading int

const (
	readAbsolute quantityReading = iota
	readDifference
	readRowDelta
)

func readingOf(d quantityDecl) quantityReading {
	switch {
	case d.spec != nil && d.spec.Delta:
		return readDifference
	case d.delta:
		return readRowDelta
	}
	return readAbsolute
}

// graphQuantityGroup is the candidate kinds that read a stored value alike.
type graphQuantityGroup struct {
	reading quantityReading
	kinds   []string
	specs   []*vocabulary.QuantitySpec
}

func groupGraphDecls(decls []quantityDecl) []graphQuantityGroup {
	var groups []graphQuantityGroup
	for _, d := range decls {
		r := readingOf(d)
		i := slices.IndexFunc(groups, func(g graphQuantityGroup) bool { return g.reading == r })
		if i < 0 {
			groups = append(groups, graphQuantityGroup{reading: r})
			i = len(groups) - 1
		}
		groups[i].kinds = append(groups[i].kinds, d.kind)
		groups[i].specs = append(groups[i].specs, d.spec)
	}
	return groups
}

// condQuantityGraph is condQuantity under the switch.
func (ds *dataset) condQuantityGraph(ctx context.Context, x dbx, b *builder, types []*vocabulary.Kind, key string, c substrate.Cond, decls []quantityDecl, bounded bool) error {
	v := `(props->` + sqlLiteral(key) + `)`
	groups := groupGraphDecls(decls)
	compare := func(op string, raw any) (string, error) {
		at, err := ds.builderUnits(ctx, x, b)
		if err != nil {
			return "", err
		}
		g := at.graph
		oid, ovalue, err := graphOperand(g, raw)
		if err != nil {
			return "", fmt.Errorf("%w: %s compares against a quantity value: %w", substrate.ErrValidation, key, err)
		}
		used, err := ds.usedSpellings(ctx, x, b, key, g, oid, decls)
		if err != nil {
			return "", err
		}
		var ors []string
		var refused error
		for _, grp := range groups {
			clause, err := graphQuantityClause(b, g, v, grp, used, op, oid, ovalue, bounded)
			if err != nil && len(types) == 0 {
				if refused == nil {
					refused = err
				}
				continue
			}
			if err != nil {
				return "", fmt.Errorf("%w: %s compares against a quantity value: %w", substrate.ErrValidation, key, err)
			}
			if len(groups) > 1 || len(types) == 0 {
				clause = `(kind = ANY(` + b.textArray(grp.kinds) + `) AND ` + clause + `)`
			}
			ors = append(ors, clause)
		}
		if len(ors) == 0 && refused != nil {
			return "", fmt.Errorf("%w: %s compares against a quantity value: %w", substrate.ErrValidation, key, refused)
		}
		if len(ors) == 0 {
			return `FALSE`, nil
		}
		return `(` + strings.Join(ors, ` OR `) + `)`, nil
	}
	for _, o := range []struct {
		op string
		v  any
	}{{"=", c.Eq}, {">", c.Gt}, {">=", c.Gte}, {"<", c.Lt}, {"<=", c.Lte}} {
		if o.v == nil {
			continue
		}
		clause, err := compare(o.op, o.v)
		if err != nil {
			return err
		}
		b.add(clause)
	}
	if len(c.In) > 0 {
		ors := make([]string, 0, len(c.In))
		for _, raw := range c.In {
			clause, err := compare("=", raw)
			if err != nil {
				return err
			}
			ors = append(ors, clause)
		}
		b.add(`(` + strings.Join(ors, ` OR `) + `)`)
	}
	if c.Exists != nil {
		clause := `jsonb_exists(props, ` + b.arg(key) + `)`
		if !*c.Exists {
			clause = `NOT ` + clause
		}
		b.add(clause)
	}
	return nil
}

// graphOperand reads a filter's operand: a quantity value with no
// comparator, its unit resolved through the graph.
func graphOperand(g *vocabulary.UnitGraph, raw any) (string, *big.Rat, error) {
	if m, ok := raw.(map[string]any); ok {
		if _, has := m[vocabulary.QuantityComparator]; has {
			return "", nil, fmt.Errorf("an operand is one value; a comparator belongs to a stored value")
		}
	}
	coerced, err := coercer{graphUnits: true}.quantity(&vocabulary.Property{Datatype: vocabulary.DatatypeQuantity}, raw)
	if err != nil {
		return "", nil, err
	}
	operand := coerced.(map[string]any)
	written := operand[vocabulary.QuantityUnit].(string)
	id, ok := g.Resolve(written)
	if !ok {
		return "", nil, fmt.Errorf("%s: %q names no unit of this repository", vocabulary.QuantityUnit, written)
	}
	value, _ := new(big.Rat).SetString(operand[vocabulary.QuantityValue].(string))
	return id, value, nil
}

// usedSpellings are the spellings of the units of the operand's linear
// component that a filtered kind's stored values use, each with the unit it
// names: the one row a mapping's where asks about, else a probe of
// unit_uses per spelling over the declaring kinds, else, for a read with no
// table behind it, every spelling of the component.
func (ds *dataset) usedSpellings(ctx context.Context, x dbx, b *builder, key string, g *vocabulary.UnitGraph, oid string, decls []quantityDecl) (map[string]string, error) {
	candidates := map[string]string{}
	for _, id := range g.Component(oid) {
		u, _ := g.Unit(id)
		for _, s := range unitSpellings(u) {
			candidates[s] = id
		}
	}
	if b.rowProps != nil {
		out := map[string]string{}
		if m, ok := b.rowProps[key].(map[string]any); ok {
			if s, _ := m[vocabulary.QuantityUnit].(string); s != "" {
				if id, ok := g.Resolve(s); ok && slices.Contains(g.Component(oid), id) {
					out[s] = id
				}
			}
		}
		return out, nil
	}
	if x == nil {
		if len(candidates) > maxQuantityUnits {
			return nil, fmt.Errorf("%w: %s compares in a unit whose linear component holds more than %d spellings; filter a list, whose rows say which they use",
				substrate.ErrValidation, key, maxQuantityUnits)
		}
		return candidates, nil
	}
	kinds := make([]string, 0, len(decls))
	for _, d := range decls {
		kinds = append(kinds, d.kind)
	}
	spellings := sortedKeys(candidates)
	rows, err := x.QueryContext(ctx, `
		SELECT s FROM unnest($1::text[]) s
		WHERE EXISTS (SELECT 1 FROM unit_uses WHERE unit = s AND kind = ANY($2::text[]))`, spellings, kinds)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: read the units %s uses: %w", key, err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out[s] = candidates[s]
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) > maxQuantityUnits {
		return nil, fmt.Errorf("%w: %s holds values in more than %d spellings of one linear component; a read compares in at most that many",
			substrate.ErrValidation, key, maxQuantityUnits)
	}
	return out, nil
}

// graphQuantityClause is one comparison for one group of kinds: the operand
// converted exactly into each used spelling, a difference by the factor
// alone, and a bounded value's half-line read as quantityClause reads it.
func graphQuantityClause(b *builder, g *vocabulary.UnitGraph, v string, grp graphQuantityGroup, used map[string]string, op, oid string, ovalue *big.Rat, bounded bool) (string, error) {
	for _, spec := range grp.specs {
		if spec == nil || spec.Unit == "" {
			continue
		}
		anchor, ok := g.Resolve(spec.Unit)
		if !ok {
			return "", fmt.Errorf("the declaration's unit %q names no unit of this repository", spec.Unit)
		}
		if _, _, err := g.Linear(oid, anchor, false); err != nil {
			return "", err
		}
	}
	value := quantityValueSQL(v)
	in := func(id string, delta bool) string {
		f, o, err := g.Linear(oid, id, delta)
		if err != nil {
			return ""
		}
		t := new(big.Rat).Mul(ovalue, f)
		t.Add(t, o)
		return value + ` * ` + t.Denom().String() + ` - (` + t.Num().String() + `)`
	}
	var arms []string
	for _, s := range sortedKeys(used) {
		id := used[s]
		var expr string
		switch grp.reading {
		case readAbsolute:
			expr = in(id, false)
		case readDifference:
			expr = in(id, true)
		case readRowDelta:
			expr = `(CASE WHEN ` + deltaSQL + ` THEN ` + orNull(in(id, true)) + ` ELSE ` + orNull(in(id, false)) + ` END)`
		}
		if expr == "" {
			continue
		}
		arms = append(arms, `WHEN `+b.arg(s)+` THEN `+expr)
	}
	if len(arms) == 0 {
		return `FALSE`, nil
	}
	diff := `(CASE (` + v + `->>'` + vocabulary.QuantityUnit + `') ` + strings.Join(arms, ` `) + ` END)`
	return boundedComparison(v, diff, op, bounded), nil
}

// boundedComparison compares diff (the stored value minus the operand) with
// zero, reading a bounded stored value's half-line where values may carry
// one (vocabulary.QuantityComparators).
func boundedComparison(v, diff, op string, bounded bool) string {
	if !bounded {
		return diff + ` ` + op + ` 0`
	}
	cmp := `coalesce(` + v + `->>'` + vocabulary.QuantityComparator + `', '')`
	var arms string
	switch op {
	case "=":
		return `(` + cmp + ` = '' AND ` + diff + ` = 0)`
	case "<":
		arms = `WHEN '' THEN ` + diff + ` < 0 WHEN '<' THEN ` + diff + ` <= 0 WHEN '<=' THEN ` + diff + ` < 0`
	case "<=":
		arms = `WHEN '' THEN ` + diff + ` <= 0 WHEN '<' THEN ` + diff + ` <= 0 WHEN '<=' THEN ` + diff + ` <= 0`
	case ">":
		arms = `WHEN '' THEN ` + diff + ` > 0 WHEN '>' THEN ` + diff + ` >= 0 WHEN '>=' THEN ` + diff + ` > 0`
	case ">=":
		arms = `WHEN '' THEN ` + diff + ` >= 0 WHEN '>' THEN ` + diff + ` >= 0 WHEN '>=' THEN ` + diff + ` >= 0`
	}
	return `(CASE ` + cmp + ` ` + arms + ` ELSE FALSE END)`
}

// --- ordering -----------------------------------------------------------------

// quantityOrderGraph is the key an order by a quantity sorts by under the
// switch: the value converted to the root of the one linear component every
// candidate declaration anchors to.
func quantityOrderGraph(at *unitGraphAt, property string, decls []quantityDecl) (string, error) {
	g := at.graph
	root := ""
	for _, d := range decls {
		if d.spec == nil || (d.spec.Unit == "" && len(d.spec.Units) == 0) {
			return "", fmt.Errorf("%w: cannot order by %s: %s declares no unit, so its values may lie in units that do not convert to one another; declare `unit` on it",
				substrate.ErrValidation, property, d.kind)
		}
		named := append([]string{}, d.spec.Units...)
		if d.spec.Unit != "" {
			named = append([]string{d.spec.Unit}, named...)
		}
		for _, s := range named {
			id, ok := g.Resolve(s)
			if !ok {
				return "", fmt.Errorf("%w: cannot order by %s: %s names %q, which is no unit of this repository",
					substrate.ErrValidation, property, d.kind, s)
			}
			r, _ := g.Root(id)
			switch {
			case root == "":
				root = r
			case r != root:
				return "", fmt.Errorf("%w: cannot order by %s: its declarations name units that do not convert to one another (%s and %s)",
					substrate.ErrValidation, property, root, id)
			}
		}
	}
	v := `(props->` + sqlLiteral(property) + `)`
	groups := groupGraphDecls(decls)
	if len(groups) == 1 {
		return graphOrderCase(g, v, root, groups[0].reading)
	}
	var sb strings.Builder
	sb.WriteString(`(CASE`)
	for _, grp := range groups {
		kinds := make([]string, len(grp.kinds))
		for i, k := range grp.kinds {
			kinds[i] = sqlLiteral(k)
		}
		expr, err := graphOrderCase(g, v, root, grp.reading)
		if err != nil {
			return "", err
		}
		sb.WriteString(` WHEN kind IN (` + strings.Join(kinds, `, `) + `) THEN ` + expr)
	}
	sb.WriteString(` END)`)
	return sb.String(), nil
}

// graphOrderCase is one reading's key: an arm per spelling of each unit of
// the root's component, its exact map to the root through linearSQL. The
// arms live in the graph's memo, so they are built once per graph.
func graphOrderCase(g *vocabulary.UnitGraph, v, root string, reading quantityReading) (string, error) {
	built, err := g.Memo("order|"+root+"|"+strconv.Itoa(int(reading)), func() (any, error) {
		var arms []quantityArm
		for _, id := range g.Component(root) {
			arm := func(delta bool) string {
				f, o, err := g.Linear(id, root, delta)
				if err != nil {
					return `NULL::numeric`
				}
				return linearSQL(valuePlaceholder, f, o)
			}
			expr := arm(reading == readDifference)
			if reading == readRowDelta {
				expr = `(CASE WHEN ` + deltaSQL + ` THEN ` + arm(true) + ` ELSE ` + arm(false) + ` END)`
			}
			u, _ := g.Unit(id)
			for _, s := range unitSpellings(u) {
				arms = append(arms, quantityArm{code: s, expr: expr})
			}
		}
		return arms, nil
	})
	if err != nil {
		return "", err
	}
	arms := built.([]quantityArm)
	value := quantityValueSQL(v)
	var sb strings.Builder
	sb.WriteString(`(CASE (` + v + `->>'` + vocabulary.QuantityUnit + `')`)
	for _, a := range arms {
		sb.WriteString(` WHEN ` + sqlLiteral(a.code) + ` THEN ` + strings.ReplaceAll(a.expr, valuePlaceholder, value))
	}
	sb.WriteString(` END)`)
	return sb.String(), nil
}

// quantityIndexRefusal refuses, under the switch, a declared index on a
// quantity property: the index serves every repository that holds the kind,
// and its key would depend on one repository's graph.
func quantityIndexRefusal(types []*vocabulary.Kind) error {
	for _, t := range types {
		for _, cols := range t.Indices {
			for _, c := range cols {
				if p, ok := t.Prop(c); ok && p.Datatype == vocabulary.DatatypeQuantity {
					return fmt.Errorf("%w: %s indices: %s is a quantity, and an index on one is refused: its key would convert through one repository's unit graph while the index serves every repository holding the kind; order by it unindexed",
						substrate.ErrValidation, t.Identity, c)
				}
			}
		}
	}
	return nil
}
