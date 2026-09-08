package vocabulary_test

// The `retired:` block (decision 0053): a package header's spent kind names
// and a kind's spent property names, enum values and states. The loader holds
// the block's shape and the one contradiction a single document can carry, a
// name both retired and declared. What the block binds across documents (a
// later declaration of a retired name, a list that shrinks) is the engine's,
// tested in internal/engine/retirement_db_test.go.

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/vocabulary"
)

// mkRetiredPackage builds one package whose header carries the given
// `retired:` block beside one declared kind.
func mkRetiredPackage(retired string) fstest.MapFS {
	return fstest.MapFS{"g.yaml": &fstest.MapFile{Data: []byte(`kind: substrate.reamde.dev/core/package
metadata: {id: g.example.com/g}
data:
  authority: g.example.com
  package: g
  version: 1
` + retired + `---
kind: substrate.reamde.dev/core/kind
metadata: {id: g.example.com/g/thing}
data:
  authority: g.example.com
  package: g
  names: {singular: thing}
  properties:
    label: {type: string}
`)}}
}

func TestRetiredReserved(t *testing.T) {
	t.Run("a package retires kind names", func(t *testing.T) {
		r, err := vocabulary.LoadFS(mkRetiredPackage("  retired:\n    kinds: [widget, gadget]\n"))
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		g, ok := r.PackageByName("g.example.com/g")
		if !ok {
			t.Fatal("package did not load")
		}
		if got := g.RetiredKinds; !reflect.DeepEqual(got, []string{"widget", "gadget"}) {
			t.Fatalf("RetiredKinds = %v", got)
		}
	})

	t.Run("a package refuses a retired name it declares", func(t *testing.T) {
		_, err := vocabulary.LoadFS(mkRetiredPackage("  retired:\n    kinds: [thing]\n"))
		wantContains(t, err, "kind g.example.com/g/thing is retired and declared; a retired name is never declared again")
	})

	t.Run("a package's block holds its shape", func(t *testing.T) {
		for body, want := range map[string]string{
			"  retired: [thing]\n":                      "data.retired: a block of retired names",
			"  retired:\n    kinds: widget\n":           "a list of names",
			"  retired:\n    kinds: [Widget]\n":         `"Widget" must be one lowercase word`,
			"  retired:\n    kinds: [widget, widget]\n": `"widget" is listed twice`,
			"  retired:\n    traits: [x]\n":             `unknown key "traits"`,
		} {
			_, err := vocabulary.LoadFS(mkRetiredPackage(body))
			wantContains(t, err, want)
		}
	})

	t.Run("a kind retires properties, values and states", func(t *testing.T) {
		ty := loadThing(t, `  properties:
    label: {type: string}
    level: {type: enum, values: [high]}
    phase:
      type: state
      states: [open, done]
      initial: open
      transitions:
        - {from: open, to: done}
  retired:
    properties: [size, weight]
    values:
      level: [low]
      flavor: [salty]
    states:
      phase: [archived]
`)
		want := vocabulary.KindRetirement{
			Properties: []string{"size", "weight"},
			Values:     map[string][]string{"level": {"low"}, "flavor": {"salty"}},
			States:     map[string][]string{"phase": {"archived"}},
		}
		if !reflect.DeepEqual(ty.Retired, want) {
			t.Fatalf("Retired = %+v, want %+v", ty.Retired, want)
		}
		// The block rides the Definition map as authored, which is what the
		// projection stores and the rebuild reads back.
		if _, held := ty.Definition["retired"]; !held {
			t.Fatal("the Definition map dropped `retired`")
		}
	})

	t.Run("a kind refuses a retired name it declares", func(t *testing.T) {
		loadThingErr(t, `  properties:
    size: {type: string}
  retired:
    properties: [size]
`, `property "size" is retired and declared; a retired name is never declared again`)
		loadThingErr(t, `  properties:
    level: {type: enum, values: [low, high]}
  retired:
    values:
      level: [low]
`, `"low" is retired and declared; a retired name is never declared again`)
		loadThingErr(t, `  properties:
    phase:
      type: state
      states: [open, done]
      initial: open
      transitions:
        - {from: open, to: done}
  retired:
    states:
      phase: [done]
`, `"done" is retired and declared; a retired name is never declared again`)
	})

	t.Run("a kind's block holds its shape", func(t *testing.T) {
		for body, want := range map[string]string{
			"  retired: [size]\n":                                "data.retired: a block of retired names",
			"  retired:\n    properties: size\n":                 "a list of names",
			"  retired:\n    properties: [Size]\n":               `"Size" must be camelCase`,
			"  retired:\n    properties: [size, size]\n":         `"size" is listed twice`,
			"  retired:\n    properties: [title]\n":              `"title" is a built-in property`,
			"  retired:\n    fields: [x]\n":                      `unknown key "fields"`,
			"  retired:\n    values: [low]\n":                    "a map of property name to a list of names",
			"  retired:\n    values:\n      level: low\n":        "a list of names",
			"  retired:\n    values:\n      level: [Low]\n":      `"Low" must be a lowercase word`,
			"  retired:\n    values:\n      level: [low, low]\n": `"low" is listed twice`,
			"  retired:\n    values:\n      label: [low]\n":      "not an enum property; only an enum retires values",
			"  retired:\n    states:\n      label: [done]\n":     "not a state property; only a state property retires states",
			"  retired:\n    values:\n      Level: [low]\n":      `property "Level" must be camelCase`,
		} {
			loadThingErr(t, "  properties:\n    label: {type: string}\n"+body, want)
		}
	})

	t.Run("an entry needs no live subject", func(t *testing.T) {
		// A retired property may itself be gone, and a value list may name a
		// property the kind no longer declares: the reservation outlives its
		// subject by design.
		ty := loadThing(t, `  properties:
    label: {type: string}
  retired:
    properties: [size]
    values:
      level: [low]
    states:
      phase: [archived]
`)
		if ty.Retired.Empty() {
			t.Fatal("the block parsed as empty")
		}
	})
}

func wantContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want one containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want one containing %q", err, want)
	}
}
