package engine

// Whose hand resolved it (decision record 0327): a question, a call waiting
// for approval and a change request each link the actor record of the hand
// that moved it out of its waiting state, stamped by the engine, and `me` in
// a filter says which of them are the reader's own.

import (
	"context"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// resolvedByMe is the ids of the records of kind whose prop links the actor
// of the person ctx acts for.
func resolvedByMe(t *testing.T, ds *dataset, ctx context.Context, kind, prop string) []string {
	t.Helper()
	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Kinds: []string{kind}, Properties: map[string]substrate.Cond{prop: {Eq: filterMe}},
	}, First: 100})
	if err != nil {
		t.Fatalf("list %s by %s = me: %v", kind, prop, err)
	}
	var ids []string
	for _, rec := range page.Records {
		ids = append(ids, rec.ID)
	}
	return ids
}

func actorPath(a substrate.Actor) string { return vocabulary.RecordPath(kindActor, actorID(a)) }

func TestAnAnswerLinksThePersonWhoGaveIt(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	ada := r.person(r.adaID, substrate.ActorPerson)
	bo := r.person(r.boID, substrate.PersonActor(r.boID))
	ask := func() (string, *substrate.Record) {
		t.Helper()
		thread, _ := r.chat(t, ada, substrate.ActorPerson, "scribe", "",
			fakeTurn{calls: []fakeCall{{"ask", runsAsk}}}, fakeTurn{content: "asked."})
		page, err := r.ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{
			Kinds: []string{vocabulary.KindLLMInteraction}, Properties: map[string]substrate.Cond{"thread": {Eq: thread}},
		}, First: 10})
		if err != nil || len(page.Records) != 1 {
			t.Fatalf("the ask of %s: %v %v", thread, page, err)
		}
		// The resolution's resume takes the next scripted turn: it is waited
		// for, so the next chat's script is the next chat's.
		r.fake.script("scribe", fakeTurn{content: "noted."})
		return thread, page.Records[0]
	}
	resumed := func(thread string) {
		t.Helper()
		waitFor(t, "the resume after the resolution", func() bool {
			_, props := threadStatusOf(t, r.ds, thread)
			return props[propThreadLastReply] == "noted."
		})
	}

	// Pending, it links nobody.
	thread, answered := ask()
	if answered.Properties[propResolvedBy] != nil {
		t.Fatalf("a pending question links %v", answered.Properties[propResolvedBy])
	}
	// The editor answers the admin's question: the link is the editor's.
	if _, err := r.ds.Patch(bo, substrate.ActorPerson, vocabulary.KindLLMInteraction, answered.ID, substrate.PatchInput{
		Properties: map[string]any{"state": interactionAnswered, "answers": []any{map[string]any{"question": "sure", "selected": []any{"yes"}}}},
		IfVersion:  &answered.Version,
	}); err != nil {
		t.Fatalf("the editor's answer: %v", err)
	}
	resumed(thread)
	got, err := r.ds.Get(context.Background(), vocabulary.KindLLMInteraction, answered.ID)
	if err != nil {
		t.Fatal(err)
	}
	if path := storedReferencePath(got.Properties[propResolvedBy]); path != actorPath(substrate.PersonActor(r.boID)) {
		t.Fatalf("resolvedBy = %q, want the editor's actor %q", path, actorPath(substrate.PersonActor(r.boID)))
	}
	// The admin dismisses another: the link is the first person's.
	thread, dismissed := ask()
	if _, err := r.ds.Patch(ada, substrate.ActorPerson, vocabulary.KindLLMInteraction, dismissed.ID, substrate.PatchInput{
		Properties: map[string]any{"state": interactionDismissed}, IfVersion: &dismissed.Version,
	}); err != nil {
		t.Fatalf("the admin's dismissal: %v", err)
	}
	resumed(thread)
	got, err = r.ds.Get(context.Background(), vocabulary.KindLLMInteraction, dismissed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if path := storedReferencePath(got.Properties[propResolvedBy]); path != actorPath(substrate.ActorPerson) {
		t.Fatalf("resolvedBy = %q, want the first person's actor", path)
	}
	// Each reads their own by `me`, and nobody else's.
	if mine := resolvedByMe(t, r.ds, bo, vocabulary.KindLLMInteraction, propResolvedBy); len(mine) != 1 || mine[0] != answered.ID {
		t.Fatalf("the editor's own = %v, want the one they answered", mine)
	}
	if mine := resolvedByMe(t, r.ds, ada, vocabulary.KindLLMInteraction, propResolvedBy); len(mine) != 1 || mine[0] != dismissed.ID {
		t.Fatalf("the admin's own = %v, want the one they dismissed", mine)
	}
	// The link is the engine's: a write that sends one is refused, and a
	// resolved record's cannot be moved.
	_, third := ask()
	if _, err := r.ds.Patch(ada, substrate.ActorPerson, vocabulary.KindLLMInteraction, third.ID, substrate.PatchInput{
		Properties: map[string]any{"state": interactionDismissed, propResolvedBy: actorPath(substrate.PersonActor(r.boID))},
		IfVersion:  &third.Version,
	}); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("a dismissal that names another person as its hand: %v, want a validation refusal", err)
	}
}

