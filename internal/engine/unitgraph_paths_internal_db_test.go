package engine

// The paths that reach the unit graph end to end (decision 0186): a function
// body's host.units calls through the engine's real backend, a change
// request's diff accepted onto a quantity, and a declaration's narrowing
// previewed and applied, each against the repository's own graph.

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const unitsFnPackage = "units.test.dev/unitfns"

// installUnitsFunction installs one Python function whose body asks every
// host.units call of the repository's graph and answers what each said.
func installUnitsFunction(t *testing.T, ds *dataset) string {
	t.Helper()
	src := `def main(input, host):
    out = {}
    lb = host.units.convert("1", "pound", "kg")
    out["lb"] = [str(lb), lb.exact]
    economy = host.units.convert("30", "mpg", "L/100km")
    out["economy"] = [str(economy), economy.exact]
    out["difference"] = str(host.units.convert("1", "Cel", "degF", delta=True))
    out["canonical"] = host.units.canonical("[lb_av]")
    out["mass"] = host.units.converts("lb", "g")
    out["apart"] = host.units.converts("kg", "K")
    out["expression"] = host.units.converts("mpg", "L/100km")
    out["measure"] = hasattr(host.units, "measure")
    try:
        host.units.convert("1", "kg", "K")
        out["across"] = "answered"
    except Exception as e:
        out["across"] = str(e)
    return {"output": out}
`
	if err := enginetest.Install(context.Background(), ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "unitfns", Authority: unitsFnPackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(unitsFnPackage, 0),
			vocabulary.ActorManifest(unitsFnPackage, vocabulary.PackageActor(unitsFnPackage)),
			vocabulary.FunctionManifest(unitsFnPackage, "convert", map[string]any{
				"description": "asks every host.units call of the repository's graph",
				"runtime":     vocabulary.RuntimePython,
				"timeout":     "PT20S",
				"source":      src,
			}),
		},
	}); err != nil {
		t.Fatalf("install the function: %v", err)
	}
	return unitsFnPackage + "/convert"
}

// A function body converts through the repository's graph, loaded through
// the engine's own backend: an alias, an expression route (inexact), a
// difference, the id a spelling stores as, and whether two units convert.
// `measure` is no call any more, and a conversion across components is
// refused. An owner's edit of a conversion reaches the next call.
func TestAFunctionConvertsThroughTheRepositorysGraph(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not on PATH; the function cannot run")
	}
	ctx := context.Background()
	ds := openSwitched(t)
	fn := installUnitsFunction(t, ds)
	call := func() map[string]any {
		t.Helper()
		out, _, err := ds.CallFunction(ctx, substrate.ActorAPI, fn, map[string]any{})
		if err != nil {
			t.Fatalf("call the function: %v", err)
		}
		m, ok := out.(map[string]any)
		if !ok {
			t.Fatalf("the function answered %#v", out)
		}
		return m
	}
	got := call()
	if lb, _ := got["lb"].([]any); len(lb) != 2 || lb[0] != "0.45359237" || lb[1] != true {
		t.Errorf("1 pound in kg = %v, want 0.45359237, exact", got["lb"])
	}
	if e, _ := got["economy"].([]any); len(e) != 2 || !strings.HasPrefix(e[0].(string), "7.84") || e[1] != false {
		t.Errorf("30 mpg in L/100km = %v, want about 7.84, inexact", got["economy"])
	}
	if got["difference"] != "1.8" {
		t.Errorf("a difference of 1 Cel in degF = %v, want 1.8", got["difference"])
	}
	if got["canonical"] != "lb" {
		t.Errorf("canonical([lb_av]) = %v, want the id lb", got["canonical"])
	}
	if got["mass"] != true || got["apart"] != false || got["expression"] != false {
		t.Errorf("converts: lb-g %v, kg-K %v, mpg-L/100km %v; want true, false, false (an expression route is not exact)",
			got["mass"], got["apart"], got["expression"])
	}
	if got["measure"] != false {
		t.Errorf("host.units still has measure: %v", got["measure"])
	}
	if s, _ := got["across"].(string); !strings.Contains(s, "do not convert") {
		t.Errorf("kg to K answered %v, want a refusal", got["across"])
	}
	putConversion(t, ds, "lb-g", "lb", "g", map[string]any{"factor": "454"})
	if lb, _ := call()["lb"].([]any); len(lb) != 2 || lb[0] != "0.454" {
		t.Errorf("after the owner's edit, 1 pound in kg = %v, want 0.454", lb)
	}
}

