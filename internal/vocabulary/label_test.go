package vocabulary_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/vocabulary"
)

func labelFixture(label string) map[string]string {
	return map[string]string{"d.example.com/d/authority.yaml": `kind: substrate.reamde.dev/core/package
metadata:
  id: d.example.com/d
data:
  authority: d.example.com
  package: d
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: d.example.com/d/conversation
data:
  authority: d.example.com
  package: d
  names:
    singular: conversation
` + label + `  properties:
    name:
      type: string
`}
}

// A kind declares its display label, both forms, and the parsed kind carries
// it structurally (decision 0106). A kind that declares none has the zero
// label, and a client humanizes the name.
func TestKindLabel(t *testing.T) {
	r := loadFixture(t, labelFixture("  label:\n    singular: Channel\n    plural: Channels\n"))
	k, ok := r.ByIdentity("d.example.com/d/conversation")
	if !ok {
		t.Fatal("kind missing")
	}
	if k.Label != (vocabulary.KindLabel{Singular: "Channel", Plural: "Channels"}) {
		t.Errorf("label = %+v", k.Label)
	}
	// A mass noun declares the same word twice; nothing pluralizes it.
	r = loadFixture(t, labelFixture("  label:\n    singular: Sync progress\n    plural: Sync progress\n"))
	if k, _ := r.ByIdentity("d.example.com/d/conversation"); k.Label.Plural != "Sync progress" {
		t.Errorf("mass-noun label = %+v", k.Label)
	}

	r = loadFixture(t, labelFixture(""))
	if k, _ := r.ByIdentity("d.example.com/d/conversation"); !k.Label.Empty() {
		t.Errorf("undeclared label = %+v, want the zero label", k.Label)
	}
}

func TestKindLabelRefusals(t *testing.T) {
	long := strings.Repeat("x", 81)
	for what, tc := range map[string]struct{ label, want string }{
		"a bare string":        {"  label: Channel\n", "data.label: must be a mapping"},
		"no plural":            {"  label:\n    singular: Channel\n", "data.label.plural is required"},
		"no singular":          {"  label:\n    plural: Channels\n", "data.label.singular is required"},
		"an empty form":        {"  label:\n    singular: \"\"\n    plural: Channels\n", "data.label.singular is required"},
		"an unknown key":       {"  label:\n    singular: Channel\n    plural: Channels\n    short: Ch\n", `data.label: unknown key "short"`},
		"a newline":            {"  label:\n    singular: \"Chan\\nnel\"\n    plural: Channels\n", "data.label.singular: a short single-line caption"},
		"padding":              {"  label:\n    singular: \" Channel\"\n    plural: Channels\n", "data.label.singular: no leading or trailing whitespace"},
		"over the caption cap": {"  label:\n    singular: Channel\n    plural: " + long + "\n", "data.label.plural: a short caption (at most 80 chars), got 81"},
	} {
		fsys := fstest.MapFS{}
		for name, body := range labelFixture(tc.label) {
			fsys[name] = &fstest.MapFile{Data: []byte(body)}
		}
		_, err := vocabulary.LoadFS(fsys)
		if err == nil {
			t.Errorf("%s: label loaded", what)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error = %v, want it to name %q", what, err, tc.want)
		}
	}
}
