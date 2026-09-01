package gql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"

	"github.com/geoah/substrate/internal/strictjson"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The schema is generated from the repository's type registry. Mutation and
// filter inputs are JSON scalars that unmarshal onto the substrate contract
// structs (substrate.PutInput, PatchInput, Filter, …) rather than generated
// GraphQL input objects: the contract is the single source of truth for
// those shapes, and mirroring it as inputs would double every schema change
// and explode the SDL for no expressive gain.

// jsonScalar passes arbitrary JSON through in both directions.
var jsonScalar = graphql.NewScalar(graphql.ScalarConfig{
	Name:         "JSON",
	Description:  "Arbitrary JSON value.",
	Serialize:    func(v any) any { return v },
	ParseValue:   func(v any) any { return v },
	ParseLiteral: parseJSONLiteral,
})

// referenceScalar carries a `reference` property's value: the referent's record
// PATH, "<kind>/<id>" — one string, not a pair, so a pointer compares and
// indexes as the single value it is. It stays a NAMED scalar rather than plain
// String so the schema still distinguishes a pointer from prose: the console
// deep-links a Reference field to the referent's detail page, and it can only
// know to do that from the type.
var referenceScalar = graphql.NewScalar(graphql.ScalarConfig{
	Name:         "Reference",
	Description:  `A pointer at another record, as its path: "<kind>/<id>".`,
	Serialize:    coerceReferencePath,
	ParseValue:   coerceReferencePath,
	ParseLiteral: parseReferenceLiteral,
})

// coerceReferencePath passes a path through and answers nil for anything else,
// which is how a scalar says "not this type" in graphql-go.
//
// A STORED VALUE COMES IN EITHER SHAPE. A reference declared with `properties:`
// stores `{ref, …}` and one declared without stores the flat path, and which a
// row holds was decided when it was written: dropping `properties:` from a live
// declaration leaves every stored value an object, and reading only the string
// would answer null for a pointer that is plainly there. The scalar therefore
// reads `ref` out of an object rather than consulting the declaration
// (engine/refs.go splitReferenceValue states the rule).
func coerceReferencePath(value any) any {
	switch v := value.(type) {
	case string:
		return v
	case map[string]any:
		s, ok := v[vocabulary.ReferenceValueKey].(string)
		if !ok {
			return nil
		}
		return s
	}
	return nil
}

func parseReferenceLiteral(valueAST ast.Value) any {
	if v, ok := valueAST.(*ast.StringValue); ok {
		return v.Value
	}
	return nil
}

// longScalar carries int64 values (Record.version, Change.seq, changelog
// resume seqs) that GraphQL's built-in 32-bit Int would overflow past 2^31. It
// serializes as a plain JSON number, which round-trips a full int64 safely
// both ways; graphql-go marshals the response with encoding/json, so an int64
// result is written as a number, not truncated to int32.
var longScalar = graphql.NewScalar(graphql.ScalarConfig{
	Name:         "Long",
	Description:  "A 64-bit signed integer, serialized as a JSON number.",
	Serialize:    coerceLong,
	ParseValue:   coerceLong,
	ParseLiteral: parseLongLiteral,
})

// coerceLong normalizes any numeric wire representation to an int64 (nil when
// the value is not a whole number in range). It is the Serialize and ParseValue
// path for the Long scalar.
func coerceLong(value any) any {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	case uint:
		return int64(v)
	case uint32:
		return int64(v)
	case uint64:
		return int64(v)
	case float32:
		return wholeInt64(float64(v))
	case float64:
		// A float reaches here from a variable the body decoded as a
		// non-integral or too-large number (graphql.go's normalizeVariables) —
		// exactly the two shapes regress #15 forbids, so it is refused rather
		// than truncated or wrapped.
		return wholeInt64(v)
	case *int64:
		if v == nil {
			return nil
		}
		return *v
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return nil
		}
		return i
	case string:
		i, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil
		}
		return i
	}
	return nil
}

// wholeInt64 is a float's int64 value, or nil when the float is fractional or
// outside the int64 range. The upper bound is a >= comparison against 2^63
// because float64(math.MaxInt64) rounds UP to exactly 2^63 — the one value an
// int64 cannot hold.
func wholeInt64(f float64) any {
	if math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) {
		return nil
	}
	if f >= 9223372036854775808.0 || f < -9223372036854775808.0 {
		return nil
	}
	return int64(f)
}

