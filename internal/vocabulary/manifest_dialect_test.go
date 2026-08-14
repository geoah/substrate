package vocabulary_test

// The manifest dialect's typed spellings, and they are the ONLY ones: a tool
// entry names its arm, a function declares flat arguments and its capability keys
// on `data` itself, a mapping rule is an object, a trait's variants are a list.
// Each spelling that came before is refused here, naming what replaced it — the
// stored rows written that way are translated by the dialect rung, whose frozen
// grammar and fixtures live in internal/engine — and each refusal the dialect
// adds is asserted by message.

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/vocabulary"
)

// --- agent tools: the explicit builtin arm -------------------------------------

// An entry names its arm: `{builtin: query}` for a built-in, `{callable: …}` for
// a function, and the parsed tool carries the model-facing name either way.
func TestAgentToolEntriesNameTheirArm(t *testing.T) {
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
	entries := tools(`[{builtin: query}, {builtin: graphql}, {builtin: mutate}, {callable: ag.example.com/annotate}]`)
	if len(entries) != 4 || entries[0].Builtin != vocabulary.AgentToolQuery || entries[0].Name != vocabulary.AgentToolQuery {
		t.Fatalf("tools %+v", entries)
	}
	if entries[3].Callable != "ag.example.com/annotate" || entries[3].Name != "annotate" {
		t.Fatalf("the callable entry %+v", entries[3])
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
		// The retired spelling: a bare string named the arm by its value, so a typo
		// in a built-in's name became a callable nothing declares.
		"a bare built-in name": {`[query]`, `"query" is a bare string — an entry names its arm: {builtin: query}`},
		"a bare callable identity": {
			`[ag.example.com/annotate]`,
			`"ag.example.com/annotate" is a bare string — an entry names its arm: {callable: ag.example.com/annotate}`,
		},
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

// An argument list compiles to the schema map the engine reads: CheckValue holds
// a call to it, and the model-facing tool card is rendered from it.
func TestFunctionArgumentsCompileToTheSchema(t *testing.T) {
	cases := []struct {
		name string
		args string
		want map[string]any
	}{
		{
			name: "a described scalar",
			args: `    - {name: url, type: string, description: the page to fetch}`,
			want: map[string]any{"type": "object", "properties": map[string]any{
				"url": map[string]any{"type": "string", "description": "the page to fetch"},
			}},
		},
		{
			name: "the number and boolean arms",
			args: `    - {name: limit, type: int}
    - {name: ratio, type: float}
    - {name: dryRun, type: bool}`,
			want: map[string]any{"type": "object", "properties": map[string]any{
				"limit":  map[string]any{"type": "number"},
				"ratio":  map[string]any{"type": "number"},
				"dryRun": map[string]any{"type": "boolean"},
			}},
		},
		{
			name: "repeated is the array arm",
			args: `    - {name: kinds, type: string, repeated: true, description: the kinds to sweep}`,
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
			name: "json is the escape hatch",
			args: `    - {name: payload, type: json, description: whatever the endpoint returns}`,
			want: map[string]any{"type": "object", "properties": map[string]any{
				"payload": map[string]any{"type": "any", "description": "whatever the endpoint returns"},
			}},
		},
		{
			name: "no arguments closes the object",
			args: `    []`,
			want: map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := flatFn(t, "  arguments:\n"+tc.args+"\n").Input
			if !reflect.DeepEqual(args, tc.want) {
				t.Fatalf("arguments compiled to\n %#v\nwant\n %#v", args, tc.want)
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

// --- function capabilities: on data itself --------------------------------------

// The five capability keys ride `data` — one grant, declared once — and the
// envelope they parse to is what every gate downstream reads.
func TestFunctionCapabilitiesRideData(t *testing.T) {
	caps := flatFn(t, `  reads:
    kinds: [fn.example.com/widget]
    budgets: {calls: 4}
  call: [fn.example.com/mirror]
  network: ["https://*"]
  mutations: [merge]
`).Caps
	if len(caps.Emit) != 1 || caps.Reads == nil || caps.Reads.Calls != 4 ||
		!caps.AllowsCall("fn.example.com/mirror") || !caps.AllowsMutation(vocabulary.MutationMerge) {
		t.Fatalf("envelope %+v", caps)
	}
}

func TestFunctionCapabilityRefusals(t *testing.T) {
	cases := map[string]struct{ data, want string }{
		// A capability key's refusals name the path it was written at, so the fix
		// goes where the author is already looking.
		"a refusal names the key's path": {
			`  description: d
  runtime: python
  emit: [fn.example.com/nothing]
  source: "def main(input, host): return {}"
`,
			"data.emit: unknown type",
		},
		"a reads refusal names the key's path": {
			`  description: d
  runtime: python
  emit: [fn.example.com/gadget]
  reads: {kinds: [fn.example.com/nothing]}
  source: "def main(input, host): return {}"
`,
			"data.reads.kinds: unknown type",
		},
		"a call refusal names the key's path": {
			`  description: d
  runtime: python
  emit: [fn.example.com/gadget]
  call: [fn.example.com/nothing]
  source: "def main(input, host): return {}"
`,
			"data.call: unknown function",
		},
		"a mutations refusal names the key's path": {
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

// A map value is a rule OBJECT, and its merge defaults to atomic.
func TestMappingRuleIsAnObject(t *testing.T) {
	object := loadMapping(t, `    name: {path: name.displayName}
`)
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
		// The retired spelling: a bare path string, which left one property with
		// two shapes.
		"a bare path is refused": {
			`    name: name.displayName
`,
			`"name.displayName" is a bare path — a rule is an object: {path: name.displayName}`,
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

func TestTraitVariantsAreAList(t *testing.T) {
	variants := func(r *vocabulary.Registry) map[string]map[string]vocabulary.Datatype {
		t.Helper()
		g, ok := r.AuthorityByName("c.example.com")
		if !ok {
			t.Fatal("c.example.com missing")
		}
		return g.Traits["temporal"].Variants
	}
	listed := variants(loadTrait(t, `    - name: point
      properties: {at: datetime}
    - name: range
      properties: {at: datetime, endsAt: datetime}
`))
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
		// The retired spelling: a mapping of variant name to properties, which is a
		// keyed map of keyed maps.
		"the variant mapping is refused": {
			`    point: {at: datetime}
    range: {at: datetime, endsAt: datetime}
`,
			"a mapping of variant name to properties — the variants are a LIST",
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

// --- the two bare LISTS the flip retired ----------------------------------------

// An index names its properties and an OAuth feature names its scopes: both used
// to be written as a bare list, and the property dialect cannot state either of
// those shapes — a list of lists, or a keyed map of lists. Each is refused
// naming the object that replaced it; stored rows are translated by the rung.
func TestBareListSpellingsAreRefused(t *testing.T) {
	const indexDoc = `kind: core.substrate.reamde.dev/authority
metadata: {id: i.example.com}
data: {version: v1alpha1}
---
kind: core.substrate.reamde.dev/kind
metadata: {id: i.example.com/widget}
data:
  authority: i.example.com
  names: {singular: widget, plural: widgets}
  indices: [[label, other]]
  properties:
    label: {type: string}
    other: {type: string}
`
	const scopesDoc = `kind: core.substrate.reamde.dev/authority
metadata: {id: s.bundles.example.com}
data: {version: v1alpha1}
---
kind: core.substrate.reamde.dev/trait
metadata: {id: s.bundles.example.com/oauth2}
data:
  authority: s.bundles.example.com
  properties: {clientId: string, clientSecret: secret}
---
kind: core.substrate.reamde.dev/kind
metadata: {id: s.bundles.example.com/config}
data:
  authority: s.bundles.example.com
  names: {singular: config, plural: configs}
  traits: [oauth2]
  properties:
    enabledThing: {type: bool}
---
kind: core.substrate.reamde.dev/bundle
metadata: {id: s.bundles.example.com/s}
data:
  authority: s.bundles.example.com
  description: one config, so the closure is whole
  inputs:
    client: {kind: s.bundles.example.com/config}
  installs:
    - s.bundles.example.com/config
    - s.bundles.example.com/oauth2
  oauth2:
    clientInput: client
    authorizationEndpoint: https://example.com/authorize
    tokenEndpoint: https://example.com/token
    featureScopes:
      enabledThing: [read:thing]
`
	for name, tc := range map[string]struct{ doc, want string }{
		"an index is an object": {
			indexDoc,
			"data.indices: a bare list of property names — an index names them: {properties: [label other]}",
		},
		"a feature's scopes take a field": {
			scopesDoc,
			`featureScopes["enabledThing"]: a bare list of scopes — the toggle's value names them: {scopes: [...]}`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := vocabulary.LoadFS(fstest.MapFS{
				"all.yaml": &fstest.MapFile{Data: []byte(tc.doc)},
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got: %v", tc.want, err)
			}
		})
	}
}
