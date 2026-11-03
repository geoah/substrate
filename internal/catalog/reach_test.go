package catalog

import (
	"reflect"
	"testing"
	"testing/fstest"
)

// A hand-written closure whose code reaches in every way a closure's can: a
// function that reads, writes and declares hosts, an agent with both
// grants, a record trigger and a mapping.
const reachManifest = `
kind: substrate.reamde.dev/core/package
metadata:
  id: demo.example.com/demo
data:
  authority: demo.example.com
  package: demo
  version: 3
---
kind: substrate.reamde.dev/core/bundle
metadata:
  id: demo.example.com/demo
data:
  authority: demo.example.com
  package: demo
  description: a demo
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: demo.example.com/demo/note
data:
  authority: demo.example.com
  package: demo
  name: note
  description: a note
---
kind: substrate.reamde.dev/core/function
metadata:
  id: demo.example.com/demo/ingest
data:
  authority: demo.example.com
  package: demo
  name: ingest
  description: turns a request into a note
  permissions:
    reads:
      kinds: [note, other.example.com/things/*]
    writes: [note]
    network: [api.example.com, 10.0.0.1:8443]
---
kind: substrate.reamde.dev/core/agent
metadata:
  id: demo.example.com/demo/scribe
data:
  authority: demo.example.com
  package: demo
  description: writes notes down
  prompt: write
  provider: default
  model: gpt-5
  permissions:
    reads:
      kinds: [note, substrate.reamde.dev/health/sleep]
    writes: [note]
---
kind: substrate.reamde.dev/core/recordmapping
metadata:
  id: demo.example.com/demo/fromthing
data:
  authority: demo.example.com
  package: demo
  from: other.example.com/things/thing
  to: demo.example.com/demo/note
  property: subject
---
kind: substrate.reamde.dev/core/trigger
metadata:
  id: demo-on-sleep
data:
  properties:
    enabled: true
    source:
      record:
        kinds: [substrate.reamde.dev/health/sleep, other.example.com/*]
    callable: substrate.reamde.dev/core/function/demo.example.com/demo/ingest
`

func reachBundle(t *testing.T, manifest string) *Bundle {
	t.Helper()
	c, err := Load(ProviderRoot(fstest.MapFS{"demo/bundle.yaml": &fstest.MapFile{Data: []byte(manifest)}}))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	b, ok := c.ByID("demo.example.com/demo")
	if !ok {
		t.Fatalf("the bundle is not in the catalog: %v", c.Warnings())
	}
	return b
}

// The reach of a closure is every kind its functions, agents, triggers and
// mappings read or write, one entry per kind and verb with everything that
// reaches it, bare names completed under the declaring package.
func TestAClosuresReachIsEverythingItsCodeTouches(t *testing.T) {
	b := reachBundle(t, reachManifest)
	fn := ReachBy{Type: "function", ID: "demo.example.com/demo/ingest"}
	agent := ReachBy{Type: "agent", ID: "demo.example.com/demo/scribe"}
	mapping := ReachBy{Type: "recordmapping", ID: "demo.example.com/demo/fromthing"}
	trigger := ReachBy{Type: "trigger", ID: "demo-on-sleep"}
	want := []Reach{
		{Pattern: "demo.example.com/demo/note", By: []ReachBy{fn, agent}},
		{Pattern: "other.example.com/things/*", By: []ReachBy{fn}},
		{Pattern: "demo.example.com/demo/note", Write: true, By: []ReachBy{fn, agent, mapping}},
		{Pattern: "substrate.reamde.dev/health/sleep", By: []ReachBy{agent, trigger}},
		{Pattern: "other.example.com/things/thing", By: []ReachBy{mapping}},
		{Pattern: "other.example.com/*", By: []ReachBy{trigger}},
	}
	if got := b.Reach(); !reflect.DeepEqual(got, want) {
		t.Fatalf("reach =\n%+v\nwant\n%+v", got, want)
	}
	if f := b.Functions(); len(f) != 1 || !reflect.DeepEqual(f[0].Reads, []string{"demo.example.com/demo/note", "other.example.com/things/*"}) ||
		!reflect.DeepEqual(f[0].Network, []string{"api.example.com", "10.0.0.1:8443"}) {
		t.Fatalf("functions = %+v", f)
	}
	if a := b.Agents(); len(a) != 1 || a[0].ID != agent.ID || len(a[0].Reads) != 2 || a[0].Writes[0] != "demo.example.com/demo/note" {
		t.Fatalf("agents = %+v", a)
	}
	if m := b.Mappings(); len(m) != 1 || m[0].From != "other.example.com/things/thing" || m[0].To != "demo.example.com/demo/note" {
		t.Fatalf("mappings = %+v", m)
	}
	if tr := b.Triggers(); len(tr) != 1 || !reflect.DeepEqual(tr[0].Kinds, []string{"substrate.reamde.dev/health/sleep", "other.example.com/*"}) {
		t.Fatalf("triggers = %+v", tr)
	}
}

// An upgrade's reach grows when the candidate reaches what the held closure
// did not: a new kind, a write where there was a read, a wider glob. It
// does not grow for the same reach by other code, a narrower one, or a read
// where there was a write.
func TestReachGrowsOnlyPastWhatIsHeld(t *testing.T) {
	read := func(p string) Reach { return Reach{Pattern: p} }
	write := func(p string) Reach { return Reach{Pattern: p, Write: true} }
	held := []Reach{
		write("demo.example.com/demo/note"), read("other.example.com/things/*"), write("acme.example.com/*"),
	}
	for name, c := range map[string]struct {
		candidate []Reach
		grows     []Reach
	}{
		"the same": {candidate: held},
		"other code, same reach": {candidate: []Reach{
			{Pattern: "demo.example.com/demo/note", Write: true, By: []ReachBy{{Type: "agent", ID: "demo.example.com/demo/scribe"}}},
		}},
		"a read where a write was held":  {candidate: []Reach{read("demo.example.com/demo/note")}},
		"a kind under a held glob":       {candidate: []Reach{read("other.example.com/things/thing"), write("acme.example.com/stock/item")}},
		"a narrower glob":                {candidate: []Reach{read("acme.example.com/stock/*")}},
		"a new kind":                     {candidate: []Reach{read("demo.example.com/demo/draft")}, grows: []Reach{read("demo.example.com/demo/draft")}},
		"a write where a read was held":  {candidate: []Reach{write("other.example.com/things/thing")}, grows: []Reach{write("other.example.com/things/thing")}},
		"a wider glob":                   {candidate: []Reach{read("other.example.com/*")}, grows: []Reach{read("other.example.com/*")}},
		"a glob over an exact held kind": {candidate: []Reach{write("demo.example.com/demo/*")}, grows: []Reach{write("demo.example.com/demo/*")}},
		"nothing held":                   {candidate: []Reach{read("x.example.com/y/z")}, grows: []Reach{read("x.example.com/y/z")}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := ReachGrows(held, c.candidate); !reflect.DeepEqual(got, c.grows) {
				t.Fatalf("ReachGrows = %+v, want %+v", got, c.grows)
			}
		})
	}
	if got := ReachGrows(nil, nil); got != nil {
		t.Fatalf("nothing against nothing grows %+v", got)
	}
}
