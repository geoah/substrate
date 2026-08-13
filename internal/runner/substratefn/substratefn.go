// Package substratefn is the Go SDK an inline `runtime: go` function body is
// compiled against. The runner (functions/runner) wraps the manifest's
// source into a `body` package beside a copy of this file, generates a main
// that calls Serve(body.Main), and builds the module with the standard
// toolchain — so this package must stay STDLIB-ONLY: it is embedded verbatim
// into a module with no other dependencies.
//
// The compiled binary is a child process speaking the runner's JSON-lines
// protocol, version 4, on stdio (one frame per line; see functions/runner's
// protocol.go for the full contract): the parent sends `describe` and
// `invoke` frames, each carrying a `reqId` the child echoes on every frame
// it emits, alongside an explicit frame `kind`. During an invoke the body
// may issue host calls (Get/List/Search/Call), each a `{"kind": "call"}`
// frame answered by the next stdin line's `{"kind": "reply"}`; the final
// `{"kind": "response"}` frame carries output, effects and logs. Serve
// detaches the protocol stream before the body runs: os.Stdout is rebound to
// stderr, so a body's fmt.Println lands in the parent's capped changelog buffer,
// never on the wire.
package substratefn

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// The wire caps, kept below the parent's 16 MiB scanner ceiling.
const (
	maxFrameBytes = 8 << 20
	maxLogLines   = 200
	maxLogChars   = 4096
)

// Input is one invocation: the level-triggered envelope (or the call args)
// plus the delivery bookkeeping the runtime contract pins.
type Input struct {
	// Mode is trigger (a dispatched record delivery), schedule (a due RRULE
	// fire), webhook (a webhook trigger's wake), manual (an owner's run or a
	// parked retry — no cursor motion) or call (a direct invocation with
	// arbitrary Args).
	Mode     string   `json:"mode"`
	Envelope Envelope `json:"envelope"`
	// Args is call mode's input, validated against the manifest's `input:`
	// schema when one is declared.
	Args any `json:"args"`
	// Config is the callable's resolved configuration, resolved by the engine
	// from the bundle's config and account rows; nil for a callable whose
	// bundle declares none.
	Config map[string]any `json:"config"`
	// Resume is the paged-checkpoint cursor: the opaque value the previous
	// page returned as Result.More.Cursor, handed back so the body resumes.
	// Nil on a fresh delivery and on every non-paged one.
	Resume any `json:"resume"`
	// Budgets mirrors the manifest's read budget, informational: the host
	// enforces it per call.
	Budgets     *Budgets `json:"budgets"`
	CausalDepth int      `json:"causalDepth"`
	// CallDepth is how many host Calls deep this invocation sits.
	CallDepth int `json:"callDepth"`
	// IdempotencyKey identifies the delivery ("<repository>/<trigger>/<seq or
	// fire id>").
	IdempotencyKey string `json:"idempotencyKey"`
}

// Envelope is level-triggered: the operation, the changed property names,
// and the record's CURRENT state — no previous values. Schedule and webhook
// deliveries carry Fire instead of Change/Record; call mode carries none.
type Envelope struct {
	Change Change `json:"change"`
	// Record is the triggering record's current state; nil after a delete.
	Record *Record `json:"record"`
	// Fire identifies a schedule or webhook delivery.
	Fire       *Fire      `json:"fire"`
	Repository Repository `json:"repository"`
}

// Fire is one schedule occurrence or webhook wake: a stable id (missed
// schedule ticks coalesce to one fire) and the occurrence instant.
type Fire struct {
	ID string `json:"id"`
	At string `json:"at"`
}

// Change is the triggering changelog row, a hint: the function computes
// against Record, not against what the row said.
type Change struct {
	Seq int64  `json:"seq"`
	Op  string `json:"op"` // create | update | delete
	// Kind is the record's kind REFERENCE: "tasks.substrate.geoah.me/task", or a bare
	// "task" for a repository-local kind.
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Actor string `json:"actor"`
	// Changed names the properties the change touched, when the changelog
	// payload carried them; values are never included.
	Changed []string `json:"changed,omitempty"`
}

// Record is the bound record: the trimmed shape the envelope carries, not
// the full wire record (host reads return that one).
type Record struct {
	ID         string                  `json:"id"`
	Kind       string                  `json:"kind"`
	Properties map[string]any          `json:"properties"`
	Edges      map[string][]EdgeTarget `json:"edges"`
}

// EdgeTarget is one edge destination as the envelope carries it.
type EdgeTarget struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
}

// Repository carries the repository's owner, for "assigned to me" guards.
type Repository struct {
	Owner string `json:"owner"`
}

