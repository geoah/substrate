---
status: accepted
date: 2026-09-08
decision-makers: George Antoniadis
---

# 0060. A record carries the kind version that last wrote it

## Context and Problem Statement

A record's `version` counts its edits. Nothing said which version of the
kind's declaration validated the properties it holds: not the `records` row,
not the `record` delta in the changelog, not the wire
([#146](https://github.com/geoah/substrate/issues/146)). Admission refuses
narrowing and never backfills, so every stored value is valid under the live
declaration and a conversion that has to treat values written under two
declarations differently (a property rename, a remap) cannot tell them apart.
A kind's effective version is its own pin, else its package's
(`internal/vocabulary/load.go`), and it is known at every write as
`applySpec.ty.Version`.

## Considered Options

- One stamp per record: the effective kind version at the write that last
  moved the row, as a `records` column and a key on the `record` delta.
- One stamp per property, so a rename can tell which properties were written
  under which declaration.
- A pair per record, the kind's own version and its package's, so a package
  move is visible even where the kind pins.
- Derive the stamp at replay from the `core/kind` declaration row the
  changelog holds before each data write, storing nothing.
- Move the stamp on every fold of the row, an identical re-put included.

## Decision Outcome

Chosen: one stamp per record, the kind's effective version, moved only by a
write that moves the row. `records.kind_version bigint NOT NULL DEFAULT 0`
(migration `0021`) holds it; the writer sets it on the row it produced
(`apply` from `sp.ty.Version`, merge and split from the kind they resolved);
`diffRow` carries it as `kindVersion` on the `record` delta when it moved;
`applyTo` restores it; `upsertRecord` writes it whenever any other column
moves and leaves the row alone otherwise. Nothing recomputes it from the live
registry, so a rebuild stamps each row with the version that wrote it. It is
served as `kindVersion` on `substrate.Record` and in the CLI's `status` block,
omitted while it is 0.

Per property was rejected because the delta already names the properties
that moved, so a per-property stamp is the record stamp joined with the
changelog, at a fraction of the cost. The pair was rejected because the
effective version is the one the loader applies, and a package move that
leaves a pinned kind's declaration unchanged changed nothing about the
record. Deriving at replay was rejected because the fold is deliberately
schema-free about data (`fold.go`): every decision the declaration made at
write time is in the delta as a value, and the stamp is one more. Moving the
stamp on every fold was rejected because an identical re-put must stay silent
(no edit-counter bump, no changelog entry), and a stamp that moved alone would
either break that or leave the row saying one thing and the changelog another.

`0` is the absent stamp, the spelling
[0002](0002-declaration-versions-are-integers.md) gave declaration versions:
every row written before the column and every replayed entry whose delta
carries no `kindVersion` reads 0, and nothing reconstructs the history. A
writer that resolved no declaration leaves the stamp where it was; no delta
ever carries a stamp of 0.

The key is not a changelog dialect bump. `foldOpsOf` decodes without
`DisallowUnknownFields`, so a binary older than the key replays the entry,
drops the stamp and folds the row to 0: the same reading it gives every entry
older than the key, and neither a refusal nor a misread value. The dialect
rung (`changelogdialect.go`) stays at 2.

### Consequences

- Good, because a later conversion can ask each record which declaration
  validated it, and the answer survives a rebuild without consulting the
  registry.
- Good, because the stamp costs one column and one optional key, and the
  identical-re-put invariant holds unchanged.
- Bad, because history older than the stamp is 0 forever; a record nobody
  writes again never learns its version, and a conversion has to treat 0 as
  "unknown, assume the oldest".
- Bad, because a label-only or annotation-forced write re-validates the
  properties and moves the stamp to the current version without a property
  moving. That is the honest reading (the row was written under that
  declaration), but a reader expecting the stamp to move only with the
  properties will be surprised.
- Bad, because the stamp rides the raw delta the `/changes` feed serves, and
  that payload is not a client contract
  ([#377](https://github.com/geoah/substrate/issues/377)); it is documented
  on the record and nowhere else.

### Confirmation

`TestARecordCarriesTheKindVersionThatWroteIt` (`internal/engine/kindversion_db_test.go`)
writes under two versions of one kind, checks the identical re-put stays
silent, and rebuilds the repository against the fold snapshot, which now
includes `kind_version`. `TestKindVersionIsAValueTheDeltaCarries`
(`internal/engine/fold_internal_test.go`) pins the three spellings: carried
when moved, absent means unchanged, never 0. `TestWireGolden` and the console's
`wire.golden.test.ts` hold `kindVersion` on the wire.

## More Information

[0020](0020-dialect-keys-are-reserved-not-tolerated.md) cites #146 for the
retirement markers (`reserved` lists of retired property names, enum values
and kind names). That work moved to
[#384](https://github.com/geoah/substrate/issues/384); 0020's body is frozen,
so the correction lives here. This record is a prerequisite for the rename and
remap of [#151](https://github.com/geoah/substrate/issues/151) only if that
implementation reads per-record stamps.

Revisit if a conversion needs to tell properties of one record apart by the
declaration that wrote each: that is the per-property option, and it would be
a new column and a new delta key beside this one, not a change to it.
