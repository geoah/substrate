# Formats and standards for strength workouts

> **Where this came from.** Written on 2026-10-30 in the owner's private repository, as `research/workouts/`, for
> the app then called Spotter (Substrate Workouts now); the version here is the one the
> app's second build used (the private repository's #118). It was carried into this repository on
> 2026-11-02 as written, with two changes. The links resolve inside this repository. The
> draft kind paths `mneme.example.org/training/<kind>` now read as the app-owned
> `~/training/<kind>` they became (decision record [0203](../../../../docs/decisions/0203-an-app-keeps-its-own-package-under-a-declare-scope.md)). Where the
> text names the package home it recommended, a Mneme package `mneme.example.org/training`,
> that name is left as written: the home changed later, and the reasons are in
> [decisions.md](../decisions.md). Mentions of Mneme, private repository paths or
> `apps/apple/spotter` describe the plan at the time. The package now lives in
> [`Package/`](../../package/README.md), and the app's spec is [DESIGN.md](../../DESIGN.md).

Research slice 01. Date: 2026-10-30.

The question: is there a common, well-known format for storing workouts,
especially strength training with sets, that Spotter can reuse or align with,
so that data from other providers can be imported later and Spotter's data
can be exported?

Evidence markers:

- **[web]** checked today on the web or in a primary artefact: a published
  specification, an OpenAPI document, a library's source on GitHub or Google
  Maven, a terminology server.
- **[sdk]** checked today in the installed Xcode 27.2 SDKs (iOS 27.2,
  watchOS 27.2): headers and `.swiftinterface` files.
- **[src]** checked today in source on this machine: substrate at production's
  commit (exported at `/tmp/spotter-substrate-main`), its git history, this
  worktree.
- **[recalled]** from memory, not re-checked today.
- **[unverified]** I could not confirm it, or only a secondary source says so.

This builds on the substrate research slice
`/tmp/spotter-substrate-main/docs/plans/health-data/02-workouts.md`
(2026-10-08), cited below as "02-workouts §n", and on
`04-aggregators-and-models.md` §2.4 and §2.5 in the same directory for Open
mHealth and FHIR. I re-checked the parts this document leans on. Section 1
lists what changed or was wrong.

## 0. The answer

**There is no common format for strength sets.** None of the standards
evaluated here, and no format used by more than one vendor, carries all of a
strength session down to the set: exercise, order, set type, reps, load,
effort, rest and grouping together. FIT and Health Connect carry parts of it
at the set (§3.3, §3.4).

- **Health standards stop at the session.** FHIR core has no exercise
  resource. The HL7 Physical Activity IG records "exercise vital signs" (days
  per week, minutes) and session measures. The HL7 Personal Health Records IG
  (ballot) models a workout as an Observation with members, and its exercise
  codes are copied from HealthKit and Health Connect. LOINC and SNOMED CT have
  no code for a set, a rep count or a lifted load. IEEE 1752.1 has one
  physical-activity episode with a movement count and no load [web].
- **Apple has nothing below the workout.** HealthKit in the iOS 27.2 SDK has
  no set, rep or load type, and WorkoutKit goals cannot say "8 reps" or
  "60 kg" [sdk].
- **Set-level data exists only in vendor formats:** Garmin FIT (`set` and
  `workout_step`), Health Connect (`ExerciseSegment`, whose weight, set index
  and RPE are still alpha), app formats (the Hevy API and CSV, the Strong CSV,
  wger) and one aggregator's normalised model (Terra `strength_data`) [web].
- **Open "workout format" projects exist but nobody uses them.** openweight,
  OpenSet, WODIS and PWF are small JSON drafts, each in one repository with
  0 to 7 GitHub stars. Liftoscript is a real, used language, but only for Liftosaur's
  programs [web].

**The verdict.** Spotter owns its model and aligns with the others field by
field.

- **Basis of the internal model:** Hevy's structure, because it is the app
  structure the owner already uses and the one the most importers accept. A
  session holds exercise entries, which carry a superset group, and each entry
  holds typed sets. Add what Hevy lacks and others have:
  - set start and end times (FIT, Health Connect);
  - load as a quantity with its unit, plus a load type (Terra);
  - RIR beside RPE (wger, Terra);
  - planned rest and actual rest (wger, Terra);
  - the plan's targets copied onto each executed set (wger, openweight);
  - a `codes` list per exercise, following substrate's metric precedent.
- **Import targets:** Hevy (API and CSV), Strong CSV, FIT activity files and
  wger. Fitbod and FitNotes CSVs are lossy imports. Apple Health workouts are
  only for matching sessions.
- **Export targets:** Strong CSV (the de facto import format), the Hevy API,
  later a FIT activity file with `set` messages, and optionally a FHIR Bundle.
- **Apple Health:** one `HKWorkout` per session, of type
  `traditionalStrengthTraining`. It carries Spotter's session id in
  `HKMetadataKeyExternalUUID` and `HKMetadataKeySyncIdentifier`, and a session
  effort score related to the workout. It has no per-exercise detail.

Section 8 gives the full verdict and section 9 the field mapping.

## 1. What changed since 02-workouts

- **Health Connect segment fields have shipped, but only in alpha.**
  `ExerciseSegment.weight`, `setIndex` and `rateOfPerceivedExertion`, and
  `ExerciseSessionRecord.rateOfPerceivedExertion`, arrived in androidx
  `connect-client` 1.2.0-alpha03 (2026-03-25). They are still alpha in
  1.2.0-alpha06 (2026-08-26), the latest release. The stable 1.1.0 has only
  `repetitions`. The fields sit behind `FEATURE_EXERCISE_SESSION_IMPROVEMENTS`
  and platform SDK extension 21 on Android 14 and later [web]. 02-workouts §1.8
  had them as "[unverified as shipped]".
- **Hevy workouts link to their routine.** A Hevy workout now carries
  `routine_id`, "The ID of the routine that this workout belongs to" [web].
  02-workouts §4.3 found no link field.
- **wger logs RiR, not RPE.** wger stores reps in reserve (0 to 4.5 in steps of
  0.5) as `rir` and `rir_target`, and has no RPE field [web].
  02-workouts §1.11 said "RIR and RPE".
- **Counts:**
  - wger serves 904 exercises today (the `exerciseinfo` and `exercise`
    counts); 02-workouts had 919 [web].
  - Health Connect has exactly 68 segment types, counting unknown, rest and
    pause. A strength session accepts 42 "exercise segments" plus the 5
    universal ones, 47 in all [web].
  - I count 121 `HKQuantityTypeIdentifier` declarations in the iOS 27.2 SDK,
    where 02-workouts §1.7 said 132 symbols. The counting differs, but the
    conclusion holds: none is for sets, reps or load [sdk].
- **Not covered before:**
  - the HL7 Personal Health Records IG and its workout profile (2.3);
  - Terra's normalised `strength_data` (4.7);
  - the open workout-format drafts (5);
  - `HKWorkoutSession` on iPhone from iOS 26 (3.1).

## 2. Health standards

### 2.1 FHIR R4 and R5 core

- **There is no exercise or workout resource.** The R5 resource list was
  checked today and has none [web]. R4 has none either [recalled].
- **An exercise record is an Observation.** The `observation-category` code
  `activity` means "Observations that measure or record any bodily activity
  that enhances or maintains physical fitness and overall health and
  wellness" [web].
  - Grouping uses `hasMember`; multi-part values use `component` [web].
  - R5 adds `triggeredBy`, `instantiates[x]` (an ObservationDefinition) and
    `bodyStructure` [web].
  - `basedOn` can name a CarePlan, a ServiceRequest and other request
    resources [web].
- **Plans have no set structure.** CarePlan activities, ServiceRequest (which
  the Physical Activity IG uses for exercise prescriptions) and
  PlanDefinition/ActivityDefinition have none. The closest is `Timing` (count,
  duration, frequency) [recalled].
- **FHIR chat favours one Observation per queried assertion.** In a FHIR chat
  thread on rehabilitation exercise, an implementer proposed one Observation
  per exercise and called one per rep (exercises × sets × reps a day)
  "overkill". A FHIR core author answered with one Observation per rep,
  grouped by `hasMember` under one Observation per set: "Every separate
  assertion that would reasonably be queried or analyzed independently should
  be a separate Observation". `component` is for values almost always read
  together, such as systolic and diastolic [web].
- **Conclusion:** nothing in FHIR carries a set, rep or load structure. Any
  FHIR export of sets uses local codes (8.4).

### 2.2 HL7 Physical Activity IG

**Status.** `hl7.fhir.us.physical-activity` 1.0.0, STU 1, trial use, maturity
2, on FHIR 4.0.1. It was published 2023-08-25, after a 2023-03-28 ballot, and
is still the current version [web].

**Scope.** It "standardizes interoperability expectations for systems involved
in measuring, reporting, and intervening" on a patient's physical activity. It
focuses on "what the patient is currently doing/planning to do" [web].

**Profiles** [web]:

- **Exercise vital sign (EVS):** `PAObservationEVS`, with variants for days
  per week, minutes per day and minutes per week, plus
  `PAObservationStrengthDaysPerWeek`.
- **Supporting observations:**
  - `PAObservationActivityMeasure`: "measures that are specific to a single
    physical activity";
  - `PAObservationActivityGroup`: code `PAPanel`, no value, `hasMember` of at
    least one activity measure;
  - `PAObservationTimeMeasure`: daily totals.
- **Care and workflow:** `PACarePlan`, `PAGoal`, `PAServiceRequest`,
  `PATaskForReferralManagement`, `PAConditionLowPA`, `PADiagnosticReport`.

**Codes** [web]:

- **EVS (LOINC):**
  - 89555-7, days per week of moderate to strenuous activity;
  - 68516-4, minutes per day on those days;
  - 82290-8, frequency of moderate to vigorous aerobic activity;
  - 82291-6, "Frequency of muscle-strengthening physical activity", in
    `d/wk`.
- **Activity measures (LOINC):**
  - 73985-4 "Exercise activity", whose value is a LOINC answer. Answer list
    LL734-5 includes LA11839-0 "Weights". Answer list LL6247-2 includes
    LA33433-6 "Weight-lifting", LA33437-7 "Leg Press" and LA33434-4 "Leg
    Extensions".
  - 55411-3 "Exercise duration";
  - 77592-4 and 77593-2, moderate and vigorous activity (IPAQ);
  - pedometer heart rate and calories.
- **Time measures (LOINC):** 41950-7 steps per 24 h, 8873-2, 40443-4, 41979-6.
- **IG temporary code system:** includes `PhysicalActivity`, `PAPanel` and
  `Experience`.

