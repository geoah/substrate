package engine

// Whose rules hold a run's work, stored on its own records (heldto.go,
// decision 0276): every record a run writes carries its `heldTo`, a thread's
// is its turn's, a continuation adds its hand and keeps the holders before,
// work reads its own item's and never the thread's or a history's, and an
// item from before the release is refused and asked again.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// holdersIn is the stored `heldTo` of kind/id; the test fails when it has none.
func holdersIn(t *testing.T, ds *dataset, kind, id string) storedHolders {
	t.Helper()
	row, err := ds.loadRowDB(context.Background(), eref{Kind: kind, ID: id})
	if err != nil || row == nil {
		t.Fatalf("read %s %s: %v", kind, id, err)
	}
	h, ok := storedHoldersOf(row.Props[propHeldTo])
	if !ok {
		t.Fatalf("%s %s carries no %s: %v", kind, id, propHeldTo, row.Props)
	}
	return h
}

// heldMessages are the ids of thread's messages, each with whether it
// carries a `heldTo`.
func heldMessages(t *testing.T, ds *dataset, thread string) map[string]bool {
	t.Helper()
	rows, err := ds.db.QueryContext(context.Background(), `SELECT id, props ? 'heldTo' FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND `+referencePathSQL("props", msgRelThread)+` = $2`,
		typeMessage, vocabulary.RecordPath(typeThread, thread))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		var held bool
		if err := rows.Scan(&id, &held); err != nil {
			t.Fatal(err)
		}
		out[id] = held
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// threadSays reports whether a message of thread carries text.
func threadSays(t *testing.T, ds *dataset, thread, text string) bool {
	t.Helper()
	var found bool
	if err := ds.db.QueryRowContext(context.Background(), `SELECT EXISTS (SELECT 1 FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND `+referencePathSQL("props", msgRelThread)+` = $2
		  AND props->>'content' LIKE $3)`,
		typeMessage, vocabulary.RecordPath(typeThread, thread), "%"+text+"%").Scan(&found); err != nil {
		t.Fatal(err)
	}
	return found
}

// An app's turn stamps the person it acts for and its token on the thread,
// every message and the approval it leaves; a person who approves is added
// to the approval and the holders before stay; and the person's own next
// turn takes the thread over, with no token.
func TestARunsRecordsCarryWhoseRulesHoldTheirWork(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	actor, ctx, token := r.app(t, "writer.example.org", "agents:"+r.pkg+"/*", "write:"+r.note)
	thread, id := r.approvalOf(t, ctx, actor, "setter", "setprovider")
	ada, bo := actorID(substrate.ActorPerson), actorID(substrate.PersonActor(r.boID))
	for what, h := range map[string]storedHolders{
		"the thread":   holdersIn(t, r.ds, typeThread, thread),
		"the approval": holdersIn(t, r.ds, vocabulary.KindLLMApproval, id),
	} {
		if h.onBehalfOf != ada || !slices.Equal(h.tokens, []string{token}) || len(h.continuedBy) != 0 {
			t.Errorf("%s of the app's turn is held to %+v, want ada and the app's token", what, h)
		}
	}
	for msg, held := range heldMessages(t, r.ds, thread) {
		if !held {
			t.Errorf("message %s of the app's turn carries no %s", msg, propHeldTo)
		}
	}

	r.approve(t, r.person(r.boID, substrate.PersonActor(r.boID)), thread, id)
	if h := holdersIn(t, r.ds, vocabulary.KindLLMApproval, id); h.onBehalfOf != ada || !slices.Equal(h.continuedBy, []string{bo}) || !slices.Equal(h.tokens, []string{token}) {
		t.Fatalf("the approval bo approved is held to %+v, want ada, the app's token and bo", h)
	}

	r.chat(t, r.person(r.adaID, substrate.ActorPerson), substrate.ActorPerson, "setter", thread, fakeTurn{content: "hello."})
	if h := holdersIn(t, r.ds, typeThread, thread); h.onBehalfOf != ada || len(h.tokens) != 0 || len(h.continuedBy) != 0 {
		t.Fatalf("the thread after ada's own turn is held to %+v, want ada alone", h)
	}
}

// Work whose item was written before the release, when nothing recorded who
// held it, is refused: the call does not run, the conversation is told to
// ask again, and its report starts no resume that would act for nobody.
func TestWorkAskedBeforeTheReleaseIsRefusedAndAskedAgain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRunsRepository(t)
	ada := r.person(r.adaID, substrate.ActorPerson)
	r.fake.script("saver", fakeTurn{calls: []fakeCall{{"savenote", `{}`}}}, fakeTurn{content: "it waits."})
	res, err := r.ds.ChatAgent(ada, substrate.ActorPerson, r.pkg+"/saver", substrate.ChatInput{Message: "go"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	held := approvalsOf(t, r.ds, res.Thread)
	if len(held) != 1 {
		t.Fatalf("approvals = %d, want 1", len(held))
	}
	id := held[0].ID
	if _, err := r.ds.db.ExecContext(ctx, `UPDATE records SET props = props - 'heldTo' WHERE kind = $1 AND id = $2`, vocabulary.KindLLMApproval, id); err != nil {
		t.Fatal(err)
	}
	version := approvalRow(t, r.ds, id).Version
	if _, err := r.ds.Patch(ada, substrate.ActorPerson, vocabulary.KindLLMApproval, id, substrate.PatchInput{
		Properties: map[string]any{propApprovalState: approvalApproved}, IfVersion: &version,
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the refused call's end", func() bool {
		return approvalRow(t, r.ds, id).States[propApprovalState] == approvalFailed
	})
	if r.notes(t) != 0 {
		t.Fatal("the call asked before the release ran")
	}
	waitFor(t, "the report of the refusal", func() bool { return threadSays(t, r.ds, res.Thread, "ask again") })
	for msg, carries := range heldMessages(t, r.ds, res.Thread) {
		if row, _ := r.ds.loadRowDB(ctx, eref{Kind: typeMessage, ID: msg}); carries && strings.Contains(fmt.Sprint(row.Props["content"]), "ask again") {
			t.Fatalf("the report of the refused call carries holders: %v", row.Props[propHeldTo])
		}
	}
	if n := len(r.fake.requestsOf("saver")); n != 2 {
		t.Fatalf("the model was called %d times, want 2: the report resumed the conversation", n)
	}
}

// reportRow is the system row of thread whose content carries text, and its
// stored `heldTo` (nil where it has none); the test fails while there is
// none.
func reportRow(t *testing.T, ds *dataset, thread, text string) (string, map[string]any) {
	t.Helper()
	var id string
	var held []byte
	waitFor(t, "the report "+text, func() bool {
		err := ds.db.QueryRowContext(context.Background(), `SELECT id, props->'heldTo' FROM records
			WHERE kind = $1 AND deleted_at IS NULL AND `+referencePathSQL("props", msgRelThread)+` = $2
			  AND props->>'role' = $3 AND props->>'content' LIKE $4 ORDER BY created_at DESC LIMIT 1`,
			typeMessage, vocabulary.RecordPath(typeThread, thread), msgRoleSystem, "%"+text+"%").Scan(&id, &held)
		return err == nil
	})
	var out map[string]any
	if len(held) > 0 {
		if err := json.Unmarshal(held, &out); err != nil {
			t.Fatal(err)
		}
	}
	return id, out
}

// An ask from before the release, answered or dismissed, is reported with
// no holders, the answer's hand's never among them: the report says to ask
// again, and no resume runs from it.
func TestAnAskFromBeforeTheReleaseResumesNothing(t *testing.T) {
	t.Parallel()
	for _, state := range []string{interactionAnswered, interactionDismissed} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			r := newRunsRepository(t)
			ada := r.person(r.adaID, substrate.ActorPerson)
			thread, _ := r.chat(t, ada, substrate.ActorPerson, "scribe", "",
				fakeTurn{calls: []fakeCall{{"ask", runsAsk}}}, fakeTurn{content: "asked."})
			page, err := r.ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{vocabulary.KindLLMInteraction}}, First: 10})
			if err != nil || len(page.Records) != 1 {
				t.Fatalf("the ask: %v %v", page, err)
			}
			ask := page.Records[0]
			if _, err := r.ds.db.ExecContext(ctx, `UPDATE records SET props = props - 'heldTo' WHERE kind = $1 AND id = $2`, vocabulary.KindLLMInteraction, ask.ID); err != nil {
				t.Fatal(err)
			}
			props := map[string]any{"state": state}
			if state == interactionAnswered {
				props["answers"] = []any{map[string]any{"question": "sure", "selected": []any{"yes"}}}
			}
			if _, err := r.ds.Patch(ada, substrate.ActorPerson, vocabulary.KindLLMInteraction, ask.ID, substrate.PatchInput{Properties: props, IfVersion: &ask.Version}); err != nil {
				t.Fatalf("ada %s the ask: %v", state, err)
			}
			if _, held := reportRow(t, r.ds, thread, "ask again"); held != nil {
				t.Fatalf("the report of an ask from before the release carries holders: %v", held)
			}
			time.Sleep(300 * time.Millisecond)
			if n := len(r.fake.requestsOf("scribe")); n != 2 {
				t.Fatalf("the model was called %d times, want 2: the report resumed the conversation", n)
			}
		})
	}
}

