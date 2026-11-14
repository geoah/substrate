package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// An erasure keeps the deletion's line, so a permanent delete moves no
// horizon: a stream held below that line and a forward page from below it
// both read on and meet the deletion, with no value. Only once the pass
// forgets the line, past the deletion period, does the horizon rise to it
// and refuse the same positions.
func TestAPermanentDeleteMovesNoHorizonUntilItsLineIsForgotten(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clock := time.Now().UTC()
	f := newShippedFixture(t, WithCompactionInterval(-1), WithDeletionsKept(time.Hour),
		WithTestCompactionClock(func() time.Time { return clock }))
	d, task := f.ds, f.box()
	filter := substrate.ChangeFilter{Kinds: []string{task}}
	put := func(name string) *substrate.Record {
		t.Helper()
		r, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: task, Properties: map[string]any{"name": name}})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	readers := map[string]func(after int64) ([]substrate.Change, error){
		"a stream": func(after int64) ([]substrate.Change, error) {
			return d.Changes(substrate.WithTailRead(ctx), after, filter, 100)
		},
		"a forward page": func(after int64) ([]substrate.Change, error) {
			return d.Changes(ctx, after, filter, 100)
		},
	}

	put("before")
	if _, err := readers["a stream"](0); err != nil {
		t.Fatalf("open the stream: %v", err)
	}
	head, err := d.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cursor := head.Seq
	gone := put("erasable-gone")
	deleted, err := d.Delete(ctx, substrate.ActorPerson, task, gone.ID, substrate.DeleteInput{Purge: true})
	if err != nil || deleted.Erasure == nil || deleted.Erasure.Pending {
		t.Fatalf("the permanent delete: %+v (%v), want its erasure done", deleted, err)
	}
	// A write after it: the pass never forgets a deletion from the head.
	put("after")
	if head, err = d.Head(ctx); err != nil || head.Horizon > cursor {
		t.Fatalf("the horizon is %d (%v) after the permanent delete, want it at or below the stream's %d", head.Horizon, err, cursor)
	}
	for name, read := range readers {
		chs, err := read(cursor)
		if err != nil {
			t.Fatalf("%s below the permanent delete: %v, want it served", name, err)
		}
		met := false
		for _, c := range chs {
			met = met || (c.RecordID == gone.ID && c.Op == substrate.OpGC)
		}
		raw, _ := json.Marshal(chs)
		if !met || strings.Contains(string(raw), "erasable-gone") {
			t.Fatalf("%s below the permanent delete read %s, want the deletion with no value", name, raw)
		}
	}

	clock = time.Now().UTC().Add(2 * time.Hour)
	if report, err := d.CompactionPass(ctx); err != nil || report.Forgotten == 0 {
		t.Fatalf("the pass past the deletion period forgot %d (%v), want the deletion", report.Forgotten, err)
	}
	if head, err = d.Head(ctx); err != nil || head.Horizon <= cursor {
		t.Fatalf("the horizon is %d (%v) once the deletion is forgotten, want it above the stream's %d", head.Horizon, err, cursor)
	}
	for name, read := range readers {
		if chs, err := read(cursor); !errors.Is(err, substrate.ErrStaleHistory) {
			t.Fatalf("%s below the forgotten deletion read %+v (%v), want it told to list again", name, chs, err)
		}
	}
}
