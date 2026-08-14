package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// THE ONE LOOP (primitives §5, ticket 007 ruling 3). Every agent invocation
// — a trigger delivery, the call API, a sub-agent call, an interactive chat
// turn — runs this loop: completions through internal/llm against the
// provider row's wire, tool schemas from function manifests, tool calls dispatched
// through the existing callable machinery with effects held to the AGENT's
// emit and applied under `function:<name>`, and the transcript + tally
// written as thread/message records AS THE LOOP RUNS. Streaming and mid-run
// input are transport concerns of the API surface (api/agents.go), never a
// second runtime. Sub-agents are child invocations: their OWN budgets, an
// `agentDepth` counter separate from causal depth (cap ≤ 3), a child thread
// naming its caller in `parent`, and a token/cost tally that rolls up onto the
// ROOT thread. The loop terminates on a final tool-free reply, any budget,
// or its deadline — over-budget is a SETTLED outcome, not a failure.

// The core data kinds the loop reads and writes. The agent runtime is part of
// the substrate, so its vocabulary is core's like the rest of the machinery —
// the `llmprovider`/`llmthread`/`llmmessage` names carry the subsystem instead of a
// separate authority (the former `ai.substrate.reamde.dev`, folded in 2026-08-12), and
// the `agent` kind was always core's.
const (
	typeProvider  = "core.substrate.reamde.dev/llmprovider"
	typeThread    = "core.substrate.reamde.dev/llmthread"
	typeMessage   = "core.substrate.reamde.dev/llmmessage"
	msgRelThread  = "thread"
	threadRelPare = "parent"
)

// Thread statuses. `running` settles to exactly one of the other three.
const (
	threadRunning    = "running"
	threadOK         = "ok"
	threadOverBudget = "overbudget"
	threadError      = "error"
)

// The two modes only the loop mints (the rest are the runner's).
const (
	agentModeChat     = "chat"
	agentModeSubagent = "subagent"
)

// listDefaultFirst bounds a query tool's list/search page when the model
// asks for nothing specific.
const listDefaultFirst = 20

// agentInvocation is one loop entry: who is asked what, under which
// delivery, how deep, and where the events stream.
type agentInvocation struct {
	mode string
	// user is the first user message: the delivery envelope (JSON) on
	// triggered runs, the human's text on chat, the caller's input on call
	// and sub-agent invocations.
	user string
	// userActor attributes the user message row; empty means the agent's
	// own actor (triggered runs — the envelope is the machinery talking).
	userActor substrate.Actor
	// threadID continues an existing thread (chat); empty mints one.
	threadID string
	// parent is the calling agent's thread, on sub-agent invocations.
	parent string
	// depth is the agentDepth: 0 at the root, +1 per sub-agent hop. A
	// separate counter from causalDepth on purpose.
	depth int
	// causedBy stamps every row the loop writes; 0 on direct invocations.
	causedBy int64
	// causalDepth rides into function-tool sub-calls so the changelog chain
	// cap keeps holding through an agent hop.
	causalDepth int
	// emit streams loop events; nil disables streaming.
	emit func(substrate.AgentEvent)
	// tally is the shared root roll-up; nil at the root.
	tally *agentTally
	// emitCeiling carries the CALLER chain's effective emit set into a
	// sub-agent invocation; ceilinged distinguishes an empty ceiling (the
	// caller may write NOTHING, so neither may any child) from the root's
	// absent one. Every child hop intersects, so no delegation can widen
	// what the chain's narrowest ancestor allowed.
	emitCeiling []string
	ceilinged   bool
	// delivery is the stable delivery identity (repository/trigger/seq or fire
	// id; per-call ids for direct invocations): tool idempotency keys derive
	// from it plus the call path and ordinal, so a RETRIED delivery hands an
	// external deduper the same keys. Thread ids are trace-only.
	delivery string
}

// agentTally is the root roll-up: one number answers "what did this chain
// cost", so every loop on the chain adds to the same tally.
type agentTally struct {
	prompt     int
	completion int
	cost       float64
	effects    map[string]int
}

func (t *agentTally) effectsTotal() int {
	n := 0
	for _, c := range t.effects {
		n += c
	}
	return n
}

// providerConfig is one llmprovider row resolved against the host's gateway
// fallbacks: WHERE completions are bought. The apiKey property is
// secret-typed, so this reads the RAW row — the redacting read surface never
// sees the key.
type providerConfig struct {
	id       string
	wire     llm.Wire
	cfg      llm.Config
	defaults map[string]any
	// pricing is USD per 1M tokens keyed by model id; a model absent from the
	// table costs 0 and only the token tally is authoritative.
	pricing map[string]modelPrice
}

type modelPrice struct {
	inPer1M  float64
	outPer1M float64
}

