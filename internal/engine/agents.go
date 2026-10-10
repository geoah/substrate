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
// llm/provider row, and the two direct entry points — the call API and chat.
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
// leaves the claim, which the next open parks as interrupted
// (settleInterruptedAgentRuns) and that open's first dispatcher pass reruns
// once (rerunInterruptedAgentRuns): the loop's effects are never committed
// without a recorded delivery state, and a run interrupted again waits for a
// person.
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
		delivery: fmt.Sprintf("%s/%s/%d", ds.Repository().ID, tr.ID, ch.Seq),
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
	// loop's writes and the completion below (bundles.go).
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
		delivery: fmt.Sprintf("%s/%s/%s", ds.Repository().ID, tr.ID, fid),
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
		return nil, fmt.Errorf("%w: %w", callableRefusal(name), err)
	}
	// Admission under the lifecycle fence, held through the whole loop's writes
	// (thread, every message, settlement) — a disable draining this call waits
	// for the thread to settle before it returns (bundles.go).
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

// ChatAgent is the same loop with a live client attached: open or continue a
// thread against any agent with a user message, the assistant turns
// streaming through emit. No trigger, no cursor — the thread is ordinary
// data the console renders.
func (ds *dataset) ChatAgent(ctx context.Context, actor substrate.Actor, name, threadID, message string, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	ag, err := ds.registry().ResolveAgent(name)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", callableRefusal(name), err)
	}
	if ag.HiddenFromChat {
		// The declaration's own word: an agent hidden from chat (an llm-as-judge)
		// exists to be called by other agents, and the chat surface says so
		// instead of opening a thread the console would never have offered.
		return nil, fmt.Errorf("%w: agent %s is hidden from chat: call it from another agent, or drop data.hiddenFromChat from its declaration",
			substrate.ErrValidation, ag.Identity())
	}
	// Admission under the lifecycle fence, held through the whole chat turn's
	// writes (thread claim/mint, every message, settlement).
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

// interruptedThreadReason is the reason a thread a stopped writer left
// `running` settles with (settleInterruptedAgentRuns).
const interruptedThreadReason = "interrupted: the server stopped during the run"

// settleInterruptedAgentRuns is the agent runs' open-time sweep, beside
// settleInterruptedSyncs: at open no loop of this repository is running (0083:
// one writer per repository, and open runs before the dataset is published),
// so every claim still stored and every thread still `running` belongs to a
// run the last writer's stop ended. A claim becomes an ordinary parked
// failure naming the stop, so the listing and a replay stop treating it as a
// live run; the first dispatcher pass reruns it once
// (rerunInterruptedAgentRuns, decision 0151), and a hand may retry or forget
// it before that. A thread settles to `error` naming the stop.
func (ds *dataset) settleInterruptedAgentRuns(ctx context.Context) error {
	if ds.svc.readOnly {
		return nil
	}
	if err := ds.settleInterruptedClaims(ctx); err != nil {
		return err
	}
	return ds.settleInterruptedThreads(ctx)
}

