package gql

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/graphql-go/graphql"

	"github.com/geoah/substrate/internal/strictjson"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// WHAT THIS READS. `KindInfo.Definition` is the kind's DECLARATION as the loader
// parsed it — the authored data map, which is also what the declaration's row
// stores as its properties (engine/dataset.go typeInfo). It is not a stored
// `definition` blob: that spelling is refused everywhere now.
//
// The builder reads only the parts it renders and ignores everything else, so a
// key a newer binary declares never breaks schema construction. It walks the map
// rather than a typed declaration because the schema is built from
// `substrate.KindInfo` — the API builds it too, and `internal/api` may not import
// the engine (nor `internal/substrate` the vocabulary, which declares the parsed
// kind). `kind.properties` is the meta-kind's one json leaf for the same reason a
// property declaration cannot state its own grammar: it recurses.

func definitionMap(def map[string]any, key string) map[string]any {
	m, _ := def[key].(map[string]any)
	return m
}

// declaredProperties returns the type's property names, sorted.
func declaredProperties(def map[string]any) []string {
	return sortedKeys(definitionMap(def, "properties"))
}

// propertyDef returns one property's declaration map.
func propertyDef(def map[string]any, prop string) map[string]any {
	p, _ := definitionMap(def, "properties")[prop].(map[string]any)
	return p
}