func (ds *dataset) resolveProvider(ctx context.Context, id string) (*providerConfig, error) {
	row, err := ds.loadRowDB(ctx, eref{Kind: typeProvider, ID: id})
	if err != nil {
		return nil, err
	}
	if row == nil || row.DeletedAt != nil || row.Kind != typeProvider {
		// Nothing seeds a provider: a repository holds none until its owner
		// writes one, so the row an agent names is ABSENT rather than
		// misconfigured — and the message says which, and what to do.
		return nil, fmt.Errorf("%w: llmprovider row %q does not resolve — create it: a row carries the wire, the endpoint and the key, and the llm example bundle ships two ready to key",
			substrate.ErrValidation, id)
	}
	pc := &providerConfig{id: id, pricing: map[string]modelPrice{}}
	wire, _ := row.Props["wire"].(string)
	pc.wire = llm.Wire(wire)
	pc.cfg.BaseURL, _ = row.Props["baseURL"].(string)
	// apiKey is secret-typed: the stored value is a ref into the sealed
	// store, resolved here, at the one read that pays with it.
	storedKey, _ := row.Props["apiKey"].(string)
	pc.cfg.APIKey, err = ds.openSecretValue(ctx, storedKey)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: open llmprovider %q apiKey: %w", id, err)
	}
	// headers and pricing are REPEATED OBJECTS: the substrate has no map
	// datatype, so a keyed table is a list whose key is a declared field
	// (`name`, `model`). Coercion has already checked the fields; a later row
	// for the same key wins, which is what a table means.
	for _, entry := range objectRows(row.Props["headers"]) {
		name, _ := entry["name"].(string)
		if name == "" {
			continue
		}
		if pc.cfg.Headers == nil {
			pc.cfg.Headers = map[string]string{}
		}
		pc.cfg.Headers[name] = fmt.Sprint(entry["value"])
	}
	pc.defaults, _ = row.Props["defaults"].(map[string]any)
	for _, entry := range objectRows(row.Props["pricing"]) {
		model, _ := entry["model"].(string)
		if model == "" {
			continue
		}
		in, _ := anyFloat(entry["inputPer1M"])
		out, _ := anyFloat(entry["outputPer1M"])
		pc.pricing[model] = modelPrice{inPer1M: in, outPer1M: out}
	}

	// What each wire needs from a row is internal/llm's own fact (llm.Wire.Policy);
	// what the engine owns is the host-gateway pairing and the wording.
	policy, known := pc.wire.Policy()
	if !known {
		if pc.wire == "" {
			return nil, fmt.Errorf("%w: llmprovider row %q declares no wire — one of %s",
				substrate.ErrValidation, id, llm.WireNames())
		}
		return nil, fmt.Errorf("%w: llmprovider row %q declares wire %q — one of %s",
			substrate.ErrValidation, id, wire, llm.WireNames())
	}
	// The host gateway fallbacks are ONE unit: the host key travels ONLY to
	// the host URL. A row that selects its own baseURL must carry its own
	// apiKey — falling back independently would send the host-wide gateway
	// bearer to an arbitrary repository-chosen endpoint.
	if policy.HostGatewayFallback && pc.cfg.BaseURL == "" {
		pc.cfg.BaseURL = ds.svc.llmBaseURL
		if pc.cfg.APIKey == "" {
			pc.cfg.APIKey = ds.svc.llmAPIKey
		}
		if pc.cfg.BaseURL == "" {
			return nil, fmt.Errorf("%w: llmprovider row %q has no baseURL and the host has no LLM gateway configured",
				substrate.ErrValidation, id)
		}
	}
	if policy.RequiresBaseURL && pc.cfg.BaseURL == "" {
		return nil, fmt.Errorf("%w: llmprovider row %q declares no baseURL — the %s wire has no host default, so the row names its own endpoint",
			substrate.ErrValidation, id, pc.wire)
	}
	if policy.RequiresAPIKey && pc.cfg.APIKey == "" {
		return nil, fmt.Errorf("%w: llmprovider row %q declares no apiKey — the host gateway key travels to the host's own gateway and nowhere else",
			substrate.ErrValidation, id)
	}
	return pc, nil
}

// mergeParams folds the provider row's defaults UNDER the agent's own params —
// the agent wins on every key it names — and runs the result through the
// LOADER's own validator, so a provider row is held to exactly the rules an
// agent manifest is and neither can carry a knob the loop would drop.
func mergeParams(providerID string, defaults, own map[string]any) (llm.Params, error) {
	// Sized to one half: a summed capacity hint is an overflowable
	// allocation size to the scanner, and the other half grows the map.
	merged := make(map[string]any, len(defaults))
	for k, v := range defaults {
		merged[k] = v
	}
	for k, v := range own {
		merged[k] = v
	}
	p, err := vocabulary.ParseAgentParams(merged)
	if err != nil {
		// The agent's own params were validated at LOAD, so whatever the
		// merged map fails on came from the row's defaults — and the row id is
		// the only thing in the message that finds it again.
		return llm.Params{}, fmt.Errorf("%w: llmprovider row %q: defaults.%w",
			substrate.ErrValidation, providerID, err)
	}
	return llm.Params{Temperature: p.Temperature, MaxTokens: p.MaxTokens}, nil
}

// objectRows reads a repeated-object property's stored value: a list of
// mappings, each already coerced against the declared fields. Anything else
// (an absent property, a legacy map from before the shape was declared) reads
// as no rows rather than a panic — the boot upgrade is what refuses the old
// shape, not a dispatch.
func objectRows(v any) []map[string]any {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if entry, ok := item.(map[string]any); ok {
			out = append(out, entry)
		}
	}
	return out
}

func anyFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

// agentTool is one dispatchable tool: a sub-agent, or a function — and on a
// function, `builtin` names which host implementation runs it. A host function's
// fn is the DECLARATION (the card the model was shown); its body is this loop.
type agentTool struct {
	builtin string
	fn      *vocabulary.Function
	sub     *vocabulary.Agent
}

// agentLoop is one running invocation's state.
type agentLoop struct {
	ds *dataset
	ag *vocabulary.Agent
	// provider is WHERE this run buys completions; model is WHAT it asks for,
	// the agent's own word, sent verbatim.
	provider *providerConfig
	model    string
	params   llm.Params
	client   llm.Client
	in       agentInvocation
	actor    substrate.Actor

	threadID string
	turn     int // next message ordinal on the thread

	// emit is the EFFECTIVE emit set: the agent's own, intersected with the
	// inherited ceiling on sub-agent invocations. Every write gate in the
	// loop — function-tool effects and propose — holds to this, never to the
	// agent's declared emit alone.
	emit []string

	defs   []llm.Tool
	byName map[string]agentTool

	// own counters (the thread row's); the tally aggregates across the chain
	turns      int
	toolCalls  int
	prompt     int
	completion int
	cost       float64
	readCalls  int
	readRows   int
}