func parseLongLiteral(v ast.Value) any {
	switch v := v.(type) {
	case *ast.IntValue:
		i, err := strconv.ParseInt(v.Value, 10, 64)
		if err != nil {
			return nil
		}
		return i
	case *ast.StringValue:
		i, err := strconv.ParseInt(v.Value, 10, 64)
		if err != nil {
			return nil
		}
		return i
	}
	return nil
}

// argInt64 reads a Long-typed argument, which parses to int64 (older Int-typed
// args parsed to int); it accepts either so a resolver survives the scalar
// widening.
func argInt64(args map[string]any, key string) (int64, bool) {
	switch v := args[key].(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	}
	return 0, false
}

func parseJSONLiteral(v ast.Value) any {
	switch v := v.(type) {
	case *ast.StringValue:
		return v.Value
	case *ast.BooleanValue:
		return v.Value
	case *ast.IntValue:
		return v.Value
	case *ast.FloatValue:
		return v.Value
	case *ast.EnumValue:
		return v.Value
	case *ast.ListValue:
		out := make([]any, 0, len(v.Values))
		for _, item := range v.Values {
			out = append(out, parseJSONLiteral(item))
		}
		return out
	case *ast.ObjectValue:
		out := map[string]any{}
		for _, f := range v.Fields {
			out[f.Name.Value] = parseJSONLiteral(f.Value)
		}
		return out
	}
	return nil
}

// traitFields overrides the derived shape for capabilities whose
// properties are hot columns rather than declared properties.
var traitFields = map[string]graphql.Fields{
	"temporal": {
		"at":     &graphql.Field{Type: graphql.DateTime, Resolve: resolveHotColumn("at")},
		"endsAt": &graphql.Field{Type: graphql.DateTime, Resolve: resolveHotColumn("endsAt")},
	},
}

type schemaBuilder struct {
	types []substrate.KindInfo

	changeType *graphql.Object
	recordIF   *graphql.Interface

	traitIF   map[string]*graphql.Interface
	machineIF map[string]*graphql.Interface
	// machineStamps is the union of stamp properties every type declaring
	// the machine writes; the interface promises all of them.
	machineStamps map[string][]string

	objects   map[string]*graphql.Object // by type identity
	objByName map[string]string          // GraphQL name -> identity
	generic   *graphql.Object
	// refObjects holds the generated object type of every reference property,
	// keyed by the owning "<kind>.<property>". Built once and reused, so a
	// repeated reference's list wraps the same type its single-valued twin
	// would; keyed by owner rather than by generated NAME, so a colliding pair
	// cannot be served the other pair's fields (reservedNames refuses one).
	refObjects map[string]*graphql.Object
}

// BuildSchema generates the repository's schema from its type registry.
func BuildSchema(types []substrate.KindInfo) (graphql.Schema, error) {
	sorted := make([]substrate.KindInfo, len(types))
	copy(sorted, types)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Identity < sorted[j].Identity })

	b := &schemaBuilder{
		types:         sorted,
		traitIF:       map[string]*graphql.Interface{},
		machineIF:     map[string]*graphql.Interface{},
		machineStamps: map[string][]string{},
		objects:       map[string]*graphql.Object{},
		objByName:     map[string]string{},
		refObjects:    map[string]*graphql.Object{},
	}
	return b.build()
}

func (b *schemaBuilder) build() (graphql.Schema, error) {
	b.changeType = graphql.NewObject(graphql.ObjectConfig{
		Name: "Change",
		Fields: graphql.Fields{
			"seq":      &graphql.Field{Type: graphql.NewNonNull(longScalar)},
			"ts":       &graphql.Field{Type: graphql.NewNonNull(graphql.DateTime)},
			"actor":    &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
			"op":       &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
			"recordId": &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
			"kind":     &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
			"payload":  &graphql.Field{Type: jsonScalar},
		},
	})

	b.recordIF = graphql.NewInterface(graphql.InterfaceConfig{
		Name:        "Record",
		Fields:      b.recordFields(),
		ResolveType: b.resolveType,
	})

	b.collectInterfaces()
	if err := b.buildObjects(); err != nil {
		return graphql.Schema{}, err
	}

	all := make([]graphql.Type, 0, len(b.objects)+1)
	names := make([]string, 0, len(b.objects))
	for id := range b.objects {
		names = append(names, id)
	}
	sort.Strings(names)
	for _, id := range names {
		all = append(all, b.objects[id])
	}
	all = append(all, b.generic)

	return graphql.NewSchema(graphql.SchemaConfig{
		Query:    b.queryType(),
		Mutation: b.mutationType(),
		Types:    all,
	})
}

