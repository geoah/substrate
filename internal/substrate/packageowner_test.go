package substrate

import "testing"

// A takeover reads the app off a package row: the hand its `declaredBy` links,
// named by the client id where an earlier binary stamped one and by the app's
// publisher otherwise, and no app at all for a package the person, an agent
// or the substrate declared.
func TestAPackageOwnerIsReadOffItsRow(t *testing.T) {
	const client = "https://cdn.example.net/workouts/client.json"
	hands := map[string]Actor{
		"a-app":    AppActor("cdn.example.net"),
		"a-person": ActorPerson,
		"a-agent":  AgentActor("ada.example.com", "llm", "notekeeper"),
	}
	keyOf := func(id string) Actor { return hands[id] }
	link := func(id string) map[string]any {
		return map[string]any{"ref": "substrate.reamde.dev/core/actor/" + id}
	}
	for _, c := range []struct {
		name  string
		props map[string]any
		app   bool
		words string
	}{
		{"stamped with its client id", map[string]any{PackageDeclaredBy: link("a-app"), PackageDeclaredByClient: client}, true, client},
		{"stamped with its host alone", map[string]any{PackageDeclaredBy: link("a-app")}, true, "cdn.example.net"},
		{"the person's", map[string]any{PackageDeclaredBy: link("a-person"), PackageDeclaredByClient: client}, false, ""},
		{"an agent's", map[string]any{PackageDeclaredBy: link("a-agent")}, false, ""},
		{"unstamped", map[string]any{}, false, ""},
	} {
		owner := PackageOwnerOf(c.props, keyOf)
		if owner.App() != c.app || owner.String() != c.words {
			t.Errorf("%s: owner = %+v (app %v), want app %v named %q", c.name, owner, owner.App(), c.app, c.words)
		}
	}
}
