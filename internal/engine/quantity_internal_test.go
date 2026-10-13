package engine

import (
	"encoding/json"
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

func TestCoerceQuantity(t *testing.T) {
	mass := &vocabulary.QuantitySpec{
		Measure: "mass", Units: []string{"kg", "[lb_av]", "ug"},
		Min: &vocabulary.QuantityBound{Value: "0", Unit: "kg"}, Max: &vocabulary.QuantityBound{Value: "700", Unit: "[lb_av]"},
	}
	lab := &vocabulary.QuantitySpec{Measure: "mass-concentration", Comparator: true}
	for _, tc := range []struct {
		name string
		spec *vocabulary.QuantitySpec
		in   map[string]any
		want map[string]any
	}{
		{"digits as written", mass, quantityValue("72.50", "kg"), quantityValue("72.50", "kg")},
		{"a canonical spelling", mass, quantityValue("+072.5", "kg"), quantityValue("72.5", "kg")},
		{"an alias stores its code", mass, quantityValue("5", "μg"), quantityValue("5", "ug")},
		{"the other micro sign", mass, quantityValue("5", "µg"), quantityValue("5", "ug")},
		{"pounds inside a kilogram bound", mass, quantityValue("154", "[lb_av]"), quantityValue("154", "[lb_av]")},
		{"a bound converted exactly", mass, quantityValue("317.51463", "kg"), quantityValue("317.51463", "kg")},
		{"a comparator where declared", lab, quantityValue("0.5", "ng/mL", "<"), quantityValue("0.5", "ng/mL", "<")},
		{"a null comparator", lab, map[string]any{"value": "1", "unit": "mg/dL", "comparator": nil}, quantityValue("1", "mg/dL")},
		{"undeclared: a unit names its measure", nil, quantityValue("62", "bpm"), quantityValue("62", "/min")},
		{"undeclared: a compound", nil, quantityValue("3", "kg/h"), quantityValue("3", "kg/h")},
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
		{"a bare number", mass, 72.5, "is an object"},
		{"a text", mass, "72.5 kg", "is an object"},
		{"a JSON number value", mass, map[string]any{"value": 72.5, "unit": "kg"}, "written as a string"},
		{"a json.Number value", mass, map[string]any{"value": json.Number("72.5"), "unit": "kg"}, "written as a string"},
		{"an exponent", mass, quantityValue("7.25e1", "kg"), "without an exponent"},
		{"no value", mass, map[string]any{"unit": "kg"}, "needs value"},
		{"no unit", mass, map[string]any{"value": "1"}, "needs unit"},
		{"an unknown unit", mass, quantityValue("1", "kgs"), "unknown unit symbol"},
		{"a unit outside the measure", mass, quantityValue("1", "km"), "not mass"},
		{"a unit outside the list", mass, quantityValue("1", "g"), "not one of the declared units"},
		{"below min", mass, quantityValue("-1", "kg"), "must be >= 0 kg"},
		{"above max, converted", mass, quantityValue("317.52", "kg"), "must be <= 700 lb"},
		{"an unknown member", mass, map[string]any{"value": "1", "unit": "kg", "scale": 2}, `"scale" is none of them`},
		{"a comparator undeclared", mass, quantityValue("1", "kg", "<"), "declares `comparator: true`"},
		{"a comparator that is no comparator", lab, quantityValue("1", "mg/dL", "~"), "comparator is one of"},
		{"undeclared: an ambiguous unit", nil, quantityValue("1", "J/s"), "ambiguous"},
		{"undeclared: a level in a compound", nil, quantityValue("1", "dB/s"), "exactly as the table lists it"},
	} {
		_, err := coerceScalar(quantityProperty(tc.spec), tc.in)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err = %v, want one containing %q", tc.name, err, tc.want)
		}
	}
}

func TestQuantityTitlesAndSearchText(t *testing.T) {
	if got := quantityText(quantityValue("72.5", "kg")); got != "72.5 kg" {
		t.Fatalf("quantityText = %q", got)
	}
	if got := quantityText([]any{quantityValue("1", "kg"), "junk", quantityValue("37", "Cel")}); got != "1 kg, 37 °C" {
		t.Fatalf("quantityText of a list = %q", got)
	}
}

