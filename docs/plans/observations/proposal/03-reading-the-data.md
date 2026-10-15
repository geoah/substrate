# Reading the data: the API

Every screen is one request to `GET /api/v1/records`: a list read with a filter, or the new `aggregate` mode with a metric, a range, a bucket rule, reducers and a source policy. Every aggregate answer has one shape.

## The response shape

A named struct in `internal/substrate`, mirrored in the console's `types.ts` and pinned by the wire golden:

```json
{"metric": "george.example.com/observations/metric/health/heart_rate",
 "unit": "/min",
 "bucket": {"calendar": "week", "weekStart": "monday", "timezone": "Europe/Athens"},
 "from": "raw",
 "exact": true,
 "rows": [
   {"start": "2026-04-06T00:00:00+03:00", "end": "2026-04-13T00:00:00+03:00",
    "from": "raw", "sources": ["garmin-forerunner-265"], "coverage": "P6DT22H10M",
    "values": [
      {"reducer": {"durationIn": [95, 114]}, "value": "612.4", "unit": "min"},
      {"reducer": {"durationIn": [114, 133]}, "value": "171.0", "unit": "min"}],
    "count": 24810, "bounded": 0, "synthetic": false}],
 "head": {"changelog": 2171234, "generation": 7}}
```

| Field | Why it is there |
| --- | --- |
| `start`, `end` with offsets | a 23-hour day across a clock change is visible, and a recorded-local day can be 20 or 28 hours |
| `from` per row | which of raw points or a vendor summary served the row, so `prefer-raw` is auditable |
| `sources`, `coverage` | which devices fed the row and for how long, so a thin week reads as thin |
| one `unit` per reducer | `count` has none, `durationIn` is a duration, `stddev` of a temperature is a temperature difference |
| `bounded` | lab values with a comparator, counted but not averaged |
| `synthetic` | a bucket `fill` created, never a measurement |
| `exact` | false when a float projection or a rounded output unit was involved |
| `head` | the changelog position the read was served at, for a cache or a diff |

## Every screen as one request

| Screen | Request, in short | Served from | What you do with the result |
| --- | --- | --- | --- |
| WHOOP recovery, today | list read, `periodId` equals the cycle id | `records` | the recovery card in WHOOP's units |
| recovery, HRV, strain trend | `from: summaries`, recorded-local day, `last`, `rolling: 7` | summary points | a line with its smoothed band |
| heart rate zones, six months weekly | five `durationIn`, `maxGap`, source priority | raw points | stacked zone bars; thresholds are the app's |
| time in zone per workout | the same with `bucket: {records: {filter: workouts}}` | raw points | a bar per activity, each opening its workout |
| daily steps, one year | `sum`, `prefer-raw` | rollups, vendor totals on thin days | the steps calendar |
| weight and body fat, weekly | `avg`, `min`, `max`, `count`, `rolling: 4` | rollups | trend line with band |
| biomarker trend | list read ordered by `at`; yearly `avg` with `count` | `records`; rollups | points with ranges and flags |
| resting heart rate per day, all sources | 5th percentile, `sources: separate` | raw points | a per-device comparison |
| sleep stages per night, one month | coded amounts, `bucket: {records: sleep}`, `groupBy: [code]`, `sum` | rollups inside, raw at the boundaries | stacked bars per night |
| average temperature per room per day | `twa`, `min`, `max`, `groupBy: [about]`, `maxGap: PT3H` | rollups | a line per room |
| kWh per day from a meter that resets | `increase`, `unit: kW.h` | rollups plus the step between rows | the energy bar chart, reset-proof |
| hours the front door was open per week | `{timeInState: ["open"]}`, `maxGap: P1D` | rollups | a duration per week |
| the current value of everything | `groupBy: [metric, source, about]`, `last`, no range | `observed_latest` | the dashboard |
| an 800-pixel chart over any range | `downsample={"points": 800, "method": "m4"}` | rollups' first, last, min, max per column | the zoomable line; never fed into rolling windows |
| money per month, tasks closed per week | `property` instead of `metric`: a declared numeric or quantity property on the filtered kinds, aggregated in `numeric` | `records` with typed expression indexes | the same shape for non-observation kinds |

The parts of a request, each with the rule it carries:

