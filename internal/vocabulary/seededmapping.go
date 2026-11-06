package vocabulary

import (
	"fmt"
	"sort"
)

// A SEEDED MAPPING WAITS (decision record 0232). Core declares the mappings
// from the shipped providers onto `contact`, because only the owner of a
// mapping's target may declare it (record 49) and nothing else owns
// `contact`. Core is seeded into every repository and no token may edit it,
// so a core mapping that refused the registry would refuse the repository's
// own vocabulary with no edit to fix it. Instead it is HELD: left out of
// Mappings(), so nothing projects through it, gives its source a slot, or
// counts it in an invariant, for as long as any of these is true:
//
//   - its source kind is not installed (the provider is absent);
//   - it does not resolve against the source as installed (an older
//     provider version that lacks a path it reads);
//   - a mapping of another package already fills its slot, or its source
//     declares the slot's name as something else;
//   - another package's mapping reads its target or writes its source, which
//     would break the bipartite rule.
//
// The answer is a function of the registry alone, recomputed at every build
// (Finalize, Install, InstallAll, Remove), and nothing about it is stored.
// So installing the provider is what switches the mapping on, through the
// same apply that links the sources a new mapping finds (record 0107), and
// uninstalling it switches it off without the stranded-mapping refusal a
// package's own mapping would raise (record 49 item 4).
//
// A mapping of another package never waits: a mapping its owner wrote and
// can edit is refused where it does not fit, as before.

// mappingsWait reports whether a package's mappings wait for their source
// instead of refusing. Named rather than read off Package.Source, for the
// reason resolvePackage gives for `notifies:`: the loader stamps whatever
// source its caller hands it.
func mappingsWait(pkg string) bool {
	return pkg == PackageCore
}

// HeldMapping is a seeded mapping the registry leaves out, with the reason.
type HeldMapping struct {
	Mapping *Mapping
	Reason  string
}

// HeldMappings lists the seeded mappings this registry holds, by identity.
func (r *Registry) HeldMappings() []HeldMapping {
	r.mu.RLock()
	held := r.held
	r.mu.RUnlock()
	var out []HeldMapping
	for _, g := range r.PackageList() {
		if !mappingsWait(g.Identity) {
			continue
		}
		for _, n := range g.MappingOrder {
			m := g.Mappings[n]
			if reason, ok := held[m.Identity()]; ok {
				out = append(out, HeldMapping{Mapping: m, Reason: reason})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Mapping.Identity() < out[j].Mapping.Identity() })
	return out
}

// holdSeededMappings recomputes which seeded mappings wait. It runs after
// every package's reference pins resolved and before the invariants and the
// subject-slot reconcile, which read Mappings() and so never see a held one.
func (r *Registry) holdSeededMappings() {
	var seeded []*Mapping
	slots := map[mappingSlot]*Mapping{}
	sources := map[string]*Mapping{}
	targets := map[string]*Mapping{}
	for _, g := range r.PackageList() {
		for _, n := range g.MappingOrder {
			m := g.Mappings[n]
			if mappingsWait(m.Package) {
				seeded = append(seeded, m)
				continue
			}
			slots[mappingSlot{from: m.From, property: m.Property}] = m
			if _, ok := sources[m.From]; !ok {
				sources[m.From] = m
			}
			if _, ok := targets[m.To]; !ok {
				targets[m.To] = m
			}
		}
	}
	held := map[string]string{}
	for _, m := range seeded {
		if reason := r.seededMappingWaits(m, slots, sources, targets); reason != "" {
			held[m.Identity()] = reason
		}
	}
	r.mu.Lock()
	r.held = held
	r.mu.Unlock()
}

// seededMappingWaits returns why a seeded mapping waits, or "" when it is
// live. slots, sources and targets index the other packages' mappings by
// (source, property), by source kind and by target kind.
func (r *Registry) seededMappingWaits(m *Mapping, slots map[mappingSlot]*Mapping, sources, targets map[string]*Mapping) string {
	from, ok := r.ByIdentity(m.From)
	if !ok {
		return fmt.Sprintf("its source kind %s is not installed", m.From)
	}
	if other, ok := slots[mappingSlot{from: m.From, property: m.Property}]; ok {
		return fmt.Sprintf("mapping %s already fills %s.%s", other.Identity(), m.From, m.Property)
	}
	if other, ok := sources[m.To]; ok {
		return fmt.Sprintf("mapping %s reads %s, and a target is never a source", other.Identity(), m.To)
	}
	if other, ok := targets[m.From]; ok {
		return fmt.Sprintf("mapping %s writes %s, and a source is never a target", other.Identity(), m.From)
	}
	// A slot this mapping synthesized on an earlier pass is its own. Any
	// other declaration under the name is the source's, and only a subject
	// reference may be adopted.
	if sp, ok := from.Props[m.Property]; ok && sp.MappedBy != m.Identity() &&
		(sp.MappedBy != "" || sp.Datatype != DatatypeReference || !sp.Subject) {
		return fmt.Sprintf("%s declares %s itself", m.From, m.Property)
	}
	if problems := r.resolveMapping(m); len(problems) > 0 {
		return fmt.Sprintf("it does not fit %s as installed: %s", m.From, problems[0])
	}
	return ""
}

// heldMapping reports whether Mappings() leaves m out. A seeded mapping whose
// source is absent is out even before the first build has computed the set.
func (r *Registry) heldMapping(m *Mapping, held map[string]string) bool {
	if !mappingsWait(m.Package) {
		return false
	}
	if _, ok := held[m.Identity()]; ok {
		return true
	}
	_, ok := r.ByIdentity(m.From)
	return !ok
}
