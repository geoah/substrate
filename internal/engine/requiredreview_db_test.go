package engine_test

// The three holes a review of the `required` work found, each reproduced here
// before it was closed:
//
//   - admission counted a MISSING key while the write path also refuses an
//     empty one, so `required` could land on a kind whose rows already held ""
//     and lock every later write to them out;
//   - `required` inside an object's declared fields was declarable and
//     unenforced, on a shipped kind (`agent.tools[].function`) among others;
//   - `unlink` is the verb that clears an edge, and it took no notice of a
//     required one.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const reviewAuthority = "reqreview.example.substrate.reamde.dev"

// reviewDocs declares one kind whose `name` is optional or required as the
// caller asks, so the same store can be walked across the narrowing.
func reviewDocs(required bool) []map[string]any {
	name := map[string]any{"type": "string"}
	if required {
		name["required"] = true
	}
	return []map[string]any{
		vocabulary.AuthorityManifest(reviewAuthority, 0),
		vocabulary.KindManifest(reviewAuthority,
			map[string]any{"singular": "note", "plural": "notes"},
			map[string]any{"properties": map[string]any{
				"name": name,
				"tags": map[string]any{"type": "string", "repeated": true},
			}}),
	}
}

// Admission and the write path must draw the same line under "has a value". A
// row holding "" satisfies `props ? 'name'` and fails `required`, so counting
// the missing KEY alone admits a narrowing that strands the very rows the guard
// exists to protect: every later write to them is refused, with no way to
// migrate them under the declaration that refuses them.
func TestAddingRequiredCountsAnEmptyValueAsMissing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, reviewDocs(false)); err != nil {
		t.Fatalf("apply the optional declaration: %v", err)
	}
	for id, props := range map[string]map[string]any{
		"empty":   {"name": ""},
		"missing": {"tags": []any{"a"}},
		"list":    {"name": "held", "tags": []any{}},
	} {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: reviewAuthority + "/note", ID: id, Properties: props,
		})
	}

	_, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, reviewDocs(true))
	if err == nil {
		t.Fatal("adding `required` must be refused while live records hold no value for it")
	}
	// Two records lack a value: the one with no key, and the one with "".
	if !strings.Contains(err.Error(), "2 live records") {
		t.Fatalf("the guard must count the empty value as missing, got %v", err)
	}
}

// A required FIELD is part of the object's declared shape, and an object value
// is written whole, so the value the write stores is what has to satisfy it.
func TestRequiredObjectFieldIsEnforced(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	docs := []map[string]any{
		vocabulary.AuthorityManifest(reviewAuthority+".fields", 0),
		vocabulary.KindManifest(reviewAuthority+".fields",
			map[string]any{"singular": "profile", "plural": "profiles"},
			map[string]any{"properties": map[string]any{
				"contact": map[string]any{"type": "object", "fields": map[string]any{
					"email": map[string]any{"type": "email", "required": true},
					"label": map[string]any{"type": "string"},
				}},
				"seen": map[string]any{"type": "object", "repeated": true, "fields": map[string]any{
					"stamp": map[string]any{"type": "datetime", "required": true},
				}},
			}}),
	}
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, docs); err != nil {
		t.Fatalf("apply the field declaration: %v", err)
	}
	kind := reviewAuthority + ".fields/profile"

	for name, tc := range map[string]struct {
		props map[string]any
		want  string
	}{
		"an empty object":        {map[string]any{"contact": map[string]any{}}, "props.contact: .email"},
		"the field nulled":       {map[string]any{"contact": map[string]any{"email": nil, "label": "home"}}, "props.contact: .email"},
		"a sibling only":         {map[string]any{"contact": map[string]any{"label": "home"}}, "props.contact: .email"},
		"an item of a list":      {map[string]any{"seen": []any{map[string]any{}}}, "props.seen: [0]: .stamp"},
		"the second item of one": {map[string]any{"seen": []any{map[string]any{"stamp": "2026-08-17T00:00:00Z"}, map[string]any{}}}, "props.seen: [1]: .stamp"},
	} {
		_, err := ds.Put(ctx, owner, substrate.PutInput{Kind: kind, ID: "p1", Properties: tc.props})
		if err == nil {
			t.Fatalf("%s: a required field must be refused when the written object lacks it", name)
		}
		wantProblem(t, err, tc.want)
	}

	// The whole object, satisfied, still writes.
	mustPut(t, ds, owner, substrate.PutInput{Kind: kind, ID: "p1", Properties: map[string]any{
		"contact": map[string]any{"email": "ada@example.com"},
		"seen":    []any{map[string]any{"stamp": "2026-08-17T00:00:00Z"}},
	}})
}

