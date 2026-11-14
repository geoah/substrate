package vocabulary

// A NEW KEY RAISES THE DIALECT (decision record 0268). Every closed key set a
// declaration document is held to is recorded in testdata/dialect.golden.json
// under the dialect number it was admitted at. A server refuses a document
// carrying a key it does not know, so a key added, removed or moved between
// sets is a declaration format an older server cannot read, and the number a
// package states, and a server publishes, has to say so.
//
// TestDialectKeysMatchTheGolden holds the sets to the golden, and its
// `-update-dialect` refuses to rewrite sets that moved under an unchanged
// number. TestEveryClosedKeySetIsInTheDialectGolden refuses a key set this
// package declares and the golden does not hold. `mise run dialect:check`
// holds the golden against the base branch's (TestDialectAgainstTheBase), so
// two branches that each add a key and each raise the number from 3 to 4 do
// not merge into two keys under one number.

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var updateDialect = flag.Bool("update-dialect", false,
	"rewrite testdata/dialect.golden.json, refusing key sets that moved under an unchanged vocabulary.Dialect")

const dialectGoldenPath = "testdata/dialect.golden.json"

// dialectGolden is the golden's shape: the number, the sets it covers, and
// the dialect each key after dialect 2 arrived in (keyIntroducedAt), by set.
type dialectGolden struct {
	Dialect      int64                       `json:"dialect"`
	Keys         map[string][]string         `json:"keys"`
	IntroducedAt map[string]map[string]int64 `json:"introducedAt"`
}

func currentDialect() dialectGolden {
	g := dialectGolden{Dialect: Dialect, Keys: map[string][]string{}, IntroducedAt: map[string]map[string]int64{}}
	for name, set := range closedKeySets {
		g.Keys[name] = slices.Sorted(maps.Keys(set))
	}
	for name, keys := range keyIntroducedAt {
		g.IntroducedAt[name] = maps.Clone(keys)
	}
	return g
}

func readDialectGolden(t *testing.T, path string) dialectGolden {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var g dialectGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return g
}

// dialectProblems is the rule both checks apply: the number never goes down,
// key sets that differ from the earlier ones carry a higher number, a key
// added since enters keyIntroducedAt at that number, and a key's dialect
// there never moves. Without the last two the table would hold only what a
// step remembered to write, and a package could state a dialect below a key
// it carries.
// enforcedBeforeTheGolden are the closed key sets the loader held inline,
// a key compared to the words it may be, before they were registered (the
// second review of decision 0268's step), each with exactly the keys it held
// then: their keys were refused then as now, so the dialect-3 golden gaining
// one, as it was, changed no document a server reads. The list is closed and
// pinned: it applies against a golden at enforcedBeforeTheGoldenAt alone, to
// a set with exactly these keys, so a set added to it, or a key added to one
// of its sets, is refused under an unchanged number like any other change.
var enforcedBeforeTheGolden = map[string][]string{
	"quantityBoundKeys":         {"unit", "value"},
	"shippedRecordKeys":         {"data", "kind", "metadata"},
	"shippedRecordMetadataKeys": {"id"},
	"shippedRecordDataKeys":     {"properties"},
	"agentParamKeys":            {"engineGuidance", "maxTokens", "reasoningEffort", "reasoningSummary", "temperature"},
}

// enforcedBeforeTheGoldenAt is the dialect of the golden the sets above were
// registered after: the first, step 29's own.
const enforcedBeforeTheGoldenAt = 3

// enforcedBefore reports whether set name, holding keys now and absent from
// the earlier golden, is one the loader held inline before the golden.
func enforcedBefore(earlier dialectGolden, name string, keys []string) bool {
	if _, held := earlier.Keys[name]; held {
		return false
	}
	pinned, ok := enforcedBeforeTheGolden[name]
	return ok && earlier.Dialect == enforcedBeforeTheGoldenAt && slices.Equal(pinned, keys)
}

