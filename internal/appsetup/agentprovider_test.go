package appsetup

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/catalog"

	"github.com/geoah/substrate/internal/substrate"
)

func TestProviderOptionsAndChoices(t *testing.T) {
	agents := []substrate.AuthorizationProviderAgent{{Agent: "coach.example.com/coach/run", Models: []string{"a", "b"}, SpendCentsPerDay: 100}}
	providers := []*substrate.Record{
		{ID: "fits", Title: "My AI", Properties: map[string]any{"pricing": []any{map[string]any{"model": "gateway/a"}}}},
		{ID: "wrong", Properties: map[string]any{"pricing": []any{map[string]any{"model": "unknown"}}}},
	}
	options := providerOptions(agents, providers)
	if len(options) != 1 || options[0].ID != "fits" || options[0].Models[agents[0].Agent] != "gateway/a" {
		t.Fatalf("%+v", options)
	}
	rows := []substrate.AuthorizationSetup{{Package: &substrate.AuthorizationPackage{ProviderInputs: []substrate.AuthorizationProviderInput{{Package: "coach.example.com/coach", Input: "model", Options: options}}}}}
	if err := SelectProviders(rows, nil); err == nil {
		t.Fatal("missing choice admitted")
	}
	choice := substrate.AuthorizationProviderChoice{Package: "coach.example.com/coach", Input: "model", Provider: "wrong"}
	if err := SelectProviders(rows, []substrate.AuthorizationProviderChoice{choice}); err == nil {
		t.Fatal("unoffered choice admitted")
	}
	choice.Provider = "fits"
	if err := SelectProviders(rows, []substrate.AuthorizationProviderChoice{choice}); err != nil {
		t.Fatal(err)
	}
}

type providerDataset struct {
	*fakeDataset
	providers []*substrate.Record
	bindings  []substrate.AuthorizationProviderChoice
}

func (d *providerDataset) List(context.Context, substrate.Query) (*substrate.Page, error) {
	return &substrate.Page{Records: d.providers}, nil
}

func (d *providerDataset) BindBundleInput(_ context.Context, pkg, input, record string) error {
	d.bindings = append(d.bindings, substrate.AuthorizationProviderChoice{Package: pkg, Input: input, Provider: record})
	return nil
}

func TestProviderPlanAndBinding(t *testing.T) {
	const pkg = "coach.example.com/coach"
	raw := closure(pkg, 1, nil, `kind: substrate.reamde.dev/core/agent
metadata:
  id: coach.example.com/coach/run
data:
  authority: coach.example.com
  package: coach
  description: Coach
  prompt: Coach.
  providerInput: model
  models: [a]
  budgets:
    spendCentsPerDay: 100
`)
	raw = strings.Replace(raw, "  description: The coach package.", "  description: The coach package.\n  inputs:\n    model:\n      kind: substrate.reamde.dev/llm/provider", 1)
	cat, err := catalog.Load(catalog.ProviderRoot(fstest.MapFS{"coach.example.com/coach/bundle.yaml": {Data: []byte(raw)}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Warnings()) != 0 {
		t.Fatal(cat.Warnings())
	}
	ds := &providerDataset{fakeDataset: newFakeDataset()}
	details := []substrate.AuthorizationDetail{pkgDetail(pkg, 1)}
	rows, _, err := Plan(context.Background(), ds, cat, details, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Blocked == nil || rows[0].Blocked.Reason != substrate.BlockNoModel {
		t.Fatalf("%+v", rows)
	}
	ds.providers = []*substrate.Record{{ID: "fits", Properties: map[string]any{"pricing": []any{map[string]any{"model": "a"}}}}}
	rows, _, err = Plan(context.Background(), ds, cat, details, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Blocked != nil || rows[0].Package.ProviderInputs[0].Selected != "fits" {
		t.Fatalf("%+v", rows)
	}
	if err := NewRunner(cat).setUpRow(context.Background(), ds, substrate.ActorAPI, details[0], rows[0]); err != nil {
		t.Fatal(err)
	}
	if len(ds.bindings) != 1 || ds.bindings[0].Provider != "fits" {
		t.Fatalf("%+v", ds.bindings)
	}
}
