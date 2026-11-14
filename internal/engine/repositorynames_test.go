package engine

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every door that turns a name into a repository goes through the one lookup
// (lookupRepositoryByName, decision record 0287), so a door added later
// cannot forget the former names. Two shapes would: a query that reads the
// repositories, or the former names, by name anywhere but repositorynames.go,
// and a door outside the engine that compares a name with a repository's
// current name alone, where a repository answers to every name in Names().
func TestEveryNameLookupGoesThroughTheOneFunction(t *testing.T) {
	byName := regexp.MustCompile(`(?is)\b(repositories|repository_former_names)\b.*\bWHERE\b.*\b(authority|name)\s*=`)
	rawString := regexp.MustCompile("(?s)`[^`]*`")
	// A repository's own name, compared with anything but "", in every door;
	// and in the HTTP layer, where every name arrives from a request, any
	// `.Authority` compared with a variable.
	currentOnly := map[string][]*regexp.Regexp{}
	own := []*regexp.Regexp{
		regexp.MustCompile(`Repository\(\)\.Authority\s*[!=]=\s*[^"\s]`),
		regexp.MustCompile(`[!=]=\s*[\w.]*Repository\(\)\.Authority\b`),
	}
	for _, door := range []string{"internal/catalog", "internal/appsetup", "internal/mcp", "cmd/substratectl", "cmd/substrated"} {
		currentOnly[door] = own
	}
	currentOnly["internal/api"] = append(own,
		regexp.MustCompile(`\b\w+\.Authority\s*[!=]=\s*[a-zA-Z_]`),
		regexp.MustCompile(`[a-zA-Z_)]\s*[!=]=\s*\w+(\.\w+)*\.Authority\b`))
	root := filepath.Join("..", "..")
	checked := 0
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			checked++
			if rel != "internal/engine/repositorynames.go" {
				for _, q := range rawString.FindAllString(string(src), -1) {
					if byName.MatchString(q) {
						t.Errorf("%s reads a repository by name in its own query, past the former names: use the one lookup (lookupRepositoryByName, RepositoryIDByName)\n%s", rel, q)
					}
				}
			}
			for door, patterns := range currentOnly {
				if !strings.HasPrefix(rel, door+"/") {
					continue
				}
				for i, line := range strings.Split(string(src), "\n") {
					for _, re := range patterns {
						if re.MatchString(line) {
							t.Errorf("%s:%d compares a name with a repository's current name alone, where it answers to Names(): %s", rel, i+1, strings.TrimSpace(line))
						}
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if checked < 100 {
		t.Fatalf("read %d files; the walk did not find the tree", checked)
	}
}
