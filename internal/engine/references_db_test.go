package engine_test

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const refAuthority = "refs.example.substrate.reamde.dev"

// refDocs installs a tiny authority for the reference property type:
// two target types (widget, gadget) and a holder carrying a pinned reference,
// an unconstrained one, and a repeated one.
func refDocs() []map[string]any {
	return []map[string]any{
		vocabulary.AuthorityManifest(refAuthority, ""),
		vocabulary.KindManifest(refAuthority,
			map[string]any{"singular": "widget", "plural": "widgets"},
			map[string]any{}),
		vocabulary.KindManifest(refAuthority,
			map[string]any{"singular": "gadget", "plural": "gadgets"},
			map[string]any{}),
		vocabulary.KindManifest(refAuthority,
			map[string]any{"singular": "holder", "plural": "holders"},
			map[string]any{"properties": map[string]any{
				"pin":    map[string]any{"type": "reference", "to": "widget"},
				"anyref": map[string]any{"type": "reference", "to": "any"},
				"pins":   map[string]any{"type": "reference", "to": "widget", "repeated": true},
			}}),
	}
}

func installRefAuthority(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	if _, err := applier(t, ds).ApplyVocabularyDocuments(context.Background(), owner, refDocs()); err != nil {
		t.Fatalf("install ref authority: %v", err)
	}
}

// asRef asserts a stored value is a canonical {authority, type, id} reference.
func asRef(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("reference did not read back as an object: %T %v", v, v)
	}
	for _, k := range []string{"kind", "id"} {
		if _, ok := m[k].(string); !ok {
			t.Fatalf("reference missing %q: %v", k, m)
		}
	}
	return m
}

// TestReferenceRoundTrip writes a reference from a bare {type, id} and reads
// back the canonical {authority, type, id}, then re-applies that canonical value —
// the get -o yaml → apply round trip. The referent record need not exist.
func TestReferenceRoundTrip(t *testing.T) {
	_, ds := newDataset(t)
	installRefAuthority(t, ds)

	// A pointer at a widget that does NOT exist yet — a reference is a pointer,
	// not an edge, so no existence gate refuses it.
	h := mustPut(t, ds, owner, substrate.PutInput{
		Kind:       refAuthority + "/holder",
		Properties: map[string]any{"pin": map[string]any{"kind": "widget", "id": "ghost"}},
	})
	got := mustGet(t, ds, h.Kind, h.ID)
	ref := asRef(t, got.Properties["pin"])
	if ref["kind"] != refAuthority+"/widget" || ref["id"] != "ghost" {
		t.Fatalf("pin = %v, want the bare name resolved to a kind reference", ref)
	}

	// Re-apply the canonical shape the read produced (apply round-trip).
	h2 := mustPut(t, ds, owner, substrate.PutInput{
		Kind:       refAuthority + "/holder",
		ID:         h.ID,
		Properties: map[string]any{"pin": got.Properties["pin"]},
	})
	if r2 := asRef(t, h2.Properties["pin"]); r2["id"] != "ghost" {
		t.Fatalf("round-trip pin = %v", r2)
	}
}

// TestReferenceUnknownTypeRefused refuses a reference whose referent type is
// not a known type.
func TestReferenceUnknownTypeRefused(t *testing.T) {
	_, ds := newDataset(t)
	installRefAuthority(t, ds)

	_, err := ds.Put(context.Background(), owner, substrate.PutInput{
		Kind:       refAuthority + "/holder",
		Properties: map[string]any{"anyref": map[string]any{"kind": "nosuch", "id": "x"}},
	})
	wantErr(t, err, substrate.ErrValidation, "unknown referent type")
}

// TestReferenceToMismatchRefused refuses a value whose type is not the pinned
// `to:` type.
func TestReferenceToMismatchRefused(t *testing.T) {
	_, ds := newDataset(t)
	installRefAuthority(t, ds)

	_, err := ds.Put(context.Background(), owner, substrate.PutInput{
		Kind:       refAuthority + "/holder",
		Properties: map[string]any{"pin": map[string]any{"kind": "gadget", "id": "g1"}},
	})
	wantErr(t, err, substrate.ErrValidation, "to mismatch")
}

// TestReferenceAnyNeedsType refuses a bare id on a `to: any` reference — there
// is no declaration to supply the type.
func TestReferenceAnyNeedsType(t *testing.T) {
	_, ds := newDataset(t)
	installRefAuthority(t, ds)

	_, err := ds.Put(context.Background(), owner, substrate.PutInput{
		Kind:       refAuthority + "/holder",
		Properties: map[string]any{"anyref": "barestring"},
	})
	wantErr(t, err, substrate.ErrValidation, "bare id on to:any")
}

// TestRepeatedReferences writes and reads a list of references.
func TestRepeatedReferences(t *testing.T) {
	_, ds := newDataset(t)
	installRefAuthority(t, ds)

	h := mustPut(t, ds, owner, substrate.PutInput{
		Kind: refAuthority + "/holder",
		Properties: map[string]any{"pins": []any{
			map[string]any{"kind": "widget", "id": "a"},
			map[string]any{"authority": refAuthority, "type": "widget", "id": "b"},
		}},
	})
	got := mustGet(t, ds, h.Kind, h.ID)
	list, ok := got.Properties["pins"].([]any)
	if !ok || len(list) != 2 {
		t.Fatalf("pins = %T %v", got.Properties["pins"], got.Properties["pins"])
	}
	for i, want := range []string{"a", "b"} {
		if r := asRef(t, list[i]); r["id"] != want || r["kind"] != refAuthority+"/widget" {
			t.Fatalf("pins[%d] = %v", i, r)
		}
	}
}
