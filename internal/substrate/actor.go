package substrate

import (
	"context"
	"strings"
)

// Actor is the key of a writing hand inside one process ("person",
// "bundle:example.com:mail", "substrate"): what the engine decides a writer's
// tier, its namespace and its echo on. Attribution, not authorization. It is
// never stored and never on the wire: a stored write links the hand's
// `substrate.reamde.dev/core/actor` record (decision 0270).
//
// THE KEYS are closed:
//
//	person                                  the repository's person, through any of its tokens
//	bundle:<authority>:<package>            an install, and the package's own hand
//	function:<authority>:<package>:<name>   a function's effect
//	agent:<authority>:<package>:<name>      an agent's effect
//	app:<host>                              a write by an app's token
//	substrate                               the engine's own hand
//
// Every one is derived by the substrate from the token or the code that
// runs; no request names one.
//
// A machine hand carries the FULL AUTHORITY and the PACKAGE, never a first
// label, and the separator is a colon because `<actor>/<name>` label and
// annotation keys reserve the slash (engine metaKeyAllowed). An authority
// always carries a dot, so nothing derived here can collide with a flat name.
// Decision records 0025 and 0047 have the why.
type Actor string

const (
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
// `bundle:<authority>:<package>`. Like the host namespace above, it is decided
// by name equality — the declaration ownership check (engine) admits it for
// shipped kinds — so a request may never claim one either.
const BundleActorPrefix = "bundle:"

// FunctionActorPrefix and AgentActorPrefix open the two dispatch hands
// installed code writes under: `function:<authority>:<package>:<name>` for a
// function's effects, `agent:<authority>:<package>:<name>` for an agent's.
// Both are the substrate's to stamp on a dispatch, so a request may never
// claim one. They are held apart because one package may declare a function
// and an agent of the same name, and a shared string would merge their manager
// rows and their trigger self-exclusion.
const (
	FunctionActorPrefix = "function:"
	AgentActorPrefix    = "agent:"
)

// AppActorPrefix opens the actor an app's token writes under (decisions
// 0194 and 0209), so a change an app made reads as the app's and never as
// the owner's. What follows the prefix is what the approval proved:
// `app:<authority>` for an app whose publisher was verified, `app:local`
// for one whose access went to a program on the owner's device, and
// `app:<host>`, the host of its client id, for any other. The server derives
// it from the token record, so a request may never claim one, and no package
// may declare one: its tier is the engine's to decide, not data an install
// writes. The token behind each write is the changelog's principal, which
// tells two apps of one publisher apart.
const AppActorPrefix = "app:"

// AppActorLocal is the actor of every app whose access went to a loopback
// address: nothing tells two programs on the owner's device apart, so none
// is named by the host or the publisher it claims.
const AppActorLocal Actor = AppActorPrefix + "local"

// AppActor renders the actor of an app named by host, a `host[:port]` as a
// URL carries it, or by a verified authority. An IPv6 literal loses its
// brackets (`[::1]:8080` is `app:::1:8080`), since the actor is also the
// namespace of the app's label and annotation keys, which take no bracket.
func AppActor(host string) Actor {
	host = strings.NewReplacer("[", "", "]", "").Replace(strings.ToLower(host))
	return Actor(AppActorPrefix + host)
}

// IsAppActor reports whether an actor is an app's.
func IsAppActor(a Actor) bool {
	return strings.HasPrefix(string(a), AppActorPrefix) && len(a) > len(AppActorPrefix)
}

// retiredConnectorPrefix was a second spelling of BundleActorPrefix, minted
// from an authority's FIRST LABEL until record 0025 retired it. Nothing mints
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

// BundleActor renders a package's own writing hand,
// `bundle:<authority>:<package>` — the actor an install (a copy of the
// catalog's manifests into the repository's changelog) carries, and the hand a
// package's own installed code writes under.
func BundleActor(authority, pkg string) Actor {
	return Actor(BundleActorPrefix + authority + ":" + pkg)
}

// FunctionActor and AgentActor render the two dispatch hands. Both take the
// declaring authority and package, so two closures declaring a callable of one
// name stay two actors.
func FunctionActor(authority, pkg, name string) Actor {
	return Actor(FunctionActorPrefix + authority + ":" + pkg + ":" + name)
}

func AgentActor(authority, pkg, name string) Actor {
	return Actor(AgentActorPrefix + authority + ":" + pkg + ":" + name)
}

// IsBundleActor reports whether an actor is a bundle path — the seed, an
// install or an upgrade of shipped vocabulary.
func IsBundleActor(a Actor) bool {
	return strings.HasPrefix(string(a), BundleActorPrefix) && len(string(a)) > len(BundleActorPrefix)
}

// IsPerson reports whether actor is a person of the repository: its first,
// or another a person's actor names by their identity (PersonActor).
func IsPerson(a Actor) bool {
	return a == ActorPerson || strings.HasPrefix(string(a), PersonActorPrefix)
}

type assistantKey struct{}

// WithAssistant marks ctx as an assistant's request at the MCP door
// (decision 0230). The owner approved the assistant to do what they do, so
// it holds the owner's hand at the gates a person's door alone passes,
// while its writes keep its own `app:` actor.
func WithAssistant(ctx context.Context) context.Context {
	return context.WithValue(ctx, assistantKey{}, true)
}

// OwnersHand reports whether actor acts as the repository's owner: the
// person, or an app's actor on a request the MCP door marked.
// Code the assistant runs writes under its own actor (`function:`,
// `agent:`), which the mark never covers.
func OwnersHand(ctx context.Context, actor Actor) bool {
	if IsPerson(actor) {
		return true
	}
	marked, _ := ctx.Value(assistantKey{}).(bool)
	return marked && strings.HasPrefix(string(actor), AppActorPrefix)
}

// ReservedActor reports whether an actor name is one the substrate derives and
// therefore may never be claimed by a request: the `substrate` namespace, the
// `bundle:` / `function:` / `agent:` paths installed code and shipped
// declarations write under, the `app:` path an app's token writes under, and
// the retired `connector:` spelling of the first of them. A name that merely RESEMBLES
// one ("substrateish.example.com") is ordinary.
func ReservedActor(a Actor) bool {
	s := string(a)
	return s == HostActorNamespace || strings.HasPrefix(s, HostActorNamespace+".") ||
		strings.HasPrefix(s, BundleActorPrefix) ||
		strings.HasPrefix(s, FunctionActorPrefix) ||
		strings.HasPrefix(s, AgentActorPrefix) ||
		strings.HasPrefix(s, AppActorPrefix) ||
		strings.HasPrefix(s, retiredConnectorPrefix)
}

// NamesItsRecords reports whether a writer may create a record under an id it
// chooses. Only code that derives the id from data names its records: the
// engine and the host's other hands, a bundle's install (a seed or sample
// carries its records' ids), and a function, whose effects compose ids from
// upstream keys so a replay converges on the same rows (decision 0162). A
// person, an app and an agent give a new record of a kind the repository
// installed a minted id (decision 0228): a name chosen for what a record
// says stops matching once the record is retitled.
func NamesItsRecords(a Actor) bool {
	s := string(a)
	return s == HostActorNamespace || strings.HasPrefix(s, HostActorNamespace+".") ||
		IsBundleActor(a) ||
		strings.HasPrefix(s, FunctionActorPrefix)
}

// AN ACTOR IS A RECORD (decision 0270). Every hand that writes is one record
// of `substrate.reamde.dev/core/actor`, with a type and a link to what it is,
// and a history line, a change event and a stored value name it by that
// record's id, never by a spelling. The Actor values above are what the engine
// passes around inside one process to decide a writer's tier, its namespace
// and its echo: keys, never stored and never on the wire. CanonicalActor says
// which record a key is, and vocabulary.ActorID the record's id.

// The types of actor, the values of an actor record's `type`.
const (
	ActorTypePerson   = "person"
	ActorTypeApp      = "app"
	ActorTypeFunction = "function"
	ActorTypeAgent    = "agent"
	ActorTypePackage  = "package"
	ActorTypeSystem   = "system"
)

// ActorTypes are the six, in the order a reader lists them.
var ActorTypes = []string{ActorTypePerson, ActorTypeApp, ActorTypeFunction, ActorTypeAgent, ActorTypePackage, ActorTypeSystem}

// ActorPerson is the key of the repository's first person, the one its
// creation wrote, whichever of their tokens they write through: which client
// a write came from is on its token.
const ActorPerson Actor = "person"

// PersonActorPrefix marks the key of a person of the repository other than
// its first, named by their identity's id.
const PersonActorPrefix = "person:"

// PersonActor is the key of the person whose identity is identity, when they
// are not the repository's first person (ActorPerson).
func PersonActor(identity string) Actor { return Actor(PersonActorPrefix + identity) }

// CanonicalActor is the key of the actor record a writing hand is. The doors,
// and any label a person's request once claimed for itself, are the person; the engine's hands (`substrate`, `substrate.oauth`,
// `substrate.webhook`, a policy's judge) are the system; the seed is the core
// package; a mapping's hold on a subject slot is its package. Every other
// key is its own.
func CanonicalActor(a Actor) Actor {
	s := string(a)
	switch {
	case a == "":
		return ""
	case a == ActorPerson, a == "api", a == "console", a == "substratectl":
		// The three doors a person's request once named itself by.
		return ActorPerson
	case strings.HasPrefix(s, PersonActorPrefix) && len(s) > len(PersonActorPrefix):
		return a
	case s == HostActorNamespace || strings.HasPrefix(s, HostActorNamespace+".") || strings.HasPrefix(s, "policy:"):
		return ActorSystem
	case a == ActorSeed:
		return BundleActor("substrate.reamde.dev", "core")
	case strings.HasPrefix(s, "mapping:"):
		// `mapping:<authority>/<package>/<name>`: the declaration's package.
		parts := strings.Split(strings.TrimPrefix(s, "mapping:"), "/")
		if len(parts) == 3 {
			return BundleActor(parts[0], parts[1])
		}
	case !ReservedActor(a) && !strings.HasPrefix(s, actorByIDPrefix):
		// Any other name a full-access token's request claimed through the
		// retired actor header: a label of the person's own.
		return ActorPerson
	}
	return a
}

// ActorTypeOf is the type of the actor record a key is, "" for a key no type
// covers.
func ActorTypeOf(a Actor) string {
	c := CanonicalActor(a)
	s := string(c)
	switch {
	case IsPerson(c):
		return ActorTypePerson
	case c == ActorSystem:
		return ActorTypeSystem
	case IsAppActor(c):
		return ActorTypeApp
	case strings.HasPrefix(s, FunctionActorPrefix):
		return ActorTypeFunction
	case strings.HasPrefix(s, AgentActorPrefix):
		return ActorTypeAgent
	case IsBundleActor(c), strings.HasPrefix(s, retiredConnectorPrefix):
		return ActorTypePackage
	}
	return ""
}

// actorByIDPrefix marks a key that names an actor record by its id alone: a
// stored link whose hand this process cannot tell, kept so that it round-trips
// to the same id (vocabulary.ActorID).
const actorByIDPrefix = "id:"

// ActorByID is the key of the actor record id names, for a stored link whose
// hand is not known.
func ActorByID(id string) Actor { return Actor(actorByIDPrefix + id) }

// ActorIDOf reports the id an ActorByID key names.
func ActorIDOf(a Actor) (string, bool) {
	return strings.CutPrefix(string(a), actorByIDPrefix)
}
