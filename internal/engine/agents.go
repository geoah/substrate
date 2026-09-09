package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// The engine's agent plumbing around the loop (agentloop.go): the well-known
// llmprovider row, and the two direct entry points — the call API and chat.
// Trigger dispatch enters through functions.go's deliver/deliverFire, which
// branch on the trigger's callable kind.

// deliverToAgent runs one record delivery through the loop (deliver()'s
// agent branch, after the guard passed): the delivery is CLAIMED first
// (settlement.claim: the cursor's compare-and-swap and the delivery recorded
// as in flight, one transaction), then the envelope becomes the first user
// message and the loop's writes land incrementally under the agent's actor
// with caused_by stamped, and the claim is COMPLETED inside the transaction
// that settles the thread (agentloop.go settle, agentInvocation.complete):
// the thread's terminal status, the claim's retirement, the delivery entry
// and the run record are one commit. A concurrent dispatcher's duplicate
// loses the claim's swap and runs nothing. A loop error rides the ordinary
// retries, which find the claim, and parks by rewriting it. A crash mid-loop
// leaves the claim, listed as in flight and retried by hand: the loop's
// effects are never committed without a recorded delivery state, and nothing
// redelivers by itself.
func (ds *dataset) deliverToAgent(ctx context.Context, tr *trigger, ch substrate.Change, depth int, envelope map[string]any, mode string, advance bool, settle *settlement) (deliverResult, error) {
	var res deliverResult
	claim, err := ds.claimAgentDelivery(ctx, settle)
	if err != nil {
		return res, err
	}
	user, err := json.Marshal(envelope)
	if err != nil {
		return res, err
	}
	ares, err := ds.runAgent(ctx, tr.Agent, agentInvocation{
		mode: mode, user: string(user),
		causedBy: ch.Seq, causalDepth: depth,
		// The stable delivery identity (the function-trigger key shape):
		// tool idempotency keys derive from it, so a RETRIED delivery — a
		// fresh thread by construction — reproduces the same keys.
		delivery: fmt.Sprintf("%s/%s/%d", ds.Repository().Name, tr.ID, ch.Seq),
		complete: agentCompletion(settle, claim, advance),
	})
	if err != nil {
		return res, err
	}
	return agentResult(advance, ares), nil
}

// claimAgentDelivery writes an agent delivery's claim in its own transaction
// (settlement.claim); nil settles nothing and claims nothing.
func (ds *dataset) claimAgentDelivery(ctx context.Context, settle *settlement) (int64, error) {
	if settle == nil {
		return 0, nil
	}
	var claim int64
	err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		id, err := settle.claim(t)
		claim = id
		return err
	})
	if err != nil {
		// A claim the transaction took and rolled back is given back. A
		// retry's hold is the caller's (RetryTriggerFailure, deliverFire on a
		// pending webhook request) and outlives this attempt.
		if settle.retire == 0 {
			settle.release()
		}
		return 0, err
	}
	if settle.retire == 0 {
		settle.claimed = claim
	}
	// A pending webhook request's row now reads as in flight; the rewrite
	// committed, so a later attempt of the same fire does not repeat it.
	settle.pending = nil
	return claim, nil
}

// agentCompletion is the hook the loop runs inside the thread's settling
// transaction (agentloop.go settle): the claim retires with the run record
// there. nil when nothing settles.
func agentCompletion(settle *settlement, claim int64, advance bool) func(t *txn, ares *substrate.AgentResult) error {
	if settle == nil {
		return nil
	}
	return func(t *txn, ares *substrate.AgentResult) error {
		return settle.complete(t, claim, agentResult(advance, ares))
	}
}

// agentResult is a settled agent delivery's outcome: ran when the loop
// applied any effect, moved when the dispatch owned the cursor.
func agentResult(advance bool, ares *substrate.AgentResult) deliverResult {
	res := deliverResult{moved: advance, effects: ares.EffectsByAction}
	if ares.Effects > 0 {
		res.ran = 1
	}
	return res
}

