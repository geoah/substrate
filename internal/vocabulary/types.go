// Package schema loads the substrate's declarative vocabulary: streams of
// authority/type/metadata/data manifests — shipped files and connector payloads
// alike — become a validated registry of authorities and types that the engine
// validates every write against.
package vocabulary

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/substrate"
)

// Datatype is a built-in property datatype: what a VALUE is, never what a
// RECORD is. The word "kind" belongs to the declared kind of a record, and
// using it for both is what made this package read backwards.
type Datatype string

const (
	DatatypeString     Datatype = "string"
	DatatypeText       Datatype = "text"
	DatatypeMarkdown   Datatype = "markdown"
	DatatypeInt        Datatype = "int"
	DatatypeFloat      Datatype = "float"
	DatatypeBool       Datatype = "bool"
	DatatypeDatetime   Datatype = "datetime"
	DatatypeDate       Datatype = "date"
	DatatypeDuration   Datatype = "duration"
	DatatypeEmail      Datatype = "email"
	DatatypeURL        Datatype = "url"
	DatatypePhone      Datatype = "phone"
	DatatypeTimezone   Datatype = "timezone"
	DatatypeRecurrence Datatype = "recurrence"
	DatatypeEnum       Datatype = "enum"
	DatatypeJSON       Datatype = "json"
	// DatatypeSecret is confidential MATERIAL: the record and the changelog
	// store only an opaque ref into the engine's sealed store, where the
	// value itself lives encrypted. Reads redact even the ref; only the host
	// reads that spend the material ever resolve it. The credential's
	// passwordRef/totpRef and the accountconfig tokenRef are the same shape
	// with engine-minted refs.
	DatatypeSecret Datatype = "secret"
	// DatatypeDigest is a one-way hash the server minted to COMPARE, never to
	// reveal: redacted on the wire like a secret, but stored as the value
	// itself, because the engine matches it in SQL (a token authenticates by
	// digest containment on every request). The value is 64 lowercase hex
	// characters, a SHA-256, and only substrate paths write it.
	DatatypeDigest Datatype = "digest"
	// DatatypeBlobRef references a content-addressed blob by its digest (the blob
	// record's id). The stored value is the digest string; reads resolve it to
	// the blob's manifest ({digest, mimeType, size, status}), never the bytes
	// inline. Edges point at records; a blob-ref points at bytes.
	DatatypeBlobRef Datatype = "blobref"
	// DatatypeState is a state machine declared as a property: states, initial
	// and transitions live on the property, its current value is the record's
	// current state (MODEL §11.4). MODEL §11.5 once said edges were the ONE way
	// to point at another record; ticket 033 qualified that. An edge is still
	// the only TRAVERSABLE relationship, but a stored typed pointer is
	// DatatypeReference below.
	DatatypeState Datatype = "state"
	// DatatypeObject is an inline structured property: named fields declared
	// right on the property, each a scalar, a reference or another object, to
	// MaxFieldDepth levels. `json` survives only for payloads whose shape we do
	// not own; anything a path reads must be declared. Not in builtinKinds: it
	// is never a refinement base, never a capability property, and its parse
	// branch owns its own key set.
	DatatypeObject Datatype = "object"
	// DatatypeReference is a typed POINTER stored as a property value: the same
	// {authority, type, id} triple S1/A9 made canonical for an edge target
	//, but a stored value, not a graph edge. An EDGE is a
	// traversable relationship (incoming views, subject resolution); a
	// `reference` is data — a manifest field that names another type+record
	// (a trigger's `callable`). Its optional `to:` pins the referent type
	// like an edge's `to:` (`to: any`, or absent, leaves it unconstrained).
	// Validation checks shape + that the referent TYPE exists; the referent
	// RECORD is NOT required to exist at write time — a reference is a
	// pointer, like an edge target may be a bare id the target's own
	// admission resolves. Its own parse branch owns its key set. Not in
	// builtinKinds: never a refinement base and never an object field.
	DatatypeReference Datatype = "reference"
)

// ToAny is the `to:` value that leaves a reference (or, by the same word, an
// edge) unconstrained: any type is an admissible referent, and the value must
// then carry an explicit type. An absent `to:` on a reference reads the same.
const ToAny = "any"

