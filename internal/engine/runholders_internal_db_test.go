package engine

// What holds a run (run.go, decision 0276), over the roles repository: every
// read and write of a run's code is held to the persons and the apps its
// turn works for, read now, a continuation is admitted only where its hand
// could have started the run, and the record of the run itself is the one
// exemption.

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// runsRepository is the roles repository with a package of agents on the
// fake LLM, and a note kind they read and write.
type runsRepository struct {
	*rolesRepository
	fake      *fakeLLM
	pkg, note string
}

const runsAsk = `{"questions":[{"id":"sure","prompt":"Are you sure?"}]}`

func newRunsRepository(t *testing.T) *runsRepository {
	t.Helper()
	ctx := context.Background()
	rr := newRolesRepository(t)
	ds := rr.ds
	fake := newFakeLLM(t)
	ds.mu.Lock()
	ds.skipThreadTitles = true
	ds.mu.Unlock()
	if _, err := ds.Put(rr.as(rr.adaID), substrate.ActorPerson, substrate.PutInput{
		Kind: typeProvider, ID: "runsllm",
		Properties: map[string]any{"wire": "openai", "baseURL": fake.srv.URL, "apiKey": "row-key-runsllm"},
	}); err != nil {
		t.Fatal(err)
	}
	pkg := ds.Repository().Authority + "/runs"
	note := pkg + "/note"
	agent := func(name string, data map[string]any) map[string]any {
		data["description"] = name + " under test"
		data["prompt"] = "You are " + name + "."
		data["provider"] = "runsllm"
		data["model"] = name
		return vocabulary.AgentManifest(pkg, name, data)
	}
	tool := func(fn string) map[string]any { return map[string]any{"function": fn} }
	approved := func(name, kind, id string) map[string]any {
		return vocabulary.FunctionManifest(pkg, name, map[string]any{
			"description": name + " under test",
			"runtime":     vocabulary.RuntimePython,
			"effect":      vocabulary.FunctionEffectIrreversible,
			"approval":    vocabulary.FunctionApprovalRequired,
			"permissions": map[string]any{"writes": []any{kind}},
			"source": `
def main(input, host):
    return {"effects": [{"action": "put", "kind": "` + kind + `", "id": "` + id + `", "properties": {"wire": "openai"} if "` + kind + `".endswith("/provider") else {"name": "saved"}}],
            "output": {}}
`,
		})
	}
	reads := func(kinds ...string) map[string]any {
		out := make([]any, len(kinds))
		for i, k := range kinds {
			out[i] = k
		}
		return map[string]any{"kinds": out}
	}
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "note"}, map[string]any{
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
		}),
		approved("setprovider", typeProvider, "set-by-a-call"),
		approved("savenote", note, fid("saved-by-a-call")),
		agent("scribe", map[string]any{
			"tools":       []any{tool(vocabulary.HostFunctionQuery), tool(vocabulary.HostFunctionWrite), tool(vocabulary.HostFunctionAsk)},
			"permissions": map[string]any{"reads": reads(note), "writes": []any{note, vocabulary.KindLLMInteraction}},
		}),
		agent("keeper", map[string]any{
			"tools":       []any{tool(vocabulary.HostFunctionAsk)},
			"permissions": map[string]any{"reads": reads(typeProvider), "writes": []any{vocabulary.KindLLMInteraction}},
		}),
		agent("setter", map[string]any{
			"tools":       []any{tool(pkg + "/setprovider")},
			"permissions": map[string]any{"reads": reads(note), "writes": []any{typeProvider}},
		}),
		agent("saver", map[string]any{
			"tools":       []any{tool(pkg + "/savenote")},
			"permissions": map[string]any{"reads": reads(note), "writes": []any{note}},
		}),
		agent("lead", map[string]any{
			"subagents":   []any{pkg + "/scribe"},
			"permissions": map[string]any{"reads": reads(note), "writes": []any{note}},
		}),
		agent("delegator", map[string]any{
			"subagents":   []any{pkg + "/setter"},
			"permissions": map[string]any{"reads": reads(note), "writes": []any{typeProvider}},
		}),
		agent("digger", map[string]any{
			"tools":       []any{tool(vocabulary.HostFunctionWrite)},
			"permissions": map[string]any{"reads": reads(note), "writes": []any{note}},
		}),
		agent("boss", map[string]any{
			"subagents":   []any{map[string]any{"ref": pkg + "/digger", "background": true}},
			"permissions": map[string]any{"reads": reads(note), "writes": []any{note}},
		}),
		agent("pinner", map[string]any{
			"permissions": map[string]any{"reads": reads(typeProvider, note)},
			"context": map[string]any{"pinned": []any{map[string]any{
				"kind": typeProvider, "orderBy": "id", "fields": []any{"baseURL"},
			}}},
		}),
		agent("notepinner", map[string]any{
			"tools":       []any{tool(vocabulary.HostFunctionAsk)},
			"permissions": map[string]any{"reads": reads(note), "writes": []any{vocabulary.KindLLMInteraction}},
			"context": map[string]any{"pinned": []any{map[string]any{
				"kind": note, "orderBy": "id", "fields": []any{"name"},
			}}},
		}),
		agent("editor", map[string]any{
			"tools":       []any{tool(vocabulary.HostFunctionEdit)},
			"permissions": map[string]any{"reads": reads(note), "writes": []any{note}},
		}),
		agent("declarer", map[string]any{
			"tools":       []any{tool(vocabulary.HostFunctionVocabulary)},
			"permissions": map[string]any{"writes": []any{vocabulary.CoreKind("package"), vocabulary.CoreKind("kind")}},
		}),
		agent("previewer", map[string]any{
			"tools":       []any{tool(vocabulary.HostFunctionAppletPreview)},
			"permissions": map[string]any{"reads": reads(note)},
		}),
		vocabulary.FunctionManifest(pkg, "reader", map[string]any{
			"description": "reader under test",
			"runtime":     vocabulary.RuntimePython,
			"arguments":   []any{map[string]any{"name": "op", "type": "string", "required": true}},
			"permissions": map[string]any{"reads": reads(note)},
			"source": `
def main(input, host):
    op = input["args"]["op"]
    if op == "get":
        return {"output": host.records.get("` + note + `", "` + fid("n1") + `")}
    if op == "list":
        return {"output": host.records.list("` + note + `", first=1)}
    if op == "aggregate":
        return {"output": host.records.aggregate({"filter": {"kinds": ["` + note + `"]}, "aggregate": {"reducers": ["count"]}})}
    return {"output": host.search(q="seen", mode="lexical", kinds=["` + note + `"])}
`,
		}),
		agent("historian", map[string]any{
			"tools": []any{tool(vocabulary.HostFunctionEdit), tool(vocabulary.HostFunctionWrite)},
			"permissions": map[string]any{
				"reads":  reads(typeThread, typeMessage, vocabulary.KindLLMInteraction, vocabulary.KindLLMApproval),
				"writes": []any{typeThread, typeMessage, vocabulary.KindLLMInteraction, vocabulary.KindLLMApproval},
			},
		}),
		agent("proposer", map[string]any{
			"tools":       []any{tool(vocabulary.HostFunctionPropose)},
			"permissions": map[string]any{"reads": reads(note), "writes": []any{vocabulary.KindRecordPatchRequest}},
		}),
		agent("prober", map[string]any{
			"tools":       []any{tool(vocabulary.HostFunctionPropose)},
			"permissions": map[string]any{"reads": reads(note), "writes": []any{vocabulary.KindRecordPatchRequest}},
		}),
		vocabulary.FunctionManifest(pkg, "starter", map[string]any{
			"description": "starter under test",
			"runtime":     vocabulary.RuntimePython,
			"permissions": map[string]any{"agents": []any{pkg + "/setter"}},
			"source": `
def main(input, host):
    return {"output": host.agents.call("` + pkg + `/setter", "go")}
`,
		}),
	}); err != nil {
		t.Fatalf("install the runs package: %v", err)
	}
	return &runsRepository{rolesRepository: rr, fake: fake, pkg: pkg, note: note}
}

