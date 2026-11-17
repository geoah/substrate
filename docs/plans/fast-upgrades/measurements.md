# Measurements behind the fast-upgrades plan

Status: measured 2026-11-16 and 2026-11-17. These are the numbers [fast
upgrades](../fast-upgrades.md) and its [postmortem](postmortem.md) rest on.
Two sources:

- **The production upgrade**: an 11.7M-line repository on Postgres 16, with
  `pg_stat_statements`, as the postmortem describes.
- **A generated history** on a laptop: a Linux container in Docker on an
  Apple-silicon Mac with 12 cores given to Docker, and Postgres 16 with
  pgvector in a container beside it, on tmpfs with durability off, as the
  engine's tests run it. Wall times there are this machine's and no promise.
  Buffers read per statement depend on plans, statistics and data rather
  than on the machine's speed, and they are what the plan's scale test
  watches.

The generator was a throwaway test in `internal/engine` and is not in this
change; the plan's `TestReplayCostStaysFlat` is what it becomes.

## The generated history

It writes, through the ordinary write path, 12,000 rounds of:

- one write of a **hot record**, an account whose cursor moves every round;
- one write of a **link** whose repeated reference grows by one entry a
  write, to 1,000 entries, after which the next link starts;
- two **trigger deliveries** on two triggers, each a claim parked in one
  transaction (`foldPark`) and retired with the cursor moved in the next
  (`foldUnpark`, `foldCursor`), as an agent delivery does;
- four ordinary writes spread over **150 kinds**, each declaring three
  indexes, so `records` carries 528 indexes, against production's 508.

That is 121,286 lines and 411 MB of payload as text, 250 MB as stored
`jsonb`: Postgres's own compression of the payloads is 1.6×.

## The replay against its position

`RebuildRepository` replayed it, once on `main` and once on the tip of the
#1482–#1484 stack. Every 5,000 lines, inside the replay's own transaction
(`WithTestReplay`), each probe ran under `EXPLAIN (ANALYZE, BUFFERS)` in a
savepoint that was rolled back; the statements per line came from
`WithTestReplaySQL`.

The probes, each named by what it stands for:

- **unpark**: `DELETE FROM trigger_failures WHERE id = $1 AND trigger_id = $2`
  (`foldUnpark`), for a trigger that parks and retires a claim on every
  delivery;
- **cursor**: the trigger cursor's `INSERT … ON CONFLICT` (`foldCursor`);
- **hot row**: the `SELECT` of the record rewritten every round, by its key;
- **hot marks**: a read of that record's `property_marks` rows, by its key;
- **refs**: `DELETE FROM refs WHERE src_kind = $1 AND src = $2` for a link.
  On `main` it is the last link, which the replay reaches at its end; on the
  stack it is the first, which the replay finishes by line 10,000.

Buffers read per call, at the line the replay had reached:

| Line | `main`: lines/s | `main`: statements a line | stack: lines/s | stack: statements a line | unpark | cursor, `main` | cursor, stack | hot row | hot marks |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 5,000 | 473 | 17.8 | 982 | 3.8 | 27 | 12 | 9 | 293–327 | 282–284 |
| 10,000 | 81 | 66.0 | 682 | 3.6 | 60 | 23 | 6 | 593–635 | 648–650 |
| 20,000 | 75 | 66.0 | 631 | 3.6 | 127 | 46 | 6 | 1,021–1,063 | 1,379–1,381 |
| 40,000 | 67 | 65.8 | 540 | 3.3 | 261 | 92 | 6 | 2,174–2,216 | 2,788–2,790 |
| 60,000 | 68 | 64.8 | 507 | 2.4 | 2 | 137 | 6 | 3,355–3,397 | 3,338–3,340 |
| 80,000 | 65 | 64.8 | 413 | 2.4 | 2 | 183 | 6 | 4,536–4,578 | 3,886–3,888 |
| 100,000 | 67 | 64.8 | 364 | 2.4 | 2 | 228 | 6 | 5,718–5,760 | 4,436–4,438 |
| 120,000 | 54 | 64.8 | 244 | 2.4 | 798 | 274 | 6 | 6,897–6,939 | 4,986–4,988 |

