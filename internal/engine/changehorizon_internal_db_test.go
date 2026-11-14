package engine

// The tail's window obeys the horizon: rows it reads after a forgetting
// committed come with the horizon that forgetting raised, so a stream below
// it is refused from the window as from the table, never answered with the
// rows past its cursor that lack the deletion.

import (
	"context"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// A stream fed from the window, whose rows the window had not read yet when
// a forgetting committed. The window catches up after the commit and before
// the reset that follows it (compacted), which is where such a read lands:
// the test runs the forgetting's own transaction and leaves the reset out.
func TestAWindowFilledAfterAForgettingRefusesAStreamBelowIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// The forgetting needs a purged record's history, kept as a release
	// before the erasure kept it: a permanent delete's erasure is itself a
	// compaction, whose reset of the window this test leaves out.
	d := openInternalDataset(t, WithCompactionInterval(-1), WithTestPurgeKeepsHistory())
	tail := substrate.WithTailRead(ctx)
	const task = "samples.substrate.reamde.dev/tasks/task"
	put := func(name string) *substrate.Record {
		t.Helper()
		r, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: task, Properties: map[string]any{"name": name}})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	read := func(after int64) ([]substrate.Change, error) {
		return d.Changes(tail, after, substrate.ChangeFilter{Kinds: []string{task}}, 100)
	}

	put("before")
	// A stream opened at the head, fed from the window the read opens.
	if _, err := read(0); err != nil {
		t.Fatalf("open the window: %v", err)
	}
	d.tail.mu.Lock()
	ready, cursor := d.tail.ready, d.tail.from
	d.tail.mu.Unlock()
	if !ready || cursor == 0 {
		t.Fatalf("the window is ready %v from %d, want it open at the head", ready, cursor)
	}
	if chs, err := read(cursor); err != nil || len(chs) != 0 {
		t.Fatalf("the stream at the head read %+v (%v), want nothing", chs, err)
	}

	gone := put("gone")
	if _, err := d.Delete(ctx, substrate.ActorPerson, task, gone.ID, substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatal(err)
	}
	after := put("after")
	head, err := d.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The pass's own steps: the index of foreign references it builds
	// first, then one batch of the forgetting.
	if _, err := d.backfillForeign(ctx, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("index foreign references: %v", err)
	}
	refs, err := d.purgedRecords(ctx, 0, head.Seq-1, eref{})
	if err != nil || len(refs) != 1 {
		t.Fatalf("purged records %v (%v), want the one", refs, err)
	}
	if err := d.withCompactionTx(ctx, true, func(t *txn) error {
		_, err := t.forgetRecords(head.Seq-1, []eref{refs[0].ref})
		return err
	}); err != nil {
		t.Fatalf("forget: %v", err)
	}
	if head, err = d.Head(ctx); err != nil || head.Horizon <= cursor {
		t.Fatalf("the horizon is %d (%v), want one above the stream's cursor %d", head.Horizon, err, cursor)
	}

	if chs, err := read(cursor); !errors.Is(err, substrate.ErrStaleHistory) {
		t.Fatalf("the stream below the horizon read %+v (%v), want it refused as stale", chs, err)
	}
	d.tail.mu.Lock()
	from := d.tail.from
	d.tail.mu.Unlock()
	if cursor < from {
		t.Fatalf("the window moved from %d past the stream's cursor %d: the table answered it, not the window", from, cursor)
	}
	// A stream at the horizon is answered from the same window.
	chs, err := read(head.Horizon)
	if err != nil || len(chs) != 1 || chs[0].RecordID != after.ID {
		t.Fatalf("the stream at the horizon read %+v (%v), want the write after it", chs, err)
	}
}
