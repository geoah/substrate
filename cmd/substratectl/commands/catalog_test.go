package commands

import (
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// The one guard line the fake's blocked upgrades carry: the shape the engine
// renders, naming the kind, the property and the count.
const labelGuard = `type substrate.reamde.dev/core/llmprovider: property "label" dropped while 1 live records still carry it — null it on them first`

// `substratectl catalog` is where a refused upgrade becomes readable from a
// terminal: core's, which no catalog entry carries, and an installed
// provider's, both with the version motion and every guard line. Before it
// the CLI printed no upgrade state at all.
func TestCatalogPrintsUpgradesAndBlockersForCoreAndProviders(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.fake.shipped = []substrate.ShippedUpgrade{{
		Package: "substrate.reamde.dev/core",
		Upgrade: substrate.BundleUpgrade{Available: true, From: 16, To: 17, Blockers: []string{labelGuard}},
	}}
	h.fake.catalog = []catalogEntry{
		{
			CatalogBundle: substrate.CatalogBundle{
				ID: "providers.substrate.reamde.dev/google", Name: "google", Authority: "providers.substrate.reamde.dev",
				Package: "google", Version: 4, Tier: substrate.TierProvider,
			},
			Installed: true,
			Upgrade: &substrate.BundleUpgrade{
				Available: true, From: 3, To: 4,
				Blockers: []string{`type providers.substrate.reamde.dev/google/contact: property "middleName" dropped while 3 live records still carry it — null it on them first`},
			},
		},
		{
			CatalogBundle: substrate.CatalogBundle{
				ID: "providers.substrate.reamde.dev/linear", Name: "linear", Authority: "providers.substrate.reamde.dev",
				Package: "linear", Version: 2, Tier: substrate.TierProvider,
			},
			Installed: true,
			Upgrade:   &substrate.BundleUpgrade{Available: true, From: 1, To: 2},
		},
		{
			CatalogBundle: substrate.CatalogBundle{
				ID: "samples.substrate.reamde.dev/tasks", Name: "tasks", Authority: "samples.substrate.reamde.dev",
				Package: "tasks", Version: 8, Tier: substrate.TierSample,
			},
		},
	}

	stdout, _ := h.mustRun("catalog")
	for _, want := range []string{
		"GET /api/v1/vocabulary/upgrade",
		"GET /api/v1/catalog",
	} {
		if !contains(h.fake.doorRequests(), want) {
			t.Errorf("catalog did not call %s: %v", want, h.fake.doorRequests())
		}
	}
	// One row per shipped package, core first: the tier, whether it is here,
	// the version the binary ships and the motion an upgrade would make.
	for _, want := range []string{
		"PACKAGE",
		"substrate.reamde.dev/core               seed       true        17        16 -> 17, blocked",
		"providers.substrate.reamde.dev/google   provider   true        4         3 -> 4, blocked",
		"providers.substrate.reamde.dev/linear   provider   true        2         1 -> 2",
		"samples.substrate.reamde.dev/tasks      sample     false       8",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("catalog table lacks %q:\n%s", want, stdout)
		}
	}
	// Every blocked upgrade's guard lines, verbatim, under the table: they
	// are the migration instructions.
	for _, want := range []string{
		"substrate.reamde.dev/core: the upgrade is blocked\n  " + labelGuard,
		"providers.substrate.reamde.dev/google: the upgrade is blocked\n  type providers.substrate.reamde.dev/google/contact",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("catalog does not print the guard lines %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "linear: the upgrade is blocked") {
		t.Errorf("an admitted upgrade is printed as blocked:\n%s", stdout)
	}
}

// A server that previews no shipped upgrade (an older binary, or a dataset
// without the seam) still lists its catalog: the core row is simply absent.
func TestCatalogListsWithoutAShippedPreview(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.fake.shippedStatus = 501
	h.fake.catalog = []catalogEntry{{
		CatalogBundle: substrate.CatalogBundle{
			ID: "samples.substrate.reamde.dev/tasks", Name: "tasks", Authority: "samples.substrate.reamde.dev",
			Package: "tasks", Version: 8, Tier: substrate.TierSample,
		},
	}}
	stdout, _ := h.mustRun("catalog")
	if !strings.Contains(stdout, "samples.substrate.reamde.dev/tasks") {
		t.Fatalf("the catalog is not listed:\n%s", stdout)
	}
	if strings.Contains(stdout, "substrate.reamde.dev/core") {
		t.Fatalf("a core row was invented without a preview:\n%s", stdout)
	}
}

func TestCatalogJSONCarriesTheBlockers(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.fake.shipped = []substrate.ShippedUpgrade{{
		Package: "substrate.reamde.dev/core",
		Upgrade: substrate.BundleUpgrade{Available: true, From: 16, To: 17, Blockers: []string{labelGuard}},
	}}
	stdout, _ := h.mustRun("catalog", "-o", "json")
	for _, want := range []string{`"id": "substrate.reamde.dev/core"`, `"tier": "seed"`, `"from": 16`, `"to": 17`, `"blockers"`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("catalog json lacks %s:\n%s", want, stdout)
		}
	}
}
