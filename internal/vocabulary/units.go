package vocabulary

// The unit API: parse a code, name its measure, convert between two codes
// exactly. Everything here reads the table in unitstable.go through the
// grammar in unitsgrammar.go.
//
// A MEASURE is the substrate's word for a named quantity kind (mass,
// temperature, rate): a dimension alone cannot keep apart what a person needs
// kept apart (energy and torque, a fraction and a count, real and apparent
// power), so every conversion and comparison happens inside one measure.
//
// How a unit finds its measure, in order:
//
//  1. A code the table lists belongs to the measures that list it. Where two
//     measures list it (K, Cel and [degF]; 1) and the property names neither,
//     primaryMeasure decides.
//  2. An arbitrary atom ([IU]) puts the unit in `arbitrary`. A level or an
//     offset temperature is used exactly as the table lists it, never inside a
//     compound and never prefixed beyond its row.
//  3. A term of annotations and numbers alone ({steps}, 10*3{copies}) is a
//     `count`; an annotated count over a time ({beats}/min) is a `rate`.
//  4. A code that differs from a table code only by its metric prefixes (GJ
//     beside J, kV.A beside V.A) belongs to that code's measure.
//  5. Anything else belongs to the measure whose dimension and distinct atoms
//     (Gy, Sv, Bq, which never stand for the units they equal) it has, when
//     exactly one has them. Two (J/m is a force or an energy per distance; J/s is a
//     power or an apparent power) is ambiguous, and the property must declare
//     `measures:`. A measure marked listed (amount-fraction, score) takes no
//     code by this rule: its codes are the ones rules 1 and 4 give it.

import (
	"errors"
	"fmt"
	"math/big"
	"slices"
	"sort"
	"strings"
	"sync"
)

// Measure is one named quantity kind of the unit table.
type Measure struct {
	// Name is how a declaration names it: `measures: mass`.
	Name string
	// Base is the unit every value of the measure converts to for ordering
	// and aggregation; empty for `arbitrary`, which has none.
	Base string
	// Scale says how the measure's values convert and combine.
	Scale Scale
	// Display is the unit a client shows a value in when nothing else says.
	Display string
	// Units are the codes the table lists for the measure. A compound or
	// prefixed code of the same dimension belongs to it too (MeasureOf).
	Units []string
	// Note is the one fact a reader needs about the measure, or empty.
	Note string

	dim        dimension
	annotation string
	listed     bool
	// distinct is the base's distinct-atom identity, which every unit of the
	// measure carries.
	distinct string
}

// Dimension renders the measure's dimension over the nine base quantities
// (L M T I Θ N J A B), "1" for a dimensionless one, and "" where a dimension
// says nothing about conversion (levels and arbitrary units).
func (m Measure) Dimension() string {
	switch m.Scale {
	case ScaleLogarithmic, ScaleArbitrary:
		return ""
	}
	return m.dim.String()
}

// Unit is one parsed unit code.
type Unit struct {
	code string
	tree *termNode
	val  value
	// local is set for a code the substrate defines (dBm).
	local *atomDef
	// offset is set when the code is exactly one affine atom (Cel, [degF]):
	// its zero in kelvin on the absolute scale.
	offset *big.Rat
	// level says the code is exactly one level (dB, [pH], dBm).
	level bool
	// misused says the code places a level or an offset temperature inside a
	// compound, under an exponent or under a prefix the table does not list:
	// valid UCUM, and a unit no measure here admits.
	misused bool
}

// Code is the code as written.
func (u Unit) Code() string { return u.code }

// Print is the unit's print symbol: `°C` for Cel, `μg` for ug, `kWh` for
// kW.h, `beats/min` for {beats}/min. It is for display; a stored value holds
// the code.
func (u Unit) Print() string {
	if p, ok := unitPrints[u.code]; ok {
		return p
	}
	if u.local != nil {
		return u.local.print
	}
	return renderPrint(u.tree)
}

// annotations is the unit's annotation identity: two units convert only when
// it is equal.
func (u Unit) annotations() string { return countKey(u.val.notes) }

// arbitraries is the unit's arbitrary-atom identity, the same way.
func (u Unit) arbitraries() string { return countKey(u.val.arbitrary) }

