# Spotter research: strength workouts on substrate

> **Where this came from.** Written on 2026-10-30 in rxtx.dev, as `research/workouts/`, for
> the app then called Spotter (Substrate Workouts now); the version here is the one the
> app's second build used (geoah/rxtx.dev#118). It was carried into this repository on
> 2026-11-02 as written, with two changes. The links resolve inside this repository. The
> draft kind paths `mneme.rxtx.dev/training/<kind>` now read as the app-owned
> `~/training/<kind>` they became (decision record [0203](../../../../docs/decisions/0203-an-app-keeps-its-own-package-under-a-declare-scope.md)). Where the
> text names the package home it recommended, a Mneme package `mneme.rxtx.dev/training`,
> that name is left as written: the home changed later, and the reasons are in
> [decisions.md](../decisions.md). Mentions of Mneme, rxtx.dev paths or
> `apps/apple/spotter` describe the plan at the time. The package now lives in
> [`Package/`](../../package/README.md), and the app's spec is [DESIGN.md](../../DESIGN.md).

Index and synthesis of research slices 01 to 06. Date: 2026-10-30.

Spotter is an iPhone app for strength sessions in the gym. It targets iOS 27 with Liquid Glass; a watch app comes later and is design only for now. Routines are written in the substrate console by its agent. The app browses them, runs a session from one (or from nothing), and stores sessions and sets on substrate. Each finished session also goes to Apple Health as one strength workout.

The six documents build on substrate's `docs/plans/health-data/02-workouts.md` (2026-10-08, main `ffd7b967`), and all six were fact-checked on 2026-10-30. This file adds no new research. Each point names the document and section that hold its evidence, written as "01 §8.1" or "05 DI" (DI is a document's "Design implications" section). The evidence markers ([web], [sdk], [src], [recalled], [unverified]) are in those sections. Where this file states a fact in its own words, it repeats the source's marker.

## The documents

- [01-formats-and-standards.md](01-formats-and-standards.md): whether a common workout format exists (FHIR and the HL7 IGs, HealthKit, WorkoutKit, Health Connect, FIT, Hevy, Strong, wger, Terra, open drafts). It also covers a field mapping, exercise identity and a FHIR export sketch.
- [02-training-methods.md](02-training-methods.md): set structures (warm-ups, failure, drops, rest-pause, myo-reps, clusters, timed and unilateral work), grouping, the seven rest slots and the rule that picks one, evidence on rest, effort and volume, and 28 data-model requirements.
- [03-progression.md](03-progression.md): suggesting the next set. Part A covers the research on e1RM, RPE and fatigue. Part B is a deterministic specification with test vectors V1 to V19.
- [04-ios-platform.md](04-ios-platform.md): iOS 27 APIs checked in the 27.2 SDK. It covers Liquid Glass, the rest timer on a locked phone (Live Activity, time-sensitive notification, AlarmKit), the Apple Health export, session safety, readiness for a watch app, and signing.
- [05-substrate-fit.md](05-substrate-fit.md): where routines, sessions and sets live in substrate. It compares three record shapes, sketches the recommended kinds, and covers the link to Apple Health and `health/workout`.
- [06-app-teardown.md](06-app-teardown.md): the owner's 13 Hevy screenshots, read screen by screen. It also surveys eleven other strength apps and their reviews, and gives a prioritised feature list, flows and pain points.
- [07-coaching.md](07-coaching.md): written later, on 2026-11-02, for the workout coach: intake, programme defaults, adjusting from data, recovery signals, safety and memory.

## Short answers

### Is there a common format Spotter can reuse?

No. None of the standards evaluated here carries all of a strength session down to the set, and no format is shared by more than one vendor. "Down to the set" means exercise, order, set type, reps, load, effort, rest and grouping together (01 §0); FIT and Health Connect carry parts of it at the set.

