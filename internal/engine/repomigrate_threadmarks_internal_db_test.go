package engine

// Repository migration 14 (repomigration_0014_mark_existing_threads_read.go,
// decision 0313): threads from before marks are given their newest message
// and their count, every person a mark that reads through it, a second run
// finds nothing to do, and a rebuild reproduces the fold.

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// plantedThread is one thread written as a release before marks left it.
type plantedThread struct {
	id, mode, status string
	// reply is the assistant row a mark should read through.
	reply string
	// asks is how many pending questions name it.
	asks int
}

// plantRow folds one row as a write from before this release would have
// left it: through the changelog, with none of the write path's stamps.
func plantRow(tx *txn, kind, id string, props map[string]any, states map[string]string) error {
	if states == nil {
		states = map[string]string{}
	}
	row := &erow{ID: id, Kind: kind, States: states, Props: props, Labels: map[string]any{}, CreatedAt: tx.now, UpdatedAt: tx.now}
	if _, err := tx.foldRow(nil, row, false, false); err != nil {
		return err
	}
	return tx.appendChange(substrate.ActorSystem, substrate.OpPut, id, kind, map[string]any{"created": true, "properties": sortedKeys(props)})
}

// plantThreads writes threads, their messages and their questions as they
// stood before marks: no `lastMessage`, no count and nobody's mark.
func plantThreads(t *testing.T, ds *dataset, agent string, threads []*plantedThread) {
	t.Helper()
	ref := func(kind, id string) map[string]any { return referenceValueOf(vocabulary.RecordPath(kind, id)) }
	err := ds.inTx(context.Background(), substrate.ActorSystem, true, func(tx *txn) error {
		for n, th := range threads {
			if err := plantRow(tx, typeThread, th.id, map[string]any{
				"agent": ref(kindAgent, agent), "provider": "runsllm", "model": "lead", "mode": th.mode,
				"status": th.status, "agentDepth": 0, "subject": fmt.Sprintf("chat %d", n),
				"startedAt": "2026-10-01T10:00:00Z", "finishedAt": "2026-10-01T10:00:05Z",
			}, nil); err != nil {
				return err
			}
			th.reply = fid(th.id + "-reply")
			for turn, m := range []struct{ id, role string }{
				{fid(th.id + "-sent"), "user"}, {th.reply, "assistant"}, {fid(th.id + "-summary"), msgRoleSummary},
			} {
				if err := plantRow(tx, typeMessage, m.id, map[string]any{
					"role": m.role, "content": m.role + " text", "turn": turn, msgRelThread: ref(typeThread, th.id),
				}, nil); err != nil {
					return err
				}
			}
			for i := range th.asks {
				if err := plantRow(tx, vocabulary.KindLLMInteraction, fid(fmt.Sprintf("%s-ask-%d", th.id, i)), map[string]any{
					msgRelThread: ref(typeThread, th.id),
					"questions":  []any{map[string]any{"id": "sure", "prompt": "Are you sure?"}},
				}, map[string]string{propInteractionState: interactionPending}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("plant the threads: %v", err)
	}
}

// marksOf are the live marks of thread, by the actor each is of.
func marksOf(t *testing.T, ds *dataset, thread string) map[string]*erow {
	t.Helper()
	rows, err := ds.liveRowsOf(context.Background(), typeThreadMark)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*erow{}
	for _, row := range rows {
		if referenceTargetOf(row, propMarkThread).ID == thread {
			out[referenceTargetOf(row, propMarkReadBy).ID] = row
		}
	}
	return out
}

// rerunMigration14 takes the migration's row out of the ledger and opens the
// repository again, as a binary that carries it meets a repository an older
// one wrote. It answers the dataset and how long the open took.
func rerunMigration14(t *testing.T, r *runsRepository) (*dataset, time.Duration) {
	t.Helper()
	ctx := context.Background()
	if _, err := r.r.s.maint.ExecContext(ctx, `DELETE FROM repository_migrations WHERE repository = $1 AND version = 14`, r.repo); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	r.r.restart()
	d, err := r.r.s.Dataset(ctx, r.ada)
	if err != nil {
		t.Fatalf("open after the migration: %v", err)
	}
	took := time.Since(started)
	r.ds = d.(*dataset)
	return r.ds, took
}

func TestRepositoryMigrationMarksExistingThreadsRead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newRunsRepository(t)
	ada := r.person(r.adaID, substrate.ActorPerson)
	// A thread this release settled, which nobody has opened: it holds its
	// newest message already, and the migration gives nobody a mark of it.
	settled, _ := r.chat(t, ada, substrate.ActorPerson, "lead", "", fakeTurn{content: "one."})

	chats := 40
	if n, err := strconv.Atoi(os.Getenv("SUBSTRATE_TEST_MARK_THREADS")); err == nil && n > 0 {
		chats = n
	}
	var threads []*plantedThread
	for i := range chats {
		th := &plantedThread{id: fid(fmt.Sprintf("old-chat-%d", i)), mode: agentModeChat, status: threadOK}
		if i%10 == 0 {
			th.asks = 2
		}
		threads = append(threads, th)
	}
	scheduled := &plantedThread{id: fid("old-scheduled"), mode: "schedule", status: threadError}
	sub := &plantedThread{id: fid("old-subagent"), mode: agentModeSubagent, status: threadOK}
	judge := &plantedThread{id: fid("old-judge"), mode: agentModeJudge, status: threadOK}
	running := &plantedThread{id: fid("old-running"), mode: agentModeChat, status: threadRunning}
	threads = append(threads, scheduled, sub, judge, running)
	plantThreads(t, r.ds, r.pkg+"/lead", threads)

	// What an open costs with nothing to migrate, to read the migration's
	// own cost against.
	started := time.Now()
	r.r.restart()
	d, err := r.r.s.Dataset(ctx, r.ada)
	if err != nil {
		t.Fatal(err)
	}
	plain := time.Since(started)
	r.ds = d.(*dataset)

	before := changelogHead(t, r.ds)
	ds, took := rerunMigration14(t, r)
	wrote := changelogHead(t, ds) - before
	people, err := ds.liveRowsOf(ctx, kindActor)
	if err != nil {
		t.Fatal(err)
	}
	persons := 0
	for _, a := range people {
		if a.Props["type"] == substrate.ActorTypePerson {
			persons++
		}
	}
	t.Logf("migration 14 over %d threads and %d people: the open took %s against %s with nothing to migrate, and wrote %d changelog lines",
		len(threads), persons, took.Round(time.Millisecond), plain.Round(time.Millisecond), wrote)
	if persons != 4 {
		t.Fatalf("the repository holds %d people, want the four the fixture registers", persons)
	}

	// A measuring run plants thousands; the first of them say what every one
	// of them holds.
	checked := min(chats, 50)
	for _, th := range append(threads[:checked:checked], scheduled) {
		if got := lastMessageOf(t, ds, th.id); got != th.reply {
			t.Fatalf("thread %s names %q as its newest message, want its reply %s and never its summary row", th.id, got, th.reply)
		}
		if got := waitingOf(t, ds, th.id); got != th.asks {
			t.Fatalf("thread %s counts %d waiting, want %d", th.id, got, th.asks)
		}
		marks := marksOf(t, ds, th.id)
		if len(marks) != persons {
			t.Fatalf("thread %s has %d marks, want one for each of %d people", th.id, len(marks), persons)
		}
		for actor, mark := range marks {
			if mark.ID != threadMarkID(th.id, actor) || referenceTargetOf(mark, propMarkReadThrough).ID != th.reply || mark.Props[propMarkReadAt] == nil {
				t.Fatalf("thread %s: the mark of %s is %s reading through %v", th.id, actor, mark.ID, mark.Props)
			}
		}
	}
	// A sub-agent's thread and a judge's are parts of another run.
	for _, th := range []*plantedThread{sub, judge} {
		if got, marks := lastMessageOf(t, ds, th.id), marksOf(t, ds, th.id); got != "" || len(marks) != 0 {
			t.Errorf("a %s thread was given newest message %q and %d marks, want neither", th.mode, got, len(marks))
		}
	}
	// A thread that was running is settled by the open's recovery, which
	// stamps it, and nobody has read how it ended.
	if status, _ := threadStatusOf(t, ds, running.id); status != threadError {
		t.Fatalf("the running thread is %s after the open, want settled error", status)
	}
	if got, marks := lastMessageOf(t, ds, running.id), marksOf(t, ds, running.id); got != running.reply || len(marks) != 0 {
		t.Errorf("the interrupted thread names %q with %d marks, want its reply and no mark", got, len(marks))
	}
	if marks := marksOf(t, ds, settled); len(marks) != 0 {
		t.Errorf("a thread settled under this release, which nobody opened, was given %d marks", len(marks))
	}

	// A mark the migration wrote moves as any other.
	if _, err := ds.Put(ada, substrate.ActorPerson, substrate.PutInput{Kind: typeThreadMark, Properties: map[string]any{
		propMarkThread: threads[0].id, propMarkUnread: true,
	}}); err != nil {
		t.Fatalf("mark a migrated thread unread: %v", err)
	}

	// A second run finds nothing to do.
	head := changelogHead(t, ds)
	ds, _ = rerunMigration14(t, r)
	if got := changelogHead(t, ds); got != head {
		t.Fatalf("a second run of the migration wrote %d changelog lines", got-head)
	}

	live := foldSnapshotOf(t, ds)
	if _, err := ds.svc.RebuildRepository(ctx, ds.info.ID); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if rebuilt := foldSnapshotOf(t, ds); live != rebuilt {
		t.Fatalf("the rebuilt fold is not the live one\n%s", firstDifferenceOf([]byte(live), []byte(rebuilt)))
	}
}
