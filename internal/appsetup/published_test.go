package appsetup

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The packages an authority document publishes beside the fixture catalog
// (decision 0214): training needs the binary's base first, the coach needs
// training, and the rest each need what nothing offers at the floor.
const (
	docAuthority   = "example.org"
	docName        = "Example Publishing"
	docURL         = "https://example.org/.well-known/substrate/authority.json"
	trainingPkg    = docAuthority + "/training"
	coachPkg       = docAuthority + "/trainingcoach"
	needsGonePkg   = docAuthority + "/needsgone"
	needsNewerPkg  = docAuthority + "/needsnewer"
	needsSampleDoc = docAuthority + "/needssample"
)

// publishing is the fixture catalog with closures an authority document
// publishes beside the binary's, answering Resolve as the catalog's document
// reader will (decision 0212): the binary wins for every id it ships, a
// listed package resolves to its newest version at the floor or later, and
// anything else is not published, with the reason in the owner's words. The
// upgrade preview of a listed package is whatever the test set.
type publishing struct {
	*catalog.Catalog
	listed   map[string]*catalog.Bundle
	previews map[string]*substrate.BundleUpgrade
}

// ResolveFor answers as Resolve: the own-authority refusal is the
// catalog's, and its own tests hold it.
func (c *publishing) ResolveFor(ctx context.Context, id string, atLeast int64, _ substrate.Dataset) (*catalog.Bundle, error) {
	return c.Resolve(ctx, id, atLeast)
}

func (c *publishing) Resolve(_ context.Context, id string, atLeast int64) (*catalog.Bundle, error) {
	if b, ok := c.Catalog.ByID(id); ok {
		return b, nil
	}
	authority, _ := vocabulary.SplitPackageRef(id)
	b, ok := c.listed[id]
	switch {
	case !ok:
		return nil, fmt.Errorf("%w: %s publishes no document listing %s", catalog.ErrNotPublished, authority, id)
	case b.Version < atLeast:
		return nil, fmt.Errorf("%w: %s lists %s at version %d, and the app needs %d or later", catalog.ErrNotPublished, authority, id, b.Version, atLeast)
	}
	return b, nil
}

func (c *publishing) Upgrade(ctx context.Context, id string, ds substrate.Dataset, held *substrate.BundleStatus) (*substrate.BundleUpgrade, error) {
	if _, ok := c.listed[id]; ok {
		return c.previews[id], nil
	}
	return c.Catalog.Upgrade(ctx, id, ds, held)
}

func publishedCatalog(t *testing.T) *publishing {
	t.Helper()
	documents := fstest.MapFS{
		"example.org/training/bundle.yaml": {Data: []byte(closure(trainingPkg, 3, map[string]int{basePkg: 2},
			kindDoc(trainingPkg+"/session", "  label:\n    singular: Session\n    plural: Sessions\n"),
		))},
		"example.org/trainingcoach/bundle.yaml": {Data: []byte(closure(coachPkg, 2, map[string]int{trainingPkg: 3},
			kindDoc(coachPkg+"/memory", "  label:\n    singular: Memory\n    plural: Memories\n"),
		))},
		"example.org/needsgone/bundle.yaml":   {Data: []byte(closure(needsGonePkg, 1, map[string]int{docAuthority + "/gone": 1}))},
		"example.org/needsnewer/bundle.yaml":  {Data: []byte(closure(needsNewerPkg, 1, map[string]int{trainingPkg: 9}))},
		"example.org/needssample/bundle.yaml": {Data: []byte(closure(needsSampleDoc, 1, map[string]int{samplePkg: 1}))},
	}
	docs, err := catalog.Load(catalog.ProviderRoot(documents))
	if err != nil {
		t.Fatal(err)
	}
	if w := docs.Warnings(); len(w) > 0 {
		t.Fatalf("the published fixtures dropped %v", w)
	}
	listed := map[string]*catalog.Bundle{}
	for _, b := range docs.Bundles() {
		listed[b.ID] = b.FromDocument(catalog.Origin{
			Authority: docAuthority, Name: docName, DocumentURL: docURL,
			ClosureURL: fmt.Sprintf("https://example.org/packages/%s-%d.yaml", b.Package, b.Version), SHA256: strings.Repeat("ab", 32),
		})
	}
	return &publishing{Catalog: testCatalog(t), listed: listed, previews: map[string]*substrate.BundleUpgrade{}}
}

