package substrate

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors. The engine wraps these (errors.Is-matchable) with
// human-readable detail; the API layer maps them to status codes.
var (
	ErrNotFound = errors.New("substrate: not found")
	ErrConflict = errors.New("substrate: version conflict") // CAS mismatch
	ErrGuard    = errors.New("substrate: operation not allowed here")
	// ErrLossyConversion marks a vocabulary change whose conversion plan
	// removes values from the fold (a dropped property nulled on live records,
	// an enum value renamed onto a value the stored declaration still admits)
	// and arrived without a ConversionConfirm for that plan, or with one for
	// another plan. The engine wraps it together with ErrGuard; the API
	// answers 403 under its own code, so a caller can preview the plan and
	// confirm it (decision 0067).
	ErrLossyConversion = errors.New("substrate: lossy conversion refused")
	ErrValidation      = errors.New("substrate: validation failed")
	// ErrStaleHistory is a list cursor this server cannot resume, through no
	// doing of the client's: it was minted under another history generation
	// (a restore replaced the changelog since), or the filter and order it
	// was cut from now resolve to another order (a declaration or a unit
	// changed), or by another version of the cursor's format. The client
	// lists again. It wraps ErrValidation so a caller that only knows the
	// older sentinel still refuses it; the API answers it as the changelog's
	// own 410.
	ErrStaleHistory = fmt.Errorf("%w: the cursor cannot be resumed", ErrValidation)
	ErrForbidden    = errors.New("substrate: forbidden") // e.g. foreign label namespace, system type write
	// ErrGated marks a write a policy HELD rather than refused: it converted
	// into a recordpatchrequest awaiting review, and the message names it. The
	// policy door runs only inside the agent loop, where the hold becomes a
	// tool result the model reads, so this sentinel never leaves the loop and
	// is not a wire error code (0037).
	ErrGated = errors.New("substrate: held for review")
	ErrAuth  = errors.New("substrate: authentication failed")
	// ErrTOTPRequired is an ErrAuth that says one more thing: the password was
	// right, the account has a second factor, and no code came. A client asks
	// for the code and tries again (decision 0237).
	ErrTOTPRequired = fmt.Errorf("%w: a code from the authenticator is required", ErrAuth)
	// ErrFunctionFault marks a callable that failed WHILE ITS BODY RAN — a
	// raise, a bad effect, a failed sub-call. It is distinct from ErrValidation,
	// which is the caller's arguments failing the declared input schema before
	// the body runs. A body fault is a server-side execution fault (500
	// function_failed), never invalid caller input (422).
	ErrFunctionFault = errors.New("substrate: function body failed")
	// ErrParked marks a hand's retry of a parked delivery that RAN AND FAILED
	// AGAIN: the row stays parked, one attempt older and carrying the new
	// error, and the message names that error's first line. It is the
	// retry's own outcome, not a fault of the server answering it (409
	// parked, never a masked 500 "internal error"): the operator asked for the
	// delivery to be made and is told why it still cannot be, with the row
	// under `…/parked` holding the whole text. It is distinct from
	// ErrValidation, which refuses the retry before anything runs (a callable
	// that no longer resolves), and from ErrConflict, which is the same row
	// already being retried.
	ErrParked = errors.New("substrate: delivery parked")
	// ErrUnavailable marks an answer the substrate cannot give YET: the state
	// it needs is still being built, and the same call succeeds later without
	// the caller changing anything. Semantic search returns it while the
	// resolved embeddings pair has no vectors and the queue holds work (a
	// repository restored from its directory, a re-embed, before the drain
	// has bought them), so "no vectors yet" never reads as "no matches". It is
	// distinct from ErrValidation, which needs the caller to change something.
	ErrUnavailable = errors.New("substrate: not available yet")
	// ErrCorrupt marks stored data that fails its own check: a blob whose
	// bytes no longer hash to the digest that names them, or no longer have
	// the size its manifest declares. The damage is the store's, and no
	// request the caller can make repairs it, so the API answers 500
	// `internal` with the message, which names the digest, rather than the
	// masked "internal error"; a 409 would tell the client to re-read and
	// retry, and every retry would meet the same bytes. `repository verify`
	// reports the same damage.
	ErrCorrupt = errors.New("substrate: stored data is corrupt")
	// ErrRestartRequired marks a refusal only a server restart lifts: the
	// engine holds state it cannot reconcile with the database while it
	// runs, so it refuses rather than guess, and the boot check is the
	// repair. A repository directory left behind its tables by a write that
	// failed after it committed, when the in-process catch-up failed too, is
	// the case. It is distinct from ErrUnavailable, which the same call
	// outlives with nobody acting, so an engine caller that retries an
	// ErrUnavailable does not loop on it. The API still answers it `503
	// unavailable` with Retry-After, and the message names the restart.
	ErrRestartRequired = errors.New("substrate: refused until the server restarts")
	// ErrTooLarge marks a request body past a bound only the engine knows,
	// because it depends on what the request addresses: a webhook body over
	// what an agent callable reads as its user message. The API answers 413
	// `bad_request`.
	ErrTooLarge = errors.New("substrate: request body too large")
	// ErrNoRunningTurn answers an attach or a stop on a thread no detached
	// chat turn of this process runs on: the thread is settled, or a turn no
	// client can stream (a triggered run) holds it. It wraps ErrConflict, so
	// the API answers 409.
	ErrNoRunningTurn = fmt.Errorf("%w: no running chat turn", ErrConflict)
	// ErrObservedRederiving answers an aggregate read over an observed index
	// the background pass still owes: the records moved under a changed
	// declaration, a changed metric's `rollups`, a rebuild or an import, and
	// the index holds what they derived before. It wraps ErrUnavailable, so
	// the API answers 503 with Retry-After, and the payload's reason is
	// ReasonObservedRederiving so a client tells it from every other 503.
	ErrObservedRederiving = fmt.Errorf("%w: the observed index this read selects is being re-derived", ErrUnavailable)
	// ErrErasing refuses a create at an id whose permanent delete is still
	// erasing what the record owned, at any level of it: the batches find
	// what it owned by its id, so a record created there meanwhile would
	// lose what it owns to them. The compaction pass lifts it with nobody
	// acting, so it wraps ErrUnavailable: the API answers 503 with
	// Retry-After and the reason ReasonErasing, and a delivery whose write
	// meets it is retried over minutes like a temporary failure. The
	// message names the deleted record and how many it still owns.
	ErrErasing = fmt.Errorf("%w: the record at this id is being erased", ErrUnavailable)
	// ErrAllowance refuses a call the operator would pay for (decision 0235)
	// because the repository's allowance for the UTC day is used up, or would
	// be by this call's reserved ceiling. The message names the site, the
	// reset and the way out. It wraps ErrGuard, so the API answers 403
	// `guard`, and the payload's reason is ReasonQuota so a client tells it
	// from every other guard.
	ErrAllowance = fmt.Errorf("%w: daily allowance", ErrGuard)
	// ErrKeyRefused refuses a model call because the vendor refused the key
	// it carried (a 401, or a 403 whose error names the key): the
	// operator's key or the owner's own on an llm/provider row. The caller's
	// own access is not in question, so it is neither ErrGuard nor
	// ErrForbidden, and nothing changes until someone writes a working key,
	// so it is not ErrUnavailable, which an engine caller may retry. The API
	// answers it 503 `unavailable` with Retry-After, as it answers
	// ErrRestartRequired, and the payload's reason is ReasonKeyRefused. The
	// message names the vendor and where to add a key.
	ErrKeyRefused = errors.New("substrate: the provider refused its key")
	// ErrWriteInDoubt marks an error that follows a write whose commit may
	// have landed: the commit was sent and its answer was lost, or Postgres
	// committed and a step after it failed (decision 0165). The engine wraps
	// it together with the cause, so the cause still decides the status; the
	// payload's reason is ReasonInDoubt. A write's error without it rolled
	// back. The client reads the record, or repeats the request under the
	// same Idempotency-Key, which answers the first attempt's outcome if it
	// landed.
	ErrWriteInDoubt = errors.New("substrate: the write may have landed")
	// ErrFirstAttemptRunning and ErrFirstAttemptLanded mark a conflict that
	// is about the first attempt under an Idempotency-Key, not this request,
	// which did not run: the first has not answered yet and may still land
	// (ReasonFirstAttemptRunning), or it landed and its outcome was too large
	// to keep (ReasonFirstAttemptLanded), so its write stands and is read
	// where the message says. Each follows ErrConflict, whose words lead.
	ErrFirstAttemptRunning = errors.New("the first attempt under this key has not answered")
	ErrFirstAttemptLanded  = errors.New("the first attempt under this key landed")
	// ErrSignOutIncomplete marks a password change that asked to sign out
	// everywhere and did not finish: the password changed and the tokens
	// were not ended, or either write is in doubt (wrapped with
	// ErrWriteInDoubt, which then means the password may have changed). The
	// API answers it with a message that says which, and what to do (sign
	// out everywhere again, with the new password or, while in doubt, the
	// one that signs in), whatever the cause was.
	ErrSignOutIncomplete = errors.New("substrate: the password change asked to sign out everywhere, and the sign-out did not finish")
	// ErrSetAside refuses every request for a repository the server set
	// aside at its start: its start-up check found it damaged, so the
	// server serves the others and never opens this one, and writes nothing
	// to it from then on. It lifts when the operator repairs it and restarts the
	// server, so it is not ErrUnavailable, which an engine caller may retry.
	// The API answers it 503 `unavailable` with Retry-After, and the
	// payload's reason is ReasonSetAside.
	ErrSetAside = errors.New("substrate: the repository is set aside")
)

