// Package kinddialect reads one authority directory of kind declarations in the
// property dialect and nothing else: no registry, no cross-document
// resolution, no semantic validation. It is the reader cmd/kindsgen generates
// from.
//
// It exists as a SECOND reader on purpose. The generator's output is compiled
// into internal/corekinds, so a generator that read the declarations through
// internal/vocabulary would sit downstream of code it produced only indirectly
// — and one broken generated file would take the loader's package graph, and
// with it the generator, out of the build. Nothing here imports anything from
// this module; yaml.v3 is the whole dependency.
//
// The price of a second reader is drift, and the conformance test in
// internal/corekinds is what is paid instead: it holds every document's
// property set, datatype, enum values, container flags and nesting depth
// against internal/vocabulary's loader, in both directions. A feature this
// reader does not understand is a REFUSAL, never a skip: generating a type that
// silently drops a declared property is the one failure a generated file cannot
// be reviewed out of.
package kinddialect

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// MaxFieldDepth is how deep declared fields nest: a kind's own property is
// level 1, so a level-MaxFieldDepth field holds a scalar and never an object.
// It mirrors vocabulary.MaxFieldDepth, and the conformance test holds the two
// to the same answer on every shipped document.
const MaxFieldDepth = 4

// Kind is one `core.substrate.reamde.dev/kind` document, as the dialect reads
// it: what a generator needs and not one field more.
type Kind struct {
	// File is the document's path inside the authority directory, for an error
	// that names where to go.
	File string
	// Ref is the kind reference, which is the document's metadata.id.
	Ref       string
	Authority string
	Name      string // names.singular
	Plural    string
	// Version is the EFFECTIVE version: the kind's own `data.version` where it
	// pins one, else its authority's.
	Version         string
	Description     string
	DisplayTemplate string
	// Props are the declared properties in AUTHORED order: generated output
	// follows the document's own shape, so a reviewer can read the two side by
	// side.
	Props []*Property
}

// Property is one declared value slot, at any admitted depth.
type Property struct {
	Name        string
	DisplayName string
	Description string
	// Datatype is the declared built-in: the spellings vocabulary.Datatype
	// carries. A refinement (an authority-local property type) is refused
	// rather than resolved — core declares none, and resolving one needs the
	// registry this reader deliberately does not build.
	Datatype string
	Repeated bool
	Keyed    bool
	// KeyPattern is the declared contract a keyed map's keys hold to: "camel",
	// "kindRef", or empty for any non-empty key.
	KeyPattern string
	// Required mirrors the declared `required:` hint. It is METADATA and never a
	// container decision: the write path does not enforce it (vocabulary
	// Property.Required), so a stored row may lack a required property and the
	// generated types have to be able to hold that row.
	Required bool
	Managed  bool
	Writer   string
	// RenamedFrom, Inverse and InverseDescription are admitted, carried and
	// generated INTO NOTHING: a rename nothing performs yet and a label nothing
	// resolves by change no type. They are read so the conformance test can hold
	// both readers to one answer about them.
	RenamedFrom        string
	Inverse            string
	InverseDescription string
	// To is a reference property's declared referent, verbatim: a full kind
	// reference, a bare kind name or "any". Resolution belongs to the registry.
	To string
	// Values is an enum's admissible set in declaration order, each value with
	// its OPTIONAL label.
	Values []EnumValue
	// Pattern is the declared regexp source, uncompiled: the generated decoder
	// compiles it.
	Pattern string
	Min     *float64
	Max     *float64
	// Fields are an object property's declared fields, in authored order; nil
	// for every other datatype.
	Fields []*Property
	// Machine is a `type: state` property's machine; nil for every other
	// datatype.
	Machine *Machine
	// Depth is the level this property sits at: a kind's own property is 1.
	Depth int
	// Implicit marks a property no `properties:` block declares: a transition's
	// stamp target, which the loader declares for the author as a datetime. It is
	// a stored property like any other — a reader that skipped it would generate a
	// type that refuses a row the engine itself wrote.
	Implicit bool
}