**Strength.** The IG counts strength days per week and goes no further. It has
no exercise identity, sets, reps or load.

### 2.3 HL7 Personal Health Records IG (ballot) and other drafts

**Status.** `hl7.fhir.uv.phr` 1.0.0-ballot2, from HL7 Patient Empowerment, an
STU 1 ballot at maturity 1, on FHIR 4.0.1. The CI build is dated 2026-09-17
[web]. Its own description: "Guidance on how to create longitudinal personal
health records using FHIR".

**The PGHD Workout profile** [web] is an Observation with:

- one category from `observation-category` (the example uses `activity`);
- a required code slice bound to `observation-workout-codes`, plus an optional
  LOINC slice;
- `subject`, `effective[x]` (dateTime or Period), `device` and `hasMember`.

**Its example** is a walking workout from an Apple Watch [web]:

- `effectivePeriod` holds the start and end;
- the identifier is the HealthKit UUID;
- `hasMember` points to a distance Observation and an active energy
  Observation.

**The workout codes** [web]: `observation-workout-codes` takes 159 concepts
from `observation-pghd-codes`. They are HealthKit's activity types in
camelCase (`traditionalStrengthTraining`, `functionalStrengthTraining`) plus
Health Connect's exercise and segment types (`benchPress`, `deadlift`,
`latPullDown`, `squat`, `rest`, `pause`). So the only "standard" exercise codes
in FHIR today are Health Connect's segment list, renamed.

**It has no reps, load or set structure.** Ballot content may change.

**Two other drafts** model no sets [web]:

- MITRE's Standard Patient Health Record IG 0.4.4, a 2023 CI build, has a
  logical model of HKWorkout (duration, activity type, activities, events).
- Finland's Kanta PHR fitness profiles (0.1.0, draft) define an abstract
  fitness Observation with an "Exercise Tracking" child.

### 2.4 LOINC, SNOMED CT and UCUM

- **LOINC (2.83).** I searched tx.fhir.org for "repetitions", "sets",
  "weight lifted", "resistance training" and "one repetition maximum". It has
  nothing for a set, an exercise rep count or a lifted load [web]. The useful
  codes are:
  - 73985-4 Exercise activity;
  - 55411-3 Exercise duration;
  - 82289-0 "Rating of perceived exertion [Score]";
  - 74008-4 Exercise intensity;
  - 82291-6 muscle-strengthening frequency.
- **SNOMED CT.** It has no named lifts and no set concept [web]. The closest
  concepts are:
  - 61686008 "Physical exercise";
  - 386291006 "Exercise promotion: strength training", a nursing
    intervention;
  - 736786002 "Provision of advice about muscle strengthening exercise".
- **UCUM.** `kg`, `[lb_av]`, `s`, `min` and `m` cover the measures. A rep
  count is a unity with an annotation, such as `{reps}` [recalled]. Substrate's
  units already carry UCUM codes (`[lb_av]` for `lb`, `docs/units.md`) [src].

### 2.5 Substrate's FHIR-shaped catalogue as precedent

- **Decision 0193** (proposed 2026-10-28) [src] makes `clinical` one kind per
  FHIR R4 resource. Each kind keeps the resource's meaning, the property names
  a FHIR mapper expects and the value sets. Enum values drop their hyphens
  (`enteredinerror`). A code is one `coding` object (system, version, code,
  display). The upstream id is the record id, and `revised` with `revision`
  makes a re-export rewrite in place (`docs/clinical.md`). The reason given:
  "FHIR is the shape every clinical export already takes, so a source maps
  onto it field by field."
- **For strength, that reason points elsewhere.** No export takes a FHIR shape
  (2.1 to 2.4). The shape strength exports share is Hevy's and FIT's. The
  decision's mechanics still transfer: upstream ids as record ids, `revision`,
  one coding object, unit-bearing quantities.
- **Precedents already in substrate** [src]:
  - `health/workout` is a session summary. Its `sport` enum includes
    `strength`, and it has `sportSource`, `duration`, `activeEnergy`,
    `totalEnergy`, heart rate, `zoneDurations`, `notes`, `origin` and a `file`
    blobref (`kinds/substrate.reamde.dev/health/workout.yaml`).
  - `observations/metric` has `codes`, a repeated `{system, code, version,
    display}`. `system` is a URI where one exists and a short word otherwise
    (`healthkit`, `healthconnect`, `garmin`). For example, heart rate lists
    LOINC 8867-4, `HKQuantityTypeIdentifierHeartRate` and `HeartRateRecord`
    (`kinds/substrate.reamde.dev/observations/metric.yaml`,
    `health/metrics.yaml`).
  - The `applehealth` provider maps "traditional strength training" and
    "functional strength training" to `strength`. It keeps the workout's
    HealthKit metadata as JSON
    (`kinds/providers.substrate.reamde.dev/applehealth/bundle.yaml`).
  - The deleted `samples/fitness` package (removed in `f47c21f5`,
    2026-09-10) had `exercise` and `workoutset`. `workoutset` held a
    `weightKg` float and a `setType` enum (normal, warmup, dropset, amrap,
    failure). Issue #125 had flagged that a weight with no unit is ambiguous
    (`06-substrate-fit.md`).

### 2.6 Open mHealth and IEEE 1752.1

- **Status.** The `omh` schemas are deprecated in favour of IEEE 1752.1
  schemas of the same name (04-aggregators §2.4) [web, per prior slice].
- **IEEE 1752.1 `physical-activity`** [web]:
  - required fields: `activity_name` (free text; the schema recommends the
    CDC guidelines or the Compendium of Physical Activities) and
    `effective_time_frame` (a time interval);
  - `base_movement_quantity`, with unit steps, laps, skips, movements or
    strokes;
  - `distance`, `cumulative_elevation_gain` and `duration`;
  - `duration_light_activity`, `duration_moderate_activity` and
    `duration_vigorous_activity`;
  - `average_cadence`, `average_speed`, `kcal_burned` and `met_value`;
  - `reported_activity_intensity` (light, moderate, vigorous);
  - `descriptive_statistic` with its denominator.
- **Strength.** One data point per set is possible, with reps as
  "movements". There is no load, set type or grouping. The OMH repository also
  holds a `granola/hk-workout` schema, a HealthKit mirror [web]. Neither fits
  strength.

### 2.7 schema.org

- **`ExercisePlan`** (a CreativeWork and a PhysicalActivity) has
  `activityDuration`, `activityFrequency`, `additionalVariable` (text such as
  "exercise order, repetitions, distance, or progression"), `exerciseType`
  (text), `intensity`, `repetitions`, `restPeriods` and `workload` (energy)
  [web].
- **`ExerciseAction`** has `exerciseType`, `exercisePlan`, `distance`,
  `exerciseCourse`, `opponent` and sports properties. It has no sets, reps or
  load [web].
- **Use.** Web markup for a published routine, not interchange.

## 3. Platforms

### 3.1 Apple HealthKit (iOS 27.2 SDK)

**No set, rep or load type** [sdk]:

- `HKTypeIdentifiers.h` declares 121 quantity type identifiers, and none is
  for sets, reps or lifted load.
- "repetition" does not occur in any HealthKit header.
- The watchOS 27.2 SDK adds nothing for strength.
- iOS 27 added `HKQuantityTypeIdentifierHeartRateVariabilityRMSSD`, workout
  zones and menopause categories. iOS 26 added medications.

**Strength activity types** (`HKWorkoutActivityType`) [sdk]:

- `traditionalStrengthTraining` (50): "Primarily machines and/or free
  weights";
- `functionalStrengthTraining` (20): "Primarily free weights and/or body weight
  and/or accessories";
- also `coreTraining` (59), `crossTraining` (11) "Any mix of cardio and/or
  strength training", `highIntensityIntervalTraining` (63), `cooldown` (80)
  and `preparationAndRecovery` (33).

**`HKWorkout`** [sdk]:

- Properties: `workoutActivityType`, `workoutEvents`, `workoutActivities`
  (iOS 16), `duration`, `allStatistics`, `statistics(for:)` and `metadata`.
- iOS 27 adds `zoneGroupsByType` and `zoneGroup(for:)` on `HKWorkout` and
  `HKWorkoutActivity`. An `HKWorkoutZoneConfiguration` has a `source` of
  system, user or app.
- The `workoutWithActivityType:` initialisers have been deprecated since
  iOS 17: "Use HKWorkoutBuilder". The builder's calls are
  `beginCollection`, `addSamples`, `addWorkoutEvents`, `addMetadata`,
  `addWorkoutActivity`, `endCollection` and `finishWorkout`.
- From iOS 26, `HKWorkoutSession(healthStore:configuration:)`,
  `associatedWorkoutBuilder`, `HKLiveWorkoutBuilder` and
  `recoverActiveWorkoutSession` are available on iPhone, not only on the
  watch.

**Events** [sdk]: `HKWorkoutEventType` has pause, resume, lap, marker,
motionPaused, motionResumed, segment and pauseOrResumeRequest. Events carry
metadata. An app could hide sets in segment events with custom metadata, but
nothing reads them, so this document does not recommend it.

**Effort** [sdk]:

- `workoutEffortScore` and `estimatedWorkoutEffortScore` (iOS 18) use the unit
  `appleEffortScore`.
- `relateWorkoutEffortSample(_:with:activity:)` (async in Swift; Objective-C
  `relateWorkoutEffortSample:withWorkout:activity:completion:`, iOS 18)
  attaches a score to a workout or one of its activities;
  `HKWorkoutEffortRelationshipQuery` reads it back. The Swift spelling was
  checked by type-checking a call against the iOS 27.2 SDK.
- Apple does not document the accepted range in the SDK. It is 1 to 10 in the
  Fitness app [recalled]. Developer forum threads report a CR-10-like scale,
  scores that relate without error but do not show, and a fix by moving from
  Objective-C to Swift [web, secondary].

**Metadata.** `HKMetadata.h` declares 71 keys [sdk]. The ones that matter
here:

- `HKMetadataKeyExternalUUID`: "a unique identifier for an HKObject that is set
  by its source. Uniqueness of objects with the same value of this key is not
  enforced by HealthKit."
- `HKMetadataKeySyncIdentifier` with `HKMetadataKeySyncVersion`: "Saving a
  HKObject with a HKMetadataKeySyncIdentifier will replace an existing HKObject
  with the same HKMetadataKeySyncIdentifier value if the new HKObject has a
  greater HKMetadataKeySyncVersion." A sync version is required whenever a
  sync identifier is set. The header adds only that the new sample "will be
  added to all HKWorkouts or HKCorrelations containing the replaced sample";
  it says nothing about the samples associated with a replaced workout
  itself, so whether those survive is [unverified].
