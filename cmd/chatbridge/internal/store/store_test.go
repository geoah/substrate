package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/cmd/chatbridge/internal/contract"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	st, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func ev(chat string) contract.Event {
	return &contract.ChatDelete{Head: contract.Head{Type: contract.TypeChatDelete}, Chat: chat}
}

// The function refuses a batch past a gap forever, so the sequence has
// none: not under concurrent writers, not after a rolled-back transaction.
func TestOutboxSequenceHasNoGaps(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for w := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 25 {
				fail := i%5 == 0
				err := st.Do(ctx, func(tx *Tx) error {
					if _, err := tx.Append(ctx, ev("x")); err != nil {
						return err
					}
					if fail {
						return errors.New("rolled back")
					}
					return nil
				})
				if err != nil && !fail {
					t.Errorf("writer %d: %v", w, err)
				}
			}
		}()
	}
	wg.Wait()
	rows, err := st.Pending(ctx, 0, Limits{Events: 1000, JSON: 1 << 30, Media: 1 << 40})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 8*20 {
		t.Fatalf("%d events, want %d", len(rows), 8*20)
	}
	for i, r := range rows {
		if r.Seq != int64(i+1) {
			t.Fatalf("event %d has seq %d", i, r.Seq)
		}
	}
}

func TestPendingCutsABatchAtItsLimits(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	for range 10 {
		if err := st.Do(ctx, func(tx *Tx) error { _, err := tx.Append(ctx, ev("chat")); return err }); err != nil {
			t.Fatal(err)
		}
	}
	rows, _ := st.Pending(ctx, 0, Limits{Events: 4, JSON: 1 << 20, Media: 1})
	if len(rows) != 4 || rows[0].Seq != 1 || rows[3].Seq != 4 {
		t.Fatalf("by count: %d rows", len(rows))
	}
	one := len(rows[0].Body) + 1
	rows, _ = st.Pending(ctx, 4, Limits{Events: 100, JSON: batchOverhead + 2*one, Media: 1})
	if len(rows) != 2 || rows[0].Seq != 5 {
		t.Fatalf("by bytes: %d rows", len(rows))
	}
	rows, _ = st.Pending(ctx, 4, Limits{Events: 100, JSON: 1})
	if len(rows) != 1 {
		t.Fatalf("an event over the limit alone still goes: %d rows", len(rows))
	}
	if err := st.Trim(ctx, 9); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.Depth(ctx, 0); n != 1 {
		t.Fatalf("%d left after trimming", n)
	}
}

func TestOneClaimPerAction(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = st.Do(ctx, func(tx *Tx) error {
				ok, err := tx.Claim(ctx, Claim{Action: "a", Attempt: RandomID(4), Claimed: true, ClaimedAt: time.Now()})
				if ok {
					mu.Lock()
					won++
					mu.Unlock()
				}
				return err
			})
		}()
	}
	wg.Wait()
	if won != 1 {
		t.Fatalf("%d claims landed", won)
	}
	set := func() bool {
		var changed bool
		_ = st.Do(ctx, func(tx *Tx) error {
			var err error
			changed, err = tx.SetOutcome(ctx, "a", contract.OutcomeDone, "m", "", "")
			return err
		})
		return changed
	}
	if !set() || set() {
		t.Fatal("an outcome was recorded twice")
	}
}

func TestMediaIsCollectedOnceNoEventNamesIt(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	info, err := st.PutMedia(ctx, []byte("bytes"), "image/png", "a.png")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Do(ctx, func(tx *Tx) error { _, err := tx.Append(ctx, ev("x"), info.ID); return err }); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	if n, _ := st.CollectMedia(ctx, future); n != 0 {
		t.Fatal("a file a pending event names was collected")
	}
	_ = st.Trim(ctx, 1)
	if n, _ := st.CollectMedia(ctx, future); n != 1 {
		t.Fatal("an unnamed file was kept")
	}
}
