package appsetup

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/catalog"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
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
	rows := []substrate.AuthorizationSetup{{Action: substrate.SetupInstall, Package: &substrate.AuthorizationPackage{ProviderInputs: []substrate.AuthorizationProviderInput{{Package: "coach.example.com/coach", Input: "model", Options: options}}}}}
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
	b, _ := cat.ByID(pkg)
	published := &publishing{Catalog: testCatalog(t), listed: map[string]*catalog.Bundle{
		pkg: b.FromDocument(catalog.Origin{Authority: "coach.example.com", DocumentURL: "https://coach.example.com/authority.json", AddonFor: basePkg}),
	}}
	ds.statuses[basePkg] = substrate.BundleStatus{ID: basePkg, Installed: true, Enabled: true, Version: 2}
	omitted, _, err := Plan(context.Background(), ds, published, details, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if omitted[0].Blocked == nil || omitted[0].Blocked.Reason != substrate.BlockNoModel {
		t.Fatalf("expected unpriced add-on: %+v", omitted)
	}
	omitted, err = SelectAddons(omitted, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := NewRunner(published).Run(context.Background(), ds, substrate.ActorConsole, details, omitted, Options{}, RunOptions{}, func([]string) bool { return true })
	if out.Failure != nil || out.Gone || len(ds.bindings) != 0 || len(ds.installs) != 0 {
		t.Fatalf("excluded add-on run: %+v, bindings: %v, installs: %v", out, ds.bindings, ds.installs)
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

func TestInstalledProviderInputNeedsOnlyMissingBindings(t *testing.T) {
	const pkg = "coach.example.com/coach"
	for _, tc := range []struct {
		name, bound string
		want        string
	}{
		{"unbound sole option", "", substrate.SetupEnable},
		{"valid binding", "fits", substrate.SetupNone},
		{"unoffered binding", "gone", substrate.SetupEnable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ds := &providerDataset{fakeDataset: newFakeDataset()}
			ds.providers = []*substrate.Record{
				{Kind: vocabulary.CoreKind(vocabulary.DocAgent), ID: pkg + "/run", Properties: map[string]any{"providerInput": "model", "models": []any{"a"}}},
				{ID: "fits", Properties: map[string]any{"pricing": []any{map[string]any{"model": "a"}}}},
			}
			ds.records[recordKey(vocabulary.CoreKind(vocabulary.DocBundle), pkg)] = &substrate.Record{Properties: map[string]any{"bindings": map[string]any{"model": map[string]any{"ref": vocabulary.LLMKind("provider") + "/" + tc.bound}}}}
			p := &planner{ds: ds, statuses: map[string]*substrate.BundleStatus{pkg: {Inputs: []substrate.InputStatus{{Kind: vocabulary.LLMKind("provider")}}}}}
			row := substrate.AuthorizationSetup{Action: substrate.SetupNone, Package: &substrate.AuthorizationPackage{ID: pkg}}
			if err := p.providerInputs(context.Background(), &row); err != nil {
				t.Fatal(err)
			}
			if row.Action != tc.want || len(row.Package.ProviderInputs) != 1 || row.Package.ProviderInputs[0].Selected != "fits" {
				t.Fatalf("plan: %+v, inputs: %+v", row, row.Package.ProviderInputs)
			}
		})
	}
}

func TestExcludedAddonIgnoresProviderChoices(t *testing.T) {
	rows := []substrate.AuthorizationSetup{{
		Action: substrate.SetupInstall, Blocked: &substrate.AuthorizationBlock{Reason: substrate.BlockNoModel},
		Package: &substrate.AuthorizationPackage{ID: "example.com/coach", AddonFor: "example.com/provider", ProviderInputs: []substrate.AuthorizationProviderInput{{Package: "example.com/coach", Input: "model"}}},
	}}
	selected, err := SelectAddons(rows, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, choices := range [][]substrate.AuthorizationProviderChoice{nil, {{Package: "example.com/coach", Input: "model", Provider: "unoffered"}}} {
		if err := SelectProviders(selected, choices); err != nil {
			t.Fatalf("excluded choice: %v", err)
		}
	}
	if err := SelectProviders(rows, nil); err == nil {
		t.Fatal("included add-on without a provider was admitted")
	}
	if len(providerChoices(selected)) != 0 {
		t.Fatal("excluded choices forwarded to runner")
	}
}

// capturingAddons keeps the planner configureAddons hands the catalog, so a
// test runs a deferred add-on's planning directly.
type capturingAddons struct {
	*publishing
	plan catalog.AddonPlanner
}

func (c *capturingAddons) SetAddonPlanner(plan catalog.AddonPlanner) { c.plan = plan }

// A waiting add-on takes no provider choice at approval, since nothing
// could keep one until its provider lands, and its later install waits
// again unless its agents' provider input resolves on its own.
func TestADeferredAgentAddonInstallsOnlyWhereItsProviderResolves(t *testing.T) {
	ctx := context.Background()
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
	b, _ := cat.ByID(pkg)
	published := &publishing{Catalog: testCatalog(t), listed: map[string]*catalog.Bundle{
		pkg: b.FromDocument(catalog.Origin{Authority: "coach.example.com", DocumentURL: "https://coach.example.com/authority.json", AddonFor: basePkg}),
	}}
	priced := func(id string) *substrate.Record {
		return &substrate.Record{ID: id, Properties: map[string]any{"pricing": []any{map[string]any{"model": "a"}}}}
	}
	ds := &providerDataset{fakeDataset: newFakeDataset(), providers: []*substrate.Record{priced("fits"), priced("also")}}
	rows, _, err := Plan(ctx, ds, published, []substrate.AuthorizationDetail{pkgDetail(pkg, 1)}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Action != substrate.SetupWait || rows[0].Blocked != nil || len(rows[0].Package.ProviderInputs) != 0 {
		t.Fatalf("a waiting add-on asked for a provider: %+v, inputs %+v", rows[0], rows[0].Package.ProviderInputs)
	}

	ds.statuses[basePkg] = substrate.BundleStatus{ID: basePkg, Installed: true, Enabled: true, Version: 2}
	capture := &capturingAddons{publishing: published}
	configureAddons(capture)
	addon := substrate.AppAddon{Package: pkg, Version: 1, AddonFor: basePkg}
	if _, blocked := capture.plan(ctx, ds, substrate.TokenInfo{}, addon); blocked == nil || blocked.Reason != substrate.BlockNoModel {
		t.Fatalf("two offered providers and no binding installed the add-on: %+v", blocked)
	}
	ds.providers = ds.providers[:1]
	if expect, blocked := capture.plan(ctx, ds, substrate.TokenInfo{}, addon); blocked != nil || len(expect) != 1 || expect[pkg] == nil || expect[pkg].Version != 1 {
		t.Fatalf("the repository's only provider did not resolve the input: %+v", blocked)
	}
}
