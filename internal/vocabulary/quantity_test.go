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
      measures: mass
      units: [kg, "[lb_av]"]
      min: {value: "0", unit: kg}
      max: {value: "700", unit: "[lb_av]"}
      default: {value: "70", unit: kg}
    result: {type: quantity, comparator: true}
    history: {type: quantity, repeated: true}
    byRoom: {type: quantity, keyed: true, measures: temperature}
    reading:
      type: object
      fields:
        level: {type: quantity, measures: level}
    sensor:
      type: reference
      kind: any
      properties:
        offset: {type: quantity, measures: temperature-difference}
`)
		w := ty.Props["weight"]
		if w.Datatype != vocabulary.DatatypeQuantity || w.Quantity == nil {
			t.Fatalf("weight = %+v, want a quantity", w)
		}
		q := w.Quantity
		if q.Measure != "mass" || len(q.Units) != 2 || q.Comparator {
			t.Fatalf("weight spec = %+v", q)
		}
		if q.Min == nil || q.Min.Value != "0" || q.Min.Unit != "kg" || q.Max == nil || q.Max.Unit != "[lb_av]" {
			t.Fatalf("weight bounds = %+v %+v", q.Min, q.Max)
		}
		if w.Min != nil || w.Max != nil {
			t.Fatal("a quantity bound landed in the number bounds")
		}
		if !ty.Props["result"].Quantity.Comparator {
			t.Fatal("comparator: true did not parse")
		}
		if !ty.Props["history"].Repeated || !ty.Props["byRoom"].Keyed {
			t.Fatal("repeated and keyed quantities did not parse")
		}
		if f := ty.Props["reading"].Fields["level"]; f == nil || f.Quantity == nil || f.Quantity.Measure != "level" {
			t.Fatalf("object field = %+v", f)
		}
		if l := ty.Props["sensor"].Properties["offset"]; l == nil || l.Quantity == nil || l.Quantity.Measure != "temperature-difference" {
			t.Fatalf("link property = %+v", l)
		}
		props, _ := ty.Definition["properties"].(map[string]any)
		weight, _ := props["weight"].(map[string]any)
		if weight["measures"] != "mass" {
			t.Fatalf("the declaration's measures did not ride into the definition: %v", weight)
		}
	})
	t.Run("a refinement names the measure", func(t *testing.T) {
		ty := loadThing(t, `  properties:
    air: {type: airtemperature}
    narrow: {type: airtemperature, units: [Cel]}
