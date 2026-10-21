---
status: proposed
date: 2026-10-21
decision-makers: George Antoniadis
---

# 0185. A table draws its results as tiles, and every tile takes one size

## Context and Problem Statement

A lab report's page lists its results in a table
([0175](0175-a-record-view-draws-tiles-trends-and-tables-from-one-batched-read-per-block.md)):
a row per analyte, its flag in a word, its range in a cell. The stats block
draws the figures of a WHOOP cycle as tiles with dials, bars, bands and a
change, but its items are fixed: a metric the view names, the metric a
reference property of the record holds, or a property of the record. A
report holds none of its results' metrics, and one report holds 36 analytes
where the next holds 115 or a DXA scan's regions, so no fixed list of items
fits it.

Each stats tile also took the width of what it held (a wrapping row with a
160 px floor), so the tiles of one block came out at three widths and
several heights.

## Considered Options

- Add `show: tiles` to the table block
- Let a stats block repeat its items over the records pointing here
- A Mneme view naming fixed analytes by panel, with their ranges written in
  the view

## Decision Outcome

Chosen: add `show` (`rows` or `tiles`) to the table block, because the table
already reads the records, nests a converted twin and knows a result's flag
and reference range by the core `observed` and `result` traits; tiles are
another way to draw the same read.

Under `tiles`, each listed result of an observed single-value kind is a tile:
its metric's name (and its channel) linked to the result's page, its value
with its flag in the flag's tone, a bar against its first reference range,
and its change since the previous result of the same metric and channel. One
more list read finds the previous results: the listed kind's records of the
listed metrics before the earliest listed result, the 100 newest, about whom
the table's read is. A twin is said on its result's tile. A flagged result
comes first, the rest by name. A kind that binds no observed single value
draws rows and says why.

Every tile, a stats block's or a table's, sits in one grid: up to three to a
row, each a third of the row and at most 320 px wide, fewer below three
176 px tiles, every tile as tall as the tallest.

A stats block repeating its items needs a second record context inside a
block, and its bands and baseline mean nothing per result. Fixed panels
write each lab's ranges into the view, which the result already carries, and
draw "no reading" for every analyte a report did not test.

### Consequences

- Good, because a report of any size and any lab draws, with the ranges and
  flags the lab printed.
- Good, because `show` is a new optional field: the console package moves to
  4 and every stored view reads and writes as it did
  ([0184](0184-a-record-view-grows-by-adding-a-field-never-by-retyping-one.md)).
- Bad, because a result whose previous one is older than the 100 newest
  earlier results of the report's metrics shows no change.
- Bad, because the change carries no tone: a view names no better direction
  for an analyte.
