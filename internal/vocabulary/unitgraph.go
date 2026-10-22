package vocabulary

// THE UNIT GRAPH (decision 0186): a repository's unit and conversion records,
// read into one immutable value that resolves a spelling to a unit and
// converts between two units.
//
// Linear conversions (`to = from * factor + offset`, exact rationals) divide
// the units into components, and every unit holds an exact affine map to its
// component's root, the smallest id in byte order: two units of a component
// convert by composing two maps, with no all-pairs matrix. Expression
// conversions (CEL over one double) join components, at most once between
// any two, so a value has one route; they convert one value and never a sum,
// a comparison or a difference.
//
// Nothing here reads a database. The engine builds a graph per repository and
// graph revision, and refuses a transaction that leaves BuildUnitGraph naming
// a problem.

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/google/cel-go/cel"
)

// The two core kinds the graph is read from.
var (
	KindUnit       = CoreKind("unit")
	KindConversion = CoreKind("conversion")
)

// The graph's bounds. Each one is refused by name: a record past a record
// bound by CheckUnitRecord or CheckConversionRecord, a repository past a
// count or a composed map past its bits by BuildUnitGraph, and a route past
// its length by Convert.
const (
	// MaxUnits and MaxConversions bound one repository's graph, which every
	// read that converts builds once per revision.
	MaxUnits       = 2000
	MaxConversions = 4000
	// MaxUnitAliases bounds one unit's aliases.
	MaxUnitAliases = 32
	// MaxUnitSpelling bounds the bytes of a unit's code and of each alias, as
	// MaxIDLen bounds its id.
	MaxUnitSpelling = 128
	// MaxConversionExpr bounds the bytes of an expression and of its inverse.
	MaxConversionExpr = 1024
	// MaxExpressionRoute bounds the expression conversions one value crosses.
	MaxExpressionRoute = 4
	// MaxUnitMapBits bounds the numerator and the denominator of every
	// composed map to a root: a chain of exact factors grows them, and every
	// conversion inside the component multiplies them again.
	MaxUnitMapBits = 4096
)

// UnitScale says how a unit's values combine. The empty scale is a ratio
// scale, whose values add and average.
type UnitScale string

const (
	// UnitScaleLevel is logarithmic: its values do not add (dB, pH).
	UnitScaleLevel UnitScale = "level"
	// UnitScalePowerLevel is the level of a power: its values neither add
	// nor average (dB SPL, dBm).
	UnitScalePowerLevel UnitScale = "powerlevel"
)

// UnitRecord is one unit record, as CheckUnitRecord reads it.
type UnitRecord struct {
	// ID is the record id, the spelling a stored quantity carries.
	ID   string
	Name string
	// Symbol is what a title prints after the value; the id where the record
	// declares none, and empty for a unit that prints nothing (`1`).
	Symbol string
	// Code is the UCUM spelling, for interchange; a writer may send it.
	Code string
	// Aliases are other spellings a writer may send.
	Aliases []string
	Scale   UnitScale
	// Turn is one full circle in the unit, or nil.
	Turn *big.Rat
}

// ConversionRecord is one conversion record, as CheckConversionRecord reads
// it: Factor (and Offset) for a linear conversion, Expr and Inverse for an
// expression one.
type ConversionRecord struct {
	ID string
	// From and To are unit ids.
	From, To string
	// Factor is above zero; nil for an expression conversion. Offset is nil
	// where the record writes none.
	Factor, Offset *big.Rat
	Expr, Inverse  string
}

// Linear reports whether the conversion is a linear one.
func (c ConversionRecord) Linear() bool { return c.Factor != nil }

// UnitProblem is one reason a graph is refused, naming the record it is
// about; the ID is empty for a problem of the whole repository.
type UnitProblem struct {
	Kind    string
	ID      string
	Message string
}

func (p UnitProblem) Error() string {
	if p.ID == "" {
		return p.Kind + ": " + p.Message
	}
	return p.Kind + "/" + p.ID + ": " + p.Message
}

// --- the records --------------------------------------------------------------

// CheckUnitRecord reads one unit record's properties. It holds the shape the
// graph needs: `scale` is `level` or `powerlevel`, `turn` a rational above
// zero, `code` and each of at most MaxUnitAliases `aliases` a spelling of at
// most MaxUnitSpelling bytes. Properties it does not read are left to the
// kind's declaration.
func CheckUnitRecord(id string, props map[string]any) (UnitRecord, error) {
	u := UnitRecord{ID: id, Symbol: id}
	if !ValidID(id) {
		return u, fmt.Errorf("%q is not a record id", id)
	}
	var err error
	if u.Name, _, err = textProperty(props, "name"); err != nil {
		return u, err
	}
	if symbol, set, err := textProperty(props, "symbol"); err != nil {
		return u, err
	} else if set {
		u.Symbol = symbol
	}
	code, set, err := textProperty(props, "code")
	if err != nil {
		return u, err
	}
	if set {
		if err := checkUnitSpelling(code); err != nil {
			return u, fmt.Errorf("code: %w", err)
		}
		u.Code = code
	}
	if raw, ok := props["aliases"]; ok && raw != nil {
		list, ok := stringList(raw)
		if !ok {
			return u, fmt.Errorf("aliases: a list of spellings")
		}
		if len(list) > MaxUnitAliases {
			return u, fmt.Errorf("aliases: a unit holds at most %d aliases, and this one lists %d", MaxUnitAliases, len(list))
		}
		for i, alias := range list {
			if err := checkUnitSpelling(alias); err != nil {
				return u, fmt.Errorf("aliases[%d]: %w", i, err)
			}
		}
		u.Aliases = list
	}
	scale, _, err := textProperty(props, "scale")
	if err != nil {
		return u, err
	}
	switch UnitScale(scale) {
	case "", UnitScaleLevel, UnitScalePowerLevel:
		u.Scale = UnitScale(scale)
	default:
		return u, fmt.Errorf("scale: %q is not a scale: write %s or %s, or leave it out for a ratio", scale, UnitScaleLevel, UnitScalePowerLevel)
	}
	if turn, set, err := rationalProperty(props, "turn"); err != nil {
		return u, err
	} else if set {
		if turn.Sign() <= 0 {
			return u, fmt.Errorf("turn: one full circle is above zero")
		}
		u.Turn = turn
	}
	return u, nil
}

