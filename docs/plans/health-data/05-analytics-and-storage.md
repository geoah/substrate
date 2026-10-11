# Health data analytics, aggregation API and time-series storage in Postgres

Researcher 5 of 6. Slice: what applications compute from health data, the query
primitives that implies, prior art for an aggregation API, and how to store very
large per-user volumes in Postgres under substrate's changelog-is-truth model.
Written 2026-10-08.

Provenance legend used throughout:

- **[web]** checked on the web today, URL given.
- **[bench]** measured today on this machine (Postgres 16.15 + TimescaleDB
  2.30.2 in a throwaway container, scripts in `/tmp/health-research/bench/`).
- **[repo]** read in the substrate tree at
  `/home/geoah/.t3/worktrees/substrate/t3-d9d5090b`.
- **[recalled]** from memory, not re-verified today. Treat as unverified.

## Contents

1. How applications use health and fitness data (and the computation each needs)
2. The query primitives a general API must support
3. Prior art for the API shape
4. A small declarative JSON aggregation request for substrate
5. Volume estimates
6. Storage options in Postgres, measured
7. Units at the query layer
8. Timezones
9. Recommendation, ranked by fit with substrate's values
10. Sources

---

## 1. How applications use health and fitness data

Each row names the use, the computation, and the primitive it leans on (the
primitives are defined in section 2).

### 1.1 Readiness and recovery scores

| Product | Inputs | Computation | Primitives |
|---|---|---|---|
| WHOOP Recovery | overnight HRV (weighted toward slow-wave sleep and late night), RHR, respiratory rate, sleep performance, skin temperature, SpO2 | each input compared to the user's own baseline; WHOOP says HRV is compared to a 30-day baseline [web: whoop.com/thelocker/heart-rate-variability-hrv, support.whoop.com WHOOP-Recovery]. Formula unpublished. | per-session statistic (one value per sleep), rolling 30-day mean and SD, z-score |
| Oura Readiness | nine contributors: activity balance, body temperature, HRV balance, previous day activity, previous night, recovery index, resting heart rate, sleep balance (and sleep regularity) [web: Oura API v2 `daily_readiness` contributors] | HRV balance compares a recency-weighted 14-day HRV average against a 3-month average [web: ouraring.com/blog/hrv-balance, support.ouraring.com 360057791533] | rolling window (14 d, 90 d), weighted mean |
| Garmin Body Battery | RMSSD-derived stress, sleep, activity | Firstbeat engine, 5 to 100, charges during rest and drains with stress [web: the5krunner, androidauthority]. Formula unpublished. | per-minute stress series, cumulative state over the day |

What substrate needs from these: none of the formulas are public, so the
useful thing is to store the vendor's score as a mirror value (WHOOP is already
mirrored, `kinds/providers.substrate.reamde.dev/whoop/` [repo]) AND make the
inputs cheap to recompute: one value per night per metric, and rolling
baselines over 7, 14, 30 and 60 to 90 nights.

### 1.2 HRV baselines

- Sport science uses ln(RMSSD) because RMSSD is right-skewed; the log is near
  normal [web: Plews et al. 2012/2013 via researchgate 235906316, ryun.app].
- Plews and Buchheit: 7-day rolling mean of ln(RMSSD) against a 60 to 90 day
  baseline; "smallest worthwhile change" (SWC) is 0.5 x SD of the baseline;
  the coefficient of variation (CV = SD / mean) of the 7-day window is itself a
  signal (rising CV means instability) [web: researchgate figure 221863314,
  airbone42/360-data-athlete methodology].
- Computation per day d: `x_d = ln(rmssd_night_d)`; `m7 = mean(x_{d-6..d})`;
  `m60, s60 = mean, sd(x_{d-59..d})`; `z = (x_d - m60) / s60`;
  `cv7 = sd7 / m7`; band = `m60 +- 0.5*s60`.
- Primitives: per-session statistic (one HRV value per night, chosen by the
  vendor or computed from 5-minute RMSSD samples during sleep), a
  **transform before aggregate** (`ln`), rolling mean and SD over N
  observations or N days, z-score.

### 1.3 Sleep debt and sleep regularity

- Sleep debt: `sum over last 14 nights of (need - actual)`, need often a
  per-user constant or vendor value. Primitive: daily sum, rolling sum.
- Sleep Regularity Index (Phillips et al. 2017): the probability of being in
  the same state (asleep or awake) at two time points 24 hours apart, scaled 0
  to 100 [web: nature.com s41598-018-32402-5, DiMe emerging measure PDF].
  Computation: resample the sleep/wake interval stream to a fixed epoch (30 s
  or 1 min), compare each epoch with the epoch 24 h later, average agreement.
  Primitives: **intervals to a regular grid** (state at each epoch), lag
  join, mean. This is the one metric here that is not a bucket aggregate.
- Midpoint and its SD (social jet lag): per-session statistic plus SD over
  a window, and it needs local clock time, not UTC (section 8).

### 1.4 Resting heart rate trend

Per night: min or a low percentile (e.g. 5th) of HR during sleep, or the
vendor's RHR. Then a 7-day mean and a 30 to 60 day baseline. Primitives:
filter samples to a session's time range (a join of HR samples with the sleep
session interval), percentile, rolling mean.

### 1.5 Training load

- Performance Management Chart: daily TSS (training stress score), CTL =
  42-day EWMA, ATL = 7-day EWMA, TSB = CTL - ATL;
  `CTL_t = CTL_{t-1} + (TSS_t - CTL_{t-1}) / 42` or the `e^{-1/42}` form
  [web: trainingpeaks.com science-of-the-performance-manager,
  markliversedge.blogspot.com Banister in GoldenCheetah].
- Acute:chronic workload ratio (ACWR): 7-day over 28-day load, rolling or
  EWMA. Its use for injury prediction is disputed (mathematical coupling,
  no evidence for load management) [web: Impellizzeri et al. 2020,
  PMC8138569]. Still asked for by apps.
- TSS itself is per session: `(sec x NP x IF) / (FTP x 3600) x 100` where NP
  is normalized power, a 30-second rolling mean raised to the 4th power,
  averaged, 4th root [recalled]. TRIMP is the HR-based equivalent [recalled].
- Primitives: per-session statistics over a 1 Hz power stream (rolling 30 s
  mean, power mean), **daily sum with gap filling to zero** (EWMA needs a
  value every day, rest days are 0, not missing), **EWMA over days** (a
  recursive window: not expressible as a plain SQL window aggregate, needs a
  recursive CTE, a custom aggregate, or client-side).

### 1.6 Zone time per week

Seconds in each HR zone (zones from max HR or LTHR, user-specific and
changing over time) per local week. Computation: for each sample, duration
until the next sample (capped, e.g. 10 s) assigned to the zone of its value;
sum per zone per week. Primitives: **time-weighted** aggregation
(duration-weighted, not count-weighted, because sampling rate changes between
rest and workout), histogram with caller-supplied bucket edges, local-week
buckets with a chosen week start.

### 1.7 Step and energy daily totals

Sum of interval records (steps per minute, kcal per minute) per local day.
The hard part is duplicates: phone and watch both count steps. Health Connect
deduplicates only activity and sleep data, by the user's app priority, inside
the aggregate call [web: developer.android.com aggregate-data]. Apple's
`HKStatisticsQuery` merges sources by default and `separateBySource` splits
them [web: Apple HKStatisticsOptions JSON]. Open Wearables picks one ranked
source per series type with an opt-in `filter_by_priority` [web:
github.com/the-momentum/open-wearables PR 1628]. Primitives: sum, **source
priority** (per metric, per time slice), local-day buckets, intervals that
straddle a bucket edge (prorate or assign by start).

### 1.8 Weight trend

Hacker's Diet: exponentially smoothed moving average with 10% smoothing,
`trend_t = trend_{t-1} + 0.1 x (w_t - trend_{t-1})`, over daily weighings
[web: fourmilab.ch hackdiet signalnoise]. Missing days carry the trend forward.
Primitives: one value per local day (first weighing of the morning, or mean),
LOCF, EWMA per observation.

### 1.9 Glucose (CGM)

- Time in range: percent of readings in 70 to 180 mg/dL; also below 54, 54 to
  69, 181 to 250, above 250 (international consensus, Battelino et al. 2019)
  [web: TIR definition via clinicaltrials SAP; ranges recalled].
- Mean, SD, CV = SD / mean (target 36% or lower) [recalled for the 36%].
- GMI (%) = 3.31 + 0.02392 x mean glucose (mg/dL) [web: Bergenstal 2018 via
  diabetesjournals.org care/42/4/e60, provider.dexcom.com].
- Typically over 14 days with at least 70% sensor wear [recalled].
- Primitives: mean, SD, count, **count where value in range** (a
  conditional aggregate or a histogram with fixed edges), coverage (count over
  expected count), **unit conversion** (mmol/L x 18.016 = mg/dL [recalled
  factor, glucose molar mass 180.16 g/mol]).

### 1.10 Glucose response after meals

For each meal event (a nutrition record with a time), take the glucose
samples from t0 - 15 min to t0 + 120 min; compute baseline (mean of pre-meal
window), peak, time to peak, delta, incremental area under the curve (iAUC,
trapezoid above baseline) [recalled]. Primitives: **event-anchored windows**
(join a discrete event series to a sample series by relative time),
interpolation to a regular grid, per-window statistics. This is a join
between two kinds, which a records filter cannot express today.

