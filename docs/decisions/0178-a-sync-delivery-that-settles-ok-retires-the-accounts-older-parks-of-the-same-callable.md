---
status: proposed
date: 2026-10-20
decision-makers: George Antoniadis
---

# 0178. A sync delivery that settles ok retires the account's older parks of the same callable

## Context and Problem Statement

[0142](0142-a-schedule-fire-that-settles-retires-its-triggers-older-parked-fires.md)
retired only schedule fires, because a record park is one record's change
that no other delivery delivers again. A delivery that stamps a sync is not
that: its body reads the account and the upstream as they stand, like a
fire, and the change that fired it was the account's own request or connect.
On the 2026-10-20 rehearsal of #1005 a provider read 2 parked after an ok run
of the same account, and the Google card had read Needs attention for 22
days on one park. Parked run rows were also never pruned, so a trigger's run
list grew by every park forever.

## Considered Options

- Count around the parks in the status reads (rejected in 0142 for the same
  reasons).
- Retire the parks at the ok settle, through the unpark fold on the settling
  delivery's own entry.
- Leave them to a retry or a forget by hand.

## Decision Outcome

Chosen: retire at the settle. When a delivery that `syncStampFor` stamped
(the record's kind binds `sync`, the callable is a function of the kind's own
package, never an agent) settles ok, and the body left the record at neither
`erroring` nor `throttled`, every park of a record change naming the same
record of the same kind, of any trigger whose callable is this one, parked
before the delivery started, retires with it, at most 1000 per settle. A
webhook request, a fire, a claim in flight, interrupted or in its rerun, and
a row a hand holds are never retired. Each retired trigger's parked alert
recounts, and its failing alert resolves at zero
([0177](0177-a-failing-alert-needs-a-standing-park-and-ends-with-the-last-one-or-its-trigger.md)).
A schedule fire still retires no record park.

Run retention changes with it: a parked run older than the trigger's newest
`ok` run whose delivery no longer stands leaves with the runs past the
twenty. The newest `ok` run, the parks after it, and the run of a park still
standing stay outside the twenty. A settling transaction tombstones at most
100 runs, oldest first, so the first settle after the deploy, which finds a
trigger's whole backlog of ended parks at once (at least 110 on one production
trigger), does not hold the changelog lock for all of it; the rest go at
the trigger's next settles. This amends 0142's consequence that a
retired park's error survives in a run row, and
[0152](0152-a-hand-retry-writes-a-run-row-and-the-newest-ok-run-outlives-retention.md)'s
rule that parked runs are kept.

### Consequences

- Good, because the provider page reads no problem once the account syncs,
  and a Sync now clears an on-connect park.
- Good, because health is unchanged: every run it counts is kept.
- Bad, because a sync body whose work depended on the change that fired it
  (a request carrying parameters) loses its park without a retry; the
  engine cannot tell, and the trait's contract that the body reads the
  account as it stands is the only argument.
- Bad, because a retired park's error survives in a run row only within
  retention, and always in the changelog.
- Bad, because a park whose changelog entry is gone is never retired this
  way: the kind is matched through it.
- Bad, because the first settles after the deploy each write up to 100
  tombstones under the changelog lock.

### Confirmation

`internal/engine/syncsupersede_db_test.go` (the rehearsal's step d is
`TestASyncDeliveryThatSettlesOkRetiresTheAccountsOlderParks`; the
`erroring` and `throttled` exception is
`TestASyncDeliveryThatLeavesTheAccountErroringOrThrottledRetiresNothing`, the
kind match `TestASyncDeliveryLeavesTheParkOfTheSameIDUnderAnotherKind`, and
the held row `TestASyncDeliveryLeavesAParkAHandHolds`),
`TestAScheduleFireOfTheSameFunctionLeavesARecordParkParked` in
`schedulesupersede_db_test.go`, and
`TestParkedRunsOlderThanTheNewestOkRunLeaveWithTheRetention`,
`TestASettlePrunesAtMostOneBatchOfRuns` and
`TestARecordDeliveryOfAPlainKindLeavesItsParksParked` in
`health_fn_db_test.go`.
