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
// `apply`, and rendered again is BYTE-stable for an unchanged record. A generic
// client reads, modifies, and writes the same object, so the render is
// deterministic (yaml sorts nothing for us; the property map is written in key
// order) and the loop is a fixed point.
func TestCanonicalEnvelopeRoundTripIsByteStable(t *testing.T) {
	at := time.Unix(1_700_000_000, 0).UTC()
	e := &substrate.Record{
		ID:        "p1",
		Kind:      "people.substrate.reamde.dev/person",
		Version:   7,
		CreatedAt: at,
		UpdatedAt: at,
		// Deliberately unsorted, and every reference SITE at once: a single one,
		// a repeated one, and one carrying link properties. Every value is the
		// object the server serves (decision 0044). The string shorthand is an
		// input spelling, so it never appears in a record read back.
		Properties: map[string]any{
			"name":  "Ada",
			"age":   36,
			"knows": map[string]any{"ref": "people.substrate.reamde.dev/person/p2"},
			"employs": []any{
				map[string]any{"ref": "people.substrate.reamde.dev/person/p3"},
				map[string]any{"ref": "people.substrate.reamde.dev/person/p2"},
			},
			"memberOf": []any{
				map[string]any{"ref": "people.substrate.reamde.dev/organization/org2", "role": "member"},
				map[string]any{"ref": "people.substrate.reamde.dev/organization/org1", "role": "admin"},
			},
		},
		Labels: map[string]any{"owner/pinned": true},
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

	// The write shape the two documents produce is identical, references and
	// all: a repeated reference keeps its authored order, and the object shape
	// keeps its `ref` beside its link properties.
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
	employs, ok := in1.Properties["employs"].([]any)
	if !ok || len(employs) != 2 {
		t.Fatalf("repeated reference = %#v, want two entries", in1.Properties["employs"])
	}
	first, _ := employs[0].(map[string]any)
	if first["ref"] != "people.substrate.reamde.dev/person/p3" {
		t.Fatalf("repeated reference = %#v, want the authored order", in1.Properties["employs"])
	}
}

// A document that writes the STRING SHORTHAND still applies, and the CLI hands
// it to the server unchanged: the shorthand is the server's to normalize, and a
// client that rewrote it would be a second coercion to keep in step with the
// first. What the round trip above pins is the other half: what comes BACK is
// the object.
func TestApplyCarriesTheReferenceShorthandThrough(t *testing.T) {
	raw := []byte(`kind: people.substrate.reamde.dev/person
metadata:
  id: p1
data:
  properties:
    name: Ada
    knows: people.substrate.reamde.dev/person/p2
    employs:
      - people.substrate.reamde.dev/person/p3
`)
	in, err := parseOneDocument(t, raw).putInput()
	if err != nil {
		t.Fatalf("putInput: %v", err)
	}
	if in.Properties["knows"] != "people.substrate.reamde.dev/person/p2" {
		t.Fatalf("knows = %#v, want the authored shorthand carried through", in.Properties["knows"])
	}
	employs, ok := in.Properties["employs"].([]any)
	if !ok || len(employs) != 1 || employs[0] != "people.substrate.reamde.dev/person/p3" {
		t.Fatalf("employs = %#v, want the authored shorthand carried through", in.Properties["employs"])
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
