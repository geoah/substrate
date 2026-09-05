// Package vocabulary loads the substrate's declarative vocabulary: streams of
// kind/metadata/data manifests — shipped files and installed payloads alike —
// become a validated registry of PACKAGES and the kinds they declare, which the
// engine validates every write against.
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
	DatatypeString   Datatype = "string"
	DatatypeText     Datatype = "text"
	DatatypeMarkdown Datatype = "markdown"
	DatatypeInt      Datatype = "int"
	DatatypeFloat    Datatype = "float"
	// DatatypeDecimal is an EXACT decimal number, carried as a string
	// ("19.99") on every wire and stored as a canonical decimal string. Money
	// is the reason it exists: every JSON door in and out of storage rides
	// float64, so a bare number may arrive already rounded, and the engine
	// refuses one rather than store the rounding. Filters and ordering compare
	// it numerically (::numeric), not as text.
	DatatypeDecimal    Datatype = "decimal"
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
	// the blob's manifest ({digest, mediaType, size, status}), never the bytes
	// inline. A reference points at a record; a blob-ref points at bytes.
	DatatypeBlobRef Datatype = "blobref"
	// DatatypeState is a state machine declared as a property: states, initial
	// and transitions live on the property, its current value is the record's
	// current state (MODEL §11.4).
	DatatypeState Datatype = "state"
	// DatatypeObject is an inline structured property: named fields declared
	// right on the property, each a scalar, a reference or another object, to
	// MaxFieldDepth levels. `json` survives only for payloads whose shape we do
	// not own; anything a path reads must be declared. Not in builtinKinds: it
	// is never a refinement base, never a capability property, and its parse
	// branch owns its own key set.
	DatatypeObject Datatype = "object"
	// DatatypeReference is the one way a record points at another record: a
	// stored value naming the referent's record PATH ("<kind>/<id>"), flat when
	// the declaration carries no link `properties:` and `{ref: "<kind>/<id>",
	// ...}` when it does. Its optional `kind:` PIN says which kind's records it
	// names (`kind: any`, or absent, leaves it unconstrained), which is what
	// lets a client offer a picker and what supplies the kind an authored bare
	// id omits — an id with no slash in it, because a slash-bearing one is
	// written in full path form or refused (SplitRecordPath says why).
	// Validation checks shape + that the referent KIND exists; the referent
	// RECORD has to exist only under `mustExist:`. Its own parse branch owns
	// its key set. Not in builtinKinds: never a refinement base and never an
	// object field.
	DatatypeReference Datatype = "reference"
)

// ToAny is the unconstrained pin: any kind is an admissible referent, and the
// value must then be a FULL path, since there is no pin to supply the kind a
// bare id leaves out. It is spelled `kind: any`, and an absent pin reads the
// same.
const ToAny = "any"

// OnDeleteCascade is the one declared `onDelete:` behavior: the referent OWNS
// this record, so collecting the referent collects this record too. An absent
// `onDelete:` detaches — the value stays and dangles once the referent is
// purged.
const OnDeleteCascade = "cascade"