- `HKMetadataKeyWorkoutBrandName`: "the brand name of a particular workout".
  Whether the Fitness app shows it as a title [unverified].
- `HKMetadataKeyIndoorWorkout`, `HKMetadataKeyCoachedWorkout`,
  `HKMetadataKeyGroupFitness`, `HKMetadataKeyTimeZone` and
  `HKMetadataKeyActivityType` (iOS 17).
- There is no title key and no notes key.
- Custom keys are allowed. "values must be either NSString, NSNumber, NSDate,
  or HKQuantity", so the values are flat with no nesting.

**Rumoured set logging.** September 2026 press reports cite watchOS 27 beta
code: the Workout app may log sets with weight, reps, equipment and duration
[unverified, secondary]. The public 27.2 SDKs contain no such type [sdk].

### 3.2 WorkoutKit

- **Plan types:** `WorkoutPlan.Workout` is one of `goal`, `custom`, `pacer` or
  `swimBikeRun` [sdk].
- **`CustomWorkout`** takes an activity, a location, `displayName`, `warmup`,
  `blocks` and `cooldown`:
  - a block (`IntervalBlock`) has `steps` and `iterations`;
  - an `IntervalStep` is work or recovery around a `WorkoutStep`;
  - a `WorkoutStep` has a `goal`, an `alert` and `displayName` (iOS 18)
    [sdk].
- **Goals cannot express a strength set.** `WorkoutGoal` is one of `open`,
  `distance`, `time`, `energy` or `poolSwimDistanceWithTime` (iOS 18). It has
  no repetitions and no load [sdk].
- **Link back.** `HKWorkout.workoutPlan` returns the plan an executed workout
  ran [sdk].