// settleInterruptedClaims rewrites every stored claim to interruptedAgentError,
// one ledger entry per trigger, so a rebuild and a restore agree. A row an
// earlier binary parked as interrupted, in the words it wrote, is rewritten
// the same way, so it is rerun too.
func (ds *dataset) settleInterruptedClaims(ctx context.Context) error {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT id, trigger_id, seq, fire_id, record_id, attempts, parked_at, payload
		FROM trigger_failures WHERE last_error IN ($1, $2, $3) ORDER BY trigger_id, id`,
		inFlightError, legacyInFlightError, legacyInterruptedAgentError)
	if err != nil {
		return err
	}
	byTrigger := map[string][]foldFailure{}
	var order []string
	for rows.Next() {
		var f foldFailure
		var trigger string
		var payload []byte
		if err := rows.Scan(&f.ID, &trigger, &f.Seq, &f.FireID, &f.RecordID, &f.Attempts, &f.ParkedAt, &payload); err != nil {
			_ = rows.Close()
			return err
		}
		f.ParkedAt = f.ParkedAt.UTC()
		f.Payload = json.RawMessage(payload)
		f.LastError = interruptedAgentError
		// The interrupted run was one attempt.
		f.Attempts = max(f.Attempts, 1)
		if _, seen := byTrigger[trigger]; !seen {
			order = append(order, trigger)
		}
		byTrigger[trigger] = append(byTrigger[trigger], f)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, trigger := range order {
		failures := byTrigger[trigger]
		err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
			for _, f := range failures {
				if err := t.lockFailure(trigger, int64(f.ID)); err != nil {
					return err
				}
				if err := t.parkTx(trigger, f); err != nil {
					return err
				}
			}
			if err := t.settleDelivery(trigger); err != nil {
				return err
			}
			// The newest claim names the record, as a dispatched park does.
			return t.raiseInterruptedAlert(trigger, int64(failures[len(failures)-1].Seq))
		})
		if err != nil {
			return fmt.Errorf("settle interrupted agent deliveries of trigger %s: %w", trigger, err)
		}
		ds.svc.log.Warn("substrate: agent deliveries interrupted by a stop, parked for the first trigger pass to rerun once",
			"trigger", logSafeID(trigger), "deliveries", len(failures))
	}
	return nil
}

// The stages of an open's one walk over the agent deliveries the last stop
// interrupted (dataset.interruptedRerun).
const (
	rerunNotStarted int32 = iota
	rerunWalking
	rerunWalked
)

// rerunInterruptedAgentRuns reruns, once per open and from the first
// dispatcher pass (ProcessTriggers), every agent delivery the open-time sweep
// parked as interrupted after one attempt (settleInterruptedAgentRuns),
// decision 0151. That includes a function body's agent claim (decision
// 0121), whose rerun runs the body again from its start. A rerun is a retry
// by hand that the server runs: it delivers the parked change or fire to the
// trigger as the trigger stands at the rerun, callable and arguments
// included, so a trigger edited since the claim reruns its new callable. It
// runs where resumeWebhooks runs, for the same reason: only the
// server dispatches, so an operator's process (a rebuild, a reset) opens the
// repository and reruns nothing, and a read-only process appends nothing.
// The walk is one detached task that reruns one delivery at a time, so a slow
// loop does not hold the pass. A walk that could not read its rows, or that
// panicked, is started again by the next pass; the rows it already reran no
// longer qualify.
func (ds *dataset) rerunInterruptedAgentRuns() {
	if ds.svc.readOnly || !ds.interruptedRerun.CompareAndSwap(rerunNotStarted, rerunWalking) {
		return
	}
	if !ds.spawn("interrupted agent rerun", func(ctx context.Context) {
		stage := rerunNotStarted
		defer func() { ds.interruptedRerun.Store(stage) }()
		if ds.runInterruptedAgentReruns(ctx) {
			stage = rerunWalked
		}
	}) {
		ds.interruptedRerun.Store(rerunNotStarted)
	}
}

// runInterruptedAgentReruns is the walk: every row carrying
// interruptedAgentError at attempt 1, oldest first. It reports false when it
// could not read them. Rows a shutdown kept it from reaching keep their
// error, and the next open reruns them.
func (ds *dataset) runInterruptedAgentReruns(ctx context.Context) bool {
	type interrupted struct {
		trigger string
		id      int64
	}
	rows, err := ds.db.QueryContext(ctx, `
		SELECT trigger_id, id FROM trigger_failures
		WHERE last_error = $1 AND attempts = 1 ORDER BY id`, interruptedAgentError)
	if err != nil {
		if ctx.Err() == nil {
			ds.svc.log.Error("substrate: interrupted agent deliveries could not be read for their rerun",
				"repository", ds.Repository().ID, "error", err)
		}
		return false
	}
	var found []interrupted
	for rows.Next() {
		var r interrupted
		if err := rows.Scan(&r.trigger, &r.id); err != nil {
			_ = rows.Close()
			return false
		}
		found = append(found, r)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return false
	}
	for _, r := range found {
		if ctx.Err() != nil {
			break
		}
		ds.rerunInterruptedAgentRun(ctx, r.trigger, r.id)
	}
	return true
}

// The fixed words a rerun's log line carries beside the webhook fire's
// (webhookFireOutcome), for the cases a fire does not have.
const (
	rerunOutcomeOK     = "ok"     // the rerun settled and the row retired
	rerunOutcomeParked = "parked" // the rerun failed and the row holds its error at attempt 2
	rerunOutcomeHeld   = "held"   // the trigger is disabled or its callable does not resolve
	rerunOutcomeCapped = "capped" // the trigger's agent is at a spend cap
)

// rerunInterruptedAgentRun reruns one interrupted agent delivery, the row
// failureID of triggerID. The order is what makes it at most once: the
// in-process claim first (settlement.acquire), so a hand already retrying or
// forgetting the row keeps it and a hand arriving later answers conflict;
// then, under that claim and the row lock, the row is checked again and
// rewritten to attempt 2 and rerunAgentError in its own ledger entry
// (markRerun); then the delivery runs through the retry's own body
// (retryHeldFailure); then the claim is released. One log line per delivery
// names the trigger, the failure, the callable the trigger names now (the
// one the rerun runs) and the outcome as a fixed word, never the error,
// which may quote the request or the model.
func (ds *dataset) rerunInterruptedAgentRun(ctx context.Context, triggerID string, failureID int64) {
	attrs := []any{"repository", ds.Repository().ID, "trigger", logSafeID(triggerID), "failure", failureID}
	skipped := func(outcome string, extra ...any) {
		ds.svc.log.Warn("substrate: agent delivery a restart interrupted was not rerun, it stays parked",
			append(append(attrs, "outcome", outcome), extra...)...)
	}
	settle := &settlement{ds: ds, trigger: triggerID}
	if err := settle.acquire(failureID); err != nil {
		ds.svc.log.Info("substrate: agent delivery a restart interrupted was not rerun, a hand holds it",
			append(attrs, "outcome", fireOutcomeRunning)...)
		return
	}
	defer settle.release()
	tr, _, err := ds.triggerByID(ctx, triggerID)
	if err == nil {
		attrs = append(attrs, "callable", logSafeID(tr.callablePath()))
	}
	if err != nil || !tr.Enabled || !tr.runnable() {
		skipped(rerunOutcomeHeld)
		return
	}
	// An agent at a spend cap is checked before the rewrite, as a hand's
	// retry is checked before its hold: rerun, it would be refused inside
	// its loop and parked at attempt 2, its one rerun spent on nothing. Its
	// row stays as the sweep left it, for a hand or the next open. A
	// function body that runs an agent meets the cap inside the body, as a
	// dispatched delivery does.
	if tr.Agent != nil {
		if ctx, err = ds.refuseAtSpendCap(ctx, tr.Agent); err != nil {
			if errors.Is(err, errSpendHeld) {
				skipped(rerunOutcomeCapped)
			} else {
				skipped(webhookFireOutcome(err), "error", err)
			}
			return
		}
	}
	f, err := ds.markRerun(ctx, triggerID, failureID)
	if err != nil {
		skipped(webhookFireOutcome(err), "error", err)
		return
	}
	if f == nil {
		ds.svc.log.Info("substrate: agent delivery a restart interrupted was not rerun, a hand ended it",
			append(attrs, "outcome", fireOutcomeRetired)...)
		return
	}
	// f is the row as it stood, at attempt 1: a rerun that fails parks at
	// attempt 2, the attempt the rewrite already names.
	_, err = ds.retryHeldFailure(ctx, tr, *f, settle)
	outcome := rerunOutcomeOK
	switch {
	case err == nil:
	case errors.Is(err, substrate.ErrParked):
		outcome = rerunOutcomeParked
	default:
		outcome = webhookFireOutcome(err)
	}
	attrs = append(attrs, "outcome", outcome)
	if outcome == rerunOutcomeOK {
		ds.svc.log.Info("substrate: reran an agent delivery a restart interrupted", attrs...)
		return
	}
	ds.svc.log.Warn("substrate: reran an agent delivery a restart interrupted, it stays parked for a hand", attrs...)
}

// markRerun rewrites an interrupted agent delivery's row before its rerun
// runs: attempt 2 and rerunAgentError, on a delivery entry of its own, so a
// stop during the rerun leaves a row the next open neither rewrites nor
// reruns. It returns the row as it stood, or nil when the row is gone or no
// longer an interrupted run at attempt 1 (a hand retried or forgot it before
// the claim was taken).
func (ds *dataset) markRerun(ctx context.Context, triggerID string, failureID int64) (*foldFailure, error) {
	var before *foldFailure
	err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		before = nil
		if err := t.lockFailure(triggerID, failureID); err != nil {
			if errors.Is(err, errFailureRetired) {
				return nil
			}
			return err
		}
		f, err := scanFailure(t.row(failureRowSQL, failureID, triggerID), triggerID, failureID)
		if err != nil {
			return err
		}
		if f.LastError != interruptedAgentError || f.Attempts != 1 {
			return nil
		}
		rerun := f
		rerun.LastError = rerunAgentError
		rerun.Attempts = 2
		rerun.ParkedAt = t.now
		if err := t.parkTx(triggerID, rerun); err != nil {
			return err
		}
		if err := t.settleDelivery(triggerID); err != nil {
			return err
		}
		before = &f
		return nil
	})
	if err != nil {
		return nil, err
	}
	return before, nil
}

// lostThreadReason is the reason a thread settles with when the run that
// held it ended inside a live process without settling it: a panic, a
// canceled context, a write that failed on the way out.
const lostThreadReason = "interrupted: the agent run holding this thread stopped without settling it, and its lease expired at "

// settleLostThreads settles a `running` thread whose lease expired and whose
// loop this process does not run (runningThreads) to `error`, naming the
// lost run. The lease is the loop's deadline plus slack (agentloop.go
// leaseUntil), so no live loop outlives it. Only the thread settles: a
// claim the run left already lists as interrupted (presentFailure), and the
// sweep reruns nothing (decision 0064). The next open parks that claim as
// interrupted, and its first pass reruns it once (decision 0151).
func (ds *dataset) settleLostThreads(ctx context.Context) error {
	if ds.svc.readOnly {
		return nil
	}
	rows, err := ds.db.QueryContext(ctx, `
		SELECT id, COALESCE(props->>'leaseUntil', '') FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND props->>'status' = $2
		ORDER BY id`, typeThread, threadRunning)
	if err != nil {
		return err
	}
	var ids []string
	now := nowUTC()
	for rows.Next() {
		var id, lease string
		if err := rows.Scan(&id, &lease); err != nil {
			_ = rows.Close()
			return err
		}
		if leaseExpired(lease, now) {
			ids = append(ids, id)
		}
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if _, live := ds.runningThreads.Load(id); live {
			continue
		}
		var settled bool
		err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
			settled = false
			if err := t.lockRegistryDepShared(); err != nil {
				return err
			}
			ref := eref{Kind: typeThread, ID: id}
			if err := t.lockRecord(ref); err != nil {
				return err
			}
			// Re-read under the row lock: a continuation may have claimed
			// the thread with a fresh lease since the scan.
			row, err := t.loadRow(ref, true)
			if err != nil || row == nil || row.DeletedAt != nil {
				return err
			}
			status, _ := row.Props["status"].(string)
			lease, _ := row.Props["leaseUntil"].(string)
			if status != threadRunning || !leaseExpired(lease, t.now) {
				return nil
			}
			reason := lostThreadReason + lease
			if lease == "" {
				reason = lostThreadReason + "an unknown time"
			}
			if _, err := t.patch(ref, substrate.PatchInput{Properties: map[string]any{
				"status":     threadError,
				"reason":     reason,
				"finishedAt": t.now.Format(time.RFC3339Nano),
			}}); err != nil {
				return err
			}
			settled = true
			return nil
		})
		if err != nil {
			return fmt.Errorf("settle lost thread %s: %w", id, err)
		}
		if settled {
			ds.svc.log.Warn("substrate: agent thread lost its run, marked error", "thread", id)
		}
	}
	return nil
}

// leaseExpired reports whether a thread's `leaseUntil` lies before now. A
// missing or unreadable lease is expired, as claimThread reads it.
func leaseExpired(lease string, now time.Time) bool {
	at, err := time.Parse(time.RFC3339Nano, lease)
	return err != nil || !now.Before(at)
}

// settleInterruptedThreads settles every `running` thread to `error`.
func (ds *dataset) settleInterruptedThreads(ctx context.Context) error {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT id FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND props->>'status' = $2
		ORDER BY id`, typeThread, threadRunning)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
			_, err := t.patch(eref{Kind: typeThread, ID: id}, substrate.PatchInput{Properties: map[string]any{
				"status":     threadError,
				"reason":     interruptedThreadReason,
				"finishedAt": t.now.Format(time.RFC3339Nano),
			}})
			return err
		})
		if err != nil {
			return fmt.Errorf("settle interrupted thread %s: %w", id, err)
		}
		ds.svc.log.Warn("substrate: agent thread interrupted by a stop, marked error", "thread", id)
	}
	return nil
}
