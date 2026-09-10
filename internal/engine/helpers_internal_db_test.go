package engine

// Fixtures the in-package suites share: a repository holding one plain widget
// kind, driven at transaction level (the race dataset), and a reopenable
// repository holding the widget connector with its inline mirror function
// (the registry harness). Both live here because several subjects use them
// and none owns them.

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// newRaceDataset provisions a repository with one plain widget kind and hands
// back the INTERNAL dataset, so tests can drive txn-level paths directly.
func newRaceDataset(t *testing.T) *dataset {
	t.Helper()
	ctx := context.Background()
	dsn := MigratedDSN(t)
	svc, err := OpenForTest(t, ctx, dsn,
		WithDataRoot(t.TempDir()),
		WithCredentialKey(TestCredentialKey), WithKindsDir(CoreKindsDir))
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	d, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	ds, ok := d.(*dataset)
	if !ok {
		t.Fatalf("dataset is a %T", d)
	}
	importVocabulary(t, ds, "tasks")
	if err := enginetest.Install(ctx, ds, substrate.ActorAPI, enginetest.Manifest{
		Name: "race", Authority: racePackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(racePackage, 0),
			vocabulary.ActorManifest(racePackage, vocabulary.PackageActor(racePackage)),
			vocabulary.KindManifest(racePackage, map[string]any{"singular": "widget"},
				map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		},
	}); err != nil {
		t.Fatalf("register connector: %v", err)
	}
	return ds
}

func racePut(t *testing.T, ds *dataset, props map[string]any) *substrate.Record {
	t.Helper()
	e, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{
		Kind: raceWidget, Properties: props,
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	return e
}

const (
	racePackage = "race.test.dev/race"
	raceWidget  = racePackage + "/widget"
	raceActor   = substrate.Actor("function.racer." + racePackage)
)

// reopenableWidgetDataset returns an open() that reopens the same repository schema — the
// repository-open migration harness.
func reopenableWidgetDataset(t *testing.T) (open func() *dataset, closeSvc func()) {
	t.Helper()
	ctx := context.Background()
	dsn := MigratedDSN(t)
	var last substrate.Service
	created := false
	imported := false
	open = func() *dataset {
		svc, err := OpenForTest(t, ctx, dsn,
			WithDataRoot(t.TempDir()),
			WithCredentialKey(TestCredentialKey), WithKindsDir(CoreKindsDir))
		if err != nil {
			t.Fatalf("open engine: %v", err)
		}
		last = svc
		if !created {
			if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
				t.Fatalf("create repository: %v", err)
			}
			created = true
		}
		d, err := svc.Dataset(ctx, testdb.Repository(t))
		if err != nil {
			t.Fatalf("open dataset: %v", err)
		}
		if !imported {
			importVocabulary(t, d, "tasks")
			imported = true
		}
		return d.(*dataset)
	}
	closeSvc = func() {
		if last != nil {
			_ = last.Close()
			last = nil
		}
	}
	t.Cleanup(closeSvc)
	return open, closeSvc
}

// widgetsManifest is the widget connector: one kind, one inline mirror function,
// and optionally the function's default trigger.
func widgetsManifest(withTrigger bool) enginetest.Manifest {
	m := enginetest.Manifest{
		Name: "widgets", Authority: widgetsPackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(widgetsPackage, 0),
			vocabulary.ActorManifest(widgetsPackage, vocabulary.PackageActor(widgetsPackage)),
			vocabulary.KindManifest(widgetsPackage, map[string]any{"singular": "widget"},
				map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
			vocabulary.FunctionManifest(widgetsPackage, "mirror", map[string]any{
				"description": "mirrors widgets into tasks",
				"runtime":     vocabulary.RuntimePython,
				"permissions": map[string]any{"writes": []any{"samples.substrate.reamde.dev/tasks/task"}},
				"source": `
def main(input, host):
    env = input["envelope"]
    return {"effects": [{"action": "put", "kind": "samples.substrate.reamde.dev/tasks/task",
                         "id": "t-" + env["change"]["id"],
                         "properties": {"name": env["record"]["properties"]["name"]}}]}
`,
			}),
		},
	}
	if withTrigger {
		m.Triggers = []enginetest.Trigger{{
			ID: "on-" + widgetsMirror,
			Properties: map[string]any{
				"enabled":  true,
				"source":   map[string]any{"record": map[string]any{"kinds": []any{widgetsWidget}, "ops": []any{"create", "update"}}},
				"callable": vocabulary.RecordPath("substrate.reamde.dev/core/function", widgetsMirror),
			},
		}}
	}
	return m
}

const (
	widgetsPackage = "widgets.test.dev/widgets"
	widgetsWidget  = widgetsPackage + "/widget"
	widgetsMirror  = widgetsPackage + "/mirror"
)
