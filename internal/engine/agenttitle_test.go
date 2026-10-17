package engine

import (
	"runtime"
	"strings"
	"testing"
	"time"
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
		{"see [array[index]](https://example.com) here", "see array[index] here"},
		{"an [unmatched bracket and ](paren", "an [unmatched bracket and ](paren"},
		{"a [label] with no target", "a [label] with no target"},
		{"mail <https://example.com/x> or <ada@example.com>", "mail https://example.com/x or ada@example.com"},
		{"a < b and c > d", "a < b and c > d"},
		{"**bold** and __strong__ and *em* and _em_", "bold and strong and em and em"},
		{"***both*** and **nested _em_ here**", "both and nested em here"},
		{"run `go test` on ~~old~~ new", "run go test on old new"},
		{"call `__init__` and ``a ` tick``", "call __init__ and a ` tick"},
		{"an `unclosed tick", "an `unclosed tick"},
		{"keep snake_case_name and 2*3 as text", "keep snake_case_name and 2*3 as text"},
		{"compute 2 * 3 * 4", "compute 2 * 3 * 4"},
		{"clear /tmp/_cache/file now", "clear /tmp/_cache/file now"},
		{"open https://example.com/a_b_c*d now", "open https://example.com/a_b_c*d now"},
		{`escaped \*stars\* stay`, "escaped *stars* stay"},
		{"## A heading", "A heading"},
		{"> - quoted item", "quoted item"},
		{"> > nested quote", "nested quote"},
		{"- [x] done item", "done item"},
		{"12. numbered", "numbered"},
		{"* starred item", "starred item"},
		{"---", ""},
		{"* * *", ""},
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
	// The scan bound never splits a rune either.
	long := strings.Repeat("é", subjectScanBytes)
	if got := boundedLine(long); !utf8.ValidString(got) || len(got) > subjectScanBytes {
		t.Fatalf("bounded line: %d bytes, valid %v", len(got), utf8.ValidString(got))
	}
}

// A request may carry a megabyte on one line; the reduction reads a bounded
// prefix of it, once.
func TestCleanSubjectOfAHugeMarkerRunIsBounded(t *testing.T) {
	for _, in := range []string{
		strings.Repeat(">", 1<<20) + "hello",
		strings.Repeat("> ", 1<<19) + "hello",
		strings.Repeat("[", 1<<20) + "hello",
		strings.Repeat("` ", 1<<19) + "hello",
		strings.Repeat("*a ", 1<<18) + "hello",
	} {
		started := time.Now()
		var got string
		allocs := testing.AllocsPerRun(3, func() { got = cleanSubject(in) })
		if took := time.Since(started); took > 2*time.Second {
			t.Errorf("cleanSubject of a %d byte line took %v", len(in), took)
		}
		// The scan reads subjectScanBytes at most, so what it allocates is a
		// function of that bound, never of the line.
		if allocs > 4*subjectScanBytes {
			t.Errorf("cleanSubject of a %d byte line made %v allocations", len(in), allocs)
		}
		if utf8.RuneCountInString(got) > threadSubjectRunes {
			t.Errorf("subject of %d runes", utf8.RuneCountInString(got))
		}
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		before := stats.TotalAlloc
		cleanSubject(in)
		runtime.ReadMemStats(&stats)
		if spent := stats.TotalAlloc - before; spent > 256*subjectScanBytes {
			t.Errorf("cleanSubject of a %d byte line allocated %d bytes", len(in), spent)
		}
	}
}

func TestCleanTitleReadsMarkdownAsPlainText(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"**Title:** Trip to *Berlin*": "Trip to Berlin",
		"# `Budget` review.":          "Budget review",
		"[Plans](x) for **Q4**":       "Plans for Q4",
		"[1. Introduction](x)":        "1. Introduction",
		"[---](x)":                    "---",
		"---":                         "---",
		"\n\n> ## Weekly plan\nmore":  "Weekly plan",
	} {
		if got := cleanTitle(in); got != want {
			t.Errorf("cleanTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
