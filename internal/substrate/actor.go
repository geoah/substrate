package substrate

import "strings"

// Actor is the attributed identity of a writer ("console", "substratectl",
// "bundle:gmail.bundles.example.com", "substrate"). Attribution, not
// authorization.
//
// THE ACTOR DOMAIN is closed:
//
//	console                          a write from the console
//	substratectl                     a write from the operator's CLI
//	api                              a write from a client holding a token, door unnamed
//	bundle:<authority>               an install, and the authority's own hand
//	function:<authority>:<name>      a function's effect
//	agent:<authority>:<name>         an agent's effect
//	substrate                        the engine's own hand
//
// The three human doors are attribution the CALLER declares
// (`X-Substrate-Actor`); the last four are the substrate's own writing hands
// and a request may never claim one (ReservedActor).
//
// A machine hand carries the FULL AUTHORITY, never its first label, and the
// separator is a colon because `<actor>/<name>` label and annotation keys
// reserve the slash (engine metaKeyAllowed). An authority always carries a
// dot, so nothing derived here can collide with a flat name. Decision record
// 0020 has the why.
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
// declarations under: the core seed is `bundle:core`, an install is
// `bundle:<authority>`. Like the host namespace above, it is decided by name
// equality — the declaration authority check (engine) admits it for shipped
// kinds — so a request may never claim one either.
const BundleActorPrefix = "bundle:"

// FunctionActorPrefix and AgentActorPrefix open the two dispatch hands
// installed code writes under: `function:<authority>:<name>` for a function's
// effects, `agent:<authority>:<name>` for an agent's. Both are the
// substrate's to stamp on a dispatch, so a request may never claim one. They
// are held apart because one authority may declare a function and an agent of
// the same name, and a shared string would merge their manager rows and their
// trigger self-exclusion.
const (
	FunctionActorPrefix = "function:"
	AgentActorPrefix    = "agent:"
)

// retiredConnectorPrefix was a second spelling of BundleActorPrefix, minted
// from an authority's FIRST LABEL until record 0020 retired it. Nothing mints
// it now, and no changelog entry that carries it can be rewritten (the actor
// is in the hashed preimage), so it survives here for one reason: a request
// that could claim it would write something that reads as a past sync's hand.
const retiredConnectorPrefix = "connector:"

// ActorSeed is the actor the embedded core tree is copied into a new
// repository's changelog under. The tree is a SEED, not an authority: this actor
// says the entries came from the substrate's own shipped tree, once, at
// creation. `core` is a flat word and an authority always carries a dot, so
// it can never be an install's `bundle:<authority>`.
const ActorSeed Actor = BundleActorPrefix + "core"

// BundleActor renders a bundle's own writing hand, `bundle:<authority>` — the
// actor an install (a copy of the catalog's manifests into the repository's
// changelog) carries, and the hand an authority's own installed code writes
// under.
func BundleActor(authority string) Actor { return Actor(BundleActorPrefix + authority) }

// FunctionActor and AgentActor render the two dispatch hands. Both take the
// declaring authority, so two bundles declaring a callable of one name stay
// two actors.
func FunctionActor(authority, name string) Actor {
	return Actor(FunctionActorPrefix + authority + ":" + name)
}

func AgentActor(authority, name string) Actor {
	return Actor(AgentActorPrefix + authority + ":" + name)
}

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
// `substrate` namespace, the `bundle:` / `function:` / `agent:` paths
// installed code and shipped declarations write under, and the retired
// `connector:` spelling of the first of them. A name that merely RESEMBLES
// one ("substrateish.example.com") is ordinary.
func ReservedActor(a Actor) bool {
	s := string(a)
	return s == HostActorNamespace || strings.HasPrefix(s, HostActorNamespace+".") ||
		strings.HasPrefix(s, BundleActorPrefix) ||
		strings.HasPrefix(s, FunctionActorPrefix) ||
		strings.HasPrefix(s, AgentActorPrefix) ||
		strings.HasPrefix(s, retiredConnectorPrefix)
}
