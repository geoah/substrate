package engine

// Thread marks (threadmarks.go, decision 0313): a mark is named for its
// thread and its person, a person writes only their own, its position only
// moves forward, a thread settles naming its newest message, and what waits
// on a person is counted on the thread.

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// messageIDsOf are the ids of thread's live messages, oldest first by the
// order a mark compares them in.
func messageIDsOf(t *testing.T, ds *dataset, thread string) []string {
	t.Helper()
	rows, err := ds.db.QueryContext(context.Background(), `SELECT id FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND `+referencePathSQL("props", msgRelThread)+` = $2
		ORDER BY (props->>'turn')::bigint, created_at, id`,
		typeMessage, vocabulary.RecordPath(typeThread, thread))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// lastMessageOf is the message thread's `lastMessage` names, "" for none.
func lastMessageOf(t *testing.T, ds *dataset, thread string) string {
	t.Helper()
	row, err := ds.loadRowDB(context.Background(), eref{Kind: typeThread, ID: thread})
	if err != nil || row == nil {
		t.Fatalf("thread %s: %v", thread, err)
	}
	return referenceTargetOf(row, propThreadLastMessage).ID
}

// waitingOf is thread's stored count of what waits on a person.
func waitingOf(t *testing.T, ds *dataset, thread string) int {
	t.Helper()
	_, props := threadStatusOf(t, ds, thread)
	n, _ := anyFloat(props[propThreadWaiting])
	return int(n)
}

// markRow is the stored mark under id, nil when there is none.
func markRow(t *testing.T, ds *dataset, id string) *erow {
	t.Helper()
	row, err := ds.loadRowDB(context.Background(), eref{Kind: typeThreadMark, ID: id})
	if err != nil {
		t.Fatal(err)
	}
	return row
}

// read writes a mark of thread through message, as ctx's person, with no id:
// the engine names it.
func (r *runsRepository) read(ctx context.Context, thread, message string) (*substrate.Record, error) {
	return r.ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: typeThreadMark, Properties: map[string]any{
		propMarkThread: thread, propMarkReadThrough: message,
	}})
}

