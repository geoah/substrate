package commands

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/geoah/substrate/internal/substrate"
)

// The canonical envelope must round-trip: `get -o yaml` rendered, parsed by
// `apply`, and rendered again is BYTE-stable for an unchanged record, edges in
// one shape. A generic client reads, modifies, and writes the same
// object; the render is deterministic (sorted rels, sorted targets, sorted map
// keys) so the loop is a fixed point.
func TestCanonicalEnvelopeRoundTripIsByteStable(t *testing.T) {
	at := time.Unix(1_700_000_000, 0).UTC()
	e := &substrate.Record{
		ID:        "p1",
		Kind:      "people.substrate.geoah.me/person",
		Version:   7,
		CreatedAt: at,
		UpdatedAt: at,
		// Deliberately unsorted maps + several rels/targets: the render must
		// impose a stable order regardless of Go's map iteration.
		Properties: map[string]any{"name": "Ada", "company": "Analytical", "age": 36},
		Labels:     map[string]any{"owner/pinned": true},
		Edges: map[string][]substrate.EdgeTarget{
			"memberOf": {
				{ID: "org2", Kind: "people.substrate.geoah.me/organization", Properties: map[string]any{"role": "member"}},
				{ID: "org1", Kind: "people.substrate.geoah.me/organization", Properties: map[string]any{"role": "admin"}},
			},
			"knows": {{ID: "p2", Kind: "people.substrate.geoah.me/person"}},
		},
	}

	d1 := recordDocument(e, nil)
	y1, err := marshalDocument(d1)
	if err != nil {
		t.Fatalf("marshal 1: %v", err)
	}

	// Parse it back exactly as `apply` does, then re-render.
	d2 := parseOneDocument(t, y1)
	y2, err := marshalDocument(d2)
	if err != nil {
		t.Fatalf("marshal 2: %v", err)
	}
	if !bytes.Equal(y1, y2) {
		t.Fatalf("round-trip is not byte-stable:\n--- first ---\n%s\n--- second ---\n%s", y1, y2)
	}

	// Rendering the SAME record twice is identical too (deterministic order).
	y1b, _ := marshalDocument(recordDocument(e, nil))
	if !bytes.Equal(y1, y1b) {
		t.Fatalf("render is not deterministic:\n%s\n---\n%s", y1, y1b)
	}

	// The write shape the two documents produce is identical — edges as ONE
	// list, references whole ({authority, type, id}).
	in1, err := d1.putInput()
	if err != nil {
		t.Fatalf("putInput 1: %v", err)
	}
	in2, err := d2.putInput()
	if err != nil {
		t.Fatalf("putInput 2: %v", err)
	}
	if !reflect.DeepEqual(in1, in2) {
		t.Fatalf("write inputs differ:\n%+v\n%+v", in1, in2)
	}
	if len(in1.Edges) != 3 {
		t.Fatalf("edges collapsed to %d, want 3 (memberOf x2 + knows)", len(in1.Edges))
	}
}

func parseOneDocument(t *testing.T, raw []byte) *document {
	t.Helper()
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	var node yaml.Node
	if err := dec.Decode(&node); err != nil {
		t.Fatalf("decode yaml: %v", err)
	}
	d, err := nodeDocument(unwrapNode(&node), "roundtrip")
	if err != nil {
		t.Fatalf("nodeDocument: %v", err)
	}
	return d
}
