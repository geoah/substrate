package engine

// The two interleavings of a purge reservation that only a txn-level driver
// can produce: a put racing the sweep that reserves its id, and a replay of
// history written before reservations existed.

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

func widgetInput(id, name string) substrate.PutInput {
	return substrate.PutInput{Kind: raceWidget, ID: id, Properties: map[string]any{"name": name}}
}

// GC takes the record's advisory lock before its row lock. Without it a put
// that passed checkID before the reservation existed waits out the purge on
// the row lock alone, its re-evaluated select finds no row, and it creates a
// record beside its own reservation, so every later put at the id is refused
// while the record is live. Behind the lock the put meets the reservation.
func TestGCReservationSerializesWithPut(t *testing.T) {
	t.Parallel()
	ds := newRaceDataset(t)
	ctx := context.Background()
	const id = "widget-raced"
	if _, err := ds.Put(ctx, substrate.ActorAPI, widgetInput(id, "first")); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := ds.Delete(ctx, substrate.ActorAPI, raceWidget, id); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// The barrier holds the tombstone's ROW lock, so the sweep parks inside
	// loadRow FOR UPDATE with its advisory lock already taken.
	barrier, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("barrier: %v", err)
	}
	if _, err := barrier.ExecContext(ctx, `SELECT id FROM records WHERE id = $1 FOR UPDATE`, id); err != nil {
		t.Fatalf("barrier lock: %v", err)
	}
	gcDone := make(chan error, 1)
	go func() {
		_, err := ds.RunGC(ctx)
		gcDone <- err
	}()
	time.Sleep(300 * time.Millisecond)

	putDone := make(chan error, 1)
	go func() {
		_, err := ds.Put(ctx, substrate.ActorAPI, widgetInput(id, "second"))
		putDone <- err
	}()
	time.Sleep(300 * time.Millisecond)
	select {
	case err := <-putDone:
		t.Fatalf("the put did not queue behind the sweep: %v", err)
	default:
	}
	_ = barrier.Rollback()

	if err := <-gcDone; err != nil {
		t.Fatalf("gc: %v", err)
	}
	if err := <-putDone; !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("a put racing the purge of its id: want conflict, got %v", err)
	}
	// The invariant: no live record stands beside a reservation for its id.
	err = ds.db.QueryRowContext(ctx, `SELECT 1 FROM records WHERE kind = $1 AND id = $2`, raceWidget, id).Scan(new(int))
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("a record exists at the purged id: %v", err)
	}
	var target string
	if err := ds.db.QueryRowContext(ctx,
		`SELECT record_id FROM former_ids WHERE record_kind = $1 AND former_id = $2`, raceWidget, id).Scan(&target); err != nil {
		t.Fatalf("reservation: %v", err)
	}
	if target != purgedTarget {
		t.Fatalf("former_ids row for the purged id names %q", target)
	}
}

// History written before reservations existed can hold put, delete, gc, put
// at one id, and its database has a live record and no reservation. A replay
// reserves the id at the gc entry and must release it at the create that
// follows, or the rebuilt fold refuses every put at a record the un-rebuilt
// database accepts.
func TestRebuildReleasesReservationUnderPreFixCreate(t *testing.T) {
	t.Parallel()
	ds := newRaceDataset(t)
	ctx := context.Background()
	const id = "widget-prefix"
	if _, err := ds.Put(ctx, substrate.ActorAPI, widgetInput(id, "first")); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := ds.Delete(ctx, substrate.ActorAPI, raceWidget, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("gc: %v", err)
	}
	// The engine that wrote this history never reserved the id; its changelog
	// is the same, so dropping the row is the pre-fix database exactly.
	if _, err := ds.db.ExecContext(ctx,
		`DELETE FROM former_ids WHERE record_kind = $1 AND former_id = $2`, raceWidget, id); err != nil {
		t.Fatalf("drop reservation: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, widgetInput(id, "second")); err != nil {
		t.Fatalf("pre-fix put after gc: %v", err)
	}

	before, err := ds.FoldSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.svc.RebuildRepository(ctx, "geoah"); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	after, err := ds.FoldSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("the rebuild diverged from the database:\n%s\n---\n%s", before, after)
	}
	err = ds.db.QueryRowContext(ctx,
		`SELECT 1 FROM former_ids WHERE record_kind = $1 AND former_id = $2`, raceWidget, id).Scan(new(int))
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("a reservation stands beside the live record: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, widgetInput(id, "third")); err != nil {
		t.Fatalf("put at the live record after rebuild: %v", err)
	}
}
