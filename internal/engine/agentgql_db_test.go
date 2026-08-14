package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// The graphql/mutate built-ins and the subagentOnly withholding, against the
// crew fixture (agents_db_test.go): archivist reads through graphql alone,
// editor mutates within its widget-only emit, judge exists to be called.

func gqlToolArgs(t *testing.T, m map[string]any) string {
	t.Helper()
	buf, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(buf)
}

// lastToolMessage returns the newest role=tool row on a thread.
func lastToolMessage(t *testing.T, ds *dataset, threadID string) map[string]any {
	t.Helper()
	var tool map[string]any
	for _, m := range threadMessages(t, ds, threadID) {
		if m["role"] == "tool" {
			tool = m
		}
	}
	if tool == nil {
		t.Fatal("no tool message on the thread")
	}
	return tool
}

func TestAgentGraphQLReads(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewAuthority + "/widget", ID: "w-listed",
		Properties: map[string]any{"name": "first"},
	}); err != nil {
		t.Fatal(err)
	}
	fake.script("gql",
		fakeTurn{calls: []fakeCall{{"graphql", gqlToolArgs(t, map[string]any{
			"query": `{ records(filter: {kinds: ["crew.test.dev/widget"]}, first: 5) { nodes { id kind } } }`,
		})}}},
		fakeTurn{content: "one widget"},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/archivist", "list widgets")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.Status != threadOK || res.Reply != "one widget" {
		t.Fatalf("result: %+v", res)
	}
	tool := lastToolMessage(t, ds, res.Thread)
	if tool["ok"] != true {
		t.Fatalf("graphql read failed: %v", tool["content"])
	}
	content, _ := tool["content"].(string)
	if !strings.Contains(content, "w-listed") {
		t.Fatalf("result does not carry the row: %s", content)
	}
}

func TestAgentGraphQLRefusesMutations(t *testing.T) {
	t.Parallel()
	// The chat-grade read tool holds at the AST: a mutation in its document
	// is a tool error the model sees, and nothing lands.
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("gql",
		fakeTurn{calls: []fakeCall{{"graphql", gqlToolArgs(t, map[string]any{
			"query": `mutation { put(input: {kind: "crew.test.dev/widget", id: "w-sneak", properties: {name: "no"}}) { id } }`,
		})}}},
		fakeTurn{content: "refused, stopping"},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/archivist", "write one")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	tool := lastToolMessage(t, ds, res.Thread)
	if tool["ok"] != false {
		t.Fatal("a mutation document passed the read-only gate")
	}
	if content, _ := tool["content"].(string); !strings.Contains(content, "mutations are not allowed") {
		t.Fatalf("refusal does not name the gate: %s", content)
	}
	if _, err := ds.Get(ctx, crewAuthority+"/widget", "w-sneak"); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the refused mutation landed: %v", err)
	}
	if res.Effects != 0 {
		t.Fatalf("effects tallied on a refusal: %+v", res)
	}
}

func TestAgentMutateWritesWithinEmit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("mut",
		fakeTurn{calls: []fakeCall{{"mutate", gqlToolArgs(t, map[string]any{
			"query": `mutation { put(input: {kind: "crew.test.dev/widget", id: "w-made", properties: {name: "made"}}) { id } }`,
		})}}},
		fakeTurn{content: "made it"},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/editor", "make a widget")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.Status != threadOK || res.Effects != 1 {
		t.Fatalf("result: %+v", res)
	}
	tool := lastToolMessage(t, ds, res.Thread)
	if tool["ok"] != true {
		t.Fatalf("mutate failed: %v", tool["content"])
	}
	e, err := ds.Get(ctx, crewAuthority+"/widget", "w-made")
	if err != nil {
		t.Fatalf("the mutation did not land: %v", err)
	}
	if e.Properties["name"] != "made" {
		t.Fatalf("widget props: %+v", e.Properties)
	}
	// The write landed under the AGENT's actor, through the public write path.
	changes, err := ds.Changes(ctx, 0, substrate.ChangeFilter{RecordID: "w-made", Kinds: []string{crewAuthority + "/widget"}}, 10)
	if err != nil || len(changes) == 0 {
		t.Fatalf("changes: %v %v", changes, err)
	}
	if changes[0].Actor != "function:editor" {
		t.Fatalf("actor %q, want the agent's", changes[0].Actor)
	}
}

