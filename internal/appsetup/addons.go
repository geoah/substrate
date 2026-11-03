package appsetup

import (
	"context"
	"fmt"
	"slices"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
)

// SelectAddons leaves unapproved optional rows inert without changing row
// indices, which the parked request uses for progress and failures.
func SelectAddons(rows []substrate.AuthorizationSetup, selected []string) ([]substrate.AuthorizationSetup, error) {
	for _, id := range selected {
		if !slices.ContainsFunc(rows, func(r substrate.AuthorizationSetup) bool {
			return r.Package != nil && r.Package.ID == id && r.Package.AddonFor != ""
		}) {
			return nil, fmt.Errorf("%w: an approved add-on must be on the plan", substrate.ErrValidation)
		}
	}
	out := slices.Clone(rows)
	for i, row := range out {
		if row.Package == nil || row.Package.AddonFor == "" || slices.Contains(selected, row.Package.ID) {
			continue
		}
		pkg := *row.Package
		pkg.Settings = nil
		out[i].Package, out[i].Action, out[i].Blocked = &pkg, substrate.SetupNone, nil
	}
	return out, nil
}

// WaitingAddons contains only selected add-ons whose provider is absent.
func WaitingAddons(rows []substrate.AuthorizationSetup) []substrate.AppAddon {
	var out []substrate.AppAddon
	for _, row := range rows {
		if row.Package != nil && row.Action == substrate.SetupWait {
			out = append(out, substrate.AppAddon{Package: row.Package.ID, Version: row.Package.Required, AddonFor: row.Package.AddonFor})
		}
	}
	return out
}

// addonCatalog is a Catalog that installs waiting add-ons when their
// provider lands, and so needs the approval policy that plans them.
type addonCatalog interface {
	Catalog
	SetAddonPlanner(catalog.AddonPlanner)
}

// configureAddons has the catalog plan a waiting add-on (decision 0216)
// when its provider lands, against the approval of the token that keeps it
// waiting: the scopes that token holds bound the add-on's code exactly as
// the page's checked scopes bound it at approval (decision 0215), and a
// token without scopes is a full approval.
func configureAddons(cat addonCatalog) {
	cat.SetAddonPlanner(func(ctx context.Context, ds substrate.Dataset, token substrate.TokenInfo, addon substrate.AppAddon) *substrate.AuthorizationBlock {
		approved := scope.FullApproval()
		if token.Scopes != nil {
			var err error
			if approved, err = scope.ApprovalOf(token.Scopes); err != nil {
				return block(substrate.BlockUnsupported, "The approval that keeps this add-on waiting cannot be read; approve the app again.", "")
			}
		}
		rows, _, err := Plan(ctx, ds, cat, []substrate.AuthorizationDetail{{Type: substrate.DetailPackage, Package: addon.Package, Version: addon.Version}}, Options{Approved: approved})
		if err != nil {
			return block(substrate.BlockUnsupported, "The waiting add-on could not be planned.", "")
		}
		row := rows[0]
		if row.Package.AddonFor != addon.AddonFor {
			return block(substrate.BlockUnsupported, "The add-on now names a different provider; approve it again.", ProviderPage(addon.Package))
		}
		if row.Blocked != nil {
			return row.Blocked
		}
		if row.Action == substrate.SetupWait {
			return block(substrate.BlockUnsupported, "The add-on's provider is still missing.", "")
		}
		if err := ValidateSettings(rows, nil); err != nil {
			return block(substrate.BlockNeedsSetting, "Fill the add-on's required settings on its package page.", ProviderPage(addon.Package))
		}
		return nil
	})
}