func dialectProblems(earlier, now dialectGolden) []string {
	var problems []string
	if now.Dialect < earlier.Dialect {
		problems = append(problems, fmt.Sprintf("vocabulary.Dialect went from %d down to %d: a dialect only goes up", earlier.Dialect, now.Dialect))
	}
	var moved []string
	for _, name := range slices.Sorted(maps.Keys(mergeNames(earlier.Keys, now.Keys))) {
		was, is := earlier.Keys[name], now.Keys[name]
		if slices.Equal(was, is) {
			continue
		}
		if enforcedBefore(earlier, name, is) {
			continue
		}
		added, removed := diffKeys(was, is)
		moved = append(moved, fmt.Sprintf("%s: added %v, removed %v", name, added, removed))
	}
	if len(moved) > 0 && now.Dialect == earlier.Dialect {
		problems = append(problems, fmt.Sprintf(
			"the declaration keys changed and vocabulary.Dialect stayed at %d: raise it to %d (internal/vocabulary/dialect.go), because a server that does not know a key refuses the document carrying it:\n  %s",
			now.Dialect, now.Dialect+1, strings.Join(moved, "\n  ")))
	}
	// A golden from before the table was held names no key's dialect.
	if earlier.IntroducedAt == nil {
		return problems
	}
	for _, name := range slices.Sorted(maps.Keys(now.Keys)) {
		if enforcedBefore(earlier, name, now.Keys[name]) {
			continue
		}
		added, _ := diffKeys(earlier.Keys[name], now.Keys[name])
		for _, k := range added {
			if at := now.IntroducedAt[name][k]; at != now.Dialect {
				problems = append(problems, fmt.Sprintf(
					"%s.%s is a key added since dialect %d, and keyIntroducedAt (internal/vocabulary/dialect.go) puts it at %d: enter it at %d, the dialect it raises the number to, so a package that carries it states at least that",
					name, k, earlier.Dialect, at, now.Dialect))
			}
		}
	}
	// A line given to a key the earlier golden already held says the key's
	// meaning changed (decision 0290's `writer:`), so it takes the number
	// that change raises the dialect to, and only in a change that raises
	// it; held from then on as every line is, below.
	for _, name := range slices.Sorted(maps.Keys(now.IntroducedAt)) {
		for _, k := range slices.Sorted(maps.Keys(now.IntroducedAt[name])) {
			if _, recorded := earlier.IntroducedAt[name][k]; recorded || !slices.Contains(earlier.Keys[name], k) {
				continue
			}
			if at := now.IntroducedAt[name][k]; at != now.Dialect || now.Dialect == earlier.Dialect {
				problems = append(problems, fmt.Sprintf(
					"keyIntroducedAt gives %s.%s, a key dialect %d already held, a line at %d: a key whose meaning changes takes its line at the number the change raises the dialect to, %d, in the change that raises it",
					name, k, earlier.Dialect, at, earlier.Dialect+1))
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(earlier.IntroducedAt)) {
		for _, k := range slices.Sorted(maps.Keys(earlier.IntroducedAt[name])) {
			was := earlier.IntroducedAt[name][k]
			is, kept := now.IntroducedAt[name][k]
			switch {
			case kept && is != was:
				problems = append(problems, fmt.Sprintf(
					"keyIntroducedAt moves %s.%s from dialect %d to %d: the dialect a key arrived in never moves", name, k, was, is))
			case !kept && slices.Contains(now.Keys[name], k):
				// A key that left its set takes its line with it, a change
				// of the set the number answers for; one still in it keeps
				// its line, or a package could state less than it needs.
				problems = append(problems, fmt.Sprintf(
					"keyIntroducedAt drops %s.%s, which arrived in dialect %d and is still in its set: the dialect a key arrived in never goes", name, k, was))
			}
		}
	}
	return problems
}

func mergeNames(a, b map[string][]string) map[string]bool {
	out := map[string]bool{}
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}

func diffKeys(was, is []string) (added, removed []string) {
	for _, k := range is {
		if !slices.Contains(was, k) {
			added = append(added, k)
		}
	}
	for _, k := range was {
		if !slices.Contains(is, k) {
			removed = append(removed, k)
		}
	}
	return added, removed
}

func TestDialectKeysMatchTheGolden(t *testing.T) {
	now := currentDialect()
	if *updateDialect {
		// The first golden has nothing to be held to.
		if _, err := os.Stat(dialectGoldenPath); err == nil {
			if problems := dialectProblems(readDialectGolden(t, dialectGoldenPath), now); len(problems) > 0 {
				t.Fatalf("%s is not rewritten:\n  %s", dialectGoldenPath, strings.Join(problems, "\n  "))
			}
		}
		raw, err := json.MarshalIndent(now, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dialectGoldenPath, append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	golden := readDialectGolden(t, dialectGoldenPath)
	if problems := dialectProblems(golden, now); len(problems) > 0 {
		t.Fatalf("%s", strings.Join(problems, "\n"))
	}
	if golden.Dialect != now.Dialect || !maps.EqualFunc(golden.Keys, now.Keys, slices.Equal) ||
		!maps.EqualFunc(golden.IntroducedAt, now.IntroducedAt, maps.Equal) {
		t.Fatalf("%s does not match the key sets and the dialect in this tree: regenerate it with "+
			"`go test ./internal/vocabulary/ -run TestDialectKeysMatchTheGolden -update-dialect`", dialectGoldenPath)
	}
}

// TestEveryClosedKeySetIsInTheDialectGolden reads this package's source for
// every package-level `…Keys = map[string]bool{…}`, so a key set added
// without a place in closedKeySets fails here instead of escaping the rule.
func TestEveryClosedKeySetIsInTheDialectGolden(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var declared []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if !strings.HasSuffix(name.Name, "Keys") || i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.CompositeLit)
					if !ok {
						continue
					}
					if m, ok := lit.Type.(*ast.MapType); ok && fmt.Sprint(m.Key) == "string" && fmt.Sprint(m.Value) == "bool" {
						declared = append(declared, name.Name)
					}
				}
			}
		}
	}
	// A closed check written inline, a key compared to the words it may be
	// or switched over them, is a set no golden sees: register it as a
	// …Keys set (inlineKeyChecks).
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, at := range inlineKeyChecks(file) {
			t.Errorf("%s compares a ranged key to the words it may be inline: a closed key set is a registered …Keys set the dialect golden holds",
				fset.Position(at))
		}
	}
	for _, name := range declared {
		if _, ok := closedKeySets[name]; !ok {
			t.Errorf("%s is a closed key set the dialect golden does not hold: add it to closedKeySets (dialectkeys_test.go) and raise vocabulary.Dialect if a document may now carry a key it did not", name)
		}
	}
	if len(declared) != len(closedKeySets) {
		t.Errorf("closedKeySets lists %d sets and the source declares %d", len(closedKeySets), len(declared))
	}
}