// Budgets mirrors the manifest's `capabilities.reads.budgets`.
type Budgets struct {
	Calls int `json:"calls"`
	Rows  int `json:"rows"`
}

// Effect is one returned consequence. The seven actions are put, patch,
// delete, link, unlink, merge and split; the host holds every effect to the
// manifest's emit allowlist, and merge/split to its mutations grant. The id
// is required on every action but split: functions are writers, and writers
// control the ids of what they write — that is what makes replays and
// retries idempotent by construction.
type Effect struct {
	Action string `json:"action"`
	Kind   string `json:"kind"` // the kind reference, held to emit
	ID     string `json:"id,omitempty"`
	// IfAbsent makes a put create-only: an existing record is a no-op, so a
	// minting function never resets state owned by later stages.
	IfAbsent bool `json:"ifAbsent,omitempty"`
	// IfVersion is the optimistic-concurrency precondition on a put or patch:
	// the write applies only if the addressed record's stored version equals
	// it (a non-existent record is version 0), else the delivery fails a
	// conflict. The safe read-then-conditional-write primitive.
	IfVersion  *int64         `json:"ifVersion,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
	// Edges only apply on put: rel → an id, a {authority, type, id} reference,
	// or a list of either.
	Edges map[string]any `json:"edges,omitempty"`
	// Rel and To shape link/unlink; ID is the source record.
	Rel string `json:"rel,omitempty"`
	To  any    `json:"to,omitempty"`
	// Loser rides a merge (ID is the winner); Merge rides a split (the merge
	// record's id).
	Loser string `json:"loser,omitempty"`
	Merge string `json:"merge,omitempty"`
}

// Version wraps an int64 as an *int64 for the IfVersion precondition on a put
// or patch effect — `IfVersion: substratefn.Version(e.Version)`.
func Version(n int64) *int64 { return &n }

// Result is what a body returns: effects the host applies transactionally,
// and an output value for the caller (a trigger discards it; Call and tool
// invocations read it).
type Result struct {
	Output  any      `json:"output,omitempty"`
	Effects []Effect `json:"effects,omitempty"`
	// More, non-nil, is the paged-checkpoint continuation: this page is done,
	// but there is more work. The host commits this page's effects and
	// More.Cursor together, then re-invokes this body OFF THE CAUSAL CHAIN
	// with Input.Resume set to More.Cursor — paging a backfill without
	// spending the causal-depth budget. Nil means drained.
	More *Continuation `json:"more,omitempty"`
}

// Continuation is a paged body's "not done yet": an opaque resume cursor the
// host persists and hands back verbatim on the next invocation's Resume.
type Continuation struct {
	Cursor any `json:"cursor,omitempty"`
}

// Handler is the function body's entrypoint signature.
type Handler func(in *Input, host *Host) (*Result, error)

// Host is the body's side of the host API: logging plus the capability-scoped
// reads, and the SDK surface (Records/Functions/Effects/IDs/Page) namespaced
// beside them. Writes STAY effects: Effects is a buffered builder the runner
// returns as the delivery's effect list, never a mid-body write.
type Host struct {
	config  map[string]any
	logs    []string
	dropped int
	proto   *protocol
	reqID   uint64
	// sdkErr is the first fatal SDK-validation error any builder, id helper or
	// page continuation recorded — an exceptionless body cannot raise, so the
	// first bad shape is remembered and fails the WHOLE delivery once at return
	// (the Go analog of the Python builder raising). No partial emit.
	sdkErr error

	// Records are the typed, type-scoped, budget-aware reads; Functions is
	// function-to-function composition; Effects is the buffered-effects
	// builder; IDs mints deterministic ids; Page wraps the paged checkpoint.
	Records   *Records
	Functions *Functions
	Effects   *Effects
	IDs       *IDs
	Page      *Page
}

// failSDK records the first fatal SDK-validation error; later ones are dropped
// (the first mistake is the one to report).
func (h *Host) failSDK(err error) {
	if h.sdkErr == nil {
		h.sdkErr = err
	}
}

// initSDK wires the namespaced SDK objects onto a fresh per-invocation host.
func (h *Host) initSDK(in *Input) {
	h.Records = &Records{host: h}
	h.Functions = &Functions{host: h}
	h.Effects = &Effects{host: h}
	h.IDs = &IDs{host: h}
	var resume any
	if in != nil {
		resume = in.Resume
	}
	h.Page = &Page{host: h, resume: resume}
}

// Config returns the invocation's resolved config; nil for now.
func (h *Host) Config() map[string]any { return h.config }

// Log records one line, surfaced on the delivery's response; lines and line
// lengths are capped.
func (h *Host) Logf(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	if len(line) > maxLogChars {
		line = line[:maxLogChars] + "...[truncated]"
	}
	if len(h.logs) >= maxLogLines {
		h.dropped++
		return
	}
	h.logs = append(h.logs, line)
}

// logsOut is the capped changelog list a response carries.
func (h *Host) logsOut() []string {
	if h.dropped > 0 {
		return append(h.logs, fmt.Sprintf("... %d more log lines dropped", h.dropped))
	}
	return h.logs
}

// Get reads one record by its FULL reference — kind + id; a bare id names
// nothing — through the host, gated by the manifest's reads allowlist.
// Absence — or a record outside the allowlist — is (nil, nil), a normal
// answer.
func (h *Host) Get(kind, id string) (map[string]any, error) {
	raw, err := h.call("get", map[string]any{"kind": kind, "id": id})
	if err != nil {
		return nil, err
	}
	var out struct {
		Record map[string]any `json:"record"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("substratefn: decode get result: %w", err)
	}
	return out.Record, nil
}

