package engine

// Offered functions (record 0198): a bundle's function offers itself to
// agents, and an agent's `toolsFrom` takes the offered functions of the
// enabled bundles it matches, after the tools it names.

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	offersPackage = "offers.test.dev/offers"
	otherPackage  = "other.test.dev/other"
	askerPackage  = "asker.test.dev/asker"
)

func provisionOfferedTools(t *testing.T) (*dataset, *fakeLLM) {
	t.Helper()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeProvider, ID: "offerllm",
		Properties: map[string]any{"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-offerllm"},
	}); err != nil {
		t.Fatalf("put llm/provider row: %v", err)
	}
	offered := func(pkg, name string, offer, extra map[string]any) map[string]any {
		data := map[string]any{
			"description": name + " under test",
			"runtime":     vocabulary.RuntimePython,
			"effect":      vocabulary.FunctionEffectRead,
			"source": `
def main(input, host):
    return {"output": {"said": "` + name + `"}}
`,
		}
		if offer != nil {
			data["offer"] = offer
		}
		for k, v := range extra {
			data[k] = v
		}
		return vocabulary.FunctionManifest(pkg, name, data)
	}
	agents := map[string]any{"agents": true}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(offersPackage, 0),
		vocabulary.BundleManifest(offersPackage, map[string]any{
			"description": "a bundle offering functions",
			"installs": []any{
				offersPackage + "/note", offersPackage + "/today", offersPackage + "/agenda",
				offersPackage + "/clash", offersPackage + "/hidden", offersPackage + "/stamp",
			},
		}),
		vocabulary.KindManifest(offersPackage, map[string]any{"singular": "note"},
			map[string]any{"properties": map[string]any{"text": map[string]any{"type": "string"}}}),
		offered(offersPackage, "today", agents, map[string]any{
			"arguments": []any{map[string]any{"name": "day", "type": "string", "description": "the day"}},
		}),
		offered(offersPackage, "agenda", agents, nil),
		offered(offersPackage, "clash", map[string]any{"agents": true, "name": "pinned"}, nil),
		offered(offersPackage, "hidden", nil, nil),
		offered(offersPackage, "stamp", agents, map[string]any{
			"effect":      vocabulary.FunctionEffectWrite,
			"permissions": map[string]any{"writes": []any{offersPackage + "/note"}},
			"source": `
def main(input, host):
    return {"effects": [{"action": "put", "kind": "` + offersPackage + `/note", "id": "stamped",
                         "properties": {"text": "stamped"}}],
            "output": {"ok": True}}
`,
		}),
	}); err != nil {
		t.Fatalf("install the offering bundle: %v", err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(otherPackage, 0),
		vocabulary.BundleManifest(otherPackage, map[string]any{
			"description": "a second bundle offering one function",
			"installs":    []any{otherPackage + "/ping"},
		}),
		offered(otherPackage, "ping", agents, nil),
	}); err != nil {
		t.Fatalf("install the second bundle: %v", err)
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(askerPackage, 0),
		vocabulary.AgentManifest(askerPackage, "asst", map[string]any{
			"description": "takes offered functions", "prompt": "You assist.",
			"provider": "offerllm", "model": "offered",
			"tools": []any{
				map[string]any{"function": offersPackage + "/agenda", "name": "pinned"},
			},
			"toolsFrom": []any{map[string]any{"functions": "*"}},
		}),
	}); err != nil {
		t.Fatalf("install the agent: %v", err)
	}
	return ds, fake
}

// toolNamesSent reads the tool names of one completion request, in order.
func toolNamesSent(t *testing.T, req map[string]any) []string {
	t.Helper()
	var names []string
	tools, _ := req["tools"].([]any)
	for _, tv := range tools {
		tool, _ := tv.(map[string]any)
		fn, _ := tool["function"].(map[string]any)
		names = append(names, fmt.Sprint(fn["name"]))
	}
	return names
}

func TestAgentTakesOfferedFunctionsOfEnabledBundles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := provisionOfferedTools(t)
	const asst = askerPackage + "/asst"

	fake.script("offered",
		fakeTurn{calls: []fakeCall{{"offers_today", toolArgs(t, map[string]any{"day": "monday"})}}},
		fakeTurn{calls: []fakeCall{{"offers_stamp", "{}"}}},
		fakeTurn{content: "done"},
	)
	res, err := ds.CallAgent(ctx, asst, "what's on today")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.Status != threadOK {
		t.Fatalf("settled %q (%s)", res.Status, res.Reason)
	}
	reqs := fake.requestsOf("offered")
	if len(reqs) != 3 {
		t.Fatalf("completions: %d, want 3", len(reqs))
	}
	// The named tool first; then every offered function in identity order,
	// less the one already named (agenda), the one whose name a named tool
	// holds (clash) and the one not offered (hidden).
	want := []string{"pinned", "offers_stamp", "offers_today", "other_ping"}
	for i, req := range reqs {
		if got := toolNamesSent(t, req); !reflect.DeepEqual(got, want) {
			t.Fatalf("completion %d tools = %v, want %v", i, got, want)
		}
	}
	// The card is the function's own: its description and arguments.
	tools, _ := reqs[0]["tools"].([]any)
	today, _ := tools[2].(map[string]any)["function"].(map[string]any)
	if today["description"] != "today under test" {
		t.Fatalf("offers_today card = %v", today)
	}
	if props, _ := today["parameters"].(map[string]any)["properties"].(map[string]any); props["day"] == nil {
		t.Fatalf("offers_today parameters carry no day: %v", today["parameters"])
	}

	// An offered tool dispatches like a named one: the call runs, and an
	// effect outside the agent's write allowlist is refused, nothing applied.
	if got := fmt.Sprint(toolRowOf(t, ds, res.Thread, "offers_today")["content"]); !strings.Contains(got, `"said":"today"`) {
		t.Fatalf("offers_today result = %s", got)
	}
	if got := fmt.Sprint(toolRowOf(t, ds, res.Thread, "offers_stamp")["content"]); !strings.Contains(got, "effective emit allowlist") {
		t.Fatalf("offers_stamp result = %s, want the emit refusal", got)
	}
	if _, err := ds.Get(ctx, offersPackage+"/note", "stamped"); err == nil {
		t.Fatal("an offered function's effect landed outside the agent's write allowlist")
	}

	// A disabled bundle's offered functions are gone from the next run.
	if err := ds.DisableBundle(ctx, otherPackage); err != nil {
		t.Fatalf("disable: %v", err)
	}
	fake.script("offered", fakeTurn{content: "again"})
	if _, err := ds.CallAgent(ctx, asst, "and now"); err != nil {
		t.Fatalf("second call: %v", err)
	}
	reqs = fake.requestsOf("offered")
	if got, want := toolNamesSent(t, reqs[len(reqs)-1]), []string{"pinned", "offers_stamp", "offers_today"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tools after the disable = %v, want %v", got, want)
	}
}
