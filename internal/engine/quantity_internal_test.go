package engine

import (
	"encoding/json"
	"math/big"
	"reflect"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

func quantityValue(value, unit string, cmp ...string) map[string]any {
	m := map[string]any{"value": value, "unit": unit}
	if len(cmp) > 0 {
		m["comparator"] = cmp[0]
	}
	return m
}

func quantityProperty(spec *vocabulary.QuantitySpec) *vocabulary.Property {
	return &vocabulary.Property{Name: "weight", Datatype: vocabulary.DatatypeQuantity, Quantity: spec}
}

// Coercion holds a quantity to its shape alone: the write transaction
// resolves the unit and holds it to the declaration (quantitywrite.go).
func TestCoerceQuantity(t *testing.T) {
	lab := &vocabulary.QuantitySpec{Unit: "ng/mL", Comparator: true}
	for _, tc := range []struct {
		name string
		spec *vocabulary.QuantitySpec
		in   map[string]any
		want map[string]any
	}{
		{"digits as written", nil, quantityValue("72.50", "kg"), quantityValue("72.50", "kg")},
		{"a canonical spelling", nil, quantityValue("+072.5", "kg"), quantityValue("72.5", "kg")},
		{"a spelling kept for the write to resolve", nil, quantityValue("62", "bpm"), quantityValue("62", "bpm")},
		{"a comparator where declared", lab, quantityValue("0.5", "ng/mL", "<"), quantityValue("0.5", "ng/mL", "<")},
		{"a null comparator", lab, map[string]any{"value": "1", "unit": "mg/dL", "comparator": nil}, quantityValue("1", "mg/dL")},
	} {
		got, err := coerceScalar(quantityProperty(tc.spec), tc.in)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: got %#v, want %#v", tc.name, got, tc.want)
		}
	}
	for _, tc := range []struct {
		name string
		spec *vocabulary.QuantitySpec
		in   any
		want string
	}{
		{"a bare number", nil, 72.5, "is an object"},
		{"a text", nil, "72.5 kg", "is an object"},
		{"a JSON number value", nil, map[string]any{"value": 72.5, "unit": "kg"}, "written as a string"},
		{"a json.Number value", nil, map[string]any{"value": json.Number("72.5"), "unit": "kg"}, "written as a string"},
		{"an exponent", nil, quantityValue("7.25e1", "kg"), "without an exponent"},
		{"no value", nil, map[string]any{"unit": "kg"}, "needs value"},
		{"no unit", nil, map[string]any{"value": "1"}, "needs unit"},
		{"an unknown member", nil, map[string]any{"value": "1", "unit": "kg", "scale": 2}, `"scale" is none of them`},
		{"a comparator undeclared", nil, quantityValue("1", "kg", "<"), "declares `comparator: true`"},
		{"a comparator that is no comparator", lab, quantityValue("1", "mg/dL", "~"), "comparator is one of"},
	} {
		_, err := coerceScalar(quantityProperty(tc.spec), tc.in)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err = %v, want one containing %q", tc.name, err, tc.want)
		}
	}
}

// The index reads the stored id, never a symbol, so a symbol edit moves no
// search result (decision 0186).
func TestQuantitySearchText(t *testing.T) {
	if got := quantityText(quantityValue("72.5", "kg")); got != "72.5 kg" {
		t.Fatalf("quantityText = %q", got)
	}
	if got := quantityText([]any{quantityValue("1", "kg"), "junk", quantityValue("37", "Cel")}); got != "1 kg, 37 Cel" {
		t.Fatalf("quantityText of a list = %q", got)
	}
}

