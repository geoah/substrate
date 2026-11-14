---
status: accepted
date: 2026-11-14
decision-makers: the 1.0 plan's orchestrator session, pending George Antoniadis (#1391)
---

# 0305. A permanent delete reaches what holds the record's values, and not what it owns

## Context and Problem Statement

A record's values live in more places than its own lines: the change
requests that target it, the records merged into it and the merge's and
split's own records, the mapped targets that copied a value from it, and the
records it owns through `onDelete: cascade`. Step 24 of the 1.0 plan (#1313)
has a permanent delete erase the record's history
([0304](0304-a-permanent-delete-erases-the-records-history-and-the-intent-is-in-the-history.md)),
and had to say which of these it erases with the record and which it leaves,
because the person asked for the record to be gone and is owed a plain
answer about what is not. The page asks that "a permanent delete compacts
that record at once" and that no value of the record is left in the log.

## Considered Options

- Erase the record's own history alone.
- Erase it with everything that holds its values by a typed link, and leave
  what it owns to the delete's cascade, as
  [0122](0122-a-delete-may-purge-so-a-put-starts-the-record-over.md) does.
- Erase what it owns too, in batches the compaction pass runs, each owned
  record the way the record itself is erased.

## Decision Outcome

Chosen: everything that holds the record's values by a typed link, and not
what it owns. A permanent delete's own transaction purges these records and
writes the intent to erase each one's history, and the erasure follows the
commit, held by what 0304 names (a delivering trigger, a line known not to
verify, an index still being derived) until it can run whole. It reaches:

- the record;
- each change request whose target is the record, pending or settled, found
  by its typed link: closed by the delete, and erased with it;
- each record merged into it, by its former ids, and the merge's and any
  split's own records, which keep what moved;
- each mapped target's copy of a value it derived from the record, found by
  the mark's `derivedFrom` or by the subject its mapping points at, which an
  earlier delete's tombstone leaves: the target's history up to the line
  that released the copy is compacted, its current state kept.

All of it is read before the tombstone, the tombstone goes through the
mapping's lifecycle, then the purge and the intents, in that one
transaction: a finalizer on the record or on any record it would take with
it refuses the delete whole, before anything is purged, but for the merge's
own hold on its loser, which keeps the loser for a split the delete makes
impossible. A merge or a split line that
outlives the erasure, because the other record of the pair lives on, keeps
its fold and the ids it names, and loses the copies of the pair's values it
carried. A record that points at the deleted one by any reference is not
erased: its values are its own, and its pointer dangles.

What the record owns is deleted with it as 0122 has it: the delete's cascade
tombstones it for the collector's sweep, and the next `put` at the record's
id creates a fresh record at once. Its own history is not erased by this
step: it stays as any deleted record's does, until the compaction pass
forgets old deletions past the deletion period
([0300](0300-a-paced-pass-compacts-what-is-past-each-kinds-retention-and-forgets-old-deletions.md)),
or until that record is permanently deleted itself.

The third option, erasing what the record owns too, is not taken now, and
#1391 asks George whether to take it. The page does not ask for it; it
would make the put that starts a record over (0122) wait while a closure
stands, an account connected again among them; and three reviews of a
version that took it found that what a record owns is asked again at every
batch, so a sweep, a patch of the owning reference or a declaration that
changes between two batches changes the answer. That version is kept on the
branch `geoah/v1-24-owned-erasure`.

The `gc` line stays, carrying no value: its op, the record's kind and id and
the hand that deleted it, until the compaction pass forgets the deletion
past the deletion period, as it forgets every old deletion (0300). It is how
a reader learns the record went, and what a rule that must remember a
shipped record's removal can read. Whether the record seed reads it is that
rule's to decide, and it is open.

Out of reach, and said so in the docs: text another record holds that
quotes the record (an agent's message, a memory pinned from it, a tool
call's arguments or result, an alert's title, a change request's diff of
another record); a request already in the webhook spool; a provider's sync,
which brings a mirror record back unless it remembers the deletion, which
is open too; and every copy taken before the delete.

### Consequences

- Good, because what exists only to change the record or holds a copy of
  its values goes with it, and a mapped target keeps its current values and
  loses only its own history of the copy.
- Good, because the delete is one transaction: it is whole or refused, and
  a put at the id starts the record over at once.
- Bad, because what the record owns keeps its own history, its values in
  it, until the deletion period has passed or it is permanently deleted
  itself.
- Bad, because a quoting record keeps the quote: nothing finds it reliably.
- Bad, because a mirror deleted permanently comes back at its next sync
  until providers remember deletions.

### Confirmation

`TestAPermanentDeleteLeavesNoValueOfTheRecordInAnyShape` (the source of a
mapping, a merge winner and its loser, a record whose merge and split
records hold its moved values, a change request's target, a sealed value, a
stored Idempotency-Key answer, an embedding and a queued one, an observed
point, a parked failure and a drain on its lines),
`TestAPermanentDeleteReachesWhatAsksToChangeTheRecordAndWhatWasMergedIntoIt`,
`TestAPermanentDeleteLeavesWhatTheRecordOwnsToTheSweep`,
`TestAPermanentDeleteStripsTheMergeCopiesOfItsValues` and
`TestAPermanentDeleteErasesAMappedTargetsCopyOfTheValue`, each with a value
the scan looks for; `TestPurgeThenPutStartsARecordOver` and
`TestPurgeTombstonesCascadeChildren` (`internal/engine/core_db_test.go`) for
0122's behaviour.
