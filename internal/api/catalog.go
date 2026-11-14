package api

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
)

// getCatalog lists the installable bundle closures shipped in the binary, each
// flagged with whether this repository has it installed and whether the
// shipped closure would upgrade it.
func (h *handler) getCatalog(w http.ResponseWriter, r *http.Request) {
	items := []substrate.CatalogItem{}
	if h.catalog != nil {
		installed, err := h.installedBundles(r.Context())
		if err != nil {
			writeSubstrateError(w, r, err)
			return
		}
		for _, b := range h.catalog.Bundles() {
			items = append(items, h.catalogItemFor(r, b, installed.copyOf(b)))
		}
		var accounts map[string]bool
		for _, id := range installed.ids {
			if _, binary := h.catalog.ByID(id); binary {
				continue
			}
			ds := DatasetFrom(r.Context())
			row, err := ds.Get(r.Context(), "substrate.reamde.dev/core/package", id)
			if err != nil || row.Properties["publishedFrom"] == nil {
				continue
			}
			b, resolveErr := h.catalog.ResolveFor(r.Context(), id, 0, ds)
			if resolveErr != nil {
				// The closure is out of reach, so what the package is for is
				// read off what the repository stores: a provider stays one
				// while its publisher cannot be read (decision record 0238).
				if accounts == nil {
					if accounts, err = accountPackages(r.Context(), ds); err != nil {
						writeSubstrateError(w, r, err)
						return
					}
				}
				st := installed.byID[id]
				from, _ := row.Properties["publishedFrom"].(string)
				items = append(items, substrate.CatalogItem{CatalogBundle: substrate.CatalogBundle{ID: id, Name: st.Name, Authority: st.Authority, Package: st.Package, KeptBy: substrate.KeptByPublisher, Provider: accounts[id], Publisher: &substrate.AuthorizationPackagePublisher{Authority: st.Authority, DocumentURL: from, FromFile: strings.HasPrefix(from, "file:")}}, Installed: true, Upgrade: &substrate.BundleUpgrade{Blockers: []string{resolveErr.Error()}}})
				continue
			}
			items = append(items, h.catalogItemFor(r, b, installed.byID[id]))
		}
	}
	writeJSON(w, http.StatusOK, substrate.Listed(items))
}

// getCatalogItem is one shipped bundle's detail — the closure it installs
// (types/functions/agents/triggers) so the console can preview, plus the
// installed flag and the upgrade preview.
func (h *handler) getCatalogItem(w http.ResponseWriter, r *http.Request) {
	if h.catalog == nil {
		writeError(w, http.StatusNotFound, codeNotFound, "no catalog is shipped")
		return
	}
	b, err := h.catalog.ResolveFor(r.Context(), pathParam(r, "id"), 0, DatasetFrom(r.Context()))
	if err != nil {
		writeError(w, http.StatusNotFound, codeNotFound, err.Error())
		return
	}
	installed, err := h.installedBundles(r.Context())
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, h.catalogItemFor(r, b, installed.copyOf(b)))
}

// installedSet is every bundle installed in this repository, read once per
// request: the statuses by id, the ids in one order, and the id set the
// catalog's HeldID lookup takes.
type installedSet struct {
	byID map[string]*substrate.BundleStatus
	ids  []string
	held map[string]bool
}

// copyOf is the installed bundle that IS catalog entry b in this repository,
// or nil. A copy that carries an origin is matched on it: two authorities may
// each publish a sample of one package word, and both land in the
// repository's local namespace as `~/<package>`, so the landed id alone would
// read both entries as installed after either import and hand both the one
// copy's provenance. Only a copy stamped before the origin existed, and a
// provider (which is never stamped), fall back to the id lookup HeldID does.
func (s installedSet) copyOf(b *catalog.Bundle) *substrate.BundleStatus {
	for _, id := range s.ids {
		if st := s.byID[id]; st.Origin == b.ID {
			return st
		}
	}
	st := s.byID[b.HeldID(s.held)]
	if st == nil || st.Origin != "" {
		return nil
	}
	return st
}

