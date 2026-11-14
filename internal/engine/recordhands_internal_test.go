package engine

import (
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// Decision 0311: of the lines the engine writes acting for a hand, a purge
// alone is that hand's, to the rule that keeps a shipped record its owner's.
// Any actor other than the shipping hands is another hand, as before.
func TestAPurgeTheEngineWritesForAHandIsThatHands(t *testing.T) {
	t.Parallel()
	shipper := substrate.BundleActor(shipperAuthority, "shipper")
	shipping := shippedHands(shipper)
	system, person, bundle := actorID(substrate.ActorSystem), actorID(substrate.ActorPerson), actorID(shipper)
	for _, c := range []struct {
		name string
		hand recordHand
		want bool
	}{
		{"the engine's purge for a person", recordHand{system, person, string(substrate.OpGC)}, true},
		{"the engine's delete for a person", recordHand{system, person, string(substrate.OpDelete)}, false},
		{"the engine's patch for a person", recordHand{system, person, string(substrate.OpPatch)}, false},
		{"the engine's purge for no one", recordHand{system, "", string(substrate.OpGC)}, false},
		{"the engine's purge for the shipper", recordHand{system, bundle, string(substrate.OpGC)}, false},
		{"the shipper's purge for a person", recordHand{bundle, person, string(substrate.OpGC)}, false},
		{"the shipper's install", recordHand{bundle, "", string(substrate.OpPut)}, false},
		{"a person's edit", recordHand{person, "", string(substrate.OpPatch)}, true},
	} {
		if got := c.hand.another(shipping); got != c.want {
			t.Errorf("%s: another = %v, want %v", c.name, got, c.want)
		}
	}
}
