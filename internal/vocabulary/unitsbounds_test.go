package vocabulary_test

// The unit grammar on hostile input: every code below is refused with an
// error, never a panic, through each door a unit enters by (ParseUnit, a
// declaration's `units:`, and the conversion the host call serves).

import (
	"math/big"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// refusesUnit holds one code to a refusal naming want, on every door.
func refusesUnit(t *testing.T, code, want string) {
	t.Helper()
	check := func(door string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s admitted %q", door, code)
		}
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("%s refused %q with %q, which does not name %q", door, code, err, want)
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
	loadThingErr(t, "  properties:\n    w: {type: quantity, measures: length, units: [\""+code+"\"]}\n", want)
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
	for _, code := range []string{"m257", "m9", "m-9", "L3", "m8.m", "m5.m5", "m100.m100", "/m5.m5", "(m4.m4).m"} {
		t.Run(code, func(t *testing.T) {
			refusesUnit(t, code, "beyond the bound of ±8")
		})
	}
	for _, code := range []string{"m8", "m-8", "m5/m5", "m4.m4", "/m8", "kg/(cm5.m2)", "10*23"} {
		if _, err := vocabulary.ParseUnit(code); err != nil {
			t.Errorf("%s is inside the bound and was refused: %v", code, err)
		}
	}
}
