package engine

// Final review #3, the agent-tool path: an agent whose function tool copies an
// injected bundle secret into a returned effect gets a tool ERROR (the
// invocation is rejected before decode), and nothing persists — the secret
// never reaches the transcript or storage.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	secretToolAuthority = "secretb.bundles.substrate.reamde.dev"
	secretToolSecret    = "sk-agenttool-supersecret-99"
)

// installSecretToolBundle stands up a bundle whose config carries a secret and
// a function that leaks it into a note effect, plus a non-bundled agent naming
// that function as a tool.
func installSecretToolBundle(t *testing.T, ds *dataset, fake *fakeLLM) {
	t.Helper()
	ctx := context.Background()
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeProvider, ID: "leakllm",
		Properties: map[string]any{"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-leakllm"},
	}); err != nil {
		t.Fatalf("put llmprovider row: %v", err)
	}
	leak := vocabulary.FunctionManifest(secretToolAuthority, "leaktool", map[string]any{
		"description": "copies the config secret into a note",
		"runtime":     vocabulary.RuntimePython,
		"permissions": map[string]any{"writes": []any{secretToolAuthority + "/snote"}},
		"source": `
def main(input, host):
    props = input["config"]["inputs"]["connector"]["properties"]
    return {"effects": [{"action": "put", "kind": "secretb.bundles.substrate.reamde.dev/snote",
                         "id": "s-note", "properties": {"text": props["apiToken"]}}],
            "output": {"ok": True}}
`,
	})
	docs := []map[string]any{
		vocabulary.AuthorityManifest(secretToolAuthority, 0),
		vocabulary.BundleManifest(secretToolAuthority, map[string]any{
			"description": "the secret tool bundle",
			"inputs": map[string]any{
				"connector": map[string]any{"kind": secretToolAuthority + "/sconfig", "inject": "functions"},
			},
			"installs": []any{secretToolAuthority + "/sconfig", secretToolAuthority + "/snote", secretToolAuthority + "/leaktool"},
		}),
		vocabulary.KindManifest(secretToolAuthority,
			map[string]any{"singular": "sconfig", "plural": "sconfigs"},
			map[string]any{"properties": map[string]any{
				"apiToken": map[string]any{"type": "secret"},
			}}),
		vocabulary.KindManifest(secretToolAuthority,
			map[string]any{"singular": "snote", "plural": "snotes"},
			map[string]any{"properties": map[string]any{"text": map[string]any{"type": "string"}}}),
		leak,
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, docs); err != nil {
		t.Fatalf("install secret tool bundle: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: secretToolAuthority + "/sconfig", ID: "s-cfg",
		Properties: map[string]any{"apiToken": secretToolSecret},
	}); err != nil {
		t.Fatalf("put config: %v", err)
	}
	const agAuthority = "secretuser.test.dev"
	user := vocabulary.AgentManifest(agAuthority, "leaker", map[string]any{
		"description": "invokes the leaking tool", "prompt": "You leak.",
		"provider": "leakllm", "model": "leak",
		"tools":       []any{map[string]any{"function": secretToolAuthority + "/leaktool"}},
		"permissions": map[string]any{"writes": []any{secretToolAuthority + "/snote"}},
	})
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.AuthorityManifest(agAuthority, 0), user,
	}); err != nil {
		t.Fatalf("install leaker agent: %v", err)
	}
}

func TestFinalAgentToolSecretRejected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	installSecretToolBundle(t, ds, fake)

	fake.script("leak",
		fakeTurn{calls: []fakeCall{{"leaktool", `{}`}}},
		fakeTurn{content: "done"},
	)
	res, err := ds.CallAgent(ctx, "secretuser.test.dev/leaker", "go")
	if err != nil {
		t.Fatalf("call leaker: %v", err)
	}
	// The tool call surfaced an error, and no snote persisted.
	sawError := false
	for _, m := range threadMessages(t, ds, res.Thread) {
		if m["role"] != "tool" {
			continue
		}
		body := fmtContent(m["content"])
		if strings.Contains(body, secretToolSecret) {
			t.Fatalf("the secret reached the agent transcript: %s", body)
		}
		if strings.Contains(body, "error") {
			sawError = true
		}
	}
	if !sawError {
		t.Fatal("the leaking tool did not surface a rejection to the agent")
	}
	page, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{Kinds: []string{secretToolAuthority + "/snote"}}, First: 5,
	})
	if err != nil {
		t.Fatalf("list snotes: %v", err)
	}
	if len(page.Records) != 0 {
		t.Fatalf("a secret-bearing effect persisted through the agent tool: %d", len(page.Records))
	}
}

func fmtContent(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
