package vocabulary_test

// The reserved dialect keys (issue 110): `unique`, `deprecated` and an edge's
// `properties` block. Each is admitted, validated and stored, and NONE of them
// changes a write: no index polices `unique`, no path reads `deprecated`, and
// an edge write still carries whatever props it likes.
//
// They are reserved rather than implemented because a declaration key set is
// CLOSED: an unknown key quarantines the authority that ships it, and fails
// the open for core, so every added key is a coordinated upgrade of every
// binary that might read the closure. Landing the keys inert costs one such
// event and buys the implementations after it none.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/vocabulary"
)

// mkThing builds one authority with one kind, its `data:` body written by the
// caller, so a case reads as the declaration it is about.
func mkThing(body string) fstest.MapFS {
	return fstest.MapFS{"g.yaml": &fstest.MapFile{Data: []byte(`kind: core.substrate.reamde.dev/authority
metadata: {id: g.example.com}
data: {version: 1}
---
kind: core.substrate.reamde.dev/kind
metadata: {id: g.example.com/thing}
data:
  authority: g.example.com
  names: {singular: thing, plural: things}
` + body)}}
}

func loadThing(t *testing.T, body string) *vocabulary.Kind {
	t.Helper()
	r, err := vocabulary.LoadFS(mkThing(body))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	ty, ok := r.ByIdentity("g.example.com/thing")
	if !ok {
		t.Fatal("g.example.com/thing did not load")
	}
	return ty
}

func loadThingErr(t *testing.T, body, want string) {
	t.Helper()
	_, err := vocabulary.LoadFS(mkThing(body))
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want one containing %q", err, want)
	}
}

// --- unique ----------------------------------------------------------------

func TestUniqueReserved(t *testing.T) {
	t.Run("admitted, parsed and stored", func(t *testing.T) {
		ty := loadThing(t, `  properties:
    email: {type: email, unique: true}
    owner: {type: reference, kind: any, unique: true}
`)
		if !ty.Props["email"].Unique || !ty.Props["owner"].Unique {
			t.Fatalf("unique not parsed: email=%v owner=%v", ty.Props["email"].Unique, ty.Props["owner"].Unique)
		}
		// Stored: the definition map is what the projection persists, so a
		// reservation that did not ride into it would be lost on restart.
		props, _ := ty.Definition["properties"].(map[string]any)
		email, _ := props["email"].(map[string]any)
		if got, _ := email["unique"].(bool); !got {
			t.Fatalf("definition unique = %v", email["unique"])
		}
	})

	t.Run("nothing enforces it", func(t *testing.T) {
		// The marker is inert by construction: it is not on any of the
		// containers the write path reads, so this test is the whole statement
		// of that. When enforcement lands it lands with its own tests.
		ty := loadThing(t, `  properties:
    email: {type: email, unique: true}
`)
		if len(ty.Indices) != 0 {
			t.Fatalf("unique asked for an index: %+v", ty.Indices)
		}
	})

	t.Run("a list or a map is an error", func(t *testing.T) {
		loadThingErr(t, `  properties:
    tags: {type: string, repeated: true, unique: true}
`, "one value per record")
		loadThingErr(t, `  properties:
    labels: {type: string, keyed: true, unique: true}
`, "one value per record")
	})

	t.Run("a datatype without a comparable value is an error", func(t *testing.T) {
		loadThingErr(t, `  properties:
    blob: {type: json, unique: true}
`, "there is no value to hold unique")
		loadThingErr(t, `  properties:
    key: {type: secret, unique: true}
`, "two writes of one value are two different stored values")
		loadThingErr(t, `  properties:
    spec: {type: object, unique: true, fields: {a: {type: string}}}
`, `unknown key "unique"`)
	})

	t.Run("a field is an error", func(t *testing.T) {
		loadThingErr(t, `  properties:
    spec: {type: object, fields: {code: {type: string, unique: true}}}
`, "not a field")
	})

	// The three reserved markers are read by one helper on both branches, so a
	// reference gets renamedFrom's checks exactly once and exactly as a scalar
	// does.
	t.Run("a reference reads the same markers, once", func(t *testing.T) {
		ty := loadThing(t, `  properties:
    owner: {type: reference, kind: any, renamedFrom: holder}
`)
		if got := ty.Props["owner"].RenamedFrom; got != "holder" {
			t.Fatalf("RenamedFrom = %q", got)
		}
		_, err := vocabulary.LoadFS(mkThing(`  properties:
    owner: {type: reference, kind: any, renamedFrom: owner}
`))
		if err == nil {
			t.Fatal("renamedFrom naming the property itself must refuse")
		}
		if n := strings.Count(err.Error(), "names the property itself"); n != 1 {
			t.Fatalf("the problem is reported %d times: %v", n, err)
		}
	})
}