// recordFields is the shape every record object and the Record interface
// share; each object gets its own copy plus its declared properties.
func (b *schemaBuilder) recordFields() graphql.Fields {
	return graphql.Fields{
		"id":   &graphql.Field{Type: graphql.NewNonNull(graphql.ID)},
		"kind": &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
		// Set only when the read was addressed by a former id: the record is
		// the canonical one, and this says so (MODEL §4.1). Null — not "" —
		// on a canonical read: GraphQL cannot omit a requested field, so
		// absent has to be spelled null.
		"canonicalId": &graphql.Field{Type: graphql.ID, Resolve: resolveCanonicalID},
		// The ids merges fused into this one, server-set.
		"formerIds":   &graphql.Field{Type: graphql.NewList(graphql.NewNonNull(graphql.ID))},
		"title":       &graphql.Field{Type: graphql.String},
		"body":        &graphql.Field{Type: graphql.String},
		"version":     &graphql.Field{Type: graphql.NewNonNull(longScalar)},
		"createdAt":   &graphql.Field{Type: graphql.NewNonNull(graphql.DateTime)},
		"updatedAt":   &graphql.Field{Type: graphql.NewNonNull(graphql.DateTime)},
		"deletedAt":   &graphql.Field{Type: graphql.DateTime},
		"labels":      &graphql.Field{Type: jsonScalar},
		"annotations": &graphql.Field{Type: jsonScalar},
		// The raw property map, state properties included; there is no
		// separate states map (MODEL §11.4, §11.6).
		"properties": &graphql.Field{Type: jsonScalar},
		// Per-property provenance: manager, updatedAt and the
		// alternatives. Non-null only on single-record reads — record(id) —
		// because only those assemble it.
		"propertyMeta": &graphql.Field{Type: jsonScalar, Resolve: resolvePropertyMeta},
		// Reverse pointers are NOT on the record manifest: they are a
		// derived, separately paged resource (REST GET …/{id}/incoming) so an
		// unbounded reverse fan-out never inflates the canonical document.
		"history": &graphql.Field{
			Type: graphql.NewList(graphql.NewNonNull(b.changeType)),
			Args: graphql.FieldConfigArgument{
				"first": &graphql.ArgumentConfig{Type: graphql.Int},
			},
			Resolve: resolveHistory,
		},
	}
}

// collectInterfaces derives the capability and machine interfaces from the
// registry: a capability interface promises the properties every type bound
// to it declares, a machine interface its state plus every declared stamp.
func (b *schemaBuilder) collectInterfaces() {
	capProps := map[string][][]string{}
	for _, t := range b.types {
		for _, c := range typeCapabilities(t.Definition) {
			capProps[c] = append(capProps[c], declaredProperties(t.Definition))
		}
		for m, stamps := range typeMachines(t.Definition) {
			b.machineStamps[m] = mergeSorted(b.machineStamps[m], stamps)
		}
	}

	for _, c := range sortedKeys(capProps) {
		fields := graphql.Fields{}
		if override, ok := traitFields[c]; ok {
			for k, f := range override {
				fields[k] = f
			}
		} else {
			for _, p := range intersect(capProps[c]) {
				fields[camelCase(p)] = &graphql.Field{Type: graphql.String, Resolve: resolveProp(p)}
			}
		}
		if len(fields) == 0 {
			continue
		}
		b.traitIF[c] = graphql.NewInterface(graphql.InterfaceConfig{
			Name:        titleCase(c),
			Fields:      fields,
			ResolveType: b.resolveType,
		})
	}

	for _, m := range sortedKeys(b.machineStamps) {
		fields := graphql.Fields{
			camelCase(m): &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: resolveState(m)},
		}
		for _, s := range b.machineStamps[m] {
			fields[camelCase(s)] = &graphql.Field{Type: graphql.DateTime, Resolve: resolveTimeProp(s)}
		}
		b.machineIF[m] = graphql.NewInterface(graphql.InterfaceConfig{
			Name:        "Has" + titleCase(m),
			Fields:      fields,
			ResolveType: b.resolveType,
		})
	}
}

