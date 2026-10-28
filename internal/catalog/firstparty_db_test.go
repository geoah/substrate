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
	clinicalID    = "substrate.reamde.dev/clinical"
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

	// clinical requires labs, which requires health: one install brings all
	// three (decision record 0193).
	t.Run("clinical", func(t *testing.T) {
		ds := newDataset(t)
		if _, _, err := c.Install(ctx, substrate.ActorAPI, clinicalID, ds); err != nil {
			t.Fatalf("install %s without labs or health: %v", clinicalID, err)
		}
		requirePublishedInstall(t, c, ds, healthID, healthID+"/workout")
		requirePublishedInstall(t, c, ds, labsID, labsID+"/labreport")
		requirePublishedInstall(t, c, ds, clinicalID, clinicalID+"/encounter")
	})
}

// A CLINICAL RECORD LANDS IN EVERY SHAPE A PROVIDER WRITES (decision record
// 0193): a practice, a consultation whose coded entries keep their codes and
// which names the lab report filed at it, a condition recorded at it, a
// prescribed medicine and one issue of it, and a document, a message, an
// allergy, a vaccination, an appointment, a referral and a registration.
func TestClinicalRecordsLand(t *testing.T) {
	ctx := context.Background()
	ds := newDataset(t)
	c := loadCatalog(t)
	if _, _, err := c.Install(ctx, substrate.ActorAPI, clinicalID, ds); err != nil {
		t.Fatalf("install %s: %v", clinicalID, err)
	}
	kind := func(name string) string { return clinicalID + "/" + name }
	put := func(k, id string, props map[string]any) {
		t.Helper()
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{Kind: k, ID: id, Properties: props}); err != nil {
			t.Fatalf("put %s/%s: %v", k, id, err)
		}
	}
	practice := kind("organization") + "/ods/A12345"
	consultation := kind("encounter") + "/gp/c1"
	put(kind("organization"), "ods/A12345", map[string]any{
		"name": "Riverside Medical Practice", "type": "gppractice",
		"identifiers": []any{map[string]any{
			"system": "https://fhir.nhs.uk/Id/ods-organization-code", "value": "A12345", "label": "ODS code",
		}},
		"address": map[string]any{"lines": []any{"1 River Road"}, "city": "London", "postalCode": "N1 1AA"},
	})
	put(kind("patient"), "nhs", map[string]any{
		"name": "NHS England", "fullName": "Ada Example", "birthDate": "1990-01-01",
		"identifiers": []any{map[string]any{"system": "https://fhir.nhs.uk/Id/nhs-number", "value": "9990000000", "label": "NHS number"}},
		"practice":    practice, "pharmacy": practice, "registeredAt": "2024-01-10",
	})
	put(labsID+"/labreport", "gp/r1", map[string]any{"lab": "Riverside Medical Practice", "at": "2026-03-02T09:30:00Z"})
	put(kind("encounter"), "gp/c1", map[string]any{
		"at": "2026-03-02T09:30:00Z", "summary": "Blood pressure review", "type": "Face to face consultation",
		"class": "inperson", "status": "finished", "precision": "time", "organization": practice,
		"participants": []any{map[string]any{"name": "Dr Example", "role": "Dr"}},
		"entries": []any{map[string]any{
			"section": "Examination", "term": "O/E - blood pressure reading", "text": "128/82", "upstreamId": "e1",
			"coding": map[string]any{"system": "http://snomed.info/sct", "code": "163020007", "display": "O/E - blood pressure reading"},
		}},
		"text":     "**Examination**\n\nO/E - blood pressure reading: 128/82",
		"reports":  []any{labsID + "/labreport/gp/r1"},
		"revision": 1,
	})
	put(kind("condition"), "gp/p1", map[string]any{
		"name": "Essential hypertension", "at": "2026-03-02T00:00:00Z", "precision": "day", "clinicalStatus": "active",
		"category": "problem", "significance": "major", "encounter": consultation, "organization": practice,
	})
	put(kind("allergy"), "gp/a1", map[string]any{
		"name": "Allergy to penicillin", "substance": "penicillin", "type": "allergy", "category": "medication",
		"criticality": "high", "clinicalStatus": "active", "at": "2015-06-01T00:00:00Z", "precision": "year",
		"reactions": []any{map[string]any{"manifestation": "Hives", "severity": "moderate"}},
	})
	put(kind("immunization"), "gp/i1", map[string]any{
		"name": "Influenza vaccination", "status": "completed", "doseNumber": "1", "at": "2025-10-01T10:00:00Z",
	})
	put(kind("medicationrequest"), "gp/m1", map[string]any{
		"medication": "Amlodipine 5mg tablets", "dosage": "One tablet once a day", "quantity": "28 tablets",
		"course": "repeat", "status": "active", "at": "2026-03-02T09:40:00Z", "organization": practice,
	})
	put(kind("medicationdispense"), "eps/X1/1", map[string]any{
		"medication": "Amlodipine 5mg tablets", "quantity": "28 tablets", "status": "completed", "sourceStatus": "Dispensed",
		"prescription": "X1", "request": kind("medicationrequest") + "/gp/m1", "pharmacy": practice,
		"at": "2026-03-03T10:00:00Z", "statusAt": "2026-03-04T12:00:00Z",
	})
	put(kind("appointment"), "gp/ap1", map[string]any{
		"name": "Telephone appointment", "status": "fulfilled", "channel": "telephone",
		"at": "2026-03-02T09:30:00Z", "endsAt": "2026-03-02T09:40:00Z", "encounter": consultation,
	})
	put(kind("referral"), "ers/1", map[string]any{
		"specialty": "Cardiology", "status": "active", "priority": "routine", "at": "2026-03-02T10:00:00Z",
		"plannedWait": "P18W", "typicalWait": "P20W", "estimatedTreatmentAt": "2026-07-01", "waitsAsOf": "2026-03-01",
	})
	put(kind("document"), "gp/d1", map[string]any{
		"name": "Discharge summary", "type": "Letter", "at": "2026-02-20T00:00:00Z", "precision": "day",
		"text": "Discharged home.", "status": "current", "encounter": consultation,
	})
	put(kind("communication"), "app/1", map[string]any{
		"sender": "Riverside Medical Practice", "body": "Your blood test results are back.", "at": "2026-03-05T08:00:00Z",
	})

	row, err := ds.Get(ctx, kind("encounter"), "gp/c1")
	if err != nil {
		t.Fatalf("read the consultation back: %v", err)
	}
	entries, _ := row.Properties["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("the consultation holds %d entries, want 1: %v", len(entries), row.Properties["entries"])
	}
	coding, _ := entries[0].(map[string]any)["coding"].(map[string]any)
	if coding["code"] != "163020007" {
		t.Errorf("the entry's code did not survive the write: %v", entries[0])
	}
	if row.Title != "Blood pressure review" {
		t.Errorf("the consultation titles itself %q, want its summary", row.Title)
	}
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