func TestQuantityNarrows(t *testing.T) {
	kg := &vocabulary.QuantityBound{Value: "0", Unit: "kg"}
	g := &vocabulary.QuantityBound{Value: "0", Unit: "g"}
	for _, tc := range []struct {
		name      string
		cur, cand *vocabulary.QuantitySpec
		want      bool
	}{
		{"nothing changes", &vocabulary.QuantitySpec{Unit: "kg"}, &vocabulary.QuantitySpec{Unit: "kg"}, false},
		{"an anchor named", &vocabulary.QuantitySpec{}, &vocabulary.QuantitySpec{Unit: "kg"}, true},
		{"an anchor moved", &vocabulary.QuantitySpec{Unit: "kg"}, &vocabulary.QuantitySpec{Unit: "m"}, true},
		{"an anchor dropped", &vocabulary.QuantitySpec{Unit: "kg"}, &vocabulary.QuantitySpec{}, false},
		{"delta flipped", &vocabulary.QuantitySpec{}, &vocabulary.QuantitySpec{Delta: true}, true},
		{"a unit list added", &vocabulary.QuantitySpec{}, &vocabulary.QuantitySpec{Units: []string{"kg"}}, true},
		{"a unit removed", &vocabulary.QuantitySpec{Units: []string{"kg", "g"}}, &vocabulary.QuantitySpec{Units: []string{"kg"}}, true},
		{"a unit added", &vocabulary.QuantitySpec{Units: []string{"kg"}}, &vocabulary.QuantitySpec{Units: []string{"kg", "g"}}, false},
		{"the list dropped", &vocabulary.QuantitySpec{Units: []string{"kg"}}, &vocabulary.QuantitySpec{}, false},
		{"the comparator off", &vocabulary.QuantitySpec{Comparator: true}, &vocabulary.QuantitySpec{}, true},
		{"the comparator on", &vocabulary.QuantitySpec{}, &vocabulary.QuantitySpec{Comparator: true}, false},
		{"a bound added", &vocabulary.QuantitySpec{}, &vocabulary.QuantitySpec{Min: kg}, true},
		{"a bound moved", &vocabulary.QuantitySpec{Min: kg}, &vocabulary.QuantitySpec{Min: g}, true},
		{"a bound dropped", &vocabulary.QuantitySpec{Max: kg}, &vocabulary.QuantitySpec{}, false},
	} {
		if got := quantityNarrows(tc.cur, tc.cand); got != tc.want {
			t.Fatalf("%s: quantityNarrows = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The narrowing guard counts with the write path itself: a stored value
// strands exactly when the candidate declaration's next write through the
// graph would refuse it.
func TestQuantityStrandsIsTheWritePath(t *testing.T) {
	g, problems := vocabulary.BuildUnitGraph([]vocabulary.UnitRecord{{ID: "kg"}, {ID: "g"}, {ID: "lb", Code: "[lb_av]"}, {ID: "m"}},
		[]vocabulary.ConversionRecord{
			{ID: "g-kg", From: "g", To: "kg", Factor: big.NewRat(1, 1000)},
			{ID: "lb-kg", From: "lb", To: "kg", Factor: big.NewRat(45359237, 100000000)},
		})
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	cand := quantityProperty(&vocabulary.QuantitySpec{Unit: "kg", Units: []string{"kg"}})
	strands := quantityRefusesIn([]fieldStep{{key: "weight"}}, cand)(g)
	for raw, want := range map[string]bool{
		`{"value": "70", "unit": "kg"}`:                    false,
		`{"value": "154", "unit": "[lb_av]"}`:              true,
		`{"value": "1", "unit": "m"}`:                      true,
		`{"value": "70", "unit": "kg", "comparator": "<"}`: true,
		`"seventy"`: false,
	} {
		if got := strands([]byte(raw)); got != want {
			t.Fatalf("strands(%s) = %v, want %v", raw, got, want)
		}
	}
	list := quantityRefusesIn([]fieldStep{{key: "history", repeated: true}}, cand)(g)
	if !list([]byte(`[{"value": "1", "unit": "kg"}, {"value": "1", "unit": "g"}]`)) {
		t.Fatal("a list holding one refused value does not strand")
	}
}

// The quantity value's digit bound is the quantity's alone. A `decimal`
// stored before the bound existed must still coerce unchanged: a refusal here
// fails its round trip and drops it when its tombstone is restored
// (admitRestored treats a coercion error as removal).
func TestDecimalKeepsNoDigitBound(t *testing.T) {
	long := strings.Repeat("7", vocabulary.MaxDecimalDigits+1) + "." + strings.Repeat("3", 500)
	dec := &vocabulary.Property{Name: "amount", Datatype: vocabulary.DatatypeDecimal}
	got, err := coerceScalar(dec, long)
	if err != nil {
		t.Fatalf("a long decimal: %v", err)
	}
	if got != long {
		t.Fatalf("a long decimal stored %q, want it unchanged", got)
	}
	if _, err := coerceScalar(quantityProperty(nil), quantityValue(long, "kg")); err == nil ||
		!strings.Contains(err.Error(), "at most 1000 digits") {
		t.Fatalf("a long quantity value: got %v, want the digit bound", err)
	}
}
