package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The graphql/mutate built-ins and the hiddenFromChat withholding, against the
// crew fixture (agents_db_test.go): archivist reads through graphql alone,
// editor mutates within its widget-only emit, arbiter decides change requests
// within its own, judge exists to be called.

// taskKind is a kind OUTSIDE the crew authority's own vocabulary: the arbiter's
// emit does not name it, so it is the confused-deputy target.
const taskKind = "tasks.substrate.reamde.dev/task"

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
	if changes[0].Actor != substrate.Actor("agent:"+crewAuthority+":editor") {
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

// --- deciding a change request through the mutate tool -----------------------

// putRequest lands one proposed patch request against a target, the way an app
// or the API would.
func putRequest(t *testing.T, ds *dataset, id, targetKind, targetID string, diff map[string]any) *substrate.Record {
	t.Helper()
	e, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{
		Kind: vocabulary.KindRecordPatchRequest, ID: id,
		Properties: map[string]any{
			"diff":   map[string]any{"properties": diff},
			"target": vocabulary.RecordPath(targetKind, targetID),
		},
	})
	if err != nil {
		t.Fatalf("put request %s: %v", id, err)
	}
	return e
}

// decideArgs is the mutate document that decides one request — the accept an
// agent reviewer writes.
func decideArgs(t *testing.T, id, decision string) string {
	t.Helper()
	return gqlToolArgs(t, map[string]any{
		"query": `mutation Decide($id: ID!, $decision: JSON!) {
			patch(kind: "core.substrate.reamde.dev/recordpatchrequest", id: $id, input: $decision) { id }
		}`,
		"variables": map[string]any{
			"id": id, "decision": map[string]any{"properties": map[string]any{"decision": decision}},
		},
	})
}

func TestAgentMutateDecidesRequestsWithinEmit(t *testing.T) {
	t.Parallel()
	// Accept and reject are SYMMETRIC through the mutate tool: the wrapper
	// carries the agent's effective emit into the transaction, so an accept is
	// bounded by that ceiling exactly like a function-tool effect — it applies
	// when the target kind is in the emit, and refuses as a confused deputy when
	// it is not. Rejecting runs no transition and always worked.
	ctx := context.Background()
	ds, fake := openAgentDataset(t)

	widget, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewAuthority + "/widget", ID: "w-decided", Properties: map[string]any{"name": "raw"},
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: taskKind, ID: "t-guarded", Properties: map[string]any{"name": "not the agent's"},
	})
	if err != nil {
		t.Fatal(err)
	}
	putRequest(t, ds, "req-accept", widget.Kind, widget.ID, map[string]any{"name": "better"})
	putRequest(t, ds, "req-deputy", task.Kind, task.ID, map[string]any{"description": "smuggled"})
	putRequest(t, ds, "req-reject", widget.Kind, widget.ID, map[string]any{"name": "worse"})

	fake.script("arbiter",
		fakeTurn{calls: []fakeCall{{"mutate", decideArgs(t, "req-accept", "accepted")}}},
		fakeTurn{calls: []fakeCall{{"mutate", decideArgs(t, "req-deputy", "accepted")}}},
		fakeTurn{calls: []fakeCall{{"mutate", decideArgs(t, "req-reject", "rejected")}}},
		fakeTurn{content: "decided"},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/arbiter", "work the inbox")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if res.Status != threadOK {
		t.Fatalf("result: %+v", res)
	}
	var tools []map[string]any
	for _, m := range threadMessages(t, ds, res.Thread) {
		if m["role"] == "tool" {
			tools = append(tools, m)
		}
	}
	if len(tools) != 3 {
		t.Fatalf("tool rows: %d", len(tools))
	}

	// The accept within the emit ran the transition: the diff applied and the
	// decision is stamped.
	if tools[0]["ok"] != true {
		t.Fatalf("the in-emit accept failed: %v", tools[0]["content"])
	}
	accepted, err := ds.Get(ctx, vocabulary.KindRecordPatchRequest, "req-accept")
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Properties["decision"] != "accepted" || accepted.Properties["decidedAt"] == nil {
		t.Fatalf("accepted request: %+v", accepted.Properties)
	}
	if got, err := ds.Get(ctx, widget.Kind, widget.ID); err != nil || got.Properties["name"] != "better" {
		t.Fatalf("the accepted diff did not land: %+v %v", got, err)
	}

	// The accept OUTSIDE the emit is refused: the request stays proposed,
	// annotated, and the task is untouched.
	if tools[1]["ok"] != false {
		t.Fatalf("a confused-deputy accept reported ok: %v", tools[1]["content"])
	}
	if content, _ := tools[1]["content"].(string); !strings.Contains(content, "emit allowlist") {
		t.Fatalf("the refusal does not name the ceiling: %s", content)
	}
	deputy, err := ds.Get(ctx, vocabulary.KindRecordPatchRequest, "req-deputy")
	if err != nil {
		t.Fatal(err)
	}
	if deputy.Properties["decision"] != "proposed" || deputy.Annotations["substrate/conflict"] == nil {
		t.Fatalf("refused request: %+v %+v", deputy.Properties, deputy.Annotations)
	}
	if got, err := ds.Get(ctx, task.Kind, task.ID); err != nil || got.Properties["description"] != nil {
		t.Fatalf("the agent smuggled a task patch past its ceiling: %+v %v", got, err)
	}

	// Reject still works, and applies nothing.
	if tools[2]["ok"] != true {
		t.Fatalf("the reject failed: %v", tools[2]["content"])
	}
	rejected, err := ds.Get(ctx, vocabulary.KindRecordPatchRequest, "req-reject")
	if err != nil {
		t.Fatal(err)
	}
	if rejected.Properties["decision"] != "rejected" || rejected.Properties["decidedAt"] == nil {
		t.Fatalf("rejected request: %+v", rejected.Properties)
	}
	if got, err := ds.Get(ctx, widget.Kind, widget.ID); err != nil || got.Properties["name"] != "better" {
		t.Fatalf("the rejected diff applied: %+v %v", got, err)
	}
}

