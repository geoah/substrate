package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"

	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The engine's half of the run contract: build the level-triggered envelope,
// evaluate the compiled `when:` guard (CEL — the guard dialect and nothing
// more), hand the body to the shared runner child process, and decode the
// returned effects against the capability envelope (effects.go). A body may
// host-Call other functions: the callee runs to completion inside the
// caller's invocation, its effects accumulate — decoded against the
// CALLEE's envelope — and everything applies together in the CALLER's
// delivery transaction. Nothing here can crash the dispatcher — every error
// is a delivery error the caller retries and parks.

// deliveryEnvelope assembles the envelope for one delivery. The record is
// loaded OUTSIDE any transaction and without the changelog append lock: the
// change row is a hint, current state is what the callable sees.
func (ds *dataset) deliveryEnvelope(ctx context.Context, ch substrate.Change) (map[string]any, error) {
	row, err := ds.loadRowDB(ctx, eref{Kind: ch.Kind, ID: ch.RecordID})
	if err != nil {
		return nil, err
	}
	var e *substrate.Record
	if row != nil && row.DeletedAt == nil {
		if e, err = ds.hydrate(ctx, ds.db, row, true, false); err != nil {
			return nil, err
		}
	}
	return runner.Envelope(ch, e, ds.Repository().Name), nil
}

