package engine_test

// Codex regression round 2 (server). Each test pins one closed-file finding:
//   #2  keyset ordering is total under (type, id) across a cross-type walk
//   #3  the list→watch head is the FIRST page's head, carried through the walk
//   #11 dialect step 6 retypes/drops the identity-bearing side tables too
//   #12 refuse-narrowing catches reference-target and object-field narrowing

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// TestListKeysetTotalOrderAcrossTypes pins codex regress #2: a cross-type list
// whose two rows share an id AND an equal sort value must paginate through
// BOTH — the (type, id) tiebreak makes the keyset order strictly total, so a
// page boundary neither skips nor duplicates. With an id-only tiebreak the
// second page's seek (id > "dup") admitted nothing and one row was lost.
func TestListKeysetTotalOrderAcrossTypes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	// Two DIFFERENT types, the SAME id, the SAME title (the sort key).
	mustPut(t, ds, owner, substrate.PutInput{Kind: "person", ID: "dup", Properties: map[string]any{"title": "same"}})
	mustPut(t, ds, owner, substrate.PutInput{Kind: "organization", ID: "dup", Properties: map[string]any{"title": "same"}})

	q := substrate.Query{
		Filter:  substrate.Filter{Kinds: []string{"person", "organization"}},
		OrderBy: []substrate.Order{{Property: "title"}},
		First:   1,
	}
	seen := map[string]bool{}
	for range 5 {
		page, err := ds.List(ctx, q)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, e := range page.Records {
			key := e.Kind + "/" + e.ID
			if seen[key] {
				t.Fatalf("duplicate at a page boundary: %s", key)
			}
			seen[key] = true
		}
		if page.Cursor == "" {
			break
		}
		q.After = page.Cursor
	}
	if len(seen) != 2 {
		t.Fatalf("the paged cross-type walk saw %d of 2 records (a page boundary skipped one): %v", len(seen), seen)
	}
}

// TestListHeadIsFirstPageHeadAcrossWalk pins codex regress #3: every page of a
// cursor walk reports the FIRST page's changelog head, so the list→watch
// handoff pins the snapshot the walk began at. A write that lands mid-walk
// bumps the changelog head, but the later page must still report the original
// head — the watch replays that write, it is not lost.
func TestListHeadIsFirstPageHeadAcrossWalk(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	for range 5 {
		mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "p"}})
	}
	q := substrate.Query{Filter: substrate.Filter{Kinds: []string{"person"}}, First: 2}
	page1, err := ds.List(ctx, q)
	if err != nil {
		t.Fatalf("list page 1: %v", err)
	}
	if page1.Head == 0 || page1.Cursor == "" {
		t.Fatalf("page 1 head=%d cursor=%q — want a non-zero head and a continuation", page1.Head, page1.Cursor)
	}

	// A write lands BETWEEN the pages, bumping the changelog head.
	mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "mid-walk"}})

	q.After = page1.Cursor
	page2, err := ds.List(ctx, q)
	if err != nil {
		t.Fatalf("list page 2: %v", err)
	}
	if page2.Head != page1.Head {
		t.Fatalf("page 2 head = %d, want the first page's head %d — a mid-walk insert would be lost from the list→watch handoff", page2.Head, page1.Head)
	}
}

const evoRefAuthority = "evoref.example.substrate.reamde.dev"

// evoRefDocs builds the authority with two referent types and a holder carrying a
// reference and an object property whose fields drive the object-narrowing
// checks.
func evoRefDocs(holderProps map[string]any) []map[string]any {
	return []map[string]any{
		vocabulary.AuthorityManifest(evoRefAuthority, 0),
		vocabulary.KindManifest(evoRefAuthority, map[string]any{"singular": "widget", "plural": "widgets"}, map[string]any{}),
		vocabulary.KindManifest(evoRefAuthority, map[string]any{"singular": "gadget", "plural": "gadgets"}, map[string]any{}),
		vocabulary.KindManifest(evoRefAuthority, map[string]any{"singular": "holder", "plural": "holders"},
			map[string]any{"properties": holderProps}),
	}
}

func evoRefBaseProps() map[string]any {
	return map[string]any{
		"ref": map[string]any{"type": "reference", "kind": "any"},
		"spec": map[string]any{"type": "object", "fields": map[string]any{
			"a": map[string]any{"type": "string"},
			"b": map[string]any{"type": "int"},
		}},
	}
}

func evoRefApply(t *testing.T, ds substrate.Dataset, holderProps map[string]any) error {
	t.Helper()
	_, err := applier(t, ds).ApplyVocabularyDocuments(context.Background(), owner, evoRefDocs(holderProps))
	return err
}

// TestSchemaEvolutionRefusesReferenceAndObjectNarrowing pins codex regress #12:
// refuse-narrowing recurses into a reference's `to:` target and an object's
// fields, each refused with the stranded live-row count — not silently
// admitted as the outer-kind-only classifier did.
func TestSchemaEvolutionRefusesReferenceAndObjectNarrowing(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	if err := evoRefApply(t, ds, evoRefBaseProps()); err != nil {
		t.Fatalf("install base authority: %v", err)
	}
	// One holder: its reference points at widget, its object carries both fields.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: evoRefAuthority + "/holder",
		Properties: map[string]any{
			"ref":  vocabulary.RecordPath("widget", "w1"),
			"spec": map[string]any{"a": "x", "b": 5},
		},
	})

	t.Run("reference target narrowed", func(t *testing.T) {
		props := evoRefBaseProps()
		props["ref"] = map[string]any{"type": "reference", "kind": "gadget"}
		wantNarrowingGuard(t, evoRefApply(t, ds, props),
			`reference "ref" narrows its target to `+evoRefAuthority+`/gadget`, "1 live records")
	})

	t.Run("object field dropped", func(t *testing.T) {
		props := evoRefBaseProps()
		props["spec"] = map[string]any{"type": "object", "fields": map[string]any{
			"a": map[string]any{"type": "string"},
		}}
		wantNarrowingGuard(t, evoRefApply(t, ds, props),
			`object "spec" drops field "b"`, "1 live records")
	})

	t.Run("object field kind changed", func(t *testing.T) {
		props := evoRefBaseProps()
		props["spec"] = map[string]any{"type": "object", "fields": map[string]any{
			"a": map[string]any{"type": "string"},
			"b": map[string]any{"type": "string"},
		}}
		wantNarrowingGuard(t, evoRefApply(t, ds, props),
			`object "spec" field "b" changes kind int → string`, "1 live records")
	})
}
