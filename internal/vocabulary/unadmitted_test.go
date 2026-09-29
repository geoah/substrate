package vocabulary_test

import (
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// UnadmittedKinds hands back a package that did not admit as it parsed, with
// the subject slot its own mappings synthesize on their source kinds: its own
// kind, and a kind of the registry, which comes back reshaped while the
// registry keeps its own declaration.
func TestUnadmittedKindsCarryTheirSubjectSlots(t *testing.T) {
	r := loadVocab(t)
	const pkg = "parked.example.com/parked"
	const target = pkg + "/card"
	const live = "vocab.example.com/vocab/note"
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
		vocabulary.MappingManifest(pkg, "notecard", map[string]any{
			"from": live, "to": target, "property": "card",
		}),
	)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := r.Install(g); err == nil {
		t.Fatal("a package pinning a kind nobody declares admitted")
	}
	liveNote, _ := r.ByIdentity(live)

	subjectSlot := func(ty *vocabulary.Kind) bool {
		slot, ok := ty.Props["card"]
		return ok && slot.Datatype == vocabulary.DatatypeReference && slot.Subject && slot.To == target
	}
	kinds, reshaped := r.UnadmittedKinds([]*vocabulary.Package{g})
	src, ok := kinds[pkg+"/source"]
	if _, hasCard := kinds[target]; !ok || !hasCard || len(kinds) != 2 {
		t.Fatalf("unadmitted kinds = %v, want the package's two kinds", kinds)
	}
	if _, ok := src.Props["ghost"]; !ok {
		t.Fatal("the unadmitted kind lost the property that refused it")
	}
	if !subjectSlot(src) {
		t.Fatalf("the unadmitted kind carries subject slot %+v, want a subject reference at %s", src.Props["card"], target)
	}
	note, ok := reshaped[live]
	if !ok || len(reshaped) != 1 || !subjectSlot(note) {
		t.Fatalf("reshaped = %v, want %s with the slot the package's mapping gives it", reshaped, live)
	}
	if _, ok := r.ByIdentity(pkg + "/source"); ok {
		t.Fatal("UnadmittedKinds installed the package into the registry")
	}
	if got, _ := r.ByIdentity(live); got != liveNote || subjectSlot(got) {
		t.Fatal("UnadmittedKinds changed a kind of the registry")
	}
	if kinds, reshaped := r.UnadmittedKinds(nil); kinds != nil || reshaped != nil {
		t.Fatal("no packages must give no kinds")
	}
}
