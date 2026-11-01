package vocabulary_test

// Background calls (decision record 0200): a function declares
// `execution: background`, a tool entry or a sub-agent entry sets
// `background: true`, and a host function is refused both.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/vocabulary"
)

func TestFunctionExecution(t *testing.T) {
	for _, tc := range []struct {
		execution, refusal string
		background         bool
	}{
		{execution: ""},
		{execution: "foreground"},
		{execution: "background", background: true},
		{execution: "later", refusal: "data.execution"},
	} {
		line := ""
		if tc.execution != "" {
			line = "  execution: " + tc.execution + "\n"
		}
		r, err := loadFnAuthority(t, `  description: imports a mailbox
  runtime: python
  source: "def main(input, host): return {}"
`+line)
		if tc.refusal != "" {
			if err == nil || !strings.Contains(err.Error(), tc.refusal) {
				t.Fatalf("execution %q: err %v, want one naming %s", tc.execution, err, tc.refusal)
			}
			continue
		}
		if err != nil {
			t.Fatalf("execution %q: %v", tc.execution, err)
		}
		fn, err := r.ResolveFunction("fn.example.com/fn/mirror")
		if err != nil {
			t.Fatal(err)
		}
		if fn.Background() != tc.background {
			t.Fatalf("execution %q: Background() = %v", tc.execution, fn.Background())
		}
	}
}

func TestHostFunctionIsNeverBackground(t *testing.T) {
	fsys := fstest.MapFS{"hf.example.com/hf/all.yaml": &fstest.MapFile{Data: []byte(hostFn(`  description: asks the user
  runtime: host
  execution: background
`))}}
	_, err := vocabulary.LoadFS(fsys)
	if err == nil || !strings.Contains(err.Error(), "cannot run as a background job") {
		t.Fatalf("err %v, want the host refusal", err)
	}
}

func TestAgentBackgroundEntries(t *testing.T) {
	r, err := loadAgAuthorityWithCore(t, agAuthority(`  description: files widgets
  prompt: You file widgets.
  provider: default
  model: claude-opus-5
  tools:
    - {function: ag.example.com/ag/annotate, background: true}
    - {function: substrate.reamde.dev/core/query}
  subagents:
    - {ref: ag.example.com/ag/sorter, background: true}
  permissions:
    writes: [ag.example.com/ag/widget]
    reads:
      kinds: [ag.example.com/ag/widget]
`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	ag, err := r.ResolveAgent("ag.example.com/ag/classifier")
	if err != nil {
		t.Fatal(err)
	}
	if !ag.Tools[0].Background || ag.Tools[1].Background {
		t.Fatalf("tools %+v", ag.Tools)
	}
	if len(ag.Subagents) != 1 || !ag.BackgroundSubagents["ag.example.com/ag/sorter"] {
		t.Fatalf("subagents %v, background %v", ag.Subagents, ag.BackgroundSubagents)
	}

	_, err = loadAgAuthorityWithCore(t, agAuthority(`  description: files widgets
  prompt: You file widgets.
  provider: default
  model: claude-opus-5
  tools:
    - {function: substrate.reamde.dev/core/query, background: true}
  permissions:
    reads:
      kinds: [ag.example.com/ag/widget]
`))
	if err == nil || !strings.Contains(err.Error(), "is a host function") {
		t.Fatalf("a background host tool: err %v", err)
	}
}
