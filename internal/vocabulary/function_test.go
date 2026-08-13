package vocabulary_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/vocabulary"
)

// fnAuthority renders a minimal authority with two types and one function whose
// data is the given YAML block (indented under data:).
func fnAuthority(fnData string) string {
	return `kind: core.substrate.reamde.dev/authority
metadata:
  id: fn.example.com
data:
  version: v1alpha1
---
kind: core.substrate.reamde.dev/kind
metadata:
  id: fn.example.com/widget
data:
  authority: fn.example.com
  names: {singular: widget, plural: widgets}
  properties:
    name: {type: string}
---
kind: core.substrate.reamde.dev/kind
metadata:
  id: fn.example.com/gadget
data:
  authority: fn.example.com
  names: {singular: gadget, plural: gadgets}
---
kind: core.substrate.reamde.dev/function
metadata:
  id: fn.example.com/mirror
data:
  authority: fn.example.com
` + fnData
}

func loadFnAuthority(t *testing.T, fnData string) (*vocabulary.Registry, error) {
	t.Helper()
	fsys := fstest.MapFS{"fn.example.com/all.yaml": &fstest.MapFile{Data: []byte(fnAuthority(fnData))}}
	return vocabulary.LoadFS(fsys)
}

func TestFunctionLoads(t *testing.T) {
	r, err := loadFnAuthority(t, `  description: mirrors widgets into gadgets
  runtime: python
  timeoutMs: 250
  input:
    type: object
    properties:
      name: {type: string}
      count: {type: number}
    required: [name]
  output:
    type: object
    properties:
      ok: {type: boolean}
  capabilities:
    emit: [fn.example.com/gadget]
    reads:
      kinds: [fn.example.com/widget]
      budgets: {calls: 4}
    call: [fn.example.com/mirror]
    network: ["https://*"]
    mutations: [merge]
  source: |
    def main(input, host):
        return {"effects": []}
`)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	fn, err := r.ResolveFunction("mirror")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if fn.Identity() != "fn.example.com/mirror" {
		t.Fatalf("identity: %s", fn.Identity())
	}
	if fn.Actor() != "function:mirror" {
		t.Fatalf("actor: %s", fn.Actor())
	}
	if fn.Description == "" {
		t.Fatal("description lost")
	}
	if fn.Runtime != vocabulary.RuntimePython || !strings.Contains(fn.Source, "def main") {
		t.Fatalf("body: %q %q", fn.Runtime, fn.Source)
	}
	if fn.TimeoutMs != 250 {
		t.Fatalf("timeoutMs: %d", fn.TimeoutMs)
	}
	if fn.Input == nil || fn.Output == nil {
		t.Fatal("input/output schemas lost")
	}
	// The capability envelope, all five arms.
	if len(fn.Caps.Emit) != 1 || fn.Caps.Emit[0] != "fn.example.com/gadget" {
		t.Fatalf("emit: %v", fn.Caps.Emit)
	}
	if fn.Caps.Reads == nil || fn.Caps.Reads.Calls != 4 || fn.Caps.Reads.Rows != vocabulary.DefaultReadRows {
		t.Fatalf("reads: %+v", fn.Caps.Reads)
	}
	if !fn.Caps.AllowsCall("fn.example.com/mirror") || fn.Caps.AllowsCall("fn.example.com/other") {
		t.Fatalf("call: %v", fn.Caps.Call)
	}
	if len(fn.Caps.Network) != 1 {
		t.Fatalf("network: %v", fn.Caps.Network)
	}
	if !fn.Caps.AllowsMutation(vocabulary.MutationMerge) || fn.Caps.AllowsMutation(vocabulary.MutationSplit) {
		t.Fatalf("mutations: %v", fn.Caps.Mutations)
	}
	// The declared input schema holds values to its shape.
	if err := vocabulary.CheckValue(fn.Input, map[string]any{"name": "x", "count": float64(2)}); err != nil {
		t.Fatalf("valid input refused: %v", err)
	}
	if err := vocabulary.CheckValue(fn.Input, map[string]any{"count": float64(2)}); err == nil {
		t.Fatal("missing required property accepted")
	}
	if err := vocabulary.CheckValue(fn.Input, map[string]any{"name": "x", "extra": true}); err == nil {
		t.Fatal("undeclared property accepted")
	}
	if err := vocabulary.CheckValue(fn.Input, map[string]any{"name": 4.0}); err == nil {
		t.Fatal("mistyped property accepted")
	}
}

