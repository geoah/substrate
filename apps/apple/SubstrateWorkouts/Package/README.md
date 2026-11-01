# The training package

Substrate Workouts' own package: the exercises the owner trains and the
machines they are done on, the workout routines the console's agent writes,
and the training sessions and sets the app logs. The manifests are the YAML
files beside this one, under the authority `~`; the app carries them
(`WorkoutsKit/Sources/WorkoutsCore/Resources/Package/training.json`, written
by `apps/apple/scripts/workouts-package.py`) and declares them in a
repository through its `declare:~/training` scope, so they land as
`<authority>/training` (decision record
[0203](../../../../docs/decisions/0203-an-app-keeps-its-own-package-under-a-declare-scope.md)).
Below, `~/training/…` is that name.

| File | Holds |
| --- | --- |
| `training.yaml` | the package document (its version) and the bundle document (`requires`, `installs`) |
| `exercise.yaml` | `~/training/exercise` |
| `setup.yaml` | `~/training/setup`, labelled "Exercise setup" |
| `routine.yaml` | `~/training/routine`, labelled "Workout routine" |
| `session.yaml` | `~/training/session`, labelled "Training session" |
| `set.yaml` | `~/training/set` |

**Changing it.** A change ships as the next `version` in `training.yaml`,
and only additive changes (a new kind, a new optional property, a new enum
value): the server refuses the app's token an apply that removes anything the
package holds, or that it would have to confirm as lossy, so a narrowing
waits for the owner. A bundle the app sends lists its kinds and nothing more
(no `modules`, `oauth2` or `inputs`), and a kind declares no `indices` or
`embed`. Run the generator, then `mise run apple:test:workouts`, whose
codec drift test reads the kinds' property names from the generated
`kinds.json`.

