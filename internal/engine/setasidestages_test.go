package engine

// EVERY STAGE OF THE START THAT SETS A REPOSITORY ASIDE IS REACHED BY A REAL
// FAILURE (validation item 4 of the 1.0 review). setaside.go names each step
// of the start that may set a repository aside, and a stage is a promise only
// while a test reaches it through a failure the start really meets (damage on
// the disk, in the database or in the store, never the gate called by hand
// or a seam standing in for one) and holds the stage the start named. Each
// stage is listed here with the declarations that do, by file and name, or
// as reached only through a seam, with why no real failure reaches it. A
// stage added to setaside.go fails here until it is listed, and a listed
// declaration that goes, or no longer names its stage, fails it until its
// line does.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// setAsideStageCases are the declarations that reach each stage through a
// real failure and hold it: a test, or the table of cases a test drives.
var setAsideStageCases = map[string][]string{
	setAsideAtKey: {
		"startgate_internal_db_test.go:TestADeletionOfARepositorySetAsideWaits",
		"repositoryid_internal_db_test.go:TestAManifestClaimingAnotherRepositorysIDTouchesNothingOfIt",
		"../testenv/setaside_db_test.go:asideCases",
	},
	setAsideAtID:   {"repositoryid_internal_db_test.go:TestARowWhosePreIDDirectoryCannotBeProvenIsSetAsideWithIt"},
	setAsideAtTake: {"startgate_internal_db_test.go:TestATakeWhoseEraseTheDatabaseRefusesIsSetAsideAtTheTake"},
	setAsideAtCheck: {
		"conversion_internal_db_test.go:TestAConversionStopsAtARowItCannotVerify",
		"../testenv/setaside_db_test.go:asideCases",
	},
	setAsideAtImport:  {"startgate_internal_db_test.go:TestA0xDirectoryWhoseLineDoesNotVerifyIsSetAsideAtTheImport"},
	setAsideAtRestore: {"../testenv/setaside_db_test.go:asideCases"},
	setAsideAtStore:   {"startgate_internal_db_test.go:TestARestoreWhoseStoreStopsAnsweringIsSetAsideAtTheArchiveStore"},
}

// setAsideOnlyBySeam are the stages no real failure reaches today, each with
// why. They are kept on purpose: a change that makes the step fail for one
// repository alone meets a start that already sets that one aside.
var setAsideOnlyBySeam = map[string]string{
	setAsideAtLease: "every real failure of the lease's acquire is the server's and stops the start by design " +
		"(another writer, a lease lost, the database gone); the orphan retake cannot meet this process's own " +
		"name at a start, since each process mints its own; and the lock table filling is the server's capacity, " +
		"not one repository's",
}

func TestEverySetAsideStageIsReachedByARealFailure(t *testing.T) {
	t.Parallel()
	stages := setAsideStages(t)
	if len(stages) == 0 {
		t.Fatal("setaside.go declares no stage: the guard reads nothing")
	}
	for name, value := range stages {
		cases, real := setAsideStageCases[value]
		_, seam := setAsideOnlyBySeam[value]
		switch {
		case real && seam:
			t.Errorf("the stage %q (%s) is listed both as reached by a real failure and only through a seam", value, name)
		case !real && !seam:
			t.Errorf("the stage %q (%s) has no case: reach it through a real failure in a test that holds the stage, and list the test in setAsideStageCases, or say in setAsideOnlyBySeam why nothing real reaches it", value, name)
		}
		for _, c := range cases {
			file, decl, _ := strings.Cut(c, ":")
			src, ok := declarationSource(t, file, decl)
			switch {
			case !ok:
				t.Errorf("setAsideStageCases lists %s for %q, which is not declared there", c, value)
			case !strings.Contains(src, name) && !strings.Contains(src, strconv.Quote(value)):
				t.Errorf("%s is listed for %q and names neither %s nor %q", c, value, name, value)
			}
		}
	}
	for value := range setAsideStageCases {
		if !slices.Contains(mapValues(stages), value) {
			t.Errorf("setAsideStageCases lists %q, which setaside.go no longer declares", value)
		}
	}
	for value := range setAsideOnlyBySeam {
		if !slices.Contains(mapValues(stages), value) {
			t.Errorf("setAsideOnlyBySeam lists %q, which setaside.go no longer declares", value)
		}
	}
}

// setAsideStages reads setaside.go's stage constants: each name with its
// value.
func setAsideStages(t *testing.T) map[string]string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "setaside.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, n := range vs.Names {
				if !strings.HasPrefix(n.Name, "setAsideAt") || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok {
					continue
				}
				v, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatal(err)
				}
				out[n.Name] = v
			}
		}
	}
	return out
}

// declarationSource is the source of the top-level function or variable
// named decl in file.
func declarationSource(t *testing.T, file, decl string) (string, bool) {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, raw, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil && d.Name.Name == decl {
				return string(raw[fset.Position(d.Pos()).Offset:fset.Position(d.End()).Offset]), true
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if vs, ok := spec.(*ast.ValueSpec); ok && slices.ContainsFunc(vs.Names, func(n *ast.Ident) bool { return n.Name == decl }) {
					return string(raw[fset.Position(d.Pos()).Offset:fset.Position(d.End()).Offset]), true
				}
			}
		}
	}
	return "", false
}

func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}
