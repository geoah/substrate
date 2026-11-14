// Package scope is the grammar of an app token's scopes (decision 0194): what
// an app asks for at /oauth/authorize, what the owner approves on the
// approval page, what the token record stores, and what the API holds every
// request made with that token to.
//
// A scope is `read:<pattern>` or `write:<pattern>` over kinds, and write
// includes read of the same pattern. The pattern is the kind grant grammar of
// decision 0080 (`*`, `<authority>/*`, `<authority>/<package>/*` or one kind
// reference) in which `~`, in the authority position, is the repository's
// local namespace, where its own kinds live (decision record 0259): an app
// asks for `read:~/tasks/*` without knowing whose repository it will be, and
// the token stores it as asked. The repository's full name is another
// spelling of `~`, which Localize folds.
//
// A read or write pattern may instead name a trait (decision 0225):
// `read:implements(<trait>)` covers what `read:*` would among the kinds whose
// declaration binds the trait when a request is checked, so a kind that binds
// it later is covered from then on. The trait is named in full, `~` standing
// for the authority as it does in a kind pattern.
//
// `agents:<pattern>` and `decide:<pattern>` (decision 0195) take the same
// grammar over agent ids, which are `<authority>/<package>/<name>` like a kind
// reference: the first talks to the matching agents and reads their threads,
// the second decides the change requests their threads proposed. Neither
// reads or writes a kind through the rules of read and write.
//
// `declare:` is retired (decision 0214): an app that brings its own kinds
// publishes them as a package its approval installs, through a `package`
// authorization detail. A request naming the verb is refused with words that
// say so, and a stored token carrying it fails closed, so the app signs in
// again.
//
// `full` (decision 0204) is a scope with no pattern, asked for alone: the
// owner hands the app everything a password sign-in gets, through the same
// approval. It is never stored: approving it mints a token without scopes,
// which is full access (decision 0143), so Parse, CheckStored and NewGrant
// refuse it and only ParseRequest reads it.
//
// `mcp` (decision 0230) is a scope with no pattern, asked for alone like
// `full`: the owner hands an assistant everything the console does but the
// auth material, at the MCP door alone. Unlike `full` it IS stored, as the
// one-entry list `["mcp"]`, so a binary that does not know the word refuses
// the token instead of reading it as full access. It holds no grant:
// NewGrant refuses it, and the API's bearer check admits it at the MCP door
// before any grant is read.
//
// `webhook:<trigger id>` (docs/plans/app-setup.md) names one webhook
// trigger, never a glob: the app delivers to it with its own token and reads
// its parked deliveries. An app never asks for it in `scope`; only a
// `webhook` authorization detail the owner approved puts it on a token, so
// ParseRequest refuses it and CheckStored admits it.
//
// `call:<pattern>` (decision 0219) takes the kind grammar over function ids,
// which are `<authority>/<package>/<name>` like a kind reference: the app
// calls the matching functions, and only those whose own reach (the kinds
// their `permissions` read and write) its read and write scopes cover,
// which ReachCovered decides. A function of the core or llm package is
// never matched: naming one outright is refused, and a glob skips them.
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
	// Call runs the functions whose ids match, each only where the grant's
	// reads and writes cover what the function itself reaches (decision
	// 0219).
	Call Verb = "call"
	// Full is everything the owner's own token does. A request names it
	// alone, as the bare word `full`.
	Full Verb = "full"
	// MCP is everything the owner's own token does but the auth material,
	// at the MCP door alone (decision 0230). A request names it alone, as
	// the bare word `mcp`, and the token stores it as `["mcp"]`.
	MCP Verb = "mcp"
)

// retiredDeclare is the verb decision 0203 gave an app to keep a package of
// its own, retired by decision 0214. Both doors refuse it by name, so an app
// built against the old grammar is told what replaced it rather than that
// the verb is unknown.
const retiredDeclare = "declare"

// FullScope is the one scope of the Full verb.
var FullScope = Scope{Verb: Full}

// MCPScope is the one scope of the MCP verb.
var MCPScope = Scope{Verb: MCP}

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

