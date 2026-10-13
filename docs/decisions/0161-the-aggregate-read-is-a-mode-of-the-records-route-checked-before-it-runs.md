---
status: proposed
date: 2026-10-11
decision-makers: George Antoniadis
---

# 0161. The aggregate read is a mode of the records route, checked before it runs

## Context and problem statement

Every observations screen (a steps calendar, heart rate zones per week, kWh
per day, hours a door was open) is a bucketed reduction, and functions and
agents need the same answers. The records read is one route
([0079](0079-graphql-is-removed-and-the-records-read-is-one-route.md)), and
the request and response are a wire contract the console mirrors by hand, so
their shape is hard to change once clients exist
([the plan, section 8](../plans/observations.md),
[reading the data](../plans/observations/proposal/03-reading-the-data.md)).

## Considered options

- A mode of `GET /api/v1/records`, selected by `aggregate=`
- A second route for aggregates
- No server aggregation: clients and functions page through records and
  reduce them
- Free-form expressions, in the manner of PromQL or SQL

## Decision outcome

Chosen: a mode of the records route.

- **Selector.** At most one of `metric` (a full reference; the index is
  read), `metrics` (up to 16 full references read over one range, bucket,
  `groupBy` and sources policy, each with its own unit, reducers, `from`
  and summary statistic, checked each on its own and held to the budgets
  together; every row names its metric, and in the recorded-local calendar
  the rows of one bucket share its bounds, so the metrics join on `start`)
  and `property` (a declared number, quantity, money or duration
  property on the filtered kinds, read from `records` in exact arithmetic,
  grouped by the records' own string, enum or reference properties where
  `property.groupBy` names them), never a bare word. A record's UTC offset
  is a property of its kind, so the recorded-local calendar places a
  property read's record by the declared offset property `property.utcOffset`
  names, and refuses a record holding none as it refuses an offsetless
  reading. A bucketed read names one of the three; the latest read may name
  none and select its streams by `filter` alone.
- **Four named time modes.** `{"every", "origin"}` is a physical window on
  UTC instants. `{"calendar", "timezone"}` is a calendar window in a named
  zone, 23 or 25 hours across a clock change. `{"calendar"}` with no zone is
  the recorded-local day, where each reading's own offset decides its date.
  `{"records": {"filter", "around"}}` uses each matched record's
  `[at, endsAt)` as a bucket, where an amount counts by the part of its span
  inside, its number prorated linearly and the row counting the amounts it
  cut in `prorated`; in the other modes a point lands whole by its start. A reading with no offset is bucketed by a named
  zone or refused, never by UTC or the server's clock: a calendar with a
  `timezone` places it by that zone, and the recorded-local calendar refuses
  a stream holding one (a `422` naming the stream and
  `bucket.fallbackTimezone`) unless the request names
  `bucket.fallbackTimezone`, the zone it places such a reading by, each row
  counting those readings in `offsetless`.
- **Typed reducers**, checked against the value's form
  ([0156](0156-the-forms-are-gauge-amount-counter-and-summary-and-live-on-the-value.md)),
  each answering with its own unit.
- **`from`**: `raw` (the default), `summaries` or `prefer-raw`. One bucket
  never mixes raw points and vendor summaries. `prefer-raw` uses raw points
  where a bucket holds one and its coverage reaches `minCoverage` (default
  0.9) and the summary otherwise, and each row says which it used.
- **Source policies** (`separate` by default, `priority` with an ordered
  list, `summaries`) resolve per stored interval before bucketing, so a total
  does not change with the bucket width. `priority` weighs the higher
  sources' amounts and summaries around the read's windows too, so a total
  does not change with where the range starts either, and under `priority`
  and `summaries` the one-statistic rule holds per source and per row.
