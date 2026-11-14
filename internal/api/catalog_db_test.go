package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/kinds"
	"github.com/geoah/substrate/samples"
)

// catalogEnv is a dbEnv whose handler ships cat.
func catalogEnv(t *testing.T, cat *catalog.Catalog, opts ...dbOption) *dbEnv {
	t.Helper()
	return newDBEnv(t, append(opts, withConfig(func(c *Config) { c.Catalog = cat }))...)
}

// catalogListing is the catalog listing bearer reads, by entry id.
func catalogListing(t *testing.T, env *dbEnv, bearer string) map[string]substrate.CatalogItem {
	t.Helper()
	rec := env.do(t, http.MethodGet, "/api/v1/catalog", bearer, nil)
	wantStatus(t, rec, http.StatusOK)
	out := map[string]substrate.CatalogItem{}
	for _, item := range decodeJSON[substrate.OperationalList[substrate.CatalogItem]](t, rec).Items {
		out[item.ID] = item
	}
	return out
}

// catalogEntry is one listed entry, failing the test where it is not listed.
func catalogEntry(t *testing.T, env *dbEnv, id string) substrate.CatalogItem {
	t.Helper()
	item, ok := catalogListing(t, env, env.token)[id]
	if !ok {
		t.Fatalf("bundle %q missing from the catalog listing", id)
	}
	return item
}

// catalogDetail is one entry's own read.
func catalogDetail(t *testing.T, env *dbEnv, bearer, id string) substrate.CatalogItem {
	t.Helper()
	rec := env.do(t, http.MethodGet, "/api/v1/catalog/"+url.PathEscape(id), bearer, nil)
	wantStatus(t, rec, http.StatusOK)
	return decodeJSON[substrate.CatalogItem](t, rec)
}

// catalogAdd adds each catalog entry as the owner.
func catalogAdd(t *testing.T, env *dbEnv, ids ...string) {
	t.Helper()
	for _, id := range ids {
		rec := env.do(t, http.MethodPost, "/api/v1/catalog/"+url.PathEscape(id)+"/add", env.token, nil)
		wantStatus(t, rec, http.StatusOK)
	}
}

// catalogAddReadinglist adds the reading-list sample the way the console
// does: the samples it requires first, each rehomed.
func catalogAddReadinglist(t *testing.T, env *dbEnv) {
	t.Helper()
	catalogAdd(t, env, peopleBundleID, "samples.substrate.reamde.dev/messaging", rlBundleID)
}

// catalogEditCopy redeclares kind, one of an imported copy's, with one more
// property: the owner's edit, which moves the copy off its stamp.
func catalogEditCopy(t *testing.T, env *dbEnv, kind string) {
	t.Helper()
	info, err := env.ds.KindByRef(t.Context(), kind)
	if err != nil {
		t.Fatalf("read %s: %v", kind, err)
	}
	def := map[string]any{}
	for k, v := range info.Definition {
		def[k] = v
	}
	props := map[string]any{"ownerNote": map[string]any{"type": "string"}}
	if declared, ok := def["properties"].(map[string]any); ok {
		for k, v := range declared {
			props[k] = v
		}
	}
	def["properties"] = props
	if _, err := env.ds.ApplyVocabularyDocuments(t.Context(), substrate.ActorPerson, []map[string]any{{
		"kind": corePackage + "/kind", "metadata": map[string]any{"id": kind}, "data": def,
	}}); err != nil {
		t.Fatalf("edit %s: %v", kind, err)
	}
}

// catalogPreviewFails is a repository whose upgrade previews fail: a database
// fault under the preview's reads, which no request can make the engine
// produce.
type catalogPreviewFails struct{ substrate.Dataset }

// errCatalogPreview reads like a driver error that names the deployment, so
// serving it would leak it.
var errCatalogPreview = errors.New("dial tcp db-internal.example.com:5432: connection refused")

func (catalogPreviewFails) PlanBundleUpgrade(context.Context, []map[string]any) (substrate.BundleUpgrade, error) {
	return substrate.BundleUpgrade{}, errCatalogPreview
}