// runAgent is the loop's one entry. Everything the loop writes lands
// incrementally — thread first, each turn as it settles — so a crash or a
// budget leaves a readable trace; the delivery machinery around it stays
// at-least-once (a retried delivery is a fresh thread).
func (ds *dataset) runAgent(ctx context.Context, ag *vocabulary.Agent, in agentInvocation) (*substrate.AgentResult, error) {
	provider, err := ds.resolveProvider(ctx, ag.Provider)
	if err != nil {
		return nil, err
	}
	params, err := mergeParams(provider.id, provider.defaults, ag.Params)
	if err != nil {
		return nil, err
	}
	client, err := llm.New(provider.wire, provider.cfg)
	if err != nil {
		return nil, fmt.Errorf("%w: agent %s: %w", substrate.ErrValidation, ag.Identity(), err)
	}
	if in.tally == nil {
		in.tally = &agentTally{effects: map[string]int{}}
	}
	if in.delivery == "" {
		// Direct invocations mint a per-call identity — two manual runs are
		// two operations to any external deduper (the CallFunction precedent).
		id, err := newID()
		if err != nil {
			return nil, err
		}
		in.delivery = fmt.Sprintf("%s/agentrun/%s", ds.Repository().Name, id)
	}
	l := &agentLoop{
		ds: ds, ag: ag, provider: provider, model: ag.Model, params: params, in: in,
		client: client,
		actor:  substrate.Actor(ag.Actor()),
		emit:   effectiveEmit(ag.Emit, in.emitCeiling, in.ceilinged),
	}
	if err := l.buildTools(); err != nil {
		return nil, err
	}
	messages, err := l.openThread(ctx)
	if err != nil {
		return nil, err
	}
	l.event(substrate.AgentEvent{Kind: substrate.AgentEventThread, Thread: l.threadID})

	deadline := nowUTC().Add(time.Duration(ag.Budgets.DeadlineSeconds) * time.Second)
	lctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	status, reason, reply := threadOverBudget, "", ""
loop:
	for l.turns < ag.Budgets.MaxTurns {
		if !nowUTC().Before(deadline) {
			reason = "deadline exceeded"
			break
		}
		l.turns++
		res, err := l.complete(lctx, messages)
		if err != nil {
			serr := l.settle(ctx, threadError, err.Error(), reply)
			return nil, errors.Join(fmt.Errorf("agent %s: llm: %w", l.ag.Identity(), err), serr)
		}
		content, calls, usage := res.Content, res.ToolCalls, res.Usage
		if usage != nil {
			price := l.provider.pricing[l.model]
			l.prompt += usage.PromptTokens
			l.completion += usage.CompletionTokens
			turnCost := (float64(usage.PromptTokens)*price.inPer1M + float64(usage.CompletionTokens)*price.outPer1M) / 1e6
			l.cost += turnCost
			l.in.tally.prompt += usage.PromptTokens
			l.in.tally.completion += usage.CompletionTokens
			l.in.tally.cost += turnCost
		}
		if len(calls) == 0 {
			// The final tool-free reply is the loop's natural end.
			if err := l.putMessage(ctx, l.actor, map[string]any{"role": "assistant", "content": content}); err != nil {
				return nil, err
			}
			status, reply = threadOK, content
			break
		}
		callsJSON := make([]any, 0, len(calls))
		for _, tc := range calls {
			callsJSON = append(callsJSON, map[string]any{"id": tc.ID, "name": tc.Name, "arguments": tc.Arguments})
		}
		props := map[string]any{"role": "assistant", "toolCalls": callsJSON}
		if content != "" {
			props["content"] = content
		}
		if err := l.putMessage(ctx, l.actor, props); err != nil {
			return nil, err
		}
		messages = append(messages, llm.Message{
			Role: llm.RoleAssistant, Content: content, ToolCalls: calls,
		})
		for _, tc := range calls {
			var out string
			var ok bool
			if l.toolCalls >= l.ag.Budgets.MaxToolCalls {
				// The v4 lesson: refuse with a synthetic result the model
				// SEES, so it can land a final reply instead of a silent
				// truncation. MaxTurns and the deadline still bound a model
				// that ignores the signal.
				out, ok = toolError(fmt.Sprintf("tool-call budget exhausted (max %d) — stop calling tools and write your final reply", l.ag.Budgets.MaxToolCalls)), false
				if reason == "" {
					reason = fmt.Sprintf("max tool calls reached (%d)", l.ag.Budgets.MaxToolCalls)
				}
			} else {
				l.toolCalls++
				l.toolEvent(substrate.AgentEventToolStarted, tc, nil, "")
				out, ok = l.dispatch(lctx, tc)
			}
			l.toolEvent(substrate.AgentEventToolFinished, tc, &ok, out)
			// `ok` is stored, not inferred: a replayed transcript otherwise has
			// to sniff the failure envelope out of the payload, and a
			// successful result carrying an `error` key would counterfeit it.
			if err := l.putMessage(ctx, l.actor, map[string]any{
				"role": "tool", "content": out, "toolCallId": tc.ID, "tool": tc.Name, "ok": ok,
			}); err != nil {
				return nil, err
			}
			messages = append(messages, llm.Message{
				Role: llm.RoleTool, ToolCallID: tc.ID, ToolName: tc.Name, Content: out,
			})
		}
		if lctx.Err() != nil {
			reason = "deadline exceeded"
			break loop
		}
	}
	if status == threadOverBudget && reason == "" {
		reason = fmt.Sprintf("max turns reached (%d)", l.ag.Budgets.MaxTurns)
	}
	if err := l.settle(ctx, status, reason, reply); err != nil {
		return nil, err
	}
	res := &substrate.AgentResult{
		Reply: reply, Thread: l.threadID, Status: status, Reason: reason,
		Turns: l.turns, ToolCalls: l.toolCalls,
		Effects: l.in.tally.effectsTotal(),
	}
	// The result's tally is the ROOT roll-up when this loop is the root, its
	// own spend when it is a sub-agent (the parent keeps rolling).
	if l.in.parent == "" && l.in.mode != agentModeSubagent {
		res.PromptTokens, res.CompletionTokens = l.in.tally.prompt, l.in.tally.completion
		res.CostUSD = l.in.tally.cost
		res.EffectsByAction = l.in.tally.effects
	} else {
		res.PromptTokens, res.CompletionTokens = l.prompt, l.completion
		res.CostUSD = l.cost
	}
	res.TotalTokens = res.PromptTokens + res.CompletionTokens
	l.event(substrate.AgentEvent{Kind: substrate.AgentEventDone, Result: res})
	return res, nil
}