// The ordering expression is a function of the declaration and the table,
// so the same declaration renders the same SQL every time: a declared index
// on it then matches the ordering, and a cursor minted under it stays valid.
func TestQuantityBaseSQLIsStable(t *testing.T) {
	spec := []*vocabulary.QuantitySpec{{Measure: "temperature"}}
	a := quantityBaseSQL(`(props->'temp')`, spec)
	if a != quantityBaseSQL(`(props->'temp')`, spec) {
		t.Fatal("the expression is not deterministic")
	}
	for _, want := range []string{`WHEN 'Cel' THEN`, `WHEN '[degF]' THEN`, `+ (5463)`, `regexp_replace(`} {
		if !strings.Contains(a, want) {
			t.Fatalf("temperature expression lacks %q: %s", want, a)
		}
	}
	diff := quantityBaseSQL(`(props->'temp')`, []*vocabulary.QuantitySpec{{Measure: "temperature-difference"}})
	if strings.Contains(diff, "5463") {
		t.Fatalf("a temperature difference carries the Celsius offset: %s", diff)
	}
	rate := quantityBaseSQL(`(props->'hr')`, []*vocabulary.QuantitySpec{{Measure: "rate"}})
	if strings.Count(rate, `WHEN '/min'`) != 1 {
		t.Fatalf("an annotated row and its bare form must share one arm: %s", rate)
	}
	undeclared := quantityBaseSQL(`(props->'x')`, []*vocabulary.QuantitySpec{nil})
	if strings.Contains(undeclared, `NULL::numeric`) || !strings.Contains(undeclared, `WHEN 'rad' THEN ((`) || !strings.Contains(undeclared, `WHEN '[pH]' THEN`) {
		t.Fatalf("rad must key through pi and a level by its own number, and no arm reads NULL: %s", undeclared)
	}
	// Stored codes the declared arms already cover add nothing, so the
	// expression stays the declared index's; one they do not cover adds its
	// arm after them.
	mass := []*vocabulary.QuantitySpec{{Measure: "mass"}}
	declared := quantityBaseSQL(`(props->'w')`, mass)
	if got := quantityBaseSQL(`(props->'w')`, mass, "kg", "g", "{each}kg"); got != declared {
		t.Fatalf("covered stored codes changed the expression:\n%s\n%s", got, declared)
	}
	widened := quantityBaseSQL(`(props->'w')`, mass, "Mg")
	if !strings.HasPrefix(widened, strings.TrimSuffix(declared, ` END)`)) || !strings.Contains(widened, `WHEN 'Mg' THEN ((`) {
		t.Fatalf("a stored Mg did not add its arm after the declared ones: %s", widened)
	}
}

func TestStripAnnotations(t *testing.T) {
	for in, want := range map[string]string{
		"{beats}/min": "/min", "V.A{reactive}": "V.A", "{steps}": "", "kg": "kg", "10*3{copies}/mL": "10*3/mL",
	} {
		if got := stripAnnotations(in); got != want {
			t.Fatalf("stripAnnotations(%q) = %q, want %q", in, got, want)
		}
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
		{"nothing changes", &vocabulary.QuantitySpec{Measure: "mass"}, &vocabulary.QuantitySpec{Measure: "mass"}, false},
		{"a measure named", &vocabulary.QuantitySpec{}, &vocabulary.QuantitySpec{Measure: "mass"}, true},
		{"a measure changed", &vocabulary.QuantitySpec{Measure: "mass"}, &vocabulary.QuantitySpec{Measure: "length"}, true},
		{"a measure dropped", &vocabulary.QuantitySpec{Measure: "mass"}, &vocabulary.QuantitySpec{}, false},
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

// The narrowing guard counts with the write coercion itself: a stored value
// strands exactly when the candidate declaration's next write would refuse it.
func TestQuantityStrandsIsTheWritePath(t *testing.T) {
	cand := quantityProperty(&vocabulary.QuantitySpec{Measure: "mass", Units: []string{"kg"}})
	strands := quantityStrands([]fieldStep{{key: "weight"}}, cand)
	for raw, want := range map[string]bool{
		`{"value": "70", "unit": "kg"}`:                    false,
		`{"value": "154", "unit": "[lb_av]"}`:              true,
		`{"value": "70", "unit": "kg", "comparator": "<"}`: true,
		`"seventy"`: false,
	} {
		if got := strands([]byte(raw)); got != want {
			t.Fatalf("strands(%s) = %v, want %v", raw, got, want)
		}
	}
	list := quantityStrands([]fieldStep{{key: "history", repeated: true}}, cand)
	if !list([]byte(`[{"value": "1", "unit": "kg"}, {"value": "1", "unit": "g"}]`)) {
		t.Fatal("a list holding one refused value does not strand")
	}
}
