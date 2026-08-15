---
status: accepted
date: 2026-08-15
decision-makers: George Antoniadis
---

# 0021. `account` is an optional owner edge on shared kinds and a provenance string on mirrors

## Context and Problem Statement

Two spellings of `account` shipped side by side
([#124](https://github.com/geoah/substrate/issues/124), under the
[#119](https://github.com/geoah/substrate/issues/119) tracker). Every provider
mirror kind carries `account` as a string property holding the account
record's id; the shared kinds `calendar.calendar`,
`messaging.conversation` and `messaging.emailthread` carried a required
`ownerRef` edge named `account` pointing at `any`. The edge made those three
kinds writable only by a connector, because a person authoring a calendar has
no provider account to point at, and that is why they were provider-only in
practice while `order`, `task` and `place` are not. The v1 freeze makes the
choice one-way: dropping `required` later is free, but turning a live edge
into a property, or a live property into an edge, is a narrowing the boot
upgrade refuses while records hold the old shape.

## Considered Options

- One spelling everywhere, the edge: retype `account` on every mirror kind
  from a string property to an owner edge
- One spelling everywhere, the string: drop the edge from the three shared
  kinds and carry the account id as a string property there too
- Two spellings, each named for what it does: an optional owner edge where the
  account owns the record, a string where it is only provenance

## Decision Outcome

Chosen: two spellings, and the `required` marker leaves the three shared
edges. The edge and the string are not two ways of saying one thing. The edge
is ownership: a shared kind's rows live in a shared authority that a bundle
purge never reaches, so `ownerRef` is the only thing that removes the
calendars, conversations and threads a connection brought in, and a string can
never cascade. The string is provenance: a mirror row lives in the bundle's own
authority, it holds the provider's own shape, and the id it carries says which
account it was read from.

Retyping the mirrors to edges was the option that would have unified them, and
it is refused for released stores: dropping `account` from a mirror kind while
rows carry it is a refused narrowing, so the change would strand every
repository that has synced anything. Dropping the edge from the shared kinds
would have cost the cascade, which is the only mechanism that removes synced
records when a connection goes away.

So the rule, stated once: an `account` EDGE is `ownerRef: true` and never
`required: true`; an `account` PROPERTY is a string carrying the account
record's id, which a sync body reads as provenance and the engine never
follows.

### Consequences

- Good, because `calendar.calendar`, `messaging.conversation` and
  `messaging.emailthread` can be created by hand, by an importer or by an
  agent, which is what a shared vocabulary is for.
- Good, because the cascade is unchanged: a synced record still points at its
  account, and deleting the account still collects it.
- Bad, because a record can now exist with no account at all, so a reader that
  assumed the edge was there has to handle its absence. Nothing in the tree
  did.
- Bad, because the two spellings stay, and a newcomer reading `account` still
  has to look at whether it is under `properties` or `edges` to know what it
  means.
- Bad, because deleting one account still leaves that account's mirror rows
  behind: the string ties them to nothing, and the only verb that clears them
  is a whole-bundle purge. A per-account sweep is the open work this decision
  does not do.

### Confirmation

`kinds/kinds_test.go`'s `TestAccountIsAnOwnerEdgeOrAProvenanceString` walks
every declaration in the tree and refuses an `account` edge that is required
or not `ownerRef`, an `account` property that is not a string, and a kind that
declares both. `TestSharedKindsAreAuthorableWithoutAnAccount` in
`internal/engine` creates all three shared kinds with no account, and
`TestFinalizersAndOwnerRefGC` beside it still holds the cascade.

## More Information

The same issue settled the provider-key spelling in the Linear bundle: its
three mirror kinds said `providerId` where every other bundle names the
provider's own key, so they now say `userId`, `teamId` and `issueId`.

Revisit when disconnecting one account has to clear that account's mirror rows.
That needs a sweep keyed on the string, or an edge the mirrors cannot take
until their rows can be migrated.