// Unlink is the verb that clears an edge, so it is where a required edge has to
// be defended: birth-only enforcement leaves the record one call away from
// standing without the edge its declaration requires.
func TestUnlinkRefusesToClearARequiredEdge(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	docs := []map[string]any{
		vocabulary.AuthorityManifest(reviewAuthority+".edges", 0),
		vocabulary.KindManifest(reviewAuthority+".edges",
			map[string]any{"singular": "owner", "plural": "owners"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		vocabulary.KindManifest(reviewAuthority+".edges",
			map[string]any{"singular": "asset", "plural": "assets"},
			map[string]any{
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
				"edges": map[string]any{
					"holder": map[string]any{"to": reviewAuthority + ".edges/owner", "required": true},
					"seenBy": map[string]any{"to": reviewAuthority + ".edges/owner", "many": true},
				},
			}),
	}
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, docs); err != nil {
		t.Fatalf("apply the edge declaration: %v", err)
	}
	ownerKind, assetKind := reviewAuthority+".edges/owner", reviewAuthority+".edges/asset"
	mustPut(t, ds, owner, substrate.PutInput{Kind: ownerKind, ID: "ada", Properties: map[string]any{"name": "Ada"}})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: assetKind, ID: "a1",
		Properties: map[string]any{"name": "press"},
		Edges: []substrate.EdgeInput{
			{Rel: "holder", To: substrate.EdgeRef{ID: "ada"}},
			{Rel: "seenBy", To: substrate.EdgeRef{ID: "ada"}},
		},
	})

	err := ds.Unlink(ctx, owner, assetKind, "a1", "holder", substrate.EdgeRef{ID: "ada"})
	if err == nil {
		t.Fatal("unlinking the last target of a required edge must be refused")
	}
	wantProblem(t, err, "holder")

	// An edge that is not required still unlinks.
	if err := ds.Unlink(ctx, owner, assetKind, "a1", "seenBy", substrate.EdgeRef{ID: "ada"}); err != nil {
		t.Fatalf("unlink an optional edge: %v", err)
	}
}

// Admission must refuse a FIELD turning required for the same reason it refuses
// a property turning required: the write path enforces it afterwards, so a
// declaration that lands over objects without a value for it leaves those
// records unwritable.
func TestAddingARequiredFieldIsRefusedWhileObjectsLackIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	authority := reviewAuthority + ".fieldguard"
	docs := func(required bool) []map[string]any {
		email := map[string]any{"type": "email"}
		if required {
			email["required"] = true
		}
		return []map[string]any{
			vocabulary.AuthorityManifest(authority, 0),
			vocabulary.KindManifest(authority,
				map[string]any{"singular": "profile", "plural": "profiles"},
				map[string]any{"properties": map[string]any{
					"contact": map[string]any{"type": "object", "fields": map[string]any{
						"email": email,
						"label": map[string]any{"type": "string"},
					}},
				}}),
		}
	}
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, docs(false)); err != nil {
		t.Fatalf("apply the optional field: %v", err)
	}
	kind := authority + "/profile"
	// One object without the field, one with it, and one record with no object
	// at all: only the first is stranded.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: kind, ID: "bare",
		Properties: map[string]any{"contact": map[string]any{"label": "home"}},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: kind, ID: "held",
		Properties: map[string]any{"contact": map[string]any{"email": "ada@example.com"}},
	})
	mustPut(t, ds, owner, substrate.PutInput{Kind: kind, ID: "none"})

	_, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, docs(true))
	if err == nil {
		t.Fatal("a field becoming required must be refused while objects lack a value for it")
	}
	if !strings.Contains(err.Error(), "1 live records") || !strings.Contains(err.Error(), "email") {
		t.Fatalf("the guard must count the one stranded object, got %v", err)
	}

	// With the stranded object backfilled, the same declaration lands.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: kind, ID: "bare",
		Properties: map[string]any{"contact": map[string]any{"email": "bare@example.com", "label": "home"}},
	})
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, docs(true)); err != nil {
		t.Fatalf("the narrowing must land once nothing is stranded: %v", err)
	}
}
