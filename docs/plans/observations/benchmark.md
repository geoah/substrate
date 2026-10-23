# Observed index: the benchmark

Measured on 2026-10-18 for the segment layout: one row per observed record
under a private stream id, rollups in the tiers each metric names, and the
index derived behind a rebuild by the background pass
([decision 0160](../../decisions/0160-the-observed-index-keeps-a-segment-per-record-and-a-rollup-serves-only-exact-answers.md)).

```bash
docker run -d --name substrate-bench -p 127.0.0.1:5491:5432 -e POSTGRES_PASSWORD=bench pgvector/pgvector:pg16
SUBSTRATE_BENCH_OBSERVED=1 \
SUBSTRATE_BENCH_DSN='postgres://postgres:bench@127.0.0.1:5491/postgres?sslmode=disable' \
SUBSTRATE_BENCH_ROOT=/var/tmp/substrate-bench SUBSTRATE_BENCH_DAYS=30 \
go test -count=1 -v -timeout 3h -run '^TestObservedIndexBenchmark$' ./internal/engine/
```

`TestObservedIndexBenchmark` (`internal/engine/observed_bench_db_test.go`)
refuses a database with `fsync`, `synchronous_commit` or `full_page_writes`
off. It imports the `observations` and `health` samples and writes, hour by
hour and each record through its own `Put`:

- a home of 20 sensors (temperature, humidity and power gauges, energy
  counters), a reading a minute, one closed trace per sensor and hour, its
  id the sensor and the hour, at Berlin's offset;
- three wearables: heart rate from all three, a reading every five minutes;
  steps from two, a reading a minute; sleep stages from two, one night trace
  of 30-second epochs. Their streams overlap in time.

It then reads, rewrites one day of every sensor at revision 2 (a
correction burst), patches 50 records with a property the index does not
read, rebuilds the repository, waits for the background re-derivation, and
reads again. Every metric keeps the default tiers, `15m`, `1h` and `1d`.
Since issue 991 the sample's sensor metrics keep `1h` and `1d` alone; the
section on it below has the numbers, and the results here are from before.

**Caveats.** One run, scaled down: 30 days, not a year, because a durable
write on this machine takes about 150 ms and a year of these writes is about
11 hours; the year below is extrapolated, linearly, from the 30 days. The
box is a 16-core AMD EPYC-Rome VM with 31 GB of RAM, Go 1.27.2, load average
between 2 and 6 from other work, at commit a2491521. Postgres 16 runs in a
container on the VM's root disk with its defaults (`shared_buffers` 128 MB)
and durability on; the changelog segment files are on the same disk.
`pg_test_fsync` measures that disk at about 40 ms per `fdatasync`, so every
write time below is mostly the disk's.

## Results

