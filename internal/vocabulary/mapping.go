package vocabulary

import (
	"fmt"
	"sort"
	"strings"
)

// A recordmapping is the sixth manifest kind: it says how one source-record
// kind's properties reach the subject one of its reference properties names.
// That reference declares `subject: true` and the mapping names it, so the
// write path can refuse to move a subject without reading the mapping set.
// Everything record 33 ruled about it stands, restated here: created with its
// record, moved only by merge and split, never `onDelete: cascade`, and no
// reference anywhere else may land on a mapped source kind.

// Merge is how one target property combines contributions (§7.1): atomic —
// one source's value wins whole — or union, the deduped union of every live
// source's items, only onto repeated properties.
const (
	MergeAtomic = "atomic"
	MergeUnion  = "union"
)

// Mapping is one parsed recordmapping. From and To are full kind references,
// each resolved like a reference pin, and the declaring package owns at least
// one of them (record 49); Property names the declared `subject: true`
// reference on From that points at the subject.
type Mapping struct {
	Name string
	// Package is the identity of the package that declares it.
	Package  string
	From     string
	To       string
	Property string
	// Match is the ordered identifier probes (§6.1): when a source record
	// arrives without a subject, the first probe whose values find candidates
	// decides — exactly one candidate links, zero or several create a fresh
	// subject. May be empty: a link-only mapping always creates.
	Match []MatchRule
	// Map is assignment paths per target property, nothing else — no
	// expression language, computation stays in connector normalize (§7.1).
	// May be empty.
	Map      map[string]*MapRule
	MapOrder []string

	// Definition is the declaration's own data map, exactly as authored — what
	// the row stores as its properties.
	Definition map[string]any
}

// Identity is "<authority>/<package>/<name>".
func (m *Mapping) Identity() string { return m.Package + "/" + m.Name }

// MatchRule is one identifier probe: values extracted from the source record
// via From, looked up in the target's To property.
type MatchRule struct {
	From Path
	To   string
}

// MapRule is one target property's assignment.
type MapRule struct {
	Path  Path
	Merge string // MergeAtomic | MergeUnion
}

// Path is a parsed assignment path: `a` (a property), `a.b` (a
// field of an object property) or `a[].b` (that field across a repeated
// object property). Nothing else — no deeper nesting, no expressions.
type Path struct {
	Prop     string
	Field    string // empty for the bare-property form
	OverList bool   // the `[]` hop: Prop is repeated, the result is a list
}

// String renders the path back to its declared spelling.
func (p Path) String() string {
	switch {
	case p.Field == "":
		return p.Prop
	case p.OverList:
		return p.Prop + "[]." + p.Field
	default:
		return p.Prop + "." + p.Field
	}
}

// ParsePath parses the three path forms. Segments follow the one casing rule
// every declared name does.
func ParsePath(s string) (Path, error) {
	if s == "" {
		return Path{}, fmt.Errorf("a path is required (`a`, `a.b` or `a[].b`)")
	}
	prop, field, cut := strings.Cut(s, ".")
	p := Path{Prop: prop, Field: field}
	if over, ok := strings.CutSuffix(prop, "[]"); ok {
		p.Prop, p.OverList = over, true
		if !cut {
			// `a[]` alone extracts nothing: a repeated property is just `a`.
			return Path{}, fmt.Errorf("path %q: `[]` walks into a field — a repeated property is plainly %q", s, over)
		}
	}
	switch {
	case !ValidCamel(p.Prop):
		return Path{}, fmt.Errorf("path %q: %q must be %s", s, p.Prop, camelRule)
	case cut && strings.Contains(field, "."):
		return Path{}, fmt.Errorf("path %q: fields are one level deep (`a.b`, `a[].b`)", s)
	case cut && !ValidCamel(field):
		return Path{}, fmt.Errorf("path %q: %q must be %s", s, field, camelRule)
	}
	return p, nil
}

