package engine

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// liveCaseFixtures names, for each live case of the package, the test that
// runs its fixture without a key up to its first call
// (livefixture_db_test.go).
var liveCaseFixtures = map[string]string{
	"TestLiveAgentChainAcrossWires": "TestTheLiveAgentChainInstallsAndReachesItsFirstCall",
	"TestLiveCoachPersonas":         "TestTheCoachPersonasSeedAndReachTheirFirstCall",
}

// EVERY LIVE CASE NAMES THE TEST OF ITS FIXTURE (#847). A live case runs
// only where the keys are, weekly, so its fixture is held on every run by a
// test of its own. A TestLive function this table does not name fails here,
// and so does an entry whose case or fixture test no file declares.
func TestEveryLiveCaseNamesTheTestOfItsFixture(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	fset := token.NewFileSet()
	for _, file := range files {
		f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") {
				declared[fn.Name.Name] = true
			}
		}
	}
	for name := range declared {
		if strings.HasPrefix(name, "TestLive") && liveCaseFixtures[name] == "" {
			t.Errorf("%s is a live case liveCaseFixtures does not name: give it a test that runs its fixture without a key, and name it there", name)
		}
	}
	for live, fixture := range liveCaseFixtures {
		if !declared[live] {
			t.Errorf("liveCaseFixtures names %s, which no test file declares", live)
		}
		if !declared[fixture] {
			t.Errorf("liveCaseFixtures names %s as the fixture test of %s, and no test file declares it", fixture, live)
		}
	}
}
