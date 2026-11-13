package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// The record path carries the canonical-id contract (MODEL §4.1): a read
// addressed at a former id answers with the canonical record and names it,
// and a read at the canonical id names nothing.
func TestCanonicalIDOnAFormerIDRead(t *testing.T) {
	t.Parallel()
	env := newDBEnv(t, withSamples("people"))
	env.put(t, personKind, "winner", map[string]any{"name": "Ada"})
	env.put(t, personKind, "loser", map[string]any{"name": "A. Lovelace"})
	if _, err := env.ds.Merge(context.Background(), substrate.ActorSystem, substrate.MergeInput{
		Kind: personKind, Winner: "winner", Loser: "loser",
	}); err != nil {
		t.Fatalf("merge: %v", err)
	}

	rec := env.do(t, http.MethodGet, peoplePath+"/loser", env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	got := decodeJSON[substrate.Record](t, rec)
	if got.ID != "winner" || got.CanonicalID != "winner" {
		t.Fatalf("REST read of a former id = %+v", got)
	}
	rec = env.do(t, http.MethodGet, peoplePath+"/winner", env.token, nil)
	wantStatus(t, rec, http.StatusOK)
	if direct := decodeJSON[substrate.Record](t, rec); direct.CanonicalID != "" {
		t.Fatalf("canonicalId on a canonical read: %q", direct.CanonicalID)
	}
}