// app is a token ada approves for an app, with scopes, and the context of a
// request it makes.
func (r *runsRepository) app(t *testing.T, client string, scopes ...string) (substrate.Actor, context.Context, string) {
	t.Helper()
	info, _, err := r.ds.MintToken(r.as(r.adaID), substrate.MintTokenInput{
		Label: client, Scopes: scopes, ClientID: "https://" + client + "/client.json", ClientName: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := substrate.WithPrincipal(substrate.WithOnBehalfOf(r.as(r.adaID), substrate.ActorPerson), info.ID)
	return substrate.Actor("app:" + client), ctx, info.ID
}

// person is the context of a request a person makes for themselves.
func (r *runsRepository) person(identity string, key substrate.Actor) context.Context {
	return substrate.WithOnBehalfOf(r.as(identity), key)
}

// chat runs one chat turn of agent, in thread when it names one, and its
// tool rows.
func (r *runsRepository) chat(t *testing.T, ctx context.Context, actor substrate.Actor, agent, thread string, script ...fakeTurn) (string, []map[string]any) {
	t.Helper()
	r.fake.script(agent, script...)
	res, err := r.ds.ChatAgent(ctx, actor, r.pkg+"/"+agent, substrate.ChatInput{Message: "go", Thread: thread}, nil)
	if err != nil {
		t.Fatalf("chat with %s: %v", agent, err)
	}
	return res.Thread, toolRows(t, r.ds, res.Thread)
}

// lastTool is the latest tool row of thread.
func lastTool(t *testing.T, ds *dataset, thread string) map[string]any {
	t.Helper()
	rows := toolRows(t, ds, thread)
	if len(rows) == 0 {
		t.Fatalf("thread %s has no tool row", thread)
	}
	return rows[len(rows)-1]
}

func (r *runsRepository) notes(t *testing.T) int {
	t.Helper()
	page, err := r.ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{Kinds: []string{r.note}}, First: 100})
	if err != nil {
		t.Fatal(err)
	}
	return len(page.Records)
}

