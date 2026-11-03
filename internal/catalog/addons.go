package catalog

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// AddonPlanner checks a waiting package under the live token's approval.
// Catalog owns the install walk; appsetup owns the approval policy.
type AddonPlanner func(context.Context, substrate.Dataset, substrate.TokenInfo, substrate.AppAddon) *substrate.AuthorizationBlock

// SetAddonPlanner wires the approval planner before this catalog is served.
func (c *Catalog) SetAddonPlanner(plan AddonPlanner) { c.addonPlanner = plan }

type addonWalkKey struct{}

func (c *Catalog) installAddons(ctx context.Context, ds substrate.Dataset, providers []*Bundle) []substrate.AddonInstallResult {
	if c.addonPlanner == nil || ctx.Value(addonWalkKey{}) != nil {
		return nil
	}
	ctx = context.WithValue(ctx, addonWalkKey{}, true)
	tokens, err := ds.Tokens(ctx)
	if err != nil {
		return []substrate.AddonInstallResult{{State: substrate.SetupWait, Problem: "Waiting add-ons could not be read. Install the provider again to retry."}}
	}
	statuses, err := ds.BundleStatusesUncounted(ctx)
	if err != nil {
		return []substrate.AddonInstallResult{{State: substrate.SetupWait, Problem: "Waiting add-ons could not be checked. Install the provider again to retry."}}
	}
	installed := map[string]int64{}
	for _, st := range statuses {
		if st.Installed {
			installed[st.ID] = st.ShippedVersion
			if installed[st.ID] == 0 {
				installed[st.ID] = st.Version
			}
		}
	}
	held := map[string]bool{}
	for _, b := range providers {
		held[b.ID] = true
	}
	var out []substrate.AddonInstallResult
	for _, token := range tokens {
		if token.ExpiresAt != nil && !token.ExpiresAt.After(time.Now()) {
			continue
		}
		for _, addon := range token.AppAddons {
			if !held[addon.AddonFor] || installed[addon.Package] >= addon.Version {
				continue
			}
			result := substrate.AddonInstallResult{Package: addon.Package, TokenID: token.ID, State: substrate.SetupWait}
			if blocked := c.addonPlanner(ctx, ds, token, addon); blocked != nil {
				result.Problem = blocked.Problem
			} else if _, _, err := c.Install(ctx, substrate.ActorConsole, addon.Package, &addonConsentDataset{Dataset: ds, tokenID: token.ID}); err != nil {
				result.Problem = "The add-on could not be installed. Open its package page to resolve it."
				if errors.Is(err, substrate.ErrForbidden) {
					result.Problem = "The add-on's approval is no longer live. Approve the app again."
				}
			} else {
				result.State = "installed"
				installed[addon.Package] = addon.Version
			}
			out = append(out, result)
		}
	}
	return out
}

// Each closure, including requirements, rechecks consent after catalog fetches.
// The engine repeats this check inside the write transaction.
type addonConsentDataset struct {
	substrate.Dataset
	tokenID string
}

func (d *addonConsentDataset) InstallBundleClosure(ctx context.Context, actor substrate.Actor, docs []map[string]any, data []substrate.PutInput, opts substrate.BundleInstall) ([]*substrate.Record, error) {
	tokens, err := d.Tokens(ctx)
	if err != nil {
		return nil, err
	}
	for _, token := range tokens {
		if token.ID == d.tokenID && (token.ExpiresAt == nil || token.ExpiresAt.After(time.Now())) {
			opts.ApprovalTokenID = token.ID
			return d.Dataset.InstallBundleClosure(ctx, actor, docs, data, opts)
		}
	}
	return nil, fmt.Errorf("%w: the add-on's approval is no longer live", substrate.ErrForbidden)
}