// loadRowDB reads one record row by its full (type, id) identity, outside
// any transaction.
func (ds *dataset) loadRowDB(ctx context.Context, ref eref) (*erow, error) {
	row, err := scanRecord(ds.db.QueryRowContext(ctx,
		`SELECT `+recordCols+` FROM records WHERE kind = $1 AND id = $2`, ref.Kind, ref.ID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row, nil
}

// evalWhenProgram runs one compiled guard against the envelope's bindings.
//
// A guard that indexes a property not yet on the record — tokenStatus before
// OAuth lands, any writer-gated optional before its writer has run — raises
// CEL's "no such key". That absence is a normal NON-MATCH, not an
// infrastructure failure: the delivery SKIPS (and its cursor advances) instead
// of parking-and-retrying an account that will never satisfy the guard until a
// later write anyway. Authors should still reach for the `"k" in map` idiom
// (it short-circuits and reads cleaner); this keeps a missing guard from
// bricking a pre-write record behind a parked delivery. Every other eval error
// (a real type error, an interrupt, a cost-limit trip) still surfaces.
func evalWhenProgram(ctx context.Context, prog cel.Program, envelope map[string]any) (bool, error) {
	out, _, err := prog.ContextEval(ctx, envelope)
	if err != nil {
		if isMissingKeyErr(err) {
			return false, nil
		}
		return false, fmt.Errorf("when: %w", err)
	}
	b, ok := out.(types.Bool)
	if !ok {
		return false, fmt.Errorf("when: returned %s, not a boolean", out.Type().TypeName())
	}
	return bool(b), nil
}

// isMissingKeyErr reports whether a CEL eval error is the map-index miss on an
// absent key — "no such key: <k>", CEL's stable message for indexing a map
// with a key it does not hold.
func isMissingKeyErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no such key")
}

// runnerSpec flattens one function's body and capabilities for the runner,
// pinned to this repository: live runner state (python namespaces, Go processes)
// is keyed by repository + function identity, never by bare source.
func (ds *dataset) runnerSpec(fn *vocabulary.Function) runner.Spec {
	return ds.runnerSpecIn(fn, ds.registry())
}

// runnerSpecIn is runnerSpec against an EXPLICIT registry — the candidate at
// admission time, where a bundle's shared modules must resolve BEFORE the new
// registry publishes (registration prepares bodies before activation, so the
// live registry does not yet carry the bundle).
func (ds *dataset) runnerSpecIn(fn *vocabulary.Function, reg *vocabulary.Registry) runner.Spec {
	spec := runner.Spec{
		Repository: ds.Repository().Name, Function: fn.Identity(),
		Runtime: fn.Runtime, Source: fn.Source, TimeoutMs: fn.TimeoutMs,
		CallTargets: fn.Caps.Call,
		// The network declaration reaches the runner so the sandbox can
		// enforce its EMPTINESS: a body that declared no egress gets no
		// sockets. Before this it stopped at the manifest.
		Network: fn.Caps.Network,
	}
	if fn.Caps.Reads != nil {
		spec.ReadTypes = fn.Caps.Reads.Kinds
		spec.ReadCalls = fn.Caps.Reads.Calls
		spec.ReadRows = fn.Caps.Reads.Rows
	}
	// A bundled function sees its bundle's shared library modules on the import
	// path (PYTHONPATH for python, the vendored `lib` package for go). The
	// module set re-keys the installation, so changing one re-registers or
	// rebuilds the body exactly like editing it.
	if b, ok := reg.BundleOf(fn.Authority); ok && len(b.Modules) > 0 {
		spec.Modules = b.Modules
	}
	return spec
}

// runCallable executes one function body and returns EVERY effect to apply —
// sub-call effects first, in call order, then the body's own — plus the
// output value. The body runs BEFORE the effects transaction opens, exactly
// where the CEL evaluation used to sit. It discards any paged-checkpoint
// continuation: the single-shot callers (manual run, host Call, the call API,
// the agent loop) never page.
func (ds *dataset) runCallable(ctx context.Context, fn *vocabulary.Function, in runner.Input) ([]effect, any, error) {
	effects, output, _, err := ds.runCallableRaw(ctx, fn, in)
	return effects, output, err
}

// runCallableRaw is runCallable with the paged-checkpoint continuation
// surfaced: a non-nil *runner.Continuation means the body finished a PAGE and
// wants re-invoking (the drain loop in functions.go). The delivery paths use
// this; everyone else takes runCallable and ignores paging.
func (ds *dataset) runCallableRaw(ctx context.Context, fn *vocabulary.Function, in runner.Input) ([]effect, any, *runner.Continuation, error) {
	inv := &invocation{ds: ds, stack: []string{fn.Identity()}, scrub: newScrubber()}
	// The runner's `config` field, resolved per invocation (bundleconfig.go):
	// a bundle function receives its bundle's config record properties —
	// secrets resolved — and its account records, live tokens injected. The
	// injected values arm the invocation SCRUBBER: nothing that leaves the
	// runner boundary (logs, error text — and so run rows and parked
	// failures — outputs, and so tool transcripts and API responses) may
	// carry them verbatim.
	if in.Config == nil {
		cfg, secrets, err := ds.resolveFunctionConfig(ctx, fn)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("run: config: %w", err)
		}
		in.Config = cfg
		inv.scrub.add(secrets...)
	}
	res, err := runner.Shared.Invoke(ctx, ds.runnerSpec(fn), in, &callBackend{
		inv: inv, fn: fn, key: in.IdempotencyKey, causalDepth: in.CausalDepth,
	})
	if err != nil {
		return nil, nil, nil, inv.scrub.err(fmt.Errorf("run: %w", err))
	}
	for _, line := range res.Logs {
		ds.svc.log.Info("substrate: function log", "function", fn.Identity(), "line", inv.scrub.text(line))
	}
	// The returned effects are ADDRESSED data the outbound scrubber cannot
	// redact in place: an injected secret copied into a property value, id or
	// relation would persist verbatim. Reject the whole invocation
	// before decode — nothing applies — rather than write the redaction marker
	// as user data. The scrubber holds every value injected anywhere under this
	// root (config + each sub-call's), so a sub-call's secret is caught too.
	if inv.scrub.found(res.Effects) {
		return nil, nil, nil, fmt.Errorf("run: %w", errSecretInEffects)
	}
	// The paged-checkpoint continuation is the OTHER value that leaves the
	// runner boundary un-scrubbed: the host stores `more.cursor` verbatim in
	// paged_cursors. It is opaque — redacting it would corrupt
	// resume — so a secret copied into it rejects the whole invocation, exactly
	// like a secret-bearing effect, before any page or cursor commits. The
	// scrubber holds every value injected anywhere under this root.
	if res.More != nil && inv.scrub.found(res.More) {
		return nil, nil, nil, fmt.Errorf("run: %w", errSecretInContinuation)
	}
	own, err := ds.decodeEffects(fn, res.Effects)
	if err != nil {
		return nil, nil, nil, inv.scrub.err(fmt.Errorf("run: %w", err))
	}
	return append(inv.effects, own...), inv.scrub.value(res.Output), res.More, nil
}

// invocation is the per-delivery state one root invocation and its nested
// Calls share: the identity stack (recursion refusal), the accumulated
// sub-call effects and the call ordinal.
type invocation struct {
	ds *dataset
	// stack holds the function identities from the root down, inclusive: a
	// Call to anything already on it is refused — direct and mutual
	// recursion both, so a cycle can never exhaust the python host slots.
	stack []string
	// effects accumulates the sub-calls' decoded effects, in call order.
	// They apply in the CALLER's delivery transaction, before the caller's
	// own — each decoded against ITS function's capability envelope. A
	// FAILED sub-call contributes nothing: Call checkpoints the length and
	// truncates back on any target error, so a caller that catches a
	// callee's failure never smuggles the callee's descendants' effects
	// into its own success.
	effects []effect
	// calls counts the host Calls issued under this root invocation, in
	// order. The ordinal rides each child idempotency key, so two calls to
	// the SAME callee in one body are two distinct operations to any
	// external deduper.
	calls int
	// scrub accumulates every secret injected ANYWHERE under this root
	// invocation (the root's config and each sub-call's), and every outbound
	// surface passes through it. Exact-value scrubbing is containment only —
	// transformed or body-side network exfiltration needs opaque credential
	// handles instead of raw values (a recorded follow-up).
	scrub *scrubber
}

