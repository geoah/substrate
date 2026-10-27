package catalog_test

// The first-party catalogues through the provider door (decision record
// 0192): an install brings every published package it requires, a package
// that declares no kinds lands its records, and a shipped trigger never takes
// an id another package's callable holds.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	applehealthID = "providers.substrate.reamde.dev/applehealth"
	environmentID = "substrate.reamde.dev/environment"
)

// requirePublishedInstall fails unless pkg is installed in ds as a published
// package at the version the catalog ships, its kind published with it.
func requirePublishedInstall(t *testing.T, c *catalog.Catalog, ds substrate.Dataset, pkg, kind string) {
	t.Helper()
	b, ok := c.ByID(pkg)
	if !ok {
		t.Fatalf("the shipped catalog does not serve %s", pkg)
	}
	if got := sourceOf(t, ds, kindPackageRef, pkg); got != vocabulary.SourcePublished {
		t.Errorf("%s package source = %q, want %q", pkg, got, vocabulary.SourcePublished)
	}
	if kind != "" {
		if got := sourceOf(t, ds, kindKindRef, kind); got != vocabulary.SourcePublished {
			t.Errorf("%s source = %q, want %q", kind, got, vocabulary.SourcePublished)
		}
	}
	st, err := ds.(bundleStatuser).BundleStatus(context.Background(), pkg)
	if err != nil {
		t.Fatalf("bundle status %s: %v", pkg, err)
	}
	if !st.Installed || st.ShippedVersion != b.Version {
		t.Errorf("%s status: installed %v, shippedVersion %d; want installed at the shipped %d", pkg, st.Installed, st.ShippedVersion, b.Version)
	}
}

// Installing a provider on a repository without the health catalogue its
// adapter writes installs health first, published and stamped as the
// catalog's own install would leave it, metric records included; and labs,
// a catalogue that requires health, brings it the same way.
func TestAnInstallBringsThePublishedPackagesItRequires(t *testing.T) {
	ctx := context.Background()
	c := loadCatalog(t)

	t.Run("applehealth", func(t *testing.T) {
		ds := newDataset(t)
		if _, err := ds.KindByRef(ctx, healthID+"/sleep"); !errors.Is(err, substrate.ErrNotFound) {
			t.Fatalf("a fresh repository already declares health: %v", err)
		}
		if _, _, err := c.Install(ctx, substrate.ActorAPI, applehealthID, ds); err != nil {
			t.Fatalf("install %s without health: %v", applehealthID, err)
		}
		requirePublishedInstall(t, c, ds, healthID, healthID+"/sleep")
		requirePublishedInstall(t, c, ds, applehealthID, applehealthID+"/metricbatch")
		if _, err := ds.Get(ctx, metricKind, "health/heart_rate"); err != nil {
			t.Errorf("the health metric records did not land with it: %v", err)
		}
	})

	t.Run("labs", func(t *testing.T) {
		ds := newDataset(t)
		if _, _, err := c.Install(ctx, substrate.ActorAPI, labsID, ds); err != nil {
			t.Fatalf("install %s without health: %v", labsID, err)
		}
		requirePublishedInstall(t, c, ds, healthID, healthID+"/workout")
		requirePublishedInstall(t, c, ds, labsID, labsID+"/labresult")
		// Taking labs again finds health in place and changes nothing.
		head, err := ds.Head(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := c.Install(ctx, substrate.ActorAPI, labsID, ds); err != nil {
			t.Fatalf("re-install %s: %v", labsID, err)
		}
		if after, err := ds.Head(ctx); err != nil || after != head {
			t.Errorf("re-installing labs moved the head %d -> %d (%v)", head.Seq, after.Seq, err)
		}
	})
}

// A catalogue that declares no kinds, only metric records, installs with an
// empty `installs:` and lands its records under the seeded metric kind.
func TestAPackageThatDeclaresNoKindsInstallsItsRecords(t *testing.T) {
	ctx := context.Background()
	ds := newDataset(t)
	c := loadCatalog(t)
	if _, _, err := c.Install(ctx, substrate.ActorAPI, environmentID, ds); err != nil {
		t.Fatalf("install %s: %v", environmentID, err)
	}
	requirePublishedInstall(t, c, ds, environmentID, "")
	for _, id := range []string{
		"environment/air_temperature", "environment/relative_humidity", "environment/illuminance",
		"environment/atmospheric_pressure", "environment/energy", "environment/power",
	} {
		if _, err := ds.Get(ctx, metricKind, id); err != nil {
			t.Errorf("the %s metric did not land: %v", id, err)
		}
	}
	st, err := ds.(bundleStatuser).BundleStatus(ctx, environmentID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Kinds != 0 {
		t.Errorf("%s holds %d kinds, want none", environmentID, st.Kinds)
	}
}

// triggerTree is a provider tree of two packages under one authority, each
// shipping a kind, a function and a trigger, both triggers at one id.
func triggerTree(t *testing.T) *catalog.Catalog {
	t.Helper()
	files := fstest.MapFS{}
	for _, name := range []string{"alpha", "beta"} {
		pkg := triggerAuthority + "/" + name
		docs := []map[string]any{
			vocabulary.PackageManifest(pkg, 1),
			vocabulary.ActorManifest(pkg, vocabulary.PackageActor(pkg)),
			vocabulary.BundleManifest(pkg, map[string]any{
				"description": "a provider whose trigger id another package ships too",
				"installs":    []any{pkg + "/note", pkg + "/notify"},
			}),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "note"},
				map[string]any{"displayTemplate": "{name}", "properties": map[string]any{
					"name": map[string]any{"type": "string"},
				}}),
			vocabulary.FunctionManifest(pkg, "notify", map[string]any{
				"description": "answers a delivery and writes nothing",
				"runtime":     vocabulary.RuntimePython,
				"source":      "def main(input, host):\n    return {\"output\": {}}\n",
			}),
			{
				"kind":     "substrate.reamde.dev/core/trigger",
				"metadata": map[string]any{"id": sharedTrigger},
				"data": map[string]any{"properties": map[string]any{
					"enabled": true,
					"source": map[string]any{"record": map[string]any{
						"kinds": []any{pkg + "/note"}, "ops": []any{"create"},
					}},
					"callable": triggerCallable(name),
				}},
			},
		}
		var file strings.Builder
		for _, d := range docs {
			out, err := yaml.Marshal(d)
			if err != nil {
				t.Fatalf("render a manifest: %v", err)
			}
			file.WriteString("---\n")
			file.Write(out)
		}
		files[name+"/bundle.yaml"] = &fstest.MapFile{Data: []byte(file.String())}
	}
	c, err := catalog.Load(catalog.ProviderRoot(files))
	if err != nil {
		t.Fatalf("load the provider tree: %v", err)
	}
	if w := c.Warnings(); len(w) > 0 {
		t.Fatalf("the provider tree did not load: %v", w)
	}
	return c
}

