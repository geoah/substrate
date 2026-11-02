# Proposal 087: Spotter, strength sessions in the gym on substrate

> **A historical record.** This is proposal 087 as written for the rxtx.dev repository
> (`proposals/087-spotter.md`, created 2026-10-30, as last revised on the branch of
> geoah/rxtx.dev#118 on 2026-10-31), when the app was Spotter and its package a draft
> Mneme package, `mneme.rxtx.dev/training`. It is superseded by this folder: the app is
> Substrate Workouts, its package is its own `~/training` ([`Package/`](../../Package/README.md),
> decision record [0203](../../../../../docs/decisions/0203-an-app-keeps-its-own-package-under-a-declare-scope.md)),
> and [decisions.md](../decisions.md) records what was decided since, including which of
> the open questions below were answered. Links now point at this repository's copies
> (the research in [`../research/`](../research/README.md), the examples in
> [`../examples/`](../examples/README.md), the package and the spec beside this folder);
> the text and the kind names are as written.

**Status:** 🚧 In Progress (app V1 built and released; the package is still a draft)
**Author:** Opus 5.5 for the owner, from research slices 01 to 06 and the lead's decisions
**Created:** 2026-10-30

## Summary

Spotter is an iPhone app (iOS 27, Liquid Glass) that runs strength sessions in the gym and keeps
their data on substrate. The substrate console's agent writes the routines; the app browses them,
starts a session from one or from nothing, runs it, and saves one strength workout per session to
Apple Health. This proposal is the entry point to the whole effort: the format verdict, the draft
package `mneme.rxtx.dev/training` (five kinds, proven on a private substrate), the decisions taken
on the owner's behalf with their alternatives, the app's V1, V1.1 and V2, the rollout steps only
the owner can approve, and the questions still open.

- The research and its synthesis (requirements 1 to 39, the disagreements, the open questions):
  [research/workouts/README.md](../research/README.md).
- The package: [research/workouts/substrate/training/](../../Package/README.md).
- The example records: [research/workouts/substrate/examples/](../examples/README.md).
- The app: [apps/apple/spotter/](../../README.md), designed in
  [apps/apple/spotter/docs/DESIGN.md](../../DESIGN.md).

## Motivation

The owner trains with Hevy and asked for an app of their own that:

- runs strength sessions in the gym, from a routine or empty, with exercises done in any order;
- handles supersets, warm-ups, drop sets, myo-reps and timed sets, with RPE on each set;
- times every rest and notifies when it ends;
- suggests the next set from this session and from earlier ones;
- takes its routines from the substrate console, where the agent writes them;
- writes one strength workout per session to Apple Health;
- gets a watch app later; for now the watch is design only.

Hevy falls short in ways the owner's 13 screenshots show. It has one rest per exercise, so the
rest inside a superset and the rest after its round cannot differ: in "Fully Body Collection #1"
the Glute Bridge and Bird Dog superset has rest off on both members, so there is no rest after
the round either (06 §1; 02 §3.4). Its hints are "last time" only. Its data lives in Hevy's cloud,
not beside the owner's other health records on substrate, where Apple Health and WHOOP workouts
already land as `substrate.reamde.dev/health/workout`.

## The format verdict

1. None of the evaluated standards carries all of a strength session down to the set: FHIR and its
   IGs stop at the session, FIT and Health Connect carry only parts of a set, and no LOINC or
   SNOMED CT code for a set, a rep count or a lifted load was found (01 §0, §2).
2. Apple has nothing below the workout: HealthKit has no set, rep or load type, and WorkoutKit
   cannot say "8 reps" or "60 kg" (01 §3.1, §3.2).
3. Set-level data exists only in vendor formats (FIT, Health Connect's alpha segments, Hevy,
   Strong, wger, Terra), and the open drafts have no second implementer (01 §3.3 to §5).
4. Exercises have no shared key: each keeps every source's code as `{system, code, version,
   display}`, and importers match on `(system, code)` (01 §7).
5. So Spotter owns its model (Hevy's structure plus set times, quantity loads, RIR, rests and
   copied targets) and maps to the others field by field ([research/workouts/](../research/README.md)).

## Design

### The data model

Package `mneme.rxtx.dev/training`, version 1, requires `substrate.reamde.dev/health` and installs
five kinds:

```text
routine  (label "Workout routine"; slug id, written by the console's agent)
  groups[]   supersets and circuits: kind, pairing, rounds, roundRest (once per group)
  slots[]    key, group, rests ──exercise (mustExist)──► exercise
    sets[]   planned sets: key, role, structure, termination, targets (rpeMin/rpeMax ranges,
             loadPercent), rest override

exercise (slug id)  measurement, equipment, laterality, load grid, codes[], suggestion settings

setup    (label "Exercise setup"; slug id) one station ──exercise (mustExist)──► exercise
  gym, its own load grid (loadBase, loadStep, loadMax)

session  (label "Training session"; UUIDv7 from the phone) ──routine──► routine
  slots[], groups[]   the order and rests as run ──exercise (mustExist)──► exercise
    sets[]   the planned sets the session started with, done or not (written at Finish)
  status active ─► finished, at/endsAt, timezone, sport, sessionRPE, revision, healthKitWorkout
  workout ──► substrate.reamde.dev/health/workout   (written by the Apple Health adapter)

set      (one performed set or one segment; UUIDv7) ──session (mustExist, cascade)──► session
  exercise (mustExist), slot, order, ordinal, role, structure, load, reps, rests
  setup (mustExist, no cascade) ──► setup
  continues (mustExist, cascade) ──► set   the head of its chain: drops, myo mini-sets, sides
  what the numbers meant: loadBasis, implements, bodyShare, laterality, repsBasis
  how each value got there: rpe + rpeMethod, rpeScale, completionMethod, timeBasis, restMethod
  target { plannedSet: <planned set key>, the full prescription as it ran }
```

- **Manifests:** [bundle](../../Package/training.yaml),
  [exercise](../../Package/exercise.yaml),
  [setup](../../Package/setup.yaml),
  [routine](../../Package/routine.yaml),
  [session](../../Package/session.yaml),
  [set](../../Package/set.yaml). The
  [package README](../../Package/README.md) maps every property to its
  requirement and holds the rules for reading chains, effective load, volume, Discard and the
  Health link.
- **Examples** from the screenshots: [exercises](../examples/exercises.yaml)
  (30), [routines](../examples/routines.yaml) (the four of "Full Body 3x
  Week (Back-Friendly)"), [sessions](../examples/sessions.yaml) (3
  sessions, 65 sets). [coverage](../examples/coverage.yaml) is not the
  owner's: 61 records for the methods the screenshots do not show, five setups among them.
- **Proof:** `substratectl validate` built from production's commit (`ffd7b967`) passes all of it.
  On 2026-10-31, after the data-model review, a private, throwaway substrate built from that commit
  took all 163 records, answered `unchanged` to a second apply, and refused every invalid write
  tried.

### Decisions taken for the owner

Each may be overturned by the owner; "instead of" names the alternative set aside.

1. **Package home.** A draft in `research/workouts/substrate/training/`, outside
   `svc/mneme-v6/kinds/`, so no Mneme apply picks it up; nothing is applied to `geoah.me` or the
   shared dev server. _Instead of:_ Mneme's tree now, or a first-party `substrate.reamde.dev`
   catalogue, which stays possible later as a row copy that keeps the ids (05 §4.2).
2. **Kinds and names.** `exercise`, `routine` ("Workout routine", the owner's and Hevy's word),
   `session` ("Training session") and `set`; full names keep the routine apart from
   `mneme.rxtx.dev/health/routine`. A folder is a string on the routine plus a `position`.
   _Instead of:_ `workouttemplate` (06 DI 10), a folder kind, or the package names `strength` or
   `gym`.
3. **One record per performed set** (05 Shape B). The session embeds its ordered slots; group
   values are stored once per group. _Instead of:_ sets embedded in the session (every tick
   patches one array, and nested values cannot be filtered or aggregated), or one record per
   exercise performance.
4. **Effort.** `rpe` as entered (decimal, half steps) with `rpeMethod` saying how it got there
   (entered, pre-filled from the plan, copied, imported), an optional `rir`, and `rpeScale`
   (`rirbased` on Spotter's sets); a missing rating is valid, a pre-filled one reads as missing,
   and RIR = 10 − RPE is derived at read on the RIR-based scales only. Targets are ranges
   (`rpeMin`, `rpeMax`). _Instead of:_ one canonical RIR converted at write (02 §5.5), which loses
   what a source's 9.5 meant.
5. **Load.** A quantity in the unit entered, kg by default, lb allowed, with a `loadMode` beside
   it and assistance as its own positive quantity. _Instead of:_ a float in kg, or a negative load
   for assisted machines.
6. **Rests.** 02 §3's slots: set rest and warm-up rest per slot, transition per group member,
   round rest once per group, intra-set pause, side switch, and a per-set override. When the
   routine says nothing, 02 §4.3 and §4.4's evidence-based defaults apply, and a warm-up rest is
   45 s. _Instead of:_ Hevy's one rest per exercise, or a group rest copied onto every member,
   where one value can disagree with itself (02 §3.4).
7. **Myo-reps.** Fagerli's stop rule by default (stop when a mini-set loses a rep from the first),
   with rule, target, cap and total configurable in the data; every mini-set is its own set record
   in a chain. _Instead of:_ a fixed target by default, or one logged total, which hides the
   mini-sets from suggestions.
8. **Drop sets.** A chain with no rest inside, each drop 20 % lighter by default, rounded down to
   the load grid. _Instead of:_ drops as rows inside one record, which no read can filter.
9. **Suggestions.** 03 Part B exactly: one pure function with a one-line reason ("Last time 35 kg
   × 13 at RPE 9.5; try 35 kg × 14", 03 V2). Below the rep range the load is cut; a raise inside a
   session is capped at one step or 10 %, whichever is larger. _Instead of:_ 06 §4.8's "hold the
   load once below the range", or several steps up from history.
10. **PREVIOUS.** The last time the same exercise was done anywhere, on the same setup (a `setup`
    record the set names; none for free weights). _Instead of:_ the last time in the same
    routine, which goes blank when the programme changes.
11. **Session RPE.** Optional, asked at Finish and skippable; when given it becomes the Health
    workout's effort score. _Instead of:_ deriving it from the sets, or leaving it to V1.1.
12. **AlarmKit** is deferred to V1.1; V1's rest end is a time-sensitive notification and the Live
    Activity. _Instead of:_ a V1 opt-in (04 §2.4).
13. **Screen awake** during a session: a setting, on by default. _Instead of:_ off by default.
14. **Apple Health.** `HKWorkoutBuilder` at Finish, `traditionalStrengthTraining` unless the
    routine names another type, indoor, `HKMetadataKeyExternalUUID` and
    `HKMetadataKeySyncIdentifier` set to the session id, no energy, no re-save after an edit in
    V1. _Instead of:_ a MET energy estimate (Activity would count a guess as measured), or
    re-saving after edits (unverified on a device, and substrate keeps the old mirror row).
15. **V1 keeps its data on the device**, starting from the bundled examples; live sync is V2,
    after the owner applies the package. V1 still encodes its model into the package's records,
    and tests prove that mapping. _Instead of:_ sync in V1, which would wait on production.

The package's review rounds took smaller decisions, each argued in its README: planned sets carry
a required `key` that `set.target.plannedSet` names; every exercise reference is `mustExist`; reps
count per side on unilateral work; Discard purges the session and its sets, and there is no
`abandoned` state; no kind declares an index. The data-model review below changed decisions 4 and
10 and added the rest of its list.

### Data-model review, 2026-10-31

Two independent reviews of the package (Fable and Astra) found the kinds and the split between
catalogue, plan and log sound, and the risk in meaning lost when a set is written: a pre-filled
RPE indistinguishable from a rating, exercise edits that reinterpret old loads, and a plan that
leaves no lasting copy. The package was still a draft that nothing had applied, so every change
landed at version 1. What changed and why (the package README's "Data-model review" has the
table, and each manifest the rule):

- **RPE provenance** (`set.rpeMethod`, `set.rirMethod`). The Rows screen pre-fills the plan's
  RPE, so one tap logs it; without a marker, history cannot tell that from a rating, ever. The
  suggestion, PRs and exports read a `prefilled` value as no rating ("assuming RPE 8").
- **How a set was completed** (`completionMethod`): Finish's "Mark them done as shown" is not a
  confirmation of each value.
- **Effort targets as ranges** (`rpeMin`, `rpeMax`, `rirMin`, `rirMax`), because nested names can
  never be renamed later. The suggestion reads the top of the range.
- **A `setup` kind**, and `set.setup` a reference to it: a string can never become a reference
  once stored, and one gym's machine is not another's. `resistanceLabel` for bands and levels.
- **Relative loads** (`loadPercent`, `loadReference`; `referenceLoad` on the target), with the
  rule that `target.load` is always the resolved absolute load.
- **The load's meaning copied onto each set** (`loadBasis`, `implements`, `bodyShare`,
  `laterality`, `repsBasis`), so an exercise edit never reinterprets history.
- **The full prescription on the set** (`tempo`, `timerMode`, `myoStopRule`, `dropReps`,
  `intraSetPause` in `set.target`), and **the plan on the session** (`session.slots[].sets`, every
  planned set, done or not, written at Finish).
- **The effort scale stated** (`rpeScale: rirbased` on every Spotter set), and the README's
  unconditional "RIR = 10 − RPE" corrected: it does not hold on CR10.
- **Time and rest provenance** (`timeBasis`, `restMethod`): an import's set sits at its session's
  start and Spotter's rest ends at a touch, neither a measured instant.
- **`session.sport`** in `health/workout`'s code set (Spotter writes `strength`), with
  `activityType` kept as the Apple Health save's setting only; **`timeZone` renamed `timezone`**,
  as core's `recurring` trait and the Apple Health provider spell it; **`set.continues`
  `mustExist`**, heads written first.
- **Documentation**: the mapping tables' errors fixed (Health Connect's `setIndex` and
  `AmrapTarget`, FIT's `message_index`, `wkt_step_index`, `file_id` and pauses, HealthKit's brand
  name, Hevy's `updated_at`, Strong's `Workout #`, the effort score's 1-to-10 floor); a code-system
  registry with export URIs; rules for a second writer (`ifVersion` on every edit, `revision` per
  record, one owning writer per session, agent changes as proposals); and a reserved list for
  later (conditioning groups, cardio machines, programs, derived PRs and 1RMs, import source
  preservation, fractional seconds, per-set `source`).

The app's side (a codec that reads `{ref}` objects and keeps unknown nested fields, writing the
new fields, the suggestion and presenters honouring provenance and ranges, no session RPE of 0 to
Health) is the app's work, tracked in DESIGN.md.

