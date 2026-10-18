package engine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// A result over the cap becomes a valid JSON envelope no larger than the cap,
// its head cut on a rune boundary; one under it passes untouched.
func TestCapToolResult(t *testing.T) {
	t.Parallel()
	if got := capToolResult(`{"ok":true}`, 1024); got != `{"ok":true}` {
		t.Fatalf("an under-cap result changed: %s", got)
	}
	// Quotes and multi-byte runes: escaping grows the head, and a cut may
	// land inside a rune.
	big := `{"rows":[` + strings.Repeat(`"résumé ✈ \"quoted\" <tag>",`, 5000) + `""]}`
	for _, limit := range []int{1024, 4096, 49152} {
		got := capToolResult(big, limit)
		if len(got) > limit {
			t.Fatalf("limit %d: envelope is %d bytes", limit, len(got))
		}
		var env struct {
			Truncated bool   `json:"truncated"`
			Bytes     int    `json:"bytes"`
			Limit     int    `json:"limit"`
			Head      string `json:"head"`
			Note      string `json:"note"`
		}
		if err := json.Unmarshal([]byte(got), &env); err != nil {
			t.Fatalf("limit %d: not JSON: %v", limit, err)
		}
		if !env.Truncated || env.Bytes != len(big) || env.Limit != limit || !strings.HasPrefix(big, env.Head) ||
			!utf8.ValidString(env.Head) || len(env.Head) < limit/3 {
			t.Fatalf("limit %d: envelope = truncated %v bytes %d limit %d head %d bytes", limit, env.Truncated, env.Bytes, env.Limit, len(env.Head))
		}
		if !strings.Contains(env.Note, "tool-result cap; ask for fewer rows, a filter or a smaller window") {
			t.Fatalf("limit %d: note = %q", limit, env.Note)
		}
	}
	if n := capToolResult(big, 49152); !strings.Contains(n, "48 KiB tool-result cap") {
		t.Fatalf("the default cap's note: %.200s", n[len(n)-200:])
	}
}

func TestUTF8Prefix(t *testing.T) {
	t.Parallel()
	s := "a✈b" // ✈ is three bytes
	for n, want := range map[int]string{0: "", 1: "a", 2: "a", 3: "a", 4: "a✈", 5: "a✈b", 9: "a✈b"} {
		if got := utf8Prefix(s, n); got != want {
			t.Errorf("utf8Prefix(%q, %d) = %q, want %q", s, n, got, want)
		}
	}
}

// Only a query of two or more positive words without OR has a relaxed form
// that matches more.
func TestSearchPlanRelaxes(t *testing.T) {
	t.Parallel()
	for q, want := range map[string]bool{
		"flight":                      false,
		"flight booking":              true,
		"flight -booking":             false,
		"flight OR booking":           false,
		`"flight booking"`:            false,
		"flight booking confirmation": true,
	} {
		if got := parseSearch(q).relaxes(); got != want {
			t.Errorf("relaxes(%q) = %v, want %v", q, got, want)
		}
	}
}

func TestLexicalCapNote(t *testing.T) {
	t.Parallel()
	if n := (lexicalCap{}).note(5 * time.Second); n != "" {
		t.Fatalf("an uncapped search has a note: %q", n)
	}
	got := lexicalCap{budget: true, readCapped: true, readMax: 2000}.note(5 * time.Second)
	want := "search stopped at the 5s budget, so the hits are only records matching every word. " +
		"more than 2000 records match, so only the newest 2000 matches were ranked. " +
		"Use one or two distinctive words (a name, a code, a number), or filter.kinds, to narrow it."
	if got != want {
		t.Fatalf("note = %q\nwant %q", got, want)
	}
}
