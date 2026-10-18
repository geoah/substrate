package engine

// One tool call is bounded on its own, inside the run: a `query` or function
// call past the agent's toolTimeoutSeconds answers the model a tool error and
// the run goes on, an agent that sets none gives a function call the
// function's own timeout, and a result past maxToolResultBytes is replaced by a
// truncation envelope in what the model reads, what the thread stores and
// what the stream carries. The `query` card states the limits in force.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const boundsPackage = "bounds.test.dev/bounds"

// provisionBoundsAgents installs a slow function, a function whose output is
// large, one that takes 2 s of its declared 3 s, one that never ends inside
// its declared 1 s, and two agents on one provider row: `tight`, with a 1 s
// tool timeout and a 4 KiB result cap, and `roomy`, on the defaults.
func provisionBoundsAgents(t *testing.T) (*dataset, *fakeLLM) {
	t.Helper()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeProvider, ID: "boundsllm",
		Properties: map[string]any{
			"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-boundsllm",
			"pricing": []any{
				map[string]any{"model": "btight", "inputPer1M": "1", "outputPer1M": "5"},
				map[string]any{"model": "broomy", "inputPer1M": "1", "outputPer1M": "5"},
			},
		},
	}); err != nil {
		t.Fatalf("put llm/provider row boundsllm: %v", err)
	}
	agent := func(name string, data map[string]any) map[string]any {
		data["description"] = name + " under test"
		data["prompt"] = "You are " + name + "."
		return vocabulary.AgentManifest(boundsPackage, name, data)
	}
	tools := []any{
		map[string]any{"function": boundsPackage + "/sleeper"},
		map[string]any{"function": boundsPackage + "/bulky"},
		map[string]any{"function": boundsPackage + "/napper"},
		map[string]any{"function": boundsPackage + "/spinner"},
		map[string]any{"function": vocabulary.HostFunctionQuery},
	}
	reads := map[string]any{"reads": map[string]any{"kinds": []any{boundsPackage + "/note"}}}
	docs := []map[string]any{
		vocabulary.PackageManifest(boundsPackage, 0),
		vocabulary.KindManifest(boundsPackage, map[string]any{"singular": "note"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		vocabulary.FunctionManifest(boundsPackage, "sleeper", map[string]any{
			"description": "takes longer than any tool timeout under test",
			"runtime":     vocabulary.RuntimePython,
			"timeout":     "PT30S",
			"source": `
def main(input, host):
    n = 0
    while True:
        n += 1
`,
		}),
		vocabulary.FunctionManifest(boundsPackage, "bulky", map[string]any{
			"description": "returns far more than a tool result may carry",
			"runtime":     vocabulary.RuntimePython,
			"source": `
def main(input, host):
    return {"output": {"rows": ["résumé ✈ row %d" % i for i in range(20000)]}}
`,
		}),
		vocabulary.FunctionManifest(boundsPackage, "napper", map[string]any{
			"description": "takes two of the three seconds it declares",
			"runtime":     vocabulary.RuntimePython,
			"timeout":     "PT3S",
			"source": `
import time

def main(input, host):
    time.sleep(2)
    return {"output": {"slept": 2}}
`,
		}),
		vocabulary.FunctionManifest(boundsPackage, "spinner", map[string]any{
			"description": "never ends inside the one second it declares",
			"runtime":     vocabulary.RuntimePython,
			"timeout":     "PT1S",
			"source": `
def main(input, host):
    while True:
        pass
`,
		}),
		agent("tight", map[string]any{
			"provider": "boundsllm", "model": "btight", "tools": tools, "permissions": reads,
			"budgets": map[string]any{"toolTimeoutSeconds": 1, "maxToolResultBytes": 4096},
		}),
		agent("roomy", map[string]any{
			"provider": "boundsllm", "model": "broomy", "tools": tools, "permissions": reads,
		}),
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, docs); err != nil {
		t.Fatalf("install the bounds package: %v", err)
	}
	return ds, fake
}

func TestAgentToolCallTimesOutAndALargeResultIsCapped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := provisionBoundsAgents(t)
	fake.script("btight",
		fakeTurn{calls: []fakeCall{{"sleeper", "{}"}}},
		fakeTurn{calls: []fakeCall{{"bulky", "{}"}}},
		fakeTurn{content: "done"},
	)
	began := time.Now()
	res, err := ds.CallAgent(ctx, boundsPackage+"/tight", "go")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.Status != threadOK || res.Reply != "done" {
		t.Fatalf("a timed-out tool failed the run: %+v", res)
	}
	if took := time.Since(began); took > 20*time.Second {
		t.Fatalf("the run took %s: the 1 s tool timeout did not end the slow call", took)
	}
	rows := toolRowsOf(t, ds, res.Thread)
	slow := toolRowNamed(t, rows, "sleeper")
	if slow["ok"] != false || slow["content"] != toolError("tool call timed out after 1s — narrow the call (fewer words, a filter, a smaller window) or answer from what you have") {
		t.Fatalf("the slow call's row = ok %v content %v", slow["ok"], slow["content"])
	}
	big := toolRowNamed(t, rows, "bulky")
	content, _ := big["content"].(string)
	if big["ok"] != true || len(content) > 4096 {
		t.Fatalf("the bulky row is ok %v and %d bytes, want ok and at most 4096", big["ok"], len(content))
	}
	var env struct {
		Truncated bool   `json:"truncated"`
		Bytes     int    `json:"bytes"`
		Limit     int    `json:"limit"`
		Head      string `json:"head"`
		Note      string `json:"note"`
	}
	if err := json.Unmarshal([]byte(content), &env); err != nil {
		t.Fatalf("the stored envelope is not JSON: %v: %s", err, content)
	}
	if !env.Truncated || env.Bytes <= 4096 || env.Limit != 4096 || !strings.HasPrefix(env.Head, `{"effects":0,"output":{"rows":["résumé ✈ row 0"`) ||
		env.Note != "result exceeded the 4 KiB tool-result cap; ask for fewer rows, a filter or a smaller window" {
		t.Fatalf("envelope = %+v", env)
	}
	// What the model read on the next turn is the envelope, not the result.
	reqs := fake.requestsOf("btight")
	last, _ := json.Marshal(reqs[len(reqs)-1]["messages"])
	if len(last) > 64<<10 || !strings.Contains(string(last), `\"truncated\":true`) {
		t.Fatalf("the model's next request carried %d bytes of history: %.300s", len(last), last)
	}
}

