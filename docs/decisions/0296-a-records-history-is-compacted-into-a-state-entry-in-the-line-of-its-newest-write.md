---
status: accepted
date: 2026-11-14
decision-makers: George Antoniadis
---

# 0296. A record's history is compacted into a state entry in the line of its newest write

## Context and Problem Statement

The 1.0 review (#1313, `docs/plans/v1-review.md`, F6) keeps the changelog as
an order and as the write path, and drops complete history as the source of
truth: a record's entries older than its retention are replaced by one
**state entry**, the record's whole state as of the newest entry it
replaces, and sequence numbers are kept, so older history has gaps (0017).
The page leaves open where a state entry lives in the line, what it holds,
what it may replace, and what that means for the archive (0277, 0279) and
for the readers of old entries. The compaction pass and its retention are
later steps; this record is what they call.

## Considered Options

- A new line per state, appended at the head.
- A new op that turns the newest replaced line into a state line.
- A new effect inside the line of the record's newest write, one per family
  of rows a record holds.

## Decision Outcome

Chosen: a new effect inside the line of the record's newest write. A line
carries the effects of every record its transaction wrote, so the unit is
one record's effects, never a line. A line at the head would move the state
past the entries kept above it, and the readers that follow a record by
address (a trigger's read, a watch's filter) would meet it out of order; an
op names one write and a line is often many. In the line of the newest
write it replaces, the state keeps that write's seq, time and actor, so
"who last wrote this" survives, and every reader meets the record once
where it last changed.

- **Two effects.** `state` stands for a record's own rows: its records row
  with its version, kind version and three times, its annotations and its
  property marks, each beside a value the row holds. `ledger` stands for a
  trigger's delivery rows. A replay takes the version and the times from
  the state, where it counts and stamps them for every other effect. A
  `state` goes on the newest line addressed to the record that carries one of
  its effects and is not a `delivery` line, which no public read shows.
- **Taken by the one fold.** A state is what the fold held at its place:
  the compaction replays the family's effects up to it, through `foldOne`,
  inside a savepoint, and reads the rows. There is no second account of what
  an effect does.
- **What is never replaced.** `former` and `resync` (a former effect can take
  a former id from a third record, so no record's state could re-state the
  trail exactly), a purge that is not the record's last effect on its
  state's line, the lines a parked failure or a drain names, a trigger's
  delivery effects while the webhook spool holds a request, every effect of
  a family below one of those that stays (it replays against the rows they
  left), and the head. Nor is any line at or above the first that does not
  verify: a compaction reads nothing of such a line but its seq and stops
  below it, as if the line under it were its seq, so it never gives a line
  a checksum it had no right to, the rule a conversion keeps. Every line
  below verified, a transaction that ends below it lies wholly below it,
  and one that holds it ends at or above it and is never re-framed. It names
  the line, and goes past it once the row is written back.
- **The dialect.** The two effects raise the changelog dialect to the one
  they need, through a conversion step that rewrites no line, so a server
  that cannot replay them refuses the repository at its open.
- **The archive's contract does not change.** A compaction records the
  lowest seq it changed, and the next pass writes its pieces again from the
  one holding it, under new names: rules 1, 2 and 5 hold as written. A piece
  covers a range of seqs and holds the lines the history holds in it.
- **What a reader gets.** The change event names the record with `state`
  set; a state that stands for the record's creation presents as it; its
  before values read unknown. The oldest position a client may resume from
  does not move: several changes arriving as one is harmless. A forgotten
  deletion moves it, and that is the permanent delete's step.

### Consequences

- Good, because a replay of a compacted history gives the rows it gave
  before, versions and times included, and a replay of an uncompacted one
  runs no new code.
- Good, because the archive and its contract take compaction without a new
  rule: the first manifest of an archive pass that starts after the
  compaction committed holds none of what was replaced (a pass already
  running publishes its earlier cut first), and an archive taken before it
  still restores the history it held.
- Bad, because a state carries the record's values, and a resync keeps the
  side rows of a merged pair, so a compaction alone does not erase a value
  from history: a permanent delete must strip them too.
- Bad, because the rules that ask history "did any hand ever write this?"
  (a shipped trigger's package, a seeded record's owner) read only the lines
  a compaction keeps until they read the marks, so the compaction pass must
  not run before they do.
- Bad, because one line that does not verify holds back the compaction of
  everything written after it until an operator writes it back, and the
  archive pass stops at it the same way.
- Bad, because the conversion step walks every repository's history once at
  the upgrade, though it changes no line, and a rollback past it is refused.

### Confirmation

`TestACompactedHistoryReplaysToTheSameFold` (every table a rebuild fills,
read from the schema, row for row, after compacting up to a middle seq and
then up to the head), `TestACompactedArchiveImportsAsTheArchiveBeforeItDid`,
`TestAWatchResumedInsideACompactedRangeSeesEveryRecordItWouldHaveOnce`,
`TestATriggerReplayedFromTheStartOfACompactedHistorySeesEachLiveRecordOnce`,
`TestABinaryFromBeforeStateEntriesRefusesACompactedRepository`, and
`TestChangelogDialectCoversTheChangelogVocabulary`.

## More Information

The code is `internal/engine/compact.go`; the format is
[the stored format's state entries](../stored-format.md#state-entries).
`substratectl repository compact` is the operator's door. Revisit when a
kind's retention asks for a state of a record that is not its own line's.
