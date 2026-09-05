package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/geoah/substrate/kinds"
	"github.com/geoah/substrate/samples"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
)

const webBundleID = "samples.substrate.reamde.dev/web"

// statusErrDataset is a fake dataset that DOES run the bundle lifecycle but
// whose status reads fail — the repository/database fault review #11 must surface
// as an error rather than an empty installed set.
type statusErrDataset struct {
	*fakeDataset
	err error
}

func (d statusErrDataset) BundleStatuses(context.Context) ([]substrate.BundleStatus, error) {
	return nil, d.err
}

func (d statusErrDataset) BundleStatus(context.Context, string) (substrate.BundleStatus, error) {
	return substrate.BundleStatus{}, d.err
}

// The rest of the substrate.BundleOps seam is stubbed: this fake exists only
// to fail the status reads while still resolving as a bundle-running dataset.
func (d statusErrDataset) BundlePackage(context.Context, string) (string, error) {
	return "samples.substrate.reamde.dev/web", nil
}
func (d statusErrDataset) DisableBundle(context.Context, string) error { return nil }
func (d statusErrDataset) BindBundleInput(context.Context, string, string, string) error {
	return nil
}
func (d statusErrDataset) EnableBundle(context.Context, string) error    { return nil }
func (d statusErrDataset) UninstallBundle(context.Context, string) error { return nil }
func (d statusErrDataset) PurgeBundle(context.Context, string) (int, error) {
	return 0, nil
}

func (d statusErrDataset) StartOAuth(context.Context, substrate.Actor, string) (string, error) {
	return "", nil
}

func (d statusErrDataset) TypesImplementing(context.Context, string) ([]substrate.KindInfo, error) {
	return nil, nil
}

var _ substrate.BundleOps = statusErrDataset{}

// statusErrService authenticates into a statusErrDataset, so the handler
// resolves a dataset whose bundle-status seam fails.
type statusErrService struct {
	*fakeService
	err error
}

func (s *statusErrService) Authenticate(ctx context.Context, secret string) (substrate.Dataset, substrate.TokenInfo, error) {
	ds, info, err := s.fakeService.Authenticate(ctx, secret)
	if err != nil {
		return nil, info, err
	}
	return statusErrDataset{fakeDataset: ds.(*fakeDataset), err: s.err}, info, nil
}

