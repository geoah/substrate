//go:build erasememory

package engine

// The memory a permanent delete's erasure holds, measured once for #1452:
// a record of 20,000 lines of 50 KB each, deleted permanently, with the
// process's heap sampled while the erasure runs. Not part of the suite: it
// writes a gigabyte of history.

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

func TestTheMemoryAPermanentDeleteOfALongRecordHolds(t *testing.T) {
	ctx := context.Background()
	d := openInternalDataset(t, WithCompactionInterval(-1))
	const lines, size, perTx = 20000, 50 << 10, 100
	value := func() string {
		raw := make([]byte, size*3/4)
		if _, err := rand.Read(raw); err != nil {
			t.Fatal(err)
		}
		// A patch's line carries the old value and the new: half the size
		// each makes a line of about size.
		return base64.StdEncoding.EncodeToString(raw)[:size/2]
	}
	rec, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": value()}})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	for i := 0; i < lines; i += perTx {
		if err := d.inTx(ctx, substrate.ActorPerson, false, func(tx *txn) error {
			for range perTx {
				if _, err := tx.patch(eref{Kind: ledgerTask, ID: rec.ID}, substrate.PatchInput{Properties: map[string]any{"name": value()}}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	var stored, n int64
	if err := d.db.QueryRowContext(ctx, `SELECT count(*), sum(pg_column_size(payload)) FROM changelog WHERE kind = $1 AND record_id = $2`,
		ledgerTask, rec.ID).Scan(&n, &stored); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %d lines, %d MiB stored, in %s", n, stored>>20, time.Since(started).Round(time.Second))
	measureErasure(t, d, ledgerTask, rec.ID, nil)
}

// The same for a trigger of a long delivery history, the ticket's example:
// lines deliveries of size bytes each, written as parked failures retired
// in the next transaction (largeDeliveries), and the trigger deleted
// permanently.
var (
	eraseMemorySmall = flag.Bool("erasememory.small", false, "write a short trigger history, to try the harness")
	eraseMemoryScale = flag.Int("erasememory.scale", 1, "how many times the trigger's delivery history to write")
)

// stepPeaks is the heap's peak between two steps of an erasure, beside what
// each step read (sawBatch tells of a step after it commits).
type stepPeaks struct {
	window atomic.Uint64
	mu     sync.Mutex
	steps  []stepPeak
}

type stepPeak struct {
	kind                   string
	ids                    int
	lines                  int
	bytes, largest, second int64
	peak                   uint64
}

func (p *stepPeaks) saw(heap uint64) {
	for {
		cur := p.window.Load()
		if heap <= cur || p.window.CompareAndSwap(cur, heap) {
			return
		}
	}
}

func (p *stepPeaks) step(kind string, ids []string, lines int, bytes, largest, second int64) {
	peak := p.window.Swap(0)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.steps = append(p.steps, stepPeak{kind: kind, ids: len(ids), lines: lines, bytes: bytes, largest: largest, second: second, peak: peak})
}

func TestTheMemoryAPermanentDeleteOfALongTriggerHolds(t *testing.T) {
	ctx := context.Background()
	peaks := &stepPeaks{}
	// The archive is held off, so the heap is the erasure's and the pass's.
	d := openInternalDataset(t, WithCompactionInterval(-1), WithArchiveInterval(time.Hour), WithTestCompactionBytes(0, peaks.step))
	installLedgerConnector(t, d, nowUTC().Add(-time.Hour).Truncate(time.Minute))
	lines, size := 4000**eraseMemoryScale, 100<<10
	if *eraseMemorySmall {
		lines, size = 40, 1<<10
	}
	started := time.Now()
	sizes := make([]int, lines)
	for i := range sizes {
		sizes[i] = size
	}
	largeDeliveries(t, d, ledgerHourly, sizes...)
	var stored, n int64
	if err := d.db.QueryRowContext(ctx, `SELECT count(*), sum(pg_column_size(payload)) FROM changelog WHERE kind = $1 AND record_id = $2`,
		typeTrigger, ledgerHourly).Scan(&n, &stored); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %d lines, %d MiB stored, in %s", n, stored>>20, time.Since(started).Round(time.Second))
	measureErasure(t, d, typeTrigger, ledgerHourly, peaks)
	steps := slices.Clone(peaks.steps)
	slices.SortFunc(steps, func(a, b stepPeak) int { return cmp.Compare(b.peak, a.peak) })
	t.Logf("%d steps", len(steps))
	for i, s := range steps[:min(5, len(steps))] {
		t.Logf("peak %d: %d MiB, in a batch of %d %s records that read %d lines, %d KiB stored, the two largest %d and %d KiB",
			i+1, s.peak>>20, s.ids, s.kind, s.lines, s.bytes>>10, s.largest>>10, s.second>>10)
	}
}

// measureErasure deletes id permanently and runs passes until its erasure
// is done, sampling the process's heap the while.
func measureErasure(t *testing.T, d *dataset, kind, id string, peaks *stepPeaks) {
	t.Helper()
	ctx := context.Background()
	runtime.GC()
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
	var peakHeap, peakSys atomic.Uint64
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			if m.HeapAlloc > peakHeap.Load() {
				peakHeap.Store(m.HeapAlloc)
			}
			if m.Sys > peakSys.Load() {
				peakSys.Store(m.Sys)
			}
			if peaks != nil {
				peaks.saw(m.HeapAlloc)
			}
			select {
			case <-done:
				return
			case <-tick.C:
			}
		}
	}()
	started := time.Now()
	deleted, err := d.Delete(ctx, substrate.ActorPerson, kind, id, substrate.DeleteInput{Purge: true})
	if err != nil {
		t.Fatalf("permanent delete: %v", err)
	}
	pending := deleted.Erasure != nil && deleted.Erasure.Pending
	t.Logf("the permanent delete's request took %s, its erasure %+v", time.Since(started).Round(time.Millisecond), deleted.Erasure)
	passes := 0
	for ; pending && passes < 1000; passes++ {
		if _, err := d.CompactionPass(ctx); err != nil {
			t.Logf("pass %d failed: %v", passes+1, err)
			break
		}
		p, err := d.pendingErasures(ctx, d.db, []eref{{Kind: kind, ID: id}})
		if err != nil {
			t.Fatal(err)
		}
		pending = len(p) > 0
	}
	took := time.Since(started)
	close(done)
	wg.Wait()
	var left int64
	if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM changelog WHERE kind = $1 AND record_id = $2`, kind, id).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if *eraseMemorySmall {
		for _, r := range rowsOf(t, d, `SELECT seq || ' ' || op || ' ' || (SELECT string_agg(e->>'kind', ',') FROM jsonb_array_elements(payload->'fold') e) || ' ' || pg_column_size(payload) FROM changelog WHERE kind = $1 AND record_id = $2 ORDER BY seq LIMIT 12`, kind, id) {
			t.Logf("left: %s", r)
		}
	}
	var leftBytes int64
	if err := d.db.QueryRowContext(ctx, `SELECT coalesce(sum(pg_column_size(payload)), 0) FROM changelog WHERE kind = $1 AND record_id = $2`, kind, id).Scan(&leftBytes); err != nil {
		t.Fatal(err)
	}
	t.Logf("the erasure took %s and %d passes, pending %v, %d lines of it left, %d KiB; heap before %d MiB, peak heap %d MiB, peak sys %d MiB",
		took.Round(time.Millisecond), passes, pending, left, leftBytes>>10, base.HeapAlloc>>20, peakHeap.Load()>>20, peakSys.Load()>>20)
}

// What the lines the winner of 500 merges retains come to: every step of
// its erasure reads them, and the bounds count them.
func TestWhatTheWinnerOf500MergesRetains(t *testing.T) {
	ctx := context.Background()
	d := openInternalDataset(t, WithCompactionInterval(-1))
	winner, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": "winner"}})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 500 {
		loser, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": "a loser of a merge, with a name of some length"}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.Merge(ctx, substrate.ActorPerson, substrate.MergeInput{Kind: ledgerTask, Winner: winner.ID, Loser: loser.ID}); err != nil {
			t.Fatalf("merge %d: %v", i, err)
		}
	}
	if _, err := d.Forget(ctx, substrate.ActorPerson, ledgerTask, winner.ID, substrate.ForgetInput{Finish: true}); err != nil {
		t.Fatalf("forget: %v", err)
	}
	for range 100 {
		if p, _ := d.pendingErasures(ctx, d.db, nil); len(p) == 0 {
			break
		}
		if _, err := d.eraseNow(ctx, nil, true, time.Time{}, nil); err != nil {
			t.Fatal(err)
		}
	}
	sizes, err := d.recordLines(ctx, ledgerTask, []string{winner.ID}, 1<<62)
	if err != nil {
		t.Fatal(err)
	}
	s := sizes[winner.ID]
	t.Logf("after its erasure the winner of 500 merges keeps %d lines, %d bytes stored, the largest %d", s.lines, s.bytes, s.largest)
}

// The time a forget request takes on a record of n lines of 10 KB, at two
// sizes: the door reads the record before its first step (erase.go
// unerasableLive), the step itself is held to the bounds, so what grows
// with the record is the door's.
func TestTheTimeAForgetRequestOfALongRecordTakes(t *testing.T) {
	ctx := context.Background()
	for _, lines := range []int{4000, 8000} {
		d := openInternalDataset(t, WithCompactionInterval(-1), WithArchiveInterval(time.Hour))
		rec, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": "a long record"}})
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < lines; i += 100 {
			if err := d.inTx(ctx, substrate.ActorPerson, false, func(tx *txn) error {
				for range 100 {
					raw := make([]byte, 10<<10*3/4)
					if _, err := rand.Read(raw); err != nil {
						return err
					}
					if _, err := tx.patch(eref{Kind: ledgerTask, ID: rec.ID}, substrate.PatchInput{Properties: map[string]any{"name": base64.StdEncoding.EncodeToString(raw)[:5<<10]}}); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}
		var stored int64
		if err := d.db.QueryRowContext(ctx, `SELECT sum(pg_column_size(payload)) FROM changelog WHERE kind = $1 AND record_id = $2`, ledgerTask, rec.ID).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		e, err := d.Forget(ctx, substrate.ActorPerson, ledgerTask, rec.ID, substrate.ForgetInput{})
		if err != nil {
			t.Fatal(err)
		}
		took := time.Since(started)
		started = time.Now()
		var why string
		if err := d.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
			var err error
			why, err = tx.unerasableLive(eref{Kind: ledgerTask, ID: rec.ID})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		door := time.Since(started)
		started = time.Now()
		if err := d.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
			_, err := tx.mergeLines(eref{Kind: ledgerTask, ID: rec.ID}, 1<<62)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		merges := time.Since(started)
		var steps []string
		for range 3 {
			started = time.Now()
			if _, err := d.eraseNow(ctx, nil, true, time.Time{}, nil); err != nil {
				t.Fatal(err)
			}
			steps = append(steps, time.Since(started).Round(time.Millisecond).String())
		}
		t.Logf("%d lines, %d MiB stored: the forget request took %s (pending %v); its door's read alone %s (%q); the merge lines' read alone %s; the next steps alone %v",
			lines+1, stored>>20, took.Round(time.Millisecond), e.Pending, door.Round(time.Millisecond), why, merges.Round(time.Millisecond), steps)
	}
}
