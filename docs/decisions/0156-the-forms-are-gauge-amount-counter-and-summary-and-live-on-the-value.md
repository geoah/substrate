---
status: proposed
date: 2026-10-11
decision-makers: George Antoniadis
---

# 0156. The forms are `gauge`, `amount`, `counter` and `summary`, and they live on the value

## Context and problem statement

A number alone does not say how to aggregate it: summing heart rates and
averaging a meter reading are the common mistakes. Version 1 of the plan
named the forms by time (`instant`, `interval`). The review of 2026-10-10
then found that the engine claimed to read nothing from the `metric` record
while `wrapAt`, `circular` and the temperature-difference flag lived there
([the plan, section 13, finding 1](../plans/observations.md)). The form
words are part of the `observed` trait's first contract
([0154](0154-an-observed-value-binds-the-core-observed-trait-which-never-gains-a-property.md)),
so they are fixed once.

## Considered options

- Four forms named for what the number means (`gauge`, `amount`, `counter`,
  `summary`), with every reading rule on the value
- The version 1 names, `instant`, `interval`, `counter` and `summary`
- A fifth `state` form for categorical values
- The form and the reading rules on the `metric` record, read by the engine

## Decision outcome

Chosen: the four forms, on the value, with the reading rules beside them.

| Form | Is | Legal reducers |
| --- | --- | --- |
| `gauge` | a level at an instant, held until its hold ends ([0158](0158-a-gauge-carries-a-hold-a-null-ends-it-and-its-mean-is-time-weighted.md)) | `avg`, `twa`, `min`, `max`, `first`, `last`, `count`, `stddev`, `percentile`, `durationIn`, `circularMean`; coded: `count`, `first`, `last`, `timeInState` |
| `amount` | a quantity accrued over `[at, endsAt)` | `count`, `sum`, `avg`, `min`, `max`, `first`, `last`, `stddev`, `percentile`; coded: `count`, `sum`, `min`, `max`, by code |
| `counter` | a running total ([0157](0157-a-counter-is-stored-as-sent-and-differenced-at-read-under-one-of-three-contracts.md)) | `count`, `first`, `last`, `min`, `max`, `increase`, `rate` |
| `summary` | a statistic over a stated period, with `statistic` and `period` | `count`, `first`, `last`, `avg`, `min`, `max` over one `statistic`, and `sum` over a `sum` or `count` statistic; never pooled with the other three |

A reducer the form does not admit is a `422` naming the form.

A categorical value (a door `open`, a sleep stage `rem`, a mood) is a gauge
with `code` set instead of `value`, or an amount with `code` beside `value`,
so 94 minutes of `rem` carries both and sums by code. It is never a fifth
form and never a number: a boolean is two named codes, never `0` and `1`, so the form
check can refuse a mean of door states. `state` was refused as a form because
the `state` datatype is a state machine the substrate polices, and a door's
code is data it does not police.

A summary a vendor computed by its own method (WHOOP's recovery, a readiness
score) says `statistic: vendor`, not `mean` and not a new word such as
`score`.

`form`, `unit`, `hold`, `resetRule` with `resetAt`, `delta` (a temperature
difference rather than a temperature), `circular` (an angle averaged on the
circle) and `wrapAt` are on the observation and the trace. The `metric`
record keeps names, codes, display and recommended reducers, and the write
rules and the observed index read nothing from it. An edit to a metric
therefore cannot relabel an indexed number, and projecting one record never
reads another. The aggregate read takes the metric's `measures` and `unit`:
the measure it converts every value into and the unit it answers in when the
request names none. An edit to either changes the answers, never a stored
value.

`gauge` and `amount` are the OpenMetrics and Home Assistant words. The form
says what the number means, not when it was taken, and `interval` also
collided with a trace's spacing property.

### Consequences

- Good, because `sum` over a gauge and `avg` over a counter are refused
  before any point is reduced.
- Good, because a derived row is a function of its own record and the
  declaration alone, so a correction touches only its own buckets.
- Bad, because every reading repeats what its metric already says (form,
  unit, hold), and the metric's defaults are advice a writer may ignore.
- Bad, because two writers may record one metric under different forms, and
  a read has to name the one it wants.
- Bad, because `statistic: vendor` says nothing about the method; the
  metric's `algorithm` says that, for readers only.

### Confirmation

In internal/engine: `TestAggregateRefusesOverBudgetBeforeRunning` (`sum`
over a gauge and `increase` over an amount are a `422` naming the form),
`TestAggregateCounterIncreaseAndRateAcrossAReset` (`avg` over a counter is
one too), `TestObservedValueAndCodeAreExclusive`, and
`TestEditingAMetricChangesNoIndexedRow` (rewriting a metric record's form,
unit, hold and measure leaves `observed_points`, `observed_rollups` and
`observed_latest` unchanged, and a reading written after the rewrite
derives the same points as the one written before it).

## More information

[The plan, sections 5.1 and 5.2](../plans/observations.md), and the form
table in [under the hood](../plans/observations/proposal/04-under-the-hood.md).
[The implementation brief](../plans/observations/implementation/README.md)
settled the form names and `statistic: vendor`. The plan's sections 5.3 and
5.5 still place `wrapAt` and `circular` on the metric; its own section 13
moves them, and this record follows section 13.