// A change request's report carries the request's holders, whom the line
// that made it names and the hand that decided it as one who continued it,
// never the deciding transaction's own: an editor's acceptance of ada's
// agent's proposal resumes ada's work, with the editor added.
func TestAChangeRequestsReportCarriesTheRequestsHolders(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRunsRepository(t)
	ada := r.person(r.adaID, substrate.ActorPerson)
	if _, err := r.ds.Put(ada, substrate.ActorPerson, substrate.PutInput{Kind: r.note, ID: fid("n1"), Properties: map[string]any{"name": "first"}}); err != nil {
		t.Fatal(err)
	}
	thread, _ := r.chat(t, ada, substrate.ActorPerson, "proposer", "",
		fakeTurn{calls: []fakeCall{{"propose", toolArgs(t, map[string]any{
			"kind": r.note, "target": fid("n1"), "diff": map[string]any{"properties": map[string]any{"name": "better"}}, "rationale": "tidy",
		})}}}, fakeTurn{content: "proposed."})
	req := onlyPatchRequest(t, r.ds)
	r.fake.script("proposer", fakeTurn{content: "done."})
	bo := substrate.PersonActor(r.boID)
	if _, err := r.ds.Patch(r.person(r.boID, bo), bo, vocabulary.KindRecordPatchRequest, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": decisionAccepted}, IfVersion: &req.Version,
	}); err != nil {
		t.Fatalf("bo accepts ada's agent's proposal: %v", err)
	}
	_, held := reportRow(t, r.ds, thread, "proposalDecision")
	got, ok := storedHoldersOf(held)
	if !ok || got.onBehalfOf != actorID(substrate.ActorPerson) || !slices.Equal(got.continuedBy, []string{actorID(bo)}) {
		t.Fatalf("the decision's report is held to %v, want ada, with bo who decided it", held)
	}
	waitFor(t, "the resume after the decision", func() bool {
		_, props := threadStatusOf(t, r.ds, thread)
		return props[propThreadLastReply] == "done."
	})
	_ = ctx
}