// catalogItemFor assembles one wire entry, asking the catalog for the upgrade
// preview only where it can mean anything: an installed bundle. `held` is the
// status of the bundle this repository holds for the entry, nil when it holds
// none; the preview is attached only when it moves something, so an up-to-date
// bundle marshals exactly as before.
//
// A preview that FAILS costs that entry its upgrade offer and nothing else.
// The listing is what the console's Registry (and now its sidebar badge, on
// every page) reads, so one unpreviewable closure must not blank it — the
// same reason catalog.Load drops a broken directory instead of bricking the
// shipped set. The offer is an extra; the listing is the promise. The failure
// itself is not dropped: it rides the entry as one fixed blocker line, so a
// database fault reads as an upgrade nobody can take, not as an entry with
// nothing to offer. The error goes to the server log alone: a driver error
// names a host or a table, and a 200 body for a repository token is not where
// the deployment is described (never leak the deployment).
func (h *handler) catalogItemFor(r *http.Request, b *catalog.Bundle, held *substrate.BundleStatus) substrate.CatalogItem {
	ctx := r.Context()
	installed := held != nil
	item := substrate.CatalogItem{CatalogBundle: b.CatalogBundle, Installed: installed}
	// The held copy's provenance, when it has one: which shipped id it was
	// imported from, at which version, and whether it has been edited since.
	// The status computed it; the entry only carries it to the console.
	if held != nil {
		item.Origin, item.OriginVersion, item.Modified = held.Origin, held.OriginVersion, held.Modified
	}
	// Each suggested mapping's state in THIS repository (decision record
	// 0049): whether the declaration is here, whether the provider it reads
	// is, and whether it fits the version installed. The shipped closure
	// carries no state of its own, so this list is only ever this read's; a
	// read that fails leaves it unstated rather than blanking the entry, for
	// the same reason the upgrade preview below does.
	if ds := DatasetFrom(ctx); ds != nil {
		if states, err := b.SuggestedMappingStates(ctx, ds); err == nil {
			item.SuggestedMappings = states
		}
	}
	if !installed {
		return item
	}
	up, err := h.catalog.Upgrade(ctx, b.ID, DatasetFrom(ctx), held)
	if err != nil {
		if clientCanceled(ctx, err) {
			slog.Debug("catalog: upgrade preview canceled by the client", append(requestLogAttrs(r), "bundle", b.ID, "error", err)...)
		} else {
			slog.Error("catalog: upgrade preview failed", append(requestLogAttrs(r), "bundle", b.ID, "error", err)...)
		}
		item.Upgrade = &substrate.BundleUpgrade{Blockers: []string{failedPreviewBlocker}}
		return item
	}
	// A preview with nothing to move and nothing to say is no offer. One that
	// could not even build the shipped closure answers not-available WITH
	// blockers (engine PlanBundleUpgrade), and those are kept: they are the
	// reason the upgrade cannot be taken. One over an EDITED copy is kept
	// even when nothing moved (decision record 0070): a re-import of that
	// copy needs a confirmation, and this preview is where its hash is read.
	if up == nil || (!up.Available && len(up.Blockers) == 0 && !up.DiscardsEdits) {
		return item
	}
	item.Upgrade = up
	return item
}

// failedPreviewBlocker is the one guard line a preview that could not run
// leaves on its entry. Fixed text: the error itself is logged, never served.
// The console keys on this exact line to say the preview failed rather than
// that live rows block it (web/console/src/lib/bundles.ts).
const failedPreviewBlocker = "the upgrade preview failed; see the server log"

