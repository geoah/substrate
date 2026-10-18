# 04. Health data aggregators and open data models

Researcher slice: how others already solved "many sources, one schema". Written 2026-10-08.

How to read this file:

- **Checked** means I fetched the primary source (official docs, OpenAPI spec, source code, schema repo) during this session. Raw copies of most of them are under `/tmp/health-research/raw/`.
- **Recalled** means it comes from my own knowledge and I did not re-fetch it today. Treat it as likely but **unverified**.
- **Unverified** is used for anything I could not confirm at all.
- Field names are quoted verbatim in backticks.

Contents:

1. Part 1: commercial and open source aggregators
2. Part 1 summary: the common denominator
3. Part 2: platform, standard and open source data models
4. Part 3a: side-by-side table of the five shapes
5. Part 3b: unit systems and unit strings
6. Part 3c: design lessons and published mistakes
7. Sources

---

## Part 1: commercial and open source aggregators

### 1.1 Terra (tryterra.co)

**Checked**: `https://docs.tryterra.co/reference/health-and-fitness-api/data-models.md`, the OpenAPI bundle `github.com/tryterra/openapi/dist/core/v2-bundled.yaml`, `https://docs.tryterra.co/llms-full.txt` (pricing, events, Apple Health SDK, lab reports).

**Sources.** "500+ wearables and apps, including Apple Health and Samsung Health through the mobile SDKs" (cloud OAuth providers such as Garmin, Fitbit, Oura, WHOOP, Polar, Withings, Dexcom, plus on-device SDKs for HealthKit, Health Connect, Samsung Health). Also a BLE/ANT+ Streaming API, a Lab Reports API (PDF to biomarkers) and Vantage (test kit ordering, FHIR results).

**Unified model.** Document-shaped, one payload per "data type" and period. Models: `Activity`, `Body`, `Daily`, `Sleep`, `Nutrition`, `Menstruation`, `Hormone`, `Athlete`, `PlannedWorkout`. Every model is a tree of `*_data` groups, each holding summary scalars plus `*_samples` arrays. Every field is nullable unless stated. Version string `2022_03_16`; "more fields may be added down the line (but no field name should ever change within a given version)".

- `metadata`: `start_time`, `end_time` (ISO 8601 with offset, required), `summary_id` (Activity and Sleep), `upload_type` (enum `UploadType`: Unknown 0, Automatic 1, Manual 2, Update 3, Delete 4, Pending 5, Third party upload 6), `timestamp_localization` (0 = UTC, 1 = LOCAL), `type` (`ActivityType` for Activity), `is_nap` (Sleep), `name`, `city`, `state`, `country`.
- `device_data`: `name`, `manufacturer`, `serial_number`, `hardware_version`, `software_version`, `activation_timestamp`, `last_upload_date`, `data_provided` (`DeviceDataType[]`), `other_devices[]`, `sensor_state` (Body).
- `Activity`: `active_durations_data`, `calories_data` (`BMR_calories`, `calorie_samples`, `net_activity_calories`, `total_burned_calories`...), `distance_data` (`detailed.{distance_samples, elevation_samples, floors_climbed_samples, step_samples}`, `summary.{distance_meters, steps, floors_climbed, elevation{...}, swimming{...}}`), `energy_data.energy_kilojoules`, `heart_rate_data` (`detailed.{hr_samples, hrv_samples_rmssd, hrv_samples_sdnn}`, `summary.{avg_hr_bpm, max_hr_bpm, min_hr_bpm, resting_hr_bpm, avg_hrv_rmssd, avg_hrv_sdnn, hr_zone_data[], user_max_hr_bpm}`), `lap_data.laps[]`, `MET_data`, `movement_data` (`avg_speed_meters_per_second`, `avg_cadence_rpm`, `avg_pace_minutes_per_kilometer`, `speed_samples`, `cadence_samples`, `torque_samples`...), `oxygen_data` (`saturation_samples`, `vo2max_ml_per_min_per_kg`...), `polyline_map_data.summary_polyline`, `position_data` (`position_samples`, `start_pos_lat_lng_deg`...), `power_data`, `strain_data`, `strength_data.exercises[]`, `TSS_data`, `work_data.work_kilojoules`, `cheat_detection`, `data_enrichment.stress_score`.
- `Body`: `blood_pressure_data.blood_pressure_samples[]`, `heart_data` (`ecg_signal[]`, `afib_classification_samples`, `rr_interval_samples`, `pulse_wave_velocity_samples`, nested `heart_rate_data`), `hydration_data`, `ketone_data`, `measurements_data.measurements[]` plus `day_avg_weight_kg`, `day_avg_bodyfat_percentage`, `day_avg_height_cm`, `day_avg_bone_mass_g`..., `oxygen_data`, `temperature_data` (`body_temperature_samples`, `skin_temperature_samples`, `ambient_temperature_samples`, `day_avg_*_celsius`), `glucose_data` (`blood_glucose_samples`, `detailed_blood_glucose_samples`, `day_avg_blood_glucose_mg_per_dL`, `time_in_range`, `gmi`, `sensor_usage`).
- `Daily`: like Activity minus session fields, plus `scores.{activity, recovery, sleep}`, `stress_data` (`body_battery_samples`, `samples`, `*_stress_duration_seconds`), `data_enrichment` (readiness, cardiovascular, immune, respiratory scores with `*_contributors[]`), `tag_data.tags[]`.
- `Sleep`: `sleep_durations_data` (`asleep.duration_deep_sleep_state_seconds`, `duration_REM_sleep_state_seconds`..., `awake.sleep_latency_seconds`..., `other.duration_in_bed_seconds`, `hypnogram_samples[]`, `sleep_efficiency`), `respiration_data` (`breaths_data`, `oxygen_saturation_data`, `snoring_data`, each with its own `start_time`/`end_time`/`samples`), `heart_rate_data`, `readiness_data`, `temperature_data.delta`, `scores.sleep`.
- `Nutrition`: `meals[]` (`id`, `name`, `timestamp`, `type`, `quantity`, `macros`, `micros`), `drink_samples[]` (`drink_volume`, `drink_unit`, `drink_name`), `summary.macros` (`calories`, `protein_g`, `carbohydrates_g`, `fat_g`, `sodium_mg`...), `summary.micros` (about 50 fields, `*_mg` or `*_g`), `water_ml`.
- `Menstruation`: `menstruation_data` (`menstruation_flow[]`, `intervals[]`, `period_length_days`, `cycle_length_days`, `current_phase`, `ovulation_day`, `is_predicted_cycle`...).
- `Hormone` (no metadata wrapper): `timestamp`, `lh_mIU_per_ml`, `e3g_ng_per_ml`, `pdg_ug_per_ml`, `hcg_mIU_per_ml`, `fsh_mIU_per_ml`.

Sample shapes (from the OpenAPI bundle): every sample is `{timestamp, <value_field_with_unit_in_name>, ...}`:

| Sample | Fields |
| --- | --- |
| `HeartRateDataSample` | `timestamp`, `bpm`, `timer_duration_seconds`, `context` |
| `StepSample` | `timestamp`, `steps` ("Cumulative number of steps taken up to associated timestamp, since the start"), `timer_duration_seconds` |
| `DistanceSample` | `timestamp`, `distance_meters` (cumulative since start), `timer_duration_seconds` |
| `CalorieSample` | `timestamp`, `calories`, `timer_duration_seconds` |
| `GlucoseDataSample` | `timestamp`, `blood_glucose_mg_per_dL`, `glucose_level_flag`, `trend_arrow` |
| `BloodPressureSample` | `timestamp`, `systolic_bp`, `diastolic_bp` (mmHg) |
| `OxygenSaturationSample` | `timestamp`, `percentage`, `type` (blood vs muscle) |
| `TemperatureSample` | `timestamp`, `temperature_celsius` |
| `SleepHypnogramSample` | `timestamp`, `level` (`SleepLevel`: Unknown 0, Awake 1, Sleeping 2, Out of Bed 3, Light 4, Deep 5, REM 6) |
| `MeasurementDataSample` | `measurement_time`, `BMI`, `BMR`, `weight_kg`, `height_cm`, `bodyfat_percentage`, `bone_mass_g`, `muscle_mass_g`, `lean_mass_g`, `water_percentage`, `insulin_units`, `insulin_type`, `urine_color`, `user_notes`, `visceral_fat_level`... (a wide row, not one quantity) |
| `LapSample` | `start_time`, `end_time`, `distance_meters`, `calories`, `avg_hr_bpm`, `avg_speed_meters_per_second`, `total_strokes`, `stroke_type`, `avg_watts`, `active_duration_seconds`, `trigger_method` |
| `ECGReading` | `start_timestamp`, `avg_hr_bpm`, `afib_classification`, `raw_signal[]` |

Notes on the sample design: step and distance samples are **cumulative counters since the start of the activity**, not per-interval deltas; heart rate is instantaneous; hypnogram samples are level changes (stage holds until the next sample) rather than intervals.

**Units policy.** "All data is normalised to the standard metric/SI units defined in Terra's data models, regardless of the end user's device or provider unit settings" (help-center page `units-metric-only-no-imperial`). The unit lives **in the field name** (`_kg`, `_meters`, `_seconds`, `_bpm`, `_celsius`, `_mg_per_dL`, `_ml_per_min_per_kg`, `_kilojoules`), not in a separate field. Glucose is mg/dL. Energy is both kcal (`calories`) and kJ (`energy_kilojoules`).

**Delivery.** Event-based push to a "destination": webhook, SQL database (Postgres/MySQL), Supabase, S3/GCS/Azure, SQS/Kafka, MongoDB, Firestore. Webhook body: `{"type": "activity", "data": [ ... ], "user": {...}}`. Pull via REST for historical ranges, optionally "send to destination" asynchronously. Mobile SDKs for HealthKit, Health Connect, Samsung Health.

**Identity and updates.** Activity and Sleep: `metadata.summary_id`. Body, Daily, Nutrition: the pair `metadata.start_time` + `metadata.end_time`. Menstruation: `metadata.start_time`. Hormone: top-level `timestamp`. "Whenever you receive an event ... which constitutes a duplicate, based on the data type's unique identifier ... the received data will always be a superset of any previous data received. This means you should always overwrite." So: whole-document replace keyed by summary id or period. The Daily payload "can come in multiple times in one day, with updated `steps` field every time". In the SQL destination, `terra_data_payloads` is "one row per data event ... Same-day data for the same user and data type is deduplicated (last write wins)".

**Overlap and priority.** Terra does **not** merge across providers; each connection is a separate Terra user. Its iOS docs warn that "If a user has both a cloud-based Terra connection (e.g. WHOOP, Garmin) and the same provider's companion app syncing into Apple Health, you'll receive duplicate data", and the fix is client-side: `Terra.setIgnoredSources(["com.whoop.app", ...])` by bundle id.

**Timezones.** Timestamps carry the offset (`2024-10-03T18:17:45.000000+08:00`); `timestamp_localization` says whether the time is UTC or local. The local-day boundary is the provider's (Daily `start_time`/`end_time`).

**Lab results (relevant to biomarkers).** The Lab Reports result object has four layers: `source` (verbatim: `name`, `value` as printed, `units` as printed, `flag`, `reference_text`), `biomarker` (normalised identity: `key` slug, `display_name`, `loinc_code`, `specimen`), `measurement` (one typed value: `type` in `numeric | bounded | qualitative | text | absent`, with `numeric`, `bounded {operator: lt|gt, value}`, `qualitative {text}`, `text`, `absent_reason`, plus `units` and `ucum_code`), and `interpretation` (`flag`: high, low, normal, critical_high...). Webhook events carry a stable `event_id` for dedup; reprocessing emits a new `event_id`.

**Pricing.** Credits. Subscription includes 100,000 credits per month; then $0.005 per credit up to 1M, $0.003 beyond. An active authentication costs 200 credits per month (so about $1 per connection per month past the free allowance); the first 400 events per authentication are free, then 0.5 credits per event. Lab Reports: first 3,500 biomarkers included, then 30 credits per biomarker.

### 1.2 Junction (formerly Vital, tryvital.io)

**Checked**: `https://api.tryvital.io/openapi.json` (version 0.4.898), docs pages `wearables/providers/resources`, `data-primary-key`, `timestamps-and-time-zones`, `data-attributions`, `historical-data-pull-range`, `data-ingestion-bounds`, `sense/*` (query DSL), `changelog/wearables/deprecations`.

**Sources.** "300+ wearables" via cloud providers (`fitbit`, `garmin`, `oura`, `whoop_v2`, `withings`, `polar`, `strava`, `peloton`, `wahoo`, `zwift`, `eight_sleep`, `dexcom_v3`, `abbott_libreview`, `freestyle_libre`, `omron`, `kardia`, `hammerhead`, `google_fit`, Google Health API...) and SDK providers (`apple_health_kit`, `health_connect`, BLE devices `omron_ble`, `contour_ble`, `accuchek_ble`, `freestyle_libre_ble`). Also lab testing.

**Unified model.** Two categories, explicitly independent:

