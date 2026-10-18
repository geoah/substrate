# 01. Wearable platform APIs (researched 2026-10-08)

Slice: wearable and sensor platforms that a substrate provider would sync from. Part A
is per-platform facts. Part B is the consolidated metric catalogue across platforms,
one table per domain. Part C lists the unit disagreements and vendor-proprietary
scores. Part D is data volume per user per day.

## Provenance legend

- **[web]**: checked on the web on 2026-10-08 against the URL cited next to it.
- **[spec]**: read from a machine-readable spec downloaded on 2026-10-08 (Oura
  OpenAPI 1.41, `https://cloud.ouraring.com/v2/static/json/openapi-1.41.json`).
- **[repo]**: read from this repository (`kinds/providers.substrate.reamde.dev/whoop/bundle.yaml`).
- **[recalled]**: from my training knowledge, not re-verified today. Treat as
  unverified; field names may have drifted.
- **[secondary]**: a third-party aggregator or blog (Terra, Open Wearables, Validic,
  the5krunner), not the vendor.

## Headline changes as of 2026-10 (the landscape moved this year)

1. **Fitbit Web API is turned off on 2026-10-30**; support ended 2026-09-30. Its
   replacement is the **Google Health API** at `https://health.googleapis.com/v4/`,
   Google OAuth, tokens do not carry over, users must re-consent. [web]
   https://developers.google.com/health/migration
2. **Google Fit REST and Android APIs** are supported only "until the end of 2026";
   new sign-ups were closed on 2024-05-01. The server-side successor is the Google
   Health API; the on-device successor is Health Connect. [web]
   https://developer.android.com/health-and-fitness/health-connect/migration/fit ,
   https://sahha.ai/blog/google-fit-api-sunset-migration/
3. **Oura personal access tokens were removed in December 2025**; OAuth2 only. The
   v1 API is sunset. [spec]
4. **WHOOP v1 webhooks were removed**; v2 uses UUID ids for sleep and workout. [web]
   https://developer.whoop.com/docs/developing/webhooks
5. **Apple iOS 27 adds an RMSSD HRV quantity type** beside the old SDNN one, and
   Apple Watch Series 12 samples heart rate every 5 s all day (17,280 samples/day).
   [secondary] https://www.themomentum.ai/blog/apple-watch-series-12-ios-27-rmssd-healthkit .
   The exact identifier (likely `HKQuantityTypeIdentifierHeartRateVariabilityRMSSD`)
   is unverified.
6. **Samsung Health Data SDK** replaced the old Samsung Health Android SDK; partner
   approval does not transfer; March 2026 added irregular heart rhythm notification
   and sleep apnea types. Still on-device only, no cloud API. [web]
   https://developer.samsung.com/health/blog/en/new-health-data-type-support-in-samsung-health-data-sdk