// holdsPublished marks a document-published package installed at version,
// stamped as the catalog's install stamps it, its kinds in the registry.
func (d *fakeDataset) holdsPublished(b *catalog.Bundle, version int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.statuses[b.ID] = substrate.BundleStatus{ID: b.ID, Installed: true, Enabled: true, Version: version, ShippedVersion: version}
	d.packageRecordLocked(b.ID, vocabulary.SourcePublished, version, version)
	for _, k := range b.Kinds() {
		d.kinds[k.Identity] = substrate.KindInfo{Identity: k.Identity, Source: vocabulary.SourcePublished, Definition: k.Definition, Label: k.Label}
	}
}

func wantPublisher(t *testing.T, what string, got *substrate.AuthorizationPackagePublisher) {
	t.Helper()
	if got == nil || got.Authority != docAuthority || got.Name != docName || got.DocumentURL != docURL || got.FromFile {
		t.Fatalf("%s publisher = %+v, want the document's", what, got)
	}
}

// A DOCUMENT-PUBLISHED PACKAGE RESOLVES LIKE A SHIPPED ONE (decision 0214):
// its row installs the newest version its publisher lists at the floor or
// later and names the publisher; its requirements resolve the same way, a
// binary package and a published one alike, each before what needs it; and
// what the repository holds already is passed over.
func TestADocumentPublishedPackageRowNamesItsPublisherAndWhatItBrings(t *testing.T) {
	cat := publishedCatalog(t)
	ds := newFakeDataset()
	row := plan(t, ds, cat, pkgDetail(coachPkg, 1))[0]
	info := row.Package
	if row.Action != substrate.SetupInstall || row.Blocked != nil || info.Shipped != 2 || info.Held != 0 || info.Tier != substrate.TierPublished || info.Description == "" {
		t.Fatalf("coach = %+v %+v", row, info)
	}
	wantPublisher(t, "the coach's", info.Publisher)
	if len(info.Kinds) != 1 || info.Kinds[0].Identity != coachPkg+"/memory" || !info.Kinds[0].Added {
		t.Fatalf("the coach's own kinds = %+v", info.Kinds)
	}
	if len(info.Requires) != 2 || info.Requires[0].ID != basePkg || info.Requires[1].ID != trainingPkg {
		t.Fatalf("the coach brings %+v first, want base then training", info.Requires)
	}
	base, training := info.Requires[0], info.Requires[1]
	if base.Action != substrate.SetupInstall || base.Shipped != 2 || base.Publisher != nil || base.Blocked != nil || len(base.Kinds) != 1 {
		t.Fatalf("the base step = %+v", base)
	}
	if training.Action != substrate.SetupInstall || training.Shipped != 3 || training.Blocked != nil || len(training.Kinds) != 1 || training.Description == "" {
		t.Fatalf("the training step = %+v", training)
	}
	wantPublisher(t, "the training step's", training.Publisher)

	// What is held at the floor is passed over, and a row in place still
	// says who publishes it.
	ds.installed(cat.Catalog, basePkg, 2)
	if row := plan(t, ds, cat, pkgDetail(coachPkg, 1))[0]; len(row.Package.Requires) != 1 || row.Package.Requires[0].ID != trainingPkg {
		t.Fatalf("with base in place, the coach brings %+v first", row.Package.Requires)
	}
	ds.holdsPublished(cat.listed[trainingPkg], 3)
	rows := plan(t, ds, cat, pkgDetail(trainingPkg, 2), pkgDetail(coachPkg, 2))
	if rows[0].Action != substrate.SetupNone || rows[0].Blocked != nil || rows[0].Package.Held != 3 || rows[0].Package.Shipped != 3 {
		t.Fatalf("training in place = %+v %+v", rows[0], rows[0].Package)
	}
	wantPublisher(t, "training in place", rows[0].Package.Publisher)
	if rows[1].Action != substrate.SetupInstall || len(rows[1].Package.Requires) != 0 {
		t.Fatalf("with training in place, the coach = %+v %+v", rows[1], rows[1].Package)
	}

	// Planned for its action alone, a published row keeps its chain and its
	// publisher and carries no facts.
	rows, _, err := Plan(context.Background(), newFakeDataset(), cat, []substrate.AuthorizationDetail{pkgDetail(coachPkg, 1)}, Options{ActionOnly: map[string]bool{coachPkg: true}})
	if err != nil {
		t.Fatal(err)
	}
	if info := rows[0].Package; rows[0].Action != substrate.SetupInstall || info.Kinds != nil || info.Publisher == nil ||
		len(info.Requires) != 2 || info.Requires[1].Kinds != nil || info.Requires[1].Publisher == nil {
		t.Fatalf("an action-only published row = %+v", info)
	}
}