// columnProp resolves the column-backed properties a record carries but no
// manifest declares: title always, the temporals when a capability binds them.
// They are legal path sources and map targets (§7.1, hot props). `body` is NOT
// here: a kind that carries a body declares it (#68), so the declaration in
// t.Props resolves it, and a kind that does not declare body has none to map.
func columnProp(t *Kind, name string) (*Property, bool) {
	switch name {
	case "title":
		return &Property{Name: "title", Datatype: DatatypeString}, true
	case "at", "endsAt", "dueAt":
		if t.UsesHot(name) {
			return &Property{Name: name, Datatype: DatatypeDatetime}, true
		}
	}
	return nil, false
}

// PathProperty type-checks a path against a declared type: the terminal
// property, and whether evaluating the path against a stored row yields a
// list. The engine reuses it to evaluate map and match paths (§7.1); the
// loader is where a disagreement fails, on the manifest that caused it.
func PathProperty(t *Kind, p Path) (*Property, bool, error) {
	prop, ok := t.Props[p.Prop]
	if !ok {
		if prop, ok = columnProp(t, p.Prop); !ok {
			return nil, false, fmt.Errorf("record type %q declares no property %q", t.Identity, p.Prop)
		}
	}
	if p.Field == "" {
		return prop, prop.Repeated, nil
	}
	switch {
	case prop.Datatype != DatatypeObject:
		return nil, false, fmt.Errorf("%s.%s is %s, not an object — only object properties have fields", t.Identity, p.Prop, prop.Datatype)
	case p.OverList && !prop.Repeated:
		return nil, false, fmt.Errorf("%s.%s is not repeated — use %s.%s", t.Identity, p.Prop, p.Prop, p.Field)
	case !p.OverList && prop.Repeated:
		return nil, false, fmt.Errorf("%s.%s is repeated — use %s[].%s", t.Identity, p.Prop, p.Prop, p.Field)
	}
	f, ok := prop.Fields[p.Field]
	if !ok {
		return nil, false, fmt.Errorf("%s.%s has no field %q", t.Identity, p.Prop, p.Field)
	}
	return f, p.OverList, nil
}

// --- the loader's half ----------------------------------------------------

var mappingDataKeys = map[string]bool{
	"authority": true, "package": true, "from": true, "to": true, "property": true,
	"match": true, "map": true, "description": true,
}

var matchRuleKeys = map[string]bool{"from": true, "to": true}

var mapRuleKeys = map[string]bool{"path": true, "merge": true}

