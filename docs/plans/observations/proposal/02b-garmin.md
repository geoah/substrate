# Garmin: dense device samples

Garmin is the opposite of WHOOP: thousands of samples a day, packed by the adapter into one `trace` record per metric, source and hour. Its data arrives through the Apple Health export or a Health Connect provider, attributed to the watch.

## What arrives, and what the adapter writes

From an Apple Health export, every heart rate reading is one row: a timestamp, a value in `count/min`, and the source name `Garmin Connect`. At rest Garmin writes a reading every one to two minutes; during a run, every second. Steps arrive as chunks with a start, an end and a count.

The adapter writes one trace per hour for heart rate. The id includes the source segment because this provider delivers more than one device:

```yaml
kind: george.example.com/observations/trace
metadata:
  id: applehealth/iphone/garmin-forerunner-265/heart_rate/2026-10-07T06
data:
  properties:
    metric: {ref: george.example.com/observations/metric/health/heart_rate}
    source: {ref: george.example.com/observations/source/garmin-forerunner-265}
    form: gauge
    unit: /min
    hold: PT4M                       # twice the resting cadence; a gap longer than this is a gap
    at: 2026-10-07T06:00:00Z
    endsAt: 2026-10-07T07:00:00Z
    utcOffsetStart: 180
    utcOffsetEnd: 180
    offsets: [0, 118000, 241000, 362000, 1800000, 1801000, 1802000]   # ms from `at`; irregular
    values: ["58", "57", "59", "61", "112", "118", "124"]           # exact digits; a run starts at 06:30
    origins:
      - {ref: providers.substrate.reamde.dev/applehealth/export/2026-10-08}
    revision: 2
    stats:                           # managed: the engine computes it on write
      count: 1812
      min: "55"
      max: "171"
      mean: 131.6
      m2: 2203418.2
      first: {at: 2026-10-07T06:00:00Z, value: "58"}
      last: {at: 2026-10-07T06:59:58Z, value: "96"}
      coverage: PT59M58S
```

| Garmin data | Written as | Notes |
| --- | --- | --- |
| heart rate | `trace`, `gauge`, one per hour | irregular `offsets`; `hold: PT4M` |
| steps | `trace`, `amount`, one per hour | each value spans from its offset to the next; a `null` is a gap |
| a 1 Hz power or cadence stream | `trace` with `every: PT1S` | regular spacing needs no offsets |
| Garmin's daily step total | one `summary` observation, `period: day` | reachable with `from: summaries` |
| sleep with stage segments | a typed `health/sleep` record | stages as coded amounts beside it |
| a workout | a typed `health/workout` record with its FIT file as a blob | laps and sets as child records |

A point inside a trace is not addressable by a filter. That is the trade the packing makes, and it is why a weighing or a lab value is an `observation`, not a trace.

## What lands in Postgres

One hour of heart rate is one `records` row (about 8 KB for 5-second data, under 2 KB at rest), one `observed_points` row per sample, four `observed_rollups` rows (one per 15-minute bucket for this stream) and one `observed_latest` row. A late export that rewrites the hour replaces all of them in one transaction.

| Table | Rows per hour of heart rate | Key |
| --- | --- | --- |
| `records` | 1 | (repository, kind, id) |
| `observed_points` | 30 at rest, 3,600 in a run | (repository, kind, record\_id, ts) |
| `observed_rollups` | 4 | (repository, kind, metric, source, about, channel, form, code, utc\_offset\_min, bucket15) |
| `observed_latest` | 1, updated | (repository, kind, metric, source, about, channel) |

One rollup row for the 06:30 bucket, the start of the run: `n` 900, `min` 98, `max` 171, `mean` 141.2, `m2` for the variance, `first_ts` 06:30:00 with 112, `last_ts` 06:44:59 with 166, `covered_ms` 899000, `max_gap_ms` 1000, `held_ms` 900000, `twa_sum` for the time-weighted mean.

## The calls, and what you build from them

| Screen | Request, in short | Served from | Build with the result |
| --- | --- | --- | --- |
| heart rate zones, six months weekly | five `durationIn` reducers, `maxGap`, source priority, `unit: min` | raw points (a rollup knows the mean, not time above 152) | the stacked zone chart; the five durations sum to the week's covered time |
| time in zone per workout | the same with `bucket: {records: {filter: workouts}}` | raw points | Garmin's per-activity zone bar; each row carries the workout's reference |
| daily steps for a year | `sum`, `prefer-raw`, recorded-local day, `unit: {steps}` | rollups; Garmin's total on thin days | the steps calendar; `from` on each row says which it used |
| resting heart rate per night | 5th percentile over the sleep window with `bucket: {records: sleep}` | raw points | a resting trend that ignores daytime |
| an 800-pixel chart of any range | `downsample={"points": 800, "method": "m4"}` | rollups' first, last, min, max per column | the zoomable line; never fed into rolling windows |
| this week against last | `every: P1W` with `origin` on a Monday, `avg`, `max` | rollups | two rows to compare |

The zones request in full. For this user it reads about 600 thousand points (estimate: two-minute readings at rest plus five hours a week of one-second workout data), inside the default budget:

```json
{"filter": {"kinds": ["george.example.com/observations/trace"]},
 "aggregate": {
   "metric": {"ref": "george.example.com/observations/metric/health/heart_rate"},
   "from": "raw",
   "range": {"gte": "2026-04-06T00:00:00+03:00", "lt": "2026-10-05T00:00:00+03:00"},
   "bucket": {"calendar": "week", "weekStart": "monday", "timezone": "Europe/Athens"},
   "reducers": [
     {"durationIn": [95, 114]}, {"durationIn": [114, 133]}, {"durationIn": [133, 152]},
     {"durationIn": [152, 171]}, {"durationIn": [171, 220]}],
   "maxGap": "PT5M",
   "sources": {"policy": "priority", "order": ["garmin-forerunner-265", "apple-watch-s10"]},
   "unit": "min",
   "budget": {"maxSamples": 5000000}}}
```

The response has 26 rows, each with five durations in minutes, the sources that contributed, the covered time and `from: raw`. The zone boundaries are the app's (here from a maximum of 190); a change of thresholds is a new request, never a rewrite of data.

Daily steps for a year, served from rollups because a sum of amounts merges exactly:

```json
{"aggregate": {
   "metric": {"ref": "george.example.com/observations/metric/health/steps"},
   "from": "prefer-raw",
   "range": {"gte": "2025-10-05T00:00:00+03:00", "lt": "2026-10-05T00:00:00+03:00"},
   "bucket": {"calendar": "day"},
   "reducers": ["sum"],
   "unit": "{steps}"}}
```

`prefer-raw` sums the chunks where a day has at least 90 percent coverage and falls back to Garmin's daily `summary` otherwise. `{"calendar": "day"}` with no zone is the recorded-local day: each sample's own offset decides its date, so a travel day is 20 or 28 hours long and is reported as such.
