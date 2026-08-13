package vocabulary

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/geoah/substrate/internal/substrate"
)

// SourceBuiltin marks authorities that came from a shipped schema file;
// SourceInstalled marks connector manifests.
const (
	SourceBuiltin   = "builtin"
	SourceInstalled = "installed"
)

// DefaultVersion is the version a authority manifest gets when it declares
// none.
const DefaultVersion = "v1alpha1"

var reCapBinding = regexp.MustCompile(`^([a-z][a-zA-Z0-9]*)(?:\(\s*([a-z][a-zA-Z0-9]*)\s*(?::\s*([a-zA-Z0-9,\s]*))?\s*\))?$`)

// LoadFS parses every .yaml document under fsys into one registry. The unit
// is the document, not the file: manifests are grouped by `data.authority`, and
// all authorities are built before any edge or cross-authority trait is resolved,
// so files may reference each other in any order.
func LoadFS(fsys fs.FS) (*Registry, error) {
	docs, err := readDocuments(fsys)
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("substrate/schema: no schema manifests found")
	}
	authorities, err := BuildAuthorities(docs, SourceBuiltin)
	if err != nil {
		return nil, err
	}
	r := NewRegistry()
	for _, g := range authorities {
		if err := r.add(g); err != nil {
			return nil, err
		}
	}
	if err := r.Finalize(); err != nil {
		return nil, err
	}
	return r, nil
}

// LoadDir is LoadFS over a filesystem path.
func LoadDir(dir string) (*Registry, error) { return LoadFS(os.DirFS(dir)) }

