package engine_test

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// Skeptic #1: does split destroy owner writes made AFTER the merge?
func TestSk1SplitRevertsPostMergeOwnerWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	winner := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Nina Ray"},
	})
	loser := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "N. Ray"},
		Labels:      map[string]any{"owner/shelf": "audio"},
		Annotations: map[string]any{"owner/note": "from loser"},
	})
	rec, err := ds.Merge(ctx, owner, winner.Kind, winner.ID, loser.ID)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	afterMerge := mustGet(t, ds, winner.Kind, winner.ID)
	t.Logf("after merge: labels=%v annotations=%v", afterMerge.Labels, afterMerge.Annotations)

	// The owner now curates the merged record: months of edits, say.
	mustPatch(t, ds, owner, winner.Kind, winner.ID, substrate.PatchInput{
		Labels:      map[string]any{"owner/shelf": "favorites"},
		Annotations: map[string]any{"owner/note": "curated after the merge"},
	})
	curated := mustGet(t, ds, winner.Kind, winner.ID)
	t.Logf("after owner curation: labels=%v annotations=%v", curated.Labels, curated.Annotations)
	if curated.Labels["owner/shelf"] != "favorites" || curated.Annotations["owner/note"] != "curated after the merge" {
		t.Fatalf("precondition failed: %v %v", curated.Labels, curated.Annotations)
	}

	if _, err := ds.Split(ctx, owner, rec.ID); err != nil {
		t.Fatalf("split: %v", err)
	}
	w := mustGet(t, ds, winner.Kind, winner.ID)
	l := mustGet(t, ds, loser.Kind, loser.ID)
	t.Logf("after split winner: labels=%v annotations=%v", w.Labels, w.Annotations)
	t.Logf("after split loser:  labels=%v annotations=%v", l.Labels, l.Annotations)

	if w.Labels["owner/shelf"] != "favorites" {
		t.Errorf("DATA LOSS: winner label owner/shelf = %v (owner wrote \"favorites\" after the merge)", w.Labels["owner/shelf"])
	}
	if w.Annotations["owner/note"] != "curated after the merge" {
		t.Errorf("DATA LOSS: winner annotation owner/note = %v", w.Annotations["owner/note"])
	}
}

// Same class, "overwritten" path: the winner had the key before the merge,
// the loser's newer value won, and the owner edits it after the merge.
func TestSk1SplitClobbersOverwrittenKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	winner := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Nina Ray"},
		Annotations: map[string]any{"owner/note": "winner original"},
	})
	loser := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "N. Ray"},
		Annotations: map[string]any{"owner/note": "loser newer"},
	})
	rec, err := ds.Merge(ctx, owner, winner.Kind, winner.ID, loser.ID)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	mustPatch(t, ds, owner, winner.Kind, winner.ID, substrate.PatchInput{
		Annotations: map[string]any{"owner/note": "curated after the merge"},
	})
	if _, err := ds.Split(ctx, owner, rec.ID); err != nil {
		t.Fatalf("split: %v", err)
	}
	w := mustGet(t, ds, winner.Kind, winner.ID)
	t.Logf("after split winner: annotations=%v", w.Annotations)
	if w.Annotations["owner/note"] != "curated after the merge" {
		t.Errorf("DATA LOSS: winner annotation owner/note = %v", w.Annotations["owner/note"])
	}
}
