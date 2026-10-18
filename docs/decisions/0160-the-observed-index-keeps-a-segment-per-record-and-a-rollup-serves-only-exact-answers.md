---
status: accepted
date: 2026-10-18
decision-makers: George Antoniadis
---

# 0160. The observed index keeps a segment per record, and a rollup serves only exact answers

## Context and problem statement

A heavy health user writes about 17 million point values a year (an estimate
in [under the hood](../plans/observations/proposal/04-under-the-hood.md)),
and a home with 100 sensors at a reading a minute writes 52.6 million,
packed into trace records whose properties Postgres cannot reduce quickly.
An index over them has to stay a pure function of the fold, like `refs` and
`fts`, because the changelog is the truth
([0050](0050-the-changelog-is-checksummed-segment-files-and-postgres-indexes-it.md))
and a rebuild must reproduce it. The review of 2026-10-10 found that a rollup
cannot answer every `maxGap`, threshold or priority request (finding 2), and
that expiring the index and deleting raw data are different operations
(finding 9) ([the plan, section 13](../plans/observations.md)). The first
layout kept one row per point, about 1,377 bytes each, 15-minute rollups
alone, and derived every point inside a rebuild's transaction
([the benchmark](../plans/observations/benchmark.md)).

## Considered options

- One row per record (a segment) under a private stream id, its points
  derived again from the record by every reader; rollups in tiers the metric
  names; the latest point per stream
- One row per point, keyed by a private stream id
- Points only, every aggregate a raw scan
- Rollups computed by a scheduled job outside the fold
- Readings outside records and the changelog, in a store of their own

## Decision outcome

Chosen: segments, tiered rollups, the latest row and the eligibility rule.

A **stream** is one run of readings: `kind`, `metric`, `source`, `about`,
`channel` and a summary's `statistic`. A stream's points of one `form` are
what a gauge's hold and a counter's steps run across. Within a bucket those
points split into **groups** by `code`, `unit`, `delta`, `utc_offset_min`
and `offset_known`.

- `observed_streams` gives each stream a private `bigint` id and keeps the
  longest extent of any record it has held. The id never leaves the engine:
  no record, changelog line or API answer names it, and a rebuild may
  allocate another.
- `observed_segments` has one row per observed record: its stream, form,
  first and last point, the latest end of a span, its point counts, offset,
  codes, and a fingerprint of everything the index reads from it. A read of
  `[lo, hi)` takes the segments starting in `[lo - longest extent, hi)` and
  derives their points from the records with the fold's own `deriveObserved`.
  A write whose fingerprint did not move derives nothing again.
- `observed_rollups` has one row per stream, form, tier, bucket and group,
  for gauges, amounts and counters and never for summaries: `n`, `sum`,
  `min`, `max`, `mean`, `m2`, the first and last points, `covered_ms`,
  `held_ms`, `twa_sum`, `last_hold_ms`, the gaps, `increase numeric`,
  `resets`, `sin_sum` and `cos_sum`. The tiers are the metric's `rollups`
  (`5m`, `15m`, `1h`, `1d`; absent means `15m`, `1h`, `1d`, empty none). A
  bucket starts at a multiple of its width in the group's own offset, so a
  `1d` row is a local day. The finest tier is computed from the points and
  each coarser one composed from the next finer tier's rows with the merge
  the read uses, or from the points where the finer rows cannot say it
  exactly.
- `observed_latest` has one row per stream: the newest point with its value,
  code, unit, form, offset, record id and the stream's count of available
  points.

A fifth table, `observed_index`, records the version of the derivation
rules. An open whose binary carries newer rules re-derives every row of
every observed kind behind it, and a golden corpus test fails, naming the
version, when a change moves what the rules derive.

All carry `repository` under the same row-level security and grants as
`records` and `refs`. The fold records what each written record derives and
the settle at the end of the transaction applies it under the repository's
observed lock: the segments whose fingerprint moved, the rollup rows of
every bucket of every tier their old and new points stood in, and the
latest rows of their streams. Every reader orders points at one instant by
record id compared byte by byte, then by index in the record. A rebuild and
a boot import clear the tables and queue every observed kind for the
background pass instead of deriving inside the replay; a metric whose
`rollups` change queues its rows the same way; `repository reindex
--observed` queues every row. `repository verify` and the export never read
the tables.

