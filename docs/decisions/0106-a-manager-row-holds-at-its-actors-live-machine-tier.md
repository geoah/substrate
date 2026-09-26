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
all read the tier through it. An apply that declares an actor at the machine
tier (newly, or down from owner or bundle) recomputes every mapped record
where that actor holds a row above machine, in the apply's transaction, so
the release happens without waiting for a source write.

Re-resolving every row fails: a machine row may be recompute's credit to a
source's actor, and promoting that actor would freeze values recompute
wrote. It would also release every pin of an uninstalled bundle, which
[0092](0092-an-orphaned-mapping-target-is-marked-and-collected-on-request.md)
refuses. A package's `bundle:` hand keeps its stored tier, because its rows
can carry the dispatch stamp, which its declaration does not show. Rewriting
the stored rows would add changelog entries for a change nobody wrote, and
the recompute already writes the values that move.

### Consequences

- Good, because declaring an importer at `tier: machine` is the whole
  repair: the next apply hands its values back to the sources.
- Good, because the stored row stays what the write recorded, so a rebuild
  replays it unchanged.
- Bad, because promotion is not symmetric: declaring an actor above machine
  does not pin rows it wrote at the machine tier; only its next write does.
- Bad, because an apply that declares an actor at the machine tier costs one
  recompute per record that actor holds, inside the apply's transaction.
- Bad, because `propertyMeta.tier` can differ from the stored column.

### Confirmation

`TestDeclaringAnActorAtTheMachineTierReleasesWhatItHolds` and
`TestAStoredOwnerRowOfAMachineActorReadsAsMachine` in
`internal/engine/tiers_db_test.go`.