func (b *schemaBuilder) buildObjects() error {
	reserved, err := b.reservedNames()
	if err != nil {
		return err
	}
	for _, t := range b.types {
		// The GraphQL name is a PURE FUNCTION of the type's identity and
		// source: shipped types keep bare names, installed types are always
		// authority-prefixed, and the name never depends on which OTHER types
		// are in the registry. A name that lands on a structural/interface
		// name, or on another type's name, is REFUSED here with a clear error
		// rather than silently renamed — installing a bundle can never rename
		// an existing type's GraphQL name.
		name := graphqlTypeName(t)
		if owner, taken := reserved[name]; taken {
			return fmt.Errorf("graphql: type %s cannot take name %q — reserved for %s; rename the type or its authority", t.Identity, name, owner)
		}
		if prior, taken := b.objByName[name]; taken && prior != t.Identity {
			return fmt.Errorf("graphql: types %s and %s both map to GraphQL name %q — authority-prefix collision; rename one type or its authority", prior, t.Identity, name)
		}
		fields := b.recordFields()
		ifaces := []*graphql.Interface{b.recordIF}

		for _, p := range declaredProperties(t.Definition) {
			fname := camelCase(p)
			// A declared property may collide with an Record interface
			// field (core.substrate.reamde.dev/kind declares "version": the
			// declaration's own version, while Record.version is the CAS
			// counter — two different numbers). Identical String fields
			// (title, body) defer to the interface column; everything else
			// gets a Prop suffix, because same type does not mean same value.
			if _, reserved := fields[fname]; reserved {
				if fname == "title" || fname == "body" {
					continue
				}
				fname += "Prop" // e.g. type.core's version vs Record.version Int!
			}
			fields[fname] = &graphql.Field{
				Type:    b.propertyType(t, p),
				Resolve: resolveProp(p),
			}
		}
		for _, c := range typeCapabilities(t.Definition) {
			iface, ok := b.traitIF[c]
			if !ok {
				continue
			}
			for fname, f := range iface.Fields() {
				fields[fname] = fieldFromDefinition(f)
			}
			ifaces = append(ifaces, iface)
		}
		// dueAt is a hot property bound through temporal(point: dueAt).
		if hasDueAt(t.Definition) {
			fields["dueAt"] = &graphql.Field{Type: graphql.DateTime, Resolve: resolveHotColumn("dueAt")}
		}
		for m := range typeMachines(t.Definition) {
			iface, ok := b.machineIF[m]
			if !ok {
				continue
			}
			for fname, f := range iface.Fields() {
				fields[fname] = fieldFromDefinition(f)
			}
			ifaces = append(ifaces, iface)
		}

		obj := graphql.NewObject(graphql.ObjectConfig{
			Name:        name,
			Description: t.Identity,
			Fields:      fields,
			Interfaces:  ifaces,
		})
		b.objects[t.Identity] = obj
		b.objByName[name] = t.Identity
	}

	b.generic = graphql.NewObject(graphql.ObjectConfig{
		Name:        "GenericRecord",
		Description: "Fallback shape for records of an unregistered type.",
		Fields:      b.recordFields(),
		Interfaces:  []*graphql.Interface{b.recordIF},
	})
	return nil
}

// reservedNames is the closed set of GraphQL names a registry type may not
// claim: the structural types and scalars, the capability and machine
// interfaces derived from this registry, and the object type EVERY reference
// property generates. A collision is a schema-build error, not a silent rename.
//
// It also REFUSES a registry whose own reference names collide with each
// other: `<Kind><Property>Reference` is a pure function of (kind, property),
// and two pairs can spell one name (`task`.`noteX` and `taskNote`.`x`). One
// generated object would then carry the fields of whichever pair was built
// first, and the other property would serve its link data under the wrong
// schema. Nothing but single-word kind names keeps that from happening, which
// is not a rule the loader enforces.
func (b *schemaBuilder) reservedNames() (map[string]string, error) {
	r := map[string]string{
		"Record":           "the Record interface",
		"GenericRecord":    "the fallback record type",
		"Change":           "the Change type",
		"Reference":        "the Reference scalar",
		"RecordConnection": "the list connection type",
		"SearchHit":        "the search hit type",
		"ChangePage":       "the changelog page type",
		"Query":            "the query root",
		"Mutation":         "the mutation root",
		"JSON":             "the JSON scalar",
		"Long":             "the Long scalar",
	}
	for _, iface := range b.traitIF {
		r[iface.Name()] = "a capability interface"
	}
	for _, iface := range b.machineIF {
		r[iface.Name()] = "a machine interface"
	}
	// The generated reference objects are named from (kind, property) before
	// any object is built, so a kind whose own name lands on one is refused
	// here rather than colliding inside graphql-go's type map.
	for _, t := range b.types {
		for _, p := range declaredProperties(t.Definition) {
			pd := propertyDef(t.Definition, p)
			if kind, _ := pd["type"].(string); kind != "reference" {
				continue
			}
			name := referenceObjectName(t, p)
			owner := "the reference type of " + t.Identity + "." + p
			if prior, taken := r[name]; taken {
				return nil, fmt.Errorf(
					"graphql: %s and %s both map to GraphQL name %q — rename one property or its kind",
					prior, owner, name)
			}
			r[name] = owner
		}
	}
	return r, nil
}

