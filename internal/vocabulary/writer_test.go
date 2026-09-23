package vocabulary_test

// The `writer:` role on every property shape. A connector's resume cursor is
// an object and a connection's identity (`account.user`, `account.address`)
// is a reference, and both are the connector's hand exactly as a scalar cursor
// is. The console offers every property WITHOUT a non-owner role for editing,
// so a role the loader dropped on those two shapes was the Add account form
// asking a person to type a page token and pick their own identity out of an
// empty list. Parsed on the scalar, object and reference branches alike;
// refused when it names no role; a state machine's key set has no such key.

import (
	"testing"
)

func TestWriterRoleOnEveryShape(t *testing.T) {
	ty := loadThing(t, `  properties:
    label: {type: string}
    cadence: {type: string, writer: owner}
    cursor: {type: string, writer: connector}
    resume:
      type: object
      writer: connector
      fields:
        pageToken: {type: string}
    owner:
      type: reference
      kind: any
      writer: connector
    grant:
      type: reference
      kind: any
      writer: oauth
`)
	for name, want := range map[string]string{
		"label":   "",
		"cadence": "owner",
		"cursor":  "connector",
		"resume":  "connector",
		"owner":   "connector",
		"grant":   "oauth",
	} {
		if got := ty.Props[name].Writer; got != want {
			t.Errorf("%s: Writer = %q, want %q", name, got, want)
		}
	}
	// The role rides into the stored definition on the object and the
	// reference the way it does on a scalar: the console reads it from there.
	props, _ := ty.Definition["properties"].(map[string]any)
	for _, name := range []string{"resume", "owner"} {
		decl, _ := props[name].(map[string]any)
		if decl["writer"] != "connector" {
			t.Errorf("%s: stored declaration = %v, want writer: connector", name, decl)
		}
	}
}

func TestWriterRoleRefusals(t *testing.T) {
	loadThingErr(t, `  properties:
    resume:
      type: object
      writer: janitor
      fields:
        pageToken: {type: string}
`, `writer "janitor" is not a role`)
	loadThingErr(t, `  properties:
    owner: {type: reference, kind: any, writer: janitor}
`, `writer "janitor" is not a role`)
	loadThingErr(t, `  properties:
    cursor: {type: string, writer: janitor}
`, `writer "janitor" is not a role`)
	// A machine has one key set of its own, and a role is not in it.
	loadThingErr(t, `  properties:
    phase:
      type: state
      writer: connector
      states: [open, done]
      initial: open
`, `unknown key "writer"`)
}