Where two numbers stand in one cell they are the two runs, which read within a
few percent of each other. The unpark read the same in both. The whole
replay took 1,553.5 s on `main` (78 lines/s, 52.5 statements a line) and
264.8 s on the stack (458 lines/s, 2.8 statements a line).

What it shows:

- **#1482–#1484 did what they said.** On `main` a link's write deleted and
  inserted again every `refs` row it held. At the end of the run, the last
  link's delete read 382,507 buffers a call, up from 69,895 at line 115,000.
  The stack replays the same history 5.9 times faster with about a
  nineteenth of the statements, and its cursor upsert stays at 6 buffers where `main`'s grew to
  274.
- **The stack still slows down 4× in 121,000 lines.** It goes from 982
  lines/s to 244. Two of the growths are ones no pull request has touched.
  Reading the hot record's row grows by about 0.57 buffers for every time the
  record was rewritten, to 6,897. Its marks grow to 4,988. Each is a key looked
  up through an index that holds an entry for every version the transaction
  wrote of it, and nothing prunes those before the commit. Production's
  `INSERT INTO records`, at 608 buffers a call, has the same shape.
- **The unpark's plan is a coin toss.**
  - In both runs the unpark walked `trigger_failures_trigger` up to line
    45,000, at 294 buffers and rising.
  - From line 50,000 to line 110,000 it took the primary key, at 2 buffers.
  - By line 115,000 it was back on the trigger index, at 764 buffers.
  - Production stayed on the trigger index to line 6.2M.

  The planner prices the plan from an emptied table's statistics, scaled by
  the table's current size. Where that lands is nothing to build on.
- **The cause is the one transaction, shown.** Once the stack's replay
  committed, the same probes read about as much: 6,989 buffers for the hot
  row, 5,024 for its marks and 1,010 for the unpark, because the versions are
  still there. After a `VACUUM (ANALYZE)` of the fold tables they read 4, 6
  and 0 (the last a sequential scan of a table now known to be empty).
  Committed batches, with pruning and fresh statistics between them, hold
  every statement near those numbers.

## What a line costs apart from SQL

Over all 121,286 lines, one goroutine, outside any replay, in each run (the
two ran side by side for part of the time, which is the difference):

| Work | `main` run | Stack run | Per line | Per GB of payload |
| --- | --- | --- | --- | --- |
| Read every row from the table (`payload::text`) | 1.9 s | 2.1 s | 16–17 µs | 4.6–5.1 s |
| Hold every row to its checksum: canonical encode and SHA-256 | 11.7 s | 13.1 s | 96–108 µs | 28–32 s |
| Decode every payload for the fold (`decodeNumberPreserving`) | 4.4 s | 5.2 s | 36–43 µs | 10.7–12.6 s |

The replay does not check checksums (`tableCursor` reads, it does not
verify). On the stack, whose replay took 264.8 s, decoding every payload took
5.2 s and reading every row 2.1 s: **2.0% and 0.8% of the replay**. On `main`,
4.4 s of 1,553.5 s is 0.3%. Production's replay of about 10 hours decoded
24.7 GB. At this machine's 12.6 s a GB that is about 5 minutes, under 1% of
the replay. A decoder that cost nothing would save that share and no more.

## Encodings

One line of each shape from the generated history, in Linux on the same
machine. The protobuf column is the line as a `structpb.Struct`, which is
what a protobuf line must be for the payload: a `record` effect's property
values are whatever the kind declares, so a typed protobuf message covers the
line's envelope and nothing in it.

