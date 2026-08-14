package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/geoah/substrate/internal/kinddialect"
)

// A plan is what BOTH emitters read: the Go and the TypeScript view of a
// declaration have to agree about which types exist, what they are called and
// which fields are optional, and the one way to guarantee that is for the two
// emitters to share the decisions instead of each deriving them.
type plan struct {
	Authority string
	Kinds     []*kindPlan
}

type kindPlan struct {
	Decl *kinddialect.Kind
	// Name is the Go type name, which is also the TypeScript interface name.
	Name string
	// Structs are the kind's own struct first, then one per object property or
	// object field, depth first in declaration order.
	Structs []*structPlan
	// States are the machines the kind declares. A state is NOT stored in the
	// properties map — it lives in the record's own state column — so it is
	// planned apart from the properties even though it decodes with them.
	States []*statePlan
}

type structPlan struct {
	Name string
	// Doc is the declaration's own description, as the type's doc comment.
	Doc string
	// Owner is the kind reference every struct of a kind belongs to, root and
	// nested alike: a refusal names the declaration to go and read, and the path
	// says where inside it.
	Owner string
	// Root marks the struct that IS the kind's properties; the rest are its
	// object fields.
	Root bool
	// Path is the authored path of the object this struct is a value of
	// (`pricing`, `properties.fields`), empty for the root.
	Path   string
	Fields []*fieldPlan
	// Enums are the enum types this struct's fields declare.
	Enums []*enumPlan
}

// class is the code-generation family of a declared datatype: the datatypes
// that generate the same shape and the same decoder call share one.
type class int

const (
	classText class = iota
	classEnum
	classState
	classDatetime
	classInt
	classFloat
	classBool
	classSecret
	classDigest
	classBlobRef
	classJSON
	classReference
	classObject
)

type fieldPlan struct {
	Decl *kinddialect.Property
	// Key is the authored property (or field) name: the map key.
	Key string
	// Name is the Go field name, which is the key with its initial uppercased.
	// The declared spelling already carries its initialisms (`baseURL`), so
	// nothing here re-spells a name and no name is invented.
	Name  string
	Class class
	// GoElem and TSElem are the ELEMENT type: the container (repeated, keyed) is
	// applied by the emitters, which spell it differently.
	GoElem string
	TSElem string
	// Optional is the absence of `required: true`. An optional non-repeated,
	// non-keyed field generates as a POINTER in Go and with `?` in TypeScript:
	// absence stored is absence authored, and a materialized zero value is a
	// silent round trip bug.
	Optional bool
	// Nested is the struct an object field's element is.
	Nested *structPlan
	// Enum is the enum type an enum field's element is.
	Enum *enumPlan
	// State is the machine a state property is.
	State *statePlan
}

func (f *fieldPlan) repeated() bool { return f.Decl.Repeated }
func (f *fieldPlan) keyed() bool    { return f.Decl.Keyed }

// pointer reports whether the Go field is a pointer: an optional single value,
// except a `json` one, whose Dynamic already carries absence as nil.
func (f *fieldPlan) pointer() bool {
	return f.Optional && !f.repeated() && !f.keyed() && f.Class != classJSON
}

type enumPlan struct {
	Name   string
	Doc    string
	Values []kinddialect.EnumValue
}

type statePlan struct {
	Name    string
	Doc     string
	Machine *kinddialect.Machine
	// Property is the declared property name the machine is.
	Property string
}

// goNames spells each core kind's Go (and TypeScript) type name. It is a table,
// not a derivation: the loader holds a kind's name to ONE lowercase word
// (`llmprovider`, `recordpatchrequest`), and no mechanical split of one word can
// know where its words are, or that LLM is an initialism. A kind missing from
// here fails the generation instead of inventing a spelling nobody chose.
var goNames = map[string]string{
	"actor":              "Actor",
	"agent":              "Agent",
	"authority":          "Authority",
	"blob":               "Blob",
	"bundle":             "Bundle",
	"credential":         "Credential",
	"function":           "Function",
	"kind":               "Kind",
	"llmmessage":         "LLMMessage",
	"llmprovider":        "LLMProvider",
	"llmthread":          "LLMThread",
	"propertytype":       "PropertyType",
	"recordmapping":      "RecordMapping",
	"recordmerge":        "RecordMerge",
	"recordmergerequest": "RecordMergeRequest",
	"recordpatchrequest": "RecordPatchRequest",
	"recordsplit":        "RecordSplit",
	"recoverykey":        "RecoveryKey",
	"repository":         "Repository",
	"run":                "Run",
	"token":              "Token",
	"trait":              "Trait",
	"trigger":            "Trigger",
}

