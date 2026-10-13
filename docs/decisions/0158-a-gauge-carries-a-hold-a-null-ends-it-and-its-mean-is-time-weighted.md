---
status: proposed
date: 2026-10-11
decision-makers: George Antoniadis
---

# 0158. A gauge carries a hold, a null ends it, and its mean is time-weighted

## Context and problem statement

Most home protocols report on change: a thermometer that sits at 21.5 for
three hours sends one point, so a plain mean of points is wrong. The plan's
section 5.4 had an adapter copy the previous period's last value to the start
of each new trace. The review of 2026-10-10 called that copy a faked
measurement (finding 4) and found that unavailable, not measured and invalid
are different and none of them is zero (finding 6)
([the plan, section 13](../plans/observations.md)).

## Considered options

- A hold on the value, a time-weighted mean, the point current at the start
  of a read fetched at read time, and a `null` in a trace that ends the hold
- A plain mean of points, with no hold
- Section 5.4's copy: the adapter writes the previous value at offset 0 of
  each new trace
- A `null` in a trace as a gap the hold bridges

## Decision outcome

Chosen: the first.

- `hold` on a gauge value is how long it stands after its timestamp when no
  newer point exists. Absent means until the next point. A single gauge
  with an `endsAt` holds no longer than that span. A periodic sensor
  sets twice its period, an on-change sensor its heartbeat, a button press
  `PT0S`.
- `maxGap` on a request caps every hold: a hold expires at `maxGap` inside a
  gap, even when a later point arrives.
- A `null` inside a trace means unavailable from here. It ends the hold and
  counts in nothing. An observation says why a value is absent in
  `absentReason`, and `quality` gains `unavailable`. A `null` does not bridge,
  because a later heartbeat must not turn an outage into coverage.
- The point current at the start of a read's range is fetched at read time,
  one indexed lookup per stream (an index key, defined in 0160), and never
  copied into the next trace.
- `twa`, the time-weighted mean, is the mean of on-change data. Rollups
  carry `twa_sum`, `held_ms`, `last_hold_ms` and `max_gap_ms`, so `twa` is
  served from rollups whenever the request's `maxGap` allows
  ([0160](0160-the-observed-index-is-three-derived-tables-and-a-rollup-serves-only-exact-answers.md)).
- Held intervals are built from every transition first and filtered by code
  or threshold after, so filtering `closed` out first cannot overcount the
  time a door was open.

A plain mean weighs a three-hour flat reading as one point. The copy puts a
value in a trace that its source never sent at that time, and a bridged
`null` reports coverage for an outage.

### Consequences

- Good, because every stored point is a real measurement.
- Good, because a three-hour flat temperature weighs three hours.
- Bad, because a read pays one extra lookup per stream for the point current
  at its start.
- Bad, because a gauge written with no `hold` that goes silent holds its
  last value until the next point or the request's `maxGap`; a request that
  names no `maxGap` gets the long hold.
- Bad, because a `maxGap` below a bucket's `max_gap_ms` sends `twa` and
  `timeInState` to raw points for that bucket.

### Confirmation

In internal/observed, the specification the read and the rollups are held
to: `TestASilentSensorHoldsUntilMaxGapOrItsHold` (`twa` weighs a three-hour
flat reading three hours, and a `maxGap` or a `hold` ends it),
`TestANullInATraceEndsTheHold`, `TestMaxGapEndsAHoldInsideAGap`,
`TestTheStartPointIsCarriedIn` (a range that starts mid-hold takes the
earlier point's value), `TestGaugeHoldEndsAtTheEnd`,
`TestTimeInStateBuildsIntervalsBeforeFiltering` and
`TestRollupServesTwaOnlyWithinMaxGap`. That no trace holds a copy of an
earlier point is held by review only.

## More information

[The plan, section 5.4](../plans/observations.md) has the hold rule and the
withdrawn copy; [under the hood](../plans/observations/proposal/04-under-the-hood.md)
has the reducer table; [the implementation brief](../plans/observations/implementation/README.md)
settled the `null` rule. Revisit if a source family reports gaps a `null`
cannot describe.
