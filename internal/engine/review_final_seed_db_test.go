package engine

// Final review #8, re-keyed by ticket 001: record identity is the (type, id)
// pair, so cheap/mid/strong are ordinary ids OF THE LLM TYPE — no reservation
// spans types anymore. A row of a DIFFERENT type at a tier id is no collision:
// seeding proceeds beside it and both rows stay readable by their own
// (type, id). What still holds is create-only on the llm type itself: a
// same-type row at a seed id — an owner's live re-tier or a deliberate
// tombstone — is left exactly as the owner had it.

import (
	"context"
	"testing"
)

// A row of ANOTHER type at a tier id is no collision: seeding mints the llm
// row beside it, and each row answers to its own (type, id).
func TestFinalSeedCoexistsWithOtherTypeAtTierID(t *testing.T) {
	ctx := context.Background()
	ds := openInternalDataset(t)
	// Simulate the pre-re-key repository the old reservation guarded against: the
	// id `cheap` is held by a NON-llm record, and no llm row exists there.
	if _, err := ds.db.ExecContext(ctx, `
		UPDATE records SET kind = $2, title = 'kept', props = '{"title":"kept"}'::jsonb
		WHERE kind = $3 AND id = $1`, "cheap", "tasks.substrate.reamde.dev/task", typeLLM); err != nil {
		t.Fatalf("fabricate other-type occupant: %v", err)
	}
	// Re-seeding proceeds: a different-type occupant is not a conflict.
	if err := ds.seedAgentDefaults(ctx); err != nil {
		t.Fatalf("seeding beside a different-type row failed: %v", err)
	}
	// The llm tier row exists again, seeded fresh…
	llm, err := ds.Get(ctx, typeLLM, "cheap")
	if err != nil {
		t.Fatalf("the seeded llm row: %v", err)
	}
	if llm.Properties["provider"] != "litellm" || llm.Properties["model"] == nil {
		t.Fatalf("seeded llm row wrong: %+v", llm.Properties)
	}
	// …and the other-type occupant coexists untouched at the same id.
	task, err := ds.Get(ctx, "tasks.substrate.reamde.dev/task", "cheap")
	if err != nil {
		t.Fatalf("the coexisting non-llm row: %v", err)
	}
	if task.Properties["title"] != "kept" {
		t.Fatalf("seeding disturbed the other-type row: %+v", task.Properties)
	}
}

// A same-type row is preserved: an owner's re-tier stands and a deliberate
// tombstone is not resurrected — re-seeding is a no-op on both.
func TestFinalSeedSameTypePreserved(t *testing.T) {
	ctx := context.Background()
	ds := openInternalDataset(t)
	// Owner re-tiers `cheap` to a custom model.
	if _, err := ds.db.ExecContext(ctx, `
		UPDATE records SET props = jsonb_set(props, '{model}', '"custom/model-x"')
		WHERE kind = $2 AND id = $1`, "cheap", typeLLM); err != nil {
		t.Fatalf("re-tier cheap: %v", err)
	}
	// Owner deletes `strong`.
	if _, err := ds.db.ExecContext(ctx, `
		UPDATE records SET deleted_at = now() WHERE kind = $2 AND id = $1`,
		"strong", typeLLM); err != nil {
		t.Fatalf("tombstone strong: %v", err)
	}
	if err := ds.seedAgentDefaults(ctx); err != nil {
		t.Fatalf("re-seed with same-type rows: %v", err)
	}
	cheap, err := ds.loadRowDB(ctx, eref{Kind: typeLLM, ID: "cheap"})
	if err != nil || cheap == nil {
		t.Fatalf("load cheap: %v", err)
	}
	if cheap.Props["model"] != "custom/model-x" {
		t.Fatalf("re-seed clobbered the owner's re-tier: %v", cheap.Props["model"])
	}
	strong, err := ds.loadRowDB(ctx, eref{Kind: typeLLM, ID: "strong"})
	if err != nil || strong == nil {
		t.Fatalf("load strong: %v", err)
	}
	if strong.DeletedAt == nil {
		t.Fatal("re-seed resurrected a deliberately deleted tier")
	}
}
