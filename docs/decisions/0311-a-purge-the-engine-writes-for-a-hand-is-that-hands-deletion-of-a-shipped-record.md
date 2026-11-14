---
status: accepted
date: 2026-11-14
decision-makers: the 1.0 plan's orchestrator session, pending George Antoniadis
---

# 0311. A purge the engine writes for a hand is that hand's deletion of a shipped record

## Context and Problem Statement

An open's seeds and a package's install write a record they ship again
unless a hand other than the shipping ones ever wrote it
([0191](0191-a-shipped-record-is-held-by-any-hand-but-the-one-that-ships-it.md)).
[0310](0310-a-record-a-package-ships-is-left-out-of-compaction-for-now.md)
reads that from the hands that wrote the record, the lines that remain and
the `writers` their state entries keep, and asks only about each line's
actor.

A permanent delete purges records under the engine's hand, acting for the
person who asked
([0305](0305-a-permanent-delete-reaches-what-holds-the-records-values-and-not-what-it-owns.md)).
When one of them is a shipped record, its erasure leaves one line: the
engine's purge. To the rule that line is a shipping hand's, so the next open
or upgrade writes the record again, which the owner deleted.

## Considered Options

- A record's purge (`gc` line) the engine writes acting for a hand that is
  not a shipping one is that hand's deletion.
- Every line the engine writes acting for a person is that person's.
- Leave the rule on the actor alone.

## Decision Outcome

Chosen: a purge (`gc` line) the engine writes acting for a hand that is not
a shipping one is that hand's deletion, to every rule that asks whether
another hand wrote a shipped record (the seeds, the unit seed, the
installer).

It covers what a permanent delete purges in the person's name:

- what it takes with the record: a shipped record merged into the one
  permanently deleted is purged with it, and stays gone;
- what the record owned: the delete's cascade takes it, and a shipped
  record among it stays gone. The cascade's line is a `gc` by its
  operation although the record it names is tombstoned for the sweep, not
  purged, so nobody should read the operation as "purged".

It covers no other line the engine writes. Every request binds whom it acts
for, so every line the engine writes inside one acts for the request's
person: a package's purge, a recompute, a mark. Were those the person's, a
package purged and installed again would come back without its shipped
records, because the purge's `delete` lines would hold each one. That is
why the wider option lost.

Leaving the rule alone lost because the owner's permanent delete would not
stand.

The rule reads a line's operation as it reads its actor. A state entry keeps
the operation of each line it replaces in `writers`, so the answer is the
same after a compaction.

### Consequences

- Good, because an owner's permanent delete stands for every record it
  takes, as it does for the record they named.
- Good, because a package's purge and install still behave as they did.
- Bad, because the rule now reads a line's operation and whom it acted for,
  where it read one column: a new door that purges for a person must decide
  which it means.
- Bad, because a shipped record whose deletion the sweep made in no one's
  name is not covered (below).

### Confirmation

In `internal/engine/compactshipped_internal_db_test.go`:

- `TestAShippedRecordMergedIntoOneErasedStaysGone` and
  `TestAShippedRecordTheErasedRecordOwnedStaysGone`, the two cases it
  covers;
- `TestAPackagePurgedForAPersonShipsItsRecordsAgain`, the door the wider
  option breaks;
- `TestTheEnginesPurgeForAHandSurvivesACompaction`, the answer after a
  compaction of the purge.

And `TestAPurgeTheEngineWritesForAHandIsThatHands`
(`internal/engine/recordhands_internal_test.go`), the rule over each hand.

## More Information

Amends 0310 on this one point. It stands until the shipped-record rule stops
reading history, the plan's step that is stopped for the owner.

The one known case it does not reach: the winner deleted without purge and
collected by the sweep after the deletion period. That `gc` is the engine's
own, acting for no one, so a shipped record merged into it comes back at the
next upgrade, as it does today.
