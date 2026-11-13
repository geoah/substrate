package engine

// The measured-value checks cost nothing where nothing they judge was
// written: a put, a patch, a delete and a purge of a plain kind run none of
// them, in a repository with observed points of another kind as well, and
// the same counter sees them run once a write touches the unit graph or a
// kind that holds a segment. The dataset learns the kinds holding segments
// at open, so the first delete after it asks nothing either.

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// measureLog records the checks the test's own writes ran, by actor, so a
// background pass of the same service is not counted.
type measureLog struct {
	mu     sync.Mutex
	checks []string
}

func (l *measureLog) hook(actor substrate.Actor, check string) {
	if actor != substrate.ActorAPI {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.checks = append(l.checks, check)
}

func (l *measureLog) take() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.checks
	l.checks = nil
	return out
}

func TestAWriteOfAPlainKindRunsNoMeasuredValueCheck(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var log measureLog
	ds := newRaceDataset(t, func(o *options) { o.measureHook = log.hook })
	_ = log.take()

	rec := racePut(t, ds, map[string]any{"name": "plain"})
	if _, err := ds.Patch(ctx, substrate.ActorAPI, raceWidget, rec.ID, substrate.PatchInput{
		Properties: map[string]any{"name": "still plain"},
	}); err != nil {
		t.Fatalf("patch: %v", err)
	}
	if _, err := ds.Delete(ctx, substrate.ActorAPI, raceWidget, rec.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	gone := racePut(t, ds, map[string]any{"name": "purged"})
	if _, err := ds.Delete(ctx, substrate.ActorAPI, raceWidget, gone.ID, substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if got := log.take(); len(got) != 0 {
		t.Fatalf("writes of a kind with no measured values ran %q, want none", got)
	}

	// The counter is live: a unit write runs the unit graph's check.
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: vocabulary.KindUnit, ID: "zzmeasure",
		Properties: map[string]any{"name": "measure test unit", "code": "[zzmeasure]"},
	}); err != nil {
		t.Fatalf("put a unit: %v", err)
	}
	if got := log.take(); !slices.Contains(got, measureUnitGraph) {
		t.Fatalf("a unit write ran %q, want the unit graph's check among them", got)
	}

	// A repository holding observed points of another kind is no different.
	ds.observedKinds.note("other.test.dev/other/reading")
	other := racePut(t, ds, map[string]any{"name": "other"})
	if _, err := ds.Delete(ctx, substrate.ActorAPI, raceWidget, other.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := log.take(); len(got) != 0 {
		t.Fatalf("a delete of a plain kind beside another kind's observed points ran %q, want none", got)
	}

	// A kind that holds a segment, written while it bound `observed`, still
	// asks whether the record leaving the fold left one behind.
	ds.observedKinds.note(raceWidget)
	held := racePut(t, ds, map[string]any{"name": "held"})
	if _, err := ds.Delete(ctx, substrate.ActorAPI, raceWidget, held.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := log.take(); !slices.Equal(got, []string{measureObservedProbe}) {
		t.Fatalf("a delete of a kind holding a segment ran %q, want the probe alone", got)
	}
}