| Shape | JSON bytes | zstd of the JSON | protobuf `Struct` | zstd of the protobuf | Verify (encode and SHA-256) | Decode for the fold | `json.Unmarshal` into a map | protobuf `Unmarshal` | zstd decompress | Lift through steps 3, 4 and 5 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Link write, 1,000 entries | 57,577 | 17,577 | 61,683 | 17,973 | 1.27 ms | 534 µs | 480 µs | 511 µs | 38 µs | 805 µs |
| Hot record write | 782 | 356 | 875 | 497 | 27.5 µs | 6.5 µs | 9.8 µs | 9.9 µs | 2.3 µs | 13.5 µs |
| Ordinary write | 568 | 325 | 645 | 441 | 21 µs | 4.4 µs | 6.0 µs | 7.7 µs | 2.3 µs | 8.3 µs |
| Park | 565 | 358 | 630 | 466 | 16.1 µs | 3.5 µs | 6.5 µs | 5.8 µs | 2.3 µs | 6.8 µs |
| Unpark | 484 | 287 | 538 | 366 | 15.9 µs | 3.3 µs | 5.2 µs | 6.7 µs | 2.1 µs | 6.2 µs |

- **Size.** The protobuf `Struct` is 7 to 14% larger than the canonical JSON.
  zstd shrinks the link line 3.3× and a small line 1.4 to 2.2×, against
  Postgres's 1.6× over the whole history.
- **Speed.** The protobuf decode is no faster than the JSON one. Checking a
  line's checksum costs two to five times decoding it, because it encodes
  the line again in the canonical form.
- **What this does not measure.** A typed protobuf envelope around a compact
  binary encoding of the property values was not built, and neither was a
  stored form whose checksum is checked by hashing the stored bytes. The
  numbers rule out the generic `Struct` form for these shapes, and say
  decoding is a small share of a replay; they do not rule out every binary
  form.
- **Where the bytes are.** A link's 1,000th write is 57.6 KB, and its 1,000
  writes leave about 29 MB of history: each carries the whole list. Written as
  one added entry each, the same 1,000 writes would be about 0.6 MB, about
  fifty times less.
- **A lift.** Steps 3, 4 and 5 over a line already in their dialect, which
  they leave unchanged, cost about one and a half decodes. The three steps
  each parse the payload; a chain that decodes once and runs every step on
  the tree would cost about one decode.

## The floor of a bulk build

A third run on the stack wrote 4,000 rounds with twelve ordinary writes each,
every one a new record, for a larger fold: 73,286 lines, 196 MB of payload,
and a fold of 248,414 rows in `records`, `refs` and `property_marks`. Its
replay took 119.3 s (614 lines/s). Then, on the fold it had made:

| Work | Rows | Time | Rate |
| --- | --- | --- | --- |
| Copy the fold into new tables with no index | 248,417 | 0.15 s | 1.7M rows/s |
| Their primary keys and two main indexes, after | | 0.12 s | |
| **The swap's floor**: insert the fold into tables that carry every index the live ones do, 528 on `records` | 248,414 | 2.05 s | 121,000 rows/s |
| **A fold migration's floor**: one set-based `UPDATE` of every row of the three tables | 248,414 | 2.68 s | 93,000 rows/s |
| One per-kind partial index on the copied `records` | | 0.012 s | |
| The replay that made that fold | 73,286 lines | 119.3 s | 614 lines/s |

The second run's fold, 91,438 rows, took 0.75 s for the same `UPDATE`
(122,000 rows/s).

So putting a whole fold into place costs about a sixtieth of replaying the
history that made it, and rewriting a whole fold about a forty-fifth, on a
history this short. The gap grows with the history, because the fold grows
with the records a person keeps and the history with every write ever made.
A per-kind partial index is built by scanning all of `records`, so building
450 of them again after a bulk load scans the table 450 times. On production,
restoring the 437 dropped for the replay took 8 minutes in six parallel
sessions, 55 to 60 a minute. That is the largest part of a bulk build after
the reading, which is why the plan builds them behind the open.

## Production numbers this rests on

