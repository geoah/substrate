// Package scope is the grammar of an app token's scopes (decision 0194): what
// an app asks for at /oauth/authorize, what the owner approves on the
// approval page, what the token record stores, and what the API holds every
// request made with that token to.
//
// A scope is `read:<pattern>` or `write:<pattern>` over kinds, and write
// includes read of the same pattern. The pattern is the kind grant grammar of
// decision 0080 (`*`, `<authority>/*`, `<authority>/<package>/*` or one kind
// reference) with one addition: `~` in the authority position stands for the
// repository's own authority, which an app cannot know before the owner signs
// in. The token stores the resolved form, never `~`.
//
// `agents:<pattern>` and `decide:<pattern>` (decision 0195) take the same
// grammar over agent ids, which are `<authority>/<package>/<name>` like a kind
// reference: the first talks to the matching agents and reads their threads,
// the second decides the change requests their threads proposed. Neither
// reads or writes a kind through the rules of read and write.
//
// `webhook:<trigger id>` (docs/plans/app-setup.md) names one webhook
// trigger, never a glob: the app delivers to it with its own token and reads
// its parked deliveries. An app never asks for it in `scope`; only a
// `webhook` authorization detail the owner approved puts it on a token, so
// ParseRequest refuses it and CheckStored admits it.
package scope

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/geoah/substrate/internal/vocabulary"
)

// Verb is what a scope does with its pattern.
type Verb string

const (
	Read  Verb = "read"
	Write Verb = "write"
	// Agents talks to the agents whose ids match: chat, their threads,
	// messages and interactions, and their own rows.
	Agents Verb = "agents"
	// Decide accepts or rejects the change requests the matching agents'
	// threads proposed, and reads them.
	Decide Verb = "decide"
	// Webhook delivers to one webhook trigger with the token, and reads that
	// trigger's parked deliveries.
	Webhook Verb = "webhook"
)

const (
	// Home is the authority placeholder: `read:~/tasks/*` asks for the tasks
	// package under whatever authority the owner's repository has.
	Home = "~"
)

// A request's scope parameter is bounded, since a parked request holds it in
// memory and the approval page lists every entry.
const (
	maxRequest = 4096
	maxScopes  = 32
)

// homeStandIn is a syntactically valid authority that stands in for Home
// while a pattern is checked, so `~` is held to exactly the grammar a real
// authority is. `.invalid` is reserved (RFC 2606) and never names a real one.
const homeStandIn = "home.invalid"

// unwritable are the packages no scope writes, whatever it says: they hold
// the machinery that runs code (functions, agents, triggers), spends the
// owner's LLM budget (providers), decides for the owner (policies,
// interactions, change requests) or IS the vocabulary.
var unwritable = []string{vocabulary.PackageCore, vocabulary.PackageLLM}

// Scope is one parsed entry.
type Scope struct {
	Verb    Verb
	Pattern string
}

// String is the scope's canonical spelling, the one the token stores.
func (s Scope) String() string { return string(s.Verb) + ":" + s.Pattern }

// OverAgents reports whether the pattern names agent ids rather than kinds.
func (s Scope) OverAgents() bool { return s.Verb == Agents || s.Verb == Decide }

// HomeRelative reports whether the pattern still names `~` as its authority.
func (s Scope) HomeRelative() bool {
	return s.Pattern == Home+"/*" || strings.HasPrefix(s.Pattern, Home+"/")
}

// Resolve completes `~` with the repository's authority. A scope that names
// no `~` comes back unchanged.
func (s Scope) Resolve(authority string) Scope {
	if s.HomeRelative() {
		s.Pattern = authority + s.Pattern[len(Home):]
	}
	return s
}

// WebhookScope is the scope an approved `webhook` detail puts on a token.
func WebhookScope(trigger string) Scope { return Scope{Verb: Webhook, Pattern: trigger} }

// Parse reads one scope, in a request or as stored: `~` is allowed, and
// `webhook:` parses (ParseRequest refuses it in a request).
func Parse(raw string) (Scope, error) {
	verb, pattern, ok := strings.Cut(raw, ":")
	if !ok {
		return Scope{}, fmt.Errorf("scope %q is not `<verb>:<pattern>`, the verb one of read, write, agents or decide", raw)
	}
	s := Scope{Verb: Verb(verb), Pattern: pattern}
	switch s.Verb {
	case Read, Write, Agents, Decide, Webhook:
	default:
		return Scope{}, fmt.Errorf("scope %q: %q is not read, write, agents or decide", raw, verb)
	}
	problem := ""
	switch {
	case s.Verb == Webhook:
		// One trigger by its record id: a webhook is delivered to one
		// door, so there is nothing to glob, and a trigger id is the
		// repository's own, so there is no authority to complete.
		if !vocabulary.ValidID(s.Pattern) || strings.Contains(s.Pattern, "*") {
			problem = "is not one trigger's id"
		}
	case s.OverAgents():
		problem = agentPatternProblem(s)
	default:
		problem = patternProblem(s)
	}
	if problem != "" {
		return Scope{}, fmt.Errorf("scope %q %s", raw, problem)
	}
	return s, nil
}

