package vocabulary_test

// The `writer:` on every property shape (decision 0290): the types of actor
// that may write it, one or a list. A sync's resume cursor is an object and a
// connection's identity (`account.user`, `account.address`) is a reference,
// and both are the sync's hand exactly as a scalar cursor is. The console
// offers a property a person may write for editing, so a writer the loader
// dropped on those two shapes was the Add account form asking a person to
// type a page token and pick their own identity out of an empty list. Parsed
// on the scalar, object and reference branches alike; refused when it names
// no type of actor, or a word from before actor types, naming its
// replacement; a state machine's key set has no such key.

import (
	"slices"
	"testing"
)

func TestWriterOnEveryShape(t *testing.T) {
	ty := loadThing(t, `  properties:
    label: {type: string}
    cadence: {type: string, writer: person}
    cursor: {type: string, writer: [function, agent]}
    resume:
      type: object
      writer: [function, agent]
      fields:
        pageToken: {type: string}
    owner:
      type: reference
      kind: any
      writer: [function, agent]
    grant:
      type: reference
      kind: any
      writer: system
    shared: {type: string, writer: [person, app]}
`)
	for name, want := range map[string][]string{
		"label":   nil,
		"cadence": {"person"},
		"cursor":  {"function", "agent"},
		"resume":  {"function", "agent"},
		"owner":   {"function", "agent"},
		"grant":   {"system"},
		"shared":  {"person", "app"},
	} {
		if got := ty.Props[name].Writers; !slices.Equal(got, want) {
			t.Errorf("%s: Writers = %q, want %q", name, got, want)
		}
	}
	for name, cases := range map[string]map[string]bool{
		"label":   {"person": true, "app": true, "package": true},
		"cadence": {"person": true, "app": false, "function": false},
		"cursor":  {"function": true, "agent": true, "person": false, "package": false},
		"grant":   {"system": true, "person": false},
	} {
		for typ, want := range cases {
			if got := ty.Props[name].WrittenBy(typ); got != want {
				t.Errorf("%s written by %s = %v, want %v", name, typ, got, want)
			}
		}
	}
	// The writer rides into the stored definition on the object and the
	// reference the way it does on a scalar, as written: the console reads
	// it from there.
	props, _ := ty.Definition["properties"].(map[string]any)
	for _, name := range []string{"resume", "owner"} {
		decl, _ := props[name].(map[string]any)
		if list, _ := decl["writer"].([]any); len(list) != 2 || list[0] != "function" || list[1] != "agent" {
			t.Errorf("%s: stored declaration = %v, want writer: [function, agent]", name, decl)
		}
	}
	if decl, _ := props["grant"].(map[string]any); decl["writer"] != "system" {
		t.Errorf("grant: stored declaration = %v, want writer: system", decl)
	}
}

// Each word from before actor types is refused naming the one that replaced
// it, on every shape, as the envelope refuses a retired key naming the key
// that took its job.
func TestARetiredWriterIsRefusedNamingItsReplacement(t *testing.T) {
	for old, instead := range map[string]string{
		"owner":     "write person",
		"connector": "write [function, agent]",
		"oauth":     "write system",
	} {
		loadThingErr(t, `  properties:
    cursor: {type: string, writer: `+old+`}
`, `writer "`+old+`" is retired: `+instead)
		loadThingErr(t, `  properties:
    resume:
      type: object
      writer: `+old+`
      fields:
        pageToken: {type: string}
`, `writer "`+old+`" is retired: `+instead)
		loadThingErr(t, `  properties:
    owner: {type: reference, kind: any, writer: [person, `+old+`]}
`, `writer "`+old+`" is retired: `+instead)
	}
}

func TestWriterRefusals(t *testing.T) {
	loadThingErr(t, `  properties:
    resume:
      type: object
      writer: [function, agent]
      fields:
        pageToken: {type: string, writer: person}
`, "properties.resume.fields.pageToken: a field takes no `writer:` — the property's `writer:` guards its whole value")
	loadThingErr(t, `  properties:
    resume:
      type: object
      writer: janitor
      fields:
        pageToken: {type: string}
`, `writer "janitor" is not a type of actor`)
	loadThingErr(t, `  properties:
    owner: {type: reference, kind: any, writer: janitor}
`, `writer "janitor" is not a type of actor`)
	loadThingErr(t, `  properties:
    cursor: {type: string, writer: []}
`, `an empty list admits no writer`)
	loadThingErr(t, `  properties:
    cursor: {type: string, writer: [person, person]}
`, `writer names "person" twice`)
	loadThingErr(t, `  properties:
    cursor: {type: string, writer: 3}
`, `writer: a type of actor or a list of them`)
	// A machine has one key set of its own, and a writer is not in it.
	loadThingErr(t, `  properties:
    phase:
      type: state
      writer: [function, agent]
      states: [open, done]
      initial: open
`, `unknown key "writer"`)
}
