package scope

import (
	"fmt"
	"slices"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// An app's agent (decision 0207) is approved by an `agent` authorization
// detail that names it, the exact kinds it reads and writes and its daily
// cap. The token stores the approval beside its scopes, never as one: the
// agent's reach is the agent's, and the app's own scopes stay what the app
// itself uses. Every reader of an approval holds each kind to
// AgentKindProblem again, so a damaged token never widens an agent.

// InteractionKind is the one kind an app's agent writes beyond its
// approval, and only beside the `ask` tool, which writes it.
var InteractionKind = vocabulary.PackageLLM + "/interaction"

// AgentKindProblem holds one kind an app's agent reads or writes to the
// effective-permission rule: one kind named in full, never a glob, never
// the repository's auth material and never a kind of the core or llm
// package, which hold the machinery that runs code, spends the owner's
// provider budget or decides for the owner. A pattern's coverage is not the
// rule: `write:*` covers `llm/memory` as a pattern while no scope writes it.
// `~` stands for the repository's authority, as in a request. It answers ""
// when the kind is admissible.
func AgentKindProblem(kind string) string {
	p := kind
	if p == Home || strings.HasPrefix(p, Home+"/") {
		p = homeStandIn + p[len(Home):]
	}
	if strings.Contains(p, Home) {
		return "puts `~` somewhere other than the authority"
	}
	if vocabulary.IsTypeGlob(p) || !vocabulary.ValidTypeGlob(p) {
		return "is not one kind named in full, `<authority>/<package>/<name>`"
	}
	if authority, _, _ := vocabulary.SplitKindRef(p); authority == "" {
		return "names a kind without its authority and package"
	}
	if vocabulary.AuthKinds[p] {
		return "is the repository's own auth material, which no app's agent reaches"
	}
	if inUnwritable(p) {
		return "belongs to the core or llm package, which no app's agent reads or writes"
	}
	return ""
}

// ResolveAgentKind completes `~` in an agent detail's kind or agent id.
func ResolveAgentKind(kind, authority string) string {
	if strings.HasPrefix(kind, Home+"/") {
		return authority + kind[len(Home):]
	}
	return kind
}

// WithAgents is the grant with the agents its token's owner approved.
func (g Grant) WithAgents(agents []substrate.AgentApproval) Grant {
	g.agents = slices.Clone(agents)
	return g
}

// Agent is the grant's approval of the agent whose record id is id.
func (g Grant) Agent(id string) (substrate.AgentApproval, bool) {
	for _, a := range g.agents {
		if a.Agent == id {
			return a, true
		}
	}
	return substrate.AgentApproval{}, false
}

// Agents are the grant's approved agents.
func (g Grant) Agents() []substrate.AgentApproval { return slices.Clone(g.agents) }

// ApprovesAgentRead reports whether one of the grant's approved agents reads
// kind, which lets the token learn the kind's definition (never its records)
// so its app declares only kinds the repository holds.
func (g Grant) ApprovesAgentRead(kind string) bool {
	return slices.ContainsFunc(g.agents, func(a substrate.AgentApproval) bool { return slices.Contains(a.Reads, kind) })
}

// AgentReachProblem says why an agent that reads reads, writes writes and
// caps its day at capCents is not covered by approval, "" when it is:
// every read approved, every write approved (the interaction kind aside,
// only when ask is a tool), a cap of at least a cent and at most the
// approved one, and every kind admissible under AgentKindProblem.
func AgentReachProblem(approval substrate.AgentApproval, reads, writes []string, ask bool, capCents *int) string {
	for _, list := range [][]string{approval.Reads, approval.Writes} {
		for _, k := range list {
			if p := AgentKindProblem(k); p != "" {
				return fmt.Sprintf("its approval names %s, which %s", k, p)
			}
		}
	}
	for _, k := range reads {
		if !slices.Contains(approval.Reads, k) {
			return fmt.Sprintf("it reads %s, which its approval does not", k)
		}
	}
	for _, k := range writes {
		if k == InteractionKind {
			if !ask {
				return fmt.Sprintf("it writes %s without the ask tool, which is the only one that writes it", k)
			}
			continue
		}
		if !slices.Contains(approval.Writes, k) {
			return fmt.Sprintf("it writes %s, which its approval does not", k)
		}
	}
	switch {
	case capCents == nil:
		return "it declares no budgets.spendCentsPerDay, which an app's agent must"
	case *capCents < 1:
		return fmt.Sprintf("its budgets.spendCentsPerDay is %d, and an app's agent spends at least a cent a day or is removed", *capCents)
	case *capCents > approval.SpendCentsPerDay:
		return fmt.Sprintf("its budgets.spendCentsPerDay is %d, above the %d cents the owner approved", *capCents, approval.SpendCentsPerDay)
	case approval.SpendCentsPerDay > substrate.AppAgentSpendCeilingCents:
		return fmt.Sprintf("its approval caps it at %d cents, above the %d an app's agent may spend", approval.SpendCentsPerDay, substrate.AppAgentSpendCeilingCents)
	}
	return ""
}

// CoversAgent reports whether held approves at least what want asks: the
// same agent, every read and write, and a cap at least as high.
func CoversAgent(held, want substrate.AgentApproval) bool {
	if held.Agent != want.Agent || held.SpendCentsPerDay < want.SpendCentsPerDay {
		return false
	}
	for _, k := range want.Reads {
		if !slices.Contains(held.Reads, k) {
			return false
		}
	}
	for _, k := range want.Writes {
		if !slices.Contains(held.Writes, k) {
			return false
		}
	}
	return true
}