// readDocuments walks the schema tree in lexical order and parses every
// document it holds.
func readDocuments(fsys fs.FS) ([]Document, error) {
	var out []Document
	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if ext := path.Ext(name); ext != ".yaml" && ext != ".yml" {
			return nil
		}
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return fmt.Errorf("substrate/schema: read %s: %w", name, err)
		}
		docs, err := ParseStream(data)
		if err != nil {
			return fmt.Errorf("substrate/schema: %s: %w", name, err)
		}
		out = append(out, docs...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ParseYAML parses one manifest stream into its authorities — one file, one test
// fixture, one paste.
func ParseYAML(data []byte, source string) ([]*Authority, error) {
	docs, err := ParseStream(data)
	if err != nil {
		return nil, err
	}
	return BuildAuthorities(docs, source)
}

// Manifest is the legacy connector-registration payload shape (name, authority,
// and the manifest document list). The POST …/connectors shim and the frozen
// substrate.ConnectorManifest wire type were removed at the v1 freeze (ticket
// 004, ruling A12); this struct survives ONLY as the in-memory shape the
// historical stored-manifest promotion (dialect step 4) decodes an old blob
// into, and the loader test's fixture. It is not a wire contract.
type Manifest struct {
	Name      string           `json:"name"`
	Authority string           `json:"authority"`
	Manifests []map[string]any `json:"manifests"`
}

// ParseManifest turns a connector's installed payload — a list of manifest
// documents — into the single authority it installs.
func ParseManifest(m Manifest) (*Authority, error) {
	docs := make([]Document, 0, len(m.Manifests))
	var problems []string
	for _, raw := range m.Manifests {
		d, errs := documentFrom(raw, "")
		if len(errs) > 0 {
			problems = append(problems, errs...)
			continue
		}
		docs = append(docs, d)
	}
	if len(problems) > 0 {
		return nil, validationError(problems)
	}
	authorities, err := BuildAuthorities(docs, SourceInstalled)
	if err != nil {
		return nil, err
	}
	switch {
	case len(authorities) == 0:
		return nil, validationError([]string{"manifest: declares no authority"})
	case len(authorities) > 1:
		names := make([]string, 0, len(authorities))
		for _, g := range authorities {
			names = append(names, g.Name)
		}
		return nil, validationError([]string{
			fmt.Sprintf("manifest: installs one authority, got %s", strings.Join(names, ", ")),
		})
	case m.Authority != "" && authorities[0].Name != m.Authority:
		return nil, validationError([]string{
			fmt.Sprintf("manifest: authority %q, but its manifests declare %q", m.Authority, authorities[0].Name),
		})
	}
	return authorities[0], nil
}

// BuildAuthorities turns a document stream into authorities, one per declared
// `data.authority`. Every problem in every document is reported at once.
func BuildAuthorities(docs []Document, source string) ([]*Authority, error) {
	l := &loader{}
	buckets := map[string]*authorityDocs{}
	var order []string
	bucket := func(authority string) *authorityDocs {
		b, ok := buckets[authority]
		if !ok {
			b = &authorityDocs{}
			buckets[authority] = b
			order = append(order, authority)
		}
		return b
	}
	for _, d := range docs {
		authority := mstr(d.Data, "authority")
		if d.Kind == DocAuthority {
			authority = d.ID
		}
		if authority == "" {
			l.errf("%s %s: data.authority is required", d.Kind, d.ID)
			continue
		}
		if !ValidAuthority(authority) {
			l.errf("%s %s: data.authority %q must be a DNS name", d.Kind, d.ID, authority)
			continue
		}
		b := bucket(authority)
		// The bundle closure counts every member kind, present and future:
		// anything but the header, the actors and the bundle itself.
		if d.Kind != DocAuthority && d.Kind != DocActor && d.Kind != DocBundle {
			b.memberIDs = append(b.memberIDs, d.ID)
		}
		switch d.Kind {
		case DocAuthority:
			if b.authority != nil {
				l.errf("authority %s: declared twice", d.ID)
				continue
			}
			b.authority = &d
		case DocActor:
			b.actors = append(b.actors, d)
		case DocTrait:
			b.capabilities = append(b.capabilities, d)
		case DocPropertyType:
			b.datatypes = append(b.datatypes, d)
		case DocKind:
			b.types = append(b.types, d)
		case DocRecordMapping:
			b.mappings = append(b.mappings, d)
		case DocFunction:
			b.functions = append(b.functions, d)
		case DocAgent:
			b.agents = append(b.agents, d)
		case DocBundle:
			b.bundles = append(b.bundles, d)
		}
	}
	sort.Strings(order)
	// A VOCABULARY bundle's authority is SHIPPED vocabulary whichever door it
	// came through (bundle.go): the binary publishes people/tasks/media/…, the
	// registry only DELIVERS them. So it is built `builtin` even on an install
	// path — which keeps its GraphQL names bare (`Person`, not `People_Person`)
	// and keeps its declarations behind the authority chokepoint, exactly as
	// when the creation seed still wrote them.
	vocabulary := VocabularyBundleAuthorities(docs)
	out := make([]*Authority, 0, len(order))
	for _, name := range order {
		src := source
		if vocabulary[name] {
			src = SourceBuiltin
		}
		if g := l.buildAuthority(name, buckets[name], src); g != nil {
			out = append(out, g)
		}
	}
	if len(l.problems) > 0 {
		return nil, validationError(l.problems)
	}
	return out, nil
}

// authorityDocs holds one authority's manifests, bucketed by type: an authority is built
// datatypes first, then traits, then types, so a document's position in
// the stream never decides whether it resolves.
type authorityDocs struct {
	authority    *Document
	actors       []Document
	capabilities []Document
	datatypes    []Document
	types        []Document
	mappings     []Document
	functions    []Document
	agents       []Document
	bundles      []Document
	// memberIDs are the ids of every member-kind document declared into the
	// authority — the set a bundle's `installs:` must equal (bundle.go).
	memberIDs []string
}

type loader struct {
	authority *Authority
	problems  []string
}

func (l *loader) errf(format string, args ...any) {
	l.problems = append(l.problems, fmt.Sprintf(format, args...))
}

func validationError(problems []string) error {
	sort.Strings(problems)
	return &substrate.ValidationError{Problems: problems}
}

var authorityDataKeys = map[string]bool{"version": true}

var actorDataKeys = map[string]bool{"authority": true, "tier": true}

var datatypeDataKeys = map[string]bool{
	"authority": true, "base": true, "pattern": true, "min": true, "max": true,
	"values": true, "description": true,
}

var capabilityDataKeys = map[string]bool{
	"authority": true, "oneOf": true, "properties": true, "description": true,
}

// buildAuthority assembles one authority from its manifests.
func (l *loader) buildAuthority(name string, gd *authorityDocs, source string) *Authority {
	if gd.authority == nil {
		l.errf("authority %s: no authority manifest declares it", name)
		return nil
	}
	g := &Authority{
		Name:          name,
		Source:        source,
		ActorTiers:    map[string]substrate.Tier{},
		PropertyTypes: map[string]*PropertyType{},
		Traits:        map[string]*Trait{},
		Kinds:         map[string]*Kind{},
		Mappings:      map[string]*Mapping{},
		Functions:     map[string]*Function{},
		Agents:        map[string]*Agent{},
		SourceYAML:    gd.authority.Source,
	}
	l.authority = g

	l.checkKeys("authority "+name, gd.authority.Data, authorityDataKeys)
	g.Version = mstr(gd.authority.Data, "version")
	if g.Version == "" {
		g.Version = DefaultVersion
	}

	sortDocs(gd.actors)
	for _, d := range gd.actors {
		where := "actor " + d.ID
		l.checkKeys(where, d.Data, actorDataKeys)
		if !ValidActor(d.ID) {
			l.errf("%s: must be a lowercase dotted name", where)
			continue
		}
		if slices.Contains(g.Actors, d.ID) {
			l.errf("%s: declared twice in %s", where, name)
			continue
		}
		// The manager tier is an EXPLICIT attribute of the actor (ticket 002,
		// ruling A10's second half): declared data, never inferred from the
		// actor's spelling. Authority-declared actors are the sync machinery, so
		// machine is the default.
		tier := substrate.TierMachine
		switch declared := mstr(d.Data, "tier"); declared {
		case "":
		case string(substrate.TierOwner), string(substrate.TierBundle), string(substrate.TierMachine):
			tier = substrate.Tier(declared)
		default:
			l.errf("%s: tier must be owner, bundle or machine, got %q", where, declared)
			continue
		}
		if d.ID == name && tier != substrate.TierMachine {
			// Record 60: an authority-named actor has no record row of its own — the
			// authority row is it, and that row carries no tier to round-trip.
			l.errf("%s: an authority-named actor is the connector's own hand — its tier is machine", where)
			continue
		}
		g.Actors = append(g.Actors, d.ID)
		g.ActorTiers[d.ID] = tier
	}

	// Custom property types: refinements of a base type.
	sortDocs(gd.datatypes)
	for _, d := range gd.datatypes {
		where := DocPropertyType + " " + d.ID
		l.checkKeys(where, d.Data, datatypeDataKeys)
		l.parseDescription(where+": data", d.Data)
		local, ok := l.localName(where, d.ID, name)
		if !ok {
			continue
		}
		base := mstr(d.Data, "base")
		if base == "" {
			l.errf("%s: data.base is required", where)
			continue
		}
		p := l.parseProperty(where, local, map[string]any{
			"type":    base,
			"pattern": d.Data["pattern"],
			"min":     d.Data["min"],
			"max":     d.Data["max"],
			"values":  d.Data["values"],
		}, false)
		if p == nil {
			continue
		}
		p.Refined = local
		if _, dup := g.PropertyTypes[local]; dup {
			l.errf("%s: declared twice", where)
			continue
		}
		g.PropertyTypes[local] = &PropertyType{
			Name: local, Authority: name, Base: p.Datatype, Prop: p,
			Definition: d.Data, SourceYAML: d.Source,
		}
		g.DatatypeOrder = append(g.DatatypeOrder, local)
	}
	sort.Strings(g.DatatypeOrder)

	// Traits (the Go type is still Trait; the wire says trait).
	sortDocs(gd.capabilities)
	for _, d := range gd.capabilities {
		where := DocTrait + " " + d.ID
		l.checkKeys(where, d.Data, capabilityDataKeys)
		l.parseDescription(where+": data", d.Data)
		local, ok := l.localName(where, d.ID, name)
		if !ok {
			continue
		}
		c := &Trait{
			Name: local, Authority: name, Definition: d.Data, SourceYAML: d.Source,
		}
		if one := mmap(d.Data, "oneOf"); len(one) > 0 {
			c.Variants = map[string]map[string]Datatype{}
			for vname, vdef := range one {
				props := map[string]Datatype{}
				for pname, pkind := range asMap(vdef) {
					if !ValidCamel(pname) {
						l.errf("%s: data.oneOf.%s.%s: must be %s", where, vname, pname, camelRule)
						continue
					}
					k := Datatype(fmt.Sprint(pkind))
					if !builtinKinds[k] {
						l.errf("%s: data.oneOf.%s.%s: unknown property type %q", where, vname, pname, k)
						continue
					}
					props[pname] = k
				}
				c.Variants[vname] = props
			}
		}
		if props := mmap(d.Data, "properties"); len(props) > 0 {
			c.Properties = map[string]Datatype{}
			for pname, pkind := range props {
				if !ValidCamel(pname) {
					l.errf("%s: data.properties.%s: must be %s", where, pname, camelRule)
					continue
				}
				k := Datatype(fmt.Sprint(pkind))
				if !builtinKinds[k] {
					if _, ok := g.PropertyTypes[fmt.Sprint(pkind)]; !ok {
						l.errf("%s: data.properties.%s: unknown property type %q", where, pname, pkind)
						continue
					}
				}
				c.Properties[pname] = k
			}
		}
		if _, dup := g.Traits[local]; dup {
			l.errf("%s: declared twice", where)
			continue
		}
		g.Traits[local] = c
		g.TraitOrder = append(g.TraitOrder, local)
	}
	sort.Strings(g.TraitOrder)

	// Kinds.
	sortDocs(gd.types)
	for _, d := range gd.types {
		t := l.parseType(d)
		if t == nil {
			continue
		}
		if _, dup := g.Kinds[t.Name]; dup {
			l.errf("%s %s: declared twice", DocKind, t.Identity)
			continue
		}
		g.Kinds[t.Name] = t
		g.KindOrder = append(g.KindOrder, t.Name)
	}
	sort.Strings(g.KindOrder)

	// Mappings, after types: `from` must be a type this authority declares
	// (§6.1); everything about the target is deferred to Finalize/Install.
	sortDocs(gd.mappings)
	for _, d := range gd.mappings {
		m := l.parseMapping(d)
		if m == nil {
			continue
		}
		if _, dup := g.Mappings[m.Name]; dup {
			l.errf("%s %s: declared twice", DocRecordMapping, d.ID)
			continue
		}
		g.Mappings[m.Name] = m
		g.MappingOrder = append(g.MappingOrder, m.Name)
	}
	sort.Strings(g.MappingOrder)

	// Functions: CEL compiles at parse; emit and exact trigger types resolve
	// against the registry in Finalize/Install, like edge targets.
	sortDocs(gd.functions)
	for _, d := range gd.functions {
		fn := l.parseFunction(d)
		if fn == nil {
			continue
		}
		if _, dup := g.Functions[fn.Name]; dup {
			l.errf("%s %s: declared twice", DocFunction, d.ID)
			continue
		}
		g.Functions[fn.Name] = fn
		g.FunctionOrder = append(g.FunctionOrder, fn.Name)
	}
	sort.Strings(g.FunctionOrder)

	// Agents (agent.go): tool callables, sub-agents and emit resolve against
	// the registry in Finalize/Install; the llmprovider data row resolves at dispatch.
	l.buildAuthorityAgents(gd, g)

	// The bundle document (bundle.go): the owned-authority rule and the install
	// closure check against the authority's own members; the configType resolves
	// in Finalize/Install, after trait bindings.
	l.buildBundle(gd)
	return g
}

// localName splits an `<authority>/<name>` kind reference and checks it
// addresses this authority. Identity is metadata.id (FORMAT.md §2), so a
// manifest that spells it inconsistently is a load error, not a silent rename.
func (l *loader) localName(where, identity, authority string) (string, bool) {
	head, local := SplitKindRef(identity)
	if head != authority || local == "" {
		l.errf("%s: metadata.id must be %q", where, KindRef(authority, "<name>"))
		return "", false
	}
	if !ValidCamel(local) {
		l.errf("%s: %q must be %s", where, local, camelRule)
		return "", false
	}
	return local, true
}

func (l *loader) checkKeys(where string, data map[string]any, allowed map[string]bool) {
	for k := range data {
		if allowed[k] {
			continue
		}
		if replacement, gone := deletedDataKeys[k]; gone {
			l.errf("%s: key %q is deleted — %s", where, k, replacement)
			continue
		}
		l.errf("%s: unknown key %q", where, k)
	}
}

// deletedDataKeys are the keys the pinned design removed, each naming what
// replaced it. No compatibility shim for any of them: a manifest still
// carrying one would otherwise look obeyed.
var deletedDataKeys = map[string]string{
	"capabilities":       "traits (records 62 and 63: the binding key follows the kind)",
	"bundles":            "traits (record 63 — `bundle` now names the install unit)",
	"propertyPrecedence": "the manager ledger decides — recompute yields to anyone outside the machine tier (record 51)",
	"machines":           "a machine is a `type: state` property",
	"props":              "properties",
	"aliasNamespaces":    "identity is the id and nothing else",
	"identifying":        "identity is the id and nothing else",
	"id":                 "a writer supplies metadata.id; there are no id strategies",
	"merge":              "merge is manual and owner-driven",
	"llm":                "provider + model — an llmprovider record id and the model string sent on every completion",
	"binding":            "an ordinary edge plus a recordmapping document (record 50)",
	"projects":           "an ordinary edge plus a recordmapping document (record 50)",
	"actor":              "transitions carry no guard — anyone may perform any of them",
}

// sortDocs orders a type's manifests by identity, so the registry a stream
// produces never depends on which file a document sat in.
func sortDocs(docs []Document) {
	sort.Slice(docs, func(i, j int) bool { return docs[i].ID < docs[j].ID })
}

var typeDataKeys = map[string]bool{
	"authority": true, "names": true, "displayTemplate": true, "properties": true,
	"edges": true, "traits": true, "indices": true,
	"description": true, "version": true,
}

var namesKeys = map[string]bool{"singular": true, "plural": true}

func (l *loader) parseType(doc Document) *Kind {
	g := l.authority
	d := doc.Data
	where := DocKind + " " + doc.ID
	l.checkKeys(where, d, typeDataKeys)
	description := l.parseDescriptionMax(where+": data", d, maxKindDescription)

	names := mmap(d, "names")
	l.checkKeys(where+": data.names", names, namesKeys)
	name := mstr(names, "singular")
	if name == "" {
		l.errf("%s: data.names.singular is required", where)
		return nil
	}
	if !ValidName(name) {
		l.errf("%s: data.names.singular: type names are one lowercase word [a-z][a-z0-9]*", where)
		return nil
	}
	// The type's identity is its metadata.id and must agree with the names
	// block; nothing derives one from the other.
	if doc.ID != KindRef(g.Name, name) {
		l.errf("%s: metadata.id must be %q", where, KindRef(g.Name, name))
		return nil
	}

	t := &Kind{
		Name:        name,
		Authority:   g.Name,
		Identity:    doc.ID,
		Version:     g.Version,
		Source:      g.Source,
		Description: description,
		Props:       map[string]*Property{},
		Edges:       map[string]*Edge{},
		Machines:    map[string]*Machine{},
		HotColumns:  map[string]bool{},
		Definition:  d,
		SourceYAML:  doc.Source,
	}
	if v := mstr(d, "version"); v != "" {
		t.Version = v
	}
	t.Plural = mstr(names, "plural")
	switch {
	case t.Plural == "":
		l.errf("%s: data.names.plural is required (never auto-derived)", where)
	case !ValidName(t.Plural):
		l.errf("%s: data.names.plural: one lowercase word [a-z][a-z0-9]*", where)
	}
	t.DisplayTemplate = mstr(d, "displayTemplate")

	// properties, state machines among them (MODEL §11.4)
	for pname, pdef := range mmap(d, "properties") {
		if !ValidCamel(pname) {
			l.errf("%s: data.properties.%s: must be %s", where, pname, camelRule)
			continue
		}
		if reason, reserved := reservedProps[pname]; reserved {
			l.errf("%s: data.properties.%s: %s", where, pname, reason)
			continue
		}
		p := l.parseProperty(fmt.Sprintf("%s: data.properties.%s", where, pname), pname, asMap(pdef), true)
		if p == nil {
			continue
		}
		t.Props[pname] = p
		if p.Machine == nil {
			continue
		}
		t.Machines[pname] = p.Machine
	}
	// Stamp targets are implicitly declared datetime properties, and a stamp
	// may not collide with one the author wrote: two declarations of one name
	// mean one of them is silently ignored.
	for _, pname := range sortedKeys(mapOfAny(t.Machines)) {
		for _, tr := range t.Machines[pname].Transitions {
			for _, stamp := range sortedKeys(mapOfAny(tr.Stamps)) {
				if existing, ok := t.Props[stamp]; ok {
					if !existing.Implicit {
						l.errf("%s: data.properties.%s: stamp %q collides with a declared property",
							where, pname, stamp)
					}
					continue
				}
				t.Props[stamp] = &Property{Name: stamp, Datatype: DatatypeDatetime, Implicit: true}
			}
		}
	}

	for n := range t.Props {
		t.PropOrder = append(t.PropOrder, n)
	}
	sort.Strings(t.PropOrder)

	// renamedFrom's sibling half (reserved, ticket 003): the previous name may
	// not be one the type still declares — both present is not a rename — and
	// a built-in column never renames into a declared property.
	for _, pname := range t.PropOrder {
		rf := t.Props[pname].RenamedFrom
		if rf == "" {
			continue
		}
		pwhere := fmt.Sprintf("%s: data.properties.%s", where, pname)
		if _, declared := t.Props[rf]; declared {
			l.errf("%s.renamedFrom: %q is still declared on the type — a rename drops the old declaration", pwhere, rf)
		}
		if _, reserved := reservedProps[rf]; reserved {
			l.errf("%s.renamedFrom: %q is a built-in property — it never renames", pwhere, rf)
		}
	}

	// edges
	if _, isList := d["edges"].([]any); isList {
		l.errf("%s: data.edges: a mapping of rel → target, not a list", where)
	}
	for ename, edef := range mmap(d, "edges") {
		if !ValidCamel(ename) {
			l.errf("%s: data.edges.%s: must be %s", where, ename, camelRule)
			continue
		}
		ed := asMap(edef)
		l.checkKeys(fmt.Sprintf("%s: data.edges.%s", where, ename), ed, edgeKeys)
		to := mstr(ed, "to")
		if to == "" {
			l.errf("%s: data.edges.%s: to is required", where, ename)
			continue
		}
		ewhere := fmt.Sprintf("%s: data.edges.%s", where, ename)
		inverse, inverseDesc := l.parseInverse(ewhere, ed)
		t.Edges[ename] = &Edge{
			Name:               ename,
			Description:        l.parseDescription(ewhere, ed),
			To:                 to,
			Many:               mbool(ed, "many"),
			Required:           mbool(ed, "required"),
			OwnerRef:           mbool(ed, "ownerRef"),
			Inverse:            inverse,
			InverseDescription: inverseDesc,
		}
	}
	for n := range t.Edges {
		t.EdgeOrder = append(t.EdgeOrder, n)
	}
	sort.Strings(t.EdgeOrder)
	// One name, one pointer. A kind declaring an edge AND a property under one
	// name leaves every reader to pick which it meant, and they do not agree:
	// a display template resolves the edge, the graph emits both, the write
	// path writes one and validates the other. Cheap to refuse, and refusing
	// keeps "an edge and a reference are the same relationship differently
	// stored" true — two of them under one name is two relationships.
	for _, n := range t.EdgeOrder {
		if _, dup := t.Props[n]; dup {
			l.errf("%s: %q is declared as both an edge and a property — one name is one pointer", where, n)
		}
	}

	// traits
	for _, cv := range mslice(d, "traits") {
		s := fmt.Sprint(cv)
		m := reCapBinding.FindStringSubmatch(strings.TrimSpace(s))
		if m == nil {
			l.errf("%s: data.traits: cannot parse %q", where, s)
			continue
		}
		c, ok := g.Traits[m[1]]
		if !ok {
			// Not declared here: resolved against the registry in Finalize,
			// which is the only point where sibling authorities are all loaded.
			g.pendingTraits = append(g.pendingTraits, pendingCapBinding{
				Kind: name, Cap: m[1], Variant: m[2], Cols: m[3],
			})
			continue
		}
		b, contracts, problems := bindCapability(t.Identity, m[1], m[2], m[3], c)
		l.problems = append(l.problems, problems...)
		g.pending = append(g.pending, contracts...)
		if b == nil {
			continue
		}
		t.applyCapability(*b)
	}

	// indices
	for _, iv := range mslice(d, "indices") {
		cols, ok := iv.([]any)
		if !ok {
			l.errf("%s: data.indices: each index is a list of columns", where)
			continue
		}
		var idx []string
		for _, c := range cols {
			idx = append(idx, fmt.Sprint(c))
		}
		if len(idx) > 0 {
			t.Indices = append(t.Indices, idx)
		}
	}

	if t.DisplayTemplate != "" {
		tmpl, err := ParseTemplate(t.DisplayTemplate)
		if err != nil {
			l.errf("%s: data.displayTemplate: %v", where, err)
		} else {
			l.checkTemplate(where, t, tmpl)
			t.Template = tmpl
		}
	}
	return t
}

// checkTemplate validates a display template's tokens against the type's own
// declarations, so a typo fails at load and not as an empty title. A bare
// token is a property, an edge, a column-backed property or {snippet}; a
// dotted one is an edge's property — the target is another type's business —
// or one level into an object property's declared fields.
func (l *loader) checkTemplate(where string, t *Kind, tmpl *Template) {
	for _, ref := range tmpl.Refs() {
		switch {
		case ref.Snippet:
		case ref.Edge != "":
			if _, ok := t.Edges[ref.Edge]; ok {
				continue
			}
			// A dotted token over a REFERENCE reads the referent's property,
			// the same hop an edge takes — the head is a pointer either way,
			// and only the storage differs. The property it names belongs to
			// the referent's kind, which `to:` may not even pin, so it is not
			// checkable here; the renderer answers "" for one that is absent.
			if p, ok := t.Props[ref.Edge]; ok && p.Datatype == DatatypeReference {
				continue
			}
			if p, ok := t.Props[ref.Edge]; ok && p.Datatype == DatatypeObject {
				if _, ok := p.Fields[ref.Prop]; ok {
					continue
				}
				l.errf("%s: data.displayTemplate: {%s.%s}: %s has no field %q",
					where, ref.Edge, ref.Prop, ref.Edge, ref.Prop)
				continue
			}
			l.errf("%s: data.displayTemplate: {%s.%s}: %s declares no edge, reference or object property %q",
				where, ref.Edge, ref.Prop, t.Name, ref.Edge)
		default:
			if p, ok := t.Props[ref.Prop]; ok {
				// A title is an unredacted, FTS-indexed column: a sensitive
				// property rendered into it would leak around every
				// read-surface redaction. The runtime resolver skips them too
				// (edge targets and legacy vocabularies), but a declaration
				// should fail loudly, not render empty.
				if p.Sensitive() {
					l.errf("%s: data.displayTemplate: {%s}: %s is %s-typed and a sensitive value never renders into a title",
						where, ref.Prop, ref.Prop, p.Datatype)
				}
				continue
			}
			if _, ok := t.Edges[ref.Prop]; ok {
				continue
			}
			if _, reserved := reservedProps[ref.Prop]; reserved {
				continue
			}
			l.errf("%s: data.displayTemplate: {%s}: %s declares no property or edge %q",
				where, ref.Prop, t.Name, ref.Prop)
		}
	}
}

// mapOfAny adapts a typed map to the shape sortedKeys reads, so key ordering
// is one implementation.
func mapOfAny[V any](m map[string]V) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// One casing rule, and the loader is where it is enforced (record 44's
// rider): every declared name and every system key is camelCase with
// initialisms uppercase — `displayTemplate`, `oneOf`, `ownerRef`, `onEnter`,
// `endsAt`, `icalUID`. The snake spellings are not aliases, they are errors:
// a silently-false `owner_ref` is exactly the failure this strictness exists
// to prevent.
var edgeKeys = map[string]bool{
	"to": true, "many": true, "required": true, "ownerRef": true,
	"description": true, "inverse": true, "inverseDescription": true,
}

// reservedProps are the five properties EVERY record already carries, each
// with its own storage column. They are written and read like
// any other property, so redeclaring one is not an override — it is two
// declarations of one name, and the write path would validate against the
// wrong one. `at`/`endsAt`/`dueAt` arrive through a temporal trait, and
// `title`/`body` are built in.
var reservedProps = map[string]string{
	"title":  "every record carries `title`; a derived one is a displayTemplate",
	"body":   "every record carries `body`",
	"at":     "`at` is the temporal trait's, bound with `traits: [temporal(point)]`",
	"endsAt": "`endsAt` is the temporal trait's, bound with `traits: [temporal(range)]`",
	"dueAt":  "`dueAt` is the temporal trait's, bound with `traits: [\"temporal(point: dueAt)\"]`",
}

// camelRule is the one sentence every declared name is checked against, so
// the loader says the same thing everywhere.
const camelRule = "camelCase ([a-z][a-zA-Z0-9]*)"

// maxDescription bounds a declared description: it is a tooltip, not
// documentation, so it is one short sentence — the manifest's comments stay
// the long-form home.
const maxDescription = 200

// maxKindDescription bounds a KIND's description. A kind's is not a tooltip:
// the console heads the kind's page with it, and a reader arriving at
// `core.substrate.reamde.dev/run` needs what the thing is AND what writes it,
// which is two sentences. Still one line — the folded scalar (`>-`) is how a
// manifest wraps one.
const maxKindDescription = 400

// parseDescription reads a declaration's `description`, holding it to the one
// rule: a single short sentence, no newlines, at most maxDescription chars.
func (l *loader) parseDescription(where string, d map[string]any) string {
	return l.parseDescriptionMax(where, d, maxDescription)
}

// parseDescriptionMax is parseDescription with the bound named, so a kind can
// carry two sentences where a property carries one. Newlines are refused
// either way: comments are the long-form home.
//
// The bound counts CHARACTERS, which is what the error says and what an author
// counts. Bytes would make the limit depend on the spelling — an em dash costs
// three of them — so a description of dashes would be held to a third of one
// in ASCII.
func (l *loader) parseDescriptionMax(where string, d map[string]any, max int) string {
	desc := mstr(d, "description")
	n := utf8.RuneCountInString(desc)
	switch {
	case desc == "":
		return ""
	case strings.ContainsAny(desc, "\n\r"):
		l.errf("%s.description: one short sentence, no newlines — comments are the long-form home", where)
		return ""
	case n > max:
		l.errf("%s.description: one short sentence (at most %d chars), got %d", where, max, n)
		return ""
	}
	return desc
}

// parseInverse reads the OPTIONAL name the other side of a pointer goes by —
// `thread` on a message declaring `inverse: messages`, which is what a reader
// standing on the thread calls the set pointing at it.
//
// It is a LABEL, never an identifier (Property.Inverse says why), so the only
// thing enforced HERE is that it is spelled like every other declared name, and
// that a description does not describe an inverse nobody declared. Collisions
// are settled where the authority is finalized (checkAuthorityInverses) and
// only within one authority — the name lives on the TARGET's side, so what the
// declaring kind happens to call its own properties cannot conflict with it.
func (l *loader) parseInverse(where string, d map[string]any) (name, description string) {
	name = mstr(d, "inverse")
	if name == "" {
		if mstr(d, "inverseDescription") != "" {
			l.errf("%s.inverseDescription: describes an `inverse` that is not declared", where)
		}
		return "", ""
	}
	if !ValidCamel(name) {
		l.errf("%s.inverse: %q must be %s", where, name, camelRule)
		return "", ""
	}
	return name, l.parseDescriptionMax(
		where+".inverseDescription",
		map[string]any{"description": mstr(d, "inverseDescription")},
		maxDescription,
	)
}

// maxDisplayName bounds a declared human label: it is a field caption, not a
// sentence, so it stays short and single-line.
const maxDisplayName = 80

// parseDisplayName reads a declaration's OPTIONAL `displayName` — the human
// label a client renders instead of the raw camelCase name. Absent is legal
// (the client humanizes the name itself); a present one is short and
// single-line.
func (l *loader) parseDisplayName(where string, d map[string]any) string {
	name := mstr(d, "displayName")
	switch {
	case name == "":
		return ""
	case strings.ContainsAny(name, "\n\r"):
		l.errf("%s.displayName: a short single-line label, no newlines", where)
		return ""
	case len(name) > maxDisplayName:
		l.errf("%s.displayName: a short label (at most %d chars), got %d", where, maxDisplayName, len(name))
		return ""
	}
	return name
}

// valueRule is the sentence enum and state VALUES are checked against: they
// are data, not names.
const valueRule = "a lowercase word ([a-z][a-z0-9]*)"

// enumValueKeys is the closed key set of a labeled enum value entry.
var enumValueKeys = map[string]bool{"value": true, "label": true}

// parseEnumValue reads ONE `values:` entry in either declared form (record
// 64): a bare scalar (`last30d`) is a value with no label; a mapping
// (`{value: last30d, label: "Last 30 days"}`) carries both. The value is held
// to the same lowercase-word rule whichever form declared it — labels are free
// text. Returns false (with an error recorded) on a malformed entry.
func (l *loader) parseEnumValue(where string, v any) (EnumValue, bool) {
	if m := asMapOrNil(v); m != nil {
		l.checkKeys(where+".values[]", m, enumValueKeys)
		val := mstr(m, "value")
		if !ValidValue(val) {
			l.errf("%s.values: %q must be %s", where, val, valueRule)
			return EnumValue{}, false
		}
		return EnumValue{Value: val, Label: mstr(m, "label")}, true
	}
	s := fmt.Sprint(v)
	if !ValidValue(s) {
		l.errf("%s.values: %q must be %s", where, s, valueRule)
		return EnumValue{}, false
	}
	return EnumValue{Value: s, Label: ""}, true
}

// enumValuesToAny renders a compiled value set back into the Definition map's
// canonical wire form — `[{value, label}]`, always both keys — so the read
// surfaces (the console's kind mirror) see one shape whether the manifest
// authored bare scalars or labeled mappings.
func enumValuesToAny(values []EnumValue) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = map[string]any{"value": v.Value, "label": v.Label}
	}
	return out
}

