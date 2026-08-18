package engine

import (
	"strings"
	"testing"
)

// A put effect's edge target carries the edge row's own properties, and every
// other key is refused. Before this the decoder read `kind` and `id` and
// dropped the rest in silence, so a function that asked for edge properties got
// a link without them while every other write surface answers 422 for a
// property it cannot honor.
func TestDecodeEffectEdgesReadsTargetProperties(t *testing.T) {
	edges, err := decodeEffectEdges(map[string]any{
		"peer": map[string]any{
			"kind": "lab/node", "id": "n2",
			"properties": map[string]any{"role": "lead"},
		},
		"plain": "n3",
	})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(edges) != 2 {
		t.Fatalf("decoded %d edges, want 2", len(edges))
	}
	// sortedKeys orders the rels, so `peer` comes before `plain`.
	if got := edges[0]; got.Rel != "peer" || got.To.ID != "n2" || got.Properties["role"] != "lead" {
		t.Fatalf("peer decoded as %+v", got)
	}
	if got := edges[1]; got.Rel != "plain" || got.To.ID != "n3" || got.Properties != nil {
		t.Fatalf("plain decoded as %+v", got)
	}
}

func TestDecodeEffectEdgesRefusesUnknownTargetKeys(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target any
		want   string
	}{
		{"unknown key", map[string]any{"id": "n2", "props": map[string]any{}}, `unknown key "props"`},
		{"properties is a map", map[string]any{"id": "n2", "properties": "role=lead"}, "properties is a map"},
		{"still needs an id", map[string]any{"kind": "lab/node"}, "needs an id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeEffectEdges(map[string]any{"peer": tc.target})
			if err == nil {
				t.Fatal("the target must be refused, not silently accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error must name %q, got: %v", tc.want, err)
			}
		})
	}
}