// callBackend serves one function invocation's host calls: the capability-
// scoped reads from the dataset's ordinary read surface — the same
// projection the wire shows, secrets redacted, canonical ids resolved,
// never a wider view — and Call, the function-to-function invocation.
type callBackend struct {
	inv         *invocation
	fn          *vocabulary.Function
	key         string
	causalDepth int
}

func (b *callBackend) Get(ctx context.Context, typ, id string) (*substrate.Record, error) {
	e, err := b.inv.ds.Get(ctx, typ, id)
	if errors.Is(err, substrate.ErrNotFound) {
		// Absence is a normal answer: existence probes ask exactly this.
		return nil, nil
	}
	return e, err
}

func (b *callBackend) List(ctx context.Context, q substrate.Query) (*substrate.Page, error) {
	return b.inv.ds.List(ctx, q)
}

func (b *callBackend) Search(ctx context.Context, in substrate.SearchInput) ([]substrate.Hit, error) {
	return b.inv.ds.Search(ctx, in)
}

// ResolveKind answers the runner's reads gate in the repository's own
// vocabulary: bare `task` and `tasks.substrate.reamde.dev/task` are one kind, and the
// allowlist is written in identities. A name the registry does not know comes
// back unchanged and is refused there.
func (b *callBackend) ResolveKind(name string) string {
	ty, err := b.inv.ds.resolveType(name)
	if err != nil || ty == nil {
		return name
	}
	return ty.Identity
}