// CheckConversionRecord reads one conversion record's properties: `from` and
// `to`, references to two different unit records, and exactly one form,
// `factor` above zero with an optional `offset`, or `expr` and `inverse`
// together, each at most MaxConversionExpr bytes of CEL over one double
// `value` typed to return a double.
func CheckConversionRecord(id string, props map[string]any) (ConversionRecord, error) {
	c := ConversionRecord{ID: id}
	if !ValidID(id) {
		return c, fmt.Errorf("%q is not a record id", id)
	}
	var err error
	if c.From, err = unitReference(props, "from"); err != nil {
		return c, err
	}
	if c.To, err = unitReference(props, "to"); err != nil {
		return c, err
	}
	if c.From == c.To {
		return c, fmt.Errorf("to: a conversion joins two units, and this one names %s twice", c.From)
	}
	// A null property is an absent one, as every property reader here reads
	// it, so the form is chosen by what the record writes.
	written := func(key string) bool { return props[key] != nil }
	hasFactor, hasOffset := written("factor"), written("offset")
	hasExpr, hasInverse := written("expr"), written("inverse")
	switch {
	case hasFactor && (hasExpr || hasInverse):
		return c, fmt.Errorf("a conversion is linear (factor, offset) or an expression (expr, inverse), never both")
	case hasOffset && !hasFactor:
		return c, fmt.Errorf("offset: an offset belongs to a linear conversion, which writes a factor")
	case hasExpr != hasInverse:
		return c, fmt.Errorf("an expression conversion writes expr and inverse together, so it converts both ways")
	case !hasFactor && !hasExpr:
		return c, fmt.Errorf("a conversion writes a factor (and an optional offset), or expr and inverse")
	}
	if hasFactor {
		factor, set, err := rationalProperty(props, "factor")
		if err != nil {
			return c, err
		}
		if !set {
			return c, fmt.Errorf("factor: a linear conversion writes a factor")
		}
		if factor.Sign() <= 0 {
			return c, fmt.Errorf("factor: a factor is above zero, so every linear conversion is increasing")
		}
		c.Factor = factor
		if offset, set, err := rationalProperty(props, "offset"); err != nil {
			return c, err
		} else if set {
			c.Offset = offset
		}
		return c, nil
	}
	for _, key := range []string{"expr", "inverse"} {
		src, _, err := textProperty(props, key)
		if err != nil {
			return c, err
		}
		if _, err := CompileConversionExpr(src); err != nil {
			return c, fmt.Errorf("%s: %w", key, err)
		}
		if key == "expr" {
			c.Expr = src
		} else {
			c.Inverse = src
		}
	}
	return c, nil
}

func textProperty(props map[string]any, key string) (string, bool, error) {
	raw, ok := props[key]
	if !ok || raw == nil {
		return "", false, nil
	}
	s, ok := raw.(string)
	if !ok {
		return "", false, fmt.Errorf("%s: a string", key)
	}
	return s, true, nil
}

// rationalProperty reads a rational written as a string. A number is refused
// rather than read, because a float has already lost the digits a factor
// such as 0.45359237 needs.
func rationalProperty(props map[string]any, key string) (*big.Rat, bool, error) {
	raw, ok := props[key]
	if !ok || raw == nil {
		return nil, false, nil
	}
	s, ok := raw.(string)
	if !ok {
		return nil, false, fmt.Errorf(`%s: write it as a string of digits or a fraction ("0.45359237", "5/9"), so no digit is lost`, key)
	}
	r, err := ParseRational(s)
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", key, err)
	}
	return r, true, nil
}

func stringList(raw any) ([]string, bool) {
	switch t := raw.(type) {
	case []string:
		return slices.Clone(t), true
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	}
	return nil, false
}

// unitReference reads a reference to a unit record: the stored object, the
// full path, or a bare id.
func unitReference(props map[string]any, key string) (string, error) {
	raw, ok := props[key]
	if !ok || raw == nil {
		return "", fmt.Errorf("%s: a conversion names the unit it converts %s", key, key)
	}
	path := referentPath(raw)
	if path == "" {
		return "", fmt.Errorf("%s: a reference to a %s record", key, KindUnit)
	}
	id := path
	if kind, rest, ok := SplitRecordPath(path); ok {
		if kind != KindUnit {
			return "", fmt.Errorf("%s: %s is a %s record, not a %s", key, path, kind, KindUnit)
		}
		id = rest
	}
	if !ValidID(id) {
		return "", fmt.Errorf("%s: %q is not a unit id", key, id)
	}
	return id, nil
}

