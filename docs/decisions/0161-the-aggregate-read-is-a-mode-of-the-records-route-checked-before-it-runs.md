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

- **Selector.** Exactly one of `metric` (a full reference; the index is
  read) and `property` (a declared numeric or quantity property on the
  filtered kinds, read from `records` in `numeric`), never a bare word.
- **Four named time modes.** `{"every", "origin"}` is a physical window on
  UTC instants. `{"calendar", "timezone"}` is a calendar window in a named
  zone, 23 or 25 hours across a clock change. `{"calendar"}` with no zone is
  the recorded-local day, where each reading's own offset decides its date.
  `{"records": {"filter", "around"}}` uses each matched record's
  `[at, endsAt)` as a bucket. A reading with no offset is bucketed by a named
  zone or refused, never by UTC or the server's clock.
- **Typed reducers**, checked against the value's form
  ([0156](0156-the-forms-are-gauge-amount-counter-and-summary-and-live-on-the-value.md)),
  each answering with its own unit.
- **`from`**: `raw` (the default), `summaries` or `prefer-raw`. One bucket
  never mixes raw points and vendor summaries. `prefer-raw` uses raw points
  where a bucket holds one and its coverage reaches `minCoverage` (default
  0.9) and the summary otherwise, and each row says which it used.
- **Source policies** (`separate` by default, `priority` with an ordered
  list, `summaries`) resolve per stored interval before bucketing, so a total
  does not change with the bucket width.
- **Budgets.** `maxBuckets` (groups times buckets, default 1,440, cap
  10,000), `maxSamples` and a statement timeout inside the caller's deadline
  are checked before the read runs. Over budget is a `422` naming the limit;
  a result is never truncated.
- **One response shape**, `AggregateResult` in `internal/substrate`, pinned
  by the wire golden and mirrored in `types.ts`: per-row `start` and `end`
  with offsets, `from`, `sources`, `coverage`, `bounded`, `synthetic` and
  `unavailable`, plus `exact` and the changelog `head`.

The mode is refused beside `q`, `watch`, `count`, `orderBy`, `first`,
`after`, `offset` and `expand`. Its evaluation order is fixed: select
streams, build held intervals, resolve the source policy, clip to the range,
filter intervals by code or threshold, reduce, fill, roll. Functions call it
as `host.records.aggregate`, agents through the `query` tool's `aggregate`
argument, and the CLI as `substratectl aggregate`.

A second route would repeat the records route's filter, row-level security
and allow-list, and split 0079's one read. Client-side reduction pages
millions of rows. A free-form expression cannot be checked against forms and
budgets before it runs.

### Consequences

- Good, because a screen is one request, and the console, a function and an
  agent get the same answer.
- Good, because a wrong reducer, unit or budget is refused before any work.
- Bad, because the records route's allow-list gains another mode with its
  own refusals.
- Bad, because joins beyond a matched record's range, moving averages,
  continuous queries and grouping by a property of the `about` record are
  left out; a function does them.
- Bad, because a budget refusal pushes a client to narrower requests instead
  of a degraded answer.

### Confirmation

`TestWireGolden` (internal/substrate) holds the response's field names and
the console's vitest holds `types.ts` to them. Tests the work package 4
implementers will write: `TestAggregateRefusesOverBudgetBeforeRunning`,
`TestAggregateNeverMixesRawAndSummariesInABucket`,
`TestAggregatePriorityResolvesBeforeBucketing` (one total at 5-minute and at
daily buckets) and `TestAggregateIsRefusedBesideListParameters`, with the
mode carried by `internal/api/fake_test.go`.

## More information

[The implementation brief](../plans/observations/implementation/README.md)
pins the request and response field names; an implementer may add a field,
never rename one. Version 1's
[section 5.5](../plans/health-data.md) has the reasoning behind each part.
Revisit if a screen needs a reduction the fixed evaluation order cannot
express.