var builtinKinds = map[Datatype]bool{
	DatatypeString: true, DatatypeText: true, DatatypeMarkdown: true, DatatypeInt: true,
	DatatypeFloat: true, DatatypeDecimal: true, DatatypeBool: true, DatatypeDatetime: true, DatatypeDate: true,
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
	// Required is the declared `required:`: the record must hold a value for
	// this property AFTER every write, which the engine enforces on the merged
	// row (checkRequiredProps, in internal/engine). A Default is what keeps a
	// required property writable without naming it. ADDING `required` to a
	// property is a narrowing definition change, refused by admission while
	// live rows lack the property, and a Default does not backfill.
	Required bool
	// Default is the declared `default:`, the value a CREATE stores when it
	// does not name the property. It is materialized at write time, into the
	// stored value and the changelog delta both: a default applied on read
	// would be derived data, and the fold would no longer be the truth.
	//
	// The value is the author's literal, held to this property's own
	// declaration at admission (checkDeclaredDefaults, in internal/engine) so a
	// kind whose default no write could store is refused rather than stored. It
	// is declarable on a single scalar property alone: not on a list or a keyed
	// map, not on an object, a reference, a state or a secret.
	Default any
	// Unique is the RESERVED single-value uniqueness marker: at most one live
	// record of the kind may carry any given value. Admitted, validated and
	// stored, but NOT ENFORCED: no index exists and no write is refused for a
	// duplicate today. It is reserved so the day enforcement arrives (a
	// narrowing-style count of violating rows plus a partial unique index beside
	// ensureIndices) the key is already in the dialect every binary reads.
	// Loader-validated: a single value only (never a list or a map), never on a
	// datatype whose values do not compare (uniqueForbiddenKinds), and never on
	// an object's field, where the constraint would name a position inside a
	// value rather than a property.
	Unique bool
	// Deprecated is the RESERVED add-and-deprecate marker: this declaration
	// still validates and still stores, and a client should stop offering it:
	// a picker greys it out, a form drops it below the live ones, a tool card
	// leaves it out of what it suggests writing. Admitted and stored; nothing
	// server-side reads it, so a write of a deprecated property is an ordinary
	// write. A deprecated property may not also be `required:`, which would tell
	// a form to stop offering a value it refuses to submit without.
	Deprecated bool
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
	// MustExist requires the referent RECORD to exist when the value is written
	// (a tombstoned row counts as existing); a value naming no record is
	// refused with the same not-found any addressed read gives. Absent, the
	// default, leaves a reference a plain pointer, which is what lets a write
	// name a record a later sync creates. It needs no pin: the stored value
	// carries the kind.
	MustExist bool
	// OnDelete says what becomes of THIS record when the referent dies:
	// OnDeleteCascade collects it in the same sweep (internal/engine/gc.go),
	// empty detaches. Declarable on a kind's OWN single-valued reference only —
	// a list or a map names no single owner, and a pointer inside an object is
	// not a kind's own property — so the loader refuses the other shapes rather
	// than leaving a declaration the sweep would silently never reach. No pin
	// is required: the refs index finds the sources naming a referent whether
	// or not the declaration pins a kind.
	OnDelete string
	// Properties is the LINK DATA declared beside the pointer
	// (`properties:` on a reference): the value becomes an object, `{ref:
	// "<kind>/<id>", <name>: <value>}`, and this map is the WHOLE admission —
	// a write naming anything it does not hold is refused. PropertyOrder holds
	// the sorted names. Legal on a single or repeated reference, never on a
	// keyed one and never inside an object.
	//
	// Each entry is a FLAT SINGLE VALUE: one scalar, enum or refinement, never
	// a list, a map, an object, a machine or a pointer
	// (linkPropForbiddenKinds). That is what keeps the block declarable in
	// core's own `kind` declaration, where the recursion a property block
	// admits could not be typed; anything that needs more than that is a
	// record with a reference at each end.
	Properties    map[string]*Property
	PropertyOrder []string
	// Subject marks the reference a recordmapping's source record points at:
	// the one property whose referent the source record describes (mapping.go).
	// The mapping document names the property and this marker is what the write
	// path reads, so a generic put or patch can refuse to move a subject
	// without consulting the mapping set. Only merge, split and the creating
	// write set it.
	Subject bool
	// To is a `reference` property's optional referent-kind constraint: a
	// resolved full type identity a value must name, ToAny ("any") for
	// unconstrained, or empty when no `kind:` was declared (also
	// unconstrained). Resolved from a bare name to a full identity in
	// Finalize. Empty for every non-reference kind.
	To string
	// ToTrait is a `reference` property's optional TRAIT pin: a resolved full
	// trait identity ("substrate.reamde.dev/core/accountconfig") the referent
	// KIND must implement, the twin of `To`'s kind pin. A reference pins EITHER
	// `kind:` (To) or `trait:` (ToTrait), never both. It is what lets a
	// provider-agnostic kind own its account without naming one provider's
	// account kind: any record whose kind implements the trait is an admissible
	// referent, and the set of kinds implementing a trait is finite (the
	// registry's Implementing).
	// Resolved from a bare name to a full identity in Finalize. Empty for every
	// non-reference kind and for a kind-pinned reference. A trait pin supplies no
	// kind for a bare id, so its value is a full "<kind>/<id>" path, like `any`.
	ToTrait string
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
	// Implicit marks a synthesized property: a machine-stamp target the
	// document does not declare. The loader keeps synthesizing these so
	// declarations stored before targets were declarable keep parsing; the
	// shipped tree declares every target.
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
	KeyPatternKindRef: "a kind reference (`task` or `example.com/tasks/task`)",
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
	// Deprecated is the RESERVED add-and-deprecate marker on one admissible
	// value: still admitted on writes, still held by every record that carries
	// it, and no longer offered by a picker. Removing a value live records hold
	// is the narrowing this exists to avoid.
	Deprecated bool
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
		Value      string `yaml:"value"`
		Label      string `yaml:"label"`
		Deprecated bool   `yaml:"deprecated"`
	}
	if err := node.Decode(&m); err != nil {
		return err
	}
	e.Value, e.Label, e.Deprecated = m.Value, m.Label, m.Deprecated
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

