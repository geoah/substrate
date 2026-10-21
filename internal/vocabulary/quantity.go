package vocabulary

import (
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The quantity value shape lives with the datatype it belongs to
// (DatatypeQuantity): the engine's write coercion, its filters and ordering,
// the narrowing guard and the title renderer all read these names.
//
// A quantity value is `{value: "72.5", unit: "kg"}`, with `comparator: "<"`
// beside them where the property declares `comparator: true`. The value is
// exact digits, a string, as a decimal is (a JSON number rides float64 and may
// arrive rounded); the unit is the id of a unit record of the repository's
// unit graph (decision 0186), stored as the id whatever code or alias the
// writer sent; the comparator says the true
// value lies in the half-line it names, so a lab's `<0.5 ng/mL` is
// `{value: "0.5", unit: "ng/mL", comparator: "<"}`.
const (
	QuantityValue      = "value"
	QuantityUnit       = "unit"
	QuantityComparator = "comparator"
)

// QuantityComparators are the comparator's admitted values.
var QuantityComparators = []string{"<", "<=", ">=", ">"}

// QuantitySpec is what a quantity property declares beyond its datatype.
type QuantitySpec struct {
	// Units is the declared `units:`, a narrowing to listed unit spellings;
	// nil admits every unit of the anchor's linear component.
	Units []string
	// Comparator is the declared `comparator: true`: a value may carry one.
	Comparator bool
	// Min and Max are the declared bounds, each a quantity compared exactly
	// after converting the value into the bound's unit.
	Min, Max *QuantityBound
	// Unit is the declared `unit:` (decision 0186): a unit spelling every
	// value's unit must share a linear component with, resolved against the
	// repository's unit graph.
	Unit string
	// Delta is the declared `delta: true`: every value is a difference,
	// which converts by the factor alone.
	Delta bool
}

// QuantityBound is a declared `min:` or `max:` of a quantity property.
type QuantityBound struct {
	// Value is the bound's canonical digits.
	Value string
	// Unit is the bound's unit code.
	Unit string
}

// Rat is the bound's exact value.
func (b QuantityBound) Rat() *big.Rat {
	r, _ := new(big.Rat).SetString(b.Value)
	return r
}

// String renders the bound as declared: "40 kg".
func (b QuantityBound) String() string {
	return FormatQuantityIn(nil, map[string]any{QuantityValue: b.Value, QuantityUnit: b.Unit})
}

// FormatQuantityIn renders a stored quantity value the way FormatQuantity
// does, printing the symbol the repository's unit graph holds for the
// value's unit (decision 0186): "72.5 kg", "90°", "8000 steps". A unit the
// graph does not hold prints as stored.
func FormatQuantityIn(g *UnitGraph, v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	value, _ := m[QuantityValue].(string)
	unit, _ := m[QuantityUnit].(string)
	if value == "" || unit == "" {
		return ""
	}
	cmp, _ := m[QuantityComparator].(string)
	symbol := unit
	if g != nil {
		symbol = g.Symbol(unit)
	}
	switch symbol {
	case "":
		return cmp + value
	case "°", "′", "″", "'", "''":
		// An angle's symbol sits against the number: 90°, not 90 °.
		return cmp + value + symbol
	}
	return cmp + value + " " + symbol
}

// QuantitySearchText is a stored quantity value as the search index reads
// it under the unit graph (decision 0186): the digits and the stored unit
// id, never a symbol, so a symbol edit or a rebuild never moves a result.
func QuantitySearchText(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	value, _ := m[QuantityValue].(string)
	unit, _ := m[QuantityUnit].(string)
	if value == "" || unit == "" {
		return ""
	}
	cmp, _ := m[QuantityComparator].(string)
	return cmp + value + " " + unit
}

var (
	reDecimal         = regexp.MustCompile(`^[+-]?[0-9]+(\.[0-9]+)?$`)
	reDecimalExponent = regexp.MustCompile(`^[+-]?[0-9]*\.?[0-9]+[eE][+-]?[0-9]+$`)
)

// MaxDecimalDigits bounds the digits of a quantity value, its integer and
// fraction parts together. Every quantity filter, order and index casts the
// value to Postgres numeric, which holds at most 16383 fractional digits, and
// big.Rat reads a decimal in time quadratic in its length; no measurement
// needs more. The `decimal` datatype is NOT bounded: it stored any length
// before the quantity type existed, and a stored value the bound refused
// would fail its own round trip and drop on restore.
const MaxDecimalDigits = 1000

// CanonicalDecimal holds one authored quantity value to the decimal grammar
// (CanonicalDecimalUnbounded) and to MaxDecimalDigits. The length is checked
// before anything else reads the text.
func CanonicalDecimal(s string) (string, error) {
	tooLong := fmt.Errorf("a decimal holds at most %d digits", MaxDecimalDigits)
	if len(s) > MaxDecimalDigits+2 {
		return "", tooLong
	}
	out, err := CanonicalDecimalUnbounded(s)
	if err != nil {
		return "", err
	}
	if len(strings.TrimLeft(strings.Replace(out, ".", "", 1), "-")) > MaxDecimalDigits {
		return "", tooLong
	}
	return out, nil
}

// CanonicalDecimalUnbounded holds one authored decimal to the grammar (an
// optional sign, digits, an optional fraction, and nothing else) and answers
// the canonical spelling: no '+', no leading zeros, no negative zero. The
// SCALE is data: "19.90" stays "19.90". No exponent: the datatype is the
// exact digits, and "1.999e1" admits a second spelling for every value. It is
// the `decimal` datatype's spelling, at any length.
func CanonicalDecimalUnbounded(s string) (string, error) {
	if !reDecimal.MatchString(s) {
		if reDecimalExponent.MatchString(s) {
			return "", fmt.Errorf("a decimal writes its digits out, without an exponent")
		}
		return "", fmt.Errorf(`expected a decimal ("19.99"): an optional sign, digits, an optional fraction`)
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimLeft(s, "+-")
	intPart, frac, _ := strings.Cut(s, ".")
	intPart = strings.TrimLeft(intPart, "0")
	if intPart == "" {
		intPart = "0"
	}
	out := intPart
	if frac != "" {
		out += "." + frac
	}
	// "-0.00" is zero: the sign drops so equal values store one spelling.
	if neg && strings.Trim(out, "0.") != "" {
		out = "-" + out
	}
	return out, nil
}

// parseQuantitySpec reads a quantity property's own keys: `unit`, `units`,
// `delta`, `comparator`, and `min`/`max` as quantity values. inherited is a
// refinement's spec, which the property's own keys override. Every unit is a
// spelling of the repository's unit graph (decision 0186), held here to its
// shape alone.
func (l *loader) parseQuantitySpec(where string, d map[string]any, inherited *QuantitySpec) *QuantitySpec {
	s := &QuantitySpec{}
	if inherited != nil {
		*s = *inherited
		s.Units = slices.Clone(inherited.Units)
	}
	if raw, declared := d["unit"]; declared && raw != nil {
		unit, ok := raw.(string)
		if !ok {
			l.errf("%s.unit: %v is not a unit spelling", where, raw)
			return nil
		}
		if err := checkUnitSpelling(unit); err != nil {
			l.errf("%s.unit: %v", where, err)
			return nil
		}
		s.Unit = unit
	}
	if raw, declared := d["delta"]; declared && raw != nil {
		b, ok := raw.(bool)
		if !ok {
			l.errf("%s.delta: true or false", where)
			return nil
		}
		s.Delta = b
	}
	return l.parseGraphQuantitySpec(where, d, s)
}

// parseGraphQuantitySpec reads the rest of a declaration that names `unit`:
// its `units`, `comparator` and bounds, each unit a spelling held to its
// shape alone. Whether a spelling names a unit, and whether a bound compares
// with every admitted unit, is a repository's to say, so the write checks
// both against the repository's graph.
func (l *loader) parseGraphQuantitySpec(where string, d map[string]any, s *QuantitySpec) *QuantitySpec {
	if raw, declared := d["units"]; declared && raw != nil {
		list, ok := raw.([]any)
		if !ok || len(list) == 0 {
			l.errf("%s.units: a list of unit spellings, at least one", where)
			return nil
		}
		s.Units = nil
		for _, item := range list {
			unit, ok := item.(string)
			if !ok {
				l.errf("%s.units: %v is not a unit spelling", where, item)
				return nil
			}
			if err := checkUnitSpelling(unit); err != nil {
				l.errf("%s.units: %v", where, err)
				return nil
			}
			if slices.Contains(s.Units, unit) {
				l.errf("%s.units: %s is listed twice", where, unit)
				return nil
			}
			s.Units = append(s.Units, unit)
		}
	}
	if raw, declared := d["comparator"]; declared && raw != nil {
		b, ok := raw.(bool)
		if !ok {
			l.errf("%s.comparator: true or false", where)
			return nil
		}
		s.Comparator = b
	}
	for _, key := range []string{"min", "max"} {
		raw, declared := d[key]
		if !declared || raw == nil {
			continue
		}
		b, err := parseGraphQuantityBound(raw)
		if err != nil {
			l.errf("%s.%s: %v", where, key, err)
			return nil
		}
		if key == "min" {
			s.Min = b
		} else {
			s.Max = b
		}
	}
	if s.Min != nil && s.Max != nil && s.Min.Unit == s.Max.Unit && s.Max.Rat().Cmp(s.Min.Rat()) < 0 {
		l.errf("%s: max %s is below min %s", where, s.Max, s.Min)
		return nil
	}
	return s
}

// parseGraphQuantityBound reads one bound of a declaration that names
// `unit`: its digits and a unit spelling.
func parseGraphQuantityBound(raw any) (*QuantityBound, error) {
	m := asMapOrNil(raw)
	if m == nil {
		return nil, fmt.Errorf(`a quantity bound is a quantity value {value: "40", unit: kg}`)
	}
	for k := range m {
		if k != QuantityValue && k != QuantityUnit {
			return nil, fmt.Errorf("a bound holds %s and %s, and %q is neither", QuantityValue, QuantityUnit, k)
		}
	}
	digits, err := boundDigits(m[QuantityValue])
	if err != nil {
		return nil, err
	}
	unit, _ := m[QuantityUnit].(string)
	if unit == "" {
		return nil, fmt.Errorf("a bound names its %s", QuantityUnit)
	}
	if err := checkUnitSpelling(unit); err != nil {
		return nil, fmt.Errorf("%s: %w", QuantityUnit, err)
	}
	return &QuantityBound{Value: digits, Unit: unit}, nil
}

// boundDigits reads a bound's value, written as a string or a YAML number.
func boundDigits(raw any) (string, error) {
	var digits string
	switch v := raw.(type) {
	case string:
		digits = v
	case int:
		digits = strconv.Itoa(v)
	case int64:
		digits = strconv.FormatInt(v, 10)
	case float64:
		digits = strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return "", fmt.Errorf("%s is the bound's digits (\"40\")", QuantityValue)
	}
	c, err := CanonicalDecimal(digits)
	if err != nil {
		return "", fmt.Errorf("%s: %w", QuantityValue, err)
	}
	return c, nil
}

// orList joins names as a sentence reads them: "a, b or c".
func orList(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
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

// ConvertedDigits is how many significant digits RatDecimal keeps of a
// converted value that has no finite decimal expansion (1/3 of a kg in g is
// 333.33...).
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
