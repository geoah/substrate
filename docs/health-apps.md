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
2026-09-24 in London (UTC+1). Four providers deliver her data through their
adapters, and two labs report three blood panels. The sources the answers
name, by the bare id a filter and `sources.order` take:

| Source id | Is |
| --- | --- |
| `whoop/e2e` | the WHOOP band |
| `garmin/ada/account` | Garmin Connect: the all-day streams, sleep, daily totals, weigh-ins and blood pressure |
| `garmin/ada/forerunner265` | the Garmin watch's workouts and their samples |
| `applehealth/iphone/apple-watch`, `applehealth/iphone/iphone` | the Apple Watch and the phone, through Apple Health |
| `applehealth/iphone/health` | series Apple Health merged from several devices |
| `applehealth/iphone/withings` | a Withings scale and cuff, through Apple Health |
| `googlefit/e2e/google-pixel-watch-2.8d1f3c7a` | a Pixel Watch, through Google Fit |
| `googlefit/e2e/withings-body.4a91be20`, `googlefit/e2e/withings-bpm-connect.7c03d5e1` | the Withings scale and cuff, through Google Fit |
| `googlefit/e2e/com-strava` | Strava's workouts, through Google Fit |
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
totals, whose spans end inside the day, stay on their own day. Every source
writes a nightly summary over the night, WHOOP's recovery included, so one
placement serves them all.

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
     {"ref": "ada.example.com/observations/metric/health/hrv_rmssd", "from": "summaries", "statistic": "avg"}],
   "range": {"gte": "2026-10-11T00:00:00+03:00", "lt": "2026-10-12T00:00:00+03:00"},
   "bucket": {"calendar": "day", "place": "end"},
   "groupBy": ["code"],
   "sources": {"policy": "priority",
               "order": ["garmin/ada/account", "garmin/ada/forerunner265", "whoop/e2e",
                         "applehealth/iphone/apple-watch", "applehealth/iphone/iphone"]},
   "reducers": ["last"]}}
```

```text
{"units": {"health/active_energy": "kcal", "health/hrv_rmssd": "ms", "health/resting_heart_rate": "/min",
           "health/sleep_stage": "h", "health/steps": "{steps}", "health/whoop_recovery": "%"},
 "bucket": {"calendar": "day", "place": "end"}, "exact": false,
 "rows": [{"start": "2026-10-11T00:00:00+03:00", "end": "2026-10-12T00:00:00+03:00",
           "group": {"metric": "health/active_energy"}, "from": "raw", "sources": ["garmin/ada/account"],
           "values": [{"reducer": "sum", "value": "1871", "unit": "kcal"}], "count": 96, ...},
          {"start": "2026-10-11T00:00:00+03:00", ...,
           "group": {"metric": "health/hrv_rmssd"}, "from": "summaries", "sources": ["garmin/ada/account"],
           "values": [{"reducer": "last", "value": "62", "unit": "ms"}], "count": 1, ...},
          {"start": "2026-10-11T00:00:00+03:00", ...,
           "group": {"metric": "health/resting_heart_rate"}, "from": "summaries", "sources": ["garmin/ada/account"],
           "values": [{"reducer": "last", "value": "50", "unit": "/min"}], "count": 1, ...},
          {..., "group": {"metric": "health/sleep_stage", "code": "awake"}, "sources": ["garmin/ada/account"],
           "values": [{"reducer": "sum", "value": "0.136666666666667", "unit": "h"}], "count": 5, ...},
          {..., "group": {"metric": "health/sleep_stage", "code": "deep"},
           "values": [{"reducer": "sum", "value": "1.72611111111111", "unit": "h"}], "count": 6, ...},
          {..., "group": {"metric": "health/sleep_stage", "code": "light"},
           "values": [{"reducer": "sum", "value": "4.69194444444445", "unit": "h"}], "count": 12, ...},
          {..., "group": {"metric": "health/sleep_stage", "code": "rem"},
           "values": [{"reducer": "sum", "value": "1.69527777777778", "unit": "h"}], "count": 5, ...},
          {"start": "2026-10-11T00:00:00+03:00", ...,
           "group": {"metric": "health/steps"}, "from": "raw", "sources": ["garmin/ada/account"],
           "values": [{"reducer": "sum", "value": "23687", "unit": "{steps}"}], "count": 96, ...},
          {"start": "2026-10-11T00:00:00+03:00", ...,
           "group": {"metric": "health/whoop_recovery"}, "from": "summaries", "sources": ["whoop/e2e"],
           "values": [{"reducer": "last", "value": "67", "unit": "%"}], "count": 1, ...}], ...}
```

Garmin's 15-minute step and energy chunks covered the whole day, so
`prefer-raw` read them and not Garmin's daily total (which also says 23,687).
Only WHOOP writes a recovery, so its row names WHOOP whatever the `order`.

The Garmin night ranks first, so the WHOOP, Apple and Google Fit stages that
overlap it are dropped whole and every row names one source. Garmin writes a
night's HRV as two summaries, its average and its maximum; the entry's
`statistic` picks the average.

The tiles: the latest read (no `range`, no `bucket`, `reducers: ["last"]`)
answers the newest value of each metric from any source, with its instant:

```json
{"filter": {"properties": {"metric": {"in": ["health/body_mass", "health/body_fat_fraction", "health/vo2_max"]}}},
 "aggregate": {"groupBy": ["metric"], "reducers": ["last"]}}
