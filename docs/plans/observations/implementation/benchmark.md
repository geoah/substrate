# Observed index: the benchmark gate

Measured on 2026-10-11 for work package 3, the gate the plan's finding 14
sets before the rollup layout is frozen: the whole write, correction and
rebuild path of the observed index, on real tables.

```bash
SUBSTRATE_BENCH_OBSERVED=1 go test -count=1 -v -run '^TestObservedIndexBenchmark$' ./internal/engine/
```

`TestObservedIndexBenchmark` (`internal/engine/observed_bench_db_test.go`)
imports the `observations` and `health` samples and writes 30 days of three
streams, one record per stream and hour, each through its own `Put`: a heart
rate trace at 1-minute spacing (60 points), a step amount trace at 15-minute
spacing (4 points), and for two hours a day a power trace at 1-second
spacing (3600 points). It then rewrites one day's traces at revision 2 and
rebuilds the repository, and checks the rebuild reproduced the three tables.

**Caveat: one box, one run.** A 16-core AMD EPYC-Rome VM with 31 GB of RAM,
Go 1.27.2, load average between 2 and 6 from other work during the run, at
commit f6ed69f2; an earlier run at 5d6bc6a8 gave write and rebuild times
within 1 percent of these and read times within 10 percent. Postgres 16 runs in
the test container on tmpfs with `fsync=off`, `synchronous_commit=off` and
`full_page_writes=off`, and the changelog segments are on tmpfs too, so
every time below is a floor for a disk that syncs, and the WAL bytes leave
out full-page images. Reads run on the app role's pool, row level security
in force, best of three.

## Results

| Phase | Records | Points | Rollup rows | Changelog bytes | WAL bytes | Wall time |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| write 30 days | 1500 | 262080 | 6000 | 7473985 | 426999808 | 1m8.486s |
| rewrite one day, revision 2 | 50 | 8736 | 6000 | 7575857 | 9786000 | 1.066s |
| rebuild | 1629 | 262080 | 6000 | 7575857 | 310538672 | 24.189s |

Records and points are the phase's own, except the rebuild's, which are the
repository's (the 129 records past the 1500 are the seeded and imported
declarations, the provider rows and the metric catalogues). Rollup rows and changelog bytes are the totals after the
phase.

| Stream, write phase | Puts | Points per put | Mean wall time per put |
| --- | ---: | ---: | ---: |
| heart rate | 720 | 60 | 39.52ms |
| steps | 720 | 4 | 33.56ms |
| power | 60 | 3600 | 262.98ms |

A trace of one point took about 12 ms in a separate probe, which is the
write path without much index work. Timers placed in the engine for that
probe (and removed) split a 3600-point power put into about 110 ms
inserting the points, about 80 ms recomputing its four rollup rows, of which
most is reading the 3600 points back through `database/sql`, and under 2 ms
for the latest row.

| Table, after the write | Rows | Total bytes | Bytes per row, with indexes | Bytes per row, heap tuple |
| --- | ---: | ---: | ---: | ---: |
| records | 1629 | 6651904 | 4083 | 1292 |
| observed_points | 262080 | 360923136 | 1377 | 424 |
| observed_rollups | 6000 | 6045696 | 1007 | 452 |
| observed_latest | 3 | 65536 | 21845 | 395 |

| Read, 30 days, daily mean per stream | Rows read | Wall time |
| --- | ---: | ---: |
| heart rate from rollups | 30 | 3.74ms |
| heart rate from raw points | 30 | 64.45ms |
| power from rollups | 30 | 1.01ms |
| power from raw points | 30 | 257.31ms |

The first run found a plan fault: the rollup recompute joined the touched
buckets against `observed_points`, and the planner, with no statistics on
the fresh rows, put the stream outside the loop and read every point of it
on every write. The heart rate put grew from 22 ms to 41 ms over 200 hours
of writes and the power put to 500 ms. The recompute now reads each bucket
through a fenced `LATERAL` subquery, one range of the stream index, and the
numbers above are after that fix.

## Should the layout be frozen

Freeze the rollup layout, not the points layout. Rollups are cheap and they
pay: 6000 rows for a month of three streams, about 1 KB a row with its key,
and a month's daily mean reads about 15 times (heart rate) to 260 times
(power) faster from them than from raw points. `observed_points` is the cost: 1377 bytes a point with its two
indexes, 424 in the heap, almost all of it the stream key (`kind`, `metric`,
`source`, `about`, `channel`) and `record_id` repeated as text on every row
and in both indexes. At the plan's estimate of 17 million points a year for a
heavy user that is about 23 GB a year, and the write phase logged about
1.6 KB of WAL per point, the records and the changelog included. Before a release fixes it, move the stream key into an
`observed_streams` table with a `bigint` id and key points and rollups by
that id; a point row would drop to roughly 100 bytes of heap and its index
entries to a few dozen bytes. The write path's other cost, reading a
bucket's points back through `database/sql` to recompute its rows, can be
cut by computing a bucket from the points a write just derived when no other
record has points in it, but it is a third of a dense trace's write and not
urgent.

## What WP3 left for WP4

The tables differ from the proposal in these ways; docs/observations.md
("The observed index") has the rules.

- The rollup key is the stream key plus `form`, `bucket15`, `code`, `unit`,
  `delta` and `utc_offset_min`. Statistics are in the row's own `unit`, so
  the read converts each row to the requested unit before merging, in the
  measure its `delta` names (`temperature-difference` where true).
- `first_value` and `last_value` are `numeric`, the stored digits.
- A counter's `increase`, `resets` and `covered_ms` already include the step
  in from the stream's previous reading, so the increase of a run of whole
  buckets is the sum of its rows' and `rate` is that over the sum of
  `covered_ms`. No window function and no counter contract is needed at read.
- `twa` and `timeInState` across rows need the carry: the value a bucket's
  last point holds past its end runs until the next row's
  `bucket15 + start_gap_ms`, `last_ts + last_hold_ms` (null for no limit) or
  `last_ts + maxGap`, whichever comes first, across buckets with no row; the
  carry into the range's first bucket is the point current at its start,
  one lookup on `observed_points_stream_idx`. `internal/observed.HeldFromRows`
  is that rule, tested against raw points.
- The eligibility rule is `internal/observed.Eligible`: a bucket answers
  `twa` and `timeInState` from rows only when `maxGap` is absent or at least
  its `max_gap_ms`; otherwise its raw points.
- `percentile`, `durationIn` and thresholds read raw points; the window
  reducers in `internal/observed` are what they must equal.
- A bucket of unavailable points alone has a row with `n` 0, and a coded
  trace's nulls fall in the group with an empty `code` and `unit`.
- `observed_latest` keeps the last available value after a null; the read
  finds a later null in `observed_points` if it reports unavailability. Its
  hold column is `hold_ms`, and it adds `ts_end`, `delta`, `statistic`,
  `bounded` and `points`.
- An absent offset is 0, and an absent `source`, `about`, `channel` or
  `code` is the empty string in every key.
- The stream index is `(repository, metric, kind, source, about, channel,
  ts)`; a read over every source of a metric for a range lists the streams
  from `observed_latest` first, then reads each stream's range, because
  Postgres 16 cannot skip-scan the key.
