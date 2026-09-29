package catalog_test

// The provider install's `shippedVersion` stamp against a REAL engine (issue
// #642): the stamp is what the upgrade preview measures the next shipped
// closure from, a package without one is measured from its stored version,
// and a stamp above what this binary ships offers nothing.

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// whoopShipped is the whoop package version the tree ships right now.
func whoopShipped(t *testing.T) int64 {
	t.Helper()
	b, ok := loadCatalog(t).ByID(whoopID)
	if !ok || b.Version == 0 {
		t.Fatalf("the shipped catalog carries no versioned %s", whoopID)
	}
	return b.Version
}

// whoopCatalog is a catalog holding only the whoop provider, its package
// header moved to version and, when probe is set, a property added to its
// config kind: another binary's shipped whoop.
func whoopCatalog(t *testing.T, version int64, probe bool) *catalog.Catalog {
	t.Helper()
	return whoopCatalogWith(t, version, whoopRelease{probe: probe})
}

// whoopRelease is what another binary's whoop changes beside its version:
// probe adds a property to the config kind, header gives the package header a
// description, trigger disables the shipped on-connect trigger, a data record
// no declaration diff sees, and kind adds whoopNewKind, unpinned, to the
// closure.
type whoopRelease struct {
	probe, header, trigger, kind bool
}

// whoopNewKind is the kind a whoopRelease with kind set adds.
const whoopNewKind = whoopID + "/probe"

// whoopCatalogWith is whoopCatalog with the changes rel names.
func whoopCatalogWith(t *testing.T, version int64, rel whoopRelease) *catalog.Catalog {
	t.Helper()
	probe, header := rel.probe, rel.header
	root := t.TempDir()
	dst := filepath.Join(root, whoopPackage)
	if err := os.MkdirAll(dst, 0o750); err != nil {
		t.Fatal(err)
	}
	copyFile := func(src, to string, rewrite func(string) string) {
		raw, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("read %s: %v", src, err)
		}
		doc := string(raw)
		if rewrite != nil {
			doc = rewrite(doc)
		}
		if err := os.WriteFile(to, []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	copyFile(filepath.Join(filepath.Dir(whoopDir), "authority.yaml"), filepath.Join(root, "authority.yaml"), nil)
	copyFile(filepath.Join(whoopDir, "triggers.yaml"), filepath.Join(dst, "triggers.yaml"), func(doc string) string {
		if rel.trigger {
			doc = mustReplace(t, doc, "\n  id: whoop-on-connect\ndata:\n  properties:\n    enabled: true\n",
				"\n  id: whoop-on-connect\ndata:\n  properties:\n    enabled: false\n")
		}
		return doc
	})
	shipped := whoopShipped(t)
	copyFile(filepath.Join(whoopDir, "bundle.yaml"), filepath.Join(dst, "bundle.yaml"), func(doc string) string {
		doc = mustReplace(t, doc, "\n  version: "+strconv.FormatInt(shipped, 10)+"\n", "\n  version: "+strconv.FormatInt(version, 10)+"\n")
		if header {
			// The first `package: whoop` line is the package header's.
			doc = mustReplace(t, doc, "\n  package: whoop\n",
				"\n  package: whoop\n  description: a header the other binary wrote\n")
		}
		if probe {
			// The first kind-level property block is the config kind's.
			doc = mustReplace(t, doc, "\n  properties:\n",
				"\n  properties:\n    probe:\n      type: string\n      description: a property the other binary added\n")
		}
		if rel.kind {
			// `installs:` names exactly the package's schema members.
			doc = mustReplace(t, doc, "\n  installs:\n", "\n  installs:\n    - "+whoopNewKind+"\n")
			doc = strings.TrimRight(doc, "\n") + "\n---\nkind: substrate.reamde.dev/core/kind\nmetadata:\n  id: " + whoopNewKind +
				"\ndata:\n  authority: " + whoopAuthority + "\n  package: " + whoopPackage +
				"\n  description: a kind the other binary added\n  purpose: internal\n  names:\n    singular: probe\n" +
				"  properties:\n    note:\n      type: string\n      description: a note\n"
		}
		return doc
	})
	c, err := catalog.Load(catalog.ProviderRoot(os.DirFS(root)))
	if err != nil {
		t.Fatalf("load the whoop catalog: %v", err)
	}
	return c
}

// handApplyWhoop applies the shipped whoop files by hand with the package
// header at version, the bundle description edited when edit is set: the
// hand applies that run a package past the shipped line.
func handApplyWhoop(t *testing.T, ds substrate.Dataset, version int64, edit bool) {
	t.Helper()
	docs := whoopClosure(t)
	for _, d := range docs {
		data, _ := d["data"].(map[string]any)
		switch d["kind"] {
		case kindPackageRef:
			data["version"] = version
		case "substrate.reamde.dev/core/bundle":
			if edit {
				data["description"] = "edited by hand: " + data["description"].(string)
			}
		}
	}
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorAPI, docs); err != nil {
		t.Fatalf("hand apply whoop: %v", err)
	}
}

