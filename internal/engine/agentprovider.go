package engine

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func (ds *dataset) resolveAgentProvider(ctx context.Context, ag *vocabulary.Agent) (*providerConfig, string, error) {
	id := ag.Provider
	if ag.ProviderInput != "" {
		b, ok := ds.registry().BundleOf(ag.Package)
		if !ok {
			return nil, "", fmt.Errorf("%w: agent %s has no bundle", substrate.ErrValidation, ag.Identity())
		}
		input, err := ds.resolveBundleInput(ctx, b, ag.ProviderInput)
		if err != nil {
			return nil, "", err
		}
		if input.Row == nil {
			return nil, "", fmt.Errorf("%w: agent %s input %s: %s", substrate.ErrValidation, ag.Identity(), ag.ProviderInput, input.Detail)
		}
		id = input.Row.ID
	}
	provider, err := ds.resolveProvider(ctx, id)
	if err != nil {
		return nil, "", err
	}
	model := ag.Model
	if ag.ProviderInput != "" {
		model = vocabulary.PreferredAgentModel(ag.Models, slices.Sorted(maps.Keys(provider.pricing)))
		if model == "" {
			return nil, "", fmt.Errorf("%w: provider row %q prices none of agent %s's models %v", substrate.ErrValidation, id, ag.Identity(), ag.Models)
		}
	}
	return provider, model, nil
}
