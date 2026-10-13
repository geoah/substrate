package vocabulary

// THE UCUM GRAMMAR, case-sensitive, over the atoms and prefixes of
// unitstable.go. A code is a term of components joined by `.` (multiply) and
// `/` (divide, left to right), with an optional leading `/` for a reciprocal.
// A component is a simple unit (an atom, or a metric atom under a prefix) with
// an optional signed integer exponent, a positive integer factor, an
// annotation in braces, a simple unit or factor followed by an annotation, or a
// parenthesized term (https://ucum.org/ucum section 2, and the formal grammar
// at github.com/ucum-org/ucum/blob/main/docs/formal-grammar.md).
//
// The parse yields an exact value: a rational factor and a power of pi over a
// nine-component dimension, plus what the substrate keeps beside it and UCUM
// throws away: the annotations (part of a unit's identity here), the
// arbitrary atoms and the special atoms used.

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// errUnknownSymbol marks a code that is well formed but names an atom this
// table does not carry. The UCUM conformance run tells it apart from a grammar
// error, because the table is a subset and the suite is not.
var errUnknownSymbol = errors.New("unknown unit symbol")

// dimension is the exponent of each of the nine base quantities.
type dimension [dimCount]int8

func (d dimension) zero() bool { return d == dimension{} }

func (d dimension) String() string {
	var parts []string
	for i, e := range d {
		switch e {
		case 0:
		case 1:
			parts = append(parts, dimNames[i])
		default:
			parts = append(parts, fmt.Sprintf("%s%d", dimNames[i], e))
		}
	}
	if len(parts) == 0 {
		return "1"
	}
	return strings.Join(parts, " ")
}

// value is what a term denotes: factor x pi^pi in the coherent units of dim.
type value struct {
	factor *big.Rat
	pi     int
	dim    dimension
	// notes counts each annotation by the side of the fraction it sits on:
	// `{beats}/min` holds beats once, `/{beats}` holds it minus once.
	notes map[string]int
	// arbitrary counts each arbitrary atom by its exponent.
	arbitrary map[string]int
	// distinct counts each distinct atom (Gy, Sv, Bq) by its exponent.
	distinct map[string]int
	// specials are the affine and level atoms the term uses.
	specials []string
	// counted says an annotation or a factor stands as a component on its own
	// (`{steps}`, `{beats}/min`): the term counts things.
	counted bool
	// symbols counts the simple units, so a term of numbers and annotations
	// alone is told apart from one that names something.
	symbols int
	// pi in an atom definition is an atom like any other; this marks the use.
}

func unitValue() value {
	return value{factor: big.NewRat(1, 1), notes: map[string]int{}, arbitrary: map[string]int{}, distinct: map[string]int{}}
}

// MaxUnitDimension bounds each base quantity's exponent in a unit's dimension,
// in every atom raised to its power and in every product along the way. The
// table's atoms stay within ±4 (the farad); past the bound a code measures
// nothing a person writes, and the int8 a dimension holds would wrap.
const MaxUnitDimension = 8

// The bounds a code is held to while it is parsed, before anything is
// evaluated, so a short code cannot buy a long evaluation (`10*1000000000`
// once asked for a billion rational multiplications). Inside them the work
// and the size of the factor are small: at most MaxUnitComponents factors,
// each a table constant or a number of MaxUnitFactorDigits raised to at most
// MaxUnitExponent, by squaring.
const (
	// MaxUnitExponent bounds a written exponent: `10*24`, the largest power
	// UCUM's prefixes name, fits, and `10*25` does not. A dimension is held
	// tighter still, by MaxUnitDimension.
	MaxUnitExponent = 24
	// MaxUnitFactorDigits bounds a numeric factor's digits (`8.h`, `100.m`).
	MaxUnitFactorDigits = 12
	// MaxUnitNesting bounds how deep parentheses nest.
	MaxUnitNesting = 8
	// MaxUnitComponents bounds the components of a whole code, parenthesized
	// ones included.
	MaxUnitComponents = 32
)

