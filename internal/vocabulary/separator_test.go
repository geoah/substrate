package vocabulary_test

import (
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// A `---` separator carrying content is legal YAML the verbatim slicer cannot
// represent; it must be refused, never silently dropped (a dropped manifest is
// a type or actor missing from the registry with a green build).
func TestSeparatorWithContentIsRefused(t *testing.T) {
	in := "--- {kind: core.substrate.reamde.dev/actor, metadata: {id: api}, data: {authority: core.substrate.reamde.dev}}\n"
	if _, err := vocabulary.ParseStream([]byte(in)); err == nil || !strings.Contains(err.Error(), "stand alone") {
		t.Fatalf("want a stand-alone-separator error, got %v", err)
	}

	// The bare separator stays a separator.
	ok := "---\nkind: core.substrate.reamde.dev/actor\nmetadata: {id: api}\ndata: {authority: core.substrate.reamde.dev}\n"
	docs, err := vocabulary.ParseStream([]byte(ok))
	if err != nil || len(docs) != 1 {
		t.Fatalf("bare separator: docs=%d err=%v", len(docs), err)
	}
}
