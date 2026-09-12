package catalog

import (
	"testing"
	"testing/fstest"
)

// The closure names every plane a bundle adds, and says what each one IS. A
// traits-only package used to reach the wire as an empty closure, because
// nothing read trait documents at all, and a function or an agent arrived as a
// bare id with the prose it declares left behind in the file. Held against a
// hand-written tree rather than a shipped one, so it stays true whatever the
// shipped packages are called.
func TestClosureCarriesEveryPlaneWithItsProse(t *testing.T) {
	fsys := fstest.MapFS{
		"demo/bundle.yaml": &fstest.MapFile{Data: []byte(demoManifest)},
	}
	c, err := Load(SampleRoot(fsys))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	b, ok := c.ByID("demo.example.com/demo")
	if !ok {
		t.Fatal("the hand-written bundle is not in the catalog")
	}
	cl := b.Closure
	if want := []string{"demo.example.com/demo/repeating"}; !sameIDs(cl.Traits, want) {
		t.Errorf("traits = %v, want %v", cl.Traits, want)
	}
	for _, tc := range []struct {
		what string
		got  string
		want string
	}{
		{"trait", cl.TraitDescriptions["demo.example.com/demo/repeating"], "a thing that repeats"},
		{"kind", cl.KindDescriptions["demo.example.com/demo/note"], "a note"},
		{"function", cl.FunctionDescriptions["demo.example.com/demo/ingest"], "turns a request into a note"},
		{"agent", cl.AgentDescriptions["demo.example.com/demo/scribe"], "writes notes down"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s description = %q, want %q", tc.what, tc.got, tc.want)
		}
	}
	if want := []string{"demo-webhook"}; !sameIDs(cl.Triggers, want) {
		t.Errorf("triggers = %v, want %v", cl.Triggers, want)
	}
	const callable = "substrate.reamde.dev/core/function/demo.example.com/demo/ingest"
	if got := cl.TriggerCallables["demo-webhook"]; got != callable {
		t.Errorf("trigger callable = %q, want %q", got, callable)
	}
	// A trait is a declaration, so it rides the vocabulary batch rather than
	// the data plane. The trigger and the row whose kind is merely NAMED
	// `trait` are the two records this closure writes, and neither is counted
	// as a declaration of it.
	if got := len(cl.Records); got != 2 {
		t.Errorf("records = %v, want the trigger and the row", cl.Records)
	}
	for _, r := range cl.Records {
		if r.ID == "a-row" && r.Kind != "other.example.com/things/trait" {
			t.Errorf("the row lost its kind: %+v", r)
		}
	}
	if sameIDs(cl.Traits, []string{"demo.example.com/demo/repeating", "a-row"}) {
		t.Error("a data record of a kind named trait was counted as a trait")
	}
}

func sameIDs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

const demoManifest = `
kind: substrate.reamde.dev/core/bundle
metadata:
  id: demo.example.com/demo
data:
  authority: demo.example.com
  package: demo
  description: a hand-written closure
---
kind: substrate.reamde.dev/core/trait
metadata:
  id: demo.example.com/demo/repeating
data:
  description: a thing that repeats
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: demo.example.com/demo/note
data:
  description: a note
---
kind: substrate.reamde.dev/core/function
metadata:
  id: demo.example.com/demo/ingest
data:
  description: turns a request into a note
---
kind: substrate.reamde.dev/core/agent
metadata:
  id: demo.example.com/demo/scribe
data:
  description: writes notes down
---
kind: substrate.reamde.dev/core/trigger
metadata:
  id: demo-webhook
data:
  properties:
    callable: substrate.reamde.dev/core/function/demo.example.com/demo/ingest
---
# A DATA record whose kind is NAMED trait under somebody else's package. It
# is a row, not a declaration, and the closure must count it as one.
kind: other.example.com/things/trait
metadata:
  id: a-row
data:
  properties:
    name: not a declaration
`