// An agent that sets no toolTimeoutSeconds gives a function call the
// function's declared timeout, not the `query` default: napper declares 3 s
// and takes 2 s, past a `query` default lowered to 1 s, and answers. A call
// past its declared timeout answers the runner's error naming that timeout,
// which the tool's bound sits behind.
func TestAgentFunctionCallTakesTheFunctionsOwnTimeout(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := provisionBoundsAgents(t)
	ds.svc.queryToolTimeout = time.Second
	fake.script("broomy",
		fakeTurn{calls: []fakeCall{{"napper", "{}"}}},
		fakeTurn{calls: []fakeCall{{"spinner", "{}"}}},
		fakeTurn{content: "done"},
	)
	res, err := ds.CallAgent(ctx, boundsPackage+"/roomy", "go")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.Status != threadOK || res.Reply != "done" {
		t.Fatalf("run = %+v", res)
	}
	rows := toolRowsOf(t, ds, res.Thread)
	napper := toolRowNamed(t, rows, "napper")
	if napper["ok"] != true || !strings.Contains(napper["content"].(string), `"slept":2`) {
		t.Fatalf("napper was cut short: ok %v content %v", napper["ok"], napper["content"])
	}
	spinner := toolRowNamed(t, rows, "spinner")
	content, _ := spinner["content"].(string)
	if spinner["ok"] != false || !strings.Contains(content, "invocation exceeded 1s") || strings.Contains(content, "tool call timed out") {
		t.Fatalf("spinner's row = ok %v content %v, want the runner's own timeout error", spinner["ok"], content)
	}
}

// The `query` card ends with the limits in force for the agent, overrides
// included, and says whether the repository can rank by meaning.
func TestAgentQueryCardCarriesTheLimitsInForce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := provisionBoundsAgents(t)
	card := func(model, agent string) string {
		t.Helper()
		fake.script(model, fakeTurn{content: "ok"})
		if _, err := ds.CallAgent(ctx, boundsPackage+"/"+agent, "go"); err != nil {
			t.Fatalf("call %s: %v", agent, err)
		}
		reqs := fake.requestsOf(model)
		tools, _ := reqs[len(reqs)-1]["tools"].([]any)
		for _, raw := range tools {
			fn, _ := raw.(map[string]any)["function"].(map[string]any)
			if fn["name"] == "query" {
				d, _ := fn["description"].(string)
				return d
			}
		}
		t.Fatalf("%s's request carries no query tool: %v", agent, tools)
		return ""
	}
	tight := card("btight", "tight")
	for _, want := range []string{
		"Read records.",
		"never a list of common words",
		"This repository has no embeddings provider, so q matches words only: semantic and hybrid fall back to lexical.",
		"a search stops at 5s or at the newest 2000 matching records",
		"a call stops after 1s",
		"a result over 4 KiB is replaced by",
		"narrow once",
	} {
		if !strings.Contains(tight, want) {
			t.Errorf("tight's query card lacks %q:\n%s", want, tight)
		}
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeProvider, ID: "boundsllm", Properties: map[string]any{"embedModel": "text-embedding-ada-002"},
	}); err != nil {
		t.Fatalf("declare the embeddings model: %v", err)
	}
	roomy := card("broomy", "roomy")
	for _, want := range []string{
		"This repository has an embeddings provider, so hybrid (the default) also ranks by meaning.",
		"a call stops after 30s",
		"a result over 48 KiB is replaced by",
	} {
		if !strings.Contains(roomy, want) {
			t.Errorf("roomy's query card lacks %q:\n%s", want, roomy)
		}
	}
}

// A search whose relaxed top-up runs out of the budget answers what matched
// every word, marked capped, instead of failing.
func TestLexicalSearchDegradesWhenTheTopUpRunsOutOfBudget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const budget = 2 * time.Second
	ds := openInternalDataset(t, WithSearchBudget(budget))
	importVocabulary(t, ds, "tasks")
	for id, name := range map[string]string{"both": "flight booking", "flight": "flight to Lisbon", "booking": "booking for dinner"} {
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: "samples.substrate.reamde.dev/tasks/task", ID: id, Properties: map[string]any{"name": name},
		}); err != nil {
			t.Fatal(err)
		}
	}
	// The top-up's deadline is four fifths of the budget from the start.
	ds.svc.searchTopUpHook = func() { time.Sleep(budget * 9 / 10) }
	res, err := ds.Search(ctx, substrate.SearchInput{Q: "flight booking", Mode: substrate.SearchLexical})
	if err != nil {
		t.Fatalf("a top-up past the budget failed the search: %v", err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Record.ID != "both" {
		t.Fatalf("hits = %v, want the strict match alone", res.Hits)
	}
	if !res.Capped || !strings.HasPrefix(res.Note, "search stopped at the 2s budget, so the hits are only records matching every word.") {
		t.Fatalf("capped = %v, note = %q", res.Capped, res.Note)
	}
}
