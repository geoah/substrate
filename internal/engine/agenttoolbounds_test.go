package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/geoah/substrate/internal/vocabulary"
)

// An explicit toolTimeoutSeconds bounds every call; without one a `query`
// takes the default and a function its declared timeout plus the start
// grace, never less than the default; a deadline closer than either wins.
func TestToolBound(t *testing.T) {
	t.Parallel()
	now := time.Now()
	declares := func(d time.Duration) *vocabulary.Function { return &vocabulary.Function{Timeout: d} }
	for _, c := range []struct {
		name     string
		explicit int
		query    time.Duration
		fn       *vocabulary.Function
		deadline time.Time
		want     time.Duration
	}{
		{"query, no budget", 0, 0, nil, time.Time{}, 30 * time.Second},
		{"query, lowered default", 0, time.Second, nil, time.Time{}, time.Second},
		{"query, explicit budget", 12, 0, nil, time.Time{}, 12 * time.Second},
		{"function declaring 50s, no budget", 0, 0, declares(50 * time.Second), time.Time{}, 55 * time.Second},
		{"function declaring nothing, no budget", 0, 0, declares(0), time.Time{}, 30 * time.Second},
		{"function declaring 5s, no budget", 0, 0, declares(5 * time.Second), time.Time{}, 30 * time.Second},
		{"function declaring 25s, no budget", 0, 0, declares(25 * time.Second), time.Time{}, 30 * time.Second},
		{"function declaring 26s, no budget", 0, 0, declares(26 * time.Second), time.Time{}, 31 * time.Second},
		{"function, lowered query default", 0, time.Second, declares(10 * time.Second), time.Time{}, 15 * time.Second},
		{"function declaring nothing, lowered query default", 0, time.Second, declares(0), time.Time{}, 10 * time.Second},
		{"function, explicit budget below its timeout", 20, 0, declares(50 * time.Second), time.Time{}, 20 * time.Second},
		{"function, explicit budget above its timeout", 90, 0, declares(50 * time.Second), time.Time{}, 90 * time.Second},
		{"function, run deadline sooner", 0, 0, declares(50 * time.Second), now.Add(7 * time.Second), 7 * time.Second},
		{"query, run deadline later", 0, 0, nil, now.Add(time.Minute), 30 * time.Second},
		{"explicit, run deadline sooner", 40, 0, nil, now.Add(2 * time.Second), 2 * time.Second},
	} {
		if got := toolBound(c.explicit, c.query, c.fn, c.deadline, now); got != c.want {
			t.Errorf("%s: toolBound = %s, want %s", c.name, got, c.want)
		}
	}
}

// The timeout error names the bound in force, rounded up to whole seconds.
func TestToolTimedOutNamesTheBound(t *testing.T) {
	t.Parallel()
	call, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-call.Done()
	got, ok := toolTimedOut(context.Background(), call, 55*time.Second)
	if !ok || got != toolError("tool call timed out after 55s — narrow the call (fewer words, a filter, a smaller window) or answer from what you have") {
		t.Fatalf("toolTimedOut = %q, %v", got, ok)
	}
	if got, _ := toolTimedOut(context.Background(), call, 1500*time.Millisecond); !strings.Contains(got, "after 2s") {
		t.Fatalf("a fractional bound reads %q", got)
	}
	loop, stop := context.WithCancel(context.Background())
	stop()
	if _, ok := toolTimedOut(loop, call, time.Second); ok {
		t.Fatal("a call the loop ended answered the tool's timeout error")
	}
}

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
	want := "Search stopped looking for records matching only some of the words at four fifths of its 5s budget, so the lexical hits are only records matching every word. " +
		"More than 2000 records match, so only the newest 2000 matches were ranked. " +
		"Use one or two distinctive words (a name, a code, a number), or filter.kinds, to narrow it."
	if got != want {
		t.Fatalf("note = %q\nwant %q", got, want)
	}
}
