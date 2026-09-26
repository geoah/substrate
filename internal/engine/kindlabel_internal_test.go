package engine

import (
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The vocabulary read carries a kind's declared label as its own field, and
// omits it for a kind that declares none (decision 0106).
func TestTypeInfoCarriesTheDeclaredLabel(t *testing.T) {
	fsys := fstest.MapFS{"d.example.com/d/d.yaml": &fstest.MapFile{Data: []byte(`kind: substrate.reamde.dev/core/package
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
  label:
    singular: Channel
    plural: Channels
  properties:
    name:
      type: string
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: d.example.com/d/note
data:
  authority: d.example.com
  package: d
  names:
    singular: note
  properties:
    name:
      type: string
`)}}
	reg, err := vocabulary.LoadFS(fsys)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	k, _ := reg.ByIdentity("d.example.com/d/conversation")
	info := typeInfo(k)
	if info.Label == nil || *info.Label != (substrate.KindLabel{Singular: "Channel", Plural: "Channels"}) {
		t.Errorf("label = %+v", info.Label)
	}
	n, _ := reg.ByIdentity("d.example.com/d/note")
	if got := typeInfo(n).Label; got != nil {
		t.Errorf("undeclared label = %+v, want nil", got)
	}
}
