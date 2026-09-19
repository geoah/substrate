package vocabulary

import (
	"fmt"
	"sort"
)

// THE MAPPING OWNS ITS LINK (record 96). A recordmapping names `property:`, and
// that property is the reference on the SOURCE kind pointing at the subject.
// Until record 96 the source kind had to declare it, so a provider had to know
// the word its consumer would use — `task` on `github/issue`, `person` on
// `github/user` — and a second consumer with a different word needed the
// provider to have anticipated it.
//
// Now the mapping synthesizes it. The slot storage is unchanged: it is a
// property the write path fills, exactly as before. What changed is who
// declares it, and this file is that half — a reconcile over the registry's
// resolved mappings, re-run by every door that mutates the registry, which
// is what lets a mapping installed long after its source kind reach back and
// give that kind its slot.
//
// A source kind that STILL declares the slot keeps it: the declaration is the
// author's (its description, its `required:`) and the mapping takes over the
// pin and the marker. That is what makes the change invisible to a repository
// holding a bundle written before it — and what makes a re-applied document
// that carries the slot round-trip rather than collide.

// subjectPropertyOf builds the reference a mapping synthesizes on its source
// kind when the kind declares nothing under that name.
//
// SINGLE, `mustExist`, never cascading: a source record describes ONE subject
// that exists, and deleting the subject must not collect the records that
// describe it. NOT `required`, because the slot is filled by the write path's
// own match-or-mint and not by the writer, and a required slot would refuse
// the very create that fills it. `managed` is the declaration that a client
// may echo the value and never change it (Property.Managed): re-pointing is
// merge and split, which is the subject rule and stricter than managed alone.
func subjectPropertyOf(m *Mapping) *Property {
	return &Property{
		Name:     m.Property,
		Datatype: DatatypeReference,
		To:       m.To,
		Description: fmt.Sprintf("The %s this record describes, linked by mapping %s.",
			KindName(m.To), m.Identity()),
		MustExist: true,
		Subject:   true,
		Managed:   true,
		MappedBy:  m.Identity(),
	}
}

// adoptSubjectProperty is the other half: the source kind DOES declare the
// slot, so the declaration stands and the mapping stamps it. The pin is the
// mapping's whichever way the declaration left it (record 49 had the engine
// apply this at every write; it is applied once, here, now), and `subject`,
// `mustExist` and the marker are asserted so a declared slot and a synthesized
// one are the same property to everything downstream.
func adoptSubjectProperty(declared *Property, m *Mapping) *Property {
	p := *declared
	p.To, p.ToTrait = m.To, ""
	p.Subject, p.MustExist, p.Managed = true, true, true
	p.MappedBy = m.Identity()
	p.mappedOver = declared
	return &p
}

// mappingSubjectProblems reconciles every source kind's subject slot against
// the registry's resolved mappings, and reports the collisions.
//
// RECONCILE, not append: it runs on every Finalize, Install and InstallAll, on
// a registry that may already carry the slots an earlier pass put there, so it
// first undoes its own previous work (Property.mappedOver) and then applies
// what the current mapping set asks for. That is what removes the slot when
// the mapping goes: a registry rebuilt without the mapping rebuilds the kind
// without the property.
//
// It is COPY-ON-WRITE. A registry clone shares its packages and kinds with the
// registry it was cloned from (Registry.Clone), and a candidate registry is a
// clone: mutating a kind in place would leak a candidate's mapping into the
// live registry and outlive the apply that was refused. So a kind that changes
// is REPLACED — a copy of the kind inside a copy of its package — and only the
// cloned index points at it.
func (r *Registry) mappingSubjectProblems() []string {
	var problems []string
	// The desired set, by source kind: property name -> the mapping that owns
	// it. A mapping whose source this repository does not hold is skipped;
	// resolveMapping already reported it, naming the package to import.
	desired := map[string]map[string]*Mapping{}
	for _, m := range r.Mappings() {
		if _, ok := r.ByIdentity(m.From); !ok {
			continue
		}
		if desired[m.From] == nil {
			desired[m.From] = map[string]*Mapping{}
		}
		// Two mappings on one slot are refused by mappingInvariantProblems,
		// which names both; taking the first here keeps this pass from
		// depending on map order while that problem is reported.
		if _, taken := desired[m.From][m.Property]; !taken {
			desired[m.From][m.Property] = m
		}
	}
	updated := map[string]*Kind{}
	for _, t := range r.Kinds() {
		next, changed, kindProblems := reconcileSubjectSlots(t, desired[t.Identity])
		problems = append(problems, kindProblems...)
		if changed {
			updated[t.Identity] = next
		}
	}
	if len(updated) > 0 {
		r.replaceKinds(updated)
	}
	return problems
}

