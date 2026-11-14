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
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// classKeepers is every non-test Go file that still names a class, with how
// many times and what for, so a new use is a new number a review reads, in a
// listed file as much as in a new one.
var classKeepers = map[string]struct {
	uses int
	why  string
}{
	"internal/substrate/tier.go":                                        {6, "the classes themselves, which the owner's gates read (step 19)"},
	"internal/vocabulary/types.go":                                      {2, "a function's and an agent's hand stands at the bundle tier, which the owner's gates read (step 19)"},
	"internal/engine/rows.go":                                           {5, "an actor's tier, which the owner's gates read (step 19)"},
	"internal/engine/rebuild.go":                                        {1, "the replay's writing context (step 19)"},
	"internal/engine/compact.go":                                        {2, "the capture's and the rewrite's writing context, as the replay's (step 19)"},
	"internal/engine/repodir.go":                                        {1, "the import's writing context (step 19)"},
	"internal/engine/sync.go":                                           {1, "a sync's writes stand at the bundle tier, which the owner's gates read (step 19)"},
	"internal/engine/marks.go":                                          {1, "setManager, the landed repository migration 0006's manager words, kept as it landed"},
	"internal/engine/repomigration_0005_remove_reply_relay_contacts.go": {2, "a landed migration's words, kept as it landed"},
	"internal/engine/repomigration_0006_key_the_repository_record_by_its_id.go": {1, "a landed migration's words, kept as it landed"},
	"internal/engine/oauth.go":        {1, "only the owner starts an OAuth flow (step 19)"},
	"internal/engine/writegate.go":    {1, "only the owner passes the owner's gates (step 19)"},
	"internal/engine/write.go":        {6, "only the owner adjusts a change request, a bundle's writes skip the version check, and installed code never writes a policy (step 19)"},
	"internal/engine/approvals.go":    {1, "installed code answers no approval (step 19)"},
	"internal/engine/interactions.go": {1, "installed code answers no interaction (step 19)"},
}

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
	tierClass     = regexp.MustCompile(`\bTier(Owner|Bundle|Machine)\b`)
)

func TestNoWriterWordFromBeforeActorTypesIsLeft(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("the repository root is not two levels up: %v", err)
	}
	classed := map[string]int{}
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
		if strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go") {
			if n := len(tierClass.FindAll(raw, -1)); n > 0 {
				classed[rel] = n
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range slices.Sorted(maps.Keys(classed)) {
		keep, ok := classKeepers[rel]
		switch {
		case !ok:
			t.Errorf("%s names a writer class (TierOwner, TierBundle, TierMachine); a writer is a type of actor now, and only the files listed in classKeepers keep a class, for an owner-only check (step 19) or a landed migration's words", rel)
		case classed[rel] != keep.uses:
			t.Errorf("%s names a class %d times and classKeepers says %d: a new use is an owner-only check's (step 19) to say, and a use gone lowers the number", rel, classed[rel], keep.uses)
		}
	}
	for rel := range classKeepers {
		if _, ok := classed[rel]; !ok {
			t.Errorf("classKeepers lists %s, which names no class any more: take it off the list", rel)
		}
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
