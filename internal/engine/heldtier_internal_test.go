package engine

import (
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// heldTierIn's branches (record 0106): only a row stored above the machine
// tier whose actor a declaration puts AT the machine tier moves, and it moves
// down. A door name, a package's own bundle hand and a promoted actor keep the
// tier the write stored.
func TestHeldTierIn(t *testing.T) {
	reg, err := vocabulary.LoadFS(fstest.MapFS{"held.yaml": {Data: []byte(`
kind: substrate.reamde.dev/core/package
metadata:
  id: held.test.dev/held
data:
  authority: held.test.dev
  package: held
  version: 1
---
kind: substrate.reamde.dev/core/actor
metadata:
  id: importer
data:
  authority: held.test.dev
  package: held
  tier: machine
---
kind: substrate.reamde.dev/core/actor
metadata:
  id: promoted
data:
  authority: held.test.dev
  package: held
  tier: owner
---
kind: substrate.reamde.dev/core/actor
metadata:
  id: bundle:held.test.dev:held
data:
  authority: held.test.dev
  package: held
`)}})
	if err != nil {
		t.Fatalf("load the declarations: %v", err)
	}
	if tier, ok := reg.ActorTier("bundle:held.test.dev:held"); !ok || tier != substrate.TierMachine {
		t.Fatalf("the bundle hand declares %q (%v), want machine", tier, ok)
	}
	for _, c := range []struct {
		name   string
		actor  string
		stored substrate.Tier
		want   substrate.Tier
	}{
		{"a machine actor's owner row is released", "importer", substrate.TierOwner, substrate.TierMachine},
		{"a machine actor's bundle row is released", "importer", substrate.TierBundle, substrate.TierMachine},
		{"a bundle hand keeps its dispatch stamp", "bundle:held.test.dev:held", substrate.TierBundle, substrate.TierBundle},
		{"promotion never pins a machine row", "promoted", substrate.TierMachine, substrate.TierMachine},
		{"a promoted actor's owner row stays", "promoted", substrate.TierOwner, substrate.TierOwner},
		{"a door name is never released", string(substrate.ActorAPI), substrate.TierOwner, substrate.TierOwner},
		{"an undeclared name keeps its row", "stranger", substrate.TierOwner, substrate.TierOwner},
	} {
		if got := heldTierIn(reg, c.actor, c.stored); got != c.want {
			t.Errorf("%s: heldTierIn(%s, %s) = %s, want %s", c.name, c.actor, c.stored, got, c.want)
		}
	}
}
