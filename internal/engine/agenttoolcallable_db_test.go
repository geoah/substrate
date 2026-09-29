package engine

// The tool rows' callable: every `llm/message` row that names a tool (an
// assistant row's `toolCalls[]` entry and the tool row that answers it)
// carries the callable's identity in the actor spelling (record 0025) beside
// the model-facing alias, so rows written under different aliases, by
// different agents, join on one value.

import (
	"context"
	"encoding/json"
	"sort"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const aliasPackage = "alias.test.dev/alias"

// The callables the rows must name, spelled out rather than derived: the
// spelling is the contract a reader joins on.
const (
	aliasCountCallable = "function:alias.test.dev:alias:count"
	aliasRightCallable = "agent:alias.test.dev:alias:right"
	aliasQueryCallable = "function:substrate.reamde.dev:core:query"
)

// provisionAliasAgents installs one function and two agents that alias it
// differently: `left` names it `tally` and delegates to `right`, which names
// it `size` and also holds the query host function under its own name.
func provisionAliasAgents(t *testing.T) (*dataset, *fakeLLM) {
	t.Helper()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeProvider, ID: "aliasllm",
		Properties: map[string]any{
			"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-aliasllm",
			"pricing": []any{
				map[string]any{"model": "aleft", "inputPer1M": "1", "outputPer1M": "5"},
				map[string]any{"model": "aright", "inputPer1M": "1", "outputPer1M": "5"},
			},
		},
	}); err != nil {
		t.Fatalf("put llm/provider row aliasllm: %v", err)
	}
	agent := func(name string, data map[string]any) map[string]any {
		data["description"] = name + " under test"
		data["prompt"] = "You are " + name + "."
		return vocabulary.AgentManifest(aliasPackage, name, data)
	}
	docs := []map[string]any{
		vocabulary.PackageManifest(aliasPackage, 0),
		vocabulary.FunctionManifest(aliasPackage, "count", map[string]any{
			"description": "counts the characters of a word, writing nothing",
			"runtime":     vocabulary.RuntimePython,
			"arguments":   []any{map[string]any{"name": "word", "type": "string", "required": true}},
			"returns":     []any{map[string]any{"name": "length", "type": "int"}},
			"source": `
def main(input, host):
    return {"output": {"length": len(input["args"]["word"])}}
`,
		}),
		agent("left", map[string]any{
			"provider": "aliasllm", "model": "aleft",
			"tools": []any{
				map[string]any{"function": aliasPackage + "/count", "name": "tally"},
			},
			"subagents": []any{aliasPackage + "/right"},
		}),
		agent("right", map[string]any{
			"provider": "aliasllm", "model": "aright",
			"tools": []any{
				map[string]any{"function": aliasPackage + "/count", "name": "size"},
				map[string]any{"function": vocabulary.HostFunctionQuery, "name": "lookup"},
			},
			"permissions": map[string]any{
				"reads": map[string]any{"kinds": []any{crewPackage + "/widget"}},
			},
		}),
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, docs); err != nil {
		t.Fatalf("install the alias authority: %v", err)
	}
	return ds, fake
}

// runAliasChain runs `left`, which calls `tally` and then `right`; `right`
// calls `size` and `lookup` and replies. It returns left's thread and the
// child thread right ran on.
func runAliasChain(t *testing.T, ds *dataset, fake *fakeLLM) (string, string) {
	t.Helper()
	fake.script("aleft",
		fakeTurn{calls: []fakeCall{
			{"tally", toolArgs(t, map[string]any{"word": "widget"})},
			{"right", toolArgs(t, map[string]any{"input": "measure gadget"})},
		}},
		fakeTurn{content: "done"},
	)
	fake.script("aright",
		fakeTurn{calls: []fakeCall{
			{"size", toolArgs(t, map[string]any{"word": "gadget"})},
			{"lookup", toolArgs(t, map[string]any{"kind": crewPackage + "/widget"})},
		}},
		fakeTurn{content: "six"},
	)
	res, err := ds.CallAgent(context.Background(), aliasPackage+"/left", "measure things")
	if err != nil {
		t.Fatalf("call left: %v", err)
	}
	if res.Status != threadOK {
		t.Fatalf("left settled %q (%s)", res.Status, res.Reason)
	}
	var child string
	for _, m := range threadMessages(t, ds, res.Thread) {
		if m["role"] == "tool" && m["name"] == "right" {
			var payload struct {
				Thread string `json:"thread"`
			}
			content, _ := m["content"].(string)
			if err := json.Unmarshal([]byte(content), &payload); err != nil {
				t.Fatalf("the sub-agent result is not JSON: %s", content)
			}
			child = payload.Thread
		}
	}
	if child == "" {
		t.Fatal("left's thread carries no sub-agent result naming right's thread")
	}
	return res.Thread, child
}

