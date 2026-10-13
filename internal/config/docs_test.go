package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// operationsDoc is the page whose configuration table an operator reads to
// set the server up.
var operationsDoc = filepath.Join("..", "..", "docs", "operations.md")

// Every variable the server reads through envconfig has a row in the
// configuration table of docs/operations.md, so a setting cannot ship that an
// operator has no way to find.
func TestOperationsDocListsEveryVariable(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(operationsDoc)
	if err != nil {
		t.Fatal(err)
	}
	rows := configurationRows(t, string(raw))
	names := envconfigNames(reflect.TypeFor[Config]())
	// A floor, so a reflection that finds nothing cannot pass vacuously.
	if len(names) < 20 {
		t.Fatalf("found %d envconfig names on Config, want every one: %v", len(names), names)
	}
	for _, name := range names {
		if !rows[name] {
			t.Errorf("%s is read from the environment but has no row in the configuration table of %s", name, operationsDoc)
		}
	}
}

// configurationRows is the set of variable names that open a row of the
// table under the page's "## Configuration" heading: "| `NAME` | ...".
func configurationRows(t *testing.T, doc string) map[string]bool {
	t.Helper()
	_, section, ok := strings.Cut(doc, "\n## Configuration\n")
	if !ok {
		t.Fatalf("%s has no ## Configuration section", operationsDoc)
	}
	if end := strings.Index(section, "\n## "); end >= 0 {
		section = section[:end]
	}
	rows := map[string]bool{}
	for _, line := range strings.Split(section, "\n") {
		rest, ok := strings.CutPrefix(line, "| `")
		if !ok {
			continue
		}
		if name, _, ok := strings.Cut(rest, "`"); ok {
			rows[name] = true
		}
	}
	return rows
}

// envconfigNames is every envconfig name a struct reads, the untagged struct
// fields (Data) included.
func envconfigNames(t reflect.Type) []string {
	var out []string
	for f := range t.Fields() {
		if name := f.Tag.Get("envconfig"); name != "" {
			out = append(out, name)
			continue
		}
		if f.Type.Kind() == reflect.Struct {
			out = append(out, envconfigNames(f.Type)...)
		}
	}
	return out
}