// Review W2 #11: key PRESENCE closes an object — `properties: {}` declares
// "no properties" and refuses every key; only a schema with no `properties`
// key at all is the bare open object. And review W2 #8's dialect half: a
// declared schema refuses nil unless it is `any`.
func TestCheckValueEmptyPropertiesClosesTheObject(t *testing.T) {
	closed := map[string]any{"type": "object", "properties": map[string]any{}}
	if err := vocabulary.CheckValue(closed, map[string]any{"anything": true}); err == nil {
		t.Fatal("an explicitly empty properties map admitted an undeclared key")
	}
	if err := vocabulary.CheckValue(closed, map[string]any{}); err != nil {
		t.Fatalf("the empty object refused: %v", err)
	}
	open := map[string]any{"type": "object"}
	if err := vocabulary.CheckValue(open, map[string]any{"anything": true}); err != nil {
		t.Fatalf("the bare object schema closed: %v", err)
	}
	// nil against a declared shape is a violation; `any` alone stays open.
	if err := vocabulary.CheckValue(closed, nil); err == nil {
		t.Fatal("nil passed a declared object schema")
	}
	if err := vocabulary.CheckValue(map[string]any{"type": "string"}, nil); err == nil {
		t.Fatal("nil passed a declared string schema")
	}
	if err := vocabulary.CheckValue(map[string]any{"type": "any"}, nil); err != nil {
		t.Fatalf("nil refused by any: %v", err)
	}
}

// minimalFn is a loadable function body block; the cases below break one
// thing each.
const minimalFn = `  description: mirrors widgets
  runtime: python
  capabilities: {emit: [fn.example.com/gadget]}
  source: "def main(input, host): return {}"
`