// patternProblem holds a pattern to the grammar and the rules every scope
// keeps, returning "" when it is admissible.
func patternProblem(s Scope) string {
	p := s.Pattern
	if s.HomeRelative() {
		p = homeStandIn + p[len(Home):]
	}
	if strings.Contains(p, Home) {
		return "puts `~` somewhere other than the authority"
	}
	if !vocabulary.ValidTypeGlob(p) {
		return "is not a kind pattern: `*`, `<authority>/*`, `<authority>/<package>/*` or `<authority>/<package>/<name>`"
	}
	if !vocabulary.IsTypeGlob(p) {
		// ValidTypeGlob admits a bare name for a declaration's own package;
		// a scope has no declaring package, so every kind is named in full
		// (decision 0101).
		if authority, _, _ := vocabulary.SplitKindRef(p); authority == "" {
			return "names a kind without its authority and package"
		}
		if vocabulary.AuthKinds[p] {
			return "names the repository's own auth material, which no scope reaches"
		}
	}
	if s.Verb == Write && writesUnwritable(p) {
		return "writes the core or llm package, which no scope writes"
	}
	return ""
}

// agentPatternProblem holds an agents or decide pattern to the grammar over
// agent ids: `*`, `<authority>/*`, `<authority>/<package>/*` or one id, whose
// last segment is an agent's camelCase name rather than a kind's word.
func agentPatternProblem(s Scope) string {
	p := s.Pattern
	if s.HomeRelative() {
		p = homeStandIn + p[len(Home):]
	}
	if strings.Contains(p, Home) {
		return "puts `~` somewhere other than the authority"
	}
	const shape = "is not an agent pattern: `*`, `<authority>/*`, `<authority>/<package>/*` or `<authority>/<package>/<name>`"
	if vocabulary.IsTypeGlob(p) {
		if !vocabulary.ValidTypeGlob(p) {
			return shape
		}
		return ""
	}
	authority, pkg, name := vocabulary.SplitKindRef(p)
	if authority == "" || strings.Contains(name, "/") || strings.Contains(name, "*") ||
		!vocabulary.ValidAuthority(authority) || !vocabulary.ValidPackage(pkg) || !vocabulary.ValidCamel(name) {
		return shape
	}
	return ""
}

// writesUnwritable reports whether a write pattern names an unwritable
// package outright: one of its kinds, or the package glob. A wider glob
// (`substrate.reamde.dev/*`, `*`) is admissible; matching skips those
// packages.
func writesUnwritable(p string) bool {
	for _, pkg := range unwritable {
		if p == pkg+"/*" || strings.HasPrefix(p, pkg+"/") {
			return true
		}
	}
	return false
}

// inUnwritable reports whether a kind belongs to an unwritable package.
func inUnwritable(kind string) bool {
	for _, pkg := range unwritable {
		if strings.HasPrefix(kind, pkg+"/") {
			return true
		}
	}
	return false
}

// ParseRequest reads the space-separated `scope` parameter of an
// authorization request: each entry parsed, duplicates dropped, order kept.
// `offline_access` is a standard scope this server has nothing to say about,
// and is dropped. An empty request is refused: an app says what it needs.
func ParseRequest(param string) ([]Scope, error) {
	if len(param) > maxRequest {
		return nil, fmt.Errorf("the scope parameter is longer than %d bytes", maxRequest)
	}
	var out []Scope
	seen := map[string]bool{}
	for _, raw := range strings.Fields(param) {
		if raw == "offline_access" {
			continue
		}
		s, err := Parse(raw)
		if err != nil {
			return nil, err
		}
		if s.Verb == Webhook {
			return nil, fmt.Errorf("scope %q: an app asks for a webhook with a `webhook` authorization detail, never in scope", raw)
		}
		if seen[s.String()] {
			continue
		}
		seen[s.String()] = true
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, errors.New("the request names no scope")
	}
	if len(out) > maxScopes {
		return nil, fmt.Errorf("the request names more than %d scopes", maxScopes)
	}
	return out, nil
}

