package vocabulary_test

// The manifest dialect's typed spellings: a tool entry that names its built-in
// arm explicitly, a function's flat argument list and its hoisted capability
// keys, a mapping rule as an object, and a trait's variants as a list. Each one
// is proved to parse to the SAME parsed struct the map/wrapper/string spelling
// parses to — the dialect is two ways to write one declaration, never two
// declarations — and each refusal the dialect adds is asserted by message.

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/vocabulary"
)

// --- agent tools: the explicit builtin arm -------------------------------------

// `{builtin: query}` and the bare `query` are one entry.
func TestAgentBuiltinToolEntryEqualsBareName(t *testing.T) {
	tools := func(entries string) []vocabulary.AgentTool {
		t.Helper()
		r, err := loadAgAuthority(t, agAuthority(`  description: classifies widgets
  prompt: You classify widgets.
  provider: default
  model: claude-opus-5
  tools: `+entries+`
  emit: [ag.example.com/widget]
  reads:
    kinds: [ag.example.com/widget]
`))
		if err != nil {
			t.Fatalf("load %s: %v", entries, err)
		}
		ag, err := r.ResolveAgent("ag.example.com/classifier")
		if err != nil {
			t.Fatal(err)
		}
		return ag.Tools
	}
	bare := tools(`[query, graphql, mutate, ag.example.com/annotate]`)
	explicit := tools(`[{builtin: query}, {builtin: graphql}, {builtin: mutate}, {callable: ag.example.com/annotate}]`)
	if !reflect.DeepEqual(bare, explicit) {
		t.Fatalf("tools differ:\n bare     %+v\n explicit %+v", bare, explicit)
	}
	if len(bare) != 4 || bare[0].Builtin != vocabulary.AgentToolQuery || bare[3].Callable == "" {
		t.Fatalf("tools %+v", bare)
	}
}

// The grants a built-in needs fire on the object arm exactly as on the bare
// name: the entry is the declaration, not its spelling.
func TestAgentBuiltinToolEntryGrants(t *testing.T) {
	cases := map[string]struct{ tools, want string }{
		"query needs reads": {`[{builtin: query}]`, "query needs data.reads"},
		"propose needs the request type in emit": {
			`[{builtin: propose}]`,
			"propose needs core.substrate.reamde.dev/recordpatchrequest in data.emit",
		},
		"mutate needs emit": {`[{builtin: mutate}]`, "mutate needs data.emit"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := loadAgAuthority(t, agAuthority(`  description: d
  prompt: p
  provider: default
  model: claude-opus-5
  tools: `+tc.tools+`
`))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got: %v", tc.want, err)
			}
		})
	}
}

func TestAgentToolEntryRefusals(t *testing.T) {
	cases := map[string]struct{ tools, want string }{
		"both arms":                 {`[{builtin: query, callable: ag.example.com/annotate}]`, "an entry names exactly one of builtin and callable"},
		"neither arm":               {`[{name: markWidget}]`, "neither builtin nor callable"},
		"unknown builtin":           {`[{builtin: frobnicate}]`, "builtin \"frobnicate\" — one of query, propose, graphql, mutate"},
		"aliased builtin":           {`[{builtin: graphql, name: ask}]`, "builtin \"graphql\" takes no name or description"},
		"described builtin":         {`[{builtin: graphql, description: asks the graph}]`, "builtin \"graphql\" takes no name or description"},
		"unknown entry key":         {`[{builtin: graphql, alias: ask}]`, "unknown key \"alias\""},
		"a callable is an identity": {`[{callable: annotate}]`, "callable \"annotate\" — a full function identity"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := loadAgAuthority(t, agAuthority(`  description: d
  prompt: p
  provider: default
  model: claude-opus-5
  emit: [ag.example.com/widget]
  reads:
    kinds: [ag.example.com/widget]
  tools: `+tc.tools+`
`))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got: %v", tc.want, err)
			}
		})
	}
}

// --- function IO: the flat argument list ---------------------------------------

// flatFn loads one function whose data carries the given IO block (indented two
// spaces, under data:) and returns it parsed.
func flatFn(t *testing.T, io string) *vocabulary.Function {
	t.Helper()
	r, err := loadFnAuthority(t, `  description: d
  runtime: python
  emit: [fn.example.com/gadget]
  source: "def main(input, host): return {}"
`+io)
	if err != nil {
		t.Fatalf("load:\n%s\n%v", io, err)
	}
	fn, err := r.ResolveFunction("fn.example.com/mirror")
	if err != nil {
		t.Fatal(err)
	}
	return fn
}