// A preview that fails costs that entry its upgrade offer and NOTHING else:
// the listing still answers 200, because the console reads it on every page.
// The failure is not swallowed with the offer, though. It rides the entry as
// one fixed blocker line, so the owner sees an upgrade nobody can take
// instead of an entry with nothing to say. The error text stays in the server
// log: a driver error names the deployment, and a repository token's 200 body
// is not where that goes. The installed bundle is a PROVIDER: only a provider
// is offered an upgrade preview at all without a copy's stamp (decision
// record 0048).
func TestCatalogListSurvivesAFailedUpgradePreview(t *testing.T) {
	t.Parallel()
	cat := catalogShipped(t)
	env := catalogEnv(t, cat)
	if _, _, err := cat.Install(t.Context(), substrate.ActorPerson, googleBundleID, env.ds); err != nil {
		t.Fatalf("install google: %v", err)
	}
	env.wrap = func(ds substrate.Dataset) substrate.Dataset { return catalogPreviewFails{ds} }

	wantFailedPreview := func(item substrate.CatalogItem) {
		t.Helper()
		if item.Upgrade == nil {
			t.Fatalf("bundle %s dropped its failed preview silently", item.ID)
		}
		if item.Upgrade.Available {
			t.Errorf("bundle %s offers an upgrade its preview could not compute", item.ID)
		}
		if len(item.Upgrade.Blockers) != 1 || item.Upgrade.Blockers[0] != failedPreviewBlocker {
			t.Errorf("bundle %s blockers = %q, want exactly %q", item.ID, item.Upgrade.Blockers, failedPreviewBlocker)
		}
		if strings.Contains(strings.Join(item.Upgrade.Blockers, ";"), errCatalogPreview.Error()) {
			t.Errorf("bundle %s serves the preview's error text to a repository token: %q", item.ID, item.Upgrade.Blockers)
		}
	}
	listing := catalogListing(t, env, env.token)
	if len(listing) == 0 {
		t.Fatal("the listing is empty")
	}
	google, found := listing[googleBundleID]
	if !found || !google.Installed {
		t.Fatalf("%s is not listed installed: %+v", googleBundleID, google)
	}
	wantFailedPreview(google)
	for id, item := range listing {
		// Only the installed provider is previewed; nothing else may
		// inherit its failure.
		if id != googleBundleID && item.Upgrade != nil {
			t.Errorf("bundle %s carries an upgrade it was never previewed for", id)
		}
	}
	wantFailedPreview(catalogDetail(t, env, env.token, googleBundleID))
}

func TestCatalogListReturnsShippedBundles(t *testing.T) {
	t.Parallel()
	cat := catalogShipped(t)
	env := catalogEnv(t, cat)
	shipped, ok := cat.ByID(rlBundleID)
	if !ok {
		t.Fatalf("the catalog ships no %s", rlBundleID)
	}
	item := catalogEntry(t, env, rlBundleID)
	if item.Name != "readinglist" || item.Authority != "samples.substrate.reamde.dev" || item.Package != "readinglist" || item.Version != shipped.Version || item.Version == 0 {
		t.Errorf("reading-list entry fields = %+v", item.CatalogBundle)
	}
	if item.Installed {
		t.Error("readinglist must not read installed in a repository that never took it")
	}
}

// The catalog list says who keeps each entry on the wire: the console's two
// sections and what adding a row lands are read from it, so google is kept by
// its publisher and the reading list by the repository (decision record 0258).
func TestCatalogListSaysWhoKeepsEachPackage(t *testing.T) {
	t.Parallel()
	env := catalogEnv(t, catalogShipped(t))
	listing := catalogListing(t, env, env.token)
	want := map[string]string{
		googleBundleID: substrate.KeptByPublisher,
		rlBundleID:     substrate.KeptByRepository,
	}
	for id, keptBy := range want {
		item, ok := listing[id]
		if !ok {
			t.Errorf("bundle %q missing from catalog list", id)
			continue
		}
		if item.KeptBy != keptBy {
			t.Errorf("%s keptBy = %q, want %q", id, item.KeptBy, keptBy)
		}
	}
	for id, item := range listing {
		if item.KeptBy == "" {
			t.Errorf("bundle %q says nobody keeps it, so the console cannot place it", id)
		}
	}
}

