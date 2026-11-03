# Example records for substrate.reamde.dev/training

> **Where this came from.** Written on 2026-10-30 and revised on 2026-10-31 in rxtx.dev,
> as `research/workouts/substrate/examples/`, for the app then called Spotter; carried
> here on 2026-11-02. The records and this page now use the app's own kind names,
> `substrate.reamde.dev/training/<kind>` (decision record
> [0203](../../../../../docs/decisions/0203-an-app-keeps-its-own-package-under-a-declare-scope.md)),
> where they said `mneme.rxtx.dev/training/<kind>`. The YAML was also rewritten in block
> style for this repository's `lint:yaml` and `fmt:check`; the values did not change. The
> rest of the page is as written: where it talks of applying the package to Mneme or of
> `geoah.me`, it describes the plan at the time.

Records in the shape `substratectl apply -f` takes, for the training package,
now [`Package/`](../../Package/README.md) and then a draft in rxtx.dev's
`research/workouts/substrate/training/`. Three files are built from the
owner's 13 Hevy screenshots; a fourth, `coverage.yaml`, adds the methods the
screenshots do not show. Date: 2026-10-30, revised 2026-10-31 after three
reviews, and again on 2026-10-31 for the data-model review's decisions
(package README, "Data-model review, 2026-10-31").