// An argument list compiles to the schema map the engine already reads: the one
// the `input:` spelling produces, key for key.
func TestFunctionArgumentsCompileToTheSchema(t *testing.T) {
	cases := []struct {
		name string
		args string
		// input is the schema spelling of the same shape, empty where the
		// schema dialect has no way to say it (an enum's values).
		input string
		want  map[string]any
	}{
		{
			name:  "a described scalar",
			args:  `    - {name: url, type: string, description: the page to fetch}`,
			input: `    type: object` + "\n" + `    properties: {url: {type: string, description: the page to fetch}}`,
			want: map[string]any{"type": "object", "properties": map[string]any{
				"url": map[string]any{"type": "string", "description": "the page to fetch"},
			}},
		},
		{
			name: "the number and boolean arms",
			args: `    - {name: limit, type: int}
    - {name: ratio, type: float}
    - {name: dryRun, type: bool}`,
			input: `    type: object
    properties:
      limit: {type: number}
      ratio: {type: number}
      dryRun: {type: boolean}`,
			want: map[string]any{"type": "object", "properties": map[string]any{
				"limit":  map[string]any{"type": "number"},
				"ratio":  map[string]any{"type": "number"},
				"dryRun": map[string]any{"type": "boolean"},
			}},
		},
		{
			name:  "repeated is the array arm",
			args:  `    - {name: kinds, type: string, repeated: true, description: the kinds to sweep}`,
			input: `    type: object` + "\n" + `    properties: {kinds: {type: array, description: the kinds to sweep, items: {type: string}}}`,
			want: map[string]any{"type": "object", "properties": map[string]any{
				"kinds": map[string]any{
					"type": "array", "description": "the kinds to sweep",
					"items": map[string]any{"type": "string"},
				},
			}},
		},
		{
			name: "required lists the required arguments in declaration order",
			args: `    - {name: url, type: string, required: true}
    - {name: depth, type: int}
    - {name: token, type: string, required: true}`,
			input: `    type: object
    properties:
      url: {type: string}
      depth: {type: number}
      token: {type: string}
    required: [url, token]`,
			want: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url":   map[string]any{"type": "string"},
					"depth": map[string]any{"type": "number"},
					"token": map[string]any{"type": "string"},
				},
				"required": []any{"url", "token"},
			},
		},
		{
			name: "an enum is a string closed over its values",
			args: `    - {name: op, type: enum, values: [patch, create, delete]}`,
			want: map[string]any{"type": "object", "properties": map[string]any{
				"op": map[string]any{"type": "string", "enum": []any{"patch", "create", "delete"}},
			}},
		},
		{
			name:  "json is the escape hatch",
			args:  `    - {name: payload, type: json, description: whatever the endpoint returns}`,
			input: `    type: object` + "\n" + `    properties: {payload: {type: any, description: whatever the endpoint returns}}`,
			want: map[string]any{"type": "object", "properties": map[string]any{
				"payload": map[string]any{"type": "any", "description": "whatever the endpoint returns"},
			}},
		},
		{
			name:  "no arguments closes the object",
			args:  `    []`,
			input: `    {type: object, properties: {}}`,
			want:  map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := flatFn(t, "  arguments:\n"+tc.args+"\n").Input
			if !reflect.DeepEqual(args, tc.want) {
				t.Fatalf("arguments compiled to\n %#v\nwant\n %#v", args, tc.want)
			}
			if tc.input == "" {
				return
			}
			schema := flatFn(t, "  input:\n"+tc.input+"\n").Input
			if !reflect.DeepEqual(args, schema) {
				t.Fatalf("arguments compiled to\n %#v\ninput: declares\n %#v", args, schema)
			}
		})
	}
}

// `returns:` is the same list on the output side, and the compiled schema is
// what a declared output holds a returned value to.
func TestFunctionReturnsCompileToTheSchema(t *testing.T) {
	fn := flatFn(t, `  returns:
    - {name: ok, type: bool, required: true}
    - {name: pages, type: string, repeated: true}
`)
	want := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ok":    map[string]any{"type": "boolean"},
			"pages": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required": []any{"ok"},
	}
	if !reflect.DeepEqual(fn.Output, want) {
		t.Fatalf("returns compiled to\n %#v\nwant\n %#v", fn.Output, want)
	}
	if err := vocabulary.CheckValue(fn.Output, map[string]any{"ok": true}); err != nil {
		t.Fatalf("a valid return refused: %v", err)
	}
	if err := vocabulary.CheckValue(fn.Output, map[string]any{"pages": []any{"a"}}); err == nil {
		t.Fatal("a missing required return admitted")
	}
}