// A trait scope (decision 0225) names a trait rather than kinds, spelled as
// the records filter spells one: `read:implements(<trait>)` covers what
// `read:*` would among the kinds whose declaration binds the trait when a
// request is checked, and `write:` the same.
const (
	traitOpen  = "implements("
	traitClose = ")"
)

// TraitPattern is the pattern of the trait scope over trait.
func TraitPattern(trait string) string { return traitOpen + trait + traitClose }

// traitOf is the trait a pattern names, and whether it is a trait pattern.
func traitOf(pattern string) (string, bool) {
	if !strings.HasPrefix(pattern, traitOpen) || !strings.HasSuffix(pattern, traitClose) {
		return "", false
	}
	return pattern[len(traitOpen) : len(pattern)-len(traitClose)], true
}

// Kind is one kind as a read or write scope is held to it: its reference,
// whether its declared purpose is `internal`, which neither a glob nor a
// trait scope reaches, and the traits its declaration binds, each by its
// full identity (vocabulary.TraitBindingRef).
type Kind struct {
	Ref      string
	Internal bool
	Traits   []string
	// Control is whether the kind is a control kind (vocabulary.ControlClassOf),
	// which a role's pattern reaches only by naming it.
	Control bool
}

// KindOf is the kind ref as its declaration's data says it (a registry
// kind's Definition, a closure's shipped data): whether its purpose is
// `internal`, and the trait each of its `traits:` entries binds.
func KindOf(ref string, definition map[string]any) Kind {
	k := Kind{Ref: ref}
	if purpose, _ := definition["purpose"].(string); purpose == vocabulary.PurposeInternal {
		k.Internal = true
	}
	var entries []string
	switch v := definition["traits"].(type) {
	case []string:
		entries = v
	case []any:
		for _, e := range v {
			if s, ok := e.(string); ok {
				entries = append(entries, s)
			}
		}
	}
	for _, e := range entries {
		if trait, ok := vocabulary.TraitBindingRef(e); ok {
			k.Traits = append(k.Traits, trait)
		}
	}
	return k
}

// binds reports whether the kind's declaration binds trait, matched in its
// canonical spelling, so no other casing stands for it.
func (k Kind) binds(trait string) bool { return slices.Contains(k.Traits, trait) }

// unwritable are the packages no scope writes, whatever it says: they hold
// the machinery that runs code (functions, agents, triggers), spends the
// owner's LLM budget (providers), decides for the owner (policies,
// interactions, change requests) or IS the vocabulary. The applets package
// is one too: an applet runs reads and writes under the token of whoever
// opens it, and an image URL it builds from a record's values carries them
// to its host, so only the owner, their assistant's designer and the MCP
// door write one (docs/plans/applets.md).
var unwritable = []string{vocabulary.PackageCore, vocabulary.PackageLLM, vocabulary.PackageApplets}

// Scope is one parsed entry.
type Scope struct {
	Verb    Verb
	Pattern string
}

// String is the scope's canonical spelling, the one the token stores.
func (s Scope) String() string {
	if s.Verb == Full || s.Verb == MCP {
		return string(s.Verb)
	}
	return string(s.Verb) + ":" + s.Pattern
}

// RequestsFull reports whether a parsed request asks for full access, which
// ParseRequest admits only alone.
func RequestsFull(scopes []Scope) bool { return slices.Contains(scopes, FullScope) }

// RequestsMCP reports whether a parsed request asks for an assistant's
// access at the MCP door, which ParseRequest admits only alone.
func RequestsMCP(scopes []Scope) bool { return slices.Contains(scopes, MCPScope) }

// RequestsWhole reports whether a parsed request asks for a scope the owner
// approves whole or not at all, through a human door: `full` or `mcp`.
func RequestsWhole(scopes []Scope) bool { return RequestsFull(scopes) || RequestsMCP(scopes) }

// IsMCP reports whether a token's stored scopes are an assistant's, which
// work at the MCP door alone.
func IsMCP(stored []string) bool { return len(stored) == 1 && stored[0] == string(MCP) }