// NOT PUBLISHED BLOCKS THE APPROVAL: a package no document lists, one listed
// only below the floor, and either needed first, each with the catalog's
// words; a binary sample a published package needs stays the owner's to
// import.
func TestAPackageNothingPublishesAtTheFloorBlocksTheApproval(t *testing.T) {
	cat := publishedCatalog(t)
	ds := newFakeDataset()
	wantNotPublished := func(t *testing.T, what string, block *substrate.AuthorizationBlock, needsFirst bool, words string) {
		t.Helper()
		lead := "This package is not published where this server can install it: "
		if needsFirst {
			lead = "It needs a package first that is not published where this server can install it: "
		}
		if block == nil || block.Reason != substrate.BlockNotPublished || block.Fix != "" ||
			!strings.HasPrefix(block.Problem, lead) || !strings.Contains(block.Problem, words) || !strings.HasSuffix(block.Problem, ".") {
			t.Fatalf("%s = %+v, want not published, saying %q", what, block, words)
		}
	}
	rows := plan(t, ds, cat, pkgDetail(docAuthority+"/unlisted", 1), pkgDetail(trainingPkg, 9), pkgDetail(needsGonePkg, 1), pkgDetail(needsNewerPkg, 1), pkgDetail(needsSampleDoc, 1))
	wantNotPublished(t, "an unlisted package", rows[0].Blocked, false, "example.org publishes no document listing example.org/unlisted")
	if rows[0].Package.Shipped != 0 || rows[0].Package.Publisher != nil {
		t.Fatalf("an unlisted package's row = %+v", rows[0].Package)
	}
	wantNotPublished(t, "a floor above the newest listed", rows[1].Blocked, false, "lists example.org/training at version 3, and the app needs 9 or later")
	wantNotPublished(t, "a package needing an unlisted one", rows[2].Blocked, true, "publishes no document listing example.org/gone")
	if steps := rows[2].Package.Requires; len(steps) != 1 || steps[0].ID != docAuthority+"/gone" || steps[0].Action != substrate.SetupInstall || steps[0].Shipped != 0 {
		t.Fatalf("the steps of a package needing an unlisted one = %+v", steps)
	}
	wantNotPublished(t, "its step", rows[2].Package.Requires[0].Blocked, false, "publishes no document listing example.org/gone")
	wantNotPublished(t, "a package needing a floor above the newest", rows[3].Blocked, true, "lists example.org/training at version 3, and the app needs 9 or later")
	wantBlocked(t, "a sample needed first", rows[4].Blocked, substrate.BlockSample, "/data")
	wantBlocked(t, "the sample step", rows[4].Package.Requires[0].Blocked, substrate.BlockSample, "/data")
	if i := FirstBlocked(rows); i != 0 {
		t.Fatalf("FirstBlocked = %d", i)
	}
}

