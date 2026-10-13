package engine

// QUANTITY VALUES: write coercion, filters, ordering, the index expression and
// the narrowing guard, for `type: quantity` (vocabulary/quantity.go says what a
// quantity value is, vocabulary/units.go how a unit converts).
//
// Every comparison is exact. SQL never rounds a converted value: a filter
// converts its operand into each stored unit in Go, as a rational n/d, and asks
// SQL `value * d - n`, whose sign is the sign of the stored value minus the
// operand however the two units relate, because every conversion inside a
// measure is increasing. An ordering reads the value converted to the measure's
// base, where a division may round past the twentieth significant digit; an
// order is a key, and nothing compares against it.

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// coerceQuantity admits one quantity value: exactly the three members, the
// value exact digits written as a string, the unit a code the declaration
// admits (an alias is stored as its code), and a comparator only where the
// property declares `comparator: true`. A declared min/max bounds the value,
// compared exactly after converting it into each bound's unit.
func coerceQuantity(p *vocabulary.Property, v any) (any, error) {
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
	digits, err := vocabulary.QuantityDigits(s)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", vocabulary.QuantityValue, err)
	}
	written, _ := m[vocabulary.QuantityUnit].(string)
	if written == "" {
		return nil, fmt.Errorf("a quantity value needs %s, a UCUM code (kg, Cel, mmol/L)", vocabulary.QuantityUnit)
	}
	unit, err := vocabulary.CanonicalUnit(written)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", vocabulary.QuantityUnit, err)
	}
	if _, err := p.Quantity.CheckUnit(unit); err != nil {
		return nil, fmt.Errorf("%s: %w", vocabulary.QuantityUnit, err)
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
	if q := p.Quantity; q != nil && (q.Min != nil || q.Max != nil) {
		r, _ := new(big.Rat).SetString(digits)
		if err := q.CheckBounds(r, unit); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// quantityText is a quantity property's value as a title and the search index
// read it: "72.5 kg", or a list's values joined.
func quantityText(v any) string {
	if list, ok := v.([]any); ok {
		var parts []string
		for _, item := range list {
			if s := vocabulary.FormatQuantity(item); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	}
	return vocabulary.FormatQuantity(v)
}

// --- filters ---

// quantityDecl is one candidate kind's declaration of a quantity name.
type quantityDecl struct {
	kind string
	spec *vocabulary.QuantitySpec
	// delta says the name is the `value` of a kind binding the observed
	// trait's single variant, whose `delta: true` makes a temperature a
	// difference (observedindex.go measureFor reads a row the same way).
	delta bool
}

func (d quantityDecl) measure() string {
	if d.spec == nil {
		return ""
	}
	return d.spec.Measure
}

// quantityGroup is the candidate kinds that read a stored unit the same way:
// those declaring the same measure, or none, alike in reading `delta`.
type quantityGroup struct {
	measure string
	delta   bool
	kinds   []string
	specs   []*vocabulary.QuantitySpec
}

// deltaSQL is true on a row whose `delta` says its temperature is a
// difference.
const deltaSQL = `(props->>'delta') = 'true'`

// groupQuantityDecls groups the declarations by how they read a stored unit,
// in the order the kinds come.
func groupQuantityDecls(decls []quantityDecl) []quantityGroup {
	var groups []quantityGroup
	for _, d := range decls {
		measure := d.measure()
		i := slices.IndexFunc(groups, func(g quantityGroup) bool { return g.measure == measure && g.delta == d.delta })
		if i < 0 {
			groups = append(groups, quantityGroup{measure: measure, delta: d.delta})
			i = len(groups) - 1
		}
		groups[i].kinds = append(groups[i].kinds, d.kind)
		groups[i].specs = append(groups[i].specs, d.spec)
	}
	return groups
}

// condQuantity filters one quantity property. A comparison's operand is itself
// a quantity value, and it holds only inside the operand's measure: `lt`
// {155, [lb_av]} is the mass values below 155 pounds, a stored 72.4 kg among
// them, and a stored value of another measure never matches. `in` is any of
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
	decls := quantityDecls(types, key)
	bounded := slices.ContainsFunc(decls, func(d quantityDecl) bool { return d.spec != nil && d.spec.Comparator })
	if len(c.In) > 0 && bounded {
		return fmt.Errorf("%w: %s may hold a bounded value (<0.5), which no set of values certainly contains: compare it with eq or the comparison operators",
			substrate.ErrValidation, key)
	}
	groups := groupQuantityDecls(decls)
	// The rows' units are read only for a comparison: an `exists` alone needs
	// none. The name is a literal, never a placeholder: a comparison no stored
	// unit converts into renders FALSE and never reads the value, and a
	// placeholder bound and never used is one Postgres cannot type.
	var stored []string
	v := `(props->` + sqlLiteral(key) + `)`
	if c.Eq != nil || c.Gt != nil || c.Gte != nil || c.Lt != nil || c.Lte != nil || len(c.In) > 0 {
		var err error
		if stored, err = ds.storedQuantityUnits(ctx, x, b, decls, key); err != nil {
			return err
		}
	}
	compare := func(op string, raw any) (string, error) {
		var ors []string
		for _, g := range groups {
			clause, err := quantityClause(b, v, g, stored, op, raw, bounded)
			if err != nil {
				return "", fmt.Errorf("%w: %s compares against a quantity value: %w", substrate.ErrValidation, key, err)
			}
			if len(groups) > 1 {
				clause = `(kind = ANY(` + b.textArray(g.kinds) + `) AND ` + clause + `)`
			}
			ors = append(ors, clause)
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
			d.delta = d.measure() == "" || d.measure() == "temperature"
		}
		out = append(out, d)
	}
	return out
}

// storedQuantityUnits are the unit codes a filter's clause must convert into.
// A declaration that lists `units:` (or names a level's measure, which admits
// its listed codes alone) closes the set, and its codes are the whole answer.
// Any other admits compounds and prefixed codes no table row lists, so the
// rows themselves are the only complete list: the one row a mapping's where
// asks about, or one read of the filtered kinds' rows (readStoredUnits),
// beside the codes the declarations' measures list.
func (ds *dataset) storedQuantityUnits(ctx context.Context, x dbx, b *builder, decls []quantityDecl, key string) ([]string, error) {
	var out []string
	add := func(code string) {
		if code != "" && !slices.Contains(out, code) {
			out = append(out, code)
		}
	}
	var open []string
	for _, d := range decls {
		if d.spec != nil {
			for _, u := range d.spec.Units {
				add(u)
			}
		}
		if quantityUnitsClosed(d.spec) {
			continue
		}
		if d.spec != nil {
			if m, ok := vocabulary.LookupMeasure(d.spec.Measure); ok {
				for _, u := range m.Units {
					add(u)
				}
			}
		}
		open = append(open, d.kind)
	}
	switch {
	case b.rowProps != nil:
		if m, ok := b.rowProps[key].(map[string]any); ok {
			u, _ := m[vocabulary.QuantityUnit].(string)
			add(u)
		}
	case b.overRecords && x != nil && len(open) > 0:
		units, err := readStoredUnits(ctx, x, b, open, key)
		if err != nil {
			return nil, err
		}
		for _, u := range units {
			add(u)
		}
	}
	slices.Sort(out)
	return out, nil
}

// maxQuantityUnits bounds the distinct units one read collects from the rows
// for one quantity: each is an arm of the read's SQL, and the grammar lets a
// writer spell one unit many ways (`kg`, `kg.1`, `g.10*3`).
const maxQuantityUnits = 256

// quantityUnitsClosed reports whether a declaration names every unit a value
// may hold: a `units:` list, or a level's measure, whose listed codes are the
// only ones admitted.
func quantityUnitsClosed(spec *vocabulary.QuantitySpec) bool {
	if spec == nil {
		return false
	}
	if len(spec.Units) > 0 {
		return true
	}
	m, ok := vocabulary.LookupMeasure(spec.Measure)
	return ok && m.Scale == vocabulary.ScaleLogarithmic
}

// readStoredUnits is the one read of the units the rows of kinds hold under a
// quantity's name, under every condition the read's builder holds so far (the
// whole filter for an order; for a filter's clause every condition but the
// quantity comparisons after it, buildFilter building those last), so a
// narrow range or a narrow filter reads a narrow set of rows. It reads them
// on the side the read selects, the live rows or the tombstones, and answers
// at most maxQuantityUnits codes; more is refused, naming the declaration
// that would close the set.
func readStoredUnits(ctx context.Context, x dbx, b *builder, kinds []string, key string) ([]string, error) {
	side := `deleted_at IS NULL`
	if b.tombstones {
		side = `deleted_at IS NOT NULL`
	}
	args := append(append([]any(nil), b.args...), key, kinds, maxQuantityUnits+1)
	n := len(b.args)
	k, ks, lim := "$"+strconv.Itoa(n+1), "$"+strconv.Itoa(n+2), "$"+strconv.Itoa(n+3)
	where := append(append([]string(nil), b.where...),
		`kind = ANY(`+ks+`::text[])`, side, `jsonb_typeof(props->`+k+`::text) = 'object'`)
	rows, err := x.QueryContext(ctx, `SELECT DISTINCT props->`+k+`::text->>'`+vocabulary.QuantityUnit+`'
		FROM records WHERE `+strings.Join(where, " AND ")+`
		LIMIT `+lim, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var u *string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		if u != nil && *u != "" {
			out = append(out, *u)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) > maxQuantityUnits {
		return nil, fmt.Errorf("%w: %s holds more than %d distinct units across the rows this read covers; declare `units:` on it so a read need not collect them",
			substrate.ErrValidation, key, maxQuantityUnits)
	}
	slices.Sort(out)
	return out, nil
}

// quantityValueSQL is the stored value's digits as numeric, or NULL where the
// value at `v` is not a quantity's digits: a name one kind declares a quantity
// may be another kind's object of another shape, and the cast must not fail
// that kind's rows. Digits past vocabulary.MaxQuantityDigits read NULL too,
// because past numeric's own bound the cast fails the whole query.
func quantityValueSQL(v string) string {
	value := `(` + v + `->>'` + vocabulary.QuantityValue + `')`
	return `(CASE WHEN length(` + value + `) <= ` + strconv.Itoa(vocabulary.MaxQuantityDigits+2) +
		` AND ` + value + ` ~ '^-?[0-9]+(\.[0-9]+)?$' THEN ` + value + `::numeric END)`
}

// quantityClause is one comparison against one operand for one group of
// kinds: the operand converted exactly into each stored unit of its measure,
// and the comparator's half-line read where values may carry one. In a group
// that reads `delta`, a stored temperature on a row marked a difference is
// compared with the operand read as a difference too.
func quantityClause(b *builder, v string, g quantityGroup, stored []string, op string, raw any, bounded bool) (string, error) {
	if m, ok := raw.(map[string]any); ok {
		if _, has := m[vocabulary.QuantityComparator]; has {
			return "", fmt.Errorf("an operand is one value; a comparator belongs to a stored value")
		}
	}
	spec := &vocabulary.QuantitySpec{Measure: g.measure}
	coerced, err := coerceQuantity(&vocabulary.Property{Datatype: vocabulary.DatatypeQuantity, Quantity: spec}, raw)
	if err != nil {
		return "", err
	}
	operand := coerced.(map[string]any)
	ounit := operand[vocabulary.QuantityUnit].(string)
	ovalue, _ := new(big.Rat).SetString(operand[vocabulary.QuantityValue].(string))
	om, err := vocabulary.ResolveMeasure(ounit, g.measure)
	if err != nil {
		return "", err
	}
	value := quantityValueSQL(v)
	// in is the stored value minus the operand converted into s inside
	// measure, or "" where the operand does not convert.
	in := func(measure, s string) string {
		t, err := vocabulary.ConvertIn(measure, ovalue, ounit, s)
		if err != nil {
			return ""
		}
		return value + ` * ` + t.Denom().String() + ` - (` + t.Num().String() + `)`
	}
	var arms []string
	for _, s := range stored {
		sm, err := vocabulary.ResolveMeasure(s, g.measure)
		if err != nil {
			continue
		}
		var expr string
		if sm.Name == om.Name {
			expr = in(om.Name, s)
		}
		if g.delta && sm.Name == "temperature" {
			diff := in("temperature-difference", s)
			if diff == "" && expr == "" {
				continue
			}
			expr = `(CASE WHEN ` + deltaSQL + ` THEN ` + orNull(diff) + ` ELSE ` + orNull(expr) + ` END)`
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
	if !bounded {
		return diff + ` ` + op + ` 0`, nil
	}
	// A bounded value matches where the comparison holds for every value its
	// half-line admits (vocabulary.QuantityComparators): a stored `<5` is below
	// 10 and below 5, a stored `<=5` below 5.1 but not below 5.
	cmp := `coalesce(` + v + `->>'` + vocabulary.QuantityComparator + `', '')`
	var arms2 string
	switch op {
	case "=":
		return `(` + cmp + ` = '' AND ` + diff + ` = 0)`, nil
	case "<":
		arms2 = `WHEN '' THEN ` + diff + ` < 0 WHEN '<' THEN ` + diff + ` <= 0 WHEN '<=' THEN ` + diff + ` < 0`
	case "<=":
		arms2 = `WHEN '' THEN ` + diff + ` <= 0 WHEN '<' THEN ` + diff + ` <= 0 WHEN '<=' THEN ` + diff + ` <= 0`
	case ">":
		arms2 = `WHEN '' THEN ` + diff + ` > 0 WHEN '>' THEN ` + diff + ` >= 0 WHEN '>=' THEN ` + diff + ` > 0`
	case ">=":
		arms2 = `WHEN '' THEN ` + diff + ` >= 0 WHEN '>' THEN ` + diff + ` >= 0 WHEN '>=' THEN ` + diff + ` >= 0`
	}
	return `(CASE ` + cmp + ` ` + arms2 + ` ELSE FALSE END)`, nil
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

// annotationsSQL is the pattern an ordering strips from a stored unit before
// it reads its factor: an annotation names what is counted and never scales
// the value, so {beats}/min orders as /min does.
const annotationsSQL = `'\{[^}]*\}'`

// quantityOrderSQL is the key an order by a quantity sorts by: the value at
// `v` converted to its measure's base (quantityBaseSQL), read for each group
// of kinds the way that group reads a stored unit, so a code two
// declarations read in two measures (Cel as a temperature and as a
// difference) keys each kind's rows in its own. With one group and no
// `delta`, it is quantityBaseSQL itself, the declared index's text.
func quantityOrderSQL(v string, groups []quantityGroup, stored ...string) string {
	if len(groups) == 1 {
		return quantityCaseSQL(v, groups[0].specs, groups[0].delta, stored...)
	}
	var sb strings.Builder
	sb.WriteString(`(CASE`)
	for _, g := range groups {
		kinds := make([]string, len(g.kinds))
		for i, k := range g.kinds {
			kinds[i] = sqlLiteral(k)
		}
		sb.WriteString(` WHEN kind IN (` + strings.Join(kinds, `, `) + `) THEN ` + quantityCaseSQL(v, g.specs, g.delta, stored...))
	}
	sb.WriteString(` END)`)
	return sb.String()
}

// quantityBaseSQL is the value at `v` converted to its measure's base, the key
// an ordering sorts by and a declared index holds. It has an arm for each code
// the declarations name (the declared `units:` where a property lists them,
// else the measure's codes where one is declared, else every code the table
// lists) and then one for each of the stored codes the read found that no
// declared arm covers: the grammar admits compounds and prefixed codes no row
// lists (`Mg`, `kg/h`), and each converts by the same factor its write was
// admitted under (vocabulary.OrderFactor), so no accepted number keys NULL. A
// level or an arbitrary unit, which has no base, sorts by its own number; rad
// reaches deg through pi at 40 places. Every spec names one measure, or none:
// quantityOrderSQL keeps declarations of two measures apart.
//
// Each arm gives a row the same key whichever arms beside it, so the key is a
// function of the row and the declaration alone: a cursor minted under fewer
// arms seeks the same position (List signs it with the declared arms only),
// and with no stored code beyond the declared ones the expression is the
// declared index's own text.
func quantityBaseSQL(v string, specs []*vocabulary.QuantitySpec, stored ...string) string {
	return quantityCaseSQL(v, specs, false, stored...)
}

// quantityArm is one code's arm of a base-value CASE: the code it keys,
// annotations stripped, and its expression over valuePlaceholder.
type quantityArm struct{ code, expr string }

// valuePlaceholder stands for the value expression in a cached arm.
const valuePlaceholder = "\x00value\x00"

// declaredArms memoizes the arms a declaration names, by measure, unit list
// and delta: every one is a function of the immutable unit table, and the
// table's every code (a property naming no measure) is hundreds of parses.
var declaredArms sync.Map

// quantityCaseSQL is quantityBaseSQL for a group that may read `delta`: a
// temperature arm then keys a row marked a difference without the offset.
func quantityCaseSQL(v string, specs []*vocabulary.QuantitySpec, delta bool, stored ...string) string {
	measure := ""
	var arms []quantityArm
	seen := map[string]bool{}
	for _, s := range specs {
		var listed []string
		if s != nil {
			measure = s.Measure
			listed = s.Units
		}
		for _, a := range declaredArmsFor(measure, listed, delta) {
			if !seen[a.code] {
				seen[a.code] = true
				arms = append(arms, a)
			}
		}
	}
	for _, c := range stored {
		if a, ok := baseArm(measure, c, delta); ok && !seen[a.code] {
			seen[a.code] = true
			arms = append(arms, a)
		}
	}
	value := quantityValueSQL(v)
	var sb strings.Builder
	sb.WriteString(`(CASE regexp_replace(` + v + `->>'` + vocabulary.QuantityUnit + `', ` + annotationsSQL + `, '', 'g')`)
	for _, a := range arms {
		sb.WriteString(` WHEN ` + sqlLiteral(a.code) + ` THEN ` + strings.ReplaceAll(a.expr, valuePlaceholder, value))
	}
	sb.WriteString(` END)`)
	return sb.String()
}

// declaredArmsFor is the arms one declaration names, in order: the declared
// `units:` where it lists them, else its measure's codes, else every code the
// table lists.
func declaredArmsFor(measure string, listed []string, delta bool) []quantityArm {
	key := measure + "|" + strings.Join(listed, " ") + "|" + strconv.FormatBool(delta)
	if cached, ok := declaredArms.Load(key); ok {
		return cached.([]quantityArm)
	}
	codes := slices.Clone(listed)
	if len(listed) == 0 {
		if m, ok := vocabulary.LookupMeasure(measure); ok {
			codes = m.Units
		} else {
			codes = vocabulary.UnitCodes()
		}
	}
	var arms []quantityArm
	seen := map[string]bool{}
	for _, c := range codes {
		if a, ok := baseArm(measure, c, delta); ok && !seen[a.code] {
			seen[a.code] = true
			arms = append(arms, a)
		}
	}
	declaredArms.Store(key, arms)
	return arms
}

// baseArm is one code's arm: its factor to the base of the measure it reads
// in, the value itself for a level or an arbitrary unit, NULL for a code no
// factor reaches; false for a code the measure does not admit. With delta, a
// temperature keys a row marked a difference by its factor alone.
func baseArm(measure, code string, delta bool) (quantityArm, bool) {
	m, err := vocabulary.ResolveMeasure(code, measure)
	if err != nil {
		return quantityArm{}, false
	}
	arm := quantityArm{code: stripAnnotations(code)}
	switch m.Scale {
	case vocabulary.ScaleLogarithmic, vocabulary.ScaleArbitrary:
		arm.expr = valuePlaceholder
		return arm, true
	}
	linear := func(measure string) string {
		f, o, err := vocabulary.OrderFactor(measure, code)
		if err != nil {
			return `NULL::numeric`
		}
		return linearSQL(valuePlaceholder, f, o)
	}
	arm.expr = linear(m.Name)
	if delta && m.Name == "temperature" {
		arm.expr = `(CASE WHEN ` + deltaSQL + ` THEN ` + linear("temperature-difference") + ` ELSE ` + arm.expr + ` END)`
	}
	return arm, true
}

// quantityOrderTerms widens each quantity term of an order with an arm for
// every stored unit its declared arms do not cover (quantityBaseSQL), read
// from the rows of the kinds the read covers that declare the property
// without closing its units (readStoredUnits). A term orderExpr resolved to
// anything but the declared quantity expression is left as it is, and so is
// a property whose every declaration closes its units, so its expression
// stays the declared index's.
func (ds *dataset) quantityOrderTerms(ctx context.Context, x dbx, b *builder, types []*vocabulary.Kind, orders []substrate.Order, terms []orderTerm) ([]orderTerm, error) {
	for i, o := range orders {
		decls, ok := ds.quantityProp(types, o.Property)
		if !ok || i >= len(terms) {
			continue
		}
		v := `(props->` + sqlLiteral(o.Property) + `)`
		groups := groupQuantityDecls(decls)
		if terms[i].expr != quantityOrderSQL(v, groups) {
			continue
		}
		var open []string
		for _, d := range decls {
			if !quantityUnitsClosed(d.spec) {
				open = append(open, d.kind)
			}
		}
		if len(open) == 0 {
			continue
		}
		stored, err := readStoredUnits(ctx, x, b, open, o.Property)
		if err != nil {
			return nil, err
		}
		terms[i].expr = quantityOrderSQL(v, groups, stored...)
	}
	return terms, nil
}

// stripAnnotations is annotationsSQL in Go, so the CASE's keys are the codes
// the SQL compares them against.
func stripAnnotations(code string) string {
	var b strings.Builder
	depth := 0
	for _, r := range code {
		switch {
		case r == '{':
			depth++
		case r == '}' && depth > 0:
			depth--
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// linearSQL renders value x f + o with integer arithmetic and one division,
// which is where numeric rounds, and only past its twentieth digit.
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
		expr = `(` + expr + ` / ` + den.String() + `)`
	}
	return expr
}

// --- the narrowing guard ---

// quantityNarrows reports whether a changed quantity declaration could refuse
// a value the current one admits: a measure named or changed, a unit list
// added or shortened, a comparator turned off, a bound added or moved. A move
// of a bound in either direction counts: across units the direction is a
// conversion away, and the count is what decides.
func quantityNarrows(cur, cand *vocabulary.QuantitySpec) bool {
	if cand == nil {
		return false
	}
	if cur == nil {
		cur = &vocabulary.QuantitySpec{}
	}
	switch {
	case cand.Measure != "" && cand.Measure != cur.Measure:
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

// quantityStrands judges one stored root value for a quantity narrowing: true
// when a quantity under the declared path is one the candidate declaration's
// write coercion refuses, which is the whole rule, so the count and the next
// write cannot disagree.
func quantityStrands(path []fieldStep, cand *vocabulary.Property) func([]byte) bool {
	return func(raw []byte) bool {
		var root any
		if err := json.Unmarshal(raw, &root); err != nil {
			return false
		}
		for _, leaf := range leavesAtPath(root, path) {
			if _, ok := leaf.(map[string]any); !ok {
				continue
			}
			if _, err := coerceQuantity(cand, leaf); err != nil {
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
	var parts []string
	if cand.Measure != cur.Measure && cand.Measure != "" {
		parts = append(parts, "measures "+cand.Measure)
	}
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
	return strings.Join(parts, ", ")
}