func checkUnitSpelling(s string) error {
	switch {
	case s == "":
		return fmt.Errorf("a spelling is not empty")
	case len(s) > MaxUnitSpelling:
		return fmt.Errorf("a spelling holds at most %d bytes", MaxUnitSpelling)
	case !utf8.ValidString(s):
		return fmt.Errorf("%q is not UTF-8", s)
	case strings.TrimSpace(s) != s:
		return fmt.Errorf("%q starts or ends with a space, which a writer's spelling is trimmed of", s)
	}
	return nil
}

// --- the expression dialect ---------------------------------------------------

var (
	conversionEnvOnce sync.Once
	conversionEnv     *cel.Env
	conversionEnvErr  error
)

// CompileConversionExpr compiles one conversion expression: standard CEL over
// one double, `value`, returning a double (`235.214583 / value`). It runs
// under the bounds a trigger's guard runs under.
func CompileConversionExpr(src string) (cel.Program, error) {
	if len(src) > MaxConversionExpr {
		return nil, fmt.Errorf("an expression holds at most %d bytes", MaxConversionExpr)
	}
	if strings.TrimSpace(src) == "" {
		return nil, fmt.Errorf("an expression is not empty")
	}
	conversionEnvOnce.Do(func() {
		conversionEnv, conversionEnvErr = cel.NewEnv(cel.Variable("value", cel.DoubleType))
	})
	if conversionEnvErr != nil {
		return nil, conversionEnvErr
	}
	ast, iss := conversionEnv.Compile(src)
	if iss != nil && iss.Err() != nil {
		return nil, iss.Err()
	}
	if out := ast.OutputType(); !out.IsExactType(cel.DoubleType) {
		return nil, fmt.Errorf("must return a double, returns %s (write 2.0, not 2)", out)
	}
	return conversionEnv.Program(ast,
		cel.InterruptCheckFrequency(celInterruptFrequency),
		cel.CostLimit(celCostLimit),
	)
}

// --- the graph ----------------------------------------------------------------

// UnitGraph is one repository's units and conversions at one revision. It is
// immutable once built and safe for concurrent use.
type UnitGraph struct {
	units       map[string]*graphUnit
	ids         []string
	conversions []ConversionRecord
	// spellings resolve a spelling to an id, tier by tier: ids, then codes,
	// then aliases, each holding the spellings as written and normalized.
	spellings  [3]map[string]string
	components []*unitComponent
	// expressions are the expression conversions the forest admitted, and
	// forest their edges by component.
	expressions []*expressionEdge
	forest      map[int][]forestStep

	memoMu sync.Mutex
	memo   map[string]any
}

type graphUnit struct {
	rec  UnitRecord
	comp int
	// root = value * factor + offset.
	factor, offset *big.Rat
}

type unitComponent struct {
	root    string
	members []string
	scale   UnitScale
	// turn is one full circle in the root unit, or nil.
	turn *big.Rat
}

type expressionEdge struct {
	conv ConversionRecord
	once sync.Once
	// expr and inverse are compiled at the first conversion that crosses the
	// edge, so a read that converts nothing compiles nothing.
	expr, inverse cel.Program
	err           error
}

type forestStep struct {
	edge    int
	other   int
	forward bool
}

// MaxUnitGraphMemo bounds the entries Memo keeps.
const MaxUnitGraphMemo = 4096

