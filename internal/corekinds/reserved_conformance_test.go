package corekinds_test

// The reserved keys (issue 110) across BOTH readers, on a fixture rather than
// on kinds/core.substrate.reamde.dev.
//
// The conformance test next door holds the two readers to one answer about
// every shipped document, which is the right instrument for everything core
// declares. It cannot see these keys: nothing in core is deprecated or unique
// yet, so `unique` and `deprecated` compare equal by both being absent, and a
// reader that stopped reading either would pass. A reservation nobody uses
// yet is exactly the thing that rots quietly, so it gets a declaration of its
// own here.

import (
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/kinddialect"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestBothReadersReadTheReservedKeys(t *testing.T) {
	fsys := fstest.MapFS{"g.yaml": &fstest.MapFile{Data: []byte(`kind: core.substrate.reamde.dev/authority
metadata:
  id: g.example.com
data:
  version: 1
---
kind: core.substrate.reamde.dev/kind
metadata:
  id: g.example.com/thing
data:
  authority: g.example.com
  names:
    singular: thing
    plural: things
  properties:
    email:
      type: email
      unique: true
    size:
      type: string
      deprecated: true
    owner:
      type: reference
      kind: any
      unique: true
      deprecated: true
    flavor:
      type: enum
      values:
        - value: sweet
        - value: salty
          deprecated: true
`)}}

	reg, err := vocabulary.LoadFS(fsys)
	if err != nil {
		t.Fatalf("loader: %v", err)
	}
	loaded, ok := reg.ByIdentity("g.example.com/thing")
	if !ok {
		t.Fatal("the loader did not read the fixture kind")
	}
	kinds, err := kinddialect.ReadFS(fsys)
	if err != nil {
		t.Fatalf("reader: %v", err)
	}
	if len(kinds) != 1 {
		t.Fatalf("the reader saw %d kinds, want 1", len(kinds))
	}
	read := map[string]*kinddialect.Property{}
	for _, p := range kinds[0].Props {
		read[p.Name] = p
	}

	for _, name := range []string{"email", "size", "owner", "flavor"} {
		d, r := loaded.Props[name], read[name]
		if d == nil || r == nil {
			t.Fatalf("%s: loader %v, reader %v", name, d, r)
		}
		if d.Unique != r.Unique || d.Deprecated != r.Deprecated {
			t.Errorf("%s: loader unique=%v deprecated=%v, reader unique=%v deprecated=%v",
				name, d.Unique, d.Deprecated, r.Unique, r.Deprecated)
		}
	}
	// Both readers saw the markers as declared, not as zero values: a test
	// where every field is false passes whatever either reader does.
	if !loaded.Props["email"].Unique || !loaded.Props["size"].Deprecated {
		t.Fatalf("the loader read no markers: %+v", loaded.Props)
	}
	dv, rv := loaded.Props["flavor"].Values, read["flavor"].Values
	if len(dv) != 2 || len(rv) != 2 {
		t.Fatalf("enum values: loader %v, reader %v", dv, rv)
	}
	if dv[1].Deprecated != rv[1].Deprecated || !dv[1].Deprecated {
		t.Errorf("deprecated enum value: loader %+v, reader %+v", dv[1], rv[1])
	}
}