### 1.11 Menstrual cycle phase correlations

Cycle day and phase are derived from period-start events (and optionally
temperature shift). Then: mean nightly temperature, RHR, HRV by cycle day or
phase across many cycles. Primitives: **bucket by a derived calendar**
(cycle day, not civil day), which means grouping by a value computed from
another series. Practically: compute a per-day `cycle_day` series first,
then group by it.

### 1.12 SpO2 nightly minimum, respiratory rate baseline

Per sleep session: min SpO2, percent of time below 90%, mean respiratory
rate. Illness detection: nightly respiratory rate or temperature deviates
from the personal baseline (rolling mean and SD) by more than k SD
(published for COVID-19 detection with WHOOP and Fitbit data, e.g. Mishra et
al. 2020 [recalled]). Primitives: session-scoped min, time-weighted fraction
below threshold, rolling baseline, z-score.

### 1.13 Correlation queries

"Does alcohol the night before change my HRV", "do late workouts hurt deep
sleep". Computation: build a per-day table with one column per variable
(alcohol units from nutrition logs on day d, HRV on night d to d+1; workout
end time on day d, deep-sleep minutes on night d), align by local day with a
lag, then compare groups (mean HRV on drink days vs not) or compute a
correlation. Primitives: **multi-metric alignment on a shared local-day
axis**, lag, per-day reduction of each input with its own function, outer
join (missing on one side stays missing). The statistics themselves (t-test,
Pearson) can be client-side once the aligned table exists; the server's job
is the aligned table.

### 1.14 Lab trends over years

A few hundred analyte results per year at most, but: units vary by lab and
country (glucose mg/dL vs mmol/L, cholesterol mg/dL vs mmol/L, vitamin D
ng/mL vs nmol/L), reference ranges vary by lab and are attached to the result,
values can be qualifiers ("<5", "positive"). FHIR models these as
Observation with `valueQuantity` (UCUM) and `referenceRange` [recalled;
FHIR $stats requires UCUM-coded valueQuantity, web: hl7.org
observation-operation-stats]. Primitives: no bucketing; filter by analyte
code (LOINC), **convert to a canonical unit on read**, show the per-result
reference range, last value and trend. Volume is irrelevant; unit
correctness is everything.

### 1.15 LLM agents

An agent answering "how did I sleep this month" needs ~30 rows (one per night:
duration, efficiency, stages, HRV, RHR) or one row (monthly means), never
30,000 HR samples. A year of 5-second HR is 6.3 million samples [computed];
the substrate records read pages at 500 rows max [CONTEXT.md], so a year read
raw is 12,600 pages. Primitives the agent needs: daily and monthly summaries
with a handful of statistics, a **row budget** (the server refuses or
downsamples a request whose result exceeds N rows), and a
self-describing result (units, bucket bounds, timezone used, sources used).
Google's Health API caps `rollUp` pages at 10,000 points and ranges at 14 days
for heart rate [web: developers.google.com/health rollUp], which shows the
same concern from the vendor side.

---

## 2. Query primitives a general API must support

Derived from section 1. "Must" means at least two uses above depend on it.

| # | Primitive | Must / should | Used by |
|---|---|---|---|
| P1 | Time-bucketed aggregate: `count, sum, avg, min, max, first, last` | must | everything |
| P2 | `stddev`, `variance` (sample), and `percentile(p)` | must | HRV CV, glucose SD and CV, RHR as low percentile |
| P3 | Calendar buckets in a named IANA zone: hour, local day, local week (week start configurable), month, year, with DST (a local day is 23, 24 or 25 h) | must | daily totals, weekly zones, sleep, correlations |
| P4 | Fixed-duration buckets aligned to an origin (1 min, 5 min, 15 min, 1 h) | must | charts, intraday |
| P5 | Aggregation semantics declared per metric: cumulative (sum) vs discrete (avg, min, max) vs time-weighted | must | steps vs HR; Apple `HKQuantityAggregationStyle` has exactly `cumulative`, `discreteArithmetic`, `discreteTemporallyWeighted`, `discreteEquivalentContinuousLevel` [web] |
| P6 | Gap filling: emit empty buckets, fill with `null`, `0`, LOCF, or linear interpolation | must | EWMA over days (0), weight trend (LOCF), charts |
| P7 | Rolling windows over buckets: mean, sum, SD over the previous N buckets | must | HRV 7/60 day, RHR, sleep debt, glucose 14 day |
| P8 | EWMA over buckets with a time constant | should | CTL/ATL, weight trend. Can be client-side once P6 exists |
| P9 | Group by source, filter by source, and resolve by source priority | must | steps, energy, any multi-device metric |
| P10 | Session-scoped statistics: aggregate samples inside the `[at, endsAt)` of another record (sleep, workout, meal window) | must | RHR, SpO2 min, TSS, glucose response |
| P11 | Multi-metric alignment on one bucket axis (one row per bucket, one column per metric) | must | correlations, agent summaries |
| P12 | Conditional aggregates / histograms with caller edges (count or duration where value in [a, b)) | must | TIR, HR zones, SpO2 below 90% |
| P13 | Downsampling for charts to a target point count: M4 (min, max, first, last per pixel column, pixel-exact for line charts, Jugel et al. VLDB 2014) or LTTB (Steinarsson 2013 MSc thesis) [web] | should | charts of raw samples |
| P14 | Unit of the result chosen by the caller (`unit: "mmol/L"`) | must | glucose, labs, weight |
| P15 | Value transform before aggregation (`ln`) | should | HRV |
| P16 | Row budget and an explicit truncation signal | must | agents, public API |

What the API should NOT try to be: a general expression language. PromQL and
Flux show where that ends (section 3). The statistics in section 1 that
need arbitrary math (correlation tests, Banister fitting, SRI) can be done by
the caller, or by a substrate function, once P1 to P12 return compact aligned
tables.

---
## 3. Prior art for the API shape

### 3.1 Apple HealthKit

- `HKStatisticsQuery` (one bucket) and `HKStatisticsCollectionQuery`
  (`quantityType`, `quantitySamplePredicate`, `options`, `anchorDate`,
  `intervalComponents`), results enumerated per interval with
  `enumerateStatistics(from:to:)`; the anchor date fixes where buckets start
  (e.g. Monday 00:00 local for weeks) [web: devfright.com
  how-to-use-the-hkstatisticscollectionquery; Apple page did not render, the
  parameter list is recalled].
- `HKStatisticsOptions`: `separateBySource`, `discreteAverage`,
  `discreteMin`, `discreteMax`, `cumulativeSum`, `mostRecent`, `duration`
  (and deprecated `discreteMostRecent`). Discrete and cumulative options
  cannot be combined; `separateBySource` combines with anything [web:
  developer.apple.com/tutorials/data/documentation/healthkit/hkstatisticsoptions.json].
- The aggregation style is a property of the quantity TYPE, not the query:
  `HKQuantityAggregationStyle` = `cumulative`, `discreteArithmetic`,
  `discreteTemporallyWeighted`, `discreteEquivalentContinuousLevel` [web:
  .../hkquantityaggregationstyle.json]. Asking for a sum of heart rate is an
  error, not a number.
- Units: every value is an `HKQuantity` read with an `HKUnit` the caller
  chooses (`doubleValue(for: .count().unitDivided(by: .minute()))`)
  [recalled]. Storage unit is invisible to the caller.
- Default statistics merge overlapping sources (watch and phone steps) by a
  user-ordered source priority in the Health app [recalled].
- Timezone: `HKMetadataKeyTimeZone` per sample, but HealthKit sets it mainly on
  sleep and workouts, rarely on quantity samples [web: open-wearables issues
  1779 and ios_sdk 69, which report daily summaries split at UTC midnight
  because of this].
- Lesson: (1) aggregation style belongs to the metric's declaration, (2) the
  caller picks the result unit, (3) bucket alignment is an anchor plus calendar
  components, evaluated in a calendar with a timezone.

### 3.2 Android Health Connect

- `aggregate(AggregateRequest(metrics, timeRangeFilter, dataOriginFilter))`,
  `aggregateGroupByDuration(..., timeRangeSlicer = Duration)`,
  `aggregateGroupByPeriod(..., timeRangeSlicer = Period)` [web:
  developer.android.com/health-and-fitness/health-connect/aggregate-data].
- Metrics are constants on the record type, named `<RECORD>.<FIELD>_<FN>`:
  `StepsRecord.COUNT_TOTAL`, `HeartRateRecord.BPM_AVG/BPM_MIN/BPM_MAX`,
  `DistanceRecord.DISTANCE_TOTAL`,
  `ActiveCaloriesBurnedRecord.ACTIVE_CALORIES_TOTAL`,
  `BloodPressureRecord.SYSTOLIC_AVG` [web]. The set of metrics per type is
  closed: there is no percentile, SD or median.
- Period buckets REQUIRE `LocalDateTime` bounds; passing `Instant` throws
  `IllegalStateException` [web]. Duration buckets accept either. That is the
  physical vs civil time split made explicit in the type system.
- Results are sparse (empty buckets are omitted) [web].
- Deduplication by app priority applies to activity and sleep data only;
  other types aggregate everything [web].