// ReasonObservedRederiving is the ErrorPayload reason of ErrObservedRederiving.
const ReasonObservedRederiving = "observed_rederiving"

// ReasonErasing is the ErrorPayload reason of ErrErasing.
const ReasonErasing = "erasing"

// ReasonQuota is the ErrorPayload reason of ErrAllowance.
const ReasonQuota = "quota"

// ReasonKeyRefused is the ErrorPayload reason of ErrKeyRefused.
const ReasonKeyRefused = "key_refused"

// ReasonSetAside is the ErrorPayload reason of ErrSetAside.
const ReasonSetAside = "set_aside"

// ReasonStaleRevision is the ErrorPayload reason of a ValidationError whose
// problems include a written `revision` below the stored one (decision 0162):
// a newer write of the record landed first, so that write never lands
// whatever else it fixes, and a client that queued it drops it rather than
// asking its owner. It names no record, so a request that writes several (a
// function call's effects, refused and rolled back whole) is not safe to drop
// on it alone. A write that removes a stored revision is not stale and
// carries no reason.
const ReasonStaleRevision = "stale_revision"

// ReasonInDoubt is the ErrorPayload reason of ErrWriteInDoubt, on `internal`
// and on `unavailable`.
const ReasonInDoubt = "in_doubt"

// ReasonRepositoryRequired is the ErrorPayload reason of a
// RepositoryRequiredError (auth.go), on `conflict`.
const ReasonRepositoryRequired = "repository_required"

