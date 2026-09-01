package gql

import (
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// The Reference scalar reads a STORED value, and a stored reference comes in
// either shape: the flat path a declaration without link data writes, or the
// `{ref, <props>}` object one with `properties:` writes. Which shape a row holds
// was decided when it was written, so dropping `properties:` from a live
// declaration leaves objects behind that the scalar must still read — a reader
// that consulted the declaration would answer null for a pointer that is there.
func TestReferenceScalarReadsBothStoredShapes(t *testing.T) {
	const path = "people.substrate.reamde.dev/person/p1"
	for name, tc := range map[string]struct {
		value any
		want  any
	}{
		"a flat path": {value: path, want: path},
		"an object carrying link data": {
			value: map[string]any{vocabulary.ReferenceValueKey: path, "role": "author"},
			want:  path,
		},
		"an object with only the pointer": {
			value: map[string]any{vocabulary.ReferenceValueKey: path},
			want:  path,
		},
		"an object without a pointer": {value: map[string]any{"role": "author"}, want: nil},
		"a number":                    {value: 42, want: nil},
	} {
		t.Run(name, func(t *testing.T) {
			if got := coerceReferencePath(tc.value); got != tc.want {
				t.Fatalf("coerceReferencePath(%v) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}