func TestFunctionLoadErrors(t *testing.T) {
	cases := map[string]struct {
		data string
		want string
	}{
		"description is required": {
			data: `  runtime: python
  capabilities: {emit: [fn.example.com/gadget]}
  source: "def main(input, host): return {}"
`,
			want: "data.description is required",
		},
		"runtime is required": {
			data: `  description: d
  capabilities: {emit: [fn.example.com/gadget]}
  source: "def main(input, host): return {}"
`,
			want: "data.runtime",
		},
		"runtime is python or go": {
			data: `  description: d
  runtime: cel
  capabilities: {emit: [fn.example.com/gadget]}
  source: "def main(input, host): return {}"
`,
			want: "python or go",
		},
		"source is required": {
			data: `  description: d
  runtime: python
  capabilities: {emit: [fn.example.com/gadget]}
`,
			want: "data.source is required",
		},
		"the run arm is deleted": {
			data: `  description: d
  capabilities: {emit: [fn.example.com/gadget]}
  run: {cel: "[]"}
`,
			want: "key \"run\" is deleted",
		},
		"the on subscription is deleted": {
			data: `  description: d
  on: {types: ["*"]}
  runtime: python
  capabilities: {emit: [fn.example.com/gadget]}
  source: "def main(input, host): return {}"
`,
			want: "trigger record",
		},
		"the when guard is deleted": {
			data: `  description: d
  when: record != null
  runtime: python
  capabilities: {emit: [fn.example.com/gadget]}
  source: "def main(input, host): return {}"
`,
			want: "trigger source.record.when",
		},
		"coalesce is deleted": {
			data: `  description: d
  coalesce: true
  runtime: python
  capabilities: {emit: [fn.example.com/gadget]}
  source: "def main(input, host): return {}"
`,
			want: "trigger source.record.coalesce",
		},
		"top-level emit is deleted": {
			data: `  description: d
  runtime: python
  emit: [fn.example.com/gadget]
  source: "def main(input, host): return {}"
`,
			want: "capabilities.emit",
		},
		"top-level reads is deleted": {
			data: `  description: d
  runtime: python
  reads: {kinds: [fn.example.com/widget]}
  capabilities: {emit: [fn.example.com/gadget]}
  source: "def main(input, host): return {}"
`,
			want: "capabilities.reads",
		},
		"timeout is bounded": {
			data: `  description: d
  runtime: python
  timeoutMs: 600000
  capabilities: {emit: [fn.example.com/gadget]}
  source: "def main(input, host): return {}"
`,
			want: "data.timeoutMs",
		},
		"reads.kinds is required": {
			data: `  description: d
  runtime: python
  capabilities:
    emit: [fn.example.com/gadget]
    reads: {budgets: {calls: 4}}
  source: "def main(input, host): return {}"
`,
			want: "reads.kinds is required",
		},
		"reads.kinds take no globs": {
			data: `  description: d
  runtime: python
  capabilities:
    emit: [fn.example.com/gadget]
    reads: {kinds: ["fn.example.com/*"]}
  source: "def main(input, host): return {}"
`,
			want: "no globs",
		},
		"reads.kinds must exist": {
			data: `  description: d
  runtime: python
  capabilities:
    emit: [fn.example.com/gadget]
    reads: {kinds: [fn.example.com/nothing]}
  source: "def main(input, host): return {}"
`,
			want: "reads.kinds: unknown type",
		},
		"reads budgets are bounded": {
			data: `  description: d
  runtime: python
  capabilities:
    emit: [fn.example.com/gadget]
    reads: {kinds: [fn.example.com/widget], budgets: {rows: 999999}}
  source: "def main(input, host): return {}"
`,
			want: "budgets.rows",
		},
		"mutations are merge and split only": {
			data: `  description: d
  runtime: python
  capabilities:
    emit: [fn.example.com/gadget]
    mutations: [delete]
  source: "def main(input, host): return {}"
`,
			want: "data.capabilities.mutations[0]",
		},
		"after is reserved unimplemented": {
			data: `  description: d
  after: fn.example.com/other
  runtime: python
  capabilities: {emit: [fn.example.com/gadget]}
  source: "def main(input, host): return {}"
`,
			want: "data.after is reserved",
		},
		"emit is required": {
			data: `  description: d
  runtime: python
  source: "def main(input, host): return {}"
`,
			want: "capabilities.emit is required",
		},
		"emit types must exist": {
			data: `  description: d
  runtime: python
  capabilities: {emit: [fn.example.com/nothing]}
  source: "def main(input, host): return {}"
`,
			want: "capabilities.emit: unknown type",
		},
		"call targets take no globs": {
			data: `  description: d
  runtime: python
  capabilities:
    emit: [fn.example.com/gadget]
    call: ["fn.example.com/*"]
  source: "def main(input, host): return {}"
`,
			want: "no globs",
		},
		"call targets must be registered functions": {
			data: `  description: d
  runtime: python
  capabilities:
    emit: [fn.example.com/gadget]
    call: [fn.example.com/nothing]
  source: "def main(input, host): return {}"
`,
			want: "capabilities.call: unknown function",
		},
		"input schema type is checked": {
			data: `  description: d
  runtime: python
  input: {type: struct}
  capabilities: {emit: [fn.example.com/gadget]}
  source: "def main(input, host): return {}"
`,
			want: "data.input",
		},
		"input schema keys are closed": {
			data: `  description: d
  runtime: python
  input: {type: object, additionalProperties: false}
  capabilities: {emit: [fn.example.com/gadget]}
  source: "def main(input, host): return {}"
`,
			want: "unknown key",
		},
		"required names declared properties": {
			data: `  description: d
  runtime: python
  input:
    type: object
    properties: {name: {type: string}}
    required: [missing]
  capabilities: {emit: [fn.example.com/gadget]}
  source: "def main(input, host): return {}"
`,
			want: "not a declared property",
		},
		"output schema nests validation": {
			data: `  description: d
  runtime: python
  output:
    type: array
    items: {type: struct}
  capabilities: {emit: [fn.example.com/gadget]}
  source: "def main(input, host): return {}"
`,
			want: "data.output.items",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := loadFnAuthority(t, tc.data)
			if err == nil {
				t.Fatal("expected a load error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestFunctionSourceSizeCap(t *testing.T) {
	big := strings.Repeat("# padding\n", vocabulary.SourceMaxBytes/10+1)
	_, err := loadFnAuthority(t, `  description: d
  runtime: python
  capabilities: {emit: [fn.example.com/gadget]}
  source: |
`+"    def main(input, host): return {}\n    "+strings.ReplaceAll(big, "\n", "\n    ")+"\n")
	if err == nil || !strings.Contains(err.Error(), "the inline cap is") {
		t.Fatalf("size cap not enforced: %v", err)
	}
}

func TestFunctionDefaults(t *testing.T) {
	r, err := loadFnAuthority(t, minimalFn)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	fn, err := r.ResolveFunction("fn.example.com/mirror")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if fn.TimeoutMs != vocabulary.DefaultRunTimeoutMs {
		t.Fatalf("default timeout: %d", fn.TimeoutMs)
	}
	if fn.Input != nil || fn.Output != nil {
		t.Fatal("undeclared input/output must stay nil")
	}
	if len(fn.Caps.Call) != 0 {
		t.Fatalf("call defaults empty: %v", fn.Caps.Call)
	}
}

func TestTriggerGlobHelpers(t *testing.T) {
	// The glob vocabulary trigger sources use, exported for the engine's
	// trigger admission.
	for _, ok := range []string{"*", "fn.example.com/*", "fn.example.com/widget", "widget"} {
		if !vocabulary.ValidTypeGlob(ok) {
			t.Fatalf("%q must be a valid glob", ok)
		}
	}
	for _, bad := range []string{"Widget", "wid*fn.example.com/get", "*."} {
		if vocabulary.ValidTypeGlob(bad) {
			t.Fatalf("%q must not be a valid glob", bad)
		}
	}
	if !vocabulary.MatchTypeGlob("*", "anything.at.all") ||
		!vocabulary.MatchTypeGlob("fn.example.com/*", "fn.example.com/widget") ||
		vocabulary.MatchTypeGlob("fn.example.com/*", "tasks.substrate.geoah.me/task") ||
		!vocabulary.MatchTypeGlob("fn.example.com/widget", "fn.example.com/widget") {
		t.Fatal("glob matching broke")
	}
	for _, op := range []string{"create", "update", "delete"} {
		if !vocabulary.ValidFunctionOp(op) {
			t.Fatalf("%q must be a valid op", op)
		}
	}
	if vocabulary.ValidFunctionOp("upsert") {
		t.Fatal("upsert is not an op")
	}
}

func TestFunctionManifestRenders(t *testing.T) {
	m := vocabulary.FunctionManifest("fn.example.com", "mirror", map[string]any{
		"description":  "mirrors",
		"runtime":      vocabulary.RuntimePython,
		"capabilities": map[string]any{"emit": []any{"fn.example.com/gadget"}},
		"source":       "def main(input, host): return {}",
	})
	if m["kind"] != vocabulary.CoreKind(vocabulary.DocFunction) {
		t.Fatalf("kind: %v", m["kind"])
	}
	meta := m["metadata"].(map[string]any)
	if meta["id"] != "fn.example.com/mirror" {
		t.Fatalf("id: %v", meta["id"])
	}
	data := m["data"].(map[string]any)
	if data["authority"] != "fn.example.com" {
		t.Fatalf("authority: %v", data["authority"])
	}
}
