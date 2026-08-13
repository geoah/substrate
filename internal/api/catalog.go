package api

import (
	"context"
	"net/http"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
)

// catalogItem is one catalog entry on the wire: the shipped bundle plus
// whether THIS repository already installed it, and (for an installed one the
// shipped closure has moved past) what re-installing would do. The bundle's
// closure documents are unexported, so only the preview metadata marshals.
type catalogItem struct {
	*catalog.Bundle
	Installed bool `json:"installed"`
	// Upgrade is present only when the shipped closure moves something here:
	// the version motion and the guard lines an install would refuse on. The
	// upgrade itself is the existing install verb, unchanged.
	Upgrade *substrate.BundleUpgrade `json:"upgrade,omitempty"`
}

// getCatalog lists the installable bundle closures shipped in the binary, each
// flagged with whether this repository has it installed and whether the
// shipped closure would upgrade it.
func (h *handler) getCatalog(w http.ResponseWriter, r *http.Request) {
	items := []catalogItem{}
	if h.catalog != nil {
		installed, err := h.installedBundles(r.Context())
		if err != nil {
			writeSubstrateError(w, err)
			return
		}
		for _, b := range h.catalog.Bundles() {
			item, err := h.catalogItemFor(r.Context(), b, installed[b.ID])
			if err != nil {
				writeSubstrateError(w, err)
				return
			}
			items = append(items, item)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"catalog": items})
}

// getCatalogItem is one shipped bundle's detail — the resources it installs
// (types/functions/agents/triggers) so the console can preview, plus the
// installed flag and the upgrade preview.
func (h *handler) getCatalogItem(w http.ResponseWriter, r *http.Request) {
	if h.catalog == nil {
		writeError(w, http.StatusNotFound, codeNotFound, "no catalog is shipped")
		return
	}
	b, ok := h.catalog.ByID(pathParam(r, "id"))
	if !ok {
		writeError(w, http.StatusNotFound, codeNotFound, "unknown bundle "+pathParam(r, "id"))
		return
	}
	installed, err := h.installedBundles(r.Context())
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	item, err := h.catalogItemFor(r.Context(), b, installed[b.ID])
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// catalogItemFor assembles one wire entry, asking the catalog for the upgrade
// preview only where it can mean anything: an installed bundle. The preview is
// attached only when it moves something, so an up-to-date bundle marshals
// exactly as before.
func (h *handler) catalogItemFor(ctx context.Context, b *catalog.Bundle, installed bool) (catalogItem, error) {
	item := catalogItem{Bundle: b, Installed: installed}
	if !installed {
		return item, nil
	}
	up, err := h.catalog.Upgrade(ctx, b.ID, DatasetFrom(ctx))
	if err != nil {
		return item, err
	}
	if up != nil && up.Available {
		item.Upgrade = up
	}
	return item, nil
}

// postCatalogInstall applies a shipped bundle's closure into the caller's
// repository through the same schema/apply admission path an explicit apply uses —
// owner-only, atomic, refuse-breakage, idempotent (re-install is the bundle's
// own upgrade semantics). The response is the installed bundle's computed
// status.
func (h *handler) postCatalogInstall(w http.ResponseWriter, r *http.Request) {
	if h.catalog == nil {
		writeError(w, http.StatusNotFound, codeNotFound, "no catalog is shipped")
		return
	}
	ctx := r.Context()
	b, err := h.catalog.Install(ctx, ActorFrom(ctx), pathParam(r, "id"), DatasetFrom(ctx))
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	// The response is the installed bundle's computed status — the one schema
	// this endpoint promises. A dataset that could install the closure runs the
	// bundle lifecycle; if the required post-install status still cannot be
	// computed, that is a fault, surfaced as an error rather than a false
	// success in a different shape.
	ops, ok := bundlesFrom(ctx)
	if !ok {
		writeError(w, http.StatusInternalServerError, codeInternal,
			"installed bundle "+b.ID+" but this substrate computes no bundle status")
		return
	}
	st, err := ops.BundleStatus(ctx, b.ID)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// installedBundles is the set of bundle ids installed in this repository. A dataset
// that runs no bundle lifecycle has none — an empty set, no error. A status
// READ that fails is a fault (repository/database), returned as an error so the
// caller fails with the normal substrate error shape instead of silently
// reporting installed integrations as available.
func (h *handler) installedBundles(ctx context.Context) (map[string]bool, error) {
	out := map[string]bool{}
	ops, ok := bundlesFrom(ctx)
	if !ok {
		return out, nil
	}
	statuses, err := ops.BundleStatuses(ctx)
	if err != nil {
		return nil, err
	}
	for _, st := range statuses {
		if st.Installed {
			out[st.ID] = true
		}
	}
	return out, nil
}
