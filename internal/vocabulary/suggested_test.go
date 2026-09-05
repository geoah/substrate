package vocabulary_test

import (
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// The three documents the classifier has to tell apart, all in one package's
// closure: a mapping onto its own kind from a foreign one (suggested, and
// prunable), a mapping that owns both ends (never pruned, it lands with the
// closure), and a mapping onto a FOREIGN kind, which is illegal (record 0049)
// and must reach the loader to be refused by name.
func suggestedFixture() []map[string]any {
	const owner = "ada.example.com/people"
	mapping := func(name string, data map[string]any) map[string]any {
		full := map[string]any{"authority": "ada.example.com", "package": "people"}
		for k, v := range data {
			full[k] = v
		}
		return map[string]any{
			"kind":     "substrate.reamde.dev/core/recordmapping",
			"metadata": map[string]any{"id": owner + "/" + name},
			"data":     full,
		}
	}
	return []map[string]any{
		{
			"kind":     "substrate.reamde.dev/core/bundle",
			"metadata": map[string]any{"id": owner},
			"data": map[string]any{
				"authority": "ada.example.com", "package": "people",
				"installs": []any{
					owner + "/person",
					owner + "/fromforeign",
					owner + "/ownsboth",
					owner + "/toforeign",
				},
			},
		},
		mapping("fromforeign", map[string]any{
			"from": "providers.substrate.reamde.dev/github/user",
			"to":   owner + "/person",
		}),
		mapping("ownsboth", map[string]any{
			"from": owner + "/contact",
			"to":   owner + "/person",
		}),
		mapping("toforeign", map[string]any{
			"from": "providers.substrate.reamde.dev/github/user",
			"to":   "other.example.com/people/person",
		}),
	}
}

func TestSuggestedMappingsAreForeignSourceAndOwnTarget(t *testing.T) {
	got := vocabulary.SuggestedMappings(suggestedFixture())
	if len(got) != 1 {
		var ids []string
		for _, sm := range got {
			ids = append(ids, sm.ID)
		}
		t.Fatalf("classified %v, want the foreign-source mapping alone", ids)
	}
	sm := got[0]
	if sm.ID != "ada.example.com/people/fromforeign" {
		t.Errorf("id = %q", sm.ID)
	}
	if sm.Package != "providers.substrate.reamde.dev/github" {
		t.Errorf("package = %q, want the package `from` lives in", sm.Package)
	}
	if sm.From != "providers.substrate.reamde.dev/github/user" || sm.To != "ada.example.com/people/person" {
		t.Errorf("ends = %q -> %q", sm.From, sm.To)
	}
	if sm.Data == nil {
		t.Error("the document's data is not carried, so nothing can check the mapping fits")
	}
}

// A mapping onto a kind the declaring package does NOT own is refused by the
// loader, so it must not be classified: pruning it would make an illegal
// declaration depend on whether its source package happens to be installed.
func TestAMappingOntoAForeignKindIsNotPrunable(t *testing.T) {
	for _, sm := range vocabulary.SuggestedMappings(suggestedFixture()) {
		if sm.To == "other.example.com/people/person" {
			t.Fatalf("%s is classified as suggested, so the door would drop it instead of refusing it", sm.ID)
		}
	}
	// And the loader is what refuses it: the document is still in the batch
	// after a prune of everything the classifier DID name.
	drop := map[string]bool{}
	for _, sm := range vocabulary.SuggestedMappings(suggestedFixture()) {
		drop[sm.ID] = true
	}
	kept := map[string]bool{}
	for _, d := range vocabulary.WithoutMappings(suggestedFixture(), drop) {
		if id, _ := mapPathString(d, "metadata", "id"); id != "" {
			kept[id] = true
		}
	}
	for _, id := range []string{"ada.example.com/people/toforeign", "ada.example.com/people/ownsboth"} {
		if !kept[id] {
			t.Errorf("%s was pruned; only a suggested mapping may be", id)
		}
	}
}

// The document AND its `installs:` entry go together: `installs:` is the
// package, both directions, so half a prune is a closure the loader refuses.
func TestWithoutMappingsPrunesTheDocumentAndItsInstallsEntry(t *testing.T) {
	docs := suggestedFixture()
	const dropped = "ada.example.com/people/fromforeign"
	out := vocabulary.WithoutMappings(docs, map[string]bool{dropped: true})
	for _, d := range out {
		if id, _ := mapPathString(d, "metadata", "id"); id == dropped {
			t.Fatalf("%s survived the prune", dropped)
		}
		if kind, _ := d["kind"].(string); kind != "substrate.reamde.dev/core/bundle" {
			continue
		}
		data, _ := d["data"].(map[string]any)
		installs, _ := data["installs"].([]any)
		for _, iv := range installs {
			if iv == dropped {
				t.Fatalf("installs still lists %s, so the closure would not balance", dropped)
			}
		}
		if len(installs) != 3 {
			t.Fatalf("installs = %v, want the other three", installs)
		}
	}
	// The ORIGINALS are untouched: the catalog holds one parsed copy of each
	// shipped closure and serves every repository from it.
	data, _ := docs[0]["data"].(map[string]any)
	if installs, _ := data["installs"].([]any); len(installs) != 4 {
		t.Fatalf("the prune mutated the shipped document: %v", installs)
	}
}

func mapPathString(m map[string]any, keys ...string) (string, bool) {
	var cur any = m
	for _, k := range keys {
		mm, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		cur = mm[k]
	}
	s, ok := cur.(string)
	return s, ok
}