// mul folds o into v, raised to the power exp. It refuses a division by zero
// rather than panic: the parser refuses a zero factor, and this holds the
// arithmetic to the same rule whatever reaches it. The dimension is checked
// before anything is computed, in int, so it never narrows.
func (v *value) mul(o value, exp int) error {
	if exp < 0 && o.factor.Sign() == 0 {
		return errZeroDivisor
	}
	var dim dimension
	for i := range v.dim {
		e := int(v.dim[i])
		if o.dim[i] != 0 {
			if exp > MaxUnitDimension || exp < -MaxUnitDimension {
				return fmt.Errorf("%s to the power %d is beyond the bound of ±%d", dimNames[i], exp, MaxUnitDimension)
			}
			e += int(o.dim[i]) * exp
		}
		if e > MaxUnitDimension || e < -MaxUnitDimension {
			return fmt.Errorf("%s to the power %d is beyond the bound of ±%d", dimNames[i], e, MaxUnitDimension)
		}
		dim[i] = int8(e)
	}
	if exp > MaxUnitExponent || exp < -MaxUnitExponent {
		return fmt.Errorf("the power %d is beyond the bound of ±%d", exp, MaxUnitExponent)
	}
	switch {
	case exp == 1:
		v.factor.Mul(v.factor, o.factor)
	case exp == -1:
		v.factor.Quo(v.factor, o.factor)
	case exp != 0:
		// By squaring: big.Int.Exp on the numerator and the denominator.
		n := big.NewInt(int64(max(exp, -exp)))
		pow := new(big.Rat).SetFrac(new(big.Int).Exp(o.factor.Num(), n, nil), new(big.Int).Exp(o.factor.Denom(), n, nil))
		if exp > 0 {
			v.factor.Mul(v.factor, pow)
		} else {
			v.factor.Quo(v.factor, pow)
		}
	}
	v.pi += o.pi * exp
	v.dim = dim
	for k, n := range o.notes {
		v.notes[k] += n * exp
	}
	for k, n := range o.arbitrary {
		v.arbitrary[k] += n * exp
	}
	for k, n := range o.distinct {
		v.distinct[k] += n * exp
	}
	v.specials = append(v.specials, o.specials...)
	v.counted = v.counted || o.counted
	v.symbols += o.symbols
	return nil
}

// errZeroDivisor marks a term that divides by zero.
var errZeroDivisor = errors.New("a unit divides by zero")

// atom is a resolved atom: its definition and the value it denotes.
type atom struct {
	def atomDef
	val value
	// offset is an affine atom's zero in kelvin.
	offset *big.Rat
}

var (
	atomsOnce  sync.Once
	atomsByKey map[string]*atom
	prefixes   map[string]*big.Rat
	prefixList []prefixDef // longest code first, so `da` is tried before `d`
	atomsErr   error
)

