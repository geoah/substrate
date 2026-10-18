package engine

// The changelog is the truth and the fold tables (rebuild.go foldTables) are
// what fold.go makes of it, so a rebuild reproduces them only if every write
// to them either IS the fold or is a derivation the rebuild re-runs. Nothing in
// the compiler holds that: a new `UPDATE records` anywhere in the package
// builds and passes every test that does not rebuild afterwards. These tests
// read the package's source instead, so a new write site fails here until
// somebody decides why it may exist and lists it below.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// foldWriteSites is every function that names a fold table after INSERT INTO,
// UPDATE, DELETE FROM, MERGE INTO or TRUNCATE, or builds such a statement
// around a table name known only at run time. The key is file:function, so an
// entry follows the function that writes and not the one that calls it; the
// value is why a rebuild still reproduces, or may lose, what it writes.
var foldWriteSites = map[string]string{
	// The fold itself: foldPrimitives names who may call each of these
	// (TestFoldPathPrimitivesAreCalledOnlyFromTheFold).
	"rows.go:upsertRecord":      "the record effect's write",
	"rows.go:applyTombstone":    "the tombstone effect's write",
	"rows.go:applyPurge":        "the purge effect's write",
	"rows.go:applyAnnotation":   "the annotation effect's write",
	"rows.go:applyManager":      "the manager effect's write",
	"rows.go:applyFormerID":     "the former-id effect's write",
	"fold.go:applyResync":       "the resync effect's write, which merge and split record and a replay applies",
	"delivery.go:applyDelivery": "the delivery ledger effects' writes",
	"delivery.go:upsertPage":    "applyDelivery's page effect",

	// Derived storage no entry carries: each is a function of the folded rows
	// and the declarations, and a rebuild derives it again.
	"fold.go:rederiveFTS":            "`fts`, the search index over the folded row",
	"refs.go:syncRefs":               "the refs projection of one row, derived again by foldRecordOp on replay",
	"refs.go:syncRefsOfRows":         "the refs projection of a page of rows, for a declaration change",
	"orphans.go:syncOrphaned":        "the `orphaned_at` mark, derived again by rederiveOffers",
	"orphans.go:clearOrphanMarks":    "the `orphaned_at` marks of a kind no mapping targets any more",
	"mapping.go:afterTombstone":      "the `orphaned_at` mark of a tombstoned target",
	"ambiguous.go:markAmbiguous":     "the `ambiguous_at` mark, derived again by rederiveOffers",
	"ambiguous.go:deriveAmbiguousOf": "the `ambiguous_at` marks of a source kind",
	"rebuild.go:rederiveOffers":      "clears every `ambiguous_at` mark before deriving them again",
	// The observed index (decision 0160): a record's segment is a function
	// of its folded row and its kind's binding, recorded by syncObserved from
	// foldRecordOp and written by settleObserved at the transaction's end with
	// the rollups and latest rows of what it touched. A rebuild clears the
	// tables (foldTables) and the background pass derives them again from
	// every row (queueObservedRederivation).
	"observedsettle.go:deleteSegments":     "the observed segments of rows a transaction wrote again or removed",
	"observedsettle.go:insertSegmentBatch": "the observed segments of rows a transaction wrote",
	"observedsettle.go:streamIDs":          "a stream's private id, which a rebuild may allocate again differently",
	"observedsettle.go:widenExtents":       "a stream's longest record extent, a bound that only grows and that a rebuild derives again",
	"observedsettle.go:dropUnnamedTiers":   "the rollup rows of tiers a metric no longer names, around the instants a transaction touched",
	"observedsettle.go:replaceRows":        "the rollup rows of the buckets a transaction's segments touched, before they are computed again",
	"observedsettle.go:insertRollupBatch":  "the rollup rows of the buckets a transaction's segments touched",
	"observedsettle.go:recomputeLatest":    "the observed latest rows of the streams a transaction's segments touched",

	// Merge and split rewrite the side stores with set-shaped statements and
	// record a resync effect that restates the result (fold.go recordResync).
	"merge.go:moveManagers":    "merge's manager rows, restated by the resync effect",
	"merge.go:moveAnnotations": "merge's annotations, restated by the resync effect",
	"merge.go:splitIf":         "split's side-store rows, restated by the resync effect",
	"rows.go:moveFormerIDs":    "merge's former-id flattening, which no effect records, so a rebuild leaves chains where the live table is flat (#821)",

	// Writes that carry state of their own.
	"functions.go:advanceCursor": "the trigger scan position: it acknowledges no delivery, so a restore that loses it re-reads rows that deliver nothing",

	// Whole-table statements whose table is a variable.
	"rebuild.go:rebuild":                 "clears every fold table before the replay fills them again",
	"repositories.go:eraseRepositoryOn":  "erases a whole repository, changelog included",
	"repositories.go:sweepOrphans":       "erases the rows of a repository with no control-plane row, which nothing can reach",
	"embedprovider.go:pruneUnembeddable": "its tables are embeddings and embed_queue, neither a fold table",
}