// asMapOrNil returns the map v decodes to, or nil when v is not a map — the
// discriminator parseEnumValue branches on (a mapping entry vs a bare scalar).
func asMapOrNil(v any) map[string]any {
	switch v.(type) {
	case map[string]any, map[any]any:
		return asMap(v)
	default:
		return nil
	}
}

// bindCapability resolves one `trait(variant: hot)` declaration against a
// trait definition, wherever that definition was declared. It returns the
// binding, the property contracts the trait imposes on the type (checked
// once every type is parsed), and any problems.
func bindCapability(typeIdent, capName, variant, cols string, c *Trait) (*TraitBinding, []pendingCapProp, []string) {
	var problems []string
	where := DocKind + " " + typeIdent + ": data.traits"
	errf := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	// The binding keeps the RESOLVED trait's identity beside the declared
	// bare name: host behavior compares identities, never spellings.
	b := &TraitBinding{Trait: capName, Identity: c.Identity(), Variant: variant, Columns: map[string]string{}}
	var props map[string]Datatype
	switch {
	case len(c.Variants) > 0:
		if variant == "" {
			errf("%s: %s requires a variant, e.g. %s(point)", where, capName, capName)
			return nil, nil, problems
		}
		v, ok := c.Variants[variant]
		if !ok {
			errf("%s: %s has no variant %q", where, capName, variant)
			return nil, nil, problems
		}
		props = v
	default:
		if variant != "" {
			errf("%s: %s declares no variants", where, capName)
			return nil, nil, problems
		}
		props = c.Properties
	}
	// Hot properties map onto record columns; everything else must be an
	// ordinary declared property of the same type.
	names := hotOrder(props)
	var remap []string
	for _, col := range strings.Split(cols, ",") {
		col = strings.TrimSpace(col)
		if col != "" {
			remap = append(remap, col)
		}
	}
	for i, pname := range names {
		if !isHot(pname) {
			continue
		}
		col := pname
		if i < len(remap) {
			col = remap[i]
		}
		if !isHot(col) {
			errf("%s: %q is not a hot property (at, endsAt, dueAt)", where, col)
			continue
		}
		b.Columns[pname] = col
	}
	var contracts []pendingCapProp
	for _, pname := range sortedNames(props) {
		if isHot(pname) {
			continue
		}
		// Non-hot trait properties are a type contract on the type's
		// own declarations.
		contracts = append(contracts, pendingCapProp{typeIdent, capName, pname, props[pname]})
	}
	return b, contracts, problems
}

