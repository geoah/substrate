package commands

import (
	"strings"
	"testing"
)

// Every spelling the envelope replaced is refused BY NAME, because the fix is
// a rename and the message is the rename. A document written in the old
// envelope carries several of them at once, so the checks run before the
// envelope's own keys are looked for: being told what `kind` is now beats
// being told the document has no `kind`.
func TestApplyNamesTheReplacementForEveryRenamedKey(t *testing.T) {
	for _, tc := range []struct{ name, doc, want string }{
		{
			"apiVersion",
			"apiVersion: tasks.substrate.reamde.dev/v1alpha1\nkind: task\nmetadata: {name: t1}\nspec: {}\n",
			"writes `apiVersion`, which is gone",
		},
		{
			"group",
			"group: core.substrate.reamde.dev\nkind: tasks.substrate.reamde.dev/task\nmetadata: {id: t1}\ndata: {}\n",
			"which are one key now: `kind`",
		},
		{
			"type",
			"type: task\nmetadata: {id: t1}\ndata: {}\n",
			"which are one key now: `kind`",
		},
		{
			"spec",
			"kind: tasks.substrate.reamde.dev/task\nmetadata: {id: t1}\nspec: {}\n",
			"writes `spec`, which is `data`",
		},
		{
			"metadata.name",
			"kind: tasks.substrate.reamde.dev/task\nmetadata: {name: t1}\ndata: {}\n",
			"writes `metadata.name`, which is `metadata.id`",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.writeConfig()
			h.stdin.WriteString(tc.doc)
			_, _, err := h.run("apply", "-f", "-")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to name %s", err, tc.want)
			}
			if len(h.fake.requests) > 0 {
				t.Fatalf("nothing may be written, saw %v", h.fake.requests)
			}
		})
	}
}

// The envelope carries ONE kind reference: the split `group:`/`type:` pair is
// the pre-envelope document's shape and is named as such.
func TestApplyKeepsTheAuthorityInTheKind(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.stdin.WriteString("group: core.substrate.reamde.dev\ntype: task\nmetadata: {id: t1}\ndata: {}\n")
	_, _, err := h.run("apply", "-f", "-")
	if err == nil || !strings.Contains(err.Error(), "which are one key now: `kind`") {
		t.Fatalf("err = %v, want the one-key error", err)
	}
}

// A top-level `id:` under the envelope is two answers to one question, and the
// one that gets dropped silently is the id someone meant to keep.
func TestApplyRefusesAFlatIDUnderTheEnvelope(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.stdin.WriteString("kind: tasks.substrate.reamde.dev/task\nid: t1\nmetadata: {id: t2}\ndata: {}\n")
	_, _, err := h.run("apply", "-f", "-")
	if err == nil || !strings.Contains(err.Error(), "mixes the envelope with the pre-envelope key `id`") {
		t.Fatalf("err = %v", err)
	}
	if len(h.fake.requests) > 0 {
		t.Fatalf("nothing may be written, saw %v", h.fake.requests)
	}
}

// `type` is the singular name; a plural resolves to a dedicated error rather
// than becoming a second lookup namespace.
func TestApplyRefusesThePluralAsType(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	h.stdin.WriteString("kind: tasks.substrate.reamde.dev/tasks\nmetadata: {id: t1}\ndata: {}\n")
	_, _, err := h.run("apply", "-f", "-")
	if err == nil || !strings.Contains(err.Error(), "singular") {
		t.Fatalf("err = %v, want the singular-name error", err)
	}
}
