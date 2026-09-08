package vocabulary_test

// `requiresAtLeast:` is the floor a bundle puts under a package it requires
// (decision record 0070): one key per required package, one integer each, and
// resolveBundle refuses the closure while the repository holds the package
// below it. The grammar is a minimum and nothing else.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	rqPeople = "ada.example.com/people"
	rqTasks  = "ada.example.com/tasks"
)

// rqPackage renders a one-kind closure at the given package version.
func rqPackage(pkg string, version int) string {
	return strings.Replace(bnPackageKind(pkg, "thing"+strings.Split(pkg, "/")[1]),
		"  version: 1\n", "  version: "+strconv.Itoa(version)+"\n", 1)
}

// rqRequiring renders the tasks closure with the given `requires:` and
// `requiresAtLeast:` blocks spliced into its bundle document.
func rqRequiring(requires, atLeast string) string {
	doc := bnPackageKind(rqTasks, "task")
	block := "  description: one kind, so the closure is whole\n"
	return strings.Replace(doc, block, block+requires+atLeast, 1)
}

func TestRequiresAtLeastIsSatisfiedAtOrAboveTheFloor(t *testing.T) {
	requires := "  requires:\n    - " + rqPeople + "\n"
	atLeast := "  requiresAtLeast:\n    " + rqPeople + ": 3\n"
	for _, held := range []int{3, 4} {
		r, err := loadBnPackages(rqPackage(rqPeople, held), rqRequiring(requires, atLeast))
		if err != nil {
			t.Fatalf("people at %d satisfies a floor of 3, yet the closure refused: %v", held, err)
		}
		g, ok := r.PackageByName(rqTasks)
		if !ok || g.Bundle == nil {
			t.Fatal("the tasks bundle did not load")
		}
		if got := g.Bundle.RequiresAtLeast[rqPeople]; got != 3 {
			t.Errorf("RequiresAtLeast[%s] = %d, want 3", rqPeople, got)
		}
	}
}

func TestRequiresAtLeastRefusesAPackageBelowTheFloor(t *testing.T) {
	requires := "  requires:\n    - " + rqPeople + "\n"
	atLeast := "  requiresAtLeast:\n    " + rqPeople + ": 3\n"
	_, err := loadBnPackages(rqPackage(rqPeople, 2), rqRequiring(requires, atLeast))
	if err == nil {
		t.Fatal("people at version 2 satisfied a floor of 3")
	}
	for _, want := range []string{"requiresAtLeast", rqPeople, "version 3 or later", "holds version 2"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, err)
		}
	}
}

// A closure with no floor is what it was before the key existed: the package's
// presence is the whole requirement.
func TestRequiresWithoutAFloorAcceptsAnyVersion(t *testing.T) {
	requires := "  requires:\n    - " + rqPeople + "\n"
	r, err := loadBnPackages(rqPackage(rqPeople, 1), rqRequiring(requires, ""))
	if err != nil {
		t.Fatalf("a bare requires refused: %v", err)
	}
	g, _ := r.PackageByName(rqTasks)
	if g.Bundle.RequiresAtLeast != nil {
		t.Errorf("a bundle with no requiresAtLeast carries a floor: %v", g.Bundle.RequiresAtLeast)
	}
}

// The grammar is a minimum, on a declared requirement, as an integer of at
// least one. Everything else is refused at the door, so no closure can carry a
// floor no binary honors (decision record 0020).
func TestRequiresAtLeastRefusesWhatItCannotHonor(t *testing.T) {
	requires := "  requires:\n    - " + rqPeople + "\n"
	cases := map[string]struct{ atLeast, want string }{
		"a package requires does not list": {
			"  requiresAtLeast:\n    other.example.com/things: 2\n",
			"data.requires does not list",
		},
		"a string": {
			"  requiresAtLeast:\n    " + rqPeople + ": v3\n",
			"is not a version",
		},
		"a fraction": {
			"  requiresAtLeast:\n    " + rqPeople + ": 2.5\n",
			"is not a version",
		},
		"zero": {
			"  requiresAtLeast:\n    " + rqPeople + ": 0\n",
			"below 1",
		},
		"a list": {
			"  requiresAtLeast:\n    - " + rqPeople + "\n",
			"must be a map",
		},
		"an empty map": {
			"  requiresAtLeast: {}\n",
			"present but empty",
		},
		"a ceiling": {
			"  requiresAtMost:\n    " + rqPeople + ": 9\n",
			"unknown key \"requiresAtMost\"",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := loadBnPackages(rqPackage(rqPeople, 5), rqRequiring(requires, tc.atLeast))
			if err == nil {
				t.Fatalf("%s was admitted", name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal does not say %q: %v", tc.want, err)
			}
		})
	}
}

// The floor is a document key like any other, so it survives the parse and a
// registry rebuild reads it back from the bundle's definition.
func TestRequiresAtLeastStaysOnTheDefinition(t *testing.T) {
	requires := "  requires:\n    - " + rqPeople + "\n"
	atLeast := "  requiresAtLeast:\n    " + rqPeople + ": 3\n"
	r, err := loadBnPackages(rqPackage(rqPeople, 3), rqRequiring(requires, atLeast))
	if err != nil {
		t.Fatal(err)
	}
	g, _ := r.PackageByName(rqTasks)
	def, _ := g.Bundle.Definition["requiresAtLeast"].(map[string]any)
	if v, _ := vocabulary.VersionValue(def[rqPeople]); v != 3 {
		t.Errorf("definition.requiresAtLeast = %v, want %s: 3", g.Bundle.Definition["requiresAtLeast"], rqPeople)
	}
}