// newStatusErrEnv is a catalog env whose dataset runs the bundle lifecycle but
// fails every status read.
func newStatusErrEnv(t *testing.T) *testEnv {
	t.Helper()
	cat, err := catalog.Load(catalog.ProviderRoot(kinds.Bundles()), catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	base := newFakeService()
	svc := &statusErrService{fakeService: base, err: errBoom}
	clock := &testClock{}
	return &testEnv{
		svc:   base,
		h:     New(Config{Service: svc, Now: clock.now, Catalog: cat}),
		clock: clock,
	}
}

// newCatalogEnv is a testEnv whose handler ships the real example catalog.
func newCatalogEnv(t *testing.T) *testEnv {
	t.Helper()
	cat, err := catalog.Load(catalog.ProviderRoot(kinds.Bundles()), catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	svc := newFakeService()
	clock := &testClock{}
	return &testEnv{
		svc:   svc,
		h:     New(Config{Service: svc, Now: clock.now, Catalog: cat}),
		clock: clock,
	}
}

// upgradeErrDataset installs the web bundle and then fails every upgrade
// PREVIEW: the listing must still serve, because the console's Registry (and
// its sidebar badge, on every page) reads it.
type upgradeErrDataset struct {
	*fakeDataset
	err error
}

// The installed bundle is a PROVIDER: only a provider is offered an upgrade
// preview at all (decision record 0048), so a sample here would leave the
// failing preview unreached and this test proving nothing.
func (d upgradeErrDataset) BundleStatuses(context.Context) ([]substrate.BundleStatus, error) {
	return []substrate.BundleStatus{{
		ID: googleBundleID, Name: "google", Authority: "providers.substrate.reamde.dev", Package: "google",
		Installed: true, Enabled: true,
	}}, nil
}

func (d upgradeErrDataset) BundleStatus(context.Context, string) (substrate.BundleStatus, error) {
	return substrate.BundleStatus{ID: googleBundleID, Installed: true, Enabled: true}, nil
}

func (d upgradeErrDataset) PlanBundleUpgrade(context.Context, []map[string]any) (substrate.BundleUpgrade, error) {
	return substrate.BundleUpgrade{}, d.err
}

func (d upgradeErrDataset) BundlePackage(context.Context, string) (string, error) {
	return googleBundleID, nil
}
func (d upgradeErrDataset) DisableBundle(context.Context, string) error { return nil }
func (d upgradeErrDataset) BindBundleInput(context.Context, string, string, string) error {
	return nil
}
func (d upgradeErrDataset) EnableBundle(context.Context, string) error    { return nil }
func (d upgradeErrDataset) UninstallBundle(context.Context, string) error { return nil }
func (d upgradeErrDataset) PurgeBundle(context.Context, string) (int, error) {
	return 0, nil
}

func (d upgradeErrDataset) StartOAuth(context.Context, substrate.Actor, string) (string, error) {
	return "", nil
}

func (d upgradeErrDataset) TypesImplementing(context.Context, string) ([]substrate.KindInfo, error) {
	return nil, nil
}

var (
	_ substrate.BundleOps            = upgradeErrDataset{}
	_ substrate.BundleUpgradePlanner = upgradeErrDataset{}
)

type upgradeErrService struct {
	*fakeService
	err error
}

func (s *upgradeErrService) Authenticate(ctx context.Context, secret string) (substrate.Dataset, substrate.TokenInfo, error) {
	ds, info, err := s.fakeService.Authenticate(ctx, secret)
	if err != nil {
		return nil, info, err
	}
	return upgradeErrDataset{fakeDataset: ds.(*fakeDataset), err: s.err}, info, nil
}

func newUpgradeErrEnv(t *testing.T) *testEnv {
	t.Helper()
	cat, err := catalog.Load(catalog.ProviderRoot(kinds.Bundles()), catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	base := newFakeService()
	svc := &upgradeErrService{fakeService: base, err: errBoom}
	clock := &testClock{}
	return &testEnv{
		svc:   base,
		h:     New(Config{Service: svc, Now: clock.now, Catalog: cat}),
		clock: clock,
	}
}

// A preview that fails costs that entry its upgrade offer and NOTHING else.
// The offer is an extra; the listing is the promise, and the console reads it
// on every page.
func TestCatalogListSurvivesAFailedUpgradePreview(t *testing.T) {
	env := newUpgradeErrEnv(t)
	tok := env.svc.token("geoah")

	rec := env.do(t, http.MethodGet, "/api/v1/catalog", tok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog list = %d, want 200 (a failed preview must not blank the listing): %s", rec.Code, rec.Body)
	}
	body := decodeJSON[struct {
		Items []struct {
			ID      string                   `json:"id"`
			Upgrade *substrate.BundleUpgrade `json:"upgrade"`
		} `json:"items"`
	}](t, rec)
	if len(body.Items) == 0 {
		t.Fatal("the listing is empty")
	}
	for _, item := range body.Items {
		if item.Upgrade != nil {
			t.Errorf("bundle %s carries an upgrade from a failed preview", item.ID)
		}
	}

	rec = env.do(t, http.MethodGet, "/api/v1/catalog/"+url.PathEscape(googleBundleID), tok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("catalog detail = %d, want 200: %s", rec.Code, rec.Body)
	}
}

func TestCatalogListReturnsShippedBundles(t *testing.T) {
	env := newCatalogEnv(t)
	tok := env.svc.token("geoah")
	rec := env.do(t, http.MethodGet, "/api/v1/catalog", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	body := decodeJSON[struct {
		Items []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			Authority string `json:"authority"`
			Package   string `json:"package"`
			Version   int64  `json:"version"`
			Installed bool   `json:"installed"`
		} `json:"items"`
	}](t, rec)
	var found bool
	for _, item := range body.Items {
		if item.ID == webBundleID {
			found = true
			if item.Name != "web" || item.Authority != "samples.substrate.reamde.dev" || item.Package != "web" || item.Version != 8 {
				t.Errorf("web entry fields = %+v", item)
			}
			if item.Installed {
				t.Error("web must not read installed on a fake dataset with no bundle lifecycle")
			}
		}
	}
	if !found {
		t.Fatalf("web bundle not in catalog list: %+v", body.Items)
	}
}

const googleBundleID = "providers.substrate.reamde.dev/google"

// The catalog list carries each entry's TIER on the wire: the console's two
// sections and the door each row offers are read from it, so google is a
// provider and the web sample is a sample (decision record 0048).
func TestCatalogListCarriesTheTier(t *testing.T) {
	env := newCatalogEnv(t)
	tok := env.svc.token("geoah")
	rec := env.do(t, http.MethodGet, "/api/v1/catalog", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	body := decodeJSON[struct {
		Items []struct {
			ID   string `json:"id"`
			Tier string `json:"tier"`
		} `json:"items"`
	}](t, rec)
	want := map[string]string{
		googleBundleID: substrate.TierProvider,
		webBundleID:    substrate.TierSample,
	}
	seen := map[string]bool{}
	for _, item := range body.Items {
		if w, ok := want[item.ID]; ok {
			seen[item.ID] = true
			if item.Tier != w {
				t.Errorf("%s tier = %q, want %q", item.ID, item.Tier, w)
			}
		}
		if item.Tier == "" {
			t.Errorf("bundle %q carries no tier, so the console cannot place it", item.ID)
		}
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("bundle %q missing from catalog list", id)
		}
	}
}

// The two doors are not interchangeable: a PROVIDER installs under the
// authority that publishes it, so importing one is refused and the refusal
// names the verb that does work.
func TestCatalogImportRefusesAProvider(t *testing.T) {
	env := newCatalogEnv(t)
	tok := env.svc.token("geoah")
	rec := env.do(t, http.MethodPost, "/api/v1/catalog/"+url.PathEscape(googleBundleID)+"/import", tok, nil)
	wantErrorCode(t, rec, http.StatusUnprocessableEntity, codeValidation)
	if body := rec.Body.String(); !strings.Contains(body, "install") {
		t.Errorf("the refusal does not name the verb that works: %s", body)
	}
}

// Import is an owner action, exactly as install is: a request attributed to a
// machine is refused before the closure is touched.
func TestCatalogImportRefusesNonOwner(t *testing.T) {
	env := newCatalogEnv(t)
	tok := env.svc.token("geoah")
	rec := env.do(t, http.MethodPost, "/api/v1/catalog/"+url.PathEscape(webBundleID)+"/import", tok, nil,
		actorHeader, "reader.substrate.reamde.dev")
	wantErrorCode(t, rec, http.StatusForbidden, codeForbidden)
}

// An unknown id is refused by the CATALOG, not by the router, so the message
// names the bundle. A bare 404 would also come back from a path that routes
// nowhere, which is why the body is what this asserts.
func TestCatalogImportUnknownNamesTheBundle(t *testing.T) {
	env := newCatalogEnv(t)
	tok := env.svc.token("geoah")
	rec := env.do(t, http.MethodPost, "/api/v1/catalog/nope.example.com%2Fnothing/import", tok, nil)
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
	if body := rec.Body.String(); !strings.Contains(body, "nope.example.com/nothing") {
		t.Errorf("the refusal does not name the bundle asked for: %s", body)
	}
}

func TestCatalogDetailPreviewsTheClosure(t *testing.T) {
	env := newCatalogEnv(t)
	tok := env.svc.token("geoah")
	rec := env.do(t, http.MethodGet, "/api/v1/catalog/"+url.PathEscape(webBundleID), tok, nil)
	wantStatus(t, rec, http.StatusOK)
	item := decodeJSON[catalogItem](t, rec)
	if len(item.Closure.Functions) != 4 || len(item.Closure.Records) != 4 {
		t.Errorf("closure = %+v", item.Closure)
	}
}

func TestCatalogDetailUnknownIs404(t *testing.T) {
	env := newCatalogEnv(t)
	tok := env.svc.token("geoah")
	rec := env.do(t, http.MethodGet, "/api/v1/catalog/nope.bundles.substrate.reamde.dev", tok, nil)
	wantErrorCode(t, rec, http.StatusNotFound, codeNotFound)
}

// A status-read fault is surfaced as an error, never a 200 that silently
// reports an installed integration as available — on both the
// list and the detail read.
func TestCatalogListSurfacesStatusReadFailure(t *testing.T) {
	env := newStatusErrEnv(t)
	tok := env.svc.token("geoah")
	rec := env.do(t, http.MethodGet, "/api/v1/catalog", tok, nil)
	wantErrorCode(t, rec, http.StatusInternalServerError, codeInternal)
}

func TestCatalogDetailSurfacesStatusReadFailure(t *testing.T) {
	env := newStatusErrEnv(t)
	tok := env.svc.token("geoah")
	rec := env.do(t, http.MethodGet, "/api/v1/catalog/"+url.PathEscape(webBundleID), tok, nil)
	wantErrorCode(t, rec, http.StatusInternalServerError, codeInternal)
}

// Install is an owner action: a request ATTRIBUTED to something other than
// the owner is refused before the closure is touched. The actor rides on the
// header now — attribution, not authorization — so this is the same refusal
// reached a shorter way.
func TestCatalogInstallRefusesNonOwner(t *testing.T) {
	env := newCatalogEnv(t)
	tok := env.svc.token("geoah")
	rec := env.do(t, http.MethodPost, "/api/v1/catalog/"+url.PathEscape(webBundleID)+"/install", tok, nil,
		actorHeader, "reader.substrate.reamde.dev")
	wantErrorCode(t, rec, http.StatusForbidden, codeForbidden)
}

// heldDataset reports one bundle installed, whichever id the test names. It
// exists to ask the catalog read the one question the two tiers make
// ambiguous: which id counts as "this repository has it".
type heldDataset struct {
	*fakeDataset
	id string
}

func (d heldDataset) BundleStatuses(context.Context) ([]substrate.BundleStatus, error) {
	return []substrate.BundleStatus{{
		ID: d.id, Name: "web", Installed: true, Enabled: true,
	}}, nil
}

func (d heldDataset) BundleStatus(_ context.Context, id string) (substrate.BundleStatus, error) {
	if id != d.id {
		return substrate.BundleStatus{}, fmt.Errorf("%w: bundle %q", substrate.ErrNotFound, id)
	}
	return substrate.BundleStatus{ID: d.id, Installed: true, Enabled: true}, nil
}

func (d heldDataset) BundlePackage(context.Context, string) (string, error) { return d.id, nil }
func (d heldDataset) DisableBundle(context.Context, string) error           { return nil }
func (d heldDataset) BindBundleInput(context.Context, string, string, string) error {
	return nil
}
func (d heldDataset) EnableBundle(context.Context, string) error    { return nil }
func (d heldDataset) UninstallBundle(context.Context, string) error { return nil }
func (d heldDataset) PurgeBundle(context.Context, string) (int, error) {
	return 0, nil
}

func (d heldDataset) StartOAuth(context.Context, substrate.Actor, string) (string, error) {
	return "", nil
}

func (d heldDataset) TypesImplementing(context.Context, string) ([]substrate.KindInfo, error) {
	return nil, nil
}

var _ substrate.BundleOps = heldDataset{}

type heldService struct {
	*fakeService
	id string
}

func (s *heldService) Authenticate(ctx context.Context, secret string) (substrate.Dataset, substrate.TokenInfo, error) {
	ds, info, err := s.fakeService.Authenticate(ctx, secret)
	if err != nil {
		return nil, info, err
	}
	return heldDataset{fakeDataset: ds.(*fakeDataset), id: s.id}, info, nil
}

// newHeldEnv is a catalog env whose repository holds exactly one bundle, under
// the id given.
func newHeldEnv(t *testing.T, id string) *testEnv {
	t.Helper()
	cat, err := catalog.Load(catalog.ProviderRoot(kinds.Bundles()), catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	base := newFakeService()
	svc := &heldService{fakeService: base, id: id}
	clock := &testClock{}
	return &testEnv{
		svc:   base,
		h:     New(Config{Service: svc, Now: clock.now, Catalog: cat}),
		clock: clock,
	}
}

// installedFor reads one catalog entry's `installed` flag off the listing.
func installedFor(t *testing.T, env *testEnv, id string) bool {
	t.Helper()
	tok := env.svc.token("geoah")
	rec := env.do(t, http.MethodGet, "/api/v1/catalog", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	body := decodeJSON[struct {
		Items []struct {
			ID        string `json:"id"`
			Installed bool   `json:"installed"`
		} `json:"items"`
	}](t, rec)
	for _, item := range body.Items {
		if item.ID == id {
			return item.Installed
		}
	}
	t.Fatalf("bundle %q missing from the catalog listing", id)
	return false
}

// A sample this repository IMPORTED is held under the rehomed id, so that is
// what the listing has to look for.
func TestCatalogReportsAnImportedSampleInstalled(t *testing.T) {
	// The fake repository's authority is <name>.example.com.
	env := newHeldEnv(t, "geoah.example.com/web")
	if !installedFor(t, env, webBundleID) {
		t.Error("an imported sample reads as available, so the console offers it again")
	}
}

// A sample INSTALLED verbatim is held under the SHIPPED id, which is still a
// door while the providers name sample packages under `requires:`. The listing
// has to see that one too, or the console offers an install that already ran.
func TestCatalogReportsAVerbatimInstalledSampleInstalled(t *testing.T) {
	env := newHeldEnv(t, webBundleID)
	if !installedFor(t, env, webBundleID) {
		t.Error("a verbatim-installed sample reads as available, so the console offers it again")
	}
}

// A repository holding neither reads as neither: the two-id lookup must not
// make everything look installed.
func TestCatalogReportsAnUntakenSampleAvailable(t *testing.T) {
	env := newHeldEnv(t, googleBundleID)
	if installedFor(t, env, webBundleID) {
		t.Error("a sample this repository does not have reads as installed")
	}
}
