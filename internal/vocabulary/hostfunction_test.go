package vocabulary_test

// `runtime: host` — the function whose body is the engine — and the rule that
// made it possible: a function may declare no `emit`, because a pure function
// returns its output and writes nothing.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/vocabulary"
)

// A PURE FUNCTION IS A FUNCTION. `emit` used to be required and non-empty, which
// taught authors to name a kind they never wrote to.
func TestPureFunctionDeclaresNoEmit(t *testing.T) {
	r, err := loadFnAuthority(t, `  description: reads the web and returns hits
  runtime: python
  arguments:
    - {name: query, type: string, required: true}
  returns:
    - {name: results, type: json, repeated: true}
  permissions:
    network: ["api.example.com"]
  source: "def main(input, host): return {'output': {'results': []}}"
`)
	if err != nil {
		t.Fatalf("a pure function refused: %v", err)
	}
	fn, err := r.ResolveFunction("fn.example.com/fn/mirror")
	if err != nil {
		t.Fatal(err)
	}
	if len(fn.Caps.Emit) != 0 {
		t.Fatalf("emit %v — the declaration named none", fn.Caps.Emit)
	}
}

// hostFn is one host function declaration under a shipped authority.
func hostFn(data string) string {
	return `kind: substrate.reamde.dev/core/package
metadata:
  id: hf.example.com/hf
data:
  authority: hf.example.com
  package: hf
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: hf.example.com/hf/widget
data:
  authority: hf.example.com
  package: hf
  names: {singular: widget, plural: widgets}
---
kind: substrate.reamde.dev/core/function
metadata:
  id: hf.example.com/hf/ask
data:
  authority: hf.example.com
  package: hf
` + data
}

func TestHostFunctionLoadsAsACardAlone(t *testing.T) {
	fsys := fstest.MapFS{"hf.example.com/hf/all.yaml": &fstest.MapFile{Data: []byte(hostFn(
		`  description: runs a read-only GraphQL query against the repository
  runtime: host
  arguments:
    - {name: query, type: string, required: true, description: the document to execute}
    - {name: variables, type: json}
`))}}
	r, err := vocabulary.LoadFS(fsys)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	fn, err := r.ResolveFunction("hf.example.com/hf/ask")
	if err != nil {
		t.Fatal(err)
	}
	if !fn.IsHost() {
		t.Fatalf("runtime %q is not host", fn.Runtime)
	}
	if fn.Source != "" || fn.Timeout != 0 {
		t.Fatalf("a host function carries a body or a timeout: source=%q timeout=%s", fn.Source, fn.Timeout)
	}
	// The card is the declaration: the compiled schema is what the model is shown.
	props, _ := fn.Input["properties"].(map[string]any)
	if len(props) != 2 {
		t.Fatalf("arguments compiled to %#v", fn.Input)
	}
	if _, typed := props["variables"].(map[string]any)["type"]; typed {
		t.Fatalf("a json argument declares a type: %#v", props["variables"])
	}
}

// ONLY A SHIPPED BUILD MAY DECLARE ONE. The engine implements what the engine
// ships, so a `host` runtime arriving through an install names a body nothing has.
func TestHostRuntimeIsRefusedFromAnInstalledAuthority(t *testing.T) {
	doc := hostFn(`  description: claims the engine implements it
  runtime: host
`)
	if _, err := vocabulary.ParseYAML([]byte(doc), vocabulary.SourceBuiltin); err != nil {
		t.Fatalf("the shipped build refused a host function: %v", err)
	}
	_, err := vocabulary.ParseYAML([]byte(doc), vocabulary.SourceInstalled)
	if err == nil || !strings.Contains(err.Error(), "host is the ENGINE's own implementation") {
		t.Fatalf("an installed host function was admitted: %v", err)
	}
}

func TestHostFunctionRefusals(t *testing.T) {
	cases := map[string]struct{ data, want string }{
		"a body": {
			`  description: d
  runtime: host
  source: "def main(input, host): return {}"
`,
			"a host function has no inline body",
		},
		"a timeout": {
			`  description: d
  runtime: host
  timeout: PT0.25S
`,
			"a host function runs in process under its caller's budgets",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fsys := fstest.MapFS{"hf.example.com/hf/all.yaml": &fstest.MapFile{Data: []byte(hostFn(tc.data))}}
			_, err := vocabulary.LoadFS(fsys)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got: %v", tc.want, err)
			}
		})
	}
}

// A BODY CANNOT SUB-CALL A HOST FUNCTION: the engine runs one under a CALLER's
// grants, and a function body has none to lend.
func TestFunctionCallRefusesAHostTarget(t *testing.T) {
	fsys := fstest.MapFS{"hf.example.com/hf/all.yaml": &fstest.MapFile{Data: []byte(hostFn(
		`  description: d
  runtime: host
`) + `---
kind: substrate.reamde.dev/core/function
metadata:
  id: hf.example.com/hf/caller
data:
  authority: hf.example.com
  package: hf
  description: tries to call the built-in
  runtime: python
  permissions:
    call: [hf.example.com/hf/ask]
  source: "def main(input, host): return {}"
`)}}
	_, err := vocabulary.LoadFS(fsys)
	if err == nil || !strings.Contains(err.Error(), "is a host function") {
		t.Fatalf("a body was allowed to sub-call a host function: %v", err)
	}
}

// A BARE NAME NEVER RESOLVES TO A HOST FUNCTION. The four are named for what
// they do, which is exactly what a repository's own function is likeliest to be
// called, so `query` keeps meaning the user's `query`.
func TestBareNameSkipsHostFunctions(t *testing.T) {
	fsys := fstest.MapFS{
		"substrate.reamde.dev/core/core.yaml": &fstest.MapFile{Data: []byte(coreHostStub)},
		"own.example.com/own/all.yaml": &fstest.MapFile{Data: []byte(`kind: substrate.reamde.dev/core/package
metadata:
  id: own.example.com/own
data:
  authority: own.example.com
  package: own
  version: 1
---
kind: substrate.reamde.dev/core/function
metadata:
  id: own.example.com/own/query
data:
  authority: own.example.com
  package: own
  description: the repository's own query helper
  runtime: python
  source: "def main(input, host): return {}"
`)},
	}
	r, err := vocabulary.LoadFS(fsys)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	fn, err := r.ResolveFunction("query")
	if err != nil {
		t.Fatalf("the bare name did not resolve: %v", err)
	}
	if fn.Identity() != "own.example.com/own/query" {
		t.Fatalf("the bare name resolved to %s", fn.Identity())
	}
	// The identity still resolves the built-in.
	host, err := r.ResolveFunction(vocabulary.HostFunctionQuery)
	if err != nil || !host.IsHost() {
		t.Fatalf("the host identity did not resolve: %v", err)
	}
	// And a bare name with ONLY a host function behind it says where to look.
	if _, err := r.ResolveFunction("graphql"); err == nil ||
		!strings.Contains(err.Error(), vocabulary.HostFunctionGraphQL) {
		t.Fatalf("the bare built-in name resolved or misreported: %v", err)
	}
}