// BuildUnitGraph reads units and conversions into a graph, and names every
// problem that refuses them: a repository past MaxUnits or MaxConversions,
// two units answering one spelling, a conversion naming a unit that does not
// exist, a cycle of linear conversions that does not compose to the
// identity, two units of different scales in one component, a composed map
// past MaxUnitMapBits, a circular component (one holding a turn) with an
// offset or with turns that disagree, and two expression routes between the
// same components. The graph is built from what passes and answers whatever
// it can, so a caller decides whether a problem refuses its write.
func BuildUnitGraph(units []UnitRecord, conversions []ConversionRecord) (*UnitGraph, []UnitProblem) {
	g := &UnitGraph{
		units:  make(map[string]*graphUnit, len(units)),
		forest: map[int][]forestStep{},
		memo:   map[string]any{},
	}
	for i := range g.spellings {
		g.spellings[i] = map[string]string{}
	}
	var problems []UnitProblem
	unitProblem := func(id, format string, args ...any) {
		problems = append(problems, UnitProblem{Kind: KindUnit, ID: id, Message: fmt.Sprintf(format, args...)})
	}
	convProblem := func(id, format string, args ...any) {
		problems = append(problems, UnitProblem{Kind: KindConversion, ID: id, Message: fmt.Sprintf(format, args...)})
	}
	if len(units) > MaxUnits {
		unitProblem("", "a repository holds at most %d units, and this one holds %d", MaxUnits, len(units))
	}
	if len(conversions) > MaxConversions {
		convProblem("", "a repository holds at most %d conversions, and this one holds %d", MaxConversions, len(conversions))
	}

	sorted := slices.Clone(units)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	for _, u := range sorted {
		if _, dup := g.units[u.ID]; dup {
			unitProblem(u.ID, "the id names two units")
			continue
		}
		u.Aliases = slices.Clone(u.Aliases)
		if u.Turn != nil {
			u.Turn = new(big.Rat).Set(u.Turn)
		}
		g.units[u.ID] = &graphUnit{rec: u, comp: -1}
		g.ids = append(g.ids, u.ID)
	}
	problems = append(problems, g.indexSpellings()...)

	// The coefficients are copied, not shared: a caller mutating its records
	// after the build would otherwise change what Conversions reports while
	// the maps built from them stay put.
	g.conversions = slices.Clone(conversions)
	for i := range g.conversions {
		g.conversions[i] = copyConversionRecord(g.conversions[i])
	}
	sort.SliceStable(g.conversions, func(i, j int) bool { return g.conversions[i].ID < g.conversions[j].ID })
	var linear, expression []int
	seen := map[string]bool{}
	for i, c := range g.conversions {
		switch {
		case seen[c.ID]:
			convProblem(c.ID, "the id names two conversions")
			continue
		case g.units[c.From] == nil:
			convProblem(c.ID, "from names %s, which is no unit of this repository", c.From)
			continue
		case g.units[c.To] == nil:
			convProblem(c.ID, "to names %s, which is no unit of this repository", c.To)
			continue
		case c.From == c.To:
			convProblem(c.ID, "a conversion joins two units, and this one names %s twice", c.From)
			continue
		}
		seen[c.ID] = true
		if c.Linear() {
			linear = append(linear, i)
		} else {
			expression = append(expression, i)
		}
	}
	problems = append(problems, g.buildComponents(linear)...)
	problems = append(problems, g.checkCircular(linear)...)
	problems = append(problems, g.buildForest(expression)...)
	return g, problems
}

// indexSpellings fills the spelling tiers and names every spelling two units
// answer, the normalized forms included.
func (g *UnitGraph) indexSpellings() []UnitProblem {
	var problems []UnitProblem
	owner := map[string]string{}
	for _, id := range g.ids {
		u := g.units[id].rec
		tiers := [3][]string{{u.ID}, nil, u.Aliases}
		if u.Code != "" {
			tiers[1] = []string{u.Code}
		}
		for tier, spellings := range tiers {
			for _, s := range spellings {
				for _, key := range unitSpellingKeys(s) {
					if prev, taken := owner[key]; !taken {
						owner[key] = id
					} else if prev != id {
						how := fmt.Sprintf("%q", s)
						if key != s {
							how = fmt.Sprintf("%q, read as %q,", s, key)
						}
						problems = append(problems, UnitProblem{
							Kind: KindUnit, ID: id,
							Message: fmt.Sprintf("the spelling %s already names unit %s: one spelling names one unit", how, prev),
						})
					}
					if _, set := g.spellings[tier][key]; !set {
						g.spellings[tier][key] = id
					}
				}
			}
		}
	}
	return problems
}

// unitSpellingKeys are the keys one spelling is found under: as written, and
// normalized where that differs.
func unitSpellingKeys(s string) []string {
	if n := normalizeUnitCharacters(s); n != s {
		return []string{s, n}
	}
	return []string{s}
}

// buildComponents walks the linear conversions from each unit in id order, so
// the first unit a walk starts from is its component's root, and gives each
// unit its exact map to the root. A conversion that reaches a unit already
// mapped closes a cycle, and is checked against the map it would give.
func (g *UnitGraph) buildComponents(linear []int) []UnitProblem {
	var problems []UnitProblem
	type adj struct {
		conv    int
		forward bool
	}
	edges := map[string][]adj{}
	for _, i := range linear {
		c := g.conversions[i]
		edges[c.From] = append(edges[c.From], adj{i, true})
		edges[c.To] = append(edges[c.To], adj{i, false})
	}
	done := map[int]bool{}
	parent := map[string]int{}
	for _, rootID := range g.ids {
		root := g.units[rootID]
		if root.comp >= 0 {
			continue
		}
		comp := &unitComponent{root: rootID, scale: root.rec.Scale}
		idx := len(g.components)
		g.components = append(g.components, comp)
		root.comp, root.factor, root.offset = idx, big.NewRat(1, 1), new(big.Rat)
		queue := []string{rootID}
		for len(queue) > 0 {
			x := g.units[queue[0]]
			queue = queue[1:]
			comp.members = append(comp.members, x.rec.ID)
			for _, e := range edges[x.rec.ID] {
				if done[e.conv] {
					continue
				}
				done[e.conv] = true
				c := g.conversions[e.conv]
				offset := c.Offset
				if offset == nil {
					offset = new(big.Rat)
				}
				// to = from * f + o. Walking forward, x is from and
				// root = ((y - o) / f) * a + b; walking back, x is to and
				// root = (y * f + o) * a + b.
				var yID string
				factor, off := new(big.Rat), new(big.Rat)
				if e.forward {
					yID = c.To
					factor.Quo(x.factor, c.Factor)
					off.Mul(offset, factor)
					off.Sub(x.offset, off)
				} else {
					yID = c.From
					factor.Mul(c.Factor, x.factor)
					off.Mul(offset, x.factor)
					off.Add(off, x.offset)
				}
				y := g.units[yID]
				if y.comp >= 0 {
					if y.factor.Cmp(factor) != 0 || y.offset.Cmp(off) != 0 {
						problems = append(problems, UnitProblem{
							Kind: KindConversion, ID: c.ID,
							Message: g.cycleMessage(c, parent),
						})
					}
					continue
				}
				if y.rec.Scale != comp.scale {
					problems = append(problems, UnitProblem{
						Kind: KindConversion, ID: c.ID,
						Message: fmt.Sprintf("joins %s (%s) and %s (%s): the units a linear conversion joins share one scale",
							c.From, scaleName(g.units[c.From].rec.Scale), c.To, scaleName(g.units[c.To].rec.Scale)),
					})
					continue
				}
				if !ratWithin(factor, MaxUnitMapBits) || !ratWithin(off, MaxUnitMapBits) {
					problems = append(problems, UnitProblem{
						Kind: KindConversion, ID: c.ID,
						Message: fmt.Sprintf("composes a map from %s to %s past %d bits: a composed factor or offset holds at most %d bits in its numerator and its denominator",
							yID, rootID, MaxUnitMapBits, MaxUnitMapBits),
					})
					continue
				}
				y.comp, y.factor, y.offset = idx, factor, off
				parent[yID] = e.conv
				queue = append(queue, yID)
			}
		}
		sort.Strings(comp.members)
	}
	return problems
}