// List queries records; params is a substrate Query as JSON (filter, orderBy,
// first, after). Returns the page map: {"records": [...], "cursor": ...}.
func (h *Host) List(params map[string]any) (map[string]any, error) {
	raw, err := h.call("list", params)
	if err != nil {
		return nil, err
	}
	var out struct {
		Page map[string]any `json:"page"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("substratefn: decode list result: %w", err)
	}
	return out.Page, nil
}

// Search runs the lexical/semantic search; params is a substrate SearchInput
// as JSON (q, mode, types, k). Returns the hits list.
func (h *Host) Search(params map[string]any) ([]any, error) {
	raw, err := h.call("search", params)
	if err != nil {
		return nil, err
	}
	var out struct {
		Hits []any `json:"hits"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("substratefn: decode search result: %w", err)
	}
	return out.Hits, nil
}

// Call invokes another function by full identity — gated by the manifest's
// `capabilities.call` allowlist and charged against the call budget — and
// returns its output. The callee's effects apply in THIS delivery's
// transaction.
func (h *Host) Call(function string, args any) (any, error) {
	raw, err := h.call("call", map[string]any{"function": function, "input": args})
	if err != nil {
		return nil, err
	}
	var out struct {
		Output any `json:"output"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("substratefn: decode call result: %w", err)
	}
	return out.Output, nil
}

// call issues one host call and returns the raw `result` bytes, so each caller
// decodes into the shape it wants — a raw map (Host.Get) or a typed read with
// an int64 Version (Records.Get). Decoding into `any` up front would collapse
// every number to float64 and defeat the typed CAS idiom.
func (h *Host) call(method string, params map[string]any) (json.RawMessage, error) {
	if err := h.proto.emit(map[string]any{
		"kind": "call", "reqId": h.reqID, "host": method, "params": params,
	}); err != nil {
		return nil, err
	}
	if !h.proto.in.Scan() {
		return nil, fmt.Errorf("substratefn: host went away during %s", method)
	}
	var reply struct {
		Kind   string          `json:"kind"`
		ReqID  uint64          `json:"reqId"`
		OK     bool            `json:"ok"`
		Error  string          `json:"error"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(h.proto.in.Bytes(), &reply); err != nil {
		return nil, fmt.Errorf("substratefn: decode %s reply: %w", method, err)
	}
	if reply.Kind != "reply" || reply.ReqID != h.reqID {
		// The parent kills a desynchronized child; failing the body is all
		// that is left to do from this side.
		return nil, fmt.Errorf("substratefn: protocol desync during %s", method)
	}
	if !reply.OK {
		return nil, fmt.Errorf("%s", reply.Error)
	}
	return reply.Result, nil
}

// --- the SDK: typed reads, composition, buffered effects, ids, paging -------

// Records are typed, kind-scoped, budget-aware reads over the host's read
// calls. A forbidden type or an exhausted budget surfaces as an error carrying
// the engine's reason. Reads see COMMITTED state — never this delivery's
// staged effects (the engine's computed view would make a local overlay lie).
type Records struct{ host *Host }

// ReadRecord is a TYPED record read: unlike the raw map Host.Get returns (whose
// numbers decode as float64), Version is a real int64, so the CAS idiom
// `IfVersion: substratefn.Version(e.Version)` is writable off a read. Reads see
// COMMITTED state — never this delivery's staged effects.
type ReadRecord struct {
	ID          string                  `json:"id"`
	Kind        string                  `json:"kind"`
	CanonicalID string                  `json:"canonicalId,omitempty"`
	Version     int64                   `json:"version"`
	Properties  map[string]any          `json:"properties"`
	Labels      map[string]any          `json:"labels,omitempty"`
	Edges       map[string][]EdgeTarget `json:"edges,omitempty"`
}

// ReadPage is a typed list page.
type ReadPage struct {
	Records []ReadRecord `json:"records"`
	Cursor  string       `json:"cursor,omitempty"`
	Total   int          `json:"total,omitempty"`
}

// ReadHit is a typed search hit.
type ReadHit struct {
	Record   *ReadRecord `json:"record"`
	Lexical  float64     `json:"lexical,omitempty"`
	Semantic float64     `json:"semantic,omitempty"`
}

// Get reads one record by its full (kind, id) identity, typed; (nil, nil)
// for an absent or out-of-allowlist id. The int64 Version feeds a guarded
// write.
func (e *Records) Get(kind, id string) (*ReadRecord, error) {
	raw, err := e.host.call("get", map[string]any{"kind": kind, "id": id})
	if err != nil {
		return nil, err
	}
	var out struct {
		Record *ReadRecord `json:"record"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("substratefn: decode get result: %w", err)
	}
	return out.Record, nil
}

// GetRaw is the untyped escape hatch: the record as a map (numbers as float64).
func (e *Records) GetRaw(kind, id string) (map[string]any, error) { return e.host.Get(kind, id) }

// ListQuery is a kind-scoped list; Kinds is required (an unscoped read trips).
type ListQuery struct {
	Kinds     []string
	Where     map[string]any // property conditions, e.g. {"account": {"eq": id}}
	First     int
	After     string
	OrderBy   []map[string]any
	WithEdges bool
}

func (q ListQuery) params() map[string]any {
	filter := map[string]any{"kinds": q.Kinds}
	if len(q.Where) > 0 {
		filter["properties"] = q.Where
	}
	params := map[string]any{"filter": filter}
	if q.First > 0 {
		params["first"] = q.First
	}
	if q.After != "" {
		params["after"] = q.After
	}
	if len(q.OrderBy) > 0 {
		params["orderBy"] = q.OrderBy
	}
	if q.WithEdges {
		params["withEdges"] = true
	}
	return params
}

// List queries records and returns a typed page.
func (e *Records) List(q ListQuery) (*ReadPage, error) {
	raw, err := e.host.call("list", q.params())
	if err != nil {
		return nil, err
	}
	var out struct {
		Page *ReadPage `json:"page"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("substratefn: decode list result: %w", err)
	}
	if out.Page == nil {
		out.Page = &ReadPage{}
	}
	return out.Page, nil
}

// ListRaw is the untyped escape hatch: the page as a map.
func (e *Records) ListRaw(q ListQuery) (map[string]any, error) { return e.host.List(q.params()) }

// SearchQuery is a kind-scoped lexical/semantic search; Kinds is required.
type SearchQuery struct {
	Q     string
	Kinds []string
	K     int
	Mode  string
}

func (s SearchQuery) params() map[string]any {
	params := map[string]any{"q": s.Q, "kinds": s.Kinds}
	if s.K > 0 {
		params["k"] = s.K
	}
	if s.Mode != "" {
		params["mode"] = s.Mode
	}
	return params
}

// Search returns typed hits.
func (e *Records) Search(s SearchQuery) ([]ReadHit, error) {
	raw, err := e.host.call("search", s.params())
	if err != nil {
		return nil, err
	}
	var out struct {
		Hits []ReadHit `json:"hits"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("substratefn: decode search result: %w", err)
	}
	return out.Hits, nil
}

// SearchRaw is the untyped escape hatch: the hits as a slice of any.
func (e *Records) SearchRaw(s SearchQuery) ([]any, error) { return e.host.Search(s.params()) }

// Functions is function-to-function composition, gated by capabilities.call.
// The callee's effects accumulate into THIS delivery's transaction.
type Functions struct{ host *Host }

// Call invokes another function by full identity and returns its output.
func (f *Functions) Call(function string, args any) (any, error) {
	return f.host.Call(function, args)
}

// Staged is a handle to one buffered effect — NOT a record, and NEVER a return
// value (a body returns Result.Effects OR stages, not both). Reads never
// reflect it; the engine applies the whole buffer atomically after the body
// returns.
type Staged struct{ action, kind, id string }

func (s *Staged) Action() string { return s.action }
func (s *Staged) Kind() string   { return s.kind }
func (s *Staged) ID() string     { return s.id }

// The shared id/identifier alphabets — mirrors of the engine's schema/naming.go
// (and byte-identical to host.py's), so the SDK rejects locally exactly what
// the engine rejects at admission.
const maxIDLen = 128

var (
	reID    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~:@/-]*$`)
	reKind  = regexp.MustCompile(`^(?:[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+/)?[a-z][a-z0-9]*$`)
	reIdent = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)
)