// fieldFromDefinition rebuilds an object field from an interface's field
// definition; graphql-go's FieldDefinition is not itself usable as a Field.
func fieldFromDefinition(f *graphql.FieldDefinition) *graphql.Field {
	args := graphql.FieldConfigArgument{}
	for _, a := range f.Args {
		args[a.PrivateName] = &graphql.ArgumentConfig{Type: a.Type, DefaultValue: a.DefaultValue}
	}
	return &graphql.Field{Type: f.Type, Args: args, Resolve: f.Resolve}
}

// graphqlTypeName is the deterministic GraphQL object name for a kind. The
// rule itself lives in the schema package (vocabulary.GraphQLName), because the
// DECLARATION path has to apply it too: two kinds that resolve to one GraphQL
// name are refused when the second is declared, never silently renamed.
func graphqlTypeName(t substrate.KindInfo) string {
	return vocabulary.GraphQLName(t.Identity, t.Source)
}

func (b *schemaBuilder) resolveType(p graphql.ResolveTypeParams) *graphql.Object {
	e, ok := p.Value.(*substrate.Record)
	if !ok || e == nil {
		return b.generic
	}
	if obj, ok := b.objects[e.Kind]; ok {
		return obj
	}
	return b.generic
}

// ---- root types ---------------------------------------------------------

// The JSON arguments carry their grammar in their DESCRIPTION, because the
// scalar cannot carry it in its type: `filter` is deliberately a JSON scalar
// (see the note at the top of this file), so introspection shows a caller the
// word "JSON" and nothing else. A client with only introspection to read (an
// agent holding the `graphql` tool) then guesses the shape, and a guess that
// comes back as a bare `unknown field "at"` reads as a missing FEATURE rather
// than a misspelling: one reported the substrate could not filter by date at
// all, which it has always been able to do.
//
// The key lists are computed from the contract structs, so a field added to
// substrate.Filter appears here without anybody remembering to write it down.
var (
	filterArgDescription = "Narrows the list. Keys: " + strings.Join(strictjson.Keys(substrate.Filter{}), ", ") +
		". `properties` and `labels` map a name to one condition object (" +
		strings.Join(strictjson.Keys(substrate.Cond{}), ", ") +
		"); timestamps are RFC3339 strings, compared as instants. A kind with the " +
		"temporal trait carries `at` and `endsAt` (tasks: `dueAt`) as filterable, " +
		"orderable properties, so one day's events are " +
		`{"kinds": ["calendar.substrate.reamde.dev/calendarevent"], ` +
		`"properties": {"at": {"gte": "2026-08-15T00:00:00Z", "lt": "2026-08-16T00:00:00Z"}}}.`

	orderByArgDescription = "Sort keys, applied in order: [{" + strings.Join(strictjson.Keys(substrate.Order{}), ", ") +
		"}]. `property` is a declared property, a temporal property (at, endsAt, dueAt), " +
		"createdAt or updatedAt, always camelCase: the snake spelling is refused naming " +
		"its replacement. Default: createdAt, newest first."

	changeFilterArgDescription = "Narrows the changelog walk. Keys: " +
		strings.Join(strictjson.Keys(substrate.ChangeFilter{}), ", ") + "."
)

