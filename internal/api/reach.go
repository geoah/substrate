package api

import (
	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// viewReach is what a request may read and write through the scoped view: an
// app's token held to the scopes its approval gave it (decision 0194), or a
// person who is not an admin held to the rules of the roles they hold
// (decision 0276). The engine checks every write again by the same rules;
// the view is where a read is held to them, and where a door their rules do
// not open is answered.
type viewReach interface {
	// Reads and Writes report whether a kind is read or written whole.
	Reads(scope.Kind) bool
	Writes(scope.Kind) bool
	// NamesAnyPackage and NamesPackage say whether the package rows, which
	// are declarations, are read for a package the reach needs set up.
	NamesAnyPackage() bool
	NamesPackage(pkg string) bool
	// Delivers is a webhook the reach delivers to.
	Delivers(trigger string) bool
	// Talks, TalksTo, DecidesAny and Decides are the agents the reach talks
	// to and the proposals of theirs it decides.
	Talks() bool
	TalksTo(agent string) bool
	DecidesAny() bool
	Decides(agent string) bool
	// CallsAny and Calls are the functions the reach calls.
	CallsAny() bool
	Calls(function string) bool
	// Covers reports whether a pattern code reads or writes (an agent the
	// reach talks to, a function it calls) stays within the reach, kinds
	// declared later included.
	Covers(verb scope.Verb, pattern string, kind scope.Kind, registry map[string]substrate.KindInfo) bool
}

// appReach is an app token's grant.
type appReach struct{ scope.Grant }

func (a appReach) Covers(verb scope.Verb, pattern string, kind scope.Kind, registry map[string]substrate.KindInfo) bool {
	return a.ReachCovered(verb, pattern, kind, internalKinds(registry)...)
}

// personReach is a person's roles. A person talks to every agent and calls
// every function whose reach their rules cover, the code then acting for
// them: what it writes is held by the engine to both its declaration and the
// person's rules. Package rows are declarations, a control kind, so only an
// admin reads them, and an admin is never held here. Webhooks are an app's.
// The proposals agents put up are decided by a hand whose rules write
// change requests; the accept writes the target as that hand, which the
// engine holds to its rules.
type personReach struct{ roles scope.Roles }

func (p personReach) Reads(k scope.Kind) bool  { return p.roles.Reaches(scope.Read, k) }
func (p personReach) Writes(k scope.Kind) bool { return p.roles.Reaches(scope.Write, k) }
func (personReach) NamesAnyPackage() bool      { return false }
func (personReach) NamesPackage(string) bool   { return false }
func (personReach) Delivers(string) bool       { return false }
func (personReach) Talks() bool                { return true }
func (personReach) TalksTo(string) bool        { return true }
func (p personReach) DecidesAny() bool {
	return p.roles.Reaches(scope.Write, scope.Kind{Ref: vocabulary.KindRecordPatchRequest})
}
func (p personReach) Decides(string) bool { return p.DecidesAny() }
func (personReach) CallsAny() bool        { return true }
func (personReach) Calls(string) bool     { return true }

func (p personReach) Covers(verb scope.Verb, pattern string, kind scope.Kind, registry map[string]substrate.KindInfo) bool {
	return p.roles.Covers(verb, pattern, kind, controlKinds(registry))
}

// controlKinds are the registry's control kinds and sign-in material.
func controlKinds(registry map[string]substrate.KindInfo) []string {
	var out []string
	for ref, k := range registry {
		if k.Control != "" {
			out = append(out, ref)
		}
	}
	return out
}

// rolesOf reads the roles the engine answered for a person, in a repository
// whose names that spell `~` are spellings.
func rolesOf(held []substrate.RoleRules, spellings ...string) scope.Roles {
	roles := make([]scope.Role, len(held))
	for i, h := range held {
		roles[i] = scope.Role{ID: h.ID, Rules: make([]scope.Rule, len(h.Rules))}
		for j, r := range h.Rules {
			rule := scope.Rule{Kinds: r.Kinds}
			for _, v := range r.Verbs {
				rule.Verbs = append(rule.Verbs, scope.Verb(v))
			}
			roles[i].Rules[j] = rule
		}
	}
	return scope.NewRoles(roles, spellings...)
}

// scopeKindOf is a registry kind as a reach is held to it.
func scopeKindOf(ref string, k substrate.KindInfo) scope.Kind {
	sk := scope.KindOf(ref, k.Definition)
	sk.Control = k.Control != ""
	return sk
}