// reconcileSubjectSlots returns the kind as the mapping set asks for it: the
// declared properties with this pass's own previous work undone, plus one slot
// per mapping. `changed` is false when the answer is the kind itself, which is
// the common case and the one that must not copy.
func reconcileSubjectSlots(t *Kind, want map[string]*Mapping) (*Kind, bool, []string) {
	var problems []string
	// base is the kind's properties with every earlier synthesis undone: a
	// purely synthesized slot disappears, an adopted one reverts to the
	// declaration it was stamped onto.
	base := make(map[string]*Property, len(t.Props))
	order := make([]string, 0, len(t.PropOrder))
	for _, n := range t.PropOrder {
		p := t.Props[n]
		if p.MappedBy != "" {
			if p.mappedOver == nil {
				continue
			}
			p = p.mappedOver
		}
		base[n] = p
		order = append(order, n)
	}
	next := make(map[string]*Property, len(base)+len(want))
	for n, p := range base {
		next[n] = p
	}
	added := make([]string, 0, len(want))
	for _, name := range sortedMappingNames(want) {
		m := want[name]
		declared, ok := base[name]
		switch {
		case !ok:
			next[name] = subjectPropertyOf(m)
			added = append(added, name)
		case declared.Datatype == DatatypeReference && declared.Subject:
			next[name] = adoptSubjectProperty(declared, m)
		default:
			// THE COLLISION. A mapping's property is its own: a source kind
			// that declares the name for something else would have the
			// mapping silently take the slot over, so the mapping is refused
			// and the author picks the other word.
			problems = append(problems, fmt.Sprintf(
				"%s %s: data.property: %s already declares %q as %s — a mapping's property is its own, so name another or drop that declaration",
				DocRecordMapping, m.Identity(), m.From, name, declared.Datatype))
		}
	}
	if len(added) == 0 && sameProps(t.Props, next) {
		return t, false, problems
	}
	// A synthesized name lands at the END of the declared order, so a
	// declaration's own key order is never reshuffled by a mapping install.
	sort.Strings(added)
	out := *t
	out.Props = next
	out.PropOrder = append(append([]string(nil), order...), added...)
	return &out, true, problems
}

// sameProps reports whether two property maps hold the same pointers under the
// same names — the cheap "nothing to do" test the reconcile takes to avoid
// copying a kind on every registry mutation.
func sameProps(a, b map[string]*Property) bool {
	if len(a) != len(b) {
		return false
	}
	for n, p := range a {
		if b[n] != p {
			return false
		}
	}
	return true
}

func sortedMappingNames(m map[string]*Mapping) []string {
	out := make([]string, 0, len(m))
	for n := range m {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// replaceKinds swaps updated kinds into this registry alone: a copy of each
// touched package holding the copied kind, and the two indexes repointed. The
// packages and kinds a clone SHARES with its origin are never written through
// (mappingSubjectProblems says why).
func (r *Registry) replaceKinds(updated map[string]*Kind) {
	r.mu.Lock()
	defer r.mu.Unlock()
	byPackage := map[string][]*Kind{}
	for _, t := range updated {
		byPackage[t.Package] = append(byPackage[t.Package], t)
	}
	for pkg, kinds := range byPackage {
		g, ok := r.packages[pkg]
		if !ok {
			continue
		}
		cp := *g
		cp.Kinds = make(map[string]*Kind, len(g.Kinds))
		for n, t := range g.Kinds {
			cp.Kinds[n] = t
		}
		for _, t := range kinds {
			cp.Kinds[t.Name] = t
		}
		r.packages[pkg] = &cp
	}
	for ident, t := range updated {
		old := r.byIdent[ident]
		r.byIdent[ident] = t
		for i, c := range r.byName[t.Name] {
			if c == old {
				r.byName[t.Name][i] = t
			}
		}
	}
}

// SynthesizedDeclaration renders a mapping-owned property the way a document
// would have declared it, for the READ surfaces alone: `KindByRef` merges
// these into the declaration it serves, so a console reading a kind's
// properties sees the slot the mapping put there, flagged `managed` and naming
// the mapping that owns it.
//
// It is never written back. The stored declaration row is the DOCUMENT, and
// the document does not declare this property — which is what keeps
// `get -o yaml | apply -f` honest and the reconcile the one place the slot
// comes from.
func (p *Property) SynthesizedDeclaration() map[string]any {
	if p.MappedBy == "" {
		return nil
	}
	out := map[string]any{
		"type":      string(DatatypeReference),
		"kind":      p.To,
		"mustExist": true,
		"subject":   true,
		"managed":   true,
		"mappedBy":  p.MappedBy,
	}
	if p.DisplayName != "" {
		out["displayName"] = p.DisplayName
	}
	if p.Description != "" {
		out["description"] = p.Description
	}
	if p.Required {
		out["required"] = true
	}
	return out
}

// MappedProperties lists a kind's mapping-owned properties, by name — what a
// read surface merges into the declaration it serves.
func (t *Kind) MappedProperties() map[string]*Property {
	var out map[string]*Property
	for _, n := range t.PropOrder {
		if p := t.Props[n]; p.MappedBy != "" {
			if out == nil {
				out = map[string]*Property{}
			}
			out[n] = p
		}
	}
	return out
}