var builtinKinds = map[Datatype]bool{
	DatatypeString: true, DatatypeText: true, DatatypeMarkdown: true, DatatypeInt: true,
	DatatypeFloat: true, DatatypeBool: true, DatatypeDatetime: true, DatatypeDate: true,
	DatatypeDuration: true, DatatypeEmail: true, DatatypeURL: true, DatatypePhone: true,
	DatatypeTimezone: true, DatatypeRecurrence: true, DatatypeEnum: true,
	DatatypeJSON: true, DatatypeSecret: true, DatatypeDigest: true,
	DatatypeState: true, DatatypeBlobRef: true,
}

// shortStringKinds are the string-family kinds that carry FTS band B and
// support eq/prefix/in filters.
var shortStringKinds = map[Datatype]bool{
	DatatypeString: true, DatatypeEmail: true, DatatypeURL: true, DatatypePhone: true,
	DatatypeTimezone: true, DatatypeRecurrence: true, DatatypeEnum: true,
}

// IsShortString reports whether k is a short string-family kind.
func IsShortString(k Datatype) bool { return shortStringKinds[k] }

// IsLongText reports whether k is a prose kind (FTS band C, snippet source).
func IsLongText(k Datatype) bool { return k == DatatypeText || k == DatatypeMarkdown }

// Property is one declared value slot on a type.
type Property struct {
	Name string
	// DisplayName is the OPTIONAL human label a client renders instead of the
	// raw camelCase property name (`backfillDepth` → "Backfill depth"). Absent
	// leaves the client to humanize the name itself, so it stays backward
	// compatible; a short label, no newlines, bounded like a description.
	DisplayName string
	// Description is the declared one-sentence explanation — the console's
	// hover tooltip. One short sentence, enforced at load; the manifest's
	// comments stay the long-form home.
	Description string
	Datatype    Datatype
	Refined     string // the declared type name when it is a custom refinement
	Repeated    bool   // declared `repeated: true`: a list of Datatype
	// Keyed is declared `keyed: true`, the twin of Repeated: the stored value is
	// a JSON OBJECT whose every value follows the rest of this declaration (the
	// declared fields for an object, the declared scalar otherwise). The KEYS are
	// data, so nothing declares them one by one — KeyPattern is the whole
	// contract they hold to. Keyed and Repeated are never both true: a map is
	// not a list.
	//
	// A keyed map whose values are themselves a keyed map is not declarable, and
	// that is deliberate: the value's shape IS this declaration, so there is no
	// second node to hang a second `keyed:` on. Two data-keyed levels in a row
	// would leave every reader guessing which level a path addressed.
	Keyed bool
	// KeyPattern is the declared contract a keyed map's keys hold to:
	// KeyPatternCamel (a property-name key), KeyPatternKindRef (a bare or
	// qualified kind reference), or empty for any non-empty key. It is
	// meaningless without Keyed and refused there.
	KeyPattern string
	// Values is an enum's ordered admissible set — each an opaque value paired
	// with an OPTIONAL human label. Declaration order is render
	// order. Validation reads Value alone (ValueStrings); the Label is purely
	// presentational, and an empty one leaves the client to humanize the value.
	// A non-enum property may still carry a Value-only set (a state property's
	// declared value list), which reads through ValueStrings the same way.
	Values  []EnumValue
	Pattern *regexp.Regexp
	Min     *float64
	Max     *float64
	Embed   bool
	FTS     bool
	// Required mirrors the declared `required:` hint. The engine does not
	// enforce it on writes (it stays a form-level hint the read surfaces
	// consume from Definition) — but ADDING it to a property is a narrowing
	// definition change, refused by admission while live rows lack the
	// property.
	Required bool
	// RenamedFrom is the RESERVED declared-evolution key (ticket 003, ruling
	// A3): the previous name of this property, admitted and stored so the
	// manifest dialect has room for a one-time rewrite, but NOT yet acted on —
	// no projection rewrites rows today, and admission still refuses the
	// rename while live rows carry the old name. Loader-validated: camelCase,
	// not the property's own name, not a name the type still declares, never
	// a reserved built-in.
	RenamedFrom string
	// Machine is the state machine a `type: state` property declares; nil
	// for every other kind.
	Machine *Machine
	// To is a `reference` property's optional referent-type constraint
	//, the twin of an edge's `to:`: a resolved full type
	// identity a value must name, ToAny ("any") for unconstrained, or empty
	// when no `to:` was declared (also unconstrained). Resolved from a bare
	// name to a full identity in Finalize, exactly like an edge target. Empty
	// for every non-reference kind.
	To string
	// Inverse names this relationship as the TARGET reads it — `thread` on a
	// message is `messages` on the thread — so a graph view standing on the
	// target can say what points at it in the model's own words instead of
	// reading back the pointer's name, which is written from the other side.
	//
	// IT IS A LABEL, NEVER AN IDENTIFIER. Nothing filters, routes, resolves or
	// looks anything up by it, and no two declarations are reconciled through
	// it. That is what makes a collision harmless: two authorities may both
	// call their side `messages`, and the result is two groups sharing a word,
	// each named by the kind it comes from. Only a collision INSIDE one
	// authority is refused, because there it is an author's own slip.
	//
	// InverseDescription is the one-liner that side carries, same rule as a
	// property's Description.
	Inverse            string
	InverseDescription string
	// Fields are an object's declared fields: scalar kinds, authority-local
	// refinements, references or further objects, each in its own container
	// (single, Repeated or Keyed), nesting to MaxFieldDepth. Nil for every other
	// kind. Objects and keyed maps stay out of FTS, embed and the filter grammar
	// at every level until a consumer arrives (§15).
	Fields     map[string]*Property
	FieldOrder []string
	// Implicit marks properties declared indirectly (machine stamps).
	Implicit bool
	// Managed marks a property the ENGINE stamps: `version`, `source`, the
	// quarantine fields, the bundle lifecycle bools. It is the declaration of
	// that fact, so a client can render the property read-only instead of
	// offering an input the write path will not honor. The write path READS this
	// flag: a declaration write carrying a managed property is refused unless the
	// value equals the one the row already holds, so an edit can never look
	// obeyed, and `get -o yaml | apply -f` still round-trips
	// (checkDeclarationWrite, in internal/engine). This is the only statement of
	// the stamped set; the engine keeps no hand-written list beside it.
	Managed bool
	// RefersTo marks a STRING property whose value names something in the
	// registry rather than being free text: a kind (`emit: [person]`), a
	// function, an agent, an authority or an llmprovider. It changes no stored
	// value and validates nothing — existence is settled at registry
	// finalization, which knows the whole closure — and exists so a client can
	// offer a picker from the declaration alone instead of a hand-kept map of
	// property name to picker. Refused on any other datatype: a reference-typed
	// pointer already carries its referent in `to:`.
	RefersTo string
	// Writer restricts WHICH actor role may write this property, enforced
	// server-side after the merged row is known. Empty is
	// unrestricted — the ordinary "nothing ranks writers" rule. The declared
	// roles are:
	//   - "oauth"     — only the host OAuth facility's actor (tokenRef,
	//     tokenStatus, grantedScopes: the facility's hands on an account).
	//   - "connector" — only installed bundle code (function.*/agent.*):
	//     the connector's own sync state (syncToken, lastSyncedAt, syncStatus).
	//   - "owner"     — only an owner-tier actor.
	// A host-written property a stranger could otherwise assign directly is the
	// vulnerability this closes: the console blacklist is not a boundary.
	Writer string
}

