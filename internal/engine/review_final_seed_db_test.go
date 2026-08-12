package engine

// Final review #8, re-keyed by ticket 001: record identity is the (kind, id)
// pair, so `default` is an ordinary id OF THE LLMPROVIDER KIND — no reservation
// spans kinds anymore. A row of a DIFFERENT kind at that id is no collision:
// seeding proceeds beside it and both rows stay readable by their own
// (kind, id). What still holds is create-only on the llmprovider kind itself: a
// same-kind row at the seed id — an owner's live re-pointing or a deliberate
// tombstone — is left exactly as the owner had it.

import (
	"context"
	"testing"
)

// A row of ANOTHER kind at the seed id is no collision: seeding mints the
// llmprovider row beside it, and each row answers to its own (kind, id).
func TestFinalSeedCoexistsWithOtherTypeAtSeedID(t *testing.T) {
	ctx := context.Background()
	ds := openInternalDataset(t)
	// Simulate the pre-re-key repository the old reservation guarded against: the
	// id `default` is held by a NON-llmprovider record, and no provider row exists.
	if _, err := ds.db.ExecContext(ctx, `
		UPDATE records SET kind = $2, title = 'kept', props = '{"title":"kept"}'::jsonb
		WHERE kind = $3 AND id = $1`, providerSeedID, "tasks.substrate.reamde.dev/task", typeProvider); err != nil {
		t.Fatalf("fabricate other-kind occupant: %v", err)
	}
	// Re-seeding proceeds: a different-kind occupant is not a conflict.
	if err := ds.seedAgentDefaults(ctx); err != nil {
		t.Fatalf("seeding beside a different-kind row failed: %v", err)
	}
	// The provider row exists again, seeded fresh…
	provider, err := ds.Get(ctx, typeProvider, providerSeedID)
	if err != nil {
		t.Fatalf("the seeded llmprovider row: %v", err)
	}
	if provider.Properties["wire"] != "openai" || provider.Properties["name"] != providerSeedID {
		t.Fatalf("seeded llmprovider row wrong: %+v", provider.Properties)
	}
	// …and the other-kind occupant coexists untouched at the same id.
	task, err := ds.Get(ctx, "tasks.substrate.reamde.dev/task", providerSeedID)
	if err != nil {
		t.Fatalf("the coexisting non-provider row: %v", err)
	}
	if task.Properties["title"] != "kept" {
		t.Fatalf("seeding disturbed the other-kind row: %+v", task.Properties)
	}
}

// A same-kind row is preserved: an owner's re-pointing stands and a deliberate
// tombstone is not resurrected — re-seeding is a no-op on both.
func TestFinalSeedSameTypePreserved(t *testing.T) {
	ctx := context.Background()
	ds := openInternalDataset(t)
	// Owner re-points `default` at their own gateway.
	if _, err := ds.db.ExecContext(ctx, `
		UPDATE records SET props = jsonb_set(props, '{baseURL}', '"https://gateway.example.com/v1"')
		WHERE kind = $2 AND id = $1`, providerSeedID, typeProvider); err != nil {
		t.Fatalf("re-point default: %v", err)
	}
	if err := ds.seedAgentDefaults(ctx); err != nil {
		t.Fatalf("re-seed with a same-kind row: %v", err)
	}
	kept, err := ds.loadRowDB(ctx, eref{Kind: typeProvider, ID: providerSeedID})
	if err != nil || kept == nil {
		t.Fatalf("load default: %v", err)
	}
	if kept.Props["baseURL"] != "https://gateway.example.com/v1" {
		t.Fatalf("re-seed clobbered the owner's re-pointing: %v", kept.Props["baseURL"])
	}

	// A deliberate tombstone is not resurrected either.
	if _, err := ds.db.ExecContext(ctx, `
		UPDATE records SET deleted_at = now() WHERE kind = $2 AND id = $1`,
		providerSeedID, typeProvider); err != nil {
		t.Fatalf("tombstone default: %v", err)
	}
	if err := ds.seedAgentDefaults(ctx); err != nil {
		t.Fatalf("re-seed with a tombstoned row: %v", err)
	}
	gone, err := ds.loadRowDB(ctx, eref{Kind: typeProvider, ID: providerSeedID})
	if err != nil || gone == nil {
		t.Fatalf("load tombstoned default: %v", err)
	}
	if gone.DeletedAt == nil {
		t.Fatal("re-seed resurrected a deliberately deleted provider row")
	}
}
