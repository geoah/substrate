---
status: proposed
date: 2026-11-16
decision-makers: George Antoniadis
---

# 0332. An upgrade never walks a repository's history

## Context and Problem Statement

[0260](0260-the-stored-format-is-a-contract-each-release-keeps-a-directory-and-history-converts-forward-once.md)
converts history forward once: at the boot, each pending step walks every
line and rewrites it in place, in one transaction, and
[0309](0309-a-conversion-rebuilds-the-fold-only-where-a-line-changed-or-a-step-says-so.md)
then rebuilds the fold by replaying every line. 0260 recorded the cost ("its
time grows with history") and planned to time it at 100 GB before 1.0. The
upgrade of a production repository of 11.7M lines from v0.123.1 timed it:
about 23 hours of boot with no mistakes, and about 48 in practice
([postmortem](../plans/fast-upgrades/postmortem.md)). A history of 100M lines
would take about a week. The cost grows with how many writes a repository
has ever had, not with what a release changes, and nothing in the current
design bounds it.

## Considered Options

- Keep converting in place at the boot, and make the walks and the replay
  faster.
- Convert in place, but online: batches committed behind the open while the
  server serves.
- A line keeps the dialect it was written in and is lifted through the chain
  where it is read; the fold moves by a migration of its own.

## Decision Outcome

Proposed: the third, as
[the fast upgrades plan](../plans/fast-upgrades.md) sets out. An upgrade
costs what it changes and never how long the history is:

- **No upgrade reads or writes a repository's history in the boot or the
  open.** A raised dialect is one row of the repository's dialect ranges,
  from the head plus one, with what the step's lift reads frozen beside it.
- **A line is never rewritten to change its shape.** Every read of a line
  holds it to its stored checksum in the frame it was written in, then lifts
  it through the steps above its dialect, in one place, so every reader
  above that place sees one shape. A compaction and an optional background
  re-encode, in committed batches, move old lines to the current dialect
  over time.
- **The fold moves by a fold migration** that each step changing what the
  fold holds ships, proven by a commutation test (the fold of the lifted
  history equals the migrated fold of the old one) over the kept directories
  and the generated scale histories, and held to a time budget; what the
  fold cannot answer is derived behind the open, in batches.
- **Work that must be long runs behind the open**, in committed batches that
  resume where they stopped, and a repository not ready answers `503`.

Faster walks and a faster replay were ruled out as the upgrade path because
they move the constant and not the growth: a replay at ten times today's
speed still takes hours at 100M lines, and its cost grows with every write
the repository takes. Converting online in batches meets the downtime, but a
history half-converted must be read in both shapes while it runs, which is
the reader the third option keeps, plus a rewrite of every line and every
archive piece that nothing needs.

### Consequences

- Good, because a stored-format change costs a row and a fold migration
  whose size is the fold's, which grows with the data a person keeps, not
  with their history.
- Good, because a stored line keeps the checksum it was stamped with, so a
  checksum proves a line undamaged since it was written, and the archive
  never writes its pieces again for a conversion.
- Bad, because the conversion chain stays in the binary at the read, not
  only at the boot, and the claim that every read of `changelog` passes
  through it must be held by a source guard.
- Bad, because a fold migration is new code a step author writes, and it is
  only as right as the corpus its commutation test runs on.
- Bad, because a step whose rewrite needs more than a line, its range's
  context and the declarations in force cannot be a lift, and must ship a
  background re-encode first.

### Confirmation

A scale test generates histories with the shapes a long-lived repository
holds and holds an upgrade over them to the rule: a history one dialect
behind, at two lengths, opens in the same time and reads no changelog row in
the boot. Each step's commutation test holds its fold migration. A source
guard holds every query of the `changelog` table to the line reader that
lifts.

## More Information

Supersedes, once accepted, the part of
[0260](0260-the-stored-format-is-a-contract-each-release-keeps-a-directory-and-history-converts-forward-once.md)
that converts history in place at the boot (its format, its chain and its
kept directories stand), and
[0309](0309-a-conversion-rebuilds-the-fold-only-where-a-line-changed-or-a-step-says-so.md),
whose question no longer arises. Amends
[0099](0099-a-repository-migration-is-code-the-boot-runs-once-and-records.md):
a repository migration that reads the history runs behind the open.