func scaleName(s UnitScale) string {
	if s == "" {
		return "a ratio"
	}
	return "a " + string(s)
}

// cycleMessage says what a conversion closing an inconsistent cycle states
// against what the conversions already walked state, and names them.
func (g *UnitGraph) cycleMessage(c ConversionRecord, parent map[string]int) string {
	from, to := g.units[c.From], g.units[c.To]
	f, o := composeLinear(from, to, false)
	offset := c.Offset
	if offset == nil {
		offset = new(big.Rat)
	}
	return fmt.Sprintf("closes a cycle that does not agree: this conversion says %s, and %s say %s; every route between two units gives one answer",
		affineText(c.From, c.To, c.Factor, offset), strings.Join(g.treePath(c.From, c.To, parent), ", "), affineText(c.From, c.To, f, o))
}

// treePath names the conversions the walk joined two units of one component
// by.
func (g *UnitGraph) treePath(a, b string, parent map[string]int) []string {
	up := func(id string) []string {
		chain := []string{id}
		for {
			i, ok := parent[id]
			if !ok {
				return chain
			}
			c := g.conversions[i]
			if c.From == id {
				id = c.To
			} else {
				id = c.From
			}
			chain = append(chain, id)
		}
	}
	ca, cb := up(a), up(b)
	depth := map[string]int{}
	for i, id := range cb {
		depth[id] = i
	}
	var path []string
	for _, id := range ca {
		if j, ok := depth[id]; ok {
			for _, below := range cb[:j] {
				path = append(path, g.conversions[parent[below]].ID)
			}
			break
		}
		path = append(path, g.conversions[parent[id]].ID)
	}
	sort.Strings(path)
	return path
}

func affineText(from, to string, factor, offset *big.Rat) string {
	s := fmt.Sprintf("%s = %s * %s", to, from, FormatRational(factor))
	switch offset.Sign() {
	case 1:
		s += " + " + FormatRational(offset)
	case -1:
		s += " - " + FormatRational(new(big.Rat).Neg(offset))
	}
	return s
}

func ratWithin(r *big.Rat, bits int) bool {
	return r.Num().BitLen() <= bits && r.Denom().BitLen() <= bits
}

// checkCircular holds a component holding a turn to one direction in every
// unit: no conversion inside it carries an offset, and every turn it declares
// is the same circle.
func (g *UnitGraph) checkCircular(linear []int) []UnitProblem {
	var problems []UnitProblem
	for _, comp := range g.components {
		var first string
		for _, id := range comp.members {
			u := g.units[id]
			if u.rec.Turn == nil {
				continue
			}
			turn := new(big.Rat).Mul(u.rec.Turn, u.factor)
			if comp.turn == nil {
				comp.turn, first = turn, id
				continue
			}
			if turn.Cmp(comp.turn) != 0 {
				want := new(big.Rat).Quo(comp.turn, u.factor)
				problems = append(problems, UnitProblem{
					Kind: KindUnit, ID: id,
					Message: fmt.Sprintf("turn %s disagrees with %s's: through the conversions, one turn of %s is %s %s",
						FormatRational(u.rec.Turn), first, first, FormatRational(want), id),
				})
			}
		}
	}
	for _, i := range linear {
		c := g.conversions[i]
		from, to := g.units[c.From], g.units[c.To]
		if from.comp < 0 || from.comp != to.comp {
			continue
		}
		comp := g.components[from.comp]
		if comp.turn != nil && c.Offset != nil && c.Offset.Sign() != 0 {
			problems = append(problems, UnitProblem{
				Kind: KindConversion, ID: c.ID,
				Message: fmt.Sprintf("carries an offset inside a circular component (%s declares a turn): a direction reads one way in every unit of it, so its conversions have no offset",
					comp.root),
			})
		}
	}
	return problems
}