- **Health standards stop at the session.** FHIR has no exercise resource. The HL7 Physical Activity IG counts strength days per week. The PHR IG's PGHD Workout profile (still a ballot) is an Observation with members and no reps or load. No LOINC or SNOMED CT code for a set, a rep count or a lifted load was found, which is not proof that none exists in every edition [web] (01 §2).
- **Apple has nothing below the workout.** HealthKit in the iOS 27.2 SDK has no set, rep or load type. WorkoutKit goals cannot express "8 reps" or "60 kg" [sdk] (01 §3.1, §3.2).
- **Set-level data exists only in vendor formats.** These are:
  - FIT `set` messages;
  - Health Connect's `ExerciseSegment`, whose weight, set index and RPE are still alpha in 1.2.0-alpha06;
  - the Hevy API and CSV, the Strong CSV and wger;
  - Terra's `strength_data` [web] (01 §3.3 to §4.7).
- **The open drafts are not used.** openweight, OpenSet, WODIS and PWF have 0 to 7 GitHub stars each, and none has a second implementer [web] (01 §5).
- **Exercises have no shared key.** FIT uses enum pairs, Health Connect integers, Hevy and wger opaque ids, and Strong a name string (01 §7).

### What Spotter adopts

- **Its own model, mapped field by field to the others.** The structure is Hevy's, which the owner already uses and most importers accept. A session holds exercise entries; each entry carries a superset group and holds typed sets. Spotter adds what Hevy lacks and other formats have:
  - set start and end times (FIT, Health Connect);
  - load as a quantity with its unit, plus a load type (Terra);
  - RIR beside RPE (wger, Terra);
  - planned and actual rest (wger, Terra);
  - the plan's targets copied onto each set (wger, openweight);
  - a `codes` list per exercise (01 §0, §8.1).
- **Storage.** One substrate record per set, beside session, exercise and routine records. They go in a new Mneme package, `mneme.rxtx.dev/training`, shaped to the first-party conventions. A later move to a `substrate.reamde.dev` catalogue is then a row copy that keeps the ids (05 §3.5, §4).
- **Apple Health.** One `HKWorkout` per session, of type `traditionalStrengthTraining`, with the session id in `HKMetadataKeyExternalUUID`. It has no per-exercise detail and no energy estimate (01 §8.3; 04 §3; 05 §5.2).
- **Imports and exports, later.**
  - Imports: Hevy (API or CSV), Strong CSV, FIT activity files and wger. Fitbod and FitNotes CSVs are lossy one-off imports.
  - Exports: Strong CSV first, then FIT with `set` messages. A FHIR Bundle on the PGHD Workout profile only if someone needs one (01 §8.2, §8.4).
- **Exercise identity.** Each exercise keeps every source's code as `{system, code, version, display}`. Importers resolve by `(system, code)`, never by fuzzy name. An unknown code creates a pending mapping that the owner confirms (01 §7).

## Requirements

Deduplicated across the six documents. Where the documents disagree, the requirement follows the side taken in the next section.

### Data model