```

```text
{"rows": [{"start": "2026-10-10T08:25:17+03:00", "group": {"metric": "health/body_fat_fraction"},
           "sources": ["googlefit/e2e/withings-body.4a91be20"],
           "values": [{"reducer": "last", "value": "27.1", "unit": "%"}], "count": 147, ...},
          {"start": "2026-10-10T08:25:17+03:00", "group": {"metric": "health/body_mass"},
           "sources": ["googlefit/e2e/withings-body.4a91be20"],
           "values": [{"reducer": "last", "value": "71.79", "unit": "kg"}], "count": 148, ...},
          {"start": "2026-10-11T10:24:00+03:00", "group": {"metric": "health/vo2_max"},
           "sources": ["applehealth/iphone/apple-watch"],
           "values": [{"reducer": "last", "value": "48.12", "unit": "mL/(kg.min)"}], "count": 66, ...}], ...}
```

A row's `start` is the value's instant at its own offset, and `count` is
every point the group holds, so a tile reads "71.79 kg, 10 October 08:25,
Withings". The latest read takes no `sources` policy: the newest value wins
whichever path delivered it.

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
   "sources": {"policy": "priority", "order": ["garmin/ada/account", "whoop/e2e", "applehealth/iphone/apple-watch"]},
   "reducers": ["last"],
   "rolling": {"buckets": 30, "reducers": ["avg", "stddev"], "through": "previous"}}}
```

```text
{"metric": "health/resting_heart_rate", "unit": "/min", "from": "summaries",
 "rows": [{"start": "2026-09-12T00:00:00+03:00", "end": "2026-09-13T00:00:00+03:00", "sources": ["garmin/ada/account"],
           "values": [{"reducer": "last", "value": "52", "unit": "/min"},
                      {"reducer": "avg", "of": "last", "rolling": 30, "n": 30, "value": "53.1666666666667", "unit": "/min"},
                      {"reducer": "stddev", "of": "last", "rolling": 30, "n": 30, "value": "3.32749488688145", "unit": "/min"}], ...},
          ...
          {"start": "2026-09-20T00:00:00+01:00", "end": "2026-09-21T00:00:00+01:00", ...,
           "values": [{"reducer": "last", "value": "52", ...},
                      {"reducer": "avg", "of": "last", "rolling": 30, "n": 30, "value": "52.9333333333333", ...},
                      {"reducer": "stddev", "of": "last", "rolling": 30, "n": 30, "value": "3.25508149752899", ...}], ...},
          ...
          {"start": "2026-10-11T00:00:00+03:00", "end": "2026-10-12T00:00:00+03:00", ...,
           "values": [{"reducer": "last", "value": "50", ...},
                      {"reducer": "avg", "of": "last", "rolling": 30, "n": 30, "value": "51.2", ...},
                      {"reducer": "stddev", "of": "last", "rolling": 30, "n": 30, "value": "1.86904610251683", ...}], ...}]}
```

On 2026-10-11 the band is 51.2 plus or minus 1.9, and the day's 50 sits
inside it. The London days are labelled at `+01:00`, each on the day Ada
lived it.

The window reaches before the range, so the first day's baseline is already
30 days deep. A short window against the long one (a 7-day mean against the
30-day band) is a second read with `"buckets": 7`: `rolling` takes one
window length per read. Taking the natural logarithm of each value before
the mean, as sport science does with RMSSD, is the app's.

## A night

A night screen shows the hypnogram, the stage totals and what the heart and
lungs did during it. Every device that recorded the night wrote a
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
   {"id": "garmin/ada/x4b2c9e1-6ab1b204/sleep",
    "properties": {"source": {"ref": ".../source/garmin/ada/account"}, "utcOffsetStart": 60, "scores": {"overall": "82"}, ...}},
   {"id": "googlefit/e2e/Sleep1790030340000",
    "properties": {"source": {"ref": ".../source/googlefit/e2e/google-pixel-watch-2.8d1f3c7a"}, "utcOffsetStart": 180, ...}},
   {"id": "whoop/e2e/0915234f-c9d5-4cd7-b894-ab927380b68c/sleep",
    "properties": {"source": {"ref": ".../source/whoop/e2e"}, "timeAsleep": "PT7H30M49.542S", "disturbances": 11,
                   "scores": {"sleepPerformance": "98", "sleepConsistency": "86"}, ...}}], ...}
```

Each record carries the night's span at its own offsets. Google Fit's says
`+03:00` in London, because Fit sends no offset and its adapter takes the
account's zone
([G-30](plans/observations/requirements.md#g-30-guessed-utc-offsets)).
Only Apple Health sends what `latency` is computed from.

The stage totals, inside one device's night. `bucket.records` makes each
night a bucket; its `sources` keeps the Garmin night where the devices
overlap, the request's `sources` keeps the Garmin stages inside it, and
`utcOffsets` writes the row at the night's own offsets:

```json
{"filter": {"kinds": ["ada.example.com/observations/observation"]},
 "aggregate": {
   "metric": {"ref": "ada.example.com/observations/metric/health/sleep_stage"},
   "range": {"gte": "2026-09-21T12:00:00+01:00", "lt": "2026-09-22T12:00:00+01:00"},
   "bucket": {"records": {
     "filter": {"kinds": ["ada.example.com/health/sleep"], "properties": {"isNap": {"eq": false}}},
     "sources": {"policy": "priority", "property": "source",
                 "order": ["garmin/ada/account", "whoop/e2e", "applehealth/iphone/apple-watch"]},
     "utcOffsets": {"start": "utcOffsetStart", "end": "utcOffsetEnd"}}},
   "groupBy": ["code"],
   "sources": {"policy": "priority", "order": ["garmin/ada/account", "whoop/e2e", "applehealth/iphone/apple-watch"]},
   "reducers": ["sum"],
   "unit": "min"}}
