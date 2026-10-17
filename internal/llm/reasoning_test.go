package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// The fixtures under testdata/ are LiteLLM's openai-wire replies for
// anthropic/claude-haiku-5-5, recorded on 2026-10-17 with ids, timestamps and
// signatures redacted. A thinking model streams its text in
// delta.reasoning_content ahead of the content, and repeats it with the
// signature in delta.thinking_blocks; with reasoning_effort alone the thinking
// text is empty and only the signed block arrives.

func fixtureServer(t *testing.T, name, contentType string) *httptest.Server {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func fixtureClient(t *testing.T, name, contentType string) Client {
	t.Helper()
	client, err := New(WireOpenAI, Config{BaseURL: fixtureServer(t, name, contentType).URL, APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestOpenAIStreamsReasoningBeforeTheToolCall(t *testing.T) {
	client := fixtureClient(t, "litellm-thinking-tool-call.sse", "text/event-stream")
	var got []Delta
	res, err := client.Complete(context.Background(), Request{
		Model: "anthropic/claude-haiku-5-5", Messages: []Message{{Role: RoleUser, Content: "weather?"}},
	}, func(d Delta) {
		if (d.Text == "") == (d.Reasoning == "") {
			t.Fatalf("a delta must set exactly one of Text and Reasoning: %+v", d)
		}
		got = append(got, d)
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	const wantReasoning = "Germany's capital shifted from Bonn to Berlin after reunification, so I should get the weather for Berlin.\n\n"
	if res.Reasoning != wantReasoning {
		t.Fatalf("reasoning = %q", res.Reasoning)
	}
	var reasoning, text strings.Builder
	sawText := false
	for _, d := range got {
		if d.Reasoning != "" {
			if sawText {
				t.Fatalf("a reasoning delta after the text began: %+v", got)
			}
			reasoning.WriteString(d.Reasoning)
		}
		if d.Text != "" {
			sawText = true
			text.WriteString(d.Text)
		}
	}
	// The final thinking_blocks chunk repeats the whole text with the
	// signature: it must not be counted twice.
	if reasoning.String() != res.Reasoning {
		t.Fatalf("streamed reasoning %q, result %q", reasoning.String(), res.Reasoning)
	}
	if text.String() != res.Content || !strings.HasPrefix(res.Content, "The country is Germany.") {
		t.Fatalf("streamed text %q, content %q", text.String(), res.Content)
	}
	if strings.Contains(res.Content, "reunification, so I should") {
		t.Fatalf("the reasoning leaked into the content: %q", res.Content)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].Name != "get_weather" || res.ToolCalls[0].Arguments != `{"city": "Berlin"}` {
		t.Fatalf("tool calls = %+v", res.ToolCalls)
	}
	if res.Stop != StopToolCall {
		t.Fatalf("stop = %q", res.Stop)
	}
}

func TestOpenAIEffortWithoutThinkingTextStreamsNoReasoning(t *testing.T) {
	client := fixtureClient(t, "litellm-effort-only.sse", "text/event-stream")
	res, err := client.Complete(context.Background(), Request{
		Model: "anthropic/claude-haiku-5-5", Messages: []Message{{Role: RoleUser, Content: "37*43-17?"}},
	}, func(d Delta) {
		if d.Reasoning != "" {
			t.Fatalf("a reasoning delta from a stream with no thinking text: %+v", d)
		}
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if res.Reasoning != "" || res.Content != "1574" {
		t.Fatalf("result = %+v", res)
	}
}

func TestOpenAIOneShotReadsReasoningContent(t *testing.T) {
	client := fixtureClient(t, "litellm-thinking-oneshot.json", "application/json")
	res, err := client.Complete(context.Background(), Request{
		Model: "anthropic/claude-haiku-5-5", Messages: []Message{{Role: RoleUser, Content: "37*43-17?"}},
	}, nil)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if res.Content != "1574" || !strings.HasPrefix(res.Reasoning, "37*43=1591, minus 17 gives 1574.") {
		t.Fatalf("result = %+v", res)
	}
}

// The native Messages wire streams a thinking block as thinking_delta
// fragments and a signature_delta before the block stops; a
// redacted_thinking block arrives whole, with opaque data and no text.
var anthropicThinkingEvents = []string{
	`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[],"usage":{"input_tokens":5,"output_tokens":0}}}`,
	`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Bonn moved, "}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"so Berlin."}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-part-1"}}`,
	`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-part-2"}}`,
	`{"type":"content_block_stop","index":0}`,
	`{"type":"content_block_start","index":1,"content_block":{"type":"redacted_thinking","data":"opaque-data"}}`,
	`{"type":"content_block_stop","index":1}`,
	`{"type":"content_block_start","index":2,"content_block":{"type":"text","text":""}}`,
	`{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"Checking "}}`,
	`{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"Berlin."}}`,
	`{"type":"content_block_stop","index":2}`,
	`{"type":"content_block_start","index":3,"content_block":{"type":"tool_use","id":"toolu_0","name":"get_weather","input":{}}}`,
	`{"type":"content_block_delta","index":3,"delta":{"type":"input_json_delta","partial_json":"{\"city\":\"Berlin\"}"}}`,
	`{"type":"content_block_stop","index":3}`,
	`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":9}}`,
	`{"type":"message_stop"}`,
}

func anthropicEventServer(t *testing.T, events []string) *anthropicServer {
	t.Helper()
	return newAnthropicServer(t, func(w http.ResponseWriter, _ map[string]any) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			var typed struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal([]byte(e), &typed)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typed.Type, e)
		}
	})
}

func TestAnthropicStreamsThinkingBeforeTextAndKeepsTheSignature(t *testing.T) {
	srv := anthropicEventServer(t, anthropicThinkingEvents)
	var got []Delta
	client := newAnthropic(Config{BaseURL: srv.srv.URL, APIKey: "sk-test"})
	msg, err := client.complete(context.Background(), anthropicParams(Request{
		Model: "m", Messages: []Message{{Role: RoleUser, Content: "weather?"}},
	}), func(d Delta) { got = append(got, d) })
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	want := []Delta{{Reasoning: "Bonn moved, "}, {Reasoning: "so Berlin."}, {Text: "Checking "}, {Text: "Berlin."}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("deltas = %+v, want %+v", got, want)
	}
	thinking, redacted := msg.Content[0], msg.Content[1]
	if thinking.Type != "thinking" || thinking.Thinking != "Bonn moved, so Berlin." || thinking.Signature != "sig-part-1sig-part-2" || thinking.Text != "" {
		t.Fatalf("thinking block = %+v", thinking)
	}
	if redacted.Type != "redacted_thinking" || redacted.Data != "opaque-data" || redacted.Thinking != "" {
		t.Fatalf("redacted block = %+v", redacted)
	}
	res := anthropicResult(msg)
	if res.Reasoning != "Bonn moved, so Berlin." || res.Content != "Checking Berlin." {
		t.Fatalf("result = %+v", res)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].Arguments != `{"city":"Berlin"}` || res.Stop != StopToolCall {
		t.Fatalf("tool calls = %+v, stop %q", res.ToolCalls, res.Stop)
	}
}

func TestAnthropicOneShotReadsThinkingBlocks(t *testing.T) {
	srv := newAnthropicServer(t, func(w http.ResponseWriter, _ map[string]any) {
		jsonBody(w, map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "model": "m",
			"content": []any{
				map[string]any{"type": "thinking", "thinking": "add them", "signature": "sig"},
				map[string]any{"type": "redacted_thinking", "data": "opaque"},
				map[string]any{"type": "text", "text": "4"},
			},
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 3, "output_tokens": 2},
		})
	})
	res, err := srv.client(t, nil).Complete(context.Background(), Request{
		Model: "m", Messages: []Message{{Role: RoleUser, Content: "2+2"}},
	}, nil)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if res.Reasoning != "add them" || res.Content != "4" {
		t.Fatalf("result = %+v", res)
	}
}

func TestAnthropicStreamWithoutThinkingCallsNoReasoningDelta(t *testing.T) {
	srv := anthropicEventServer(t, []string{
		`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[],"usage":{"input_tokens":5,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
		`{"type":"message_stop"}`,
	})
	res, err := srv.client(t, nil).Complete(context.Background(), Request{
		Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, func(d Delta) {
		if d.Reasoning != "" {
			t.Fatalf("a reasoning delta from a stream with no thinking: %+v", d)
		}
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if res.Reasoning != "" || res.Content != "hi" {
		t.Fatalf("result = %+v", res)
	}
}
