package engine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// A filter NARROWS. `types` and `implements` intersect — they never union —
// because a COLLECTION read forces filter.types from the path, so a
// union let `/tasks?filter={"implements":"temporal"}` answer with every
// temporal row in the repository: transcripts, calendar events, rows of types the
// caller never addressed.
func TestListIntersectsTypesAndImplements(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)

	task := mustPut(t, ds, substrate.ActorAPI, substrate.PutInput{
		Kind: "tasks.substrate.geoah.me/task",
		Properties: map[string]any{
			"title": "send the rack layout", "dueAt": "2026-08-04T09:00:00Z",
		},
	})
	transcript := mustPut(t, ds, substrate.ActorAPI, substrate.PutInput{
		Kind: "calendar.substrate.geoah.me/transcript",
		Properties: map[string]any{
			"title": "the standup", "text": "…",
			"at": "2026-08-03T09:00:00Z", "endsAt": "2026-08-03T09:30:00Z",
		},
	})

	// The collection read: the path names the type, so nothing outside it may
	// come back, however broad the trait is.
	page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Kinds: []string{"tasks.substrate.geoah.me/task"}, Implements: "temporal",
	}})
	if err != nil {
		t.Fatalf("collection read with implements: %v", err)
	}
	if len(page.Records) != 1 || page.Records[0].ID != task.ID {
		t.Fatalf("collection read returned %v, want only the task %s", ids(page.Records), task.ID)
	}
	for _, e := range page.Records {
		if e.Kind != "tasks.substrate.geoah.me/task" {
			t.Fatalf("collection read returned a %s — a collection never answers outside itself", e.Kind)
		}
	}

	// The REPOSITORY-wide read: `implements` alone still means every implementor,
	// which is what the trait-records resource asks for.
	page, err = ds.List(ctx, substrate.Query{Filter: substrate.Filter{Implements: "temporal"}})
	if err != nil {
		t.Fatalf("repository-wide implements read: %v", err)
	}
	got := map[string]bool{}
	for _, e := range page.Records {
		got[e.ID] = true
	}
	if !got[task.ID] || !got[transcript.ID] {
		t.Fatalf("repository-wide implements read = %v, want both implementors", ids(page.Records))
	}
}

// The intersection is named when it is empty: a collection that does not
// implement the trait is a caller mistake, and an empty page would read as
// "nothing matched" rather than "nothing could".
func TestListRefusesATypeThatDoesNotImplement(t *testing.T) {
	ctx := context.Background()
	_, ds := newDataset(t)
	mustPut(t, ds, substrate.ActorAPI, substrate.PutInput{
		Kind: "people.substrate.geoah.me/person", Properties: map[string]any{"name": "Ada"},
	})

	_, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{
		Kinds: []string{"people.substrate.geoah.me/person"}, Implements: "temporal",
	}})
	if !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("error = %v, want a validation error naming the mismatch", err)
	}
}
