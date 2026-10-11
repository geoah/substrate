package vocabulary_test

// The unit grammar on hostile input: every code below is refused with an
// error, never a panic, through each door a unit enters by (ParseUnit, a
// declaration's `units:`, and the conversion the host call serves).

import (
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/vocabulary"
)

// refusesUnit holds one code to a refusal naming want, on every door.
func refusesUnit(t *testing.T, code, want string) {
	t.Helper()
	for _, problem := range unitRefusalProblems(code, want) {
		t.Error(problem)
	}
}

// unitRefusalProblems asks every door about one code and says where it was
// admitted, or refused for another reason than want. It does not touch a
// testing.T, so a goroutine under a deadline may run it.
func unitRefusalProblems(code, want string) []string {
	var problems []string
	check := func(door string, err error) {
		switch {
		case err == nil:
			problems = append(problems, door+" admitted "+code)
		case !strings.Contains(err.Error(), want):
			problems = append(problems, door+" refused "+code+" with "+err.Error()+", which does not name "+want)
		}
	}
	_, err := vocabulary.ParseUnit(code)
	check("ParseUnit", err)
	_, err = vocabulary.CanonicalUnit(code)
	check("CanonicalUnit", err)
	_, err = vocabulary.MeasureOf(code)
	check("MeasureOf", err)
	_, err = vocabulary.Convert(big.NewRat(1, 1), code, "m")
	check("Convert from", err)
	_, err = vocabulary.ConvertIn("length", big.NewRat(1, 1), "m", code)
	check("ConvertIn to", err)
	_, err = vocabulary.LoadFS(mkThing("  properties:\n    w: {type: quantity, measures: length, units: [\"" + code + "\"]}\n"))
	check("a declaration's units", err)
	return problems
}

// A factor of zero has no reciprocal: `m/0` divided by zero and panicked the
// write, the declaration loader and the host call alike.
func TestAZeroFactorIsRefused(t *testing.T) {
	for _, code := range []string{"m/0", "/0", "(m/0)", "m/(2.0)", "m/(0.m)", "m/00", "0", "0.m", "m.0"} {
		t.Run(code, func(t *testing.T) {
			refusesUnit(t, code, "a factor is a positive integer")
		})
	}
}

// A dimension is nine small exponents, each held within ±8 in every atom
// raised to its power and in every product along the way. `m257` once
// narrowed to int8 and read as metres; `m100.m100` wrapped to L-56.
func TestADimensionBeyondItsBoundIsRefused(t *testing.T) {
	for _, code := range []string{"m9", "m-9", "m24", "L3", "m8.m", "m5.m5", "/m5.m5", "(m4.m4).m"} {
		t.Run(code, func(t *testing.T) {
			refusesUnit(t, code, "beyond the bound of ±8")
		})
	}
	// An exponent past MaxUnitExponent is refused before its dimension is
	// computed at all.
	for _, code := range []string{"m257", "m100.m100"} {
		t.Run(code, func(t *testing.T) {
			refusesUnit(t, code, "beyond the bound of ±")
		})
	}
	for _, code := range []string{"m8", "m-8", "m5/m5", "m4.m4", "/m8", "kg/(cm5.m2)", "10*23"} {
		if _, err := vocabulary.ParseUnit(code); err != nil {
			t.Errorf("%s is inside the bound and was refused: %v", code, err)
		}
	}
}

// A short code must not buy a long evaluation: `10*1000000000` asked for a
// billion rational multiplications. The exponent, the factor's digits, the
// nesting and the component count are bounded before anything is evaluated,
// and each refusal names its bound. Every door must answer within the
// deadline, which the unbounded loop never met.
func TestUnitEvaluationIsBounded(t *testing.T) {
	components := strings.Repeat("2.", vocabulary.MaxUnitComponents) + "2"
	nested := strings.Repeat("(", vocabulary.MaxUnitNesting+1) + "m" + strings.Repeat(")", vocabulary.MaxUnitNesting+1)
	for _, c := range []struct{ code, want string }{
		{"10*1000000000", "beyond the bound of ±24"},
		{"[ppm]1000000000", "beyond the bound of ±24"},
		{"[pi]99999999999999999999", "beyond the bound of ±24"},
		{"10*25", "beyond the bound of ±24"},
		{"10*-25", "beyond the bound of ±24"},
		{"1234567890123.m", "beyond the bound of 12 digits"},
		{nested, "beyond the bound of 8"},
		{components, "beyond the bound of 32"},
	} {
		t.Run(c.code, func(t *testing.T) {
			done := make(chan []string, 1)
			go func() { done <- unitRefusalProblems(c.code, c.want) }()
			select {
			case problems := <-done:
				for _, p := range problems {
					t.Error(p)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("%q was still evaluating after 5 s", c.code)
			}
		})
	}
	fits := strings.Repeat("2.", vocabulary.MaxUnitComponents-1) + "m"
	deep := strings.Repeat("(", vocabulary.MaxUnitNesting) + "m" + strings.Repeat(")", vocabulary.MaxUnitNesting)
	for _, code := range []string{"10*24", "10*-24", "[ppm]24", "123456789012.m", fits, deep} {
		if _, err := vocabulary.ParseUnit(code); err != nil {
			t.Errorf("%s is inside every bound and was refused: %v", code, err)
		}
	}
}
