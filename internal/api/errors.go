package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"slices"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/geoah/substrate/internal/substrate"
)

// Error codes of the wire envelope. The set is closed: clients switch on it
// and nothing else ever appears in `error.code` (errorCodes below is the
// set). The client-error family is 4xx; the server family is split so a
// client can tell "try again" (unavailable) from a callable's own fault
// (function_failed) from "genuine fault" (internal). `compacted` is the
// changelog's one 410 signal.
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
	codeUnavailable = "unavailable"  // 503 — transient; ALWAYS with Retry-After
	codeCompacted   = "compacted"    // 410: a change cursor the changelog cannot resume; re-list
	// codeFunctionFailed is 500: a callable's body faulted while running. It is
	// distinct from codeValidation (422) so a caller tells its own bad
	// arguments from the function failing to execute (substrate.ErrFunctionFault).
	codeFunctionFailed = "function_failed" // 500 — a callable body faulted
	// codeParked is 409: a hand's retry of a parked delivery ran and failed
	// again, so the row stays parked one attempt older and the message names
	// the new error (substrate.ErrParked). The retry did what was asked; the
	// delivery still cannot be made, which is the row's state and not a
	// server fault, so it is never the masked 500 it once was.
	codeParked = "parked" // 409 — the retried delivery failed again and stays parked
	// codeTOTPRequired is 401: the password was right and the account has a
	// second factor, so the same request with a code is what succeeds
	// (substrate.ErrTOTPRequired). Distinct from codeAuth so a client asks for
	// the code rather than saying the password was wrong.
	codeTOTPRequired = "totp_required" // 401
)

// errorCodes is the closed set: every code constant above, and nothing a
// handler writes is outside it. The console's union
// (web/console/src/lib/api/errorcodes.golden.json) and the list in
// docs/api.md are held to it by errorcodes_test.go, so a code added above is
// added here, and the golden is regenerated with
// `go test ./internal/api/ -run TestErrorCodes -update`.
var errorCodes = []string{
	codeAuth,
	codeBadRequest,
	codeCompacted,
	codeConflict,
	codeForbidden,
	codeFunctionFailed,
	codeGuard,
	codeInternal,
	codeLossy,
	codeNotFound,
	codeParked,
	codeRateLimited,
	codeTOTPRequired,
	codeUnavailable,
	codeValidation,
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Debug("write response", "error", err)
	}
}

// writeError is the one emit that takes its code as a string, so it is where
// the closed set is held for a code that reaches it through a variable: one
// outside errorCodes is a defect of the handler, and the client is answered
// `internal` rather than a code it was never told about. Every other emit
// names a code constant in an ErrorPayload literal, which
// TestEveryEmittedCodeIsADeclaredConstant holds.
func writeError(w http.ResponseWriter, status int, code, msg string, problems ...string) {
	if !slices.Contains(errorCodes, code) {
		slog.Error("a handler answered with a code outside the closed set", "code", code, "status", status)
		writeJSON(w, http.StatusInternalServerError, substrate.ErrorEnvelope{Error: substrate.ErrorPayload{Code: codeInternal, Message: "internal error"}})
		return
	}
	writeJSON(w, status, substrate.ErrorEnvelope{Error: substrate.ErrorPayload{Code: code, Message: msg, Problems: problems}})
}

// restartRetryAfter is the Retry-After on a refusal only a server restart
// lifts (substrate.ErrRestartRequired).
const restartRetryAfter = 30 * time.Second

// keyRefusedRetryAfter is the Retry-After on a model call the vendor refused
// for its key (substrate.ErrKeyRefused): the embed drain's first wait on a
// refused key, and longer than the console waits out on its own.
const keyRefusedRetryAfter = time.Minute

// setAsideRetryAfter is the Retry-After on a repository the server set aside
// at its start (substrate.ErrSetAside): it serves again only after its
// operator repairs it and restarts the server, so a sender that retries on
// its own (a webhook's provider) waits minutes, not seconds.
const setAsideRetryAfter = 5 * time.Minute

// erasingRetryAfter is the Retry-After on a create at an id whose permanent
// delete is still erasing what the record owned (substrate.ErrErasing): the
// compaction pass that lifts it runs every few minutes.
const erasingRetryAfter = time.Minute