// A mark created without an id is named `<thread id>.<actor id>` and filled
// with the person who wrote it. Its position moves forward and never back:
// a write naming an earlier message lands nothing and answers the stored
// record. Mark as Unread never moves the position, and the next write of the
// position clears it. A message of another thread, or none, is refused.
func TestAMarkIsNamedForItsThreadAndPersonAndOnlyMovesForward(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	ada := r.person(r.adaID, substrate.ActorPerson)
	thread, _ := r.chat(t, ada, substrate.ActorPerson, "lead", "", fakeTurn{content: "one."})
	r.chat(t, ada, substrate.ActorPerson, "lead", thread, fakeTurn{content: "two."})
	other, _ := r.chat(t, ada, substrate.ActorPerson, "lead", "", fakeTurn{content: "elsewhere."})
	msgs := messageIDsOf(t, r.ds, thread)
	if len(msgs) != 4 {
		t.Fatalf("the thread holds %d messages, want two turns of two", len(msgs))
	}
	first, last := msgs[1], msgs[3]
	id := threadMarkID(thread, actorID(substrate.ActorPerson))

	rec, err := r.read(ada, thread, first)
	if err != nil {
		t.Fatalf("a first mark: %v", err)
	}
	if rec.ID != id {
		t.Fatalf("the mark is %s, want %s", rec.ID, id)
	}
	row := markRow(t, r.ds, id)
	if got := referenceTargetOf(row, propMarkReadBy); got != (eref{Kind: kindActor, ID: actorID(substrate.ActorPerson)}) {
		t.Fatalf("readBy = %v, want the writer's own actor", got)
	}
	stamped, _ := row.Props[propMarkReadAt].(string)
	if stamped == "" {
		t.Fatal("a taken position stamps no readAt")
	}

	// A value the write sends for readAt is dropped, not refused.
	if _, err := r.ds.Put(ada, substrate.ActorPerson, substrate.PutInput{Kind: typeThreadMark, ID: id, Properties: map[string]any{
		propMarkThread: thread, propMarkReadThrough: last, propMarkReadAt: "2001-01-01T00:00:00Z",
	}}); err != nil {
		t.Fatalf("a move forward carrying its own readAt: %v", err)
	}
	row = markRow(t, r.ds, id)
	moved, _ := row.Props[propMarkReadAt].(string)
	if referenceTargetOf(row, propMarkReadThrough).ID != last || moved == "2001-01-01T00:00:00Z" || moved == stamped {
		t.Fatalf("after the move the mark reads through %s at %s, want %s at a new stamp", referenceTargetOf(row, propMarkReadThrough).ID, moved, last)
	}

	// Backwards: nothing lands, and the answer is what is stored.
	version := row.Version
	rec, err = r.read(ada, thread, first)
	if err != nil {
		t.Fatalf("a late write must not fail: %v", err)
	}
	if got := writtenReferenceID(rec.Properties[propMarkReadThrough], typeMessage); got != last || rec.Version != version {
		t.Fatalf("a write naming an earlier message answered %s at version %d, want the stored %s at %d", got, rec.Version, last, version)
	}

	// Mark as Unread is a flag, and the position stays.
	if _, err := r.ds.Patch(ada, substrate.ActorPerson, typeThreadMark, id, substrate.PatchInput{Properties: map[string]any{propMarkUnread: true}}); err != nil {
		t.Fatalf("mark as unread: %v", err)
	}
	row = markRow(t, r.ds, id)
	if row.Props[propMarkUnread] != true || referenceTargetOf(row, propMarkReadThrough).ID != last || row.Props[propMarkReadAt] != moved {
		t.Fatalf("after Mark as Unread the mark is %v, want the flag set and the position and its time left", row.Props)
	}
	// A late write does not clear it; the next write of the position does.
	if _, err := r.read(ada, thread, first); err != nil {
		t.Fatal(err)
	}
	if markRow(t, r.ds, id).Props[propMarkUnread] != true {
		t.Fatal("a write naming an earlier message cleared Mark as Unread")
	}
	if _, err := r.read(ada, thread, last); err != nil {
		t.Fatal(err)
	}
	row = markRow(t, r.ds, id)
	if row.Props[propMarkUnread] == true || row.Props[propMarkReadAt] == moved {
		t.Fatalf("reading the thread again left the mark %v, want the flag cleared and readAt stamped again", row.Props)
	}

	// A message of another thread, and one that does not exist.
	for what, message := range map[string]string{
		"a message of another thread": messageIDsOf(t, r.ds, other)[0],
		"a message that is not there": fid("no-such-message"),
	} {
		if _, err := r.read(ada, thread, message); !errors.Is(err, substrate.ErrValidation) {
			t.Errorf("%s: %v, want refused as validation", what, err)
		}
	}
	// The two references are fixed, and the id is theirs.
	if _, err := r.ds.Put(ada, substrate.ActorPerson, substrate.PutInput{Kind: typeThreadMark, ID: id, Properties: map[string]any{propMarkThread: other}}); !errors.Is(err, substrate.ErrValidation) {
		t.Errorf("a mark moved to another thread: %v, want refused", err)
	}
	if _, err := r.ds.Put(ada, substrate.ActorPerson, substrate.PutInput{Kind: typeThreadMark, ID: fid("a-name"), Properties: map[string]any{propMarkThread: thread}}); !errors.Is(err, substrate.ErrValidation) {
		t.Errorf("a mark under an id of its writer's choosing: %v, want refused", err)
	}
	if _, err := r.ds.Put(ada, substrate.ActorPerson, substrate.PutInput{Kind: typeThreadMark, Properties: map[string]any{propMarkThread: fid("no-such-thread")}}); !errors.Is(err, substrate.ErrNotFound) {
		t.Errorf("a mark of a thread that is not there: %v, want not found", err)
	}
}

