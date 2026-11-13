---
status: accepted
date: 2026-11-13
decision-makers: George Antoniadis
---

# 0283. Trigger run records keep a week of history, and positions stay in the log

## Context and Problem Statement

[0064](0064-trigger-bookkeeping-is-a-delivery-ledger-folded-from-the-changelog.md)
made a trigger's bookkeeping part of the changelog: the cursor, the schedule,
the parked failures and the paged drains are `delivery` entries the fold
reads back, and every delivery also writes a `triggerrun` record. History
keeps all of them forever, though the current state keeps only the newest
runs of each trigger. The 1.0 plan (pull request 1313, row 8, and its table
of records to amend) amends 0064: positions stay in the log; run records get
short retention. A kind can now state its retention (0282).

## Considered Options

- Retention for the `delivery` entries too.
- Retention for the run records alone, positions untouched.

## Decision Outcome

Chosen: the run records alone. `core/triggerrun` declares `retention: P7D`,
and so does `core/alert`, the other record the substrate writes about its
own runs (an alert opens, updates and resolves in place; 0148). Each
record's latest state is always kept, so the run that parked and the alert
that is open are read as before; what goes after a week, once the compaction
lands, is the run's and the alert's own history of updates.

The `delivery` entries are not records of a kind and carry no retention:
they are the positions a trigger resumes from and the parked failures a
retry names, and 0064's guarantee that a restored directory resumes every
trigger where it stopped rests on them. They stay in the log as 0064 has
them.

This amends 0064 and replaces none of it.

### Consequences

- Good, because what is probably most of a busy repository's history, runs
  nobody reads after a week, stops growing it once the compaction lands.
- Bad, because the history of a run or an alert older than a week will not be
  shown; its latest state is.

### Confirmation

`TestEveryShippedKindsRetentionIsHeld` (kinds) holds `triggerrun` and `alert`
at `P7D`.