// toolRowOf returns the one tool row on a thread that answers the named tool.
func toolRowOf(t *testing.T, ds *dataset, thread, name string) map[string]any {
	t.Helper()
	var found map[string]any
	for _, m := range threadMessages(t, ds, thread) {
		if m["role"] == "tool" && m["name"] == name {
			if found != nil {
				t.Fatalf("thread %s carries two tool rows for %s", thread, name)
			}
			found = m
		}
	}
	if found == nil {
		t.Fatalf("thread %s carries no tool row for %s", thread, name)
	}
	return found
}

// toolCallOf returns the assistant row's `toolCalls` entry for the named tool.
func toolCallOf(t *testing.T, ds *dataset, thread, name string) map[string]any {
	t.Helper()
	for _, m := range threadMessages(t, ds, thread) {
		if m["role"] != "assistant" {
			continue
		}
		for _, c := range objectRows(m["toolCalls"]) {
			if c["name"] == name {
				return c
			}
		}
	}
	t.Fatalf("thread %s carries no assistant tool call named %s", thread, name)
	return nil
}

// ONE FUNCTION, TWO ALIASES, TWO AGENTS, ONE JOIN KEY. The rows keep the
// name each model saw, and the callable is the same string on both sides of
// both calls, so a query by the callable finds every call of the function
// whatever an agent named it.
func TestToolRowsJoinOnTheCallableAcrossAliases(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := provisionAliasAgents(t)
	left, right := runAliasChain(t, ds, fake)

	for _, c := range []struct{ thread, alias string }{{left, "tally"}, {right, "size"}} {
		row := toolRowOf(t, ds, c.thread, c.alias)
		if row["ok"] != true {
			t.Fatalf("%s failed: %v", c.alias, row["content"])
		}
		if row["callable"] != aliasCountCallable {
			t.Fatalf("the %s tool row names callable %v, want %s", c.alias, row["callable"], aliasCountCallable)
		}
		call := toolCallOf(t, ds, c.thread, c.alias)
		if call["callable"] != aliasCountCallable {
			t.Fatalf("the %s tool call names callable %v, want %s", c.alias, call["callable"], aliasCountCallable)
		}
		if call["id"] != row["toolCallId"] {
			t.Fatalf("the %s tool row answers %v, the call is %v", c.alias, row["toolCallId"], call["id"])
		}
	}

	// The join itself, in SQL: every tool row and every assistant call of the
	// function, by the callable alone, across both threads.
	names := func(query string) []string {
		t.Helper()
		rows, err := ds.db.QueryContext(ctx, query, typeMessage, aliasCountCallable)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		var out []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatal(err)
			}
			out = append(out, name)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		sort.Strings(out)
		return out
	}
	results := names(`
		SELECT props->>'name' FROM records
		WHERE kind = $1 AND deleted_at IS NULL
		  AND props->>'role' = 'tool' AND props->>'callable' = $2`)
	if len(results) != 2 || results[0] != "size" || results[1] != "tally" {
		t.Fatalf("tool rows joined on %s: %v, want [size tally]", aliasCountCallable, results)
	}
	calls := names(`
		SELECT c->>'name' FROM records, jsonb_array_elements(props->'toolCalls') c
		WHERE kind = $1 AND deleted_at IS NULL
		  AND props->>'role' = 'assistant' AND c->>'callable' = $2`)
	if len(calls) != 2 || calls[0] != "size" || calls[1] != "tally" {
		t.Fatalf("tool calls joined on %s: %v, want [size tally]", aliasCountCallable, calls)
	}

	// And through the records read a client lists with.
	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Kinds:      []string{typeMessage},
		Properties: map[string]substrate.Cond{"callable": {Eq: aliasCountCallable}},
	}})
	if err != nil {
		t.Fatalf("list by callable: %v", err)
	}
	var listed []string
	for _, r := range page.Records {
		name, _ := r.Properties["name"].(string)
		listed = append(listed, name)
	}
	sort.Strings(listed)
	if len(listed) != 2 || listed[0] != "size" || listed[1] != "tally" {
		t.Fatalf("records listed by callable %s: %v, want [size tally]", aliasCountCallable, listed)
	}
}

