package appsetup

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/geoah/substrate/internal/catalog"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func (p *planner) providerInputs(ctx context.Context, row *substrate.AuthorizationSetup) error {
	// A waiting add-on takes no provider choice now: nothing could keep it
	// until its provider lands, and its install then needs the input to
	// resolve on its own (inputsResolve).
	if row.Package == nil || row.Blocked != nil || row.Action == substrate.SetupWait || p.opts.ActionOnly[row.Package.ID] {
		return nil
	}
	ids := []string{row.Package.ID}
	for _, step := range row.Package.Requires {
		ids = append(ids, step.ID)
	}
	for _, id := range ids {
		if id == row.Package.ID && row.Action == substrate.SetupNone {
			if !p.hasProviderInput(id) {
				continue
			}
			inputs, err := heldProviderInputs(ctx, p.ds, id)
			if err != nil {
				return err
			}
			row.Package.ProviderInputs = append(row.Package.ProviderInputs, inputs...)
		} else if b := p.bundle(id); b != nil {
			row.Package.ProviderInputs = append(row.Package.ProviderInputs, b.AgentProviderInputs()...)
		}
	}
	if len(row.Package.ProviderInputs) == 0 {
		return nil
	}
	var providers []*substrate.Record
	q := substrate.Query{Filter: substrate.Filter{Kinds: []string{vocabulary.LLMKind("provider")}}, First: 500}
	for {
		page, err := p.ds.List(ctx, q)
		if err != nil {
			return err
		}
		providers = append(providers, page.Records...)
		if page.Cursor == "" {
			break
		}
		q.After = page.Cursor
	}
	// What each row can run priced, as dispatch reads it: the operator's
	// list for a row it serves, else the row's own entries and its
	// llm/model records (decision 0242).
	offers, err := p.ds.ModelOffers(ctx)
	if err != nil {
		return err
	}
	needsBinding := false
	for i := range row.Package.ProviderInputs {
		input := &row.Package.ProviderInputs[i]
		input.Options = providerOptions(input.Agents, providers, offers)
		if len(input.Options) == 0 {
			row.Blocked = block(substrate.BlockNoModel, fmt.Sprintf("No AI provider prices the models for %s input %s.", input.Package, input.Input), "/data/substrate.reamde.dev/llm/provider")
		}
		if len(input.Options) == 1 {
			input.Selected = input.Options[0].ID
		}
		bundle, err := p.ds.Get(ctx, vocabulary.CoreKind(vocabulary.DocBundle), input.Package)
		if err != nil && !errors.Is(err, substrate.ErrNotFound) {
			return err
		}
		validBinding := false
		if err == nil && bundle.DeletedAt == nil {
			bindings, _ := bundle.Properties["bindings"].(map[string]any)
			bound := vocabulary.ReferentID(bindings[input.Input], vocabulary.LLMKind("provider"))
			if slices.ContainsFunc(input.Options, func(o substrate.AuthorizationProviderOption) bool { return o.ID == bound }) {
				input.Selected = bound
				validBinding = true
			}
		}
		needsBinding = needsBinding || !validBinding
	}
	// A preselected option is not the owner's binding.
	if row.Action == substrate.SetupNone && needsBinding {
		row.Action = substrate.SetupEnable
	}
	return nil
}

func providerOptions(agents []substrate.AuthorizationProviderAgent, providers []*substrate.Record, offers map[string][]string) []substrate.AuthorizationProviderOption {
	out := []substrate.AuthorizationProviderOption{}
	for _, row := range providers {
		priced := offers[row.ID]
		option := substrate.AuthorizationProviderOption{ID: row.ID, Label: row.Title, Models: map[string]string{}}
		if option.Label == "" {
			option.Label = row.ID
		}
		for _, ag := range agents {
			model := vocabulary.PreferredAgentModel(ag.Models, priced)
			if model == "" {
				break
			}
			option.Models[ag.Agent] = model
		}
		if len(option.Models) == len(agents) {
			out = append(out, option)
		}
	}
	return out
}

