package engine_test

// THE API MAINTAINS THE VERSION (issue #48). An incoming declaration's
// version is honored only when it moves past the stored one; absent, echoed
// or lower resolves server-side: a changed definition lands at stored+1, an
// unchanged one keeps its stored version, and a declaration that cannot pin
// a version of its own moves its authority forward instead.

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

func TestApplyMaintainsDeclarationVersions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	sa := applier(t, ds)

	nameOnly := map[string]any{"name": map[string]any{"type": "string"}}
	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(swPackage, 0),
		swTypeDoc("widget", "widgets", nameOnly),
	}); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	widget := func() int64 {
		ti, err := ds.KindByRef(ctx, swPackage+"/widget")
		if err != nil {
			t.Fatalf("read the kind: %v", err)
		}
		return ti.Version
	}
	if v := widget(); v != 1 {
		t.Fatalf("a first declaration is version 1, got %d", v)
	}

	// An unchanged re-apply keeps the stored version: `get | apply` is a no-op.
	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		swTypeDoc("widget", "widgets", nameOnly),
	}); err != nil {
		t.Fatalf("unchanged re-apply: %v", err)
	}
	if v := widget(); v != 1 {
		t.Fatalf("an unchanged re-apply moved the version to %d", v)
	}

	// A changed declaration with no version lands at stored+1.
	twoProps := map[string]any{
		"name": map[string]any{"type": "string"},
		"size": map[string]any{"type": "int"},
	}
	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		swTypeDoc("widget", "widgets", twoProps),
	}); err != nil {
		t.Fatalf("changed apply: %v", err)
	}
	if v := widget(); v != 2 {
		t.Fatalf("a changed declaration must land at stored+1 = 2, got %d", v)
	}

	// An ECHOED version (get -o yaml | apply -f, edited) is not a pin: the
	// change still increments.
	echoed := vocabulary.KindManifest(swPackage,
		map[string]any{"singular": "widget", "plural": "widgets"},
		map[string]any{"version": 2, "properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"size": map[string]any{"type": "int"},
			"hue":  map[string]any{"type": "string"},
		}})
	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, []map[string]any{echoed}); err != nil {
		t.Fatalf("echoed-version apply: %v", err)
	}
	if v := widget(); v != 3 {
		t.Fatalf("an echoed version must not pin: want 3, got %d", v)
	}

	// An explicit version PAST the stored one is honored as written.
	pinned := vocabulary.KindManifest(swPackage,
		map[string]any{"singular": "widget", "plural": "widgets"},
		map[string]any{"version": 10, "properties": twoProps})
	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, []map[string]any{pinned}); err != nil {
		t.Fatalf("pinned apply: %v", err)
	}
	if v := widget(); v != 10 {
		t.Fatalf("an explicit forward version must be honored: want 10, got %d", v)
	}

	// A changed FUNCTION cannot pin a version of its own, so its package
	// moves forward instead, even with no package document in the batch.
	packageVersion := func() int64 {
		row := mustGet(t, ds, "substrate.reamde.dev/core/package", swPackage)
		v, _ := vocabulary.VersionValue(row.Properties["version"])
		return v
	}
	fnBody := func(logLine string) map[string]any {
		return vocabulary.FunctionManifest(swPackage, "poke", map[string]any{
			"runtime": vocabulary.RuntimeGo,
			"source": `
import "substratefn.local/substratefn"

func Main(in *substratefn.Input, host *substratefn.Host) (*substratefn.Result, error) {
	host.Logf("` + logLine + `")
	return &substratefn.Result{}, nil
}
`,
			"description": "test function poke",
			"permissions": map[string]any{"writes": []any{swPackage + "/widget"}},
		})
	}
	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, []map[string]any{fnBody("one")}); err != nil {
		t.Fatalf("apply the function: %v", err)
	}
	before := packageVersion()
	if _, err := sa.ApplyVocabularyDocuments(ctx, owner, []map[string]any{fnBody("two")}); err != nil {
		t.Fatalf("change the function: %v", err)
	}
	if v := packageVersion(); v != before+1 {
		t.Fatalf("a changed function must move its authority %d -> %d, got %d", before, before+1, v)
	}
}