// Re-installing or upgrading a provider writes its shipped trigger again, and
// the shipped document ships disabled and without a key. What the owner set
// on it, being enabled and the webhook key every sender was given, survives:
// a provider upgrade never disarms a webhook it does not own the key of.
func TestAReinstallKeepsAnArmedWebhook(t *testing.T) {
	ctx := context.Background()
	c := loadCatalog(t)
	ds := newDataset(t)
	if _, _, err := c.Install(ctx, substrate.ActorAPI, applehealthID, ds); err != nil {
		t.Fatalf("install %s: %v", applehealthID, err)
	}
	const triggerKind, id, key = "substrate.reamde.dev/core/trigger", "applehealth-webhook", "an-owners-key-0123456789abcdef"
	row, err := ds.Get(ctx, triggerKind, id)
	if err != nil {
		t.Fatalf("read the shipped trigger: %v", err)
	}
	source, _ := row.Properties["source"].(map[string]any)
	webhook, _ := source["webhook"].(map[string]any)
	webhook["key"] = key
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: triggerKind, ID: id, Properties: map[string]any{"enabled": true, "source": source},
	}); err != nil {
		t.Fatalf("arm the trigger: %v", err)
	}

	if _, _, err := c.Install(ctx, substrate.ActorAPI, applehealthID, ds); err != nil {
		t.Fatalf("re-install %s: %v", applehealthID, err)
	}
	row, err = ds.Get(ctx, triggerKind, id)
	if err != nil {
		t.Fatalf("read the trigger after the re-install: %v", err)
	}
	source, _ = row.Properties["source"].(map[string]any)
	webhook, _ = source["webhook"].(map[string]any)
	if row.Properties["enabled"] != true || webhook["key"] != key {
		t.Fatalf("the re-install left enabled=%v key=%v, want the owner's true and %q", row.Properties["enabled"], webhook["key"], key)
	}
	if headers, _ := webhook["headers"].([]any); len(headers) == 0 {
		t.Errorf("the shipped headers did not land with the kept key: %v", webhook)
	}
}