// Effects is the buffered-effects builder. Each method APPENDS a staged effect
// to a write-only buffer and returns a Staged handle (never a record); the
// runner returns the buffer as the delivery's effects. ONE mode per invocation:
// a body that also sets Result.Effects while the buffer is non-empty is refused
// at return (the two apply orders are unrelated and can self-conflict under
// CAS). There is no flush — the buffer IS the return. Shapes are validated here
// (a known action, a well-formed id/kind/rel/edge, a non-negative IfVersion)
// and caller maps are SNAPSHOT-copied through JSON, so a mistake is a clear
// body error and a reused map is not aliased; the first error fails the whole
// delivery. The engine stays authoritative for the emit ceiling and kind
// admission.
type Effects struct {
	host   *Host
	staged []Effect
}

func (e *Effects) fail(err error) *Staged {
	e.host.failSDK(err)
	return &Staged{}
}

func (e *Effects) add(ef Effect) *Staged {
	e.staged = append(e.staged, ef)
	return &Staged{action: ef.Action, kind: ef.Kind, id: ef.ID}
}

// PutEffect stages a create/upsert. IfAbsent makes it create-only; IfVersion
// makes it apply only against a matching stored version.
type PutEffect struct {
	Kind       string
	ID         string
	Properties map[string]any
	Edges      map[string]any
	IfAbsent   bool
	IfVersion  *int64
}

