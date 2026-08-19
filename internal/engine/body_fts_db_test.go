package engine_test

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Body is a declared, column-backed text property (#68), and FTS follows the
// property's own `fts` flag: a declared body indexes as prose unless the
// declaration opts out, and a kind that declares no body carries none. Three
// kinds pin the three outcomes: an `article` with a default-fts body, a `memo`
// with `fts: false`, and a `tag` that declares no body at all.
func TestDeclaredBodyAndPerPropertyFTS(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	const authority = "reader.connectors.substrate.reamde.dev"
	if err := enginetest.Install(ctx, ds, substrate.ActorSystem, enginetest.Manifest{
		Name: "reader", Authority: authority,
		Manifests: []map[string]any{
			vocabulary.AuthorityManifest(authority, 1),
			vocabulary.ActorManifest(authority, "connector:reader"),
			// A declared body, fts defaulting true for text: indexed.
			vocabulary.KindManifest(authority,
				map[string]any{"singular": "article", "plural": "articles"},
				map[string]any{"properties": map[string]any{
					"body": map[string]any{"type": "text"},
				}}),
			// A declared body opting out of the index: stored and served, never
			// searched.
			vocabulary.KindManifest(authority,
				map[string]any{"singular": "memo", "plural": "memos"},
				map[string]any{"properties": map[string]any{
					"body": map[string]any{"type": "text", "fts": false},
				}}),
			// No body declared: a body write is refused like any undeclared name.
			vocabulary.KindManifest(authority,
				map[string]any{"singular": "tag", "plural": "tags"},
				map[string]any{"properties": map[string]any{
					"label": map[string]any{"type": "string"},
				}}),
		},
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	act := substrate.Actor("connector:reader")

	// (b) A default-fts body indexes: the article is found by a word only its
	// body holds.
	article := mustPut(t, ds, act, substrate.PutInput{
		Kind: authority + "/article", ID: "a1",
		Properties: map[string]any{"body": "the peregrine falcon dives"},
	})
	hits, err := ds.Search(ctx, substrate.SearchInput{
		Q: "peregrine", Mode: substrate.SearchLexical, Kinds: []string{authority + "/article"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Record.ID != article.ID {
		t.Fatalf("indexed body not found: %v", hitIDs(hits))
	}

	// (a) `fts: false` stores and serves the body but never indexes it.
	memo := mustPut(t, ds, act, substrate.PutInput{
		Kind: authority + "/memo", ID: "m1",
		Properties: map[string]any{"body": "the peregrine falcon dives"},
	})
	full := mustGet(t, ds, memo.Kind, memo.ID)
	if full.Body != "the peregrine falcon dives" || full.Properties["body"] != "the peregrine falcon dives" {
		t.Fatalf("fts:false body not served: body=%q props=%v", full.Body, full.Properties["body"])
	}
	hits, err = ds.Search(ctx, substrate.SearchInput{
		Q: "peregrine", Mode: substrate.SearchLexical, Kinds: []string{authority + "/memo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("fts:false body was indexed: %v", hitIDs(hits))
	}

	// (c) A kind that declares no body refuses a body write.
	if _, err := ds.Put(ctx, act, substrate.PutInput{
		Kind: authority + "/tag", ID: "t1",
		Properties: map[string]any{"label": "x", "body": "nope"},
	}); err == nil {
		t.Fatal("body on a kind that declares none must be refused")
	} else {
		wantErr(t, err, substrate.ErrValidation, "not declared")
	}
}
