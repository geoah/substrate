---
status: proposed
date: 2026-10-10
decision-makers: George Antoniadis
---

# 0150. A schedule lane fires its due triggers in parallel, one fire per trigger

## Context and problem statement

[0147](0147-a-pass-runs-its-schedule-triggers-in-a-lane-beside-its-record-triggers.md)
moved a pass's schedule triggers into a lane beside its record triggers, and
the lane delivered one fire at a time. A repository with several schedules
due at one minute fired them one after another: on a four-core arm64 host
the seven provider syncs due each hour started 1 to 3 minutes late, and the
log read `schedule fire dispatched late ... late=3m13s` for
`google-calendar-scheduled`
([#883](https://github.com/geoah/substrate/issues/883)). 0147 named this as
the reason to revisit it. This record amends 0147's "each lane delivers one
at a time" for the schedule lane, and the way the dispatcher's ceiling of
sixteen runner processes holds.

## Considered options

- Workers in the schedule lane, up to a per-lane cap, one fire per trigger,
  and a process-wide count of delivery slots that every dispatcher delivery
  holds, record trigger turns included
- The same workers, with the process-wide slots held by schedule fires only
- The same workers with a per-lane cap and no process-wide bound
- A goroutine per due trigger, unbounded

## Decision outcome

Chosen: workers with a per-lane cap and process-wide slots for every
delivery, because it is the only option that keeps the ceiling 0147
documented. The lane runs up to 4 triggers' fires at once
(`SUBSTRATE_TRIGGER_LANE_WORKERS`, 1 to 16), and every delivery a pass runs,
each record trigger's turn and each schedule fire, holds one of 16 slots
the whole process shares. Eight passes of two lanes ran at most 16 at once
before; a per-lane cap alone allows 8 x (1 + 4) = 40, slots for schedule
fires alone allow 8 + 16 = 24, and an unbounded lane allows one per schedule
trigger. A wake, a retry by hand and a webhook fire take no slot, as they
took none of the dispatcher's eight passes.

The other invariants hold. A trigger with a worker running gets no second
one, so its occurrences still fire oldest first, each once, and the fire
state's compare-and-swap still settles a race with a wake or another
process. A trigger still fires at most ten occurrences per pass however
many looks the lane takes. The lane runs its first round whole even when the
record lane is already done, which is what a repository with no record
triggers needs, and then stops launching once the record lane ends. The pass
returns only after every worker has ended, a panic in the lane's own code
included. Each worker carries its own recover, because the lane's does not
reach a child goroutine: a trigger whose fire panicked fires no more that
pass, its slot is given back, and its occurrence stops counting as in
flight.

### Consequences

- Good, because the due fires of different triggers start together: in the
  test that measures it, five fires of 1.2 s start 1.25 s apart instead of
  4.9 s with one worker.
- Good, because the process runs no more runner processes and transactions
  than before: sixteen.
- Bad, because a record trigger's turn now waits for a slot when sixteen
  deliveries are in flight across the process. At the default width 0147's
  "the record lane never waits for a fire" still holds within one
  repository (4 + 1 is under 16), but not across a host with several busy
  repositories, and not even for a sole repository whose lane is set to 16.
- Bad, because triggers that name the same Python function still fire one
  after another: the runner keeps one process per repository, function and
  content hash, and that process takes one invocation at a time
  (`internal/runner/runner.go`, `proc.roundtrip`). Their workers wait there
  holding a lane worker and a slot each.
- Bad, because four fires of one repository now compete for the CPU and for
  the changelog lock at commit, so on a small host each may run slower than
  it did alone.

### Confirmation

`internal/engine/schedulelane_db_test.go`: five schedule triggers on five
functions, each fire 1.2 s, due at one instant, all start within 3 s and
never more than four run at once; a repository with no record triggers fires
six due triggers in one pass, which returns only once all six settled; a
fire that panics in its worker is contained, the other trigger's fire
settles, every slot is given back, no occurrence is left counted as in
flight and the next pass fires the occurrence;
and a trigger due every second whose fire takes 1.2 s still runs its
occurrences oldest first and never two at once. `internal/config` and
`engine.Open` refuse a lane wider than the 16 slots, and a negative one.

## More information

Revisit if fires of one function start to wait on each other, which would
call for more than one runner process per function, or if record trigger
turns are seen waiting for slots, which would call for slots the record
lane does not share.
