package commands

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/authority"
)

// The publisher's own door: `publish --check` refuses a release that renames
// a kind with `movedFrom`, with or without --allow-narrowing, before it reads
// the published document (decision 0269).
func TestPublishCheckRefusesARename(t *testing.T) {
	const closure = `kind: substrate.reamde.dev/core/package
metadata:
  id: publisher.example/notes
data:
  authority: publisher.example
  package: notes
  version: 2
---
kind: substrate.reamde.dev/core/bundle
metadata:
  id: publisher.example/notes
data:
  authority: publisher.example
  package: notes
  description: Notes.
  installs: [publisher.example/notes/memo]
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: publisher.example/notes/memo
data:
  authority: publisher.example
  package: notes
  movedFrom: publisher.example/notes/note
  names:
    singular: memo
  properties:
    name:
      type: string
`
	for _, allow := range []bool{false, true} {
		_, err := checkPublication(context.Background(), authority.NewReader(authority.Options{}),
			"publisher.example/notes", 2, "https://publisher.example/notes-2.yaml", []byte(closure), allow)
		if err == nil || !strings.Contains(err.Error(), "renames a kind") ||
			!strings.Contains(err.Error(), "kind publisher.example/notes/memo: movedFrom publisher.example/notes/note") ||
			!strings.Contains(err.Error(), "deprecated: true") {
			t.Fatalf("allow-narrowing=%v: err = %v, want the rename refused saying what to do instead", allow, err)
		}
	}
}