// loadAtoms resolves every atom definition through the grammar, in an order
// the definitions themselves decide: an atom is resolved when its unit
// expression is.
func loadAtoms() {
	prefixes = map[string]*big.Rat{}
	for _, p := range prefixDefs {
		r, ok := new(big.Rat).SetString(p.value)
		if !ok {
			atomsErr = fmt.Errorf("prefix %s: bad value %q", p.code, p.value)
			return
		}
		prefixes[p.code] = r
		prefixList = append(prefixList, p)
	}
	sort.SliceStable(prefixList, func(i, j int) bool { return len(prefixList[i].code) > len(prefixList[j].code) })
	defs := map[string]atomDef{}
	for _, d := range atomDefs {
		if _, dup := defs[d.code]; dup {
			atomsErr = fmt.Errorf("atom %s declared twice", d.code)
			return
		}
		defs[d.code] = d
	}
	atomsByKey = map[string]*atom{}
	resolving := map[string]bool{}
	var resolve func(code string) (*atom, error)
	resolve = func(code string) (*atom, error) {
		if a, ok := atomsByKey[code]; ok {
			return a, nil
		}
		d, ok := defs[code]
		if !ok {
			return nil, fmt.Errorf("%w %q", errUnknownSymbol, code)
		}
		if resolving[code] {
			return nil, fmt.Errorf("atom %s is defined in terms of itself", code)
		}
		resolving[code] = true
		defer delete(resolving, code)
		a := &atom{def: d, val: unitValue()}
		a.val.symbols = 1
		switch {
		case d.class == atomPi:
			a.val.pi = 1
		case d.class == atomLevel:
			a.val.specials = []string{code}
		case d.class == atomArbitrary:
			a.val.arbitrary[code] = 1
		case d.base != nil:
			f, ok := new(big.Rat).SetString(d.value)
			if !ok {
				return nil, fmt.Errorf("atom %s: bad value %q", code, d.value)
			}
			a.val.factor = f
			for _, c := range d.base {
				a.val.dim[c.dim] = c.exp
			}
		default:
			f, ok := new(big.Rat).SetString(d.value)
			if !ok {
				return nil, fmt.Errorf("atom %s: bad value %q", code, d.value)
			}
			n, err := parseTerm(d.unit, resolve)
			if err != nil {
				return nil, fmt.Errorf("atom %s: %w", code, err)
			}
			v, err := evalNode(n)
			if err != nil {
				return nil, fmt.Errorf("atom %s: %w", code, err)
			}
			a.val.factor = new(big.Rat).Mul(f, v.factor)
			a.val.pi = v.pi
			a.val.dim = v.dim
			for k, x := range v.arbitrary {
				a.val.arbitrary[k] = x
			}
			for k, x := range v.distinct {
				a.val.distinct[k] = x
			}
		}
		if d.distinct {
			a.val.distinct[code] = 1
		}
		if d.class == atomAffine {
			off, ok := new(big.Rat).SetString(d.offset)
			if !ok {
				return nil, fmt.Errorf("atom %s: bad offset %q", code, d.offset)
			}
			a.offset = off
			a.val.specials = []string{code}
		}
		atomsByKey[code] = a
		return a, nil
	}
	for _, d := range atomDefs {
		if _, err := resolve(d.code); err != nil {
			atomsErr = err
			return
		}
	}
}

func atoms() (map[string]*atom, error) {
	atomsOnce.Do(loadAtoms)
	return atomsByKey, atomsErr
}

// lookupAtom finds a loaded atom; it is the resolver every parse after loading
// uses.
func lookupAtom(code string) (*atom, error) {
	all, err := atoms()
	if err != nil {
		return nil, err
	}
	if a, ok := all[code]; ok {
		return a, nil
	}
	return nil, fmt.Errorf("%w %q", errUnknownSymbol, code)
}

// --- the syntax tree ---

// node is one component of a term.
type node interface{ isNode() }

// unitNode is a simple unit: an atom under an optional prefix, raised to exp.
type unitNode struct {
	prefix string
	atom   *atom
	exp    int
	expSet bool
	expRaw string
	note   *string
}

// numberNode is a positive integer factor.
type numberNode struct {
	digits string
	note   *string
}

// noteNode is an annotation standing alone.
type noteNode struct{ text string }

// groupNode is a parenthesized term.
type groupNode struct{ inner *termNode }

// termNode is components joined by operators; ops[i] joins parts[i] and
// parts[i+1].
type termNode struct {
	reciprocal bool
	parts      []node
	ops        []byte
}

func (unitNode) isNode()   {}
func (numberNode) isNode() {}
func (noteNode) isNode()   {}
func (groupNode) isNode()  {}
func (termNode) isNode()   {}

// parser reads one code.
type parser struct {
	s       string
	i       int
	resolve func(string) (*atom, error)
	// depth is how many parentheses are open, and components how many
	// components the code has so far, each held to its bound as it grows.
	depth      int
	components int
}