```

```text
{"metric": "health/sleep_stage", "unit": "min", "sources": ["garmin/ada/account"],
 "rows": [{"start": "2026-09-21T23:39:00+01:00", "end": "2026-09-22T07:24:00+01:00", "group": {"code": "awake"},
           "record": {"ref": "ada.example.com/health/sleep/garmin/ada/x4b2c9e1-6ab1b204/sleep", "version": 1},
           "values": [{"reducer": "sum", "value": "16.8666666666667", "unit": "min"}], "count": 3, ...},
          {..., "group": {"code": "deep"}, "values": [{"reducer": "sum", "value": "91.6166666666667", ...}], "count": 5, ...},
          {..., "group": {"code": "light"}, "values": [{"reducer": "sum", "value": "247.516666666667", ...}], "count": 10, ...},
          {..., "group": {"code": "rem"}, "values": [{"reducer": "sum", "value": "109", ...}], "count": 5, ...}], ...}
```

A share is a stage over the night's sum (deep: 91.6 of 465 minutes, 20
percent). The same read over a month makes a row per night and code, one
device's night each.

The hypnogram is the stage observations themselves, one device's, in time
order:

```http
GET /api/v1/records?filter={"kinds": ["ada.example.com/observations/observation"],
                            "properties": {"metric": {"eq": "health/sleep_stage"}, "source": {"eq": "garmin/ada/account"},
                                           "at": {"gte": "2026-09-21T20:00:00+01:00", "lt": "2026-09-22T10:00:00+01:00"}}}
                   &orderBy=at&first=100
```

```text
{"records": [
   {"id": "garmin/ada/x4b2c9e1-6ab1b204/stage/1790030340",
    "properties": {"code": "awake", "at": "2026-09-21T22:39:00Z", "endsAt": "2026-09-21T22:52:00Z",
                   "value": {"value": "13", "unit": "min"}, "utcOffset": 60, ...}},
   {"id": "garmin/ada/x4b2c9e1-6ab1b204/stage/1790031120",
    "properties": {"code": "light", "at": "2026-09-21T22:52:00Z", "endsAt": "2026-09-21T23:24:16Z", ...}},
   {"id": "garmin/ada/x4b2c9e1-6ab1b204/stage/1790033056",
    "properties": {"code": "deep", "at": "2026-09-21T23:24:16Z", "endsAt": "2026-09-21T23:56:46Z", ...}},
   ...], ...}
```

The night has 23 stages. A list read whose filter bounds `at` on both ends
is the window read, which orders by `at` alone; that is the order a
hypnogram wants.

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
                 "order": ["garmin/ada/account", "whoop/e2e", "applehealth/iphone/apple-watch"]},
     "utcOffsets": {"start": "utcOffsetStart", "end": "utcOffsetEnd"}}},
   "sources": {"policy": "priority",
               "order": ["garmin/ada/account", "applehealth/iphone/apple-watch", "applehealth/iphone/health"]}}}
```

```text
{"units": {"health/heart_rate": "/min", "health/respiratory_rate": "/min", "health/spo2": "%"},
 "rows": [{"start": "2026-09-21T23:39:00+01:00", "end": "2026-09-22T07:24:00+01:00",
           "group": {"metric": "health/heart_rate"}, "record": {"ref": ".../health/sleep/garmin/ada/x4b2c9e1-6ab1b204/sleep", ...},
           "sources": ["applehealth/iphone/apple-watch", "garmin/ada/account", "googlefit/e2e/google-pixel-watch-2.8d1f3c7a"],
           "values": [{"reducer": "min", "value": "44", "unit": "/min"},
                      {"reducer": "avg", "value": "51.1075268817204", "unit": "/min"},
                      {"reducer": {"percentile": 0.05}, "value": "45.6", "unit": "/min"}], "count": 93, ...},
          {..., "group": {"metric": "health/respiratory_rate"}, "sources": ["garmin/ada/account"],
           "values": [{"reducer": "avg", "value": "14.4154838709677", "unit": "/min"}], "count": 31, ...},
          {..., "group": {"metric": "health/spo2"}, "sources": ["garmin/ada/account"],
           "values": [{"reducer": "min", "value": "95", "unit": "%"},
                      {"reducer": "avg", "value": "96.7872340425532", "unit": "%"}], "count": 47, ...}], ...}
```

Every point the heart rate row reduces is Garmin's: with the source
filtered to `garmin/ada/account` the row answers the same `count` 93, `min`
44, `avg` and 5th percentile. Garmin samples every five minutes and each
value holds for less than that, so the Apple Watch and the Pixel Watch add
only held time in the gaps. That is why the row lists all three sources,
and it moves `coverage` from 6 hours 12 minutes (Garmin alone) to 7 hours
23 minutes 11 seconds.

No reducer answers when the lowest heart rate happened. A downsample to one
column does: its `min` carries the instant `at` it was taken.

```json
{"filter": {"properties": {"source": {"eq": "garmin/ada/account"}, "channel": {"exists": false}}},
 "aggregate": {"metric": {"ref": "ada.example.com/observations/metric/health/heart_rate"},
               "range": {"gte": "2026-09-21T23:39:00+01:00", "lt": "2026-09-22T07:24:00+01:00"}},
 "downsample": {"points": 1}}
```

```text
{"bucket": {"every": "PT8H", "origin": "2026-09-21T22:30:00Z"},
 "rows": [{"start": "2026-09-21T23:39:00+01:00", "end": "2026-09-22T07:24:00+01:00",
           "values": [{"reducer": "first", "value": "58", "at": "2026-09-21T23:40:00+01:00", ...},
                      {"reducer": "last", "value": "56", "at": "2026-09-22T07:20:00+01:00", ...},
                      {"reducer": "min", "value": "44", "at": "2026-09-22T06:30:00+01:00", ...},
                      {"reducer": "max", "value": "61", "at": "2026-09-22T02:40:00+01:00", ...}], "count": 93, ...}], ...}
```

