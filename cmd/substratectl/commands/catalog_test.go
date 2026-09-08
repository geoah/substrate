package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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
	h.fake.catalog = []substrate.CatalogItem{
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
// So does one whose preview fails outright: the catalog read is the reason
// the command was run, and the core row is the extra. The unexpected status
// is said once, on stderr.
func TestCatalogListsWithoutAShippedPreview(t *testing.T) {
	for _, status := range []int{404, 501, 500} {
		h := newHarness(t)
		h.writeConfig()
		h.fake.shippedStatus = status
		h.fake.catalog = []substrate.CatalogItem{{
			CatalogBundle: substrate.CatalogBundle{
				ID: "samples.substrate.reamde.dev/tasks", Name: "tasks", Authority: "samples.substrate.reamde.dev",
				Package: "tasks", Version: 8, Tier: substrate.TierSample,
			},
		}}
		stdout, stderr, err := h.run("catalog")
		if err != nil {
			t.Fatalf("shipped read answering %d aborted the listing: %v", status, err)
		}
		if !strings.Contains(stdout, "samples.substrate.reamde.dev/tasks") {
			t.Fatalf("%d: the catalog is not listed:\n%s", status, stdout)
		}
		if strings.Contains(stdout, "substrate.reamde.dev/core") {
			t.Fatalf("%d: a core row was invented without a preview:\n%s", status, stdout)
		}
		if noted := strings.Contains(stderr, "answered 500"); noted != (status == 500) {
			t.Fatalf("%d: stderr = %q; only an unexpected status is noted", status, stderr)
		}
	}
}

// A repository ahead of its binary (a rollback) previews core as not
// available with the stored version above the shipped one. No motion is
// printed: "18 -> 17" would read as a downgrade the boot never performs.
func TestCatalogPrintsNoMotionForAnUnavailableUpgrade(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.fake.shipped = []substrate.ShippedUpgrade{{
		Package: "substrate.reamde.dev/core",
		Upgrade: substrate.BundleUpgrade{Available: false, From: 18, To: 17},
	}}
	stdout, _ := h.mustRun("catalog")
	if strings.Contains(stdout, "18 -> 17") || strings.Contains(stdout, "restart") {
		t.Fatalf("an unavailable upgrade prints a motion:\n%s", stdout)
	}
	if !regexp.MustCompile(`(?m)^substrate\.reamde\.dev/core\s+seed\s+true\s+17\s*$`).MatchString(stdout) {
		t.Fatalf("the core row is not listed at the shipped version with an empty UPGRADE cell:\n%s", stdout)
	}
}

// `-o json` on a server with nothing shipped prints an empty list, not null.
func TestCatalogJSONIsAListWhenEmpty(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.fake.shippedStatus = 501
	stdout, _ := h.mustRun("catalog", "-o", "json")
	if strings.TrimSpace(stdout) != "[]" {
		t.Fatalf("catalog json = %q, want []", stdout)
	}
}

// Core's other pending state: the last blocking record is migrated, the
// preview has no blockers, and the store stays old until the server starts
// again. The row says a restart lands it rather than reading like a
// provider's one-command upgrade.
func TestCatalogSaysAnAdmittedCoreUpgradeLandsAtRestart(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.fake.shipped = []substrate.ShippedUpgrade{
		{
			Package: "substrate.reamde.dev/core",
			Upgrade: substrate.BundleUpgrade{Available: true, From: 16, To: 17},
		},
	}
	stdout, _ := h.mustRun("catalog")
	if !strings.Contains(stdout, "16 -> 17, lands at restart") {
		t.Fatalf("an admitted core upgrade does not say a restart lands it:\n%s", stdout)
	}
	if strings.Contains(stdout, "blocked") {
		t.Fatalf("an admitted upgrade reads as blocked:\n%s", stdout)
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

// A shipped package with no stored version has never landed here: a second
// builtin package a core guard withholds, say. Its row reads not installed,
// rather than the seed tier being assumed present.
func TestCatalogReadsASeedPackageNotYetHeldAsAbsent(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.fake.shipped = []substrate.ShippedUpgrade{
		{
			Package: "substrate.reamde.dev/core",
			Upgrade: substrate.BundleUpgrade{Available: true, From: 16, To: 17, Blockers: []string{labelGuard}},
		},
		{
			Package: "substrate.reamde.dev/extra",
			Upgrade: substrate.BundleUpgrade{Available: true, To: 3, Blockers: []string{labelGuard}},
		},
	}
	stdout, _ := h.mustRun("catalog")
	if !regexp.MustCompile(`(?m)^substrate\.reamde\.dev/core\s+seed\s+true\s+17\s`).MatchString(stdout) {
		t.Fatalf("core is not listed as held:\n%s", stdout)
	}
	if !regexp.MustCompile(`(?m)^substrate\.reamde\.dev/extra\s+seed\s+false\s+3\s`).MatchString(stdout) {
		t.Fatalf("a shipped package with no stored version reads as installed:\n%s", stdout)
	}
}

const googleProvider = "providers.substrate.reamde.dev/google"

// lossyGoogle is a catalog entry whose upgrade preview removes values: one
// null step the door runs only confirmed (decision 0067).
func lossyGoogle() substrate.CatalogItem {
	return substrate.CatalogItem{
		CatalogBundle: substrate.CatalogBundle{
			ID: googleProvider, Name: "google", Authority: "providers.substrate.reamde.dev",
			Package: "google", Version: 4, Tier: substrate.TierProvider,
		},
		Installed: true,
		Upgrade: &substrate.BundleUpgrade{
			Available: true, From: 3, To: 4,
			ConversionPlan: substrate.ConversionPlan{
				Lossy: true, Work: 3, PlanHash: "cafe", ChangelogSeq: 41,
				Steps: []substrate.ConversionStep{{
					Step: substrate.StepNull, Kind: googleProvider + "/contact", Property: "middleName", Records: 3, Lossy: true,
				}},
			},
		},
	}
}

// lastConfirm decodes the confirmation the last request's body carried.
func lastConfirm(t *testing.T, h *harness) (substrate.ConversionConfirm, bool) {
	t.Helper()
	raw, ok := h.fake.lastBody["confirm"]
	if !ok {
		return substrate.ConversionConfirm{}, false
	}
	var c substrate.ConversionConfirm
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("decode the confirmation: %v", err)
	}
	return c, true
}

const tasksSample = "samples.substrate.reamde.dev/tasks"

// editedTasks is a held sample copy the reader edited since importing it: the
// server keeps a preview on it with nothing shipped moved, so the re-import's
// confirmation has a hash to name (decision record 0070).
func editedTasks() substrate.CatalogItem {
	return substrate.CatalogItem{
		CatalogBundle: substrate.CatalogBundle{
			ID: tasksSample, Name: "tasks", Authority: "samples.substrate.reamde.dev",
			Package: "tasks", Version: 7, Tier: substrate.TierSample,
			Origin: tasksSample, OriginVersion: 7, Modified: true,
		},
		Installed: true,
		Upgrade: &substrate.BundleUpgrade{
			DiscardsEdits:  true,
			ConversionPlan: substrate.ConversionPlan{PlanHash: "d15c", ChangelogSeq: 9},
		},
	}
}

// `import --allow-data-loss` over an edited copy reads the preview, says the
// edits go, and confirms exactly that plan.
func TestImportConfirmsAReimportThatReplacesEdits(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.fake.catalog = []substrate.CatalogItem{editedTasks()}
	stdout, _ := h.mustRun("import", tasksSample, "--allow-data-loss")
	for _, want := range []string{"GET /api/v1/catalog", "POST /api/v1/catalog/" + tasksSample + "/import"} {
		if !contains(h.fake.doorRequests(), want) {
			t.Errorf("import did not call %s: %v", want, h.fake.doorRequests())
		}
	}
	confirm, ok := lastConfirm(t, h)
	if !ok || confirm != (substrate.ConversionConfirm{PlanHash: "d15c", ChangelogSeq: 9}) {
		t.Fatalf("the import body carried confirmation %+v (present=%v)", confirm, ok)
	}
	for _, want := range []string{
		tasksSample + ": confirming plan d15c at changelog seq 9, which replaces edits:",
		"replaces your copy of " + tasksSample + " whole: the declarations edited since it was imported go with it",
		"geoah.example.com/tasks imported",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("import did not print %q:\n%s", want, stdout)
		}
	}
	// Without the flag the import is the bare POST, and the server's refusal
	// is the answer.
	h.mustRun("import", tasksSample)
	if _, ok := lastConfirm(t, h); ok {
		t.Fatal("a bare import sent a confirmation")
	}
}

// `substratectl catalog` says an edited copy is one, and which command
// replaces the edits.
func TestCatalogSaysAnEditedCopyTakesImportAgain(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.fake.catalog = []substrate.CatalogItem{editedTasks()}
	stdout, _ := h.mustRun("catalog")
	if !regexp.MustCompile(`(?m)^samples\.substrate\.reamde\.dev/tasks\s+sample\s+true\s+7\s+edited copy$`).MatchString(stdout) {
		t.Errorf("the UPGRADE column does not say the copy was edited:\n%s", stdout)
	}
	want := tasksSample + ": your copy was edited since it was imported; importing it again replaces those edits, confirm it with `substratectl import " + tasksSample + " --allow-data-loss`"
	if !strings.Contains(stdout, want) {
		t.Errorf("catalog did not print %q:\n%s", want, stdout)
	}
}

// `install --allow-data-loss` binds the consent to the preview: it reads the
// catalog, prints the steps that remove values and sends that preview's hash
// and changelog head as the confirmation, never a bare yes.
func TestInstallConfirmsALossyUpgradeItPreviewed(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.fake.catalog = []substrate.CatalogItem{lossyGoogle()}
	stdout, _ := h.mustRun("install", googleProvider, "--allow-data-loss")
	for _, want := range []string{"GET /api/v1/catalog", "POST /api/v1/catalog/" + googleProvider + "/install"} {
		if !contains(h.fake.doorRequests(), want) {
			t.Errorf("install did not call %s: %v", want, h.fake.doorRequests())
		}
	}
	confirm, ok := lastConfirm(t, h)
	if !ok || confirm != (substrate.ConversionConfirm{PlanHash: "cafe", ChangelogSeq: 41}) {
		t.Fatalf("the install body carried confirmation %+v (present=%v)", confirm, ok)
	}
	for _, want := range []string{
		googleProvider + ": confirming plan cafe at changelog seq 41, which removes values:",
		"drops middleName on " + googleProvider + "/contact: its value leaves 3 live records (lossy: the values stay in the changelog only)",
		googleProvider + " installed",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("install did not print %q:\n%s", want, stdout)
		}
	}
}

