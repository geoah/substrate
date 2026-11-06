package substrate_test

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// A machine hand carries the FULL AUTHORITY AND THE PACKAGE (records 0025 and
// 0047). Two authorities sharing a first label, two packages under one
// authority, and two packages declaring a callable of one name are all
// different writers, which is what attribution, the manager ledger and the
// trigger self-exclusion key on.
func TestMachineActorsCarryTheFullAuthorityAndPackage(t *testing.T) {
	const (
		authority = "samples.substrate.reamde.dev"
		other     = "samples.bundles.substrate.reamde.dev"
	)

	if got := substrate.BundleActor(authority, "llm"); got != "bundle:samples.substrate.reamde.dev:llm" {
		t.Fatalf("bundle actor %q", got)
	}
	if substrate.BundleActor(authority, "llm") == substrate.BundleActor(other, "llm") {
		t.Fatal("two authorities sharing a first label share a bundle actor")
	}
	if substrate.BundleActor(authority, "llm") == substrate.BundleActor(authority, "web") {
		t.Fatal("two packages under one authority share a bundle actor")
	}
	if got := substrate.FunctionActor(authority, "llm", "summarize"); got != "function:samples.substrate.reamde.dev:llm:summarize" {
		t.Fatalf("function actor %q", got)
	}
	if substrate.FunctionActor(authority, "llm", "summarize") == substrate.FunctionActor(other, "llm", "summarize") {
		t.Fatal("two authorities declaring one function name share an actor")
	}
	if substrate.FunctionActor(authority, "llm", "summarize") == substrate.FunctionActor(authority, "web", "summarize") {
		t.Fatal("two packages declaring one function name share an actor")
	}
	if got := substrate.AgentActor(authority, "llm", "summarize"); got != "agent:samples.substrate.reamde.dev:llm:summarize" {
		t.Fatalf("agent actor %q", got)
	}
	// A function and an agent of one name in one package are two hands.
	if substrate.FunctionActor(authority, "llm", "summarize") == substrate.AgentActor(authority, "llm", "summarize") {
		t.Fatal("a function and an agent of one name share an actor")
	}
	// The separator is a colon: `<actor>/<name>` metadata keys reserve the
	// slash, so a derived actor may never contain one.
	for _, a := range []substrate.Actor{
		substrate.BundleActor(authority, "llm"),
		substrate.FunctionActor(authority, "llm", "summarize"),
		substrate.AgentActor(authority, "llm", "summarize"),
	} {
		if strings.Contains(string(a), "/") {
			t.Fatalf("actor %q carries a slash, which a metadata key namespace claims", a)
		}
	}
	// The seed is a flat word and an authority always carries a dot, so the
	// two can never be the same string.
	if substrate.ActorSeed == substrate.BundleActor(authority, "llm") {
		t.Fatal("an install collides with the seed's actor")
	}
}

// Every hand the substrate mints is refused at the door, and so is the retired
// `connector:` spelling: entries carrying it exist for good, and a request
// able to claim it could write what reads as a past sync's hand.
func TestReservedActorHoldsTheRetiredConnectorPrefix(t *testing.T) {
	for _, a := range []substrate.Actor{
		"connector:gmail", "connector:anything",
		substrate.BundleActor("samples.substrate.reamde.dev", "web"),
		substrate.FunctionActor("samples.substrate.reamde.dev", "web", "harvest"),
		substrate.AgentActor("samples.substrate.reamde.dev", "web", "harvest"),
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

// An app's actor is derived by the server from the token, so a request may
// never claim one; a name that merely resembles it is ordinary.
func TestAnAppActorIsReservedAndDerived(t *testing.T) {
	if a := substrate.AppActor("Notes.Example.org:8443"); a != "app:notes.example.org:8443" || !substrate.IsAppActor(a) {
		t.Fatalf("AppActor = %q", a)
	}
	if !substrate.ReservedActor("app:notes.example.org") {
		t.Fatal("app:notes.example.org is claimable")
	}
	for _, a := range []substrate.Actor{"app", "apps", "application:x", "app:"} {
		if substrate.IsAppActor(a) {
			t.Errorf("IsAppActor(%q) = true", a)
		}
	}
}

// An assistant at the MCP door holds the owner's hand under its own app
// actor (decision 0230); the mark never covers code it runs, which writes
// under a function's or an agent's actor, nor an app without the mark.
func TestAnAssistantHoldsTheOwnersHandOnlyAtTheDoor(t *testing.T) {
	marked := substrate.WithAssistant(context.Background())
	for _, c := range []struct {
		ctx   context.Context
		actor substrate.Actor
		want  bool
	}{
		{context.Background(), substrate.ActorConsole, true},
		{context.Background(), substrate.ActorAPI, true},
		{context.Background(), substrate.AppActorLocal, false},
		{marked, substrate.AppActorLocal, true},
		{marked, substrate.AppActor("claude.ai"), true},
		{marked, substrate.Actor("function:ada.example.com:notes:stats"), false},
		{marked, substrate.Actor("agent:ada.example.com:notes:scribe"), false},
		{marked, substrate.ActorSystem, false},
	} {
		if got := substrate.OwnersHand(c.ctx, c.actor); got != c.want {
			t.Errorf("substrate.OwnersHand(%v, %q) = %v, want %v", c.ctx == marked, c.actor, got, c.want)
		}
	}
}
