package engine

// THE DIALECT-1 GRAMMAR'S FIXTURES. One declaration per translated kind, written
// twice: once in the spelling an older binary stored inside its `definition`
// blob, once in the spelling this binary's loader admits. The frozen grammar
// translates the first, the LIVE loader parses the result, and what it leaves
// behind must be the data map the second one parses to — because that map is the
// row the rung writes.
//
// Every fixture asserts the freeze is load-bearing too: the old stream does NOT
// load without the translation. If one of these ever loads bare, the tombstone in
// internal/vocabulary went missing and this file stopped being the last reader of
// the spelling.

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/geoah/substrate/internal/vocabulary"
)

// dialectOnePair is one declaration in the two spellings.
type dialectOnePair struct {
	name string
	old  string
	want string
}

// dialectOneHeader is the closure every fixture declares against — one authority
// and one kind, in the spelling the live loader admits — so the document under
// test is the only dialect-1 thing in the stream.
const dialectOneHeader = `
kind: core.substrate.reamde.dev/authority
metadata:
  id: x.example.com
data: {}
---
kind: core.substrate.reamde.dev/kind
metadata:
  id: x.example.com/widget
data:
  authority: x.example.com
  names:
    singular: widget
    plural: widgets
  properties:
    label:
      type: string
`

func TestDialectOneSpellingsTranslateToTheTypedDeclaration(t *testing.T) {
	const header = dialectOneHeader
	for _, tc := range []dialectOnePair{
		{
			name: "a function's capability wrapper hoists and its schema flattens",
			old: `
kind: core.substrate.reamde.dev/function
metadata:
  id: x.example.com/fn
data:
  authority: x.example.com
  description: does a thing
  runtime: python
  source: |
    def main(input, host):
        return {}
  input:
    type: object
    properties:
      query:
        type: string
        description: what to look for
      limit:
        type: number
      tags:
        type: array
        items:
          type: string
    required: [query]
  output:
    type: object
    properties:
      hits:
        type: array
        items:
          type: object
          properties:
            url:
              type: string
  capabilities:
    emit: [x.example.com/widget]
    network: ["api.example.com"]
    reads:
      kinds: [x.example.com/widget]
`,
			want: `
kind: core.substrate.reamde.dev/function
metadata:
  id: x.example.com/fn
data:
  authority: x.example.com
  description: does a thing
  runtime: python
  source: |
    def main(input, host):
        return {}
  arguments:
    - name: limit
      type: float
    - name: query
      type: string
      required: true
      description: what to look for
    - name: tags
      type: string
      repeated: true
  returns:
    # A list of objects is a REPEATED json: the shape of an element is not this
    # function's to own, and the container still is.
    - name: hits
      type: json
      repeated: true
  emit: [x.example.com/widget]
  network: ["api.example.com"]
  reads:
    kinds: [x.example.com/widget]
`,
		},
		{
			name: "an agent's bare tool strings name their arm",
			old: `
kind: core.substrate.reamde.dev/agent
metadata:
  id: x.example.com/ag
data:
  authority: x.example.com
  description: an agent
  prompt: be useful
  provider: default
  model: gpt-5
  tools: [query, x.example.com/fn]
  emit: [x.example.com/widget]
  reads:
    kinds: [x.example.com/widget]
`,
			want: `
kind: core.substrate.reamde.dev/agent
metadata:
  id: x.example.com/ag
data:
  authority: x.example.com
  description: an agent
  prompt: be useful
  provider: default
  model: gpt-5
  tools:
    - builtin: query
    - callable: x.example.com/fn
  emit: [x.example.com/widget]
  reads:
    kinds: [x.example.com/widget]
`,
		},
		{
			name: "a trait's variant map becomes the variant list",
			old: `
kind: core.substrate.reamde.dev/trait
metadata:
  id: x.example.com/spanned
data:
  authority: x.example.com
  oneOf:
    point:
      at: datetime
    range:
      at: datetime
      endsAt: datetime
`,
			want: `
kind: core.substrate.reamde.dev/trait
metadata:
  id: x.example.com/spanned
data:
  authority: x.example.com
  oneOf:
    - name: point
      properties:
        at: datetime
    - name: range
      properties:
        at: datetime
        endsAt: datetime
`,
		},
		{
			name: "a mapping rule's bare path takes its object",
			old: `
kind: core.substrate.reamde.dev/recordmapping
metadata:
  id: x.example.com/widgetwidget
data:
  authority: x.example.com
  from: x.example.com/widget
  to: x.example.com/widget
  edge: twin
  map:
    label: label
`,
			want: `
kind: core.substrate.reamde.dev/recordmapping
metadata:
  id: x.example.com/widgetwidget
data:
  authority: x.example.com
  from: x.example.com/widget
  to: x.example.com/widget
  edge: twin
  map:
    label:
      path: label
`,
		},
		{
			name: "an index names its properties",
			old: `
kind: core.substrate.reamde.dev/kind
metadata:
  id: x.example.com/gadget
data:
  authority: x.example.com
  names:
    singular: gadget
    plural: gadgets
  indices:
    - - label
      - other
  properties:
    label:
      type: string
    other:
      type: string
`,
			want: `
kind: core.substrate.reamde.dev/kind
metadata:
  id: x.example.com/gadget
data:
  authority: x.example.com
  names:
    singular: gadget
    plural: gadgets
  indices:
    - properties:
        - label
        - other
  properties:
    label:
      type: string
    other:
      type: string
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := translatedDeclarations(t, header+"---"+tc.old)
			want := parsedDeclarations(t, header+"---"+tc.want)
			for id, data := range want {
				if !reflect.DeepEqual(got[id], data) {
					t.Fatalf("%s:\n dialect 1 → %#v\n declared  → %#v", id, got[id], data)
				}
			}
			if len(got) != len(want) {
				t.Fatalf("the two spellings left %d and %d declarations behind", len(got), len(want))
			}
			// The freeze is load-bearing: the live loader refuses the stored
			// spelling, so nothing but this package translates it.
			if _, err := vocabulary.ParseYAML([]byte(header+"---"+tc.old), vocabulary.SourceInstalled); err == nil {
				t.Fatal("the dialect-1 spelling still loads bare — the loader's tombstone is gone")
			}
		})
	}
}

// TestDialectOneBundleFeatureScopesTakeAField is the bundle's own translation: a
// keyed map of LISTS is the one shape the property dialect cannot state, so the
// value took a field. It stands apart because a bundle needs its whole owned
// closure around it.
func TestDialectOneBundleFeatureScopesTakeAField(t *testing.T) {
	const closure = `
kind: core.substrate.reamde.dev/authority
metadata:
  id: x.bundles.example.com
data: {}
---
kind: core.substrate.reamde.dev/kind
metadata:
  id: x.bundles.example.com/config
data:
  authority: x.bundles.example.com
  names:
    singular: config
    plural: configs
  traits: [oauth2]
  properties:
    enabledThing:
      type: bool
---
kind: core.substrate.reamde.dev/trait
metadata:
  id: x.bundles.example.com/oauth2
data:
  authority: x.bundles.example.com
  properties:
    clientId: string
    clientSecret: secret
---
kind: core.substrate.reamde.dev/bundle
metadata:
  id: x.bundles.example.com/x
data:
  authority: x.bundles.example.com
  inputs:
    client:
      kind: x.bundles.example.com/config
  installs:
    - x.bundles.example.com/config
    - x.bundles.example.com/oauth2
  oauth2:
    clientInput: client
    authorizationEndpoint: https://example.com/authorize
    tokenEndpoint: https://example.com/token
    featureScopes:
      enabledThing:
`
	got := translatedDeclarations(t, closure+"        - read:thing\n")
	want := parsedDeclarations(t, closure+"        scopes:\n          - read:thing\n")
	const id = "x.bundles.example.com/x"
	if !reflect.DeepEqual(got[id], want[id]) {
		t.Fatalf("%s:\n dialect 1 → %#v\n declared  → %#v", id, got[id], want[id])
	}
	if _, err := vocabulary.ParseYAML([]byte(closure+"        - read:thing\n"), vocabulary.SourceInstalled); err == nil {
		t.Fatal("the bare scope list still loads — the loader's tombstone is gone")
	}
}

// storedFunction is one stored function's blob, as an older binary wrote it.
func storedFunction(t *testing.T, body string) map[string]any {
	t.Helper()
	return documents(t, `
kind: core.substrate.reamde.dev/function
metadata:
  id: x.example.com/fn
data:
  authority: x.example.com
  description: does a thing
  runtime: python
  source: |
    def main(input, host):
        return {}
  emit: [x.example.com/widget]
`+body)[0].Data
}

// TestDialectOneIOThatWouldMoveTheWireShapeRefuses is the migration's own safety
// rule: a dialect-1 IO schema translates only when the flat list keeps the shape
// a CALLER sends. Where it cannot, the rung fails loudly instead of stamping a
// store whose callables answer a contract nobody agreed to — a wrapper the
// grammar invented, or an argument it dropped.
func TestDialectOneIOThatWouldMoveTheWireShapeRefuses(t *testing.T) {
	for name, tc := range map[string]struct{ body, want string }{
		"a bare-value input": {
			`  input:
    type: string
`,
			`data.input declares type "string", not an object of named arguments`,
		},
		"a bare-value output": {
			`  output:
    type: array
    items: {type: string}
`,
			`data.output declares type "array", not an object of named arguments`,
		},
		"an object open to any argument": {
			`  input:
    type: object
`,
			"data.input declares an object with no properties, which admitted ANY argument",
		},
		"an argument name the flat list cannot hold": {
			`  input:
    type: object
    properties:
      page_url: {type: string}
`,
			`data.input.properties has an argument named "page_url"`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := dialectOneData(vocabulary.DocFunction, storedFunction(t, tc.body))
			if err == nil {
				t.Fatal("the grammar fabricated a translation for a shape that has none")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got: %v", tc.want, err)
			}
			// The fix the operator has to make is in the message.
			if !strings.Contains(err.Error(), "re-declare it with") {
				t.Fatalf("the refusal must name the flat spelling: %v", err)
			}
		})
	}
}

// TestDialectOneIOThatKeepsTheWireShapeTranslates is the other side of that rule.
// A NESTED object degrades to `json`: the container and the argument name survive,
// only the validation of what is inside weakens, and a caller sends exactly what
// it sent before. `{type: any}` constrained nothing, and neither does an absent
// side, so the exact translation is to say nothing at all.
func TestDialectOneIOThatKeepsTheWireShapeTranslates(t *testing.T) {
	data, err := dialectOneData(vocabulary.DocFunction, storedFunction(t, `  input:
    type: object
    properties:
      opts:
        type: object
        properties:
          depth: {type: number}
      pages:
        type: array
        items:
          type: object
          properties:
            url: {type: string}
    required: [opts]
  output:
    type: any
`))
	if err != nil {
		t.Fatalf("a translatable shape refused: %v", err)
	}
	want := []any{
		map[string]any{"name": "opts", "type": "json", "required": true},
		map[string]any{"name": "pages", "type": "json", "repeated": true},
	}
	if !reflect.DeepEqual(data["arguments"], want) {
		t.Fatalf("arguments translated to\n %#v\nwant\n %#v", data["arguments"], want)
	}
	for _, gone := range []string{"input", "output", "returns"} {
		if _, held := data[gone]; held {
			t.Fatalf("the translation left %q behind: %#v", gone, data)
		}
	}
}

// TestDialectOneStoredProseWeakensToTheLiveBound is the one weakening the
// grammar performs. Dialect 1 passed an `input:` schema through without parsing
// the prose inside it, so a stored description may be multi-line and far past the
// live bound; the live parse REFUSES both, and refusing here would fail the whole
// repository's open over a comment. The prose is collapsed and truncated instead
// — and the argument a caller sends is untouched.
func TestDialectOneStoredProseWeakensToTheLiveBound(t *testing.T) {
	// 309 runes over ten lines: both refusals of the live parse at once.
	prose := strings.TrimSuffix(strings.Repeat("what the caller is looking for\n", 10), "\n")
	data, err := dialectOneData(vocabulary.DocFunction, storedFunction(t, `  input:
    type: object
    properties:
      query:
        type: string
        description: |
`+indented(prose)+`
    required: [query]
`))
	if err != nil {
		t.Fatalf("a stored description refused a translation: %v", err)
	}
	args, _ := data["arguments"].([]any)
	if len(args) != 1 {
		t.Fatalf("arguments translated to %#v", data["arguments"])
	}
	arg, _ := args[0].(map[string]any)
	desc, _ := arg["description"].(string)
	switch {
	case strings.ContainsAny(desc, "\n\r"):
		t.Fatalf("the weakened description is still multi-line: %q", desc)
	case utf8.RuneCountInString(desc) != dialectOneMaxDescription:
		t.Fatalf("the weakened description is %d runes, want the bound %d: %q",
			utf8.RuneCountInString(desc), dialectOneMaxDescription, desc)
	case !strings.HasPrefix(desc, "what the caller is looking for what the caller"):
		t.Fatalf("the weakening lost the prose it keeps: %q", desc)
	}
	if arg["name"] != "query" || arg["type"] != vocabulary.ArgumentString || arg["required"] != true {
		t.Fatalf("the weakening moved the argument: %#v", arg)
	}
	// THE LIVE LOADER IS WHAT THE BOUND BELONGS TO: the weakened value has to
	// pass its parse, which is what pins the frozen bound to the live one — a live
	// bound that narrows fails here rather than at somebody's open.
	if _, err := vocabulary.BuildAuthorities(append(documents(t, dialectOneHeader),
		vocabulary.Document{Kind: vocabulary.DocFunction, ID: "x.example.com/fn", Data: data}),
		vocabulary.SourceInstalled); err != nil {
		t.Fatalf("the live loader refuses the weakened description: %v", err)
	}
}

// indented indents a block for a YAML literal scalar, four spaces past the key's
// own six, so the fixture above can hold real newlines.
func indented(s string) string {
	return "          " + strings.ReplaceAll(s, "\n", "\n          ")
}

// TestDialectOneTraitVariantNameThatMovesABindingRefuses is the trait's own
// safety rule. Dialect 1 validated a variant's PROPERTY names and never the
// variant's own, so a store may hold `oneOf: {point_in_time: …}`, which the typed
// variant list cannot hold. It has no translation: a kind binds a variant BY NAME
// (`traits: [temporal(point)]`) and its records store the binding, so re-spelling
// it would re-point every one of them. The rung refuses by name, with the fix in
// the message, instead of handing the loader an entry it rejects.
func TestDialectOneTraitVariantNameThatMovesABindingRefuses(t *testing.T) {
	docs := documents(t, `
kind: core.substrate.reamde.dev/trait
metadata:
  id: x.example.com/spanned
data:
  authority: x.example.com
  oneOf:
    point:
      at: datetime
    point_in_time:
      at: datetime
`)
	data, err := dialectOneData(vocabulary.DocTrait, docs[0].Data)
	if err == nil {
		t.Fatalf("the grammar fabricated a variant name the loader refuses: %#v", data)
	}
	for _, want := range []string{`"point_in_time"`, "re-declare the trait"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal must name the variant and the fix (%q missing): %v", want, err)
		}
	}
	// The refusal is WHOLE: a translation that kept the variants it could would
	// unbind the records naming the one it dropped, so `oneOf` is left as stored.
	if _, isList := docs[0].Data["oneOf"].([]any); isList {
		t.Fatalf("the refusal half-translated the variants: %#v", docs[0].Data["oneOf"])
	}
	// The names the list CAN hold still translate: the point/range case in the
	// fixture table above is that same trait's real variants.
	if _, err := dialectOneData(vocabulary.DocTrait, documents(t, `
kind: core.substrate.reamde.dev/trait
metadata:
  id: x.example.com/spanned
data:
  authority: x.example.com
  oneOf:
    point:
      at: datetime
`)[0].Data); err != nil {
		t.Fatalf("a translatable variant name refused: %v", err)
	}
}

// TestDialectOnePropertyTypeValuesTakeTheirLabels is the refinement's fixture,
// and the only one whose stored spelling the live loader still ADMITS — an author
// may write `values: [warm, cool]` today too. That is exactly why the translation
// is frozen HERE: core's `propertytype` declares `values` as a repeated OBJECT, so
// the row a dialect-1 blob migrates into cannot hold a bare scalar, and the shape
// it takes may not depend on a normalization the live declaration owns and could
// move with.
func TestDialectOnePropertyTypeValuesTakeTheirLabels(t *testing.T) {
	const refinement = `---
kind: core.substrate.reamde.dev/propertytype
metadata:
  id: x.example.com/shade
data:
  authority: x.example.com
  base: enum
  values: [warm, cool]
---
kind: core.substrate.reamde.dev/kind
metadata:
  id: x.example.com/swatch
data:
  authority: x.example.com
  names:
    singular: swatch
    plural: swatches
  properties:
    shade:
      type: shade
`
	docs := documents(t, dialectOneHeader+refinement)
	data, err := dialectOneData(vocabulary.DocPropertyType, docs[2].Data)
	if err != nil {
		t.Fatalf("translating a refinement refused: %v", err)
	}
	// What enumValuesToAny renders for a bare scalar: both keys, no label, in the
	// order the author declared them.
	want := []any{
		map[string]any{"value": "warm", "label": ""},
		map[string]any{"value": "cool", "label": ""},
	}
	if !reflect.DeepEqual(data["values"], want) {
		t.Fatalf("values translated to\n %#v\nwant\n %#v", data["values"], want)
	}
	twice, err := dialectOneData(vocabulary.DocPropertyType, data)
	if err != nil || !reflect.DeepEqual(twice["values"], want) {
		t.Fatalf("translating twice moved the values: %#v (%v)", twice["values"], err)
	}
	// And the live loader takes what came out, with the value set intact.
	if got := translatedDeclarations(t, dialectOneHeader+refinement)["x.example.com/shade"]; !reflect.DeepEqual(got["values"], want) {
		t.Fatalf("the loaded refinement holds\n %#v\nwant\n %#v", got["values"], want)
	}
}

// TestDialectOneTranslationIsIdempotent pins what the rung leans on for a
// half-migrated store: a declaration already in the typed spelling is its own
// translation, so the grammar may run over any row.
func TestDialectOneTranslationIsIdempotent(t *testing.T) {
	docs := documents(t, `
kind: core.substrate.reamde.dev/agent
metadata:
  id: x.example.com/ag
data:
  authority: x.example.com
  tools:
    - builtin: query
    - callable: x.example.com/fn
  indices: []
`)
	once, err := dialectOneData(vocabulary.DocAgent, docs[0].Data)
	if err != nil {
		t.Fatalf("translating a typed declaration refused: %v", err)
	}
	twice, err := dialectOneData(vocabulary.DocAgent, once)
	if err != nil {
		t.Fatalf("translating twice refused: %v", err)
	}
	if !reflect.DeepEqual(once, twice) {
		t.Fatalf("translating twice moved the data:\n once %#v\n twice %#v", once, twice)
	}
	tools, _ := once["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("tools = %#v", once["tools"])
	}
	for _, tv := range tools {
		if _, isEntry := tv.(map[string]any); !isEntry {
			t.Fatalf("a tool entry is %T", tv)
		}
	}
}

// TestDeclarationBlobRowIsRefusedOutsideTheRung is the bolt on the back: after the
// rung no live row carries a blob, so a reader that meets one says so by name
// instead of rebuilding an authority around a declaration nothing reads.
func TestDeclarationBlobRowIsRefusedOutsideTheRung(t *testing.T) {
	props := map[string]any{
		"authority":         "x.example.com",
		propDeclarationBlob: map[string]any{"authority": "x.example.com", "description": "does a thing"},
		"version":           "v1alpha1",
	}
	if _, _, err := rowDocument("x.example.com/fn", kindFunction, props, typedRows); err == nil {
		t.Fatal("a definition-bearing row was read as a declaration")
	} else if !strings.Contains(err.Error(), propDeclarationBlob) {
		t.Fatalf("the refusal must name the blob: %v", err)
	}
	d, ok, err := rowDocument("x.example.com/fn", kindFunction, props, dialectOneRows)
	if err != nil || !ok {
		t.Fatalf("the rung's read refused the blob: %v", err)
	}
	if d.Data["description"] != "does a thing" {
		t.Fatalf("the rung's read lost the blob's content: %#v", d.Data)
	}
}

// TestDeclarationBlobIsRefusedByPresence closes the guard's bypasses: it asks
// whether the KEY is there, not whether its value is the map dialect 1 stored, and
// it asks for every schema kind rather than only the ones with a blob-bearing arm.
// A null, a string or a list under that key is not a declaration either, and
// reading the row's typed properties around one would rebuild an authority from
// half a declaration.
func TestDeclarationBlobIsRefusedByPresence(t *testing.T) {
	for name, tc := range map[string]struct {
		kind  string
		props map[string]any
		want  string
	}{
		"a null blob on a function": {
			kindFunction,
			map[string]any{"authority": "x.example.com", "description": "d", propDeclarationBlob: nil},
			"carries a `definition` property",
		},
		"a string blob on a kind": {
			kindKind,
			map[string]any{"authority": "x.example.com", propDeclarationBlob: "{}"},
			"carries a `definition` property",
		},
		"a list blob on an agent": {
			kindAgent,
			map[string]any{"authority": "x.example.com", propDeclarationBlob: []any{"a"}},
			"carries a `definition` property",
		},
		"a blob on an authority row": {
			kindAuthority,
			map[string]any{"version": "v1alpha1", propDeclarationBlob: map[string]any{"version": "v1alpha1"}},
			"carries a `definition` property",
		},
		"a blob on an actor row": {
			kindActor,
			map[string]any{"authority": "x.example.com", propDeclarationBlob: map[string]any{}},
			"carries a `definition` property",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := rowDocument("x.example.com/thing", tc.kind, tc.props, typedRows)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("a live read admitted it: %v", err)
			}
			if !errors.Is(err, ErrDeclarationUntranslated) {
				t.Fatalf("the refusal must be by name: %v", err)
			}
			// The RUNG refuses these too: none of them is a translation it can do,
			// and stamping the dialect over one would call the store typed.
			if _, _, err := rowDocument("x.example.com/thing", tc.kind, tc.props, dialectOneRows); err == nil {
				t.Fatal("the rung translated a blob that is not a declaration map")
			}
		})
	}
}

// documents parses a manifest stream into documents, envelope-validated.
func documents(t *testing.T, src string) []vocabulary.Document {
	t.Helper()
	docs, err := vocabulary.ParseStream([]byte(src))
	if err != nil {
		t.Fatalf("parse the stream: %v", err)
	}
	return docs
}

// parsedDeclarations parses a manifest stream and returns every declaration's
// parsed DATA MAP, keyed by identity — what a declaration row stores.
func parsedDeclarations(t *testing.T, src string) map[string]map[string]any {
	t.Helper()
	authorities, err := vocabulary.ParseYAML([]byte(src), vocabulary.SourceInstalled)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return declarationDataMaps(authorities)
}

// translatedDeclarations is the RUNG'S pipeline over a stored stream: every
// document's data map through the frozen dialect-1 grammar, then the live loader,
// then the data map each declaration leaves behind.
func translatedDeclarations(t *testing.T, src string) map[string]map[string]any {
	t.Helper()
	docs := documents(t, src)
	for i, d := range docs {
		data, err := dialectOneData(d.Kind, d.Data)
		if err != nil {
			t.Fatalf("%s %s: the grammar refused a translation: %v", d.Kind, d.ID, err)
		}
		docs[i].Data = data
	}
	authorities, err := vocabulary.BuildAuthorities(docs, vocabulary.SourceInstalled)
	if err != nil {
		t.Fatalf("the translated stream does not load: %v", err)
	}
	return declarationDataMaps(authorities)
}

func declarationDataMaps(authorities []*vocabulary.Authority) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, g := range authorities {
		for _, n := range g.KindOrder {
			out[g.Kinds[n].Identity] = g.Kinds[n].Definition
		}
		for _, n := range g.TraitOrder {
			out[g.Traits[n].Identity()] = g.Traits[n].Definition
		}
		for _, n := range g.DatatypeOrder {
			out[g.PropertyTypes[n].Identity()] = g.PropertyTypes[n].Definition
		}
		for _, n := range g.MappingOrder {
			out[g.Mappings[n].Identity()] = g.Mappings[n].Definition
		}
		for _, n := range g.FunctionOrder {
			out[g.Functions[n].Identity()] = g.Functions[n].Definition
		}
		for _, n := range g.AgentOrder {
			out[g.Agents[n].Identity()] = g.Agents[n].Definition
		}
		if g.Bundle != nil {
			out[g.Bundle.Identity()] = g.Bundle.Definition
		}
	}
	return out
}
