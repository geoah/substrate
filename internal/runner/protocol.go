package runner

import (
	"context"
	"errors"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The invoke protocol (version 4), pinned so a later transport swap (Connect
// Describe/Invoke on a local socket) is mechanical — the frames below map
// 1:1 onto RPCs and the host-call frames onto a bidirectional stream.
//
// Every parent→child request carries a monotonically increasing `reqId`;
// every child→parent frame carries an explicit `kind` and echoes the request
// id it belongs to. Anything else on the protocol stream — a missing kind, a
// stale or mismatched reqId, an undecodable line — is a desync: the parent
// KILLS the child (its state is unknowable) and the delivery rides the
// dispatcher's ordinary retry-then-park.
//
//	parent → child   {"op": "register", "reqId": N, "id": <key>, "source": <body>}   (python host only)
//	                 {"op": "deregister", "reqId": N, "id": <key>}                   (python host only)
//	                 {"op": "describe", "reqId": N}
//	                 {"op": "invoke", "reqId": N, "id": <key>, "input": Input}
//	child → parent   {"kind": "response", "reqId": N, "ok": true, "output": ..., "effects": [...], "more": {"cursor": ...}?, "logs": [...]}
//	                 {"kind": "response", "reqId": N, "ok": false, "error": "...", "logs": [...]}
//	                 {"kind": "response", "reqId": N, "ok": true, "functions": [...], "protocol": 4}   (describe)
//
// A response's optional `more` is the PAGED-CHECKPOINT continuation: it means
// "this page is done — commit its effects, then re-invoke me with this
// cursor". The host commits the page's effects and the opaque `more.cursor`
// together, then re-invokes the SAME body OFF THE CAUSAL CHAIN (the same
// causalDepth, no self-emit) with the cursor echoed back on the next
// invoke's `resume` field, draining until a response omits `more`. Backfill
// paging without spending the causal-depth budget; incremental deliveries
// (no `more`) are untouched.
//
// While an invoke is in flight the child may interleave HOST CALLS — the
// capability-scoped read API plus `call`, the function-to-function
// invocation — each carrying the invoke's reqId and answered by the
// parent's next line:
//
//	child → parent   {"kind": "call", "reqId": N, "host": "get"|"list"|"search", "params": {...}}
//	child → parent   {"kind": "call", "reqId": N, "host": "call", "params": {"function": "<identity>", "input": ...}}
//	parent → child   {"kind": "reply", "reqId": N, "ok": true, "result": {...}} / {"kind": "reply", "reqId": N, "ok": false, "error": "..."}
//
// A `call` runs the target function to completion INSIDE the caller's
// invocation: the parent gates it on `permissions.call`, charges the
// caller's call budget, runs the target body (its effects accumulate into
// the CALLER's delivery transaction) and replies with the target's output.
//
// One frame per line, JSON. The protocol stream is the child's ORIGINAL
// stdout, which both hosts detach from user code before any body runs: the
// python host dups fd 1 for itself and rebinds sys.stdout into the
// invocation's capped logs (and sys.stdin to /dev/null), the Go SDK grabs
// os.Stdout for its encoder and rebinds the os.Stdout variable to stderr. A
// body's print therefore lands in logs, never on the wire. Child stderr is
// captured by the parent into a capped ring buffer surfaced on failures.
// Children cap their response frames and changelog lines below the parent's
// scanner ceiling; a frame over the ceiling is a scanner error that kills the
// child rather than wedging it.

// ProtocolVersion pins the wire contract above. It participates in the Go
// build cache key, so a binary compiled against an older protocol is rebuilt
// instead of desyncing; the describe response carries it for verification.
const ProtocolVersion = 4

// The invocation modes.
const (
	// ModeTrigger is a dispatched record delivery (moves the trigger cursor).
	ModeTrigger = "trigger"
	// ModeSchedule is a due RRULE fire (advances the trigger's fire state,
	// no changelog row underneath).
	ModeSchedule = "schedule"
	// ModeWebhook is a webhook trigger's wake (mints nothing durable but a
	// run row).
	ModeWebhook = "webhook"
	// ModeManual is an owner's run or a parked retry (no cursor motion).
	ModeManual = "manual"
	// ModeCall is a direct invocation with arbitrary input — the HTTP call
	// API or another function's host Call. No cursor, no envelope.
	ModeCall = "call"
)

// Input is one invocation, exactly what the runtime contract pins: mode, the
// level-triggered envelope (or the call args), config (nil for now), the
// read budgets, the causal depth and the idempotency key.
type Input struct {
	Mode string `json:"mode"`
	// Envelope carries change/record/repository — the same three bindings the
	// trigger's `when:` guard evaluates (Envelope below builds it). Schedule
	// and webhook deliveries carry fire/repository instead; call mode carries no
	// envelope at all.
	Envelope map[string]any `json:"envelope,omitempty"`
	// Args is call mode's arbitrary input, validated against the manifest's
	// `input:` schema when one is declared.
	Args any `json:"args,omitempty"`
	// Config is the callable's resolved configuration, as the engine resolves
	// it from the bundle's injected inputs and account rows
	// (engine/invocationconfig.go); nil for a callable outside any bundle.
	Config map[string]any `json:"config,omitempty"`
	// Resume is the PAGED-CHECKPOINT cursor: the opaque `more.cursor` the
	// previous page of THIS invocation chain returned, handed straight back so
	// the body picks up where it left off. Nil on a fresh delivery and on
	// every non-paged one — a body that never returns `more` never sees it.
	Resume any `json:"resume,omitempty"`
	// Budgets mirrors the manifest's read budgets, informational — the host
	// enforces them per call.
	Budgets *Budgets `json:"budgets,omitempty"`
	// CausalDepth is how many caused_by hops sit under the triggering
	// change, sub-calls included — each host Call hands the callee its
	// caller's depth plus one; the cap is the dispatcher's.
	CausalDepth int `json:"causalDepth"`
	// CallDepth is how many host Calls deep this invocation sits: 0 for a
	// delivery or a top-level call, +1 per nested Call. It also selects the
	// python host slot, so a nested python body never waits on the process
	// its caller is blocking.
	CallDepth int `json:"callDepth,omitempty"`
	// IdempotencyKey identifies the delivery ("<repository>/<trigger>/<seq or
	// fire id>", repository-qualified because per-repository changelog sequences
	// collide): a body that talks to the outside world can dedupe on it.
	IdempotencyKey string `json:"idempotencyKey"`
}

// Budgets is the per-invocation read budget.
type Budgets struct {
	Calls int `json:"calls"`
	Rows  int `json:"rows"`
}

// Result is a completed invocation: the output value, the raw effect values
// (the engine decodes them against the capability envelope), the body's changelog
// lines and — for a paged body — the continuation.
type Result struct {
	Output  any
	Effects []any
	Logs    []string
	// More, non-nil, is the paged-checkpoint continuation: this page is done
	// but the body has more work. The host commits this page's effects and
	// More.Cursor together, then re-invokes the body OFF THE CAUSAL CHAIN with
	// Resume set to More.Cursor. Nil means drained — the ordinary single-shot
	// completion.
	More *Continuation
}

// Continuation is a paged body's "not done yet": an opaque resume cursor the
// host persists and hands back verbatim. The host never interprets it — a
// seq, a page token, a provider cursor, whatever the body needs to resume.
type Continuation struct {
	Cursor any `json:"cursor,omitempty"`
}

// Backend is where the host calls land. Get addresses one record by its
// FULL identity — the (type, id) pair; a bare id names nothing — and returns
// nil (no error) when no record of that type has the id: absence is a normal
// answer. Call runs another function to completion under the CALLER's
// delivery (the engine gates depth, recursion and input shape; the runner
// gates the allowlist and budget) and returns its output.
// ResolveKind turns a kind REFERENCE — bare (`task`) or authority-qualified
// (`tasks.substrate.reamde.dev/task`) — into the identity the store and the reads
// allowlist are written in. It is the one thing the runner cannot answer for
// itself: the registry lives on the other side of this interface, and without
// it a reads capability declared in one spelling would refuse a body that asks
// in the other. An unknown name comes back unchanged, so it fails the
// allowlist exactly as an undeclared kind does.
type Backend interface {
	Get(ctx context.Context, typ, id string) (*substrate.Record, error)
	List(ctx context.Context, q substrate.Query) (*substrate.Page, error)
	Search(ctx context.Context, in substrate.SearchInput) ([]substrate.Hit, error)
	Call(ctx context.Context, function string, args any) (any, error)
	ResolveKind(name string) string
}

// The deterministic trip reasons: retrying reproduces them, so the
// dispatcher parks immediately instead of burning attempts.
var (
	ErrReadForbidden = errors.New("read outside the reads allowlist")
	ErrReadBudget    = errors.New("read budget exhausted")
	ErrCallForbidden = errors.New("call outside the call allowlist")
)

// Deterministic reports whether the error is a trip a retry would reproduce.
func Deterministic(err error) bool {
	return errors.Is(err, ErrReadForbidden) || errors.Is(err, ErrReadBudget) ||
		errors.Is(err, ErrCallForbidden)
}

// Envelope assembles the level-triggered envelope for one delivery: the
// change (op, changed property names), the record's CURRENT state — nil
// after a delete — and the repository. Only the record's trimmed shape
// crosses: id, kind reference, properties and shallow edges. The `when:` guard
// binds this map directly; the runner marshals it to the body.
func Envelope(ch substrate.Change, e *substrate.Record, repositoryOwner string) map[string]any {
	change := map[string]any{
		"seq":   ch.Seq,
		"op":    OpOf(ch),
		"kind":  ch.Kind,
		"id":    ch.RecordID,
		"actor": string(ch.Actor),
	}
	// The changed-property names, when the payload carries them: the
	// level-triggered contract names what moved, never the old values.
	if names, ok := ch.Payload["properties"].([]any); ok && len(names) > 0 {
		change["changed"] = names
	}
	envelope := map[string]any{
		"change":     change,
		"record":     nil,
		"repository": map[string]any{"owner": repositoryOwner},
	}
	if e == nil {
		return envelope
	}
	edges := map[string]any{}
	for rel, targets := range e.Edges {
		list := make([]any, 0, len(targets))
		for _, tgt := range targets {
			list = append(list, map[string]any{
				"id": tgt.ID, "kind": tgt.Kind, "title": tgt.Title,
			})
		}
		edges[rel] = list
	}
	envelope["record"] = map[string]any{
		"id":         e.ID,
		"kind":       e.Kind,
		"properties": e.Properties,
		"edges":      edges,
	}
	return envelope
}

// FireEnvelope assembles the envelope for a schedule or webhook delivery:
// no change, no record — just the fire's identity and the repository.
func FireEnvelope(fireID string, at time.Time, repositoryOwner string) map[string]any {
	return map[string]any{
		"fire": map[string]any{
			"id": fireID,
			"at": at.UTC().Format(time.RFC3339),
		},
		"repository": map[string]any{"owner": repositoryOwner},
	}
}

// OpOf maps a changelog row onto the create/update/delete vocabulary
// trigger sources declare: a put whose payload says `created` is a create, delete
// and gc are deletes, everything else — patch, link, unlink, merge, split,
// a restoring put — is an update.
func OpOf(ch substrate.Change) string {
	switch ch.Op {
	case substrate.OpPut:
		if created, _ := ch.Payload["created"].(bool); created {
			return vocabulary.FunctionOpCreate
		}
		return vocabulary.FunctionOpUpdate
	case substrate.OpDelete, substrate.OpGC:
		return vocabulary.FunctionOpDelete
	default:
		return vocabulary.FunctionOpUpdate
	}
}

// SplitIdentity splits a kind reference into its authority and local name.
func SplitIdentity(ident string) (local, authority string) {
	authority, local = vocabulary.SplitKindRef(ident)
	return local, authority
}
