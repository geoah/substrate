package api

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
)

// kindsDataset answers a fixed registry: the stored declarations a package
// the binary does not ship is placed by.
type kindsDataset struct {
	*fakeDataset
	kinds []substrate.KindInfo
	reads int
}

func (d *kindsDataset) Kinds(context.Context) ([]substrate.KindInfo, error) {
	d.reads++
	return d.kinds, nil
}

// A bundle status says what the package is for (decision record 0238), so
// the counts beside Providers never read the catalog: the catalog's answer
// for what the binary ships, and the stored declarations for the rest.
func TestBundleStatusesSayWhatEachPackageIsFor(t *testing.T) {
	cat, err := catalog.Load(catalog.Shipped()...)
	if err != nil {
		t.Fatal(err)
	}
	h := &handler{catalog: cat}
	ds := &kindsDataset{fakeDataset: newFakeDataset("ada"), kinds: []substrate.KindInfo{
		{Identity: "example.com/crm/account", Authority: "example.com", Package: "crm", Definition: map[string]any{
			"traits": []any{"substrate.reamde.dev/core/accountconfig"},
		}},
		{Identity: "example.com/notes/note", Authority: "example.com", Package: "notes", Definition: map[string]any{}},
	}}
	statuses := []substrate.BundleStatus{
		{ID: "providers.substrate.reamde.dev/google"},
		{ID: "providers.substrate.reamde.dev/slacklive"},
		{ID: "substrate.reamde.dev/clinical"},
		{ID: "example.com/crm"},
		{ID: "example.com/notes"},
	}
	if err := h.placeBundles(context.Background(), ds, statuses); err != nil {
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
	if ds.reads != 1 {
		t.Errorf("the registry was read %d times for two packages from outside the binary, want once", ds.reads)
	}

	shipped := []substrate.BundleStatus{{ID: "providers.substrate.reamde.dev/google"}}
	ds.reads = 0
	if err := h.placeBundles(context.Background(), ds, shipped); err != nil {
		t.Fatal(err)
	}
	if ds.reads != 0 {
		t.Error("the registry was read for a package the binary ships")
	}
}