### From a session to Apple Health and `health/workout`

```text
Finish ─► HKWorkoutBuilder ─► one HKWorkout (ExternalUUID = SyncIdentifier = session id)
                                 │  Substrate Sync, already on the phone, posts every
                                 ▼  HKWorkout of the day to the applehealth webhook
          providers.substrate.reamde.dev/applehealth/workout   applehealth/<account>/<UUID>
                                 │  adapter applehealth@5
                                 ▼
          substrate.reamde.dev/health/workout                  applehealth/<account>/<UUID>
                                 ▲
          session.workout ───────┘  V2, written once a GET finds the row
          session.healthKitWorkout = <UUID>, written as soon as the save returns
```

- **One visit, two records, no duplicate.** Spotter writes the session and its sets; the existing
  Apple Health path writes the one `health/workout`. The session holds what Health cannot (sets,
  loads, RPE); the workout holds what the session does not (heart rate, a sport for weekly load).
  Spotter never writes `health/workout`, which would show the visit twice in every list (05 §5.2).
- **Finding the row.** `<account>` is Substrate Sync's (`substrate-sync-` and eight hex characters
  of an install id kept in its own Keychain), which Spotter, with no App Group, cannot read. V2
  lists `providers.substrate.reamde.dev/applehealth/account`, GETs the workout under each, and
  writes `session.workout` only when one answers. V1 has no server and leaves it empty.
