# WHOOP: vendor rollups

WHOOP sends scored objects, never raw samples, so everything from WHOOP lands as `summary` observations that keep WHOOP's own cycle identity, plus typed sleep and workout records.

## What arrives, and what the adapter writes

The existing WHOOP provider (`providers.substrate.reamde.dev/whoop`, version 18) mirrors WHOOP's v2 objects whole, in WHOOP's units, and keeps doing so unchanged:

```yaml
kind: providers.substrate.reamde.dev/whoop/recovery
metadata:
  id: "1047392"                     # WHOOP's cycle id, as the mirror keys it today
data:
  properties:
    user: {ref: providers.substrate.reamde.dev/whoop/user/418203}
    cycle: {ref: providers.substrate.reamde.dev/whoop/cycle/1047392}
    sleep: {ref: providers.substrate.reamde.dev/whoop/sleep/e3b0c442-98fc-4c14-9afb-1c1e0a7d4b9a}
    scoreState: scored
    score:
      userCalibrating: false
      recoveryScore: 67
      restingHeartRate: 52
      hrvRmssdMilli: 61.3
      spo2Percentage: 96.5
      skinTempCelsius: 33.8
    createdAt: 2026-10-07T04:12:31Z
    updatedAt: 2026-10-07T04:12:31Z
```

The `healthwhoop` sample holds one function on a record trigger for the five mirror kinds. One recovery row yields five observations with ids composed from WHOOP's own identifiers, never minted (the user's authority is `george.example.com` throughout):

```yaml
kind: george.example.com/observations/observation
metadata:
  id: whoop/george/1047392/recovery   # whoop / the WHOOP account record's id / cycle id / role
data:
  properties:
    metric: {ref: george.example.com/observations/metric/health/whoop_recovery}
    source: {ref: george.example.com/observations/source/whoop-4-0}
    form: summary
    statistic: vendor                 # WHOOP's own method, not a mean or a sum
    period: cycle
    periodId: "1047392"
    value: {value: "67", unit: "%"}
    at: 2026-10-06T22:41:00+03:00     # the cycle's span, read from the cycle row
    endsAt: 2026-10-07T22:38:00+03:00
    utcOffset: 180
    origin: {ref: providers.substrate.reamde.dev/whoop/recovery/1047392}
    revision: 1759810351              # WHOOP's updated_at as seconds; a lower revision is refused
```

| From | Written | Id | Value |
| --- | --- | --- | --- |
| recovery | `health/whoop_recovery` | `whoop/george/1047392/recovery` | `67 %`, `summary` |
| recovery | `health/hrv_rmssd` | `.../1047392/hrv` | `61.3 ms`, `summary` |
| recovery | `health/resting_heart_rate` | `.../1047392/resting_heart_rate` | `52 /min`, `summary` |
| recovery | `health/spo2` | `.../1047392/spo2` | `96.5 %`, `summary` |
| recovery | `health/skin_temperature` | `.../1047392/skin_temperature` | `33.8 Cel`, `summary` |
| cycle | `health/whoop_strain` | `.../1047392/strain` | `14.2`, unit `1`, `summary` |
| cycle | `health/energy_expended` | `.../1047392/energy` | `8125.4 kJ`, `amount`: kilojoules stay kilojoules; a reader asking for `kcal` gets 1942 at read time |
| sleep | `health/sleep` (typed) | `whoop/george/e3b0c442.../sleep` | time in bed, efficiency, respiratory rate, WHOOP's performance scores |
| sleep | `health/sleep_stage`, one coded `amount` per stage total | `.../e3b0c442.../stage/rem` | `rem` 94 min, `deep` 71, `light` 211, `awake` 38 |

When WHOOP rescores a cycle, the mirror row's `updatedAt` rises, the trigger fires again, the same ids are rewritten with the higher revision, and the index follows. When WHOOP deletes a sleep, the adapter finds its outputs by `referencing` the origin and deletes them. Two cycles on one calendar date stay two rows because `periodId` differs.

## What lands in Postgres

The recovery observation is one row in `records` and one row in `observed_points`. No `observed_rollups` row: summaries are never rolled up, because a mean of vendor scores is a mean of means. One `observed_latest` row for the stream.

| Column | `records` row | `observed_points` row |
| --- | --- | --- |
| repository | george.example.com | george.example.com |
| kind | george.example.com/observations/observation | same |
| id / record\_id | whoop/george/1047392/recovery | same |
| at / ts | 2026-10-06 19:41:00+00 | same |
| ends\_at / ts\_end | 2026-10-07 19:38:00+00 | same |
| props | the whole `data.properties` as jsonb, GIN-indexed | not present |
| metric, source, about, channel | inside `props` | `health/whoop_recovery`, `whoop-4-0`, owner, none |
| form, statistic | inside `props` | `summary`, `vendor` |
| unit, value, value\_base | inside `props` as `{"value": "67", "unit": "%"}` | `%`, 67, 0.67 |
| utc\_offset\_min | inside `props` | 180 |
| version / revision | 1 | 1759810351 |

## The calls, and what you build from them

| Screen | Request, in short | Served from | Build with the result |
| --- | --- | --- | --- |
| today's recovery | list read: kind `observation`, `periodId` equals the cycle id | `records` | the recovery card: five values in WHOOP's own units, with the cycle's span |
| 90-day recovery trend | aggregate `from: summaries`, recorded-local day, `last` | summary points | a line chart; add `rolling: {buckets: 7, reducers: [avg]}` for the smoothed line |
| HRV baseline against today | the same with `health/hrv_rmssd`, `unit: ms`, `rolling: {buckets: 30}` | summary points | today's value against its 30-day mean, WHOOP's green and red band |
| strain against recovery | two requests, strain and recovery per day, joined on the row's `start` in the app | summary points | the scatter WHOOP draws; the engine does no joins |
| sleep stages per night | aggregate `health/sleep_stage`, `bucket: {records: sleep}`, `groupBy: [code]`, `sum` | rollups inside, raw at the boundaries | stacked bars per night, each bar opening its sleep record |
| calories per day | aggregate `health/energy_expended`, calendar day, `sum`, `unit: kcal` | rollups | the energy chart; kJ to kcal is exact (4184 J) and happens at read time |
| raw re-sync check | list read on the mirror kind, `updatedAt` greater than a date | `records` | what WHOOP changed since yesterday, before the adapter touched it |

The trend request in full:

```json
{"filter": {"kinds": ["george.example.com/observations/observation"]},
 "aggregate": {
   "metric": {"ref": "george.example.com/observations/metric/health/whoop_recovery"},
   "from": "summaries",
   "range": {"gte": "2026-07-09T00:00:00+03:00", "lt": "2026-10-08T00:00:00+03:00"},
   "bucket": {"calendar": "day"},
   "reducers": ["last"],
   "rolling": {"buckets": 7, "reducers": ["avg"]}}}
```

`from: summaries` is what keeps a WHOOP score from being averaged with somebody's derived daily mean. The list read for today's card:

```http
GET /api/v1/records?filter={"kinds":["george.example.com/observations/observation"],
  "properties":{"periodId":{"eq":"1047392"}}}
```