func (b *schemaBuilder) queryType() *graphql.Object {
	connection := graphql.NewObject(graphql.ObjectConfig{
		Name: "RecordConnection",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type: graphql.NewList(b.recordIF),
				Description: "The page's records, as the Record interface: id, kind, title and the " +
					"other shared fields read directly, and a kind's own properties need an " +
					`inline fragment, nodes { id ... on Calendarevent { summary at } }.`,
			},
			"cursor": &graphql.Field{Type: graphql.String},
			// No `total`: the keyset walk never counts the matching set, and a
			// field that answered 0 for "unknown" is a wrong count, not a
			// missing one. A client that needs a size walks the pages.
			// head is the changelog head seq at the snapshot this page was read
			// from: resume the watch from it for a gapless,
			// no-double-see list→watch handoff.
			"head": &graphql.Field{Type: longScalar},
		},
	})
	hit := graphql.NewObject(graphql.ObjectConfig{
		Name: "SearchHit",
		Fields: graphql.Fields{
			"record":   &graphql.Field{Type: b.recordIF},
			"lexical":  &graphql.Field{Type: graphql.Float},
			"semantic": &graphql.Field{Type: graphql.Float},
		},
	})
	changePage := graphql.NewObject(graphql.ObjectConfig{
		Name: "ChangePage",
		Fields: graphql.Fields{
			"changes": &graphql.Field{Type: graphql.NewList(graphql.NewNonNull(b.changeType))},
			// `from` is a transparent seq, not an opaque cursor (ruling A5's one
			// continuation rule): it is the last seq shown, passed back as the
			// next call's `from` to resume the forward walk.
			"from": &graphql.Field{Type: longScalar},
		},
	})

	return graphql.NewObject(graphql.ObjectConfig{
		Name: "Query",
		Fields: graphql.Fields{
			"record": &graphql.Field{
				// Ref-addressed: identity is the (type, id)
				// pair, so the lookup names both.
				Type: b.recordIF,
				Args: graphql.FieldConfigArgument{
					"kind": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)},
					"id":   &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.ID)},
				},
				Resolve: resolveRecord,
			},
			"records": &graphql.Field{
				Type:        connection,
				Description: "The one generic list query: every kind, one grammar, keyset-paged.",
				Args: graphql.FieldConfigArgument{
					"filter":  &graphql.ArgumentConfig{Type: jsonScalar, Description: filterArgDescription},
					"orderBy": &graphql.ArgumentConfig{Type: jsonScalar, Description: orderByArgDescription},
					"first":   &graphql.ArgumentConfig{Type: graphql.Int, Description: "Page size: default 50, max 500."},
					"after":   &graphql.ArgumentConfig{Type: graphql.String, Description: "The `cursor` a previous page returned. Opaque: never parse it."},
				},
				Resolve: resolveRecords,
			},
			"search": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(hit)),
				Args: graphql.FieldConfigArgument{
					"q":     &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)},
					"mode":  &graphql.ArgumentConfig{Type: graphql.String},
					"kinds": &graphql.ArgumentConfig{Type: graphql.NewList(graphql.NewNonNull(graphql.String))},
					"k":     &graphql.ArgumentConfig{Type: graphql.Int},
				},
				Resolve: resolveSearch,
			},
			"changelog": &graphql.Field{
				Type: changePage,
				Args: graphql.FieldConfigArgument{
					// `from` is a transparent resume seq, not an
					// opaque cursor: the walk reads changes with seq > from.
					"from":   &graphql.ArgumentConfig{Type: longScalar, Description: "Resume seq: the walk reads changes with seq > from."},
					"filter": &graphql.ArgumentConfig{Type: jsonScalar, Description: changeFilterArgDescription},
					"first":  &graphql.ArgumentConfig{Type: graphql.Int, Description: "Page size: default 100."},
				},
				Resolve: resolveChangelog,
			},
		},
	})
}

func (b *schemaBuilder) mutationType() *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: "Mutation",
		Fields: graphql.Fields{
			"put": &graphql.Field{
				Type:    b.recordIF,
				Args:    graphql.FieldConfigArgument{"input": &graphql.ArgumentConfig{Type: graphql.NewNonNull(jsonScalar)}},
				Resolve: resolvePut,
			},
			"patch": &graphql.Field{
				Type: b.recordIF,
				Args: graphql.FieldConfigArgument{
					"kind":      &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)},
					"id":        &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.ID)},
					"input":     &graphql.ArgumentConfig{Type: graphql.NewNonNull(jsonScalar)},
					"ifVersion": &graphql.ArgumentConfig{Type: longScalar},
				},
				Resolve: resolvePatch,
			},
			"delete": &graphql.Field{
				Type: b.recordIF,
				Args: graphql.FieldConfigArgument{
					"kind": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)},
					"id":   &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.ID)},
				},
				Resolve: resolveDelete,
			},
			"merge": &graphql.Field{
				Type: b.recordIF,
				Args: graphql.FieldConfigArgument{
					"kind":   &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)},
					"winner": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.ID)},
					"loser":  &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.ID)},
				},
				Resolve: resolveMerge,
			},
			"split": &graphql.Field{
				Type:    b.recordIF,
				Args:    graphql.FieldConfigArgument{"mergeId": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.ID)}},
				Resolve: resolveSplit,
			},
		},
	})
}

// ---- resolvers ----------------------------------------------------------

var errNoDataset = errors.New("substrate: no dataset on request")

func datasetOf(ctx context.Context) (substrate.Dataset, substrate.Actor, error) {
	ds := DatasetFrom(ctx)
	if ds == nil {
		return nil, "", errNoDataset
	}
	return ds, ActorFrom(ctx), nil
}

