package engine

// EVERY STAGE A FAULT SEAM REPORTS IS NAMED BY A TEST (validation item 5 of
// the 1.0 review). A seam is a method of the engine's whose one parameter is
// `stage string`, named for the Fault or the Stage it reports: it runs a
// test's hook at a durable step of a write, an archive pass, an import, a
// conversion, a keying or an erase, where an error stops the process as a
// kill there would. Each stage the engine hands a seam, and each stage the
// changelog files hand the hook the engine passes them (their `stop`), is a
// place the process may die, and only a test that stops it there shows the
// next start finishes what it began. A stage no test names fails here until
// one does; the files that only give a stage its exported name do not count.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestEveryFaultSeamStageIsNamedByATest(t *testing.T) {
	t.Parallel()
	engineFiles := parsedFiles(t, ".", false)
	consts := stringConstants(engineFiles)
	seams := faultSeams(engineFiles)
	if len(seams) == 0 {
		t.Fatal("no seam found: the guard reads nothing")
	}
	stages, forwarded := seamStages(engineFiles, seams, consts)
	if forwarded {
		for _, name := range changelogfileStops(t) {
			stages["changelogfile."+name] = true
		}
	}
	if len(stages) < 30 {
		t.Fatalf("found %d stages, fewer than the engine reports: the guard reads the seams wrong", len(stages))
	}
	t.Logf("%d seams report %d stages", len(seams), len(stages))
	aliases := exportedStageNames(t)
	used := map[string]bool{}
	for _, dir := range []string{".", "../testenv", "../api"} {
		maps.Copy(used, namesUsedInCode(t, dir))
	}
	for _, stage := range sortedKeys(stages) {
		names := []string{strings.TrimPrefix(stage, "changelogfile.")}
		names = append(names, aliases[stage]...)
		if !slices.ContainsFunc(names, func(n string) bool { return used[n] }) {
			t.Errorf("the seam stage %s (%q) is named by no test: stop the process there in a test and show the next start finishes it", stage, consts[stage])
		}
	}
}

// namesUsedInCode is every identifier the test files of dir use in code:
// inside a function's body, or in a package-level value a function's body
// uses (a table of stages a test drives). A name in a comment or inside a
// string is not one. export_test.go and this file are left out: one
// re-exports the stages and the other reads them.
func namesUsedInCode(t *testing.T, dir string) map[string]bool {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	inBodies := map[string]bool{}
	tables := map[string][]string{}
	idents := func(n ast.Node) []string {
		var out []string
		ast.Inspect(n, func(m ast.Node) bool {
			if id, ok := m.(*ast.Ident); ok {
				out = append(out, id.Name)
			}
			return true
		})
		return out
	}
	for _, name := range files {
		if base := filepath.Base(name); base == "export_test.go" || base == "faultstages_test.go" {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Body != nil {
					for _, n := range idents(d.Body) {
						inBodies[n] = true
					}
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					var values []string
					for _, v := range vs.Values {
						values = append(values, idents(v)...)
					}
					for _, n := range vs.Names {
						tables[n.Name] = append(tables[n.Name], values...)
					}
				}
			}
		}
	}
	used := maps.Clone(inBodies)
	for table, values := range tables {
		if inBodies[table] {
			for _, n := range values {
				used[n] = true
			}
		}
	}
	return used
}

// parsedFiles parses dir's Go files, the tests among them or not.
func parsedFiles(t *testing.T, dir string, tests bool) []*ast.File {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var out []*ast.File
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") != tests {
			continue
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, f)
	}
	return out
}

// stringConstants are the package-level string constants, by name.
func stringConstants(files []*ast.File) map[string]string {
	out := map[string]string{}
	for _, f := range files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, n := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						v, _ := strconv.Unquote(lit.Value)
						out[n.Name] = v
					}
				}
			}
		}
	}
	return out
}

