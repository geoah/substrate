package vocabulary_test

// The `notifies:` transition marker's admission rules
// (docs/plans/thread-interactions.md): only the SEEDED kinds may carry it in
// this build, and the marker must name a reference property pinned to the llm
// package's thread. The POSITIVE case is the shipped tree itself —
// recordpatchrequest's decision transitions carry the marker, and the
// shipped-vocabulary load test refuses a tree that does not admit.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/vocabulary"
)

func loadProblems(t *testing.T, files map[string]string) string {
	t.Helper()
	fsys := fstest.MapFS{}
	for name, body := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(body)}
	}
	_, err := vocabulary.LoadFS(fsys)
	if err == nil {
		t.Fatal("the fixture loaded; a refusal was expected")
	}
	return err.Error()
}

func TestNotifiesRefusedOutsideTheSeededPackages(t *testing.T) {
	problems := loadProblems(t, map[string]string{
		"ops.example.com/ops/authority.yaml": `kind: substrate.reamde.dev/core/package
metadata:
  id: ops.example.com/ops
data:
  authority: ops.example.com
  package: ops
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: ops.example.com/ops/deployrequest
data:
  authority: ops.example.com
  package: ops
  names:
    singular: deployrequest
  properties:
    state:
      type: state
      states: [pending, done]
      transitions:
        - {from: pending, to: done, notifies: thread}
`,
	})
	if !strings.Contains(problems, "only the substrate's own seeded kinds may notify a thread") {
		t.Fatalf("the refusal does not name the seeded restriction: %s", problems)
	}
}

func TestNotifiesDemandsAThreadReference(t *testing.T) {
	problems := loadProblems(t, map[string]string{
		"substrate.reamde.dev/llm/llm.yaml": `kind: substrate.reamde.dev/core/package
metadata:
  id: substrate.reamde.dev/llm
data:
  authority: substrate.reamde.dev
  package: llm
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: substrate.reamde.dev/llm/thread
data:
  authority: substrate.reamde.dev
  package: llm
  names:
    singular: thread
  properties:
    status:
      type: string
`,
		"substrate.reamde.dev/core/authority.yaml": `kind: substrate.reamde.dev/core/package
metadata:
  id: substrate.reamde.dev/core
data:
  authority: substrate.reamde.dev
  package: core
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: substrate.reamde.dev/core/gadgetrequest
data:
  authority: substrate.reamde.dev
  package: core
  names:
    singular: gadgetrequest
  properties:
    # thread is a plain string here, not a reference: the marker must refuse.
    thread:
      type: string
    state:
      type: state
      states: [pending, done]
      transitions:
        - {from: pending, to: done, notifies: thread}
`,
	})
	if !strings.Contains(problems, "must be a reference property pinned to substrate.reamde.dev/llm/thread") {
		t.Fatalf("the refusal does not name the reference contract: %s", problems)
	}
}
