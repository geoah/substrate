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
		home := homeAuthority(r.Context())
		for _, b := range h.catalog.Bundles() {
			items = append(items, h.catalogItemFor(r.Context(), b, b.HeldID(installed, home) != ""))
		}
	}
	writeJSON(w, http.StatusOK, substrate.Listed(items))
}

// homeAuthority is the authority this repository owns, which is where a SAMPLE lands
// (decision records 0046 and 0048). A dataset that names none answers "", and
// a sample then reads as its shipped id, which is never installed.
func homeAuthority(ctx context.Context) string {
	ds := DatasetFrom(ctx)
	if ds == nil {
		return ""
	}
	return ds.Repository().Authority
}

// getCatalogItem is one shipped bundle's detail — the closure it installs
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
	held := b.HeldID(installed, homeAuthority(r.Context())) != ""
	writeJSON(w, http.StatusOK, h.catalogItemFor(r.Context(), b, held))
}

// catalogItemFor assembles one wire entry, asking the catalog for the upgrade
// preview only where it can mean anything: an installed bundle. The preview is
// attached only when it moves something, so an up-to-date bundle marshals
// exactly as before.
//
// A preview that FAILS costs that entry its upgrade offer and nothing else.
// The listing is what the console's Registry (and now its sidebar badge, on
// every page) reads, so one unpreviewable closure must not blank it — the
// same reason catalog.Load drops a broken directory instead of bricking the
// shipped set. The offer is an extra; the listing is the promise.
func (h *handler) catalogItemFor(ctx context.Context, b *catalog.Bundle, installed bool) catalogItem {
	item := catalogItem{Bundle: b, Installed: installed}
	if !installed {
		return item
	}
	up, err := h.catalog.Upgrade(ctx, b.ID, DatasetFrom(ctx))
	if err != nil || up == nil || !up.Available {
		return item
	}
	item.Upgrade = up
	return item
}

// postCatalogInstall applies a shipped bundle's closure into the caller's
// repository through the same schema/apply admission path an explicit apply uses —
// owner-only, atomic, refuse-breakage, idempotent (re-install is the bundle's
// own upgrade semantics). The response is the installed bundle's computed
// status.
func (h *handler) postCatalogInstall(w http.ResponseWriter, r *http.Request) {
	h.takeCatalogBundle(w, r, (*catalog.Catalog).Install, false)
}

// postCatalogImport is the SAMPLE door: the same admission over a closure
// rehomed onto this repository's own authority first, so what lands is the
// repository's own vocabulary. A PROVIDER id is refused here, naming install.
func (h *handler) postCatalogImport(w http.ResponseWriter, r *http.Request) {
	h.takeCatalogBundle(w, r, (*catalog.Catalog).Import, true)
}

// takeCatalogBundle runs one of the two doors and answers with the status of
// the bundle as it LANDED. `rehomed` says which id that is: the import rewrote
// the closure onto this repository's authority, so its bundle record is
// `<authority>/<package>`, while an install landed the id the request named.
// Asking for the wrong one is a 404 on a write that succeeded.
func (h *handler) takeCatalogBundle(w http.ResponseWriter, r *http.Request,
	take func(*catalog.Catalog, context.Context, substrate.Actor, string, substrate.Dataset) (*catalog.Bundle, error),
	rehomed bool,
) {
	if h.catalog == nil {
		writeError(w, http.StatusNotFound, codeNotFound, "no catalog is shipped")
		return
	}
	ctx := r.Context()
	b, err := take(h.catalog, ctx, ActorFrom(ctx), pathParam(r, "id"), DatasetFrom(ctx))
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	landed := b.ID
	if rehomed {
		landed = b.LandedID(homeAuthority(ctx))
	}
	// The response is the landed bundle's computed status, the one schema
	// this endpoint promises. A dataset that could install the closure runs the
	// bundle lifecycle; if the required post-install status still cannot be
	// computed, that is a fault, surfaced as an error rather than a false
	// success in a different shape.
	ops, ok := bundlesFrom(ctx)
	if !ok {
		writeError(w, http.StatusInternalServerError, codeInternal,
			"installed bundle "+landed+" but this substrate computes no bundle status")
		return
	}
	st, err := ops.BundleStatus(ctx, landed)
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