The package was designed from a body of research into training methods,
progression, exchange formats and other apps, and reviewed twice before its
first write. Section numbers such as "02 §4.3", requirement numbers ("req
13") and open questions below cite that research, which stays with the app's
history outside this tree; the decisions they led to are all stated here.

The manifests' comments say why each choice was made; this file collects
them, maps every property to the requirement it serves, and lists what was
left out.

## The shape

```text
routine ──slots[].exercise (mustExist)──► exercise ◄── variationOf (exercise)
   ▲                                       ▲  ▲  ▲
   │ routine                               │  │  └── setup.exercise (required, mustExist)
   │                                       │  └───── slots[].exercise (mustExist)
session ──workout──► substrate.reamde.dev/health/workout
   ▲    ──source───► substrate.reamde.dev/observations/source
   │    slots[].sets: the planned sets it started with
   │ session (required, mustExist, cascade)
  set ──exercise (required, mustExist)──► exercise
  set ──setup (mustExist, no cascade)───► setup
  set ──continues (mustExist, cascade)──► set (the head of its chain)
```

- **exercise**: a catalogue entry. Ids are readable slugs chosen by the
  writer (`chest-press-machine`).
- **setup**: one station an exercise is done on, such as one gym's chest
  press, with its own load grid. Ids are readable slugs chosen by the writer
  (`hotel-gym-chest-press`).
- **routine**: the plan, written by the console or its agent. Ids are
  readable slugs (`fully-body-collection-1`).
- **session**: one gym visit as the app ran it. Ids are lowercase UUIDv7s
  chosen on the phone.
- **set**: one performed set, or one drop, mini-set, cluster or side of a
  structured set. Ids are lowercase UUIDv7s chosen on the phone.

## Exercise

`~/training/exercise`, purpose `primary`, title `{name}`, no
traits.

| Property                                         | Type                                                     | Req       | Why                                                                                                                                                                                                                                         |
| ------------------------------------------------ | -------------------------------------------------------- | --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `name`                                           | string, required                                         | 13        | The name the owner reads, such as "Chest Press (Machine)". The id does not change when it does.                                                                                                                                             |
| `aliases`                                        | string, repeated                                         | 13        | Search words: RDL, DB, the gym's name for a machine.                                                                                                                                                                                        |
| `measurement`                                    | enum, required                                           | 13, 28, 5 | Hevy's eight exercise types spelled by the enum rule, plus `weightedbodyweightreps`. It decides the set row's columns and how a load reads. `repsonly`, `duration` and `distanceduration` move no load. |
| `equipment`                                      | enum                                                     | 13        | Hevy's nine values plus `cable` and `plateloaded`, which have their own default steps.                                                                                                                                                      |
| `laterality`                                     | enum                                                     | 13        | bilateral, unilateral, alternating. On the last two a set's reps count per side. A set copies it when logged.                                                                                                                       |
| `loadBasis`                                      | enum                                                     | 5, 13     | What a load counts: the total, one implement, or one side. No source records it. A set copies it when logged.                                                                                                                       |
| `implements`                                     | int, at least 1                                          | 5, 13     | How many implements a set holds: 2 for a lateral raise, 1 for a single-leg Romanian deadlift. Absent means one. A set copies it when logged.                                                                          |
| `primaryMuscles`, `secondaryMuscles`             | enum, repeated                                           | 13, 32    | Hevy's twenty lay groups. Fractional counting reads 1 for primary and 0.5 for secondary.                                                                                                                                          |
| `restClass`                                      | enum                                                     | 13, 24    | The rows of 02 §4.3, which give the default set rest when the routine names none.                                                                                                                                                           |
| `variationOf`                                    | reference to `exercise`                                  | 13        | wger's variation group as a pointer.                                                                                                                                                                                                        |
| `codes`                                          | object, repeated: `system`, `code`, `version`, `display` | 14, 39    | The same fields, names and types as `observations/metric.codes`; `system` and `code` are required here. Importers resolve by `(system, code)`. The systems are listed in [Code systems](#code-systems).                                     |
| `loadBase`, `loadStep`, `loadMax`                | quantity, kg or lb, at least 0                           | 13, 29    | The load grid of 03 §10.5. `loadStep`'s unit is the exercise's unit. A setup overrides it for one station.                                                                                                               |
| `bodyShare`                                      | decimal, 0 to 1                                          | 13        | The share of body mass a bodyweight set moves. Read only for the three bodyweight measurements. A bodyweight set copies it when logged.                                                                                                     |
| `defaultRepsMin`, `defaultRepsMax`, `defaultRPE` | int, int, decimal                                        | 13        | The targets when a routine names none.                                                                                                                                     |
| `repGain`                                        | int                                                      | 13        | Reps added per session at an unchanged load.                                                                                                                                                                                                |
| `dropPercent`                                    | decimal, 0 to 100                                        | 13        | Each drop's cut; absent means 20.                                                                                                                                                                                                           |
| `timeStep`                                       | duration                                                 | 13        | Time added per session to a timed set; absent means 5 s.                                                                                                                                                                                    |
| `myoStopRule`, `myoMaxMiniSets`                  | enum, int                                                | 13        | The myo rule of 03 §11.6; absent means `repdrop` and 5.                                                                                                                                                                                     |

Notes:

- **No `unit` property.** The suggestion engine's settings have one, but a separate unit could
  disagree with the step it describes. `loadStep` carries it: a 10 lb pin
  makes a pound exercise in a kilogram gym. With no step, the owner's
  default unit applies.
- **No `mode` or `canAddLoad`.** `measurement` says both:
  `weightedbodyweightreps` adds load to body mass,
  `bodyweightassistedreps` subtracts assistance, `bodyweightreps` adds
  nothing, and the three no-load measurements have no grid at all.
- **Default grids by equipment** are the engine's, with two rows of their own: a
  plate-loaded machine starts at 0 and steps by 2.5 kg (one plate a side),
  and `plate`, `resistanceband`, `suspension`, `other` and `none` have no
  default grid, so the load is the lifter's until `loadStep` names one
  (base 0).
- **Exercise references are `mustExist`** on the routine slot, the session
  slot and the set. A slug the console's agent mistypes
  (`chest-press-machin`) is refused when the routine is written, not found
  missing at the gym, where the app would have no name, no measurement (the
  set row's columns) and no load grid. The check is free now; added later,
  it would be a narrowing that admission counts against every dangling value
  already stored (`docs/vocabulary.md`, "Evolution"). A tombstoned or
  merged-away exercise still counts as existing, and only the references a
  write names are checked, so a PATCH that leaves `exercise` out is never
  refused (`internal/engine/references.go`).
- **A set copies what its numbers mean.** `laterality`, `loadBasis`,
  `implements` and `bodyShare` are defaults for future sets: each set copies
  them, resolved, when it is logged ([Set](#set)), so an edit here never
  reinterprets a stored load or rep count.
- **Settings are the kind's own properties, not an object.** Each can then
  be renamed or made required by declaration; an object's fields cannot
 . The kind stays small, so
  the conversion ceiling never applies to it.
- **Never a record mapping's target.** A kind a `recordmapping` targets
  takes server-assigned ids only (decision record 0049), which would break
  the readable slugs.
- **Hevy template ids are not in the examples.** Hevy identifies an exercise
  only by `exercise_template_id`, read from the owner's account
  (`GET /v1/exercise_templates`). An
  importer that meets an unknown code holds the pending mapping itself and
  writes nothing for it until the owner confirms it; the confirmation adds
  the code to an exercise here or creates a new one. A set cannot name a
  placeholder: `set.exercise` is required and `mustExist`.

## Setup

`~/training/setup`, purpose `supporting`, label "Exercise
setup", title `{name}`, no traits. One station an exercise is done on: the
same chest press is a 5 kg stack in one gym and a 10 lb stack in another, so
its PREVIOUS, its grid and its suggestions differ.

| Property                          | Type                                           | Req    | Why                                                                                      |
| --------------------------------- | ---------------------------------------------- | ------ | ---------------------------------------------------------------------------------------- |
| `name`                            | string, required                               | 15     | "Chest Press (Machine), hotel gym".                                                      |
| `exercise`                        | reference to `exercise`, required, `mustExist` | 15     | The exercise done on it. One setup is one exercise at one place.                         |
| `gym`                             | string                                         | 15     | Where it is, such as "Hotel gym".                                                        |
| `loadBase`, `loadStep`, `loadMax` | quantity, kg or lb, at least 0                 | 13, 29 | This station's grid; absent means the exercise's. `loadStep`'s unit is the setup's unit. |
| `notes`                           | text                                           | 15     | A seat height, or which of two machines it is.                                           |

Notes:

- **Why a kind and not a label.** `set.setup` was a free string in the first
  draft. A property's datatype cannot change while live records hold it, so
  a string could never become a reference; the kind exists before the first
  set names one. A reference also filters through the refs table, as
  `exercise` does, and `referencing` a setup lists its sets.
- **Deleting a setup never touches a set.** `set.setup` has no `onDelete`;
  a deleted setup leaves its sets naming it. Two
  setups found to be the same machine are joined with a merge.
- **Its grid overrides the exercise's**, field by field; every other
  suggestion setting stays on the exercise.
- **Substrate Workouts writes none yet.** Its machine history is shared,
  like free weights'. The catalogue's test fixtures have five setups, for a
  hotel gym and an old gym.

## Routine

`~/training/routine`, purpose `primary`, label "Workout
routine", title `{name}`, no traits.

| Property                                                                   | Type                                           | Req                                        | Why                                                                                                                                                                                                                        |
| -------------------------------------------------------------------------- | ---------------------------------------------- | ------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `name`                                                                     | string, required                               | 16                                         | "Day 1 – Strength Foundation".                                                                                                                                                                                             |
| `folder`                                                                   | string                                         | decisions table, 06 §3 V1 item 1           | The folder it is listed under.                                                                                                                                                                                             |
| `position`                                                                 | int, from 1                                    | decisions table, 06 §3 V1 item 1           | Its place in the folder.                                                                                                                                                                                                   |
| `notes`                                                                    | text                                           | 20, 06 §3 V1 item 2                        | What the routine is for, shown in the preview before Start. `text`, as first-party `notes` is.                                                                                                                             |
| `deload`                                                                   | bool                                           | 12, 02 §6.3                                | A deload routine; a session started from it copies the flag.                                                                                                                                                               |
| `activityType`                                                             | enum                                           | 34                                         | The HealthKit workout type its sessions are saved as; absent means traditional strength training. A session copies it at Start. The Health save's setting only: a session's sport is `session.sport`.                      |
| `groups`                                                                   | object, repeated                               | 4, 16                                      | Supersets, circuits and paired sets, each stored once.                                                                                                                                                                     |
| `groups[].key`                                                             | string, required                               | 4                                          | What member slots name.                                                                                                                                                                                                    |
| `groups[].groupKind`                                                       | enum, required                                 | 16                          | superset, triset, giantset, circuit, paired.                                                                                                                                                                               |
| `groups[].pairing`                                                         | enum                                           | 16                          | How members relate; picks the default transition.                                                                                                                                                                |
| `groups[].rounds`                                                          | int                                            | 16                          | Absent means as many as the member with the most working sets; warm-ups run before round 1.                                                                                                                                |
| `groups[].roundRest`                                                       | duration                                       | 16, 24                                     | The rest after a round, once per group.                                                                                                                                                                                    |
| `slots`                                                                    | object, repeated                               | 16                                         | The exercises in order.                                                                                                                                                                                                    |
| `slots[].key`                                                              | string, required                               | 16, 10                                     | Unique in the routine; a session slot keeps it as `routineSlot`.                                                                                                                                                           |
| `slots[].exercise`                                                         | reference to `exercise`, required, `mustExist` | 16                                         | The exercise done in the slot.                                                                                                                                                                                             |
| `slots[].group`                                                            | string                                         | 4                                          | The key of the group it belongs to.                                                                                                                                                                                        |
| `slots[].setRest`, `slots[].warmupRest`                                    | duration                                       | 16, 24                                     | Per slot.                                                                                                                                                                                                                  |
| `slots[].transition`                                                       | duration                                       | 16, 24                                     | Per group member, to the next member in the round.                                                                                                                                                                         |
| `slots[].sideSwitch`, `slots[].sideOrder`, `slots[].sideMatch`             | duration, enum, bool                           | 16                         | Per-side settings of a unilateral slot.                                                                                                                                                                                    |
| `slots[].notes`                                                            | text                                           | 4                                          | Slot notes, such as a seat height.                                                                                                                                                                                         |
| `slots[].sets`                                                             | object, repeated                               | 16                                         | Planned sets in order, warm-ups first.                                                                                                                                                                                     |
| `sets[].key`                                                               | string, required                               | 10                                         | Unique in the slot and kept across edits (`w1`, `s2`); a performed set names it in `target.plannedSet`.                                                                                                                    |
| `sets[].role`                                                              | enum, default `working`                        | 6                                          | warm-up or working.                                                                                                                                                                                                        |
| `sets[].roleLabel`                                                         | enum                                           | 6                                          | top or backoff, on a working set.                                                                                                                                                                                          |
| `sets[].structure`                                                         | enum, default `straight`                       | 6                                          | straight, drop, myo, myomatch, restpause, cluster.                                                                                                                                                                         |
| `sets[].termination`                                                       | enum                                           | 6, 16                                      | reps, reprange, amrap, failure, rirstop, time, distance, totalreps. An effort target is a stop rule only under `rirstop`; otherwise it selects the load.                                                         |
| `sets[].repsMin`, `sets[].repsMax`                                         | int                                            | 16                                         | Exact reps are equal values.                                                                                                                                                                                               |
| `sets[].load`, `sets[].assistance`                                         | quantity, kg or lb                             | 5, 16                                      | An absolute load (the added load on bodyweight plus) or assistance. A seed: history wins once it exists. `load` wins over `loadPercent` when both are given.                                                    |
| `sets[].loadPercent`, `sets[].loadReference`                               | decimal 0 to 200, enum                         | 16, open question 13        | A load as a percentage of a tested 1RM, an e1RM, a training max or body mass. The app resolves it to an absolute load at Start; V1 does not read it yet.                                                                   |
| `sets[].rpeMin`, `sets[].rpeMax`, `sets[].rirMin`, `sets[].rirMax`         | decimal                                        | 8, 16                                      | The effort target as a range, as the agent wrote it; one value is min equal to max. The suggestion reads `rpeMax`, the range's top, as the load selector.                                                        |
| `sets[].durationMin`, `sets[].durationMax`, `sets[].timerMode`             | duration, duration, enum                       | 16, 31                                     | A timed set's target and how its timer runs; countdown by default.                                                                                                                                                         |
| `sets[].distance`                                                          | quantity, m                                    | 16                                         | A carry's distance.                                                                                                                                                                                                        |
| `sets[].tempo`                                                             | string, four phases                            | 16                                         | Display only, such as 3-1-1-0.                                                                                                                                                                                             |
| `sets[].miniSetReps`, `sets[].miniSets`, `sets[].totalReps`                | int                                            | 16, decisions table                        | Myo-rep, rest-pause and cluster parameters: the mini-set target, the cap, the total. `miniSets` counts after the first bout for every structure, so four clusters are `miniSets: 3`.                                       |
| `sets[].myoStopRule`                                                       | enum                                           | 16                         | This set's stop rule, overriding the exercise's.                                                                                                                                                                           |
| `sets[].drops`, `sets[].dropPercent`, `sets[].dropStep`, `sets[].dropReps` | int, decimal, quantity, int                    | 16, decisions table | Drop-set parameters: how many drops, each drop's cut as a percentage or a load (the step wins when both are given), and each drop's reps (absent: to failure). `dropPercent` and `dropStep` are also a back-off set's cut. |
| `sets[].intraSetPause`                                                     | duration                                       | 16, 24                                     | The pause inside a structured set.                                                                                                                                                                                         |
| `sets[].rest`                                                              | duration                                       | 16                         | Overrides the rest after this set.                                                                                                                                                                                         |

Notes:

- **Absent is not zero.** A rest left out takes the app's default. `PT0S` means no rest and no timer, as Hevy's "Rest: OFF".
- **Defaults are not declared.** A `default:` on a field is stored into
  every object the agent writes, which would hide whether a value is the
  default or the owner's choice. The two declared defaults (`role:
  working`, `structure: straight`) are the exceptions, because those values
  mean the same thing whoever wrote them.
- **Planned sets have keys.** Substrate serves no record as it was at an
  older version ("a past version cannot be reconstructed from public rows",
  `docs/changelog.md`), so a performed set that named its planned set by
  position would point at a different one as soon as the agent inserted a
  warm-up. The key is required, because a field inside an object can never
  be made required later without a backfill, and the agent keeps a planned
  set's key when it edits the routine. `w1` and `s2` are the examples'
  convention, not a rule.
- **A PUT merges.** `slots` and `groups` are replaced whole by a PUT that
  names them, but a property the PUT leaves out keeps its old value
  (`docs/api.md`, "put merges and never prunes"): a routine rewritten
  without `deload` stays a deload routine, and its sessions stay discounted.
  Clearing a property is a PATCH naming it `null`. An agent that writes
  routines is told so in its prompt.
- **Segments are not written out.** A myo set's mini-sets, a drop set's
  drops and a unilateral set's sides are generated by the app from the
  planned set and logged as chained set records.
- **No `revised`, no temporal trait.** A plan has no instant of its own, and
  it is not rewritten from an upstream that numbers its versions. Its record
  `status.version` is the version a session keeps, to tell that the routine
  changed since.
- **Effort targets are ranges**, as reps are: `rpeMin` equal to `rpeMax` is
  one value, "RPE 7 to 8" is a range. A field inside an object can never
  be renamed, so the pair is declared now rather than beside a single `rpe`
  later.
- **Percent loads are declared, not used yet.** `loadPercent` and
  `loadReference` give the shape; the reference number itself (a 1RM or
  training max per exercise and setup) has no home yet, and when one is
  needed it is a derived observation ([Left out](#left-out-and-why)). A set
  done from such a plan keeps the resolved absolute load in
  `target.load` when the reference is known; Substrate Workouts knows only
  `bodymass` (the session's `bodyMass`).

## Session

`~/training/session`, purpose `primary`, label "Training
session", title `{name|routine|id}`, traits
`substrate.reamde.dev/core/temporal(range)` and
`substrate.reamde.dev/core/revised`, no declared index.

| Property                         | Type                                                                                                                                                                 | Req                      | Why                                                                                                                                                                                                                                                   |
| -------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `at`, `endsAt`                   | the trait's                                                                                                                                                          | 12, 17                   | Start and end, both editable at Finish.                                                                                                                                                                                                      |
| `name`                           | string                                                                                                                                                               | 12, 17                   | The heading on the local clock, written by the app ("Fully Body Collection #1, Fri 30 Oct 13:42"), because a template renders an instant in UTC. Without one (an importer's, a console write) the title is the routine's, else the id.                |
| `status`                         | state: `active`, `finished`                                                                                                                                          | 12, 18, 22               | Born `active`; `active` to `finished` at Finish. Moved only by PATCH.                                                                                                                                                                                 |
| `routine`                        | reference to `routine`                                                                                                                                               | 2, 12                    | The plan it started from. No `mustExist`: deleting a routine leaves the history alone.                                                                                                                                                                |
| `routineVersion`                 | int                                                                                                                                                                  | 12                       | The routine record's `status.version` at Start, which tells a reader the routine has changed since.                                                                                                                                                   |
| `deload`                         | bool                                                                                                                                                                 | 12                       | Copied from the routine, editable.                                                                                                                                                                                                                    |
| `sport`, `sportSource`           | enum (`health/workout`'s 19 values), string                                                                                                                          | 12, 17                   | What sport the session was, in the code set `health/workout` uses, so a Substrate Workouts session and a Garmin or WHOOP strength workout count as one sport. Substrate Workouts writes `strength`; an importer maps its source's word and keeps the word in `sportSource`. |
| `activityType`                   | enum                                                                                                                                                                 | 34                       | Copied from the routine at Start; absent means traditional strength training. The Health save and its retries read it here. Export only: it says what Health was sent, not what the session was.                                                      |
| `slots`                          | object, repeated: `key` (required), `exercise` (required, `mustExist`), `group`, `routineSlot`, `notes`, `setRest`, `warmupRest`, `transition`, `sideSwitch`, `sets` | 4, 10, 23, 24            | The exercises in the order shown. Reordering, adding, replacing and grouping is one PATCH. The rests are written when the session has no routine slot for them or the owner changed one in the gym.                                                   |
| `slots[].sets`                   | object, repeated: the routine's planned-set fields, `key` required                                                                                                   | 10, 16; open question 32 | The routine slot's planned sets as the session started with them, done or not, with their keys, so the plan can be set against what was done after the routine changes or goes. Written at Finish. Absent on a slot with no routine slot.             |
| `groups`                         | object, repeated: `key` (required), `groupKind` (required), `pairing`, `rounds`, `roundRest`                                                                         | 4                        | The session's supersets, stored once each, with the values of a group made in the gym or changed there.                                                                                                                                               |
| `pauses`                         | object, repeated: `start` (required), `end`                                                                                                                          | 12                       | The paused spans. Named `start` and `end` because `at` and `endsAt` are reserved at every level.                                                                                                                                                      |
| `utcOffsetStart`, `utcOffsetEnd` | int, -1080 to 1080                                                                                                                                                   | 12, 17                   | As `health/workout`; the aggregate read's recorded-local calendar reads them.                                                                                                                                                                         |
| `timezone`                       | timezone (an IANA name)                                                                                                                                              | 34                       | The zone the session was recorded in, which `HKMetadataKeyTimeZone` takes and two offsets cannot give back. Spelled as core's `recurring` trait and the Apple Health provider spell it.                                                               |
| `bodyMass`                       | quantity, kg or lb                                                                                                                                                   | 12                       | The body mass on the day, from Apple Health, so bodyweight sets keep their effective load.                                                                                                                                                 |
| `sessionRPE`                     | decimal, 0 to 10                                                                                                                                                     | 12, 34                   | The CR10 rating asked at Finish, if given. Not named `rpe`, because the sets' `rpe` is on another scale. Apple's effort score runs from 1, so a 0 is never sent to Health.                                                                            |
| `notes`                          | text                                                                                                                                                                 | 12, 17                   | Session notes.                                                                                                                                                                                                                                        |
| `healthKitWorkout`               | string, uppercase UUID                                                                                                                                               | 12, 34                   | The UUID HealthKit returned for the saved workout.                                                                                                                                                                                                    |
| `workout`                        | reference to `substrate.reamde.dev/health/workout`                                                                                                                   | 12, 34                   | `applehealth/<account>/<UUID>`, where `<account>` is Substrate Sync's; written once Substrate Workouts has found the row ([Apple Health](#apple-health)).                                                                                                        |
| `source`                         | reference to `substrate.reamde.dev/observations/source`                                                                                                              | 17, 39                   | The app or import it came from.                                                                                                                                                                                                                       |
| `about`                          | reference, any kind                                                                                                                                                  | 17                       | Who trained; absent means the owner.                                                                                                                                                                                                                  |
| `origin`                         | reference, any kind                                                                                                                                                  | 17, 39                   | The record an importer built it from. Part of `revised`'s contract.                                                                                                                                                                                   |
| `revision`                       | int                                                                                                                                                                  | 12, 17, 18, 34           | Raised with every change the phone sends; also `HKMetadataKeySyncVersion`. Part of `revised`'s contract.                                                                                                                                              |
| `adapter`                        | string                                                                                                                                                               | 17, 39                   | What wrote it, `<name>@<version>` (`substrate-workouts@0.1.0`, or an importer's function).                                                                                                                                                                       |

Notes:

- **What `revised` guards.** The write path refuses a `revision` below the
  one a LIVE record holds (decision record 0162), so an outbox replay of an
  old PUT that arrives after a newer write is refused as stale, where a
  plain PUT, which merges and never prunes, would write the old values
  back. It guards nothing else: a create and a put onto a tombstone start a
  record afresh (`internal/engine/observed.go`), a put after a purge creates
  it again, and an equal revision is admitted
  (`internal/vocabulary/observed.go`), so Discard empties the outbox first
  ([How the app writes](#how-the-app-writes)).
- **Slots, and the plan as it stood.** Each slot from a routine keeps that
  routine slot's planned sets as the session started with them
  (`slots[].sets`), every one, done or not, with the routine's keys. No
  read serves a routine as it was at an older version, and `routineVersion`
  only says it has changed, so this is the one lasting copy of what was
  planned. The fields are the routine's planned-set fields, with a
  percentage load resolved into `load`. Each performed set still carries the
  targets it was done against (`set.target`), and no set record is written
  for a planned set nobody did. A slot that Finish drops
  because nothing in it was done takes its planned sets with it; keeping
  such slots is the same open question. The rests the owner set in the gym,
  or that an empty session's exercises run with (last time's, else the
  defaults), are kept where they apply: `setRest`, `warmupRest`,
  `transition` and `sideSwitch` on the slot, `pairing`, `rounds` and
  `roundRest` on the group. The rest each set got is on the set
  (`restPlanned`).
- **Not a resume source.** A session in progress lives on the phone, as
  one event log and its folded state, and the app resumes from that log.
  Substrate Workouts writes its records at Finish, the plan snapshot with
  them. A writer that sends the snapshot at Start gives a second device the planned
  sets still to do; a running rest timer stays the phone's.
- **`sport` is what the session was; `activityType` is what Health was
  sent.** Both are kept: the first is `health/workout`'s code set, which a
  reader counting strength sessions across sources filters on; the second
  is the Apple Health save's choice and nothing reads it but the save and
  an export.
- **Discard purges.** From inside the session, Discard deletes the record
  with `?purge=true`: the row is collected at once and its sets are
  tombstoned with it (`set.session` cascades), so lists and aggregates stop
  showing them before the next session reads PREVIOUS. A plain DELETE
  would leave the sets listed until the collector's next pass, about five
  minutes, and a late PUT inside that window would restore the session.
- **No way back.** There is no transition out of `finished`. Editing a
  finished session's sets or notes needs no state move; reopening is an
  additive transition if the owner wants it.

## Set

`~/training/set`, purpose `supporting`, title `{exercise}`,
traits `substrate.reamde.dev/core/temporal(point)` and
`substrate.reamde.dev/core/revised`, no declared index.

| Property                                                                                                                                                                                                   | Type                                                               | Req                | Why                                                                                                                                                                                                                                                      |
| ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------ | ------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `at`                                                                                                                                                                                                       | the trait's                                                        | 9, 3               | When the set ended (the tick). `timeBasis` says what kind of instant it is.                                                                                                                                                                              |
| `session`                                                                                                                                                                                                  | reference to `session`, required, `mustExist`, `onDelete: cascade` | 3, 18              | The session owns its sets; the outbox writes the session first.                                                                                                                                                                                          |
| `exercise`                                                                                                                                                                                                 | reference to `exercise`, required, `mustExist`                     | 3                  | The exercise actually done, which may be a substitute.                                                                                                                                                                                                   |
| `slot`                                                                                                                                                                                                     | string, required                                                   | 3                  | The session slot's key.                                                                                                                                                                                                                                  |
| `order`                                                                                                                                                                                                    | int, required                                                      | 3                  | The row in its slot, from 1, warm-ups first, a chain's records in turn.                                                                                                                                                                                  |
| `ordinal`                                                                                                                                                                                                  | int                                                                | 11                 | Its number among the slot's sets of the same role, a chain counted once. Working set n is compared with working set n.                                                                                                                                   |
| `setup`                                                                                                                                                                                                    | reference to `setup`, `mustExist`, no `onDelete`                   | 15                 | The station; history is kept per setup. Absent for free weights, which share their history everywhere. V1 writes none.                                                                                                                                   |
| `role`                                                                                                                                                                                                     | enum, default `working`                                            | 6, 32              | warm-up or working. Filtered on for hard sets, PRs and volume.                                                                                                                                                                                           |
| `roleLabel`                                                                                                                                                                                                | enum                                                               | 6                  | top or backoff.                                                                                                                                                                                                                                          |
| `structure`                                                                                                                                                                                                | enum, default `straight`                                           | 6                  | The same on every record of a chain.                                                                                                                                                                                                                     |
| `continues`                                                                                                                                                                                                | reference to `set`, `mustExist`, `onDelete: cascade`               | 7                  | The head of the chain this record continues. The head owns its chain and is written first.                                                                                                                                                               |
| `chainPosition`                                                                                                                                                                                            | int                                                                | 7                  | 1 for the head, 2 for the first drop, mini-set, cluster or second side.                                                                                                                                                                                  |
| `side`                                                                                                                                                                                                     | enum: left, right                                                  | 7                  | When the sides are logged as two records.                                                                                                                                                                                                                |
| `loadMode`                                                                                                                                                                                                 | enum                                                               | 5                  | external, bodyweight, bodyweightplus, assisted. Absent when the exercise moves no load.                                                                                                                                                                  |
| `load`                                                                                                                                                                                                     | quantity, kg or lb                                                 | 5                  | In the unit entered, as `loadBasis` counts it; the added load on bodyweight plus.                                                                                                                                                                        |
| `assistance`                                                                                                                                                                                               | quantity, kg or lb                                                 | 5                  | A positive amount, never a negative load.                                                                                                                                                                                                                |
| `resistanceLabel`                                                                                                                                                                                          | string                                                             | 5   | A resistance that is not a load, as the equipment labels it: a red band, level 7. Never turned into kilograms.                                                                                                                                           |
| `reps`                                                                                                                                                                                                     | int, at least 0                                                    | 3, 6               | Completed reps, counted as `repsBasis` says. 0 is allowed for a missed single (Strong).                                                                                                                                                                  |
| `duration`                                                                                                                                                                                                 | duration                                                           | 17, 31             | A timed set's time, past its target if the owner kept going. First-party name and type.                                                                                                                                                                  |
| `distance`                                                                                                                                                                                                 | quantity, m                                                        | 17                 | A carry's distance. First-party name and type.                                                                                                                                                                                                           |
| `loadBasis`, `implements`, `bodyShare`                                                                                                                                                                     | enum, int, decimal 0 to 1                                          | 5, 13              | What the load counted when the set was logged, copied from the exercise and resolved: the two with any load, `bodyShare` with the three bodyweight modes.                                                                                                |
| `laterality`, `repsBasis`                                                                                                                                                                                  | enum, enum: total, perside                                         | 3, 13              | Whether the exercise worked the sides together, one at a time or in turn, and whether `reps` is both sides' or one side's count. Copied when logged; Substrate Workouts counts per side on unilateral and alternating work.                                         |
| `rpe`                                                                                                                                                                                                      | decimal, 0 to 10                                                   | 8                  | The effort the set was logged with, on the scale `rpeScale` names.                                                                                                                                                                                       |
| `rpeMethod`                                                                                                                                                                                                | enum: entered, prefilled, copied, imported                         | 8                  | How `rpe` got its value, written whenever `rpe` is ([Effort provenance](#effort-provenance)).                                                                                                                                                            |
| `rir`, `rirMethod`                                                                                                                                                                                         | decimal at least 0, enum                                           | 8                  | Reps in reserve as a source logged them, never derived from `rpe`, and how it got its value.                                                                                                                                                             |
| `rpeScale`                                                                                                                                                                                                 | enum: rirbased, hevy, strong, cr10                                 | 8, 39              | The scale of `rpe` and of the target's effort. Substrate Workouts writes `rirbased` on every set; absent reads as `rirbased`.                                                                                                                                       |
| `reachedFailure`, `failedRepAttempted`                                                                                                                                                                     | bool                                                               | 6                  | The outcome, kept apart from RPE 10.                                                                                                                                                                                                                     |
| `completionMethod`                                                                                                                                                                                         | enum: individual, bulk, imported                                   | 33                 | Ticked on its own, marked done with the other unticked rows at Finish, or imported.                                                                                                                                                                      |
| `notes`                                                                                                                                                                                                    | text                                                               | 02 §7 item 20      | A note on this set alone: partials, a machine setting.                                                                                                                                                                                                   |
| `sourceType`                                                                                                                                                                                               | string                                                             | 39 | An imported set's own type as its source wrote it (Hevy's `dropset`, Strong's `D`).                                                                                                                                                                      |
| `timeBasis`                                                                                                                                                                                                | enum: completion, session, estimated                               | 9                  | What `at` is: the tick, a time of the session's standing in for one nobody recorded (an import's start, a bulk row's end), or an estimate.                                                                                                               |
| `startedAt`                                                                                                                                                                                                | datetime                                                           | 9                  | When the set began, where known (a timed set's play).                                                                                                                                                                                                    |
| `restBefore`                                                                                                                                                                                               | duration                                                           | 9                  | The rest actually taken before this set.                                                                                                                                                                                                                 |
| `restMethod`                                                                                                                                                                                               | enum: start, interaction, imported, estimated                      | 9                  | How `restBefore` was measured: to this set's recorded start (a timed set's play), to the end of the rest on the device (Skip or the first touch), as a source recorded it, or estimated. Substrate Workouts writes the first two. Written whenever `restBefore` is. |
| `restPlanned`                                                                                                                                                                                              | duration                                                           | 9, 10              | The rest the timer counted down before it.                                                                                                                                                                                                               |
| `restSkipped`                                                                                                                                                                                              | bool                                                               | 9                  | Skip was pressed.                                                                                                                                                                                                                                        |
| `target`                                                                                                                                                                                                   | object                                                             | 10, 6              | The planned set this fulfils and its targets as they stood. Absent for a set the plan did not have.                                                                                                                                                      |
| `target.plannedSet`                                                                                                                                                                                        | string                                                             | 10                 | The planned set's `key` in the routine slot that the session slot's `routineSlot` names.                                                                                                                                                                 |
| `target.termination`                                                                                                                                                                                       | enum                                                               | 6                  | What was to end the set.                                                                                                                                                                                                                                 |
| `target.repsMin`, `target.repsMax`, `target.assistance`, `target.rpeMin`, `target.rpeMax`, `target.rirMin`, `target.rirMax`, `target.durationMin`, `target.durationMax`, `target.distance`, `target.tempo` | as on the planned set                                              | 10                 | Copied when the set is done, so history survives a routine edit and the suggestion knows the aim.                                                                                                                                              |
| `target.load`, `target.loadPercent`, `target.loadReference`, `target.referenceLoad`                                                                                                                        | quantity, decimal, enum, quantity                                  | 10, 16             | `load` is always the resolved absolute load; a percentage plan adds the percentage, what it was of, and that reference load as it stood.                                                                                                                 |
| `target.timerMode`, `target.myoStopRule`, `target.intraSetPause`                                                                                                                                           | enum, enum, duration                                               | 10       | As the engine ran them: the planned set's, else the default it applied ([Set](#set) notes).                                                                                                                                                              |
| `target.miniSetReps`, `target.miniSets`, `target.totalReps`, `target.drops`, `target.dropPercent`, `target.dropStep`, `target.dropReps`                                                                    | as on the planned set                                              | 10       | The structure's parameters as they stood, which the suggestion reads for drops and mini-sets. A myo-rep match's total is copied resolved (the matched set's reps).                                                                                       |
| `origin`                                                                                                                                                                                                   | reference, any kind                                                | 17, 39             | The record an importer built it from.                                                                                                                                                                                                                    |
| `revision`                                                                                                                                                                                                 | int                                                                | 17, 18             | Raised with every change sent.                                                                                                                                                                                                                           |

Notes:

- **Why `temporal(point)`.** Every set has an end (the tick); few have a
  known start (only a timed set's play button gives one). A range would
  have to invent a start, and a range kind's `at` is its start on every
  timeline read. `startedAt` holds the start when it is known.
- **An imported set with no times** (Hevy and Strong record none) sits `at`
  its session's start, with `timeBasis: session`, and leaves `startedAt` and
  the rest fields empty. Left with no `at`, it would sort after every timed
  set (a list sorts nullable keys last, `internal/engine/query.go`) and
  break "last time". Sets that share an `at` are ordered by `order`. This is
  the one place the package bends req 9's "leave these fields empty", and
  `timeBasis` says so: a reader or an export that needs a measured instant
  (a Health Connect segment, a FIT `set` time) takes only `completion`. A
  row marked done at Finish sits at the session's end, also `session`.
- **Chains point at their head**, not at the record just before. Deleting a
  middle drop then orphans nothing, and a chain is the head plus the
  records naming it, ordered by `chainPosition`. The head owns its chain
  (`continues` cascades): deleting working set 2 takes 2a and 2b with it,
  so no drop is left pointing at nothing with a position from 2. Seen on the
  test server: purging a rest-pause chain's head took its two mini-sets.
- **The title is `{exercise}`**, the exercise's own title, which is its
  name. A bare reference renders the id when the exercise is missing, where
  `{exercise.name}` would render nothing, and it ties the set to no property
  of the exercise, which a rename would otherwise have to rewrite first
  (`docs/vocabulary.md`). A title is written with the set and never
  refreshed, so it shows the name as it stood then.
- **What the numbers meant is on the set.** `loadBasis`, `implements`,
  `bodyShare`, `laterality` and `repsBasis` are copied from the exercise
  when the set is logged, resolved: an exercise setting left out is written
  as its default (`total`, 1, 1, `bilateral`). A later edit of the exercise
  then changes how future sets are read and never a stored one; a deliberate
  correction of history is a PATCH of the sets. Substrate Workouts counts reps per side
  on unilateral and alternating work (`perside`); an importer keeps its
  source's count (21 lunges in all is `reps: 21, repsBasis: total`) and
  never guesses which side did the extra rep.
- **The effective prescription.** `target` copies every field the planned
  set names, and four are written as the engine ran them where the plan
  leaves them out: `timerMode` on a timed target (`countdown`),
  `myoStopRule` on a myo-rep head (the exercise's, else `repdrop`),
  `intraSetPause` on a structured head (15 s for myo-reps and myo-rep
  match, 20 s for rest-pause and clusters, `PT0S` for drops) and a myo-rep
  match's `totalReps` (the matched set's reps). `dropReps` absent means each
  drop went to failure. A chain's later records carry `plannedSet` and their
  own targets only: a drop's or a mini-set's reps, the second side's reps or
  time (with its `timerMode`).
- **`target.load` is always absolute.** A planned set given as a percentage
  writes the load it resolved to, with `loadPercent`, `loadReference` and
  `referenceLoad` beside it, so a reader never has to know the reference to
  read the load. When the reference is not known, `load` and
  `referenceLoad` are absent, and `loadPercent` with `loadReference` alone
  say what was planned: today that is every reference but `bodymass`, since
  Substrate Workouts keeps no one-rep max or training max yet. A target with
  `loadPercent` and no `load` is a percentage left unresolved, never a set
  planned with no load.
- **How it was completed.** `completionMethod` is `individual` for a row
  ticked on its own, `bulk` for a row Finish's "Mark them done as shown"
  committed (`at` the session's end, `timeBasis: session`, no rest fields),
  and `imported`. A reader that wants only confirmed values filters on
  `individual`.
- **What is top level.** Everything a list filters or the aggregate read
  reduces: the references, `role`, `structure`, `load`, `reps`, `duration`,
  `rpe`, `rpeMethod`, `completionMethod`, `restBefore`, `setup`,
  `reachedFailure`. The aggregate read takes
  only a kind's own non-repeated properties. The plan's targets
  are read only with the set, so they are one object.
- **The 10,000-record ceiling**. At about 5,000 sets a year the set
  kind passes the server's in-place conversion ceiling in about two years.
  Until then a rename, an enum remap or a new required property converts in
  place; after it, through expand-and-contract. Adding an optional property
  or an enum value is additive at any size. That is why every property
  here is optional except the five that locate a set (`session`,
  `exercise`, `slot`, `order`, and the trait's `at` in practice). The
  review's fields are top-level for the same reason: each can still be
  renamed by declaration while the kind is small.

### Effort provenance

The Rows screen shows the plan's RPE pre-filled on the set being logged,
and one tap logs the set with it. `rpeMethod` keeps that apart from a
rating, and `rirMethod` does the same for `rir`. The rules:

- **Written whenever the value is.** A set with `rpe` has `rpeMethod`; a set
  with neither was not rated. The server does not tie the two (a
  declaration cannot), so the rule is the writer's: seen on a test
  server, a set with `rpeMethod` and no `rpe` is admitted. A reader ignores
  a method with no value.
- **`entered`**: the lifter chose the value, by tapping it, even when it is
  the value already shown. Equality with the plan never decides the method.
- **`prefilled`**: the plan's value was shown and the set logged with it
  untouched. Such a record has `rpe` equal to the top of its target's
  effort range by construction: `target.rpeMax`, else 10 − `target.rirMin`.
- **`copied`**: PREVIOUS was tapped to copy last time's values into the
  row. A later tap on the RPE makes it `entered`.
- **`imported`**: from another app, where how it was entered is unknown.
  An importer never claims `entered`.
- **Clearing the rating** removes `rpe` and `rpeMethod` together (a PATCH
  naming both `null`).
- **Absent `rpeMethod` with `rpe` present** is a set written before the
  field existed; the first build's local history was written only when tapped, so its
  migration may stamp `entered`. A method value a reader does not know (a
  later version's) is no rating.

What a reader does with it:

- **Only `entered` and `imported` are ratings.** A `prefilled` or `copied`
  RPE is no rating: the owner did not report it for this set.
- **The suggestion** treats a `prefilled` or `copied` RPE as absent (03
  §10.2): a set that met its target is taken at the target's top
  (`rpeMax`, else 10 − `rirMin`), one that fell short as having reached
  failure, and the explanation says "assuming RPE 8", never "at RPE 8".
- **PRs, e1RM and charts** take `entered` and `imported` values as
  reported effort, never a `prefilled` or `copied` one, and only on an
  RIR-based scale (`rirbased`, `hevy`, `strong`, or none written): a
  `cr10` RPE is not reps in reserve.
- **Chain folding** keeps provenance per record: a chain's RPE is the
  highest rated (`entered` or `imported`) RPE among its records; with none,
  the folded set is unrated.
- **Exports** (Hevy `rpe`, Strong `RPE`, a FHIR 82289-0 component) carry
  rated values only.
- **Filters**: "every set the lifter rated" is `rpeMethod eq entered`;
  "rated, pre-filled or copied" is `rpe exists`.

## Nesting

Substrate caps object nesting at four levels: a kind's own property is level
1, and a level-4 field holds a scalar (`MaxFieldDepth = 4`,
`internal/vocabulary/types.go`). A quantity and a reference are scalars for
this purpose: both are admitted inside an object at any declared depth
(`docs/data-model.md`, "References"; the write path walks nested quantities
in `internal/engine/quantitywrite.go`).

| Level | routine                                                                                       | session                                                             | set                                                                                                       |
| ----- | --------------------------------------------------------------------------------------------- | ------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| 1     | `groups`, `slots`                                                                             | `slots`, `groups`, `pauses`                                         | `target`                                                                                                  |
| 2     | group fields; slot fields, and `sets`                                                         | slot fields (rests included) and `sets`; group fields; pause fields | target fields, all scalars (`load`, `referenceLoad`, `assistance`, `distance`, `dropStep` are quantities) |
| 3     | planned-set fields, all scalars (`load`, `assistance`, `distance`, `dropStep` are quantities) | the snapshot's planned-set fields, the same scalars                 |                                                                                                           |
| 4     | unused                                                                                        | unused                                                              |                                                                                                           |

02 §7 item 1 draws the plan as a tree of five levels (routine, groups,
slots, planned sets, segments). Flattened: groups and slots are siblings at
level 1 and a slot names its group by key; segments are not written out.
That leaves level 4 free for one more object inside a planned set, should a
target ever need one. The plan snapshot on the session's slots
(`slots[].sets`, level 2, its fields at level 3) uses the same depth as the
routine. Checked against the loader's closed key sets (`propKeys`,
`objectPropKeys`, `referencePropKeys`, `typeDataKeys` in
`internal/vocabulary/load.go`) and its depth rule: every declaration here
uses only those keys, and the deepest field is at level 3.

The checker refuses the level past the cap: adding an object with fields
inside a planned-set field fails with `fields nest 4 levels deep at most (a
kind's own property is level 1) — flatten this one`, and a quantity and a
reference at level 4 pass (both run on scratch copies).

What objects cost, and why they are where they are:

- An object's fields are outside the filter grammar and the aggregate read.
  Nothing here filters on a slot, a group, a pause or a target.
- A field cannot take `renamedFrom`, and a new required field is not
  backfilled by declaration. Every field is optional except the
  identifying few: keys (a group's, a slot's, a planned set's), a slot's
  exercise, a group's kind, a pause's start, and a code's system and code.
- **`mustExist` does reach references inside objects.** The engine walks an
  object's declared fields and checks each reference with its own
  declaration (`normalizeReferenceValue`, `internal/engine/references.go`),
  so `slots[].exercise` is checked on the routine and the session. Seen on
  a test server: `props.slots[0].exercise: reference names
  ~/training/exercise/chest-press-machin, which does not
  exist`.
- **`referencing` does reach references inside objects**, so "which routines use the
  chest press" and "which sessions included it" are index-backed reverse
  reads on `slots[].exercise`.
- **`expand` does not.** It follows a kind's own top-level references only
  (`expandProperties`, `internal/engine/expand.go`), so a routine preview
  reads the exercises separately ([Reading](#reading)).
- `onDelete: cascade` is refused on a reference inside an object, which is
  why the two cascades, `set.session` and `set.continues`, are top-level.

## Indices

None is declared.

- **Req 3 names `[exercise, at]` and `[session, at]`**, and the first draft
  declared those two and `[routine, at]` on the session, as log kinds often do. None of the three
  serves its read:
  - A list filtered by a single reference is answered from the `refs`
    table, through `refs_dst_idx`, then sorted by `at`. Row level security
    keeps the planner from probing an index through `props->`, which is
    not leakproof. Substrate's own comments say so: `condReference` in
    `internal/engine/query.go`, and `refindexrls_db_test.go`.
  - Checked with `EXPLAIN` on a test server, as the
    repository's own database role. The engine's SQL for the chest press
    history ran as `refs_dst_idx`, then `records_pkey`, then a sort. The
    same filter written on the path expression read every set of the
    repository and filtered them.
- **So they were dropped.** Each was maintained on every set and session
  written and read by nothing, and a package declares as little as
  possible. The reads are still cheap: a few hundred sets per exercise a
  year, sorted after one index probe. Declaring one later builds it and
  touches no record, should a substrate release let the planner use it.
- **Every single reference still gets an index** on its path without a
  declaration (`referenceIndexStatements`, `internal/engine/indices.go`),
  which serves the order by a reference and the reads that cannot use
  `refs`. On a test server the apply built exactly those, twelve: one
  per scalar reference (`session`, `exercise`, `continues`, `setup`,
  `origin` on the set; `routine`, `workout`, `source`, `about`, `origin` on
  the session; `variationOf` on the exercise; `exercise` on the setup), and
  nothing else.
- Nothing indexes a quantity: the server refuses one, because its order
  converts through the unit graph (`internal/engine/indices.go`). The
  offline checker does not catch this; the server does (seen on the first
  test server).
- A list filter compares a `duration` as text (`PT10M` sorts before
  `PT9M`), so no read filters on `restBefore` or `duration`; the aggregate
  read reduces them exactly. Seen on a test server: `restBefore
  gte PT2M` admits `PT6S`, and a descending order puts `PT9S` above
  `PT6M30S`.

## Ids and record paths

A record is identified by `(kind, id)`, and its path is `<kind>/<id>`
(`docs/data-model.md`, "Records and identity"). The id does not repeat the
kind.

| Kind     | Id                                     | Path                                                                   |
| -------- | -------------------------------------- | ---------------------------------------------------------------------- |
| exercise | `chest-press-machine`                  | `~/training/exercise/chest-press-machine`                 |
| setup    | `hotel-gym-chest-press`                | `~/training/setup/hotel-gym-chest-press`                  |
| routine  | `fully-body-collection-1`              | `~/training/routine/fully-body-collection-1`              |
| session  | `01a18d0c-5c80-7d3e-9a51-3c8e2b7d4f10` | `~/training/session/01a18d0c-5c80-7d3e-9a51-3c8e2b7d4f10` |
| set      | `01a18d18-8d98-7c2b-9d3e-0f1a2b3c4d5e` | `~/training/set/01a18d18-8d98-7c2b-9d3e-0f1a2b3c4d5e`     |

- Writer-chosen ids allow RFC 3986 unreserved characters plus `:`, `@` and
  `/`, up to 128 characters. A UUIDv7 or a slug needs no escaping. The
  `workout` reference's id (`applehealth/<account>/<UUID>`) holds `/`, which
  a REST path percent-encodes.
- None of these kinds may become a `recordmapping` target, whose ids are
  server-assigned (decision record 0049).
- An importer composes ids from the upstream key, such as
  `hevy-<workout id>` for a session and `hevy-<workout id>-<exercise
  index>-<set index>` for a set, so a re-import rewrites in place
  ([Importing](#importing)).

## How the app writes

Substrate Workouts writes through an outbox on the phone (WorkoutsKit's
`Outbox`), in this order:

| Step                                         | Write                                                                                                                                                                                                                                                                                                                                                            |
| -------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A new exercise or setup                      | `PUT` it at a slug before anything names it: a setup, a routine slot, a session slot and a set refuse an exercise that does not exist, and a set refuses a setup that does not exist.                                                                                                                                                                            |
| Start                                        | `PUT` the session at its UUIDv7, without `status`, with `at`, `utcOffsetStart`, `timezone`, `name`, `sport: strength`, `routine`, `routineVersion`, `deload` and `activityType` (the routine's), `slots`, `groups`, `revision: 1`.                                                                                                                               |
| Complete a set                               | `PUT` the set at its UUIDv7, `revision: 1`, with `completionMethod`, `timeBasis`, `rpeScale`, the copied `loadBasis`, `implements`, `bodyShare`, `laterality` and `repsBasis`, `rpeMethod` beside any `rpe`, `restMethod` beside any `restBefore`, and the full `target`. The session's create is earlier in the outbox (`mustExist`), and so is a chain's head. |
| Edit a set                                   | `PATCH` the set with `ifVersion`, raising `revision`; `null` clears a property.                                                                                                                                                                                                                                                                                  |
| Reorder, add, replace, group, change a rest  | `PATCH` the session's `slots` and `groups` whole, with `ifVersion`, raising `revision`.                                                                                                                                                                                                                                                                          |
| Finish                                       | `PATCH` `status: finished`, `endsAt`, `utcOffsetEnd`, `sessionRPE`, and `slots` with each slot's planned-set snapshot (`slots[].sets`), with `ifVersion`, raising `revision`. Rows marked done in bulk are `PUT` first, `completionMethod: bulk`.                                                                                                                |
| After the Health save, once the row is found | `PATCH` `healthKitWorkout` (at once) and `workout` (when found), with `ifVersion`, raising `revision` ([Apple Health](#apple-health)).                                                                                                                                                                                                                           |
| Discard                                      | Drop every queued write for the session and its sets from the outbox, then `DELETE` the session with `?ifVersion=<version>&purge=true`.                                                                                                                                                                                                                          |

- Never `status` in a PUT: a create replayed after Finish would try to move
  the state back, and the server refuses a PUT that moves a state.
- A `409` on a session patch means re-read, merge, retry. A `422` naming a
  revision below the stored one means a newer write already landed: drop
  the entry.
- The revision rule guards live records only, and an equal revision is
  admitted. Discard is therefore ordered: the outbox drops the session's
  and its sets' queued writes first, because after the purge a replayed
  Start PUT would create the session again and a replayed set PUT would
  then restore its set. A replay at the same revision would also overwrite
  an edit made in the console at that revision; the phone raises
  `revision` on every write so that two writes never share one.
- A PATCH that names no `revision` keeps the stored one (the merged row is
  checked), so the rule never refuses a write that leaves it out; the phone
  raises it anyway, because the Health workout's sync version reads it.

### More than one writer

The app is the only writer of its sessions. Before a second device, the
console's agent or an importer edits what the app wrote, these rules hold:

- **`ifVersion` on every edit** of a stored record: a set, a session, a
  routine, an exercise, a setup. The record's `status.version` is the
  concurrency token; a `409` means re-read, merge, retry. Seen on the
  test server: a set PATCH with a stale `ifVersion` got `409`
  (`version conflict: ifVersion 4, stored 1`).
- **`revision` is per record**, raised by that record's writer on every
  write. It is the delivery order (`revised` refuses a lower one), not a
  conflict guard: two devices can both write revision 7, and an equal
  revision is admitted.
- **One owning writer per session** until a shared-write protocol exists:
  the device that started the session writes it and its sets, and another
  device writes only after an explicit takeover the first one honours. A
  second writer brings per-set `source` and `adapter` with it
  ([Left out](#left-out-and-why)).
- **The agent proposes, it does not edit live.** A console or agent change
  to a routine a session is running, or a change proposed from a session
  back to its routine, is a proposal against a base
  version through substrate's own surface (`core/propose`, which lands a
  `substrate.reamde.dev/core/recordpatchrequest` the owner accepts), never
  a second approval lifecycle inside these kinds. Nothing ever rewrites a
  performed set's `target`.

## Apple Health

At Finish the app saves one workout through `HKWorkoutBuilder`:

| HealthKit                     | Value                                                                                                                                         |
| ----------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| activity type                 | `session.activityType`, else `traditionalStrengthTraining` (the one place `activityType` is read)                                             |
| location                      | indoor                                                                                                                                        |
| start, end                    | `session.at`, `session.endsAt`; pauses as pause and resume events                                                                             |
| `HKMetadataKeyExternalUUID`   | the session id                                                                                                                                |
| `HKMetadataKeySyncIdentifier` | the session id                                                                                                                                |
| `HKMetadataKeySyncVersion`    | `session.revision` at the save (the Finish patch's)                                                                                           |
| `HKMetadataKeyTimeZone`       | `session.timezone`                                                                                                                            |
| effort score                  | `sessionRPE`, when the owner gave one from 1 to 10; a 0 is never sent, because Apple's effort score runs from 1 |
| energy, per-exercise detail   | none                                                                                                                                          |

The returned workout's UUID is written to `healthKitWorkout` at once.
`workout` points at the row the Apple Health adapter builds,
`substrate.reamde.dev/health/workout/applehealth/<account>/<UUID>`, and
the app cannot compute `<account>` on its own:

- **The account is Substrate Sync's.** Substrate Sync names its Apple
  Health account `substrate-sync-` and the first eight characters of its
  install id, a random UUID it keeps in its own Keychain
  (`apps/apple/SubstrateSync/Sources/Sync/LocalState.swift`), and it keys
  each workout row `applehealth/<account>/<id>`
  (`kinds/providers.substrate.reamde.dev/applehealth/README.md`, "What
  lands where"). The app has no App Group to read that Keychain.
  `iphone` in substrate's `docs/health-apps.md` is that document's own
  example account, not the shape a Substrate Sync install uses.
- **So the app finds the row on the server.** After the save it lists
  `providers.substrate.reamde.dev/applehealth/account` (a read its grant
  must name) and GETs `health/workout/applehealth/<account>/<UUID>` for
  each account; it writes `workout` only once one answers. Until then the
  session holds `healthKitWorkout` alone and the outbox retries the
  lookup. A pointer written on a guess would dangle by design, which
  nothing would ever flag, and the reverse read ("which session is this
  Health workout", a `referencing` read on the row) would find nothing.
- **The app writes `healthKitWorkout` today** and leaves `workout` empty:
  the lookup above is not built yet.

No re-save after an edit.

## Reading

The reads, as list filters (`GET /api/v1/records`, `substratectl get
--filter`):

- **PREVIOUS and the suggestion's history**: `filter: {kinds:
  [~/training/set], properties: {exercise: {eq: <path>},
  setup: <eq the setup's path, or exists false>}}`, `orderBy:
  at:desc,order:desc`
  (imported sets share their session's `at`), a page of about 100,
  `expand: session`. Group by session; within a session, compare set with
  set by (slot, role, ordinal), folding each chain into its head first (see
  below). Two slots of one exercise in one session are compared slot by
  slot.
- **One session**: `GET` the session, then the sets with `session eq <path>`.
  Sort by the session's slot order, then `order`. Never bound `at` on both
  ends for this read: that turns the list into the window read, which
  orders by `at` only and refuses any other order with a 400.
- **A routine preview, or an active session's empty slots**: `GET` the
  routine, then list the exercises by the ids in `slots[].exercise` (or
  read them from the app's cached catalogue). `expand` follows only a
  kind's top-level references, so it cannot fetch them in the same read.
- **The routine list**: routines by `folder`, then `position`. Folders are
  plain strings with no order of their own, so the list sorts folders by
  name; a `folderPosition` is additive should the owner want another order.
- **A session in progress anywhere**: `status eq active`.
- **When a routine was last done**: sessions with `routine eq <path>`,
  `orderBy: at:desc`, `first: 1`.
- **Trends**: the aggregate read's property mode on `load` (or `reps`),
  `groupBy: [exercise]`, weekly buckets, `role eq working` and
  `continues exists false`. The aggregate read's `count` counts
  records, so without the second filter a myo-rep chain counts each
  mini-set and a two-sided set counts twice. Seen on a test server: in
  the week of 26 October the lateral raise's working records number 7 and
  its chain heads 2.

What a reader derives, never stored:

- **Chains fold into one set.** A record with `chainPosition` above 1 is a
  segment of its head's set (`continues`): it is never a working set of its
  own, never working-set evidence, and gets no ordinal of its
  own. The set's RPE is the highest rated RPE (`rpeMethod` `entered` or
  `imported`) on any of its records; a `prefilled` one counts as none, so
  a chain with no rated record is unrated ([Effort provenance](#effort-provenance)).
  - **Side chains** (`side` on the records, `structure` straight): one set
    with `repsLeft` and `repsRight`, reps the lower of the two, RPE the higher rated of the two, `at` the later
    side's.
  - **Drops**: segments after the head, each with its own load.
  - **Myo-rep, myo-rep match, rest-pause and cluster** records past the
    head: mini-sets (03's `myoMini`); the head is the activation set or
    the first bout or cluster.
- **The set label**: `W` for a warm-up; the `ordinal` for a
  working set; for a later record of a drop, rest-pause or cluster chain,
  the ordinal and a letter (`2a`, `2b`); `M` for a myo mini-set; the
  ordinal and the side for a side record (`1 left`, `1 right`).
- **03's set kind**: `warmup` from `role`; `drop`, `myoMini` as above for
  segments; `myoActivation` for the head of a myo or myo-match chain;
  `timed` from the exercise's measurement (`duration`, `weightduration`);
  `failure` from `reachedFailure`, a `target.termination` of `failure`, or
  one of `amrap` (03 has no AMRAP kind: it is a failure set with an open
  top, `repsMax` absent); else `normal`. A `target.termination` of
  `rirstop` keeps the load the plan or history gives and shows the
  predicted reps only as a hint: the set ends at the effort, not at a
  count.
- **RIR from RPE**: 10 − `rpe`, at read, and only on the RIR-based scales
  (`rpeScale` `rirbased`, `hevy`, `strong`, or absent). A `cr10` value is a
  general effort rating (Health Connect's and FIT's 0 to 10), not a count of
  reps left, and gives no RIR. Where a set has both `rir` and `rpe`, `rir`
  wins for anything that needs reps in reserve; the two are kept as logged.
- **The effort a suggestion reads** from a set: an `entered` or `imported`
  `rpe` on an RIR-based scale (`rirbased`, `hevy`, `strong`, or none
  written; never `cr10`, nor a scale or method the reader does not know);
  else, for a set that met its target, the target's `rpeMax` (or 10 −
  `rirMin`), and for one that fell short, 10. A `prefilled` or
  `copied` `rpe` is read as absent, and the reason text says "assuming RPE
  8".
- **Effective load**: only for the six load-bearing
  measurements. `weightreps`, `weightduration` and `shortdistanceweight`
  use `load`; `bodyweightreps`, `weightedbodyweightreps` and
  `bodyweightassistedreps` use the set's `bodyShare` (the exercise's for a
  set written before the field) times the session's `bodyMass`, plus `load`
  or less `assistance`. A `repsonly`,
  `duration` or `distanceduration` set has no effective load and adds
  nothing to tonnage, e1RM or load records; its sets carry no `loadMode`.
  The owner's Hevy screenshot agrees: after ten such sets and a 25 kg × 15
  warm-up, its Volume read 375 kg.
- **Volume load**: reps × effective load per record, warm-ups out by
  default. A `perimplement` load is multiplied by the set's
  `implements`, and a `perside` load by 2 unless the set is `unilateral`
  (so on a bilateral and an alternating set alike). Reps count as logged:
  `repsBasis` is not read, so an alternating set's per-side count is not
  doubled (an alternating curl of 10 a side with a pair of 10 kg dumbbells
  is 10 × 10 × 2 = 200 kg), and a side chain's two records each count
  their own. An imported alternating set whose `repsBasis` is `total` is
  counted as given, so it counts twice what Substrate Workouts's per-side count of the
  same work would; the app imports nothing. Comparisons (PREVIOUS, e1RM,
  records) use the load as logged and never multiply it. The set's own
  `loadBasis`, `implements`, `bodyShare` and `laterality` are read, so an
  edit to the exercise never re-reads past volume; a set written before
  those fields falls back to the exercise as it is now.
- **Volume, hard sets, e1RM, session tonnage and duration**: in clients
 .

## Exporting

Rules for a Hevy or Strong export, where Substrate Workouts's model says more than the
target can hold:

- **Title** (Hevy `title`, Strong `Workout Name`): the routine's `name` when
  the session has a routine that still resolves, else the session's `name`.
- **Strong `Workout #`**: the session's position among the exported
  sessions, ordered by `at`.
- **Strong's "Rest Timer" rows**: `restBefore`, the rest taken.
- **A side chain** exports as one set: the lower reps and the higher RPE.
- **A drop chain** exports as Hevy `dropset` rows after the head, and
  Strong `D` rows.
- **Myo-rep, rest-pause and cluster mini-sets** export as Hevy `normal`
  rows and Strong numbered rows. Neither target can say otherwise, so the
  target counts more sets than Substrate Workouts does.
- **RPE**: only a rated value (`rpeMethod` `entered` or `imported`); a
  `prefilled` one is left out. Hevy takes 6, 7, 7.5 and so on to 10 (05
  §2.5). A 6.5 rounds down to 6, and a value below 6 is left out. A `cr10`
  value is not exported as Hevy's or Strong's RPE.
- **Health Connect and FIT set numbers**: Health Connect's `setIndex`
  numbers sets, not records, so an export numbers the slot's chain heads in
  `order` and writes the head's number on every record of the chain. FIT's
  `message_index` numbers `set` messages across the whole file, so an export
  renumbers. A set with `timeBasis` other than `completion` has no measured
  instant and becomes no Health Connect segment.
- **Pounds**: Hevy holds kilograms only (`weight_kg`), so a load in lb is
  converted, rounded to two decimals, and loses its unit. Strong keeps the
  unit (`Weight Unit`).
- **No load** (`repsonly`, `duration`, `distanceduration`): no weight
  column.

## Importing

Imports come later. What the package expects of a Hevy importer:

- **Ids from upstream keys**: `hevy-<workout id>` for the session and
  `hevy-<workout id>-<exercise index>-<set index>` for each set, indices
  from 0 as Hevy numbers them. The indices are positions, not identities:
  Hevy has no stable set id. A PUT merges and never prunes, so a set
  deleted or reordered in Hevy after an earlier import would leave a stale
  row behind: a re-import of a workout purges its session first (`DELETE
  ?purge=true`, which tombstones its sets) and then writes it again. A
  tombstoned set is restored with its old properties by a PUT at its id
  before the collector's sweep, so the importer writes every property it
  means to keep and clears the rest; reconciling the set inventory instead
  of purging is reserved ([Left out](#left-out-and-why)).
- **Times**: Hevy gives UTC instants and no offset. The importer assumes
  one zone, named in its configuration (the owner's, Europe/London), and
  writes `timezone`, the two offsets and the local `name` from it, and
  `sport: strength`.
- **Sets**: at the session's start (`timeBasis: session`), no rest fields
  (Hevy records none), `completionMethod: imported`, `rpe` with `rpeScale:
  hevy` and `rpeMethod: imported`, `sourceType` set to Hevy's own type, and
  the load's meaning copied from the exercise as Substrate Workouts copies it, except
  that the rep count is the source's (`repsBasis: total` where Hevy counted
  both sides together).
- **Setups**: where the owner tells the importer which gym, it writes a
  setup per exercise and gym (`old-gym-chest-press`) before the sets that
  name it.
  `warmup` becomes `role: warmup`; `normal` a working straight set;
  `failure` a working set with `reachedFailure`; consecutive `dropset` rows
  the drops of the set before them, which becomes a drop chain's head
 .
- **Load mode**: from the template's type; `reps_only` and `duration` sets
  carry no `loadMode`.
- **Exercises**: resolved by `(hevy, <template id>)`; an unknown one is a
  mapping the owner confirms before any of its sets is written
  ([Exercise](#exercise)).
- **Dropped**: Hevy's `custom_metric` and a routine's `rest_seconds` (rests
  are on routines, which the importer does not write).
- **Routines**: Hevy's `routine_id` names no routine here, because the
  owner's routines are written by the console's agent under slugs; an
  imported session carries no `routine`. A repeated `codes` on the routine
  is additive, should that link be wanted.
- **Health**: Hevy may have saved its own HKWorkout for the same session,
  which Substrate Sync then delivers as a `health/workout` row no session
  points at. Linking or hiding it is open question 25.

## Left out, and why

| Left out                                                                | Why                                                                                                                                                             |
| ----------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A kind per exercise performance (05 Shape C)                            | The session's slots hold order, group, notes, rests and the plan snapshot; sets hold the rest.                                                                  |
| Records for planned sets nobody did                                     | Open question 32. The session's plan snapshot (`slots[].sets`) lists every planned set; only performed sets are records.                                        |
| An `abandoned` state                                                    | No path writes one, and a state with no way out could never be finished. Open question 32; additive.                                                   |
| Derived values (volume, e1RM, tonnage, session duration)                | Open question 31; the house style keeps derived values off records, and nothing on the server reads them yet.                                          |
| `utcOffset` on a set                                                    | A per-set read uses a named zone or the session's offsets.                                                                                            |
| `about`, `source`, `adapter` on a set                                   | A set's subject and writer are its session's while one device writes. Per-set `source` and `adapter` come with a second writer (below).                         |
| A gym on the session                                                    | A set's `setup` names the station, and the setup its `gym`.                                                                                                     |
| Per-setup settings beyond the load grid                                 | A setup overrides the grid only; rest class, rep targets and the rest stay on the exercise until a gym needs otherwise. Two setups of one machine are merged.   |
| A record of a reference load (1RM, training max)                        | `loadPercent` needs one to resolve; when it is wanted it is a derived observation (below), and `target.referenceLoad` keeps the number each set used.           |
| Exercise notes, images                                                  | No requirement; open question 33 for images.                                                                                                                    |
| `revision` on the routine, the exercise and the setup                   | None is rewritten from a versioned upstream; the record version serves.                                                                                         |
| A flag for "bodyweight counts towards volume"           | `bodyShare` covers it: bodyweight counts at its share, and only for the bodyweight measurements. Hevy's convention (push-ups count 0) is a flag away; additive. |
| `blockRest`                                                             | Its value is the finished slot's set rest or round rest; a declared override is an additive field.                                                    |
| An upper bound on a rest, each ±15 s tap, which rest slot a rest filled | Not required; the slot is derived from the sets around it.                                                                                                      |
| A myo-match reference to the matched set                                | The previous working set's reps say what is matched, and `target.totalReps` holds the resolved total.                                                           |
| A unit preference record                                                | Open question 4: the app's setting.                                                                                                                             |
| A `finished` to `active` transition                                     | Not needed to edit a finished session; additive.                                                                                                                |
| `codes` on the routine, `folderPosition`                                | Additive when an importer or a second folder needs them.                                                                                                        |
| `measurement` copied onto the set                                       | The set's `loadMode` and which of `reps`, `duration` and `distance` it holds already say how it reads; additive if a history view needs more.                   |
| Hevy template ids in the examples                                       | Read from the owner's Hevy account, which this draft could not reach ([Exercise](#exercise)).                                                                   |
| `requires` on the observations package                                  | Seeded; see `training.yaml`.                                                                                                                                  |
| A training sub-agent and grants                                | Not part of this package: an agent that writes routines is the owner's to declare, with `writes` naming the exercise, setup and routine kinds.                                                                          |

### Later: reserved, not built

The reviews named these; each is additive (a new optional property, a new
enum value or a new kind), so none is declared before something writes it.
The names are reserved so the first writer uses them:

- **Conditioning groups.** `groupKind` values `emom`, `amraptime` and
  `intervals` (Tabata is an `intervals` group), with group fields
  `interval` (one interval's length), `work` (the work inside it) and
  `timeCap` (an AMRAP's cap), on the routine's and the session's groups; on
  the session's groups, `roundsDone` and `repsDone` (the reps into the
  unfinished round). A set's round inside a circuit is its `ordinal`. FIT's
  `amrap`, `emom` and `tabata` sub-sports map onto them.
- **Cardio machines in a session.** On the set, as `health/workout`
  spells them: `activeEnergy` (quantity, kJ), `averageHeartRate` (quantity,
  1/min), and `level` (int) and `incline` (decimal, percent). Heart-rate
  streams stay the Health workout's traces.
- **Programs above routines.** A `program` kind (name, weeks, deload week,
  the progression scheme), `routine.program` (a reference) and
  `session.week` (int). The `folder` string stays display grouping and is
  never grown into a program.
- **Derived PRs and 1RMs.** Computed in clients for now.
  When stored, they are `observations/observation` rows of metrics such as
  `training/e1rm` and `training/training_max`, keyed by the algorithm and
  its version, with the sets in `derivedFrom`, and invalidated when a set
  they read changes. Never properties of these kinds.
- **Import source preservation.** `session.file` (a blobref, as
  `health/workout.file`) for the source file or payload; ids scoped by the
  source account as well as the provider; a re-import that reconciles the
  session's set inventory (update by id, clear omitted properties, delete
  what the source dropped) instead of purging it.
- **Fractional-second durations** for FIT imports, whose `duration` is in
  milliseconds; the app's duration codec rounds to whole seconds today.
- **Per-set `source` and `adapter`** when a second device writes sets, so a
  row says which device logged it.

## Field mapping

Mostly from 01 §9, which checked each entry. The rows 01 §9 does not have
come from 01 §4.1 (Hevy's exercise template), 01 §7 (exercise codes), 01
§8.3 and 02 §3.4 (pauses) and 02 §2.3 (failure). The data-model reviews of
2026-10-31 corrected the rows for Health Connect's `setIndex`, its
`AmrapTarget`, FIT's `message_index`, `wkt_step_index`, `file_id` and
pauses, HealthKit's brand name, Hevy's `updated_at`, Strong's `Workout #`,
the effort score's floor and the CR10 conversion. "alpha" marks a Health
Connect field from 1.2.0-alpha03 or later. "none" means the target cannot
hold it: an export drops it, an import leaves it empty. A mapping with a
condition holds only when the condition does. [Exporting](#exporting) says
what an export does where the shapes differ.

### Session

| Substrate Workouts                                          | HealthKit                                                                                                                                           | Health Connect                                                        | FIT                                                                                                                                                         | Hevy (API)                                                                                                    | Strong (CSV)                                                                                |
| ------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| id                                               | `HKMetadataKeyExternalUUID`, `HKMetadataKeySyncIdentifier`                                                                                          | `metadata.clientRecordId`                                             | none: `file_id` serial and time identify the file, which may hold several sessions, so an import keys on them and the session's index                       | `workout.id`                                                                                                  | none                                                                                        |
| the session's place among the exported ones      | none                                                                                                                                                | none                                                                  | none                                                                                                                                                        | none                                                                                                          | `Workout #`, a number an export assigns and a re-export renumbers: export only, never a key |
| `revision`                                       | `HKMetadataKeySyncVersion`                                                                                                                          | `metadata.clientRecordVersion`                                        | none                                                                                                                                                        | none: `updated_at` is a timestamp, so an importer keeps its own count and raises it when `updated_at` changes | none                                                                                        |
| `at`, `endsAt`, `utcOffsetStart`, `utcOffsetEnd` | `startDate`, `endDate`                                                                                                                              | `startTime`, `startZoneOffset`, `endTime`, `endZoneOffset`            | `session.start_time`, `total_elapsed_time`, `activity.local_timestamp`                                                                                      | `start_time`, `end_time`                                                                                      | `Date` (local), `Duration (sec)`                                                            |
| `timezone`                                       | `HKMetadataKeyTimeZone`                                                                                                                             | none                                                                  | none                                                                                                                                                        | none                                                                                                          | none                                                                                        |
| `pauses`                                         | pause and resume events                                                                                                                             | `EXERCISE_SEGMENT_TYPE_PAUSE` segments                                | timer stop and start `event` messages and their timestamps; `total_elapsed_time` minus `total_timer_time` gives only the pauses' total, not where they were | none                                                                                                          | none                                                                                        |
| `sport`                                          | the workout's activity type, as the Apple Health adapter maps it onto `health/workout.sport`                                                        | `exerciseType`, `EXERCISE_TYPE_STRENGTH_TRAINING` (70) for `strength` | `sport` training (10), `sub_sport` strength_training (20) for `strength`                                                                                    | implicit                                                                                                      | implicit                                                                                    |
| `activityType`                                   | the activity type saved, `traditionalStrengthTraining` (50) by default                                                                              | none: an export reads `sport`                                         | none: an export reads `sport`                                                                                                                               | none                                                                                                          | none                                                                                        |
| `name`                                           | none: `HKMetadataKeyWorkoutBrandName` is a brand, not a title; a custom metadata key may carry the name for readers that know it | `title`                                                               | none                                                                                                                                                        | `title`                                                                                                       | `Workout Name`                                                                              |
| `notes`                                          | none                                                                                                                                                | `notes`                                                               | none                                                                                                                                                        | `description`                                                                                                 | `Workout Notes`                                                                             |
| `sessionRPE`                                     | `workoutEffortScore`, related to the workout, 1 to 10: a 0 is not sent                                                                              | `rateOfPerceivedExertion` (alpha, CR10)                               | `session.workout_rpe` (CR10 × 10)                                                                                                                           | none                                                                                                          | none                                                                                        |
| `routine`                                        | `workoutPlan` (WorkoutKit plans only)                                                                                                               | `plannedExerciseSessionId`                                            | none                                                                                                                                                        | `routine_id`                                                                                                  | none                                                                                        |
| `slots` order                                    | none                                                                                                                                                | segment time order                                                    | message order and time                                                                                                                                      | `exercises[].index`                                                                                           | row order                                                                                   |
| `slots[].exercise`                               | none                                                                                                                                                | `segmentType`                                                         | `category`, `category_subtype`                                                                                                                              | `exercise_template_id`                                                                                        | `Exercise Name`                                                                             |
| `slots[].notes`                                  | none                                                                                                                                                | none                                                                  | none                                                                                                                                                        | `exercises[].notes`                                                                                           | none (Strong's notes are per set)                                                           |
| `slots[].group`                                  | none                                                                                                                                                | none (plan blocks only)                                               | none (plan repeat steps only)                                                                                                                               | `superset_id`                                                                                                 | none                                                                                        |
| `slots[].sets`                                   | none                                                                                                                                                | `PlannedExerciseSessionRecord` blocks and steps                       | `workout` and `workout_step` messages                                                                                                                       | the routine's `exercises[].sets`                                                                              | none                                                                                        |

### Set

| Substrate Workouts                                              | HealthKit                | Health Connect                                                                                                                                                                               | FIT                                                                                                                                                                                                                        | Hevy (API)                               | Strong (CSV)                                  |
| ---------------------------------------------------- | ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------- | --------------------------------------------- |
| `order`                                              | none                     | segment time order                                                                                                                                                                           | none: `message_index` numbers `set` messages across the whole file, so an export renumbers and an import takes `order` from message order within the exercise's run                                                        | `sets[].index`                           | `Set Order`                                   |
| a set, a chain counted once (`ordinal`, `continues`) | none                     | `setIndex` (alpha): numbers sets, not records, and several segments may share one; every record of a chain exports its head's number, and an import makes records sharing an index one chain | none                                                                                                                                                                                                                       | none                                     | none                                          |
| `role`, `structure`                                  | none                     | none                                                                                                                                                                                         | `set_type` active (a warm-up is lost)                                                                                                                                                                                      | `type`: warmup, normal, failure, dropset | `Set Order` `W`, `D`                          |
| `sourceType`                                         | none                     | none                                                                                                                                                                                         | none                                                                                                                                                                                                                       | `type`, as written                       | `Set Order`'s letter, as written              |
| `reps`                                               | none                     | `repetitions`                                                                                                                                                                                | `repetitions`                                                                                                                                                                                                              | `reps`                                   | `Reps`                                        |
| `load` with its unit                                 | none                     | `weight: Mass` (alpha)                                                                                                                                                                       | `weight` (kg, in 1/16) and `weight_display_unit`                                                                                                                                                                           | `weight_kg`                              | `Weight (kg)`, or `Weight` with `Weight Unit` |
| `loadMode`                                           | none                     | none                                                                                                                                                                                         | none                                                                                                                                                                                                                       | the template's `type`                    | none                                          |
| `loadBasis`, `implements`, `bodyShare`, `repsBasis`  | none                     | none                                                                                                                                                                                         | none                                                                                                                                                                                                                       | none                                     | none          |
| `laterality`, `side`                                 | none                     | left and right segment types (2 exercises)                                                                                                                                                   | in the exercise name                                                                                                                                                                                                       | none                                     | none                                          |
| `resistanceLabel`                                    | none                     | none                                                                                                                                                                                         | none                                                                                                                                                                                                                       | none                                     | none                                          |
| `duration`                                           | none                     | segment `endTime` minus `startTime`                                                                                                                                                          | `duration` (milliseconds; whole seconds here, see [Later](#later-reserved-not-built))                                                                                                                                      | `duration_seconds`                       | `Seconds`                                     |
| `distance`                                           | none                     | none                                                                                                                                                                                         | none                                                                                                                                                                                                                       | `distance_meters`                        | `Distance (meters)`                           |
| `rpe`, `rpeScale`                                    | none                     | segment `rateOfPerceivedExertion` (alpha, float, CR10): `rpeScale: cr10`                                                                                                                     | none                                                                                                                                                                                                                       | `rpe` (6 to 10): `rpeScale: hevy`        | `RPE`: `rpeScale: strong`                     |
| `rpeMethod`, `rirMethod`                             | none                     | none (`metadata.recordingMethod` is per record, not per value)                                                                                                                               | none                                                                                                                                                                                                                       | none: an import writes `imported`        | none: an import writes `imported`             |
| `rir`                                                | none                     | none                                                                                                                                                                                         | none                                                                                                                                                                                                                       | none                                     | none                                          |
| `completionMethod`                                   | none                     | none                                                                                                                                                                                         | none                                                                                                                                                                                                                       | none: an import writes `imported`        | none: an import writes `imported`             |
| `notes`                                              | none                     | none                                                                                                                                                                                         | none                                                                                                                                                                                                                       | none                                     | `Notes` (on set rows)                         |
| `startedAt`, `at`                                    | none                     | segment `startTime`, `endTime` (required): only from a set with `timeBasis: completion` and a known start                                                                                    | `start_time`, `timestamp`                                                                                                                                                                                                  | none                                     | none                                          |
| `timeBasis`                                          | none                     | none                                                                                                                                                                                         | none                                                                                                                                                                                                                       | none: an import writes `session`         | none: an import writes `session`              |
| `restBefore`                                         | none                     | a `REST` segment, only when both its ends are known instants                                                                                                                                 | a `set` with `set_type` rest and its `duration`                                                                                                                                                                            | none                                     | a "Rest Timer" row                            |
| `restPlanned`, and the routine's rests               | WorkoutKit recovery step | plan step with phase rest and a duration goal                                                                                                                                                | `workout_step` with intensity rest                                                                                                                                                                                         | routine `rest_seconds` (per exercise)    | none                                          |
| `target.repsMin`, `target.repsMax`                   | none                     | plan `RepetitionsGoal`                                                                                                                                                                       | `duration_type` reps and `duration_value`                                                                                                                                                                                  | routine `reps`, `rep_range`              | none                                          |
| `target.load`                                        | none                     | plan `WeightTarget`                                                                                                                                                                          | `exercise_weight`                                                                                                                                                                                                          | routine `weight_kg`                      | none                                          |
| `target.rpeMin`, `target.rpeMax`                     | none                     | plan `RateOfPerceivedExertionTarget` (int, CR10): one value, so an export writes `rpeMax` rounded, and an RIR-based value is not CR10                                                        | none                                                                                                                                                                                                                       | none                                     | none                                          |
| `target.termination` `amrap`                         | none                     | plan `AmrapTarget` (as many reps as possible, often with a duration goal)                                                                                                                    | none                                                                                                                                                                                                                       | none                                     | none                                          |
| `target.plannedSet`                                  | none                     | none                                                                                                                                                                                         | `set.wkt_step_index`, a number naming a `workout_step` of an exported workout: an export maps (routine, slot key, planned-set key) to a step index, since `s1` recurs in every slot; an import keeps the index in `origin` | none                                     | none                                          |
| `continues`, `chainPosition`                         | none                     | none                                                                                                                                                                                         | none                                                                                                                                                                                                                       | consecutive `dropset` rows, no link      | `D` rows, no link                             |
| `reachedFailure`                                     | none                     | none                                                                                                                                                                                         | none                                                                                                                                                                                                                       | `type: failure`                          | none                                          |
| `failedRepAttempted`                                 | none                     | none                                                                                                                                                                                         | none                                                                                                                                                                                                                       | none (Hevy logs the last completed rep)  | the app's Failure tag               |
| `target.tempo`, the planned set's `tempo`            | none                     | none                                                                                                                                                                                         | none                                                                                                                                                                                                                       | none                                     | none                                          |

### Exercise

| Substrate Workouts                              | HealthKit | Health Connect                            | FIT                            | Hevy (API)                                                       | Strong (CSV)                        |
| ------------------------------------ | --------- | ----------------------------------------- | ------------------------------ | ---------------------------------------------------------------- | ----------------------------------- |
| `codes`                              | none      | `system: healthconnect`, the segment type | `system: fit`, `category/name` | `system: hevy`, the template id                                  | `system: strong`, the exercise name |
| `measurement`                        | none      | none                                      | none                           | template `type` (`weight_reps` and the rest)                     | none                                |
| `equipment`                          | none      | in the segment type's name                | in the exercise name           | template `equipment`                                             | in the name's brackets              |
| `primaryMuscles`, `secondaryMuscles` | none      | none                                      | none                           | template `primary_muscle_group` (one), `secondary_muscle_groups` | none                                |

How the effort scales map on import: Hevy and Strong write `rpe` as given
with `rpeScale` `hevy` or `strong` and `rpeMethod: imported`; a Health
Connect or FIT effort is CR10 (`sessionRPE`, or `rpeScale: cr10` on a set);
wger's RIR goes to `rir` unchanged with `rirMethod: imported`. CR10 is a general effort scale: no reps-in-reserve count follows
from it, so 10 − `rpe` is never applied to a `cr10` value. A FHIR export of
set effort (a LOINC 82289-0 component) carries the scale beside the value,
because that code names perceived exertion, not a calibration.

### Code systems

`exercise.codes[].system` holds the short word below, or the URI where the
system publishes one. A FHIR export writes the export URI as
`Coding.system`. Only the PHR code system publishes a URI; the others are
URLs chosen here, each the system's own public home, until the publisher
names one.

| `system`                                                       | `code` holds                                                                                                      | Export URI                                                                                       |
| -------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| `http://hl7.org/fhir/uv/phr/CodeSystem/observation-pghd-codes` | the HL7 PHR IG's workout code                                | itself                                                                                           |
| `fit`                                                          | `category/name` from the FIT profile, such as `squat/barbell_back_squat`; `version` the profile, such as 21.218.0 | `https://developer.garmin.com/fit/`                                                              |
| `healthconnect`                                                | the `ExerciseSegment` constant, such as `EXERCISE_SEGMENT_TYPE_PLANK`                                             | `https://developer.android.com/reference/androidx/health/connect/client/records/ExerciseSegment` |
| `hevy`                                                         | the exercise template id; a custom template's id is the owner's account's own                                     | `https://api.hevyapp.com/docs/`                                                                  |
| `strong`                                                       | the exact export name, such as `Squat (Barbell)`; a custom name is the owner's own                                | `https://www.strong.app/`                                                                        |
| `wger`                                                         | the exercise's uuid                                                                                               | `https://wger.de/`                                                                               |
| `free-exercise-db`                                             | its id, such as `Barbell_Squat`                                                                                   | `https://github.com/yuhonas/free-exercise-db`                                                    |

A code can be broader than an exercise (a Health Connect segment type covers
several machines), so two exercises sharing one are never merged for it, and
an importer resolving by `(system, code)` confirms a broad match with the
owner.

## Examples

Built from the owner's Hevy screenshots, in their own numbers. The
screenshots show the folder "Full Body 3x Week (Back-Friendly)" holding
"Fully Body Collection #1", "Day 1 – Strength Foundation", "Day 2 – Volume
& Stability" and a fourth routine whose name is cut off. They show one
session's exercises with their rests and last-time values, but not which of
those routines it was: the example files them under the first. The date is
illustrative (the screenshots are undated); the times follow the
screenshots' clocks, with the session starting between 13:42:34 and
13:42:48 ("11s" at 13:42, "1min 26s" at 13:44). What the screenshots do
not show is illustrative too: the muscles and rest classes, the 5 kg step
(the logged 25 and 35 kg fit it), the end time, the session RPE, and the
earlier session's date and times. The Health account (`substrate-sync-`
and eight characters) and the workout's UUID are illustrative.

This is an excerpt. The full records are in
WorkoutsKit's test fixtures
(`Tests/WorkoutsCoreTests/Fixtures/examples/`), which are the encoding's
test vectors; `coverage.json` there
adds the methods the owner's screenshots do not show. Where the excerpt and
the files differ, the files win:

- **The routine and the 30 October session** keep the screenshots' Hevy
  values (exact rep targets, a 1 min 2 s plank).
  the starter routines (`Resources/Bundled/routines.json`) prescribes for the folder's
  purpose instead: a 10 to 15 range at RPE 9.5 on the chest press, a 45 s
  to 1 min 30 s plank, and a 1 min round rest on the superset. Do not apply
  this routine over the file's: a PUT replaces `slots` whole, so the file's
  other 16 slots would go.
- **The exercises and last time's drop chain** are the files' records,
  field for field. The chain's session (27 October) is in
  `sessions.json`, and `set.session` is
  `mustExist`, so that file is applied before this excerpt's last two
  records. Without it the server refuses them, naming the missing session
.

Every record here was written to a running substrate, in that order, and
the two session patches after them.

### Exercises

```yaml
kind: ~/training/exercise
metadata:
  id: chest-press-machine
data:
  properties:
    name: Chest Press (Machine)
    measurement: weightreps
    equipment: machine
    laterality: bilateral
    loadBasis: total
    primaryMuscles: [chest]
    secondaryMuscles: [shoulders, triceps]
    restClass: compound
    loadStep: {value: "5", unit: kg}
---
kind: ~/training/exercise
metadata:
  id: plank
data:
  properties:
    name: Plank
    measurement: duration
    equipment: none
    laterality: bilateral
    primaryMuscles: [abdominals]
    restClass: timed
    codes:
      - {system: free-exercise-db, code: Plank, display: Plank}
      - {system: healthconnect, code: EXERCISE_SEGMENT_TYPE_PLANK}
```

Crunch, Glute Bridge and Bird Dog follow the same pattern
(`measurement: repsonly`, `equipment: none`; Bird Dog is `laterality:
alternating`, and its reps count per side).

### A workout routine

Rests and targets as the screenshots show them: Plank with a 15 s rest and
last time's 1:02 and 0:53; Crunch with the rest off; Glute Bridge and Bird
Dog as a superset with no rest; Chest Press (Machine) with 1 min 30 s, a
25 kg warm-up and working sets at 35 kg, the second followed by a drop.

```yaml
kind: ~/training/routine
metadata:
  id: fully-body-collection-1
data:
  properties:
    name: "Fully Body Collection #1"
    folder: Full Body 3x Week (Back-Friendly)
    position: 1
    groups:
      - key: a
        groupKind: superset
        roundRest: PT0S
    slots:
      - key: plank
        exercise: ~/training/exercise/plank
        setRest: PT15S
        sets:
          - {key: s1, durationMin: PT1M2S, durationMax: PT1M2S}
          - {key: s2, durationMin: PT53S, durationMax: PT53S}
      - key: crunch
        exercise: ~/training/exercise/crunch
        setRest: PT0S
        sets:
          - {key: s1, repsMin: 15, repsMax: 15}
          - {key: s2, repsMin: 15, repsMax: 15}
      - key: glute-bridge
        exercise: ~/training/exercise/glute-bridge
        group: a
        transition: PT0S
        sets:
          - {key: s1, repsMin: 15, repsMax: 15}
          - {key: s2, repsMin: 15, repsMax: 15}
      - key: bird-dog
        exercise: ~/training/exercise/bird-dog
        group: a
        sets:
          - {key: s1, repsMin: 15, repsMax: 15}
          - {key: s2, repsMin: 15, repsMax: 15}
      - key: chest-press
        exercise: ~/training/exercise/chest-press-machine
        setRest: PT1M30S
        sets:
          - {key: w1, role: warmup, load: {value: "25", unit: kg}, repsMin: 15, repsMax: 15}
          - {key: s1, load: {value: "35", unit: kg}, repsMin: 15, repsMax: 15}
          - {key: s2, load: {value: "35", unit: kg}, repsMin: 13, repsMax: 13, structure: drop, drops: 1, dropReps: 8}
```

On write, each planned set gains `role: working` and `structure: straight`
where it names neither (the declared defaults); the session's plan snapshot
below does the same.

### A training session: the create, Finish, then the Health link

```yaml
kind: ~/training/session
metadata:
  id: 01a18d0c-5c80-7d3e-9a51-3c8e2b7d4f10
data:
  properties:
    name: "Fully Body Collection #1, Fri 30 Oct 13:42"
    at: "2026-10-30T13:42:40Z"
    utcOffsetStart: 0
    timezone: Europe/London
    sport: strength
    routine: ~/training/routine/fully-body-collection-1
    routineVersion: 1
    groups:
      - {key: a, groupKind: superset}
    slots:
      - {key: plank, exercise: ~/training/exercise/plank, routineSlot: plank}
      - {key: crunch, exercise: ~/training/exercise/crunch, routineSlot: crunch}
      - {key: glute-bridge, exercise: ~/training/exercise/glute-bridge, group: a, routineSlot: glute-bridge}
      - {key: bird-dog, exercise: ~/training/exercise/bird-dog, group: a, routineSlot: bird-dog}
      - {key: chest-press, exercise: ~/training/exercise/chest-press-machine, routineSlot: chest-press}
    revision: 1
    adapter: substrate-workouts@0.1.0
---
# Finish, sent as PATCH with ifVersion: the state, the end, the session RPE,
# and the slots again, each with the planned sets the session started with,
# done or not. Revision 13 is the Health workout's HKMetadataKeySyncVersion.
kind: ~/training/session
metadata:
  id: 01a18d0c-5c80-7d3e-9a51-3c8e2b7d4f10
data:
  properties:
    status: finished
    endsAt: "2026-10-30T14:31:00Z"
    utcOffsetEnd: 0
    sessionRPE: "7"
    slots:
      - key: plank
        exercise: ~/training/exercise/plank
        routineSlot: plank
        sets:
          - {key: s1, durationMin: PT1M2S, durationMax: PT1M2S}
          - {key: s2, durationMin: PT53S, durationMax: PT53S}
      - key: crunch
        exercise: ~/training/exercise/crunch
        routineSlot: crunch
        sets:
          - {key: s1, repsMin: 15, repsMax: 15}
          - {key: s2, repsMin: 15, repsMax: 15}
      - key: glute-bridge
        exercise: ~/training/exercise/glute-bridge
        group: a
        routineSlot: glute-bridge
        sets:
          - {key: s1, repsMin: 15, repsMax: 15}
          - {key: s2, repsMin: 15, repsMax: 15}
      - key: bird-dog
        exercise: ~/training/exercise/bird-dog
        group: a
        routineSlot: bird-dog
        sets:
          - {key: s1, repsMin: 15, repsMax: 15}
          - {key: s2, repsMin: 15, repsMax: 15}
      - key: chest-press
        exercise: ~/training/exercise/chest-press-machine
        routineSlot: chest-press
        sets:
          - {key: w1, role: warmup, load: {value: "25", unit: kg}, repsMin: 15, repsMax: 15}
          - {key: s1, load: {value: "35", unit: kg}, repsMin: 15, repsMax: 15}
          - {key: s2, load: {value: "35", unit: kg}, repsMin: 13, repsMax: 13, structure: drop, drops: 1, dropReps: 8}
    revision: 13
---
# The Health link, sent as PATCH with ifVersion once a GET of the computed
# health/workout id answered
kind: ~/training/session
metadata:
  id: 01a18d0c-5c80-7d3e-9a51-3c8e2b7d4f10
data:
  properties:
    healthKitWorkout: 9931515A-49DA-4850-A20D-956A5228C096
    workout: substrate.reamde.dev/health/workout/applehealth/substrate-sync-5e2b9c41/9931515A-49DA-4850-A20D-956A5228C096
    revision: 14
```

### Sets

Every set Substrate Workouts writes says how it was completed (`completionMethod`),
what its `at` is (`timeBasis`), the scale of its effort (`rpeScale`), what
its numbers meant (`laterality`, and with a load `loadBasis` and
`implements`, with reps `repsBasis`), how its rating got there
(`rpeMethod`, beside any `rpe`) and how its rest was measured
(`restMethod`, beside any `restBefore`).

```yaml
# Plank, working set 1: 1:01 against 1:02 planned. No load mode: a plank
# moves no load. The countdown is the timer mode the engine ran, written
# into the target although the plan left it out.
kind: ~/training/set
metadata:
  id: 01a18d0d-9cd0-7b1a-8c2d-9e0f1a2b3c4d
data:
  properties:
    session: ~/training/session/01a18d0c-5c80-7d3e-9a51-3c8e2b7d4f10
    exercise: ~/training/exercise/plank
    slot: plank
    order: 1
    ordinal: 1
    at: "2026-10-30T13:44:02Z"
    timeBasis: completion
    completionMethod: individual
    startedAt: "2026-10-30T13:43:01Z"
    duration: PT1M1S
    laterality: bilateral
    rpeScale: rirbased
    target: {plannedSet: s1, termination: time, durationMin: PT1M2S, durationMax: PT1M2S, timerMode: countdown}
    revision: 1
---
# Chest Press (Machine), the warm-up: 25 kg x 15 at RPE 6, which the owner
# tapped (no plan RPE to pre-fill on a warm-up). Corrected once after the
# tick, so revision 2.
kind: ~/training/set
metadata:
  id: 01a18d18-8d98-7c2b-9d3e-0f1a2b3c4d5e
data:
  properties:
    session: ~/training/session/01a18d0c-5c80-7d3e-9a51-3c8e2b7d4f10
    exercise: ~/training/exercise/chest-press-machine
    slot: chest-press
    order: 1
    ordinal: 1
    role: warmup
    at: "2026-10-30T13:55:59Z"
    timeBasis: completion
    completionMethod: individual
    loadMode: external
    load: {value: "25", unit: kg}
    reps: 15
    loadBasis: total
    implements: 1
    laterality: bilateral
    repsBasis: total
    rpe: "6"
    rpeMethod: entered
    rpeScale: rirbased
    target: {plannedSet: w1, termination: reps, repsMin: 15, repsMax: 15, load: {value: "25", unit: kg}}
    revision: 2
---
# Last time's working set 2 and its drop, as the PREVIOUS column shows them
# (35 kg x 13 @ 9.5, then 35 kg x 8 @ 9.5): one chain, labelled 2 and 2a.
# Both are records of the fixtures' sessions.json, whose 27 October session
# must be written first (`session` is mustExist), and the head is written
# before the drop (`continues` is mustExist). Their targets are that file's
# routine's, a 10 to 15 range at RPE 9.5 with one drop, run with no pause.
kind: ~/training/set
metadata:
  id: 01a17daa-4040-7b3c-8d5e-6f7081920314
data:
  properties:
    session: ~/training/session/01a17d8d-afc0-7a2b-bc4d-5e6f70819203
    exercise: ~/training/exercise/chest-press-machine
    slot: chest-press
    order: 3
    ordinal: 2
    structure: drop
    chainPosition: 1
    at: "2026-10-27T14:01:12Z"
    timeBasis: completion
    completionMethod: individual
    loadMode: external
    load: {value: "35", unit: kg}
    reps: 13
    loadBasis: total
    implements: 1
    laterality: bilateral
    repsBasis: total
    rpe: "9.5"
    rpeMethod: entered
    rpeScale: rirbased
    restBefore: PT1M35S
    restMethod: interaction
    restPlanned: PT1M30S
    target: {plannedSet: s2, termination: reprange, repsMin: 10, repsMax: 15, load: {value: "35", unit: kg}, rpeMin: "9.5", rpeMax: "9.5", drops: 1, intraSetPause: PT0S}
    revision: 1
---
kind: ~/training/set
metadata:
  id: 01a17daa-f3f0-7c4d-9e6f-708192031425
data:
  properties:
    session: ~/training/session/01a17d8d-afc0-7a2b-bc4d-5e6f70819203
    exercise: ~/training/exercise/chest-press-machine
    slot: chest-press
    order: 4
    ordinal: 2
    structure: drop
    continues: ~/training/set/01a17daa-4040-7b3c-8d5e-6f7081920314
    chainPosition: 2
    at: "2026-10-27T14:01:58Z"
    timeBasis: completion
    completionMethod: individual
    loadMode: external
    load: {value: "35", unit: kg}
    reps: 8
    loadBasis: total
    implements: 1
    laterality: bilateral
    repsBasis: total
    rpe: "9.5"
    rpeMethod: entered
    rpeScale: rirbased
    restBefore: PT6S
    restMethod: interaction
    restPlanned: PT0S
    target: {plannedSet: s2}
    revision: 1
```

The drop's 35 kg is the value Hevy pre-fills from the set before (Hevy has
no drop rule), which the owner logged; whether the pin moved is not known.
Under this package the app would suggest 25 kg for it: 20 % off 35 kg is
28 kg, rounded down to the 5 kg step.