- **The reverse read**, "which session is this workout", is a `referencing` read on the
  `health/workout` path; no extra property is needed.
- **The Health side cannot see the session id yet.** Substrate Sync forwards only `HKTimeZone` in
  a workout's metadata (05 §5.1); forwarding the external UUID is rollout step 7.

### The app

Screens, flows and the watch design are in [DESIGN.md](../../DESIGN.md).
`SpotterKit`, a Foundation-only package, holds the model, the session state machine, the rest
rules, the suggestions and the record encoding; the app and widget targets hold only platform
code (req 35).

**V1, the first real gym session.** Routines grouped by folder, a preview, Start from a routine or
empty. One active session, an event log on disk rewritten atomically on every change, resumable
on launch, from the tab bar accessory and from the Live Activity. Any-order logging with an "Up
next" pointer; supersets and circuits with rounds; warm-ups, drop and myo-rep chains, timed sets
that count down. Set rows whose columns follow the exercise's measurement, grey values from the
suggestion, tap PREVIOUS to copy, RPE in half steps with the reps-in-reserve text, an outlier
guard. One rest coordinator: an absolute end time, ±15 s and Skip, one time-sensitive
notification naming the next set, a Live Activity that never shows a live 0:00. Finish with
editable times, the optional session RPE and the Health save through a retry queue; Discard only
from inside the session. Released through apps.internal.rxtx.dev.

