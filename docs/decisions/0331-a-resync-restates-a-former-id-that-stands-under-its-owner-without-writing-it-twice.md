---
status: accepted
date: 2026-11-15
decision-makers: the 1.0 plan's orchestrator session, for George Antoniadis
---

# 0331. A resync restates a former id that stands under its owner without writing it twice

## Context and Problem Statement

A merge's or a split's resync re-states the side stores of the pair it
joins. On replay it clears the annotations, the marks and the former-id rows
of each record in its scope, then writes its rows back. That clearing is how
a merge or a split moves a former id from one record to another: a merge of
`b` into `c` clears `a>b` and writes `a>c`.

An erasure
([0304](0304-a-permanent-delete-erases-the-records-history-and-the-intent-is-in-the-history.md))
must take the erased record's values out of the resyncs that restate them.
The builds of `main` from #1384 to the fix for
[#1460](https://github.com/geoah/substrate/issues/1460) did that in two ways,
and neither replays. No release carries either.

- **A forget of a live record** took it out of the resync's scope and kept
  every former-id row the resync restated. Its replay then no longer cleared
  the record's former-id rows. A row it restated could already stand from the
  merge's own `former` effect (the winner of two merges or more), and the fold
  inserted it a second time, which the primary key refused.
- **The erasure of a purged record** removed the resync whole. A merge whose
  loser was split back and then deleted permanently kept its merge line with
  no resync, and a replay refuses such a line (`foldRefuses`): what the
  resync restated for the live winner is gone.

Either way the repository can no longer be rebuilt, restored from its own
archive, or replayed at an upgrade.

## Considered Options

- Keep the resync whole but for the erased record's values, and let the fold
  take a former-id row a resync restates where the same row already stands
  under the same owner.
- Keep the resync whole but for the erased record's values, and repair the
  histories already written.
- Take the record out of the scope and keep only the former-id rows that name
  a record left in it. This filters one resync at a time, and a chain of
  merges defeats it: with `b` out of the scope of the merge of `b` into `c`,
  `a>b` stands when the resync writes `a>c`.
- Let a resync's former-id row overwrite whatever row stands under that id.

## Decision Outcome

Chosen: both halves of the first option.

- **An erasure keeps every resync, its scope and its former-id rows, and
  takes out only the annotation and mark rows it restated for the erased
  record**: that record's values as they were. Its state, above them,
  restates its values as they are, and a resync above the state is not
  touched. The scope and the former-id rows are ids, and the replay's
  clearing is what moves a former id, so the history replays as it did
  before the erasure.
- **The fold takes a former-id row a resync restates that already stands
  under the same owner**, as the trail it is, and does not write it twice.
  A row that stands under another owner is a second owner of the id, and the
  resync is refused as before.

The first half alone would leave every history the forgets already wrote
unable to rebuild. The fold accepts those lines instead of having a
migration rewrite them, for three reasons:

- A repository migration writes through the changelog, as ordinary record
  writes appended at the head
  ([0099](0099-a-repository-migration-is-code-the-boot-runs-once-and-records.md)).
  It cannot change a line already written, so every replay would still meet
  the old line below whatever it appended.
- The one writer that rewrites a line in place is a conversion step, and a
  step raises the changelog dialect
  ([0260](0260-the-stored-format-is-a-contract-each-release-keeps-a-directory-and-history-converts-forward-once.md)).
  A raised dialect is refused by every earlier server, so a rollback across
  this fix would lose the repository. The lines are already in dialect 8's
  shape. What they say is true: the row did stand under that owner. They
  only say it twice, so there is no shape for a step to convert.
- A replay that tolerates the lines works wherever a replay runs (a rebuild,
  a restore of an archive taken at any point, an import, an upgrade's
  replay), with nothing that must run first and nothing that can be left
  half done.

No line changes shape, so the changelog dialect stays 8. Only a binary with
this fold accepts the doubled row: an older one still refuses that history.

The fold repairs the first kind of history only. **A merge or a split line
that a purged record's erasure left with no resync stays refused.** What the
resync restated for the live winner is in no other line, and a replay that
passed the line would give a fold silently unlike the live one. Such a line
exists only where, on a build of `main` from #1384 to this fix, a merge's
loser was split back and then deleted permanently. That repository can be
neither rebuilt, nor restored from its own archive, nor replayed at an
upgrade. Its operator restores the backup taken before that delete.
`repository verify` reports such a line, with its seq, so an operator learns
of it from a verify and not from a failed restore. Verify reads the line's
shape, not how it came to be, so its finding names the known cause and asks
the operator to confirm it before restoring.

The third option was this fix's first form, and a chain of merges refuses
it. The last would let a resync silently move a former id to another record:
the second owner the primary key exists to refuse.

### Consequences

- Good, because an erasure now leaves a history that replays into the live
  fold for every shape the tests hold, chains of merges among them.
- Good, because a repository whose history a forget wrote rebuilds, restores
  and replays at an upgrade again, with no migration and no new dialect.
- Good, because a second owner of a former id is still refused, by name.
- Bad, because the fold now accepts a history that writes the same former-id
  row twice where it once refused it, so a history that does that for another
  reason replays without a word.
- Bad, because a repository with a merge line left with no resync is not
  repaired. Its operator restores a backup.
- Bad, because a value the erased record had that a merge moved onto its
  partner stays in the partner's rows of the merge's resync. A forget left
  it there before too. A permanent delete did not: those builds removed the
  whole resync, which is what left the line no replay accepts. The reply's
  `kept` names the merge's and split's own records, not these lines (see
  [#1462](https://github.com/geoah/substrate/issues/1462)).

### Confirmation

`internal/engine/eraserebuild_internal_db_test.go`:

- the reproduction;
- every shape of erasure rebuilt into the live fold and verified, chains of
  merges included;
- either record of a merge split back erased, its values gone from the
  resyncs and both records' side rows rebuilt as they stand live;
- a forget's old history rebuilt and restored;
- the old merge line with no resync refused by a rebuild and a restore,
  naming its seq, and reported by a verify;
- a resync giving another record's former id refused.

## More Information

This settles what 0304's erasure does to a merge's or a split's resync,
which 0304 left to the code. Built in the pull request that closes #1460.
