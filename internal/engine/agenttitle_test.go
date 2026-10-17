package engine

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCleanTitle(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		`"Trip to Berlin".`:             "Trip to Berlin",
		"\n\nTitle: Weekly  plan\nmore": "Weekly plan",
		"“Quoted”":                      "Quoted",
		"   ":                           "",
		strings.Repeat("x", 100):        strings.Repeat("x", threadSubjectRunes),
	} {
		if got := cleanTitle(in); got != want {
			t.Errorf("cleanTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanSubjectReadsMarkdownAsPlainText(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ in, want string }{
		{"About [Q4 budget draft](ref:mneme.rxtx.dev/tasks/task/abc): what is left?", "About Q4 budget draft: what is left?"},
		{"see ![the chart](https://example.com/a_(b).png) now", "see the chart now"},
		{"mail <https://example.com/x> or <ada@example.com>", "mail https://example.com/x or ada@example.com"},
		{"**bold** and __strong__ and *em* and _em_", "bold and strong and em and em"},
		{"run `go test` on ~~old~~ new", "run go test on old new"},
		{"keep snake_case_name and 2*3 as text", "keep snake_case_name and 2*3 as text"},
		{"## A heading", "A heading"},
		{"> quoted - [ ] task", "quoted - [ ] task"},
		{"> - quoted item", "quoted item"},
		{"- [x] done item", "done item"},
		{"12. numbered", "numbered"},
		{"* starred item", "starred item"},
		{"---", ""},
		{"#hashtag stays", "#hashtag stays"},
		{"Ünïcødé **ἀρχή** [日本語](x) 👍", "Ünïcødé ἀρχή 日本語 👍"},
		{"“Quoted”", "Quoted"},
	} {
		if got := cleanSubject(c.in); got != c.want {
			t.Errorf("cleanSubject(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCleanSubjectCutsWholeRunes(t *testing.T) {
	t.Parallel()
	in := "**" + strings.Repeat("日本", 50) + "**"
	got := cleanSubject(in)
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) != threadSubjectRunes {
		t.Fatalf("cut %q: %d runes, valid %v", got, utf8.RuneCountInString(got), utf8.ValidString(got))
	}
	if got != strings.Repeat("日本", threadSubjectRunes/2) {
		t.Fatalf("cut %q", got)
	}
}

func TestCleanTitleReadsMarkdownAsPlainText(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"**Title:** Trip to *Berlin*": "Trip to Berlin",
		"# `Budget` review.":          "Budget review",
		"[Plans](x) for **Q4**":       "Plans for Q4",
	} {
		if got := cleanTitle(in); got != want {
			t.Errorf("cleanTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
