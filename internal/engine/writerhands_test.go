package engine

import (
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The hands that write as the system's type and are not the engine's own
// pass (decision 0290). A policy rule's acceptance carries a request's diff
// and accepts for the person whose rule it is, so it is held as the person
// is, as the owner tier held it before. The system, the OAuth facility and
// the webhook door decide their own values and write any property. A scoped
// token whose hand reads as the person passes no guarded property.
func TestAPolicysAcceptanceIsHeldAsThePersonAndTheSystemsOwnHandsWriteAny(t *testing.T) {
	ty := &vocabulary.Kind{Identity: "x.example.com/p/thing", Props: map[string]*vocabulary.Property{
		"verified": {Name: "verified", Writers: []string{substrate.ActorTypePerson}},
		"note":     {Name: "note", Writers: []string{substrate.ActorTypeFunction, substrate.ActorTypeAgent}},
		"status":   {Name: "status", Writers: []string{substrate.ActorTypeSystem}},
		"free":     {Name: "free"},
	}}
	check := func(tx *txn, name string) error { return tx.checkPropertyOwnership(ty, []string{name}) }

	policy := &txn{actor: substrate.Actor("policy:accept-small")}
	if err := check(policy, "verified"); err != nil {
		t.Errorf("a policy's acceptance of a person's property: %v", err)
	}
	for _, name := range []string{"note", "status"} {
		err := check(policy, name)
		if !errors.Is(err, substrate.ErrForbidden) || !strings.Contains(err.Error(), "a policy rule, which accepts for the person (person)") {
			t.Errorf("a policy's acceptance of %s = %v, want refused naming the policy", name, err)
		}
	}
	for _, hand := range []substrate.Actor{substrate.ActorSystem, actorOAuth, actorWebhook} {
		for _, name := range []string{"verified", "note", "status", "free"} {
			if err := check(&txn{actor: hand}, name); err != nil {
				t.Errorf("%s writing %s: %v", hand, name, err)
			}
		}
	}
	scoped := &txn{actor: substrate.ActorPerson, scoped: true}
	for _, name := range []string{"verified", "note", "status"} {
		if err := check(scoped, name); !errors.Is(err, substrate.ErrForbidden) {
			t.Errorf("a scoped token naming no app writing %s = %v, want refused", name, err)
		}
	}
	if err := check(scoped, "free"); err != nil {
		t.Errorf("a scoped token naming no app writing an unguarded property: %v", err)
	}
}