Nothing here has been applied to production (`geoah.me`) or to the shared
dev server. The files pass the offline checker, and on 2026-10-31 a private,
throwaway substrate built from the commit production runs took all 163
records with no refusal ([Verification](#verification)).

## How these relate to the app

> **Since 2026-11-02 the app carries one sample routine, not the owner's folder**
> ([decision S1](../decisions.md#s1-one-sample-routine-that-shows-every-feature-written-once-and-deletable)).
> [`sample.yaml`](sample.yaml) is that routine and its thirteen exercises. The four files below it
> are test vectors now: the engine's tests replay the owner's sessions on the owner's
> routines, so they stay here, and the app ships none of them.

These YAML files are the readable form of records the app carries as JSON, in the flat
shape substrate's REST API speaks (`{"kind", "id", "properties"}`). The YAML is the source:
`apps/apple/scripts/workouts-package.py` writes the JSON from it, and
`mise run lint:python`, which CI's `lint` job runs, and `mise run apple:test:workouts` both run
the script with `--check`, so a change to one without the other fails:

| JSON in `apps/apple/`                                                    | Written from                                  | Used for                           |
| ------------------------------------------------------------------------ | --------------------------------------------- | ---------------------------------- |
| `WorkoutsKit/Sources/WorkoutsCore/Resources/Bundled/exercises.json`      | [`sample.yaml`](sample.yaml)'s exercises      | The catalogue signed out, and what the first sync writes into an empty repository |
| `WorkoutsKit/Sources/WorkoutsCore/Resources/Bundled/routines.json`       | [`sample.yaml`](sample.yaml)'s routine        | The same                           |
| `WorkoutsKit/Tests/WorkoutsCoreTests/Fixtures/examples/*.json`           | The four test-vector files, 163 records       | Codec, engine, presenter and suggestion tests |

Each JSON file names its YAML in its `source` field. The script loads the YAML with
PyYAML's safe loader minus the timestamp resolver, so instants stay strings, and turns each
document into `{kind, id: metadata.id, properties: data.properties}`. It also refuses an
exercise id written differently in two files: the sample's exercises are the test vectors'
records, value for value. The SwiftUI previews read no JSON: they run the bundled sample
through the engine, so they show what the app carries. On 2026-11-02 the script's output for
the four test-vector files was byte for byte the JSON that had been kept in step by hand
until then.

## Files

| File                               | Records                                                    | Holds                                                                                                                                                                                                                                                                                                 |
| ---------------------------------- | ---------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [`sample.yaml`](sample.yaml)       | 13 exercises, 1 routine                                    | **What the app ships.** "Sample: every feature": every measurement, side, load basis, set structure and way a set ends the app runs, each exercise with a note saying what it shows. Its header lists what each part shows and what is left out, with why.                                                                                          |
| [`exercises.yaml`](exercises.yaml) | 30 exercises                                               | Every exercise the screenshots show, with measurement type, equipment, laterality, load basis, implements, muscles, rest class, suggestion settings, and codes where the code is certain.                                                                                                             |
| [`routines.yaml`](routines.yaml)   | 4 workout routines                                         | The folder "Full Body 3x Week (Back-Friendly)": Fully Body Collection #1, Day 1 – Strength Foundation, Day 2 – Volume & Stability, and Day 3 (title hidden in the screenshot). Every planned set has a key.                                                                                           |
| [`sessions.yaml`](sessions.yaml)   | 3 training sessions, then 65 sets                          | Two finished Day 1 sessions (Thu 22 and Mon 26 October 2026, either side of the clock change) and the finished Fully Body Collection #1 session of Tue 27 October that the screenshots' PREVIOUS column reads from. Each session keeps the planned sets it started with; every set is its own record. |
| [`coverage.yaml`](coverage.yaml)   | 6 exercises, 5 setups, 1 routine, 3 sessions, then 46 sets | Not the owner's: test vectors for what the other files leave out. Setups at two gyms, a routine of every method, a session run from it, an empty session in progress, and a Hevy import.                                                                                                              |

Each file's header comment says what is the owner's and what is
illustrative, and why each choice was made. The comment above each set
record says what it is; in `sessions.yaml` it also quotes what research 03
Part B suggests for that set.

## Order

Apply in this order: exercises, then routines, then sessions, then the
coverage file.

1. **Exercises first.** Routine slots, session slots and sets point at them,
   and all three references are `mustExist`: a routine written before its
   exercises is refused, naming each missing one.
2. **Routines.** Sessions point at them (not `mustExist`: deleting a
   routine leaves its sessions alone).
3. **Sessions, then sets.** A set's `session` is `mustExist`, so a set
   written before its session is refused. `sessions.yaml` lists the three
   sessions first and the sets after them, and `apply -f` sends a file's
   record documents one at a time, in file order
   (`cmd/substratectl/commands/apply.go`), so one apply of the file is
   enough. `apply -f` stops at the first refusal.
4. **`coverage.yaml` last.** It lists its own exercises, then its setups,
   its routine, its sessions and its sets, in that order: a setup names an
   exercise and a set names a setup, both `mustExist`.
5. **A chain's head before its members.** `continues` is `mustExist`, so in
   every file a drop, a mini-set or a second side comes after the record it
   continues.

## Applying them to a scratch repository

Use a repository of your own on a substrate at main `ffd7b967` (the commit
production runs) or later. Never use `geoah.me`, nor the owner's
`mneme.rxtx.dev` repository on the dev server, and never a database or
port another session holds. Mneme's guide
(`svc/mneme-v6/CLAUDE.md` in rxtx.dev, "A scratch
repository") says how to get one. Substrate's `docs/getting-started.md`
covers `substratectl register` on a local server of your own.

With the scratch repository as the current context, run from the worktree's
root. These commands were run on 2026-10-31 on a private substrate at
`ffd7b967` ([On a running substrate](#on-a-running-substrate)).

```bash
# 1. The health catalogue the package requires (session.workout points into
#    it). Without it the package's apply is refused as a whole.
substratectl install substrate.reamde.dev/health

# 2. The draft package: one vocabulary batch.
substratectl apply \
  -f research/workouts/substrate/training/bundle.yaml \
  -f research/workouts/substrate/training/exercise.yaml \
  -f research/workouts/substrate/training/setup.yaml \
  -f research/workouts/substrate/training/routine.yaml \
  -f research/workouts/substrate/training/session.yaml \
  -f research/workouts/substrate/training/set.yaml

# 3. The records, in order.
substratectl apply -f apps/apple/SubstrateWorkouts/docs/examples/exercises.yaml
substratectl apply -f apps/apple/SubstrateWorkouts/docs/examples/routines.yaml
substratectl apply -f apps/apple/SubstrateWorkouts/docs/examples/sessions.yaml
substratectl apply -f apps/apple/SubstrateWorkouts/docs/examples/coverage.yaml

# 4. What landed: 36 exercises, 5 setups, 5 routines, 6 sessions and 111
#    sets. A list pages at 50 unless --limit says otherwise.
substratectl get substrate.reamde.dev/training/exercise
substratectl get substrate.reamde.dev/training/setup
substratectl get substrate.reamde.dev/training/routine
substratectl get substrate.reamde.dev/training/session
substratectl get substrate.reamde.dev/training/set --limit 200
```

The health catalogue's own three files from the substrate checkout
(`kinds/substrate.reamde.dev/health/health.yaml`, `sleep.yaml`,
`workout.yaml`) can be applied with `-f` instead of step 1. That lands it
`installed` rather than `published`. Re-applying any of the files is safe:
apply merges and never prunes, a put that names a session's current state
moves nothing, and no revision goes down. On the proving server a second
apply of every file answered `unchanged` for all 163 records. One
exception, by design: once the in-progress session in `coverage.yaml` has
been finished by PATCH (which raises its revision), a re-apply of that file
stops at it as a stale delivery.

What the offline tools cannot check, and what the server showed on
2026-10-31:

- **Units.** `kg`, `lb` and `m` resolve to the seeded unit records, and a
  load reads back as written. Every load came back in the unit written,
  the 60 lb assistance included.
- **`mustExist`.** A set applied before its session is refused, and so is a
  routine or a session whose slot names an exercise that does not exist, a
  set naming a setup or a chain head that does not exist, and a setup naming
  a missing exercise. All 111 sets landed after their sessions, setups and
  heads, and every slot's exercise resolved.
- **Declared defaults.** Every planned set (in a routine or in a session's
  plan snapshot) and set that names neither gets `role: working` and
  `structure: straight`. The server filled `role` on 106 planned sets in
  routines, 66 in snapshots and 101 sets, and `structure` on 110, 68 and 86,
  and `status: active` on the one session created without a status; nothing
  else differed from what was written, apart from each record's computed
  `title`.
- **The revision rule.** A session re-applied at a lower `revision` is
  refused as a stale delivery; an equal one is admitted.
- **States.** A session created `finished` stays so; one created without a
  status is `active` and finishes by PATCH. `abandoned` is not a state.
- **Lists.** One exercise's history comes back newest first, filtered by
  `exercise` and ordered by `at` descending, then `order` descending for the
  imported sets that share an `at`. The server answers a filter by
  reference from its `refs` table, with no declared index (package README,
  "Indices").

## What the screenshots give, and what was decided here

- **Fully Body Collection #1 is the routine the screenshotted session ran.**
  The card shows its first six exercises. The session's set grids and its
  reorder list show the rest, from Chest Press (Machine) to Push Up, in that
  order. That makes 21 slots. Nothing is assumed between Goblet Squat and
  Chest Press (Machine), which no screenshot shows.
- **Day 1 and Day 2 hold the five names their cards show.** A card stops
  after two lines, so the agent may append more.
- **The fourth card's title is hidden** behind the in-progress bar. Its
  exercises are read from the card text: Calf Extension (Machine), Leg Press
  (Machine), Chest Fly (Dumbbell), Seated Row (Machine), Overhead Press
  (Dumbbell). It is named "Day 3" with the id `day-3`, so a rename keeps the
  id.
- **Romanian Deadlift is the dumbbell one.** The card cuts the name at the
  bracket, and the folder is back-friendly. Dumbbells hang beside the legs,
  can be put down at once, and start far lighter than a 20 kg bar. The owner
  already does the single-leg version with dumbbells.
- **The rests the screenshots show are kept**:
  - Plank 15 s;
  - Crunch and Lumbar Extension Stretch off (`PT0S`);
  - Chest Press (Machine) 1 min 30 s;
  - nothing inside the Glute Bridge and Bird Dog superset.

  One change: the superset gets a 1 min round rest, which Hevy cannot
  express (research 02 §3.4). The rest inside the superset and the rest
  after it then differ.
- **Prescriptions are written for the folder's purpose.** They are
  - rep ranges with a target RPE;
  - the hinge and the squat kept further from failure (RPE 7);
  - one or two warm-ups on each routine's first compound;
  - slot notes that keep the lower back neutral.

  Loads are seeds. The chest press keeps the screenshot's 25 kg warm-up,
  35 kg working load and RPE 9.5, with a 10 to 15 range, which is the stored
  target that test vectors V1 and V2 assume.
- **Planned sets have keys**: `w1` and `w2` for warm-ups, `s1` to `s3` for
  working sets. A set's `target.plannedSet` names the key, never a position.
- **Exercise details the screenshots do not give are judgement calls**:
  - the goblet squat uses a kettlebell, as free-exercise-db's does;
  - the two plate-loaded machines log one arm's plates (`loadBasis:
    perside`, 2.5 kg steps);
  - the dumbbell exercises hold two dumbbells (`implements: 2`), except the
    single-leg Romanian deadlift, whose slot note says one;
  - "Lumbar Extention Stretch" is Hevy's spelling. It is kept as an alias,
    and the name is spelt correctly.
- **Reps count per side** on the one-side-at-a-time and alternating
  exercises: the bird dog's 10 reps are 10 with each side, which is why each
  bird dog set takes 1 min 15 s.
- **Codes only where certain.**
  - `free-exercise-db` ids come from yuhonas/free-exercise-db,
    `dist/exercises.json`, fetched 2026-10-30. An id is used only where its
    entry is the same movement on the same equipment, checked against the
    entry's instructions.
  - `healthconnect` codes are androidx `ExerciseSegment` constants. They are
    used only where a segment type exists for exactly that movement.
  - The selectorized chest press, seated row and vertical traction have no
    code, nor does any exercise whose catalogue entry differs (a barbell
    Romanian deadlift, a ball torso rotation).
  - No Hevy template ids. Hevy identifies an exercise only by its template
    id, read from the owner's account (`GET /v1/exercise_templates`), which
    this draft could not reach. An importer adds them, matched on the exact
    titles the screenshots print, as the owner confirms each mapping.
- **Hevy's "D" row is the second record of working set 2's chain.**
  - The 35 kg × 13 set at RPE 9.5 is the head: `structure: drop`,
    `chainPosition: 1`, `ordinal: 2`.
  - The 35 kg × 8 drop at RPE 9.5 `continues` it, with `chainPosition: 2`
    and the same ordinal and planned set.
  - Its `restBefore` is 6 s, and its `restPlanned` is `PT0S`, because a drop
    runs no timer.
  - It is one working set, labelled 2 and 2a. It is not a fourth set, so
    counts, PREVIOUS and the suggestion all see set 2.
  - The drop's 35 kg is the value Hevy pre-fills from the set before (Hevy
    has no drop rule), which the owner logged; whether the pin moved is not
    known. Spotter would have offered 25 kg (V10a).
- **No load on the core work.** The plank, crunch, lumbar extension stretch,
  glute bridge and bird dog sets carry no `loadMode`: they move no load and
  add nothing to volume. The screenshot agrees: after ten such sets and the
  25 kg × 15 warm-up, Hevy's Volume read 375 kg.
- **Rest is measured as the examples assume the app does.**
  - `restBefore` runs from the tick of the set before to the end of the
    rest: Skip, or the first touch on the next set. A rep set has no
    recorded start.
  - `restPlanned` is research 02 §3.3's choice on the set before.
  - `startedAt` is only on timed sets and carries.

  The app's design decides the details. The header of `sessions.yaml` has
  the full rule.
- **Finish drops slots with no sets.** The Fully Body session keeps the nine
  exercises done of 21, in routine order, which is the order shown and the
  order an export takes; the sets' `at` gives the order they were done in.
  This is an assumption about Finish (open question 32).
- **Each kept slot holds the plan it started with** (`slots[].sets`): the
  routine slot's planned sets, done or not, with their keys and the
  routine's values. Every planned set here was done, so no record shows a
  planned set that was not; a routine slot with no set done is not kept,
  and its planned sets go with it.
- **How each value got there.** Every Spotter set carries
  `completionMethod: individual`, `timeBasis: completion`, `rpeScale:
  rirbased`, and a `restMethod` beside any `restBefore`: `start` where a
  timed set's play ended the rest (the 27 October second plank, and every
  timed set of the coverage session with a rest before it), `interaction`
  elsewhere. Every
  rating is `rpeMethod: entered` except one: the 26 October leg press set 1
  (105 kg × 9) was logged with the plan's RPE 8 shown pre-filled and left
  untouched, `rpeMethod: prefilled`. Both kinds are in the files.
- **What the numbers meant.** Each set copies its exercise's `laterality`,
  and with a load its `loadBasis` and `implements` (the push-ups also
  `bodyShare`, 0.65; the assisted pull-up and the weighted dip in
  `coverage.yaml` 1, since their exercises name none), with `repsBasis` beside
  any reps: `perside` on the split squat, the Pallof press and the bird dog,
  `total` elsewhere.
- **Targets are the full prescription.** Effort targets are ranges
  (`rpeMin` and `rpeMax`, equal here, except Day 3's overhead press at RPE 7
  to 8, which no session ran). Each set's target also carries what the
  engine ran with where the plan left it out: `timerMode: countdown` on the
  planks, `myoStopRule: repdrop` and a 15 s `intraSetPause` on the myo-rep
  sets, `PT0S` on the drop set, and the Romanian deadlift's tempo.
- **Time.** Europe/London; the clocks went back at 02:00 on Sunday
  25 October 2026.
  - The first Day 1 session is at 12:34 BST: `at` 11:34:08Z,
    `utcOffsetStart` 60.
  - The second is at 12:31 GMT: `at` 12:31:15Z, offsets 0.
  - `name` is on the local clock, and each session's `timezone` is
    Europe/London; its `sport` is `strength`.
- **Apple Health.** Each session's `workout` points at
  `applehealth/substrate-sync-5e2b9c41/<UUID>`: Substrate Sync's account
  for the phone is `substrate-sync-` and the first eight characters of its
  install id. `5e2b9c41` and the UUIDs are illustrative; the app reads the
  real account from the server (package README, "Apple Health").
- **Ids.**
  - Exercises and routines use slugs of the name.
  - Sessions and sets use lowercase UUIDv7s whose first 48 bits are the
    record's own instant: a session's start, a set's end. The random bits are
    fixed by a seed.
  - The Fully Body session's id, and the ids and times of its chest press
    set 2 and drop, are the ones in the package README's excerpt.
- **Session revisions.** Each session is at revision 3, counting only
  Start, Finish and the Health link. The app raises `revision` on every
  patch it sends, so a session with a pause or a note would end higher (the
  package README's excerpt ends at 14).
- **Illustrative.** The dates, both Day 1 sessions, every time and rest, the
  sets the screenshots do not show, the body mass, the Health account and
  the HealthKit UUIDs are illustrative.

## What coverage.yaml adds

None of it is the owner's. Its sessions are dated after 27 October or carry
a setup, and share no exercise with the 30 October table below, so they
move none of the other files' vectors.

- **Six exercises**, one for each measurement the owner's exercises do not
  use: a rowing machine (distance and time), an assisted pull-up whose stack
  is in pounds, a weighted dip, a farmer's carry (load and distance, two
  dumbbells), a side plank (timed, one side at a time) and a weighted plank
  (load and time). With the owner's four, all nine measurements appear.
- **Coverage – every method**, a routine in the folder "Spotter test
  vectors", with `activityType: functionalstrengthtraining`.
- **Wed 28 Oct, run from that routine**, which copies the activity type.
  The rope pushdown is swapped for the bar pushdown (a new session slot
  naming the routine slot it replaces), and the leg press has a set the plan
  did not have, with no target.
- **Five setups**: the hotel gym's chest press (a 2.5 kg grid from 5 kg to
  80 kg, overriding the home gym's 5 kg step), leg extension and seated
  row, and the old gym's chest press and seated row.
- **Thu 29 Oct, an empty session in progress**: created with no `status`
  and no `endsAt`, as the app's Start writes it. A deload at a hotel gym,
  so every set names the hotel gym's setup of its exercise
  (`hotel-gym-chest-press` and so on). The owner added three
  exercises, set a 30 s warm-up rest on one and made the other two a
  superset with a 15 s transition and a 1 min 30 s round rest; the slots
  and the group hold those values, because no routine does. Its sets have
  no target. Two are at RPE 6.5, which a Hevy export rounds down to 6.
- **Sat 3 Oct, a Hevy workout imported**: ids composed from Hevy's keys
  (`hevy-<workout id>` and `hevy-<workout id>-<exercise index>-<set
  index>`), every set at the session's start with no rest fields, Hevy's
  RPE scale, Hevy's own set types in `sourceType` (a `dropset` row as a
  drop chain's second record, a `failure` set as `reachedFailure`), the old
  gym's setups, and imported provenance: `completionMethod: imported`,
  `timeBasis: session`, `rpeMethod: imported`. No routine, no body mass; the offsets and the local
  `name` come from the zone the importer assumes.

## Methods covered

| Method                                           | Routine                                                                                              | Session records                                                                                                                         |
| ------------------------------------------------ | ---------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| Warm-ups, and a rest override after the last one | Day 1 leg press (also the first compound of every routine)                                           | W1 and W2 of both Day 1 sessions; the Fully Body chest press W1                                                                         |
| A warm-up rest per slot                          | coverage leg press (`warmupRest: PT1M`)                                                              | 28 Oct leg press 1 (`restPlanned` PT1M)                                                                                                 |
| Top set and back-off set                         | coverage leg press (`roleLabel`, back-off `dropPercent: 10`)                                         | 28 Oct: 110 kg × 6, then 95 kg × 10                                                                                                     |
| A set the plan did not have                      | none                                                                                                 | 28 Oct leg press 3 (no target)                                                                                                          |
| Myo-reps (activation, mini-sets, stop rule)      | Day 1 lateral raise, set 2                                                                           | 22 Oct: 16 + 4, 4, 4, 3 (stops when a mini-set loses a rep); 26 Oct: 17 + 4, 4, 4, 4, 4 (stops at the cap of five)                      |
| Myo-rep match                                    | coverage seated leg curl, set 2                                                                      | 28 Oct: 7 + 3 + 2, matching set 1's 12 (`totalReps` resolved)                                                                           |
| Rest-pause                                       | coverage leg extension (`miniSets: 2`, 20 s)                                                         | 28 Oct: 12 + 4 + 3                                                                                                                      |
| Cluster, with an RIR target                      | coverage hip thrust (4 clusters of 3, `rirMin`/`rirMax` 2)                                           | 28 Oct: 3, 3, 3, 3                                                                                                                      |
| Drop set                                         | Fully Body chest press, set 2                                                                        | 27 Oct: 2 and 2a; the Hevy import's dropset row                                                                                         |
| AMRAP, and a failed rep                          | coverage pushdown, set 2                                                                             | 28 Oct: 16 reps, `reachedFailure`, `failedRepAttempted`, a set note                                                                     |
| A set to failure                                 | Day 2 rope pushdown, set 3; coverage dip, set 3                                                      | 28 Oct dip 3 (`reachedFailure`); the Hevy import's failure set                                                                          |
| A substituted exercise                           | coverage pushdown                                                                                    | 28 Oct: the bar pushdown in the rope's slot                                                                                             |
| Timed holds                                      | Fully Body plank; coverage weighted plank                                                            | 27 Oct: 1:02 and 0:53, with `startedAt`; 28 Oct: 0:55 and 0:50 with 10 kg                                                               |
| One side at a time, with a side switch           | Fully Body split squat, single-leg RDL, torso rotation, Pallof press, step down; coverage side plank | 27 Oct: split squat and Pallof press, left then right; 28 Oct: side plank, the right side matched to the left (`sideMatch`), stopwatch  |
| Distance                                         | coverage rowing warm-up, carry                                                                       | 28 Oct: 500 m in 2:05; 40 m carries with 20 kg in each hand                                                                             |
| Bodyweight, ended by an effort target            | Fully Body push up (`termination: rirstop`)                                                          | 27 Oct: 14 and 11 at RPE 9, with the session's `bodyMass`                                                                               |
| Assistance in pounds, bodyweight plus a load     | coverage assisted pull-up, weighted dip                                                              | 28 Oct: 60 lb of assistance; 10 kg added, and a warm-up with 0 kg added                                                                 |
| Superset with different rests inside and after   | Fully Body glute bridge and bird dog (0 s, then 1 min)                                               | 27 Oct: `restPlanned` PT0S between members, PT1M between rounds                                                                         |
| Superset with the default transition             | Day 2 incline press and vertical traction (antagonists, 15 s by default, 2 min 30 s round rest)      | none                                                                                                                                    |
| Paired sets with rounds, warm-ups before round 1 | coverage pull-up and dip (3 rounds, 1 min between, 1 min 30 s per round)                             | 28 Oct: the dip warm-up, then three rounds                                                                                              |
| A circuit with rounds                            | coverage carry, side plank, weighted plank (2 rounds, 1 min)                                         | 28 Oct: two rounds                                                                                                                      |
| A superset made in the gym, rests on the session | none                                                                                                 | 29 Oct: the slots' `transition`, `warmupRest` and `setRest`, the group's `pairing`, `rounds` and `roundRest`                            |
| Tempo                                            | Day 1 Romanian deadlift (3-1-1-0)                                                                    | none                                                                                                                                    |
| A skipped rest, a pause, a corrected set         | none                                                                                                 | 22 Oct iso-lateral set 3 (`restSkipped`); 26 Oct pause; 26 Oct iso-lateral set 1 (`revision: 2`)                                        |
| A set with no RPE                                | none                                                                                                 | 22 Oct row set 3 and RDL set 1; the screenshot's unrated sets                                                                           |
| A pre-filled RPE beside entered ones             | Day 1 leg press, RPE 8                                                                               | 26 Oct leg press 1 (`rpeMethod: prefilled`); every other rating `entered`                                                               |
| The plan as it stood                             | every routine slot                                                                                   | every kept slot's `slots[].sets`                                                                                                        |
| An RPE range                                     | Day 3 overhead press (RPE 7 to 8)                                                                    | none                                                                                                                                    |
| A setup with its own grid                        | none                                                                                                 | the hotel gym's chest press setup (2.5 kg steps)                                                                                        |
| A deload, setups, an in-progress session         | none                                                                                                 | 29 Oct (`deload`, the hotel gym's setups, no `status`)                                                                                  |
| An import                                        | none                                                                                                 | 3 Oct (`rpeScale: hevy`, `sourceType`, the old gym's setups, `completionMethod: imported`, `timeBasis: session`, `rpeMethod: imported`) |
| An activity type                                 | coverage routine                                                                                     | 28 Oct copies it                                                                                                                        |

Not exercised by any record here: a planned set that was not done,
tri-sets and giant sets, `dropStep` on a routine or a set, `dropReps` (only
the package README's excerpt has one), a planned set's own `myoStopRule`,
`timerMode: countdown` written out on a routine, `rpeScale` `strong` or
`cr10`, `rir` logged on a set and `rirMethod`, `rpeMethod: copied`,
`completionMethod: bulk`, `timeBasis: estimated`, `restMethod` `imported` or
`estimated`, `resistanceLabel`,
`loadPercent`, `loadReference` and `referenceLoad`, `sportSource`, and a
session's `pauses` with no end (a session paused now). The package README's
proof wrote each of the new ones on the server once
(package README, "On a running substrate").

## Progression the suggestion engine can use

A throwaway script implemented research 03 §10 to §12, the specification
the V1 app's suggestion function follows, and ran it over `sessions.yaml`
alone, with no earlier Hevy history: the copied targets, the exercises' load
grids, and history newest first. The results below are what the app's tests
should reproduce from these files. The two lateral raise rows that changed
in review were recomputed by hand from the same rules.

**The first Day 1 session (Thu 22 Oct)** has no history:

- Every first working set is a cold start from the routine's load: "First
  time; routine load, try 100 kg × 6".
- The iso-lateral press's set 1 went 20 kg × 10 at RPE 7, so set 2 is raised
  inside the session: "Set 1: 20 kg × 10 at RPE 7, easier than RPE 8; try
  22.5 kg × 8".
- The Romanian deadlift's set 1 stopped at 7 reps with no RPE. That counts
  as RPE 10, so set 2 is cut: "Set 1: 16 kg × 7, harder than RPE 7; try
  12 kg × 10".
- The lateral raise's set 1 went 6 kg × 13 at RPE 7, so the activation set
  is kept at the load with the reps the set implies: "Set 1: 6 kg × 13 at
  RPE 7; try 6 kg × 15".
- The myo-rep set's fourth mini-set got 3: "Stop: mini-set 4 got 3, first
  was 4".

**The second Day 1 session (Mon 26 Oct)**, four days later:

| Set                         | Suggested                                                      | Done                         |
| --------------------------- | -------------------------------------------------------------- | ---------------------------- |
| Leg press W1, W2            | Warm-up, as last time: 40 kg × 12, then 70 kg × 6              | as suggested                 |
| Leg press 1                 | Last time 100 kg × 10 at RPE 7.5, top of range; try 105 kg × 9 | 105 kg × 9, RPE 8 pre-filled |
| Leg press 2                 | Set 1: 105 kg × 9, assuming RPE 8; try 105 kg × 9              | 105 kg × 9 at RPE 8.5        |
| Leg press 3                 | Set 2: 105 kg × 9 at RPE 8.5; try 105 kg × 8                   | 105 kg × 8 at RPE 9          |
| Iso-lateral press 1         | Last time 20 kg × 10 at RPE 7, top of range; try 22.5 kg × 8   | 22.5 kg × 10 at RPE 8        |
| Iso-lateral press 2         | Set 1: 22.5 kg × 10 at RPE 8; try 22.5 kg × 10                 | 22.5 kg × 9 at RPE 8.5       |
| Iso-lateral press 3         | Set 2: 22.5 kg × 9 at RPE 8.5; try 22.5 kg × 8                 | 22.5 kg × 8 at RPE 8.5       |
| Seated row 1                | Last time 45 kg × 12 at RPE 8, top of range; try 50 kg × 9     | 50 kg × 10 at RPE 8          |
| Seated row 2                | Set 1: 50 kg × 10 at RPE 8; try 50 kg × 10                     | 50 kg × 10 at RPE 8.5        |
| Seated row 3                | Set 2: 50 kg × 10 at RPE 8.5; try 50 kg × 9                    | 50 kg × 9 at RPE 9           |
| Lateral raise 1             | Last time 6 kg × 13 at RPE 7; try 6 kg × 15                    | 6 kg × 14 at RPE 7           |
| Lateral raise 2, activation | Set 1: 6 kg × 14 at RPE 7; try 6 kg × 16                       | 6 kg × 17 at RPE 9           |
| Lateral raise 2, mini-sets  | Mini-set 1 to 5: 6 kg × 4, then "Stop: 5 mini-sets done"       | 4, 4, 4, 4, 4                |
| Romanian deadlift 1         | Last time 16 kg × 7, below target; try 12 kg × 11              | 12 kg × 12 at RPE 7          |
| Romanian deadlift 2         | Set 1: 12 kg × 12 at RPE 7; try 12 kg × 12                     | 12 kg × 12 at RPE 7.5        |
| Romanian deadlift 3         | Set 2: 12 kg × 12 at RPE 7.5; try 12 kg × 11                   | 12 kg × 12 at RPE 8          |

Leg press set 1's RPE 8 was pre-filled from the plan and logged untouched,
so the suggestion reads it as no rating: the set met its range, so it is
taken at the target's top, RPE 8, which gives the same numbers as a rated 8
and a reason that says "assuming RPE 8", never "at RPE 8" (package README,
"Effort provenance").

The history rule appears in each of its three directions: one step up when
the top of the range was reached, reps added inside the range, and a load
cut below it (the lead decision: cut, not repeat). The leg press, the
iso-lateral press and the row all carry more load than in the first
session. Each activation set does one rep more than its set 1 implies
(13 at RPE 7 is 16 in reach, then 16 at RPE 9 is 17), which is inside the
error of an RPE rating.

**The next Fully Body Collection #1 session**, before anything is done.
This is the screenshots' own session on 30 Oct, which these files do not
hold. Its PREVIOUS column is these records; the push-up rows assume the
same 82.4 kg body mass as on 27 October:

| Set                                  | Suggested                                                | Research 03 test vector                  |
| ------------------------------------ | -------------------------------------------------------- | ---------------------------------------- |
| Chest press W1                       | Warm-up, as last time: 25 kg × 15                        | V8b                                      |
| Chest press 1                        | Last time 35 kg × 15, top of range; try 40 kg × 11       | V1                                       |
| Chest press 2                        | Last time 35 kg × 13 at RPE 9.5; try 35 kg × 14          | V2                                       |
| Chest press 2a, after set 2 at 35 kg | Drop 20% from 35 kg: 25 kg to failure                    | V10a's numbers                           |
| Plank 1                              | Last time 1:02; try 1:07                                 | V11a                                     |
| Plank 2                              | Last time 0:53; try 0:58                                 | V11b                                     |
| Bulgarian split squat 1              | Last time 10 kg × 10 at RPE 8.5; try 10 kg × 10          | V18's weaker side; the higher side's RPE |
| Bulgarian split squat 2              | Last time 10 kg × 9 at RPE 9; try 10 kg × 9              | V18's weaker side; the higher side's RPE |
| Push up 1                            | Last time BW × 14 at RPE 9; try BW × 15, shown as a hint | 11.3's single-point grid; `rirstop`      |
| Push up 2                            | Last time BW × 11 at RPE 9; try BW × 12, shown as a hint | 11.3's single-point grid; `rirstop`      |

In that session Hevy's grey placeholders were last time's values (35 kg and
15 reps for set 1, 35 kg and 13 for set 2, 35 kg and 8 for the drop).
Spotter's would be the suggestions above.

How the last four rows read the records (package README, "Reading"):

- **A side chain is one set.** The split squat's set 2 was left 10 at
  RPE 8.5, then right 9 at RPE 9. Its reps are the weaker side's, 9 (03
  §10.3, test vector V18), and its RPE the higher of the two, 9, so
  `n = 9 + 10 − 9 = 10` and the history rule adds one rep to 8: 9 reps at
  the same load. Set 1 (10 and 10, RPE 8 and 8.5) gives 10.
- **A `rirstop` set ends at its effort.** The push-up's plan has no rep
  range, so 03 falls back to 8 to 12 at the planned RPE 9. A plain
  bodyweight exercise has a grid of one point, so history adds a rep with
  no cap: 15, then 12. Under `rirstop` that count is shown only as a hint:
  the set ends when one rep is left in reserve.

## Verification

Run on 2026-10-31, after the data-model review, with
`/tmp/spotter-bin/substratectl`, built from substrate main `ffd7b967`, from
the worktree's root.

**The files with the package files.** The checker resolves references only
into the files given and the seeded packages, so the health catalogue's
three files are added. Without them the package itself does not resolve:
see the package README. The command, with `<package files>` standing for
the six files in `research/workouts/substrate/training/` and `<health
catalogue>` for `health.yaml`, `sleep.yaml` and `workout.yaml` in
`/tmp/spotter-substrate-main/kinds/substrate.reamde.dev/health/`, followed
by `echo "exit=$?"`:

```console
$ substratectl validate <package files> <health catalogue> -f .../exercises.yaml -f .../routines.yaml -f .../sessions.yaml -f .../coverage.yaml
declarations: 11 documents checked in 2 packages (substrate.reamde.dev/training, substrate.reamde.dev/health)
records: 163 documents, envelope checked
checked by the server only: declared defaults, references into packages beyond these files and the shipped core and llm, and record properties
exit=0
```

**Consistency of the files**, checked by a script over the parsed records
of all four (the offline checker reads only the envelopes, so the same
script also checks every property against the kinds: each key declared at
its level, each enum value one of the declared, each bound, decimal and
duration):

- every id is unique, and every UUIDv7 has version 7, the RFC variant, and a
  timestamp equal to its record's `at`;
- every reference to an exercise, a setup, a routine, a session or a chain
  head names a record written earlier in the order above, and a set's setup
  is a setup of that set's exercise;
- every set lies inside its session's span, belongs to a slot of its
  session, and names that slot's exercise;
- every slot's `order` runs 1, 2, 3 in time order;
- every `continues` names a chain head of the same session, slot, ordinal
  and structure;
- every copied target equals the session's plan snapshot's planned set,
  found by key, and every snapshot equals its routine slot's planned sets;
  no target holds the old `rpe` or `rir`;
- every set has `rpeMethod` exactly when it has `rpe`, and `restMethod`
  exactly when it has `restBefore`; Spotter's sets carry `rpeScale:
  rirbased`, `completionMethod: individual` and `timeBasis: completion`,
  the import's `imported`, `imported` and `session`; the one `prefilled`
  rating equals its target's `rpeMax`;
- every set's `laterality`, `repsBasis`, `loadBasis`, `implements` and
  `bodyShare` are its exercise's, resolved, and the load fields appear only
  with a load;
- every session has `timezone` and `sport: strength`;
- every planned-set key is unique in its slot, warm-ups come first, and a
  group's `rounds` equals its members' most working sets;
- every timed set's `startedAt` plus `duration` is its `at`;
- a set has a `loadMode` exactly when its exercise moves a load;
- no rest ends after the tick it precedes, and no rep set is done faster
  than 1.8 s a rep (per side on an alternating exercise).

### On a running substrate

Run on 2026-10-31 on a private, throwaway substrate built from
`ffd7b967`, with a Postgres of its own; the package README's
"On a running substrate" (a section of the draft's README in rxtx.dev,
not carried into this repository)
says how it was set up and lists every refusal it gave. In a repository
holding the health catalogue and the package, the files applied in order
with no refusal and no change to any of them:

```console
$ substratectl apply -f apps/apple/SubstrateWorkouts/docs/examples/exercises.yaml
substrate.reamde.dev/training/exercise/leg-press-machine created
… 30 created
$ substratectl apply -f apps/apple/SubstrateWorkouts/docs/examples/routines.yaml
substrate.reamde.dev/training/routine/fully-body-collection-1 created
substrate.reamde.dev/training/routine/day-1-strength-foundation created
substrate.reamde.dev/training/routine/day-2-volume-stability created
substrate.reamde.dev/training/routine/day-3 created
$ substratectl apply -f apps/apple/SubstrateWorkouts/docs/examples/sessions.yaml
substrate.reamde.dev/training/session/01a16363-cf80-73ed-85fd-704f6ce38d73 created
substrate.reamde.dev/training/session/01a17831-8a38-7502-a232-81e9c175d65e created
substrate.reamde.dev/training/session/01a17d8d-afc0-7a2b-bc4d-5e6f70819203 created
… 65 sets created
$ substratectl apply -f apps/apple/SubstrateWorkouts/docs/examples/coverage.yaml
… 6 exercises, 5 setups, 1 routine, 3 sessions and 46 sets created
```

Applied before the exercises, `routines.yaml` was refused, naming each
missing exercise (`props.slots[0].exercise: reference names
substrate.reamde.dev/training/exercise/leg-press-machine, which does not exist`,
and so on).

What the records answered:

- **The folder.** Routines with `folder eq "Full Body 3x Week
  (Back-Friendly)"`, ordered by `position`: Fully Body Collection #1, Day
  1, Day 2, Day 3.
- **One session.** The 27 October session read back as written: finished,
  nine slots in routine order, each with its plan snapshot (the lumbar
  extension stretch's lists `s1` and `s2`, both done), the superset,
  82.4 kg body mass, its `timezone`, `sport: strength`, the HealthKit UUID
  and its `workout` pointer.
- **The chest press history**, filtered by `exercise` and ordered by `at`
  descending: the hotel gym's two sets (29 October), 35 kg × 8 at RPE 9.5
  (2a, `continues` the next row), 35 kg × 13 at RPE 9.5 (2), 35 kg × 15
  (1), 25 kg × 15 (the warm-up), then the old gym's four imported sets.
  With `setup exists false`: the 27 October four. With `setup eq` the hotel
  gym's chest press setup: its two.
- **Ratings.** `rpeMethod eq prefilled` finds the one pre-filled set,
  `entered` 58 and `imported` 3.
- **One session's sets.** The 27 October session's 24 sets, filtered by
  `session` and ordered by `at`, in the order worked; each set's title is
  its exercise's name, from `{exercise}`.
- **References.** Every set's `session`, `exercise`, `continues` and
  `setup` resolves (259 references). `referencing` the chest press finds
  its one routine, three sessions through the nested `slots[].exercise`, two
  setups and ten sets. The `workout` pointers name Apple Health rows that do not exist
  here, which is allowed.
- **Round trip.** Every record compared field by field with its file: only
  the declared defaults, the in-progress session's `status: active` and
  each record's computed `title` were added.
- **The in-progress session** finished by PATCH with `ifVersion` (a stale
  one got 409) at revision 6, after which a re-apply of `coverage.yaml` is
  refused at that session as a stale delivery, as [Applying them to a scratch repository](#applying-them-to-a-scratch-repository) says.

The package README's excerpt matches these files for the records it shares
with them (the chest press, the plank, and the 27 October drop chain).
Applied after `sessions.yaml` in a fresh repository, those came back
`unchanged`. In a third repository, `coverage.yaml` applied after
`exercises.yaml` and `routines.yaml` alone created all 61 of its records:
it depends on no record of `sessions.yaml`.

## Caution for the V1 app

The lead decision is that the V1 app ships with the example records bundled.
The exercises and routines of the first two files are fit to ship. The
sessions are test vectors: apart from the screenshot's numbers, their sets
are invented. Bundled as the owner's history, they would put invented loads
into PREVIOUS and into every suggestion. Bundle them for tests and previews
only, or keep them apart from the owner's own sessions. `coverage.yaml` is
for tests only, all of it: its exercises and routine are not the owner's
either.

How the app uses these records today is in
[How these relate to the app](#how-these-relate-to-the-app) above.