func whoopPreview(t *testing.T, c *catalog.Catalog, ds substrate.Dataset) *substrate.BundleUpgrade {
	t.Helper()
	up, err := c.Upgrade(context.Background(), whoopID, ds, nil)
	if err != nil {
		t.Fatalf("upgrade preview: %v", err)
	}
	if up == nil {
		t.Fatal("no upgrade preview for a held provider")
	}
	return up
}

func namesChange(up *substrate.BundleUpgrade, kind, id string) bool {
	for _, ch := range up.Changes {
		if ch.Kind == kind && ch.ID == id {
			return true
		}
	}
	return false
}

// The issue's own path: google 35 from hand applies, google 33 shipped, the
// install lands 36, and shipped 34 must still be offered. Here with whoop.
func TestAProviderInstalledOverAHigherStoredVersionIsOfferedTheNextShipped(t *testing.T) {
	ds := newDataset(t)
	ctx := context.Background()
	shipped := whoopShipped(t)

	handApplyWhoop(t, ds, shipped+2, true)
	if _, _, err := loadCatalog(t).Install(ctx, substrate.ActorAPI, whoopID, ds); err != nil {
		t.Fatalf("install over the hand-applied whoop: %v", err)
	}
	if got := packageVersion(t, ds, whoopID); got != shipped+3 {
		t.Fatalf("the install landed whoop at %d, want stored+1 = %d", got, shipped+3)
	}
	row, err := ds.Get(ctx, kindPackageRef, whoopID)
	if err != nil {
		t.Fatalf("get the package row: %v", err)
	}
	if v, _ := vocabulary.VersionValue(row.Properties["shippedVersion"]); v != shipped {
		t.Errorf("the install stamped shippedVersion %v, want %d", row.Properties["shippedVersion"], shipped)
	}
	st, err := ds.(bundleStatuser).BundleStatus(ctx, whoopID)
	if err != nil {
		t.Fatalf("bundle status: %v", err)
	}
	if st.Version != shipped+3 || st.ShippedVersion != shipped {
		t.Errorf("the status reads version %d, shippedVersion %d, want %d and %d", st.Version, st.ShippedVersion, shipped+3, shipped)
	}
	if up := whoopPreview(t, loadCatalog(t), ds); up.Available {
		t.Fatalf("the closure just installed previews an upgrade: %+v", up)
	}

	up := whoopPreview(t, whoopCatalog(t, shipped+1, true), ds)
	if !up.Available || up.From != shipped || up.To != shipped+1 {
		t.Fatalf("the next shipped whoop previews %+v, want available %d -> %d", up, shipped, shipped+1)
	}
	if !namesChange(up, vocabulary.DocKind, whoopID+"/config") || len(up.Changes) != 1 {
		t.Errorf("the preview does not name the changed kind alone: %+v", up.Changes)
	}
	for _, ch := range up.Changes {
		if ch.From != shipped+3 || ch.To != shipped+4 {
			t.Errorf("%s %s previews %d -> %d, want stored %d -> stored+1 %d", ch.Kind, ch.ID, ch.From, ch.To, shipped+3, shipped+4)
		}
	}
	if len(up.Blockers) != 0 {
		t.Errorf("an additive upgrade carries blockers: %v", up.Blockers)
	}
}