// buildForest admits each expression conversion that joins two components no
// earlier one already joins, directly or through others, so every two
// components have at most one route.
func (g *UnitGraph) buildForest(expression []int) []UnitProblem {
	var problems []UnitProblem
	set := make([]int, len(g.components))
	for i := range set {
		set[i] = i
	}
	find := func(i int) int {
		for set[i] != i {
			set[i] = set[set[i]]
			i = set[i]
		}
		return i
	}
	for _, i := range expression {
		c := g.conversions[i]
		cf, ct := g.units[c.From].comp, g.units[c.To].comp
		if cf < 0 || ct < 0 {
			continue
		}
		if cf == ct {
			problems = append(problems, UnitProblem{
				Kind: KindConversion, ID: c.ID,
				Message: fmt.Sprintf("joins %s and %s, which linear conversions already join: an expression conversion joins two units no linear route does", c.From, c.To),
			})
			continue
		}
		if find(cf) == find(ct) {
			problems = append(problems, UnitProblem{
				Kind: KindConversion, ID: c.ID,
				Message: fmt.Sprintf("is a second route between %s and %s: expression conversions join two components at most once, so a value has one route", c.From, c.To),
			})
			continue
		}
		set[find(cf)] = find(ct)
		edge := len(g.expressions)
		g.expressions = append(g.expressions, &expressionEdge{conv: c})
		g.forest[cf] = append(g.forest[cf], forestStep{edge: edge, other: ct, forward: true})
		g.forest[ct] = append(g.forest[ct], forestStep{edge: edge, other: cf, forward: false})
	}
	return problems
}

// --- reading the graph --------------------------------------------------------

// Resolve finds the unit a writer's spelling names, trimmed: as an id, then a
// code, then an alias, then each again with the spelling normalized (both
// micro signs as `u`, `²` and `³` as digits). It answers the unit's id.
func (g *UnitGraph) Resolve(spelling string) (string, bool) {
	s := strings.TrimSpace(spelling)
	for _, key := range unitSpellingKeys(s) {
		for _, tier := range g.spellings {
			if id, ok := tier[key]; ok {
				return id, true
			}
		}
	}
	return "", false
}

// Unit is one unit's record.
func (g *UnitGraph) Unit(id string) (UnitRecord, bool) {
	u, ok := g.units[id]
	if !ok {
		return UnitRecord{}, false
	}
	return copyUnitRecord(u.rec), true
}

// Units lists every unit, in id order.
func (g *UnitGraph) Units() []UnitRecord {
	out := make([]UnitRecord, 0, len(g.ids))
	for _, id := range g.ids {
		out = append(out, copyUnitRecord(g.units[id].rec))
	}
	return out
}

// Conversions lists every conversion the graph was built from, in id order.
func (g *UnitGraph) Conversions() []ConversionRecord {
	out := make([]ConversionRecord, len(g.conversions))
	for i, c := range g.conversions {
		out[i] = copyConversionRecord(c)
	}
	return out
}

func copyConversionRecord(c ConversionRecord) ConversionRecord {
	if c.Factor != nil {
		c.Factor = new(big.Rat).Set(c.Factor)
	}
	if c.Offset != nil {
		c.Offset = new(big.Rat).Set(c.Offset)
	}
	return c
}

func copyUnitRecord(u UnitRecord) UnitRecord {
	u.Aliases = slices.Clone(u.Aliases)
	if u.Turn != nil {
		u.Turn = new(big.Rat).Set(u.Turn)
	}
	return u
}

// Root is the root of a unit's linear component, the smallest id in it.
func (g *UnitGraph) Root(id string) (string, bool) {
	u, ok := g.units[id]
	if !ok {
		return "", false
	}
	return g.components[u.comp].root, true
}

// ToRoot is a unit's exact map to its root: root = value * factor + offset.
func (g *UnitGraph) ToRoot(id string) (factor, offset *big.Rat, ok bool) {
	u, ok := g.units[id]
	if !ok {
		return nil, nil, false
	}
	return new(big.Rat).Set(u.factor), new(big.Rat).Set(u.offset), true
}

// Component lists the units of a unit's linear component, in id order.
func (g *UnitGraph) Component(id string) []string {
	u, ok := g.units[id]
	if !ok {
		return nil
	}
	return slices.Clone(g.components[u.comp].members)
}

// Symbol is what a title prints after a value in the unit; the id itself
// for a unit the graph does not hold.
func (g *UnitGraph) Symbol(id string) string {
	if u, ok := g.units[id]; ok {
		return u.rec.Symbol
	}
	return id
}

// Scale is the unit's scale, which every unit of its component shares.
func (g *UnitGraph) Scale(id string) UnitScale {
	if u, ok := g.units[id]; ok {
		return u.rec.Scale
	}
	return ""
}

// Turn is one full circle in the unit: the turn it declares, else the one its
// circular component's turn converts to, else nil.
func (g *UnitGraph) Turn(id string) *big.Rat {
	u, ok := g.units[id]
	if !ok {
		return nil
	}
	if u.rec.Turn != nil {
		return new(big.Rat).Set(u.rec.Turn)
	}
	if turn := g.components[u.comp].turn; turn != nil {
		return new(big.Rat).Quo(turn, u.factor)
	}
	return nil
}

