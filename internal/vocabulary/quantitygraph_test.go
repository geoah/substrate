package vocabulary_test

import "testing"

// Decision 0186, step 3: a quantity property may name its units the way the
// repository's unit graph reads them, by `unit`, and say its values are
// differences by `delta`. The loader holds both to their shape alone; a
// repository's graph judges the spellings at write.
func TestQuantityGraphDeclarations(t *testing.T) {
	t.Run("unit, units, delta and bounds", func(t *testing.T) {
		ty := loadThing(t, `  properties:
    weight:
      type: quantity
      unit: kg
      units: [kg, lb, steps]
      min: {value: "0", unit: kg}
      max: {value: "700", unit: lb}
    change: {type: quantity, unit: Cel, delta: true}
    sensor:
      type: reference
      kind: any
      properties:
        offset: {type: quantity, unit: K, delta: true}
`)
		q := ty.Props["weight"].Quantity
		if !q.GraphDialect() || q.Unit != "kg" || len(q.Units) != 3 || q.Measure != "" {
			t.Fatalf("weight spec = %+v", q)
		}
		if q.Min == nil || q.Min.Unit != "kg" || q.Max == nil || q.Max.Unit != "lb" || q.Max.Value != "700" {
			t.Fatalf("weight bounds = %+v %+v", q.Min, q.Max)
		}
		if c := ty.Props["change"].Quantity; !c.Delta || c.Unit != "Cel" {
			t.Fatalf("change spec = %+v", c)
		}
		if o := ty.Props["sensor"].Properties["offset"].Quantity; o == nil || !o.Delta || o.Unit != "K" {
			t.Fatalf("link offset spec = %+v", o)
		}
		props, _ := ty.Definition["properties"].(map[string]any)
		if w, _ := props["weight"].(map[string]any); w["unit"] != "kg" {
			t.Fatalf("unit did not ride into the definition: %v", w)
		}
	})
	t.Run("the table's checks stay off a graph declaration", func(t *testing.T) {
		q := loadThing(t, "  properties:\n    w: {type: quantity, unit: kg, units: [lb]}\n").Props["w"].Quantity
		if _, err := q.CheckUnit("[lb_av]"); err != nil {
			t.Fatalf("the table path held a graph declaration's units list: %v", err)
		}
	})
	for _, c := range []struct{ name, body, want string }{
		{"unit beside measures", `    w: {type: quantity, unit: kg, measures: mass}`, "not both"},
		{"a unit that is no string", `    w: {type: quantity, unit: 3}`, "is not a unit spelling"},
		{"a padded unit", `    w: {type: quantity, unit: " kg"}`, "starts or ends with a space"},
		{"delta that is no bool", `    w: {type: quantity, delta: "yes"}`, "true or false"},
		{"a unit listed twice", `    w: {type: quantity, unit: kg, units: [lb, lb]}`, "listed twice"},
		{"an empty unit list", `    w: {type: quantity, unit: kg, units: []}`, "at least one"},
		{"a bound without a unit", `    w: {type: quantity, unit: kg, min: {value: "0"}}`, "names its unit"},
		{"max below min in one unit", `    w: {type: quantity, unit: kg, min: {value: "2", unit: kg}, max: {value: "1", unit: kg}}`, "is below min"},
		{"unit on a string", `    w: {type: string, unit: kg}`, "only a quantity property declares unit"},
		{"delta on a decimal", `    w: {type: decimal, delta: true}`, "only a quantity property declares delta"},
	} {
		t.Run(c.name, func(t *testing.T) {
			loadThingErr(t, "  properties:\n"+c.body+"\n", c.want)
		})
	}
	t.Run("delta alone keeps the table dialect", func(t *testing.T) {
		q := loadThing(t, "  properties:\n    w: {type: quantity, measures: temperature, delta: true}\n").Props["w"].Quantity
		if q.GraphDialect() || !q.Delta || q.Measure != "temperature" {
			t.Fatalf("spec = %+v", q)
		}
	})
}
