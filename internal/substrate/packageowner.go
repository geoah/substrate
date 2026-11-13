package substrate

import "strings"

// The three managed properties of a package row that say who declared it
// (decisions 0109 and 0205, the second retired by 0214). The engine stamps
// `declaredBy` and no document may write any of them.
const (
	// PackageDeclaredBy links the actor record that first declared the
	// package: an app's, the person's, the system's, an agent's or a
	// function's (decision 0270). It stays readable, and nothing keys
	// ownership on it.
	PackageDeclaredBy = "declaredBy"
	// PackageDeclaredByClient was the full client id of the app that
	// declared the package under its `declare:` scope. The scope is retired
	// (decision 0214): the property is deprecated, no server path writes it,
	// and a takeover clears it from a row that still carries it.
	PackageDeclaredByClient = "declaredByClient"
	// PackageTakenOverFrom names the app a takeover took the package from:
	// its client id where one was stamped, its publisher otherwise.
	PackageTakenOverFrom = "takenOverFrom"
)

// PackageOwner is the app that declared a package, read off its row. The zero
// value is no app: a package the owner, an agent, a function or the substrate
// declared, or one taken over. A takeover (decision 0205's way back, which
// outlives its lock) is what still reads it.
type PackageOwner struct {
	// Actor is the hand the row's `declaredBy` links, kept only where it is
	// an app's.
	Actor Actor
	// ClientID is the row's `declaredByClient`, where an earlier binary
	// stamped one.
	ClientID string
}

// PackageOwnerOf reads the owner off a package row's properties. keyOf is
// the hand an actor record's id links, which only the repository can say.
func PackageOwnerOf(props map[string]any, keyOf func(id string) Actor) PackageOwner {
	ref, _ := props[PackageDeclaredBy].(map[string]any)
	path, _ := ref["ref"].(string)
	id := path[strings.LastIndex(path, "/")+1:]
	if id == "" {
		return PackageOwner{}
	}
	by := keyOf(id)
	if !IsAppActor(by) {
		return PackageOwner{}
	}
	client, _ := props[PackageDeclaredByClient].(string)
	return PackageOwner{Actor: by, ClientID: client}
}

// App reports whether an app declared the package.
func (o PackageOwner) App() bool { return IsAppActor(o.Actor) }

// String names the owner for a person: the client id where it is stamped,
// the app's publisher otherwise.
func (o PackageOwner) String() string {
	if o.ClientID != "" {
		return o.ClientID
	}
	return strings.TrimPrefix(string(o.Actor), AppActorPrefix)
}