func sortedNames(props map[string]Datatype) []string {
	out := make([]string, 0, len(props))
	for n := range props {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

type pendingCapProp struct {
	Kind     string // type identity
	Cap      string
	Prop     string
	Datatype Datatype
}

// pendingCapBinding is a `traits: [x]` declaration naming a trait
// the authority does not declare itself.
type pendingCapBinding struct {
	Kind    string // local type name
	Cap     string
	Variant string
	Cols    string
}

func hotOrder(props map[string]Datatype) []string {
	names := make([]string, 0, len(props))
	for n := range props {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if (names[i] == "at") != (names[j] == "at") {
			return names[i] == "at"
		}
		return names[i] < names[j]
	})
	return names
}

// isHot reports whether a trait property name is one of the three that
// occupy their own storage column. The declared spellings are camelCase; the
// columns underneath keep SQL's snake.
func isHot(s string) bool { return s == "at" || s == "endsAt" || s == "dueAt" }

// machineKeys are the state-property keys that describe the machine itself;
// the rest of propKeys is meaningless on one.
var machineKeys = map[string]bool{
	"type": true, "states": true, "initial": true, "transitions": true,
	"description": true, "displayName": true,
}

var transitionKeys = map[string]bool{
	"from": true, "to": true, "stamps": true, "onEnter": true,
}

// onEnterActions are the declared effects a transition may name.
var onEnterActions = map[string]bool{"applyDiff": true, "applyMerge": true}

func (l *loader) parseMachine(where, name string, d map[string]any) *Machine {
	m := &Machine{Name: name}
	for _, s := range mslice(d, "states") {
		str := fmt.Sprint(s)
		if !ValidValue(str) {
			l.errf("%s.states: %q must be %s", where, str, valueRule)
			continue
		}
		m.States = append(m.States, str)
	}
	if len(m.States) == 0 {
		l.errf("%s: states are required", where)
		return nil
	}
	switch init := d["initial"].(type) {
	case nil:
		m.Initial = m.States[0]
	case string:
		if !m.HasState(init) {
			l.errf("%s.initial: %q is not a declared state", where, init)
			return nil
		}
		m.Initial = init
	case map[string]any:
		l.errf("%s.initial: a single declared state — the per-actor map is deleted with the guards", where)
		return nil
	default:
		l.errf("%s.initial: expected a declared state", where)
		return nil
	}
	if _, ok := d["transitions"]; !ok {
		l.errf("%s: transitions are required", where)
		return nil
	}
	for i, tv := range mslice(d, "transitions") {
		td := asMap(tv)
		l.checkKeys(fmt.Sprintf("%s.transitions[%d]", where, i), td, transitionKeys)
		tr := &Transition{
			From:    mstr(td, "from"),
			To:      mstr(td, "to"),
			OnEnter: mstr(td, "onEnter"),
		}
		if !m.HasState(tr.From) || !m.HasState(tr.To) {
			l.errf("%s.transitions[%d]: %q → %q references an undeclared state", where, i, tr.From, tr.To)
			continue
		}
		if stamps := mmap(td, "stamps"); len(stamps) > 0 {
			tr.Stamps = map[string]string{}
			for prop, val := range stamps {
				if !ValidCamel(prop) {
					l.errf("%s.transitions[%d].stamps: %q must be %s", where, i, prop, camelRule)
					continue
				}
				v := fmt.Sprint(val)
				if v != "now" {
					l.errf("%s.transitions[%d].stamps.%s: only \"now\" is defined", where, i, prop)
					continue
				}
				tr.Stamps[prop] = v
			}
		}
		if tr.OnEnter != "" && !onEnterActions[tr.OnEnter] {
			l.errf("%s.transitions[%d].onEnter: only \"applyDiff\" and \"applyMerge\" are defined", where, i)
		}
		m.Transitions = append(m.Transitions, tr)
	}
	return m
}

// propKeys is the closed key set of an ordinary property. `ref` is gone with
// the kind it constrained (MODEL §11.5): edges are the one way to point at
// another record. `fields` is only meaningful on `type: object` and gets a
// targeted error anywhere else.
var propKeys = map[string]bool{
	"type": true, "repeated": true, "embed": true, "fts": true, "values": true,
	"pattern": true, "min": true, "max": true,
	"description": true, "base": true,
	"fields": true, "writer": true, "displayName": true,
	// Presentational hints the read surfaces (the console's config/account form)
	// consume verbatim from the Definition map: `required` marks a field the
	// form refuses to submit empty, `default` seeds a create (an enum's default
	// option). Both ride into Definition; the engine does not enforce them on
	// writes — but ADDING `required` to a stored declaration is a narrowing
	// change, refused by admission while live rows lack the property (ticket
	// 003, ruling A3).
	"required": true, "default": true,
	// renamedFrom is RESERVED for declared evolution:
	// the property's previous name, admitted and stored (it rides in the
	// Definition map like everything else) but not yet acted on — nothing
	// rewrites rows today. Shape-checked in parseProperty; the sibling and
	// reserved-name checks live in parseType, where the whole property set is
	// known.
	"renamedFrom": true,
}

// writerRoles is the closed set of a property's `writer:` restriction: who
// alone may write the property, enforced in the write path after the merged
// row is known.
var writerRoles = map[string]bool{
	WriterOAuth: true, WriterConnector: true, WriterOwner: true,
}

// objectPropKeys is an object property's own key set: no fts, no
// embed, no filter machinery — object properties stay out of all three until
// a consumer arrives (§15). `repeated: true` is allowed.
var objectPropKeys = map[string]bool{
	"type": true, "fields": true, "repeated": true, "description": true,
	"displayName": true,
}

// referencePropKeys is a reference property's own key set: `to:`
// pins the referent type (a full identity, a bare name, or `any`), and the
// value is a {authority, type, id} triple — the string-family refinements
// (pattern/min/max/values/fts/embed) never apply. `repeated: true` gives a
// list of references.
var referencePropKeys = map[string]bool{
	"type": true, "to": true, "repeated": true, "description": true,
	"displayName": true, "required": true, "renamedFrom": true,
	"inverse": true, "inverseDescription": true,
}

// fieldForbiddenKinds are the kinds an object field may not be:
// fields are one level deep and hold declared scalars — `json` is for shapes
// we do not own, a secret never hides inside a struct, a machine is a
// property of its own.
var fieldForbiddenKinds = map[Datatype]string{
	DatatypeObject:  "object fields are one level deep — no nested objects",
	DatatypeJSON:    "a field is a declared scalar; `json` is only for shapes we do not own",
	DatatypeSecret:  "a secret is its own property, never a field",
	DatatypeDigest:  "a digest is its own property, never a field",
	DatatypeState:   "a machine is its own property, never a field",
	DatatypeBlobRef: "a blob-ref is its own property, never a field — reads resolve it to a manifest",
}

func (l *loader) parseProperty(where, name string, d map[string]any, allowRefinement bool) *Property {
	raw := mstr(d, "type")
	if raw == "" {
		l.checkKeys(where, d, propKeys)
		l.errf("%s: type is required", where)
		return nil
	}
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		l.errf("%s: type %q: a list is `repeated: true` on the property, not a bracketed type", where, raw)
		return nil
	}
	p := &Property{Name: name, Repeated: mbool(d, "repeated")}
	// Every property kind may carry a description and a display label — the
	// ordinary key set, the state machine's and the object's all allow both.
	p.Description = l.parseDescription(where, d)
	p.DisplayName = l.parseDisplayName(where, d)
	kind := Datatype(raw)

	// A state property IS the machine (MODEL §11.4): its own key set, its own
	// parser, and nothing else on this branch applies to it.
	if kind == DatatypeState {
		l.checkKeys(where, d, machineKeys)
		switch {
		case !allowRefinement:
			l.errf("%s: state is a property type, not a base type to refine", where)
			return nil
		case p.Repeated:
			l.errf("%s: a state property holds one state, not a list", where)
			return nil
		}
		p.Datatype = DatatypeState
		p.Machine = l.parseMachine(where, name, d)
		if p.Machine == nil {
			return nil
		}
		return p
	}
	// An object property declares its fields inline: its own key
	// set, and each field is parsed like a property of the enclosing authority.
	if kind == DatatypeObject {
		l.checkKeys(where, d, objectPropKeys)
		if !allowRefinement {
			l.errf("%s: object is a property type, not a base type to refine", where)
			return nil
		}
		p.Datatype = DatatypeObject
		p.Fields = l.parseFields(where, d)
		if p.Fields == nil {
			return nil
		}
		for n := range p.Fields {
			p.FieldOrder = append(p.FieldOrder, n)
		}
		sort.Strings(p.FieldOrder)
		return p
	}
	// A reference property is a typed pointer: its own key set —
	// it takes `to:` (the referent-type constraint) and never the string-family
	// refinements (pattern/min/max/values/fts/embed have no meaning on a
	// {authority, type, id} value). `to:` resolves from a bare name to a full
	// identity in Finalize, like an edge's `to:`.
	if kind == DatatypeReference {
		l.checkKeys(where, d, referencePropKeys)
		if !allowRefinement {
			l.errf("%s: reference is a property type, not a base type to refine", where)
			return nil
		}
		p.Datatype = DatatypeReference
		if to := mstr(d, "to"); to != "" {
			p.To = to
		}
		p.Required = mbool(d, "required")
		p.Inverse, p.InverseDescription = l.parseInverse(where, d)
		if rf := mstr(d, "renamedFrom"); rf != "" {
			switch {
			case !ValidCamel(rf):
				l.errf("%s.renamedFrom: %q must be %s", where, rf, camelRule)
			case rf == name:
				l.errf("%s.renamedFrom: names the property itself", where)
			default:
				p.RenamedFrom = rf
			}
		}
		return p
	}
	l.checkKeys(where, d, propKeys)
	if _, hasFields := d["fields"]; hasFields {
		l.errf("%s: fields is only for `type: object` (record 49)", where)
		return nil
	}
	if !builtinKinds[kind] {
		dt, ok := l.authority.PropertyTypes[raw]
		if !ok || !allowRefinement {
			l.errf("%s: unknown property type %q", where, raw)
			return nil
		}
		ref := dt.Prop
		p.Datatype = ref.Datatype
		p.Refined = ref.Refined
		p.Pattern = ref.Pattern
		p.Min, p.Max = ref.Min, ref.Max
		p.Values = ref.Values
	} else {
		p.Datatype = kind
	}
	if pat := mstr(d, "pattern"); pat != "" {
		re, err := regexp.Compile(pat)
		if err != nil {
			l.errf("%s.pattern: %v", where, err)
		} else {
			p.Pattern = re
		}
	}
	if v, ok := mfloat(d, "min"); ok {
		p.Min = &v
	}
	if v, ok := mfloat(d, "max"); ok {
		p.Max = &v
	}
	for _, v := range mslice(d, "values") {
		ev, ok := l.parseEnumValue(where, v)
		if !ok {
			continue
		}
		p.Values = append(p.Values, ev)
	}
	if p.Datatype == DatatypeEnum && len(p.Values) == 0 {
		l.errf("%s: enum needs values", where)
	}
	// Canonicalize the Definition map's `values` to the labeled wire form so
	// the read surfaces see one shape regardless of how the manifest authored
	// them. Only when this property declared `values` inline: a datatype
	// refinement inherits its set from the base and carries no `values` key
	// here.
	if _, declared := d["values"]; declared && len(p.Values) > 0 {
		d["values"] = enumValuesToAny(p.Values)
	}
	p.Embed = mbool(d, "embed")
	p.FTS = IsShortString(p.Datatype) || IsLongText(p.Datatype)
	if p.Sensitive() {
		p.FTS = false
		p.Embed = false
	}
	if v, ok := d["fts"].(bool); ok {
		p.FTS = v && !p.Sensitive()
	}
	// A sensitive list would seal and scrub element-wise, and nothing needs
	// one: refusing it here keeps "redacted" meaning "the whole value".
	if p.Repeated && p.Sensitive() {
		l.errf("%s: a %s property holds one value, never a list", where, p.Datatype)
	}
	if p.Embed && !IsLongText(p.Datatype) && !IsShortString(p.Datatype) {
		l.errf("%s: embed is only meaningful for string-family properties", where)
	}
	if w := mstr(d, "writer"); w != "" {
		if !writerRoles[w] {
			l.errf("%s: writer %q is not a role — one of %s, %s, %s", where, w,
				WriterOAuth, WriterConnector, WriterOwner)
		} else {
			p.Writer = w
		}
	}
	p.Required = mbool(d, "required")
	// renamedFrom (reserved, ticket 003): the shape checks that need no
	// sibling knowledge. parseType finishes the job once the whole property
	// set is parsed.
	if rf := mstr(d, "renamedFrom"); rf != "" {
		switch {
		case !ValidCamel(rf):
			l.errf("%s.renamedFrom: %q must be %s", where, rf, camelRule)
		case rf == name:
			l.errf("%s.renamedFrom: names the property itself", where)
		default:
			p.RenamedFrom = rf
		}
	}
	return p
}

