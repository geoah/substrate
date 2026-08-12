package api

import (
	"net/http"
)

func (h *handler) getIncoming(w http.ResponseWriter, r *http.Request) {
	ds, ti, ok := h.collection(w, r)
	if !ok {
		return
	}
	id := resourceID(r)
	// Incoming is a fixed-order reverse-edge read: it honors only first/after.
	// filter/orderBy/withEdges/withAnnotations do not apply, so their presence
	// is a bad_request naming the param rather than a silent drop.
	if bad := rejectParams(r, "filter", "orderBy", "withEdges", "withAnnotations"); bad != "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, bad+" is not supported on incoming")
		return
	}
	q, err := parseQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	// The path names the full identity: the read is type-scoped.
	page, err := ds.Incoming(r.Context(), ti.Identity, id, q.First, q.After)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}
