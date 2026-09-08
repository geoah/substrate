package engine

import (
	"context"
	"strings"
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

// rowFree reports whether a record's row is free of a FOR UPDATE lock: a
// probe transaction's NOWAIT select, rolled back at once.
func rowFree(t *testing.T, ds *dataset, kind, id string) bool {
	t.Helper()
	ctx := context.Background()
	probe, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = probe.Rollback() }()
	_, err = probe.ExecContext(ctx,
		`SELECT id FROM records WHERE kind = $1 AND id = $2 FOR UPDATE NOWAIT`, kind, id)
	if err == nil {
		return true
	}
	if !strings.Contains(err.Error(), "could not obtain lock") {
		t.Fatalf("probe the row: %v", err)
	}
	return false
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
	if _, err := ds.Delete(ctx, substrate.ActorAPI, raceWidget, "v"); err != nil {
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
	time.Sleep(400 * time.Millisecond)
	if !rowFree(t, ds, raceWidget, "v") {
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
	time.Sleep(400 * time.Millisecond)
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