V1 runs on the bundled examples, on the device. The exercises and routines are fit to ship; the
example sessions are test vectors with invented loads, so they are for tests and previews only,
never the owner's history (examples README, "Caution for the V1 app"). Until V2 a routine changes
only with a new build.

**V1.1.** The AlarmKit loud rest alarm (opt-in), a warm-up ramp generator, PRs, a plate calculator and per-gym equipment, the Live Activity in the watch's Smart
Stack, an audio cue over headphones, and editing a finished session.

**V2, live substrate sync.** A substrate client, an outbox (the session's PUT before its first
set, `ifVersion` on every session patch, `revision` raised on every write, Discard drains the
outbox before the purge) and a change-feed cursor. Routines then come live from the console's
agent, and `session.workout` is filled. The plan snapshot V1 writes at Finish
(`session.slots[].sets`) can then be sent at Start too, for resuming on another device; until a
shared-write protocol exists, one device owns a session's writes.

**Later.** A native watchOS app as a remote of the phone's session; importers (Hevy, Strong, FIT)
and exports (Strong CSV, then FIT); editing routines in the app.

## Alternatives considered

- **Adopt an existing format** (FHIR's PGHD Workout, Health Connect, FIT, Hevy's): none carries
  the whole set, and none is shared by two vendors (01 §0).