// OverAgents reports whether the pattern names agent ids rather than kinds.
func (s Scope) OverAgents() bool { return s.Verb == Agents || s.Verb == Decide }

// Trait is the trait a read or write scope names, in full, and whether it
// names one rather than kinds.
func (s Scope) Trait() (string, bool) {
	if s.Verb != Read && s.Verb != Write {
		return "", false
	}
	return traitOf(s.Pattern)
}

// HomeRelative reports whether the pattern names `~` as its authority, a trait
// scope's trait included.
func (s Scope) HomeRelative() bool {
	p := s.Pattern
	if trait, ok := s.Trait(); ok {
		p = trait
	}
	return p == Home+"/*" || strings.HasPrefix(p, Home+"/")
}

// Localize folds the repository's full name, authority, into `~`:
// `read:ada.example.com/tasks/*` is `read:~/tasks/*` in the repository named
// ada.example.com, the one spelling a token stores. A scope naming another
// authority comes back unchanged.
func (s Scope) Localize(authority string) Scope {
	if trait, ok := s.Trait(); ok {
		s.Pattern = TraitPattern(vocabulary.LocalizeRef(trait, authority))
		return s
	}
	s.Pattern = vocabulary.LocalizeRef(s.Pattern, authority)
	return s
}

// WebhookScope is the scope an approved `webhook` detail puts on a token.
func WebhookScope(trigger string) Scope { return Scope{Verb: Webhook, Pattern: trigger} }

// The verbs a stored scope may carry, and the four of them a request may ask
// for, as a refusal names them.
const (
	storedVerbs    = "read, write, agents, decide, call or webhook"
	requestedVerbs = "read, write, agents, decide or call, or `full` or `mcp` alone"
	// retiredDeclareWords is what both doors say of the retired verb.
	retiredDeclareWords = "`declare:` is retired (decision 0214): an app that needs a package of its own installed names it in a `package` authorization detail, published under its authority"
)

// ErrRetired marks a scope whose verb a later decision retired. A stored
// grant holding one no longer authenticates, but it is not damage: the token
// is still the owner's to see and revoke.
var ErrRetired = errors.New("the scope's verb is retired")

// retiredError is a retired verb's refusal: its words, and ErrRetired for a
// caller that tells retirement from damage.
type retiredError struct{ raw string }

func (e retiredError) Error() string        { return fmt.Sprintf("scope %q: %s", e.raw, retiredDeclareWords) }
func (e retiredError) Is(target error) bool { return target == ErrRetired }

// Parse reads one scope, in a request or as stored: `~` is allowed, and
// `webhook:` parses (ParseRequest refuses it in a request).
func Parse(raw string) (Scope, error) {
	verb, pattern, ok := strings.Cut(raw, ":")
	if !ok {
		return Scope{}, fmt.Errorf("scope %q is not `<verb>:<pattern>`, the verb one of %s", raw, storedVerbs)
	}
	s := Scope{Verb: Verb(verb), Pattern: pattern}
	switch s.Verb {
	case Read, Write, Agents, Decide, Call, Webhook:
	case retiredDeclare:
		return Scope{}, retiredError{raw}
	default:
		return Scope{}, fmt.Errorf("scope %q: %q is not %s", raw, verb, storedVerbs)
	}
	problem := ""
	switch {
	case s.Verb == Webhook:
		// One trigger by its record id: a webhook is delivered to one
		// door, so there is nothing to glob, and a trigger id is the
		// repository's own, so there is no authority to complete.
		if !vocabulary.ValidTriggerID(s.Pattern) {
			problem = "is not one trigger's id"
		}
	case s.Verb == Call:
		problem = callPatternProblem(s)
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
	if trait, ok := s.Trait(); ok {
		return traitProblem(trait)
	}
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
		return "writes the core, llm or applets package, which no scope writes"
	}
	return ""
}