// Memo answers the value stored under key, building and storing it on the
// first call. A reader keeps what it derives from one graph here (a filter's
// SQL arms for a component), so the cache lives and dies with the graph. An
// error is answered and never stored, and past MaxUnitGraphMemo entries a
// value is built and not stored.
func (g *UnitGraph) Memo(key string, build func() (any, error)) (any, error) {
	g.memoMu.Lock()
	v, ok := g.memo[key]
	g.memoMu.Unlock()
	if ok {
		return v, nil
	}
	v, err := build()
	if err != nil {
		return nil, err
	}
	g.memoMu.Lock()
	defer g.memoMu.Unlock()
	if prev, ok := g.memo[key]; ok {
		return prev, nil
	}
	if len(g.memo) < MaxUnitGraphMemo {
		g.memo[key] = v
	}
	return v, nil
}

// Linear is the exact map between two units of one linear component:
// to = from * factor + offset. A difference (delta) keeps the factor and
// drops every offset. Two units no linear route joins are refused, and so is
// an id that names no unit.
func (g *UnitGraph) Linear(from, to string, delta bool) (factor, offset *big.Rat, err error) {
	fu, ok := g.units[from]
	if !ok {
		return nil, nil, fmt.Errorf("%s is no unit of this repository", from)
	}
	tu, ok := g.units[to]
	if !ok {
		return nil, nil, fmt.Errorf("%s is no unit of this repository", to)
	}
	if fu.comp != tu.comp {
		return nil, nil, g.apart(fu, tu)
	}
	factor, offset = composeLinear(fu, tu, delta)
	return factor, offset, nil
}

func (g *UnitGraph) apart(fu, tu *graphUnit) error {
	if _, err := g.expressionRoute(fu.comp, tu.comp); err == nil {
		return fmt.Errorf("%s and %s convert only through an expression conversion, which converts one value and never a sum, a comparison or a difference",
			fu.rec.ID, tu.rec.ID)
	}
	return fmt.Errorf("%s and %s do not convert: no conversion joins them", fu.rec.ID, tu.rec.ID)
}

// composeLinear maps from to to through their shared root: with
// root = from * a1 + b1 and root = to * a2 + b2,
// to = from * (a1 / a2) + (b1 - b2) / a2.
func composeLinear(fu, tu *graphUnit, delta bool) (factor, offset *big.Rat) {
	factor = new(big.Rat).Quo(fu.factor, tu.factor)
	offset = new(big.Rat)
	if !delta {
		offset.Sub(fu.offset, tu.offset)
		offset.Quo(offset, tu.factor)
	}
	return factor, offset
}

// UnitConversion is one value Convert converted.
type UnitConversion struct {
	// Value is the converted digits: every one where an exact answer has a
	// finite decimal expansion, else ConvertedDigits significant digits, and
	// at most MaxDecimalDigits (fitDecimal) in either case.
	Value string
	// Unit is the id converted to.
	Unit string
	// Exact is false where Value was rounded, and for every answer an
	// expression conversion gave.
	Exact bool
}

// Convert converts a value written as exact digits between two units, each
// spelled any way Resolve reads. Inside one linear component the answer is
// exact (and rounded only where it repeats); across components it takes the
// one route of expression conversions, evaluated in float64, each step's
// result finite, and is never exact. A difference (delta) never crosses an
// expression conversion. A refusal names the input it refuses: value, from,
// to, or the conversion.
func (g *UnitGraph) Convert(ctx context.Context, value, from, to string, delta bool) (UnitConversion, error) {
	digits, err := CanonicalDecimal(value)
	if err != nil {
		return UnitConversion{}, fmt.Errorf("value: %w", err)
	}
	fromID, ok := g.Resolve(from)
	if !ok {
		return UnitConversion{}, fmt.Errorf("from: %q names no unit of this repository", from)
	}
	toID, ok := g.Resolve(to)
	if !ok {
		return UnitConversion{}, fmt.Errorf("to: %q names no unit of this repository", to)
	}
	v, _ := new(big.Rat).SetString(digits)
	fu, tu := g.units[fromID], g.units[toID]
	if fu.comp == tu.comp {
		factor, offset := composeLinear(fu, tu, delta)
		r := v.Mul(v, factor)
		r.Add(r, offset)
		out, exact := RatDecimal(r, ConvertedDigits)
		if _, err := CanonicalDecimal(out); err != nil {
			if out, err = fitDecimal(r); err != nil {
				return UnitConversion{}, err
			}
			exact = false
		}
		return UnitConversion{Value: out, Unit: toID, Exact: exact}, nil
	}
	if delta {
		return UnitConversion{}, fmt.Errorf("%s and %s: a difference converts through linear conversions alone, and no linear route joins them", fromID, toID)
	}
	route, err := g.expressionRoute(fu.comp, tu.comp)
	if err != nil {
		return UnitConversion{}, fmt.Errorf("%s and %s do not convert: %w", fromID, toID, err)
	}
	// The value stays exact between expressions: each linear stretch maps it
	// as a rational and only then reads it as a float, because a composed
	// factor such as 10^-360 is past float64 where its product with the
	// value is not.
	r, cur := v, fu
	var x float64
	for _, step := range route {
		if err := ctx.Err(); err != nil {
			return UnitConversion{}, err
		}
		edge := g.expressions[step.edge]
		src, dst := edge.conv.From, edge.conv.To
		if !step.forward {
			src, dst = dst, src
		}
		if x, err = floatLinear(cur, g.units[src], r, digits); err != nil {
			return UnitConversion{}, err
		}
		if x, err = edge.eval(ctx, x, step.forward); err != nil {
			return UnitConversion{}, err
		}
		r, cur = new(big.Rat).SetFloat64(x), g.units[dst]
	}
	if x, err = floatLinear(cur, tu, r, digits); err != nil {
		return UnitConversion{}, err
	}
	out, err := CanonicalDecimal(strconv.FormatFloat(x, 'f', -1, 64))
	if err != nil {
		r := new(big.Rat)
		r.SetFloat64(x)
		if out, err = fitDecimal(r); err != nil {
			return UnitConversion{}, err
		}
	}
	return UnitConversion{Value: out, Unit: toID, Exact: false}, nil
}

