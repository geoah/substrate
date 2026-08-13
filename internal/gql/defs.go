package gql

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode"

	"github.com/graphql-go/graphql"

	"github.com/geoah/substrate/internal/strictjson"
)

// The type definition is the raw YAML-shaped map the schema loader
// validated; the GraphQL layer reads only the parts it renders and ignores
// everything else, so unknown keys never break schema construction.

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
// `reference` renders as the Reference object ({authority, type, id});
// the stored value is that map, so the default resolver reads its fields. An
// `object` property carries inline structured data: it renders as
// the JSON scalar, lossless, rather than flattening to String.
//
// A `repeated: true` property is a LIST of its element type for EVERY kind
// : the element type is resolved first, then wrapped once — the
// prior code returned the bare scalar for numeric/boolean/date/json before the
// list wrapper was ever applied.
func (b *schemaBuilder) propertyType(def map[string]any, prop string) graphql.Output {
	pd := propertyDef(def, prop)
	kind, _ := pd["type"].(string)
	repeated, _ := pd["repeated"].(bool)

	var elem graphql.Output
	switch kind {
	case "int", "integer":
		elem = graphql.Int
	case "long", "int64":
		elem = longScalar
	case "float", "number":
		elem = graphql.Float
	case "bool", "boolean":
		elem = graphql.Boolean
	case "datetime":
		elem = graphql.DateTime
	case "json", "object":
		elem = jsonScalar
	case "reference":
		elem = b.referenceType
	default:
		elem = graphql.String
	}
	if repeated {
		return graphql.NewList(elem)
	}
	return elem
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