// Work nobody asked for, a trigger's or a function's, is held to nobody: its
// records carry an empty `heldTo`, which binds no person and no token.
func TestWorkNobodyAskedForIsHeldToNobody(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRunsRepository(t)
	if _, err := r.ds.Put(ctx, substrate.ActorSystem, substrate.PutInput{Kind: typeThread, ID: "nobodys", Properties: map[string]any{
		"agent": map[string]any{vocabulary.ReferenceValueKey: vocabulary.RecordPath(kindAgent, r.pkg+"/scribe")}, "subject": "nobody's",
		"mode": "chat", "status": "ok", "provider": "runsllm", "model": "scribe", "agentDepth": 0, "startedAt": "2026-01-01T00:00:00Z",
	}}); err != nil {
		t.Fatal(err)
	}
	if h := holdersIn(t, r.ds, typeThread, "nobodys"); h.onBehalfOf != "" || len(h.continuedBy) != 0 || len(h.tokens) != 0 {
		t.Fatalf("a thread nobody asked for is held to %+v, want nobody", h)
	}
	held, err := r.ds.heldBy(ctx, typeThread, "nobodys")
	if err != nil || substrate.OnBehalfOfFrom(held) != "" || len(heldPersons(held)) != 0 || len(runTokens(held)) != 0 {
		t.Fatalf("work held by a thread nobody asked for acts for %q, %v, %v (%v), want nobody",
			substrate.OnBehalfOfFrom(held), heldPersons(held), runTokens(held), err)
	}
}

// An app's token that answered an ask is a holder of the work the answer
// continues: revoked while that work runs, it refuses what is left of it.
func TestAContinuersTokenRevokedLaterRefusesTheContinuedWork(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	ada := r.person(r.adaID, substrate.ActorPerson)
	arrived, release := make(chan struct{}), make(chan struct{})
	r.chat(t, ada, substrate.ActorPerson, "scribe", "",
		fakeTurn{calls: []fakeCall{{"ask", runsAsk}}}, fakeTurn{content: "asked."})
	page, err := r.ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{Kinds: []string{vocabulary.KindLLMInteraction}}, First: 10})
	if err != nil || len(page.Records) != 1 {
		t.Fatalf("the ask: %v %v", page, err)
	}
	ask := page.Records[0]
	thread := referenceID(ask.Properties[msgRelThread])
	r.fake.script("scribe",
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", r.note, "", map[string]any{"name": "x"})}}, arrived: arrived, release: release},
		fakeTurn{content: "done."})
	actor, actx, token := r.app(t, "writer.example.org", "agents:"+r.pkg+"/*", "write:"+r.note)
	if _, err := r.ds.Patch(actx, actor, vocabulary.KindLLMInteraction, ask.ID, substrate.PatchInput{
		Properties: map[string]any{"state": interactionAnswered, "answers": []any{map[string]any{"question": "sure", "selected": []any{"yes"}}}},
		IfVersion:  &ask.Version,
	}); err != nil {
		t.Fatalf("the app answers the ask: %v", err)
	}
	if h := holdersIn(t, r.ds, vocabulary.KindLLMInteraction, ask.ID); !slices.Contains(h.tokens, token) {
		t.Fatalf("the answered ask is held to %+v, want the app's token among them", h)
	}
	<-arrived
	if _, err := r.ds.Delete(r.as(r.adaID), substrate.ActorPerson, kindToken, token, substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	close(release)
	waitFor(t, "the continued turn's end", func() bool {
		_, props := threadStatusOf(t, r.ds, thread)
		return props[propThreadLastReply] == "done."
	})
	if row := lastTool(t, r.ds, thread); row["ok"] != false || !strings.Contains(fmt.Sprint(row["content"]), "revoked") || r.notes(t) != 0 {
		t.Fatalf("the continued work after the answering app's token was revoked: %v", row)
	}
}

