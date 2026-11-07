package engine

// The vocabulary built-in (decision record 0239): an agent plans and applies
// a batch of declarations, held to its effective emit, the policy door and
// every guard the apply door holds a batch to. The seeded Moss and its
// architect are the agents under test, so the sample is held to what it
// promises too.

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// openMossDataset opens a repository with the LLM sample seeded and its
// `openai` row pointed at the fake. It installs nothing else: the sample
// carries no function body, so the suite runs where no sandbox can confine
// one.
func openMossDataset(t *testing.T) (*dataset, *fakeLLM, string) {
	t.Helper()
	ds := openInternalDataset(t, WithLLMSampleSeed(true))
	fake := newFakeLLM(t)
	ds.mu.Lock()
	ds.skipThreadTitles = true
	ds.mu.Unlock()
	if _, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{
		Kind: typeProvider, ID: "openai",
		Properties: map[string]any{"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-openai"},
	}); err != nil {
		t.Fatal(err)
	}
	return ds, fake, testdb.Repository(t)
}

// gardenDocs is a new package under authority and its one kind.
func gardenDocs(authority string) []any {
	return []any{
		map[string]any{
			"kind":     kindPackage,
			"metadata": map[string]any{"id": authority + "/garden"},
			"data":     map[string]any{"authority": authority, "package": "garden"},
		},
		map[string]any{
			"kind":     kindKind,
			"metadata": map[string]any{"id": authority + "/garden/plant"},
			"data": map[string]any{
				"authority": authority, "package": "garden",
				"description":     "A plant you look after.",
				"names":           map[string]any{"singular": "plant"},
				"displayTemplate": "{name}",
				"properties": map[string]any{
					"name":           map[string]any{"type": "string", "description": "what you call it"},
					"waterEveryDays": map[string]any{"type": "int", "description": "days between waterings"},
				},
			},
		},
	}
}

// toolRows lists a thread's tool rows in order.
func toolRows(t *testing.T, ds *dataset, threadID string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, m := range threadMessages(t, ds, threadID) {
		if m["role"] == "tool" {
			out = append(out, m)
		}
	}
	return out
}

// A whole session as the sample wires it: the owner asks Moss to start
// tracking plants, Moss hands the collection to its architect, which plans
// and applies the declaration, and Moss writes the first record into it.
func TestMossShapesACollectionThroughItsArchitectAndFillsIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake, repo := openMossDataset(t)
	docs := gardenDocs(repo)
	plant := repo + "/garden/plant"

	fake.script("gpt-5",
		// Moss: a collection is the architect's.
		fakeTurn{calls: []fakeCall{{"mossArchitect", toolArgs(t, map[string]any{
			"input": "The owner asked to start tracking their plants: a name and how often each wants water. The owner asked for this.",
		})}}},
		// The architect plans, then applies the same batch.
		fakeTurn{calls: []fakeCall{{"vocabulary", toolArgs(t, map[string]any{"documents": docs, "plan": true})}}},
		fakeTurn{calls: []fakeCall{{"vocabulary", toolArgs(t, map[string]any{"documents": docs})}}},
		fakeTurn{content: "Applied " + plant + "."},
		// Moss writes the first record under a minted id.
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", plant, "", map[string]any{"name": "Fern", "waterEveryDays": 7})}}},
		fakeTurn{content: "Your plants have a home, and the fern is in it."},
	)
	res, err := ds.ChatAgent(ctx, substrate.ActorAPI, repo+"/llm/moss", substrate.ChatInput{Message: "start tracking my plants; I have a fern I water weekly"}, nil)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if res.Status != threadOK {
		t.Fatalf("settled %q (%s)", res.Status, res.Reason)
	}

	reqs := fake.requestsOf("gpt-5")
	names := toolNamesSent(t, reqs[0])
	for _, name := range []string{"query", "write", "propose", "ask", "wait", "mossArchitect", "mossLibrarian"} {
		if !slices.Contains(names, name) {
			t.Fatalf("Moss's tools = %v, want %s among them", names, name)
		}
	}
	if slices.Contains(names, "vocabulary") {
		t.Fatal("Moss carries vocabulary; only its architect may write declarations")
	}
	if got := toolNamesSent(t, reqs[1]); !slices.Equal(got, []string{"query", "vocabulary"}) {
		t.Fatalf("architect's tools = %v, want [query vocabulary]", got)
	}
	if card := toolDescription(t, reqs[1], "vocabulary"); !strings.Contains(card, "The owner's authority is "+repo+".") {
		t.Fatalf("the vocabulary card does not name the owner's authority: %s", card)
	}

	if _, err := ds.resolveType(plant); err != nil {
		t.Fatalf("the architect's kind is not declared: %v", err)
	}
	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{plant}}, First: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || page.Records[0].Properties["name"] != "Fern" {
		t.Fatalf("plants = %+v, want the fern", page.Records)
	}

	rows := toolRows(t, ds, res.Thread)
	if len(rows) != 2 {
		t.Fatalf("Moss's tool rows = %d, want the architect call and the write", len(rows))
	}
	child, records, kinds, _ := writesOf(t, rows[0])
	if records != 2 || !slices.Contains(kinds, kindKind) || !slices.Contains(kinds, kindPackage) {
		t.Fatalf("architect wrote %d records of %v, want the package and the kind", records, kinds)
	}
	arch := toolRows(t, ds, child)
	if len(arch) != 2 {
		t.Fatalf("architect tool rows = %d, want plan and apply", len(arch))
	}
	if arch[0]["ok"] != true || len(changeKinds(arch[0])) != 0 {
		t.Fatalf("plan row: ok %v, changes %v; a plan writes nothing", arch[0]["ok"], arch[0]["changes"])
	}
	if content, _ := arch[0]["content"].(string); strings.Contains(content, "blockers") {
		t.Fatalf("plan of a fresh package has blockers: %s", content)
	}
	if arch[1]["ok"] != true || !slices.Contains(changeKinds(arch[1]), kindKind) {
		t.Fatalf("apply row: ok %v, changes %v; want the kind's entry stamped", arch[1]["ok"], arch[1]["changes"])
	}
}

