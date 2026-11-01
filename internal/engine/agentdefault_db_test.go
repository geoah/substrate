package engine

// The seeded default agent, as shipped: its system prompt carries the
// owner's active memories, and its tools include what installed bundles
// offer, on the request the model actually receives.

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestDefaultAgentPinsMemoriesAndTakesOfferedTools(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t, WithLLMSampleSeed(true))
	fake := provisionAgents(t, ds)
	// The sample names the `openai` row; point it at the fake server.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeProvider, ID: "openai",
		Properties: map[string]any{"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-openai"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(offersPackage, 0),
		vocabulary.BundleManifest(offersPackage, map[string]any{
			"description": "a bundle offering one function",
			"installs":    []any{offersPackage + "/today"},
		}),
		vocabulary.FunctionManifest(offersPackage, "today", map[string]any{
			"description": "what needs the owner today",
			"runtime":     vocabulary.RuntimePython,
			"effect":      vocabulary.FunctionEffectRead,
			"offer":       map[string]any{"agents": true},
			"source":      "def main(input, host):\n    return {\"output\": {}}\n",
		}),
	}); err != nil {
		t.Fatalf("install the offering bundle: %v", err)
	}
	putMemory(t, ds, "veg", "The owner is vegetarian.", nil)
	putMemory(t, ds, "old", "The owner lives in Lisbon.", map[string]any{"active": false})

	fake.script("gpt-5", fakeTurn{content: "hello"})
	if _, err := ds.ChatAgent(ctx, substrate.ActorAPI, testdb.Repository(t)+"/llm/substrate", substrate.ChatInput{Message: "hi"}, nil); err != nil {
		t.Fatalf("chat: %v", err)
	}
	reqs := fake.requestsOf("gpt-5")
	if len(reqs) != 1 {
		t.Fatalf("completions: %d, want 1", len(reqs))
	}
	system := pinnedSystemOf(t, reqs[0])
	want := pinnedOpen + "\n" + pinnedIntro + "\n" +
		`<records kind="` + kindMemory + `">` + "\n" +
		`{"id":"veg","text":"The owner is vegetarian."}` + "\n" +
		"</records>\n" + pinnedClose
	if !strings.HasSuffix(system, want) {
		t.Fatalf("system prompt does not end with the active memories:\n%s", system)
	}
	if !strings.Contains(system, engineGuidance) {
		t.Fatal("system prompt carries no engine guidance")
	}
	names := toolNamesSent(t, reqs[0])
	for _, name := range []string{"query", "write", "propose", "ask", "offers_today"} {
		if !slices.Contains(names, name) {
			t.Fatalf("tools = %v, want %s among them", names, name)
		}
	}
	if got := reqs[0]["reasoning_effort"]; got != "low" {
		t.Fatalf("reasoning_effort = %v, want low", got)
	}
}
