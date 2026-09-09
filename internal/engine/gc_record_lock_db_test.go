package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// Which racer the barrier lets reach the tombstone's row first.
type sweepRound int

const (
	// freeRound races the sweep and the put with no barrier.
	freeRound sweepRound = iota
	// sweepFirst parks the sweep on the row, probes its locks, queues the put
	// behind it, then releases: the sweep purges and the put creates.
	sweepFirst
	// putFirst parks the put on the row, queues the sweep behind it, then
	// releases: the put restores and the sweep skips the live row.
	putFirst
)

// The sweep and a put at the id it is purging never overlap: inTx takes the
// changelog lock before either body runs, so every round ends one of two ways
// and never between them: the put restores the tombstone and the sweep finds
// a live row, or the sweep purges and the put creates a fresh record. Neither
// side errors. The two parked shapes force one history each, so both are
// proven every run; the free rounds may land on either. The outcome set is
// the same without the lock in gcPass, because the changelog lock alone
// serializes these two; the sweepFirst probe is what fails there.
func TestGCSweepSerializesWithPutAtSameID(t *testing.T) {
	t.Parallel()
	ds := newRaceDataset(t)
	outcomes := map[string]int{}
	for i := range 12 {
		shape := sweepRound(i % 3)
		got := raceSweepAgainstPut(t, ds, fmt.Sprintf("widget-race-%d", i), shape)
		switch {
		case shape == sweepFirst && got != "purged then created":
			t.Fatalf("round %d: the sweep reached the row first and the history is %q", i, got)
		case shape == putFirst && got != "restored":
			t.Fatalf("round %d: the put reached the row first and the history is %q", i, got)
		}
		outcomes[got]++
	}
	t.Logf("outcomes: %v", outcomes)
	if outcomes["restored"] == 0 || outcomes["purged then created"] == 0 {
		t.Fatalf("both histories must occur in one run, got %v", outcomes)
	}
}

// raceSweepAgainstPut runs one round at one id and names the history it
// produced. In a parked round a barrier transaction holds the tombstone's row
// lock until the first racer is parked on it and the second is queued behind
// the first on the changelog lock. Every wait polls the server's lock tables
// under a deadline (waitUntil, waitParkedOn), because a sleep cannot show that
// a racer reached its wait, and the barrier rolls back on every exit so a
// failed assertion cannot leave a racer blocked behind it.
func raceSweepAgainstPut(t *testing.T, ds *dataset, id string, shape sweepRound) string {
	t.Helper()
	ctx := context.Background()
	put := func(name string) error {
		_, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: raceWidget, ID: id, Properties: map[string]any{"name": name},
		})
		return err
	}
	if err := put("first"); err != nil {
		t.Fatalf("%s: put: %v", id, err)
	}
	if _, err := ds.Delete(ctx, substrate.ActorAPI, raceWidget, id, substrate.DeleteInput{}); err != nil {
		t.Fatalf("%s: delete: %v", id, err)
	}
	gcDone, putDone := make(chan error, 1), make(chan error, 1)
	startSweep := func() {
		go func() {
			_, err := ds.RunGC(ctx)
			gcDone <- err
		}()
	}
	startPut := func() { go func() { putDone <- put("second") }() }

	if shape == freeRound {
		startSweep()
		startPut()
	} else {
		var barrier *sql.Tx
		var barrierPID int
		var err error
		if barrier, err = ds.db.BeginTx(ctx, nil); err != nil {
			t.Fatalf("%s: barrier: %v", id, err)
		}
		defer func() { _ = barrier.Rollback() }()
		if err := barrier.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&barrierPID); err != nil {
			t.Fatalf("%s: barrier pid: %v", id, err)
		}
		if _, err := barrier.ExecContext(ctx,
			`SELECT id FROM records WHERE kind = $1 AND id = $2 FOR UPDATE`, raceWidget, id); err != nil {
			t.Fatalf("%s: barrier lock: %v", id, err)
		}
		if shape == sweepFirst {
			startSweep()
		} else {
			startPut()
		}
		waitUntil(t, id+": the first racer to park on the row", func() bool {
			return blockedBy(t, ds, barrierPID) >= 1
		})
		if shape == sweepFirst {
			// lockRecord precedes loadRow, so the parked sweep holds the
			// record's advisory lock, the lock an addressed write takes before
			// its own row lock (checkID, lockCanonical).
			if tryLockFree(t, ds, "record|"+eref{Kind: raceWidget, ID: id}.key()) {
				t.Fatalf("%s: the parked sweep does not hold the record's advisory lock", id)
			}
			startPut()
		} else {
			startSweep()
		}
		// The second racer queues on the changelog lock inTx takes first.
		if err := waitParkedOn(t, ds, changelogLockKey); err != nil {
			t.Fatalf("%s: the second racer did not queue behind the first: %v", id, err)
		}
		_ = barrier.Rollback()
	}
	if err := <-gcDone; err != nil {
		t.Fatalf("%s: gc: %v", id, err)
	}
	if err := <-putDone; err != nil {
		t.Fatalf("%s: put racing the sweep: %v", id, err)
	}
	return assertSweepOutcome(t, ds, id)
}

