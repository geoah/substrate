package engine

import (
	"fmt"
	"strings"
	"testing"
)

// Every list-token version this tree has minted names the repository
// migration that starts the paged drains over for it, a migration of its
// own, later in the chain than the one the version before named: a drain's
// saved continuation is the engine's, and a version raised without its own
// migration leaves every drain that saved a token of the version before
// refused at each retry. Raising keysetVersion fails here until its
// migration lands and is listed in keysetDrainsStartOver.
func TestEveryKeysetVersionStartsTheDrainsOver(t *testing.T) {
	chain := map[string]int{}
	for _, m := range repositoryMigrations {
		chain[m.Name] = m.Version
	}
	for _, problem := range keysetMigrationProblems(keysetDrainsStartOver, keysetVersion, chain) {
		t.Error(problem)
	}
}

// The check holds each way a table can go wrong.
func TestTheKeysetGuardRefusesAVersionWithoutAMigrationOfItsOwn(t *testing.T) {
	chain := map[string]int{"seven": 7, "nine": 9}
	for name, c := range map[string]struct {
		table map[int]string
		upTo  int
		says  string
	}{
		"a version with none":              {map[int]string{1: "", 2: "seven"}, 3, "version 3 names no"},
		"a migration named twice":          {map[int]string{1: "", 2: "seven", 3: "seven"}, 3, "version 3 names \"seven\""},
		"a migration earlier in the chain": {map[int]string{1: "", 2: "nine", 3: "seven"}, 3, "version 3 names \"seven\""},
		"no such migration":                {map[int]string{1: "", 2: "eight"}, 2, "which is no repository migration"},
		"the first naming one":             {map[int]string{1: "seven"}, 1, "version 1 is the first"},
	} {
		t.Run(name, func(t *testing.T) {
			got := strings.Join(keysetMigrationProblems(c.table, c.upTo, chain), "\n")
			if !strings.Contains(got, c.says) {
				t.Fatalf("the check says %q, want %q", got, c.says)
			}
		})
	}
	if got := keysetMigrationProblems(map[int]string{1: "", 2: "seven", 3: "nine"}, 3, chain); len(got) != 0 {
		t.Fatalf("a sound table is refused: %v", got)
	}
}

// keysetMigrationProblems is what is wrong with table for list-token
// versions 1 to upTo, against the repository migrations by name.
func keysetMigrationProblems(table map[int]string, upTo int, chain map[string]int) []string {
	var out []string
	previous := 0
	for v := 1; v <= upTo; v++ {
		name, ok := table[v]
		switch {
		case !ok:
			out = append(out, fmt.Sprintf("list-token version %d names no repository migration that starts the paged drains over: add one, as migration 7 (start_paged_drains_over) does for version 2, and list it in keysetDrainsStartOver", v))
		case v == 1:
			if name != "" {
				out = append(out, fmt.Sprintf("version 1 is the first and starts nothing over, but names %q", name))
			}
		case chain[name] == 0:
			out = append(out, fmt.Sprintf("list-token version %d names %q, which is no repository migration", v, name))
		case chain[name] <= previous:
			out = append(out, fmt.Sprintf("list-token version %d names %q, repository migration %d, which a version before it named, or which comes before that one's: each version needs a migration of its own, later in the chain", v, name, chain[name]))
		default:
			previous = chain[name]
		}
	}
	if len(table) != upTo {
		out = append(out, fmt.Sprintf("keysetDrainsStartOver lists %d versions; this tree has minted %d", len(table), upTo))
	}
	return out
}