// Call runs another function to completion inside this invocation. The
// runner already gated the caller's `capabilities.call` allowlist and
// charged its call budget; the engine's half gates depth, recursion and the
// callee's declared input/output shapes. The callee's effects accumulate on
// the shared invocation — they apply in the ROOT delivery's transaction —
// and its output returns to the calling body.
func (b *callBackend) Call(ctx context.Context, ident string, args any) (any, error) {
	ds := b.inv.ds
	target, err := ds.registry().ResolveFunction(ident)
	if err != nil {
		return nil, fmt.Errorf("call: %w", err)
	}
	// Nested admission re-checks the callee's bundle lifecycle under the
	// root's already-held fence: a live bundle cannot invoke a
	// disabled bundle's function, and — because the fence is held for the
	// whole tree — cannot commit that callee's effects after its lifecycle
	// verb returns. No re-acquire (the lease is inherited via ctx).
	if _, _, err := ds.admitCallable(ctx, target.Authority, target.Identity()); err != nil {
		return nil, fmt.Errorf("call: %w", err)
	}
	for _, on := range b.inv.stack {
		if on == target.Identity() {
			return nil, fmt.Errorf("call: %s is already on the call stack — recursion is refused", target.Identity())
		}
	}
	// Sub-calls ride the causal-depth cap: a delivery at depth D nests at
	// most cap−D calls, so a runaway chain stops with the same distinct
	// error whether it loops through the changelog or through Call.
	depth := b.causalDepth + 1
	if depth >= causalDepthCap {
		return nil, fmt.Errorf("%w: call to %s at depth %d (cap %d)", errCausalDepth, target.Identity(), depth, causalDepthCap)
	}
	if target.Input != nil {
		if err := vocabulary.CheckValue(target.Input, args); err != nil {
			return nil, fmt.Errorf("call %s: input: %w", target.Identity(), err)
		}
	}
	b.inv.stack = append(b.inv.stack, target.Identity())
	defer func() { b.inv.stack = b.inv.stack[:len(b.inv.stack)-1] }()
	// The child key extends the caller's — the stack path — plus the shared
	// invocation's call ordinal: two calls to the same callee are two
	// distinct operations, and the path+ordinal pair survives across
	// runtimes because the key is composed here, never inside a body.
	b.inv.calls++
	key := fmt.Sprintf("%s/call/%d/%s", b.key, b.inv.calls, target.Identity())
	// Checkpoint the shared effect list BEFORE the callee runs: its own
	// effects and its descendants' apply only if the whole call settles
	// clean. On any failure below — target error, effect decode error,
	// output validation error — the list truncates back, so a caller that
	// CATCHES the failure cannot commit a half-finished callee's effects.
	mark := len(b.inv.effects)
	// The callee resolves ITS OWN config — a caller's bundle never leaks
	// into another bundle's function. Its secrets join the SHARED invocation
	// scrubber: whatever bubbles back up through the root's boundary is
	// held to the whole tree's injected values.
	cfg, secrets, err := ds.resolveFunctionConfig(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("call %s: config: %w", target.Identity(), err)
	}
	b.inv.scrub.add(secrets...)
	res, err := runner.Shared.Invoke(ctx, ds.runnerSpec(target), runner.Input{
		Mode:           runner.ModeCall,
		Args:           args,
		Config:         cfg,
		CausalDepth:    depth,
		CallDepth:      len(b.inv.stack) - 1,
		IdempotencyKey: key,
	}, &callBackend{inv: b.inv, fn: target, key: key, causalDepth: depth})
	if err != nil {
		b.inv.effects = b.inv.effects[:mark]
		return nil, b.inv.scrub.err(fmt.Errorf("call %s: %w", target.Identity(), err))
	}
	for _, line := range res.Logs {
		ds.svc.log.Info("substrate: function log", "function", target.Identity(), "line", b.inv.scrub.text(line))
	}
	// The callee's effects face the same addressed-data gate as the root's
	//: a secret copied into an id, relation or property value is
	// rejected before decode. Truncate the callee's accumulated effects back
	// first, so a caller that catches this cannot commit them.
	if b.inv.scrub.found(res.Effects) {
		b.inv.effects = b.inv.effects[:mark]
		return nil, fmt.Errorf("call %s: %w", target.Identity(), errSecretInEffects)
	}
	effects, err := ds.decodeEffects(target, res.Effects)
	if err != nil {
		b.inv.effects = b.inv.effects[:mark]
		return nil, b.inv.scrub.err(fmt.Errorf("call %s: %w", target.Identity(), err))
	}
	// A declared Output validates even a nil answer: omitting `output` or
	// returning JSON null is a shape violation unless the schema is `any`.
	if target.Output != nil {
		if err := vocabulary.CheckValue(target.Output, res.Output); err != nil {
			b.inv.effects = b.inv.effects[:mark]
			return nil, b.inv.scrub.err(fmt.Errorf("call %s: output: %w", target.Identity(), err))
		}
	}
	b.inv.effects = append(b.inv.effects, effects...)
	// The callee's output returns RAW to the calling BODY (both sit inside
	// the runner boundary); the root's boundary scrub covers whatever the
	// caller re-emits.
	return res.Output, nil
}

