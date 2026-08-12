package vocabulary

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
)

// An agent is a manifest kind (deliberately unnumbered — the normative kind
// list lives in the contract): a callable whose body is an LLM loop
// (primitives §5). The row IS the prompt store — the changelog is its version
// history — and everything else on it is references: one `provider`
// data-record id plus the `model` it asks that provider for,
// `tools:` (the built-ins `query` and `propose` plus callable functions,
// each optionally aliased for the agent's own prompt context), `agents:`
// (sub-agents), `budgets:` and `emit:`. Agents dispatch exactly like
// functions — triggers, the call API, sub-agent calls — under the actor
// `function:<name>`; the loop itself is host-side (engine/agentloop.go).

// The built-in agent tools. `query` is the capability-scoped read (gated by
// the agent's `reads:`); `propose` emits `recordpatchrequest` records (gated
// by `emit:` naming the request type). Everything else in `tools:` is a
// callable function.
const (
	AgentToolQuery   = "query"
	AgentToolPropose = "propose"
)

// The request params an agent may name in `params:`. The set is closed on
// purpose: a param the loop cannot pass to every dialect is a knob that
// silently does nothing, so an unrecognized key is a load error. A provider
// row's `defaults` is validated against the same set at dispatch.
const (
	AgentParamTemperature = "temperature"
	AgentParamMaxTokens   = "maxTokens"
)

// AgentParamKeys is the recognized set, in the order the errors name it.
var AgentParamKeys = []string{AgentParamMaxTokens, AgentParamTemperature}

// KindRecordPatchRequest is the request type `propose` emits — the one
// `*request` vocabulary the built-in speaks in this build.
const KindRecordPatchRequest = "core.substrate.reamde.dev/recordpatchrequest"

// The budget bounds. Depth is the sub-agent chain cap — a separate counter
// from causal depth, hard-capped at 3.
const (
	DefaultAgentTurns       = 8
	MaxAgentTurns           = 64
	DefaultAgentToolCalls   = 32
	MaxAgentToolCalls       = 256
	DefaultAgentDeadlineSec = 120
	MaxAgentDeadlineSec     = 600
	DefaultAgentDepth       = 3
	MaxAgentDepth           = 3
	// AgentPromptMaxBytes bounds the inline prompt, like a function's source.
	AgentPromptMaxBytes = 64 << 10
)

// Agent is one parsed agent: the loop's declaration, nothing more.
type Agent struct {
	Name      string
	Authority string
	// Description is model-facing and REQUIRED: the agent is its own tool
	// card wherever it appears as a sub-agent.
	Description string
	// Prompt is the system prompt; the row is the prompt store.
	Prompt string
	// Provider is an llmprovider data-record id (`default` or a custom row):
	// WHERE the loop buys completions. Data rows are runtime state, so the
	// reference resolves at dispatch, never at load.
	Provider string
	// Model is WHAT the loop asks that provider for, sent verbatim on every
	// completion.
	Model string
	// Params are this agent's request knobs (temperature, maxTokens); the
	// provider row's defaults sit under them at dispatch.
	Params map[string]any
	// Tools lists what the model may call, in declaration order.
	Tools []AgentTool
	// Agents lists sub-agent identities the loop exposes as tools.
	Agents []string
	// Budgets bounds one invocation.
	Budgets AgentBudgets
	// Emit is the allowlist for the agent's writes: every tool-call effect is
	// held to it, and it names which request types `propose` may emit. Empty
	// means the agent writes nothing.
	Emit []string
	// Reads scopes the `query` built-in exactly like a function's
	// capability-scoped reads; nil means `query` is not granted.
	Reads *FunctionReads

	// Definition is the manifest's data map, exactly as authored.
	Definition map[string]any
}

// AgentTool is one `tools:` entry: a built-in by name, or a callable
// function with an optional per-agent alias (name/description override the
// prompt-facing card; the function manifest stays the canonical source).
type AgentTool struct {
	// Builtin is "query" or "propose"; empty for callables.
	Builtin string
	// Callable is the function identity, for callable entries.
	Callable string
	// Name is the model-facing tool name: the alias when declared, else the
	// builtin's name or the function's local name.
	Name string
	// Description is the alias when declared; empty means the canonical
	// description (the function manifest's) is the card.
	Description string
}

// AgentBudgets bounds one agent invocation. Sub-agents carry their OWN
// budgets; only Depth constrains the chain below a caller.
type AgentBudgets struct {
	MaxTurns        int
	MaxToolCalls    int
	DeadlineSeconds int
	Depth           int
}

