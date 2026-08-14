package substrate

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The console mirrors these shapes BY HAND. web/console/src/lib/api/types.ts is
// several hundred lines of TypeScript interfaces written to match the structs
// in this package, and nothing generates it: there is no OpenAPI document, no
// SDL export and no code generator anywhere in this tree, which is a deliberate
// simplification and also the one place the two halves can silently disagree.
//
// A Go field renamed, added or retagged used to be invisible to the console
// until something failed in a browser. This is the guard: the field names Go
// serializes are written to a golden file, this test fails when they move, and
// a vitest beside the golden fails when the TypeScript does not match it. The
// golden is the contract the two sides meet at, and it is reviewed as a diff.
//
// Adding a shape here is deliberate — it commits the console to tracking it.

var updateGolden = flag.Bool("update", false, "rewrite the wire golden file")

// goldenPath is relative because the console consumes the same file: the test
// writes it into web/console so a vitest can import it without reaching across
// the repository at runtime.
const goldenPath = "../../web/console/src/lib/api/wire.golden.json"

// wireTypes are the shapes types.ts mirrors. The KEY is the TypeScript name,
// which is the console's word for it — `Record` is `SubstrateRecord` there
// because TypeScript already owns `Record<K, V>`.
var wireTypes = map[string]any{
	"SubstrateRecord":     Record{},
	"EdgeTarget":          EdgeTarget{},
	"EdgeRef":             EdgeRef{},
	"EdgeInput":           EdgeInput{},
	"PropertyMeta":        PropertyMeta{},
	"PropertyAlternative": PropertyAlternative{},
	"PutInput":            PutInput{},
}

// jsonFields lists the wire names a struct serializes, in declaration order. A
// field tagged `json:"-"` is not on the wire and is not listed: Record.Title
// and Record.At are server-side projections of properties, and the console is
// right not to know them.
func jsonFields(t *testing.T, v any) []string {
	t.Helper()
	rt := reflect.TypeOf(v)
	if rt.Kind() != reflect.Struct {
		t.Fatalf("wire type %s is not a struct", rt)
	}
	var out []string
	for i := range rt.NumField() {
		f := rt.Field(i)
		if !f.IsExported() {
			continue
		}
		if f.Anonymous {
			// Nothing here embeds today. If something starts to, its fields
			// are promoted onto the wire and this guard would quietly miss
			// them, so it stops instead.
			t.Fatalf("%s embeds %s: teach jsonFields to flatten it", rt.Name(), f.Type)
		}
		tag, ok := f.Tag.Lookup("json")
		if !ok {
			t.Fatalf("%s.%s has no json tag: the wire name would be the Go name by accident", rt.Name(), f.Name)
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		if name == "" {
			t.Fatalf("%s.%s has a json tag that names nothing", rt.Name(), f.Name)
		}
		out = append(out, name)
	}
	return out
}

func TestWireGoldenMatchesStructs(t *testing.T) {
	got := make(map[string][]string, len(wireTypes))
	for name, v := range wireTypes {
		got[name] = jsonFields(t, v)
	}

	encoded, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	encoded = append(encoded, '\n')

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(goldenPath, encoded, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (regenerate with `go test ./internal/substrate/ -run TestWireGolden -update`): %v", err)
	}
	if string(want) != string(encoded) {
		t.Errorf("the wire shapes have moved and %s is stale.\n\n"+
			"Regenerate it:\n"+
			"  go test ./internal/substrate/ -run TestWireGolden -update\n\n"+
			"then make web/console/src/lib/api/types.ts match — `pnpm test` in\n"+
			"web/console fails until it does.\n\ngot:\n%s\nwant:\n%s",
			goldenPath, encoded, want)
	}
}
