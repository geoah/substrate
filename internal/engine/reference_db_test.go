package engine_test

// References as first-class pointers: filterable, renderable in a display
// template, and enforced when required.
//
// The filter is the load-bearing one. A reference stores as a {kind, id}
// OBJECT, so the generic text comparison every other property filters through
// (`props->>'x'` against the value's spelling) could never match one — the
// grammar accepted the filter and returned nothing. It filters by CONTAINMENT
// now, which is also the one jsonb operator `records_props_idx` indexes, so
// asking "which rows point at this record" is index-backed for every reference
// of every kind with no per-kind declaration at all.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const firstClassAuthority = "firstclass.example.substrate.reamde.dev"

// refVocabulary declares a target kind and two pointers at it: one single,
// one repeated, plus a display template that reads THROUGH the pointer.
func firstClassVocabulary(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	docs := []map[string]any{
		vocabulary.AuthorityManifest(firstClassAuthority, ""),
		vocabulary.KindManifest(firstClassAuthority,
			map[string]any{"singular": "target", "plural": "targets"},
			map[string]any{
				"displayTemplate": "{name}",
				"properties":      map[string]any{"name": map[string]any{"type": "string"}},
			}),
		vocabulary.KindManifest(firstClassAuthority,
			map[string]any{"singular": "pointer", "plural": "pointers"},
			map[string]any{
				// The template reads the referent's own property: without a
				// reference branch in the resolver this renders "" for the
				// whole token, and with no dotted form admitted at all there
				// would be no way to write it.
				"displayTemplate": "{target.name}: {note}",
				"properties": map[string]any{
					"note": map[string]any{"type": "string"},
					"target": map[string]any{
						"type": "reference", "kind": firstClassAuthority + "/target",
						"inverse":            "pointers",
						"inverseDescription": "the rows that name this target",
					},
					"alsoSeen": map[string]any{
						"type": "reference", "kind": firstClassAuthority + "/target",
						"repeated": true, "inverse": "seenBy",
					},
				},
			}),
		// A BARE reference token: renders the referent's title, and the id it
		// holds when there is no referent to read.
		vocabulary.KindManifest(firstClassAuthority,
			map[string]any{"singular": "tag", "plural": "tags"},
			map[string]any{
				"displayTemplate": "{target}",
				"properties": map[string]any{
					"target": map[string]any{
						"type": "reference", "kind": firstClassAuthority + "/target",
					},
				},
			}),
		vocabulary.KindManifest(firstClassAuthority,
			map[string]any{"singular": "roster", "plural": "rosters"},
			map[string]any{
				"properties": map[string]any{
					"targets": map[string]any{
						"type": "reference", "kind": firstClassAuthority + "/target",
						"repeated": true, "required": true,
					},
				},
			}),
		vocabulary.KindManifest(firstClassAuthority,
			map[string]any{"singular": "strict", "plural": "stricts"},
			map[string]any{
				"properties": map[string]any{
					"target": map[string]any{
						"type": "reference", "kind": firstClassAuthority + "/target",
						"required": true,
					},
				},
			}),
	}
	if _, err := applier(t, ds).ApplyVocabularyDocuments(context.Background(), owner, docs); err != nil {
		t.Fatalf("install the reference vocabulary: %v", err)
	}
}