// A change request's diff is resolved through the graph: the accepted
// target stores the unit's id, and a unit the graph lacks or a value past a
// bound is refused, when proposed or else when accepted, where the refused
// request stays proposed.
func TestAChangeRequestWritesThroughTheGraph(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openSwitched(t)
	putThing(t, ds, "scale", map[string]any{"weight": qty("70", "kg")})
	const requestKind = "substrate.reamde.dev/core/recordpatchrequest"
	agent := substrate.Actor("engram")
	propose := func(props map[string]any) *substrate.Record {
		t.Helper()
		target, err := ds.Get(ctx, thingKind, "scale")
		if err != nil {
			t.Fatal(err)
		}
		rec, err := ds.Put(ctx, agent, substrate.PutInput{Kind: requestKind, Properties: map[string]any{
			"diff":   map[string]any{"properties": props, "ifVersion": target.Version},
			"target": vocabulary.RecordPath(thingKind, "scale"),
		}})
		if err != nil {
			t.Fatalf("propose %v: %v", props, err)
		}
		return rec
	}
	accept := func(req *substrate.Record) error {
		v := req.Version
		_, err := ds.Patch(ctx, substrate.ActorAPI, req.Kind, req.ID, substrate.PatchInput{
			Properties: map[string]any{"decision": "accepted"}, IfVersion: &v,
		})
		return err
	}
	req := propose(map[string]any{"weight": qty("160", "pound")})
	if err := accept(req); err != nil {
		t.Fatalf("accept a weight in pounds: %v", err)
	}
	got, err := ds.Get(ctx, thingKind, "scale")
	if err != nil {
		t.Fatal(err)
	}
	if u := storedUnit(t, got, "weight"); u != "lb" {
		t.Errorf("the accepted weight stores unit %q, want the id lb", u)
	}
	// A diff every accept would refuse is refused when proposed; one the
	// proposal admits and the accept refuses leaves the request proposed.
	for name, props := range map[string]map[string]any{
		"a unit the graph lacks": {"weight": qty("1", "furlong")},
		"a value past its bound": {"bounded": qty("100", "kg")},
	} {
		target, err := ds.Get(ctx, thingKind, "scale")
		if err != nil {
			t.Fatal(err)
		}
		req, err := ds.Put(ctx, agent, substrate.PutInput{Kind: requestKind, Properties: map[string]any{
			"diff":   map[string]any{"properties": props, "ifVersion": target.Version},
			"target": vocabulary.RecordPath(thingKind, "scale"),
		}})
		if err != nil {
			if !errors.Is(err, substrate.ErrValidation) {
				t.Errorf("%s: propose err = %v, want a validation refusal", name, err)
			}
			continue
		}
		if err := accept(req); !errors.Is(err, substrate.ErrValidation) && !errors.Is(err, substrate.ErrConflict) {
			t.Errorf("%s: accept err = %v, want a refusal", name, err)
		}
		after, err := ds.Get(ctx, req.Kind, req.ID)
		if err != nil {
			t.Fatal(err)
		}
		if after.Properties["decision"] != "proposed" {
			t.Errorf("%s: the refused request is %v, want proposed", name, after.Properties["decision"])
		}
	}
}

// A declaration's narrowing is judged through the graph on both doors: the
// preview lists the rows a weight anchored to another component would
// refuse, the apply refuses them, and an anchor in the same component is
// admitted on both.
func TestANarrowingIsPreviewedAndRefusedThroughTheGraph(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openSwitched(t)
	putUnit(t, ds, "m", map[string]any{"name": "meter", "code": "m"})
	putThing(t, ds, "pounds", map[string]any{"weight": qty("150", "[lb_av]")})
	putThing(t, ds, "kilos", map[string]any{"weight": qty("70", "kg")})
	docs := func(weight map[string]any) []map[string]any {
		return []map[string]any{
			vocabulary.KindManifest(thingsPackage, map[string]any{"singular": "thing"}, withProp(thingDeclaration(), "weight", weight)),
		}
	}
	narrowing := docs(map[string]any{"type": "quantity", "unit": "m"})
	plan, err := ds.PlanVocabularyApply(ctx, substrate.ActorAPI, narrowing)
	if err != nil {
		t.Fatalf("preview the narrowing: %v", err)
	}
	if blockers := strings.Join(plan.Blockers, "; "); !strings.Contains(blockers, "weight") || !strings.Contains(blockers, "2 live records") {
		t.Errorf("the preview's blockers = %q, want the weight's two rows", blockers)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, narrowing); err == nil || !strings.Contains(err.Error(), "2 live records") {
		t.Errorf("apply the narrowing: err = %v, want the two rows named", err)
	}
	widening := docs(map[string]any{"type": "quantity", "unit": "g"})
	if plan, err := ds.PlanVocabularyApply(ctx, substrate.ActorAPI, widening); err != nil || len(plan.Blockers) != 0 {
		t.Errorf("preview an anchor of the same component: %v, blockers %v", err, plan.Blockers)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, widening); err != nil {
		t.Errorf("apply an anchor of the same component: %v", err)
	}
}
