package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// shippedUpgradeDataset is a fake that previews the boot upgrade: it answers
// whatever the test seeded, so the handler is held to serving the engine's
// answer verbatim, not to computing one.
type shippedUpgradeDataset struct {
	*fakeDataset
	plans []substrate.ShippedUpgrade
	err   error
}

func (d shippedUpgradeDataset) PlanShippedUpgrade(context.Context) ([]substrate.ShippedUpgrade, error) {
	return d.plans, d.err
}

var _ substrate.ShippedUpgradePlanner = shippedUpgradeDataset{}

type shippedUpgradeService struct {
	*fakeService
	plans []substrate.ShippedUpgrade
	err   error
}

func (s *shippedUpgradeService) Authenticate(ctx context.Context, secret string) (substrate.Dataset, substrate.TokenInfo, error) {
	ds, info, err := s.fakeService.Authenticate(ctx, secret)
	if err != nil {
		return nil, info, err
	}
	return shippedUpgradeDataset{fakeDataset: ds.(*fakeDataset), plans: s.plans, err: s.err}, info, nil
}

func newShippedUpgradeEnv(t *testing.T, plans []substrate.ShippedUpgrade, err error) *testEnv {
	t.Helper()
	base := newFakeService()
	svc := &shippedUpgradeService{fakeService: base, plans: plans, err: err}
	clock := &testClock{}
	return &testEnv{
		svc:   base,
		h:     New(Config{Service: svc, Now: clock.now}),
		clock: clock,
	}
}

// A withheld core upgrade is readable by a repository token: the package, the
// versions the boot compared and the guard lines it refused on, the same
// shape a catalog entry's `upgrade` carries.
func TestVocabularyUpgradeServesTheShippedPreview(t *testing.T) {
	want := []substrate.ShippedUpgrade{{
		Package: corePackage,
		Upgrade: substrate.BundleUpgrade{
			Available: true, From: 16, To: 17,
			Changes:  []substrate.BundleUpgradeChange{{Kind: "kind", ID: corePackage + "/llmprovider", From: 8, To: 9}},
			Blockers: []string{`type ` + corePackage + `/llmprovider: property "label" dropped while 1 live records still carry it — null it on them first`},
		},
	}}
	env := newShippedUpgradeEnv(t, want, nil)
	tok := env.svc.token(fakeRepository)

	rec := env.do(t, http.MethodGet, "/api/v1/vocabulary/upgrade", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	body := decodeJSON[substrate.OperationalList[substrate.ShippedUpgrade]](t, rec)
	if len(body.Items) != 1 {
		t.Fatalf("items = %+v, want one", body.Items)
	}
	got := body.Items[0]
	if got.Package != corePackage || !got.Upgrade.Available || got.Upgrade.From != 16 || got.Upgrade.To != 17 {
		t.Fatalf("entry = %+v", got)
	}
	if len(got.Upgrade.Blockers) != 1 || got.Upgrade.Blockers[0] != want[0].Upgrade.Blockers[0] {
		t.Fatalf("blockers = %q", got.Upgrade.Blockers)
	}
	if len(got.Upgrade.Changes) != 1 || got.Upgrade.Changes[0].ID != corePackage+"/llmprovider" {
		t.Fatalf("changes = %+v", got.Upgrade.Changes)
	}

	// No token, no read: the preview is a repository read like the catalog.
	rec = env.do(t, http.MethodGet, "/api/v1/vocabulary/upgrade", "", nil)
	wantStatus(t, rec, http.StatusUnauthorized)
}

// A repository with nothing to upgrade answers an empty list, not an absent
// one, so a client can tell "up to date" from "no answer".
func TestVocabularyUpgradeAnswersAnEmptyList(t *testing.T) {
	env := newShippedUpgradeEnv(t, nil, nil)
	rec := env.do(t, http.MethodGet, "/api/v1/vocabulary/upgrade", env.svc.token(fakeRepository), nil)
	wantStatus(t, rec, http.StatusOK)
	if got := rec.Body.String(); got != "{\"items\":[]}\n" {
		t.Fatalf("body = %s, want an empty items list", got)
	}
}

// A preview that fails is the ordinary substrate error, not a 200 with
// nothing in it.
func TestVocabularyUpgradeReportsAFailedPreview(t *testing.T) {
	env := newShippedUpgradeEnv(t, nil, errBoom)
	rec := env.do(t, http.MethodGet, "/api/v1/vocabulary/upgrade", env.svc.token(fakeRepository), nil)
	wantErrorCode(t, rec, http.StatusInternalServerError, codeInternal)
}

// A dataset that does not preview the boot upgrade answers 501, the same way
// every other optional seam does.
func TestVocabularyUpgradeIsUnsupportedWithoutThePlanner(t *testing.T) {
	env := newTestEnv(t)
	rec := env.do(t, http.MethodGet, "/api/v1/vocabulary/upgrade", env.svc.token(fakeRepository), nil)
	wantErrorCode(t, rec, http.StatusNotImplemented, codeUnsupported)
}
