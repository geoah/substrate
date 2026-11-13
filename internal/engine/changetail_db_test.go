package engine_test

// A stream following the tail is answered from the repository's one shared
// read, and that read keeps the order clients resume by: a walk through it
// under eight concurrent writers sees every seq the changelog holds, as the
// table's own read does (TestChangesCursorWalkSkipsNoSeqUnderConcurrentWriters).

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
)

func TestTheTailSkipsNoSeqUnderConcurrentWriters(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	ctx := context.Background()
	if err := enginetest.InstallAccountType(ctx, ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}
	tail := substrate.WithTailRead(ctx)

	const writers = 8
	const perWriter = 40
	var cursor int64
	observed := map[int64]bool{}
	walk := func() {
		for {
			chs, err := ds.Changes(tail, cursor, substrate.ChangeFilter{}, 200)
			if err != nil {
				t.Errorf("tail changes: %v", err)
				return
			}
			if len(chs) == 0 {
				return
			}
			for _, c := range chs {
				if c.Seq <= cursor {
					t.Errorf("seq %d after cursor %d: the tail went back", c.Seq, cursor)
				}
				observed[c.Seq] = true
				cursor = c.Seq
			}
		}
	}
	walk()

	stop := make(chan struct{})
	walked := make(chan struct{})
	go func() {
		defer close(walked)
		for {
			select {
			case <-stop:
				walk()
				return
			default:
				walk()
			}
		}
	}()
	var wg sync.WaitGroup
	for w := range writers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := range perWriter {
				if _, err := ds.Put(ctx, owner, substrate.PutInput{
					Kind:       enginetest.AccountType,
					ID:         fid(extID("gmail.account", fmt.Sprintf("tail%d-%d@acme.com", w, i))),
					Properties: map[string]any{"provider": "gmail", "label": "L", "status": "ok"},
				}); err != nil {
					t.Errorf("put: %v", err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(stop)
	<-walked

	var missed []int64
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
			if !observed[c.Seq] {
				missed = append(missed, c.Seq)
			}
			last = c.Seq
		}
	}
	if len(missed) > 0 {
		t.Fatalf("the tail skipped %d seqs the changelog holds, first %d", len(missed), missed[0])
	}
}

// A commit whose answer was lost after it landed signals nothing, and a
// stream resumed from its last cursor still reads the write, with no other
// write after it to move the tail.
func TestTheTailReadsAWriteWhoseCommitAnswerWasLost(t *testing.T) {
	t.Parallel()
	var lose atomic.Bool
	_, ds := newDataset(t, engine.WithTestCommitInDoubt(lose.Load))
	ctx := context.Background()
	if err := enginetest.InstallAccountType(ctx, ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}
	tail := substrate.WithTailRead(ctx)
	put := func(email string) error {
		_, err := ds.Put(ctx, owner, substrate.PutInput{
			Kind:       enginetest.AccountType,
			ID:         fid(extID("gmail.account", email)),
			Properties: map[string]any{"provider": "gmail", "label": "L", "status": "ok"},
		})
		return err
	}
	if err := put("warm@acme.com"); err != nil {
		t.Fatalf("put: %v", err)
	}
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

	lose.Store(true)
	err := put("lost@acme.com")
	lose.Store(false)
	if !errors.Is(err, substrate.ErrWriteInDoubt) {
		t.Fatalf("the put whose answer was lost = %v, want a write in doubt", err)
	}
	chs, err := ds.Changes(tail, cursor, substrate.ChangeFilter{}, 200)
	if err != nil {
		t.Fatalf("tail changes: %v", err)
	}
	if len(chs) == 0 || chs[len(chs)-1].RecordID != fid(extID("gmail.account", "lost@acme.com")) {
		t.Fatalf("the stream resumed at %d read %d rows, want the write whose answer was lost", cursor, len(chs))
	}
}