- **Records' source policy.** The records mode's windows and the property
  read's records are records several sources write of one night or one run.
  `bucket.records.sources` and `property.sources` (`separate` by default, or
  `priority`) drop a record whose own `[at, endsAt)` overlaps a kept record
  of a higher-ranked source, the amounts' rule, optionally only where named
  `match` properties are equal; a record with no `endsAt` drops no record
  with a span. The request names the property holding a
  record's source, because core contracts no source on a record that does
  not bind `observed`, and the policy takes its own `order`, apart from the
  points' policy, so a read may take one source's nights and another's
  stages.
- **`fill` and `rolling`.** `fill` makes a group's buckets dense (`null`,
  `zero` or `locf`), and `rolling` adds `avg`, `sum`, `min`, `max` or
  `stddev` over a window of buckets ending at each bucket or the one before
  it, reading the buckets the window needs before the range.
- **Budgets.** `maxBuckets` (series times the buckets the read answers,
  default 1,440, cap 10,000; a rolling window's buckets before the range
  are held to the cap instead), `maxSamples` (the raw points and rollup rows the statements take,
  each statement's counted before it runs), `maxGroups`, and a statement
  timeout inside the caller's deadline. Each budget is checked before the
  work it bounds: the groups, the series and the buckets after the
  statements that select the streams and find the range's forms and codes,
  and before any point is reduced. Two checks come later, both before a
  row is written. A recorded-local read's points can name days the range's
  offsets do not, so it counts those buckets against `maxBuckets` after
  evaluating the points. A metric read checks the groups and codes that
  hold a row against `maxGroups` again after assembling the rows. Over
  budget is a `422` naming the limit; a result is never truncated.
- **One response shape**, `AggregateResult` in `internal/substrate`, pinned
  by the wire golden and mirrored in `types.ts`: per-row `start` and `end`
  with offsets, `from`, `sources`, `coverage`, `bounded`, `synthetic` and
  `unavailable`, plus `exact` and the changelog `head`.

The mode is refused beside `q`, `watch`, `count`, `orderBy`, `first`,
`after`, `offset`, `expand` and `withAnnotations`. Its evaluation order is fixed: select
streams, build held intervals, resolve the source policy, clip to the range,
filter intervals by code or threshold, reduce, fill, roll. Functions call it
as `host.records.aggregate`, agents through the `query` tool's `aggregate`
argument (`filter` and `downsample` beside it, the other arms' arguments
refused as the route refuses them), and the CLI as `substratectl aggregate`.

A second route would repeat the records route's filter, row-level security
and allow-list, and split 0079's one read. Client-side reduction pages
millions of rows. A free-form expression cannot be checked against forms and
budgets before it runs.

### Consequences

- Good, because a screen is one request, and the console, a function and an
  agent get the same answer.
- Good, because a wrong reducer or unit is refused before any point is
  reduced, and so is a budget, apart from the two later checks above,
  which still come before a row is written.
- Bad, because the records route's allow-list gains another mode with its
  own refusals.
- Bad, because joins beyond a matched record's range, continuous queries
  and grouping by a property of the `about` record are left out; a function
  does them.
- Bad, because a budget refusal pushes a client to narrower requests instead
  of a degraded answer.

### Confirmation

`TestWireGoldenMatchesStructs` (internal/substrate) holds the request's and
the response's field names and the console's vitest holds `types.ts` to
them. In internal/engine: `TestAggregateRefusesOverBudgetBeforeRunning`,
`TestAggregateNeverMixesRawAndSummariesInABucket`,
and `TestAggregatePriorityResolvesBeforeBucketing` (one total at 5-minute
and at daily buckets). In internal/api, against the hand-written fake:
`TestAggregateIsRefusedBesideListParameters`.

## More information

[The implementation brief](../plans/observations/implementation/README.md)
pins the request and response field names; an implementer may add a field,
never rename one. Version 1's
[section 5.5](../plans/health-data.md) has the reasoning behind each part.
Revisit if a screen needs a reduction the fixed evaluation order cannot
express.
