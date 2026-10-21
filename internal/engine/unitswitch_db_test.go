package engine_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The unit graph (decision 0186) end to end through the doors an
// internal test does not reach: a function's host.units calls answered by
// the engine's real backend, and a change request proposed and accepted.

func putUnitRecord(t *testing.T, ds substrate.Dataset, id string, props map[string]any) {
	t.Helper()
	mustPut(t, ds, owner, substrate.PutInput{Kind: vocabulary.KindUnit, ID: id, Properties: props})
}

func putConversionRecord(t *testing.T, ds substrate.Dataset, id, from, to string, form map[string]any) {
	t.Helper()
	props := map[string]any{
		"from": map[string]any{"ref": vocabulary.KindUnit + "/" + from},
		"to":   map[string]any{"ref": vocabulary.KindUnit + "/" + to},
	}
	for k, v := range form {
		props[k] = v
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: vocabulary.KindConversion, ID: id, Properties: props})
}

// switchedUnits writes the units the tests below convert between: a mass
// component with a unit only this repository declares, and Celsius an offset
// from kelvin.
func switchedUnits(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	for id, props := range map[string]map[string]any{
		"kg":    {"name": "kilogram", "code": "kg"},
		"lb":    {"name": "pound", "code": "[lb_av]", "aliases": []any{"pound"}},
		"stone": {"name": "house stone", "symbol": "st"},
		"K":     {"name": "kelvin", "code": "K"},
		"Cel":   {"name": "degree Celsius", "code": "Cel", "symbol": "°C"},
	} {
		putUnitRecord(t, ds, id, props)
	}
	putConversionRecord(t, ds, "lb-kg", "lb", "kg", map[string]any{"factor": "0.45359237"})
	putConversionRecord(t, ds, "stone-lb", "stone", "lb", map[string]any{"factor": "14"})
	putConversionRecord(t, ds, "Cel-K", "Cel", "K", map[string]any{"factor": "1", "offset": "273.15"})
}

// A function's host.units calls convert through the repository's graph by
// the engine's real backend: a unit only this repository declares converts
// by its own factor, a difference drops the offset, converts answers the
// graph's components, `measure` is no call any more, and an edited
// conversion moves the next invocation's answer.
func TestFunctionConvertsThroughTheRepositorysGraph(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if err := enginetest.Install(ctx, ds, owner, fnConnector(nil, pyFn("convert", map[string]any{}, []any{widgetType}, `
def main(input, host):
    u = host.units
    stones = u.convert("2", "stone", "kg")
    return {"output": {
        "stones": str(stones), "exact": stones.exact,
        "difference": str(u.convert("10", "Cel", "K", delta=True)),
        "temperature": str(u.convert("10", "Cel", "K")),
        "converts": u.converts("stone", "pound"),
        "apart": u.converts("stone", "K"),
        "measure": hasattr(u, "measure"),
    }}
`))); err != nil {
		t.Fatalf("install the function: %v", err)
	}
	switchedUnits(t, ds)
	call := func() map[string]any {
		t.Helper()
		out, _, err := ds.CallFunction(ctx, substrate.ActorAPI, fnPackage+"/convert", map[string]any{})
		if err != nil {
			t.Fatalf("call: %v", err)
		}
		return out.(map[string]any)
	}
	got := call()
	for key, want := range map[string]any{
		"stones": "12.70058636", "exact": true, "difference": "10", "temperature": "283.15",
		"converts": true, "apart": false, "measure": false,
	} {
		if got[key] != want {
			t.Errorf("%s = %v, want %v", key, got[key], want)
		}
	}
	putConversionRecord(t, ds, "stone-lb", "stone", "lb", map[string]any{"factor": "10"})
	if got := call(); got["stones"] != "9.0718474" {
		t.Errorf("after the edit, 2 stone = %v kg, want 9.0718474", got["stones"])
	}
}

const (
	cargoPackage = "cargo.test.dev/cargo"
	cargoKind    = cargoPackage + "/crate"
)

// A change request's quantities are held at the proposal as a write holds
// them: a value in a unit outside the declaration's component is refused
// when it is proposed rather than when somebody accepts, and an accepted
// request stores the unit's id whatever spelling the proposal used.
func TestChangeRequestQuantitiesResolveThroughTheGraph(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	switchedUnits(t, ds)
	if err := enginetest.Install(ctx, ds, owner, enginetest.Manifest{
		Name: "cargo", Authority: cargoPackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(cargoPackage, 0),
			vocabulary.ActorManifest(cargoPackage, vocabulary.PackageActor(cargoPackage)),
			vocabulary.KindManifest(cargoPackage, map[string]any{"singular": "crate"}, map[string]any{
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
					"weight": map[string]any{
						"type": "quantity", "unit": "kg",
						"max": map[string]any{"value": "100", "unit": "kg"},
					},
				},
			}),
		},
	}); err != nil {
		t.Fatalf("install the crate kind: %v", err)
	}
	crate := mustPut(t, ds, owner, substrate.PutInput{Kind: cargoKind, Properties: map[string]any{
		"name": "c", "weight": map[string]any{"value": "1", "unit": "kg"},
	}})
	propose := func(weight map[string]any) (*substrate.Record, error) {
		return ds.Put(ctx, engram, substrate.PutInput{Kind: requestKind, Properties: map[string]any{
			"target": vocabulary.RecordPath(cargoKind, crate.ID),
			"diff":   map[string]any{"properties": map[string]any{"weight": weight}},
		}})
	}
	_, err := propose(map[string]any{"value": "1", "unit": "K"})
	if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "K does not convert to kg") {
		t.Fatalf("a proposal in another component: err = %v", err)
	}
	_, err = propose(map[string]any{"value": "300", "unit": "pound"})
	if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "must be <= 100 kg") {
		t.Fatalf("a proposal past the bound: err = %v", err)
	}
	req, err := propose(map[string]any{"value": "3", "unit": "stone"})
	if err != nil {
		t.Fatalf("a proposal in a repository-only unit: %v", err)
	}
	if err := accept(t, ds, req.ID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	got := mustGet(t, ds, cargoKind, crate.ID)
	if w, _ := got.Properties["weight"].(map[string]any); w["value"] != "3" || w["unit"] != "stone" {
		t.Fatalf("the accepted weight is %v", got.Properties["weight"])
	}
	req, err = propose(map[string]any{"value": "2", "unit": "pound"})
	if err != nil {
		t.Fatalf("a proposal in an alias: %v", err)
	}
	if err := accept(t, ds, req.ID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if w, _ := mustGet(t, ds, cargoKind, crate.ID).Properties["weight"].(map[string]any); w["unit"] != "lb" {
		t.Fatalf("the accepted alias stored %v, want the unit's id", w)
	}
}
