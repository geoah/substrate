package engine_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
)

// TestZZSK1SeqGap runs the connectors' exact cursor algorithm (Changes(after=cursor),
// cursor = max seq observed) against the real engine while concurrent writers
// commit. If bigserial + READ COMMITTED lets a lower seq commit after a higher
// one, the poller permanently skips rows.
func TestZZSK1SeqGap(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	if err := enginetest.InstallAccountType(context.Background(), ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}
	ctx := context.Background()

	const writers = 8
	const perWriter = 60
	total := writers * perWriter

	stop := make(chan struct{})
	observed := map[int64]bool{}
	var mu sync.Mutex
	var cursor int64

	var pollWG sync.WaitGroup
	pollWG.Add(1)
	go func() {
		defer pollWG.Done()
		for {
			select {
			case <-stop:
				// final drain
				for {
					chs, err := ds.Changes(ctx, cursor, substrate.ChangeFilter{}, 200)
					if err != nil || len(chs) == 0 {
						return
					}
					mx := cursor
					for _, c := range chs {
						mu.Lock()
						observed[c.Seq] = true
						mu.Unlock()
						if c.Seq > mx {
							mx = c.Seq
						}
					}
					cursor = mx
				}
			default:
			}
			chs, err := ds.Changes(ctx, cursor, substrate.ChangeFilter{}, 200)
			if err != nil {
				t.Errorf("changes: %v", err)
				return
			}
			mx := cursor
			for _, c := range chs {
				mu.Lock()
				observed[c.Seq] = true
				mu.Unlock()
				if c.Seq > mx {
					mx = c.Seq
				}
			}
			cursor = mx
			time.Sleep(time.Millisecond)
		}
	}()

	var wg sync.WaitGroup
	for w := range writers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := range perWriter {
				_, err := ds.Put(ctx, owner, substrate.PutInput{
					Kind:       enginetest.AccountType,
					ID:         extID("gmail.account", fmt.Sprintf("w%d-%d@acme.com", w, i)),
					Properties: map[string]any{"provider": "gmail", "label": "L", "status": "ok"},
				})
				if err != nil {
					t.Errorf("put: %v", err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	time.Sleep(300 * time.Millisecond)
	close(stop)
	pollWG.Wait()

	// Ground truth: every seq actually in the changelog.
	var all []int64
	var last int64
	for {
		chs, err := ds.Changes(ctx, last, substrate.ChangeFilter{}, 500)
		if err != nil {
			t.Fatalf("changes: %v", err)
		}
		if len(chs) == 0 {
			break
		}
		for _, c := range chs {
			all = append(all, c.Seq)
			last = c.Seq
		}
	}
	var missed []int64
	for _, s := range all {
		mu.Lock()
		ok := observed[s]
		mu.Unlock()
		if !ok {
			missed = append(missed, s)
		}
	}
	t.Logf("wrote %d records, changelog rows %d, poller observed %d, MISSED %d: %v",
		total, len(all), len(observed), len(missed), missed)
	if len(missed) > 0 {
		t.Fatalf("poller permanently skipped %d changelog rows: %v", len(missed), missed)
	}
}