A rollup serves a reducer only when its stored statistics answer it exactly:
`count`, `sum`, `min`, `max`, `first`, `last`, `avg`, `stddev`, `increase`,
`rate` and `circularMean` always; a gauge (its time-weighted `avg` and
`timeInState` among them) only when the request's `maxGap` is absent or not
below the bucket's `max_gap_ms`; `percentile`, `durationIn`, any threshold
and a row holding a censored value (a comparator: the row keeps how many,
not which) never. A read takes the coarsest tier whose buckets fit its own,
finer tiers where they do not, and raw points where none does, within its
budget.

Each point keeps its value's comparator (`<0.01`), so a reader answers a
censored value with it. No raw reading expires; retention is deferred.

The index is the records' only where it is current. While the background
pass owes a kind's re-derivation (queued, part done or interrupted), or the
index was derived under older rules than the binary's, an aggregate read
over the kind (the read lands after the index) is refused as unavailable
(`ErrObservedRederiving`, which wraps `ErrUnavailable`: a `503` with
`Retry-After` and the reason `observed_rederiving`), checked in the read's
own snapshot. A pass a
metric's `rollups` change queued holds back only the reads that may reach
that metric. `exact` on an answer speaks of numeric precision alone.

A row per point costs about a kilobyte a reading even keyed by a stream id,
twenty times the record that holds it. A raw scan per chart reads millions
of points for a year. A scheduled job would make the derived rows depend on
a clock, so a live write and a rebuild could disagree. A store outside the
records breaks the one truth, the rebuild and the export.

### Consequences

- Good, because the index costs tens of bytes a reading, not a kilobyte:
  the records and the changelog hold the readings, the segments a row per
  record, and the rollups a row per bucket and tier.
- Good, because a year of daily means reads a row a day, and a metric picks
  its tiers.
- Good, because a rebuild holds the write lock for the records alone, and
  verify and export never depend on the index.
- Bad, because a raw read and a bucket recompute parse the records' JSON
  again, which is slower per point than reading a point row.
- Bad, because a rollup row is wide, and `about`, `code` and `channel` in the
  key multiply the rows; the 15-minute tier is the largest table of a home's
  repository.
- Bad, because after a rebuild, an import or a tier change the kinds are
  unreadable until the pass ends, minutes for years of readings.

### Confirmation

In `internal/observed`: `TestComposeMatchesThePoints` (a composed row is
the row the points make, every form, every alignment, every pair of tiers)
and `TestRollupServesTwaOnlyWithinMaxGap`. In `internal/engine`:
`TestObservedDerivationMatchesTheGolden`,
`TestObservedRollupEqualsTheRawAggregate`, `TestRebuildReproducesTheObservedTables`,
`TestObservedCoarserRowsFollowAPointAtAnotherOffset`,
`TestObservedCounterCorrectionReachesTheNextReadingAtAnotherOffset`,
`TestObservedMetricWriteDuringItsReprojectionPage`,
`TestObservedPatchTheIndexDoesNotReadDerivesNothing`,
`TestVerifyAndExportReadNoObservedTable` and
`TestObservedTiesAtOneInstantFollowByteOrder`. Seven land with the
aggregate read, which reads the index back:
`TestObservedRollupsAnswerAsRawPoints` (generated streams through the rows
and the raw points), `TestAggregateTiersServeTheCoarsestRowsThatFit`,
`TestObservedIndexIsolation`, `TestObservedMetricRollupsChangeRederivesItsRows`,
`TestObservedRebuildRefusesReadsUntilTheIndexIsDerived`,
`TestObservedReindexRepairsTheIndex` and
`TestAggregateIsUnavailableWhileTheIndexIsRederived`.

## More information

[The plan, section 8.1](../plans/observations.md) and
[under the hood](../plans/observations/proposal/04-under-the-hood.md) hold
the first design; [the benchmark](../plans/observations/benchmark.md)
measures this one against it on a durable database. The tables are in
migration 0013. Revisit when a home's 15-minute tier outgrows its records,
or when retention is asked for.
