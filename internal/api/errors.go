package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// Error codes of the wire envelope. The set is closed: clients switch on it
// and nothing else ever appears in `error.code`. The client-error
// family is 4xx; the server family is split so a client can tell "try again"
// (unavailable) from "never here" (unsupported) from "genuine fault"
// (internal). `compacted` is the changelog's one 410 signal.
const (
	codeValidation  = "validation"   // 422
	codeConflict    = "conflict"     // 409
	codeGuard       = "guard"        // 403
	codeNotFound    = "not_found"    // 404
	codeForbidden   = "forbidden"    // 403
	codeAuth        = "auth"         // 401
	codeRateLimited = "rate_limited" // 429 (+ Retry-After)
	codeBadRequest  = "bad_request"  // 400
	codeInternal    = "internal"     // 500 — a genuine, unexpected server fault
	codeUnsupported = "unsupported"  // 501 — a capability absent from this deployment
	codeUnavailable = "unavailable"  // 503 — transient; ALWAYS with Retry-After
	codeCompacted   = "compacted"    // 410 — from= below the retention horizon; re-list
)

type errorPayload struct {
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	Problems []string `json:"problems,omitempty"`
}

type errorEnvelope struct {
	Error errorPayload `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Debug("write response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, msg string, problems ...string) {
	writeJSON(w, status, errorEnvelope{Error: errorPayload{Code: code, Message: msg, Problems: problems}})
}

// writeUnsupported is the 501 emit: a capability this deployment does not
// carry (no bundles, no change feed, no agents, …). It is NOT a server fault
// and NOT the way to feature-detect — GET /.well-known/substrate/server.json
// discovery is.
func writeUnsupported(w http.ResponseWriter, msg string) {
	writeError(w, http.StatusNotImplemented, codeUnsupported, msg)
}

// writeUnavailable is the 503 emit: a transient condition the caller should
// retry. Ruling A6 makes Retry-After mandatory on every unavailable, so it is
// set here and cannot be forgotten at a call site. retryAfter rounds up to at
// least one second.
func writeUnavailable(w http.ResponseWriter, retryAfter time.Duration, msg string) {
	secs := int(retryAfter.Seconds())
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	writeError(w, http.StatusServiceUnavailable, codeUnavailable, msg)
}

// writeCompacted is the 410 emit for a `from=`/`before=` seq below the
// retention horizon: the requested history is gone, so the client
// must re-list and resume from a fresh cursor. It is a distinct, stable signal
// a client MUST handle, never a silent gap.
func writeCompacted(w http.ResponseWriter, horizon int64) {
	writeError(w, http.StatusGone, codeCompacted,
		fmt.Sprintf("requested seq is below the retention horizon %d; re-list and resume", horizon))
}

// problemFor maps an engine sentinel error onto the wire status + problem
// object. It is the single source of truth shared by the REST writer and the
// watch terminal error frame, so a substrate error means the same thing
// wherever it surfaces.
func problemFor(err error) (int, errorPayload) {
	var ve *substrate.ValidationError
	switch {
	case errors.As(err, &ve):
		return http.StatusUnprocessableEntity, errorPayload{Code: codeValidation, Message: err.Error(), Problems: ve.Problems}
	case errors.Is(err, substrate.ErrValidation):
		return http.StatusUnprocessableEntity, errorPayload{Code: codeValidation, Message: err.Error()}
	case errors.Is(err, substrate.ErrNotFound):
		return http.StatusNotFound, errorPayload{Code: codeNotFound, Message: err.Error()}
	case errors.Is(err, substrate.ErrConflict):
		return http.StatusConflict, errorPayload{Code: codeConflict, Message: err.Error()}
	case errors.Is(err, substrate.ErrGuard):
		return http.StatusForbidden, errorPayload{Code: codeGuard, Message: err.Error()}
	case errors.Is(err, substrate.ErrForbidden):
		return http.StatusForbidden, errorPayload{Code: codeForbidden, Message: err.Error()}
	case errors.Is(err, substrate.ErrAuth):
		return http.StatusUnauthorized, errorPayload{Code: codeAuth, Message: err.Error()}
	default:
		return http.StatusInternalServerError, errorPayload{Code: codeInternal, Message: "internal error"}
	}
}

// writeSubstrateError maps the engine's sentinel errors onto the wire
// envelope; unknown errors are 500 without leaking their text shape.
func writeSubstrateError(w http.ResponseWriter, err error) {
	status, p := problemFor(err)
	if status >= http.StatusInternalServerError {
		slog.Error("request failed", "error", err)
	}
	writeJSON(w, status, errorEnvelope{Error: p})
}