1. Spotter owns its model. Every outside format is a mapping, not the basis (01 §8.1, DI 1).
2. The kinds are exercise, routine (the plan), session and set, in `mneme.rxtx.dev/training` version 1. Plan and session are separate kinds, and the session references the plan it started from (05 §2.1 lesson #275, §3.5, §4.1).
3. Each performed set is its own record. It has a required, cascading `session` reference (`mustExist`), an `exercise` reference, a slot key and an order, with indices `[exercise, at]` and `[session, at]`. Anything that is filtered or aggregated is a top-level property (05 §1.3, §1.4, §3.6).
4. The session embeds its ordered slots: the exercise, the superset group key and notes. Reordering is then one patch. Group values (kind, round rest, rounds) are stored once per group, not on every member (05 §3.6; 02 §7 items 1 and 2).
5. Load is a substrate `quantity` in the unit entered (`unit: kg`, `units: [kg, lb]`), never a float. Beside it go a load mode (external, bodyweight, bodyweight plus load, assisted) and the exercise's load basis (total, per implement or per side). Assistance is stored as a positive value of its own (01 DI 2; 02 §7 items 7, 20, 21; 03 §4; 05 §1.2). Each set copies what its load and reps meant (load basis, implements, body share, laterality, rep count basis) when it is logged, so an exercise edit never reinterprets history (data-model review, 2026-10-31).
6. Role, structure, termination and outcome are separate fields (02 §0 item 2, §7 items 4 to 6):
   - role: warm-up or working, with an optional `top` or `backoff` label;
   - structure: straight, drop, myo, myo-match, rest-pause or cluster;
   - termination: reps, rep range, AMRAP, failure, RIR stop, time, distance or total reps;
   - outcome: the flags `reachedFailure` and `failedRepAttempted`.
7. A structured set is a chain of set records: each drop, myo mini-set or side is one record. Each record links to the set it continues and has a position in the chain. Counting, PREVIOUS and suggestions treat the chain as one set (02 §0 item 3; 01 DI 8; 06 DI 3; 03 §11.6, §11.7).
8. Effort is stored as entered and never converted on write. That means `rpe` (a decimal on the RIR-based scale), `rir` when a source logs RIR, and a scale tag on imported values such as Borg CR10. RIR = 10 − RPE is applied at read, on the RIR-based scales only, never on CR10. A missing rating is a valid state (01 DI 3; 02 §5.5; 03 §10.2; 05 §3.6). Each value also says how it was given (rated, pre-filled from the plan, copied, imported), and a pre-filled value reads as no rating (data-model review, 2026-10-31).
9. Each set records when it ended, and when it started if known. It also records the rest actually taken before it, the prescribed rest and whether the rest was skipped. Imported sets without times leave these fields empty (01 DI 4; 02 §7 item 18; 06 DI 4). Each set says whether its time and rest were measured, stood in for, or estimated, and how it was completed (data-model review, 2026-10-31).
10. Each set links to the planned set it fulfils; the link is empty for added sets and empty sessions. The targets in force are copied onto the set: reps or a rep range, load, an RPE or RIR range, duration and rest, and the rest of the prescription as the engine ran it (01 DI 6; 02 §7 item 19; 03 DI 1). The session also keeps the planned sets it started with, done or not (data-model review, 2026-10-31).
11. Each set has a working ordinal, so PREVIOUS and suggestions compare working set n with working set n, not row with row (01 DI 7; 03 §10.7; 06 §4.8).
12. The session holds (05 §3.6, DI; 02 §7 item 22; 03 DI 4; 04 DI):
    - a `name` written on the local clock;
    - start, end, `utcOffsetStart` and `utcOffsetEnd`, and pause spans;
    - a `status` state, active then finished, moved only by PATCH;
    - the routine and its revision, and a deload flag;
    - a body-mass snapshot and a session RPE on the CR10 scale;
    - notes and its own `revision`;
    - the HealthKit workout UUID, and a `workout` reference to `health/workout`;
    - the sport in `health/workout`'s code set, and the IANA time zone as `timezone` (data-model review, 2026-10-31).
13. The exercise holds (01 §7; 02 §7 items 23 to 27; 03 §9.1, DI 5):
    - an id chosen by the writer, a name and aliases;
    - a measurement type (Hevy's eight, plus weighted bodyweight), equipment, laterality and load basis;
    - primary and secondary muscles, a rest class and `variationOf`;
    - the suggestion settings: unit, base, step, max, mode, body share, defaults, rep gain, drop percent, time step and myo rule.
14. Exercise identity is a repeated `codes` object shaped like `observations/metric.codes`. Importers resolve by `(system, code)` (01 §7, DI 9).
15. Sets carry a setup, so machine and cable history does not mix across machines or gyms unless the owner links them (03 §7, DI 3). A setup is a record of its own, one exercise at one place with its own load grid, and a set names it by reference (data-model review, 2026-10-31).
16. A routine is ordered groups of slots, each slot holding planned sets. The rest of its shape (02 §7 items 1 to 17, §10; 03 DI 6; 06 DI 2):
    - Each planned set has a type and targets: reps or a range; a load (absolute, percent, effort-driven, bodyweight, or relative to the previous set); an RPE or RIR target, marked as a load selector or a stop rule; a duration with its timer mode; distance; tempo; per-side settings; and myo and drop parameters.
    - Rests: `setRest` and `warmupRest` per slot, `transition` per group member, `roundRest` per group, `intraSetPause`, `sideSwitch`, and an override per set.
    - Method parameters live in this data, not in Swift, so the agent can change them without an app release.
17. Kinds use the first-party property names and types: `about`, `source`, `origin`, `revision`, the UTC offsets, `name`, `notes` as `text`, `duration` and `distance`. They bind `temporal(range)` and `revised`. The app reads kind names from one configuration point (05 §4.2, DI).
18. Ids are chosen on the device (lowercase UUIDv7). Writes follow these rules (05 §3.3, DI):
    - create with PUT, and never put `status` in a PUT;
    - use `ifVersion` on every session patch;
    - the outbox writes a session before its first set.
19. The set kind's shape should be right before it holds about 10,000 records, which is about two years. After that, a rename needs expand-and-contract (05 §1.8, §3.4).
20. The console's agent writes routines through a training sub-agent. The training kinds go in that sub-agent's `writes` and in the assistant's (05 §2.4, DI).

### Session engine and app

21. The session lives on the phone. It is one event log plus its folded state, rewritten atomically on every change, with `.completeFileProtectionUntilFirstUserAuthentication`. Substrate is the sync target, reached through a retrying outbox and a change-feed cursor. The UI never waits for the network (04 §4.1; 05 §6; 06 DI 1, DI 11).
22. One session is active at a time (06 §4.3, §4.7):
    - it can be resumed on launch, from the tab bar accessory, and from the Live Activity;
    - the conflict dialog offers Resume, Finish and start, Discard and start, and Cancel;
    - Discard is reached only from inside the session, with one confirmation.
23. Logging goes in any order: any set can be ticked. An "Up next" pointer is recomputed after every change, in this order: the rest of the chain, the next member of the round, the next set of the exercise, the next exercise with an unfinished set (06 §4.4).
24. The rest is chosen from the set just completed and the set the plan expects next. The choices are warm-up rest, intra-set pause or side switch, transition, round rest, block rest, then set rest. If the lifter moves to another set, the rest keeps counting. A drop chain has no rest timer (02 §3.3; 06 §4.5).
25. Rest ends at an absolute `Date`. One `RestCoordinator` is the only writer. It updates the file, the notification, the optional alarm and the Live Activity in a fixed order, with a revision guard and timestamped Live Activity updates. It offers ±15 s and Skip, and −15 s never moves the end before now (04 §2.4; 06 §5 item 2).
26. The rest-end alert is one time-sensitive local notification with a fixed identifier that names the next set. It is replaced on every change and hidden while the session screen is visible. Intra-set pauses, side switches and transitions under about 20 s use in-app haptics only (04 §2.3; 02 §10; 06 §4.5).
27. A Live Activity starts with the session. It shows the exercise, Up next, the session clock, the countdown, ±15 s, Skip and Done. Its `staleDate` is the rest's end, so it never shows a live 0:00 (04 §2.3; 06 §1.11, DI 7).
28. Set rows (06 §1.3, §4.1, V1 items 7 and 8):
    - the columns follow the exercise's measurement type;
    - the grey placeholder values come from the suggestion, else the routine, else last time, and the check commits them;
    - a PREVIOUS value can be tapped to copy it into the row.
29. The next-set suggestion follows 03 Part B. It is one pure function in a Foundation-only package, re-run on every change, never committed on its own, and shown with a one-line reason (03 §8, DI 7, DI 8; 06 §4.8).
30. Entry validation: a set whose e1RM is outside 0.5 to 1.5 times the recent median asks "did you mean …?" before the set reaches history (03 DI 10; 06 V1 item 19).
31. Timed sets count down to their target by default, with a stopwatch as an option, and keep counting past zero. They use the same coordinator as rests (02 §2.8; 04 §2.4; 06 V1 item 14).
32. Counting (02 §6.2; 06 §5 item 13):
    - warm-ups stay out of hard sets, PRs, e1RM and volume by default, as a visible setting;
    - a chain counts as one set;
    - muscle volume counts 1 for a primary muscle and 0.5 for a secondary one, as a versioned derived view.
33. Finish (06 §4.6; 04 §3.6):
    - one dialog for unticked sets;
    - editable start and end times, then a summary;
    - writes to substrate and to Health go through the retry queue;
    - Finish never waits on Health.
34. Apple Health (04 §3; 05 §5.2; 01 §8.3):
    - one `HKWorkout` per session through `HKWorkoutBuilder`, `traditionalStrengthTraining` (a routine may override it), indoor;
    - metadata: `HKMetadataKeyExternalUUID` and `HKMetadataKeySyncIdentifier` set to the session id, `HKMetadataKeySyncVersion` set to its revision, and the time zone;
    - no energy and no per-exercise detail; the session RPE as the effort score, if the owner wants one;
    - the returned UUID is stored on the session, and `session.workout` points at `health/workout/applehealth/<account>/<UUID>`;
    - Spotter never writes `health/workout` itself.
35. Code layout (04 §5, §6, DI):
    - a Foundation-only `SpotterKit` package holds the model, the session state machine, the rest rules, the suggestions, and the substrate client with its outbox;
    - the app target and a widget extension hold only the platform code;
    - no App Group is needed.
36. Interface (04 §1; 06 §6):
    - three tabs: Workout, History, Exercises;
    - the in-progress bar is a `tabViewBottomAccessory`, and the session is a full-screen cover that zooms from it;
    - glass goes only on the system bars, menus, sheets and the floating rest bar;
    - the set grid stays dense content.
37. One set label scheme everywhere: W, 1, 2, 2a for a drop, M for myo. One formatter for every duration (06 §5 items 6 and 14).
38. Release (04 §6; repository `AGENTS.md`):
    - the app is listed in `apps/apple/apps.json`, wired to the shared updater, and released with `mise run apple:release`;
    - the app needs the HealthKit and time-sensitive entitlements;
    - the widget's version keys use the same variables as the app's.
39. Importers, when they come (01 §8.2; 02 §7 item 28; 03 DI 11; 05 §3.5):
    - each writes one set record per upstream set, at an id composed from the upstream key;
    - each keeps the source's own values beside the mapped ones;
    - each asks for the unit when the file does not say.

## Where the documents disagree

1. **How to store effort.** 01 keeps RPE and RIR as two fields and converts at read. 02 stores one canonical RIR plus the entered value and its scale. 05's sketch has `rpe` only, and 03's engine takes RPE. **Taken: 01, with 02's scale tag** (requirement 8). A converted value is derived data. Hevy's 9.5 means "maybe one more rep" and Boostcamp's means "no more reps, slightly more load" [web], so converting at write would lose which was meant (02 §5.2, §5.5).
2. **Set types and structured sets.** 01 has one type enum with a `continues` link. 02 has four independent axes, with segments as child rows of one set. 03 has seven kinds. 05 has a five-value `setType` and leaves the semantics open. 06 has chains. **Taken: 02's axes as separate top-level fields, with segments as linked set records (01, 06) rather than embedded rows** (requirements 6 and 7). The reason is that nested values cannot be filtered or aggregated [src] (05 §1.3, §1.4). 03's kinds, and its `repsLeft` and `repsRight`, are derived from these fields at read.
3. **Where performed rest lives.** 01 records the rest before the set, and so does its FHIR sketch. 05 has `restAfter`. 02 wants an interval. **Taken: rest before, on the set** (requirement 9). It is known when the set completes, so each set is still written once, which is 05's own argument for one record per set (05 §3.5 item 3). The interval follows from the previous set's end and this set's start.
4. **Group rests on every slot.** 05's slots each carry `rest` and `restAfterGroup`. 02 keeps the transition per member and the round rest per group. **Taken: 02's semantics, with the round rest stored once per group** (requirement 4). Copied onto every member, one group value can disagree with itself. Kept only on the last member, it breaks when the group is reordered, which is Hevy's failure (02 §3.4).
5. **Re-saving the Health workout after an edit.** 01 and 04 use the sync identifier so that an edit replaces the workout. 05 says to avoid re-saving, because substrate keeps the old mirror row. **Taken: set the sync keys on the first save, but do not re-save after an edit in V1** (requirement 34). Replacement is unverified on a device (04 §3.3), and the cleanup of the old mirror row needs an answer from the owner (question 27).
6. **How the Health copy links back.** 01 expects the adapter to link through `HKMetadataKeyExternalUUID`. 06 proposes a join on time overlap or a marker. 04 found that Substrate Sync does not read that key, and 05 found that the only metadata it forwards is `HKTimeZone` [src]. **Taken: 05.** Spotter stores the UUID and points at the `health/workout` id computed from it. Forwarding the external UUID needs a separate Substrate Sync change (question 28).
7. **Copying plan targets onto sets.** 01 and 03 require it. 05 leaves it as an open question. **Taken: required** (requirement 10). Without the copy, 03's handling of missing RPE and its "same rep scheme" test fall back to defaults (03 DI 1).
8. **Which rests notify.** 02 skips notifications for pauses under about 20 s, by rest slot. 04 skips rests under about 10 s. **Taken: 02, by slot** (requirement 26). With a 10 s cut-off, the default 15 s myo pause and 15 s transitions would raise system notifications while the phone is in the lifter's hand.
9. **The default rest inside a superset.** 02 picks it by the group's purpose: 0 for the same muscle, 15 s for antagonists, 1:00 for paired sets. 06 uses 0 throughout. **Taken: the routine's value first, then 02 §4.4 by purpose, then 0 when no purpose is given.**
10. **The suggestion rule.** 06 §4.8, marked as a proposal, holds the load when sets fall below the range and moves at most one step within a session. 03 lowers the load to reach the bottom of the range (V7), and caps a raise at one step or 10 %. **Taken: 03**, which has a specification and test vectors that were re-run independently. The hold-the-load variant is question 18.
11. **Warm-up suggestions.** 02 generates a percentage ramp. 03 copies last time's warm-ups. 06 puts the generator in V1.1. **Taken: copy last time's warm-ups in V1, and generate a ramp in V1.1.**
12. **AlarmKit.** 04 ships it in V1 as an opt-in setting. 06 puts it under "Later". **Taken:** the coordinator supports it from the start (04 §2.4). The setting ships in V1 only if the owner's phone is on silent at the gym (question 10).
13. **Session RPE.** 04 asks for it at Finish in V1. 06 lists it for V1.1. 01 asks whether to ask for it or derive it. **Taken: the field is in the V1 model.** The prompt depends on question 2.
14. **The kind name `routine`.** 05's sketch uses it. 06 says not to, because Mneme's `health/routine` means a recurring habit. **Taken: 06.** Prefer `workouttemplate`, the name Mneme v6 itself used until T-110 [src] (06 DI 10), with "Routines" in the interface. The owner decides (question 30).
15. **Energy in Apple Health.** 01 offers a MET-based estimate as an option. 04 writes none. **Taken: 04.** A guess would count in Activity totals as if it were measured (04 §3.4).
16. **`codes` or `codings`.** 01 says `codes`, 05 says `codings`. **Taken: `codes`**, the first-party name in `observations/metric`, following 05's own rule to reuse first-party names.

## Still to check on a device

The documents mark these [unverified]. The design keeps a fallback for each until it has been tested.

- Whether a workout saved again with the same sync identifier and a higher version replaces the earlier one, and whether its heart-rate samples survive (01 §3.1; 04 §3.3).
- Whether the time-sensitive capability is provisioned through the App Store Connect API key. If it is not, ship `.active` notifications first (04 §6).
- Whether a stale Live Activity re-renders on time without the app running, and how its buttons behave in the watch's Smart Stack (04 DI; 06 §5 item 3).
- Whether a Spotter workout earns Exercise ring minutes, and whether the effort score shows in the Fitness app (04 DI; 01 §3.1).

## Open questions for the owner

Each question merges the same question from several documents. The document's own number follows the section, as in "01 Q6".

Logging and effort

1. RPE from 6 to 10 in half steps with the reps-in-reserve text, as Hevy shows it. Include 6.5 (Strong has it, Hevy does not), values below 6, or RIR wording? (01 Q6; 02 Q1; 06 Q4)
2. Ask for a session RPE at Finish and write it as Apple's effort score, or derive one from the sets, or skip it? (01 Q3; 02 Q11; 04 Q5; 06 Q13)
3. Record a failed rep attempt as well as the completed reps? Log unilateral work per side, or as one number? Log tempo? (02 Q6, Q8; 01 Q7)
4. Pounds ever, or kilograms only? Where should the kg or lb display preference live? (01 Q1; 05 Q13; 06 Q3)
5. Which gyms, bars, plates, dumbbell steps and stack steps? Round down or to the nearest step, and which way on assisted machines? (02 Q12; 03 Q14; 06 Q3)

Rest and timers

6. Should warm-ups have their own rest (proposed 45 s)? Should they count toward volume, sets and PRs? (02 Q3; 06 Q5)
7. Defaults for the rest inside a superset and after a round? Do warm-ups run before the first round? Is there a round rest after them? (02 Q4; 06 Q7)
8. Start a rest timer after the last set of an exercise, as Hevy does? (02 Q5)
9. Should timed sets count down by default? Should a timed set complete itself when the countdown ends? (04 Q7; 06 Q9)
10. Is the phone usually on silent at the gym? If so, should the loud AlarmKit rest alarm be on by default? (04 Q2; 06 Q10)
11. Keep the screen awake during a session by default? (04 Q6; 06 Q16)

Methods

12. Which myo-rep stop rule: Fagerli's (stop when a mini-set loses a rep from the first), a fixed target, a total, or RP's match? Show the pause as breaths or seconds? Log every mini-set (proposed)? (02 Q2; 03 Q10; 06 Q8)
13. Are EMOMs, circuits for time or Tabata intervals needed? Percent-of-1RM loads? Are routine loads a seed or binding? (02 Q9, Q10; 03 Q8)
14. What makes a set "hard" for weekly volume: RIR 4 or less, RIR 3 or less, or every working set? (02 Q7)

Suggestions

15. When the routine says nothing, use 8 to 12 reps at RPE 8, or RPE 9, as the owner's Hevy logs suggest? (03 Q1)
16. Let history raise the load one step only, or several steps? (03 Q2)
17. Change the load inside a session, capped at 10 % upward, or keep one load and change only reps? (03 Q4)
18. When reps fall below the range, cut the load (proposed) or repeat it once? (03 Q3; 06 §4.8)
19. Accept the heuristic constants? They are the fatigue allowance, the long-break cuts, 5 s per session for timed sets, and 20 % drops rounded down. (03 Q5, Q11 to Q13)
20. Where should PREVIOUS come from: the last time anywhere (proposed), or the last time in the same routine? (03 Q6; 06 Q6)
21. Keep machine and cable history per gym? How should setups be named and linked? (03 Q7)
22. Use the latest HealthKit body mass on or before the session day? What body share for push-ups and dips? (03 Q9)

Apple Health

23. Is an Apple Watch worn at the gym, with Apple's Strength workout started on it? If so, link to that workout instead of writing a second one. Is a chest strap used? (04 Q1, Q3)
24. Should the workout carry the routine name as its brand name? (01 Q5)
25. Is read-time deduplication enough against WHOOP's strength workout, or should it be linked to the session? (05 Q6)
26. Should Spotter ever write `health/workout` itself? Which of the two rows is `supporting` on timelines? (05 Q3, Q10)
27. After an edit, should the Health workout be saved again, and may the old mirror row be deleted? (05 Q4)
28. Should Substrate Sync forward `HKExternalUUID` and the source bundle id (a substrate pull request)? (05 Q5)

Substrate and the app

29. Is one record per set acceptable, at about 5,000 records a year and two reads to render a session? (05 Q2)
30. Which names: package `training`, `strength` or `gym`; kind `routine` or `workouttemplate`; and `session`? (05 Q1; 06 DI 10)
31. Store derived values on records (set volume, e1RM, session tonnage), or compute them in clients? (05 Q7)
32. Record planned sets that were skipped? On discard, delete everything or keep an abandoned record? (05 Q8; 06 Q1)
33. Should the exercise catalogue be written by the agent as needed, or seeded from free-exercise-db? Equipment first or movement first in names? Where do images come from? (01 Q8; 05 Q9; 06 Q14)
34. Should Spotter use its own token, or share Substrate Sync's sign-in through the Keychain? (04 Q8; 05 Q11)
35. Should edits during a session, or suggestions, ever be proposed back to the routine for the agent to review? (06 Q11, Q12)
36. Should a second device, such as an iPad, see the open session live? (06 Q2)
37. Which history to import first: Hevy, Strong or none? Is there older data in Fitbod, FitNotes or Garmin? Would a FIT export to Garmin Connect help? (01 Q2, Q9; 05 Q12)
38. PR celebrations: a banner and medal as Hevy shows, or something quieter? (06 Q15)

## Sources

- [01-formats-and-standards.md](01-formats-and-standards.md), [02-training-methods.md](02-training-methods.md), [03-progression.md](03-progression.md), [04-ios-platform.md](04-ios-platform.md), [05-substrate-fit.md](05-substrate-fit.md), [06-app-teardown.md](06-app-teardown.md). Each lists its own primary sources.
- Prior research in substrate at main `ffd7b967`: [docs/plans/health-data/02-workouts.md](https://github.com/geoah/substrate/blob/ffd7b967/docs/plans/health-data/02-workouts.md) and [06-substrate-fit.md](https://github.com/geoah/substrate/blob/ffd7b967/docs/plans/health-data/06-substrate-fit.md).
- [Hevy API OpenAPI document](https://api.hevyapp.com/docs/) (version 0.0.1).
- [Garmin FIT JavaScript SDK](https://github.com/garmin/fit-javascript-sdk) (profile 21.218.0).
- [androidx Health Connect release notes](https://developer.android.com/jetpack/androidx/releases/health-connect).
- [HL7 PHR IG, PGHD Workout profile](https://build.fhir.org/ig/HL7/personal-health-record-format-ig/en/StructureDefinition-pghd-workout.json).
- [HL7 Physical Activity IG](https://hl7.org/fhir/us/physical-activity/).
- [wger manager models](https://github.com/wger-project/wger/tree/master/wger/manager/models).
- [Terra strength training](https://docs.tryterra.co/unified-api/strength-training).
- [openweight](https://github.com/radupana/openweight).