func TestFunctionArgumentRefusals(t *testing.T) {
	cases := map[string]struct{ io, want string }{
		"arguments and input both declare the call": {
			`  arguments: [{name: url, type: string}]
  input: {type: object}
`,
			"data.arguments and data.input both declare one shape",
		},
		"returns and output both declare the result": {
			`  returns: [{name: ok, type: bool}]
  output: {type: object}
`,
			"data.returns and data.output both declare one shape",
		},
		"arguments is a list": {
			`  arguments: {url: {type: string}}
`,
			"a LIST of named arguments",
		},
		"an argument is a map": {
			`  arguments: [url]
`,
			"an argument is a {name, type} map",
		},
		"argument names are camelCase": {
			`  arguments: [{name: page_url, type: string}]
`,
			`"page_url" must be camelCase`,
		},
		"an argument is named once": {
			`  arguments: [{name: url, type: string}, {name: url, type: int}]
`,
			`"url" is declared twice`,
		},
		"the type set is closed": {
			`  arguments: [{name: url, type: uri}]
`,
			`type: "uri" — one of string, int, float, bool, enum, json`,
		},
		"an argument key set is closed": {
			`  arguments: [{name: url, type: string, format: uri}]
`,
			`unknown key "format"`,
		},
		"an enum declares its values": {
			`  arguments: [{name: op, type: enum}]
`,
			"type enum declares its values",
		},
		"values belong to an enum": {
			`  arguments: [{name: url, type: string, values: [a, b]}]
`,
			"values belongs to type enum, not string",
		},
		"enum values are non-empty": {
			`  arguments: [{name: op, type: enum, values: []}]
`,
			"a non-empty LIST of the values the argument admits",
		},
		"enum values are strings": {
			`  arguments: [{name: op, type: enum, values: [{value: patch}]}]
`,
			"a non-empty string",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := loadFnAuthority(t, `  description: d
  runtime: python
  emit: [fn.example.com/gadget]
  source: "def main(input, host): return {}"
`+tc.io)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got: %v", tc.want, err)
			}
		})
	}
}

// --- function capabilities: hoisted onto data ----------------------------------

// The five capability keys parse to one envelope wherever they are written.
func TestFunctionHoistedCapabilitiesEqualTheWrapper(t *testing.T) {
	caps := func(io string) vocabulary.FunctionCaps {
		t.Helper()
		return flatFn(t, io).Caps
	}
	// flatFn's own envelope is hoisted, so the fixture declares the wrapper
	// arm here and the hoisted one is added around it.
	hoisted := caps(`  reads:
    kinds: [fn.example.com/widget]
    budgets: {calls: 4}
  call: [fn.example.com/mirror]
  network: ["https://*"]
  mutations: [merge]
`)
	r, err := loadFnAuthority(t, `  description: d
  runtime: python
  source: "def main(input, host): return {}"
  capabilities:
    emit: [fn.example.com/gadget]
    reads:
      kinds: [fn.example.com/widget]
      budgets: {calls: 4}
    call: [fn.example.com/mirror]
    network: ["https://*"]
    mutations: [merge]
`)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	fn, err := r.ResolveFunction("fn.example.com/mirror")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(hoisted, fn.Caps) {
		t.Fatalf("envelopes differ:\n hoisted %+v\n wrapped %+v", hoisted, fn.Caps)
	}
	if len(hoisted.Emit) != 1 || hoisted.Reads == nil || hoisted.Reads.Calls != 4 ||
		!hoisted.AllowsCall("fn.example.com/mirror") || !hoisted.AllowsMutation(vocabulary.MutationMerge) {
		t.Fatalf("envelope %+v", hoisted)
	}
}

// Mixing is legal as long as no key is written twice: the wrapper is a place to
// put a key, not a mode the document is in.
func TestFunctionMixedCapabilitySpellings(t *testing.T) {
	fn := flatFn(t, `  capabilities:
    reads:
      kinds: [fn.example.com/widget]
`)
	if len(fn.Caps.Emit) != 1 || fn.Caps.Reads == nil || len(fn.Caps.Reads.Kinds) != 1 {
		t.Fatalf("envelope %+v", fn.Caps)
	}
}

