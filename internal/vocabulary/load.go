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
	l := &loader{source: source}
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
		authority := d.DeclaredAuthority()
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
	// source is the origin BuildAuthorities was called with, before the
	// vocabulary-bundle override below promotes an individual authority to
	// `builtin`. One rule reads it — `runtime: host`, which only the shipped
	// build may declare (function.go) — and it reads the CALL's source on
	// purpose: a vocabulary bundle is shipped VOCABULARY however it arrived, but
	// it is still installed, and the engine implements only what the engine
	// ships.
	source string
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
	g.Version = l.parseVersion("authority "+name, gd.authority.Data)
	if g.Version == 0 {
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
		}, false, 1)
		if p == nil {
			continue
		}
		p.Refined = local
		// THE ONE VALUE THE PARSE NORMALIZES, and it normalizes a COPY. Core's
		// `propertytype` declares `values` as a repeated OBJECT, so the row this
		// declaration projects into must hold {value, label} entries whichever form
		// the author wrote — a bare scalar would be refused by the meta-kind's own
		// declaration. The caller's document is left untouched: nothing this parse
		// does may reach a map somebody else holds.
		data := d.Data
		if _, declared := data["values"]; declared && len(p.Values) > 0 {
			data = mapWith(data, "values", enumValuesToAny(p.Values))
		}
		if _, dup := g.PropertyTypes[local]; dup {
			l.errf("%s: declared twice", where)
			continue
		}
		g.PropertyTypes[local] = &PropertyType{
			Name: local, Authority: name, Base: p.Datatype, Prop: p,
			Definition: data, SourceYAML: d.Source,
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
		if one, has := d.Data["oneOf"]; has {
			c.Variants = l.parseTraitVariants(where, one)
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
	// closure check against the authority's own members; the inputs resolve
	// in Finalize/Install, after trait bindings.
	l.buildBundle(gd)
	return g
}

// traitVariantKeys is one `oneOf:` entry's closed key set.
var traitVariantKeys = map[string]bool{"name": true, "properties": true}

// parseTraitVariants reads a trait's `oneOf:` — the variant set a kind picks one
// of with `traits: [temporal(point)]`. A variant is an entry carrying its own
// name ({name, properties}), and the LIST of them is the one spelling: a mapping
// of name to properties is refused, because a keyed map of keyed maps leaves
// every reader guessing which level a path addresses. Stored traits written that
// way are translated by the dialect rung (engine/dialectonegrammar.go).
//
// Nil when nothing is declared, which is what makes a trait variant-free.
func (l *loader) parseTraitVariants(where string, raw any) map[string]map[string]Datatype {
	out := map[string]map[string]Datatype{}
	switch list := raw.(type) {
	case []any:
		for i, ev := range list {
			vwhere := fmt.Sprintf("%s: data.oneOf[%d]", where, i)
			ed := asMapOrNil(ev)
			if ed == nil {
				l.errf("%s: a variant is a {name, properties} map, got %T", vwhere, ev)
				continue
			}
			l.checkKeys(vwhere, ed, traitVariantKeys)
			name := mstr(ed, "name")
			if !ValidCamel(name) {
				l.errf("%s.name: %q must be %s", vwhere, name, camelRule)
				continue
			}
			if _, dup := out[name]; dup {
				l.errf("%s.name: variant %q is declared twice", vwhere, name)
				continue
			}
			out[name] = l.parseTraitVariantProps(vwhere+".properties", mmap(ed, "properties"))
		}
	default:
		names := sortedKeys(asMap(raw))
		l.errf("%s: data.oneOf: a mapping of variant name to properties — the variants are a LIST: [{name: %s, properties: {…}}, …]",
			where, firstOr(names, "<variant>"))
		return nil
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// firstOr is the first name a refusal can quote, or a placeholder when there is
// none to quote.
func firstOr(names []string, fallback string) string {
	if len(names) == 0 {
		return fallback
	}
	return names[0]
}

// parseTraitVariantProps reads one variant's properties. A variant's properties
// are built-in datatypes only: the variants exist for the machinery bound to
// record columns, and a refinement is authority-local while a trait resolves
// across authorities.
func (l *loader) parseTraitVariantProps(where string, props map[string]any) map[string]Datatype {
	out := map[string]Datatype{}
	for _, pname := range sortedKeys(props) {
		if !ValidCamel(pname) {
			l.errf("%s.%s: must be %s", where, pname, camelRule)
			continue
		}
		k := Datatype(fmt.Sprint(props[pname]))
		if !builtinKinds[k] {
			l.errf("%s.%s: unknown property type %q", where, pname, k)
			continue
		}
		out[pname] = k
	}
	return out
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

// declarationDataKeys is every schema kind's admitted `data` key set, keyed by
// the manifest short name — the same maps checkKeys holds each document to.
var declarationDataKeys = map[string]map[string]bool{
	DocAuthority:     authorityDataKeys,
	DocActor:         actorDataKeys,
	DocKind:          typeDataKeys,
	DocTrait:         capabilityDataKeys,
	DocPropertyType:  datatypeDataKeys,
	DocRecordMapping: mappingDataKeys,
	DocFunction:      functionDataKeys,
	DocAgent:         agentDataKeys,
	DocBundle:        bundleDataKeys,
}

// DeclarationDataKeys is the `data` keys a schema kind's document admits.
//
// It exists because a declaration ROW carries more than its document does: what
// the engine stamps (a version, an origin, the quarantine marks, the bundle
// lifecycle bools) and, on a repository an older binary once wrote, the retired
// spellings. Reading a row back as a document is therefore a WHITELIST — these
// keys and nothing else — which is what keeps a property some FUTURE binary
// stamps from reaching this loader as an unknown key, and keeps the engine from
// holding a second, hand-maintained copy of this set.
//
// The map is a copy: the sets themselves are this package's own.
func DeclarationDataKeys(short string) map[string]bool {
	keys := declarationDataKeys[short]
	out := make(map[string]bool, len(keys))
	for k := range keys {
		out[k] = true
	}
	return out
}

// deletedDeclarationKeys is every schema kind's own deleted `data` keys, keyed
// by the manifest short name. The kinds absent from it retired no key of their
// own; the keys every kind retired live in deletedDataKeys.
var deletedDeclarationKeys = map[string]map[string]string{
	DocFunction: deletedFunctionKeys,
	DocAgent:    deletedAgentKeys,
}

// DeletedDeclarationKeys is one schema kind's deleted `data` keys, each naming
// what replaced it.
//
// The YAML door refuses a document by this set (parseFunction, parseAgent). It
// is exported because a declaration also arrives as PROPERTIES, through the
// generic record verbs, and that door has to say the same sentence: a PUT
// carrying `emit` is the same mistake as a manifest carrying it, and being told
// "not declared" there while the loader names `permissions.writes` here would
// make the fix depend on which door the writer knocked at.
//
// The map is a copy: the sets themselves are this package's own.
func DeletedDeclarationKeys(short string) map[string]string {
	keys := deletedDeclarationKeys[short]
	out := make(map[string]string, len(keys))
	for k, v := range keys {
		out[k] = v
	}
	return out
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
//
// The last block is dialect 1's own row spellings: the `definition` blob and the
// mirrors a pre-typed projection wrote beside it. They are not manifest keys
// anybody authored, but a legacy EXPORT is a document too, and `apply -f` of one
// arrives here. The write path already names them (engine's
// checkDeclarationWrite), so this door says the same thing rather than "unknown
// key".
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
	"configType":         "inputs — named configuration needs, each naming a kind; resolution is bound edge, the id \"default\", then the sole live record",

	"definition": "the retired blob: a declaration carries its own properties now, one key per authored field",
	"name":       "the retired mirror: a declaration's local name is metadata.id",
	"plural":     "the retired mirror: a kind's collection segment is `names.plural`",
	"functions":  "the retired mirror: an agent names its callables under `tools`",
	"subagents":  "the retired mirror: an agent names its sub-agents under `agents`",
	"sourceYAML": "the retired mirror: nothing stores a document's text, and the parsed declaration is the row",

	// A pointer is a POINTER now. `refersTo` was a hint beside a string, read by
	// nothing server-side and enforced nowhere; the property it marked is
	// `type: reference` with a `kind:` pin, which is checked on every write.
	"refersTo": "`type: reference` with a `kind:` pin — a pointer is a reference, and the pin is enforced rather than suggested",
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

// indexKeys is one declared index's key set: the properties it covers, in order.
var indexKeys = map[string]bool{"properties": true}

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
	if v := l.parseVersion(where, d); v != 0 {
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
		p := l.parseProperty(fmt.Sprintf("%s: data.properties.%s", where, pname), pname, asMap(pdef), true, 1)
		if p == nil {
			continue
		}
		t.Props[pname] = p
		if p.Machine == nil {
			continue
		}
		t.Machines[pname] = p.Machine
	}
	// A stamp target is a stored property, and the author may say so: a
	// declared target must be a single-valued datetime, because that is what
	// the engine writes into it. One left undeclared is still synthesized as
	// an implicit datetime property, NOT refused: stored declarations written
	// before targets were declarable must keep parsing at open, or the
	// repository holding them cannot be opened to upgrade them. The shipped
	// tree itself declares every target (cmd/kindsgen refuses an undeclared
	// one).
	for _, pname := range sortedKeys(mapOfAny(t.Machines)) {
		for _, tr := range t.Machines[pname].Transitions {
			for _, stamp := range sortedKeys(mapOfAny(tr.Stamps)) {
				if existing, ok := t.Props[stamp]; ok {
					if !existing.Implicit &&
						(existing.Datatype != DatatypeDatetime || existing.Repeated || existing.Keyed) {
						l.errf("%s: data.properties.%s: stamp target %q must be a single-valued datetime property",
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
		e := &Edge{
			Name:               ename,
			Description:        l.parseDescription(ewhere, ed),
			To:                 to,
			Many:               mbool(ed, "many"),
			Required:           mbool(ed, "required"),
			OwnerRef:           mbool(ed, "ownerRef"),
			Inverse:            inverse,
			InverseDescription: inverseDesc,
			Deprecated:         mbool(ed, "deprecated"),
		}
		if e.Deprecated && e.Required {
			l.errf("%s: an edge is deprecated or required, never both: a client cannot stop offering a link a creation is refused without", ewhere)
		}
		e.Props, e.PropOrder = l.parseEdgeProps(ewhere, ed)
		t.Edges[ename] = e
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

	// indices. An index NAMES its properties (`{properties: [...]}`); the bare
	// list of names a kind used to be written with is refused, since one shape
	// per property is what lets the meta-kind declare this one. Stored rows
	// written that way are translated by the dialect rung
	// (engine/dialectonegrammar.go).
	for _, iv := range mslice(d, "indices") {
		if cols, bare := iv.([]any); bare {
			l.errf("%s: data.indices: a bare list of property names — an index names them: {properties: %v}", where, cols)
			continue
		}
		im := asMapOrNil(iv)
		if im == nil {
			l.errf("%s: data.indices: each index names its properties ({properties: [...]})", where)
			continue
		}
		l.checkKeys(where+": data.indices[]", im, indexKeys)
		cols, ok := im["properties"].([]any)
		if !ok {
			l.errf("%s: data.indices: properties is a list of property names", where)
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
// token is a property, an edge, a column-backed property or a DERIVED token
// ({snippet}, {localName}, {id}); a dotted one is an edge's property — the
// target is another type's business — or one level into an object property's
// declared fields.
func (l *loader) checkTemplate(where string, t *Kind, tmpl *Template) {
	for _, ref := range tmpl.Refs() {
		switch {
		// A derived token needs no declaration to check against: it is computed
		// from the record. But a kind MAY declare a property or an edge of the
		// token's name, and then the declaration is what renders
		// (Template.Render), so the token is held to the same rules the bare form
		// gets — a sensitive property must not reach a title by wearing a derived
		// token's name. The answer is discarded on purpose: undeclared is legal
		// here and nowhere else.
		case ref.Derived != "":
			l.ownToken(where, t, ref.Derived)
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
			if l.ownToken(where, t, ref.Prop) {
				continue
			}
			l.errf("%s: data.displayTemplate: {%s}: %s declares no property or edge %q",
				where, ref.Prop, t.Name, ref.Prop)
		}
	}
}

// ownToken resolves a BARE token against the kind's own declarations and reports
// whether anything declared the name: a property, an edge, or one of the
// column-backed properties every record carries. It is where the refusals that
// belong to a bare token live, so the derived tokens get them too.
//
// A title is an unredacted, FTS-indexed column, so a sensitive property rendered
// into one would leak around every read-surface redaction. The runtime resolver
// skips them as well (edge targets and legacy vocabularies), but a declaration
// should fail loudly rather than render empty.
func (l *loader) ownToken(where string, t *Kind, name string) bool {
	if p, ok := t.Props[name]; ok {
		if p.Sensitive() {
			l.errf("%s: data.displayTemplate: {%s}: %s is %s-typed and a sensitive value never renders into a title",
				where, name, name, p.Datatype)
		}
		return true
	}
	if _, ok := t.Edges[name]; ok {
		return true
	}
	_, reserved := reservedProps[name]
	return reserved
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
	// RESERVED, both of them: `deprecated` is the marker every declaration
	// carries, and `properties` declares what an edge ROW may hold. See
	// Edge.Deprecated and Edge.Props.
	"deprecated": true, "properties": true,
}

// edgePropKeys is the closed key set of ONE declared edge property: the scalar
// half of propKeys and nothing else.
//
// What is missing is the point. `repeated`/`keyed` are containers, and an edge
// property is one flat value; `fts`/`embed` are index placement on a record's
// own columns, which an edge row does not have; `managed`/`writer` name a
// stamping engine and a restricted writer, neither of which reaches an edge
// row; `default` would need a `json` field in core's `kind` declaration, which
// the dialect refuses inside an object; `renamedFrom` and `unique` are
// evolution and identity, both of which belong to the record the edge hangs
// off. Every one of them can be added later, which is the ordinary
// coordinated event this reservation exists to make rarer, not to abolish.
var edgePropKeys = map[string]bool{
	"type": true, "description": true, "displayName": true,
	"required": true, "deprecated": true,
	"values": true, "pattern": true, "min": true, "max": true,
}

// edgePropForbiddenKinds are the datatypes an edge property may never be, each
// saying why. The rule behind the list: an edge row is a LINK with a few flat
// values on it, and anything that needs a shape, a machine, a resolver or a
// lifecycle of its own is a record: declare the record and hang two edges off
// it.
var edgePropForbiddenKinds = map[Datatype]string{
	DatatypeObject:    "an edge property is a flat value; a shape with fields is a record, with an edge at each end",
	DatatypeJSON:      "`json` is a shape we do not own, and an edge row is not a place to hide one",
	DatatypeState:     "a machine belongs to a record, which can be transitioned; an edge row cannot",
	DatatypeSecret:    "a secret is a property of a record, sealed and read through its own path",
	DatatypeDigest:    "a digest is minted onto a record",
	DatatypeBlobRef:   "a blob-ref resolves on a record's read path, which an edge row does not have",
	DatatypeReference: "the edge IS the pointer, and a second one on the same row is a record with two ends",
}

// parseEdgeProps reads the RESERVED `edges.<rel>.properties` block: the
// properties an edge row of this rel may carry. Nothing validates an edge write
// against it yet (issue 111); this is the declaration door alone.
//
// Each entry parses through parseProperty, so a refinement resolves, an enum
// gets its values and a pattern compiles exactly as they do on a record's own
// property. The narrower key set is checked FIRST, so `repeated: true` on an
// edge property is named as the key it is rather than parsed and then
// contradicted.
func (l *loader) parseEdgeProps(where string, ed map[string]any) (map[string]*Property, []string) {
	if _, declared := ed["properties"]; !declared {
		return nil, nil
	}
	raw := mmap(ed, "properties")
	if len(raw) == 0 {
		l.errf("%s.properties: an edge declares the properties its rows carry; drop the key rather than declaring none", where)
		return nil, nil
	}
	out := map[string]*Property{}
	for pname, pdef := range raw {
		pwhere := where + ".properties." + pname
		if !ValidCamel(pname) {
			l.errf("%s: must be %s", pwhere, camelRule)
			continue
		}
		pd := asMapOrNil(pdef)
		if pd == nil {
			// No bare-datatype shorthand here, and that is deliberate: the
			// shorthand belongs to an object's `fields:`, and what an edge
			// declares has to survive the round trip into core's `kind` row,
			// which holds each edge property as a mapping.
			l.errf("%s: an edge property is a mapping, `{type: int}`, not a bare datatype", pwhere)
			continue
		}
		l.checkKeys(pwhere, pd, edgePropKeys)
		// Refused by name before the parse: a state declaration would otherwise
		// fail on its missing machine first, and an object on its missing fields.
		if reason, bad := edgePropForbiddenKinds[Datatype(mstr(pd, "type"))]; bad {
			l.errf("%s: %s", pwhere, reason)
			continue
		}
		// MaxFieldDepth, not 1: an edge property declares no fields, and a depth
		// at the floor means any nesting that ever reached here is refused by the
		// same guard a record's own properties are held to.
		p := l.parseProperty(pwhere, pname, pd, true, MaxFieldDepth)
		if p == nil {
			continue
		}
		// A refinement resolves to its base datatype, so the rule holds through
		// one: `type: isbn` is a string here and `type: someObject` is refused.
		if reason, bad := edgePropForbiddenKinds[p.Datatype]; bad {
			l.errf("%s: %s", pwhere, reason)
			continue
		}
		// THE AUTHORED BLOCK IS THE STORED BLOCK, and this is what keeps it so.
		// A record property may write `values: [low, high]`, but core's `kind`
		// holds an edge property's values as {value, label} objects, so
		// admitting the bare word here would mean rewriting somebody's
		// declaration on its way into the row: the row would then differ from
		// the document that produced it, which is how a byte-identical re-apply
		// starts bumping a version every time.
		for _, v := range mslice(pd, "values") {
			if asMapOrNil(v) == nil {
				l.errf("%s.values: an edge property spells a value as a mapping, `{value: %v}`, never a bare word", pwhere, v)
			}
		}
		out[pname] = p
	}
	if len(out) == 0 {
		return nil, nil
	}
	order := make([]string, 0, len(out))
	for n := range out {
		order = append(order, n)
	}
	sort.Strings(order)
	return out, order
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

// maxCallableDescription bounds a FUNCTION's description, and a function's is
// not a tooltip either: it is the model-facing tool CARD, the whole text an LLM
// reads before deciding to call. The four host functions' cards teach an entire
// surface — the `graphql` one names every root, the batching advice and the two
// refusals — and they used to be Go string literals with no bound at all. Still
// one line: a folded scalar (`>-`) is how a declaration wraps one.
const maxCallableDescription = 1000

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
// `deprecated` is the RESERVED marker on one value: removing a value live
// records hold is a narrowing the guard refuses, so deprecating it is the move
// the dialect has to have a word for.
var enumValueKeys = map[string]bool{"value": true, "label": true, "deprecated": true}

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
		return EnumValue{Value: val, Label: mstr(m, "label"), Deprecated: mbool(m, "deprecated")}, true
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
//
// `deprecated` is rendered only where it was declared. It is an absent-means-
// false marker like every other one, and writing it out everywhere would
// rewrite the stored form of every refinement that already carries values.
func enumValuesToAny(values []EnumValue) []any {
	out := make([]any, len(values))
	for i, v := range values {
		m := map[string]any{"value": v.Value, "label": v.Label}
		if v.Deprecated {
			m["deprecated"] = true
		}
		out[i] = m
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
	// `deprecated` is admitted on every property branch, a machine included: a
	// picker that stops offering a deprecated property stops offering it
	// whether the property holds a value or a state.
	"deprecated": true,
}

var transitionKeys = map[string]bool{
	"from": true, "to": true, "stamps": true, "onEnter": true, "notifies": true,
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
			From:     mstr(td, "from"),
			To:       mstr(td, "to"),
			OnEnter:  mstr(td, "onEnter"),
			Notifies: mstr(td, "notifies"),
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
	// `keyed` is `repeated`'s twin (a map instead of a list) and `keyPattern` is
	// the contract its keys hold to; `managed` says the engine stamps the
	// property. Both are declared shape, so they ride into the Definition map
	// like every other key.
	"keyed": true, "keyPattern": true, "managed": true,
	// The two the engine enforces on writes: `required` says the record holds a
	// value for this property after every write, `default` says what a create
	// that does not name it stores. Both also ride into Definition, where the
	// read surfaces (the console's config/account form) consume them verbatim.
	// ADDING `required` to a stored declaration is a narrowing change, refused
	// by admission while live rows lack the property (ticket 003, ruling A3).
	"required": true, "default": true,
	// renamedFrom is RESERVED for declared evolution:
	// the property's previous name, admitted and stored (it rides in the
	// Definition map like everything else) but not yet acted on — nothing
	// rewrites rows today. Shape-checked in parseProperty; the sibling and
	// reserved-name checks live in parseType, where the whole property set is
	// known.
	"renamedFrom": true,
	// `unique` and `deprecated` are RESERVED the same way and for the same
	// reason: a key set is closed, so an unknown key quarantines the authority
	// that ships it, and a dialect key nobody can use until every binary in an
	// ecosystem has been upgraded is a key that has to land before the closure
	// that wants it. Both are validated here and stored in the Definition map;
	// neither changes a write. See Property.Unique and Property.Deprecated.
	"unique": true, "deprecated": true,
}

// uniqueForbiddenKinds are the datatypes `unique:` never applies to, each
// saying why. The line is whether a stored value has an equality an index
// could police: a container has none that means what the author intends, and a
// sealed secret compares as its ciphertext, which differs per write of the
// same plaintext.
var uniqueForbiddenKinds = map[Datatype]string{
	DatatypeObject:  "an object holds fields; mark the field that identifies the record, not the wrapper",
	DatatypeJSON:    "`json` is a shape we do not own; there is no value to hold unique",
	DatatypeState:   "a machine is a position, and every record of a kind passes through the same states",
	DatatypeSecret:  "a secret stores sealed, so two writes of one value are two different stored values",
	DatatypeBlobRef: "a blob-ref names bytes many records may legitimately share",
}

// writerRoles is the closed set of a property's `writer:` restriction: who
// alone may write the property, enforced in the write path after the merged
// row is known.
var writerRoles = map[string]bool{
	WriterOAuth: true, WriterConnector: true, WriterOwner: true,
}

// objectPropKeys is an object property's own key set: no fts, no
// embed, no filter machinery — object properties stay out of all three until
// a consumer arrives (§15). `repeated: true` is allowed, and `keyed: true` is
// its twin.
// `unique` is absent on purpose: an object's identifying value is one of its
// fields, and `unique` on a field is refused too (parseFields), so the whole
// question stays where an index could answer it.
var objectPropKeys = map[string]bool{
	"type": true, "fields": true, "repeated": true, "description": true,
	"displayName": true, "keyed": true, "keyPattern": true, "managed": true,
	"deprecated": true,
}

// referencePropKeys is a reference property's own key set: `kind:` pins WHICH
// KIND's records the pointer names (a full identity, a bare name, or `any`),
// and the string-family refinements (pattern/min/max/values/fts/embed) never
// apply to a pointer. `repeated: true` gives a list of references, and a
// reference is admitted inside an object or a keyed map like any other field.
//
// The pin is `kind:`, not `to:`, because the two words say different things.
// `to:` is the EDGE's: the far end of a traversable relationship. A reference
// property is data that NAMES A RECORD, and what a reader needs from the
// declaration is which kind's records those are — that is the word a picker
// keys on. A reference still spelling `to:` is refused by name
// (deletedReferencePropKeys).
var referencePropKeys = map[string]bool{
	"type": true, "kind": true, "repeated": true, "description": true,
	"displayName": true, "required": true, "renamedFrom": true,
	"inverse": true, "inverseDescription": true,
	"keyed": true, "keyPattern": true, "managed": true,
	// `unique` on a pointer is the one-to-one link: at most one live record may
	// name any given referent. Reserved like everywhere else: nothing enforces
	// it yet.
	"unique": true, "deprecated": true,
}

// deletedReferencePropKeys are the reference declaration's retired keys, each
// naming its replacement. There is one, and it is a rename rather than a
// removal: the pin outlived the word.
var deletedReferencePropKeys = map[string]string{
	"to": "kind — `to:` is the EDGE's word; a reference property pins the kind whose records it names",
}

// fieldForbiddenKinds are the kinds an object field may not be, at any level:
// each of them is a whole property. `json` is for shapes we do not own and a
// field is declared shape by definition; a secret never hides inside a struct
// (redacted means the whole value); a digest is server-minted; a machine moves
// by transition, which needs a property to move; a blob-ref resolves to a
// manifest on read, which reads walk properties to do.
var fieldForbiddenKinds = map[Datatype]string{
	DatatypeJSON:    "a field is declared shape; `json` is only for shapes we do not own",
	DatatypeSecret:  "a secret is its own property, never a field",
	DatatypeDigest:  "a digest is its own property, never a field",
	DatatypeState:   "a machine is its own property, never a field",
	DatatypeBlobRef: "a blob-ref is its own property, never a field — reads resolve it to a manifest",
}

// keyedForbiddenKinds are the datatypes `keyed:` never applies to. They are
// fieldForbiddenKinds for the same reason plus one: a keyed `json` would be two
// escape hatches stacked, and a `json` property already holds the whole map.
var keyedForbiddenKinds = map[Datatype]string{
	DatatypeJSON:    "`json` already holds a map — a keyed json is two escape hatches in one",
	DatatypeSecret:  "a secret holds one value, never a map of them",
	DatatypeDigest:  "a digest holds one value, never a map of them",
	DatatypeState:   "a machine is one state, never a map of them",
	DatatypeBlobRef: "a blob-ref names one blob, never a map of them",
}

func (l *loader) parseProperty(where, name string, d map[string]any, allowRefinement bool, depth int) *Property {
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
	// `keyed` and `managed` are read before the branch: all three parse branches
	// (scalar, object, reference) admit them, and the state branch's own key set
	// refuses them as unknown.
	p.Keyed = mbool(d, "keyed")
	p.Managed = mbool(d, "managed")
	// `deprecated` is read here for the same reason: all four branches admit it,
	// because a client stops offering a deprecated declaration whatever shape it
	// holds. It is RESERVED: stored, read by no server-side path.
	p.Deprecated = mbool(d, "deprecated")
	if p.Keyed && p.Repeated {
		l.errf("%s: keyed and repeated are the two containers — a declaration is one or the other", where)
		return nil
	}
	if kp := mstr(d, "keyPattern"); kp != "" {
		switch {
		case !p.Keyed:
			l.errf("%s: keyPattern is the contract on a KEYED map's keys — declare `keyed: true` or drop it", where)
		case kp != KeyPatternCamel && kp != KeyPatternKindRef:
			l.errf("%s: keyPattern %q is not a contract — one of %s, %s", where, kp, KeyPatternCamel, KeyPatternKindRef)
		default:
			p.KeyPattern = kp
		}
	}

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
		// A keyed object's fields describe its VALUES, so leaving them out is the
		// one way to reach for a map of maps — refused by name rather than as a
		// bare "object needs fields", because the author asking for it has to hear
		// what to write instead.
		if p.Keyed && len(mmap(d, "fields")) == 0 {
			l.errf("%s: a keyed object declares the fields its values follow — a map OF maps is not declarable; flatten it or make the inner level a repeated variant list", where)
			return nil
		}
		// THE CLOSED EMPTY OBJECT: `fields: {}`, declared and empty, is a value
		// with no fields at all — every key refused, which is what an arm that
		// says "present" and nothing more needs (a trigger's webhook source). It
		// is spelled apart from an ABSENT `fields:`, which is the author forgetting
		// to say what the object holds.
		if raw, declared := d["fields"]; declared && len(asMap(raw)) == 0 {
			p.Fields = map[string]*Property{}
			return p
		}
		p.Fields = l.parseFields(where, d, depth+1)
		if p.Fields == nil {
			return nil
		}
		for n := range p.Fields {
			p.FieldOrder = append(p.FieldOrder, n)
		}
		sort.Strings(p.FieldOrder)
		return p
	}
	// A reference property is a typed pointer: its own key set — it takes
	// `kind:` (which kind's records it names) and never the string-family
	// refinements (pattern/min/max/values/fts/embed have no meaning on a record
	// reference). The pin resolves from a bare name to a full identity in
	// Finalize, exactly as an edge's `to:` does.
	if kind == DatatypeReference {
		for k := range d {
			if replacement, gone := deletedReferencePropKeys[k]; gone {
				l.errf("%s: key %q is deleted — %s", where, k, replacement)
				return nil
			}
		}
		l.checkKeys(where, d, referencePropKeys)
		if !allowRefinement {
			l.errf("%s: reference is a property type, not a base type to refine", where)
			return nil
		}
		p.Datatype = DatatypeReference
		if pin := mstr(d, "kind"); pin != "" {
			p.To = pin
		}
		p.Required = mbool(d, "required")
		p.Inverse, p.InverseDescription = l.parseInverse(where, d)
		l.parseReservedMarkers(where, name, d, p)
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
	// NOTHING IS WRITTEN BACK. A property's `values` stay exactly as the author
	// spelled them — bare scalars or {value, label} mappings — because the map this
	// parse walks IS the declaration a row stores (engine/vocabularywrite.go
	// authorityDeclarations), and rewriting it here would store a document nobody
	// wrote. Both spellings parse to the same Values, and every reader of the
	// stored form takes both (the console's parseEnumValues, EnumValue's own
	// UnmarshalYAML).
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
	// A keyed map is a container, and containers stay out of FTS, embed and the
	// filter grammar exactly as objects do: a map renders as "" everywhere a
	// value is read as a string, so an indexed keyed property would claim a band
	// it can never fill.
	if p.Keyed {
		if reason, bad := keyedForbiddenKinds[p.Datatype]; bad {
			l.errf("%s: %s", where, reason)
			return nil
		}
		// The DECLARED keys, not the computed ones: FTS defaults to true for the
		// whole string family, and a keyed string is exactly the case that must
		// leave the band without the author being blamed for it.
		if mbool(d, "fts") || mbool(d, "embed") {
			l.errf("%s: a keyed map stays out of fts and embed, exactly as an object does", where)
		}
		p.FTS, p.Embed = false, false
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
	if raw, declared := d["default"]; declared {
		p.Default = l.parseDefault(where, p, raw)
	}
	l.parseReservedMarkers(where, name, d, p)
	return p
}

// parseReservedMarkers reads the three RESERVED property keys the scalar and
// reference branches share (`renamedFrom`, `unique` and `deprecated`), and
// records the shape problems that need no knowledge of the property's
// siblings. parseType finishes renamedFrom's job once the whole property set is
// parsed; nothing finishes the other two, because nothing acts on them yet.
func (l *loader) parseReservedMarkers(where, name string, d map[string]any, p *Property) {
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
	if mbool(d, "unique") {
		switch {
		case p.Repeated || p.Keyed:
			l.errf("%s.unique: one value per record; a list or a map holds several, and nothing says which of them is the unique one", where)
		default:
			if reason, bad := uniqueForbiddenKinds[p.Datatype]; bad {
				l.errf("%s.unique: %s", where, reason)
			} else {
				p.Unique = true
			}
		}
	}
	// A form told to stop offering a property it may not submit without has
	// nothing left to do, so the pair is refused rather than left to a client to
	// resolve.
	if p.Deprecated && p.Required {
		l.errf("%s: a property is deprecated or required, never both: required means a form refuses to submit without it", where)
	}
}

// parseDefault holds a declared `default:` to a value this property could
// actually store: one literal of the declared family, an enum's own value, and
// never on a container or a property whose value is not the author's to write.
// The value's OWN rules — a pattern, a bound, an instant's range — are the
// write path's coercion, which admission runs over every declared default
// (checkDeclaredDefaults, in internal/engine), so a kind whose default no write
// could store is refused there rather than at every create.
func (l *loader) parseDefault(where string, p *Property, v any) any {
	switch {
	case v == nil:
		l.errf("%s.default: a default is a value — absent is what having none means", where)
		return nil
	case p.Repeated || p.Keyed:
		l.errf("%s.default: a default fills one value, and this property holds a %s",
			where, map[bool]string{true: "list", false: "map"}[p.Repeated])
		return nil
	case p.Managed:
		l.errf("%s.default: the engine stamps a managed property, so a default would never be its value", where)
		return nil
	case p.Sensitive():
		l.errf("%s.default: a %s value is never written into a declaration", where, p.Datatype)
		return nil
	case p.Datatype == DatatypeBlobRef:
		l.errf("%s.default: a blob-ref names bytes that exist, which a declaration cannot", where)
		return nil
	}
	switch p.Datatype {
	case DatatypeBool:
		if _, ok := v.(bool); !ok {
			l.errf("%s.default: expected true or false", where)
			return nil
		}
	case DatatypeInt, DatatypeFloat:
		switch v.(type) {
		case int, int64, float64:
		default:
			l.errf("%s.default: expected a number", where)
			return nil
		}
	case DatatypeJSON:
		// A json property holds a shape we do not own, so its default is any
		// literal the document carried.
	default:
		s, ok := v.(string)
		if !ok {
			l.errf("%s.default: expected a %s, written as a string", where, p.Datatype)
			return nil
		}
		if vals := p.ValueStrings(); len(vals) > 0 && !slices.Contains(vals, s) {
			l.errf("%s.default: %q is not one of %s", where, s, strings.Join(vals, ", "))
			return nil
		}
	}
	return v
}

// parseFields parses one object level's field declarations: camelCase names,
// the reserved-word rule, declared scalars, references or further objects, each
// a single value, a list (`repeated`) or a map (`keyed`). A bare kind is
// shorthand for `{type: kind}`.
//
// depth is the level the FIELDS sit at — a kind's own property is level 1, so
// its fields are level 2 — and MaxFieldDepth is the floor: the guards that
// refuse a narrowing walk this nesting one jsonb notch per level
// (engine/schemadiff.go), and a dialect that recursed without a bound would
// need a general path walker there instead.
func (l *loader) parseFields(where string, d map[string]any, depth int) map[string]*Property {
	if depth > MaxFieldDepth {
		l.errf("%s: fields nest %d levels deep at most (a kind's own property is level 1) — flatten this one",
			where, MaxFieldDepth)
		return nil
	}
	raw := mmap(d, "fields")
	if len(raw) == 0 {
		l.errf("%s: object needs fields — `fields: {}` is the closed empty object, and absent is not that", where)
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
		fp := l.parseProperty(fwhere, fname, fd, true, depth)
		if fp == nil {
			continue
		}
		// A refinement resolves to its base kind, so the rule holds through it.
		if reason, bad := fieldForbiddenKinds[fp.Datatype]; bad {
			l.errf("%s: %s (record 49)", fwhere, reason)
			continue
		}
		if fp.RenamedFrom != "" {
			l.errf("%s: renamedFrom is only for a type's own property, not a field", fwhere)
			continue
		}
		// `unique` names a whole property one index can police. Inside an object
		// it would name a position within one value, and under a repeated or keyed
		// container several positions per record, so it is refused where the
		// constraint could not be stated, rather than stored and later found
		// unenforceable.
		if fp.Unique {
			l.errf("%s: unique marks a type's own property, not a field", fwhere)
			continue
		}
		// `managed` says the ENGINE stamps a property; the write path stamps
		// properties, never positions inside one, so on a field it would be a
		// claim nothing can honor.
		if fp.Managed {
			l.errf("%s: managed marks a type's own property, not a field", fwhere)
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
	problems = append(problems, r.bundleNameProblems()...)
	if len(problems) > 0 {
		return validationError(problems)
	}
	r.mu.Lock()
	r.version++
	r.mu.Unlock()
	return nil
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
		// A KIND'S OWN reference properties, and deliberately not the reference
		// FIELDS inside its objects.
		//
		// A nested `inverse` was admitted and STORED by every earlier binary (the
		// key set allowed it; resolution and this check simply walked past it), so
		// claiming one here would make a declaration that finalized yesterday
		// inadmissible today — a core parse of it is fatal at repository open and
		// an installed closure quarantines, both for a label nothing resolves,
		// routes or looks anything up by. This check runs on the STORED-row
		// rebuild and on a fresh admission through the same Install, so there is
		// no seam that could hold new declarations to a stricter rule without
		// holding old rows to it too. The claim moves to depth the day something
		// CONSUMES a nested inverse: then the rebuild needs a tolerant path
		// first, and this comment is the record of why.
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

// referenceSite is one reference-typed declaration found on a kind: the
// property, and the dotted path that addresses it ("callable", or
// "tools.fields.callable" for one declared inside an object).
type referenceSite struct {
	Path string
	Prop *Property
}

// referenceSites lists every reference a kind declares — its own properties and
// the reference FIELDS inside object properties, keyed maps and repeated objects
// alike, to MaxFieldDepth. `to:` resolution walks it, so a nested pointer cannot
// be half-admitted: before this, a reference field parsed and then never had its
// `to:` resolved at all, leaving the write path to compare a bare name against a
// full identity. The inverse-claim check deliberately does NOT
// (checkAuthorityInverses says why).
func referenceSites(t *Kind) []referenceSite {
	var out []referenceSite
	for _, pn := range t.PropOrder {
		out = appendReferenceSites(out, pn, t.Props[pn])
	}
	return out
}

func appendReferenceSites(out []referenceSite, path string, p *Property) []referenceSite {
	if p.Datatype == DatatypeReference {
		out = append(out, referenceSite{Path: path, Prop: p})
	}
	for _, fn := range p.FieldOrder {
		out = appendReferenceSites(out, path+".fields."+fn, p.Fields[fn])
	}
	return out
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
		// authorities; `any` (and absent) stay unconstrained. Every admitted
		// depth, not just the kind's own properties: an unresolved `to:` on a
		// nested reference would compare a bare name against a full identity on
		// every write and refuse the value the declaration asked for.
		for _, site := range referenceSites(t) {
			p := site.Prop
			if p.To == "" || p.To == ToAny {
				continue
			}
			if Qualified(p.To) {
				if _, ok := r.ByIdentity(p.To); !ok {
					problems = append(problems, fmt.Sprintf("%s: data.properties.%s.to: unknown referent type %q", where, site.Path, p.To))
				}
				continue
			}
			if local, ok := g.Kinds[p.To]; ok {
				p.To = local.Identity
				continue
			}
			resolved, err := r.Resolve(p.To)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: data.properties.%s.to: %v", where, site.Path, err))
				continue
			}
			p.To = resolved.Identity
		}
		// A `notifies:` transition reports into a thread, so the marker must
		// name a reference property PINNED to core's llmthread — and, until
		// the resume bounds have earned wider trust, only core's own kinds
		// carry it (docs/plans/thread-interactions.md): a bundle kind minting
		// resume-on-transition would be an unbounded paid-compute trigger.
		machineNames := make([]string, 0, len(t.Machines))
		for mn := range t.Machines {
			machineNames = append(machineNames, mn)
		}
		sort.Strings(machineNames)
		for _, mn := range machineNames {
			for i, tr := range t.Machines[mn].Transitions {
				if tr.Notifies == "" {
					continue
				}
				at := fmt.Sprintf("%s: data.properties.%s.transitions[%d].notifies", where, mn, i)
				if t.Authority != "core.substrate.reamde.dev" {
					problems = append(problems, fmt.Sprintf("%s: only core kinds may notify a thread in this build", at))
					continue
				}
				p, ok := t.Prop(tr.Notifies)
				if !ok || p.Datatype != DatatypeReference || p.To != KindLLMThread {
					problems = append(problems, fmt.Sprintf("%s: %q must be a reference property pinned to %s", at, tr.Notifies, KindLLMThread))
				}
			}
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
	// The bundle's inputs, after trait bindings resolved (bundle.go).
	problems = append(problems, r.resolveBundle(g)...)
	return problems
}

// --- small YAML map helpers ---

// mapWith is m with one key replaced, on a COPY: the declaration a parse hands
// on may differ from the document only where the meta-kind's own shape demands
// it, and never by mutating what the caller holds.
func mapWith(m map[string]any, key string, value any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	out[key] = value
	return out
}

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

// parseVersion reads a document's own `version` out of its data: an
// incremental integer of at least 1, or 0 when the document carries none.
// Anything else is a problem, and a string gets the pointed message — the
// retired `v1alpha3` spelling must refuse loudly, not order silently.
func (l *loader) parseVersion(where string, d map[string]any) int64 {
	raw, ok := d["version"]
	if !ok || raw == nil {
		return 0
	}
	if _, isString := raw.(string); isString {
		l.errf("%s: data.version is an incremental integer now (v1alpha3 became 3), not %q", where, raw)
		return 0
	}
	v, ok := VersionValue(raw)
	if !ok || v < 1 {
		l.errf("%s: data.version must be an integer of at least 1, not %v", where, raw)
		return 0
	}
	return v
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

// bundleNameProblems is the DECLARATION-TIME uniqueness check on bundle names.
// A bundle's name is its authority's first label (bundle.go), and three things
// key on it alone: the actor an install writes under (`bundle:<name>`), the
// prefix every installed kind's GraphQL name carries, and the bundle's own
// `metadata.id` suffix. While an extension authority had to be spelled
// "<name>.bundles.substrate.reamde.dev", one label meant one authority and the
// uniqueness came free; the authority is free now, so two of them can reach for
// one label and it is checked here instead. The actor is the reason this cannot
// be left to the GraphQL check: two bundles sharing `bundle:llm` write as each
// other, and the trigger loop guard keys on exactly that name — it would drop
// one bundle's writes as though they were the other's echo, silently.
func (r *Registry) bundleNameProblems() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	byName := map[string][]string{}
	for name, g := range r.authorities {
		if g.Bundle == nil {
			continue
		}
		byName[g.Bundle.Name] = append(byName[g.Bundle.Name], name)
	}
	var problems []string
	for name, authorities := range byName {
		if len(authorities) < 2 {
			continue
		}
		sort.Strings(authorities)
		problems = append(problems, fmt.Sprintf(
			"bundle name %s is claimed by %s — a bundle's name is its authority's first label, and it is the actor an install writes under; rename one of them",
			name, strings.Join(authorities, " and ")))
	}
	sort.Strings(problems)
	return problems
}