// effectiveEmit intersects an agent's declared emit with the inherited
// ceiling: a sub-agent may never write a type its calling chain could not.
func effectiveEmit(own, ceiling []string, ceilinged bool) []string {
	if !ceilinged {
		return own
	}
	allowed := map[string]bool{}
	for _, t := range ceiling {
		allowed[t] = true
	}
	out := make([]string, 0, len(own))
	for _, t := range own {
		if allowed[t] {
			out = append(out, t)
		}
	}
	return out
}

// emitAllows holds a type to the loop's EFFECTIVE emit set.
func (l *agentLoop) emitAllows(ident string) bool {
	for _, t := range l.emit {
		if t == ident {
			return true
		}
	}
	return false
}

func (l *agentLoop) event(ev substrate.AgentEvent) {
	if l.in.emit != nil {
		l.in.emit(ev)
	}
}

// toolEvent emits one side of a dispatch's lifecycle. The call ID rides both
// sides: a turn may dispatch one tool several times, so a client pairing by
// NAME settles the wrong card.
func (l *agentLoop) toolEvent(kind string, tc llm.ToolCall, ok *bool, out string) {
	if l.in.emit == nil {
		return
	}
	ev := substrate.AgentEvent{Kind: kind, ID: tc.ID, Tool: tc.Name, OK: ok}
	if kind == substrate.AgentEventToolStarted {
		ev.Args = tc.Arguments
	} else {
		ev.Output = out
	}
	l.in.emit(ev)
}

// --- thread + message rows -------------------------------------------------------

// openThread mints (or continues) the thread and returns the chat history —
// prior user/assistant turns on a continued thread, and the new user message,
// whose row lands before the first completion. The system prompt is not a
// message: it rides llm.Request.System, and each wire places it.
func (l *agentLoop) openThread(ctx context.Context) ([]llm.Message, error) {
	var messages []llm.Message
	if l.in.threadID != "" {
		// The lease FIRST: a second active turn is rejected before anything
		// lands on the thread.
		if err := l.claimThread(ctx); err != nil {
			return nil, err
		}
		history, maxTurn, err := l.loadHistory(ctx)
		if err != nil {
			return nil, err
		}
		messages = append(messages, history...)
		l.turn = maxTurn + 1
	} else {
		id, err := newID()
		if err != nil {
			return nil, err
		}
		l.threadID = id
		props := map[string]any{
			// `agent` and `parent` are REFERENCES: a bare id, which each `kind:` pin
			// resolves to the kind it pins, the same shorthand a single-target
			// edge took.
			"agent": l.ag.Identity(), "provider": l.provider.id, "model": l.model, "mode": l.in.mode,
			"status": threadRunning, "agentDepth": l.in.depth,
			"startedAt":  nowUTC().Format(time.RFC3339Nano),
			"leaseUntil": l.leaseUntil().Format(time.RFC3339Nano),
		}
		if l.in.parent != "" {
			props[threadRelPare] = l.in.parent
		}
		in := substrate.PutInput{Kind: typeThread, ID: id, Properties: props}
		if err := l.putRow(ctx, l.actor, in); err != nil {
			return nil, err
		}
	}
	userActor := l.in.userActor
	if userActor == "" {
		userActor = l.actor
	}
	if err := l.putMessage(ctx, userActor, map[string]any{"role": "user", "content": l.in.user}); err != nil {
		return nil, err
	}
	messages = append(messages, llm.Message{Role: llm.RoleUser, Content: l.in.user})
	return messages, nil
}

// agentLeaseSlack pads the turn lease past the loop deadline, so only a
// crashed continuation's lease ever expires while its loop could still run.
const agentLeaseSlack = 30 * time.Second

func (l *agentLoop) leaseUntil() time.Time {
	return nowUTC().Add(time.Duration(l.ag.Budgets.DeadlineSeconds)*time.Second + agentLeaseSlack)
}

// claimThread takes the per-thread turn lease for a CONTINUED thread: the
// whole continuation runs under it, and a second concurrent turn is rejected
// — never interleaved. The row lock serializes two claimants; the loser sees
// `running` with a live lease and refuses. A stale lease (a crashed
// continuation past its deadline plus slack) is taken over.
func (l *agentLoop) claimThread(ctx context.Context) error {
	return l.ds.inTx(ctx, l.actor, false, func(t *txn) error {
		t.causedBy = l.in.causedBy
		if err := t.lockRecord(eref{Kind: typeThread, ID: l.in.threadID}); err != nil {
			return err
		}
		row, err := t.loadRow(eref{Kind: typeThread, ID: l.in.threadID}, true)
		if err != nil {
			return err
		}
		if row == nil || row.DeletedAt != nil || row.Kind != typeThread {
			return fmt.Errorf("%w: thread %s", substrate.ErrNotFound, l.in.threadID)
		}
		// `agent` is a reference — a {kind, id} pair — so the ownership check
		// reads the id it names, not the value's spelling.
		if agent := referenceID(row.Props["agent"]); agent != l.ag.Identity() {
			return fmt.Errorf("%w: thread %s belongs to agent %q — open a new thread", substrate.ErrValidation, l.in.threadID, agent)
		}
		if status, _ := row.Props["status"].(string); status == threadRunning {
			if until, _ := row.Props["leaseUntil"].(string); until != "" {
				if at, perr := time.Parse(time.RFC3339Nano, until); perr == nil && nowUTC().Before(at) {
					return fmt.Errorf("%w: thread %s already has a running turn — wait for it to settle", substrate.ErrConflict, l.in.threadID)
				}
			}
			// No lease, or an expired one: a crashed turn — take over.
		}
		l.threadID = l.in.threadID
		_, err = t.patch(eref{Kind: typeThread, ID: l.threadID}, substrate.PatchInput{Properties: map[string]any{
			"status": threadRunning, "leaseUntil": l.leaseUntil().Format(time.RFC3339Nano),
		}})
		return err
	})
}