// ReasonFirstAttemptRunning and ReasonFirstAttemptLanded are the
// ErrorPayload reasons of ErrFirstAttemptRunning and ErrFirstAttemptLanded,
// on `conflict`.
const (
	ReasonFirstAttemptRunning = "first_attempt_running"
	ReasonFirstAttemptLanded  = "first_attempt_landed"
)

// ErrorReasons is the closed set of reasons: every Reason constant above and
// nothing else. A client switches on a reason, so one it was never told about
// must not reach it: the API drops a reason outside the list
// (internal/api problemFor), and the console's union and docs/api.md are held
// to it (internal/api errorreasons_test.go).
var ErrorReasons = []string{
	ReasonErasing,
	ReasonFirstAttemptLanded,
	ReasonFirstAttemptRunning,
	ReasonInDoubt,
	ReasonKeyRefused,
	ReasonObservedRederiving,
	ReasonQuota,
	ReasonRepositoryRequired,
	ReasonSetAside,
	ReasonStaleRevision,
}

// ErrorEnvelope is the one body every refused request answers with.
type ErrorEnvelope struct {
	Error ErrorPayload `json:"error"`
}

// ErrorPayload is the refusal itself: a code from the closed wire set, a
// message, and for a validation refusal the problems twice, as the engine's
// strings and split into ProblemDetails. Head and Generation ride a
// `compacted` refusal only: the changelog head and history generation the
// client re-lists from and resumes at, so a refused cursor names its
// replacement. Head is a pointer so an empty changelog's 0 is still written.
// Reason narrows a code for a client that acts on the case, from a closed
// set: ReasonObservedRederiving, ReasonKeyRefused, ReasonSetAside and
// ReasonErasing on `unavailable`, ReasonQuota on `guard`, ReasonStaleRevision on `validation`,
// ReasonInDoubt on `internal` and `unavailable`, and
// ReasonFirstAttemptRunning and ReasonFirstAttemptLanded on `conflict`.
// A client that does not know a reason acts on the code.
type ErrorPayload struct {
	Code           string          `json:"code"`
	Reason         string          `json:"reason,omitempty"`
	Message        string          `json:"message"`
	Problems       []string        `json:"problems,omitempty"`
	ProblemDetails []ProblemDetail `json:"problemDetails,omitempty"`
	Head           *int64          `json:"head,omitempty"`
	Generation     string          `json:"generation,omitempty"`
	// Repositories are the repositories a person may open, on a sign-in
	// refused with ReasonRepositoryRequired: one of them goes in the
	// request's `repository`.
	Repositories []RepositoryChoice `json:"repositories,omitempty"`
}