func TestCatalogDetailPreviewsTheClosure(t *testing.T) {
	t.Parallel()
	env := catalogEnv(t, catalogShipped(t))
	item := catalogDetail(t, env, env.token, rlBundleID)
	if len(item.Closure.Functions) != 4 || len(item.Closure.Records) != 6 {
		t.Errorf("closure = %+v", item.Closure)
	}
}

// The preview of a sample copy EDITED since it was imported stays on the entry
// even when nothing shipped moved (decision record 0070): `available` false,
// `discardsEdits` true, and the hash the re-import's confirmation names. A
// preview with nothing to offer, block or discard is dropped as before.
func TestCatalogKeepsThePreviewOfAnEditedCopy(t *testing.T) {
	t.Parallel()
	env := catalogEnv(t, catalogShipped(t))
	catalogAddReadinglist(t, env)
	if up := catalogEntry(t, env, rlBundleID).Upgrade; up != nil {
		t.Errorf("a preview with nothing to say rides the entry: %+v", up)
	}

	catalogEditCopy(t, env, env.repository+"/readinglist/page")
	head := vocabularyHead(t, env)
	up := catalogEntry(t, env, rlBundleID).Upgrade
	if up == nil || !up.DiscardsEdits || up.Available || up.PlanHash == "" || up.ChangelogSeq != head.Seq {
		t.Fatalf("an edited copy's entry carries %+v, want the discarding preview with its hash at %d", up, head.Seq)
	}
}

// A closure's `requiresAtLeast` floors ride the catalog entry beside
// `requires` (decision record 0070), so the console can read them against the
// held bundles' versions before the button is pressed.
func TestCatalogCarriesTheRequiresFloors(t *testing.T) {
	t.Parallel()
	env := catalogEnv(t, catalogShipped(t))
	item := catalogDetail(t, env, env.token, "samples.substrate.reamde.dev/tasks")
	if item.RequiresAtLeast[peopleBundleID] == 0 {
		t.Errorf("the tasks entry carries no floor under people: requires %v, requiresAtLeast %v", item.Requires, item.RequiresAtLeast)
	}
	for pkg := range item.RequiresAtLeast {
		if !slices.Contains(item.Requires, pkg) {
			t.Errorf("floor %s is under a package requires does not list: %v", pkg, item.Requires)
		}
	}
}

// catalogWidgets is a provider root shipping widgets.example.com/widgets at
// version, its widget kind declaring the properties named.
func catalogWidgets(t *testing.T, version string, props ...string) *catalog.Catalog {
	t.Helper()
	var declared strings.Builder
	for _, p := range props {
		declared.WriteString("    " + p + ":\n      type: string\n")
	}
	root := fstest.MapFS{"widgets/bundle.yaml": &fstest.MapFile{Data: []byte(`
kind: substrate.reamde.dev/core/package
metadata:
  id: widgets.example.com/widgets
data:
  authority: widgets.example.com
  package: widgets
  version: ` + version + `
---
kind: substrate.reamde.dev/core/bundle
metadata:
  id: widgets.example.com/widgets
data:
  authority: widgets.example.com
  package: widgets
  description: a provider under test
  installs:
    - widgets.example.com/widgets/widget
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: widgets.example.com/widgets/widget
data:
  authority: widgets.example.com
  package: widgets
  names:
    singular: widget
  properties:
` + declared.String())}}
	cat, err := catalog.Load(catalog.ProviderRoot(root))
	if err != nil {
		t.Fatalf("load the widgets provider: %v", err)
	}
	if _, ok := cat.ByID("widgets.example.com/widgets"); !ok {
		t.Fatalf("the widgets provider did not load: %v", cat.Warnings())
	}
	return cat
}