// loadHistory rebuilds a continued thread's prose history: user and
// assistant turns in order. Tool exchanges are per-run artifacts — audit,
// not context — so the replay stays robust against tool renames.
func (l *agentLoop) loadHistory(ctx context.Context) ([]llm.Message, int, error) {
	probe, err := json.Marshal(map[string]any{
		msgRelThread: vocabulary.RecordPath(typeThread, l.threadID),
	})
	if err != nil {
		return nil, 0, err
	}
	// `thread` is a reference, so the transcript read is a containment probe
	// — which rides the repository-wide GIN index on props.
	rows, err := l.ds.db.QueryContext(ctx, `
		SELECT e.props FROM records e
		WHERE e.kind = $1 AND e.deleted_at IS NULL AND e.props @> $2::jsonb
		ORDER BY e.created_at, e.id`,
		typeMessage, string(probe))
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	var out []llm.Message
	maxTurn := -1
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, 0, err
		}
		var props map[string]any
		if err := json.Unmarshal(raw, &props); err != nil {
			return nil, 0, err
		}
		if t, ok := anyFloat(props["turn"]); ok && int(t) > maxTurn {
			maxTurn = int(t)
		}
		role, _ := props["role"].(string)
		content, _ := props["content"].(string)
		switch role {
		case "user":
			out = append(out, llm.Message{Role: llm.RoleUser, Content: content})
		case "assistant":
			if content != "" && props["toolCalls"] == nil {
				out = append(out, llm.Message{Role: llm.RoleAssistant, Content: content})
			}
		}
	}
	return out, maxTurn, rows.Err()
}

func (l *agentLoop) putMessage(ctx context.Context, actor substrate.Actor, props map[string]any) error {
	id, err := newID()
	if err != nil {
		return err
	}
	props["turn"] = l.turn
	l.turn++
	// The thread is a REFERENCE now: a bare id, which the `kind:` pin resolves, the same
	// shorthand a single-target edge took.
	props[msgRelThread] = l.threadID
	return l.putRow(ctx, actor, substrate.PutInput{
		Kind: typeMessage, ID: id, Properties: props,
	})
}

// settle writes the thread's final shape. Counters ACCUMULATE onto whatever
// the row already carries, so a continued chat thread keeps one running
// total; the root thread additionally absorbs the whole chain's tally (cost
// rolls up — children settled before their caller's loop continues). The
// read-and-add runs under the row lock in ONE transaction, so no concurrent
// settle can lose an increment.
func (l *agentLoop) settle(ctx context.Context, status, reason, reply string) error {
	prompt, completion, cost := l.prompt, l.completion, l.cost
	if l.in.parent == "" && l.in.mode != agentModeSubagent {
		prompt, completion, cost = l.in.tally.prompt, l.in.tally.completion, l.in.tally.cost
	}
	return l.ds.inTx(ctx, l.actor, false, func(t *txn) error {
		t.causedBy = l.in.causedBy
		if err := t.lockRecord(eref{Kind: typeThread, ID: l.threadID}); err != nil {
			return err
		}
		row, err := t.loadRow(eref{Kind: typeThread, ID: l.threadID}, true)
		if err != nil {
			return err
		}
		base := func(key string) int {
			if row == nil {
				return 0
			}
			v, _ := anyFloat(row.Props[key])
			return int(v)
		}
		baseCost := 0.0
		if row != nil {
			baseCost, _ = anyFloat(row.Props["costUSD"])
		}
		props := map[string]any{
			"status": status, "turns": base("turns") + l.turns,
			"toolCalls":    base("toolCalls") + l.toolCalls,
			"promptTokens": base("promptTokens") + prompt, "completionTokens": base("completionTokens") + completion,
			"totalTokens": base("totalTokens") + prompt + completion,
			"costUSD":     baseCost + cost,
			"finishedAt":  nowUTC().Format(time.RFC3339Nano),
		}
		if reason != "" {
			props["reason"] = reason
		}
		_, err = t.patch(eref{Kind: typeThread, ID: l.threadID}, substrate.PatchInput{Properties: props})
		return err
	})
}

func (l *agentLoop) putRow(ctx context.Context, actor substrate.Actor, in substrate.PutInput) error {
	return l.ds.inTx(ctx, actor, false, func(t *txn) error {
		t.causedBy = l.in.causedBy
		_, err := t.put(in)
		return err
	})
}

// --- tools ---------------------------------------------------------------------

// buildTools compiles the model-facing tool cards: one per `tools:` entry
// (description from the resolved function's declaration, alias from the agent's
// entry) plus one per sub-agent. Definition order is declaration order, so the
// on-wire schema stays stable across turns.
//
// THE BUILT-INS TAKE THE SAME PATH AS EVERY OTHER CALLABLE, because they are
// function records: their cards used to be Go literals here, and they are the
// `description` and the `arguments:` of the four `runtime: host` declarations
// core ships. What still differs is only WHERE the body lives — a host function
// dispatches in this loop, anything else goes to the runner — which is what
// agentTool.builtin carries.
func (l *agentLoop) buildTools() error {
	l.byName = map[string]agentTool{}
	add := func(name, description string, params any, t agentTool) {
		l.byName[name] = t
		l.defs = append(l.defs, llm.Tool{Name: name, Description: description, Parameters: params})
	}
	openObject := map[string]any{"type": "object"}
	for _, t := range l.ag.Tools {
		fn, err := l.ds.registry().ResolveFunction(t.Callable)
		if err != nil {
			return fmt.Errorf("%w: agent %s: tool %s: %w", substrate.ErrValidation, l.ag.Identity(), t.Name, err)
		}
		// A host function this build has no arm for would reach the runner and trip
		// the backstop mid-conversation. It can only happen by shipping a fifth
		// `runtime: host` declaration without wiring the loop, so it refuses here
		// instead — at the build, where the message can say which.
		if fn.IsHost() && t.Builtin == "" {
			return fmt.Errorf("%w: agent %s: tool %s: %s is a host function this build does not implement",
				substrate.ErrValidation, l.ag.Identity(), t.Name, fn.Identity())
		}
		description := t.Description
		if description == "" {
			description = fn.Description
		}
		var params any = openObject
		if fn.Input != nil {
			params = fn.Input
		}
		tool := agentTool{fn: fn, builtin: t.Builtin}
		add(t.Name, description, params, tool)
	}
	for _, ident := range l.ag.Agents {
		sub, err := l.ds.registry().ResolveAgent(ident)
		if err != nil {
			return fmt.Errorf("%w: agent %s: sub-agent %s: %w", substrate.ErrValidation, l.ag.Identity(), ident, err)
		}
		add(sub.Name, sub.Description,
			map[string]any{"type": "object", "properties": map[string]any{
				"input": map[string]any{"type": "string", "description": "the task for the sub-agent"},
			}, "required": []any{"input"}}, agentTool{sub: sub})
	}
	return nil
}

