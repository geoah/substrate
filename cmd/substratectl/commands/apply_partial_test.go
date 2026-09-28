package commands

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

const partialAgentID = "mine.example.com/mine/helper"

func seedAgent(h *harness) {
	at := time.Unix(1_700_000_000, 0).UTC()
	h.fake.seed(&substrate.Record{
		ID: partialAgentID, Kind: "substrate.reamde.dev/core/agent", Version: 4,
		CreatedAt: at, UpdatedAt: at,
		Labels: map[string]any{"owner/pinned": true},
		Properties: map[string]any{
			"authority": "mine.example.com", "package": "mine",
			"description": "helps", "prompt": "You help.", "provider": "openai",
			"model": "gpt-5", "hiddenFromChat": false,
			// Stamped, never authored: the whole document leaves it out.
			"source": "authored",
		},
	})
}

// sentDocuments is the batch the last vocabulary apply carried.
func sentDocuments(t *testing.T, h *harness) []map[string]any {
	t.Helper()
	var docs []map[string]any
	if err := json.Unmarshal(h.fake.lastBody["documents"], &docs); err != nil {
		t.Fatalf("documents: %v", err)
	}
	return docs
}

// A declaration naming neither data.authority nor data.package can only be a
// change to a stored one: apply is put, so it is laid over the stored
// declaration and only the keys it writes change. The record habit of writing
// them under data.properties reads the same.
func TestApplyCompletesAPartialDeclaration(t *testing.T) {
	for name, body := range map[string]string{
		"keys under data":            "data:\n  hiddenFromChat: true\n",
		"keys under data.properties": "data:\n  properties:\n    hiddenFromChat: true\n",
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.writeConfig()
			seedAgent(h)
			h.stdin.WriteString("kind: substrate.reamde.dev/core/agent\nmetadata:\n  id: " + partialAgentID + "\n" + body)
			out, _ := h.mustRun("apply", "-f", "-")
			if !strings.Contains(out, "agent/"+partialAgentID+" applied") {
				t.Fatalf("output:\n%s", out)
			}
			docs := sentDocuments(t, h)
			if len(docs) != 1 {
				t.Fatalf("sent %d documents", len(docs))
			}
			data, _ := docs[0]["data"].(map[string]any)
			want := map[string]any{
				"authority": "mine.example.com", "package": "mine", "description": "helps",
				"prompt": "You help.", "provider": "openai", "model": "gpt-5", "hiddenFromChat": true,
			}
			for k, v := range want {
				if data[k] != v {
					t.Errorf("data.%s = %v, want %v", k, data[k], v)
				}
			}
			for _, k := range []string{"properties", "source"} {
				if _, ok := data[k]; ok {
					t.Errorf("data carries %q: %v", k, data)
				}
			}
			meta, _ := docs[0]["metadata"].(map[string]any)
			if labels, _ := meta["labels"].(map[string]any); labels["owner/pinned"] != true {
				t.Errorf("the stored labels did not ride along: %v", meta)
			}
		})
	}
}

// A document naming data.authority or data.package is whole, and goes out
// exactly as written: dropping a key from a file still drops it.
func TestApplySendsAWholeDeclarationUntouched(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	seedAgent(h)
	h.stdin.WriteString("kind: substrate.reamde.dev/core/agent\nmetadata:\n  id: " + partialAgentID +
		"\ndata:\n  authority: mine.example.com\n  package: mine\n  prompt: Only this.\n")
	h.mustRun("apply", "-f", "-")
	data, _ := sentDocuments(t, h)[0]["data"].(map[string]any)
	if len(data) != 3 || data["prompt"] != "Only this." {
		t.Fatalf("a whole declaration was changed on the way: %v", data)
	}
	for _, req := range h.fake.requests {
		if strings.HasPrefix(req, "GET /api/v1/substrate.reamde.dev/core/agent/") {
			t.Fatalf("a whole declaration read the stored one: %v", h.fake.requests)
		}
	}
}

func TestApplyRefusesAPartialDeclarationNothingStores(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.stdin.WriteString("kind: substrate.reamde.dev/core/agent\nmetadata:\n  id: mine.example.com/mine/nobody\ndata:\n  hiddenFromChat: true\n")
	_, errOut, err := h.run("apply", "-f", "-")
	if err == nil {
		t.Fatal("a partial declaration of nothing stored was applied")
	}
	if msg := err.Error() + errOut; !strings.Contains(msg, "no such declaration to change") ||
		!strings.Contains(msg, "data.authority and data.package") {
		t.Fatalf("refusal: %v %s", err, errOut)
	}
}
