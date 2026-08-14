package engine

// THE DIALECT-1 MANIFEST GRAMMAR, FROZEN.
//
// Dialect 1 stored a declaration's authored content in one `definition` blob, and
// that blob was written in a grammar the LIVE loader no longer admits: an agent
// tool as a bare string, a function's `capabilities:` wrapper and its recursive
// `input:`/`output:` schemas, a trait's variants as a mapping, a mapping rule as
// a bare path, an index as a bare list of names, an OAuth feature's scopes as a
// bare list. Each of those spellings is refused there now, naming what replaced
// it (internal/vocabulary), so the RUNG carries the grammar it needs to read the
// rows an older binary left — because the rows cannot be re-authored and a store
// written years ago is not going to change.
//
// It changes ONLY if a bug is found in the translation itself. A new spelling is
// never added here: dialect 1 is closed, and a spelling dialect 2 retires is
// dialect 2's business, not this file's.
//
// Every function is a TOTAL rewrite of one dialect-1 spelling into the dialect-2
// one, in place, and NEVER a validation: what comes out goes straight to the live
// loader, which is what refuses a document dialect 1 would have refused too — one
// validator, not two. Two further rules hold for all of them, and the rung leans
// on both. They are IDEMPOTENT: a dialect-2 value is its own translation, which is
// what lets a half-migrated store migrate exactly the rest. And they never
// INVENT: a spelling is translated, never defaulted, so absence stored stays
// absence authored.

import (
	"fmt"
	"sort"

	"github.com/geoah/substrate/internal/vocabulary"
)

// dialectOneData translates one stored `definition` blob into the data map the
// live loader parses, by the kind that stored it. A kind not listed stored no
// dialect-1 spelling of its own (an authority header, an actor, a property type),
// so its blob is already the document.
func dialectOneData(short string, data map[string]any) (map[string]any, error) {
	switch short {
	case vocabulary.DocKind:
		dialectOneIndices(data)
	case vocabulary.DocTrait:
		dialectOneTraitVariants(data)
	case vocabulary.DocRecordMapping:
		dialectOneMapRules(data)
	case vocabulary.DocFunction:
		dialectOneFunctionCaps(data)
		if err := dialectOneFunctionIO(data); err != nil {
			return nil, err
		}
	case vocabulary.DocAgent:
		dialectOneAgentTools(data)
	case vocabulary.DocBundle:
		dialectOneFeatureScopes(data)
	}
	return data, nil
}

// retiredDeclarationProps are dialect 1's own row properties: the `definition`
// blob and the mirror columns that pre-typed projection wrote beside it — the
// id-derived `name` and `plural`, an agent's function/sub-agent mirrors, a
// never-stored `sourceYAML`. It lives with the grammar because it is the same
// fact: what a dialect-1 row carried and a dialect-2 row does not.
//
// The rung deletes them from a row it translates, `engineOwned` excludes them
// from the properties it preserves (without which the blob would survive its own
// translation), and the projection writes each as an explicit null so a
// merge-only put clears one off a legacy row. The list cannot grow: a spelling is
// retired once.
func retiredDeclarationProps(short string) map[string]bool {
	out := map[string]bool{"definition": true, "sourceYAML": true, "name": true}
	switch short {
	case vocabulary.DocKind:
		out["plural"] = true
	case vocabulary.DocAgent:
		out["functions"], out["subagents"] = true, true
	}
	return out
}

// propDeclarationBlob is dialect 1's one authored property: the whole
// declaration, as json. Spelled once so the refusals that name it are greppable.
const propDeclarationBlob = "definition"

// dialectOneIndices rewrites a kind's `indices:` entries from the bare list of
// property names into the object naming them.
func dialectOneIndices(d map[string]any) {
	raw, has := d["indices"]
	if !has {
		return
	}
	list, ok := raw.([]any)
	if !ok {
		return
	}
	out := make([]any, 0, len(list))
	for _, iv := range list {
		if cols, isList := iv.([]any); isList {
			out = append(out, map[string]any{"properties": cols})
			continue
		}
		out = append(out, iv)
	}
	d["indices"] = out
}

// dialectOneTraitVariants rewrites a trait's `oneOf:` from the mapping of variant
// name to properties into the variant LIST. The map form is the one that could
// not survive the typed dialect: a keyed map of keyed maps leaves every reader
// guessing which level a path addresses.
func dialectOneTraitVariants(d map[string]any) {
	raw, has := d["oneOf"]
	if !has {
		return
	}
	if _, isList := raw.([]any); isList {
		return
	}
	variants := dialectOneMap(raw)
	out := make([]any, 0, len(variants))
	for _, name := range sortedKeys(variants) {
		out = append(out, map[string]any{"name": name, "properties": variants[name]})
	}
	d["oneOf"] = out
}