// parseMapping parses one recordmapping document. Everything that needs the
// target kind — the subject reference's shape, path type-checks, the bipartite
// rule — is deferred to Finalize/Install, like a reference pin: `to` may name a
// kind in an authority that has not loaded yet, and so may `from` (record 49).
func (l *loader) parseMapping(d Document) *Mapping {
	g := l.pkg
	where := DocRecordMapping + " " + d.ID
	l.checkKeys(where, d.Data, mappingDataKeys)
	local, ok := l.localName(where, d.ID, g.Identity)
	if !ok {
		return nil
	}
	m := &Mapping{
		Name: local, Package: g.Identity,
		From:       ReferentID(d.Data["from"], CoreKind(DocKind)),
		To:         ReferentID(d.Data["to"], CoreKind(DocKind)),
		Property:   mstr(d.Data, "property"),
		Map:        map[string]*MapRule{},
		Definition: d.Data,
	}
	// Both ends are spelled in full: a bare name would go ambiguous the day a
	// second authority ships the same word, at registration and not at review.
	fromPkg, fromLocal := KindPackage(m.From), KindName(m.From)
	switch {
	case m.From == "":
		l.errf("%s: data.from is required", where)
		return nil
	case fromPkg == "" || fromLocal == "":
		l.errf("%s: data.from is a full kind reference (\"providers.substrate.reamde.dev/linear/issue\"), never a bare one", where)
		return nil
	}
	switch {
	case m.To == "":
		l.errf("%s: data.to is required", where)
		return nil
	case !Qualified(m.To):
		l.errf("%s: data.to is a full type name (\"samples.substrate.reamde.dev/people/person\"), never a bare one (§6.1)", where)
		return nil
	}
	// WHO MAY DECLARE THIS MAPPING (record 49). Ownership of the TARGET is what
	// licenses a mapping: only the package that declares `to` may say what
	// projects onto it, so a provider ships mirrors and no mapping, and a
	// third package may declare none. `from` is therefore free to live
	// anywhere and resolves at Finalize/Install, the way a `to` pin does; a
	// source kind in this package is still checked here, where the closure is
	// in hand.
	if KindPackage(m.To) != g.Identity {
		l.errf("%s: data.to: %s is declared in %s, and a mapping onto a kind is declared by the package that owns that kind",
			where, m.To, KindPackage(m.To))
		return nil
	}
	if fromPkg == g.Identity {
		if _, ok := g.Kinds[fromLocal]; !ok {
			l.errf("%s: data.from: %s is not a declared type of %s", where, m.From, g.Identity)
			return nil
		}
	}
	if m.Property == "" {
		l.errf("%s: data.property is required — the `subject: true` reference on %s that names the subject", where, m.From)
		return nil
	}
	if _, isMap := d.Data["match"].(map[string]any); isMap {
		l.errf("%s: data.match: an ordered LIST of probes — order is which one decides", where)
		return nil
	}
	for i, mv := range mslice(d.Data, "match") {
		md := asMap(mv)
		mwhere := fmt.Sprintf("%s: data.match[%d]", where, i)
		l.checkKeys(mwhere, md, matchRuleKeys)
		from, err := ParsePath(mstr(md, "from"))
		if err != nil {
			l.errf("%s.from: %v", mwhere, err)
			continue
		}
		to := mstr(md, "to")
		if !ValidCamel(to) {
			l.errf("%s.to: %q must be %s", mwhere, to, camelRule)
			continue
		}
		m.Match = append(m.Match, MatchRule{From: from, To: to})
	}
	for tname, rv := range mmap(d.Data, "map") {
		mwhere := fmt.Sprintf("%s: data.map.%s", where, tname)
		if !ValidCamel(tname) {
			l.errf("%s: must be %s", mwhere, camelRule)
			continue
		}
		rule := &MapRule{Merge: MergeAtomic}
		// A rule is an OBJECT. The bare path string is refused: one property has
		// one shape, which is what lets the meta-kind declare a rule's own fields.
		// Nothing translates a stored mapping written that way: the rung that
		// did was deleted before the first release (#217), so the store it
		// comes from is refused at open.
		if s, bare := rv.(string); bare {
			l.errf("%s: %q is a bare path — a rule is an object: {path: %s}", mwhere, s, s)
			continue
		}
		rd := asMap(rv)
		l.checkKeys(mwhere, rd, mapRuleKeys)
		raw := mstr(rd, "path")
		if mg := mstr(rd, "merge"); mg != "" {
			if mg != MergeAtomic && mg != MergeUnion {
				l.errf("%s.merge: %q is not a merge — \"atomic\" or \"union\"", mwhere, mg)
				continue
			}
			rule.Merge = mg
		}
		p, err := ParsePath(raw)
		if err != nil {
			l.errf("%s: %v", mwhere, err)
			continue
		}
		rule.Path = p
		m.Map[tname] = rule
	}
	for n := range m.Map {
		m.MapOrder = append(m.MapOrder, n)
	}
	sort.Strings(m.MapOrder)
	return m
}