// parseTerm parses a whole code into its tree.
func parseTerm(s string, resolve func(string) (*atom, error)) (*termNode, error) {
	if s == "" {
		return nil, fmt.Errorf("a unit is not empty: write 1 for a pure number")
	}
	for i := range len(s) {
		if s[i] < 33 || s[i] > 126 {
			return nil, fmt.Errorf("%q: a unit code is printable ASCII; %q at byte %d is not", s, s[i:i+1], i)
		}
	}
	p := &parser{s: s, resolve: resolve}
	t := &termNode{}
	if p.peek() == '/' {
		p.i++
		t.reciprocal = true
	}
	if err := p.term(t); err != nil {
		return nil, err
	}
	if p.i != len(s) {
		return nil, p.errf("unexpected %q", s[p.i:p.i+1])
	}
	return t, nil
}

func (p *parser) peek() byte {
	if p.i < len(p.s) {
		return p.s[p.i]
	}
	return 0
}

func (p *parser) errf(format string, args ...any) error {
	return fmt.Errorf("%q at %d: %s", p.s, p.i, fmt.Sprintf(format, args...))
}

func (p *parser) term(t *termNode) error {
	for {
		if p.components++; p.components > MaxUnitComponents {
			return p.errf("a code of more components is beyond the bound of %d", MaxUnitComponents)
		}
		n, err := p.component()
		if err != nil {
			return err
		}
		t.parts = append(t.parts, n)
		switch c := p.peek(); c {
		case '.', '/':
			p.i++
			t.ops = append(t.ops, c)
		default:
			return nil
		}
	}
}

func (p *parser) component() (node, error) {
	switch p.peek() {
	case 0:
		return nil, p.errf("a term is missing after an operator")
	case '(':
		if p.depth++; p.depth > MaxUnitNesting {
			return nil, p.errf("parentheses nested deeper are beyond the bound of %d", MaxUnitNesting)
		}
		p.i++
		inner := &termNode{}
		if err := p.term(inner); err != nil {
			return nil, err
		}
		if p.peek() != ')' {
			return nil, p.errf("an opening parenthesis is not closed")
		}
		p.i++
		p.depth--
		return groupNode{inner: inner}, nil
	case '{':
		text, err := p.annotation()
		if err != nil {
			return nil, err
		}
		return noteNode{text: text}, nil
	case ')', '}', '.', '/':
		return nil, p.errf("a term is missing")
	}
	start := p.i
	depth := 0
	for p.i < len(p.s) {
		c := p.s[p.i]
		if depth == 0 && (c == '.' || c == '/' || c == '(' || c == ')' || c == '{' || c == '}') {
			break
		}
		switch c {
		case '[':
			depth++
		case ']':
			depth--
			if depth < 0 {
				return nil, p.errf("a closing bracket has no opening one")
			}
		}
		p.i++
	}
	if depth != 0 {
		return nil, p.errf("an opening bracket is not closed")
	}
	symbol := p.s[start:p.i]
	var note *string
	if p.peek() == '{' {
		text, err := p.annotation()
		if err != nil {
			return nil, err
		}
		note = &text
	}
	if isDigits(symbol) {
		// UCUM's factor is a positive integer, and a zero one has no
		// reciprocal: `m/0` would divide by it.
		if strings.Trim(symbol, "0") == "" {
			return nil, p.errf("a factor is a positive integer, and %s is zero", symbol)
		}
		if len(symbol) > MaxUnitFactorDigits {
			return nil, p.errf("a factor of %d digits is beyond the bound of %d digits", len(symbol), MaxUnitFactorDigits)
		}
		return numberNode{digits: symbol, note: note}, nil
	}
	head, exp := splitExponent(symbol)
	u := unitNode{note: note, exp: 1}
	if exp != "" {
		// Digits past the bound's own are refused before Atoi reads them, so
		// one too long for an int names the bound as well.
		n, err := strconv.Atoi(exp)
		if len(strings.TrimLeft(exp, "+-")) > len(strconv.Itoa(MaxUnitExponent)) || err == nil && (n > MaxUnitExponent || n < -MaxUnitExponent) {
			return nil, p.errf("the exponent %s is beyond the bound of ±%d", exp, MaxUnitExponent)
		}
		if err != nil {
			return nil, p.errf("exponent %q is not an integer", exp)
		}
		u.exp, u.expSet, u.expRaw = n, true, exp
	}
	if err := p.simpleUnit(head, &u); err != nil {
		return nil, err
	}
	return u, nil
}

