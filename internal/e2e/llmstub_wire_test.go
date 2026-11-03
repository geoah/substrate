package e2e

import (
	"encoding/json"
	"testing"
)

func TestLLMStubContent(t *testing.T) {
	for _, content := range []string{
		`"{\"record\":{\"id\":\"transcript\"}}"`,
		`[{"type":"text","text":"{\"record\":{\"id\":\"transcript\"}}"},{"type":"text","text":"Repository context"}]`,
	} {
		var req llmReq
		if err := json.Unmarshal([]byte(`{"messages":[{"role":"user","content":`+content+`}]}`), &req); err != nil {
			t.Fatal(err)
		}
		if got := req.deliveredRecord()["id"]; got != "transcript" {
			t.Fatalf("delivered record id = %v", got)
		}
	}
	for _, content := range []string{`null`, `""`, `[]`} {
		var m llmMessage
		if err := json.Unmarshal([]byte(`{"role":"assistant","content":`+content+`}`), &m); err != nil {
			t.Fatal(err)
		}
		if m.Content != "" {
			t.Fatalf("empty content = %q", m.Content)
		}
	}
	var m llmMessage
	if err := json.Unmarshal([]byte(`{"content":[{"type":"image_url"}]}`), &m); err == nil {
		t.Fatal("unsupported content silently accepted")
	}
}
