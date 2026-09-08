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
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// dialectTwoOps and dialectTwoEffects are the changelog vocabulary since
// dialect 2, where references absorbed edges (decision 0044): `link` and
// `unlink` stopped being ops and `edge`/`unedge`/`edge1` stopped being
// effects. Dialect 1 entries carrying any of the five are refused at the fold
// by name (fold.go foldRefuses) rather than replayed into a store with no
// pointers in it. Dialect 3 keeps this vocabulary unchanged: its rung is the
// `txn` frame on every entry and the checksum over it (decision 0057), not a
// spelling. Dialect 4 keeps it unchanged too: its rung is the `kindVersion`
// key on the record delta (decision 0060, TestTheKindVersionStampIsDialectFour),
// so the lists hold.
var (
	dialectTwoOps = []string{
		"put", "patch", "delete", "merge", "split", "gc",
	}
	dialectTwoEffects = []string{
		"record", "tombstone", "purge", "bump",
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
		{"changelog ops", "../substrate/change.go", "Op", dialectTwoOps},
		{"fold effects", "fold.go", "foldKind", dialectTwoEffects},
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

// Dialect 4 is the `record` delta carrying `kindVersion` (decision 0060). A
// dialect 3 binary does not refuse the key: foldOpsOf decodes without
// DisallowUnknownFields, so it replays the entry, drops the stamp and folds
// the row to 0 with nothing saying so. The rung is what makes that binary
// refuse at the gate, so the constant and the key are pinned together: a
// binary that writes the key stamps 4, and one that stops writing it may not
// keep the number.
func TestTheKindVersionStampIsDialectFour(t *testing.T) {
	t.Parallel()
	if maxChangelogDialect != 4 {
		t.Fatalf("maxChangelogDialect = %d; the kindVersion stamp is rung 4", maxChangelogDialect)
	}
	if err := admitChangelogDialect("geoah", maxChangelogDialect, 3); !errors.Is(err, ErrChangelogDialectNewer) {
		t.Fatalf("a dialect 3 binary admitted a repository stamped 4: %v", err)
	}
	if err := admitChangelogDialect("geoah", 3, maxChangelogDialect); err != nil {
		t.Fatalf("a repository stamped 3 must open under this binary: %v", err)
	}
	raw, err := json.Marshal(rowDelta{KindVersion: 4})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"kindVersion":4}` {
		t.Fatalf("the record delta spells the stamp as %s; dialect 4 is the key `kindVersion`", raw)
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

// A repository this binary stamps (3) is refused by a binary whose maximum is
// 2, which is v0.46.0 and v0.47.0: the frame is invisible to their fold, so
// the dialect is what stops their boot catch-up from re-stamping every
// `changelog.hash` without `txn` (changelogdialect.go, rung three).
func TestChangelogDialectThreeIsRefusedByADialectTwoBinary(t *testing.T) {
	t.Parallel()
	if maxChangelogDialect < 3 {
		t.Fatalf("maxChangelogDialect = %d; the txn frame is rung 3", maxChangelogDialect)
	}
	err := admitChangelogDialect("geoah", 3, 2)
	if !errors.Is(err, ErrChangelogDialectNewer) {
		t.Fatalf("a dialect 2 binary admitted a repository stamped 3: %v", err)
	}
	if !strings.Contains(err.Error(), "dialect 3, this binary replays <= 2") {
		t.Fatalf("the refusal must name both numbers: %v", err)
	}
	if err := admitChangelogDialect("geoah", 2, maxChangelogDialect); err != nil {
		t.Fatalf("a repository stamped 2 must open under this binary: %v", err)
	}
	if err := admitChangelogDialect("geoah", 0, maxChangelogDialect); err != nil {
		t.Fatalf("an unstamped repository must open: %v", err)
	}
}