// A package no install stamped (a hand apply, or an install from before the
// stamp existed) is measured from its stored version, as before.
func TestAnUnstampedProviderIsMeasuredFromItsStoredVersion(t *testing.T) {
	ds := newDataset(t)
	shipped := whoopShipped(t)
	handApplyWhoop(t, ds, shipped+2, false)

	if up := whoopPreview(t, whoopCatalog(t, shipped+1, true), ds); up.Available || up.From != shipped+2 {
		t.Errorf("an unstamped whoop at %d is offered shipped %d: %+v", shipped+2, shipped+1, up)
	}
	if up := whoopPreview(t, whoopCatalog(t, shipped+3, true), ds); !up.Available || up.From != shipped+2 || up.To != shipped+3 {
		t.Errorf("an unstamped whoop at %d previews shipped %d as %+v", shipped+2, shipped+3, up)
	}
}

// A stamp above what this binary ships is a newer binary's install: an older
// binary offers nothing, which is the boot upgrade's never-downgrade rule.
func TestAStampAboveTheShippedVersionOffersNothing(t *testing.T) {
	ds := newDataset(t)
	ctx := context.Background()
	shipped := whoopShipped(t)
	if _, _, err := whoopCatalog(t, shipped+5, true).Install(ctx, substrate.ActorAPI, whoopID, ds); err != nil {
		t.Fatalf("install the newer whoop: %v", err)
	}
	up := whoopPreview(t, loadCatalog(t), ds)
	if up.Available || len(up.Changes) != 0 {
		t.Errorf("an older binary offers its whoop over a newer install: %+v", up)
	}
}

// A SAMPLE installed verbatim stays editable, and no digest says whether it
// was edited, so the install stamps no shipped version: a stamp would keep
// offering it the next shipped closure, and taking that would replace edits
// without the confirmation an imported copy's edits need (decision 0070).
func TestAVerbatimSampleInstallStampsNoShippedVersion(t *testing.T) {
	ds := newDataset(t)
	ctx := context.Background()
	c := loadCatalog(t)
	importVocabulary(t, c, ds, append(tasksRequires, tasksBundleID)...)
	row, err := ds.Get(ctx, kindPackageRef, tasksBundleID)
	if err != nil {
		t.Fatalf("get the package row: %v", err)
	}
	if v, held := row.Properties["shippedVersion"]; held && v != nil {
		t.Errorf("a verbatim sample install stamped shippedVersion %v", v)
	}
	st, err := ds.(bundleStatuser).BundleStatus(ctx, tasksBundleID)
	if err != nil {
		t.Fatalf("bundle status: %v", err)
	}
	if st.ShippedVersion != 0 {
		t.Errorf("a verbatim sample's status reads shippedVersion %d, want none", st.ShippedVersion)
	}
}

// installOverHandApplied is the issue's setup with whoop: hand applies run
// the package two versions past the shipped line, then the shipped closure is
// installed over them. It answers the versions the install left.
func installOverHandApplied(t *testing.T, ds substrate.Dataset) map[string]int64 {
	t.Helper()
	handApplyWhoop(t, ds, whoopShipped(t)+2, true)
	c := loadCatalog(t)
	if _, _, err := c.Install(context.Background(), substrate.ActorAPI, whoopID, ds); err != nil {
		t.Fatalf("install over the hand-applied whoop: %v", err)
	}
	b, _ := c.ByID(whoopID)
	return closureVersions(t, ds, b)
}

// takeWhoop previews rel at version past the stamp, asserts it is offered
// from the stamp, installs it, and asserts the stamp moved to version, no
// declaration version moved, and the offer ended.
func takeWhoop(t *testing.T, ds substrate.Dataset, installed map[string]int64, version int64, rel whoopRelease) {
	t.Helper()
	ctx := context.Background()
	shipped := whoopShipped(t)
	next := whoopCatalogWith(t, version, rel)
	up := whoopPreview(t, next, ds)
	if !up.Available || up.From != shipped || up.To != version {
		t.Fatalf("the release past the stamp previews %+v, want available %d -> %d", up, shipped, version)
	}
	if len(up.Changes) != 0 {
		t.Errorf("a release that changes no declaration lists changes: %+v", up.Changes)
	}
	if _, _, err := next.Install(ctx, substrate.ActorAPI, whoopID, ds); err != nil {
		t.Fatalf("install the release: %v", err)
	}
	b, _ := loadCatalog(t).ByID(whoopID)
	for decl, v := range closureVersions(t, ds, b) {
		if v != installed[decl] {
			t.Errorf("%s moved %d -> %d on a release that changes no declaration", decl, installed[decl], v)
		}
	}
	st, err := ds.(bundleStatuser).BundleStatus(ctx, whoopID)
	if err != nil {
		t.Fatalf("bundle status: %v", err)
	}
	if st.ShippedVersion != version {
		t.Errorf("the stamp reads %d after the install, want %d", st.ShippedVersion, version)
	}
	if up := whoopPreview(t, next, ds); up.Available {
		t.Errorf("the taken release is still offered: %+v", up)
	}
}