// EnumValue is one admissible enum value and the optional human label a client
// renders beside it.
type EnumValue struct {
	Value string
	Label string
}

// Machine is a state property's declared machine. A state is NOT stored in the
// properties map: it lives in the record's own state column, so a generator
// keeps it apart from the property struct it generates.
type Machine struct {
	States      []string
	Initial     string
	Transitions []Transition
}

// Transition is one legal move, with the properties the move stamps and the
// declared onEnter effect.
type Transition struct {
	From    string
	To      string
	OnEnter string
	// Stamps are the stamped property names in sorted order, each mapped to the
	// declared stamp value ("now" is the only one defined).
	Stamps []Stamp
}

// Stamp is one property a transition stamps.
type Stamp struct {
	Property string
	Value    string
}

// Field returns the named field of an object property.
func (p *Property) Field(name string) (*Property, bool) {
	for _, f := range p.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return nil, false
}

// ReadDir reads every kind document in an authority DIRECTORY.
func ReadDir(dir string) ([]*Kind, error) { return ReadFS(os.DirFS(dir)) }

// ReadFS reads every kind document in an authority directory served as a
// filesystem whose root IS that directory. The result is ordered by kind
// reference, so the generated output does not depend on directory order.
func ReadFS(fsys fs.FS) ([]*Kind, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	r := &reader{authorityVersion: DefaultVersion}
	var out []*Kind
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		raw, err := fs.ReadFile(fsys, e.Name())
		if err != nil {
			return nil, err
		}
		kinds, err := r.readFile(e.Name(), raw)
		if err != nil {
			return nil, err
		}
		out = append(out, kinds...)
	}
	r.checkOtherDocuments(out)
	// The EFFECTIVE version, defaulted exactly as the loader defaults it: a
	// kind's own `data.version` where it pins one, else the authority's, else
	// v1alpha1. The boot upgrade keys on this answer, so a reader that reported
	// the authored value would disagree with the loader on every kind that pins
	// nothing.
	for _, k := range out {
		if k.Version == "" {
			k.Version = r.authorityVersion
		}
	}
	if err := r.err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	return out, nil
}

// DefaultVersion is the version a declaration projects with when neither it nor
// its authority pins one. It mirrors vocabulary.DefaultVersion.
const DefaultVersion = "v1alpha1"

// reader collects every refusal before reporting, so one run names every
// declaration a generator would have to be told about rather than the first.
type reader struct {
	problems []string
	// authorityVersion is the authority document's `data.version`, which every
	// kind that pins none projects with.
	authorityVersion string
	// others are the documents in this directory that are NOT kind
	// declarations, by their envelope's kind reference and the file it sat in.
	// They are judged once the declared kinds are known: a data record of a
	// declared kind is legitimately not generator input, and a MISSPELLED
	// document kind is a declaration silently missing from the generated types.
	others []otherDocument
}

type otherDocument struct {
	ref  string
	file string
}

func (r *reader) errf(format string, args ...any) {
	r.problems = append(r.problems, fmt.Sprintf(format, args...))
}

func (r *reader) err() error {
	if len(r.problems) == 0 {
		return nil
	}
	sort.Strings(r.problems)
	return fmt.Errorf("kinddialect: %d declaration(s) this reader does not understand:\n  %s",
		len(r.problems), strings.Join(r.problems, "\n  "))
}

// AuthorityCore publishes the meta-kinds, so every VOCABULARY document names one
// of its kinds whatever authority the document declares into. It mirrors
// vocabulary.AuthorityCore.
const AuthorityCore = "core.substrate.reamde.dev"

// kindDocumentRef is the one document kind this reader reads.
const kindDocumentRef = AuthorityCore + "/kind"

