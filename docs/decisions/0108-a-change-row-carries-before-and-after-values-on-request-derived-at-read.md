---
status: accepted
date: 2026-09-26
decision-makers: George Antoniadis
---

# 0108. A change row carries before and after values on request, derived at read

## Context and Problem Statement

The console's History reads the change feed and could only say which
properties an entry touched ("changed Priority"), because
[0061](0061-a-change-event-names-the-affected-records-and-clients-fetch-them.md)
put no value on a public row and left "optional event detail (per-record
values, ops, redaction policy)" to a later record. The owner wants History to
say what changed ("Priority: High → Urgent", "+ grace@example.com"). An
entry's stored effects already hold what each property became
(`rowDelta.set`, `del`); nothing stores what it was.

## Considered Options

- Derive the before at read: walk the record's earlier entries newest first
  to the last one that set or cleared the property, and project the after
  from the entry's own effects. Opt-in per read.
- Record the before at write: `diffRow` holds the loaded row, so the delta
  could carry a `was` map beside `set`. The read becomes a projection.
- Keep names only and let a client fetch past versions. There is no read of a
  past version, so this is the status quo.

## Decision Outcome

Chosen: derive the before at read, on `GET /api/v1/changes?values=1`, into
`affected[].properties: [{name, before, after, beforeUnknown}]`. It needs no
change to what is stored, so every entry already written answers too, and the
before is read from the changelog itself, so it cannot disagree with the fold
the way a stored copy could. Recording at write was rejected because it
doubles what every update stores for a value history already holds, and still
answers nothing for the history written before it.

The values are a projection in the record read's own terms, not the delta
grammar 0061 kept off the wire: property names as a read of the record shows
them (the property map, a non-templated `title`, a declared `body`, the three
instants, the machine states), and a sensitive property reads `<redacted>` on
both sides exactly as `redactProps` renders it. A kind that is no longer
declared carries no values. A storage spelling can still change without an
API change, which is what 0061 protected; this amends 0061's "no value travels
on a public row" for reads that ask, and leaves `affected` and the rest of it
standing.

The walk is honest about what it cannot see. It stops at the record's
creation (no value before it). It reads the record's own entries and any
merge or split naming it, and checks the record's versions run unbroken; a
gap, an entry written before entries held values, a history with no creation,
or a previous write further back than 1024 entries reads `beforeUnknown`,
never a guess.

### Consequences

- Good, because every history already on disk gets values, with no dialect
  change and no new stored field.
- Good, because the before is the changelog's own answer, recomputed each
  read, so the truth rule holds by construction.
- Good, because a client that does not ask pays nothing and sees the same rows.
- Bad, because a page with values costs one indexed read per round per record
  on it, and a record written thousands of times since a property last moved
  answers `beforeUnknown` past the bound.
- Bad, because an effect on a record that rode an entry addressed to neither
  it nor its merge pair, and is not on the page, is found only as a version
  gap, and the before reads unknown where a stored copy would have known it.
- Bad, because values now travel on the feed and its watch stream when asked,
  so redaction has to follow the record read's rules on one more surface.

### Confirmation

`TestChangeValuesCarryBeforeAndAfterAcrossPutPatchDeleteAndRestore` and
`TestChangeValuesWalkBackAcrossAMergeAndItsSplit` hold every derived value to
the record read at the versions on either side of it, one-row pages included;
`TestChangeValuesRedactASecretLikeARead` holds redaction
(internal/engine/changevalues_db_test.go). The walk's refusals to guess are
unit tests in changevalues_internal_test.go. `wire.golden.json` pins
`AffectedRecord.properties` and `PropertyChange`.

## More Information

Amends [0061](0061-a-change-event-names-the-affected-records-and-clients-fetch-them.md)
without superseding it. Reopen if the bound bites in practice: a stored `was`
beside `set` is the other option above, and would still need this read for
older history.
