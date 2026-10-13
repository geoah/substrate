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
// arrive rounded); the unit is a code the unit table reads (units.go), stored
// as the code whatever alias the writer sent; the comparator says the true
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
	// Measure is the declared `measures:`: every stored unit belongs to it.
	// Empty admits every unit that names its own measure (MeasureOf).
	Measure string
	// Units is the declared `units:`, a narrowing to listed codes; nil admits
	// every unit of the measure.
	Units []string
	// Comparator is the declared `comparator: true`: a value may carry one.
	Comparator bool
	// Min and Max are the declared bounds, each a quantity compared exactly
	// after converting the value into the bound's unit.
	Min, Max *QuantityBound
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

// String renders the bound as a title would: "40 kg".
func (b QuantityBound) String() string {
	return FormatQuantity(map[string]any{QuantityValue: b.Value, QuantityUnit: b.Unit})
}

// CheckUnit holds a stored or written unit code to the declaration: its
// measure, and the listed units.
func (s *QuantitySpec) CheckUnit(code string) (Measure, error) {
	measure := ""
	if s != nil {
		measure = s.Measure
	}
	m, err := ResolveMeasure(code, measure)
	if err != nil {
		return Measure{}, err
	}
	if s != nil && len(s.Units) > 0 && !slices.Contains(s.Units, code) {
		return Measure{}, fmt.Errorf("%s is not one of the declared units %s", code, strings.Join(s.Units, ", "))
	}
	return m, nil
}

// CheckBounds holds a value to the declared bounds, compared exactly after
// converting the value into each bound's unit. A comparator does not move the
// check: the stated number is what a bound reads.
func (s *QuantitySpec) CheckBounds(v *big.Rat, unit string) error {
	if s == nil {
		return nil
	}
	if s.Min != nil {
		in, err := ConvertIn(s.Measure, v, unit, s.Min.Unit)
		if err != nil {
			return fmt.Errorf("cannot compare with min %s: %w", s.Min, err)
		}
		if in.Cmp(s.Min.Rat()) < 0 {
			return fmt.Errorf("must be >= %s", s.Min)
		}
	}
	if s.Max != nil {
		in, err := ConvertIn(s.Measure, v, unit, s.Max.Unit)
		if err != nil {
			return fmt.Errorf("cannot compare with max %s: %w", s.Max, err)
		}
		if in.Cmp(s.Max.Rat()) > 0 {
			return fmt.Errorf("must be <= %s", s.Max)
		}
	}
	return nil
}

// FormatQuantity renders a STORED quantity value the way a title shows it: the
// comparator, the digits as stored and the unit's print symbol, "72.5 kg",
// "37 °C", "<0.5 ng/mL", "8000 steps", "90°". A value that is not a quantity
// shape renders "", which is how every other unrenderable value reads in a
// title.
func FormatQuantity(v any) string {
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
	symbol := UnitSymbol(unit)
	switch {
	case symbol == "":
		return cmp + value
	case unit == "deg" || unit == "'" || unit == "''":
		// An angle's symbol sits against the number: 90°, not 90 °.
		return cmp + value + symbol
	}
	return cmp + value + " " + symbol
}

var (
	reDecimal         = regexp.MustCompile(`^[+-]?[0-9]+(\.[0-9]+)?$`)
	reDecimalExponent = regexp.MustCompile(`^[+-]?[0-9]*\.?[0-9]+[eE][+-]?[0-9]+$`)
)

