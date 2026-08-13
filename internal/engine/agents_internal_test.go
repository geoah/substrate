package engine

// The agent plumbing's pure halves: what a call input renders as, and the
// seeded provider row's pricing table.

import (
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// An EMPTY input is refused exactly like a nil one: some wires reject an empty
// user text block outright (anthropic 400s), so letting one through would
// settle a thread on an error after its rows had already landed.
func TestAgentUserContentRefusesAnEmptyInput(t *testing.T) {
	for _, in := range []any{nil, ""} {
		if _, err := agentUserContent(in); !errors.Is(err, substrate.ErrValidation) {
			t.Fatalf("agentUserContent(%#v) = %v, want a validation refusal", in, err)
		}
	}
	got, err := agentUserContent("do the thing")
	if err != nil || got != "do the thing" {
		t.Fatalf("agentUserContent(string) = %q, %v", got, err)
	}
	got, err = agentUserContent(map[string]any{"a": 1})
	if err != nil || got != `{"a":1}` {
		t.Fatalf("agentUserContent(map) = %q, %v", got, err)
	}
}