func refusedRow(row map[string]any) bool {
	content, _ := row["content"].(string)
	return row["ok"] == false && strings.Contains(content, "may not")
}

// An app's turn is held to the app: the write its grant lacks is refused.
// The person's next turn in the same thread is the person's, and makes it.
func TestAnAppsTurnIsHeldToTheAppAndThePersonsNextTurnIsNot(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	actor, ctx, _ := r.app(t, "reader.example.org", "agents:"+r.pkg+"/*", "read:"+r.note)
	write := fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", r.note, "", map[string]any{"name": "x"})}}}
	thread, rows := r.chat(t, ctx, actor, "scribe", "", write, fakeTurn{content: "done."})
	if len(rows) != 1 || !refusedRow(rows[0]) || r.notes(t) != 0 {
		t.Fatalf("the app's turn wrote a note its grant lacks: %v", rows)
	}
	r.chat(t, r.person(r.adaID, substrate.ActorPerson), substrate.ActorPerson, "scribe", thread, write, fakeTurn{content: "done."})
	if row := lastTool(t, r.ds, thread); row["ok"] != true || r.notes(t) != 1 {
		t.Fatalf("the person's next turn in the thread: %v", row)
	}
}

// A thread one app opened and another continues: each turn is held to its
// own app.
func TestEachTurnOfAThreadIsHeldToItsOwnApp(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	writer, wctx, _ := r.app(t, "writer.example.org", "agents:"+r.pkg+"/*", "write:"+r.note)
	reader, rctx, _ := r.app(t, "reader.example.org", "agents:"+r.pkg+"/*", "read:"+r.note)
	write := fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", r.note, "", map[string]any{"name": "x"})}}}
	thread, rows := r.chat(t, wctx, writer, "scribe", "", write, fakeTurn{content: "done."})
	if len(rows) != 1 || rows[0]["ok"] != true || r.notes(t) != 1 {
		t.Fatalf("the writing app's turn: %v", rows)
	}
	r.chat(t, rctx, reader, "scribe", thread, write, fakeTurn{content: "done."})
	if row := lastTool(t, r.ds, thread); !refusedRow(row) || r.notes(t) != 1 {
		t.Fatalf("the reading app's turn in the writing app's thread: %v", row)
	}
}

