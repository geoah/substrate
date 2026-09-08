---
status: accepted
date: 2026-09-08
decision-makers: George Antoniadis (via the issue-151 agent session)
---

# 0063. A property rename is ordinary record writes, never a fold-time conversion

## Context and Problem Statement

`renamedFrom:` entered the dialect as a reservation
([0020](0020-dialect-keys-are-reserved-not-tolerated.md)): admitted, stored
and acted on by nothing, so a rename whose old name live records still carried
was refused like a drop (`schemadiff.go` `typeNarrowings`), and the only way
to rename a property with data was to null the value on every record and lose
it. [Issue #151](https://github.com/geoah/substrate/issues/151) asks for the
rename to land. PR #86 settled the shape of a conversion (ordinary writes in
the changelog, no fold-time upcasting) but closed unmerged and its plan
document was deleted, so the reasoning existed nowhere in the tree. The
choice binds the fold: `fold.go` reads no declaration for data today, and the
first conversion decides whether it stays so.

## Considered Options

- Ordinary record writes at admission: the apply moves every live record's
  value to the new name inside the apply transaction, one record effect and one
  changelog entry per record, through the same fold every write takes
- A fold-time upcast: the fold reads `renamedFrom` off the declaration and
  rewrites the old name to the new one as it replays, so no entry is written
- A new fold effect that names the kind and the two property names once, and
  a rebuild rewrites every row of the kind when it replays it
- Keep refusing, and leave nulling the values by hand as the only rename

## Decision Outcome

Chosen: ordinary record writes at admission (`internal/engine/rename.go`),
because it is the one option under which a fresh replay reproduces the
records without reading a declaration. The fold's containment claim is that
`records` is a function of the changelog alone; an upcast makes it a function
of the changelog and the kind rows, ordered, and a rebuild would give a
different answer for every entry depending on which declaration was live when
the replay reached it. A kind-wide rename effect keeps the changelog
self-contained but adds a second implementation of "what a change does to the
records table" for one operation, with the manager, offer and embedding rows
to move beside it, and its per-record consequences (a version move, a change a
watcher or a trigger sees) would be invisible on the wire. Per-record entries
say exactly what happened to each record, which is what a consumer keeping a
copy from the change feed needs
([issue #377](https://github.com/geoah/substrate/issues/377)).

What the transaction does per record: a Set of the new name and a Del of the
old in one `record` effect, the title re-rendered under the new declaration, a
`patch` entry carrying `properties` and `renamed`, the manager row moved with
its actor, tier, principal and `updated_at` (the effect carries the stamp on
changelog dialect 5, since a dialect 4 binary would decode the key as absent, so
`propertyMeta` keeps saying when the value was last written, and a replay
reproduces it), the offer rows and the embeddings rekeyed, the
old name's queue row dropped and the new name enqueued where it embeds. The
entry's actor is the hand that applied the declaration. Triggers fire per
entry as for any patch; policies are not consulted, because the declaration
write was already authorized at its own chokepoint.

Whatever else the new declaration changes is classified against the old one
under the old name, so a rename that also retypes or adds `required` refuses
with the count exactly as the same change under the old name would. A rename
is refused while a reader of the old name remains: a mapping path (the
candidate compile re-resolves every mapping from or onto the kind), the
kind's own `displayTemplate`, or another kind's template reading through a
reference that can resolve to the kind, whether pinned at it, unpinned
(`kind: any` or none) or pinned at a trait it implements (`renameGuards`).
A rename also takes a name the stored kind does not declare: `size` dropped
and `renamedFrom: size` added to an existing `dimensions` would overwrite
every record's `dimensions` and collide the rows keyed on the two names, and
the loader cannot see the stored kind, so `renameGuards` refuses it; and a
live record already carrying a value under the new name (a tombstone restored
after the name was dropped) refuses with the count, as every narrowing does,
so the move never replaces a value. The template guard reads declarations and
never stored values, so it is conservative: an unpinned reference no value
points at the renamed kind still refuses. What no guard reads is CEL: a
trigger's or a policy's guard expression naming the old property compiles
against the record it runs on, not against a declaration, and the door does
not parse CEL for property names, so rewriting such a guard stays the
author's job. `body`
never renames, in either direction, because it is a column and not a key of
`props`.

The boot upgrade of the shipped tree converts at open, in the transaction that
projects the declaration, under the system actor. The other two doors were
worse: projecting the new declaration and leaving the rows under the old name
is the disagreement the upgrade guards exist to prevent, and skipping forever
is a rename no repository ever receives. The boot has no candidate registry of
its own, so for a rename it builds one (`shippedCandidate`: the stored
registry with the upgraded packages replaced by the shipped declarations) and
compiles it, so a stored package the tree does not ship, whose mapping or
template reads the renamed property, refuses the boot as it would refuse the
apply, instead of being quarantined at the next load.

The marker stays on the stored declaration once the rename landed. Nothing
acts on it again, because no live record carries the old name, and
[0055](0055-a-retired-name-is-declared-and-never-inferred-from-a-prune.md)
reconciles it with retirement.

### Consequences

- Good, because the fold still reads no declaration and `RebuildRepository`
  reproduces renamed records byte for byte from the changelog alone.
- Good, because every consumer of the changelog sees each record change as
  the patch it was, with the record's `version` moved.
- Bad, because a kind with N records carrying the old name appends N entries
  and rewrites N rows in one transaction, under the vocabulary write mutex
  and the exclusive registry-dependency lock, and nothing caps it. The bound
  is stated rather than enforced.
- Bad, because every renamed record's `version` moves, so a client holding an
  `ifVersion` across the apply is refused on its next write.
- Bad, because triggers fire once per renamed record.
- Bad, because a mapping in another package whose path reads the renamed
  property cannot be rewritten before the rename lands (its path type-checks
  against the stored declaration) and the rename refuses while it stands: the
  way out is to delete the mapping, land the rename, and declare it again,
  which loses nothing but costs the author two steps. Rewriting the path in
  the same transaction would be a declaration write into another package,
  which this record does not take on.
- Bad, because [0020](0020-dialect-keys-are-reserved-not-tolerated.md) says
  `renamedFrom` is acted on by nothing and its body is frozen; this record
  carries the correction, and 0020's decision (reserve by name, no tolerated
  prefix) stands.

### Confirmation

`TestRenameMovesTheValueOfEveryLiveRecord` (`internal/engine/rename_db_test.go`)
holds the values, the manager, offer and queue rows (the manager's stamp
included), one entry per record and the identical rebuild;
`TestRenameRefusesAReaderOfTheOldName`,
`TestRenameRefusesADestinationTheStoredKindDeclares` and
`TestRenameThatAlsoNarrowsRefusesWithTheCount` hold the refusals;
`TestBootUpgradeConvertsAShippedRename` and
`TestBootUpgradeRefusesARenameAStoredTemplateReads`
(`upgrade_guard_db_test.go`) hold the boot door; `TestRenamedFromReserved`
(`internal/vocabulary/vocabulary_test.go`) holds the loader's shape rules.

## More Information

Backfill from `default:` and a lossless enum remap are
[issue #152](https://github.com/geoah/substrate/issues/152)'s; they are
expected to take the same shape (writes at admission), and a shape that could
not is the trigger for reopening this record. The other trigger is a kind whose
live count makes one transaction per rename unaffordable, which would need a
bound or a batched conversion this record does not provide.
