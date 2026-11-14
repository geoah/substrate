---
status: accepted
date: 2026-11-14
decision-makers: George Antoniadis
---

# 0305. A permanent delete reaches what the record owns and what asks to change it

## Context and Problem Statement

A record's values live in more places than its own lines: the records it
owns through `onDelete: cascade`, the change requests that target it, the
records merged into it, and the mapped targets that copied a value from it.
Step 24 of the 1.0 plan (#1313) had to say which of these a permanent delete
erases ([0304](0304-a-permanent-delete-erases-the-records-history-and-the-intent-is-in-the-history.md)),
and what it leaves, because the person asked for the thing to be gone and
is owed a plain answer about what is not. It replaces
[0122](0122-a-delete-may-purge-so-a-put-starts-the-record-over.md), whose
purge tombstoned what the record owns for the collector's sweep and kept
the record's history.

## Considered Options

- Erase the record's own history alone, and tombstone what it owns, as 0122
  did.
- Erase it with everything that holds its values by a typed link, and say
  plainly what holds them otherwise.

## Decision Outcome

Chosen: everything that holds its values by a typed link. What 0122 chose
stands: `delete` takes `purge`, the record is collected in the delete's own
transaction, and the next `put` at its id creates a fresh record, once what
it owned is erased (below); a record a finalizer holds refuses with `409`
and the delete rolls back, a purge through a merge loser's former id refuses
with `409` naming the canonical id, and a declaration record refuses with
`422`. What changes is what the purge reaches. It purges and erases, and the
reply's `erasure.purged` names them:

- what the record owns, its whole closure: a record owns another when the
  other's own top-level reference to it, or to one of its former ids,
  declares `onDelete: cascade`, pinned to its kind or to a trait it
  implements; an owned record already tombstoned by an earlier delete is
  taken too, and the closure goes in batches (below);
- each change request whose target is the record, pending or settled, found
  by its typed link: closed by the delete, and erased with it;
- each record merged into it, by its former ids, and the merge's and any
  split's own records, which keep what moved;
- each mapped target's copy of a value it derived from the record, found by
  the mark's `derivedFrom` or by the subject its mapping points at, which an
  earlier delete's tombstone leaves: the target's history up to the line
  that released the copy is compacted, its current state kept.

A merge or a split line that outlives the erasure, because the other record
of the pair lives on, keeps its fold and the ids it names, and loses the
copies of the pair's values it carried. A record that points at the deleted
one by any other reference (one with no `onDelete`, or one nested inside an
object) is not owned: it stays as it is, its values, the link's properties
included, are its own, and its pointer dangles.

What the record owns goes in batches, so that no transaction grows with the
closure. The delete's transaction writes the record's own `erase` effect
marked `owned`, naming the hand its line acted for and the ids besides its
own it owns through, its former ids, which its purge takes the rows of; it
purges nothing it owns. A batch is one transaction under the changelog
lock that takes up to 100 records the record owns directly, live or
tombstoned, and does to each exactly what the request does to the record it
was asked to delete, by the same function: what the tombstone and the purge
take away is read first (the related records above, the mapped copies, the
ids it owns through), the tombstone goes through the mapping's own
lifecycle, then the purge and the intents. Only what that record owns in
turn waits, for its own `owned` intent. A record a finalizer holds is
tombstoned by the batch that meets it and waits, and the closure with it,
until the finalizer is released. A batch that finds nothing left takes the
owner's intent out, and that transaction, like every erasure, holds above a
line known not to verify. The request runs one batch in all: a record that
owns fewer than 100 records, none of which owns any in turn, is done in the
request, and every deeper level goes to the compaction pass. The pass runs
the rest within its budget, sleeping as long as each batch took, and while a
closure waits it comes back a minute after it ends rather than at its
interval. The collector's sweep leaves a tombstone a standing closure has
still to erase. A batch's lines are the engine's, acting for the person the
deleting line acted for; the fold and the marks need nothing from it, since
the rows and marks it writes go with the records.

Until its batch, an owned record stays as it is, listed, read and written
like any other, and the reply says so. A put or a patch of one the batches
have not reached lands, and is erased with the rest, its later lines
included. A record written while the intent stands, with a cascading
reference to the purged id, is owned by it and goes with the rest; one
written after the intent is gone dangles; and a reference that declares
`mustExist` refuses either, since the record is gone. A create at the
deleted id, or at the id of an owned record that owns in turn, is refused
until that record's batches are done, as `503 unavailable` with the reason
`erasing`, because the batches find what it owns by the id and would take a
new record's too, and so is a create at one of its former ids. A delivery
that meets the refusal is retried by itself where a temporary failure is,
so a sync that writes the record again gets through once the pass has
finished; where its code may not be run twice unattended (a function that
may run an agent or call another), it parks for a person, with the reason
and when to retry. The reply's `erasure.owned.remaining` is a floor: what
the record still owns directly, counted no further than a batch and one,
while what those own in turn is counted only as the batches reach them.

A permanent delete is refused `not_permitted` when the record's kind can
own, through `onDelete: cascade` and transitively, a control kind or sign-in
material that the caller could not delete directly, and the refusal names
that kind, whether or not this record holds such a child. The check reads
the declarations, kinds and not records, so it is decided before anything
is purged and is never met halfway through a closure. An admin is never
refused by it. It lands with the roles, which this record's branch does not
yet carry.

The `gc` line stays, carrying no value: its op, the record's kind and id and
the hand that deleted it, until the compaction pass forgets the deletion
past the deletion period, as it forgets every old deletion
([0300](0300-a-paced-pass-compacts-what-is-past-each-kinds-retention-and-forgets-old-deletions.md)).
It is how a reader learns the record went, and what a rule that must
remember a shipped record's removal can read. Whether
the record seed reads it is that rule's to decide, and it is open.

Out of reach, and said so in the docs: text another record holds that
quotes the record (an agent's message, a memory pinned from it, a tool
call's arguments or result, an alert's title, a change request's diff of
another record); a request already in the webhook spool; a provider's sync,
which brings a mirror record back unless it remembers the deletion, which
is open too; and every copy taken before the delete.

### Consequences

- Good, because what the person deletes takes with it every record that is
  its part or exists only to change it.
- Good, because a mapped target keeps its current values and loses only its
  own history of the copy.
- Bad, because what a purge used to leave as a tombstone for the sweep is
  gone at its batch: a client that read an owned child after its owner's
  purge gets `404` once the batch reaches it.
- Bad, because a large closure takes several passes, and until its batch an
  owned record is still there to read.
- Bad, because a record created again at the deleted id, an account
  connected again among them, waits for the batches: its create answers
  `erasing` until they are done.
- Bad, because a quoting record keeps the quote: nothing finds it reliably.
- Bad, because a mirror deleted permanently comes back at its next sync
  until providers remember deletions.

### Confirmation

`TestPurgeThenPutStartsARecordOver`, `TestPurgeErasesOwnedChildrenAndLeavesReferrers`,
`TestPurgeRefusesAHeldRecord`, `TestPurgeRefusesADeclarationRecord`,
`TestPurgeRefusesAFormerID` and `TestPurgeUnderIfVersion`
(`internal/engine/core_db_test.go`);
`TestAPermanentDeleteReachesWhatTheRecordOwnsAndWhatAsksToChangeIt`,
`TestAPermanentDeleteTakesWhatItOwnsInBatches`,
`TestACreateAtAnIdWhoseClosureStandsWaitsForIt`,
`TestADeliveryRefusedWhileErasingGetsThroughOnItsOwn`,
`TestEveryShapeIsErasedTheSameDirectlyAndByABatch` (each shape erased
directly and by a batch, the same rows left),
`TestTheSweepLeavesARecordAClosureHasStillToErase`,
`TestTheLastTransactionOfAClosureHoldsAboveAKnownCut`,
`TestAPermanentDeleteStripsTheMergeCopiesOfItsValues` and
`TestAPermanentDeleteErasesAMappedTargetsCopyOfTheValue`, each with a value
the scan looks for.
