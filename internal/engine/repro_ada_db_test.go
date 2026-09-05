package engine_test

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// Repro: prod "unexpected EOF" on a contact CREATE carrying the full
// Ada payload (object name + two emails + phones + organizations + etag)
// while match probes run.
func TestReproAdaCreate(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)
	_, err := ds.Put(context.Background(), "google.connectors.substrate.reamde.dev/google/people", substrate.PutInput{
		Kind: "google.connectors.substrate.reamde.dev/google/contact",
		ID:   "people-c1001",
		Properties: map[string]any{
			"name": map[string]any{"displayName": "Ada Lovelace", "firstName": "Ada", "lastName": "Lovelace"},
			"emails": []any{
				map[string]any{"value": "ada@example.org", "type": "home", "primary": true, "verified": true},
				map[string]any{"value": "ada@analytical.engines", "type": "work"},
			},
			"phones": []any{
				map[string]any{"value": "+44 20 7946 0111", "canonical": "+442079460111", "type": "mobile"},
			},
			"etag": "a1",
		},
	})
	if err != nil {
		t.Fatalf("ada create: %v", err)
	}
}
