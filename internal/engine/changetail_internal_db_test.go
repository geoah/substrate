package engine

// A commit whose answer was lost may still be finishing in Postgres when the
// streams wake: the wake-up reaches every stream without another write, and
// the window it reopens waits for the changelog lock the commit holds, so it
// does not open below a row that lands after it.

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

func TestTheTailReopensAfterACommitThatLandsLate(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	headRead := make(chan struct{}, 64)
	ds := newRaceDataset(t, func(o *options) {
		o.tailHeadHook = func() { headRead <- struct{}{} }
	})
	signals := ds.WatchSignal(ctx)
	tail := substrate.WithTailRead(ctx)
	racePut(t, ds, map[string]any{"name": "warm"})
	var cursor int64
	for {
		chs, err := ds.Changes(tail, cursor, substrate.ChangeFilter{}, 200)
		if err != nil {
			t.Fatalf("tail changes: %v", err)
		}
		if len(chs) == 0 {
			break
		}
		cursor = chs[len(chs)-1].Seq
	}
	// Every signal the warm write sent is consumed, as a stream that drained
	// through it has.
	select {
	case <-signals:
	case <-time.After(5 * time.Second):
		t.Fatal("the warm write signaled nothing")
	}
	for drained := false; !drained; {
		select {
		case <-signals:
		case <-headRead:
		default:
			drained = true
		}
	}

	// The doubted commit, still running in Postgres: it holds the
	// changelog lock and has written its row, which is not committed yet.
	late, err := ds.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = late.Rollback() }()
	if _, err := late.ExecContext(ctx, `SELECT pg_advisory_xact_lock(`+advisoryKeySQL+`)`, ds.scope.lockKey(changelogLockKey)); err != nil {
		t.Fatalf("take the changelog lock: %v", err)
	}
	if _, err := late.ExecContext(ctx, `
		INSERT INTO changelog (seq, actor, op, record_id, kind, payload, principal)
		VALUES ($1, $2, $3, $4, $5, '{}'::jsonb, '')`,
		cursor+1, string(substrate.ActorPerson), string(substrate.OpPut), fid("late"), raceWidget); err != nil {
		t.Fatalf("write the late row: %v", err)
	}
	// Its client was answered in doubt: what inTxOn runs then.
	ds.commitDoubted()

	// The streams wake with no other write.
	select {
	case <-signals:
	case <-time.After(5 * time.Second):
		t.Fatal("a commit in doubt woke no stream")
	}
	read := make(chan []substrate.Change, 1)
	go func() {
		chs, err := ds.Changes(tail, cursor, substrate.ChangeFilter{}, 200)
		if err != nil {
			t.Errorf("tail changes: %v", err)
		}
		read <- chs
	}()
	// The commit lands only after the reopening read the head, or after the
	// reopening has waited a second without reading it: under the changelog
	// lock it cannot read the head before the commit ends, and without the
	// lock it reads the head before the commit, every time.
	select {
	case <-headRead:
	case <-time.After(time.Second):
	}
	if err := late.Commit(); err != nil {
		t.Fatalf("land the late commit: %v", err)
	}
	first := <-read
	again, err := ds.Changes(tail, cursor, substrate.ChangeFilter{}, 200)
	if err != nil {
		t.Fatalf("tail changes: %v", err)
	}
	for _, chs := range [][]substrate.Change{first, again} {
		if len(chs) != 1 || chs[0].Seq != cursor+1 {
			t.Fatalf("a stream resumed at %d read %d rows after the late commit landed, want its row", cursor, len(chs))
		}
	}
}