// writeUnavailable is the 503 emit: a transient condition the caller should
// retry. Retry-After is mandatory on every unavailable, so it is
// set here and cannot be forgotten at a call site. retryAfter rounds up to at
// least one second.
func writeUnavailable(w http.ResponseWriter, retryAfter time.Duration, msg string) {
	writeUnavailablePayload(w, retryAfter, substrate.ErrorPayload{Code: codeUnavailable, Message: msg})
}

// unavailablePayload is the 503 a bearer whose repository will not open
// answers: the service's own refusal where a client acts on it (a repository
// set aside at the server's start), else msg alone, the cause being one the
// client is not shown.
func unavailablePayload(cause error, msg string) substrate.ErrorPayload {
	if errors.Is(cause, substrate.ErrSetAside) {
		_, p := problemFor(cause)
		return p
	}
	return substrate.ErrorPayload{Code: codeUnavailable, Message: msg}
}

// writeUnavailablePayload is writeUnavailable for a payload that carries
// more than the message (a reason).
func writeUnavailablePayload(w http.ResponseWriter, retryAfter time.Duration, p substrate.ErrorPayload) {
	secs := int(retryAfter.Seconds())
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	writeJSON(w, http.StatusServiceUnavailable, substrate.ErrorEnvelope{Error: p})
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
	var pick *substrate.RepositoryRequiredError
	switch {
	case errors.Is(err, substrate.ErrWriteInDoubt):
		return inDoubtProblem(err)
	case errors.As(err, &pick):
		return http.StatusConflict, substrate.ErrorPayload{
			Code: codeConflict, Reason: substrate.ReasonRepositoryRequired,
			Message: "this person may open several repositories: send the sign-in again naming one in `repository`", Repositories: pick.Repositories,
		}
	case errors.As(err, &ve):
		// The one reason that arrives as a value: one outside the closed set
		// is not sent, and the client acts on the code.
		reason := ve.Reason
		if reason != "" && !slices.Contains(substrate.ErrorReasons, reason) {
			slog.Error("a validation refusal carried a reason outside the closed set", "reason", reason)
			reason = ""
		}
		return http.StatusUnprocessableEntity, substrate.ErrorPayload{Code: codeValidation, Reason: reason, Message: err.Error(), Problems: ve.Problems, ProblemDetails: ve.Details()}
	case errors.Is(err, substrate.ErrValidation):
		return http.StatusUnprocessableEntity, substrate.ErrorPayload{Code: codeValidation, Message: err.Error()}
	case errors.Is(err, substrate.ErrParked):
		return http.StatusConflict, substrate.ErrorPayload{Code: codeParked, Message: err.Error()}
	case errors.Is(err, substrate.ErrFunctionFault):
		return http.StatusInternalServerError, substrate.ErrorPayload{Code: codeFunctionFailed, Message: err.Error()}
	case errors.Is(err, substrate.ErrCorrupt):
		// The message names the damaged digest, which the caller addressed
		// itself, so it is said back instead of masked.
		return http.StatusInternalServerError, substrate.ErrorPayload{Code: codeInternal, Message: err.Error()}
	case errors.Is(err, substrate.ErrNotFound):
		return http.StatusNotFound, substrate.ErrorPayload{Code: codeNotFound, Message: err.Error()}
	case errors.Is(err, substrate.ErrConflict):
		p := substrate.ErrorPayload{Code: codeConflict, Message: err.Error()}
		switch {
		case errors.Is(err, substrate.ErrFirstAttemptRunning):
			p.Reason = substrate.ReasonFirstAttemptRunning
		case errors.Is(err, substrate.ErrFirstAttemptLanded):
			p.Reason = substrate.ReasonFirstAttemptLanded
		}
		return http.StatusConflict, p
	case errors.Is(err, substrate.ErrLossyConversion):
		return http.StatusForbidden, substrate.ErrorPayload{Code: codeLossy, Message: err.Error()}
	case errors.Is(err, substrate.ErrAllowance):
		return http.StatusForbidden, substrate.ErrorPayload{Code: codeGuard, Reason: substrate.ReasonQuota, Message: err.Error()}
	case errors.Is(err, substrate.ErrGuard):
		return http.StatusForbidden, substrate.ErrorPayload{Code: codeGuard, Message: err.Error()}
	case errors.Is(err, substrate.ErrForbidden):
		return http.StatusForbidden, substrate.ErrorPayload{Code: codeForbidden, Message: err.Error()}
	case errors.Is(err, substrate.ErrTOTPRequired):
		return http.StatusUnauthorized, substrate.ErrorPayload{Code: codeTOTPRequired, Message: err.Error()}
	case errors.Is(err, substrate.ErrAuth):
		return http.StatusUnauthorized, substrate.ErrorPayload{Code: codeAuth, Message: err.Error()}
	case errors.Is(err, substrate.ErrTooLarge):
		return http.StatusRequestEntityTooLarge, substrate.ErrorPayload{Code: codeBadRequest, Message: err.Error()}
	case errors.Is(err, substrate.ErrKeyRefused):
		return http.StatusServiceUnavailable, substrate.ErrorPayload{Code: codeUnavailable, Reason: substrate.ReasonKeyRefused, Message: err.Error()}
	case errors.Is(err, substrate.ErrSetAside):
		return http.StatusServiceUnavailable, substrate.ErrorPayload{Code: codeUnavailable, Reason: substrate.ReasonSetAside, Message: err.Error()}
	case errors.Is(err, substrate.ErrObservedRederiving):
		return http.StatusServiceUnavailable, substrate.ErrorPayload{Code: codeUnavailable, Reason: substrate.ReasonObservedRederiving, Message: err.Error()}
	case errors.Is(err, substrate.ErrErasing):
		return http.StatusServiceUnavailable, substrate.ErrorPayload{Code: codeUnavailable, Reason: substrate.ReasonErasing, Message: err.Error()}
	case errors.Is(err, substrate.ErrRestartRequired), errors.Is(err, substrate.ErrUnavailable):
		return http.StatusServiceUnavailable, substrate.ErrorPayload{Code: codeUnavailable, Message: err.Error()}
	default:
		return http.StatusInternalServerError, substrate.ErrorPayload{Code: codeInternal, Message: "internal error"}
	}
}

