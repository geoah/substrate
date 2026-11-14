package api

import (
	"encoding/json"
	"testing"
)

// A history row's values lose the `heldTo` a run's record took in them, and
// nothing else: a kind of the owner's that names a property the same keeps
// it, and the payload the engine handed over is left as it was.
func TestAHistoryRowsValuesLoseWhoseRulesHoldARun(t *testing.T) {
	const raw = `{"fold":[
		{"kind":"record","ref":"substrate.reamde.dev/llm/message","id":"m1","delta":{"set":{"content":"hi","heldTo":{"tokens":[{"ref":"substrate.reamde.dev/core/token/t1"}]}}}},
		{"kind":"record","ref":"x.example.com/notes/note","id":"n1","delta":{"set":{"heldTo":"the owner's own"}}},
		{"kind":"mark","ref":"substrate.reamde.dev/llm/message","id":"m1","property":"heldTo"}
	]}`
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(withoutHeldToEffects(payload))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Fold []struct {
			Delta struct {
				Set map[string]any `json:"set"`
			} `json:"delta"`
		} `json:"fold"`
	}
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatal(err)
	}
	if set := out.Fold[0].Delta.Set; set["heldTo"] != nil || set["content"] != "hi" {
		t.Fatalf("the message's values = %v, want its content without heldTo", set)
	}
	if set := out.Fold[1].Delta.Set; set["heldTo"] != "the owner's own" {
		t.Fatalf("the note's values = %v, want its own heldTo kept", set)
	}
	again, _ := json.Marshal(payload)
	var before map[string]any
	_ = json.Unmarshal([]byte(raw), &before)
	want, _ := json.Marshal(before)
	if string(again) != string(want) {
		t.Fatalf("the payload handed over was changed: %s", again)
	}
}