// parseFields parses an object property's field declarations:
// camelCase names, the reserved-word rule, scalar built-ins or authority-local
// refinements only, one level deep, one value each. A bare kind is shorthand
// for `{type: kind}`.
func (l *loader) parseFields(where string, d map[string]any) map[string]*Property {
	raw := mmap(d, "fields")
	if len(raw) == 0 {
		l.errf("%s: object needs fields", where)
		return nil
	}
	out := map[string]*Property{}
	for fname, fdef := range raw {
		fwhere := where + ".fields." + fname
		if !ValidCamel(fname) {
			l.errf("%s: must be %s", fwhere, camelRule)
			continue
		}
		if reason, reserved := reservedProps[fname]; reserved {
			l.errf("%s: %s", fwhere, reason)
			continue
		}
		fd := asMap(fdef)
		if s, ok := fdef.(string); ok {
			fd = map[string]any{"type": s}
		}
		// The forbidden kinds are refused by name before the parse: a state
		// declaration would otherwise fail on its missing machine first.
		if reason, bad := fieldForbiddenKinds[Datatype(mstr(fd, "type"))]; bad {
			l.errf("%s: %s (record 49)", fwhere, reason)
			continue
		}
		fp := l.parseProperty(fwhere, fname, fd, true)
		if fp == nil {
			continue
		}
		// A refinement resolves to its base kind, so the rule holds through it.
		if reason, bad := fieldForbiddenKinds[fp.Datatype]; bad {
			l.errf("%s: %s (record 49)", fwhere, reason)
			continue
		}
		if fp.Repeated {
			l.errf("%s: a field holds one value — repeat the object property, not the field", fwhere)
			continue
		}
		if fp.RenamedFrom != "" {
			l.errf("%s: renamedFrom is only for a type's own property, not a field", fwhere)
			continue
		}
		out[fname] = fp
	}
	return out
}

