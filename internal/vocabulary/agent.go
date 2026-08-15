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
// `tools:` (callable functions, the four host built-ins among them, each
// optionally aliased for the agent's own prompt context), `agents:`
// (sub-agents), `budgets:` and `permissions:`. Agents dispatch exactly like
// functions — triggers, the call API, sub-agent calls — under the actor
// `function:<name>`; the loop itself is host-side (engine/agentloop.go).

// The built-in agent tools, by the LOCAL NAME of the host function record that
// declares each one. `query` is the capability-scoped read (gated by the agent's
// `permissions.reads`); `propose` emits `recordpatchrequest` records (gated by
// `permissions.writes` naming the request type); `graphql` is the
// WHOLE-repository read-only GraphQL surface (declaring it is the grant: there
// is no narrower scope to declare, which is why the scoped `query` survives
// beside it); `mutate` executes GraphQL mutations, each written kind held to the
// agent's effective emit (so it needs a non-empty `permissions.writes`).
//
// A `tools:` entry names ONE of them the way it names any other function: by
// identity, under `function:`. There is no second arm — see agentBuiltinByIdentity.
const (
	AgentToolQuery   = "query"
	AgentToolPropose = "propose"
	AgentToolGraphQL = "graphql"
	AgentToolMutate  = "mutate"
	AgentToolAsk     = "ask"
)

// agentBuiltinByIdentity maps a host function's IDENTITY onto the built-in the
// loop dispatches. It is what makes `{function: core.substrate.reamde.dev/query}`
// carry the same grant check and the same dispatch as the retired `{builtin:
// query}` arm: the four are ordinary function records the registry ships, so
// they resolve like any callable, and this is the only place that knows which of
// them the engine implements in the loop rather than the runner.
var agentBuiltinByIdentity = map[string]string{
	HostFunctionQuery:   AgentToolQuery,
	HostFunctionPropose: AgentToolPropose,
	HostFunctionGraphQL: AgentToolGraphQL,
	HostFunctionMutate:  AgentToolMutate,
	HostFunctionAsk:     AgentToolAsk,
}

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

// KindLLMThread is the thread kind a `notifies:` transition reports into.
const KindLLMThread = "core.substrate.reamde.dev/llmthread"

// KindLLMInteraction is the batch-of-questions kind the `ask` built-in emits.
const KindLLMInteraction = "core.substrate.reamde.dev/llminteraction"

// The declared `resume:` values: whether a resolution resumes the agent's
// thread. Absent means always.
const (
	AgentResumeAlways = "always"
	AgentResumeNever  = "never"
)

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
	// Emit is `permissions.writes` parsed: every tool-call effect is held to it,
	// and it names which request types `propose` may emit. Empty means the agent
	// writes nothing.
	Emit []string
	// Reads is `permissions.reads` parsed, scoping the `query` built-in exactly
	// like a function's; nil means `query` is not granted.
	Reads *FunctionReads
	// SubagentOnly withholds the agent from the interactive chat surface: the
	// console keeps it off the chat list and ChatAgent refuses it. Everything
	// else still dispatches it: sub-agent calls (the point, an llm-as-judge
	// exists to be called by other agents), the call API, and triggers.
	SubagentOnly bool
	// Resume says whether a resolution on this agent's thread-borne records
	// (a decided proposal, an answered interaction) RESUMES the thread —
	// "never" records the resolution row and stops there; "always" (and
	// absent, today's behavior) continues the thread. A resume is a paid
	// agent turn, so this is the declaration's own cost knob (issue #69).
	Resume string

	// Definition is the declaration's own data map, exactly as authored — what
	// the row stores as its properties.
	Definition map[string]any
}

