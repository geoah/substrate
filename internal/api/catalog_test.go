package api

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/geoah/substrate/kinds"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
)

const webBundleID = "web.bundles.substrate.reamde.dev/web"

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
func (d statusErrDataset) BundleAuthority(context.Context, string) (string, error) {
	return "web.bundles.substrate.reamde.dev", nil
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
	cat, err := catalog.Load(kinds.Bundles())
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
	cat, err := catalog.Load(kinds.Bundles())
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

func (d upgradeErrDataset) BundleStatuses(context.Context) ([]substrate.BundleStatus, error) {
	return []substrate.BundleStatus{{
		ID: webBundleID, Name: "web", Authority: "web.bundles.substrate.reamde.dev",
		Installed: true, Enabled: true,
	}}, nil
}

func (d upgradeErrDataset) BundleStatus(context.Context, string) (substrate.BundleStatus, error) {
	return substrate.BundleStatus{ID: webBundleID, Installed: true, Enabled: true}, nil
}

func (d upgradeErrDataset) PlanBundleUpgrade(context.Context, []map[string]any) (substrate.BundleUpgrade, error) {
	return substrate.BundleUpgrade{}, d.err
}

func (d upgradeErrDataset) BundleAuthority(context.Context, string) (string, error) {
	return "web.bundles.substrate.reamde.dev", nil
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
	cat, err := catalog.Load(kinds.Bundles())
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

	rec = env.do(t, http.MethodGet, "/api/v1/catalog/"+url.PathEscape(webBundleID), tok, nil)
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
			Version   int64  `json:"version"`
			Installed bool   `json:"installed"`
		} `json:"items"`
	}](t, rec)
	var found bool
	for _, item := range body.Items {
		if item.ID == webBundleID {
			found = true
			if item.Name != "web" || item.Authority != "web.bundles.substrate.reamde.dev" || item.Version != 8 {
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

const googleBundleID = "google.bundles.substrate.reamde.dev/google"

// The catalog list response carries the curated integration facet on the wire:
// the google provider bundle is integration=true, the web bundle false, so the
// console can render the Integration badge and filter.
func TestCatalogListCarriesIntegrationFacet(t *testing.T) {
	env := newCatalogEnv(t)
	tok := env.svc.token("geoah")
	rec := env.do(t, http.MethodGet, "/api/v1/catalog", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	body := decodeJSON[struct {
		Items []struct {
			ID          string `json:"id"`
			Integration bool   `json:"integration"`
		} `json:"items"`
	}](t, rec)
	want := map[string]bool{googleBundleID: true, webBundleID: false}
	seen := map[string]bool{}
	for _, item := range body.Items {
		if w, ok := want[item.ID]; ok {
			seen[item.ID] = true
			if item.Integration != w {
				t.Errorf("%s integration = %v, want %v", item.ID, item.Integration, w)
			}
		}
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("bundle %q missing from catalog list", id)
		}
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
