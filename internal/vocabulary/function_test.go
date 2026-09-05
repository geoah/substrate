package vocabulary_test

import (
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/geoah/substrate/internal/vocabulary"
)

// fnAuthority renders a minimal authority with two types and one function whose
// data is the given YAML block (indented under data:).
func fnAuthority(fnData string) string {
	return `kind: substrate.reamde.dev/core/package
metadata:
  id: fn.example.com/fn
data:
  authority: fn.example.com
  package: fn
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: fn.example.com/fn/widget
data:
  authority: fn.example.com
  package: fn
  names: {singular: widget, plural: widgets}
  properties:
    name: {type: string}
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: fn.example.com/fn/gadget
data:
  authority: fn.example.com
  package: fn
  names: {singular: gadget, plural: gadgets}
---
kind: substrate.reamde.dev/core/function
metadata:
  id: fn.example.com/fn/mirror
data:
  authority: fn.example.com
  package: fn
` + fnData
}

func loadFnAuthority(t *testing.T, fnData string) (*vocabulary.Registry, error) {
	t.Helper()
	fsys := fstest.MapFS{"fn.example.com/fn/all.yaml": &fstest.MapFile{Data: []byte(fnAuthority(fnData))}}
	return vocabulary.LoadFS(fsys)
}

// loadFnNetwork loads the mirror function with one `permissions.network` entry,
// returning any load error, so a case reads as the entry it is about.
func loadFnNetwork(t *testing.T, entry string) error {
	t.Helper()
	_, err := loadFnAuthority(t, `  description: d
  runtime: python
  permissions:
    network: [`+entry+`]
  source: "def main(input, host): return {}"
`)
	return err
}

// Issue #50: a `permissions.network` entry is a bare destination: a host, a
// host:port or a CIDR. The loader validates SHAPE, not reachability. It refuses
// a URL, a glob, a bad port and a non-host string, so a malformed entry fails at
// load; a private or loopback address is well-formed and accepted, because the
// runtime egress confinement plus its operator escape decide reachability at
// connect, not the loader.
func TestNetworkEntryGrammar(t *testing.T) {
	good := []string{
		`"api.example.com"`,       // a bare host
		`"api.example.com:443"`,   // a host with a port
		`"example.com."`,          // a rooted DNS name (one trailing dot)
		`"93.184.216.34"`,         // a public IPv4 literal
		`"93.184.216.0/24"`,       // a public CIDR
		`"2001:db8::1"`,           // a bare IPv6 literal, no brackets
		`"[2606:4700::1111]"`,     // a bracketed IPv6 with no port
		`"[2606:4700::1111]:443"`, // a bracketed IPv6 with a port
		// A private or loopback address is a valid SHAPE: the confinement plus its
		// operator escape, not the loader, decide reachability at connect.
		`"127.0.0.1:11434"`,
		`"10.0.0.0/8"`,
		`"169.254.169.254"`,
	}
	for _, entry := range good {
		if err := loadFnNetwork(t, entry); err != nil {
			t.Fatalf("well-formed network entry %s refused: %v", entry, err)
		}
	}

	bad := map[string]string{
		`"https://api.example.com"`: "is a URL",
		`"api.example.com/api/v1"`:  "is neither a host, a host:port nor a CIDR",
		`"*.example.com"`:           "carries a glob",
		`"api.example.com:70000"`:   "port outside 1..65535",
		`""`:                        "is empty",          // an empty entry
		`"not a host"`:              "is not a hostname", // a space in the host
		`"例え.jp"`:                   "is not a hostname", // a raw IDN needs punycode
	}
	for entry, want := range bad {
		err := loadFnNetwork(t, entry)
		if err == nil {
			t.Fatalf("malformed network entry %s was admitted", entry)
		}
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("network entry %s: error %q does not name %q", entry, err, want)
		}
	}
}

