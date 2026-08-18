package api

import (
	"context"
	"net/http"

	"github.com/geoah/substrate/internal/substrate"
)

// edgeBody is the REST link/unlink request body: an EdgeRef naming the target
// ({authority, type, id}, or bare {id} where the edge declaration pins a single
// target type), plus optional edge properties on a link. Unlink ignores the
// properties. The EdgeRef fields flatten into the top level, so the body reads
// `{"id":"…","type":"…","authority":"…","properties":{…}}`.
type edgeBody struct {
	substrate.EdgeRef
	Properties map[string]any `json:"properties,omitempty"`
}

// linkEdge adds one outgoing edge to the addressed source record: the verb
// lives at the record, POST …/{authority}/{kind}/{id}/edges/{rel}.
// It returns the refreshed source record, like put/patch.
func (h *handler) linkEdge(w http.ResponseWriter, r *http.Request) {
	ds, ti, addr, ok := h.collection(w, r, true)
	if !ok {
		return
	}
	var body edgeBody
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	if body.ID == "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, "an edge target needs an id")
		return
	}
	ctx := r.Context()
	id := addr.id
	rel := pathParam(r, "rel")
	if err := ds.Link(ctx, ActorFrom(ctx), ti.Identity, id, rel, body.EdgeRef, body.Properties); err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeRecord(w, ctx, ds, ti.Identity, id)
}

// unlinkEdge removes one outgoing edge from the addressed source record:
// DELETE …/{authority}/{kind}/{id}/edges/{rel} with the same EdgeRef body. It is
// the mutation a REST client could not perform before this stage — a put could
// add an edge but never drop one.
func (h *handler) unlinkEdge(w http.ResponseWriter, r *http.Request) {
	ds, ti, addr, ok := h.collection(w, r, true)
	if !ok {
		return
	}
	var body edgeBody
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	if body.ID == "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, "an edge target needs an id")
		return
	}
	ctx := r.Context()
	id := addr.id
	rel := pathParam(r, "rel")
	if err := ds.Unlink(ctx, ActorFrom(ctx), ti.Identity, id, rel, body.EdgeRef); err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeRecord(w, ctx, ds, ti.Identity, id)
}

// writeRecord re-reads the addressed record and writes it as the 200 body —
// the response link/unlink share with put/patch, so a client sees the source's
// edges after the mutation.
func writeRecord(w http.ResponseWriter, ctx context.Context, ds substrate.Dataset, typ, id string) {
	ent, err := ds.Get(ctx, typ, id)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ent)
}
