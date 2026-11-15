package engine

// Pinned and archived (threadmarks.go, decision 0322): two flags on a
// person's own mark of a thread. Each is that person's to write and nobody
// else's, a write of one never moves how far they have read, no run moves
// either, and a list of threads is narrowed by the reader's own.

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// flag writes one of ctx's person's own flags of thread, with no id: the
// engine names the mark, and makes it where there was none.
func (r *runsRepository) flag(ctx context.Context, thread, name string, on bool) (*substrate.Record, error) {
	return r.ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: typeThreadMark, Properties: map[string]any{
		propMarkThread: thread, name: on,
	}})
}

// myThreads lists the chat threads as ctx's person, narrowed by their own
// marks: the ones a mark of theirs meeting conds points at, or with exists
// false the ones none does. Oldest first.
func (r *runsRepository) myThreads(t *testing.T, ctx context.Context, conds map[string]substrate.Cond, exists bool) []string {
	t.Helper()
	props := map[string]substrate.Cond{propMarkReadBy: {Eq: substrate.FilterMe}}
	for name, c := range conds {
		props[name] = c
	}
	page, err := r.ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{
			Kinds:        []string{typeThread},
			Properties:   map[string]substrate.Cond{"mode": {Eq: "chat"}},
			ReferencedBy: &substrate.ReferencedBy{Kind: typeThreadMark, Property: propMarkThread, Properties: props, Exists: &exists},
		},
		OrderBy: []substrate.Order{{Property: "createdAt"}}, First: 50,
	})
	if err != nil {
		t.Fatalf("list my threads under %v: %v", conds, err)
	}
	ids := make([]string, len(page.Records))
	for i, rec := range page.Records {
		ids[i] = rec.ID
	}
	return ids
}

var (
	isPinned   = map[string]substrate.Cond{propMarkPinned: {Eq: true}}
	isArchived = map[string]substrate.Cond{propMarkArchived: {Eq: true}}
)

// A person pins and archives a thread they never opened: the write makes
// their mark, with no position and no time of one, and the thread is still
// as unread for them as it was. Neither flag moves the position, the unread
// flag or the stamp, and reading the thread moves neither flag.
func TestAPersonPinsAndArchivesAThreadTheyNeverOpened(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	ada := r.person(r.adaID, substrate.ActorPerson)
	bo := r.person(r.boID, substrate.PersonActor(r.boID))
	thread, _ := r.chat(t, ada, substrate.ActorPerson, "lead", "", fakeTurn{content: "one."})
	r.chat(t, ada, substrate.ActorPerson, "lead", thread, fakeTurn{content: "two."})
	msgs := messageIDsOf(t, r.ds, thread)
	first, last := msgs[1], msgs[3]
	id := threadMarkID(thread, actorID(substrate.PersonActor(r.boID)))

	rec, err := r.flag(bo, thread, propMarkPinned, true)
	if err != nil {
		t.Fatalf("pin a thread never opened: %v", err)
	}
	if rec.ID != id {
		t.Fatalf("the pin made mark %s, want %s", rec.ID, id)
	}
	row := markRow(t, r.ds, id)
	if row.Props[propMarkPinned] != true || named(row.Props, propMarkReadThrough) || named(row.Props, propMarkReadAt) {
		t.Fatalf("a pin on a thread never opened stored %v, want the flag and no position", row.Props)
	}
	if got := referenceTargetOf(row, propMarkReadBy); got.ID != actorID(substrate.PersonActor(r.boID)) {
		t.Fatalf("the pin's mark is read by %v, want its writer", got)
	}
	if _, err := r.flag(bo, thread, propMarkArchived, true); err != nil {
		t.Fatalf("archive a thread never opened: %v", err)
	}
	row = markRow(t, r.ds, id)
	if row.Props[propMarkArchived] != true || row.Props[propMarkPinned] != true || named(row.Props, propMarkReadThrough) || named(row.Props, propMarkReadAt) {
		t.Fatalf("after the archive the mark is %v, want both flags and still no position", row.Props)
	}

	// Reading it takes the position and leaves the flags.
	if _, err := r.read(bo, thread, first); err != nil {
		t.Fatalf("read a thread pinned before it was opened: %v", err)
	}
	row = markRow(t, r.ds, id)
	stamped, _ := row.Props[propMarkReadAt].(string)
	if referenceTargetOf(row, propMarkReadThrough).ID != first || stamped == "" || row.Props[propMarkPinned] != true || row.Props[propMarkArchived] != true {
		t.Fatalf("after the first read the mark is %v, want the position, its time and both flags", row.Props)
	}
	// A flag written beside a position that is behind the stored one lands,
	// and the position stays; one written alone leaves the position, its
	// time and Mark as Unread alone.
	if _, err := r.read(bo, thread, last); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ds.Patch(bo, substrate.ActorPerson, typeThreadMark, id, substrate.PatchInput{Properties: map[string]any{propMarkUnread: true}}); err != nil {
		t.Fatal(err)
	}
	stamped, _ = markRow(t, r.ds, id).Props[propMarkReadAt].(string)
	if _, err := r.ds.Put(bo, substrate.ActorPerson, substrate.PutInput{Kind: typeThreadMark, Properties: map[string]any{
		propMarkThread: thread, propMarkReadThrough: first, propMarkPinned: false,
	}}); err != nil {
		t.Fatalf("unpin beside a late position: %v", err)
	}
	if _, err := r.flag(bo, thread, propMarkArchived, false); err != nil {
		t.Fatalf("unarchive: %v", err)
	}
	row = markRow(t, r.ds, id)
	if row.Props[propMarkPinned] == true || row.Props[propMarkArchived] == true {
		t.Fatalf("after unpin and unarchive the mark is %v", row.Props)
	}
	if referenceTargetOf(row, propMarkReadThrough).ID != last || row.Props[propMarkReadAt] != stamped || row.Props[propMarkUnread] != true {
		t.Fatalf("the flags moved what the person read: %v, want through %s at %s, still marked unread", row.Props, last, stamped)
	}
}