// A call an app's turn left waiting is the app's work, whoever spoke in the
// thread since: a person's own turn between the turn and the approval does
// not lend it the person's rules. The other order of hands holds the same
// way: a call a person's turn left stays the person's after an app's turn.
func TestAnApprovalIsHeldToTheTurnThatAskedNotTheThreadsLatest(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	ada := r.person(r.adaID, substrate.ActorPerson)
	actor, actx, _ := r.app(t, "reader.example.org", "agents:"+r.pkg+"/*", "read:"+r.note)
	leave := func(ctx context.Context, hand substrate.Actor) (string, string) {
		t.Helper()
		thread, _ := r.chat(t, ctx, hand, "setter", "", fakeTurn{calls: []fakeCall{{"setprovider", `{}`}}}, fakeTurn{content: "it waits."})
		held := approvalsOf(t, r.ds, thread)
		if len(held) != 1 {
			t.Fatalf("approvals = %d, want 1", len(held))
		}
		return thread, held[0].ID
	}

	thread, id := leave(actx, actor)
	r.chat(t, ada, substrate.ActorPerson, "setter", thread, fakeTurn{content: "hello."})
	r.fake.script("setter", fakeTurn{content: "done."})
	if state := r.approve(t, ada, thread, id); state != approvalFailed {
		t.Fatalf("the app's call, approved after ada's own turn, ended %s, want failed", state)
	}

	thread, id = leave(ada, substrate.ActorPerson)
	r.chat(t, actx, actor, "setter", thread, fakeTurn{content: "hello."})
	r.fake.script("setter", fakeTurn{content: "done."})
	if state := r.approve(t, ada, thread, id); state != approvalDone {
		t.Fatalf("ada's call, approved after the app's turn, ended %s, want done", state)
	}
}

// A call left waiting in a thread a function started, with nobody behind
// it, is held to whoever approves it alone.
func TestAFunctionStartedThreadsApprovalIsHeldToItsApprover(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRunsRepository(t)
	run := func() (string, string) {
		t.Helper()
		r.fake.script("setter", fakeTurn{calls: []fakeCall{{"setprovider", `{}`}}}, fakeTurn{content: "it waits."}, fakeTurn{content: "done."})
		if _, _, err := r.ds.CallFunction(ctx, substrate.ActorSystem, r.pkg+"/starter", nil); err != nil {
			t.Fatalf("the function: %v", err)
		}
		threads := threadsOfAgent(t, r.ds, r.pkg+"/setter")
		thread := threads[len(threads)-1]
		held := approvalsOf(t, r.ds, thread)
		if len(held) != 1 {
			t.Fatalf("approvals of the function's thread = %d, want 1", len(held))
		}
		if h := holdersIn(t, r.ds, vocabulary.KindLLMApproval, held[0].ID); h.onBehalfOf != "" || len(h.tokens) != 0 {
			t.Fatalf("the function's approval is held to %+v, want nobody", h)
		}
		return thread, held[0].ID
	}
	thread, id := run()
	if state := r.approve(t, r.person(r.boID, substrate.PersonActor(r.boID)), thread, id); state != approvalFailed {
		t.Fatalf("the function's call an editor approved ended %s, want failed", state)
	}
	thread, id = run()
	if state := r.approve(t, r.person(r.adaID, substrate.ActorPerson), thread, id); state != approvalDone {
		t.Fatalf("the function's call the admin approved ended %s, want done", state)
	}
}

// Nothing is searched for the turn a call belongs to: an app's call stays
// the app's after more than five hundred messages in its thread.
func TestAnApprovalIsHeldPastFiveHundredMessages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRunsRepository(t)
	actor, actx, _ := r.app(t, "reader.example.org", "agents:"+r.pkg+"/*", "read:"+r.note)
	thread, id := r.approvalOf(t, actx, actor, "setter", "setprovider")
	for i := range 501 {
		if _, err := r.ds.Put(ctx, substrate.ActorSystem, substrate.PutInput{Kind: typeMessage, ID: fid(fmt.Sprintf("filler-%d", i)), Properties: map[string]any{
			"role": "assistant", "content": "filler", "turn": 1000 + i,
			msgRelThread: map[string]any{vocabulary.ReferenceValueKey: vocabulary.RecordPath(typeThread, thread)},
		}}); err != nil {
			t.Fatal(err)
		}
	}
	if state := r.approve(t, r.person(r.adaID, substrate.ActorPerson), thread, id); state != approvalFailed {
		t.Fatalf("the app's call, approved past 501 messages, ended %s, want failed", state)
	}
}

