# Building health apps

This page is for someone building an app like WHOOP, Oura, Apple Health or
Garmin Connect on a substrate. Each section is one screen: the requests that
fill it and the answer each one gave, abbreviated. The requests read the
records the `observations` and `health` samples declare and the provider
adapters write ([observations](observations.md)), through the records route's
[aggregate read](api.md#the-aggregate-read) and its list read. Where a screen
still needs work in the app or a change in the substrate, the section says
so and links the requirement it falls under in
[what health apps need](plans/observations/requirements.md).

## The data behind the answers

Every answer below came from a demo repository, `ada.example.com`, loaded
with `synth` ([synthetic provider data](synthetic-data.md)): 90 days, from
2026-07-14 to 2026-10-11, of one invented person, Ada. She lives in Athens
(UTC+3), has a cold from 2026-08-26 to 2026-08-29, and spends 2026-09-20 to
2026-09-24 in London (UTC+1). Two providers deliver her data through their
adapters, WHOOP and Apple Health, and two labs report three blood panels.
The answers were read on build `fab7066b` at changelog head 72469, with every
adapter's trigger at lag 0. The sources the answers name, by the bare id a
filter and `sources.order` take:

| Source id | Is |
| --- | --- |
| `whoop/e2e` | the WHOOP band: nightly and daily summaries, sleeps and workouts, no sample series |
| `applehealth/iphone/apple-watch`, `applehealth/iphone/iphone` | the Apple Watch and the phone, through Apple Health |
| `applehealth/iphone/health` | series Apple Health merged from several devices |
| `applehealth/iphone/withings` | a Withings scale and cuff, through Apple Health |
| `applehealth/iphone/contour` | a glucose meter, through Apple Health |
| `manual` | values Ada entered by hand: a hotel scale's weighing in London |
| `bioiatriki`, `harborpoint` | an Athens lab (SI units) and a US lab (mg/dL) |

A source's record path is `ada.example.com/observations/source/<id>`, and a
metric's is `ada.example.com/observations/metric/health/<name>`. The answers
below shorten both to the id after `source/` or `metric/`, and cut keys a
screen does not use; `...` marks a cut. A record's reference property keeps
its wire shape, `{"ref": "<path>"}`, with the path cut to
`.../source/<id>`.

## Sending a request

`$SUBSTRATE_SERVER` is the substrate's URL and `$SUBSTRATE_TOKEN` a token
for the repository: `substratectl login` mints one, `substratectl token
create` mints one for an app ([substratectl](substratectl.md#registering)),
and `POST /login` is the same over HTTP ([logging in](auth.md#logging-in)).
A token has full access to its repository: it reads and writes every record
in it ([tokens](auth.md#tokens)).

Each recipe shows one JSON document whose top-level keys (`filter`,
`aggregate`, `downsample`) are query parameters of `GET /api/v1/records`.
The same document is the body `substratectl aggregate --body` takes:

```bash
curl -sG "$SUBSTRATE_SERVER/api/v1/records" \
  -H "Authorization: Bearer $SUBSTRATE_TOKEN" \
  --data-urlencode 'aggregate={"metric": {"ref": "ada.example.com/observations/metric/health/steps"}, ...}'

substratectl aggregate --body today.json -o json
```

A function's `host.records.aggregate` takes the document with one addition,
`filter.kinds`, because the function's reads allowlist refuses a body that
names no kind ([the SDK](functions.md#the-sdk)). A metric read without it
reads every kind that binds `observed`, here
`ada.example.com/observations/observation`,
`ada.example.com/observations/trace` and `ada.example.com/health/labresult`,
so a function names the ones its metric is written in.

A list read is shown as the request line itself, its `filter`, `orderBy`
and `first` query parameters unencoded.

## Today

A day screen shows the day's totals, the vendor scores for the day, last
night's sleep and the newest body readings. Two reads fill it. The day and
its night are one read: `place: "end"` puts each value with a span on the
day its span ends, so last night counts on the morning it ended, and a day's
totals, whose spans end inside the day, stay on their own day. WHOOP writes
its nightly summaries over the night, recovery included, so one placement
serves them all.

The day and the night, on the local day Ada lived it (`calendar: "day"` with
no `timezone` is the recorded-local calendar), each metric from the first
source in `order` that holds it:

```json
{"aggregate": {
   "metrics": [
     {"ref": "ada.example.com/observations/metric/health/steps", "reducers": ["sum"], "from": "prefer-raw"},
     {"ref": "ada.example.com/observations/metric/health/active_energy", "reducers": ["sum"], "unit": "kcal", "from": "prefer-raw"},
     {"ref": "ada.example.com/observations/metric/health/resting_heart_rate", "from": "summaries", "statistic": "vendor"},
     {"ref": "ada.example.com/observations/metric/health/whoop_recovery", "from": "summaries", "statistic": "vendor"},
     {"ref": "ada.example.com/observations/metric/health/sleep_stage", "reducers": ["sum"], "unit": "h"},
     {"ref": "ada.example.com/observations/metric/health/hrv_rmssd", "from": "summaries", "statistic": "vendor"}],
   "range": {"gte": "2026-10-11T00:00:00+03:00", "lt": "2026-10-12T00:00:00+03:00"},
   "bucket": {"calendar": "day", "place": "end"},
   "groupBy": ["code"],
   "sources": {"policy": "priority",
               "order": ["whoop/e2e", "applehealth/iphone/apple-watch", "applehealth/iphone/iphone",
                         "applehealth/iphone/health"]},
   "reducers": ["last"]}}
```

```text
{"units": {"health/active_energy": "kcal", "health/hrv_rmssd": "ms", "health/resting_heart_rate": "/min",
           "health/sleep_stage": "h", "health/steps": "{steps}", "health/whoop_recovery": "%"},
 "bucket": {"calendar": "day", "place": "end"}, "exact": false,
 "rows": [{"start": "2026-10-11T00:00:00+03:00", "end": "2026-10-12T00:00:00+03:00",
           "group": {"metric": "health/active_energy"}, "from": "raw", "sources": ["applehealth/iphone/apple-watch"],
           "values": [{"reducer": "sum", "value": "1955.6507910472", "unit": "kcal"}], "count": 790, ...},
          {"start": "2026-10-11T00:00:00+03:00", ...,
           "group": {"metric": "health/hrv_rmssd"}, "from": "summaries", "sources": ["whoop/e2e"],
           "values": [{"reducer": "last", "value": "61.180447", "unit": "ms"}], "count": 1, ...},
          {"start": "2026-10-11T00:00:00+03:00", ...,
           "group": {"metric": "health/resting_heart_rate"}, "from": "summaries", "sources": ["whoop/e2e"],
           "values": [{"reducer": "last", "value": "50", "unit": "/min"}], "count": 1, ...},
          {..., "group": {"metric": "health/sleep_stage", "code": "awake"}, "sources": ["whoop/e2e"],
           "values": [{"reducer": "sum", "value": "0.1578475", "unit": "h"}], "count": 1, ...},
          {..., "group": {"metric": "health/sleep_stage", "code": "deep"},
           "values": [{"reducer": "sum", "value": "1.74658111111111", "unit": "h"}], "count": 1, ...},
          {..., "group": {"metric": "health/sleep_stage", "code": "light"},
           "values": [{"reducer": "sum", "value": "4.64007111111111", "unit": "h"}], "count": 1, ...},
          {..., "group": {"metric": "health/sleep_stage", "code": "rem"},
           "values": [{"reducer": "sum", "value": "1.69527777777778", "unit": "h"}], "count": 1, ...},
          {"start": "2026-10-11T00:00:00+03:00", ...,
           "group": {"metric": "health/steps"}, "from": "raw",
           "sources": ["applehealth/iphone/apple-watch", "applehealth/iphone/iphone"],
           "values": [{"reducer": "sum", "value": "22999.7547924254", "unit": "{steps}"}], "count": 158, ...},
          {"start": "2026-10-11T00:00:00+03:00", ...,
           "group": {"metric": "health/whoop_recovery"}, "from": "summaries", "sources": ["whoop/e2e"],
           "values": [{"reducer": "last", "value": "67", "unit": "%"}], "count": 1, ...}], ...}
```

WHOOP writes no steps and no active energy, so those rows fall to the Apple
sources below it. The watch's steps rank above the phone's: where both
counted the same minutes the watch's count is kept, and the phone's fills
the rest of the day. Apple Health's step rows carry fractions, so the day's
sum does too; the app rounds it.

The WHOOP night ranks first, so the Apple Watch's stage totals for the same
night are dropped and every stage row names WHOOP. Both adapters write a
night's stages as one total per stage, `count` 1 each. WHOOP's resting heart
rate is a summary of the night, which ends on the morning of the 11th; Apple
Health's, a summary of the whole day, overlaps it and is dropped.

The tiles: the latest read (`"latest": true`, no `range` or `bucket`)
answers the newest value of each metric from any source, with its instant:

```json
{"filter": {"properties": {"metric": {"in": ["health/body_mass", "health/body_fat_fraction", "health/vo2_max"]}}},
 "aggregate": {"latest": true, "groupBy": ["metric"]}}
```

```text
{"rows": [{"start": "2026-10-10T08:25:04+03:00", "group": {"metric": "health/body_fat_fraction"},
           "sources": ["applehealth/iphone/withings"],
           "values": [{"reducer": "last", "value": "26", "unit": "%"}], "count": 49, ...},
          {"start": "2026-10-10T08:25:04+03:00", "group": {"metric": "health/body_mass"},
           "sources": ["applehealth/iphone/withings"],
           "values": [{"reducer": "last", "value": "71.9", "unit": "kg"}], "count": 50, ...},
          {"start": "2026-10-11T10:24:00+03:00", "group": {"metric": "health/vo2_max"},
           "sources": ["applehealth/iphone/apple-watch"],
           "values": [{"reducer": "last", "value": "48.12", "unit": "mL/(kg.min)"}], "count": 36, ...}], ...}
```

A row's `start` is the value's instant at its own offset, and `count` is
every point the group holds (body mass counts the 49 weigh-ins and the one
Ada typed in), so a tile reads "71.9 kg, 10 October 08:25, Withings". The
latest read takes no `sources` policy: the newest value wins whichever path
delivered it.

## Calories burned across providers

A day's total energy is one metric for some providers and two for others.
WHOOP writes the total as `health/energy_expended`, in kJ. Apple Health
writes `health/active_energy` and `health/basal_energy`, and no total. One
read of the three metrics, grouped by source, answers every source's
numbers:

```json
{"aggregate": {
   "metrics": [
     {"ref": "ada.example.com/observations/metric/health/energy_expended", "from": "prefer-raw", "unit": "kcal"},
     {"ref": "ada.example.com/observations/metric/health/active_energy", "from": "prefer-raw"},
     {"ref": "ada.example.com/observations/metric/health/basal_energy", "from": "prefer-raw"}],
   "range": {"gte": "2026-10-10T00:00:00+03:00", "lt": "2026-10-11T00:00:00+03:00"},
   "bucket": {"calendar": "day"},
   "groupBy": ["source"],
   "reducers": ["sum"]}}
```

```text
{"units": {"health/active_energy": "kcal", "health/basal_energy": "kcal", "health/energy_expended": "kcal"},
 "rows": [{..., "group": {"metric": "health/active_energy", "source": "applehealth/iphone/apple-watch"},
           "values": [{"reducer": "sum", "value": "597.972379825884", "unit": "kcal"}], "count": 830, ...},
          {..., "group": {"metric": "health/basal_energy", "source": "applehealth/iphone/apple-watch"},
           "values": [{"reducer": "sum", "value": "1420.20805254493", "unit": "kcal"}], "count": 1440, ...}], ...}
```

The app takes a source's `energy_expended` where it has one and adds its
`active_energy` and `basal_energy` otherwise: 2,018.2 kcal from the Apple
Watch. No read adds two metrics.

WHOOP answers no row that day. Its total covers a cycle, from one sleep
onset to the next, and the read counts it whole on the date the cycle
starts. Ada fell asleep at 23:38 on 9 October and at 00:13 on 11 October,
so no cycle started on the 10th, and the cycle that holds most of it counts
on the 9th (1,959.2 kcal). A cycle has no date of its own to bucket on
([R-TIME-4](plans/observations/requirements.md#apps-time-and-calendars)).

## Trends with a baseline

A trend screen draws a nightly value against the person's own normal: a
band of the mean plus or minus one standard deviation over the 30 days
before, and "calibrating" until enough days are in. `rolling` with
`through: "previous"` leaves the day out of its own baseline, and each
rolling value's `n` counts the days its window held. The read below is the
daily resting heart rate, one source's value per day:

```json
{"aggregate": {
   "metric": {"ref": "ada.example.com/observations/metric/health/resting_heart_rate"},
   "from": "summaries",
   "range": {"gte": "2026-09-12T00:00:00+03:00", "lt": "2026-10-12T00:00:00+03:00"},
   "bucket": {"calendar": "day"},
   "sources": {"policy": "priority", "order": ["applehealth/iphone/health", "whoop/e2e"]},
   "reducers": ["last"],
   "rolling": {"buckets": 30, "reducers": ["avg", "stddev"], "through": "previous"}}}
```

```text
{"metric": "health/resting_heart_rate", "unit": "/min", "from": "summaries",
 "rows": [{"start": "2026-09-12T00:00:00+03:00", "end": "2026-09-13T00:00:00+03:00", "sources": ["applehealth/iphone/health"],
           "values": [{"reducer": "last", "value": "52", "unit": "/min"},
                      {"reducer": "avg", "of": "last", "rolling": 30, "n": 30, "value": "53.4", "unit": "/min"},
                      {"reducer": "stddev", "of": "last", "rolling": 30, "n": 30, "value": "3.30252428706689", "unit": "/min"}], ...},
          ...
          {"start": "2026-09-20T00:00:00+01:00", "end": "2026-09-21T00:00:00+01:00", ...,
           "values": [{"reducer": "last", "value": "51", ...},
                      {"reducer": "avg", "of": "last", "rolling": 30, "n": 30, "value": "53.0666666666667", ...},
                      {"reducer": "stddev", "of": "last", "rolling": 30, "n": 30, "value": "3.41499959329752", ...}], ...},
          ...
          {"start": "2026-10-11T00:00:00+03:00", "end": "2026-10-12T00:00:00+03:00", ...,
           "values": [{"reducer": "last", "value": "50", ...},
                      {"reducer": "avg", "of": "last", "rolling": 30, "n": 30, "value": "50.8333333333333", ...},
                      {"reducer": "stddev", "of": "last", "rolling": 30, "n": 30, "value": "2.23730989260664", ...}], ...}]}
```

On 2026-10-11 the band is 50.8 plus or minus 2.2, and the day's 50 sits
inside it. The London days are labelled at `+01:00`, each on the day Ada
lived it.

The read keeps the default `place`, `start`, because Apple Health's resting
heart rate is a summary of a calendar day, which starts at its own local
midnight. WHOOP's value is a summary of a night, which starts the evening
before: a read of WHOOP alone sends `"place": "end"`, so each night lands on
the day it ends ([the four time modes](api.md#the-four-time-modes)). In this
read `priority` drops every WHOOP night, because Apple Health's days overlap
them all.

The window reaches before the range, so the first day's baseline is already
30 days deep. A short window against the long one (a 7-day mean against the
30-day band) is a second read with `"buckets": 7`: `rolling` takes one
window length per read. Taking the natural logarithm of each value before
the mean, as sport science does with RMSSD, is the app's.

## A night

A night screen shows the stage totals and what the heart and lungs did
during the night. Every device that recorded the night wrote a
`health/sleep` record of its own:

```http
GET /api/v1/records?filter={"kinds": ["ada.example.com/health/sleep"],
                            "properties": {"endsAt": {"gte": "2026-09-22T00:00:00+01:00", "lt": "2026-09-22T14:00:00+01:00"}}}
                   &orderBy=source
```

```text
{"records": [
   {"id": "applehealth/iphone/apple-watch/sleep/2026-09-21T22:52:00Z",
    "properties": {"source": {"ref": ".../source/applehealth/iphone/apple-watch"}, "at": "2026-09-21T22:39:00Z", "endsAt": "2026-09-22T06:24:00Z",
                   "utcOffsetStart": 60, "utcOffsetEnd": 60, "timeInBed": "PT7H45M", "timeAsleep": "PT7H28M8S",
                   "deep": "PT1H31M37S", "rem": "PT1H49M", "light": "PT4H7M31S", "awake": "PT16M52S",
                   "efficiency": {"value": "96.37", "unit": "%"}, "latency": "PT13M", "isNap": false, ...}},
   {"id": "whoop/e2e/0915234f-c9d5-4cd7-b894-ab927380b68c/sleep",
    "properties": {"source": {"ref": ".../source/whoop/e2e"}, "at": "2026-09-21T22:39:00.693Z", "endsAt": "2026-09-22T06:24:00.227Z",
                   "utcOffsetStart": 60, "utcOffsetEnd": 60, "timeAsleep": "PT7H30M49.542S", "disturbances": 11,
                   "respiratoryRate": {"value": "14.3720703", "unit": "/min"},
                   "scores": {"sleepPerformance": "98", "sleepConsistency": "86"}, ...}}], ...}
```

Each record carries the night's span at its own offsets. Only Apple Health
sends what `latency` is computed from, and only WHOOP scores the night.

The stage totals, inside one device's night. `bucket.records` makes each
night a bucket; its `sources` keeps the WHOOP night where the devices
overlap, the request's `sources` keeps the WHOOP stages inside it, and
`utcOffsets` writes the row at the night's own offsets:

```json
{"filter": {"kinds": ["ada.example.com/observations/observation"]},
 "aggregate": {
   "metric": {"ref": "ada.example.com/observations/metric/health/sleep_stage"},
   "range": {"gte": "2026-09-21T12:00:00+01:00", "lt": "2026-09-22T12:00:00+01:00"},
   "bucket": {"records": {
     "filter": {"kinds": ["ada.example.com/health/sleep"], "properties": {"isNap": {"eq": false}}},
     "sources": {"policy": "priority", "property": "source",
                 "order": ["whoop/e2e", "applehealth/iphone/apple-watch"]},
     "utcOffsets": {"start": "utcOffsetStart", "end": "utcOffsetEnd"}}},
   "groupBy": ["code"],
   "sources": {"policy": "priority", "order": ["whoop/e2e", "applehealth/iphone/apple-watch"]},
   "reducers": ["sum"],
   "unit": "min"}}
```

```text
{"metric": "health/sleep_stage", "unit": "min", "sources": ["whoop/e2e"],
 "rows": [{"start": "2026-09-21T23:39:00.693+01:00", "end": "2026-09-22T07:24:00.227+01:00", "group": {"code": "awake"},
           "record": {"ref": "ada.example.com/health/sleep/whoop/e2e/0915234f-c9d5-4cd7-b894-ab927380b68c/sleep", "version": 1},
           "values": [{"reducer": "sum", "value": "14.1665333333333", "unit": "min"}], "count": 1, ...},
          {..., "group": {"code": "deep"}, "values": [{"reducer": "sum", "value": "90.6856166666667", ...}], "count": 1, ...},
          {..., "group": {"code": "light"}, "values": [{"reducer": "sum", "value": "251.140083333333", ...}], "count": 1, ...},
          {..., "group": {"code": "rem"}, "values": [{"reducer": "sum", "value": "109", ...}], "count": 1, ...}], ...}
```

A share is a stage over the night's sum (deep: 90.7 of 465 minutes, 20
percent). The same read over a month makes a row per night and code, one
device's night each.

A hypnogram needs each stage interval in time order. Neither shipped adapter
writes one: WHOOP's API and Apple Health's export send a night's stage
totals, and each adapter writes one `health/sleep_stage` observation per
stage over the whole night (`count` 1 above). An adapter whose upstream
sends the intervals writes an observation per interval, and a list read
whose filter bounds `at` on both ends (the window read, which orders by
`at` alone) answers them in the order a hypnogram draws.

Heart rate, SpO2 and breathing inside the same night, several metrics over
the same records-mode window:

```json
{"aggregate": {
   "metrics": [
     {"ref": "ada.example.com/observations/metric/health/heart_rate", "reducers": ["min", "avg", {"percentile": 0.05}]},
     {"ref": "ada.example.com/observations/metric/health/spo2", "reducers": ["min", "avg"]},
     {"ref": "ada.example.com/observations/metric/health/respiratory_rate", "reducers": ["avg"]}],
   "range": {"gte": "2026-09-21T12:00:00+01:00", "lt": "2026-09-22T12:00:00+01:00"},
   "bucket": {"records": {
     "filter": {"kinds": ["ada.example.com/health/sleep"], "properties": {"isNap": {"eq": false}}},
     "sources": {"policy": "priority", "property": "source",
                 "order": ["whoop/e2e", "applehealth/iphone/apple-watch"]},
     "utcOffsets": {"start": "utcOffsetStart", "end": "utcOffsetEnd"}}},
   "sources": {"policy": "priority",
               "order": ["applehealth/iphone/apple-watch", "applehealth/iphone/health"]}}}
```

```text
{"units": {"health/heart_rate": "/min", "health/respiratory_rate": "/min", "health/spo2": "%"},
 "rows": [{"start": "2026-09-21T23:39:00.693+01:00", "end": "2026-09-22T07:24:00.227+01:00",
           "group": {"metric": "health/heart_rate"}, "record": {"ref": ".../health/sleep/whoop/e2e/0915234f-c9d5-4cd7-b894-ab927380b68c/sleep", ...},
           "sources": ["applehealth/iphone/apple-watch"], "coverage": "PT5H50M59.307S",
           "values": [{"reducer": "min", "value": "42", "unit": "/min"},
                      {"reducer": "avg", "value": "49.5409663765289", "unit": "/min"},
                      {"reducer": {"percentile": 0.05}, "value": "42.45", "unit": "/min"}], "count": 90, ...},
          {..., "group": {"metric": "health/respiratory_rate"}, "sources": ["applehealth/iphone/health"],
           "values": [{"reducer": "avg", "value": "14.7129052406395", "unit": "/min"}], "count": 30, ...},
          {..., "group": {"metric": "health/spo2"}, "sources": ["applehealth/iphone/health"],
           "values": [{"reducer": "min", "value": "94", "unit": "%"},
                      {"reducer": "avg", "value": "96.795687375997", "unit": "%"}], "count": 46, ...}], ...}
```

The window is the WHOOP night, and the points inside it are Apple's: WHOOP
writes no sample series. The watch samples heart rate about every five
minutes and holds each value four, so the heart rate row's `coverage` is 5
hours 51 minutes of a 7 hour 45 minute night, and `avg`, the time-weighted
mean, weighs each value by the time it held.

No reducer answers when the lowest heart rate happened. A downsample to one
column does: its `min` carries the instant `at` it was taken.

```json
{"filter": {"properties": {"source": {"eq": "applehealth/iphone/apple-watch"}}},
 "aggregate": {"metric": {"ref": "ada.example.com/observations/metric/health/heart_rate"},
               "range": {"gte": "2026-09-21T23:39:00+01:00", "lt": "2026-09-22T07:24:00+01:00"}},
 "downsample": {"points": 1}}
```

```text
{"bucket": {"every": "PT8H", "origin": "2026-09-21T22:30:00Z"},
 "rows": [{"start": "2026-09-21T23:39:00+01:00", "end": "2026-09-22T07:24:00+01:00",
           "values": [{"reducer": "first", "value": "55", "at": "2026-09-21T23:41:00+01:00", ...},
                      {"reducer": "last", "value": "58", "at": "2026-09-22T07:20:00+01:00", ...},
                      {"reducer": "min", "value": "42", "at": "2026-09-22T00:27:00+01:00", ...},
                      {"reducer": "max", "value": "59", "at": "2026-09-22T01:23:00+01:00", ...}], "count": 90, ...}], ...}
```

The lowest heart rate of the night, 42, came at 00:27, under an hour after
Ada fell asleep.

A sleep trend counts each night on the day Ada woke up, at the offset she
woke at. A property read over the sleep records does it, one device's night
where several recorded it:

```json
{"filter": {"kinds": ["ada.example.com/health/sleep"], "properties": {"isNap": {"eq": false}}},
 "aggregate": {
   "property": {"name": "timeAsleep", "anchor": "endsAt", "utcOffset": "utcOffsetEnd",
                "sources": {"policy": "priority", "property": "source",
                            "order": ["whoop/e2e", "applehealth/iphone/apple-watch"]}},
   "range": {"gte": "2026-09-13T00:00:00+03:00", "lt": "2026-10-04T00:00:00+03:00"},
   "bucket": {"calendar": "week", "weekStart": "sunday"},
   "reducers": ["avg", "stddev", "count"],
   "unit": "h"}}
```

```text
{"property": "timeAsleep", "unit": "h", "bucket": {"calendar": "week", "weekStart": "sunday"},
 "rows": [{"start": "2026-09-13T00:00:00+03:00", "end": "2026-09-20T00:00:00+03:00",
           "values": [{"reducer": "avg", "value": "7.6503701984126984127", "unit": "h"},
                      {"reducer": "stddev", "value": "0.49387534623206762294", "unit": "h"},
                      {"reducer": "count", "value": "7"}], ...},
          {"start": "2026-09-20T00:00:00+01:00", "end": "2026-09-27T00:00:00+03:00",
           "values": [{"reducer": "avg", "value": "7.6528739285714285714", ...}, ...], ...},
          {"start": "2026-09-27T00:00:00+03:00", "end": "2026-10-04T00:00:00+03:00",
           "values": [{"reducer": "avg", "value": "7.7273220238095238095", ...}, ...], ...}], ...}
```

The week of the trip starts at London's midnight and ends at Athens's,
because its first night ended in London and its last in Athens.

## A workout with zones and pace

A workout screen shows the session's figures, the time in each heart rate
zone, the heart rate and pace curves, and the recovery after it. Each device
that recorded the run wrote a `health/workout`:

```http
GET /api/v1/records?filter={"kinds": ["ada.example.com/health/workout"],
                            "properties": {"at": {"gte": "2026-09-22T06:00:00+01:00"}, "endsAt": {"lt": "2026-09-22T10:00:00+01:00"}}}
                   &orderBy=source
```

```text
{"records": [
   {"id": "applehealth/iphone/9931515A-49DA-4850-A20D-956A5228C096",
    "properties": {"source": {"ref": ".../source/applehealth/iphone/apple-watch"}, "sport": "running", "sportSource": "Outdoor Run",
                   "at": "2026-09-22T06:47:45Z", "endsAt": "2026-09-22T07:24:28Z", "utcOffsetStart": 60,
                   "duration": "PT36M43S", "distance": {"value": "5.945", "unit": "km"},
                   "activeEnergy": {"value": "377.2", "unit": "kcal"}, "averageHeartRate": {"value": "142", "unit": "/min"},
                   "maxHeartRate": {"value": "162", "unit": "/min"}, "elevationGain": {"value": "44.7", "unit": "m"}, ...}},
   {"id": "whoop/e2e/c953c21f-cb2c-403a-964a-33b0c2e58afd/workout",
    "properties": {"source": {"ref": ".../source/whoop/e2e"}, "sport": "running", "sportSource": "running", "strain": "10.6036",
                   "duration": "PT36M59.785S", "distance": {"value": "5975.12967505", "unit": "m"},
                   "totalEnergy": {"value": "1620.28426", "unit": "kJ"},
                   "zoneDurations": {"zone3": "PT29M29.83S", "zone4": "PT4M29.973S", ...}, ...}}], ...}
```

Every adapter maps its provider's word onto one `sport` value and keeps the
word in `sportSource`. The watch's run is 36 minutes 43 seconds over 5.945
km, a mean pace of about 6:11 per kilometre.

The time in zones, over one device's run: the records mode with
`match: ["sport"]` keeps the Apple Watch run where the devices recorded the
same run, and `durationIn` sums the time heart rate held inside each band.
The bands here are 50, 60, 70, 80 and 90 percent of a maximum of 188; the
zones in force on the day are the app's to keep, since no record holds
them.

```json
{"aggregate": {
   "metric": {"ref": "ada.example.com/observations/metric/health/heart_rate"},
   "range": {"gte": "2026-09-22T00:00:00+01:00", "lt": "2026-09-23T00:00:00+01:00"},
   "bucket": {"records": {
     "filter": {"kinds": ["ada.example.com/health/workout"], "properties": {"sport": {"eq": "running"}}},
     "sources": {"policy": "priority", "property": "source", "match": ["sport"],
                 "order": ["applehealth/iphone/apple-watch", "whoop/e2e"]},
     "utcOffsets": {"start": "utcOffsetStart", "end": "utcOffsetEnd"}}},
   "sources": {"policy": "priority", "order": ["applehealth/iphone/apple-watch"]},
   "reducers": [{"durationIn": [94, 113]}, {"durationIn": [113, 132]}, {"durationIn": [132, 150]},
                {"durationIn": [150, 169]}, {"durationIn": [169, 220]}, "avg", "max"],
   "durationUnit": "min"}}
```

```text
{"metric": "health/heart_rate", "unit": "/min", "sources": ["applehealth/iphone/apple-watch"],
 "rows": [{"start": "2026-09-22T07:47:45+01:00", "end": "2026-09-22T08:24:28+01:00",
           "record": {"ref": "ada.example.com/health/workout/applehealth/iphone/9931515A-49DA-4850-A20D-956A5228C096", "version": 1},
           "values": [{"reducer": {"durationIn": [94, 113]}, "value": "0", "unit": "min"},
                      {"reducer": {"durationIn": [113, 132]}, "value": "0", "unit": "min"},
                      {"reducer": {"durationIn": [132, 150]}, "value": "18", "unit": "min"},
                      {"reducer": {"durationIn": [150, 169]}, "value": "8", "unit": "min"},
                      {"reducer": {"durationIn": [169, 220]}, "value": "0", "unit": "min"},
                      {"reducer": "avg", "value": "141.598968587156", "unit": "/min"},
                      {"reducer": "max", "value": "156.186952", "unit": "/min"}],
           "count": 7, "coverage": "PT27M15S", "nonAdditive": true, ...}], ...}
```

The zones cover 26 of the run's 37 minutes. The heart rate comes from the
watch's all-day stream, a value about every five minutes held for four, so
about one minute in five holds no value (`coverage` 27 minutes 15 seconds)
and the run's seven readings decide every zone. The Apple Health adapter
writes a workout's 60-second `heartRateData` to no stream. WHOOP's own
`zoneDurations` on its record are WHOOP's computation from its own heart
rate and differ (29.5 minutes in its zone 3).

The curves, a minute at a time from the start to a few minutes past the end,
heart rate and distance in one read; the minutes after the end are the
recovery:

```json
{"filter": {"properties": {"source": {"eq": "applehealth/iphone/apple-watch"}}},
 "aggregate": {
   "metrics": [
     {"ref": "ada.example.com/observations/metric/health/heart_rate", "reducers": ["avg"]},
     {"ref": "ada.example.com/observations/metric/health/distance", "reducers": ["sum"], "unit": "m"}],
   "range": {"gte": "2026-09-22T07:47:00+01:00", "lt": "2026-09-22T08:35:00+01:00"},
   "bucket": {"every": "PT1M"}}}
```

```text
{"units": {"health/distance": "m", "health/heart_rate": "/min"}, "bucket": {"every": "PT1M"},
 "rows": [{"start": "2026-09-22T07:47:00+01:00", "group": {"metric": "health/heart_rate"},
           "values": [{"reducer": "avg", "value": "58", "unit": "/min"}], "count": 0, ...},
          ...
          {"start": "2026-09-22T07:51:00+01:00", "group": {"metric": "health/distance"},
           "values": [{"reducer": "sum", "value": "158.8043674", "unit": "m"}], "count": 1, ...},
          {"start": "2026-09-22T07:51:00+01:00", "group": {"metric": "health/heart_rate"},
           "values": [{"reducer": "avg", "value": "156.186952", "unit": "/min"}], "count": 1, ...},
          ...
          {"start": "2026-09-22T08:23:00+01:00", "group": {"metric": "health/heart_rate"},
           "values": [{"reducer": "avg", "value": "142.700619", "unit": "/min"}], "count": 0, ...},
          {"start": "2026-09-22T08:24:00+01:00", "group": {"metric": "health/distance"},
           "values": [{"reducer": "sum", "value": null, "unit": "m"}], "count": 0, ...},
          {"start": "2026-09-22T08:25:00+01:00", "group": {"metric": "health/heart_rate"},
           "values": [{"reducer": "avg", "value": "90", "unit": "/min"}], "count": 1, ...},
          ...], ...}
```

`avg`, the time-weighted mean, answers a held value in a minute with no
reading of its own (`count` 0), and a minute past the hold has no heart rate
row at all (08:24, 08:29 and 08:30). The first reading after the run, at
08:25, is 90 per minute: 66 below the run's highest. A one-minute recovery
needs a stream that keeps sampling densely after the end.

Pace is the app's arithmetic: the unit table has a `pace` measure, but no
adapter writes `health/pace` or `health/speed`. A minute's pace is 1,000
over its distance in metres, in minutes per kilometre (158.8 m is 6:18), and
the run's mean pace is its `duration` over its `distance`, never a mean of
the minutes' paces. Splits per kilometre have no read either: the watch's
distance is an amount trace, a minute at a time, which the app cuts at each
kilometre ([R-WORK-3](plans/observations/requirements.md#apps-workouts)).

## Weekly load

A training-load screen shows the week's time per sport and an acute load
against a chronic one. The week's training is a property read over the
workout records themselves: `property.sources` counts each run once where
several devices recorded it, and `property.groupBy` makes a row per sport:

```json
{"filter": {"kinds": ["ada.example.com/health/workout"]},
 "aggregate": {
   "property": {"name": "duration", "utcOffset": "utcOffsetStart", "groupBy": ["sport"],
                "sources": {"policy": "priority", "property": "source", "match": ["sport"],
                            "order": ["applehealth/iphone/apple-watch", "whoop/e2e"]}},
   "range": {"gte": "2026-09-13T00:00:00+03:00", "lt": "2026-10-11T00:00:00+03:00"},
   "bucket": {"calendar": "week", "weekStart": "sunday"},
   "reducers": ["sum", "count"],
   "unit": "h"}}
```

```text
{"property": "duration", "unit": "h", "bucket": {"calendar": "week", "weekStart": "sunday"},
 "rows": [{"start": "2026-09-13T00:00:00+03:00", "end": "2026-09-20T00:00:00+03:00", "group": {"properties": {"sport": "running"}},
           "values": [{"reducer": "sum", "value": "2.8888888888888888889", "unit": "h"}, {"reducer": "count", "value": "3"}], ...},
          {"start": "2026-09-13T00:00:00+03:00", ..., "group": {"properties": {"sport": "strength"}},
           "values": [{"reducer": "sum", "value": "0.95138888888888888889", ...}, {"reducer": "count", "value": "1"}], ...},
          ...
          {"start": "2026-09-20T00:00:00+01:00", "end": "2026-09-27T00:00:00+01:00", "group": {"properties": {"sport": "running"}},
           "values": [{"reducer": "sum", "value": "0.61194444444444444444", ...}, {"reducer": "count", "value": "1"}], ...},
          {"start": "2026-09-20T00:00:00+03:00", "end": "2026-09-27T00:00:00+03:00", "group": {"properties": {"sport": "strength"}},
           "values": [{"reducer": "sum", "value": "0.86833333333333333333", ...}, {"reducer": "count", "value": "1"}], ...},
          ...
          {"start": "2026-10-04T00:00:00+03:00", ..., "group": {"properties": {"sport": "cycling"}},
           "values": [{"reducer": "sum", "value": "1.0997222222222222222", ...}, {"reducer": "count", "value": "1"}], ...}, ...], ...}
```

Both devices recorded each session; each counts once. Each sport's row of a
week carries the offsets of its own first and last workout, so the London
week's run starts at `+01:00` while the same week's strength session row
starts at `+03:00`. Rows carry no bucket index, so an app joins the sports
of a week by the local date in `start` (`2026-09-20` in both), not by the
instant.

The load: training minutes per day, rest days filled with zero, and the
7-day mean beside the 28-day mean, one read per window length:

```json
{"filter": {"kinds": ["ada.example.com/health/workout"]},
 "aggregate": {
   "property": {"name": "duration", "utcOffset": "utcOffsetStart",
                "sources": {"policy": "priority", "property": "source", "match": ["sport"],
                            "order": ["applehealth/iphone/apple-watch", "whoop/e2e"]}},
   "range": {"gte": "2026-09-28T00:00:00+03:00", "lt": "2026-10-12T00:00:00+03:00"},
   "bucket": {"calendar": "day"},
   "reducers": ["sum"],
   "fill": "zero",
   "rolling": {"buckets": 7, "reducers": ["avg"]},
   "unit": "min"}}
```

```text
{"property": "duration", "unit": "min", "bucket": {"calendar": "day"},
 "rows": [{"start": "2026-09-28T00:00:00+03:00", ...,
           "values": [{"reducer": "sum", "value": "38.6", "unit": "min"},
                      {"reducer": "avg", "of": "sum", "rolling": 7, "n": 7, "value": "32.6047619047619", "unit": "min"}], ...},
          {"start": "2026-09-30T00:00:00+03:00", ..., "synthetic": true, "count": 0,
           "values": [{"reducer": "sum", "value": "0", ...}, {"reducer": "avg", "of": "sum", "rolling": 7, "n": 7, "value": "33.1142857142857", ...}]},
          ...
          {"start": "2026-10-11T00:00:00+03:00", ...,
           "values": [{"reducer": "sum", "value": "98.666666666666666667", ...},
                      {"reducer": "avg", "of": "sum", "rolling": 7, "n": 7, "value": "46.9714285714286", ...}], ...}]}
```

The same read with `"buckets": 28` answers 34.6440476190476 on 2026-10-11:
an acute load of 47 minutes a day against a chronic 34.6, a ratio of 1.36.

An exponentially weighted load (fitness, fatigue and form) is the app's,
computed from the daily rows.

## Steps across travel

A step count is per local day, and on a trip the local day moves. In the
recorded-local calendar each value lands on the date its own UTC offset
names. Each row says which source and which points served it:

```json
{"aggregate": {
   "metric": {"ref": "ada.example.com/observations/metric/health/steps"},
   "from": "prefer-raw",
   "range": {"gte": "2026-09-19T00:00:00+03:00", "lt": "2026-09-26T00:00:00+03:00"},
   "bucket": {"calendar": "day", "fallbackTimezone": "Europe/Athens"},
   "sources": {"policy": "priority",
               "order": ["applehealth/iphone/apple-watch", "applehealth/iphone/iphone"]},
   "reducers": ["sum"]}}
```

```text
{"metric": "health/steps", "unit": "{steps}", "bucket": {"calendar": "day", "fallbackTimezone": "Europe/Athens"},
 "from": "prefer-raw",
 "rows": [{"start": "2026-09-19T00:00:00+03:00", "end": "2026-09-20T00:00:00+03:00", "from": "raw",
           "sources": ["applehealth/iphone/apple-watch", "applehealth/iphone/iphone"],
           "values": [{"reducer": "sum", "value": "7575.28085904331", "unit": "{steps}"}], "count": 159, ...},
          {"start": "2026-09-20T00:00:00+01:00", "end": "2026-09-21T00:00:00+01:00",
           "values": [{"reducer": "sum", "value": "11275.5748838589", ...}], "count": 125, ...},
          {"start": "2026-09-21T00:00:00+01:00", "end": "2026-09-22T00:00:00+01:00",
           "values": [{"reducer": "sum", "value": "9081.3588736407", ...}], "count": 135, ...},
          ...
          {"start": "2026-09-24T00:00:00+01:00", "end": "2026-09-25T00:00:00+01:00", "sources": ["applehealth/iphone/iphone"],
           "values": [{"reducer": "sum", "value": "14506.2960468793", ...}], "count": 150, ...},
          {"start": "2026-09-25T00:00:00+03:00", "end": "2026-09-26T00:00:00+03:00",
           "values": [{"reducer": "sum", "value": "7821.4849841496", ...}], "count": 174, ...}], ...}
```

Each row's bounds are written at the offsets of its own values: the London
days at `+01:00`, the days at home at `+03:00`. The 2026-09-24 row's end
reads London's midnight and the next row starts at Athens's midnight, two
hours earlier.

`fallbackTimezone` places a value written without an offset by that zone's
clock and counts it in the row's `offsetless`; without it, a stream holding
such a value refuses the read. The same read in a named zone
(`"timezone": "Europe/Athens"`) cuts every day at Athens midnight, as a home
screen that ignores travel would:

```text
{"bucket": {"calendar": "day", "timezone": "Europe/Athens"},
 "rows": [...,
          {"start": "2026-09-20T00:00:00+03:00", "end": "2026-09-21T00:00:00+03:00", "values": [{"reducer": "sum", "value": "11151.6932932884", ...}], ...},
          {"start": "2026-09-21T00:00:00+03:00", "end": "2026-09-22T00:00:00+03:00", "values": [{"reducer": "sum", "value": "9078.7163399419", ...}], ...},
          {"start": "2026-09-22T00:00:00+03:00", "end": "2026-09-23T00:00:00+03:00", "values": [{"reducer": "sum", "value": "18818.2459145191", ...}], ...},
          ...
          {"start": "2026-09-24T00:00:00+03:00", "end": "2026-09-25T00:00:00+03:00", "values": [{"reducer": "sum", "value": "14595.4503954077", ...}], ...}, ...]}
```

In the Athens read each London day starts and ends two hours early, at
22:00 London time: 2026-09-22 reads 18,818 steps there and 18,826 on the day
Ada lived.

## Weight in kg and lb

Weight reads in any mass unit, and the read converts each stored value.
`exact` is `false` where any value came from a float64 reduction or a
conversion that rounds ([the response](api.md#the-response)). In kg below
it is `false` because of `avg`: no value needs converting, so `min` and
`max` answer the stored digits. Weekly, the Withings scale's weigh-ins and
the one Ada typed in by hand:

```json
{"filter": {"properties": {"source": {"in": ["applehealth/iphone/withings", "manual"]}}},
 "aggregate": {
   "metric": {"ref": "ada.example.com/observations/metric/health/body_mass"},
   "range": {"gte": "2026-09-07T00:00:00+03:00", "lt": "2026-10-05T00:00:00+03:00"},
   "bucket": {"calendar": "week", "weekStart": "monday", "timezone": "Europe/Athens"},
   "reducers": ["avg", "min", "max", "count"],
   "sources": {"policy": "priority", "order": ["applehealth/iphone/withings", "manual"]},
   "unit": "kg"}}
```

```text
{"metric": "health/body_mass", "unit": "kg", "exact": false,
 "rows": [{"start": "2026-09-07T00:00:00+03:00", "end": "2026-09-14T00:00:00+03:00", "sources": ["applehealth/iphone/withings"],
           "coverage": "PT0S",
           "values": [{"reducer": "avg", "value": "72.44", "unit": "kg"}, {"reducer": "min", "value": "72.14", "unit": "kg"},
                      {"reducer": "max", "value": "72.83", "unit": "kg"}, {"reducer": "count", "value": "4"}], ...},
          ...
          {"start": "2026-09-21T00:00:00+03:00", "end": "2026-09-28T00:00:00+03:00", "sources": ["applehealth/iphone/withings", "manual"],
           "values": [{"reducer": "avg", "value": "71.7966666666667", ...}, {"reducer": "min", "value": "71.64", ...},
                      {"reducer": "max", "value": "72.0", ...}, {"reducer": "count", "value": "3"}], ...},
          {"start": "2026-09-28T00:00:00+03:00", "end": "2026-10-05T00:00:00+03:00", "sources": ["applehealth/iphone/withings"],
           "values": [{"reducer": "avg", "value": "71.9525", ...}, {"reducer": "min", "value": "71.82", ...},
                      {"reducer": "max", "value": "72.1", ...}, {"reducer": "count", "value": "4"}], ...}], ...}
```

The week of the London trip holds two weigh-ins at home and the 72.0 kg Ada
typed in from a hotel scale, `manual/2026-09-22-weight`. `priority` answers
one row per week for the two sources, where `separate` would answer a row
per source.

A weigh-in holds for no time (`hold` `PT0S`, as the catalogue declares for
every spot body reading), so `coverage` is `PT0S` and the time-weighted
`avg` of a gauge falls back to the mean of the week's weigh-ins: 72.44 is
the four values of the first week added and divided by four.

The same read with `"unit": "[lb_av]"`:

```text
{"metric": "health/body_mass", "unit": "[lb_av]", "exact": false,
 "rows": [{"start": "2026-09-07T00:00:00+03:00", ...,
           "values": [{"reducer": "avg", "value": "159.702862726725", "unit": "[lb_av]"},
                      {"reducer": "min", "value": "159.04147594017068673", "unit": "[lb_av]"},
                      {"reducer": "max", "value": "160.56266554924634204", "unit": "[lb_av]"},
                      {"reducer": "count", "value": "4"}], ...}, ...]}
```

In `[lb_av]` the read is not `exact` without `avg` either: a pound is
0.45359237 kg, so dividing by it rounds `min` and `max` to 20 significant
digits. The app rounds each value to the metric record's
`displayPrecision`.

The filter names the scale's source because a scale that reached the
repository through two providers would arrive as two sources with nothing
linking them
([G-31](plans/observations/requirements.md#g-31-one-device-under-several-sources)),
and `separate` would show each weigh-in twice.

## Blood pressure

Each reading is two observations, systolic and diastolic, each carrying
both numbers in `components`:

```http
GET /api/v1/records?filter={"kinds": ["ada.example.com/observations/observation"],
                            "properties": {"metric": {"eq": "health/systolic_blood_pressure"},
                                           "at": {"gte": "2026-09-27T00:00:00+03:00", "lt": "2026-09-28T00:00:00+03:00"}}}
                   &orderBy=at&first=10
```

```text
{"records": [
   {"id": "applehealth/iphone/withings/systolic_blood_pressure/2026-09-27T05:51:30Z",
    "properties": {"at": "2026-09-27T05:51:30Z", "utcOffset": 180, "value": {"value": "118", "unit": "mm[Hg]"},
                   "source": {"ref": ".../source/applehealth/iphone/withings"}, "hold": "PT0S",
                   "components": [{"metric": {"ref": ".../metric/health/systolic_blood_pressure"}, "value": {"value": "118", "unit": "mm[Hg]"}},
                                  {"metric": {"ref": ".../metric/health/diastolic_blood_pressure"}, "value": {"value": "69", "unit": "mm[Hg]"}}],
                   "origin": {"ref": "providers.substrate.reamde.dev/applehealth/metricbatch/applehealth/iphone/blood_pressure/minutes/2026-09-27T05"}, ...}}], ...}
```

The Sunday reading is the Withings cuff's, through Apple Health. `origin`
names the delivery the adapter read it from; the record is the adapter's,
and a correction is a record of Ada's own from a source ranked above it
([correcting a value](observations.md#correcting-a-value-an-adapter-wrote)).

The month's averages, both numbers aligned in one read, one row per source:

```json
{"aggregate": {
   "metrics": [
     {"ref": "ada.example.com/observations/metric/health/systolic_blood_pressure", "reducers": ["avg", "max"]},
     {"ref": "ada.example.com/observations/metric/health/diastolic_blood_pressure", "reducers": ["avg", "max"]}],
   "range": {"gte": "2026-09-01T00:00:00+03:00", "lt": "2026-10-01T00:00:00+03:00"},
   "bucket": {"calendar": "month", "timezone": "Europe/Athens"},
   "groupBy": ["source"]}}
```

```text
{"units": {"health/diastolic_blood_pressure": "mm[Hg]", "health/systolic_blood_pressure": "mm[Hg]"},
 "bucket": {"calendar": "month", "timezone": "Europe/Athens"},
 "rows": [{"start": "2026-09-01T00:00:00+03:00", "end": "2026-10-01T00:00:00+03:00",
           "group": {"metric": "health/diastolic_blood_pressure", "source": "applehealth/iphone/withings"},
           "values": [{"reducer": "avg", "value": "72.3333333333333", "unit": "mm[Hg]"}, {"reducer": "max", "value": "77", ...}], "count": 3, ...},
          {..., "group": {"metric": "health/systolic_blood_pressure", "source": "applehealth/iphone/withings"},
           "values": [{"reducer": "avg", "value": "114.666666666667", "unit": "mm[Hg]"}, {"reducer": "max", "value": "118", ...}], "count": 3, ...}], ...}
```

The category of a reading (normal, elevated, stage 1, stage 2) is the
app's: no record holds a classification table
([R-BODY-2](plans/observations/requirements.md#apps-body-and-activity-goals)).

## Labs in both unit systems

A report and every result on it:

```http
GET /api/v1/records?filter={"kinds": ["ada.example.com/health/labresult"],
                            "referencing": {"ref": "ada.example.com/health/labreport/harborpoint/2026-08-31", "property": "report"}}
                   &orderBy=metric&first=50
```

```text
{"records": [
   ...,
   {"id": "harborpoint/2026-08-31/hba1c/as/health/hba1c_ifcc",
    "properties": {"value": {"value": "34.4", "unit": "mmol/mol"},
                   "conversion": {"formula": "ngsp-to-ifcc", "parameters": {"slope": "10.929", "intercept": "2.15"}, ...}, ...}},
   ...,
   {"id": "harborpoint/2026-08-31/ldl_cholesterol_mass/as/health/ldl_cholesterol",
    "properties": {"metric": {"ref": ".../metric/health/ldl_cholesterol"}, "value": {"value": "2.897", "unit": "mmol/L"},
                   "referenceRanges": [{"high": {"value": "2.586", "unit": "mmol/L"}}],
                   "interpretation": [{"code": "H", "display": "High"}],
                   "derivedFrom": [{"ref": "ada.example.com/health/labresult/harborpoint/2026-08-31/ldl_cholesterol_mass"}],
                   "conversion": {"formula": "molar-mass", "parameters": {"molarMass": "386.65"}, "version": "1"}, ...}},
   {"id": "harborpoint/2026-08-31/ldl_cholesterol_mass",
    "properties": {"metric": {"ref": ".../metric/health/ldl_cholesterol_mass"}, "value": {"value": "112", "unit": "mg/dL"},
                   "coding": {"system": "http://loinc.org", "code": "13457-7"},
                   "referenceRanges": [{"high": {"value": "100", "unit": "mg/dL"}}],
                   "interpretation": [{"code": "H", "display": "High"}], "status": "final", ...}},
   ...],
 "matches": {...}, "head": ..., "generation": ...}
```

The report holds 16 results: the lab's eight in mg/dL, ng/mL and %, and the
eight `convertlab` derived, in the order of their metric paths. A derived
result converts the reference range with the value, each bound at the
value's decimal places, and keeps the lab's flag.

The US lab reported in mg/dL, and the health sample's `convertlab` function
wrote each result again in the paired SI metric, naming its input and the
formula. So one analyte's trend reads in either unit system across every
lab, each reading once:

```json
{"aggregate": {
   "metric": {"ref": "ada.example.com/observations/metric/health/ldl_cholesterol"},
   "range": {"gte": "2026-07-01T00:00:00+03:00", "lt": "2026-10-12T00:00:00+03:00"},
   "bucket": {"calendar": "day", "timezone": "Europe/Athens"},
   "groupBy": ["source"],
   "reducers": ["last"],
   "unit": "mmol/L"}}
```

```text
{"metric": "health/ldl_cholesterol", "unit": "mmol/L", "exact": true,
 "rows": [{"start": "2026-07-20T00:00:00+03:00", "group": {"source": "bioiatriki"},
           "values": [{"reducer": "last", "value": "3.1", "unit": "mmol/L"}], "count": 1, ...},
          {"start": "2026-08-31T00:00:00+03:00", "group": {"source": "harborpoint"},
           "values": [{"reducer": "last", "value": "2.897", "unit": "mmol/L"}], "count": 1, ...},
          {"start": "2026-10-05T00:00:00+03:00", "group": {"source": "bioiatriki"},
           "values": [{"reducer": "last", "value": "2.8", "unit": "mmol/L"}], "count": 1, ...}], ...}
```

The same trend in mg/dL is a read of `health/ldl_cholesterol_mass` with
`"unit": "mg/dL"`; a read of `ldl_cholesterol` in mg/dL is a `422`, because
the two are different measures:

```text
{"metric": "health/ldl_cholesterol_mass", "unit": "mg/dL", "exact": true,
 "rows": [{"start": "2026-07-20T00:00:00+03:00", "group": {"source": "bioiatriki"},
           "values": [{"reducer": "last", "value": "120", "unit": "mg/dL"}], ...},
          {"start": "2026-08-31T00:00:00+03:00", "group": {"source": "harborpoint"},
           "values": [{"reducer": "last", "value": "112", "unit": "mg/dL"}], ...},
          {"start": "2026-10-05T00:00:00+03:00", "group": {"source": "bioiatriki"},
           "values": [{"reducer": "last", "value": "108", "unit": "mg/dL"}], ...}], ...}
```

A derived value keeps one significant digit more than its input: the Athens
lab's 3.1 mmol/L reads 120 mg/dL.

Each point's own reference range and flag are on the result records, so a
chart that draws them reads the list beside the aggregate.

## Resting heart rate across providers and merged

Side by side, one row per source (`separate`, the default). WHOOP's value
spans the night from its evening onset, so `place: "end"` counts each night
in the week of the morning it ended. Apple Health's is a summary of a day,
which ends at midnight and stays on that day:

```json
{"aggregate": {
   "metric": {"ref": "ada.example.com/observations/metric/health/resting_heart_rate"},
   "from": "summaries",
   "range": {"gte": "2026-10-05T00:00:00+03:00", "lt": "2026-10-12T00:00:00+03:00"},
   "bucket": {"calendar": "week", "weekStart": "monday", "timezone": "Europe/Athens", "place": "end"},
   "reducers": ["avg", "min", "max", "count"]}}
```

```text
{"metric": "health/resting_heart_rate", "unit": "/min", "from": "summaries",
 "rows": [{"start": "2026-10-05T00:00:00+03:00", "end": "2026-10-12T00:00:00+03:00", "sources": ["applehealth/iphone/health"],
           "values": [{"reducer": "avg", "value": "51", ...}, {"reducer": "min", "value": "48", ...},
                      {"reducer": "max", "value": "55", ...}, {"reducer": "count", "value": "7"}], ...},
          {..., "sources": ["whoop/e2e"],
           "values": [{"reducer": "avg", "value": "50.7142857142857", ...}, {"reducer": "min", "value": "46", ...},
                      {"reducer": "max", "value": "53", ...}, {"reducer": "count", "value": "7"}], ...}], ...}
```

Each provider computes its own resting heart rate, so the two weeks
disagree (a mean of 51 against 50.7): these are two methods, not two
readings of one value.

Merged, one source per day: the same read with
`"sources": {"policy": "priority", "order": ["whoop/e2e", "applehealth/iphone/health"]}`.
A summary of a lower source is dropped where it overlaps a higher one's:

```text
{"sources": ["whoop/e2e"],
 "rows": [{"start": "2026-10-05T00:00:00+03:00", "end": "2026-10-12T00:00:00+03:00", "sources": ["whoop/e2e"],
           "values": [{"reducer": "avg", "value": "50.7142857142857", ...}, {"reducer": "min", "value": "46", ...},
                      {"reducer": "max", "value": "53", ...}, {"reducer": "count", "value": "7"}], ...}], ...}
```

Every Apple Health day overlaps a WHOOP night, so the merged week is
WHOOP's alone. Without `order`, `priority` ranks the sources by each source
record's `priorities` for the metric, so an app that stores the user's
choice there sends no order at all.

The lowest of the month and the record that holds it is a list read ordered
by value. A filter that bounds `at` on both ends turns a list into the window
read, which orders by `at` alone, so the month is `at` from its start and
`endsAt` before its end. That bound fits interval observations only, such as
WHOOP's per-night summaries here:

```http
GET /api/v1/records?filter={"kinds": ["ada.example.com/observations/observation"],
                            "properties": {"metric": {"eq": "health/resting_heart_rate"}, "source": {"eq": "whoop/e2e"},
                                           "at": {"gte": "2026-09-01T00:00:00+03:00"}, "endsAt": {"lt": "2026-10-01T00:00:00+03:00"}}}
                   &orderBy=value&first=1
```

```text
{"records": [
   {"id": "whoop/e2e/744890628/resting_heart_rate",
    "properties": {"at": "2026-09-28T20:11:00.823Z", "endsAt": "2026-09-29T03:46:00.868Z", "utcOffset": 180,
                   "value": {"value": "47", "unit": "/min"}, "form": "summary", "statistic": "vendor",
                   "period": "sleep", "periodId": "d2f5a65a-24c4-42ca-9294-42f2881b3370", ...}}],
 "cursor": "...", ...}
```

WHOOP's lowest of September was 47, in the night that ended on the morning
of 29 September. The record's id keeps the cycle the recovery scored
(744890628); its `periodId` is the night's sleep id.

An instant has no `endsAt`, so the same filter over
`health/systolic_blood_pressure` answers no records at all. For an instant
the app sends two reads: a [downsample](#a-chart-sized-series) to one column
over the month, whose `min` carries the instant it was taken, then a list
read whose `at` is `gte` and `lte` that instant. On the demo the Withings
cuff's lowest systolic of September is 109 at `2026-09-13T08:48:30+03:00`,
and the second read answers the record
`applehealth/iphone/withings/systolic_blood_pressure/2026-09-13T05:48:30Z`.
A sample packed in a trace has no record of its own: the downsample answers
its value and instant, as [a night](#a-night) shows for heart rate.

## The stream list

A settings or sources screen lists every stream: each metric from each
source, with its newest value and how many points it holds. The latest read
answers it in one request:

```json
{"filter": {"implements": "substrate.reamde.dev/core/observed"},
 "aggregate": {"latest": true, "groupBy": ["metric", "source", "about", "channel", "statistic"],
               "budget": {"maxGroups": 2000}}}
```

```text
{"rows": [...,
          {"start": "2026-10-11T23:56:00+03:00", "group": {"metric": "health/heart_rate", "source": "applehealth/iphone/apple-watch"},
           "form": "gauge", "values": [{"reducer": "last", "value": "49", "unit": "/min"}], "count": 24915, ...},
          {"start": "2026-10-11T00:13:00.105+03:00", "end": "2026-10-11T22:47:00.201+03:00",
           "group": {"metric": "health/heart_rate", "source": "whoop/e2e", "statistic": "avg"},
           "form": "summary", "values": [{"reducer": "last", "value": "65", "unit": "/min"}], "count": 90, ...},
          {"start": "2026-10-11T00:13:00.105+03:00", "end": "2026-10-11T22:47:00.201+03:00",
           "group": {"metric": "health/heart_rate", "source": "whoop/e2e", "statistic": "max"},
           "form": "summary", "values": [{"reducer": "last", "value": "158", "unit": "/min"}], "count": 90, ...},
          {"start": "2026-10-11T08:43:00.935+03:00", "end": "2026-10-11T10:22:00.653+03:00",
           "group": {"metric": "health/heart_rate", "source": "whoop/e2e", "channel": "workout", "statistic": "avg"},
           "form": "summary", "values": [{"reducer": "last", "value": "153", "unit": "/min"}], "count": 62, ...},
          ...,
          {"start": "2026-10-11T21:55:00+03:00", "end": "2026-10-11T21:56:00+03:00",
           "group": {"metric": "health/steps", "source": "applehealth/iphone/iphone"},
           "form": "amount", "values": [{"reducer": "last", "value": "10.8053996785", "unit": "{steps}"}], "count": 13900, ...}, ...],
 "sources": [9 source paths], ...}
```

The demo answers 72 streams from 9 sources. A summary stream with two
statistics is two rows, and a `channel` (WHOOP's heart rate over a workout,
beside its heart rate over a whole cycle) is a stream of its own.

The newest value of an amount is one sample (a minute of steps), not a
day's total; a day's total is the [today](#today) read.

## Incremental sync

An app that keeps its own copy of the screens it drew re-reads only what
changed. Every read answers the changelog position it was served at, in
`head` (a steps read answered `"head": {"changelog": 72469, "generation":
"pn2vgcqd6wfg"}`), and the change feed resumes from there, naming each
record a later write touched:

```bash
curl -sN "$SUBSTRATE_SERVER/api/v1/changes?watch=1&from=50000&generation=pn2vgcqd6wfg&values=1&kinds=ada.example.com/observations/observation,ada.example.com/observations/trace,ada.example.com/health/sleep,ada.example.com/health/workout" \
  -H "Authorization: Bearer $SUBSTRATE_TOKEN"
```

```text
{"bookmark": 50000, "generation": "pn2vgcqd6wfg"}
{"seq": 50003, "op": "put", "kind": "ada.example.com/observations/trace",
 "recordId": "applehealth/iphone/apple-watch/heart_rate/2026-09-06T07", "actor": "function:ada.example.com:healthapple:adaptapplehealth",
 "payload": {"created": true, ...},
 "affected": [{"kind": "ada.example.com/observations/trace", "id": "applehealth/iphone/apple-watch/heart_rate/2026-09-06T07", "version": 1,
               "properties": [{"name": "at", "after": "2026-09-06T07:00:00Z"},
                              {"name": "endsAt", "after": "2026-09-06T08:00:00Z"},
                              {"name": "hold", "after": "PT4M"},
                              {"name": "metric", "after": {"ref": "ada.example.com/observations/metric/health/heart_rate"}},
                              {"name": "source", "after": {"ref": ".../source/applehealth/iphone/apple-watch"}}, ...]}]}
{"seq": 50008, ..., "recordId": "applehealth/iphone/apple-watch/heart_rate/2026-09-06T08", ...}
...
```

Position 50000 lies inside the Apple Health adapter's backlog, so the lines
after it are the hours of heart rate the adapter wrote next, one trace an
hour: an app showing 6 September reads that day again.

The stream opens with a bookmark and sends one line per change. With
`values=1` a line names only the properties the write moved, before and
after. A creation moves every property, so its `metric`, `source`, `at` and
`endsAt` tell the app which days to read again. An update names only what
changed: a rescore, or a re-delivered hour of a trace, that changes `value`
or `values` names neither `metric` nor `at`. To place an update the app
reads the record at its path, the id percent-encoded
(`GET /api/v1/ada.example.com/observations/trace/applehealth%2Fiphone%2Fapple-watch%2Fheart_rate%2F2026-09-06T07`),
or keeps its own map from record to day. A delete moves no property and
leaves no record to read, so only that map places it. A feed that names the
time ranges a write touched is a follow-up
([G-32](plans/observations/requirements.md#g-32-change-feed-time-ranges)).
A connection that drops resumes from the last `seq` it read, with the same
`generation`.

## A chart-sized series

A chart of a week of heart rate needs about one value per pixel column, not
every point. `downsample` cuts the range into at most `points` columns, each
answering `first`, `last`, `min` and `max` with the instant each was taken:

```json
{"filter": {"properties": {"source": {"eq": "applehealth/iphone/apple-watch"}}},
 "aggregate": {"metric": {"ref": "ada.example.com/observations/metric/health/heart_rate"},
               "range": {"gte": "2026-09-20T00:00:00+03:00", "lt": "2026-09-27T00:00:00+03:00"}},
 "downsample": {"points": 400}}
```

```text
{"metric": "health/heart_rate", "unit": "/min", "bucket": {"every": "PT30M", "origin": "2026-09-19T21:00:00Z"},
 "rows": [{"start": "2026-09-20T00:00:00+03:00", "end": "2026-09-20T00:30:00+03:00",
           "values": [{"reducer": "first", "value": "71", "unit": "/min", "at": "2026-09-20T00:00:00+03:00"},
                      {"reducer": "last", "value": "73", "unit": "/min", "at": "2026-09-20T00:25:00+03:00"},
                      {"reducer": "min", "value": "65", "unit": "/min", "at": "2026-09-20T00:20:00+03:00"},
                      {"reducer": "max", "value": "73", "unit": "/min", "at": "2026-09-20T00:25:00+03:00"}], "count": 6, ...},
          ...], ...}
```

The week's 1,933 points come back as 336 columns: 400 columns would be 25.2
minutes wide, and the read widens a column to whole quarter hours so rollup
rows can serve it.

## Two units on one chart

A chart with two axes, heart rate beside steps by the hour, is one
`metrics` read. The response's `units` names each metric's unit, and each
value carries its own:

```json
{"aggregate": {
   "metrics": [
     {"ref": "ada.example.com/observations/metric/health/heart_rate", "reducers": ["avg", "max"]},
     {"ref": "ada.example.com/observations/metric/health/steps", "reducers": ["sum"]}],
   "range": {"gte": "2026-09-22T06:00:00+01:00", "lt": "2026-09-22T12:00:00+01:00"},
   "bucket": {"every": "PT1H"},
   "sources": {"policy": "priority", "order": ["applehealth/iphone/apple-watch", "applehealth/iphone/iphone"]}}}
```

```text
{"units": {"health/heart_rate": "/min", "health/steps": "{steps}"}, "bucket": {"every": "PT1H"},
 "rows": [...,
          {"start": "2026-09-22T07:00:00+01:00", "end": "2026-09-22T08:00:00+01:00", "group": {"metric": "health/heart_rate"},
           "values": [{"reducer": "avg", "value": "74.7517098723404", "unit": "/min"}, {"reducer": "max", "value": "156.186952", "unit": "/min"}], "count": 12, ...},
          {"start": "2026-09-22T07:00:00+01:00", "end": "2026-09-22T08:00:00+01:00", "group": {"metric": "health/steps"},
           "values": [{"reducer": "sum", "value": "2207.1752888377", "unit": "{steps}"}], "count": 6, ...},
          {"start": "2026-09-22T08:00:00+01:00", ..., "group": {"metric": "health/heart_rate"},
           "values": [{"reducer": "avg", "value": "109.970036428571", ...}, {"reducer": "max", "value": "148.46532", ...}], ...},
          {"start": "2026-09-22T08:00:00+01:00", ..., "group": {"metric": "health/steps"},
           "values": [{"reducer": "sum", "value": "4386.7914255387", ...}], ...}, ...], ...}
```

Rows come in bucket order, then by metric path, so a chart reads each
hour's pair together.

## What an app still does itself

Some screens need work no request does yet. Each item links the requirement
that tracks it:

- Zones, goals and settings in force on a past day (maximum heart rate, zone
  bounds, a step goal, a glucose target) have no record; the app keeps them
  ([G-33](plans/observations/requirements.md#g-33-kinds-an-app-writes-about-the-person)).
- Units: every read takes `unit`, but no record holds the person's
  preference, so each app keeps its own
  ([G-34](plans/observations/requirements.md#g-34-unit-preference));
  `GET /api/v1/units` serves the unit table and its exact factors.
- Pace, splits, exponentially weighted load and score bands are the app's
  arithmetic
  ([R-UNIT-2](plans/observations/requirements.md#apps-units),
  [G-50](plans/observations/requirements.md#g-50-reducers-and-filter-arms)).
- A web app on another origin cannot call the API yet: the server answers no
  CORS preflight
  ([C-9](plans/observations/requirements.md#c-9-cors-for-browser-apps)).
- A write is one record per request, so an app that writes many values
  (a backfill, a derived score per day) sends many requests, or calls a
  function of its own whose effects write them in one invocation
  ([functions](functions.md),
  [C-14](plans/observations/requirements.md#c-14-multi-record-writes)).
- An adapter's outputs are read-only to the owner: an edit to one is undone
  by the adapter's next delivery
  ([C-1](plans/observations/requirements.md#c-1-owner-corrections-undone-by-replays)).
  A correction is the owner's own record from a source ranked above the
  provider ([correcting a value](observations.md#correcting-a-value-an-adapter-wrote)).
