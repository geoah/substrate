package api

import (
	"net/http"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// Both read surfaces carry the canonical-id contract (MODEL §4.1): a read
// addressed at a former id answers with the canonical record and names it.
func TestCanonicalIDOnBothReadSurfaces(t *testing.T) {
	env := newTestEnv(t)
	tok := env.svc.token("geoah")
	ds := env.svc.datasets["geoah"]
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

	res := env.gql(t, tok, `query { record(kind: "samples.substrate.reamde.dev/people/person", id: "loser") { id canonicalId title } }`, nil)
	ent, _ := res.Data["record"].(map[string]any)
	if ent["id"] != "winner" || ent["canonicalId"] != "winner" {
		t.Fatalf("GraphQL read of a former id = %+v", ent)
	}
	res = env.gql(t, tok, `query { record(kind: "samples.substrate.reamde.dev/people/person", id: "winner") { id canonicalId } }`, nil)
	ent, _ = res.Data["record"].(map[string]any)
	if v, present := ent["canonicalId"]; !present || v != nil {
		t.Fatalf("canonicalId on a canonical read must be null, got %v", v)
	}
}
