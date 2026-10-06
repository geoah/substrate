---
status: proposed
date: 2026-10-06
decision-makers: George Antoniadis
---

# 0147. A pass runs its schedule triggers in a lane beside its record triggers

## Context and Problem Statement

A repository's dispatcher pass fired its due schedule occurrences and then
walked its record triggers one at a time, each for up to 30 seconds
([#638](https://github.com/geoah/substrate/issues/638)). An occurrence that
fell due during the walk waited for the rest of it and for the next pass. On
a four-core arm64 host with several backlogged record triggers a pass took
about 10 minutes, and hourly syncs started 5 to 11 minutes late: Gmail's
19:00 fire started at 19:08:21. Nothing bounded the wait but the backlog.

## Considered Options

- A schedule lane: the pass runs its schedule triggers in a second
  goroutine beside the record triggers, one delivery at a time, looking for
  due occurrences every 5 seconds until the record triggers are done
- Check for due occurrences between record deliveries and fire them first
- Cut a record trigger's 30 seconds short while an occurrence is due

## Decision Outcome

Chosen: the schedule lane, because it is the only option whose bound does
not depend on how long one record delivery takes. A record trigger can run
an agent loop or a paged drain for minutes, and the other two options start
the due fire only after that delivery in hand returns; cutting slices short
also still walks every remaining record trigger first. The lane starts a due
fire within one poll, behind only the fires of the repository's other
schedule triggers, and the record lane never waits for a fire.

The invariants hold without new locks. A trigger has one source arm, so it
runs in one lane, and each lane delivers one at a time: no trigger has two
deliveries in flight from the dispatcher, and the schedule lane skips a
trigger the record lane runs that pass, in case its source changed mid-pass.
The dispatcher still runs one pass per repository, and the pass returns only
after both lanes end, a panic in the record lane included. A trigger behind
on missed occurrences still fires at most ten per pass, however many times
the lane looks. Fire states,
cursors, claims, coalescing and the retiring of parked fires
([0142](0142-a-schedule-fire-that-settles-retires-its-triggers-older-parked-fires.md))
are unchanged, and deliveries of two triggers of one repository already ran
at once before this (a webhook fire, a wake or a retry beside a pass). The
lane reads the trigger rows again at each poll, which costs what an idle
repository's pass costs every dispatcher tick.

### Consequences

- Good, because a due fire starts within about 5 seconds whatever the record
  triggers owe; in the test that measures it, 16 ms instead of 2.45 s.
- Good, because the record triggers keep their whole budgets: the lane takes
  nothing from them.
- Bad, because a pass now runs up to two invocations at once, so the
  dispatcher's 8 slots bound 16 runner processes and 16 transactions, and a
  schedule fire competes with a record delivery for the CPU.
- Bad, because a function on both a schedule and a record trigger now
  overlaps itself more often, which it must already survive
  ([0093](0093-a-guarded-write-may-declare-that-losing-is-normal.md)).
- Bad, because schedule fires still wait for each other: a slow schedule
  delays the next one due in the same repository.

### Confirmation

`internal/engine/schedulelane_db_test.go`: with three record triggers whose
every delivery spends the budget, an hourly occurrence due mid-pass starts
within a second of its due time while every widget is still delivered once
per trigger; and a schedule due every second whose fire takes 1.2 s runs its
occurrences oldest first, each once, never two at once, while a record
trigger drains beside it. A pass whose record delivery panics stops its
schedule lane before it returns, and a lane that looks many times in one
pass fires no more missed occurrences than one pass allows.

## More Information

Revisit if one repository's schedule fires start to wait on each other, which
would call for a concurrency limit inside the lane rather than one fire at a
time.
