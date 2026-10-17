package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/geoah/substrate/internal/substrate"
)

// The model's reasoning: streamed as its own event ahead of the text, kept
// on the assistant row as a bounded summary with the time spent, and never
// sent back to a model.

func assistantRows(t *testing.T, ds *dataset, thread string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, row := range threadMessages(t, ds, thread) {
		if row["role"] == "assistant" {
			out = append(out, row)
		}
	}
	return out
}

func TestAReasoningModelStreamsItsThinkingAndTheRowKeepsASummary(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("chat",
		fakeTurn{reasoning: "I should look it up first.", signature: "sig-1", content: "Checking.", calls: []fakeCall{{name: "nosuch", args: `{}`}}, finish: "tool_calls"},
		fakeTurn{reasoning: "The lookup failed, so I answer plainly.", content: "hello there"},
	)
	var events []substrate.AgentEvent
	res, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/chatter", "", "hi", func(ev substrate.AgentEvent) {
		events = append(events, ev)
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	// Per completion: every reasoning event comes before that completion's
	// first delta, and the pieces add up to the scripted thinking.
	var reasoning, text []string
	cur, curText, sawDelta := "", "", false
	flush := func() {
		if cur != "" || curText != "" {
			reasoning, text = append(reasoning, cur), append(text, curText)
		}
		cur, curText, sawDelta = "", "", false
	}
	for _, ev := range events {
		switch ev.Kind {
		case substrate.AgentEventReasoning:
			if sawDelta {
				t.Fatalf("a reasoning event after the turn's text began: %+v", events)
			}
			cur += ev.Text
		case substrate.AgentEventDelta:
			sawDelta = true
			curText += ev.Text
		case substrate.AgentEventToolStarted:
			flush()
		}
	}
	flush()
	if strings.Join(reasoning, "|") != "I should look it up first.|The lookup failed, so I answer plainly." ||
		strings.Join(text, "|") != "Checking.|hello there" {
		t.Fatalf("reasoning %q, text %q", reasoning, text)
	}
	rows := assistantRows(t, ds, res.Thread)
	if len(rows) != 2 {
		t.Fatalf("assistant rows: %d", len(rows))
	}
	for i, want := range []string{"I should look it up first.", "The lookup failed, so I answer plainly."} {
		if rows[i]["reasoning"] != want {
			t.Fatalf("row %d reasoning = %v", i, rows[i]["reasoning"])
		}
		if _, ok := rows[i]["reasoningMillis"]; !ok || intProp(rows[i], "reasoningMillis") < 0 {
			t.Fatalf("row %d reasoningMillis = %v", i, rows[i]["reasoningMillis"])
		}
	}
	// Within the run, the tool round trip carries the signed block back on
	// the assistant message that called the tool, and nowhere else.
	reqs := fake.requestsOf("chat")
	echo, _ := json.Marshal(reqs[1]["messages"])
	if strings.Count(string(echo), `"thinking_blocks"`) != 1 || !strings.Contains(string(echo), `"signature":"sig-1"`) {
		t.Fatalf("the tool round trip did not echo the signed block: %s", echo)
	}
	for _, row := range threadMessages(t, ds, res.Thread) {
		raw, _ := json.Marshal(row)
		if strings.Contains(string(raw), "sig-1") {
			t.Fatalf("a row kept the signed block: %s", raw)
		}
	}
	// A continuation replays the thread, and no reasoning reaches the model.
	fake.script("chat", fakeTurn{content: "again"})
	if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/chatter", res.Thread, "more", nil); err != nil {
		t.Fatalf("continue: %v", err)
	}
	reqs = fake.requestsOf("chat")
	for i, req := range reqs {
		if i == 1 {
			continue
		}
		raw, _ := json.Marshal(req["messages"])
		if strings.Contains(string(raw), "look it up") || strings.Contains(string(raw), "lookup failed") ||
			strings.Contains(string(raw), "reasoning") {
			t.Fatalf("request %d replayed reasoning: %s", i, raw)
		}
	}
}

func TestAOneShotReasoningKeepsTheTextWithoutATime(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("chat", fakeTurn{reasoning: "Short thought.", content: "done"})
	// A call streams to no client, so its completion is one-shot; a chat
	// turn always streams, into its hub.
	res, err := ds.CallAgent(ctx, crewPackage+"/chatter", "hi")
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	rows := assistantRows(t, ds, res.Thread)
	if len(rows) != 1 || rows[0]["reasoning"] != "Short thought." {
		t.Fatalf("assistant rows: %+v", rows)
	}
	if _, ok := rows[0]["reasoningMillis"]; ok {
		t.Fatalf("a one-shot completion recorded a thinking time: %+v", rows[0])
	}
}

func TestAModelWithoutReasoningWritesNoReasoningFields(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("chat", fakeTurn{content: "hello there"})
	var kinds []string
	res, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/chatter", "", "hi", func(ev substrate.AgentEvent) {
		kinds = append(kinds, ev.Kind)
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	for _, k := range kinds {
		if k == substrate.AgentEventReasoning {
			t.Fatalf("a reasoning event from a model that streamed none: %v", kinds)
		}
	}
	for _, row := range assistantRows(t, ds, res.Thread) {
		if _, ok := row["reasoning"]; ok {
			t.Fatalf("row carries reasoning: %+v", row)
		}
		if _, ok := row["reasoningMillis"]; ok {
			t.Fatalf("row carries reasoningMillis: %+v", row)
		}
	}
}

func TestReasoningIsBoundedOnTheRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	long := strings.Repeat("é", 6000) + strings.Repeat("z", 4000)
	fake.script("chat", fakeTurn{reasoning: long, content: "done"})
	var streamed int
	res, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/chatter", "", "hi", func(ev substrate.AgentEvent) {
		if ev.Kind == substrate.AgentEventReasoning {
			streamed += utf8.RuneCountInString(ev.Text)
		}
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	// The live stream carries every word; the row keeps the first 4000 runes.
	if streamed != 10000 {
		t.Fatalf("streamed %d runes of reasoning", streamed)
	}
	rows := assistantRows(t, ds, res.Thread)
	kept, _ := rows[0]["reasoning"].(string)
	if utf8.RuneCountInString(kept) != agentReasoningRunes || kept != strings.Repeat("é", agentReasoningRunes) {
		t.Fatalf("kept %d runes", utf8.RuneCountInString(kept))
	}
}

func TestAnAgentAskingForAReasoningSummarySendsTheThinkingKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("think", fakeTurn{reasoning: "Summarized.", content: "done"})
	fake.script("chat", fakeTurn{content: "done"})
	if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/thinker", "", "hi", func(substrate.AgentEvent) {}); err != nil {
		t.Fatalf("chat thinker: %v", err)
	}
	if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewPackage+"/chatter", "", "hi", func(substrate.AgentEvent) {}); err != nil {
		t.Fatalf("chat chatter: %v", err)
	}
	asked := fake.requestsOf("think")[0]
	thinking, _ := asked["thinking"].(map[string]any)
	if thinking["type"] != "adaptive" || thinking["display"] != "summarized" || asked["reasoning_effort"] != "low" {
		t.Fatalf("request = %v", asked)
	}
	if _, sent := fake.requestsOf("chat")[0]["thinking"]; sent {
		t.Fatalf("an agent that did not ask sent thinking: %v", fake.requestsOf("chat")[0])
	}
}