func TestAnApprovalLinksThePersonWhoDecidedIt(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	ada := r.person(r.adaID, substrate.ActorPerson)
	bo := r.person(r.boID, substrate.PersonActor(r.boID))
	thread, id := r.approvalOf(t, ada, substrate.ActorPerson, "saver", "savenote")
	if row := approvalRow(t, r.ds, id); row.Props[propDecidedBy] != nil {
		t.Fatalf("a pending approval links %v", row.Props[propDecidedBy])
	}
	if state := r.approve(t, bo, thread, id); state != approvalDone {
		t.Fatalf("the approved call ended %s", state)
	}
	// The engine's own moves after it (running, done) leave the link alone.
	row := approvalRow(t, r.ds, id)
	if path := storedReferencePath(row.Props[propDecidedBy]); path != actorPath(substrate.PersonActor(r.boID)) {
		t.Fatalf("decidedBy = %q, want the editor's actor", path)
	}
	if mine := resolvedByMe(t, r.ds, bo, vocabulary.KindLLMApproval, propDecidedBy); len(mine) != 1 || mine[0] != id {
		t.Fatalf("the editor's own = %v", mine)
	}
	if mine := resolvedByMe(t, r.ds, ada, vocabulary.KindLLMApproval, propDecidedBy); len(mine) != 0 {
		t.Fatalf("the admin's own = %v, want none", mine)
	}
}

func TestAChangeRequestLinksWhoDecidedItAndAPolicysLinksNobody(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{
		Kind: crewPackage + "/widget", ID: fid("w-who"), Properties: map[string]any{"name": "raw"},
	}); err != nil {
		t.Fatal(err)
	}
	fake.script("root",
		fakeTurn{calls: []fakeCall{{"propose", `{"kind":"crew.test.dev/crew/widget","target":"` + fid("w-who") + `","diff":{"properties":{"name":"better"}},"rationale":"tidy"}`}}},
		fakeTurn{content: "proposed."},
		fakeTurn{content: "acknowledged."},
	)
	if _, err := ds.CallAgent(ctx, crewPackage+"/classifier", "tidy the widget"); err != nil {
		t.Fatal(err)
	}
	req := onlyPatchRequest(t, ds)
	if req.Properties[propDecidedBy] != nil {
		t.Fatalf("a proposed request links %v", req.Properties[propDecidedBy])
	}
	if _, err := ds.Patch(ctx, substrate.ActorPerson, vocabulary.KindRecordPatchRequest, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": decisionRejected}, IfVersion: &req.Version,
	}); err != nil {
		t.Fatalf("reject: %v", err)
	}
	got, err := ds.Get(ctx, vocabulary.KindRecordPatchRequest, req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if path := storedReferencePath(got.Properties[propDecidedBy]); path != actorPath(substrate.ActorPerson) {
		t.Fatalf("decidedBy = %q, want the person's actor", path)
	}
	if at, _ := got.Properties["decidedAt"].(string); at == "" {
		t.Fatal("a decided request carries no decidedAt")
	}
}

func TestARequestAPolicyDecidedLinksNobody(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	gatePolicyWithJudge(t, ds, "judged-who", map[string]any{"autoAccept": 0.9})
	fake.script("edit",
		fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", crewPackage+"/widget", fid("w-policy"), map[string]any{"name": "wanted"})}}},
		fakeTurn{content: "held."},
		fakeTurn{content: "through."},
	)
	fake.script("vjudge", fakeTurn{content: `{"verdict":"accept","confidence":0.97,"rationale":"small and honest"}`})
	if _, err := ds.CallAgent(ctx, crewPackage+"/editor", "make a widget"); err != nil {
		t.Fatal(err)
	}
	req := onlyPatchRequest(t, ds)
	judgedAnnotation(t, ds, req.ID)
	waitUntil(t, "the judged accept landing", func() bool {
		got, err := ds.Get(ctx, vocabulary.KindRecordPatchRequest, req.ID)
		return err == nil && got.Properties["decision"] == decisionAccepted
	})
	got, err := ds.Get(ctx, vocabulary.KindRecordPatchRequest, req.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The rule decided it, and the request names the rule: no hand to link.
	if got.Properties[propDecidedBy] != nil {
		t.Fatalf("a request a policy decided links %v", got.Properties[propDecidedBy])
	}
	if got.Properties["policy"] == nil {
		t.Fatal("a request a policy decided names no policy")
	}
}
