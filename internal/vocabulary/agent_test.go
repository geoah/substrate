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
  emit: [ag.example.com/widget]
---
kind: core.substrate.reamde.dev/agent
metadata:
  id: ag.example.com/sorter
data:
  authority: ag.example.com
  description: files widgets where they belong
  prompt: You sort widgets.
  provider: default
  model: claude-opus-5
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

// coreHostStub is the minimum of core an agent fixture needs to NAME a built-in.
// The four built-ins are `runtime: host` function records, so
// `{callable: core.substrate.reamde.dev/graphql}` resolves against the registry
// like any other callable — a fixture declaring no core cannot name one, which is
// exactly the refusal a real repository gets for a function nobody installed.
// (`host` is admissible here because LoadFS builds `builtin`.)
const coreHostStub = `kind: core.substrate.reamde.dev/authority
metadata:
  id: core.substrate.reamde.dev
data:
  version: v1alpha1
---
kind: core.substrate.reamde.dev/kind
metadata:
  id: core.substrate.reamde.dev/recordpatchrequest
data:
  authority: core.substrate.reamde.dev
  names: {singular: recordpatchrequest, plural: recordpatchrequests}
  properties:
    op: {type: string}
---
kind: core.substrate.reamde.dev/function
metadata:
  id: core.substrate.reamde.dev/query
data:
  authority: core.substrate.reamde.dev
  description: reads records under the caller's allowlist
  runtime: host
---
kind: core.substrate.reamde.dev/function
metadata:
  id: core.substrate.reamde.dev/graphql
data:
  authority: core.substrate.reamde.dev
  description: reads the whole repository through GraphQL
  runtime: host
---
kind: core.substrate.reamde.dev/function
metadata:
  id: core.substrate.reamde.dev/mutate
data:
  authority: core.substrate.reamde.dev
  description: writes through GraphQL under the calling agent's emit
  runtime: host
---
kind: core.substrate.reamde.dev/function
metadata:
  id: core.substrate.reamde.dev/propose
data:
  authority: core.substrate.reamde.dev
  description: lands a reviewed change request
  runtime: host
  emit: [core.substrate.reamde.dev/recordpatchrequest]
`

// loadAgAuthorityWithCore is loadAgAuthority with the core stub beside it, for
// the fixtures that name a built-in.
func loadAgAuthorityWithCore(t *testing.T, agData string) (*vocabulary.Registry, error) {
	t.Helper()
	fsys := fstest.MapFS{
		"ag.example.com/all.yaml":             &fstest.MapFile{Data: []byte(agData)},
		"core.substrate.reamde.dev/core.yaml": &fstest.MapFile{Data: []byte(coreHostStub)},
	}
	return vocabulary.LoadFS(fsys)
}

func TestAgentLoads(t *testing.T) {
	r, err := loadAgAuthority(t, agAuthority(`  description: classifies widgets
  prompt: You classify widgets.
  provider: default
  model: claude-opus-5
  tools:
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
  provider: default
  model: claude-opus-5
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

// The graphql built-in needs no grant beyond its declaration (it is read-only
// and repository-wide by design), mutate rides the emit allowlist, and
// subagentOnly is an ordinary parsed flag: one manifest proves all three. The
// built-ins are named BY IDENTITY, and the derived Builtin word is what the
// grant checks and the loop's dispatch read off the resolved entry.
func TestAgentGraphQLBuiltinsAndSubagentOnly(t *testing.T) {
	r, err := loadAgAuthorityWithCore(t, agAuthority(`  description: reads and writes the graph
  prompt: You tend widgets.
  provider: default
  model: claude-opus-5
  subagentOnly: true
  tools:
    - {callable: core.substrate.reamde.dev/graphql}
    - {callable: core.substrate.reamde.dev/mutate}
  emit: [ag.example.com/widget]
`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	ag, err := r.ResolveAgent("ag.example.com/classifier")
	if err != nil {
		t.Fatal(err)
	}
	if !ag.SubagentOnly {
		t.Fatal("subagentOnly did not parse")
	}
	if len(ag.Tools) != 2 ||
		ag.Tools[0].Builtin != vocabulary.AgentToolGraphQL ||
		ag.Tools[1].Builtin != vocabulary.AgentToolMutate {
		t.Fatalf("tools %+v", ag.Tools)
	}
	// The unmarked sibling reads as chattable.
	sorter, err := r.ResolveAgent("ag.example.com/sorter")
	if err != nil {
		t.Fatal(err)
	}
	if sorter.SubagentOnly {
		t.Fatal("sorter is not declared subagentOnly")
	}
}

func TestAgentRefusals(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{"no prompt", `  description: d
  provider: default
  model: claude-opus-5
`, "data.prompt is required"},
		{"no provider", `  description: d
  prompt: p
`, "data.provider is required"},
		{"no model", `  description: d
  prompt: p
  provider: default
`, "data.model is required"},
		{"unknown param", `  description: d
  prompt: p
  provider: default
  model: claude-opus-5
  params: {topP: 0.5}
`, "data.params.topP is not a request param"},
		{"non-numeric temperature", `  description: d
  prompt: p
  provider: default
  model: claude-opus-5
  params: {temperature: warm}
`, "data.params.temperature"},
		{"fractional maxTokens", `  description: d
  prompt: p
  provider: default
  model: claude-opus-5
  params: {maxTokens: 1.5}
`, "data.params.maxTokens"},
		{"query without reads", `  description: d
  prompt: p
  provider: default
  model: claude-opus-5
  tools: [{callable: core.substrate.reamde.dev/query}]
`, "query needs data.reads"},
		{"propose without emit", `  description: d
  prompt: p
  provider: default
  model: claude-opus-5
  tools: [{callable: core.substrate.reamde.dev/propose}]
`, "propose needs core.substrate.reamde.dev/recordpatchrequest in data.emit"},
		{"mutate without emit", `  description: d
  prompt: p
  provider: default
  model: claude-opus-5
  tools: [{callable: core.substrate.reamde.dev/mutate}]
`, "mutate needs data.emit"},
		{"self sub-agent", `  description: d
  prompt: p
  provider: default
  model: claude-opus-5
  agents: [ag.example.com/classifier]
`, "may not name itself"},
		{"depth above cap", `  description: d
  prompt: p
  provider: default
  model: claude-opus-5
  budgets: {depth: 4}
`, "between 1 and 3"},
		{"unknown sub-agent", `  description: d
  prompt: p
  provider: default
  model: claude-opus-5
  agents: [ag.example.com/ghost]
`, "unknown agent"},
		{"tool collides with sub-agent", `  description: d
  prompt: p
  provider: default
  model: claude-opus-5
  tools:
    - {callable: ag.example.com/annotate, name: sorter}
  agents: [ag.example.com/sorter]
`, "collides with tool name"},
		{"a bare tool string", `  description: d
  prompt: p
  provider: default
  model: claude-opus-5
  tools: [frobnicate]
`, `"frobnicate" is a bare string — an entry names its callable`},
		// THE TOMBSTONE: the `builtin:` arm is deleted, whatever it named.
		{"the builtin arm", `  description: d
  prompt: p
  provider: default
  model: claude-opus-5
  tools: [{builtin: query}]
`, `builtin "query" is deleted — the built-ins are function records: {callable: core.substrate.reamde.dev/query}`},
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
