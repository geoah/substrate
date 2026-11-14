package llm

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

// A message a person sends while a chat turn runs is handed to the model at
// the turn's next call (decision 0318). These are the conversations that
// makes, in the order the loop builds them and the order a later replay
// reads them back, each sent through both wires and held to the order that
// wire's vendor accepts.
var steeredConversations = []struct {
	name     string
	messages []Message
}{
	{
		name: "read after a call's result",
		messages: []Message{
			{Role: RoleUser, Content: "list my tasks"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call_0", Name: "query", Arguments: `{"kind":"task"}`}}},
			{Role: RoleTool, ToolCallID: "call_0", ToolName: "query", Content: `{"records":[]}`},
			{Role: RoleUser, Content: "only the open ones", Context: "<turn_context>\nnow: x\n</turn_context>"},
		},
	},
	{
		name: "read after the results of calls made together",
		messages: []Message{
			{Role: RoleUser, Content: "list my tasks and notes"},
			{Role: RoleAssistant, Content: "looking", ToolCalls: []ToolCall{
				{ID: "call_0", Name: "query", Arguments: `{"kind":"task"}`},
				{ID: "call_1", Name: "query", Arguments: `{"kind":"note"}`},
			}},
			{Role: RoleTool, ToolCallID: "call_0", ToolName: "query", Content: `{"records":[]}`},
			{Role: RoleTool, ToolCallID: "call_1", ToolName: "query", Content: `{"records":[]}`},
			{Role: RoleUser, Content: "skip the notes"},
			{Role: RoleUser, Content: "and sort by date"},
		},
	},
	{
		name: "read after a background job's report",
		messages: []Message{
			{Role: RoleUser, Content: "import the file"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call_0", Name: "wait", Arguments: `{}`}}},
			{Role: RoleTool, ToolCallID: "call_0", ToolName: "wait", Content: `{"state":"working"}`},
			{Role: RoleUser, Content: `{"event":"toolCompleted"}`},
			{Role: RoleUser, Content: "is it done?"},
		},
	},
	{
		name: "read at the turn's first call",
		messages: []Message{
			{Role: RoleUser, Content: "list my tasks"},
			{Role: RoleUser, Content: "the open ones"},
		},
	},
	{
		name: "starting the next turn after a finished reply",
		messages: []Message{
			{Role: RoleUser, Content: "list my tasks"},
			{Role: RoleAssistant, Content: "You have none."},
			{Role: RoleUser, Content: "and my notes?"},
		},
	},
	{
		name: "replayed after calls and a later turn",
		messages: []Message{
			{Role: RoleUser, Content: "list my tasks"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call_0", Name: "query", Arguments: `{"kind":"task"}`}}},
			{Role: RoleTool, ToolCallID: "call_0", ToolName: "query", Content: `{"records":[]}`},
			{Role: RoleUser, Content: "only the open ones"},
			{Role: RoleAssistant, Content: "None are open."},
			{Role: RoleUser, Content: "thanks"},
		},
	},
}

func TestASteeredConversationIsOneEachWireAccepts(t *testing.T) {
	for _, tc := range steeredConversations {
		t.Run(tc.name+"/anthropic", func(t *testing.T) {
			srv := newAnthropicServer(t, func(w http.ResponseWriter, _ map[string]any) {
				jsonBody(w, map[string]any{
					"id": "msg_1", "type": "message", "role": "assistant", "model": "m",
					"content": []any{}, "usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
				})
			})
			if _, err := srv.client(t, nil).Complete(context.Background(), Request{Model: "m", Messages: tc.messages}, nil); err != nil {
				t.Fatalf("complete: %v", err)
			}
			wire, _ := srv.last["messages"].([]any)
			if err := anthropicAccepts(wire); err != nil {
				t.Fatalf("%v\nmessages = %s", err, mustJSON(wire))
			}
		})
		t.Run(tc.name+"/openai", func(t *testing.T) {
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
			if _, err := client.Complete(context.Background(), Request{Model: "m", Messages: tc.messages}, nil); err != nil {
				t.Fatalf("complete: %v", err)
			}
			wire, _ := srv.last["messages"].([]any)
			if err := openAIAccepts(wire); err != nil {
				t.Fatalf("%v\nmessages = %s", err, mustJSON(wire))
			}
		})
	}
}

// The two checks refuse what the vendors refuse, so a pass above means
// something.
func TestTheWireOrderChecksRefuseAMessageBetweenACallAndItsResult(t *testing.T) {
	call := map[string]any{"type": "tool_use", "id": "call_0", "name": "query", "input": map[string]any{}}
	result := map[string]any{"type": "tool_result", "tool_use_id": "call_0", "content": "{}"}
	text := map[string]any{"type": "text", "text": "wait"}
	if err := anthropicAccepts([]any{
		map[string]any{"role": "user", "content": []any{text}},
		map[string]any{"role": "assistant", "content": []any{call}},
		map[string]any{"role": "user", "content": []any{text, result}},
	}); err == nil {
		t.Fatal("anthropic: text before a tool result in the answering turn was accepted")
	}
	if err := anthropicAccepts([]any{
		map[string]any{"role": "user", "content": []any{text}},
		map[string]any{"role": "assistant", "content": []any{call}},
		map[string]any{"role": "user", "content": []any{text}},
	}); err == nil {
		t.Fatal("anthropic: a call with no result in the next turn was accepted")
	}
	if err := anthropicAccepts([]any{
		map[string]any{"role": "user", "content": []any{text}},
		map[string]any{"role": "assistant", "content": []any{text}},
	}); err == nil {
		t.Fatal("anthropic: a conversation ending on the assistant was accepted")
	}
	calls := []any{map[string]any{"id": "call_0", "type": "function"}}
	if err := openAIAccepts([]any{
		map[string]any{"role": "user", "content": "go"},
		map[string]any{"role": "assistant", "tool_calls": calls},
		map[string]any{"role": "user", "content": "wait"},
		map[string]any{"role": "tool", "tool_call_id": "call_0", "content": "{}"},
	}); err == nil {
		t.Fatal("openai: a user message between a call and its result was accepted")
	}
}

// anthropicAccepts holds a Messages API body's `messages` to the order the
// API accepts: the first turn is the user's and the roles alternate; every
// `tool_use` of an assistant turn is answered by a `tool_result` in the next
// turn, whose results come before any of its text; no result answers a call
// the turn before did not make; and the conversation ends on the user.
func anthropicAccepts(messages []any) error {
	var open map[string]bool
	want := "user"
	for i, raw := range messages {
		m, _ := raw.(map[string]any)
		role, _ := m["role"].(string)
		if role != want {
			return fmt.Errorf("message %d has role %q, want %q: the roles alternate from the user", i, role, want)
		}
		blocks, _ := m["content"].([]any)
		if role == "assistant" {
			open = map[string]bool{}
			for _, b := range blocks {
				if block, _ := b.(map[string]any); block["type"] == "tool_use" {
					open[block["id"].(string)] = true
				}
			}
			want = "user"
			continue
		}
		sawOther := false
		for _, b := range blocks {
			block, _ := b.(map[string]any)
			if block["type"] != "tool_result" {
				sawOther = true
				continue
			}
			id, _ := block["tool_use_id"].(string)
			if !open[id] {
				return fmt.Errorf("message %d answers call %q, which the turn before did not make", i, id)
			}
			if sawOther {
				return fmt.Errorf("message %d has text before the result of call %q", i, id)
			}
			delete(open, id)
		}
		if len(open) > 0 {
			return fmt.Errorf("message %d leaves %d calls of the turn before unanswered", i, len(open))
		}
		want = "assistant"
	}
	if want == "user" && len(messages) > 0 {
		return fmt.Errorf("the conversation ends on the assistant's turn")
	}
	return nil
}

// openAIAccepts holds a chat completions body's `messages` to the order the
// API accepts: the `tool` messages that answer an assistant message's
// `tool_calls` follow it at once, one per call, and nothing else comes
// between; no `tool` message stands anywhere else.
func openAIAccepts(messages []any) error {
	open := map[string]bool{}
	for i, raw := range messages {
		m, _ := raw.(map[string]any)
		role, _ := m["role"].(string)
		if role == "tool" {
			id, _ := m["tool_call_id"].(string)
			if !open[id] {
				return fmt.Errorf("message %d answers call %q, which is not waiting for a result", i, id)
			}
			delete(open, id)
			continue
		}
		if len(open) > 0 {
			return fmt.Errorf("message %d (%s) comes before %d results of the calls before it", i, role, len(open))
		}
		if role == "assistant" {
			calls, _ := m["tool_calls"].([]any)
			for _, c := range calls {
				call, _ := c.(map[string]any)
				open[call["id"].(string)] = true
			}
		}
	}
	if len(open) > 0 {
		return fmt.Errorf("the conversation ends with %d calls unanswered", len(open))
	}
	return nil
}
