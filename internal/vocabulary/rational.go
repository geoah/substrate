package vocabulary

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

// MaxRationalDigits bounds each part of a written rational, numerator and
// denominator apart: big.Rat reads a decimal in time quadratic in its length,
// and a conversion factor that needs more than 64 digits is a mistake.
const MaxRationalDigits = 64

// reRational is one exact rational as a conversion writes it: decimal digits,
// or a numerator over a denominator, each part digits with an optional
// fraction, the sign on the numerator alone. No exponent, for the reason
// CanonicalDecimalUnbounded gives.
var reRational = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?(/[0-9]+(\.[0-9]+)?)?$`)

// ParseRational reads an exact rational written as digits ("0.45359237") or
// as a fraction ("5/9", "1/0.3048"). A zero denominator, an exponent and a
// part longer than MaxRationalDigits digits are refused.
func ParseRational(s string) (*big.Rat, error) {
	if len(s) > 2*MaxRationalDigits+4 {
		return nil, fmt.Errorf("a rational holds at most %d digits in each part", MaxRationalDigits)
	}
	if !reRational.MatchString(s) {
		if reDecimalExponent.MatchString(s) {
			return nil, fmt.Errorf("%q: write a rational's digits out, without an exponent", s)
		}
		return nil, fmt.Errorf(`%q is not a rational: digits ("0.45359237") or a fraction ("5/9")`, s)
	}
	num, den, fraction := strings.Cut(s, "/")
	for _, part := range []string{num, den} {
		if n := len(strings.TrimLeft(strings.Replace(part, ".", "", 1), "-")); n > MaxRationalDigits {
			return nil, fmt.Errorf("a rational holds at most %d digits in each part, and %q has %d", MaxRationalDigits, part, n)
		}
	}
	r, ok := new(big.Rat).SetString(num)
	if !ok {
		return nil, fmt.Errorf("%q is not a rational", s)
	}
	if fraction {
		d, ok := new(big.Rat).SetString(den)
		if !ok {
			return nil, fmt.Errorf("%q is not a rational", s)
		}
		if d.Sign() == 0 {
			return nil, fmt.Errorf("%q divides by zero", s)
		}
		r.Quo(r, d)
	}
	return r, nil
}

// FormatRational writes an exact rational the way ParseRational reads it:
// decimal digits where the value has a finite decimal expansion ("0.45359237"),
// else a fraction in lowest terms ("5/9").
func FormatRational(r *big.Rat) string {
	if digits, exact := RatDecimal(r, ConvertedDigits); exact {
		return digits
	}
	return r.RatString()
}
