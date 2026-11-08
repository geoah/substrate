package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/applet"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// preRuntimeTree is the shipped tree with the applets package as its first
// version declared it: no runtime, so every applet was A2UI screens.
func preRuntimeTree(t *testing.T) string {
	t.Helper()
	tree := shippedTree(t)
	path := filepath.Join(tree, vocabulary.PackageApplets, "applets.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := regexp.MustCompile(`(?m)^  version: \d+$`).ReplaceAllString(string(raw), "  version: 1")
	start := strings.Index(doc, "    runtime:\n")
	end := strings.Index(doc, "    description:\n      type: text")
	if start < 0 || end < start {
		t.Fatal("the shipped applet kind declares no runtime to take away")
	}
	if err := os.WriteFile(path, []byte(doc[:start]+doc[end:]), 0o600); err != nil {
		t.Fatal(err)
	}
	return tree
}

// An applet written before an applet named its runtime is A2UI screens, and
// the migration says so before the shipped upgrade that requires a runtime
// meets it, so the upgrade lands rather than being refused over it.
func TestRepositoryMigrationNamesTheRuntimeOfEveryApplet(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root := engine.MigratedDSN(t), t.TempDir()
	repo := testdb.Repository(t)

	svc := openTree(t, dsn, preRuntimeTree(t), engine.WithDataRoot(root))
	if _, err := svc.CreateRepository(ctx, repo); err != nil {
		t.Fatalf("create on binary N: %v", err)
	}
	ds, err := svc.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := planter(t, ds).PlantRecord(ctx, applet.Kind, "books", map[string]any{
		"name": "Books",
		"screens": map[string]any{"home": map[string]any{"title": "Books", "components": []any{
			map[string]any{"id": "root", "component": "Text", "text": "Books"},
		}}},
	}); err != nil {
		t.Fatalf("plant an applet as binary N stored it: %v", err)
	}
	raw, err := engine.OpenScopedDB(dsn, repo, engine.RoleApp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	rmClearLedger(t, dsn, repo)
	_ = svc.Close()

	// Binary N+1.
	svc = openTree(t, dsn, shippedTree(t), engine.WithDataRoot(root))
	ds, err = svc.Dataset(ctx, repo)
	if err != nil {
		t.Fatalf("open on binary N+1: %v", err)
	}
	if got := rmLedger(t, raw); got[4] != "name_applet_runtimes" {
		t.Fatalf("ledger = %v, want migration 4 recorded", got)
	}
	if got := mustGet(t, ds, applet.Kind, "books").Properties["runtime"]; got != applet.RuntimeA2UI {
		t.Fatalf("the applet's runtime = %v, want %s", got, applet.RuntimeA2UI)
	}
	kind, err := ds.KindByRef(ctx, applet.Kind)
	if err != nil {
		t.Fatal(err)
	}
	runtime, _ := kind.Definition["properties"].(map[string]any)["runtime"].(map[string]any)
	if runtime["required"] != true {
		t.Fatalf("the upgrade did not land: the applet kind's runtime is %v", runtime)
	}
	statuses, err := ds.BundleStatuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range statuses {
		if st.ID == vocabulary.PackageApplets && st.Quarantined {
			t.Fatalf("the applets package is quarantined: %s", st.QuarantineReason)
		}
	}
	// The applet renders on the runtime it now names.
	frame, err := (&applet.Runtime{Data: ds}).Render(ctx, mustGet(t, ds, applet.Kind, "books"), substrate.AppletRenderInput{})
	if err != nil || frame.Applet.Runtime != applet.RuntimeA2UI {
		t.Fatalf("render: %+v %v", frame, err)
	}
}
