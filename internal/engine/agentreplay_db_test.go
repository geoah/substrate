package engine

// A follow-up message replays what the previous answer read (decision 0201):
// the second request of a chat carries the first run's tool call and its
// stored result, and an agent declaring `replayTools: none` sends the prose
// alone.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// chatTwice runs a two-message chat on agent, whose first answer calls the
// annotate tool once, and returns the second request the endpoint received.
func chatTwice(t *testing.T, agent, model string) map[string]any {
	t.Helper()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script(model,
		fakeTurn{content: "noting it", calls: []fakeCall{{"annotate", `{"id":"t-recalled"}`}}},
		fakeTurn{content: "annotated t-recalled"},
		fakeTurn{content: "it was t-recalled"},
	)
	first, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/"+agent, substrate.ChatInput{Message: "annotate something"}, nil)
	if err != nil || first.Status != threadOK {
		t.Fatalf("first message: %+v, %v", first, err)
	}
	next, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/"+agent, substrate.ChatInput{Thread: first.Thread, Message: "which id was it?"}, nil)
	if err != nil || next.Status != threadOK {
		t.Fatalf("follow-up: %+v, %v", next, err)
	}
	reqs := fake.requestsOf(model)
	if len(reqs) != 3 {
		t.Fatalf("model calls: %d, want 3", len(reqs))
	}
	return reqs[2]
}

func TestAFollowUpReplaysTheToolExchangesItsThreadAlreadyMade(t *testing.T) {
	t.Parallel()
	req := chatTwice(t, "recaller", "recall")
	msgs, _ := req["messages"].([]any)
	// system, the first message, the call, its answer, the first reply, the
	// follow-up: the second run's request starts where the first run's last
	// request ended.
	if len(msgs) != 6 {
		raw, _ := json.Marshal(msgs)
		t.Fatalf("follow-up history: %d messages, want 6: %s", len(msgs), raw)
	}
	call, _ := msgs[2].(map[string]any)
	calls, _ := call["tool_calls"].([]any)
	if call["role"] != "assistant" || call["content"] != "noting it" || len(calls) != 1 {
		t.Fatalf("replayed call turn: %v", call)
	}
	tc, _ := calls[0].(map[string]any)
	fn, _ := tc["function"].(map[string]any)
	if tc["id"] != "call_0" || fn["name"] != "annotate" || fn["arguments"] != `{"id":"t-recalled"}` {
		t.Fatalf("replayed call: %v", tc)
	}
	answer, _ := msgs[3].(map[string]any)
	if answer["role"] != "tool" || answer["tool_call_id"] != "call_0" || answer["content"] == "" {
		t.Fatalf("replayed answer: %v", answer)
	}
	if reply, _ := msgs[4].(map[string]any); reply["role"] != "assistant" || reply["content"] != "annotated t-recalled" {
		t.Fatalf("replayed reply: %v", reply)
	}
	if last, _ := msgs[5].(map[string]any); last["role"] != "user" || msgText(last) != "which id was it?" {
		t.Fatalf("follow-up turn: %v", last)
	}
}

// The replayed prefix is the first run's own last request, byte for byte, so
// a provider's prompt cache written by the first run serves the second.
func TestAFollowUpRepeatsThePreviousRequestAsItsPrefix(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("recall",
		fakeTurn{calls: []fakeCall{{"annotate", `{"id":"t-prefix"}`}}},
		fakeTurn{content: "done"},
		fakeTurn{content: "again"},
	)
	first, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/recaller", substrate.ChatInput{Message: "go"}, nil)
	if err != nil {
		t.Fatalf("first message: %v", err)
	}
	if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/recaller", substrate.ChatInput{Thread: first.Thread, Message: "more"}, nil); err != nil {
		t.Fatalf("follow-up: %v", err)
	}
	reqs := fake.requestsOf("recall")
	earlier, _ := reqs[1]["messages"].([]any)
	later, _ := reqs[2]["messages"].([]any)
	if len(later) != len(earlier)+2 {
		t.Fatalf("follow-up carries %d messages over the earlier request's %d, want the reply and the new turn", len(later), len(earlier))
	}
	a, _ := json.Marshal(earlier)
	b, _ := json.Marshal(later[:len(earlier)])
	if string(a) != string(b) {
		t.Fatalf("the replayed prefix differs from the request that produced it:\n%s\n%s", a, b)
	}
}

func TestAFollowUpOnAnAgentThatReplaysNoToolsSendsTheProseAlone(t *testing.T) {
	t.Parallel()
	req := chatTwice(t, "recallerprose", "recallprose")
	msgs, _ := req["messages"].([]any)
	if len(msgs) != 4 {
		raw, _ := json.Marshal(msgs)
		t.Fatalf("follow-up history: %d messages, want system, first, reply, follow-up: %s", len(msgs), raw)
	}
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		if mm["role"] == "tool" || mm["tool_calls"] != nil {
			t.Fatalf("a tool exchange replayed under replayTools none: %v", mm)
		}
	}
}
