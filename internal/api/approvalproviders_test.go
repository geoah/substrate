package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
)

func TestApprovalIgnoresExcludedAddonProviderChoice(t *testing.T) {
	const raw = `kind: substrate.reamde.dev/core/package
metadata:
  id: example.org/training
data:
  authority: example.org
  package: training
  version: 3
---
kind: substrate.reamde.dev/core/bundle
metadata:
  id: example.org/training
data:
  authority: example.org
  package: training
  description: Coach.
  inputs:
    model:
      kind: substrate.reamde.dev/llm/provider
  installs: [example.org/training/run]
---
kind: substrate.reamde.dev/core/agent
metadata:
  id: example.org/training/run
data:
  authority: example.org
  package: training
  description: Coach.
  prompt: Coach.
  providerInput: model
  models: [unpriced]
  budgets:
    spendCentsPerDay: 100
`
	for _, sent := range []bool{false, true} {
		t.Run(map[bool]string{false: "no choice", true: "irrelevant choice"}[sent], func(t *testing.T) {
			e := newSetupEnv(t)
			cat, err := catalog.Load(catalog.ProviderRoot(fstest.MapFS{"training/bundle.yaml": {Data: []byte(raw)}}))
			if err != nil {
				t.Fatal(err)
			}
			b, ok := cat.ByID(dpTraining)
			if !ok || len(cat.Warnings()) != 0 {
				t.Fatal(cat.Warnings())
			}
			packages := newPublishingCatalog(t, e.catalog)
			packages.training = b.FromDocument(catalog.Origin{Authority: dpAuthority, DocumentURL: dpDocURL, AddonFor: healthPkg})
			e.repo.statuses[healthPkg] = substrate.BundleStatus{ID: healthPkg, Installed: true, Enabled: true, Version: 1}
			e.h = New(Config{Service: e.svc, ClientDocuments: e.docs, Catalog: e.catalog, Packages: packages})
			id := e.park(setupRequest("read:"+dpTraining+"/*", `[{"type":"package","package":"example.org/training","version":3}]`))
			view := e.view(id)
			if len(view.Setup) != 1 || view.Setup[0].Blocked == nil || view.Setup[0].Blocked.Reason != substrate.BlockNoModel {
				t.Fatalf("expected no-model add-on: %+v", view.Setup)
			}
			days := 30
			approval := substrate.AuthorizationApproval{LifetimeDays: &days, Scopes: []string{"read:" + dpTraining + "/*"}, PlanHash: view.PlanHash}
			if sent {
				approval.ProviderChoices = []substrate.AuthorizationProviderChoice{{Package: dpTraining, Input: "model", Provider: "irrelevant"}}
			}
			body, err := json.Marshal(approval)
			if err != nil {
				t.Fatal(err)
			}
			result := e.do(http.MethodPost, "/api/v1/authorizations/"+id, e.owner, body, "application/json")
			wantStatus(t, result, http.StatusOK)
			if len(e.repo.installs) != 0 {
				t.Fatalf("excluded add-on installed: %v", e.repo.installs)
			}
		})
	}
}