// AgentTool is one `tools:` entry: the FUNCTION it names, with an optional
// per-agent alias (name/description override the prompt-facing card; the
// function declaration stays the canonical source).
type AgentTool struct {
	// Builtin is DERIVED, never authored: the built-in word when Callable names
	// one of the four host functions, empty otherwise. The grant checks and the
	// loop's dispatch switch read it, so what used to be a second arm of the
	// `tools:` union is now one lookup on the identity the entry already carries.
	Builtin string
	// Callable is the identity `function:` names — always set, built-ins
	// included. The authored key is `function` because an entry admits nothing
	// else: a sub-agent is named on `agents:`, and `callable` is the TRIGGER's
	// word, where a target really may be either.
	Callable string
	// Name is the model-facing tool name: the alias when declared, else the
	// function's local name.
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

// EmitAllows reports whether the agent's write allowlist names a type.
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
	"tools": true, "agents": true, "budgets": true, "permissions": true,
	"subagentOnly": true, "resume": true,
}

// deletedAgentKeys are the removed keys, each naming what replaced it. An
// agent's two grants group under `permissions:` with a function's five, and the
// permission to write is named for writing: a bare `emit:` said nothing about
// being a grant at all. No compatibility shim: the rows written that way are
// translated by the dialect rung (engine/dialectonegrammar.go).
var deletedAgentKeys = map[string]string{
	"emit":  "permissions.writes: the grants group under `permissions:`, and the permission to write is named for writing",
	"reads": "permissions.reads: the grants group under `permissions:`",
}

// agentPermissionKeys is the agent's grant object: a function's five minus the
// three an LLM loop has no body to spend (call, network, mutations).
var agentPermissionKeys = map[string]bool{"reads": true, "writes": true}

var agentBudgetKeys = map[string]bool{
	"maxTurns": true, "maxToolCalls": true, "deadlineSeconds": true, "depth": true,
}

// agentToolKeys is a tool entry's key set: `function` names the tool, `name` and
// `description` alias its card for this agent.
var agentToolKeys = map[string]bool{
	"function": true, "name": true, "description": true,
}

// deletedAgentToolKeys are the retired keys of a tool ENTRY, each naming its
// replacement. `callable` said the entry might name something other than a
// function, and it never could: a sub-agent is named on `agents:`, and the word
// belongs to a trigger, whose target really is a function OR an agent.
var deletedAgentToolKeys = map[string]string{
	"callable": "function — a tool entry names a function, and only a function",
}

