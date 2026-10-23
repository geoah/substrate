---
status: proposed
date: 2026-10-23
decision-makers: George Antoniadis
---

# 0188. The Observations page folds a metric's sources into one row, and keeps channels apart

## Context and Problem Statement

The console's Observations page (`/observations`) listed one row per
stream: a metric from one source about one thing on one channel. A metric
two sources measured was two rows with two newest values, so the reader
compared dates to find the current one. On one production repository every
blood marker tested by two labs appeared twice among 319 rows
([#1024](https://github.com/geoah/substrate/issues/1024)). The open question
was what one row folds: sources only, or a source's channels too (a WHOOP
workout's heart rate beside its cycle's).

## Considered Options

- Keep one row per stream, and leave the comparison to the reader.
- Fold sources into one row per metric, about and channel.
- Fold sources and channels into one row per metric and about.

## Decision Outcome

Chosen: fold sources into one row per metric, about and channel, because a
second source of one measurement is the same question answered twice, while a
channel is a different context. A WHOOP workout's heart rate is not a reading
of the resting day its cycle's heart rate describes, so folding it would put
a workout's 141 /min forward as the day's newest heart rate.

The folded row names its sources, shows the newest value across them and
whose it is, and counts every member's points. It leaves **Covered** empty,
because the time each source covered overlaps and does not add up. A toggle
beside the metric opens the row to its streams, newest first. Its link opens
the timeline on every member source, one line each (`group=source`).
Grouping by source shows the streams unfolded, since a folded row belongs to
no single source. A stream no other source shares stays a row of its own.
The selection stays one token per stream, so a folded row's box selects every
member.

### Consequences

- Good, because a metric reads once with its current value, and the 319
  rows of the repository above lose every duplicate lab marker.
- Good, because the selection, the address and saved views are unchanged:
  they still name streams.
- Bad, because the folded row's form and statistic are its newest member's,
  so a metric that one source writes as samples and another as a vendor
  summary shows only the newest one's form until the row is opened.
- Bad, because the row count no longer equals the stream count; the page
  header and the group heads count streams, and the grid shows fewer rows.

### Confirmation

`web/console/src/pages/observations.test.tsx` holds one row for heart rate
from two sources, its sources, newest value and summed points, the link
naming both sources, the toggle that opens it, and WHOOP's workout channel
as a separate row. `web/console/src/lib/stream-table.test.ts` holds
`foldSources` keeping a channel and a thing apart.

## More Information

Revisit if a reader needs channels folded too: that is a grouping choice
on top of this one, not a change to what a row is.
