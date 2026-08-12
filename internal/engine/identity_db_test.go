package engine_test

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// Merge and split are manual and owner-driven. Nothing fuses
// by value, so there is nothing else in this file.

func TestManualMergeMovesLabelsAndAnnotations(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)

	winner := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Nina Ray"},
		Labels:      map[string]any{"owner/pinned": true},
		Annotations: map[string]any{"owner/note": "first"},
	})
	loser := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "N. Ray"},
		Labels:      map[string]any{"owner/shelf": "audio"},
		Annotations: map[string]any{"owner/note": "second", "owner/extra": 7},
	})
	rec, err := ds.Merge(ctx, owner, winner.Kind, winner.ID, loser.ID)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	got := mustGet(t, ds, winner.Kind, winner.ID)
	if got.Labels["owner/shelf"] != "audio" || got.Labels["owner/pinned"] != true {
		t.Fatalf("labels = %v", got.Labels)
	}
	// Newest annotation wins; the loser's extra key moves over.
	if got.Annotations["owner/note"] != "second" {
		t.Fatalf("annotations = %v", got.Annotations)
	}
	if got.Annotations["owner/extra"] == nil {
		t.Fatalf("annotations = %v", got.Annotations)
	}
	if got.Properties["name"] != "Nina Ray" {
		t.Fatal("merge must not merge properties")
	}

	if _, err := ds.Split(ctx, owner, rec.ID); err != nil {
		t.Fatalf("split: %v", err)
	}
	w := mustGet(t, ds, winner.Kind, winner.ID)
	l := mustGet(t, ds, loser.Kind, loser.ID)
	if _, ok := w.Labels["owner/shelf"]; ok {
		t.Fatalf("split should strip moved labels: %v", w.Labels)
	}
	if w.Annotations["owner/note"] != "first" {
		t.Fatalf("split should restore the overwritten annotation: %v", w.Annotations)
	}
	if w.Annotations["owner/extra"] != nil {
		t.Fatalf("split should strip moved annotations: %v", w.Annotations)
	}
	if l.Annotations["owner/note"] != "second" || l.Annotations["owner/extra"] == nil {
		t.Fatalf("loser annotations not restored: %v", l.Annotations)
	}
	if l.DeletedAt != nil {
		t.Fatal("loser should be live again")
	}
	if got := mustGet(t, ds, loser.Kind, loser.ID); got.CanonicalID != "" {
		t.Fatalf("the loser answers to its own id again: %+v", got)
	}
}

func TestMergeLoserSurvivesGC(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)
	a := mustPut(t, ds, owner, substrate.PutInput{Kind: "organization", Properties: map[string]any{"name": "A"}})
	b := mustPut(t, ds, owner, substrate.PutInput{Kind: "organization", Properties: map[string]any{"name": "B"}})
	if _, err := ds.Merge(ctx, owner, a.Kind, a.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.RunGC(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Get(ctx, b.Kind, b.ID); err != nil {
		t.Fatalf("GC collected a merge loser: %v", err)
	}
}