func recordOf(p graphql.ResolveParams) *substrate.Record {
	e, _ := p.Source.(*substrate.Record)
	return e
}

// GraphQL and REST answer with the SAME authority: a token reads and writes
// its whole repository. The per-resolver scope gates that used
// to stand here went with the least-privilege model — what a caller may not
// touch is decided by the kind (the auth kinds refuse generic writes at the
// engine's chokepoint), never by a capability list on the credential.

// resolveCanonicalID answers null rather than "" when the read was already
// canonical: the field means "you asked by a former id, here is the real one",
// and an empty string is a value, not the absence of one.
func resolveCanonicalID(p graphql.ResolveParams) (any, error) {
	e := recordOf(p)
	if e == nil || e.CanonicalID == "" {
		return nil, nil
	}
	return e.CanonicalID, nil
}

// resolvePropertyMeta answers null when the read carried none: lists never
// assemble it, and null — not {} — is how GraphQL spells absent.
func resolvePropertyMeta(p graphql.ResolveParams) (any, error) {
	e := recordOf(p)
	if e == nil || len(e.PropertyMeta) == 0 {
		return nil, nil
	}
	return e.PropertyMeta, nil
}

func resolveProp(name string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		e := recordOf(p)
		if e == nil {
			return nil, nil
		}
		return e.Properties[name], nil
	}
}

// referenceValue reads the source of a generated reference object's field. The
// stored value is an object keyed by `ref`, and a bare path is accepted too:
// coercion normalizes a props-less write to the object shape, so the string is
// what a value written before that normalization still reads as.
func referenceValue(source any) (path string, props map[string]any) {
	switch v := source.(type) {
	case string:
		return v, nil
	case map[string]any:
		s, _ := v[vocabulary.ReferenceValueKey].(string)
		return s, v
	}
	return "", nil
}

func resolveReferencePath(p graphql.ResolveParams) (any, error) {
	path, _ := referenceValue(p.Source)
	return path, nil
}

func resolveLinkProp(name string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		_, props := referenceValue(p.Source)
		return props[name], nil
	}
}

// resolveReferenceTarget follows the pointer. A reference outlives its target
// (a purge leaves the value behind), and an unpinned one may name a kind this
// repository never declared, so a target that does not resolve is null rather
// than an error: the `ref` field still answers where it pointed.
func resolveReferenceTarget(p graphql.ResolveParams) (any, error) {
	path, _ := referenceValue(p.Source)
	kind, id, ok := vocabulary.SplitRecordPath(path)
	if !ok {
		return nil, nil
	}
	ds, _, err := datasetOf(p.Context)
	if err != nil {
		return nil, err
	}
	return nilOnNotFound(ds.Get(p.Context, kind, id))
}

func resolveTimeProp(name string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		e := recordOf(p)
		if e == nil {
			return nil, nil
		}
		switch v := e.Properties[name].(type) {
		case nil:
			return nil, nil
		case time.Time:
			return v, nil
		case *time.Time:
			return v, nil
		case string:
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return nil, nil
			}
			return t, nil
		}
		return nil, nil
	}
}

func resolveHotColumn(name string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		e := recordOf(p)
		if e == nil {
			return nil, nil
		}
		switch name {
		case substrate.PropAt:
			return e.At, nil
		case substrate.PropEndsAt:
			return e.EndsAt, nil
		case substrate.PropDueAt:
			return e.DueAt, nil
		}
		return nil, nil
	}
}

// resolveState reads a state property out of the properties map: a machine is
// a property, and its current state is that property's value (MODEL §11.4).
func resolveState(name string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		e := recordOf(p)
		if e == nil {
			return "", nil
		}
		s, _ := e.Properties[name].(string)
		return s, nil
	}
}

func resolveHistory(p graphql.ResolveParams) (any, error) {
	e := recordOf(p)
	if e == nil {
		return nil, nil
	}
	ds, _, err := datasetOf(p.Context)
	if err != nil {
		return nil, err
	}
	first, _ := p.Args["first"].(int)
	if first <= 0 {
		first = 50
	}
	// History is addressed by FULL identity: the record id alone
	// is not unique — two types may share an id — so the audit trail is scoped
	// to (type, id), never the bare id. The changelog `type` column and the
	// Types predicate carry the type half.
	return ds.Changes(p.Context, 0, substrate.ChangeFilter{RecordID: e.ID, Kinds: []string{e.Kind}}, first)
}

