package vocabulary_test

// The GRAMMAR half of `movedFrom` (decision record 0078). What the loader can
// decide alone is decided here; whether the move is compatible with the
// declaration the repository HOLDS is the engine's, and MovedFromProblems is
// the seam between the two.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/vocabulary"
)

// movedKindDoc is one package holding one kind that says where it came from.
func movedKindDoc(movedFrom string) map[string]string {
	return map[string]string{
		"ada.example.com/crew/crew.yaml": `kind: substrate.reamde.dev/core/package
metadata:
  id: ada.example.com/crew
data:
  authority: ada.example.com
  package: crew
  version: 1
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: ada.example.com/crew/widget
data:
  authority: ada.example.com
  package: crew
  names:
    singular: widget
  movedFrom: ` + movedFrom + `
  properties:
    name:
      type: string
`,
	}
}

func TestMovedFromRefusesWhatCouldNeverBeMoved(t *testing.T) {
	for name, tc := range map[string]struct{ movedFrom, says string }{
		"a bare name":           {"widget", "fully qualified kind reference"},
		"another authority":     {"other.example.com/crew/widget", "under another authority"},
		"the kind itself":       {"ada.example.com/crew/widget", "names this kind itself"},
		"a package, not a kind": {"ada.example.com/crew", "fully qualified kind reference"},
	} {
		t.Run(name, func(t *testing.T) {
			problems := loadProblems(t, movedKindDoc(tc.movedFrom))
			if !strings.Contains(problems, tc.says) {
				t.Fatalf("the refusal does not say why: %s", problems)
			}
		})
	}
}

// A `movedFrom` naming a kind the tree does not ship is ADMITTED: the loader
// never sees the stored side, and the whole point of the key is to name a kind
// this binary no longer declares. It does nothing on a repository that never
// had it.
func TestMovedFromAdmitsAKindThisTreeNoLongerShips(t *testing.T) {
	fsys := fstest.MapFS{}
	for name, body := range movedKindDoc("ada.example.com/old/widget") {
		fsys[name] = &fstest.MapFile{Data: []byte(body)}
	}
	reg, err := vocabulary.LoadFS(fsys)
	if err != nil {
		t.Fatalf("a movedFrom naming an absent kind must load: %v", err)
	}
	ty, ok := reg.ByIdentity("ada.example.com/crew/widget")
	if !ok {
		t.Fatal("the kind did not load")
	}
	if ty.MovedFrom != "ada.example.com/old/widget" {
		t.Fatalf("movedFrom = %q, want it stored as written", ty.MovedFrom)
	}
}

// The compatibility half: the two property sets must be IDENTICAL. The rows
// travel with their properties untouched and under their own names, so a
// property the old kind declares and the new one does not is a value dropped by
// a key nobody wrote; a property the new kind declares and the old one does not
// is a shape the plan never said it was installing; and a rename is neither,
// because the move copies the old key. Move first, change after.
func TestMovedFromProblemsDemandIdenticalPropertySets(t *testing.T) {
	old := &vocabulary.Kind{
		Identity:  "ada.example.com/old/widget",
		PropOrder: []string{"kept", "gone", "renamed"},
		Props: map[string]*vocabulary.Property{
			"kept": {Name: "kept"}, "gone": {Name: "gone"}, "renamed": {Name: "renamed"},
		},
	}
	moved := &vocabulary.Kind{
		Identity:  "ada.example.com/crew/widget",
		MovedFrom: "ada.example.com/old/widget",
		PropOrder: []string{"kept", "renamedNow", "added"},
		Props: map[string]*vocabulary.Property{
			"kept":       {Name: "kept"},
			"renamedNow": {Name: "renamedNow", RenamedFrom: "renamed"},
			"added":      {Name: "added"},
		},
	}
	joined := strings.Join(vocabulary.MovedFromProblems(old, moved), " ")
	for _, want := range []string{`"gone"`, `"renamed"`, `"renamedNow"`, `"added"`} {
		if !strings.Contains(joined, want) {
			t.Errorf("problems do not name %s: %s", want, joined)
		}
	}
	// An identical pair is no problem at all.
	same := &vocabulary.Kind{
		Identity:  "ada.example.com/crew/widget",
		MovedFrom: "ada.example.com/old/widget",
		PropOrder: old.PropOrder,
		Props:     old.Props,
	}
	if got := vocabulary.MovedFromProblems(old, same); got != nil {
		t.Errorf("an identical property set is refused: %q", got)
	}
	// A repository that never held the old kind has nothing to move.
	if got := vocabulary.MovedFromProblems(nil, moved); got != nil {
		t.Fatalf("problems against no stored kind = %q, want none", got)
	}
}