// A person moves only their own mark, whatever their role: an editor's write,
// patch and delete of the admin's are refused by a rule, a viewer keeps
// their own, and no installed code keeps anybody's.
func TestAPersonWritesOnlyTheirOwnMark(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	ada := r.person(r.adaID, substrate.ActorPerson)
	bo := r.person(r.boID, substrate.PersonActor(r.boID))
	cy := r.person(r.cyID, substrate.PersonActor(r.cyID))
	thread, _ := r.chat(t, ada, substrate.ActorPerson, "lead", "", fakeTurn{content: "one."})
	r.chat(t, ada, substrate.ActorPerson, "lead", thread, fakeTurn{content: "two."})
	msgs := messageIDsOf(t, r.ds, thread)
	if _, err := r.read(ada, thread, msgs[1]); err != nil {
		t.Fatal(err)
	}
	adas := threadMarkID(thread, actorID(substrate.ActorPerson))

	if _, err := r.ds.Put(bo, substrate.ActorPerson, substrate.PutInput{Kind: typeThreadMark, ID: adas, Properties: map[string]any{
		propMarkThread: thread, propMarkReadThrough: msgs[3],
	}}); !errors.Is(err, substrate.ErrNotPermitted) {
		t.Fatalf("a second person moving the first's mark: %v, want refused by a rule", err)
	}
	if _, err := r.ds.Patch(bo, substrate.ActorPerson, typeThreadMark, adas, substrate.PatchInput{Properties: map[string]any{propMarkUnread: true}}); !errors.Is(err, substrate.ErrNotPermitted) {
		t.Fatalf("a second person marking the first's thread unread: %v, want refused by a rule", err)
	}
	if _, err := r.ds.Delete(bo, substrate.ActorPerson, typeThreadMark, adas, substrate.DeleteInput{}); !errors.Is(err, substrate.ErrNotPermitted) {
		t.Fatalf("a second person deleting the first's mark: %v, want refused by a rule", err)
	}
	if row := markRow(t, r.ds, adas); row.DeletedAt != nil || referenceTargetOf(row, propMarkReadThrough).ID != msgs[1] || row.Props[propMarkUnread] == true {
		t.Fatalf("the first person's mark moved under another's hand: %v", row.Props)
	}
	// Naming another person in a mark of one's own is refused too.
	if _, err := r.ds.Put(bo, substrate.ActorPerson, substrate.PutInput{Kind: typeThreadMark, Properties: map[string]any{
		propMarkThread: thread, propMarkReadBy: actorID(substrate.ActorPerson),
	}}); !errors.Is(err, substrate.ErrNotPermitted) {
		t.Fatalf("a mark naming another person: %v, want refused by a rule", err)
	}

	// Each person's own, the viewer's included.
	for who, ctx := range map[string]context.Context{r.boID: bo, r.cyID: cy} {
		rec, err := r.read(ctx, thread, msgs[3])
		if err != nil {
			t.Fatalf("%s's own mark: %v", who, err)
		}
		if want := threadMarkID(thread, actorID(substrate.PersonActor(who))); rec.ID != want {
			t.Fatalf("%s's mark is %s, want %s", who, rec.ID, want)
		}
	}
	if _, err := r.ds.Delete(cy, substrate.ActorPerson, typeThreadMark, threadMarkID(thread, actorID(substrate.PersonActor(r.cyID))), substrate.DeleteInput{}); err != nil {
		t.Fatalf("a viewer deleting their own mark: %v", err)
	}

	// Installed code keeps nobody's place, whatever it acts for.
	for _, hand := range []substrate.Actor{
		substrate.Actor(substrate.AgentActorPrefix + r.ds.Repository().Authority + ":runs:lead"),
		substrate.Actor(substrate.FunctionActorPrefix + r.ds.Repository().Authority + ":runs:reader"),
		substrate.Actor("bundle:" + r.ds.Repository().Authority + ":runs"),
	} {
		if _, err := r.ds.Put(ada, hand, substrate.PutInput{Kind: typeThreadMark, ID: adas, Properties: map[string]any{
			propMarkThread: thread, propMarkReadThrough: msgs[3],
		}}); !errors.Is(err, substrate.ErrNotPermitted) {
			t.Errorf("%s writing a person's mark: %v, want refused by a rule", hand, err)
		}
	}
}