// toolError shapes a failure the model sees — the structured `error` key v4
// converged on; an empty tool result makes most models loop.
func toolError(msg string) string {
	out, _ := json.Marshal(map[string]any{"error": msg})
	return string(out)
}

func toolJSON(v any) string {
	out, err := json.Marshal(v)
	if err != nil {
		return toolError("marshal result: " + err.Error())
	}
	return string(out)
}

// dispatch runs one tool call. A tool failure is a RESULT the model sees and
// steers around, never a delivery failure — only the LLM transport itself
// fails the loop.
func (l *agentLoop) dispatch(ctx context.Context, tc llm.ToolCall) (string, bool) {
	tool, ok := l.byName[tc.Name]
	if !ok {
		return toolError("unknown tool: " + tc.Name), false
	}
	var args map[string]any
	if s := strings.TrimSpace(tc.Arguments); s != "" {
		if err := json.Unmarshal([]byte(s), &args); err != nil {
			return toolError("arguments are not a JSON object: " + err.Error()), false
		}
	}
	switch {
	case tool.builtin == vocabulary.AgentToolQuery:
		return l.dispatchQuery(ctx, args)
	case tool.builtin == vocabulary.AgentToolPropose:
		return l.dispatchPropose(ctx, args)
	case tool.builtin == vocabulary.AgentToolGraphQL:
		return l.dispatchGraphQL(ctx, args)
	case tool.builtin == vocabulary.AgentToolMutate:
		return l.dispatchMutate(ctx, args)
	case tool.sub != nil:
		return l.dispatchSubAgent(ctx, tool.sub, args)
	default:
		return l.dispatchFunction(ctx, tool.fn, args)
	}
}

// --- the query built-in ----------------------------------------------------------

// dispatchQuery is the capability-scoped read: the agent's `reads:` is the
// allowlist and the budget, enforced exactly like a function's — a get
// outside the allowlist answers like an absent id, list/search clamp to the
// remaining row budget, and a blown budget is a tool error the model sees.
//
// The loop owns the BUDGET; runQueryTool owns the reading. The split exists
// because `core.substrate.reamde.dev/query` is also callable directly, where the
// caller is a token that owns the whole repository and there is no loop to
// account against.
func (l *agentLoop) dispatchQuery(ctx context.Context, args map[string]any) (string, bool) {
	reads := l.ag.Reads
	if reads == nil {
		return toolError("query is not granted — the agent declares no reads"), false
	}
	if l.readCalls >= reads.Calls {
		return toolError(fmt.Sprintf("read budget exhausted (%d calls)", reads.Calls)), false
	}
	l.readCalls++
	remaining := reads.Rows - l.readRows
	if remaining <= 0 {
		return toolError(fmt.Sprintf("read budget exhausted (%d rows)", reads.Rows)), false
	}
	out, ok, rows := l.ds.runQueryTool(ctx, queryScope{kinds: reads.Kinds, rows: remaining}, args)
	l.readRows += rows
	return out, ok
}

// queryScope bounds one `query` call: the kind allowlist every read is held to,
// and the rows it may still spend. A NIL allowlist is the whole repository — what
// a token holds, and never what an agent's `reads:` grants, which the loader
// refuses empty.
type queryScope struct {
	kinds []string
	rows  int
}

func (s queryScope) allows(ident string) bool {
	if s.kinds == nil {
		return true
	}
	for _, t := range s.kinds {
		if t == ident {
			return true
		}
	}
	return false
}