// A shipped release past the stamp is offered whatever it changes, as a
// copy's release past `originVersion` is (decision record 0070) and as an
// unstamped provider's release past its stored version is. One that bumps
// only the package version changes no declaration: taking it moves the stamp
// alone.
func TestAVersionOnlyReleasePastTheStampIsOffered(t *testing.T) {
	ds := newDataset(t)
	installed := installOverHandApplied(t, ds)
	takeWhoop(t, ds, installed, whoopShipped(t)+1, whoopRelease{})
}

// A release past the stamp that changes only a data record the closure ships
// (a trigger) is invisible to any declaration diff, and is offered all the
// same: taking it lands the trigger and moves the stamp.
func TestATriggerOnlyReleasePastTheStampIsOfferedAndLands(t *testing.T) {
	ds := newDataset(t)
	installed := installOverHandApplied(t, ds)
	takeWhoop(t, ds, installed, whoopShipped(t)+1, whoopRelease{trigger: true})
	row, err := ds.Get(context.Background(), "substrate.reamde.dev/core/trigger", "whoop-on-connect")
	if err != nil {
		t.Fatalf("get the on-connect trigger: %v", err)
	}
	if enabled, _ := row.Properties["enabled"].(bool); enabled {
		t.Errorf("the release's trigger change did not land: %v", row.Properties)
	}
}

// A release past the stamp that adds an unpinned kind over stored versions
// that ran ahead: the new kind rides the package, which the install lands at
// stored+1 and not at the shipped number, so the preview lists every
// declaration at the version the install then gives it.
func TestANewKindPastTheStampPreviewsTheVersionItLandsAt(t *testing.T) {
	ds := newDataset(t)
	ctx := context.Background()
	shipped := whoopShipped(t)
	installOverHandApplied(t, ds)

	next := whoopCatalogWith(t, shipped+1, whoopRelease{kind: true})
	up := whoopPreview(t, next, ds)
	if !up.Available || !namesChange(up, vocabulary.DocKind, whoopNewKind) {
		t.Fatalf("the release adding %s is not offered naming it: %+v", whoopNewKind, up)
	}
	if _, _, err := next.Install(ctx, substrate.ActorAPI, whoopID, ds); err != nil {
		t.Fatalf("install the release: %v", err)
	}
	b, _ := next.ByID(whoopID)
	after := closureVersions(t, ds, b)
	for _, ch := range up.Changes {
		if ch.Kind != vocabulary.DocKind && ch.Kind != vocabulary.DocPackage {
			continue
		}
		if got := after[ch.Kind+" "+ch.ID]; got != ch.To {
			t.Errorf("%s %s previews landing at %d, the install landed it at %d", ch.Kind, ch.ID, ch.To, got)
		}
	}
	if got := after["kind "+whoopNewKind]; got <= shipped+1 {
		t.Errorf("the new kind landed at %d, want it riding the stored package past the shipped %d", got, shipped+1)
	}
}

