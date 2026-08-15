package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const baseBundle = `kind: core.substrate.reamde.dev/authority
metadata: {id: t.example.com}
data: {version: 1}
---
kind: core.substrate.reamde.dev/bundle
metadata: {id: t.example.com/t}
data:
  authority: t.example.com
  installs: [t.example.com/thing, t.example.com/other]
`

const baseThing = `kind: core.substrate.reamde.dev/kind
metadata: {id: t.example.com/thing}
data:
  authority: t.example.com
  names: {singular: thing, plural: things}
  properties:
    label: {type: string}
`

const baseOther = `kind: core.substrate.reamde.dev/kind
metadata: {id: t.example.com/other}
data:
  authority: t.example.com
  names: {singular: other, plural: others}
`

func writeTree(t *testing.T, files map[string]string) *tree {
	t.Helper()
	root := t.TempDir()
	for rel, doc := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tr, err := loadTree(root)
	if err != nil {
		t.Fatalf("load tree: %v", err)
	}
	return tr
}

func baseFiles() map[string]string {
	return map[string]string{
		"t.example.com/bundle.yaml": baseBundle,
		"t.example.com/thing.yaml":  baseThing,
		"t.example.com/other.yaml":  baseOther,
	}
}

func TestUnchangedTreePasses(t *testing.T) {
	if got := diffTrees(writeTree(t, baseFiles()), writeTree(t, baseFiles())); len(got) != 0 {
		t.Fatalf("an identical tree violates: %v", got)
	}
}

func TestChangedKindNeedsABump(t *testing.T) {
	head := baseFiles()
	head["t.example.com/thing.yaml"] = strings.Replace(baseThing,
		"    label: {type: string}", "    label: {type: string}\n    extra: {type: string}", 1)
	got := diffTrees(writeTree(t, baseFiles()), writeTree(t, head))
	if len(got) != 1 || !strings.Contains(got[0], "t.example.com/thing") {
		t.Fatalf("a changed kind under an unmoved version passes: %v", got)
	}

	// The kind's own version bump admits the same change.
	head["t.example.com/thing.yaml"] = strings.Replace(head["t.example.com/thing.yaml"],
		"  authority: t.example.com", "  authority: t.example.com\n  version: 2", 1)
	if got := diffTrees(writeTree(t, baseFiles()), writeTree(t, head)); len(got) != 0 {
		t.Fatalf("a bumped kind still violates: %v", got)
	}
}

func TestAuthorityBumpCoversItsDeclarations(t *testing.T) {
	head := baseFiles()
	head["t.example.com/thing.yaml"] = strings.Replace(baseThing,
		"    label: {type: string}", "    label: {type: string}\n    extra: {type: string}", 1)
	head["t.example.com/bundle.yaml"] = strings.Replace(baseBundle, "version: 1", "version: 2", 1)
	if got := diffTrees(writeTree(t, baseFiles()), writeTree(t, head)); len(got) != 0 {
		t.Fatalf("an authority bump does not cover its kinds: %v", got)
	}
}

func TestRemovedDeclarationNeedsAnAuthorityBump(t *testing.T) {
	head := baseFiles()
	delete(head, "t.example.com/other.yaml")
	got := diffTrees(writeTree(t, baseFiles()), writeTree(t, head))
	if len(got) != 1 || !strings.Contains(got[0], "t.example.com/other") {
		t.Fatalf("a pruned declaration under an unmoved authority passes: %v", got)
	}

	head["t.example.com/bundle.yaml"] = strings.Replace(baseBundle, "version: 1", "version: 2", 1)
	if got := diffTrees(writeTree(t, baseFiles()), writeTree(t, head)); len(got) != 0 {
		t.Fatalf("a pruned declaration under a bumped authority violates: %v", got)
	}
}

const freshKind = `kind: core.substrate.reamde.dev/kind
metadata: {id: t.example.com/fresh}
data:
  authority: t.example.com
  names: {singular: fresh, plural: freshes}
`

