package vocabulary

import (
	"fmt"
	"sort"
	"strings"
)

// A recordmapping is the sixth manifest kind: it says how one
// source-record type's properties reach the subject its edge points at. The
// edge itself stays an ordinary declared edge — the mapping, not a flag, is
// what the engine recognizes — and everything record 33 ruled about it stands,
// restated here: created with its record, moved only by merge and split,
// never ownerRef, and no other edge may land on a mapped source type.

// Merge is how one target property combines contributions (§7.1): atomic —
// one source's value wins whole — or union, the deduped union of every live
// source's items, only onto repeated properties.
const (
	MergeAtomic = "atomic"
	MergeUnion  = "union"
)

// Mapping is one parsed recordmapping. From is a type declared in Authority; To
// is a full type name, resolved like an edge target; Edge names the declared
// edge on From that records the link.
type Mapping struct {
	Name      string
	Authority string
	From      string
	To        string
	Edge      string
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

	// Definition is the manifest's data map, exactly as authored.
	Definition map[string]any
	// SourceYAML is the verbatim manifest; installed authorities have no original
	// text, so theirs is derived.
	SourceYAML string
}

// Identity is "<authority>/<name>".
func (m *Mapping) Identity() string { return KindRef(m.Authority, m.Name) }

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

// columnProp resolves the column-backed properties every record carries but
// no manifest declares (reservedProps): title and body always, the temporals
// when a capability binds them. They are legal path sources and map targets
// (§7.1 — hot props).
func columnProp(t *Kind, name string) (*Property, bool) {
	switch name {
	case "title":
		return &Property{Name: "title", Datatype: DatatypeString}, true
	case "body":
		return &Property{Name: "body", Datatype: DatatypeText}, true
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
	"authority": true, "from": true, "to": true, "edge": true,
	"match": true, "map": true, "description": true,
}

var matchRuleKeys = map[string]bool{"from": true, "to": true}

var mapRuleKeys = map[string]bool{"path": true, "merge": true}

// parseMapping parses one recordmapping document. Everything that needs the
// target type — the edge's shape, path type-checks, the bipartite rule — is
// deferred to Finalize/Install, like edge targets: `to` may name a type in a
// authority that has not loaded yet.
func (l *loader) parseMapping(d Document) *Mapping {
	g := l.authority
	where := DocRecordMapping + " " + d.ID
	l.checkKeys(where, d.Data, mappingDataKeys)
	local, ok := l.localName(where, d.ID, g.Name)
	if !ok {
		return nil
	}
	m := &Mapping{
		Name: local, Authority: g.Name,
		From: mstr(d.Data, "from"), To: mstr(d.Data, "to"), Edge: mstr(d.Data, "edge"),
		Map:        map[string]*MapRule{},
		Definition: d.Data, SourceYAML: d.Source,
	}
	// `from` lives in data.authority and is spelled in full (§6.1): a mapping is
	// authority-local, like a datatype.
	rest, fromLocal := SplitKindRef(m.From)
	switch {
	case m.From == "":
		l.errf("%s: data.from is required", where)
		return nil
	case rest != g.Name || fromLocal == "":
		l.errf("%s: data.from must be %q — a mapping's source kind lives in its own authority (§6.1)", where, KindRef(g.Name, "<name>"))
		return nil
	}
	if _, ok := g.Kinds[fromLocal]; !ok {
		l.errf("%s: data.from: %s is not a declared type of %s", where, m.From, g.Name)
		return nil
	}
	// `to` is a full type name: a bare name would go ambiguous the day a
	// second authority ships the same word — at registration, not at review.
	switch {
	case m.To == "":
		l.errf("%s: data.to is required", where)
		return nil
	case !Qualified(m.To):
		l.errf("%s: data.to is a full type name (\"people.substrate.geoah.me/person\"), never a bare one (§6.1)", where)
		return nil
	}
	if m.Edge == "" {
		l.errf("%s: data.edge is required — the declared edge on %s that records the link", where, m.From)
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
		var raw string
		switch v := rv.(type) {
		case string:
			raw = v
		default:
			rd := asMap(rv)
			l.checkKeys(mwhere, rd, mapRuleKeys)
			raw = mstr(rd, "path")
			if mg := mstr(rd, "merge"); mg != "" {
				if mg != MergeAtomic && mg != MergeUnion {
					l.errf("%s.merge: %q is not a merge — \"atomic\" or \"union\"", mwhere, mg)
					continue
				}
				rule.Merge = mg
			}
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

// resolveMapping validates one mapping once every edge target is a resolved
// identity: the edge's shape, and every path against both declared types, so
// a disagreement fails on the manifest that caused it and not on the first
// sync that hits it (§7.1).
func (r *Registry) resolveMapping(m *Mapping) []string {
	var problems []string
	where := DocRecordMapping + " " + m.Identity()
	errf := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	from, ok := r.ByIdentity(m.From)
	if !ok {
		return problems // already reported at parse
	}
	to, ok := r.ByIdentity(m.To)
	if !ok {
		errf("%s: data.to: unknown type %q", where, m.To)
		return problems
	}
	// The subject edge's shape (§6.1): declared on the source, single,
	// required, never ownerRef, pointing exactly at data.to.
	ed, ok := from.Edge(m.Edge)
	switch {
	case !ok:
		errf("%s: data.edge: %s declares no edge %q", where, m.From, m.Edge)
	case ed.To != m.To:
		errf("%s: data.edge: %s.%s points at %s, not data.to %s", where, m.From, m.Edge, ed.To, m.To)
	default:
		if !ed.Required {
			errf("%s: data.edge: the subject edge is required: true — a source record without its subject cannot exist", where)
		}
		if ed.Many {
			errf("%s: data.edge: the subject edge is single-target (many: false) — a record that describes two things is two records", where)
		}
		if ed.OwnerRef {
			errf("%s: data.edge: the subject edge is never ownerRef — deleting the subject must not GC the records that describe it (§6.1)", where)
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
// , once every edge target is resolved: exactly one mapping per
// source type, the source→subject graph stays bipartite — a mapping's `to`
// may never itself be any mapping's `from` — and no edge anywhere may land on
// a mapped source type, which keeps resolution one hop deep (§6.2). Registry-
// wide, because a mapping installs with its connector long after the
// vocabulary that names its target was loaded.
func (r *Registry) mappingInvariantProblems() []string {
	var problems []string
	byFrom := map[string]*Mapping{}
	for _, m := range r.Mappings() {
		if prev, ok := byFrom[m.From]; ok {
			problems = append(problems, fmt.Sprintf(
				"%s %s: %s already has mapping %s — exactly one mapping per source type (§6.1)",
				DocRecordMapping, m.Identity(), m.From, prev.Identity()))
			continue
		}
		byFrom[m.From] = m
	}
	for _, m := range r.Mappings() {
		if other, ok := byFrom[m.To]; ok {
			problems = append(problems, fmt.Sprintf(
				"%s %s: data.to: %s is itself the source of mapping %s — the source→subject graph stays bipartite (record 50)",
				DocRecordMapping, m.Identity(), m.To, other.Identity()))
		}
	}
	for _, t := range r.Kinds() {
		for _, en := range t.EdgeOrder {
			e := t.Edges[en]
			m, ok := byFrom[e.To]
			if !ok {
				continue
			}
			problems = append(problems, fmt.Sprintf(
				"%s %s: data.edges.%s: no edge may target %s, the source type of mapping %s — point it at %s",
				DocKind, t.Identity, en, e.To, m.Identity(), m.To))
		}
	}
	return problems
}

// --- registry lookups ------------------------------------------------------

// Mappings lists every loaded mapping, ordered by identity — one mirror
// record each (§9.1).
func (r *Registry) Mappings() []*Mapping {
	var out []*Mapping
	for _, g := range r.AuthorityList() {
		for _, n := range g.MappingOrder {
			out = append(out, g.Mappings[n])
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Identity() < out[j].Identity() })
	return out
}

// MappingFor returns the one mapping whose source is the given type identity.
// A type carrying one is a source record: its id is the provider's key, its
// subject edge is create-time only, and its writes recompute the subject.
func (r *Registry) MappingFor(from string) (*Mapping, bool) {
	for _, m := range r.Mappings() {
		if m.From == from {
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
