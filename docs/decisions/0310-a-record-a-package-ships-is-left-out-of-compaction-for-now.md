---
status: accepted
date: 2026-11-14
decision-makers: the 1.0 plan's orchestrator session, pending George Antoniadis
---

# 0310. A record a package ships is left out of compaction, for now

## Context and Problem Statement

An open writes the records the seeded packages ship: the console's views
([0229](0229-a-collections-shipped-saved-views-are-records-of-a-seeded-console-kind.md)),
the models
([0242](0242-a-model-a-provider-offers-is-a-record-and-an-agent-may-run-on-a-list-of-them.md)),
the units and conversions
([0186](0186-units-and-conversions-are-core-records-and-a-value-converts-through-the-repositorys-graph.md)),
the three vendor provider rows and the three roles. An install or an upgrade
of a package writes the data records the package ships, such as a
catalogue's metrics. Either writes a record again where it is missing or
differs from what ships, unless a hand other than the shipping ones ever
wrote or deleted it, and it reads that from the record's history
([0191](0191-a-shipped-record-is-held-by-any-hand-but-the-one-that-ships-it.md)).
That is how the owner's edit and deletion stand.

A compaction
([0296](0296-a-records-history-is-compacted-into-a-state-entry-in-the-line-of-its-newest-write.md))
puts a record's state on its newest line and removes the lines under it.
Where the newest line is a shipping hand's, the owner's line goes with them:
the sweep's purge of a metric the owner deleted, or the engine's backfill of
a view the owner edited. The next open or upgrade then finds no other hand
and writes the record over what the owner did. Forgetting an old deletion
removes every line of the record, with the same result.

## Considered Options

- Leave a shipped record's history out of compaction, until the rule reads
  something other than the history.
- Keep a short list of the shipped records an owner removed or changed, and
  have the seeds and the installs read it instead of the history.
- Accept that a shipped record comes back once its history is compacted.

## Decision Outcome

Chosen: leave a shipped record's history out of compaction, as an interim.
No compaction replaces an entry of a record a package ships, and nothing that
removes a record's entries for their age takes one. Its history stays whole,
and the rule reads it as it did before any compaction. It is the one option
that changes nothing a release has stored and nothing on the wire, so the
other two stay open.

"Ships" is one predicate, `shippedAmong` in `internal/engine/recordseed.go`,
with two ways to answer it, both asked when the compaction runs:

- the lists the open's seeds write from, each where its seed runs. There is
  no second list, and a record the seeds stop naming loses the exemption;
- the hands that ever wrote the record, where one is a package's own
  (`bundle:<authority>:<package>`, the hand an install and an upgrade write
  under), whoever wrote the record since. A package's functions, its sync
  and its agents write under their own hands, so what they mirror is
  compacted as anything else.

A declaration and a trigger are neither. An upgrade decides a declaration by
its version, never its history. An install writes a package's triggers again
whatever their history says, so keeping a trigger's history, whose delivery
entries are most of what compaction takes, would protect nothing.

A state entry keeps who wrote what it replaces and nothing of what they
wrote: the actor of every line it replaces, the one that line acted for and
the line's operation (`writers`), joined with those of a state entry it
replaces in turn. Every rule that asks which hands ever wrote a record reads
the lines that remain and these, through one reader (`recordHands`): which
package shipped a webhook trigger, and whether an actor other than a shipped
record's shipping hands ever wrote it. This set is what lets a rule ask about
past writers after a compaction. The exemption remains because a deletion that is forgotten
leaves no state to hold it.

A door that erases a record's history at its owner's word is the owner's
explicit act, and it may still take a shipped record's values, where the rule
reads the owner's hand afterwards. Where it would not, the erasure of that
shipped record is refused.

### Consequences

- Good, because an owner's edit or deletion of a shipped record outlives any
  compaction, as it did before there was one.
- Good, because nothing a release has stored changes, so either of the other
  options can replace this without a conversion.
- Bad, because the history of every shipped record is never compacted: about
  390 records with this tree (8 views, 10 models, 365 units and conversions,
  3 provider rows and 3 roles), and each installed package's data records,
  109 across the catalogues and providers this tree serves. An earlier value
  of one stays until its owner erases it.
- Bad, because the deletion of a shipped record is never forgotten: the
  history keeps its lines for good, after its package is removed too, since
  a later install may write the record again.

### Confirmation

The tests in `internal/engine/compactshipped_internal_db_test.go` and
`TestACompactionKeepsWhoShippedAWebhookTrigger`: a seeded
record and an installed package's record that the owner edited or deleted,
compacted, then opened or upgraded again, are as the owner left them; one
nobody touched still takes what ships next; and an ordinary record, and one
a package's function wrote, are compacted beside them; and a webhook
trigger a package wrote, compacted, is still refused open.

## More Information

The ways out this keeps open, any of which replaces it:

- a short list of the shipped records an owner removed or changed, kept for
  good and read by the seeds and the installs in place of the history,
  which then compacts like any other;
- accepting that a shipped record comes back once its history is compacted,
  so an owner's removal lasts as long as its history;
- this, for good.

A shipped trigger the owner deleted comes back at its package's next
upgrade, with or without compaction: the install's rule exempts the triggers
a package ships, and this record does not change it.

Revisit when the rule reads something other than the history: step 25 of the
1.0 plan (#1313), which is stopped until this is settled.