The lowest heart rate of the night, 44, came at 06:30, under an hour before
waking.

A sleep trend counts each night on the day Ada woke up, at the offset she
woke at. A property read over the sleep records does it, one device's night
where several recorded it:

```json
{"filter": {"kinds": ["ada.example.com/health/sleep"], "properties": {"isNap": {"eq": false}}},
 "aggregate": {
   "property": {"name": "timeAsleep", "anchor": "endsAt", "utcOffset": "utcOffsetEnd",
                "sources": {"policy": "priority", "property": "source",
                            "order": ["garmin/ada/account", "whoop/e2e", "applehealth/iphone/apple-watch"]}},
   "range": {"gte": "2026-09-13T00:00:00+03:00", "lt": "2026-10-04T00:00:00+03:00"},
   "bucket": {"calendar": "week", "weekStart": "sunday"},
   "reducers": ["avg", "stddev", "count"],
   "unit": "h"}}
```

```text
{"property": "timeAsleep", "unit": "h", "bucket": {"calendar": "week", "weekStart": "sunday"},
 "rows": [{"start": "2026-09-13T00:00:00+03:00", "end": "2026-09-20T00:00:00+03:00",
           "values": [{"reducer": "avg", "value": "7.6954761904761904762", "unit": "h"},
                      {"reducer": "stddev", "value": "0.49591331919621863732", "unit": "h"},
                      {"reducer": "count", "value": "7"}], ...},
          {"start": "2026-09-20T00:00:00+01:00", "end": "2026-09-27T00:00:00+03:00",
           "values": [{"reducer": "avg", "value": "7.6441666666666666667", ...}, ...], ...},
          {"start": "2026-09-27T00:00:00+03:00", "end": "2026-10-04T00:00:00+03:00",
           "values": [{"reducer": "avg", "value": "7.7023015873015873016", ...}, ...], ...}], ...}
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
                   "duration": "PT36M43S", "distance": {"value": "5.945", "unit": "km"},
                   "activeEnergy": {"value": "377.2", "unit": "kcal"}, "averageHeartRate": {"value": "142", "unit": "/min"}, ...}},
   {"id": "garmin/ada/21100007000/workout",
    "properties": {"source": {"ref": ".../source/garmin/ada/forerunner265"}, "sport": "running", "sportSource": "RUNNING",
                   "at": "2026-09-22T06:47:00Z", "endsAt": "2026-09-22T07:24:00Z", "utcOffsetStart": 60,
                   "duration": "PT37M", "distance": {"value": "6007.81", "unit": "m"},
                   "activeEnergy": {"value": "368", "unit": "kcal"}, "averageHeartRate": {"value": "143", "unit": "/min"},
                   "maxHeartRate": {"value": "159", "unit": "/min"}, "elevationGain": {"value": "35", "unit": "m"}, ...}},
   {"id": "googlefit/e2e/strava-15179005962",
    "properties": {"source": {"ref": ".../source/googlefit/e2e/com-strava"}, "sport": "running", "sportSource": "8", "duration": "PT35M53S", ...}},
   {"id": "whoop/e2e/c953c21f-cb2c-403a-964a-33b0c2e58afd/workout",
    "properties": {"source": {"ref": ".../source/whoop/e2e"}, "sport": "running", "strain": "10.6036",
                   "totalEnergy": {"value": "1620.28426", "unit": "kJ"},
                   "zoneDurations": {"zone3": "PT29M29.83S", "zone4": "PT4M29.973S", ...}, ...}}], ...}
```

Every adapter maps its provider's word onto one `sport` value and keeps the
word in `sportSource`. Garmin's run is 37 minutes over 6.008 km, a mean pace
of about 6:10 per kilometre.

The time in zones, over one device's run: the records mode with
`match: ["sport"]` keeps the Garmin run where the devices recorded the same
run, and `durationIn` sums the time heart rate held inside each band. The
bands here are 50, 60, 70, 80 and 90 percent of a maximum of 188; the zones
in force on the day are the app's to keep, since no record holds them.

```json
{"aggregate": {
   "metric": {"ref": "ada.example.com/observations/metric/health/heart_rate"},
   "range": {"gte": "2026-09-22T00:00:00+01:00", "lt": "2026-09-23T00:00:00+01:00"},
   "bucket": {"records": {
     "filter": {"kinds": ["ada.example.com/health/workout"], "properties": {"sport": {"eq": "running"}}},
     "sources": {"policy": "priority", "property": "source", "match": ["sport"],
                 "order": ["garmin/ada/forerunner265", "applehealth/iphone/apple-watch", "whoop/e2e"]},
     "utcOffsets": {"start": "utcOffsetStart", "end": "utcOffsetEnd"}}},
   "sources": {"policy": "priority", "order": ["garmin/ada/forerunner265", "applehealth/iphone/apple-watch"]},
   "reducers": [{"durationIn": [94, 113]}, {"durationIn": [113, 132]}, {"durationIn": [132, 150]},
                {"durationIn": [150, 169]}, {"durationIn": [169, 220]}, "avg", "max"],
   "maxGap": "PT2M",
   "unit": "min"}}
```

