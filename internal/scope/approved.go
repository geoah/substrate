package scope

import (
	"slices"

	"github.com/geoah/substrate/internal/vocabulary"
)

// Code an approval installs reaches no further than the scopes it approves
// (decision 0215): every kind a package's functions, agents, triggers and
// mappings read or write must be one the approved scopes cover, or the
// package is not installed. Approved is the scopes an approval leaves
// checked, as that rule reads them: `full` covers everything, and any other
// set covers a reach exactly as a Grant of those scopes would cover a
// request for it, so an app's token never reaches less than the code its
// approval installed.

// Approved is the scopes an approval leaves checked. The zero value covers
// nothing: a plan that forgets to say what was approved blocks every
// package that ships code rather than admit one.
type Approved struct {
	full   bool
	scopes []Scope
}

// FullApproval covers everything: the owner approved `full` access, which
// is a token without scopes (decision 0204).
func FullApproval() Approved { return Approved{full: true} }

// ApprovalOf reads the checked scopes in their resolved spelling: `full`
// alone, or a list CheckStored admits. An empty list covers nothing.
func ApprovalOf(scopes []string) (Approved, error) {
	if slices.Equal(scopes, []string{FullScope.String()}) {
		return FullApproval(), nil
	}
	if len(scopes) == 0 {
		return Approved{}, nil
	}
	if err := CheckStored(scopes); err != nil {
		return Approved{}, err
	}
	out := Approved{scopes: make([]Scope, 0, len(scopes))}
	for _, raw := range scopes {
		s, _ := Parse(raw)
		out.scopes = append(out.scopes, s)
	}
	return out, nil
}

// ApprovalFrom is what a stored grant approves: the token's own scopes, so
// a check with the token says whether the packages it asks for reach past
// what the token itself may do.
func ApprovalFrom(g Grant) Approved { return Approved{scopes: slices.Clone(g.scopes)} }

// Full reports whether everything is covered.
func (a Approved) Full() bool { return a.full }

// Covers uses the same effective reach rule as call and chat admission.
// kind is the kind an exact pattern names (Grant.ReachCovered), and
// internalKinds must include the installed and candidate internal kinds.
func (a Approved) Covers(pattern string, write bool, kind Kind, internalKinds ...string) bool {
	if a.full {
		return true
	}
	verb := Read
	if write {
		verb = Write
	}
	return (Grant{scopes: a.scopes}).ReachCovered(verb, pattern, kind, internalKinds...)
}

// Covering is the scope that would cover one reach, as a block names it:
// `write:<pattern>` or `read:<pattern>`, and `full` where no scope can,
// since no scope reaches the auth kinds or writes the core or llm package.
func Covering(pattern string, write bool) string {
	if vocabulary.IsTypeGlob(pattern) && globReachesUnwritable(pattern) {
		return FullScope.String()
	}
	s := Scope{Verb: Read, Pattern: pattern}
	if write {
		s.Verb = Write
	}
	if _, err := Parse(s.String()); err != nil {
		return FullScope.String()
	}
	return s.String()
}