func TestTriggerFiresAgentThatAcceptsRequest(t *testing.T) {
	t.Parallel()
	// The whole loop, end to end: a proposed request fires a trigger, the agent
	// it names decides it through the mutate tool, and the accepted diff lands
	// on the target.
	ctx := context.Background()
	ds, fake := openAgentDataset(t)

	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger, ID: "on-proposal",
		Properties: map[string]any{
			"source": map[string]any{"record": map[string]any{
				"kinds": []any{vocabulary.KindRecordPatchRequest}, "ops": []any{"create"},
			}},
			"callable": vocabulary.RecordPath("core.substrate.reamde.dev/agent", crewAuthority+"/arbiter"),
		},
	}); err != nil {
		t.Fatalf("put the proposal trigger: %v", err)
	}
	widget, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewAuthority + "/widget", ID: "w-reviewed", Properties: map[string]any{"name": "raw"},
	})
	if err != nil {
		t.Fatal(err)
	}

	fake.script("arbiter",
		fakeTurn{calls: []fakeCall{{"mutate", decideArgs(t, "req-fired", "accepted")}}},
		fakeTurn{content: "reviewed"},
	)
	putRequest(t, ds, "req-fired", widget.Kind, widget.ID, map[string]any{"name": "reviewed"})
	if _, err := ds.ProcessTriggers(ctx); err != nil {
		t.Fatalf("process: %v", err)
	}

	req, err := ds.Get(ctx, vocabulary.KindRecordPatchRequest, "req-fired")
	if err != nil {
		t.Fatal(err)
	}
	if req.Properties["decision"] != "accepted" {
		t.Fatalf("the fired agent did not accept: %+v %+v", req.Properties, req.Annotations)
	}
	if got, err := ds.Get(ctx, widget.Kind, widget.ID); err != nil || got.Properties["name"] != "reviewed" {
		t.Fatalf("the accepted diff did not land: %+v %v", got, err)
	}
	if threads := agentThreadsOf(t, ds, "arbiter"); len(threads) != 1 || threads[0]["mode"] != "record" {
		t.Fatalf("arbiter threads: %+v", threads)
	}
}

func TestHiddenFromChatWithholdsChatAlone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)

	// The chat surface refuses before any completion is bought.
	_, err := ds.ChatAgent(ctx, substrate.ActorAPI, crewAuthority+"/judge", "", "hi", nil)
	if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "hidden from chat") {
		t.Fatalf("chat on an agent hidden from chat: %v", err)
	}

	// The flag projects onto the agent's row, where the console's filter
	// reads it; the unmarked sibling projects without the key.
	row, err := ds.Get(ctx, kindAgent, crewAuthority+"/judge")
	if err != nil {
		t.Fatal(err)
	}
	if row.Properties["hiddenFromChat"] != true {
		t.Fatalf("projection: %+v", row.Properties["hiddenFromChat"])
	}
	if chatter, err := ds.Get(ctx, kindAgent, crewAuthority+"/chatter"); err != nil || chatter.Properties["hiddenFromChat"] != nil {
		t.Fatalf("unmarked agent projects hiddenFromChat: %v %v", chatter, err)
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