// blockedBy counts the sessions waiting on a lock the given backend holds.
func blockedBy(t *testing.T, ds *dataset, pid int) int {
	t.Helper()
	var n int
	if err := ds.db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid))`, pid).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// assertSweepOutcome checks one round against the two admissible histories
// and names the one it found: exactly one live row at the id, whose version
// and changelog entries agree on which side won.
func assertSweepOutcome(t *testing.T, ds *dataset, id string) string {
	t.Helper()
	ctx := context.Background()
	var live, total int
	if err := ds.db.QueryRowContext(ctx, `
		SELECT count(*) FILTER (WHERE deleted_at IS NULL), count(*)
		FROM records WHERE kind = $1 AND id = $2`, raceWidget, id).Scan(&live, &total); err != nil {
		t.Fatalf("%s: rows: %v", id, err)
	}
	if live != 1 || total != 1 {
		t.Fatalf("%s: %d live of %d rows at the id, want exactly one live", id, live, total)
	}
	var version int64
	if err := ds.db.QueryRowContext(ctx,
		`SELECT version FROM records WHERE kind = $1 AND id = $2`, raceWidget, id).Scan(&version); err != nil {
		t.Fatalf("%s: version: %v", id, err)
	}

	rows, err := ds.db.QueryContext(ctx,
		`SELECT op, payload FROM changelog WHERE kind = $1 AND record_id = $2 ORDER BY seq`, raceWidget, id)
	if err != nil {
		t.Fatalf("%s: changelog: %v", id, err)
	}
	defer func() { _ = rows.Close() }()
	var ops []string
	var last *rowDelta
	for rows.Next() {
		var op string
		var payload []byte
		if err := rows.Scan(&op, &payload); err != nil {
			t.Fatalf("%s: scan: %v", id, err)
		}
		ops = append(ops, op)
		var body map[string]json.RawMessage
		if err := json.Unmarshal(payload, &body); err != nil {
			t.Fatalf("%s: payload: %v", id, err)
		}
		var effects []foldOp
		if raw, ok := body[foldPayloadKey]; ok {
			if err := json.Unmarshal(raw, &effects); err != nil {
				t.Fatalf("%s: fold effects: %v", id, err)
			}
		}
		last = nil
		for _, ef := range effects {
			if ef.Kind == foldRecord {
				last = ef.Delta
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: changelog rows: %v", id, err)
	}

	put, del, gc := string(substrate.OpPut), string(substrate.OpDelete), string(substrate.OpGC)
	switch {
	case slices.Equal(ops, []string{put, del, put}):
		// The put won: create, delete, restore moved the same row three times.
		if version != 3 || last == nil || !last.Restored {
			t.Fatalf("%s: restore history %v with version %d and delta %+v", id, ops, version, last)
		}
		return "restored"
	case slices.Equal(ops, []string{put, del, gc, put}):
		// The sweep won: the put created a fresh row after the purge.
		if version != 1 || last == nil || !last.Created {
			t.Fatalf("%s: purge history %v with version %d and delta %+v", id, ops, version, last)
		}
		return "purged then created"
	}
	t.Fatalf("%s: history %v is neither a restore nor a purge followed by a create", id, ops)
	return ""
}
