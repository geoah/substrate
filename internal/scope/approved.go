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

// Covers reports whether the approval covers one reach: a kind, or a glob
// over kinds as the grant grammar spells one, read or written. A write
// scope covers a read; a glob scope covers a glob reach it is at least as
// wide as, and a kind reach it matches, unless internal says the kind's
// declared purpose is `internal`, which only its exact scope reaches. The
// auth kinds are covered by nothing, and no scope writes the core or llm
// package, so a write there is covered by `full` alone.
func (a Approved) Covers(pattern string, write, internal bool) bool {
	if a.full {
		return true
	}
	want := Scope{Verb: Read, Pattern: pattern}
	if write {
		want.Verb = Write
	}
	if !vocabulary.IsTypeGlob(pattern) {
		if vocabulary.AuthKinds[pattern] || (write && inUnwritable(pattern)) {
			return false
		}
	}
	return slices.ContainsFunc(a.scopes, func(held Scope) bool { return Covers(held, want, internal) })
}

// Covering is the scope that would cover one reach, as a block names it:
// `write:<pattern>` or `read:<pattern>`, and `full` where no scope can,
// since no scope reaches the auth kinds or writes the core or llm package.
func Covering(pattern string, write bool) string {
	s := Scope{Verb: Read, Pattern: pattern}
	if write {
		s.Verb = Write
	}
	if _, err := Parse(s.String()); err != nil {
		return FullScope.String()
	}
	return s.String()
}