// CallFunction is the callable invocation API (`mode: call`): arbitrary
// input, validated against the manifest's `input:` schema when one is
// declared; no cursor motion, no run row; effects — the body's and its
// sub-calls' — applied in one transaction under the FUNCTION's actor. It
// returns the output (checked against `output:` when declared) and how many
// effects applied.
func (ds *dataset) CallFunction(ctx context.Context, name string, args any) (any, int, error) {
	fn, err := ds.registry().ResolveFunction(name)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", substrate.ErrNotFound, err)
	}
	// Admission under the bundle lifecycle fence, held until the effects
	// commit: a concurrent disable/uninstall/purge waits this invocation out
	// instead of racing effects in behind it (bundles.go, review #2). The
	// leased context flows into runCallable so nested host Calls inherit it.
	ctx, release, err := ds.admitCallable(ctx, fn.Authority, fn.Identity())
	if err != nil {
		return nil, 0, err
	}
	defer release()
	if fn.Input != nil {
		if err := vocabulary.CheckValue(fn.Input, args); err != nil {
			return nil, 0, fmt.Errorf("%w: input: %w", substrate.ErrValidation, err)
		}
	}
	callID, err := newID()
	if err != nil {
		return nil, 0, err
	}
	effects, output, err := ds.runCallable(ctx, fn, runner.Input{
		Mode: runner.ModeCall,
		Args: args,
		// Unique per call: a manual invocation is not a delivery, so nothing
		// external should dedupe two of them into one.
		IdempotencyKey: fmt.Sprintf("%s/%s/call/%s", ds.Repository().Name, fn.Identity(), callID),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", substrate.ErrValidation, err)
	}
	// A declared Output validates even a nil answer (`any` stays open): the
	// shape contract holds before any effect commits.
	if fn.Output != nil {
		if err := vocabulary.CheckValue(fn.Output, output); err != nil {
			return nil, 0, fmt.Errorf("%w: output: %w", substrate.ErrValidation, err)
		}
	}
	if len(effects) > 0 {
		actor := substrate.Actor(fn.Actor())
		err = ds.inTx(ctx, actor, false, func(t *txn) error {
			t.setEffectEmit(fn.Caps.Emit)
			if err := t.lockEffectTargets(effects); err != nil {
				return err
			}
			for _, ef := range effects {
				if err := t.applyEffect(ef); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return nil, 0, err
		}
	}
	return output, len(effects), nil
}

const (
	// maxPrepareBatch caps how many function bodies ONE admission batch warms.
	// The 32 MiB schema request can carry hundreds of small dependency-declaring
	// bodies; without a count cap a single apply could serialize an unbounded
	// number of cold uv resolves under the schema-write lock (finding #13).
	maxPrepareBatch = 64
	// prepareBatchBudget is the AGGREGATE wall-clock a whole preparation phase
	// may take, independent of body count. A single cold uv resolve is bounded
	// by uvProvisionTimeout (120s); this bounds the SUM across the batch, so
	// many stalling PEP 723 bodies cannot hold schema-write serialization for
	// N × 120s. It comfortably exceeds a single provision floor, so a batch with
	// one legitimate cold resolve plus cached bodies still admits.
	prepareBatchBudget = 5 * time.Minute
)

// prepareFunctions compiles and registers the given bodies SYNCHRONOUSLY —
// the admission half of "bodies prepare at registration": schema apply calls
// it for every ADDED or CHANGED body before activation, and the first body
// that cannot compile or load fails the whole batch. The installer hears the
// build or registration error instead of parking the first delivery.
//
// Two batch-wide admission bounds guard the shared schema-write serialization
// (finding #13): a count cap on functions-per-batch, and an aggregate deadline
// over the WHOLE phase. Preparation runs SEQUENTIALLY, so at most one uv
// provision (network + resolve) is ever in flight per batch — the concurrent-
// uv-process cap the smallest fix asks for is satisfied by construction.
func (ds *dataset) prepareFunctions(ctx context.Context, reg *vocabulary.Registry, fns []*vocabulary.Function) error {
	if len(fns) > maxPrepareBatch {
		return fmt.Errorf("%w: an admission batch prepares at most %d function bodies, got %d — split the apply into smaller batches",
			substrate.ErrValidation, maxPrepareBatch, len(fns))
	}
	ctx, cancel := context.WithTimeout(ctx, prepareBatchBudget)
	defer cancel()
	for _, fn := range fns {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%w: function preparation exceeded the %s admission budget before %s",
				substrate.ErrValidation, prepareBatchBudget, fn.Identity())
		}
		if err := runner.Shared.Warm(ctx, ds.runnerSpecIn(fn, reg)); err != nil {
			return fmt.Errorf("%w: function %s: body failed to prepare: %w",
				substrate.ErrValidation, fn.Identity(), err)
		}
	}
	return nil
}

// warmFunctions prepares every registered body at repository open, ahead of its
// first delivery. Registration already prepared these synchronously, so this
// is a cache warm-up — but a persisted body that no longer prepares (a
// toolchain change, a corrupted cache) is a real outage in the making, so a
// failure surfaces as an ERROR naming what will park, not a shrug.
func (ds *dataset) warmFunctions() {
	fns := ds.registry().Functions()
	if len(fns) == 0 {
		return
	}
	go func() {
		for _, fn := range fns {
			if err := runner.Shared.Warm(context.Background(), ds.runnerSpec(fn)); err != nil {
				ds.svc.log.Error("substrate: function body failed to prepare at repository open — its deliveries will park until the body or toolchain is fixed",
					"repository", ds.Repository().Name, "function", fn.Identity(), "error", err)
			}
		}
	}()
}

// reconcileRunner retires runner state no live registration references — the
// registry-publish hook: python registrations of removed or superseded bodies
// deregister, their Go processes stop. An uninstalled bundle's functions are
// gone from the registry (uninstall tears the authority down, ticket 034), so they
// drop out here with everything else the last apply removed. A DISABLED
// bundle's functions stay registered — disable only refuses invocation.
// Build-cache artifacts stay (immutable, shared; eviction is a later policy).
func (ds *dataset) reconcileRunner(ctx context.Context) {
	fns := ds.registry().Functions()
	live := make([]runner.Spec, 0, len(fns))
	for _, fn := range fns {
		live = append(live, ds.runnerSpec(fn))
	}
	runner.Shared.Reconcile(ctx, ds.Repository().Name, live)
}