// vocabularyDocumentKinds are the other documents a kinds/ directory holds — the
// authority header, its actors, its traits, its property types, its mappings and
// its callables. They are recognized and skipped, which is different from being
// ignored: a document kind that is on NEITHER this list nor the tree's own
// declared kinds is a misspelling, and a misspelled `kind:` would drop a whole
// declaration out of the generated types without a word. It mirrors
// vocabulary's schemaDocumentKinds.
var vocabularyDocumentKinds = keys("authority", "kind", "trait", "propertytype",
	"recordmapping", "function", "agent", "actor", "bundle")

// The manifest envelope: four keys, and the keys the pinned design REMOVED each
// naming what took its job. A document still carrying one would otherwise look
// obeyed. Both mirror vocabulary/document.go.
var (
	envelopeKeys        = keys("kind", "metadata", "data", "status")
	metadataKeys        = keys("id", "labels", "annotations")
	deletedEnvelopeKeys = map[string]string{
		"apiVersion": "kind (the version left the envelope)",
		"group":      "kind (one kind reference names the authority and the name)",
		"type":       "kind",
		"spec":       "data",
	}
)

func (r *reader) readFile(file string, raw []byte) ([]*Kind, error) {
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	var out []*Kind
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		if len(doc.Content) == 0 {
			continue
		}
		m, ok := asMapping(doc.Content[0])
		if !ok {
			return nil, fmt.Errorf("%s: a document is a mapping", file)
		}
		ref := m.str("kind")
		where := file + ": " + ref
		if id := documentID(m); id != "" {
			where = file + ": " + id
		}
		r.checkEnvelope(where, m)
		if ref != kindDocumentRef {
			// The AUTHORITY header carries the version every kind that pins none
			// projects with, so it is read even though it generates nothing.
			if ref == AuthorityCore+"/"+docAuthority {
				r.readAuthorityVersion(where, m)
			}
			r.others = append(r.others, otherDocument{ref: ref, file: file})
			continue
		}
		if k := r.readKind(file, m); k != nil {
			out = append(out, k)
		}
	}
	return out, nil
}

const docAuthority = "authority"

// checkEnvelope holds the four-key envelope and the metadata block closed. A key
// nobody reads is a key an author believes in: `metadata.typo` beside a real id
// reads as accepted, and the next reader of the file copies it.
func (r *reader) checkEnvelope(where string, doc *mapping) {
	for _, k := range doc.keys {
		if replacement, deleted := deletedEnvelopeKeys[k]; deleted {
			r.errf("%s: %q was replaced by %s", where, k, replacement)
			continue
		}
		if !envelopeKeys[k] {
			r.errf("%s: unknown envelope key %q — a document is kind, metadata, data and status", where, k)
		}
	}
	if meta, ok := asMapping(doc.at("metadata")); ok {
		r.checkKeys(where+".metadata", meta, metadataKeys)
	}
}

func (r *reader) readAuthorityVersion(where string, doc *mapping) {
	data, ok := asMapping(doc.at("data"))
	if !ok {
		r.errf("%s: an authority document carries data", where)
		return
	}
	if v := data.str("version"); v != "" {
		r.authorityVersion = v
	}
}

// checkOtherDocuments judges the documents this reader did not generate from. A
// vocabulary document (the authority, an actor, a trait) and a DATA record of a
// kind the tree declares are both legitimately not generator input; anything
// else naming one of this authority's own kinds is a name nothing declares, and
// the likeliest cause is a typo in `kind:` — which silently costs a whole
// declaration.
//
// A document naming ANOTHER authority's kind is not judged: whether that kind
// exists is the registry's question, and this reader holds one directory.
func (r *reader) checkOtherDocuments(declared []*Kind) {
	authority := r.authority(declared)
	local := map[string]bool{}
	for _, k := range declared {
		local[k.Name] = true
	}
	for _, other := range r.others {
		refAuthority, name := splitRef(other.ref)
		switch {
		case other.ref == "":
			r.errf("%s: a document declares its kind", other.file)
		case refAuthority == AuthorityCore && vocabularyDocumentKinds[name]:
		case refAuthority == authority && local[name]:
		case refAuthority == authority || refAuthority == AuthorityCore:
			r.errf("%s: document kind %q is neither a vocabulary document nor a kind %s declares — a misspelled `kind:` drops the whole document out of the generated types",
				other.file, other.ref, refAuthority)
		}
	}
}