// Property writer roles: the admitted values of a
// property's `writer:` restriction.
const (
	WriterOAuth     = "oauth"
	WriterConnector = "connector"
	WriterOwner     = "owner"
)

// The admitted `keyPattern:` contracts of a keyed map's keys. Both reuse the
// validator the rest of the loader holds the same spelling to, so a key and a
// declared name (or a kind reference) can never drift apart.
const (
	KeyPatternCamel   = "camel"
	KeyPatternKindRef = "kindRef"
)

// The admitted `refersTo:` markers: what a string property's value NAMES.
// `provider` is an llmprovider record, which is data rather than vocabulary and
// is why it is not spelled with the others' registry sense.
const (
	RefersToKind      = "kind"
	RefersToFunction  = "function"
	RefersToAgent     = "agent"
	RefersToAuthority = "authority"
	RefersToProvider  = "provider"
)

// MaxFieldDepth is how deep declared fields nest: a kind's own property is
// level 1, so a level-MaxFieldDepth field holds a scalar and never an object.
// The bound is declared rather than discovered because every narrowing guard
// walks it (engine/schemadiff.go) — an unbounded dialect would need a general
// jsonb path walker in exactly the code a regression must not hide in.
const MaxFieldDepth = 4

// KeyPatternRegexp is the anchored regular expression a keyed map's keys match
// under the given contract, and "" when the contract admits every non-empty key.
// It exists so a SQL guard can count the keys a TIGHTENED contract would refuse
// instead of refusing every populated map: the pattern travels, the grammar
// stays here (naming.go).
func KeyPatternRegexp(pattern string) string {
	switch pattern {
	case KeyPatternCamel:
		return "^" + camelRE + "$"
	case KeyPatternKindRef:
		return "^" + kindRefRE + "$"
	}
	return ""
}