- Series records: `HeartRateRecord` holds `samples` (time, bpm) between
  `startTime`/`endTime`, each record carries `startZoneOffset`/`endZoneOffset`
  [web: developer.android.com write-data]. So the storage unit is a chunk of
  samples, with offsets, not a row per sample [recalled that this is the
  "series record" pattern used for HR, speed, power, cadence, steps cadence].

### 3.3 Google Health API (v4), the Fitbit Web API replacement

- One resource shape: `GET /v4/users/me/dataTypes/{type}/dataPoints` with
  `list`, `reconcile` (merged streams across devices), `rollUp` (physical
  time windows) and `dailyRollUp` (civil days) [web:
  tryterra.co blog everything-you-need-to-know-about-google-health-new-api;
  developers.google.com/health/reference/rest/v4].
- `rollUp` body: `range` (closed-open), `windowSize` (protobuf Duration, min 1
  s), `pageSize` (default 1440, max 10,000), `dataSourceFamily`
  (`all-sources`, `google-wearables`, `google-sources`, `self-sources`).
  Ranges are capped at 14 days for heart rate, calories, active minutes and
  90 days for the rest [web: .../dataPoints/rollUp; get-based issue 1635].
- `dailyRollUp` takes a `CivilTimeInterval` and `windowSizeDays`; the result
  carries `civilStartTime`/`civilEndTime` [web: .../dataPoints/dailyRollUp].
- Each rollup value is typed per data type with `{field}_{function}` names
  (e.g. `count_sum`, `bpm_avg`) [web].
- Data points carry `startUtcOffset`/`endUtcOffset`; 5-second HR is the
  default intraday resolution (about 8,700 samples a day) [web: tryterra blog].
- Fitbit Web API sunset: September 2026 [web: tryterra blog]. The older Fitbit
  intraday endpoints were `/1/user/-/activities/heart/date/{date}/1d/{1sec|1min|5min|15min}.json`,
  max 24 h per call [web: dev.fitbit.com intraday reference].
- Lesson: the closest existing design to what substrate needs. Physical vs
  civil buckets are two verbs; reconcile (source merging) is its own verb; page
  and range caps keep it cheap.

### 3.4 Oura, WHOOP, Garmin, Terra, Junction (Vital), Open Wearables

- Oura v2: daily documents (`daily_sleep`, `daily_readiness`,
  `daily_activity`, `daily_spo2`) by `start_date`/`end_date`, and one raw
  stream (`heartrate`) by `start_datetime`/`end_datetime` [web:
  cloud.ouraring.com/v2/docs and community references]. No aggregation
  endpoint: the vendor ships the aggregates it wants you to see. Oura's own
  documents embed sample arrays (e.g. sleep `heart_rate.items` with
  `interval` and `timestamp`) [recalled; see researcher on Oura schemas,
  `/tmp/health-research/oura-schemas.txt`].
- WHOOP v2: cycles, recovery, sleep, workout as summary documents; no raw HR
  [repo: whoop provider; recalled].
- Garmin Health API: push-based summaries (dailies, epochs at 15 min, sleeps,
  activities with samples, body composition) [recalled, unverified].
- Terra: normalized `Activity`, `Body`, `Daily`, `Sleep`, `Nutrition`,
  `Menstruation` documents, each with a `summary` block and a `detailed`
  block of sample arrays, e.g. `heart_rate_data.summary.avg_hr_bpm` and
  `heart_rate_data.detailed.hr_samples` [web: docs.tryterra.co data-models].
  This is the chunk-with-summary pattern (option d in section 6).
- Junction (formerly Vital): `GET /v2/timeseries/{user_id}/{metric}/grouped`
  with `start_date`, `end_date`, `provider`, cursor; samples carry `start`,
  `end`, `value`, `unit`, `timezone_offset` (seconds), grouped by provider
  with source metadata [web: docs.junction.com timeseries/steps]. No
  server-side bucketing found [unverified that none exists].
- Open Wearables (MIT, Postgres + Redis, FastAPI): one
  `data_point_series` table with `value Numeric(10,3)` (which quantizes GPS to
  about 110 m, issue 1587) [web]; `/timeseries` gained `resolution` = raw, 1
  min, 5 min, 15 min, 1 h, aggregated in SQL per (bucket, source, type)
  with `avg` for rates and `sum` for counters (`AGGREGATION_METHOD_BY_TYPE`),
  UTC buckets only, daily totals left to `/summaries` which resolves the local
  day [web: PR 1624]; `filter_by_priority` picks one ranked source per series
  type [web: PR 1628]. A direct small-scale precedent for substrate, including
  its two mistakes to avoid: a fixed-scale numeric column for every metric,
  and UTC-only intraday buckets.

### 3.5 General series query languages

- **PromQL**: instant vectors and range vectors; `*_over_time` family
  (`avg, min, max, sum, count, quantile, stddev, stdvar, last, first,
  present`), `rate`/`increase` with counter-reset handling; a range query takes
  `start`, `end`, `step` [web: prometheus.io querying/functions]. Calendar
  functions (`day_of_week`, `hour`, ...) are UTC only [web]. Lesson: the
  `step` + `*_over_time` split (bucket width vs lookback window) maps cleanly
  to P4 and P7; the UTC-only calendar is why PromQL is wrong for "per local
  day".
- **InfluxDB Flux**: `aggregateWindow(every, fn, createEmpty, location)` with
  a `timezone.location(name: "Europe/...")` for calendar windows [web:
  docs.influxdata.com flux aggregatewindow]. Flux is in maintenance mode and
  not supported in InfluxDB 3, which moved to SQL and InfluxQL [web:
  docs.influxdata.com flux future-of-flux]. Lesson: a bespoke pipeline
  language did not survive; declarative requests compiled to SQL did.
- **TimescaleDB hyperfunctions**: `time_bucket(width, ts, timezone, origin,
  offset)`, `time_bucket_gapfill` with `locf()` and `interpolate()` (timezone
  argument since 2.9), and in the separate Toolkit extension `stats_agg`,
  `percentile_agg`/`uddsketch`/`tdigest`, `counter_agg`, `gauge_agg`,
  `time_weight`, `heartbeat_agg`, `lttb`, `asap_smooth`, `candlestick_agg`
  [web: tigerdata.com docs, github.com/timescale/timescaledb-toolkit;
  timezone gapfill since 2.9 per issue threads]. `time_bucket_gapfill` with a
  timezone has a string of DST bugs (issues 2462, 5334, 6788, 6844, 7287,
  8998, the last about bucket assignment at DST end) [web]. Lesson: the
  vocabulary of functions is the right one; the DST bugs show local-day
  bucketing is subtle even for a specialist.
- **Grafana**: every panel query carries a time range, `maxDataPoints`
  (pixel budget) and `intervalMs`, exposed to the datasource as `$__interval`
  so the backend picks a bucket width that yields about one point per pixel
  [web: grafana.com query-transform-data; data_source HTTP API]. Lesson: P13
  and P16 as two request fields.

### 3.6 FHIR

- FHIR search returns resources, not aggregates. The R4 Observation `$stats`
  operation exists (subject, code/system/coding, duration in hours or period,
  `statistic` codes: average, maximum, minimum, count, median, std-dev, sum,
  variance, 20th/80th percentile, quartiles, ... ; `include` to return source
  observations) but it is Trial Use, maturity 3, one bucket per call, and only
  over UCUM `valueQuantity` [web: hl7.org/fhir/R4/observation-operation-stats.html].
  `$lastn` returns the last N observations per code [recalled]. Few servers
  implement `$stats` [recalled, unverified].
- The workaround in practice is to flatten FHIR to tables and run SQL: SQL on
  FHIR v2 `ViewDefinition` (FHIRPath columns, `forEach` unnesting, one
  resource type per view) published under CC0, adopted into HL7 ballot, with
  ViewDefinition expected in FHIR R6 core [web: sql-on-fhir.org/ig/2.0.0,
  build.fhir.org sql-on-fhir-v2, npj Digit Med s41746-025-01708-w], and
  Google's FHIR analytics view layer [web: developers.google.com
  open-health-stack fhir-analytics view-layer].
- Lesson for substrate: the "declared view projected to a table" pattern is
  exactly substrate's derived index pattern. A kind's declaration says which
  values are series values; the engine projects them into a narrow table; a
  small aggregate request runs over that table.

### 3.7 Summary of prior art

| System | Bucket model | Agg semantics | Source handling | Units | Gaps |
|---|---|---|---|---|---|
| HealthKit | anchor + `DateComponents` in a calendar | per type (`HKQuantityAggregationStyle`) | merged by priority, or `separateBySource` | caller picks `HKUnit` | absent |
| Health Connect | `Duration` or `Period` (civil, `LocalDateTime` only) | metric constants per type | dedup by priority for activity/sleep, `dataOriginFilter` | typed values (`Energy`, `Length`) | sparse |
| Google Health v4 | `rollUp` (physical) vs `dailyRollUp` (civil) | `{field}_{fn}` per type | `reconcile`, `dataSourceFamily` | fixed per type | sparse [unverified] |
| Open Wearables | 1m/5m/15m/1h UTC, daily via summaries | avg for rates, sum for counters | `filter_by_priority` | stored per sample | raw |
| PromQL | `step` + lookback | function per query | labels | none | none |
| Flux | `aggregateWindow(every, location)` | function per query | tags | none | `createEmpty` |
| Timescale | `time_bucket(_gapfill)` with tz | SQL aggregates + toolkit | SQL | none | `locf`, `interpolate` |
| FHIR `$stats` | one period per call | statistic codes | n/a | UCUM required | n/a |

