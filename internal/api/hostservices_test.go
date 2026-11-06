package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
)

// hostServicesAnswer is the canned engine answer the handler tests read back.
func hostServicesAnswer() *substrate.HostServices {
	return &substrate.HostServices{
		Host: &substrate.HostInfo{Budget: &substrate.HostBudget{
			CentsPerDay: 200, UsedCents: 37, ResetsAt: time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC),
			ByService: []substrate.HostSpend{{Service: substrate.HostServiceLLM, Cents: 37}},
		}},
		Items: []substrate.HostService{{
			ID: "llm/openai", Service: substrate.HostServiceLLM, Label: "openai",
			Target: "substrate.reamde.dev/llm/provider/openai",
			Source: substrate.HostSourceHost, Readiness: substrate.HostReadyReady, Models: []string{"gpt-5"},
		}},
	}
}

// The owner reads the engine's answer, with the host's name taken from the
// site directory: the engine never reads the site.
func TestHostServicesAnswersTheOwnerWithTheSitesName(t *testing.T) {
	svc := newFakeService()
	svc.datasets[fakeRepository].hostServices = hostServicesAnswer()
	h := New(Config{Service: svc, Site: siteDir(t), HostDefaults: true})
	rec := serve(h, http.MethodGet, "/api/v1/host/services", map[string]string{"Authorization": "Bearer " + svc.token(fakeRepository)})
	wantStatus(t, rec, http.StatusOK)
	var got substrate.HostServices
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Host == nil || got.Host.Name != "Example" || got.Host.Budget.UsedCents != 37 {
		t.Fatalf("host = %+v", got.Host)
	}
	if len(got.Items) != 1 || got.Items[0].Source != substrate.HostSourceHost || got.Items[0].Models[0] != "gpt-5" {
		t.Fatalf("items = %+v", got.Items)
	}
}

// A scoped app token is refused at the route, and a full-access one reads it
// as the owner does (decision 0204).
func TestHostServicesRefusesAScopedAppToken(t *testing.T) {
	svc := newFakeService()
	svc.datasets[fakeRepository].hostServices = hostServicesAnswer()
	h := New(Config{Service: svc, HostDefaults: true})
	scoped := appToken(svc, fakeRepository, "read:"+fakeRepository+"/notes/note")
	rec := serve(h, http.MethodGet, "/api/v1/host/services", map[string]string{"Authorization": "Bearer " + scoped})
	wantStatus(t, rec, http.StatusForbidden)

	full := svc.tokenWith(fakeRepository, func(i *substrate.TokenInfo) {
		i.ClientID, i.ClientName = appClientID, "Notes"
	})
	rec = serve(h, http.MethodGet, "/api/v1/host/services", map[string]string{"Authorization": "Bearer " + full})
	wantStatus(t, rec, http.StatusOK)
}

// The scoped dataset refuses the read itself too, so a route that ever moves
// into the scoped surface does not hand it out.
func TestScopedDatasetRefusesHostServices(t *testing.T) {
	ds := newScopedDataset(newFakeDataset(fakeRepository), scope.Grant{}, substrate.Actor("app:example.com"))
	if _, err := ds.HostServices(t.Context()); err == nil {
		t.Fatal("the scoped dataset answered the host services read")
	}
}

// Discovery lists hostServices only where the server booted with a host file.
func TestDiscoveryListsHostServicesOnlyWithAHostFile(t *testing.T) {
	for _, loaded := range []bool{false, true} {
		h := New(Config{Service: newFakeService(), HostDefaults: loaded})
		rec := serve(h, http.MethodGet, "/.well-known/substrate/server.json", nil)
		wantStatus(t, rec, http.StatusOK)
		var doc struct {
			Features []featureInfo `json:"features"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		has := slices.ContainsFunc(doc.Features, func(f featureInfo) bool { return f.Name == substrate.FeatureHostServices })
		if has != loaded {
			t.Fatalf("host file %v: hostServices listed = %v", loaded, has)
		}
	}
	if slices.ContainsFunc(features, func(f featureInfo) bool { return f.Name == substrate.FeatureHostServices }) {
		t.Fatal("the shared feature list gained hostServices: a server with no host file would announce it")
	}
}

// The host's allowance answers 403 guard with the quota reason, so a client
// tells it from every other guard.
func TestHostBudgetRefusalCarriesTheQuotaReason(t *testing.T) {
	status, p := problemFor(fmt.Errorf("agent x: llm: %w: this repository has used 200 of the host's 200 cents", substrate.ErrHostBudget))
	if status != http.StatusForbidden || p.Code != codeGuard || p.Reason != substrate.ReasonQuota {
		t.Fatalf("problem = %d %+v", status, p)
	}
	if _, p := problemFor(fmt.Errorf("%w: spend cap", substrate.ErrGuard)); p.Reason != "" {
		t.Fatalf("a plain guard carries reason %q", p.Reason)
	}
}
