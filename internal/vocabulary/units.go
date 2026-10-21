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
//     beside J, kV.A beside V.A) or a factor of 1 (1/min beside /min) belongs
//     to that code's measure. Failing that, an annotated code whose key
//     without its annotations is a code of a listed measure (mmol/mol{creat}
//     beside mmol/mol) belongs to that measure.
//  5. Anything else belongs to the measure whose dimension and distinct atoms
//     (Gy, Sv, Bq, which never stand for the units they equal) it has, when
//     exactly one has them. Two (J/m is a force or an energy per distance; J/s is a
//     power or an apparent power) is ambiguous, and the property must declare
//     `measures:`. A measure marked listed (amount-fraction, score) takes no
//     code by this rule: its codes are the ones rules 1 and 4 give it.

import (
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
//
// A code that differs from one unitPrints names only by a prefix on its first
// atom prints that way too: `kV.A{reactive}` is `kvar`, `MW.h` is `MWh`.
//
// A print that is an alias of another code, or a spelling CanonicalUnit
// refuses, prints as the code instead: `{count}/min` would print `count/min`,
// which a writer typing the title back would store as `/min`.
func (u Unit) Print() string {
	p := u.symbol()
	if alias, ok := unitAliases[p]; ok && alias != u.code {
		return u.code
	}
	if _, refused := refusedSpellings[p]; refused {
		return u.code
	}
	return p
}

func (u Unit) symbol() string {
	if p, ok := unitPrints[u.code]; ok {
		return p
	}
	if u.local != nil {
		return u.local.print
	}
	if p, ok := prefixedPrint(u); ok {
		return p
	}
	return renderPrint(u.tree)
}

// prefixedPrint is the print of a code that is a unitPrints code with a
// prefix on its first atom, the prefix's print before the code's.
func prefixedPrint(u Unit) (string, bool) {
	t, err := loadTable()
	if err != nil || u.tree == nil || u.tree.reciprocal || len(u.tree.parts) == 0 {
		return "", false
	}
	first, ok := u.tree.parts[0].(unitNode)
	if !ok || first.prefix == "" {
		return "", false
	}
	for _, code := range t.prints[u.plain()] {
		if u.code != first.prefix+code {
			continue
		}
		for _, p := range prefixDefs {
			if p.code == first.prefix {
				return p.print + unitPrints[code], true
			}
		}
	}
	return "", false
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
// (`m2`, `m+2` and `m02`; `/s`, `s-1` and `1/s`; `kW.h` and `h.kW`). A factor
// of exactly 1 multiplies nothing and is left out. Nothing else cancels:
// `mol/mol` and `g/kg` keep their atoms, and two keys never meet by arithmetic.
func (u Unit) plain() string { return u.plainKey(true) }

// bare is the prefix-free key without the annotations: `mmol/mol{creat}`
// shares the bare key of `mol/mol`.
func (u Unit) bare() string { return u.plainKey(false) }

func (u Unit) plainKey(notes bool) string {
	if u.tree == nil {
		return u.code
	}
	var parts []string
	plainTerm(u.tree, 1, notes, &parts)
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

// plainTerm appends a term's occurrences to parts, each under the sign the
// term's position gives it, and its annotations where notes says.
func plainTerm(t *termNode, sign int, notes bool, parts *[]string) {
	if t.reciprocal {
		sign = -sign
	}
	note := func(text *string, s int) {
		if notes && text != nil {
			*parts = append(*parts, fmt.Sprintf("{%s}^%d", *text, s))
		}
	}
	for i, part := range t.parts {
		s := sign
		if i > 0 && t.ops[i-1] == '/' {
			s = -sign
		}
		switch x := part.(type) {
		case unitNode:
			*parts = append(*parts, fmt.Sprintf("%s^%d", x.atom.def.code, x.exp*s))
			note(x.note, s)
		case numberNode:
			if x.digits != "1" {
				*parts = append(*parts, fmt.Sprintf("%s^%d", x.digits, s))
			}
			note(x.note, s)
		case noteNode:
			note(&x.text, s)
		case groupNode:
			plainTerm(x.inner, s, notes, parts)
		}
	}
}

// ParseUnit reads one case-sensitive UCUM code over the table's atoms and
// prefixes, or one of the substrate's own codes (dBm). Aliases are not read
// here: CanonicalUnit maps them first.
//
// A code the table lists answers the parse the table made when it loaded,
// shared: nothing mutates a parsed Unit.
func ParseUnit(code string) (Unit, error) {
	t, err := loadTable()
	if err != nil {
		return Unit{}, err
	}
	if rows := t.rows[code]; len(rows) > 0 {
		return rows[0].unit, nil
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
// what the grammar does not read and the spellings refusedSpellings names.
func CanonicalUnit(input string) (string, error) {
	s := strings.TrimSpace(input)
	if why, refused := refusedSpellings[s]; refused {
		return "", fmt.Errorf("%q: %s", s, why)
	}
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

// RefusedUnitSpellings lists the spellings CanonicalUnit refuses by name, each
// with what to write instead.
func RefusedUnitSpellings() map[string]string {
	out := make(map[string]string, len(refusedSpellings))
	for k, v := range refusedSpellings {
		out[k] = v
	}
	return out
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
}

type unitTable struct {
	measures []Measure
	byName   map[string]*Measure
	rows     map[string][]unitRow // code -> its rows, in table order
	plain    map[string][]string  // prefix-free key (Unit.plain) -> measures listing a code of it
	// bare is the measures listing an unannotated code, by its prefix-free
	// key, among the measures that admit listed codes alone (Measure.listed).
	bare map[string][]string
	// primary is primaryMeasure by prefix-free key, so `mK` reads as `K` does.
	primary map[string]string
	// prints are the unitPrints codes whose first atom has no prefix, by
	// prefix-free key, for prefixedPrint.
	prints       map[string][]string
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
		plain: map[string][]string{}, bare: map[string][]string{}, bases: map[string]Unit{}, primary: map[string]string{},
		prints: map[string][]string{},
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
				if d.listed && len(nonZero(u.val.notes)) == 0 && !slices.Contains(t.bare[key], d.name) {
					t.bare[key] = append(t.bare[key], d.name)
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
	for code := range unitPrints {
		u, err := parse(code)
		if err != nil {
			return nil, fmt.Errorf("unitPrints names %s: %w", code, err)
		}
		if u.tree == nil || u.tree.reciprocal || len(u.tree.parts) == 0 {
			continue
		}
		if first, ok := u.tree.parts[0].(unitNode); ok && first.prefix == "" {
			t.prints[u.plain()] = append(t.prints[u.plain()], code)
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
	if names, key := t.plainNames(u); len(names) > 0 {
		if len(names) == 1 {
			return t.byName[names[0]], nil
		}
		if p, ok := t.primary[key]; ok {
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
		return nil, fmt.Errorf("%s (dimension %s) measures nothing the unit table names", u.code, u.val.dim)
	case 1:
		return t.byName[candidates[0]], nil
	}
	return nil, fmt.Errorf("%s is ambiguous: it could be %s; declare `measures:` on the property", u.code, orList(candidates))
}

// plainNames are the measures listing a code of the unit's prefix-free key,
// and that key. Where none does, an annotated code takes the listed-codes-only
// measure (Measure.listed) of its bare key, which the dimension rule never
// reaches: an annotation says what was measured, never which quantity, so
// `mmol/mol{creat}` is an amount-fraction as `mmol/mol` is, and converts
// only to the same annotation.
func (t *unitTable) plainNames(u Unit) ([]string, string) {
	key := u.plain()
	if names := t.plain[key]; len(names) > 0 || len(nonZero(u.val.notes)) == 0 {
		return names, key
	}
	key = u.bare()
	return t.bare[key], key
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
	names, _ := t.plainNames(u)
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
		return fmt.Errorf("a %s measure admits no code by its dimension", m.Scale)
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
	measure, err := ConversionMeasure(from, to)
	if err != nil {
		return nil, err
	}
	return ConvertIn(measure, v, from, to)
}

// ConversionMeasure names the measure Convert converts two codes in.
func ConversionMeasure(from, to string) (string, error) {
	mf, errFrom := MeasureOf(from)
	if errFrom == nil {
		if _, err := ResolveMeasure(to, mf.Name); err == nil {
			return mf.Name, nil
		}
	}
	mt, errTo := MeasureOf(to)
	if errTo == nil {
		if _, err := ResolveMeasure(from, mt.Name); err == nil {
			return mt.Name, nil
		}
	}
	switch {
	case errFrom != nil:
		return "", errFrom
	case errTo != nil:
		return "", errTo
	}
	return "", fmt.Errorf("%s is %s and %s is %s: a value converts only inside one measure", from, mf.Name, to, mt.Name)
}

// Conversion is one value ConvertDigits converted.
type Conversion struct {
	// Value is the converted digits: every one where the result has a finite
	// decimal expansion, else ConvertedDigits significant digits, and in
	// either case at most MaxDecimalDigits (fitDecimal).
	Value string
	// Unit is the code converted to.
	Unit string
	// Measure is the measure the value converted in.
	Measure string
	// Exact is false where Value was rounded.
	Exact bool
}

// ConvertDigits converts a value written as exact digits (CanonicalDecimal)
// between two unit spellings, each read through the aliases (`°C`, `kWh`).
// Without a measure the two codes are read as Convert reads them; a measure
// names the one to convert in (`temperature-difference` for a deviation). A
// refusal names the input it refuses: value, from, to or the conversion.
func ConvertDigits(value, from, to, measure string) (Conversion, error) {
	digits, err := CanonicalDecimal(value)
	if err != nil {
		return Conversion{}, fmt.Errorf("value: %w", err)
	}
	fromCode, err := CanonicalUnit(from)
	if err != nil {
		return Conversion{}, fmt.Errorf("from: %w", err)
	}
	toCode, err := CanonicalUnit(to)
	if err != nil {
		return Conversion{}, fmt.Errorf("to: %w", err)
	}
	if measure == "" {
		if measure, err = ConversionMeasure(fromCode, toCode); err != nil {
			return Conversion{}, err
		}
	}
	v, _ := new(big.Rat).SetString(digits)
	r, err := ConvertIn(measure, v, fromCode, toCode)
	if err != nil {
		return Conversion{}, err
	}
	out, exact := RatDecimal(r, ConvertedDigits)
	if _, err := CanonicalDecimal(out); err != nil {
		if out, err = fitDecimal(r); err != nil {
			return Conversion{}, err
		}
		exact = false
	}
	return Conversion{Value: out, Unit: toCode, Measure: measure, Exact: exact}, nil
}

// fitDecimal rounds a value half to even to the fraction digits a decimal of
// MaxDecimalDigits keeps beside its integer part, so a converted value can be
// written back. A value whose integer part, or whose first significant digit,
// lies past those digits does not fit and is refused naming the bound.
func fitDecimal(r *big.Rat) (string, error) {
	abs := new(big.Rat).Abs(r)
	intDigits := len(new(big.Int).Quo(abs.Num(), abs.Denom()).String())
	places := MaxDecimalDigits - intDigits
	if places < 0 {
		return "", fmt.Errorf("the converted value has %d integer digits, and a decimal holds at most %d digits", intDigits, MaxDecimalDigits)
	}
	scaled := new(big.Rat).Mul(abs, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(places)), nil)))
	q, rem := new(big.Int).QuoRem(scaled.Num(), scaled.Denom(), new(big.Int))
	switch new(big.Int).Mul(rem, big.NewInt(2)).Cmp(scaled.Denom()) {
	case 1:
		q.Add(q, big.NewInt(1))
	case 0:
		if q.Bit(0) == 1 {
			q.Add(q, big.NewInt(1))
		}
	}
	if q.Sign() == 0 {
		return "", fmt.Errorf("the converted value's first significant digit lies past the %d digits a decimal holds", MaxDecimalDigits)
	}
	out := placePoint(q.String(), places)
	if strings.Contains(out, ".") {
		out = strings.TrimRight(strings.TrimRight(out, "0"), ".")
	}
	if r.Sign() < 0 {
		out = "-" + out
	}
	if _, err := CanonicalDecimal(out); err != nil {
		return "", fmt.Errorf("the converted value: %w", err)
	}
	return out, nil
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
	if u.val.pi != t.bases[m.Name].val.pi {
		return nil, nil, fmt.Errorf("%s reaches %s only through pi: no rational number states it", code, m.Base)
	}
	factor, offset = t.toBase(m, u, nil)
	return factor, offset, nil
}

// toBase is the one formula from a unit to its measure's base:
// base = value * factor + offset, the factor the ratio of the two units'
// factors and the offset an absolute temperature's zero. A power of pi
// between them is read at pi, which only an order key passes (OrderFactor).
func (t *unitTable) toBase(m *Measure, u Unit, pi *big.Rat) (factor, offset *big.Rat) {
	factor = new(big.Rat).Quo(u.val.factor, t.bases[m.Name].val.factor)
	for n := u.val.pi - t.bases[m.Name].val.pi; n != 0 && pi != nil; {
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
	return factor, offset
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
	if m.Scale == ScaleLogarithmic || m.Scale == ScaleArbitrary || u.val.pi == t.bases[m.Name].val.pi {
		return ToBase(m.Name, code)
	}
	pi, _ := new(big.Rat).SetString(orderPi)
	factor, offset = t.toBase(m, u, pi)
	return factor, offset, nil
}

// PrintRules are what Unit.Print reads beside the grammar, for a client that
// renders a print symbol itself: each prefix's and atom's print, the print of
// every code unitPrints names, the ones of them whose first atom carries no
// prefix (prefixedPrint's), and the annotations that are a measure's
// identity.
type PrintRules struct {
	Prefixes    map[string]string
	Atoms       map[string]string
	Prints      map[string]string
	Wholes      []string
	Annotations []string
}

// UnitPrintRules answers the print rules.
func UnitPrintRules() PrintRules {
	r := PrintRules{Prefixes: map[string]string{}, Atoms: map[string]string{}, Prints: map[string]string{}}
	for _, p := range prefixDefs {
		r.Prefixes[p.code] = p.print
	}
	for _, a := range atomDefs {
		r.Atoms[a.code] = a.print
	}
	for code, print := range unitPrints {
		r.Prints[code] = print
	}
	if t, err := loadTable(); err == nil {
		for _, codes := range t.prints {
			r.Wholes = append(r.Wholes, codes...)
		}
	}
	sort.Strings(r.Wholes)
	for _, d := range measureDefs {
		if d.annotation != "" {
			r.Annotations = append(r.Annotations, d.annotation)
		}
	}
	return r
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
// W).
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