// A role reduced between two turns of one run holds from the next turn.
func TestARoleReducedBetweenTwoTurnsHoldsTheNextTurn(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	bo := r.person(r.boID, substrate.PersonActor(r.boID))
	write := fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", r.note, "", map[string]any{"name": "x"})}}}
	thread, rows := r.chat(t, bo, substrate.PersonActor(r.boID), "scribe", "", write, fakeTurn{content: "done."})
	if len(rows) != 1 || rows[0]["ok"] != true {
		t.Fatalf("the editor's first turn: %v", rows)
	}
	if err := r.r.s.SetPersonRoles(context.Background(), r.bo, r.ada, scope.RoleViewer); err != nil {
		t.Fatal(err)
	}
	r.chat(t, bo, substrate.PersonActor(r.boID), "scribe", thread, write, fakeTurn{content: "done."})
	if row := lastTool(t, r.ds, thread); !refusedRow(row) || r.notes(t) != 1 {
		t.Fatalf("the second turn after the role was reduced: %v", row)
	}
}

// An app's token revoked between two reads of one turn refuses the second.
func TestATokenRevokedBetweenTwoReadsOfOneTurnRefusesTheSecond(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	actor, ctx, token := r.app(t, "reader.example.org", "agents:"+r.pkg+"/*", "read:"+r.note)
	query := toolArgs(t, map[string]any{"kind": r.note})
	arrived, release := make(chan struct{}), make(chan struct{})
	r.fake.script("scribe",
		fakeTurn{calls: []fakeCall{{"query", query}}},
		fakeTurn{calls: []fakeCall{{"query", query}}, arrived: arrived, release: release},
		fakeTurn{content: "done."},
	)
	done := make(chan *substrate.AgentResult, 1)
	go func() {
		res, err := r.ds.ChatAgent(ctx, actor, r.pkg+"/scribe", substrate.ChatInput{Message: "go"}, nil)
		if err != nil {
			t.Errorf("chat: %v", err)
		}
		done <- res
	}()
	<-arrived
	if _, err := r.ds.Delete(r.as(r.adaID), substrate.ActorPerson, kindToken, token, substrate.DeleteInput{}); err != nil {
		t.Fatalf("revoke the app's token: %v", err)
	}
	close(release)
	res := <-done
	if res == nil {
		t.FailNow()
	}
	rows := toolRows(t, r.ds, res.Thread)
	if len(rows) != 2 || rows[0]["ok"] != true {
		t.Fatalf("the first read: %v", rows)
	}
	if content, _ := rows[1]["content"].(string); rows[1]["ok"] != false || !strings.Contains(content, "revoked") {
		t.Fatalf("the read after the revoke: %v", rows[1])
	}
}

// approvalOf runs agent's turn whose call needs approval, in ctx: its thread,
// and the approval it lands.
func (r *runsRepository) approvalOf(t *testing.T, ctx context.Context, actor substrate.Actor, agent, tool string) (string, string) {
	t.Helper()
	thread, _ := r.chat(t, ctx, actor, agent, "",
		fakeTurn{calls: []fakeCall{{tool, `{}`}}}, fakeTurn{content: "it waits."}, fakeTurn{content: "done."})
	held := approvalsOf(t, r.ds, thread)
	if len(held) != 1 {
		t.Fatalf("approvals of %s's turn = %d, want 1", agent, len(held))
	}
	return thread, held[0].ID
}

// approve has ctx's hand approve id, and waits for its call's end and the
// resume of thread it reports into, so the next turn's script is the next
// turn's.
func (r *runsRepository) approve(t *testing.T, ctx context.Context, thread, id string) string {
	t.Helper()
	row := approvalRow(t, r.ds, id)
	version := row.Version
	if _, err := r.ds.Patch(ctx, substrate.ActorPerson, vocabulary.KindLLMApproval, id, substrate.PatchInput{
		Properties: map[string]any{propApprovalState: approvalApproved}, IfVersion: &version,
	}); err != nil {
		t.Fatalf("approve %s: %v", id, err)
	}
	var state string
	waitFor(t, "the approved call's end", func() bool {
		state = approvalRow(t, r.ds, id).States[propApprovalState]
		return state == approvalDone || state == approvalFailed
	})
	waitFor(t, "the resume after the approval", func() bool {
		_, props := threadStatusOf(t, r.ds, thread)
		return props[propThreadLastReply] == "done."
	})
	return state
}

