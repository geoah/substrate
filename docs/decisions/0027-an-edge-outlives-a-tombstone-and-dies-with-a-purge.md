---
status: accepted
date: 2026-08-17
decision-makers: George Antoniadis (via the issue-111 agent session)
---

# 0027. An edge needs its target at write, outlives a tombstone, and dies with a purge

## Context and Problem Statement

Written after the fact, reconstructed from the code paths named below and from
[issue #111](https://github.com/geoah/substrate/issues/111). The engine already
answers three questions about a link whose ends can disappear, and it answers
them differently at each point:

- A write REFUSES an edge whose target row is absent (`resolveEdgeRef`,
  `internal/engine/write.go`, returns `ErrNotFound`).
- A tombstone (soft delete) leaves every edge row standing
  (`applyTombstone`, `internal/engine/rows.go`, touches `records` alone).
- A hard delete removes every edge at both ends (`applyPurge`,
  `internal/engine/rows.go`, `DELETE FROM edges WHERE (src_kind = $1 AND src =
  $2) OR (dst_kind = $1 AND dst = $2)`).

The three rules only make sense together, and together they lived in three
code comments in two files. A reader who found one of them could not tell
whether the other two were the design or an oversight, and the first person to
"fix" the inconsistency would break undelete and the merge trail.

## Considered Options

- Write it down as it stands: refuse at write, keep on tombstone, cascade on
  purge
- Cascade on tombstone too: a soft delete removes the record's edges
- Refuse nothing at write and let any edge dangle, filtering at read

## Decision Outcome

Chosen: write it down as it stands. The split is the only one of the three
that survives undelete, and each rule earns its place separately.

**A write needs the target to exist.** `resolveEdgeRef` loads the row and
refuses when it is absent, so an edge is never born dangling. The check runs
under the same locks the write already holds, which is what stops a bind
racing a delete (`internal/engine/inputs.go`). Existence, not liveness: a
tombstoned row still exists, and an edge may point at one. The merge record's
`loser` edge names a tombstone on purpose.

**A tombstone keeps the edges.** A soft delete is reversible, and a cascade
would make it not: undelete would return a record stripped of every link it
had, with nothing anywhere to rebuild them from. The changelog holds the
tombstone, not a hundred synthetic unlinks, so a cascade here would also mean
the fold could no longer replay what the write did.

**A purge cascades.** A hard delete is the end of the record, and an edge row
naming an id no record holds is a row nothing can ever resolve or clean up. It
cascades at BOTH ends, because an edge is a row about two records and the
survivor should not keep pointing into a hole.

The cost is that a read can meet an edge whose target is a tombstone. That is
the deliberate half: a tombstone is visible to a reader who asks for it, and
hiding the link would hide the deletion too.

### Consequences

- Good, because undelete restores a record with its links intact, and the fold
  replays exactly the deltas the write appended.
- Good, because no edge row can name an id the records table has never held,
  so a dangling row is always a purge that has not run yet, never a write that
  should not have been accepted.
- Bad, because a read may resolve an edge to a tombstoned record, and every
  read surface has to decide for itself whether to show it.
- Bad, because a purge is the only thing that collects edge rows: a repository
  that only ever tombstones accumulates edges to records nobody can reach.

### Confirmation

The engine suite: the merge and blob tests assert the tombstone-then-hard-delete
sequence (`internal/engine/blobs_review_db_test.go`), and `resolveEdgeRef`'s
refusal is exercised wherever a write names a target that is not there.

## More Information

Issue [#111](https://github.com/geoah/substrate/issues/111) asked for this rule
to leave the code comments. The same change makes an edge's declared
`properties` block real and starts classifying edge diffs as narrowings, both
of which assume this split: a narrowing count over the `edges` table only means
anything because a purge is the one thing that removes a row from it.