// resolveMapping validates one mapping once every reference pin is a resolved
// identity: the subject reference's shape, and every path against both declared
// kinds, so a disagreement fails on the manifest that caused it and not on the
// first sync that hits it (§7.1).
func (r *Registry) resolveMapping(m *Mapping) []string {
	var problems []string
	where := DocRecordMapping + " " + m.Identity()
	errf := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	from, ok := r.ByIdentity(m.From)
	if !ok {
		// The source kind may live in another package (record 49), so its
		// absence is reported HERE and not at parse: one legible problem
		// naming what to import first, the shape `requires:` refuses an
		// install with.
		errf("%s: data.from names %s, which this repository does not have: import that kind's package first, or delete this mapping",
			where, m.From)
		return problems
	}
	to, ok := r.ByIdentity(m.To)
	if !ok {
		errf("%s: data.to: unknown type %q", where, m.To)
		return problems
	}
	// The subject reference's shape (§6.1): a kind's own reference property,
	// marked `subject: true`, single, mustExist and never cascading. The PIN is
	// the declaring package's choice (record 49): a mirror kind whose targets
	// its own package cannot know leaves the reference unpinned and optional,
	// and the mapping's `to` is the subject kind for that property. A pin, where
	// there is one, still has to agree with `to`, and a pinned subject is
	// required.
	sp, ok := from.Props[m.Property]
	pinned := ok && sp.To != "" && sp.To != ToAny
	switch {
	case !ok:
		errf("%s: data.property: %s declares no property %q", where, m.From, m.Property)
	case sp.Datatype != DatatypeReference:
		errf("%s: data.property: %s.%s is %s — a subject is a `type: reference` property", where, m.From, m.Property, sp.Datatype)
	case pinned && sp.To != m.To:
		errf("%s: data.property: %s.%s points at %q, not data.to %s", where, m.From, m.Property, sp.To, m.To)
	case sp.ToTrait != "" && !to.Implements(sp.ToTrait):
		errf("%s: data.property: %s.%s pins the trait %s, which %s does not implement", where, m.From, m.Property, sp.ToTrait, m.To)
	default:
		if !sp.Subject {
			errf("%s: data.property: %s.%s is missing `subject: true` — the write path reads the marker, not this document", where, m.From, m.Property)
		}
		if pinned && !sp.Required {
			errf("%s: data.property: a subject reference pinned at %s is required: true, because a source record that names its target kind cannot exist without one", where, m.To)
		}
		if !sp.MustExist {
			errf("%s: data.property: the subject reference is mustExist: true — a source record describes a subject that exists", where)
		}
		if sp.Repeated || sp.Keyed {
			errf("%s: data.property: the subject reference is single-valued — a record that describes two things is two records", where)
		}
		if sp.Cascades() {
			errf("%s: data.property: the subject reference never cascades — deleting the subject must not collect the records that describe it (§6.1)", where)
		}
	}
	// Match probes are identifier lookups: both ends stay in the short-string
	// family, the only kinds the engine probes by value.
	for i, mr := range m.Match {
		mwhere := fmt.Sprintf("%s: data.match[%d]", where, i)
		sp, _, err := PathProperty(from, mr.From)
		switch {
		case err != nil:
			errf("%s.from: %v", mwhere, err)
		case !IsShortString(sp.Datatype):
			errf("%s.from: %s is %s — a probe extracts short-string or email values", mwhere, mr.From, sp.Datatype)
		}
		tp, ok := to.Props[mr.To]
		if !ok || tp.Implicit || !IsShortString(tp.Datatype) {
			errf("%s.to: a probe names a declared short-string or email property on %s, got %q", mwhere, m.To, mr.To)
		}
	}
	// Map rules type-check terminal against target; refinements compare by
	// base Datatype, which is what Property.Kind stores.
	for _, tname := range m.MapOrder {
		rule := m.Map[tname]
		mwhere := fmt.Sprintf("%s: data.map.%s", where, tname)
		sp, repeated, err := PathProperty(from, rule.Path)
		if err != nil {
			errf("%s: %v", mwhere, err)
			continue
		}
		switch sp.Datatype {
		case DatatypeObject:
			errf("%s: %s is an object — a map path ends at a field, never a whole object", mwhere, rule.Path)
			continue
		case DatatypeState:
			errf("%s: %s is a state — a state moves through its transitions, never a mapping (record 40)", mwhere, rule.Path)
			continue
		}
		if sp.Sensitive() {
			errf("%s: %s is %s-typed: a sensitive value never leaves its record", mwhere, rule.Path, sp.Datatype)
			continue
		}
		tp, ok := to.Props[tname]
		if !ok {
			if tp, ok = columnProp(to, tname); !ok {
				errf("%s: %s declares no property %q", mwhere, m.To, tname)
				continue
			}
		}
		if tp.IsState() {
			errf("%s: %s.%s is a state — a state moves through its transitions, never a mapping (record 40)", mwhere, m.To, tname)
			continue
		}
		if sp.Datatype != tp.Datatype {
			errf("%s: %s is %s, %s.%s is %s — a map path type-checks against both ends", mwhere, rule.Path, sp.Datatype, m.To, tname, tp.Datatype)
			continue
		}
		// Cardinality: union needs a repeated target (a scalar path
		// contributes a singleton, which is legal); a repeated source without
		// union needs a repeated atomic target of the same kind.
		switch {
		case rule.Merge == MergeUnion && !tp.Repeated:
			errf("%s: merge: union needs a repeated target — %s.%s is not", mwhere, m.To, tname)
		case rule.Merge != MergeUnion && repeated != tp.Repeated:
			errf("%s: %s and %s.%s disagree on repetition — a repeated source needs merge: union or a repeated target", mwhere, rule.Path, m.To, tname)
		}
	}
	return problems
}

