package vocabulary_test

// The `notifies:` transition marker's admission rules
// (docs/plans/thread-interactions.md): only core kinds may carry it in this
// build, and the marker must name a reference property pinned to core's
// llmthread. The POSITIVE case is the shipped tree itself —
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

func TestNotifiesRefusedOutsideCore(t *testing.T) {
	problems := loadProblems(t, map[string]string{
		"ops.example.com/authority.yaml": `kind: core.substrate.reamde.dev/authority
metadata:
  id: ops.example.com
data:
  version: 1
---
kind: core.substrate.reamde.dev/kind
metadata:
  id: ops.example.com/deployrequest
data:
  authority: ops.example.com
  names:
    singular: deployrequest
    plural: deployrequests
  properties:
    state:
      type: state
      states: [pending, done]
      transitions:
        - {from: pending, to: done, notifies: thread}
`,
	})
	if !strings.Contains(problems, "only core kinds may notify a thread") {
		t.Fatalf("the refusal does not name the core restriction: %s", problems)
	}
}

func TestNotifiesDemandsAThreadReference(t *testing.T) {
	problems := loadProblems(t, map[string]string{
		"core.substrate.reamde.dev/authority.yaml": `kind: core.substrate.reamde.dev/authority
metadata:
  id: core.substrate.reamde.dev
data:
  version: 1
---
kind: core.substrate.reamde.dev/kind
metadata:
  id: core.substrate.reamde.dev/llmthread
data:
  authority: core.substrate.reamde.dev
  names:
    singular: llmthread
    plural: llmthreads
  properties:
    status:
      type: string
---
kind: core.substrate.reamde.dev/kind
metadata:
  id: core.substrate.reamde.dev/gadgetrequest
data:
  authority: core.substrate.reamde.dev
  names:
    singular: gadgetrequest
    plural: gadgetrequests
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
	if !strings.Contains(problems, "must be a reference property pinned to core.substrate.reamde.dev/llmthread") {
		t.Fatalf("the refusal does not name the reference contract: %s", problems)
	}
}