// simpleUnit resolves a symbol to an atom, or a prefix and a metric atom. The
// whole symbol is tried as an atom first, so `cd` is the candela and `Pa` the
// pascal, never a centi-day or a peta-year.
func (p *parser) simpleUnit(head string, u *unitNode) error {
	if head == "" {
		return p.errf("an exponent needs a unit before it")
	}
	a, err := p.resolve(head)
	if err == nil {
		u.atom = a
		return nil
	}
	if !errors.Is(err, errUnknownSymbol) {
		return err
	}
	for _, pre := range prefixList {
		rest, ok := strings.CutPrefix(head, pre.code)
		if !ok || rest == "" {
			continue
		}
		a, err := p.resolve(rest)
		if err != nil {
			if errors.Is(err, errUnknownSymbol) {
				continue
			}
			return err
		}
		if !a.def.metric {
			return fmt.Errorf("%q: %s takes no prefix (%s%s): UCUM marks it non-metric", p.s, rest, pre.code, rest)
		}
		u.prefix, u.atom = pre.code, a
		return nil
	}
	// No atom begins with a digit but 10* and 10^, so a symbol that does is a
	// number glued to a unit, whatever the table carries: `12h` is written
	// `12.h`.
	if head[0] >= '0' && head[0] <= '9' {
		return p.errf("a number is joined to a unit by a period: %q is not a symbol", head)
	}
	return fmt.Errorf("%q: %w %q", p.s, errUnknownSymbol, head)
}

