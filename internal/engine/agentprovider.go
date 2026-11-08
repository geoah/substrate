package engine

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// resolveAgentProvider opens the row a run of ag completes against and names
// the model it sends. threadID is the thread the run continues, "" for a new
// one: an agent on runsOn keeps a continued thread on its own model.
func (ds *dataset) resolveAgentProvider(ctx context.Context, ag *vocabulary.Agent, threadID string) (*providerConfig, string, error) {
	if len(ag.RunsOn) > 0 {
		choice, err := ds.chooseRunsOn(ctx, ag, threadID)
		if err != nil {
			return nil, "", err
		}
		provider, err := ds.resolveProvider(ctx, choice.provider)
		if err != nil {
			return nil, "", err
		}
		// Chosen against the operator's list already; held again against
		// the transport just opened, which a key written since may have moved.
		if err := ds.checkOperatorModel(provider, "agent "+ag.Identity(), choice.model); err != nil {
			return nil, "", err
		}
		return provider, choice.model, nil
	}
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
	if err := ds.checkOperatorModel(provider, "agent "+ag.Identity(), model); err != nil {
		return nil, "", err
	}
	return provider, model, nil
}
