package engine

// The replay of a continued thread's tool exchanges (decision 0201), without
// a database: the message sequence replayView emits for stored rows, held as
// a golden list, and what compaction makes of the replayed exchanges.

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/llm"
)

// replayLine is one replayed message as a golden line: the role, then what
// the wire would carry for it.
func replayLine(m llm.Message) string {
	switch {
	case m.Role == llm.RoleTool:
		return fmt.Sprintf("tool %s %s: %s", m.ToolCallID, m.ToolName, m.Content)
	case len(m.ToolCalls) > 0:
		calls := make([]string, 0, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			calls = append(calls, tc.ID+" "+tc.Name+tc.Arguments)
		}
		return fmt.Sprintf("assistant %q calls %s", m.Content, strings.Join(calls, ", "))
	default:
		return m.Role + ": " + m.Content
	}
}

func namedCallsRow(id, content string, calls ...[3]string) threadRow {
	stored := make([]any, 0, len(calls))
	for _, c := range calls {
		stored = append(stored, map[string]any{"id": c[0], "name": c[1], "arguments": c[2]})
	}
	props := map[string]any{"role": "assistant", "toolCalls": stored}
	if content != "" {
		props["content"] = content
	}
	return threadRow{id: id, props: props}
}

func namedToolRow(id, callID, name, content string) threadRow {
	return threadRow{id: id, props: map[string]any{
		"role": "tool", "toolCallId": callID, "name": name, "content": content, "ok": true,
	}}
}

func TestReplayViewReplaysToolExchanges(t *testing.T) {
	interrupted := toolError("this call was interrupted before it returned a result; it may or may not have taken effect")
	env := `{"event":"interactionAnswered","interaction":"x"}`
	for name, tc := range map[string]struct {
		rows   []threadRow
		tools  bool
		want   []string
		origin []int
	}{
		"each call answered after it, in call order": {
			rows: []threadRow{
				userRow("u0", "what is due?"),
				namedCallsRow("c0", "looking", [3]string{"call_a", "query", `{"q":"due"}`}, [3]string{"call_b", "get", `{"id":"t1"}`}),
				namedToolRow("t0", "call_a", "query", `{"rows":[]}`),
				namedToolRow("t1", "call_b", "get", `{"error":"not found"}`),
				assistantRow("a0", "nothing is due"),
				userRow("u1", "and tomorrow?"),
			},
			tools: true,
			want: []string{
				"user: what is due?",
				`assistant "looking" calls call_a query{"q":"due"}, call_b get{"id":"t1"}`,
				`tool call_a query: {"rows":[]}`,
				`tool call_b get: {"error":"not found"}`,
				"assistant: nothing is due",
				"user: and tomorrow?",
			},
			origin: []int{0, 1, 2, 3, 4, 5},
		},
		"a row landing between a call and its answer replays after the answers": {
			rows: []threadRow{
				userRow("u0", "q"),
				namedCallsRow("c0", "", [3]string{"call_a", "ask", "{}"}, [3]string{"call_b", "get", "{}"}),
				namedToolRow("t0", "call_a", "ask", "asked"),
				{id: "r0", props: map[string]any{"role": msgRoleSystem, "content": env}},
				namedToolRow("t1", "call_b", "get", "got"),
				assistantRow("a0", "done"),
			},
			tools: true,
			want: []string{
				"user: q",
				`assistant "" calls call_a ask{}, call_b get{}`,
				"tool call_a ask: asked",
				"tool call_b get: got",
				"user: " + env,
				"assistant: done",
			},
			origin: []int{0, 1, 2, 4, 3, 5},
		},
		"a call whose answer never landed is answered as interrupted": {
			rows: []threadRow{
				userRow("u0", "q"),
				namedCallsRow("c0", "", [3]string{"call_a", "get", "{}"}, [3]string{"call_b", "put", "{}"}),
				namedToolRow("t0", "call_a", "get", "got"),
				userRow("u1", "still there?"),
			},
			tools: true,
			want: []string{
				"user: q",
				`assistant "" calls call_a get{}, call_b put{}`,
				"tool call_a get: got",
				"tool call_b put: " + interrupted,
				"user: still there?",
			},
			origin: []int{0, 1, 2, 1, 3},
		},
		"an answer whose call is not replayed is dropped": {
			rows: []threadRow{
				userRow("u0", "q"),
				namedToolRow("t0", "call_x", "get", "stray"),
				assistantRow("a0", "r"),
			},
			tools:  true,
			want:   []string{"user: q", "assistant: r"},
			origin: []int{0, 2},
		},
		"a summary over a tool exchange replaces it whole": {
			rows: []threadRow{
				userRow("u0", "q0"),
				namedCallsRow("c0", "", [3]string{"call_a", "get", "{}"}),
				namedToolRow("t0", "call_a", "get", "got"),
				assistantRow("a0", "r0"),
				userRow("u1", "q1"),
				namedCallsRow("c1", "", [3]string{"call_b", "get", "{}"}),
				namedToolRow("t1", "call_b", "get", "got again"),
				assistantRow("a1", "r1"),
				summaryRow("s", "S", "u0", "a0"),
				userRow("u2", "q2"),
			},
			tools: true,
			want: []string{
				"user: " + summaryMessage("S").Content,
				"user: q1",
				`assistant "" calls call_b get{}`,
				"tool call_b get: got again",
				"assistant: r1",
				"user: q2",
			},
			origin: []int{8, 4, 5, 6, 7, 9},
		},
		"none replays the prose alone": {
			rows: []threadRow{
				userRow("u0", "q"),
				namedCallsRow("c0", "looking", [3]string{"call_a", "get", "{}"}),
				namedToolRow("t0", "call_a", "get", "got"),
				assistantRow("a0", "r"),
			},
			tools:  false,
			want:   []string{"user: q", "assistant: r"},
			origin: []int{0, 3},
		},
	} {
		t.Run(name, func(t *testing.T) {
			view, origin, _ := replayView(tc.rows, tc.tools, nil)
			got := make([]string, 0, len(view))
			for _, m := range view {
				got = append(got, replayLine(m))
			}
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Fatalf("view:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
			if !reflect.DeepEqual(origin, tc.origin) {
				t.Fatalf("origin = %v, want %v", origin, tc.origin)
			}
		})
	}
}

// The same rows replay as the same messages: a continued thread's prefix is
// what a prompt cache keys on, so a replay that varied by call would miss it
// on every continuation. The rows are re-read through JSON, as loadThreadRows
// reads them, so map order and number types cannot leak in.
func TestReplayViewIsAFunctionOfTheRows(t *testing.T) {
	rows := []threadRow{
		userRow("u0", "q"),
		namedCallsRow("c0", "", [3]string{"call_a", "get", `{"b":2,"a":1}`}, [3]string{"call_b", "put", "{}"}),
		namedToolRow("t1", "call_b", "put", "put"),
		namedToolRow("t0", "call_a", "get", "got"),
		assistantRow("a0", "r"),
	}
	first, _, _ := replayView(rows, true, nil)
	raw, err := json.Marshal(func() []map[string]any {
		out := make([]map[string]any, 0, len(rows))
		for _, r := range rows {
			out = append(out, r.props)
		}
		return out
	}())
	if err != nil {
		t.Fatal(err)
	}
	var props []map[string]any
	if err := json.Unmarshal(raw, &props); err != nil {
		t.Fatal(err)
	}
	reread := make([]threadRow, len(rows))
	for i := range rows {
		reread[i] = threadRow{id: rows[i].id, props: props[i]}
	}
	for range 3 {
		again, _, _ := replayView(reread, true, nil)
		if !reflect.DeepEqual(first, again) {
			t.Fatalf("replay changed:\n%+v\n%+v", first, again)
		}
	}
	// The stored argument text travels as written, never re-encoded.
	if first[1].ToolCalls[0].Arguments != `{"b":2,"a":1}` {
		t.Fatalf("arguments = %q", first[1].ToolCalls[0].Arguments)
	}
	// Answers follow the calls' order, not the rows'.
	if first[2].ToolCallID != "call_a" || first[3].ToolCallID != "call_b" {
		t.Fatalf("answers out of call order: %+v", first[2:4])
	}
}

// A replayed exchange is part of what compaction weighs and folds: the call's
// arguments count toward the estimate, the cut still lands on a user row past
// every answer, and the transcript labels calls and results apart from the
// user's words.
func TestCompactionWeighsAndFoldsReplayedToolExchanges(t *testing.T) {
	call := llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "call_a", Name: "get", Arguments: body}}}
	if got := messageTokens(call); got < bodyTokens {
		t.Fatalf("a call carrying %d characters of arguments estimates %d tokens", len(body), got)
	}

	rows := []threadRow{
		userRow("u0", body),
		namedCallsRow("c0", "", [3]string{"call_a", "get", "{}"}),
		namedToolRow("t0", "call_a", "get", body),
		assistantRow("a0", body),
		userRow("u1", body),
		namedCallsRow("c1", "", [3]string{"call_b", "get", "{}"}),
		namedToolRow("t1", "call_b", "get", body),
		assistantRow("a1", body),
		userRow("u2", body), assistantRow("a2", body),
	}
	p, ok := plan(t, rows, 2*bodyTokens)
	if !ok {
		t.Fatal("no plan")
	}
	if rows[p.fromIdx].id != "u0" || rows[p.throughIdx].id != "a1" {
		t.Fatalf("range %s..%s, want u0..a1", rows[p.fromIdx].id, rows[p.throughIdx].id)
	}
	transcript := compactionTranscript(p.covered, p.speakers)
	for _, want := range []string{"[Assistant called get]: {}", "[Tool result get]: " + body} {
		if !strings.Contains(transcript, want) {
			t.Fatalf("transcript lacks %q:\n%s", want, transcript)
		}
	}
	if strings.Contains(transcript, "[Assistant]: \n") || strings.HasSuffix(strings.Split(transcript, "\n")[1], "[Assistant]: ") {
		t.Fatalf("a call-only turn rendered an empty prose line:\n%s", transcript)
	}
}
