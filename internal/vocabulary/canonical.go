package vocabulary

// THE CANONICAL DECLARATION DATA.
//
// A declaration row stores its document's DATA MAP — the properties ARE the
// declaration — so the map a parse leaves behind has to be the one form the core
// declarations declare. The loader still admits the spellings that came before
// (a row an older binary stored, a document a client has not moved yet), so each
// function here rewrites one of those spellings into the declared form, in place,
// right after the parse that validated it.
//
// Two rules hold for all of them. They are IDEMPOTENT: a canonical value is its
// own canonical form, and BuildAuthorities may run over one document twice (the
// per-authority retry). And they never invent: a spelling is translated, never
// defaulted — a key the author left out stays out, so absence stored is absence
// authored.
//
// Every function here dies with the spelling it translates.

import (
	"fmt"
	"sort"
)

// canonicalIndices rewrites a kind's `indices:` entries into the declared form —
// each index an object naming its properties — from the bare list of property
// names an index used to be spelled as.
func canonicalIndices(d map[string]any) {
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

// canonicalTraitVariants rewrites a trait's `oneOf:` from the mapping of variant
// name to properties into the variant LIST every other repeated declaration is
// spelled as. The map form is the one that cannot survive the typed dialect: a
// keyed map of keyed maps leaves every reader guessing which level a path
// addresses, so `properties` keeps the keyed level and the variants take the
// list.
func canonicalTraitVariants(d map[string]any) {
	raw, has := d["oneOf"]
	if !has {
		return
	}
	if _, isList := raw.([]any); isList {
		return
	}
	variants := asMap(raw)
	out := make([]any, 0, len(variants))
	for _, name := range sortedKeys(variants) {
		out = append(out, map[string]any{"name": name, "properties": variants[name]})
	}
	d["oneOf"] = out
}

// canonicalMapRules wraps a mapping rule authored as a bare path string in the
// {path} object the declaration holds, so one property has one shape.
func canonicalMapRules(d map[string]any) {
	raw, has := d["map"]
	if !has {
		return
	}
	rules := asMap(raw)
	for name, rv := range rules {
		if s, isString := rv.(string); isString {
			rules[name] = map[string]any{"path": s}
		}
	}
	d["map"] = rules
}

// canonicalAgentTools names each `tools:` entry's ARM. A bare string named the
// arm by its value — a built-in by name, anything else a callable identity —
// which read as one shape holding two kinds of thing; the entry says which now.
func canonicalAgentTools(d map[string]any) {
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
		case agentBuiltins[s]:
			out = append(out, map[string]any{"builtin": s})
		default:
			out = append(out, map[string]any{"callable": s})
		}
	}
	d["tools"] = out
}

// canonicalFunctionCaps hoists the capability envelope out of the
// `capabilities:` wrapper onto `data` itself: one grant, declared once, at the
// level the declaration declares it. A key already hoisted wins — the parse
// refused having both, so at most one is present.
func canonicalFunctionCaps(d map[string]any) {
	wrapped, has := d["capabilities"]
	if !has {
		return
	}
	for k, v := range asMap(wrapped) {
		if !functionCapsKeys[k] {
			continue
		}
		if _, hoisted := d[k]; !hoisted {
			d[k] = v
		}
	}
	delete(d, "capabilities")
}

// canonicalFunctionIO rewrites a recursive `input:`/`output:` schema as the flat
// `arguments:`/`returns:` list. The flat list is the declared form: one level of
// named arguments, so a function's model-facing card is valid by construction
// and the value check never walks a recursion.
//
// The ORDER is the sorted argument name, which is all a schema map has to give:
// an authored `arguments:` list is left exactly as it stands, so a declaration
// keeps the order its author wrote and only a translated one is sorted.
func canonicalFunctionIO(d map[string]any) {
	canonicalIOSide(d, "arguments", "input")
	canonicalIOSide(d, "returns", "output")
}

func canonicalIOSide(d map[string]any, flat, nested string) {
	raw, has := d[nested]
	if !has {
		return
	}
	delete(d, nested)
	if _, already := d[flat]; already {
		return // the parse refused both spellings; this cannot happen
	}
	schema := asMapOrNil(raw)
	if schema == nil {
		return
	}
	d[flat] = flatArguments(schema, nested)
}

// flatArguments translates one shape schema into the flat argument list. An
// argument whose own shape is not flat — an object, a list of objects — becomes
// a `json` argument, the named escape hatch for a value whose shape the function
// does not own; a schema that is not an object of properties at all becomes ONE
// json argument named for the side it came from, since there are no names to
// carry.
func flatArguments(schema map[string]any, side string) []any {
	ty, _ := schema["type"].(string)
	props := asMapOrNil(schema["properties"])
	if ty != "object" || props == nil {
		return []any{map[string]any{"name": side, "type": ArgumentJSON}}
	}
	required := map[string]bool{}
	for _, rv := range mslice(schema, "required") {
		required[fmt.Sprint(rv)] = true
	}
	names := make([]string, 0, len(props))
	for name := range props {
		if !ValidCamel(name) {
			// A name the flat list cannot hold takes the whole side with it: half
			// a translation would silently drop an argument the body reads.
			return []any{map[string]any{"name": side, "type": ArgumentJSON}}
		}
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]any, 0, len(names))
	for _, name := range names {
		arg := map[string]any{"name": name}
		leaf := asMap(props[name])
		if desc, ok := leaf["description"].(string); ok && desc != "" {
			arg["description"] = desc
		}
		if required[name] {
			arg["required"] = true
		}
		if lt, _ := leaf["type"].(string); lt == "array" {
			arg["repeated"] = true
			leaf = asMap(leaf["items"])
		}
		arg["type"] = flatArgumentType(leaf)
		if values := mslice(leaf, "enum"); len(values) > 0 {
			arg["type"] = ArgumentEnum
			arg["values"] = values
		}
		out = append(out, arg)
	}
	return out
}

// flatArgumentType maps one leaf schema onto the argument type word. `number`
// answers `float`: the two integer words compile to one wire number, so the
// wider of them is the one that cannot refuse a value the schema admitted.
func flatArgumentType(leaf map[string]any) string {
	switch lt, _ := leaf["type"].(string); lt {
	case "string":
		return ArgumentString
	case "number":
		return ArgumentFloat
	case "boolean":
		return ArgumentBool
	default:
		return ArgumentJSON
	}
}

// canonicalFeatureScopes wraps an OAuth feature's scope list in the {scopes}
// object the declaration holds. A keyed map of LISTS is the one shape the
// property dialect cannot state — `keyed` and `repeated` are the two containers
// and a declaration is one or the other — so the value takes a field.
func canonicalFeatureScopes(d map[string]any) {
	raw, has := d["oauth2"]
	if !has {
		return
	}
	oauth2 := asMap(raw)
	fsRaw, has := oauth2["featureScopes"]
	if !has {
		return
	}
	fs := asMap(fsRaw)
	for toggle, sv := range fs {
		if list, isList := sv.([]any); isList {
			fs[toggle] = map[string]any{"scopes": list}
		}
	}
	oauth2["featureScopes"] = fs
	d["oauth2"] = oauth2
}