// Every line of keyIntroducedAt names a key of a registered set, at a
// dialect after the number was held to the keys and no later than the one
// this loader reads.
func TestEveryKeyIntroductionNamesARegisteredKey(t *testing.T) {
	for name, keys := range keyIntroducedAt {
		set, ok := closedKeySets[name]
		if !ok {
			t.Errorf("keyIntroducedAt names %s, which is no registered key set", name)
			continue
		}
		for k, at := range keys {
			if !set[k] {
				t.Errorf("keyIntroducedAt names %s.%s, which the set does not hold", name, k)
			}
			if at < 3 || at > Dialect {
				t.Errorf("keyIntroducedAt puts %s.%s at dialect %d: a key arrives after dialect 2 and no later than %d", name, k, at, Dialect)
			}
		}
	}
}

// TestDialectAgainstTheBase is `mise run dialect:check`: the golden in the
// tree against the base branch's, through dialectProblems. It runs only
// where DIALECT_BASE_GOLDEN names the base's golden.
func TestDialectAgainstTheBase(t *testing.T) {
	path := os.Getenv("DIALECT_BASE_GOLDEN")
	if path == "" {
		t.Skip("DIALECT_BASE_GOLDEN is unset: this runs under mise run dialect:check")
	}
	base := readDialectGolden(t, path)
	if problems := dialectProblems(base, readDialectGolden(t, dialectGoldenPath)); len(problems) > 0 {
		t.Fatalf("against the base branch:\n  %s", strings.Join(problems, "\n  "))
	}
}

// A set a golden gains under the same number is a change of the declaration
// format, unless the loader held it inline before it was registered
// (enforcedBeforeTheGolden): then no document a server reads changed.
func TestANewlyRegisteredSetIsAChangeUnlessItWasEnforcedBefore(t *testing.T) {
	earlier := dialectGolden{Dialect: 3, Keys: map[string][]string{"propKeys": {"type"}}}
	for name, want := range map[string]bool{"newBlockKeys": true, "quantityBoundKeys": false} {
		now := dialectGolden{Dialect: 3, Keys: map[string][]string{"propKeys": {"type"}, name: {"unit", "value"}}}
		if got := len(dialectProblems(earlier, now)) > 0; got != want {
			t.Errorf("%s registered under an unchanged number: refused %v, want %v", name, got, want)
		}
	}
}

