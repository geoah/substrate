package scope

import (
	"slices"
	"strings"

	"github.com/geoah/substrate/internal/vocabulary"
)

// A ROLE is a record of `core/role`: a named list of rules, each a list of
// verbs over a list of patterns in the grammar a read or write scope uses
// (decision 0275). Rules only add. What a person may do in a repository is
// what the roles their actor names allow, read on each request.
//
// A pattern reaches every kind it matches but a control kind and sign-in
// material, which it reaches only by naming that kind exactly
// (vocabulary.ControlClassOf, decision 0080). Unlike an app's scope, a glob
// reaches a kind whose purpose is `internal`: a role says what a person of
// the repository may do, and the person's own sign-in reaches every kind.
//
// The role `admin` may do everything, control kinds and sign-in material
// included: it reaches them by being `admin`, which no rule spells. It is
// also the role the fixed check on roles reads: only a hand that holds it
// changes a role, or the roles an actor holds, and the last one is not
// taken away.

// RoleAdmin, RoleEditor and RoleViewer are the record ids of the three roles
// that ship.
const (
	RoleAdmin  = "admin"
	RoleEditor = "editor"
	RoleViewer = "viewer"
)

// Rule is one rule of a role: the verbs it allows over the kinds its
// patterns reach.
type Rule struct {
	Verbs []Verb
	Kinds []string
}

// Role is one role as a request reads it: its record id and its rules.
type Role struct {
	ID    string
	Rules []Rule
}

// ShippedRole is a role the substrate writes into every repository, with
// the words a person reads it by.
type ShippedRole struct {
	Role
	Name, Description string
}

// ShippedRoles are the three roles every repository holds. `admin`'s rule is
// the same as `editor`'s: what it reaches beyond it, it reaches by being
// `admin`.
var ShippedRoles = []ShippedRole{
	{
		Role: Role{ID: RoleAdmin, Rules: []Rule{{Verbs: []Verb{Read, Write}, Kinds: []string{"*"}}}},
		Name: "Admin", Description: "may do everything, the control kinds and sign-in material included, and changes the roles people hold",
	},
	{
		Role: Role{ID: RoleEditor, Rules: []Rule{{Verbs: []Verb{Read, Write}, Kinds: []string{"*"}}}},
		Name: "Editor", Description: "reads and writes every kind that is not a control kind",
	},
	{
		Role: Role{ID: RoleViewer, Rules: []Rule{{Verbs: []Verb{Read}, Kinds: []string{"*"}}}},
		Name: "Viewer", Description: "reads every kind that is not a control kind",
	},
}

// Roles are the roles one hand holds, read in the repository whose full name,
// authority, is another spelling of `~`, where its own kinds are (decision
// record 0259).
type Roles struct {
	roles     []Role
	authority string
}

// NewRoles holds roles, a pattern naming authority read as `~`.
func NewRoles(roles []Role, authority string) Roles {
	return Roles{roles: roles, authority: authority}
}

// Admin reports whether the roles include `admin`.
func (r Roles) Admin() bool {
	return slices.ContainsFunc(r.roles, func(role Role) bool { return role.ID == RoleAdmin })
}

// Held are the roles held, in the order held.
func (r Roles) Held() []Role { return slices.Clone(r.roles) }

// IDs are the record ids of the roles held, in the order held.
func (r Roles) IDs() []string {
	out := make([]string, len(r.roles))
	for i, role := range r.roles {
		out[i] = role.ID
	}
	return out
}

// Reaches reports whether the roles let verb reach kind, write including
// read of the same kind. It is the one answer to whether a role reaches a
// control kind: `admin` reaches every kind; no rule writes an actor or a
// role, which is the fixed check on who may do what; a rule reaches any
// other control kind or sign-in material only by naming it exactly, and any
// other kind by any of its patterns.
func (r Roles) Reaches(verb Verb, kind Kind) bool {
	if r.Admin() {
		return true
	}
	// Who may do what is the fixed check, never a rule: no rule hands out a
	// write of an actor or a role, the roles an actor holds among them.
	if verb == Write && (kind.Ref == vocabulary.CoreKind("actor") || kind.Ref == vocabulary.CoreKind("role")) {
		return false
	}
	for _, role := range r.roles {
		for _, rule := range role.Rules {
			if !ruleAllows(rule, verb) {
				continue
			}
			for _, p := range rule.Kinds {
				if r.patternReaches(p, kind) {
					return true
				}
			}
		}
	}
	return false
}

// Covers reports whether the roles let verb reach every kind pattern names,
// those declared later included: an exact pattern as Reaches says; a glob
// when a rule's pattern holds the whole glob and every control kind and
// sign-in material it matches among controlKinds is reached by name.
func (r Roles) Covers(verb Verb, pattern string, kind Kind, controlKinds []string) bool {
	if r.Admin() {
		return true
	}
	if !vocabulary.IsTypeGlob(pattern) {
		kind.Ref = pattern
		return r.Reaches(verb, kind)
	}
	for _, k := range controlKinds {
		if vocabulary.MatchTypeGlob(pattern, k) && !r.Reaches(verb, Kind{Ref: k, Control: true}) {
			return false
		}
	}
	for _, role := range r.roles {
		for _, rule := range role.Rules {
			if !ruleAllows(rule, verb) {
				continue
			}
			for _, p := range rule.Kinds {
				p = r.resolve(p)
				if p == pattern || (vocabulary.IsTypeGlob(p) && strings.HasPrefix(globPrefix(pattern), globPrefix(p))) {
					return true
				}
			}
		}
	}
	return false
}

func ruleAllows(rule Rule, verb Verb) bool {
	return slices.Contains(rule.Verbs, verb) || (verb == Read && slices.Contains(rule.Verbs, Write))
}

func (r Roles) resolve(pattern string) string {
	if trait, ok := traitOf(pattern); ok {
		return TraitPattern(vocabulary.LocalizeRef(trait, r.authority))
	}
	return vocabulary.LocalizeRef(pattern, r.authority)
}

func (r Roles) patternReaches(pattern string, kind Kind) bool {
	pattern = r.resolve(pattern)
	if pattern == kind.Ref {
		return true
	}
	if kind.Control || vocabulary.AuthKinds[kind.Ref] {
		return false
	}
	if trait, ok := traitOf(pattern); ok {
		return kind.binds(trait)
	}
	return vocabulary.IsTypeGlob(pattern) && vocabulary.MatchTypeGlob(pattern, kind.Ref)
}
