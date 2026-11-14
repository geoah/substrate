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

// openMossDataset opens a repository with the shipped assistant, its
// `anthropic` row pointed at the fake on the wire the fake speaks, and one
// kind of the owner's own with a long text to edit: `<repo>/scratch/pad`. It
// installs no function body, so the suite runs where no sandbox can confine
// one.
func openMossDataset(t *testing.T) (*dataset, *fakeLLM, string) {
	t.Helper()
	ds := openInternalDataset(t)
	fake := newFakeLLM(t)
	ds.mu.Lock()
	ds.skipThreadTitles = true
	ds.mu.Unlock()
	if _, err := ds.Put(context.Background(), substrate.ActorPerson, substrate.PutInput{
		Kind: typeProvider, ID: "anthropic",
		Properties: map[string]any{"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-anthropic"},
	}); err != nil {
		t.Fatal(err)
	}
	repo := testdb.Repository(t)
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(repo+"/scratch", 0),
		vocabulary.KindManifest(repo+"/scratch", map[string]any{"singular": "pad"}, map[string]any{
			"description":     "A practice pad: a name and a long note.",
			"displayTemplate": "{name}",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "what the pad is called"},
				"note": map[string]any{"type": "text", "description": "the pad's text"},
			},
		}),
	}); err != nil {
		t.Fatalf("declare the scratch pad: %v", err)
	}
	return ds, fake, repo
}

// mossModel is the model Moss and its architect name, so one script serves
// both, in the order the chain calls them.
const mossModel = "claude-opus-5-5"

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

	fake.script(mossModel,
		// Moss: a collection is the architect's.
		fakeTurn{calls: []fakeCall{{"architect", toolArgs(t, map[string]any{
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
	res, err := ds.ChatAgent(ctx, substrate.ActorPerson, mossID, substrate.ChatInput{Message: "start tracking my plants; I have a fern I water weekly"}, nil)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if res.Status != threadOK {
		t.Fatalf("settled %q (%s)", res.Status, res.Reason)
	}

	reqs := fake.requestsOf(mossModel)
	names := toolNamesSent(t, reqs[0])
	for _, name := range []string{"query", "write", "propose", "ask", "wait", "edit", "architect", "librarian"} {
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
	if card := toolDescription(t, reqs[1], "vocabulary"); !strings.Contains(card, "live under `~`") {
		t.Fatalf("the vocabulary card does not name the owner's own namespace, ~: %s", card)
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
	// The package's install is when its actor comes to be (decision 0270):
	// its record is the third write.
	child, records, kinds, _ := writesOf(t, rows[0])
	if records != 3 || !slices.Contains(kinds, kindKind) || !slices.Contains(kinds, kindPackage) || !slices.Contains(kinds, kindActor) {
		t.Fatalf("architect wrote %d records of %v, want the package, its actor and the kind", records, kinds)
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
		{[]any{map[string]any{"kind": repo + "/scratch/pad", "metadata": map[string]any{"id": "x"}, "data": map[string]any{}}}, "not a declaration kind"},
		{[]any{shipped}, "shipped package"},
	}
	turns := make([]fakeTurn, 0, len(cases)+1)
	for _, c := range cases {
		turns = append(turns, fakeTurn{calls: []fakeCall{{"vocabulary", toolArgs(t, map[string]any{"documents": c.docs})}}})
	}
	fake.script(mossModel, append(turns, fakeTurn{content: "refused, stopping"})...)
	res, err := ds.CallAgent(ctx, architectID, "try them all")
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
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{
		Kind: vocabulary.KindRecordPatchPolicy, ID: fid("gate-kinds"),
		Properties: map[string]any{
			"selector": map[string]any{"kinds": []any{kindKind}, "agents": []any{architectID}},
			"action":   "gate",
		},
	}); err != nil {
		t.Fatalf("put the policy: %v", err)
	}
	docs := gardenDocs(repo)
	fake.script(mossModel,
		fakeTurn{calls: []fakeCall{{"vocabulary", toolArgs(t, map[string]any{"documents": docs, "plan": true})}}},
		fakeTurn{calls: []fakeCall{{"vocabulary", toolArgs(t, map[string]any{"documents": docs})}}},
		fakeTurn{content: "gated"},
	)
	res, err := ds.CallAgent(ctx, architectID, "declare plants")
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
// kind declaration named in the agent's writes, and a glob does not name it.
func TestAgentVocabularyNeedsTheKindKindNamedInWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _, repo := openMossDataset(t)
	for _, writes := range [][]any{{kindPackage}, {"*"}} {
		_, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
			vocabulary.PackageManifest(repo+"/probe", 0),
			vocabulary.AgentManifest(repo+"/probe", "shaper", map[string]any{
				"description": "declares nothing", "prompt": "x", "provider": "openai", "model": "gpt-5",
				"tools":       []any{map[string]any{"function": vocabulary.HostFunctionVocabulary}},
				"permissions": map[string]any{"writes": writes},
			}),
		})
		if err == nil || !strings.Contains(err.Error(), "vocabulary needs "+kindKind+" named") {
			t.Fatalf("writes %v: err = %v, want the load error naming %s", writes, err, kindKind)
		}
	}
}

// A glob never reaches a declaration kind or a trigger: Moss's `*` covers
// the owner's data, and its `write` still cannot reshape the vocabulary or
// schedule code.
func TestAGlobGrantWritesNoDeclarationOrTrigger(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake, repo := openMossDataset(t)
	fake.script(mossModel,
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", kindKind, repo+"/llm/sneaky", map[string]any{
			"authority": repo, "package": "llm", "description": "x",
		})}}},
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", typeTrigger, "", map[string]any{
			"callable": kindAgent + "/" + mossID,
			"source":   map[string]any{"schedule": map[string]any{"every": "1h"}},
		})}}},
		fakeTurn{content: "refused"},
	)
	res, err := ds.CallAgent(ctx, mossID, "declare one, then schedule yourself")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	rows := toolRows(t, ds, res.Thread)
	for i, want := range []string{"is a declaration kind", "is the trigger kind"} {
		if content, _ := rows[i]["content"].(string); rows[i]["ok"] != false || !strings.Contains(content, want) {
			t.Fatalf("call %d under *: ok %v, %s; want a refusal naming %q", i, rows[i]["ok"], content, want)
		}
	}
	if _, err := ds.resolveType(repo + "/llm/sneaky"); err == nil {
		t.Fatal("a declaration landed through a glob grant")
	}
	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{typeTrigger}}, First: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 0 {
		t.Fatalf("a trigger landed through a glob grant: %+v", page.Records)
	}
}