// The golden holds keyIntroducedAt: a key added since the earlier golden
// enters it at the dialect it raises the number to, and a key's dialect there
// never moves, so the table is what a package's keys are read by and not
// what a step remembered to write.
func TestAKeyAddedEntersKeyIntroducedAtAtTheNumberItRaises(t *testing.T) {
	earlier := dialectGolden{
		Dialect:      3,
		Keys:         map[string][]string{"packageDataKeys": {"dialect", "version"}},
		IntroducedAt: map[string]map[string]int64{"packageDataKeys": {"dialect": 3}},
	}
	now := dialectGolden{
		Dialect:      4,
		Keys:         map[string][]string{"packageDataKeys": {"dialect", "retention", "version"}},
		IntroducedAt: map[string]map[string]int64{"packageDataKeys": {"dialect": 3, "retention": 4}},
	}
	if problems := dialectProblems(earlier, now); len(problems) != 0 {
		t.Fatalf("a key entered at the number it raises was refused: %v", problems)
	}
	unentered := now
	unentered.IntroducedAt = map[string]map[string]int64{"packageDataKeys": {"dialect": 3}}
	if problems := dialectProblems(earlier, unentered); len(problems) != 1 || !strings.Contains(problems[0], "packageDataKeys.retention is a key added since dialect 3") {
		t.Fatalf("a key added and not entered: %v", problems)
	}
	moved := now
	moved.IntroducedAt = map[string]map[string]int64{"packageDataKeys": {"dialect": 4, "retention": 4}}
	if problems := dialectProblems(earlier, moved); len(problems) != 1 || !strings.Contains(problems[0], "the dialect a key arrived in never moves") {
		t.Fatalf("a key's dialect that moved: %v", problems)
	}
	dropped := now
	dropped.IntroducedAt = map[string]map[string]int64{"packageDataKeys": {"retention": 4}}
	if problems := dialectProblems(earlier, dropped); len(problems) != 1 || !strings.Contains(problems[0], "keyIntroducedAt drops packageDataKeys.dialect") {
		t.Fatalf("a key's line dropped while the key stays: %v", problems)
	}
	// A key the earlier golden held, whose meaning a raise changes, takes a
	// line at the new number, and at no other, and never without a raise.
	changed := now
	changed.IntroducedAt = map[string]map[string]int64{"packageDataKeys": {"dialect": 3, "retention": 4, "version": 4}}
	if problems := dialectProblems(earlier, changed); len(problems) != 0 {
		t.Fatalf("an existing key's line at the number its change raises: %v", problems)
	}
	early := now
	early.IntroducedAt = map[string]map[string]int64{"packageDataKeys": {"dialect": 3, "retention": 4, "version": 3}}
	if problems := dialectProblems(earlier, early); len(problems) != 1 || !strings.Contains(problems[0], "a key dialect 3 already held, a line at 3") {
		t.Fatalf("an existing key's line below the number its change raises: %v", problems)
	}
	unraised := dialectGolden{
		Dialect:      3,
		Keys:         earlier.Keys,
		IntroducedAt: map[string]map[string]int64{"packageDataKeys": {"dialect": 3, "version": 3}},
	}
	if problems := dialectProblems(earlier, unraised); len(problems) != 1 || !strings.Contains(problems[0], "packageDataKeys.version, a key dialect 3 already held") {
		t.Fatalf("an existing key's line with no raise: %v", problems)
	}
	// A key that left its set takes its line with it.
	removed := dialectGolden{
		Dialect:      4,
		Keys:         map[string][]string{"packageDataKeys": {"version"}},
		IntroducedAt: map[string]map[string]int64{},
	}
	if problems := dialectProblems(earlier, removed); len(problems) != 0 {
		t.Fatalf("a key removed from its set, with its line: %v", problems)
	}
	// A set registered after the loader held it inline carries no new key.
	registered := now
	registered.Keys = maps.Clone(now.Keys)
	registered.Keys["quantityBoundKeys"] = []string{"unit", "value"}
	if problems := dialectProblems(earlier, registered); len(problems) != 0 {
		t.Fatalf("a set enforced before the golden, registered: %v", problems)
	}
}