7. **Garmin's Connect Developer Program** application form has reportedly been
   "under construction" through mid-2026; new direct partners are not being onboarded
   (unverified, [secondary] https://github.com/antsal06/wearable-api-field-guide/blob/main/providers/garmin.md).

---

# Part A. Platforms

## A1. Oura (API v2)

Sources: [spec] OpenAPI 1.41; https://cloud.ouraring.com/v2/docs

- **Auth**: OAuth2 authorization code, `https://cloud.ouraring.com/oauth/authorize`,
  token `https://api.ouraring.com/oauth/token`. Scopes: `email`, `personal`,
  `daily`, `heartrate`, `workout`, `tag`, `session`, `spo2`, `heart_health`. Apps are
  capped at 10 users until Oura approves them. A user whose Oura membership lapsed
  gets `403`.
- **Objects** (`/v2/usercollection/<name>`, each with `/{document_id}`):
  `daily_activity`, `daily_cardiovascular_age`, `daily_readiness`,
  `daily_resilience`, `daily_sleep`, `daily_spo2`, `daily_stress`, `enhanced_tag`,
  `heartrate` (time series, no single-document route), `personal_info`,
  `rest_mode_period`, `ring_battery_level` (time series), `ring_configuration`,
  `session`, `sleep`, `sleep_time`, `tag`, `vO2_max` (sic, capital O in the path),
  `workout`. The webhook data type enum also lists `meal`, which has no read route in
  1.41.
- **Query**: daily collections take `start_date`/`end_date` (ISO dates);
  `heartrate` and `ring_battery_level` take `start_datetime`/`end_datetime` and a
  `latest` flag. All lists take `next_token` and an optional `fields` projection.
- **Pagination**: opaque `next_token` in the response.
- **Time**: `day` is an ISO date (Oura's day for activity runs from 04:00 local);
  `timestamp`, `bedtime_start`, `start_datetime` are "LocalizedDateTime" (ISO 8601
  with the local offset); `heartrate` rows carry `timestamp` (UTC) plus
  `timestamp_unix` (milliseconds). `sleep_time` windows carry `day_tz` (offset in
  seconds) and offsets from local midnight in seconds.
- **Packed series inside documents**: `PublicSample` is `{interval (seconds),
  items[] (float or null), timestamp}`. Used by `sleep.heart_rate`, `sleep.hrv`,
  `session.heart_rate`, `session.heart_rate_variability`, `session.motion_count`,
  `daily_activity.met`. Plus character-string encodings:
  `sleep.sleep_phase_5_min` and `sleep.sleep_phase_30_sec` ('1' deep, '2' light,
  '3' REM, '4' awake), `sleep.app_sleep_phase_5_min` (to be removed),
  `sleep.movement_30_sec` ('1' no motion .. '4' active),
  `daily_activity.class_5_min` ('0' non-wear, '1' rest, '2' inactive, '3' low,
  '4' medium, '5' high).
- **Webhooks**: `/v2/webhook/subscription` (create, list, update, delete,
  `renew/{id}`), authenticated with `x-client-id`/`x-client-secret`. A subscription
  is `{callback_url, verification_token, event_type (create|update|delete),
  data_type}` and has an `expiration_time`, so subscriptions must be renewed.
  Notifications arrive about 30 s after the phone syncs. Oura recommends one
  historical pull at connect, then webhooks.
- **Updates**: signalled by an `update` webhook event; the document keeps its `id`.
  `sleep.sleep_analysis_reason` says why a sleep was (re)created
  (`foreground_sleep_analysis`, `bedtime_edit`, `background_sleep_analysis`,
  `background_created_foreground_updated`), and `sleep.type` can become `deleted`.
  There is no `updated_at` field, so polling cannot detect a rewrite without
  re-reading and diffing.
- **Rate limits**: two layers (per token, per application); `429` carries
  `Retry-After`, `X-RateLimit-Limit`, `X-RateLimit-Window`, `X-RateLimit-Reset`,
  `X-RateLimit-Tier`. No number is published in 1.41 (the old documented figure was
  5000 requests per 5 minutes, [recalled]).
- **Backfill**: the full account history is readable by date range (no documented
  horizon). [spec, by absence of a limit]
- **Bulk export**: the Oura web app offers a CSV/JSON data export to the user
  [recalled].

Key fields with units (all [spec]):

| Object | Field | Unit / encoding |
|---|---|---|
| `heartrate` row | `bpm` int; `source` enum `awake, workout, rest, sleep, live, session` | bpm |
| `sleep` | `average_heart_rate`, `lowest_heart_rate` | bpm (from 30 s samples; differs from the app's 5 min aggregate) |
| `sleep` | `average_hrv` int | ms (RMSSD, unit implied, not stated in the spec) |
| `sleep` | `average_breath` | breaths/min |
| `sleep` | `awake_time`, `deep_sleep_duration`, `light_sleep_duration`, `rem_sleep_duration`, `total_sleep_duration`, `time_in_bed`, `latency` | seconds |
| `sleep` | `efficiency` | 1..100 |
| `sleep` | `heart_rate`, `hrv` | `PublicSample`, `interval` 300 s typical |
| `sleep` | `type` | `deleted, sleep, long_sleep, late_nap, rest` |
| `sleep.readiness`, `daily_readiness` | `temperature_deviation`, `temperature_trend_deviation` | degrees Celsius, deviation from personal baseline |
| `daily_readiness` | `score`; `contributors.{activity_balance, body_temperature, hrv_balance, previous_day_activity, previous_night, recovery_index, resting_heart_rate, sleep_balance, sleep_regularity}` | 1..100 |
| `daily_sleep` | `score`; `contributors.{deep_sleep, efficiency, latency, rem_sleep, restfulness, timing, total_sleep}` | 1..100 |
| `daily_activity` | `active_calories`, `total_calories`, `target_calories` | kcal |
| `daily_activity` | `steps` | count |
| `daily_activity` | `equivalent_walking_distance`, `target_meters`, `meters_to_target` | m |
| `daily_activity` | `high_activity_time`, `medium_activity_time`, `low_activity_time`, `sedentary_time`, `resting_time`, `non_wear_time` | seconds |
| `daily_activity` | `*_met_minutes`, `average_met_minutes`; `met` (`PublicSample`, 60 s) | MET-minutes; MET |
| `daily_spo2` | `spo2_percentage.average`; `breathing_disturbance_index` | %; 0..100 |
| `daily_stress` | `stress_high`, `recovery_high`; `day_summary` (`restored, normal, stressful`) | seconds |
| `daily_resilience` | `level` (`limited, adequate, solid, strong, exceptional`); `contributors.{sleep_recovery, daytime_recovery, stress}` | 0..100 |
| `daily_cardiovascular_age` | `vascular_age` (18..100 years); `pulse_wave_velocity` | years; m/s |
| `vO2_max` | `vo2_max` int | ml/kg/min (unit implied) |
| `workout` | `calories`; `distance`; `intensity` (`easy, moderate, hard`); `source` | kcal; m |
| `session` | `type` (`breathing, meditation, nap, relaxation, rest, body_status`); `mood` | samples as `PublicSample` |
| `ring_battery_level` | `level` 0..100, `charging`, `in_charger` | % |

## A2. WHOOP (API v2)

Sources: [web] https://developer.whoop.com/api , /docs/developing/webhooks ,
/docs/developing/rate-limiting ; [repo] the shipped WHOOP bundle.

- **Auth**: OAuth2. Scopes `read:recovery`, `read:cycles`, `read:workout`,
  `read:sleep`, `read:profile`, `read:body_measurement` (plus `offline` for refresh
  tokens [recalled]).
- **Objects**: `GET /v2/cycle`, `/v2/cycle/{id}`, `/v2/cycle/{id}/sleep`,
  `/v2/cycle/{id}/recovery`, `/v2/recovery`, `/v2/activity/sleep[/{id}]`,
  `/v2/activity/workout[/{id}]`, `/v2/user/profile/basic`,
  `/v2/user/measurement/body`, `DELETE /v2/user/access`. Partner routes
  (`/v2/partner/requisition`, `service-request`, results) exist for lab-testing
  partners.
- **Granularity**: per-event only. No HR series, no HRV series, no step series via
  the API. A cycle (wake to wake) is the "day".
- **Fields** (units from docs and repo): cycle `score.strain` (0..21 log scale),
  `score.kilojoule` (kJ), `score.average_heart_rate`, `score.max_heart_rate` (bpm),
  `step_count` (count; listed in the v2 reference fetched today, not in the shipped
  bundle; verify before relying on it). Recovery `score.recovery_score` (0..100),
  `score.resting_heart_rate` (bpm), `score.hrv_rmssd_milli` (ms RMSSD),
  `score.spo2_percentage` (%), `score.skin_temp_celsius` (absolute degrees C, not a
  deviation), `score.user_calibrating` (bool). Sleep
  `score.stage_summary.total_{in_bed,awake,no_data,light_sleep,slow_wave_sleep,rem_sleep}_time_milli`
  (ms), `sleep_cycle_count`, `disturbance_count`,
  `score.sleep_needed.{baseline,need_from_sleep_debt,need_from_recent_strain,need_from_recent_nap}_milli`
  (ms), `score.respiratory_rate` (breaths/min),
  `sleep_performance_percentage`, `sleep_consistency_percentage`,
  `sleep_efficiency_percentage` (0..100), `nap` (bool). Workout `sport_name`,
  `sport_id`, `score.strain`, `score.kilojoule` (kJ), `score.distance_meter`,
  `score.altitude_gain_meter`, `score.altitude_change_meter` (m),
  `score.percent_recorded` (0..100), `score.zone_durations.zone_{zero..five}_milli`
  (ms).
- **score_state**: `SCORED`, `PENDING_SCORE`, `UNSCORABLE` ([web] lists `SCORED`;
  the other two [recalled] and present in the repo bundle).
- **Time**: `start`, `end`, `created_at`, `updated_at` in UTC ISO 8601 plus
  `timezone_offset` (`"-05:00"`). A cycle in progress has no `end`.
- **Pagination**: `limit` max 25, `nextToken`; filter `start`/`end`.
- **Webhooks**: `recovery.updated|deleted`, `sleep.updated|deleted`,
  `workout.updated|deleted` (create is sent as `updated`). Payload `{user_id, id,
  type, trace_id}`; HMAC SHA256 over timestamp+body, `X-WHOOP-Signature` and
  `X-WHOOP-Signature-Timestamp`; 5 retries over about an hour. No cycle webhook.
- **Updates**: `updated_at` moves on re-score; webhook `*.updated`.
- **Rate limits**: 100 requests/minute, 10,000/day per app by default.
- **Backfill**: full history by date range [recalled].
- **Bulk export**: WHOOP app CSV export (physiological_cycles.csv, sleeps.csv,
  workouts.csv, journal_entries.csv) [recalled].

## A3. Google Health API (v4; successor to Fitbit Web API and Google Fit REST)

Sources: [web] https://developers.google.com/health/data-types ,
https://developers.google.com/health/reference/rest ,
https://developers.google.com/health/reference/rest/v4/users.dataTypes.dataPoints ,
https://developers.google.com/health/rate-limits ,
https://developers.google.com/health/migration/api-specifications ;
[secondary] https://tryterra.co/blog/everything-you-need-to-know-about-google-health-new-api

- **Auth**: Google OAuth2. Scopes are bundles under
  `https://www.googleapis.com/auth/googlehealth.*`: `activity_and_fitness`,
  `health_metrics_and_measurements`, `sleep`, `nutrition`, `ecg`, `irn`,
  `reproductive_health`, `mindfulness`, `logged_symptoms`, each with `.readonly`
  and/or `.writeonly`. All are "restricted" scopes, so a production app needs
  Google's privacy/security review.
- **One resource shape**: `users/{user}/dataTypes/{data-type}/dataPoints/{id}`.
  Methods: `list` (raw points with source), `get`, `reconcile` (merged across
  sources), `rollUp` (physical time windows), `dailyRollUp` (civil days), `create`,
  `patch`, `batchDelete`, `exportExerciseTcx`. A `DataPoint` is `{name, dataSource,
  <one union member>}`; `dataSource` names platform, device, application,
  recording method.
- **Time**: sample types carry `sampleTime {physicalTime (UTC RFC 3339),
  utcOffset ("0s" duration)}`; interval and session types carry start and end
  times with `startUtcOffset`/`endUtcOffset`. Local civil time is recoverable for
  every point.
- **Data types** (dataType id; record type; resolution). Interval types at
  1-minute resolution: `steps`, `distance`, `floors`, `altitude`,
  `active-energy-burned`, `active-minutes`, `active-zone-minutes`,
  `time-in-heart-rate-zone`, `total-calories` (rollup only),
  `calories-in-heart-rate-zone` (rollup only), plus `activity-level`,
  `sedentary-period`, `swim-lengths-data`. Sample types: `heart-rate` (1 s
  resolution per the data types page; Terra reports 5 s default),
  `heart-rate-variability`, `oxygen-saturation` (1 min, validated only),
  `weight`, `height`, `body-fat`, `blood-glucose`, `core-body-temperature`,
  `skin-temperature-sensors` (v4beta), `vo2-max`, `run-vo2-max`,
  `respiratory-rate-sleep-summary`. Daily types: `daily-heart-rate-variability`,
  `daily-heart-rate-zones`, `daily-oxygen-saturation`, `daily-respiratory-rate`,
  `daily-resting-heart-rate`, `daily-sleep-temperature-derivations`,
  `daily-vo2-max`. Sessions: `sleep`, `exercise`, `electrocardiogram`,
  `irregular-rhythm-notification`, `hydration-log`, `nutrition-log`. Food
  entities: `food`, `food-measurement-unit`. Write-only today: `menstrual-period`,
  `ovulation-test`, `moods`, `symptoms`. Union member also exists for
  `basalEnergyBurned`.
- **Units are in the field names**: `weightGrams`, `distance.millimeters`,
  `altitude.gainMillimeters`, `heartRate.beatsPerMinute` (int64 as string, 1..300),
  `oxygenSaturation.percentage`, `bodyFat.percentage`,
  `heartRateVariability.rootMeanSquareOfSuccessiveDifferencesMilliseconds`,
  `bloodGlucose.bloodGlucoseMilligramsPerDeciliter` (0..900), VO2 max
  `mlPerKgPerMinute`, energy in kcal, core body temperature in Celsius. Daily HRV
  carries `averageHeartRateVariabilityMilliseconds`, `nonRemHeartRateBeatsPerMinute`,
  `entropy`, `deepSleepRootMeanSquareOfSuccessiveDifferencesMilliseconds`; daily
  resting HR has `beatsPerMinute` and `calculationMethod` (`WITH_SLEEP`,
  `ONLY_WITH_AWAKE_DATA`); daily SpO2 has `averagePercentage`,
  `lowerBoundPercentage`, `upperBoundPercentage`, `standardDeviationPercentage`.
  (Field names converted from the RPC snake_case by the fetch tool; spot-check
  before coding.)
- **Sleep stages**: `AWAKE`, `LIGHT`, `DEEP`, `REM`, plus `ASLEEP` and `RESTLESS`
  for classic (non-staged) sleep. [web, partly secondary]
- **Query limits**: max query range 14 days for `heart-rate`,
  `calories-in-heart-rate-zone`, `active-minutes`, `total-calories`; 90 days for
  other rollups.
- **Notifications**: `projects.subscribers` and `.subscriptions` resources (create,
  list, patch, delete); Google markets "auto-subscribing webhooks". Payload shape
  not verified.
- **Rate limits**: 300 requests/minute per user (2.5 QPS for unverified apps);
  120,000/minute and 86.4 M/day per project.
- **Backfill**: users who re-consent can read data kept in the Google Health app;
  the horizon is not published (unverified).
- **Bulk export**: Google Takeout (Fitbit data as JSON/CSV) [recalled]; TCX per
  exercise via `exportExerciseTcx`.

## A4. Fitbit Web API (legacy; off on 2026-10-30)

Sources: [web] https://dev.fitbit.com/build/reference/web-api/ (temperature and HRV
intraday pages fetched); rest [recalled]. Documented here because existing exports
and historical mirrors still use its shapes.

- **Auth**: Fitbit OAuth2 with PKCE; scopes `activity`, `heartrate`, `sleep`,
  `weight`, `nutrition`, `oxygen_saturation`, `respiratory_rate`, `temperature`,
  `cardio_fitness`, `electrocardiogram`, `irregular_rhythm_notifications`,
  `location`, `profile`, `settings`, `social`.
- **Units follow the `Accept-Language` header** (`en_US` imperial, `en_GB` stone,
  default metric). The same endpoint returns miles or km, lb or kg, F or C. This is
  the worst unit trap in the catalogue.
- **Intraday**: heart rate `1sec`/`1min`/`5min`/`15min` detail levels; steps,
  calories, distance, floors, elevation at `1min`/`5min`/`15min`; HRV intraday
  `minutes[].value.{rmssd (ms), coverage, hf, lf}` every 5 minutes during sleep;
  SpO2 intraday per minute during sleep; breathing rate per sleep stage. Intraday
  for third-party apps needed approval; personal apps had it.
- **Sleep**: `levels.data[]` and `levels.shortData[]` with `level` in
  `deep, light, rem, wake` (stages) or `asleep, restless, awake` (classic), 30 s
  granularity.
- **Temperature**: `temp/skin` returns `nightlyRelative` (deviation from baseline,
  C or F by header) and `logType` (`dedicated_temp_sensor`, `other_sensors`);
  `temp/core` is user-logged absolute.
- **Cardio fitness**: `vo2Max` as a single value or a range string such as
  `"44-48"`.
- **ECG**: readings with `waveformSamples`, `samplingFrequencyHz` (250 [recalled]),
  `scalingFactor`, `numberOfWaveformSamples`, `resultClassification`, `leadNumber`.
- **Webhooks**: Subscriptions API per collection (`activities`, `body`, `foods`,
  `sleep`, `userRevokedAccess`), notification lists `{collectionType, date,
  ownerId, subscriptionId}`; the client then re-fetches the date.
- **Rate limit**: 150 requests/hour per user.
- **Time**: local time without offset in most responses; the user's profile
  carries `timezone` and `offsetFromUTCMillis`. Ambiguous across travel.

## A5. Health Connect (Android, on-device)

Sources: [web] https://developer.android.com/health-and-fitness/health-connect/data-types ,
https://developer.android.com/reference/kotlin/androidx/health/connect/client/records/SkinTemperatureRecord

- **No cloud API.** An Android app with runtime permissions reads the on-device
  store; a server only sees what that app uploads. Same for Samsung and Apple.
- **Record types**: Activity: `ActiveCaloriesBurnedRecord`,
  `ActivityIntensityRecord`*, `CyclingPedalingCadenceRecord`, `DistanceRecord`,
  `ElevationGainedRecord`, `ExerciseSessionRecord` (with `ExerciseRoute` GPS),
  `FloorsClimbedRecord`, `PlannedExerciseSessionRecord`*, `PowerRecord`,
  `SpeedRecord`, `StepsRecord`, `StepsCadenceRecord`, `TotalCaloriesBurnedRecord`,
  `Vo2MaxRecord`, `WheelchairPushesRecord`. Body: `BasalMetabolicRateRecord`,
  `BodyFatRecord`, `BodyWaterMassRecord`, `BoneMassRecord`, `HeightRecord`,
  `LeanBodyMassRecord`, `WeightRecord`. Cycle: `BasalBodyTemperatureRecord`,
  `CervicalMucusRecord`, `IntermenstrualBleedingRecord`,
  `MenstruationFlowRecord`, `MenstruationPeriodRecord`, `OvulationTestRecord`,
  `SexualActivityRecord`. Nutrition: `HydrationRecord`, `NutritionRecord`. Sleep:
  `SleepSessionRecord`. Vitals: `BloodGlucoseRecord`, `BloodPressureRecord`,
  `BodyTemperatureRecord`, `HeartRateRecord`, `HeartRateVariabilityRmssdRecord`,
  `OxygenSaturationRecord`, `RespiratoryRateRecord`, `RestingHeartRateRecord`,
  `SkinTemperatureRecord`*. Wellness: `MindfulnessSessionRecord`*. (* behind a
  feature flag.) Medical records in FHIR format are a separate API.
- **Shapes** [recalled]: three record shapes: instantaneous (`time`,
  `zoneOffset`), interval (`startTime`, `startZoneOffset`, `endTime`,
  `endZoneOffset`), and series (interval plus `samples[]`, e.g.
  `HeartRateRecord.samples[{time, beatsPerMinute}]`). Values are typed unit objects
  (`Energy`, `Mass`, `Length`, `Temperature`, `Pressure`, `BloodGlucose`, `Volume`,
  `Power`, `Velocity`) readable in any unit, so there is no wire unit.
  `SkinTemperatureRecord` is `baseline` plus `deltas[{time, delta}]`, a deviation
  series. Sleep stage constants: `STAGE_TYPE_UNKNOWN 0, AWAKE 1, SLEEPING 2,
  OUT_OF_BED 3, LIGHT 4, DEEP 5, REM 6, AWAKE_IN_BED 7`. Every record has
  `metadata {id, dataOrigin (package), lastModifiedTime, clientRecordId,
  clientRecordVersion, device, recordingMethod}`.
- **Changes**: `getChangesToken` then `getChanges` returns upserts and deletions;
  tokens expire after 30 days [recalled].
- **History**: reads limited to 30 days before the permission grant unless the app
  holds `READ_HEALTH_DATA_HISTORY`; background reads need
  `READ_HEALTH_DATA_IN_BACKGROUND`. Rate limited on device.

## A6. Samsung Health Data SDK (on-device)

Sources: [web] https://developer.samsung.com/health/data/overview.html , the March
2026 blog cited above.

- On-device SDK on Android 10+, Samsung Health 6.30.2+; partner approval to
  distribute (developer mode for testing). No cloud API.
- Readable types (23): activity summary, exercise, heart rate, sleep, steps, blood
  pressure, blood oxygen, blood glucose, body composition, body temperature, skin
  temperature, nutrition, water intake, floors climbed, energy score, sleep apnea,
  irregular heart rhythm notification, goals (active calories, active time, sleep,
  steps, water intake, nutrition), user profile. Writable: 12.
- Has aggregate operations and change reading. Exact field names not verified;
  Samsung also writes most of this into Health Connect.
- `ENERGY_SCORE` is a Samsung proprietary readiness-style score.

## A7. Apple HealthKit (on-device; reaches a server only via an app or an export)

Sources: [web] https://developer.apple.com/documentation/healthkit/hkcategoryvaluesleepanalysis/asleeprem ;
[secondary] Health Auto Export docs https://help.healthyapps.dev/en/health-auto-export/export-format/ ;
other details [recalled].

- No cloud API. Three routes to a server: (1) a companion iOS app using
  HealthKit (`HKAnchoredObjectQuery` gives inserts plus deleted object UUIDs since an
  anchor, the change signal; background delivery via `HKObserverQuery`); (2) the
  user's manual **Apple Health export**, a zip with `export.xml`
  (`<Record type="HKQuantityTypeIdentifierHeartRate" sourceName=.. unit="count/min"
  creationDate=.. startDate="2026-10-01 08:00:00 +0200" endDate=.. value="61"/>`,
  `<Workout>` with `<WorkoutStatistics>`, `<ActivitySummary>`), `export_cda.xml`,
  `workout-routes/*.gpx`, `electrocardiograms/*.csv`; (3) third-party exporters such
  as **Health Auto Export**, which POSTs JSON
  `{data:{metrics[], workouts[], stateOfMind[], medications[], symptoms[],
  cycleTracking[], ecg[], heartRateNotifications[]}}`; a metric is `{name, units,
  data[{date, qty, source}]}`, heart rate rows carry `Min`/`Avg`/`Max`, dates are
  `yyyy-MM-dd HH:mm:ss Z`, and the user picks the time grouping (seconds, minutes,
  hours, days) and units.
- **Identifiers** (HK prefix dropped): quantity types `heartRate` (count/min),
  `restingHeartRate`, `walkingHeartRateAverage`, `heartRateRecoveryOneMinute`,
  `heartRateVariabilitySDNN` (ms, SDNN not RMSSD), RMSSD from iOS 27,
  `oxygenSaturation` (fraction 0..1, shown as %), `respiratoryRate`,
  `bodyTemperature`, `basalBodyTemperature`,
  `appleSleepingWristTemperature` (degC, absolute nightly; the app shows the
  deviation), `stepCount`, `distanceWalkingRunning`, `distanceCycling`,
  `distanceSwimming`, `flightsClimbed`, `activeEnergyBurned`, `basalEnergyBurned`
  (kcal default; user may pick kJ), `appleExerciseTime`, `appleStandTime`,
  `appleMoveTime`, `vo2Max` (mL/(kg*min)), `bodyMass`, `bodyMassIndex`,
  `bodyFatPercentage`, `leanBodyMass`, `height`, `waistCircumference`,
  `bloodGlucose` (mg/dL or mmol/L), `bloodPressureSystolic`/`Diastolic` (mmHg,
  correlated as `bloodPressure`), `dietaryWater`, `dietaryEnergyConsumed` and 30+
  `dietary*`, `walkingSpeed`, `walkingStepLength`, `walkingAsymmetryPercentage`,
  `walkingDoubleSupportPercentage`, `stairAscentSpeed`, `sixMinuteWalkTestDistance`,
  `runningPower`, `runningSpeed`, `runningStrideLength`,
  `runningVerticalOscillation`, `runningGroundContactTime`, `cyclingPower`,
  `cyclingCadence`, `physicalEffort`, `timeInDaylight`,
  `environmentalAudioExposure`, `headphoneAudioExposure`, `appleSleepingBreathingDisturbances`.
  Category types: `sleepAnalysis` (`inBed`, `asleepUnspecified`, `awake`,
  `asleepCore`, `asleepDeep`, `asleepREM`), `menstrualFlow`, `ovulationTestResult`,
  `cervicalMucusQuality`, `intermenstrualBleeding`, `sexualActivity`,
  `irregularHeartRhythmEvent`, `highHeartRateEvent`, `lowHeartRateEvent`,
  `sleepApneaEvent`, `mindfulSession`. Plus `HKElectrocardiogram` (512 Hz voltage
  samples [recalled]), `HKWorkout` with `HKWorkoutRoute` (GPS, about 1 Hz), and
  `HKHeartbeatSeriesSample` (beat-to-beat timestamps, from Breathe/HRV readings).
- Apple Watch HR cadence: historically every few minutes at rest and every 1..5 s in
  workouts; Series 12 and Ultra 4 every 5 s all day [secondary].
- Note Apple "Core" sleep equals other vendors' "light".

## A8. Garmin (Connect Developer Program: Health API, Activity API, Women's Health API, Training API)

Sources: [web] https://developer.garmin.com/gc-developer-program/health-api/ ;
field names [secondary] from ilumivu field docs
(https://ilumivu.freshdesk.com/support/solutions/articles/9000258793-garmin-health-api-data-daily-summary ,
https://ilumivu.freshdesk.com/support/solutions/articles/9000259229-garmin-health-api-data-stress-body-battery-respiration-rate ),
https://openwearables.io/blog/garmin-connect-api-developer-guide-activities-health-metrics ;
the rest [recalled] from the Health REST API specification 2.9.x.

- **Auth/access**: partner-gated (approval, evaluation keys first). OAuth 2.0 with
  PKCE replaced OAuth 1.0a [secondary]. Backfill needs the user to grant
  `HISTORICAL_DATA_EXPORT`.
- **Delivery**: push or ping/pull, one subscription per summary type. Push POSTs
  the summaries in the body; ping POSTs `callbackURL`s to fetch within a window. No
  general polling; notifications are retried and held up to 7 days.
- **Backfill**: per summary type, about 30 days (older accounts and some types up to
  90 days or more [recalled conflicting]), 24-hour max window per backfill request.
  Data retained at Garmin for the partner about 7 days after first availability.
- **Rate limits**: evaluation keys limited; production "not rate-limited";
  aggregators assume 100 requests/minute.
- **Time**: `startTimeInSeconds` (Unix seconds UTC) plus
  `startTimeOffsetInSeconds` (local offset); `calendarDate` (local date). Series
  inside a summary are maps `timeOffset<Thing>` from seconds-after-start to value.
- **Updates**: a summary for the same day is re-sent as the day fills in; consumers
  dedupe on `summaryId` and keep the latest (for sleep, `validation` moves from
  `ENHANCED_TENTATIVE` to `ENHANCED_FINAL` [recalled]). Deregistrations and
  user permission changes are their own notification types.
- **Summary types**:
  - `dailies`: `calendarDate`, `steps`, `distanceInMeters`, `activeKilocalories`,
    `bmrKilocalories`, `durationInSeconds`, `activeTimeInSeconds`,
    `moderateIntensityDurationInSeconds` (MET 3..6),
    `vigorousIntensityDurationInSeconds` (MET > 6), `floorsClimbed`,
    `min/max/average/restingHeartRateInBeatsPerMinute`,
    `timeOffsetHeartRateSamples` (15 s cadence [recalled]), `averageStressLevel`,
    `maxStressLevel` (1..100, -1 insufficient), `stressDurationInSeconds`,
    `rest/activity/low/medium/highStressDurationInSeconds`, `stressQualifier`
    (`unknown, calm, balanced, stressful, very_stressful`),
    `bodyBatteryChargedValue`, `bodyBatteryDrainedValue`, goals.
  - `epochs`: 15-minute (900 s) buckets with `activityType`, `steps`,
    `distanceInMeters`, `activeKilocalories`, `met`, `intensity`
    (`SEDENTARY, ACTIVE, HIGHLY_ACTIVE`), `meanMotionIntensity`,
    `maxMotionIntensity` [recalled].
  - `sleeps`: `durationInSeconds`, `deepSleepDurationInSeconds`,
    `lightSleepDurationInSeconds`, `remSleepInSeconds`,
    `awakeDurationInSeconds`, `unmeasurableSleepInSeconds`, `sleepLevelsMap`
    with keys `deep, light, rem, awake` (lists of start/end spans),
    `timeOffsetSleepSpo2`, `timeOffsetSleepRespiration`, `overallSleepScore
    {value, qualifierKey}`, `sleepScores` sub-scores [recalled].
  - `stressDetails`: `timeOffsetStressLevelValues` (3-minute cadence; -1 off wrist,
    -2 large motion, -3 not enough data), `timeOffsetBodyBatteryValues` (3 min).
  - `hrv`: `lastNightAvg`, `lastNight5MinHigh` (ms RMSSD), `hrvValues` map at 5 min
    [recalled].
  - `respiration`: `timeOffsetEpochToBreaths` (breaths/min, 1-minute cadence).
  - `pulseox`: `timeOffsetSpo2Values` (%; per minute in sleep, spot checks
    otherwise), `onDemand` flag [recalled].
  - `bodyComps`: `weightInGrams`, `bodyFatInPercent`, `bodyMassIndex`,
    `muscleMassInGrams`, `boneMassInGrams`, `bodyWaterInPercent`,
    `measurementTimeInSeconds` [recalled; grams is the notable unit].
  - `bloodPressures`: `systolic`, `diastolic` (mmHg), `pulse` (bpm), `sourceType`
    [recalled].
  - `userMetrics`: `vo2Max`, `vo2MaxCycling` (ml/kg/min), `fitnessAge` (years)
    [recalled].
  - `skinTemp`: `avgDeviationCelsius` (deviation) [recalled].
  - `healthSnapshot`, `moveIQActivities`, `mct` (menstrual cycle tracking, via the
    Women's Health API: `periodStartDate`, `dayInCycle`, `cycleLength`,
    `predictedCycleLength`, `currentPhase`, pregnancy fields [recalled]).
- **Activity API**: `activities` (summary: `activityType`, `durationInSeconds`,
  `distanceInMeters`, `activeKilocalories`, `averageHeartRateInBeatsPerMinute`,
  `averageSpeedInMetersPerSecond`, `averagePaceInMinutesPerKilometer`,
  `totalElevationGainInMeters`, ...), `activityDetails` with `samples[]`
  (`startTimeInSeconds`, `latitudeInDegree`, `longitudeInDegree`,
  `elevationInMeters`, `airTemperatureCelcius` (sic), `heartRate`,
  `speedMetersPerSecond`, `stepsPerMinute`, `totalDistanceInMeters`,
  `powerInWatts`, `bikeCadenceInRPM`, `swimCadenceInStrokesPerMinute`,
  `timerDurationInSeconds`, `clockDurationInSeconds`, `movingDurationInSeconds`),
  usually 1 s recording, and the original FIT file (also GPX, TCX).
- **Beat-to-beat** intervals exist (Health SDK / enhanced Health API) under a
  separate commercial licence.

## A9. Polar AccessLink (API v3)

Sources: [web] https://www.polar.com/accesslink-api/ (fetched twice); details
[recalled] where marked.

- **Auth**: OAuth2; the user must be registered to the client (`POST /v3/users`).
- **Resources**: exercises (by hashed id; FIT, TCX, GPX; samples, zones, route;
  only exercises uploaded in the last 30 days), daily activity (summaries and
  samples; date ranges max 28 days, 365 days of history), continuous heart rate (5
  minute samples `{heart_rate, sample_time "HH:MM:SS"}` per date), sleep (Sleep
  Plus Stages: `sleep_score`, `light_sleep`, `deep_sleep`, `rem_sleep` in
  seconds, `continuity` 1..5, `hypnogram` map from "HH:MM" to stage code
  0 wake, 1 REM, 2 lighter non-REM, 3 light non-REM, 4 deep non-REM, 5 unknown
  [recalled], `heart_rate_samples`), Nightly Recharge (`ans_charge` (-10..+10
  [recalled]), `hrv_avg` (ms), `breathing_rate_avg`, `beat_to_beat_avg` (ms),
  `hrv_samples` and `breathing_samples` at 5 minutes [recalled],
  `nightly_recharge_status` 1..6), SleepWise (alertness, circadian bedtime), cardio
  load (`cardio_load`, `strain`, `tolerance`, `cardio_load_ratio`, levels),
  biosensing (body temperature, sleep skin temperature in degrees C, skin contact,
  wrist ECG test, SpO2 test), physical info (weight kg, height cm, VO2 max,
  max/resting HR, aerobic/anaerobic thresholds).
- **Exercise samples**: per `sample-type` code, a recording rate in seconds and a
  comma-separated value string (HR, speed, cadence, altitude, power, distance,
  temperature, RR intervals [recalled]).
- **Change model**: older v3 "transactions" (create, list, commit) for exercises,
  activity and physical info are deprecated in favour of direct hashed-id
  endpoints; webhooks for `EXERCISE`, `SLEEP`, `CONTINUOUS_HEART_RATE`,
  `ACTIVITY_SUMMARY`, `PHYSICAL_INFORMATION`, SleepWise events; HMAC SHA256 signed.
- **Rate limits**: per 15 min `500 + users x 20`; per 24 h `5000 + users x 100`.
- **Time**: ISO 8601; `start_time_utc_offset` in minutes; durations as ISO 8601
  periods (`PT3H11M`).

## A10. Withings (Public Health Data API)

Sources: [web] https://developer.withings.com/developer-guide/v3/data-api/all-available-health-data/ ;
API mechanics [recalled] from https://developer.withings.com/api-reference/ (page is
JavaScript-rendered and could not be fetched).

- **Auth**: OAuth2; scopes `user.info`, `user.metrics`, `user.activity`,
  `user.sleepevents`. Access tokens last 3 hours. Data is also tiered by Withings
  "packs" (Basic, Total, Wellness, Regulated) for partners.
- **Measure** `action=getmeas`: `measuregrps[{grpid, date (unix s), created,
  modified, category (1 real, 2 objective), deviceid, measures[{value, type,
  unit}]}]`. The real value is `value * 10^unit` (an integer mantissa and a decimal
  exponent: exact by construction). `type` codes [recalled]: 1 weight kg, 4 height
  m, 5 fat-free mass kg, 6 fat ratio %, 8 fat mass kg, 9 diastolic mmHg, 10 systolic
  mmHg, 11 heart pulse bpm, 12 temperature C, 54 SpO2 %, 71 body temperature C, 73
  skin temperature C, 76 muscle mass kg, 77 hydration kg, 88 bone mass kg, 91 pulse
  wave velocity m/s, 123 VO2 max ml/min/kg, 130 AFib result, 135 QRS ms, 136 PR ms,
  137 QT ms, 138 QTc ms, 139 AFib from PPG, 155 vascular age, 167 nerve health
  score, 168 extracellular water kg, 169 intracellular water kg, 170 visceral fat,
  173..175 segmental fat-free mass, 226 basal metabolic rate kcal, 227 metabolic
  age, 229 electrodermal activity score. 155, 170 and 226 confirmed [web].
- **Measure v2** `getactivity` (daily: `steps`, `distance` m, `elevation` m,
  `soft`, `moderate`, `intense` s, `active`, `calories`, `totalcalories` kcal,
  `hr_average`, `hr_min`, `hr_max`, `hr_zone_0..3` s, `timezone`), `getintradayactivity`
  (per-sample: `steps`, `elevation`, `calories`, `distance`, `stroke`, `pool_lap`,
  `duration`, `heart_rate`, `spo2_auto`, `rmssd`, `sdnn1` at about 1 min when worn
  [recalled]), `getworkouts`.
- **Sleep v2** `get` (per-segment `state` 0 awake, 1 light, 2 deep, 3 REM, plus
  per-minute `hr`, `rr`, `snoring`, `sdnn_1`, `rmssd`, `mvt_score`) and
  `getsummary` (`wakeupduration`, `lightsleepduration`, `deepsleepduration`,
  `remsleepduration`, `total_sleep_time`, `total_timeinbed` s, `sleep_score`,
  `sleep_efficiency`, `sleep_latency`, `waso`, `hr_average`, `rr_average`,
  `apnea_hypopnea_index`, `breathing_disturbances_intensity`, `snoring`) [recalled].
- **Heart v2**: `list` and `get` for ECG (`signal[]` microvolts, `sampling_frequency`
  300 or 500 Hz, `wearposition`, AFib result) [recalled].
- **Notifications**: `notify subscribe` with `appli` codes: 1 weight/body, 4
  BP/HR/SpO2/temperature, 16 activity, 44 sleep, 46 user actions, 50 in bed, 51 out
  of bed, 54 ECG, 58 glucose [recalled]. The callback says "something changed
  between startdate and enddate"; the client re-reads with `lastupdate`, which also
  returns rewritten and deleted groups (the official change signal).
- **Rate limit**: 120 requests/minute per partner [recalled].
- **Time**: Unix seconds UTC plus a `timezone` IANA name on many objects.

## A11. Dexcom (API v3)

Sources: [web] https://developer.dexcom.com/docs/dexcomv3/endpoint-overview/

- **Auth**: OAuth2, regional bases (`api.dexcom.com`, `api.dexcom.eu`,
  `api.dexcom.jp`, sandbox).
- **Endpoints**: `/v3/users/self/egvs`, `events`, `calibrations`, `alerts`,
  `devices`, `dataRange`.
- **EGV fields** [recalled beyond the units sentence]: `recordId`, `systemTime`
  (UTC from the device clock), `displayTime` (local as shown to the user, can be
  user-shifted), `transmitterId`, `transmitterTicks`, `value`, `status`, `trend`
  (`doubleUp, singleUp, fortyFiveUp, flat, fortyFiveDown, singleDown, doubleDown,
  none, notComputable, rateOutOfRange`), `trendRate`, `unit`, `rateUnit`,
  `displayDevice`, `transmitterGeneration`. Values below 40 or above 400 come as
  status flags rather than numbers [recalled].
- **Units**: "All glucose values are reported in units of mg/dL and trend rates in
  units of mg/dL/min." [web]
- **Cadence**: one EGV every 5 minutes (288/day).
- **Delay**: 1 hour in the US, 3 hours elsewhere. Not real-time.
- **Window**: max 30 days per request; no webhooks; polling only.
- **Rate limit**: 60,000 calls per app per hour.

## A12. Abbott FreeStyle Libre (LibreView)

Sources: [web, secondary] https://www.diabetescare.abbott/partnerships/integrations/en.html ,
https://docs.junction.com/wearables/guides/abbott-libreview ,
https://ccnhealth.com/articles/announcements/ccn-health-freestyle-libre-integration

- No public self-serve developer API. Partner onboarding with Abbott; patient
  consent flows through LibreView practice or LibreLinkUp sharing. Unofficial
  LibreLinkUp clients exist (https://github.com/FokkeZB/libreview-unofficial).
- Cadence: Libre 3 streams a reading every minute (1,440/day); Libre 2 stores 15
  minute history (96/day) plus 1 minute real-time to the phone [recalled]. Units
  mg/dL or mmol/L by region.
- LibreView CSV export columns [recalled]: `Device Timestamp` (local, no offset),
  `Record Type` (0 historic, 1 scan, ...), `Historic Glucose mg/dL`,
  `Scan Glucose mg/dL`, notes, carbs, insulin.

## A13. Ultrahuman (Partner API, "UltraSignal")

Sources: [web] https://vision.ultrahuman.com/developer-docs

- **Auth**: personal API token (own data) or OAuth2 with scopes `profile`,
  `ring_data`, `cgm_data` [secondary]; a third party is authorized by the user
  entering a Partner ID in the app.
- **Endpoint**: `GET https://partner.ultrahuman.com/api/v1/partner/daily_metrics`
  with `date` (a whole day in the user's timezone) or `start_epoch`/`end_epoch`
  (max 7 days), optional `email`.
- **Metrics** (objects in a `metric_data` list): `hr` (bpm series), `temp` (skin C
  series), `hrv` (series, RMSSD ms [recalled]), `spo2` (%), `steps`, `motion`,
  `night_rhr`/`sleep_rhr`, `sleep` (score, `total_sleep`, `time_in_bed`,
  `sleep_efficiency`, `rem_sleep`, `deep_sleep`, `light_sleep`,
  `full_sleep_cycles`, `tosses_and_turns`, `temperature_deviation`, `hr_drop`,
  `restorative_sleep`, `morning_alertness`), `recovery`, `recovery_index`,
  `movement_index`, `active_minutes`, `vo2_max`, `avg_sleep_hrv`,
  `average_body_temperature`; CGM (M1): `glucose` (mg/dL), `average_glucose`,
  `glucose_variability` (%), `hba1c` (estimate), `time_in_target` (%),
  `metabolic_score`. Series cadence not documented; about 5 minutes [recalled].
- No webhooks documented; polling per day.

## A14. Eight Sleep

Sources: [secondary] https://github.com/api-evangelist/eight-sleep ,
https://github.com/mezz64/pyEight

- No official public API. Community clients use `auth-api.8slp.net`,
  `client-api.8slp.net` (`/v1/users/{id}/intervals`, `/trends`). Per-night interval
  with `stages[{stage: awake|light|deep|rem|out, duration s}]`, `score`, and
  `timeseries` arrays of `[timestamp, value]` for `heartRate`, `hrv`,
  `respiratoryRate`, `tempBedC`, `tempRoomC`, `tnt` (toss and turn) at about 1..5
  minutes [recalled]. Treat as unstable; a provider would be best effort.

## A15. Suunto (Suunto Cloud API via API Zone)

Sources: [web] https://apizone.suunto.com/webhooks ; [secondary]
https://openwearables.io/blog/how-to-integrate-suunto-data-into-your-health-app

- Partner programme (Azure API Management portal, subscription key plus OAuth2).
- Workouts as summary JSON and FIT files (all samples, laps). "247" data: daily
  activity samples (HR, steps, energy), sleep with stages, recovery; webhooks for new
  workouts, 247 activity and 247 sleep. Exact field names not verified.

## A16. COROS

Sources: [secondary] https://the5krunner.com/2026/05/13/coros-mcp-ai-data/ ,
https://tryterra.co/integrations/coros

- Partner-only API (no self-serve keys). Workouts as FIT; daily summaries (steps,
  calories, average and resting HR, stress), sleep (score, deep/light/REM, awake),
  HRV assessment (daily average and normal range), recovery %, training load,
  VO2max and race predictions. The COROS MCP (2026) withholds per-second HR and
  pace streams, GPS, laps and power; HR is a workout average and a daily figure.

## A17. Bulk export formats seen across platforms

| Format | Who | Notes |
|---|---|---|
| FIT (binary, Garmin SDK) | Garmin, COROS, Suunto, Polar, Wahoo, Google Health (exercise) | per-second records, laps, sessions, device info, HRV (RR) messages; the de facto workout format |
| TCX (XML) | Garmin, Polar, Google Health `exportExerciseTcx`, Fitbit | trackpoints with time, position, HR, cadence |
| GPX (XML) | Garmin, Polar, Apple export `workout-routes/` | position and time; HR only via extensions |
| Apple `export.xml` + `export_cda.xml` | Apple Health | one XML element per sample; local time with offset; multi-GB for long histories |
| Health Auto Export JSON/CSV | iOS exporter | user-chosen grouping and units |
| CSV app exports | WHOOP, Oura, Withings, LibreView, Eight Sleep (unverified) | vendor columns, local time often without offset |
| Google Takeout | Fitbit, Google Fit | per-type JSON/CSV (Fitbit) and per-day JSON (Fit) [recalled] |

---

# Part B. Consolidated metric catalogue

Column legend: **Shape** is `instant` (a sample at a time), `interval` (an aggregate
over a stated window), `session` (an event with start and end, holding values), or
`daily` (one per day, by the vendor's day definition). **Cadence** is the typical
native resolution. Platforms are abbreviated: OU Oura, WH WHOOP, GH Google Health
API, FB Fitbit legacy, HC Health Connect, SA Samsung, AP Apple HealthKit, GA Garmin,
PO Polar, WI Withings, DX Dexcom, LI Libre, UH Ultrahuman, ES Eight Sleep, CO
COROS, SU Suunto. Field names are as each platform spells them; tags as in the
legend above apply per platform section.

## B1. Heart rate

| Canonical metric | Platform: field | Unit | Shape | Cadence | Range |
|---|---|---|---|---|---|
| heart_rate | OU `heartrate.bpm` (+`source`) | bpm int | instant | 5 min at rest, about 1 s in workouts/live | 30..220 |
| | GH `heart-rate` `beatsPerMinute` | bpm int64 | instant | 1 s (5 s stated by Terra) | 1..300 |
| | FB `activities-heart-intraday.dataset[].value` | bpm | instant | 1 s / 1 min | |
| | HC `HeartRateRecord.samples[].beatsPerMinute` | bpm | series in interval | device-defined | 1..300 |
| | AP `heartRate` | count/min (float) | instant | 5 s (Series 12), else minutes at rest, 1..5 s in workout | |
| | GA `timeOffsetHeartRateSamples` (dailies); `samples[].heartRate` (activity) | bpm | series map; instant | 15 s daily; 1 s activity | |
| | PO continuous HR `heart_rate`; exercise samples | bpm | instant | 5 min; 1 s in exercise | |
| | WI intraday `heart_rate`; meas type 11 | bpm | instant | about 1..10 min; spot | |
| | UH `hr` | bpm | series | about 5 min | |
| | ES `timeseries.heartRate` | bpm | series | per night, 1..5 min | |
| | SA heart rate | bpm | series | 10 min background or continuous, 1 s workout (unverified) | |
| resting_heart_rate | OU `sleep.lowest_heart_rate` (no explicit RHR field) | bpm | session | per sleep | |
| | WH `recovery.score.resting_heart_rate` | bpm | daily (per cycle) | | |
| | GH `daily-resting-heart-rate.beatsPerMinute` (+`calculationMethod`) | bpm | daily | | |
| | HC `RestingHeartRateRecord` | bpm | instant | daily | |
| | AP `restingHeartRate` | count/min | daily-ish instant | | |
| | GA `restingHeartRateInBeatsPerMinute` | bpm | daily | | |
| | UH `night_rhr`/`sleep_rhr` | bpm | daily | | |
| average/min/max HR over a window | WH cycle and workout `average_heart_rate`, `max_heart_rate`; OU `sleep.average_heart_rate`; GA `min/max/averageHeartRateInBeatsPerMinute`; WI `hr_average/min/max`; HAE `Min/Avg/Max` | bpm | interval | per cycle/sleep/day/workout | |
| walking_heart_rate_average | AP `walkingHeartRateAverage` | count/min | daily | | Apple only |
| heart_rate_recovery_1min | AP `heartRateRecoveryOneMinute` | count/min | session | per workout | Apple only |
| HR zone time | WH `zone_durations.zone_{zero..five}_milli` (ms, 6 zones); GH `time-in-heart-rate-zone`, `daily-heart-rate-zones`, `active-zone-minutes` (min); GA activity zones (s); WI `hr_zone_0..3` (s, 4 zones); PO exercise zones | ms / min / s | interval | per workout or day | zone counts differ: 4, 5, 6 |
| beat_to_beat (RR intervals) | AP `HKHeartbeatSeriesSample`; GA beat-to-beat (licensed); PO exercise RR samples; FIT `hrv` message | ms | instant per beat | about 1/s | 300..2000 ms |
| irregular rhythm / AFib event | GH `irregular-rhythm-notification`; FB IRN; AP `irregularHeartRhythmEvent`; SA IHRN; WI types 130, 139 | event/classification | session | rare | |
| high/low HR event | AP `highHeartRateEvent`, `lowHeartRateEvent` | event | session | rare | Apple only |

## B2. Heart rate variability

| Canonical metric | Platform: field | Unit | Algorithm | Shape | Cadence |
|---|---|---|---|---|---|
| hrv_rmssd (sleep summary) | OU `sleep.average_hrv` | ms (implied) | RMSSD | session | per sleep |
| | WH `recovery.score.hrv_rmssd_milli` | ms | RMSSD (last slow-wave periods) | daily per cycle | |
| | GH `daily-heart-rate-variability.averageHeartRateVariabilityMilliseconds`, `deepSleepRootMeanSquareOfSuccessiveDifferencesMilliseconds` | ms | RMSSD | daily | |
| | FB `hrv.value.dailyRmssd`, `deepRmssd` | ms | RMSSD | daily | |
| | GA `hrv.lastNightAvg`, `lastNight5MinHigh` | ms | RMSSD | daily | |
| | PO `nightly_recharge.hrv_avg` | ms | RMSSD | daily | |
| | WI sleep `rmssd` | ms | RMSSD | per minute in sleep | |
| | UH `avg_sleep_hrv` | ms | unverified | daily | |
| | CO HRV assessment daily average | ms | unverified | daily | |
| hrv_rmssd (series) | OU `sleep.hrv` `PublicSample` | ms | RMSSD | 300 s | |
| | GH `heart-rate-variability.rootMeanSquareOfSuccessiveDifferencesMilliseconds` | ms | RMSSD | instant, 5 min | |
| | FB intraday `rmssd`, `coverage`, `hf`, `lf` | ms; ms^2 for hf/lf (unverified) | RMSSD + spectral | 5 min in sleep | |
| | HC `HeartRateVariabilityRmssdRecord.heartRateVariabilityMillis` | ms | RMSSD | instant | |
| | GA `hrvValues` | ms | RMSSD | 5 min | |
| | PO `hrv_samples` | ms | RMSSD | 5 min | |
| | OU `session.heart_rate_variability` | ms | RMSSD | 5 s..60 s within a session (unverified) | |
| | UH `hrv` | ms | unverified | about 5 min | |
| | ES `timeseries.hrv` | ms | unverified | per night | |
| hrv_sdnn | AP `heartRateVariabilitySDNN` | ms | **SDNN** over about 60 s spot readings | instant | several per day; every 5 min on Series 12 |
| | WI sleep `sdnn_1`, intraday `sdnn1` | ms | SDNN | per minute | |
| hrv_rmssd (Apple, iOS 27) | AP RMSSD type (identifier unverified) | ms | RMSSD | instant | |
| HRV entropy / non-REM HR | GH `entropy`, `nonRemHeartRateBeatsPerMinute` | unitless; bpm | | daily | Google only |
| hrv status / balance | GA HRV status (`BALANCED`, `UNBALANCED`, `LOW`, `POOR`) [recalled]; OU `contributors.hrv_balance` 1..100; CO normal range | enum/score | | daily | vendor-proprietary |

## B3. Sleep sessions, stages and scores

| Canonical metric | Platform: field | Unit | Shape | Notes |
|---|---|---|---|---|
| sleep session bounds | OU `bedtime_start/end` (local+offset); WH `start/end` + `timezone_offset`; GH `sleep.interval`; HC `SleepSessionRecord`; AP `sleepAnalysis` samples; GA `startTimeInSeconds`+`durationInSeconds`; PO sleep start/end; WI sleep `startdate/enddate`; ES interval `ts` | timestamps | session | naps: OU `type` (`late_nap`, `rest`), WH `nap` bool, HC/GH separate sessions |
| sleep stage timeline | OU `sleep_phase_5_min`, `sleep_phase_30_sec` (char codes 1 deep 2 light 3 REM 4 awake); GH `stages[]`; FB `levels.data[]`; HC `stages[]` (constants 0..7); AP category samples; GA `sleepLevelsMap`; PO `hypnogram` (codes 0..5); WI sleep v2 `state` 0..3; ES `stages[]` | per-segment | series | 30 s typical epoch; OU 30 s or 5 min |
| time_in_bed | OU `time_in_bed` (s); WH `total_in_bed_time_milli` (ms); WI `total_timeinbed` (s); UH `time_in_bed`; AP `inBed` spans | s / ms | session | |
| total_sleep | OU `total_sleep_duration` (s); GH `summary.totalDuration` [unverified]; GA `durationInSeconds`; WI `total_sleep_time` (s); UH `total_sleep` | s | session | WHOOP has no total: sum of light+SWS+REM |
| deep sleep | OU `deep_sleep_duration` s; WH `total_slow_wave_sleep_time_milli` ms; GA `deepSleepDurationInSeconds`; WI `deepsleepduration` s; PO `deep_sleep` s; UH `deep_sleep` | s / ms | session | WHOOP calls it slow wave sleep |
| light sleep | OU `light_sleep_duration`; WH `total_light_sleep_time_milli`; GA `lightSleepDurationInSeconds`; WI `lightsleepduration`; PO `light_sleep`; AP `asleepCore` | s / ms | session | Apple calls it Core |
| REM sleep | OU `rem_sleep_duration`; WH `total_rem_sleep_time_milli`; GA `remSleepInSeconds`; WI `remsleepduration`; PO `rem_sleep` | s / ms | session | |
| awake in sleep | OU `awake_time`; WH `total_awake_time_milli`; GA `awakeDurationInSeconds`; WI `wakeupduration`, `waso` | s / ms | session | |
| unmeasured | WH `total_no_data_time_milli`; GA `unmeasurableSleepInSeconds` | ms / s | session | |
| sleep latency | OU `latency` s; WI `sleep_latency` s | s | session | |
| sleep efficiency | OU `efficiency` 1..100; WH `sleep_efficiency_percentage`; WI `sleep_efficiency` (0..1 or %, unverified); UH `sleep_efficiency` | % | session | |
| wake/disturbance count | WH `disturbance_count`; WI `wakeupcount`; OU `restless_periods`; UH `tosses_and_turns`; ES `tnt` | count | session | different definitions |
| sleep cycles | WH `sleep_cycle_count`; UH `full_sleep_cycles` | count | session | |
| movement during sleep | OU `movement_30_sec` (codes 1..4); WI `mvt_score`; OU `session.motion_count` | code / score | series | |
| sleep need / debt | WH `sleep_needed.*_milli` (ms) | ms | daily | WHOOP only |
| sleep score | OU `daily_sleep.score` 1..100 (+7 contributors); WH `sleep_performance_percentage`, `sleep_consistency_percentage`; GA `overallSleepScore.value` 0..100 (+`qualifierKey`); PO `sleep_score` 1..100, `continuity` 1..5; WI `sleep_score` 0..100; UH `sleep_score`; CO sleep score; ES `score`; SA sleep score | 0..100 | daily/session | all proprietary |
| optimal bedtime | OU `sleep_time.optimal_bedtime` (offsets s, `day_tz`); PO SleepWise circadian bedtime | s from midnight | daily | proprietary |
| apnea / breathing disturbance | OU `daily_spo2.breathing_disturbance_index` 0..100; WI `apnea_hypopnea_index` events/h, `breathing_disturbances_intensity`; AP `appleSleepingBreathingDisturbances`, `sleepApneaEvent`; SA sleep apnea | index | daily | not comparable across vendors |
| snoring | WI `snoring` (s), ES (unverified) | s | session | |

Stage-name sets (the core disagreement):

| Platform | Stage values |
|---|---|
| Oura | deep, light, REM, awake (codes 1..4) |
| WHOOP | awake, light, slow wave, REM, no data |
| Google Health | AWAKE, LIGHT, DEEP, REM; ASLEEP, RESTLESS (classic) |
| Fitbit legacy | deep, light, rem, wake; asleep, restless, awake (classic) |
| Health Connect | UNKNOWN, AWAKE, SLEEPING, OUT_OF_BED, LIGHT, DEEP, REM, AWAKE_IN_BED |
| Apple | inBed, asleepUnspecified, awake, asleepCore, asleepDeep, asleepREM |
| Garmin | deep, light, rem, awake (+ unmeasurable total) |
| Polar | wake, REM, lighter non-REM, light non-REM, deep non-REM, unknown (0..5) |
| Withings | awake, light, deep, REM (0..3) |
| Eight Sleep | awake, light, deep, rem, out (unverified) |

A canonical set needs at least: `in_bed`, `awake`, `asleep_unspecified`,
`light` (= Core = N1+N2), `deep` (= slow wave = N3), `rem`, `out_of_bed`,
`unmeasured`. Polar's split of light into "lighter" and "light" collapses into
`light`; Fitbit/Google "restless" maps to `asleep_unspecified` with a flag.

## B4. Respiration

| Canonical metric | Platform: field | Unit | Shape | Cadence |
|---|---|---|---|---|
| respiratory_rate (sleep avg) | OU `sleep.average_breath`; WH `sleep.score.respiratory_rate`; GH `daily-respiratory-rate.breathsPerMinute`, `respiratory-rate-sleep-summary`; FB `br.value.breathingRate` (+ per stage); PO `breathing_rate_avg`; WI `rr_average`; ES | breaths/min | session/daily | per sleep |
| respiratory_rate (series) | GA `timeOffsetEpochToBreaths`; GA sleep `timeOffsetSleepRespiration`; HC `RespiratoryRateRecord.rate`; AP `respiratoryRate`; PO `breathing_samples`; WI sleep `rr`; ES `timeseries.respiratoryRate` | breaths/min | instant | 1 min (GA), 5 min (PO), per minute (WI) |

## B5. Blood oxygen (SpO2)

| Canonical metric | Platform: field | Unit | Shape | Cadence | Range |
|---|---|---|---|---|---|
| spo2 (nightly average) | OU `daily_spo2.spo2_percentage.average`; WH `recovery.score.spo2_percentage`; GH `daily-oxygen-saturation.averagePercentage` (+lower/upper bound, stddev); FB `spo2.value.avg/min/max`; UH `spo2` | % | daily | per night | 80..100 |
| spo2 (series or spot) | GH `oxygen-saturation.percentage` (1 min, validated); FB intraday (1 min); HC `OxygenSaturationRecord.percentage`; AP `oxygenSaturation` (**fraction 0..1**); GA `timeOffsetSpo2Values`, `timeOffsetSleepSpo2`; PO SpO2 test; WI type 54, `spo2_auto`; SA blood oxygen | % or fraction | instant | 1 min in sleep; spot otherwise | 70..100 |

## B6. Temperature

| Canonical metric | Platform: field | Unit | Kind of value | Shape |
|---|---|---|---|---|
| skin temperature deviation from personal baseline | OU `daily_readiness.temperature_deviation`, `temperature_trend_deviation`; FB `nightlyRelative`; GA `skinTemp.avgDeviationCelsius` [recalled]; HC `SkinTemperatureRecord.deltas[].delta` (with `baseline`); UH `temperature_deviation` | degC delta (FB: C or F by header) | **relative** | daily / series |
| skin temperature absolute | WH `recovery.score.skin_temp_celsius`; GH `daily-sleep-temperature-derivations` (average and nightly lowest, Celsius), `skin-temperature-sensors` (v4beta); AP `appleSleepingWristTemperature`; UH `temp` series, `average_body_temperature`; PO sleep skin temperature; WI type 73 | degC | **absolute** | daily / series |
| body (core) temperature | GH `core-body-temperature` (C, 0..100); FB `temp/core`; HC `BodyTemperatureRecord` (+ `measurementLocation`); AP `bodyTemperature`; WI types 12, 71; PO body temperature | degC (or F in FB/AP display) | absolute | instant (spot) |
| basal body temperature | HC `BasalBodyTemperatureRecord`; AP `basalBodyTemperature` | degC | absolute | daily |
| bed / room temperature | ES `tempBedC`, `tempRoomC` | degC | environment | series |

## B7. Activity: steps, energy, distance, floors, intensity

| Canonical metric | Platform: field | Unit | Shape | Cadence |
|---|---|---|---|---|
| steps | OU `daily_activity.steps`; WH `cycle.step_count` (unverified, new); GH `steps.count`; FB `activities-steps` (+intraday 1 min); HC `StepsRecord.count`; AP `stepCount`; GA `steps` (dailies, epochs 15 min); PO daily `steps` + samples; WI `steps` (daily + intraday); UH `steps`; CO; SA | count | interval / daily | 1 min (GH/FB), 15 min (GA epochs), irregular (AP) |
| step cadence | HC `StepsCadenceRecord` (steps/min); GA `stepsPerMinute`; AP running cadence via workout | steps/min | instant | 1 s in workout |
| active energy | OU `active_calories` kcal; WH (none; only total kJ); GH `active-energy-burned` kcal; HC `ActiveCaloriesBurnedRecord.energy`; AP `activeEnergyBurned` kcal (user may set kJ); GA `activeKilocalories`; PO `active_calories` kcal; WI `calories` kcal | kcal | interval/daily | 1 min (GH) |
| total energy | OU `total_calories` kcal; WH cycle/workout `kilojoule` **kJ**; GH `total-calories` kcal; HC `TotalCaloriesBurnedRecord`; GA active + `bmrKilocalories`; WI `totalcalories` kcal | kcal / kJ | interval/daily | |
| basal energy / BMR | GH `basalEnergyBurned`; HC `BasalMetabolicRateRecord` (**power**, kcal/day); AP `basalEnergyBurned` (energy, kcal); GA `bmrKilocalories`; WI type 226 kcal | kcal or kcal/day | daily / instant | |
| distance | OU `equivalent_walking_distance` m (an energy equivalent, not travelled); WH workout `distance_meter`; GH `distance.millimeters` **mm**; FB `distance` **km or miles by header**; HC `DistanceRecord.distance` (Length); AP `distanceWalkingRunning` etc. (m default, km/mi display); GA `distanceInMeters`; PO m; WI `distance` m | m / mm / km / mi | interval | 1 min (GH) |
| floors | GH `floors` (count); FB `floors`; HC `FloorsClimbedRecord.floors` (double); AP `flightsClimbed`; GA `floorsClimbed`; SA floors | count | interval | 1 min |
| elevation gain | GH `altitude.gainMillimeters` mm; FB `elevation` (m or ft); HC `ElevationGainedRecord` (Length); GA `totalElevationGainInMeters`; WH `altitude_gain_meter`; WI `elevation` m | m / mm / ft | interval | |
| active / intensity minutes | GH `active-minutes`, `active-zone-minutes`, `activity-level`; FB `minutesLightlyActive/FairlyActive/VeryActive`, AZM; GA `moderate/vigorousIntensityDurationInSeconds`; OU `high/medium/low_activity_time` s, `class_5_min`; AP `appleExerciseTime` (min), `appleStandTime`, `appleMoveTime`; WI `soft/moderate/intense` s; HC `ActivityIntensityRecord` (moderate/vigorous); UH `active_minutes` | min / s | interval/daily | definitions differ per vendor |
| sedentary time | GH `sedentary-period`; OU `sedentary_time` s; GA epochs `SEDENTARY` | s / interval | interval | |
| MET | OU `met` series (60 s), `*_met_minutes`; GA epochs `met`; FB `activityCalories`-derived | MET, MET-min | series/daily | |
| non-wear | OU `non_wear_time` s, `class_5_min` '0'; GA stress `-1` off wrist | s | daily | |
| stand hours | AP `appleStandHour` category | count | daily | Apple only |
| speed / power / cadence in workout | HC `SpeedRecord`, `PowerRecord`, `CyclingPedalingCadenceRecord`; AP `runningSpeed`, `runningPower`, `cyclingPower`, `cyclingCadence`; GA `speedMetersPerSecond`, `powerInWatts`, `bikeCadenceInRPM`; FIT records | m/s, W, rpm | instant | 1 s |
| GPS track | HC `ExerciseRoute`; AP `HKWorkoutRoute` / GPX; GA `latitudeInDegree`/`longitudeInDegree`; PO route/GPX; FIT; GH TCX export | degrees, m | instant | 1 s |
| swim | GH `swim-lengths-data`; GA `swimCadenceInStrokesPerMinute`, SWOLF; AP `swimmingStrokeCount`, `distanceSwimming`; WI `stroke`, `pool_lap` | count, m | interval | per length |
| workout session | OU `workout` (`activity`, `intensity`, `calories` kcal, `distance` m, `source`); WH `workout` (`sport_name`, strain, kJ, zones); GH `exercise` (`exerciseType` enum, `metricsSummary`, `splits[]`); HC `ExerciseSessionRecord` (exercise type int constants, segments, laps); AP `HKWorkout` (`HKWorkoutActivityType`); GA `activities`; PO exercises; WI `getworkouts`; CO; SU | session | session | sport taxonomies all differ |

## B8. Body composition and weight

| Canonical metric | Platform: field | Unit | Shape |
|---|---|---|---|
| weight | WH `user/measurement/body.weight_kilogram` kg; GH `weight.weightGrams` **g**; FB `weight` (kg, lb or stone by header); HC `WeightRecord.weight` (Mass); AP `bodyMass` (kg default, lb display); GA `weightInGrams` **g** [recalled]; PO physical info kg; WI type 1 (value x 10^unit kg); OU `personal_info.weight` kg | kg / g / lb | instant |
| height | WH `height_meter` m; GH `height` (unit unverified, likely mm); HC `HeightRecord` (Length); AP `height`; PO cm; WI type 4 m; OU `personal_info.height` m | m / cm / mm | instant |
| BMI | AP `bodyMassIndex`; GA `bodyMassIndex`; FB `bmi`; GH none (client computes) | kg/m^2 | instant |
| body fat % | GH `body-fat.percentage`; HC `BodyFatRecord.percentage`; AP `bodyFatPercentage` (fraction 0..1); GA `bodyFatInPercent`; WI type 6; FB `fat` | % or fraction | instant |
| fat mass | WI type 8 kg | kg | instant |
| fat-free / lean mass | WI type 5 kg; HC `LeanBodyMassRecord`; AP `leanBodyMass` | kg | instant |
| muscle mass | WI type 76 kg; GA `muscleMassInGrams` g | kg / g | instant |
| bone mass | WI type 88 kg; HC `BoneMassRecord`; GA `boneMassInGrams` g | kg / g | instant |
| body water | WI type 77 (hydration, kg); HC `BodyWaterMassRecord` (kg); GA `bodyWaterInPercent` (%); WI 168/169 extra/intracellular water kg | kg or % | instant |
| visceral fat | WI type 170 (index) | rating | instant |
| waist circumference | AP `waistCircumference` | m / cm | instant |
| segmental composition | WI 173..175 and related (unverified) | kg | instant |
| metabolic age | WI 227 | years | instant |

## B9. Glucose

| Canonical metric | Platform: field | Unit | Shape | Cadence |
|---|---|---|---|---|
| glucose (CGM) | DX `egvs[].value` (+`trend`, `trendRate` mg/dL/min, `systemTime` UTC, `displayTime` local); LI historic/scan glucose; UH `glucose` | mg/dL (LI also mmol/L) | instant | DX 5 min; LI3 1 min; LI2 15 min history |
| glucose (spot / any source) | GH `blood-glucose.bloodGlucoseMilligramsPerDeciliter` (+`measurementSource`, `mealType`, `measurementTiming`, `specimen`); HC `BloodGlucoseRecord.level` (+`specimenSource`, `mealType`, `relationToMeal`); AP `bloodGlucose` (mg/dL or mmol/L, + `HKMetadataKeyBloodGlucoseMealTime`); SA blood glucose; WI appli 58 | mg/dL or mmol/L | instant | |
| CGM summaries | UH `average_glucose`, `glucose_variability` %, `time_in_target` %, `hba1c` (estimate), `metabolic_score` | mixed | daily | proprietary |
| calibrations, events | DX `calibrations`, `events` (carbs, insulin, exercise, health) | | event | |

## B10. Blood pressure

| Canonical metric | Platform: field | Unit | Shape |
|---|---|---|---|
| systolic / diastolic | WI types 10 / 9; HC `BloodPressureRecord.systolic/diastolic` (Pressure) + `bodyPosition`, `measurementLocation`; AP `bloodPressureSystolic/Diastolic` correlation; GA `bloodPressures.systolic/diastolic`; SA blood pressure; FB (none) | mmHg | instant (spot) |
| pulse with BP | WI type 11; GA `pulse` | bpm | instant |
| hypertension notification | AP (watchOS 26, `hypertensionEvent`, unverified) | event | session |
| pulse wave velocity / vascular age | WI 91 (m/s), 155 (years); OU `daily_cardiovascular_age.pulse_wave_velocity` (m/s), `vascular_age` (years) | m/s; years | instant/daily |

## B11. ECG

| Canonical metric | Platform: field | Unit | Shape |
|---|---|---|---|
| ECG recording | GH `electrocardiogram` (`sampleTime`, `heartRateBpm`, `resultClassification`, waveform unverified); FB `waveformSamples`, `samplingFrequencyHz`, `scalingFactor`, `resultClassification`; AP `HKElectrocardiogram` (voltage microvolts, 512 Hz, `classification`, `averageHeartRate`); WI Heart v2 `signal` (microvolts, 300/500 Hz) + types 135..138 intervals ms; PO wrist ECG test; SA (Samsung Health Monitor) | microvolts at 250..512 Hz | session (30 s typical) |
| ECG intervals | WI QRS 135, PR 136, QT 137, QTc 138 | ms | instant |
| classification | AP `sinusRhythm`, `atrialFibrillation`, `inconclusive*`; FB/GH result classifications; WI AFib type 130 | enum | session |

A 30 s single-lead ECG at 512 Hz is about 15,000 samples (60 KB as int32, 100+ KB
as JSON): this is the case for `blobref` rather than properties.

## B12. Menstrual cycle and reproductive health

| Canonical metric | Platform: field | Shape |
|---|---|---|
| period / flow | HC `MenstruationPeriodRecord` (interval), `MenstruationFlowRecord` (`flow` light/medium/heavy); AP `menstrualFlow` (`unspecified, light, medium, heavy, none`) + `HKMetadataKeyMenstrualCycleStart`; GH `menstrual-period` (write-only); GA Women's Health `mct` | interval / daily |
| ovulation test | HC `OvulationTestRecord` (`positive, high, negative, inconclusive`); AP `ovulationTestResult`; GH `ovulation-test` (write-only) | instant |
| cervical mucus | HC `CervicalMucusRecord`; AP `cervicalMucusQuality` | instant |
| intermenstrual bleeding | HC, AP | instant |
| sexual activity | HC `SexualActivityRecord`; AP `sexualActivity` | instant |
| basal body temperature | HC, AP | daily |
| cycle phase / predictions | GA `dayInCycle`, `cycleLength`, `currentPhase` [recalled]; OU (Natural Cycles partnership, not in API) | daily, proprietary |

## B13. Vendor scores (stress, readiness, strain, recovery, load)

None of these has a cross-vendor meaning. Store as mirror-native values with the
vendor's name; do not map one onto another.

| Score | Platform: field | Range / unit | Inputs (vendor-described) |
|---|---|---|---|
| Readiness | OU `daily_readiness.score` + 9 contributors | 1..100 | HRV balance, RHR, temperature, sleep, activity |
| Recovery | WH `recovery_score` | 0..100 % | HRV, RHR, sleep, SpO2, skin temp |
| Recovery | UH `recovery`, `recovery_index`; CO recovery % and time to full recovery | 0..100 | |
| Strain | WH `strain` (cycle, workout) | 0..21 logarithmic | cardiovascular load |
| Body Battery | GA `bodyBatteryChargedValue`, `DrainedValue`, `timeOffsetBodyBatteryValues` | 0..100 | stress, sleep, activity |
| Stress level | GA `timeOffsetStressLevelValues`, `averageStressLevel` | 1..100 (rest < 26, low 26..50, medium 51..75, high 76..100; negatives are status codes) | HRV |
| Stress | OU `daily_stress.stress_high`, `recovery_high` (s), `day_summary` | seconds; enum | |
| Resilience | OU `daily_resilience.level` + contributors | enum of 5 | |
| Energy score | SA `ENERGY_SCORE` | 0..100 (unverified) | |
| Nightly Recharge / ANS charge | PO `ans_charge`, `nightly_recharge_status` | -10..+10; 1..6 | HRV, breathing, HR |
| Cardio load | PO `cardio_load`, `strain`, `tolerance`, `cardio_load_ratio` | TRIMP-based | |
| Training load / status | CO short/long load and ratio; GA Training API (acute load, training status, unverified) | | |
| Activity score | OU `daily_activity.score` + 6 contributors | 1..100 | |
| Sleep scores | see B3 | 0..100 | |
| Movement index | UH `movement_index` | score | |
| Morning alertness | UH `morning_alertness` | | |
| Metabolic score | UH `metabolic_score` | | |
| Active Zone Minutes | GH/FB AZM (minutes weighted x2 in cardio/peak zones) | min | HR zones |
| Move/Exercise/Stand rings | AP `ActivitySummary` (`activeEnergyBurned`, goal, `appleExerciseTime`, `appleStandHours`) | kcal, min, h | |
| Cardiovascular age | OU `vascular_age`; WI 155 | years | PWV |
| Fitness age | GA `fitnessAge` | years | VO2max |
| Vitality and other new scores | WI "coming soon" list | | |

## B14. VO2max and fitness estimates

| Canonical metric | Platform: field | Unit | Shape | Notes |
|---|---|---|---|---|
| vo2max | OU `vO2_max.vo2_max` int; GH `vo2-max`, `daily-vo2-max`, `run-vo2-max` (`mlPerKgPerMinute`, `measurementMethod`); FB `cardioscore.vo2Max` (value **or range string "44-48"**); HC `Vo2MaxRecord.vo2MillilitersPerMinuteKilogram` (+`measurementMethod`); AP `vo2Max` mL/(kg*min); GA `vo2Max`, `vo2MaxCycling`; PO physical info `vo2-max`; WI type 123; UH `vo2_max`; CO | ml/kg/min | instant/daily | estimation method differs: running pace+HR, resting, cycling power |
| lactate/HR thresholds | PO aerobic/anaerobic threshold (bpm); CO threshold pace | bpm, s/km | instant | |
| race predictions | CO; GA (unverified) | durations | daily | proprietary |
| six-minute walk, walking metrics | AP `sixMinuteWalkTestDistance`, `walkingSpeed`, `walkingStepLength`, `walkingAsymmetryPercentage`, `walkingDoubleSupportPercentage`, `stairAscentSpeed`, `appleWalkingSteadiness` | m, m/s, % | daily-ish | Apple only |

## B15. Hydration and nutrition

| Canonical metric | Platform: field | Unit | Shape |
|---|---|---|---|
| water intake | GH `hydration-log` (VolumeQuantity); FB water log (ml or fl oz by header); HC `HydrationRecord.volume`; AP `dietaryWater` (mL); SA water intake | mL / fl oz | session/instant |
| food log | GH `nutrition-log`, `food`, `food-measurement-unit`; FB food logs; HC `NutritionRecord` (energy, about 40 nutrients, `mealType`); AP `dietaryEnergyConsumed` and 30+ `dietary*` types; SA nutrition; OU webhook `meal` type (no read route yet) | kcal, g, mg, ug | session |

## B16. Other signals seen

| Metric | Platform | Unit |
|---|---|---|
| device battery | OU `ring_battery_level.level` | % |
| device and firmware | OU `ring_configuration`; GH `pairedDevices`; DX `devices`; HC `metadata.device` | |
| mindfulness / sessions / mood | OU `session` (+`mood`); HC `MindfulnessSessionRecord`; AP `mindfulSession`, `stateOfMind`; GH `moods` (write-only) | session |
| tags / journal | OU `tag`, `enhanced_tag` (`tag_type_code`, start/end, comment); WHOOP journal (CSV only) | event |
| rest/illness mode | OU `rest_mode_period` | session |
| daylight, audio exposure | AP `timeInDaylight` (min), `environmentalAudioExposure` (dBASPL) | |
| electrodermal activity | WI 229 (unverified); Fitbit EDA scan (not in Google Health list) | score |
| urine analysis | WI U-Scan (pH, specific gravity, ketones, vitamin C, calcium, creatinine) | mixed |
| nerve health | WI 167 | score |

---

# Part C. Unit and semantic disagreements to design for

1. **Energy**: WHOOP sends kJ (`kilojoule`); everyone else kcal (Oura,
   Garmin, Google Health, Polar, Withings); Apple and Health Connect store a typed
   energy and let the reader pick; Health Auto Export emits whatever unit the user
   chose. Basal energy is an energy (kcal) at Apple and Garmin but a power
   (kcal/day) in Health Connect's `BasalMetabolicRateRecord`.
2. **Durations**: WHOOP milliseconds (`*_milli`); Oura, Garmin, Withings, Polar
   seconds (Polar also ISO 8601 periods `PT3H11M`); Google Health minutes for
   active/zone minutes; Apple exercise time minutes.
3. **Distance**: Google Health millimetres (`millimeters`, `gainMillimeters`);
   most others metres; Fitbit km or miles by `Accept-Language`; Oura's
   `equivalent_walking_distance` is an energy equivalent, not distance travelled.
4. **Mass**: Google Health grams (`weightGrams`), Garmin grams
   (`weightInGrams`), WHOOP/Polar/Withings kilograms, Fitbit kg/lb/stone by header,
   Withings as mantissa x 10^exponent.
5. **Height**: WHOOP and Withings metres, Polar centimetres, Google Health
   unverified (likely millimetres).
6. **Fractions vs percent**: Apple `oxygenSaturation` and `bodyFatPercentage` are
   0..1 fractions; everyone else 0..100. Withings `sleep_efficiency` may be a
   fraction (unverified).
7. **HRV algorithm**: Apple SDNN (ms) until iOS 27; Withings reports both SDNN and
   RMSSD; everyone else RMSSD. Window differs: WHOOP last slow-wave sleep, Oura
   whole night average of 5-minute values, Google/Fitbit deep sleep and whole sleep,
   Apple about 60 s spot readings. Same unit (ms), not the same quantity.
8. **Temperature**: absolute degC (WHOOP `skin_temp_celsius`, Apple wrist
   temperature, Google `daily-sleep-temperature-derivations`, Withings) versus
   deviation from a personal baseline (Oura `temperature_deviation`, Fitbit
   `nightlyRelative`, Garmin `avgDeviationCelsius`, Health Connect `deltas` with a
   `baseline`). Fitbit returns F for `en_US`. A deviation in F is a 1.8x scale with
   no offset, unlike an absolute conversion. Skin, wrist, finger, core and basal are
   different body sites.
9. **Glucose**: mg/dL (Dexcom always, Google always) versus mmol/L (Libre outside
   the US, Apple by user preference). Factor 18.016 (molar mass of glucose / 10).
   Dexcom out-of-range readings are status flags, not numbers.
10. **Sleep stages**: name sets differ (B3). "Core" (Apple) = "light"; "slow wave"
    (WHOOP) = "deep"; Polar has two light stages; classic Fitbit/Google has
    restless/asleep without depth; Health Connect separates `AWAKE_IN_BED` and
    `OUT_OF_BED`; Apple's `inBed` overlaps the asleep samples rather than
    partitioning time.
11. **Day boundary**: Oura's activity day starts 04:00 local; WHOOP's day is a
    physiological cycle (wake to wake) with no fixed clock boundary; Garmin uses
    `calendarDate` local midnight; Google `dailyRollUp` uses civil days in the
    user's offset at the time. Daily values from different vendors do not cover the
    same 24 hours.
12. **Time encoding**: UTC instant plus offset (WHOOP `timezone_offset` string,
    Garmin `startTimeOffsetInSeconds`, Google `utcOffset` duration, Health Connect
    `ZoneOffset`, Polar offset minutes); local time with offset in one string (Oura,
    Apple export, Health Auto Export); local time without offset (Fitbit legacy,
    LibreView CSV); both a UTC and a user-adjustable "display" clock (Dexcom
    `systemTime`/`displayTime`); IANA zone name (Withings `timezone`). Ultrahuman
    applies the user's *latest* timezone to past data.
13. **Packed series**: Oura `PublicSample {interval, items[]}` and character-coded
    strings; Garmin `timeOffset*` maps of seconds-after-start to value; Health
    Connect series records; FIT binary; Polar comma-separated sample strings;
    Withings sleep v2 per-minute maps. A provider must unpack each into individual
    samples or keep the packed form as one value.
14. **Sentinel values**: Garmin stress -1/-2/-3 and averageStressLevel -1; Oura
    `items[]` contains nulls; Dexcom status codes; Google Health only "validated"
    SpO2; Health Connect `RecordingMethod` (active, automatic, manual).
15. **Integers as strings**: Google Health int64 fields are JSON strings
    (`"beatsPerMinute": "61"`).
16. **Range-valued numbers**: Fitbit cardio fitness `"44-48"`.
17. **Zone counts**: WHOOP 6 HR zones (0..5), Withings 4, Google/Fitbit 3 named
    zones plus AZM, Garmin 5. Zone boundaries are user-specific.

---

# Part D. Change signalling, sync and volume

## D1. How each platform signals new and changed data

| Platform | New data | Changed data | Deleted data | Cursor for polling |
|---|---|---|---|---|
| Oura | webhook `create` per data_type | webhook `update`; same `id`; `sleep_analysis_reason` | webhook `delete`; `sleep.type = deleted` | none; date range re-read |
| WHOOP | webhook `*.updated` | `*.updated`, `updated_at` moves | `*.deleted` | `start`/`end` + `nextToken`; no cycle webhook |
| Google Health | subscriptions (shape unverified) | data point `patch`; reconcile view changes | `batchDelete` | `pageToken`, time filters |
| Fitbit legacy | subscription notification per collection and date | same (re-read the date) | same | none |
| Health Connect | `getChanges` (upserts) | `getChanges`, `metadata.lastModifiedTime` | `getChanges` deletions | changes token (30 days) |
| Apple HealthKit | `HKObserverQuery` + anchored query | sample replaced (delete + insert) | anchored query deleted objects | `HKQueryAnchor` |
| Garmin | push/ping per summary type | the same `summaryId` re-sent with fuller data | deregistration / permission change notices | none (backfill only) |
| Polar | webhook per type | re-fetch | (unverified) | hashed-id lists |
| Withings | `notify` callback with a time window | `getmeas` `lastupdate` returns modified groups | `lastupdate` returns deleted groups | `lastupdate` |
| Dexcom | none | none | none | 30-day windows, `dataRange` |
| Ultrahuman | none | none | none | per day |

## D2. Estimated data volume per heavy user per day

Assumptions: the user wears the device 24 h, sleeps 8 h, does one 90-minute
GPS workout with a chest strap, syncs often. "Points" are individual numeric samples
after unpacking. Bytes are vendor JSON as received; a compact columnar store needs
roughly 10 to 30 bytes per point. These are estimates, not measurements.

| Platform | Main contributors | Points/day | Vendor JSON/day |
|---|---|---|---|
| Oura | `heartrate` (5 min at rest, finer during sleep/workout), sleep `hrv`/`heart_rate` at 300 s, `sleep_phase_30_sec` (960 chars), `movement_30_sec`, `met` at 60 s (1,440), daily docs | 2,000..4,000 | 150..400 KB |
| WHOOP (API) | 1 cycle, 1 recovery, 1..2 sleeps, 1..3 workouts | about 100 scalars | 5..15 KB |
| Google Health | `heart-rate` at 1..5 s (17k..86k), steps/distance/calories/AZM per minute (about 6k), SpO2 per minute in sleep (480), HRV 5 min, sleep stages | 25,000..95,000 | 4..15 MB (verbose per-point envelopes with `dataSource`) |
| Fitbit legacy | intraday HR 1 s where recorded (about 20k..40k), 1-min steps/cal/dist/floors (5,760), sleep 30 s levels | 25,000..45,000 | 1..3 MB |
| Apple HealthKit | HR every 5 s (17,280) on Series 12; steps, distance, active and basal energy as many short samples (5k..15k); workout route 1 Hz (5,400 points); HRV every 5 min; sleep stage samples | 30,000..45,000 | 8..15 MB as export.xml rows (about 250 B each); a multi-year export.xml is 1..5 GB |
| Garmin | `timeOffsetHeartRateSamples` at 15 s (5,760), stress and Body Battery at 3 min (960), respiration 1 min (1,440), SpO2 in sleep (480), HRV 5 min (96), epochs (96 x ~10), activity details 1 s x 90 min x ~12 channels (65k) or the FIT file | 15,000 + 65,000 workout | 0.5 MB + 2..5 MB workout JSON (FIT about 0.3..1 MB) |
| Polar | continuous HR 5 min (288), activity samples, nightly recharge 5 min, exercise 1 s samples | 1,000 + workout 30k..60k | 0.2 MB + 1..3 MB |
| Withings (ScanWatch + Sleep Analyzer + scale) | intraday per minute when active, sleep v2 per-minute `hr`/`rr`/`snoring`/`rmssd`, a few measure groups | 3,000..6,000 | 200..500 KB |
| Dexcom | 288 EGVs | 288 x ~8 fields | about 60..100 KB |
| Libre 3 | 1,440 readings | 1,440 | about 150..250 KB |
| Ultrahuman | hr, temp, hrv, motion, steps series about 5 min, sleep, CGM (if M1) | 1,500..3,000 | 100..300 KB |
| Eight Sleep | per-night series for HR, HRV, RR, bed and room temperature | 500..2,000 | 50..200 KB |
| Health Connect / Samsung | whatever devices write; Galaxy Watch HR every 10 min to continuous, 1 s in workouts | 2,000..90,000 | depends on uploader |

Order of magnitude: a heavy multi-device user produces 10^4 to 10^5 numeric
points per day (about 4 to 40 million per year) and 1 to 20 MB of vendor JSON per
day. At one changelog line per point, a year is tens of millions of lines per
user, which rules out one record per sample for the dense series (heart rate, steps
per minute, GPS, CGM) and points at a packed-series value or a blob per window.
Daily summaries, sessions and scores are small (tens to hundreds per day) and fit
the one-record-per-object mirror model the WHOOP bundle already uses.

## D3. Rate limits and windows in one place

| Platform | Limit | Max window per request | Page size |
|---|---|---|---|
| Oura | per token and per app, unpublished; `Retry-After` | date range, unpublished | `next_token` |
| WHOOP | 100/min, 10,000/day per app | `start`/`end` | 25 |
| Google Health | 300/min per user; 120k/min, 86.4M/day per project | 14 days (HR, total calories, active minutes, HR-zone calories); 90 days rollups | `pageToken` |
| Fitbit legacy | 150/hour per user | 1 day for 1 s intraday, 24 h | |
| Garmin | production unthrottled (claimed); about 100/min assumed | 24 h per backfill call; about 30..90 days backfill | |
| Polar | 15 min: 500 + 20 x users; 24 h: 5000 + 100 x users | 28 days activity, 30-day exercise availability | |
| Withings | about 120/min (unverified) | `startdate`/`enddate`, `lastupdate` | `offset` + `more` |
| Dexcom | 60,000/hour per app | 30 days | |
| Ultrahuman | unpublished | 7 days | |
| Health Connect | on-device quotas | 30 days history without `READ_HEALTH_DATA_HISTORY` | `pageToken` |

---

# Sources

Vendor primary sources (fetched 2026-10-08):

- Oura OpenAPI 1.41: https://cloud.ouraring.com/v2/static/json/openapi-1.41.json , docs https://cloud.ouraring.com/v2/docs
- WHOOP: https://developer.whoop.com/api , https://developer.whoop.com/docs/developing/webhooks , https://developer.whoop.com/docs/developing/rate-limiting
- Google Health API: https://developers.google.com/health/data-types , https://developers.google.com/health/reference/rest , https://developers.google.com/health/reference/rest/v4/users.dataTypes.dataPoints , https://developers.google.com/health/reference/rpc/google.devicesandservices.health.v4 , https://developers.google.com/health/rate-limits , https://developers.google.com/health/migration , https://developers.google.com/health/migration/api-specifications
- Fitbit Web API: https://dev.fitbit.com/build/reference/web-api/temperature/get-temperature-skin-summary-by-date/ , https://dev.fitbit.com/build/reference/web-api/intraday/get-hrv-intraday-by-date/
- Health Connect: https://developer.android.com/health-and-fitness/health-connect/data-types , https://developer.android.com/health-and-fitness/health-connect/migration/fit , https://developer.android.com/reference/kotlin/androidx/health/connect/client/records/SkinTemperatureRecord
- Samsung: https://developer.samsung.com/health/data/overview.html , https://developer.samsung.com/health/blog/en/new-health-data-type-support-in-samsung-health-data-sdk
- Apple: https://developer.apple.com/documentation/healthkit/hkcategoryvaluesleepanalysis/asleeprem
- Garmin: https://developer.garmin.com/gc-developer-program/health-api/
- Polar: https://www.polar.com/accesslink-api/
- Withings: https://developer.withings.com/developer-guide/v3/data-api/all-available-health-data/
- Dexcom: https://developer.dexcom.com/docs/dexcomv3/endpoint-overview/
- Ultrahuman: https://vision.ultrahuman.com/developer-docs
- Suunto: https://apizone.suunto.com/webhooks

Secondary sources:

- https://tryterra.co/blog/everything-you-need-to-know-about-google-health-new-api
- https://sahha.ai/blog/google-fit-api-sunset-migration/
- https://openwearables.io/blog/garmin-connect-api-developer-guide-activities-health-metrics
- https://github.com/antsal06/wearable-api-field-guide/blob/main/providers/garmin.md
- https://ilumivu.freshdesk.com/support/solutions/articles/9000258793-garmin-health-api-data-daily-summary
- https://ilumivu.freshdesk.com/support/solutions/articles/9000259229-garmin-health-api-data-stress-body-battery-respiration-rate
- https://www.themomentum.ai/blog/apple-watch-series-12-ios-27-rmssd-healthkit
- https://help.healthyapps.dev/en/health-auto-export/export-format/
- https://www.diabetescare.abbott/partnerships/integrations/en.html , https://docs.junction.com/wearables/guides/abbott-libreview , https://ccnhealth.com/articles/announcements/ccn-health-freestyle-libre-integration , https://github.com/FokkeZB/libreview-unofficial
- https://github.com/api-evangelist/eight-sleep , https://github.com/mezz64/pyEight
- https://openwearables.io/blog/how-to-integrate-suunto-data-into-your-health-app
- https://the5krunner.com/2026/05/13/coros-mcp-ai-data/ , https://tryterra.co/integrations/coros

Repository: `kinds/providers.substrate.reamde.dev/whoop/bundle.yaml` and `README.md`.
