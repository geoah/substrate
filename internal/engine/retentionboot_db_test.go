package engine_test

// The binary's own upgrade applies the shipped retention defaults (decision
// 0282): it asks nobody, so the release that ships them says so, and an
// owner who wants the history kept writes an override, which the upgrade
// leaves standing.

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// A repository that core seeded while trigger runs and alerts kept all of
// their history is moved to seven days by the boot upgrade to this release's
// core. An override of trigger runs written before the upgrade keeps them at
// all through it, and one of alerts written after it keeps them at all from
// then on.
func TestTheBootUpgradeShortensTriggerRunsAndAlertsAndAnOverrideKeepsThemAll(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const (
		alertKind      = corePackage + "/alert"
		triggerRunKind = corePackage + "/triggerrun"
	)
	retentions := func(t *testing.T, ds substrate.Dataset) map[string]substrate.KindRetention {
		t.Helper()
		all, err := ds.KindRetentions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]substrate.KindRetention{}
		for _, kr := range all {
			out[kr.Kind] = kr
		}
		return out
	}
	override := func(t *testing.T, ds substrate.Dataset, id, kind string) {
		t.Helper()
		mustPut(t, ds, owner, substrate.PutInput{Kind: corePackage + "/retention", ID: id, Properties: map[string]any{
			"appliesTo": map[string]any{"ref": "substrate.reamde.dev/core/kind/" + kind},
			"retention": "all",
		}})
	}

	// The release before: neither kind states a retention, and core and
	// triggerrun's own pin stand one version lower.
	before := shippedTree(t)
	for _, name := range []string{"alert.yaml", "triggerrun.yaml"} {
		patchShipped(t, filepath.Join(before, corePackage, name), func(doc string) string {
			if !strings.Contains(doc, "\n  retention: P7D\n") {
				t.Fatalf("%s no longer ships seven days", name)
			}
			return strings.Replace(doc, "\n  retention: P7D\n", "\n", 1)
		})
	}
	patchShipped(t, filepath.Join(before, corePackage, "triggerrun.yaml"), func(doc string) string {
		pin, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(reDeclaredVersion.FindString(doc)), "version:")))
		if err != nil {
			t.Fatalf("triggerrun pins no version of its own: %v", err)
		}
		return pinVersion(t, doc, strconv.Itoa(pin-1))
	})
	shipped, err := strconv.Atoi(declaredVersion(t, before, corePackage))
	if err != nil {
		t.Fatal(err)
	}
	bumpPackageVersion(t, before, corePackage, strconv.Itoa(shipped-1))

	svc, dsn := newService(t, engine.WithKindsDir(before), engine.WithCredentialKey(engine.TestCredentialKey))
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create the repository under the release before: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	got := retentions(t, ds)
	for _, kind := range []string{alertKind, triggerRunKind} {
		if r := got[kind]; r.Declared != "" || r.Effective != "all" {
			t.Fatalf("before the upgrade %s reads %+v, want all of it kept and nothing declared", kind, r)
		}
	}
	override(t, ds, "runs", triggerRunKind)
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}

	// This release's boot upgrade.
	svc = openTree(t, dsn, shippedTree(t))
	t.Cleanup(func() { _ = svc.Close() })
	ds, err = svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	got = retentions(t, ds)
	if r := got[alertKind]; r.Declared != "P7D" || r.Override != "" || r.Effective != "P7D" {
		t.Errorf("after the upgrade alerts read %+v, want seven days declared and in effect", r)
	}
	if r := got[triggerRunKind]; r.Declared != "P7D" || r.Override != "all" || r.OverrideID != "runs" || r.Effective != "all" {
		t.Errorf("after the upgrade trigger runs read %+v, want seven days declared and all kept by the override written before", r)
	}

	override(t, ds, "alerts", alertKind)
	if r := retentions(t, ds)[alertKind]; r.Declared != "P7D" || r.Override != "all" || r.OverrideID != "alerts" || r.Effective != "all" {
		t.Errorf("alerts with an override written after the upgrade read %+v, want all kept", r)
	}
}