func TestFunctionLoads(t *testing.T) {
	r, err := loadFnAuthority(t, `  description: mirrors widgets into gadgets
  runtime: python
  timeout: PT0.25S
  arguments:
    - {name: name, type: string, required: true}
    - {name: count, type: float}
  returns:
    - {name: ok, type: bool}
  permissions:
    writes: [fn.example.com/fn/gadget]
    reads:
      kinds: [fn.example.com/fn/widget]
      budgets: {calls: 4}
    call: [fn.example.com/fn/mirror]
    network: ["api.example.com"]
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
	if fn.Identity() != "fn.example.com/fn/mirror" {
		t.Fatalf("identity: %s", fn.Identity())
	}
	if fn.Actor() != "function:fn.example.com:fn:mirror" {
		t.Fatalf("actor: %s", fn.Actor())
	}
	if fn.Description == "" {
		t.Fatal("description lost")
	}
	if fn.Runtime != vocabulary.RuntimePython || !strings.Contains(fn.Source, "def main") {
		t.Fatalf("body: %q %q", fn.Runtime, fn.Source)
	}
	if fn.Timeout != 250*time.Millisecond {
		t.Fatalf("timeout: %s", fn.Timeout)
	}
	if fn.Input == nil || fn.Output == nil {
		t.Fatal("input/output schemas lost")
	}
	// The grant, all five arms.
	if len(fn.Caps.Emit) != 1 || fn.Caps.Emit[0] != "fn.example.com/fn/gadget" {
		t.Fatalf("emit: %v", fn.Caps.Emit)
	}
	if fn.Caps.Reads == nil || fn.Caps.Reads.Calls != 4 || fn.Caps.Reads.Rows != vocabulary.DefaultReadRows {
		t.Fatalf("reads: %+v", fn.Caps.Reads)
	}
	if !fn.Caps.AllowsCall("fn.example.com/fn/mirror") || fn.Caps.AllowsCall("fn.example.com/fn/other") {
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
// declared schema refuses nil unless it declares no type at all.
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
	// A schema with NO type constrains nothing — the flat dialect's `json`
	// argument, and JSON Schema's own spelling of "any value".
	if err := vocabulary.CheckValue(map[string]any{}, nil); err != nil {
		t.Fatalf("nil refused by an untyped schema: %v", err)
	}
}

// minimalFn is a loadable function body block; the cases below break one
// thing each.
const minimalFn = `  description: mirrors widgets
  runtime: python
  permissions:
    writes: [fn.example.com/fn/gadget]
  source: "def main(input, host): return {}"
`

func TestFunctionLoadErrors(t *testing.T) {
	cases := map[string]struct {
		data string
		want string
	}{
		"description is required": {
			data: `  runtime: python
  permissions:
    writes: [fn.example.com/fn/gadget]
  source: "def main(input, host): return {}"
`,
			want: "data.description is required",
		},
		"runtime is required": {
			data: `  description: d
  permissions:
    writes: [fn.example.com/fn/gadget]
  source: "def main(input, host): return {}"
`,
			want: "data.runtime",
		},
		"runtime is python, go or host": {
			data: `  description: d
  runtime: cel
  permissions:
    writes: [fn.example.com/fn/gadget]
  source: "def main(input, host): return {}"
`,
			want: "python, go, host",
		},
		"source is required": {
			data: `  description: d
  runtime: python
  permissions:
    writes: [fn.example.com/fn/gadget]
`,
			want: "data.source is required",
		},
		"the run arm is deleted": {
			data: `  description: d
  permissions:
    writes: [fn.example.com/fn/gadget]
  run: {cel: "[]"}
`,
			want: "key \"run\" is deleted",
		},
		"the on subscription is deleted": {
			data: `  description: d
  on: {types: ["*"]}
  runtime: python
  permissions:
    writes: [fn.example.com/fn/gadget]
  source: "def main(input, host): return {}"
`,
			want: "trigger record",
		},
		"the when guard is deleted": {
			data: `  description: d
  when: record != null
  runtime: python
  permissions:
    writes: [fn.example.com/fn/gadget]
  source: "def main(input, host): return {}"
`,
			want: "trigger source.record.when",
		},
		"coalesce is deleted": {
			data: `  description: d
  coalesce: true
  runtime: python
  permissions:
    writes: [fn.example.com/fn/gadget]
  source: "def main(input, host): return {}"
`,
			want: "trigger source.record.coalesce",
		},
		"timeout is bounded": {
			data: `  description: d
  runtime: python
  timeout: PT10M
  permissions:
    writes: [fn.example.com/fn/gadget]
  source: "def main(input, host): return {}"
`,
			want: "data.timeout",
		},
		"timeout is whole milliseconds": {
			data: `  description: d
  runtime: python
  timeout: PT0.0005S
  permissions:
    writes: [fn.example.com/fn/gadget]
  source: "def main(input, host): return {}"
`,
			want: "sub-millisecond",
		},
		"reads.kinds is required": {
			data: `  description: d
  runtime: python
  permissions:
    writes: [fn.example.com/fn/gadget]
    reads: {budgets: {calls: 4}}
  source: "def main(input, host): return {}"
`,
			want: "data.permissions.reads.kinds is required",
		},
		"reads.kinds take no globs": {
			data: `  description: d
  runtime: python
  permissions:
    writes: [fn.example.com/fn/gadget]
    reads: {kinds: ["fn.example.com/*"]}
  source: "def main(input, host): return {}"
`,
			want: "no globs",
		},
		"reads.kinds must exist": {
			data: `  description: d
  runtime: python
  permissions:
    writes: [fn.example.com/fn/gadget]
    reads: {kinds: [fn.example.com/fn/nothing]}
  source: "def main(input, host): return {}"
`,
			want: "data.permissions.reads.kinds: unknown type",
		},
		"reads budgets are bounded": {
			data: `  description: d
  runtime: python
  permissions:
    writes: [fn.example.com/fn/gadget]
    reads: {kinds: [fn.example.com/fn/widget], budgets: {rows: 999999}}
  source: "def main(input, host): return {}"
`,
			want: "budgets.rows",
		},
		"mutations are merge and split only": {
			data: `  description: d
  runtime: python
  permissions:
    writes: [fn.example.com/fn/gadget]
    mutations: [delete]
  source: "def main(input, host): return {}"
`,
			want: "data.permissions.mutations[0]",
		},
		"after is reserved unimplemented": {
			data: `  description: d
  after: fn.example.com/fn/other
  runtime: python
  permissions:
    writes: [fn.example.com/fn/gadget]
  source: "def main(input, host): return {}"
`,
			want: "data.after is reserved",
		},
		"emit types must exist": {
			data: `  description: d
  runtime: python
  permissions:
    writes: [fn.example.com/fn/nothing]
  source: "def main(input, host): return {}"
`,
			want: "data.permissions.writes: unknown type",
		},
		"call targets take no globs": {
			data: `  description: d
  runtime: python
  permissions:
    writes: [fn.example.com/fn/gadget]
    call: ["fn.example.com/*"]
  source: "def main(input, host): return {}"
`,
			want: "no globs",
		},
		"call targets must be registered functions": {
			data: `  description: d
  runtime: python
  permissions:
    writes: [fn.example.com/fn/gadget]
    call: [fn.example.com/fn/nothing]
  source: "def main(input, host): return {}"
`,
			want: "data.permissions.call: unknown function",
		},
		// The retired IO spellings, each naming the flat list that replaced it.
		"the input schema is deleted": {
			data: `  description: d
  runtime: python
  input: {type: object, properties: {name: {type: string}}}
  permissions:
    writes: [fn.example.com/fn/gadget]
  source: "def main(input, host): return {}"
`,
			want: `key "input" is deleted — arguments`,
		},
		"the output schema is deleted": {
			data: `  description: d
  runtime: python
  output: {type: object}
  permissions:
    writes: [fn.example.com/fn/gadget]
  source: "def main(input, host): return {}"
`,
			want: `key "output" is deleted — returns`,
		},
		"the capability wrapper is deleted": {
			data: `  description: d
  runtime: python
  capabilities: {emit: [fn.example.com/fn/gadget]}
  source: "def main(input, host): return {}"
`,
			want: `key "capabilities" is deleted — permissions`,
		},
		// The hoisted grant keys, each naming its place inside `permissions:`.
		"the hoisted emit is deleted": {
			data: `  description: d
  runtime: python
  emit: [fn.example.com/fn/gadget]
  source: "def main(input, host): return {}"
`,
			want: `key "emit" is deleted — permissions.writes`,
		},
		"the hoisted reads is deleted": {
			data: `  description: d
  runtime: python
  reads: {kinds: [fn.example.com/fn/widget]}
  source: "def main(input, host): return {}"
`,
			want: `key "reads" is deleted — permissions.reads`,
		},
		"the hoisted call is deleted": {
			data: `  description: d
  runtime: python
  call: [fn.example.com/fn/mirror]
  source: "def main(input, host): return {}"
`,
			want: `key "call" is deleted — permissions.call`,
		},
		"the hoisted network is deleted": {
			data: `  description: d
  runtime: python
  network: ["https://*"]
  source: "def main(input, host): return {}"
`,
			want: `key "network" is deleted — permissions.network`,
		},
		"the hoisted mutations is deleted": {
			data: `  description: d
  runtime: python
  mutations: [merge]
  source: "def main(input, host): return {}"
`,
			want: `key "mutations" is deleted — permissions.mutations`,
		},
		"permissions is an object": {
			data: `  description: d
  runtime: python
  permissions: [writes]
  source: "def main(input, host): return {}"
`,
			want: "data.permissions: an OBJECT of grants",
		},
		"a grant outside the set is refused": {
			data: `  description: d
  runtime: python
  permissions: {writes: [fn.example.com/fn/gadget], emit: [fn.example.com/fn/gadget]}
  source: "def main(input, host): return {}"
`,
			want: "data.permissions: unknown key",
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
  permissions:
    writes: [fn.example.com/fn/gadget]
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
	fn, err := r.ResolveFunction("fn.example.com/fn/mirror")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if fn.Timeout != vocabulary.DefaultRunTimeout {
		t.Fatalf("default timeout: %s", fn.Timeout)
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
	for _, ok := range []string{"*", "fn.example.com/*", "fn.example.com/fn/widget", "widget"} {
		if !vocabulary.ValidTypeGlob(ok) {
			t.Fatalf("%q must be a valid glob", ok)
		}
	}
	for _, bad := range []string{"Widget", "wid*fn.example.com/fn/get", "*."} {
		if vocabulary.ValidTypeGlob(bad) {
			t.Fatalf("%q must not be a valid glob", bad)
		}
	}
	if !vocabulary.MatchTypeGlob("*", "anything.at.all") ||
		!vocabulary.MatchTypeGlob("fn.example.com/*", "fn.example.com/fn/widget") ||
		vocabulary.MatchTypeGlob("fn.example.com/*", "samples.substrate.reamde.dev/tasks/task") ||
		!vocabulary.MatchTypeGlob("fn.example.com/fn/widget", "fn.example.com/fn/widget") {
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
	m := vocabulary.FunctionManifest("fn.example.com/fn", "mirror", map[string]any{
		"description": "mirrors",
		"runtime":     vocabulary.RuntimePython,
		"permissions": map[string]any{"writes": []any{"fn.example.com/fn/gadget"}},
		"source":      "def main(input, host): return {}",
	})
	if m["kind"] != vocabulary.CoreKind(vocabulary.DocFunction) {
		t.Fatalf("kind: %v", m["kind"])
	}
	meta := m["metadata"].(map[string]any)
	if meta["id"] != "fn.example.com/fn/mirror" {
		t.Fatalf("id: %v", meta["id"])
	}
	data := m["data"].(map[string]any)
	if data["authority"] != "fn.example.com" {
		t.Fatalf("authority: %v", data["authority"])
	}
}
