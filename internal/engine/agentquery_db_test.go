package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const shopPackage = "catalogue.agent.example/shop"

// provisionShop declares a primary product, a supporting review pointing at
// it, an internal note, two records, a provider row on the fake, and the
// agents under test: `clerk` reads the whole package, `buyer` reads products
// alone and delegates to `clerk`.
func provisionShop(t *testing.T) (*dataset, *fakeLLM) {
	t.Helper()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	product, note := shopPackage+"/product", shopPackage+"/note"
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeProvider, ID: "cataloguellm",
		Properties: map[string]any{"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-cataloguellm"},
	}); err != nil {
		t.Fatalf("put the provider row: %v", err)
	}
	query := []any{map[string]any{"function": vocabulary.HostFunctionQuery}}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(shopPackage, 1),
		vocabulary.KindManifest(shopPackage, map[string]any{"singular": "product"}, map[string]any{
			"description":     "A thing the shop sells. Synced nightly.",
			"properties":      map[string]any{"name": map[string]any{"type": "string"}},
			"displayTemplate": "{name}",
		}),
		vocabulary.KindManifest(shopPackage, map[string]any{"singular": "review"}, map[string]any{
			"purpose":     "supporting",
			"description": "What a buyer said about a product.",
			"properties": map[string]any{
				"text":    map[string]any{"type": "string"},
				"product": map[string]any{"type": "reference", "kind": product},
			},
			"displayTemplate": "{text}",
		}),
		vocabulary.KindManifest(shopPackage, map[string]any{"singular": "note"}, map[string]any{
			"purpose":         "internal",
			"properties":      map[string]any{"text": map[string]any{"type": "string"}},
			"displayTemplate": "{text}",
		}),
		vocabulary.AgentManifest(shopPackage, "clerk", map[string]any{
			"description": "answers about the shop",
			"prompt":      "You are the clerk.\n",
			"provider":    "cataloguellm", "model": "cclerk", "tools": query,
			"permissions": map[string]any{"reads": map[string]any{"kinds": []any{shopPackage + "/*"}}},
		}),
		vocabulary.AgentManifest(shopPackage, "buyer", map[string]any{
			"description": "buys from the shop",
			"prompt":      "You are the buyer.",
			"provider":    "cataloguellm", "model": "cbuyer", "tools": query,
			"subagents":   []any{shopPackage + "/clerk"},
			"permissions": map[string]any{"reads": map[string]any{"kinds": []any{product}}},
		}),
	}); err != nil {
		t.Fatalf("install the shop: %v", err)
	}
	for _, in := range []substrate.PutInput{
		{Kind: product, ID: "cup", Properties: map[string]any{"name": "Tajimi cup"}},
		{Kind: note, ID: "said", Properties: map[string]any{"text": "the user asked about the tajimi cup"}},
	} {
		if _, err := ds.Put(ctx, substrate.ActorAPI, in); err != nil {
			t.Fatal(err)
		}
	}
	return ds, fake
}

