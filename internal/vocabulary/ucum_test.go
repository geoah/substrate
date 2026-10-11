package vocabulary

// THE UCUM CONFORMANCE SUITE, run against the grammar.
//
// testdata/ucum/UcumFunctionalTests.xml is UCUM's own functional suite (its
// NOTICE says where it came from and under what license). Its validation cases
// hold the parser to the grammar: every valid code must parse and every invalid
// one must be refused. Its conversion cases hold the parser's factors to the
// published answers.
//
// The unit table is a subset of UCUM's atoms, so a case over an atom the table
// does not carry is SKIPPED AND COUNTED, never passed: the parser cannot tell
// a valid code over an atom it lacks from an invalid one, and a skip must not
// read as agreement. The counts are logged, and a floor fails the run when a
// change to the grammar or the table makes it skip more than it did.

import (
	"encoding/xml"
	"errors"
	"math/big"
	"os"
	"strings"
	"testing"
)

type ucumSuite struct {
	Validation struct {
		Cases []struct {
			ID     string `xml:"id,attr"`
			Unit   string `xml:"unit,attr"`
			Valid  string `xml:"valid,attr"`
			Reason string `xml:"reason,attr"`
		} `xml:"case"`
	} `xml:"validation"`
	Conversion struct {
		Cases []struct {
			ID      string `xml:"id,attr"`
			Value   string `xml:"value,attr"`
			Src     string `xml:"srcUnit,attr"`
			Dst     string `xml:"dstUnit,attr"`
			Outcome string `xml:"outcome,attr"`
		} `xml:"case"`
	} `xml:"conversion"`
}

// The floors: how many cases ran when the table last grew. A run below them
// means the grammar or the table lost ground.
const (
	ucumValidationFloor = 474
	ucumConversionFloor = 27
)

func TestUCUMFunctionalSuite(t *testing.T) {
	raw, err := os.ReadFile("testdata/ucum/UcumFunctionalTests.xml")
	if err != nil {
		t.Fatal(err)
	}
	var suite ucumSuite
	if err := xml.Unmarshal(raw, &suite); err != nil {
		t.Fatal(err)
	}
	if _, err := loadTable(); err != nil {
		t.Fatal(err)
	}

	var ran, skipped, valid, invalid int
	skippedAtoms := map[string]int{}
	for _, c := range suite.Validation.Cases {
		_, err := parseUnit(c.Unit)
		if errors.Is(err, errUnknownSymbol) {
			skipped++
			skippedAtoms[unknownSymbolIn(err)]++
			continue
		}
		ran++
		switch want := c.Valid == "true"; {
		case want && err != nil:
			t.Errorf("validation %s: %q is valid UCUM and the grammar refuses it: %v", c.ID, c.Unit, err)
		case !want && err == nil:
			t.Errorf("validation %s: %q is invalid UCUM (%s) and the grammar reads it", c.ID, c.Unit, c.Reason)
		case want:
			valid++
		default:
			invalid++
		}
	}
	t.Logf("UCUM validation: %d cases ran (%d valid, %d invalid), %d skipped over atoms the table does not carry",
		ran, valid, invalid, skipped)
	t.Logf("skipped symbols: %v", skippedAtoms)
	if ran < ucumValidationFloor {
		t.Errorf("only %d validation cases ran, below the floor of %d", ran, ucumValidationFloor)
	}

	var cran, cskipped int
	for _, c := range suite.Conversion.Cases {
		src, err1 := parseUnit(c.Src)
		dst, err2 := parseUnit(c.Dst)
		if errors.Is(err1, errUnknownSymbol) || errors.Is(err2, errUnknownSymbol) {
			cskipped++
			continue
		}
		if err1 != nil || err2 != nil {
			t.Errorf("conversion %s: %q to %q does not parse: %v %v", c.ID, c.Src, c.Dst, err1, err2)
			continue
		}
		cran++
		if src.val.dim != dst.val.dim {
			t.Errorf("conversion %s: %s (%s) and %s (%s) have different dimensions", c.ID, c.Src, src.val.dim, c.Dst, dst.val.dim)
			continue
		}
		v, ok := new(big.Rat).SetString(c.Value)
		if !ok {
			t.Fatalf("conversion %s: value %q", c.ID, c.Value)
		}
		got := new(big.Rat).Mul(v, new(big.Rat).Quo(src.val.factor, dst.val.factor))
		got.Mul(got, piPower(src.val.pi-dst.val.pi))
		if !withinLastDigit(got, c.Outcome) {
			t.Errorf("conversion %s: %s %s is %s %s, the suite says %s", c.ID, c.Value, c.Src, got.FloatString(30), c.Dst, c.Outcome)
		}
	}
	t.Logf("UCUM conversion: %d cases ran, %d skipped over atoms the table does not carry", cran, cskipped)
	if cran < ucumConversionFloor {
		t.Errorf("only %d conversion cases ran, below the floor of %d", cran, ucumConversionFloor)
	}
}

// unknownSymbolIn is the symbol an errUnknownSymbol names, for the log.
func unknownSymbolIn(err error) string {
	s := err.Error()
	i := strings.LastIndex(s, errUnknownSymbol.Error())
	if i < 0 {
		return s
	}
	return strings.TrimSpace(s[i+len(errUnknownSymbol.Error()):])
}

// piDigits is pi to 64 places, the precision UCUM's own definition carries.
const piDigits = "3.1415926535897932384626433832795028841971693993751058209749445923"

func piPower(n int) *big.Rat {
	pi, _ := new(big.Rat).SetString(piDigits)
	out := big.NewRat(1, 1)
	for ; n > 0; n-- {
		out.Mul(out, pi)
	}
	for ; n < 0; n++ {
		out.Quo(out, pi)
	}
	return out
}

// withinLastDigit holds an exact result to a published one the way the suite
// writes them: to the significant digits the outcome carries (6.30 [in_i] is
// "0.160" m), so the result lies within half a unit of the outcome's last
// digit.
func withinLastDigit(got *big.Rat, outcome string) bool {
	want, ok := new(big.Rat).SetString(outcome)
	if !ok {
		return false
	}
	mantissa, exp := outcome, 0
	if i := strings.IndexAny(outcome, "eE"); i >= 0 {
		mantissa = outcome[:i]
		e, ok := new(big.Int).SetString(strings.TrimPrefix(outcome[i+1:], "+"), 10)
		if !ok {
			return false
		}
		exp = int(e.Int64())
	}
	places := 0
	if _, frac, dotted := strings.Cut(mantissa, "."); dotted {
		places = len(frac)
	}
	// Half a unit of the last written digit: 0.5 x 10^(exp - places).
	half := big.NewRat(1, 2)
	ten := big.NewRat(10, 1)
	for range places - exp {
		half.Quo(half, ten)
	}
	for range exp - places {
		half.Mul(half, ten)
	}
	diff := new(big.Rat).Sub(got, want)
	return diff.Abs(diff).Cmp(half) <= 0
}
