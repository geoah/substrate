package engine

// The seeded secretary: Moss hands it a mail job, and it runs on the Google
// provider's offered tools alone, taken by pattern when the run builds its
// tools. Moss itself takes every offered function. Stand-in bundles offer
// one function each under the Google provider's package name, under another
// provider's and under a third authority, and a sync function offers
// nothing; no tool body runs.

import (
	"context"
	"slices"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const secretaryModel = "claude-sonnet-5-5"

func TestSecretaryTakesTheGoogleToolsAlone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake, _ := openMossDataset(t)

	fn := func(pkg, name string, offer map[string]any) map[string]any {
		data := map[string]any{
			"description": name + " under test",
			"runtime":     vocabulary.RuntimePython,
			"effect":      vocabulary.FunctionEffectRead,
			"source":      "def main(input, host):\n    return {\"output\": {}}\n",
		}
		if offer != nil {
			data["offer"] = offer
		}
		return vocabulary.FunctionManifest(pkg, name, data)
	}
	offer := func(tool string) map[string]any { return map[string]any{"agents": true, "name": tool} }
	for pkg, fns := range map[string][]map[string]any{
		"providers.substrate.reamde.dev/google": {
			fn("providers.substrate.reamde.dev/google", "searchmail", offer("gmail_search")),
			fn("providers.substrate.reamde.dev/google", "syncgmail", nil),
		},
		"providers.substrate.reamde.dev/slack": {
			fn("providers.substrate.reamde.dev/slack", "postmessage", offer("slack_post")),
		},
		"web.test.dev/web": {
			fn("web.test.dev/web", "websearch", offer("web_search")),
		},
	} {
		var installs []any
		for _, f := range fns {
			installs = append(installs, f["metadata"].(map[string]any)["id"])
		}
		docs := []map[string]any{
			vocabulary.PackageManifest(pkg, 0),
			vocabulary.BundleManifest(pkg, map[string]any{"description": "a stand-in bundle", "installs": installs}),
		}
		if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, append(docs, fns...)); err != nil {
			t.Fatalf("install %s: %v", pkg, err)
		}
	}

	fake.script(mossModel,
		fakeTurn{calls: []fakeCall{{"secretary", toolArgs(t, map[string]any{
			"input": "Find Sam's mail about Friday. The owner asked only to look. Now: 2026-11-09 10:00 Europe/Athens.",
		})}}},
		fakeTurn{content: "Nothing from Sam about Friday yet."},
	)
	fake.script(secretaryModel, fakeTurn{content: "No mail from Sam mentions Friday."})
	res, err := ds.ChatAgent(ctx, substrate.ActorPerson, mossID, substrate.ChatInput{Message: "did Sam write about Friday?"}, nil)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if res.Status != threadOK {
		t.Fatalf("settled %q (%s)", res.Status, res.Reason)
	}

	moss := toolNamesSent(t, fake.requestsOf(mossModel)[0])
	for _, name := range []string{"secretary", "gmail_search", "slack_post", "web_search"} {
		if !slices.Contains(moss, name) {
			t.Fatalf("Moss's tools = %v, want %s among them", moss, name)
		}
	}
	reqs := fake.requestsOf(secretaryModel)
	if len(reqs) != 1 {
		t.Fatalf("the secretary ran %d turns, want 1", len(reqs))
	}
	if got := toolNamesSent(t, reqs[0]); !slices.Equal(got, []string{"query", "gmail_search"}) {
		t.Fatalf("the secretary's tools = %v, want query and the Google provider's offered tool alone", got)
	}
}