// ProblemDetail is the field-addressable form of one validation problem. The
// engine emits problems as "path: message" strings (`props.name: required`);
// the API splits each on its first ": " so a form maps a problem to the input
// it concerns without parsing prose. A string without the separator keeps its
// whole text as the message and an empty path.
type ProblemDetail struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// ValidationError carries per-field detail for the UI's YAML editor. Reason
// is the ErrorPayload reason the refusal answers with, "" or
// ReasonStaleRevision.
type ValidationError struct {
	Problems []string
	Reason   string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%v: %v", ErrValidation, e.Problems)
}
func (e *ValidationError) Unwrap() error { return ErrValidation }

// Details derives the structured siblings of the problem strings, each split
// on its first ": ". A string without the separator keeps its whole text as
// the message and an empty path, so a malformed problem never drops silently.
// The API's 422 and `substratectl validate` both carry what it returns, so
// the two print the same lines.
func (e *ValidationError) Details() []ProblemDetail {
	if len(e.Problems) == 0 {
		return nil
	}
	out := make([]ProblemDetail, len(e.Problems))
	for i, p := range e.Problems {
		if path, msg, ok := strings.Cut(p, ": "); ok {
			out[i] = ProblemDetail{Path: path, Message: msg}
		} else {
			out[i] = ProblemDetail{Message: p}
		}
	}
	return out
}

// AcceptConflictError is a FAILED accept of a change request: the decision
// transition rolled back because the change it would perform does not apply to
// the target as it stands — the diff is a no-op against the stored values, the
// target moved or vanished, a guard or the accepting actor's emit ceiling
// refused it, the merge no longer holds. The request stays `proposed` and
// carries Reason as its conflict annotation.
//
// It matches ErrConflict, because the decision conflicts with the state of the
// target, and it deliberately has NO Unwrap: the inner refusal is the accept's
// reason, not the caller's own error, so the wire answers one 409 whatever the
// cause instead of a 422 wrapped in a version conflict the caller's ifVersion
// never failed (#553). Reason therefore carries no sentinel prefix of its own.
type AcceptConflictError struct {
	Reason string
}

func (e *AcceptConflictError) Error() string {
	return "substrate: the accepted diff did not apply: " + e.Reason
}

func (e *AcceptConflictError) Is(target error) bool { return target == ErrConflict }

// VersionConflictError is a FAILED compare-and-set: the write carried an
// IfVersion precondition and the stored record does not sit at it (a
// non-existent record reads as version 0). It matches ErrConflict, so every
// caller that only knows the sentinel refuses it unchanged and the API still
// answers one 409 with the same words.
//
// It is a typed error rather than a wrapped sentinel because a CAS mismatch is
// the one conflict a caller may want to tell apart from every other reason a
// write conflicts (a former id, a failed accept, a lost cursor swap): a
// function's effect may declare that losing the race is a NORMAL outcome, and
// the engine can only honor that if it can see which conflict it is holding
// (decision 0093).
type VersionConflictError struct {
	// Want is the precondition the writer asserted; Have is the version the
	// record actually sits at.
	Want, Have int64
}

func (e *VersionConflictError) Error() string {
	return fmt.Sprintf("%v: ifVersion %d, stored %d", ErrConflict, e.Want, e.Have)
}

func (e *VersionConflictError) Is(target error) bool { return target == ErrConflict }
