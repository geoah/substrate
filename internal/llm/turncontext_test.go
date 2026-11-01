package llm

import (
	"context"
	"net/http"
	"testing"
)

// A user turn's context is a content block of its own on both wires, after
// the text, and a turn with no text sends the context alone (decision 0196).
func TestTurnContextIsItsOwnBlockOnBothWires(t *testing.T) {
	messages := []Message{
		{Role: RoleUser, Content: "what is due?", Context: "<turn_context>\nnow: x\n</turn_context>"},
		{Role: RoleAssistant, Content: "nothing"},
		{Role: RoleUser, Context: "<turn_context>\nnow: y\n</turn_context>"},
	}
	t.Run("anthropic", func(t *testing.T) {
		srv := newAnthropicServer(t, func(w http.ResponseWriter, _ map[string]any) {
			jsonBody(w, map[string]any{
				"id": "msg_1", "type": "message", "role": "assistant", "model": "m",
				"content": []any{}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
			})
		})
		if _, err := srv.client(t, nil).Complete(context.Background(), Request{Model: "m", Messages: messages}, nil); err != nil {
			t.Fatalf("complete: %v", err)
		}
		wire, _ := srv.last["messages"].([]any)
		if len(wire) != 3 {
			t.Fatalf("messages = %s", mustJSON(wire))
		}
		first, _ := wire[0].(map[string]any)["content"].([]any)
		if len(first) != 2 || first[0].(map[string]any)["text"] != messages[0].Content || first[1].(map[string]any)["text"] != messages[0].Context {
			t.Fatalf("first user turn = %s", mustJSON(first))
		}
		last, _ := wire[2].(map[string]any)["content"].([]any)
		if len(last) != 1 || last[0].(map[string]any)["text"] != messages[2].Context {
			t.Fatalf("context-only turn = %s", mustJSON(last))
		}
	})
	t.Run("openai", func(t *testing.T) {
		srv := newOpenAIServer(t, func(w http.ResponseWriter, _ map[string]any) {
			jsonBody(w, map[string]any{
				"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "ok"}}},
				"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
			})
		})
		client, err := New(WireOpenAI, Config{BaseURL: srv.srv.URL, APIKey: "k"})
		if err != nil {
			t.Fatalf("new: %v", err)
		}
		if _, err := client.Complete(context.Background(), Request{Model: "m", Messages: messages}, nil); err != nil {
			t.Fatalf("complete: %v", err)
		}
		wire, _ := srv.last["messages"].([]any)
		if len(wire) != 3 {
			t.Fatalf("messages = %s", mustJSON(wire))
		}
		parts, _ := wire[0].(map[string]any)["content"].([]any)
		if len(parts) != 2 || parts[0].(map[string]any)["text"] != messages[0].Content || parts[1].(map[string]any)["text"] != messages[0].Context {
			t.Fatalf("first user turn = %s", mustJSON(wire[0]))
		}
		if got := wire[2].(map[string]any)["content"]; got != messages[2].Context {
			t.Fatalf("context-only turn = %s", mustJSON(wire[2]))
		}
	})
}
