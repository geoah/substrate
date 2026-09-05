package gql

import (
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// The Reference scalar reads the `ref` FIELD of a reference object, which is
// what every stored reference now is (decision 0044). It also reads the bare
// path a pre-0044 row still holds: a reader never picks its parse from the
// declaration, because the declaration in force says nothing about what an
// unrewritten row carries, and consulting it would answer null for a pointer
// that is plainly there.
func TestReferenceScalarReadsBothStoredShapes(t *testing.T) {
	const path = "samples.substrate.reamde.dev/people/person/p1"
	for name, tc := range map[string]struct {
		value any
		want  any
	}{
		// The pre-0044 spelling, and what an unnormalized caller may hand over.
		"a bare path": {value: path, want: path},
		"an object carrying link data": {
			value: map[string]any{vocabulary.ReferenceValueKey: path, "role": "author"},
			want:  path,
		},
		// The shape every write stores today, link data or not.
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