func TestFunctionCapabilityRefusals(t *testing.T) {
	cases := map[string]struct{ data, want string }{
		"emit declared twice": {
			`  description: d
  runtime: python
  emit: [fn.example.com/gadget]
  capabilities: {emit: [fn.example.com/widget]}
  source: "def main(input, host): return {}"
`,
			`"emit" is declared at data.emit AND at data.capabilities.emit`,
		},
		"reads declared twice": {
			`  description: d
  runtime: python
  emit: [fn.example.com/gadget]
  reads: {kinds: [fn.example.com/widget]}
  capabilities:
    reads: {kinds: [fn.example.com/gadget]}
  source: "def main(input, host): return {}"
`,
			`"reads" is declared at data.reads AND at data.capabilities.reads`,
		},
		// A hoisted key's own refusals name the hoisted path, so the fix goes
		// where the author wrote it.
		"a hoisted refusal names the hoisted path": {
			`  description: d
  runtime: python
  emit: [fn.example.com/nothing]
  source: "def main(input, host): return {}"
`,
			"data.emit: unknown type",
		},
		"a hoisted reads refusal names the hoisted path": {
			`  description: d
  runtime: python
  emit: [fn.example.com/gadget]
  reads: {kinds: [fn.example.com/nothing]}
  source: "def main(input, host): return {}"
`,
			"data.reads.kinds: unknown type",
		},
		"a hoisted call refusal names the hoisted path": {
			`  description: d
  runtime: python
  emit: [fn.example.com/gadget]
  call: [fn.example.com/nothing]
  source: "def main(input, host): return {}"
`,
			"data.call: unknown function",
		},
		"a hoisted mutations refusal names the hoisted path": {
			`  description: d
  runtime: python
  emit: [fn.example.com/gadget]
  mutations: [delete]
  source: "def main(input, host): return {}"
`,
			"data.mutations[0]",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := loadFnAuthority(t, tc.data)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got: %v", tc.want, err)
			}
		})
	}
}

// --- recordmapping: a rule as an object ---------------------------------------

const mapHead = `kind: core.substrate.reamde.dev/authority
metadata: {id: m.example.com}
data: {version: v1alpha1}
---
kind: core.substrate.reamde.dev/kind
metadata: {id: m.example.com/person}
data:
  authority: m.example.com
  names: {singular: person, plural: people}
  properties:
    name: {type: string}
    emails: {type: email, repeated: true}
---
kind: core.substrate.reamde.dev/kind
metadata: {id: m.example.com/row}
data:
  authority: m.example.com
  names: {singular: row, plural: rows}
  properties:
    name:
      type: object
      fields: {displayName: {type: string}}
    emails:
      type: object
      repeated: true
      fields: {value: email}
  edges:
    person: {to: person, required: true}
---
kind: core.substrate.reamde.dev/recordmapping
metadata: {id: m.example.com/rowperson}
data:
  authority: m.example.com
  from: m.example.com/row
  to: m.example.com/person
  edge: person
  map:
`

func loadMapping(t *testing.T, rules string) *vocabulary.Mapping {
	t.Helper()
	r, err := vocabulary.LoadFS(fstest.MapFS{
		"m.example.com/all.yaml": &fstest.MapFile{Data: []byte(mapHead + rules)},
	})
	if err != nil {
		t.Fatalf("load:\n%s\n%v", rules, err)
	}
	m, ok := r.MappingFor("m.example.com/row")
	if !ok {
		t.Fatal("m.example.com/row carries a mapping")
	}
	return m
}

// A map value is a rule object; the bare path is the same rule with the default
// merge.
func TestMappingRuleObjectEqualsBarePath(t *testing.T) {
	bare := loadMapping(t, `    name: name.displayName
`)
	object := loadMapping(t, `    name: {path: name.displayName}
`)
	if !reflect.DeepEqual(bare.Map, object.Map) || !reflect.DeepEqual(bare.MapOrder, object.MapOrder) {
		t.Fatalf("rules differ:\n bare   %+v\n object %+v", bare.Map["name"], object.Map["name"])
	}
	want := vocabulary.MapRule{
		Path:  vocabulary.Path{Prop: "name", Field: "displayName"},
		Merge: vocabulary.MergeAtomic,
	}
	if *object.Map["name"] != want {
		t.Fatalf("rule = %+v, want %+v", *object.Map["name"], want)
	}
	// The merge arm is what the object form is for: a repeated target unions
	// every live source's items.
	union := loadMapping(t, `    emails: {path: "emails[].value", merge: union}
`)
	got := *union.Map["emails"]
	if got != (vocabulary.MapRule{
		Path:  vocabulary.Path{Prop: "emails", Field: "value", OverList: true},
		Merge: vocabulary.MergeUnion,
	}) {
		t.Fatalf("union rule = %+v", got)
	}
}

