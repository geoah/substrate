package api

import (
	"net/http"

	"github.com/geoah/substrate/internal/substrate"
)

func (h *handler) getIncoming(w http.ResponseWriter, r *http.Request) {
	ds, ti, addr, ok := h.collection(w, r, true)
	if !ok {
		return
	}
	id := addr.id
	// Incoming is a fixed-order reverse read: it honors first/after, plus the
	// two narrowings a drill-down needs — `rel` (one relationship) and
	// `fromKind` (one source kind), which are what let a client expand ONE
	// group without pulling every inbound pointer the record has.
	//
	// filter/orderBy/withEdges/withAnnotations still do not apply, so their
	// presence is a bad_request naming the param rather than a silent drop.
	if bad := rejectParams(r, "filter", "orderBy", "withEdges", "withAnnotations"); bad != "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, bad+" is not supported on incoming")
		return
	}
	// Anything else outside the grammar is named too: a silently ignored
	// narrowing returns the UNFILTERED fan-in looking filtered.
	if bad := unsupportedParam(r, incomingParams...); bad != "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, bad+" is not supported on incoming")
		return
	}
	q, err := parseQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	// The path names the full identity: the read is type-scoped.
	page, err := ds.Incoming(r.Context(), ti.Identity, id, substrate.IncomingOptions{
		First:    q.First,
		After:    q.After,
		Rel:      r.URL.Query().Get("rel"),
		FromKind: r.URL.Query().Get("fromKind"),
	})
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}