// keyPatternRE compiles what KeyPatternRegexp hands out, so the write path and
// the narrowing guard's SQL are not two spellings of one grammar but one
// spelling asked twice. Nothing else in the loader needs to be this literal;
// this contract does, because a key the write path admits and the guard refuses
// is a stored map that can never be brought under its own declaration.
var keyPatternRE = map[string]*regexp.Regexp{
	KeyPatternCamel:   regexp.MustCompile(KeyPatternRegexp(KeyPatternCamel)),
	KeyPatternKindRef: regexp.MustCompile(KeyPatternRegexp(KeyPatternKindRef)),
}

// keyPatternRule is what each contract says when it refuses, in the author's
// terms rather than the regexp's.
var keyPatternRule = map[string]string{
	KeyPatternCamel:   camelRule,
	KeyPatternKindRef: "a kind reference (`task` or `tasks.example.com/task`)",
}

// CheckKey holds one key of a keyed map to the declared contract. The keys are
// DATA: an undeclared key is admitted (that is what a map is for), and this is
// the whole check that stands between the writer and the stored value.
//
// It matches the shared PATTERN rather than calling the sibling validators, and
// for kindRef that is a deliberate narrowing of one: ValidKindReference splits on
// the first slash and validates the authority only when it is non-empty, so it
// admits "/task" — an empty authority, which is not a kind reference and which
// no reference the registry resolves has ever been spelled as. Fixing the
// validator itself would move a trigger source and every function allowlist that
// stored one, so the quirk stays where it is and the KEY contract holds to the
// grammar the guard counts against.
func (p *Property) CheckKey(key string) error {
	if key == "" {
		return fmt.Errorf("a keyed map's key is never empty")
	}
	re, contracted := keyPatternRE[p.KeyPattern]
	if !contracted {
		return nil
	}
	if !re.MatchString(key) {
		return fmt.Errorf("key %q must be %s", key, keyPatternRule[p.KeyPattern])
	}
	return nil
}

// EnumValue is one admissible value of an enum, paired with the OPTIONAL
// human label a client renders beside it (`last30d` → "Last 30 days"), so a
// select shows a name and submits the value. The label is purely
// presentational: validation is on Value alone, and an empty Label leaves the
// client to humanize Value itself.
type EnumValue struct {
	Value string
	Label string
}

// UnmarshalYAML admits BOTH declared forms, so a stored closure whose enum was
// written as bare strings still loads beside one written with labels:
//   - a bare scalar (`last30d`) parses to {Value: "last30d", Label: ""};
//   - a mapping (`{value: last30d, label: "Last 30 days"}`) parses to both.
//
// The substrate's own loader decodes manifests to map[string]any and walks the
// values list itself (see load.go's parseEnumValue), so this is the seam for
// any direct yaml.v3 decode into []EnumValue — the two agree on both forms.
func (e *EnumValue) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		e.Value, e.Label = node.Value, ""
		return nil
	}
	var m struct {
		Value string `yaml:"value"`
		Label string `yaml:"label"`
	}
	if err := node.Decode(&m); err != nil {
		return err
	}
	e.Value, e.Label = m.Value, m.Label
	return nil
}

// ValueStrings returns just the admissible values, in declared order — the set
// validation checks against, and what a non-enum value-set reads. Nil-safe.
func (p *Property) ValueStrings() []string {
	if len(p.Values) == 0 {
		return nil
	}
	out := make([]string, len(p.Values))
	for i, v := range p.Values {
		out[i] = v.Value
	}
	return out
}

// IsState reports whether the property is a state machine.
func (p *Property) IsState() bool { return p.Datatype == DatatypeState }

// Secret reports whether the property is confidential MATERIAL: the stored
// value is a ref into the sealed store and the material is scrubbed at the
// runner boundary. Every secret is also Sensitive.
func (p *Property) Secret() bool { return p.Datatype == DatatypeSecret }

