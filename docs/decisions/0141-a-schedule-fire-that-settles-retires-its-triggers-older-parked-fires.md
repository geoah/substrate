---
status: accepted
date: 2026-09-29
decision-makers: George Antoniadis
---

# 0141. A schedule fire that settles retires its trigger's older parked fires

## Context and Problem Statement

A schedule trigger's parked fire stayed in `trigger_failures` after every
later fire of the same trigger settled `ok`
([#746](https://github.com/geoah/substrate/issues/746)). On one repository
`github-scheduled` held 110 parked fires from before a fix, the console read
"110 runs failed and are waiting to be tried again" and kept the provider at
Needs attention, and the only offered fix re-ran 110 full drains for no new
data. [0091](0091-a-parked-delivery-is-retried-or-forgotten.md) made retry
and forget the two ways a row ends, so nothing else could clear them.

## Considered Options

- A schedule fire that settles retires the trigger's parked fires at or
  before its occurrence
- `GET /api/v1/sync/status` and the trigger status count only the parked
  fires newer than the trigger's last `ok` fire, and the rows stay
- Leave it to `retry-parked` and `forget`

## Decision Outcome

Chosen: the settling fire retires them. A schedule fire carries nothing a
later fire does not carry again: its envelope is the occurrence and the
trigger's arguments, and its body reads the repository and the upstream as
they stand when it runs. Once a later fire of the same trigger has settled,
retrying the parked one repeats that work. Counting around the rows would
fix the number and leave `…/parked`, the retry button and a restore holding
110 rows that nobody should retry.

The rule has four limits, each because the parked row carries work no later
fire repeats. A record trigger's park is one record's change and is never
retired this way. A webhook request's park carries its request. A fire id
that is not a schedule occurrence (a wake) is not an occurrence to compare.
An agent claim, in flight or interrupted, is left to a person, as
[0064](0064-trigger-bookkeeping-is-a-delivery-ledger-folded-from-the-changelog.md)
leaves it, because its run may have written records.

A settling fire is a dispatched occurrence or a parked one retried by hand;
either retires the parks at or before its occurrence and none after it. The
rows are deleted through the same unpark fold a delivered retry writes, on
the settling fire's own delivery entry, so a rebuild and a restore agree.
`trigger_failures` has no state column to mark, and the parked run rows and
the changelog keep the history. A row a hand is retrying at that moment
keeps its retry.

This amends 0091: retry and forget are still the two verbs, and a settled
later occurrence is a third way a schedule park ends. 0091's rejection of
age-based expiry stands. This rule reacts to a delivery that did the work,
never to a clock.

### Consequences

- Good, because a recovered scheduled sync reads as recovered at its next
  `ok` fire, with no action and no repeated drains. Rows parked before this
  change clear the same way.
- Good, because every reader (`GET /api/v1/sync/status`, the trigger status,
  `…/parked`, the console's retry) agrees, since the rows are gone and no
  reader needs a filter.
- Bad, because a schedule body whose work depends on its occurrence (a digest
  of one given day) loses the parked day without a retry. Such a body reads
  its parks as lost once a later fire settles.
- Bad, because the parked row's error is gone from `…/parked`. It survives
  in the fire's `parked` run row, which run retention never prunes, and in
  the changelog.

### Confirmation

`internal/engine/schedulesupersede_db_test.go`: a function schedule parks
three occurrences, the middle one settles and retires the first two while
the later park stays, the last settles and the trigger status and sync
status read 0 parked, and a rebuild reproduces the fold. A retry by hand of
the middle occurrence retires the first and leaves the last. An agent
schedule fire retires the same way. A record trigger's park survives a later
`ok` delivery and a schedule fire of the same function.