// An editor who approves an admin's call is held to their own rules too: the
// call writes a provider, which the editor's role does not, and is refused;
// the admin's approval of the same call runs it.
func TestAnEditorsApprovalOfAnAdminsCallIsHeldToTheEditor(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	ada := r.person(r.adaID, substrate.ActorPerson)
	thread, id := r.approvalOf(t, ada, substrate.ActorPerson, "setter", "setprovider")
	if state := r.approve(t, r.person(r.boID, substrate.PersonActor(r.boID)), thread, id); state != approvalFailed {
		t.Fatalf("the call an editor approved ended %s, want failed", state)
	}
	if row, _ := r.ds.loadRowDB(context.Background(), eref{Kind: typeProvider, ID: "set-by-a-call"}); row != nil {
		t.Fatal("the call an editor approved wrote a provider")
	}
	thread, id = r.approvalOf(t, ada, substrate.ActorPerson, "setter", "setprovider")
	if state := r.approve(t, ada, thread, id); state != approvalDone {
		t.Fatalf("the call the admin approved ended %s, want done", state)
	}
}

// An editor may not answer the ask of a run they could not start: its agent
// reads providers, a control kind. The admin answers it.
func TestAnEditorMayNotAnswerARunTheyCouldNotStart(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	thread, _ := r.chat(t, r.person(r.adaID, substrate.ActorPerson), substrate.ActorPerson, "keeper", "",
		fakeTurn{calls: []fakeCall{{"ask", runsAsk}}}, fakeTurn{content: "asked."}, fakeTurn{content: "noted."})
	var interaction *substrate.Record
	page, err := r.ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{Kinds: []string{vocabulary.KindLLMInteraction}}, First: 10})
	if err != nil || len(page.Records) != 1 {
		t.Fatalf("the ask of %s: %v %v", thread, page, err)
	}
	interaction = page.Records[0]
	answer := func(ctx context.Context) error {
		_, err := r.ds.Patch(ctx, substrate.ActorPerson, vocabulary.KindLLMInteraction, interaction.ID, substrate.PatchInput{
			Properties: map[string]any{"state": interactionAnswered, "answers": []any{map[string]any{"question": "sure", "selected": []any{"yes"}}}},
			IfVersion:  &interaction.Version,
		})
		return err
	}
	if err := answer(r.person(r.boID, substrate.PersonActor(r.boID))); !errors.Is(err, substrate.ErrNotPermitted) {
		t.Fatalf("an editor answering a run they could not start: %v, want refused by a rule", err)
	}
	if err := answer(r.person(r.adaID, substrate.ActorPerson)); err != nil {
		t.Fatalf("the admin answering their own run: %v", err)
	}
}

// The ask tool's exemption is the ask: an agent an app started deletes no
// interaction through its write tool when the app's grant does not write
// interactions, though the agent's emit names the kind.
func TestAnAppsAgentDeletesNoInteractionItsAppMayNotWrite(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	r.chat(t, r.person(r.adaID, substrate.ActorPerson), substrate.ActorPerson, "scribe", "",
		fakeTurn{calls: []fakeCall{{"ask", runsAsk}}}, fakeTurn{content: "asked."})
	page, err := r.ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{Kinds: []string{vocabulary.KindLLMInteraction}}, First: 10})
	if err != nil || len(page.Records) != 1 {
		t.Fatalf("another conversation's ask: %v %v", page, err)
	}
	other := page.Records[0].ID
	actor, ctx, _ := r.app(t, "writer.example.org", "agents:"+r.pkg+"/*", "write:"+r.note)
	_, rows := r.chat(t, ctx, actor, "scribe", "",
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "delete", vocabulary.KindLLMInteraction, other, nil)}}}, fakeTurn{content: "done."})
	if len(rows) != 1 || !refusedRow(rows[0]) {
		t.Fatalf("the app's agent deleting another conversation's ask: %v", rows)
	}
	if row, err := r.ds.loadRowDB(context.Background(), eref{Kind: vocabulary.KindLLMInteraction, ID: other}); err != nil || row == nil || row.DeletedAt != nil {
		t.Fatalf("the other conversation's ask after the app's agent: %v %v", row, err)
	}
}

