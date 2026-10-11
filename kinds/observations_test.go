package kinds_test

import (
	"slices"
	"testing"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/kinds"
	"github.com/geoah/substrate/samples"
)

func shippedSample(t *testing.T, pkg string) *catalog.Bundle {
	t.Helper()
	cat, err := catalog.Load(catalog.ProviderRoot(kinds.Bundles()), catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatalf("load the shipped catalog: %v", err)
	}
	b, ok := cat.ByID(samples.Authority + "/" + pkg)
	if !ok {
		t.Fatalf("no shipped sample %s", pkg)
	}
	return b
}

// THE FOUR BASE KIND NAMES ARE FROZEN (decision 0159). A sample's kind cannot
// be moved by the catalog's upgrade: renaming one after the first import is a
// repository migration in every repository that took it. A fifth kind is an
// addition, which this pin lets through; a rename or a removal fails here.
func TestObservationsSampleKindNamesAreFrozen(t *testing.T) {
	b := shippedSample(t, "observations")
	for _, name := range []string{"metric", "observation", "trace", "source"} {
		if !slices.Contains(b.Closure.Kinds, samples.Authority+"/observations/"+name) {
			t.Errorf("observations/%s is gone: the base kind names are frozen before the first import", name)
		}
	}
	reg, err := vocabulary.LoadFS(kinds.Seed())
	if err != nil {
		t.Fatalf("load the seed: %v", err)
	}
	if err := installClosure(reg, map[string]*catalog.Bundle{b.ID: b}, b, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	for name, variant := range map[string]string{"observation": vocabulary.ObservedSingle, "trace": vocabulary.ObservedPacked} {
		k, ok := reg.ByIdentity(samples.Authority + "/observations/" + name)
		if !ok {
			t.Fatalf("observations/%s did not install", name)
		}
		if bind, ok := k.Binding(vocabulary.TraitObservedCore); !ok || bind.Variant != variant {
			t.Errorf("observations/%s binds %+v, want %s(%s)", name, bind, vocabulary.TraitObservedCore, variant)
		}
	}
}

// Every domain sample requires the base with a floor (decisions 0070 and
// 0159), so an import of health into a repository holding an older base is
// refused rather than landing kinds that point at properties it lacks.
func TestHealthRequiresObservations(t *testing.T) {
	base := shippedSample(t, "observations")
	health := shippedSample(t, "health")
	if !slices.Contains(health.Requires, base.ID) {
		t.Fatalf("health requires %v, not %s", health.Requires, base.ID)
	}
	floor, ok := health.RequiresAtLeast[base.ID]
	switch {
	case !ok:
		t.Errorf("health requires %s with no requiresAtLeast floor", base.ID)
	case floor < 1 || floor > base.Version:
		t.Errorf("health's floor on %s is %d; the shipped base is version %d", base.ID, floor, base.Version)
	}
}

// The Apple Health adapter writes the health sample's kinds from the Apple
// Health provider's mirrors, so it requires both with a floor: an import into
// a repository missing either, or holding an older one, is refused rather
// than landing a function whose trigger and writes name kinds that are not
// there. The install follows its requires: provider, observations, health,
// then the adapter.
func TestHealthAppleRequiresHealthAndTheProvider(t *testing.T) {
	adapter := shippedSample(t, "healthapple")
	health := shippedSample(t, "health")
	cat, err := catalog.Load(catalog.ProviderRoot(kinds.Bundles()), catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatalf("load the shipped catalog: %v", err)
	}
	provider, ok := cat.ByID("providers.substrate.reamde.dev/applehealth")
	if !ok {
		t.Fatal("no shipped applehealth provider")
	}
	for _, req := range []*catalog.Bundle{health, provider} {
		if !slices.Contains(adapter.Requires, req.ID) {
			t.Errorf("healthapple requires %v, not %s", adapter.Requires, req.ID)
		}
		if floor, ok := adapter.RequiresAtLeast[req.ID]; !ok || floor < 1 || floor > req.Version {
			t.Errorf("healthapple's floor on %s is %d (set %v); the shipped one is version %d", req.ID, floor, ok, req.Version)
		}
	}
	byPackage := map[string]*catalog.Bundle{}
	for _, b := range cat.Bundles() {
		byPackage[b.ID] = b
	}
	reg, err := vocabulary.LoadFS(kinds.Seed())
	if err != nil {
		t.Fatalf("load the seed: %v", err)
	}
	if err := installClosure(reg, byPackage, adapter, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.ResolveFunction(adapter.ID + "/healthappleadapt"); err != nil {
		t.Fatalf("the adapter's function did not install: %v", err)
	}
}

// THE GARMIN ADAPTER INSTALLS AFTER WHAT IT READS AND WRITES. It requires the
// garmin provider, whose mirror kinds its trigger watches, and the health
// sample with its observations base, whose kinds it writes, each with a floor;
// installed in `requires:` order on the seed alone, the whole chain admits.
func TestHealthGarminRequiresHealthAndTheProvider(t *testing.T) {
	cat, err := catalog.Load(catalog.ProviderRoot(kinds.Bundles()), catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatalf("load the shipped catalog: %v", err)
	}
	adapter := shippedSample(t, "healthgarmin")
	byPackage := map[string]*catalog.Bundle{}
	for _, b := range cat.Bundles() {
		byPackage[b.ID] = b
	}
	for _, req := range []string{samples.Authority + "/observations", samples.Authority + "/health", "providers.substrate.reamde.dev/garmin"} {
		b, ok := byPackage[req]
		if !ok {
			t.Fatalf("no shipped bundle %s", req)
		}
		if !slices.Contains(adapter.Requires, req) {
			t.Errorf("healthgarmin requires %v, not %s", adapter.Requires, req)
		}
		if floor, ok := adapter.RequiresAtLeast[req]; !ok || floor < 1 || floor > b.Version {
			t.Errorf("healthgarmin's floor on %s is %d (set %v); the shipped one is version %d", req, floor, ok, b.Version)
		}
	}
	reg, err := vocabulary.LoadFS(kinds.Seed())
	if err != nil {
		t.Fatalf("load the seed: %v", err)
	}
	if err := installClosure(reg, byPackage, adapter, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.ResolveFunction(samples.Authority + "/healthgarmin/garminhealth"); err != nil {
		t.Errorf("the adapter function did not install: %v", err)
	}
}