// Without the flag the install is the bare POST it always was: no preview is
// read and no body is sent, so a lossy upgrade is the server's 403, which the
// CLI renders with the hint naming the flag this command carries.
func TestInstallWithoutTheFlagIsRefusedWithTheHint(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.fake.catalog = []substrate.CatalogItem{lossyGoogle()}
	h.fake.installRefusesLossy = true
	_, _, err := h.run("install", googleProvider)
	if err == nil {
		t.Fatal("a lossy install without the flag must fail")
	}
	if contains(h.fake.doorRequests(), "GET /api/v1/catalog") {
		t.Errorf("a bare install read the catalog: %v", h.fake.doorRequests())
	}
	if _, ok := lastConfirm(t, h); ok {
		t.Fatal("a bare install sent a confirmation")
	}
	// Rendered as the binary renders it (renderError), the refusal names the
	// flag this command carries.
	var rendered bytes.Buffer
	renderError(&rendered, err)
	for _, want := range []string{
		"error: the schema change removes values from stored records and needs a confirmation",
		"hint: re-run `substratectl install <provider> --allow-data-loss`",
	} {
		if !strings.Contains(rendered.String(), want) {
			t.Errorf("the refusal did not render %q:\n%s", want, rendered.String())
		}
	}
	// With the flag the same install confirms the previewed plan and lands.
	h.mustRun("install", googleProvider, "--allow-data-loss")
	if confirm, ok := lastConfirm(t, h); !ok || confirm.PlanHash != "cafe" {
		t.Fatalf("the flagged install carried confirmation %+v (present=%v)", confirm, ok)
	}
}