func (p *parser) annotation() (string, error) {
	p.i++ // '{'
	start := p.i
	for p.i < len(p.s) && p.s[p.i] != '}' {
		if p.s[p.i] == '{' {
			return "", p.errf("annotations do not nest")
		}
		p.i++
	}
	if p.i == len(p.s) {
		return "", p.errf("an annotation is not closed")
	}
	text := p.s[start:p.i]
	p.i++ // '}'
	return text, nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// splitExponent separates a symbol's trailing signed integer: `m2` is m to the
// 2, `s-1` is s to the -1, `10*3` is 10* to the 3. Digits inside brackets
// (`m[H2O]`) are the atom's.
func splitExponent(symbol string) (head, exp string) {
	end := len(symbol)
	i := end
	for i > 0 && symbol[i-1] >= '0' && symbol[i-1] <= '9' {
		i--
	}
	if i == end {
		return symbol, ""
	}
	j := i
	if j > 0 && (symbol[j-1] == '+' || symbol[j-1] == '-') {
		j--
	}
	if j == 0 {
		return symbol, ""
	}
	return symbol[:j], symbol[j:]
}

// --- evaluation ---

func evalNode(n node) (value, error) {
	v := unitValue()
	var err error
	switch x := n.(type) {
	case *termNode:
		err = evalTerm(&v, x)
	case termNode:
		err = evalTerm(&v, &x)
	}
	return v, err
}

func evalTerm(v *value, t *termNode) error {
	inner := unitValue()
	for i, part := range t.parts {
		sign := 1
		if i > 0 && t.ops[i-1] == '/' {
			sign = -1
		}
		c, err := evalComponent(part)
		if err != nil {
			return err
		}
		if err := inner.mul(c, sign); err != nil {
			return err
		}
	}
	if t.reciprocal {
		return v.mul(inner, -1)
	}
	return v.mul(inner, 1)
}

func evalComponent(n node) (value, error) {
	v := unitValue()
	switch x := n.(type) {
	case unitNode:
		u := unitValue()
		if err := u.mul(x.atom.val, 1); err != nil {
			return value{}, err
		}
		if x.prefix != "" {
			u.factor.Mul(u.factor, prefixes[x.prefix])
		}
		if err := v.mul(u, x.exp); err != nil {
			return value{}, err
		}
		if x.note != nil {
			v.notes[*x.note]++
		}
	case numberNode:
		f, _ := new(big.Int).SetString(x.digits, 10)
		v.factor.SetInt(f)
		if x.note != nil {
			v.notes[*x.note]++
			v.counted = true
		}
	case noteNode:
		v.notes[x.text]++
		v.counted = true
	case groupNode:
		if err := evalTerm(&v, x.inner); err != nil {
			return value{}, err
		}
	}
	return v, nil
}

// --- rendering ---

// render writes the tree back as a code; with prefixes false every prefix is
// dropped, which is the key a prefixed code shares with its table row.
func render(t *termNode, prefixes bool) string {
	var b strings.Builder
	if t.reciprocal {
		b.WriteByte('/')
	}
	renderTerm(&b, t, prefixes)
	return b.String()
}

func renderTerm(b *strings.Builder, t *termNode, prefixes bool) {
	for i, part := range t.parts {
		if i > 0 {
			b.WriteByte(t.ops[i-1])
		}
		switch x := part.(type) {
		case unitNode:
			if prefixes {
				b.WriteString(x.prefix)
			}
			b.WriteString(x.atom.def.code)
			if x.expSet {
				b.WriteString(x.expRaw)
			}
			if x.note != nil {
				b.WriteString("{" + *x.note + "}")
			}
		case numberNode:
			b.WriteString(x.digits)
			if x.note != nil {
				b.WriteString("{" + *x.note + "}")
			}
		case noteNode:
			b.WriteString("{" + x.text + "}")
		case groupNode:
			b.WriteByte('(')
			renderTerm(b, x.inner, prefixes)
			b.WriteByte(')')
		}
	}
}

var superscripts = map[rune]rune{
	'0': '⁰', '1': '¹', '2': '²', '3': '³', '4': '⁴', '5': '⁵', '6': '⁶', '7': '⁷', '8': '⁸', '9': '⁹',
	'-': '⁻', '+': '⁺',
}

// renderPrint writes the tree with print symbols: `kg.m/s2` as `kg·m/s²`. An
// annotation that stands alone prints as its text, and one beside a unit is
// dropped, because it qualifies what the symbol already says.
func renderPrint(t *termNode) string {
	var b strings.Builder
	if t.reciprocal {
		b.WriteByte('/')
	}
	renderPrintTerm(&b, t)
	return b.String()
}

func renderPrintTerm(b *strings.Builder, t *termNode) {
	for i, part := range t.parts {
		if i > 0 {
			if t.ops[i-1] == '.' {
				b.WriteString("·")
			} else {
				b.WriteByte('/')
			}
		}
		switch x := part.(type) {
		case unitNode:
			for _, p := range prefixDefs {
				if p.code == x.prefix {
					b.WriteString(p.print)
				}
			}
			b.WriteString(x.atom.def.print)
			if x.expSet && x.expRaw != "1" {
				for _, r := range x.expRaw {
					b.WriteRune(superscripts[r])
				}
			}
		case numberNode:
			b.WriteString(x.digits)
			if x.note != nil {
				b.WriteString(" " + *x.note)
			}
		case noteNode:
			b.WriteString(x.text)
		case groupNode:
			b.WriteByte('(')
			renderPrintTerm(b, x.inner)
			b.WriteByte(')')
		}
	}
}