// A sub-agent's call is its parent turn's work: approved, in the root's
// conversation, after the root's next turn, which is ada's, it is still held
// to the app that drove the parent's.
func TestASubagentsApprovalAfterTheRootsNextTurnIsHeldToTheParentsTurn(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	ada := r.person(r.adaID, substrate.ActorPerson)
	actor, actx, token := r.app(t, "reader.example.org", "agents:"+r.pkg+"/*", "read:"+r.note)
	r.fake.script("setter", fakeTurn{calls: []fakeCall{{"setprovider", `{}`}}}, fakeTurn{content: "it waits."})
	root, _ := r.chat(t, actx, actor, "delegator", "",
		fakeTurn{calls: []fakeCall{{"setter", toolArgs(t, map[string]any{"input": "set it"})}}}, fakeTurn{content: "delegated."})
	held := approvalsOf(t, r.ds, root)
	if len(held) != 1 {
		t.Fatalf("approvals of the sub-agent's call = %d, want 1", len(held))
	}
	r.chat(t, ada, substrate.ActorPerson, "delegator", root, fakeTurn{content: "hello."})
	if h := holdersIn(t, r.ds, typeThread, root); len(h.tokens) != 0 {
		t.Fatalf("the root after ada's turn is held to %+v, want ada alone", h)
	}
	if h := holdersIn(t, r.ds, vocabulary.KindLLMApproval, held[0].ID); !slices.Contains(h.tokens, token) {
		t.Fatalf("the sub-agent's approval is held to %+v, want the app's token", h)
	}
	r.fake.script("delegator", fakeTurn{content: "done."})
	if state := r.approve(t, ada, root, held[0].ID); state != approvalFailed {
		t.Fatalf("the sub-agent's call, approved after the root's next turn, ended %s, want failed", state)
	}
}

// A background job is its turn's work however long it runs: held before it
// reads whose rules hold it, while two more turns of the person claim the
// thread, it reads its own tool row's, the app's turn's, and the person's
// turns do not lend it their rules.
func TestAJobOutlivingTwoMoreTurnsIsHeldToItsTurn(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	ada := r.person(r.adaID, substrate.ActorPerson)
	actor, actx, _ := r.app(t, "reader.example.org", "agents:"+r.pkg+"/*", "read:"+r.note)
	entered, hold := make(chan struct{}), make(chan struct{})
	var once sync.Once
	r.ds.beforeJob = func() {
		once.Do(func() { close(entered) })
		<-hold
	}
	r.fake.script("digger",
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", r.note, "", map[string]any{"name": "dug"})}}},
		fakeTurn{content: "dug."})
	thread, _ := r.chat(t, actx, actor, "boss", "",
		fakeTurn{calls: []fakeCall{{"digger", toolArgs(t, map[string]any{"input": "dig"})}}}, fakeTurn{content: "started."})
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the job never started")
	}
	r.fake.script("boss", fakeTurn{content: "one."}, fakeTurn{content: "two."}, fakeTurn{content: "the dig is done."})
	r.chat(t, ada, substrate.ActorPerson, "boss", thread)
	r.chat(t, ada, substrate.ActorPerson, "boss", thread)
	if h := holdersIn(t, r.ds, typeThread, thread); len(h.tokens) != 0 {
		t.Fatalf("ada's turns did not claim the thread: %+v", h)
	}
	close(hold)
	waitFor(t, "the resume after the job", func() bool {
		_, props := threadStatusOf(t, r.ds, thread)
		return props[propThreadLastReply] == "the dig is done."
	})
	diggers := threadsOfAgent(t, r.ds, r.pkg+"/digger")
	if len(diggers) != 1 {
		t.Fatalf("the job's threads = %v", diggers)
	}
	if row := lastTool(t, r.ds, diggers[0]); !refusedRow(row) || r.notes(t) != 0 {
		t.Fatalf("the job's write after two of ada's turns: %v", row)
	}
}

// An actor a piece of work is held to that cannot be read leaves the work
// where it is: the open's settlement of interrupted approvals errs rather
// than settle it for nobody, and the next pass settles it.
func TestAnActorLookupThatFailsDuringRecoverySettlesNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRunsRepository(t)
	bo := r.person(r.boID, substrate.PersonActor(r.boID))
	thread, id := r.approvalOf(t, bo, substrate.PersonActor(r.boID), "saver", "savenote")
	if _, err := r.ds.db.ExecContext(ctx, `UPDATE records SET states = jsonb_set(states, '{state}', '"running"') WHERE kind = $1 AND id = $2`,
		vocabulary.KindLLMApproval, id); err != nil {
		t.Fatal(err)
	}
	lookup := errors.New("the database did not answer")
	r.r.failRules(lookup)
	if err := r.ds.settleInterruptedApprovals(ctx); !errors.Is(err, lookup) {
		t.Fatalf("the settlement with the actor unread: %v, want the lookup's error", err)
	}
	if row := approvalRow(t, r.ds, id); row.Props["reportedAt"] != nil {
		t.Fatalf("the approval was settled while its actor could not be read: %v", row.Props)
	}
	r.r.failRules(nil)
	if err := r.ds.settleInterruptedApprovals(ctx); err != nil {
		t.Fatal(err)
	}
	if row := approvalRow(t, r.ds, id); row.Props["reportedAt"] == nil || row.States[propApprovalState] != approvalFailed {
		t.Fatalf("the next pass's settlement: %v %v", row.States, row.Props)
	}
	_ = thread
}

