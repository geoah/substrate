package engine

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// Every check the install path runs (InstallBundleClosure, and a
// transaction's end after its work) has a release's answer in
// releaseAdmission: run in admitRelease, or not, with the reason. A rule
// the installer gains later fails this until its author says which.
//
// It is a tripwire, not a proof. It sees the calls spelled in the two
// function bodies, InstallBundleClosure's (its inline `extra` included)
// and inTxOn's after `fn(t)`. It does not see a check added inside a
// helper either calls, nor a callback defined elsewhere that the batch
// runs (a batch's beforeGuards, set by its caller).
func TestEveryInstallCheckHasAReleaseAnswer(t *testing.T) {
	fset := token.NewFileSet()
	calls := map[string]bool{}
	collect := func(n ast.Node) {
		ast.Inspect(n, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch f := c.Fun.(type) {
			case *ast.Ident:
				switch f.Name {
				case "append", "len", "make", "cap", "delete", "max", "min", "new", "panic":
				default:
					calls[f.Name] = true
				}
			case *ast.SelectorExpr:
				if x, ok := f.X.(*ast.Ident); ok {
					switch x.Name {
					case "ds", "t", "vocabulary":
						calls[f.Sel.Name] = true
					}
				}
				if inner, ok := f.X.(*ast.SelectorExpr); ok {
					if x, ok := inner.X.(*ast.Ident); ok && x.Name == "t" && inner.Sel.Name == "ds" {
						calls[f.Sel.Name] = true
					}
				}
			}
			return true
		})
	}
	find := func(file, name string) *ast.FuncDecl {
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == name {
				return fn
			}
		}
		t.Fatalf("%s declares no %s", file, name)
		return nil
	}
	collect(find("vocabularywrite.go", "InstallBundleClosure").Body)
	// A transaction's end: every statement after the one that runs its work.
	inTx := find("dataset.go", "inTxOn")
	after := false
	for _, st := range inTx.Body.List {
		if after {
			collect(st)
			continue
		}
		ast.Inspect(st, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "fn" {
					after = true
				}
			}
			return true
		})
	}
	if !after {
		t.Fatal("inTxOn runs no fn: the transaction's end was not found")
	}
	for name := range calls {
		if _, ok := releaseAdmission[name]; !ok {
			t.Errorf("the install path calls %s, which releaseAdmission does not answer: say whether admitRelease runs it, and why", name)
		}
	}
	for name, a := range releaseAdmission {
		if !calls[name] {
			t.Errorf("releaseAdmission answers %s, which the install path no longer calls: take it out", name)
		}
		if a.why == "" {
			t.Errorf("releaseAdmission answers %s with no reason", name)
		}
	}
}