// authority is the authority this directory declares into: every kind document
// agrees on it, and a disagreement is a directory holding two authorities, which
// no shipped tree does.
func (r *reader) authority(declared []*Kind) string {
	authority := ""
	for _, k := range declared {
		switch {
		case authority == "":
			authority = k.Authority
		case k.Authority != authority:
			r.errf("%s: %s declares into %s while the rest of the directory declares into %s",
				k.File, k.Ref, k.Authority, authority)
		}
	}
	return authority
}

// splitRef splits a kind reference on its ONE slash, mirroring
// vocabulary.SplitKindRef: a bare name has no authority.
func splitRef(ref string) (authority, name string) {
	if i := strings.Index(ref, "/"); i >= 0 {
		return ref[:i], ref[i+1:]
	}
	return "", ref
}

func documentID(doc *mapping) string {
	meta, ok := asMapping(doc.at("metadata"))
	if !ok {
		return ""
	}
	return meta.str("id")
}

// kindDataKeys mirrors vocabulary's typeDataKeys. An unknown key here is a
// dialect the generator has not been taught, and generating past it would
// produce a type that quietly means something else.
var kindDataKeys = keys("authority", "names", "displayTemplate", "properties",
	"edges", "traits", "indices", "description", "version")

var namesKeys = keys("singular", "plural")

func (r *reader) readKind(file string, doc *mapping) *Kind {
	meta, _ := asMapping(doc.at("metadata"))
	data, ok := asMapping(doc.at("data"))
	if meta == nil || !ok {
		r.errf("%s: a kind document carries metadata and data", file)
		return nil
	}
	k := &Kind{
		File:            file,
		Ref:             meta.str("id"),
		Authority:       data.str("authority"),
		Version:         data.str("version"),
		Description:     data.str("description"),
		DisplayTemplate: data.str("displayTemplate"),
	}
	where := file + ": " + k.Ref
	r.checkKeys(where+".data", data, kindDataKeys)
	names, _ := asMapping(data.at("names"))
	if names == nil {
		r.errf("%s: data.names is required", where)
		return nil
	}
	r.checkKeys(where+".data.names", names, namesKeys)
	k.Name, k.Plural = names.str("singular"), names.str("plural")
	if k.Ref == "" || k.Name == "" || k.Plural == "" {
		r.errf("%s: a kind declares metadata.id, names.singular and names.plural", where)
		return nil
	}
	props, _ := asMapping(data.at("properties"))
	if props == nil {
		r.errf("%s: data.properties is required", where)
		return nil
	}
	for _, name := range props.keys {
		if p := r.readProperty(where+".properties."+name, name, props.at(name), 1); p != nil {
			k.Props = append(k.Props, p)
		}
	}
	r.addStampProperties(where, k)
	return k
}

// addStampProperties declares what a transition's stamps declare for the
// author: an implicit datetime property per stamp target, appended in sorted
// order. The loader does exactly this (a stamp is a stored property with its own
// column-less slot in properties), so a reader that stopped at the authored
// block would generate a type that refuses `decidedAt` — a value the engine
// itself stamps.
func (r *reader) addStampProperties(where string, k *Kind) {
	declared := map[string]*Property{}
	for _, p := range k.Props {
		declared[p.Name] = p
	}
	var stamped []string
	for _, p := range k.Props {
		if p.Machine == nil {
			continue
		}
		for _, t := range p.Machine.Transitions {
			for _, s := range t.Stamps {
				if existing, ok := declared[s.Property]; ok {
					if !existing.Implicit {
						r.errf("%s: stamp %q collides with a declared property", where, s.Property)
					}
					continue
				}
				implicit := &Property{
					Name:     s.Property,
					Datatype: TypeDatetime,
					Depth:    1,
					Implicit: true,
				}
				declared[s.Property] = implicit
				stamped = append(stamped, s.Property)
			}
		}
	}
	sort.Strings(stamped)
	for _, name := range stamped {
		k.Props = append(k.Props, declared[name])
	}
}

