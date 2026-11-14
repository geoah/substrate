package engine

// No property name is stored apart (decision 0273). `body` had a column, a
// field of every history entry and a branch in every place that read either;
// these hold that none comes back. The behavior is held by the property
// tests in internal/testenv (bodyproperty_db_test.go), which answer `body`
// and another name the same.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// bodyLiteralAllowed are the files that may still spell "body": an HTTP
// request's body, which is no property, and the two one-time steps that
// carry history from the column into the properties.
var bodyLiteralAllowed = map[string]string{
	"webhooks.go":         "a webhook request's body",
	"webhooksignature.go": "a webhook request's body",
	"conversion_0004_move_body_into_properties.go":                "the step that moved it",
	"repomigration_0011_drop_a_body_its_kind_does_not_declare.go": "the migration that cleared what the column hid",
}

func TestNoPropertyNameIsSpecialInTheSource(t *testing.T) {
	t.Parallel()
	for _, dir := range []string{".", "../vocabulary", "../substrate"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range files {
			name := filepath.Base(path)
			if strings.HasSuffix(name, "_test.go") || bodyLiteralAllowed[name] != "" {
				continue
			}
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				if s, err := strconv.Unquote(lit.Value); err == nil && s == "body" {
					t.Errorf("%s spells \"body\": a property name the engine treats apart from the others", path)
				}
				return true
			})
		}
	}
}

// The row, the history entry's delta and the record carry no field of their
// own for one property.
func TestNoRowOrDeltaFieldIsAPropertyOfItsOwn(t *testing.T) {
	t.Parallel()
	for _, typ := range []reflect.Type{reflect.TypeOf(erow{}), reflect.TypeOf(rowDelta{}), reflect.TypeOf(substrate.Record{})} {
		for i := range typ.NumField() {
			f := typ.Field(i)
			if strings.EqualFold(f.Name, "body") || strings.HasPrefix(f.Tag.Get("json"), "body") {
				t.Errorf("%s.%s is a field of its own for the body property", typ.Name(), f.Name)
			}
		}
	}
}
