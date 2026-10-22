package engine

import (
	"testing"
)

// Every unit migration 3 anchors a measure to is a unit the shipped seed
// writes, so a rewritten declaration names a unit every repository holds.
func TestPreGraphMeasureUnitsAreShippedUnits(t *testing.T) {
	shipped, err := shippedUnitRecords()
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, s := range shipped {
		if s.kind == preGraphUnitKind {
			ids[s.id] = true
		}
	}
	for measure, unit := range preGraphMeasureUnits {
		if !ids[unit] {
			t.Errorf("measure %s anchors to %s, which the shipped seed does not write", measure, unit)
		}
	}
}

// The declaration rewrite: a measure becomes its unit at any depth, a
// temperature difference a delta, `arbitrary` nothing; a metric kind's unit
// strings become references and gain `units`; an observed kind's managed
// `measure` is retired; and a second pass changes nothing.
func TestPreGraphDeclarationRewrite(t *testing.T) {
	q := func(measures string) map[string]any { return map[string]any{"type": "quantity", "measures": measures} }
	kind := map[string]any{
		"traits": []any{"substrate.reamde.dev/core/temporal(range)", "substrate.reamde.dev/core/observed(single)", "substrate.reamde.dev/core/metric"},
		"properties": map[string]any{
			"distance": q("length"),
			"change":   q("temperature-difference"),
			"titre":    q("arbitrary"),
			"kept":     map[string]any{"type": "quantity", "measures": "mass", "unit": "g"},
			"leg": map[string]any{"type": "object", "fields": map[string]any{
				"pace": q("pace"),
			}},
			"measure":  map[string]any{"type": "string", "managed": true},
			"measures": map[string]any{"type": "string"},
			"unit":     map[string]any{"type": "string", "description": "the unit"},
		},
	}
	out, changed, metric, observed, left := rewritePreGraphDeclaration(kindKind, kind)
	if !changed || !metric || !observed || len(left) != 0 {
		t.Fatalf("changed %v, metric %v, observed %v, left %v", changed, metric, observed, left)
	}
	props := out["properties"].(map[string]any)
	for name, want := range map[string]string{"distance": "m", "change": "K", "kept": "g"} {
		if p := props[name].(map[string]any); p["unit"] != want || p["measures"] != nil {
			t.Errorf("%s = %v, want unit %s", name, p, want)
		}
	}
	if p := props["change"].(map[string]any); p["delta"] != true {
		t.Errorf("change = %v, want a delta", p)
	}
	if p := props["titre"].(map[string]any); p["unit"] != nil || p["measures"] != nil {
		t.Errorf("titre = %v, want neither key", p)
	}
	if p := props["leg"].(map[string]any)["fields"].(map[string]any)["pace"].(map[string]any); p["unit"] != "s/m" {
		t.Errorf("leg.pace = %v, want unit s/m", p)
	}
	if _, held := props["measure"]; held {
		t.Error("measure is still declared")
	}
	if r := out["retired"].(map[string]any)["properties"].([]any); len(r) != 1 || r[0] != "measure" {
		t.Errorf("retired = %v, want measure", out["retired"])
	}
	if _, held := props["measures"]; held {
		t.Error("the metric's measures is still declared")
	}
	if u := props["unit"].(map[string]any); u["type"] != "reference" || u["kind"] != preGraphUnitKind || u["description"] != "the unit" {
		t.Errorf("unit = %v", u)
	}
	if u := props["units"].(map[string]any); u["type"] != "reference" || u["repeated"] != true {
		t.Errorf("units = %v", u)
	}
	if kind["properties"].(map[string]any)["distance"].(map[string]any)["measures"] != "length" {
		t.Error("the rewrite edited the row it read")
	}
	if _, again, _, _, _ := rewritePreGraphDeclaration(kindKind, out); again {
		t.Error("a second pass changed the rewritten declaration")
	}

	pt, changed, _, _, _ := rewritePreGraphDeclaration(kindPropertyType, map[string]any{"base": "quantity", "measures": "rate"})
	if !changed || pt["unit"] != "1/min" || pt["measures"] != nil {
		t.Errorf("propertytype = %v, changed %v", pt, changed)
	}
	_, _, _, _, left = rewritePreGraphDeclaration(kindKind, map[string]any{"properties": map[string]any{"x": q("nonesuch")}})
	if len(left) != 1 || left[0] != "nonesuch" {
		t.Errorf("an unknown measure: left %v", left)
	}
}