func TestReferenceFilterFindsWhatPointsAtARecord(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	firstClassVocabulary(t, ds)

	a := mustPut(t, ds, owner, substrate.PutInput{
		Kind: firstClassAuthority + "/target", ID: "a",
		Properties: map[string]any{"name": "Ada"},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: firstClassAuthority + "/target", ID: "b",
		Properties: map[string]any{"name": "Bea"},
	})
	// A bare id is admitted where a write admits one: `to:` pins the kind.
	at := mustPut(t, ds, owner, substrate.PutInput{
		Kind: firstClassAuthority + "/pointer", ID: "p1",
		Properties: map[string]any{"note": "first", "target": a.ID},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: firstClassAuthority + "/pointer", ID: "p2",
		Properties: map[string]any{"note": "second", "target": "b"},
	})

	list := func(value any) []string {
		t.Helper()
		page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{
			Kinds:      []string{firstClassAuthority + "/pointer"},
			Properties: map[string]substrate.Cond{"target": {Eq: value}},
		}})
		if err != nil {
			t.Fatalf("list by reference: %v", err)
		}
		ids := make([]string, 0, len(page.Records))
		for _, r := range page.Records {
			ids = append(ids, r.ID)
		}
		return ids
	}

	if got := list("a"); len(got) != 1 || got[0] != at.ID {
		t.Fatalf("a bare id must find the rows pointing at it, got %v", got)
	}
	// The canonical PATH answers identically to the bare id the pin completes.
	if got := list(vocabulary.RecordPath(firstClassAuthority+"/target", "a")); len(got) != 1 {
		t.Fatalf("the full path must find the same row, got %v", got)
	}
	if got := list("b"); len(got) != 1 || got[0] != "p2" {
		t.Fatalf("the filter must not leak across referents, got %v", got)
	}
	if got := list("nobody"); len(got) != 0 {
		t.Fatalf("an unreferenced id must find nothing, got %v", got)
	}
}

func TestReferenceFilterReachesIntoARepeatedPointer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	firstClassVocabulary(t, ds)

	for _, id := range []string{"a", "b", "c"} {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: firstClassAuthority + "/target", ID: id,
			Properties: map[string]any{"name": id},
		})
	}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: firstClassAuthority + "/pointer", ID: "p1",
		Properties: map[string]any{"target": "a", "alsoSeen": []any{"b", "c"}},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: firstClassAuthority + "/pointer", ID: "p2",
		Properties: map[string]any{"target": "a", "alsoSeen": []any{"c"}},
	})

	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Kinds:      []string{firstClassAuthority + "/pointer"},
		Properties: map[string]substrate.Cond{"alsoSeen": {Eq: "b"}},
	}})
	if err != nil {
		t.Fatalf("list by repeated reference: %v", err)
	}
	if len(page.Records) != 1 || page.Records[0].ID != "p1" {
		t.Fatalf("containment must reach inside the list, got %d rows", len(page.Records))
	}
}

func TestReferenceFilterMembershipAndPresence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	firstClassVocabulary(t, ds)

	for _, id := range []string{"a", "b"} {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: firstClassAuthority + "/target", ID: id,
			Properties: map[string]any{"name": id},
		})
	}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: firstClassAuthority + "/pointer", ID: "p1", Properties: map[string]any{"target": "a"},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: firstClassAuthority + "/pointer", ID: "p2", Properties: map[string]any{"target": "b"},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: firstClassAuthority + "/pointer", ID: "p3", Properties: map[string]any{"note": "points nowhere"},
	})

	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Kinds:      []string{firstClassAuthority + "/pointer"},
		Properties: map[string]substrate.Cond{"target": {In: []any{"a", "b"}}},
	}})
	if err != nil {
		t.Fatalf("list by reference membership: %v", err)
	}
	if len(page.Records) != 2 {
		t.Fatalf("in must match either referent, got %d rows", len(page.Records))
	}

	no := false
	page, err = ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Kinds:      []string{firstClassAuthority + "/pointer"},
		Properties: map[string]substrate.Cond{"target": {Exists: &no}},
	}})
	if err != nil {
		t.Fatalf("list by reference absence: %v", err)
	}
	if len(page.Records) != 1 || page.Records[0].ID != "p3" {
		t.Fatalf("exists:false must find the row that points nowhere, got %d rows", len(page.Records))
	}
}

func TestReferenceFilterRefusesAnOrdering(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	firstClassVocabulary(t, ds)

	// A pointer names a record or it does not; comparing two of them orders
	// their spelling, not the things. Refused, rather than silently answering
	// with whatever the text comparison happened to do.
	_, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Kinds:      []string{firstClassAuthority + "/pointer"},
		Properties: map[string]substrate.Cond{"target": {Gt: "a"}},
	}})
	wantErr(t, err, substrate.ErrValidation, "reference")
	if !strings.Contains(err.Error(), "use eq or in") {
		t.Fatalf("the refusal must say what to use instead, got: %v", err)
	}
}

