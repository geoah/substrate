package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The engine's agent plumbing around the loop (agentloop.go): the schema-row
// projection, the well-known llmprovider row, and the two direct entry points
// — the call API and chat. Trigger dispatch enters through functions.go's
// deliver/deliverFire, which branch on the trigger's callable kind.

// groupAgentDeclarations renders one authority's agent declarations as projected
// rows — schemawrite.go's one-line seam, kept with the kind it projects.
func groupAgentDeclarations(g *vocabulary.Authority, add func(short, typeIdent, id string, props map[string]any)) error {
	for _, n := range g.AgentOrder {
		a := g.Agents[n]
		def, err := jsonSafe(a.Definition)
		if err != nil {
			return err
		}
		// The first-class columns (core.substrate.reamde.dev/agent) are the projected
		// summary of the manifest; `definition` stays authoritative.
		// functions lists the CALLABLE tools (built-ins query/propose are not
		// records and stay in `definition`); subagents mirrors the sub-agent
		// identities.
		var functions []any
		for _, t := range a.Tools {
			if t.Callable != "" {
				functions = append(functions, t.Callable)
			}
		}
		subagents := make([]any, 0, len(a.Agents))
		for _, ident := range a.Agents {
			subagents = append(subagents, ident)
		}
		props := map[string]any{
			"name": a.Name, "authority": a.Authority,
			"description": a.Description, "prompt": a.Prompt,
			"provider": a.Provider, "model": a.Model,
			"definition": def, "sourceYAML": nil,
		}
		if len(functions) > 0 {
			props["functions"] = functions
		}
		if len(subagents) > 0 {
			props["subagents"] = subagents
		}
		if a.SubagentOnly {
			// Projected only when set, like the lists above: the console's
			// chat list filters on it, and an absent key reads as chattable.
			props["subagentOnly"] = true
		}
		add(vocabulary.DocAgent, kindAgent, a.Identity(), props)
	}
	return nil
}

// deliverToAgent runs one record delivery through the loop (deliver()'s
// agent branch, after the guard passed): the envelope becomes the first user
// message, the loop's writes land incrementally under the agent's actor with
// caused_by stamped, and the cursor advances AFTER the loop settles —
// compare-and-swap, so a concurrent dispatcher's duplicate rolls its cursor
// motion back, but agent deliveries are at-least-once by construction (the
// loop's writes are not one transaction; a retried delivery is a new
// thread). A loop error rides the ordinary retries and parks.
func (ds *dataset) deliverToAgent(ctx context.Context, tr *trigger, ch substrate.Change, from int64, depth int, envelope map[string]any, mode string) (deliverResult, error) {
	var res deliverResult
	advance := from >= 0
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
	})
	if err != nil {
		return res, err
	}
	if advance {
		if err := ds.advanceCursor(ctx, tr.ID, from, ch.Seq); err != nil {
			return res, err
		}
		res.moved = true
	}
	res.effects = ares.EffectsByAction
	if ares.Effects > 0 {
		res.ran = 1
	}
	return res, nil
}

// agentFire runs one schedule/webhook fire through the loop — deliverFire's
// agent branch. The fire-state CAS lands after the loop settles: a lost swap
// means a concurrent dispatcher fired the occurrence too, and this one's
// writes stand as at-least-once duplicates (function fires stay
// effectively-once; agent fires are at-least-once by construction).
func (ds *dataset) agentFire(ctx context.Context, tr *trigger, mode, fid string, at time.Time, lastFire *time.Time) (map[string]int, int, error) {
	// Admission under the lifecycle fence, held through the loop's writes and
	// the fire-state CAS below (bundles.go, review #2).
	ctx, release, err := ds.admitCallable(ctx, tr.Agent.Authority, tr.Agent.Identity())
	if err != nil {
		return nil, 0, err
	}
	defer release()
	user, err := json.Marshal(runner.FireEnvelope(fid, at, ds.Repository().Name))
	if err != nil {
		return nil, 0, err
	}
	ares, err := ds.runAgent(ctx, tr.Agent, agentInvocation{
		mode: mode, user: string(user),
		// Stable per occurrence: a retried fire reuses the fire id, so tool
		// keys survive the retry (the functionFire key shape).
		delivery: fmt.Sprintf("%s/%s/%s", ds.Repository().Name, tr.ID, fid),
	})
	if err != nil {
		return nil, 0, err
	}
	if lastFire != nil {
		err = ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
			return t.advanceScheduleTx(tr.ID, *lastFire, at)
		})
		if err != nil {
			return nil, 0, err
		}
	}
	applied := 0
	if ares.Effects > 0 {
		applied = 1
	}
	return ares.EffectsByAction, applied, nil
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
	ag, err := ds.registry().ResolveAgent(name)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", substrate.ErrNotFound, err)
	}
	// Admission under the lifecycle fence, held through the whole loop's writes
	// (thread, every message, settlement) — a disable draining this call waits
	// for the thread to settle before it returns (bundles.go, review #2).
	ctx, release, err := ds.admitCallable(ctx, ag.Authority, ag.Identity())
	if err != nil {
		return nil, err
	}
	defer release()
	user, err := agentUserContent(input)
	if err != nil {
		return nil, err
	}
	res, err := ds.runAgent(ctx, ag, agentInvocation{mode: "call", user: user})
	if err != nil {
		return nil, agentEntryError(err)
	}
	return res, nil
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
	if ag.SubagentOnly {
		// The declaration's own word: a subagent-only agent (an llm-as-judge)
		// exists to be called by other agents, and the chat surface says so
		// instead of opening a thread the console would never have offered.
		return nil, fmt.Errorf("%w: agent %s is subagent-only: call it from another agent, or drop data.subagentOnly from its declaration",
			substrate.ErrValidation, ag.Identity())
	}
	// Admission under the lifecycle fence, held through the whole chat turn's
	// writes (thread claim/mint, every message, settlement) — review #2.
	ctx, release, err := ds.admitCallable(ctx, ag.Authority, ag.Identity())
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
