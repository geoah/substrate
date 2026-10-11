---
status: proposed
date: 2026-10-11
decision-makers: George Antoniadis
---

# 0160. The observed index is three derived tables, and a rollup serves only exact answers

## Context and problem statement

A heavy health user writes about 17 million point values a year (an estimate
in [under the hood](../plans/observations/proposal/04-under-the-hood.md)),
packed into trace records whose properties Postgres cannot reduce quickly.
An index over them has to stay a pure function of the fold, like `refs` and
`fts`, because the changelog is the truth
([0050](0050-the-changelog-is-checksummed-segment-files-and-postgres-indexes-it.md))
and a rebuild must reproduce it. The review of 2026-10-10 found that a rollup
cannot answer every `maxGap`, threshold or priority request (finding 2), and
that expiring the index and deleting raw data are different operations
(finding 9) ([the plan, section 13](../plans/observations.md)).

## Considered options

- Three tables the fold derives (points, 15-minute rollups, the latest point
  per stream), with a rule for when a rollup may answer
- Points only, every aggregate a raw scan
- Rollups computed by a scheduled job outside the fold
- A persistent stream record per metric, source and thing

## Decision outcome

Chosen: three derived tables and the eligibility rule.

A **stream** is the index key of one run of readings: `kind`, `metric`,
`source`, `about`, `channel`, `form`, `code` and `utc_offset_min`.

- `observed_points` has one row per value (one per observation, one per
  point of a trace): the stream key, the time span, the unit,
  `value float8`, `value_exact numeric` for counters, `value_base`, `code`,
  `statistic`, `bounded` and `quality`.
- `observed_rollups` has one row per stream and 15-minute bucket, for
  gauges, amounts and counters and never for summaries: `n`, `sum`, `min`,
  `max`, `mean`, `m2`, the first and last points, `covered_ms`, `held_ms`,
  `twa_sum`, `last_hold_ms`, `max_gap_ms`, `increase numeric`, `resets`,
  `sin_sum` and `cos_sum`. Fifteen minutes divides every UTC offset in use,
  so a civil day at a reading's own offset is a sum of whole rows. `about`,
  `code` and `channel` are in the key because a rollup can serve a request
  only when every predicate is constant over its keys.
- `observed_latest` has one row per stream: the newest point with its value,
  code, unit, form, hold, offset, record id and revision. It is the stream
  list; there is no stream record.

All three carry `repository` under the same row-level security and grants as
`records` and `refs`. The fold writes them in the write's transaction, in
`foldRecordOp` and in the tombstone, purge, restore, merge and
declaration-removal paths. Every bucket the old and the new rows touch is
recomputed once per transaction. `observed_latest` is recomputed for the old
and the new stream when a correction re-keys or moves the newest point, ties
broken by revision, then record id. A rebuild clears and replays all three;
`repository verify` and the export never read them.

A rollup serves a reducer only when its stored statistics answer it exactly:
`count`, `sum`, `min`, `max`, `first`, `last`, `avg`, `stddev`, `increase`,
`rate` and `circularMean` always; `twa` and `timeInState` only when the
request's `maxGap` is absent or not below the bucket's `max_gap_ms`;
`percentile`, `durationIn` and any threshold never. Otherwise the read scans
raw points within its budget, or answers `unavailable` for a range whose
points have expired.

Phase 2 keeps every raw record. Expiring `observed_points` rows is an index
policy and never deletes a record; a read over an expired range marks its
rows `unavailable: true` for reducers that need points. Retention tiers are
deferred.

A raw scan per chart reads millions of rows for a year. A scheduled job
would make the derived rows depend on a clock, so a live write and a rebuild
could disagree. A stream record's other uses (per-stream defaults and names)
stay an open question.

### Consequences

- Good, because a year-long chart reads rollup rows, not millions of points.
- Good, because a rebuild reproduces the index, and verify and export never
  depend on it.
- Bad, because every trace write deletes and reinserts its points and
  recomputes its buckets in the write's transaction. The review's fourteenth
  finding makes measuring that path a gate before the rollup layout is
  frozen.
- Bad, because the rollup row is wide, and `about`, `code` and `channel` in
  the key multiply the rows.
- Bad, because percentiles, thresholds and a `maxGap` tighter than the data
  scan raw points and reach the budget sooner.

### Confirmation

Tests the work package 3 implementers will write:
`TestObservedRollupEqualsTheRawAggregate` (a bounded raw aggregate equals its
rollup across a partial boundary, a correction, a deletion and a rebuild, the
check the plan's section 11 sets for phase 2),
`TestRebuildReproducesTheObservedTables`,
`TestVerifyAndExportReadNoObservedTable`,
`TestRollupServesTwaOnlyWithinMaxGap` and
`TestAnExpiredRangeAnswersUnavailable`.

## More information

[The plan, section 8.1](../plans/observations.md) and
[under the hood](../plans/observations/proposal/04-under-the-hood.md) hold
the columns and the reducer table;
[the implementation brief](../plans/observations/implementation/README.md)
puts the tables in migration 0012 and defers retention tiers. Revisit when
the benchmark gate reports, or when a home has a year of data and asks for a
daily rollup level.
