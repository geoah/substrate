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
	return `kind: substrate.reamde.dev/core/package
metadata:
  id: ag.example.com/ag
data:
  authority: ag.example.com
  package: ag
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: ag.example.com/ag/widget
data:
  authority: ag.example.com
  package: ag
  names: {singular: widget, plural: widgets}
  properties:
    name: {type: string}
---
kind: substrate.reamde.dev/core/function
metadata:
  id: ag.example.com/ag/annotate
data:
  authority: ag.example.com
  package: ag
  description: annotates a widget
  runtime: python
  source: "def main(input, host): return {}"
  permissions:
    writes: [ag.example.com/ag/widget]
---
kind: substrate.reamde.dev/core/agent
metadata:
  id: ag.example.com/ag/sorter
data:
  authority: ag.example.com
  package: ag
  description: files widgets where they belong
  prompt: You sort widgets.
  provider: default
  model: claude-opus-5
---
kind: substrate.reamde.dev/core/agent
metadata:
  id: ag.example.com/ag/classifier
data:
  authority: ag.example.com
  package: ag
` + agData
}

func loadAgAuthority(t *testing.T, agData string) (*vocabulary.Registry, error) {
	t.Helper()
	fsys := fstest.MapFS{"ag.example.com/ag/all.yaml": &fstest.MapFile{Data: []byte(agData)}}
	return vocabulary.LoadFS(fsys)
}

// coreHostStub is the minimum of core an agent fixture needs to NAME a built-in.
// The four built-ins are `runtime: host` function records, so
// `{function: substrate.reamde.dev/core/graphql}` resolves against the registry
// like any other callable — a fixture declaring no core cannot name one, which is
// exactly the refusal a real repository gets for a function nobody installed.
// (`host` is admissible here because LoadFS builds `builtin`.)
const coreHostStub = `kind: substrate.reamde.dev/core/package
metadata:
  id: substrate.reamde.dev/core
data:
  authority: substrate.reamde.dev
  package: core
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: substrate.reamde.dev/core/recordpatchrequest
data:
  authority: substrate.reamde.dev
  package: core
  names: {singular: recordpatchrequest, plural: recordpatchrequests}
  properties:
    op: {type: string}
---
kind: substrate.reamde.dev/core/function
metadata:
  id: substrate.reamde.dev/core/query
data:
  authority: substrate.reamde.dev
  package: core
  description: reads records under the caller's allowlist
  runtime: host
---
kind: substrate.reamde.dev/core/function
metadata:
  id: substrate.reamde.dev/core/graphql
data:
  authority: substrate.reamde.dev
  package: core
  description: reads the whole repository through GraphQL
  runtime: host
---
kind: substrate.reamde.dev/core/function
metadata:
  id: substrate.reamde.dev/core/mutate
data:
  authority: substrate.reamde.dev
  package: core
  description: writes through GraphQL under the calling agent's emit
  runtime: host
---
kind: substrate.reamde.dev/core/function
metadata:
  id: substrate.reamde.dev/core/propose
data:
  authority: substrate.reamde.dev
  package: core
  description: lands a reviewed change request
  runtime: host
  permissions:
    writes: [substrate.reamde.dev/core/recordpatchrequest]
`

// loadAgAuthorityWithCore is loadAgAuthority with the core stub beside it, for
// the fixtures that name a built-in.
func loadAgAuthorityWithCore(t *testing.T, agData string) (*vocabulary.Registry, error) {
	t.Helper()
	fsys := fstest.MapFS{
		"ag.example.com/ag/all.yaml":          &fstest.MapFile{Data: []byte(agData)},
		"substrate.reamde.dev/core/core.yaml": &fstest.MapFile{Data: []byte(coreHostStub)},
	}
	return vocabulary.LoadFS(fsys)
}

// A temperature outside 0..2 is refused at load, so no projected agent or
// provider record can violate the kind's own `min: 0`/`max: 2` (which the
// upgrade narrowing check does not police). One validator serves the manifest
// and the provider defaults both.
func TestParseAgentParamsTemperatureBound(t *testing.T) {
	for _, temp := range []float64{-0.1, 2.1, 3} {
		if _, err := vocabulary.ParseAgentParams(map[string]any{"temperature": temp}); err == nil {
			t.Fatalf("temperature %v accepted, want refused", temp)
		}
	}
	for _, temp := range []float64{0, 1, 2} {
		p, err := vocabulary.ParseAgentParams(map[string]any{"temperature": temp})
		if err != nil {
			t.Fatalf("temperature %v refused: %v", temp, err)
		}
		if p.Temperature == nil || float64(*p.Temperature) != temp {
			t.Fatalf("temperature %v parsed as %v", temp, p.Temperature)
		}
	}
}

