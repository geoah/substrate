package substrate

import (
	"errors"
	"fmt"
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
	// ErrStaleHistory is a list cursor minted under another history
	// generation: a restore replaced the changelog since, so the positions
	// the cursor carries mean nothing here. The client lists again. It wraps
	// ErrValidation so a caller that only knows the older sentinel still
	// refuses it; the API answers it as the changelog's own 410.
	ErrStaleHistory = fmt.Errorf("%w: cursor from another history", ErrValidation)
	ErrForbidden    = errors.New("substrate: forbidden") // e.g. foreign label namespace, system type write
	// ErrGated marks a write a policy HELD rather than refused: it converted
	// into a recordpatchrequest awaiting review, and the message names it. The
	// policy door runs only inside the agent loop, where the hold becomes a
	// tool result the model reads, so this sentinel never leaves the loop and
	// is not a wire error code (0037).
	ErrGated = errors.New("substrate: held for review")
	ErrAuth  = errors.New("substrate: authentication failed")
	// ErrFunctionFault marks a callable that failed WHILE ITS BODY RAN — a
	// raise, a bad effect, a failed sub-call. It is distinct from ErrValidation,
	// which is the caller's arguments failing the declared input schema before
	// the body runs. A body fault is a server-side execution fault (500
	// function_failed), never invalid caller input (422).
	ErrFunctionFault = errors.New("substrate: function body failed")
	// ErrUnavailable marks an answer the substrate cannot give YET: the state
	// it needs is still being built, and the same call succeeds later without
	// the caller changing anything. Semantic search returns it while the
	// resolved embeddings pair has no vectors and the queue holds work (a
	// repository restored from its directory, a re-embed, before the drain
	// has bought them), so "no vectors yet" never reads as "no matches". It is
	// distinct from ErrValidation, which needs the caller to change something.
	ErrUnavailable = errors.New("substrate: not available yet")
)

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
type ErrorPayload struct {
	Code           string          `json:"code"`
	Message        string          `json:"message"`
	Problems       []string        `json:"problems,omitempty"`
	ProblemDetails []ProblemDetail `json:"problemDetails,omitempty"`
	Head           *int64          `json:"head,omitempty"`
	Generation     string          `json:"generation,omitempty"`
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

// ValidationError carries per-field detail for the UI's YAML editor.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%v: %v", ErrValidation, e.Problems)
}
func (e *ValidationError) Unwrap() error { return ErrValidation }

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
