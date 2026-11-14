---
status: accepted
date: 2026-11-14
decision-makers: George Antoniadis
---

# 0300. A paced pass compacts what is past each kind's retention, and forgets old deletions

## Context and Problem Statement

[0296](0296-a-records-history-is-compacted-into-a-state-entry-in-the-line-of-its-newest-write.md)
gives a record's entries one state entry and a function to write it; a kind
declares how long its history is kept, and a repository may override it
(0282).
The 1.0 review (#1313, F6) asks for a background pass that applies the one to
the other, gives way to real work, remembers deletions for a period and tells
a client behind it `compacted`. What it leaves open is the unit of work, how
it finds a record's history without reading all of it, what it costs the
archive, and where the oldest resumable position is kept.

## Considered Options

- Compact each window by reading the whole history up to the cutoff.
- Compact each window by reading only its records' history, through an
  index of the effects one record's line carries for another.
- Keep every deletion for ever and leave the horizon at 0.

## Decision Outcome

Chosen: a pass per repository that reads only the records it compacts. The
whole-history read is what 0296's `repository compact` does once; a pass that
runs every hour cannot.

- **What it picks.** Per kind, the cutoff is the newest entry written before
  now minus the kind's retention, the head for a kind that keeps nothing, and
  nothing for one that keeps everything. The pass keeps per kind the seq it
  compacted up to (`compaction_marks`) and works the window between. An
  entry's age is its line's `ts`.
- **How it finds a record's history.** Its own lines by address, and the
  lines that carry an effect on it for another record, from
  `changelog_foreign`, which the write path fills at commit for the lines
  that have any (a merge's loser, a resync's scope, late-settled effects). A
  history that came in otherwise is indexed by the pass before it compacts.
- **Holds that end.** A record a parked failure's line, a drain's or the
  webhook spool holds part of, all of it included, is kept as held
  (`compaction_held`) while the kind's mark moves on. Each pass retries one
  batch of a kind's held records first, in turn from where the last got to,
  so a hold that lasts takes no more of a pass than that.
- **Pacing.** Batches of 25 records, one transaction each under the
  changelog lock, a sleep as long as the batch, two minutes a pass. A pass
  stopped anywhere is redone; a record already at its state is not rewritten.
- **The archive.** A compaction records every seq it changed; the first
  archive pass that starts after it rewrites only the runs of pieces holding
  one, each run ending on
  a line that is there, so piece ranges stay contiguous and no rule of the
  archive's contract changes ([0279](0279-the-archive-holds-against-a-written-contract.md)).
- **The horizon.** A purged record whose deletion is older than the
  deletion period (30 days by default) is forgotten whole or not at all:
  never past an enabled record trigger's cursor, never on the head line, and
  not while a line of it must stay (one a parked failure names), when the
  pass comes back to it. A record trigger that is not enabled holds nothing:
  the pass starts it over from the beginning, once, before it forgets past
  its cursor, and a replay below the horizon starts from the beginning. The
  floor is read again under each batch's changelog lock. The repository's horizon moves
  to the line the deletion was on, is read with the head, answered `410
  compacted` below it, and named on the change page. A permanent delete
  forgets at once through the same `forgetRecords` and `raiseHorizon`.
- **Known damage.** While a line of the history is known not to verify,
  nothing is compacted at or above it, and the repair comes first.
  A batch reads a record's lines by the very columns damage may change, so
  what any reader that writes anyway finds is noted for all
  (`changelog_damage`): the door, the pass's reads, its index's derivation,
  and the archive pass where it refuses the row. Every
  compaction reads the lowest note under its changelog lock and stays below
  it, and a batch inside what the index covers while its derivation runs.
  A batch also stops below a damaged line of its own reads: its records'
  lines, and the other lines of each transaction it re-frames, the only
  ones whose `txn` it moves. A note goes once its row verifies again, and
  the pass then goes on by itself.

### Consequences

- Good, because what a pass reads and uploads is bounded by what changed
  since the last one, not by the history's length.
- Good, because a client behind a forgotten deletion is told so, the one
  case where several changes arriving as one is not harmless.
- Bad, because the write path writes one more table, for the lines that
  carry another record's effects.
- Bad, because a record trigger that never moves holds every later
  deletion, and the horizon, back.
- Bad, because one line known not to verify holds back the compaction of
  everything written after it, for every record, until it is repaired.
- Bad, because damage no reader has met is unknown to all of them: a batch
  finds a record's lines by their address and the index of the effects
  they carry, so a line whose damage changed those can have a state placed
  above it without the effect it was written with, until a reader meets
  it. `repository verify
  --recanonicalize` reads every line and names it.
- Bad, because the first pass of the release compacts at once everything
  past the shipped retentions: a repository that syncs loses most of its
  history's bytes in it, each record keeping its latest state.

### Confirmation

`TestThePassCompactsWhatIsPastEachKindsRetention`,
`TestAPassStoppedAtEachStageIsFinishedByTheNext`,
`TestTheArchiveAfterTenPassesHoldsTheTableAndUploadsWhatChanged`,
`TestThePassForgetsAnOldDeletionAndMovesTheHorizon` and
`TestAResumeBelowAForgottenDeletionIsToldCompacted`.

## More Information

The code is `internal/engine/compactpass.go` and `compactindex.go`;
[running a substrate](../operations.md#the-compaction-pass) has the settings.
