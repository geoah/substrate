package engine

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// shelfPackage holds the clerk, an agent with query and write over one kind
// and no function body, so these tests run where no body can be confined.
const shelfPackage = "shelf.test.dev/shelf"

// openShelfDataset declares the shelf and points the clerk's provider row at
// a fresh fake model server, which answers as the model "clerk".
func openShelfDataset(t *testing.T) (*dataset, *fakeLLM) {
	t.Helper()
	ctx := context.Background()
	ds := openInternalDataset(t)
	fake := newFakeLLM(t)
	ds.mu.Lock()
	ds.skipThreadTitles = true
	ds.mu.Unlock()
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeProvider, ID: "clerkllm",
		Properties: map[string]any{
			"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-clerkllm",
			"pricing": []any{map[string]any{"model": "clerk", "inputPer1M": "1", "outputPer1M": "5"}},
		},
	}); err != nil {
		t.Fatalf("put the provider row: %v", err)
	}
	widget := shelfPackage + "/widget"
	docs := []map[string]any{
		vocabulary.PackageManifest(shelfPackage, 0),
		vocabulary.KindManifest(shelfPackage, map[string]any{"singular": "widget"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		vocabulary.AgentManifest(shelfPackage, "clerk", map[string]any{
			"description": "clerk under test", "prompt": "You are clerk.",
			"provider": "clerkllm", "model": "clerk",
			"tools": []any{
				map[string]any{"function": vocabulary.HostFunctionQuery},
				map[string]any{"function": vocabulary.HostFunctionWrite},
			},
			"permissions": map[string]any{
				"reads":  map[string]any{"kinds": []any{widget}},
				"writes": []any{widget},
			},
		}),
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, docs); err != nil {
		t.Fatalf("declare the shelf: %v", err)
	}
	return ds, fake
}

// toolRowsByTurn is toolRows in turn order: the calls of one response run
// side by side, so their rows land in any order.
func toolRowsByTurn(t *testing.T, ds *dataset, threadID string) []map[string]any {
	t.Helper()
	tools := toolRows(t, ds, threadID)
	sort.SliceStable(tools, func(i, j int) bool {
		a, _ := tools[i]["turn"].(float64)
		b, _ := tools[j]["turn"].(float64)
		return a < b
	})
	return tools
}

// A MODEL THAT RESENT A REFUSED READ IS NOT READ FOR AGAIN. An assistant asked
// the aggregate read to group by kind, which the read no longer takes, and
// sent the identical call again on the next turn: two failures, both shown
// to the owner. The refusal now says the call is invalid, the repeat is
// answered from it without reading, and a write in between lets the same
// call read again, since what a read refuses can depend on what is written.
func TestAgentDoesNotRunARefusedReadAgain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openShelfDataset(t)
	widget := shelfPackage + "/widget"
	grouped := toolArgs(t, map[string]any{
		"kind": widget,
		"aggregate": map[string]any{
			"property": map[string]any{"name": "name"},
			"range":    map[string]any{"gte": "2026-01-01T00:00:00Z", "lt": "2026-02-01T00:00:00Z"},
			"reducers": []any{"sum"},
			"groupBy":  []any{"kind"},
		},
	})
	fake.script("clerk",
		fakeTurn{calls: []fakeCall{{"query", grouped}}},
		fakeTurn{calls: []fakeCall{{"query", grouped}}},
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", widget, "", map[string]any{"name": "bolt"})}}},
		fakeTurn{calls: []fakeCall{{"query", grouped}}},
		fakeTurn{content: "grouped by nothing"},
	)
	res, err := ds.CallAgent(ctx, shelfPackage+"/clerk", "how many of each do I have")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.Status != threadOK {
		t.Fatalf("result: %+v", res)
	}
	tools := toolRowsByTurn(t, ds, res.Thread)
	if len(tools) != 4 {
		t.Fatalf("%d tool rows, want 4: %v", len(tools), tools)
	}
	refusal := func(i int) map[string]any {
		t.Helper()
		if tools[i]["ok"] != false {
			t.Fatalf("tool row %d answered ok: %v", i, tools[i]["content"])
		}
		s, _ := tools[i]["content"].(string)
		return toolResult(t, s)
	}
	first := refusal(0)
	if msg, _ := first["error"].(string); !strings.Contains(msg, "groupBy kind was removed") || first["invalid"] != true || first["note"] != invalidCallNote {
		t.Fatalf("the refusal does not say the call is invalid: %v", first)
	}
	if first["repeated"] != nil {
		t.Fatalf("the first refusal reads as a repeat: %v", first)
	}
	repeat := refusal(1)
	if repeat["repeated"] != true || repeat["error"] != first["error"] || repeat["note"] != repeatedCallNote {
		t.Fatalf("the repeat was not answered from the refusal: %v", repeat)
	}
	if tools[2]["ok"] != true {
		t.Fatalf("the write failed: %v", tools[2]["content"])
	}
	if after := refusal(3); after["repeated"] != nil || after["invalid"] != true {
		t.Fatalf("after a write the call did not read again: %v", after)
	}
}

