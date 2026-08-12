package substrate

import "strings"

// Actor is the attributed identity of a writer ("console", "substratectl",
// "connector:gmail", "substrate"). Attribution, not authorization.
//
// THE ACTOR DOMAIN is closed and flat:
//
//	console            a write from the console
//	substratectl              a write from the operator's CLI
//	api                a write from a client holding a token, door unnamed
//	connector:<name>   a connector's sync
//	function:<name>    a function's effect
//	bundle:<name>      a bundle writing its own declarations
//	substrate          the engine's own hand
//
// The three human doors are attribution the CALLER declares
// (`X-Substrate-Actor`); the last three are the substrate's own writing hands
// and a request may never claim one (ReservedActor).
type Actor string

const (
	// ActorAPI is the default attribution for an authenticated write that
	// names no door: a client holds a token, and that is all the substrate
	// knows about it. The console sends ActorConsole and substratectl sends
	// ActorCLI; both are ordinary claimable names.
	ActorAPI     Actor = "api"
	ActorConsole Actor = "console"
	ActorCLI     Actor = "substratectl"
	// ActorSystem is the engine's own hand — recompute, mirrors, merge
	// execution, the boot-time vocabulary upgrade, and the auth paths that
	// write the credential and token records. A bare word, not a DNS name: the actor
	// domain is closed, so nothing has to be forward-qualified to stay out of
	// the vocabulary's way.
	ActorSystem Actor = "substrate"
)

// HostActorNamespace is the first label the host's own writing hands share:
// ActorSystem above and the OAuth facility's `substrate.oauth`. Those hands
// are decided by actor-NAME equality at write time, so a request that could
// CLAIM one of them could forge a credential ref, a token status or a
// connected account's address. Nothing internal takes its actor from a
// request, so the whole namespace is refused at the one place a caller names
// an actor: the `X-Substrate-Actor` header (api/auth.go).
const HostActorNamespace = "substrate"

// BundleActorPrefix opens the actor a SHIPPED TREE OR CATALOG writes its
// declarations under: the core seed is `bundle:core`, an
// bundle install is `bundle:<name>`. Like the host namespace above, it is
// decided by name equality — the declaration authority check (engine) admits
// it for shipped kinds — so a request may never claim one either.
const BundleActorPrefix = "bundle:"

// ConnectorActorPrefix and FunctionActorPrefix open the two machine hands
// installed code writes under: `connector:<name>` for a connector's sync,
// `function:<name>` for a function's effects. Both are the substrate's to
// stamp on a dispatch, so a request may never claim one.
const (
	ConnectorActorPrefix = "connector:"
	FunctionActorPrefix  = "function:"
)

// ActorSeed is the actor the embedded core tree is copied into a new
// repository's changelog under. The tree is a SEED, not an authority: this actor
// says the entries came from the substrate's own shipped tree, once, at
// creation.
const ActorSeed Actor = BundleActorPrefix + "core"

// BundleActor renders a bundle's own writing hand, `bundle:<name>` — the
// actor an install (a copy of the catalog's manifests into the repository's
// changelog) carries.
func BundleActor(name string) Actor { return Actor(BundleActorPrefix + name) }

// ConnectorActor and FunctionActor render the two dispatch hands.
func ConnectorActor(name string) Actor { return Actor(ConnectorActorPrefix + name) }
func FunctionActor(name string) Actor  { return Actor(FunctionActorPrefix + name) }

// IsBundleActor reports whether an actor is a bundle path — the seed, an
// install or an upgrade of shipped vocabulary.
func IsBundleActor(a Actor) bool {
	return strings.HasPrefix(string(a), BundleActorPrefix) && len(string(a)) > len(BundleActorPrefix)
}

// HumanActors are the three doors a request may attribute itself to. They are
// ordinary claimable names — attribution is not authorization — and they are
// the whole of what a human write can say about itself.
var HumanActors = map[Actor]bool{ActorAPI: true, ActorConsole: true, ActorCLI: true}

// ReservedActor reports whether an actor name is one of the host's own
// writing hands and therefore may never be claimed by a request: the
// `substrate` namespace, and the `bundle:` / `connector:` / `function:`
// paths installed code and shipped declarations write under. A name that
// merely RESEMBLES one ("substrateish.example.com") is ordinary.
func ReservedActor(a Actor) bool {
	s := string(a)
	return s == HostActorNamespace || strings.HasPrefix(s, HostActorNamespace+".") ||
		strings.HasPrefix(s, BundleActorPrefix) ||
		strings.HasPrefix(s, ConnectorActorPrefix) ||
		strings.HasPrefix(s, FunctionActorPrefix)
}