// SelectProviders validates choices against the freshly read plan. No choice
// can name another package input or an unoffered provider.
func SelectProviders(rows []substrate.AuthorizationSetup, choices []substrate.AuthorizationProviderChoice) error {
	for _, choice := range choices {
		found := false
		for _, row := range rows {
			if row.Package == nil {
				continue
			}
			inputs := row.Package.ProviderInputs
			for j := range inputs {
				input := &inputs[j]
				if input.Package != choice.Package || input.Input != choice.Input {
					continue
				}
				found = true
				if row.Action == substrate.SetupNone {
					continue
				}
				if !slices.ContainsFunc(input.Options, func(o substrate.AuthorizationProviderOption) bool { return o.ID == choice.Provider }) {
					return fmt.Errorf("%w: provider %q is not offered for %s input %s", substrate.ErrValidation, choice.Provider, choice.Package, choice.Input)
				}
				input.Selected, found = choice.Provider, true
			}
		}
		if !found {
			return fmt.Errorf("%w: no provider input %s/%s in this approval", substrate.ErrValidation, choice.Package, choice.Input)
		}
	}
	for _, row := range rows {
		if row.Package == nil || row.Action == substrate.SetupNone {
			continue
		}
		for _, input := range row.Package.ProviderInputs {
			if input.Selected == "" {
				return fmt.Errorf("%w: choose an AI provider for %s input %s", substrate.ErrValidation, input.Package, input.Input)
			}
		}
	}
	return nil
}

func providerChoices(rows []substrate.AuthorizationSetup) []substrate.AuthorizationProviderChoice {
	var out []substrate.AuthorizationProviderChoice
	for _, row := range rows {
		if row.Package == nil || row.Action == substrate.SetupNone {
			continue
		}
		for _, input := range row.Package.ProviderInputs {
			out = append(out, substrate.AuthorizationProviderChoice{Package: input.Package, Input: input.Input, Provider: input.Selected})
		}
	}
	return out
}

func bindProviders(ctx context.Context, ds substrate.Dataset, row substrate.AuthorizationSetup) error {
	if row.Package == nil {
		return nil
	}
	for _, input := range row.Package.ProviderInputs {
		if err := ds.BindBundleInput(ctx, input.Package, input.Input, input.Selected); err != nil {
			return err
		}
	}
	return nil
}

func heldProviderInputs(ctx context.Context, ds substrate.Dataset, pkg string) ([]substrate.AuthorizationProviderInput, error) {
	var docs []map[string]any
	q := substrate.Query{Filter: substrate.Filter{Kinds: []string{vocabulary.CoreKind(vocabulary.DocAgent)}}, First: 500}
	for {
		page, err := ds.List(ctx, q)
		if err != nil {
			return nil, err
		}
		for _, row := range page.Records {
			if strings.HasPrefix(row.ID, pkg+"/") {
				docs = append(docs, vocabulary.AgentManifest(pkg, vocabulary.KindName(row.ID), row.Properties))
			}
		}
		if page.Cursor == "" {
			break
		}
		q.After = page.Cursor
	}
	return catalog.AgentProviderInputs(pkg, docs), nil
}

func (p *planner) hasProviderInput(pkg string) bool {
	st := p.statuses[pkg]
	return st != nil && slices.ContainsFunc(st.Inputs, func(input substrate.InputStatus) bool { return input.Kind == vocabulary.LLMKind("provider") })
}

// inputsResolve reports whether every provider input of a planned row
// resolves without a choice made now: a valid binding, the id `default`
// among the offered rows, or the repository's only provider row, which is
// how an input resolves at dispatch.
func inputsResolve(ctx context.Context, ds substrate.Dataset, row substrate.AuthorizationSetup) (bool, error) {
	if row.Package == nil {
		return true, nil
	}
	for _, input := range row.Package.ProviderInputs {
		offered := func(id string) bool {
			return slices.ContainsFunc(input.Options, func(o substrate.AuthorizationProviderOption) bool { return o.ID == id })
		}
		bundle, err := ds.Get(ctx, vocabulary.CoreKind(vocabulary.DocBundle), input.Package)
		if err != nil && !errors.Is(err, substrate.ErrNotFound) {
			return false, err
		}
		if err == nil && bundle.DeletedAt == nil {
			bindings, _ := bundle.Properties["bindings"].(map[string]any)
			if bound := vocabulary.ReferentID(bindings[input.Input], vocabulary.LLMKind("provider")); bound != "" && offered(bound) {
				continue
			}
		}
		if offered("default") {
			continue
		}
		page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{vocabulary.LLMKind("provider")}}, First: 2})
		if err != nil {
			return false, err
		}
		if len(page.Records) == 1 && offered(page.Records[0].ID) {
			continue
		}
		return false, nil
	}
	return true, nil
}