// Cascades reports whether the referent of this reference owns the record that
// declares it: collecting the referent collects this record too.
func (p *Property) Cascades() bool { return p.OnDelete == OnDeleteCascade }

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

// Transition is one legal machine move. It carries no guard: anyone may
// perform any transition.
type Transition struct {
	From    string
	To      string
	Stamps  map[string]string
	OnEnter string
	// Notifies names the reference property (pinned to core's llmthread)
	// whose thread this transition reports into: the engine writes the
	// resolution's `system` message there and schedules the resume, the one
	// primitive under proposal decisions and interaction answers alike
	// (docs/plans/thread-interactions.md). Empty for the ordinary transition
	// that reports nowhere.
	Notifies string
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
	Name string
	// Package is the identity of the package that declares the trait.
	Package string
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

// Identity is "<authority>/<package>/<name>".
func (c *Trait) Identity() string { return c.Package + "/" + c.Name }

// PropertyType is a custom property type: a refinement of a built-in kind with a
// pattern, a range or an enumeration bolted on. Unlike a capability it is
// package-local — a property type is only usable inside its own package.
type PropertyType struct {
	Name string
	// Package is the identity of the package that declares the property type.
	Package string
	Base    Datatype
	// Prop is the refinement as the property parser applies it.
	Prop       *Property
	Definition map[string]any
	SourceYAML string
}

// Identity is "<authority>/<package>/<name>".
func (d *PropertyType) Identity() string { return d.Package + "/" + d.Name }

// TraitBinding is one type's use of a trait, with the optional
// hot-column remapping (`temporal(point: dueAt)`).
type TraitBinding struct {
	Trait string
	// Identity is the RESOLVED trait's full identity
	// ("substrate.reamde.dev/core/accountconfig"): the declaration names a bare trait,
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
	Name string
	// Package is the identity of the package that declares the kind — the
	// group it is versioned, owned and quarantined in.
	Package  string
	Identity string
	Version  int64
	Source   string // "builtin" | "installed"
	// Description is what this kind is for, in the author's own words: it
	// heads the kind's page in the console, so it says what the thing is and
	// what writes it. A property's description is a tooltip and holds to one
	// sentence; a kind's gets two (maxKindDescription), still on one line.
	Description string

	DisplayTemplate string
	Template        *Template

	Props     map[string]*Property
	PropOrder []string
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
// FULL trait identity ("substrate.reamde.dev/core/accountconfig" — the resolved binding
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

// Package is one loaded closure: every declaration that named this package,
// wherever those documents were declared. The package is the unit a
// declaration is versioned, owned and quarantined in (decision 0047).
//
// An AUTHORITY document builds one of these too, with no members: an authority
// says what it is and owns the packages published under it. Identity tells the
// two apart: a package's carries the one slash, an authority's does not.
type Package struct {
	// Identity is the group key: "<authority>/<package>", or the bare
	// authority for an authority row.
	Identity string
	// Authority is the DNS-style name the package publishes under.
	Authority string
	// Name is the package's own word ("core", "google", "tasks"), empty on an
	// authority row.
	Name    string
	Version int64
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
	// Bundle is the package's bundle document, set only on the packages a
	// bundle is named for (bundle.go).
	Bundle *Bundle
	// Description is what the package or the authority is for, in the
	// author's own words.
	Description string

	// SourceYAML is the package's own header manifest, verbatim.
	SourceYAML string

	// pending holds trait property contracts checked once every kind
	// in the package is parsed.
	pending []pendingCapProp
	// pendingTraits holds trait bindings whose trait is not declared
	// in this package; they resolve against the registry in Finalize/Install.
	pendingTraits []pendingCapBinding
}

// IsAuthority reports whether this group is an AUTHORITY row rather than a
// package: it declares no members and its identity is the authority itself.
func (p *Package) IsAuthority() bool { return p.Name == "" }

// Registry holds every loaded package: the shipped files plus whatever a
// repository installed. Safe for concurrent use; Version bumps on install.
type Registry struct {
	mu       sync.RWMutex
	packages map[string]*Package
	order    []string
	byIdent  map[string]*Kind
	byName   map[string][]*Kind
	version  int64
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		packages: map[string]*Package{},
		byIdent:  map[string]*Kind{},
		byName:   map[string][]*Kind{},
	}
}

// Clone returns an independent registry sharing the (immutable) loaded
// authorities, so a repository can install authorities without touching its siblings.
func (r *Registry) Clone() *Registry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c := NewRegistry()
	for _, name := range r.order {
		c.packages[name] = r.packages[name]
		c.order = append(c.order, name)
	}
	for k, v := range r.byIdent {
		c.byIdent[k] = v
	}
	for k, v := range r.byName {
		c.byName[k] = append([]*Kind(nil), v...)
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

// Packages lists loaded group identities in load order — every package, and
// every authority row beside them.
func (r *Registry) Packages() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.order...)
}