- **Keep Hevy**: no rest inside a superset, no coach-style hints, and the data stays in Hevy's
  cloud. Importing Hevy history stays on the "later" list.
- **Sessions only in Apple Health**: HealthKit has nowhere to put a set.
- The per-decision alternatives are listed with each decision above.

## Implementation plan

Done: the research (fact-checked 2026-10-30), the package and its examples (three reviews, proven
2026-10-31), and the app's scaffold, listed in `apps/apple/apps.json` and wired to the shared
updater. Agents build V1 to DESIGN.md and release it with `mise run apple:release
apps/apple/spotter`; none of that needs the owner. The steps that do:

1. **Review this proposal.** Overturn any decision above; answer what you can below.
2. **Try V1 on the phone.** Install it from apps.internal.rxtx.dev and help with the checks the
   research README lists under "Still to check on a device", such as whether a stale Live
   Activity re-renders on time and whether a Spotter workout earns Exercise ring minutes.
3. **Approve the move ticket.** An agent files it as the next free `T-nnn` in
   svc/mneme-v6/docs/tickets/ (`svc/mneme-v6/docs/tickets/README.md` in rxtx.dev), in state `decision`
   until the owner approves. Its steps are the package README's "Moving it into Mneme": amend
   `svc/mneme-v6/docs/plan.md`, whose V6.6 puts `workout`, `workoutset`, `workouttemplate` and
   `exercise` under `health/`, and the package-name list in `svc/mneme-v6/CLAUDE.md`; then copy
   the manifests to `svc/mneme-v6/kinds/training/`, keeping version 1.
4. **Allow the scratch-repository apply.** With the owner's go-ahead, an agent applies
   `substrate.reamde.dev/health`, the package and the example records in a scratch repository of
   its own (`svc/mneme-v6/CLAUDE.md`, "A scratch repository"; the examples README), never the
   owner's `mneme.rxtx.dev`. `coverage.yaml` stays away from the owner's history. If production
   has moved past `ffd7b967`, the server-only checks are repeated.