// A person or an app stops only a run it could have started: an editor
// stops no turn of an agent that reads providers.
func TestAStopIsAContinuationOfTheRun(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	thread, _ := r.chat(t, r.person(r.adaID, substrate.ActorPerson), substrate.ActorPerson, "keeper", "",
		fakeTurn{calls: []fakeCall{{"ask", runsAsk}}}, fakeTurn{content: "asked."})
	if _, err := r.ds.StopAgentTurn(r.person(r.boID, substrate.PersonActor(r.boID)), substrate.PersonActor(r.boID), thread); !errors.Is(err, substrate.ErrNotPermitted) {
		t.Fatalf("an editor stopping a run they could not start: %v, want refused by a rule", err)
	}
	if _, err := r.ds.StopAgentTurn(r.person(r.adaID, substrate.ActorPerson), substrate.ActorPerson, thread); errors.Is(err, substrate.ErrNotPermitted) {
		t.Fatalf("the admin stopping their own run: %v", err)
	}
}

// Whether a proposal's target exists is a read: an agent that may not read
// providers learns nothing of one, there or not.
func TestAProposalProbesNothingItsAgentMayNotRead(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	ada := r.person(r.adaID, substrate.ActorPerson)
	propose := func(target string) fakeCall {
		return fakeCall{"propose", toolArgs(t, map[string]any{
			"kind": typeProvider, "target": target, "diff": map[string]any{"properties": map[string]any{"wire": "anthropic"}}, "rationale": "probe",
		})}
	}
	_, rows := r.chat(t, ada, substrate.ActorPerson, "prober", "",
		fakeTurn{calls: []fakeCall{propose("runsllm"), propose("no-such-provider")}}, fakeTurn{content: "done."})
	if len(rows) != 2 {
		t.Fatalf("tool rows = %v", rows)
	}
	for _, row := range rows {
		if content := fmt.Sprint(row["content"]); row["ok"] != false || !strings.Contains(content, "not in the reads allowlist") {
			t.Fatalf("a proposal on a provider the agent may not read answered %v", row)
		}
	}
}

// A read that resolves to no kind reads nothing, never every kind: a query
// naming no kind, made for an app whose grant reads no kind the repository
// declares, gets no record, where the person's own lists them.
func TestAnEmptyScopeReadsNothing(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	if _, err := r.ds.Put(r.as(r.adaID), substrate.ActorPerson, substrate.PutInput{Kind: r.note, ID: fid("seen"), Properties: map[string]any{"name": "seen"}}); err != nil {
		t.Fatal(err)
	}
	actor, actx, _ := r.app(t, "blind.example.org", "read:"+r.pkg+"/nothing")
	out, _, err := r.ds.CallFunction(actx, actor, vocabulary.HostFunctionQuery, map[string]any{})
	if err != nil || strings.Contains(fmt.Sprint(out), "seen") {
		t.Fatalf("a query for an app that reads no declared kind: %v %v, want no record", out, err)
	}
	out, _, err = r.ds.CallFunction(r.person(r.adaID, substrate.ActorPerson), substrate.ActorPerson, vocabulary.HostFunctionQuery, map[string]any{})
	if err != nil || !strings.Contains(fmt.Sprint(out), "seen") {
		t.Fatalf("the person's own query naming no kind: %v %v, want the note among the records", out, err)
	}
}

// The answer to an app's ask resumes the app's work: the person who answers
// adds their rules to it, and the app's grant still holds the resumed turn,
// so the write the app may not make is refused though the person may.
func TestAnAnswerResumesTheAskersWork(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	actor, actx, token := r.app(t, "reader.example.org", "agents:"+r.pkg+"/*", "read:"+r.note)
	thread, _ := r.chat(t, actx, actor, "scribe", "",
		fakeTurn{calls: []fakeCall{{"ask", runsAsk}}}, fakeTurn{content: "asked."})
	page, err := r.ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{Kinds: []string{vocabulary.KindLLMInteraction}}, First: 10})
	if err != nil || len(page.Records) != 1 {
		t.Fatalf("the ask: %v %v", page, err)
	}
	ask := page.Records[0]
	r.fake.script("scribe",
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", r.note, "", map[string]any{"name": "x"})}}},
		fakeTurn{content: "done."})
	if _, err := r.ds.Patch(r.person(r.adaID, substrate.ActorPerson), substrate.ActorPerson, vocabulary.KindLLMInteraction, ask.ID, substrate.PatchInput{
		Properties: map[string]any{"state": interactionAnswered, "answers": []any{map[string]any{"question": "sure", "selected": []any{"yes"}}}},
		IfVersion:  &ask.Version,
	}); err != nil {
		t.Fatalf("ada answers the app's ask: %v", err)
	}
	waitFor(t, "the resumed turn's end", func() bool {
		_, props := threadStatusOf(t, r.ds, thread)
		return props[propThreadLastReply] == "done."
	})
	if row := lastTool(t, r.ds, thread); !refusedRow(row) || r.notes(t) != 0 {
		t.Fatalf("the turn ada's answer resumed wrote what the app may not: %v", row)
	}
	if h := holdersIn(t, r.ds, vocabulary.KindLLMInteraction, ask.ID); !slices.Contains(h.tokens, token) {
		t.Fatalf("the answered ask is held to %+v, want the app's token kept", h)
	}
}