// A preview flag that is not a boolean is refused, never read as an apply;
// and a batch that would convert live records is the owner's, whatever the
// plan said.
func TestAgentVocabularyRefusesAStringPlanAndAConversion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake, repo := openMossDataset(t)
	plant := repo + "/garden/plant"
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, docMaps(gardenDocs(repo))); err != nil {
		t.Fatal(err)
	}
	fern, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: plant, Properties: map[string]any{"name": "Fern", "waterEveryDays": 7}})
	if err != nil {
		t.Fatal(err)
	}
	renamed := gardenDocs(repo)[1:]
	props := renamed[0].(map[string]any)["data"].(map[string]any)["properties"].(map[string]any)
	delete(props, "waterEveryDays")
	props["wateringDays"] = map[string]any{"type": "int", "renamedFrom": "waterEveryDays", "description": "days between waterings"}
	fake.script(mossModel,
		fakeTurn{calls: []fakeCall{{"vocabulary", toolArgs(t, map[string]any{"documents": renamed, "plan": "true"})}}},
		fakeTurn{calls: []fakeCall{{"vocabulary", toolArgs(t, map[string]any{"documents": renamed})}}},
		fakeTurn{content: "stopped"},
	)
	res, err := ds.CallAgent(ctx, architectID, "rename the interval")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	rows := toolRows(t, ds, res.Thread)
	for i, want := range []string{"plan must be true or false", "would rewrite 1 live records"} {
		if content, _ := rows[i]["content"].(string); rows[i]["ok"] != false || !strings.Contains(content, want) {
			t.Fatalf("call %d: ok %v, %s; want a refusal naming %q", i, rows[i]["ok"], content, want)
		}
	}
	got, err := ds.Get(ctx, plant, fern.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Properties["waterEveryDays"] == nil || got.Properties["wateringDays"] != nil {
		t.Fatalf("the refused rename converted the record: %v", got.Properties)
	}
}

// docMaps is gardenDocs' documents in the shape ApplyVocabularyDocuments takes.
func docMaps(docs []any) []map[string]any {
	out := make([]map[string]any, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.(map[string]any))
	}
	return out
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