```text
{"metric": "health/heart_rate", "unit": "/min", "sources": ["garmin/ada/forerunner265"],
 "rows": [{"start": "2026-09-22T07:47:00+01:00", "end": "2026-09-22T08:24:00+01:00",
           "record": {"ref": "ada.example.com/health/workout/garmin/ada/21100007000/workout", "version": 1},
           "values": [{"reducer": {"durationIn": [94, 113]}, "value": "1.5", "unit": "min"},
                      {"reducer": {"durationIn": [113, 132]}, "value": "1.5", "unit": "min"},
                      {"reducer": {"durationIn": [132, 150]}, "value": "25.5", "unit": "min"},
                      {"reducer": {"durationIn": [150, 169]}, "value": "8.5", "unit": "min"},
                      {"reducer": {"durationIn": [169, 220]}, "value": "0", "unit": "min"},
                      {"reducer": "avg", "value": "143.081081081081", "unit": "/min"},
                      {"reducer": "max", "value": "159", "unit": "/min"}],
           "count": 74, "coverage": "PT37M", "nonAdditive": true, ...}], ...}
```

The zones add up to the run's 37 minutes. WHOOP's own `zoneDurations` on its
record are WHOOP's computation from its own heart rate and differ (29.5
minutes in its zone 3).

The curves, a minute at a time from the start to a few minutes past the end,
heart rate and speed in one read; the minutes after the end are the recovery:

```json
{"aggregate": {
   "metrics": [
     {"ref": "ada.example.com/observations/metric/health/heart_rate", "reducers": ["avg"]},
     {"ref": "ada.example.com/observations/metric/health/speed", "reducers": ["avg"], "unit": "km/h"}],
   "range": {"gte": "2026-09-22T07:47:00+01:00", "lt": "2026-09-22T08:35:00+01:00"},
   "bucket": {"every": "PT1M"},
   "sources": {"policy": "priority", "order": ["garmin/ada/forerunner265", "garmin/ada/account"]}}}
```

```text
{"units": {"health/heart_rate": "/min", "health/speed": "km/h"}, "bucket": {"every": "PT1M"},
 "rows": [{"start": "2026-09-22T07:47:00+01:00", "group": {"metric": "health/heart_rate"}, "sources": ["garmin/ada/forerunner265"],
           "values": [{"reducer": "avg", "value": "103.5", "unit": "/min"}], "count": 2, ...},
          {"start": "2026-09-22T07:47:00+01:00", "group": {"metric": "health/speed"},
           "values": [{"reducer": "avg", "value": "7.317", "unit": "km/h"}], "count": 2, ...},
          ...
          {"start": "2026-09-22T07:53:00+01:00", "group": {"metric": "health/heart_rate"}, "values": [{"value": "155", ...}], ...},
          {"start": "2026-09-22T07:53:00+01:00", "group": {"metric": "health/speed"}, "values": [{"value": "10.4724", ...}], ...},
          ...
          {"start": "2026-09-22T08:24:00+01:00", "group": {"metric": "health/heart_rate"}, "values": [{"value": "109", ...}], "count": 1, ...},
          {"start": "2026-09-22T08:25:00+01:00", "group": {"metric": "health/heart_rate"}, "values": [{"value": null, ...}], "count": 0, ...},
          ...
          {"start": "2026-09-22T08:30:00+01:00", "group": {"metric": "health/heart_rate"}, "sources": ["garmin/ada/account"],
           "values": [{"reducer": "avg", "value": "92", "unit": "/min"}], "count": 1, ...}, ...], ...}
```

The watch's activity stream stops at the session's end, and its last value
holds a few minutes, so `avg` answers `null` in minutes with no reading.
After that the all-day stream, which samples every five minutes, takes over
by priority: 92 per minute at 08:30, six minutes after 109 at the end. A
one-minute recovery needs a stream that keeps sampling densely after the end.

Pace is the app's arithmetic: the unit table has a `pace` measure, but a
speed read does not answer in it, and no adapter writes `health/pace`. A
minute's pace is 60 over its speed in km/h, and the run's mean pace is its
`duration` over its `distance`, never a mean of the minutes' paces. Splits
per kilometre have no read either: Garmin writes the run's distance as an
amount trace, which the app cuts at each kilometre
([R-WORK-3](plans/observations/requirements.md#apps-workouts)).

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
                            "order": ["garmin/ada/forerunner265", "whoop/e2e", "applehealth/iphone/apple-watch"]}},
   "range": {"gte": "2026-09-13T00:00:00+03:00", "lt": "2026-10-11T00:00:00+03:00"},
   "bucket": {"calendar": "week", "weekStart": "sunday"},
   "reducers": ["sum", "count"],
   "unit": "h"}}
```

```text
{"property": "duration", "unit": "h", "bucket": {"calendar": "week", "weekStart": "sunday"},
 "rows": [{"start": "2026-09-13T00:00:00+03:00", "end": "2026-09-20T00:00:00+03:00", "group": {"properties": {"sport": "running"}},
           "values": [{"reducer": "sum", "value": "2.8833333333333333333", "unit": "h"}, {"reducer": "count", "value": "3"}], ...},
          {"start": "2026-09-13T00:00:00+03:00", ..., "group": {"properties": {"sport": "strength"}},
           "values": [{"reducer": "sum", "value": "0.95", ...}, {"reducer": "count", "value": "1"}], ...},
          ...
          {"start": "2026-09-20T00:00:00+01:00", "end": "2026-09-27T00:00:00+01:00", "group": {"properties": {"sport": "running"}},
           "values": [{"reducer": "sum", "value": "0.61666666666666666667", ...}, {"reducer": "count", "value": "1"}], ...},
          ...
          {"start": "2026-10-04T00:00:00+03:00", ..., "group": {"properties": {"sport": "cycling"}},
           "values": [{"reducer": "sum", "value": "1.1", ...}, {"reducer": "count", "value": "1"}], ...}, ...], ...}