// agentFire runs one schedule/webhook fire through the loop, deliverFire's
// agent branch, under the same claim-then-complete protocol as
// deliverToAgent: the fire state moves with the claim before the loop, so a
// concurrent dispatcher's duplicate loses the swap there and runs nothing.
func (ds *dataset) agentFire(ctx context.Context, tr *trigger, mode, fid string, at time.Time, envelope map[string]any, settle *settlement) (int, error) {
	// Admission under the lifecycle fence, held through the claim, the
	// loop's writes and the completion below (bundles.go, review #2).
	ctx, release, err := ds.admitCallable(ctx, tr.Agent.Package, tr.Agent.Identity())
	if err != nil {
		return 0, err
	}
	defer release()
	claim, err := ds.claimAgentDelivery(ctx, settle)
	if err != nil {
		return 0, err
	}
	env, err := ds.fireEnvelope(ctx, envelope, fid, at)
	if err != nil {
		return 0, err
	}
	user, err := json.Marshal(env)
	if err != nil {
		return 0, err
	}
	ares, err := ds.runAgent(ctx, tr.Agent, agentInvocation{
		mode: mode, user: string(user),
		// Stable per occurrence: a retried fire reuses the fire id, so tool
		// keys survive the retry (the functionFire key shape).
		delivery: fmt.Sprintf("%s/%s/%s", ds.Repository().Name, tr.ID, fid),
		complete: agentCompletion(settle, claim, false),
	})
	if err != nil {
		return 0, err
	}
	return agentResult(false, ares).ran, nil
}

// The callable lifecycle gate for agents is callableGroupBlocked (bundles.go),
// generic to functions and agents alike and re-checked under the held fence by
// admitCallable: a disabled or uninstalled bundle's agent refuses invocation on
// EVERY entry — the call API, chat, and sub-agent dispatch. Trigger dispatch
// additionally blocks bundled callables of both kinds at load
// (blockBundledCallable) and re-admits under the fence at delivery.

// CallAgent is the callable invocation API's agent half (`mode: call`):
// arbitrary input becomes the first user message, the loop runs to
// settlement, and the final reply returns with the thread id — the durable
// trace is the thread, so unlike a function call something IS minted.
func (ds *dataset) CallAgent(ctx context.Context, name string, input any) (*substrate.AgentResult, error) {
	// The request's Idempotency-Key first, before the agent is resolved or
	// admitted (idempotency.go): a stored outcome answers a repeat even after
	// the agent was disabled or uninstalled, because the first attempt ran.
	call, stored, err := ds.beginIdempotent(ctx, idemAgentCall, agentCallInput{Name: name, Input: input})
	if err != nil {
		return nil, err
	}
	if stored != nil {
		var replayed substrate.AgentResult
		if err := json.Unmarshal(stored, &replayed); err != nil {
			return nil, fmt.Errorf("decode the stored outcome: %w", err)
		}
		return &replayed, nil
	}
	// The key is consumed: the loop, its tools and its sub-agents run
	// without one, so a mutate tool's create never inherits it.
	ctx = substrate.WithoutIdempotencyKey(ctx)
	res, err := ds.callAgentOnce(ctx, name, input, call)
	if err != nil {
		// Nothing settled. The reservation goes unless the attempt opened a
		// thread, whose effects may have committed (attachThread).
		call.release(ctx)
		return nil, err
	}
	return res, nil
}