- **Strength support is a runtime question.**
  `CustomWorkout.supportsActivity(.traditionalStrengthTraining)` needs a
  device to answer, so I could not check it [unverified]. Even where it is
  supported, a strength routine becomes open steps with display names ("Bench
  press 8 × 60 kg") and timed recovery steps. The Workout app would log
  neither reps nor load.

### 3.3 Android Health Connect

Read from the `connect-client` sources jars on Google Maven: 1.1.0 (stable,
2025-10-08) and 1.2.0-alpha06 (2026-08-26). The release dates come from the
androidx release notes [web].

**`ExerciseSessionRecord`** [web]:

- start and end times with zone offsets;
- `exerciseType`, one of 61 values: `STRENGTH_TRAINING` (70), `WEIGHTLIFTING`
  (81), `CALISTHENICS` (13) and others;
- `title`, `notes`, `segments`, `laps`, `exerciseRouteResult` and
  `plannedExerciseSessionId`;
- `rateOfPerceivedExertion` (alpha03 and later);
- `metadata.clientRecordId` and `clientRecordVersion`, which give idempotent
  upserts.

**`ExerciseSegment`** [web]:

- stable fields: `startTime`, `endTime`, `segmentType` and `repetitions`;
- alpha03 and later: `weight: Mass?`, `setIndex: Int?` and
  `rateOfPerceivedExertion: Float?` (0 to 10, "Borg CR10");
- the `setIndex` documentation: "A set is a group of consecutive repetitions
  (reps) of a specific exercise performed without a break". Several segments
  may share a set index. The index "may also go back to zero in a single
  ExerciseSessionRecord", for example 0, 1, 2, 0, 1, 2 for three sets of two
  exercises.

**Segment types** [web]:

- 68 values. A strength session accepts the 42 "exercise segments" plus the
  universal ones: `OTHER_WORKOUT`, `PAUSE`, `REST`, `STRETCHING`, `UNKNOWN`.
- The 42 include arm curl, bench press, deadlift, lat pull-down, leg press,
  lunge, pull-up, shoulder press, squat, hip thrust, kettlebell swing and
  plank.
- Missing are push-up, dip, barbell row (only `DUMBBELL_ROW`), chest fly, calf
  raise and hip abduction.
- Left and right variants exist only for the dumbbell curl and the dumbbell
  triceps extension.
- Segments must lie inside the session and must not overlap (04-aggregators
  §2.2). Sequential sets fit. Rest is a `REST` segment.

**`PlannedExerciseSessionRecord`** [web]:

- `blocks`; a `PlannedExerciseBlock` has `repetitions`, `steps` and
  `description`;
- a `PlannedExerciseStep` has `exerciseType` (a segment type),
  `exercisePhase` (warmup, rest, active, cooldown, recovery, unknown),
  `completionGoal`, `performanceTargets` and `description`. The source's own
  example is "8x 60kg barbell squats";
- completion goals: distance, distance and duration, steps, duration,
  repetitions, total calories, active calories, unknown, manual completion;
- performance targets: power, speed, cadence, heart rate, `WeightTarget(mass)`,
  `RateOfPerceivedExertionTarget(rpe: Int 0..10)`, `AmrapTarget` and unknown;
- `completedExerciseSessionId` pairs with the executed session's
  `plannedExerciseSessionId`.

**For Spotter**, Health Connect is Android-only. It matters as an import shape
and as a mapping vocabulary.

### 3.4 Garmin FIT

Read from `profile.js` in the FIT JavaScript SDK 21.218.0, released
2026-10-06 [web].

**`set` (message 225)** [web]:

- fields: `timestamp`, `duration` (s, scale 1000), `repetitions`, `weight`
  (kg, scale 16), `set_type`, `start_time`, `category` (array),
  `category_subtype` (array), `weight_display_unit`, `message_index` and
  `wkt_step_index`;
- `set_type` is only `rest` or `active`;
- `weight_display_unit` is `other`, `kilogram` or `pound`.

The set has no RPE, no warm-up, drop or failure type, no grouping and no
notes. Rest is explicit: a `set` with `set_type` rest and a duration. The
resolution of 1/16 kg (62.5 g) means a load entered in pounds does not survive
exactly.

**`session`** [web] carries `workout_rpe` ("Common Borg CR10 / 0-10 RPE scale,
multiplied 10x") and `workout_feel` (0 to 100). Strength is `sport` training
(10) with `sub_sport` strength_training (20).

**Plans: `workout` (26) and `workout_step` (27)** [web]:

- `wkt_step_name`, `duration_type` and `duration_value`. Duration types
  include `reps` (29), `repetition_time` (28), `time`, `open` and
  `repeat_until_steps_cmplt`.
- target type and value, plus secondary targets;
- `intensity`: active, rest, warmup, cooldown, recovery, interval, other;
- `notes`, `exercise_category`, `exercise_name`, `exercise_weight` (kg, scale
  100) and `weight_display_unit`;
- `exercise_title` (264) names an exercise in a workout file.

A superset in a plan is a repeat step over several exercise steps [recalled].
An executed file has no grouping.

**Exercise taxonomy:** 52 categories (plus `unknown`) and 51 name enums with
1846 names. Re-counted today, this matches 02-workouts §2.1 [web].

**Licence:** see 02-workouts §2.1. Garmin's developer program is closed to new
applicants (02-workouts §0.2).

**Export path to Garmin Connect.** Garmin Connect accepts uploaded FIT
activity files that carry `set` messages. The open source tool `hevy2garmin`
(MIT, 173 stars, pushed 2026-10-26) builds them from Hevy workouts and maps
"433+ Hevy exercises to Garmin FIT SDK categories" [web].

### 3.5 Garmin Connect workout JSON (unofficial)

From the `python-garminconnect` library's `workout.py` (MIT, pushed
2026-10-30). The format is reverse-engineered and Garmin does not publish it
[web, unofficial].

- **Sport:** `sportType` strength_training is id 5.
- **`workoutSegments[].workoutSteps[]`** are `ExecutableStepDTO` or
  `RepeatGroupDTO`.
- **An `ExecutableStepDTO`** has `stepOrder`, `stepType`, `endCondition`,
  `endConditionValue`, `targetType`, `category`, `exerciseName`,
  `weightValue`, `weightUnit` and `description`:
  - `stepType`: 1 warmup, 2 cooldown, 3 interval, 4 recovery, 5 rest,
    6 repeat, 7 other, 8 main;
  - `endCondition`: 1 lap button, 2 time, 3 distance, 4 calories, 5 power,
    6 heart rate, 7 iterations, 8 fixed rest, 9 fixed repetition, 10 reps.
- **A `RepeatGroupDTO`** has `numberOfIterations`, `workoutSteps`,
  `skipLastRestStep` and `smartRepeat`.
- **Weight units disagree.** The library writes `weightValue` in grams under a
  kilogram unit with factor 1000. Another project reports that the workout
  service takes kilograms [web, secondary]. This is the FIT `workout_step` in
  JSON.

## 4. App formats

### 4.1 Hevy public API

Read from the OpenAPI document embedded in `api.hevyapp.com/docs` (version
0.0.1). The API is for Pro users, and Hevy warns it may "completely change the
structure or abandon the project entirely" (02-workouts §0.3) [web].

- **Workout:** `id`, `title`, `routine_id`, `description`, `start_time`,
  `end_time`, `updated_at`, `created_at` and `exercises[]`.
  - An exercise has `index`, `title`, `notes`, `exercise_template_id`,
    `superset_id` and `sets[]`.
  - A set has `index`, `type`, `weight_kg`, `reps`, `distance_meters`,
    `duration_seconds`, `rpe` and `custom_metric`.
  - `type` is normal, warmup, dropset or failure.
  - On POST, `rpe` is one of 6, 7, 7.5, 8, 8.5, 9, 9.5 or 10.
- **Routine:** `id`, `title`, `folder_id` and `exercises[]`.
  - An exercise has `index`, `title`, `rest_seconds` ("The rest time in
    seconds between sets of the exercise"), `notes`, `exercise_template_id`,
    `superset_id` and `sets[]`.
  - A set has `index`, `type`, `weight_kg`, `reps`, `rep_range {start, end}`,
    `distance_meters`, `duration_seconds` and `custom_metric`.
  - A routine has no target RPE and no tempo. Rest is per exercise only.
- **Exercise template:** `id`, `title`, `type`, `primary_muscle_group`,
  `secondary_muscle_groups`, `equipment`, `is_custom` and `thumbnail_url`.
  - `type` is one of `weight_reps`, `reps_only`, `bodyweight_reps`,
    `bodyweight_assisted_reps`, `duration`, `weight_duration`,
    `distance_duration` or `short_distance_weight`.
  - There are 20 muscle groups and 9 equipment values (02-workouts §1.9).
- **History:** `/v1/exercise_history/{exerciseTemplateId}` returns per-set
  history with the workout times, `set_type` and `rpe`. This is the data
  behind Hevy's PREVIOUS column.
- **Change feed:** `/v1/workouts/events` (02-workouts §1.9).
- **Missing:**
  - set timestamps, a done flag and actual rest;
  - RIR, tempo and side;
  - a unit (kg only);
  - a load type, which is implied by the template's `type`.
- **RPE on screen.** The owner's screenshot of Hevy's RPE keypad shows 6, then
  7 to 10 in half steps, with no 6.5: the same eight values the API accepts.
  Each is worded as reps in reserve: 6 is "Moderate Effort, Could do 4 more
  reps". This is the RIR-based RPE scale. (02-workouts §3 says "6 to 10 in
  half steps", which misses the gap at 6.5.)

### 4.2 Hevy CSV export

From importer fixtures and documentation in four GitHub projects
[web, secondary].

- **Columns:** `title`, `start_time`, `end_time`, `description`,
  `exercise_title`, `superset_id`, `exercise_notes`, `set_index`, `set_type`,
  `weight_kg` or `weight_lbs`, `reps`, `distance_km` or `distance_miles`,
  `duration_seconds`, `rpe`.
- **Units** follow the user's setting, and the unit is in the header name.
- **Dates** look like "26 Jul 2025, 07:06": local, with no offset.
- **Exercises** are identified by title only.

### 4.3 Strong CSV export

Strong has no API (02-workouts §1.9). The format below comes from importer
test fixtures on GitHub [web, secondary].

- **Current header:** `Workout #`, `Date`, `Workout Name`, `Duration (sec)`,
  `Exercise Name`, `Set Order`, `Weight (kg)`, `Reps`, `RPE`,
  `Distance (meters)`, `Seconds`, `Notes`, `Workout Notes`.
- **Older exports** use semicolons and separate `Weight Unit` and
  `Distance Unit` columns. They write durations such as "1h 5m" and may use
  locale decimal commas.
- **`Set Order`** holds the set number, `W` for a warm-up and `D` for a drop
  set. A row "Rest Timer" holds the rest in `Seconds`. Whether a failure set
  has its own marker I could not confirm [unverified].
- **Missing:** no column carries supersets, though Strong supports them in the
  app [unverified]. There are no set timestamps, and dates are local with no
  offset.
- **It is the import format other apps accept.** Hevy imports Strong CSV, so
  converters target it; a Fitbod-to-Hevy converter writes "Strong-format CSV"
  [web].

### 4.4 Fitbod CSV export

From a converter's column mapping on GitHub [web, secondary].

- **iOS export (`WorkoutExport.csv`):** `Date`, `Exercise`, `Reps`,
  `Weight(kg)`, `Duration(s)`, `Distance(m)`, `isWarmup` and `Note` are the
  columns it maps. The converter's README gives the full header, which also
  has `Incline`, `Resistance` and `multiplier`, as 02-workouts §1.9 recalled
  [web, secondary].
- **Android:** support mails a database extract with `date`,
  `exercise_name`, `Reps`, `weight_kg`, `duration_seconds`,
  `distance_meters` and `isWarmup`.
- **Missing:** RPE, supersets, rest and a session boundary beyond the date.

### 4.5 FitNotes CSV export

From importer fixtures on GitHub [web, secondary].

- **Columns:** `Date`, `Exercise`, `Category`, `Weight (kgs)`, `Reps`,
  `Distance`, `Distance Unit` (or `Distance (Unit)`) and `Time`. Newer exports
  add `Comment`.
- **Missing:** the time of day, set type, RPE, supersets and rest.

### 4.6 wger

Read from the source on GitHub (last commit 2026-10-29) and the public API
[web].

**The plan:**

- **Routine:** `name`, `description`, `start`, `end`, `is_template`,
  `is_public`, `fit_in_week`.
- **Day:** `order`, `type`, `is_rest`, `need_logs_to_advance`. The type is one
  of custom, enom, amrap, hiit, tabata, edt, rft or afap.
- **Slot:** `order` and `comment`. Its docstring reads "Model for a set of
  exercises", and a slot with more than one entry is a superset
  (`is_superset`).
- **SlotEntry:** `exercise`, `order`, `type`, `repetition_unit`,
  `weight_unit`, rounding and `comment`. The type is one of normal, warmup,
  dropset, myo, partial, forced, tut, iso or jump.

**Targets are per-iteration "config" rows**, which give wger its progression
rules:

- `SetsConfig`, `RepetitionsConfig`, `WeightConfig`, `RiRConfig` and
  `RestConfig`, each with a `Max…` twin for the upper bound of a range;
- each row has `iteration`, `value`, `operation` (`+`, `-` or replace),
  `step` (absolute, percent or n/a), `repeat` and `requirements`.

**Execution:**

- **WorkoutSession:** `routine`, `day`, `datetime_start`, `datetime_end`,
  `notes` and `impression` (bad, neutral, good).
- **WorkoutLog, one row per set:** `date`, `session`, `exercise`, `routine`,
  `slot_entry`, `iteration`; `repetitions`, `repetitions_target` and its unit;
  `weight`, `weight_target` and its unit; `rir` and `rir_target`; `rest`
  ("Logged rest time") and `rest_target`.
- **The log keeps the target beside the actual value**, and points back to
  the slot entry and iteration that planned it.

**Units:**

- repetition units: repetitions, until failure, max reps, seconds, minutes,
  kilometres, metres, miles;
- weight units: kg, lb, body weight, plates, km/h, mph.

**Effort:** RiR is one of 0, 0.5, 1 … 4.5. There is no RPE.

**Licence:** AGPL-3.0 for the code. Each exercise carries its own licence
(CC-BY-SA 3, CC-BY-SA 4, CC-BY 4, CC0 or ODbL) and `license_author`.

### 4.7 Terra `strength_data` (a commercial aggregator's normalised model)

Terra announced per-set strength data in its activity payload on
2026-09-08, with nine integrations. Its documentation now lists Garmin and
COROS (device-detected), TrainingPeaks, Hevy, wger, Fitbod, Strong,
StrongLifts, Lyfta and Liftosaur [web]. The quotes below are the comments in
its data-model reference.

- **`strength_data`:** `body_weight_kg` and `exercises[]`.
- **Exercise:**
  - `exercise_name`: "the source's own label, verbatim; may be null". The
    guide says to use movements and modifiers for logic and `exercise_name`
    for display;
  - `movements[]` and `target`;
  - `modifiers`: implement, surface, grip width, grip orientation,
    laterality, stance, position, bar position, tempo, range of motion,
    accommodating resistance, assistance, programme;
  - `primary_muscles` and `secondary_muscles`;
  - `group_id`: "shared by exercises in the same superset/circuit; null
    otherwise";
  - `notes`, a per-exercise `rpe`, `source` (user logged, device detected)
    and `sets[]`.
- **Set:**
  - `set_type`: normal, warmup, cooldown, dropset, failure, amrap, unknown;
  - `reps`: "per side for unilateral movements";
  - `weight_kg` with `load_type`: external, bodyweight, bodyweight_plus,
    assisted, unknown;
  - `rpe` and `rir`. The RIR is "never converted to or from rpe";
  - `duration_seconds`, `distance_meters` and `notes`;
  - `rest_after_seconds`: "rest after this set, within its exercise; rest
    between exercises is not represented";
  - `start_time` and `end_time`, null where the source has none.
- **Load meaning is not normalised.** `weight_kg` "is the load as the source
  logged it — a barbell's total, but a single dumbbell for dumbbell work.
  Sources do not agree on whether to double it, and Terra does not normalise
  it" [web].

**Lessons for Spotter:**

- Record what a load means: the load type, and whether it counts per side or
  per implement.
- Keep RPE and RIR apart.
- Store rest as data.
- Allow a set with no timestamps.

## 5. Open workout-format efforts

| Effort                        | What it is                                                                                                       | Licence            | Activity                                 | Adoption signal                       |
| ----------------------------- | ---------------------------------------------------------------------------------------------------------------- | ------------------ | ---------------------------------------- | ------------------------------------- |
| Liftoscript (Liftosaur)       | A text language for programs, such as `Bench Press / 3x8 / progress: lp(5lb)`                                    | AGPL-3.0 (the app) | pushed 2026-10-28                        | 766 stars; used inside Liftosaur only |
| openweight                    | JSON Schemas for a workout log, a template, a program and a lifter profile, with converters from Strong and Hevy | Apache-2.0         | created 2025-11-29, last push 2026-03-03 | 7 stars                               |
| OpenSet                       | "sport-agnostic" JSON standard for sessions, workouts and programs                                               | MIT                | created 2026-02-19, last push 2026-09-13 | 7 stars                               |
| WODIS                         | JSON spec for moving training logs between apps                                                                  | MIT                | one day, 2026-02-27                      | 0 stars                               |
| Portable Workout Format (PWF) | JSON format for describing and running workouts                                                                  | Apache-2.0         | created 2026-10-27                       | 0 stars                               |
| Open Workout Format           | A Markdown draft v0.1 from the Set app                                                                           | not checked        | not checked                              | [unverified]                          |

All rows are [web] from the repositories' GitHub metadata, except the last.

**Liftoscript** [web]:

- `NxR` sets, rep ranges such as `3x8-12`, `@8` RPE, `80%` of 1RM, absolute
  loads (`60kg`, `100lb`) and a bare rest timer (`90s`);
- `warmup:` sections, set and exercise variations, and `superset: GROUP`;
- progressions `lp`, `dp`, `sum` and `custom()` scripts;
- weeks and days written as `#` and `##` headings.

It describes programs only, not logs. It is a model for "routines written by
an agent as text".

**openweight** has the most complete set record of any open draft [web]:

- `reps`, `weight` with `unit` (kg or lb), `durationSeconds`, `distance` with
  `distanceUnit`;
- `rpe`, `rir`, `toFailure` and `type` (working, warmup, dropset, backoff,
  amrap, cluster, myo, rest-pause, failure);
- `restSeconds`, `tempo` ("3-1-2-0") and `notes`;
- `targetReps`, `targetWeight` and `targetRPE`;
- `supersetId` on the exercise and `templateId` on the log.

It is a good checklist for the internal model and a cheap optional export.
Nothing imports it.

**None is a standard.** No standards body backs any of them, and none has a
second implementer.

## 6. Capability matrix

"alpha" means the field exists only in Health Connect's 1.2.0 alpha library.
"plan" means it exists only in the format's plan (routine) side. Sources are
in sections 2 to 5.

### 6.1 Standards and platforms

| Concept                | FHIR (core, PA IG, PHR IG)                       | IEEE 1752.1                            | HealthKit and WorkoutKit             | Health Connect                                   | Garmin FIT                               | schema.org                  |
| ---------------------- | ------------------------------------------------ | -------------------------------------- | ------------------------------------ | ------------------------------------------------ | ---------------------------------------- | --------------------------- |
| Session                | Observation with `effectivePeriod` (PHR workout) | `physical-activity` episode            | `HKWorkout`                          | `ExerciseSessionRecord`                          | `activity` and `session`                 | `ExerciseAction`            |
| Exercise ref           | none; PHR reuses Health Connect names            | `activity_name` text                   | none                                 | `segmentType` (68)                               | `category` and `category_subtype` (1846) | `exerciseType` text         |
| Set order              | none                                             | none                                   | none                                 | `setIndex` (alpha) and time                      | `message_index` and time                 | none                        |
| Set type               | none                                             | none                                   | none                                 | none; `REST` segments; plan phase warmup         | `active` or `rest`                       | none                        |
| Reps                   | no code                                          | `base_movement_quantity` (movements)   | none                                 | `repetitions`                                    | `repetitions`                            | plan `repetitions`          |
| Load and unit          | no code                                          | none                                   | none                                 | `weight: Mass` (alpha); plan `WeightTarget`      | `weight` kg/16 plus display unit         | none (`workload` is energy) |
| Bodyweight or assisted | none                                             | none                                   | none                                 | none                                             | none                                     | none                        |
| Duration               | LOINC 55411-3 (session)                          | `duration`                             | workout `duration`; plan `time` goal | segment start and end                            | `duration`                               | plan `activityDuration`     |
| Distance               | session only                                     | `distance`                             | statistics; plan goal                | session `DistanceRecord`                         | lap level                                | `distance`                  |
| RPE or RIR             | LOINC 82289-0 RPE                                | intensity: light, moderate or vigorous | session effort score                 | segment and session RPE (alpha); plan RPE target | session `workout_rpe`                    | plan `intensity` text       |
| Rest                   | none                                             | none                                   | plan recovery step                   | `REST` segment; plan rest step                   | rest `set`; plan rest step               | plan `restPeriods`          |
| Superset or grouping   | `hasMember` nesting, no semantics                | none                                   | plan block iterations                | plan blocks                                      | plan repeat step                         | none                        |
| Tempo                  | none                                             | none                                   | none                                 | none                                             | none                                     | none                        |
| Side                   | `bodySite` only                                  | none                                   | none                                 | left and right types for 2 exercises             | inside exercise names                    | none                        |
| Plan link              | `basedOn`                                        | none                                   | `HKWorkout.workoutPlan`              | `plannedExerciseSessionId` (session)             | `set.wkt_step_index`                     | `exercisePlan`              |
| Notes                  | `note`                                           | none                                   | none (custom metadata)               | session `notes`; plan `description`              | plan step `notes`                        | `description`               |

### 6.2 Apps, an aggregator and an open draft

| Concept                | Hevy API                   | Hevy CSV                | Strong CSV                    | Fitbod CSV    | FitNotes CSV      | wger                                | Terra                    | openweight                |
| ---------------------- | -------------------------- | ----------------------- | ----------------------------- | ------------- | ----------------- | ----------------------------------- | ------------------------ | ------------------------- |
| Session                | workout with start and end | rows by title and start | `Workout #`, `Date`, duration | date only     | date only         | `WorkoutSession`                    | activity                 | log                       |
| Exercise ref           | template id                | title                   | name string                   | name string   | name and category | exercise id or uuid                 | source label plus facets | name, equipment, category |
| Set order              | `index`                    | `set_index`             | `Set Order`                   | row order     | row order         | log order                           | array order              | array order               |
| Set type               | 4 types                    | 4 types                 | `W`, `D`                      | `isWarmup`    | none              | 9 types (plan)                      | 7 types                  | free, 9 suggested         |
| Reps                   | `reps`                     | `reps`                  | `Reps`                        | `Reps`        | `Reps`            | `repetitions` and unit              | `reps` (per side)        | `reps`                    |
| Load and unit          | `weight_kg`                | kg or lb by header      | kg by header, or unit column  | kg            | kg by header      | `weight` and unit                   | `weight_kg`              | `weight` and `unit`       |
| Bodyweight or assisted | by template type           | none                    | none                          | none          | none              | "Body Weight" unit                  | `load_type`              | weight omitted            |
| Duration               | `duration_seconds`         | `duration_seconds`      | `Seconds`                     | `Duration(s)` | `Time`            | seconds or minutes unit             | `duration_seconds`       | `durationSeconds`         |
| Distance               | `distance_meters`          | km or miles             | metres                        | metres        | value and unit    | km, m or miles unit                 | `distance_meters`        | value and unit            |
| RPE or RIR             | RPE 6 to 10                | RPE                     | RPE                           | none          | none              | RiR 0 to 4.5                        | both, kept apart         | both                      |
| Rest                   | plan, per exercise         | none                    | "Rest Timer" rows             | none          | none              | actual and target per set           | `rest_after_seconds`     | `restSeconds`             |
| Superset or grouping   | `superset_id`              | `superset_id`           | none [unverified]             | none          | none              | slot with several entries           | `group_id`               | `supersetId`              |
| Tempo                  | none                       | none                    | none                          | none          | none              | `tut` type only                     | coarse modifier          | `tempo` string            |
| Side                   | none                       | none                    | none                          | none          | none              | none                                | laterality modifier      | none                      |
| Plan link              | `routine_id`               | none                    | none                          | none          | none              | routine, day, slot entry, iteration | none                     | `templateId` and targets  |
| Notes                  | workout and exercise       | workout and exercise    | set and workout               | `Note`        | `Comment` (newer) | session notes, impression           | exercise and set         | session, exercise and set |

Reading the matrices:

- **Supersets** are carried only by Hevy, wger, Terra and openweight. FIT and
  Health Connect have grouping in plans only.
- **Rest** inside a superset and rest after it can be told apart only where
  rest is a positional step in a plan: FIT, Health Connect, WorkoutKit and
  Garmin's `skipLastRestStep`. Hevy has one rest value per exercise.
- **Set timestamps** exist in FIT, Health Connect, Terra and wger (the log
  `date`), but not in Hevy or Strong.
- **Tempo and side** are carried by almost nothing. They belong in Spotter's
  model only if the owner logs them.
- **Plan targets on the executed set** appear only in wger (`*_target`) and
  openweight (`target*`). That is the pattern that keeps history meaningful
  after a routine changes.

## 7. Exercise identity

There is no shared key. Every source names an exercise its own way: an enum
pair (FIT), an int (Health Connect), an opaque id (Hevy, wger), a free string
(Strong, Fitbod, FitNotes) or a display label with facets (Terra). Importers
match by name (02-workouts §3, re-checked). Terra keeps the source's label
for display and tells consumers to use its movement and modifier facets for
logic [web].

| Catalogue                       | Size                                                                  | Licence                                           | Key                                       | Muscle model                              | Equipment model                                 |
| ------------------------------- | --------------------------------------------------------------------- | ------------------------------------------------- | ----------------------------------------- | ----------------------------------------- | ----------------------------------------------- |
| FIT profile 21.218.0 [web]      | 1846 names in 52 categories                                           | FIT Protocol License (02-workouts §2.1)           | `(category, name)` integers               | none; categories are movement patterns    | in the name (`barbell_back_squat`)              |
| Health Connect segments [web]   | 68, of which 42 strength                                              | Apache-2.0 (code)                                 | int constant                              | none                                      | in the name (`DUMBBELL_ROW`)                    |
| HL7 PHR codes [web]             | Health Connect's segment names in camelCase, within 159 workout codes | HL7 (ballot)                                      | code string                               | none                                      | in the name                                     |
| wger [web]                      | 904                                                                   | per exercise: CC-BY-SA 3 or 4, CC-BY 4, CC0, ODbL | id and uuid; `variation_group`            | 15 anatomical muscles, with front or back | 12 values                                       |
| free-exercise-db [web]          | 876                                                                   | Unlicense (public domain)                         | string id from the name (`Barbell_Squat`) | 17 lay muscles, primary and secondary     | 12 values plus null                             |
| Hevy [web]                      | about 430 built-in [unverified; hevy2garmin maps "433+"] plus custom  | proprietary                                       | template id                               | 20 lay groups, one primary                | 9 values; `type` decides which set fields apply |
| ExerciseDB OSS (02-workouts §3) | 1500                                                                  | AGPL-3.0, non-commercial API                      | id                                        | 10 body parts and 38 muscles              | 30 values                                       |
| Terra [web]                     | open vocabulary                                                       | proprietary                                       | none (label plus facets)                  | shared muscle list                        | `modifiers.implement`                           |

Facets the catalogues disagree on, which Spotter must decide once:

- **Equipment variants.** Barbell, dumbbell, smith, cable and machine versions
  are separate exercises in FIT, Health Connect, Hevy and free-exercise-db.
  wger links variants with `variation_group`.
- **Unilateral work.** Health Connect splits left and right for two exercises.
  FIT uses `single_arm_*` names. Hevy, Strong and wger have no side. Terra
  counts reps per side and marks laterality.
- **Load meaning.** A barbell load is the total. A dumbbell load is one bell or
  the pair. A plate-loaded machine may count one side. No source records which
  (Terra says so outright).
- **Field set.** Hevy's template `type` decides which fields a set has: reps
  only, weight and reps, bodyweight-assisted reps, duration, weight and
  duration, distance and duration. The same idea appears as wger's units and
  Terra's `load_type`.

**Recommendation for Spotter:**

1. **Mint Spotter's own exercise records** in its substrate package. Each has
   a stable id, a name and aliases.
   - `measure`: which set fields apply, in Hevy's eight-way split.
   - `equipment` from a small enum.
   - `laterality`: bilateral, unilateral or alternating.
   - `loadCounts`: total, per implement or per side.
   - `primaryMuscles` and `secondaryMuscles` from one lay vocabulary. 17 of
     Hevy's 20 groups map one to one onto free-exercise-db's 17 (Hevy's
     upper back is its middle back); Hevy's cardio, full body and other have
     no counterpart [web]. Hevy's groups match the muscle map in the owner's
     screenshots.
   - `variationOf`, which plays the role of wger's variation group.
2. **Keep every source's code on the exercise**, in a repeated
   `codes: {system, code, version, display}` object, exactly as
   `observations/metric` does. Suggested systems:
   - `fit`, as `category/name`, for example `squat/barbell_back_squat`;
   - `healthconnect` (`EXERCISE_SEGMENT_TYPE_SQUAT`);
   - `hevy` (template id) and `strong` (the exact export name, such as
     `Squat (Barbell)`);
   - `wger` (uuid) and `free-exercise-db` (id);
   - `http://hl7.org/fhir/uv/phr/CodeSystem/observation-pghd-codes` where a
     PHR code exists.
3. **An importer resolves by `(system, code)`** and never by fuzzy name alone.
   An unknown code creates a pending mapping that the console's agent proposes
   and the owner confirms, so two lifts are never merged silently.
4. **Do not bulk-seed a catalogue.** Start from the exercises in the owner's
   routines.
   - free-exercise-db is the only catalogue that can be copied without
     conditions.
   - wger's records would bring share-alike and attribution duties.
   - Copying FIT's enum tables wholesale is the licence question 02-workouts
     §2.1 left open. A handful of FIT codes as mapping values is ordinary use
     of the protocol [unverified as a legal reading].
   - hevy2garmin's Hevy-to-FIT table (MIT) can speed up the mapping. Its FIT
     side carries the same open question.

## 8. Verdict

### 8.1 What to adopt as the basis of the internal model

Adopt no external format wholesale. Take the structure that Hevy, wger, Terra
and openweight share, use the field names Health Connect and Terra use where
they exist, and add what the matrices show is missing.

- **Routine (plan).** Ordered exercise slots. Each slot has an exercise, a
  superset group, notes, the rest between exercises of a group and the rest
  after the group. A slot holds planned sets, each with a type and targets:
  reps or a rep range, a load or %1RM, RPE or RIR, duration, distance, and
  rest after. These are wger's ranges, Hevy's `rep_range` and Health Connect's
  targets.
- **Session (execution).**
  - start and end with the UTC offset;
  - title, notes and session RPE;
  - the routine it came from;
  - the body mass in force, needed for bodyweight loads;
  - the HealthKit workout UUID once written.
- **Exercise entry.** Exercise, order, superset group, notes, and the routine
  slot it came from.
- **Set.**
  - order within the entry and within the session;
  - type: warmup, working, failure, drop, myo activation, myo mini-set, amrap;
  - `reps`, and `load` as a quantity in the unit entered (kg or lb, exact
    digits, which substrate's `quantity` already does);
  - `loadType`: external, bodyweight, bodyweight plus, assisted;
  - duration, distance, `rpe`, `rir`;
  - started and ended times, and actual rest before the set;
  - side and an optional tempo;
  - notes;
  - the planned set it came from, with its targets copied onto the set;
  - a link to the set it continues, for drop and myo chains.

Why this shape:

- Every row of 6.2 maps into it without loss.
- Every column can be produced from it with the losses listed in section 9.
- It keeps the plan context on the executed set, the "store the context of
  the day" lesson of 02-workouts §7.

### 8.2 Import and export targets

| Target                   | Direction                           | Format                 | Fidelity                                         | Notes                                                                       |
| ------------------------ | ----------------------------------- | ---------------------- | ------------------------------------------------ | --------------------------------------------------------------------------- |
| Hevy                     | import, export                      | API JSON; CSV          | high: types, supersets, RPE, routine link        | the API needs Pro and may change; use CSV for history                       |
| Strong                   | import, export                      | CSV                    | medium: no supersets [unverified], no timestamps | the format other apps import; ask the unit on import if the header lacks it |
| Garmin and other watches | import; export later                | FIT activity file      | medium: sets, timing, rest; no set types or RPE  | a FIT `set` export gets Spotter sessions into Garmin Connect                |
| wger                     | import                              | REST API               | high: RiR, rest, targets, supersets              | self-hostable; AGPL                                                         |
| Fitbod, FitNotes         | import                              | CSV                    | low: no grouping, no effort, date-only           | one-off migration                                                           |
| Apple Health             | export (and read back for matching) | HealthKit              | session only                                     | 8.3                                                                         |
| Health Connect           | none on iPhone                      | n/a                    | n/a                                              | vocabulary only                                                             |
| FHIR                     | export, optional                    | Bundle of Observations | session and sets with local codes                | 8.4                                                                         |
| openweight               | export, optional                    | JSON                   | high                                             | cheap; no consumers                                                         |

### 8.3 What Apple Health gets

One workout per finished session, written with `HKWorkoutBuilder` or, if the
session ran as an `HKWorkoutSession` on the iPhone (iOS 26) or a watch, its
live builder [sdk]:

- **Type and place:** `traditionalStrengthTraining`, with location `indoor`
  and `HKMetadataKeyIndoorWorkout` true.
- **Time:** start and end as the session's. Pauses become pause and resume
  events only if Spotter supports pausing.
- **Identity:**
  - `HKMetadataKeyExternalUUID` = Spotter's session record id.
  - `HKMetadataKeySyncIdentifier` = the same id, with `HKMetadataKeySyncVersion`
    = the session's revision, so that an edited session replaces its workout
    instead of adding a second one. Whether a replace keeps associated heart
    rate samples [unverified].
- **Title:** `HKMetadataKeyWorkoutBrandName` = the routine's name, or none
  (open question).
- **Effort:** a `workoutEffortScore` sample related with
  `relateWorkoutEffortSample`. Its value is the session RPE the owner gives at
  the end, or one derived from the sets.
- **Energy and heart rate:** only what a workout session measured (watch or
  connected sensor). Writing an estimate, MET × body mass × hours, needs a
  decision (open question). The 2024 Adult Compendium gives 3.5 MET for
  "Resistance (weight) training, multiple exercises, 8-15 reps at varied
  resistance" (02054) and 6.0 MET for vigorous lifting (02050) [web].
- **Optional custom keys** (flat values only):
  - `SpotterRoutineID` (string);
  - `SpotterSetCount` (number);
  - `SpotterVolume` (an `HKQuantity` in kg).
- **Nothing per exercise.** Neither `HKWorkoutActivity` per exercise nor set
  events, as the owner asked.

**The round trip into substrate.** The applehealth provider mirrors this
workout into `health/workout`, keeping the metadata as JSON [src]. Its
adapter can then link the `health/workout` row to the Spotter session through
`HKMetadataKeyExternalUUID`. Whether Health Auto Export forwards custom
metadata keys [unverified].

### 8.4 What a FHIR export would look like

Use the PHR IG's PGHD Workout profile for the session. It is an Observation,
category `activity`, coded `traditionalStrengthTraining`, and the profile
requires `subject` and `effective[x]` [web]. Give it `hasMember`
links to one Observation per exercise entry, each with `hasMember` links to
one Observation per set. Set values are components with local codes, except
where LOINC has one (RPE 82289-0). A 20-set session is 1 + entries + 20
Observations, which is fine for an export. This follows the FHIR chat answer
in 2.1 (members grouped by `hasMember`, values read together in `component`),
one level coarser: the set, not the rep, is the smallest Observation.

`<spotter-cs>` below stands for a Spotter-owned code system URI, still to be
chosen.

```json
{
  "resourceType": "Bundle",
  "type": "collection",
  "entry": [
    { "resource": {
      "resourceType": "Observation", "id": "session-01J9",
      "meta": { "profile": ["http://hl7.org/fhir/uv/phr/StructureDefinition/pghd-workout"] },
      "status": "final",
      "category": [{ "coding": [{ "system": "http://terminology.hl7.org/CodeSystem/observation-category", "code": "activity" }] }],
      "code": { "coding": [{ "system": "http://hl7.org/fhir/uv/phr/CodeSystem/observation-pghd-codes", "code": "traditionalStrengthTraining" }] },
      "subject": { "reference": "Patient/owner" },
      "effectivePeriod": { "start": "2026-10-30T07:02:00+00:00", "end": "2026-10-30T08:05:00+00:00" },
      "identifier": [{ "system": "<spotter-cs>/session", "value": "01J9" }],
      "component": [
        { "code": { "coding": [{ "system": "http://loinc.org", "code": "82289-0" }] }, "valueInteger": 8 },
        { "code": { "coding": [{ "system": "http://loinc.org", "code": "55411-3" }] },
          "valueQuantity": { "value": 63, "system": "http://unitsofmeasure.org", "code": "min" } }
      ],
      "hasMember": [{ "reference": "Observation/entry-01J9-1" }]
    }},
    { "resource": {
      "resourceType": "Observation", "id": "entry-01J9-1", "status": "final",
      "code": { "coding": [
        { "system": "<spotter-cs>/exercise", "code": "barbell-back-squat", "display": "Back squat (barbell)" },
        { "system": "http://hl7.org/fhir/uv/phr/CodeSystem/observation-pghd-codes", "code": "squat" } ] },
      "component": [{ "code": { "coding": [{ "system": "<spotter-cs>/measure", "code": "superset-group" }] }, "valueInteger": 1 }],
      "hasMember": [{ "reference": "Observation/set-01J9-1-2" }]
    }},
    { "resource": {
      "resourceType": "Observation", "id": "set-01J9-1-2", "status": "final",
      "code": { "coding": [{ "system": "<spotter-cs>/exercise", "code": "barbell-back-squat" }] },
      "effectivePeriod": { "start": "2026-10-30T07:10:05+00:00", "end": "2026-10-30T07:10:41+00:00" },
      "component": [
        { "code": { "coding": [{ "system": "<spotter-cs>/measure", "code": "set-type" }] },
          "valueCodeableConcept": { "coding": [{ "system": "<spotter-cs>/set-type", "code": "working" }] } },
        { "code": { "coding": [{ "system": "<spotter-cs>/measure", "code": "reps" }] }, "valueInteger": 8 },
        { "code": { "coding": [{ "system": "<spotter-cs>/measure", "code": "load" }] },
          "valueQuantity": { "value": 100, "system": "http://unitsofmeasure.org", "code": "kg" } },
        { "code": { "coding": [{ "system": "http://loinc.org", "code": "82289-0" }] }, "valueQuantity": { "value": 8.5 } },
        { "code": { "coding": [{ "system": "<spotter-cs>/measure", "code": "rest-before" }] },
          "valueQuantity": { "value": 150, "system": "http://unitsofmeasure.org", "code": "s" } }
      ]
    }}
  ]
}
```

Notes on the sketch:

- **RPE needs a decimal.** LOINC 82289-0 is a score. The set uses
  `valueQuantity` because R4's `valueInteger` cannot hold 8.5. R4's
  `component.value[x]` allows Quantity, integer and others, but no decimal
  [web].
- **Routines do not export well.** A routine could become a CarePlan or a
  ServiceRequest with a `Timing`, named from each set's `basedOn`. FHIR has no
  set structure to put it in, so I would not export routines (2.1).
- **Shape follows substrate's catalogue.** If the owner ever wants these in
  the `clinical` catalogue's sense, the same reasoning as decision 0193
  applies: keep FHIR's shape where it exists (Observation, `hasMember`,
  `component`), and accept local codes where it does not.

## 9. Field mapping: internal concepts to HealthKit, Health Connect, FIT, Hevy and Strong

"none" means the target cannot hold it; an export drops it, and an import
leaves it empty. "alpha" is Health Connect 1.2.0-alpha03 or later. HC is
Health Connect. All entries are from sections 3 and 4.

| Spotter concept                  | HealthKit                                                  | Health Connect                                                              | FIT                                                                    | Hevy (API)                               | Strong (CSV)                                  |
| -------------------------------- | ---------------------------------------------------------- | --------------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------- | --------------------------------------------- |
| session id                       | `HKMetadataKeyExternalUUID`, `HKMetadataKeySyncIdentifier` | `metadata.clientRecordId`                                                   | `file_id` serial and time [recalled]                                   | `workout.id`                             | `Workout #`                                   |
| session revision                 | `HKMetadataKeySyncVersion`                                 | `metadata.clientRecordVersion`                                              | none                                                                   | `updated_at`                             | none                                          |
| started at, ended at, UTC offset | `startDate`, `endDate`, `HKMetadataKeyTimeZone`            | `startTime`, `startZoneOffset`, `endTime`, `endZoneOffset`                  | `session.start_time`, `total_elapsed_time`, `activity.local_timestamp` | `start_time`, `end_time`                 | `Date` (local), `Duration (sec)`              |
| active duration                  | `duration`                                                 | none                                                                        | `total_timer_time`                                                     | none                                     | `Duration (sec)`                              |
| session kind (strength)          | `traditionalStrengthTraining` (50)                         | `EXERCISE_TYPE_STRENGTH_TRAINING` (70)                                      | `sport` training (10), `sub_sport` strength_training (20)              | implicit                                 | implicit                                      |
| title                            | `HKMetadataKeyWorkoutBrandName` (closest)                  | `title`                                                                     | none in an activity file                                               | `title`                                  | `Workout Name`                                |
| session notes                    | none (custom key)                                          | `notes`                                                                     | none                                                                   | `description`                            | `Workout Notes`                               |
| session RPE                      | `workoutEffortScore`, related to the workout               | `rateOfPerceivedExertion` (alpha)                                           | `session.workout_rpe` (×10)                                            | none                                     | none                                          |
| routine (plan link)              | `workoutPlan` (WorkoutKit plans only)                      | `plannedExerciseSessionId`                                                  | none at session level                                                  | `routine_id`                             | none                                          |
| exercise                         | none                                                       | `segmentType` (42 strength values, else `WEIGHTLIFTING` or `OTHER_WORKOUT`) | `category`, `category_subtype`                                         | `exercise_template_id` (and `title`)     | `Exercise Name`                               |
| exercise entry order             | none                                                       | segment time order                                                          | message order and time                                                 | `exercises[].index`                      | row order                                     |
| exercise notes                   | none                                                       | none                                                                        | none                                                                   | `exercises[].notes`                      | `Notes` (on set rows)                         |
| superset group                   | none                                                       | none (plan blocks only)                                                     | none (plan repeat steps only)                                          | `superset_id`                            | none                                          |
| set order                        | none                                                       | `setIndex` (alpha)                                                          | `message_index`                                                        | `sets[].index`                           | `Set Order`                                   |
| set type                         | none                                                       | none (`REST` segment for rest; plan phase warmup)                           | `set_type` active or rest                                              | `type`: normal, warmup, dropset, failure | `Set Order` `W`, `D`                          |
| reps                             | none                                                       | `repetitions`                                                               | `repetitions`                                                          | `reps`                                   | `Reps`                                        |
| load and unit                    | none                                                       | `weight: Mass` (alpha)                                                      | `weight` (kg, 1/16) and `weight_display_unit`                          | `weight_kg`                              | `Weight (kg)`, or `Weight` with `Weight Unit` |
| load type                        | none                                                       | none                                                                        | none                                                                   | template `type`                          | none                                          |
| duration                         | none                                                       | segment `endTime` minus `startTime`                                         | `duration`                                                             | `duration_seconds`                       | `Seconds`                                     |
| distance                         | none                                                       | none                                                                        | none                                                                   | `distance_meters`                        | `Distance (meters)`                           |
| set RPE                          | none                                                       | segment `rateOfPerceivedExertion` (alpha)                                   | none                                                                   | `rpe` (6 to 10)                          | `RPE`                                         |
| set RIR                          | none                                                       | none                                                                        | none                                                                   | none                                     | none                                          |
| set started, ended               | none                                                       | segment `startTime`, `endTime` (required)                                   | `start_time`, `timestamp`                                              | none                                     | none                                          |
| actual rest                      | none                                                       | `REST` segment                                                              | `set` with `set_type` rest                                             | none                                     | "Rest Timer" row                              |
| planned rest                     | WorkoutKit recovery step                                   | plan step with phase rest and a duration goal                               | `workout_step` with intensity rest                                     | routine `rest_seconds` (per exercise)    | none                                          |
| planned reps, range              | none                                                       | plan `RepetitionsGoal`                                                      | `duration_type` reps and `duration_value`                              | routine `reps`, `rep_range`              | none                                          |
| planned load                     | none                                                       | plan `WeightTarget`                                                         | `exercise_weight`                                                      | routine `weight_kg`                      | none                                          |
| planned RPE                      | none                                                       | plan `RateOfPerceivedExertionTarget` (int), `AmrapTarget`                   | none                                                                   | none                                     | none                                          |
| planned-set link                 | none                                                       | none                                                                        | `set.wkt_step_index`                                                   | none                                     | none                                          |
| side                             | none                                                       | left and right types (2 exercises)                                          | in the exercise name                                                   | none                                     | none                                          |
| tempo                            | none                                                       | none                                                                        | none                                                                   | none                                     | none                                          |
| set notes                        | none                                                       | none                                                                        | none                                                                   | none                                     | `Notes`                                       |

## Design implications for Spotter

1. **Own the model; do not adopt a format.** Nothing to adopt exists (section
   0). Design Spotter's substrate kinds on 8.1 and treat every external format
   as a mapping, as decision 0193 treats FHIR for clinical records.
2. **Store the load as a quantity in the unit the owner entered.** Use
   substrate's `quantity` with `units: [kg, lb]`, alongside `loadType` and the
   exercise's `loadCounts`. FIT's 1/16 kg and Hevy's kg-only API both lose
   pound loads, so converting at write would bake the loss in.
3. **Keep RPE and RIR as two fields.** Hevy's keypad is RPE worded as RIR, and
   wger and Terra keep RIR and never convert. Spotter's next-set
   suggestions can convert at read.
4. **Give every set a start and an end time, even when estimated, and record
   rest as data.** The data needs are:
   - Health Connect requires segment times;
   - FIT exports need them;
   - the rest timer, the next-set suggestion and "rest inside a superset vs
     after it" all need actual rest.
     Hevy and Strong have no timestamps, so an import leaves them empty.
5. **Model the superset group on the exercise entry, and rest in two places
   on the plan:** between exercises of a group, and after the group. Hevy
   cannot hold the distinction (one `rest_seconds` per exercise). An export
   to Hevy puts the group rest on the last exercise of the group and 0 on the
   others [unverified as Hevy's behaviour].
6. **Copy the plan's targets onto each executed set and point at the planned
   set.** This is wger's `*_target` and `slot_entry`, and FIT's
   `wkt_step_index`. History then survives routine edits, and the "previous"
   and suggestion lookups can match warm-up to warm-up and working set n to
   working set n.
7. **Index sets per exercise per session (`entrySetIndex`) as well as per
   session (`sessionSetIndex`).** Hevy's PREVIOUS column and its
   `exercise_history` endpoint work per exercise and set index. Health
   Connect's `setIndex` may also restart per exercise (its documented
   example is 0, 1, 2, 0, 1, 2).
8. **Set types:** warmup, working, failure, drop, amrap, myo activation and
   myo mini-set. Export them as follows:
   - to Hevy as warmup, normal, failure or dropset; myo becomes normal;
   - to Strong as `W` and `D`;
   - to FIT as active, with warm-up lost;
   - to Health Connect as nothing.
     Keep a `continues` link for drop and myo chains so that volume and
     suggestions can treat a chain as one effort.
9. **Exercises carry a `codes` list.** Make it identical in shape to
   `observations/metric.codes`, so that importers resolve by `(system, code)`
   (section 7). Mapping a new source then means adding codes, not changing the
   schema.
10. **Apple Health gets the session only, keyed by Spotter's id** (8.3). The
    applehealth importer then sees the same workout come back, and must link
    rather than duplicate. Both sides should key on
    `HKMetadataKeyExternalUUID`.
11. **Routines are text-friendly.** The console agent writes routines today.
    Liftoscript shows a whole program fits in lines like `Squat / 3x5 @8 /
    150s`. A small, documented text form for a routine slot, or at least
    agent instructions that use the same vocabulary as the kinds, will cut
    errors. This is optional and later.
12. **Export later, in this order:**
    - Strong CSV: one function, accepted by the most apps;
    - FIT activity with `set` messages, if the owner wants sessions in Garmin
      Connect;
    - FHIR (8.4), only if a clinician or the `clinical` catalogue needs it.

## Open questions for the owner

1. **Pounds.** Do you ever log in pounds, or with pound plates? If never,
   kg-only storage is simpler. If sometimes, the unit is kept per set (design
   implication 2).
2. **History import.** Which history should come in first: Hevy (API with a
   Pro key, or the CSV export), Strong, or none? Is there older data in
   Fitbod, FitNotes or a Garmin watch?
3. **Session effort.** Should Spotter ask for a session RPE at the end, to
   write as Apple's effort score and FIT's `workout_rpe`? Or should it derive
   one from the set RPEs?
4. **Estimated energy.** When no watch or sensor measured the session, should
   Spotter write an estimate to Apple Health, or leave energy empty?
5. **Workout title.** Should the Apple Health workout carry the routine name in
   `HKMetadataKeyWorkoutBrandName`, or nothing?
6. **RPE or RIR.** Do you rate effort as RPE (Hevy style, 6 to 10), as RIR, or
   both? The model keeps both. The keypad shows one.
7. **Tempo and side.** Do you log tempo (for example 3-1-1-0) or left/right
   sides separately? If not, drop them from v1.
8. **Exercise naming.** Should exercises be named by equipment first ("Barbell
   back squat") or by movement first ("Back squat (barbell)", as Hevy and
   Strong do)? Is a seed list wanted, or only the exercises in your routines?
9. **FIT export.** Would sessions in Garmin Connect (via a FIT file) be useful,
   or is Apple Health enough?

## Sources

Prior research (read-only export of substrate main `ffd7b967`):

- `/tmp/spotter-substrate-main/docs/plans/health-data/02-workouts.md`
- `/tmp/spotter-substrate-main/docs/plans/health-data/04-aggregators-and-models.md`
- `/tmp/spotter-substrate-main/docs/plans/health-data/06-substrate-fit.md`

Substrate source [src]:

- `/tmp/spotter-substrate-main/docs/decisions/0193-the-clinical-record-is-a-first-party-catalogue-shaped-on-fhir.md`
- `/tmp/spotter-substrate-main/docs/clinical.md`
- `/tmp/spotter-substrate-main/docs/units.md`
- `/tmp/spotter-substrate-main/docs/data-model.md`
- `/tmp/spotter-substrate-main/kinds/substrate.reamde.dev/clinical/encounter.yaml`
- `/tmp/spotter-substrate-main/kinds/substrate.reamde.dev/health/workout.yaml`
- `/tmp/spotter-substrate-main/kinds/substrate.reamde.dev/observations/metric.yaml`
- `/tmp/spotter-substrate-main/kinds/substrate.reamde.dev/health/metrics.yaml`
- `/tmp/spotter-substrate-main/kinds/providers.substrate.reamde.dev/applehealth/bundle.yaml`
- Substrate git `f47c21f5^:samples/fitness/` (`exercise.yaml`, `workoutset.yaml`, `workoutlog.yaml`)

SDK [sdk]:

- `iPhoneOS27.2.sdk/System/Library/Frameworks/HealthKit.framework/Headers/`
  (`HKTypeIdentifiers.h`, `HKWorkout.h`, `HKWorkoutActivity.h`,
  `HKWorkoutActivityType.h`, `HKWorkoutBuilder.h`, `HKWorkoutSession.h`,
  `HKHealthStore.h`, `HKMetadata.h`, `HKObject.h`, `HKUnit.h`,
  `HKWorkoutEffortRelationshipQuery.h`) and
  `HealthKit.swiftmodule/arm64e-apple-ios.swiftinterface`
- `iPhoneOS27.2.sdk/System/Library/Frameworks/WorkoutKit.framework/Modules/WorkoutKit.swiftmodule/arm64e-apple-ios.swiftinterface`
- `WatchOS27.2.sdk/System/Library/Frameworks/HealthKit.framework/Headers/`

HL7 FHIR and terminologies [web]:

- [FHIR R5 resource list](https://hl7.org/fhir/R5/resourcelist.html)
- [FHIR R5 Observation](https://hl7.org/fhir/R5/observation.html) and [R4 Observation](https://hl7.org/fhir/R4/observation.html)
- [observation-category code system (JSON)](https://terminology.hl7.org/7.0.1/CodeSystem-observation-category.json)
- [Physical Activity IG](https://hl7.org/fhir/us/physical-activity/)
- [Physical Activity IG artifacts](https://hl7.org/fhir/us/physical-activity/artifacts.html)
- [Physical Activity IG history](https://hl7.org/fhir/us/physical-activity/history.html)
- [PA activity-related codes value set](https://hl7.org/fhir/us/physical-activity/ValueSet-pa-observation-code-activity.json)
- [PA EVS codes value set](https://hl7.org/fhir/us/physical-activity/ValueSet-pa-observation-code-evs.json)
- [PA activity group profile](https://hl7.org/fhir/us/physical-activity/StructureDefinition-pa-observation-activity-group.json)
- [PA example activity type](https://hl7.org/fhir/us/physical-activity/Observation-ExampleActivityType.json)
- [Personal Health Records IG, workout codes](https://build.fhir.org/ig/HL7/personal-health-record-format-ig/en/ValueSet-observation-workout-codes.html)
- [PHR IG PGHD Workout profile (JSON)](https://build.fhir.org/ig/HL7/personal-health-record-format-ig/en/StructureDefinition-pghd-workout.json)
- [PHR IG package](https://build.fhir.org/ig/HL7/personal-health-record-format-ig/package.tgz)
- [Standard Patient Health Record IG, HKWorkout logical model](https://build.fhir.org/ig/HL7/standard-patient-health-record-ig/branches/master/StructureDefinition-apple-health-kit-workout-sample.html)
- [Finnish PHR fitness profile](https://packages2.fhir.org/xig/resource/fi.digious.kanta.test%7C0.0.1-preview-2/StructureDefinition/FIPHRSDFitness.json)
- [FHIR chat: rehab exercise results](https://chat-archive.fhir.org/stream/179166-implementers/topic/rehab.20exercise.20results.html)
- [FHIR chat: fitness profiles (Finnish PHR)](https://chat-archive.fhir.org/stream/179222-finnish-PHR/topic/Fitness.20profiles.html)
- [tx.fhir.org LOINC and SNOMED CT lookups](https://tx.fhir.org/r4/)

Open mHealth, IEEE and schema.org [web]:

- [IEEE 1752 physical-activity schema](https://w3id.org/ieee/ieee-1752-schema/physical-activity.json)
- [Open mHealth schemas repository](https://github.com/openmhealth/schemas)
- [schema.org ExercisePlan](https://schema.org/ExercisePlan)
- [schema.org ExerciseAction](https://schema.org/ExerciseAction)
- [2024 Adult Compendium of Physical Activities](https://pacompendium.com/adult-compendium/) and [conditioning exercise codes](https://pacompendium.com/conditioning-exercise)

Apple [web]:

- [WorkoutKit CustomWorkout](https://developer.apple.com/documentation/workoutkit/customworkout)
- [HKMetadataKeyWorkoutBrandName](https://developer.apple.com/documentation/healthkit/hkmetadatakeyworkoutbrandname)
- [Developer forums: workout effort scores](https://developer.apple.com/forums/thread/764884)
- [Developer forums: read workout effort scores](https://developer.apple.com/forums/thread/772319)
- [TechRadar on rumoured Workout app set logging](https://techradar.com/health-fitness/smartwatches/im-sick-of-my-apple-watchs-workout-app-lacking-this-key-fitness-feature-but-a-new-apple-watch-ultra-4-rumor-suggests-it-could-finally-catch-up-with-apps-like-smartgym)
- [Cult of Mac on the watchOS 27 leak](https://www.cultofmac.com/news/watchos-27-readiness-app-apple-watch-leak)

Android Health Connect [web]:

- [connect-client versions (Maven metadata)](https://dl.google.com/android/maven2/androidx/health/connect/connect-client/maven-metadata.xml)
- [connect-client 1.1.0 sources](https://dl.google.com/android/maven2/androidx/health/connect/connect-client/1.1.0/connect-client-1.1.0-sources.jar)
- [connect-client 1.2.0-alpha06 sources](https://dl.google.com/android/maven2/androidx/health/connect/connect-client/1.2.0-alpha06/connect-client-1.2.0-alpha06-sources.jar)
- [androidx Health Connect release notes](https://developer.android.com/jetpack/androidx/releases/health-connect)

Garmin [web]:

- [FIT JavaScript SDK (profile.js, release 21.218.0)](https://github.com/garmin/fit-javascript-sdk)
- [python-garminconnect workout.py](https://github.com/cyberjunky/python-garminconnect/blob/master/garminconnect/workout.py)
- [hevy2garmin](https://github.com/drkostas/hevy2garmin)
- [Garmin MCP strength workout tool notes](https://glama.ai/mcp/servers/Taxuspt/garmin_mcp/tools/create_strength_workout)

Apps [web]:

- [Hevy API docs](https://api.hevyapp.com/docs/) and [embedded OpenAPI](https://api.hevyapp.com/docs/swagger-ui-init.js)
- Hevy CSV header in [LiftShift](https://github.com/aree6/LiftShift/blob/main/frontend/constants.ts), [quantifiedme](https://github.com/ErikBjare/quantifiedme/blob/master/src/quantifiedme/load/hevy.py), [workout-chart](https://github.com/Reda2200/workout-chart/blob/main/workout.csv.example), [Multi_Hevy_Workout_Dashboard](https://github.com/multiplex55/Multi_Hevy_Workout_Dashboard)
- Strong CSV fixtures in [gymtracker](https://github.com/anes7374/gymtracker/blob/main/tests/strong-csv.test.js) and [oros](https://github.com/koulaxizis/oros/blob/main/tests/fitness.test.js); [Strong CSV import notes](https://repstack.pages.dev/import/strong-csv/)
- Fitbod columns in [fitbod-to-hevy-converter](https://github.com/sinan92/fitbod-to-hevy-converter/blob/main/src/app/converter/column-mapping.ts) and its [README](https://github.com/sinan92/fitbod-to-hevy-converter/blob/main/README.md)
- FitNotes fixtures in [pumpR](https://github.com/magnusnissel/pumpR) and [SBL-App](https://github.com/AviRamoutar/SBL-App)
- wger [manager models](https://github.com/wger-project/wger/tree/master/wger/manager/models), [RiR options](https://github.com/wger-project/wger/blob/master/wger/manager/consts.py), [public API](https://wger.de/api/v2/)
- Terra [strength training](https://docs.tryterra.co/unified-api/strength-training), [data models](https://docs.tryterra.co/reference/health-and-fitness-api/data-models), [announcement](https://tryterra.co/blog/strength-training-api)

Exercise catalogues [web]:

- [free-exercise-db](https://github.com/yuhonas/free-exercise-db) and [dist/exercises.json](https://raw.githubusercontent.com/yuhonas/free-exercise-db/main/dist/exercises.json)
- [wger exerciseinfo](https://wger.de/api/v2/exerciseinfo/), [muscles](https://wger.de/api/v2/muscle/), [equipment](https://wger.de/api/v2/equipment/), [licences](https://wger.de/api/v2/license/)

Open workout formats [web]:

- [Liftoscript documentation](https://www.liftosaur.com/doc/liftoscript) and [Liftosaur repository](https://github.com/astashov/liftosaur)
- [openweight](https://github.com/radupana/openweight) and its [workout-log schema](https://github.com/radupana/openweight/blob/main/docs/public/schemas/workout-log.schema.json)
- [OpenSet](https://github.com/getdiby/OpenSet) and [openset.dev](https://openset.dev/)
- [WODIS](https://github.com/aassoiants/workout-open-data-spec)
- [Portable Workout Format](https://github.com/portableworkoutformat/pwf)
- [Open Workout Format draft (Set app)](https://set-app.pages.dev/workout-format.html)
- [Structured Workout Format](https://structuredworkoutformat.pages.dev/specification)

Owner-provided artefact:

- Hevy RPE keypad screenshot: an attachment the owner shared, not kept in this repository.