// Put stages a put effect.
func (e *Effects) Put(p PutEffect) *Staged {
	if err := validKind("put", p.Kind); err != nil {
		return e.fail(err)
	}
	if err := validID("put", "id", p.ID); err != nil {
		return e.fail(err)
	}
	if p.IfVersion != nil {
		if *p.IfVersion < 0 {
			return e.fail(fmt.Errorf("effects.put: ifVersion is a non-negative version, got %d", *p.IfVersion))
		}
		if p.IfAbsent {
			return e.fail(fmt.Errorf("effects.put: IfAbsent and IfVersion cannot combine — IfAbsent makes an existing row a no-op before the version check; pick one"))
		}
	}
	props, err := cloneMap("put", "properties", p.Properties)
	if err != nil {
		return e.fail(err)
	}
	edges, err := cloneEdges("put", p.Edges)
	if err != nil {
		return e.fail(err)
	}
	return e.add(Effect{
		Action: "put", Kind: p.Kind, ID: p.ID, Properties: props,
		Edges: edges, IfAbsent: p.IfAbsent, IfVersion: p.IfVersion,
	})
}

// PatchEffect stages an in-place mutation. IfVersion is the optimistic
// precondition.
type PatchEffect struct {
	Kind       string
	ID         string
	Properties map[string]any
	IfVersion  *int64
}

// Patch stages a patch effect.
func (e *Effects) Patch(p PatchEffect) *Staged {
	if err := validKind("patch", p.Kind); err != nil {
		return e.fail(err)
	}
	if err := validID("patch", "id", p.ID); err != nil {
		return e.fail(err)
	}
	if p.IfVersion != nil && *p.IfVersion < 0 {
		return e.fail(fmt.Errorf("effects.patch: ifVersion is a non-negative version, got %d", *p.IfVersion))
	}
	props, err := cloneMap("patch", "properties", p.Properties)
	if err != nil {
		return e.fail(err)
	}
	return e.add(Effect{
		Action: "patch", Kind: p.Kind, ID: p.ID, Properties: props,
		IfVersion: p.IfVersion,
	})
}

// Delete stages a tombstone.
func (e *Effects) Delete(kind, id string) *Staged {
	if err := validKind("delete", kind); err != nil {
		return e.fail(err)
	}
	if err := validID("delete", "id", id); err != nil {
		return e.fail(err)
	}
	return e.add(Effect{Action: "delete", Kind: kind, ID: id})
}

// LinkEffect stages an edge write; To is a bare id string or a
// map[string]any{authority, type, id} reference. ID is the source record.
type LinkEffect struct {
	Kind       string
	ID         string
	Rel        string
	To         any
	Properties map[string]any
}

// Link stages a link effect.
func (e *Effects) Link(l LinkEffect) *Staged {
	if err := validKind("link", l.Kind); err != nil {
		return e.fail(err)
	}
	if err := validID("link", "id", l.ID); err != nil {
		return e.fail(err)
	}
	if err := validRel("link", l.Rel); err != nil {
		return e.fail(err)
	}
	if err := validEdgeTarget("to", l.To); err != nil {
		return e.fail(fmt.Errorf("effects.link: %w", err))
	}
	to, err := jsonClone("link", "to", l.To)
	if err != nil {
		return e.fail(err)
	}
	props, err := cloneMap("link", "properties", l.Properties)
	if err != nil {
		return e.fail(err)
	}
	return e.add(Effect{Action: "link", Kind: l.Kind, ID: l.ID, Rel: l.Rel, To: to, Properties: props})
}