// Every refusal is a tool result the agent reads, and nothing lands: a
// document outside its emit, a data record, a shipped package. (A package
// under another authority is refused by authorizeNewPackage for every hand
// but the substrate's; this harness lifts that rule.)
func TestAgentVocabularyRefusesOutsideItsGrant(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake, repo := openMossDataset(t)
	agentDoc := map[string]any{
		"kind":     kindAgent,
		"metadata": map[string]any{"id": repo + "/llm/sneaky"},
		"data":     map[string]any{"authority": repo, "package": "llm", "description": "x", "prompt": "x"},
	}
	shipped := map[string]any{
		"kind":     kindKind,
		"metadata": map[string]any{"id": vocabulary.KindLLMMemory},
		"data":     map[string]any{"authority": "substrate.reamde.dev", "package": "llm", "description": "x"},
	}
	cases := []struct {
		docs []any
		want string
	}{
		{[]any{agentDoc}, "not in agent"},
		{[]any{map[string]any{"kind": repo + "/llm/scratchpad", "metadata": map[string]any{"id": "x"}, "data": map[string]any{}}}, "not a declaration kind"},
		{[]any{shipped}, "shipped package"},
	}
	turns := make([]fakeTurn, 0, len(cases)+1)
	for _, c := range cases {
		turns = append(turns, fakeTurn{calls: []fakeCall{{"vocabulary", toolArgs(t, map[string]any{"documents": c.docs})}}})
	}
	fake.script("gpt-5", append(turns, fakeTurn{content: "refused, stopping"})...)
	res, err := ds.CallAgent(ctx, repo+"/llm/mossArchitect", "try them all")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	rows := toolRows(t, ds, res.Thread)
	if len(rows) != len(cases) {
		t.Fatalf("tool rows = %d, want %d", len(rows), len(cases))
	}
	for i, c := range cases {
		content, _ := rows[i]["content"].(string)
		if rows[i]["ok"] != false || !strings.Contains(content, c.want) {
			t.Fatalf("case %d: ok %v, %s; want a refusal naming %q", i, rows[i]["ok"], content, c.want)
		}
	}
	if _, err := ds.registry().ResolveAgent(repo + "/llm/sneaky"); err == nil {
		t.Fatal("a refused agent declaration landed")
	}
}

// The owner's gate speaks for declarations as for records: a gated apply is
// refused whole, and the plan, which writes nothing, still answers.
func TestAgentVocabularyApplyMeetsTheOwnersPolicy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake, repo := openMossDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: vocabulary.KindRecordPatchPolicy, ID: fid("gate-kinds"),
		Properties: map[string]any{
			"selector": map[string]any{"kinds": []any{kindKind}, "agents": []any{repo + "/llm/mossArchitect"}},
			"action":   "gate",
		},
	}); err != nil {
		t.Fatalf("put the policy: %v", err)
	}
	docs := gardenDocs(repo)
	fake.script("gpt-5",
		fakeTurn{calls: []fakeCall{{"vocabulary", toolArgs(t, map[string]any{"documents": docs, "plan": true})}}},
		fakeTurn{calls: []fakeCall{{"vocabulary", toolArgs(t, map[string]any{"documents": docs})}}},
		fakeTurn{content: "gated"},
	)
	res, err := ds.CallAgent(ctx, repo+"/llm/mossArchitect", "declare plants")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	rows := toolRows(t, ds, res.Thread)
	if len(rows) != 2 || rows[0]["ok"] != true {
		t.Fatalf("plan under a gate: %v", rows)
	}
	if content, _ := rows[1]["content"].(string); rows[1]["ok"] != false || !strings.Contains(content, "gates put "+kindKind) {
		t.Fatalf("gated apply: ok %v, %s", rows[1]["ok"], content)
	}
	if _, err := ds.resolveType(repo + "/garden/plant"); err == nil {
		t.Fatal("a gated declaration landed")
	}
}

// The grant is a load error, not a dispatch surprise: vocabulary needs the
// kind declaration in the agent's writes.
func TestAgentVocabularyNeedsTheKindKindInWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _, repo := openMossDataset(t)
	_, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, []map[string]any{
		vocabulary.PackageManifest(repo+"/probe", 0),
		vocabulary.AgentManifest(repo+"/probe", "shaper", map[string]any{
			"description": "declares nothing", "prompt": "x", "provider": "openai", "model": "gpt-5",
			"tools":       []any{map[string]any{"function": vocabulary.HostFunctionVocabulary}},
			"permissions": map[string]any{"writes": []any{kindPackage}},
		}),
	})
	if err == nil || !strings.Contains(err.Error(), "vocabulary needs "+kindKind) {
		t.Fatalf("err = %v, want the load error naming %s", err, kindKind)
	}
}

// toolDescription reads one tool's description off a completion request.
func toolDescription(t *testing.T, req map[string]any, name string) string {
	t.Helper()
	tools, _ := req["tools"].([]any)
	for _, tv := range tools {
		fn, _ := tv.(map[string]any)["function"].(map[string]any)
		if fn["name"] == name {
			s, _ := fn["description"].(string)
			return s
		}
	}
	t.Fatalf("no tool %s on the request", name)
	return ""
}