// A pinned block is replayed only while what its holders may read is what it
// was when the block was taken: bo, an admin, starts a thread whose agent
// pins the providers, and made an editor, bo's own next turn takes the block
// again, without the providers an editor does not read.
func TestAPinnedBlockIsTakenAgainWhenItsHoldersRolesChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRunsRepository(t)
	if _, err := r.ds.Put(r.as(r.adaID), substrate.ActorPerson, substrate.PutInput{Kind: typeProvider, ID: "pinned-provider", Properties: map[string]any{
		"wire": "openai", "baseURL": "https://pinned.example.com", "apiKey": "row-key-pinned",
	}}); err != nil {
		t.Fatal(err)
	}
	bo := substrate.PersonActor(r.boID)
	if err := r.r.s.SetPersonRoles(ctx, r.bo, r.ada, scope.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	thread, _ := r.chat(t, r.person(r.boID, bo), bo, "pinner", "", fakeTurn{content: "first."})
	if reqs := r.fake.requestsOf("pinner"); len(reqs) != 1 || !requestCarries(reqs[0], "pinned.example.com") {
		t.Fatal("the admin's turn did not pin the provider: the test proves nothing")
	}
	if err := r.r.s.SetPersonRoles(ctx, r.bo, r.ada, scope.RoleEditor); err != nil {
		t.Fatal(err)
	}
	r.chat(t, r.person(r.boID, bo), bo, "pinner", thread, fakeTurn{content: "second."})
	reqs := r.fake.requestsOf("pinner")
	if len(reqs) != 2 || requestCarries(reqs[1], "pinned.example.com") {
		t.Fatal("the editor's turn replayed the block the admin's turn took, providers and all")
	}
}

// A thread's title is its first turn's work, held to that turn's holders
// from when it is queued: a second person's turn that claims the thread
// before the title runs does not lend it their rules.
func TestATitleIsHeldToItsFirstTurnNotTheThreadsNext(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRunsRepository(t)
	entered, hold := make(chan struct{}), make(chan struct{})
	var once sync.Once
	r.ds.mu.Lock()
	r.ds.skipThreadTitles = false
	r.ds.mu.Unlock()
	r.ds.beforeTitle = func() {
		once.Do(func() { close(entered) })
		<-hold
	}
	thread, _ := r.chat(t, r.person(r.adaID, substrate.ActorPerson), substrate.ActorPerson, "scribe", "", fakeTurn{content: "first."})
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the title never queued")
	}
	bo := substrate.PersonActor(r.boID)
	r.chat(t, r.person(r.boID, bo), bo, "scribe", thread, fakeTurn{content: "second."})
	if h := holdersIn(t, r.ds, typeThread, thread); h.onBehalfOf != actorID(bo) {
		t.Fatalf("bo's turn did not claim the thread: %+v", h)
	}
	r.fake.script("scribe", fakeTurn{content: "A title"})
	close(hold)
	var behalf string
	waitFor(t, "the title", func() bool {
		err := r.ds.db.QueryRowContext(ctx, `SELECT coalesce(on_behalf_of, '') FROM changelog
			WHERE kind = $1 AND record_id = $2 AND payload::text LIKE '%A title%' ORDER BY seq DESC LIMIT 1`, typeThread, thread).Scan(&behalf)
		return err == nil
	})
	if behalf != actorID(substrate.ActorPerson) {
		t.Fatalf("the title was written for %q, want ada's first turn's %q", behalf, actorID(substrate.ActorPerson))
	}
}

