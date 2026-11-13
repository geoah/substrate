package engine

// The shipped assistant, Moss: its system prompt carries the owner's active
// memories, and its tools include what installed bundles offer, on the
// request the model actually receives.

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestDefaultAgentPinsMemoriesAndTakesOfferedTools(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t)
	fake := provisionAgents(t, ds)
	// Moss names the `anthropic` row; point it at the fake server, on the
	// wire the fake speaks.
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{
		Kind: typeProvider, ID: "anthropic",
		Properties: map[string]any{"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-anthropic"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
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
	putMemory(t, ds, fid("veg"), "The owner is vegetarian.", nil)
	putMemory(t, ds, fid("old"), "The owner lives in Lisbon.", map[string]any{"active": false})

	fake.script(mossModel, fakeTurn{content: "hello"})
	if _, err := ds.ChatAgent(ctx, substrate.ActorPerson, mossID, substrate.ChatInput{Message: "hi"}, nil); err != nil {
		t.Fatalf("chat: %v", err)
	}
	reqs := fake.requestsOf(mossModel)
	if len(reqs) != 1 {
		t.Fatalf("completions: %d, want 1", len(reqs))
	}
	system := pinnedSystemOf(t, reqs[0])
	want := pinnedOpen + "\n" + pinnedIntro + "\n" +
		`<records kind="` + kindMemory + `">` + "\n" +
		`{"id":"` + fid("veg") + `","text":"The owner is vegetarian."}` + "\n" +
		"</records>\n" + pinnedClose
	if !strings.HasSuffix(system, want) {
		t.Fatalf("system prompt does not end with the active memories:\n%s", system)
	}
	if !strings.Contains(system, engineGuidance) {
		t.Fatal("system prompt carries no engine guidance")
	}
	names := toolNamesSent(t, reqs[0])
	for _, name := range []string{"query", "write", "propose", "ask", "wait", "edit", "offers_today"} {
		if !slices.Contains(names, name) {
			t.Fatalf("tools = %v, want %s among them", names, name)
		}
	}
	if got := reqs[0]["reasoning_effort"]; got != "low" {
		t.Fatalf("reasoning_effort = %v, want low", got)
	}
}