// runQueryTool executes one `query` call and reports the rows it read, so the
// caller's budget (the loop's, when there is one) stays the caller's business.
func (ds *dataset) runQueryTool(ctx context.Context, scope queryScope, args map[string]any) (string, bool, int) {
	if id, _ := args["id"].(string); id != "" {
		// A get names the FULL identity: type + id. The type is
		// checked against the allowlist BEFORE the read, and a disallowed or
		// unknown type answers exactly like an absent id — never an oracle.
		typ, _ := args["kind"].(string)
		if typ == "" {
			return toolError("get needs a kind — records are addressed by (kind, id)"), false, 0
		}
		if !scope.allows(typ) {
			if ty, err := ds.resolveType(typ); err != nil || !scope.allows(ty.Identity) {
				return toolJSON(map[string]any{"record": nil}), true, 1
			}
		}
		e, err := ds.Get(ctx, typ, id)
		if errors.Is(err, substrate.ErrNotFound) || errors.Is(err, substrate.ErrValidation) {
			return toolJSON(map[string]any{"record": nil}), true, 1
		}
		if err != nil {
			return toolError(err.Error()), false, 1
		}
		if !scope.allows(e.Kind) {
			// The uniform absence answer: a disallowed get is never an
			// existence oracle.
			return toolJSON(map[string]any{"record": nil}), true, 1
		}
		return toolJSON(map[string]any{"record": e}), true, 1
	}
	if q, _ := args["q"].(string); q != "" {
		in := substrate.SearchInput{Q: q}
		if k, ok := anyFloat(args["k"]); ok && int(k) > 0 {
			in.K = int(k)
		} else {
			in.K = listDefaultFirst
		}
		in.K = min(in.K, scope.rows)
		if types, _ := args["kinds"].([]any); len(types) > 0 {
			for _, tv := range types {
				ident := fmt.Sprint(tv)
				if !scope.allows(ident) {
					return toolError(ident + " is not in the reads allowlist"), false, 0
				}
				in.Kinds = append(in.Kinds, ident)
			}
		} else {
			in.Kinds = scope.kinds
		}
		hits, err := ds.Search(ctx, in)
		if err != nil {
			return toolError(err.Error()), false, 0
		}
		return toolJSON(map[string]any{"hits": hits}), true, len(hits)
	}
	ident, _ := args["kind"].(string)
	if ident == "" {
		return toolError("pass id, kind, or q"), false, 0
	}
	ty, err := ds.resolveType(ident)
	if err != nil {
		return toolError(err.Error()), false, 0
	}
	if !scope.allows(ty.Identity) {
		return toolError(ident + " is not in the reads allowlist"), false, 0
	}
	q := substrate.Query{Filter: substrate.Filter{Kinds: []string{ty.Identity}}, First: listDefaultFirst}
	if raw, ok := args["filter"].(map[string]any); ok {
		buf, _ := json.Marshal(raw)
		var f substrate.Filter
		if err := json.Unmarshal(buf, &f); err != nil {
			return toolError("filter: " + err.Error()), false, 0
		}
		f.Kinds = []string{ty.Identity}
		q.Filter = f
	}
	if first, ok := anyFloat(args["first"]); ok && int(first) > 0 {
		q.First = int(first)
	}
	q.First = min(q.First, scope.rows)
	page, err := ds.List(ctx, q)
	if err != nil {
		return toolError(err.Error()), false, 0
	}
	return toolJSON(map[string]any{"records": page.Records}), true, len(page.Records)
}

// --- the propose built-in ---------------------------------------------------------

// dispatchPropose lands one recordpatchrequest: the polite write. Gated by
// the agent's emit naming the request type (checked at load, held again
// here), applied under the agent's actor like every other effect. The op
// selects patch (change an existing target), create (mint a new record on
// accept) or delete (tombstone one) — a create proposal drops the
// target-must-exist guard. The diff is validated and normalised
// against the target type at PROPOSE time, so a malformed proposal
// (a wrapper-less diff, an immutable or unknown key) is a tool error the model
// sees and never reaches the inbox. The check is kept HERE, ahead of the write,
// only for that error: the write path runs the same normalizeDiff at admission
// (admitRequestDiff), so every other door is held to it too.
func (l *agentLoop) dispatchPropose(ctx context.Context, args map[string]any) (string, bool) {
	if !l.emitAllows(vocabulary.KindRecordPatchRequest) {
		return toolError(vocabulary.KindRecordPatchRequest + " is not in the agent's effective emit allowlist"), false
	}
	op, _ := args["op"].(string)
	if op == "" {
		op = opPatch
	}
	rationale, _ := args["rationale"].(string)
	diff, _ := args["diff"].(map[string]any)

	props := map[string]any{"op": op}
	if rationale != "" {
		props["rationale"] = rationale
	}
	var edges []substrate.EdgeInput

	switch op {
	case opPatch:
		target, _ := args["target"].(string)
		targetKind, _ := args["kind"].(string)
		if target == "" || targetKind == "" {
			return toolError("propose patch needs kind + target — records are addressed by (kind, id)"), false
		}
		ty, err := l.ds.resolveType(targetKind)
		if err != nil {
			return toolError(err.Error()), false
		}
		row, err := l.ds.loadRowDB(ctx, eref{Kind: ty.Identity, ID: target})
		if err != nil {
			return toolError(err.Error()), false
		}
		if row == nil || row.DeletedAt != nil {
			return toolError("target " + target + " does not exist"), false
		}
		norm, err := normalizeDiff(ty, diff, opPatch)
		if err != nil {
			return toolError(err.Error()), false
		}
		props["diff"] = norm
		edges = []substrate.EdgeInput{{Rel: "target", To: substrate.EdgeRef{Kind: row.Kind, ID: row.ID}}}

	case opCreate:
		typeIdent, _ := args["kind"].(string)
		newTargetID, _ := args["id"].(string)
		if typeIdent == "" || newTargetID == "" {
			return toolError("propose create needs kind (a kind reference) and id (a stable id for the new record)"), false
		}
		ty, err := l.ds.resolveType(typeIdent)
		if err != nil {
			return toolError(err.Error()), false
		}
		norm, err := normalizeDiff(ty, diff, opCreate)
		if err != nil {
			return toolError(err.Error()), false
		}
		props["diff"] = norm
		props["targetKind"] = ty.Identity
		props["targetId"] = newTargetID

	case opDelete:
		target, _ := args["target"].(string)
		targetKind, _ := args["kind"].(string)
		if target == "" || targetKind == "" {
			return toolError("propose delete needs kind + target — records are addressed by (kind, id)"), false
		}
		// A delete proposes no values. Dropping the model's diff silently would
		// leave it believing a change it wrote is under review, so say so — on
		// PRESENCE, not content, because an empty diff is a claim about the
		// proposal too (and the engine's admission refuses it either way).
		if raw, named := args["diff"]; named && raw != nil {
			return toolError("propose delete proposes no values — drop the diff, or propose a patch"), false
		}
		ty, err := l.ds.resolveType(targetKind)
		if err != nil {
			return toolError(err.Error()), false
		}
		row, err := l.ds.loadRowDB(ctx, eref{Kind: ty.Identity, ID: target})
		if err != nil {
			return toolError(err.Error()), false
		}
		if row == nil || row.DeletedAt != nil {
			return toolError("target " + target + " does not exist"), false
		}
		edges = []substrate.EdgeInput{{Rel: "target", To: substrate.EdgeRef{Kind: row.Kind, ID: row.ID}}}

	default:
		return toolError("propose op must be one of create, patch, delete"), false
	}

	id, err := newID()
	if err != nil {
		return toolError(err.Error()), false
	}
	err = l.ds.inTx(ctx, l.actor, false, func(t *txn) error {
		t.causedBy = l.in.causedBy
		_, err := t.put(substrate.PutInput{
			Kind: vocabulary.KindRecordPatchRequest, ID: id, Properties: props, Edges: edges,
		})
		return err
	})
	if err != nil {
		return toolError(err.Error()), false
	}
	l.in.tally.effects["propose"]++
	return toolJSON(map[string]any{"id": id}), true
}

