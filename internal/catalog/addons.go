package catalog

import (
	"context"
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
			} else if _, _, err := c.Install(ctx, substrate.ActorConsole, addon.Package, ds); err != nil {
				result.Problem = "The add-on could not be installed. Open its package page to resolve it."
			} else {
				result.State = "installed"
				installed[addon.Package] = addon.Version
			}
			out = append(out, result)
		}
	}
	return out
}