func TestAddedDeclarationNeedsNoBump(t *testing.T) {
	// DELIBERATE, and the asymmetry with removal and with wiring is the whole
	// point — this passes free while TestRemovedDeclarationNeedsAnAuthorityBump
	// and TestAddedAndRemovedWiringCountToo both demand a bump.
	//
	// The rule those two enforce is "no repository is ever offered the change".
	// It does not apply here: both upgrade paths carry an added declaration on
	// its ABSENCE, not on a version. seed.go's boot upgrade writes the
	// authority when any declaration `!exists`, and PlanBundleUpgrade lists the
	// same declaration as a change with no `from` and sets Available. Delivery
	// wiring has no such arm, which is exactly why it needs the bump and this
	// does not.
	//
	// The console renders the resulting version-less move on purpose too
	// (upgradeMotion in web/console/src/lib/bundles.ts). Demanding a bump here
	// would be a rule the machinery does not need, so what this pins is that
	// the diff stays quiet — the check has been read as a bug twice.
	head := baseFiles()
	head["t.example.com/fresh.yaml"] = freshKind
	if got := diffTrees(writeTree(t, baseFiles()), writeTree(t, head)); len(got) != 0 {
		t.Fatalf("an added declaration demands a bump it does not need: %v", got)
	}
}

func TestAddedDeclarationInInstallsStillNeedsTheBundleBump(t *testing.T) {
	// The other half of the pair: listing the new kind in `installs` CHANGES
	// the bundle declaration, and that is an ordinary changed declaration. So
	// an addition that touches the bundle document is caught after all — by
	// the rule that was always there, naming the document that actually
	// changed.
	head := baseFiles()
	head["t.example.com/fresh.yaml"] = freshKind
	head["t.example.com/bundle.yaml"] = strings.Replace(baseBundle,
		"installs: [t.example.com/thing, t.example.com/other]",
		"installs: [t.example.com/thing, t.example.com/other, t.example.com/fresh]", 1)
	got := diffTrees(writeTree(t, baseFiles()), writeTree(t, head))
	if len(got) != 1 || !strings.Contains(got[0], "bundle t.example.com/t") {
		t.Fatalf("a changed installs list under an unmoved version passes: %v", got)
	}
}

func TestRemovedBundleDirectoryPasses(t *testing.T) {
	if got := diffTrees(writeTree(t, baseFiles()), writeTree(t, map[string]string{})); len(got) != 0 {
		t.Fatalf("a bundle leaving the tree whole violates: %v", got)
	}
}

const baseTriggers = `kind: core.substrate.reamde.dev/trigger
metadata: {id: t.example.com/onthing}
data:
  properties:
    callable: {kind: core.substrate.reamde.dev/function, id: t.example.com/f}
    kinds: [t.example.com/thing]
`

func TestChangedDeliveryWiringNeedsAnAuthorityBump(t *testing.T) {
	base := baseFiles()
	base["t.example.com/triggers.yaml"] = baseTriggers
	head := baseFiles()
	head["t.example.com/triggers.yaml"] = strings.Replace(baseTriggers,
		"    kinds: [t.example.com/thing]", "    kinds: [t.example.com/other]", 1)
	got := diffTrees(writeTree(t, base), writeTree(t, head))
	if len(got) != 1 || !strings.Contains(got[0], "delivery wiring") {
		t.Fatalf("a changed trigger under an unmoved authority passes: %v", got)
	}

	head["t.example.com/bundle.yaml"] = strings.Replace(baseBundle, "version: 1", "version: 2", 1)
	if got := diffTrees(writeTree(t, base), writeTree(t, head)); len(got) != 0 {
		t.Fatalf("a bumped authority does not cover its wiring: %v", got)
	}
}

func TestAddedAndRemovedWiringCountToo(t *testing.T) {
	base := baseFiles()
	head := baseFiles()
	head["t.example.com/triggers.yaml"] = baseTriggers
	if got := diffTrees(writeTree(t, base), writeTree(t, head)); len(got) != 1 {
		t.Fatalf("added wiring under an unmoved authority passes: %v", got)
	}
	if got := diffTrees(writeTree(t, head), writeTree(t, base)); len(got) != 1 {
		t.Fatalf("removed wiring under an unmoved authority passes: %v", got)
	}
}

func TestUnchangedWiringPasses(t *testing.T) {
	files := baseFiles()
	files["t.example.com/triggers.yaml"] = baseTriggers
	if got := diffTrees(writeTree(t, files), writeTree(t, files)); len(got) != 0 {
		t.Fatalf("unchanged wiring violates: %v", got)
	}
}

func TestVersionNeverMovesBackward(t *testing.T) {
	head := baseFiles()
	head["t.example.com/bundle.yaml"] = strings.Replace(baseBundle, "version: 1", "version: 2", 1)
	got := diffTrees(writeTree(t, head), writeTree(t, baseFiles()))
	if len(got) == 0 {
		t.Fatal("a downgraded tree passes")
	}
	for _, v := range got {
		if !strings.Contains(v, "backward") {
			t.Fatalf("the violation does not say backward: %v", got)
		}
	}
}