// PackageByName returns a loaded group by its identity.
func (r *Registry) PackageByName(identity string) (*Package, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	g, ok := r.packages[identity]
	return g, ok
}

// PackageList returns every loaded group, ordered by identity.
func (r *Registry) PackageList() []*Package {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Package, 0, len(r.packages))
	for _, n := range r.order {
		out = append(out, r.packages[n])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Identity < out[j].Identity })
	return out
}

// Authorities lists the authorities the loaded packages publish under, sorted.
func (r *Registry) Authorities() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := map[string]bool{}
	var out []string
	for _, n := range r.order {
		a := r.packages[n].Authority
		if a == "" || seen[a] {
			continue
		}
		seen[a] = true
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// PackagesOf lists the package identities published under one authority,
// sorted. An authority row is not one of them.
func (r *Registry) PackagesOf(authority string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []string
	for _, n := range r.order {
		g := r.packages[n]
		if g.Authority == authority && !g.IsAuthority() {
			out = append(out, g.Identity)
		}
	}
	sort.Strings(out)
	return out
}

// Traits lists every declared trait across packages, ordered by
// identity.
func (r *Registry) Traits() []*Trait {
	var out []*Trait
	for _, g := range r.PackageList() {
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
	for _, g := range r.PackageList() {
		for _, n := range g.DatatypeOrder {
			out = append(out, g.PropertyTypes[n])
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Identity() < out[j].Identity() })
	return out
}

// ResolveTrait finds a trait by bare name, in the declaring package first and
// then uniquely across packages — the same rule a short `kind:` pin follows.
func (r *Registry) ResolveTrait(pkg, name string) (*Trait, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if g, ok := r.packages[pkg]; ok {
		if c, ok := g.Traits[name]; ok {
			return c, nil
		}
	}
	var found []*Trait
	for _, n := range r.order {
		if c, ok := r.packages[n].Traits[name]; ok {
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

// ByIdentity looks a kind up by "<authority>/<package>/<name>".
func (r *Registry) ByIdentity(identity string) (*Kind, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.byIdent[identity]
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
		for _, a := range r.packages[name].Actors {
			if !seen[a] {
				seen[a] = true
				out = append(out, a)
			}
		}
	}
	sort.Strings(out)
	return out
}

// ActorPackage returns the authority that declared an actor.
func (r *Registry) ActorPackage(actor string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, name := range r.order {
		for _, a := range r.packages[name].Actors {
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
		g := r.packages[name]
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
