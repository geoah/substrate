package engine_test

// THE FLOOR BINDS A COPY (decision record 0100). A bundle's `requiresAtLeast`
// is compared against a copy of the required package and dropped against a
// package this repository declared by hand under its own authority: the
// number on the user's own package is unrelated to the sample's, and the
// closure's pins are what admission holds against the kinds it declares.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// rfPackage renders a one-kind package at version 1: `<pkg>/<kind>` with one
// string property.
func rfPackage(pkg, kind string) []map[string]any {
	return []map[string]any{
		vocabulary.PackageManifest(pkg, 1),
		vocabulary.KindManifest(pkg, map[string]any{"singular": kind}, map[string]any{
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
		}),
	}
}

// rfRequiring renders a bundle package `<pkg>` whose one kind pins
// `<required>/<kind>` and whose bundle requires `required` at `floor` or later.
func rfRequiring(pkg, required, kind string, floor int) []map[string]any {
	return []map[string]any{
		vocabulary.PackageManifest(pkg, 1),
		vocabulary.BundleManifest(pkg, map[string]any{
			"description":     "one kind pinning the required package's",
			"requires":        []any{required},
			"requiresAtLeast": map[string]any{required: floor},
			"installs":        []any{pkg + "/event"},
		}),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "event"}, map[string]any{
			"properties": map[string]any{
				"attendee": map[string]any{"type": "reference", "kind": required + "/" + kind},
			},
		}),
	}
}

func TestARequiresFloorBindsACopyAndNotAHandDeclaredPackage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newCoreDataset(t)
	home := testdb.Repository(t)

	// The repository's own people, declared by hand at version 1.
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, rfPackage(home+"/people", "person")); err != nil {
		t.Fatalf("declare the repository's own people: %v", err)
	}
	// A closure declaring against people at 4 or later lands: the floor is
	// dropped, the requirement is met by name, and the pin resolves.
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, rfRequiring(home+"/calendar", home+"/people", "person", 4)); err != nil {
		t.Fatalf("a floor against a hand-declared package refused the closure: %v", err)
	}
	row, err := ds.Get(ctx, "substrate.reamde.dev/core/bundle", home+"/calendar")
	if err != nil {
		t.Fatalf("read the landed bundle: %v", err)
	}
	if _, has := row.Properties["requiresAtLeast"]; has {
		t.Errorf("the landed bundle still carries a floor: %v", row.Properties["requiresAtLeast"])
	}
	if got, _ := row.Properties["requires"].([]any); len(got) != 1 {
		t.Errorf("the landed bundle's requires = %v, want the one package", row.Properties["requires"])
	}
	// The preview agrees with the door.
	plan, err := ds.PlanVocabularyApply(ctx, owner, rfRequiring(home+"/calendar", home+"/people", "person", 4))
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(plan.Blockers) != 0 {
		t.Errorf("the preview blocks what the door admitted: %v", plan.Blockers)
	}

	// A COPY below the floor is refused: the same package word, stamped with
	// an origin, at version 1.
	if _, err := ds.ApplyVocabularyDocumentsWith(ctx, owner, rfPackage(home+"/tools", "tool"),
		substrate.VocabularyApply{Origin: "samples.example.com/tools"}); err != nil {
		t.Fatalf("land a stamped copy: %v", err)
	}
	_, err = ds.ApplyVocabularyDocuments(ctx, owner, rfRequiring(home+"/workshop", home+"/tools", "tool", 4))
	if err == nil {
		t.Fatal("a floor against a copy below it admitted the closure")
	}
	if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "version 4 or later") || !strings.Contains(err.Error(), "holds version 1") {
		t.Fatalf("the refusal does not name both versions: %v", err)
	}

	// A package under ANOTHER authority is not the repository's own, so its
	// floor stands.
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, rfPackage("other.example.com/things", "thing")); err != nil {
		t.Fatalf("declare a package under another authority: %v", err)
	}
	_, err = ds.ApplyVocabularyDocuments(ctx, owner, rfRequiring(home+"/uses", "other.example.com/things", "thing", 4))
	if err == nil || !strings.Contains(err.Error(), "version 4 or later") {
		t.Fatalf("a floor against another authority's package: %v, want the version refusal", err)
	}

	// A package the batch itself declares keeps its floor: the batch's own
	// package document says the version, and it is compared.
	batch := append(rfPackage(home+"/shelves", "shelf"), rfRequiring(home+"/library", home+"/shelves", "shelf", 4)...)
	_, err = ds.ApplyVocabularyDocuments(ctx, owner, batch)
	if err == nil || !strings.Contains(err.Error(), "version 4 or later") {
		t.Fatalf("a floor against a package the same batch declares at 1: %v, want the version refusal", err)
	}
}
