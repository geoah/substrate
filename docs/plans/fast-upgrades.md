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
line is never rewritten to change its shape. The fold is moved forward by a
migration of its own, proven to give the fold a replay would give. Work that
must be long runs behind the open, in committed batches, resumable, while
the repository serves.

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
   out. It runs where a line is read, not at the boot.
2. **A fold migration.** Set-based statements over the fold that do to its
   rows what the lift does to the lines: `records.kind`, `refs.src_kind` and
   `refs.dst_kind`, and each reference value in `props` at a position the
   kind declares as a reference, from `ada.example.com/…` to `~/…`. It works
   per kind, in batches.
3. **A commutation test.** For every history in the corpus (the kept
   directories, the generated scale histories, the step's own fixtures), the
   test holds the fold of the lifted history to the migrated fold of the old
   history, through `FoldSnapshot`.

At the boot the repository records one row: from seq 11,706,647 on, its lines
are in dialect 6, and the lines below are dialect 5, read through step 6's
lift. The row also carries the name that lift needs, `ada.example.com`, so a
rename later does not change what an old line reads as. The fold migration
then rewrites the 1.55M fold rows it touches. No line is read, the checksums
stay as they were stamped, and the archive keeps every piece it has.

The downtime is the fold migration. A set-based rewrite of every fold row
ran at 93,000 to 122,000 rows a second in the measurements, into a `records`
table carrying 528 indexes. If each of the 1.55M records carries about ten
`refs` and mark rows (production's counts were not taken), that is about
three minutes. A history of 100M lines over the same fold
takes the same time: the fold grows with the data a person has, the history
with every write ever made. A fold migration that would run past its budget
runs behind the open instead ([below](#3-the-fold-moves-by-its-own-migration)).

## The owner's questions

**Do we need to move away from the current stored data structure?**
Partly. The changelog as the truth, JSON lines with closed key sets, the
canonical form and its checksum, the archive's pieces and manifest, and the
fold as tables derived from the lines all stay. They are what let every
failed attempt start over cleanly and what proved each speed-up safe. Three
things change:

- a history may hold lines of several dialects at once, read through the
  conversion chain, instead of being rewritten to one;
- the fold gets migrations of its own, so a shape change does not need a
  replay;
- a repeated property gets a delta form, so a list that grows by one entry
  costs one entry of history, not the whole list.

**Do we need protobuf, or another binary encoding that takes less space?**
No. The changelog's `payload` is already stored as `jsonb`, a binary form,
and TOAST compresses it. Decoding JSON was 2% of the replay's time in the
measurements, under 1% of production's by the same rate, and the Go process
used 0.65 of a core during the production replay, so a decoder that cost
nothing would save at most that. Protobuf
cannot type the payload either: a `record` effect carries the person's
property values, whose shape is whatever their kind declares, so a protobuf
line would carry them as a generic `Struct`. Measured on this
history's line shapes, a `Struct` was 7 to 14% larger than the canonical JSON and
no faster to decode ([measurements](fast-upgrades/measurements.md#encodings)).
Where the bytes come from is the whole-value form: a link whose list grows to
1,000 entries leaves about 29 MB of history behind it, and the delta form
below would leave about 0.6 MB. For size alone, compression does more than
any encoding: zstd shrinks the heaviest line 3.3×, where Postgres's own
compression shrank the whole generated history 1.6×.

**Do we need something faster?** Yes, but not a faster codec. The time goes
into Postgres: about 9 statements a line through the live write path, each
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
be in the same dialect, which is what forces the rewrite. Making the dialect
belong to a range of seqs lets old and new lines stand side by side. Field
numbering of the protobuf kind would add nothing a dialect number does not
already say.

**Do we need a way to move forward without going over the previous history
at all?** Yes. That is the core of this plan: a reader that knows each line's
dialect and lifts it through the chain as it reads, so an upgrade writes one
row and a fold migration, and never walks the history. It needs no new
encoding: the lift is the conversion chain that exists today, moved from the
boot to the read.

## The design

### 1. A line keeps the dialect it was written in

`changelog_dialect` holds one dialect per repository today. It becomes a list
of ranges, `changelog_dialects (repository, from_seq, dialect, context)`: the
lines from `from_seq` up to the next row's `from_seq` are in `dialect`. A new
repository has one row, `(1, current)`. An upgrade that raises the dialect
appends one row at the head plus one, under the changelog lock, in a
transaction that writes nothing else to the history. That is the whole of a
stored-format change at the boot.

`context` holds what a step's lift reads from the repository, frozen when the
range is closed. Step 6 needs the repository's name, and a name is an alias
that may change (decision 0287), so the range carries the name the lines
below it were written under. A lift reads only the line, the context and the
declarations in force at the line's transaction, so a line reads the same in
every copy and at every time (decision 0279's rule stands).

**One place lifts.** `changelogfile.Entry` gains no field. The table's line
reader (`queryChecksumRows` and the cursors over it) and the archive's piece
reader hold each row to its stored checksum in the frame it was written in,
as they do today, then pass it through the steps above its dialect. Every
reader above that point, the fold, the change feed, a trigger's envelope, the
export's consumers, sees one shape, the current one. Decision 0260 rejected
"a decoder per dialect, chosen per line", because every reader of history
would have carried every old shape. This is not that: the readers carry
nothing, and the chain is the one 0260 built, run at the read instead of at
the boot.

**SQL that reads inside a payload.** About forty statements in fifteen files
of `internal/engine` filter or project on `payload` in Postgres (`payload ->`,
`@>`, `jsonb_path_exists`), and Postgres sees the stored shape, not the lifted
one. Each one is made to hold under several dialects in one of three ways: it
names only keys and values no step changes, or it is limited to the ranges at
or above the dialect it was written for and the older ranges go through the
lift in Go, or it reads a column or a side table the write path fills, as
`changelog_foreign` already is for the effects a line carries for another
record. A source guard lists every such statement with the way it holds, and
fails on one it does not list.

**A step that reads declarations.** Step 6 reads every version of every
declaration the history holds. A reader builds that list once per repository
and process from the declaration lines alone, as #1470's declarations pass
does: 4,715 versions in the 11.7M-line history. It is bounded by the
vocabulary's history, not by the data's.

**Checksums stop moving.** A stored line keeps the checksum it was stamped
with, so a checksum proves a line undamaged since it was written, not since
its last conversion (the "Bad" consequence 0260 recorded goes away). The
archive cuts a piece at each range boundary and names each piece's dialect,
which its directory `changelog/<dialect>/` already does, and the manifest
lists the ranges with their context. An archive no longer writes every piece
again after a conversion.

**Old dialects still leave.** Two things move lines to the current dialect,
and neither is on the upgrade path. A compaction (decision 0296) writes its
state entries in the current dialect. And an optional background pass
re-encodes a range in batches of a few thousand lines, each its own
transaction under the changelog lock, splitting the range as it goes. A lift
is dropped from the binary once no supported archive can hold its dialect,
which is 0260's rule that a step is carried for one major version.

### 2. What a lift costs, and who pays it

A lift is a pure function of one line and runs in microseconds
([measurements](fast-upgrades/measurements.md#encodings)). The replay reads
each line once; the change feed and the triggers read recent lines, which are
in the current dialect. The cost of a dialect in a history falls only on the
reads of the old lines, and only until they are compacted or re-encoded.

### 3. The fold moves by its own migration

A step that changes what the fold holds ships a **fold migration**:
statements, or Go in batches, that bring the fold's rows from the old shape
to the new one. Four of the five steps of this upgrade that rewrote lines had
one the fold could answer from its own rows, and the fifth shows what the
next section is for:

| Step | Its fold migration |
| --- | --- |
| 2 `drop_recovery_wrap` | Drop `sealedKey` from the recovery key's row (repository migration 8 purges the row anyway) |
| 3 `link_actors` | Rewrite each stored actor spelling to its actor record id, by a table from spelling to id |
| 4 `move_body_into_properties` | `props = props || jsonb_build_object('body', body)` where `body` is set |
| 5 `marks` | Fill `property_marks` from the `property_managers` rows the fold held. The step's rule also reads whether a machine's `manager` effect was its own line's write, which no row keeps, so this one is a derivation over the lines that carried `manager` effects ([section 4](#4-what-only-the-history-holds-is-derived-behind-the-open)) |
| 6 `local_namespace` | Rewrite `kind`, the `refs` kinds and each reference value from the name to `~`, per kind |

A fold migration is proven by a **commutation test**, the same instrument
that proved #1470 and #1482: for every history in the corpus,
`FoldSnapshot(fold(lift(h))) == FoldSnapshot(migrate(fold(h)))`. The corpus
is the kept directories, the generated scale histories
([section 7](#7-a-scale-test-that-holds-the-shapes-real-histories-hold)) and
the step's own fixtures, and `TestKeptDirectories` keeps holding the result
to what each release served.

A fold migration has a budget: it must finish within five minutes on the
largest fold the scale test models, extrapolated by rows. One that cannot is
written to run behind the open, in batches, with the code reading both
shapes until it ends, and its pull request says so. That is a second read
path for a while, which is why it is the exception: the budget is what keeps
it rare.

### 4. What only the history holds is derived behind the open

A few changes need a fact the fold does not keep: a new fold table whose
rows only some effects carried, or a rule that reads how a value came to be.
Those run as a background derivation, like the re-derivation of a reshaped
kind's indexes today (`reprojection.go`): pages of a few hundred rows, each
page its own transaction, the position kept on a row so a restart resumes,
and the read that needs the result answering `503` with `Retry-After` until
it is done, as an aggregate read over the observed index does today. It reads
only the lines it needs through the kind and record indexes, never the whole
table in one pass.

The same rule takes the two history reads that hold an open today out of it:
the actor fill (`actorfill.go`, "reading the repository's history once for
the actor records it links") and the boot upgrade's relinking of shipped
mappings, which held the open for 11 and 35 minutes.

### 5. A replay, when one is needed, is a bulk build beside the live fold

A restore, an import into an empty database and `repository rebuild` still
fold the whole history. That stops being the live write path in one
transaction:

- **Read in bulk.** Lines are read in seq order, outside the changelog lock,
  in one snapshot, and lifted and decoded on every core.
- **Build each record once.** A record's effects only ever touch that record,
  but for a merge's and a split's `resync` and the few others
  `changelog_foreign` already indexes. So effects are partitioned by record
  and folded in memory, spilled by hash partition when they do not fit, and
  each record's final row, marks and annotations, and each trigger's final
  ledger rows, are written once. The lines with foreign effects are barriers
  every partition reaches before they apply.
- **Write with `COPY`, index after.** Rows go into shadow tables with their
  primary keys only. `refs`, `fts`, the unit uses and the offers are derived
  set-based from the final rows, then the secondary indexes are built, the
  per-kind partial indexes among them.
- **Commit in batches and swap once.** Each batch commits, so autovacuum,
  `ANALYZE` and pruning see the tables as they fill, and a restart resumes
  from the last batch. The live fold is replaced in one short transaction at
  the end, so no reader ever sees half a fold: the guarantee the one
  transaction gave, kept by the swap instead.
- **Prove it.** The bulk build's fold is held to the live write path's on
  the corpus, through `FoldSnapshot`.

The live write path's replay stays as the reference the bulk build is tested
against, and as `repository rebuild --exact` for a person who wants the
proof replayed line by line.

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
it does for one set aside. A schema migration does its DDL in the boot only
where Postgres does it in constant time (a column with a constant default, a
table, a constraint marked `NOT VALID`). An index on a large table is built
`CONCURRENTLY` behind the listener, and a rewrite of rows, like the re-keying
of decision 0261, runs in batches behind the open, with the code working
before and after it.

### 7. A scale test that holds the shapes real histories hold

The harness behind the [measurements](fast-upgrades/measurements.md) becomes a
test, `TestReplayCostStaysFlat`, run nightly and on any change under
`internal/engine` to the fold, the replay, the delivery ledger, the
conversions or the migrations. It generates a history with:

- a record rewritten every round, thousands of times;
- reference lists that grow by one entry a write, to a thousand entries;
- a hundred and fifty kinds, each declaring three indexes, so `records`
  carries more than 500;
- trigger deliveries that park a claim and retire it, with the cursor moved
  on each;
- merges, splits and a compaction or two.

It replays the history and samples, inside the replay's own transaction,
the buffers each fold statement reads (`EXPLAIN (ANALYZE, BUFFERS)` in a
savepoint, through `WithTestReplay`) and the statements per line (through
`WithTestReplaySQL`). Buffers are the same on any machine, so the test holds
them to a budget: no statement's buffers per call at the last tenth of the
history may be more than twice its buffers at the first tenth. Wall time is
checked the same way, against itself in the same run. It also holds an
upgrade to its rule: a history one dialect behind, at two lengths, must open
in the same time, reading no changelog row in the boot.

The kept directories stay what they are, the proof that every effect any
release wrote reads back. The scale test is the proof that reading them
does not get slower with the history.

### 8. A preflight that runs before any long run

`substratectl upgrade preflight` runs with the new release's binary against
the repository's archive, and needs no database: the archive is the history
in its documented format. It reads every line, runs every pending lift and
fold migration's checks over it, and reports:

- every line a lift refuses, by seq, with the step's reason (#1473's two
  shapes would have been the first two lines of its report);
- for each phase, the work it will do and how long it will take, from the
  throughput it measured on this history, never from a number in a doc;
- the largest lines and the stretches of the history that are heaviest.

On a repository of 11.7M lines it takes minutes: step 6's checks over the
real declaration lines took 10. It runs before the release is cut, on a copy
of the largest repository, and again before each deployment.

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
| Batched, resumable replay of today's path | The same replay, committed every few thousand lines into shadow tables, `ANALYZE` between batches | About 8 h, with no growth and no restart lost | About 70 h | Phase 0 for restores and rebuilds; not an upgrade path |
| Bulk replay | Section 5 | A floor of about 5 min; under 30 min with what the floor leaves out | A floor of about 25 min; under an hour | Phase 3, for restores, imports and rebuilds |
| Lift on read and fold migration | Sections 1 and 3 | A row, and a fold migration of about 3 min | The same: it depends on the fold, not the history | **Phase 1–2, the upgrade path** |
| Fold checkpoints | The archive keeps the fold at a seq; a restore loads it and replays the tail | A load of a few minutes, and the tail | The same, and the tail | Later, only if bulk restores are too slow |
| Online shadow rebuild beside the old version | The old binary serves while the new one builds a fold beside it, then a swap | No downtime but the swap | The same | Not now: two binaries writing one database break decision 0325's one writer, and every schema change would have to suit both |
| Binary line encoding | Protobuf or similar for the line | Saves at most the decode share, 2% of a replay | The same share | No ([the questions](#the-owners-questions)) |
| A delta form for repeated properties | `add` and `remove` for a repeated property instead of its whole value | Cuts an append-heavy record's history about 50× from the next write on | The same | Phase 4, as a dialect raised the new way |
| Compaction (exists) | Folds a record's history past its kind's retention into a state entry | Shrinks what a replay reads, for kinds that do not keep everything | The same | Keep, and tell owners of sync-written kinds that they keep everything by default |
| Preflight | Section 8 | Minutes | Tens of minutes | Phase 0 |
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
open) and leaves 0296, 0300 and 0277 as they are.

## Phases

Each phase is its own pull requests, and each ends with the scale test
holding what it built.

**Phase 0: the next upgrade cannot go like this one.**

1. Land #1473, #1482, #1483 and #1484.
2. `TestReplayCostStaysFlat`, with the shapes and the budget above. On the
   stack's tip it fails today, on the hot record's row, its marks and the
   unpark ([measurements](fast-upgrades/measurements.md#the-replay-against-its-position)).
3. The replay commits in batches into shadow tables and swaps at its end,
   resumable from its last batch, with the tables vacuumed and analyzed
   between batches. This is the fix for the whole class: the measurements show
   every probe back at 0 to 6 buffers once the versions can be pruned and the
   statistics are fresh. The test of step 2 passes after it.
4. Until step 3 lands, a replay holds parked failures as #1483 holds the
   ledger rows, writing those that survive at its end, so an unpark is a
   delete from memory: the one fix the next upgrade cannot wait for.
5. The server listens before the boot check ends and answers `503` for a
   repository that is not ready.
6. The preflight, first for the conversion steps that exist.
7. The runbook rules, in the skill and in the operations guide.

**Phase 1: lines keep their dialect.** The range table, the lift at the line
reader and the piece reader, the manifest's ranges, and the archive no longer
writing pieces again after a raise. Steps 2–8 stay as they are for a history
below dialect 8; the first step written after this phase is written the new
way, and the scale test's upgrade check holds it.

**Phase 2: fold migrations.** The fold migration, its budget and its
commutation test as part of a step; the background derivation for what only
history holds; the actor fill and the shipped-mapping relink moved behind the
open.

**Phase 3: bulk replay.** The bulk build for restores, imports and rebuilds,
held to the write path's fold. Fold checkpoints only if a restore of the
largest repository still takes more than an hour.

**Phase 4: the delta form for repeated properties**, raised as a dialect the
phase 1 way, with its lift (an old line's whole value stays a whole value)
and no fold migration.

## Risks and open questions

- **A lift that cannot be written.** A step whose rewrite needs more than the
  line, the context frozen with its range and the declarations in force
  cannot be a lift. None of steps 2–8 is one, but a step that needs one ships
  a background re-encode before it can raise the dialect.
- **Every reader passes through one function.** The lift is only as good as
  the claim that nothing reads `changelog.payload` around it, and about forty
  statements do today, in SQL ([section 1](#1-a-line-keeps-the-dialect-it-was-written-in)).
  A source guard in the style of
  `TestEveryReadOfTheChangesByAPositionCarriesTheHorizon` holds every query
  of the table to the reader or to the list of statements that hold under
  every dialect. Making that list is the largest single piece of phase 1.
- **Fold migrations are new code to get right.** The commutation test is what
  makes them safe, and it is only as wide as its corpus. The scale test's
  generated histories and the kept directories are the corpus; a step author
  adds fixtures for the shapes their step touches.
- **The bulk build is a second implementation of the fold.** It is held to
  the write path by the same snapshot comparison, on every history the tests
  generate. The write path stays the definition.
- **How large a fold migration may be before it must run online** is a
  budget this page sets at five minutes on the modelled fold. It should be
  revisited with the first real one.
