# Spotter training package: data-model review

> **Where this came from.** Part of the data-model review of 2026-10-31, kept verbatim
> ([the index](../../README.md#reviews)). It reviewed the draft package `mneme.rxtx.dev/training`
> in rxtx.dev's `research/workouts/substrate/training/`, the package that became the
> app's own `~/training` in [`Package/`](../../../Package/README.md). The worktree paths
> and `/tmp` paths it cites were on the build Mac and are not in this repository.

Reviewer: Fable 5.1, 2026-10-31. Tree: `/Users/geoah/.t3/worktrees/rxtx.dev/t3-9e79ae4f`
(branch `feat/spotter-ui-review`). Substrate at production's commit:
`/tmp/spotter-substrate-main`. Nothing in the repository was edited.

Line references are `path:line` in that tree unless the path starts with
`substrate/`, which means `/tmp/spotter-substrate-main/`.

## Verdict

The package is sound and ready to carry real data once four things are
settled, all of them in places substrate cannot fix later: nested fields
(no `renamedFrom`, no backfill) and datatype choices (a string can never
become a reference). The rest of the model is well argued, matches
substrate's first-party spelling where the two overlap, and should not be
churned.

The four decisions to take before the first real set is written:

1. **RPE provenance** (the Rows screen). Add `set.rpeMethod`
   (`entered` or `prefilled`) and make the suggestion engine and every
   export read only an `entered` RPE as a rating. Technically additive, but
   any set written before it exists is ambiguous forever, V1's local
   history included.
2. **Effort targets as ranges** on the planned set and on `set.target`:
   `rpeMin`/`rpeMax` and `rirMin`/`rirMax`, the pattern `repsMin`/`repsMax`
   and `durationMin`/`durationMax` already follow. These are object fields,
   so the name chosen now is permanent.
3. **`set.setup` is a key, not a label.** Either a reference to a small
   `setup` kind (recommended: it is what 03 §9.1 asks for, "per exercise
   and setup") or, at minimum, a slug pattern so a later `setup` record can
   take the same id. A string cannot become a reference once a value is
   stored.
4. **Relative loads.** Reserve the field names for percent-of-reference
   loads on the planned set (`loadPercent`, `loadReference`) and write down
   that `target.load` is always the resolved absolute load. Additive, but
   the semantics of `target.load` must be fixed before an importer or the
   agent writes a percent plan.

Everything else is either additive (conditioning groups, programs, bands,
cardio fields, derived values, skipped sets) or a documentation fix in the
README's mapping tables (five rows, listed below).

## Recommended changes, in priority order

Severity key: **A** = do before real data (cannot be changed in place
later); **B** = do soon (cheap now, costs a conversion or a parallel field
later); **C** = later (additive whenever needed).

### A1. RPE provenance: `set.rpeMethod`

See the dedicated section at the end for the full argument. The change:

```yaml
# set.yaml, after rpeScale
rpeMethod:
  type: enum
  description: how rpe got its value; written whenever rpe is written
  values:
    - {value: entered, label: Rated by the lifter}
    - {value: prefilled, label: Pre-filled from the plan and left unchanged}
```

Aligns with: HealthKit `HKMetadataKeyWasUserEntered` (a sample the user
typed versus one the device produced); Health Connect `Metadata.recordingMethod`
(`MANUAL_ENTRY` versus `AUTOMATICALLY_RECORDED`), which substrate already
mirrors as `observation.recordingMethod`
(`substrate/kinds/substrate.reamde.dev/observations/observation.yaml:207`);
03 §10.2's own distinction between a logged and an assumed RPE
(`research/workouts/03-progression.md:294-301`).

Cost of doing it later: every set written by the Rows screen before the
field exists carries an `rpe` nobody can classify. The field is additive on
the kind, but the data is not recoverable.

### A2. Effort targets as ranges (nested fields, permanent names)

`research/workouts/02-training-methods.md:350` says ranges "should be
allowed" (Boostcamp prescribes RPE ranges; wger has `MaxRiRConfig`), and
`02-training-methods.md:427` (requirement 8) asks for "RIR value or range,
or RPE value or range". The planned set already spells every other target
as a pair, but effort is a single value:

```yaml
# routine.yaml:322-332 and set.yaml:333-341, today
rpe: {type: decimal, min: 1, max: 10}
rir: {type: decimal, min: 0}
```

Recommended:

```yaml
rpeMin: {type: decimal, min: 1, max: 10, description: the lowest effort aimed for; equal to rpeMax for one value}
rpeMax: {type: decimal, min: 1, max: 10, description: the highest effort aimed for}
rirMin: {type: decimal, min: 0}
rirMax: {type: decimal, min: 0}
```

Same four fields in `set.target`. The app's `PlannedSet` and `SetTarget`
(`apps/apple/spotter/SpotterKit/Sources/SpotterCore/Model/PerformedSet.swift:139-176`)
and 158 example records change with it; the suggestion engine reads
`rpeMax` where it read `rpe` (a range's ceiling is the load selector,
02 §5.4).

Why now: these sit inside `slots[].sets[]` and inside `target`, and
"renamedFrom is only for a type's own property, not a field"
(`research/workouts/05-substrate-fit.md:253`). The only later path is a
second pair beside the first, with `rpe` meaning "the ceiling" by
convention. If the owner prefers that path, write the convention into
`routine.yaml` now so the agent's prompt can say it.

### A3. `set.setup`: a key with a home, not a free string

Today `set.setup` is a string ("such as a gym's name", `set.yaml:126-129`)
and the load grid lives once per exercise (`exercise.yaml:254-274`). But
03's configuration is explicitly "per exercise and setup"
(`research/workouts/03-progression.md:200`): the same chest press is a
5 kg stack in one gym and a 10 lb stack in another, and V1.1 promises
"per-gym equipment" (`proposals/087-spotter.md:210`). With one grid per
exercise the second gym needs a second exercise record, which splits the
exercise's history, muscles and codes for no reason.

Recommended: a fifth, small kind, and `set.setup` a reference to it.

```yaml
# setup.yaml (new)
kind: substrate.reamde.dev/core/kind
metadata: {id: mneme.rxtx.dev/training/setup}
data:
  authority: mneme.rxtx.dev
  package: training
  description: one station an exercise is done on, such as a gym's chest press machine
  purpose: supporting
  names: {singular: setup}
  displayTemplate: "{name}"
  properties:
    name: {type: string, required: true}
    exercise: {type: reference, kind: mneme.rxtx.dev/training/exercise, required: true, mustExist: true}
    gym: {type: string, description: where it is, such as hotel-gym}
    loadBase: {type: quantity, unit: kg, units: [kg, lb], min: {value: "0", unit: kg}}
    loadStep: {type: quantity, unit: kg, units: [kg, lb], min: {value: "0", unit: kg}}
    loadMax:  {type: quantity, unit: kg, units: [kg, lb], min: {value: "0", unit: kg}}
    notes: {type: text}
```

```yaml
# set.yaml:126, after
setup:
  type: reference
  kind: mneme.rxtx.dev/training/setup
  mustExist: true
  description: the station it was done on; absent for free weights, which share history everywhere
```

The exercise keeps its grid as the default; a setup overrides it. PREVIOUS
and the suggestion already filter on `setup`, and a reference filters
through the refs table like `exercise` does (README "Indices"). Linking
two gyms' machines later is `merge` on two setup records, the same tool the
package already relies on for duplicate exercises (`exercise.yaml:6-8`).

Minimum alternative if a fifth kind is unwanted now: keep the string but
declare `pattern: "^[a-z0-9][a-z0-9-]*$"` and document that the value is
the id a future `setup` record will take. A pattern added later is a
narrowing refused while any stored value fails it
(`substrate/docs/vocabulary.md:386-392`).

Cost of doing it later: a string cannot be retyped ("changing a property's
datatype" is refused with live records, `substrate/docs/vocabulary.md:378`),
so the reference would have to be a second property beside the string for
the life of the kind. V1 never writes `setup`
(`apps/apple/spotter/docs/DESIGN.md:720`), so the window is open until
V1.1.

### A4. Relative loads: fix the names and the meaning of `target.load`

02 §7 item 7 (`research/workouts/02-training-methods.md:425`) lists load
targets the planned set cannot say: a percentage of a reference (1RM, e1RM,
training max), effort-driven, and a band. Open question 13 asks whether
percent plans are needed. Add the fields now so the agent and an importer
(FIT `workout_step` targets, wger `MaxWeightConfig`) have somewhere to put
them, and so `target.load`'s meaning is settled:

```yaml
# routine.yaml, planned set, beside load
loadPercent:
  type: decimal
  min: 0
  max: 200
  description: the load as a percentage of loadReference; load wins when both are given
loadReference:
  type: enum
  description: what loadPercent is a percentage of
  values:
    - {value: onerm, label: Tested one-rep max}
    - {value: e1rm, label: Estimated one-rep max}
    - {value: trainingmax, label: Training max}
    - {value: bodymass, label: Body mass}
```

Rule to write into `set.yaml` and the README: `target.load` is always the
absolute load the plan resolved to when the set was done; `target.loadPercent`
and `target.loadReference` may be copied beside it so a reader knows why.
This is additive (new optional fields in an object admit,
`research/workouts/05-substrate-fit.md:249`), so **B** by mechanics, but the
reference number itself (a per-exercise, per-setup 1RM or training max)
has no home: see C6.

### B1. `sessionRPE`: keep 0 to 10, state the Apple floor

`session.yaml:276-281` allows 0 (Borg CR10 has a 0). Apple's effort score
"is a 1 to 10 quantity" (`research/workouts/04-ios-platform.md:29`), so a 0
cannot be sent. No schema change; add to README "Apple Health": "a
sessionRPE of 0 is not written as an effort score". FIT `workout_rpe` is
CR10 ×10 and Health Connect's session RPE is a 0 to 10 float, both fit.

### B2. `timeZone` spelling

`session.yaml:257-263` justifies `timeZone` as "Apple's spelling, as the
first-party Apple kinds spell it". The calendar provider does spell it
`timeZone` (`substrate/kinds/providers.substrate.reamde.dev/apple/bundle.yaml:873`),
but the closer precedent, the Apple Health provider, spells it `timezone`
(`substrate/kinds/providers.substrate.reamde.dev/applehealth/bundle.yaml:110`),
and so does core's `recurring` trait (`substrate/kinds/substrate.reamde.dev/core/core.yaml:198`).
Pick `timezone` to match the trait and the health side. Top-level on a kind
with a few hundred records a year, so `renamedFrom` works for years; cheap
now, cheap later. Low weight.

### B3. Two devices writing: revision and `ifVersion` discipline

`revised` admits an equal revision (`session.yaml:17-18`), so a watch and a
phone that both patch a set at revision 2 overwrite each other silently.
Not a schema change, but write into "How the app writes (V2)": every set
PATCH carries `ifVersion`; `revision` is raised per record, never per
device; and when a second writer appears, `set.adapter` and `set.source`
(both additive, listed under "Left out") are added so a row says which
device logged it. Decide before the watch app, not before real data.

### B4. `codes.system` content: add the standard URIs

Schema is right (FHIR `Coding` shape, as `observations/metric.codes`). The
README's Exercise mapping table (`research/workouts/substrate/training/README.md`,
"Field mapping > Exercise") lists `healthconnect`, `fit`, `hevy`, `strong`
but drops the one row 01 §7 recommended with a URI:
`http://hl7.org/fhir/uv/phr/CodeSystem/observation-pghd-codes`
(`research/workouts/01-formats-and-standards.md:900`), which is the only
place a FHIR export can code an exercise. Add it, plus `wger` and
`free-exercise-db`, so importers agree on the words. Documentation only.

### C1. Conditioning groups (EMOM, AMRAP for time, Tabata, circuits for time)

All additive. Reserve, when needed:

```yaml
# routine.groups[] and session.groups[]
groupKind: + emom, amraptime, tabata
interval: {type: duration, description: for emom and tabata, the length of one interval}
work:     {type: duration, description: for tabata, the work inside an interval}
timeCap:  {type: duration, description: for amraptime, the cap}
# session.groups[] only
roundsDone: {type: int, min: 0}
repsDone:   {type: int, min: 0, description: the reps into the unfinished last round}
```

A set's round inside a circuit is its `ordinal`, so no `round` field. FIT
has `amrap`, `emom` and `tabata` sub-sports
(`research/workouts/02-training-methods.md:146`), so the names map on import.

### C2. Cardio machines in a session

`measurement: distanceduration` covers a rower or a treadmill, but a set
has nowhere for the machine's own numbers. Additive on the set when wanted,
spelled as `health/workout` spells them: `activeEnergy` (quantity, kJ),
`averageHeartRate` (quantity, 1/min), plus `level` (int) and `incline`
(decimal, percent). Keep heart-rate streams out: they are the Health
workout's traces.

### C3. Bands, partial reps, tempo on the record

- `band` (string, the colour or label) on the planned set and the set.
  02 §9 item 9 puts it in notes; a field makes it comparable. Additive.
- `partialReps` (int, min 0) on the set, beside `reps`. Additive.
- `tempo` is planned only and not copied into `target`
  (`set.yaml:289-385` has no `tempo`); add `target.tempo` and
  `target.timerMode` when a history view wants them. Additive.

### C4. Programs above routines

`folder` and `position` are display grouping (`routine.yaml:91-98`). A
mesocycle needs more: a `program` kind (name, weeks, deload week, the
progression scheme) with `routine.program` (reference) and `session.week`
(int). All additive; the folder string stays the display grouping. The
progression scheme per slot (double progression bounds, linear increment)
is also additive on `slots[]`. Do not model it before the owner's plans
need it, but do not grow `folder` into it either.

### C5. Coach or agent proposed changes

No schema needed. Substrate's reviewed write is `core/propose`, which lands
a `substrate.reamde.dev/core/recordpatchrequest` the owner accepts
(`substrate/kinds/substrate.reamde.dev/core/recordpatchrequest.yaml:1-30`,
`substrate/docs/agents.md:321-324`). "Propose in-session edits back to the
routine" (open question 35) is a patch request on the routine from Spotter's
token; the frozen envelope is the audit trail.

### C6. PRs, 1RMs and derived values

Open question 31 (compute in clients) holds for V1 and V2. When a reference
number is needed for A4, or a chart wants rollups, the first-party-aligned
path is observations: a function writes `training/e1rm` and
`training/training_max` metrics (`observations/metric` records with LOINC
none, `codes` empty) as `observation` rows with `about` the owner, a
`channel` of the exercise id, and `derivedFrom` the sets
(`substrate/kinds/substrate.reamde.dev/observations/observation.yaml:214`).
That buys the observed index and its rollups for free and keeps derived
values off the training kinds, which Mneme's kind-design forbids anyway
(README "Left out").

### C7. Skipped planned sets and `abandoned`

Both additive, as the README says. If the owner wants them: a `skipped`
state on nothing (no set record is written for a skipped set) but a
`session.slots[].sets` snapshot (level 2, allowed by the nesting cap) at
Start, so a reader can diff planned against done. That is the same
snapshot V2 wants for a second device.

### C8. Imports

The shapes fit: ids from upstream keys, `origin`, `sourceType`, `rpeScale`,
`session.source`. Two small additions when the first importer is written:
`routine.codes` (so a Hevy `routine_id` can link, README "Left out") and
nothing else. For a CSV import with no upstream record, `origin` stays
empty and the id carries the key, which is what `clinical` does too
(`substrate/docs/clinical.md`, "Ids are the upstream's").

## What should stay as is

- **One record per set, chains for structures** (`set.yaml:1-39`). The
  only shape the server can filter and aggregate; matches FIT `set`,
  Health Connect `ExerciseSegment`, Hevy and Strong rows one to one.
- **Effort as entered, RIR derived at read, `rpeScale` by vendor**
  (`set.yaml:41-45`, research README disagreement 1). The vendor tag is
  right: Hevy and Strong agree on 9.5, Boostcamp does not
  (`research/workouts/02-training-methods.md:326-328`), so the scale is a
  vendor fact. Adding a vendor is additive.
- **Load as a quantity in the unit entered, `loadMode` beside it,
  assistance positive** (`set.yaml:47-51`). Substrate units carry UCUM
  (`[lb_av]`), so this is UCUM-aligned without saying so. `distance` with
  `unit: m` and no `units` list admits km and mi already.
- **`codes` shaped as `observations/metric.codes`, system and code
  required** (`exercise.yaml:230-250`). FHIR `Coding` field for field.
- **First-party spellings**: `about`, `source`, `origin`, `revision`,
  `adapter`, `utcOffsetStart`/`End`, `name`, `notes` as `text`,
  `duration`, `distance`, `bodyMass` (the `health/body_mass` metric's
  name). A later move to a first-party catalogue is a row copy.
- **`activityType` as HealthKit's enum, spelled by the enum rule**
  (`routine.yaml:113-121`). It is what the Health save needs; the
  cross-source sport code lives on the linked `health/workout.sport`.
- **Planned-set `key` required; `target.plannedSet` a key, not a
  position** (`routine.yaml:47-51`, `set.yaml:294-300`). Correct given that
  substrate serves no record at an older version.
- **`mustExist` on every exercise reference; `set.session` and
  `set.continues` cascade; `session.routine` without `mustExist`.**
- **Group values stored once per group; absent rest is not zero; no
  declared defaults on rests** (`routine.yaml:38-62`). The Hevy failure
  (02 §3.4) is avoided and the owner's choice stays distinguishable from
  the engine's default.
- **No declared indices** (README "Indices"). The EXPLAIN evidence is
  specific and a declared index later touches no record.
- **`requires: substrate.reamde.dev/health`** for the `workout` pin. A
  `kind: any` pin would avoid the dependency, but the health catalogue is
  already on `geoah.me` (Apple Health rows land there) and the typed pin
  is worth more than the freedom.
- **`sessionRPE` as CR10, named apart from the sets' `rpe`.** Right scale
  for Apple's effort score, FIT `workout_rpe` and Health Connect.
- **Measurement, equipment and muscle enums from Hevy's vocabulary.** The
  owner's words, and `codes` carries the others.
- **Discard purges; no `abandoned` yet; status moves only by PATCH.**

## Errors and gaps in the README's mapping tables

`research/workouts/substrate/training/README.md`, section "Field mapping".

1. **Set table, `order` ↔ Health Connect `setIndex (alpha)`.** `setIndex`
   identifies a *set*, "several segments may share a set index", and it
   may restart per exercise (`research/workouts/01-formats-and-standards.md:448-452`).
   So a drop chain's records share one `setIndex`. It maps to the chain
   head's position in its slot (the head's `order`, or `ordinal` with
   warm-ups counted), not to each record's `order`. On export, every record
   of a chain writes its head's index.
2. **Set table, `order` ↔ FIT `message_index`.** `message_index` numbers
   `set` messages across the whole file, not within an exercise. An export
   renumbers; an import derives `order` from message order within a
   category run. Say so.
3. **Session table, `sessionRPE` ↔ HealthKit `workoutEffortScore`.** The
   effort score is 1 to 10 (`research/workouts/04-ios-platform.md:29`); the
   property admits 0. Note the floor (B1).
4. **Session table, `id` ↔ Strong `Workout #`.** Strong's `Workout #` is a
   sequence number assigned at export (the "Exporting" section says so);
   in the `id` row it reads as an identifier an importer could key on. It
   cannot: a re-export renumbers. Move it to its own row or mark it
   "export only".
5. **Session table, `revision` ↔ Hevy `updated_at`.** A timestamp is not a
   monotone integer; an importer cannot write it into `revision` and must
   count its own. Mark it "derived".
6. **Set table, `rpe` ↔ Health Connect segment `rateOfPerceivedExertion`.**
   The prose under the tables says a Health Connect set RPE is CR10, but
   the row does not, while the `target.rpe` row does say "(int, CR10)".
   Add "(float, CR10)" to the row for consistency.
7. **Exercise table, `codes`.** Missing the PHR code system URI, `wger` and
   `free-exercise-db` (B4).
8. **Session table, `timeZone` ↔ Health Connect none.** Correct, but the
   kind comment's claim about first-party spelling is half right (B2).

Nothing else in the tables contradicts the research documents or the
substrate sources I checked.

## The RPE provenance recommendation

### What changes with the Rows screen

Today the engine's rule is clear: "`logged` is true only in case 1. The
explanation shows 'at RPE x' only when logged"
(`research/workouts/03-progression.md:294-301`), and V1 enforces it by
construction: "RPE is never ghosted"
(`apps/apple/spotter/SpotterKit/Sources/SpotterCore/Present/Ghost.swift:5`),
so `set.rpe` is "what the owner tapped" (`set.yaml:42`). The plan's RPE
already travels on the record as `target.rpe` (`Rows.swift:193` copies
`planned.rpe` into the row's target).

The Rows screen pre-fills `target.rpe` into the RPE cell and one tap logs
the set with it. After that, a record with `rpe: "8"` and `target: {rpe:
"8"}` can mean either "the lifter rated it 8" or "the lifter did not rate
it". In the owner's own example sessions 10 of 17 rated sets already carry
an `rpe` equal to the plan's (`research/workouts/substrate/examples/sessions.yaml`,
counted by `target.rpe`), so the ambiguity would cover most of the history.

It matters in three places:

- **The suggestion.** 03 §10.2 case 3: a set that fell short of `repsMin`
  with no RPE is assumed to have gone to failure (RPE 10). A pre-filled 8
  accepted on a short set turns that into "RPE 8 at fewer reps", which
  lowers the e1RM and cuts the load next time for a set the lifter may
  simply not have rated. 03 §13 adds "RPE logged on a set that fell short:
  the logged RPE wins" (`03-progression.md:493`), so the engine would trust
  it. Boostcamp's rule for the same screen is the right one: "a logged RPE
  above the prescribed RPE holds the weight; leaving RPE blank never holds
  it" (`03-progression.md:156`). A pre-fill must count as blank.
- **The explanation string** ("Last time 35 kg × 13 at RPE 9.5") would
  quote a rating nobody gave.
- **Exports and later readers** (Hevy `rpe`, a FHIR 82289-0 component, an
  "average RPE" chart) would report plan values as perceived exertion.

### Options weighed

1. **Store nothing new; write `rpe` only when the sheet was touched.**
   Keeps the kind's own principle ("as entered") and the engine unchanged;
   the history view shows `target.rpe` in grey for an unrated set. It
   loses the fact that the lifter saw an 8 and did not object, which is
   weak but real evidence, and it makes the Rows screen's "logged with the
   plan's RPE" a fiction on the record.
2. **Store `rpe` plus a provenance marker.** Keeps the record faithful to
   what the screen showed and confirmed, lets the engine treat the two
   cases differently, and lets the policy change later without losing
   data (a retroactive rule needs the marker, not the other way round).
3. **A general `prefilled` list** of every field committed from the ghost
   unchanged (`load`, `reps`, `rpe`, …). More information, one property,
   but the question readers ask is "did a human rate this?", which wants a
   plain `eq` filter, and a ticked load is a fact the lifter confirmed
   about the bar, where a ticked RPE is a judgement they may never have
   formed. Keep the general list as a later option if the outlier guard
   wants to know which loads were typed.

Recommendation: option 2.

### The concrete model

```yaml
# set.yaml, after rpeScale (set.yaml:235-242)
rpeMethod:
  type: enum
  description: how rpe got its value, written whenever rpe is; entered when
    the lifter chose it, prefilled when the plan's target was shown and
    left unchanged at the tick
  values:
    - {value: entered, label: Rated by the lifter}
    - {value: prefilled, label: Pre-filled from the plan, unchanged}
```

Rules to write beside it:

- **Write rule (app).** Whenever `rpe` is written, `rpeMethod` is written.
  Opening the RPE sheet and confirming the same value counts as `entered`
  (the lifter looked). Changing the value is `entered`. A one-tap log with
  the cell untouched is `prefilled`. A set with no plan RPE and no tap has
  neither field. Imports write `entered` (the source's user rated it) and
  keep `rpeScale` as now. V1's local history predates the field and was
  written only when tapped (`Ghost.swift:5`), so its migration stamps
  `entered`.
- **`target.rpe` is unchanged**: the plan in force. A `prefilled` record
  therefore has `rpe == target.rpe` by construction; the duplication is
  deliberate so a filter on `rpe` still works and the screen's value is
  what the record says.
- **Read rule (03 §10.2, README "Reading").** `logged` is true only when
  `rpeMethod` is `entered`. A `prefilled` RPE is read exactly as an absent
  one: a set that met its target is at `target.rpe` (which is the same
  number), a set that fell short is assumed to have reached failure, and
  the explanation never says "at RPE x". `effectiveRPE` in
  `SpotterCore/Present/SuggestionInputs.swift:205` and `Suggest/Rules.swift:75`
  are where the check lands.
- **Chain folding.** "The set's RPE is the highest RPE logged on any of
  its records" (README "Reading") becomes the highest `entered` RPE; if
  none is entered, the folded set is unrated.
- **Exports.** Hevy, Strong and a FHIR 82289-0 component take `entered`
  RPE only. The Health effort score is the session's, unaffected.
- **Filters.** "Every set the lifter rated": `rpeMethod eq entered`.
  "Rated or not": `rpe exists`.

Why not a bool `rpePrefilled`: an enum takes a third value additively
(`suggested`, if the engine ever pre-fills a predicted RPE rather than the
plan's), a bool does not.

Severity A, because a set written without the marker cannot be classified
afterwards; the field itself is additive and costs nothing to declare now.

## Substrate fit and evolution, checked

- **Narrowing refusals** (`substrate/docs/vocabulary.md:371-427`): the
  package adds no `required` outside identifying keys, no index, and
  bounds that only loosen later (`target.rpe min: 1` versus `set.rpe min:
  0` is inconsistent but harmless, since lowering a `min` widens). The two
  patterns (`tempo`, `healthKitWorkout`) can be loosened later as long as
  every stored value still matches. The `tempo` pattern refuses two-digit
  phases ("10-1-1-0"); loosen when it bites.
- **Conversion ceiling.** About 5,000 sets a year against a 10,000-record
  ceiling (`research/workouts/05-substrate-fit.md:259-263`): top-level set
  properties are renameable for roughly two years; `target.*` and the
  session's `slots[]`/`groups[]` never are. That is why A1 to A4 are
  ordered as they are.
- **References and cascades.** `set.session` and `set.continues` cascade
  and are top-level, as they must be; `exercise.variationOf` has no
  `onDelete`, which is right (a deleted parent leaves the child). A merged
  exercise still resolves through the refs index.
- **Ids.** UUIDv7 on the phone, slugs for catalogue kinds, upstream keys
  for imports; none may be a `recordmapping` target. Fine. If A3 lands,
  setup ids are slugs chosen by the writer, like exercises.
- **Package requirement.** `health` is required for one typed pin;
  `observations` is seeded. Fine.
- **Decide before data versus later.** Before: A1 to A4 and the `setup`
  datatype. Later and additive: every C item, `abandoned`, the plan
  snapshot on the session, `set.adapter`/`set.source`, `routine.codes`,
  `folderPosition`, new enum values, new units in `units:` lists.

## Evidence index

- Package: `research/workouts/substrate/training/{bundle,exercise,routine,session,set}.yaml`
  and `README.md` (decisions, mapping tables, "Reading", "Indices").
- Requirements and research: `research/workouts/README.md` (reqs 5 to 11,
  disagreements 1 and 2, open questions 13, 21, 31, 32, 35);
  `01-formats-and-standards.md` §2.3 to §2.7, §3.3, §3.4, §7, §8.4, §9;
  `02-training-methods.md` §2.12, §5.2 to §5.5, §7 items 7, 8, 18;
  `03-progression.md` §9.1, §10.2, §13; `04-ios-platform.md:29`;
  `05-substrate-fit.md` §1.8, §3.4.
- App: `SpotterCore/Present/Ghost.swift:5`, `Engine/Rows.swift:193`,
  `Present/SuggestionInputs.swift:131-147,205`, `Suggest/Rules.swift:75`,
  `Model/PerformedSet.swift`, `docs/DESIGN.md:720`.
- Substrate: `docs/vocabulary.md:334-520`, `docs/traits.md:264-270`,
  `docs/units.md`, `kinds/substrate.reamde.dev/health/workout.yaml`,
  `observations/metric.yaml:42-59`, `observations/observation.yaml:128-225`,
  `core/core.yaml:198`, `core/recordpatchrequest.yaml`,
  `providers.substrate.reamde.dev/applehealth/bundle.yaml:110`,
  `providers.substrate.reamde.dev/apple/bundle.yaml:873`.
- Standards, as recorded in research 01 with their URLs: HL7 PHR IG PGHD
  Workout profile (`build.fhir.org/ig/HL7/personal-health-record-format-ig`),
  LOINC 82289-0, Health Connect `ExerciseSegment`/`PlannedExerciseSessionRecord`
  (androidx `connect-client` 1.2.0-alpha06), FIT profile 21.218.0
  (`set`, `session.workout_rpe`, `workout_step`), Hevy OpenAPI, IEEE 1752.1
  `physical-activity`, schema.org `ExercisePlan`.