// floatLinear converts an exact value inside one component exactly, and
// answers the result as the float an expression reads. A result past
// float64, or a nonzero one that would read as zero, is refused; input is
// the value's digits, named where no conversion moved it.
func floatLinear(fu, tu *graphUnit, r *big.Rat, input string) (float64, error) {
	if fu != tu {
		factor, offset := composeLinear(fu, tu, false)
		r = new(big.Rat).Mul(r, factor)
		r.Add(r, offset)
	}
	x, _ := r.Float64()
	if math.IsInf(x, 0) || (x == 0 && r.Sign() != 0) {
		if fu == tu {
			return 0, fmt.Errorf("value: %s is past the range a float converts in", input)
		}
		return 0, fmt.Errorf("%s to %s: the value converts past the range of a float", fu.rec.ID, tu.rec.ID)
	}
	return x, nil
}

func (e *expressionEdge) eval(ctx context.Context, x float64, forward bool) (float64, error) {
	e.once.Do(func() {
		if e.expr, e.err = CompileConversionExpr(e.conv.Expr); e.err != nil {
			e.err = fmt.Errorf("expr: %w", e.err)
			return
		}
		if e.inverse, e.err = CompileConversionExpr(e.conv.Inverse); e.err != nil {
			e.err = fmt.Errorf("inverse: %w", e.err)
		}
	})
	if e.err != nil {
		return 0, fmt.Errorf("conversion %s: %w", e.conv.ID, e.err)
	}
	prg, which := e.expr, "expr"
	if !forward {
		prg, which = e.inverse, "inverse"
	}
	out, _, err := prg.ContextEval(ctx, map[string]any{"value": x})
	if err != nil {
		return 0, fmt.Errorf("conversion %s: %s: %w", e.conv.ID, which, err)
	}
	y, ok := out.Value().(float64)
	if !ok {
		return 0, fmt.Errorf("conversion %s: %s returned %v, not a double", e.conv.ID, which, out.Value())
	}
	if math.IsInf(y, 0) || math.IsNaN(y) {
		return 0, fmt.Errorf("conversion %s: %s returned %v for %v: a conversion answers a finite number", e.conv.ID, which, y, x)
	}
	return y, nil
}

// expressionRoute is the one route of expression conversions between two
// components.
func (g *UnitGraph) expressionRoute(from, to int) ([]forestStep, error) {
	type hop struct {
		prev int
		step forestStep
	}
	came := map[int]hop{from: {prev: -1}}
	queue := []int{from}
	for len(queue) > 0 && !hasKey(came, to) {
		c := queue[0]
		queue = queue[1:]
		for _, s := range g.forest[c] {
			if _, seen := came[s.other]; seen {
				continue
			}
			came[s.other] = hop{prev: c, step: s}
			queue = append(queue, s.other)
		}
	}
	if !hasKey(came, to) {
		return nil, fmt.Errorf("no conversion joins them")
	}
	var route []forestStep
	for c := to; c != from; c = came[c].prev {
		route = append(route, came[c].step)
	}
	if len(route) > MaxExpressionRoute {
		return nil, fmt.Errorf("the route crosses %d expression conversions, and a value crosses at most %d", len(route), MaxExpressionRoute)
	}
	slices.Reverse(route)
	return route, nil
}

func hasKey[K comparable, V any](m map[K]V, k K) bool {
	_, ok := m[k]
	return ok
}

// --- spellings ----------------------------------------------------------------

// unitCharacters are the characters a spelling is normalized by: both micro
// signs, which look alike and are two code points, and the superscript
// digits.
var unitCharacters = map[rune]string{
	'µ': "u", // MICRO SIGN
	'μ': "u", // GREEK SMALL LETTER MU
	'²': "2",
	'³': "3",
}

// normalizeUnitCharacters rewrites unitCharacters. A superscript run right
// after a component that is the bare number 10 is that number's power, so it
// takes UCUM's `10*`: 10³/μL is 10*3/uL, where the plain digits would read
// as 103/uL.
func normalizeUnitCharacters(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		to, ok := unitCharacters[r]
		if !ok {
			b.WriteRune(r)
			continue
		}
		if superscriptDigit(r) && tenEnds(runes[:i]) {
			b.WriteString("*")
		}
		b.WriteString(to)
	}
	return b.String()
}

func superscriptDigit(r rune) bool { return r == '²' || r == '³' }

// tenEnds reports whether the text before a superscript run ends in a
// component that is the bare number 10: the start of the code, or an
// operator or an opening parenthesis, then 10.
func tenEnds(before []rune) bool {
	n := len(before)
	if n < 2 || before[n-2] != '1' || before[n-1] != '0' {
		return false
	}
	return n == 2 || strings.ContainsRune("./(", before[n-3])
}
