---
status: proposed
date: 2026-10-19
decision-makers: George Antoniadis
---

# 0175. A record view draws tiles, trends and tables from one batched read per block, and a band is view data

## Context and Problem Statement

A record view
([0166](0166-a-record-page-layout-is-a-record-in-a-seeded-console-package.md))
reads raw points over a record's own window, one aggregate read per metric, and
draws lines. WHOOP writes nightly summaries stamped at the sleep onset, labs
write a value a few times a year with its own reference range, and a room or a
meter is a record with no window at all, so the sleep page says nothing was
measured, the cycle page is the generic sheet, the lab report lists titles, and
the metric page has no trend. The engine serves two aggregate reads at once per
process, so a page cannot fire a read per number. The plan is [record view
blocks](../plans/observations/record-view-blocks.md).

## Considered Options

- Widen `recordview` with blocks that answer one value, a trend and a table,
  each batched into one `metrics` read, with bands as block data scoped to a
  metric.
- A seeded `metricview` kind holding each metric's presentation (direction,
  bands, headline reducer, mark), and built-in metric and observation pages
  keyed on the seeded traits.
- A seeded calendar `page` kind whose window comes from the address, with the
  same blocks.

## Decision Outcome

Chosen: widen `recordview`, because it adds no kind and no engine code, keeps
every layout a data record a sample ships and an owner edits, and gives the
pages the owner asked for: dials with WHOOP's bands, contributors
against the last 30 nights, trend strips, a lab table with nested unit twins, a
biomarker trend with each result's own range, a room and a meter page over the
shipped climate and energy metrics. The window grammar grows (`open`, `last`,
`aboutSelf`, `sourceSelf`; `start` and `end` optional), `chart` reads summaries
and channels and draws bands, and `stats`, `trend`, `table`, `breakdown` and
`lanes` join the six blocks; the console package moves to version 2. A
`metricview` record is the better long-term home for a metric's bands and
direction and stops a band repeating per view; it is the next step, with
block-level `bands` becoming the per-view override. A calendar page kind is the
right shape for day and week pages and builds on the same blocks later.

- Every block issues one `metrics` read, never one per metric. A stats block
  issues one more for each further way it reads an already-read metric
  (another `from`, `statistic` or `unit`), separate reads for its coded items,
  and twice that with a baseline; a cycle page costs eight.
- A block's grammar keeps scalars in its lists and bands at block level,
  because a declared field nests four levels at most and a level-four field is
  a scalar.
- `from` is a plain string the console checks, because every `values` entry
  must be one lowercase word and `prefer-raw` is not.
- The console names no sample, metric id or sample kind; WHOOP's thresholds
  ship in the healthwhoop sample's view.
- Bumping `healthwhoop` to ship a view also moves its `ADAPTER` stamp, as the
  version test requires.

### Consequences

- Good, because a sample adds a WHOOP cycle page, a lab report table or a room
  page by shipping a record, and the owner edits bands, columns and baselines
  in place.
- Good, because a page's reads are batched and a test counts them for every
  shipped view, so a view edit or a batching change that multiplies a page's
  reads fails before it ships.
- Bad, because a band lives in every view that draws it until a per-metric
  presentation record exists; WHOOP's bands repeat in the cycle and the sleep
  views.
- Bad, because the `lanes` block is declared before any console draws it, so a
  view using it shows a notice on this console.
- Bad, because a view is checked only as far as the grammar reaches: a stats
  item over a window its metric never lands in draws "not measured", and
  nothing says the window was wrong.

### Confirmation

`internal/engine/recordview_db_test.go` admits a view with every new block and
a window with no end, and refuses a bare-id metric in a stats item, a table
kind that is no kind, a trend span under two and a band tone outside the list.
`web/console/src/lib/record-view.test.ts` pins every read's JSON,
`record-view.shipped.test.ts` holds every shipped view to the console rules and
to shipped metric paths, `console-names-no-sample.test.ts` keeps the console
free of sample names, and
`web/console/src/components/record-view/shipped-reads.test.tsx` mounts every
shipped view and holds it to its count of aggregate reads (the cycle page's
eight among them). `TestBootUpgradeMovesAConsole1RepositoryWithLiveViewsToConsole2`
in `internal/engine/recordview_db_test.go` opens a repository at console
package 1 holding imported, owner-edited and owner-written version 1 views on
this build: the boot upgrade lands version 2, and every view reads back
unchanged and still writes.

## More Information

Record
[0166](0166-a-record-page-layout-is-a-record-in-a-seeded-console-package.md) is
the mechanism this extends; [record view
blocks](../plans/observations/record-view-blocks.md) is the plan with the
worked views. Reopen when a per-metric presentation record lands (bands move
off the views) or when a calendar page kind is built.
