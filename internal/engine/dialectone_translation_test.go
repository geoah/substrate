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
	"reflect"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// dialectOnePair is one declaration in the two spellings.
type dialectOnePair struct {
	name string
	old  string
	want string
}

func TestDialectOneSpellingsTranslateToTheTypedDeclaration(t *testing.T) {
	const header = `
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
	before := docs[0].Data
	once := dialectOneData(vocabulary.DocAgent, before)
	twice := dialectOneData(vocabulary.DocAgent, once)
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
		docs[i].Data = dialectOneData(d.Kind, d.Data)
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