// mappingInvariantProblems checks the registry-wide rules a mapping carries
// , once every reference pin is resolved: exactly one mapping per
// (source kind, subject property), so one mirror kind reaches two subject
// kinds through two `subject: true` references and two mappings through one
// reference stay refused (record 49). The source-to-subject graph stays
// bipartite: a mapping's `to` may never itself be any mapping's `from`, and
// no reference anywhere may land on a mapped source kind, which keeps
// resolution one hop deep (§6.2). Registry-wide, because a mapping installs
// with its connector long after the vocabulary that names its target was
// loaded.
func (r *Registry) mappingInvariantProblems() []string {
	var problems []string
	// bySlot is the mapping set's key. byFrom is the source-kind index the
	// bipartite and no-reference rules read; a source's first mapping names
	// the violation, so the message does not depend on which of its mappings
	// the loop reached.
	bySlot := map[mappingSlot]*Mapping{}
	byPair := map[mappingSlot]*Mapping{}
	byFrom := map[string][]*Mapping{}
	for _, m := range r.Mappings() {
		byFrom[m.From] = append(byFrom[m.From], m)
		slot := mappingSlot{from: m.From, property: m.Property}
		if prev, ok := bySlot[slot]; ok {
			problems = append(problems, fmt.Sprintf(
				"%s %s: %s.%s already has mapping %s, and one subject property carries one mapping (record 49)",
				DocRecordMapping, m.Identity(), m.From, m.Property, prev.Identity()))
			continue
		}
		bySlot[slot] = m
		// One source kind reaches a given target kind through ONE property.
		// Two slots onto the same target leave every reader choosing between
		// them: the subject hop would have two answers for one pin, and a
		// recompute would count one source's contributions twice.
		pair := mappingSlot{from: m.From, property: m.To}
		if prev, ok := byPair[pair]; ok {
			problems = append(problems, fmt.Sprintf(
				"%s %s: %s already reaches %s through mapping %s, on property %s (record 49)",
				DocRecordMapping, m.Identity(), m.From, m.To, prev.Identity(), prev.Property))
			continue
		}
		byPair[pair] = m
	}
	for _, m := range r.Mappings() {
		if others, ok := byFrom[m.To]; ok {
			problems = append(problems, fmt.Sprintf(
				"%s %s: data.to: %s is itself the source of mapping %s — the source→subject graph stays bipartite (record 50)",
				DocRecordMapping, m.Identity(), m.To, others[0].Identity()))
		}
	}
	// Every declared reference site, nested ones included: a pointer at a source
	// kind is a second hop whichever level it sits at.
	for _, t := range r.Kinds() {
		for _, site := range referenceSites(t) {
			ms, ok := byFrom[site.Prop.To]
			if !ok {
				continue
			}
			problems = append(problems, fmt.Sprintf(
				"%s %s: data.properties.%s: no reference may name %s, the source kind of mapping %s — pin it at %s",
				DocKind, t.Identity, site.Path, site.Prop.To, ms[0].Identity(), ms[0].To))
		}
	}
	return problems
}