func buildPlan(authority string, decls []*kinddialect.Kind) (*plan, error) {
	p := &plan{Authority: authority}
	taken := map[string]string{} // generated type name -> where it came from
	for _, decl := range decls {
		name, ok := goNames[decl.Name]
		if !ok {
			return nil, fmt.Errorf("%s: kind %q has no Go spelling: add it to goNames in cmd/kindsgen/plan.go",
				decl.File, decl.Name)
		}
		k := &kindPlan{Decl: decl, Name: name}
		root := &structPlan{Name: name, Doc: decl.Description, Owner: decl.Ref, Root: true}
		k.Structs = append(k.Structs, root)
		if err := planFields(k, root, decl.Props); err != nil {
			return nil, fmt.Errorf("%s: %w", decl.File, err)
		}
		for _, s := range k.Structs {
			if err := claim(taken, s.Name, decl.Ref); err != nil {
				return nil, err
			}
			for _, e := range s.Enums {
				if err := claim(taken, e.Name, decl.Ref); err != nil {
					return nil, err
				}
			}
		}
		for _, s := range k.States {
			if err := claim(taken, s.Name, decl.Ref); err != nil {
				return nil, err
			}
		}
		p.Kinds = append(p.Kinds, k)
	}
	return p, nil
}

func claim(taken map[string]string, name, owner string) error {
	if prev, ok := taken[name]; ok {
		return fmt.Errorf("generated type %s would be declared twice, by %s and by %s", name, prev, owner)
	}
	taken[name] = owner
	return nil
}

// planFields plans one struct's fields, appending the structs its object fields
// need to the kind. The nested struct's name is its owner's name plus the
// field's, which makes it unique by construction and readable at the use site
// (`LLMProviderPricing`). Nothing is singularized: a repeated `pricing` is
// LLMProviderPricing, not LLMProviderPricing minus an English guess.
func planFields(k *kindPlan, s *structPlan, props []*kinddialect.Property) error {
	for _, prop := range props {
		f := &fieldPlan{
			Decl:     prop,
			Key:      prop.Name,
			Name:     exported(prop.Name),
			Optional: !prop.Required,
		}
		switch prop.Datatype {
		case kinddialect.TypeString, kinddialect.TypeText, kinddialect.TypeMarkdown,
			kinddialect.TypeEmail, kinddialect.TypeURL, kinddialect.TypePhone,
			kinddialect.TypeTimezone, kinddialect.TypeRecurrence, kinddialect.TypeDate,
			kinddialect.TypeDuration:
			f.Class, f.GoElem, f.TSElem = classText, "string", "string"
		case kinddialect.TypeDatetime:
			f.Class, f.GoElem, f.TSElem = classDatetime, "string", "string"
		case kinddialect.TypeInt:
			f.Class, f.GoElem, f.TSElem = classInt, "int64", "number"
		case kinddialect.TypeFloat:
			f.Class, f.GoElem, f.TSElem = classFloat, "float64", "number"
		case kinddialect.TypeBool:
			f.Class, f.GoElem, f.TSElem = classBool, "bool", "boolean"
		case kinddialect.TypeSecret:
			f.Class, f.GoElem, f.TSElem = classSecret, "SecretRef", "SecretRef"
		case kinddialect.TypeDigest:
			f.Class, f.GoElem, f.TSElem = classDigest, "Digest", "Digest"
		case kinddialect.TypeBlobRef:
			f.Class, f.GoElem, f.TSElem = classBlobRef, "BlobDigest", "BlobDigest"
		case kinddialect.TypeJSON:
			f.Class, f.GoElem, f.TSElem = classJSON, "Dynamic", "Dynamic"
		case kinddialect.TypeReference:
			f.Class, f.GoElem, f.TSElem = classReference, "Reference", "Reference"
		case kinddialect.TypeEnum:
			f.Class = classEnum
			f.Enum = &enumPlan{
				Name:   s.Name + f.Name,
				Doc:    prop.Description,
				Values: prop.Values,
			}
			f.GoElem, f.TSElem = f.Enum.Name, f.Enum.Name
			s.Enums = append(s.Enums, f.Enum)
		case kinddialect.TypeState:
			f.Class = classState
			f.State = &statePlan{
				Name:     s.Name + f.Name,
				Doc:      prop.Description,
				Machine:  prop.Machine,
				Property: prop.Name,
			}
			f.GoElem, f.TSElem = f.State.Name, f.State.Name
			k.States = append(k.States, f.State)
		case kinddialect.TypeObject:
			f.Class = classObject
			nested := &structPlan{
				Name:  s.Name + f.Name,
				Doc:   prop.Description,
				Owner: s.Owner,
				Path:  joinPath(s.Path, prop.Name),
			}
			f.Nested = nested
			f.GoElem, f.TSElem = nested.Name, nested.Name
			k.Structs = append(k.Structs, nested)
			if err := planFields(k, nested, prop.Fields); err != nil {
				return err
			}
		default:
			return fmt.Errorf("property %s: datatype %q has no generated shape", prop.Name, prop.Datatype)
		}
		s.Fields = append(s.Fields, f)
	}
	return checkMembers(s)
}

