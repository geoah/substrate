---
status: accepted
date: 2026-09-08
decision-makers: George Antoniadis (via the issue-384 agent session)
---

# 0055. A retired name is a declared reservation, never inferred from a prune

## Context and Problem Statement

Removing a declaration leaves no trace once no live row carries it. A kind
name, a property, an enum value or a state that is dropped today can be
declared again tomorrow for another concept, and history, integrations and
tombstoned rows that carry the old meaning point at the new one
(`typeNarrowings` walks the current property order only;
`droppedTypeGuards` counts live rows only; `pruneSchemaRows` tombstones the
declaration row and `gcPass` purges it). Record
[0020](0020-dialect-keys-are-reserved-not-tolerated.md) left the marker out
of its batch because its shape was unsettled, and
[issue #384](https://github.com/geoah/substrate/issues/384) asks for the
shape and for what it binds. Three questions need an answer that code can
hold: what a retirement is and where it lives, whether a kind may be dropped
while tombstoned records of it remain, and what a rehomed sample import
([0048](0048-providers-are-published-samples-are-copied.md)) does when it
collides with a local retirement.

## Considered Options

- An explicit `retired:` block on the declaration document: kind names on
  the package header, properties, enum values and states on the kind
- Every prune writes a retirement: a name removed from a closure is spent
- A side table of retired names, written by the prune transaction
- Nothing: a dropped name may return, and history orphans by design

For tombstones: refuse a kind drop while tombstoned records exist; or admit
it and let retirement close the hole.

For the rehomed import: refuse on collision; or exempt it because the
closure arrives under another spelling.

## Decision Outcome

Chosen: an explicit `retired:` block, on the declaration document, at two
levels. The package header reserves kind names (`retired: {kinds: [widget]}`).
A kind reserves its own property names, enum values by property and states by
property (`retired: {properties: [size], values: {level: [low]}, states:
{phase: [archived]}}`). The block is validated at load (a name that is both
declared and retired refuses the document; every entry holds to the rule its
live counterpart holds to), stored on the row like every other declaration
key, and read back from it, so `RebuildRepository` and `get -o yaml` carry it.

Retirement is an act the author performs by writing the name into the list.
A prune without it is what it is today: the guards refuse it while live rows
hold the old shape, it admits otherwise, and the name may be declared again.
An author who wants a name spent says so; the engine never infers permanence
from a removal, because a rename in flight, a mistaken apply and an uninstall
that will be reinstalled all look like prunes.

A retirement is permanent and lives as long as its package or kind. The
engine carries a stored retirement into every later document of the same
package or kind before it resolves versions, so a document that omits the
list does not lift it (the apply verb merges and never prunes). On the boot
upgrade, where the shipped tree is the candidate and nothing merges, a tree
that drops a stored retirement refuses the upgrade like any other narrowing.
Removing a package whole removes its reservations with it: the package name
itself is not reserved here.

A retired name refuses on every admission door with the same sentence: the
apply verb, `InstallBundleClosure` (which the catalog's install and import
ride), the boot upgrade of the shipped tree and the upgrade preview. The
shipped tree is held by `kinds:check` as well, because a tree that reuses or
un-retires a name would otherwise be refused only at the boot of somebody's
repository. `deprecated` keeps a name usable; `retired` does not. Whether a
later `renamedFrom` pointing at a retired name may lift the refusal is
[issue #151](https://github.com/geoah/substrate/issues/151)'s to decide, and
until it does the answer is no.

A kind drop is not refused for tombstoned records. Two shipped flows delete
the data and then drop the kind or uninstall the bundle, and the collector
sweeps every five minutes, so counting tombstones would turn "delete, then
uninstall" into a wait with nothing to do. Tombstones carrying a stale shape
is already the stated posture of every narrowing guard (the counts
deliberately do not see them), and a dropped kind is not special. The hole
the review named, a resurrected tombstone inheriting rows under a reused
name, is what retirement is for: a retired name is never declared again, so
its tombstones never resurrect under another meaning.

A rehomed sample import that collides with a local retirement refuses. The
rehomed closure is declared under the repository's own authority, which is
the authority the retirement was written under, so it is the same door and
the same reservation. Rehoming changes the spelling of the authority, not
who owns the name.

Names removed before this record lands carry no reservation. There is no
backfill: the engine cannot tell a rename from a retirement in a changelog
that recorded neither.

The side table was rejected because a marker outside the changelog does not
survive `RebuildRepository`, and every-prune-retires was rejected because it
spends names nobody meant to spend. Doing nothing leaves the defect.

### Consequences

- Good, because a retirement is a declaration like any other: it rides the
  changelog, survives a rebuild, exports with the kind and needs no new
  storage.
- Good, because the four doors refuse the same change with the same
  sentence, and the tree is refused before it ships.
- Good, because the existing prune-and-return behavior is untouched: nothing
  a repository could do yesterday is refused today unless its author wrote
  `retired:`.
- Bad, because a retirement is irreversible by design. A typo in the list
  spends a name, and the only recourse is another name.
- Bad, because a sample package whose kind name the repository retired under
  the same package can never be imported. The import rehomes the authority
  and keeps the package name, so the collision has no way around it.
- Bad, because an unretired drop still leaves tombstones a reused name could
  resurrect, for up to one gc sweep. The message on the drop names
  `retired.kinds` so the author can choose.
- Bad, because retirement covers top-level property names, enum values and
  states only. An object's nested fields, a reference's link properties, and
  traits, property types, functions and agents have no reservation; each is
  a later addition to the block.

### Confirmation

`TestRetiredReserved` (internal/vocabulary/retired_test.go) holds the
loader's validation. `TestRetirementRefusesOnEveryDoor`
(internal/engine/retirement_db_test.go) retires a kind, a property, an enum
value and a state, rebuilds the repository, re-declares each on the apply
door, the install door and the upgrade preview, and moves a property whose
value was retired to another datatype and back;
`TestBootUpgradeRefusesARetiredName`
(internal/engine/upgrade_guard_db_test.go) does the same on the boot door,
and `TestImportRefusesARetiredKindName` (internal/catalog/retired_db_test.go)
on a rehomed sample import.
`TestSchemaEvolutionAdditiveAdmits` (internal/engine/evolution_db_test.go)
holds that a removed but unretired value returns and a retired one does not.
`TestRetiredNameReuseIsRefused` (cmd/vocabularydiff/main_test.go) holds the
tree.

## More Information

This is the marker [0020](0020-dialect-keys-are-reserved-not-tolerated.md)
deferred. Purged record ids are the same defect one level down and belong to
[issue #372](https://github.com/geoah/substrate/issues/372). Reopen trigger:
a rename mechanism (#151) that needs to read a retired name, or a need to
retire a nested field, a link property or a non-kind declaration.
