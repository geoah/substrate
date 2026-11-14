package mcp

import (
	"net/http"
	"strings"
	"testing"
)

const compactedBody = `{"error":{"code":"compacted","message":"the cursor was minted by another version of the server's paging; list again","head":90,"generation":"g2"}}`

// A page the substrate can no longer resume is the tool's error, and its
// words tell the agent what to do: start again from the first page and drop
// what it read with the old cursor, once, and stop and tell the person if
// any page of the walk it started over is refused too, a later page as much
// as the first, so a walk refused on its second page every time ends. A
// query and a history read alike.
func TestACompactedPageTellsTheAgentToStartAgain(t *testing.T) {
	for _, tc := range []struct{ tool, args, first string }{
		{"query", `{"kind":"ada.example.com/tasks/task","after":"c1"}`, "query without after"},
		{"history", `{"before":40,"generation":"g1"}`, "history without before and generation"},
	} {
		api := &fakeAPI{answers: []Response{{Status: http.StatusGone, Body: []byte(compactedBody)}}}
		text, isErr := call(t, api, tc.tool, tc.args)
		if !isErr {
			t.Fatalf("%s told compacted = %q, want the tool's error", tc.tool, text)
		}
		for _, want := range []string{
			"410", "compacted", tc.first, "drop what you read", "Start a walk over once",
			"any page of the walk you started over", "the first or a later one", "stop and tell the person",
		} {
			if !strings.Contains(text, want) {
				t.Errorf("%s told compacted says %q, want %q in it", tc.tool, text, want)
			}
		}
	}
}

// Any other refusal keeps the substrate's words alone.
func TestAnotherRefusalOfAPageIsNotCompacted(t *testing.T) {
	api := &fakeAPI{answers: []Response{{Status: http.StatusUnprocessableEntity, Body: []byte(`{"error":{"code":"validation","message":"the cursor was minted for another filter"}}`)}}}
	text, isErr := call(t, api, "query", `{"kind":"ada.example.com/tasks/task","after":"c1"}`)
	if !isErr || !strings.Contains(text, "422") || strings.Contains(text, "start again") {
		t.Fatalf("a 422 on a page = %q", text)
	}
}

// The kinds list walks every kind declaration itself. A continuation the
// substrate can no longer resume starts the walk again from the first page,
// once, with what it read dropped; a second refusal is the tool's error.
func TestKindsStartAgainWhenACursorIsCompacted(t *testing.T) {
	first := `{"records":[{"id":"ada.example.com/notes/note","properties":{}}],"cursor":"c1"}`
	last := `{"records":[{"id":"ada.example.com/tasks/task","properties":{}}]}`
	api := &fakeAPI{answers: []Response{
		{Status: 200, Body: []byte(first)},
		{Status: http.StatusGone, Body: []byte(compactedBody)},
		{Status: 200, Body: []byte(first)},
		{Status: 200, Body: []byte(last)},
	}}
	text, isErr := call(t, api, "kinds", `{}`)
	if isErr {
		t.Fatal(text)
	}
	if !strings.Contains(text, `"count":2`) || strings.Count(text, "ada.example.com/notes/note") != 1 {
		t.Fatalf("kinds after a restart = %s, want the two kinds once each", text)
	}
	if len(api.got) != 4 || api.got[2].Query.Get("after") != "" {
		t.Fatalf("the walk sent %d requests (the third with after %q), want 4, the third from the first page", len(api.got), api.got[2].Query.Get("after"))
	}

	api = &fakeAPI{answers: []Response{
		{Status: 200, Body: []byte(first)},
		{Status: http.StatusGone, Body: []byte(compactedBody)},
		{Status: 200, Body: []byte(first)},
		{Status: http.StatusGone, Body: []byte(compactedBody)},
	}}
	if text, isErr := call(t, api, "kinds", `{}`); !isErr || !strings.Contains(text, "compacted") {
		t.Fatalf("kinds refused twice = %q, want the tool's error", text)
	}
	if len(api.got) != 4 {
		t.Fatalf("the walk sent %d requests, want 4 (one restart)", len(api.got))
	}
}
