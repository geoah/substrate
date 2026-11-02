package substrate

// The three managed properties of a package row that say who keeps its
// declarations (decisions 0109 and 0205). The engine stamps them and no
// document may write them.
const (
	// PackageDeclaredBy is the actor that first declared the package:
	// `app:<host>` for an app, a door, the system, an agent or a function
	// otherwise. It stays readable, and names the host only.
	PackageDeclaredBy = "declaredBy"
	// PackageDeclaredByClient is the full client id of the app that declared
	// the package, beside its host-only actor: what ownership is compared on,
	// so two apps on one host, and two loopback builds, are two owners.
	PackageDeclaredByClient = "declaredByClient"
	// PackageTakenOverFrom names the app a takeover took the package from:
	// its client id where one was stamped, its actor otherwise.
	PackageTakenOverFrom = "takenOverFrom"
)

// PackageOwner is the app that keeps a package's declarations (decision
// 0205), read off its row. The zero value is no app: a package the owner, an
// agent, a function or the substrate declared, one taken over, or one created
// before the stamp.
type PackageOwner struct {
	// Actor is the row's `declaredBy`, kept only where it names an app.
	Actor Actor
	// ClientID is the row's `declaredByClient`: the client id the app was
	// approved under. Empty on a package an app declared before the client id
	// was stamped, whose first apply by an app on the same host claims it.
	ClientID string
}

// PackageOwnerOf reads the owner off a package row's properties.
func PackageOwnerOf(props map[string]any) PackageOwner {
	by, _ := props[PackageDeclaredBy].(string)
	if !IsAppActor(Actor(by)) {
		return PackageOwner{}
	}
	client, _ := props[PackageDeclaredByClient].(string)
	return PackageOwner{Actor: Actor(by), ClientID: client}
}

// App reports whether an app owns the package.
func (o PackageOwner) App() bool { return IsAppActor(o.Actor) }

// Admits reports whether the app writing as actor, under the client id
// clientID, is this owner. Where the row carries a client id the two must be
// equal, so a second app on the same host is not the owner. Where it carries
// only the host (an install from before the client id was stamped), the first
// app on that host to write claims it: claim is true, and the write stamps
// its client id, after which only that client id is admitted. An app actor
// with no client id at all (an engine caller, never a request) is admitted
// against a host-only stamp and claims nothing.
func (o PackageOwner) Admits(actor Actor, clientID string) (ok, claim bool) {
	if !o.App() || actor != o.Actor {
		return false, false
	}
	switch {
	case o.ClientID != "":
		return clientID == o.ClientID, false
	case clientID != "":
		return true, true
	}
	return true, false
}

// String names the owner for a person: the client id where it is stamped,
// the actor otherwise.
func (o PackageOwner) String() string {
	if o.ClientID != "" {
		return o.ClientID
	}
	return string(o.Actor)
}
