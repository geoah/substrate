package api

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// bundleplaceKindReads counts the registry reads that reach the engine: the
// count is what says the catalog answered, which no engine answer shows.
type bundleplaceKindReads struct {
	substrate.Dataset
	reads *int
}

func (d bundleplaceKindReads) Kinds(ctx context.Context) ([]substrate.KindInfo, error) {
	*d.reads++
	return d.Dataset.Kinds(ctx)
}

// A bundle status says what the package is for (decision record 0238), so
// the counts beside Providers never read the catalog: the catalog's answer
// for what the binary ships, and the stored declarations for the rest, read
// once however many packages come from outside the binary.
func TestBundleStatusesSayWhatEachPackageIsFor(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t)
	cat, err := catalog.Load(catalog.Shipped()...)
	if err != nil {
		t.Fatal(err)
	}
	crm, notes := env.repository+"/crm", env.repository+"/notes"
	if _, err := env.ds.ApplyVocabularyDocuments(t.Context(), substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(crm, 0),
		vocabulary.KindManifest(crm, map[string]any{"singular": "account"}, map[string]any{
			"displayTemplate": "{label}",
			"traits":          []any{vocabulary.TraitAccountConfigCore},
			"properties": map[string]any{
				"provider":      map[string]any{"type": "string"},
				"label":         map[string]any{"type": "string"},
				"status":        map[string]any{"type": "string"},
				"tokenRef":      map[string]any{"type": "secret"},
				"tokenStatus":   map[string]any{"type": "string"},
				"grantedScopes": map[string]any{"type": "string"},
			},
		}),
		vocabulary.PackageManifest(notes, 0),
		vocabulary.KindManifest(notes, map[string]any{"singular": "note"}, map[string]any{
			"properties": map[string]any{"text": map[string]any{"type": "string"}},
		}),
	}); err != nil {
		t.Fatalf("declare the two packages: %v", err)
	}
	h := &handler{catalog: cat}
	var reads int
	ds := bundleplaceKindReads{Dataset: env.ds, reads: &reads}
	// A status names the two packages as the engine answers them, under `~`
	// (decision record 0259).
	statuses := []substrate.BundleStatus{
		{ID: "providers.substrate.reamde.dev/google"},
		{ID: "providers.substrate.reamde.dev/slacklive"},
		{ID: "substrate.reamde.dev/clinical"},
		{ID: "~/crm"},
		{ID: "~/notes"},
	}
	if err := h.placeBundles(t.Context(), ds, statuses); err != nil {
		t.Fatal(err)
	}
	want := []struct {
		provider bool
		addonFor string
	}{
		{provider: true},
		{provider: true, addonFor: "providers.substrate.reamde.dev/slack"},
		{},
		{provider: true},
		{},
	}
	for i, st := range statuses {
		if st.Provider != want[i].provider || st.AddonFor != want[i].addonFor {
			t.Errorf("%s: provider %v, addonFor %q; want %v, %q", st.ID, st.Provider, st.AddonFor, want[i].provider, want[i].addonFor)
		}
	}
	if reads != 1 {
		t.Errorf("the registry was read %d times for two packages from outside the binary, want once", reads)
	}

	shipped := []substrate.BundleStatus{{ID: "providers.substrate.reamde.dev/google"}}
	reads = 0
	if err := h.placeBundles(t.Context(), ds, shipped); err != nil {
		t.Fatal(err)
	}
	if reads != 0 {
		t.Error("the registry was read for a package the binary ships")
	}
}
