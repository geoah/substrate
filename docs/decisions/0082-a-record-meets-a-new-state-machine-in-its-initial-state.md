---
status: accepted
date: 2026-09-15
decision-makers: George Antoniadis
---

# 0082. A record meets a new state machine in its initial state

## Context and Problem Statement

Binding a `state` property onto a kind that already holds records left every
one of those records permanently outside the machine
([#545](https://github.com/geoah/substrate/issues/545)). The declaration was
admitted as pure widening — a state takes no `default:`, so no conversion step
ran and nothing was written into the rows' `states` — and the write path then
refused every door out: the transition guard read the absent state as `""`, no
declared transition leaves `""`, and a put may not move a state at all. A
creation is born in the declared initial state; a record that predates the
machine was born nowhere, and the only ways forward were deleting and
rewriting the rows or never declaring the machine. That is the general shape of
"add a state machine to a kind that has data", so it blocked any vocabulary
that grows a machine after its first record.

## Considered Options

- Backfill the machine's `initial` onto every record standing outside it at
  admission, as ordinary record writes, on every door.
- Treat an absent state as `initial` inside the transition guard: `"" → quiet`
  a no-op, `"" → raised` the declared `quiet → raised`.
- Leave it: require a machine to be declared before the kind's first record,
  and migrate by deleting and rewriting rows otherwise.

## Decision Outcome

Chosen: the backfill. Admitting a declaration whose kind declares a machine
some live record holds no state for writes that machine's `initial` onto every
such record, inside the admission's transaction, as ordinary record writes —
one `patch` changelog entry per record carrying `backfilled: [<state>]`,
through the same fold every write takes, exactly as a `default:` beside
`required:` backfills ([0066](0066-a-backfill-and-an-enum-remap-are-ordinary-record-writes.md)).
It runs on all three doors that share the conversion path: `/vocabulary/apply`,
the catalog import and upgrade, and the shipped boot upgrade. It is counted on
the conversion plan and both previews as its own step, `enter`, rather than as
a backfill, because a machine's `initial` is not a `default:` and "backfilled
with its default" would name a key the declaration does not have. It loses
nothing, so it needs no confirmation
([0067](0067-a-lossy-conversion-runs-only-with-a-confirmation-bound-to-its-preview.md)).

The guard is unchanged: it still refuses a transition out of an absent state.
After this, an absent state on a declared machine is a bug, not a position —
which is why the step is classified for EVERY machine the candidate declares
and not only for the ones a diff adds. A machine every record already stands in
costs one count and is not a step, and a repository stranded by an earlier
binary is repaired the next time its declaration is admitted.

The guard-only option was rejected because it puts the fold and the guard at
odds: the row's `states` still holds nothing, so a read, a `states`-keyed
filter and an index answer "no state" while the guard answers "initial", and
every consumer has to know the declaration to reconcile them. It also repairs
nothing on disk, so the same record would have to be special-cased forever, and
a repository that upgraded past the bug would stay half-fixed. Refusing to
admit the machine instead (the count a narrowing takes) was never on the table:
the change strands nothing, there is a value that cannot be wrong, and the
substrate's answer to "a widening the rows must follow" is already a
conversion.

### Consequences

- Good, because a machine can be added to a kind with data: the records enter
  it, the first transition on an old row lands, and nothing has to be deleted.
- Good, because the state lands as values in the changelog, so a rebuild and a
  fresh import reproduce it without reading the declaration that wrote it.
- Good, because the plan says what will happen before it happens, on the same
  preview, hash and work ceiling every other conversion uses.
- Bad, because adding a machine to a kind with more live records than the
  deployment's conversion ceiling (`SUBSTRATE_CONVERSION_CEILING`, 10000 by
  default) is refused, and the refusal's advice ("declare the new shape beside
  the old one") does not fit a state: the operator raises the ceiling for the
  apply instead.
- Bad, because every machine the candidate declares costs one count per kind on
  each apply and preview, whether or not anything is outside it.
- Bad, because the changelog entry says `backfilled` while the plan says
  `enter`: two words for one step, kept because the changelog key means "a
  value the record was missing was filled in" and the plan's word has to be
  honest about where the value came from.

### Confirmation

`TestStateEntryEntersEveryRecordThatPredatesTheMachine` in
`internal/engine/convert_db_test.go` (the apply door: the preview's count, the
entered records, the transition that follows, the one entry per record, the
idempotent second admission and the replay) and
`TestBootUpgradeEntersAShippedStateMachine` in
`internal/engine/upgrade_guard_db_test.go` (the boot door).

## More Information

The step is `substrate.StepEnter` on the wire, counted in
`conversionPlan.wire` and performed in `convertRecord`
(`internal/engine/convert.go`). The transition guard it feeds is in
`internal/engine/write.go`.