// inDoubtMessage is what a write whose commit may have landed answers in
// place of the masked "internal error": the cause is the server's, and what
// the client needs is what it may conclude.
const inDoubtMessage = "the write may have landed: the server lost the database's answer to its commit, or a step after the commit failed; read the record, or repeat the request under the same Idempotency-Key"

// inDoubtProblem answers an error that followed a commit that may have landed
// (substrate.ErrWriteInDoubt) with the reason in_doubt: 503 where the
// repository now refuses writes until a restart, and 500 otherwise. No other
// code carries it, because no refusal of the request itself reaches the
// commit.
func inDoubtProblem(err error) (int, substrate.ErrorPayload) {
	if errors.Is(err, substrate.ErrRestartRequired) {
		return http.StatusServiceUnavailable, substrate.ErrorPayload{Code: codeUnavailable, Reason: substrate.ReasonInDoubt, Message: err.Error()}
	}
	return http.StatusInternalServerError, substrate.ErrorPayload{Code: codeInternal, Reason: substrate.ReasonInDoubt, Message: inDoubtMessage}
}

// writeSubstrateError maps the engine's sentinel errors onto the wire
// envelope; unknown errors are 500 without leaking their text shape. A
// `forbidden` to an app's scoped token carries `WWW-Authenticate:
// insufficient_scope`, because the scoped surface refuses with
// substrate.ErrForbidden; no other refusal does, since no scope the app
// could ask for changes it.
//
// A 5xx is logged at ERROR with the request's method, route and repository,
// because a line without them cannot be traced to anything. A failure that is
// the client's own cancellation is logged at DEBUG instead: the client went
// away, the server did nothing wrong, and the body written after it reaches
// nobody. A 503 is logged by writeUnavailableCause.
func writeSubstrateError(w http.ResponseWriter, r *http.Request, err error) {
	status, p := problemFor(err)
	if p.Code == codeForbidden {
		if _, scoped := substrate.ScopesFrom(r.Context()); scoped {
			w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope"`)
		}
	}
	writeProblem(w, r, err, status, p)
}

// writeProblem writes the problem err maps to, logging a 5xx as
// writeSubstrateError says, for a door that words the problem itself.
func writeProblem(w http.ResponseWriter, r *http.Request, err error, status int, p substrate.ErrorPayload) {
	if status == http.StatusServiceUnavailable {
		writeUnavailableCause(w, r, err, p)
		return
	}
	if status >= http.StatusInternalServerError {
		if clientCanceled(r.Context(), err) {
			slog.Debug("request canceled by the client", append(requestLogAttrs(r), "error", err)...)
		} else {
			slog.Error("request failed", append(requestLogAttrs(r), "error", err)...)
		}
	}
	writeJSON(w, status, substrate.ErrorEnvelope{Error: p})
}

