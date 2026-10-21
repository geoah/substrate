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
// decimal digits where the value has a finite decimal expansion that fits
// MaxRationalDigits ("0.45359237"), else a fraction in lowest terms ("5/9"),
// else a fraction with one part written as decimal digits ("1/0.3048"), so
// every value ParseRational reads is written back as text it reads. A value
// no form fits (a composed map past the record bound) is written in lowest
// terms.
func FormatRational(r *big.Rat) string {
	if digits, exact := RatDecimal(r, ConvertedDigits); exact && readsAs(digits, r) {
		return digits
	}
	if s := r.RatString(); readsAs(s, r) {
		return s
	}
	if s, ok := scaledRational(r); ok {
		return s
	}
	return r.RatString()
}

// scaledRational writes r as a fraction with a decimal part: an integer over
// a decimal with e places is r / 10^e in lowest terms, and a decimal with e
// places over an integer is r * 10^e. A rational ParseRational read as
// a/10^m over b/10^n is one of them, with e = |n - m| and parts no longer
// than the ones it read, so the smallest e that fits is found within
// MaxRationalDigits.
func scaledRational(r *big.Rat) (string, bool) {
	sign := ""
	if r.Sign() < 0 {
		sign = "-"
	}
	abs := new(big.Rat).Abs(r)
	scale := new(big.Int)
	for e := 1; e <= MaxRationalDigits; e++ {
		scale.Exp(big.NewInt(10), big.NewInt(int64(e)), nil)
		down := new(big.Rat).SetFrac(abs.Num(), new(big.Int).Mul(abs.Denom(), scale))
		if s := sign + down.Num().String() + "/" + placePoint(down.Denom().String(), e); readsAs(s, r) {
			return s, true
		}
		up := new(big.Rat).SetFrac(new(big.Int).Mul(abs.Num(), scale), abs.Denom())
		if s := sign + placePoint(up.Num().String(), e) + "/" + up.Denom().String(); readsAs(s, r) {
			return s, true
		}
	}
	return "", false
}

func readsAs(s string, r *big.Rat) bool {
	got, err := ParseRational(s)
	return err == nil && got.Cmp(r) == 0
}