// add indexes a parsed authority without resolving cross-authority references.
func (r *Registry) add(g *Authority) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.authorities[g.Name]; ok {
		return fmt.Errorf("substrate/schema: authority %s already loaded", g.Name)
	}
	r.authorities[g.Name] = g
	r.order = append(r.order, g.Name)
	for _, name := range g.KindOrder {
		t := g.Kinds[name]
		r.byIdent[t.Identity] = t
		r.byName[t.Name] = append(r.byName[t.Name], t)
		key := g.Name + "/" + t.Plural
		if prev, ok := r.byPlural[key]; ok {
			return fmt.Errorf("substrate/schema: plural %q collides in authority %s (%s and %s)",
				t.Plural, g.Name, prev.Name, t.Name)
		}
		r.byPlural[key] = t
	}
	return nil
}

// Finalize resolves every authority's edge targets and trait contracts.
func (r *Registry) Finalize() error {
	r.mu.RLock()
	authorities := make([]*Authority, 0, len(r.order))
	for _, n := range r.order {
		authorities = append(authorities, r.authorities[n])
	}
	r.mu.RUnlock()
	var problems []string
	for _, g := range authorities {
		problems = append(problems, r.resolveAuthority(g)...)
	}
	problems = append(problems, r.mappingInvariantProblems()...)
	problems = append(problems, r.graphqlNameProblems()...)
	if len(problems) > 0 {
		return validationError(problems)
	}
	r.mu.Lock()
	r.version++
	r.mu.Unlock()
	return nil
}