func TestAgentMutateHoldsEmitAndRefusesMerge(t *testing.T) {
	t.Parallel()
	// Editor's emit names widgets alone: a put outside it refuses naming the
	// allowlist, and merge refuses on principle — both as results the model
	// sees, with nothing applied.
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("mut",
		fakeTurn{calls: []fakeCall{{"mutate", gqlToolArgs(t, map[string]any{
			"query": `mutation { put(input: {kind: "tasks.substrate.reamde.dev/task", id: "t-sneak", properties: {title: "no"}}) { id } }`,
		})}}},
		fakeTurn{calls: []fakeCall{{"mutate", gqlToolArgs(t, map[string]any{
			"query": `mutation { merge(kind: "crew.test.dev/widget", winner: "w-a", loser: "w-b") { id } }`,
		})}}},
		fakeTurn{content: "blocked twice"},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/editor", "go")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.Effects != 0 {
		t.Fatalf("a refused mutation tallied: %+v", res)
	}
	msgs := threadMessages(t, ds, res.Thread)
	var toolContents []string
	for _, m := range msgs {
		if m["role"] == "tool" {
			if m["ok"] == true {
				t.Fatalf("a gated mutation reported ok: %v", m["content"])
			}
			content, _ := m["content"].(string)
			toolContents = append(toolContents, content)
		}
	}
	if len(toolContents) != 2 {
		t.Fatalf("tool rows: %v", toolContents)
	}
	if !strings.Contains(toolContents[0], "effective emit allowlist") {
		t.Fatalf("emit refusal does not name the gate: %s", toolContents[0])
	}
	if !strings.Contains(toolContents[1], "owner's decision") {
		t.Fatalf("merge refusal does not name the owner: %s", toolContents[1])
	}
	if _, err := ds.Get(ctx, "tasks.substrate.reamde.dev/task", "t-sneak"); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the refused put landed: %v", err)
	}
}

func TestSubagentOnlyWithholdsChatAlone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)

	// The chat surface refuses before any completion is bought.
	_, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewAuthority+"/judge", "", "hi", nil)
	if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "subagent-only") {
		t.Fatalf("chat on a subagent-only agent: %v", err)
	}

	// The flag projects onto the agent's row, where the console's filter
	// reads it; the unmarked sibling projects without the key.
	row, err := ds.Get(ctx, kindAgent, crewAuthority+"/judge")
	if err != nil {
		t.Fatal(err)
	}
	if row.Properties["subagentOnly"] != true {
		t.Fatalf("projection: %+v", row.Properties["subagentOnly"])
	}
	if chatter, err := ds.Get(ctx, kindAgent, crewAuthority+"/chatter"); err != nil || chatter.Properties["subagentOnly"] != nil {
		t.Fatalf("unmarked agent projects subagentOnly: %v %v", chatter, err)
	}

	// The call API still dispatches it, and so does a caller's sub-agent hop.
	fake.script("judge", fakeTurn{content: "verdict"})
	if res, err := ds.CallAgent(ctx, crewAuthority+"/judge", "weigh this"); err != nil || res.Reply != "verdict" {
		t.Fatalf("call on a subagent-only agent: %+v %v", res, err)
	}
	fake.script("justice",
		fakeTurn{calls: []fakeCall{{"judge", `{"input":"weigh this"}`}}},
		fakeTurn{content: "delivered"},
	)
	fake.script("judge", fakeTurn{content: "guilty"})
	res, err := ds.CallAgent(ctx, crewAuthority+"/justice", "try the case")
	if err != nil || res.Status != threadOK || res.Reply != "delivered" {
		t.Fatalf("sub-agent hop to a subagent-only agent: %+v %v", res, err)
	}
}
