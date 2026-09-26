---
status: accepted
date: 2026-09-26
decision-makers: George Antoniadis (via the issue-672 agent session)
---

# 0106. A package row names the actor that declared it

## Context and Problem Statement

A package an agent declares for a person (recipes, a reading list) lists in
the console exactly like one the owner declared by hand, because nothing on
the `package` row says who declared it
([issue #672](https://github.com/geoah/substrate/issues/672)). The console
redesign needs that fact to show "Made by <agent>" and to group a package's
collections, tools and agent. Actors are engine-derived everywhere else
([0025](0025-an-actor-carries-the-full-authority.md)), so this one must be
too.

## Considered Options

- A managed `declaredBy` property on the `package` row, stamped with the
  creating transaction's actor.
- Derive it on read from the actor of the row's first changelog entry.
- A `declaredBy` key a package document may carry.

## Decision Outcome

Chosen: the managed `declaredBy` property. The write that creates a `package`
row (none stored, or only a tombstone) sets it to the transaction's actor; a
write to a live row leaves it alone, stamped or not, and no document key
spells it, so the loader refuses a document that tries. The changelog carries
the stamp like any other value, so a rebuild and an export keep it. A changelog
lookup on every read costs a scan per package on every listing, and a document
key would let a client name any actor.

### Consequences

- Good, because the record read already returns it: no route, no wire struct.
- Good, because `bundle:core`, `bundle:<authority>:<package>`, a door
  (`console`, `substratectl`, `api`) and `agent:<authority>:<package>:<name>`
  tell a seeded, a catalog, a hand-declared and an agent-declared package apart.
- Bad, because a package created before this change carries no stamp and
  nothing backfills it; a later write does not stamp it either, since that
  would name whoever touched it next.
- Bad, because an agent's declaration reads as agent-made only where the
  engine carries the agent's actor into the vocabulary apply; a vocabulary
  apply over HTTP is attributed to the door it came through.

### Confirmation

`TestPackageRecordsTheActorThatDeclaredIt` and
`TestPackageDocumentCannotWriteDeclaredBy` in `internal/engine`.

## More Information

Revisit if a package's declarer must be recoverable for packages that predate
the stamp: a repository migration could read the actor of each package row's
first changelog entry.