// Validate reports whether an authority WOULD install, without installing it: the
// same resolution and projection checks Install runs, against a throwaway
// clone. Re-registering a connector whose manifest changed must fail at the
// registration that changed it — storing a manifest that only fails at the
// next repository-open turns a 200 into a boot that never completes.
func (r *Registry) Validate(g *Authority) error {
	probe := r.Clone()
	probe.remove(g.Name)
	return probe.Install(g)
}

// Install adds an already-parsed authority and resolves it, bumping the version
// counter the GraphQL layer caches against.
func (r *Registry) Install(g *Authority) error {
	if err := r.add(g); err != nil {
		return err
	}
	problems := r.resolveAuthority(g)
	// The registry-wide mapping invariants re-run whole: a re-registration
	// may add a mapping whose violation lives on an already-loaded edge.
	problems = append(problems, r.mappingInvariantProblems()...)
	if len(problems) > 0 {
		r.remove(g.Name)
		return validationError(problems)
	}
	r.mu.Lock()
	r.version++
	r.mu.Unlock()
	return nil
}

// Remove uninstalls an authority. It is the candidate-build step of a schema
// write: clone the live registry, remove the touched authorities, install their
// rebuilt replacements. Unknown names are no-ops.
func (r *Registry) Remove(authority string) { r.remove(authority) }

// InstallAll adds a set of parsed authorities and resolves them together, so
// authorities that reference each other install in any order — the shape both the
// batch apply verb and the repository-open rebuild need. On any problem nothing
// stays installed and every problem is reported at once.
func (r *Registry) InstallAll(authorities []*Authority) error {
	for i, g := range authorities {
		if err := r.add(g); err != nil {
			for _, undo := range authorities[:i] {
				r.remove(undo.Name)
			}
			return err
		}
	}
	var problems []string
	for _, g := range authorities {
		problems = append(problems, r.resolveAuthority(g)...)
	}
	problems = append(problems, r.mappingInvariantProblems()...)
	if len(problems) > 0 {
		for _, g := range authorities {
			r.remove(g.Name)
		}
		return validationError(problems)
	}
	r.mu.Lock()
	r.version++
	r.mu.Unlock()
	return nil
}

