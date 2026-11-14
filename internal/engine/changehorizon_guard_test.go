package engine

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// EVERY READ OF THE CHANGES BY A POSITION CARRIES THE HORIZON
// (changehorizon.go). A read of the history by `seq >` stands for a reader
// moving forward from a position, and each one in this package is listed
// here with what holds it:
//
//   - horizon: it reads through changesAbove or queryChangesWithHorizon, the
//     rows with the horizon in one snapshot (the test checks the call);
//   - floor: the reader holds a floor of its own that no forgetting passes
//     (a record trigger's cursor: forgetDeletions stops below the lowest);
//   - maintenance: no reader's position, the engine's own pass over the
//     table (an archive, a compaction, a verify, a rebuild).
//
// A new read fails the test until it is listed, and one that goes fails it
// until its line does.
var changeReads = map[string]string{
	"query.go:Changes":                   "horizon: a stream's batch and a forward list, after the position the client holds",
	"changetail.go:catchUpTail":          "horizon: the tail's window, filled with the horizon its streams are refused below",
	"functions.go:triggerRead":           "floor: a record trigger reads past its own cursor, and forgetDeletions forgets nothing past the lowest",
	"archive.go:refreshArchiveStats":     "maintenance: the archive's own stats, from where its last piece ended",
	"compact.go:applyCompactEdits":       "maintenance: the compaction's rewrite of a record's own lines",
	"compact.go:compactBarriers":         "maintenance: the compaction's barriers inside a window it holds the lock over",
	"compact.go:compactPlans":            "maintenance: the compaction's plans inside a window it holds the lock over",
	"compact.go:transactionDamage":       "maintenance: the compaction's check of the transactions it would cut",
	"compactindex.go:backfillForeign":    "maintenance: the index of foreign lines, backfilled from its own mark",
	"compactpass.go:forgetBatch":         "maintenance: the forgetting reads the trigger cursors that hold it back",
	"erase.go:checkpointLines":           "maintenance: an erasure's own read of the drains' checkpoints of the record it erases, under the changelog lock",
	"erase.go:erasureWaits":              "maintenance: the trigger cursors an erasure waits on, read against the line it erases up to",
	"compactpass.go:horizonHeldBy":       "maintenance: the trigger cursor that holds the horizon back",
	"compactpass.go:stepAt":              "maintenance: the line a long record's next step compacts it up to, inside the window the pass compacts",
	"compactpass.go:purgedRecords":       "maintenance: the forgetting's purged records, between its own marks",
	"compactpass.go:startOverBelow":      "maintenance: the paged drains that start over below a forgetting",
	"compactpass.go:windowRecords":       "maintenance: the pass's records inside the window it compacts",
	"verify.go:scanChecksumPage":         "maintenance: the verify and the conversion walk every line, the whole table",
	"verify.go:scanStampPage":            "maintenance: the verify reads every line's stamp, the whole table",
	"verify.go:verifyArchive":            "maintenance: the verify asks its own snapshot for a line between two pieces of the archive, below a piece it skips",
	"webhookspool.go:webhookRecordedSQL": "maintenance: the webhook spool asks whether its own delivery entry stands above the seq the ledger recorded",
}

var seqForward = regexp.MustCompile(`\bseq\s*>=?\s`)

func TestEveryReadOfTheChangesByAPositionCarriesTheHorizon(t *testing.T) {
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
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			// A package-level constant or variable holding such a read is
			// listed by its name.
			if gen, ok := decl.(*ast.GenDecl); ok {
				for _, spec := range gen.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					reads := false
					for _, v := range vs.Values {
						ast.Inspect(v, func(n ast.Node) bool {
							if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING && seqForward.MatchString(lit.Value) {
								reads = true
							}
							return true
						})
					}
					if !reads {
						continue
					}
					for _, id := range vs.Names {
						key := name + ":" + id.Name
						found[key] = true
						if _, listed := changeReads[key]; !listed {
							t.Errorf("%s reads the history by `seq >` and is not in changeReads: list it with what holds it", key)
						}
					}
				}
				continue
			}
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			reads, horizon := false, false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.BasicLit:
					if n.Kind == token.STRING && seqForward.MatchString(n.Value) {
						reads = true
					}
				case *ast.SelectorExpr:
					if n.Sel.Name == "changesAbove" || n.Sel.Name == "queryChangesWithHorizon" {
						horizon = true
					}
				}
				return true
			})
			if !reads {
				continue
			}
			key := name + ":" + fn.Name.Name
			found[key] = true
			why, listed := changeReads[key]
			switch {
			case !listed:
				t.Errorf("%s reads the history by `seq >` and is not in changeReads: read it through changesAbove, or list it with what holds it (horizon, floor or maintenance)", key)
			case strings.HasPrefix(why, "horizon") && !horizon:
				t.Errorf("%s is listed as reading with the horizon but calls neither changesAbove nor queryChangesWithHorizon", key)
			}
		}
	}
	keys := make([]string, 0, len(changeReads))
	for k := range changeReads {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if !found[k] {
			t.Errorf("changeReads lists %s, which reads the history by `seq >` no more: remove its line", k)
		}
	}
}