// distincts is the unit's distinct-atom identity (Gy, Sv, Bq), the same way.
func (u Unit) distincts() string { return countKey(u.val.distinct) }

func countKey(m map[string]int) string {
	var parts []string
	for k, n := range m {
		if n != 0 {
			parts = append(parts, fmt.Sprintf("{%s}%d", k, n))
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

// plain is the code's prefix-free key: `GJ` and `kJ` share the key of `J`. It
// lists every atom, number and annotation the term holds with the signed
// exponent it carries, in a fixed order, so two spellings of one term share it
// (`m2`, `m+2` and `m02`; `/s` and `s-1`; `kW.h` and `h.kW`). Nothing cancels:
// `mol/mol` and `g/kg` keep their atoms, and two keys never meet by arithmetic.
func (u Unit) plain() string {
	if u.tree == nil {
		return u.code
	}
	var parts []string
	plainTerm(u.tree, 1, &parts)
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

// plainTerm appends a term's occurrences to parts, each under the sign the
// term's position gives it.
func plainTerm(t *termNode, sign int, parts *[]string) {
	if t.reciprocal {
		sign = -sign
	}
	for i, part := range t.parts {
		s := sign
		if i > 0 && t.ops[i-1] == '/' {
			s = -sign
		}
		switch x := part.(type) {
		case unitNode:
			*parts = append(*parts, fmt.Sprintf("%s^%d", x.atom.def.code, x.exp*s))
			if x.note != nil {
				*parts = append(*parts, fmt.Sprintf("{%s}^%d", *x.note, s))
			}
		case numberNode:
			*parts = append(*parts, fmt.Sprintf("%s^%d", x.digits, s))
			if x.note != nil {
				*parts = append(*parts, fmt.Sprintf("{%s}^%d", *x.note, s))
			}
		case noteNode:
			*parts = append(*parts, fmt.Sprintf("{%s}^%d", x.text, s))
		case groupNode:
			plainTerm(x.inner, s, parts)
		}
	}
}

// ParseUnit reads one case-sensitive UCUM code over the table's atoms and
// prefixes, or one of the substrate's own codes (dBm). Aliases are not read
// here: CanonicalUnit maps them first.
func ParseUnit(code string) (Unit, error) {
	if _, err := loadTable(); err != nil {
		return Unit{}, err
	}
	return parseUnit(code)
}

// parseUnit is ParseUnit without waiting on the table, which the table's own
// build needs.
func parseUnit(code string) (Unit, error) {
	if d, ok := localUnits[code]; ok {
		return Unit{code: code, local: &d, level: true, val: unitValue()}, nil
	}
	t, err := parseTerm(code, lookupAtom)
	if err != nil {
		return Unit{}, err
	}
	val, err := evalNode(t)
	if err != nil {
		return Unit{}, fmt.Errorf("%q: %w", code, err)
	}
	u := Unit{code: code, tree: t, val: val}
	if len(u.val.specials) > 0 {
		u.misused = true
		if one, ok := soleUnit(t); ok {
			switch one.atom.def.class {
			case atomAffine:
				if one.prefix == "" {
					u.offset, u.misused = one.atom.offset, false
				}
			case atomLevel:
				u.level, u.misused = true, false
			}
		}
	}
	return u, nil
}

// soleUnit is the one simple unit a code consists of, unannotated and raised
// to no power, if that is what it is.
func soleUnit(t *termNode) (unitNode, bool) {
	if t.reciprocal || len(t.parts) != 1 {
		return unitNode{}, false
	}
	un, ok := t.parts[0].(unitNode)
	if !ok || un.note != nil || un.exp != 1 {
		return unitNode{}, false
	}
	return un, true
}

// CanonicalUnit maps an input spelling to the code a quantity stores: an alias
// (`°C`, `mmHg`, `bpm`, `kWh`, `mcg`) to its code, both micro signs and the
// superscript digits to their ASCII spellings (`μg/m³` is `ug/m3`), and refuses
// what the grammar does not read.
func CanonicalUnit(input string) (string, error) {
	s := strings.TrimSpace(input)
	if alias, ok := unitAliases[s]; ok {
		s = alias
	} else if n := normalizeUnitCharacters(s); n != s {
		s = n
		if alias, ok := unitAliases[s]; ok {
			s = alias
		}
	}
	if _, err := ParseUnit(s); err != nil {
		return "", err
	}
	return s, nil
}

func normalizeUnitCharacters(s string) string {
	var b strings.Builder
	for _, r := range s {
		if to, ok := unitCharacters[r]; ok {
			b.WriteString(to)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// UnitAliases lists every input alias and the code it maps to.
func UnitAliases() map[string]string {
	out := make(map[string]string, len(unitAliases))
	for k, v := range unitAliases {
		out[k] = v
	}
	return out
}

// --- the measure table, loaded once ---

type unitRow struct {
	measure string
	unit    Unit
	// factor and offset convert a value to the measure's base:
	// base = value * factor + offset. Nil factor: no rational factor exists
	// (levels, arbitrary units, rad against deg).
	factor *big.Rat
	offset *big.Rat
}

type unitTable struct {
	measures []Measure
	byName   map[string]*Measure
	rows     map[string][]unitRow // code -> its rows, in table order
	plain    map[string][]string  // prefix-free key (Unit.plain) -> measures listing a code of it
	// primary is primaryMeasure by prefix-free key, so `mK` reads as `K` does.
	primary      map[string]string
	bases        map[string]Unit
	codesInOrder []string
}

var (
	tableOnce sync.Once
	table     *unitTable
	tableErr  error
)

func loadTable() (*unitTable, error) {
	tableOnce.Do(func() { table, tableErr = buildTable() })
	return table, tableErr
}

func buildTable() (*unitTable, error) {
	if _, err := atoms(); err != nil {
		return nil, err
	}
	t := &unitTable{
		measures: make([]Measure, 0, len(measureDefs)),
		byName:   map[string]*Measure{}, rows: map[string][]unitRow{},
		plain: map[string][]string{}, bases: map[string]Unit{}, primary: map[string]string{},
	}
	parse := parseUnit
	for _, d := range measureDefs {
		if _, dup := t.byName[d.name]; dup {
			return nil, fmt.Errorf("measure %s declared twice", d.name)
		}
		m := Measure{
			Name: d.name, Base: d.base, Scale: d.scale, Display: d.display,
			Units: slices.Clone(d.units), Note: d.note, annotation: d.annotation, listed: d.listed,
		}
		var base Unit
		if d.base != "" {
			b, err := parse(d.base)
			if err != nil {
				return nil, fmt.Errorf("measure %s: base %s: %w", d.name, d.base, err)
			}
			base = b
			m.dim = b.val.dim
			m.distinct = b.distincts()
		}
		t.bases[d.name] = base
		if !slices.Contains(d.units, d.display) {
			return nil, fmt.Errorf("measure %s: display unit %s is not one of its codes", d.name, d.display)
		}
		t.measures = append(t.measures, m)
		t.byName[d.name] = &t.measures[len(t.measures)-1]
		seen := map[string]bool{}
		for _, code := range d.units {
			if seen[code] {
				return nil, fmt.Errorf("measure %s lists %s twice", d.name, code)
			}
			seen[code] = true
			u, err := parse(code)
			if err != nil {
				return nil, fmt.Errorf("measure %s: %w", d.name, err)
			}
			row := unitRow{measure: d.name, unit: u}
			if d.base != "" {
				row.factor, row.offset = baseFactor(m.Scale, u, base)
			}
			if d.scale != ScaleLogarithmic && d.scale != ScaleArbitrary && d.scale != ScaleCount && u.val.dim != m.dim {
				return nil, fmt.Errorf("measure %s: %s has dimension %s, the measure %s", d.name, code, u.val.dim, m.dim)
			}
			if d.base != "" && u.distincts() != m.distinct {
				return nil, fmt.Errorf("measure %s: %s carries %q, the measure %q", d.name, code, u.distincts(), m.distinct)
			}
			if _, listed := t.rows[code]; !listed {
				t.codesInOrder = append(t.codesInOrder, code)
			}
			t.rows[code] = append(t.rows[code], row)
			if u.tree != nil {
				key := u.plain()
				if !slices.Contains(t.plain[key], d.name) {
					t.plain[key] = append(t.plain[key], d.name)
				}
			}
		}
	}
	for code, rows := range t.rows {
		if len(rows) > 1 {
			p, ok := primaryMeasure[code]
			if !ok {
				return nil, fmt.Errorf("%s is listed by %d measures and primaryMeasure names none of them", code, len(rows))
			}
			if !slices.ContainsFunc(rows, func(r unitRow) bool { return r.measure == p }) {
				return nil, fmt.Errorf("primaryMeasure sends %s to %s, which does not list it", code, p)
			}
		}
	}
	for code, measure := range primaryMeasure {
		u, err := parse(code)
		if err != nil {
			return nil, fmt.Errorf("primaryMeasure names %s: %w", code, err)
		}
		t.primary[u.plain()] = measure
	}
	for alias, code := range unitAliases {
		if _, err := parse(alias); err == nil {
			return nil, fmt.Errorf("alias %q is itself a code", alias)
		}
		if _, err := parse(code); err != nil {
			return nil, fmt.Errorf("alias %q maps to %q: %w", alias, code, err)
		}
	}
	return t, nil
}

// baseFactor is the exact conversion of u to base on a scale, or nil where no
// rational number states it.
func baseFactor(scale Scale, u, base Unit) (factor, offset *big.Rat) {
	switch scale {
	case ScaleLogarithmic, ScaleArbitrary:
		return nil, nil
	}
	if u.val.pi != base.val.pi {
		return nil, nil
	}
	factor = new(big.Rat).Quo(u.val.factor, base.val.factor)
	offset = new(big.Rat)
	if scale == ScaleAffine && u.offset != nil {
		offset.Set(u.offset)
	}
	return factor, offset
}

// Measures lists every measure, in table order.
func Measures() []Measure {
	t, err := loadTable()
	if err != nil {
		return nil
	}
	out := make([]Measure, len(t.measures))
	for i, m := range t.measures {
		m.Units = slices.Clone(m.Units)
		out[i] = m
	}
	return out
}

// LookupMeasure finds a measure by name.
func LookupMeasure(name string) (Measure, bool) {
	t, err := loadTable()
	if err != nil {
		return Measure{}, false
	}
	m, ok := t.byName[name]
	if !ok {
		return Measure{}, false
	}
	out := *m
	out.Units = slices.Clone(m.Units)
	return out, true
}

// UnitCodes lists every code the table lists, in table order, each once.
func UnitCodes() []string {
	t, err := loadTable()
	if err != nil {
		return nil
	}
	return slices.Clone(t.codesInOrder)
}

// MeasureOf names the measure a code belongs to when nothing declares one (the
// package comment lists the rules). An ambiguous code is refused, naming the
// measures it could be.
func MeasureOf(code string) (Measure, error) {
	return ResolveMeasure(code, "")
}

// ResolveMeasure names the measure a code belongs to under a property's
// declared `measures:`; an empty declaration is MeasureOf.
func ResolveMeasure(code, declared string) (Measure, error) {
	t, err := loadTable()
	if err != nil {
		return Measure{}, err
	}
	u, err := ParseUnit(code)
	if err != nil {
		return Measure{}, err
	}
	m, err := t.resolve(u, declared)
	if err != nil {
		return Measure{}, err
	}
	out := *m
	out.Units = slices.Clone(m.Units)
	return out, nil
}

// errNoMeasure marks a valid code no measure admits.
var errNoMeasure = errors.New("no measure")

func (t *unitTable) resolve(u Unit, declared string) (*Measure, error) {
	if declared != "" {
		m, ok := t.byName[declared]
		if !ok {
			return nil, fmt.Errorf("%q is not a measure", declared)
		}
		return m, t.admits(m, u)
	}
	if rows := t.rows[u.code]; len(rows) > 0 {
		if len(rows) == 1 {
			return t.byName[rows[0].measure], nil
		}
		return t.byName[primaryMeasure[u.code]], nil
	}
	if err := t.notInCompound(u); err != nil {
		return nil, err
	}
	if len(nonZero(u.val.arbitrary)) > 0 {
		return t.byName["arbitrary"], nil
	}
	if m := t.counted(u); m != nil {
		return m, nil
	}
	if names := t.plain[u.plain()]; len(names) > 0 {
		if len(names) == 1 {
			return t.byName[names[0]], nil
		}
		if p, ok := t.primary[u.plain()]; ok {
			return t.byName[p], nil
		}
		return nil, fmt.Errorf("%s is ambiguous: it could be %s; declare `measures:` on the property", u.code, orList(names))
	}
	var candidates []string
	for i := range t.measures {
		m := &t.measures[i]
		if !m.listed && t.dimensionAdmits(m, u) == nil {
			candidates = append(candidates, m.Name)
		}
	}
	// A measure whose units carry an annotation (reactive power) claims a unit
	// carrying it outright.
	for _, name := range candidates {
		if a := t.byName[name].annotation; a != "" && u.val.notes[a] > 0 {
			return t.byName[name], nil
		}
	}
	switch len(candidates) {
	case 0:
		return nil, fmt.Errorf("%w: %s (dimension %s) measures nothing the unit table names", errNoMeasure, u.code, u.val.dim)
	case 1:
		return t.byName[candidates[0]], nil
	}
	return nil, fmt.Errorf("%s is ambiguous: it could be %s; declare `measures:` on the property", u.code, orList(candidates))
}

// admits says whether a declared measure takes a code.
func (t *unitTable) admits(m *Measure, u Unit) error {
	if rows := t.rows[u.code]; len(rows) > 0 {
		var names []string
		for _, r := range rows {
			if r.measure == m.Name {
				return nil
			}
			names = append(names, r.measure)
		}
		return fmt.Errorf("%s measures %s, not %s", u.code, orList(names), m.Name)
	}
	if err := t.notInCompound(u); err != nil {
		return err
	}
	switch m.Scale {
	case ScaleLogarithmic:
		return fmt.Errorf("%s is not a %s unit: a level is one of %s", u.code, m.Name, strings.Join(m.Units, ", "))
	case ScaleArbitrary:
		if len(nonZero(u.val.arbitrary)) == 0 {
			return fmt.Errorf("%s is not an arbitrary unit: it names no assay unit such as [IU]", u.code)
		}
		return nil
	}
	if len(nonZero(u.val.arbitrary)) > 0 {
		return fmt.Errorf("%s is an arbitrary unit, not %s", u.code, m.Name)
	}
	if c := t.counted(u); c != nil {
		if c.Name == m.Name {
			return nil
		}
		return fmt.Errorf("%s counts things (%s), not %s", u.code, c.Name, m.Name)
	}
	if m.Scale == ScaleCount {
		return fmt.Errorf("%s is not a count: a count is an annotation such as {steps}", u.code)
	}
	names := t.plain[u.plain()]
	if len(names) > 0 && !slices.Contains(names, m.Name) {
		return fmt.Errorf("%s measures %s, not %s", u.code, orList(names), m.Name)
	}
	if m.listed && len(names) == 0 {
		return fmt.Errorf("%s is not %s: the measure admits %s and those codes under another prefix, nothing else",
			u.code, m.Name, strings.Join(m.Units, ", "))
	}
	return t.dimensionAdmits(m, u)
}

// dimensionAdmits is the last rule: the dimension, and a measure's own
// annotation.
func (t *unitTable) dimensionAdmits(m *Measure, u Unit) error {
	switch m.Scale {
	case ScaleLogarithmic, ScaleArbitrary, ScaleCount:
		return fmt.Errorf("%w", errNoMeasure)
	}
	if u.val.dim != m.dim {
		return fmt.Errorf("%s (dimension %s) is not %s (dimension %s)", u.code, u.val.dim, m.Name, m.dim)
	}
	if m.annotation != "" && u.val.notes[m.annotation] <= 0 {
		return fmt.Errorf("a %s unit carries {%s}", m.Name, m.annotation)
	}
	if u.distincts() != m.distinct {
		return fmt.Errorf("%s is not %s: Gy, Sv and Bq never stand for the units they equal, nor one for another", u.code, m.Name)
	}
	return nil
}

// counted names the measure of a term that counts things: annotations and
// numbers alone are a count, the same over a time a rate.
func (t *unitTable) counted(u Unit) *Measure {
	if !u.val.counted {
		return nil
	}
	if u.val.dim.zero() && u.val.pi == 0 && t.namesNothing(u) {
		return t.byName["count"]
	}
	if u.val.dim == (dimension{dimTime: -1}) {
		return t.byName["rate"]
	}
	return nil
}

// namesNothing says a term holds no atom but numbers: `{steps}`, `10*3{x}`.
func (t *unitTable) namesNothing(u Unit) bool {
	if u.tree == nil {
		return false
	}
	return !namesAtom(u.tree)
}

func namesAtom(tn *termNode) bool {
	for _, n := range tn.parts {
		switch x := n.(type) {
		case unitNode:
			if c := x.atom.def.code; c != "10*" && c != "10^" {
				return true
			}
		case groupNode:
			if namesAtom(x.inner) {
				return true
			}
		}
	}
	return false
}

// notInCompound refuses a level or an offset temperature anywhere but alone
// as the table lists it.
func (t *unitTable) notInCompound(u Unit) error {
	if u.misused || (u.level && len(t.rows[u.code]) == 0) || (u.offset != nil && len(t.rows[u.code]) == 0) {
		return fmt.Errorf("%s: a level or an offset temperature is used exactly as the table lists it, never inside a compound or under another prefix", u.code)
	}
	return nil
}

func nonZero(m map[string]int) map[string]int {
	out := map[string]int{}
	for k, n := range m {
		if n != 0 {
			out[k] = n
		}
	}
	return out
}

func orList(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}

// --- conversion ---

// Convert converts a value between two codes of one measure, exactly. The
// measure is the one both codes share, read the way MeasureOf reads them, so
// Cel to [degF] is an absolute temperature (37 Cel is 98.6 [degF]); ConvertIn
// names the measure where that is not what is meant. It refuses codes in two
// measures, two arbitrary atoms, two annotations, two distinct atoms (Gy and
// Sv), a level, and a factor no rational number states (rad to deg).
//
// The two codes are read the same way whichever comes first: where one alone is
// ambiguous (J/s is a power or an apparent power), the other's measure decides.
func Convert(v *big.Rat, from, to string) (*big.Rat, error) {
	mf, errFrom := MeasureOf(from)
	if errFrom == nil {
		if _, err := ResolveMeasure(to, mf.Name); err == nil {
			return ConvertIn(mf.Name, v, from, to)
		}
	}
	mt, errTo := MeasureOf(to)
	if errTo == nil {
		if _, err := ResolveMeasure(from, mt.Name); err == nil {
			return ConvertIn(mt.Name, v, from, to)
		}
	}
	switch {
	case errFrom != nil:
		return nil, errFrom
	case errTo != nil:
		return nil, errTo
	}
	return nil, fmt.Errorf("%s is %s and %s is %s: a value converts only inside one measure", from, mf.Name, to, mt.Name)
}

// ConvertIn converts a value between two codes of the named measure, exactly:
// in `temperature` an offset applies (0 Cel is 32 [degF]), in
// `temperature-difference` it does not (1 Cel is 1.8 [degF]).
func ConvertIn(measure string, v *big.Rat, from, to string) (*big.Rat, error) {
	t, err := loadTable()
	if err != nil {
		return nil, err
	}
	fu, err := ParseUnit(from)
	if err != nil {
		return nil, err
	}
	tu, err := ParseUnit(to)
	if err != nil {
		return nil, err
	}
	m, err := t.resolve(fu, measure)
	if err != nil {
		return nil, err
	}
	if err := t.admits(m, tu); err != nil {
		return nil, err
	}
	if from == to {
		return new(big.Rat).Set(v), nil
	}
	ff, fo, err := t.linear(m, fu, tu)
	if err != nil {
		return nil, err
	}
	out := new(big.Rat).Mul(v, ff)
	return out.Add(out, fo), nil
}

// linear is the exact map from one code to another inside a measure:
// to = from * factor + offset.
func (t *unitTable) linear(m *Measure, from, to Unit) (factor, offset *big.Rat, err error) {
	if from.code == to.code {
		return big.NewRat(1, 1), new(big.Rat), nil
	}
	switch m.Scale {
	case ScaleLogarithmic:
		return nil, nil, fmt.Errorf("%s and %s are levels: a level converts to nothing but itself", from.code, to.code)
	}
	if from.annotations() != to.annotations() {
		return nil, nil, fmt.Errorf("%s and %s count different things: an annotation is part of a unit, and two never convert", from.code, to.code)
	}
	if from.arbitraries() != to.arbitraries() {
		return nil, nil, fmt.Errorf("%s and %s are different arbitrary units: an assay unit converts only to itself under another metric prefix", from.code, to.code)
	}
	if from.distincts() != to.distincts() {
		return nil, nil, fmt.Errorf("%s and %s are kept apart: Gy, Sv and Bq never convert to the units they equal, nor one to another", from.code, to.code)
	}
	if from.val.pi != to.val.pi {
		return nil, nil, fmt.Errorf("%s and %s differ by a power of pi: no rational number states the conversion", from.code, to.code)
	}
	if from.val.dim != to.val.dim {
		return nil, nil, fmt.Errorf("%s and %s have different dimensions", from.code, to.code)
	}
	factor = new(big.Rat).Quo(from.val.factor, to.val.factor)
	offset = new(big.Rat)
	if m.Scale == ScaleAffine {
		// to = ((from * ff + fo) - to_o) / tf, with ff and tf the factors to
		// kelvin and fo and to_o the zeros in kelvin.
		fo, too := new(big.Rat), new(big.Rat)
		if from.offset != nil {
			fo.Set(from.offset)
		}
		if to.offset != nil {
			too.Set(to.offset)
		}
		offset.Sub(fo, too)
		offset.Quo(offset, to.val.factor)
	}
	return factor, offset, nil
}

// ToBase is the exact map from a code to its measure's base unit:
// base = value * factor + offset. It refuses a level, an arbitrary unit and a
// code no rational factor reaches (rad), whose values compare only in their own
// unit.
func ToBase(measure, code string) (factor, offset *big.Rat, err error) {
	t, err := loadTable()
	if err != nil {
		return nil, nil, err
	}
	u, err := ParseUnit(code)
	if err != nil {
		return nil, nil, err
	}
	m, err := t.resolve(u, measure)
	if err != nil {
		return nil, nil, err
	}
	switch m.Scale {
	case ScaleLogarithmic, ScaleArbitrary:
		return nil, nil, fmt.Errorf("%s is a %s unit: it has no base to convert to", code, m.Scale)
	}
	base := t.bases[m.Name]
	if u.val.pi != base.val.pi {
		return nil, nil, fmt.Errorf("%s reaches %s only through pi: no rational number states it", code, m.Base)
	}
	factor = new(big.Rat).Quo(u.val.factor, base.val.factor)
	offset = new(big.Rat)
	if m.Scale == ScaleAffine && u.offset != nil {
		offset.Set(u.offset)
	}
	return factor, offset, nil
}

// orderPi is pi to 40 places, the value OrderFactor reads it at.
const orderPi = "3.1415926535897932384626433832795028841971"

// OrderFactor is ToBase for an ordering key: the same exact map, except that a
// code no rational factor reaches (rad, which reaches deg only through pi)
// takes its factor with pi to 40 places, because an order is a key and
// nothing compares against it. It still refuses a level and an arbitrary
// unit, which have no base.
func OrderFactor(measure, code string) (factor, offset *big.Rat, err error) {
	t, err := loadTable()
	if err != nil {
		return nil, nil, err
	}
	u, err := ParseUnit(code)
	if err != nil {
		return nil, nil, err
	}
	m, err := t.resolve(u, measure)
	if err != nil {
		return nil, nil, err
	}
	base := t.bases[m.Name]
	if m.Scale == ScaleLogarithmic || m.Scale == ScaleArbitrary || u.val.pi == base.val.pi {
		return ToBase(m.Name, code)
	}
	pi, _ := new(big.Rat).SetString(orderPi)
	factor = new(big.Rat).Quo(u.val.factor, base.val.factor)
	for n := u.val.pi - base.val.pi; n != 0; {
		if n > 0 {
			factor.Mul(factor, pi)
			n--
		} else {
			factor.Quo(factor, pi)
			n++
		}
	}
	offset = new(big.Rat)
	if m.Scale == ScaleAffine && u.offset != nil {
		offset.Set(u.offset)
	}
	return factor, offset, nil
}

// UnitSymbol is a code's print symbol, or the code itself when it does not
// parse.
func UnitSymbol(code string) string {
	u, err := ParseUnit(code)
	if err != nil {
		return code
	}
	return u.Print()
}

// ConvertedDigits is how many significant digits RatDecimal keeps of a
// converted value that has no finite decimal expansion (1 kcal/d is 523/10800
// W): the precision an ordering key reads a quantity at, too.
const ConvertedDigits = 20

// RatDecimal writes an exact value as decimal digits. A value with a finite
// decimal expansion is written whole and exact is true (72.4, 98.6, 0.0001);
// any other is rounded half to even to `significant` significant digits and
// exact is false, so a caller can say it rounded.
func RatDecimal(r *big.Rat, significant int) (digits string, exact bool) {
	if r.Sign() == 0 {
		return "0", true
	}
	neg := r.Sign() < 0
	num := new(big.Int).Abs(r.Num())
	den := new(big.Int).Set(r.Denom())
	sign := ""
	if neg {
		sign = "-"
	}
	// A finite expansion: the denominator holds no prime but 2 and 5.
	rest, twos, fives := new(big.Int).Set(den), 0, 0
	two, five, ten := big.NewInt(2), big.NewInt(5), big.NewInt(10)
	m := new(big.Int)
	for new(big.Int).Mod(rest, two).Sign() == 0 {
		rest.Quo(rest, two)
		twos++
	}
	for m.Mod(rest, five).Sign() == 0 {
		rest.Quo(rest, five)
		fives++
	}
	if rest.Cmp(big.NewInt(1)) == 0 {
		scale := max(twos, fives)
		n := new(big.Int).Mul(num, new(big.Int).Exp(ten, big.NewInt(int64(scale)), nil))
		n.Quo(n, den)
		return sign + placePoint(n.String(), scale), true
	}
	if significant < 1 {
		significant = 1
	}
	abs := new(big.Rat).SetFrac(num, den)
	// k is the decimal exponent: 10^(k-1) <= |r| < 10^k.
	k := len(num.String()) - len(den.String()) + 1
	pow := func(e int) *big.Rat {
		if e >= 0 {
			return new(big.Rat).SetInt(new(big.Int).Exp(ten, big.NewInt(int64(e)), nil))
		}
		return new(big.Rat).SetFrac(big.NewInt(1), new(big.Int).Exp(ten, big.NewInt(int64(-e)), nil))
	}
	for abs.Cmp(pow(k-1)) < 0 {
		k--
	}
	for abs.Cmp(pow(k)) >= 0 {
		k++
	}
	scaled := new(big.Rat).Mul(abs, pow(significant-k))
	q, rem := new(big.Int).QuoRem(scaled.Num(), scaled.Denom(), new(big.Int))
	switch new(big.Int).Mul(rem, two).Cmp(scaled.Denom()) {
	case 1:
		q.Add(q, big.NewInt(1))
	case 0:
		if q.Bit(0) == 1 {
			q.Add(q, big.NewInt(1))
		}
	}
	if len(q.String()) > significant {
		q.Quo(q, ten)
		k++
	}
	shift := significant - k
	s := q.String()
	if shift <= 0 {
		return sign + s + strings.Repeat("0", -shift), false
	}
	out := placePoint(s, shift)
	if strings.Contains(out, ".") {
		out = strings.TrimRight(strings.TrimRight(out, "0"), ".")
	}
	return sign + out, false
}

// placePoint writes digits with `scale` of them after a decimal point.
func placePoint(digits string, scale int) string {
	if scale == 0 {
		return digits
	}
	if len(digits) <= scale {
		digits = strings.Repeat("0", scale-len(digits)+1) + digits
	}
	return digits[:len(digits)-scale] + "." + digits[len(digits)-scale:]
}
