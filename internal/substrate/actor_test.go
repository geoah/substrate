package substrate_test

import (
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// A machine hand carries the FULL AUTHORITY (record 0020). Two authorities
// sharing a first label, and two authorities declaring a callable of one name,
// are different writers — which is what attribution, the manager ledger and
// the trigger self-exclusion all key on.
func TestMachineActorsCarryTheFullAuthority(t *testing.T) {
	one, two := "llm.examples.substrate.reamde.dev", "llm.bundles.substrate.reamde.dev"

	if got := substrate.BundleActor(one); got != "bundle:llm.examples.substrate.reamde.dev" {
		t.Fatalf("bundle actor %q", got)
	}
	if substrate.BundleActor(one) == substrate.BundleActor(two) {
		t.Fatal("two authorities sharing a first label share a bundle actor")
	}
	if got := substrate.FunctionActor(one, "summarize"); got != "function:llm.examples.substrate.reamde.dev:summarize" {
		t.Fatalf("function actor %q", got)
	}
	if substrate.FunctionActor(one, "summarize") == substrate.FunctionActor(two, "summarize") {
		t.Fatal("two authorities declaring one function name share an actor")
	}
	if got := substrate.AgentActor(one, "summarize"); got != "agent:llm.examples.substrate.reamde.dev:summarize" {
		t.Fatalf("agent actor %q", got)
	}
	// A function and an agent of one name under one authority are two hands.
	if substrate.FunctionActor(one, "summarize") == substrate.AgentActor(one, "summarize") {
		t.Fatal("a function and an agent of one name share an actor")
	}
	// The separator is a colon: `<actor>/<name>` metadata keys reserve the
	// slash, so a derived actor may never contain one.
	for _, a := range []substrate.Actor{
		substrate.BundleActor(one), substrate.FunctionActor(one, "summarize"), substrate.AgentActor(one, "summarize"),
	} {
		if strings.Contains(string(a), "/") {
			t.Fatalf("actor %q carries a slash, which a metadata key namespace claims", a)
		}
	}
	// The seed is a flat word and an authority always carries a dot, so the
	// two can never be the same string.
	if substrate.ActorSeed == substrate.BundleActor(one) {
		t.Fatal("an install collides with the seed's actor")
	}
}

// Every hand the substrate mints is refused at the door, and so is the retired
// `connector:` spelling: entries carrying it exist for good, and a request
// able to claim it could write what reads as a past sync's hand.
func TestReservedActorHoldsTheRetiredConnectorPrefix(t *testing.T) {
	for _, a := range []substrate.Actor{
		"connector:gmail", "connector:anything",
		substrate.BundleActor("web.bundles.substrate.reamde.dev"),
		substrate.FunctionActor("web.bundles.substrate.reamde.dev", "harvest"),
		substrate.AgentActor("web.bundles.substrate.reamde.dev", "harvest"),
		substrate.ActorSeed, substrate.ActorSystem, "substrate.oauth",
	} {
		if !substrate.ReservedActor(a) {
			t.Errorf("actor %q is claimable by a request", a)
		}
	}
	// A name that merely resembles one is ordinary, and so are the doors.
	for _, a := range []substrate.Actor{
		"connectorish", "substrateish.example.com", "functions",
		substrate.ActorAPI, substrate.ActorConsole, substrate.ActorCLI,
	} {
		if substrate.ReservedActor(a) {
			t.Errorf("actor %q is refused at the door but names no writing hand", a)
		}
	}
}