// foldPrimitives are the functions that apply an effect to the fold tables,
// each with the only functions that may call it. A write path that called
// one directly would change a table and record no effect, so a rebuild would
// not reproduce it: txn.fold applies AND records, and foldEntry is the
// replay.
var foldPrimitives = map[string][]string{
	"foldEntry":       {"rebuild.go:rebuild"},
	"foldOne":         {"fold.go:fold", "fold.go:foldEntry"},
	"foldRecordOp":    {"fold.go:foldOne"},
	"upsertRecord":    {"fold.go:foldRecordOp"},
	"applyTombstone":  {"fold.go:foldOne"},
	"applyPurge":      {"fold.go:foldOne"},
	"applyAnnotation": {"fold.go:foldOne"},
	"applyManager":    {"fold.go:foldOne"},
	"applyFormerID":   {"fold.go:foldOne"},
	"applyResync":     {"fold.go:foldOne"},
	"applyDelivery":   {"fold.go:foldOne"},
	"upsertPage":      {"delivery.go:applyDelivery"},
}

// foldWriteRe matches one write statement's head. The table is a name,
// quoted or not and schema-qualified or not, or a table known only at run
// time: the hole a non-constant operand of a concatenation left
// (dynamicTable), or a format verb.
var foldWriteRe = regexp.MustCompile(`(?i)\b(?:INSERT\s+INTO|UPDATE|DELETE\s+FROM|MERGE\s+INTO|TRUNCATE(?:\s+TABLE)?)\s+(?:ONLY\s+)?((?:"?[a-z_][a-z0-9_$]*"?\s*\.\s*)?"?(?:[a-z_][a-z0-9_$]*|` + dynamicTable + `|%[-+# 0-9.*]*[a-z])"?)`)

// dynamicTable stands in for an operand of a string concatenation that is not
// a constant, so a statement whose table is a variable still reads as a
// write.
const dynamicTable = "\x00"

func TestFoldPathWritesOnlyFromListedFunctions(t *testing.T) {
	t.Parallel()
	sites := foldWritesInPackage(t)
	for _, key := range slices.Sorted(maps.Keys(sites)) {
		if _, ok := foldWriteSites[key]; !ok {
			t.Errorf("%s writes a fold table outside the fold (%s). Route the write through txn.fold, or add the function to foldWriteSites with the reason a rebuild still reproduces what it writes",
				key, strings.Join(sites[key], ", "))
		}
	}
	for _, key := range slices.Sorted(maps.Keys(foldWriteSites)) {
		if _, ok := sites[key]; !ok {
			t.Errorf("foldWriteSites lists %s, which no longer writes a fold table: drop the entry", key)
		}
	}
}

func TestFoldPathPrimitivesAreCalledOnlyFromTheFold(t *testing.T) {
	t.Parallel()
	declared := map[string]bool{}
	callers := map[string]map[string]bool{}
	for name, file := range packageSources(t) {
		for _, decl := range file.Decls {
			owner := name + ":" + declOwner(decl)
			var body ast.Node = decl
			if fn, ok := decl.(*ast.FuncDecl); ok {
				if _, listed := foldPrimitives[fn.Name.Name]; listed && fn.Recv != nil {
					declared[fn.Name.Name] = true
				}
				// The declaration's own name is not a use.
				if fn.Body == nil {
					continue
				}
				body = fn.Body
			}
			// Every reference counts, a method value as much as a call: either
			// lets the function holding it run the primitive.
			ast.Inspect(body, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok {
					if _, listed := foldPrimitives[id.Name]; listed {
						if callers[id.Name] == nil {
							callers[id.Name] = map[string]bool{}
						}
						callers[id.Name][owner] = true
					}
				}
				return true
			})
		}
	}
	for _, p := range slices.Sorted(maps.Keys(foldPrimitives)) {
		if !declared[p] {
			t.Errorf("foldPrimitives lists %s, which the package no longer declares as a method: drop it, or name what replaced it", p)
		}
		allowed := foldPrimitives[p]
		for _, caller := range slices.Sorted(maps.Keys(callers[p])) {
			if !slices.Contains(allowed, caller) {
				t.Errorf("%s reaches the fold primitive %s; only %s may. A write path folds through txn.fold, so the effect lands in the changelog entry too",
					caller, p, strings.Join(allowed, " and "))
			}
		}
		for _, caller := range allowed {
			if !callers[p][caller] {
				t.Errorf("foldPrimitives says %s calls %s, and it no longer does: update the entry", caller, p)
			}
		}
	}
}

