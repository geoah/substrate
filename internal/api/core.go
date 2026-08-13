package api

import (
	"io"
	"net/http"

	"github.com/geoah/substrate/internal/strictjson"
)

// Nothing in core has endpoint-shaped collection behavior any more: the
// connector collection and its POST install shim went at the v1 freeze (ticket
// 004, ruling A12) and the repositories collection went with the control plane
// (B1), so every core collection is an ordinary resource and the sole install
// path is the schema-apply batch.

// The token endpoints moved OUT of the versioned resource tree and out of
// this file: `/register`, `/login`, `/tokens` sit beside `/api/…`
//  and live in auth_endpoints.go. With them went the whole
// least-privilege apparatus — scopes, the actor delegation check, the
// narrowing rules — because a token now has full access to its repository
// and nothing else. The repository-management endpoints went with
// the control plane in B1.

type mergeInput struct {
	// Kind is the merged records' kind reference: identity is the (kind, id)
	// pair, so a merge names the kind beside the two ids.
	Kind   string `json:"kind"`
	Winner string `json:"winner"`
	Loser  string `json:"loser"`
}

func (h *handler) postMerges(w http.ResponseWriter, r *http.Request) {
	var req mergeInput
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	if req.Kind == "" {
		writeError(w, http.StatusUnprocessableEntity, codeValidation,
			"kind is required — a merge addresses two records of one kind by (kind, id)")
		return
	}
	ctx := r.Context()
	ent, err := DatasetFrom(ctx).Merge(ctx, ActorFrom(ctx), req.Kind, req.Winner, req.Loser)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, ent)
}

type splitRequest struct {
	Merge string `json:"merge"`
}

func (h *handler) postSplits(w http.ResponseWriter, r *http.Request) {
	var req splitRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	ent, err := DatasetFrom(ctx).Split(ctx, ActorFrom(ctx), req.Merge)
	if err != nil {
		writeSubstrateError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, ent)
}

// maxRequestBody caps every JSON request body, authenticated or not.
const maxRequestBody = 1 << 20

func decodeBody(r *http.Request, v any) error {
	return decodeJSONStrict(http.MaxBytesReader(nil, r.Body, maxRequestBody), v)
}

// decodeJSONStrict decodes exactly one JSON value into v with unknown fields
// REFUSED, then requires the stream to end. A misspelled top-level
// key — a dropped `ifVersion` CAS precondition, a broadened `filter` — is a
// `bad_request` NAMING the offending key, never a silent drop. Openness stays
// only inside the map-valued fields (`properties`, `labels`, `annotations`,
// the filter's `properties`, an object property, a `json`-typed property),
// whose dynamic keys a struct decoder never inspects.
//
// The decode runs in two passes because encoding/json matches struct fields
// CASE-INSENSITIVELY, so `ifversion` would quietly bind to `ifVersion` — the
// freeze wants exact casing. The first pass walks the top-level object's keys
// against v's exact json tags (also refusing a duplicate key), the second is
// the ordinary strict struct decode.
func decodeJSONStrict(r io.Reader, v any) error {
	raw, err := io.ReadAll(r)
	if err != nil {
		return err // MaxBytesReader surfaces "http: request body too large" here
	}
	return decodeStrictBytes(raw, v, false)
}

// decodeStrictBytes is the shared strict decode (exact-key check,
// unknown-field refusal, end-of-stream check), moved to internal/strictjson so
// the GraphQL remarshal path (internal/gql) holds JSON-scalar inputs to the
// SAME rules this body decoder does. This name survives as the api package's
// spelling of it.
func decodeStrictBytes(raw []byte, v any, useNumber bool) error {
	return strictjson.DecodeBytes(raw, v, useNumber)
}
