---
status: accepted
date: 2026-09-08
decision-makers: George Antoniadis (via the issue-385 agent session)
---

# 0066. A backfill and an enum remap are ordinary record writes, composed with the rename

## Context and Problem Statement

Two declaration changes were refused whenever live records stood in the way,
with no route through but rewriting the records by hand: adding `required:` to
a property some records lacked (a declared `default:` seeded creates and
nothing else), and removing an enum value some records held.
[Issue #385](https://github.com/geoah/substrate/issues/385) asks for a
backfill and a lossless remap to land the way
[0063](0063-a-property-rename-is-ordinary-record-writes.md) landed the rename.
Three things had to be chosen: how each is declared, what happens when a
record is touched by more than one of them, and where the line between a
lossless and a lossy change sits.

## Considered Options

- Ordinary record writes at admission, the shape 0063 chose for the rename,
  with the three steps composed into one rewrite per record
- Each step as its own pass over the kind, one entry per record per step
- A fold-time conversion that reads the declaration during replay
- Keep refusing, and leave the hand-rewrite as the only route

For the declaration grammar:

- Backfill: `required: true` beside a `default:` is the trigger, no marker
- Backfill: a separate marker (`backfill: true`, or a literal) on the property
- Remap: `renamedFrom:` on a value entry (`{value: done, renamedFrom:
  complete}`), the property-level key's twin
- Remap: a property-level map (`remap: {complete: done}`)

## Decision Outcome

Chosen: ordinary record writes at admission (`internal/engine/convert.go`),
composed. Every step a batch declares against one kind runs in one pass over
that kind's live records, in id order: renames first, then backfills, then
remaps, so a backfill and a remap read a property under the name the
candidate declares. A record any step touches is rewritten once: one record
effect, one `patch` entry whose payload names the properties and says which
step moved them (`renamed`, `backfilled`, `remapped`). The fold replays the
effects and never the keys, so a rebuild and a fresh import reproduce the
converted records without reading a declaration, as 0063 requires, and the
entry needs no changelog dialect rung: a binary before this one reads it as
the patch it is. A pass per step would append up to three entries per record
for one apply and move the record's `version` three times; a fold-time
conversion is the option 0063 already rejected.

**A backfill needs no marker.** The trigger is a property that becomes
`required:` (or is added as one) with a `default:` beside it, while live
records hold no value for it, the key absent or an empty value (`""`, `[]`,
`{}`, `null`), exactly the rows the write path would refuse `required` on.
The declaration already says what every record must hold and what a write
that names nothing stores; a record without the value is what a create
without the property would have been, so the default is the one value the
declaration itself nominates. A second key could only agree with the first or
contradict it. A `default:` alone still rewrites nothing: it seeds creates.
`required:` without a default keeps refusing with the count, and the message
now says to declare one. The backfilled value is coerced as a create's default
is and stored under the actor that applied the declaration, at the
transaction's tier, so its manager row reads as a create's would.

**A remap is `renamedFrom:` on the value entry**, reserved by name as
[0020](0020-dialect-keys-are-reserved-not-tolerated.md) requires: the loader
admits it (`enumValueKeys`), so a binary older than the key refuses the
closure at load, and adding it is the coordinated event every new key is.
Core's `propertytype` declares its `values` as a typed object, so the field
is added there too, at version 11. The loader holds the shape: a lowercase
word, not the value itself, not a value the list still declares, no two
values naming one previous value; the key is refused inside an object's
`fields:` and on a link property, where the engine never rewrites a value. The
engine rewrites the old spelling in a scalar, a list and a keyed map. A
property-level map was the other spelling, and it would have put the rename
of a value somewhere other than beside the value, unlike the rename of a
property. A remapped value keeps its manager row: the spelling moved, not who
wrote it. Offer rows describe what a mapping's source would write and are left
alone by both steps; the rename rekeys them because they are keyed by the
renamed property.

**Lossless is judged by declaration.** A remap whose target is a value the
stored declaration still admits would make the records holding either
spelling one set, and nothing here may discard a stored distinction. It is
refused without a count, under `substrate.ErrLossyConversion` wrapped with
`ErrGuard`, so the wire answers the guard code and a caller can still tell it
apart. A value renamed onto a value the stored set did not have is injective
by construction: the loader forbids two values taking one, the retained values
map to themselves, and the target is new. The boot upgrade of the shipped
tree never runs a lossy step: it refuses and reports the line through
`GET /api/v1/vocabulary/upgrade`, as it does every other guard. The
confirmation that would admit a lossy plan, and the preview of every step
with its count, are [issue #152](https://github.com/geoah/substrate/issues/152)'s.

The boot upgrade converts at open, in the transaction that projects the
declaration, against `shippedCandidate`, exactly as it converts a rename.
The upgrade preview still lists renames alone (`renames` on the upgrade
plan); a backfill or a remap shows up there only through its absence from the
blockers, until #152 gives every step a line.

### Consequences

- Good, because a kind can gain a required property, and an enum can respell
  a value, while records exist, through both doors, and a fresh replay
  reproduces the result byte for byte.
- Good, because a record touched by a rename, a backfill and a remap in one
  apply moves its `version` once and appends one entry.
- Bad, because the bound 0063 states is now the bound of three steps: a kind
  with N records any step touches costs N entries and N row rewrites in one
  transaction, under the vocabulary write mutex and the exclusive
  registry-dependency lock, and nothing caps it.
- Bad, because a backfill is implicit: an author who adds `required:` and a
  `default:` to a kind with thousands of records gets thousands of entries
  without a preview telling them so, until #152.
- Bad, because a value renamed onto a retained value cannot be expressed at
  all today, even where no record holds the old spelling; the author drops the
  value instead, which the guard admits once nothing holds it.
- Bad, because 0063's More Information attributes the backfill and the remap
  to #152 and expects them to take this shape; its body is frozen, so this
  record carries the correction: they are #385's, and they did.

### Confirmation

`TestBackfillWritesTheDefaultOntoEveryRecordLackingAValue`,
`TestRemapRewritesEveryRecordHoldingTheOldValue`,
`TestRemapOntoARetainedValueIsRefusedAsLossy` and
`TestRenameBackfillAndRemapComposeIntoOneEntryPerRecord`
(`internal/engine/convert_db_test.go`) hold the values, the manager and queue
rows, the one entry per record, the named refusal, and the identical rebuild
and import; `TestBootUpgradeConvertsAShippedBackfillAndRemap` and
`TestBootUpgradeRefusesAShippedLossyRemap`
(`internal/engine/upgrade_guard_db_test.go`) hold the boot door;
`TestEnumValueRenamedFromReserved` (`internal/vocabulary/vocabulary_test.go`)
holds the loader's shape rules.

## More Information

Reopen triggers: a conversion that cannot be expressed as record writes at
admission (a `null` step, a value computed from other properties), which
would need a step grammar this record does not provide; a lossy remap an
owner confirms, which is #152's and must judge lossiness over the whole plan;
and a live count that makes one transaction unaffordable, the bound 0063
already names.
