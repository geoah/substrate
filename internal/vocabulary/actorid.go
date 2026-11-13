package vocabulary

import (
	"strings"
	"sync"

	"github.com/geoah/substrate/internal/substrate"
)

// AN ACTOR RECORD'S ID IS DERIVED FROM WHAT IT IS (decision 0270). The id of
// the `substrate.reamde.dev/core/actor` record a writing hand is comes from the
// hand's canonical key alone (substrate.CanonicalActor), in the minted shape,
// so the same actor is the same record in every rebuild and import, and a
// conversion handed one history line at a time turns an old spelling into its
// link with nothing else to read. The repository's one person and the system
// are one record each in every repository: an id is a repository's own.

// actorIDs remembers, for this process, the key behind every id ActorID has
// derived: the one way back from an id to the hand the engine decides on
// (ActorKeyOf). A key is the same in every repository, so one map serves all.
var actorIDs sync.Map // id -> substrate.Actor

// ActorID is the id of the actor record a writing hand is, "" for none. A
// key that only names a record's id (substrate.ActorByID) is that id.
func ActorID(a substrate.Actor) string {
	if a == "" {
		return ""
	}
	if id, ok := substrate.ActorIDOf(a); ok {
		return id
	}
	key := substrate.CanonicalActor(a)
	id := DerivedMintedID("actor", string(key))
	actorIDs.LoadOrStore(id, key)
	return id
}

// ActorKeyOf is the canonical key behind an actor record's id, as far as this
// process has derived it or learned it (RememberActor). An id it has not is
// answered false; the engine then reads the record (engine actorKeyOf).
func ActorKeyOf(id string) (substrate.Actor, bool) {
	v, ok := actorIDs.Load(id)
	if !ok {
		return "", false
	}
	return v.(substrate.Actor), true
}

// RememberActor teaches this process the key behind an actor record read
// back from storage, so ActorKeyOf answers for it.
func RememberActor(id string, key substrate.Actor) {
	if id != "" && key != "" {
		actorIDs.LoadOrStore(id, key)
	}
}

// ActorRef is the reference value naming an actor record.
func ActorRef(id string) map[string]any {
	return map[string]any{"ref": RecordPath(CoreKind(DocActor), id)}
}

// ActorProperties is what the actor record of a key holds: its type, a name
// to show, and the link to what it is.
func ActorProperties(a substrate.Actor, personName string) map[string]any {
	key := substrate.CanonicalActor(a)
	s := string(key)
	props := map[string]any{"type": substrate.ActorTypeOf(key)}
	switch props["type"] {
	case substrate.ActorTypePerson:
		props["name"] = personName
		if identity, ok := strings.CutPrefix(s, substrate.PersonActorPrefix); ok {
			props["identity"] = identity
		}
	case substrate.ActorTypeSystem:
		props["name"] = "substrate"
	case substrate.ActorTypeApp:
		publisher := strings.TrimPrefix(s, substrate.AppActorPrefix)
		props["name"] = publisher
		props["publisher"] = publisher
	case substrate.ActorTypeFunction, substrate.ActorTypeAgent:
		parts := strings.Split(s, ":")
		if len(parts) == 4 {
			identity := parts[1] + "/" + parts[2] + "/" + parts[3]
			props["name"] = identity
			link := DocFunction
			if props["type"] == substrate.ActorTypeAgent {
				link = DocAgent
			}
			props[link] = map[string]any{"ref": RecordPath(CoreKind(link), identity)}
		}
	case substrate.ActorTypePackage:
		parts := strings.Split(s, ":")
		if len(parts) == 3 && parts[0]+":" == substrate.BundleActorPrefix {
			pkg := parts[1] + "/" + parts[2]
			props["name"] = pkg
			props[DocPackage] = map[string]any{"ref": RecordPath(CoreKind(DocPackage), pkg)}
		} else {
			props["name"] = s
		}
	}
	return props
}

// ActorKeyFrom is the key an actor record stands for, read from its type and
// its link: the way back from a stored id when this process never derived it.
func ActorKeyFrom(props map[string]any) (substrate.Actor, bool) {
	link := func(name, kind string) (string, bool) {
		ref, _ := props[name].(map[string]any)
		path, _ := ref["ref"].(string)
		return strings.CutPrefix(path, CoreKind(kind)+"/")
	}
	switch props["type"] {
	case substrate.ActorTypePerson:
		// The first person's record names their identity too: the caller
		// that holds the record's id tells it apart (engine actorKeyIn).
		if identity, _ := props["identity"].(string); identity != "" {
			return substrate.PersonActor(identity), true
		}
		return substrate.ActorPerson, true
	case substrate.ActorTypeSystem:
		return substrate.ActorSystem, true
	case substrate.ActorTypeApp:
		if p, _ := props["publisher"].(string); p != "" {
			return substrate.Actor(substrate.AppActorPrefix + p), true
		}
	case substrate.ActorTypeFunction, substrate.ActorTypeAgent:
		kind, prefix := DocFunction, substrate.FunctionActorPrefix
		if props["type"] == substrate.ActorTypeAgent {
			kind, prefix = DocAgent, substrate.AgentActorPrefix
		}
		if identity, ok := link(kind, kind); ok {
			if parts := strings.Split(identity, "/"); len(parts) == 3 {
				return substrate.Actor(prefix + parts[0] + ":" + parts[1] + ":" + parts[2]), true
			}
		}
	case substrate.ActorTypePackage:
		if pkg, ok := link(DocPackage, DocPackage); ok {
			if parts := strings.Split(pkg, "/"); len(parts) == 2 {
				return substrate.BundleActor(parts[0], parts[1]), true
			}
		}
	}
	return "", false
}