func (r *Registry) remove(authority string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	g, ok := r.authorities[authority]
	if !ok {
		return
	}
	delete(r.authorities, authority)
	for i, n := range r.order {
		if n == authority {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	for _, name := range g.KindOrder {
		t := g.Kinds[name]
		delete(r.byIdent, t.Identity)
		delete(r.byPlural, g.Name+"/"+t.Plural)
		rest := r.byName[t.Name][:0]
		for _, c := range r.byName[t.Name] {
			if c != t {
				rest = append(rest, c)
			}
		}
		r.byName[t.Name] = rest
	}
}

// checkAuthorityInverses refuses two pointers of ONE authority that claim the
// same inverse name on the same target: standing on that target, both would say
// `messages` and mean different sets, and the author who wrote both is the one
// who can fix it.
//
// Deliberately per-authority. Two authorities colliding is legal — an inverse is
// a label, never an identifier (Property.Inverse), so the reader sees two groups
// sharing a word, each named by the kind it comes from. Refusing that would let
// any bundle claim a common word and break every install that came after it,
// which is a far worse failure than a repeated label.
//
// Runs after `to:` resolution, so the target compared is the resolved identity;
// an unconstrained pointer (`any`, or no `to:`) names no target to collide on.
func (r *Registry) checkAuthorityInverses(g *Authority) []string {
	type claim struct{ kind, pointer string }
	seen := map[string]claim{}
	var problems []string
	take := func(where, target, inverse string, c claim) {
		if inverse == "" || target == "" || target == ToAny {
			return
		}
		key := target + "\x00" + inverse
		if prev, dup := seen[key]; dup {
			problems = append(problems, fmt.Sprintf(
				"%s: inverse %q on %s is already claimed by %s.%s — one authority cannot call two things by one name on one target",
				where, inverse, target, prev.kind, prev.pointer))
			return
		}
		seen[key] = c
	}
	for _, tn := range g.KindOrder {
		t := g.Kinds[tn]
		where := DocKind + " " + t.Identity
		for _, en := range t.EdgeOrder {
			e := t.Edges[en]
			take(where+": data.edges."+en, e.To, e.Inverse, claim{t.Name, en})
		}
		for _, pn := range t.PropOrder {
			p := t.Props[pn]
			if p.Datatype != DatatypeReference {
				continue
			}
			take(where+": data.properties."+pn, p.To, p.Inverse, claim{t.Name, pn})
		}
	}
	return problems
}

func (r *Registry) resolveAuthority(g *Authority) []string {
	var problems []string
	for _, tn := range g.KindOrder {
		t := g.Kinds[tn]
		where := DocKind + " " + t.Identity
		for _, en := range t.EdgeOrder {
			e := t.Edges[en]
			if e.To == "any" {
				continue
			}
			if Qualified(e.To) {
				if _, ok := r.ByIdentity(e.To); !ok {
					problems = append(problems, fmt.Sprintf("%s: data.edges.%s: unknown target type %q", where, en, e.To))
				}
				continue
			}
			// Short names resolve in-authority first, then uniquely across authorities.
			if local, ok := g.Kinds[e.To]; ok {
				e.To = local.Identity
				continue
			}
			resolved, err := r.Resolve(e.To)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: data.edges.%s: %v", where, en, err))
				continue
			}
			e.To = resolved.Identity
		}
		// Reference properties resolve their `to:` the same way an edge does:
		// a bare name to a full identity, in-authority first then uniquely across
		// authorities; `any` (and absent) stay unconstrained.
		for _, pn := range t.PropOrder {
			p := t.Props[pn]
			if p.Datatype != DatatypeReference || p.To == "" || p.To == ToAny {
				continue
			}
			if Qualified(p.To) {
				if _, ok := r.ByIdentity(p.To); !ok {
					problems = append(problems, fmt.Sprintf("%s: data.properties.%s.to: unknown referent type %q", where, pn, p.To))
				}
				continue
			}
			if local, ok := g.Kinds[p.To]; ok {
				p.To = local.Identity
				continue
			}
			resolved, err := r.Resolve(p.To)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: data.properties.%s.to: %v", where, pn, err))
				continue
			}
			p.To = resolved.Identity
		}
	}
	problems = append(problems, r.checkAuthorityInverses(g)...)
	// Mappings, once the authority's edge targets are resolved identities: the
	// registry-wide invariants (bipartite, one mapping per source, no edge
	// onto a source type) run after every authority has, in Finalize and Install.
	for _, mn := range g.MappingOrder {
		problems = append(problems, r.resolveMapping(g.Mappings[mn])...)
	}
	for _, fn := range g.FunctionOrder {
		problems = append(problems, r.resolveFunction(g.Functions[fn])...)
	}
	problems = append(problems, r.resolveAuthorityAgents(g)...)
	// Trait bindings that named a trait from another authority: the
	// registry is the only place all sibling authorities are visible.
	contracts := append([]pendingCapProp(nil), g.pending...)
	for _, pc := range g.pendingTraits {
		t, ok := g.Kinds[pc.Kind]
		if !ok {
			continue
		}
		c, err := r.ResolveTrait(g.Name, pc.Cap)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s %s: data.traits: %v", DocKind, t.Identity, err))
			continue
		}
		b, more, probs := bindCapability(t.Identity, pc.Cap, pc.Variant, pc.Cols, c)
		problems = append(problems, probs...)
		contracts = append(contracts, more...)
		if b != nil {
			t.applyCapability(*b)
		}
	}
	g.pendingTraits = nil

	for _, p := range contracts {
		t, ok := r.ByIdentity(p.Kind)
		if !ok {
			continue
		}
		where := DocKind + " " + p.Kind
		prop, ok := t.Props[p.Prop]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: trait %s requires property %s: %s",
				where, p.Cap, p.Prop, p.Datatype))
			continue
		}
		if prop.Datatype != p.Datatype {
			problems = append(problems, fmt.Sprintf("%s: data.properties.%s: trait %s requires type %s, got %s",
				where, p.Prop, p.Cap, p.Datatype, prop.Datatype))
		}
	}
	// The bundle's configType, after trait bindings resolved (bundle.go).
	problems = append(problems, r.resolveBundle(g)...)
	return problems
}

// --- small YAML map helpers ---

func asMap(v any) map[string]any {
	switch m := v.(type) {
	case map[string]any:
		return m
	case map[any]any:
		out := map[string]any{}
		for k, val := range m {
			out[fmt.Sprint(k)] = val
		}
		return out
	default:
		return map[string]any{}
	}
}

func mstr(m map[string]any, k string) string {
	if v, ok := m[k]; ok && v != nil {
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprint(v)
	}
	return ""
}

func mbool(m map[string]any, k string) bool {
	v, _ := m[k].(bool)
	return v
}

func mfloat(m map[string]any, k string) (float64, bool) {
	switch v := m[k].(type) {
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case float64:
		return v, true
	default:
		return 0, false
	}
}

func mmap(m map[string]any, k string) map[string]any {
	if v, ok := m[k]; ok {
		return asMap(v)
	}
	return map[string]any{}
}

func mslice(m map[string]any, k string) []any {
	v, _ := m[k].([]any)
	return v
}

// graphqlNameProblems is the DECLARATION-TIME uniqueness check on resolved
// GraphQL names. Two kinds that resolve to one
// GraphQL object name would make the schema unbuildable — or, worse, let a
// later declaration rename an earlier one — so the SECOND declaration is
// refused here, at the same moment every other narrowing refusal happens,
// rather than at the next schema build. Finalize runs on the CANDIDATE
// registry of every declaration write (engine/schemawrite.go), so this is
// checked before any row lands.
func (r *Registry) graphqlNameProblems() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	byName := map[string][]string{}
	for _, t := range r.byIdent {
		name := GraphQLName(t.Identity, t.Source)
		if name == "" {
			continue
		}
		byName[name] = append(byName[name], t.Identity)
	}
	var problems []string
	for name, idents := range byName {
		if len(idents) < 2 {
			continue
		}
		sort.Strings(idents)
		problems = append(problems, fmt.Sprintf(
			"graphql name %s is claimed by %s — one kind per GraphQL name; rename one of them",
			name, strings.Join(idents, " and ")))
	}
	sort.Strings(problems)
	return problems
}
