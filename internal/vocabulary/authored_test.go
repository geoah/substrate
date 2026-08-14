package vocabulary_test

// M5, THE STRUCTURAL HALF: a parsed declaration is a READ-ONLY VIEW of the
// document, and the document is what a row stores. So nothing the parse does may
// reach the map the caller handed it — a canonicalized value, an injected
// default, a sorted list — because the projection serializes that map back into
// the row's properties (engine/vocabularywrite.go authorityDeclarations) and a
// mutation there stores a declaration nobody wrote.

import (
	"reflect"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// authoredStream exercises every declaration kind that carries a value the parse
// could be tempted to rewrite: an enum authored as bare scalars beside labeled
// mappings, a property type refining an enum, an index, a mapping rule, a
// trait's variants, and a function's flat arguments.
const authoredStream = `
kind: core.substrate.reamde.dev/authority
metadata: {id: a.example.com}
data: {version: v1alpha1}
---
kind: core.substrate.reamde.dev/propertytype
metadata: {id: a.example.com/grade}
data:
  authority: a.example.com
  base: enum
  values: [good, bad]
---
kind: core.substrate.reamde.dev/trait
metadata: {id: a.example.com/spanned}
data:
  authority: a.example.com
  oneOf:
    - {name: point, properties: {at: datetime}}
---
kind: core.substrate.reamde.dev/kind
metadata: {id: a.example.com/widget}
data:
  authority: a.example.com
  names: {singular: widget, plural: widgets}
  indices: [{properties: [status]}]
  properties:
    grade: {type: grade}
    status:
      type: enum
      values:
        - {value: draft, label: Draft}
        - live
    stage:
      type: state
      states: [new, done]
      initial: new
      transitions:
        - {from: new, to: done, stamps: {doneAt: now}}
    box:
      type: object
      fields:
        label: {type: string}
---
kind: core.substrate.reamde.dev/kind
metadata: {id: a.example.com/row}
data:
  authority: a.example.com
  names: {singular: row, plural: rows}
  properties:
    label: {type: string}
  edges:
    widget: {to: widget, required: true}
---
kind: core.substrate.reamde.dev/recordmapping
metadata: {id: a.example.com/rowwidget}
data:
  authority: a.example.com
  from: a.example.com/row
  to: a.example.com/widget
  edge: widget
  map:
    status: {path: label}
---
kind: core.substrate.reamde.dev/function
metadata: {id: a.example.com/fn}
data:
  authority: a.example.com
  description: does a thing
  runtime: python
  source: "def main(input, host): return {}"
  arguments:
    - {name: url, type: string, required: true}
  emit: [a.example.com/widget]
`

// TestTheParseNeverMutatesTheDocument is the invariant itself: build the
// authorities from one copy of the stream and compare the documents against a
// second, independent parse of the same text. Any in-place rewrite shows up as a
// difference, whichever key it touched.
func TestTheParseNeverMutatesTheDocument(t *testing.T) {
	built, err := vocabulary.ParseStream([]byte(authoredStream))
	if err != nil {
		t.Fatalf("parse the stream: %v", err)
	}
	if _, err := vocabulary.BuildAuthorities(built, vocabulary.SourceInstalled); err != nil {
		t.Fatalf("build: %v", err)
	}
	untouched, err := vocabulary.ParseStream([]byte(authoredStream))
	if err != nil {
		t.Fatalf("re-parse the stream: %v", err)
	}
	if len(built) != len(untouched) {
		t.Fatalf("the two parses read %d and %d documents", len(built), len(untouched))
	}
	for i := range built {
		if !reflect.DeepEqual(built[i].Data, untouched[i].Data) {
			t.Fatalf("%s %s: the parse mutated the document\n after  %#v\n before %#v",
				built[i].Kind, built[i].ID, built[i].Data, untouched[i].Data)
		}
	}
}

// TestADeclarationIsTheAuthoredMap is the same invariant read from the other end:
// what the projection stores is the document's own data map, key for key.
//
// The ONE exception is a property type's `values`, and it is the meta-kind's own
// shape that demands it: core's `propertytype` declares them as a repeated
// {value, label} object, so a row must hold that whichever way the author wrote
// it. The loader normalizes a COPY (never the document, per the test above), and
// a client's round-trip of the stored form is idempotent.
func TestADeclarationIsTheAuthoredMap(t *testing.T) {
	docs, err := vocabulary.ParseStream([]byte(authoredStream))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	authored := map[string]map[string]any{}
	for _, d := range docs {
		authored[d.ID] = d.Data
	}
	authorities, err := vocabulary.BuildAuthorities(docs, vocabulary.SourceInstalled)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(authorities) != 1 {
		t.Fatalf("built %d authorities", len(authorities))
	}
	g := authorities[0]
	for _, n := range g.KindOrder {
		ty := g.Kinds[n]
		if !reflect.DeepEqual(ty.Definition, authored[ty.Identity]) {
			t.Fatalf("kind %s: the declaration is not the authored map\n parsed   %#v\n authored %#v",
				ty.Identity, ty.Definition, authored[ty.Identity])
		}
	}
	for _, n := range g.TraitOrder {
		c := g.Traits[n]
		if !reflect.DeepEqual(c.Definition, authored[c.Identity()]) {
			t.Fatalf("trait %s: %#v", c.Identity(), c.Definition)
		}
	}
	for _, n := range g.MappingOrder {
		m := g.Mappings[n]
		if !reflect.DeepEqual(m.Definition, authored[m.Identity()]) {
			t.Fatalf("mapping %s: %#v", m.Identity(), m.Definition)
		}
	}
	for _, n := range g.FunctionOrder {
		fn := g.Functions[n]
		if !reflect.DeepEqual(fn.Definition, authored[fn.Identity()]) {
			t.Fatalf("function %s: %#v", fn.Identity(), fn.Definition)
		}
	}
	// A kind's enum values keep the author's spelling: the meta-kind types
	// `properties` as the one json leaf, so there is nothing to normalize for.
	props, _ := g.Kinds["widget"].Definition["properties"].(map[string]any)
	status, _ := props["status"].(map[string]any)
	vals, _ := status["values"].([]any)
	if len(vals) != 2 || vals[1] != "live" {
		t.Fatalf("the kind's enum values were rewritten: %#v", status["values"])
	}
	// The property type's are the exception, and they are objects.
	pt := g.PropertyTypes["grade"]
	ptVals, _ := pt.Definition["values"].([]any)
	if len(ptVals) != 2 {
		t.Fatalf("the property type's values = %#v", pt.Definition["values"])
	}
	first, isMap := ptVals[0].(map[string]any)
	if !isMap || first["value"] != "good" || first["label"] != "" {
		t.Fatalf("a property type's values must be {value, label} objects: %#v", ptVals[0])
	}
	// And normalizing them is idempotent: the stored form parses back to itself.
	again, err := vocabulary.ParseStream([]byte(authoredStream))
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	for i, d := range again {
		if d.ID == "a.example.com/grade" {
			again[i].Data["values"] = ptVals
		}
	}
	round, err := vocabulary.BuildAuthorities(again, vocabulary.SourceInstalled)
	if err != nil {
		t.Fatalf("the stored form does not load: %v", err)
	}
	if !reflect.DeepEqual(round[0].PropertyTypes["grade"].Definition["values"], ptVals) {
		t.Fatalf("the round trip moved the values: %#v", round[0].PropertyTypes["grade"].Definition["values"])
	}
}
