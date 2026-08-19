package engine

// The changelog dialect names a VOCABULARY: the ops an entry may carry and the
// fold effects its payload may spell (changelogdialect.go). Nothing in the
// compiler ties the constant to that vocabulary, so this test does. It reads
// the two files the wire values are declared in and holds them to the lists
// below, so ADDING an op or an effect kind fails here until somebody decides
// whether an older binary's fold would refuse or misread it, and bumps
// maxChangelogDialect in the same commit if it would.
//
// The lists are wire values, not identifiers: renaming the Go constant is free,
// changing what lands in a payload is not.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// dialectOneOps and dialectOneEffects are the changelog vocabulary at
// maxChangelogDialect == 1.
var (
	dialectOneOps = []string{
		"put", "patch", "delete", "link", "unlink", "merge", "split", "gc",
	}
	dialectOneEffects = []string{
		"record", "tombstone", "purge", "bump",
		"edge", "unedge", "edge1",
		"annotation", "manager", "former", "resync",
	}
)

func TestChangelogDialectCoversTheChangelogVocabulary(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		what     string
		file     string
		typeName string
		want     []string
	}{
		{"changelog ops", "../substrate/change.go", "Op", dialectOneOps},
		{"fold effects", "fold.go", "foldKind", dialectOneEffects},
	} {
		got := declaredStrings(t, c.file, c.typeName)
		if len(got) != len(c.want) {
			t.Fatalf("%s: %v, dialect %d covers %v. Decide whether an older binary's fold could refuse or misread the difference, then update this list (and maxChangelogDialect if it could)",
				c.what, got, maxChangelogDialect, c.want)
		}
		have := map[string]bool{}
		for _, v := range got {
			have[v] = true
		}
		for _, v := range c.want {
			if !have[v] {
				t.Fatalf("%s: %q is gone from the source but still listed at dialect %d: a spelling history may hold cannot simply be dropped",
					c.what, v, maxChangelogDialect)
			}
		}
	}
}

// declaredStrings returns the string values of every constant in file declared
// with the named type.
func declaredStrings(t *testing.T, file, typeName string) []string {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	var out []string
	for _, decl := range parsed.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			ident, ok := vs.Type.(*ast.Ident)
			if !ok || ident.Name != typeName {
				continue
			}
			for _, v := range vs.Values {
				lit, ok := v.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				out = append(out, lit.Value[1:len(lit.Value)-1])
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s declares no %s constants — did the type move?", file, typeName)
	}
	return out
}
