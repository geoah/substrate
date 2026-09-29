package vocabulary_test

import (
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// UnadmittedKinds hands back a package that did not admit as it parsed, with
// the subject slot its own mapping synthesizes on its source kind, and leaves
// the registry it was asked of as it was.
func TestUnadmittedKindsCarryTheirSubjectSlots(t *testing.T) {
	r := loadVocab(t)
	const pkg = "parked.example.com/parked"
	const target = pkg + "/card"
	g, err := parseInstalled(
		vocabulary.PackageManifest(pkg, 1),
		vocabulary.KindManifest(pkg,
			map[string]any{"singular": "source"},
			map[string]any{"properties": map[string]any{
				"name":  map[string]any{"type": "string"},
				"ghost": map[string]any{"type": "reference", "kind": "missing.example.com/missing/ghost"},
			}}),
		vocabulary.KindManifest(pkg,
			map[string]any{"singular": "card"},
			map[string]any{"properties": map[string]any{
				"label": map[string]any{"type": "string"},
			}}),
		vocabulary.MappingManifest(pkg, "sourcecard", map[string]any{
			"from": pkg + "/source", "to": target, "property": "card",
			"map": map[string]any{"label": map[string]any{"path": "name"}},
		}),
	)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := r.Install(g); err == nil {
		t.Fatal("a package pinning a kind nobody declares admitted")
	}
	const live = "vocab.example.com/vocab/note"
	liveNote, _ := r.ByIdentity(live)

	kinds := r.UnadmittedKinds([]*vocabulary.Package{g})
	src, ok := kinds[pkg+"/source"]
	if _, hasCard := kinds[target]; !ok || !hasCard || len(kinds) != 2 {
		t.Fatalf("unadmitted kinds = %v, want the package's two kinds", kinds)
	}
	if _, ok := src.Props["ghost"]; !ok {
		t.Fatal("the unadmitted kind lost the property that refused it")
	}
	slot, ok := src.Props["card"]
	if !ok || slot.Datatype != vocabulary.DatatypeReference || !slot.Subject || slot.To != target {
		t.Fatalf("the unadmitted kind carries subject slot %+v, want a subject reference at %s", slot, target)
	}
	if _, ok := r.ByIdentity(pkg + "/source"); ok {
		t.Fatal("UnadmittedKinds installed the package into the registry")
	}
	if got, _ := r.ByIdentity(live); got != liveNote {
		t.Fatal("UnadmittedKinds changed a kind of the registry")
	}
	if r.UnadmittedKinds(nil) != nil {
		t.Fatal("no packages must give no kinds")
	}
}