// Two devices of one person race, one a message behind the other: whichever
// commits first, the mark ends on the later position.
func TestTwoDevicesRacingEndOnTheLaterPosition(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	ada := r.person(r.adaID, substrate.ActorPerson)
	thread, _ := r.chat(t, ada, substrate.ActorPerson, "lead", "", fakeTurn{content: "one."})
	for _, reply := range []string{"two.", "three.", "four."} {
		r.chat(t, ada, substrate.ActorPerson, "lead", thread, fakeTurn{content: reply})
	}
	msgs := messageIDsOf(t, r.ds, thread)
	id := threadMarkID(thread, actorID(substrate.ActorPerson))
	for i := 1; i < len(msgs); i++ {
		behind, ahead := msgs[i-1], msgs[i]
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for n, message := range []string{behind, ahead} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, errs[n] = r.read(ada, thread, message)
			}()
		}
		wg.Wait()
		if err := errors.Join(errs...); err != nil {
			t.Fatalf("round %d: a racing write failed: %v", i, err)
		}
		if got := referenceTargetOf(markRow(t, r.ds, id), propMarkReadThrough).ID; got != ahead {
			t.Fatalf("round %d: the mark ended on %s, want the later %s", i, got, ahead)
		}
	}
	// Both orders, one after the other.
	if _, err := r.read(ada, thread, msgs[0]); err != nil {
		t.Fatal(err)
	}
	if got := referenceTargetOf(markRow(t, r.ds, id), propMarkReadThrough).ID; got != msgs[len(msgs)-1] {
		t.Fatalf("the earlier write after the later one left %s", got)
	}
}

