# The unified view

Unification happens through the `metric`, never through merging. Every writer points at the same metric record, every source keeps its own rows, and each request says how to treat the sources, so nothing has to be unmerged when the user changes their mind.

## The metric and the source

The metric is the catalogue entry. The `health` sample ships one record per metric with the codes other systems use for it, so an adapter maps by code and an app finds the metric by the name it knows:

```yaml
kind: george.example.com/observations/metric
metadata:
  id: health/heart_rate
data:
  properties:
    name: Heart rate
    codes:
      - {system: http://loinc.org, code: "8867-4", display: "Heart rate"}
      - {system: healthkit, code: HKQuantityTypeIdentifierHeartRate}
      - {system: healthconnect, code: HeartRateRecord}
    dimension: rate
    unit: /min
    form: gauge
    hold: PT4M
    reducers: [avg, twa, min, max, durationIn]
    displayPrecision: 0
```

The engine reads nothing from it: the form, unit and hold on every value are what the index uses, so editing a metric can never relabel history. A user adds a metric by writing a record.

The source is the device; the origin is the delivery:

```yaml
kind: george.example.com/observations/source
metadata:
  id: garmin-forerunner-265
data:
  properties:
    name: Garmin Forerunner 265
    class: watch
    manufacturer: Garmin
    hardwareModel: Forerunner 265
    account: {ref: providers.substrate.reamde.dev/applehealth/account/iphone}
    priorities: {health/heart_rate: 2, health/steps: 1}   # the user's order per metric
```

`origin` on each observation points at the mirror row that delivered it. One watch reaching the substrate through both Apple Health and Health Connect is one source only when the upstream identity is exactly the same; otherwise it is two sources, and the source policy picks one at read time. Nothing deduplicates on a similar timestamp.

## Who writes what, and how an app reads across them

| Metric | Writers | Forms | Read across sources | Build with it |
| --- | --- | --- | --- | --- |
| `health/heart_rate` | Garmin trace, Apple Watch trace, chest strap trace in workouts, WHOOP resting heart rate | `gauge` traces; WHOOP as `summary` | `sources.policy: priority` resolves per stored interval before bucketing; WHOOP's number is reachable only with `from: summaries` | one heart rate chart from the best device per interval |
| `health/steps` | Garmin trace, iPhone trace, Garmin's daily total | `amount` traces; the total as `summary` | `priority` avoids the double count; `separate` shows both; `prefer-raw` falls back to the total on thin days | the steps calendar that never double counts |
| `health/body_mass` | Withings, Garmin Index, manual | `gauge` observations | `priority` or `separate`; a manual entry can outrank a scale for one day by `revision` | one weight line with the scale's and your entries told apart |
| `health/hrv_rmssd` | WHOOP per cycle, Oura per night, Apple Watch spot readings | `summary` from WHOOP and Oura, `gauge` from Apple | summaries and gauges never share a bucket | one vendor's line or Apple's points, never a blend of methods |
| sleep stages | WHOOP, Garmin, Oura as typed `health/sleep` records plus coded `amount` observations per stage | `amount` with `code` | `sum` by `code` with `groupBy: ["code"]` and a `priority` list; one source per night | stacked bars per night from the device you trust most |
| `health/ldl_cholesterol` | two labs in two unit systems | `gauge` lab results | never pooled across quantity kinds; a derived molar observation bridges them with `derivedFrom` | one trend across labs with each converted point stamped |

## One request, two policies

Resting heart rate per day for a month, with sources kept apart:

```json
{"aggregate": {
   "metric": {"ref": "george.example.com/observations/metric/health/heart_rate"},
   "from": "raw",
   "range": {"gte": "2026-09-01T00:00:00+03:00", "lt": "2026-10-01T00:00:00+03:00"},
   "bucket": {"calendar": "day", "timezone": "Europe/Athens"},
   "reducers": [{"percentile": 0.05}, "min"],
   "sources": {"policy": "separate"},
   "unit": "/min"}}
```

| Policy | Rows back | What the app does with them |
| --- | --- | --- |
| `separate` | one per day per source, each naming its source, coverage and point count | a per-device comparison: is the watch reading higher than the strap? |
| `priority` with `order: ["chest-strap-h10", "garmin-forerunner-265", "apple-watch-s10"]` | one per day, built from the strap where it recorded and the watch elsewhere, both listed on the rows they fed | the single trend a user expects |
| `summaries` | one per day from one vendor's `summary` rows | WHOOP's or Oura's own number, unchanged |

The fifth percentile needs raw points, so this request scans `observed_points`; swap it for `avg` and it reads rollups.

The current state of everything is one row per stream from `observed_latest`, grouped by metric and source. It is the overview card every health app opens with and what a home app calls its dashboard:

```json
{"filter": {"implements": "substrate.reamde.dev/core/observed"},
 "aggregate": {"groupBy": ["metric", "source", "about"], "reducers": ["last"]}}
```
