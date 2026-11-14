package engine

import (
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// A failed read of the actor kind is an error, never `admin`: the person of a
// repository from before actors is `admin` only where its declared-name actor
// kind is read as such, and an actor kind of today's shape reads the roles.
func TestAnActorKindThatDoesNotResolveGivesNobodyAdmin(t *testing.T) {
	t.Parallel()
	if _, before, err := preActorRoles(nil, errors.New("the registry did not load"), []string{"a.example.com"}); err == nil || before {
		t.Fatalf("an actor kind that does not resolve: before %v, %v; want an error", before, err)
	}
	old := &vocabulary.Kind{Identity: kindActor, Props: map[string]*vocabulary.Property{"name": {}}}
	if roles, before, err := preActorRoles(old, nil, []string{"a.example.com"}); err != nil || !before || !roles.Admin() {
		t.Fatalf("the declared-name actor kind: before %v, admin %v, %v; want admin", before, roles.Admin(), err)
	}
	now := &vocabulary.Kind{Identity: kindActor, Props: map[string]*vocabulary.Property{"type": {}}}
	if _, before, err := preActorRoles(now, nil, []string{"a.example.com"}); err != nil || before {
		t.Fatalf("today's actor kind: before %v, %v; want the roles read", before, err)
	}
}
