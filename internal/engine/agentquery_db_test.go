package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A search that names no kind and no purpose skips internal kinds, so a wide
// grant does not answer with machinery, the loop's own transcript first among
// it: every message that repeated the words ranked beside the records they
// were about. Naming the kind or the purpose still reaches it.
func TestAgentSearchNamingNothingSkipsInternalKinds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _ := openAgentDataset(t)
	const pkg = "purpose.agent.example/shop"
	product, note := pkg+"/product", pkg+"/note"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(pkg, 1),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "product"}, map[string]any{
			"properties":      map[string]any{"name": map[string]any{"type": "string"}},
			"displayTemplate": "{name}",
		}),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "note"}, map[string]any{
			"purpose":         "internal",
			"properties":      map[string]any{"text": map[string]any{"type": "string"}},
			"displayTemplate": "{text}",
		}),
	}); err != nil {
		t.Fatalf("declare the kinds: %v", err)
	}
	for _, in := range []substrate.PutInput{
		{Kind: product, ID: "cup", Properties: map[string]any{"name": "Tajimi cup"}},
		{Kind: note, ID: "said", Properties: map[string]any{"text": "the user asked about the tajimi cup"}},
	} {
		if _, err := ds.Put(ctx, substrate.ActorAPI, in); err != nil {
			t.Fatal(err)
		}
	}
	scope := queryScope{kinds: []string{pkg + "/*"}, rows: 50}
	search := func(extra map[string]any) string {
		t.Helper()
		args := map[string]any{"q": "tajimi", "mode": "lexical"}
		for k, v := range extra {
			args[k] = v
		}
		out, ok, _ := ds.runQueryTool(ctx, scope, args)
		if !ok {
			t.Fatalf("search %v: %s", extra, out)
		}
		return out
	}

	named := search(nil)
	if !strings.Contains(named, `"id":"cup"`) || strings.Contains(named, `"id":"said"`) {
		t.Fatalf("a search naming nothing: want the product and not the note, got %s", named)
	}
	for name, extra := range map[string]map[string]any{
		"the kind":    {"filter": map[string]any{"kinds": []any{note}}},
		"the purpose": {"filter": map[string]any{"purposes": []any{"internal"}}},
	} {
		if out := search(extra); !strings.Contains(out, `"id":"said"`) {
			t.Fatalf("a search naming %s does not reach the note: %s", name, out)
		}
	}
}
