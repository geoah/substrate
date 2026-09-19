---
status: accepted
date: 2026-09-16
decision-makers: George Antoniadis
---

# 0092. An orphaned mapping target is marked always and collected only on request

## Context and Problem Statement

A mapping's target is minted out of a source record. When the last live source
goes — the connector deleted the row, or a re-seed purged the mirrors and
minted them under new ids — recompute empties the mapped properties and the
row stays: a husk nothing describes and no probe can ever match. Measured on
Mneme v6's repository after one provider replacement (2026-09-16): **2,727 task
rows** whose `name` and `url` recomputed away, against 2,067 mapped sources, and
**1,838 unreferenced empty persons**
([#578](https://github.com/geoah/substrate/issues/578), mneme-v6 ticket T-009).
[0087](0087-an-unresolved-source-parks-instead-of-minting.md) stopped the main
SOURCE of these rows; it explicitly left the cleanup open, because deleting a
person the engine decided nobody describes is not a default anybody should get
by accident.

## Considered Options

- Leave it to the owner: a script that lists the husks and deletes them
- Delete the target in the write path, the moment its last source goes
- Mark the target as derived state, and collect the mark in the GC sweep
  behind a deployment knob
- A new record kind holding the orphaning, settled like a merge request

## Decision Outcome

Chosen: the third, split so the two halves ship differently. **The mark is
always on; the collection is off unless the deployment names a grace window.**

`records.orphaned_at` is DERIVED STORAGE, the same class as `fts` and
`property_offers`: a function of the live records, set and cleared inside the
recompute that discovers it, never carried by the changelog, and derived again
by a rebuild from the fold it replayed (`internal/engine/orphans.go`). A record
is orphaned when ALL THREE hold:

1. **Something maps onto its kind.** A record of a kind no `recordmapping`
   targets is nobody's projection and cannot be orphaned by one.
2. **No live source.** Not one live record links to it through a mapping's
   subject slot, counted over every id the record has ever had — a source
   synced before its subject won a merge still names the loser.
3. **Nothing above the machine tier holds a property.** Every
   `property_managers` row on the record is `machine`, or there are none.

Deleting a target's last source marks it; re-linking clears it on that
record's next recompute. Nothing has to be cleared by hand, and no state
machine is invented for a fact the live rows already state.

**Condition 3 is the judgement call.** The alternative on the table was
"nothing above machine, OR only writers whose bundle is gone", which would
mark a husk carrying a property some now-uninstalled bundle's function once
pinned — and would have marked Mneme's 2,727 task husks, whose surviving
property is an `externalId` a bundle-tier function wrote. It is rejected
twice over. A bundle's direct write "pins like an owner edit"
(`substrate.Tier`), so making it collectable under some conditions makes the
tier's meaning conditional on a second, invisible fact. And it would turn an
uninstall into a retroactive delete of records the bundle merely touched,
which is the class of surprise
[0096](0096-a-mapping-synthesises-its-subject-slot.md) already refuses when it
declines to clear subject links on an uninstall. The cost is stated: a husk
one bundle-tier write landed on is not marked, and the way to give it back to
the machine is the documented release — a null patch of the pinned property,
after which the next write marks it.

**The collection is a knob, `SUBSTRATE_ORPHAN_GRACE`, unset by default.** With
a positive duration the GC sweep tombstones every marked record whose mark is
older than that window AND that no live record points at (the refs index, over
every former id), and its own fixpoint collects the tombstone. Zero or unset
collects nothing. Off is the default because "the last source went" is also
exactly what a connector outage and a re-seed in flight look like from inside
the engine, and the cost of being wrong is somebody's people. The grace window
is the second guard, not a formality: a `--replace` seed leaves every target
orphaned for as long as the re-import takes.

**The door is `filter.orphaned`** on `GET /api/v1/records` (and
`substratectl get <kind> --orphaned`), so an owner can look at the set,
delete from it by hand, or watch it shrink as a seed lands — on a deployment
that collects nothing.

### Consequences

- Good, because the husks are legible for the first time: one filter answers
  "what did the re-seed leave behind", where before the only way to count them
  was a hand-written SQL script.
- Good, because the destructive half is opt-in and windowed, so a repository
  cannot lose records to an engine decision nobody asked for.
- Good, because the mark needs no lifecycle: it is a reading of the present,
  and a re-link clears it in the same transaction that makes it wrong.
- Bad, because a husk with one bundle-tier property is never marked, which is
  the larger half of the population that prompted the issue. Releasing the
  property is the remedy, and it is a write per record.
- Bad, because the mark's STAMP is not reproducible: a rebuild re-derives the
  fact and stamps it with the rebuild's own clock, so a rebuild restarts every
  grace window. That is the conservative direction — nothing is collected
  sooner than it would have been — and it is why the stamp is not in the fold
  snapshot the rebuild is compared against.
- Bad, because every write onto a mapped kind now pays one indexed read for
  the manager tiers, and a recompute pays one more refs query than it did.
- Bad, because a record with no manager rows at all is vacuously orphaned, so
  a hand-created record of a mapped kind that carries no property whatsoever
  joins the set. It carries nothing to lose, and the collection still asks
  whether anything points at it.

### Confirmation

`internal/engine/orphans_db_test.go`:
`TestDeletingTheLastSourceMarksTheTargetOrphaned` (mark, and the re-link that
clears it), `TestAPropertyHeldAboveTheMachineTierIsNotAnOrphan` (condition 3,
in both directions), `TestTheSweepCollectsAnOldUnreferencedOrphan` (old and
unreferenced goes; young stays; referenced stays) and
`TestTheSweepCollectsNoOrphanByDefault` (the knob is off).

## More Information

Closes [#578](https://github.com/geoah/substrate/issues/578); geoah/mneme-v6
`docs/upstream.md` ask O and ticket T-009. Follows
[0087](0087-an-unresolved-source-parks-instead-of-minting.md), which stopped
the growth and left the cleanup to this record.

Reopen when a bundle needs to say that its own write is derived from the
source rather than pinned — an explicit "this property follows its source"
marker on a write — which is the honest way to bring the bundle-pinned husks
into the set, and a change to what the tiers mean rather than to what an
orphan is.