func resolveRecord(p graphql.ResolveParams) (any, error) {
	ds, _, err := datasetOf(p.Context)
	if err != nil {
		return nil, err
	}
	typ, _ := p.Args["kind"].(string)
	id, _ := p.Args["id"].(string)
	if typ == "" || id == "" {
		return nil, errors.New("record: type and id required — identity is the (type, id) pair")
	}
	return nilOnNotFound(ds.Get(p.Context, typ, id))
}

func nilOnNotFound(e *substrate.Record, err error) (any, error) {
	if errors.Is(err, substrate.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return e, nil
}

func resolveRecords(p graphql.ResolveParams) (any, error) {
	ds, _, err := datasetOf(p.Context)
	if err != nil {
		return nil, err
	}
	var q substrate.Query
	if err := remarshal(p.Args["filter"], &q.Filter); err != nil {
		return nil, argError("filter", substrate.Filter{}, err)
	}
	if err := remarshal(p.Args["orderBy"], &q.OrderBy); err != nil {
		return nil, argError("orderBy", substrate.Order{}, err)
	}
	q.First, _ = p.Args["first"].(int)
	q.After, _ = p.Args["after"].(string)
	page, err := ds.List(p.Context, q)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"nodes": page.Records, "cursor": page.Cursor, "head": page.Head,
	}, nil
}

func resolveSearch(p graphql.ResolveParams) (any, error) {
	ds, _, err := datasetOf(p.Context)
	if err != nil {
		return nil, err
	}
	in := substrate.SearchInput{Q: p.Args["q"].(string)}
	if m, ok := p.Args["mode"].(string); ok && m != "" {
		in.Mode = substrate.SearchMode(strings.ToLower(m))
	}
	if ts, ok := p.Args["kinds"].([]any); ok {
		for _, t := range ts {
			if s, ok := t.(string); ok {
				in.Kinds = append(in.Kinds, s)
			}
		}
	}
	in.K, _ = p.Args["k"].(int)
	hits, err := ds.Search(p.Context, in)
	if err != nil {
		return nil, err
	}
	return hits, nil
}

func resolveChangelog(p graphql.ResolveParams) (any, error) {
	ds, _, err := datasetOf(p.Context)
	if err != nil {
		return nil, err
	}
	var f substrate.ChangeFilter
	if err := remarshal(p.Args["filter"], &f); err != nil {
		return nil, argError("filter", substrate.ChangeFilter{}, err)
	}
	from, _ := argInt64(p.Args, "from")
	first, _ := p.Args["first"].(int)
	if first <= 0 {
		first = 100
	}
	changes, err := ds.Changes(p.Context, from, f, first)
	if err != nil {
		return nil, err
	}
	last := from
	if len(changes) > 0 {
		last = changes[len(changes)-1].Seq
	}
	return map[string]any{"changes": changes, "from": last}, nil
}

func resolvePut(p graphql.ResolveParams) (any, error) {
	ds, actor, err := datasetOf(p.Context)
	if err != nil {
		return nil, err
	}
	var in substrate.PutInput
	if err := remarshal(p.Args["input"], &in); err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	return ds.Put(p.Context, actor, in)
}

func resolvePatch(p graphql.ResolveParams) (any, error) {
	ds, actor, err := datasetOf(p.Context)
	if err != nil {
		return nil, err
	}
	var in substrate.PatchInput
	if err := remarshal(p.Args["input"], &in); err != nil {
		return nil, fmt.Errorf("input: %w", err)
	}
	if n, ok := argInt64(p.Args, "ifVersion"); ok {
		in.IfVersion = &n
	}
	typ := p.Args["kind"].(string)
	return ds.Patch(p.Context, actor, typ, p.Args["id"].(string), in)
}

func resolveDelete(p graphql.ResolveParams) (any, error) {
	ds, actor, err := datasetOf(p.Context)
	if err != nil {
		return nil, err
	}
	typ := p.Args["kind"].(string)
	return ds.Delete(p.Context, actor, typ, p.Args["id"].(string))
}

func resolveMerge(p graphql.ResolveParams) (any, error) {
	ds, actor, err := datasetOf(p.Context)
	if err != nil {
		return nil, err
	}
	typ := p.Args["kind"].(string)
	return ds.Merge(p.Context, actor, typ, p.Args["winner"].(string), p.Args["loser"].(string))
}

func resolveSplit(p graphql.ResolveParams) (any, error) {
	ds, actor, err := datasetOf(p.Context)
	if err != nil {
		return nil, err
	}
	return ds.Split(p.Context, actor, p.Args["mergeId"].(string))
}
