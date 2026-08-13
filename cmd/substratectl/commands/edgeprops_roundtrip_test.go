package commands

import (
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// An edge's own properties must survive render → parse: dropping them makes a
// get|apply round trip wipe them server-side, since put overwrites the edge
// row's properties with what the input carries.
func TestEdgePropertiesSurviveTheRoundTrip(t *testing.T) {
	e := &substrate.Record{
		ID:        "c1",
		Kind:      "people.substrate.geoah.me/person",
		Version:   1,
		CreatedAt: time.Unix(0, 0).UTC(),
		UpdatedAt: time.Unix(0, 0).UTC(),
		Edges: map[string][]substrate.EdgeTarget{
			"memberOf": {{
				ID:         "org1",
				Kind:       "people.substrate.geoah.me/organization",
				Properties: map[string]any{"role": "admin", "since": "2019"},
			}},
		},
	}
	d := recordDocument(e, nil)
	if len(d.Data.Edges) != 1 || d.Data.Edges[0].Properties["role"] != "admin" {
		t.Fatalf("rendered edges = %+v, want edge-level properties", d.Data.Edges)
	}
	// The reference is rendered whole: the target's identity taken apart.
	if to := d.Data.Edges[0].To; to.Kind != "people.substrate.geoah.me/organization" || to.ID != "org1" {
		t.Fatalf("rendered reference = %+v, want {kind, id}", to)
	}
	in, err := d.putInput()
	if err != nil {
		t.Fatalf("putInput: %v", err)
	}
	if len(in.Edges) != 1 || in.Edges[0].Properties["role"] != "admin" || in.Edges[0].Properties["since"] != "2019" {
		t.Fatalf("parsed edges = %+v, want properties preserved", in.Edges)
	}
	if to := in.Edges[0].To; to.Kind != "people.substrate.geoah.me/organization" || to.ID != "org1" {
		t.Fatalf("parsed reference = %+v, want {kind, id}", to)
	}
}