// AN UPDATE OF A PUBLISHED PACKAGE IS PREVIEWED LIKE A SHIPPED ONE: the
// catalog's preview of the newest listed version blocks it on what its
// package page asks the owner to confirm, and a clean one runs.
func TestAPublishedUpgradeIsPreviewedThroughTheCatalog(t *testing.T) {
	cat := publishedCatalog(t)
	ds := newFakeDataset()
	ds.installed(cat.Catalog, basePkg, 2)
	ds.holdsPublished(cat.listed[trainingPkg], 2)
	cat.previews[trainingPkg] = &substrate.BundleUpgrade{Lossy: true}
	row := plan(t, ds, cat, pkgDetail(trainingPkg, 3))[0]
	if row.Action != substrate.SetupUpgrade || row.Package.Held != 2 || row.Package.Shipped != 3 {
		t.Fatalf("an update = %+v %+v", row, row.Package)
	}
	wantBlocked(t, "a lossy update", row.Blocked, substrate.BlockUpgradeNeedsReview, ProviderPage(trainingPkg))
	wantPublisher(t, "a blocked update's", row.Package.Publisher)
	delete(cat.previews, trainingPkg)
	if row := plan(t, ds, cat, pkgDetail(trainingPkg, 3))[0]; row.Action != substrate.SetupUpgrade || row.Blocked != nil {
		t.Fatalf("a clean update = %+v %+v", row, row.Blocked)
	}
	// Needed first, the update is previewed the same way.
	cat.previews[trainingPkg] = &substrate.BundleUpgrade{Blockers: []string{"a guard"}}
	row = plan(t, ds, cat, pkgDetail(coachPkg, 1))[0]
	wantBlocked(t, "the coach needing a refused update first", row.Blocked, substrate.BlockUpgradeNeedsReview, ProviderPage(trainingPkg))
	wantBlocked(t, "the training step", row.Package.Requires[0].Blocked, substrate.BlockUpgradeNeedsReview, ProviderPage(trainingPkg))
}

// An approval is of the closures the owner saw: a row, or a requirement,
// that would now install another version or another closure under the same
// version has moved the plan, and the runner stops before writing.
func TestAnApprovalIsBoundToTheClosuresItShowed(t *testing.T) {
	published := func(digest string) *substrate.AuthorizationPackagePublisher {
		return &substrate.AuthorizationPackagePublisher{Authority: "publisher.example", DocumentURL: "https://publisher.example/authority.json", Digest: digest}
	}
	row := func(shipped int64, digest string, reqShipped int64, reqDigest string) []substrate.AuthorizationSetup {
		return []substrate.AuthorizationSetup{{
			Type: substrate.DetailPackage, Action: substrate.SetupInstall,
			Package: &substrate.AuthorizationPackage{
				ID: "publisher.example/notes", Shipped: shipped, Publisher: published(digest),
				Requires: []substrate.AuthorizationPackageStep{{ID: "publisher.example/base", Action: substrate.SetupInstall, Shipped: reqShipped, Publisher: published(reqDigest)}},
			},
		}}
	}
	approved := row(1, "aa", 3, "bb")
	for _, tc := range []struct {
		name string
		now  []substrate.AuthorizationSetup
		want int
	}{
		{"the same closures", row(1, "aa", 3, "bb"), -1},
		{"a newer version listed", row(2, "cc", 3, "bb"), 0},
		{"another closure under the same version", row(1, "dd", 3, "bb"), 0},
		{"a requirement's newer version", row(1, "aa", 4, "ee"), 0},
		{"a requirement's other closure", row(1, "aa", 3, "ff"), 0},
	} {
		if got := Moved(approved, tc.now); got != tc.want {
			t.Errorf("%s: Moved = %d, want %d", tc.name, got, tc.want)
		}
	}
}