// The key sets of the three parse branches, mirroring vocabulary's propKeys,
// objectPropKeys, referencePropKeys and machineKeys. `embed`, `fts`, `default`
// and `renamedFrom` are admitted and carry nothing into a generated type: the
// first two are index placement, the third a form hint, the fourth reserved for
// a rewrite nothing performs yet.
var (
	scalarKeys = keys("type", "repeated", "embed", "fts", "values", "pattern",
		"min", "max", "description", "base", "fields", "writer", "displayName",
		"keyed", "keyPattern", "managed", "required", "default",
		"renamedFrom")
	objectKeys = keys("type", "fields", "repeated", "description", "displayName",
		"keyed", "keyPattern", "managed")
	referenceKeys = keys("type", "kind", "repeated", "description", "displayName",
		"required", "renamedFrom", "inverse", "inverseDescription", "keyed",
		"keyPattern", "managed")
	machineKeys    = keys("type", "states", "initial", "transitions", "description", "displayName")
	transitionKeys = keys("from", "to", "stamps", "onEnter")
	keyPatterns    = keys("camel", "kindRef")
	writerRoles    = keys("oauth", "connector", "owner")
)

// Datatype spellings, mirroring vocabulary's Datatype constants. They are
// listed rather than derived so that a datatype ADDED to the vocabulary refuses
// here until a generator knows what Go and TypeScript shape it takes.
const (
	TypeString     = "string"
	TypeText       = "text"
	TypeMarkdown   = "markdown"
	TypeInt        = "int"
	TypeFloat      = "float"
	TypeBool       = "bool"
	TypeDatetime   = "datetime"
	TypeDate       = "date"
	TypeDuration   = "duration"
	TypeEmail      = "email"
	TypeURL        = "url"
	TypePhone      = "phone"
	TypeTimezone   = "timezone"
	TypeRecurrence = "recurrence"
	TypeEnum       = "enum"
	TypeJSON       = "json"
	TypeSecret     = "secret"
	TypeDigest     = "digest"
	TypeBlobRef    = "blobref"
	TypeState      = "state"
	TypeObject     = "object"
	TypeReference  = "reference"
)

var datatypes = keys(TypeString, TypeText, TypeMarkdown, TypeInt, TypeFloat,
	TypeBool, TypeDatetime, TypeDate, TypeDuration, TypeEmail, TypeURL,
	TypePhone, TypeTimezone, TypeRecurrence, TypeEnum, TypeJSON, TypeSecret,
	TypeDigest, TypeBlobRef, TypeState, TypeObject, TypeReference)

// fieldForbidden are the datatypes an object field may never be, at any level,
// mirroring vocabulary's fieldForbiddenKinds: each of them is a whole property.
var fieldForbidden = keys(TypeJSON, TypeSecret, TypeDigest, TypeState, TypeBlobRef)

