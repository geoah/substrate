# Postmortem: upgrading an 11.7M-line repository from v0.123.1

Status: written 2026-11-17, after production went live on the new version
and rolled forward once more. The fix
for what it found is the plan in [fast upgrades](../fast-upgrades.md), and
the measurements behind both are in [measurements](measurements.md).

## Summary

A production repository of 11.7 million changelog lines was upgraded from
v0.123.1 to a build of `main`. The new binary had to rewrite and replay the
repository's whole history before it would serve, and its first open read
that history twice more. The work ran on a copy of the database while
production kept serving v0.123.1. Production was down for about 25 minutes
when the snapshot was taken and about 2 minutes at the cutover. It took 49
hours 25 minutes from the snapshot to the new version going live:

- About 24 hours were the design's own cost. Even run without a mistake, the
  current upgrade path walks this history seven times and replays it once,
  in the boot, before the server listens, and its first open then reads it
  twice more.
- About 14 hours were lost to our own process: an overnight step hung on a
  watcher inside an agent session, a startup probe shorter than our own
  estimate, and a conversion that was never dry-run against the real
  history.
- About 10 hours of replay progress were thrown away. The replay slowed down
  with every line, four times over. Each time we found the cause, fixed it
  and started the replay again from the beginning.

Writes made to production after the snapshot are lost at the cutover, except
where a sync fetches them again. So is every credential production rotated
meanwhile: one provider's OAuth refresh token, rotated during the two days,
was refused after the cutover, and that account had to be connected again
by hand.

Two things after the cutover belong to the same story:

- **A destructive default almost erased history.** The release shipped
  compaction on by default with deletions kept 30 days. Its first pass would
  have erased the whole history of every record deleted more than 30 days
  before. It was found by chance, an hour after the cutover, and turned off
  before the pass reached the repository. Nothing was erased.
- **An upgrade that walked no history was a restart.** The next roll forward,
  to a later build of `main` with one SQL migration and no conversion, cost
  about a minute of downtime.

## The repository

| What | Size |
| --- | --- |
| Changelog | 11,706,646 lines, 51.25M effects |
| Effect payloads | 24.7 GB as JSON |
| `changelog` table | 68 GB: a 21 GB heap, the rest TOAST and indexes |
| Fold tables, mid-replay | `records` 24 GB (6 GB heap, the rest dead versions the long transaction held), `refs` 11 GB, `embeddings` 11 GB, `property_marks` 10 GB |
| Database | 125 GB |

The payloads are not evenly spread. Lines 2.0–2.8M and 5.0–5.9M carry 200 to
1,700 MB of payload per 100,000 lines; elsewhere it is 40 to 180 MB. The heavy
stretches are mostly one sync app's link records. Each write appends one
entry to a reference list of up to about 1,500 entries, and a `record` effect
carries a property's whole value, so every append writes the whole list again.
The same record is rewritten thousands of times.

Most of the history is not the owner's data at all. Of the 11,892,975 lines
the repository held after the cutover:

| Kind | Lines | Payload |
| --- | --- | --- |
| `core/triggerrun`, a run record each delivery writes | 5,488,566 | 1.98 GB |
| `core/trigger`, the `delivery` lines that move a trigger's ledger | 2,440,726 | 10 GB |
| Five providers' sync-progress kinds | about 63,700 | about 39 MB |
| `core/alert` | 214 | 97 kB |

About 67% of the lines and about half the payload are the substrate's own
per-delivery bookkeeping. Almost every delivery appends a line addressed to
its trigger, at about 4 KB a line on average.

## What the first open had to do

| Stage | What it does | How long |
| --- | --- | --- |
| 1. SQL migrations 0020–0031 | Builds indexes on `changelog`, in the boot | The first index took 21 min on spinning disk |
| 2. Repository id (decision 0261) | Rewrites 21.4M rows from the name to the id | 55 min |
| 3. Boot check | Holds the 0.x history directory to the table line for line: 57 GB, CPU-bound at 500–750 lines/s | About 6.5 h, after #1455 removed a quadratic re-read of a 256 MB segment every 500 lines |
| 4. Conversion, dialect 1 to 8 | Steps 2–8 in one transaction; each step walks all 11.7M lines and rewrites rows in place | 4 h 57 min after #1463 |
| 5. Refold | Replays the whole converted history into a fresh fold, in one transaction, through the live write path | 10 h 32 min for the final run, at 200–445 lines/s; its commit and `ANALYZE` 1.5 min |
| 6. Repository migrations 6–14, at the first open | Ordinary writes through the changelog; migration 9 (`link_actors`) scans the changelog for the actors it links | About 5.5 min, 4.5 of them migration 9's scan |
| 7. Boot upgrade of the shipped vocabulary, at the first open | Brings the shipped packages to this binary's versions and relinks 9 shipped mappings | 5 min 23 s (11 min on v0.123.1's first open; 35 min in an earlier release) |
| 8. Actor fill, at the first open | "Reading the repository's history once for the actor records it links": a second whole scan of the 68 GB table, which the open waits for | 17 min 24 s |
| Behind the open | The search index of 1.55M rows re-derived under new rules (about 1,000 rows/s), and one reshaped kind's indexes (about 12 rows/s) | Not blocking; the production server started both again from the beginning after the cutover |

