---
status: proposed
date: 2026-10-20
decision-makers: George Antoniadis
---

# 0182. A record trigger is walked by one caller at a time

## Context and Problem Statement

A failed Sync now can vanish: no parked run, no failure, and the account
stays at `syncState: running`. Sync now writes `syncRequestedAt` and wakes
the account's on-request trigger. The wake walks the trigger's backlog on the
request's goroutine while the dispatcher's pass, every five seconds, walks
the same trigger, and the only thing between the two walks is the cursor's
compare-and-swap. A paged sync's first page writes `syncRequestedAck` and
moves no cursor. The pass then reaches the same change, evaluates the guard
against the record's current state, reads false, records a skip and moves
the cursor. When the wake's later page fails, its park loses the cursor swap
and yields without a trace. The rehearsal of #1005 on 2026-10-20 hit this
twice.

## Considered Options

- A per-trigger hold that a wake waits for
- A per-trigger hold that a wake tries once, answering `ran: 0` when another
  walk holds it
- A wake that only asks the dispatcher for a pass now
- A Postgres advisory lock per (repository, trigger)

## Decision Outcome

Chosen: a per-trigger hold that both callers try once.

`processRecordTrigger` takes an in-process hold on the trigger before it
reads the cursor, for a pass's turn and a wake alike, and gives it back when
the walk ends, a panic included. A pass's turn that finds a wake walking the
trigger moves on to the next trigger and comes back at the next pass. A wake
that finds a pass or another wake walking it runs nothing and answers
`{"ran": 0}` at once. The walk in progress or a later pass delivers the
woken rows: a wake's walk and a pass's turn within its budget read the
changelog until a read returns no changes, and a pass's turn past its
budget stops after the delivery in hand. An
idle pass still checks idleness first and takes no hold. A trigger that is
walking keeps its activity entry through the pass's prune.

The dispatcher already delivers every row, so a wake only speeds a delivery
up. A wake that waited would hang the Sync now request for the whole delivery
in hand, minutes for a sync, whenever the pass got there first. A wake that only nudges the dispatcher changes the verb's answer and
needs a path into substrated's per-repository queue. An advisory lock buys
nothing while a repository has one writer process
([0083](0083-a-repository-has-one-writer-and-a-second-is-refused-at-open.md)),
and holds a connection for the walk.

### Consequences

- Good, because a failing run parks once and stamps `erroring`, and a paged
  chain has one owner for its whole drain.
- Good, because neither caller waits on the other.
- Bad, because `ran: 0` no longer tells "nothing was due" from "another walk
  has it" on the wire. A wake during a pass's turn delivers nothing itself;
  its rows land in the walk in progress or a later pass. A later pass starts
only after the current pass has given every other record trigger its turn
budget, so the wait can run well past the dispatcher's tick.
- Bad, because a wake with no budget can hold a trigger through a long
  backlog, and the pass skips that trigger on every tick meanwhile.
- Bad, because the hold is per process, and is complete only while 0083
  holds.

### Confirmation

`TestAWakeAndAPassWalkOneTriggerOneAtATime` and
`TestAWakeOfATriggerThePassWalksRunsNothingAndWaitsForNothing` in
`internal/engine/syncwake_db_test.go`, and
`TestARecordWalkIsHeldOncePerTrigger` in
`internal/engine/recordwalk_internal_test.go`.

## More Information

Two invocations of one function by two different triggers over one record
still overlap, and stay the body's to guard
([0093](0093-a-guarded-write-may-declare-that-losing-is-normal.md)). The
record and schedule lanes are
[0147](0147-a-pass-runs-its-schedule-triggers-in-a-lane-beside-its-record-triggers.md)
and [0150](0150-a-schedule-lane-fires-its-due-triggers-in-parallel-one-fire-per-trigger.md);
a schedule trigger's wake beside its lane's fire is not covered here.
Revisit if a wake must drain to head whatever the pass does.