---
kind: substrate.reamde.dev/core/propertytype
metadata: {id: g.example.com/g/airtemperature}
data: {authority: g.example.com, package: g, base: quantity, measures: temperature, comparator: true}
`)
		air := ty.Props["air"]
		if air.Datatype != vocabulary.DatatypeQuantity || air.Refined != "airtemperature" ||
			air.Quantity == nil || air.Quantity.Measure != "temperature" || !air.Quantity.Comparator {
			t.Fatalf("air = %+v %+v", air, air.Quantity)
		}
		if n := ty.Props["narrow"].Quantity; n.Measure != "temperature" || len(n.Units) != 1 || n.Units[0] != "Cel" {
			t.Fatalf("narrow = %+v", n)
		}
		if len(air.Quantity.Units) != 0 {
			t.Fatal("the use site's units reached the refinement every other use shares")
		}
	})
	for _, c := range []struct{ name, body, want string }{
		{"an unknown measure", `    w: {type: quantity, measures: weight}`, "is not a measure"},
		{"a unit outside the measure", `    w: {type: quantity, measures: mass, units: [kg, km]}`, "km"},
		{"an alias in units", `    w: {type: quantity, units: ["°C"]}`, `write the code "Cel"`},
		{"a unit listed twice", `    w: {type: quantity, units: [kg, kg]}`, "listed twice"},
		{"an empty unit list", `    w: {type: quantity, units: []}`, "at least one"},
		{"a unit that is no code", `    w: {type: quantity, units: [kgs]}`, "unknown unit symbol"},
		{"an ambiguous unit undeclared", `    w: {type: quantity, units: ["J/s"]}`, "ambiguous"},
		{"a number bound", `    w: {type: quantity, measures: mass, min: 0}`, "a quantity bound is a quantity value"},
		{"a bound without a measure", `    w: {type: quantity, min: {value: "0", unit: kg}}`, "needs `measures:`"},
		{"a bound in another measure", `    w: {type: quantity, measures: mass, max: {value: "1", unit: m}}`, "not mass"},
		{"a bound with an exponent", `    w: {type: quantity, measures: mass, max: {value: "1e3", unit: kg}}`, "without an exponent"},
		{"a bound with a comparator", `    w: {type: quantity, measures: mass, max: {value: "1", unit: kg, comparator: "<"}}`, "is neither"},
		{"max below min", `    w: {type: quantity, measures: mass, min: {value: "1", unit: kg}, max: {value: "999", unit: g}}`, "is below min"},
		{"a level bound over two units", `    w: {type: quantity, measures: power-level, min: {value: "-90", unit: dBm}}`, "needs `units:`"},
		{"a rate bound", `    w: {type: quantity, measures: rate, max: {value: "300", unit: /min}}`, "needs `units:`"},
		{"a count bound", `    w: {type: quantity, measures: count, min: {value: "0", unit: "{count}"}}`, "needs `units:`"},
		{"an angle bound", `    w: {type: quantity, measures: angle, max: {value: "360", unit: deg}}`, "needs `units:`"},
		{"a listed unit the bound does not reach", `    w: {type: quantity, measures: rate, units: [/min, "{beats}/min"], max: {value: "300", unit: /min}}`, "{beats}/min"},
		{"a comparator that is not a bool", `    w: {type: quantity, comparator: "yes"}`, "true or false"},
		{"a pattern", `    w: {type: quantity, pattern: "^1"}`, "never a pattern or values"},
		{"a value set", `    w: {type: quantity, values: [a, b]}`, "never a pattern or values"},
		{"measures on a number", `    w: {type: decimal, measures: mass}`, "only a quantity property declares measures"},
		{"units on a string", `    w: {type: string, units: [kg]}`, "only a quantity property declares units"},
		{"comparator on money", `    w: {type: money, comparator: true}`, "only a quantity property declares comparator"},
		{"a default that is no quantity", `    w: {type: quantity, default: "72 kg"}`, "expected a quantity value"},
	} {
		t.Run(c.name, func(t *testing.T) {
			loadThingErr(t, "  properties:\n"+c.body+"\n", c.want)
		})
	}
	t.Run("a bound every admitted unit converts into", func(t *testing.T) {
		ty := loadThing(t, `  properties:
    pulse: {type: quantity, measures: rate, units: [/min, /s], max: {value: "300", unit: /min}}
    weight: {type: quantity, measures: mass, min: {value: "0", unit: kg}}
    heading: {type: quantity, measures: angle, units: [deg], max: {value: "360", unit: deg}}
    level: {type: quantity, measures: level, max: {value: "140", unit: dB}}
`)
		for _, name := range []string{"pulse", "weight", "heading", "level"} {
			if q := ty.Props[name].Quantity; q == nil || q.Max == nil && q.Min == nil {
				t.Fatalf("%s lost its bound: %+v", name, q)
			}
		}
	})
	t.Run("a refinement takes no bounds", func(t *testing.T) {
		loadThingErr(t, `  properties:
    w: {type: weight}
---
kind: substrate.reamde.dev/core/propertytype
metadata: {id: g.example.com/g/weight}
data: {authority: g.example.com, package: g, base: quantity, measures: mass, min: {value: "0", unit: kg}}
`, "declare min and max on the property")
	})
	t.Run("a refinement's units against the use site's measure", func(t *testing.T) {
		loadThingErr(t, `  properties:
    w: {type: weight, measures: length}
---
kind: substrate.reamde.dev/core/propertytype
metadata: {id: g.example.com/g/weight}
data: {authority: g.example.com, package: g, base: quantity, measures: mass, units: [kg]}
`, "kg measures mass, not length")
	})
}
