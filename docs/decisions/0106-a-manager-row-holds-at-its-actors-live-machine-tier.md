---
status: accepted
date: 2026-09-26
decision-makers: George Antoniadis (via the issue-583 agent session)
---

# 0106. A manager row holds at the machine tier once its actor is declared there

## Context and Problem Statement

`docs/projection.md` said the tier is resolved from the live declaration on
every write, never frozen at mint. Mapping recompute yielded on the tier
stored on the `property_managers` row instead, so an importer that wrote
under an undeclared name (the owner tier) kept every property it wrote
pinned after its name was declared at `tier: machine`, and no provider
mirror could replace or union into them
([issue #583](https://github.com/geoah/substrate/issues/583)). A same-value
re-import is suppressed as a no-op and does not refresh the row either.

## Considered Options

- Keep the stored tier and correct the doc: the only repair is a null-patch
  per property
- Re-resolve every row's tier from its actor's live declaration
- Hold a row at the machine tier when its actor's live declaration is the
  machine tier, keep the stored tier otherwise, and recompute the affected
  records when an apply declares an actor at the machine tier
- Rewrite the stored rows when an actor's declared tier changes

## Decision Outcome

Chosen: the third option. A row stored above the machine tier holds at the
machine tier when a live declaration puts its actor there (`heldTierIn`,
`internal/engine/rows.go`). The yield, the orphan mark and `propertyMeta`
read the tier through it. A kind move, a merge, a rename and the release of a
removed mapping's values (`releaseMachineManaged`) read the stored tier, so
none of them rewrites or nulls a row on the strength of a later declaration.

An apply recomputes every mapped record where an actor holds a row stored
above machine, in the apply's transaction, when the candidate declares that
actor at the machine tier and either the live declarations did not (the
transition), or the actor's package is one the batch touches. The second case
releases a repository whose actor was declared at the machine tier before
this record: re-applying the package that declares it is the release.

This amends condition 3 of
[0092](0092-an-orphaned-mapping-target-is-marked-and-collected-on-request.md):
"every row is at the machine tier" now reads "every row holds at the machine
tier". A record only a demoted actor wrote is marked the way a record a
machine actor wrote from the start is.

Re-resolving every row fails: a machine row may be recompute's credit to a
source's actor, and promoting that actor would freeze values recompute
wrote. It would also release every pin of an uninstalled bundle, which 0092
refuses. A package's `bundle:` hand keeps its stored tier, because its rows
can carry the dispatch stamp, which its declaration does not show. Rewriting
the stored rows would add changelog entries for a change nobody wrote, and
the recompute already writes the values that move.

### Consequences

- Good, because an importer declared at `tier: machine` hands every mapped
  property a live source offers back to that source in the apply, without a
  null-patch per property.
- Good, because the stored row stays what the write recorded, so a rebuild
  replays it unchanged.
- Bad, because the release is recompute's machine-tier rule, not a merge: a
  non-required mapped property the actor wrote that no live source offers is
  deleted, and a `merge: union` keeps only its sources' items. An import that
  must survive its sources is written as a source kind instead
  ([projection](../projection.md#contributing-a-value)).
- Bad, because a record with no live source whose only rows above machine are
  a demoted actor's is marked orphaned in the apply, and with
  `SUBSTRATE_ORPHAN_GRACE` set the GC sweep may collect it, unmapped
  properties included.
- Bad, because a repository where the actor was declared at the machine tier
  before this record releases nothing at boot: `propertyMeta` reports
  `machine` at once, but values and orphan marks move only at each record's
  next recompute or at an apply of the package that declares the actor.
- Bad, because promotion is not symmetric: declaring an actor above machine
  does not pin rows it wrote at the machine tier; only its next write does.
- Bad, because the apply costs one recompute per record the actor still
  holds above machine, inside the apply's transaction and under the
  registry-dependency lock, with no ceiling. A row recompute does not rewrite
  (an unmapped property, a value a source already matches) keeps its stored
  tier, so every apply of that package pays for its record again.
- Bad, because `propertyMeta.tier` can differ from the stored column.

### Confirmation

`TestDeclaringAnActorAtTheMachineTierReleasesWhatItHolds`,
`TestAStoredOwnerRowOfAMachineActorReadsAsMachine`,
`TestDeclaringAnActorAtTheMachineTierDeletesWhatNoSourceOffers` and
`TestReapplyingAMachineActorsPackageReleasesItsStaleHolds` in
`internal/engine/tiers_db_test.go`, and `TestHeldTierIn` in
`internal/engine/heldtier_internal_test.go` for the rows that keep their
stored tier.
