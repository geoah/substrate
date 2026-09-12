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
