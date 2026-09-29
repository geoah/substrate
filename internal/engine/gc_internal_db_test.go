package engine

// A GC pass reports the rows it purged, not the victims it visited. A put
// that restores a tombstone after the pass's victim query and before its row
// lock leaves a victim the pass reloads, finds live and skips; counting it
// made RunGC report a record it never collected and run one more pass than
// the sweep needed (#459).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

func TestGCPassCountsOnlyPurgedRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newRaceDataset(t)

	// Whatever the fixture tombstoned goes first, so the rounds below count
	// their own victims alone.
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatalf("drain gc: %v", err)
	}

	tombstone := func(id string) {
		t.Helper()
		if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: raceWidget, ID: id, Properties: map[string]any{"name": id},
		}); err != nil {
			t.Fatalf("%s: put: %v", id, err)
		}
		if _, err := ds.Delete(ctx, substrate.ActorAPI, raceWidget, id, substrate.DeleteInput{}); err != nil {
			t.Fatalf("%s: delete: %v", id, err)
		}
	}
	// sweep runs RunGC with the pass hook restoring the named ids after the
	// first victim query, and reports the count and the victims each pass
	// found.
	sweep := func(restore ...string) (int, []int) {
		t.Helper()
		var passes []int
		ds.svc.testGCPassHook = func(victims int) {
			passes = append(passes, victims)
			if len(passes) > 1 {
				return
			}
			for _, id := range restore {
				if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
					Kind: raceWidget, ID: id, Properties: map[string]any{"name": id + " again"},
				}); err != nil {
					t.Errorf("%s: restore under the sweep: %v", id, err)
				}
			}
		}
		defer func() { ds.svc.testGCPassHook = nil }()
		n, err := ds.RunGC(ctx)
		if err != nil {
			t.Fatalf("gc: %v", err)
		}
		return n, passes
	}
	rowState := func(id string) string {
		t.Helper()
		var tombstoned bool
		err := ds.db.QueryRowContext(ctx,
			`SELECT deleted_at IS NOT NULL FROM records WHERE kind = $1 AND id = $2`,
			raceWidget, id).Scan(&tombstoned)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return "absent"
		case err != nil:
			t.Fatalf("%s: read row: %v", id, err)
		case tombstoned:
			return "tombstoned"
		}
		return "live"
	}

	// The pass's only victim is restored under it: nothing is purged, so the
	// sweep reports zero and ends after that one pass.
	tombstone("restored-alone")
	n, passes := sweep("restored-alone")
	if n != 0 {
		t.Fatalf("sweep with its only victim restored reported %d collected, want 0", n)
	}
	if len(passes) != 1 || passes[0] != 1 {
		t.Fatalf("sweep with its only victim restored ran passes finding %v victims, want [1]", passes)
	}
	if got := rowState("restored-alone"); got != "live" {
		t.Fatalf("restored victim is %s, want live", got)
	}

	// Two victims, one restored: the sweep counts the one it purged, and
	// the purge is why it runs a second pass, which finds nothing.
	tombstone("restored")
	tombstone("purged")
	n, passes = sweep("restored")
	if n != 1 {
		t.Fatalf("sweep with one of two victims restored reported %d collected, want 1", n)
	}
	if len(passes) != 2 || passes[0] != 2 || passes[1] != 0 {
		t.Fatalf("sweep with one of two victims restored ran passes finding %v victims, want [2 0]", passes)
	}
	if got := rowState("restored"); got != "live" {
		t.Fatalf("restored victim is %s, want live", got)
	}
	if got := rowState("purged"); got != "absent" {
		t.Fatalf("unrestored victim is %s, want purged", got)
	}

	// A full batch, every victim of it restored: the pass purges nothing,
	// but the batch it filled may have left victims beyond it, so the sweep
	// runs another pass and collects the one tombstone past the batch.
	var batch []string
	for i := range gcBatch {
		id := fmt.Sprintf("batch-%03d", i)
		tombstone(id)
		batch = append(batch, id)
	}
	tombstone("past-the-batch")
	n, passes = sweep(batch...)
	if n != 1 {
		t.Fatalf("sweep with a full batch restored reported %d collected, want 1", n)
	}
	if len(passes) != 3 || passes[0] != gcBatch || passes[1] != 1 || passes[2] != 0 {
		t.Fatalf("sweep with a full batch restored ran passes finding %v victims, want [%d 1 0]", passes, gcBatch)
	}
	if got := rowState("past-the-batch"); got != "absent" {
		t.Fatalf("victim past the restored batch is %s, want purged", got)
	}
}