// crossPackageMappingProblems re-resolves every mapping declared OUTSIDE the
// packages being installed whose SOURCE or TARGET kind lives inside them.
//
// It exists because a mapping's ends may live in three different packages
// (record 49): resolvePackage type-checks a mapping's paths against both
// declared kinds, but it only runs for the packages a batch rebuilds, so
// narrowing a mirror kind would otherwise strand a mapping another package
// declared against it and nothing would say so until the next repository
// open. Install and InstallAll call it; Finalize does not need it, because it
// resolves every package there is.
func (r *Registry) crossPackageMappingProblems(installed []*Package) []string {
	inside := map[string]bool{}
	for _, g := range installed {
		inside[g.Identity] = true
	}
	var problems []string
	for _, m := range r.Mappings() {
		if inside[m.Package] {
			continue // its own package's resolve already covered it
		}
		if !inside[KindPackage(m.From)] && !inside[KindPackage(m.To)] {
			continue
		}
		if _, ok := r.ByIdentity(m.From); !ok {
			// The source kind LEAVING is not this check's to report: the
			// engine refuses that at the same guard it refuses every other
			// breakage at, naming the mapping to delete (schemadiff.go
			// strandedMappingGuards). Reporting it here too would answer one
			// situation two ways, and with the worse message.
			continue
		}
		problems = append(problems, r.resolveMapping(m)...)
	}
	return problems
}

// mappingSlot keys the mapping set: a source kind and the subject reference a
// mapping fills on it. The overlap check reuses it with the TARGET kind in the
// second field, the other pair a source kind may hold only once.
type mappingSlot struct{ from, property string }

// --- registry lookups ------------------------------------------------------

// Mappings lists every loaded mapping, ordered by identity — one mirror
// record each (§9.1).
func (r *Registry) Mappings() []*Mapping {
	var out []*Mapping
	for _, g := range r.PackageList() {
		for _, n := range g.MappingOrder {
			out = append(out, g.Mappings[n])
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Identity() < out[j].Identity() })
	return out
}

// MappingsFrom lists every mapping whose source is the given kind identity,
// ordered by mapping identity. A kind carrying one is a source record: its id
// is the provider's key, each mapping's subject reference is set at creation
// and moved only by merge and split, and its writes recompute those subjects.
// One kind may carry several, one per `subject: true` reference (record 49).
func (r *Registry) MappingsFrom(from string) []*Mapping {
	var out []*Mapping
	for _, m := range r.Mappings() {
		if m.From == from {
			out = append(out, m)
		}
	}
	return out
}

// MappingFor returns the mapping filling one source kind's named subject
// property: the (source kind, subject property) pair the set is keyed by.
func (r *Registry) MappingFor(from, property string) (*Mapping, bool) {
	for _, m := range r.Mappings() {
		if m.From == from && m.Property == property {
			return m, true
		}
	}
	return nil, false
}

// MappingsTo lists every mapping onto the given type identity, ordered by
// mapping identity. Empty means nothing manages it: its properties are
// whatever was written to it directly (§7.1). Non-empty also means its id is
// always server-assigned — nothing external names a subject.
func (r *Registry) MappingsTo(to string) []*Mapping {
	var out []*Mapping
	for _, m := range r.Mappings() {
		if m.To == to {
			out = append(out, m)
		}
	}
	return out
}

// A manager row stores its tier at write time (primitives §6), read from
// the write context's actor DATA — Registry.ActorTier answers
// the declared-actor half of it.