// Sensitive reports whether reads must redact the property and search,
// filtering, ordering, templates and change payloads must exclude it. Wider
// than Secret: a digest is server-minted and reveals no material, but it has
// no business on any wire either.
func (p *Property) Sensitive() bool {
	return p.Datatype == DatatypeSecret || p.Datatype == DatatypeDigest
}

// Edge is one declared relationship on a type. Nothing here marks a subject
// edge: a recordmapping names it, so the edge declaration stays ordinary and
// a connector's types never need a vocabulary change (§6.1, record 50).
type Edge struct {
	Name string
	// Description is the declared one-sentence explanation, same rule as a
	// property's.
	Description string
	To          string // resolved type identity, or "any"
	Many        bool
	Required    bool
	OwnerRef    bool
	// Inverse names this relationship as the TARGET reads it: `thread` on a
	// message is `messages` on the thread. See Inverse on Property — the rule
	// is one rule, and it is written there.
	Inverse            string
	InverseDescription string
}

// Transition is one legal machine move. It carries no guard: anyone may
// perform any transition.
type Transition struct {
	From    string
	To      string
	Stamps  map[string]string
	OnEnter string
}

// Machine is a state property's machine — the entire behavioral seam. Its
// name IS the property's name; storage keeps states in their own column, the
// wire shows them in `properties` (MODEL §11.4).
type Machine struct {
	Name        string
	States      []string
	Initial     string // the one state a creation is born into
	Transitions []*Transition
}

// Transition finds the legal move from → to, if declared.
func (m *Machine) Transition(from, to string) *Transition {
	for _, t := range m.Transitions {
		if t.From == from && t.To == to {
			return t
		}
	}
	return nil
}

// HasState reports whether s is declared.
func (m *Machine) HasState(s string) bool {
	for _, x := range m.States {
		if x == s {
			return true
		}
	}
	return false
}

// Trait is a trait: a reusable set of typed properties with shared
// semantics (the wire kind is `trait` — record 63; the Go name predates it).
// Traits resolve in-authority first and then uniquely across authorities, so an
// app's authority can bind one the vocabulary declares.
type Trait struct {
	Name      string
	Authority string
	// Variants maps a one_of variant name to its properties.
	Variants map[string]map[string]Datatype
	// Properties is the non-variant form.
	Properties map[string]Datatype

	// Definition is the manifest's data map.
	Definition map[string]any
	// SourceYAML is the verbatim manifest, comments included; installed
	// authorities have no original text, so theirs is derived.
	SourceYAML string
}

// Identity is "<authority>/<name>".
func (c *Trait) Identity() string { return KindRef(c.Authority, c.Name) }

// PropertyType is a custom property type: a refinement of a built-in kind with a
// pattern, a range or an enumeration bolted on. Unlike a capability it is
// authority-local — a property type is only usable inside its own authority.
type PropertyType struct {
	Name      string
	Authority string
	Base      Datatype
	// Prop is the refinement as the property parser applies it.
	Prop       *Property
	Definition map[string]any
	SourceYAML string
}

// Identity is "<authority>/<name>".
func (d *PropertyType) Identity() string { return KindRef(d.Authority, d.Name) }

// TraitBinding is one type's use of a trait, with the optional
// hot-column remapping (`temporal(point: dueAt)`).
type TraitBinding struct {
	Trait string
	// Identity is the RESOLVED trait's full identity
	// ("core.substrate.reamde.dev/accountconfig"): the declaration names a bare trait,
	// resolution pins which one, and the binding keeps that answer. Host
	// behavior keys on it EXACTLY, so a bundle-local trait wearing a core
	// trait's bare name can never counterfeit the host-recognized interfaces.
	Identity string
	Variant  string
	// Columns maps the trait's property name to the declared hot
	// property (`at`, `endsAt`, `dueAt`) it occupies on this type.
	Columns map[string]string
}