// --- deprecated ------------------------------------------------------------

func TestDeprecatedReserved(t *testing.T) {
	t.Run("admitted on every declaration a client offers", func(t *testing.T) {
		ty := loadThing(t, `  properties:
    size: {type: string, deprecated: true}
    spec: {type: object, deprecated: true, fields: {a: {type: string}}}
    owner: {type: reference, kind: any, deprecated: true}
    status:
      type: state
      deprecated: true
      states: [open, done]
      transitions:
        - {from: open, to: done}
    flavor:
      type: enum
      values:
        - value: sweet
        - {value: salty, deprecated: true}
  edges:
    author: {to: any, deprecated: true}
`)
		for _, name := range []string{"size", "spec", "owner", "status"} {
			if !ty.Props[name].Deprecated {
				t.Errorf("%s: deprecated not parsed", name)
			}
		}
		values := ty.Props["flavor"].Values
		if len(values) != 2 || values[0].Deprecated || !values[1].Deprecated {
			t.Errorf("enum values: %+v", values)
		}
		if !ty.Edges["author"].Deprecated {
			t.Error("edge: deprecated not parsed")
		}
		props, _ := ty.Definition["properties"].(map[string]any)
		size, _ := props["size"].(map[string]any)
		if got, _ := size["deprecated"].(bool); !got {
			t.Errorf("definition deprecated = %v", size["deprecated"])
		}
	})

	t.Run("deprecated and required together is an error", func(t *testing.T) {
		loadThingErr(t, `  properties:
    size: {type: string, deprecated: true, required: true}
`, "deprecated or required, never both")
		loadThingErr(t, `  properties:
    owner: {type: reference, kind: any, deprecated: true, required: true}
`, "deprecated or required, never both")
		loadThingErr(t, `  properties:
    label: {type: string}
  edges:
    author: {to: any, deprecated: true, required: true}
`, "deprecated or required, never both")
	})
}

// --- edges.<rel>.properties ------------------------------------------------

