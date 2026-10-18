# Workout apps, APIs and file formats

Researcher slice 02. Date: 2026-10-08.

How to read the evidence markers:

- **[web]** I checked it on the web or in a primary artefact (OpenAPI spec, SDK source, XSD) on 2026-10-08.
- **[recalled]** It comes from prior knowledge and I did not re-check it today.
- **[unverified]** I tried and could not confirm it, or it comes from a secondary source only.

The two findings that matter most for substrate come first, because they constrain everything after them.

## 0. Findings that constrain the design

### 0.1 Strava's 2026 API Policy forbids what a personal data server does with Strava data [web]

On 2026-06-01, Strava published a new API Agreement and a separate **API Policy**, which the Agreement incorporates by reference ([agreement](https://www.strava.com/legal/api), [policy](https://www.strava.com/legal/api_policy)). Verbatim clauses:

- **6.2 Cache and Retention**: "You may not retain Strava Data in your cache for longer than seven (7) days. ... Except for such limited caching, you may not store Strava Data".
- **5.5**: "You may not store Strava Data, or any data derived from Strava Data, in any Persistent Index. The foregoing prohibits indefinite storage in vector stores, embedding stores, search indexes, knowledge graphs, retrieval-augmented data stores, archives, and any other storage configured to enable subsequent retrieval, query, or use." It also bans "accumulating Strava Data through repeated authorized API calls into a corpus, dataset, archive, or database that exceeds the operational scope of your Developer Application."
- **5.3**: no use "in connection with the development, training, evaluation, or operation of any AI Application", explicitly including "ingestion into a context window or working memory", and extending to derived, aggregated and anonymized data. The one exception is Strava's own MCP server (3.5), which Strava subscribers may use with "their own AI Application to interact with their own data".
- **5.4**: no aggregated or de-identified analytics, and "You may not combine Strava Data with other customer data for these or any other purposes."
- **5.16**: no "abstraction layer, integration-platform-as-a-service, ... pass-through proxy, intermediary, or aggregator that re-exposes the Strava API Materials", and no "MCP Server, agent-mediated interface, or analogous mechanism that exposes ... Strava Data".
- **6.3**: deletions on Strava must be reflected within 48 hours.
- **2.5 and 6.x**: delete everything about a user on request, on deauthorization or on account deletion, and confirm in writing.
- **6.6**: the user's own right to the Bulk Data Export Tool is not limited by the agreement.

Consequence for substrate: a Strava **provider** that mirrors activities into the changelog (append-only, permanent, FTS-indexed, readable by agents) breaks 6.2, 5.5, 5.3 and 5.16 on its face. The changelog design (deletes are tombstones, history stays) is in direct tension with 6.3 and the deletion clauses. The compliant path is the user's own **bulk export** (a zip of FIT/GPX/TCX originals plus CSV), imported as files the user owns. That path does not use the API at all. Whether a self-hosted, single-user install changes any of this is a legal question I cannot answer; the policy text has no personal-use carve-out except for Strava's own MCP.

### 0.2 Garmin closed its developer program to new applicants in 2026 [web]

- Garmin stopped processing new Connect Developer Program applications in spring 2026, citing a "significant redesign and modernisation of the API program". Existing approved partners keep working. Connect IQ (on-watch apps) is unaffected ([the5krunner, 2026-09-14](https://the5krunner.com/2026/09/14/garmin-developer-api-access-paused/), [Sahha](https://sahha.ai/blog/garmin-developer-program-paused/)).
- Garmin acquired TrainingPeaks and TrainHeroic on 2026-07-22 ([Garmin IR](https://investors.garmin.com/events-and-presentations/news/news-details/2026/Garmin-acquires-TrainingPeaks-and-TrainHeroic-leading-endurance-and-strength-training-platforms-for-athletes-and-coaches/default.aspx)).
- TrainingPeaks' Partner API is approved-partners only and "Personal developer use is not currently permitted" (post updated 2026-09-12, [TrainingPeaks](https://www.trainingpeaks.com/blog/an-update-on-trainingpeaks-partner-api/)).

Consequence: for a self-hosted user, the realistic Garmin paths are (a) FIT files: the user's own export, or USB/MTP access to the device, and (b) the unofficial Garmin Connect web endpoints (the `garminconnect`/`garth` Python libraries). Option (b) breaks Garmin's terms and is fragile. A FIT importer is the most valuable single workout component substrate can build, because FIT is what Garmin, Wahoo, COROS, Suunto, Hammerhead, Zwift, Concept2 and Strava's bulk export all produce.

### 0.3 Open platforms exist and are the friendliest sources [web]

- **intervals.icu** has a full OpenAPI 3 spec at `https://intervals.icu/api/v1/docs`: 118 paths and 110 schemas, read and write. A personal API key uses HTTP basic auth (`API_KEY:<key>`), and OAuth is available for multi-user apps. Limits are 5000 requests/day and 2500 per rolling 15 minutes per key, plus 10/s per IP ([forum](https://forum.intervals.icu/t/api-access-to-intervals-icu/609)). The author's stated rule is that the API shows only your own data and that of athletes you follow. He warns against proxying Strava-sourced data, because of Strava's terms.
- **Concept2 Logbook API**: OAuth2, results with splits and per-stroke data, webhooks (`result-added`, `result-updated`, `result-deleted`), and "The API is not currently rate limited" ([docs](https://log.concept2.com/developers/documentation/)).
- **Hevy**: a public API for Pro users with an API key, documented with an embedded OpenAPI 3.0 document (version 0.0.1). It warns: "we make no guarantees that we won't completely change the structure or abandon the project entirely" ([docs](https://api.hevyapp.com/docs/)).
- **Wahoo Cloud API**: OAuth2 at `api.wahooligan.com`. Scopes cover workouts, plans, routes, power zones and offline data; it supports FIT upload and download ([reference](https://cloud-api.wahooligan.com/)). A developer approved by Wahoo within weeks is cited as a contrast to Garmin (the5krunner, above).

## 1. Platform by platform

### 1.1 Strava API v3 [web unless marked]

- **Auth**: OAuth2. Scopes include `read`, `read_all`, `profile:read_all`, `activity:read`, `activity:read_all`, `activity:write` [recalled].
- **Rate limits** ([docs](https://developers.strava.com/docs/rate-limits/)): overall 200 requests per 15 minutes and 2000 per day; "non-upload" (reads) 100 per 15 minutes and 1000 per day. 15-minute windows sit at :00/:15/:30/:45; the daily window resets at midnight UTC. Headers: `X-RateLimit-Limit`, `X-RateLimit-Usage`, `X-ReadRateLimit-Limit`, `X-ReadRateLimit-Usage`. The policy also caps "athlete capacity" per app, and increases are discretionary.
- **Endpoints**: `GET /athlete/activities` (paged list of SummaryActivity), `GET /activities/{id}` (DetailedActivity, with `segment_efforts`, `splits_metric`, `splits_standard`, `laps`, `best_efforts` [recalled for best_efforts]), `GET /activities/{id}/laps`, `GET /activities/{id}/zones`, `GET /activities/{id}/streams`, `GET /athlete/zones`, `GET /athletes/{id}/stats`, `GET /gear/{id}`, routes (GPX/TCX export), segments and segment efforts, uploads (FIT/TCX/GPX, optionally gzipped).
- **DetailedActivity fields**: id, external_id, upload_id, name, distance (m), moving_time and elapsed_time (s), total_elevation_gain, `type` (deprecated ActivityType), `sport_type`, start_date, start_date_local, timezone, utc_offset, start_latlng, end_latlng, map (summary polyline), trainer, commute, manual, private, gear_id, average_speed, max_speed (m/s), average_cadence, average_temp, average_watts, weighted_average_watts (Strava's NP analogue), kilojoules, device_watts, has_heartrate, max_watts, elev_high, elev_low, pr_count, `workout_type`, `suffer_score` (Relative Effort), description, calories, device_name, segment_efforts, splits_metric, laps, gear, photos.
- **`workout_type`** is an int: run 0 default, 1 race, 2 long run, 3 workout; ride 10 default, 11 race, 12 workout [recalled].
- **Laps**: elapsed_time, moving_time, start_date, distance, start_index, end_index (indexes into the streams), total_elevation_gain, average_speed, max_speed, average_cadence, average_watts, device_watts, lap_index, split.
- **Splits**: distance, elapsed_time, elevation_difference, moving_time, split, average_speed, pace_zone. Strava computes them per km and per mile; the device does not supply them.
- **Streams**: exactly 11 types: `time`, `latlng`, `distance`, `altitude`, `velocity_smooth`, `heartrate`, `cadence`, `watts`, `temp`, `moving`, `grade_smooth`. Request with `keys=` and `key_by_type=true`; each stream carries `series_type` (distance or time), `original_size` and `resolution`. There are no running dynamics, no L/R balance, no RR/HRV, no swim lengths, no strength sets and no developer fields. The original FIT is not downloadable through the API; only the owner's bulk export carries it.
- **Webhooks** ([docs](https://developers.strava.com/docs/webhooks/)): one subscription per app. Object types are `activity` and `athlete`; aspects are `create`, `update`, `delete`. Activity `updates` carry only `title`, `type`, `private`; deauthorization sends `authorized: false`. The callback must answer 200 within 2 seconds, and Strava retries up to 3 times. A webhook is a notification only; the app must then fetch the object.
- **Gear**: id, primary, name, distance (m), plus brand/model/frame_type on DetailedGear [recalled for the extra fields]. A gear id is `b…` for bikes and `g…` for shoes [recalled].
- **SportType enum** (56 values, from `https://developers.strava.com/swagger/sport_type.json`): AlpineSki, BackcountrySki, Badminton, Basketball, Canoeing, Cricket, Crossfit, Dance, EBikeRide, Elliptical, EMountainBikeRide, Golf, GravelRide, Handcycle, HighIntensityIntervalTraining, Hike, IceSkate, InlineSkate, Kayaking, Kitesurf, MountainBikeRide, NordicSki, Padel, PhysicalTherapy, Pickleball, Pilates, Racquetball, Ride, RockClimbing, RollerSki, Rowing, Run, Sail, Skateboard, Snowboard, Snowshoe, Soccer, Squash, StairStepper, StandUpPaddling, Surfing, Swim, TableTennis, Tennis, TrailRun, Velomobile, VirtualRide, VirtualRow, VirtualRun, Volleyball, Walk, WeightTraining, Wheelchair, Windsurf, Workout, Yoga.
- **ActivityType enum** (37 values, deprecated, `activity_type.json`): the older coarse list. A new sport type maps back to its parent (MountainBikeRide reads Ride, EMountainBikeRide reads EBikeRide).
- **Strava-derived metrics**: Relative Effort (`suffer_score`, HR-based, proprietary), weighted average watts, best efforts (fixed distances), segment PRs and KOM ranks, splits, Fitness & Freshness (subscription), and an estimated power curve [recalled].
- **Licence summary**: see 0.1. Per-user display only (6.1). There is no Strava data in AI, analytics, aggregation or a persistent index. Cache for at most 7 days; deletions must propagate within 48 h.

### 1.2 TrainingPeaks [web for access status, recalled for model]

- Partner API only, at `api.trainingpeaks.com`, OAuth2. The docs are a public GitHub wiki (`TrainingPeaks/PartnersAPI`). Now owned by Garmin.
- **Model**: a **workout** is either planned or completed on a calendar day. It has `WorkoutDay`, title, `WorkoutType` (Bike, Run, Swim, Strength, XC-Ski, Rowing, Walk, Brick, Crosstrain, Race, DayOff, MountainBike, Other, Custom [recalled]), planned versus actual duration, distance, TSS, IF, energy, elevation, and `Structure` (structured steps). When you POST a workout with a structure, the API computes planned distance, duration and TSS from the steps and overrides the given values ([search summary of wiki](https://github-wiki-see.page/m/TrainingPeaks/PartnersAPI/wiki/Frequently-Asked-Questions), [unverified] in detail).
- **Metrics**: TSS, IF, NP, hrTSS, rTSS, sTSS, tTSS, CTL/ATL/TSB (the Performance Management Chart), Peak power and pace, EF, Pa:Hr decoupling.
- **TSS source priority** ([help](https://help.trainingpeaks.com/hc/en-us/articles/204071844-TSS-calculation-troubleshooting)): power TSS (needs watts for 80% of ride time and an FTP), then rTSS (pace plus elevation plus threshold pace), then sTSS (swim), then hrTSS (HR plus LTHR), then tTSS (TRIMP-based, duration plus average HR). The user can switch the source per workout.
- **Structured workout file formats** TrainingPeaks and its ecosystem read: `.zwo` (Zwift XML), `.erg` and `.mrc` (ergometer text formats with minutes vs watts or %FTP), FIT workout files, and TP's own JSON structure [recalled].

### 1.3 intervals.icu [web]

The OpenAPI document was downloaded and inspected. It is the most complete open model of endurance training I found.

- **Paths**: `/api/v1/athlete/{id}/activities` (by date range, JSON or CSV), `/api/v1/activity/{id}`, `/activity/{id}/streams`, `/activity/{id}/intervals`, `/athlete/{id}/events` (the calendar: planned workouts, notes, races, targets), `/athlete/{id}/workouts` (library), `/athlete/{id}/folders` (plans), `/athlete/{id}/sport-settings`, `/athlete/{id}/wellness/{date}`, plus gear, chats and file upload. Workouts download as `.zwo`, `.mrc`, `.erg` and FIT.
- **`Activity`** has 184 properties. They group as follows:
  - **identity and source**: id, external_id, strava_id, source, oauth_client_id, file_type, device_name, power_meter, power_meter_serial, crank_length.
  - **time**: start_date, start_date_local, timezone, elapsed_time, moving_time, icu_recording_time, coasting_time, recording_stops.
  - **classification**: type, sub_type, trainer, commute, race, tags, group.
  - **summaries**: distance, elevation gain and loss, speeds, HR, cadence, temperature, calories, carbs_used and carbs_ingested.
  - **running dynamics**: average_stance_time, average_vertical_oscillation, average_vertical_ratio, average_step_length, average_stance_time_balance, average_leg_spring_stiffness, average_impact_loading_rate, average_stride.
  - **load**: icu_training_load, power_load, hr_load, pace_load, hr_load_type, pace_load_type, trimp, strain_score, session_rpe, icu_rpe, feel, perceived_exertion.
  - **power model**: icu_ftp, icu_pm_cp, icu_pm_w_prime, icu_pm_p_max, icu_pm_ftp, icu_rolling_cp, icu_rolling_ftp, icu_w_prime, p_max, icu_weighted_avg_watts, icu_average_watts, icu_joules, icu_joules_above_ftp, icu_max_wbal_depletion, icu_intensity, icu_variability_index, icu_efficiency_factor, icu_power_hr, decoupling, polarization_index.
  - **PMC snapshot**: icu_ctl and icu_atl, as of this activity.
  - **zones**: icu_hr_zones, icu_power_zones, pace_zones, icu_zone_times, icu_hr_zone_times, pace_zone_times, gap_zone_times, plus lthr, threshold_pace, athlete_max_hr and icu_resting_hr. These are **copies of the zone settings at the time of the activity**, so history does not shift when the user edits zones.
  - **swim**: lengths, pool_length.
  - **strength**: kg_lifted.
  - **weather**: average_weather_temp, wind speed and direction, headwind and tailwind percent, clouds, rain, snow.
  - **planning link**: paired_event_id, compliance, workout_shift_secs.
- **`Interval`** has 84 properties: start_index and end_index into the streams, type, label, group_id, zone and zone power bounds, training_load, decoupling, joules_above_ftp, W'bal start and end, average_dfa_a1 (the DFA alpha-1 HRV index), SmO2/THb (muscle oxygen), lactate, respiration, plus per-interval running dynamics and weather.
- **`Event`** (a planned item): category, start_date_local, end_date_local, name, description, `workout_doc` (structured steps), load_target, time_target, distance_target, icu_training_load, icu_ctl and icu_atl (projected), indoor, plan_workout_id.
- **`SportSettings`** (per sport group, keyed by `types`): ftp, indoor_ftp, w_prime, p_max, lthr, max_hr, threshold_pace, `power_zones`, `hr_zones`, `pace_zones` (with names), hr_load_type, pace_load_type, gap_model, best_effort_distances, mmp_model, default_gear_id.
- **`Wellness`** (per date): ctl, atl, rampRate, ctlLoad, atlLoad, weight, restingHR, hrv (rMSSD), hrvSDNN, sleepSecs, sleepScore, soreness, fatigue, stress, mood, motivation, injury, spO2, blood pressure, readiness, vo2max, steps, respiration, macros, menstrual phase.
- **`Gear`**: type, name, distance, time, retired, component_ids (a bike owns its chains and tyres), reminders, activity_filters (rules that auto-assign gear).
- **Activity type enum**: a superset of Strava's sport types plus TrackRide, Cyclocross, Hockey, OpenWaterSwim, Rugby, Transition, VirtualSki, WaterSport, Other.
- **Stream types** include watts, heartrate, cadence, latlng, altitude, temp, velocity, distance, and derived pace, GAP, W'bal and smoothed series. The full list is in the spec under the `stream_types` field [partially verified].

Design lessons from intervals.icu: (1) store the zone and threshold settings that applied **on the activity**; (2) intervals are index ranges over the streams, not separate time series; (3) planned and executed live in separate objects, linked by `paired_event_id`; (4) wellness is a daily record keyed by date.

### 1.4 Garmin [web for status and FIT, recalled for API payloads]

- **Programs**: the Health API (dailies, sleep, stress, HRV, body composition, and more), the **Activity API** (activity summaries, activity details with samples and laps, and FIT/GPX/TCX files; push or ping/pull architecture; [developer.garmin.com](https://developer.garmin.com/gc-developer-program/activity-api/)), the **Training API** (publishes workouts and training plans to the Garmin Connect calendar, which syncs them to devices, [docs](https://developer.garmin.com/gc-developer-program/training-api/)) and the Courses API. Garmin documents all of them only behind approval. New applicants have been refused since spring 2026 (0.2).
- **Activity summary payload** [recalled]: `activityType` (an uppercase string such as `RUNNING` or `LAP_SWIMMING`), startTimeInSeconds, startTimeOffsetInSeconds, durationInSeconds, distanceInMeters, averageHeartRateInBeatsPerMinute, maxHeartRateInBeatsPerMinute, activeKilocalories, averageSpeedInMetersPerSecond, averagePaceInMinutesPerKilometer, averageRunCadenceInStepsPerMinute, averageBikeCadenceInRoundsPerMinute, averagePowerInWatts, totalElevationGainInMeters, steps, deviceName, manual flag.
- **Activity details** [recalled]: `samples[]` with startTimeInSeconds, latitudeInDegree, longitudeInDegree, elevationInMeters, airTemperatureCelcius (sic), heartRate, speedMetersPerSecond, stepsPerMinute, totalDistanceInMeters, timerDurationInSeconds, clockDurationInSeconds, movingDurationInSeconds, powerInWatts, bikeCadenceInRPM, swimCadenceInStrokesPerMinute; plus `laps[]` with startTimeInSeconds. Units are spelled into the field name, which is a convention worth noting.
- **Garmin Connect workout JSON** (the unofficial web API, which the Training API mirrors closely) [recalled, unverified]: `sportType{sportTypeId, sportTypeKey}`, `workoutSegments[].workoutSteps[]`, where a step is either `ExecutableStepDTO` or `RepeatGroupDTO` (numberOfIterations). A step has `stepType` (warmup, interval, recovery, rest, cooldown, repeat, other), `endCondition` (time, distance, lap.button, iterations, reps, calories, heart.rate, power, fixed.rest), `endConditionValue`, `targetType` (no.target, power.zone, cadence, heart.rate.zone, speed.zone, pace.zone), targetValueOne and targetValueTwo or zoneNumber, and for strength `category`, `exerciseName`, `weightValue`, `weightUnit`. This is the FIT `workout_step` message in JSON.
- **Strength**: the FIT `set` message exists, but Connect IQ apps cannot write it (there is no `addSet()`), so third-party watch apps cannot create native strength sessions. Garmin builds strength features into the paid Connect+ tier ([the5krunner, 2026-03-24](https://the5krunner.com/2026/03/24/garmin-connect-plus-strength-apps/)).
- **Garmin-derived metrics** (Firstbeat, proprietary): Aerobic and Anaerobic Training Effect (0 to 5, in FIT as `totalTrainingEffect` and `totalAnaerobicTrainingEffect`, scale 10), EPOC-based Training Load (FIT `trainingLoadPeak`), Acute Load, Load Focus, Training Status, Training Readiness, HRV Status, VO2max, race predictor, recovery time, Body Battery, stamina, and native running power [recalled].

### 1.5 Peloton (unofficial) [web, secondary sources]

- There is no public API. The community uses the members' web endpoints on `api.onepeloton.com`: `POST /auth/login`, `GET /api/me`, `GET /api/user/{id}/workouts?joins=ride`, `GET /api/workout/{id}`, `GET /api/workout/{id}/performance_graph?every_n=N` ([geudrik API_DOCS](https://github.com/geudrik/peloton-client-library/blob/master/API_DOCS.md), [peloton-to-garmin](https://github.com/philosowaffle/peloton-to-garmin/discussions/822)).
- `performance_graph` returns per-N-second series (output in watts, cadence, resistance %, speed, heart rate) plus summaries: total output in kJ, distance, calories, and time in HR zones. Bike resistance is a Peloton-specific percent, not a physical unit. "Output" is the bike's estimated power.
- `peloton-to-garmin` converts Peloton workouts to FIT/TCX for Garmin and Strava; it is the de facto export path.
- **Strength**: classes carry a planned movement list; the Peloton Guide camera tracks reps [unverified].

### 1.6 Zwift [web]

- Partner-only API; hobby developer accounts are not offered ([Zwift support](https://support.zwift.com/en_us/zwift-and-third-party-platforms-SypU0LdVr)). Zwift writes a FIT file per ride and pushes it to Strava, Garmin, TrainingPeaks, intervals.icu and others.
- **`.zwo` workout format** [recalled]: XML. `<workout_file>` holds name, author, sportType (bike or run), tags and `<workout>`, whose children are `Warmup`, `Cooldown`, `Ramp` (PowerLow and PowerHigh as fractions of FTP), `SteadyState` (Duration in s, Power as a fraction of FTP, optional Cadence), `IntervalsT` (Repeat, OnDuration, OffDuration, OnPower, OffPower), `FreeRide` and `MaxEffort`, plus `textevent` cues. Run workouts use pace as a fraction of threshold pace.

### 1.7 Apple HealthKit and WorkoutKit [web]

Checked against `developer.apple.com/tutorials/data/documentation/healthkit/*.json`.

- **`HKWorkout`** (an `HKSample` with start and end): `workoutActivityType`, `duration`, `workoutEvents`, `workoutActivities` (iOS 16+, for multisport and intervals), `allStatistics` and `statistics(for:)` (sum, avg, min, max per `HKQuantityType`), `totalEnergyBurned`, `totalDistance`, `totalSwimmingStrokeCount`, `totalFlightsClimbed` (the last four are legacy accessors), `metadata`, `device`, `sourceRevision`, `workoutPlan` (iOS 17+, WorkoutKit), and **new in iOS 27**: `zoneGroupsByType` and `zoneGroup(for:)` returning `HKWorkoutZoneGroup` (zone configuration plus `zoneDurations`, with `HKWorkoutZone`, `HKWorkoutZoneConfiguration`, `HKLiveWorkoutZoneUpdate`).
- **`HKWorkoutActivity`**: a sub-span with its own `workoutConfiguration` (activityType, locationType, swimmingLocationType, lapLength), start and end, events, statistics and zone groups. A triathlon is one HKWorkout with `swimBikeRun` and five activities including transitions.
- **`HKWorkoutEventType`**: pause, resume, lap, marker, segment, motionPaused, motionResumed, pauseOrResumeRequest. Swim lengths are `lap` events whose metadata carries `HKMetadataKeySwimmingStrokeStyle`; segments are `segment` events [recalled for the metadata detail].
- **Samples during a workout** are ordinary quantity samples (heartRate, runningPower, runningSpeed, runningStrideLength, runningVerticalOscillation, runningGroundContactTime, cyclingPower, cyclingCadence, cyclingSpeed, cyclingFunctionalThresholdPower, distance per sport (distanceWalkingRunning, distanceCycling, distanceSwimming, distanceRowing, distancePaddleSports, distanceCrossCountrySkiing, distanceSkatingSports, distanceDownhillSnowSports, distanceWheelchair), swimmingStrokeCount, rowingSpeed, paddleSportsSpeed, crossCountrySkiingSpeed, underwaterDepth, waterTemperature, physicalEffort (METs), heartRateRecoveryOneMinute, vo2Max). The workout builder associates them with the workout. **A workout does not contain its streams**; a reader queries samples by predicate over the workout's time range or by association.
- **Effort**: `workoutEffortScore` (user-rated 1 to 10) and `estimatedWorkoutEffortScore` (Apple-estimated) quantity types, linked to a workout via `relateWorkoutEffortSample` (iOS 18) [recalled for the method name].
- **`HKWorkoutRoute`**: an `HKSeriesSample` of `CLLocation` (lat, lon, altitude, horizontal and vertical accuracy, course, speed, timestamp), built with `HKWorkoutRouteBuilder` and read with `HKWorkoutRouteQuery` in batches.
- **Metadata keys** [recalled]: HKMetadataKeyIndoorWorkout, HKMetadataKeyLapLength, HKMetadataKeySwimmingLocationType, HKMetadataKeyAverageMETs, HKMetadataKeyElevationAscended, HKMetadataKeyWeatherTemperature, HKMetadataKeyWeatherHumidity, HKMetadataKeyWorkoutBrandName, HKMetadataKeyAverageSpeed, HKMetadataKeyMaximumSpeed, HKMetadataKeyGroupFitness, HKMetadataKeyCoachedWorkout.
- **`HKWorkoutActivityType`**: 84 cases plus `HKWorkoutActivityTypeRest` (listed below in the taxonomy section). Three are deprecated: dance, danceInspiredTraining, mixedMetabolicCardioTraining.
- **Strength**: HealthKit has **no set, rep or load type** (none in the iOS 27 `HKQuantityTypeIdentifier` list of 132 symbols). Strength apps (Hevy, Strong, Fitbod) write a `traditionalStrengthTraining` or `functionalStrengthTraining` workout with energy and HR only. The set detail stays in the app.
- **WorkoutKit** (plans pushed to the Workout app): `CustomWorkout` (activity, location, displayName, warmup, `blocks: [IntervalBlock]` where a block is `steps: [IntervalStep]` plus `iterations`; an IntervalStep has `purpose` work or recovery and a `WorkoutStep` with a `goal` and an `alert`; then cooldown), `SingleGoalWorkout`, `PacerWorkout`, `SwimBikeRunWorkout`, `ScheduledWorkoutPlan`, `WorkoutScheduler`. `WorkoutGoal` covers distance, time, energy, open and pool swim distance with time; `WorkoutAlert` covers HR zone or range, speed, cadence, and power zone, range or threshold [type names web, cases recalled].

### 1.8 Android Health Connect [web, androidx source on `androidx-main`]

Read from `health/connect/connect-client/src/main/java/androidx/health/connect/client/records/*.kt` on 2026-10-08. Some fields may be unreleased: those on `androidx-main` can be ahead of the stable library. Weight, setIndex and RPE on a segment are **[unverified as shipped]**.

- **`ExerciseSessionRecord`** (an IntervalRecord): startTime, startZoneOffset, endTime, endZoneOffset, metadata, `exerciseType` (int), title, notes, `segments: List<ExerciseSegment>`, `laps: List<ExerciseLap>`, `exerciseRouteResult` (route data needs a separate permission and a per-session consent), `plannedExerciseSessionId`, `rateOfPerceivedExertion` (Float 0 to 10).
- **`ExerciseSegment`**: startTime, endTime, `segmentType` (int, about 70 values), `repetitions`, `weight: Mass?`, `setIndex`, `rateOfPerceivedExertion`. This is the only place in Health Connect where a strength **set** lives: one segment per set.
- **`ExerciseLap`**: startTime, endTime, `length: Length?`.
- **`ExerciseRoute.Location`**: time, latitude, longitude, horizontalAccuracy, verticalAccuracy, altitude (Length).
- **`PlannedExerciseSessionRecord`**: start and end, `hasExplicitTime`, exerciseType, `completedExerciseSessionId`, `blocks: List<PlannedExerciseBlock>`, title, notes. A `PlannedExerciseBlock` has repetitions, steps and description. A `PlannedExerciseStep` has exerciseType (a segment type), `exercisePhase` (warmup, active, cooldown, recovery, rest, unknown), `completionGoal`, `performanceTargets` and description.
  - **`ExerciseCompletionGoal`**: DistanceGoal, DistanceAndDurationGoal, StepsGoal, DurationGoal, RepetitionsGoal, TotalCaloriesBurnedGoal, ActiveCaloriesBurnedGoal (plus manual and unknown variants [recalled]).
  - **`ExercisePerformanceTarget`**: PowerTarget (min, max), SpeedTarget, CadenceTarget, HeartRateTarget, WeightTarget (mass), RateOfPerceivedExertionTarget (int), AmrapTarget, UnknownTarget.
- **Unit-typed values**: Health Connect passes `Length`, `Mass`, `Power`, `Velocity`, `Energy` objects rather than bare doubles. This is a precedent for a substrate quantity value type.
- **Per-second data** is in separate series records (`HeartRateRecord`, `PowerRecord`, `SpeedRecord`, `StepsCadenceRecord`, `CyclingPedalingCadenceRecord`, each with `samples[]`), plus interval records (`DistanceRecord`, `ActiveCaloriesBurnedRecord`, `ElevationGainedRecord`). They are linked to the session only by time overlap [recalled].

### 1.9 Strength apps

**Hevy** [web, OpenAPI]

- **Paths**: `/v1/workouts` (GET, POST), `/v1/workouts/count`, `/v1/workouts/events` (a change feed of updated and deleted workouts since a timestamp, which is what a sync cursor needs), `/v1/workouts/{id}` (GET, PUT), `/v1/routines`, `/v1/routine_folders`, `/v1/exercise_templates` (GET, POST custom), `/v1/exercise_history/{templateId}`, `/v1/body_measurements`, `/v1/user/info`.
- **Workout** → `exercises[]`. An exercise has index, title, notes, exercise_template_id and `superset_id`, then `sets[]`. A set has index, `type` (normal, warmup, dropset, failure), `weight_kg`, `reps`, `distance_meters`, `duration_seconds`, `rpe` (posted values limited to 6, 7, 7.5, 8, 8.5, 9, 9.5, 10) and `custom_metric` (steps or floors on stair machines).
- **Routine** (the plan): sets with a `rep_range` and a per-exercise `rest_seconds`. A routine has no tempo field.
- **Exercise template**: id, title, `type` (weight_reps, reps_only, bodyweight_reps, bodyweight_assisted_reps, duration, weight_duration, distance_duration, short_distance_weight), `primary_muscle_group`, `secondary_muscle_groups`, `equipment`, is_custom.
- **Muscle groups**: abdominals, shoulders, biceps, triceps, forearms, quadriceps, hamstrings, calves, glutes, abductors, adductors, lats, upper_back, traps, lower_back, chest, cardio, neck, full_body, other.
- **Equipment**: none, barbell, dumbbell, kettlebell, machine, plate, resistance_band, suspension, other.
- **Body measurements**: weight, lean mass, fat %, and circumference per body part (cm).

**Strong** [web, secondary]: no API; CSV export only. Columns: Date, Workout Name, Duration, Exercise Name, Set Order, Weight, Reps, Distance, Seconds, Notes, Workout Notes, RPE; newer exports add Workout # and Duration (sec). Weight is in the user's display unit with no unit column, so an importer must ask ([Taper guide](https://thetaperapp.com/articles/how-to-export-strong-data/)). Strong's CSV has become the import format other apps accept.

**Fitbod** [recalled, unverified]: no public API. CSV export (Date, Exercise, Reps, Weight(kg), Duration(s), Distance(m), Incline, Resistance, isWarmup, Note, multiplier). It writes HealthKit workouts. Its "muscle recovery" model is proprietary.

**TrainHeroic** (now Garmin): coach-facing strength platform. No public API [recalled].

### 1.10 Run and outdoor apps

- **Runna** (owned by Strava since April 2025, [DC Rainmaker](https://www.dcrainmaker.com/2025/04/strava-acquires-runna-thoughts-forward.html)): no public API. It pushes structured workouts to Garmin, Apple Watch (WorkoutKit), COROS, Suunto and Fitbit, and syncs to Strava. Executed data comes back via those platforms.
- **Nike Run Club**: no public API. The community used undocumented `api.nike.com` activity endpoints with a scraped bearer token; their current status is [unverified]. Export is effectively via HealthKit or Health Connect.
- **Komoot** (acquired by Bending Spoons in 2025): partner API only, no public developer program [web, secondary]. Tours export as GPX by the user.
- **Wahoo**: see 0.3. Workouts carry `workout_type_id`, starts, minutes, plus a `workout_summary` (ascent, cadence, calories, distance, duration, heart rate, power avg and normalized, speed, TSS, work, and a FIT file URL) [recalled field list].
- **Concept2**: a result has id, date, timezone, distance (m), time (tenths of a second), type (rower, skierg, bike, dynamic, slides, paddle, water, snow, rollerski, multierg), weight class, verified, stroke_rate, stroke_count, calories_total, drag_factor, heart_rate {average, min, max, ending, recovery}, and workout {splits[] or intervals[]} (type, time, distance, calories, stroke rate, HR, rest_time, rest_distance). Per-stroke data has t, d, p (pace), spm, hr. It exports CSV, FIT and TCX.

### 1.11 Open source platforms [web for licence and activity, mixed for model]

| Project | Licence | Last push | What it is | Model notes |
| --- | --- | --- | --- | --- |
| [GoldenCheetah](https://github.com/GoldenCheetah/GoldenCheetah) | GPL-2.0 | 2026-10-06 | Desktop analysis for cyclists, runners and triathletes | The reference implementation for metrics: 300+ "metrics" computed per ride, CP models (2- and 3-parameter, extended CP, Morton, Ward-Smith, Veloclinic), PMC (Banister), W'bal (Skiba integral and differential), TRIMP variants, xPower, BikeScore, GOVSS, SwimScore [recalled]. Its JSON ride format has `RIDE{STARTTIME, TAGS, INTERVALS[], SAMPLES[] (SECS, KM, WATTS, CAD, KPH, HR, ALT, LAT, LON, ...), XDATA[]}` [recalled]. |
| [Endurain](https://github.com/endurain-project/endurain) | AGPL-3.0 | 2026-10-01 | Self-hosted Strava-like tracker, Python/FastAPI plus Postgres | Read from source: `activities` (one row of summaries, `workout_feeling`, `workout_rpe`, a `gear_id` FK, strava and garmin ids, per-field privacy flags), `activity_laps` (FIT lap columns 1:1), `activity_sets` (duration, repetitions, weight, set_type, category, category_subtype: the FIT `set` message), `activity_workout_steps` (the FIT `workout_step` message), and `activity_streams` (**one row per activity per stream type**, the samples as a JSON list, types HR, power, cadence, elevation, speed, pace, map, temperature). Imports FIT/GPX/TCX and syncs Strava and Garmin Connect. |
| [wger](https://github.com/wger-project/wger) | AGPL-3.0 (code); exercise data CC-BY-SA 4.0 per exercise | 2026-10-07 | Self-hosted workout, nutrition and weight tracker with a REST API | Routines → days → slots → slot entries with sets, reps, weight, RIR and RPE; workout logs per set; a public exercise database (section 3). |
| [FitoTrack](https://codeberg.org/jannis/FitoTrack) | GPL-3.0 [recalled] | 2026-09-27 (Codeberg) | Android GPS tracker | Local SQLite, GPX export. |
| [OpenTracks](https://codeberg.org/OpenTracksApp/OpenTracks) | Apache-2.0 | moved to Codeberg 2025 | Android GPS tracker | KMZ/GPX export; sensor HR, cadence and power over BLE [recalled]. |
| [FitTrackee](https://github.com/SamR1/FitTrackee) | AGPL-3.0 | 2026-09-26 | Self-hosted outdoor activity tracker | GPX-centred; sports table with per-user settings; segments per GPX `trkseg`; equipment [recalled]. |

## 2. File formats

### 2.1 Garmin FIT [web: Garmin FIT JavaScript SDK 21.218.0, `profile.js`, licence text]

- **Structure**: a binary file. A 12- or 14-byte header (with protocol and profile version), then **definition messages** (local message type → global message number, field number, size, base type, architecture), **data messages**, and a CRC. Developer fields are declared at runtime. Compressed timestamp headers save bytes on 1 Hz records.
- **Profile 21.218.0** declares **125 global messages** and 215 types. Files come in types: activity, workout, course, settings, sport, totals, goals, schedule, monitoring (daily), weight, blood pressure, segment, and others.
- **Timestamps**: `date_time` is seconds since **1989-12-31T00:00:00Z** (add 631065600 to get Unix time). Values below 0x10000000 are relative (system time). `local_timestamp` in `activity` gives the local offset.
- **Units convention**: every numeric field is an integer with a **scale** and an **offset**: value = raw / scale − offset. Examples from `profile.js`:
  - `position_lat` and `position_long` are in **semicircles** (sint32): degrees = semicircles × 180 / 2^31.
  - `altitude` is m, scale 5, offset 500 (uint16); `enhanced_altitude` is uint32, scale 5, offset 500.
  - `speed` is m/s, scale 1000; `enhanced_speed` is uint32, scale 1000.
  - `distance` is m, scale 100.
  - `total_elapsed_time` and `total_timer_time` are s, scale 1000.
  - `vertical_oscillation` is mm, scale 10; `stance_time` is ms, scale 10; `step_length` is mm, scale 10; `vertical_ratio` and `stance_time_balance` are percent, scale 100.
  - `fractional_cadence` is rpm, scale 128, so cadence = cadence + fractional_cadence.
  - `training_stress_score` is tss, scale 10; `intensity_factor` is if, scale 1000.
  - `set.weight` is kg, scale 16; `workout_step.exercise_weight` is kg, scale 100.
  - `hrv.time` is s, scale 1000, an array of RR intervals.
  - `left_power_phase` is degrees, scale 0.7111111 (one byte maps 0..255 onto 0..360°).
  - `left_right_balance` is a bit field: the high bit says "right", the low 7 bits are the percent.
  - Invalid values are sentinels per base type (0xFF, 0xFFFF, 0x7FFFFFFF, ...), not nulls.
- **Key messages** (global numbers): `file_id` (0), `user_profile` (3), `zones_target` (7), `sport` (12), `session` (18), `lap` (19), `record` (20), `event` (21), `device_info` (23), `workout` (26), `workout_step` (27), `totals` (33), `activity` (34), `hrv` (78), `length` (101), `segment_lap` (142), `field_description` (206), `developer_data_id` (207), `time_in_zone` (216), `set` (225), `exercise_title` (264), `split` (312), `split_summary` (313), plus newer `hrv_status_summary`, `hrv_value`, `beat_intervals`, `respiration_rate`, `skin_temp_overnight`, `sleep_assessment`, `climb_pro`, `jump`, dive messages, and others.
  - **`activity`** → N **`session`** (one per sport in a multisport file) → N **`lap`** → N **`length`** (swim pool lengths: active or idle, stroke, strokes, cadence). The `record` messages (per-second samples) and `event` messages (timer start and stop, lap, workout step, gear changes, rider position) are a flat time-ordered sequence. A reader attaches them to laps by timestamp, not by reference.
  - **`session`** has about 200 fields: sport and sub_sport, the time triple (elapsed, timer, moving), distance, ascent and descent, HR, cadence, power avg and max, normalized power, TSS, IF, threshold power, left/right balance, swim (pool_length, num_lengths, avg_swolf, avg_strokes_per_length, swim_stroke), running dynamics averages, pedal dynamics, time-in-zone arrays, Training Effect, `workout_rpe` and `workout_feel`, `avg_grade_adjusted_speed`, core temperature, respiration, `rmssd_hrv`, `sdrr_hrv`.
  - **`lap`**: the same summary shape plus `intensity` (active, rest, warmup, cooldown, recovery, interval, other), `lap_trigger` (manual, time, distance, position*, session_end, fitness_equipment), `wkt_step_index` (the link from executed lap to planned step) and `repetition_num`.
  - **`record`**: position, altitude, HR, cadence, distance, speed, power, grade, temperature, running dynamics, L/R balance, torque effectiveness, pedal smoothness, power phase, SmO2/THb, e-bike battery and assist, respiration, core temperature, dive fields, and the "grit" and "flow" MTB metrics.
  - **`set`** (strength): duration, repetitions, weight (kg/16), set_type (rest or active), start_time, `category` and `category_subtype` (exercise category plus exercise name enums), weight_display_unit, wkt_step_index.
  - **`workout`** and **`workout_step`** (plans): duration_type (time, distance, HR thresholds, calories, open, repeat-until variants, power thresholds, `training_peaks_tss`, reps, time_only), target_type (speed, heart_rate, open, cadence, power, grade, resistance, power_3s/10s/30s, power_lap, swim_stroke, speed_lap, heart_rate_lap), target_value (a zone number) or custom low and high, intensity, notes, equipment (swim fins, kickboard, paddles, pull buoy, snorkel), exercise_category, exercise_name, exercise_weight, and secondary targets.
  - **`time_in_zone`**: per session or lap, times in HR, speed, cadence and power zones **with the zone boundaries and the calc type** (hr_calc_type: %max, %HRR, %LTHR; pwr_calc_type: %FTP), max HR, resting HR, threshold HR and FTP. The file carries its own zone context, as intervals.icu does.
  - **`split`** and **`split_summary`**: device-computed splits by `split_type` (interval active, rest, warmup, cooldown, recovery, climb, run/walk/stand for run-walk, ski lift vs run, surf, transition).
- **Developer fields**: a `developer_data_id` (an application UUID such as Stryd's) plus `field_description` (name, units, base type, scale, offset, optional native_mesg_num/native_field_num mapping onto a standard field) let any Connect IQ app or sensor add typed fields to `record`, `lap` and `session`. Stryd running power, Moxy SmO2 and CORE temperature arrived this way first. An importer must keep unknown developer fields with their declared names and units, or the data is lost.
- **Exercise taxonomy inside FIT**: 52 `exercise_category` values (bench_press, calf_raise, cardio, carry, chop, core, crunch, curl, deadlift, flye, hip_raise, hip_stability, hip_swing, hyperextension, lateral_raise, leg_curl, leg_raise, lunge, olympic_lift, plank, plyo, pull_up, push_up, row, shoulder_press, shoulder_stability, shrug, sit_up, squat, total_body, triceps_extension, warm_up, run, bike, cardio_sensors, move, pose, banded_exercises, battle_rope, elliptical, floor_climb, indoor_bike, indoor_row, ladder, sandbag, sled, sledge_hammer, stair_stepper, suspension, tire, run_indoor, bike_outdoor). There are **51 per-category `*_exercise_name` enums totalling 1846 named exercises**. FIT encodes "barbell back squat" as `(category=squat, name=barbell_back_squat)`, a movement-pattern taxonomy, not a muscle one.
- **Licence**: the FIT SDK is under the "FIT Protocol License". It grants a "non-exclusive, royalty-free, non-transferable, non-sublicensable, limited license to use the Licensed Technology for Licensee's internal business purposes, including to use the FIT protocol in any software created by Licensee". Section 2(c) forbids distributing the SDK to third parties "except as set forth herein". Section 2(d) forbids distributing it "so that any part of it becomes subject to any license that requires ... [it] be disclosed or distributed in source code form" (an anti-copyleft clause). Section 2(h) requires strict compliance with the protocol, and section 4 calls the SDK Confidential Information. **Implication**: vendoring Garmin's SDK into an open source repository is risky. Writing an independent decoder from the published protocol, or using an independent library, avoids distributing the SDK. Options: the Go `github.com/tormoder/fit` (MIT), the Python `fitdecode` (MIT) and `fitparse` (MIT), and the Rust `fitparser` (MIT) [licences recalled]. Those libraries embed the profile tables generated from Garmin's `Profile.xlsx`; whether that is covered by the licence is unclear [unverified]. Substrate's provider functions are Python, so `fitdecode` is the natural choice.

### 2.2 TCX (Training Center XML v2) [web: XSD]

- `TrainingCenterDatabase/Activities/Activity[@Sport]`, where **`Sport_t` has three values: Running, Biking, Other**. Then `Id` (start time), `Lap[@StartTime]` (TotalTimeSeconds, DistanceMeters, MaximumSpeed, Calories, AverageHeartRateBpm, MaximumHeartRateBpm, `Intensity` Active or Resting, Cadence, `TriggerMethod`, Notes), then `Track/Trackpoint` (Time, Position LatitudeDegrees and LongitudeDegrees, AltitudeMeters, DistanceMeters, HeartRateBpm, Cadence, SensorState), then `Creator`.
- **ActivityExtension v2** adds, per trackpoint (`TPX`), Speed, RunCadence and Watts, and per lap (`LX`) AvgSpeed, MaxBikeCadence, AvgRunCadence, MaxRunCadence, Steps, AvgWatts and MaxWatts.
- TCX also defines `Workouts` (Step, Repeat, Duration, Target with HR or speed zones) and `Courses`, and `MultiSportSession`.
- **What TCX loses compared with FIT**: sport granularity (three values), swim lengths and strokes, strength sets, running dynamics, L/R balance, pedal dynamics, HRV, temperature, device and sensor info, developer fields, events (pauses must be inferred from time gaps), zones and thresholds, and Training Effect. Run cadence is ambiguous: single-foot or both feet varies by exporter.

### 2.3 GPX 1.1 (Topografix) with extensions [web for TrackPointExtension XSD, recalled for the rest]

- `gpx/metadata`, `wpt`, `rte`, `trk/name,type,trkseg/trkpt[@lat,@lon]/ele,time,extensions`. `trk/type` is free text, so there is no sport enum.
- **Garmin TrackPointExtension v2** adds per point: `hr`, `cad`, `atemp`, `wtemp`, `depth`, `speed`, `course`, `bearing`. Power has **no** standard element: Strava, Wahoo and others write a bare `<power>` inside extensions, and readers accept it by convention. Garmin GpxExtensions v3 adds display colour and route points.
- **What GPX loses**: laps (a `trkseg` break is the only boundary, and it is overloaded for pauses), any summary, sport type, power (non-standard), every non-positional sensor beyond HR, cadence and temperature, swim, strength and plans. GPX is a route and track format; treat it as geometry plus timestamps.

### 2.4 Other formats seen [recalled]

- **`.zwo`** (Zwift workouts, XML): see 1.6.
- **`.erg` and `.mrc`**: text headers plus `[COURSE DATA]` minute/watts or minute/%FTP pairs.
- **GoldenCheetah `.json`** rides and the intervals.icu workout text DSL (for example `- 10m 55-75%` lines).
- **KML and KMZ** (OpenTracks).
- **CSV**: Strong, Hevy, Fitbod and Concept2.
- **Strava bulk export**: a zip with `activities.csv`, the original uploads in `activities/` (`.fit.gz`, `.gpx`, `.tcx.gz`), `media/` and gear CSVs. This is the one Strava path that substrate can legally retain, because it is the user's own copy (policy 6.6).

### 2.5 Fidelity ladder

| Data | FIT | TCX | GPX + TPX | Strava API | HealthKit | Health Connect |
| --- | --- | --- | --- | --- | --- | --- |
| Multisport sessions | yes | MultiSportSession | no | no (one activity per sport) | HKWorkoutActivity | no (one session per type) |
| Laps with trigger and intensity | yes | intensity, trigger | no | laps (no intensity) | lap and segment events | laps (time and length only) |
| Planned step ↔ lap link | `wkt_step_index` | no | no | no | via workoutPlan [unverified] | `plannedExerciseSessionId` (session level) |
| Swim lengths and stroke | `length` | no | no | no | lap events with stroke metadata | segments by stroke type |
| Strength sets | `set` | no | no | no | no | `ExerciseSegment` with reps, weight [unverified shipped] |
| Per-second HR, speed, cadence, power | yes | yes (power via ext) | HR, cad (power non-std) | 11 streams | separate samples | separate series records |
| Running dynamics | yes | no | no | no | VO, GCT, stride, power | no [recalled] |
| Pedal dynamics, L/R balance | yes | no | no | no | no | no |
| RR intervals | `hrv` | no | no | no | heartbeat series (separate) | no |
| Zones and thresholds at the time | `time_in_zone`, `zones_target` | no | no | `/activities/{id}/zones` | iOS 27 zone groups | no |
| Developer fields | yes | no | free-form ext | no | metadata only | no |
| Perceived exertion | `workout_rpe`, `workout_feel` | no | no | no (UI only) [unverified] | effort score | session and segment RPE |
| Gear | no (device_info only) | no | no | gear_id | no | no |

## 3. Exercise taxonomy for strength training

| Source | Size | Licence | Muscle model | Equipment model | Other facets |
| --- | --- | --- | --- | --- | --- |
| **FIT profile** (21.218.0) [web] | 1846 names in 51 category enums | FIT Protocol License | none: categories are movement patterns (squat, deadlift, row, pull_up, ...) | implied in the name (`barbell_back_squat`, `dumbbell_...`) | category + subtype pair is the identity |
| **Health Connect** segment types [web] | about 70 | Apache-2.0 (code) | none | implied in the name (`DUMBBELL_ROW`, `BARBELL_SHOULDER_PRESS`) | also covers cardio and swim strokes; left and right arm variants are separate values |
| **wger** [web, `wger.de/api/v2`] | 919 exercises (the `exerciseinfo` count) | AGPL code; exercise records carry their own licence, e.g. CC-BY-SA 4.0 with `license_author` | **15 anatomical muscles** with Latin name, English name and `is_front` (for body diagrams): anterior deltoid, biceps brachii, biceps femoris, brachialis, gastrocnemius, gluteus maximus, latissimus dorsi, obliquus externus abdominis, pectoralis major, quadriceps femoris, rectus abdominis, serratus anterior, soleus, trapezius, triceps brachii; primary vs `muscles_secondary` | 12: barbell, bench, cable machine, dumbbell, gym mat, incline bench, kettlebell, pull-up bar, resistance band, SZ-bar, Swiss ball, none (bodyweight) | 8 categories (abs, arms, back, calves, cardio, chest, legs, shoulders); `variation_group` links variants; translations; images and videos |
| **free-exercise-db** [web, `yuhonas/free-exercise-db`] | 876 | Unlicense (public domain) | 17 lay muscles: abdominals, abductors, adductors, biceps, calves, chest, forearms, glutes, hamstrings, lats, lower back, middle back, neck, quadriceps, shoulders, traps, triceps; primary and secondary | 12: body only, machine, other, foam roll, kettlebells, dumbbell, cable, barbell, bands, medicine ball, exercise ball, e-z curl bar (77 null) | `force` (push, pull, static), `level` (beginner, intermediate, expert), `mechanic` (compound, isolation), `category` (strength, stretching, plyometrics, strongman, powerlifting, cardio, olympic weightlifting), instructions, images |
| **ExerciseDB v1 OSS** [web, `oss.exercisedb.dev`] | 1500 | AGPL-3.0 code; free API non-commercial with attribution | 10 `bodyParts` (back, cardio, chest, lower arms, lower legs, neck, shoulders, upper arms, upper legs, waist) plus 38 `targetMuscles`/`secondaryMuscles` (mixed anatomical and lay: pectorals, latissimus dorsi, erector spinae, rotator cuff, grip muscles, cardiovascular system, ...) | 30 (stepmill machine, smith machine, trap bar, EZ bar, olympic barbell, leverage machine, cable, bodyweight, assisted, weighted, ...) | GIF per exercise, step instructions. The commercial version claims 11000+ exercises. |
| **Hevy** [web] | about 400+ built-in [unverified count] plus custom | proprietary | 20 groups (section 1.9): one primary plus secondary | 9 | `type` defines which set fields apply (weight_reps, duration, distance_duration, ...) |

How they disagree:

- **Identity**: FIT and Health Connect identify an exercise by enum (stable integers), the open databases by id or uuid, Hevy by template id. There is no shared key, so a mapping table is required, and fuzzy name matching is the norm in importers.
- **Muscle vocabulary**: anatomical (wger: biceps femoris), lay (free-exercise-db and Hevy: hamstrings), regional (ExerciseDB bodyParts: upper legs) and none (FIT, Health Connect). A canonical model needs a two-level hierarchy: region → muscle group → anatomical muscle. FMA or SNOMED CT body structure codes could anchor it [suggestion].
- **Equipment**: barbell variants (olympic barbell, EZ/SZ bar, trap bar, smith machine) are separate in ExerciseDB, partly merged elsewhere; "machine" is one bucket in Hevy and free-exercise-db.
- **Unilateral work**: Health Connect splits left and right into separate types; FIT has `single_arm_*` names; Hevy and Strong log a single exercise with no side field.
- **What a set needs**, comparing all sources: exercise reference, order index, set type (warmup, normal or working, dropset, failure, plus rest in FIT), reps, load (mass with unit, or bodyweight, or assistance as negative load, or band colour), duration, distance, RPE (Hevy uses 6 to 10 in half steps; Health Connect 0 to 10; RIR in wger), rest after the set, tempo (no API carries tempo as a field; it lives in notes), superset grouping, side (L/R), and start time (FIT and Health Connect only). Plans add rep ranges, a target load or %1RM, and target RPE.

## 4. Workout data model comparison

### 4.1 Session

| Platform | Session object | Time | Sport | Multisport |
| --- | --- | --- | --- | --- |
| Strava | Activity | start_date (UTC) + start_date_local + timezone + utc_offset; moving_time vs elapsed_time | `sport_type` (56) + legacy `type` | separate activities |
| FIT | activity → session(s) | start_time, total_elapsed_time, total_timer_time, total_moving_time | `sport` + `sub_sport` pair | sessions within one activity, `transition` sport |
| intervals.icu | Activity | start_date, start_date_local, timezone, elapsed, moving, recording time | `type` | separate activities, `Transition` type |
| Garmin API | activity summary | startTimeInSeconds + offset; duration | `activityType` string | parent and child activities [recalled] |
| HealthKit | HKWorkout | start and end dates, duration | `HKWorkoutActivityType` + indoor metadata + swimming location | HKWorkoutActivity children |
| Health Connect | ExerciseSessionRecord | start and end Instant + zone offsets | `exerciseType` int | none |
| WHOOP v2 | workout | start, end, timezone_offset | `sport_name` free string | none |
| Hevy | workout | start_time, end_time | none (implicitly strength) | n/a |
| Concept2 | result | date + timezone, time in tenths | `type` (erg kind) | n/a |

Every platform distinguishes **elapsed vs timer or moving time**; a canonical model needs all three. Most store local time plus offset; HealthKit stores only UTC, with a `HKMetadataKeyTimeZone` metadata key [recalled].

### 4.2 Laps, intervals, splits, segments

- **Device laps** (FIT `lap`, Strava `laps`, HealthKit lap events, Health Connect `laps`): a time range with summary metrics and a trigger.
- **Splits** are recomputed by distance (Strava per km and per mile; FIT `split` by type).
- **Intervals** are analysis ranges (intervals.icu `Interval` with start_index and end_index, recomputed or user-edited, with `lock_intervals`).
- **Segments**: Strava segments are geographic, matched by Strava; Health Connect segments are exercise phases or sets; HealthKit `segment` events are app-defined.
- One word, three meanings. Substrate should name them apart: **lap** (recorded boundary), **split** (derived fixed-distance or fixed-time slice), **interval** (an analysis range, possibly matched to a planned step) and **segment effort** (a geographic match).

### 4.3 Planned vs executed

| Platform | Plan object | Step model | Link to executed |
| --- | --- | --- | --- |
| FIT | workout file: `workout` + `workout_step` | duration type/value, target type/value or low/high, intensity, repeat steps refer back by index | `lap.wkt_step_index`, `set.wkt_step_index` |
| Garmin Training API / Connect | workout JSON (segments → steps or repeat groups) | stepType, endCondition, targetType, zones | Garmin matches internally |
| TrainingPeaks | workout with `Structure` | steps with intensity targets as %FTP, %LTHR or %threshold pace | the same workout object gains actuals ("completed") |
| intervals.icu | Event with `workout_doc`; library Workout | text DSL → steps | `Activity.paired_event_id`, `compliance` |
| HealthKit / WorkoutKit | CustomWorkout etc. | blocks × iterations → steps (goal + alert) | `HKWorkout.workoutPlan` |
| Health Connect | PlannedExerciseSessionRecord | blocks × repetitions → steps (phase, completion goal, performance targets incl. weight, RPE, AMRAP) | `completedExerciseSessionId` ↔ `plannedExerciseSessionId` |
| Hevy | routine | exercises → sets with rep_range, rest_seconds | the workout is created from a routine [no explicit link field found in the schema] |
| Zwift | `.zwo` | typed blocks with power as a fraction of FTP | none |

Targets are expressed **relative to a threshold** (%FTP, %LTHR, %threshold pace, zone number) in almost every plan format. Resolving a target to absolute watts needs the threshold in force on the day of execution.

### 4.4 Domain specifics

- **Swimming**: FIT `length` (active or idle, stroke, strokes, cadence) under laps; pool_length plus unit on the session; SWOLF = length time in seconds + strokes. HealthKit uses lap events with stroke style; Health Connect uses segment types per stroke; Strava keeps only the activity totals plus laps. Open water uses GPS; pool uses accelerometer lengths.
- **Cycling power**: per-second `watts` (Strava), `power` + `accumulated_power` + L/R balance + torque effectiveness + pedal smoothness + platform centre offset + power phase (FIT), cyclingPower samples (HealthKit), PowerRecord (Health Connect). Derived: NP, IF, TSS, VI, kJ, W'bal, MMP curve.
- **Running dynamics** (FIT `record` and `session`; HealthKit samples; intervals.icu averages): cadence (steps/min, FIT stores **strides per minute** for running, so readers double it), vertical oscillation (mm), ground contact time (ms), GCT balance (%), vertical ratio (%), step length (mm), running power (W, vendor-specific), leg spring stiffness and impact loading rate (Stryd and COROS via developer fields).
- **Per-second streams**: FIT at 1 Hz or "smart recording" (irregular), Strava 11 arrays sharing an index, HealthKit and Health Connect irregular samples. A one-hour ride at 1 Hz with 10 channels is 36,000 values. A year of training at 8 h/week is about 1.5 M samples per channel. **This is the volume problem**: one record per sample would be about 15 M records per user-year for workouts alone. The platforms store streams as **columnar arrays per activity** (Strava streams, Endurain's JSON per stream type, intervals.icu streams, GoldenCheetah samples). That fits substrate's `blobref` (content-addressed, 64 MiB cap) far better than records.
- **Routes and GPS**: FIT semicircles, GPX and TCX decimal degrees, HealthKit CLLocation with accuracies, Health Connect Location with accuracies, Strava `latlng` stream plus an encoded summary polyline. Privacy zones (Strava, Endurain's `hide_location`) trim the start and end.
- **Equipment and gear**: Strava gear (bike, shoes, distance), intervals.icu gear with components and reminders, Endurain gear, Garmin gear (unofficial). FIT has `device_info` for sensors, not gear. HealthKit and Health Connect have no gear.
- **Perceived exertion and notes**: FIT `workout_rpe` (0 to 100 scaled, per Garmin's 1 to 10 UI [unverified scale]) and `workout_feel`; intervals.icu `icu_rpe`, `feel`, `session_rpe` (RPE × minutes); Health Connect session RPE 0 to 10 and segment RPE; HealthKit workout effort score 1 to 10; Endurain `workout_rpe` and `workout_feeling`; Hevy per-set RPE. Notes: Strava `description`, HealthKit metadata, Health Connect `notes`, Hevy per-exercise `notes`, intervals.icu description plus chat.

### 4.5 Sport taxonomies

Enumerations collected:

1. **Strava `SportType`**: 56 values (1.1). Legacy **`ActivityType`**: 37.
2. **FIT `sport`**: 82 values: generic, running, cycling, transition, fitness_equipment, swimming, basketball, soccer, tennis, american_football, training, walking, cross_country_skiing, alpine_skiing, snowboarding, rowing, mountaineering, hiking, multisport, paddling, flying, e_biking, motorcycling, boating, driving, golf, hang_gliding, horseback_riding, hunting, fishing, inline_skating, rock_climbing, sailing, ice_skating, sky_diving, snowshoeing, snowmobiling, stand_up_paddleboarding, surfing, wakeboarding, water_skiing, kayaking, rafting, windsurfing, kitesurfing, tactical, jumpmaster, boxing, floor_climbing, baseball, diving, shooting, winter_sport, grinding, hiit, video_gaming, racket, wheelchair_push_walk, wheelchair_push_run, meditation, para_sport, disc_golf, team_sport, cricket, rugby, hockey, lacrosse, volleyball, water_tubing, wakesurfing, water_sport, archery, mixed_martial_arts, motor_sports, snorkeling, dance, jump_rope, pool_apnea, mobility, geocaching, canoeing, all.
3. **FIT `sub_sport`**: 112 values. Examples: treadmill, street, trail, track, spin, indoor_cycling, road, mountain, downhill, cyclocross, hand_cycling, track_cycling, indoor_rowing, elliptical, stair_climbing, lap_swimming, open_water, flexibility_training, strength_training, cardio_training, yoga, pilates, indoor_running, gravel_cycling, e_bike_mountain, virtual_activity, obstacle, breathing, indoor_climbing, bouldering, hiit, amrap, emom, tabata, triathlon, duathlon, brick, swim_run, pickleball, padel, squash, badminton, racquetball, table_tennis, ultra, rucking, enduro, pool_triathlon, plus diving, flying and motor sub-sports.
4. **HealthKit `HKWorkoutActivityType`**: 84 cases (3 deprecated): americanFootball, archery, australianFootball, badminton, barre, baseball, basketball, bowling, boxing, cardioDance, climbing, cooldown, coreTraining, cricket, crossCountrySkiing, crossTraining, curling, cycling, discSports, downhillSkiing, elliptical, equestrianSports, fencing, fishing, fitnessGaming, flexibility, functionalStrengthTraining, golf, gymnastics, handCycling, handball, highIntensityIntervalTraining, hiking, hockey, hunting, jumpRope, kickboxing, lacrosse, martialArts, mindAndBody, mixedCardio, other, paddleSports, pickleball, pilates, play, preparationAndRecovery, racquetball, rowing, rugby, running, sailing, skatingSports, snowSports, snowboarding, soccer, socialDance, softball, squash, stairClimbing, stairs, stepTraining, surfingSports, swimBikeRun, swimming, tableTennis, taiChi, tennis, trackAndField, traditionalStrengthTraining, transition, underwaterDiving, volleyball, walking, waterFitness, waterPolo, waterSports, wheelchairRunPace, wheelchairWalkPace, wrestling, yoga; deprecated: dance, danceInspiredTraining, mixedMetabolicCardioTraining.
5. **Health Connect `EXERCISE_TYPE_*`**: 61 values: BADMINTON, BASEBALL, BASKETBALL, BIKING, BIKING_STATIONARY, BOOT_CAMP, BOXING, CALISTHENICS, CRICKET, DANCING, ELLIPTICAL, EXERCISE_CLASS, FENCING, FOOTBALL_AMERICAN, FOOTBALL_AUSTRALIAN, FRISBEE_DISC, GOLF, GUIDED_BREATHING, GYMNASTICS, HANDBALL, HIGH_INTENSITY_INTERVAL_TRAINING, HIKING, ICE_HOCKEY, ICE_SKATING, MARTIAL_ARTS, OTHER_WORKOUT, PADDLING, PARAGLIDING, PILATES, RACQUETBALL, ROCK_CLIMBING, ROLLER_HOCKEY, ROWING, ROWING_MACHINE, RUGBY, RUNNING, RUNNING_TREADMILL, SAILING, SCUBA_DIVING, SKATING, SKIING, SNOWBOARDING, SNOWSHOEING, SOCCER, SOFTBALL, SQUASH, STAIR_CLIMBING, STAIR_CLIMBING_MACHINE, STRENGTH_TRAINING, STRETCHING, SURFING, SWIMMING_OPEN_WATER, SWIMMING_POOL, TABLE_TENNIS, TENNIS, VOLLEYBALL, WALKING, WATER_POLO, WEIGHTLIFTING, WHEELCHAIR, YOGA.
6. **WHOOP v2 `sport_name`**: about 120 free strings (from this repo's `kinds/providers.substrate.reamde.dev/whoop/bundle.yaml`). They include non-exercise activities such as sauna, ice bath, cooking, cleaning, public speaking, dedicated parenting, babywearing, high stress work, musical performance and watching sports. `sport_id` was retired after 2025-09-01.
7. **intervals.icu activity type**: about 60 values (1.3).
8. **TCX `Sport_t`**: 3 values. **GPX**: free text.
9. **TrainingPeaks WorkoutType**: about 14 [recalled].
10. **Garmin Connect `activityType` keys**: about 100+ snake_case keys with a parent-type hierarchy (e.g. `running` > `trail_running`, `treadmill_running`, `track_running`, `indoor_running`, `virtual_run`, `obstacle_run`, `street_running`, `ultra_run`) [recalled, unverified count].

How they disagree:

| Concept | Strava | FIT | HealthKit | Health Connect | WHOOP |
| --- | --- | --- | --- | --- | --- |
| treadmill run | Run + `trainer` (VirtualRun for app runs) | running/treadmill | running + IndoorWorkout | RUNNING_TREADMILL | running |
| trail run | TrailRun | running/trail | running (no trail) | RUNNING (no trail) | running |
| indoor bike | Ride + trainer, or VirtualRide | cycling/indoor_cycling, or virtual_activity, or spin | cycling + indoor | BIKING_STATIONARY | cycling or spin |
| gravel / MTB / e-MTB | GravelRide / MountainBikeRide / EMountainBikeRide | cycling/gravel_cycling, mountain, e_bike_mountain (or sport e_biking) | cycling | BIKING | mountain biking |
| pool vs open water | Swim (no split) | swimming/lap_swimming vs open_water | swimming + swimmingLocationType | SWIMMING_POOL vs SWIMMING_OPEN_WATER | swimming |
| indoor rower | Rowing + trainer, VirtualRow | rowing/indoor_rowing | rowing + indoor | ROWING_MACHINE | rowing |
| strength | WeightTraining | training/strength_training | traditional- vs functionalStrengthTraining | STRENGTH_TRAINING vs WEIGHTLIFTING vs CALISTHENICS | weightlifting, powerlifting, functional fitness, strength trainer |
| HIIT | HighIntensityIntervalTraining | hiit (sport) or training/hiit, amrap, emom, tabata | highIntensityIntervalTraining | HIGH_INTENSITY_INTERVAL_TRAINING | hiit |
| triathlon | three activities | multisport + sessions | swimBikeRun + activities | none | triathlon |
| pickleball / padel | Pickleball, Padel | racket/pickleball, racket/padel | pickleball (no padel) | none | pickleball, padel |
| meditation / breathing | none | meditation, training/breathing | mindAndBody | GUIDED_BREATHING | meditation |
| wheelchair | Wheelchair | wheelchair_push_walk/run | wheelchairWalkPace/RunPace | WHEELCHAIR | wheelchair pushing |

Patterns:

- Strava encodes environment in the type (VirtualRide).
- FIT uses a two-level pair.
- HealthKit uses one coarse type plus metadata.
- Health Connect uses one flat list with a few environment splits.
- WHOOP uses an open list mixing exercise with life activities.

A canonical model should use **a sport hierarchy plus orthogonal facets**: environment (indoor, outdoor, virtual), surface or terrain, assisted (e-bike), venue (pool, open water). It should keep the **source's own code** verbatim beside the mapped canonical value. FIT's `sport`/`sub_sport` pair is the richest open base, but its licence (section 2.1) may make copying the enum tables into the repository questionable [unverified].

## 5. Training metrics catalogue

Formulas are standard literature, recalled. Where a platform's documentation was checked today it is marked [web].

| Metric | Inputs | Formula or reference | Computed by |
| --- | --- | --- | --- |
| **NP** (Normalized Power) | 1 Hz power | 30 s rolling mean of power; raise to the 4th power; mean; 4th root (Coggan, *Training and Racing with a Power Meter*) | TP, intervals.icu (`icu_weighted_avg_watts`), GC, WKO, Garmin (FIT `normalized_power`), Wahoo; Strava `weighted_average_watts` is an analogue |
| **IF** (Intensity Factor) | NP, FTP | IF = NP / FTP | TP, intervals.icu, GC, Garmin |
| **TSS** | duration s, NP, IF, FTP | TSS = (s × NP × IF) / (FTP × 3600) × 100 = hours × IF² × 100 [web: TrainingPeaks] | TP, intervals.icu (power load), GC (as BikeStress), Garmin FIT field, Wahoo |
| **VI** (Variability Index) | NP, average power | VI = NP / AP | TP, intervals.icu, GC |
| **EF** (Efficiency Factor) | NP (or NGP), average HR | EF = NP / avgHR (run: NGP speed / avgHR) | TP, intervals.icu (`icu_efficiency_factor`), GC |
| **Aerobic decoupling (Pa:Hr, Pw:Hr)** | power or pace and HR over halves | (EF first half − EF second half) / EF first half | TP, intervals.icu (`decoupling`), GC |
| **rTSS** | NGP, FTPa (threshold pace) | hours × (NGP / threshold pace as speed)² × 100 | TP, intervals.icu (pace load) |
| **NGP / GAP** | pace, grade | Minetti et al. 2002 energy cost of running on gradients (TP NGP; Strava's pre-2017 GAP). Strava since 2017 uses a model fit on HR across millions of runs, giving more downhill credit [web, secondary: [Strava eng](https://medium.com/strava-engineering/an-improved-gap-model-8b07ae8886c3), [reverse engineering](https://aaron-schroeder.github.io/reverse-engineering/grade-adjusted-pace.html)] | Strava (GAP), TP (NGP), intervals.icu (`gap`, `gap_model` selectable), Garmin (FIT `avg_grade_adjusted_speed`), Apple [unverified] |
| **sTSS** | NSS (normalised swim speed), threshold swim pace | hours × IF³ × 100 (cubed, because drag rises with the cube of speed) | TP |
| **hrTSS** | HR stream, LTHR | time-in-HR-zone weighted, normalised so one hour at LTHR = 100; TP's exact weights are proprietary [unverified] | TP; intervals.icu "HRSS" via TRIMP scaled to LTHR [recalled] |
| **tTSS** | duration, average HR | TRIMP-based estimate [web: TP help] | TP |
| **TRIMP (Banister)** | minutes, HRrest, HRmax, avg HR, sex | TRIMP = t × HRr × 0.64 e^(1.92 HRr) (men) or 0.86 e^(1.67 HRr) (women), HRr = (HRavg − HRrest)/(HRmax − HRrest) (Banister 1991; Morton, Fitz-Clarke, Banister 1990) | intervals.icu (`trimp`), GC, Firstbeat-derived tools; Strava Relative Effort is HR-zone weighted, proprietary |
| **Edwards TRIMP** | minutes in 5 HR zones (50-60 % … 90-100 % HRmax) | Σ minutes_z × z (z = 1..5) (Edwards 1993) | GC, many coaching tools |
| **Lucia TRIMP** | minutes in 3 zones split by VT1/VT2 | Σ minutes × (1, 2, 3) (Lucia et al. 2003) | GC, research |
| **iTRIMP** | individual HR-lactate curve | per-athlete exponential weighting (Manzi et al. 2009) | research, GC [recalled] |
| **Session RPE (sRPE)** | CR-10 RPE, minutes | load = RPE × minutes (Foster et al. 2001) | intervals.icu (`session_rpe`), coaching apps; universal fallback for strength |
| **Monotony and strain** | daily loads over 7 days | monotony = mean/SD; strain = weekly load × monotony (Foster 1998) | GC, TrainingPeaks [unverified], coaching tools |
| **PMC: CTL, ATL, TSB** | daily load series | CTL_t = CTL_{t−1} + (L_t − CTL_{t−1})/42; ATL the same with 7; TSB = CTL − ATL (yesterday's values in TP). Exponential variant uses k = 1 − e^(−1/τ). Fitness-fatigue model after Banister 1975 (performance = p0 + k1·fitness − k2·fatigue, τ1 ≈ 42, τ2 ≈ 7). The defaults 42 and 7 are TP's and intervals.icu's [web: intervals.icu forum, 42 and 7] | TP (PMC), intervals.icu (Fitness, Fatigue, Form; `rampRate`), GC, Strava Fitness & Freshness (on Relative Effort), Garmin Acute Load and Training Status (EPOC basis) |
| **Ramp rate** | CTL | ΔCTL per week | TP, intervals.icu |
| **ACWR** | acute (7 d) and chronic (28 d) loads | ratio, rolling or EWMA (Gabbett 2016; criticised by Impellizzeri et al. 2020) | team-sport tools, Garmin "load ratio" [unverified] |
| **MMP / power curve** | power stream | max mean power for every duration 1 s … N h, per activity and aggregated per period | intervals.icu, GC, TP (Peak Power), Strava (Power Curve, subscription), WKO |
| **Critical Power, W'** | MMP points 2-20 min | 2-parameter: P = W'/t + CP (Monod & Scherrer 1965); 3-parameter adds Pmax (Morton 1996); others are extended CP, Peronnet-Thibault, Ward-Smith | GC (many models), intervals.icu (`icu_pm_cp`, `icu_pm_w_prime`, `icu_pm_p_max`, Morton 3P [recalled]), WKO (proprietary PD model with mFTP, FRC, Pmax, TTE) |
| **W'bal** | power stream, CP, W' | Skiba 2012 integral (τ = 546 e^(−0.01 D_CP) + 316) or differential form (Skiba 2015) | GC, intervals.icu (`wbal_start`, `wbal_end`, `icu_max_wbal_depletion`), Garmin [unverified] |
| **FTP** | test or model | 95 % of 20 min power (Allen and Coggan); 60 min power; ramp-test 75 % of 1 min peak; model eFTP from the power curve | all; intervals.icu `icu_pm_ftp`, `icu_rolling_ftp`; Garmin auto FTP; Zwift; Apple stores `cyclingFunctionalThresholdPower` |
| **HR zones** | HRmax, HRrest, LTHR | %HRmax (Garmin default 50/60/70/80/90 %); **Karvonen** %HRR: target = HRrest + p × (HRmax − HRrest) (Karvonen 1957); **Friel** %LTHR run: Z1 < 85, Z2 85-89, Z3 90-94, Z4 95-99, Z5a 100-102, Z5b 103-106, Z5c > 106; bike: Z1 < 81, Z2 81-89, Z3 90-93, Z4 94-99, Z5a 100-102, Z5b 103-106, Z5c > 106; **Coggan** %LTHR: < 68, 69-83, 84-94, 95-105, > 106; Seiler 3-zone (VT1/VT2) | TP (Friel, Coggan, Karvonen, custom), intervals.icu (configurable), Garmin (%max, %HRR, %LTHR: the same three as FIT `hr_zone_calc`), Strava, Polar, Apple (iOS 17+ zones; iOS 27 HKWorkoutZoneGroup) |
| **Power zones** | FTP | **Coggan 7**: Active Recovery < 55 %, Endurance 56-75, Tempo 76-90, Threshold 91-105, VO2max 106-120, Anaerobic 121-150, Neuromuscular > 150. Also iLevels (WKO), Zwift 6 zones, intervals.icu custom plus "sweet spot" 84-97 % | TP, intervals.icu, GC, Garmin, Zwift, Wahoo (`/v1/power_zones`) |
| **Pace zones** | threshold pace | Friel run pace zones; Daniels E/M/T/I/R paces from VDOT | TP, intervals.icu, Runna, Garmin |
| **Time in zone / polarisation** | zone times | distribution; Polarization Index (Treff et al. 2019) = log10(Z1/Z2 × Z3 × 100) | intervals.icu (`polarization_index`), GC, TP |
| **VO2max estimates** | various | Uth-Sørensen VO2max ≈ 15.3 × HRmax/HRrest; Cooper 12-min; Daniels VDOT from race times; Firstbeat (HR vs speed or power in steady segments); Apple cardio fitness (walk and run HR response) | Garmin, Apple (`vo2Max`), Polar Fitness Test, Runna and Strava race predictions, intervals.icu `vo2max` wellness field |
| **Running power** | speed, grade, body mass, accelerometer, wind | vendor models: Stryd (footpod), Garmin native, Apple `runningPower`, COROS, Polar. Not interchangeable (Garmin native read about 20 % above Stryd in early comparisons [recalled, unverified]) | Stryd, Garmin, Apple, COROS, Polar, Suunto |
| **Cadence** | steps or revolutions | run spm (FIT stores strides per minute + fractional); bike rpm; swim strokes per minute | all |
| **Vertical oscillation, GCT, GCT balance, vertical ratio, step length** | HRM-Pro, RD pod or wrist | VO (mm), GCT (ms), balance (% L), ratio = VO / step length (%) | Garmin, Apple (VO, GCT, stride length), COROS, Stryd, intervals.icu averages |
| **SWOLF** | length time, strokes | seconds + strokes per length | Garmin (`avg_swolf`), Apple [unverified], FORM, intervals.icu |
| **Training Effect** | HR, EPOC | Firstbeat aerobic and anaerobic 0-5 | Garmin, Suunto, COROS (own version) |
| **Strain (WHOOP)** | HR | 0-21 logarithmic, proprietary | WHOOP; intervals.icu `strain_score` is a separate, own metric [recalled] |
| **e1RM** | load, reps | Epley: w × (1 + r/30); Brzycki: w × 36/(37 − r); RPE-adjusted e1RM via an RPE/RIR table (Tuchscherer; Zourdos et al. 2016; Helms et al. 2016) | Hevy, Strong, Fitbod, wger [unverified], intervals.icu no |
| **Volume load / tonnage** | sets, reps, load | Σ reps × load per exercise, per muscle group (credit secondary muscles at 0.5 is a common convention) | Hevy, Strong, Fitbod, intervals.icu (`kg_lifted`) |
| **Hard sets per muscle per week** | sets with RPE ≥ 7 or RIR ≤ 3 | count | Hevy (sets per muscle), RP Hypertrophy, Fitbod |

## 6. How consumer apps use the data

These are the queries a canonical store must answer. Each names the access pattern it implies.

1. **Weekly and monthly volume per sport**: sum of distance, moving time, elevation and load grouped by ISO week and sport (Strava Training Log and profile stats, Garmin reports, intervals.icu weekly totals, HealthKit `HKStatisticsCollectionQuery` with weekly anchors). *Pattern*: group-by over session summaries (thousands of rows per user), by time bucket and sport hierarchy, in the user's local time zone.
2. **Time in zone distribution**: Σ per-session zone times per week, per sport, with the zone scheme of the day (intervals.icu, TP, Garmin, Apple iOS 27). *Pattern*: sum of an array-valued summary; needs the zone times **stored per session**, not recomputed from streams at query time.
3. **Fitness vs fatigue (PMC)**: a daily series of load, then an EWMA recursion over days including zero-load days, plus a projection over planned workouts (intervals.icu projects CTL forward from `Event.icu_training_load`). *Pattern*: dense daily time series from a sparse event table, then a windowed recursive computation. SQL window functions alone do not express the recursion cleanly; it is computed incrementally in application code everywhere.
4. **PRs and best efforts**: fastest 1 km, 5 km, 10 km, half and full marathon (Strava best efforts, Garmin PRs); best 5 s, 1 min, 5 min, 20 min and 60 min power (MMP); longest ride; biggest climb; heaviest lift and best e1RM per exercise (Hevy PRs: heaviest weight, best 1RM, best set volume, best session volume). *Pattern*: per-activity derived maxima stored on the activity (MMP curve as an array, best efforts as a map), then max-over-period queries. Computing them needs a sliding window over the stream: once at ingest, not at query time.
5. **Progressive overload (strength)**: per exercise over time: top set load × reps, e1RM trend, weekly volume load, sets per muscle group per week, "last time you did X" (Hevy exercise history endpoint, Fitbod). *Pattern*: filter sets by exercise reference, order by time, group by week; a muscle-group roll-up joins through the exercise taxonomy.
6. **Race readiness and predictions**: TSB on race day, Garmin race predictor and Training Readiness, Runna and Strava predictions, Daniels VDOT equivalent times. *Pattern*: the latest values of derived series plus best recent efforts.
7. **Compliance with plan**: planned vs executed load, duration and distance per day and week; per-step compliance (intervals.icu `compliance`, TP compliance colours, Garmin workout completion). *Pattern*: join plan ↔ execution by reference, aggregate the differences.
8. **Gear mileage and replacement**: Σ distance per gear since purchase, with reminders at thresholds (Strava, intervals.icu reminders, Garmin). *Pattern*: group-by over a reference.
9. **Trends of efficiency**: EF, decoupling and HR at a given pace or power over months (intervals.icu power/HR charts, Garmin Training Status). *Pattern*: a per-activity scalar time series with smoothing.
10. **Map and heatmap**: all routes in a bounding box (Strava personal heatmap, FitTrackee, Endurain). *Pattern*: spatial index over route geometry, or precomputed tiles.
11. **Activity detail**: charts of streams with laps and intervals overlaid, zoomed by range (every app). *Pattern*: read one activity's streams in full, slice by index range. A columnar blob per activity is ideal.
12. **Cross-source dedup**: the same run arrives from Garmin, Strava, HealthKit and intervals.icu. Apps match on start time ± tolerance plus sport plus duration (intervals.icu, RunGap, HealthFit) and pick a preferred source per field. *Pattern*: a time-range overlap query, which substrate's `temporal(range)` trait already indexes.

## 7. Implications for substrate (input to the proposal phase)

These are observations, not decisions.

- **Two storage shapes**:
  - **Summary records**: session, lap, set, planned workout, step, gear, zone settings. There are thousands per user-year, and they fit the records table and its filters.
  - **Sample streams**: millions per user-year. These belong in a columnar `blobref` per activity (FIT original plus a decoded column set), with per-activity derived summaries (MMP curve, best efforts, zone times, NP) written as properties at ingest.
  - Every platform studied stores streams this way; none stores one row per sample.
- **Keep the FIT original** as a blob. FIT is the lossless interchange format (developer fields, HRV, running dynamics); TCX and GPX are lossy subsets.
- **Store the context of the day** (FTP, LTHR, threshold pace, zone boundaries, body mass) on each session, as intervals.icu and FIT `time_in_zone` do. Later threshold edits must not rewrite history.
- **Plans and executions are different kinds** linked by a reference. Step-level linkage follows FIT's `wkt_step_index` pattern.
- **Sport**: keep the upstream code verbatim on the mirror and map to a canonical hierarchy plus facets in the projected layer, the same mirror-then-project split substrate already uses.
- **Aggregations needed**: time-bucket group-by with sum, max and count in local time; EWMA over a dense daily series; max over period; group-by reference (gear, exercise, muscle group). The current API has none of these (CONTEXT.md).
- **Licence gates**:
  - Strava API data cannot be persisted, indexed, aggregated or exposed to agents (2026 policy). Only the user's bulk export is safe to import.
  - The Garmin FIT SDK should not be vendored; use an independent MIT decoder.
  - wger exercise data is CC-BY-SA (attribution and share-alike on the derived database); free-exercise-db is public domain, the easiest seed; ExerciseDB is AGPL and non-commercial.
  - Hevy's API is explicitly unstable.

## Sources

Primary documents checked on 2026-10-08:

- Strava: [API Agreement 2026](https://www.strava.com/legal/api), [API Policy 2026](https://www.strava.com/legal/api_policy), [API reference](https://developers.strava.com/docs/reference/), [SportType swagger](https://developers.strava.com/swagger/sport_type.json), [ActivityType swagger](https://developers.strava.com/swagger/activity_type.json), [rate limits](https://developers.strava.com/docs/rate-limits/), [webhooks](https://developers.strava.com/docs/webhooks/)
- Garmin: [Activity API](https://developer.garmin.com/gc-developer-program/activity-api/), [Training API](https://developer.garmin.com/gc-developer-program/training-api/), [FIT JavaScript SDK 21.218.0 incl. LICENSE.txt and profile.js](https://github.com/garmin/fit-javascript-sdk), [TCX v2 XSD](https://www8.garmin.com/xmlschemas/TrainingCenterDatabasev2.xsd), [ActivityExtension v2 XSD](https://www8.garmin.com/xmlschemas/ActivityExtensionv2.xsd), [TrackPointExtension v2 XSD](https://www8.garmin.com/xmlschemas/TrackPointExtensionv2.xsd)
- Garmin news: [the5krunner on the API freeze](https://the5krunner.com/2026/09/14/garmin-developer-api-access-paused/), [the5krunner on Connect+ and strength](https://the5krunner.com/2026/03/24/garmin-connect-plus-strength-apps/), [Garmin acquires TrainingPeaks and TrainHeroic](https://investors.garmin.com/events-and-presentations/news/news-details/2026/Garmin-acquires-TrainingPeaks-and-TrainHeroic-leading-endurance-and-strength-training-platforms-for-athletes-and-coaches/default.aspx), [Sahha on the paused program](https://sahha.ai/blog/garmin-developer-program-paused/)
- TrainingPeaks: [Partner API update](https://www.trainingpeaks.com/blog/an-update-on-trainingpeaks-partner-api/), [TSS article](https://www.trainingpeaks.com/learn/articles/estimating-training-stress-score-tss/), [TSS troubleshooting](https://help.trainingpeaks.com/hc/en-us/articles/204071844-TSS-calculation-troubleshooting)
- intervals.icu: [OpenAPI](https://intervals.icu/api/v1/docs), [API access forum post](https://forum.intervals.icu/t/api-access-to-intervals-icu/609), [fitness chart](https://www.intervals.icu/features/fitness-chart/)
- Apple: [HKWorkoutActivityType](https://developer.apple.com/documentation/healthkit/hkworkoutactivitytype), [HKWorkout](https://developer.apple.com/documentation/healthkit/hkworkout), [HKWorkoutEventType](https://developer.apple.com/documentation/healthkit/hkworkouteventtype), [HKWorkoutZoneGroup](https://developer.apple.com/documentation/healthkit/hkworkoutzonegroup), [WorkoutKit](https://developer.apple.com/documentation/workoutkit), [HKQuantityTypeIdentifier](https://developer.apple.com/documentation/healthkit/hkquantitytypeidentifier)
- Health Connect: [androidx records source](https://github.com/androidx/androidx/tree/androidx-main/health/connect/connect-client/src/main/java/androidx/health/connect/client/records)
- Strength and rowing APIs: [Hevy API](https://api.hevyapp.com/docs/), [Concept2 Logbook API](https://log.concept2.com/developers/documentation/), [Wahoo Cloud API](https://cloud-api.wahooligan.com/)
- Exercise databases: [wger API](https://wger.de/api/v2/), [free-exercise-db](https://github.com/yuhonas/free-exercise-db), [ExerciseDB OSS API](https://oss.exercisedb.dev/docs), [ExerciseDB repo](https://github.com/ExerciseDB/exercisedb-api)
- Open source platforms: [Endurain](https://github.com/endurain-project/endurain), [GoldenCheetah](https://github.com/GoldenCheetah/GoldenCheetah), [FitTrackee](https://github.com/SamR1/FitTrackee), [OpenTracks](https://codeberg.org/OpenTracksApp/OpenTracks), [FitoTrack](https://codeberg.org/jannis/FitoTrack)
- Secondary: [Peloton API notes](https://github.com/geudrik/peloton-client-library/blob/master/API_DOCS.md), [Zwift third-party platforms](https://support.zwift.com/en_us/zwift-and-third-party-platforms-SypU0LdVr), [Strava acquires Runna](https://www.dcrainmaker.com/2025/04/strava-acquires-runna-thoughts-forward.html), [Strong CSV guide](https://thetaperapp.com/articles/how-to-export-strong-data/), [Strava GAP model](https://medium.com/strava-engineering/an-improved-gap-model-8b07ae8886c3), [GAP reverse engineering](https://aaron-schroeder.github.io/reverse-engineering/grade-adjusted-pace.html)
- In-repo: `kinds/providers.substrate.reamde.dev/whoop/bundle.yaml` (the WHOOP v2 sport list)
