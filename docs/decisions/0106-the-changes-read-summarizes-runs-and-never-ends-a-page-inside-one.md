---
status: accepted
date: 2026-09-26
decision-makers: George Antoniadis (via the issue-674 agent session)
---

# 0106. The changes read summarizes runs and never ends a page inside one

## Context and Problem Statement

The console's Home and History pages fold consecutive changes into one
sentence ("You added 60 tasks") in the browser, over one page of
`GET /api/v1/changes`. A run that crosses the page edge is cut short, so Home
reads up to four more pages and then says the run without a count
([#674](https://github.com/geoah/substrate/issues/674)). A client needs the
count of a run from one read.

## Considered Options

- Return runs beside the rows of an ordinary page, extending the oldest run
  below the page.
- A `runs=1` form of the history page whose `first` counts runs, each read to
  its end, grouped by actor, kind and a verb.
- Count the oldest run in SQL (find the row that breaks it, count the rows
  above), a new storage method.

## Decision Outcome

Chosen: `runs=1` on the history page, computed in the API over
`ChangesBefore` in batches of 500. The page asks for runs, so `first=6` is six
sentences, and every run carries its exact `count`, its distinct `records`,
its seq range and its time span. The verb is `create`, `restore` or `update`
for a `put` (by the payload's `created` and `restored` flags; a `patch` is an
`update`) and the op otherwise, which is where the console's own fold draws
its lines. The filter applies first, so runs are consecutive in the filtered
feed. Rows beside runs would leave the client counting how many runs a page
of rows holds, which is the guess this removes. A SQL count needs the verb
spelled in SQL beside the Go one, and the batch walk costs little at personal
scale.

### Consequences

- Good, because one read answers "60 tasks", and the cursor is the oldest
  run's `oldestSeq`, so the next page starts at the row that broke it.
- Good, because the storage contract is unchanged: the fake and the engine
  answer the same `ChangesBefore`.
- Bad, because the read costs what its runs hold: a sync that wrote 50,000
  rows in one run reads 50,000 rows to count them.
- Bad, because a run is not split by time. Two bursts of the same act a day
  apart with nothing between them read as one run; a client that splits by
  time still needs the rows.
- Bad, because a run carries no row: a client that renders one record's
  values reads that record, or the `changes` page, separately.

### Confirmation

`internal/api/changeruns_test.go`: a run longer than one batch comes back
whole with the cursor under it, the verb splits `put` three ways and folds
`patch` into `update`, a filtered-out row does not break a run, and the
refusals (`runs` other than `1`, with `watch` or `from`, a continuation with
no generation).

## More Information

Revisit when a client needs runs split by a time gap (a `gap` parameter is
additive), or when a run's cost shows up in the read's latency: the SQL count
is the next step.