// faultSeams are the methods whose one parameter is `stage string` and whose
// name says they report a fault or a stage.
func faultSeams(files []*ast.File) map[string]bool {
	out := map[string]bool{}
	for _, f := range files {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || !(strings.HasSuffix(fn.Name.Name, "Fault") || strings.HasSuffix(fn.Name.Name, "Stage")) {
				continue
			}
			params := fn.Type.Params.List
			if len(params) != 1 || len(params[0].Names) != 1 || params[0].Names[0].Name != "stage" {
				continue
			}
			if id, ok := params[0].Type.(*ast.Ident); ok && id.Name == "string" {
				out[fn.Name.Name] = true
			}
		}
	}
	return out
}

// seamStages are the stages the engine hands a seam: a constant, or, where
// the call passes a field of a table, the constants that open the table's
// rows in the same function. forwarded says a seam is handed on as a value,
// whose stages the callee chooses.
func seamStages(files []*ast.File, seams map[string]bool, consts map[string]string) (map[string]bool, bool) {
	out := map[string]bool{}
	forwarded := false
	for _, f := range files {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				for _, arg := range call.Args {
					if sel, ok := arg.(*ast.SelectorExpr); ok && seams[sel.Sel.Name] {
						forwarded = true
					}
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || !seams[sel.Sel.Name] || len(call.Args) != 1 {
					return true
				}
				switch arg := call.Args[0].(type) {
				case *ast.Ident:
					if _, ok := consts[arg.Name]; ok {
						out[arg.Name] = true
					}
				default:
					ast.Inspect(fn.Body, func(m ast.Node) bool {
						if lit, ok := m.(*ast.CompositeLit); ok && len(lit.Elts) > 0 {
							if id, ok := lit.Elts[0].(*ast.Ident); ok {
								if _, ok := consts[id.Name]; ok {
									out[id.Name] = true
								}
							}
						}
						return true
					})
				}
				return true
			})
		}
	}
	return out, forwarded
}

// changelogfileStops are the stages the changelog files hand the hook a
// caller passes them, which they call `stop`.
func changelogfileStops(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, f := range parsedFiles(t, "../changelogfile", false) {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			if fn, ok := call.Fun.(*ast.Ident); ok && fn.Name == "stop" {
				if id, ok := call.Args[0].(*ast.Ident); ok && !slices.Contains(out, id.Name) {
					out = append(out, id.Name)
				}
			}
			return true
		})
	}
	return out
}

// exportedStageNames are the names seams.go and export_test.go give a stage
// for the tests outside the package: stage, or changelogfile.X, to its
// exported names.
func exportedStageNames(t *testing.T) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, name := range []string{"seams.go", "export_test.go"} {
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, n := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					switch v := vs.Values[i].(type) {
					case *ast.Ident:
						out[v.Name] = append(out[v.Name], n.Name)
					case *ast.SelectorExpr:
						if pkg, ok := v.X.(*ast.Ident); ok {
							key := pkg.Name + "." + v.Sel.Name
							out[key] = append(out[key], n.Name)
						}
					}
				}
			}
		}
	}
	return out
}

// A stage named only in a comment, or inside a longer string, is not named:
// both guards count what code uses.
func TestAStageNamedOnlyInACommentIsNotNamed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := `package engine

// stopAtFirst is in a comment only.
var table = []string{stopInTable}

var unusedTable = []string{stopInUnusedTable}

func TestX(t *testing.T) {
	_ = "a string that mentions stopInString"
	_ = stopInBody
	for range table {
	}
}
`
	if err := os.WriteFile(filepath.Join(dir, "x_test.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	used := namesUsedInCode(t, dir)
	for name, want := range map[string]bool{
		"stopAtFirst": false, "stopInString": false, "stopInUnusedTable": false,
		"stopInBody": true, "stopInTable": true,
	} {
		if used[name] != want {
			t.Errorf("%s: used %t, want %t", name, used[name], want)
		}
	}
	f, err := parser.ParseFile(token.NewFileSet(), "x_test.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !usesStage(f, "stopInBody", "") || usesStage(f, "stopAtFirst", "") || usesStage(f, "absent", "mentions stopInString") {
		t.Error("usesStage counts a comment, or a stage's value inside a longer string")
	}
	if !usesStage(f, "absent", "a string that mentions stopInString") {
		t.Error("usesStage does not count a stage's value as a whole string literal")
	}
}
