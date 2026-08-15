package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// The int contract: a safe integer, refused past 2^53-1 in magnitude on EVERY
// input shape, because both the REST decode and the jsonb read-back ride
// float64 and a bigger value corrupts silently on one of those trips. A
// json.Number is read by its own spelling, so 2^53+1 is refused rather than
// rounded into range.
func TestCoerceIntIsASafeInteger(t *testing.T) {
	p := &vocabulary.Property{Name: "n", Datatype: vocabulary.DatatypeInt}
	for _, tc := range []struct {
		name string
		in   any
		want int64
	}{
		{"a small float64", float64(12), 12},
		{"the largest safe integer", float64(maxSafeInt), maxSafeInt},
		{"the most negative safe integer", float64(-maxSafeInt), -maxSafeInt},
		{"a json.Number in range", json.Number("42"), 42},
	} {
		got, err := coerceScalar(p, tc.in)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Fatalf("%s: got %v, want %d", tc.name, got, tc.want)
		}
	}
	for _, tc := range []struct {
		name string
		in   any
		want string
	}{
		{"2^53 as a float64", float64(maxSafeInt + 1), "safe integer"},
		{"-2^53 as a float64", float64(-maxSafeInt - 1), "safe integer"},
		{"2^53 as an int64", int64(maxSafeInt + 1), "safe integer"},
		// The silent-corruption case the bound exists for: 2^53+1 decodes to
		// the float64 2^53, inside any naive bound, so the spelling is what
		// refuses it.
		{"2^53+1 as a json.Number", json.Number("9007199254740993"), "safe integer"},
		{"far past the bound", float64(1e300), "safe integer"},
		{"a fraction", float64(1.5), "expected an integer"},
		{"a string", "12", "expected a number"},
	} {
		if _, err := coerceScalar(p, tc.in); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: got %v, want it to name %q", tc.name, err, tc.want)
		}
	}
}

// The decimal contract: a string of exact digits, canonicalized but never
// rescaled, and a bare JSON number refused because it may already be rounded.
func TestCoerceDecimalIsExact(t *testing.T) {
	p := &vocabulary.Property{Name: "amount", Datatype: vocabulary.DatatypeDecimal}
	for in, want := range map[string]string{
		"19.99":                  "19.99",
		"19.90":                  "19.90", // the scale is data
		"+007.50":                "7.50",
		"0":                      "0",
		"-0.00":                  "0.00",
		"-12.05":                 "-12.05",
		"9007199254740993.00001": "9007199254740993.00001", // past any float64
	} {
		got, err := coerceScalar(p, in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got != want {
			t.Fatalf("%q: got %v, want %q", in, got, want)
		}
	}
	for _, tc := range []struct {
		name string
		in   any
		want string
	}{
		{"a bare JSON number", float64(19.99), "written as a string"},
		{"a json.Number", json.Number("19.99"), "written as a string"},
		{"an exponent", "1.9e2", "without an exponent"},
		{"a bare dot", ".5", "expected a decimal"},
		{"a trailing dot", "19.", "expected a decimal"},
		{"prose", "about twenty", "expected a decimal"},
		{"an empty string", "", "expected a decimal"},
	} {
		if _, err := coerceScalar(p, tc.in); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: got %v, want it to name %q", tc.name, err, tc.want)
		}
	}
	zero := 0.0
	bounded := &vocabulary.Property{Name: "amount", Datatype: vocabulary.DatatypeDecimal, Min: &zero}
	if _, err := coerceScalar(bounded, "-0.01"); err == nil || !strings.Contains(err.Error(), ">= 0") {
		t.Fatalf("min: got %v, want the bound named", err)
	}
	if got, err := coerceScalar(bounded, "0.00"); err != nil || got != "0.00" {
		t.Fatalf("min boundary: got %v, %v", got, err)
	}
}

// The duration contract: ISO 8601 is the ONE grammar, in and out. Years and
// months are refused (no fixed length), Go's own syntax is refused (a second
// grammar for the same word), and the stored form is a deterministic ISO
// decomposition, so every value has exactly one spelling.
func TestCoerceDurationIsISO8601Only(t *testing.T) {
	p := &vocabulary.Property{Name: "for", Datatype: vocabulary.DatatypeDuration}
	for in, want := range map[string]string{
		"PT47M12S": "PT47M12S",
		"PT1M":     "PT1M", // a time-part M is minutes
		"PT90M":    "PT1H30M",
		"PT36H":    "P1DT12H",
		"P1DT12H":  "P1DT12H",
		"P1D":      "P1D",
		"P2W":      "P14D",
		"P1W2DT3H": "P9DT3H",
		"PT0.5H":   "PT30M",
		"PT1.5S":   "PT1.5S",
		"-PT30M":   "-PT30M",
		"PT0S":     "PT0S",
	} {
		got, err := coerceScalar(p, in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got != want {
			t.Fatalf("%q: got %v, want %q", in, got, want)
		}
	}
	for _, tc := range []struct{ in, want string }{
		{"P1Y", "no fixed length"},
		{"P3M", "no fixed length"},
		{"P", "expected an ISO 8601 duration"},
		{"PT", "expected an ISO 8601 duration"},
		// Go's grammar is the retired spelling, refused so "duration" keeps
		// meaning one thing.
		{"47m12s", "expected an ISO 8601 duration"},
		{"3d", "expected an ISO 8601 duration"},
		{"soon", "expected an ISO 8601 duration"},
		{"P999999999999999999W", "overflows"},
	} {
		if _, err := coerceScalar(p, tc.in); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%q: got %v, want it to name %q", tc.in, err, tc.want)
		}
	}
}
