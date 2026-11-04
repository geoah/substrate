# Training methods, set structures, rest and effort

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

Spotter research, part 02. Date: 2026-10-30.

The question: which set structures and training methods must Spotter's data model and session engine support, and what does the evidence say about rest times and effort ratings?

How to read the evidence markers:

- **[web]** checked today on the web or in a primary artefact (a paper, an API spec, a vendor help article, or one of the owner's Hevy screenshots).
- **[sdk]** checked today in the installed iOS 27.2 SDK.
- **[src]** checked today in source code on this machine or in a public repository read today.
- **[recalled]** from memory, not re-checked today.
- **[unverified]** could not be confirmed, or comes from a secondary source only.

This builds on `docs/plans/health-data/02-workouts.md` in the substrate repository (dated 2026-10-08, read at `/tmp/spotter-substrate-main`). That document covers platforms, file formats, exercise taxonomies and metrics. Its section 3 ends with a list of "what a set needs" across sources, and its section 5 lists e1RM and "hard sets per muscle per week". This document does not repeat that work. It adds the method semantics, the rest semantics, the evidence on rest, effort and volume, and a requirements list. Where it leans on the earlier document, it re-checked the claim today (Hevy's API, the FIT profile, Health Connect's targets) and says so.

## 0. Findings that shape the design

1. **Rest is not one number.** A session has at least seven distinct rest slots: between sets of one exercise, after warm-up sets, the transition between exercises inside a group, the rest after a full round of a group, the pause inside a set (myo-reps, rest-pause, clusters, drop changes), the switch between sides, and the rest after a block before the next exercise. None of the apps studied models all of them. Hevy models one rest per exercise and asks users to emulate round rest by putting the rest on the last exercise of a superset [web]. wger models one rest (with an optional upper bound) per exercise entry [src]. RP Hypertrophy has no superset function at all [web].
2. **Most "set types" in apps mix four independent things**: the role of the set (warm-up or working), its structure (straight, drop, myo-reps), its termination rule (reps, AMRAP, to failure, time) and its outcome (did the lifter fail a rep). Hevy's `warmup`, `normal`, `failure`, `dropset` enum [web] and wger's nine `ExerciseType` values [src] both do this. Spotter should keep the four apart and map vendor types onto them on import.
3. **Structured sets need child rows.** Myo-reps, rest-pause, drop sets, cluster sets and per-side work are one set made of several segments, each with its own reps, load and pause. RP logs only the total reps of a myo-rep set [web]; Hevy stores drop sets as independent rows with no link to the set they continue [web]. Both lose information that the suggestion engine and set-counting rules need. wger links drop-set rows with a `next_log` pointer [src].
4. **Rest evidence points one way but weakly.** For hypertrophy, rest longer than about 60 to 90 s gives a small benefit with wide uncertainty, and nothing measurable beyond 90 s (Singer et al. 2024) [web]. For strength in trained lifters, more than 2 min (Grgic et al. 2018) [web]. The mechanism appears to be preserved volume load (Longo et al. 2022) [web]. The 2026 ACSM position stand found no effect of short versus long rest on strength at the level of pooled reviews and too little data for hypertrophy [web]. Defaults should therefore be generous (2 to 3 min for compounds, 1.5 min for isolation), editable, and the engine should never punish a longer rest.
5. **Effort should be stored as reps in reserve and shown as RPE.** The RIR-based RPE scale maps RPE 10 to 0 RIR, 9 to 1, and so on (Zourdos et al. 2016) [web]. Apps disagree on the edges: Hevy offers 6 to 10 without 6.5 [web], Strong offers 6 to 10 with 6.5 [web], wger stores RIR and derives RPE as 10 minus RIR [src], RP shows only RIR targets [web]. People misjudge RIR by about one rep on average, more on high-rep sets and far from failure (Halperin et al. 2022; Zourdos et al. 2021) [web]. A target of 2 to 3 RIR is enough for most adults (ACSM 2026) [web].
6. **Volume counting needs data the log must keep.** The best-fitting model counts a set as 1 for the target muscle and 0.5 for an indirectly trained muscle (Pelland et al. 2025) [web]. Hevy uses the same 1 and 0.5 rule but counts warm-up sets [web]. Counting needs role, structure, effort and the muscle map on every logged set.

## 1. Vocabulary used below

- **Set**: one planned or performed unit of work on one exercise, ended by rest or by moving to another exercise. A set may contain segments.
- **Segment**: a part of a set separated from the next part by a short pause or a load change: a myo-rep mini-set, a rest-pause mini-set, a drop, a cluster, or one side of a unilateral set.
- **Slot**: one exercise inside a group, with its ordered list of sets. (wger calls this a slot entry [src].)
- **Group** (block): one or more slots executed together. A group with one slot is a plain exercise. A group with two or more slots is a superset, tri-set, giant set, circuit or paired set.
- **Round**: one pass through every slot of a group, one set each.
- **Role**: warm-up or working. **Structure**: straight, drop, myo, myo-match, rest-pause, cluster. **Termination**: what ends the set (reps, rep range, AMRAP, failure, time, distance, total reps, RIR). **Outcome**: what happened (reached failure, failed a rep attempt).

## 2. Methods as the session engine must execute them

Each method is described in the same four parts: what a routine prescribes, what the engine does, what the log records, and what the app generates on its own.

### 2.1 Straight sets, including rep ranges, top sets and back-off sets

The default: a fixed number of sets of one exercise, with the same rest between sets.

- **Prescribed**: number of sets; per set a rep target (exact or a range such as 8 to 12) and a load target (absolute, percent of a reference max, or "pick a load for the effort target"); optional effort target; rest between sets. Per-set targets, not per-exercise targets, so that pyramids, ramps and a top set followed by back-off sets fit the same shape. Hevy routines carry targets per set (`rep_range`, `weight_kg`) and rest per exercise (`rest_seconds`, "the rest time in seconds between sets of the exercise") [web: Hevy OpenAPI].
- **Engine**: shows the next set with its targets and the previous performance; on completion, starts the set rest timer and schedules a notification for its end.
- **Logged**: reps, load, effort, start and completion time, the rest actually taken before the next set.
- **Generated**: the suggested load and reps (from earlier sets this session and earlier sessions). Back-off loads from a top set (work up to, say, 3 reps at RPE 9, then keep doing sets of 3 until 5 % less load is again RPE 9) are the Reactive Training Systems "fatigue percent" method [web, secondary: EliteFTS summary of Tuchscherer's manual]; Spotter can generate those later (section 9).

### 2.2 Warm-up and ramp sets

Lighter sets before the working sets of an exercise, to rehearse the movement and approach the working load.

- **Evidence**: in a crossover study, specific, aerobic or combined warm-ups did not change the number of reps performed at 80 % 1RM to failure compared with no warm-up (Ribeiro et al. 2014) [web]. Warm-ups are for readiness and technique, not for volume. They are not hard sets.
- **Prescribed**: either explicit warm-up sets, or a rule ("ramp to the first working load"). RP's guide suggests three ramp sets for the first exercise of a muscle: 12 reps with a load you could lift about 30 times, 8 reps with a 20RM load, 4 reps with a 10RM load, and fewer or none for later exercises [web: RP help]. Hevy and Strong ship a percentage-based warm-up calculator with editable steps; Hevy's also rounds to plate and dumbbell increments [web].
- **Engine**: warm-up sets carry their own rest (short or none). Strong lets each exercise have a separate rest for warm-up and working sets [web]. In a superset, warm-up sets of each member run before the first round, not inside it [unverified: no app documents this; it is how the rounds stay meaningful].
- **Logged**: as performed sets with role `warmup`. RP tells users not to log warm-ups because its algorithm treats every logged set as a working set [web]. Strong excludes warm-ups from charts and metrics [web]. Hevy counts warm-ups in its "sets per muscle" statistic and, when a setting is on, in volume and records [web]. Spotter should log them and exclude them from hard-set counts, volume load, PRs and e1RM.
- **Generated**: the ramp, from the first working load of the session: for example 50 % × 8, 70 % × 4, 85 % × 2 (rounded to the available plates or dumbbells), with fewer steps when the working load is close to the empty bar, and none for isolation exercises late in a session [recalled: common practice; the percentages are a proposal, not evidence].

### 2.3 Failure sets and AMRAP sets

Two different ideas that apps merge.

- **AMRAP** ("as many reps as possible") is a termination rule: the rep target is open. 5/3/1-style programs write it as "5+" [recalled]. Boostcamp's program builder offers reps, rep range, rep max, AMRAP, time and time range as set targets [web]. Health Connect's planned steps have an `AmrapTarget` [web: androidx source].
- **Failure** is an outcome: the set ended because another full rep was not possible. The 2026 ACSM stand defines it as "completing repetitions until volitional failure occurs when concentric movement is no longer possible" [web].
- **The apps log failure differently.** Hevy: "log the last completed rep, not the rep that you failed to complete", and a value of 0 reps is not allowed [web]. Strong: a set marked Failure with 5 reps means a 6th rep was attempted and missed, and a missed single is 0 reps marked Failure [web]. These are not the same fact. Spotter must store completed reps and a separate "failed rep attempted" flag, so both imports are lossless.
- **Prescribed**: AMRAP (optionally with a minimum), or "to failure" as a termination.
- **Engine**: no rep target to count down; prompts for reps on completion; offers RIR 0 (or RPE 10) as the default effort for a failure set.
- **Logged**: completed reps, `reachedFailure`, `failedRepAttempted`, effort.
- **Evidence**: training to momentary failure is not superior to stopping short for hypertrophy (Refalo et al. 2023) [web], and "completing sets to fatigue (momentary muscular failure) does not enhance gains in strength, hypertrophy, and power" (ACSM 2026) [web].

### 2.4 Drop sets: single, multiple and mechanical

- **Definition**: "performing sets to failure then reducing load and minimal inter-set rest" (ACSM 2026, table 2) [web]; "taking sets to concentric muscle failure at a given load, then making a drop by reducing the load and immediately taking the next set to concentric or voluntary muscle failure" (Sødal et al. 2023) [web]. A 2026 paper proposes a wider definition, "a reduction in the resistive torque of an exercise", with two kinds: **drop weight** (less external load) and **drop length** (a shorter resistance arm, that is, an easier variation) (Nuzzo 2026) [web]. The second kind is what gyms call a **mechanical drop set**: same load, easier variation, for example incline press, then flat, then a close-grip press [web, secondary].
- **Evidence**: no difference in hypertrophy against traditional sets, with some drop-set protocols taking half to a third of the time (Sødal et al. 2023, six studies) [web]. With volume equated, drop sets and rest-pause gave hypertrophy similar to traditional sets (Enes et al. 2021) [web].
- **Prescribed**: number of drops (1 for a single drop set, 2 or more for a multiple or "strip" set); load change per drop as a percentage or an absolute step; per drop a termination (failure, a rep target, or an RIR). A common rule is a 20 to 25 % reduction per drop [recalled: Schoenfeld and Grgic 2018; full text not checked].
- **Engine**: treats the whole drop set as one set. After the first segment, no rest timer: Hevy deliberately does not start the rest timer when the next set is a drop set [web]. A stopwatch can show the change time. The set rest starts after the last drop.
- **Logged**: one set with ordered segments, each with load and reps; the change time between segments.
- **Generated**: the drop loads, rounded to the equipment (plates, dumbbell pairs, one pin on a stack).
- **Mechanical drops**: model as a group of two or three slots with a zero transition and the same load (section 3), not as a new set structure. That keeps exercise-level history honest: each variation's reps stay attached to that variation.

### 2.5 Rest-pause

- **Definition**: "a hard initial bout with one or more very short pauses and additional mini-sets, usually at the same load" [web: Alpha Progression glossary]. Classic versions take the first bout to or near failure, pause 15 to 30 s, and repeat two or three times [recalled]. In a research protocol, rest-pause at 80 % 1RM used short pauses until a fixed total was reached (Prestes et al. 2019; protocol details recalled) [web for the study, recalled for the protocol]. A 2026 meta-analysis of 23 studies found a modest hypertrophy advantage for the rest-pause subgroup, while advanced systems overall did not beat traditional sets for hypertrophy (Tsartsapakis et al. 2026) [web].
- **Prescribed**: initial termination (reps, RIR or failure), pause length, number of mini-sets or a total-rep target.
- **Engine**: after each segment, a short pause timer with an audible cue at the end; stops when the mini-set count or the total is reached.
- **Logged**: segments with reps and the pause actually taken.
- **Generated**: the mini-set rows.

### 2.6 Myo-reps (Fagerli's method)

Myo-reps are a standardised rest-pause method. The method's author describes it in "Myo-reps in English" (2012, edited 2016), read today from the Internet Archive because the page is gone from his site [web]:

- **Activation set**: "Pick a load you can perform 9-20 reps with". The activation set goes close to failure; "total failure isn't an absolute requirement, and leaving a rep or two in the tank will allow you to do more total reps" [web].
- **Pause**: "3-5 deep breaths", where "3 deep breaths (in+out) is about 6-8 seconds, 5 is 10-15 seconds" [web]. The pause is tuned: if results are consistently low, use 6 to 7 breaths; if they are consistently high, 1 to 2 breaths [web]. Heavier loads tolerate up to 30 s; lighter loads need 5 to 15 s [web].
- **Mini-sets**: "up to 3-5 short mini-sets of 3-5 reps (staying close to failure on each mini-set)" [web].
- **Stop rule**: "End the Myo-rep set when you lose 1 rep from the initial or 5 mini-sets." His correct examples are `20 +4+4+4+3`, `22 +3+3+3+3+3` and `18 +5+4`; his wrong ones are `20 +4+4+4+3+3` and `18 +5+4+3+2` ("stop when you lose a rep from the initial") and `22 +3+3+3+3+3+3+3+3+3` ("stop at 5 sets of 3 reps") [web]. The examples show that "the initial" means the first mini-set, and that the mini-set which loses the rep is the last one done.
- **Notation**: `12-15 +3-5x` means an activation set of 12 to 15 reps followed by mini-sets of 3 to 5 reps [web].
- **Total-reps variant**: "you can prescribe a total number of reps for an exercise, and I would recommend that you get at least 10 reps after the activation set. You generally just keep doing mini-sets of 2-5 reps until you hit the prescribed total rep count" [web].

**Myo-rep match.** In RP Hypertrophy, a "myorep match" set uses the same 5 to 15 s pauses but performs "as many mini sets as needed to match the reps from your first set", for example 15 reps in set one, then mini-sets on set two until 15 total [web: RP help]. The same idea is used for unilateral work: do the weaker side, then match its reps with the other side [web: RP help on dumbbell movements]. Secondary sources attribute "myo-rep match" to Fagerli [unverified: not found in his 2012 article]. Other secondary sources disagree on the numbers: 8 to 15 or 12 to 20 activation reps, 20 to 30 s pauses, stop below 3 reps [web, secondary].

What the engine must do:

- **Prescribed**: activation rep range and effort (default 1 to 2 RIR); mini-set rep target (default 3 to 5); pause (default 5 breaths, shown as 15 s); maximum mini-sets (default 5); stop rule (`repDrop`: stop when a mini-set falls below the first mini-set; `belowTarget`: stop when a mini-set falls below the target; or `totalReps: n`). For myo-match: the total to match, taken from a referenced earlier set.
- **Engine**: after the activation set, a short pause timer (with a breath count or a seconds countdown, and a haptic at the end), then a mini-set row prefilled with the target; after each mini-set, apply the stop rule and either spawn the next mini-set or close the set and start the normal set rest. For myo-match, show the running total against the target.
- **Logged**: the activation set and every mini-set as segments with reps and the pause actually taken. The Alpha Progression glossary says the same: "Record the activation set, every mini-set, pause duration, and load", and warns that each mini-set "should not automatically be counted as a full normal set" [web]. RP logs only the total reps [web], which loses the structure.
- **Generated**: the mini-set rows, their targets and the stop decision.

### 2.7 Cluster sets and rest redistribution

- **Definitions**: "Cluster: traditional inter-set rest with preplanned intraset rest periods"; "Rest redistribution: short rest periods between each repetition" (ACSM 2026, table 2) [web]. Terminology in the literature is not uniform (Tufano et al. 2017) [web].
- **Evidence**: no differences from traditional sets in strength, power, velocity, hypertrophy or endurance across 29 studies, with less fatigue during the session (Davies et al. 2021) [web]. ACSM 2026 found strength unaffected by set structure [web].
- **Model**: the same shape as rest-pause, except the pauses are planned before the set starts and each cluster has a fixed rep count (for example 4 clusters of 2 reps with 20 s between) [recalled for the example]. Typical intra-set rests are 10 to 45 s [recalled].
- **Engine**: segments with a planned pause each. Low priority for V1 (section 9), but the data model should not need changes to add it.

### 2.8 Timed sets, holds and isometrics

- **Examples**: plank, side plank, wall sit, dead hang, weighted plank, isometric holds at a joint angle. Hevy has exercise types `duration` and `weight_duration` [web: Hevy OpenAPI and records article].
- **Engine modes**: (a) countdown to a target duration, with an end alert; (b) stopwatch for an open hold or a hold to failure. Hevy's inline timer is a stopwatch: "if you type a time into this section and hit play ... it will act as a timer, but this is not the case. This play option actually acts as a stopwatch" [web]. The owner's screenshots show it counting up from 00:00 while the previous value was 01:02 [web: owner's screenshot]. For a countdown, Hevy users must open a separate timer [web]. Spotter should offer both, with countdown as the default when a target exists, and keep counting past zero so an overrun is recorded.
- **Prescribed**: duration target or range, optional added load, optional per side (side plank).
- **Logged**: actual duration (seconds), load if any, side, start and end time. RPE is optional; Hevy shows no RPE column for duration exercises [web: owner's screenshot].
- **Generated**: a lead-in countdown (for example 3 s) before the hold timer starts [recalled: common in timer apps; a proposal].

### 2.9 Distance and loaded carries

- **Examples**: farmer's carry, suitcase carry (per side), sled push or pull, sandbag carry. FIT has a `carry` exercise category (with names such as `farmersWalk`) and a `sled` category [web: FIT JavaScript SDK `profile.js`]. Hevy's exercise types include `distance_duration` and `short_distance_weight` [web: Hevy OpenAPI `CustomExerciseType`].
- **Prescribed**: distance (metres) or time, load per implement, number of implements.
- **Logged**: distance, duration, load and the load basis (per hand or total).
- **Engine**: stopwatch; distance entered by hand (no GPS indoors).

### 2.10 Unilateral work (per side)

- **Examples**: single-arm row, Bulgarian split squat, single-leg press, side plank, suitcase carry.
- **How others do it**: Health Connect splits sides into separate segment types (for example `EXERCISE_SEGMENT_TYPE_DUMBBELL_CURL_LEFT_ARM` and `EXERCISE_SEGMENT_TYPE_DUMBBELL_CURL_RIGHT_ARM`) [web: androidx source]. FIT encodes side in the exercise name, for example `singleArmDumbbellBenchPress` and `singleArmCableRow` (19 `singleArm*` names in profile 21.218.0) [web: FIT JavaScript SDK `profile.js`]. Hevy and Strong log one exercise with no side field (earlier research, section 3). RP's advice: enter the weight of one dumbbell; "start with the weaker side and work towards the target RIR/Reps given. Match that number of reps with the stronger side", and enter 6 reps, not 12, for 6 per side [web].
- **Prescribed**: a unilateral flag on the slot, a side order (left first, right first, weaker first) and per-side targets. Alternating reps (left, right, left, right in one set) is a second mode.
- **Engine**: one set with two side segments and a side-switch pause (default 0). With "match", the second side's target is the first side's actual reps.
- **Logged**: reps and load per side. One unilateral set (both sides) counts as one set for each side's muscles [unverified: common convention, no source found].

### 2.11 Tempo

- **Notation**: "four-digit combination ... which describes the eccentric, isometric/transition, concentric, and isometric/transition phases", for example `2/0/3/0`; an `X` means "a voluntary explosive action" whose duration is not controlled (Wilk et al. 2021) [web]. Repetition duration from 0.5 to 8 s gave similar hypertrophy in an earlier meta-analysis [recalled: Schoenfeld et al. 2015], and ACSM 2026 found no difference between fast (0.5 s) and slow (8 s) repetitions for hypertrophy [web].
- **Hevy routines have no tempo field** (earlier research, section 1.9) [web: Hevy OpenAPI checked today, no tempo property]. Boostcamp tells program authors to put tempo in the exercise instructions text [web].
- **Model**: an optional tempo string on the planned set or slot, validated against the four-phase pattern. Display only. Spotter cannot measure tempo without sensors (section 9).

### 2.12 EMOM, AMRAP for time and timed intervals

These are group-level time structures, mostly from conditioning and CrossFit-style training. FIT has `amrap`, `emom` and `tabata` sub-sports (earlier research, section 4.5).

- **EMOM** ("every minute on the minute"): start a set at the top of each interval; rest is what remains of the interval. Variants use 2- or 3-minute intervals, or rotate exercises each minute [recalled].
- **AMRAP for time**: as many rounds of a circuit as possible within a time cap; the result is rounds plus reps [recalled]. Not the same as an AMRAP set (section 2.3).
- **Tabata**: the original protocol is seven to eight bouts of 20 s at about 170 % of VO2max with 10 s rest between (Tabata et al. 1996) [web].
- **Model**: group kinds `emom` (interval length, number of intervals, work per interval) and `amrapTime` (time cap). Rest is derived, not prescribed. Low priority for V1 (section 9).

## 3. Grouping

### 3.1 Group kinds

- **Superset**: "two exercises with little or no rest between them"; "after completing all exercises in a superset, a rest period is taken before starting the next superset" (Zhang et al. 2025) [web]. Subtypes by pairing:
  - **Agonist-antagonist** (row and press, curl and triceps extension).
  - **Similar biomechanical** or **agonist**: same muscle, also called a compound set (bench press and dumbbell press).
  - **Pre-exhaust**: an isolation exercise immediately before a compound for the same muscle (leg extension, then leg press). It did not increase target-muscle EMG in a seated-row study (Fujita et al. 2022) [web], and gave adaptations similar to traditional training with volume load equated in a preprint (Keskin et al. 2024, not peer reviewed) [web].
  - **Alternate peripheral**: one upper-body and one lower-body exercise (Zhang et al. 2025) [web].
- **Paired sets** (agonist-antagonist paired sets, also called alternating sets): "the coupling of agonist and antagonist exercises, performed in an alternating manner with rest intervals between each set" (Robbins et al. 2010) [web]. This is the case where the rest between members is a full rest, not a transition.
- **Tri-set**: three exercises in sequence; **giant set**: four or more [recalled]. Tri-sets and supersets were more time-efficient than traditional sets but raised lactate (tri-sets also raised perceived exertion), and jump performance was still possibly to likely reduced 24 h later (Weakley et al. 2017) [web].
- **Circuit**: "a sequence of exercises performed one after another with little to no rest between them, followed by a rest period after completing one full round" [web: Hevy help]. Strong calls a group of three or more a circuit [web]. ACSM 2009 recommends that circuit rest "correspond to the time needed to get from one exercise station to another" [web]. Circuit training increases hypertrophy compared with no training (ACSM 2026) [web].

Evidence for supersets as a whole: across 19 studies, supersets matched traditional sets for total reps and volume load, shortened sessions, raised lactate, energy cost and RPE, and gave similar long-term strength, strength endurance and hypertrophy. Agonist-antagonist supersets increased the reps completed; similar-biomechanical supersets reduced volume load (Zhang et al. 2025) [web].

### 3.2 Rest slots

Every rest that Spotter must be able to prescribe, time and record. Names are proposals.

1. **`setRest`**: between two working sets of the same slot when the slot is not in a group (straight sets).
2. **`warmupRest`**: after a warm-up set. Separate from `setRest` because Strong already distinguishes them [web] and because warm-up rest is short or zero.
3. **`transition`**: inside a round of a group, from one member to the next. Per member, because a tri-set may go A to B in 0 s and B to C in 30 s to move between stations. Zero for compound sets and pre-exhaust; 15 to 60 s for non-competing pairs; a full rest for paired sets.
4. **`roundRest`**: after the last member of a group's round, before the next round. Per group.
5. **`intraSetPause`**: between segments of one set: myo-rep pause, rest-pause pause, cluster rest, drop change time. Per set structure, often in breaths rather than seconds.
6. **`sideSwitch`**: between the sides of a unilateral set. Default 0.
7. **`blockRest`**: after the last set of a slot or group, before the next block starts. Hevy starts the rest timer after every completed set, including the last of an exercise [web]; Spotter should do the same by default, using the slot's `setRest` or the group's `roundRest`, labelled "Rest before next exercise".
8. **Derived rests**: in an EMOM, the rest is the rest of the interval. Not prescribed.

Each prescribed rest needs a duration in seconds, an optional upper bound (wger has `RestConfig`, 0 to 1800 s, and `MaxRestConfig`, 0 to 600 s, per entry [src]), or "open" (no timer). A planned set may override the rest that follows it (for example a longer rest after a top set).

### 3.3 Which rest applies: the rule for the engine

The owner wants the lifter to move through the exercises in any order. A fixed queue does not work. Rest should instead be a function of the set just completed and the set the plan expects next:

- The completed set is a warm-up: `warmupRest`.
- The completed set is a segment of a structured set and more segments follow: `intraSetPause` (or `sideSwitch`).
- The completed set is in a group and another member follows in this round: that member's `transition`. If the transition is 0, no timer; the screen moves to the next member (Hevy's "Smart Superset Scrolling" does this [web]).
- The completed set is the last member of the round: `roundRest`.
- The completed set is the last set of a slot or group: `blockRest`.
- Otherwise: `setRest`.

If the lifter then taps a different set, the running timer keeps counting: rest is time since the last completed set, whatever comes next. Members with fewer sets drop out of later rounds, as wger interleaves them (exercise 1 with 4 sets, 2 with 3 and 3 with 2 gives 1, 2, 3, 1, 2, 3, 1, 2, 1) [src]; the transition skips the missing member and the round rest follows the last member still present.

### 3.4 How apps represent group rest, and where they fall short

**Hevy** [web: help centre articles dated 2026-10-07 to 2026-10-28, API spec, owner's screenshots]

- Grouping: a `superset_id` on each exercise of a routine or workout; any number of exercises per superset; a colour per superset.
- Rest: one `rest_seconds` per exercise, "between sets of the exercise". For a circuit, the help centre says to "set a rest timer after the final exercise to mark the end of one full circuit". In the owner's screenshot, Glute Bridge carries a Superset badge and "Rest: OFF".
- The rest timer starts when a set is marked complete; ±15 s buttons adjust the running countdown; the timer does not start when the next set is a drop set.
- Falls short: no round rest, so the round rest lives on whichever exercise happens to be last, and reordering the superset breaks it. No transition distinct from set rest. No separate warm-up rest. The executed workout stores no rest and no set timestamps (the `Set` schema has `index`, `type`, `weight_kg`, `reps`, `distance_meters`, `duration_seconds`, `rpe`, `custom_metric`), so actual rest cannot be imported or analysed.

**Strong** [web: help centre]

- Grouping: exercises linked into a superset, shown with a vertical line; three or more are called a circuit; the Next button jumps to the next set "from the appropriate exercise".
- Rest: a default of 2:00 for all exercises, editable per exercise, with "a different duration for Warm-up Sets and Working (Regular) Sets".
- Falls short: the help centre says nothing about transitions or round rest in supersets. The CSV export has no rest or set-time columns (earlier research, section 1.9).

**Boostcamp** [web: feature pages updated October 2026]

- Grouping: supersets in the program builder and the tracker; drop sets; AMRAP; RPE, RPE range or percent of 1RM targets.
- Rest: a default rest timer plus per-exercise times in 5 s steps; a Live Activity on iPhone; no Apple Watch app.
- Falls short: program authors are told to put "tempo, rest periods or technique notes" in the exercise instructions text, so rest in a shared program is prose, not data.

**RP Hypertrophy** [web: help centre articles dated 2026-10-14]

- Grouping: "There is no specific function for supersets at this time; you would simply perform the sets for your paired exercises back to back", using notes or reordering.
- Set types: regular, myorep, myorep match. Myorep pauses of 5 to 15 s. Logs total reps only.
- Rest: guidance, not a timer prescription: rest until breathing has settled, you feel ready, supporting muscles are not cramping, and you can do at least 3 to 5 reps on the next set; "anywhere between 30 seconds to 5 minutes".
- Effort: RIR targets set by the app per week; "RIR cannot be changed in the app".
- Falls short: no grouping, no mini-set structure in the log.

**Alpha Progression** [web: vendor glossary and comparison page]

- Supports supersets, drop sets, warm-up sets "and other set types", RIR targets, mesocycles and deloads.
- Its glossary recommends recording "the exercise order, repetitions for each movement, transition time, and rest after the pair" for supersets, and every mini-set and pause for myo-reps.
- How its timer handles superset transitions and round rest: [unverified]; no primary documentation found.

**wger** [src: `wger/manager/models`, commit `dde740753e` of 2026-10-29]

- Grouping: a `Slot` is "a set of exercises"; it is a superset when it holds more than one `SlotEntry`; sets are interleaved round-robin as described in 3.3.
- Rest: `RestConfig` and `MaxRestConfig` per slot entry, with per-iteration progression rules (`operation` replace, plus or minus; `step` absolute or percent; `repeat`).
- Log: `rest` and `rest_target`, `rir` and `rir_target`, `repetitions` and `repetitions_target`, `weight` and `weight_target` on every log row; `next_log` links a drop-set chain.
- Set types: `normal`, `warmup`, `dropset`, `myo`, `partial`, `forced`, `tut`, `iso`, `jump`.
- Falls short: rest is per entry, so inside a superset it is ambiguous whether an entry's rest is the transition or the round rest.

**Formats an importer will meet**

- FIT: rest is its own record. The `set` message has `set_type` `rest` or `active`, with `duration`, `start_time`, `repetitions`, `weight`, `category`, `category_subtype` and `wkt_step_index` [web: FIT JavaScript SDK profile 21.218.0]. Workout steps have an `intensity` (`active`, `rest`, `warmup`, `cooldown`, `recovery`, `interval`, `other`) and duration types including `reps`, `time`, `open` and `repeatUntilStepsCmplt` [web].
- Health Connect: `ExerciseSegment` has `EXERCISE_SEGMENT_TYPE_REST` and `EXERCISE_SEGMENT_TYPE_PAUSE`, so rest is a time-bounded segment [web: androidx source].
- HealthKit: the owner wants one strength workout per session with no per-exercise detail, so rest does not reach Apple Health.

The lesson for Spotter: record performed rest as an interval with a start and an end (as FIT and Health Connect do), not only as a prescribed number on the exercise (as Hevy does).

## 4. Rest: the evidence and the defaults

### 4.1 What the studies say

- **Schoenfeld et al. 2016** [web]: 21 resistance-trained men, 8 weeks, 3 whole-body sessions a week of 7 exercises, 3 sets of 8 to 12RM, 1 min versus 3 min rest. The 3 min group gained more 1RM squat and bench press strength and more anterior thigh thickness, with a trend for the triceps (p = 0.06). Muscular endurance did not differ. One small study.
- **Grgic et al. 2017** [web]: six studies, short (60 s or less) versus long (more than 60 s). "Both short and long inter-set rest intervals may be useful", with a possible advantage of long rest in trained people.
- **Grgic et al. 2018** [web]: 23 studies, 491 participants. "Robust gains in muscular strength can be achieved even with short RIs (< 60 s). However, it seems that longer duration RIs (> 2 min) are required to maximize strength gains in resistance-trained individuals." For untrained people, 60 to 120 s seems sufficient.
- **Singer et al. 2024** [web]: Bayesian meta-analysis, 9 studies with 19 measurements. A "small hypertrophic benefit to employing inter-set rest interval durations >60 seconds"; "our analysis did not detect appreciable differences in hypertrophy when resting >90 seconds between sets". Effect sizes favouring longer rest: arm 0.13 (95 % CrI −0.27 to 0.51), thigh 0.17 (−0.13 to 0.43); whole body −0.08 (−0.45 to 0.29). Training to failure or not did not change the result.
- **Longo et al. 2022** [web]: unilateral knee extension at 80 % 1RM, 1 versus 3 min, with and without volume load matched. The protocols with the higher volume load grew more (about 13 % versus 7 % quadriceps cross-sectional area) whatever the rest. Rest matters through the volume it lets you do.
- **Scudese et al. 2015** [web]: five sets of bench press with an absolute 3RM load. 2, 3 and 5 min gave similar reps, 1 min gave fewer from set 2; RPE before each set was lower with 3 and 5 min than with 1 min. The authors suggest 2 min as a time-efficient minimum and 3 min if lower perceived exertion matters.
- **Ibbott et al. 2019** [web]: with self-selected rest in 5 × 5RM squats, rest grew over the sets and total rest differed between two sessions, while effort ratings stayed stable.
- **ACSM 2009** [web]: the summary gives 3 to 5 min for heavy loading (1 to 6RM) in intermediate and advanced training and for light-load power sets, 1 to 2 min for hypertrophy, and under 90 s for muscular endurance. The body text gives at least 2 to 3 min for core exercises with heavier loads (strength and power); 1 to 2 min for assistance exercises; 1 to 2 min for novice and intermediate hypertrophy, 2 to 3 min for heavy core lifts in advanced training; for muscular endurance, 1 to 2 min for sets of 15 to 20 or more reps and under 1 min for sets of 10 to 15; circuit rest equal to the time needed to change station.
- **ACSM 2026** [web]: an overview of 137 reviews. "Strength was not affected by ... short (<1 min) versus long (>1 min) between-set rest intervals"; for hypertrophy there were "insufficient data to determine if muscle hypertrophy was influenced by ... inter-set rest". It recalls the 2009 recommendation of 2 to 3 min without restating it as current advice.
- **Inside groups**: in antagonist paired sets, no rest, 30 s or 1 min between knee flexion and knee extension gave more extension reps than no prior flexion set; 3 and 5 min did not (Maia et al. 2014) [web]. The superset studies pooled by Zhang et al. 2025 used between 0 and 45 s between the two exercises and 1 to 3 min after the pair [web: extracted from the review's study table].

### 4.2 What this means for Spotter

- Longer rest is never harmful except for session time. Short rest costs reps, and lost reps are the likely mechanism for worse outcomes. Defaults should err long.
- Rest is a prescription the lifter adjusts. Self-selected rest drifts (Ibbott et al. 2019). The timer should show the target, allow ±15 s (as Hevy and the owner's screenshots do), allow skip, and record what actually happened.
- Rest needs to be comparable week to week for suggestions to be fair: "Lifting the same weight after 90 seconds and after 4 minutes are different efforts" [web: Boostcamp]. The suggestion engine should read the rest actually taken before each set.

### 4.3 Default rest between sets by exercise class

Proposed defaults. The class is an attribute of the exercise, overridable per routine slot.

| Exercise class                                                                          | Examples                                                     | Default | Range        | Basis                                         |
| --------------------------------------------------------------------------------------- | ------------------------------------------------------------ | ------- | ------------ | --------------------------------------------- |
| Heavy compound, strength sets (6 reps or fewer, or RPE 8 and above at 80 % 1RM or more) | squat, deadlift, bench press, overhead press                 | 3:00    | 2:00 to 5:00 | ACSM 2009; Grgic 2018; Scudese 2015           |
| Compound, hypertrophy sets (6 to 15 reps)                                               | Romanian deadlift, leg press, rows, pull-ups, dumbbell press | 2:00    | 1:30 to 3:00 | Schoenfeld 2016; Singer 2024                  |
| Isolation and single-joint machines                                                     | curls, lateral raises, leg extension, cable fly              | 1:30    | 1:00 to 2:00 | Singer 2024; ACSM 2009 (assistance)           |
| High-rep small muscles and trunk (15 reps or more)                                      | crunch, calf raise, back extension                           | 1:00    | 1:00 to 2:00 | ACSM 2009 (muscular endurance, 15+ reps)      |
| Timed holds and isometrics                                                              | plank, side plank, wall sit, dead hang                       | 1:00    | 0:30 to 1:30 | no direct evidence [unverified]               |
| Loaded carries and sleds                                                                | farmer's carry, sled push                                    | 2:00    | 1:30 to 3:00 | no direct evidence [unverified]               |
| Power and jumps                                                                         | box jump, medicine-ball throw, power clean                   | 3:00    | 2:00 to 5:00 | ACSM 2009 (power: 2 to 3 min, summary 3 to 5) |
| Warm-up sets                                                                            | ramp sets                                                    | 0:45    | 0:00 to 1:30 | Ribeiro 2014; Strong's separate warm-up rest  |

### 4.4 Default rests inside structures and groups

| Rest slot                                                            | Default                                    | Range                      | Basis                                                                    |
| -------------------------------------------------------------------- | ------------------------------------------ | -------------------------- | ------------------------------------------------------------------------ |
| Drop change (`intraSetPause` of a drop set)                          | 0 (no timer, stopwatch shown)              | 0 to 15 s                  | ACSM 2026 "minimal"; Sødal 2023 "immediately"; Hevy suppresses its timer |
| Myo-rep pause                                                        | 15 s, shown as 5 breaths                   | 3 to 30 s (1 to 7 breaths) | Fagerli (5 breaths 10 to 15 s, up to 30 s heavy); RP (5 to 15 s)         |
| Rest-pause pause                                                     | 20 s                                       | 10 to 30 s                 | [recalled]                                                               |
| Cluster rest                                                         | 20 s                                       | 10 to 45 s                 | [recalled]                                                               |
| Side switch                                                          | 0 s                                        | 0 to 15 s                  | practice [unverified]                                                    |
| Transition, non-competing pair (antagonist, upper with lower)        | 15 s                                       | 0 to 60 s                  | Maia 2014; Zhang 2025 study table                                        |
| Transition, same muscle (compound set, pre-exhaust, mechanical drop) | 0 s                                        | 0 to 10 s                  | the purpose is carried-over fatigue [recalled]                           |
| Transition, paired sets                                              | 1:00                                       | 0:30 to 2:00               | Robbins 2010; Maia 2014 (only 0 to 1 min raised reps)                    |
| Transition, tri-set, giant set, circuit                              | 15 s                                       | 0 to 30 s                  | ACSM 2009 (time to change station)                                       |
| Round rest, superset of compounds                                    | 2:30                                       | 2:00 to 3:00               | Zhang 2025 study table (1 to 3 min)                                      |
| Round rest, superset of isolation exercises                          | 1:30                                       | 1:00 to 2:00               | Singer 2024                                                              |
| Round rest, circuit                                                  | 2:00                                       | 1:00 to 3:00               | practice [unverified]                                                    |
| Block rest (before the next exercise)                                | the finished slot's set rest or round rest | open                       | Hevy runs the timer after the last set too                               |

## 5. Effort: RPE and reps in reserve

### 5.1 The RIR-based RPE scale

Zourdos et al. 2016 tested a resistance-training RPE scale "that corresponded to an RIR value (RPE-10 = 0-RIR, RPE-9 = 1-RIR, and so forth)" in experienced and novice squatters. RPE correlated inversely with bar velocity (r = −0.88 experienced, −0.77 novice), and experienced lifters rated a 1RM higher than novices did [web]. The scale came from powerlifting practice: Helms et al. 2016 note it "was originally created in 'The Reactive Training Systems Manual' in 2008" [web].

The scale as reprinted in Helms et al. 2016, table 1, plus half steps [web; the half steps are not in the paper: 8.5 and 7.5 follow Strong's mapping, and the 9.5 row is Boostcamp's wording, where Strong maps 9.5 to 0 to 1 RIR]:

| RPE    | Reps in reserve | Description                                                                          |
| ------ | --------------- | ------------------------------------------------------------------------------------ |
| 10     | 0               | Maximum effort                                                                       |
| 9.5    | 0               | No further reps, but the load could have been slightly higher (half step, Boostcamp) |
| 9      | 1               | 1 repetition remaining                                                               |
| 8.5    | 1 to 2          | half step                                                                            |
| 8      | 2               | 2 repetitions remaining                                                              |
| 7.5    | 2 to 3          | half step                                                                            |
| 7      | 3               | 3 repetitions remaining                                                              |
| 5 to 6 | 4 to 6          | 4 to 6 repetitions remaining                                                         |
| 3 to 4 | not defined     | Light effort                                                                         |
| 1 to 2 | not defined     | Little to no effort                                                                  |

Helms et al. 2016, table 2, gives percent of 1RM for 1 to 8 reps at RPE 7 to 10 in half steps (for example 8 reps at RPE 8 = 73.0 %, 5 reps at RPE 9 = 83.0 %). Only three cells are measured means from the experienced squatters in Zourdos et al. 2016 (1 rep at RPE 10 = 100 %, 1 rep at RPE 8 = 91 %, 8 reps at RPE 7 = 70 %); the authors call the rest "estimations" and warn "a perfect relationship between percentage 1RM and RIR should not be expected" [web]. This is the table an RPE-adjusted e1RM uses (earlier research, section 5). RPE-based and percent-based loading gave similar strength and hypertrophy over 8 weeks, with a possible small strength edge for RPE (Helms et al. 2018) [web].

### 5.2 What the apps show

- **Hevy** [web: help centre, API, owner's screenshots]: the RPE column is a setting and appears for rep-based exercises only. The keypad offers 6, 7, 7.5, 8, 8.5, 9, 9.5 and 10, with a label per value; the API accepts exactly these values. Labels from the help centre: 6 "could have done 4+ more reps", 7 "3 more", 7.5 "2 more reps, maybe even 3", 8 "2 more", 8.5 "1 more rep, maybe 2", 9 "1 more", 9.5 "could have maybe done 1 more rep", 10 "couldn't have done more reps with proper form". The owner's screenshot shows 6 as "Moderate Effort, Could do 4 more reps" and an empty state "Select RPE, Slide or tap to rate your effort". Hevy's 9.5 means "maybe one more rep", as does Strong's (RIR 0 to 1), which differs from Boostcamp's "no more reps, slightly more load". The previous column shows "35kg x 13 @ 9.5 rpe". RIR is not offered separately: "the RPE scale comes with descriptions that explain how many reps you likely had left".
- **Strong** [web]: RPE 6 to 10 including 6.5, with an explicit RIR mapping: 6 = 4, 6.5 = 3 to 4, 7 = 3, 7.5 = 2 to 3, 8 = 2, 8.5 = 1 to 2, 9 = 1, 9.5 = 0 to 1, 10 = 0. "RPE ratings of below 6 are highly subjective ... so they have not been included."
- **Boostcamp** [web]: 6 to 10 in half steps, each with a reps-in-reserve hint, for example 8 "Could do 2 more reps" and 10 "Could not do more reps or load without form failure". Programs can prescribe an RPE or an RPE range. It advises rating right after the set.
- **RP Hypertrophy** [web]: RIR targets only, set by the app; no RPE logging.
- **wger** [src]: stores `rir` and `rir_target` (one decimal place, capped at 9.5 in progression maths) and derives RPE as 10 minus RIR, mapping any RIR above 5 to RPE 4.
- **Health Connect** [web: androidx source]: planned steps take `RateOfPerceivedExertionTarget(rpe: Int)`, integers 0 to 10 that its KDoc defines on the Borg CR10 scale, not the RIR-based scale; executed segments and sessions take a float RPE from 0 to 10 (earlier research, section 1.8), and `ExerciseSegment` documents that one as CR10 too.
- **HealthKit** [sdk]: `HKQuantityTypeIdentifierWorkoutEffortScore` and `HKQuantityTypeIdentifierEstimatedWorkoutEffortScore` (iOS 18 and later, unit `appleEffortScore`) are samples linked to a workout or one of its activities with `relateWorkoutEffortSample:withWorkout:activity:completion:`, not per set. The 1 to 10 range is [recalled]. Spotter could attach a session-level effort to the one HealthKit workout it writes.

### 5.3 How accurate are RIR estimates?

- **Halperin et al. 2022** [web]: 12 studies, 414 participants. People underpredicted reps to failure by 0.95 reps on average (95 % CI 0.17 to 1.73), with very high heterogeneity. Accuracy improved closer to failure, with fewer reps (12 or fewer), and slightly in later sets; training status made no difference.
- **Zourdos et al. 2021** [web]: squats at 70 % 1RM (about 16 reps to failure). Errors were 2.05 reps at a called RPE 9, 3.65 at RPE 7 and 5.15 at RPE 5. Errors grew with the number of reps in the set.
- **Refalo et al. 2024** [web]: trained men and women on bench press at 75 % 1RM, calling 1 and 3 RIR. Mean absolute error 0.65 reps, slight underprediction, no effect of sex or experience.
- **Remmert et al. 2023** [web]: machine biceps curl, triceps pushdown and seated row at 72.5 % 1RM. Predictions were more accurate closer to failure and in later sets; experience did not help. In a 6-week study, accuracy did not improve with practice, and underestimation grew in high-rep sets.

Implications: RIR is good to about ±1 rep on low-rep compound sets near failure, and poor (±2 to 5 reps) on high-rep sets far from failure. The suggestion engine should weight effort ratings by reps and proximity to failure, and should not take an RPE 6 on a 20-rep set literally.

### 5.4 What a target RPE means

A target effort has two different uses, and the routine must say which:

1. **Load selector** with fixed reps: "5 reps at RPE 8" means choose the load that leaves about 2 reps in reserve after 5 reps [web: Boostcamp's example]. The engine suggests a load from history and the RPE-percent table; the lifter does the 5 reps and rates the set.
2. **Stop rule** with open reps: "AMRAP stopping at 2 RIR" means keep going until 2 reps remain. The engine shows no rep target, only the effort target.

Ranges (RPE 7 to 8, or RIR 1 to 3) are common [web: Boostcamp supports RPE ranges; wger has `MaxRiRConfig`] and should be allowed.

What evidence says about the target itself:

- Muscle growth increased as sets ended closer to failure, while strength gains were similar across a wide range of RIR (Robinson et al. 2024, exploratory meta-regressions with estimated RIR) [web].
- Training to momentary failure was not better than stopping short for hypertrophy; the relationship may be non-linear (Refalo et al. 2023) [web].
- ACSM 2026: "Sufficient effort ... can be accomplished by completing sets with various RTx and completion of 'near-failure' or a target of 2–3 repetitions in reserve (RIR)", and "there is insufficient evidence to quantify exact RIR and perceived exertion targets" [web].

Default working-set target: 1 to 3 RIR for isolation and machine exercises, 2 to 3 RIR for heavy compounds [recalled: common coaching practice consistent with the evidence above; a proposal].

### 5.5 How to record it

- Store one canonical number: **reps in reserve** as a decimal in half steps from 0 up (RIR = 10 − RPE on the 6 to 10 part of the scale). It is the quantity the suggestion engine needs, and it is wger's storage choice [src].
- Store alongside it the **value as entered and its scale** (`rpeRir` for the Zourdos scale, `rir`, `cr10` for Borg CR-10 session ratings, or a vendor scale such as `hevy` or `strong`), so imports stay lossless. Hevy's and Strong's 9.5 (maybe one more rep) differ from Boostcamp's (no more reps, slightly more load), and Health Connect's integer RPE is Borg CR10.
- Keep **failure as separate flags**, not RPE 10: `reachedFailure` and `failedRepAttempted` (section 2.3). An RPE 10 set may not have attempted a further rep; a failed attempt is a fact a lifter knows exactly.
- Show RPE 6 to 10 in half steps with the RIR text under each value, as Hevy, Strong and Boostcamp do. Whether to include 6.5 is an open question (section 11).
- Rate per set, optionally. Hevy lets users leave RPE blank, for example on warm-ups [web]. Missing effort must be a valid state.

## 6. Volume, frequency and deloads

### 6.1 The evidence

- **Schoenfeld, Ogborn and Krieger 2017** [web]: 15 studies. Each additional weekly set per muscle was associated with 0.37 % more muscle growth; higher volume beat lower volume by 3.9 percentage points; the three bins (fewer than 5, 5 to 9, 10 or more sets) showed only a trend (p = 0.074).
- **Pelland et al. 2025** (Sports Medicine, online 2025-12-04, print 2026, 56(2):481-505) [web]: 67 studies, 2058 participants. Sets were classified as direct or indirect; counting indirect sets as 0.5 ("fractional") fitted best. Volume increased both hypertrophy and strength with diminishing returns, much more pronounced for strength. Frequency had a consistent, diminishing effect on strength and an effect "compatible with negligible" on hypertrophy.
- **Remmert et al. 2025** (preprint, not peer reviewed) [web]: per-session volume. "The available evidence indicates a PUOS [point of undetectable outcome superiority] of ~2 and ~11 sets per session for strength gain and hypertrophy, respectively" (2 "direct" sets for strength, 11 "fractional" sets for hypertrophy). It reports that the Pelland data showed "no detectable superiority beyond ~31 'fractional' sets per week for hypertrophy and ~3 'fractional' sets per week for strength gain", and repeats the ~3 figure in its discussion. ACSM 2026, reading Pelland, gives "~2–3 sets/exercise for strength" [web]. Whether the published Pelland paper says per week or per exercise is [unverified].
- **ACSM 2026** [web]: hypertrophy was enhanced by "higher volume (≥10 sets/muscle group/wk)"; strength by frequency of at least 2 days a week, higher loads and more sets. Reading the Pelland data, it describes "diminishing returns beyond ~2–3 sets/exercise for strength and ~18–20 weekly sets for hypertrophy" and advises "at least two sets per exercise". Its headline: train all major muscle groups at least 2 days a week [web: infographic].

### 6.2 Counting rules Spotter needs

- A **hard set** is a working set (not a warm-up) with known effort of RIR 4 or less, or unknown effort [recalled: the earlier research lists "RPE ≥ 7 or RIR ≤ 3"; the threshold is a proposal and an open question].
- **Fractional counting**: 1 for each primary muscle, 0.5 for each secondary muscle (Pelland et al. 2025 [web]; Hevy uses the same weights [web]). This needs a primary and secondary muscle map on every exercise.
- **Warm-ups** count 0 (Hevy counts them; Spotter should not).
- **Structured sets**: a drop set, a rest-pause set or a myo-rep set counts as one set by default, with the rule kept as a versioned derived view so it can change. Alpha Progression warns against counting each mini-set as a full set [web]. There is no evidence for a precise equivalence [unverified].
- **Unilateral**: one set per side pair (section 2.10).
- **Volume load**: reps × load per segment, with bodyweight included only for exercises that move the whole body. Hevy adds logged body weight to the added load for pull-ups, chin-ups and dips, but not for push-ups or sit-ups, and never for custom weighted-bodyweight exercises [web]. That needs a body-mass snapshot on the session.

### 6.3 Deloads

- Consensus definition from a Delphi study of coaches: "a period of reduced training stress designed to mitigate physiological and psychological fatigue, promote recovery, and enhance preparedness for subsequent training" (Bell et al. 2023) [web].
- In practice, competitive strength and physique athletes deload for about 6.4 days every 5.6 weeks, cutting reps per set and sets per week, cutting load, raising RIR, and keeping frequency and exercise selection (Rogerson et al. 2024, survey of 246) [web].
- One trial: a full week off in the middle of a 9-week program did not change lower-body hypertrophy, power or endurance, and gave smaller lower-body isometric and dynamic strength gains than training straight through (Coleman et al. 2024) [web].
- For Spotter: a deload is a property of a routine or session (a flag plus a note), so that suggestions and trends can exclude or discount it. The agent writes deload routines; Spotter does not need to generate them.

## 7. Data-model requirements

Numbered so later documents can refer to them. "Plan" means the routine as written; "log" means the performed session.

**Structure**

1. The plan is a tree: routine, ordered groups, ordered slots per group, ordered planned sets per slot, optional segments per planned set. A group with one slot is a plain exercise. (wger: day, slot, slot entry [src]; Hevy: exercise plus `superset_id` [web]; FIT: steps plus repeat steps [web].)
2. Group kind: `single`, `superset`, `triset`, `giantset`, `circuit`, `paired`, plus reserved `emom` and `amrapTime`. Groups carry a member order and either a round count or "rounds = most sets of any member".
3. Group purpose, optional and descriptive: `antagonist`, `sameMuscle` (compound set), `preExhaust`, `upperLower`, `unrelated`, `mechanicalDrop`. Used only to pick default rests.
4. Set role: `warmup` or `working`, with an optional label `top` or `backoff` for working sets.
5. Set structure: `straight`, `drop`, `myo`, `myoMatch`, `restPause`, `cluster`. Every non-straight structure has ordered segments; every unilateral set has side segments. Each segment has its own targets and actuals.
6. Termination per set or segment: `reps` (exact), `repRange` (min, max), `amrap` (optional minimum), `failure`, `rirStop` (stop at n RIR), `time` (exact or range), `distance`, `totalReps` (for myo-match and rest-pause totals).

**Targets**

7. Load target, one of: absolute (value and unit); relative to a reference (`1RM`, `e1RM`, `trainingMax`) as a percentage, resolved to an absolute load when the session starts and stored resolved; effort-driven (no load, choose it from the effort target); bodyweight, bodyweight plus added load, or assisted (assistance as its own field, not a negative mass, as the substrate review recommends [src: `docs/plans/health-data/review/codex-compare.md`]); band (a label); relative to the previous set or segment (minus a percentage or a step) for drops and back-off sets.
8. Effort target: RIR value or range, or RPE value or range, with the scale named, and whether it is a load selector or a stop rule (section 5.4).
9. Duration target in seconds, with a timer mode (`countdown` or `stopwatch`).
10. Distance target in metres.
11. Per-side settings on the slot: unilateral flag, side order (`left`, `right`, `weakerFirst`), mode (`separate` or `alternating`), and whether the second side matches the first side's reps.
12. Tempo: an optional four-phase string (eccentric, pause, concentric, pause; digits or `X`), display only.
13. Myo-reps parameters: activation rep range and effort, mini-set rep target, pause (seconds or breaths), maximum mini-sets, stop rule (`repDrop`, `belowTarget`, `totalReps`), and for myo-match a reference to the set whose reps are matched.
14. Drop-set parameters: number of drops, change per drop (percent or absolute), termination per drop.
15. Cluster parameters: clusters per set, reps per cluster, intra-set rest.

**Rest**

16. Prescribed rest slots, each a duration in seconds, an optional upper bound, or `open`: `setRest` and `warmupRest` per slot; `transition` per group member; `roundRest` per group; `intraSetPause` per structured set; `sideSwitch` per unilateral slot; `blockRest` per group (defaulting to the set or round rest).
17. A per-set override of the rest that follows it.
18. Performed rest recorded as an interval: start, end, the slot it filled, the prescribed duration, adjustments (each ±15 s tap), and whether it was skipped. This aligns with FIT's `set_type = rest` records and Health Connect's `REST` segments [web].

**Log**

19. Each performed set links to the planned set it fulfils (nullable for added sets and empty workouts), the exercise actually done (which may be a substitute), and its order in the session. Like FIT's `wkt_step_index` and Health Connect's planned-session link [web, earlier research].
20. Each performed set and segment records: started and completed timestamps, reps, load (value, unit, load mode, implement count or per-hand basis), duration, distance, side, effort (canonical RIR plus the entered value and scale), `reachedFailure`, `failedRepAttempted`, and a note.
21. Every mass carries its unit as entered. Substrate's earlier `workoutset.weight` had no unit, which made kilograms and pounds ambiguous (substrate issue #125, cited in `docs/plans/health-data/06-substrate-fit.md` section 7) [src].
22. The session records start, end, pauses, the source routine and its revision, a deload flag, an optional session RPE (CR-10), a body-mass snapshot, and notes.

**Exercise attributes the engine needs**

23. Measurement type, as Hevy's exercise types do: `weight_reps`, `reps_only`, `bodyweight_reps`, `bodyweight_assisted_reps`, `duration`, `weight_duration`, `distance_duration`, `short_distance_weight` [web: Hevy OpenAPI `CustomExerciseType`]. Spotter needs the same set plus weighted bodyweight (Hevy handles it as a separate exercise kind in its records article [web]).
24. A rest class (section 4.3) for default rests.
25. Primary and secondary muscles, for fractional set counting.
26. Whether the exercise can be unilateral, and whether bodyweight counts towards volume load.
27. Load increments for rounding generated loads (plate pairs, dumbbell steps, stack steps).

**Imports**

28. Keep the source's own values beside the mapped ones (set type string, RPE value and scale, rest field). Mapping notes: Hevy `warmup` becomes role `warmup`; `normal` becomes `working`, `straight`; `failure` becomes `working` with `reachedFailure`; consecutive `dropset` rows become drop segments of the preceding set. Strong's Failure tag becomes `failedRepAttempted`. wger's `next_log` chain becomes segments; `myo` becomes structure `myo`; `partial`, `forced`, `tut`, `iso` and `jump` stay as source tags with a note (section 9).

## 8. What the app generates on its own

- **Warm-up ramp** from the first working load (section 2.2), rounded to the equipment.
- **Drop loads** from the parent load and the drop rule.
- **Myo-rep mini-sets**: the next mini-set row, its target and the stop decision; for myo-match, the running total.
- **Rest-pause and cluster segments** with their pauses.
- **Unilateral second side** target equal to the first side's reps when matching.
- **Rest timers and their end notifications**, picked by the rule in section 3.3.
- **Suggested load and reps for the next set**, from the current session and earlier sessions of the same exercise, using rest actually taken and effort weighted by its likely accuracy (section 5.3). The algorithm belongs to a separate document.

## 9. What not to model in V1, and why

1. **Periodisation engines and automatic volume progression** (RP and Alpha Progression style mesocycles). Routines are written in the substrate console and by the agent. ACSM 2026 found that periodization "did not consistently impact training outcomes"; its infographic says complex periodization "did not consistently change results for the average healthy adult" [web]. Deload stays a flag.
2. **Velocity-based training.** It needs a sensor or camera; nothing to measure with.
3. **Per-rep data and rep-level types** (partials, forced reps, negatives, wger's `partial`, `forced`, `tut`, `iso`, `jump`). Kept as tags and notes on import. They are rare, and the evidence for them is thin.
4. **Tempo compliance.** Tempo is shown, not measured.
5. **Mechanical drop sets as a set structure.** A group with zero transition expresses them and keeps per-exercise history correct.
6. **EMOM, AMRAP for time and Tabata in the engine.** The group kinds are reserved in the model so FIT `amrap`, `emom` and `tabata` imports have a home, but the engine waits until the owner asks.
7. **Cluster sets in the UI.** The model supports them through segments; the screen and generator can come later. Evidence shows no advantage over traditional sets for the owner's goals (Davies et al. 2021) [web].
8. **Automatic back-off sets from fatigue percentages** (Tuchscherer's method). Useful, but it belongs with the suggestion engine, after basic suggestions work.
9. **Band tension, machine settings, chains, blood-flow restriction, eccentric overload devices.** Free text on the slot or the set. Quantifying them needs per-device data nobody provides.
10. **RPE below 6 and per-set CR-10.** Allowed in storage, not offered on the keypad. Strong and Hevy leave them out because they are hard to rate [web], and the accuracy data agree (section 5.3).
11. **Volume landmarks** such as minimum effective and maximum recoverable volume. Coaching constructs without a validated per-person method; weekly fractional sets per muscle are enough for V1 charts.

## 10. Design implications for Spotter

- Build the session engine around the rest rule in section 3.3: rest is chosen from the set just completed and the set the plan expects next, and keeps counting if the lifter jumps elsewhere. That gives free order and correct superset behaviour at once.
- Give groups their own `transition` and `roundRest`. This is the owner's "different rests inside a superset versus after it", and it is where Hevy, Strong, Boostcamp, RP and wger all fall short.
- Make structured sets one set with segments: myo-reps, myo-match, rest-pause, drop sets and per-side work. The engine spawns segment rows and runs the short pause timer; the log keeps every segment.
- Record performed rest as intervals with timestamps, and record set start and completion times. Hevy's export cannot give Spotter this history, so Spotter's own data will be the only source of real rest data.
- Store effort as RIR plus the entered value and scale; show RPE 6 to 10 with RIR text; keep failure flags separate from RPE 10.
- Default rests from the exercise's class (section 4.3) and the group's purpose (section 4.4), err long, and let the routine override any of them.
- Separate warm-up rest from working rest, generate warm-up ramps, and exclude warm-ups from hard sets, PRs, e1RM and volume.
- Count hard sets per muscle with fractional weights (1 and 0.5), as a versioned derived view.
- Time-sensitive notifications should fire for `setRest`, `roundRest` and long transitions. Pauses under about 20 s (myo-reps, drop changes, side switches) should use in-app haptics and sound, because the phone is in the lifter's hand or nearby [recalled: an assumption about use, not a measured fact].
- Keep the method parameters in the routine data, not in Swift code, so the agent can write myo-reps, drop rules and rests without an app release.

## 11. Open questions for the owner

1. RPE or RIR on the keypad? The proposal is RPE 6 to 10 with RIR text under each value, as in Hevy. Include 6.5 (Strong does, Hevy does not)?
2. Myo-reps stop rule by default: the method author's "stop when a mini-set loses a rep from the first mini-set, or at 5 mini-sets", or a fixed total, or RP's match? Show the pause as breaths or as seconds?
3. Should the rest timer run after warm-up sets, and for how long by default (proposal: 45 s)?
4. In a superset, should warm-up sets of all members run before the first round (proposal) or inside the rounds?
5. After the last set of an exercise, should Spotter start a rest timer (Hevy does) or wait for the next exercise?
6. For a failed rep, do you want to record the attempt (Strong's semantics) as well as the completed reps?
7. What threshold makes a set "hard" for weekly volume: RIR 4 or less, RIR 3 or less, or every working set?
8. Should unilateral work be logged per side by default, or as one number with a "per side" flag as RP does?
9. Do routines need percent-of-1RM or percent-of-e1RM loads now, or are absolute loads and effort targets enough for V1?
10. Are EMOMs, circuits for time or Tabata-style intervals part of your training, or can they wait?
11. Should Spotter ask for a session RPE at Finish and write it to Apple Health as the workout's effort score?
12. Which units and equipment increments should generation round to (kilograms only; 1.25 kg plates; dumbbell steps in your gym)?

## Sources

Papers (abstracts read through Europe PMC or PubMed; full text where noted):

- Bell L, Strafford BW, Coleman M, Androulakis Korakakis P, Nolan D. Integrating deloading into strength and physique sports training programmes: an international Delphi consensus approach. Sports Med Open 2023;9:87. [doi:10.1186/s40798-023-00633-0](https://doi.org/10.1186/s40798-023-00633-0)
- Coleman M, Burke R, Augustin F, et al. Gaining more from doing less? The effects of a one-week deload period during supervised resistance training on muscular adaptations. PeerJ 2024;12:e16777. [doi:10.7717/peerj.16777](https://doi.org/10.7717/peerj.16777)
- Currier BS, D'Souza AC, Fiatarone Singh MA, et al.; Phillips SM (chair). American College of Sports Medicine position stand. Resistance training prescription for muscle function, hypertrophy, and physical performance in healthy adults: an overview of reviews. Med Sci Sports Exerc 2026;58(4):851-872 (full text read). [doi:10.1249/MSS.0000000000003897](https://doi.org/10.1249/MSS.0000000000003897), [ACSM announcement](https://acsm.org/science-spotlight-acsm-releases-new-position-stand-on-resistance-training/), [ACSM infographic](https://acsm.org/wp-content/uploads/2026/03/Resistance-Training-Position-Stand-infographic.pdf)
- Davies TB, Tran DL, Hogan CM, Haff GG, Latella C. Chronic effects of altering resistance training set configurations using cluster sets: a systematic review and meta-analysis. Sports Med 2021;51(4):707-736. [doi:10.1007/s40279-020-01408-3](https://doi.org/10.1007/s40279-020-01408-3)
- Enes A, Alves RC, Schoenfeld BJ, et al. Rest-pause and drop-set training elicit similar strength and hypertrophy adaptations compared with traditional sets in resistance-trained males. Appl Physiol Nutr Metab 2021;46(11):1417-1424. [doi:10.1139/apnm-2021-0278](https://doi.org/10.1139/apnm-2021-0278)
- Fujita RA, Silva NRS, Bedo BLS, Gomes MM. The pre-exhaustion method does not increase muscle activity in target muscle during strength training in untrained individuals. J Hum Kinet 2022;82:17-26. [doi:10.2478/hukin-2022-0027](https://doi.org/10.2478/hukin-2022-0027)
- Grgic J, Lazinica B, Mikulic P, Krieger JW, Schoenfeld BJ. The effects of short versus long inter-set rest intervals in resistance training on measures of muscle hypertrophy: a systematic review. Eur J Sport Sci 2017;17(8):983-993. [doi:10.1080/17461391.2017.1340524](https://doi.org/10.1080/17461391.2017.1340524)
- Grgic J, Schoenfeld BJ, Skrepnik M, Davies TB, Mikulic P. Effects of rest interval duration in resistance training on measures of muscular strength: a systematic review. Sports Med 2018;48(1):137-151. [doi:10.1007/s40279-017-0788-x](https://doi.org/10.1007/s40279-017-0788-x)
- Halperin I, Malleron T, Har-Nir I, et al. Accuracy in predicting repetitions to task failure in resistance exercise: a scoping review and exploratory meta-analysis. Sports Med 2022;52(2):377-390. [doi:10.1007/s40279-021-01559-x](https://doi.org/10.1007/s40279-021-01559-x)
- Helms ER, Cronin J, Storey A, Zourdos MC. Application of the repetitions in reserve-based rating of perceived exertion scale for resistance training. Strength Cond J 2016;38(4):42-49 (full text and tables read). [PMC4961270](https://pmc.ncbi.nlm.nih.gov/articles/PMC4961270/)
- Helms ER, Byrnes RK, Cooke DM, et al. RPE vs. percentage 1RM loading in periodized programs matched for sets and repetitions. Front Physiol 2018;9:247. [doi:10.3389/fphys.2018.00247](https://doi.org/10.3389/fphys.2018.00247)
- Ibbott P, Ball N, Welvaert M, Thompson KG. Variability and impact of self-selected interset rest periods during experienced strength training. Percept Mot Skills 2019;126(3):546-558. [doi:10.1177/0031512519835976](https://doi.org/10.1177/0031512519835976)
- Keskin K, Gogus FN, Gunay M, Fujita RA. Equated volume load: similar improvements in muscle strength, endurance, and hypertrophy for traditional, pre-exhaustion and drop sets in resistance training. Research Square preprint 2024. [doi:10.21203/rs.3.rs-4564565/v1](https://doi.org/10.21203/rs.3.rs-4564565/v1)
- Longo AR, Silva-Batista C, Pedroso K, et al. Volume load rather than resting interval influences muscle hypertrophy during high-intensity resistance training. J Strength Cond Res 2022;36(6):1554-1559. [doi:10.1519/JSC.0000000000003668](https://doi.org/10.1519/JSC.0000000000003668)
- Maia MF, Willardson JM, Paz GA, Miranda H. Effects of different rest intervals between antagonist paired sets on repetition performance and muscle activation. J Strength Cond Res 2014;28(9):2529-2535. [doi:10.1519/JSC.0000000000000451](https://doi.org/10.1519/JSC.0000000000000451)
- Nuzzo J. Drop sets: redefined. Int J Strength Cond 2026;6(1). [journal page](https://journal.iusca.org/index.php/Journal/article/view/572)
- Pelland JC, Remmert JF, Robinson ZP, Hinson SR, Zourdos MC. The resistance training dose response: meta-regressions exploring the effects of weekly volume and frequency on muscle hypertrophy and strength gains. Sports Med 2026;56(2):481-505 (online 2025-12-04). [doi:10.1007/s40279-025-02344-w](https://doi.org/10.1007/s40279-025-02344-w)
- Prestes J, Tibana RA, de Araujo Sousa E, et al. Strength and muscular adaptations after 6 weeks of rest-pause vs. traditional multiple-sets resistance training in trained subjects. J Strength Cond Res 2019;33(Suppl 1):S113-S121. [doi:10.1519/JSC.0000000000001923](https://doi.org/10.1519/JSC.0000000000001923)
- Ratamess NA, Alvar BA, Evetoch TK, et al. American College of Sports Medicine position stand. Progression models in resistance training for healthy adults. Med Sci Sports Exerc 2009;41(3):687-708 (full text read). [doi:10.1249/MSS.0b013e3181915670](https://doi.org/10.1249/MSS.0b013e3181915670)
- Refalo MC, Helms ER, Trexler ET, Hamilton DL, Fyfe JJ. Influence of resistance training proximity-to-failure on skeletal muscle hypertrophy: a systematic review with meta-analysis. Sports Med 2023;53(3):649-665. [doi:10.1007/s40279-022-01784-y](https://doi.org/10.1007/s40279-022-01784-y)
- Refalo MC, Remmert JF, Pelland JC, et al. Accuracy of intraset repetitions-in-reserve predictions during the bench press exercise in resistance-trained male and female subjects. J Strength Cond Res 2024;38(3):e78-e85. [doi:10.1519/JSC.0000000000004653](https://doi.org/10.1519/JSC.0000000000004653)
- Remmert JF, Laurson KR, Zourdos MC. Accuracy of predicted intraset repetitions in reserve (RIR) in single- and multi-joint resistance exercises among trained and untrained men and women. Percept Mot Skills 2023;130(3):1239-1254. [doi:10.1177/00315125231169868](https://doi.org/10.1177/00315125231169868)
- Remmert JF, Robinson ZP, Pelland JC, et al. Changes in intraset repetitions in reserve prediction accuracy during six weeks of bench press training in trained men. Percept Mot Skills 2023;130(5):2139-2160. [doi:10.1177/00315125231189098](https://doi.org/10.1177/00315125231189098)
- Remmert JF, Pelland JC, Robinson ZP, Hinson SR, Zourdos MC. Is there too much of a good thing? Meta-regressions of the effect of per-session volume on hypertrophy and strength. SportRxiv preprint, March 2025, not peer reviewed (full text read). [preprint PDF](https://sportrxiv.org/index.php/server/preprint/download/537/1148/1071)
- Ribeiro AS, Romanzini M, Schoenfeld BJ, et al. Effect of different warm-up procedures on the performance of resistance training exercises. Percept Mot Skills 2014;119(1):133-145. [doi:10.2466/25.29.PMS.119c17z7](https://doi.org/10.2466/25.29.PMS.119c17z7)
- Robbins DW, Young WB, Behm DG, Payne WR. Agonist-antagonist paired set resistance training: a brief review. J Strength Cond Res 2010;24(10):2873-2882. [doi:10.1519/JSC.0b013e3181f00bfc](https://doi.org/10.1519/JSC.0b013e3181f00bfc)
- Robinson ZP, Pelland JC, Remmert JF, et al. Exploring the dose-response relationship between estimated resistance training proximity to failure, strength gain, and muscle hypertrophy: a series of meta-regressions. Sports Med 2024;54(9):2209-2231. [doi:10.1007/s40279-024-02069-2](https://doi.org/10.1007/s40279-024-02069-2)
- Rogerson D, Nolan D, Androulakis Korakakis P, et al. Deloading practices in strength and physique sports: a cross-sectional survey. Sports Med Open 2024;10:26. [doi:10.1186/s40798-024-00691-y](https://doi.org/10.1186/s40798-024-00691-y)
- Schoenfeld BJ, Grgic J. Can drop set training enhance muscle growth? Strength Cond J 2018;40(6):95-98 (full text not read). [record](https://vuir.vu.edu.au/43250)
- Schoenfeld BJ, Ogborn D, Krieger JW. Dose-response relationship between weekly resistance training volume and increases in muscle mass: a systematic review and meta-analysis. J Sports Sci 2017;35(11):1073-1082. [doi:10.1080/02640414.2016.1210197](https://doi.org/10.1080/02640414.2016.1210197)
- Schoenfeld BJ, Pope ZK, Benik FM, et al. Longer interset rest periods enhance muscle strength and hypertrophy in resistance-trained men. J Strength Cond Res 2016;30(7):1805-1812. [doi:10.1519/JSC.0000000000001272](https://doi.org/10.1519/JSC.0000000000001272)
- Scudese E, Willardson JM, Simão R, et al. The effect of rest interval length on repetition consistency and perceived exertion during near maximal loaded bench press sets. J Strength Cond Res 2015;29(11):3079-3083. [doi:10.1097/JSC.0000000000000214](https://doi.org/10.1097/JSC.0000000000000214)
- Singer A, Wolf M, Generoso L, et al. Give it a rest: a systematic review with Bayesian meta-analysis on the effect of inter-set rest interval duration on muscle hypertrophy. Front Sports Act Living 2024;6:1429789 (full text read). [doi:10.3389/fspor.2024.1429789](https://doi.org/10.3389/fspor.2024.1429789)
- Sødal LK, Kristiansen E, Larsen S, van den Tillaar R. Effects of drop sets on skeletal muscle hypertrophy: a systematic review and meta-analysis. Sports Med Open 2023;9:66. [doi:10.1186/s40798-023-00620-5](https://doi.org/10.1186/s40798-023-00620-5)
- Tabata I, Nishimura K, Kouzaki M, et al. Effects of moderate-intensity endurance and high-intensity intermittent training on anaerobic capacity and VO2max. Med Sci Sports Exerc 1996;28(10):1327-1330. [doi:10.1097/00005768-199610000-00018](https://doi.org/10.1097/00005768-199610000-00018)
- Tsartsapakis I, Zafeiroudi A, Kouthouris C. Effects of advanced resistance training systems on muscle hypertrophy and strength in recreationally trained adults: a systematic review and meta-analysis. J Funct Morphol Kinesiol 2026;11(1):80. [doi:10.3390/jfmk11010080](https://doi.org/10.3390/jfmk11010080)
- Tufano JJ, Brown LE, Haff GG. Theoretical and practical aspects of different cluster set structures: a systematic review. J Strength Cond Res 2017;31(3):848-867. [doi:10.1519/JSC.0000000000001581](https://doi.org/10.1519/JSC.0000000000001581)
- Weakley JJS, Till K, Read DB, et al. The effects of traditional, superset, and tri-set resistance training structures on perceived intensity and physiological responses. Eur J Appl Physiol 2017;117(9):1877-1889. [doi:10.1007/s00421-017-3680-3](https://doi.org/10.1007/s00421-017-3680-3)
- Wilk M, Zajac A, Tufano JJ. The influence of movement tempo during resistance training on muscular strength and hypertrophy responses: a review. Sports Med 2021;51(8):1629-1650 (full text read for the notation). [PMC8310485](https://pmc.ncbi.nlm.nih.gov/articles/PMC8310485/)
- Zhang X, Weakley J, Li H, Li Z, García-Ramos A. Superset versus traditional resistance training prescriptions: a systematic review and meta-analysis exploring acute and chronic effects on mechanical, metabolic, and perceptual variables. Sports Med 2025;55(4):953-975 (full text read for the study table). [PMC12011898](https://pmc.ncbi.nlm.nih.gov/articles/PMC12011898/)
- Zourdos MC, Klemp A, Dolan C, et al. Novel resistance training-specific rating of perceived exertion scale measuring repetitions in reserve. J Strength Cond Res 2016;30(1):267-275. [doi:10.1519/JSC.0000000000001049](https://doi.org/10.1519/JSC.0000000000001049)
- Zourdos MC, Goldsmith JA, Helms ER, et al. Proximity to failure and total repetitions performed in a set influences accuracy of intraset repetitions in reserve-based rating of perceived exertion. J Strength Cond Res 2021;35(2S):S158-S165. [doi:10.1519/JSC.0000000000002995](https://doi.org/10.1519/JSC.0000000000002995)

Method sources:

- Fagerli B. Myo-reps in English. 2012, edited 2016. [Internet Archive copy of 2020-11-21](https://web.archive.org/web/20201121105635/https://borgefagerli.com/myo-reps-in-english/) (the original URL now returns 404)
- Tuchscherer's fatigue-percent method, secondary: [EliteFTS, A Reactive Training Manual overview](https://elitefts.com/blogs/training/a-reactive-training-manual-overview) (the old `/education/` URL now returns 404)
- RP Hypertrophy myorep-match, secondary: [Special intensity techniques I: myorep-match](https://mrpotato.substack.com/p/special-intensity-techniques-i-myorep)
- Myo-reps secondary summaries: [Barbell Medicine](https://www.barbellmedicine.com/blog/myo-reps/), [Coach, myo-reps workout](https://www.coachweb.com/fitness/full-body-workouts/is-this-the-most-effective-20-minute-workout-for-building-muscle-this-elite-coach-thinks-so), [Legion, myo-reps](https://legionathletics.com/myo-reps)
- RPE 9.5 description, secondary: [Top End Sports, resistance training RPE](https://www.topendsports.com/fitness/strength/rpe-resistance-training.htm)
- Mechanical drop sets, secondary: [Muscle & Strength](https://www.muscleandstrength.com/node/46714)

Apps and formats (read on 2026-10-30):

- Hevy help centre: [supersets and smart scrolling](https://help.hevyapp.com/hc/en-us/articles/35650286563095), [circuits, intervals and supersets](https://help.hevyapp.com/hc/en-us/articles/36954623739415), [rest timer](https://help.hevyapp.com/hc/en-us/articles/35385404949143), [set types](https://help.hevyapp.com/hc/en-us/articles/34896293707927), [how to use RPE](https://help.hevyapp.com/hc/en-us/articles/35687721776663), [RPE vs RIR](https://help.hevyapp.com/hc/en-us/articles/34490600233111), [duration exercise stopwatch](https://help.hevyapp.com/hc/en-us/articles/35379662997783), [warm-up calculator](https://help.hevyapp.com/hc/en-us/articles/35650921359639), [workout settings](https://help.hevyapp.com/hc/en-us/articles/33882110558743), [sets per muscle group](https://help.hevyapp.com/hc/en-us/articles/44128140257431), [weighted bodyweight volume](https://help.hevyapp.com/hc/en-us/articles/38387131819799), [set and personal records](https://help.hevyapp.com/hc/en-us/articles/38279531346455)
- Hevy API: [OpenAPI documentation](https://api.hevyapp.com/docs/) (spec version 0.0.1)
- Hevy screenshots supplied by the owner (Log Workout, RPE keypad, set type sheet, rest timer picker, superset menu, timed sets, Live Activity)
- Strong help centre: [rest timer](https://help.strongapp.io/article/231-rest-timer), [RPE](https://help.strongapp.io/article/230-about-rpe), [supersets and circuits](https://help.strongapp.io/article/98-supersets-and-circuits), [set tags](https://help.strongapp.io/article/166-set-tags), [warm-up calculator](https://help.strongapp.io/article/171-warm-up-calculator)
- Boostcamp: [program builder](https://www.boostcamp.app/features/program-builder), [rest timer](https://www.boostcamp.app/features/rest-timer), [RPE logging](https://www.boostcamp.app/features/rpe-logging), [llms.txt](https://www.boostcamp.app/llms.txt)
- RP Strength help centre: [logging weights, reps, sets](https://help.rpstrength.com/hc/en-us/articles/43828601644055-Logging-Weights-Reps-Sets), [rest times](https://help.rpstrength.com/hc/en-us/articles/43549598219799-Rest-Times), [warm-ups and starting weights](https://help.rpstrength.com/hc/en-us/articles/43549480893591-Warm-ups-and-Starting-Weights), [RIR](https://help.rpstrength.com/hc/en-us/articles/43549386649751-RIR-Reps-in-Reserve), [dumbbell movements](https://help.rpstrength.com/hc/en-us/articles/43549592737175-Logging-Dumbbell-Movements), [changing set number](https://help.rpstrength.com/hc/en-us/articles/43864695258903-Changing-Set-Number), [long workouts](https://help.rpstrength.com/hc/en-us/articles/43864739113239-Long-Workouts)
- Alpha Progression: [supersets glossary](https://alphaprogression.com/en/glossary/supersets), [myo-reps glossary](https://alphaprogression.com/en/glossary/myo-reps), [rest-pause glossary](https://alphaprogression.com/en/glossary/rest-pause), [Alpha Progression vs Hevy](https://alphaprogression.com/en/blog/alpha-progression-vs-hevy)
- wger: [`wger/manager/models`](https://github.com/wger-project/wger/tree/master/wger/manager/models) (`slot.py`, `slot_entry.py`, `rest_config.py`, `rir_config.py`, `abstract_config.py`, `log.py`, and `wger/manager/dataclasses.py`), commit `dde740753e`
- Garmin FIT: [FIT JavaScript SDK `profile.js`](https://github.com/garmin/fit-javascript-sdk/blob/main/src/profile.js), profile 21.218.0, commit `992cfad3b8`
- Health Connect: [`ExercisePerformanceTarget.kt`](https://github.com/androidx/androidx/blob/androidx-main/health/connect/connect-client/src/main/java/androidx/health/connect/client/records/ExercisePerformanceTarget.kt), [`ExerciseSegment.kt`](https://github.com/androidx/androidx/blob/androidx-main/health/connect/connect-client/src/main/java/androidx/health/connect/client/records/ExerciseSegment.kt)
- Apple SDK (iOS 27.2): `HealthKit.framework/Headers/HKTypeIdentifiers.h` and `HKUnit.h` (workout effort score), `WorkoutKit.swiftmodule/arm64e-apple-ios.swiftinterface` (`IntervalStep.Purpose`, `WorkoutGoal`)

In-repo and prior research:

- Substrate `docs/plans/health-data/02-workouts.md` (2026-10-08), sections 1.8, 1.9, 3, 4.3, 4.5 and 5
- Substrate `docs/plans/health-data/06-substrate-fit.md`, section 7 (issue #125 on units)
- Substrate `docs/plans/health-data/review/codex-compare.md`, "Scores, sleep, sports, and strength" (load modes)
