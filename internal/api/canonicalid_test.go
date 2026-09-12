package api

import (
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// The record path carries the canonical-id contract (MODEL §4.1): a read
// addressed at a former id answers with the canonical record and names it,
// and a read at the canonical id names nothing.
func TestCanonicalIDOnAFormerIDRead(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token(fakeRepository)
	ds := env.svc.datasets[fakeRepository]
	ds.records["winner"] = &substrate.Record{
		ID: "winner", Kind: "samples.substrate.reamde.dev/people/person", Title: "Ada",
		Properties: map[string]any{"name": "Ada"}, Labels: map[string]any{},
	}
	ds.records["loser"] = &substrate.Record{
		ID: "loser", Kind: "samples.substrate.reamde.dev/people/person", Title: "A. Lovelace",
		Properties: map[string]any{}, Labels: map[string]any{},
	}
	ds.formers["loser"] = "winner"

	rec := env.do(t, http.MethodGet, peoplePath+"/loser", tok, nil)
	wantStatus(t, rec, http.StatusOK)
	got := decodeJSON[substrate.Record](t, rec)
	if got.ID != "winner" || got.CanonicalID != "winner" {
		t.Fatalf("REST read of a former id = %+v", got)
	}
	rec = env.do(t, http.MethodGet, peoplePath+"/winner", tok, nil)
	if direct := decodeJSON[substrate.Record](t, rec); direct.CanonicalID != "" {
		t.Fatalf("canonicalId on a canonical read: %q", direct.CanonicalID)
	}
}