// writeUnavailableCause answers a 503 whose cause is err, and logs it once
// with the request, because the cause (a full disk under the data root, a
// database that refused the token lookup) is a server condition the operator
// must see. A refusal only a restart lifts is an ERROR, and its client is
// told to come back later than a moment's wait; so is a key the vendor
// refused, as a WARN. Any other is a WARN, and a client's own cancellation a
// DEBUG.
func writeUnavailableCause(w http.ResponseWriter, r *http.Request, err error, p substrate.ErrorPayload) {
	retryAfter := time.Second
	attrs := append(requestLogAttrs(r), "error", err)
	switch {
	case errors.Is(err, substrate.ErrRestartRequired):
		slog.Error("request refused until the server restarts", attrs...)
		retryAfter = restartRetryAfter
	case errors.Is(err, substrate.ErrKeyRefused):
		slog.Warn("request answered unavailable: the vendor refused the key", attrs...)
		retryAfter = keyRefusedRetryAfter
	case errors.Is(err, substrate.ErrSetAside):
		// The start logged why, once; a line per refused request would bury it.
		slog.Debug("request refused: the repository is set aside", attrs...)
		retryAfter = setAsideRetryAfter
	case errors.Is(err, substrate.ErrErasing):
		slog.Info("request refused: the record at its id is still being erased", attrs...)
		retryAfter = erasingRetryAfter
	case clientCanceled(r.Context(), err):
		slog.Debug("request canceled by the client", attrs...)
	default:
		slog.Warn("request answered unavailable", attrs...)
	}
	writeUnavailablePayload(w, retryAfter, p)
}

// clientCanceled reports whether err is the request's own cancellation. Both
// halves are needed: a context.Canceled while the request's context is still
// live came from a context the server canceled, which is a server fault, and
// a context.DeadlineExceeded is a server-side timeout.
func clientCanceled(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) && errors.Is(ctx.Err(), context.Canceled)
}

// requestLogAttrs names the request a log line is about: its method, chi's
// route pattern (the path when no route matched), the id its response
// carries in X-Request-Id and, once the bearer check has run, the repository.
func requestLogAttrs(r *http.Request) []any {
	route := r.URL.Path
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		if p := rctx.RoutePattern(); p != "" {
			route = p
		}
	}
	attrs := []any{"method", r.Method, "route", route}
	if id := requestIDFrom(r.Context()); id != "" {
		attrs = append(attrs, "request", id)
	}
	if ds := DatasetFrom(r.Context()); ds != nil {
		attrs = append(attrs, "repository", ds.Repository().ID)
	}
	return attrs
}

// recoverer turns a handler's panic into a 500 with the error envelope and
// one ERROR line carrying the request and the stack, so a fault in one
// request neither ends the process nor goes unlogged. http.ErrAbortHandler
// passes through untouched: it is how a handler cuts its response short on
// purpose (the export), and net/http closes the connection on it without a
// line of its own.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &startedWriter{ResponseWriter: w}
		defer func() {
			rvr := recover()
			if rvr == nil {
				return
			}
			if err, ok := rvr.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(rvr)
			}
			slog.Error("request panicked", append(requestLogAttrs(r),
				"panic", fmt.Sprint(rvr), "stack", string(debug.Stack()))...)
			if sw.started {
				// The status and part of the body are already out, so an
				// envelope now would be appended to them and the response
				// would end looking complete. Aborting closes the connection
				// instead, which the client reads as a cut response.
				panic(http.ErrAbortHandler)
			}
			writeError(w, http.StatusInternalServerError, codeInternal, "internal error")
		}()
		next.ServeHTTP(sw, r)
	})
}

// startedWriter records whether the status line has gone out, for the
// recoverer. A final status, a body write and a flush each send it, and
// chi's wrapper leaves its Status at 0 after a flush, so it cannot say.
// Flush, ReadFrom and Unwrap keep what the streams, io.Copy's sendfile path
// and http.ResponseController reach through the writer.
type startedWriter struct {
	http.ResponseWriter
	started bool
}

func (s *startedWriter) WriteHeader(code int) {
	if code >= http.StatusOK || code == http.StatusSwitchingProtocols {
		s.started = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *startedWriter) Write(b []byte) (int, error) {
	s.started = true
	return s.ResponseWriter.Write(b)
}

func (s *startedWriter) Flush() {
	s.started = true
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *startedWriter) ReadFrom(src io.Reader) (int64, error) {
	s.started = true
	return io.Copy(s.ResponseWriter, src)
}

func (s *startedWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }
