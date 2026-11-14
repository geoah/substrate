package engine

import (
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/llm"
)

// queueRow is one stored message row of a thread, for the replay tests.
func queueRow(id, role string, turn int, more map[string]any) threadRow {
	props := map[string]any{"role": role, "turn": float64(turn), "content": id}
	for k, v := range more {
		props[k] = v
	}
	return threadRow{id: id, props: props}
}

func callsOf(ids ...string) map[string]any {
	calls := make([]any, 0, len(ids))
	for _, id := range ids {
		calls = append(calls, map[string]any{"id": id, "name": "query", "arguments": "{}"})
	}
	return map[string]any{"toolCalls": calls}
}

// replayed is what a replay of rows hands the model, as `role:content`, a
// tool result as `tool:<call id>`.
func replayed(rows []threadRow) string {
	replayOrder(rows)
	msgs, _, _ := replayView(rows, true, nil)
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		switch {
		case m.Role == llm.RoleTool:
			out = append(out, "tool:"+m.ToolCallID)
		case len(m.ToolCalls) > 0:
			out = append(out, "assistant:calls")
		default:
			out = append(out, m.Role+":"+m.Content)
		}
	}
	return strings.Join(out, " ")
}

// A message sent while a turn runs is created at once, so creation order
// puts it before the reply that was streaming and the results of the calls
// under way. Replay reads it where the model did: behind every row created
// later under a smaller ordinal (decision 0318). Each case's rows are in
// creation order.
func TestReplayReadsAQueuedMessageWhereTheModelDid(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		rows []threadRow
		want string
	}{
		{
			// Sent while the call that asked for a tool was in flight: its
			// ordinal is past that reply's and below the result's.
			name: "sent during the call that made a tool call",
			rows: []threadRow{
				queueRow("m0", "user", 0, nil),
				queueRow("q1", "user", 2, nil),
				queueRow("a1", "assistant", 1, callsOf("c0")),
				queueRow("t1", "tool", 3, map[string]any{"toolCallId": "c0"}),
				queueRow("a2", "assistant", 4, nil),
			},
			want: "user:m0 assistant:calls tool:c0 user:q1 assistant:a2",
		},
		{
			// Sent while the tools ran: the results' ordinals were drawn
			// before it.
			name: "sent while two calls ran",
			rows: []threadRow{
				queueRow("m0", "user", 0, nil),
				queueRow("a1", "assistant", 1, callsOf("c0", "c1")),
				queueRow("q1", "user", 4, nil),
				queueRow("t1", "tool", 2, map[string]any{"toolCallId": "c0"}),
				queueRow("t2", "tool", 3, map[string]any{"toolCallId": "c1"}),
				queueRow("a2", "assistant", 5, nil),
			},
			want: "user:m0 assistant:calls tool:c0 tool:c1 user:q1 assistant:a2",
		},
		{
			// Sent while the final answer streamed: it starts the next turn,
			// and the conversation the next turn sends ends on it.
			name: "sent during the final answer",
			rows: []threadRow{
				queueRow("m0", "user", 0, nil),
				queueRow("q1", "user", 2, nil),
				queueRow("a1", "assistant", 1, nil),
			},
			want: "user:m0 assistant:a1 user:q1",
		},
		{
			name: "two sent during one call keep their order",
			rows: []threadRow{
				queueRow("m0", "user", 0, nil),
				queueRow("q1", "user", 2, nil),
				queueRow("q2", "user", 3, nil),
				queueRow("a1", "assistant", 1, nil),
				queueRow("a2", "assistant", 4, nil),
			},
			want: "user:m0 assistant:a1 user:q1 user:q2 assistant:a2",
		},
		{
			// A resolution's system row lands with an ordinal past the reply
			// that streams, as before: only a user row moves.
			name: "a system row written mid-reply stays where it was written",
			rows: []threadRow{
				queueRow("m0", "user", 0, nil),
				queueRow("s1", msgRoleSystem, 2, nil),
				queueRow("a1", "assistant", 1, nil),
			},
			want: "user:m0 user:s1 assistant:a1",
		},
		{
			name: "a thread nobody sent into mid-turn replays as stored",
			rows: []threadRow{
				queueRow("m0", "user", 0, nil),
				queueRow("a1", "assistant", 1, callsOf("c0")),
				queueRow("t1", "tool", 2, map[string]any{"toolCallId": "c0"}),
				queueRow("a2", "assistant", 3, nil),
				queueRow("m1", "user", 4, nil),
				queueRow("a3", "assistant", 5, nil),
			},
			want: "user:m0 assistant:calls tool:c0 assistant:a2 user:m1 assistant:a3",
		},
		{
			// A resolution lands between the message and the reply that was
			// streaming, with an ordinal past the message's. The message
			// still goes behind the reply: the turn it starts must not be
			// sent a conversation that ends on the assistant.
			name: "a resolution written between the message and the late reply",
			rows: []threadRow{
				queueRow("m0", "user", 0, nil),
				queueRow("q1", "user", 2, nil),
				queueRow("s1", msgRoleSystem, 3, nil),
				queueRow("a1", "assistant", 1, nil),
			},
			want: "user:m0 user:s1 assistant:a1 user:q1",
		},
		{
			// Rows from before ordinals were unique, or with none: a row of
			// an equal ordinal moves nothing, and nothing moves past a row
			// that carries none.
			name: "an equal or missing ordinal stops the move",
			rows: []threadRow{
				queueRow("m0", "user", 1, nil),
				queueRow("a1", "assistant", 1, nil),
				{id: "a2", props: map[string]any{"role": "assistant", "content": "a2"}},
				queueRow("a3", "assistant", 0, nil),
			},
			want: "user:m0 assistant:a1 assistant:a2 assistant:a3",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := replayed(tc.rows); got != tc.want {
				t.Fatalf("replayed\n  %s\nwant\n  %s", got, tc.want)
			}
		})
	}
}