1,033,920 readings in 18,060 observed records (18,216 records with the
samples' declarations and catalogues).

| Table | Rows | Total bytes | Heap bytes | Bytes per reading |
| --- | ---: | ---: | ---: | ---: |
| `records` | 18,216 | 53,002,240 | 28,016,640 | 51.3 |
| `changelog` | 18,219 | 32,940,032 | 26,009,600 | 31.9 |
| changelog segment files | | 62,750,106 | | 60.7 |
| `observed_streams` | 27 | 49,152 | 8,192 | 0.0 |
| `observed_segments` | 18,060 | 9,199,616 | 3,899,392 | 8.9 |
| `observed_rollups` | 97,716 | 49,479,680 | 32,759,808 | 47.9 |
| `observed_latest` | 27 | 32,768 | 8,192 | 0.0 |

The index is 56.8 bytes a reading, against 1,377 for `observed_points`
alone in the first layout. Of the rollup rows, 76,798 are the `15m` tier,
19,920 the `1h` and 998 the `1d`: a metric that names `["1h", "1d"]` keeps
about a fifth of them. The truth, the records and the changelog, is 143.9
bytes a reading.

| Phase | Count | p50 | p95 | Took | WAL bytes | Peak heap growth |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| write, sensor traces | 14,400 | 157.7 ms | 391.8 ms | 56 min 11 s (both) | 546,392,088 (both) | 10.6 MiB |
| write, wearable traces | 3,660 | 155.2 ms | 367.7 ms | | | |
| correction burst | 480 | 160.8 ms | 248.6 ms | | 11,995,696 | 9.5 MiB |
| patch of a property the index does not read | 50 | 75.2 ms | 141.3 ms | | | |
| rebuild, 18,749 entries | 1 | | | 2 min 54 s | 429,088,048 | 14.0 MiB |
| background re-derivation after it | 1 | | | 58.5 s | 122,425,808 | 38.2 MiB |

The patch commits the same records row, changelog line and segment file
as a put, and its fingerprint stops the index there: its 75 ms is the
write path without the index, and the 80 ms more a trace put took is the
index's work and its larger record. The WAL is about 530 bytes a reading,
full-page images included. The rebuild replays the records alone (9.3 ms an
entry, durable); the re-derivation behind it took 3.2 ms a record, in pages
of 500, while the repository took writes.

| Read | Runs | Rows | p50 after the write | p95 | p50 after the rebuild | p95 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| daily means per gauge sensor, Berlin days, all 30 days | 15 | 30 | 30.5 ms | 53.2 ms | 28.4 ms | 32.9 ms |
| the same from raw points (one sensor) | 1 | 30 | 730.3 ms | | 851.1 ms | |
| hourly means per gauge sensor, last 30 days | 15 | 720 | 53.9 ms | 265.8 ms | 53.8 ms | 58.9 ms |
| 15-minute means per gauge sensor, last 7 days | 15 | 672 | 47.1 ms | 52.1 ms | 46.6 ms | 49.7 ms |
| daily steps across two sources by priority, 30 days | 5 | 30 | 929.2 ms | 1.08 s | 926.6 ms | 1.17 s |
| daily heart rate p90 of one source, 30 days | 5 | 30 | 227.6 ms | 241.8 ms | 192.2 ms | 213.5 ms |

The reads' peak heap growth was 176 MiB, the priority read's raw points.
The priority and percentile reads are served from raw points by design
(the rows keep no order across sources and no distribution), and they
derive every record they touch: about 86,000 points a month for the steps.

## A year, extrapolated

The 30 days scaled by 365/30, for the same 20 sensors and three wearables
(12.6 million readings), and for a home of 100 sensors at a reading a
minute (52.6 million readings a year) at the same bytes a reading:

| | 20 sensors and 3 wearables | 100 sensors |
| --- | ---: | ---: |
| index (segments, rollups, latest, streams) | 0.72 GB | 3.0 GB |
| of which the `15m` tier | about 0.47 GB | about 2.0 GB |
| records and changelog (table and files) | 1.8 GB | 7.6 GB |
| WAL written | 6.7 GB | 28 GB |
| writes, if sent one after another (a backfill) | about 11 h on this disk | about 38 h |
| rebuild | about 35 min | about 2.2 h |
| background re-derivation after it | about 12 min | about 47 min |

Written live, the 20 sensors are 20 writes an hour; the write time matters
for a backfill alone. A year of daily means reads one `1d` row a day and
sensor, 365 rows, and the hour rows of the two days a year the clock
changes.

## The settle's statements (issue 990, 2026-10-23)

The 80 ms a trace put took over a patch was blamed on the settle's round
trips. The benchmark now counts each put's statements and the time they
take (a pgx tracer), and the count says the round trips were not most of
it. Two runs of 7 days (`SUBSTRATE_BENCH_DAYS=7`, 4,214 puts each), same
box and container as above, at commit 62810042 with the settle as it was and
then with the settle batched to one statement per step. Other work kept the
load average between 3 and 20 through both runs.

| Per put, write phase | Before | After |
| --- | ---: | ---: |
| statements | 63.4 | 53.4 |
| of them on observed_ tables | 21.4 | 11.4 |
| time in those statements | 15.5 ms | 13.3 ms |
| sensor put, p50 | 145.3 ms | 141.8 ms |
| wearable put, p50 | 142.6 ms | 138.6 ms |
| sensor put, p95 | 331.8 ms | 373.0 ms |
| write WAL | 108,819,280 bytes | 110,654,184 bytes |

The settle now sends 11 statements for a trace put: one read of the old
segments with their streams, one write of the segments and extents, one
read and one write per rollup tier, and the latest row's read and write.
That saves about 2 ms a put. The p50s moved by less than the runs' own
noise: the patch, which the change does not touch, measured 91.9 ms in the
first run and 110.7 ms in the second. Where a put's time goes, from the
tracer's split of a one-day run: `COMMIT` is 35 ms (the disk's sync), the
observed statements 13 ms, the 15 `property_managers` inserts a put 7 ms,
and about 85 ms is outside any statement. That last part holds validating
and deriving the readings and writing and syncing the changelog segment
file; the tracer does not split it further.