// pinnedAppAsk is an app's turn of the note pinner, which asks: its thread,
// its ask, and the app's token. The block it pinned carries the note named
// text.
func (r *runsRepository) pinnedAppAsk(t *testing.T, text string) (string, *substrate.Record, string) {
	t.Helper()
	if _, err := r.ds.Put(r.adaCtx(), substrate.ActorPerson, substrate.PutInput{Kind: r.note, ID: fid("pinned"), Properties: map[string]any{"name": text}}); err != nil {
		t.Fatal(err)
	}
	actor, actx, token := r.app(t, "reader.example.org", "agents:"+r.pkg+"/*", "read:"+r.note)
	thread, _ := r.chat(t, actx, actor, "notepinner", "", fakeTurn{calls: []fakeCall{{"ask", runsAsk}}}, fakeTurn{content: "asked."})
	if reqs := r.fake.requestsOf("notepinner"); len(reqs) == 0 || !requestCarries(reqs[0], text) {
		t.Fatal("the app's turn did not pin the note: the test proves nothing")
	}
	page, err := r.ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{Kinds: []string{vocabulary.KindLLMInteraction}}, First: 10})
	if err != nil || len(page.Records) != 1 {
		t.Fatalf("the ask: %v %v", page, err)
	}
	return thread, page.Records[0], token
}

// answerAndResume has ada answer ask, and answers the model requests the
// resume it starts makes, after the wait.
func (r *runsRepository) answerAndResume(t *testing.T, ask *substrate.Record) []map[string]any {
	t.Helper()
	before := len(r.fake.requestsOf("notepinner"))
	r.fake.script("notepinner", fakeTurn{content: "resumed."})
	if _, err := r.ds.Patch(r.adaCtx(), substrate.ActorPerson, vocabulary.KindLLMInteraction, ask.ID, substrate.PatchInput{
		Properties: map[string]any{"state": interactionAnswered, "answers": []any{map[string]any{"question": "sure", "selected": []any{"yes"}}}},
		IfVersion:  &ask.Version,
	}); err != nil {
		t.Fatalf("ada answers the ask: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && len(r.fake.requestsOf("notepinner")) == before {
		time.Sleep(50 * time.Millisecond)
	}
	return r.fake.requestsOf("notepinner")[before:]
}

// The stored pinned block is a read, asked again before each replay: the
// app's token whose work took it, revoked before the answer's resume,
// replays nothing, and no model request of the resume carries the block.
func TestAPinnedBlockReachesNoModelAfterItsTokenIsRevoked(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	_, ask, token := r.pinnedAppAsk(t, "pinned-for-the-app")
	if _, err := r.ds.Delete(r.adaCtx(), substrate.ActorPerson, kindToken, token, substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	reqs := r.answerAndResume(t, ask)
	if len(reqs) == 0 {
		t.Fatal("the answer resumed nothing: the test proves nothing")
	}
	for _, req := range reqs {
		if requestCarries(req, "pinned-for-the-app") {
			t.Fatal("the resume after the app's token was revoked replayed the block its work took")
		}
	}
}

// A replay whose scope cannot be read replays nothing: the app's grant read
// failing at the answer's resume, no model request carries the block.
func TestAPinnedBlockIsNotReplayedWhenItsGrantCannotBeRead(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	_, ask, _ := r.pinnedAppAsk(t, "pinned-unread-grant")
	r.fail("grant")
	defer r.heal()
	reqs := r.answerAndResume(t, ask)
	if len(reqs) == 0 {
		t.Fatal("the answer resumed nothing: the test proves nothing")
	}
	for _, req := range reqs {
		if requestCarries(req, "pinned-unread-grant") {
			t.Fatal("the resume replayed the block with the app's grant unread")
		}
	}
}

// A block is stored under what its own reads' scope answered, not under a
// second read of the rules: bo, an admin, is made an editor between the
// pinned reads and the capture, and bo's next turn takes the block again
// rather than replay the admin's.
func TestAPinnedBlockIsStoredUnderWhatItsReadsAnswered(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRunsRepository(t)
	if _, err := r.ds.Put(r.adaCtx(), substrate.ActorPerson, substrate.PutInput{Kind: typeProvider, ID: "pinned-provider", Properties: map[string]any{
		"wire": "openai", "baseURL": "https://pinned.example.com", "apiKey": "row-key-pinned",
	}}); err != nil {
		t.Fatal(err)
	}
	bo := substrate.PersonActor(r.boID)
	if err := r.r.s.SetPersonRoles(ctx, r.bo, r.ada, scope.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	r.ds.afterPinnedRead = func() {
		once.Do(func() {
			if err := r.r.s.SetPersonRoles(ctx, r.bo, r.ada, scope.RoleEditor); err != nil {
				t.Error(err)
			}
		})
	}
	thread, _ := r.chat(t, r.person(r.boID, bo), bo, "pinner", "", fakeTurn{content: "first."})
	if reqs := r.fake.requestsOf("pinner"); len(reqs) != 1 || !requestCarries(reqs[0], "pinned.example.com") {
		t.Fatal("the admin's reads did not pin the provider: the test proves nothing")
	}
	r.chat(t, r.person(r.boID, bo), bo, "pinner", thread, fakeTurn{content: "second."})
	if reqs := r.fake.requestsOf("pinner"); len(reqs) != 2 || requestCarries(reqs[1], "pinned.example.com") {
		t.Fatal("the editor's turn replayed the block the admin's reads took")
	}
}
