---
status: accepted
date: 2026-11-14
decision-makers: the 1.0 plan's orchestrator session, for George Antoniadis
---

# 0320. A trigger's own history is bookkeeping and keeps seven days

## Context and Problem Statement

Every delivery writes a run record (`core/triggerrun`) and a `delivery`
line on the trigger itself (`core/trigger`): its cursor moving, a schedule
firing, a failure parked, a paged drain's page. The shipped defaults
([0282](0282-a-kind-declares-how-long-its-history-is-kept-and-a-repository-may-override-it.md))
keep run records seven days and `core/trigger` in full, and
[0283](0283-trigger-run-records-keep-a-week-of-history-and-positions-stay-in-the-log.md)
kept the delivery lines in the log, because they are the positions a
trigger resumes from and the failures a retry names. The compaction pass
compacts no kind kept `all`, so a trigger's delivery lines are the one
bookkeeping that still grows for good: a sync firing every five minutes
writes about 105,000 a year, and on one repository two triggers hold about
10 GB of page lines from before a page stopped carrying its whole cursor
([#839](https://github.com/geoah/substrate/issues/839)). A paged delivery
that fails saves its position whole on such a line, and that position can
hold values of the records it was listing, which a permanent delete finds
only where the delivery was about the erased record
([#1397](https://github.com/geoah/substrate/issues/1397)).

## Considered Options

- `core/trigger` states `retention: P7D`.
- `core/trigger` keeps `all`, and the pass compacts a trigger's delivery
  lines alone after seven days, leaving the lines that edit it.
- Leave it: an owner who wants it writes a retention override.

## Decision Outcome

Chosen: `core/trigger` states `retention: P7D`, as `core/triggerrun` and
`core/alert` do. This amends 0282's shipped defaults for this kind, and
0283's second paragraph: a trigger's delivery state stays, its lines do not.

What always stays is what 0283 kept the lines for. The compaction replaces
a trigger's delivery effects with one `ledger` entry, its delivery rows as
they stood
([0296](0296-a-records-history-is-compacted-into-a-state-entry-in-the-line-of-its-newest-write.md)):
its cursor, its schedule's fire state, its parked failures and the resume
row of a paged drain that has not finished. A restore and a rebuild fold
them back as they folded the lines, so 0064's guarantee, that a restored
repository resumes every trigger where it stopped, holds. The trigger's own
row stays in its `state` entry, and the lines a compaction never replaces
stay: one a live parked failure, a parked webhook fire or a live drain
names, with the trigger's effects under it, until it goes, and a trigger's
delivery effects while the webhook spool holds a request. Which package
shipped a trigger is read from the hands the history keeps, which a state
entry keeps too
([0319](0319-the-hands-that-wrote-a-shipped-record-outlive-its-history.md)).
After a week each delivery line and each edit of the trigger is the pass's
to take, and goes when a pass reaches it: a pass works a little of each
interval, a long history takes many passes, and what holds a line keeps it
until the hold ends.

A trigger's own history is bookkeeping: what the substrate wrote about its
own runs, read back by nothing but the fold, which the ledger entry serves
as well. The 1.0 plan's page (pull request 1313) counts it so: every time a
trigger runs, a run record and a delivery entry are written to history, and
seven days are "for bookkeeping such as run records". The edits go with
it: the trigger's definition is its latest state, which stays, and the
first option is the one a kind's retention can state today.

The second option needs a retention per family of a record's effects,
which no declaration can state and the pass has no unit for. Leaving it
keeps the growth by default, and with it every saved position no erasure
can find.

This closes #839 without a rewrite command: the pass replaces the old page
lines with the ledger entry, in steps held to its line bound, as it
compacts any record.

### Consequences

- Good, because a trigger's history stops growing for good, and the old
  page lines of #839 are the pass's to take from a week on, over as many
  passes as they need.
- Good, because a saved position no erasure can find is the pass's to take
  a week after the drain it belongs to ends.
- Bad, because a trigger's edits and deliveries older than a week are no
  longer in the history: its latest state and its delivery state are.
- Bad, because the first passes over a repository with a long trigger
  history do that work, a batch at a time, before the pass is idle again.

### Confirmation

`TestEveryShippedKindsRetentionIsHeld` (kinds) holds `core/trigger` at
`P7D` with the bookkeeping kinds. The tests in
`internal/engine/triggerhistory_internal_db_test.go` hold what stays, what
goes, a history longer than the pass's line bound, and an owner's override.

## More Information

Amends 0282 and 0283. Built in the pull request that closes #1397 and #839.