func TestMappingRuleRefusals(t *testing.T) {
	cases := map[string]struct{ rules, want string }{
		"a rule key set is closed": {
			`    name: {path: name.displayName, combine: union}
`,
			`unknown key "combine"`,
		},
		"merge is atomic or union": {
			`    name: {path: name.displayName, merge: replace}
`,
			`"replace" is not a merge`,
		},
		"a rule declares its path": {
			`    name: {merge: union}
`,
			"a path is required",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := vocabulary.LoadFS(fstest.MapFS{
				"m.example.com/all.yaml": &fstest.MapFile{Data: []byte(mapHead + tc.rules)},
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got: %v", tc.want, err)
			}
		})
	}
}

// --- trait variants: a list ----------------------------------------------------

// traitDocs renders one trait carrying the given `oneOf:` block plus a kind
// binding the `range` variant, which is what proves the parsed variants are the
// ones a binding reads.
func traitDocs(oneOf string) string {
	return `kind: core.substrate.reamde.dev/authority
metadata: {id: c.example.com}
data: {version: v1alpha1}
---
kind: core.substrate.reamde.dev/trait
metadata: {id: c.example.com/temporal}
data:
  authority: c.example.com
  oneOf:
` + oneOf + `---
kind: core.substrate.reamde.dev/kind
metadata: {id: c.example.com/meeting}
data:
  authority: c.example.com
  names: {singular: meeting, plural: meetings}
  traits: [temporal(range)]
`
}

func loadTrait(t *testing.T, oneOf string) *vocabulary.Registry {
	t.Helper()
	r, err := vocabulary.LoadFS(fstest.MapFS{
		"c.example.com/all.yaml": &fstest.MapFile{Data: []byte(traitDocs(oneOf))},
	})
	if err != nil {
		t.Fatalf("load:\n%s\n%v", oneOf, err)
	}
	return r
}

func TestTraitVariantListEqualsTheMapping(t *testing.T) {
	variants := func(r *vocabulary.Registry) map[string]map[string]vocabulary.Datatype {
		t.Helper()
		g, ok := r.AuthorityByName("c.example.com")
		if !ok {
			t.Fatal("c.example.com missing")
		}
		return g.Traits["temporal"].Variants
	}
	mapped := variants(loadTrait(t, `    point: {at: datetime}
    range: {at: datetime, endsAt: datetime}
`))
	listed := variants(loadTrait(t, `    - name: point
      properties: {at: datetime}
    - name: range
      properties: {at: datetime, endsAt: datetime}
`))
	if !reflect.DeepEqual(mapped, listed) {
		t.Fatalf("variants differ:\n mapping %+v\n list    %+v", mapped, listed)
	}
	if len(listed) != 2 || listed["range"]["endsAt"] != vocabulary.DatatypeDatetime {
		t.Fatalf("variants %+v", listed)
	}
	// The binding reads them: `temporal(range)` binds both hot columns.
	r := loadTrait(t, `    - name: point
      properties: {at: datetime}
    - name: range
      properties: {at: datetime, endsAt: datetime}
`)
	k, ok := r.ByIdentity("c.example.com/meeting")
	if !ok {
		t.Fatal("c.example.com/meeting missing")
	}
	if !k.UsesHot("at") || !k.UsesHot("endsAt") {
		t.Fatalf("temporal(range) bound %v", k.HotColumns)
	}
}

func TestTraitVariantListRefusals(t *testing.T) {
	cases := map[string]struct{ oneOf, want string }{
		"a variant is a map": {
			`    - point
    - name: range
      properties: {at: datetime, endsAt: datetime}
`,
			"a variant is a {name, properties} map",
		},
		"a variant carries its name": {
			`    - properties: {at: datetime}
    - name: range
      properties: {at: datetime, endsAt: datetime}
`,
			`data.oneOf[0].name: "" must be camelCase`,
		},
		"a variant is declared once": {
			`    - name: range
      properties: {at: datetime}
    - name: range
      properties: {at: datetime, endsAt: datetime}
`,
			`variant "range" is declared twice`,
		},
		"a variant key set is closed": {
			`    - name: range
      properties: {at: datetime, endsAt: datetime}
      hot: [at]
`,
			`unknown key "hot"`,
		},
		"a variant property is a known datatype": {
			`    - name: range
      properties: {at: datetime, endsAt: instant}
`,
			`data.oneOf[0].properties.endsAt: unknown property type "instant"`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := vocabulary.LoadFS(fstest.MapFS{
				"c.example.com/all.yaml": &fstest.MapFile{Data: []byte(traitDocs(tc.oneOf))},
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got: %v", tc.want, err)
			}
		})
	}
}