func TestAgentLoads(t *testing.T) {
	r, err := loadAgAuthority(t, agAuthority(`  description: classifies widgets
  prompt: You classify widgets.
  provider: default
  model: claude-opus-5
  tools:
    - {function: ag.example.com/ag/annotate, name: markWidget, description: marks one widget}
  subagents: [ag.example.com/ag/sorter]
  budgets: {maxTurns: 4, depth: 2}
  permissions:
    writes:
      - ag.example.com/ag/widget
      - substrate.reamde.dev/core/recordpatchrequest
    reads:
      kinds: [ag.example.com/ag/widget]
`))
	// recordpatchrequest lives in core, which this fixture does not declare
	// — resolution must fail on exactly that, proving emit resolves.
	if err == nil || !strings.Contains(err.Error(), "substrate.reamde.dev/core/recordpatchrequest") {
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
    - {function: ag.example.com/ag/annotate, name: markWidget}
  subagents: [ag.example.com/ag/sorter]
  budgets: {maxTurns: 4, depth: 2}
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
	if ag.Actor() != "agent:ag.example.com:ag:classifier" {
		t.Fatalf("actor %q", ag.Actor())
	}
	if ag.Budgets.MaxTurns != 4 || ag.Budgets.Depth != 2 || ag.Budgets.MaxToolCalls != vocabulary.DefaultAgentToolCalls {
		t.Fatalf("budgets %+v", ag.Budgets)
	}
	if len(ag.Tools) != 1 || ag.Tools[0].Name != "markWidget" || ag.Tools[0].Callable != "ag.example.com/ag/annotate" {
		t.Fatalf("tools %+v", ag.Tools)
	}
	if ag.Reads == nil || ag.Reads.Calls != vocabulary.DefaultReadCalls {
		t.Fatalf("reads %+v", ag.Reads)
	}
}

// The graphql built-in needs no grant beyond its declaration (it is read-only
// and repository-wide by design), mutate rides the emit allowlist, and
// hiddenFromChat is an ordinary parsed flag: one manifest proves all three. The
// built-ins are named BY IDENTITY, and the derived Builtin word is what the
// grant checks and the loop's dispatch read off the resolved entry.
func TestAgentGraphQLBuiltinsAndHiddenFromChat(t *testing.T) {
	r, err := loadAgAuthorityWithCore(t, agAuthority(`  description: reads and writes the graph
  prompt: You tend widgets.
  provider: default
  model: claude-opus-5
  hiddenFromChat: true
  tools:
    - {function: substrate.reamde.dev/core/graphql}
    - {function: substrate.reamde.dev/core/mutate}
  permissions:
    writes: [ag.example.com/ag/widget]
`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	ag, err := r.ResolveAgent("ag.example.com/ag/classifier")
	if err != nil {
		t.Fatal(err)
	}
	if !ag.HiddenFromChat {
		t.Fatal("hiddenFromChat did not parse")
	}
	if len(ag.Tools) != 2 ||
		ag.Tools[0].Builtin != vocabulary.AgentToolGraphQL ||
		ag.Tools[1].Builtin != vocabulary.AgentToolMutate {
		t.Fatalf("tools %+v", ag.Tools)
	}
	// The unmarked sibling reads as chattable.
	sorter, err := r.ResolveAgent("ag.example.com/ag/sorter")
	if err != nil {
		t.Fatal(err)
	}
	if sorter.HiddenFromChat {
		t.Fatal("sorter is not declared hiddenFromChat")
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
  tools: [{function: substrate.reamde.dev/core/query}]
`, "query needs data.permissions.reads"},
		{"propose without emit", `  description: d
  prompt: p
  provider: default
  model: claude-opus-5
  tools: [{function: substrate.reamde.dev/core/propose}]
`, "propose needs substrate.reamde.dev/core/recordpatchrequest in data.permissions.writes"},
		{"mutate without emit", `  description: d
  prompt: p
  provider: default
  model: claude-opus-5
  tools: [{function: substrate.reamde.dev/core/mutate}]
`, "mutate needs data.permissions.writes"},
		{"self sub-agent", `  description: d
  prompt: p
  provider: default
  model: claude-opus-5
  subagents: [ag.example.com/ag/classifier]
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
  subagents: [ag.example.com/ag/ghost]
`, "unknown agent"},
		{"tool collides with sub-agent", `  description: d
  prompt: p
  provider: default
  model: claude-opus-5
  tools:
    - {function: ag.example.com/ag/annotate, name: sorter}
  subagents: [ag.example.com/ag/sorter]
`, "collides with tool name"},
		{"a bare tool string", `  description: d
  prompt: p
  provider: default
  model: claude-opus-5
  tools: [frobnicate]
`, `"frobnicate" is a bare string — an entry names its function`},
		// THE TOMBSTONE: the `builtin:` arm is deleted, whatever it named.
		{"the builtin arm", `  description: d
  prompt: p
  provider: default
  model: claude-opus-5
  tools: [{builtin: query}]
`, `builtin "query" is deleted — the built-ins are function records: {function: substrate.reamde.dev/core/query}`},
		// The hoisted grants are deleted too: an agent's two live under
		// `permissions:` beside a function's five.
		{"the hoisted emit", `  description: d
  prompt: p
  provider: default
  model: claude-opus-5
  emit: [ag.example.com/ag/widget]
`, `key "emit" is deleted — permissions.writes`},
		{"the hoisted reads", `  description: d
  prompt: p
  provider: default
  model: claude-opus-5
  reads: {kinds: [ag.example.com/ag/widget]}
`, `key "reads" is deleted — permissions.reads`},
		{"a grant outside the agent's set", `  description: d
  prompt: p
  provider: default
  model: claude-opus-5
  permissions: {network: ["api.example.com"]}
`, `data.permissions: unknown key "network"`},
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
