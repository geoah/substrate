# Fast upgrades: an upgrade costs what changed, not how long the history is

Status: proposal, 2026-11-16. Part of the follow-up to the upgrade of a
production repository of 11.7M lines, whose
[postmortem](fast-upgrades/postmortem.md) says what happened. The numbers
here are from that upgrade and from the [measurements](fast-upgrades/measurements.md)
taken for this page. The choice that binds later code is
[decision 0332](../decisions/0332-an-upgrade-never-walks-a-repositorys-history.md), proposed with it.

## The problem

Today a release that changes a stored shape makes every repository walk its
whole history at its next boot, once for each conversion step, and then
replay all of it into a new fold. All of that happens before the server
listens. For a production repository of 11.7M lines that was about 23 hours
of work with no mistakes, and about 48 hours in practice. A history of 100M
lines would take five to six days, and that assumes the replay's per-line
cost stays flat, which it did not at 11.7M.

What the change was had nothing to do with that cost. Step 6 changed how a
repository's own kinds are spelled, and the fold already held every record.
The cost came from how the change was applied: by rewriting every line in
place and rebuilding the fold from scratch.

## The rule

**An upgrade costs what it changes, never how long the history is.** No
upgrade reads or writes a repository's history in the boot or the open. A
line is never rewritten to change its shape for an upgrade. The fold is
moved forward by a migration of its own, held to the fold a replay would
give. Work that must be long runs behind the open, in committed batches,
resumable, while the repository serves.

