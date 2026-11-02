package substrate

import "testing"

// Ownership is the client id (decision 0205), so an app whose actor moved
// from its host to its verified authority (decision 0209) keeps its package,
// and the host-only claim compares the client id's host, never the actor.
func TestAPackageOwnerIsTheClientIDWhateverActorItWritesAs(t *testing.T) {
	const client = "https://cdn.example.net/workouts/client.json"
	stamped := PackageOwner{Actor: AppActor("cdn.example.net"), ClientID: client}
	for _, c := range []struct {
		name      string
		owner     PackageOwner
		actor     Actor
		clientID  string
		ok, claim bool
	}{
		{"the same client under its verified authority", stamped, AppActor("example.org"), client, true, false},
		{"the same client as local", stamped, AppActorLocal, client, true, false},
		{"another client on the same host", stamped, AppActor("cdn.example.net"), "https://cdn.example.net/other/client.json", false, false},
		{"a door", stamped, ActorConsole, client, false, false},
		{"a host-only stamp, claimed by a client on that host under a new actor", PackageOwner{Actor: AppActor("cdn.example.net")}, AppActor("example.org"), client, true, true},
		{"a host-only stamp, not claimed by a client on another host whose actor matches", PackageOwner{Actor: AppActor("example.org")}, AppActor("example.org"), client, false, false},
		{"a host-only stamp, an engine caller with no client id", PackageOwner{Actor: AppActor("cdn.example.net")}, AppActor("cdn.example.net"), "", true, false},
		{"no app owns it", PackageOwner{}, AppActor("example.org"), client, false, false},
	} {
		ok, claim := c.owner.Admits(c.actor, c.clientID)
		if ok != c.ok || claim != c.claim {
			t.Errorf("%s: Admits = %v, %v, want %v, %v", c.name, ok, claim, c.ok, c.claim)
		}
	}
}
