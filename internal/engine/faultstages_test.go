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
	"os"
	"path/filepath"
	"regexp"
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
	var tests []string
	for _, dir := range []string{".", "../testenv", "../api"} {
		files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if filepath.Base(f) == "export_test.go" || filepath.Base(f) == "faultstages_test.go" {
				continue
			}
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			tests = append(tests, string(raw))
		}
	}
	for _, stage := range sortedKeys(stages) {
		names := []string{strings.TrimPrefix(stage, "changelogfile.")}
		names = append(names, aliases[stage]...)
		pattern := regexp.MustCompile(`\b(` + strings.Join(names, "|") + `)\b`)
		if !slices.ContainsFunc(tests, pattern.MatchString) {
			t.Errorf("the seam stage %s (%q) is named by no test: stop the process there in a test and show the next start finishes it", stage, consts[stage])
		}
	}
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
