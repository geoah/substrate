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
would take five to six days. The cost grows with how many writes a
repository has ever had, not with what a release changes, and nothing in
the current design bounds it.

## Considered Options

- Keep converting in place at the boot, and make the walks and the replay
  faster.
- Convert in place, but online: batches committed behind the open while the
  server serves.
- Each line names the dialect it was written in and is lifted through the
  chain where its meaning is read; the fold moves by a migration of its own.

## Decision Outcome

Proposed: the third, as
[the fast upgrades plan](../plans/fast-upgrades.md) sets out. Once its phases
1 and 2 have landed, an upgrade costs what it changes and never how long the
history is. Until then, a history below dialect 8 still converts through
steps 2 to 8 as it does today, and no new step that rewrites lines lands.
The rule:

- **No upgrade reads or writes a repository's history in the boot or the
  open.** A raised dialect is the dialect the lines after it name, under a
  line format and a manifest format raised once for it, and a line without
  one is in its repository's base dialect. What a step's lift reads about
  the repository, the name its lines were written under for the local
  namespace, is kept as a timeline over seqs that travels with the archive,
  and no compaction or erasure removes what a retained line's lift reads.
- **A line is never rewritten to change its shape for an upgrade.** Readers
  are of two kinds. Raw readers (the archive, the export, an import, a
  comparison, a verify) take the verified bytes as stored. Lifted readers
  (the fold, the change feed, a trigger's envelope) take the line through the
  steps above its dialect, at one place. A compaction, an erasure and an
  optional background re-encode lift the lines they rewrite for reasons of
  their own. A lift stays in the binary for the supported-backup horizon,
  whatever the live repositories hold.
- **The fold moves by a fold migration** that each step changing what the
  fold holds ships. A commutation test (the fold of the lifted history equals
  the migrated fold of the old one) holds it over the kept directories, the
  generated scale histories and the step's fixtures, and a time budget bounds
  it. What the fold cannot answer is derived behind the open, in batches.
- **Work that must be long runs behind the open**, in committed batches that
  resume where they stopped. A replay that is still needed (a restore, an
  import, a rebuild) builds beside the live fold and swaps it in one
  transaction, either with the repository closed to writes or by catching up
  the lines written meanwhile under the changelog lock before the swap.
- **Until this is built, no release raises the changelog dialect with a step
  that rewrites lines.**

Faster walks and a faster replay were ruled out as the upgrade path because
they move the constant and not the growth: a replay at ten times today's
speed still takes hours at 100M lines, and its cost grows with every write
the repository takes. Converting online in batches meets the downtime, but a
history half-converted must be read in both shapes while it runs, which is
the reader the third option keeps, plus a rewrite of every line and every
archive piece that nothing needs.

### Consequences

- Good, because a stored-format change costs a fold migration whose size is
  the fold's, which grows with the data a person keeps, not with their
  history.
- Good, because an upgrade never moves a checksum, so a line's checksum
  proves it undamaged since it was written or last rewritten for a reason of
  its own, and the archive never writes its pieces again for a conversion.
- Bad, because the conversion chain stays in the binary at the read for the
  supported-backup horizon, and the claim that every read of `changelog`
  goes through the raw or the lifted reader, or holds under every dialect,
  must be held by a source guard over about forty SQL statements that read
  inside the payload today.
- Bad, because the line format rises once, to carry each line's dialect.
- Bad, because a fold migration is new code a step author writes, and its
  commutation test is evidence over its corpus, not a proof for every
  history.
- Bad, because a step whose lift needs a fact no index finds must ship the
  side table that indexes it a release ahead.

### Confirmation

A scale test generates histories with the shapes a long-lived repository
holds and holds an upgrade over them to the rule: a history one dialect
behind, at two sizes, opens in the same time and reads no changelog row in
the boot. Each step's commutation test holds its fold migration. A source
guard holds every query of the `changelog` table to the raw reader, the
lifted reader, or a listed statement that holds under every dialect.

## More Information

Supersedes, once accepted, the part of
[0260](0260-the-stored-format-is-a-contract-each-release-keeps-a-directory-and-history-converts-forward-once.md)
that converts history in place at the boot (its format, its chain and its
kept directories stand), and
[0309](0309-a-conversion-rebuilds-the-fold-only-where-a-line-changed-or-a-step-says-so.md),
whose question no longer arises. Amends
[0099](0099-a-repository-migration-is-code-the-boot-runs-once-and-records.md):
a repository migration that reads the history runs behind the open.