---
## 4. A small declarative JSON aggregation request for substrate

This is a sketch to make the requirements concrete, not a settled design. It
assumes the records route stays the one read route [CONTEXT.md; decision 0079
"graphql is removed and the records read is one route", repo], so aggregation
is a second optional parameter beside `filter`, not a new endpoint.

### 4.1 Principles taken from the prior art

1. The aggregation style is declared on the property, not chosen per query
   (HealthKit). A request for `sum` over a discrete property is refused, the
   way a filter operator a type does not declare is refused today
   (`internal/substrate/query.go`: "Exactly the operators the schema declares
   for the property's type are legal") [repo].
2. Physical buckets and civil buckets are different requests (Health Connect,
   Google Health v4). A civil bucket names an IANA zone; the response repeats
   the zone it used.
3. The caller names the result unit (HealthKit). Conversion happens on read.
4. Source handling is explicit: `groupBy: ["source"]` or a priority list
   (HealthKit `separateBySource`, Open Wearables `filter_by_priority`).
5. A pixel or row budget is part of the request (Grafana `maxDataPoints`,
   Google `pageSize` max 10,000).
6. The filter grammar is reused verbatim to select what is aggregated AND to
   select session records that act as buckets. No second filter language.

### 4.2 Shape

```json
GET /api/v1/records?filter=...&aggregate=...

filter:
{
  "kinds": ["alice.example.com/health/heartrate"],
  "properties": { "source": { "eq": {"ref": "providers.substrate.reamde.dev/whoop/account/acct1"} } }
}

aggregate:
{
  "value": "bpm",
  "range": { "gte": "2025-01-01T00:00:00+02:00", "lt": "2026-01-01T00:00:00+02:00" },
  "bucket": { "calendar": "day", "timezone": "Europe/Athens" },
  "functions": ["avg", "min", "max", "count", "stddev", "p05"],
  "unit": "/min",
  "fill": "null",
  "rolling": { "buckets": 7, "functions": ["avg", "stddev"] },
  "groupBy": ["source"],
  "maxBuckets": 400
}
```

Variants, one field changed each:

- Fixed width: `"bucket": {"every": "PT5M"}` (ISO 8601 duration, which is
  already a substrate property type), optional `"origin"`.
- Week: `"bucket": {"calendar": "week", "weekStart": "monday", "timezone": "..."}`.
- Session-scoped (P10): `"bucket": {"records": {"kinds": [".../health/sleep"]}}`.
  Each matched record's `[at, endsAt)` is one bucket and the response row
  carries that record's reference. Meal response windows add an offset:
  `"records": {...}, "window": {"before": "PT15M", "after": "PT2H"}`.
- Histogram (P12): `"functions": [{"durationIn": [70, 180]}, {"countIn": [null, 54]}]`.
- Source priority (P9): `"sources": {"priority": ["<ref A>", "<ref B>"]}` picks,
  per bucket, the first source with data. Per-bucket rather than per-sample,
  which is what Open Wearables does per series type [web, PR 1628];
  per-sample overlap resolution (Health Connect style) is harder and should
  wait for a real need.
- Downsampling (P13): `"bucket": {"points": 800, "method": "m4"}` returns at most
  4 x 800 raw points chosen by M4; `"method": "lttb"` returns 800.
- Alignment (P11): `"series": [ {...one aggregate...}, {...another...} ]`
  sharing one `bucket`; the response has one row per bucket and one column per
  named series. This is the request a correlation query or an agent summary
  needs. Each series carries its own `filter`.

Response:

```json
{
  "aggregate": {
    "unit": "/min",
    "timezone": "Europe/Athens",
    "bucket": {"calendar": "day"},
    "truncated": false,
    "rows": [
      { "start": "2025-03-30T00:00:00+02:00", "end": "2025-03-31T00:00:00+03:00",
        "source": {"ref": "..."},
        "values": { "avg": 61.2, "min": 48, "max": 151, "count": 16560, "stddev": 9.1, "p05": 50 },
        "rolling": { "avg": 60.4, "stddev": 1.7 } }
    ]
  }
}
```

The 2025-03-30 row is the DST day in Athens: 23 hours, so `count` is 16,560 at
5-second sampling, not 17,280. Returning `start` and `end` with offsets makes
that visible to the caller.

### 4.3 What it deliberately leaves out

- EWMA, regressions, correlation tests, SRI: a function (substrate's sandboxed
  Python runtime) or the client computes them from aligned rows. They are a
  few lines each once P1 to P12 exist.
- Arbitrary expressions (`value * 2 + other`). PromQL and Flux are the warning;
  Flux is now in maintenance mode [web].
- Joins between arbitrary kinds. The only join is "samples inside the time
  range of matched records", because that is the one every use in section 1
  needs, and it mirrors how a reference is the only link between records.

### 4.4 Where it applies

The same request works over any kind with a declared numeric property and a
`temporal` trait: money per month for an expenses kind, tasks closed per week.
Health is the motivating case, not the only one. It must read from an index
the engine maintains (declared = indexed); `props->>'bpm'` over the jsonb
`records` table is option (a) below and is too slow and too large at health
volumes.

---
## 5. Volume estimates

### 5.1 A heavy user, per day

| Stream | Rate | Samples per day |
|---|---|---|
| Heart rate at rest | every 5 s for 22.5 h (Google Health's default intraday rate is 5 s [web]) | 16,200 |
| Heart rate in workouts | 1 Hz for 1.5 h | 5,400 |
| HRV (RMSSD) | every 5 min | 288 |
| SpO2 | every minute for 8 h of sleep | 480 |
| Respiratory rate and skin temperature | every minute for 8 h | 960 |
| Steps | per minute | 1,440 |
| Active energy | per minute | 1,440 |
| Stress / body battery | every 3 min | 480 |
| Sleep stage intervals | per night | about 40 |
| CGM glucose | every 5 min | 288 |
| Ride streams (lat, lon, altitude, power, cadence, speed) | 1 Hz x 6 channels x 1.5 h, 4 days of 7 | 18,514 averaged |
| Scale (weight and 9 body composition fields) | once | 10 |
| Labs | about 200 results a year | under 1 |
| **Total** | | **about 45,500 a day** |

That is about **16.6 million samples a year** and **166 million a decade**
for one user [computed]. A moderate user (no CGM, no power meter, 1-minute
HR) is closer to 3,000 to 5,000 a day, 1 to 2 million a year [computed].

### 5.2 Bytes per sample under different encodings

| Encoding | Bytes per sample | Per year (16.6M) | Source |
|---|---|---|---|
| substrate today: one record per sample, records table with its ten indexes | 531 | 8.8 GB | [bench] 6,307,200 rows = 3,349 MB |
| plus the `changelog` table row and the segment file line (about 700 to 1,000 B each; the smallest `put` line in a dev repository is 672 B, and a line carries one `manager` fold entry per property) | about 1,500 to 2,000 more | 25 to 33 GB | [repo] line sizes measured in a dev data root; per-sample estimate |
| narrow row, btree PK `(repository text, series int, ts)` | 118 | 2.0 GB | [bench] 710 MB |
| narrow row, monthly partitions, BRIN on `ts` only | 68 | 1.1 GB | [bench] 412 MB |
| one jsonb record per (series, day), `t` and `v` arrays, TOASTed | 9.7 | 161 MB | [bench] 61 MB for 365 days |
| one row per (series, day), `int4[]` offsets + `int2[]` values | 5.5 | 91 MB | [bench] 35 MB |
| JSON text of the same day chunk (what a changelog line would carry) | 10.9 | 181 MB | [bench] 188,045 B per day of 17,280 samples |
| Oura-style regular series (`timestamp`, `interval`, `items`), JSON text | about 3 to 4 | about 60 MB | [computed; Oura `PublicSample` schema, oura-schemas.txt] |
| TimescaleDB compressed hypertable (segmentby repository, series; orderby ts) | 2.65 | 44 MB | [bench] 688 MB before, 16.7 MB after: 97.6% smaller |
| Gorilla (delta-of-delta timestamps, XOR floats) | 1.37 average on Facebook's monitoring data | 23 MB | [web] VLDB 2015 p1816 |
| 15-minute rollup rows (n, sum, min, max, sum of squares) | 5.3 MB per series per year, independent of sample rate | | [bench] 35,040 rows |

Reading the table: the cost of substrate's current model is not the sample
value, it is the per-record envelope, ten indexes, and the changelog line.
At heavy-user rates, one record per sample costs roughly 35 to 45 GB a year
across Postgres and the segment files [bench + estimate], and about 47
segment files of 256 MiB a year [computed: 45,500 x 750 B x 365].

---

## 6. Storage options in Postgres, measured

### 6.1 Benchmark setup [bench]

- Postgres 16.15, TimescaleDB 2.30.2 (`timescale/timescaledb:latest-pg16`;
  pgvector 0.8.1 is also available in that image), 8 CPUs and 8 GiB given to
  the container, `shared_buffers=2GB`, `work_mem=64MB`, JIT off. The VM's disk
  is slow (bulk inserts were WAL-write bound), so load times are pessimistic;
  queries were run warm.
- Data: one year (2025) of heart rate every 5 s, 6,307,200 samples, one
  repository, one source.
- The headline query: average HR per day for a year. Two flavours: UTC days,
  and local days in `Europe/Athens` (which includes the 23-hour 2025-03-30 and
  the 25-hour 2025-10-26).
- Scripts and raw output: `/tmp/health-research/bench/setup.sql`,
  `query.sql` to `query4.sql`, `query*.out`.

### 6.2 Results

| Option | Size for 6.3M samples | Avg per UTC day, year | Avg per Athens day, year | Avg over the year, no buckets | One day raw (17,280 rows) | Update one sample |
|---|---|---|---|---|---|---|
| (a) records table, one record per sample, `props->>'bpm'` | 3,349 MB (heap 1,895, indexes 1,453) | 7.3 s | 5.3 s serial, 2.5 s with 4 workers | 0.77 s | not measured | one put (plus a changelog line) |
| (b) narrow table, btree PK | 710 MB (heap 411, index 299) | 2.4 s | 3.9 s serial, 2.7 s with 4 workers | 0.26 s | 5.3 ms | 1.5 ms |
| (b2) narrow, monthly partitions, BRIN | 412 MB | not measured | 4.0 s | not measured | not measured | not measured |
| (c) TimescaleDB, compressed | 16.7 MB (688 MB before) | **0.23 s** (vectorized) | 4.6 s serial, 1.3 s with 4 workers | 0.06 s | not measured | 5.4 ms (decompresses a segment) |
| (c') TimescaleDB, UTC-hour buckets then re-bucket to Athens days | same | | **0.15 s** | | | |
| (d) jsonb array chunk per day: precomputed `stats` only (UTC days) | 61 MB | **1.3 ms** | n/a (chunk is a UTC day) | | | rewrite the chunk |
| (d) jsonb array chunk per day: unpack with `jsonb_array_elements_text` and re-bucket | 61 MB | | 9.6 s | | | |
| (d2) native array chunk per day, `unnest` and re-bucket | 35 MB | 1.3 s | 4.7 s | | 6.6 ms | 1.2 ms (array element) |
| (e) 15-minute rollups derived from any of the above (n, sum, min, max, sum of squares) | 5.3 MB | | **26 ms** (avg, min, max, SD) | | | re-derive one 15-minute row |

Further measurements [bench]:

- Local-day bucketing, not the scan, is the dominant cost for the raw tables:
  `date_trunc('day', ts, 'Europe/Athens')` per row costs more than the
  aggregate. On TimescaleDB a timezone argument to `time_bucket` drops the plan
  out of its vectorized aggregation (`VectorAgg` appears only for the UTC
  form). Bucketing in UTC hours first and re-bucketing the hours into local days
  gives the correct local-day answer (for whole-hour offsets) at 0.15 s instead
  of 1.3 to 4.6 s.
- The same two-stage trick on 15-minute rollups covers every real offset
  (+05:30, +05:45): a `Asia/Kolkata` day over the year took 25 ms.
- A 7-day rolling mean of daily averages over the year, from rollups: 25 ms.
- M4 downsampling of one week (120,960 samples) to 800 pixel columns from
  the narrow table: 176 ms.
- Rebuild cost of a derived narrow table from chunk records: 6.2 s to unnest
  6.3M samples from native arrays, 11.6 s from jsonb arrays, plus 9.5 s to
  build the btree. Rebuilding the 15-minute rollups from the narrow table:
  2.3 s. These are SQL bulk paths; a rebuild through substrate's fold (one
  changelog line at a time, in Go) is slower per line, which is why the number
  of LINES matters more than the number of samples.
- TimescaleDB compression of 53 weekly chunks took 39 s on this disk.

### 6.3 The options against substrate's constraints

#### (a) One record per sample in the records table (today)

- Write amplification: one changelog line (700 to 1,000 B) and one changelog
  table row per sample; about 47 segments a year for a heavy user.
- Rebuild: replays every sample through the fold; at 16.6M lines a year, a
  decade is 166M fold steps [computed]. Not measured here; substrate's fold
  throughput per line would decide it.
- Query: 2.5 to 7.3 s for one year of one metric with the jsonb cast
  [bench]; the GIN index on `props` does not help range aggregates.
- Index size: 1.45 GB of indexes for 6.3M samples [bench], mostly the ten
  generic indexes every record row carries.
- Correction: natural (one put on one record).
- Verdict: correct and simple, unaffordable at wearable rates. Fine for
  low-rate data: labs, weighings, daily summaries, sessions.

#### (b) A dedicated narrow samples table as the primary store

- Fast and small (68 to 118 B per sample [bench]); BRIN on time is about
  1000x smaller than a btree (Crunchy Data: 24 kB vs 21 MB on 1M rows) and
  competitive for large range scans, slower for small ones (11 ms vs 0.5 ms at
  100 rows, improved by `pages_per_range` 8 to 32) [web:
  crunchydata.com postgres-indexing-when-does-brin-win].
- Native partitioning by time (monthly) with `pg_partman` 5.x to create and
  retire partitions [web: github.com/pgpartman/pg_partman]. Across many
  repositories in one table, partition by time and keep `repository` leading
  in the index, or hash-partition by repository; substrate's row-level
  security on `repository` [repo: migrations 0001, 0005] applies either way.
- The problem: as the PRIMARY store it bypasses the changelog. Substrate's
  rule is that anything writing `records` directly, or a changelog entry
  reaching one store and not the other, is wrong [repo: CLAUDE.md "The
  changelog is the truth"]. A samples table that is the truth would be a
  second truth. Rejected as primary; it is the right shape for the DERIVED
  index in (e).

#### (c) TimescaleDB hypertable with compression and continuous aggregates

- Measured 97.6% size reduction and 0.06 to 0.23 s full-year aggregates
  [bench]; vendor and user reports say 90 to 98% [web: tigerdata.com docs and
  blog; dev.to polliog 150 GB to 15 GB]. Compression uses delta-of-delta plus
  Simple-8b with RLE for integers and timestamps, XOR (Gorilla-style) for
  floats, dictionary and LZ for the rest [web: tigerdata.com
  time-series-compression-algorithms-explained].
- Continuous aggregates refresh only the invalidated buckets, can be stacked
  (hierarchical), and real-time aggregation is off by default since 2.13
  [web: docs.timescale.com hierarchical continuous aggregates]. They bucket in
  one fixed timezone and have had timezone bugs (issues 1248, 6902) [web].
- Licence: core (hypertables, `time_bucket`) is Apache 2.0; compression,
  continuous aggregates, retention and job policies, gapfill and the Toolkit
  (`stats_agg`, `percentile_agg`, `lttb`, ...) are under the Timescale License
  (TSL), free to self-host, not allowed to be offered as a database service
  [web: github.com/timescale/timescaledb tsl directory; railway.com;
  timescaledb-toolkit LICENSE]. Whether a hosted "substrate as a service"
  counts as offering the database is a legal question this research did not
  settle [unverified].
- Coexistence with pgvector: fine. The `timescale/timescaledb` image used
  here ships pgvector 0.8.1 [bench]; `timescaledb-ha` ships pgvector,
  pgvectorscale and the Toolkit [web: tigerdata docs, pgvectorscale README].
  Substrate's compose file runs `pgvector/pgvector:pg16` [repo: compose.yaml],
  so adopting Timescale means changing the image and every operator's
  Postgres. Several managed Postgres offerings do not ship TimescaleDB, or
  ship only the Apache edition (AWS RDS does not offer it; Azure offers the
  Apache edition) [recalled, unverified].
- Updates to compressed data work (5.4 ms for one row [bench]) but rewrite or
  decompress a segment.
- Fit: as a DERIVED index backend it is excellent; as a hard dependency it
  conflicts with "a laptop and a stock Postgres run it". It can be optional:
  the derived table is a hypertable when the extension exists, a plain
  partitioned table otherwise. `date_bin` (Postgres 14+) is the stock
  equivalent of fixed-width `time_bucket` [recalled].

#### (d) Chunked series records

One record per (series, source, period) holding the samples as arrays, plus
summary statistics on the record.

- It is what the vendors already ship: Oura's `PublicSample` (`timestamp`,
  `interval`, `items` with nulls) [oura-schemas.txt], Terra's
  `detailed.hr_samples` beside a `summary` [web], Health Connect's
  `HeartRateRecord.samples` [web], HealthKit's series samples built with
  `HKQuantitySeriesSampleBuilder` [web: Apple JSON]. A mirror kind that keeps
  the upstream's shape [CONTEXT.md] is already a chunk.
- Chunk size is the main knob. Health Connect tells writers to create one
  `HeartRateRecord` per MINUTE rather than a day-long record that is
  constantly updated [web: developer.android.com write-data]. For substrate
  the cost is per changelog line, so the knob is: a chunk is closed and
  immutable once its period ends, and a sync writes new chunks rather than
  rewriting an open one. One chunk per (series, source, hour) gives about 24
  lines a day per series, each tens of kB; one per sync page matches how the
  provider fetches.
- Rewriting an open day chunk on every hourly sync would write the day about
  12 times over on average [computed: 24 rewrites of a chunk growing linearly,
  mean half a day each].
- Size: 5.5 to 10.9 B per sample stored, 10.9 B per sample in a JSON
  changelog line [bench]; about 3 to 4 B with Oura's regular-interval form
  [computed]. A binary Gorilla or Parquet encoding inside a `blobref` would be
  smaller (1.4 B [web]) but opaque to Postgres, to the fold and to anyone
  reading the changelog.
- Fits under the caps: an hour of 1 Hz data across 6 channels is 21,600
  samples, about 235 kB as JSON [computed from the 10.9 B measurement], below
  the 1 MiB request cap [repo: `internal/api/core.go` `maxRequestBody = 1 << 20`]
  and far below the 64 MiB segment line cap [repo:
  `internal/changelogfile/reader.go` `MaxLineBytes = 64 << 20`]. A whole day of
  1 Hz HR (86,400 samples, about 940 kB) is too close to the 1 MiB cap.
- Querying chunks directly: precomputed stats answer "avg per chunk period"
  in about 1 ms [bench], but only in the chunk's own period and zone;
  anything else means unpacking (4.7 to 9.6 s for a year [bench]).
- Correction of one sample: a new version of the chunk (one line, tens of
  kB). Rare in practice; upstream corrections usually arrive as re-sent
  periods, which map one-to-one onto chunk rewrites.

#### (e) Hybrid: chunks in the changelog, a derived narrow index for queries

The changelog holds chunk records (d). The fold, on each chunk write,
re-derives that chunk's rows in a narrow `series` table (b) and the affected
rollup rows, exactly as it re-derives `refs` and `fts` today [repo:
`internal/engine/fold.go` lines 494 to 515: "The refs index is a PROJECTION
of the row that just landed, like `fts` ... Deriving it HERE is what makes a
rebuild reproduce it byte for byte"].

- Write amplification: one changelog line per chunk, not per sample: 0.5 MB a
  day of JSON for the heavy user [computed: 45,500 x 10.9 B], under one
  256 MiB segment a year. The narrow table and rollups are index writes in
  Postgres only, never in the segment files.
- Rebuild: replay chunk lines (about 240 to 1,000 a day, not 45,500), each
  fanning out to its samples in SQL. Measured bulk fan-out: 6.2 to 11.6 s per
  6.3M samples, plus index builds [bench]. The same function serves the live
  write, the boot import and `RebuildRepository`, which is the existing rule
  for `fold.go`.
- Query: from 15-minute rollups, any local day, week or month for a year in
  about 25 ms; raw ranges for charts in about 5 ms a day, M4 for a week in
  about 0.2 s [bench].
- Index size: the narrow table is the largest piece (68 to 118 B per sample,
  1.1 to 2 GB a year for the heavy user [bench + computed]); it can be
  compressed with TimescaleDB where present (2.65 B per sample [bench]) or
  pruned to rollups for old data, because it is derived and rebuildable from
  the chunks.
- Correction: a new chunk version; the fold deletes and re-derives that
  chunk's rows and its rollup buckets.
- Declared = indexed: only properties a kind declares as series values are
  projected. The projection is driven by the declaration, like `fts` bands
  and reference sites.
- Events vs state: chunk records are STATE (the set of samples for a period),
  so a rewrite replaces the period. That matches the fold's put-and-merge
  model and keeps corrections idempotent. An "append these samples" event op
  would shrink lines for open chunks but adds a new op kind to the changelog;
  closing chunks by period avoids needing it.

### 6.4 Postgres features that matter

| Feature | Why it matters here | Evidence |
|---|---|---|
| jsonb size and TOAST | rows above about 2 kB get compressed or moved out of line; queries on TOASTed values are 2 to 10x slower; lz4 TOAST compression is faster than pglz on PG14+ | [web: evanjones.ca postgres-large-json-performance]; jsonb rows take about 2x the space of typed columns [web: heap.io when-to-avoid-jsonb] |
| row overhead | 23 to 24 B tuple header plus 4 B line pointer per row: the floor for a one-row-per-sample design | [web: heap.io; postgresql.org list] |
| `numeric` vs `float8` | numeric is variable-length and software arithmetic, about 1.2 to 2x slower in `sum` | [web: dev.to tbson87; pgedge.com] |
| `timestamptz` | 8 bytes, UTC instant; no zone stored, so the offset must be its own column | [recalled] |
| BRIN | tiny index for append-ordered time columns; tune `pages_per_range` | [web: crunchydata] |
| declarative partitioning, `pg_partman` | retire or compress old partitions; partition pruning on time ranges | [web: pg_partman] |
| parallel query | 4 workers took the Athens-day query from 5.3 s to 2.5 s on (a) and 4.6 s to 1.3 s on (c) [bench]; the planner did not choose parallel on its own for (b) until `parallel_setup_cost` was lowered [bench] | |
| `work_mem` | hash aggregates over many groups (per-minute buckets for a year: 525,600 groups) | [recalled] |
| materialized views | full refresh only; useless for per-write freshness | [recalled] |
| `pg_ivm` 1.x | immediate (trigger-time) incremental views; only `count, sum, avg, min, max`, no window functions or outer-join aggregates; slows base-table writes | [web: github.com/sraoss/pg_ivm; postgresql.org pg_ivm 1.13 news] |
| Postgres 18 | async I/O for sequential and bitmap scans; btree skip scan; virtual generated columns by default | [web: postgresql.org release 18.0] |
| `pg_duckdb` 1.0 | DuckDB's vectorized engine inside Postgres, `read_parquet` | [web: github.com/duckdb/pg_duckdb releases v1.0.0; motherduck.com] |
| `pg_mooncake` | columnstore mirror of a rowstore table in Iceberg, built on pg_duckdb plus Moonlink; v0.2 aimed at append-only time series; Mooncake Labs was acquired by Databricks in October 2025 | [web: docs.mooncake.dev; storagenewsletter.com 2025-10-22; dbta.com] |
| `pg_analytics` (ParadeDB) | archived 2025-03-19, discontinued | [web: github.com/paradedb/pg_analytics] |

DuckDB-in-Postgres and Parquet are a poor fit for substrate's model: the
truth is the changelog, the fold writes rows in the same transaction, and a
Parquet file per repository would be a third store with its own consistency
story. They would matter for a cross-repository analytics warehouse, which
substrate does not have.

---
## 7. Units at the query layer

### 7.1 Canonical storage unit vs source units

Two designs:

1. **Canonical per metric.** The projected (user-owned) series stores one unit
   per metric (`/min` for HR, `mg/dL` or `mmol/L` for glucose, `kg` for
   weight). A provider's mapping converts at projection time. Aggregation is a
   plain SQL aggregate. The mirror keeps the upstream's own unit, as substrate
   already requires for mirrors [CONTEXT.md].
2. **Per-source unit, convert on read.** Every sample carries its unit; the
   query converts each row before aggregating. Correct but slower (a CASE or
   join per row), and an `avg` across sources with different units must
   convert first, which defeats any precomputed rollup.

Prior art leans to (1) internally and caller-chosen output units: HealthKit
stores in an internal unit and converts on read to the `HKUnit` asked for
[recalled]; Health Connect stores typed values (`Mass`, `Energy`,
`BloodGlucose`) and exposes `inKilograms`, `inMillimolesPerLiter` [recalled];
FHIR `$stats` requires UCUM `valueQuantity` [web].

Recommendation: canonical unit per metric in the derived series index, the
upstream unit preserved in the mirror, and the aggregate request's `unit`
converting the RESULT (one multiplication per output row, not per sample).
Affine units (Celsius vs Fahrenheit) are the exception that converts per row
for `sum` but not for `avg/min/max`; a declared canonical unit avoids the
question for stored data.

### 7.2 UCUM libraries in Go

| Library | Version, date | Licence | Numbers | Notes |
|---|---|---|---|---|
| `github.com/gofhir/ucum/v4` | v4.2.0, 2026-08-03 | BSD-3-Clause | float64, plus `ExactService` with `*big.Rat` | stdlib only; embeds `ucum-essence.xml` v2.2 (2024-06-17); all 21 special units (Cel, [degF], pH, B); canonical forms; claims all 573 HL7 conformance tests pass; 0 known importers [web: pkg.go.dev] |
| `github.com/mattwiller/mu` | v0.1.0, 2026-08-16 | BSD-3-Clause | float64 and `big.Rat`; companions for shopspring/decimal and apd/v3 | UCUM v2.2, 312 atoms and 24 prefixes; special units; limits: 255-byte codes, 10 nesting levels, 2,048-bit magnitudes; JS/WASM build `mu-conv`; 0 known importers [web: pkg.go.dev] |
| `github.com/iimos/ucum` | v0.0.3, 2025-04-12 | Apache-2.0 | big.Rat, big.Int, float64, shopspring/decimal, apd | code-generated conversion tables; pre-1.0; 4 importers [web: pkg.go.dev] |
| `github.com/bertverhees/ucum` | not checked | not checked | not checked | port of the Java UCUM services [web: pkg.go.dev listing only, unverified] |
| `github.com/robertoaraneda/gofhir/pkg/ucum` | not checked | not checked | not checked | part of a FHIR toolkit [web: pkg.go.dev listing only, unverified] |

All of them are young, with few or no importers. For a closed set of health
units (perhaps 60 to 100 codes), a hand-written table of factors checked
against one of these libraries in a test is lower risk than a runtime
dependency; a UCUM parser becomes worth it when users write their own kinds
with arbitrary units. The exact `big.Rat` path in `gofhir/ucum` and `mu`
fits substrate's "exact numbers or refusal" value: conversion of a `decimal`
can stay exact.

### 7.3 The molar/mass problem

mg/dL to mmol/L is not a unit conversion: it needs the analyte's molar mass
(glucose 180.16 g/mol, so mmol/L x 18.016 = mg/dL; cholesterol about 386.65
g/mol, factor 38.67; triglycerides use 88.57) [recalled factors]. UCUM can
express both units but cannot convert between them alone. The NLM reference
implementation `ucum-lhc` takes an explicit `molecularWeight` option in
`convertUnitTo` for mass to moles and back [web: github.com/LHNCBC/ucum-lhc].
None of the Go libraries above advertise this [web: pkg.go.dev pages do not
mention it]. Consequence for substrate: the molar mass belongs to the
METRIC's declaration (the analyte, e.g. LOINC 2345-7 glucose), not to the
unit system; conversion between mass and substance concentration is allowed
only where the metric declares it.

Other traps [recalled]: `%` for HbA1c (NGSP) vs mmol/mol (IFCC) is a
linear formula, not a factor (`IFCC = 10.929 x (NGSP - 2.15)`); `[iU]` and
arbitrary units are commensurable with nothing; `Cel` is affine.

### 7.4 float vs decimal for measurements

- Sensors produce bounded-precision numbers: HR is an integer, CGM glucose an
  integer mg/dL or one-decimal mmol/L, weight 0.05 to 0.1 kg resolution.
  Measurement error dwarfs float64 rounding (about 1e-16 relative).
- Postgres `numeric` is variable-length and its aggregates are software
  arithmetic; one benchmark measured `sum` over 1M rows at 26 to 37 ms for
  `numeric(12,2)` vs 20 to 22 ms for `double precision`, and older ones about
  2x [web: dev.to tbson87; pgedge.com numeric post]. Parallel `sum` over
  float8 can differ between runs in the last bits [web: same dev.to post].
- Open Wearables used `Numeric(10,3)` for every metric and lost GPS
  precision (about 110 m) [web: issue 1587]: a single fixed-scale decimal is
  the wrong tool for a column shared by all metrics.
- Recommendation: `float8` in the derived series index (aggregation speed,
  compressibility with XOR schemes), and keep the exact upstream value in the
  mirror record (substrate's `decimal` type where the upstream sends a
  decimal string, e.g. lab results). Lab values that carry significant figures
  (`5.40`) keep them in the record; the index only needs the number.

## 8. Timezones

### 8.1 What each platform stores

| Platform | Per-sample zone info | Day bucketing |
|---|---|---|
| HealthKit | `HKMetadataKeyTimeZone` (zone name), set mostly on sleep and workouts, rarely on quantity samples [web: open-wearables issues 1779, ios_sdk 69] | statistics collection query in a `Calendar` (device zone by default) [recalled] |
| Health Connect | `startZoneOffset`, `endZoneOffset` (fixed offsets) on every record [web: write-data] | `LocalDateTime` ranges for Period buckets, required [web] |
| Google Health v4 | `startUtcOffset`, `endUtcOffset` [web: tryterra blog] | `dailyRollUp` over `CivilTimeInterval` [web] |
| Junction | `timezone_offset` in seconds, nullable [web] | n/a |
| Oura | timestamps with offsets; daily documents keyed by `day` [web/recalled] | vendor's day |
| WHOOP | `timezone_offset` on cycles and sleeps [recalled from the mirror kinds, repo] | vendor's cycle, not a calendar day |

Two facts follow. An offset per sample is common and enough to compute the
local wall-clock time at which the sample happened, including during travel.
A zone NAME is rarely present, so "the local day of a sample" is best defined
as `(ts + offset)::date`, the civil date where the user was, not the date in
the user's current home zone.

### 8.2 Bucketing in SQL

- Home-zone buckets: `date_trunc('day', ts, 'Europe/Athens')` (the 3-argument
  form exists since Postgres 12 [recalled]) or Timescale `time_bucket('1 day',
  ts, 'Europe/Athens')`. Correct across DST: the bucket is 23 or 25 hours on
  transition days [bench: 5-second samples per Athens day were 16,560 on
  2025-03-30 and 18,000 on 2025-10-26, against 17,280 on other days]. Wrong across travel.
- Experienced-day buckets: a stored `local_date date` (or `utc_offset int2`
  seconds/60) in the series index, computed at projection time; group by
  `local_date`. Correct across travel, and the index can be keyed on it. A
  travel day where the user crosses zones gets a 20 or 28 hour "day"; that is
  what Google's civil rollups and Health Connect's `LocalDateTime` ranges
  express [web, inference].
- Sleep belongs to the day you wake up (Oura, WHOOP cycles) [recalled]: a
  session kind declares which edge assigns its day. That is a property of the
  session kind, not of the query.
- Timescale continuous aggregates fix ONE timezone at definition time
  [inference from time_bucket signature; issues 1248, 6902]. A per-user
  timezone in a shared table means one aggregate per zone or bucketing by a
  stored local date. The DST bug list for `time_bucket_gapfill` with a
  timezone (section 3.5) argues for the stored `local_date` too.

### 8.3 Recommendation

Store `ts timestamptz` plus `utc_offset_min smallint` (nullable) per sample in
the derived index, derived from the record's own offset or zone property.
Civil-day requests group by `(ts + offset)` date when offsets exist, else by
the request's timezone; the response says which it used per bucket. Physical
buckets ignore offsets.

---

## 9. Recommendation, ranked by fit with substrate's values

The values used to rank: the changelog is the truth and records are its
fold; derived indexes are rebuildable from the fold; declared = indexed =
filterable; one read route; exact numbers or refusal; mirrors keep the
upstream's shape and units.

### Rank 1: (e) chunked series records in the changelog, a derived series index and derived rollups

- **Truth.** A series value is a record: one record per (series, source,
  closed period), holding the samples as arrays in declared properties, with
  the upstream's units in the mirror and the canonical unit in the projected
  user-owned kind. Low-rate data (labs, weighings, sessions, daily
  summaries) stays one record per observation; the same kind can declare
  either shape.
- **Derived index.** The fold projects each chunk into a narrow
  `series_samples (repository, series, ts, utc_offset_min, value float8)`
  table, keyed by an engine-assigned series id for (kind, property, source),
  and maintains 15-minute rollups `(n, sum, min, max, sum of squares)`. Both
  are dropped and re-derived for the chunk on every write, like `refs` and
  `fts`, so live writes, boot import and rebuild cannot drift. Only properties
  a kind declares as series values are projected (declared = indexed).
- **Read.** The aggregation request of section 4 on the existing records
  route: civil-day, week and month answers from rollups (about 25 ms for a
  year [bench]), raw ranges and M4/LTTB from the narrow table (about 5 ms a
  day, about 0.2 s for a week to 800 points [bench]).
- **Why first.** Changelog growth drops from about 47 segments a year to
  under one for a heavy user [computed]; rebuild replays hundreds of lines a
  day instead of tens of thousands; queries meet an agent's and a chart's
  latency budget; nothing new is a source of truth; it reuses the fold's
  existing derived-index pattern.
- **Costs.** A new derived table and its projection code; a declaration
  key (reserved by name, per decision 0020) for "this property is a series"
  and its aggregation style; chunk-closing discipline in providers; the
  narrow table is still about 1 to 2 GB a year for the heaviest user
  [bench + computed], prunable to rollups for old periods because it is
  rebuildable.

### Rank 2: (e) with TimescaleDB as an optional backend for the derived index

Same truth and same API. Where the extension is installed, the derived
narrow table is a compressed hypertable (about 2.65 B per sample, 97.6%
smaller [bench]) and UTC-hour or 15-minute aggregates use its vectorized path
[bench: 0.15 s for a year by local day via hour buckets]. Ranked second only
because it adds an extension, an image change and the TSL question (section
6.3 c); because the index is derived, the choice is reversible per
deployment.

### Rank 3: (d) chunked records alone, statistics precomputed on the record

No new table: each chunk carries `stats` and the API reads them (about 1 ms
for a year of per-chunk averages [bench]). Fits the values perfectly and
fails the uses: any bucket that is not the chunk's own period or zone means
unpacking arrays in SQL (4.7 to 9.6 s for a year [bench]); sessions, rolling
windows and source priority become client work. A reasonable first step that
(e) extends without migrating data.

### Rank 4: (a) one record per sample

Already works, needs nothing new, and is the right model for low-rate
observations. At wearable rates it costs about 35 to 45 GB a year per heavy user
across Postgres and segment files and 2.5 to 7 s per year-long aggregate
[bench + computed]. Keep it for labs, weight, blood pressure, sessions,
summaries; do not use it for intraday streams.

### Rank 5: (c) TimescaleDB hypertable as the primary store

Best raw performance measured [bench], but a hypertable that is the truth
bypasses the changelog: two truths, a rebuild that cannot reproduce it from
the segment files, and a hard dependency on a non-stock extension under the
TSL. Ruled out by "the changelog is the truth".

### Rank 6: (b) a narrow samples table as the primary store

Same objection as (c) without its compression. Its shape is right; its
role is the derived index in rank 1.

### Not recommended

- Parquet or Arrow blobs per repository with `pg_duckdb` or `pg_mooncake`:
  a third store and a second query engine for a single-user workload that
  15-minute rollups already serve in tens of milliseconds [bench].
  `pg_analytics` is archived [web].
- `pg_ivm` for rollups: its aggregate set lacks SD and percentiles and it
  slows every base-table write [web]; the fold already knows exactly which
  buckets a chunk touches.
- Binary Gorilla encoding inside a `blobref`: 1.37 B per sample [web] but
  opaque to SQL, to the fold and to anyone reading the changelog. Revisit
  only if JSON chunk lines prove too large, and then as a declared encoding.

### Open questions for the proposal phase

1. Where the series id lives: an engine-assigned integer per (kind,
   property, source) keeps the narrow table small; the mapping is itself
   derived from declarations and must be rebuildable.
2. Rollup granularity: 15 minutes covers every real UTC offset; hourly is 4x
   smaller and wrong for +05:30 and +05:45 zones. Percentiles need a sketch
   (t-digest, UDDSketch) in the rollup or a raw scan.
3. Whether chunk arrays carry per-sample timestamps (`t`, `v`) or Oura's
   `start + interval + items` form, or both as two declared shapes. The
   regular form is about 3x smaller [computed].
4. Whether the aggregate request returns rows for empty buckets by default
   (Health Connect is sparse [web]; EWMA callers want zeros).
5. Retention of the narrow table for old years (keep rollups only) and
   whether that is a per-repository setting.

---

## 10. Sources

Checked on the web on 2026-10-08:

- Apple HealthKit: `HKStatisticsOptions` https://developer.apple.com/tutorials/data/documentation/healthkit/hkstatisticsoptions.json ; `HKQuantityAggregationStyle` https://developer.apple.com/tutorials/data/documentation/healthkit/hkquantityaggregationstyle.json ; `HKQuantitySeriesSampleBuilder` https://developer.apple.com/tutorials/data/documentation/healthkit/hkquantityseriessamplebuilder.json ; collection query walkthrough https://www.devfright.com/how-to-use-the-hkstatisticscollectionquery/
- Health Connect: aggregate https://developer.android.com/health-and-fitness/health-connect/aggregate-data ; write data (per-minute HR records, zone offsets) https://developer.android.com/health-and-fitness/health-connect/write-data
- Google Health API v4: https://developers.google.com/health/reference/rest/v4/users.dataTypes.dataPoints/rollUp ; https://developers.google.com/health/reference/rest/v4/users.dataTypes.dataPoints/dailyRollUp ; overview https://tryterra.co/blog/everything-you-need-to-know-about-google-health-new-api ; range limits https://github.com/elkimek/get-based/issues/1635
- Fitbit intraday: https://dev.fitbit.com/build/reference/web-api/intraday/get-heartrate-intraday-by-date/
- Oura API v2: https://cloud.ouraring.com/v2/docs ; HRV balance https://ouraring.com/blog/hrv-balance/ ; readiness contributors https://support.ouraring.com/hc/en-us/articles/360057791533-Readiness-Contributors
- WHOOP: https://www.whoop.com/us/en/thelocker/heart-rate-variability-hrv/ ; https://support.whoop.com/s/article/WHOOP-Recovery?language=en_US
- Garmin Body Battery: https://the5krunner.com/garmin-features/sleep/body-battery/ ; https://www.androidauthority.com/garmin-body-battery-1209128/
- Terra data models: https://docs.tryterra.co/reference/health-and-fitness-api/data-models
- Junction timeseries: https://docs.junction.com/api-reference/data/timeseries/steps
- Open Wearables: https://github.com/the-momentum/open-wearables ; issue 1587 https://github.com/the-momentum/open-wearables/issues/1587 ; PR 1624 https://github.com/the-momentum/open-wearables/pull/1624 ; PR 1628 https://github.com/the-momentum/open-wearables/pull/1628 ; HealthKit zone issues https://github.com/the-momentum/open-wearables/issues/1779 , https://github.com/the-momentum/open_wearables_ios_sdk/issues/69
- PromQL functions: https://prometheus.io/docs/prometheus/latest/querying/functions/
- Flux: https://docs.influxdata.com/flux/v0/stdlib/universe/aggregatewindow/ ; https://docs.influxdata.com/flux/v0/future-of-flux/
- Grafana: https://grafana.com/docs/grafana/latest/visualizations/panels-visualizations/query-transform-data/
- TimescaleDB: hyperfunctions https://www.tigerdata.com/docs/api/latest/hyperfunctions ; compression algorithms https://www.tigerdata.com/blog/time-series-compression-algorithms-explained ; TSL directory https://github.com/timescale/timescaledb/tree/main/tsl ; Toolkit https://github.com/timescale/timescaledb-toolkit ; hierarchical caggs https://docs.timescale.com/use-timescale/latest/continuous-aggregates/hierarchical-continuous-aggregates/ ; gapfill DST issues https://github.com/timescale/timescaledb/issues/8998 , https://github.com/timescale/timescaledb/issues/7287 , https://github.com/timescale/timescaledb/issues/6788 , https://github.com/timescale/timescaledb/issues/5334 ; cagg timezone https://github.com/timescale/timescaledb/issues/1248 , https://github.com/timescale/timescaledb/issues/6902 ; compression report https://dev.to/polliog/timescaledb-compression-from-150gb-to-15gb-90-reduction-real-production-data-bnj
- pgvectorscale and HA image: https://github.com/timescale/pgvectorscale ; https://docs.timescale.com/self-hosted/latest/install/installation-docker/
- FHIR `$stats`: https://hl7.org/fhir/R4/observation-operation-stats.html ; SQL on FHIR v2 https://sql-on-fhir.org/ig/2.0.0/ , https://build.fhir.org/ig/FHIR/sql-on-fhir-v2/ , https://www.nature.com/articles/s41746-025-01708-w ; Google FHIR analytics view layer https://developers.google.com/open-health-stack/fhir-analytics/view-layer
- Downsampling: M4 https://dl.acm.org/doi/10.14778/2733004.2733066 ; LTTB thesis https://skemman.is/bitstream/1946/15343/3/SS_MSthesis.pdf ; MinMaxLTTB https://arxiv.org/pdf/2305.00332
- Gorilla: https://www.vldb.org/pvldb/vol8/p1816-teller.pdf
- Postgres: BRIN https://www.crunchydata.com/blog/postgres-indexing-when-does-brin-win ; large JSON https://www.evanjones.ca/postgres-large-json-performance.html ; jsonb overhead https://www.heap.io/blog/when-to-avoid-jsonb-in-a-postgresql-schema ; numeric vs float https://dev.to/tbson87/postgresql-numeric-vs-double-precision-vs-money-which-type-to-use-for-prices-5b9j , https://www.pgedge.com/blog/why-is-numeric-so-popular-in-postgresql-databases ; Postgres 18 https://www.postgresql.org/docs/release/18.0/ ; pg_partman https://github.com/pgpartman/pg_partman ; pg_ivm https://github.com/sraoss/pg_ivm , https://www.postgresql.org/about/news/pg_ivm-113-released-3159 ; pg_duckdb https://github.com/duckdb/pg_duckdb/releases/tag/v1.0.0 ; pg_mooncake https://docs.mooncake.dev/pg/intro/architecture , https://www.storagenewsletter.com/2025/10/22/mooncake-labs-joins-databricks-to-accelerate-the-vision-of-lakebase/ ; pg_analytics https://github.com/paradedb/pg_analytics
- UCUM in Go: https://pkg.go.dev/github.com/gofhir/ucum/v4 ; https://pkg.go.dev/github.com/mattwiller/mu ; https://pkg.go.dev/github.com/iimos/ucum ; https://pkg.go.dev/github.com/bertverhees/ucum ; ucum-lhc molecular weight https://github.com/LHNCBC/ucum-lhc
- Metrics: HRV baselines https://www.researchgate.net/publication/235906316 , https://www.ryun.app/news/lnrmssd-and-the-smallest-worthwhile-change ; PMC https://www.trainingpeaks.com/learn/articles/the-science-of-the-performance-manager/ , http://markliversedge.blogspot.com/2019/01/implementing-banister-impulse-response.html ; ACWR critique https://www.ncbi.nlm.nih.gov/pmc/articles/PMC8138569/ ; Hacker's Diet https://www.fourmilab.ch/hackdiet/e4/signalnoise.html ; SRI https://www.nature.com/articles/s41598-018-32402-5 ; GMI https://diabetesjournals.org/care/article/42/4/e60/36083/ , https://provider.dexcom.com/what-glucose-management-indicator-gmi

Read in the substrate tree: `internal/engine/migrations/0001_init.up.sql`
(records, refs, changelog tables and indexes), `internal/engine/fold.go`
(refs and fts projection in the fold), `internal/substrate/query.go` (the
`Filter` and `Cond` grammar), `internal/changelogfile/writer.go`
(`DefaultSegmentBytes = 256 << 20`), `internal/changelogfile/reader.go`
(`MaxLineBytes = 64 << 20`), `internal/api/core.go` (`maxRequestBody = 1 << 20`),
`compose.yaml` (`pgvector/pgvector:pg16`), and one dev repository's segment
file for line sizes.

Recalled and not re-verified: HealthKit unit conversion on read and source
priority; Health Connect typed units; TSS and NP formulas; TIR thresholds and
CV 36% target; glucose, cholesterol, triglyceride and HbA1c conversion
factors; Garmin Health API shape; `$lastn`; `date_trunc` 3-argument form
since PG12 and `date_bin` since PG14; managed-cloud TimescaleDB availability;
COVID-19 detection studies from wearable respiratory rate.
