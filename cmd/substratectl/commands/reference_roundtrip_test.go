package commands

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// A reference carrying link properties travels as an object (`ref` beside the
// declared properties), and the printer must hand it back whole: reshaping it
// into a bare path would make a get|apply round trip wipe the link properties
// server-side, since put writes what the input carries.
func TestReferenceLinkPropertiesSurviveTheRoundTrip(t *testing.T) {
	e := &substrate.Record{
		ID:        "c1",
		Kind:      "samples.substrate.reamde.dev/people/person",
		Version:   1,
		CreatedAt: time.Unix(0, 0).UTC(),
		UpdatedAt: time.Unix(0, 0).UTC(),
		Properties: map[string]any{
			"memberOf": map[string]any{
				"ref":  "samples.substrate.reamde.dev/people/organization/org1",
				"role": "admin",
				// The wire decoder is UseNumber, so every number arrives as
				// json.Number, inside a reference object like anywhere else.
				"since": json.Number("2019"),
			},
		},
	}
	y, err := marshalDocument(recordDocument(e, nil))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// The value is one property, not a block of its own.
	if strings.Contains(string(y), "edges:") {
		t.Fatalf("rendered document carries an edges block:\n%s", y)
	}
	in, err := parseOneDocument(t, y).putInput()
	if err != nil {
		t.Fatalf("putInput: %v", err)
	}
	ref, ok := in.Properties["memberOf"].(map[string]any)
	if !ok {
		t.Fatalf("parsed memberOf = %#v, want the object shape", in.Properties["memberOf"])
	}
	if ref["ref"] != "samples.substrate.reamde.dev/people/organization/org1" || ref["role"] != "admin" {
		t.Fatalf("parsed reference = %#v, want ref and role preserved", ref)
	}
	// A number inside a reference object is typed like every other property
	// value: untyped it renders as `since: "2019"` and applies back as a
	// string, which is not the value the substrate holds.
	if !strings.Contains(string(y), "since: 2019\n") {
		t.Fatalf("rendered document does not carry the integer 2019:\n%s", y)
	}
	if ref["since"] != 2019 {
		t.Fatalf("parsed since = %#v (%T), want the integer 2019", ref["since"], ref["since"])
	}
}