// checkMembers refuses a struct whose generated members collide: a `createdAt`
// property generates both a CreatedAt field and a CreatedAtTime accessor, so a
// sibling property spelled `createdAtTime` would generate a struct that does
// not compile. Refusing here says which two declarations disagree.
func checkMembers(s *structPlan) error {
	members := map[string]string{}
	add := func(member, from string) error {
		if prev, ok := members[member]; ok {
			return fmt.Errorf("%s: %s and %s both generate the member %s", s.Name, prev, from, member)
		}
		members[member] = from
		return nil
	}
	for _, f := range s.Fields {
		if err := add(f.Name, f.Key); err != nil {
			return err
		}
		if f.Class == classDatetime && !f.repeated() && !f.keyed() {
			if err := add(f.Name+"Time", f.Key); err != nil {
				return err
			}
		}
	}
	return nil
}

func joinPath(path, segment string) string {
	if path == "" {
		return segment
	}
	return path + "." + segment
}

// exported is the declared name with its initial uppercased and nothing else
// touched. The loader already holds every declared name to camelCase with its
// initialisms uppercase (`baseURL`, `icalUID`), so this is the whole
// transformation — a re-spelling table would be a second opinion about names
// the declarations already settled.
func exported(name string) string {
	if name == "" {
		return ""
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// identifier renders a declared VALUE (an enum value, a state) as the suffix of
// a Go constant name: every run of alphanumerics uppercased at its first rune
// and joined, so `last30d` is Last30d and `read-only` is ReadOnly.
func identifier(value string) string {
	var out strings.Builder
	for _, part := range strings.FieldsFunc(value, func(r rune) bool {
		return !isAlphanumeric(r)
	}) {
		out.WriteString(exported(part))
	}
	return out.String()
}

func isAlphanumeric(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
}

// values is an enum's or a machine's admissible set, in declaration order.
func (e *enumPlan) values() []string {
	out := make([]string, 0, len(e.Values))
	for _, v := range e.Values {
		out = append(out, v.Value)
	}
	return out
}

// labels are the declared human labels by value, absent where none was
// declared: a client renders the label and submits the value.
func (e *enumPlan) labels() []kinddialect.EnumValue {
	var out []kinddialect.EnumValue
	for _, v := range e.Values {
		if v.Label != "" {
			out = append(out, v)
		}
	}
	return out
}

// keySet is the kind's admitted property names, sorted so the generated set
// does not depend on authoring order.
func (k *kindPlan) keySet() []string {
	out := make([]string, 0, len(k.Decl.Props))
	for _, p := range k.Decl.Props {
		out = append(out, p.Name)
	}
	sort.Strings(out)
	return out
}