func (r *reader) readProperty(where, name string, n *yaml.Node, depth int) *Property {
	d, ok := asMapping(n)
	if !ok {
		// A bare string is a field's shorthand for {type: <it>}.
		if s, isScalar := scalar(n); isScalar && depth > 1 {
			d = shorthand(s)
		} else {
			r.errf("%s: a property is a mapping", where)
			return nil
		}
	}
	p := &Property{Name: name, Depth: depth, Datatype: d.str("type")}
	p.Description, p.DisplayName = d.str("description"), d.str("displayName")
	if p.Datatype == "" {
		r.errf("%s: type is required", where)
		return nil
	}
	if !datatypes[p.Datatype] {
		r.errf("%s: datatype %q is not one this reader generates for — a refinement resolves through the registry, and a new built-in needs a Go and a TypeScript shape first", where, p.Datatype)
		return nil
	}
	p.Repeated, p.Keyed = r.flag(where, d, "repeated"), r.flag(where, d, "keyed")
	p.Managed = r.flag(where, d, "managed")
	if p.Keyed && p.Repeated {
		r.errf("%s: keyed and repeated are the two containers — a declaration is one or the other", where)
		return nil
	}
	if kp := d.str("keyPattern"); kp != "" {
		switch {
		case !p.Keyed:
			r.errf("%s: keyPattern is the contract on a KEYED map's keys", where)
		case !keyPatterns[kp]:
			r.errf("%s: keyPattern %q is not a declared contract", where, kp)
		default:
			p.KeyPattern = kp
		}
	}
	switch p.Datatype {
	case TypeState:
		r.checkKeys(where, d, machineKeys)
		if p.Repeated || p.Keyed {
			r.errf("%s: a state property holds one state, never a container of them", where)
			return nil
		}
		p.Machine = r.readMachine(where, d)
		if p.Machine == nil {
			return nil
		}
		return p
	case TypeObject:
		r.checkKeys(where, d, objectKeys)
		if depth == MaxFieldDepth {
			r.errf("%s: fields nest %d levels deep at most (a kind's own property is level 1)", where, MaxFieldDepth)
			return nil
		}
		// THE CLOSED EMPTY OBJECT: `fields:` declared and empty is a value with no
		// fields at all — every key refused. It is spelled apart from an ABSENT
		// `fields:`, which is the author forgetting to say what the object holds.
		declared := d.has("fields")
		fields, _ := asMapping(d.at("fields"))
		if !declared {
			r.errf("%s: an object declares the fields its values follow", where)
			return nil
		}
		if fields == nil || len(fields.keys) == 0 {
			p.Fields = []*Property{}
			return p
		}
		for _, fname := range fields.keys {
			f := r.readProperty(where+".fields."+fname, fname, fields.at(fname), depth+1)
			if f == nil {
				continue
			}
			if fieldForbidden[f.Datatype] {
				r.errf("%s.fields.%s: %s is its own property, never a field", where, fname, f.Datatype)
				continue
			}
			p.Fields = append(p.Fields, f)
		}
		return p
	case TypeReference:
		r.checkKeys(where, d, referenceKeys)
		// `kind:` is the PIN, and `to:` is the edge's word: a reference still
		// spelling it is refused by the key set above, which is this reader's
		// whole answer to a spelling it does not understand.
		p.To = d.str("kind")
		p.Required = r.flag(where, d, "required")
		p.RenamedFrom = d.str("renamedFrom")
		p.Inverse, p.InverseDescription = d.str("inverse"), d.str("inverseDescription")
		return p
	}
	r.checkKeys(where, d, scalarKeys)
	if d.at("fields") != nil {
		r.errf("%s: fields is only for `type: object`", where)
		return nil
	}
	if d.at("base") != nil {
		r.errf("%s: base declares a property TYPE, not a property", where)
		return nil
	}
	p.Required = r.flag(where, d, "required")
	p.RenamedFrom = d.str("renamedFrom")
	p.Pattern = d.str("pattern")
	p.Min, p.Max = r.number(where, d, "min"), r.number(where, d, "max")
	for i, v := range d.seq("values") {
		if ev, ok := r.readEnumValue(fmt.Sprintf("%s.values[%d]", where, i), v); ok {
			p.Values = append(p.Values, ev)
		}
	}
	if p.Datatype == TypeEnum && len(p.Values) == 0 {
		r.errf("%s: an enum declares its values", where)
		return nil
	}
	if w := d.str("writer"); w != "" {
		if !writerRoles[w] {
			r.errf("%s: writer %q is not a declared role", where, w)
		} else {
			p.Writer = w
		}
	}
	return p
}