// A SUB-AGENT AND A HOST FUNCTION NAME THEIR OWN IDENTITY. The sub-agent's is
// its agent actor; a host function is a function record, so its identity is
// the function spelling of that record, whatever the agent aliased it to.
func TestToolRowsNameTheSubAgentAndTheHostFunction(t *testing.T) {
	t.Parallel()
	ds, fake := provisionAliasAgents(t)
	left, right := runAliasChain(t, ds, fake)

	for _, c := range []struct{ thread, alias, want string }{
		{left, "right", aliasRightCallable},
		{right, "lookup", aliasQueryCallable},
	} {
		row := toolRowOf(t, ds, c.thread, c.alias)
		if row["ok"] != true {
			t.Fatalf("%s failed: %v", c.alias, row["content"])
		}
		if row["callable"] != c.want {
			t.Fatalf("the %s tool row names callable %v, want %s", c.alias, row["callable"], c.want)
		}
		if call := toolCallOf(t, ds, c.thread, c.alias); call["callable"] != c.want {
			t.Fatalf("the %s tool call names callable %v, want %s", c.alias, call["callable"], c.want)
		}
	}
}

// A NAME THE MODEL INVENTED NAMES NO CALLABLE. The row keeps the name and
// the refusal, and carries no identity it could not resolve.
func TestToolRowsOmitTheCallableForAnUnknownTool(t *testing.T) {
	t.Parallel()
	ds, fake := provisionAliasAgents(t)
	fake.script("aright",
		fakeTurn{calls: []fakeCall{{"invented", "{}"}}},
		fakeTurn{content: "sorry"},
	)
	res, err := ds.CallAgent(context.Background(), aliasPackage+"/right", "try it")
	if err != nil {
		t.Fatalf("call right: %v", err)
	}
	row := toolRowOf(t, ds, res.Thread, "invented")
	if row["ok"] != false {
		t.Fatalf("an unknown tool dispatched: %v", row["content"])
	}
	if _, ok := row["callable"]; ok {
		t.Fatalf("the unknown tool's row names callable %v", row["callable"])
	}
	if _, ok := toolCallOf(t, ds, res.Thread, "invented")["callable"]; ok {
		t.Fatal("the unknown tool's call names a callable")
	}
}

// THE LIVE EVENTS CARRY THE SAME CALLABLE. A client renders a card from the
// stream before the rows exist, and the alias alone cannot say what ran.
func TestToolEventsCarryTheCallable(t *testing.T) {
	t.Parallel()
	ds, fake := provisionAliasAgents(t)
	fake.script("aright",
		fakeTurn{calls: []fakeCall{
			{"size", toolArgs(t, map[string]any{"word": "gadget"})},
			{"invented", "{}"},
		}},
		fakeTurn{content: "six"},
	)
	got := map[string][]string{}
	if _, err := ds.ChatAgent(context.Background(), substrate.ActorAPI, aliasPackage+"/right", "", "measure gadget", func(ev substrate.AgentEvent) {
		if ev.Kind == substrate.AgentEventToolStarted || ev.Kind == substrate.AgentEventToolFinished {
			got[ev.Tool] = append(got[ev.Tool], ev.Kind+"="+ev.Callable)
		}
	}); err != nil {
		t.Fatalf("chat right: %v", err)
	}
	want := []string{
		substrate.AgentEventToolStarted + "=" + aliasCountCallable,
		substrate.AgentEventToolFinished + "=" + aliasCountCallable,
	}
	if len(got["size"]) != 2 || got["size"][0] != want[0] || got["size"][1] != want[1] {
		t.Fatalf("size events %v, want %v", got["size"], want)
	}
	if len(got["invented"]) == 0 {
		t.Fatal("the unknown tool emitted no events")
	}
	for _, ev := range got["invented"] {
		if ev != substrate.AgentEventToolStarted+"=" && ev != substrate.AgentEventToolFinished+"=" {
			t.Fatalf("the unknown tool's event names a callable: %s", ev)
		}
	}
}