// The add body's confirmation reaches the engine as BundleInstall.Confirm
// (decision 0067): binary N+1's widgets provider drops a color two widgets
// carry, a bare POST hands the door none and the engine refuses the lossy
// plan, a confirmation for another plan is the engine's conflict, and the
// previewed one lands the drop. The provider lands as its publisher's.
func TestCatalogAddCarriesTheConfirmation(t *testing.T) {
	t.Parallel()
	const id = "widgets.example.com/widgets"
	env := catalogEnv(t, catalogWidgets(t, "2", "name"))
	if _, _, err := catalogWidgets(t, "1", "name", "color").Install(t.Context(), substrate.ActorPerson, id, env.ds); err != nil {
		t.Fatalf("install binary N's widgets: %v", err)
	}
	widget := id + "/widget"
	for _, w := range []string{"w1", "w2"} {
		env.put(t, widget, w, map[string]any{"name": w, "color": "red"})
	}
	up := catalogDetail(t, env, env.token, id).Upgrade
	if up == nil || !up.Available || !up.Lossy || up.PlanHash == "" {
		t.Fatalf("the moved provider previews %+v, want a lossy upgrade", up)
	}

	path := "/api/v1/catalog/" + url.PathEscape(id) + "/add"
	wantErrorCode(t, env.do(t, http.MethodPost, path, env.token, nil), http.StatusForbidden, codeLossy)
	if got := env.get(t, widget, "w1"); got.Properties["color"] != "red" {
		t.Fatalf("a refused install dropped the color: %+v", got)
	}
	rec := env.do(t, http.MethodPost, path, env.token, map[string]any{
		"confirm": map[string]any{"planHash": "0000", "changelogSeq": up.ChangelogSeq},
	})
	wantErrorCode(t, rec, http.StatusConflict, codeConflict)
	rec = env.do(t, http.MethodPost, path, env.token, map[string]any{
		"confirm": map[string]any{"planHash": up.PlanHash, "changelogSeq": up.ChangelogSeq},
	})
	wantStatus(t, rec, http.StatusOK)
	for _, w := range []string{"w1", "w2"} {
		if got := env.get(t, widget, w); got.Properties["color"] != nil {
			t.Fatalf("the confirmed install left %s its color: %+v", w, got)
		}
	}
	if row := env.get(t, corePackage+"/package", id); row == nil || row.Properties["source"] != vocabulary.SourcePublished {
		t.Fatalf("a provider install lost its tier on the way to the engine: %+v", row)
	}
}

// A sample this repository IMPORTED is held under the rehomed id, so that is
// what the listing has to look for.
func TestCatalogReportsAnImportedSampleInstalled(t *testing.T) {
	t.Parallel()
	env := catalogEnv(t, catalogShipped(t))
	catalogAddReadinglist(t, env)
	if !catalogEntry(t, env, rlBundleID).Installed {
		t.Error("an imported sample reads as available, so the console offers it again")
	}
}

// The held copy's provenance rides the listing entry: which shipped id it was
// imported from, at which version, and whether it has been edited since. The
// status computed it; the entry carries it so the Registry can say "imported
// at 14, edited" without a second read. An entry the repository holds no copy
// of carries none.
func TestCatalogCarriesTheHeldCopyProvenance(t *testing.T) {
	t.Parallel()
	cat := catalogShipped(t)
	env := catalogEnv(t, cat)
	catalogAddReadinglist(t, env)
	catalogEditCopy(t, env, env.repository+"/readinglist/page")
	imported := map[string]bool{peopleBundleID: false, "samples.substrate.reamde.dev/messaging": false, rlBundleID: true}
	for id, item := range catalogListing(t, env, env.token) {
		edited, held := imported[id]
		if !held {
			if item.Origin != "" || item.OriginVersion != 0 || item.Modified {
				t.Errorf("%s is not held and still carries provenance: %+v", id, item)
			}
			continue
		}
		b, _ := cat.ByID(id)
		if item.Origin != id || item.OriginVersion != b.Version || item.Modified != edited {
			t.Errorf("held %s provenance = origin %q v%d modified %v, want origin %s v%d modified %v",
				id, item.Origin, item.OriginVersion, item.Modified, id, b.Version, edited)
		}
	}
}

// catalogProvenance reads one catalog entry's installed flag and provenance
// off the listing AND its detail, which must agree.
func catalogProvenance(t *testing.T, env *dbEnv, id string) (installed bool, origin string) {
	t.Helper()
	listed := catalogEntry(t, env, id)
	detail := catalogDetail(t, env, env.token, id)
	if detail.Installed != listed.Installed || detail.Origin != listed.Origin {
		t.Errorf("%s: detail (installed=%v origin=%q) disagrees with the listing (installed=%v origin=%q)",
			id, detail.Installed, detail.Origin, listed.Installed, listed.Origin)
	}
	return listed.Installed, listed.Origin
}

