package engine

// WHOSE HAND RESOLVED IT (decision record 0327). A question, a call waiting
// for approval and a change request each end by somebody's hand, and each
// links the actor record of that hand beside the time it already kept:
// `resolvedBy` on an interaction, `decidedBy` on the other two. The write
// path stamps it in the write that moves the record out of its waiting
// state, so the link and the resolution land together and no client sends
// it. A person reads it without reading the actor kind: a reference value
// names an address, and the filter value `me` (threadmarks.go) says which of
// them are the reader's own.

import (
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The properties that link the hand, as the declarations spell them.
const (
	propResolvedBy = "resolvedBy"
	propDecidedBy  = "decidedBy"
)

// resolverProps names, for each of waitingKinds, the property that links
// whose hand moved the record out of the state it waits in.
var resolverProps = map[string]string{
	vocabulary.KindLLMInteraction:     propResolvedBy,
	vocabulary.KindLLMApproval:        propDecidedBy,
	vocabulary.KindRecordPatchRequest: propDecidedBy,
}

// stampResolver links the hand of this write on a record of one of
// waitingKinds that the write moves out of its waiting state, and answers
// accepted with the property added. The kind's later moves are the engine's
// (an approved call's run) and stamp nothing. A repository whose upgrade has
// not declared the property keeps none.
func (t *txn) stampResolver(ty *vocabulary.Kind, row *erow, machine, from string, accepted []string) []string {
	w, ok := waitingKinds[ty.Identity]
	if !ok || w.machine != machine || w.state != from {
		return accepted
	}
	// A question answered in a chat message links that message, in the same
	// write (interactions.go replyToInteraction).
	if ty.Identity == vocabulary.KindLLMInteraction && t.replying != "" && row.States[machine] == interactionReplied {
		if p, declared := ty.Prop(propRepliedIn); declared && p.To == typeMessage {
			row.Props[propRepliedIn] = referenceValueOf(vocabulary.RecordPath(typeMessage, t.replying))
			accepted = append(accepted, propRepliedIn)
		}
	}
	prop := resolverProps[ty.Identity]
	if p, declared := ty.Prop(prop); !declared || p.To != kindActor {
		return accepted
	}
	id := t.resolverID()
	if id == "" {
		return accepted
	}
	row.Props[prop] = referenceValueOf(vocabulary.RecordPath(kindActor, id))
	return append(accepted, prop)
}

// resolverID is the id of the actor record this write's hand resolves a
// record as: the person, the person an app's token acts for, or the function
// or agent that decided a change request. "" for a hand with no actor record
// of its own to name: a policy rule, whose request names the rule, and an
// app acting for nobody.
func (t *txn) resolverID() string {
	switch substrate.ActorTypeOf(t.actor) {
	case substrate.ActorTypePerson:
		return actorID(t.ds.personActorFor(t.ctx, t.actor))
	case substrate.ActorTypeApp:
		if p := t.onBehalfOfKey(t.actor); substrate.ActorTypeOf(p) == substrate.ActorTypePerson {
			return actorID(p)
		}
	case substrate.ActorTypeFunction, substrate.ActorTypeAgent:
		return actorID(t.actor)
	}
	return ""
}