// UnlinkEffect stages an edge removal.
type UnlinkEffect struct {
	Kind string
	ID   string
	Rel  string
	To   any
}

// Unlink stages an unlink effect.
func (e *Effects) Unlink(u UnlinkEffect) *Staged {
	if err := validKind("unlink", u.Kind); err != nil {
		return e.fail(err)
	}
	if err := validID("unlink", "id", u.ID); err != nil {
		return e.fail(err)
	}
	if err := validRel("unlink", u.Rel); err != nil {
		return e.fail(err)
	}
	if err := validEdgeTarget("to", u.To); err != nil {
		return e.fail(fmt.Errorf("effects.unlink: %w", err))
	}
	to, err := jsonClone("unlink", "to", u.To)
	if err != nil {
		return e.fail(err)
	}
	return e.add(Effect{Action: "unlink", Kind: u.Kind, ID: u.ID, Rel: u.Rel, To: to})
}

// Merge stages a merge (id is the winner, loser is folded into it); needs the
// capabilities.mutations grant, enforced by the engine.
func (e *Effects) Merge(kind, id, loser string) *Staged {
	if err := validKind("merge", kind); err != nil {
		return e.fail(err)
	}
	if err := validID("merge", "id", id); err != nil {
		return e.fail(err)
	}
	if err := validID("merge", "loser", loser); err != nil {
		return e.fail(err)
	}
	if id == loser {
		return e.fail(fmt.Errorf("effects.merge: winner and loser are the same id %q", id))
	}
	return e.add(Effect{Action: "merge", Kind: kind, ID: id, Loser: loser})
}

// Split stages a split of a merge record (its id); needs the mutations grant.
func (e *Effects) Split(kind, mergeID string) *Staged {
	if err := validKind("split", kind); err != nil {
		return e.fail(err)
	}
	if err := validID("split", "merge", mergeID); err != nil {
		return e.fail(err)
	}
	return e.add(Effect{Action: "split", Kind: kind, Merge: mergeID})
}

// --- shared builder validation + JSON snapshotting --------------------------

func validKind(action, s string) error {
	if s == "" {
		return fmt.Errorf("effects.%s: kind is required", action)
	}
	if !reKind.MatchString(s) {
		return fmt.Errorf("effects.%s: %q is not a kind reference (<authority>/<name>)", action, s)
	}
	return nil
}

func validID(action, field, s string) error {
	if s == "" {
		return fmt.Errorf("effects.%s: %s is required", action, field)
	}
	if len(s) > maxIDLen || !reID.MatchString(s) {
		return fmt.Errorf("effects.%s: %s %q is not a record id (URL-path-safe, at most %d characters)", action, field, s, maxIDLen)
	}
	return nil
}

func validRel(action, s string) error {
	if s == "" {
		return fmt.Errorf("effects.%s: rel is required", action)
	}
	if !reIdent.MatchString(s) {
		return fmt.Errorf("effects.%s: %q is not a relation name", action, s)
	}
	return nil
}

// validEdgeTarget accepts a bare non-empty id string or a full {authority, type,
// id} reference (all three required — the partial ref the engine would later
// reject on a polymorphic edge is caught here).
func validEdgeTarget(label string, to any) error {
	switch v := to.(type) {
	case string:
		if v == "" {
			return fmt.Errorf("%s id is empty", label)
		}
		return nil
	case map[string]any:
		for _, k := range []string{"kind", "id"} {
			s, _ := v[k].(string)
			if s == "" {
				return fmt.Errorf("%s reference needs a kind and an id", label)
			}
		}
		return nil
	default:
		return fmt.Errorf("%s is an id or a {kind, id} reference", label)
	}
}