// A person writes only their own flags, a viewer theirs, an app its
// person's; and nothing a run does moves one: a reply that settles on a
// thread its sender archived, and on one another person archived, leaves
// both archived.
func TestOnlyItsPersonMovesAThreadsPinOrArchive(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	ada := r.person(r.adaID, substrate.ActorPerson)
	bo := r.person(r.boID, substrate.PersonActor(r.boID))
	cy := r.person(r.cyID, substrate.PersonActor(r.cyID))
	thread, _ := r.chat(t, ada, substrate.ActorPerson, "lead", "", fakeTurn{content: "one."})
	adas := threadMarkID(thread, actorID(substrate.ActorPerson))
	for who, ctx := range map[string]context.Context{"the admin": ada, "the editor": bo, "the viewer": cy} {
		for _, name := range []string{propMarkPinned, propMarkArchived} {
			if _, err := r.flag(ctx, thread, name, true); err != nil {
				t.Fatalf("%s's own %s: %v", who, name, err)
			}
		}
	}
	for _, name := range []string{propMarkPinned, propMarkArchived} {
		if _, err := r.ds.Patch(bo, substrate.ActorPerson, typeThreadMark, adas, substrate.PatchInput{Properties: map[string]any{name: false}}); !errors.Is(err, substrate.ErrNotPermitted) {
			t.Fatalf("a second person clearing the first's %s: %v, want refused by a rule", name, err)
		}
		if _, err := r.ds.Put(bo, substrate.ActorPerson, substrate.PutInput{Kind: typeThreadMark, ID: adas, Properties: map[string]any{propMarkThread: thread, name: false}}); !errors.Is(err, substrate.ErrNotPermitted) {
			t.Fatalf("a second person putting the first's %s: %v, want refused by a rule", name, err)
		}
	}
	// An app writes the flags of the person it acts for.
	appActor, app, _ := r.app(t, "chat.example.com", "agents:"+r.pkg+"/*")
	if _, err := r.ds.Put(app, appActor, substrate.PutInput{Kind: typeThreadMark, Properties: map[string]any{
		propMarkThread: thread, propMarkPinned: false,
	}}); err != nil {
		t.Fatalf("an app unpinning for its person: %v", err)
	}
	if row := markRow(t, r.ds, adas); row.Props[propMarkPinned] == true || row.Props[propMarkArchived] != true {
		t.Fatalf("after the app's unpin ada's mark is %v, want unpinned and still archived", row.Props)
	}
	// No installed code keeps anybody's.
	for _, hand := range []substrate.Actor{
		substrate.Actor(substrate.AgentActorPrefix + r.ds.Repository().Authority + ":runs:lead"),
		substrate.Actor(substrate.FunctionActorPrefix + r.ds.Repository().Authority + ":runs:reader"),
		substrate.Actor("bundle:" + r.ds.Repository().Authority + ":runs"),
	} {
		if _, err := r.ds.Put(ada, hand, substrate.PutInput{Kind: typeThreadMark, ID: adas, Properties: map[string]any{
			propMarkThread: thread, propMarkArchived: false,
		}}); !errors.Is(err, substrate.ErrNotPermitted) {
			t.Errorf("%s unarchiving a person's thread: %v, want refused by a rule", hand, err)
		}
	}

	// The sender's own next message and its reply settle, and both people's
	// marks say what they said.
	r.chat(t, ada, substrate.ActorPerson, "lead", thread, fakeTurn{content: "two."})
	if status, _ := threadStatusOf(t, r.ds, thread); status != threadOK {
		t.Fatalf("the second turn settled %s", status)
	}
	for who, id := range map[string]string{
		"its sender's":     adas,
		"another person's": threadMarkID(thread, actorID(substrate.PersonActor(r.boID))),
		"the viewer's":     threadMarkID(thread, actorID(substrate.PersonActor(r.cyID))),
	} {
		row := markRow(t, r.ds, id)
		if row.Props[propMarkArchived] != true {
			t.Errorf("after a reply settled, %s mark is %v, want still archived", who, row.Props)
		}
	}
	if row := markRow(t, r.ds, threadMarkID(thread, actorID(substrate.PersonActor(r.boID)))); row.Props[propMarkPinned] != true {
		t.Errorf("after a reply settled, the editor's mark is %v, want still pinned", row.Props)
	}
}