```

Four devices recorded each run; each counts once. Each sport's row of a
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
                            "order": ["garmin/ada/forerunner265", "whoop/e2e", "applehealth/iphone/apple-watch"]}},
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
           "values": [{"reducer": "sum", "value": "38", "unit": "min"},
                      {"reducer": "avg", "of": "sum", "rolling": 7, "n": 7, "value": "32.5714285714286", "unit": "min"}], ...},
          {"start": "2026-09-30T00:00:00+03:00", ..., "synthetic": true, "count": 0,
           "values": [{"reducer": "sum", "value": "0", ...}, {"reducer": "avg", "of": "sum", "rolling": 7, "n": 7, "value": "33", ...}]},
          ...
          {"start": "2026-10-11T00:00:00+03:00", ...,
           "values": [{"reducer": "sum", "value": "99", ...}, {"reducer": "avg", "of": "sum", "rolling": 7, "n": 7, "value": "47", ...}], ...}]}
```

The same read with `"buckets": 28` answers 34.6071428571429 on 2026-10-11:
an acute load of 47 minutes a day against a chronic 34.6, a ratio of 1.36.

An exponentially weighted load (fitness, fatigue and form) is the app's,
computed from the daily rows.

## Steps across travel

A step count is per local day, and on a trip the local day moves. In the
recorded-local calendar each value lands on the date its own UTC offset
names, so the day Ada flew to London lasts 26 hours and her last London day
22. Each row says which source and which points served it:

```json
{"aggregate": {
   "metric": {"ref": "ada.example.com/observations/metric/health/steps"},
   "from": "prefer-raw",
   "range": {"gte": "2026-09-19T00:00:00+03:00", "lt": "2026-09-26T00:00:00+03:00"},
   "bucket": {"calendar": "day", "fallbackTimezone": "Europe/Athens"},
   "sources": {"policy": "priority",
               "order": ["garmin/ada/account", "applehealth/iphone/apple-watch", "applehealth/iphone/iphone"]},
   "reducers": ["sum"]}}
```

```text
{"metric": "health/steps", "unit": "{steps}", "bucket": {"calendar": "day", "fallbackTimezone": "Europe/Athens"},
 "from": "prefer-raw",
 "rows": [{"start": "2026-09-19T00:00:00+03:00", "end": "2026-09-20T00:00:00+03:00", "from": "raw", "sources": ["garmin/ada/account"],
           "values": [{"reducer": "sum", "value": "8142", "unit": "{steps}"}], "count": 96, "coverage": "P1D", ...},
          {"start": "2026-09-20T00:00:00+03:00", "end": "2026-09-21T00:00:00+01:00",
           "values": [{"reducer": "sum", "value": "12469", ...}], "count": 104, "coverage": "P1DT2H", ...},
          {"start": "2026-09-21T00:00:00+01:00", "end": "2026-09-22T00:00:00+01:00",
           "values": [{"reducer": "sum", "value": "9997", ...}], "count": 96, ...},
          ...
          {"start": "2026-09-24T00:00:00+01:00", "end": "2026-09-25T00:00:00+01:00",
           "values": [{"reducer": "sum", "value": "15935", ...}], "count": 88, "coverage": "PT22H", ...},
          {"start": "2026-09-25T00:00:00+03:00", "end": "2026-09-26T00:00:00+03:00",
           "values": [{"reducer": "sum", "value": "8689", ...}], ...}], ...}
```

The 2026-09-24 row holds 22 hours of points (its `coverage`), though its
`end` reads London's midnight: the next row starts at Athens's midnight, two
hours earlier, and each row's bounds are written at the offsets of its own
values.

`fallbackTimezone` places a value written without an offset by that zone's
clock and counts it in the row's `offsetless`; without it, a stream holding
such a value refuses the read. The same read in a named zone
(`"timezone": "Europe/Athens"`) cuts every day at Athens midnight, as a home
screen that ignores travel would:

```text
{"bucket": {"calendar": "day", "timezone": "Europe/Athens"},
 "rows": [...,
          {"start": "2026-09-20T00:00:00+03:00", "end": "2026-09-21T00:00:00+03:00", "values": [{"reducer": "sum", "value": "12339", ...}], ...},
          {"start": "2026-09-21T00:00:00+03:00", "end": "2026-09-22T00:00:00+03:00", "values": [{"reducer": "sum", "value": "9997", ...}], ...},
          {"start": "2026-09-22T00:00:00+03:00", "end": "2026-09-23T00:00:00+03:00", "values": [{"reducer": "sum", "value": "19979", ...}], ...},
          ...
          {"start": "2026-09-24T00:00:00+03:00", "end": "2026-09-25T00:00:00+03:00", "values": [{"reducer": "sum", "value": "16025", ...}], ...}, ...]}
```

In the Athens read each London day starts and ends two hours early, at
22:00 London time: 2026-09-22 reads 19,979 steps there and 19,987 on the day
Ada lived.