- **Selector.** `metric` (a full reference; the index is read) or `property` (a declared property on the filtered kinds), never a bare word.
- **`from`.** `raw` reads gauges, amounts and counters; `summaries` reads `summary` rows whose `statistic` matches; `prefer-raw` uses raw where a bucket has at least `minCoverage` (default 0.9) and the summary otherwise, saying per row which it used. One bucket never mixes the two.
- **Time, four modes.** `{"every": "PT5M"}` is a physical window on UTC instants; `{"calendar": "day", "timezone": "Europe/Athens"}` a calendar window in a zone; `{"calendar": "day"}` with no zone the recorded-local day; `{"records": {filter}}` each matched record's `[at, endsAt)`, with `around` to widen it. `offset` shifts a day's start for 04:00 vendor days; `weekStart` picks the week's first day.
- **Reducers** are structured where they take an argument and checked against the value's form: `sum` over a gauge is a `422` naming the form.
- **Sources.** `separate`, `priority` with an ordered list resolved per stored interval before bucketing, or `summaries` for one vendor's period rows.
- **`groupBy`** over `source`, `about`, `code`, `metric`, `kind`: all in the rollup key, so grouping never forces a raw scan.
- **`fill`** (`null`, `zero`, `locf`) makes buckets dense and marks them `synthetic`; **`rolling`** counts buckets, filled ones included.

Evaluation order, fixed: select streams, build held intervals from every transition, resolve the source policy, clip to the range, filter intervals by code or threshold, reduce, fill, roll.

## Two requests in full

Sleep stages per night for a month. The stage totals are coded amounts, and each night's window is the typed sleep record:

```json
{"filter": {"kinds": ["george.example.com/observations/observation"]},
 "aggregate": {
   "metric": {"ref": "george.example.com/observations/metric/health/sleep_stage"},
   "from": "raw",
   "range": {"gte": "2026-09-01T00:00:00+03:00", "lt": "2026-10-01T00:00:00+03:00"},
   "bucket": {"records": {"filter": {"kinds": ["george.example.com/health/sleep"]}}},
   "groupBy": ["code"],
   "reducers": ["sum"],
   "sources": {"policy": "priority", "order": ["whoop-4-0", "garmin-forerunner-265"]},
   "unit": "min"}}
```

Thirty rows per code (`rem`, `deep`, `light`, `awake`), each carrying the sleep record's reference, so a tap on a bar opens the night.

Average temperature per room per day for a month, the first `home` request. The sensors report on change, so the mean is time-weighted and a gap longer than three hours stops the hold:

```json
{"filter": {"kinds": ["george.example.com/observations/trace"]},
 "aggregate": {
   "metric": {"ref": "george.example.com/observations/metric/air_temperature"},
   "range": {"gte": "2026-09-01T00:00:00+03:00", "lt": "2026-10-01T00:00:00+03:00"},
   "bucket": {"calendar": "day", "timezone": "Europe/Athens"},
   "groupBy": ["about"],
   "reducers": ["twa", "min", "max"],
   "maxGap": "PT3H",
   "unit": "Cel"}}
```

Thirty rows per room, `about` naming the `home/area` record on each.

## Writing, budgets, functions and agents

**Writing** is the ordinary record write: `PUT /api/v1/<authority>/observations/observation/<id>` for a chosen id, `POST /api/v1/records` with `kind` in the body for a server-assigned one, `substratectl apply -f` from a file. A trace over its declared sample count or byte size is refused, so a day of 1 Hz data is never one record. `stats` on a trace is managed: a supplied one is ignored and recomputed.

**Budgets** bound every aggregate before it runs: `maxBuckets` (default 1,440, cap 10,000) counts groups times buckets; `maxSamples` bounds the rows the plan may read; a statement timeout sits inside the caller's remaining deadline; `groupBy` cardinality and matched records are capped. A request over budget is refused naming the limit; a result is never silently truncated.

**Functions** call `host.records.aggregate(...)` with the same body and pay against their existing read budget. **Agents** get it through the `query` tool's `aggregate` argument, so "how did I sleep this month" is one call returning thirty rows of numbers.

**Left out on purpose:** arbitrary expressions, joins beyond "values inside a matched record's window", exponential moving averages (a function computes one from aligned rows), continuous queries (a scheduled function), grouping by a property of the `about` record (resolve the rooms first, then group by `about`).