// A list of threads is narrowed by the reader's own marks: the ones they
// have not archived, the ones they have, the ones they pinned. Another
// person's flags of the same threads change nothing in it, a thread with no
// mark is not archived, and a mark made by a pin alone counts. A thread that
// waits on a person is found by its own count, archived or not.
func TestAListOfThreadsIsNarrowedByItsReadersOwnMarks(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	ada := r.person(r.adaID, substrate.ActorPerson)
	bo := r.person(r.boID, substrate.PersonActor(r.boID))
	cy := r.person(r.cyID, substrate.PersonActor(r.cyID))
	var t1, t2, t3 string
	for _, id := range []*string{&t1, &t2, &t3} {
		*id, _ = r.chat(t, ada, substrate.ActorPerson, "lead", "", fakeTurn{content: "done."})
	}
	// A fourth that waits on a person.
	asked, _ := r.chat(t, ada, substrate.ActorPerson, "keeper", "",
		fakeTurn{calls: []fakeCall{{"ask", runsAsk}}}, fakeTurn{content: "asked."})
	if n := waitingOf(t, r.ds, asked); n != 1 {
		t.Fatalf("the asking thread counts %d waiting", n)
	}
	all := []string{t1, t2, t3, asked}

	// Nobody has a mark yet: nothing is archived or pinned for anybody.
	for who, ctx := range map[string]context.Context{"ada": ada, "bo": bo} {
		if got := r.myThreads(t, ctx, isArchived, false); !slices.Equal(got, all) {
			t.Fatalf("with no marks, %s's unarchived threads are %v, want all four", who, got)
		}
		if got := r.myThreads(t, ctx, isArchived, true); len(got) != 0 {
			t.Fatalf("with no marks, %s's archived threads are %v", who, got)
		}
	}

	set := func(ctx context.Context, thread, name string, on bool) {
		t.Helper()
		if _, err := r.flag(ctx, thread, name, on); err != nil {
			t.Fatalf("set %s on %s: %v", name, thread, err)
		}
	}
	// ada archives t1 and the one that waits, and pins t2. bo archives t2
	// and pins t1 and t3. cy only reads t1.
	set(ada, t1, propMarkArchived, true)
	set(ada, asked, propMarkArchived, true)
	set(ada, t2, propMarkPinned, true)
	set(bo, t2, propMarkArchived, true)
	set(bo, t1, propMarkPinned, true)
	set(bo, t3, propMarkPinned, true)
	if _, err := r.read(cy, t1, lastMessageOf(t, r.ds, t1)); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		who                          string
		ctx                          context.Context
		unarchived, archived, pinned []string
	}{
		{"ada", ada, []string{t2, t3}, []string{t1, asked}, []string{t2}},
		{"bo", bo, []string{t1, t3, asked}, []string{t2}, []string{t1, t3}},
		{"cy", cy, all, nil, nil},
	} {
		if got := r.myThreads(t, tc.ctx, isArchived, false); !slices.Equal(got, tc.unarchived) {
			t.Errorf("%s's unarchived threads are %v, want %v", tc.who, got, tc.unarchived)
		}
		if got := r.myThreads(t, tc.ctx, isArchived, true); !slices.Equal(got, tc.archived) {
			t.Errorf("%s's archived threads are %v, want %v", tc.who, got, tc.archived)
		}
		if got := r.myThreads(t, tc.ctx, isPinned, true); !slices.Equal(got, tc.pinned) {
			t.Errorf("%s's pinned threads are %v, want %v", tc.who, got, tc.pinned)
		}
	}

	// What waits on a person is found by the thread's own count, and stays
	// found in a thread its reader archived: the archive hides no question.
	waiting := func(ctx context.Context, archived bool) []string {
		t.Helper()
		page, err := r.ds.List(ctx, substrate.Query{Filter: substrate.Filter{
			Kinds:      []string{typeThread},
			Properties: map[string]substrate.Cond{propThreadWaiting: {Gt: 0}},
			ReferencedBy: &substrate.ReferencedBy{Kind: typeThreadMark, Property: propMarkThread, Properties: map[string]substrate.Cond{
				propMarkReadBy: {Eq: substrate.FilterMe}, propMarkArchived: {Eq: true},
			}, Exists: &archived},
		}, First: 10})
		if err != nil {
			t.Fatalf("the threads that wait: %v", err)
		}
		ids := make([]string, len(page.Records))
		for i, rec := range page.Records {
			ids[i] = rec.ID
		}
		return ids
	}
	if got := waiting(ada, true); !slices.Equal(got, []string{asked}) {
		t.Fatalf("ada's archived threads that wait are %v, want the asking one", got)
	}
	if got := waiting(bo, true); len(got) != 0 {
		t.Fatalf("bo's archived threads that wait are %v, want none", got)
	}
	if got := waiting(bo, false); !slices.Equal(got, []string{asked}) {
		t.Fatalf("bo's unarchived threads that wait are %v, want the asking one", got)
	}

	// Unarchived, a thread is back in its reader's list and out of the
	// archive; unpinned, out of the pinned ones. A pinned thread its reader
	// archives is in both reads, and the mark says which.
	set(ada, t1, propMarkArchived, false)
	set(ada, t2, propMarkArchived, true)
	if got := r.myThreads(t, ada, isArchived, false); !slices.Equal(got, []string{t1, t3}) {
		t.Errorf("after an unarchive ada's unarchived threads are %v, want %v", got, []string{t1, t3})
	}
	if got, want := r.myThreads(t, ada, isPinned, true), []string{t2}; !slices.Equal(got, want) {
		t.Errorf("ada's pinned threads are %v, want %v, archived or not", got, want)
	}
	if row := markRow(t, r.ds, threadMarkID(t2, actorID(substrate.ActorPerson))); row.Props[propMarkPinned] != true || row.Props[propMarkArchived] != true {
		t.Errorf("ada's mark of a thread she pinned and archived is %v, want both flags", row.Props)
	}
	set(bo, t1, propMarkPinned, false)
	if got := r.myThreads(t, bo, isPinned, true); !slices.Equal(got, []string{t3}) {
		t.Errorf("after an unpin bo's pinned threads are %v, want %v", got, []string{t3})
	}

	// A deleted mark takes its flags with it.
	if _, err := r.ds.Delete(bo, substrate.ActorPerson, typeThreadMark, threadMarkID(t2, actorID(substrate.PersonActor(r.boID))), substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	if got := r.myThreads(t, bo, isArchived, true); len(got) != 0 {
		t.Errorf("after bo deleted the mark, bo's archived threads are %v, want none", got)
	}

	// A read that acts for nobody has no marks of its own to ask about.
	if _, err := r.ds.List(context.Background(), substrate.Query{Filter: substrate.Filter{
		Kinds: []string{typeThread},
		ReferencedBy: &substrate.ReferencedBy{Kind: typeThreadMark, Property: propMarkThread, Properties: map[string]substrate.Cond{
			propMarkReadBy: {Eq: substrate.FilterMe},
		}},
	}}); !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("`me` inside referencedBy on a read that acts for nobody: %v, want refused as validation", err)
	}
}

// An agent's query tool and a pinned read hold a read to the kinds its
// filter names, and the arm names one more: both refuse it by name.
func TestAnAgentsReadsRefuseReferencedBy(t *testing.T) {
	t.Parallel()
	r := newRunsRepository(t)
	_, err := r.ds.readList(context.Background(), substrate.Query{Filter: substrate.Filter{
		Kinds:        []string{r.note},
		ReferencedBy: &substrate.ReferencedBy{Kind: typeThreadMark, Property: propMarkThread},
	}})
	if !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("readList under referencedBy: %v, want refused as validation", err)
	}
	if arm := rankedFilterArm(substrate.Filter{ReferencedBy: &substrate.ReferencedBy{}}); arm != "referencedBy" {
		t.Fatalf("the ranked read names the arm %q, want referencedBy", arm)
	}
}