- **Summary**: "a summarization of a specific calendar day or a session". Daily: `activity`. Session: `sleep`, `sleep_cycle`, `workouts`, `body`, `meal`, `menstrual_cycle`, `electrocardiogram`, `workout_stream`. Single: `profile`.
- **Timeseries**: "a time-ordered collection of data samples", split into **discrete** ("each value is associated with a single point in time": `respiratory_rate`, `blood_oxygen`, `blood_pressure`, `glucose`, `heartrate`, `hrv`, `electrocardiogram_voltage`, `stress_level`, `fat`, `weight`) and **interval** ("each value is associated with a half-open time interval (`[start, end)`)": `calories_active`, `calories_basal`, `distance`, `floors_climbed`, `steps`, `workout_duration`, `fall`, `stand_hour`, `body_temperature`, `insulin_injection`, `vo2_max`, `carbohydrates`, `caffeine`, `water`, `mindfulness_minutes`, `daylight_exposure`, `note`, `workout_distance`...).

"There is no strict relation between a field in a Summary type and a Timeseries type" and "Junction does not require a summary total to equal an aggregate calculated from timeseries samples."

Discrete sample (`ClientFacingHeartRateTimeseries`): `timestamp`, `value`, `unit` (`bpm`), `timezone_offset` (seconds, nullable), `type` ("always null for this resource"; used for e.g. ECG lead, cholesterol reading type), `id` (deprecated). Interval sample (`ClientFacingStepsTimeseries`): `start` (inclusive), `end` (exclusive), `value`, `unit` (const `count`), `timezone_offset`, `type`, `timestamp` (deprecated alias of `start`). Multi-value samples add fields: blood pressure has `systolic`, `diastolic`, `grouping`; insulin adds `delivery_mode`, `delivery_form`, `bolus_purpose`; notes add `tags`.

Grouped response (`/v2/timeseries/{user_id}/{resource}/grouped`): `{groups: {<provider>: [ {source: ClientFacingSource, data: [samples]} ]}, next_cursor}`. `ClientFacingSource`: `provider` (slug, required), `type` (source type, default `unknown`), `app_id` (HealthKit bundle id or Health Connect package), `device_id` (uuid or null), `sport`, `workout_id` (only for `workout_*` streams).

Summaries: `ClientFacingSleep` (`id`, `calendar_date`, `bedtime_start`, `bedtime_stop`, `type` (`long_sleep`, `short_sleep`, `acknowledged_nap`, `unknown`), `timezone_offset`, `duration`, `stage_asleep_second`, `stage_light_second`, `stage_rem_second`, `stage_deep_second`, `stage_awake_second`, `efficiency`, `latency`, `hr_lowest`, `hr_average`, `average_hrv`, `respiratory_rate`, `temperature_delta`, `score`, `state` (e.g. TENTATIVE then final), `source`, `created_at`, `updated_at`). `ClientFacingActivity` (`calendar_date`, `calories_total`, `calories_active`, `steps`, `distance`, `low`/`medium`/`high` (minutes), `floors_climbed`, `time_zone` (IANA), `timezone_offset`, `heart_rate` summary). `ClientFacingWorkout`, `ClientFacingBody` similar. `sleep_cycle` is **columnar**: `session_start`, `session_end`, `stage_start_offset_second[]`, `stage_end_offset_second[]`, `stage_type[]` (`VitalSleepStage`: -1 unknown, 1 deep, 2 light, 3 REM, 4 awake, 5 manual), plus `source_provider`, `source_type`, `source_app_id`, `source_device_id`.

**Units.** One fixed unit per resource, echoed in a `unit` field on every sample: `bpm`, `count`, `kcal`, `m`, `°C` (with a non-ASCII degree sign), `kg`, `cm`, `L`, `L/min`, `min`, `%`, `index`, `unit` (insulin), `mmol/L` for glucose and cholesterol, `mV` for ECG, `rmssd` (as a unit string for HRV). Summary fields put the unit in the description suffix (`::seconds`, `::minutes`) and, more recently, in the field name (`stage_light_second` replacing `light`, `lean_body_mass_kilogram`, `waist_circumference_centimeter`).

**Delivery.** Webhooks (`daily.data.<resource>.created|updated`, `historical.data.<resource>.created`), REST pull, ETL pipelines, mobile SDKs. Junction Sense adds a query DSL and Continuous Queries that push `continuous_query.result_table.changed`.

**Identity and updates.** "Each summary is uniquely identified by its ID (`$.id`). Given the same ID, the latest version of a summary you received **replaces** all its previous versions." "Each timeseries sample is uniquely identified by a compound key of: the timeseries resource type; the source provider; the Source Type; and the sample timestamp. Given the same compound key, the latest value ... replaces all the previous values."

**Overlap and priority.** Junction Sense applies prioritisation only inside a GROUP BY: it sub-groups by provider and source type and "keep[s] only data from the sub-group that has the highest Source Type priority, or the highest Provider priority". "The selected provider and source-type pair contributes the complete aggregate output for that group. Sense does not combine individual fields from different source pairs." Source type order is fixed: `lab`, `automatic`, `watch`, `ring`, `chest_strap`, `scale`, `cuff`, `fingerprick`, `manual_scan`, `phone`, `app`, `multiple_sources`, `unknown`. Provider order is configurable per team and per summary type; a query can pass `config.provider_priority_overrides`. Grouping by both `source_provider` and `source_type` disables prioritisation. Within one stored series, "values at different timestamps remain separate, even when their intervals overlap. Aggregates such as `sum` operate on the values that remain, so overlapping intervals can both contribute."

**Timezones.** All timestamps are UTC `+00:00` with a nullable `timezone_offset` in seconds. Exception: Abbott LibreView and Freestyle Libre are **floating time**: the `+00:00` "should be **ignored**" and the components reinterpreted literally as local time, because OpenAPI 3 forces an offset. Each provider has a documented "time zone affinity" (with tz, user fallback or UTC, user fallback or absent, inferred or absent, absent, floating). Users can have a `fallback_time_zone` (`{id: "Europe/London", source_slug: "manual", updated_at}`). Sense evaluates everything in floating local time: "A 7:00 AM sample remains 7:00 AM when grouped by hour, even when the user changes time zones"; "a local-day bucket does not represent a fixed 24-hour elapsed interval"; sleep is indexed by **session end**, workouts by session start, activity by calendar date.

**Aggregation API (Sense).** JSON DSL: `select` with `{func: min|max|mean|median|stddev|count|oldest|newest|sum, arg: {<table>: <column>}}`, `group_by` with `date_trunc {value, unit}`, `date_part`, table columns and source columns, `where`, an `align` clause to fill empty buckets (carry forward, backward, nearest), list-column subqueries (`unnest`), and "value macros" (sleep analysis, chronotype, readiness). "`mean` ... of precomputed session or daily metrics is not weighted by duration or sample count."

**Backfill.** On connect, `[now - days_to_pull, now]`, provider defaults from 14 days (Strava) to 270 days (Zwift); Health Connect fixed at 30 days because Google restricts reads to 30 days before the first permission grant; configurable maximum 365 days. Per-user `ingestion_start`/`ingestion_end` bounds drop out-of-range data (±1 day because dates are floating).

**Deprecations worth reading.** The `hypnogram` timeseries was replaced by the `sleep_cycle` summary; non-grouped timeseries endpoints (no source attribution, no cursor) are removed after 2026-09-14; `source.{name, slug, logo}` (provider metadata) was split from the per-datum `source.{provider, type, app_id}`; summary `date` (a datetime at UTC midnight) was deprecated in favour of `calendar_date` (a date).

**Pricing.** Third-party reports: $0.50 per connected user per month with a $300 monthly minimum (Launch plan, 1 to 500 users). From `themomentum.ai` and `openwearables.io` comparison posts; **unverified** against Junction's own page.

### 1.3 Spike API (spikeapi.com)

**Checked**: `https://docs.spikeapi.com/llms.txt`, `technical-references/metrics_matrix.md`, `statistics_matrix.md`, `api-reference/query-timeseries.md`, `api-docs/query_options_comparison.md`, `api-reference/openapi.json`.

**Sources.** Providers listed in the metrics matrix: `apple`, `health_connect`, `samsung_health_data`, `fitbit`, `garmin`, `oura`, `polar`, `strava`, `suunto`, `whoop`, `withings`, `huawei`, `ultrahuman`, `coros`, `luna`, `omron_us`, `omron_eu`, `dexcom`, `freestyle_libre`, `google_health`. Plus nutrition-from-photo and lab report extraction.

**Unified model.** A **record plus metric dictionary**. A `QueryOutputRecord` has `record_id`, `provider_slug`, `provider_source`, `provider_source_type`, `input_method`, `activity_type`, `activity_tags`, `start_at_timestamp`, `end_at_timestamp`, `duration`, `provider_local_date`, `user_time_offset_minutes`, `is_source_aggregated`, `modified_at`, `metrics` (a map metric name to value), `provider_specific_metrics`, `samples`, `source` (`device_manufacturer`, `device_model`, `device_product`, `device_version`, `source_bundle_identifier`, `source_os_version`...). The raw `ProviderRecord` also has `timezone_ambiguous` and `expire_at`. Samples are **columnar**: `{offsets[], durations[], values[]}` relative to the record start, in milliseconds.

There are about 80 flat metric names (`heartrate`, `heartrate_max`, `steps`, `distance_walking`, `sleep_duration_rem`, `spo2`, `glucose`, `weight`, `vo2max`, `blood_pressure_systolic`...), each with one unit and an output precision (decimals).

**Query surfaces.** Five: Statistics (interval and daily), Sleep, Workouts, Time Series, Provider Records ("recommended primarily for debugging").

- Time Series response: `{metric, start_at, offsets[], durations[], values[], providers[]}` with a `merge_method`. Source selection methods: `select_best_weighted_source`, `select_highest_coverage_source`, `select_most_granular_source`, `select_highest_priority_source_type` ("Sample > Intraday > Summary > Activity priority, then weighted coverage"). Coverage methods: `merge_maximize_coverage` ("Weighted interval scheduling to maximize total time coverage"), `merge_maximize_granularity`, `merge_maximize_weighted_coverage`. Sample merging: `merge_all_samples` ("conflicts resolved by source with most total entries").
- Statistics: each statistic declares its method: `intersect_sum` (steps, active calories, distances), `intersect_union_sum` (`calories_burned_total` from `calories_burned`, `calories_burned_basal`, `calories_burned_active`; `distance_total`; `sleep_duration_total`), `duration_weighted_avg` (heart rate, HRV, resting HR), `min`, `max`, `sleep_score`. A statistic row returns `statistic_value`, `statistic_unit`, `interval_start`, `interval_end`, `providers`, `provider_sources`, `record_ids` (lineage), `row_count`, `source_coverage`.

**Units.** One canonical unit per metric, but not SI-pure: durations in `ms`, weight in `g`, height in `meters`, glucose `mg/dL`, energy `kcal`, speed `m/sec`, pace `sec/m`, temperature `celsius`, ECG `uV`, BMI `count`, percentages `percentage`, lat/long `degrees`.

**Delivery.** Webhooks after each provider update, REST queries, mobile SDKs with background delivery and backfill, an MCP server.

**Timezones.** `user_time_offset_minutes` on records, `provider_local_date`, a `timezone_ambiguous` flag.

**Pricing.** $450 per month Sandbox plan; Spike was reportedly acquired by Raintree Systems in July 2026 (both from a Sahha comparison page, **unverified**).

### 1.4 ROOK (tryrook.io)

**Checked**: `https://docs.tryrook.io/llms.txt`, the data-sources page for Garmin (field names extracted from the page), `docs/rookconnect/data-types/` and `docs/rookconnect/data_processing/` (read through a summariser, so treat wording as paraphrase).

**Sources.** "400+ wearables and health data sources": API-based (Garmin, WHOOP, Oura, Fitbit, Polar, Withings, Dexcom, Strava) and SDK-based (Apple Health, Health Connect, Samsung Health).

**Unified model.** Three "pillars": Physical, Sleep, Body. Two shapes per pillar: **summaries** (daily) and **events** (sessions and measurements). Field names carry both the unit and the JSON type as suffixes. From the Garmin data-source page: `steps_int`, `hr_avg_bpm_int`, `hr_maximum_bpm_int`, `hr_resting_bpm_int`, `hrv_avg_rmssd_float`, `traveled_distance_meters_float`, `calories_expenditure_kcal_float`, `calories_basal_metabolic_rate_kcal_float`, `speed_avg_meters_per_second_float`, `pace_avg_min_per_km_float`, `saturation_avg_percentage_int`, `weight_kg_float`, `bmi_float`, `sleep_duration_seconds_int`, `deep_sleep_duration_seconds_int`, `rem_sleep_duration_seconds_int`, `time_in_bed_seconds_int`, `sleep_efficiency_1_100_score_int`, `sleep_quality_rating_1_5_score_int`, `activity_start_datetime_string`, `activity_end_datetime_string`, `sleep_date_string`, `period_start_date_string`, `is_a_predicted_cycle_bool`, and granular arrays `hr_granular_data_array`, `steps_granular_data_array`, `hrv_rmssd_granular_data_array`, `saturation_granular_data_array`, `position_granular_data_array`, `elevation_granular_data_array`. Objects: `blood_pressure_avg_object`, `velocity_avg_object`, `position_start_object`.

**Processing (paraphrased from their docs).** Harmonisation (distance to a common unit, timestamps aligned), standardisation (sleep stages mapped to a common standard), cleaning and dedup, normalisation (calories to kcal), structuring. Dedup rules: wearables rank above SDK extractions and health kits; a "Non-Null Value Rule" lets a lower-priority source fill a null from a higher-priority one (field-level merge); a "Higher Value Rule" keeps the maximum for steps and calories; events within ±10 minutes are merged or prioritised by source rank. Updates use a `document_version`: "only higher versions matching the same `datetime` replace existing datasets". Timestamps are UTC ISO 8601 with six fractional digits.

