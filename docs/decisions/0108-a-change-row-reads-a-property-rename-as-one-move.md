---
status: accepted
date: 2026-09-26
decision-makers: George Antoniadis
---

# 0108. A change row reads a property rename as one move

## Context and Problem Statement

Change values
([0107](0107-a-change-row-carries-before-and-after-values-on-request-derived-at-read.md))
pair before and after by property name. A vocabulary apply that renames a
property (`renamedFrom`,
[0063](0063-a-property-rename-is-ordinary-record-writes.md)) writes one
`patch` per record whose effect sets the new name and clears the old one, so
History read "Label: removed Acme, Display label: added Acme" for a rename
that changed no value (issue #676).

## Considered Options

- Pair at read from the rename entry's own `payload.renamed` (old name to
  new), which convert.go already writes on a rewrite entry that moved a
  renamed value on its record.
- Add a new stored field to the rename entry naming the pairs.
- Leave the pair to the client: the payload is on the row, and the console
  could fold the two changes itself.

## Decision Outcome

Chosen: pair at read from `payload.renamed`. A property change gains
`renamedFrom`: the change is named by the new property, `before` is the value
the record held under the old name (the walk looks for it under the old
name), `after` is the value under the new one, and the old name carries no
change of its own. A pair is taken only where the entry is addressed to the
record and its effect cleared the old name and set the new one, so a payload
that does not match what the fold did pairs nothing. Nothing new is stored,
so every rename already in a history reads as a move. A new stored field was
rejected because the payload already says it, and older histories would not
have it. Leaving it to the client was rejected because every client would
repeat the pairing, and the before the walk derived would still sit on the
old name.

### Consequences

- Good, because a rename reads as one move, with the value it held and the
  value it holds, under the name a read of the record shows now.
- Good, because it needs no stored change and answers every history written.
- Bad, because the read now depends on a payload key the fold ignores
  (`renamed`); a rewrite that stopped writing it would silently read as a
  removal and an addition again. `TestChangeValuesPairARenameAsOneMove` holds
  it.

### Confirmation

`TestChangeValuesPairARenameAsOneMove` and
`TestChangeValuesKeepAFormerSecretRedacted`
(internal/engine/changevalues_db_test.go) hold the paired row, its before and
its redaction; `TestRenamesOfKeepsOnlyAMoveOnTheAddressedRecord`
(changevalues_internal_test.go) holds which pairs are taken.
`wire.golden.json` pins `PropertyChange.renamedFrom`. The console says the
move as "Display label: renamed from Label" (change-values.test.ts and
value-moves.test.tsx in web/console).

## More Information

Amends [0107](0107-a-change-row-carries-before-and-after-values-on-request-derived-at-read.md)
without superseding it.
