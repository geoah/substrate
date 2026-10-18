# Weight and body fat: a scale, or your own hand

A weighing is the simplest case: one `observation` per measure, sparse enough that every value is its own record, addressable and editable, and the one case a user writes with no provider at all.

## What arrives, and what is written

A Withings scale reports one measure group per step on the scale: weight, fat ratio, fat mass, muscle mass, hydration and bone mass, each as an integer with a power-of-ten exponent. A Garmin Index or an Eufy scale reaches Apple Health or Health Connect as separate samples sharing a timestamp. Decoding the exponent is the adapter's job; the decoded value keeps exact digits.

One observation per measure, six ids composed from the measure group id, all with the same `at`:

```yaml
kind: george.example.com/observations/observation
metadata:
  id: withings/george/5f3a91/weight     # provider / account record id / measure group id / role
data:
  properties:
    metric: {ref: george.example.com/observations/metric/health/body_mass}
    source: {ref: george.example.com/observations/source/withings-body-plus}
    form: gauge
    value: {value: "72.40", unit: kg}
    at: 2026-10-07T06:41:12+03:00
    utcOffset: 180
    origin: {ref: providers.substrate.reamde.dev/withings/measuregrp/5f3a91}
    revision: 1
```

| Id | Metric | Value |
| --- | --- | --- |
| `withings/george/5f3a91/weight` | `health/body_mass` | `72.40 kg` |
| `.../body_fat` | `health/body_fat_fraction` | `18.6 %` |
| `.../fat_mass` | `health/fat_mass` | `13.47 kg` |
| `.../muscle_mass` | `health/muscle_mass` | `55.9 kg` |
| `.../body_water` | `health/body_water_fraction` | `58.1 %` |
| `.../bone_mass` | `health/bone_mass` | `3.1 kg` |

A child's weight would add `about: {ref: george.example.com/people/person/mia}`; absent `about` means the owner. A hand-entered weighing is the same kind with no `origin`, a `source` whose class is `manual`, and an id the user chooses, or none on `POST /api/v1/records`:

```yaml
kind: george.example.com/observations/observation
metadata:
  id: manual/2026-10-09-weight
data:
  properties:
    metric: {ref: george.example.com/observations/metric/health/body_mass}
    source: {ref: george.example.com/observations/source/manual}
    form: gauge
    value: {value: "72.1", unit: kg}
    at: 2026-10-09T07:02:00+03:00
    utcOffset: 180
```

## What lands in Postgres

Six `records` rows and six `observed_points` rows per weighing. Rollup rows are sparse: one per stream per 15-minute bucket that holds a point, so a daily weigher adds six rollup rows a day. `observed_latest` holds six rows, one per stream, and that is the "current body" card.

| Table | Rows per weighing | What a row holds |
| --- | --- | --- |
| `records` | 6 | `props` with the quantity as `{"value": "72.40", "unit": "kg"}`, `at` 2026-10-07 03:41:12+00 |
| `observed_points` | 6 | metric `health/body_mass`, source `withings-body-plus`, form `gauge`, unit `kg`, value 72.4, value\_base 72.4 |
| `observed_rollups` | 6 | bucket 03:30 to 03:45 UTC, `n` 1, `min` = `max` = `mean` = 72.4, offset 180 |
| `observed_latest` | 6 updated | the newest weight, body fat and the rest per stream, with unit and form |

## The calls, and what you build from them

| Screen | Request, in short | Served from | Build with the result |
| --- | --- | --- | --- |
| weight trend, weekly for a year | `avg`, `min`, `max`, `count`, `rolling: 4`, source priority, `unit: kg` | rollups | the trend line with its weekly band and a 4-week smoothed line |
| body fat trend | the same with `health/body_fat_fraction`, `unit: %` | rollups | a second axis on the same chart |
| the same in pounds | `unit: "[lb_av]"` | rollups | converted and rounded on output; the response says it rounded; stored values never change |
| every weighing under 155 lb | list read, `value` less than `{value: "155", unit: "[lb_av]"}` | `records` | a filtered list; the operand is in pounds, the rows in kilograms |
| the current body card | `groupBy: [metric, source]`, `last`, metrics filtered to the six | `observed_latest` | six numbers with their dates and units |
| lean mass over time | two requests, weight and fat mass, subtracted per week in the app | rollups | a derived line; a derived metric is a function's job if it should persist |

The weekly trend in full. Every reducer here merges exactly from rollups, so no raw point is read:

```json
{"aggregate": {
   "metric": {"ref": "george.example.com/observations/metric/health/body_mass"},
   "range": {"gte": "2025-10-06T00:00:00+03:00", "lt": "2026-10-05T00:00:00+03:00"},
   "bucket": {"calendar": "week", "weekStart": "monday", "timezone": "Europe/Athens"},
   "reducers": ["avg", "min", "max", "count"],
   "rolling": {"buckets": 4, "reducers": ["avg"]},
   "sources": {"policy": "priority", "order": ["withings-body-plus", "manual"]},
   "unit": "kg"}}
```

The filter across units:

```http
GET /api/v1/records?filter={"kinds":["george.example.com/observations/observation"],
  "properties":{"metric":{"eq":"george.example.com/observations/metric/health/body_mass"},
                "value":{"lt":{"value":"155","unit":"[lb_av]"}}}}&orderBy=at
```

The engine converts the stored kilograms to the operand's pounds with exact rational arithmetic and compares. A stored value in a different quantity kind never matches, the way money never matches across currencies.

The current body card:

```json
{"filter": {"properties": {"metric": {"in": [
     "george.example.com/observations/metric/health/body_mass",
     "george.example.com/observations/metric/health/body_fat_fraction",
     "george.example.com/observations/metric/health/muscle_mass"]}}},
 "aggregate": {"groupBy": ["metric", "source"], "reducers": ["last"]}}
```