// CanonicalDecimal holds one authored decimal to the grammar (an optional
// sign, digits, an optional fraction, and nothing else) and answers the
// canonical spelling: no '+', no leading zeros, no negative zero. The SCALE is
// data: "19.90" stays "19.90". No exponent: the datatype is the exact digits,
// and "1.999e1" admits a second spelling for every value.
func CanonicalDecimal(s string) (string, error) {
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

// MaxQuantityDigits bounds the digits of a quantity value, its integer and
// fraction parts together. Every filter, order and index casts the value to
// Postgres numeric, which holds at most 16383 fractional digits, and big.Rat
// reads a decimal in time quadratic in its length; no measurement needs more.
const MaxQuantityDigits = 1000

// QuantityDigits is CanonicalDecimal held to MaxQuantityDigits: the digits a
// quantity value stores, a filter's operand compares and a unit conversion
// reads. The length is checked before anything else reads the text.
func QuantityDigits(s string) (string, error) {
	tooLong := fmt.Errorf("a quantity value holds at most %d digits", MaxQuantityDigits)
	if len(s) > MaxQuantityDigits+2 {
		return "", tooLong
	}
	c, err := CanonicalDecimal(s)
	if err != nil {
		return "", err
	}
	if len(strings.Trim(strings.ReplaceAll(c, ".", ""), "-")) > MaxQuantityDigits {
		return "", tooLong
	}
	return c, nil
}

// parseQuantitySpec reads a quantity property's own keys: `measures`,
// `units`, `comparator`, and `min`/`max` as quantity values. inherited is a
// refinement's spec, which the property's own keys override.
func (l *loader) parseQuantitySpec(where string, d map[string]any, inherited *QuantitySpec) *QuantitySpec {
	s := &QuantitySpec{}
	if inherited != nil {
		*s = *inherited
		s.Units = slices.Clone(inherited.Units)
	}
	if raw, declared := d["measures"]; declared && raw != nil {
		name, ok := raw.(string)
		if _, known := LookupMeasure(name); !ok || !known {
			l.errf("%s.measures: %v is not a measure; the unit table names %s", where, raw, measureNames())
			return nil
		}
		s.Measure = name
	}
	if raw, declared := d["units"]; declared && raw != nil {
		list, ok := raw.([]any)
		if !ok || len(list) == 0 {
			l.errf("%s.units: a list of unit codes, at least one", where)
			return nil
		}
		s.Units = nil
		for _, item := range list {
			code, ok := item.(string)
			if !ok {
				l.errf("%s.units: %v is not a unit code", where, item)
				return nil
			}
			canonical, err := CanonicalUnit(code)
			switch {
			case err != nil:
				l.errf("%s.units: %v", where, err)
				return nil
			case canonical != code:
				l.errf("%s.units: write the code %q, not %q: a declaration names units as they are stored", where, canonical, code)
				return nil
			case slices.Contains(s.Units, code):
				l.errf("%s.units: %s is listed twice", where, code)
				return nil
			}
			if _, err := ResolveMeasure(code, s.Measure); err != nil {
				l.errf("%s.units: %v", where, err)
				return nil
			}
			s.Units = append(s.Units, code)
		}
	}
	// A narrowed list that the measure no longer holds (a refinement's units
	// under a use site's other measure) is refused like a listed one.
	for _, code := range s.Units {
		if _, err := ResolveMeasure(code, s.Measure); err != nil {
			l.errf("%s.units: %v", where, err)
			return nil
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
		b, err := parseQuantityBound(raw, s)
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
	if s.Min != nil && s.Max != nil {
		hi, err := ConvertIn(s.Measure, s.Max.Rat(), s.Max.Unit, s.Min.Unit)
		switch {
		case err != nil:
			l.errf("%s: max %s does not compare with min %s: %v", where, s.Max, s.Min, err)
			return nil
		case hi.Cmp(s.Min.Rat()) < 0:
			l.errf("%s: max %s is below min %s", where, s.Max, s.Min)
			return nil
		}
	}
	return s
}

// parseQuantityBound reads one declared bound: a quantity value whose unit the
// declaration admits. A bound needs `measures:`, because without one a stored
// value of another measure has nothing to be compared with, and every code
// the declaration lists (its `units:`, else its measure's codes) must convert
// into the bound's unit, or the bound would refuse every value in that code:
// a rate's {count}/min never reaches /min, a count's annotations never reach
// one another, and rad reaches deg only through pi.
func parseQuantityBound(raw any, s *QuantitySpec) (*QuantityBound, error) {
	m := asMapOrNil(raw)
	if m == nil {
		return nil, fmt.Errorf(`a quantity bound is a quantity value {value: "40", unit: kg}`)
	}
	if s.Measure == "" {
		return nil, fmt.Errorf("a quantity bound needs `measures:` on the property, so every value it compares converts")
	}
	for k := range m {
		if k != QuantityValue && k != QuantityUnit {
			return nil, fmt.Errorf("a bound holds %s and %s, and %q is neither", QuantityValue, QuantityUnit, k)
		}
	}
	var digits string
	switch v := m[QuantityValue].(type) {
	case string:
		digits = v
	case int:
		digits = strconv.Itoa(v)
	case int64:
		digits = strconv.FormatInt(v, 10)
	case float64:
		digits = strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return nil, fmt.Errorf("%s is the bound's digits (\"40\")", QuantityValue)
	}
	c, err := QuantityDigits(digits)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", QuantityValue, err)
	}
	unit, _ := m[QuantityUnit].(string)
	if unit == "" {
		return nil, fmt.Errorf("a bound names its %s", QuantityUnit)
	}
	canonical, err := CanonicalUnit(unit)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", QuantityUnit, err)
	}
	if canonical != unit {
		return nil, fmt.Errorf("%s: write the code %q, not %q", QuantityUnit, canonical, unit)
	}
	if _, err := ResolveMeasure(unit, s.Measure); err != nil {
		return nil, fmt.Errorf("%s: %w", QuantityUnit, err)
	}
	m2, _ := LookupMeasure(s.Measure)
	listed := s.Units
	if len(listed) == 0 {
		if m2.Scale == ScaleArbitrary {
			// An assay measure admits every arbitrary unit, listed or not.
			return nil, fmt.Errorf("an arbitrary measure converts nothing, so a bound needs `units:` listing its one unit, %s", unit)
		}
		listed = m2.Units
	}
	one := big.NewRat(1, 1)
	for _, code := range listed {
		if _, err := ConvertIn(s.Measure, one, code, unit); err != nil {
			if len(s.Units) > 0 {
				return nil, fmt.Errorf("%s: the listed unit %s does not convert to it: %w", QuantityUnit, code, err)
			}
			return nil, fmt.Errorf("%s does not convert to %s, so a bound needs `units:` listing the codes that do: %w", code, unit, err)
		}
	}
	return &QuantityBound{Value: c, Unit: unit}, nil
}

func measureNames() string {
	var names []string
	for _, m := range Measures() {
		names = append(names, m.Name)
	}
	return strings.Join(names, ", ")
}
