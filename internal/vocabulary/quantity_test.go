package vocabulary_test

import (
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

func TestQuantityDeclarations(t *testing.T) {
	t.Run("every key and every container", func(t *testing.T) {
		ty := loadThing(t, `  properties:
    weight:
      type: quantity
      unit: kg
      units: [kg, lb]
      min: {value: "0", unit: kg}
      max: {value: "700", unit: lb}
      default: {value: "70", unit: kg}
    result: {type: quantity, comparator: true}
    history: {type: quantity, repeated: true}
    byRoom: {type: quantity, keyed: true, unit: Cel}
    reading:
      type: object
      fields:
        level: {type: quantity, unit: dB}
    sensor:
      type: reference
      kind: any
      properties:
        offset: {type: quantity, unit: K, delta: true}
`)
		w := ty.Props["weight"]
		if w.Datatype != vocabulary.DatatypeQuantity || w.Quantity == nil {
			t.Fatalf("weight = %+v, want a quantity", w)
		}
		q := w.Quantity
		if q.Unit != "kg" || q.Measure != "" || len(q.Units) != 2 || q.Comparator {
			t.Fatalf("weight spec = %+v", q)
		}
		if q.Min == nil || q.Min.Value != "0" || q.Min.Unit != "kg" || q.Max == nil || q.Max.Unit != "lb" {
			t.Fatalf("weight bounds = %+v %+v", q.Min, q.Max)
		}
		if w.Min != nil || w.Max != nil {
			t.Fatal("a quantity bound landed in the number bounds")
		}
		if !ty.Props["result"].Quantity.Comparator {
			t.Fatal("comparator: true did not parse")
		}
		if !ty.Props["history"].Repeated || !ty.Props["byRoom"].Keyed || ty.Props["byRoom"].Quantity.Unit != "Cel" {
			t.Fatal("repeated and keyed quantities did not parse")
		}
		if f := ty.Props["reading"].Fields["level"]; f == nil || f.Quantity == nil || f.Quantity.Unit != "dB" {
			t.Fatalf("object field = %+v", f)
		}
		if l := ty.Props["sensor"].Properties["offset"]; l == nil || l.Quantity == nil || l.Quantity.Unit != "K" || !l.Quantity.Delta {
			t.Fatalf("link property = %+v", l)
		}
		props, _ := ty.Definition["properties"].(map[string]any)
		weight, _ := props["weight"].(map[string]any)
		if weight["unit"] != "kg" {
			t.Fatalf("the declaration's unit did not ride into the definition: %v", weight)
		}
	})
	t.Run("a refinement names the unit", func(t *testing.T) {
		ty := loadThing(t, `  properties:
    air: {type: airtemperature}
    narrow: {type: airtemperature, units: [degF]}
    change: {type: airtemperature, delta: true}
---
kind: substrate.reamde.dev/core/propertytype
metadata: {id: g.example.com/g/airtemperature}
data: {authority: g.example.com, package: g, base: quantity, unit: Cel, comparator: true}
`)
		air := ty.Props["air"]
		if air.Datatype != vocabulary.DatatypeQuantity || air.Refined != "airtemperature" ||
			air.Quantity == nil || air.Quantity.Unit != "Cel" || !air.Quantity.Comparator || air.Quantity.Delta {
			t.Fatalf("air = %+v %+v", air, air.Quantity)
		}
		if n := ty.Props["narrow"].Quantity; n.Unit != "Cel" || len(n.Units) != 1 || n.Units[0] != "degF" {
			t.Fatalf("narrow = %+v", n)
		}
		if c := ty.Props["change"].Quantity; c.Unit != "Cel" || !c.Delta {
			t.Fatalf("change = %+v", c)
		}
		if len(air.Quantity.Units) != 0 || air.Quantity.Delta {
			t.Fatal("a use site's keys reached the refinement every other use shares")
		}
	})
	for _, c := range []struct{ name, body, want string }{
		{"measures, the table's key", `    w: {type: quantity, measures: mass}`, `unknown key "measures"`},
		{"a number bound", `    w: {type: quantity, unit: kg, min: 0}`, "a quantity bound is a quantity value"},
		{"a bound with an exponent", `    w: {type: quantity, unit: kg, max: {value: "1e3", unit: kg}}`, "without an exponent"},
		{"a bound with a comparator", `    w: {type: quantity, unit: kg, max: {value: "1", unit: kg, comparator: "<"}}`, "is neither"},
		{"a comparator that is not a bool", `    w: {type: quantity, comparator: "yes"}`, "true or false"},
		{"a pattern", `    w: {type: quantity, pattern: "^1"}`, "never a pattern or values"},
		{"a value set", `    w: {type: quantity, values: [a, b]}`, "never a pattern or values"},
		{"units on a string", `    w: {type: string, units: [kg]}`, "only a quantity property declares units"},
		{"comparator on money", `    w: {type: money, comparator: true}`, "only a quantity property declares comparator"},
		{"a default that is no quantity", `    w: {type: quantity, default: "72 kg"}`, "expected a quantity value"},
	} {
		t.Run(c.name, func(t *testing.T) {
			loadThingErr(t, "  properties:\n"+c.body+"\n", c.want)
		})
	}
	t.Run("a refinement takes no bounds", func(t *testing.T) {
		loadThingErr(t, `  properties:
    w: {type: weight}
---
kind: substrate.reamde.dev/core/propertytype
metadata: {id: g.example.com/g/weight}
data: {authority: g.example.com, package: g, base: quantity, unit: kg, min: {value: "0", unit: kg}}
`, "declare min and max on the property")
	})
	t.Run("a refinement takes no measures", func(t *testing.T) {
		loadThingErr(t, `  properties:
    w: {type: weight}
---
kind: substrate.reamde.dev/core/propertytype
metadata: {id: g.example.com/g/weight}
data: {authority: g.example.com, package: g, base: quantity, measures: mass}
`, `unknown key "measures"`)
	})
}

// Decision 0163: money is never a unit. A currency code is no UCUM code, so
// a quantity in one is refused on write and in a declaration, and money stays
// the `money` datatype's.
func TestQuantityRefusesACurrencyAsAUnit(t *testing.T) {
	for _, code := range []string{"EUR", "USD", "GBP", "JPY", "CHF", "XAU", "EUR/kg", "[EUR]"} {
		if canon, err := vocabulary.CanonicalUnit(code); err == nil {
			t.Errorf("CanonicalUnit(%q) = %q, want a refusal", code, canon)
		}
		var spec *vocabulary.QuantitySpec
		if m, err := spec.CheckUnit(code); err == nil {
			t.Errorf("CheckUnit(%q) = %s, want a refusal", code, m.Name)
		}
	}
}