// Two samples of one package word from two authorities both land as
// `<home>/readinglist`. The copy's ORIGIN says which one was imported, so only that
// entry reads installed and carries the provenance; the other is still on
// offer.
func TestCatalogMatchesAnImportedSampleByItsOrigin(t *testing.T) {
	t.Parallel()
	for _, imported := range []string{rlBundleID, otherReadinglistBundleID} {
		t.Run(imported, func(t *testing.T) {
			t.Parallel()
			env := catalogEnv(t, catalogWithOtherReadinglist(t))
			if imported == rlBundleID {
				catalogAddReadinglist(t, env)
			} else {
				catalogAdd(t, env, imported)
			}
			for _, id := range []string{rlBundleID, otherReadinglistBundleID} {
				installed, origin := catalogProvenance(t, env, id)
				if id == imported {
					if !installed || origin != imported {
						t.Errorf("imported %s: entry %s installed=%v origin=%q, want installed with its own origin", imported, id, installed, origin)
					}
					continue
				}
				if installed || origin != "" {
					t.Errorf("imported %s: entry %s installed=%v origin=%q, want on offer with no provenance", imported, id, installed, origin)
				}
			}
		})
	}
}

// A copy imported before the origin stamp existed carries none, and the
// landed id is all there is to match on: both entries read installed, as
// they did before, and neither carries provenance. The copy here is the
// package declared by hand at the landed id, which no stamp names.
func TestCatalogFallsBackToTheLandedIDForAnUnstampedCopy(t *testing.T) {
	t.Parallel()
	env := catalogEnv(t, catalogWithOtherReadinglist(t))
	pkg := env.repository + "/readinglist"
	if _, err := env.ds.ApplyVocabularyDocuments(t.Context(), substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.BundleManifest(pkg, map[string]any{"installs": []any{pkg + "/page"}}),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "page"}, map[string]any{
			"properties": map[string]any{"url": map[string]any{"type": "string"}},
		}),
	}); err != nil {
		t.Fatalf("declare the unstamped copy: %v", err)
	}
	for _, id := range []string{rlBundleID, otherReadinglistBundleID} {
		installed, origin := catalogProvenance(t, env, id)
		if !installed || origin != "" {
			t.Errorf("entry %s installed=%v origin=%q, want installed with no provenance", id, installed, origin)
		}
	}
}

// A sample INSTALLED verbatim is held under the SHIPPED id: the removed
// install route did that, and a repository that used it still holds one. No
// door installs a sample verbatim now (a provider's `requires:` leaves one
// unmet rather than install it), so this calls the catalog's Install
// directly to stand in for that stored copy. The listing has to see it, or
// the console offers an add that already ran.
func TestCatalogReportsAVerbatimInstalledSampleInstalled(t *testing.T) {
	t.Parallel()
	cat := catalogWithOtherReadinglist(t)
	env := catalogEnv(t, cat, withSamples("messaging"))
	if _, _, err := cat.Install(t.Context(), substrate.ActorPerson, rlBundleID, env.ds); err != nil {
		t.Fatalf("install the reading list verbatim: %v", err)
	}
	if !catalogEntry(t, env, rlBundleID).Installed {
		t.Error("a verbatim-installed sample reads as available, so the console offers it again")
	}
}

// A repository holding neither reads as neither: the two-id lookup must not
// make everything look installed.
func TestCatalogReportsAnUntakenSampleAvailable(t *testing.T) {
	t.Parallel()
	env := catalogEnv(t, catalogShipped(t))
	catalogAdd(t, env, googleBundleID)
	if !catalogEntry(t, env, googleBundleID).Installed {
		t.Fatal("the installed provider reads as available")
	}
	if catalogEntry(t, env, rlBundleID).Installed {
		t.Error("a sample this repository does not have reads as installed")
	}
}