// dialectOneMapRules wraps a mapping rule authored as a bare path string in the
// {path} object the declaration holds.
func dialectOneMapRules(d map[string]any) {
	raw, has := d["map"]
	if !has {
		return
	}
	rules := dialectOneMap(raw)
	for name, rv := range rules {
		if s, isString := rv.(string); isString {
			rules[name] = map[string]any{"path": s}
		}
	}
	d["map"] = rules
}

// dialectOneAgentTools names each `tools:` entry's ARM. A bare string named the
// arm by its value — a built-in by name, anything else a callable identity — and
// the four built-in names are dialect 1's own closed set, frozen here.
func dialectOneAgentTools(d map[string]any) {
	builtins := map[string]bool{"query": true, "propose": true, "graphql": true, "mutate": true}
	raw, has := d["tools"]
	if !has {
		return
	}
	list, ok := raw.([]any)
	if !ok {
		return
	}
	out := make([]any, 0, len(list))
	for _, tv := range list {
		s, isString := tv.(string)
		switch {
		case !isString:
			out = append(out, tv)
		case builtins[s]:
			out = append(out, map[string]any{"builtin": s})
		default:
			out = append(out, map[string]any{"callable": s})
		}
	}
	d["tools"] = out
}

// dialectOneFunctionCaps hoists the capability envelope out of the
// `capabilities:` wrapper onto the data map itself. A key already hoisted wins:
// dialect 1 admitted either place and refused both at once, so at most one is
// present.
func dialectOneFunctionCaps(d map[string]any) {
	// Dialect 1's five capability keys, frozen: what the wrapper could hold.
	caps := map[string]bool{
		"emit": true, "reads": true, "call": true, "network": true, "mutations": true,
	}
	wrapped, has := d["capabilities"]
	if !has {
		return
	}
	for k, v := range dialectOneMap(wrapped) {
		if !caps[k] {
			continue
		}
		if _, hoisted := d[k]; !hoisted {
			d[k] = v
		}
	}
	delete(d, "capabilities")
}

// dialectOneFunctionIO rewrites a recursive `input:`/`output:` schema as the flat
// `arguments:`/`returns:` list.
//
// The ORDER is the sorted argument name, which is all a schema map has to give.
func dialectOneFunctionIO(d map[string]any) error {
	if err := dialectOneIOSide(d, "arguments", "input"); err != nil {
		return err
	}
	return dialectOneIOSide(d, "returns", "output")
}

func dialectOneIOSide(d map[string]any, flat, nested string) error {
	raw, has := d[nested]
	if !has {
		return nil
	}
	delete(d, nested)
	if _, already := d[flat]; already {
		return nil // dialect 1 refused both spellings on one function
	}
	schema := dialectOneMapOrNil(raw)
	if schema == nil {
		// Not a schema at all: dialect 1 refused this document too, so there is
		// nothing to preserve and nothing to invent — the side is simply undeclared.
		return nil
	}
	// `{type: any}` declared NO constraint, and neither does an absent side: the
	// exact flat equivalent is to say nothing. Dropping the key is a translation,
	// not a weakening — CheckValue was never called for either shape.
	if ty, _ := schema["type"].(string); ty == "any" {
		return nil
	}
	args, err := dialectOneArguments(schema, nested)
	if err != nil {
		return err
	}
	d[flat] = args
	return nil
}

