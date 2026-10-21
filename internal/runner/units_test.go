package runner

import (
	"context"
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

func unitsCall(t *testing.T, host string, params map[string]any) hostCall {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	return hostCall{Host: host, Params: raw}
}

// A conversion reads at most vocabulary.MaxDecimalDigits digits, refused by
// name before any arithmetic, answers at most that many, so a body can write
// the answer back, and reads none once the invocation's context is done: the
// host-call loop serves it synchronously, so an unbounded one held the
// invocation past its timeout.
func TestUnitsConvertIsBounded(t *testing.T) {
	ctx := context.Background()
	long := strings.Repeat("7", vocabulary.MaxDecimalDigits+1)
	_, handled, err := serveUnits(ctx, unitsCall(t, hostUnitsConvert, map[string]any{"value": long, "from": "kg", "to": "g"}))
	if !handled || err == nil || !strings.Contains(err.Error(), "at most") {
		t.Fatalf("a value of %d digits: handled %v, err %v; want a refusal naming the bound", len(long), handled, err)
	}
	out, _, err := serveUnits(ctx, unitsCall(t, hostUnitsConvert, map[string]any{"value": long[1:], "from": "g", "to": "kg"}))
	if err != nil {
		t.Fatalf("a value at the bound: %v", err)
	}
	if got, want := out.(map[string]any)["value"].(string), long[4:]+"."+long[1:4]; got != want {
		t.Fatalf("a value at the bound converted to %.20s..., want %.20s...", got, want)
	}
	_, _, err = serveUnits(ctx, unitsCall(t, hostUnitsConvert, map[string]any{"value": long[1:], "from": "kg", "to": "g"}))
	if err == nil || !strings.Contains(err.Error(), "at most 1000 digits") {
		t.Fatalf("an answer of 1003 integer digits: %v, want a refusal naming the bound", err)
	}
	done, cancel := context.WithCancel(ctx)
	cancel()
	for _, host := range []string{hostUnitsConvert, hostUnitsCanonical, hostUnitsMeasure} {
		_, handled, err := serveUnits(done, unitsCall(t, host, map[string]any{"value": "1", "from": "kg", "to": "g", "code": "kg"}))
		if !handled || err == nil {
			t.Fatalf("%s after the context ended: handled %v, err %v; want a refusal", host, handled, err)
		}
	}
}

// host.units.convert answers a str, as it always has, that says whether it
// rounded: a result with no finite decimal expansion is 20 significant digits
// and `exact` is False.
func TestPythonUnitsConvertSaysWhetherItRounded(t *testing.T) {
	r := New()
	spec := Spec{
		Repository: "t1", Function: "units.g.exact",
		Runtime: "python",
		Source: `
import json

def main(input, host):
    kcal = host.units.convert(1, "kcal/d", "W")
    fahrenheit = host.units.convert(37, "°C", "°F")
    return {"effects": [], "output": {
        "kcal": kcal, "kcalExact": kcal.exact,
        "fahrenheit": fahrenheit, "fahrenheitExact": fahrenheit.exact,
        "isStr": isinstance(kcal, str), "json": json.dumps({"v": kcal}),
    }}
`,
		TimeoutMs: 5000,
	}
	res, err := r.Invoke(context.Background(), spec, testInput(), widgetBackend())
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	out, _ := res.Output.(map[string]any)
	for key, want := range map[string]any{
		"kcal": "0.048425925925925925926", "kcalExact": false,
		"fahrenheit": "98.6", "fahrenheitExact": true,
		"isStr": true, "json": `{"v": "0.048425925925925925926"}`,
	} {
		if out[key] != want {
			t.Errorf("%s = %#v, want %#v", key, out[key], want)
		}
	}
}

// graphFixture is a small graph: kg, g and lb joined linearly, Cel and degF
// joined through K, and mpg and L/100km joined by an expression.
func graphFixture(t *testing.T) *vocabulary.UnitGraph {
	t.Helper()
	rat := func(s string) *big.Rat {
		r, err := vocabulary.ParseRational(s)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	g, problems := vocabulary.BuildUnitGraph([]vocabulary.UnitRecord{
		{ID: "kg", Code: "kg"},
		{ID: "g", Code: "g"},
		{ID: "lb", Code: "[lb_av]"},
		{ID: "K"},
		{ID: "Cel", Aliases: []string{"°C"}},
		{ID: "degF", Code: "[degF]"},
		{ID: "mpg"},
		{ID: "L/100km"},
	}, []vocabulary.ConversionRecord{
		{ID: "lb-kg", From: "lb", To: "kg", Factor: rat("0.45359237")},
		{ID: "g-kg", From: "g", To: "kg", Factor: rat("1/1000")},
		{ID: "Cel-K", From: "Cel", To: "K", Factor: rat("1"), Offset: rat("273.15")},
		{ID: "degF-K", From: "degF", To: "K", Factor: rat("5/9"), Offset: rat("2298.35/9")},
		{ID: "mpg-L/100km", From: "mpg", To: "L/100km", Expr: "235.214583 / value", Inverse: "235.214583 / value"},
	})
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	return g
}

// Under the unit graph switch the unit calls read the repository's graph,
// loaded once per invocation at the first unit call, and each charges one
// call: a difference converts without the offset, an expression converts one
// value, `converts` answers a linear route, and `measure` is refused.
func TestHostUnitsLoadsTheGraphOnce(t *testing.T) {
	r := New()
	backend := widgetBackend()
	backend.units = graphFixture(t)
	spec := Spec{
		Repository: "t1", Function: "units.g.graph",
		Runtime: "python",
		Source: `
def main(input, host):
    out = {
        "lb": host.units.convert("1", "[lb_av]", "kg"),
        "delta": host.units.convert("0.3", "°C", "degF", delta=True),
        "absolute": host.units.convert("37", "Cel", "degF"),
        "economy": host.units.convert("30", "mpg", "L/100km"),
        "economyExact": host.units.convert("30", "mpg", "L/100km").exact,
        "canonical": host.units.canonical("[lb_av]"),
        "mass": host.units.converts("g", "lb"),
        "apart": host.units.converts("kg", "K"),
        "expression": host.units.converts("mpg", "L/100km"),
    }
    try:
        host.units.measure("kg")
        out["measure"] = "answered"
    except Exception as e:
        out["measure"] = str(e)
    return {"effects": [], "output": out}
`,
		TimeoutMs: 5000, ReadCalls: 9,
	}
	res, err := r.Invoke(context.Background(), spec, testInput(), backend)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	out, _ := res.Output.(map[string]any)
	for key, want := range map[string]any{
		"lb": "0.45359237", "delta": "0.54", "absolute": "98.6",
		"economy": "7.840486100000001", "economyExact": false, "canonical": "lb",
		"mass": true, "apart": false, "expression": false,
	} {
		if out[key] != want {
			t.Errorf("%s = %#v, want %#v", key, out[key], want)
		}
	}
	if m, _ := out["measure"].(string); !strings.Contains(m, "ask units.converts") {
		t.Errorf("measure under the graph = %q, want a refusal", m)
	}
	if backend.unitLoads != 1 {
		t.Errorf("the graph was loaded %d times, want once", backend.unitLoads)
	}
	// Nine distinct calls (the body asks one twice, which host.py answers
	// from its memo) charge nine calls; a budget of eight trips.
	spec.ReadCalls = 8
	if _, err := r.Invoke(context.Background(), spec, testInput(), backend); err == nil || !strings.Contains(err.Error(), "read budget") {
		t.Fatalf("nine unit calls under a budget of eight: %v", err)
	}
}

// Before the switch the calls read the engine's table: `converts` reads two
// codes in one measure, and nothing is charged.
func TestHostUnitsConvertsReadsTheTable(t *testing.T) {
	ctx := context.Background()
	s := &readState{backend: widgetBackend()}
	for _, c := range []struct {
		a, b string
		want bool
	}{{"kg", "[lb_av]", true}, {"mg/dL", "mmol/L", false}, {"bpm", "/s", true}, {"kgs", "kg", false}} {
		out, handled, err := s.serveUnitCall(ctx, unitsCall(t, hostUnitsConverts, map[string]any{"a": c.a, "b": c.b}))
		if !handled || err != nil {
			t.Fatalf("%s and %s: %v", c.a, c.b, err)
		}
		if got := out.(map[string]any)["converts"]; got != c.want {
			t.Errorf("%s and %s: converts %v, want %v", c.a, c.b, got, c.want)
		}
	}
	if s.calls != 0 {
		t.Errorf("the table's calls charged %d", s.calls)
	}
}