// A SAMPLE's suggested mappings ride the listing with the state each has here
// (decision record 0049): this repository holds no provider, so the four the
// people sample ships are all `waiting`, each naming the package to install
// and both ends of the projection. A provider carries none at all.
func TestCatalogListsSuggestedMappingsAndTheirState(t *testing.T) {
	t.Parallel()
	env := catalogEnv(t, catalogShipped(t))
	listing := catalogListing(t, env, env.token)
	people := listing[peopleBundleID].SuggestedMappings
	for _, sm := range people {
		if sm.State != substrate.SuggestedMappingWaiting {
			t.Errorf("%s: state = %q, want waiting on a repository with no provider", sm.ID, sm.State)
		}
		if sm.Package == "" || sm.From == "" || sm.To == "" {
			t.Errorf("%s: the entry does not say what it waits for: %+v", sm.ID, sm)
		}
	}
	if len(people) != 4 {
		t.Errorf("the people sample reports %d suggested mappings, want 4", len(people))
	}
	if google := listing[googleBundleID].SuggestedMappings; len(google) != 0 {
		t.Errorf("the google provider reports %d suggested mappings, want none: a provider declares no mapping", len(google))
	}
}

// catalogPreviewCount counts the upgrade previews the catalog asks of one
// repository and tags each with the repository that counted it: the count is
// what says the catalog reused a plan, and the tag which repository's plan an
// entry was served, neither of which the engine's own answer shows. It is a
// comparable value over the one dataset, so the catalog's plan cache sees the
// same instance on every request.
type catalogPreviewCount struct {
	substrate.Dataset
	previews *atomic.Int64
}

func (d catalogPreviewCount) PlanBundleUpgrade(ctx context.Context, docs []map[string]any) (substrate.BundleUpgrade, error) {
	d.previews.Add(1)
	plan, err := d.Dataset.PlanBundleUpgrade(ctx, docs)
	plan.Blockers = append(plan.Blockers, "counted in "+d.Repository().Authority)
	return plan, err
}

// A catalog read reuses the upgrade preview while the repository's changelog
// head stands (issue #445): the listing read twice and the entry's own read
// count one preview between them, a write counts a fresh one, and another
// repository behind the same catalog counts and is served its own. Each
// repository holds one copy of the second reading-list sample, stamped with
// it as its origin.
func TestCatalogReadsReuseTheUpgradePreviewUntilTheHeadMoves(t *testing.T) {
	t.Parallel()
	env := catalogEnv(t, catalogWithOtherReadinglist(t))
	ctx := t.Context()
	other := testdb.RepositoryLabel(t) + "2.example.com"
	if _, err := env.svc.CreateRepository(ctx, other); err != nil {
		t.Fatalf("create the second repository: %v", err)
	}
	otherDS, err := env.svc.Dataset(ctx, other)
	if err != nil {
		t.Fatalf("open the second repository: %v", err)
	}
	_, otherToken, err := otherDS.MintToken(ctx, substrate.MintTokenInput{Label: "test"})
	if err != nil {
		t.Fatalf("mint into the second repository: %v", err)
	}
	tokens := map[string]string{env.repository: env.token, other: otherToken}
	for _, tok := range tokens {
		rec := env.do(t, http.MethodPost, "/api/v1/catalog/"+url.PathEscape(otherReadinglistBundleID)+"/add", tok, nil)
		wantStatus(t, rec, http.StatusOK)
	}
	previews := map[string]*atomic.Int64{env.repository: new(atomic.Int64), other: new(atomic.Int64)}
	env.wrap = func(ds substrate.Dataset) substrate.Dataset {
		return catalogPreviewCount{Dataset: ds, previews: previews[ds.Repository().Authority]}
	}
	listed := func(repository string) []string {
		t.Helper()
		item := catalogListing(t, env, tokens[repository])[otherReadinglistBundleID]
		if item.Upgrade == nil {
			t.Fatalf("the listing for %s carries no preview of %s", repository, otherReadinglistBundleID)
		}
		return item.Upgrade.Blockers
	}
	detailed := func(repository string) []string {
		t.Helper()
		item := catalogDetail(t, env, tokens[repository], otherReadinglistBundleID)
		if item.Upgrade == nil {
			t.Fatalf("the entry for %s carries no preview", repository)
		}
		return item.Upgrade.Blockers
	}
	want := func(repository string, got []string, n int64, when string) {
		t.Helper()
		if len(got) != 1 || got[0] != "counted in "+repository {
			t.Errorf("%s: %s is served the preview %q", when, repository, got)
		}
		if c := previews[repository].Load(); c != n {
			t.Errorf("%s: %s counted %d previews, want %d", when, repository, c, n)
		}
	}

	want(env.repository, listed(env.repository), 1, "the first listing")
	want(env.repository, listed(env.repository), 1, "a second listing with no write between")
	want(env.repository, detailed(env.repository), 1, "the entry's own read")
	want(other, listed(other), 1, "another repository's first listing")
	want(other, listed(other), 1, "another repository's second listing")

	env.put(t, env.repository+"/readinglist/page", "moved", map[string]any{"url": "https://example.com/moved"})
	want(env.repository, listed(env.repository), 2, "a listing after a write")
	want(env.repository, listed(env.repository), 2, "a second listing after the write")
	want(other, listed(other), 1, "another repository after the first one's write")

	// A history replaced at the same seq is another history: the generation
	// moved, so the seq alone does not reuse a plan.
	rotator, ok := env.svc.(interface {
		RotateHistoryGeneration(context.Context, string) (engine.RotateReport, error)
	})
	if !ok {
		t.Fatal("the engine cannot rotate a history generation")
	}
	before := vocabularyHead(t, env)
	if _, err := rotator.RotateHistoryGeneration(ctx, env.repository); err != nil {
		t.Fatalf("rotate the history generation: %v", err)
	}
	if after := vocabularyHead(t, env); after.Seq != before.Seq || after.Generation == before.Generation {
		t.Fatalf("the rotation left head %+v, was %+v", after, before)
	}
	want(env.repository, listed(env.repository), 3, "a listing after the history was replaced at the same seq")

	// Concurrent listings share the cached plan: each is served a copy, which
	// the race detector holds to (mise run test:race).
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			rec := env.do(t, http.MethodGet, "/api/v1/catalog", otherToken, nil)
			if rec.Code != http.StatusOK {
				t.Errorf("a concurrent listing answered %d", rec.Code)
			}
		})
	}
	wg.Wait()
	want(other, listed(other), 1, "listings read concurrently")
}

