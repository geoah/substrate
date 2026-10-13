---
status: proposed
date: 2026-10-11
decision-makers: George Antoniadis
---

# 0157. A counter is stored as sent and differenced at read under one of three contracts

## Context and problem statement

An energy meter, an odometer or a step total reports a running total that
may reset, fall or wrap. Differencing into amounts at ingest needs the
previous period's last point and breaks on late data. The plan's one
`resetRule` (`decrease`, `declared`, `wrap`) could not describe a signed
total, several resets in one trace, or a wrap without a stated modulus, and
differencing in `float8` loses increments (the review's fifth and eleventh
findings, [the plan, section 13](../plans/observations.md)).

## Considered options

- Store and index the total as sent, difference it at read under one of
  three contracts, `monotonic`, `signed` and `wrap`, exact in `numeric`
- Difference into amounts at ingest, in the adapter
- The plan's section 5.3 rules, with Home Assistant's 10 percent drop
  tolerance on an increasing total
- The three contracts, differenced in `float8`

## Decision outcome

Chosen: store as sent, difference at read under three contracts, exact.

A counter is stored as the device sent it, indexed as it is, and differenced
only in the derived index a read uses, never in the stored record and never
at ingest. The value's `resetRule` names its contract:

- `monotonic`: any drop is a reset to zero, then counting resumes (Home
  Assistant's `total_increasing`, an odometer rollover, a plug that reboots).
- `signed`: the value may fall (a net meter exporting solar power).
  `increase` is the net change inside one `resetAt` epoch, and only a new
  `resetAt` starts a new accumulation.
- `wrap`: the register wraps at `wrapAt`, written on the value.

A trace carries one `resetAt`, so a trace splits at every reset epoch: an
adapter that sees `resetAt` change inside a period starts a new trace there.

The rollup row stores `increase`, the reset-aware sum of every step whose
later reading is in the row, `resets`, and `covered_ms`, the time those
steps span. The step in from the stream's previous available reading
belongs to the row of the later reading, wherever the earlier one is, and
the engine computes it when it computes the row: the recompute reads the
stream's last available point before the bucket and differences it under
the later reading's contract. The increase of a run of whole buckets is
therefore the sum of their rows, and `rate` is that sum over the sum of
their `covered_ms`, in the counter's unit over a second (a `kW.h`
counter's rate is in `kW.h/s`). Nothing extrapolates to the range's
boundaries; a partial bucket's `coverage` says how much of it the steps
span.

The step is computed when the row is, rather than at read with a window
function, so every reader of the rows (the aggregate read, the CLI, a
function's host call, an agent's query) takes one SQL path, a sum, and none
of them fetches the previous row or applies a counter contract. The price is
that a row depends on a point of another record: a write reads the stream's
previous reading when it recomputes a bucket, and a write that changes a
reading also recomputes the bucket of the stream's next reading, whose step
in it moved.

The engine has no drop tolerance. A noisy total (`100`, `99`, `101`) under
`monotonic` reads as a reset; its writer declares `signed` or smooths it in
the adapter.

Counter endpoints and `increase` stay exact: the point row carries
`value_exact numeric` and the rollup `increase numeric`, so an increment that
was exact in storage stays exact. `float8` serves gauge and amount analytics
only.

Differencing at ingest makes every trace depend on the one before it. A
drop tolerance is a guess the engine cannot explain to a reader, and
`float8` loses the last digits of a large meter reading.

### Consequences

- Good, because the meter reading is kept as the fact it is, and a late or
  corrected trace re-derives its own buckets and the bucket of the stream's
  next reading, nothing more.
- Good, because a daily `increase` of a `kW.h` meter is exact to its last
  stored digit.
- Good, because a read sums rows and needs no window function and no
  counter contract.
- Bad, because a jittery `monotonic` counter reports false resets and a
  large false increase until its writer declares `signed` or smooths it.
- Bad, because a rollup row is not a function of one record's points alone:
  its step in reads another record's point, and the recompute must find the
  next reading's bucket when a reading changes.
- Bad, because more than one wrap between two readings cannot be seen and
  reads as one.
- Bad, because `numeric` columns are wider and slower than `float8` in the
  two largest tables.

### Confirmation

`TestCounterIncreaseAcrossAMonotonicReset`,
`TestCounterSignedIncreaseIsNetWithinAnEpoch`, `TestCounterWrapsAtWrapAt`,
and `TestCounterIncreaseStaysExact`, where an increment exact in the stored
digits of a large reading comes back exact (`internal/observed`);
`TestCounterStepInBelongsToTheLaterWindow`, where the step across a bucket
boundary is the later row's; and `TestObservedRollupEqualsTheRawAggregate`,
where the stored rows equal the specification over the stored points
through a correction, a deletion and a rebuild.

## More information

[The plan, section 5.3](../plans/observations.md) has the original rules,
which section 13 replaces; [under the hood](../plans/observations/proposal/04-under-the-hood.md)
has the three contracts; [the implementation brief](../plans/observations/implementation/README.md)
settled them with `wrapAt` on the value and no drop tolerance. Revisit if a
common device family needs a counter none of the three describes.