// typeCapabilities returns the bound trait names ("temporal(point)" →
// "temporal").
func typeCapabilities(def map[string]any) []string {
	raw, _ := def["traits"].([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			continue
		}
		name, _, _ := strings.Cut(s, "(")
		if name = strings.TrimSpace(name); name != "" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// hasDueAt reports whether the type binds temporal onto the dueAt column.
func hasDueAt(def map[string]any) bool {
	raw, _ := def["traits"].([]any)
	for _, v := range raw {
		if s, ok := v.(string); ok && strings.Contains(s, "dueAt") {
			return true
		}
	}
	return false
}

// typeMachines maps each declared STATE PROPERTY to the stamp properties its
// transitions write. A machine is a property of kind `state` (MODEL §11.4);
// the HasX interfaces key on that property's name, exactly as they keyed on
// the machine's name before.
func typeMachines(def map[string]any) map[string][]string {
	out := map[string][]string{}
	for name, raw := range definitionMap(def, "properties") {
		m, _ := raw.(map[string]any)
		if kind, _ := m["type"].(string); kind != "state" {
			continue
		}
		stamps := map[string]struct{}{}
		trs, _ := m["transitions"].([]any)
		for _, t := range trs {
			tm, _ := t.(map[string]any)
			for s := range definitionMap(tm, "stamps") {
				stamps[s] = struct{}{}
			}
		}
		out[name] = sortedKeys(stamps)
	}
	return out
}

// propertyType maps a declared property type onto its GraphQL type.
// Refined string types (email, url, asin, …) render as String; secrets are
// String too — the engine redacts the value, the shape stays stable. A
// `reference` renders as its own generated object type (referenceType), always:
// the stored value is the object `{ref, <link properties>}`, so the projection
// is `{ref, target, <link properties>}` whether or not the declaration carries
// link data. An `object` property carries inline structured data: it renders as
// the JSON scalar, lossless, rather than flattening to String.
//
// A `repeated: true` property is a LIST of its element type for EVERY kind
// : the element type is resolved first, then wrapped once — the
// prior code returned the bare scalar for numeric/boolean/date/json before the
// list wrapper was ever applied.
//
// A `keyed: true` property is a name-keyed MAP, and its stored value is a JSON
// object whatever its values are: it renders as the JSON scalar, because typing
// it as its value type would make every read of it a serialization error.
func (b *schemaBuilder) propertyType(t substrate.KindInfo, prop string) graphql.Output {
	pd := propertyDef(t.Definition, prop)
	kind, _ := pd["type"].(string)
	repeated, _ := pd["repeated"].(bool)
	if keyed, _ := pd["keyed"].(bool); keyed {
		return jsonScalar
	}

	var elem graphql.Output
	if kind == "reference" {
		elem = b.referenceType(t, prop, pd)
	} else {
		elem = scalarType(kind)
	}
	if repeated {
		return graphql.NewList(elem)
	}
	return elem
}

// scalarType maps a datatype name onto the scalar that carries it. Link
// properties go through it too: their grammar admits the flat datatypes only,
// so the same table answers both.
func scalarType(kind string) graphql.Output {
	switch kind {
	case "int", "integer":
		return graphql.Int
	case "long", "int64":
		return longScalar
	case "float", "number":
		return graphql.Float
	case "decimal":
		// The stored value IS the exact digit string; Float would round it,
		// which is the one thing the datatype exists to refuse.
		return graphql.String
	case "bool", "boolean":
		return graphql.Boolean
	case "datetime":
		return graphql.DateTime
	case "json", "object":
		return jsonScalar
	default:
		return graphql.String
	}
}

// linkProperties returns the names of a reference declaration's link
// properties, sorted. Empty for a reference that declares none, which changes
// which FIELDS the generated object carries and not whether one is generated.
func linkProperties(pd map[string]any) []string {
	return sortedKeys(definitionMap(pd, "properties"))
}

// referenceObjectName is the GraphQL name of a reference property's generated
// object type: the kind's own object name, the property, and the `Reference`
// suffix — "PersonMemberOfReference". It is a pure function of (kind,
// property), so the name never depends on which OTHER kinds are in the
// registry, and it is reserved against kind names for the same reason every
// structural name is.
func referenceObjectName(t substrate.KindInfo, prop string) string {
	return graphqlTypeName(t) + titleCase(prop) + "Reference"
}

// referenceType is a reference property's output type: ALWAYS a generated
// object, `{ref, target, <link properties>}`. `ref` is the referent's path as
// the Reference scalar, `target` resolves the referent through the registry
// (null when it dangles), and each declared link property is typed.
//
// EVERY reference, not only a link-carrying one (0044). The stored and served
// value has one shape, so the schema has one shape too: adding `properties:` to
// a live reference then adds FIELDS to this object instead of replacing a
// scalar field with an object field, which is a breaking schema change for a
// purely additive declaration edit. A client that wants the old string selects
// `{ ref }`.
// The cache is keyed by the OWNING (kind, property), never by the generated
// name: a name is shared only by a colliding pair, which buildObjects refuses
// before it gets here, and keying by name would have handed the second pair the
// first one's fields instead.
func (b *schemaBuilder) referenceType(t substrate.KindInfo, prop string, pd map[string]any) graphql.Output {
	name := referenceObjectName(t, prop)
	owner := t.Identity + "." + prop
	if obj, built := b.refObjects[owner]; built {
		return obj
	}
	fields := graphql.Fields{
		vocabulary.ReferenceValueKey: &graphql.Field{
			Type:        graphql.NewNonNull(referenceScalar),
			Description: `The referent's path, "<kind>/<id>".`,
			Resolve:     resolveReferencePath,
		},
		// Reserved in the loader beside `ref` (vocabulary.ReferenceTargetField),
		// because the link properties below are written into this same map: a
		// declared `target` would silently replace the referent field.
		vocabulary.ReferenceTargetField: &graphql.Field{
			Type: b.recordIF,
			Description: "The referent itself, null when the pointer dangles: a reference " +
				"survives its target's purge, so a reader gets the path either way.",
			Resolve: resolveReferenceTarget,
		},
	}
	for _, lp := range linkProperties(pd) {
		lpd, _ := definitionMap(pd, "properties")[lp].(map[string]any)
		datatype, _ := lpd["type"].(string)
		fields[camelCase(lp)] = &graphql.Field{
			Type:    scalarType(datatype),
			Resolve: resolveLinkProp(lp),
		}
	}
	obj := graphql.NewObject(graphql.ObjectConfig{
		Name:        name,
		Description: t.Identity + "." + prop + ": a pointer at another record.",
		Fields:      fields,
	})
	b.refObjects[owner] = obj
	return obj
}

// ---- small helpers ------------------------------------------------------

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// intersect returns the names present in every set, sorted.
func intersect(sets [][]string) []string {
	if len(sets) == 0 {
		return nil
	}
	counts := map[string]int{}
	for _, s := range sets {
		seen := map[string]struct{}{}
		for _, v := range s {
			if _, dup := seen[v]; dup {
				continue
			}
			seen[v] = struct{}{}
			counts[v]++
		}
	}
	out := make([]string, 0, len(counts))
	for v, n := range counts {
		if n == len(sets) {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

func mergeSorted(a, b []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(a)+len(b))
	for _, s := range append(append([]string{}, a...), b...) {
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// camelCase is the identity on a declared name: one casing rule means the
// schema already spells every name the way GraphQL renders it.
func camelCase(s string) string { return s }

func titleCase(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// argError is how a rejected JSON argument comes back: a VALIDATION error (the
// caller's spelling, not the server's fault) carrying the keys the argument
// accepts. Both halves were wrong before. `filter: unknown field "at"`
// classified as `internal` told a caller the server had broken, and the
// message named no grammar to correct itself against, so a mis-shaped date
// filter reached a user as "this server rejects date-field filtering".
func argError(arg string, dst any, err error) error {
	return fmt.Errorf("%w: %s: %w. %s takes %s", substrate.ErrValidation, arg, err,
		arg, strings.Join(strictjson.Keys(dst), ", "))
}

// remarshal moves a JSON-scalar argument (a GraphQL filter/orderBy/put/patch
// input) onto a contract struct through the SAME strict decoder REST uses
// (codex regress #9): unknown or miscased top-level keys are refused, not
// silently dropped — so a typo'd `ifversion` can no longer quietly disable CAS,
// and a misspelled filter key can no longer broaden a query. Openness stays
// only inside the map-valued fields (properties/labels/annotations, a Cond
// operator), exactly as on the REST path.
func remarshal(v, dst any) error {
	if v == nil {
		return nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return strictjson.DecodeBytes(raw, dst, false)
}