// Kind is one declared kind of thing.
type Kind struct {
	Name      string
	Authority string
	Identity  string
	Plural    string
	Version   string
	Source    string // "builtin" | "installed"
	// Description is what this kind is for, in the author's own words: it
	// heads the kind's page in the console, so it says what the thing is and
	// what writes it. A property's description is a tooltip and holds to one
	// sentence; a kind's gets two (maxKindDescription), still on one line.
	Description string

	DisplayTemplate string
	Template        *Template

	Props     map[string]*Property
	PropOrder []string
	Edges     map[string]*Edge
	EdgeOrder []string
	// Machines indexes the state properties by name — the same machinery the
	// deleted `machines:` key used to fill (MODEL §11.4).
	Machines map[string]*Machine

	Traits  []TraitBinding
	Indices [][]string

	// HotColumns lists the hot properties this type's capabilities bind, in
	// {"at","endsAt","dueAt"} terms.
	HotColumns map[string]bool

	// Definition is the manifest's data map: what the GraphQL builder and the
	// console read, exactly as it was authored.
	Definition map[string]any

	// SourceYAML is the verbatim manifest this type was declared in — the
	// whole document, carrying the comments that say what the type is for.
	// Installed types have no original text: theirs is their manifest
	// marshaled back to YAML.
	SourceYAML string
}

// Prop returns the declared property, if any.
func (t *Kind) Prop(name string) (*Property, bool) {
	p, ok := t.Props[name]
	return p, ok
}

// Edge returns the declared edge, if any.
func (t *Kind) Edge(name string) (*Edge, bool) {
	e, ok := t.Edges[name]
	return e, ok
}

// UsesHot reports whether a capability binds the given hot property.
func (t *Kind) UsesHot(name string) bool { return t.HotColumns[name] }

// StateProp returns the machine a state property declares, if the name names
// one.
func (t *Kind) StateProp(name string) (*Machine, bool) {
	m, ok := t.Machines[name]
	return m, ok
}

// applyCapability records a resolved binding and the hot properties it claims.
func (t *Kind) applyCapability(b TraitBinding) {
	for _, existing := range t.Traits {
		if existing.Identity == b.Identity {
			return
		}
	}
	t.Traits = append(t.Traits, b)
	for _, col := range b.Columns {
		t.HotColumns[col] = true
	}
}

// OwnerRefEdges lists the declared edges whose target owns this record.
func (t *Kind) OwnerRefEdges() []*Edge {
	var out []*Edge
	for _, name := range t.EdgeOrder {
		if e := t.Edges[name]; e.OwnerRef {
			out = append(out, e)
		}
	}
	return out
}

// Interfaces lists the GraphQL-style interface names this type implements:
// one per bound trait, one per declared machine.
func (t *Kind) Interfaces() []string {
	var out []string
	for _, c := range t.Traits {
		out = append(out, traitInterface(c.Trait))
	}
	names := make([]string, 0, len(t.Machines))
	for n := range t.Machines {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		out = append(out, machineInterface(n))
	}
	return out
}

// Implements reports whether the type satisfies an interface selector: a
// FULL trait identity ("core.substrate.reamde.dev/accountconfig" — the resolved binding
// identity must match exactly), a bare trait name ("temporal"/"Temporal") or
// a machine name ("status"/"HasStatus"). Host checks pass full identities: a
// bare name only says what a binding was spelled as, never which trait it
// resolved to, so a bundle-local look-alike could counterfeit it.
func (t *Kind) Implements(iface string) bool {
	if Qualified(iface) {
		return t.implementsIdentity(iface)
	}
	want := strings.ToLower(iface)
	for _, c := range t.Traits {
		if strings.ToLower(c.Trait) == want || strings.ToLower(traitInterface(c.Trait)) == want {
			return true
		}
	}
	return t.implementsMachine(want)
}

// implementsIdentity reports whether a binding RESOLVED onto the given trait
// identity.
func (t *Kind) implementsIdentity(ident string) bool {
	for _, c := range t.Traits {
		if strings.EqualFold(c.Identity, ident) {
			return true
		}
	}
	return false
}

// implementsMachine reports whether the lowercased selector names a declared
// machine ("status") or its interface form ("hasstatus").
func (t *Kind) implementsMachine(want string) bool {
	for n := range t.Machines {
		if strings.ToLower(n) == want || strings.ToLower(machineInterface(n)) == want {
			return true
		}
	}
	return false
}

func traitInterface(name string) string   { return upperFirst(name) }
func machineInterface(name string) string { return "Has" + upperFirst(name) }