| What | Number | Source |
| --- | --- | --- |
| Conversion walk, before #1463 | About 1,200 lines/s, one core | The upgrade's log |
| Conversion walk, after #1463 | About 7,800 lines/s on 16 cores; walks of 50 min, 1 h 42, 42 min | The upgrade's log |
| Comparison with the 0.x directory | 500–750 lines/s, CPU-bound | The upgrade's log |
| Replay, start of the first run | 196 lines/s | The upgrade's log |
| Replay, after #1482–#1484 and 437 indexes dropped | 10 h 32 min for 11.7M lines, 200–445 lines/s: about 440 from line 6.6M to 9.0M, then 270 falling to 200 to the end | The upgrade's log |
| The replay's commit and `ANALYZE` | 1.5 min | The upgrade's log |
| Replay, in the heaviest link stretch before #1484 | 43 lines/s | The upgrade's log |
| Statements per line | About 10 | `pg_stat_statements` |
| `DELETE FROM trigger_failures …`, line 6.2M | 31.7 ms and 8,747 buffers a call, about 700 calls a minute, about 38% of wall time | `pg_stat_statements`, `EXPLAIN` as `substrate_app` |
| The same, at the end | 47 ms and about 14,000 buffers a call; about 58% of wall time over the last two hours, 52.6 s of one 90 s sample | `pg_stat_statements` |
| `INSERT INTO records` | 1.8 ms and 608 buffers a call over 497,000 calls | `pg_stat_statements`, last 95 min |
| Go process during the replay | About 0.65 of a core | The process's CPU use |
| First open after the replay | 29 min: repository migrations 6–14 about 5.5 min (migration 9's scan of the changelog about 4.5), the boot upgrade 5 min 23 s with 9 mappings relinked, the actor fill's scan of the 68 GB table 17 min 24 s | The server's log |
| Behind the open | The search index of 1.55M rows at about 1,000 rows/s; one reshaped kind's indexes at about 12 rows/s | The server's log |
| 437 per-kind indexes restored on `records` | 8 min in six parallel sessions, 55–60 a minute | The restore's log |
| Cutover downtime | About 2 min: the old server stopped at 3:25:56 pm, the new one listened at 3:26:17 and passed readiness at 3:27:51 | The platform's events |
| The roll forward two hours after the cutover, with no conversion | One SQL migration in 34 ms, listening after about 1 min, then 9 provider upgrades in 25 s | The server's log |
| The history by kind, after the cutover | 11,892,975 lines. `core/triggerrun` 5,488,566 lines and 1.98 GB, `core/trigger` (its `delivery` lines) 2,440,726 and 10 GB, five sync-progress kinds about 63,700 and 39 MB, `core/alert` 214 and 97 kB: about 67% of the lines and half the payload | A count by kind over the `changelog` table |
| The archive catching up after the conversion | Not measured. At the default 8 MiB/s a pass copies about 27 GB an hour, and pauses while functions run | `docs/operations.md`, `SUBSTRATE_ARCHIVE_BYTES_PER_SECOND` |

## From these to 11.7M and 100M lines

The rates above are this laptop's, so each figure below is a floor, not
an estimate of a deployment. Each assumes 8 cores for the parts that run in
parallel, and is given against what production measured.

| Phase | How it scales | 11.7M lines, 24.7 GB | 100M lines, about 210 GB, same fold |
| --- | --- | --- | --- |
| Today: conversion walks | Lines × steps | About 5 h, measured | About 40 h |
| Today: replay through the write path | Lines, at best | 10.5 h, measured | About 90 h if it stays flat, which it does not |
| Batched replay through the write path | Lines | About 8 h at the 350–400 lines/s production started at | About 70 h |
| Read and decode every line (bulk build) | Bytes: 17.7 s a GB on one core | About 1 min | About 8 min |
| Checksum every line, if the build checks them | Bytes: about 30 s a GB on one core | About 1.5 min | About 13 min |
| Put the fold in place (bulk build, checkpoint load) | Fold rows: 121,000 a second with every index | About 2.5 min for 17M rows | The same |
| Fold migration | Fold rows: 93,000–122,000 a second | About 3 min for 17M rows | The same |
| Lift on read, at the upgrade | Nothing | One row | One row |

The 17M fold rows assume about ten `refs` and mark rows for each of the 1.55M
records; production's counts were not taken. The bulk build's floor leaves
out building each record in memory, which costs about what decoding does,
the lines with effects on other records, which are few, and the per-kind
partial indexes. Hence the plan's "under 30 minutes" and "under an hour".
