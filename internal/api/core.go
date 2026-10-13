package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/geoah/substrate/internal/strictjson"
	"github.com/geoah/substrate/internal/substrate"
)

// Every core collection is an ordinary resource: nothing here has
// endpoint-shaped collection behavior, and the sole install path is the
// vocabulary apply batch.

// The token endpoints sit beside `/api/…` rather than inside the versioned
// resource tree, and live in auth_endpoints.go. There is no least-privilege
// apparatus (no scopes, no actor delegation check, no narrowing rules): a
// token has full access to its repository and nothing else. Repository
// management is the operator's DSN door, not an endpoint.

// idempotencyHeader is the request header a client names one attempt with.
// The engine reads it off the context (substrate.IdempotencyKeyFrom) and
// answers a repeat with the first attempt's outcome (docs/api.md,
// "Idempotency and retries").
const idempotencyHeader = "Idempotency-Key"

// idempotentContext is the request context with its Idempotency-Key bound.
// Only the handlers whose effect the server assigns call it: the id-less
// create, the function and agent calls, merge and split. Agent chat is a
// stream with nothing to replay, so its handler never does.
func idempotentContext(r *http.Request) context.Context {
	return substrate.WithIdempotencyKey(r.Context(), r.Header.Get(idempotencyHeader))
}

// The merge and split bodies ARE the engine's inputs (substrate.MergeInput,
// substrate.SplitInput): the kind beside the two ids, the merge record's id, and
// the optional version preconditions, decoded strictly so a misspelled
// `winnerVersion` is refused by name rather than dropped.
func (h *handler) postMerges(w http.ResponseWriter, r *http.Request) {
	var req substrate.MergeInput
	if !readJSON(w, r, &req, jsonBody) {
		return
	}
	if req.Kind == "" {
		writeError(w, http.StatusUnprocessableEntity, codeValidation,
			"kind is required — a merge addresses two records of one kind by (kind, id)")
		return
	}
	ctx := idempotentContext(r)
	ent, err := DatasetFrom(ctx).Merge(ctx, ActorFrom(ctx), req)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, ent)
}

func (h *handler) postSplits(w http.ResponseWriter, r *http.Request) {
	var req substrate.SplitInput
	if !readJSON(w, r, &req, jsonBody) {
		return
	}
	ctx := idempotentContext(r)
	ent, err := DatasetFrom(ctx).Split(ctx, ActorFrom(ctx), req)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, ent)
}

// maxRequestBody caps every JSON request body, authenticated or not, but the
// vocabulary batch (maxVocabularyApplyBody).
const maxRequestBody = 1 << 20

// bodyLimit is how much of a request body a route reads and how long its
// client has to send it. The server sets no ReadTimeout, because one would
// cut off every stream, so this deadline is what stops a client that sends
// the headers and then stalls from holding a goroutine and a connection.
type bodyLimit struct {
	bytes    int64
	deadline time.Duration
}

var (
	// jsonBody is every JSON body but the vocabulary batch.
	jsonBody = bodyLimit{bytes: maxRequestBody, deadline: 30 * time.Second}
	// vocabularyBody is the vocabulary apply and plan batch, which may carry
	// many inline function sources.
	vocabularyBody = bodyLimit{bytes: maxVocabularyApplyBody, deadline: 2 * time.Minute}
)

// bodyDeadline sets a read deadline d from now on the request's connection
// and returns the call that clears it. The caller clears it as soon as the
// body is read: a request with no body has net/http reading the connection
// in the background from the start, and a deadline expiring under that read
// cancels the request's context, which would end a handler still working
// on the answer. A read that failed leaves the deadline set, because
// net/http drains the unread rest of a body before it writes the answer,
// and without a deadline a client that stalled would hold that drain open
// for good. A writer that cannot take a deadline (a test recorder) reads
// without one.
func bodyDeadline(w http.ResponseWriter, d time.Duration) (clear func()) {
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Now().Add(d))
	return func() { _ = rc.SetReadDeadline(time.Time{}) }
}

// readBody reads a whole request body under limit. On a refusal it writes the
// answer and returns false: 413 for a body past the byte cap, 400 for one
// that stalled past the deadline or broke off, both with code bad_request.
func readBody(w http.ResponseWriter, r *http.Request, limit bodyLimit) ([]byte, bool) {
	clear := bodyDeadline(w, limit.deadline)
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit.bytes))
	if err == nil {
		clear()
		return raw, true
	}
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		writeError(w, http.StatusRequestEntityTooLarge, codeBadRequest,
			fmt.Sprintf("request body too large: the limit is %d bytes", tooBig.Limit))
	case errors.Is(err, os.ErrDeadlineExceeded):
		writeError(w, http.StatusBadRequest, codeBadRequest,
			fmt.Sprintf("request body not received within %s", limit.deadline))
	default:
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
	}
	return nil, false
}

// readJSON reads a JSON body under limit and decodes it strictly into v
// (decodeJSONStrict). On a refusal it writes the answer and returns false:
// readBody's, or 400 bad_request naming what the decode refused.
func readJSON(w http.ResponseWriter, r *http.Request, v any, limit bodyLimit) bool {
	raw, ok := readBody(w, r, limit)
	if !ok {
		return false
	}
	if err := decodeStrictBytes(raw, v, false); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return false
	}
	return true
}

// readRecordJSON is readJSON for a RECORD ENVELOPE (a put or a patch): the
// same strict decode, plus the replacement for a key the envelope retired.
// `edges` is a key somebody had reason to write, and a bare `unknown field
// "edges"` tells them the spelling is wrong without saying where a pointer goes
// now. Only the envelope decodes wrap: the same message on a filter or a login
// body would be advice about a document that caller is not writing.
func readRecordJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	raw, ok := readBody(w, r, jsonBody)
	if !ok {
		return false
	}
	if err := strictjson.NameRetiredKeys(decodeStrictBytes(raw, v, false)); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return false
	}
	return true
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
		return err
	}
	return decodeStrictBytes(raw, v, false)
}

// decodeStrictBytes is the shared strict decode (exact-key check,
// unknown-field refusal, end-of-stream check) in internal/strictjson, which
// the agent tools' JSON arguments are held to as well. This name is the api
// package's spelling of it.
func decodeStrictBytes(raw []byte, v any, useNumber bool) error {
	return strictjson.DecodeBytes(raw, v, useNumber)
}