// upperFirst renders a declared camelCase name as its interface name; the
// declaration is already camelCase, so only the initial moves.
func upperFirst(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// Authority is one loaded schema authority: every manifest that named it, wherever
// those documents were declared.
type Authority struct {
	Name    string
	Version string
	Source  string
	Actors  []string
	// ActorTiers is each declared actor's manager tier — an explicit
	// attribute of the actor document (`tier: owner|bundle|machine`,
	// machine by default), never derived from the actor's spelling.
	ActorTiers    map[string]substrate.Tier
	PropertyTypes map[string]*PropertyType
	DatatypeOrder []string
	Traits        map[string]*Trait
	TraitOrder    []string
	Kinds         map[string]*Kind
	KindOrder     []string
	Mappings      map[string]*Mapping
	MappingOrder  []string
	Functions     map[string]*Function
	FunctionOrder []string
	Agents        map[string]*Agent
	AgentOrder    []string
	// Bundle is the authority's bundle document, set only on owned bundle authorities
	// ("<name>.bundles.substrate.reamde.dev" — bundle.go).
	Bundle *Bundle

	// SourceYAML is the authority's own authority manifest, verbatim.
	SourceYAML string

	// pending holds trait property contracts checked once every type
	// in the authority is parsed.
	pending []pendingCapProp
	// pendingTraits holds trait bindings whose trait is not declared
	// in this authority; they resolve against the registry in Finalize/Install.
	pendingTraits []pendingCapBinding
}

// Registry holds every loaded authority: the shipped files plus whatever
// connectors installed. Safe for concurrent use; Version bumps on install.
type Registry struct {
	mu          sync.RWMutex
	authorities map[string]*Authority
	order       []string
	byIdent     map[string]*Kind
	byName      map[string][]*Kind
	byPlural    map[string]*Kind // "authority/plural"
	version     int64
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		authorities: map[string]*Authority{},
		byIdent:     map[string]*Kind{},
		byName:      map[string][]*Kind{},
		byPlural:    map[string]*Kind{},
	}
}

// Clone returns an independent registry sharing the (immutable) loaded
// authorities, so a repository can install authorities without touching its siblings.
func (r *Registry) Clone() *Registry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c := NewRegistry()
	for _, name := range r.order {
		c.authorities[name] = r.authorities[name]
		c.order = append(c.order, name)
	}
	for k, v := range r.byIdent {
		c.byIdent[k] = v
	}
	for k, v := range r.byName {
		c.byName[k] = append([]*Kind(nil), v...)
	}
	for k, v := range r.byPlural {
		c.byPlural[k] = v
	}
	c.version = r.version
	return c
}

// Version is the counter the GraphQL layer caches its schema against.
func (r *Registry) Version() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.version
}

// Authorities lists loaded authority names in load order.
func (r *Registry) Authorities() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.order...)
}

// AuthorityByName returns a loaded authority.
func (r *Registry) AuthorityByName(name string) (*Authority, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	g, ok := r.authorities[name]
	return g, ok
}

// AuthorityList returns every loaded authority, ordered by name.
func (r *Registry) AuthorityList() []*Authority {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Authority, 0, len(r.authorities))
	for _, n := range r.order {
		out = append(out, r.authorities[n])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Traits lists every declared trait across authorities, ordered by
// identity.
func (r *Registry) Traits() []*Trait {
	var out []*Trait
	for _, g := range r.AuthorityList() {
		for _, n := range g.TraitOrder {
			out = append(out, g.Traits[n])
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Identity() < out[j].Identity() })
	return out
}

// PropertyTypes lists every declared custom property type across authorities, ordered
// by identity.
func (r *Registry) PropertyTypes() []*PropertyType {
	var out []*PropertyType
	for _, g := range r.AuthorityList() {
		for _, n := range g.DatatypeOrder {
			out = append(out, g.PropertyTypes[n])
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Identity() < out[j].Identity() })
	return out
}

// ResolveTrait finds a trait by bare name, in authority first and then
// uniquely across authorities — the same rule short edge targets follow.
func (r *Registry) ResolveTrait(authority, name string) (*Trait, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if g, ok := r.authorities[authority]; ok {
		if c, ok := g.Traits[name]; ok {
			return c, nil
		}
	}
	var found []*Trait
	for _, n := range r.order {
		if c, ok := r.authorities[n].Traits[name]; ok {
			found = append(found, c)
		}
	}
	switch len(found) {
	case 0:
		return nil, fmt.Errorf("unknown trait %q", name)
	case 1:
		return found[0], nil
	default:
		names := make([]string, 0, len(found))
		for _, c := range found {
			names = append(names, c.Identity())
		}
		sort.Strings(names)
		return nil, fmt.Errorf("ambiguous trait %q: %s", name, strings.Join(names, ", "))
	}
}

// Kinds lists every declared type, ordered by identity.
func (r *Registry) Kinds() []*Kind {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Kind, 0, len(r.byIdent))
	for _, t := range r.byIdent {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Identity < out[j].Identity })
	return out
}