// inlineKeyChecks finds the closed key checks written inline in one file,
// in the two shapes one takes: a variable a range loop binds (its key, or its
// value where the key is `_`) compared with `!=` to a word, in either order;
// or switched over with a word among its cases and a `default` for the rest.
// A word is a non-empty string literal or a constant. A loop that looks the
// variable up in a registered set (closedKeySets) is checked by that set,
// its switch the per-key reading of what the set admitted, and a loop over a
// literal list compares a list it holds itself. It does not see a check
// written with `==` alone, a key read without a range loop, or a comparison
// to a variable: telling those from a value special-cased (a property named
// `body`) takes meaning the source does not carry, so review holds them.
func inlineKeyChecks(file *ast.File) []token.Pos {
	var out []token.Pos
	word := func(e ast.Expr) bool {
		switch e := e.(type) {
		case *ast.BasicLit:
			return e.Kind == token.STRING && e.Value != `""`
		case *ast.Ident:
			return e.Obj != nil && e.Obj.Kind == ast.Con
		}
		return false
	}
	ast.Inspect(file, func(n ast.Node) bool {
		loop, ok := n.(*ast.RangeStmt)
		if !ok {
			return true
		}
		if _, literal := loop.X.(*ast.CompositeLit); literal {
			return true
		}
		bound := map[string]bool{}
		for _, e := range []ast.Expr{loop.Key, loop.Value} {
			if id, ok := e.(*ast.Ident); ok && id.Name != "_" {
				bound[id.Name] = true
			}
		}
		if len(bound) == 0 {
			return true
		}
		isBound := func(e ast.Expr) bool {
			id, ok := e.(*ast.Ident)
			return ok && bound[id.Name]
		}
		registered := false
		ast.Inspect(loop.Body, func(n ast.Node) bool {
			if ix, ok := n.(*ast.IndexExpr); ok {
				if set, ok := ix.X.(*ast.Ident); ok && closedKeySets[set.Name] != nil && isBound(ix.Index) {
					registered = true
				}
			}
			return true
		})
		if registered {
			return true
		}
		ast.Inspect(loop.Body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.BinaryExpr:
				if n.Op == token.NEQ && ((isBound(n.X) && word(n.Y)) || (isBound(n.Y) && word(n.X))) {
					out = append(out, n.Pos())
				}
			case *ast.SwitchStmt:
				if !isBound(n.Tag) {
					return true
				}
				worded, defaulted := false, false
				for _, c := range n.Body.List {
					clause := c.(*ast.CaseClause)
					defaulted = defaulted || clause.List == nil
					for _, e := range clause.List {
						worded = worded || word(e)
					}
				}
				if worded && defaulted {
					out = append(out, n.Pos())
				}
			}
			return true
		})
		return true
	})
	return out
}

// The exemption is pinned: a name added to enforcedBeforeTheGolden, a key
// added to one of its sets, or a golden past the one it was made against
// gives a set registered under an unchanged number no pass.
func TestTheSetsEnforcedBeforeTheGoldenArePinned(t *testing.T) {
	want := map[string][]string{
		"quantityBoundKeys":         {"unit", "value"},
		"shippedRecordKeys":         {"data", "kind", "metadata"},
		"shippedRecordMetadataKeys": {"id"},
		"shippedRecordDataKeys":     {"properties"},
		"agentParamKeys":            {"engineGuidance", "maxTokens", "reasoningEffort", "reasoningSummary", "temperature"},
	}
	if !maps.EqualFunc(enforcedBeforeTheGolden, want, slices.Equal) || enforcedBeforeTheGoldenAt != 3 {
		t.Fatalf("enforcedBeforeTheGolden is %v at dialect %d: it is closed, pinned to the sets the loader held inline before the dialect-3 golden",
			enforcedBeforeTheGolden, enforcedBeforeTheGoldenAt)
	}
	earlier := dialectGolden{Dialect: 3, Keys: map[string][]string{"propKeys": {"type"}}}
	with := func(name string, keys ...string) dialectGolden {
		return dialectGolden{Dialect: earlier.Dialect, Keys: map[string][]string{"propKeys": {"type"}, name: keys}}
	}
	if problems := dialectProblems(earlier, with("agentParamKeys", want["agentParamKeys"]...)); len(problems) != 0 {
		t.Fatalf("a pinned set registered as it was: %v", problems)
	}
	if problems := dialectProblems(earlier, with("agentParamKeys", append(slices.Clone(want["agentParamKeys"]), "topP")...)); len(problems) == 0 {
		t.Fatal("a pinned set registered with a key it did not hold passed")
	}
	if problems := dialectProblems(earlier, with("newBlockKeys", "unit", "value")); len(problems) == 0 {
		t.Fatal("a set no pin names passed")
	}
	later := earlier
	later.Dialect = 4
	if problems := dialectProblems(later, dialectGolden{Dialect: 4, Keys: map[string][]string{"propKeys": {"type"}, "quantityBoundKeys": {"unit", "value"}}}); len(problems) == 0 {
		t.Fatal("a pinned set registered against a golden past dialect 3 passed")
	}
}
