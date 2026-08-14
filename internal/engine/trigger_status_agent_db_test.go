package engine

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// `trigger status` reports the dispatcher's own verdict, and the dispatcher
// skips on runnable(): a trigger resolves into EITHER a function or an agent.
// Testing `Callable == nil` instead marked every agent-backed trigger
// "callable … does not resolve" while it was dispatching perfectly — on a clean
// install that is the shipped conformance bundle looking broken out of the box.
func TestTriggerStatusResolvesAgentCallables(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _ := openAgentDataset(t)

	agentTrigger, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source":   map[string]any{"record": map[string]any{"kinds": []any{crewAuthority + "/widget"}, "ops": []any{"create"}}},
			"callable": map[string]any{"kind": "core.substrate.reamde.dev/agent", "id": crewAuthority + "/classifier"},
		},
	})
	if err != nil {
		t.Fatalf("put agent trigger: %v", err)
	}
	functionTrigger, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: typeTrigger,
		Properties: map[string]any{
			"source":   map[string]any{"record": map[string]any{"kinds": []any{crewAuthority + "/widget"}, "ops": []any{"create"}}},
			"callable": map[string]any{"kind": "core.substrate.reamde.dev/function", "id": crewAuthority + "/annotate"},
		},
	})
	if err != nil {
		t.Fatalf("put function trigger: %v", err)
	}

	statuses, err := ds.TriggerStatuses(ctx)
	if err != nil {
		t.Fatalf("statuses: %v", err)
	}
	seen := map[string]substrate.TriggerStatus{}
	for _, st := range statuses {
		seen[st.ID] = st
	}
	for _, id := range []string{agentTrigger.ID, functionTrigger.ID} {
		st, ok := seen[id]
		if !ok {
			t.Fatalf("no status for trigger %s", id)
		}
		if st.Error != "" {
			t.Errorf("trigger %s (callable %s) reports %q; it resolves and dispatches", id, st.Callable, st.Error)
		}
	}

	// A callable that genuinely does not resolve still says so — the fix
	// widens the test, it does not silence it. The row is written past
	// admission (which refuses an unknown callable) the way an uninstall
	// leaves one behind.
	if _, err := ds.db.ExecContext(ctx,
		`UPDATE records SET props = jsonb_set(props, '{callable}', to_jsonb($3::text))
		 WHERE kind = $1 AND id = $2`, typeTrigger, agentTrigger.ID,
		vocabulary.RecordPath(kindAgent, "crew.test.dev/ghost")); err != nil {
		t.Fatalf("strand the callable: %v", err)
	}
	statuses, err = ds.TriggerStatuses(ctx)
	if err != nil {
		t.Fatalf("statuses: %v", err)
	}
	for _, st := range statuses {
		if st.ID == agentTrigger.ID && st.Error == "" {
			t.Fatal("a stranded callable reports no error; the status is not honest")
		}
	}
}
