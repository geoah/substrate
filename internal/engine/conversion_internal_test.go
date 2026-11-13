package engine

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
)

// The chain this binary carries is numbered from 2 with no gap, converts to
// the dialect the binary stamps, and has one file per step named for it,
// `conversion_NNNN_<name>.go`, the way the repository migrations do
// (TestRepositoryMigrationsMatchTheirFiles).
func TestTheConversionChainIsNumberedAndFiled(t *testing.T) {
	t.Parallel()
	if err := checkConversionSteps(conversionSteps); err != nil {
		t.Fatal(err)
	}
	if got := chainDialect(conversionSteps); got != maxChangelogDialect {
		t.Fatalf("the chain converts to dialect %d and the binary stamps %d: a step raises maxChangelogDialect to its own dialect", got, maxChangelogDialect)
	}
	files, err := filepath.Glob("conversion_[0-9][0-9][0-9][0-9]_*.go")
	if err != nil {
		t.Fatal(err)
	}
	named := map[string]bool{}
	for _, f := range files {
		if !strings.HasSuffix(f, "_test.go") {
			named[f] = true
		}
	}
	for _, s := range conversionSteps {
		f := conversionFile(s)
		if !named[f] {
			t.Errorf("step %d (%s) has no file %s", s.Dialect, s.Name, f)
		}
		delete(named, f)
	}
	for f := range named {
		t.Errorf("%s names no step of conversionSteps", f)
	}
}

func conversionFile(s conversionStep) string {
	return fmt.Sprintf("conversion_%04d_%s.go", s.Dialect, s.Name)
}

// checkConversionSteps refuses a chain a step could not be found in by its
// number: one that starts anywhere but 2, skips, repeats a name or has a step
// that rewrites nothing.
func TestAConversionChainIsRefusedOutOfOrder(t *testing.T) {
	t.Parallel()
	line := func(*changelogfile.Entry) error { return nil }
	for name, steps := range map[string][]conversionStep{
		"starts at 3":    {{Dialect: 3, Name: "a", Line: line}},
		"skips one":      {{Dialect: 2, Name: "a", Line: line}, {Dialect: 4, Name: "b", Line: line}},
		"repeats a name": {{Dialect: 2, Name: "a", Line: line}, {Dialect: 3, Name: "a", Line: line}},
		"has no line":    {{Dialect: 2, Name: "a"}},
	} {
		if err := checkConversionSteps(steps); err == nil {
			t.Errorf("a chain that %s was admitted", name)
		}
	}
	ok := []conversionStep{{Dialect: 2, Name: "a", Line: line}, {Dialect: 3, Name: "b", Line: line}}
	if err := checkConversionSteps(ok); err != nil {
		t.Fatalf("a chain numbered 2, 3 was refused: %v", err)
	}
	if got := chainDialect(ok); got != 3 {
		t.Fatalf("the chain converts to %d, want 3", got)
	}
}

// A repository runs the steps above its stamp, and an unstamped changelog
// holds dialect-1 lines, so it runs them all.
func TestARepositoryRunsTheStepsAboveItsStamp(t *testing.T) {
	t.Parallel()
	line := func(*changelogfile.Entry) error { return nil }
	steps := []conversionStep{{Dialect: 2, Name: "a", Line: line}, {Dialect: 3, Name: "b", Line: line}}
	for stored, want := range map[int]string{0: "2 (a), 3 (b)", 1: "2 (a), 3 (b)", 2: "3 (b)", 3: ""} {
		if got := conversionNames(pendingConversions(steps, stored)); got != want {
			t.Errorf("stamped %d: pending %q, want %q", stored, got, want)
		}
	}
}