// requireTree is a provider tree of two packages under one authority, each
// declaring one kind: `needs` at version v, requiring `needed` when requires
// is set, and `needed`.
func requireTree(t *testing.T, v int64, requires bool) *catalog.Catalog {
	t.Helper()
	files := fstest.MapFS{}
	for _, name := range []string{"needs", "needed"} {
		pkg := requireAuthority + "/" + name
		version := int64(1)
		header := map[string]any{"description": "a provider of one kind", "installs": []any{pkg + "/note"}}
		if name == "needs" {
			version = v
			if requires {
				header["requires"] = []any{requireAuthority + "/needed"}
			}
		}
		docs := []map[string]any{
			vocabulary.PackageManifest(pkg, version),
			vocabulary.ActorManifest(pkg, vocabulary.PackageActor(pkg)),
			vocabulary.BundleManifest(pkg, header),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "note"},
				map[string]any{"displayTemplate": "{name}", "properties": map[string]any{
					"name": map[string]any{"type": "string"},
				}}),
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

const requireAuthority = "requires.example.com"

// A provider whose next version requires a published package the repository
// lacks is offered that version unblocked: the install brings the
// requirement first, so the preview stages it too and lists what it adds.
// Taking the upgrade then installs both.
func TestAnUpgradePreviewCountsTheRequirementsTheInstallBrings(t *testing.T) {
	ctx := context.Background()
	ds := newDataset(t)
	needs, needed := requireAuthority+"/needs", requireAuthority+"/needed"
	if _, _, err := requireTree(t, 1, false).Install(ctx, substrate.ActorAPI, needs, ds); err != nil {
		t.Fatalf("install %s: %v", needs, err)
	}

	next := requireTree(t, 2, true)
	plan, err := next.Upgrade(ctx, needs, ds, nil)
	if err != nil {
		t.Fatalf("preview %s: %v", needs, err)
	}
	if plan == nil || !plan.Available || len(plan.Blockers) > 0 {
		t.Fatalf("preview = %+v, want an unblocked upgrade", plan)
	}
	brings := false
	for _, ch := range plan.Changes {
		if ch.ID == needed+"/note" {
			brings = true
		}
	}
	if !brings {
		t.Errorf("the preview does not list %s/note, which the upgrade installs: %+v", needed, plan.Changes)
	}

	if _, _, err := next.Install(ctx, substrate.ActorAPI, needs, ds); err != nil {
		t.Fatalf("take the upgrade: %v", err)
	}
	if _, err := ds.KindByRef(ctx, needed+"/note"); err != nil {
		t.Errorf("the upgrade did not bring %s: %v", needed, err)
	}
}