// An uninstall ends the install that took the stamp. A put onto the
// tombstoned package row restores its properties, so the uninstall clears
// the stamp first: the same closure applied by hand afterwards is the
// user's package, measured from its stored version, not from a shipped
// version it never took.
func TestAnUninstallClearsTheShippedVersion(t *testing.T) {
	ds := newDataset(t)
	ctx := context.Background()
	shipped := whoopShipped(t)
	if _, _, err := loadCatalog(t).Install(ctx, substrate.ActorAPI, whoopID, ds); err != nil {
		t.Fatalf("install whoop: %v", err)
	}
	if err := ds.(bundleLifecycler).UninstallBundle(ctx, whoopID); err != nil {
		t.Fatalf("uninstall whoop: %v", err)
	}
	handApplyWhoop(t, ds, shipped+2, false)

	row, err := ds.Get(ctx, kindPackageRef, whoopID)
	if err != nil {
		t.Fatalf("get the package row: %v", err)
	}
	if v, held := row.Properties["shippedVersion"]; held && v != nil {
		t.Errorf("the hand-applied package kept the uninstalled provider's stamp %v", v)
	}
	st, err := ds.(bundleStatuser).BundleStatus(ctx, whoopID)
	if err != nil {
		t.Fatalf("bundle status: %v", err)
	}
	if st.ShippedVersion != 0 {
		t.Errorf("the status reads shippedVersion %d on a hand-applied package", st.ShippedVersion)
	}
	if up := whoopPreview(t, whoopCatalog(t, shipped+1, true), ds); up.Available || up.From != shipped+2 {
		t.Errorf("the hand-applied whoop at %d is measured from the old stamp: %+v", shipped+2, up)
	}
}

// The package header is a declaration like any other (the version rule in
// CLAUDE.md): an apply that changes only its description lands it at
// stored+1, and the same apply again keeps it.
func TestAChangedPackageHeaderMovesThePackage(t *testing.T) {
	ds := newDataset(t)
	ctx := context.Background()
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, whoopClosure(t)); err != nil {
		t.Fatalf("apply the whoop closure: %v", err)
	}
	first := packageVersion(t, ds, whoopID)
	described := func() []map[string]any {
		docs := whoopClosure(t)
		for _, d := range docs {
			if d["kind"] == kindPackageRef {
				d["data"].(map[string]any)["description"] = "a header edited by hand"
			}
		}
		return docs
	}
	for i, want := range []int64{first + 1, first + 1} {
		if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, described()); err != nil {
			t.Fatalf("apply the header edit %d: %v", i+1, err)
		}
		if got := packageVersion(t, ds, whoopID); got != want {
			t.Errorf("apply %d of the header edit landed the package at %d, want %d", i+1, got, want)
		}
	}
}

// A release past the stamp that changes only the package header is a real
// update: the preview offers it naming the package, and the install lands the
// header at stored+1 and advances the stamp.
func TestAHeaderChangePastTheStampIsOfferedAndLandsAtStoredPlusOne(t *testing.T) {
	ds := newDataset(t)
	ctx := context.Background()
	shipped := whoopShipped(t)
	handApplyWhoop(t, ds, shipped+2, true)
	if _, _, err := loadCatalog(t).Install(ctx, substrate.ActorAPI, whoopID, ds); err != nil {
		t.Fatalf("install over the hand-applied whoop: %v", err)
	}
	installed := packageVersion(t, ds, whoopID)

	next := whoopCatalogWith(t, shipped+1, whoopRelease{header: true})
	up := whoopPreview(t, next, ds)
	if !up.Available || !namesChange(up, vocabulary.DocPackage, whoopID) {
		t.Fatalf("a header-only change past the stamp is not offered naming the package: %+v", up)
	}
	for _, ch := range up.Changes {
		if ch.Kind == vocabulary.DocPackage && (ch.From != installed || ch.To != installed+1) {
			t.Errorf("the package previews %d -> %d, want %d -> %d", ch.From, ch.To, installed, installed+1)
		}
	}
	if _, _, err := next.Install(ctx, substrate.ActorAPI, whoopID, ds); err != nil {
		t.Fatalf("install the header change: %v", err)
	}
	if got := packageVersion(t, ds, whoopID); got != installed+1 {
		t.Errorf("the header change landed the package at %d, want stored+1 = %d", got, installed+1)
	}
	row, err := ds.Get(ctx, kindPackageRef, whoopID)
	if err != nil {
		t.Fatalf("get the package row: %v", err)
	}
	if v, _ := vocabulary.VersionValue(row.Properties["shippedVersion"]); v != shipped+1 {
		t.Errorf("the stamp reads %v after the install, want %d", row.Properties["shippedVersion"], shipped+1)
	}
	if up := whoopPreview(t, next, ds); up.Available {
		t.Errorf("the taken release is still offered: %+v", up)
	}
}