// cloneMap deep-copies a caller map through JSON (a snapshot, so a body reusing
// one map across a loop stages each call's value), rejecting anything not
// JSON-serializable at the offending builder call.
func cloneMap(action, field string, m map[string]any) (map[string]any, error) {
	if m == nil {
		return nil, nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("effects.%s: %s is not JSON-serializable: %w", action, field, err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("effects.%s: %s: %w", action, field, err)
	}
	return out, nil
}

// jsonClone deep-copies an arbitrary caller value through JSON.
func jsonClone(action, field string, v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("effects.%s: %s is not JSON-serializable: %w", action, field, err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("effects.%s: %s: %w", action, field, err)
	}
	return out, nil
}

// cloneEdges validates each put edge (a relation name and a well-formed target,
// recursively) and snapshots the map.
func cloneEdges(action string, edges map[string]any) (map[string]any, error) {
	if edges == nil {
		return nil, nil
	}
	for rel, targets := range edges {
		if err := validRel(action, rel); err != nil {
			return nil, err
		}
		items, ok := targets.([]any)
		if !ok {
			items = []any{targets}
		}
		for _, t := range items {
			if err := validEdgeTarget("edges."+rel, t); err != nil {
				return nil, fmt.Errorf("effects.%s: %w", action, err)
			}
		}
	}
	return cloneMap(action, "edges", edges)
}

// IDs mints deterministic, URL-safe, hash-backed ids. A function composes the
// ids of what it writes; hashing the provider key removes the truncate-a-URL
// collision foot-gun. The algorithm matches the Python SDK byte-for-byte, so
// an id is identical whichever runtime computes it. Every component is a
// required non-empty string; an empty one records a fatal SDK error and returns
// "", which the downstream write then also refuses.
type IDs struct{ host *Host }

// External is a stable id for one external record: provider + account + its id.
func (i *IDs) External(provider, account, externalID string) string {
	if provider == "" || account == "" || externalID == "" {
		i.host.failSDK(fmt.Errorf("ids.External: provider, account and external_id are required non-empty strings"))
		return ""
	}
	return ExternalID(provider, account, externalID)
}

// URL is a stable id for one URL. SAFE V1: the exact URL is hashed with only
// surrounding-ASCII-whitespace trimming — NO canonicalization, so distinct
// spellings (case, default port, trailing slash, query/fragment bytes) are
// DISTINCT ids by design; a lossy normalizer that merged `?next=/` with
// `?next=` would silently drop one page. A structural canonicalizer, when
// needed, arrives as a separate, clearly named helper.
func (i *IDs) URL(rawurl string) string {
	if strings.Trim(rawurl, " \t\n\r\f\v") == "" {
		i.host.failSDK(fmt.Errorf("ids.URL: url is a required non-empty string"))
		return ""
	}
	return URLID(rawurl)
}

// ExternalID and URLID are the PURE id functions — the algorithm the host-bound
// helpers use once their inputs are non-empty, with no host and no error
// recording. Tests and non-body callers use these directly; both are
// byte-identical to the Python SDK.
func ExternalID(provider, account, externalID string) string {
	slug := clip(slugify(provider), 48)
	digest := idHash(provider, account, externalID)
	if slug == "" {
		return digest
	}
	return slug + "-" + digest
}

func URLID(rawurl string) string {
	u := strings.Trim(rawurl, " \t\n\r\f\v")
	slug := clip(slugify("url"), 48)
	digest := idHash("url", "", u)
	if slug == "" {
		return digest
	}
	return slug + "-" + digest
}

// idHash length-prefixes each part so ("ab","c") and ("a","bc") can never
// collide — the foot-gun a truncated URL slug walked straight into.
func idHash(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		b := []byte(p)
		h.Write([]byte(strconv.Itoa(len(b))))
		h.Write([]byte(":"))
		h.Write(b)
		h.Write([]byte("|"))
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// slugify lowercases (ASCII only — no Unicode folding) and keeps ONLY ASCII
// [a-z0-9], collapsing every other run to one dash. ASCII-only so the slug is
// byte-identical to the Python SDK's: a Unicode letter/digit the old
// unicode.IsLetter path kept diverged from Python AND was rejected by the
// engine's ASCII-only id alphabet.
func slugify(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// clip trims to n bytes; the slug is ASCII, so a byte clip is a rune clip and
// stays byte-identical to the Python SDK's code-point clip.
func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// Page is the paged-checkpoint wrapper. Resume is the opaque cursor the
// previous page of this invocation chain returned (nil on a fresh delivery);
// More builds the continuation a paged body sets on Result.More, so bodies
// stop hand-building it.
type Page struct {
	host   *Host
	resume any
}

// Resume returns the incoming paged-checkpoint cursor, nil on a fresh delivery.
func (p *Page) Resume() any { return p.resume }

// More builds the continuation for the next page. A nil cursor is refused — it
// would commit a null cursor and replay page one; return a nil Result.More to
// signal drained instead. The cursor is snapshot-copied through JSON.
func (p *Page) More(cursor any) *Continuation {
	if cursor == nil {
		p.host.failSDK(fmt.Errorf("page.More: cursor is required — a nil continuation commits a null cursor and replays page one; return a nil Result.More to signal drained"))
		return &Continuation{}
	}
	c, err := jsonClone("page.more", "cursor", cursor)
	if err != nil {
		p.host.failSDK(err)
		return &Continuation{}
	}
	return &Continuation{Cursor: c}
}

// protocol owns the detached wire: the ORIGINAL stdout and the frame cap.
type protocol struct {
	out *os.File
	in  *bufio.Scanner
}

// emit writes one frame, capped below the parent's scanner ceiling.
func (p *protocol) emit(v map[string]any) error {
	raw, err := json.Marshal(v)
	if err != nil || len(raw) > maxFrameBytes {
		fallback := map[string]any{
			"kind": v["kind"], "reqId": v["reqId"], "ok": false,
			"error": fmt.Sprintf("substratefn: frame of %d bytes exceeds the %d byte cap", len(raw), maxFrameBytes),
		}
		if raw, err = json.Marshal(fallback); err != nil {
			return err
		}
	}
	_, err = p.out.Write(append(raw, '\n'))
	return err
}

// Serve runs the protocol loop until stdin closes. The generated main calls
// it with the body's Main. The protocol stream detaches first: body prints
// through os.Stdout land on stderr (the parent's capped changelog buffer), never
// on the wire.
func Serve(h Handler) {
	proto := &protocol{out: os.Stdout, in: bufio.NewScanner(os.Stdin)}
	proto.in.Buffer(make([]byte, 0, 64*1024), maxFrameBytes)
	os.Stdout = os.Stderr
	for proto.in.Scan() {
		line := proto.in.Bytes()
		if len(line) == 0 {
			continue
		}
		var req struct {
			Op    string `json:"op"`
			ReqID uint64 `json:"reqId"`
			Input *Input `json:"input"`
		}
		if err := json.Unmarshal(line, &req); err != nil {
			_ = proto.emit(map[string]any{
				"kind": "response", "reqId": req.ReqID,
				"ok": false, "error": "substratefn: decode frame: " + err.Error(),
			})
			continue
		}
		resp := dispatch(h, req.Op, req.ReqID, req.Input, proto)
		resp["kind"] = "response"
		resp["reqId"] = req.ReqID
		_ = proto.emit(resp)
	}
}

func dispatch(h Handler, op string, reqID uint64, in *Input, proto *protocol) map[string]any {
	switch op {
	case "describe":
		return map[string]any{"ok": true, "functions": []string{"main"}, "protocol": 4}
	case "invoke":
		return invoke(h, reqID, in, proto)
	default:
		return map[string]any{"ok": false, "error": "substratefn: unknown op " + op}
	}
}

// invoke runs one delivery, turning a panic into an error frame: a bad body
// parks its delivery, it never kills the runner.
func invoke(h Handler, reqID uint64, in *Input, proto *protocol) (resp map[string]any) {
	if in == nil {
		return map[string]any{"ok": false, "error": "substratefn: invoke carries no input"}
	}
	host := &Host{config: in.Config, proto: proto, reqID: reqID}
	host.initSDK(in)
	defer func() {
		if r := recover(); r != nil {
			resp = map[string]any{"ok": false, "error": fmt.Sprintf("panic: %v", r), "logs": host.logsOut()}
		}
	}()
	res, err := h(in, host)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error(), "logs": host.logsOut()}
	}
	// A builder/id/page shape error fails the whole delivery with a clear
	// message, before the engine — the Go analog of the Python builder
	// raising (it parks once, no partial emit).
	if host.sdkErr != nil {
		return map[string]any{"ok": false, "error": host.sdkErr.Error(), "logs": host.logsOut()}
	}
	if res == nil {
		res = &Result{}
	}
	// The return-path rule: ONE mode per invocation. A body either sets
	// Result.Effects OR stages on host.Effects — never both. The two apply
	// orders are unrelated (returned-first, then staged), so mixing can reverse
	// writes, duplicate them, or self-conflict under CAS. Refuse the ambiguous
	// case; one of the two is always empty here otherwise.
	if len(res.Effects) > 0 && len(host.Effects.staged) > 0 {
		return map[string]any{
			"ok": false,
			"error": fmt.Sprintf("a body sets Result.Effects OR stages on host.Effects — not both (%d returned, %d staged); the two apply orders are unrelated and can self-conflict under CAS",
				len(res.Effects), len(host.Effects.staged)),
			"logs": host.logsOut(),
		}
	}
	effects := make([]any, 0, len(res.Effects)+len(host.Effects.staged))
	for _, ef := range res.Effects {
		effects = append(effects, ef)
	}
	for _, ef := range host.Effects.staged {
		effects = append(effects, ef)
	}
	out := map[string]any{"ok": true, "output": res.Output, "effects": effects, "logs": host.logsOut()}
	// The paged-checkpoint continuation rides the response only when present:
	// a drained (non-paged) body omits it, keeping the frame byte-identical.
	if res.More != nil {
		out["more"] = res.More
	}
	return out
}