## Sensor metrics without the 15-minute tier (issue 991, 2026-10-23)

The observations sample's six shared metrics (the home sensors' metrics)
now declare `rollups: [1h, 1d]`; the wearables' health metrics keep the
default. One 7-day run, same box, after the settle change above, against
that run with every metric at the default. The load average was between 6
and 8.

| | Default tiers | Sensors at `1h` and `1d` |
| --- | ---: | ---: |
| rollup rows | 22,806 | 9,366 |
| of them `15m` | 17,919 | 4,479 (the wearables') |
| `observed_rollups`, total bytes | 11,288,576 | 4,497,408 |
| index bytes per reading | 55.4 | 27.2 |
| sensor put, p50 | 141.8 ms | 115.1 ms |
| observed statements a put | 11.4 | 9.8 |
| write WAL | 110,654,184 bytes | 98,784,704 bytes |
| 15-minute means per sensor, last 7 days, p50 | 45.0 ms | 146.3 ms |

The sensors' index is now about 16 bytes a reading (3,500 rollup rows and
3,360 segments for 201,600 readings), from about 50. The cost is the
15-minute read, which takes the raw points: 146 ms for 672 buckets a sensor,
where the `15m` rows took 45 ms. Hourly and daily reads are unchanged. Part
of the faster put is the lower load during this run.

**Shrinking the rows.** The record id columns (`first_record_id`,
`last_record_id`) are about 47 bytes of a row's 318 bytes of tuple data
(`pg_column_size`): 848,690 of 5,708,760 bytes in the default run's `15m`
tier, 441,230 of 2,855,620 bytes over every tier in this run. A compact form
(a segment's numeric id and an ordinal) would save about 35 bytes a row:
about 330 KB here, 1.4 bytes a reading, about 17 MB a year at this
workload. It needs a migration and a surrogate key on `observed_segments`,
and is not done.

## Before: one row per point (2026-10-11)

These are the first layout's numbers, kept for comparison: one row per
point in `observed_points`, 15-minute rollups alone, the index derived inside
the rebuild's transaction, on tmpfs with durability off. The test of the
same name now runs the workload above.

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

### Results

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

### A dense bucket

The hourly streams above put at most 900 points in one 15-minute bucket.
Review round 1 (finding 13) asked what one dense bucket costs, because each
write recomputes its buckets from every point in them:

```bash
SUBSTRATE_BENCH_OBSERVED=1 go test -count=1 -v -run '^TestObservedDenseBucketBenchmark$' ./internal/engine/
```

`TestObservedDenseBucketBenchmark` writes 40 power traces of 1000 points
each, all in the same 15-minute bucket of one stream (40,000 points), then
patches one trace's `quality` and rebuilds the repository. The peak heap is
the process's live heap above where it stood after a collection, sampled
every millisecond: a floor of the true peak, background work included. Same
box and container settings as above, load average about 5 from other work,
one run of each on 2026-10-11. "Before" is commit 3135d2ed, which read a
touched bucket's points into memory whole and doubled a rebuild's page until
a bucket fit; "after" is the change that streams each bucket in pages of
20,000 points through `observed.BucketBuilder`.

| Dense bucket: 40 traces of 1000 points | Before | After |
| --- | ---: | ---: |
| mean put, first 5 | 119.65ms | 121.98ms |
| mean put, last 5 | 1.048s | 797.53ms |
| most rows one read of points answered | 40000 | 20000 |
| patch one trace: wall time | 1.215s | 939ms |
| patch one trace: peak heap growth | 46963 KiB | 5834 KiB |
| rebuild: wall time | 4.480s | 3.322s |
| rebuild: peak heap growth | 67288 KiB | 34489 KiB |

What a recompute holds is now one page and each group's running state,
whatever the bucket holds. Its work is not bounded by the write: a put still
reads every point of each bucket it touches once, so the last put into this
bucket costs about seven times the first, and writing N traces into one
bucket reads on the order of N squared over 2 traces' worth of points in
all. The per-repository observed lock is held for that read. Cutting the
work itself needs a recompute that merges the written record's points into
the stored rows, which the hold and counter carry across a bucket make more
than a sum; it is not done.

### Should the layout be frozen

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

### What WP3 left for WP4

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