// count answers the size of a list's filtered set beside its page, so an
// agent counts a kind's records with first 1 rather than an aggregate; a
// ranked read and an aggregate read refuse it, and the card says how to
// count and which groups an aggregate takes.
func TestAgentQueryCountsAList(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openShelfDataset(t)
	widget := shelfPackage + "/widget"
	for _, name := range []string{"bolt", "nut", "washer"} {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: widget, ID: fid("w-" + name), Properties: map[string]any{"name": name},
		}); err != nil {
			t.Fatal(err)
		}
	}
	fake.script("clerk",
		fakeTurn{calls: []fakeCall{
			{"query", toolArgs(t, map[string]any{"kind": widget, "first": 1, "count": true})},
			{"query", toolArgs(t, map[string]any{"kind": widget, "q": "bolt", "count": true})},
			{"query", toolArgs(t, map[string]any{"kind": widget, "count": true, "aggregate": map[string]any{"property": map[string]any{"name": "name"}}})},
			{"query", toolArgs(t, map[string]any{"kind": widget, "first": 1, "count": false})},
		}},
		fakeTurn{content: "three widgets"},
	)
	res, err := ds.CallAgent(ctx, shelfPackage+"/clerk", "how many widgets")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	tools := toolRowsByTurn(t, ds, res.Thread)
	if len(tools) != 4 {
		t.Fatalf("%d tool rows, want 4", len(tools))
	}
	s, _ := tools[0]["content"].(string)
	page := toolResult(t, s)
	if records, _ := page["records"].([]any); tools[0]["ok"] != true || page["count"] != float64(3) || len(records) != 1 {
		t.Fatalf("a counted list of one: %s", s)
	}
	for i, want := range map[int]string{1: "count is not supported with q", 2: "count is refused beside aggregate"} {
		s, _ := tools[i]["content"].(string)
		if tools[i]["ok"] != false || !strings.Contains(s, want) {
			t.Errorf("tool row %d: %s, want a refusal naming %q", i, s, want)
		}
	}
	s, _ = tools[3]["content"].(string)
	if page := toolResult(t, s); tools[3]["ok"] != true || page["count"] != nil {
		t.Fatalf("count false still counted: %s", s)
	}

	reqs := fake.requestsOf("clerk")
	card := ""
	for _, raw := range reqs[0]["tools"].([]any) {
		if fn, _ := raw.(map[string]any)["function"].(map[string]any); fn["name"] == "query" {
			card, _ = fn["description"].(string)
			params, _ := fn["parameters"].(map[string]any)
			props, _ := params["properties"].(map[string]any)
			if _, ok := props["count"]; !ok {
				t.Errorf("the query card takes no count: %v", props)
			}
		}
	}
	for _, want := range []string{
		"groups by source, about, code, metric, channel, statistic, never by kind",
		"list each kind with first 1 and count true",
	} {
		if !strings.Contains(card, want) {
			t.Errorf("the query card lacks %q:\n%s", want, card)
		}
	}

	// The card is enforced on a direct call, so the declaration carries it.
	out, _, err := ds.CallFunction(ctx, substrate.ActorAPI, vocabulary.HostFunctionQuery, map[string]any{"kind": widget, "first": 1, "count": true})
	if err != nil {
		t.Fatalf("a direct counted list: %v", err)
	}
	if direct, _ := out.(map[string]any); direct["count"] != float64(3) {
		t.Fatalf("a direct counted list: %v", out)
	}
}
