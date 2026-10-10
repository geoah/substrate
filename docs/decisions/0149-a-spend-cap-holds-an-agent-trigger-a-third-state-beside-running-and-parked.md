---
status: proposed
date: 2026-10-10
decision-makers: George Antoniadis
---

# 0149. A spend cap holds an agent trigger, a third state beside running and parked

## Context and problem statement

Agent runs had no spend limit. On 2026-10-07 one repository's agents spent
about USD 60 in 7 hours, 161 of those runs from one daily rollup, and nothing
paused them ([#880](https://github.com/geoah/substrate/issues/880)). The issue
asks for an optional daily cap per agent and per repository, and for a raised
cap to resume what the cap stopped. Until now a delivery was either running or
parked, and a park ends only by a hand's retry or forget
([0091](0091-a-parked-delivery-is-retried-or-forgotten.md))
or by a later settled schedule fire
([0142](0142-a-schedule-fire-that-settles-retires-its-triggers-older-parked-fires.md)):
nothing unparks when a condition clears.

## Considered options

- Park each delivery at the cap, with a reason naming the cap
- Hold the trigger: leave the delivery unclaimed until a pass finds the spend
  under the cap
- Stop a running loop once it crosses the cap, as the other budgets stop it

## Decision outcome

Chosen: hold the trigger. Before the dispatcher claims a delivery of an agent
trigger it compares the agent's and the repository's spend with their caps,
and at a cap it stops that trigger's walk where it stands. Nothing is claimed,
so the cursor or the fire state stays on the delivery still owed, no run row
and no parked failure is written, nothing retries, and the next pass asks
again. `TriggerStatus.held` names the cap and the spend so far. A chat, a
direct call, a hand's wake, run or retry, and every other root run are refused
with the same text before a thread opens.

Parking lost because 0091 ends a park only by hand: after a raised cap
somebody would retry every parked delivery one by one, and a busy trigger at
its cap would fill the parked list with rows that say nothing about their
deliveries. Stopping a running loop lost because an agent delivery is claimed
before its loop runs, and a loop that settles `overbudget` is a settled
delivery: its work would be lost, not paused.

The caps are the agent's `budgets.spendCentsPerDay` and a core `setting`
record with the id `substrate.reamde.dev/llm/spendCentsPerDay`
([0076](0076-a-bundle-ships-its-settings-as-core-setting-and-secret-records.md)),
both whole US cents
([0140](0140-money-is-an-integer-amount-of-its-currencys-minor-unit.md)). An
absent cap is none, and 0 holds every run; a write of the setting that the
engine could not read as such is refused. Spend is `llm/thread.costUSD` over
root threads whose `finishedAt` falls in the rolling 24 hours, read once per
dispatcher pass as a snapshot per thread, reconciled with what the engine
keeps in memory per root run and keyed by its thread: what a running chain has
charged, the cost a settle committed after the snapshot was read, and the
charges of a run whose settle never committed, which stay counted for the
window because no row carries them. A run the snapshot already holds is not
counted again. A root thread carries its sub-agents' cost, so a chain counts
against the cap of the agent that started it, and a sub-agent's own cap is not
enforced.

### Consequences

- Good, because a raised cap, or a window that moves on, resumes every held
  delivery at the next pass with nothing to clean up.
- Good, because a held delivery is never lost: the cursor still owes it.
- Bad, because a run admitted under the cap runs to its end, and an admission
  cannot see the cost of a model call another run has in flight, because the
  charge lands when the call returns. Spend passes a cap by what the runs
  going at that moment spend before they finish, and two admissions in the
  same instant both pass.
- Bad, because the in-memory part lives in one process, which holds only while
  a repository has one writer
  ([0083](0083-a-repository-has-one-writer-and-a-second-is-refused-at-open.md)),
  and a restart forgets the charges of a run whose settle never committed.
- Bad, because a continued chat counts its whole thread once its last turn
  finishes in the window, which overstates an old thread continued today.
- Bad, because garbage collection keeps a deleted thread's tombstone until
  its last settle leaves the window, since that row is the durable copy of
  what the run cost.
- Bad, because a function whose body runs an agent is not held: the body meets
  the refusal as an error and its delivery parks.

### Confirmation

`internal/engine/spend_db_test.go`:
`TestASpendCapHoldsAnAgentTriggerUntilTheCapIsRaised`,
`TestAZeroSpendCapHoldsEveryRun`, `TestARepositorySpendCapHoldsEveryAgent`,
`TestARepositorySpendCapRefusesAValueItCouldNotRead`,
`TestASpendCapCountsARunStillInFlight`,
`TestASpendCapCountsARunWhoseSettleNeverCommitted`,
`TestASpendCapCountsARunOnceBetweenItsCommitAndItsEnd`,
`TestASpendCapCountsAContinuedChatsWholeThreadAtOnce`,
`TestASpendCapForgetsSpendThatLeftTheWindow`,
`TestGCKeepsAThreadThatSettledInsideTheSpendWindow`,
`TestAnUnpricedModelSpendsNothingAgainstACap` and
`TestSpendSumsRootThreadsByWhenTheyFinished`;
`TestSpendLedgerReconcilesWithTheSnapshotByThread`
(`internal/engine/spend_test.go`) holds the reconciliation, and
`TestAgentSpendCapIsOptionalAndZeroIsACap` (internal/vocabulary) the
declaration.

## More information

No alert is raised while a trigger is held; the status and one log line per
hold are the signal. Revisit if the overshoot of runs already going costs more
than their work is worth: stopping a loop at the cap is then the option to
reopen, together with a way to give its delivery back.
