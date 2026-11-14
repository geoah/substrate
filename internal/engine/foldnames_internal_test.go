package engine

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// EVERY FOLD READS THE SPELLINGS. A name the repository answers to spells
// `~` only while it holds no package published under it (vocabulary.Registry
// .Spellings), so a fold reads ds.names or t.names, never the names the
// repository answers to. Those are read where a name is shown or kept (the
// repository's info and name) and where the registry is handed them to
// fold through its own spellings; any other read of them here fails, as
// does spreading a registry's or a repository's bare names into a call
// anywhere in the tree. RepositoryInfo has no accessor for its bare names.
func TestEveryFoldReadsTheSpellings(t *testing.T) {
	allowed := map[string]int{
		"dataset.go":         1, // Repository: the names it shows
		"engine.go":          1, // SetNames: the registry folds through Spellings
		"repodir.go":         1, // SetNames
		"seed.go":            1, // SetNames
		"repositorynames.go": 4, // its definition, name, names and t.names
	}
	reads := regexp.MustCompile(`\banswersTo\b`)
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(src), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			got[f] += len(reads.FindAllString(line, -1))
		}
	}
	for f, n := range got {
		if n != allowed[f] {
			t.Errorf("%s reads the names the repository answers to %d times, %d allowed: a fold reads ds.names or t.names, the names that spell `~`", f, n, allowed[f])
		}
	}
	for f, n := range allowed {
		if got[f] != n {
			t.Errorf("%s reads the names the repository answers to %d times, want %d: update this list", f, got[f], n)
		}
	}

	spread := regexp.MustCompile(`\.Names\(\)\.\.\.`)
	for _, root := range []string{"..", "../../cmd"} {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if loc := spread.FindIndex(src); loc != nil {
				t.Errorf("%s spreads bare names into a call (%s): a fold reads the spellings", path, src[max(0, loc[0]-40):loc[1]])
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
