---
status: accepted
date: 2026-11-15
decision-makers: the 1.0 plan's orchestrator session, for George Antoniadis
---

# 0330. An erasure longer than the pass's bounds is done in steps

## Context and Problem Statement

A permanent delete, and "forget history", erase a record's history in one
transaction: "an erasure is whole or held"
([0304](0304-a-permanent-delete-erases-the-records-history-and-the-intent-is-in-the-history.md),
[0306](0306-a-record-has-a-forget-door-that-erases-its-history-before-its-current-state.md)).
Nothing bounds that transaction by the record's size
([#1452](https://github.com/geoah/substrate/issues/1452)). It replays the
whole history to take the state, holding every line's payload in the
process at once; it searches the payload of every trigger line above the
record's first line for saved positions; and its intent stays until it
finishes, so a pass that tries it first, before any compaction, tries it
again for good. Measured on a record of 20,001 lines (981 MiB stored), the
erasure ran Postgres out of 6 GB of temporary space and failed, and the
next pass tried it again. The compaction pass already holds every batch it
runs to 1,000 lines and 32 MiB of stored bytes beyond its largest lines
(#1442).

## Considered Options

- An erasure past the bounds is done in steps, each the erasure's own
  compaction up to the step's line, within the pass's bounds, and reads
  pending until its last step.
- Plain compaction steps first, then the erasure.
- Refuse an erasure of a record longer than the bounds.
- Hold such an erasure until the pass has shortened the record.

## Decision Outcome

Chosen: steps of the erasure's own compaction. This amends 0304 where it
says an erasure is whole or held: an erasure may now be pending for its
size. It still reads done only when it is whole.

- **One bound, the pass's.** No transaction of an erasure reads more than a
  batch of the pass may: the record's own lines, the trigger lines its
  search reads, the merge and split lines it strips. What a step reads again
  of the record below the lines it takes (the state the step before left,
  its merges' lines, its id trail) counts too; where that alone passes a
  bound, a step reads it and one new line. An erasure whose every part fits
  is done in one transaction, as before.
- **The steps.** First the search for the saved positions that hold the
  record's values, one range of trigger lines a step, stripping what it
  finds; a range with no trigger line costs no step. Then the copies merges
  hold, a batch a step. Then the history, a step at a time, each an erasure
  up to the step's line, so a step crosses a merge's or a split's resync as
  the last step does. A step never spans an earlier purge of the record: it
  ends on the purge's line, whose place its state takes, where a step past
  it would keep a purge that took a former id and be held for good. The
  last step is the erasure as before, once what is left fits.
- **Progress is the history's, but for one thing.** Each step reads again
  what the history holds. How far the search has looked is the one thing
  the history does not carry; it is kept on a progress row beside the
  intent (`compaction_erasure_progress`), not in a fold table. A rebuild
  keeps it, since the lines it read are unchanged; a restore or an import
  starts without it, and the search starts again, which strips nothing
  twice.
- **The request does one step** of each erasure it starts and answers
  pending, with where the erasure stands, in the reply's existing words. A
  forget with `finish` takes one step too, past the triggers it waits for.
- **The pass spends a quarter of its budget** on pending erasures, a step
  of each in turn, then compacts. A step that fails on its own is noted on
  the progress row and the pass goes on; only the server's failures end it.
- **What held an erasure holds each step**, and is reported as before.

Plain compaction steps were ruled out because a plain compaction keeps a
merge's or a split's resync and an earlier purge, and every line under
them, which an erasure removes: a record with a long history under one
could not be shortened, and its last step would read it all. Refusing a
long record's erasure would leave its owner no way to remove values they
asked to have removed. Holding the erasure until the pass shortened the
record never ends for a kind that keeps everything, which no pass shortens.

### Consequences

- Good, because no erasure, however long the record or the repository's
  trigger history, holds more than one batch of the pass in one
  transaction, and none stops the pass or the erasures behind it.
- Good, because a test erases every shape twice, in steps and whole, and
  holds the two histories to each other line for line.
- Bad, because an erasure of a long record is pending for longer: its
  values stay in the history until its last step, over as many passes as
  the share takes.
- Bad, because a permanent delete in a repository with a long trigger
  history reads all of it, a range a step, before the record's history goes.
- Bad, because two reads stay outside the bounds, beside #1453's: a step
  verifies the whole transaction of every line it edits (a merge's copy, a
  saved position, an intent), and the doors' check for an earlier purge
  that took a former id reads the record's lines below that purge, where
  it has one.
- Bad, because the two paths leave a trigger's delivery lines differently,
  and neither erases them: the last step, like the one transaction, passes
  them by, so a trigger within the bounds keeps every delivery line, while
  the steps before the last leave the trigger's ledger entry in place of
  theirs and the last keeps at most what fits within the bounds (see
  [#1471](https://github.com/geoah/substrate/issues/1471)).
- Bad, because a record that won more merges than half the line bound
  retains more lines than a step may read: each of its steps reads them and
  one new line, so its erasure takes a step for each line it has left.

### Confirmation

`internal/engine/erasestep_internal_db_test.go`: the differential test over
every shape, erased whole, in steps, and in steps with a rebuild after each,
and the tests of the bounds, a crash between steps, a step that fails every
time, a put at the purged id and a record within the bounds.

## More Information

Amends 0304, 0305 and 0306 where they say an erasure is whole or held.
Built in the pull request that closes #1452.
