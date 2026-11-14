package vocabulary_test

// The words `writer:` took before actor types (decision 0290) are gone from
// the tree, and so is the class a writer was folded into. A decision record
// and the changelog keep the history in the words of its day; nothing else
// may. The classes (owner, bundle, machine) stay only where they are still
// something other than a writer: the manager mark a property carries, which
// step 17 replaces, and the checks that only the owner may act, which step 19
// replaces. Each file that keeps one is named below with what it keeps it
// for, so a new use is a new line a review reads.

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// oldWordKeepers are the files outside decision records and the changelog
// where an old word has to stand, each with why.
var oldWordKeepers = map[string]string{
	"internal/engine/repomigrate_writers_db_test.go": "migration 10's test plants the stored declarations it rewrites, and a default value that holds the word and must stay",
}

var (
	// A retired word as a `writer:` value, in YAML, Go, TypeScript or prose,
	// alone or in a list.
	retiredWriterValue = regexp.MustCompile(`(?i)\bwriter["'\x60]?\s*[:=]\s*(\[\]any\{|\[)?\s*(["'\x60]?[a-z]+["'\x60]?\s*,\s*)*["'\x60]?(owner|connector|oauth)["'\x60]?\s*([,\]\}\x60#)]|$)`)
	// The names the old words had in code.
	retiredWriterName = regexp.MustCompile(`\b(WriterOwner|WriterConnector|WriterOAuth|writerRoles|writerRoleIs|mayWriteProp|ownerWritable)\b`)
	// A writer spoken of as a role or a class.
	writerAsClass = regexp.MustCompile(`(?i)\bwriter:?\x60? (role|roles|class|classes)\b`)
)

func TestNoWriterWordFromBeforeActorTypesIsLeft(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("the repository root is not two levels up: %v", err)
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch {
			case strings.HasPrefix(d.Name(), ".") && rel != ".":
				return fs.SkipDir
			case d.Name() == "node_modules", d.Name() == "dist", d.Name() == "build", d.Name() == "bin",
				rel == "docs/decisions",
				// The kept directories are what a released server wrote, and
				// they are frozen.
				rel == "internal/testenv/testdata/kept":
				return fs.SkipDir
			}
			return nil
		}
		if _, kept := oldWordKeepers[rel]; kept || rel == "CHANGELOG.md" || rel == "internal/vocabulary/writerwords_test.go" || !textFile(rel) {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for n, line := range strings.Split(string(raw), "\n") {
			for _, re := range []*regexp.Regexp{retiredWriterValue, retiredWriterName, writerAsClass} {
				if m := re.FindString(line); m != "" {
					t.Errorf("%s:%d says %q: a writer names types of actor (decision 0290)", rel, n+1, m)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// textFile reports whether a path is source or prose the words could be
// written in.
func textFile(rel string) bool {
	switch filepath.Ext(rel) {
	case ".go", ".yaml", ".yml", ".md", ".ts", ".tsx", ".swift", ".kt", ".json", ".py", ".sh", ".toml", ".sql":
		return true
	}
	return false
}
