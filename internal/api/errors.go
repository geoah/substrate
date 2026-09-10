package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// Error codes of the wire envelope. The set is closed: clients switch on it
// and nothing else ever appears in `error.code`. The client-error
// family is 4xx; the server family is split so a client can tell "try again"
// (unavailable) from "never here" (unsupported) from "genuine fault"
// (internal). `compacted` is the changelog's one 410 signal.
const (
	codeValidation = "validation" // 422
	codeConflict   = "conflict"   // 409
	codeGuard      = "guard"      // 403
	// codeLossy is 403: a declaration change whose conversion plan removes
	// values from the fold arrived without a confirmation for that plan
	// (substrate.ErrLossyConversion). Distinct from codeGuard so a client
	// knows to preview the plan and confirm it rather than migrate records.
	codeLossy       = "lossy"        // 403
	codeNotFound    = "not_found"    // 404
	codeForbidden   = "forbidden"    // 403
	codeAuth        = "auth"         // 401
	codeRateLimited = "rate_limited" // 429 (+ Retry-After)
	codeBadRequest  = "bad_request"  // 400
	codeInternal    = "internal"     // 500 — a genuine, unexpected server fault
	codeUnsupported = "unsupported"  // 501 — a door this deployment does not open
	codeUnavailable = "unavailable"  // 503 — transient; ALWAYS with Retry-After
	codeCompacted   = "compacted"    // 410: a change cursor the changelog cannot resume; re-list
	// codeFunctionFailed is 500: a callable's body faulted while running. It is
	// distinct from codeValidation (422) so a caller tells its own bad
	// arguments from the function failing to execute (substrate.ErrFunctionFault).
	codeFunctionFailed = "function_failed" // 500 — a callable body faulted
)

// problemDetails derives the structured siblings from the engine's problem
// strings. A string without a ": " separator keeps its whole text as the
// message and an empty path, so a malformed problem never drops silently.
func problemDetails(problems []string) []substrate.ProblemDetail {
	if len(problems) == 0 {
		return nil
	}
	out := make([]substrate.ProblemDetail, len(problems))
	for i, p := range problems {
		if path, msg, ok := strings.Cut(p, ": "); ok {
			out[i] = substrate.ProblemDetail{Path: path, Message: msg}
		} else {
			out[i] = substrate.ProblemDetail{Message: p}
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Debug("write response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, msg string, problems ...string) {
	writeJSON(w, status, substrate.ErrorEnvelope{Error: substrate.ErrorPayload{Code: code, Message: msg, Problems: problems}})
}

// writeUnsupported is the 501 emit: a door this deployment does not open. The
// register endpoints on a deployment with no invite code configured are the
// one, and it is configuration, not a missing method set; discovery says so
// as registration.open. It is not a server fault.
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

// writeCompacted is the 410 emit for a change cursor the changelog cannot
// resume: a `from=`/`before=` seq below the retention horizon, or a `from`
// above the head, under another history generation, or with no generation at
// all (watch.go resumeCursor). The history the cursor addressed is not the
// one this changelog holds, so the client must re-list and resume from the
// head the problem object names. It is a distinct, stable signal a client
// MUST handle, never a silent gap.
func writeCompacted(w http.ResponseWriter, head substrate.ChangelogHead, msg string) {
	seq := head.Seq
	writeJSON(w, http.StatusGone, substrate.ErrorEnvelope{Error: substrate.ErrorPayload{
		Code: codeCompacted, Message: msg, Head: &seq, Generation: head.Generation,
	}})
}

// problemFor maps an engine sentinel error onto the wire status + problem
// object. It is the single source of truth shared by the REST writer and the
// watch terminal error frame, so a substrate error means the same thing
// wherever it surfaces.
func problemFor(err error) (int, substrate.ErrorPayload) {
	var ve *substrate.ValidationError
	switch {
	case errors.As(err, &ve):
		return http.StatusUnprocessableEntity, substrate.ErrorPayload{Code: codeValidation, Message: err.Error(), Problems: ve.Problems, ProblemDetails: problemDetails(ve.Problems)}
	case errors.Is(err, substrate.ErrValidation):
		return http.StatusUnprocessableEntity, substrate.ErrorPayload{Code: codeValidation, Message: err.Error()}
	case errors.Is(err, substrate.ErrFunctionFault):
		return http.StatusInternalServerError, substrate.ErrorPayload{Code: codeFunctionFailed, Message: err.Error()}
	case errors.Is(err, substrate.ErrNotFound):
		return http.StatusNotFound, substrate.ErrorPayload{Code: codeNotFound, Message: err.Error()}
	case errors.Is(err, substrate.ErrConflict):
		return http.StatusConflict, substrate.ErrorPayload{Code: codeConflict, Message: err.Error()}
	case errors.Is(err, substrate.ErrLossyConversion):
		return http.StatusForbidden, substrate.ErrorPayload{Code: codeLossy, Message: err.Error()}
	case errors.Is(err, substrate.ErrGuard):
		return http.StatusForbidden, substrate.ErrorPayload{Code: codeGuard, Message: err.Error()}
	case errors.Is(err, substrate.ErrForbidden):
		return http.StatusForbidden, substrate.ErrorPayload{Code: codeForbidden, Message: err.Error()}
	case errors.Is(err, substrate.ErrAuth):
		return http.StatusUnauthorized, substrate.ErrorPayload{Code: codeAuth, Message: err.Error()}
	case errors.Is(err, substrate.ErrUnavailable):
		return http.StatusServiceUnavailable, substrate.ErrorPayload{Code: codeUnavailable, Message: err.Error()}
	default:
		return http.StatusInternalServerError, substrate.ErrorPayload{Code: codeInternal, Message: "internal error"}
	}
}

// writeSubstrateError maps the engine's sentinel errors onto the wire
// envelope; unknown errors are 500 without leaking their text shape.
func writeSubstrateError(w http.ResponseWriter, err error) {
	status, p := problemFor(err)
	if status == http.StatusServiceUnavailable {
		writeUnavailable(w, time.Second, p.Message)
		return
	}
	if status >= http.StatusInternalServerError {
		slog.Error("request failed", "error", err)
	}
	writeJSON(w, status, substrate.ErrorEnvelope{Error: p})
}
