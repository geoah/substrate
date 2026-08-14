package vocabulary_test

// The canonical declaration data: a document written in a spelling that came
// before the typed core parses to the SAME data map as one written in the
// declared spelling. That is what makes the dialect rung a translation rather
// than a rewrite — the engine stores the parsed data map, and the two spellings
// leave the same one behind.

import (
	"reflect"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// canonicalPair is one declaration in two spellings.
type canonicalPair struct {
	name string
	old  string
	want string
}

func TestOldSpellingsLeaveTheDeclaredDataBehind(t *testing.T) {
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
	for _, tc := range []canonicalPair{
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := parseOneAuthority(t, header+"---"+tc.old)
			want := parseOneAuthority(t, header+"---"+tc.want)
			for id, data := range want {
				if !reflect.DeepEqual(got[id], data) {
					t.Fatalf("%s:\n old spelling → %#v\n declared     → %#v", id, got[id], data)
				}
			}
			if len(got) != len(want) {
				t.Fatalf("the two spellings left %d and %d declarations behind", len(got), len(want))
			}
		})
	}
}

// TestAnIndexNamesItsProperties covers the kind meta-declaration's own
// translation: the bare list of property names an index used to be.
func TestAnIndexNamesItsProperties(t *testing.T) {
	const doc = `
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
  indices:
    - - label
      - other
  properties:
    label:
      type: string
    other:
      type: string
`
	data := parseOneAuthority(t, doc)["x.example.com/widget"]
	indices, ok := data["indices"].([]any)
	if !ok || len(indices) != 1 {
		t.Fatalf("indices = %#v", data["indices"])
	}
	entry, ok := indices[0].(map[string]any)
	if !ok {
		t.Fatalf("an index is %T, not the object the declaration holds", indices[0])
	}
	if props, _ := entry["properties"].([]any); len(props) != 2 {
		t.Fatalf("the index names %#v", entry)
	}
}

// TestOAuthFeatureScopesTakeAField covers the bundle's one shape the property
// dialect cannot state as authored: a keyed map of lists.
func TestOAuthFeatureScopesTakeAField(t *testing.T) {
	const doc = `
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
        - read:thing
`
	data := parseOneAuthority(t, doc)["x.bundles.example.com/x"]
	oauth2, _ := data["oauth2"].(map[string]any)
	scopes, _ := oauth2["featureScopes"].(map[string]any)
	entry, ok := scopes["enabledThing"].(map[string]any)
	if !ok {
		t.Fatalf("featureScopes = %#v", scopes)
	}
	if list, _ := entry["scopes"].([]any); len(list) != 1 {
		t.Fatalf("the toggle's scopes are %#v", entry)
	}
}

// parseOneAuthority parses a manifest stream and returns every declaration's
// parsed DATA MAP, keyed by identity — what a declaration row stores.
func parseOneAuthority(t *testing.T, src string) map[string]map[string]any {
	t.Helper()
	authorities, err := vocabulary.ParseYAML([]byte(src), vocabulary.SourceInstalled)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
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