// callAgentOnce is CallAgent's one attempt: admission, the loop, and the
// idempotency reservation bound to the thread as it opens and settled in the
// thread's settling transaction, the completion hook a trigger delivery uses
// for the same reason.
func (ds *dataset) callAgentOnce(ctx context.Context, name string, input any, call *idempotentCall) (*substrate.AgentResult, error) {
	ag, err := ds.registry().ResolveAgent(name)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", substrate.ErrNotFound, err)
	}
	// Admission under the lifecycle fence, held through the whole loop's writes
	// (thread, every message, settlement) — a disable draining this call waits
	// for the thread to settle before it returns (bundles.go, review #2).
	ctx, release, err := ds.admitCallable(ctx, ag.Package, ag.Identity())
	if err != nil {
		return nil, err
	}
	defer release()
	user, err := agentUserContent(input)
	if err != nil {
		return nil, err
	}
	// The reservation's lease follows the agent's own deadline from here;
	// attachThread extends it to the retention window once the thread opens.
	if err := call.extendLease(ctx, nowUTC().Add(time.Duration(ag.Budgets.DeadlineSeconds)*time.Second)); err != nil {
		return nil, err
	}
	inv := agentInvocation{mode: "call", user: user}
	if call != nil {
		inv.onThread = call.attachThread
		inv.complete = func(t *txn, res *substrate.AgentResult) error { return call.settleIn(t, res) }
		// The delivery identity the loop derives its tool keys from is the
		// client's key, not a per-call mint (runAgent's default): an external
		// effect under this call presents a downstream key that names the
		// client's attempt, so two attempts under one key are one effect to a
		// provider that honors it, and two keys are two.
		inv.delivery = call.downstreamKey(ag.Identity())
	}
	res, err := ds.runAgent(ctx, ag, inv)
	if err != nil {
		return nil, agentEntryError(err)
	}
	return res, nil
}

// agentCallInput is what an agent call's idempotency fingerprint covers: the
// agent addressed and the input as decoded.
type agentCallInput struct {
	Name  string `json:"name"`
	Input any    `json:"input"`
}

// agentEntryError shapes a loop error for the direct entries: a sentinel the
// engine already classified (a lease conflict, a missing thread, a guard)
// passes through; anything else — the LLM transport, mostly — reads as a
// validation failure of the invocation.
func agentEntryError(err error) error {
	for _, sentinel := range []error{substrate.ErrConflict, substrate.ErrNotFound, substrate.ErrGuard, substrate.ErrForbidden, substrate.ErrValidation} {
		if errors.Is(err, sentinel) {
			return err
		}
	}
	return fmt.Errorf("%w: %w", substrate.ErrValidation, err)
}

// ChatAgent is the same loop with a live client attached (ticket 007 ruling
// 3): open or continue a thread against any agent with a user message, the
// assistant turns streaming through emit. No trigger, no cursor — the thread
// is ordinary data the console renders.
func (ds *dataset) ChatAgent(ctx context.Context, actor substrate.Actor, name, threadID, message string, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	ag, err := ds.registry().ResolveAgent(name)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", substrate.ErrNotFound, err)
	}
	if ag.HiddenFromChat {
		// The declaration's own word: an agent hidden from chat (an llm-as-judge)
		// exists to be called by other agents, and the chat surface says so
		// instead of opening a thread the console would never have offered.
		return nil, fmt.Errorf("%w: agent %s is hidden from chat: call it from another agent, or drop data.hiddenFromChat from its declaration",
			substrate.ErrValidation, ag.Identity())
	}
	// Admission under the lifecycle fence, held through the whole chat turn's
	// writes (thread claim/mint, every message, settlement) — review #2.
	ctx, release, err := ds.admitCallable(ctx, ag.Package, ag.Identity())
	if err != nil {
		return nil, err
	}
	defer release()
	if message == "" {
		return nil, fmt.Errorf("%w: a chat turn needs a message", substrate.ErrValidation)
	}
	res, err := ds.runAgent(ctx, ag, agentInvocation{
		mode: agentModeChat, user: message, userActor: actor,
		threadID: threadID, emit: emit,
	})
	if err != nil {
		return nil, agentEntryError(err)
	}
	return res, nil
}

// agentUserContent renders a call input as the first user message: a string
// passes through, anything else travels as JSON. An EMPTY string is refused
// exactly like a nil one — a wire that rejects an empty user text block
// (anthropic 400s on it) would otherwise settle the thread on an error after
// its rows had already landed.
func agentUserContent(input any) (string, error) {
	switch v := input.(type) {
	case nil:
		return "", fmt.Errorf("%w: a call needs an input", substrate.ErrValidation)
	case string:
		if v == "" {
			return "", fmt.Errorf("%w: a call needs an input", substrate.ErrValidation)
		}
		return v, nil
	default:
		buf, err := json.Marshal(v)
		if err != nil {
			return "", fmt.Errorf("%w: input: %w", substrate.ErrValidation, err)
		}
		return string(buf), nil
	}
}
