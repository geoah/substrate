package engine

import (
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// An app's keys are its actor's namespace and, beside it, `app:<client id
// host>`, the namespace its token had before decision 0209; nobody else's.
func TestAnAppsKeyNamespaceIsItsActorOrItsClientIDsHost(t *testing.T) {
	const loopback, cdn = "http://127.0.0.1:43123/cli", "https://cdn.example.net/workouts/client.json"
	for _, c := range []struct {
		actor    substrate.Actor
		clientID string
		key      string
		ok       bool
	}{
		{substrate.AppActorLocal, loopback, "app:local/seen", true},
		{substrate.AppActorLocal, loopback, "app:127.0.0.1:43123/seen", true},
		{substrate.AppActorLocal, "http://127.0.0.1:50000/cli", "app:127.0.0.1:43123/seen", false},
		{substrate.AppActor("example.org"), cdn, "app:cdn.example.net/seen", true},
		{substrate.AppActor("example.org"), cdn, "app:example.org/seen", true},
		{substrate.AppActor("example.org"), cdn, "app:notes.example.org/seen", false},
		{substrate.AppActor("example.org"), "", "app:cdn.example.net/seen", false},
		{"agent:example.org:coach:coach", cdn, "app:cdn.example.net/seen", false},
	} {
		err := metaKeyAllowed(c.actor, c.clientID, c.key)
		if c.ok && err != nil || !c.ok && !errors.Is(err, substrate.ErrForbidden) {
			t.Errorf("%s (%s) writes %s: %v, want allowed %v", c.actor, c.clientID, c.key, err, c.ok)
		}
	}
}