// --- function tools ---------------------------------------------------------------

// dispatchFunction runs one callable through the existing machinery — the
// same runner invoke a host Call uses — then holds every returned effect to
// the AGENT's emit and applies under the agent's actor. Both envelopes gate:
// the function's own capabilities bounded the decode inside runCallable, and
// the agent's emit bounds what this agent may let land.
func (l *agentLoop) dispatchFunction(ctx context.Context, fn *vocabulary.Function, args map[string]any) (string, bool) {
	var input any
	if args != nil {
		input = args
	}
	if fn.Input != nil {
		if err := vocabulary.CheckValue(fn.Input, input); err != nil {
			return toolError("input: " + err.Error()), false
		}
	}
	// The function tool re-checks the callee's bundle lifecycle under the
	// agent root's already-held fence: an agent cannot invoke — or
	// commit effects from — a disabled bundle's function. No re-acquire; the
	// lease rides ctx from the agent's root admission.
	if _, _, err := l.ds.admitCallable(ctx, fn.Authority, fn.Identity()); err != nil {
		return toolError(err.Error()), false
	}
	// The tool key derives from the STABLE delivery identity plus the call
	// path and ordinal — never the thread id, which is minted fresh per
	// attempt: a retried delivery hands an external deduper the SAME key,
	// so an effectful tool that honors idempotency keys never double-fires.
	key := fmt.Sprintf("%s/agent/%s/%d", l.in.delivery, l.ag.Identity(), l.toolCalls)
	effects, output, err := l.ds.runCallable(ctx, fn, runner.Input{
		Mode: runner.ModeCall, Args: input,
		CausalDepth:    l.in.causalDepth,
		IdempotencyKey: key,
	})
	if err != nil {
		return toolError(err.Error()), false
	}
	if fn.Output != nil {
		if err := vocabulary.CheckValue(fn.Output, output); err != nil {
			return toolError("output: " + err.Error()), false
		}
	}
	for _, ef := range effects {
		if !l.emitAllows(ef.Type) {
			return toolError(fmt.Sprintf("effect %s %s: %s is not in agent %s's effective emit allowlist — nothing applied",
				ef.Action, ef.ID, ef.Type, l.ag.Identity())), false
		}
	}
	if len(effects) > 0 {
		err = l.ds.inTx(ctx, l.actor, false, func(t *txn) error {
			t.causedBy = l.in.causedBy
			t.setEffectEmit(l.emit)
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
			return toolError(err.Error()), false
		}
		for _, ef := range effects {
			l.in.tally.effects[ef.Action]++
		}
	}
	return toolJSON(map[string]any{"output": output, "effects": len(effects)}), true
}

// --- sub-agents ------------------------------------------------------------------

// dispatchSubAgent is the child invocation: its OWN budgets, agentDepth+1
// against the CALLER's depth cap, a child thread naming its parent,
// the shared tally rolling cost onto the root. Synchronous — the child
// settles before the caller's loop continues.
func (l *agentLoop) dispatchSubAgent(ctx context.Context, sub *vocabulary.Agent, args map[string]any) (string, bool) {
	// The lifecycle gate holds on the child hop too, re-checked under the
	// root's held fence: a disabled or uninstalled bundle's agent
	// refuses even when a live parent asks. No re-acquire; the lease rides ctx.
	if _, _, err := l.ds.admitCallable(ctx, sub.Authority, sub.Identity()); err != nil {
		return toolError(err.Error()), false
	}
	depth := l.in.depth + 1
	if depth > l.ag.Budgets.Depth {
		return toolError(fmt.Sprintf("agent depth cap: %s would run at depth %d (cap %d)", sub.Identity(), depth, l.ag.Budgets.Depth)), false
	}
	input, _ := args["input"].(string)
	if input == "" {
		return toolError("input is required — the task for the sub-agent"), false
	}
	res, err := l.ds.runAgent(ctx, sub, agentInvocation{
		mode: agentModeSubagent, user: input,
		parent: l.threadID, depth: depth,
		causedBy: l.in.causedBy, causalDepth: l.in.causalDepth,
		tally: l.in.tally,
		// The child's ceiling is THIS loop's effective emit: intersection at
		// every hop, so delegation can only narrow, never widen.
		emitCeiling: l.emit, ceilinged: true,
		// The child's delivery identity extends the caller's by the stable
		// call path (this agent, this tool ordinal) — retries reproduce it.
		delivery: fmt.Sprintf("%s/agent/%s/%d", l.in.delivery, l.ag.Identity(), l.toolCalls),
	})
	if err != nil {
		return toolError(err.Error()), false
	}
	return toolJSON(map[string]any{"reply": res.Reply, "thread": res.Thread, "status": res.Status}), res.Status != threadError
}

// --- the completion transport ------------------------------------------------------

// complete runs one model turn through the provider's wire adapter:
// streaming (deltas through emit) when a client is attached, one-shot
// otherwise. The wire lives in internal/llm; the loop only ever sees the
// neutral request and result.
func (l *agentLoop) complete(ctx context.Context, messages []llm.Message) (*llm.Result, error) {
	req := llm.Request{
		Model: l.model, System: l.ag.Prompt, Messages: messages,
		Tools: l.defs, Params: l.params,
	}
	var onDelta func(string)
	if l.in.emit != nil {
		onDelta = func(text string) {
			l.event(substrate.AgentEvent{Kind: substrate.AgentEventDelta, Text: text})
		}
	}
	return l.client.Complete(ctx, req, onDelta)
}