The first open took 29 minutes in all, from the listener (2:24:47 pm) to the
open (2:53 pm). Stages 2 and 3 are one-time costs of leaving 0.x: no later
upgrade has a 0.x directory to compare or a name to re-key. Stages 6 and 8
also ran once, for what this release changed, but they are the shape every
release repeats: an open that reads the history. Stages 1, 4, 5 and 7 are how
every upgrade works today, and each grows with the repository.

- **The conversion (stage 4).** The old code ran on one core with one `UPDATE`
  per row, at about 1,200 lines/s: more than a day for this history. #1463
  spread each page over every core and wrote a page's rewrites in one
  statement, about 7,800 lines/s. The walks still took 50 min, 1 h 42, 42 min
  and so on: each step does different work on a line, and every walk crosses
  the heavy stretches.
- **The refold (stage 5).** A declarations pass and then one full replay
  (#1470); before that it was two full replays. The live write path,
  `fold.go`, runs about 10 SQL statements per line. The first run started at
  196 lines/s. The final run, after four fixes, held about 440 lines/s from
  line 6.6M to 9.0M, then fell to 270 and on to 200 by the end, as the
  `trigger_failures` delete below grew.

## Timeline

Pacific time. "Needed" is work the current design requires; the rest was
lost.

| When | What happened | Cost |
| --- | --- | --- |
| Sat 2:03 pm | Snapshot and clone. Production restarted on v0.123.1, about 25 min of downtime. | |
| Sat 2:03–2:49 pm | False starts. The clone sat on spinning disk at 6.5 MB/s, and a metrics sidecar's readiness probe took the database out of its service while a migration held locks. The database moved to SSD. | About 45 min |
| Sat 2:49–3:44 pm | Repository id migration | 55 min, needed |
| Sat 3:44–10:21 pm | Comparison of the 0.x directory with the table; #1455 deployed part way | About 6.5 h, needed |
| Sat 10:21 pm – Sun 8:09 am | The swap to the fast conversion image hung on a watcher inside an agent session. The session ended, so the old, slow code ran. At 4:27 am the 12 h startup probe, set below our own estimate, killed the boot, and it restarted on the old code. | About 10 h lost |
| Sun 8:09 am – 12:23 pm | The fast conversion refused at the start of step 6 (decision 0259). The conversion is one transaction, so all four hours rolled back. | About 4 h lost |
| Sun 12:42–5:39 pm | Conversion rerun with #1473; committed | About 5 h, needed |
| Sun 5:45 pm – Mon 3:42 am | Refold: four starts, each after fixing a slowdown found at scale | About 10 h of replay progress discarded |
| Mon 3:50 am – 2:24 pm | The final replay, to 2:22 pm, then its commit and `ANALYZE` | 10.5 h, needed |
| Mon 2:24–2:53 pm | The first open, on the converter: repository migrations, the boot upgrade and the actor fill | 29 min, needed |
| Mon 2:53–3:25 pm | The 437 per-kind indexes restored (8 min, in six parallel sessions) and the cutover prepared | About 30 min |
| Mon 3:25:56–3:27:51 pm | Cutover: the old server stopped, the new one listened 21 s later and passed readiness 1.5 min after that | About 2 min of downtime |

From the snapshot to live: 49 hours 25 minutes. After it:

| When | What happened |
| --- | --- |
| Mon 4:38 pm | The release's defaults turn out to run compaction with deletions kept 30 days (`SUBSTRATE_DELETIONS_KEPT=720h`). Compaction is turned off in a 45-second restart, before its first pass reaches the repository. |
| Mon 5:25 pm | Roll forward to a later build of `main` carrying #1473, #1482–#1484 and decision 0333's change: one SQL migration in 34 ms, listening after about a minute, then nine provider upgrades in 25 seconds |
| Mon 5:40 pm | Compaction back on, with every deletion kept (`SUBSTRATE_DELETIONS_KEPT=0`) |

## What went right

- **Production stayed up.** It served v0.123.1 throughout, because all the
  work ran on a ZFS clone of its database and data root.
- **The cutover took 2 minutes.** The converter had already done the open's
  one-time work, so the production server's first open on the converted
  database had little left to do.
- **Every attempt could start over cleanly.** The changelog is checksummed and
  every stage either commits whole or leaves the history as it was, so each
  restart began from a known state and nothing was corrupted.
- **The speed-ups were provably safe.** The replay is deterministic, so a test
  could hold a new path to the old one: #1470's new refold against two full
  replays on the kept v0.123.1 directory, #1482's skipped `refs` rewrites
  against the live fold.
- **Profiling found every cause quickly.** Buffers per call that grew from one
  sample to the next in `pg_stat_statements` pointed at each slow statement
  within minutes of looking.
- **Each fix landed with a test that fails without it.**
- **The consent door caught a shorter history.** The provider upgrades on
  offer declared 30 days of history for every kind they mirror.
  `substratectl add` refuses an upgrade that keeps a kind's history for less
  time unless `--allow-shorter-history` takes it. The owner's tooling, which
  applies provider upgrades through it, stopped at the first refusal. Decision
  0333 now keeps a mirror's whole history.
- **An upgrade with nothing to walk was a restart.** The roll forward two
  hours after the cutover ran one SQL migration in 34 ms and listened after
  about a minute.

## What went wrong in the system

### The upgrade walks the whole history, in the boot, several times

Every stage after the id migration is proportional to the history, not to
what changed. A conversion walks every line once per step, then the fold is
rebuilt by replaying every line again. All of it happens in the boot check,
and the server listens only once every repository on it is done. Nothing
about the release made this repository's 11.7M lines relevant: step 6 changed
how a kind is spelled, and the fold already held every record.

### All-or-nothing transactions turn one bad line into hours lost

The conversion of all steps is one transaction, and so is the replay. That
gives a strong guarantee: a fold is never committed half-built, and a
crash leaves everything as it was. The price is that a refusal at the start
of step 6 discarded four hours, and each restart of the replay to deploy a
fix discarded everything it had replayed.

### One long replay transaction makes its own per-line cost grow

The replay runs the live write path, statement by statement, inside one
transaction. Two things then hold for its whole length:

1. **It plans against statistics from an empty fold.** The replay empties the
   fold tables first, and nothing the transaction writes reaches the planner's
   statistics until it commits. `plan_cache_mode = force_custom_plan` re-plans
   every statement, but against those statistics, so a plan that is right for
   an empty table is chosen again at line 6 million.
2. **Every row it rewrites or deletes stays in its own index scans.** No
   version the transaction wrote can be pruned before it commits, so a row
   rewritten a thousand times leaves a thousand index entries that every
   later lookup of that key walks.

Five slowdowns of this one class were found during this upgrade, each only at
scale:

| Where | What grew | Fix |
| --- | --- | --- |
| `records` | 508 indexes, 494 of them per-kind partial expression indexes. Every insert maintained all of them and every plan considered them. | Worked around: 437 were dropped for the replay and restored after. |
| `refs`, unchanged writes | Every write of a record deleted and re-inserted all its `refs` rows; the delete reached about 7,000 buffers a call. | #1482: a replay leaves the rows standing when a write derives the same ones. |
| `refs`, growing lists | A list growing by one entry changes on every write; the delete reached about 30,000 buffers a call and the replay fell to 43 lines/s. | #1484: a replay syncs `refs` rows by key, so an append is one insert. |
| Trigger cursor, schedule and page rows | Each delivery upserted its trigger's row; the cursor upsert grew from 0.4 ms to 5.7 ms within 1.9M lines. | #1483: a replay holds those rows and writes each once, at its end. |
| `trigger_failures` | `DELETE FROM trigger_failures WHERE id = $1 AND trigger_id = $2` (`foldUnpark`, `internal/engine/delivery.go:110`). At line 6.2M it took 31.7 ms and 8,747 buffers a call, about 700 calls a minute, about 38% of wall time, up from about 11 ms an hour and a half before. By the end it took 47 ms and about 14,000 buffers a call and about 58% of wall time over the last two hours (52.6 s of one 90 s sample). The planner, trusting the emptied table's statistics (4 pages, 8 rows), walks `trigger_failures_trigger (repository, trigger_id, parked_at)` with a filter on `id` rather than the primary key `(repository, id)`, and the table had grown to 10,450 pages by line 6.2M, of versions nothing could prune. Each unpark walks every failure its trigger parked during the replay: quadratic per trigger. | Not fixed. |

Nothing outside the transaction can fix one of these while it runs: an index
built or dropped from another session, or an `ANALYZE`, reaches the replay
only after it commits. Each fix meant stopping, deploying and starting again.

The class is not used up. A generated history with the same shapes, replayed
on the tip of the #1482–#1484 stack ([measurements](measurements.md#the-replay-against-its-position)),
still slows down 4× in 121,000 lines. Reading a record that is rewritten every
round grows by about half a buffer per rewrite, and so does reading its
property marks. The unpark's plan flips between the trigger index and the
primary key as the table grows, and back. Once the replay commits and the
tables are vacuumed, every one of those reads costs 0 to 6 buffers.

The last `pg_stat_statements` reading of the final run, over 95 minutes, shows
the shape: the unpark above at 20.8 ms a call and growing, `INSERT INTO
records` at 1.8 ms and 608 buffers a call over 497,000 calls, the record
row's `SELECT` at 0.54 ms, and the `property_marks` insert at 0.056 ms over
3.3M calls. The Go process used about 0.65 of a core: the replay waits on
Postgres, not on decoding.

### The first open reads the history twice more

After the replay, the first open took 29 minutes, and every request on the
repository waits for an open. Two of its parts read the whole history again:
repository migration 9 scanned the changelog for the actors it links (about
4.5 minutes), and the actor fill read the 68 GB table once more for the
actor records the history links (17 minutes 24 seconds). The boot upgrade
relinked 9 shipped mappings inside the open (5 minutes 23 seconds). Each ran
on the converter, so the production server found them done. Each would have
held a repository upgraded in place for as long.

### Two correctness bugs, found only by real history (#1473)

Conversion step 6 reads each line by the kind declaration in force when it
was written. It refused real history twice over: 384 kind versions written
before decision 0098 bind a trait by its bare word (`traits: [oauth2]`), and
49 `core/unit` records were written by a repository migration one transaction
before `core/unit` was declared. A 10-minute offline run of step 6's pure
functions over the real declaration lines found both, but only after a
four-hour run had failed on the first.

The fix ran the production conversion before any review. A review afterwards
found four faults in it:

- **A bare trait name was resolved against every trait ever declared,** not
  against those that governed the line.
- **The fallback for a shipped kind's early lines accepted any writer's
  line** written before the declaration, not only the engine's own.
- **It read a kind's last version at the kind's own seq.** That missed traits
  declared later in the same batch, which matters for stored
  `Idempotency-Key` answers.
- **A scan of the candidate traits ran on every line,** quadratic at worst.
  It is now memoized.

The real history was checked against each. It held three lines whose answer
depended on time, all with the kind and its trait declared in the same
transaction, and 49 early lines, all units the seed and the system wrote. So
production was unaffected, by luck rather than by review. #1484's CI also
caught that the emergency branch's new fold-writing functions were missing
from the guard that lists them (`TestFoldPathWritesOnlyFromListedFunctions`).
The branch images that ran in production had not passed CI.

### Two bugs that made a walk quadratic or single-threaded

The comparison re-read a 256 MB segment from its start every 500 lines
(#1455), and the conversion ran one line at a time on one core with one
`UPDATE` per row (#1463). Neither shows on a history of thousands of lines.

### The append-growing list makes history quadratic in bytes

A `record` effect sets a property's whole value. A list that grows by one
entry a write therefore writes `n` entries on its `n`th write, and a list of
1,500 entries leaves about 1.1 million entries of payload behind it. That is
why a few stretches of the history carry ten times the bytes of the rest, and
why those stretches were the slowest to walk and to replay. Every line of
it is kept, by design: a kind a person or an app declares keeps all of its
history, and the kinds a provider mirrors are to keep all of theirs too. So
compaction is no way out of these bytes, and they have to be cut where they
are written.

### Two thirds of the history is the substrate's own bookkeeping

The `delivery` lines and run records above are 67% of the lines and half the
bytes, and nearly every slow path the refold met was on them: the
`trigger_failures` delete, the cursor, schedule and page upserts, and the run
records. No trigger delivers either: `matchChanges` skips both, because a
delivery would feed itself. What reads them is the ledger's state, the
console's lists of runs, and the change rows the history feed draws from. They are in the history only because
decision 0064 put the delivery ledger there, when the backup was a directory
written inside the commit. Compaction trims them to seven days, over days to
weeks of passes. The plan takes them out of the history instead
([section 9](../fast-upgrades.md#9-bookkeeping-is-state-kept-beside-the-history)).

### Destructive defaults shipped in the same release

Two choices in the release would have removed history, and nothing listed
either before the boot:

- **Compaction forgot old deletions.** It was on by default with
  `SUBSTRATE_DELETIONS_KEPT=720h`, so its first pass would have erased the
  whole history of every record deleted more than 30 days before. It was
  found by chance an hour after the cutover. Decision 0334 now keeps every
  deletion unless a deployment says otherwise.
- **Mirrors kept 30 days.** The provider packages declared `retention: P30D`
  for every kind they mirror. The consent door caught it. Decision 0333 now
  keeps a mirror's whole history.

### Costs the upgrade did not count

- **The archive copies everything again.** A conversion that rewrites every
  line leaves the archive to copy the whole history once more after the
  cutover. At the default 8 MiB/s that is about an hour per 27 GB, and longer
  while functions run. It was not measured here, and `repository verify` had
  not been run after the cutover when this was written.
- **The clients.** The published app builds were 95 commits behind the new
  server, and had to be rebuilt and checked after the roll. iOS also refuses
  an install from a website over a TestFlight install with the same bundle
  id, so the owner had to delete the app and install it again.

### Our tests never held these shapes

The kept directories (`TestKeptDirectories`) are small and synthetic: they
prove every effect reads back, and were never meant to time anything. The
scale harness (`TestScaleTimings`) times a history of notes, one kind, each
written once or twice. It has no hot record, no growing list, one kind's
indexes and no trigger. Its result, "about 1,000 to 1,700 lines a second, the
same at its last line as at its first" (`docs/stored-format.md`), was true
of that history and of no real one, and it was the rate the estimates used.

## What we did wrong

- **An overnight step hung on a watcher inside an agent session.** The swap
  to the fast image depended on a background waiter in one session, and when
  the session ended so did the swap. Ten hours ran on the old code.
- **The startup probe was shorter than our own estimate.** The boot was
  expected to take more than 12 hours, and the probe killed it at 12.
- **Estimates came from assumed rates.** The refold was promised at 1,000 to
  1,700 lines/s from the doc above, and ran at 200–400. Each estimate given to
  the owner was missed.
- **Nothing dry-ran the conversion against the real history** before a
  multi-hour run. The 10-minute check that found #1473's two shapes was run
  after the four-hour failure, not before.
- **Nothing rehearsed the upgrade at production scale before the release.**
  Every slowdown and both correctness bugs were found during the upgrade
  itself, on the one copy that mattered, with production's writes piling up
  behind it.
- **Fixes were deployed by restarting a long run from zero.** Nothing in the
  replay could resume, so each improvement cost all the progress before it.
- **Fixes ran in production before review and CI.** #1473's conversion fix
  ran on the real history unreviewed, and the branch images had not passed
  CI. A review afterwards found four faults, and only the history's luck kept
  production unaffected.
- **Nobody listed what the first boot would remove.** The release switched
  on compaction with deletions forgotten after 30 days, and we learned it
  after the cutover, by chance.
- **The clients were left out of the plan.** The app builds that talk to the
  server were 95 commits behind it, and one could only be reinstalled by
  deleting it first.
- **The cutover plan accepted losing writes.** A copy upgraded beside
  production diverges from it from the snapshot on, and nothing could bring
  production's later writes across. The longer the upgrade, the more was
  lost, and it was more than writes: a credential a provider rotates is
  written too, so a refresh token rotated during the two days was gone after
  the cutover and an account had to be connected again.

## What this asks of the design

Each of these is answered in [fast upgrades](../fast-upgrades.md):

1. An upgrade's cost must not depend on how long the history is.
2. Work that must be long must be resumable, so a fix or a crash costs
   minutes, not hours.
3. A replay, when one is unavoidable, must not get slower with every line.
4. A stored-format change must be proven on real history, cheaply, before any
   long run starts.
5. The test suite must hold the shapes real histories hold, at a size where a
   cost that grows shows up.
6. An operator's long step must never depend on a session that can end.
7. An upgrade must be short enough to run in place, so nothing production
   writes meanwhile, a rotated credential included, is lost.
8. The history should hold the owner's data and what changed it, not the
   substrate's per-delivery bookkeeping.
9. A behaviour a first boot switches on that removes or rewrites data must
   be listed, and agreed to by the owner, before the boot.
10. An upgrade's cost includes the archive catching up and the clients that
    talk to the server.