**Pricing.** Core $399/month up to 750 active users; Core+ $999/month up to 5,000; Business $1,999/month up to 15,000 (from ROOK's own `llms.txt`).

### 1.5 Thryve (thryve.health)

**Checked**: `docs.thryve.health` pages `access/data-model`, `data-types-biomarker/data-type-structure`, `understand/data-annotations`, `access-data-via-wearable-api/get-epoch-data`.

**Sources.** "500+ wearable devices and health apps"; cloud (Fitbit, Garmin, Polar, Withings, Strava...) and on-device (Apple Health, Health Connect, Samsung Health), direct Bluetooth, camera-based vitals.

**Unified model.** The most normalised of the commercial models: an **entity-attribute-value** stream with an integer `dataTypeId` ("250+ data types"). Each data type declares a name, description, **value type** (`LONG`, `DOUBLE`, `BOOLEAN`, `STRING`, `JSON`, `DATE`, Binary, `FLOAT`), a unit, and whether it exists as Daily, Intraday, or both. Two temporal frames:

- **Epoch data** (`DynamicEpochValues`): `{startTimestamp, endTimestamp, timezoneOffset, dataTypeId, value, additionalDetails}`. "The end timestamp will be `null`, if the epoch entry indicates an instant measurement." "Thryve always stores data at the highest possible granularity and does not pre-aggregate epoch data." Timestamps in ISO 8601 or Unix ms (client choice); `timezoneOffset` in minutes.
- **Daily data**: `{day, timezoneOffset, dataTypeId, value, additionalDetails}` where `day` is "a calendar date in the user's local timezone". "For users traveling across timezones within a single day, `timezoneOffset` reflects the timezone where more data was recorded."

**Annotations** (`additionalDetails`): `generation` (`manual_entry`, `manual_measurement`, `automated_measurement`, `smartphone`...), `medicalGrade` (bool), `trustworthiness` (`unfavorable_measurement_context`, `doubt_from_device_source`, `doubt_from_user`, `verified_from_device_source`, `verified_from_user`), `chronologicalExactness` (minutes; on a daily value it is the deviation from 24 h when the user crossed timezones).

**Overlap.** "Smart Overlap Detection ... prevents value inflation through smart overlap detection within the same data type and data source" when Thryve computes the daily value itself; it does not apply when the source supplies the daily value. Queries return epochs that overlap the requested window; maximum query range 30 days.

### 1.6 Validic (Inform API)

**Checked**: `developer.validic.com/docs/working-with-data.md`, `reference/intraday-2.md`, `docs/standardization.md`.

**Unified model.** Seven resources: `summaries` ("Aggregated Daily Activity (always 1 per source per day)"), `intraday` ("Time Series Activity"), `cgm` ("always 1 per source per day"), `measurements`, `workouts`, `nutrition`, `sleep`. Every record has `id`, `checksum`, `version`, `log_id` (the provider's id or a date like `2017-03-24`), `type`, `category`, `start_time`, `end_time`, `utc_offset` (seconds), `offset_origin` (`source`, `profile`, `user_defined`), `created_at`, `deleted_at`, `source {type, device}`, `user {...}`, `segments[]`, and a **`metrics[]` array of `{type, unit, value, origin}`**. `origin` is `device`, `manual` or `unknown`. "If a metric is not reported by a data source ... the metric will not appear in the record. Although Validic will not send NULL for metrics." Intraday records add `granularity {interval, unit}` and per-metric `data_points[{time, value}]`.

**Units.** Every metric type has one standard unit (`s`, `kcal`, `m`, `count`): "Validic consumes data from these sources and reports them as a metric named `energy_consumed` using a standard unit of `kcal`." Version 2 dropped "expanded" pass-through data: "all source data is reviewed and standardized" and new metrics are added on request.

**Delivery.** REST and a Server-Sent Events streaming API; mobile SDKs (Inform SDK) for HealthKit and BLE devices.

### 1.7 Human API

**Checked by search only.** Acquired by LexisNexis Risk Solutions on 2023-04-24 and now sold as LexisNexis Health Intelligence, focused on medical records for insurers. The wellness API (it once listed 684 wellness sources) is not marketed any more. Old docs at `reference.humanapi.co/v2.x` still resolve. Status of the wellness endpoints: **unverified**, treat as dead.

### 1.8 Metriport (open source core)

**Checked**: `github.com/metriport/metriport` `packages/api-sdk/src/devices/models/*`.

Metriport has pivoted to medical records over health information exchanges (CommonWell, Carequality, FHIR R4 output); third-party write-ups say it "exited wearables". The Devices API code remains in the repo. Its model is a Terra-like set of day documents extending `MetriportData` with `metadata: {date, hour?, source: ProviderSource, data_source?: SourceInfo, error?}`: `Activity` (`summary.{durations, energy_expenditure, movement, biometrics}`, `activity_logs[]`), `Biometrics` (`blood_glucose`, `blood_pressure`, `heart_rate`, `hrv`, `perfusion_index_pct`, `respiration`, `temperature`), `Body`, `Nutrition`, `Sleep` (`start_time`, `end_time`, `durations.{total_seconds, awake_seconds, deep_seconds, rem_seconds, light_seconds, in_bed_seconds, time_to_fall_asleep_seconds, no_data_seconds}`, `biometrics`), `User`. The shared sample is `Sample {time, value, std_dev?, data_source?}`; `SourceInfo {source_type?: MANUAL_ENTRY | DEVICE, id?, name?, type?}`. Units in field names: `HeartRate {min_bpm, max_bpm, avg_bpm, resting_bpm, samples_bpm: Sample[]}`, `BloodGlucose {avg_mg_dL, samples_mg_dL}`.

### 1.9 Open Wearables (openwearables.io, the-momentum/open-wearables, MIT)

**Checked**: docs `architecture/unified-data-model.md`, `architecture/data-types.md`, `developer-portal/settings/priorities.md`, `data-lifecycle.md`; GitHub issues (see Part 3c).

Self-hosted FastAPI + PostgreSQL + Redis + Celery. Providers: Garmin, WHOOP, Polar, Suunto, Oura, Ultrahuman, Strava, Fitbit/Google Health API, Withings, Apple Health (SDK and XML import), Health Connect, Samsung Health.

**Data model** (the docs warn it "is still under active development and is subject to change"):

- `EventRecord` (`id`, `external_device_mapping_id`, `external_id`, `category`, `type`, `source_name`, `duration_seconds`, `start_datetime`, `end_datetime`) with a polymorphic `EventRecordDetail`: `SleepDetails` (`sleep_total_duration_minutes`, `sleep_deep_minutes`, `sleep_rem_minutes`, `sleep_light_minutes`, `sleep_awake_minutes`, `sleep_efficiency_score`, `is_nap`...), `WorkoutDetails` (`heart_rate_min/max/avg`, `energy_burned`, `distance`, `steps_count`, `max_speed`, `moving_time_seconds`, `total_elevation_gain`...).
- `DataPointSeries` (`id`, `external_device_mapping_id`, `series_type_definition_id`, `external_id`, `recorded_at`, `value` decimal). One narrow table for every time series. "Observation values are stored as float values ... handling categorical data as numerical encoding." The docs call this "a current concession" and plan to split series by data type.
- `SeriesTypeDefinition` (`id`, `code`, `unit`): the unit lives on the series type, not on the row.
- `ExternalDeviceMapping` (`user_id`, `provider_name`, `device_id`): one small id replaces (user, provider, device) on every data row, to keep rows narrow.
- `PersonalRecord` for slow-changing traits.

Series types (about 90): `heart_rate` bpm, `heart_rate_variability_rmssd` ms, `blood_glucose` mg/dL, `oxygen_saturation` %, `weight` kg, `height` cm, `body_temperature` °C, `steps` count, `active_energy` kcal, `distance_walking_running` meters, `walking_step_length` cm, `running_ground_contact_time` ms, `garmin_body_battery` %... Timeseries `resolution`: `raw`, `1min`, `5min`, `15min`, `1hour`; "Buckets are grouped per data source, so two devices reporting the same minute stay two points." Summaries: activity, sleep, body, recovery; "totals" endpoints.

**Priority.** Two ordered lists: providers, then device types within a provider (Watch, Band, Ring, Phone, Scale, Other, Unknown). "When data from multiple providers overlaps in time, the higher-priority provider's data is shown."

**Retention.** Optional archival of raw samples older than N days into daily aggregates ("roughly 1/500 of the raw rate") and optional deletion; early access.

### 1.10 HealthKit-to-server exporters: Health Auto Export

**Checked** (through a summariser): `help.healthyapps.dev/en/health-auto-export/export-format/` and `.../health-metrics/`.

iOS app that posts HealthKit data as JSON or CSV to a REST endpoint on a schedule. Top-level `data` object with `metrics`, `workouts`, `stateOfMind`, `medications`, `symptoms`, `cycleTracking`, `ecg`, `heartRateNotifications`. A metric is `{name (snake_case), units, data: [{date, qty, source?}]}` with dates as `yyyy-MM-dd HH:mm:ss Z`. Special shapes: heart rate `{Min, Avg, Max}` instead of `qty`; blood pressure `{systolic, diastolic}`; blood glucose adds `mealTime`; sleep has an aggregated daily shape (`totalSleep`, `asleep`, `core`, `deep`, `rem`, `sleepStart`, `sleepEnd`, `inBed`, `inBedStart`, `inBedEnd`) and an unaggregated per-segment shape (`startDate`, `endDate`, `qty`, `value`); insulin adds `reason`. A user-chosen "time grouping" (seconds to days) pre-aggregates before export. Workouts (v2) have `id`, `name`, `start`, `end`, `duration` and optional `{qty, units}` objects; GitHub issue Lybron/health-auto-export#65 records that workouts lack the HealthKit source. Units follow the user's Health app unit preferences (recalled, **unverified**), so a consumer must read `units`.

### 1.11 Sahha (sahha.ai)

**Checked**: `docs.sahha.ai/llms.txt`, biomarkers page (summariser).

Phone-first (HealthKit, Health Connect) analytics vendor. Outputs are derived: scores (0 to 1 with `factors[{name, value, goal, unit, score}]`), archetypes, insights, and "biomarkers": `{id, type, category, value, valueType (long|double|string|datetime), unit, aggregation (total|average|minimum|maximum|none), periodicity (daily|weekly|monthly|none), startDateTime, endDateTime, createdAtUtc}`. Raw "Data Logs" are available for research. Interesting because it makes **aggregation and periodicity explicit fields** on a derived value.

### 1.12 Fitrockr, Wearable Connect, tryvital

- **Fitrockr**: Garmin-centred research platform (Garmin Health API, high-resolution raw data, surveys). Daily summary has user, date, calories, steps, distance, activity minutes, points. Checked only at search-result level.
- **tryvital** is the old name of Junction (1.2).
- **Wearable Connect**: I could not identify a distinct vendor with public docs under this name. **Unverified**; skipped.

### 1.13 Google Health API (replacement for the Fitbit Web API)

**Checked**: `developers.google.com/health/reference/rest/v4/users.dataTypes.dataPoints` (summariser) and Terra's write-up.

The Fitbit Web API is turned off on 2026-10-30 (support ended 2026-09-30); integrators must move to the Google Health API, and tokens do not carry over. The new API is the cleanest public statement of the sample/interval/daily/session split:

- One resource, `users.dataTypes.dataPoints`, with methods `list`, `get`, `create`, `patch`, `batchDelete`, `reconcile` ("Reconcile data points from multiple data sources into a single data stream"), `rollUp` ("over physical time intervals"), `dailyRollUp` ("over civil time intervals"), `exportExerciseTcx`.
- A `DataPoint` is `{name, dataSource {recordingMethod, device {formFactor...}, platform, application}, <one of ~40 type fields>}`. Kinds: interval (steps, distance, floors), sample (heart rate, weight, SpO2), daily (daily resting HR, daily HRV), session (sleep, exercise).
- Time is explicit about both clocks: `ObservationSampleTime {physicalTime, utcOffset, civilTime}` and `ObservationTimeInterval {startTime, startUtcOffset, endTime, endUtcOffset, civilStartTime, civilEndTime}`.
- `recordingMethod`: `ACTIVELY_RECORDED`, `AUTOMATICALLY_RECORDED` (plus unspecified). `reconcile` drops per-point `dataSource`.

### 1.14 Fitbit legacy Web API (recalled)

Daily summaries per resource (`activities/steps`, `activities/heart`), intraday at 1 min or 1 s for personal apps only, sleep logs with `levels.data[{dateTime, level, seconds}]` and 30-second `shortData`, all in the user's profile timezone with **no offset in timestamps**. Being retired (see 1.13). **Recalled, unverified today.**

---

## Part 1 summary: the common denominator

Ten vendors designed their models independently. Stripping naming differences, they converge on four shapes.

**1. Sample (instant).** `{time, value, unit, source}`, sometimes `{time, value}` with the unit fixed by the series. Everyone has it. Terra `{timestamp, bpm}`, Junction `{timestamp, value, unit, timezone_offset}`, Validic `{time, value}` inside a metric, Metriport `{time, value}`, Thryve `{startTimestamp, endTimestamp: null, value, dataTypeId}`, OW `{recorded_at, value, series_type}`, Google `heartRate {sampleTime {physicalTime, utcOffset}, beatsPerMinute}`.

**2. Interval quantity.** `{start, end, value, unit, source}`, half-open `[start, end)` where it is specified (Junction says so; IEEE 1752 explicitly refuses to say). Used for anything summable: steps, distance, energy, water, nutrients. Exceptions: Terra uses cumulative counters since session start; OW stores only `recorded_at` (no end) and loses the interval.

**3. Session / event.** `{id, type, start, end, timezone, source, summary scalars, child arrays}`: workout, sleep, meal, menstrual period. Children are either nested sample arrays (Terra, Metriport, ROOK, Health Connect `samples`, `stages`, `segments`, `laps`), separate rows pointing at the session (Junction `workout_id` on `workout_*` streams, OW `EventRecord` + `DataPointSeries`), or columnar arrays (Junction `sleep_cycle`, Spike `offsets/durations/values`). Sessions always carry an upstream id (Terra `summary_id`, Junction `id`, Validic `log_id`, OW `external_id`) and are replaced whole on update.

**4. Daily summary.** `{local_date, timezone_offset or tz, source, metric scalars}`, one per (user, source, local day), replaced whole on update. Everyone keys it by a **local calendar date**, not a UTC instant: Junction `calendar_date`, Thryve `day`, Validic `log_id` = date with `start_time`/`end_time` at local midnight, Spike `provider_local_date`, Terra `start_time`/`end_time` with offset. Every vendor also says the provider's daily total and the sum of its samples will not match and should not be forced to.

Other things all of them have:

- **One canonical unit per metric**, chosen by the vendor, either in the field name (Terra, ROOK, Metriport, Junction summaries) or in a `unit` field that is constant per type (Junction samples, Validic, Spike, OW, Thryve). No commercial vendor lets a sample carry an arbitrary unit through to the consumer. They disagree on which unit: glucose is mg/dL (Terra, Spike, OW, Metriport) or mmol/L (Junction); duration is seconds (Terra, ROOK, Junction), milliseconds (Spike) or minutes (OW sleep); weight is kg or g (Spike).
- **A source tuple** of (provider, source/device type, app id, device id, recording method). Junction and Health Connect have the fullest version.
- **Replace-by-key updates**, never append: summaries by id or period, samples by (type, provider, source type, timestamp).
- **Priority, not merge**, for overlapping sources, with a known failure for additive metrics (see 3c).
- A **sample-rate warning**: Junction documents peak rates from 1 per 3 s (Apple Watch workouts) to 1 per day, and tells users not to assume a fixed cadence.

---

## Part 2: platform, standard and open source data models

### 2.1 Apple HealthKit (on-device)

**Checked**: Apple documentation JSON (`developer.apple.com/tutorials/data/documentation/healthkit/...`) for `HKQuantityTypeIdentifier` (122 identifiers fetched individually), `HKStatisticsOptions`, `HKQuantityAggregationStyle`, `HKStatisticsCollectionQuery`, `HKMetadataKeySyncIdentifier`, `HKMetadataKeySyncVersion`, `HKMetadataKeyTimeZone`, `HKCategoryValueSleepAnalysis`, `HKQuantitySample`, `HKCategorySample`, `HKCorrelation`, `HKWorkout`, `HKSourceRevision`, `HKDevice`. Export DTD from `github.com/AlekSi/applehealth/healthkit11.dtd`.

**Object model.**

- `HKObject` (`uuid`, `sourceRevision`, `device`, `metadata`) → `HKSample` (`sampleType`, `startDate`, `endDate`; an instant sample has start = end).
- `HKQuantitySample` ("A sample that represents a quantity, including the value and the units"): `quantityType`, `quantity` (an `HKQuantity` = double + `HKUnit`), `count` (more than 1 for series samples built with `HKQuantitySeriesSampleBuilder`, read with `HKQuantitySeriesSampleQuery`).
- `HKCategorySample` ("values from a short list of possible values"): `categoryType`, `value` (Int). Sleep uses `HKCategoryValueSleepAnalysis`: `inBed`, `asleepUnspecified`, `awake`, `asleepCore`, `asleepDeep`, `asleepREM` (the deprecated `asleep` equals `asleepUnspecified`). A sleep night is **many category samples**, one per stage interval, with no parent session object.
- `HKCorrelation` groups samples: blood pressure (systolic + diastolic quantity samples), food (nutrient quantity samples).
- `HKWorkout`: `workoutActivityType`, `duration`, `workoutEvents` (pause, resume, lap, segment, marker), `workoutActivities` (multi-sport), `allStatistics` (per quantity type), routes (`HKWorkoutRoute`); child samples are associated with the workout, not nested in it.
- Provenance: `HKSourceRevision {source (name, bundleIdentifier), version, productType, operatingSystemVersion}` and `HKDevice {name, manufacturer, model, hardwareVersion, firmwareVersion, softwareVersion, localIdentifier, udiDeviceIdentifier}`.
- Update semantics: samples are immutable; you delete and re-add. Dedup for writers is `HKMetadataKeySyncIdentifier` + `HKMetadataKeySyncVersion`: "If the new object has a greater sync version, the system replaces the old object with the new one", including inside workouts and correlations.
- Timezone: only as optional metadata, `HKMetadataKeyTimeZone` (an IANA name); Apple recommends it for sleep. Dates are absolute instants. Local day is computed by the reader with a `Calendar` and an anchor date.

**Aggregation.** `HKQuantityAggregationStyle`: `cumulative` ("can be summed over time"), `discreteArithmetic`, `discreteTemporallyWeighted` ("averaged over a time interval using a temporally weighted integration function"), `discreteEquivalentContinuousLevel` (audio exposure). `HKStatisticsOptions`: `cumulativeSum`, `discreteAverage`, `discreteMin`, `discreteMax`, `discreteMostRecent` (alias `mostRecent`), `duration`, `separateBySource`. "You cannot combine a discrete option with a cumulative option." `HKStatisticsQuery` (one range) and `HKStatisticsCollectionQuery` (fixed intervals from an anchor date, "for example ... the total number of steps for each day or the average heart rate for each hour"; can stay open and push updates). Statistics queries merge overlapping samples from multiple sources (Apple DTS answers on the developer forums; the algorithm is private and there is no public API that reproduces it). Raw sample queries do not merge.

**Unit grammar (recalled, unverified today).** `HKUnit(from: "count/min")`. Grammar: base units (`g`, `m`, `L`, `Pa`, `s`, `J`, `K`, `degC`, `degF`, `cal`, `kcal`, `Cal`, `count`, `%`, `mmHg`, `cmAq`, `atm`, `in`, `ft`, `mi`, `yd`, `lb`, `oz`, `st`, `min`, `hr`, `d`, `IU`, `S` (siemens), `dBASPL`, `dBHL`, `Hz`, `V`, `W`, `appleEffortScore`, `lx`), SI prefixes (`m`, `c`, `k`, `u`, `n`, `p`...), `*` and `/` with parentheses, `^` for exponents, and a molar-mass annotation `mmol<180.15588000005408>/L` for glucose. Examples in common use: `count/min`, `mg/dL`, `mmol<180.1558800000541>/L`, `kg`, `mL/(kg*min)`, `degC`, `ms`, `%`, `kcal`.

**HKQuantityTypeIdentifier list** (122 symbols, checked). Aggregation style and dimension per identifier, read from each identifier's discussion text ("These samples use ... units ... measure cumulative/discrete values"). `?` means the page did not say.

| Identifier | Style | Dimension | Since iOS |
| --- | --- | --- | --- |
| activeEnergyBurned | cumulative | energy | 8.0 |
| appleExerciseTime | cumulative | time | 9.3 |
| appleMoveTime | cumulative | time | 14.5 |
| appleSleepingBreathingDisturbances | ? | ? | 18.0 |
| appleSleepingWristTemperature | discrete | temperature | 16.0 |
| appleStandTime | cumulative | time | 13.0 |
| appleWalkingSteadiness | discrete | percentage | 15.0 |
| atrialFibrillationBurden | discrete | percentage | 16.0 |
| basalBodyTemperature | discrete | temperature | 9.0 |
| basalEnergyBurned | cumulative | energy | 8.0 |
| bloodAlcoholContent | discrete | percent | 8.0 |
| bloodGlucose | discrete | mass/volume | 8.0 |
| bloodPressureDiastolic | discrete | pressure | 8.0 |
| bloodPressureSystolic | discrete | pressure | 8.0 |
| bodyFatPercentage | discrete | percent | 8.0 |
| bodyMass | discrete | mass | 8.0 |
| bodyMassIndex | discrete | count | 8.0 |
| bodyTemperature | discrete | temperature | 8.0 |
| crossCountrySkiingSpeed | discrete | distance/time | 18.0 |
| cyclingCadence | discrete | count/min | 17.0 |
| cyclingFunctionalThresholdPower | discrete | power | 17.0 |
| cyclingPower | discrete | power | 17.0 |
| cyclingSpeed | discrete | distance/time | 17.0 |
| dietaryBiotin ... dietaryZinc (39 nutrient identifiers: biotin, caffeine, calcium, carbohydrates, chloride, cholesterol, chromium, copper, energyConsumed, fatMonounsaturated, fatPolyunsaturated, fatSaturated, fatTotal, fiber, folate, iodine, iron, magnesium, manganese, molybdenum, niacin, pantothenicAcid, phosphorus, potassium, protein, riboflavin, selenium, sodium, sugar, thiamin, vitaminA, vitaminB12, vitaminB6, vitaminC, vitaminD, vitaminE, vitaminK, water, zinc) | cumulative | mass (energy for `dietaryEnergyConsumed`, volume for `dietaryWater`) | 8.0 (water 9.0) |
| distanceCrossCountrySkiing, distancePaddleSports, distanceRowing, distanceSkatingSports | cumulative | length | 18.0 |
| distanceCycling | cumulative | length | 8.0 |
| distanceDownhillSnowSports | cumulative | length | 11.2 |
| distanceSwimming | cumulative | length | 10.0 |
| distanceWalkingRunning | cumulative | length | 8.0 |
| distanceWheelchair | cumulative | length | 10.0 |
| electrodermalActivity | discrete | conductance | 8.0 |
| environmentalAudioExposure | discrete | sound pressure | 13.0 |
| environmentalSoundReduction | discrete | sound pressure | 16.0 |
| estimatedWorkoutEffortScore | ? | ? | 18.0 |
| flightsClimbed | cumulative | count | 8.0 |
| forcedExpiratoryVolume1 | discrete | volume | 8.0 |
| forcedVitalCapacity | discrete | volume | 8.0 |
| headphoneAudioExposure | discrete | sound pressure | 13.0 |
| heartRate | discrete | count/time | 8.0 |
| heartRateRecoveryOneMinute | discrete | count | 16.0 |
| heartRateVariabilityRMSSD | ? | ? | 27.0 (new this year per Apple's JSON) |
| heartRateVariabilitySDNN | discrete | time | 11.0 |
| height | discrete | length | 8.0 |
| inhalerUsage | cumulative | count | 8.0 |
| insulinDelivery | cumulative | international units | 11.0 |
| leanBodyMass | discrete | mass | 8.0 |
| nikeFuel | cumulative | count | 8.0 |
| numberOfAlcoholicBeverages | cumulative | count | 15.0 |
| numberOfTimesFallen | cumulative | count | 8.0 |
| oxygenSaturation | discrete | percent | 8.0 |
| paddleSportsSpeed | cumulative (as stated by Apple's page; likely a doc error) | distance/time | 18.0 |
| peakExpiratoryFlowRate | discrete | volume/time | 8.0 |
| peripheralPerfusionIndex | discrete | percent | 8.0 |
| physicalEffort | discrete | kcal/(kg*hr) (METs) | 17.0 |
| pushCount | cumulative | count | 10.0 |
| respiratoryRate | discrete | count/time | 8.0 |
| restingHeartRate | discrete | count/time | 11.0 |
| rowingSpeed | discrete | distance/time | 18.0 |
| runningGroundContactTime | discrete | time | 16.0 |
| runningPower | discrete | power | 16.0 |
| runningSpeed | discrete | distance/time | 16.0 |
| runningStrideLength | discrete | length | 16.0 |
| runningVerticalOscillation | discrete | length | 16.0 |
| sixMinuteWalkTestDistance | discrete | length | 14.0 |
| stairAscentSpeed, stairDescentSpeed | discrete | distance/time | 14.0 |
| stepCount | cumulative | count | 8.0 |
| swimmingStrokeCount | cumulative | count | 10.0 |
| timeInDaylight | cumulative | time | 17.0 |
| underwaterDepth | discrete | length | 16.0 |
| uvExposure | discrete | count | 9.0 |
| vo2Max | discrete | volume/mass/time (`HKMetadataKeyVO2MaxTestType`) | 11.0 |
| waistCircumference | discrete | length | 11.0 |
| walkingAsymmetryPercentage, walkingDoubleSupportPercentage | discrete | percentage | 14.0 |
| walkingHeartRateAverage | discrete | count/time | 11.0 |
| walkingSpeed | discrete | distance/time | 14.0 |
| walkingStepLength | discrete | length | 14.0 |
| waterTemperature | discrete | temperature | 16.0 |
| workoutEffortScore | ? | ? | 18.0 |

Category types (recalled): sleepAnalysis, appleStandHour, mindfulSession, menstrualFlow, intermenstrualBleeding, ovulationTestResult, cervicalMucusQuality, sexualActivity, high/low/irregular heart rate events, toothbrushing, handwashing, many symptom types, and others.

**Apple Health export XML** (`export.xml`, DTD "HealthKit Export Version: 11", checked):

```
<!ELEMENT HealthData (ExportDate,Me,(Record|Correlation|Workout|ActivitySummary|ClinicalRecord)*)>
<!ATTLIST Record type CDATA #REQUIRED  unit CDATA #IMPLIED  value CDATA #IMPLIED
  sourceName CDATA #REQUIRED  sourceVersion CDATA #IMPLIED  device CDATA #IMPLIED
  creationDate CDATA #IMPLIED  startDate CDATA #REQUIRED  endDate CDATA #REQUIRED>
<!ELEMENT Record ((MetadataEntry|HeartRateVariabilityMetadataList)*)>
<!-- Any Records that appear as children of a correlation also appear as top-level records -->
<!ELEMENT Workout ((MetadataEntry|WorkoutEvent|WorkoutRoute)*)>   (workoutActivityType, duration, durationUnit, totalDistance, totalDistanceUnit, totalEnergyBurned, totalEnergyBurnedUnit, source..., dates)
<!ELEMENT ActivitySummary EMPTY>  (dateComponents, activeEnergyBurned(+Goal,+Unit), appleMoveMinutes(+Goal), appleExerciseTime(+Goal), appleStandHours(+Goal))
<!ELEMENT MetadataEntry EMPTY>    (key, value)
<!ELEMENT InstantaneousBeatsPerMinute EMPTY>  (bpm, time)
<!ELEMENT ClinicalRecord EMPTY>   (type, identifier, sourceName, sourceURL, fhirVersion, receivedDate, resourceFilePath)
```

Category records carry the enum name in `value` (for example `HKCategoryValueSleepAnalysisAsleepREM`). Recalled, unverified: dates are formatted `2024-01-15 07:31:02 +0100` (local offset of the exporting phone, not of the event); `device` is the stringified object description (`<<HKDevice: 0x...>, name:Apple Watch, manufacturer:Apple Inc., model:Watch, hardware:Watch6,2, software:9.1>`); newer iOS adds `WorkoutStatistics` and `WorkoutActivity` children that this DTD version does not list; exports reach several GB, so every importer (Open Wearables, DuckDB write-ups) streams it.

### 2.2 Google Health Connect (Android)

**Checked**: `androidx/androidx` source, `health/connect/connect-client/src/main/java/androidx/health/connect/client/records/`, `metadata/`, `units/`; the aggregate guide on developer.android.com.

**Record type list** (source files): `ActiveCaloriesBurnedRecord`, `ActivityIntensityRecord`, `BasalBodyTemperatureRecord`, `BasalMetabolicRateRecord`, `BloodGlucoseRecord`, `BloodPressureRecord`, `BodyFatRecord`, `BodyTemperatureRecord`, `BodyWaterMassRecord`, `BoneMassRecord`, `CervicalMucusRecord`, `CyclingPedalingCadenceRecord`, `DistanceRecord`, `ElevationGainedRecord`, `ExerciseSessionRecord` (with `ExerciseSegment`, `ExerciseLap`, `ExerciseRoute`), `FloorsClimbedRecord`, `HeartRateRecord`, `HeartRateVariabilityRmssdRecord`, `HeightRecord`, `HydrationRecord`, `IntermenstrualBleedingRecord`, `LeanBodyMassRecord`, `MenstruationFlowRecord`, `MenstruationPeriodRecord`, `MindfulnessSessionRecord`, `NutritionRecord`, `OvulationTestRecord`, `OxygenSaturationRecord`, `PlannedExerciseSessionRecord` (with blocks and steps), `PowerRecord`, `RespiratoryRateRecord`, `RestingHeartRateRecord`, `SexualActivityRecord`, `SkinTemperatureRecord`, `SleepSessionRecord`, `SpeedRecord`, `StepsCadenceRecord`, `StepsRecord`, `TotalCaloriesBurnedRecord`, `Vo2MaxRecord`, `WeightRecord`, `WheelchairPushesRecord`. Plus medical records as FHIR (`MedicalResource`, `MedicalDataSource`, `FhirResource`, `FhirVersion`).

**Four shapes, as interfaces:**

```kotlin
interface Record { val metadata: Metadata }
interface InstantaneousRecord : Record { val time: Instant; val zoneOffset: ZoneOffset? }
interface IntervalRecord : Record { val startTime: Instant; val endTime: Instant;
                                    val startZoneOffset: ZoneOffset?; val endZoneOffset: ZoneOffset? }
interface SeriesRecord<out T : Any> : IntervalRecord { val samples: List<T> }
```

Examples:

- `HeartRateRecord(startTime, startZoneOffset, endTime, endZoneOffset, samples: List<Sample>, metadata)` with `Sample(time: Instant, beatsPerMinute: Long)` (1 to 300). Aggregates `BPM_AVG`, `BPM_MIN`, `BPM_MAX`, `MEASUREMENTS_COUNT`.
- `StepsRecord(startTime, startZoneOffset, endTime, endZoneOffset, count: Long (1 to 1,000,000), metadata)`. Aggregate `COUNT_TOTAL`.
- `SleepSessionRecord(..., title, notes, stages: List<Stage>)`, `Stage(startTime, endTime, stage: Int)`; stage constants `STAGE_TYPE_UNKNOWN` 0, `AWAKE` 1, `SLEEPING` 2, `OUT_OF_BED` 3, `LIGHT` 4, `DEEP` 5, `REM` 6, `AWAKE_IN_BED` 7. Aggregate `SLEEP_DURATION_TOTAL`.
- `ExerciseSessionRecord(..., exerciseType: Int, title, notes, segments, laps, exerciseRoute, plannedExerciseSessionId)`; "Time in segments should be within the parent session, and should not overlap with each other." Heart rate and steps during the session are separate records in the same time range, not children.
- `BloodGlucoseRecord(time, zoneOffset, level: BloodGlucose, specimenSource, mealType, relationToMeal, metadata)`; `specimenSource`: interstitial fluid, capillary blood, plasma, serum, tears, whole blood; `relationToMeal`: general, fasting, before meal, after meal.
- `BloodPressureRecord(time, zoneOffset, systolic: Pressure, diastolic: Pressure, bodyPosition, measurementLocation)`.
- `SkinTemperatureRecord(..., deltas: List<Delta(time, delta: TemperatureDelta)>, baseline: Temperature?, measurementLocation)`.
- `NutritionRecord` (interval): about 40 optional nutrient fields typed `Mass?` (`protein`, `totalCarbohydrate`, `sodium`, `vitaminD`...), `energy`, `energyFromFat` as `Energy?`, `name`, `mealType`.
- `WeightRecord(time, zoneOffset, weight: Mass)`. Aggregates `WEIGHT_AVG/MIN/MAX`.

**Metadata.** `Metadata(recordingMethod: Int, id, dataOrigin: DataOrigin(packageName), lastModifiedTime, clientRecordId: String?, clientRecordVersion: Long, device: Device?)`. The constructor is now `internal`; writers use factories `Metadata.activelyRecorded(device)`, `autoRecorded(device)`, `manualEntry()`, `unknownRecordingMethod()`, so a writer must state the recording method. `RECORDING_METHOD_UNKNOWN` 0, `ACTIVELY_RECORDED` 1, `AUTOMATICALLY_RECORDED` 2, `MANUAL_ENTRY` 3. `clientRecordId`: "There is guaranteed a single entry for any type of data with same client provided identifier for a given client. Any new insertions with the same client provided identifier will either replace or be ignored depending on associated clientRecordVersion." `Device(type, manufacturer, model)` with types `UNKNOWN` 0, `WATCH` 1, `PHONE` 2, `SCALE` 3, `RING` 4, `HEAD_MOUNTED` 5, `FITNESS_BAND` 6, `CHEST_STRAP` 7, `SMART_DISPLAY` 8, `CONSUMER_MEDICAL_DEVICE` 9, `GLASSES` 10, `HEARABLE` 11, `FITNESS_MACHINE` 12, `FITNESS_EQUIPMENT` 13, `PORTABLE_COMPUTER` 14, `METER` 15.

**Units.** Typed unit classes, not strings: `BloodGlucose`, `Energy`, `Length`, `Mass`, `Percentage`, `Power`, `Pressure`, `Temperature`, `TemperatureDelta`, `Velocity`, `Volume`. Each stores (value, original unit) and converts on read (`Energy.inKilocalories`, `inJoules`...). Equality compares in a base unit (calories for `Energy`). `BloodGlucose` converts mg/dL to mmol/L with a fixed factor `1 / 18.0`.

**Aggregation.** `aggregate(metrics, timeRangeFilter, dataOriginFilter)`, `aggregateGroupByDuration(..., timeRangeSlicer = Duration)` and `aggregateGroupByPeriod(..., timeRangeSlicer = Period)`. Period buckets require `LocalDateTime` ranges: "make sure that the start and end times set are not using the `Instant` class. Otherwise, the app returns an `IllegalStateException`." Metrics are typed constants on each record class (cumulative `COUNT_TOTAL`, `DISTANCE_TOTAL`, `ENERGY_TOTAL`; statistical `*_AVG/MIN/MAX`; `DURATION_TOTAL`). Dedup: "When you perform an aggregate read, the Aggregate API accounts for any duplicate data and keeps only the data from the app with the highest priority" and "Only the Activity and Sleep data types are deduped by Health Connect"; for others "the aggregated results combine all data ... from all apps". Users, not apps, set the priority list. History: apps read only 30 days before their first permission grant unless they hold `PERMISSION_READ_HEALTH_DATA_HISTORY`. Incremental sync uses a changes token (recalled).

### 2.3 Samsung Health Data SDK

**Checked by search only.** `DataType.StepsType` and aggregate operations like `DataType.StepsType.TOTAL` read through `requestBuilder.setLocalTimeFilter(...)` or `setLocalTimeFilterWithGroup(LocalTimeFilter, LocalTimeGroup)`; steps come from phone, Galaxy Watch, Fit and Ring and are aggregated by Samsung. Filters are expressed in **local time**. Recalled, unverified: steps are readable only as aggregates in the new SDK, and point data types (heart rate, blood glucose, sleep with stages, exercise with sessions) carry a data source with device id.

### 2.4 Open mHealth and IEEE 1752.1, and Shimmer

**Checked**: `github.com/openmhealth/schemas` (`schema/omh/`), the IEEE 1752 schemas at `w3id.org/ieee/ieee-1752-schema/`, `github.com/openmhealth/shimmer`.

**Status.** Most `omh` schemas now carry a `deprecation` block ("deprecated, in favor of the IEEE 1752.1 schema of the same name"), dated 2022-12-01 to 2026-06-18. IEEE 1752.1-2021 is the standardised successor. The repo is still maintained (last push 2026-08-26). Shimmer, the connector service, has had no push since 2021-05-18; its most-discussed issues are OAuth breakage with Fitbit, iHealth and Jawbone.

**Envelope.** `data-point = {header, body}`. `header = {id, creation_date_time, schema_id {namespace, name, version}, acquisition_provenance {source_name (required), source_data_point_id, source_creation_date_time, source_last_modification_date_time, modality: sensed | self-reported}, user_id}`. The body is a schema per measure (`heart-rate`, `blood-glucose`, `step-count`, `physical-activity`, `sleep-episode`, `body-weight`...), versioned independently (`heart-rate-3.0`, `blood-glucose-4.0`).

**Measurement building blocks.**

- `unit-value = {value: number, unit: string}`. IEEE's description: "Allowed values are drawn from the Common synonyms (non-UCUM) column of [subset of] UCUM, SI and English units." So the units are UCUM **synonyms**, not UCUM codes: `beats/min` (not `/min`), `mg/dL`, `mmol/L`, `kg`, `lb`, `Metric Ton`, `%`, `steps`.
- Each measure narrows the unit enum: `heart_rate.unit` in `["beats/min"]`; `blood_glucose.unit` in `["mg/dL", "mmol/L"]`; `mass-unit-value` in `["fg","pg","ng","ug","mg","g","kg","Metric Ton","gr","oz","lb","Ton"]`; `step_count.unit` in `["steps"]`. Consumers convert; the schema does not pick one canonical unit.
- `time-frame = oneOf {date_time} | {time_interval}`; `time-interval = oneOf {start_date_time, duration} | {end_date_time, duration} | {start_date_time, end_date_time}` (the old OMH version also allowed `{date, part_of_day}`). IEEE: "No commitments are made as to whether the start or end time point itself is included in the interval." `date-time` is RFC 3339 with a mandatory offset.
- `effective_time_frame` is required on every measure; step count requires the interval form.
- `descriptive_statistic`: `average`, `count`, `maximum`, `median`, `minimum`, `standard deviation`, `sum`, `variance`, `20th percentile`, `80th percentile`, `lower quartile`, `upper quartile`, `quartile deviation`, `1st quintile`...`4th quintile`. Plus `descriptive_statistic_denominator` (for example "steps per day"). So a pre-aggregated value is the same schema as a raw value plus a statistic tag.
- Context modifiers: `temporal_relationship_to_physical_activity` (at rest, during exercise...), `temporal_relationship_to_sleep`, `temporal_relationship_to_meal` (fasting, before/after meal...), `specimen_source`, `body_location`, `body_posture`, `position_during_measurement`.
- `sleep-episode`: `effective_time_frame` (interval required), `latency_to_sleep_onset`, `latency_to_arising`, `total_sleep_time`, `wake_after_sleep_onset`, `number_of_awakenings`, `is_main_sleep`, `sleep_maintenance_efficiency_percentage`. There is no stage hypnogram in the OMH schema.
- `physical-activity`: `activity_name`, `effective_time_frame`, `distance`, `kcal_burned`, `reported_activity_intensity`, `met_value`.
- Every schema cites a SNOMED CT or LOINC concept in `references` (heart rate: SNOMED 78564009; step count: LOINC 55423-8).

**Shimmer.** Java service with one "shim" per API (Fitbit, Garmin, Google Fit, iHealth, Jawbone, Misfit, Moves, Withings, RunKeeper). Each shim returns raw JSON or normalised OMH data points. Recalled; the repo confirms it is dormant.

### 2.5 FHIR

**Checked**: R4 Vital Signs profile page, R4 `SampledData` definition, HL7 Personal Health Device IG v2.0.0 STU 2 (`hl7.org/fhir/uv/phd/en/`: ProfilesOverview, PhdBaseObservation, GeneratingtheTimeStampPartIdentifier).

**Observation (recalled core shape).** `{resourceType: Observation, identifier[], status, category[], code (CodeableConcept), subject, effectiveDateTime | effectivePeriod | effectiveInstant | effectiveTiming, issued, performer[], value[x] (valueQuantity {value, unit, system: http://unitsofmeasure.org, code} | valueCodeableConcept | valueString | valueBoolean | valueInteger | valueRange | valueRatio | valueSampledData | valueTime | valueDateTime | valuePeriod), dataAbsentReason, interpretation[], note[], bodySite, method, specimen, device, referenceRange[], hasMember[], derivedFrom[], component[{code, value[x], ...}]}`. A blood pressure is one Observation with two `component`s.

**Vital Signs profile (R4, checked).** Fixed LOINC codes and UCUM units, "extensible bindings": heart rate `8867-4` `/min`; respiratory rate `9279-1` `/min`; SpO2 `2708-6` `%`; body temperature `8310-5` `Cel` or `[degF]`; height `8302-2` `cm` or `[in_i]`; weight `29463-7` `g`, `kg` or `[lb_av]`; BMI `39156-5` `kg/m2`; blood pressure panel `85354-9` with components systolic `8480-6` and diastolic `8462-4` in `mm[Hg]`; head circumference `9843-4`; vital signs panel `85353-1`.

**SampledData (checked)** for waveforms: `origin` (SimpleQuantity, gives the unit of the whole series and the zero point), `period` (ms between samples), `factor`, `lowerLimit`, `upperLimit`, `dimensions` (samples per time point), `data` ("decimal values separated by a single space", with `E` error, `L` below and `U` above detection limit). A space-separated string is the FHIR way to keep 10,000 samples in one resource.

**Personal Health Device IG (checked).** Profiles: `PhdBaseObservation` (abstract), `PhdNumericObservation` (`valueQuantity`), `PhdCodedEnumerationObservation`, `PhdBitsEnumerationObservation`, `PhdRtsaObservation` (`valueSampledData`, "periodic samples"), `PhdStringObservation`, `PhdCompoundObservation`, `PhdCompoundNumericObservation` (`component.valueQuantity`, used for blood pressure), `PhdCoincidentTimeStampObservation` ("reports the PHD device time as observed by the PHG for time auditing purposes"), `PhdDevice`, `PhgDevice` (the gateway), `PhdPatient`. Codes are IEEE 11073-10101 MDC with UCUM units. **Dedup**: "The Observation.identifier element is used in the PHD profiles to perform conditional creates and thus eliminate data duplication." "The identifier is the concatenation of the device identifier, patient identifier, the PHD timestamp of the observation, the observation type code, the measurement duration if present, and the list of Supplemental-Types codes if any", in the order "device - patient - type - timestamp - duration - Supplemental Information", and "All PHGs compliant to this IG should implement this identifier in the same manner." The timestamp part encodes the device's own clock as reported (tick counter seconds, or seconds since 2000-01-01, plus the TZ/DST offset, plus `..duration`), and "is not to be interpreted by a consumer". So identity is derived from content, so two gateways uploading the same reading collide.

IHE PCHA is the profile family (Continua) that the PHD IG implements; I did not fetch IHE documents (**unverified** beyond the IG).

### 2.6 Garmin FIT (de facto model for workouts)

**Checked**: `github.com/garmin/fit-python-sdk` `profile.py` and `util.py`.

Binary file of definition messages and data messages. Global message numbers (`record` = 20, `session`, `lap`, `event`, `device_info`, `hrv`, `monitoring`, `sleep_level`...). Each field has a base type plus a **scale and offset** applied on decode, and a unit:

| `record` field | Base type | Scale | Offset | Unit |
| --- | --- | --- | --- | --- |
| `timestamp` | `date_time` | 1 | 0 | s |
| `position_lat` | `sint32` | 1 | 0 | semicircles |
| `distance` | `uint32` | 100 | 0 | m |
| `heart_rate` | `uint8` | 1 | 0 | bpm |
| `enhanced_speed` | `uint32` | 1000 | 0 | m/s |
| `enhanced_altitude` | `uint32` | 5 | 500 | m |
| `power` | `uint16` | 1 | 0 | watts |
| `temperature` | `sint8` | 1 | 0 | C |

Epoch: `FIT_EPOCH_S = 631065600` (1989-12-31T00:00:00Z). Recalled: `activity` message has `local_timestamp` alongside `timestamp`, which is how the local offset is recovered; developer fields carry their own name and units; invalid values are all-ones per base type. FIT `record` messages are effectively a wide table (one row per second, many nullable columns), and `distance` in a record is cumulative since start, like Terra.

### 2.7 Withings

**Checked**: `developer.withings.com/openapi.yaml`.

`measuregrp` = `{grpid, attrib, date (unix), created, modified, category (real vs objective), deviceid, hash_deviceid, model, model_id, measures[], timezone}`. `measure` = `{value, type, unit, position}` where "`value` * 10^`unit` = real value" ("Eg: value = 20 and unit = -1 => real value = 2"), always in SI. This is an **exact decimal as an integer mantissa and a base-10 exponent**. `attrib`: 0 device and unambiguous, 1 device but ambiguous (may belong to another user), 2 manual entry, 4 manual at creation, and more. `type` is an integer `meastype` (1 weight kg, 4 height m, 6 fat ratio %, 9 diastolic, 10 systolic, 11 pulse, 12 temperature, 54 SpO2, 71 body temperature, 130 AFib result which is a classification, not a quantity...). A measure group is a natural correlation (one weigh-in yields weight, fat, muscle, water, bone).

### 2.8 Gadgetbridge (open source companion app, SQLite)

**Checked**: `codeberg.org/Freeyourgadget/Gadgetbridge` `GBDaoGenerator.java` (greenDAO schema version 151).

137 sample entities, most of them per device family (`MakibesHR3ActivitySample`, `XiaomiActivitySample`, `GarminHeartRateRestingSample`...). Common columns: `timestamp` (int seconds for activity samples, long ms for "time samples"), `deviceId`, `userId`, composite primary key (timestamp, deviceId). Activity samples add device-specific `rawIntensity`, `steps`, `rawKind`, `heartRate`; other samples: `spo2`, `stress`, `respiratoryRate`, `temperature`, `weightKg`, `impedanceOhm`, `bpSystolic`, `bpDiastolic`, HRV fields. The class comment states the design: "Values like activity kind or intensity, are device specific. Normalized values can be retrieved through the corresponding SampleProvider." So Gadgetbridge stores **raw device values and normalises at read time in code**.

### 2.9 OMOP CDM 5.4

**Checked**: `ohdsi.github.io/CommonDataModel/cdm54.html`.

`MEASUREMENT`: `measurement_id`, `person_id`, `measurement_concept_id`, `measurement_date`, `measurement_datetime`, `measurement_time`, `measurement_type_concept_id`, `operator_concept_id` (`<`, `<=`, `=`, `>=`, `>`), `value_as_number`, `value_as_concept_id`, `unit_concept_id`, `range_low`, `range_high`, `provider_id`, `visit_occurrence_id`, `visit_detail_id`, `measurement_source_value`, `measurement_source_concept_id`, `unit_source_value`, `unit_source_concept_id`, `value_source_value`, `measurement_event_id`, `meas_event_field_concept_id`. `OBSERVATION` is similar with `value_as_string` and `qualifier_concept_id`. Rules: numeric results in `value_as_number`, categorical in `value_as_concept_id`; units map to a standard Unit-domain concept (UCUM-based); "Original coding from source systems must be retained in *_source_value fields." Note the pairing of a normalised value with the verbatim source value and unit on every row, and the `operator_concept_id` for censored lab values like `< 5`.

### 2.10 PhysioNet WFDB (recalled, unverified today)

A record is a header file `.hea` plus signal files. The header record line: record name, number of signals, sampling frequency (optionally `/counter`), number of samples, base time and date. One signal line per channel: file name, storage format (`16`, `212`, `80`...), ADC gain with optional baseline and units (`200(0)/mV`), ADC resolution, ADC zero, initial value, checksum, block size, description. Annotations live in a separate file (`.atr`) as (sample index, code) pairs. Physical value = (digital - baseline) / gain. Lessons: store waveforms as dense integer arrays with a per-channel gain and unit, keep time as sample index plus one base time, keep annotations separate from signal.

### 2.11 OpenHumans (recalled)

Data is shared as **files** per project (uploaded JSON, CSV, exports from Fitbit, Oura, Apple Health) with metadata tags; there is no unified measurement schema. It is a consent and file-exchange layer, not a model.

### 2.12 Contrast: metrics systems (recalled, unverified today)

- **InfluxDB line protocol**: `measurement,tag1=v1,tag2=v2 field1=1.0,field2=3i 1696771200000000000`. Tags are indexed strings (source, device); fields are typed values; the timestamp is an integer in ns. Series identity = measurement + tag set. Units are not part of the model.
- **Prometheus / OpenMetrics**: the unit is a **name suffix** in base units (`_seconds`, `_bytes`, `_celsius`, `_meters`) and counters end in `_total`; metric types are counter, gauge, histogram, summary. Counters are monotonic and readers compute `rate()`/`increase()`, handling resets. This is the same split as HealthKit's cumulative vs discrete.
- **OpenTelemetry**: units are **UCUM case-sensitive codes** (`s`, `ms`, `By`, `Cel`, `{request}`), and instruments declare temporality (cumulative vs delta) and monotonicity. The closest existing precedent for "a typed measurement with a UCUM unit and an aggregation temporality".

---

## Part 3a: side-by-side table of the five shapes

Columns: HealthKit (HK), Health Connect (HC), Google Health API (GHA), FHIR (with PHD IG), Open mHealth / IEEE 1752.1 (OMH), Terra, Junction (JX), Open Wearables (OW). Spike, Thryve and Validic appear where they add something.

### Shape 1: instant quantity sample (heart rate 62 bpm at 07:00)

| Model | Representation |
| --- | --- |
| HK | `HKQuantitySample {quantityType: heartRate, quantity: 62 count/min, startDate = endDate, sourceRevision, device, metadata}` |
| HC | `HeartRateRecord {startTime, endTime, samples: [Sample(time, beatsPerMinute: 62)], metadata}` (always a series); single values like weight use `InstantaneousRecord {time, zoneOffset, weight: Mass}` |
| GHA | `DataPoint {dataSource, heartRate {sampleTime {physicalTime, utcOffset, civilTime}, beatsPerMinute: 62}}` |
| FHIR | `Observation {code: LOINC 8867-4 (or MDC), valueQuantity {value: 62, unit: "/min", system: UCUM, code: "/min"}, effectiveDateTime, device, identifier}` |
| OMH | `{header {...acquisition_provenance}, body {heart_rate {value: 62, unit: "beats/min"}, effective_time_frame {date_time}}}` |
| Terra | `heart_rate_data.detailed.hr_samples[] {timestamp, bpm: 62}` inside a Daily, Body, Activity or Sleep document |
| JX | `{timestamp, value: 62, unit: "bpm", timezone_offset, type: null}` inside a group keyed by `source {provider, type, app_id, device_id}` |
| OW | `DataPointSeries {recorded_at, value: 62, series_type -> (heart_rate, bpm), external_device_mapping -> (user, provider, device)}` |
| Thryve | `{startTimestamp, endTimestamp: null, timezoneOffset, dataTypeId, value: 62, additionalDetails}` |

### Shape 2: interval or cumulative quantity (412 steps from 07:00 to 07:15)

| Model | Representation |
| --- | --- |
| HK | `HKQuantitySample {stepCount, 412 count, startDate 07:00, endDate 07:15}`; aggregation style `cumulative`; sum via `HKStatisticsQuery .cumulativeSum` |
| HC | `StepsRecord {startTime, endTime, startZoneOffset, endZoneOffset, count: 412}`; `COUNT_TOTAL` |
| GHA | `steps {interval {startTime, startUtcOffset, endTime, endUtcOffset, civilStartTime, civilEndTime}, count}` |
| FHIR | `Observation {code: LOINC 55423-8 (or 41950-7 per 24h), valueQuantity {412, "{steps}"}, effectivePeriod {start, end}}` |
| OMH | `step_count {value: 412, unit: "steps"}, effective_time_frame {time_interval {start_date_time, end_date_time}}` (interval required) |
| Terra | `step_samples[] {timestamp: 07:15, steps: <cumulative since session start>}`; the interval is implicit |
| JX | `{start, end, value: 412, unit: "count"}` half-open `[start, end)` |
| OW | `recorded_at` plus `value`; the end of the interval is not stored (issue #1526 and #1231 discuss telling daily totals from intraday rows) |
| Spike | columnar `offsets[]`, `durations[]`, `values[]` relative to `start_at` |

### Shape 3: categorical sample (REM from 03:10 to 03:32)

| Model | Representation |
| --- | --- |
| HK | `HKCategorySample {sleepAnalysis, value: asleepREM, startDate, endDate}`; one sample per stage interval; no session parent |
| HC | `SleepSessionRecord.stages[] Stage(startTime, endTime, stage: STAGE_TYPE_REM = 6)` nested in the session |
| GHA | `sleep {type: STAGES, interval, stages[...], summary}` nested in the session |
| FHIR | `Observation` with `valueCodeableConcept` (PHD `PhdCodedEnumerationObservation`), or per-stage Observations with `effectivePeriod`; no standard sleep profile |
| OMH | none for stages; `sleep-episode` holds only totals |
| Terra | `sleep_durations_data.hypnogram_samples[] {timestamp, level: 6}` (level changes; stage holds until next sample) |
| JX | `sleep_cycle {stage_start_offset_second[], stage_end_offset_second[], stage_type[] (3 = REM)}` columnar |
| OW | separate `sleep_stage_intervals` returned with `?include=stages` |

### Shape 4: session with child samples (a 45 min run)

| Model | Representation |
| --- | --- |
| HK | `HKWorkout {workoutActivityType: running, startDate, endDate, duration, workoutEvents, workoutActivities, allStatistics}`; child quantity samples are associated with the workout; route is `HKWorkoutRoute` |
| HC | `ExerciseSessionRecord {exerciseType, startTime, endTime, segments, laps, exerciseRoute}`; heart rate and steps are independent records in the same window |
| GHA | `exercise` session data point; intraday streams are separate data points; `exportExerciseTcx` |
| FHIR | no core workout resource; a parent Observation with `hasMember[]` or `derivedFrom[]`, or `valueSampledData` for streams |
| OMH | `physical-activity {activity_name, effective_time_frame, distance, kcal_burned}`; samples are separate data points |
| Terra | one `Activity` document: `metadata {summary_id, type, start_time, end_time}`, summaries plus nested `*_samples[]` arrays and `lap_data.laps[]` |
| JX | `workouts` summary `{id, sport, time_start, time_end, average_hr, distance...}` plus `workout_stream` summary and `workout_*` timeseries tagged with `source.workout_id` |
| OW | `EventRecord(category=workout) + WorkoutDetails`; samples in `DataPointSeries` in the same window |
| FIT | `session` + `lap[]` + `record[]` (1 Hz wide rows, cumulative `distance`) + `event[]` in one file |

### Shape 5: daily summary (steps, energy, resting HR for 2026-10-07)

| Model | Representation |
| --- | --- |
| HK | not stored for most types; computed with `HKStatisticsCollectionQuery` (anchor at local midnight, 1-day interval). `HKActivitySummary` (export `ActivitySummary dateComponents=...`) is the one stored daily object (rings). `restingHeartRate` is stored as a daily-ish sample |
| HC | not stored; `aggregateGroupByPeriod(Period.ofDays(1))` over a `LocalDateTime` range |
| GHA | `dailyRollUp` over civil time; some types are daily by nature (`dailyRestingHeartRate`, `dailyHeartRateVariability`) |
| FHIR | an Observation with `effectivePeriod` of the day, LOINC "per 24 hour" codes; no first-class summary |
| OMH | the measure schema plus `descriptive_statistic: sum` and `descriptive_statistic_denominator: d` |
| Terra | `Daily` document keyed by (`start_time`, `end_time`), replaced whole on every update during the day |
| JX | `activity` summary `{calendar_date, steps, calories_total, calories_active, distance, time_zone, timezone_offset, heart_rate {...}, source}` keyed by `id`, one per (user, provider, date) |
| OW | computed on read from series (`/summaries/activity`) or stored daily-total rows flagged `is_daily_total` |
| Thryve | `{day: "2025-05-31", timezoneOffset, dataTypeId, value}` with `chronologicalExactness` when the day was not 24 h |
| Validic | `summaries` record, "always 1 per source per day", `start_time`/`end_time` at local midnight, `utc_offset`, `offset_origin` |

### Cross-cutting rows

| Concern | HK | HC | GHA | FHIR / PHD | OMH | Terra | JX | OW |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Unit carried | `HKQuantity` (value + HKUnit), convert on read | typed unit class, convert on read | fixed per field | UCUM `code` per value | `{value, unit}` per value, enum per measure | in field name | `unit` string per sample, fixed per resource | `unit` on series type |
| Provenance | `sourceRevision`, `device`, `metadata` | `dataOrigin` (package), `device {type, manufacturer, model}`, `recordingMethod` | `dataSource {recordingMethod, device, platform, application}` | `device` ref, `performer`, `PhgDevice` | `acquisition_provenance {source_name, modality, ...}` | `device_data`, `upload_type` | `source {provider, type, app_id, device_id}` | `ExternalDeviceMapping`, `source_name` |
| Identity | `uuid`; writer key `HKMetadataKeySyncIdentifier` | `metadata.id`; writer key `clientRecordId` | `name` for identifiable types | `identifier` = device-patient-type-timestamp-duration | `header.id`, `source_data_point_id` | `summary_id` or (`start_time`, `end_time`) | summary `id`; sample = (resource, provider, source type, timestamp) | (data source, start, end) or (data source, series type, instant); `external_id` unused (#1607) |
| Update | immutable; replace by higher `HKMetadataKeySyncVersion` | upsert by `clientRecordId` with higher `clientRecordVersion` | `patch`, `batchDelete` | conditional create on identifier | not specified | superset replace | latest replaces | upsert on key |
| Overlap | statistics queries merge using Health app source order; raw queries do not | aggregate dedups Activity and Sleep by user app priority only | `reconcile` merges into one stream | none | none | none (client filters sources) | winner-takes-group by source type then provider | provider then device-type priority, one source per day (#1287) |
| Timezone | `HKMetadataKeyTimeZone` optional | `zoneOffset` per instant / per end | `utcOffset` + `civilTime` | offset in `dateTime` | offset mandatory in RFC 3339 | offset in timestamp + `timestamp_localization` | UTC + `timezone_offset` seconds; floating for Libre | stored offset (after #474) |

---

## Part 3b: unit systems and unit strings

### Systems in use

| System | Who uses it | Form |
| --- | --- | --- |
| **UCUM codes** (case-sensitive) | FHIR (`system: http://unitsofmeasure.org`), PHD IG, OMOP unit concepts, Terra lab `ucum_code`, OpenTelemetry | `/min`, `mm[Hg]`, `Cel`, `[degF]`, `kg`, `g`, `[lb_av]`, `cm`, `[in_i]`, `kg/m2`, `%`, `mg/dL`, `mmol/L`, `kcal`, `kJ`, `m`, `km`, `ms`, `s`, `min`, `h`, `d`, `L`, `mL`, `L/min`, `mL/(kg.min)`, `W`, `{steps}`, `{beats}/min`, `/min` for breaths, `[IU]`, `ng/mL`, `umol/L`, `10*3/uL` |
| **UCUM common synonyms** | Open mHealth, IEEE 1752.1 | `beats/min`, `breaths/min`, `mg/dL`, `mmol/L`, `kg`, `lb`, `Metric Ton`, `%`, `steps`, `kcal`, `m`, `km`, `mi`, `h`, `min`, `sec`, `K`, `C`, `F` |
| **HKUnit strings** (recalled) | Apple HealthKit, Health Auto Export | `count`, `count/min`, `kg`, `lb`, `g`, `mg/dL`, `mmol<180.15588000005408>/L`, `degC`, `degF`, `mmHg`, `kcal`, `Cal`, `kJ`, `m`, `km`, `mi`, `ft`, `in`, `ms`, `s`, `min`, `hr`, `d`, `%`, `mL`, `L`, `mL/(kg*min)`, `IU`, `dBASPL`, `W`, `m/s`, `S` |
| **Typed unit classes** | Health Connect | `Mass`, `Energy`, `Length`, `Pressure`, `BloodGlucose`, `Temperature`, `TemperatureDelta`, `Power`, `Velocity`, `Volume`, `Percentage`; stored with original unit, compare in base unit |
| **IEEE 11073-10101 MDC unit codes** | PHD IG (alongside UCUM) | numeric codes such as `MDC_DIM_BEAT_PER_MIN` (recalled) |
| **Unit in field name** | Terra, ROOK, Metriport, Junction summaries, Prometheus | `_kg`, `_meters`, `_seconds`, `_bpm`, `_celsius`, `_mg_per_dL`, `_ml_per_min_per_kg`, `_kilojoules`; ROOK adds the JSON type: `_kg_float`, `_seconds_int` |
| **Fixed per type, ad hoc strings** | Junction samples, Validic, Spike, OW, Thryve, Sahha | Junction `bpm`, `count`, `kcal`, `m`, `°C`, `kg`, `cm`, `L`, `L/min`, `min`, `%`, `index`, `unit`, `mmol/L`, `mV`, `rmssd`; Validic `s`, `kcal`, `m`, `count`; Spike `ms`, `g`, `meters`, `mg/dL`, `kcal`, `m/sec`, `sec/m`, `celsius`, `uV`, `percentage`, `breaths/min`, `mL/kg/min`; OW `bpm`, `ms`, `mg/dL`, `%`, `mmHg`, `brpm`, `kg`, `cm`, `°C`, `kcal`, `meters`, `m/s`, `watts`, `liters`, `L/min`, `dB`, `score`, `MET` |
| **Integer mantissa + base-10 exponent** | Withings | `{value: 65750, unit: -3}` = 65.750 in SI |
| **Base type + scale + offset** | Garmin FIT | `distance uint32 scale 100 m`, `enhanced_altitude scale 5 offset 500 m`, `position semicircles` |
| **Gain + baseline per channel** | PhysioNet WFDB | `200(0)/mV` |

### Canonical units each aggregator picked (where they differ)

| Quantity | Terra | Junction | Spike | OW | Validic | ROOK | HC base | FHIR vital signs |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Glucose | mg/dL | mmol/L | mg/dL | mg/dL | (not checked) | (not checked) | mmol/L (converts with 1/18.0) | n/a (lab LOINC codes exist for both) |
| Weight | kg | kg | g | kg | (not checked) | kg | kilograms (recalled) | kg, g, [lb_av] |
| Duration | seconds | seconds (summary), min (some intervals) | ms | minutes (sleep), seconds (workouts) | s | seconds | `Duration` | n/a |
| Energy | kcal and kJ | kcal | kcal | kcal | kcal | kcal | calories internally | n/a |
| Temperature | celsius | °C | celsius | °C | (not checked) | (not checked) | celsius (recalled) | Cel, [degF] |
| Distance | meters | m | meters | meters | m | meters | meters (recalled) | n/a |
| Heart rate | bpm | bpm | bpm | bpm | (not checked) | bpm | beats per minute (Long) | /min |
| HRV | ms (implicit) | "rmssd" | ms | ms | (not checked) | float, unit implicit | ms (recalled) | n/a |

Observation for the unit choice: the only systems that survive contact with labs and FHIR are UCUM codes (with OMOP and Terra lab results adding the verbatim source unit beside the UCUM code). Every wearable aggregator invented ad hoc strings and then disagreed with each other on glucose, weight and duration.

---

## Part 3c: design lessons and published mistakes

Each item names where it was discussed.

1. **Priority-based single-source selection undercounts additive metrics.** Open Wearables issue #1287 (open, 2026-07-09): with iPhone + Apple Watch, `_filter_by_priority` keeps only the watch for the day, so "Apple Health app (de-duplicated iPhone + Apple Watch total): 14,079 steps" vs "Open Wearables ... 4,157 steps (Apple Watch only)". The suggested fix is time-window merging for cumulative metrics, as HealthKit statistics queries do. Junction Sense has the same rule by design ("The selected provider and source-type pair contributes the complete aggregate output for that group. Sense does not combine individual fields"). Spike is the only vendor that documents interval-scheduling merges (`merge_maximize_coverage`) and a separate `intersect_union_sum` method. Lesson: dedup is per **time interval** for cumulative quantities and per **record** for discrete ones; "pick one source per day" is wrong for steps.

2. **Daily totals and intraday samples in one series double count.** OW #1231 ("API mixes daily and intraday level samples, so sometimes total number of steps or energy might be wrong") and #1526 (a per-night average lands with `is_daily_total = NULL`, indistinguishable from an intraday sample, so it is averaged with the weight of one sample). Lesson: a stored value must say what it is a statistic of (OMH `descriptive_statistic`, Sahha `aggregation` + `periodicity`) and over which interval.

3. **Bucketing by UTC date is wrong.** OW #396 ("aggregate by UTC date rather than the user's local timezone"; the fix needed the stored offset, #474 "Preserve timezone offset from wearable providers"). Junction deprecated summary `date` ("ISO8601 datetime string in UTC 00:00") for `calendar_date`. Health Connect refuses `Instant` ranges for period buckets. Google Health API carries `physicalTime` and `civilTime` side by side and offers `rollUp` and `dailyRollUp`. Lesson: store the instant and the offset at the event, and make "local day" a first-class bucket.

4. **Provider "days" do not map 1:1 to local dates.** OW #1554: WHOOP physiological cycles start at wake-up, so with a -04:00 offset "two consecutive physiological cycles can resolve to the same local calendar date"; one day showed 4,528 kcal and the next showed nothing, then fell back to the phone. Junction indexes sleep by **session end** and admits a local-day bucket is not 24 h. Thryve measures that with `chronologicalExactness`. Lesson: keep the provider's own period (cycle id, start, end) and derive the date; never key a mirror row on a computed date alone.

5. **Floating time exists and OpenAPI cannot express it.** Junction: Libre timestamps are local wall-clock with an unknown zone, sent as `+00:00` that "should be ignored", because OpenAPI 3 requires RFC 3339 offsets. Lesson: a time value needs a way to say "offset unknown", not a fake `Z`.

6. **Identity built from provider-computed timestamps creates duplicates.** OW #1607: records matched on (data source, start, end); two delivery routes differed by about 100 ms on `end_datetime`, or the data source identity changed when `device_model` arrived late, and both rows persisted, while `external_id` "is written on insert ... Nothing consults it". The PHD IG instead fixes one canonical identifier recipe that every gateway must compute the same way. HealthKit and Health Connect give writers an explicit key and version (`HKMetadataKeySyncIdentifier`/`SyncVersion`, `clientRecordId`/`clientRecordVersion`). Lesson: prefer the upstream id; when none exists, define one deterministic recipe; carry a version for replace semantics.

7. **Units ignored at ingest are silently wrong for ever.** OW #424 ("unit field from incoming requests is ignored ... If a provider sends data in unexpected units → silently stored incorrectly"), #1125 (Apple XML "mmol/L glucose stored as mg/dL"), #1119 (Health Connect glucose in mmol/L labelled mg/dL), #1138 (Fitbit distance stored in km, platform unit is metres), #1106 (step length in metres labelled cm), #1105 (fraction labelled percent). Lesson: validate dimension on write and keep the source unit next to the canonical one (OMOP `unit_source_value`, Terra lab `source.units` + `measurement.ucum_code`).

8. **A single float column for every series is a known stopgap.** OW docs: values stored as float "handling categorical data as numerical encoding ... a current concession as we are fully aware of the adverse effect this has on storage". OW #539: index bloat on `data_point_series` after deletes from the archival job. OW's data-lifecycle page: raw heart rate "grow[s] linearly with time and connected devices"; daily archives are "roughly 1/500 of the raw rate". Junction's capacity guide: very high frequency series are "1000s to low 10000s samples per day" per user. Lesson: one row per sample in a general-purpose table will not scale; batches/chunks per (series, source, period) are what columnar responses (Spike, Junction `sleep_cycle`, FHIR `SampledData`, WFDB) already do.

9. **Provider and source are different things.** Junction spent two years (deprecation deadline moved from 2024-07-31 to 2026-09-14) separating "Provider" (static metadata: name, logo, slug) from "Source" (per-datum context: provider slug, source type, app id, later device). Lesson: per-datum provenance is a small tuple of references, not an embedded copy of the provider's description.

10. **Session structure keeps getting rebuilt.** Junction replaced the `hypnogram` timeseries with a `sleep_cycle` summary tied to the sleep session. Health Connect nests stages in `SleepSessionRecord`, while HealthKit has no session object at all and everyone reconstructs nights from category samples. Lesson: sleep stages are children of a session with their own intervals, not a free-standing series.

11. **Summary and samples will disagree; do not force them to.** Junction: "There is no strict relation between a field in a Summary type and a Timeseries type"; Thryve: daily values from the source "may extend beyond available epoch data" (Fitbit intraday only during recorded activities). Lesson: keep provider summaries as their own records; computed rollups are a separate, labelled product.

12. **Mean of means is not a mean.** Junction Sense documents that `mean` over session or daily rows "is not weighted by duration or sample count" and `sum` does not reconcile overlapping intervals. Spike instead declares `duration_weighted_avg` for heart rate and HRV. HealthKit has `discreteTemporallyWeighted` as an aggregation style. Lesson: the aggregation method belongs to the quantity type, declared once.

13. **Cumulative and discrete must not mix.** HealthKit: "You cannot combine a discrete option with a cumulative option"; Prometheus counters vs gauges; OTel temporality. Terra's cumulative-since-start `steps` samples are a trap for anyone who sums them.

14. **Pass-through "extra" data rots.** Validic v2 removed "expanded" non-standard data: "all source data is reviewed and standardized". Spike calls its raw Provider Records "recommended primarily for debugging". Gadgetbridge went the other way: 137 per-device raw tables, normalised in code at read time. Lesson for substrate: the mirror keeps the upstream shape (as Gadgetbridge and Spike provider records do), and a separate declared layer carries the canonical form (as Validic and Junction do).

15. **Connectors die faster than schemas.** Shimmer (no commits since 2021; top issues are OAuth failures), Human API's wellness product (acquired and refocused), Metriport (left wearables), the Fitbit Web API (shut down 2026-10-30, tokens not transferable), Junction's 14-day Strava and 30-day Health Connect backfill limits. Lesson: the canonical model must outlive any one provider bundle, and backfill windows are provider facts to record per account.

16. **Typed value, not just a number, for labs.** Terra Lab Reports separates `numeric`, `bounded {operator, value}` ("< 5"), `qualitative`, `text`, `absent`; OMOP uses `operator_concept_id` + `value_as_number` / `value_as_concept_id`. Withings stores AFib (meastype 130) as a classification inside a numeric measure, which every consumer must special-case. Lesson: a measurement value type needs at least quantity, bounded quantity, coded value and absent-with-reason.

17. **Exact numbers are available upstream.** Withings sends an integer mantissa and base-10 exponent; FIT sends integers with a declared scale and offset. Converting to float64 at ingest throws away exactness that the source had. (Relevant to substrate's "exact numbers or refusal" value.)

18. **Context modifiers matter for interpretation.** OMH and Health Connect both attach meal relation, specimen source, body position and measurement location to glucose, blood pressure and temperature; Thryve attaches `trustworthiness` and `medicalGrade`; Withings `attrib` marks ambiguous attribution (a shared scale). These belong on the observation, not in free-text notes.

19. **Users, not apps, own priority.** Health Connect: "Only end users can alter these priority lists." Junction and OW put priorities in a team dashboard. For a single-user substrate the user is both, so priority is user configuration, ideally per quantity type.

20. **History windows are platform policy.** Health Connect: 30 days before first grant unless `PERMISSION_READ_HEALTH_DATA_HISTORY`; reinstalling resets the window. Junction defaults range from 1 day (Dexcom G6) to 270 days (Zwift), maximum 365.

---

## Sources

Aggregators:

- Terra data models: https://docs.tryterra.co/reference/health-and-fitness-api/data-models.md
- Terra OpenAPI: https://github.com/tryterra/openapi/blob/master/dist/core/v2-bundled.yaml
- Terra full docs (pricing, events, SDK, lab reports): https://docs.tryterra.co/llms-full.txt
- Junction OpenAPI: https://api.tryvital.io/openapi.json
- Junction resources: https://docs.junction.com/wearables/providers/resources
- Junction primary key: https://docs.junction.com/wearables/providers/data-primary-key
- Junction time zones: https://docs.junction.com/wearables/providers/timestamps-and-time-zones
- Junction data attributions: https://docs.junction.com/wearables/providers/data-attributions
- Junction historical pull range: https://docs.junction.com/wearables/providers/historical-data-pull-range
- Junction ingestion bounds: https://docs.junction.com/wearables/providers/data-ingestion-bounds
- Junction Sense prioritisation: https://docs.junction.com/sense/data-prioritization
- Junction Sense time semantics: https://docs.junction.com/sense/query-dsl/time-and-data-semantics
- Junction Sense SELECT and GROUP BY: https://docs.junction.com/sense/query-dsl/select-clause , https://docs.junction.com/sense/query-dsl/group-by-clause
- Junction deprecations: https://docs.junction.com/changelog/wearables/deprecations
- Spike metrics matrix: https://docs.spikeapi.com/technical-references/metrics_matrix
- Spike statistics matrix: https://docs.spikeapi.com/technical-references/statistics_matrix
- Spike time series: https://docs.spikeapi.com/api-reference/query-timeseries
- Spike OpenAPI: https://docs.spikeapi.com/api-reference/openapi.json
- ROOK index and pricing: https://docs.tryrook.io/llms.txt
- ROOK Garmin data source fields: https://docs.tryrook.io/data-sources/garmin/
- ROOK data processing: https://docs.tryrook.io/docs/rookconnect/data_processing/
- Thryve data model: https://docs.thryve.health/thryve-product-overview/access/data-model
- Thryve annotations: https://docs.thryve.health/thryve-product-overview/understand/data-annotations
- Thryve data type structure: https://docs.thryve.health/thryve-product-overview/access/data-types-biomarker/data-type-structure
- Validic data model: https://developer.validic.com/docs/working-with-data
- Validic intraday: https://developer.validic.com/reference/intraday-2
- Validic standardization: https://developer.validic.com/docs/standardization
- Human API acquisition: https://pitchbook.com/profiles/company/57698-38 , https://humanapi.co/
- Metriport devices models: https://github.com/metriport/metriport/tree/develop/packages/api-sdk/src/devices/models
- Open Wearables data model: https://openwearables.io/docs/architecture/unified-data-model
- Open Wearables data types: https://openwearables.io/docs/architecture/data-types
- Open Wearables priorities and lifecycle: https://openwearables.io/docs/developer-portal/settings/priorities , https://openwearables.io/docs/developer-portal/settings/data-lifecycle
- Open Wearables issues: https://github.com/the-momentum/open-wearables/issues/1287 , /1231 , /1526 , /1607 , /1554 , /396 , /474 , /424 , /1125 , /1119 , /1138 , /1106 , /1105 , /539
- Health Auto Export format: https://help.healthyapps.dev/en/health-auto-export/export-format/ , https://help.healthyapps.dev/en/health-auto-export/export-format/health-metrics/
- Health Auto Export workout source issue: https://github.com/Lybron/health-auto-export/issues/65
- Sahha: https://docs.sahha.ai/llms.txt , https://docs.sahha.ai/docs/products/biomarkers
- Fitrockr: https://www.fitrockr.com/wearable-api/
- Google Health API: https://developers.google.com/health/reference/rest/v4/users.dataTypes.dataPoints , https://tryterra.co/blog/everything-you-need-to-know-about-google-health-new-api
- Fitbit shutdown: https://openwearables.io/blog/fitbit-web-api-shutdown-2026-migration-guide , https://gadgetsandwearables.com/2026/09/28/fitbit-api-migration-deadline-october-30/
- Pricing (third party): https://www.themomentum.ai/blog/the-real-cost-of-wearables-integration-in-2025-build-vs-buy-analysis , https://sahha.ai/compare/spike-api-alternatives/

Platforms and standards:

- HealthKit docs (JSON): https://developer.apple.com/documentation/healthkit/hkquantitytypeidentifier , /hkstatisticsoptions , /hkquantityaggregationstyle , /hkstatisticscollectionquery , /hkmetadatakeysyncidentifier , /hkmetadatakeysyncversion , /hkmetadatakeytimezone , /hkcategoryvaluesleepanalysis , /hkunit
- Apple Health export DTD: https://github.com/AlekSi/applehealth/blob/main/healthkit11.dtd
- HealthKit dedup discussion: https://developer.apple.com/forums/thread/710937 , https://sahha.ai/blog/deduplicating-health-data-across-wearables/
- Health Connect source: https://github.com/androidx/androidx/tree/androidx-main/health/connect/connect-client/src/main/java/androidx/health/connect/client/records
- Health Connect aggregation: https://developer.android.com/health-and-fitness/guides/health-connect/develop/aggregate-data
- Samsung Health Data SDK: https://developer.samsung.com/health/data/guide/hello-sdk/aggregate-data.html
- Open mHealth schemas: https://github.com/openmhealth/schemas
- IEEE 1752 schemas: https://w3id.org/ieee/ieee-1752-schema/unit-value-1.0.json , /time-frame-1.0.json , /time-interval-1.0.json
- Shimmer: https://github.com/openmhealth/shimmer
- FHIR vital signs: https://hl7.org/fhir/R4/observation-vitalsigns.html
- FHIR SampledData: https://hl7.org/fhir/R4/datatypes.html#SampledData
- PHD IG: https://hl7.org/fhir/uv/phd/en/ProfilesOverview.html , https://hl7.org/fhir/uv/phd/en/StructureDefinition-PhdBaseObservation.html , https://hl7.org/fhir/uv/phd/en/GeneratingtheTimeStampPartIdentifier.html
- Garmin FIT SDK profile: https://github.com/garmin/fit-python-sdk
- Withings OpenAPI: https://developer.withings.com/openapi.yaml
- Gadgetbridge schema: https://codeberg.org/Freeyourgadget/Gadgetbridge/src/branch/master/GBDaoGenerator/src/nodomain/freeyourgadget/gadgetbridge/daogen/GBDaoGenerator.java
- OMOP CDM 5.4: https://ohdsi.github.io/CommonDataModel/cdm54.html
- Recalled, not fetched today: PhysioNet WFDB header format (https://physionet.org/physiotools/wag/header-5.htm), InfluxDB line protocol, Prometheus naming conventions (https://prometheus.io/docs/practices/naming/), OpenTelemetry metric semantic conventions, UCUM (https://ucum.org), OpenHumans.
