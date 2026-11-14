package engine

// THE UPGRADE NEVER HOLDS A WRITABLE HANDLE ON A 0.x DIRECTORY'S HISTORY
// (openLocalHistory). A 0.x directory's history is its segments and its
// sealed files, the one other copy of each line until the table is held to
// its checksums and converted. Of the changelogfile functions that change, or
// may change, what those hold, the engine names exactly the ones listed here,
// each behind its gate: a new use fails the test until it is listed, and a
// listed one that goes fails it until its line does. The keying's rewrite of
// the manifest (WriteManifestToKey) holds no line and is not one of them.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// historyWriters are the changelogfile functions that write under a
// directory's segments or sealed files: Open cuts an unfinished tail,
// OpenWith does unless told not to, a Writer appends, and the rest remove or
// set aside.
var historyWriters = map[string]bool{
	"Open": true, "OpenWith": true, "OpenWriter": true,
	"WriteSealed": true, "DeleteSealed": true, "DiscardPendingSealed": true,
	"SetAsideHistory": true, "RemoveHistory": true,
}

// historyWrites is every place the engine names one, by file, function and
// name, with the gate it runs behind.
var historyWrites = map[string]string{
	"repodir.go:giveUpDirectory:SetAsideHistory":     "after compareHistories held every row of the table to its own checksum and to the directory's line",
	"repodir.go:giveUpDirectory:RemoveHistory":       "after the conversion, once historyUnsettled says nothing",
	"repodir.go:reconcileRepositories:RemoveHistory": "after the rows are reconciled, once historyUnsettled says nothing",
}

func TestTheUpgradeNeverWritesA0xDirectorysHistory(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	found := map[string]bool{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "changelogfile" && historyWriters[sel.Sel.Name] {
					found[name+":"+fn.Name.Name+":"+sel.Sel.Name] = true
				}
				return true
			})
		}
	}
	for _, use := range slices.Sorted(maps.Keys(found)) {
		if _, ok := historyWrites[use]; !ok {
			t.Errorf("%s writes, or may write, a 0.x directory's history: the upgrade opens it read-only (changelogfile.OpenReadOnly) and changes it only through SetAsideHistory and RemoveHistory behind their gates; list a new gated use in historyWrites", use)
		}
	}
	for use := range historyWrites {
		if !found[use] {
			t.Errorf("historyWrites lists %s, which the engine no longer names: take its line out", use)
		}
	}
}