func (r *reader) readMachine(where string, d *mapping) *Machine {
	m := &Machine{}
	for _, n := range d.seq("states") {
		s, _ := scalar(n)
		m.States = append(m.States, s)
	}
	if len(m.States) == 0 {
		r.errf("%s: a machine declares its states", where)
		return nil
	}
	m.Initial = d.str("initial")
	if m.Initial == "" {
		m.Initial = m.States[0]
	}
	for i, tn := range d.seq("transitions") {
		td, ok := asMapping(tn)
		if !ok {
			r.errf("%s.transitions[%d]: a transition is a mapping", where, i)
			continue
		}
		r.checkKeys(fmt.Sprintf("%s.transitions[%d]", where, i), td, transitionKeys)
		t := Transition{From: td.str("from"), To: td.str("to"), OnEnter: td.str("onEnter")}
		if stamps, _ := asMapping(td.at("stamps")); stamps != nil {
			names := append([]string(nil), stamps.keys...)
			sort.Strings(names)
			for _, prop := range names {
				t.Stamps = append(t.Stamps, Stamp{Property: prop, Value: stamps.str(prop)})
			}
		}
		m.Transitions = append(m.Transitions, t)
	}
	return m
}

func (r *reader) readEnumValue(where string, n *yaml.Node) (EnumValue, bool) {
	if s, ok := scalar(n); ok {
		return EnumValue{Value: s}, true
	}
	d, ok := asMapping(n)
	if !ok {
		r.errf("%s: an enum value is a scalar or {value, label}", where)
		return EnumValue{}, false
	}
	r.checkKeys(where, d, keys("value", "label"))
	v := EnumValue{Value: d.str("value"), Label: d.str("label")}
	if v.Value == "" {
		r.errf("%s: an enum value needs a value", where)
		return EnumValue{}, false
	}
	return v, true
}

func (r *reader) checkKeys(where string, d *mapping, allowed map[string]bool) {
	for _, k := range d.keys {
		if !allowed[k] {
			r.errf("%s: unknown key %q — this reader generates from the dialect it knows, and passing an unknown key would generate a type that means something else", where, k)
		}
	}
}

func (r *reader) flag(where string, d *mapping, key string) bool {
	n := d.at(key)
	if n == nil {
		return false
	}
	s, _ := scalar(n)
	b, err := strconv.ParseBool(s)
	if err != nil {
		r.errf("%s.%s: %q is not a bool", where, key, s)
		return false
	}
	return b
}

func (r *reader) number(where string, d *mapping, key string) *float64 {
	n := d.at(key)
	if n == nil {
		return nil
	}
	s, _ := scalar(n)
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		r.errf("%s.%s: %q is not a number", where, key, s)
		return nil
	}
	return &f
}

// --- yaml.Node in authored order ---

// mapping is one YAML mapping with its key ORDER kept: generated output follows
// the document's own shape, which a map[string]any cannot do.
type mapping struct {
	keys   []string
	values map[string]*yaml.Node
}

func asMapping(n *yaml.Node) (*mapping, bool) {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil, false
	}
	m := &mapping{values: map[string]*yaml.Node{}}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i].Value
		m.keys = append(m.keys, k)
		m.values[k] = n.Content[i+1]
	}
	return m, true
}

// shorthand is a field's bare-string form (`name: string`) as the mapping the
// rest of the reader expects.
func shorthand(datatype string) *mapping {
	return &mapping{
		keys:   []string{"type"},
		values: map[string]*yaml.Node{"type": {Kind: yaml.ScalarNode, Value: datatype}},
	}
}

func (m *mapping) at(key string) *yaml.Node { return m.values[key] }

// has reports whether the key was WRITTEN, whatever its value: `fields:` with
// nothing under it is a declaration of emptiness, and absent is not that.
func (m *mapping) has(key string) bool {
	_, ok := m.values[key]
	return ok
}

func (m *mapping) str(key string) string {
	s, _ := scalar(m.values[key])
	return s
}

func (m *mapping) seq(key string) []*yaml.Node {
	n := m.values[key]
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	return n.Content
}

func scalar(n *yaml.Node) (string, bool) {
	if n == nil || n.Kind != yaml.ScalarNode {
		return "", false
	}
	return n.Value, true
}

func keys(names ...string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out
}