// CheckStored holds the scopes a token stores: nil is a full-access token and
// passes; otherwise the list is non-empty, every entry parses in its
// canonical spelling, and none still carries `~`. An empty list is refused
// because it would read back as absent, which is full access.
func CheckStored(scopes []string) error {
	if scopes != nil && len(scopes) == 0 {
		return errors.New("a scoped token names at least one scope")
	}
	for _, raw := range scopes {
		s, err := Parse(raw)
		if err != nil {
			return err
		}
		if s.HomeRelative() {
			return fmt.Errorf("scope %q still names `~`: a stored scope names the repository's authority", raw)
		}
		if s.String() != raw {
			return fmt.Errorf("scope %q is not in its canonical spelling", raw)
		}
	}
	return nil
}

// Grant is a stored token's scopes, ready to answer what one request may do.
type Grant struct {
	scopes []Scope
}

// NewGrant parses a token's stored scopes. A stored list that does not parse
// is an error, which the caller treats as a refusal: a damaged grant never
// reads as a wider one.
func NewGrant(stored []string) (Grant, error) {
	if len(stored) == 0 {
		return Grant{}, errors.New("a grant names at least one scope: a token without scopes has full access and no grant")
	}
	if err := CheckStored(stored); err != nil {
		return Grant{}, err
	}
	g := Grant{scopes: make([]Scope, 0, len(stored))}
	for _, raw := range stored {
		s, _ := Parse(raw)
		g.scopes = append(g.scopes, s)
	}
	return g, nil
}

// Scopes returns the grant's scopes in their stored spelling.
func (g Grant) Scopes() []string {
	out := make([]string, len(g.scopes))
	for i, s := range g.scopes {
		out[i] = s.String()
	}
	return out
}

// Reads reports whether the grant reads records of kind. internal is the
// kind's declared purpose being `internal`: a glob never reaches one, an
// exact scope may. Only read and write scopes read a kind whole; what agents
// and decide make readable is decided record by record (TalksTo, Decides).
func (g Grant) Reads(kind string, internal bool) bool {
	return slices.ContainsFunc(g.scopes, func(s Scope) bool {
		return (s.Verb == Read || s.Verb == Write) && matches(s.Pattern, kind, internal)
	})
}

// Delivers reports whether the grant delivers to the webhook trigger whose
// id is trigger, and reads its parked deliveries.
func (g Grant) Delivers(trigger string) bool {
	return trigger != "" && slices.ContainsFunc(g.scopes, func(s Scope) bool {
		return s.Verb == Webhook && s.Pattern == trigger
	})
}

// NamesPackage reports whether one of the grant's read or write scopes names
// a kind of the package pkg (`<authority>/<package>`): exactly, or by a glob
// that covers the package. It is what decides whether a token may learn the
// versions of a package its app needs set up, so the agent verbs and
// `webhook:` never count.
func (g Grant) NamesPackage(pkg string) bool {
	if pkg == "" {
		return false
	}
	return slices.ContainsFunc(g.scopes, func(s Scope) bool {
		if s.Verb != Read && s.Verb != Write {
			return false
		}
		if vocabulary.IsTypeGlob(s.Pattern) {
			return vocabulary.MatchTypeGlob(s.Pattern, pkg+"/")
		}
		return vocabulary.KindPackage(s.Pattern) == pkg
	})
}

// Writes reports whether the grant writes records of kind.
func (g Grant) Writes(kind string, internal bool) bool {
	if inUnwritable(kind) {
		return false
	}
	return slices.ContainsFunc(g.scopes, func(s Scope) bool { return s.Verb == Write && matches(s.Pattern, kind, internal) })
}

// TalksTo reports whether the grant talks to the agent whose record id is
// agent. A glob covers an agent declared after the approval.
func (g Grant) TalksTo(agent string) bool { return g.matchesAgent(Agents, agent) }

// Decides reports whether the grant decides what the agent whose record id is
// agent proposes.
func (g Grant) Decides(agent string) bool { return g.matchesAgent(Decide, agent) }

// Talks reports whether the grant holds any agents scope.
func (g Grant) Talks() bool { return g.has(Agents) }

// DecidesAny reports whether the grant holds any decide scope.
func (g Grant) DecidesAny() bool { return g.has(Decide) }

func (g Grant) has(v Verb) bool {
	return slices.ContainsFunc(g.scopes, func(s Scope) bool { return s.Verb == v })
}

func (g Grant) matchesAgent(v Verb, agent string) bool {
	return agent != "" && slices.ContainsFunc(g.scopes, func(s Scope) bool {
		return s.Verb == v && vocabulary.MatchTypeGlob(s.Pattern, agent)
	})
}

// matches is one pattern against one kind, under the rules every scope
// keeps: the auth kinds are never reached, and a glob never reaches an
// internal kind.
func matches(pattern, kind string, internal bool) bool {
	if vocabulary.AuthKinds[kind] {
		return false
	}
	if vocabulary.IsTypeGlob(pattern) {
		return !internal && vocabulary.MatchTypeGlob(pattern, kind)
	}
	return pattern == kind
}
