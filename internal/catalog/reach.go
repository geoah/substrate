package catalog

import (
	"slices"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The REACH of a closure (decision 0215) is every kind its code touches
// once installed: what its functions and agents read and write, the kinds
// its triggers watch, and the source and target of its mappings. An
// approval holds it to the scopes it approves (internal/appsetup), and an
// upgrade from the publisher waits for the owner when it grows
// (ReachGrows), so what the page said a package's code reaches stays true
// for as long as the package is installed.

// Reach is one kind, or one glob over kinds as the grant grammar spells it,
// that a closure's code reads or writes, with what reaches it.
type Reach struct {
	Pattern string
	Write   bool
	// By is what reaches it, in document order: each a function, agent,
	// trigger or mapping of the closure.
	By []ReachBy
}

// ReachBy is one piece of code that reaches a kind: its Type is
// vocabulary.DocFunction, DocAgent, DocRecordMapping or TriggerKind's word,
// `trigger`, and ID its id.
type ReachBy struct {
	Type string
	ID   string
}

// AgentFact is one agent a closure ships: the kinds it reads and writes, as
// its `permissions` declare them, each a kind or a glob. Its tools add no
// reach of their own: a host tool runs under these permissions, and a
// bundle function it calls runs under that function's.
type AgentFact struct {
	ID     string
	Reads  []string
	Writes []string
}

// MappingFact is one mapping a closure ships: it reads its source kind and
// writes its target.
type MappingFact struct {
	ID   string
	From string
	To   string
}

// Agents are the agents the closure ships, in document order.
func (b *Bundle) Agents() []AgentFact { return b.agents }

// Mappings are the mappings the closure ships, in document order, the
// suggested ones among them: one lands as soon as its source package is
// held, under the same approval.
func (b *Bundle) Mappings() []MappingFact { return b.mappings }

// Reach is everything the closure's code reaches, one entry per kind or
// glob and verb, in the order the documents first name each.
func (b *Bundle) Reach() []Reach { return b.reach }

// readReach fills b.reach from the facts readFacts read.
func (b *Bundle) readReach() {
	var out []Reach
	add := func(pattern string, write bool, by ReachBy) {
		if pattern == "" {
			return
		}
		i := slices.IndexFunc(out, func(r Reach) bool { return r.Pattern == pattern && r.Write == write })
		if i < 0 {
			out = append(out, Reach{Pattern: pattern, Write: write})
			i = len(out) - 1
		}
		if !slices.Contains(out[i].By, by) {
			out[i].By = append(out[i].By, by)
		}
	}
	for _, f := range b.functions {
		by := ReachBy{Type: vocabulary.DocFunction, ID: f.ID}
		for _, k := range f.Reads {
			add(k, false, by)
		}
		for _, k := range f.Writes {
			add(k, true, by)
		}
	}
	for _, a := range b.agents {
		by := ReachBy{Type: vocabulary.DocAgent, ID: a.ID}
		for _, k := range a.Reads {
			add(k, false, by)
		}
		for _, k := range a.Writes {
			add(k, true, by)
		}
	}
	for _, m := range b.mappings {
		by := ReachBy{Type: vocabulary.DocRecordMapping, ID: m.ID}
		add(m.From, false, by)
		add(m.To, true, by)
	}
	for _, t := range b.triggers {
		by := ReachBy{Type: vocabulary.KindName(vocabulary.TriggerKind), ID: t.ID}
		for _, k := range t.Kinds {
			add(k, false, by)
		}
	}
	b.reach = out
}

// ReachGrows is what candidate reaches that held does not: each of
// candidate's entries no entry of held covers, by the rule a scope covers a
// reach (scope.Covers: a write covers a read, a glob what it matches). Nil
// when the candidate reaches no further, which is what lets an upgrade
// from the publisher land without the owner (decision 0213); any entry
// means the upgrade waits for them.
func ReachGrows(held, candidate []Reach) []Reach {
	var out []Reach
	for _, r := range candidate {
		want := asScope(r)
		covered := slices.ContainsFunc(held, func(h Reach) bool { return scope.Covers(asScope(h), want, false) })
		if !covered {
			out = append(out, r)
		}
	}
	return out
}

func asScope(r Reach) scope.Scope {
	s := scope.Scope{Verb: scope.Read, Pattern: r.Pattern}
	if r.Write {
		s.Verb = scope.Write
	}
	return s
}