// catalogCountedStatuses counts the COUNTED bundle status reads anything asks
// of the repository: the count is the assertion, and no engine answer shows
// which read was asked.
type catalogCountedStatuses struct {
	substrate.Dataset
	counted *atomic.Int64
}

func (d catalogCountedStatuses) BundleStatuses(ctx context.Context) ([]substrate.BundleStatus, error) {
	d.counted.Add(1)
	return d.Dataset.BundleStatuses(ctx)
}

// A catalog read never asks for the live-row counts, in the listing, the
// entry, or the provider preview's requirement walk (issue #1086). Each count
// is an exact count of every live row of an installed kind, and the preview
// asked for all of them once per provider entry, so the listing grew with the
// data: about 30 s over a million live records. The repository holds google
// as binary N shipped it, and the handler ships binary N+1's, which moved it,
// so the preview runs and offers the upgrade.
func TestCatalogReadsNeverCountLiveRecords(t *testing.T) {
	t.Parallel()
	const header = kinds.ProvidersAuthority + "/google/bundle.yaml"
	moved := upgradeMovedTree(t, kinds.Providers(), map[string]func(string) string{
		header: func(doc string) string { return upgradePin(t, doc, 999) },
	})
	next, err := catalog.Load(catalog.ProviderRoot(moved), catalog.PublishedRoot(kinds.Catalogues()), catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatalf("load binary N+1's catalog: %v", err)
	}
	env := catalogEnv(t, next)
	if _, _, err := catalogShipped(t).Install(t.Context(), substrate.ActorPerson, googleBundleID, env.ds); err != nil {
		t.Fatalf("install binary N's google: %v", err)
	}
	counted := new(atomic.Int64)
	env.wrap = func(ds substrate.Dataset) substrate.Dataset {
		return catalogCountedStatuses{Dataset: ds, counted: counted}
	}

	item := catalogEntry(t, env, googleBundleID)
	if !item.Installed || item.Upgrade == nil || !item.Upgrade.Available {
		t.Fatalf("the listing does not offer the held %s its upgrade, so its preview never ran: %+v", googleBundleID, item.Upgrade)
	}
	if item := catalogDetail(t, env, env.token, googleBundleID); item.Upgrade == nil || !item.Upgrade.Available {
		t.Fatalf("the entry read does not offer the upgrade: %+v", item.Upgrade)
	}
	if n := counted.Load(); n != 0 {
		t.Errorf("the catalog reads asked for the counted bundle statuses %d times, want none", n)
	}
}