// foldWritesInPackage maps file:function to the fold tables its write
// statements name, and says so where a statement's table is a variable.
func foldWritesInPackage(t *testing.T) map[string][]string {
	t.Helper()
	files := packageSources(t)
	consts := packageStringConsts(files)
	tables := map[string]bool{}
	for _, table := range foldTables {
		tables[table] = true
	}
	sites := map[string][]string{}
	record := func(key, what string) {
		if !slices.Contains(sites[key], what) {
			sites[key] = append(sites[key], what)
		}
	}
	for name, file := range files {
		for _, decl := range file.Decls {
			if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.CONST {
				// A constant writes nothing until something uses it, so its
				// statement counts against each function that names it.
				continue
			}
			owner := name + ":" + declOwner(decl)
			ast.Inspect(decl, func(n ast.Node) bool {
				var sql string
				switch e := n.(type) {
				case *ast.BinaryExpr:
					if e.Op != token.ADD {
						return true
					}
					s, ok := concatenation(e, consts)
					if !ok {
						return true
					}
					sql = s
				case *ast.BasicLit:
					if e.Kind != token.STRING {
						return true
					}
					s, err := strconv.Unquote(e.Value)
					if err != nil {
						t.Fatalf("%s: unquote %s: %v", owner, e.Value, err)
					}
					sql = s
				case *ast.Ident:
					sql = consts[e.Name]
				default:
					return true
				}
				for _, m := range foldWriteRe.FindAllStringSubmatch(sql, -1) {
					switch table := tableName(m[1]); {
					case table == dynamicTable || strings.HasPrefix(table, "%"):
						record(owner, "a table named at run time")
					case tables[table]:
						record(owner, table)
					}
				}
				// A concatenation is read whole: its operands alone would
				// lose the table a variable supplies.
				_, concat := n.(*ast.BinaryExpr)
				return !concat
			})
		}
	}
	for key := range sites {
		sort.Strings(sites[key])
	}
	return sites
}

// tableName is the table a matched statement head names: the schema and the
// quotes dropped, folded to lower case.
func tableName(head string) string {
	if i := strings.LastIndex(head, "."); i >= 0 {
		head = head[i+1:]
	}
	return strings.ToLower(strings.Trim(strings.TrimSpace(head), `"`))
}

// concatenation joins a string concatenation's operands, a string constant
// by its value and anything else as dynamicTable. It reports false for an
// expression with no string literal or constant in it, which is not SQL
// being built.
func concatenation(e *ast.BinaryExpr, consts map[string]string) (string, bool) {
	var b strings.Builder
	literal := false
	var walk func(ast.Expr)
	walk = func(x ast.Expr) {
		switch v := x.(type) {
		case *ast.BinaryExpr:
			if v.Op == token.ADD {
				walk(v.X)
				walk(v.Y)
				return
			}
		case *ast.ParenExpr:
			walk(v.X)
			return
		case *ast.BasicLit:
			if v.Kind == token.STRING {
				if s, err := strconv.Unquote(v.Value); err == nil {
					literal = true
					b.WriteString(s)
					return
				}
			}
		case *ast.Ident:
			if s, ok := consts[v.Name]; ok {
				literal = true
				b.WriteString(s)
				return
			}
		}
		b.WriteString(dynamicTable)
	}
	walk(e)
	return b.String(), literal
}

// declOwner names the function a top-level declaration is, or the first
// name a var or const declaration binds.
func declOwner(decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		return d.Name.Name
	case *ast.GenDecl:
		for _, spec := range d.Specs {
			if vs, ok := spec.(*ast.ValueSpec); ok && len(vs.Names) > 0 {
				return vs.Names[0].Name
			}
		}
	}
	return "(package)"
}

// packageStringConsts is every package-level constant whose value is a
// string literal or a concatenation of literals and such constants, so a
// statement built from constants still names its table.
func packageStringConsts(files map[string]*ast.File) map[string]string {
	pending := map[string]ast.Expr{}
	for _, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, v := range vs.Values {
					if i < len(vs.Names) {
						pending[vs.Names[i].Name] = v
					}
				}
			}
		}
	}
	out := map[string]string{}
	// A constant may name one declared after it, so resolve until a pass
	// resolves nothing new.
	for progress := true; progress; {
		progress = false
		for name, v := range pending {
			if s, ok := constString(v, out); ok {
				out[name] = s
				delete(pending, name)
				progress = true
			}
		}
	}
	return out
}

// constString evaluates a string constant expression over the constants
// resolved so far, and reports false for anything else.
func constString(e ast.Expr, consts map[string]string) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		return s, err == nil
	case *ast.Ident:
		s, ok := consts[v.Name]
		return s, ok
	case *ast.ParenExpr:
		return constString(v.X, consts)
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		x, ok := constString(v.X, consts)
		if !ok {
			return "", false
		}
		y, ok := constString(v.Y, consts)
		return x + y, ok
	}
	return "", false
}

// packageSources parses every non-test Go file of the package, keyed by file
// name.
func packageSources(t *testing.T) map[string]*ast.File {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	out := map[string]*ast.File{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		if f.Name.Name != "engine" {
			continue
		}
		out[name] = f
	}
	if len(out) == 0 {
		t.Fatal("found no source files in the package directory")
	}
	return out
}