Google Fit sends no offset, so its adapter stamps each value with the
account's zone, Athens, even in London
([G-30](plans/observations/requirements.md#g-30-guessed-utc-offsets)); the
`order` above ranks it after the sources that recorded where Ada was.

## Weight in kg and lb

Weight reads in any mass unit, and the read converts each stored value.
`exact` is `false` where any value came from a float64 reduction or a
conversion that rounds ([the response](api.md#the-response)). In kg below
it is `false` because of `avg`: no value needs converting, so `min` and
`max` answer the stored digits. Weekly, the Withings scale's weigh-ins by
one path, and the ones Ada typed in by hand:

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

The read names one of the scale's paths because the Withings scale reaches
the repository through Apple Health, Google Fit and Garmin as three sources
with nothing linking them
([G-31](plans/observations/requirements.md#g-31-one-device-under-several-sources)).
`separate` shows each weigh-in three times, and `priority` keeps a weigh-in
again where a lower-ranked path stamped it a few seconds before the
higher-ranked one: ranking Apple Health, then Google Fit, then Garmin counts
10 values for the 4 weigh-ins of the week of 2026-09-07.

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
   {"id": "garmin/ada/x4b2c9e1-6ab8aec4/systolic_blood_pressure",
    "properties": {"at": "2026-09-27T05:51:00Z", "value": {"value": "118", "unit": "mm[Hg]"}, "source": {"ref": ".../source/garmin/ada/account"},
                   "components": [{"metric": {"ref": ".../metric/health/systolic_blood_pressure"}, "value": {"value": "118", "unit": "mm[Hg]"}},
                                  {"metric": {"ref": ".../metric/health/diastolic_blood_pressure"}, "value": {"value": "68", "unit": "mm[Hg]"}}],
                   "recordingMethod": "automatic", ...}},
   {"id": "googlefit-bdc260a3bb2334ce971b24bf9b102a7a",
    "properties": {"at": "2026-09-27T05:51:00Z", "value": {"value": "121", ...}, "source": {"ref": ".../source/googlefit/e2e/withings-bpm-connect.7c03d5e1"}, ...}},
   {"id": "applehealth/iphone/withings/systolic_blood_pressure/2026-09-27T05:51:30Z",
    "properties": {"at": "2026-09-27T05:51:30Z", "value": {"value": "118", ...}, "source": {"ref": ".../source/applehealth/iphone/withings"}, ...}}], ...}
```

One Sunday reading reached the repository three times, once through each
provider (the synthetic data gives each provider its own device noise), with
nothing linking them
([G-31](plans/observations/requirements.md#g-31-one-device-under-several-sources)).

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
          ...
          {..., "group": {"metric": "health/systolic_blood_pressure", "source": "applehealth/iphone/withings"},
           "values": [{"reducer": "avg", "value": "114.666666666667", "unit": "mm[Hg]"}, {"reducer": "max", "value": "118", ...}], "count": 3, ...},
          ...], ...}
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
in the week of the morning it ended:

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
          {..., "sources": ["garmin/ada/account"],
           "values": [{"reducer": "avg", "value": "51.8571428571429", ...}, {"reducer": "min", "value": "50", ...},
                      {"reducer": "max", "value": "54", ...}, {"reducer": "count", "value": "7"}], ...},
          {..., "sources": ["whoop/e2e"],
           "values": [{"reducer": "avg", "value": "50.7142857142857", ...}, {"reducer": "min", "value": "46", ...},
                      {"reducer": "max", "value": "53", ...}, {"reducer": "count", "value": "7"}], ...}], ...}
```

Each provider computes its own resting heart rate, so the three disagree by
a beat: these are three methods, not three readings of one value.

Merged, one source per day: the same read with
`"sources": {"policy": "priority", "order": ["garmin/ada/account", "whoop/e2e", "applehealth/iphone/apple-watch"]}`.
A summary of a lower source is dropped where it overlaps a higher one's:

```text
{"sources": ["garmin/ada/account"],
 "rows": [{"start": "2026-10-05T00:00:00+03:00", "end": "2026-10-12T00:00:00+03:00", "sources": ["garmin/ada/account"],
           "values": [{"reducer": "avg", "value": "51.8571428571429", ...}, {"reducer": "min", "value": "50", ...},
                      {"reducer": "max", "value": "54", ...}, {"reducer": "count", "value": "7"}], ...}], ...}
```

Without `order`, `priority` ranks the sources by each source record's
`priorities` for the metric, so an app that stores the user's choice there
sends no order at all.

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
 "aggregate": {"groupBy": ["metric", "source", "about", "channel", "statistic"], "reducers": ["last"],
               "budget": {"maxGroups": 2000}}}
```

```text
{"rows": [...,
          {"start": "2026-10-11T08:28:00+03:00", "group": {"metric": "health/hrv_rmssd", "source": "garmin/ada/account"},
           "form": "gauge", "values": [{"reducer": "last", "value": "58", "unit": "ms"}], "count": 8633, ...},
          {"start": "2026-10-11T00:13:00+03:00", "end": "2026-10-11T08:28:00+03:00",
           "group": {"metric": "health/hrv_rmssd", "source": "garmin/ada/account", "statistic": "avg"},
           "form": "summary", "values": [{"reducer": "last", "value": "62", "unit": "ms"}], "count": 90, ...},
          {"start": "2026-10-10T21:31:55+03:00",
           "group": {"metric": "health/hrv_rmssd", "source": "garmin/ada/account", "channel": "snapshot"},
           "form": "gauge", "values": [{"reducer": "last", "value": "60.2", "unit": "ms"}], "count": 288, ...},
          ...,
          {"start": "2026-10-11T23:45:00+03:00", "end": "2026-10-12T00:00:00+03:00",
           "group": {"metric": "health/steps", "source": "garmin/ada/account"},
           "form": "amount", "values": [{"reducer": "last", "value": "0", "unit": "{steps}"}], "count": 8640, ...},
          {"start": "2026-10-11T00:00:00+03:00", "end": "2026-10-12T00:00:00+03:00",
           "group": {"metric": "health/steps", "source": "garmin/ada/account", "statistic": "sum"},
           "form": "summary", "values": [{"reducer": "last", "value": "23687", "unit": "{steps}"}], "count": 90, ...}, ...],
 "sources": [16 source paths], ...}
```

The demo answers 124 streams from 16 sources. A summary stream with two
statistics is two rows, and a `channel` (Garmin's weekly health snapshot)
is a stream of its own.

The newest value of an amount is one sample (15 minutes of steps), not a
day's total; a day's total is the [today](#today) read.

## Incremental sync

An app that keeps its own copy of the screens it drew re-reads only what
changed. Every read answers the changelog position it was served at, in
`head` (a steps read answered `"head": {"changelog": 107096, "generation":
"d6tchnk5ub2w"}`), and the change feed resumes from there, naming each
record a later write touched:

```bash
curl -sN "$SUBSTRATE_SERVER/api/v1/changes?watch=1&from=107096&generation=d6tchnk5ub2w&values=1&kinds=ada.example.com/observations/observation,ada.example.com/observations/trace,ada.example.com/health/sleep,ada.example.com/health/workout" \
  -H "Authorization: Bearer $SUBSTRATE_TOKEN"
```

```text
{"bookmark": 107096, "generation": "d6tchnk5ub2w"}
{"seq": 107097, "op": "put", "kind": "ada.example.com/observations/trace",
 "recordId": "applehealth/iphone/apple-watch/steps/2026-08-26T19", "actor": "function:ada.example.com:healthapple:adaptapplehealth",
 "payload": {"created": true, ...},
 "affected": [{"kind": "ada.example.com/observations/trace", "id": "applehealth/iphone/apple-watch/steps/2026-08-26T19", "version": 1,
               "properties": [{"name": "at", "after": "2026-08-26T19:00:00Z"},
                              {"name": "endsAt", "after": "2026-08-26T20:00:00Z"},
                              {"name": "metric", "after": {"ref": "ada.example.com/observations/metric/health/steps"}},
                              {"name": "source", "after": {"ref": ".../source/applehealth/iphone/apple-watch"}},
                              {"name": "utcOffsetStart", "after": 180}, ...]}]}
...
```

The head came from a read taken while the Apple Health adapter was still
writing its backlog: 39 changes arrived in the next few seconds, the first
an hour of Apple Watch steps on 2026-08-26, so an app showing that day's
steps reads it again.

The stream opens with a bookmark and sends one line per change. With
`values=1` a line names only the properties the write moved, before and
after. A creation moves every property, so its `metric`, `source`, `at` and
`endsAt` tell the app which days to read again. An update names only what
changed: a rescore, or a re-delivered hour of a trace, that changes `value`
or `values` names neither `metric` nor `at`. To place an update the app
reads the record at its path, the id percent-encoded
(`GET /api/v1/ada.example.com/observations/trace/applehealth%2Fiphone%2Fapple-watch%2Fsteps%2F2026-08-26T19`),
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
{"filter": {"properties": {"source": {"eq": "garmin/ada/account"}, "channel": {"exists": false}}},
 "aggregate": {"metric": {"ref": "ada.example.com/observations/metric/health/heart_rate"},
               "range": {"gte": "2026-09-20T00:00:00+03:00", "lt": "2026-09-27T00:00:00+03:00"}},
 "downsample": {"points": 400}}
```

```text
{"metric": "health/heart_rate", "unit": "/min", "bucket": {"every": "PT30M", "origin": "2026-09-19T21:00:00Z"},
 "rows": [{"start": "2026-09-20T00:00:00+03:00", "end": "2026-09-20T00:30:00+03:00",
           "values": [{"reducer": "first", "value": "71", "unit": "/min", "at": "2026-09-20T00:00:00+03:00"},
                      {"reducer": "last", "value": "69", "unit": "/min", "at": "2026-09-20T00:25:00+03:00"},
                      {"reducer": "min", "value": "64", "unit": "/min", "at": "2026-09-20T00:05:00+03:00"},
                      {"reducer": "max", "value": "71", "unit": "/min", "at": "2026-09-20T00:00:00+03:00"}], "count": 6, ...},
          ...], ...}
```

The week's 2,016 points come back as 336 columns: 400 columns would be 25.2
minutes wide, and the read widens a column to whole quarter hours so it is
served from the rollup rows.

## Two units on one chart

A chart with two axes, heart rate beside steps by the hour, is one
`metrics` read. The response's `units` names each metric's unit, and each
value carries its own:

```json
{"filter": {"properties": {"source": {"eq": "garmin/ada/account"}, "channel": {"exists": false}}},
 "aggregate": {
   "metrics": [
     {"ref": "ada.example.com/observations/metric/health/heart_rate", "reducers": ["avg", "max"]},
     {"ref": "ada.example.com/observations/metric/health/steps", "reducers": ["sum"]}],
   "range": {"gte": "2026-09-22T06:00:00+01:00", "lt": "2026-09-22T12:00:00+01:00"},
   "bucket": {"every": "PT1H"}}}
```

```text
{"units": {"health/heart_rate": "/min", "health/steps": "{steps}"}, "bucket": {"every": "PT1H"},
 "rows": [...,
          {"start": "2026-09-22T07:00:00+01:00", "end": "2026-09-22T08:00:00+01:00", "group": {"metric": "health/heart_rate"},
           "values": [{"reducer": "avg", "value": "74.5", "unit": "/min"}, {"reducer": "max", "value": "153", "unit": "/min"}], "count": 12, ...},
          {"start": "2026-09-22T07:00:00+01:00", "end": "2026-09-22T08:00:00+01:00", "group": {"metric": "health/steps"},
           "values": [{"reducer": "sum", "value": "2298", "unit": "{steps}"}], "count": 4, ...},
          {"start": "2026-09-22T08:00:00+01:00", ..., "group": {"metric": "health/heart_rate"},
           "values": [{"reducer": "avg", "value": "110.166666666667", ...}, {"reducer": "max", "value": "145", ...}], ...},
          {"start": "2026-09-22T08:00:00+01:00", ..., "group": {"metric": "health/steps"},
           "values": [{"reducer": "sum", "value": "4262", ...}], ...}, ...], ...}
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
- An owner's correction to a record an adapter wrote is undone by the
  adapter's next delivery
  ([C-1](plans/observations/requirements.md#c-1-owner-corrections-undone-by-replays)).