The rule holds for a change written the way this page describes, once
phases 1 and 2 have landed ([what each phase guarantees](#what-each-phase-guarantees)).
Until then, no release raises the changelog dialect with a step that
rewrites lines.

### An example: this upgrade's step 6, done the new way

Conversion step 6 (`local_namespace`, decision 0259) moves a repository's
own kinds from its name to `~`. A dialect-5 line of the repository
`ada.example.com` says:

```json
{"kind":"ada.example.com/tasks/task","op":"put","payload":{"fold":[{"kind":"record","ref":"ada.example.com/tasks/task","id":"01jbx9…","delta":{"set":{"assignee":{"ref":"ada.example.com/people/person/01jbx7…"}}}}]}, …}
```

and dialect 6 says the same with `~/tasks/task` and `~/people/person/01jbx7…`.

**Today.** The boot check walks all 11.7M lines, rewrites each one that names
the repository and stamps a new checksum on it, all in one transaction, and
then replays all 11.7M lines through the write path into an empty fold. Then
the archive's next pass writes every piece again under `changelog/6/`. This
took about 1 hour for the walk and about 10 hours for the replay, and would
take about 8 and 80 hours at 100M lines.

**Under this plan.** The release ships three things for the step:

1. **A lift.** The step's `Line` function, unchanged: dialect 5 in, dialect 6
   out. It runs where a line's meaning is read, not at the boot. It reads the
   line, the name the line was written under, and the declarations and actor
   records in force at the line's transaction, which it reads through the
   changelog's kind index as the step does today.
2. **A fold migration.** Statements over the fold that do to its rows what
   the lift does to the lines: `records.kind`, `refs.src_kind` and
   `refs.dst_kind`, each reference value in `props` at a position the kind
   declares as a reference, and each link to an actor record whose id moves.
   It carries the rows outside history as the step's carry does today: a
   sealed value's binding, a vector's key, a stored `Idempotency-Key` answer.
   It works per kind, in batches.
3. **A commutation test.** For every history in the corpus (the kept
   directories, the generated scale histories, the step's own fixtures), the
   test holds the fold of the lifted history to the migrated fold of the old
   history, through `FoldSnapshot`.

At the boot nothing is read. The lines written from then on name dialect 6;
the 11.7M lines below keep the dialect they were written in, 5, and their
checksums. The repository records that its dialect-5 lines were written
under the name `ada.example.com`, so a rename later does not change what an
old line reads as. The fold migration then rewrites the fold rows it
touches, and the archive keeps every piece it has.

The downtime is the fold migration. A set-based rewrite of every fold row
ran at 93,000 to 122,000 rows a second in the measurements, into a `records`
table carrying 528 indexes. If each of the 1.55M records carries about ten
`refs` and mark rows (production's counts were not taken), that is about
three minutes. A history of 100M lines over the same fold takes the same
time: the fold grows with the data a person has, the history with every
write ever made. A fold migration that would run past its budget runs behind
the open instead ([below](#3-the-fold-moves-by-its-own-migration)).

## The owner's questions

**Do we need to move away from the current stored data structure?**
Partly. The changelog as the truth, JSON lines with closed key sets, the
canonical form and its checksum, the archive's pieces and manifest, and the
fold as tables derived from the lines all stay. They are what let every
failed attempt start over cleanly and what proved each speed-up safe. Three
things change:

- a line names the dialect it was written in, so a history may hold several
  at once, read through the conversion chain, instead of being rewritten to
  one;
- the fold gets migrations of its own, so a shape change does not need a
  replay;
- a repeated property gets a delta form, so a list that grows by one entry
  costs one entry of history, not the whole list.

**Do we need protobuf, or another binary encoding that takes less space?**
Not now, and not for this problem. An encoding cannot remove the work an
upgrade does in proportion to the history; only not walking the history
does. On the measurements:

- **Decoding is a small share.** Decoding JSON was 2% of the replay's time on
  the generated history, and under 1% of production's replay by the same rate.
  The Go process used 0.65 of a core during the production replay, so a
  decoder that cost nothing would save at most that.
- **The protobuf form we could measure is worse.** A `record` effect carries a
  person's property values, whose shape is whatever their kind declares, so a
  protobuf line carries them as a generic `Struct`. On this history's line
  shapes it was 7 to 14% larger than the canonical JSON and no faster to
  decode ([measurements](fast-upgrades/measurements.md#encodings)).
- **What was not measured.** A typed envelope with a compact binary value
  encoding was not built.
- **The checksum costs more than the decode.** Checking a line costs two to
  five times decoding it, because it encodes the line again in the canonical
  form. A stored form that lets a line be checked by hashing the bytes as
  stored would save more than any decoder. It is worth measuring end to end
  (size, checksum, transfer, replay) before choosing.
- **The bytes come from re-carrying whole values.** A link whose list grows to
  1,000 entries leaves about 29 MB of history behind it, and the delta form
  below would leave about 0.6 MB.
- **For size alone, compression does more than an encoding.** zstd shrinks
  the heaviest line 3.3×, where Postgres's own compression shrank the whole
  generated history 1.6×.

**Do we need something faster?** Yes, but not a faster codec. The time goes
into Postgres: about 10 statements a line through the live write path, each
paying for its plan and for every index on its table, and, inside one long
transaction, for every version the replay itself left behind. What must be
faster is the replay, for the cases where one is still needed (a restore, an
import, a rebuild): set-based, building each record once and indexes after,
committed in batches. The measurements put the floor of such a build, the
reading and decoding of every line plus a bulk copy of the fold, at more
than 30 times the replay's speed on one core. And an upgrade must not replay
at all.

**Do we need versioned messages for the payloads?** Yes, and we nearly have
them. The changelog dialect is the version of a line's shape, and the key
sets are closed, so a reader always knows which shape it holds. What is
missing is that the version belongs to the whole repository: every line must
be in the same dialect, which is what forces the rewrite. Making each line
name its own dialect lets old and new lines stand side by side. Field
numbering of the protobuf kind would add nothing a dialect number does not
already say.

**Do we need a way to move forward without going over the previous history
at all?** Yes. That is the core of this plan: a reader that knows each line's
dialect and lifts it through the chain as it reads, so an upgrade writes no
line and runs a fold migration, and never walks the history. It needs no new
encoding: the lift is the conversion chain that exists today, moved from the
boot to the read.

## The design

### 1. A line keeps the dialect it was written in

**Each line names its dialect.** The line format rises once, to 2: from then
on every line carries `dialect`, inside what its checksum covers, and the
table keeps it in a `changelog.dialect` column. A line without one, every
line written before the raise, is in its repository's **base dialect**,
which is today's per-repository stamp, frozen. An upgrade that raises the
dialect writes nothing to the history: the lines after it name the new
dialect. A line rewritten for a reason of its own (a compaction's state
entry, an erasure, a re-encode) is lifted to the current dialect first and
written with it. So a history may hold any mix of dialects, one per line,
and the sparse state entries a compaction writes inside old history are
described exactly.

**What a lift reads besides the line.** A step whose rewrite depends on the
repository reads that from a **context timeline** the repository keeps: for
each value its lift needs, the value in force for each interval of seqs.
Step 6 needs the name the lines were written under, and a name is an alias
that may change (decision 0287), so its timeline gains an interval at every
rename while lines of a dialect below 6 can still be written. The timeline
travels in the manifest beside the pieces, and a restore under another name
keeps it. The declarations and actor records in force are read from the
history itself, through the changelog's kind index, as step 6 reads them
today (`conversionPass`, `conversion.go`), and are lifted through the steps
below the one asking and no further, so no lift waits on itself. A line
reads the same in every copy and at every time (decision 0279's rule
stands).

**Two readers, raw and lifted.** The line as stored and the line as it means
today are two things, and every reader of the table or of a piece is one of
two:

- **Raw**: the verified bytes as stored, with their dialect and their stored
  checksum. The archive pass, the export, an import, the comparison of an
  archive with the table, `repository verify`, and anything that needs the
  bytes as written, such as a webhook signature checked against the request
  the spool kept, read raw. Raw lines go into a piece, and come out of one,
  unchanged.
- **Lifted**: a raw line passed through the steps above its dialect. The
  fold and every replay, the change feed, a trigger's envelope, compaction
  and erasure (before they write) and the preflight read lifted lines, in
  one shape, the current one.

Decision 0260 rejected "a decoder per dialect, chosen per line", because every
reader of history would have carried every old shape. This is not that: the
readers of meaning carry nothing, the chain is the one 0260 built, and it
runs at one place.

**SQL that reads inside a payload.** About forty statements in fifteen files
of `internal/engine` filter or project on `payload` in Postgres (`payload ->`,
`@>`, `jsonb_path_exists`), and Postgres sees the stored shape, not the lifted
one. Each one is made to hold under several dialects in one of three ways:

- it names only keys and values no step changes;
- it is limited to lines at or above the dialect it was written for, and the
  older lines go through the lift in Go;
- it reads a column or a side table the write path fills, as
  `changelog_foreign` already is for the effects a line carries for another
  record.

A source guard lists every such statement with the way it holds, and fails
on one it does not list.

**A fact a future lift needs that no index finds.** Step 6's inputs are
found through the kind index. A later step whose lift needs a fact that no
existing index finds ships the side table that indexes it one release ahead
of the step. The write path fills that side table, and a background pass
fills it for existing histories behind the open. The background pass is a
walk of the history, online and resumable, once in a repository's life. An
import fills the side table as its lines go in. Until it is filled, a read
of an old line that needs it waits: the change feed from an old position
answers `503` with `Retry-After`, and a compaction of that range waits too.

**An upgrade never moves a checksum.** A line keeps the checksum it was
stamped with until a rewrite with a reason of its own: a compaction, an
erasure or a re-encode. Each of those writes the line's bytes, its checksum
and its dialect in one statement, and the archive writes its pieces again
from the lowest seq that changed, as it does after a compaction today
(`archive_rewritten`, `archive.go`). An archive never writes its pieces
again for a conversion.

**Old dialects leave slowly, and the binary keeps reading them.** A
compaction lifts the lines it rewrites. An optional background pass
re-encodes old lines in batches of a few thousand, each its own transaction
under the changelog lock. Neither decides when a lift may leave the binary.
An export kept in a drawer is a supported backup whatever the live
repositories hold. So a lift stays for 0260's horizon, a major version. Past
it, the last release that carries a lift converts an archive offline, on a
copy (`substratectl archive convert`, to be written). That is the one
whole-history rewrite left, and it is never part of an upgrade.

### 2. What a lift costs, and who pays it

A lift is a pure function of one line and runs in microseconds: steps 3, 4
and 5 cost about one and a half decodes of the line they read
([measurements](fast-upgrades/measurements.md#encodings)). The replay reads
each line once; the change feed and the triggers mostly read recent lines,
which are in the current dialect; the raw readers pay nothing. The cost of
an old dialect falls only on the lifted reads of its lines, and only until
they are compacted or re-encoded.

### 3. The fold moves by its own migration

A step that changes what the fold holds ships a **fold migration**:
statements, or Go in batches, that bring the fold's rows, and the rows
outside history its carry moves, from the old shape to the new one. Four of
the five steps of this upgrade that rewrote lines had one the fold could
answer from its own rows, and the fifth shows what the next section is for:

| Step | Its fold migration |
| --- | --- |
| 2 `drop_recovery_wrap` | Drop `sealedKey` from the recovery key's row (repository migration 8 purges the row anyway) |
| 3 `link_actors` | Rewrite each stored actor spelling to its actor record id, by a table from spelling to id |
| 4 `move_body_into_properties` | `props = props || jsonb_build_object('body', body)` where `body` is set |
| 5 `marks` | None from the fold alone. The step reads each line's actor, whom it acted for and its mechanism, a resync's rows and a merge's or a split's moved managers, and drops or moves marks with what earlier steps took out ([`conversion_0005_marks.go`](../../internal/engine/conversion_0005_marks.go)). The `property_managers` rows keep none of that, so this is a derivation over the history ([section 4](#4-what-only-the-history-holds-is-derived-behind-the-open)) |
| 6 `local_namespace` | Rewrite `kind`, the `refs` kinds, each reference value and each moved actor link from the name to `~`, per kind, and carry the sealed bindings, vector keys and `Idempotency-Key` answers as the step's carry does |

A fold migration is held by a **commutation test**, the instrument that held
#1470 and #1482: for every history in the corpus,
`FoldSnapshot(fold(lift(h))) == FoldSnapshot(migrate(fold(h)))`. The corpus
is the kept directories, the generated scale histories
([section 7](#7-a-scale-test-that-holds-the-shapes-real-histories-hold)) and
fixtures the step's author writes for the shapes the step touches, the odd
ones first: the bare trait bindings and early unit records of #1473 are what
such fixtures are for. The test is evidence over its corpus, not a proof for
every history, which is why the [preflight](#8-a-preflight-that-runs-before-any-long-run)
runs the lift over the real one too. `TestKeptDirectories` keeps holding the
result to what each release served.

A fold migration has a budget: it must finish within five minutes on the
largest fold the scale test models, extrapolated by rows. One that cannot is
written to run behind the open, in batches, with the code reading both
shapes until it ends. Its pull request says so, and tests reads and writes
during it. That is a second read path for a while, which is why it is the
exception: the budget is what keeps it rare.

### 4. What only the history holds is derived behind the open

A few changes need a fact the fold does not keep, as step 5 did. Those run
as a background derivation, like the re-derivation of a reshaped kind's
indexes today (`reprojection.go`):

- pages of a few hundred rows, each page its own transaction;
- the position kept on a row, so a restart resumes;
- the read that needs the result answering `503` with `Retry-After` until
  the derivation is done, as an aggregate read over the observed index does
  today.

The derivation reads lifted lines, a record at a time, in seq order, through
the kind and record indexes. That holds for the forms a fact can take other
than a plain effect: for marks, a resync and a merge's moved managers. A
line written after the upgrade wins over anything the derivation would
write: the derivation fills only what no later line set. Where most lines
qualify, a derivation is still a walk of most of the history. It is online,
batched and resumable, and never in the boot.

The same rule takes the two history reads that hold an open today out of it:
the actor fill (`actorfill.go`, "reading the repository's history once for
the actor records it links") and the boot upgrade's relinking of shipped
mappings, which held the open for 11 and 35 minutes.

### 5. A replay, when one is needed, is a bulk build beside the live fold

A restore, an import into an empty database and `repository rebuild` still
fold the whole history. That stops being the live write path in one
transaction.

**No write is lost to the build.** A build has a durable row: its id, the
head it builds to, the fold version, and the last batch it committed. A
restart resumes from that batch. There are two ways it keeps writes out:

- **Closed.** A restore, an import, and a rebuild with the server stopped
  build while the repository is closed to writes for the whole build. This
  is the import-progress marker that refuses every open today, so no write
  can land between the head and the swap. Phase 0 uses this form only.
- **Online.** A rebuild of a serving repository builds from a snapshot at
  head H while writes go on, then folds the lines after H in order, as many
  passes as it takes to come close. It then takes the changelog lock, folds
  the last lines, checks that the head is the one it folded to, and swaps.
  Writes wait only for that last step.

**The swap** deletes the repository's rows from each live fold table and
inserts the shadow's, in one transaction. It then does what a rebuild does
after its commit today: analyze the tables, forget the token cache and the
unit graphs, derive the people index, and queue the observed index's
re-derivation. No reader ever sees half a fold, which is the guarantee the
one transaction gave, kept by the swap instead.

**What the build reproduces, table by table.** Before any of it is written,
the build has one entry for each table `repository rebuild` clears or keeps,
saying how it is reproduced. A test fails on a fold table without one, as
`foldpath_internal_test.go` does for the writers of the fold. The shape of
that inventory:

| How | Tables |
| --- | --- |
| Folded per record, in seq order, from the record's own effects | `records`, `annotations`, `property_marks`, `compaction_erasures` |
| Folded per trigger, keeping each row as its last effect left it | `trigger_cursors`, `trigger_schedule`, `trigger_failures`, `paged_cursors` (a middle page's cursor named by its digest, kept from the table as `keepPagedCursors` keeps it) |
| Barriers: lines every partition reaches before they apply, found through `changelog_foreign` | a merge's or a split's `resync`, which restates side rows of every record in its scope; `former_ids` at both ends |
| Global reductions, summed rather than taken from the final rows | `unit_graph`, whose revision moves on every fold of a unit or a conversion row, not once for each surviving one |
| Derived after, set-based, from the final rows | `refs`, `fts`, `unit_uses`, `property_offers`, the orphan and ambiguity marks |
| Queued behind the swap | the observed index, as a rebuild does today |
| Kept as they stand | `sealed`, `embeddings` but the vectors of records the build left no row of, `idempotency_keys` and the rest `rebuild.go` lists |

**How it is fast.** Lines are read in bulk and lifted and decoded on every
core. A record's effects are folded in memory, spilled by hash partition
when they do not fit, and each final row is written once with `COPY` into
shadow tables that carry their primary keys only. The secondary indexes come
after. The per-kind partial indexes are built `CONCURRENTLY` behind the
open, since each one scans all of `records`. Each batch commits, so
autovacuum, `ANALYZE` and pruning see the tables as they fill.

**How it is held.** The bulk build's fold is compared with the live write
path's on the corpus through `FoldSnapshot`, together with what the swap
leaves behind: the caches it clears and the work it queues. It is also
compared in tests that stop it at each batch and, for the online form,
write beside it. The live write path's replay stays as the reference, and
as `repository rebuild --exact` for whoever wants the proof replayed line by
line.

**Fold checkpoints** come after the bulk build, and only if a restore of the
largest repository still takes too long. The archive pass would write the
fold, as of a seq and a fold version, beside the pieces, and a restore would
load it and replay only the lines after it. A checkpoint is derived data in a
backup, so the restored repository verifies it behind the open by a bulk
build compared through `FoldSnapshot`.

### 6. The boot serves each repository when it is ready

Today the server listens only once the boot check has finished every
repository. It will listen first, open repositories in parallel up to a
bound, and answer `503` with `Retry-After` for one that is not ready yet, as
it does for one set aside. That keeps one repository's upgrade from holding
the others. For the repository being upgraded a `503` is still downtime, which
is what the rest of this page shortens.

A schema migration does its DDL in the boot only where Postgres does it in
constant time: a nullable column, a column with a constant default, a table,
a constraint marked `NOT VALID`. An index on a large table is built
`CONCURRENTLY` behind the listener. A rewrite of rows, like the re-keying of
decision 0261, runs in batches behind the open, with the code working before
and after it.

### 7. A scale test that holds the shapes real histories hold

The harness behind the [measurements](fast-upgrades/measurements.md) becomes a
test, `TestReplayCostStaysFlat`, run nightly and on any change under
`internal/engine` to the fold, the replay, the delivery ledger, the
conversions or the migrations. It generates histories, at two sizes and two
mixes, with:

- a record rewritten every round, thousands of times;
- reference lists that grow by one entry a write, to a thousand entries;
- a hundred and fifty kinds, each declaring three indexes, so `records`
  carries more than 500;
- trigger deliveries that park a claim and retire it, with the cursor moved
  on each;
- merges, splits and a compaction or two.

It replays each history and samples, inside the replay's own transaction,
the buffers each fold statement reads (`EXPLAIN (ANALYZE, BUFFERS)` in a
savepoint, through `WithTestReplay`) and the statements per line (through
`WithTestReplaySQL`). Buffers depend on plans, statistics and data, not on
how fast the machine is, so on the test's own pinned Postgres configuration
they are a regression signal a slow runner does not blur. The test holds
them to two budgets:

- an absolute one for each statement;
- a growth one: no statement's buffers per call in the last tenth of the
  history may be more than twice its buffers in the first tenth.

Wall time is checked against itself within the same run. The test also
holds an upgrade to its rule: a history one dialect behind, at both sizes,
must open in the same time and read no changelog row in the boot.

None of this proves that every history stays flat. It catches the shapes
that have already hurt, at a size where growth shows. The kept directories
stay what they are, the proof that every effect any release wrote reads
back.

### 8. A preflight that runs before any long run

`substratectl upgrade preflight` runs with the new release's binary. It has
two parts:

- **Declarations and context.** It reads the declaration and actor lines
  through the kind index, runs each pending step's checks of them, and
  reports every line a lift would refuse, by seq, with the step's reason.
  #1473's two shapes would have been its first two findings. The offline run
  of step 6's checks over the real declaration lines took 10 minutes.
- **The whole history.** From the archive, with no database, it reads every
  line, lifts it, and reports the lines a lift refuses, the largest lines and
  the heaviest stretches. By the measured rates, reading, decoding and
  lifting 24.7 GB is about 2 minutes of work on 8 cores. Fetching 25 GB from
  an object store usually takes longer than that, so this part takes tens of
  minutes. It is to be measured when it is built.

The fold migrations' checks need the fold. So a preflight that times them,
and the derivations, runs against a copy of the database. For each phase it
reports the work it will do and how long that will take, from the
throughput it measured on that copy, never from a number in a doc. It runs
before a release is cut, on a copy of the largest repository, and again
before each deployment.

### 9. Runbook rules

These go into [`skills/substrate-runbook-upgrade`](../../skills/substrate-runbook-upgrade/SKILL.md)
and [upgrading the binary](../operations.md#upgrading-the-binary):

1. **Run the preflight first.** A long run starts only after the preflight
   passes on that history.
2. **Estimate from measured rates only.** Quote the preflight's numbers as a
   range, and re-estimate at each stage from the rate the stage shows.
3. **No unattended step depends on a session.** A swap, a restart or a
   cutover that must happen while nobody watches is a job of the platform
   the server runs on, never a waiter in an interactive or agent session.
4. **Timeouts at least twice the estimate.** A startup probe, a job's
   deadline and a watcher's give-up all allow twice the measured estimate of
   the whole step.
5. **Watch buffers per call.** On any run longer than an hour, sample
   `pg_stat_statements` every few minutes. A statement whose buffers per call
   keep growing is a stop, not a wait.
6. **Upgrade in place when the downtime is minutes.** A copy upgraded beside
   production diverges from it. Use one only for a rehearsal, or say before
   starting which writes will be lost and when.
7. **Rehearse before release.** A release that adds a lift, a fold migration
   or a migration that touches rows is rehearsed on a copy of the largest
   repository known, with the preflight and then the upgrade, before it is
   cut. Its pull request records the measured times.

## The options

The costs are for this repository's 11.7M lines and for a history of 100M
over a fold of the same size, from the rates the
[measurements](fast-upgrades/measurements.md) give. The 100M figures for
anything that replays assume a per-line cost that stays flat, which is the
best case.

| Option | What it does | 11.7M lines | 100M lines | Verdict |
| --- | --- | --- | --- | --- |
| Today: rewrite in place, then refold | One walk per step, then one replay through the write path, in the boot | About 5 h of walks, 10–12 h of replay | About 40 h, then 85–100 h | Replace |
| Batched, resumable replay of today's path | The same replay, committed every few thousand lines into shadow tables while the repository stays closed, the tables vacuumed and analyzed between batches | About 8 h, with no growth and no restart lost | About 70 h | Phase 0, for whatever still replays; not an upgrade path |
| Bulk replay | Section 5 | A floor of about 5 min; under 30 min with what the floor leaves out | A floor of about 25 min; under an hour | Phase 3, for restores, imports and rebuilds |
| Lift on read and fold migration | Sections 1 and 3 | No line written, and a fold migration of about 3 min | The same: it depends on the fold, not the history | **Phases 1–2, the upgrade path** |
| Fold checkpoints | The archive keeps the fold at a seq; a restore loads it and replays the tail | A load of a few minutes, and the tail | The same, and the tail | Later, only if bulk restores are too slow |
| Online shadow rebuild beside the old version | The old binary serves while the new one builds a fold beside it, then a swap | No downtime but the swap | The same | Not now: two binaries writing one database break decision 0325's one writer, and every schema change would have to suit both |
| A binary line encoding | Protobuf, or another form, for the line | The protobuf `Struct` form measured larger and no faster; decoding is 2% of a replay | The same share | Not now ([the questions](#the-owners-questions)); a form that makes a checksum cheap to check is worth measuring end to end |
| A delta form for repeated properties | `add` and `remove` for a repeated property instead of its whole value | Cuts an append-heavy record's history about 50× from the next write on | The same | Phase 4, as a dialect raised the new way |
| Compaction (exists) | Folds a record's history past its kind's retention into a state entry | Shrinks what a replay reads, for kinds that do not keep everything | The same | Keep, and tell owners of sync-written kinds that they keep everything by default |
| Preflight | Section 8 | Minutes for the declarations, tens of minutes for the whole history | About eight times the whole-history part | Phase 0 |
| Scale test in CI | Section 7 | Minutes a run | n/a | Phase 0 |

**Rewriting in place against a reader that lifts.** The previous design
preferred a one-time migration to a second read path, and for small
histories that was the cheaper choice: one shape everywhere, and nothing to
carry. It cannot meet minutes at this size. A rewrite of 25 GB of lines and
the replay after it is hours of work whatever the encoding, and doing it
online, in batches, while the server serves, needs a reader that takes both
shapes during the window: a second read path again, only a temporary one. A
reader that lifts is that same reader, kept, at one point in the code, with
the chain that already exists. The background re-encode gives back "one
shape in the history" for whoever wants it, without anyone waiting for it.

## What each phase guarantees

| After | An upgrade that | Costs the repository being upgraded |
| --- | --- | --- |
| Today | Rewrites lines | Hours: a walk per step and a replay that slows as it goes |
| Phase 0 | Still rewrites lines: any history below dialect 8, such as a server still on v0.123.1 | Still hours. The replay no longer slows down, a restart resumes, a preflight has found the refusals first, and other repositories serve meanwhile |
| Phase 1 | Raises the dialect with a lift, and changes nothing the fold holds | No history read or written at the boot |
| Phase 2 | Also changes the fold | A fold migration: minutes for the fold the scale test models, or a derivation behind the open |
| Phase 3 | Needs a replay: a restore, an import, a rebuild | Under an hour at 100M lines, by the floors measured |

So the guarantee of minutes holds for upgrades written the new way, after
phases 1 and 2, and only once the preflight and a rehearsal on the real
history have confirmed it for that release. Until phases 1 and 2 land, no
release raises the changelog dialect with a step that rewrites lines. A
change that would need one waits for them.

## What changes in decisions already made

Decision 0332, proposed with this page, says the rule. When it is accepted,
it supersedes:

- **0260's "history converts forward once"**: the format stays the contract,
  the chain and the kept directories stay, and the conversion moves from a
  rewrite at the boot to a lift at the read;
- **0309**, whose question (when does a conversion rebuild the fold) no
  longer arises: a conversion never rebuilds the fold, and a fold migration
  says what it changes.

It amends 0099 (a repository migration that reads the history runs behind the
open) and leaves 0296, 0300 and 0277 as they are, but for the line format
and the manifest gaining what section 1 adds.

## Phases

Each phase is its own pull requests, and each ends with the scale test
holding what it built.

**Phase 0: the next upgrade cannot go like this one.**

1. Land #1473, #1482, #1483 and #1484.
2. `TestReplayCostStaysFlat`, with the shapes and the budgets above. On the
   stack's tip it fails today, on the hot record's row, its marks and the
   unpark ([measurements](fast-upgrades/measurements.md#the-replay-against-its-position)).
3. The replay commits in batches into shadow tables while the repository
   stays closed, with a durable build row, a resume from its last batch, the
   tables vacuumed and analyzed between batches, and one swap at its end.
   This fixes the whole class: the measurements show every probe back at 0
   to 6 buffers once the versions can be pruned and the statistics are
   fresh. The test of step 2 passes after it.
4. Until step 3 lands, a replay holds parked failures as #1483 holds the
   ledger rows, writing those that survive at its end, so an unpark is a
   delete from memory: the one fix the next upgrade cannot wait for.
5. The server listens before the boot check ends and answers `503` for a
   repository that is not ready.
6. The preflight's declarations part, for the conversion steps that exist.
7. The runbook rules, in the skill and in the operations guide.

**Phase 1: lines keep their dialect.** The line format 2 with `dialect`, the
column, the context timeline in the table and the manifest, the raw and
lifted readers, the list of SQL statements that read inside a payload and
its guard, and the archive no longer writing pieces again for a conversion.
Steps 2–8 stay as they are for a history below dialect 8. The first step
written after this phase is written the new way, and the scale test's
upgrade check holds it.

**Phase 2: fold migrations.** The fold migration, its budget and its
commutation test as part of a step; the background derivation for what only
history holds; the actor fill and the shipped-mapping relink moved behind the
open; the preflight's whole-history part and its run against a copy.

**Phase 3: bulk replay.** The bulk build for restores, imports and rebuilds,
with its inventory, held to the write path's fold. Fold checkpoints only if
a restore of the largest repository still takes more than an hour.

**Phase 4: the delta form for repeated properties**, raised as a dialect the
phase 1 way, with its lift (an old line's whole value stays a whole value)
and no fold migration.

## Risks and open questions

- **A lift that cannot be written.** A step whose rewrite needs more than the
  line, its context timeline and what the history says through an index
  cannot be a lift. None of steps 2–8 is one. A step that needs one waits for
  the side table it needs, a release ahead.
- **Every reader passes through one of two doors.** The lift is only as good
  as the claim that nothing reads `changelog.payload` for its meaning around
  it, and about forty statements do today, in SQL
  ([section 1](#1-a-line-keeps-the-dialect-it-was-written-in)). A source
  guard in the style of
  `TestEveryReadOfTheChangesByAPositionCarriesTheHorizon` holds every query
  of the table to the raw reader, the lifted reader, or the list of
  statements that hold under every dialect. Making that list is the largest
  single piece of phase 1.
- **Fold migrations are new code to get right.** The commutation test is
  evidence over its corpus, and the corpus is what a step's author makes of
  it. The preflight's run over the real history and the rehearsal are what
  catch the shapes no fixture holds.
- **The bulk build is a second implementation of the fold.** It is held to
  the write path by the same snapshot comparison, with its inventory, on
  every history the tests generate. The write path stays the definition.
- **How large a fold migration may be before it must run online** is a
  budget this page sets at five minutes on the modelled fold. It should be
  revisited with the first real one.
- **A binary form is not ruled out for good.** If measuring a stored form
  whose checksum can be checked without encoding the line again shows a
  real gain end to end, it can arrive as a line format raised the phase 1
  way.