// A call an app's turn left waiting for approval is that turn's work, read
// back from the records after a restart: held to the app's grant, which does
// not write providers, though the admin approved it.
func TestAnApprovedCallIsHeldAfterARestartToTheAppThatDroveItsTurn(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	actor, ctx, _ := r.app(t, "writer.example.org", "agents:"+r.pkg+"/*", "write:"+r.note)
	thread, id := r.approvalOf(t, ctx, actor, "setter", "setprovider")
	r.r.restart()
	d, err := r.r.s.Dataset(context.Background(), r.ada)
	if err != nil {
		t.Fatal(err)
	}
	r.ds = d.(*dataset)
	r.ds.mu.Lock()
	r.ds.skipThreadTitles = true
	r.ds.mu.Unlock()
	if state := r.approve(t, r.person(r.adaID, substrate.ActorPerson), thread, id); state != approvalFailed {
		t.Fatalf("the call the app's turn left, approved by the admin, ended %s, want failed", state)
	}
	if row, _ := r.ds.loadRowDB(context.Background(), eref{Kind: typeProvider, ID: "set-by-a-call"}); row != nil {
		t.Fatal("the call the app's turn left wrote a provider")
	}
}

// An app's token revoked between two turns of its work refuses the later
// one: the call its first turn left, approved before the revoke, writes; the
// call its second turn left, approved after, writes nothing.
func TestATokenRevokedBetweenTwoTurnsRefusesTheLater(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	ada := r.person(r.adaID, substrate.ActorPerson)
	actor, ctx, token := r.app(t, "writer.example.org", "agents:"+r.pkg+"/*", "write:"+r.note)
	thread, first := r.approvalOf(t, ctx, actor, "saver", "savenote")
	if state := r.approve(t, ada, thread, first); state != approvalDone {
		t.Fatalf("the first turn's call ended %s, want done", state)
	}
	thread, second := r.approvalOf(t, ctx, actor, "saver", "savenote")
	if _, err := r.ds.Delete(r.as(r.adaID), substrate.ActorPerson, kindToken, token, substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	if state := r.approve(t, ada, thread, second); state != approvalFailed {
		t.Fatalf("the second turn's call, approved after the revoke, ended %s, want failed", state)
	}
}

// A sub-agent's thread is held through its parent's turn: the app that
// drove the parent's turn holds the sub-agent's writes, and the sub-agent's
// thread carries that turn's token in its own `heldTo`.
func TestASubagentsThreadIsHeldThroughItsParent(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	actor, ctx, token := r.app(t, "reader.example.org", "agents:"+r.pkg+"/*", "read:"+r.note)
	r.fake.script("scribe",
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", r.note, "", map[string]any{"name": "x"})}}},
		fakeTurn{content: "tried."})
	parent, _ := r.chat(t, ctx, actor, "lead", "",
		fakeTurn{calls: []fakeCall{{"scribe", toolArgs(t, map[string]any{"input": "write a note"})}}}, fakeTurn{content: "done."})
	if r.notes(t) != 0 {
		t.Fatal("the sub-agent of an app's turn wrote a note the app's grant lacks")
	}
	var child string
	if err := r.ds.db.QueryRowContext(context.Background(), `SELECT id FROM records WHERE kind = $1 AND id <> $2 AND deleted_at IS NULL ORDER BY created_at DESC LIMIT 1`,
		typeThread, parent).Scan(&child); err != nil {
		t.Fatalf("the sub-agent's thread: %v", err)
	}
	held, err := r.ds.heldBy(context.Background(), typeThread, child)
	if got := runTokens(held); err != nil || !slices.Contains(got, token) {
		t.Fatalf("the sub-agent's thread is held to %v (%v), want the app's %q", got, err, token)
	}
	if rows := toolRows(t, r.ds, child); len(rows) == 0 || !slices.ContainsFunc(rows, refusedRow) {
		t.Fatalf("the sub-agent's write was not refused: %v", rows)
	}
}