// `substratectl catalog` prints the steps an upgrade would run, with the loss
// named and the command that confirms it.
func TestCatalogPrintsTheConversionSteps(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.fake.catalog = []substrate.CatalogItem{lossyGoogle()}
	stdout, _ := h.mustRun("catalog")
	for _, want := range []string{
		googleProvider + ": the upgrade rewrites 3 live records and removes values; confirm it with `substratectl install " + googleProvider + " --allow-data-loss`",
		"  drops middleName on " + googleProvider + "/contact: its value leaves 3 live records",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("catalog did not print %q:\n%s", want, stdout)
		}
	}
}

// `apply --allow-data-loss` previews the batch first and confirms the plan it
// printed, by its hash and changelog head; a lossless plan confirms nothing.
func TestApplyConfirmsALossyPlanItPreviewed(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.fake.plan = substrate.VocabularyPlan{ConversionPlan: substrate.ConversionPlan{
		Lossy: true, Work: 2, PlanHash: "f00d", ChangelogSeq: 7,
		Steps: []substrate.ConversionStep{{
			Step: substrate.StepRemap, Kind: "geoah.example.com/shop/widget", Property: "status",
			From: "active", To: "open", Records: 2, Lossy: true,
		}},
	}}
	file := filepath.Join(t.TempDir(), "widget.yaml")
	if err := os.WriteFile(file, []byte(`kind: substrate.reamde.dev/core/kind
metadata:
  id: geoah.example.com/shop/widget
data:
  authority: geoah.example.com
  package: shop
  names:
    singular: widget
    plural: widgets
  properties:
    status:
      type: enum
      values: [open, active]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, _ := h.mustRun("apply", "-f", file, "--allow-data-loss")
	requests := h.fake.doorRequests()
	plan, apply := -1, -1
	for i, r := range requests {
		switch r {
		case "POST /api/v1/vocabulary/plan":
			plan = i
		case "POST /api/v1/vocabulary/apply":
			apply = i
		}
	}
	if plan < 0 || apply < 0 || plan > apply {
		t.Fatalf("apply must preview before it applies: %v", requests)
	}
	confirm, ok := lastConfirm(t, h)
	if !ok || confirm != (substrate.ConversionConfirm{PlanHash: "f00d", ChangelogSeq: 7}) {
		t.Fatalf("the apply body carried confirmation %+v (present=%v)", confirm, ok)
	}
	for _, want := range []string{
		"confirming plan f00d at changelog seq 7, which removes values:",
		"rewrites status active to open on geoah.example.com/shop/widget: 2 live records rewritten (lossy: the records holding either value become one set)",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("apply did not print %q:\n%s", want, stdout)
		}
	}

	h.fake.plan = substrate.VocabularyPlan{}
	h.mustRun("apply", "-f", file, "--allow-data-loss")
	if _, ok := lastConfirm(t, h); ok {
		t.Fatal("a lossless plan was confirmed")
	}
}
