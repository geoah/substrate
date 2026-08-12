package vocabulary_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/vocabulary"
)

// agAuthority renders a minimal authority carrying one function tool and one agent
// whose data is the given YAML block (indented under data:).
func agAuthority(agData string) string {
	return `kind: core.substrate.reamde.dev/authority
metadata:
  id: ag.example.com
data:
  version: v1alpha1
---
kind: core.substrate.reamde.dev/kind
metadata:
  id: ag.example.com/widget
data:
  authority: ag.example.com
  names: {singular: widget, plural: widgets}
  properties:
    name: {type: string}
---
kind: core.substrate.reamde.dev/function
metadata:
  id: ag.example.com/annotate
data:
  authority: ag.example.com
  description: annotates a widget
  runtime: python
  source: "def main(input, host): return {}"
  capabilities:
    emit: [ag.example.com/widget]
---
kind: core.substrate.reamde.dev/agent
metadata:
  id: ag.example.com/sorter
data:
  authority: ag.example.com
  description: files widgets where they belong
  prompt: You sort widgets.
  llm: cheap
---
kind: core.substrate.reamde.dev/agent
metadata:
  id: ag.example.com/classifier
data:
  authority: ag.example.com
` + agData
}

func loadAgAuthority(t *testing.T, agData string) (*vocabulary.Registry, error) {
	t.Helper()
	fsys := fstest.MapFS{"ag.example.com/all.yaml": &fstest.MapFile{Data: []byte(agData)}}
	return vocabulary.LoadFS(fsys)
}

func TestAgentLoads(t *testing.T) {
	r, err := loadAgAuthority(t, agAuthority(`  description: classifies widgets
  prompt: You classify widgets.
  llm: cheap
  tools:
    - propose
    - {callable: ag.example.com/annotate, name: markWidget, description: marks one widget}
  agents: [ag.example.com/sorter]
  budgets: {maxTurns: 4, depth: 2}
  emit:
    - ag.example.com/widget
    - core.substrate.reamde.dev/recordpatchrequest
  reads:
    kinds: [ag.example.com/widget]
`))
	// recordpatchrequest lives in core, which this fixture does not declare
	// — resolution must fail on exactly that, proving emit resolves.
	if err == nil || !strings.Contains(err.Error(), "core.substrate.reamde.dev/recordpatchrequest") {
		t.Fatalf("emit did not resolve against the registry: %v", err)
	}
	_ = r
}

func TestAgentLoadsWithoutCoreEmit(t *testing.T) {
	r, err := loadAgAuthority(t, agAuthority(`  description: classifies widgets
  prompt: You classify widgets.
  llm: cheap
  tools:
    - {callable: ag.example.com/annotate, name: markWidget}
  agents: [ag.example.com/sorter]
  budgets: {maxTurns: 4, depth: 2}
  emit: [ag.example.com/widget]
  reads:
    kinds: [ag.example.com/widget]
`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	ag, err := r.ResolveAgent("ag.example.com/classifier")
	if err != nil {
		t.Fatal(err)
	}
	if ag.Actor() != "function:classifier" {
		t.Fatalf("actor %q", ag.Actor())
	}
	if ag.Budgets.MaxTurns != 4 || ag.Budgets.Depth != 2 || ag.Budgets.MaxToolCalls != vocabulary.DefaultAgentToolCalls {
		t.Fatalf("budgets %+v", ag.Budgets)
	}
	if len(ag.Tools) != 1 || ag.Tools[0].Name != "markWidget" || ag.Tools[0].Callable != "ag.example.com/annotate" {
		t.Fatalf("tools %+v", ag.Tools)
	}
	if ag.Reads == nil || ag.Reads.Calls != vocabulary.DefaultReadCalls {
		t.Fatalf("reads %+v", ag.Reads)
	}
}

func TestAgentRefusals(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{"no prompt", `  description: d
  llm: cheap
`, "data.prompt is required"},
		{"no llm", `  description: d
  prompt: p
`, "data.llm is required"},
		{"query without reads", `  description: d
  prompt: p
  llm: cheap
  tools: [query]
`, "query needs data.reads"},
		{"propose without emit", `  description: d
  prompt: p
  llm: cheap
  tools: [propose]
`, "propose needs core.substrate.reamde.dev/recordpatchrequest in data.emit"},
		{"self sub-agent", `  description: d
  prompt: p
  llm: cheap
  agents: [ag.example.com/classifier]
`, "may not name itself"},
		{"depth above cap", `  description: d
  prompt: p
  llm: cheap
  budgets: {depth: 4}
`, "between 1 and 3"},
		{"unknown sub-agent", `  description: d
  prompt: p
  llm: cheap
  agents: [ag.example.com/ghost]
`, "unknown agent"},
		{"tool collides with sub-agent", `  description: d
  prompt: p
  llm: cheap
  tools:
    - {callable: ag.example.com/annotate, name: sorter}
  agents: [ag.example.com/sorter]
`, "collides with tool name"},
		{"unknown tool string", `  description: d
  prompt: p
  llm: cheap
  tools: [frobnicate]
`, "query, propose, or a full function identity"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadAgAuthority(t, agAuthority(tc.data))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got: %v", tc.want, err)
			}
		})
	}
}
