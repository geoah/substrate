---
status: accepted
date: 2026-09-29
decision-makers: George Antoniadis (via the issue-434 agent session)
---

# 0144. A restoring put completes the conversions a tombstone missed

## Context and Problem Statement

A conversion (a rename, a remap, a backfill, a null step) rewrites live
records only, and the narrowing guards count live records only
([0063](0063-a-property-rename-is-ordinary-record-writes.md),
[0066](0066-a-backfill-and-an-enum-remap-are-ordinary-record-writes.md),
[0067](0067-a-lossy-conversion-runs-only-with-a-confirmation-bound-to-its-preview.md)).
A record tombstoned before such an apply keeps its old shape, and a `put`
onto it merges that shape back: it returns holding a spelling or a property
the kind no longer admits, or the restore is refused for a required value it
lacks. 0066 names the gap as a Bad consequence and leaves the choice to
[issue #434](https://github.com/geoah/substrate/issues/434). PR 771 already
made a restoring `put` drop the states of machines the kind no longer
declares.

## Considered Options

- The restoring `put` rewrites the stored row into the shape the kind declares
  now, before the writer's properties merge in
- Every conversion also rewrites tombstoned rows, and the guards count them
- Keep the posture, and document the hand repair

## Decision Outcome

Chosen: the restoring `put` completes the conversions (`internal/engine/restore.go`,
called from `apply` for `resurrect` alone). Against the declaration in force
it moves a value under a property's `renamedFrom:` name, respells a value
holding an enum value's `renamedFrom:` spelling, removes a value under an
undeclared name or in a shape the declaration's coercion refuses (a sensitive
property is not judged, since its stored form is a ref), and fills a
`required:` property's `default:` where the row holds no value. A name the
writer's `put` names is left to the merge. The manager, vector and sealed
rows follow each step as a conversion's do. The one restoring entry carries
the result as values in its delta, and its payload names the steps under a
conversion's keys (`renamed`, `remapped`, `backfilled`, `nulled`), so
`RebuildRepository` replays the same row without reading a declaration, and
the values read pairs a rename as 0114 requires.

It beat converting tombstones because it writes history only for a record
somebody restores, and the tombstones the collector purges within minutes
cost nothing. Converting them would append a `patch` entry per tombstone that
every record trigger watching the kind receives, would count tombstones into
the work ceiling and into a lossy plan's hash, and would still miss the
narrowings the guards admit because only tombstones held the value (a removed
enum value, a retype), which the restore catches through the same coercion a
write runs. Documenting the hand repair leaves every restore a refusal or a
record whose next full `put` is refused.

### Consequences

- Good, because a restored record reads back in a shape the kind admits, and
  its `get -o yaml` applies back unchanged.
- Good, because the fix lives in the one entry that restores the record, and a
  rebuild and an import reproduce it from the changelog alone.
- Bad, because the declaration is the only history read: a record tombstoned
  before two renames of one name, or two respellings of one value, holds a
  name or a spelling the current declaration no longer mentions, and the
  restore removes that value rather than following the chain.
- Bad, because a value is judged by today's coercion alone: a stored value it
  refuses is removed, lossy and unconfirmed, although no preview counted it.
  The old value stays in the changelog.
- Bad, because a split resurrects a merged-away loser through its own path
  (`merge.go splitIf`), which neither this record nor PR 771 reshapes.
- Bad, because a backfilled value on a restore is managed by the restoring
  actor, not by whoever applied the declaration that required it.

### Confirmation

`TestRestoreTakesTheSpellingARemapMovedWhileTheRecordWasATombstone`,
`TestRestoreFillsTheDefaultARequiredPropertyGainedWhileTheRecordWasATombstone`
and `TestRestoreRemovesADroppedPropertyAndMovesARenamedOne`
(`internal/engine/restore_db_test.go`) hold each step, the side rows, the
payload keys, the values read and the identical rebuild and import. Each fails
with the reshape disabled.

## More Information

This amends 0066's Bad bullet on tombstones and 0063's posture for a renamed
property on a tombstone; both records stand otherwise. Reopen if a restore
must follow a rename chain, which needs the declaration history the fold
never reads.
