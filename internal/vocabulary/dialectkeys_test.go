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

// closedKeySets are the closed key sets of the declaration documents, by the
// name the golden records them under, which is the variable's.
var closedKeySets = map[string]map[string]bool{
	"envelopeKeys": envelopeKeys, "metadataKeys": metadataKeys,
	"packageDataKeys": packageDataKeys, "packageRetiredKeys": packageRetiredKeys,
	"authorityDataKeys": authorityDataKeys, "actorDataKeys": actorDataKeys,
	"datatypeDataKeys": datatypeDataKeys, "capabilityDataKeys": capabilityDataKeys,
	"traitVariantKeys": traitVariantKeys,
	"typeDataKeys":     typeDataKeys, "kindRetiredKeys": kindRetiredKeys,
	"namesKeys": namesKeys, "indexKeys": indexKeys, "labelKeys": labelKeys, "idsKeys": idsKeys,
	"propKeys": propKeys, "objectPropKeys": objectPropKeys, "referencePropKeys": referencePropKeys,
	"linkPropKeys": linkPropKeys, "enumValueKeys": enumValueKeys,
	"machineKeys": machineKeys, "transitionKeys": transitionKeys,
	"mappingDataKeys": mappingDataKeys, "matchRuleKeys": matchRuleKeys, "mapRuleKeys": mapRuleKeys,
	"functionDataKeys": functionDataKeys, "functionArgKeys": functionArgKeys,
	"functionOfferKeys": functionOfferKeys, "functionPermissionKeys": functionPermissionKeys,
	"functionReadsKeys": functionReadsKeys, "functionBudgetKeys": functionBudgetKeys,
	"agentDataKeys": agentDataKeys, "agentPermissionKeys": agentPermissionKeys,
	"agentBudgetKeys": agentBudgetKeys, "agentCompactionKeys": agentCompactionKeys,
	"agentToolKeys": agentToolKeys, "agentToolsFromKeys": agentToolsFromKeys,
	"agentContextKeys": agentContextKeys, "agentPinKeys": agentPinKeys,
	"bundleDataKeys": bundleDataKeys, "bundleInputKeys": bundleInputKeys,
	"featureScopeKeys": featureScopeKeys, "oauth2MetaKeys": oauth2MetaKeys,
}

// dialectGolden is the golden's shape: the number and the sets it covers.
type dialectGolden struct {
	Dialect int64               `json:"dialect"`
	Keys    map[string][]string `json:"keys"`
}

func currentDialect() dialectGolden {
	g := dialectGolden{Dialect: Dialect, Keys: map[string][]string{}}
	for name, set := range closedKeySets {
		g.Keys[name] = slices.Sorted(maps.Keys(set))
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
// and key sets that differ from the earlier ones carry a higher number.
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
		added, removed := diffKeys(was, is)
		moved = append(moved, fmt.Sprintf("%s: added %v, removed %v", name, added, removed))
	}
	if len(moved) > 0 && now.Dialect == earlier.Dialect {
		problems = append(problems, fmt.Sprintf(
			"the declaration keys changed and vocabulary.Dialect stayed at %d: raise it to %d (internal/vocabulary/dialect.go), because a server that does not know a key refuses the document carrying it:\n  %s",
			now.Dialect, now.Dialect+1, strings.Join(moved, "\n  ")))
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
	if golden.Dialect != now.Dialect || !maps.EqualFunc(golden.Keys, now.Keys, slices.Equal) {
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
	for _, name := range declared {
		if _, ok := closedKeySets[name]; !ok {
			t.Errorf("%s is a closed key set the dialect golden does not hold: add it to closedKeySets (dialectkeys_test.go) and raise vocabulary.Dialect if a document may now carry a key it did not", name)
		}
	}
	if len(declared) != len(closedKeySets) {
		t.Errorf("closedKeySets lists %d sets and the source declares %d", len(closedKeySets), len(declared))
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
