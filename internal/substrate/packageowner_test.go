package substrate

import "testing"

// A takeover reads the app off a package row: the client id where an earlier
// binary stamped one, the actor otherwise, and no app at all for a package a
// door, an agent or the substrate declared.
func TestAPackageOwnerIsReadOffItsRow(t *testing.T) {
	const client = "https://cdn.example.net/workouts/client.json"
	for _, c := range []struct {
		name  string
		props map[string]any
		app   bool
		words string
	}{
		{"stamped with its client id", map[string]any{PackageDeclaredBy: "app:cdn.example.net", PackageDeclaredByClient: client}, true, client},
		{"stamped with its host alone", map[string]any{PackageDeclaredBy: "app:cdn.example.net"}, true, "app:cdn.example.net"},
		{"a door's", map[string]any{PackageDeclaredBy: string(ActorConsole), PackageDeclaredByClient: client}, false, ""},
		{"an agent's", map[string]any{PackageDeclaredBy: "agent:ada.example.com:llm:notekeeper"}, false, ""},
		{"unstamped", map[string]any{}, false, ""},
	} {
		owner := PackageOwnerOf(c.props)
		if owner.App() != c.app || owner.String() != c.words {
			t.Errorf("%s: owner = %+v (app %v), want app %v named %q", c.name, owner, owner.App(), c.app, c.words)
		}
	}
}