// agentBuiltinIdentities lists the four host functions in the order the errors
// name them.
var agentBuiltinIdentities = []string{
	HostFunctionQuery, HostFunctionPropose, HostFunctionGraphQL, HostFunctionMutate,
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

// parseAgent parses one agent document. The permissions, tool functions and
// sub-agents resolve against the registry in Finalize/Install, like a
// function's grant; the provider reference is a DATA row and resolves at
// dispatch instead.
func (l *loader) parseAgent(d Document) *Agent {
	g := l.authority
	where := DocAgent + " " + d.ID
	for k := range d.Data {
		if replacement, gone := deletedAgentKeys[k]; gone {
			l.errf("%s: key %q is deleted — %s", where, k, replacement)
			return nil
		}
	}
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
	// `provider` is a REFERENCE at llmprovider: a manifest authors the bare
	// record id and the row stores the full path, and the loop resolves the id.
	a.Provider = ReferentID(d.Data["provider"], KindRef(AuthorityCore, "llmprovider"))
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
	a.SubagentOnly = mbool(d.Data, "subagentOnly")
	a.Resume = mstr(d.Data, "resume")
	if a.Resume != "" && a.Resume != AgentResumeAlways && a.Resume != AgentResumeNever {
		l.errf("%s: data.resume: %q — \"always\" or \"never\" (absent means always)", where, a.Resume)
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
	for i, ident := range ReferentIDs(mslice(d.Data, "agents"), KindRef(AuthorityCore, DocAgent)) {
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
	perms, ok := l.permissionsObject(where, d.Data, agentPermissionKeys)
	if !ok {
		return nil
	}
	for i, t := range ReferentIDs(mslice(perms, "writes"), KindRef(AuthorityCore, DocKind)) {
		if !Qualified(t) || strings.Contains(t, "*") {
			l.errf("%s: data.permissions.writes[%d]: %q is not a full type identity; writes names them, no globs", where, i, t)
			continue
		}
		a.Emit = append(a.Emit, t)
	}
	if !l.parseAgentBudgets(where, d.Data, a) {
		return nil
	}
	// `permissions.reads` reuses the function grant's shape verbatim.
	fn := &Function{}
	if !l.parseReads(where, perms, fn) {
		return nil
	}
	a.Reads = fn.Caps.Reads

	// The built-ins' grants are load errors, not dispatch surprises. `graphql`
	// alone needs none: it is read-only and repository-wide by design, and the
	// declaration is the grant.
	for _, t := range a.Tools {
		switch t.Builtin {
		case AgentToolQuery:
			if a.Reads == nil {
				l.errf("%s: data.tools: query needs data.permissions.reads, since the built-in is capability-scoped like a function's reads", where)
				return nil
			}
		case AgentToolPropose:
			if !a.EmitAllows(KindRecordPatchRequest) {
				l.errf("%s: data.tools: propose needs %s in data.permissions.writes, which names the request kinds the agent may create", where, KindRecordPatchRequest)
				return nil
			}
		case AgentToolMutate:
			if len(a.Emit) == 0 {
				l.errf("%s: data.tools: mutate needs data.permissions.writes, which names the kinds the agent may create or change", where)
				return nil
			}
		case AgentToolAsk:
			if !a.EmitAllows(KindLLMInteraction) {
				l.errf("%s: data.tools: ask needs %s in data.permissions.writes, which names the interaction kind the agent may create", where, KindLLMInteraction)
				return nil
			}
		}
	}
	return a
}

// AgentParams are the parsed request knobs. Temperature is a pointer because
// current models differ on whether a sampling param is even accepted: nil
// means "do not send one", which is not the same as sending zero.
type AgentParams struct {
	Temperature *float32
	MaxTokens   int
}

// ParseAgentParams validates a request-param map and parses it. The set is
// closed on purpose: a param the loop cannot pass to every dialect is a knob
// that silently does nothing, so an unrecognized key is a refusal. ONE
// validator serves both callers — the loader holding an agent manifest and the
// dispatcher holding a provider row's defaults merged under it — so a provider
// row can never accept a value a manifest could not. The error leads with the
// offending KEY, and each caller prefixes its own position.
func ParseAgentParams(params map[string]any) (AgentParams, error) {
	var out AgentParams
	for _, k := range sortedKeys(params) {
		switch k {
		case AgentParamTemperature:
			f, ok := mfloat(params, k)
			if !ok {
				return AgentParams{}, fmt.Errorf("%s: %v — a number", k, params[k])
			}
			t := float32(f)
			out.Temperature = &t
		case AgentParamMaxTokens:
			f, ok := mfloat(params, k)
			if !ok || f != float64(int(f)) || int(f) < 1 {
				return AgentParams{}, fmt.Errorf("%s: %v — a positive whole number", k, params[k])
			}
			out.MaxTokens = int(f)
		default:
			return AgentParams{}, fmt.Errorf("%s is not a request param — one of %s",
				k, strings.Join(AgentParamKeys, ", "))
		}
	}
	return out, nil
}

// parseAgentParams reads `params:` — the agent's own request knobs, which the
// provider row's defaults sit under at dispatch. The manifest keeps the RAW
// map (the dispatcher merges maps, not parsed structs); validating it here is
// what makes a knob the loop would silently drop a refusal at load instead.
func (l *loader) parseAgentParams(where string, data map[string]any, a *Agent) bool {
	params := mmap(data, "params")
	if len(params) == 0 {
		return true
	}
	if _, err := ParseAgentParams(params); err != nil {
		l.errf("%s: data.params.%s", where, err)
		return false
	}
	a.Params = params
	return true
}

// parseAgentTools reads the `tools:` list. An entry names ONE tool, ONE way:
// `function:` plus a function identity, optionally aliased for this agent's
// prompt context with `name`/`description`. The four built-ins are function
// records like any other (`core.substrate.reamde.dev/query`, …), so they are
// named here exactly like a bundle's function is.
//
// THREE OLDER SPELLINGS ARE REFUSED, each naming what replaced it. A bare STRING
// named the arm by its value — a built-in if the word happened to be one, a
// function otherwise — so one shape held two kinds of thing and a typo in a
// built-in's name silently became a function nothing declares. `{builtin: x}`
// was the interim arm that split the union explicitly: it was a design miss,
// because it made the built-ins the ONE thing an agent could name that no record
// declared, and it is gone now that they are records. And `{callable: x}` said
// the entry might name something other than a function; it never could, since a
// sub-agent is named on `agents:`. The stored rows written any of those ways are
// translated by the dialect rung (engine/dialectonegrammar.go), which is the
// only reader of those spellings left.
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
			l.errf("%s: data.tools[%d]: %q is a bare string — an entry names its function: {function: %s}",
				where, i, entry, toolFunctionHint(entry))
			return false
		case map[string]any:
			twhere := fmt.Sprintf("%s: data.tools[%d]", where, i)
			if builtin := mstr(entry, "builtin"); builtin != "" {
				// THE TOMBSTONE. The built-ins are records, so the union has one arm.
				l.errf("%s: builtin %q is deleted — the built-ins are function records: {function: %s}",
					twhere, builtin, toolFunctionHint(builtin))
				return false
			}
			for k := range entry {
				if replacement, gone := deletedAgentToolKeys[k]; gone {
					l.errf("%s: key %q is deleted — %s", twhere, k, replacement)
					return false
				}
			}
			l.checkKeys(twhere, entry, agentToolKeys)
			fnIdent := ReferentID(entry["function"], KindRef(AuthorityCore, DocFunction))
			if fnIdent == "" {
				l.errf("%s: no function — an entry names one function identity (a built-in is one of %s)",
					twhere, strings.Join(agentBuiltinIdentities, ", "))
				return false
			}
			if !Qualified(fnIdent) || strings.Contains(fnIdent, "*") {
				l.errf("%s: function %q — a full function identity, no globs", twhere, fnIdent)
				return false
			}
			name := mstr(entry, "name")
			if name == "" {
				name = KindName(fnIdent)
			}
			// A built-in is aliasable exactly like any other function: the loop
			// reads the tool's name and description off the entry when it carries
			// them and off the resolved function record otherwise, which is the
			// same rule for all four arms of the old union and for every bundle
			// function.
			if !add(i, AgentTool{
				Callable: fnIdent, Name: name, Builtin: agentBuiltinByIdentity[fnIdent],
				Description: l.parseDescription(twhere, entry),
			}) {
				return false
			}
		default:
			l.errf("%s: data.tools[%d]: a tool is a {function, name, description} map, got %T", where, i, tv)
			return false
		}
	}
	return true
}