// A thread settles naming its newest message, whichever way its run ends: a
// reply, a run that writes several rows and waits, a resumed run, a failed
// call that wrote no reply, and the recovery's settle. A summary row is never
// the one named. What waits on a person is counted as it is asked and
// resolved.
func TestAThreadSettlesNamingItsNewestMessageAndCountsWhatWaits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRunsRepository(t)
	ada := r.person(r.adaID, substrate.ActorPerson)
	newest := func(thread string) string {
		t.Helper()
		msgs := messageIDsOf(t, r.ds, thread)
		return msgs[len(msgs)-1]
	}
	roleOf := func(message string) string {
		t.Helper()
		row, err := r.ds.loadRowDB(ctx, eref{Kind: typeMessage, ID: message})
		if err != nil || row == nil {
			t.Fatalf("message %s: %v", message, err)
		}
		role, _ := row.Props["role"].(string)
		return role
	}

	// A reply.
	plain, _ := r.chat(t, ada, substrate.ActorPerson, "lead", "", fakeTurn{content: "one."})
	if got := lastMessageOf(t, r.ds, plain); got != newest(plain) || roleOf(got) != "assistant" {
		t.Fatalf("after a reply lastMessage is %s (%s), want the reply %s", got, roleOf(got), newest(plain))
	}
	if n := waitingOf(t, r.ds, plain); n != 0 {
		t.Fatalf("a thread nothing waits in counts %d", n)
	}

	// A run that calls a tool, is held for approval and replies: several rows,
	// and one call waiting on a person.
	held, approval := r.approvalOf(t, ada, substrate.ActorPerson, "saver", "savenote")
	if got := lastMessageOf(t, r.ds, held); got != newest(held) || roleOf(got) != "assistant" {
		t.Fatalf("after a run of several rows lastMessage is %s (%s), want the last reply %s", got, roleOf(got), newest(held))
	}
	if n := waitingOf(t, r.ds, held); n != 1 {
		t.Fatalf("a thread with one call waiting counts %d", n)
	}
	before := lastMessageOf(t, r.ds, held)
	// The approval resolves, its report lands and the resumed run settles.
	r.approve(t, ada, held, approval)
	if n := waitingOf(t, r.ds, held); n != 0 {
		t.Fatalf("after the approval the thread still counts %d waiting", n)
	}
	waitFor(t, "the resumed run's settle to name its reply", func() bool {
		got := lastMessageOf(t, r.ds, held)
		return got != before && got == newest(held)
	})

	// A question: asked, then answered.
	asked, _ := r.chat(t, ada, substrate.ActorPerson, "keeper", "",
		fakeTurn{calls: []fakeCall{{"ask", runsAsk}}}, fakeTurn{content: "asked."}, fakeTurn{content: "noted."})
	if n := waitingOf(t, r.ds, asked); n != 1 {
		t.Fatalf("a thread with one question waiting counts %d", n)
	}
	page, err := r.ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{vocabulary.KindLLMInteraction}}, First: 10})
	if err != nil || len(page.Records) != 1 {
		t.Fatalf("the question: %v %v", page, err)
	}
	if _, err := r.ds.Patch(ada, substrate.ActorPerson, vocabulary.KindLLMInteraction, page.Records[0].ID, substrate.PatchInput{
		Properties: map[string]any{"state": interactionAnswered, "answers": []any{map[string]any{"question": "sure", "selected": []any{"yes"}}}},
		IfVersion:  &page.Records[0].Version,
	}); err != nil {
		t.Fatalf("answer the question: %v", err)
	}
	if n := waitingOf(t, r.ds, asked); n != 0 {
		t.Fatalf("after the answer the thread still counts %d waiting", n)
	}
	waitFor(t, "the resume after the answer", func() bool {
		_, props := threadStatusOf(t, r.ds, asked)
		return props[propThreadLastReply] == "noted."
	})

	// A call that fails before any reply: the newest row is the message sent.
	r.fake.script("lead", fakeTurn{status: 400})
	if _, err := r.ds.ChatAgent(ada, substrate.ActorPerson, r.pkg+"/lead", substrate.ChatInput{Message: "again", Thread: plain}, nil); err == nil {
		t.Fatal("a failed model call answered no error")
	}
	if status, _ := threadStatusOf(t, r.ds, plain); status != threadError {
		t.Fatalf("the failed turn settled %s", status)
	}
	if got := lastMessageOf(t, r.ds, plain); got != newest(plain) || roleOf(got) != "user" {
		t.Fatalf("after a failed call lastMessage is %s (%s), want the message sent %s", got, roleOf(got), newest(plain))
	}

	// A summary row above every other is not the newest message.
	sent := lastMessageOf(t, r.ds, plain)
	if _, err := r.ds.putInternal(ctx, substrate.ActorSystem, substrate.PutInput{Kind: typeMessage, ID: fid("a-summary"), Properties: map[string]any{
		"role": msgRoleSummary, "content": "what came before", "turn": 99, msgRelThread: plain,
	}}); err != nil {
		t.Fatalf("plant a summary row: %v", err)
	}
	// The recovery's settle stamps it too: a run lost with its lease expired.
	if _, err := r.ds.db.ExecContext(ctx, `UPDATE records SET props = (props - 'lastMessage') || '{"status":"running","leaseUntil":"2001-01-01T00:00:00Z"}'::jsonb
		WHERE kind = $1 AND id = $2`, typeThread, plain); err != nil {
		t.Fatal(err)
	}
	if err := r.ds.settleLostThreads(ctx); err != nil {
		t.Fatalf("settle the lost run: %v", err)
	}
	if status, _ := threadStatusOf(t, r.ds, plain); status != threadError {
		t.Fatalf("the lost run settled %s", status)
	}
	if got := lastMessageOf(t, r.ds, plain); got != sent {
		t.Fatalf("after the recovery's settle lastMessage is %s (%s), want %s, the newest row that is no summary", got, roleOf(got), sent)
	}
}