func TestReferenceRendersInADisplayTemplate(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	firstClassVocabulary(t, ds)

	mustPut(t, ds, owner, substrate.PutInput{
		Kind: firstClassAuthority + "/target", ID: "a",
		Properties: map[string]any{"name": "Ada"},
	})
	row := mustPut(t, ds, owner, substrate.PutInput{
		Kind: firstClassAuthority + "/pointer", ID: "p1",
		Properties: map[string]any{"note": "first", "target": "a"},
	})
	if row.Title != "Ada: first" {
		t.Fatalf("a template must read THROUGH a reference, got %q", row.Title)
	}

	// A BARE token renders the referent's title.
	tag := mustPut(t, ds, owner, substrate.PutInput{
		Kind: firstClassAuthority + "/tag", ID: "t1",
		Properties: map[string]any{"target": "a"},
	})
	if tag.Title != "Ada" {
		t.Fatalf("a bare reference token must render the referent's title, got %q", tag.Title)
	}

	// A reference may name a row that is not there — that is the whole
	// difference from an edge. A bare token then falls back to the id it
	// holds: a dangling pointer still NAMES something, and rendering nothing
	// would throw away the only identifier the row had.
	ghost := map[string]any{"kind": firstClassAuthority + "/target", "id": "ghost"}
	dangling := mustPut(t, ds, owner, substrate.PutInput{
		Kind: firstClassAuthority + "/tag", ID: "t2",
		Properties: map[string]any{"target": ghost},
	})
	if dangling.Title != "ghost" {
		t.Fatalf("a dangling reference must still name what it points at, got %q", dangling.Title)
	}

	// A DOTTED token over a dangling reference renders empty, and must: the
	// referent is not there to be asked, and answering with the id would claim
	// the id was its `name`.
	unread := mustPut(t, ds, owner, substrate.PutInput{
		Kind: firstClassAuthority + "/pointer", ID: "p2",
		Properties: map[string]any{"note": "second", "target": ghost},
	})
	if unread.Title != ": second" {
		t.Fatalf("a dotted token over a dangling reference must render empty, got %q", unread.Title)
	}
}

func TestRequiredReferenceIsEnforcedAtBirth(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	firstClassVocabulary(t, ds)

	// `required` on an EDGE has always been asserted at birth. A pointer that
	// moves from an edge to a reference must not quietly turn that into a
	// suggestion.
	_, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: firstClassAuthority + "/strict", ID: "s1",
	})
	wantErr(t, err, substrate.ErrValidation, "requires reference target")

	mustPut(t, ds, owner, substrate.PutInput{
		Kind: firstClassAuthority + "/target", ID: "a", Properties: map[string]any{"name": "Ada"},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: firstClassAuthority + "/strict", ID: "s1", Properties: map[string]any{"target": "a"},
	})
}

func TestReferenceFilterAsksEachKindInItsOwnShape(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	firstClassVocabulary(t, ds)

	mustPut(t, ds, owner, substrate.PutInput{
		Kind: firstClassAuthority + "/target", ID: "a", Properties: map[string]any{"name": "Ada"},
	})
	// `target` is declared TWICE across these kinds: scalar on `pointer`,
	// scalar on `strict`. `alsoSeen` is the repeated one. A filter naming
	// several kinds must probe each in the shape that kind declares, or the
	// first declaration it happens to find answers for all of them.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: firstClassAuthority + "/pointer", ID: "p1",
		Properties: map[string]any{"target": "a", "alsoSeen": []any{"a"}},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: firstClassAuthority + "/strict", ID: "s1",
		Properties: map[string]any{"target": "a"},
	})

	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Kinds: []string{
			firstClassAuthority + "/pointer",
			firstClassAuthority + "/strict",
		},
		Properties: map[string]substrate.Cond{"target": {Eq: "a"}},
	}})
	if err != nil {
		t.Fatalf("list across kinds: %v", err)
	}
	if len(page.Records) != 2 {
		t.Fatalf("both kinds' rows must answer, got %d", len(page.Records))
	}
}

func TestRequiredRepeatedReferenceRefusesAnEmptyList(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	firstClassVocabulary(t, ds)

	// An empty list names nothing, so it is as absent as no key at all.
	_, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: firstClassAuthority + "/roster", ID: "s1",
		Properties: map[string]any{"targets": []any{}},
	})
	wantErr(t, err, substrate.ErrValidation, "requires reference targets")
}