// toolFunctionHint spells the identity a retired entry meant, so a refusal names
// the exact replacement: a built-in's bare word becomes its core identity, and
// anything else was already an identity.
func toolFunctionHint(named string) string {
	for ident, word := range agentBuiltinByIdentity {
		if word == named {
			return ident
		}
	}
	return named
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
// every written and read type must exist, every tool must name a
// registered function, and every sub-agent must be a registered agent —
// same-batch installs count, like a function's call targets.
func (r *Registry) resolveAuthorityAgents(g *Authority) []string {
	var problems []string
	for _, an := range g.AgentOrder {
		a := g.Agents[an]
		where := DocAgent + " " + a.Identity()
		for _, t := range a.Emit {
			if _, ok := r.ByIdentity(t); !ok {
				problems = append(problems, fmt.Sprintf("%s: data.permissions.writes: unknown type %q", where, t))
			}
		}
		if a.Reads != nil {
			for _, t := range a.Reads.Kinds {
				if _, ok := r.ByIdentity(t); !ok {
					problems = append(problems, fmt.Sprintf("%s: data.permissions.reads.kinds: unknown type %q", where, t))
				}
			}
		}
		// EVERY tool resolves the same way, built-ins included: they are function
		// records the registry ships, so there is no arm here that skips the
		// existence check.
		for _, t := range a.Tools {
			if _, err := r.ResolveFunction(t.Callable); err != nil {
				problems = append(problems, fmt.Sprintf("%s: data.tools.function: unknown function %q", where, t.Callable))
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