// Identity is "<authority>/<name>".
func (a *Agent) Identity() string { return KindRef(a.Authority, a.Name) }

// Actor is the agent's own writing hand — a function actor by another
// name: `function:<name>`, since an agent IS a callable and the actor domain
// has one word for installed code. The local-name caveat on
// Function.Actor holds here too, and across the two: an agent and a function
// sharing a name share an actor.
func (a *Agent) Actor() string { return substrate.FunctionActorPrefix + a.Name }

// EmitAllows reports whether the agent's emit allowlist names a type.
func (a *Agent) EmitAllows(ident string) bool {
	for _, t := range a.Emit {
		if t == ident {
			return true
		}
	}
	return false
}

// reToolName is the model-facing tool name charset (the OpenAI function-name
// contract, restricted to what our aliases need).
var reToolName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`)

// --- the loader's half ---------------------------------------------------------

var agentDataKeys = map[string]bool{
	"authority": true, "description": true, "prompt": true,
	"provider": true, "model": true, "params": true,
	"tools": true, "agents": true, "budgets": true, "emit": true, "reads": true,
}

var agentBudgetKeys = map[string]bool{
	"maxTurns": true, "maxToolCalls": true, "deadlineSeconds": true, "depth": true,
}

var agentToolKeys = map[string]bool{
	"callable": true, "name": true, "description": true,
}

// buildAuthorityAgents parses one authority's agent documents — load.go's one-line
// seam, kept here with the kind it builds.
func (l *loader) buildAuthorityAgents(gd *authorityDocs, g *Authority) {
	sortDocs(gd.agents)
	for _, d := range gd.agents {
		a := l.parseAgent(d)
		if a == nil {
			continue
		}
		if _, dup := g.Agents[a.Name]; dup {
			l.errf("%s %s: declared twice", DocAgent, d.ID)
			continue
		}
		g.Agents[a.Name] = a
		g.AgentOrder = append(g.AgentOrder, a.Name)
	}
	sort.Strings(g.AgentOrder)
}

// parseAgent parses one agent document. Emit, reads, tool callables and
// sub-agents resolve against the registry in Finalize/Install, like a
// function's capability envelope; the provider reference is a DATA row and
// resolves at dispatch instead.
func (l *loader) parseAgent(d Document) *Agent {
	g := l.authority
	where := DocAgent + " " + d.ID
	l.checkKeys(where, d.Data, agentDataKeys)
	local, ok := l.localName(where, d.ID, g.Name)
	if !ok {
		return nil
	}
	a := &Agent{
		Name: local, Authority: g.Name,
		Description: l.parseDescription(where+": data", d.Data),
		Definition:  d.Data,
	}
	if a.Description == "" {
		l.errf("%s: data.description is required — the agent is its own tool card", where)
		return nil
	}
	a.Prompt = mstr(d.Data, "prompt")
	if strings.TrimSpace(a.Prompt) == "" {
		l.errf("%s: data.prompt is required — the row is the prompt store", where)
		return nil
	}
	if len(a.Prompt) > AgentPromptMaxBytes {
		l.errf("%s: data.prompt is %d bytes — the cap is %d", where, len(a.Prompt), AgentPromptMaxBytes)
		return nil
	}
	a.Provider = mstr(d.Data, "provider")
	if a.Provider == "" {
		l.errf("%s: data.provider is required — an llmprovider record id (default, or a custom row)", where)
		return nil
	}
	if !ValidID(a.Provider) {
		l.errf("%s: data.provider: %q is not a record id", where, a.Provider)
		return nil
	}
	a.Model = strings.TrimSpace(mstr(d.Data, "model"))
	if a.Model == "" {
		l.errf("%s: data.model is required — the model id sent to the provider on every completion", where)
		return nil
	}
	if !l.parseAgentParams(where, d.Data, a) {
		return nil
	}

	if !l.parseAgentTools(where, d.Data, a) {
		return nil
	}
	// Sub-agents surface as tools under their LOCAL name, so the name space
	// is one: a collision with a tool (or another sub-agent) is a load
	// error, not a silent shadow at dispatch.
	toolNames := map[string]bool{}
	for _, t := range a.Tools {
		toolNames[t.Name] = true
	}
	for i, sv := range mslice(d.Data, "agents") {
		ident := fmt.Sprint(sv)
		if !Qualified(ident) || strings.Contains(ident, "*") {
			l.errf("%s: data.agents[%d]: %q — sub-agents are full agent identities, no globs", where, i, ident)
			continue
		}
		if ident == d.ID {
			l.errf("%s: data.agents[%d]: an agent may not name itself — the depth cap is not a recursion license", where, i)
			continue
		}
		local := KindName(ident)
		if toolNames[local] {
			l.errf("%s: data.agents[%d]: %q collides with tool name %q — alias the tool", where, i, ident, local)
			continue
		}
		toolNames[local] = true
		a.Agents = append(a.Agents, ident)
	}
	for i, ev := range mslice(d.Data, "emit") {
		t := fmt.Sprint(ev)
		if !Qualified(t) || strings.Contains(t, "*") {
			l.errf("%s: data.emit[%d]: %q — emit names full type identities, no globs", where, i, t)
			continue
		}
		a.Emit = append(a.Emit, t)
	}
	if !l.parseAgentBudgets(where, d.Data, a) {
		return nil
	}
	// `reads:` reuses the function envelope's shape verbatim.
	fn := &Function{}
	if !l.parseReads(where, d.Data, fn) {
		return nil
	}
	a.Reads = fn.Caps.Reads

	// The built-ins' grants are load errors, not dispatch surprises.
	for _, t := range a.Tools {
		switch t.Builtin {
		case AgentToolQuery:
			if a.Reads == nil {
				l.errf("%s: data.tools: query needs data.reads — the built-in is capability-scoped like a function's reads", where)
				return nil
			}
		case AgentToolPropose:
			if !a.EmitAllows(KindRecordPatchRequest) {
				l.errf("%s: data.tools: propose needs %s in data.emit — emit names which request types the agent may propose", where, KindRecordPatchRequest)
				return nil
			}
		}
	}
	return a
}

// parseAgentParams reads `params:` — the agent's own request knobs, which the
// provider row's defaults sit under at dispatch. Both the keys and the value
// types are checked here: a knob the loop would silently drop is worse than a
// refusal at load.
func (l *loader) parseAgentParams(where string, data map[string]any, a *Agent) bool {
	params := mmap(data, "params")
	if len(params) == 0 {
		return true
	}
	for _, k := range sortedKeys(params) {
		switch k {
		case AgentParamTemperature:
			if _, ok := mfloat(params, k); !ok {
				l.errf("%s: data.params.%s: %v — a number", where, k, params[k])
				return false
			}
		case AgentParamMaxTokens:
			f, ok := mfloat(params, k)
			if !ok || f != float64(int(f)) || int(f) < 1 {
				l.errf("%s: data.params.%s: %v — a positive whole number", where, k, params[k])
				return false
			}
		default:
			l.errf("%s: data.params.%s is not a request param — one of %s",
				where, k, strings.Join(AgentParamKeys, ", "))
			return false
		}
	}
	a.Params = params
	return true
}

// parseAgentTools reads the `tools:` list: bare strings are built-ins or
// function identities; maps alias a callable ({callable, name, description}).
func (l *loader) parseAgentTools(where string, data map[string]any, a *Agent) bool {
	seen := map[string]bool{}
	add := func(i int, t AgentTool) bool {
		if !reToolName.MatchString(t.Name) {
			l.errf("%s: data.tools[%d]: tool name %q — letters, digits, _ and -, at most 64", where, i, t.Name)
			return false
		}
		if seen[t.Name] {
			l.errf("%s: data.tools[%d]: tool name %q is declared twice — alias one of them", where, i, t.Name)
			return false
		}
		seen[t.Name] = true
		a.Tools = append(a.Tools, t)
		return true
	}
	for i, tv := range mslice(data, "tools") {
		switch entry := tv.(type) {
		case string:
			if entry == AgentToolQuery || entry == AgentToolPropose {
				if !add(i, AgentTool{Builtin: entry, Name: entry}) {
					return false
				}
				continue
			}
			if !Qualified(entry) || strings.Contains(entry, "*") {
				l.errf("%s: data.tools[%d]: %q — query, propose, or a full function identity", where, i, entry)
				return false
			}
			name := KindName(entry)
			if !add(i, AgentTool{Callable: entry, Name: name}) {
				return false
			}
		case map[string]any:
			l.checkKeys(fmt.Sprintf("%s: data.tools[%d]", where, i), entry, agentToolKeys)
			callable := mstr(entry, "callable")
			if !Qualified(callable) || strings.Contains(callable, "*") {
				l.errf("%s: data.tools[%d]: callable %q — a full function identity (built-ins are bare strings)", where, i, callable)
				return false
			}
			name := mstr(entry, "name")
			if name == "" {
				name = KindName(callable)
			}
			if !add(i, AgentTool{
				Callable: callable, Name: name,
				Description: l.parseDescription(fmt.Sprintf("%s: data.tools[%d]", where, i), entry),
			}) {
				return false
			}
		default:
			l.errf("%s: data.tools[%d]: a tool is a string or a {callable, name, description} map, got %T", where, i, tv)
			return false
		}
	}
	return true
}

func (l *loader) parseAgentBudgets(where string, data map[string]any, a *Agent) bool {
	budgets := mmap(data, "budgets")
	l.checkKeys(where+": data.budgets", budgets, agentBudgetKeys)
	var ok bool
	if a.Budgets.MaxTurns, ok = l.boundedInt(where+": data.budgets.maxTurns", budgets, "maxTurns",
		DefaultAgentTurns, MaxAgentTurns); !ok {
		return false
	}
	if a.Budgets.MaxToolCalls, ok = l.boundedInt(where+": data.budgets.maxToolCalls", budgets, "maxToolCalls",
		DefaultAgentToolCalls, MaxAgentToolCalls); !ok {
		return false
	}
	if a.Budgets.DeadlineSeconds, ok = l.boundedInt(where+": data.budgets.deadlineSeconds", budgets, "deadlineSeconds",
		DefaultAgentDeadlineSec, MaxAgentDeadlineSec); !ok {
		return false
	}
	if a.Budgets.Depth, ok = l.boundedInt(where+": data.budgets.depth", budgets, "depth",
		DefaultAgentDepth, MaxAgentDepth); !ok {
		return false
	}
	return true
}

// resolveAuthorityAgents validates an authority's agents against the loaded registry:
// every emit and read type must exist, every tool callable must be a
// registered function, and every sub-agent must be a registered agent —
// same-batch installs count, like a function's call targets.
func (r *Registry) resolveAuthorityAgents(g *Authority) []string {
	var problems []string
	for _, an := range g.AgentOrder {
		a := g.Agents[an]
		where := DocAgent + " " + a.Identity()
		for _, t := range a.Emit {
			if _, ok := r.ByIdentity(t); !ok {
				problems = append(problems, fmt.Sprintf("%s: data.emit: unknown type %q", where, t))
			}
		}
		if a.Reads != nil {
			for _, t := range a.Reads.Kinds {
				if _, ok := r.ByIdentity(t); !ok {
					problems = append(problems, fmt.Sprintf("%s: data.reads.types: unknown type %q", where, t))
				}
			}
		}
		for _, t := range a.Tools {
			if t.Callable == "" {
				continue
			}
			if _, err := r.ResolveFunction(t.Callable); err != nil {
				problems = append(problems, fmt.Sprintf("%s: data.tools: unknown function %q", where, t.Callable))
			}
		}
		for _, ident := range a.Agents {
			if _, err := r.ResolveAgent(ident); err != nil {
				problems = append(problems, fmt.Sprintf("%s: data.agents: unknown agent %q", where, ident))
			}
		}
	}
	return problems
}

// --- registry lookups ----------------------------------------------------------

// Agents lists every loaded agent, ordered by identity.
func (r *Registry) Agents() []*Agent {
	var out []*Agent
	for _, g := range r.AuthorityList() {
		for _, n := range g.AgentOrder {
			out = append(out, g.Agents[n])
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Identity() < out[j].Identity() })
	return out
}

// ResolveAgent accepts a full identity or a bare name unique across authorities.
func (r *Registry) ResolveAgent(nameOrIdentity string) (*Agent, error) {
	var cands []*Agent
	for _, a := range r.Agents() {
		if a.Identity() == nameOrIdentity {
			return a, nil
		}
		if a.Name == nameOrIdentity {
			cands = append(cands, a)
		}
	}
	switch len(cands) {
	case 0:
		return nil, fmt.Errorf("unknown agent %q", nameOrIdentity)
	case 1:
		return cands[0], nil
	default:
		names := make([]string, 0, len(cands))
		for _, a := range cands {
			names = append(names, a.Identity())
		}
		sort.Strings(names)
		return nil, fmt.Errorf("ambiguous agent %q: %s", nameOrIdentity, strings.Join(names, ", "))
	}
}

// AgentManifest renders an agent document: the identity derives from the name
// and the authority so a caller cannot spell them inconsistently.
func AgentManifest(authority, name string, data map[string]any) map[string]any {
	full := map[string]any{"authority": authority}
	for _, k := range sortedKeys(data) {
		full[k] = data[k]
	}
	return map[string]any{
		"kind":     CoreKind(DocAgent),
		"metadata": map[string]any{"id": KindRef(authority, name)},
		"data":     full,
	}
}
