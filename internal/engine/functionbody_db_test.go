package engine_test

// THE BODY THAT RUNS IS THE ONE THE LAST APPLY LANDED (issue #576). Mneme v6
// re-applied a provider whose sync body pins a mock endpoint and watched the
// next delivery reach the PREVIOUS run's port — one apply behind, on the
// plain update path and again after an uninstall and a re-apply. Six seed runs
// went to a dead endpoint before the runner started pinning its ports.
//
// Both paths are here, and both assert the same thing in the same way: apply
// v1, drive one delivery, apply v2 with a different body, drive another, and
// read what the second delivery WROTE. The effect is the only honest witness —
// the registry can be asked what it holds, but what the dispatcher handed the
// runner is what the task row says.

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// stampSource writes the version it was built with onto a task, so a delivery
// says which body ran.
func stampSource(version string) string {
	return fmt.Sprintf(`
def main(input, host):
    env = input["envelope"]
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": "t-" + env["change"]["id"],
                         "properties": {"name": %q}}]}
`, version)
}

// stampedTask is what the delivery for one widget wrote.
func stampedTask(t *testing.T, ds substrate.Dataset, widgetID string) string {
	t.Helper()
	rec, err := ds.Get(context.Background(), taskType, "t-"+widgetID)
	if err != nil {
		t.Fatalf("the delivery for %s wrote no task: %v", widgetID, err)
	}
	name, _ := rec.Properties["name"].(string)
	return name
}

// driveStamp writes one widget, runs a dispatcher pass, and answers with the
// body version the delivery wrote.
func driveStamp(t *testing.T, ds substrate.Dataset, id string) string {
	t.Helper()
	w := mustPut(t, ds, fnActor, substrate.PutInput{
		Kind: widgetType, ID: id, Properties: map[string]any{"name": id},
	})
	process(t, ds)
	return stampedTask(t, ds, w.ID)
}

// The ordinary update path: the same function identity, a new source.
func TestAnAppliedBodyRunsOnTheVeryNextDelivery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newFnDataset(t,
		[]enginetest.Trigger{trigOn("stamp", map[string]any{"kinds": []any{widgetType}})},
		pyFn("stamp", map[string]any{}, []any{taskType}, stampSource("v1")))

	if got := driveStamp(t, ds, "w1"); got != "v1" {
		t.Fatalf("the first delivery wrote %q, want v1", got)
	}

	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		pyFn("stamp", map[string]any{}, []any{taskType}, stampSource("v2")),
	}); err != nil {
		t.Fatalf("re-apply the function with a new body: %v", err)
	}
	if got := driveStamp(t, ds, "w2"); got != "v2" {
		t.Fatalf("the delivery after the apply wrote %q, want v2 — the dispatcher ran the previous apply's body", got)
	}
}

// The uninstall path: the package's declarations go away and come back. The
// runner's registration for the old body is retired when the registry drops
// it, so the re-apply must not be able to reach it.
func TestAReappliedBodyRunsAfterItsPackageWasTornDown(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newFnDataset(t,
		[]enginetest.Trigger{trigOn("stamp", map[string]any{"kinds": []any{widgetType}})},
		pyFn("stamp", map[string]any{}, []any{taskType}, stampSource("v1")))

	if got := driveStamp(t, ds, "w1"); got != "v1" {
		t.Fatalf("the first delivery wrote %q, want v1", got)
	}

	// Drop the function alone — the prune an uninstall's teardown performs on
	// the closure, without the bundle lifecycle around it. Its trigger stops
	// resolving and skips, which is what the engine logs while a package is
	// down.
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(fnPackage, 0),
		vocabulary.ActorManifest(fnPackage, vocabulary.PackageActor(fnPackage)),
		vocabulary.KindManifest(fnPackage, map[string]any{"singular": "widget"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
	}); err != nil {
		t.Fatalf("tear the function down: %v", err)
	}
	process(t, ds)

	// Back, with a different body under the same identity.
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		pyFn("stamp", map[string]any{}, []any{taskType}, stampSource("v2")),
	}); err != nil {
		t.Fatalf("re-apply the function: %v", err)
	}
	if got := driveStamp(t, ds, "w2"); got != "v2" {
		t.Fatalf("the delivery after the re-apply wrote %q, want v2 — the uninstalled body was still cached", got)
	}
}

// THE CASE THE REPORT DESCRIBES: the apply lands MID-PASS. A dispatcher pass
// resolves every trigger once, when it loads them, and then runs for as long
// as its drains and backoffs take — minutes, on a provider seed. A body
// applied inside that window used to reach the next delivery of the SAME pass
// as the previous apply's body, which is the "one apply behind" Mneme v6
// measured; the invocation after it, in the next pass, was correct.
//
// The apply is driven from the runner's own invoke hook, so it lands between
// two deliveries of one pass with no timing to get wrong.
func TestADeliveryRunsTheBodyTheRegistryHoldsNow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var (
		mu    sync.Mutex
		ds    substrate.Dataset
		once  sync.Once
		reapl error
	)
	_, ds = newDataset(t, engine.WithTestInvokeHook(func(string) {
		once.Do(func() {
			mu.Lock()
			defer mu.Unlock()
			_, reapl = ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
				pyFn("stamp", map[string]any{}, []any{taskType}, stampSource("v2")),
			})
		})
	}))
	if err := enginetest.Install(ctx, ds, owner, fnConnector(
		[]enginetest.Trigger{trigOn("stamp", map[string]any{"kinds": []any{widgetType}})},
		pyFn("stamp", map[string]any{}, []any{taskType}, stampSource("v1")))); err != nil {
		t.Fatalf("register connector: %v", err)
	}

	// Two widgets before the pass, so one pass carries two deliveries: the
	// first re-applies the body, the second must run what it applied.
	first := mustPut(t, ds, fnActor, substrate.PutInput{
		Kind: widgetType, ID: "w1", Properties: map[string]any{"name": "w1"},
	})
	second := mustPut(t, ds, fnActor, substrate.PutInput{
		Kind: widgetType, ID: "w2", Properties: map[string]any{"name": "w2"},
	})
	process(t, ds)
	mu.Lock()
	err := reapl
	mu.Unlock()
	if err != nil {
		t.Fatalf("the mid-pass re-apply failed, so the test proves nothing: %v", err)
	}
	if got := stampedTask(t, ds, first.ID); got != "v1" {
		t.Fatalf("the delivery that ran before the apply wrote %q, want v1", got)
	}
	if got := stampedTask(t, ds, second.ID); got != "v2" {
		t.Fatalf("the delivery after the mid-pass apply wrote %q, want v2 — the pass is serving the body it resolved when it loaded", got)
	}
}
