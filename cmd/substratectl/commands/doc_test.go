package commands

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// substratectlDoc is docs/substratectl.md with its whitespace collapsed, so a
// command name a line break splits (`repository\nverify`) still reads as one,
// and its paragraphs (the blocks between blank lines) in the same form.
func substratectlDoc(t *testing.T) (doc string, paragraphs []string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "substratectl.md"))
	if err != nil {
		t.Fatalf("read docs/substratectl.md: %v", err)
	}
	space := regexp.MustCompile(`\s+`)
	for _, p := range regexp.MustCompile(`\n[ \t]*\n`).Split(string(raw), -1) {
		paragraphs = append(paragraphs, space.ReplaceAllString(p, " "))
	}
	return space.ReplaceAllString(string(raw), " "), paragraphs
}

// namesCommand reports whether text names a command: `phrase` followed by
// something that cannot continue a command word, so `bundle list` is not
// found inside `bundle listing`.
func namesCommand(text, phrase string) bool {
	for at := 0; ; {
		i := strings.Index(text[at:], phrase)
		if i < 0 {
			return false
		}
		end := at + i + len(phrase)
		if end == len(text) || !strings.ContainsRune("abcdefghijklmnopqrstuvwxyz0123456789-", rune(text[end])) {
			return true
		}
		at = end
	}
}

// documented reports whether the page names a command path. It does when it
// prints `substratectl <path>`, or the path in a code span (`token list`). A
// subcommand may also be named by its last word alone in a code span
// (`forget`), the way the page lists a parent's verbs, when one paragraph
// names that parent and no other command group: a paragraph about `trigger`
// and `bundle` together cannot say whose `status` it means.
func documented(doc string, paragraphs, groups, path []string) bool {
	full := strings.Join(path, " ")
	if namesCommand(doc, "substratectl "+full) || namesCommand(doc, "`"+full) {
		return true
	}
	if len(path) < 2 {
		return false
	}
	parent, leaf := strings.Join(path[:len(path)-1], " "), path[len(path)-1]
	names := func(p, group string) bool {
		return namesCommand(p, "substratectl "+group) || namesCommand(p, "`"+group)
	}
	for _, p := range paragraphs {
		if !names(p, parent) || !namesCommand(p, "`"+leaf) {
			continue
		}
		alone := true
		for _, g := range groups {
			if g != parent && names(p, g) {
				alone = false
				break
			}
		}
		if alone {
			return true
		}
	}
	return false
}

// commandTree walks the CLI: every command path it reaches, and the paths of
// the ones that have subcommands.
func commandTree() (paths [][]string, groups []string) {
	var walk func(c *cobra.Command, path []string)
	walk = func(c *cobra.Command, path []string) {
		for _, sub := range c.Commands() {
			if sub.Hidden {
				continue
			}
			p := append(append([]string{}, path...), sub.Name())
			paths = append(paths, p)
			if sub.HasSubCommands() {
				groups = append(groups, strings.Join(p, " "))
			}
			walk(sub, p)
		}
	}
	walk(newApp("test").rootCommand(), nil)
	return paths, groups
}

// TestSubstratectlDocNamesEveryCommand walks the CLI and requires every
// command it reaches to be named in docs/substratectl.md, so a command added
// without a line on the page fails here rather than going unfound.
func TestSubstratectlDocNamesEveryCommand(t *testing.T) {
	doc, paragraphs := substratectlDoc(t)
	paths, groups := commandTree()
	// A walk that found nothing would pass having checked nothing.
	if len(paths) < 40 {
		t.Fatalf("the walk found %d command paths; the CLI has more than 40", len(paths))
	}
	for _, p := range paths {
		if documented(doc, paragraphs, groups, p) {
			continue
		}
		if len(p) == 1 {
			t.Errorf("docs/substratectl.md does not name `substratectl %s`", p[0])
			continue
		}
		t.Errorf("docs/substratectl.md does not name `substratectl %s`: print it, or name `%s` in a code span in a paragraph about `%s` alone",
			strings.Join(p, " "), p[len(p)-1], strings.Join(p[:len(p)-1], " "))
	}
}

// The fallback reads a verb in a paragraph as its parent's only when no other
// command group shares the paragraph, so `status` beside both `trigger` and
// `bundle` documents neither.
func TestDocumentedReadsAVerbAsItsParentsAlone(t *testing.T) {
	groups := []string{"bundle", "trigger"}
	for _, tc := range []struct {
		paragraph string
		path      []string
		want      bool
	}{
		{"the `trigger` subcommands: `status` shows each trigger", []string{"trigger", "status"}, true},
		{"the `trigger` subcommands: `status` shows each trigger", []string{"bundle", "status"}, false},
		{"`substratectl bundle list` and the `trigger` verbs: `status`", []string{"bundle", "status"}, false},
		{"`substratectl bundle list` / `status` report", []string{"bundle", "status"}, true},
		{"`substratectl bundle list` / `status` report", []string{"bundle", "run"}, false},
	} {
		if got := documented("", []string{tc.paragraph}, groups, tc.path); got != tc.want {
			t.Errorf("documented(%q, %v) = %v, want %v", tc.paragraph, tc.path, got, tc.want)
		}
	}
}