// An agent holding the query tool reads the kinds it may search in its system
// prompt, after its own words, and a text search that names no kind is
// refused with a pointer back to that list (record 0172), filter.purposes
// alone included; naming the kind searches it, and a plain list still lists
// the grant.
func TestAgentQueryListsKindsAndRequiresThemForQ(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := provisionShop(t)
	product := shopPackage + "/product"
	fake.script("cclerk",
		fakeTurn{calls: []fakeCall{{"query", `{"q": "tajimi", "mode": "lexical"}`}}},
		fakeTurn{calls: []fakeCall{{"query", `{"q": "tajimi", "mode": "lexical", "filter": {"purposes": ["primary"]}}`}}},
		fakeTurn{calls: []fakeCall{{"query", `{"filter": {"search": "tajimi"}}`}}},
		fakeTurn{calls: []fakeCall{{"query", `{"q": "tajimi", "mode": "lexical", "kind": "` + product + `"}`}}},
		fakeTurn{calls: []fakeCall{{"query", `{"first": 5}`}}},
		fakeTurn{content: "done"},
	)
	res, err := ds.CallAgent(ctx, shopPackage+"/clerk", "where is the cup?")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.Status != threadOK {
		t.Fatalf("run: %+v", res)
	}

	reqs := fake.requestsOf("cclerk")
	system := systemOf(t, reqs[0])
	if !strings.HasPrefix(system, "You are the clerk.\n\n## Kinds you can read") {
		t.Fatalf("the system message does not open with the prompt, then the kinds list: %q", system)
	}
	for _, want := range []string{
		"\n" + shopPackage + "\n- " + product + ": A thing the shop sells.\n",
		"- " + shopPackage + "/review (supporting): What a buyer said about a product. References: product -> " + product + ".",
	} {
		if !strings.Contains(system, want) {
			t.Errorf("the kinds list lacks %q:\n%s", want, system)
		}
	}
	if strings.Contains(system, shopPackage+"/note") || strings.Contains(system, "Synced nightly") {
		t.Errorf("the kinds list carries the internal kind or a second sentence:\n%s", system)
	}
	for i, r := range reqs[1:] {
		if systemOf(t, r) != system {
			t.Errorf("turn %d's system prompt differs from the first turn's", i+2)
		}
	}

	rows := toolRowsOf(t, ds, res.Thread)
	if len(rows) != 5 {
		t.Fatalf("want five tool rows, got %d", len(rows))
	}
	for i, arm := range []string{"q", "q", "filter.search"} {
		content, _ := rows[i]["content"].(string)
		if rows[i]["ok"] != false || !strings.Contains(content, arm+" needs filter.kinds (or kind)") ||
			!strings.Contains(content, "filter.purposes alone is not enough") {
			t.Errorf("call %d naming no kind: ok %v content %s", i+1, rows[i]["ok"], content)
		}
	}
	for i := 3; i < 5; i++ {
		content, _ := rows[i]["content"].(string)
		if rows[i]["ok"] != true || !strings.Contains(content, `"id":"cup"`) {
			t.Errorf("call %d: ok %v content %s", i+1, rows[i]["ok"], content)
		}
	}

	// A second run renders the same bytes while nothing it reads changed.
	fake.script("cclerk", fakeTurn{content: "again"})
	if _, err := ds.CallAgent(ctx, shopPackage+"/clerk", "and now?"); err != nil {
		t.Fatalf("second call: %v", err)
	}
	again := fake.requestsOf("cclerk")
	if got := systemOf(t, again[len(again)-1]); got != system {
		t.Fatalf("a second run's system prompt differs:\n%s\n\nfirst:\n%s", got, system)
	}
}

// A sub-agent's kinds list is its own grant's, not its caller's.
func TestSubagentGetsItsOwnKindsList(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := provisionShop(t)
	fake.script("cbuyer",
		fakeTurn{calls: []fakeCall{{"clerk", `{"input": "any reviews?"}`}}},
		fakeTurn{content: "done"},
	)
	fake.script("cclerk", fakeTurn{content: "none yet"})
	if _, err := ds.CallAgent(ctx, shopPackage+"/buyer", "go"); err != nil {
		t.Fatalf("call: %v", err)
	}
	buyer := systemOf(t, fake.requestsOf("cbuyer")[0])
	clerk := systemOf(t, fake.requestsOf("cclerk")[0])
	if !strings.Contains(buyer, shopPackage+"/product") || strings.Contains(buyer, shopPackage+"/review") {
		t.Fatalf("the buyer's list is not its grant:\n%s", buyer)
	}
	if !strings.HasPrefix(clerk, "You are the clerk.") || !strings.Contains(clerk, shopPackage+"/review") {
		t.Fatalf("the sub-agent's list is not its own grant:\n%s", clerk)
	}
}

// The direct call of the query host function is not an agent's tool: a
// search naming nothing still skips internal kinds (commit 96be6499), naming
// the kind or the purpose reaches them, and `kind` is not a shorthand for q
// there.
func TestDirectQueryCallKeepsItsSearchDefaults(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _ := provisionShop(t)
	note := shopPackage + "/note"
	search := func(extra map[string]any) string {
		t.Helper()
		args := map[string]any{"q": "tajimi", "mode": "lexical"}
		for k, v := range extra {
			args[k] = v
		}
		out, _, err := ds.CallFunction(ctx, substrate.ActorAPI, vocabulary.HostFunctionQuery, args)
		if err != nil {
			t.Fatalf("search %v: %v", extra, err)
		}
		buf, _ := json.Marshal(out)
		return string(buf)
	}
	for name, tc := range map[string]struct {
		extra    map[string]any
		wantNote bool
	}{
		"nothing":     {nil, false},
		"kind arg":    {map[string]any{"kind": note}, false},
		"the kind":    {map[string]any{"filter": map[string]any{"kinds": []any{note}}}, true},
		"the purpose": {map[string]any{"filter": map[string]any{"purposes": []any{"internal"}}}, true},
	} {
		out := search(tc.extra)
		if strings.Contains(out, `"id":"said"`) != tc.wantNote {
			t.Errorf("a direct search naming %s: want the note %v, got %s", name, tc.wantNote, out)
		}
		if !tc.wantNote && !strings.Contains(out, `"id":"cup"`) {
			t.Errorf("a direct search naming %s lost the product: %s", name, out)
		}
	}
}