// traitProblem holds a trait scope's trait to the grammar: one trait named in
// full (decision 0101), `~` admitted for the authority. Any trait is
// admissible for either verb, core's included: a trait scope reaches only
// what `*` would, so the rules every glob keeps hold it already, and
// `write:implements(substrate.reamde.dev/core/accountconfig)` writes nothing.
// A trait nothing binds yet is admitted, as a glob that matches nothing is.
func traitProblem(trait string) string {
	if strings.HasPrefix(trait, Home+"/") {
		trait = homeStandIn + trait[len(Home):]
	}
	authority, pkg, name := vocabulary.SplitKindRef(trait)
	if authority == "" || strings.Contains(name, "/") || !vocabulary.ValidAuthority(authority) ||
		!vocabulary.ValidPackage(pkg) || !vocabulary.ValidCamel(name) {
		return "is not a trait pattern: `implements(<authority>/<package>/<name>)`, the trait named in full"
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

// callPatternProblem holds a call pattern to the kind grammar over function
// ids, whose last segment is a function's lowercase word as a kind's is. A
// function of the core or llm package is never an app's to call: those
// packages hold the agent built-ins and the machinery that runs code, so
// naming one outright, or the package glob, is refused; a wider glob is
// admissible and Calls skips them.
func callPatternProblem(s Scope) string {
	p := s.Pattern
	if s.HomeRelative() {
		p = homeStandIn + p[len(Home):]
	}
	if strings.Contains(p, Home) {
		return "puts `~` somewhere other than the authority"
	}
	if !vocabulary.ValidTypeGlob(p) {
		return "is not a function pattern: `*`, `<authority>/*`, `<authority>/<package>/*` or `<authority>/<package>/<name>`"
	}
	if !vocabulary.IsTypeGlob(p) {
		if authority, _, _ := vocabulary.SplitKindRef(p); authority == "" {
			return "names a function without its authority and package"
		}
	}
	if writesUnwritable(p) {
		return "names a function of the core, llm or applets package, which no app calls"
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
		if raw == string(Full) || raw == string(MCP) {
			if !seen[raw] {
				seen[raw] = true
				out = append(out, Scope{Verb: Verb(raw)})
			}
			continue
		}
		verb, _, ok := strings.Cut(raw, ":")
		switch Verb(verb) {
		case Read, Write, Agents, Decide, Call:
		case retiredDeclare:
			return nil, retiredError{raw}
		case Webhook:
			return nil, fmt.Errorf("scope %q: an app asks for a webhook with a `webhook` authorization detail, never in scope", raw)
		default:
			if !ok {
				return nil, fmt.Errorf("scope %q is not `<verb>:<pattern>`, the verb one of %s", raw, requestedVerbs)
			}
			return nil, fmt.Errorf("scope %q: %q is not %s, the verbs a request asks for", raw, verb, requestedVerbs)
		}
		s, err := Parse(raw)
		if err != nil {
			return nil, err
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
	if RequestsFull(out) && len(out) > 1 {
		return nil, errors.New("`full` is asked for alone: it already holds every other scope, and the owner approves it whole or not at all")
	}
	if RequestsMCP(out) && len(out) > 1 {
		return nil, errors.New("`mcp` is asked for alone: it already reaches what every other scope does at the MCP door, and the owner approves it whole or not at all")
	}
	if len(out) > maxScopes {
		return nil, fmt.Errorf("the request names more than %d scopes", maxScopes)
	}
	return out, nil
}

// CheckStored holds the scopes a token stores: nil is a full-access token and
// passes; otherwise the list is non-empty and every entry parses in its
// canonical spelling. An empty list is refused because it would read back as
// absent, which is full access.
func CheckStored(scopes []string) error {
	if scopes != nil && len(scopes) == 0 {
		return errors.New("a scoped token names at least one scope")
	}
	if IsMCP(scopes) {
		return nil
	}
	for _, raw := range scopes {
		if raw == string(MCP) {
			return errors.New("`mcp` is stored alone: an assistant's token names no other scope")
		}
		s, err := Parse(raw)
		if err != nil {
			return err
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
	// An assistant's token reaches nothing through a grant: the bearer check
	// admits it at the MCP door alone, before any grant is read, so a path
	// that reads one for it fails closed.
	if IsMCP(stored) {
		return Grant{}, errors.New("an assistant's `mcp` token holds no grant: it works at the MCP door alone")
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

// Reads reports whether the grant reads records of kind. A kind whose
// declared purpose is `internal` is reached by an exact scope alone, never by
// a glob or a trait scope. Only read and write scopes read a kind whole; what
// agents and decide make readable is decided record by record (TalksTo,
// Decides).
func (g Grant) Reads(kind Kind) bool {
	return slices.ContainsFunc(g.scopes, func(s Scope) bool {
		return (s.Verb == Read || s.Verb == Write) && matches(s.Pattern, kind)
	})
}

// Delivers reports whether the grant delivers to the webhook trigger whose
// id is trigger, and reads its parked deliveries.
func (g Grant) Delivers(trigger string) bool {
	return trigger != "" && slices.ContainsFunc(g.scopes, func(s Scope) bool {
		return s.Verb == Webhook && s.Pattern == trigger
	})
}

// NamesPackage reports whether one of the grant's scopes reaches into the
// package pkg (`<authority>/<package>`): a read or write over a kind of it,
// an agents or decide scope over an agent of it, or a call scope over a
// function of it, exactly or by a glob that covers the package. It is what
// decides whether a token may learn the versions of a package its app needs
// set up: a package of agents alone, such as a coach, is named by the scope
// that talks to them. `webhook:` names one trigger, never a package, and a
// trait scope names a meaning, which no package is.
func (g Grant) NamesPackage(pkg string) bool {
	if pkg == "" {
		return false
	}
	return slices.ContainsFunc(g.scopes, func(s Scope) bool {
		switch s.Verb {
		case Read, Write, Agents, Decide, Call:
		default:
			return false
		}
		if _, ok := s.Trait(); ok {
			return false
		}
		if vocabulary.IsTypeGlob(s.Pattern) {
			return vocabulary.MatchTypeGlob(s.Pattern, pkg+"/")
		}
		return vocabulary.KindPackage(s.Pattern) == pkg
	})
}

// NamesAnyPackage reports whether one of the grant's scopes is of a verb that
// names a package (NamesPackage): read, write, agents, decide or call.
func (g Grant) NamesAnyPackage() bool {
	return slices.ContainsFunc(g.scopes, func(s Scope) bool {
		switch s.Verb {
		case Read, Write, Agents, Decide, Call:
			return true
		}
		return false
	})
}

// Covers reports whether the grant already holds what want asks for: a scope
// of the same verb, or a write for a read, over a pattern at least as wide.
// internal is whether want names one kind whose declared purpose is
// `internal`, which a glob never reaches, so only the exact scope covers it;
// it means nothing for a glob or for the agent and webhook verbs. A webhook
// scope covers its own trigger alone.
func (g Grant) Covers(want Scope, internal bool) bool {
	return slices.ContainsFunc(g.scopes, func(held Scope) bool { return Covers(held, want, internal) })
}

// Covers reports whether one held scope covers what want asks for, by the
// rule Grant.Covers applies to each of a grant's scopes. It is the one
// comparison of two scopes by reach, so a closure's reach held to a scope
// (Approved.Covers) and to an earlier closure's reach (catalog.ReachGrows)
// cannot disagree with the page's own folding.
func Covers(held, want Scope, internal bool) bool {
	switch {
	case held.Verb == want.Verb:
	case held.Verb == Write && want.Verb == Read:
	default:
		return false
	}
	if held.Pattern == want.Pattern {
		return true
	}
	if _, ok := want.Trait(); ok {
		// What a trait scope reaches is `*`'s among the kinds that bind the
		// trait, so only `*` holds it whole.
		return held.Pattern == "*"
	}
	if _, ok := held.Trait(); ok {
		// Which kinds bind a trait is the registry's to say and moves with
		// it, so a trait scope covers no scope but itself.
		return false
	}
	if want.Verb == Webhook || !vocabulary.IsTypeGlob(held.Pattern) {
		return false
	}
	if vocabulary.IsTypeGlob(want.Pattern) {
		return strings.HasPrefix(globPrefix(want.Pattern), globPrefix(held.Pattern))
	}
	if internal && !want.OverAgents() {
		return false
	}
	return vocabulary.MatchTypeGlob(held.Pattern, want.Pattern)
}

// globPrefix is what every name a glob matches starts with: nothing for `*`,
// `<authority>/` for `<authority>/*`, `<authority>/<package>/` for a package.
func globPrefix(glob string) string { return strings.TrimSuffix(glob, "*") }

// Writes reports whether the grant writes records of kind. A trait scope
// never writes an account, a kind that binds `accountconfig`: the engine
// refuses an app's token that write already, and ReachCovered reads Writes,
// so neither does a trait scope cover code an approval installs that would
// (decision 0225).
func (g Grant) Writes(kind Kind) bool {
	if inUnwritable(kind.Ref) {
		return false
	}
	return slices.ContainsFunc(g.scopes, func(s Scope) bool {
		if s.Verb != Write {
			return false
		}
		if _, ok := traitOf(s.Pattern); ok && kind.binds(vocabulary.TraitAccountConfigCore) {
			return false
		}
		return matches(s.Pattern, kind)
	})
}

// Calls reports whether the grant calls the function whose id is function,
// `<authority>/<package>/<name>` in full. A glob covers a function declared
// after the approval, and never one of the core or llm package.
func (g Grant) Calls(function string) bool {
	authority, pkg, name := vocabulary.SplitKindRef(function)
	if authority == "" || !vocabulary.ValidPackage(pkg) || !vocabulary.ValidName(name) || inUnwritable(function) {
		return false
	}
	return slices.ContainsFunc(g.scopes, func(s Scope) bool {
		return s.Verb == Call && vocabulary.MatchTypeGlob(s.Pattern, function)
	})
}

// CallsAny reports whether the grant holds any call scope.
func (g Grant) CallsAny() bool { return g.has(Call) }

// ReachCovered compares code's effective reach with the token's. Code globs
// can read internal kinds that scope globs exclude. Core and llm globs need
// full access even before new kinds arrive; other globs need exact scopes
// for every currently matched internal kind, supplied in internalKinds.
// kind is the kind an exact pattern names, as the registry declares it now
// (whether it is internal, the traits it binds), so a trait scope covers
// the reach of an exact kind that binds the trait; it is unread for a glob,
// which no trait scope covers. Callers must recheck against the live
// registry on every admission.
func (g Grant) ReachCovered(verb Verb, pattern string, kind Kind, internalKinds ...string) bool {
	if !vocabulary.IsTypeGlob(pattern) {
		kind.Ref = pattern
		if verb == Write {
			return g.Writes(kind)
		}
		return g.Reads(kind)
	}
	if globReachesUnwritable(pattern) {
		return false
	}
	for _, k := range internalKinds {
		if vocabulary.MatchTypeGlob(pattern, k) && !vocabulary.AuthKinds[k] &&
			!g.ReachCovered(verb, k, Kind{Internal: true}) {
			return false
		}
	}
	return g.Covers(Scope{Verb: verb, Pattern: pattern}, false)
}

// globReachesUnwritable reports whether a glob covers a kind of an
// unwritable package: `*`, the package's authority or the package itself.
func globReachesUnwritable(glob string) bool {
	for _, pkg := range unwritable {
		if vocabulary.MatchTypeGlob(glob, pkg+"/x") {
			return true
		}
	}
	return false
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
// keeps: the auth kinds are never reached, and neither a glob nor a trait
// scope reaches an internal kind.
func matches(pattern string, kind Kind) bool {
	if vocabulary.AuthKinds[kind.Ref] {
		return false
	}
	if trait, ok := traitOf(pattern); ok {
		return !kind.Internal && kind.binds(trait)
	}
	if vocabulary.IsTypeGlob(pattern) {
		return !kind.Internal && vocabulary.MatchTypeGlob(pattern, kind.Ref)
	}
	return pattern == kind.Ref
}
