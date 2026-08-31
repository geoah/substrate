---
status: superseded
superseded-by: 0044
date: 2026-08-31
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

The three rules only make sense together, and together they lived in code
comments in four files (`internal/engine/inputs.go` states the split most
fully, `vocabularywrite.go` names the tombstone half while taking an exception
to it, `rows.go` says why a tombstoned target is legal, `query.go` gives the
reverse-edge read rule). A reader who found one of them could not tell whether
the others were the design or an oversight, and the first person to "fix" the
inconsistency would break undelete and the merge trail.

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
the fold could no longer replay what the write did. A caller that wants the
edges gone unlinks them itself, in the same transaction and as its own fold
effects: bundle uninstall does exactly that, because a re-install would
otherwise resurrect a binding the uninstall cleared
(`internal/engine/vocabularywrite.go`).

**A purge cascades.** A hard delete is the end of the record, and an edge row
naming an id no record holds is a row nothing can ever resolve or clean up. It
cascades at BOTH ends, because an edge is a row about two records and the
survivor should not keep pointing into a hole.

The rule binds WRITES, not replay. `applyEdgePut` names no record at all, and
the fold reaches it both live and on rebuild, so `RebuildRepository` replays
recorded edge effects without re-checking either end. Split does the same, out
of the merge record's stored payload (`internal/engine/merge.go`). Both are
correct on purpose: re-validating at replay would make a rebuild fail on any
changelog written before a rule existed, which is the drift the fold exists to
prevent, inverted. A row that replays but could not be written today is
history, not divergence.

That has one consequence worth stating rather than discovering. `moveEdges`
deletes every one of a merge loser's edge rows and parks a collision loser's
props, and any pair-internal edge, in the merge record's `moved` payload;
split reinserts them raw. So while a merge is open, a stored edge row can sit
somewhere the `edges` table cannot see, and a narrowing counted over that
table admits a change those parked rows contradict. Split then restores a row
carrying a property no declaration admits.

This is the tombstone window, seen from the merge record's side, and it is
the window the record-property counts have had all along: `countPropQuery`
carries `deleted_at IS NULL`, so a merged-away loser's own properties are
invisible to it too, and split resurrects them the same way an undelete does.
Edge counts are given the same posture deliberately. Holding edges stricter
than the properties beside them would mean an open merge could block a
vocabulary change that the identical change to a record property admits, which
is a refusal nobody could act on: the writer cannot see the parked rows, and
the only way to clear them is to split a merge they may still want.

The cost is that a read can meet an edge whose target is a tombstone. Nothing
filters it: `loadEdges` (`internal/engine/query.go`) inner-joins `records`, so
a tombstoned target joins and is returned with its title, and REST, GraphQL
and the console all pass it through. The same join is what hides a PURGED
target, silently, as a side effect rather than as a stated rule. Reverse edges
are the one place liveness is filtered, and it filters the SOURCE
(`query.go`): a deleted record no longer points at anything.

### Consequences

- Good, because undelete restores a record with its links intact, and the fold
  replays exactly the deltas the write appended.
- Good, because a dangling edge is a narrow set of causes: a purge that has
  not cascaded to a replayed row, never a write that should not have been
  accepted.
- Bad, because a read resolves an edge to a tombstoned record with no marker
  that it is one, and the caller has to read `status.deletedAt` to notice.
- Bad, because an edge whose target is purged disappears from a read without
  a word, so nothing surfaces the inconsistency to anyone who could fix it.
- Bad, because nothing collects a dead record's edges automatically: plenty of
  paths delete edge rows on purpose (unlink, a single-valued edge being
  replaced, a bundle uninstall clearing its bindings), but a purge is the only
  one that fires because a record ended, so a repository that only ever
  tombstones accumulates edges to records nobody can reach.
- Bad, because a merge parks the loser's edge rows outside the `edges` table
  until it is split, where a narrowing count cannot see them.

### Confirmation

`TestTombstoneKeepsEdgeRowsAndUndeleteRestoresThem`
(`internal/engine/edgeprops_db_test.go`) holds the tombstone half and the
undelete it exists for. `internal/engine/blobs_review_db_test.go` holds the
tombstone-then-hard-delete sequence. `internal/engine/merge_fixes_db_test.go`
holds split's restore, which is the merge record's payload rather than this
rule: merge is the one path where a tombstoned record does NOT keep its edge
rows, because `moveEdges` deletes them all.

Nothing holds the read-side behavior or the parked-row window. Both are
written down as costs for that reason, not claimed as guarantees.

## More Information

Issue [#111](https://github.com/geoah/substrate/issues/111) asked for this rule
to leave the code comments. The same change makes an edge's declared
`properties` block real and starts classifying edge diffs as narrowings, both
of which assume this split: a narrowing count over the `edges` table only means
anything because a purge is the one thing that removes a row from it.