// dialectOneArguments translates one dialect-1 shape schema into the flat argument
// list. An argument whose own shape is not flat — an object, a list of objects —
// becomes a `json` argument, the named escape hatch for a value whose shape the
// function does not own: the container survives, only the validation of what is
// inside it weakens, and the WIRE SHAPE a caller sends is untouched.
//
// THE WIRE SHAPE IS WHAT MAY NOT MOVE, and two dialect-1 shapes have no flat
// spelling that keeps it, so both REFUSE and the rung fails loudly rather than
// stamping a store whose callables answer a different contract than they did
// yesterday:
//
//   - a top-level schema that is not an object of named properties. `input:
//     {type: string}` took a bare string; wrapping it as `{name: input}` would
//     make every existing caller wrong. So would `{type: object}` with no
//     properties, which admitted ANY argument where a flat list closes the object.
//   - a property name the flat list cannot hold (snake_case, empty, unicode).
//     Dropping the argument would silently unadmit a value the body reads, and
//     collapsing the side into one json argument moves the shape.
//
// A repository holding one is repaired by re-installing the bundle with a flat
// declaration, under the binary that still wrote the old one.
func dialectOneArguments(schema map[string]any, side string) ([]any, error) {
	ty, _ := schema["type"].(string)
	props := dialectOneMapOrNil(schema["properties"])
	switch {
	case ty != "object":
		return nil, fmt.Errorf("data.%s declares type %q, not an object of named arguments: the flat spelling is a LIST of named arguments, so no translation keeps the wire shape a caller sends — re-declare it with `%s:` before migrating",
			side, ty, dialectOneFlatKey(side))
	case props == nil:
		return nil, fmt.Errorf("data.%s declares an object with no properties, which admitted ANY argument: a flat list closes the object, so no translation keeps the contract — re-declare it with `%s:` before migrating",
			side, dialectOneFlatKey(side))
	}
	required := map[string]bool{}
	for _, rv := range dialectOneSlice(schema, "required") {
		required[fmt.Sprint(rv)] = true
	}
	names := make([]string, 0, len(props))
	for name := range props {
		if !vocabulary.ValidCamel(name) {
			return nil, fmt.Errorf("data.%s.properties has an argument named %q, which the flat spelling cannot hold: dropping it would unadmit a value the body reads — re-declare it with `%s:` before migrating",
				side, name, dialectOneFlatKey(side))
		}
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]any, 0, len(names))
	for _, name := range names {
		arg := map[string]any{"name": name}
		leaf := dialectOneMap(props[name])
		if desc, ok := leaf["description"].(string); ok && desc != "" {
			arg["description"] = desc
		}
		if required[name] {
			arg["required"] = true
		}
		if lt, _ := leaf["type"].(string); lt == "array" {
			arg["repeated"] = true
			leaf = dialectOneMap(leaf["items"])
		}
		arg["type"] = dialectOneArgumentType(leaf)
		if values := dialectOneSlice(leaf, "enum"); len(values) > 0 {
			arg["type"] = vocabulary.ArgumentEnum
			arg["values"] = values
		}
		out = append(out, arg)
	}
	return out, nil
}

// dialectOneFlatKey is the flat key that replaced one dialect-1 IO side, for the
// refusals above: the fix an operator has to make is spelled in the message.
func dialectOneFlatKey(side string) string {
	if side == "output" {
		return "returns"
	}
	return "arguments"
}

// dialectOneArgumentType maps one dialect-1 leaf schema onto the argument type
// word. `number` answers `float`: the two integer words compile to one wire
// number, so the wider of them is the one that cannot refuse a value the schema
// admitted.
func dialectOneArgumentType(leaf map[string]any) string {
	switch lt, _ := leaf["type"].(string); lt {
	case "string":
		return vocabulary.ArgumentString
	case "number":
		return vocabulary.ArgumentFloat
	case "boolean":
		return vocabulary.ArgumentBool
	default:
		return vocabulary.ArgumentJSON
	}
}

// dialectOneFeatureScopes wraps an OAuth feature's scope list in the {scopes}
// object the declaration holds. A keyed map of LISTS is the one shape the property
// dialect cannot state — `keyed` and `repeated` are the two containers and a
// declaration is one or the other — so the value took a field.
func dialectOneFeatureScopes(d map[string]any) {
	raw, has := d["oauth2"]
	if !has {
		return
	}
	oauth2 := dialectOneMap(raw)
	fsRaw, has := oauth2["featureScopes"]
	if !has {
		return
	}
	fs := dialectOneMap(fsRaw)
	for toggle, sv := range fs {
		if list, isList := sv.([]any); isList {
			fs[toggle] = map[string]any{"scopes": list}
		}
	}
	oauth2["featureScopes"] = fs
	d["oauth2"] = oauth2
}

// --- the frozen leaf readers -----------------------------------------------
//
// The loader's own map/slice readers, copied rather than borrowed: they are part
// of the grammar this file freezes, and a translation that read a value one way
// last release must read it that way forever.

// dialectOneMap reads a nested map, empty when the value is not one. The
// map[any]any arm is the YAML decoder's shape: a stored blob is jsonb and never
// wears it, but the grammar admitted both and a frozen reader keeps its reach.
func dialectOneMap(v any) map[string]any {
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

// dialectOneMapOrNil reads a nested map, nil when the value is not one — the
// difference from dialectOneMap is the one the IO translation reads.
func dialectOneMapOrNil(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// dialectOneSlice reads a list-valued key, nil when it is absent or not a list.
func dialectOneSlice(m map[string]any, k string) []any {
	list, _ := m[k].([]any)
	return list
}