// ByIdentity looks a type up by "<authority>/<name>".
func (r *Registry) ByIdentity(identity string) (*Kind, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.byIdent[identity]
	return t, ok
}

// ByPlural resolves a REST collection segment inside an authority.
func (r *Registry) ByPlural(authority, plural string) (*Kind, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.byPlural[authority+"/"+plural]
	return t, ok
}

// Resolve accepts a full identity or a bare type name unique across authorities.
func (r *Registry) Resolve(nameOrIdentity string) (*Kind, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if t, ok := r.byIdent[nameOrIdentity]; ok {
		return t, nil
	}
	cands := r.byName[nameOrIdentity]
	switch len(cands) {
	case 0:
		return nil, fmt.Errorf("unknown type %q", nameOrIdentity)
	case 1:
		return cands[0], nil
	default:
		names := make([]string, 0, len(cands))
		for _, c := range cands {
			names = append(names, c.Identity)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("ambiguous type %q: %s", nameOrIdentity, strings.Join(names, ", "))
	}
}

// Implementing lists every type satisfying an interface selector, across
// authorities. An ambiguous bare trait name matches nothing; ImplementingStrict is
// the same read with the ambiguity reported.
func (r *Registry) Implementing(iface string) []*Kind {
	out, _ := r.ImplementingStrict(iface)
	return out
}

// ImplementingStrict lists every type satisfying an interface selector. A
// full trait identity matches resolved bindings exactly; a BARE trait name
// resolves only when it names a single declared trait across authorities — an
// ambiguous bare filter errors instead of aggregating same-named traits from
// different authorities into one answer. Machine selectors ("status"/"HasStatus")
// stay bare by nature.
func (r *Registry) ImplementingStrict(iface string) ([]*Kind, error) {
	if Qualified(iface) {
		var out []*Kind
		for _, t := range r.Kinds() {
			if t.implementsIdentity(iface) {
				out = append(out, t)
			}
		}
		return out, nil
	}
	want := strings.ToLower(iface)
	idents := map[string]bool{}
	for _, c := range r.Traits() {
		if strings.ToLower(c.Name) == want || strings.ToLower(traitInterface(c.Name)) == want {
			idents[c.Identity()] = true
		}
	}
	if len(idents) > 1 {
		names := make([]string, 0, len(idents))
		for id := range idents {
			names = append(names, id)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("ambiguous trait %q: %s — filter by a full identity", iface, strings.Join(names, ", "))
	}
	var traitIdent string
	for id := range idents {
		traitIdent = id
	}
	var out []*Kind
	for _, t := range r.Kinds() {
		if (traitIdent != "" && t.implementsIdentity(traitIdent)) || t.implementsMachine(want) {
			out = append(out, t)
		}
	}
	return out, nil
}

// Actors lists every declared actor across authorities.
func (r *Registry) Actors() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := map[string]bool{}
	var out []string
	for _, name := range r.order {
		for _, a := range r.authorities[name].Actors {
			if !seen[a] {
				seen[a] = true
				out = append(out, a)
			}
		}
	}
	sort.Strings(out)
	return out
}

// ActorAuthority returns the authority that declared an actor.
func (r *Registry) ActorAuthority(actor string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, name := range r.order {
		for _, a := range r.authorities[name].Actors {
			if a == actor {
				return name, true
			}
		}
	}
	return "", false
}

// ActorTier resolves an actor's manager tier from registry DATA:
// a declared actor document's explicit tier attribute (machine by default),
// and the bundle tier for every registered function's and agent's own
// actor. Nothing here reads the actor's spelling — an unknown actor is simply
// not the registry's to answer.
func (r *Registry) ActorTier(actor string) (substrate.Tier, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, name := range r.order {
		g := r.authorities[name]
		for _, a := range g.Actors {
			if a != actor {
				continue
			}
			if tier, ok := g.ActorTiers[a]; ok {
				return tier, true
			}
			return substrate.TierMachine, true
		}
		for _, fn := range g.Functions {
			if fn.Actor() == actor {
				return substrate.TierBundle, true
			}
		}
		for _, ag := range g.Agents {
			if ag.Actor() == actor {
				return substrate.TierBundle, true
			}
		}
	}
	return "", false
}
