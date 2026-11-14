package engine

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// planEveryStatementLoops are the calls a transaction makes once a line or a
// record when it replays a history or fills fold tables wholesale: a replay
// folds a line, a kind move carries a record, a conversion step rewrites a
// line.
var planEveryStatementLoops = []string{"foldEntry", "moveRecord", "Line"}

// EVERY BULK FOLD PLANS EACH STATEMENT. A function that calls foldEntry,
// moveRecord or a step's Line directly inside a loop must call
// planEveryStatement somewhere in its own body, so a new replay cannot keep
// a plan made while a table was smaller (rebuild.go). What it does not see: a
// loop that reaches those calls through a wrapper two calls down, and whether
// the call to planEveryStatement runs before the loop. It needs no database.
func TestEveryBulkFoldPlansEachStatement(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var bulk []string
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			loops, plans := false, false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.ForStmt, *ast.RangeStmt:
					ast.Inspect(n, func(m ast.Node) bool {
						if call, ok := m.(*ast.CallExpr); ok && slices.Contains(planEveryStatementLoops, calledName(call)) {
							loops = true
						}
						return true
					})
				case *ast.CallExpr:
					if calledName(n) == "planEveryStatement" {
						plans = true
					}
				}
				return true
			})
			if !loops {
				continue
			}
			bulk = append(bulk, fn.Name.Name)
			if !plans {
				t.Errorf("%s: %s folds or carries in a loop and does not call planEveryStatement", fset.Position(fn.Pos()), fn.Name.Name)
			}
		}
	}
	for _, want := range []string{"rebuild", "walkConversionStep", "moveRecords"} {
		if !slices.Contains(bulk, want) {
			t.Errorf("the guard no longer finds %s among the bulk folds %v: the calls it looks for moved", want, bulk)
		}
	}
}

// calledName is the name a call calls, its selector's for a method.
func calledName(call *ast.CallExpr) string {
	switch f := call.Fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

// The plan check refuses what is not one probe of the primary key: a scan of
// the key narrowed by `repository` alone, a bitmap scan on another index, a
// table scan, and a plan with no recognized path to the table; and it does
// not take refs' src_kind for records' kind.
func TestThePlanCheckRefusesWhatIsNotOneProbe(t *testing.T) {
	t.Parallel()
	const repo = "(repository = current_setting('substrate.repository'::text, true))"
	for _, c := range []struct {
		name, table, pkey string
		keys              []string
		plan              string
		ok                bool
	}{
		{
			"the key probed", "records", "records_pkey",
			[]string{"kind", "id"},
			`{"Node Type":"Index Scan","Relation Name":"records","Index Name":"records_pkey","Index Cond":"(` + repo + ` AND (kind = 'a'::text) AND (id = 'b'::text))"}`, true,
		},
		{
			"the key narrowed by repository alone", "records", "records_pkey",
			[]string{"kind", "id"},
			`{"Node Type":"Index Scan","Relation Name":"records","Index Name":"records_pkey","Index Cond":"` + repo + `","Filter":"((kind = 'a'::text) AND (id = 'b'::text))"}`, false,
		},
		{
			"a bitmap scan on another index", "records", "records_pkey",
			[]string{"kind", "id"},
			`{"Node Type":"Bitmap Heap Scan","Relation Name":"records","Plans":[{"Node Type":"Bitmap Index Scan","Index Name":"records_deleted_at_idx","Index Cond":"(` + repo + ` AND (deleted_at IS NULL))"}]}`, false,
		},
		{
			"a table scan", "records", "records_pkey",
			[]string{"kind", "id"},
			`{"Node Type":"Seq Scan","Relation Name":"records","Filter":"((kind = 'a'::text) AND (id = 'b'::text))"}`, false,
		},
		{
			"no path to the table", "refs", "refs_pkey",
			[]string{"src_kind", "src"},
			`{"Node Type":"ModifyTable","Operation":"Delete","Relation Name":"refs","Plans":[{"Node Type":"Result"}]}`, false,
		},
		{
			"refs probed by src", "refs", "refs_pkey",
			[]string{"src_kind", "src"},
			`{"Node Type":"ModifyTable","Operation":"Delete","Relation Name":"refs","Plans":[{"Node Type":"Index Scan","Relation Name":"refs","Index Name":"refs_pkey","Index Cond":"(` + repo + ` AND (src_kind = 'a'::text) AND (src = 'b'::text))"}]}`, true,
		},
		{
			"src_kind is not kind", "records", "records_pkey",
			[]string{"kind", "id"},
			`{"Node Type":"Index Scan","Relation Name":"records","Index Name":"records_pkey","Index Cond":"(` + repo + ` AND (src_kind = 'a'::text) AND (id = 'b'::text))"}`, false,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			var plan any
			if err := json.Unmarshal([]byte(c.plan), &plan); err != nil {
				t.Fatal(err)
			}
			err := keyedAccess(planAccess(plan, c.table), c.pkey, c.keys)
			if (err == nil) != c.ok {
				t.Fatalf("the check said %v, want ok %t", err, c.ok)
			}
		})
	}
}