// postCatalogAdd takes one catalog package into the caller's repository
// through the same vocabulary apply admission path an explicit apply uses:
// owner-only, atomic, refuse-breakage, idempotent (adding again is the
// package's own upgrade). Who keeps the package decides where it lands
// (decision record 0258): one its publisher keeps lands verbatim under the
// authority that publishes it; one the repository keeps, a sample, is first
// rehomed onto this repository's own authority, so what lands is the
// repository's own vocabulary. Every sample ships in the binary, so the
// binary's closure decides; anything else is its publisher's. The response
// is the status of the bundle as it LANDED: a sample's is
// `<authority>/<package>`, not the id the request named.
func (h *handler) postCatalogAdd(w http.ResponseWriter, r *http.Request) {
	if h.catalog == nil {
		writeError(w, http.StatusNotFound, codeNotFound, "no catalog is shipped")
		return
	}
	var req catalogAddRequest
	raw, ok := readBody(w, r, jsonBody)
	if !ok {
		return
	}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := decodeStrictBytes(raw, &req, false); err != nil {
			writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
			return
		}
	}
	ctx := r.Context()
	id := pathParam(r, "id")
	shipped, _ := h.catalog.ByID(id)
	copied := shipped != nil && shipped.KeptBy == substrate.KeptByRepository
	if copied && req.Expect != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest,
			"expect: "+id+" is a sample, which no publisher keeps, so there is no published version to expect")
		return
	}
	var b *catalog.Bundle
	var suggested []substrate.SuggestedMapping
	var err error
	if copied {
		b, suggested, err = h.catalog.ImportConfirmed(ctx, ActorFrom(ctx), id, DatasetFrom(ctx), req.Confirm)
	} else {
		b, suggested, err = h.catalog.InstallReviewed(ctx, ActorFrom(ctx), id, DatasetFrom(ctx), req.Confirm, req.Expect)
	}
	if err != nil {
		if errors.Is(err, catalog.ErrNotPublished) {
			writeError(w, http.StatusNotFound, codeNotFound, err.Error())
			return
		}

		writeSubstrateError(w, r, err)
		return
	}
	landed := b.ID
	if copied {
		landed = b.LandedID()
	}
	// The response is the landed bundle's computed status, the one schema
	// this endpoint promises.
	st, err := DatasetFrom(ctx).BundleStatus(ctx, landed)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	// What the closure's SUGGESTED MAPPINGS did (decision record 0049),
	// straight from the door: it decided which ones the batch carried and the
	// batch committed, so `landed` here is what was actually written. Reading
	// the states back instead would answer from a registry that may have moved
	// under the transaction.
	writeJSON(w, http.StatusOK, bundleTaken{BundleStatus: st, SuggestedMappings: suggested, Addons: b.AddonResults})
}

// catalogAddRequest is the add route's optional body: the caller's consent
// to a lossy conversion plan, bound to the preview it saw (decision 0067), and
// for a published package the version and digest the reader reviewed. No body
// is the same as no confirmation, so a lossless upgrade is the bare POST.
type catalogAddRequest struct {
	Expect  *catalog.PublishedCandidate  `json:"expect,omitempty"`
	Confirm *substrate.ConversionConfirm `json:"confirm,omitempty"`
}

// bundleTaken is what the add route answers: the landed bundle's computed
// status, plus the state of each suggested mapping the closure carries. A
// closure with none marshals exactly as the status alone did.
type bundleTaken struct {
	Addons []substrate.AddonInstallResult `json:"addons,omitempty"`
	substrate.BundleStatus
	SuggestedMappings []substrate.SuggestedMapping `json:"suggestedMappings,omitempty"`
}

// installedBundles is every bundle installed in this repository. A status
// READ that fails is a fault (repository/database), returned as an error so
// the caller fails with the normal substrate error shape instead of silently
// reporting installed providers as available. The statuses are the uncounted
// ones: an entry carries no live-row count, and counting them scans every
// live row of every installed kind on each read (issue #1086).
func (h *handler) installedBundles(ctx context.Context) (installedSet, error) {
	out := installedSet{byID: map[string]*substrate.BundleStatus{}, held: map[string]bool{}}
	statuses, err := DatasetFrom(ctx).BundleStatusesUncounted(ctx)
	if err != nil {
		return installedSet{}, err
	}
	for i := range statuses {
		if statuses[i].Installed {
			out.byID[statuses[i].ID] = &statuses[i]
			out.held[statuses[i].ID] = true
			out.ids = append(out.ids, statuses[i].ID)
		}
	}
	sort.Strings(out.ids)
	return out, nil
}
