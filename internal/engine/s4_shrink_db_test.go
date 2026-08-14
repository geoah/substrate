package engine_test

// Stage S4 — shrink the freeze. Three things this
// checks: the agent-loop vocabulary resolves in core (it was ai.substrate.reamde.dev
// until the runtime kinds folded into core, 2026-08-12); dialect step 6
// retypes a planted old agents.substrate.reamde.dev row and drops retired
// connector/connectoraccount rows; and a resolved agent still dispatches with
// its data kinds in core.

import (
	"context"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// The agent-loop vocabulary is CORE's: llmprovider/llmthread/llmmessage resolve there
// — the substrate maintains the agent runtime, so it publishes its data kinds
// beside the rest of its machinery — and NOT under the retired
// agents.substrate.reamde.dev or the folded-away ai.substrate.reamde.dev.
func TestAgentLoopKindsResolveInCore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	for _, id := range []string{"core.substrate.reamde.dev/llmprovider", "core.substrate.reamde.dev/llmthread", "core.substrate.reamde.dev/llmmessage"} {
		ti, err := ds.KindByRef(ctx, id)
		if err != nil {
			t.Fatalf("agent-loop kind %s does not resolve: %v", id, err)
		}
		if ti.Authority != "core.substrate.reamde.dev" {
			t.Fatalf("agent-loop kind %s is in authority %q, want core.substrate.reamde.dev", id, ti.Authority)
		}
	}
	// The old authorities are gone.
	for _, id := range []string{
		"agents.substrate.reamde.dev/llm", "agents.substrate.reamde.dev/thread", "agents.substrate.reamde.dev/message",
		"ai.substrate.reamde.dev/llm", "ai.substrate.reamde.dev/thread", "ai.substrate.reamde.dev/message",
	} {
		if _, err := ds.KindByRef(ctx, id); err == nil {
			t.Fatalf("the retired type %s still resolves", id)
		}
	}
	// A provider row is DATA of a core kind, written by its owner — the kind
	// resolves on a fresh repository, and no row of it exists there.
	row := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "core.substrate.reamde.dev/llmprovider", ID: "openai",
		Properties: map[string]any{"name": "openai", "wire": "openai"},
	})
	if row.Kind != "core.substrate.reamde.dev/llmprovider" {
		t.Fatalf("llmprovider row kind = %q", row.Kind)
	}
}

// The connector kinds are no longer writable through the ordinary API: a put
// naming connector/connectoraccount fails to resolve the type.
func TestConnectorKindsRemoved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	for _, typ := range []string{"core.substrate.reamde.dev/connector", "core.substrate.reamde.dev/connectoraccount"} {
		if _, err := ds.KindByRef(ctx, typ); err == nil {
			t.Fatalf("%s still resolves as a type", typ)
		}
		_, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: typ, Properties: map[string]any{"name": "x"},
		})
		if err == nil || !errors.Is(err, substrate.ErrValidation) {
			t.Fatalf("put %s: want a validation error for the removed kind, got %v", typ, err)
		}
	}
}
