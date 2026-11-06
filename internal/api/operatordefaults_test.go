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

// operatorDefaultsAnswer is the canned engine answer the handler tests read
// back.
func operatorDefaultsAnswer() *substrate.OperatorDefaults {
	return &substrate.OperatorDefaults{
		Operator: &substrate.OperatorInfo{Name: "Example", Allowance: &substrate.OperatorAllowance{
			CentsPerDay: 200, UsedCents: 37, ResetsAt: time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC),
			BySlot: []substrate.OperatorSpend{{Slot: substrate.SlotLLM, Cents: 37}},
		}},
		Items: []substrate.OperatorDefault{{
			ID: "llm/openai", Slot: substrate.SlotLLM, Label: "openai",
			Target: "substrate.reamde.dev/llm/provider/openai",
			Source: substrate.SourceOwn, OperatorOffers: true, Readiness: substrate.ReadinessReady,
		}},
	}
}

// The owner reads the engine's answer as it stands at
// GET /api/v1/operator/defaults: the site's name, the allowance, and an item
// the operator offers while the person's own key serves it.
func TestOperatorDefaultsAnswersTheOwner(t *testing.T) {
	svc := newFakeService()
	svc.datasets[fakeRepository].operatorDefaults = operatorDefaultsAnswer()
	h := New(Config{Service: svc, OperatorDefaults: true})
	rec := serve(h, http.MethodGet, "/api/v1/operator/defaults", map[string]string{"Authorization": "Bearer " + svc.token(fakeRepository)})
	wantStatus(t, rec, http.StatusOK)
	var got substrate.OperatorDefaults
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Operator == nil || got.Operator.Name != "Example" || got.Operator.Allowance.UsedCents != 37 ||
		len(got.Operator.Allowance.BySlot) != 1 || got.Operator.Allowance.BySlot[0].Slot != substrate.SlotLLM {
		t.Fatalf("operator = %+v", got.Operator)
	}
	if len(got.Items) != 1 || got.Items[0].Source != substrate.SourceOwn || !got.Items[0].OperatorOffers {
		t.Fatalf("items = %+v", got.Items)
	}
}

// A scoped app token is refused at the route, and a full-access one reads it
// as the owner does (decision 0204).
func TestOperatorDefaultsRefusesAScopedAppToken(t *testing.T) {
	svc := newFakeService()
	svc.datasets[fakeRepository].operatorDefaults = operatorDefaultsAnswer()
	h := New(Config{Service: svc, OperatorDefaults: true})
	scoped := appToken(svc, fakeRepository, "read:"+fakeRepository+"/notes/note")
	rec := serve(h, http.MethodGet, "/api/v1/operator/defaults", map[string]string{"Authorization": "Bearer " + scoped})
	wantStatus(t, rec, http.StatusForbidden)

	full := svc.tokenWith(fakeRepository, func(i *substrate.TokenInfo) {
		i.ClientID, i.ClientName = appClientID, "Notes"
	})
	rec = serve(h, http.MethodGet, "/api/v1/operator/defaults", map[string]string{"Authorization": "Bearer " + full})
	wantStatus(t, rec, http.StatusOK)
}

// The scoped dataset refuses the read itself too, so a route that ever moves
// into the scoped surface does not hand it out.
func TestScopedDatasetRefusesOperatorDefaults(t *testing.T) {
	ds := newScopedDataset(newFakeDataset(fakeRepository), scope.Grant{}, substrate.Actor("app:example.com"))
	if _, err := ds.OperatorDefaults(t.Context()); err == nil {
		t.Fatal("the scoped dataset answered the operator defaults read")
	}
}

// Discovery lists operatorDefaults only where the server booted with a
// defaults file.
func TestDiscoveryListsOperatorDefaultsOnlyWithADefaultsFile(t *testing.T) {
	for _, loaded := range []bool{false, true} {
		h := New(Config{Service: newFakeService(), OperatorDefaults: loaded})
		rec := serve(h, http.MethodGet, "/.well-known/substrate/server.json", nil)
		wantStatus(t, rec, http.StatusOK)
		var doc struct {
			Features []featureInfo `json:"features"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		has := slices.ContainsFunc(doc.Features, func(f featureInfo) bool { return f.Name == "operatorDefaults" })
		if has != loaded {
			t.Fatalf("defaults file %v: operatorDefaults listed = %v", loaded, has)
		}
	}
	if slices.ContainsFunc(features, func(f featureInfo) bool { return f.Name == substrate.FeatureOperatorDefaults }) {
		t.Fatal("the shared feature list gained operatorDefaults: a server with no defaults file would announce it")
	}
}

// A call past the allowance answers 403 guard with the quota reason, so a
// client tells it from every other guard.
func TestAllowanceRefusalCarriesTheQuotaReason(t *testing.T) {
	status, p := problemFor(fmt.Errorf("agent x: llm: %w: today's allowance from Example is used up", substrate.ErrAllowance))
	if status != http.StatusForbidden || p.Code != codeGuard || p.Reason != substrate.ReasonQuota {
		t.Fatalf("problem = %d %+v", status, p)
	}
	if _, p := problemFor(fmt.Errorf("%w: spend cap", substrate.ErrGuard)); p.Reason != "" {
		t.Fatalf("a plain guard carries reason %q", p.Reason)
	}
}
