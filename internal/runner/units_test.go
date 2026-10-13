package runner

import (
	"context"
	"encoding/json"
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

// A conversion reads at most vocabulary.MaxQuantityDigits digits, refused by
// name before any arithmetic, and none once the invocation's context is done:
// the host-call loop serves it synchronously, so an unbounded one held the
// invocation past its timeout.
func TestUnitsConvertIsBounded(t *testing.T) {
	ctx := context.Background()
	long := strings.Repeat("7", vocabulary.MaxQuantityDigits+1)
	_, handled, err := serveUnits(ctx, unitsCall(t, hostUnitsConvert, map[string]any{"value": long, "from": "kg", "to": "g"}))
	if !handled || err == nil || !strings.Contains(err.Error(), "at most") {
		t.Fatalf("a value of %d digits: handled %v, err %v; want a refusal naming the bound", len(long), handled, err)
	}
	out, _, err := serveUnits(ctx, unitsCall(t, hostUnitsConvert, map[string]any{"value": long[1:], "from": "kg", "to": "g"}))
	if err != nil {
		t.Fatalf("a value at the bound: %v", err)
	}
	if got := out.(map[string]any)["value"].(string); got != long[1:]+"000" {
		t.Fatalf("a value at the bound converted to %.20s...", got)
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