5. **Approve the console's training sub-agent.** Its `writes` name `training/exercise` and
   `training/routine`, and both join the Mneme assistant's `writes` (req 20). Its prompt: write
   an exercise before a routine names it; keep each planned set's `key` across edits and never
   reuse one; clear a property with a PATCH naming it `null`, since a PUT never clears.
6. **Apply to production.** The owner applies the package to `geoah.me`, writes Spotter's
   `observations/source` record (class `app`), and issues Spotter's token: writes on the training
   kinds, reads on `providers.substrate.reamde.dev/applehealth/account` and
   `substrate.reamde.dev/health/workout`.
7. **Approve the Substrate Sync change.** A substrate pull request makes Substrate Sync forward
   `HKMetadataKeyExternalUUID` (the key `HKExternalUUID`) and the source bundle id in each
   workout's metadata. The mirror already stores metadata as JSON, so no kind changes; a Mneme
   function could then link a session by its id, even after a re-save.
8. **Turn on V2** against `geoah.me`, with the kind names read from one configuration point (req
   17).

## Open questions for the owner

Only what the decisions above leave open. The research README's number follows each, as "(Q23)".

1. Is an Apple Watch worn at the gym, with Apple's Strength workout started on it? If so, Spotter
   should link to that workout, not write a second. Is a chest strap used? (Q23)
   _Owner, 2026-10-31: set aside for now; revisit later._
2. A WHOOP band adds a second `health/workout` for the same visit. Is read-time deduplication by
   source policy enough, and which of the session and the workouts is `supporting` on timelines?
   (Q25, Q26)
3. Once replacement is proven on a device, should an edit re-save the Health workout, and may the
   stale mirror row be deleted? (Q27)
4. The screenshots show a crossed-out bell in the status bar, which suggests the phone is on
   silent at the gym. Should V1.1's AlarmKit alarm then be on by default? (Q10)
5. The RPE keypad: 6 to 10 in half steps as Hevy, or add 6.5, values below 6, or RIR wording?
   Myo-rep pauses as breaths or seconds? (Q1, Q12)
6. With no target in the routine: 8 to 12 reps at RPE 8 (the draft's default), or RPE 9 as the
   Hevy logs suggest? (Q15)
7. Which gyms, bars, plates, dumbbell steps and stack steps? Machine setups are now `setup`
   records (one exercise at one place, with its own grid, joined by a merge when two are the same
   machine); how should they be named? (Q5, Q21)
8. Body mass from HealthKit on or before the session day? What body share for push-ups and dips?
   (Q22)
9. What makes a set "hard" for weekly volume: RIR 4 or less, RIR 3 or less, or every working set?
   (Q14)
10. Are EMOMs, circuits for time or Tabata intervals needed? Their names are reserved and each
    would be additive. Percent loads are declared (`loadPercent`), unused by V1; where should the
    reference number (a 1RM or training max) live? (Q13)
11. Compute derived values (set volume, e1RM, session tonnage) in clients, as drafted, or store
    them on records? (Q31)
12. The draft purges a discarded session and writes no set record for a skipped planned set (the
    session's plan snapshot lists it). Keep that, or keep an abandoned record? Should Finish keep
    a slot nothing was done in, so its plan snapshot survives? (Q32)
13. The exercise catalogue: written by the agent as needed, or seeded from free-exercise-db?
    Equipment or movement first in names? Where do images come from? (Q33)
14. Spotter's own token, or Substrate Sync's sign-in shared through the Keychain? (Q34)
15. Should in-session edits or suggestions be proposed back to the routine for the agent to
    review? Should a second device, such as an iPad, see the open session live? (Q35, Q36)
16. Which history to import first: Hevy, Strong or none? Is there older data in Fitbod, FitNotes
    or Garmin? Matching Hevy's exercises needs template ids from the owner's Hevy account. (Q37)
17. PR celebrations: Hevy's banner and medal, or something quieter? Should the Health workout
    carry the routine's name as its brand name? (Q38, Q24)
18. The screenshots show only part of each routine: the cards stop after two lines, and Day 3's
    title is hidden. What are Day 3's name and each day's full list, so the bundled routines are
    the owner's own?
    _Owner, 2026-10-31: keep the stand-in Day 3 and the partial lists for now._
