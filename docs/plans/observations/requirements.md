# Health app requirements and where each stands

Written 2026-10-13, after the review of the observations build (pull request
916). It condenses two requirement catalogues the review wrote: one of what
apps like WHOOP, Oura, Apple Health, Garmin Connect and Strava show their
users (`R-*` ids), and one of the query and data-model primitives health and
time-series platforms give developers (`P-*` ids). Each requirement keeps its
catalogue id, priority and sources, and states where it stands after the
review's fixes landed:

- **supported**: a request answers it today, named in the row. Where a recipe
  in [building health apps](../../health-apps.md) shows the request, the row
  links it; those requests were run against a demo repository holding 90
  days from four providers.
- **partly**: the row says what an app still has to do itself or cannot do.
- **follow-up**: nothing answers it yet; the row names the item under
  [follow-ups](#follow-ups) that holds the proposal.
- **out of scope**: a decision or the build's scope rules it out, named in
  the row.

Priorities are the catalogues': **must** (two or more reference apps depend
on it, or a number shown without it is wrong), **should** (one major app
depends on it, or apps work around its absence with client code), **could**
(niche). `G-*` ids are the review's gap list and `C-*` ids its completeness
critic; both are summarized under [follow-ups](#follow-ups) where they stay
open. Source keys in square brackets resolve under [sources](#sources).

Of the 79 app requirements, 39 are supported, 31 partly, 8 are follow-ups
and 1 is out of scope. Of the 59 platform primitives, 42 are supported, 13
partly, 2 are follow-ups and 2 are out of scope.

## Apps: time and calendars

| Id | Need | Status | Request, or what is missing |
| --- | --- | --- | --- |
| R-TIME-1 | Daily values on the local day of each value's own UTC offset (must) [S30] [S33] | supported | `bucket: {"calendar": "day"}` with no `timezone`, the recorded-local calendar; each row's `start` and `end` carry the offsets its values were taken at, so a travel day reads 26 hours. [Steps across travel](../../health-apps.md#steps-across-travel) |
| R-TIME-2 | Weeks, months and years in a named zone, across DST, with a week start (must) [S8] [S28] [S30] | supported | `bucket: {"calendar": "week", "timezone": "Europe/Athens", "weekStart": "sunday"}`; a day across a clock change is 23 or 25 hours. [Weekly load](../../health-apps.md#weekly-load) |
| R-TIME-3 | A sleep counted on the day the user woke up (must) [S6] [S32] | supported | A metric read takes `bucket.place: "end"`; a property read over `health/sleep` takes `anchor: "endsAt"` with `utcOffset: "utcOffsetEnd"`. Oura's 18:00 day boundary is not a request. [A night](../../health-apps.md#a-night) |
| R-TIME-4 | Vendor days and physiological cycles as buckets (should) | partly | The records mode makes each WHOOP cycle mirror record a bucket, and `bucket.offset` starts a day later (`PT4H`). A day that starts the evening before and is labelled with the next date has no request: `offset` is never negative. |
| R-TIME-5 | Values with no offset, and where the user was (must) [S32] [S40] | partly | A recorded-local read of a stream holding a value with no offset is a `422` unless `bucket.fallbackTimezone` places it, and each row counts those values in `offsetless`. Nothing infers an offset or records where the user was: [G-30](#g-30-guessed-utc-offsets), [G-33](#g-33-kinds-an-app-writes-about-the-person). |

## Apps: aggregation

| Id | Need | Status | Request, or what is missing |
| --- | --- | --- | --- |
| R-AGG-1 | Daily totals of steps, energy, distance, floors (must) [S30] | supported | `reducers: ["sum"]` over amounts. In the `every` and calendar modes an amount lands whole in the bucket its start (or, under `place: "end"`, its end) lies in; the records mode prorates an amount a window cuts and counts it in `prorated`. [Today](../../health-apps.md#today) |
| R-AGG-2 | Time-weighted gauge statistics per bucket (must) | supported | `twa`, with each value's `hold` and the request's `maxGap`; `min`, `max`, `count`. |
| R-AGG-3 | Intraday charts at screen resolution (must) | supported | `bucket: {"every": "PT5M"}`, or `downsample={"points": 800}`, whose `first`, `last`, `min` and `max` each carry the instant `at` they were taken. [A chart-sized series](../../health-apps.md#a-chart-sized-series) |
| R-AGG-4 | Latest-value tiles (must) [S8] [S16] | supported | The latest read: no `range`, no `bucket`, `reducers: ["last"]`; grouped by `metric` it answers each metric's newest value from any source, by `metric` and `source` each stream's. [Today](../../health-apps.md#today) |
| R-AGG-5 | Several metrics aligned on one day axis (must) [S4] [S9] [S37] | supported | `aggregate.metrics`, up to 16 metrics over one range and bucket; in the recorded-local calendar the rows of one bucket share its bounds. [Today](../../health-apps.md#today) |
| R-AGG-6 | Spread: standard deviation and percentiles (should) [S35] | supported | `stddev` and `{"percentile": q}` on gauges and amounts; `stddev` on the property read and as a rolling reducer. A read from summaries takes neither inside a bucket. |
| R-AGG-7 | Time and share of readings inside threshold ranges (must) [S24] [S34] | supported | `{"durationIn": [lo, hi]}`, one per band, in one read; a band's share is its duration over the row's `coverage`, which the app divides. [A workout](../../health-apps.md#a-workout-with-zones-and-pace) |
| R-AGG-8 | Rolling windows over days (must) | supported | `rolling: {"buckets": 7, "reducers": ["avg", "stddev"]}`; the window reaches before the range, and each rolling value's `n` counts the buckets that held a value. One window length per read. [Trends with a baseline](../../health-apps.md#trends-with-a-baseline) |
| R-AGG-9 | Dense series with gaps, fills and wear coverage (must) [S35b] | supported | `fill` (`null`, `zero`, `locf`) marks each made row `synthetic`; every row carries `coverage`. A series with no row in the range gets none under any `fill`. |
| R-AGG-10 | Totals over session records grouped by a property (must) | supported | The property read with `property.groupBy: ["sport"]` and `property.sources`, so a run four devices recorded counts once. [Weekly load](../../health-apps.md#weekly-load) |
| R-AGG-11 | Best and worst values with the record that holds them (should) | partly | For interval observations, a list read of one metric ordered by `value` answers the record, with the range as `at` from its start and `endsAt` before its end (a filter bounding `at` on both ends is the window read, which orders by `at` alone). An instant has no `endsAt`, so that filter answers nothing: a one-column downsample answers the extreme's instant, and a second list read at that instant answers the record. A sample packed in a trace has no record; the downsample answers its value and instant. No reducer answers the record inside a bucket: [G-50](#g-50-reducers-and-filter-arms). [Resting heart rate](../../health-apps.md#resting-heart-rate-across-providers-and-merged) |

## Apps: baselines

| Id | Need | Status | Request, or what is missing |
| --- | --- | --- | --- |
| R-BASE-1 | Personal baseline band per nightly metric (must) [S5] [S11] [S14] [S23] [S41] | supported | `rolling: {"buckets": 30, "reducers": ["avg", "stddev"], "through": "previous"}` leaves the night out of its own baseline, and `n` says how many nights the window held, which is the "calibrating" count. A log transform before the mean is the app's. [Trends with a baseline](../../health-apps.md#trends-with-a-baseline) |
| R-BASE-2 | Short window against long window (must) [S5] [S11] [S17] [S18] | partly | Two reads, one per window length, over the same buckets. No recency-weighted mean. |
| R-BASE-3 | Several metrics off baseline on the same night (should) [S10] [S14] | supported | `metrics` with `rolling`: each metric's nightly value and its band in one read; the app counts the metrics outside their bands. |

## Apps: sleep

| Id | Need | Status | Request, or what is missing |
| --- | --- | --- | --- |
| R-SLEEP-1 | Hypnogram of one night (must) | supported | A list read of `health/sleep_stage` observations of one source inside the night's `[at, endsAt)`, ordered by `at`. [A night](../../health-apps.md#a-night) |
| R-SLEEP-2 | Stage totals and shares per night and per week (must) [S6b] | supported | `sum` of `health/sleep_stage` grouped by `code`: per night in the records mode over `health/sleep` with `bucket.records.sources`, per week with `place: "end"`. Unstaged sleep is the `asleep` code. [A night](../../health-apps.md#a-night) |
| R-SLEEP-3 | Heart rate, HRV, breathing, SpO2 and temperature inside the sleep window (must) [S5] [S7] [S38] | supported | The records mode over the night with `metrics`: `min`, `avg`, `{"percentile": q}` and `durationIn` of other metrics inside `[at, endsAt)`, widened by `around`. The time of the lowest heart rate is a one-column downsample over the window, whose `min` carries its `at`. [A night](../../health-apps.md#a-night) |
| R-SLEEP-4 | Efficiency, latency, wake time and totals where the vendor omits them (must) [S6b] | partly | Every adapter writes `timeInBed`, `timeAsleep` and `efficiency` where the provider sends their inputs; only Apple Health sends what `latency` is computed from, and an Apple night told by segments carries no `efficiency`. |
| R-SLEEP-5 | Bedtime, wake time, midpoint and regularity in local clock time (should) [S5] [S6b] [S20] | partly | Each `health/sleep` carries `at`, `endsAt`, `utcOffsetStart` and `utcOffsetEnd`, so the app reads clock times from a list read. `circularMean` takes angles, not clock times: [G-50](#g-50-reducers-and-filter-arms). |
| R-SLEEP-6 | Sleep need and sleep debt (should) [S3] [S22] | follow-up | WHOOP's `sleepNeeded` is dropped by its adapter: [G-53](#g-53-catalogue-metrics-and-provider-fields-no-adapter-writes). |
| R-SLEEP-7 | Naps apart from the main sleep (must) [S5] [S6b] | supported | Every adapter writes `isNap` (WHOOP and Garmin from the provider, Apple Health and Google Fit by a stated rule); filter `isNap`. |
| R-SLEEP-8 | An app's own sleep score from components (should) [S20] [S25] | partly | The inputs come in one read; the score is written back one record per request: [C-14](#c-14-multi-record-writes). |

## Apps: workouts

| Id | Need | Status | Request, or what is missing |
| --- | --- | --- | --- |
| R-WORK-1 | A workout's heart rate curve and recovery after it (must) | supported | An `every` read over the workout's span with `sources: priority`, the watch's activity stream first and its all-day stream after, so the read continues past `endsAt`. Recovery is as fine as the stream that keeps sampling after the end: five minutes in the demo, whose activity streams stop at the end. [A workout](../../health-apps.md#a-workout-with-zones-and-pace) |
| R-WORK-2 | Time in heart rate zones, with the zones in force that day (must) [S24] [S26] | partly | `durationIn` per zone over the workout's window, and `zoneDurations` where the provider sends them. The zone bounds in force on the day have no record: [G-33](#g-33-kinds-an-app-writes-about-the-person). |
| R-WORK-3 | Splits per kilometre or per mile (should) [S27] | partly | Garmin writes each activity's distance as an amount trace, so a client cuts splits from its points; no read buckets by distance. |
| R-WORK-4 | Best efforts and personal records (should) [S29] | follow-up | A derivation per workout, written as observations with `derivedFrom`, and an argmax: [G-50](#g-50-reducers-and-filter-arms). |
| R-WORK-5 | Training load, acute against chronic, fitness and fatigue (must) [S2] [S12] [S12b] [S18] | partly | Daily strain or duration with `fill: zero` and a rolling `sum` or `avg`, one read per window length; an exponentially weighted mean is the app's. [Weekly load](../../health-apps.md#weekly-load) |
| R-WORK-6 | One workout delivered by several sources (must) [S31] [S33] | supported | `property.sources` or `bucket.records.sources` with `match: ["sport"]` keeps one source's record where several overlap. Nothing links the duplicates: [G-31](#g-31-one-device-under-several-sources). [Weekly load](../../health-apps.md#weekly-load) |
| R-WORK-7 | Laps, intervals and the route (could) | out of scope | Positions and tracks are out of this build ([decision 0163](../../decisions/0163-positions-forecasts-and-money-stay-out-of-the-observed-index.md)). |
| R-WORK-8 | VO2max trend and its classification (should) [S12] [S15] | partly | `health/vo2_max` gauges from Garmin and Apple Health read as a trend; the age and sex table and the settings it needs are the app's: [G-33](#g-33-kinds-an-app-writes-about-the-person). |

## Apps: vendor and app scores

| Id | Need | Status | Request, or what is missing |
| --- | --- | --- | --- |
| R-SCORE-1 | Vendor scores under their own names, scales and bands (must) [S1] [S2] [S12c] [S13] [S13b] [S23] | partly | Each vendor score is a metric of its own in `{score}` or `%` (`whoop_recovery`, `whoop_strain`, `garmin_stress`, `garmin_body_battery`) with its `algorithm`. A metric carries no bands or colours: [G-50](#g-50-reducers-and-filter-arms). |
| R-SCORE-2 | The contributors behind a score (should) [S5] [S6b] [S12c] | partly | WHOOP's recovery, HRV and resting heart rate share a `periodId` (the cycle id) and come in one `metrics` read; no reference links a score to its inputs. |
| R-SCORE-3 | Intraday vendor curves and their states (should) [S13b] | partly | Garmin stress and Body Battery are gauge traces; Garmin's sentinel values (-1, -2) are written as nulls, so the "too active to measure" state is lost. |
| R-SCORE-4 | Scores the app computes itself from raw inputs (must) [S38] [S39] [S41] | partly | Inputs and baselines come in one read; the write-back is one record per request ([C-14](#c-14-multi-record-writes)), and a recompute on a changed input needs the time ranges a change touched ([G-32](#g-32-change-feed-time-ranges)). |

## Apps: labs and glucose

| Id | Need | Status | Request, or what is missing |
| --- | --- | --- | --- |
| R-LAB-1 | A lab report with all its results (must) | supported | A list read of `health/labresult` with `filter.referencing` at the `health/labreport`; the report keeps the original `file` and the owner's `review`. [Labs](../../health-apps.md#labs-in-both-unit-systems) |
| R-LAB-2 | One analyte's trend across labs and years, in one unit (must) | supported | The health sample's `convertlab` function writes every lab result in its paired metric (molar mass, or the NGSP and IFCC equation for HbA1c), so a read of either metric holds every lab's result. [Labs](../../health-apps.md#labs-in-both-unit-systems) |
| R-LAB-3 | High and low flags, with censored values (must) | supported | `interpretation`, `referenceRanges` (quantities, converted with a derived result) and `comparator`; a read counts bounded values in `bounded`. |
| R-LAB-4 | Preliminary, final and corrected results (should) | partly | `status` and `revision` are stored and a corrected input rewrites its derived results; no aggregate read filters on `status`: [G-50](#g-50-reducers-and-filter-arms). |
| R-GLU-1 | CGM day view, time in ranges, wear, mean, CV and GMI (must) [S34] [S35b] [S36] | partly | `durationIn` bands, `avg`, `stddev` and `coverage` answer it over one glucose metric. Meter and monitor readings are not converted between unit systems the way lab results are: the demo's Contour meter reads 5.3125 mmol/L on average in September through Google Fit (`blood_glucose`) and 95.625 mg/dL through Apple Health (`blood_glucose_mass`), and neither metric reads in the other's unit. GMI and the target range are the app's. |
| R-GLU-2 | Glucose response to a meal or another logged event (should) [S35] | follow-up | No meal or journal record to anchor a window ([G-33](#g-33-kinds-an-app-writes-about-the-person)), and no filter on a reading's meal qualifier ([C-12](#c-12-qualifier-filters)). |

## Apps: body and activity goals

| Id | Need | Status | Request, or what is missing |
| --- | --- | --- | --- |
| R-BODY-1 | Weight trend (must) [S42] | partly | Weekly or daily `avg`, `min`, `max` in any mass unit, `rolling` for a trend line; an exponentially smoothed line is the app's. One scale delivered through three providers is three sources: `separate` shows each weigh-in three times, and `priority` keeps it again where a lower-ranked path stamped it seconds earlier (10 values for 4 weigh-ins in one demo week), so the recipe reads one path: [G-31](#g-31-one-device-under-several-sources). [Weight](../../health-apps.md#weight-in-kg-and-lb) |
| R-BODY-2 | Blood pressure readings and their classification (must) [S43] [S44] | partly | Every adapter writes systolic and diastolic with `components`, and one `metrics` read aligns them; the category table is the app's. [Blood pressure](../../health-apps.md#blood-pressure) |
| R-ACT-1 | Daily goals and rings, with goal history, stand hours and streaks (must) [S19] | follow-up | No goal record: [G-33](#g-33-kinds-an-app-writes-about-the-person). |
| R-ACT-2 | Intensity-weighted minutes (should) [S21] [S24] | partly | `durationIn` per zone per day; the zones from age or resting heart rate as of that day need settings ([G-33](#g-33-kinds-an-app-writes-about-the-person)), and Garmin's intensity minutes are dropped ([G-53](#g-53-catalogue-metrics-and-provider-fields-no-adapter-writes)). |

## Apps: units

| Id | Need | Status | Request, or what is missing |
| --- | --- | --- | --- |
| R-UNIT-1 | One unit preference, applied by every app (must) [S45] | partly | Every read takes `unit`, and `GET /api/v1/units` serves the unit table and its exact factors. No preference record: [G-34](#g-34-unit-preference). |
| R-UNIT-2 | Pace in min/km or min/mi (must) | partly | Speed is stored as `m/s` gauges and the unit table has a `pace` measure, but a speed read does not answer in pace, and no adapter writes `health/pace`. A workout's mean pace is its duration over its distance, computed by the app. |
| R-UNIT-3 | Glucose and lab values in mass or molar units (must) | partly | Lab results: supported through `convertlab` ([R-LAB-2](#apps-labs-and-glucose)). Meter and monitor readings: one metric per unit system. |
| R-UNIT-4 | Temperature deviations in Celsius or Fahrenheit (must) [S5] | supported | A value with `delta: true` converts by scale alone: Garmin's skin temperature deviation of 0.03 K reads 0.054 [degF]. |
| R-UNIT-5 | Fractions shown as percent (should) | supported | `fraction` holds `1` and `%`; a score is `{score}` and never converts to `%`. |

## Apps: sources and reconciliation

| Id | Need | Status | Request, or what is missing |
| --- | --- | --- | --- |
| R-SRC-1 | Provenance of every value (must) | supported | `source` (the device: `class`, `manufacturer`, `hardwareModel`, `account`), `recordingMethod`, `origin`, `adapter` and `derivedFrom` on each value. |
| R-SRC-2 | Per-metric source priority the user sets (must) [S16b] [S30] [S47] | supported | `sources: {"policy": "priority"}` ranks by each source record's `priorities[<metric id>]` when the request names no `order`, and by `order` when it does. [Resting heart rate](../../health-apps.md#resting-heart-rate-across-providers-and-merged) |
| R-SRC-3 | One measurement reaching the store by two paths, counted once (must) [S47] | follow-up | One device under two providers is two sources with nothing linking them: [G-31](#g-31-one-device-under-several-sources). |
| R-SRC-4 | Overlapping sleep sessions from several devices (must) [S33] | supported | `bucket.records.sources` keeps one device's night, and the request's `sources` chooses each metric's points inside it. Merging two devices' stages into one timeline is not a request. |
| R-SRC-5 | Vendor daily totals against raw samples, never mixed (must) | supported | `from: "prefer-raw"` with `minCoverage`; each row's `from` says which served it. [Steps across travel](../../health-apps.md#steps-across-travel) |
| R-SRC-6 | Sleep stage and sport vocabularies mapped to one set (must) | supported | One `sport` code set and one stage code set (with `asleep`) every adapter maps onto, the provider's word kept in `sportSource`; `adapter` names the mapping's version. |
| R-SRC-7 | Sources side by side (must) | supported | `sources: {"policy": "separate"}`, the default, or `groupBy: ["source"]`. [Resting heart rate](../../health-apps.md#resting-heart-rate-across-providers-and-merged) |
| R-SRC-8 | Same name, different quantity, never pooled (must) [S41b] | supported | `hrv_rmssd` and `hrv_sdnn`, `skin_temperature` and `skin_temperature_deviation` are separate metrics; a read takes one metric's streams, and a value whose unit does not convert is a `422` naming its source. |

## Apps: ingestion lifecycle

| Id | Need | Status | Request, or what is missing |
| --- | --- | --- | --- |
| R-SYNC-1 | Re-ingest changes nothing (must) | supported | Ids are composed from upstream identity, and an unchanged output is not written again. |
| R-SYNC-2 | Rescoring and revised upstream values (must) | supported | `revision` never falls; a rescore rewrites the record, and every aggregate read is computed at read time, so it follows. |
| R-SYNC-3 | Deletions upstream and disconnecting a provider (must) [S46] | partly | WHOOP and Google Fit deletions reach the adapters' outputs, and a disconnect's cascade deletes them. Garmin and Apple Health push no deletion, so a record built from a value deleted upstream stays until its mirror row is deleted. |
| R-SYNC-4 | Late and backfilled data, and telling the app what changed (must) | partly | The change feed (`/api/v1/changes?watch=1` from a read's `head`) names each changed record, and `values=1` the properties the write moved, before and after. A creation names its `metric`, `at` and `endsAt`; an update that changes only `value` or `values` names neither, and a delete names nothing, so the app reads the record or keeps its own map from record to day: [G-32](#g-32-change-feed-time-ranges). [Incremental sync](../../health-apps.md#incremental-sync) |
| R-SYNC-5 | Freshness per source and per adapter (should) | supported | `GET /api/v1/substrate.reamde.dev/core/trigger/status` (each adapter's cursor and lag), `substratectl sync status`, and the latest read's newest point per stream. |

## Apps: the app's write side

| Id | Need | Status | Request, or what is missing |
| --- | --- | --- | --- |
| R-WRITE-1 | Journal: daily behaviors, amounts and mood (must) [S4b] [S37] | follow-up | [G-33](#g-33-kinds-an-app-writes-about-the-person). |
| R-WRITE-2 | Manual measurements, edited and deleted by their author (must) [S16b] | partly | An observation written by hand with `recordingMethod: manual` and the `manual` source reads like any other. An owner's correction to an adapter's output is undone by the next delivery: [C-1](#c-1-owner-corrections-undone-by-replays). |
| R-WRITE-3 | Notes and annotations on records and on time ranges (must) [S18] | partly | `notes` on a workout and annotations on any record; no span record for an illness or a trip, and no effort rating: [G-33](#g-33-kinds-an-app-writes-about-the-person). |
| R-WRITE-4 | Effective-dated settings that change computations (must) [S26] | follow-up | [G-33](#g-33-kinds-an-app-writes-about-the-person). |
| R-WRITE-5 | Values the app derives, written back with their inputs (must) | partly | An observation carries `derivedFrom` and `conversion`, its metric an `algorithm`; one record per request ([C-14](#c-14-multi-record-writes)). |

## Apps: insights and building apps

| Id | Need | Status | Request, or what is missing |
| --- | --- | --- | --- |
| R-INS-1 | Which behaviors move a next-day metric (should) [S4] | follow-up | The metric side is one `metrics` read; the behavior side has no record ([G-33](#g-33-kinds-an-app-writes-about-the-person)). |
| R-INS-2 | Correlation between two daily metrics, with a lag (should) [S9] [S37] | partly | Two daily series aligned in one `metrics` read; the lag and the statistic are the app's. |
| R-INS-3 | Period against period (should) | supported | The same read over two ranges gives identical bucket rules, each row with its `coverage`. |
| R-APP-1 | Self-describing results (must) | supported | Every value names its unit; rows carry bounds with offsets, `sources`, `coverage`, `from`, `synthetic`, `offsetless`, `prorated`; the response carries `exact` and `units`. |
| R-APP-2 | A home screen in few reads at a predictable cost (should) | partly | The latest read and one `metrics` read fill a day screen, budgets refuse rather than truncate, and the console's shared reads show the pattern. A year view reads 15-minute rollup rows (no daily level), and a browser app on another origin cannot call the API: [C-9](#c-9-cors-for-browser-apps). [Today](../../health-apps.md#today) |
| R-APP-3 | Metric metadata a UI can render from (should) | partly | A metric names its `name`, `unit`, `displayPrecision`, `category`, `reducers` and `codeSet`; no normal range or bands: [G-50](#g-50-reducers-and-filter-arms). |
| R-APP-4 | Agent answers from a few rows (should) | supported | `host.records.aggregate` and the agent `query` tool's `aggregate` and `downsample` arguments, under the caller's read budget. |

## Platform primitives

| Id | Primitive | Status | Request, or what is missing |
| --- | --- | --- | --- |
| P-1 | Calendar buckets in a named IANA zone, correct across DST (must) [hk-scq] [hc-agg] [gfit-agg] [gh-daily] [flux-aw] [ts-gapfill] | supported | `calendar` with `timezone`. |
| P-2 | Recorded-local time: each sample's own offset decides its day (must) [hc-trf] [gh-dp] [whoop-spec] [oura-spec] | supported | The recorded-local calendar; see R-TIME-1 and R-TIME-5. |
| P-3 | Fixed-width buckets with an anchor, origin or offset (must) [hk-scq-article] [hc-agg] [influxql] | supported | `every` with `origin`; `offset` and `weekStart` on calendars. |
| P-4 | Physical time and civil time kept apart in requests (should) [gh-filters] [hc-agg] | partly | A range is two instants with offsets, never a wall-clock date, so "the local days of a trip" needs the offsets known first; a read by wall-clock range is a follow-up. |
| P-5 | A rule for which day a session belongs to (should) [oura-spec] [gh-filters] | supported | `place: "end"`; the property read's `anchor`. |
| P-6 | Interval amounts that straddle a bucket boundary (should) | supported | Stated per mode: whole in the bucket of its start (or end) in the `every` and calendar modes, prorated in the records mode. |
| P-7 | Aggregation style declared per metric; wrong reducers refused (must) [hk-opts] [hk-sq] [hc-agg] | supported | Each value's `form` decides its reducers; `sum` of a gauge is a `422`. |
| P-8 | Core statistics: sum, avg, min, max, count, most recent, duration covered (must) [hk-opts] [hk-stats] [fhir-stats] [prom-fn] | supported | The reducers by form, `last`, and each row's `coverage`. |
| P-9 | Several statistics and several metrics on one bucket axis (must) [hk-opts] [hc-agg] [gfit-agg] | supported | Several `reducers`, and `metrics`. |
| P-10 | Time-weighted average for irregular sampling (should) [ts-tw] | supported | `twa`. |
| P-11 | Dispersion and percentiles (should) [fhir-stats] [prom-fn] [ts-hyper] | supported | `stddev`, `{"percentile": q}`; not over summaries. |
| P-12 | Histograms, time in range and heart-rate zones (must) [whoop-spec] [hk-workout] [hk-zone] [gh-dp] [ts-hist] | supported | `durationIn`, one per band. |
| P-13 | Time in state for categorical streams (must) [hc-sessions] [whoop-spec] [oura-spec] [ts-state] | supported | `timeInState` for a coded gauge; `sum` by `code` for coded amounts such as sleep stages. |
| P-14 | Statistics scoped to a session's window (must) [gfit-agg] [hk-query] [hc-sessions] | supported | The records mode, with `around`. |
| P-15 | Counter increase and rate across resets (could) [prom-fn] | supported | `increase` and `rate` under the counter contracts. |
| P-16 | Smoothing, trend and regression (could) [prom-fn] [fhir-stats] | partly | `rolling` gives windowed means; exponential smoothing and regression are the app's. |
| P-17 | Merged statistics by default, per-source on request (must) [hk-stats] [hc-agg] [gfit-agg] | supported | `priority` merges, `separate` (the default) splits. |
| P-18 | User-ordered source priority, resolved per time slice (must) [apple-support] [hc-agg] | supported | `priority`, resolved per stored interval before buckets are cut. |
| P-19 | Source filter on reads and aggregates (must) [hc-agg] [hc-rrr] [hk-query] [gh-daily] | supported | `filter.properties.source` with `eq`, `in` and `exists`. |
| P-20 | Contributing sources reported with each result (should) [hc-aggres] [hk-stats] [hk-sc] | supported | `sources` on each row and on the response. |
| P-21 | Source discovery for a metric (should) [hk-source] | supported | The latest read grouped by `metric` and `source` (and `about`, `channel`, `statistic`): 124 streams from 16 sources on the demo in one request. [The stream list](../../health-apps.md#the-stream-list) |
| P-22 | A reconciled raw stream (should) [gh-reconcile] | partly | `priority` resolves points into buckets; no read answers the resolved points themselves. |
| P-23 | Provenance on every value: app, device type, recording method (must) [gh-dp] [withings-spec] [hc-sync] | supported | See R-SRC-1. |
| P-24 | Most recent value per metric or code, and last N (must) [fhir-lastn] [hk-opts] [oura-spec] [hc-rrr] [prom-fn] | partly | The latest read answers the newest value per metric, source or code. The last N values of a metric is a list read ordered by `at:desc` only where each value is a record of its own. For `health/heart_rate` on the demo that list answers the pulse of Garmin's weekly blood pressure readings and WHOOP's workout and cycle summaries, or, over `observations/trace`, hourly traces of 12 values each, so the app unpacks the last N from the traces' `values`. |
| P-25 | Raw sample reads with filter, order, limit and cursor (must) [hc-rrr] [hc-read] [hk-sample] [whoop-spec] [gh-rollup] | supported | The list read, keyset-paged. |
| P-26 | Predicates on values, metadata and a parent's child statistics (should) [hk-query] [gh-filters] | partly | Filters on `value` and declared properties; none on statistics of a session's children. |
| P-27 | Field projection (could) [oura-spec] | follow-up | [G-52](#g-52-records-route-limits). |
| P-28 | Range and page caps with an explicit signal (should) [gh-rollup] [gh-daily] [whoop-spec] | supported | Budgets refuse with a `422` naming the limit, never a cut answer. |
| P-29 | Incremental change feed with an anchor, deletions included (must) [hk-aoq] [hc-sync] [gfit-changes] [withings-spec] [fhir-http] | supported | `/api/v1/changes?watch=1` from a read's `head` and `generation`, deletions included, resumable from the last `seq`. [Incremental sync](../../health-apps.md#incremental-sync) |
| P-30 | Push notification of changes and long-running queries (should) [whoop-hooks] [oura-spec] [hk-obs] [hk-scq] | partly | The watch stream (`watch=1`) pushes changed records; nothing pushes a changed aggregate. |
| P-31 | Deletion propagation and cascade rules (must) [hc-sync] [hc-sessions] [hk-aoq] [whoop-hooks] [oura-spec] [withings-spec] | partly | See R-SYNC-3. |
| P-32 | Writer idempotency key plus version (upsert) (must) [hk-sync] [hc-sync] [fhir-http] | supported | Composed ids and a `revision` that never falls. |
| P-33 | Upstream natural keys and whole-document replace (should) [whoop-spec] [withings-spec] [oura-spec] | supported | Composed ids, and a `put` replaces the properties it names. `host.observed.trace` writes both keys of an exclusive pair (`every` and `offsets`, `values` and `codes`), the unused one null, so a rewrite replaces a period whole; a writer outside it nulls the other key itself. |
| P-34 | Provisional and final values, algorithm version (should) [whoop-spec] [oura-spec] | partly | Garmin's tentative nights and WHOOP's recoveries while it calibrates carry `quality: estimated`; WHOOP's adapter writes no value while a score is pending or unscorable; a metric names its `algorithm`. No read filters on `quality`: [G-50](#g-50-reducers-and-filter-arms). |
| P-35 | History window and backfill (could) [hc-agg] | out of scope | A provider's matter, not the read's. |
| P-36 | The value shapes: instant, interval, session, daily summary (must) [gh-filters] | supported | The forms `gauge`, `amount`, `counter`, `summary`, and the `sleep` and `workout` kinds. |
| P-37 | Packed sample arrays inside one record (must) [oura-spec] [fhir-dt] | supported | `trace`. |
| P-38 | Categorical samples (must) [hc-sessions] [oura-spec] | supported | `code` and `codeSet`. |
| P-39 | Compound values: correlations and components (must) [hk-corr] [fhir-obs] [withings-spec] | supported | `components`. |
| P-40 | Parent and child: workout to samples, sleep to stages, laps, route (must) [hk-workout] [hk-query] [hk-route] [hc-sessions] [fhir-obs] [whoop-spec] | partly | The records mode joins by window, and a night's stages share its `origin`; no reference links a WHOOP night to its cycle and recovery; laps and routes are out of scope. |
| P-41 | Session statistics consistent with the children (should) [hk-workout] [whoop-spec] | partly | Nothing checks a session's figures against its samples; each row carries `coverage`. |
| P-42 | Stored summaries tagged with statistic and period (must) [gh-dp] | supported | `summary` with `statistic`, `period` and `periodId`; `from: "summaries"`. |
| P-43 | Lineage of derived values (should) [fhir-obs] [oura-spec] | supported | `derivedFrom` and `conversion`. |
| P-44 | Measurement context: meal relation, specimen, body position, location (should) [hc-skin] [fhir-obs] | partly | `qualifiers` and a lab result's `specimen` are stored; no read filters on them: [C-12](#c-12-qualifier-filters). |
| P-45 | Quantity with a unit, converted on read (must) [hk-unit] [fhir-dt] [fhir-stats] | supported | Every read takes `unit` and converts exactly where the factor allows. |
| P-46 | Mass to molar conversion with the analyte's molar mass (should) [hk-unit] [ucum-lhc] | partly | For lab results, as derived results; not on read ([decision 0153](../../decisions/0153-a-quantity-is-exact-digits-and-a-ucum-code-converted-only-within-its-measure.md)). |
| P-47 | Per-metric unit preferences (should) [hk-pref] [hk-glucose] | follow-up | [G-34](#g-34-unit-preference). |
| P-48 | Censored values with a comparator (should) [fhir-dt] | supported | `comparator`, counted in `bounded`. |
| P-49 | Exact decimal values (could) | supported | Values are decimal strings; `exact` says when a read rounded. |
| P-50 | Reference ranges on each result (must) [fhir-obs] | supported | `referenceRanges`. |
| P-51 | Interpretation flags (should) [fhir-obs] | supported | `interpretation`. |
| P-52 | Panels: a report and its member results (must) [fhir-obs] [hk-query] | supported | `report` and `referencing`. |
| P-53 | Result status and issued time apart from effective time (should) [fhir-obs] [fhir-stats] | partly | `status` and `issuedAt` stored; no read filters on `status`. |
| P-54 | Coded test identity and grouping by code (should) [fhir-lastn] | supported | `coding` and the metric's `codes` (LOINC). |
| P-55 | FHIR resource interchange (could) | out of scope | The build's scope list. |
| P-56 | Gap filling with a declared policy (must) [ts-gapfill] [influxql] [flux-aw] [influx3-time] [gfit-agg] [hk-scq-article] [hc-agg] | supported | `fill` `null`, `zero` or `locf`; no linear interpolation. |
| P-57 | Rolling windows (must) [prom-fn] [flux-tma] | supported | `rolling`. |
| P-58 | Personal baselines and deviation from them (should) [oura-spec] [hc-skin] [garmin-hrv] | supported | See R-BASE-1. |
| P-59 | Downsampling to a point budget (should) [ts-lttb] [gh-rollup] | supported | `downsample` (M4) with each extreme's `at`. |

## Follow-ups

These stay open after the review, by design: each needs a decision or work
of its own. Each states the problem, what it costs an app, and the proposal
the review left.

### G-30 Guessed UTC offsets

Google Fit sends no UTC offset, so its adapter stamps every value with the
account's current zone, and Apple Health's daily totals use the phone's
export zone. During a trip Fit values land on the home zone's day, and a
replay after the account's zone changes moves every past Fit value to the new
zone (C-16). Proposal: infer an offset from another source's points at the
same instant, stamped with the rule used, or read it from a "where the user
was" span record ([G-33](#g-33-kinds-an-app-writes-about-the-person)); the
Fit provider could keep the zone in force per day on its dataset rows.

### G-31 One device under several sources

Done: Garmin's account source is "Garmin Connect", class `service`, and a
device-less activity names `unknown-device`. Open: a Withings scale reaches
the repository through Apple Health, Google Fit and Garmin as three sources,
so a weigh-in counts once per path under `separate`, and under `priority`
twice where the paths' timestamps differ by seconds. Proposal: a link between
sources that are one device (a `device` reference or `sameAs` on `source`)
that `priority` treats as one rank, with a tolerance for the seconds between
the paths' timestamps. Decision 0162 rules out near-equal deduplication, so
the tolerance needs that decision reopened for linked sources.

### G-32 Change feed time ranges

The change feed names the records a write changed, never the metrics and time
ranges, so an app that caches daily rows re-reads the visible range or keeps
its own map from record to day. `values=1` names only the properties a write
moved: a creation's `metric`, `at` and `endsAt` place it, but an update that
changes only `value` or `values` (a rescore, a re-delivered trace hour) names
neither, and a delete names nothing. So every update costs the app a record
read or a lookup in its own map, and every delete a lookup. Proposal: a
change row for an observed kind carries its metric, source and `[at,
endsAt)` before and after, or a read answers "the buckets changed since this
head".

### G-33 Kinds an app writes about the person

No kind holds effective-dated settings (maximum and resting heart rate, zone
bounds, FTP, birth date, sex, a glucose target), goals, journal entries
(behaviors, amounts, mood) or spans (illness, travel, rest), so zones in force
on a day, rings with goal history, meal responses and behavior insights
cannot be built from the repository. Proposal: health sample kinds `setting`
(effective-dated), `goal`, `journal` and `span`, each readable as
records-mode windows.

### G-34 Unit preference

Every app, agent and the console picks units per request; record pages show
stored units. Proposal: a per-person preference record, a unit per measure or
per metric, that the aggregate read uses as its default `unit` and that the
console and agents read. `GET /api/v1/units` (G-35, done) serves the table a
client converts with meanwhile.

### G-50 Reducers and filter arms

Five reads apps ask for have no request: the extreme of a bucket with the
instant and record that hold it (`{"argmax": {}}` answering `at` and
`record`); a clock-time circular mean over a 24-hour period for bedtime
regularity; a pace reducer that divides total time by total distance instead
of averaging paces; filter arms on `quality` and on a lab result's `status`,
so `questionable` and `entered-in-error` values can be left out; and `bands`
on `metric`, a score's or a reading's ranges and colours. Each needs a small
design.

### G-52 Records-route limits

A list with `at` bounded on both ends becomes the window read, which orders by
`at` alone and refuses `count`; a list of traces returns every trace's full
`values` array, with no `fields=` projection; a filter on an undeclared name
answers no rows instead of a `422`; and a record id holding `/` must be
percent-encoded in a record path, which `docs/api.md` states only for kind
declarations. Proposal: window ordering by other properties, `fields=`, a
`422` on an undeclared name, and a docs line for observed ids.

### G-53 Catalogue metrics and provider fields no adapter writes

`health/pace`, `fat_mass` and `elevation_gain` have no writer; the WHOOP
`user` row (weight, height) is never adapted; WHOOP `sleepNeeded` and
`percentRecorded`, Garmin intensity minutes, stress durations and
`fitnessAge`, six Apple mirror kinds and Google Fit resting heart rate are
dropped. Garmin's per-sample elevation is not written, because its positive
steps sum to four times Garmin's own climb; an altitude gauge metric would
store the profile and let a read derive climb with a stated smoothing.
Proposal: adapt each when a consumer needs it.

### C-1 Owner corrections undone by replays

An owner who fixes a workout's `sport`, marks a sleep as a nap, or deletes a
duplicate workout loses the fix at the next delivery of the mirror row,
because each adapter's `reconcile` writes every property it owns and restores
an output it deleted. The adapter upgrade procedure replays every mirror row,
which reverts every correction at once. Open decision: either `reconcile`
leaves a property another actor set since the adapter's last write (the
changelog names each write's actor), or corrections live in a record of their
own that reads prefer.

### C-9 CORS for browser apps

A web app served from its own origin cannot call the API: a preflight
`OPTIONS /api/v1/records` answers `405` with no `Access-Control-*` header.
How such an app gets a token is unmapped too, and a token reads and writes the
whole repository, since the model has no scopes. Proposal: a configured list
of allowed origins that answers preflights, and a decision on how an app
obtains its token.

### C-10 Cycle tracking and nutrition

Menstrual cycle tracking, nutrition and water, medications, mindfulness and
ECG are screens in Apple Health, Oura, Garmin Connect and WHOOP; the health
catalogue has no metric or kind for any of them. The Apple Health provider
mirrors `cycleevent`, `medication`, `symptom`, `stateofmind`, `ecg` and
`heartratenotification`, and its adapter reads none. Proposal: one domain at
a time, each as metrics and kinds in the health sample and an adapter
mapping.

### C-12 Qualifier filters

Apple writes a glucose reading's meal relation as a qualifier, but a metric
read cannot filter or group by `qualifiers` (a `400`), so fasting against
after-meal glucose cannot be averaged apart. Proposal: an `eq` and `in` arm on
qualifier codes in the metric read's filter, beside the G-50 arms.

### C-14 Multi-record writes

The records route writes one record per request: a 90-day hourly backfill of
one metric is about 2,160 requests, and an app writing derived values
(R-WRITE-5) pays one request per value. The shipped adapters avoid this
through function effects, and an app can do the same by installing a
function of its own and calling it with a batch
(`POST /api/v1/substrate.reamde.dev/core/function/{name}/call`), at the cost
of shipping a bundle. Proposal: a write of several records in one request
and one transaction on the records route.

### Other open items

- A daily rollup level: a year of a dense stream reads about 35,000 rollup
  rows per stream (decision 0160 defers it).
- A read by wall-clock range ("the local days of the London trip") without
  knowing the offsets first (P-4).
- A date property for a WHOOP cycle and a read that buckets on it, so a
  cycle's day does not depend on where its `at` falls.
- A reference from a WHOOP night to its cycle and recovery (P-40).
- A recorded-local row's bounds are written at the offsets of its own
  values, so consecutive days at two offsets overlap in instants: on the demo
  2026-09-24 (London) ends at `2026-09-25T00:00:00+01:00` while 2026-09-25
  (Athens) starts at `2026-09-25T00:00:00+03:00`, two hours earlier; the
  first row's `coverage` (22 hours) is the day's real length. Whether the end
  should be written at the next day's start is open.

## Sources

The two catalogues cite these; `[fetched]` means the page was read on
2026-10-13, `[search]` that the claim came from a search result summary. The
earlier research is in [the health data plan](../health-data.md) and its
[part 01](../health-data/01-wearables.md), [02](../health-data/02-workouts.md),
[04](../health-data/04-aggregators-and-models.md) and
[05](../health-data/05-analytics-and-storage.md), and in
[the observations plan's](../observations.md) research pages
[01](01-iot-models.md), [02](02-units-and-catalogues.md) and
[03](03-domains-and-prior-art.md).

Apps:

- S1 [search] WHOOP community, the Recovery score: https://www.community.whoop.com/t/what-is-the-recovery-score-and-how-is-it-calculated/107
- S2 [search] WHOOP, how strain works: https://www.whoop.com/au/en/thelocker/how-does-whoop-strain-work-101
- S3 [search] WHOOP, how much sleep do I need: https://www.whoop.com/us/en/thelocker/how-much-sleep-do-i-need
- S4 [search] WHOOP, behavior insights: https://www.whoop.com/thelocker/a-new-way-to-see-insights-on-which-behaviors-affect-your-recovery/
  and the Monthly Performance Assessment: https://whoop.com/eu/en/thelocker/monthly-performance-assessment-now-features-enhanced-recovery-analysis
- S4b [search] WHOOP, the journal: https://www.whoop.com/thelocker/the-whoop-journal/
- S5 [fetched] Oura, Readiness Contributors: https://support.ouraring.com/hc/en-us/articles/360057791533-Readiness-Contributors
- S6 [fetched] Oura, Sleep Score: https://support.ouraring.com/hc/en-us/articles/360025445574-Sleep-Score
- S6b [fetched] Oura, Sleep Contributors: https://support.ouraring.com/hc/en-us/articles/360057792293-Sleep-Contributors
- S7 [fetched] Oura, Readiness Score: https://support.ouraring.com/hc/en-us/articles/360025589793-Readiness-Score
- S8 [fetched] Oura, Trends: https://support.ouraring.com/hc/en-us/articles/360055983614
- S9 [search] Oura on the Web, compare two metrics: https://support.ouraring.com/hc/en-us/articles/43392924561171
- S10 [search] Oura, Health Radar and Symptom Radar: https://support.ouraring.com/en-us/articles/52627030482707-Health-Radar,
  https://ouraring.com/blog/symptom-radar/
- S11 [fetched] Garmin manual, Heart Rate Variability Status: https://www8.garmin.com/manuals/webhelp/GUID-4462687B-8DF5-4719-872E-E16754F9D4B0/EN-US/GUID-5247CEC7-AF06-44FB-B6AD-87C53D4F7941.html
- S12 [fetched] Garmin manual, Training Status: https://www8.garmin.com/manuals/webhelp/GUID-3A4F9C4A-8735-46C0-8DA9-65F11400B150/EN-US/GUID-44C7BB4B-EFF7-4A42-AC03-8A6AABB94807.html
- S12b [search] Garmin manual, Acute Load: https://www8.garmin.com/manuals-apac/webhelp/forerunner170/EN-SG/GUID-EEFF46B0-70D8-407F-B380-2379C7CDD71E-3066.html
- S12c [search] Garmin manual, Training Readiness: https://www8.garmin.com/manuals/webhelp/GUID-31D23DBB-57C2-4DF7-A0C9-8D1A00AB4BE7/EN-US/GUID-C21BE0C8-A08E-4DA1-B6C6-2E0E2DDDB372.html
- S13 [search] Garmin manual, Heart Rate Variability and Stress Level: https://www8.garmin.com/manuals/webhelp/venu/EN-US/GUID-9282196F-D969-404D-B678-F48A13D8D0CB.html
- S13b [search] Garmin manual, Body Battery: https://www8.garmin.com/manuals-apac/webhelp/forerunner965/EN-SG/GUID-DC1CDE44-C3A4-459E-B10D-B711D9DF8B26-7186.html
- S14 [fetched] Apple, Vitals on Apple Watch: https://support.apple.com/guide/watch/vitals-apd15aa7ed96/watchos
- S15 [fetched] Apple, cardio fitness levels: https://support.apple.com/en-us/108790
- S16 [search] Apple Health Summary and Highlights: https://macrumors.com/roundup/health
- S16b [fetched] Apple, Health data sources priority: https://support.apple.com/en-us/108779
- S17 [search] Apple Activity Trends, 90 days against 365: https://www.macworld.com/article/233093/ios-13-and-apple-watch-activity-trends-give-you-the-big-picture.html
- S18 [search] Apple training load and effort rating: https://www.apple.com/il/newsroom/2024/09/watchos-11-is-available-today,
  https://dcrainmaker.com/2024/07/apples-training-load-vitals-watchos11.html
- S19 [search] Apple, track daily activity: https://support.apple.com/108359
- S20 [fetched] SlashGear on the Apple Watch sleep score: https://www.slashgear.com/1978333/how-apple-watch-rates-health-with-sleep-score
- S21 [search] Google Fit, Heart Points: https://support.google.com/fit/answer/7619539
- S22 [search] Garmin, Sleep Coach: https://www.garmin.com/en-GB/blog/?p=10566
- S23 [fetched] Fitbit, Daily Readiness Score: https://support.google.com/fitbit/answer/14236710?hl=en
- S24 [fetched] Fitbit, Active Zone Minutes: https://support.google.com/fitbit/answer/14236509
- S25 [search] Fitbit, sleep score: https://support.google.com/fitbit/answer/14236513
- S26 [fetched] Strava, Run Pace Zone Analysis: https://support.strava.com/hc/en-us/articles/216918297-Run-Pace-Zone-Analysis-Premium-
- S27 [search] Strava, pace and splits: https://support.strava.com/hc/en-us/articles/115001136770-Pace-Speed
- S28 [search] Apple developer, statistics collection queries: https://developer.apple.com/documentation/healthkit/executing-statistics-collection-queries
- S29 [search] Strava, Best Efforts: https://support.strava.com/hc/en-us/articles/16601494390285-Best-Efforts-Running
- S30 [fetched] Android Health Connect, aggregated data: https://developer.android.com/health-and-fitness/health-connect/aggregate-data
- S31 [search] Cult of Mac on duplicate workouts: https://www.cultofmac.com/how-to/how-to-fix-apple-watch-duplicate-workouts
- S32 [fetched] Sahha, which day does sleep belong to: https://sahha.ai/blog/which-day-does-sleep-belong-to/
- S33 [fetched] Sahha, deduplicating health data across wearables: https://sahha.ai/blog/deduplicating-health-data-across-wearables/
- S34 [fetched] Dexcom, time in range: https://www.dexcom.com/en-us/faqs/what-is-time-in-range
- S35 [fetched] Levels, scores and glucose metrics: https://support.levels.com/article/768-how-levels-scores-and-glucose-metrics-work-a-complete-guide
- S35b [search] Dexcom CLARITY patient guide: https://www.teladochealth.com/content/dam/tdh-www/us/en/documents/helpcenter/LBL017270-Dexcom-CLARITY-Patient-Guide-to-Reports.pdf
- S36 [search] International Consensus on Time in Range (Diabetes Care 2019, doi 10.2337/dci19-0028): https://www.ajmc.com/newsroom/ada-issues-timeinrange-targets-for-cgm-use
- S37 [fetched] Exist, use cases: https://exist.io/blog/use-cases/; [search] custom tracking: https://exist.io/blog/custom-tracking/
- S38 [fetched] Kiledjian on Bevel: https://kiledjian.com/2026/07/07/bevel-turns-apple-watch-data.html
- S39 [search] TapSmart on Gentler Streak: https://www.tapsmart.com/features/deep-dive-gentler-streak/
- S40 [search] Garmin forums, data on the wrong day after a time zone change: https://forums.garmin.com/apps-software/mobile-apps-web/f/garmin-connect-web/75727/looks-like-you-changed-time-zones-two-days-later-still-no-steps-data/412072
- S41 [search] Athlytic, getting started: https://athlyticapp.helpscoutdocs.com/article/13-getting-started
- S41b [fetched] Athlytic, recovery on Apple Watch: https://athlyticapp.helpscoutdocs.com/article/17-recovery-on-apple-watch-does-not-match-the-iphone-app
- S42 [search] Withings community, health graphs: https://support.withings.com/hc/en-us/community/posts/360014441297-Health-graphs-explained
- S43 [search] Withings, BPM Vision colour codes: https://support.withings.com/hc/en-us/articles/33575381550481-BPM-Vision-Color-Code-Meanings
- S44 [search] AHA and ACC 2017 categories: https://www.medicalnewstoday.com/articles/270644
- S45 [search] Apple Health glucose units: https://www.mobihealthnews.com/news/apple-add-glucose-tracking-back-health
- S46 [fetched] WHOOP developer, webhooks: https://developer.whoop.com/docs/developing/webhooks
- S47 [fetched] Thryve, duplicate health data: https://www.thryve.health/blog/duplicate-health-data

Platforms (all fetched 2026-10-13):

- [hk-scq] https://developer.apple.com/documentation/healthkit/hkstatisticscollectionquery
- [hk-scq-article] https://developer.apple.com/documentation/healthkit/executing-statistics-collection-queries
- [hk-sc] https://developer.apple.com/documentation/healthkit/hkstatisticscollection
- [hk-stats] https://developer.apple.com/documentation/healthkit/hkstatistics
- [hk-opts] https://developer.apple.com/documentation/healthkit/hkstatisticsoptions
- [hk-sq] https://developer.apple.com/documentation/healthkit/hkstatisticsquery
- [hk-aoq] https://developer.apple.com/documentation/healthkit/hkanchoredobjectquery
- [hk-obs] https://developer.apple.com/documentation/healthkit/hkobserverquery
- [hk-sample] https://developer.apple.com/documentation/healthkit/hksamplequery
- [hk-source] https://developer.apple.com/documentation/healthkit/hksourcequery
- [hk-query] https://developer.apple.com/documentation/healthkit/hkquery
- [hk-workout] https://developer.apple.com/documentation/healthkit/hkworkout
- [hk-zone] https://developer.apple.com/documentation/healthkit/hkworkoutzonegroup
- [hk-route] https://developer.apple.com/documentation/healthkit/hkworkoutroutequery
- [hk-corr] https://developer.apple.com/documentation/healthkit/hkcorrelation
- [hk-unit] https://developer.apple.com/documentation/healthkit/hkunit
- [hk-pref] https://developer.apple.com/documentation/healthkit/hkhealthstore/preferredunits(for:completion:)
- [hk-glucose] https://developer.apple.com/documentation/healthkit/hkquantitytypeidentifier/bloodglucose
- [hk-sync] https://developer.apple.com/documentation/healthkit/hkmetadatakeysyncidentifier
- [apple-support] https://support.apple.com/en-us/108779
- [hc-agg] https://developer.android.com/health-and-fitness/guides/health-connect/develop/aggregate-data
- [hc-sync] https://developer.android.com/health-and-fitness/guides/health-connect/develop/sync-data
- [hc-read] https://developer.android.com/health-and-fitness/guides/health-connect/develop/read-data
- [hc-sessions] https://developer.android.com/health-and-fitness/guides/health-connect/develop/sessions
- [hc-trf] https://raw.githubusercontent.com/androidx/androidx/androidx-main/health/connect/connect-client/src/main/java/androidx/health/connect/client/time/TimeRangeFilter.kt
- [hc-aggres] https://raw.githubusercontent.com/androidx/androidx/androidx-main/health/connect/connect-client/src/main/java/androidx/health/connect/client/aggregate/AggregationResult.kt
- [hc-rrr] https://raw.githubusercontent.com/androidx/androidx/androidx-main/health/connect/connect-client/src/main/java/androidx/health/connect/client/request/ReadRecordsRequest.kt
- [hc-skin] https://raw.githubusercontent.com/androidx/androidx/androidx-main/health/connect/connect-client/src/main/java/androidx/health/connect/client/records/SkinTemperatureRecord.kt
- [gfit-agg] https://developers.google.com/fit/rest/v1/reference/users/dataset/aggregate
- [gfit-changes] https://developers.google.com/fit/rest/v1/reference/users/dataSources/dataPointChanges/list
- [gh-dp] https://developers.google.com/health/reference/rest/v4/users.dataTypes.dataPoints
- [gh-rollup] https://developers.google.com/health/reference/rest/v4/users.dataTypes.dataPoints/rollUp
- [gh-daily] https://developers.google.com/health/reference/rest/v4/users.dataTypes.dataPoints/dailyRollUp
- [gh-reconcile] https://developers.google.com/health/reference/rest/v4/users.dataTypes.dataPoints/reconcile
- [gh-filters] https://developers.google.com/health/filters
- [fhir-obs] https://hl7.org/fhir/R4/observation.html
- [fhir-lastn] https://hl7.org/fhir/R4/observation-operation-lastn.html
- [fhir-stats] https://hl7.org/fhir/R4/observation-operation-stats.html
- [fhir-http] https://hl7.org/fhir/R4/http.html
- [fhir-dt] https://hl7.org/fhir/R4/datatypes-definitions.html
- [oura-spec] https://cloud.ouraring.com/v2/static/json/openapi-1.41.json
- [whoop-spec] https://api.prod.whoop.com/developer/doc/openapi.json
- [whoop-hooks] https://developer.whoop.com/docs/developing/webhooks
- [garmin-hrv] https://www8.garmin.com/manuals-apac/webhelp/venu33s/EN-SG/GUID-16D80A47-53F1-44B2-8C97-98DBB188E5AF-4096.html
- [withings-spec] https://developer.withings.com/openapi.yaml
- [flux-aw] https://docs.influxdata.com/flux/v0/stdlib/universe/aggregatewindow/
- [flux-tma] https://docs.influxdata.com/flux/v0/stdlib/universe/timedmovingaverage/
- [influxql] https://docs.influxdata.com/influxdb/v1/query_language/explore-data/
- [influx3-time] https://docs.influxdata.com/influxdb3/core/reference/sql/functions/time-and-date/
- [prom-fn] https://prometheus.io/docs/prometheus/latest/querying/functions/
- [ts-gapfill] https://www.tigerdata.com/docs/api/latest/hyperfunctions/gapfilling/time_bucket_gapfill
- [ts-hyper] https://www.tigerdata.com/docs/api/latest/hyperfunctions
- [ts-state] https://www.tigerdata.com/docs/api/latest/hyperfunctions/state-tracking/state_agg
- [ts-tw] https://www.tigerdata.com/docs/api/latest/hyperfunctions/time-weighted-calculations/time_weight
- [ts-lttb] https://www.tigerdata.com/docs/reference/toolkit/downsampling/lttb
- [ts-hist] https://www.tigerdata.com/docs/api/latest/hyperfunctions/histogram/
- [ucum-lhc] https://github.com/LHNCBC/ucum-lhc