const (
	triggerAuthority = "triggers.example.com"
	sharedTrigger    = "shared-on-note"
)

// triggerCallable is the callable the named package's shared trigger
// delivers to.
func triggerCallable(pkg string) string {
	return "substrate.reamde.dev/core/function/" + triggerAuthority + "/" + pkg + "/notify"
}

// A trigger id is one namespace across packages, and an install's put merges,
// so a second package shipping a trigger at an id the first one's callable
// holds would repoint the first package's delivery at its own function. The
// install is refused whole, naming the trigger, the callable and the package
// that holds it, and the first package's trigger still delivers to it; the
// package that holds the id takes its own closure again.
func TestAShippedTriggerCannotTakeAnotherPackagesTriggerID(t *testing.T) {
	ctx := context.Background()
	ds := newDataset(t)
	c := triggerTree(t)
	alpha, beta := triggerAuthority+"/alpha", triggerAuthority+"/beta"
	if _, _, err := c.Install(ctx, substrate.ActorAPI, alpha, ds); err != nil {
		t.Fatalf("install %s: %v", alpha, err)
	}

	_, _, err := c.Install(ctx, substrate.ActorAPI, beta, ds)
	if !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("installing %s over %s's trigger id: err = %v, want a conflict", beta, alpha, err)
	}
	for _, want := range []string{sharedTrigger, triggerCallable("alpha"), alpha} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %s: %v", want, err)
		}
	}
	if _, err := ds.KindByRef(ctx, beta+"/note"); !errors.Is(err, substrate.ErrNotFound) {
		t.Errorf("the refused install left %s/note declared: %v", beta, err)
	}
	trigger := func() string {
		t.Helper()
		row, err := ds.Get(ctx, "substrate.reamde.dev/core/trigger", sharedTrigger)
		if err != nil {
			t.Fatalf("read the trigger: %v", err)
		}
		// A callable is a reference, read back as `{ref: …}`.
		switch v := row.Properties["callable"].(type) {
		case string:
			return v
		case map[string]any:
			ref, _ := v["ref"].(string)
			return ref
		}
		return ""
	}
	if got := trigger(); got != triggerCallable("alpha") {
		t.Fatalf("the refused install repointed the trigger at %s", got)
	}

	if _, _, err := c.Install(ctx, substrate.ActorAPI, alpha, ds); err != nil {
		t.Fatalf("re-install %s, which holds the trigger: %v", alpha, err)
	}
	if got := trigger(); got != triggerCallable("alpha") {
		t.Errorf("the re-install left the trigger at %s", got)
	}
}