func TestEdgePropertiesReserved(t *testing.T) {
	t.Run("admitted, parsed and stored", func(t *testing.T) {
		ty := loadThing(t, `  properties:
    label: {type: string}
  edges:
    author:
      to: any
      properties:
        order: {type: int, description: where this one sits in the list}
        role:
          type: enum
          values:
            - {value: writer}
            - {value: editor}
        since: {type: datetime, deprecated: true}
`)
		e := ty.Edges["author"]
		if got, want := e.PropOrder, []string{"order", "role", "since"}; strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("PropOrder = %v, want %v", got, want)
		}
		if got := e.Props["order"].Datatype; got != vocabulary.DatatypeInt {
			t.Errorf("order datatype = %q", got)
		}
		if got := e.Props["role"].ValueStrings(); strings.Join(got, ",") != "writer,editor" {
			t.Errorf("role values = %v", got)
		}
		if !e.Props["since"].Deprecated {
			t.Error("since: deprecated not parsed")
		}
		edges, _ := ty.Definition["edges"].(map[string]any)
		author, _ := edges["author"].(map[string]any)
		if _, stored := author["properties"]; !stored {
			t.Errorf("definition edge = %v", author)
		}
	})

	t.Run("a bare-word value is an error, and the mapping is stored verbatim", func(t *testing.T) {
		// core's `kind` holds an edge property's values as {value, label}
		// objects. Admitting the bare word a record property takes would mean
		// rewriting the author's block on its way into the row, so it is
		// refused by name instead and the stored block is what was written.
		loadThingErr(t, `  properties:
    label: {type: string}
  edges:
    author:
      to: any
      properties:
        role:
          type: enum
          values: [writer, editor]
`, "never a bare word")

		ty := loadThing(t, `  properties:
    label: {type: string}
  edges:
    author:
      to: any
      properties:
        role:
          type: enum
          values:
            - {value: writer}
            - {value: editor, label: Editor, deprecated: true}
`)
		if got := ty.Edges["author"].Props["role"].ValueStrings(); strings.Join(got, ",") != "writer,editor" {
			t.Fatalf("role values = %v", got)
		}
		edges, _ := ty.Definition["edges"].(map[string]any)
		author, _ := edges["author"].(map[string]any)
		props, _ := author["properties"].(map[string]any)
		role, _ := props["role"].(map[string]any)
		values, _ := role["values"].([]any)
		first, _ := values[0].(map[string]any)
		if len(values) != 2 || len(first) != 1 {
			// Verbatim: one authored key in, one key stored. A rewrite here is
			// what makes a re-apply of the same document look like a change.
			t.Fatalf("stored values = %v", role["values"])
		}
	})

	t.Run("a bare datatype is an error", func(t *testing.T) {
		// The `fields:` shorthand does not reach here: the stored row holds
		// each edge property as a mapping.
		loadThingErr(t, `  properties:
    label: {type: string}
  edges:
    author:
      to: any
      properties:
        order: int
`, "an edge property is a mapping")
	})

	t.Run("nothing validates an edge write against it", func(t *testing.T) {
		// The reservation's whole point: the block is a declaration and not yet
		// a contract (issue 111). Held here as the loader's own statement; the
		// write path is tested where it lives.
		ty := loadThing(t, `  properties:
    label: {type: string}
  edges:
    author:
      to: any
      properties:
        order: {type: int}
`)
		if ty.Edges["author"].Props["order"].Managed {
			t.Error("an edge property claimed the engine stamps it")
		}
	})

	t.Run("a container or a shape is an error", func(t *testing.T) {
		mk := func(prop string) string {
			return `  properties:
    label: {type: string}
  edges:
    author:
      to: any
      properties:
        ` + prop + "\n"
		}
		loadThingErr(t, mk(`when: {type: string, repeated: true}`), `unknown key "repeated"`)
		loadThingErr(t, mk(`when: {type: string, keyed: true}`), `unknown key "keyed"`)
		loadThingErr(t, mk(`when: {type: string, fts: true}`), `unknown key "fts"`)
		loadThingErr(t, mk(`when: {type: string, managed: true}`), `unknown key "managed"`)
		loadThingErr(t, mk(`when: {type: string, unique: true}`), `unknown key "unique"`)
		loadThingErr(t, mk(`when: {type: object, fields: {a: {type: string}}}`), "a shape with fields is a record")
		loadThingErr(t, mk(`when: {type: reference, kind: any}`), "the edge IS the pointer")
		loadThingErr(t, mk(`when: {type: json}`), "not a place to hide one")
		loadThingErr(t, mk(`when: {type: secret}`), "a secret is a property of a record")
		loadThingErr(t, mk(`when: {type: state, states: [a, b]}`), "a machine belongs to a record")
		loadThingErr(t, mk(`when: {type: blobref}`), "a blob-ref resolves on a record's read path")
		loadThingErr(t, mk(`when: {type: digest}`), "a digest is minted onto a record")
	})

	t.Run("a refinement resolves, and resolves to the same rule", func(t *testing.T) {
		body := `  properties:
    label: {type: string}
  edges:
    author:
      to: any
      properties:
        isbn: {type: isbn}
`
		fs := fstest.MapFS{"g.yaml": &fstest.MapFile{Data: []byte(`kind: core.substrate.reamde.dev/authority
metadata: {id: g.example.com}
data: {version: 1}
---
kind: core.substrate.reamde.dev/propertytype
metadata: {id: g.example.com/isbn}
data:
  authority: g.example.com
  base: string
  pattern: "^[0-9]{13}$"
---
kind: core.substrate.reamde.dev/kind
metadata: {id: g.example.com/thing}
data:
  authority: g.example.com
  names: {singular: thing, plural: things}
` + body)}}
		r, err := vocabulary.LoadFS(fs)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		ty, _ := r.ByIdentity("g.example.com/thing")
		p := ty.Edges["author"].Props["isbn"]
		if p.Datatype != vocabulary.DatatypeString || p.Pattern == nil {
			t.Fatalf("refinement did not resolve: %+v", p)
		}
	})

	t.Run("a name that is not camelCase is an error", func(t *testing.T) {
		loadThingErr(t, `  properties:
    label: {type: string}
  edges:
    author:
      to: any
      properties:
        Order: {type: int}
`, "camelCase")
	})

	t.Run("an empty block is an error", func(t *testing.T) {
		loadThingErr(t, `  properties:
    label: {type: string}
  edges:
    author:
      to: any
      properties: {}
`, "drop the key rather than declaring none")
	})
}
