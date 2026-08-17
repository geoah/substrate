---
status: accepted
date: 2026-08-17
decision-makers: George Antoniadis (via the issue-203 agent session)
---

# 0020. An actor carries the full authority, and `connector:` is retired

## Context and Problem Statement

`AuthorityActor` minted `connector:<first label>` and a callable's actor was
`function:<local name>` (internal/vocabulary/ref.go, function.go), so two
authorities sharing a first label, or two bundles each declaring a function of
the same name, wrote under one actor string. That string is the property
manager ledger's key, the policy door's tier lookup and the trigger
self-exclusion key (engine/changefeed.go), so one bundle's writes were dropped
as the other's echo, silently. `bundleNameProblems` (vocabulary/load.go)
refused the bundle half at declaration time and covered neither functions nor
agents. [0014](0014-authorities-widen-only-outside-the-id-alphabet.md) required
this keying to move to the full authority before authorities may widen to URLs;
this record discharges the actor half of that reservation
([issue #203](https://github.com/geoah/substrate/issues/203)).

## Considered Options

- Derive the actor from the full authority, engine-side
- Let a bundle declare the actor string it writes under
- Key on a fixed-length hash of the authority
- Keep the first label and widen `bundleNameProblems` to functions and agents

## Decision Outcome

Chosen: derive the actor from the full authority, in the engine, from the
identity the loader already holds. `bundle:<authority>` is what an install and
the authority's own hand write under, `function:<authority>:<name>` a
function's effects, `agent:<authority>:<name>` an agent's. The separator is a
colon because `<actor>/<name>` label and annotation keys reserve the slash
(engine/dataset.go `metaKeyAllowed`), and `reActor` (vocabulary/naming.go)
widens to admit the dots and the second colon. An authority always carries a
dot, so a derived actor can never collide with the flat names `api`,
`console`, `substratectl`, `substrate` or with the seed's `bundle:core`.

An actor is never registered and never self-declared. A bundle that chose its
own actor string could name another bundle's hand and inherit its manager
rows, its policy tier and its echo suppression, so the derivation stays the
engine's: `substrate.BundleActor`, `substrate.FunctionActor` and
`substrate.AgentActor` are the only mints, and `X-Substrate-Actor` refuses
every prefix they use (api/auth.go). An actor DOCUMENT naming a dispatch hand
is refused by the loader for the same reason: a declaration carries a tier,
and admitting one would let any authority set the tier another authority's
callable writes at.

A hash of the authority collides too, rarely and then unreadably, and it makes
a changelog unreadable by inspection, which is the property 0014 exists to
keep. Widening `bundleNameProblems` refuses legal declarations to protect a
key that did not have to be ambiguous.

**`connector:` is retired as a second spelling of `bundle:`.** Both named the
same thing, an authority's own installed code writing into a repository. One
spelling means one manager namespace and one thing to decide at the door.
`ConnectorActorPrefix` is gone from the exported surface; `ReservedActor`
still refuses a claimed `connector:` name, because entries written under the
old spelling exist and a request able to claim one could forge a write that
reads as a past sync's.

**Entries already in a changelog keep their old actor strings, verbatim,
forever.** The actor is part of the hashed preimage
([0009](0009-the-chain-hashes-what-postgres-stored.md)), so rewriting one
breaks the chain, and the changelog is append-only. Nothing translates an old
spelling on read: the fold replays the actor and the tier each entry recorded,
so a rebuild reproduces `connector:gmail` and its manager rows exactly as they
stood. `actorTier` resolves an unknown reserved actor to the machine tier,
which is where the old spellings already stood, so no stored row gains
standing from the rename.

The one visible seam is the first dispatch after an upgrade: a trigger's
self-exclusion keys on the new spelling, so an entry its own callable wrote
under the old one is no longer read as its own echo and may be delivered once.
It is bounded by the trigger cursor and it does not repeat.

`bundleNameProblems` is deleted. Its stated reason was the shared actor;
GraphQL name collisions are `graphqlNameProblems`' to refuse, with a message
that names both kinds, and a bundle's `metadata.id` is `<authority>/<name>`,
already unique per authority.

### Consequences

- Good, because two bundles under one first label can be installed together,
  each attributed and echo-suppressed on its own.
- Good, because 0014's actor reservation is discharged: no actor is derived
  from a first label, so the URL-authority move no longer inherits it.
- Bad, because a repository's changelog carries two spellings of the same hand
  for the rest of its life, and anyone reading history has to know that
  `connector:x` and `bundle:x.…` were the same code.
- Bad, because actors are longer, and a label key a function writes is now
  `function:<authority>:<name>/<key>`.
- Bad, because the first dispatch after the upgrade may hand a callable its
  own last write, once.
- Bad, because the GraphQL prefix (`GraphQLName`) still keys on the first
  label; 0014's reservation stands for it, and it is the only one left.

### Confirmation

`TestMachineActorsCarryTheFullAuthority` and
`TestReservedActorHoldsTheRetiredConnectorPrefix` (internal/substrate) pin the
three derivations and hold the door shut on the retired spelling;
`TestActorsCarryTheFullAuthority`, `TestValidActor` and
`TestTwoAuthoritiesMayShareABundleName` (internal/vocabulary) pin the widened
grammar, the refusal of a declared dispatch hand, and the two bundles that
used to be refused for one label; `TestOldActorSpellingSurvivesRebuild`
(internal/engine) writes an entry as `connector:gmail`, rebuilds the
repository from its changelog, and asserts the actor and its manager row come
back unchanged, at the machine tier, with the chain still verifying.

## More Information

This record discharges the actor clause of
[0014](0014-authorities-widen-only-outside-the-id-alphabet.md); that record
stands, including its id alphabet freeze and its remaining first-label
reservation on the GraphQL prefix. Reopen trigger: a hand under one authority
that is neither a bundle, a function nor an agent, which needs a fourth prefix
rather than a spelling change.
