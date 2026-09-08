package engine

import (
	"context"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// The global lock order begins with the changelog (rows.go changelogLockKey),
// and inTx takes it before a transaction locks anything else. The sweep locks
// its victim's row FOR UPDATE and a put onto the same tombstone takes its
// record lock; each used to reach the changelog lock later, at its first
// append, so a put holding the row could wait for the changelog while the
// sweep held the changelog and waited for the row, and Postgres broke the
// cycle with 40P01. With the changelog first, both park there, having locked
// nothing, and run one after the other.

// waitParked waits until n sessions are parked on one advisory key of this
// repository, the pg_locks probe registrydelete_db_test.go waitParkedOn makes
// for one, so a probe that follows runs after the writer reached the lock and
// not before, whatever the runner's load.
func waitParked(t *testing.T, ds *dataset, name string, n int) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		if err := ds.db.QueryRowContext(ctx, `
			SELECT count(*) FROM pg_locks
			WHERE locktype = 'advisory' AND NOT granted AND objsubid = 1
			  AND classid::bigint = ((hashtext(current_schema() || '|' || $1)::bigint >> 32) & 4294967295)
			  AND objid::bigint = (hashtext(current_schema() || '|' || $1)::bigint & 4294967295)`,
			ds.scope.lockKey(name)).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting >= n {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%d sessions never parked on %s within the bound", n, name)
}

func TestPutAndSweepOnOneTombstoneTakeTheChangelogFirst(t *testing.T) {
	t.Parallel()
	ds := newRaceDataset(t)
	ctx := context.Background()
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: raceWidget, ID: "v", Properties: map[string]any{"name": "v"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Delete(ctx, substrate.ActorAPI, raceWidget, "v", substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}

	// The barrier holds the changelog, so every writer parks at its first key.
	barrier, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Released on every exit, so a failing assertion below frees the parked
	// writers instead of hanging the pool close.
	defer func() { _ = barrier.Rollback() }()
	if _, err := barrier.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock(`+advisoryKeySQL+`)`, ds.scope.lockKey(changelogLockKey)); err != nil {
		t.Fatal(err)
	}

	gcDone := make(chan error, 1)
	go func() {
		_, err := ds.RunGC(ctx)
		gcDone <- err
	}()
	waitParked(t, ds, changelogLockKey, 1)
	if !rowLockFree(t, ds, raceWidget, "v") {
		t.Fatal("the sweep locked the victim's row before the changelog lock")
	}
	select {
	case err := <-gcDone:
		t.Fatalf("the sweep did not park at the changelog lock: %v", err)
	default:
	}

	putDone := make(chan error, 1)
	go func() {
		_, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: raceWidget, ID: "v", Properties: map[string]any{"name": "back"},
		})
		putDone <- err
	}()
	waitParked(t, ds, changelogLockKey, 2)
	if !tryLockFree(t, ds, "record|"+raceWidget+"|v") {
		t.Fatal("the put locked its record before the changelog lock")
	}
	select {
	case err := <-putDone:
		t.Fatalf("the put did not park at the changelog lock: %v", err)
	default:
	}

	// Released, the two run in turn: the sweep purges and the put creates, or
	// the put restores and the sweep finds nothing to collect. Neither order
	// deadlocks, and either leaves the record live.
	_ = barrier.Rollback()
	for _, done := range []chan error{gcDone, putDone} {
		if err := <-done; err != nil {
			t.Fatalf("a writer parked behind the changelog lock failed: %v", err)
		}
	}
	rec, err := ds.Get(ctx, raceWidget, "v")
	if err != nil || rec.DeletedAt != nil {
		t.Fatalf("after both: %+v, %v", rec, err)
	}
}
