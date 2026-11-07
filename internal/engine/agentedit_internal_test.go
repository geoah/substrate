package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// An edit names exactly old and new, both strings: a misspelled, missing or
// non-string new is refused rather than read as "" and deleting the text.
// Deleting is an explicit "new": "".
func TestAnEditNamesOldAndNewExactly(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   any
		says string
	}{
		{"a misspelled new", []any{map[string]any{"old": "important text", "neww": "corrected"}}, `unknown key "neww"`},
		{"a missing new", []any{map[string]any{"old": "important text"}}, "names old and new"},
		{"a null new", []any{map[string]any{"old": "important text", "new": nil}}, "names old and new"},
		{"a number for new", []any{map[string]any{"old": "a", "new": 3}}, "names old and new"},
		{"an empty old", []any{map[string]any{"old": "", "new": "x"}}, "old is empty"},
		{"no edits", []any{}, "edits is required"},
	} {
		if _, err := editsArg(tc.in); err == nil || !strings.Contains(err.Error(), tc.says) {
			t.Errorf("%s: %v, want an error that says %q", tc.name, err, tc.says)
		}
	}
	got, err := editsArg(`[{"old":"stale line","new":""},{"old":"typo","new":"fixed"}]`)
	if err != nil || len(got) != 2 || got[0] != (textEdit{Old: "stale line"}) || got[1] != (textEdit{Old: "typo", New: "fixed"}) {
		t.Fatalf("an explicit deletion and a replacement, as a JSON string: %+v, %v", got, err)
	}
}

// A view answers at most editViewLines lines whatever end asks for, and says
// where it stopped.
func TestAViewStaysWithinItsLineCap(t *testing.T) {
	text := strings.TrimSuffix(strings.Repeat("line\n", 1000), "\n")
	l := &agentLoop{}
	for _, tc := range []struct {
		start, end float64
		wantEnd    int
	}{
		{1, 900, editViewLines},
		{950, 2000, 1000},
		{10, 20, 20},
	} {
		out, ok := l.editView(&substrate.Record{Version: 1}, text, map[string]any{"start": tc.start, "end": tc.end})
		if !ok {
			t.Fatalf("view %v..%v: %s", tc.start, tc.end, out)
		}
		var v struct {
			End  int    `json:"end"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(out), &v); err != nil {
			t.Fatal(err)
		}
		if lines := strings.Count(v.Text, "\n"); v.End != tc.wantEnd || lines != tc.wantEnd-int(tc.start)+1 {
			t.Fatalf("view %v..%v: end %d with %d lines, want end %d", tc.start, tc.end, v.End, lines, tc.wantEnd)
		}
	}
}
