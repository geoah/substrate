# Substrate Workouts: design and build spec

Date: 2026-10-30. Carried into this repository on 2026-11-01 with the app.

Substrate Workouts is an iPhone app (iOS 27, Liquid Glass) for strength sessions in the gym. It lists the owner's workout
routines, starts a training session from one or from nothing, runs it (exercises in any order, supersets, warm-ups, drop
sets, myo-reps, timed sets, RPE, rest timers that alert when rest ends, next-set suggestions) and saves each finished
session to Apple Health as one strength workout.

This is the spec of the app's first build, written before it moved here, when it ran on bundled data alone ("V1") and
named its later sync "V2". The sync is built: [README.md](README.md) says how the app signs in, keeps its training
package and writes through its outbox, and the seams of §2.5 are implemented by WorkoutsKit's `WorkoutsSync`. Sections
9.8 to 9.10 and 11 describe that first build's project and release, which `apps/apple/project.yml` and
[`apps/apple/README.md`](../README.md) replace. Where a rule comes from the research behind the app, the source is given
in brackets ("req 23" is requirement 23 of the research's README; "03 §11.2" is section 11.2 of its progression study);
the research stays with the app's history outside this tree, and nobody needs it to build.

The data shapes are the training package's, beside this file in [`Package/`](Package/README.md). Where this document and
those files disagree about a property, the kind files win and this document is wrong.

## Contents

1. [Scope and decisions](#1-scope-and-decisions)
2. [Architecture](#2-architecture)
3. [Model and record codec](#3-model-and-record-codec)
4. [Session engine](#4-session-engine)
5. [Suggestions, PREVIOUS and ghost values](#5-suggestions-previous-and-ghost-values)
6. [The app layer](#6-the-app-layer)
7. [Screens](#7-screens)
8. [Liquid Glass](#8-liquid-glass)
9. [Platform](#9-platform)
10. [Test plan](#10-test-plan)
11. [Work split and order](#11-work-split-and-order)
12. [Questions that still touch V1](#12-questions-that-still-touch-v1)

## 1. Scope and decisions

### 1.1 What V1 does

- Lists the owner's workout routines by folder, previews one, and starts a session from it or from nothing.
- Runs one session at a time: any set can be ticked in any order, with an Up next pointer.
- Supports warm-ups, working sets, failure and AMRAP sets, top and back-off sets, drop sets, myo-reps, myo-rep match,
  rest-pause and cluster sets (the last two only when a routine asks for them), timed holds, distance work, one side at
  a time, supersets, tri-sets, giant sets, circuits and paired sets.
- Picks every rest from the seven rest slots (§4.7), counts it down from an absolute end time, offers −15 s, +15 s and
  Skip, and alerts with a time-sensitive notification when the phone is away.
- Shows a Live Activity on the Lock Screen and in the Dynamic Island with ±15 s, Skip and Done.
- Suggests the next set's load and reps by research 03 Part B, shown as grey ghost values with a one-line reason.
- Edits the session on the fly: add, remove, replace, reorder and group exercises; add and remove sets; change a set's
  kind; change rests; notes.
- At Finish: one dialog for unticked sets, editable start and end, an optional session RPE, a summary, then a local
  save and an Apple Health workout that never blocks Finish.
- History of finished sessions, and per exercise a history list with an estimated one-rep maximum (e1RM) trend.
- Settings: units, rest defaults, keep awake, Apple Health access, a note on where the data comes from, and the shared
  updater's `BuildSection`.

### 1.2 What V1 does not do

| Not in V1                                                       | When                                       |
| --------------------------------------------------------------- | ------------------------------------------ |
| Reading or writing substrate (client, outbox, change feed)      | V2, after the owner applies the package    |
| AlarmKit "loud rest alarm"                                      | V1.1 (the coordinator keeps a hook)        |
| Warm-up generator (percentage ramp)                             | V1.1; V1 copies last time's warm-ups       |
| Personal records, banners, medals                               | V1.1                                       |
| Plate calculator, gym equipment profiles, setup keys            | V1.1                                       |
| Editing a finished session (other than deleting it)             | V1.1                                       |
| Re-saving the Apple Health workout after an edit                | Not before a device test and owner answer  |
| Writing `substrate.reamde.dev/health/workout`                   | Never                                      |
| Routine editing in the app                                      | Later; the console's agent writes routines |
| Watch app (the Live Activity already shows on the watch)        | Later                                      |
| Imports (Hevy, Strong, FIT) and exports                         | Later                                      |
| EMOM, AMRAP for time, Tabata                                    | Later                                      |
| An iPhone `HKWorkoutSession`, heart rate, energy                | Later, if ever                             |
| Rest-pause and cluster sets from the set-kind menu (not a plan) | Later; they run when a routine asks        |

### 1.3 Decisions from the lead

Recorded as decisions the owner may overturn; this spec does not reopen them.

- **Package.** `~/training`, version 1, the app's own ([Package/](Package/README.md)). Kinds `exercise`, `routine` (label "Workout routine"), `session` (label
  "Training session"), `set`. A routine folder is a string plus a position on the routine.
- **One record per performed set.** The session embeds its ordered slots; group values are stored once per group.
- **Effort.** `rpe` as entered (a decimal on the RIR-based scale, half steps), optional `rir`, a scale tag for imports.
  No rating is valid.
- **Load.** A quantity in the unit entered, kg by default, lb allowed; a load mode beside it; assistance as its own
  positive quantity.
- **Rest.** The rest slots of 02 §3: set rest, warm-up rest, transition per group member, round rest per group,
  intra-set pause, side switch, per-set override. Defaults from 02 §4.3 and §4.4 when the routine says nothing; warm-up
  rest 45 s.
- **Myo-reps.** The method author's stop rule by default (stop when a mini-set loses a rep from the first, or after
  five), with target, cap and total configurable in data; every mini-set is its own set record in a chain. **Drop sets.** Chains, no rest inside, 20 % default drop rounded down.
- **Suggestions.** 03 Part B exactly: the load is cut below the range; an in-session raise is capped at one step or
  10 %. PREVIOUS is the last time the same exercise was done anywhere, with the same setup key.
- **Session RPE.** Optional, asked at Finish, skippable. **AlarmKit** V1.1. **Keep awake** a setting, on by default.
- **Apple Health.** `HKWorkoutBuilder` at Finish, `traditionalStrengthTraining` unless the routine says otherwise,
  indoor, `HKMetadataKeyExternalUUID` and `HKMetadataKeySyncIdentifier` set to the session id, no energy, no re-save
  after an edit. the app never writes `health/workout`; the session stores the HealthKit UUID (and in V2 the reference
  to the `health/workout` row the Apple Health adapter builds).
- **V1 data.** The app ships with the example records bundled and keeps everything on the phone. Live substrate sync
  is V2. V1 still implements the model-to-record encoding, proven by tests.

### 1.4 Decisions taken in this spec

Also open to the owner. Each is explained where it lands.

| Decision                                                                                                                                                                  | Where |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----- |
| Four tabs: Routines, History, Exercises, Settings. The scaffold's Settings tab stays; req 36 had three tabs.                                                              | §7.1  |
| The bundled exercises and routines ship; the bundled example sessions are test and preview data only, never the owner's history (the examples README's caution).          | §2.4  |
| The RPE keypad offers 6 to 10 in half steps, 6.5 included, plus Clear.                                                                                                    | §7.6  |
| A rest timer runs after an exercise's last set too ("Rest before next exercise"), a setting on by default.                                                                | §4.7  |
| Segments whose number is known (drops, clusters, the second side) are shown from the start; myo, myo-match and rest-pause mini-sets appear one at a time.                 | §4.9  |
| Timed sets count down to the target by default, keep counting past zero, and do not complete themselves; Stop completes the set.                                          | §4.3  |
| Pausing the session clock does not pause a running rest.                                                                                                                  | §4.3  |
| The Live Activity's Done completes the Up next set with its ghost values.                                                                                                 | §9.1  |
| The owner can add an exercise the catalogue lacks; it lives on the phone (V2 writes it to substrate first).                                                               | §7.6  |
| Finish drops exercises with no performed set, as the examples assume.                                                                                                     | §4.3  |
| Body mass: the latest Apple Health sample at Start, else a value typed in Settings, else none.                                                                            | §9.5  |
| A cold launch with a session in progress opens the session screen.                                                                                                        | §6.3  |
| No `HKMetadataKeyWorkoutBrandName` in V1 (open question 24).                                                                                                              | §9.5  |
| Rest measurement: the rest before a set ends at Skip, a timed set's start, the first touch on the next set after its countdown ended, or the tick, whichever comes first. | §4.8  |
| Rest-pause and cluster mini-sets, back-off loads and `rirstop` sets use small rules of the app's own around 03 Part B, which does not cover them.                         | §5.3  |

## 2. Architecture

### 2.1 Targets and modules

```text
apps/apple/
+-- WorkoutsKit/                   Swift package, Foundation only, `swift test` on the Mac
|   +-- WorkoutsCore               one module: model, record codec, session engine, rest rules,
|                                 suggestion engine, presentation, file stores, V2 seams
+-- the app/                      the app target: SwiftUI, ActivityKit, UserNotifications, HealthKit
|   +-- Shared/                   compiled into the app AND the widget extension
+-- SubstrateWorkoutsWidgets/               widget extension: the session Live Activity, nothing else
```

- **WorkoutsCore imports Foundation only.** No SwiftUI, UIKit, ActivityKit, AlarmKit, HealthKit, UserNotifications or
  network code. It builds for iOS, watchOS and macOS (the scaffold's `Package.swift` platforms). Every public type is
  `Sendable`; no type is actor-isolated. A watch app later links the same module.
- **The app target** owns the platform: the session controller (the single writer), the countdown coordinator,
  notifications, the Live Activity, HealthKit, haptics, keep-awake, and every view. Its code runs on the main actor
  (`SWIFT_DEFAULT_ACTOR_ISOLATION: MainActor`, already in `project.yml`).
- **The widget extension** renders the Live Activity from its content state. It does not link WorkoutsKit: every string
  it shows arrives preformatted in the content state. Its buttons are `LiveActivityIntent`s whose bodies run in the
  app's process (§9.1).
- **No App Group.** The widget reads only the content state, and intents run in the app.

### 2.2 Data flow

```text
 views, Live Activity intents, notification taps
        |  SessionCommand
        v
 SessionController (app, @MainActor, the only writer)
   1. mint ids and timestamps, build the SessionEvent
   2. SessionEngine.apply(event, state)  -> new SessionState   (WorkoutsCore, pure)
   3. SessionJournal.write(events, state)  atomic file write   (WorkoutsCore)
   4. CountdownCoordinator.sync(state)  -> notification, Live Activity   (app)
   5. SessionPresenter.present(state, catalog, history, settings, now) -> SessionPresentation (WorkoutsCore, pure)
        |
        v
 SwiftUI views render the presentation; Live Activity content is built from it

 Finish:  SessionEngine.records(state) -> TrainingSession + [PerformedSet]
          RecordCodec.encode -> flat records -> HistoryStore (one file per session)
          HealthExportQueue.enqueue(session id) -> HealthExporter (never blocks Finish)
```

- The UI never computes rules. It sends commands and renders `SessionPresentation`.
- Suggestions are re-run by the presenter after every change and never written into the state. The tick commits the
  values the row shows (§5.5).
- Every network-shaped seam is a protocol in WorkoutsCore with a local V1 implementation (§2.5).

### 2.3 Persistence

All files live under `Application Support/Workouts/`, created on first use and included in the device backup. Every write
is `Data.write(to:options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])`. The
protection class matters: a Live Activity button runs app code while the phone is locked, and `.complete` would make
the file unreadable then (04 §4.1). WorkoutsCore's file stores take the writing options as a parameter so the Mac tests
can pass `.atomic` alone.

| File                        | Holds                                                                                                                    | Written                                                                       |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------- |
| `active-session.json`       | The session in progress: `{"format": 1, "events": [...], "state": {...}}`. Absent when no session is open.               | After every applied event, before any `await`. Deleted at Finish and Discard. |
| `history/<session id>.json` | One finished session: `{"format": 2, "records": [session flat record, set flat records...], "local": {"health": ...}}`.  | At Finish; again when the Health save returns; deleted on Delete.             |
| `health-queue.json`         | Session ids whose Apple Health save has not succeeded yet, with the last error. One that no longer decodes is set aside. | On enqueue, success, failure.                                                 |
| `local-exercises.json`      | Exercises the owner added on the phone, as flat exercise records.                                                        | On add.                                                                       |
| `UserDefaults`              | Settings (§7.10), the set of session ids whose Live Activity the owner dismissed.                                        | On change.                                                                    |

**The journal.** The events are the truth; the stored state is a cache. On load, the controller folds the events and
compares the result with the cached state. If they differ (a bug, or a format change), it keeps the fold, logs the
difference with `os.Logger`, and rewrites the file, best effort: a rewrite that fails (a full disk) keeps the session
open and the next event writes the file again. A test asserts `fold(events) == state` after every event of every
engine test (§10.1). A session file is tens of kilobytes; rewriting it whole on every change is cheap.

A journal that does not load (another `format`, an event that no longer decodes, a fold the engine refuses) is moved
aside as `active-session.unreadable-<seconds since 1970>.json`, where the next Start cannot write over it, and listed
in Settings' "Records that could not be read". A file the phone could not read at all (locked, an I/O error) stays.
Because an update can arrive mid-session, a change to an event's `Codable` shape must decode the old shape: new fields
are optional or decode with a default; anything else raises `journalFormat` and ships a migration. A renamed field
decodes from its old name: a Start event's routine written before effort targets became ranges holds a planned set's
`rpe` or `rir`, which `PlannedSet`'s decoder reads as a range of one value, so the resumed session keeps its targets. A failed journal
write is not silent either: the cover shows "Changes are not being saved on this iPhone" until a write succeeds, and
the Finish failure alert no longer says nothing was lost.

**The history store.** At launch it reads every file in `history/` into memory (a few hundred files of about 10 KB)
on a background task, decodes them with the record codec, and builds the per-exercise index the suggestion engine and
PREVIOUS read (§5.2). A file that fails to decode is skipped and reported in Settings, never deleted. A format 1
file, written before the data-model review, is read through `HistoryMigration.fromFormat1`: the session's `timeZone`
becomes `timezone` and a the app session gets `sport: strength`; a set target's `rpe` and `rir` become ranges of one
value; an `rpe` or `rir` with no method is `entered`, since V1 stored one only when it was tapped. Nothing V1 did not
record is guessed (the load meaning, the completion method and the time basis stay absent; readers fall back to the
exercise). The next save writes the file as format 2.

### 2.4 Bundled data and the generator

In this repository the bundled and fixture JSON are the source, and `apps/apple/scripts/workouts-package.py` writes
only the package's manifests and `kinds.json`; the generator below is the first build's.

The examples in `research/workouts/substrate/examples/` are YAML in `substratectl apply -f` form. The app bundles them
as JSON in the shape substrate's REST API speaks, the flat record:

```json
{"kind": "~/training/exercise", "id": "chest-press-machine", "properties": {"name": "Chest Press (Machine)", "...": "..."}}
```

A checked-in script turns the YAML into that JSON:

```bash
cd apps/apple
uv run --with pyyaml scripts/examples.py            # rewrite the generated files
uv run --with pyyaml scripts/examples.py --check    # exit 1 and name each stale file
```

`scripts/examples.py` rules:

- It finds the repository root by walking up from its own path to the directory holding `research/`, and reads
  `research/workouts/substrate/examples/{exercises,routines,sessions,coverage}.yaml` and
  `research/workouts/substrate/training/{exercise,routine,session,set}.yaml`.
- It loads YAML with a subclass of `yaml.SafeLoader` whose implicit timestamp resolver is removed, so a datetime stays
  the string it is written as. It fails on any Python `float`, `date` or `datetime` in a record: substrate writes
  decimals, quantities' values and instants as strings, and a bare float would already be rounded.
- Each YAML document with `kind`, `metadata.id` and `data.properties` becomes `{"kind", "id", "properties"}`, in file
  order. A document that is only comments is skipped; any other shape fails the run.
- Each output file is `{"source": "<repo-relative YAML path>", "records": [...]}`, written with
  `json.dumps(..., ensure_ascii=False, indent=2)` and a final newline. No timestamps, so the output is deterministic.
- It also writes `kinds.json`: for each of the five kinds (`setup` included), its traits and every property name, with each object
  property's field names (nested), read from the kind YAML. The codec drift test reads it (§10.4).

| Output                                                                                            | Contents                                 | Used by                            |
| ------------------------------------------------------------------------------------------------- | ---------------------------------------- | ---------------------------------- |
| `WorkoutsKit/Sources/WorkoutsCore/Resources/Bundled/exercises.json`                                 | 30 exercises (`exercises.yaml`)          | The app's catalogue                |
| `WorkoutsKit/Sources/WorkoutsCore/Resources/Bundled/routines.json`                                  | 4 routines (`routines.yaml`)             | The app's catalogue                |
| `WorkoutsKit/Tests/WorkoutsCoreTests/Fixtures/examples/{exercises,routines,sessions,coverage}.json` | All 163 records                          | Codec, engine and suggestion tests |
| `WorkoutsKit/Tests/WorkoutsCoreTests/Fixtures/kinds.json`                                           | Property names per kind                  | The codec drift test               |
| `the app/Preview Content/PreviewHistory.json`                                                     | `sessions.yaml`'s 3 sessions and 65 sets | SwiftUI previews only              |

**What ships and what does not.** The exercises and routines of the first two files are the owner's and fit to ship.
The sessions are test vectors whose sets are mostly invented; loaded as history they would put invented loads into
PREVIOUS and every suggestion. So the app's history store starts empty, `sessions.json` and `coverage.json` exist only
in the test bundle, and the preview copy sits in `the app/Preview Content`, which `DEVELOPMENT_ASSET_PATHS` keeps out of
archived builds (§9.8). `coverage.yaml`'s exercises and routine are test data too and are never bundled.

### 2.5 The seam where V2 plugs in

V2 adds a second WorkoutsKit target, `WorkoutsSync` (Foundation only): the substrate client (copied from Substrate
Sync's `Client.swift` as research 05 §6 lists), an ordered outbox persisted next to the journal, a write planner that
maps engine events to `PUT`, `PATCH` and `DELETE` (the package README's "How the app writes (V2)" table), and a
change-feed reader. V1 defines the protocols it will implement and ships local implementations:

```swift
/// Exercises and routines. V1: the bundled JSON plus local-exercises.json.
/// V2: substrate lists, cached on disk, refreshed from the change feed.
public protocol CatalogSource: Sendable {
    func load() async throws -> Catalog
    /// A new catalogue whenever the source changes. V1 yields only after a local exercise is added.
    func updates() -> AsyncStream<Catalog>
}

/// Told about every change the session controller applies, in order.
/// V1: `NoSync`, which does nothing. V2: the write planner, which turns each into outbox entries.
public protocol SessionSyncSink: Sendable {
    func applied(_ event: SessionEvent, state: SessionState) async
    func finished(_ records: SessionRecords) async
    func discarded(_ sessionID: SessionID) async
    func healthLinked(_ sessionID: SessionID, healthKitWorkout: String, revision: Int) async
    func exerciseAdded(_ exercise: Exercise) async   // V2 PUTs it before anything names it
}

/// Finished sessions. V1: the local history files. V2 adds sets from substrate (imports, other devices).
public protocol HistorySource: Sendable {
    func sessions() async throws -> [SessionRecords]
}
```

The controller calls the sink after the journal write and never awaits it on the UI path. Kind names come from one
place, `KindNames` (§3.1), so a later move to a first-party catalogue is one change. V2 also fills
`session.workout` (V1 leaves it empty: finding the Apple Health account needs the server, package README "Apple
Health").

### 2.6 Source layout

```text
apps/apple/
  project.yml                         targets, entitlements, Info keys (§9.8)
  scripts/examples.py                 the generator (§2.4)
  docs/DESIGN.md                      this file
  WorkoutsKit/
    Package.swift
    Sources/WorkoutsCore/
      Model/        Values.swift Exercise.swift Routine.swift Session.swift PerformedSet.swift IDs.swift UUIDv7.swift
      Codec/        KindNames.swift FlatRecord.swift JSONValue.swift RecordCodec.swift ISODuration.swift DecimalText.swift
      Engine/       SessionState.swift SessionEvent.swift SessionEngine.swift Rows.swift Chains.swift UpNext.swift
                    Ordinals.swift Counting.swift Records.swift
      Rest/         RestRules.swift RestDefaults.swift
      Suggest/      Suggest.swift Curve.swift Grid.swift Evidence.swift Rules.swift Texts.swift Plausibility.swift
      Present/      SessionPresenter.swift SuggestionInputs.swift Previous.swift Ghost.swift Labels.swift Columns.swift
        Rows/       RowsLayout.swift RowsEditor.swift RowsPresenter.swift RowsText.swift      (§7.6a)
      Format/       Formatters.swift
      Store/        SessionJournal.swift HistoryStore.swift HistoryIndex.swift HealthQueueFile.swift LocalExercises.swift
      Seams/        CatalogSource.swift SessionSyncSink.swift HistorySource.swift BundledCatalog.swift
      Resources/Bundled/  exercises.json routines.json          (generated)
    Tests/WorkoutsCoreTests/
      Fixtures/examples/*.json  Fixtures/kinds.json             (generated)
      CodecTests.swift  EngineTests.swift  RestRuleTests.swift  UpNextTests.swift  ChainTests.swift
      SuggestVectorTests.swift  ExampleProgressionTests.swift  PresenterTests.swift  FormatTests.swift
      JournalTests.swift  HistoryStoreTests.swift  UUIDv7Tests.swift  RowsPresenterTests.swift
  the app/
    SubstrateWorkoutsApp.swift
    Shared/         WorkoutsActivityAttributes.swift SessionIntents.swift          (app + widget)
    App/            SessionController.swift AppModel.swift WorkoutsSettings.swift DeepLinks.swift
    Platform/       CountdownCoordinator.swift Notifications.swift LiveActivityController.swift
                    HealthExporter.swift BodyMassReader.swift KeepAwake.swift Haptics.swift
    Views/
      RootView.swift
      Routines/     RoutinesView.swift RoutineCard.swift RoutinePreview.swift
      Session/      SessionCover.swift SessionHeader.swift ExerciseCard.swift GroupHeader.swift SetGrid.swift
                    SetRow.swift SetKindMenu.swift RPESheet.swift TimedCell.swift RestBar.swift
                    ExercisePicker.swift NewExerciseSheet.swift ReorderSheet.swift RestPickerSheet.swift
                    InProgressAccessory.swift FinishSheet.swift
        Rows/       RowsSessionView.swift RowsBlockView.swift RowsLines.swift RowsEditorPanel.swift
                    RowsCapsuleView.swift RowsScreenModel.swift RowsStyle.swift RowsPreviews.swift   (§7.6a)
      History/      HistoryView.swift SessionDetailView.swift
      Exercises/    ExercisesView.swift ExerciseDetailView.swift E1RMChart.swift
      Settings/     SettingsView.swift RestDefaultsView.swift
    Preview Content/  PreviewHistory.json (generated)  PreviewData.swift
  SubstrateWorkoutsWidgets/
    SubstrateWorkoutsWidgets.swift  SessionLiveActivity.swift
```

## 3. Model and record codec

### 3.1 Kind names

```swift
public struct KindNames: Sendable, Hashable {
    public var exercise = "~/training/exercise"
    public var routine = "~/training/routine"
    public var session = "~/training/session"
    public var set = "~/training/set"
    public var setup = "~/training/setup"
    public var healthWorkout = "substrate.reamde.dev/health/workout"
    public var observationSource = "substrate.reamde.dev/observations/source"
    public static let v1 = KindNames()
}
```

Nothing else in the code spells a kind name. The codec and the V2 client take a `KindNames`.

### 3.2 Value types

```swift
public enum MassUnit: String, Codable, Sendable { case kg, lb }
/// A load as entered: exact digits and the unit. Never a Double in storage.
public struct Mass: Codable, Hashable, Sendable {
    public var value: Decimal          // "83.9146" stays 83.9146
    public var unit: MassUnit
    public static let poundInKilograms = Decimal(string: "0.45359237")!
    public func converted(to: MassUnit) -> Mass        // exact multiply or divide, no rounding
}
public struct Length: Codable, Hashable, Sendable { public var metres: Decimal }   // quantity unit "m"
public typealias Seconds = Int         // every duration in the app is whole seconds

public struct ExerciseID: RawRepresentable, Codable, Hashable, Sendable { public var rawValue: String } // "chest-press-machine"
public struct RoutineID: RawRepresentable, Codable, Hashable, Sendable { public var rawValue: String }  // "fully-body-collection-1"
public struct SessionID: RawRepresentable, Codable, Hashable, Sendable { public var rawValue: String }  // lowercase UUIDv7
public struct SetID: RawRepresentable, Codable, Hashable, Sendable { public var rawValue: String }      // lowercase UUIDv7, or an importer's key
public struct SetupID: RawRepresentable, Codable, Hashable, Sendable { public var rawValue: String }    // "hotel-gym-chest-press"
public struct RowID: RawRepresentable, Codable, Hashable, Sendable { public var rawValue: String }      // engine-local UUIDv7, never written to a record
public struct EventID: RawRepresentable, Codable, Hashable, Sendable { public var rawValue: String }    // UUIDv7
/// A reference to a record of a kind the training package does not own (`workout`, `source`, `about`, `origin`).
public struct RecordRef: Codable, Hashable, Sendable { public var kind: String; public var id: String }
/// How an effort value got onto a set (`set.rpeMethod`, `set.rirMethod`; §4.14).
public enum EffortMethod: String, Codable, Sendable { case entered, prefilled, copied, imported
    public var isRating: Bool                          // entered and imported; absent beside a value reads as a rating
}
/// A nested object's unknown fields (§3.4 rule 7). Decodes as empty when its key is missing, so a journal written
/// before a type had one still loads.
@propertyWrapper public struct UnknownFields: Codable, Hashable, Sendable { public var wrappedValue: [String: JSONValue] }
```

**UUIDv7.** `UUIDv7.make(at: Date, using: inout some RandomNumberGenerator) -> String`: 48-bit big-endian Unix
milliseconds, version nibble 7, 12 random bits, variant `10`, 62 random bits, lowercase and hyphenated. A session's id
uses its start, a set's id its end (`at`), as the examples' ids do. Tests inject a seeded generator.

**Time.** Every instant the engine stores is floored to a whole second when the controller creates the event. Durations
computed from them are then whole seconds, and a record's `at` equals the instant in its UUIDv7.

### 3.3 The kinds as Swift types

Field by field, in the kind files' order. Optional means the property may be absent; `[]` means a repeated property
whose absence decodes as empty. Enum raw values are the kind's spelled values exactly.

**Exercise** (`exercise.yaml`; no traits)

```swift
public struct Exercise: Codable, Hashable, Sendable, Identifiable {
    public var id: ExerciseID
    public var name: String                            // required
    public var aliases: [String]
    public var measurement: Measurement                // required
    public var equipment: Equipment?
    public var laterality: Laterality?                 // absent reads as bilateral
    public var loadBasis: LoadBasis?                   // absent reads as total
    public var implements: Int?                        // absent reads as 1
    public var primaryMuscles: [Muscle]
    public var secondaryMuscles: [Muscle]
    public var restClass: RestClass?
    public var variationOf: ExerciseID?
    public var codes: [Coding]
    public var loadBase: Mass?
    public var loadStep: Mass?                         // its unit is the exercise's unit
    public var loadMax: Mass?
    public var bodyShare: Decimal?                     // 0...1, absent reads as 1
    public var defaultRepsMin: Int?
    public var defaultRepsMax: Int?
    public var defaultRPE: Decimal?
    public var repGain: Int?
    public var dropPercent: Decimal?                   // 0...100, absent reads as 20
    public var timeStep: Seconds?                      // absent reads as 5
    public var myoStopRule: MyoStopRule?               // absent reads as repdrop
    public var myoMaxMiniSets: Int?                    // absent reads as 5
    public var extra: [String: JSONValue]              // unknown properties, kept for round trips (§3.4)
    public var isLocal: Bool                           // added on this phone; not a property
}
public struct Coding: Codable, Hashable, Sendable { public var system: String; public var code: String; public var version: String?
    public var display: String?; @UnknownFields public var extra: [String: JSONValue] }
public enum Measurement: String, Codable, Sendable { case weightreps, repsonly, bodyweightreps, weightedbodyweightreps,
    bodyweightassistedreps, duration, weightduration, distanceduration, shortdistanceweight }
public enum Equipment: String, Codable, Sendable { case none, barbell, dumbbell, kettlebell, machine, plateloaded, cable,
    plate, resistanceband, suspension, other }
public enum Laterality: String, Codable, Sendable { case bilateral, unilateral, alternating }
public enum LoadBasis: String, Codable, Sendable { case total, perimplement, perside }
public enum Muscle: String, Codable, Sendable { case abdominals, abductors, adductors, biceps, calves, cardio, chest,
    forearms, fullbody, glutes, hamstrings, lats, lowerback, neck, quadriceps, shoulders, traps, triceps, upperback, other }
public enum RestClass: String, Codable, Sendable { case heavycompound, compound, isolation, highrep, timed, carry, power }
public enum MyoStopRule: String, Codable, Sendable { case repdrop, belowtarget }
```

**Routine** (`routine.yaml`; no traits; the record's server `version` is kept for `routineVersion`)

```swift
public struct Routine: Codable, Hashable, Sendable, Identifiable {
    public var id: RoutineID
    public var version: Int?                           // the flat record's top-level "version"; bundled: nil, read as 1
    public var name: String                            // required
    public var folder: String?
    public var position: Int?
    public var notes: String?
    public var deload: Bool?
    public var activityType: ActivityType?
    public var groups: [RoutineGroup]
    public var slots: [RoutineSlot]
    public var extra: [String: JSONValue]
}
public enum ActivityType: String, Codable, Sendable { case traditionalstrengthtraining, functionalstrengthtraining,
    coretraining, highintensityintervaltraining }
public struct RoutineGroup: Codable, Hashable, Sendable {
    public var key: String                             // required
    public var groupKind: GroupKind                    // required
    public var pairing: Pairing?
    public var rounds: Int?
    public var roundRest: Seconds?
    @UnknownFields public var extra: [String: JSONValue]
}
public enum GroupKind: String, Codable, Sendable { case superset, triset, giantset, circuit, paired }
public enum Pairing: String, Codable, Sendable { case antagonist, samemuscle, preexhaust, upperlower, unrelated, mechanicaldrop }
public struct RoutineSlot: Codable, Hashable, Sendable {
    public var key: String                             // required
    public var exercise: ExerciseID                    // required
    public var group: String?
    public var setRest: Seconds?
    public var warmupRest: Seconds?
    public var transition: Seconds?
    public var sideSwitch: Seconds?
    public var sideOrder: SideOrder?
    public var sideMatch: Bool?
    public var notes: String?
    public var sets: [PlannedSet]
    @UnknownFields public var extra: [String: JSONValue]
}
public enum SideOrder: String, Codable, Sendable { case left, right, weakerfirst }
/// A routine slot's planned set, and a session slot's snapshot of one (`session.slots[].sets`, the same fields).
public struct PlannedSet: Codable, Hashable, Sendable {
    public var key: String                             // required
    public var role: Role = .working                   // declared default
    public var roleLabel: RoleLabel?
    public var structure: Structure = .straight        // declared default
    public var termination: Termination?
    public var repsMin: Int?
    public var repsMax: Int?
    public var load: Mass?                             // wins over loadPercent when both are given
    public var loadPercent: Decimal?                   // 0...200, of loadReference
    public var loadReference: LoadReference?
    public var assistance: Mass?
    public var rpeMin: Decimal?                        // an effort target is a range; one value is min = max
    public var rpeMax: Decimal?
    public var rirMin: Decimal?
    public var rirMax: Decimal?
    public var durationMin: Seconds?
    public var durationMax: Seconds?
    public var timerMode: TimerMode?
    public var distance: Length?
    public var tempo: String?
    public var miniSetReps: Int?
    public var miniSets: Int?
    public var myoStopRule: MyoStopRule?
    public var totalReps: Int?
    public var drops: Int?
    public var dropPercent: Decimal?
    public var dropStep: Mass?
    public var dropReps: Int?
    public var intraSetPause: Seconds?
    public var rest: Seconds?
    @UnknownFields public var extra: [String: JSONValue]
    public var effortTop: Decimal?                     // rpeMax, else 10 − rirMin: the load selector and the pre-fill
}
public enum LoadReference: String, Codable, Sendable { case onerm, e1rm, trainingmax, bodymass }
public enum Role: String, Codable, Sendable { case warmup, working }
public enum RoleLabel: String, Codable, Sendable { case top, backoff }
public enum Structure: String, Codable, Sendable { case straight, drop, myo, myomatch, restpause, cluster }
public enum Termination: String, Codable, Sendable { case reps, reprange, amrap, failure, rirstop, time, distance, totalreps }
public enum TimerMode: String, Codable, Sendable { case countdown, stopwatch }
```

**Training session** (`session.yaml`; traits `temporal(range)` and `revised`)

```swift
public struct TrainingSession: Codable, Hashable, Sendable, Identifiable {
    public var id: SessionID
    public var at: Date                                // the trait's start
    public var endsAt: Date?                           // the trait's end; absent while active
    public var name: String?
    public var status: SessionStatus                   // absent on decode reads as active
    public var routine: RoutineID?
    public var routineVersion: Int?
    public var deload: Bool?
    public var sport: Sport?                           // health/workout's code set; the app writes strength
    public var sportSource: String?                    // an importer's own word
    public var activityType: ActivityType?             // the Apple Health save's setting only
    public var slots: [SessionSlot]
    public var groups: [SessionGroup]
    public var pauses: [Pause]
    public var utcOffsetStart: Int?                    // minutes east of UTC, -1080...1080
    public var utcOffsetEnd: Int?
    public var timezone: String?                       // IANA name
    public var bodyMass: Mass?
    public var sessionRPE: Decimal?                    // CR10, 0...10
    public var notes: String?
    public var healthKitWorkout: String?               // uppercase UUID
    public var workout: RecordRef?                     // V2 only
    public var source: RecordRef?
    public var about: RecordRef?
    public var origin: RecordRef?
    public var revision: Int?
    public var adapter: String?                        // "substrate-workouts@0.1.0"
    public var extra: [String: JSONValue]
}
public enum SessionStatus: String, Codable, Sendable { case active, finished }
public enum Sport: String, Codable, Sendable { case running, walking, hiking, cycling, swimming, strength, yoga, pilates,
    rowing, elliptical, stairs, hiit, dance, boxing, climbing, tennis, golf, skiing, other }
public struct SessionSlot: Codable, Hashable, Sendable {
    public var key: String                             // required
    public var exercise: ExerciseID                    // required
    public var group: String?
    public var routineSlot: String?
    public var notes: String?
    public var setRest: Seconds?                       // only when set in the gym, or for an exercise with no routine slot
    public var warmupRest: Seconds?
    public var transition: Seconds?
    public var sideSwitch: Seconds?
    public var sets: [PlannedSet]                      // the routine slot's planned sets as the session started with them
    @UnknownFields public var extra: [String: JSONValue]
}
public struct SessionGroup: Codable, Hashable, Sendable {
    public var key: String                             // required
    public var groupKind: GroupKind                    // required
    public var pairing: Pairing?
    public var rounds: Int?
    public var roundRest: Seconds?
    @UnknownFields public var extra: [String: JSONValue]
}
public struct Pause: Codable, Hashable, Sendable { public var start: Date; public var end: Date?
    @UnknownFields public var extra: [String: JSONValue] }
```

**Set** (`set.yaml`; traits `temporal(point)` and `revised`)

```swift
public struct PerformedSet: Codable, Hashable, Sendable, Identifiable {
    public var id: SetID
    public var at: Date                                // when the set ended (the tick); timeBasis says what it is
    public var session: SessionID                      // required
    public var exercise: ExerciseID                    // required
    public var slot: String                            // required
    public var order: Int                              // required, from 1
    public var ordinal: Int?
    public var setup: SetupID?                         // a reference to the setup kind; V1 never writes one
    public var role: Role = .working
    public var roleLabel: RoleLabel?
    public var structure: Structure = .straight
    public var continues: SetID?                       // the chain's head; nil on the head
    public var chainPosition: Int?
    public var side: Side?
    public var loadMode: LoadMode?                     // nil when the exercise moves no load
    public var load: Mass?
    public var assistance: Mass?
    public var resistanceLabel: String?                // a band's colour, a machine's level: never kilograms
    public var reps: Int?
    public var duration: Seconds?
    public var distance: Length?
    public var loadBasis: LoadBasis?                   // what the numbers meant, copied from the exercise when logged
    public var implements: Int?
    public var bodyShare: Decimal?
    public var laterality: Laterality?
    public var repsBasis: RepsBasis?
    public var rpe: Decimal?
    public var rpeMethod: EffortMethod?                // written exactly when rpe is (§4.14)
    public var rir: Decimal?
    public var rirMethod: EffortMethod?                // written exactly when rir is
    public var rpeScale: RPEScale?                     // the app writes rirbased; absent reads as rirbased
    public var reachedFailure: Bool?
    public var failedRepAttempted: Bool?
    public var completionMethod: CompletionMethod?
    public var notes: String?
    public var sourceType: String?
    public var timeBasis: TimeBasis?
    public var startedAt: Date?
    public var restBefore: Seconds?
    public var restMethod: RestMethod?                 // written exactly when restBefore is
    public var restPlanned: Seconds?
    public var restSkipped: Bool?
    public var target: SetTarget?
    public var origin: RecordRef?
    public var revision: Int?
    public var extra: [String: JSONValue]
    public var ratedRPE: Decimal?                      // on an RIR-based scale: rpe when entered, imported or legacy; else 10 − a rated rir
    public var unratedRPE: (value: Decimal, method: EffortMethod)? // a pre-filled or copied rpe (or rir), with its method
    public var loggedRPE: Decimal?                     // rpe whatever its method, else 10 − rir on an RIR-based scale
}
public enum Side: String, Codable, Sendable { case left, right }
public enum LoadMode: String, Codable, Sendable { case external, bodyweight, bodyweightplus, assisted }
public enum RPEScale: String, Codable, Sendable { case rirbased, hevy, strong, cr10 }    // RIR = 10 − RPE on all but cr10
public enum RepsBasis: String, Codable, Sendable { case total, perside }
public enum CompletionMethod: String, Codable, Sendable { case individual, bulk, imported }
public enum TimeBasis: String, Codable, Sendable { case completion, session, estimated }
public enum RestMethod: String, Codable, Sendable { case start, interaction, imported, estimated }
public struct SetTarget: Codable, Hashable, Sendable {
    public var plannedSet: String?
    public var termination: Termination?
    public var repsMin: Int?
    public var repsMax: Int?
    public var load: Mass?                             // always the resolved absolute load
    public var loadPercent: Decimal?
    public var loadReference: LoadReference?
    public var referenceLoad: Mass?                    // the reference the percentage was taken of
    public var assistance: Mass?
    public var rpeMin: Decimal?
    public var rpeMax: Decimal?
    public var rirMin: Decimal?
    public var rirMax: Decimal?
    public var durationMin: Seconds?
    public var durationMax: Seconds?
    public var timerMode: TimerMode?
    public var distance: Length?
    public var tempo: String?
    public var miniSetReps: Int?
    public var miniSets: Int?
    public var myoStopRule: MyoStopRule?
    public var totalReps: Int?
    public var drops: Int?
    public var dropPercent: Decimal?
    public var dropStep: Mass?
    public var dropReps: Int?                          // absent: each drop to failure
    public var intraSetPause: Seconds?
    @UnknownFields public var extra: [String: JSONValue]
    public var effortTop: Decimal?                     // rpeMax, else 10 − rirMin
}
/// One finished session as records: what the history file holds and what V2 uploads.
public struct SessionRecords: Codable, Hashable, Sendable { public var session: TrainingSession; public var sets: [PerformedSet] }
public struct Catalog: Sendable { public var exercises: [ExerciseID: Exercise]; public var routines: [RoutineID: Routine]
    public var setups: [SetupID: Setup] }              // V1 bundles no setup
```

**Setup** (`setup.yaml`; no traits): one station an exercise is done on. A set names it, and PREVIOUS and suggestions
keep each setup's history apart (V1: sets with none).

```swift
public struct Setup: Codable, Hashable, Sendable, Identifiable {
    public var id: SetupID
    public var name: String                            // required
    public var exercise: ExerciseID                    // required
    public var gym: String?
    public var loadBase: Mass?                         // absent: the exercise's
    public var loadStep: Mass?
    public var loadMax: Mass?
    public var notes: String?
    public var extra: [String: JSONValue]
}
extension Exercise { public func on(_ setup: Setup?) -> Exercise }  // the setup's grid fields over the exercise's
```

### 3.4 Codec rules

`RecordCodec(kinds:)` converts between these types and `FlatRecord` (`{"kind", "id", "properties", "version"?}`, the
REST shape; other top-level server fields such as `createdAt` are ignored on decode and never sent).

1. **Kind and id.** Decoding checks `kind` equals the configured name for the type and refuses anything else. The
   record's `id` is the model's `id`.
2. **Names.** Property names are the declared names exactly. Model `extra` keys never collide with declared names.
3. **Absent and nil.** A missing property decodes as `nil` (or `[]` for a repeated one). Encoding omits every `nil`
   and every empty array, and never sends `title` (the server renders it). A `title` in a decoded record is dropped.
4. **Declared defaults are not encoded.** `role: working` and `structure: straight` (on a set, a planned set) are
   omitted, because the server fills them and the examples leave them out. Decoding a missing one gives the default.
5. **Session status.** Encoded only when `finished`. A create never names `status` (it is born `active`); this rule makes
   the Start write and the round trip of an in-progress session both correct.
6. **Types.**

   | Declared type    | JSON                                                 | Swift                   | Notes                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
   | ---------------- | ---------------------------------------------------- | ----------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
   | `string`, `text` | string                                               | `String`                | `text` keeps newlines.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
   | `int`            | number                                               | `Int`                   | A non-integral number is refused.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
   | `decimal`        | string, such as `"9.5"`                              | `Decimal`               | A JSON number is refused (substrate refuses it too). Parsed with the POSIX locale. Encoded as the shortest plain digits: no exponent, no trailing zeros, `"0"` for zero.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
   | `quantity`       | `{"value": "35", "unit": "kg"}`                      | `Mass`, `Length`        | `value` as a decimal. Units `kg` and `lb` for mass, `m` for length; anything else is refused.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
   | `duration`       | ISO 8601 string                                      | `Seconds`               | Decodes `P[nW][nD][T[nH][nM][n[.f]S]]`, a day being 24 h, rounding a fraction to the nearest second. Encodes substrate's one stored spelling: days, then a time part, zero parts omitted, `PT0S` for zero (`PT1M30S`, `P1DT12H`).                                                                                                                                                                                                                                                                                                                                                                                                                        |
   | `datetime`       | RFC 3339 string                                      | `Date`                  | Decodes any offset and optional fractional seconds. Encodes UTC with `Z` and whole seconds: `2026-10-27T13:30:00Z`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
   | `timezone`       | string                                               | `String`                | IANA name, not validated by the codec.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
   | `bool`           | boolean                                              | `Bool`                  |                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
   | `enum`           | string                                               | Swift enum              | The raw value spelled as declared. An unknown value: see rule 7.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
   | `object`         | object                                               | struct                  | Same rules for its fields.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
   | `reference`      | `{"ref": "<kind>/<id>"}` or the bare `"<kind>/<id>"` | typed id or `RecordRef` | Substrate serves the object, at every level; a write may send the bare path, which the bundled files use. Either decodes, the object with any link fields beside `ref`. Training references decode by stripping the configured kind prefix and `/`; the rest, which may contain `/`, is the id. A different kind, or an object with no `ref`, is refused. `workout`, `source`, `about` and `origin` become `RecordRef`, split at the declared kind's prefix when there is one, else at the third `/`. A reference read as an object is written back as that object while it points at the same record; a new or changed one is written as the bare path. |

7. **Forward compatibility, at every level.** An older build must never erase what a newer one wrote, so every
   object the codec reads keeps what it does not know: the record's `extra`, and an `extra` on every nested type
   (`Coding`, `RoutineGroup`, `RoutineSlot`, `PlannedSet`, `SessionSlot`, `SessionGroup`, `Pause`, `SetTarget`).
   - An unknown property or field is kept verbatim and re-emitted on encode, after the declared ones.
   - An unknown value of an optional enum that only describes leaves the typed field `nil` and keeps the raw value in
     `extra` under its name (a set's `side`, a target's `timerMode`, a session's `status`, which reads as `active`).
     A set's `rpeMethod` or `rirMethod` kept so is no rating (§4.14), and it is dropped with its value: a cleared
     `rpe` writes no `rpeMethod`. A `rpeScale` kept so is not read as RIR-based.
   - A reference read as an object is kept in `extra` under its name, so it is written back as read (rule 6).
   - An unknown value of an **execution enum**, one that decides how the engine runs a set, makes the record
     unsupported rather than read as a default: a routine's planned set's `role`, `structure`, `termination`,
     `timerMode`, `myoStopRule` and `loadReference`, a routine slot's `sideOrder`, an exercise's `laterality` (side
     chains) and `myoStopRule`, and a set's `role` and `structure`. A routine with an `emom` structure is never run as
     straight sets.
   - A session's plan snapshot (`slots[].sets`) is never run, so the same fields there only describe: an unknown value
     leaves the field at its default, is kept raw in the planned set's `extra`, and the session stays readable in
     History and PREVIOUS.
   - A group's `pairing` only picks a default transition when neither the session nor the routine names one, so an
     unknown value reads as no pairing (the group kind's default) and is kept raw; the routine still runs.
   - An unknown or missing value of a required property (`measurement`, `groupKind`, a key, a slot's `exercise`) makes
     the record undecodable.

   An unsupported record throws `CodecError.unsupported(kind:id:property:value:)`. The catalogue skips such a record
   and Settings lists it; it never crashes the app.
8. **Key order.** The encoder writes properties in the kind file's declaration order, so files diff well. Tests compare
   structurally.
9. **Round trip.** For every record the codec decodes, `encode(decode(r))` equals `r` structurally, with decimals and
   quantity values compared as numbers, durations as seconds and datetimes as instants (§10.4). That holds for the
   bundled shorthand and for substrate's served form alike.
10. **Five kinds.** `decode(_:)` reads an exercise, a routine, a session, a set or a setup (`DecodedRecord.setup`).

### 3.5 Session state to records

At Finish, `SessionEngine.records(state, adapter:)` builds `SessionRecords`. It is pure and tested against the
examples' shapes.

**The session record.**

| Property                               | Value                                                                                                                                                                                                                                                                                                     |
| -------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `id`                                   | The session id minted at Start.                                                                                                                                                                                                                                                                           |
| `at`, `endsAt`                         | The start and end confirmed in the Finish sheet.                                                                                                                                                                                                                                                          |
| `name`                                 | `"<routine name>, <EEE d MMM HH:mm>"` on the session's own clock, such as `Fully Body Collection #1, Fri 30 Oct 13:42`; for an empty session `Training session, Thu 29 Oct 18:05`. Recomputed if Finish moves the start. Formatted with `en_GB` and the session's `timezone`, so tests are deterministic. |
| `status`                               | `finished`.                                                                                                                                                                                                                                                                                               |
| `routine`, `routineVersion`            | The routine id and its `version` (1 when the routine had none, as bundled ones do); absent for an empty session.                                                                                                                                                                                          |
| `deload`, `activityType`               | Copied from the routine at Start when present. `activityType` is the Apple Health save's setting only.                                                                                                                                                                                                    |
| `sport`                                | `strength`, on every session the app writes (`health/workout`'s code set). No `sportSource`: that is an importer's.                                                                                                                                                                                       |
| `slots`                                | Slots with at least one performed set, in display order: `key`, `exercise`, `group`, `routineSlot`, `notes`, rests by the rule below, and the plan snapshot below.                                                                                                                                        |
| `groups`                               | Groups with at least two remaining member slots: `key`, `groupKind`, and `pairing`, `rounds`, `roundRest` only when the group was made in the gym or one of them was changed there. `rounds` written is the derived round count.                                                                          |
| `pauses`                               | Every pause, each with its end.                                                                                                                                                                                                                                                                           |
| `utcOffsetStart`, `utcOffsetEnd`       | `TimeZone(identifier: timezone).secondsFromGMT(for:)` / 60 at the start and at the end.                                                                                                                                                                                                                   |
| `timezone`                             | `TimeZone.current.identifier` at Start. Spelled as core's `recurring` trait spells it (it was `timeZone` before the data-model review).                                                                                                                                                                   |
| `bodyMass`                             | The value used at Start, if any.                                                                                                                                                                                                                                                                          |
| `sessionRPE`, `notes`                  | From the Finish sheet when given.                                                                                                                                                                                                                                                                         |
| `revision`                             | §4.12.                                                                                                                                                                                                                                                                                                    |
| `adapter`                              | `substrate-workouts@<CFBundleShortVersionString>`.                                                                                                                                                                                                                                                                   |
| `healthKitWorkout`                     | Absent at Finish; written when the Health save returns (§9.5).                                                                                                                                                                                                                                            |
| `workout`, `source`, `about`, `origin` | Absent in V1.                                                                                                                                                                                                                                                                                             |

**Rests on the session's slots** (session.yaml: "written when the session has no routine for them or the owner changed
one"): a slot stores every rest the owner set on it in the gym; a slot with no routine slot (an added exercise, an
empty session's) also stores the rests it ran with that the next session would need, its `setRest` when it is a plain
slot and its `transition` when it is a group member other than the last. The coverage file's in-progress session is the
test: the added leg extension stores `setRest` 1:30 (its class default) and the `warmupRest` 0:30 the owner set; the
chest press, first member of a superset made in the gym, stores its `transition` 0:15; the seated row, the last member,
stores nothing.

**The plan snapshot** (`slots[].sets`, D8): each kept slot with a routine slot carries that routine slot's planned sets
from the routine as it was at Start (`state.plan`), every one, done or not, in the routine's order, with the
routine's keys and values; a percentage of body mass is resolved into `load` (§4.5). Only the first kept slot naming a
routine slot carries it, so a slot that took over another's pending sets (a replace after a done set) does not repeat
it. A slot with no routine slot (an added exercise, an empty session's) has none, and a slot Finish drops because
nothing in it was done takes its planned sets with it (open question 32). V1 writes the snapshot at Finish with the
rest of the session; the 27 October Fully Body session's nine slots are the test.

**Each set record**, one per performed row (§4.1):

| Property                                   | Value                                                                                                                                                                                                                                                                               |
| ------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `id`, `at`                                 | The record id minted at the first tick; the tick's instant (for a Finish "mark done" row, the session's end).                                                                                                                                                                       |
| `session`, `exercise`, `slot`              | The session, the slot's exercise, the slot's key.                                                                                                                                                                                                                                   |
| `order`                                    | The row's place among the slot's performed rows in display order, from 1 (warm-ups first, a chain's records in turn).                                                                                                                                                               |
| `ordinal`                                  | §4.10, computed over performed rows only.                                                                                                                                                                                                                                           |
| `role`, `roleLabel`, `structure`           | From the row.                                                                                                                                                                                                                                                                       |
| `continues`, `chainPosition`, `side`       | For a chain: `continues` is the head's record id on every later record; `chainPosition` 1 on the head. `side` on side-chain records. A straight, unsided set carries none.                                                                                                          |
| `loadMode`                                 | From the measurement: `weightreps`, `weightduration`, `shortdistanceweight` give `external`; `bodyweightreps` gives `bodyweight`; `weightedbodyweightreps` gives `bodyweightplus`; `bodyweightassistedreps` gives `assisted`; `repsonly`, `duration`, `distanceduration` give none. |
| `load`, `assistance`                       | `external`: `load`. `bodyweightplus`: `load` is the added load, `0` allowed. `assisted`: `assistance`, no `load`. `bodyweight` and no-load measurements: neither.                                                                                                                   |
| `reps`, `duration`, `distance`             | As the measurement records them. `duration` also on a carry or a distance set timed with the stopwatch.                                                                                                                                                                             |
| `rpe`, `rpeMethod`                         | The row's RPE and how it got there (§4.14): `entered` (a tap), `copied` (PREVIOUS) or `prefilled` (the plan's value, accepted by the tick). Both or neither. `rir`, `rirMethod`, `sourceType`, `setup`, `resistanceLabel`: never written by V1.                                     |
| `rpeScale`                                 | `rirbased`, on every set.                                                                                                                                                                                                                                                           |
| `loadBasis`, `implements`                  | With a `loadMode`: the exercise snapshot's, resolved (`total` and 1 when it names none), so a later edit of the exercise reinterprets no stored load (D6).                                                                                                                          |
| `bodyShare`                                | With `loadMode` `bodyweight`, `bodyweightplus` or `assisted`: the exercise snapshot's, else 1.                                                                                                                                                                                      |
| `laterality`                               | Every set: the exercise snapshot's, else `bilateral`.                                                                                                                                                                                                                               |
| `repsBasis`                                | With `reps`: `perside` on a unilateral or alternating exercise, `total` otherwise.                                                                                                                                                                                                  |
| `reachedFailure`, `failedRepAttempted`     | `true` when set on the row; never written `false`.                                                                                                                                                                                                                                  |
| `completionMethod`, `timeBasis`            | A tick or a Stop: `individual` and `completion`. A row Finish marked done: `bulk` and `session` (its `at` is the session's end).                                                                                                                                                    |
| `notes`                                    | The set's note.                                                                                                                                                                                                                                                                     |
| `startedAt`                                | When the row's timer started (timed, distance and carry sets); otherwise absent.                                                                                                                                                                                                    |
| `restBefore`, `restPlanned`, `restSkipped` | §4.8. Absent on the session's first performed set and on rows marked done at Finish. `restSkipped` written only when `true`.                                                                                                                                                        |
| `restMethod`                               | Beside every `restBefore`: `start` when the rest ran to the set's `startedAt` (a timed set's play ended it), else `interaction` (Skip, a touch once the countdown is over, or the tick; §4.8).                                                                                      |
| `target`                                   | The row's copied target (§4.5) with what the engine ran where the plan is silent (below); absent for a row the plan did not have.                                                                                                                                                   |
| `revision`                                 | §4.12.                                                                                                                                                                                                                                                                              |

**The target as the engine ran it** (D7). The row's target is a copy of the planned set (§4.5). The record adds, on a
head, where the plan names none:

- `timerMode` on a timed target (one with `durationMin` or `durationMax`): `countdown`.
- `myoStopRule` on a myo-rep set: the exercise's, else `repdrop`.
- `intraSetPause` on any structured set: the planned set's, else §4.7's default (15 s for myo-reps and myo-rep match,
  20 s for rest-pause and clusters, `PT0S` for drops). The set's own structure decides, so a set switched to a drop in
  the gym says `PT0S`.

A chain's later record keeps the planned set's key and its own targets (§4.5); a timed second side also carries its
head's timer mode. `totalReps` on a myo-rep match head is resolved at its tick (§4.9).

## 4. Session engine

`SessionEngine` is a pure reducer in WorkoutsCore: `apply(_ event: EventEnvelope, to state: SessionState?) throws(EngineError) -> SessionState`.
It reads no clock, no files and no history; everything it needs is in the state or the event. The controller mints
ids, timestamps and any value that needs history (an added exercise's rows, ghost values a tick commits) and puts them
in the event.

### 4.1 State

```swift
public struct SessionState: Codable, Equatable, Sendable {
    public var session: SessionHeader            // the session record's own fields so far (§3.3 TrainingSession, minus slots and groups)
    public var phase: Phase                      // .active, .finished; discard deletes the state
    public var plan: Routine?                    // the routine exactly as it was at Start; nil for an empty session
    public var exercises: [ExerciseID: Exercise] // a snapshot of every exercise any slot ever named
    public var settings: SessionSettings         // rest defaults and counting switches, snapshot at Start (§4.7)
    public var slots: [SlotState]                // display order
    public var groups: [GroupState]
    public var rows: [RowID: Row]
    public var countdown: Countdown?             // the one running countdown, a rest or a timed set
    public var restClock: RestClock?             // measures the rest before the next set (§4.8)
    public var focus: RowID?                     // a pending row the owner touched since the last tick
    public var lastCompleted: RowID?             // the anchor for Up next
    public var revision: Int                     // the session record's revision (§4.12)
}

public struct SlotState: Codable, Equatable, Sendable {
    public var key: String
    public var exercise: ExerciseID
    public var group: String?
    public var routineSlot: String?
    public var notes: String?
    public var rests: SlotRests                  // values set in the gym, and an added slot's own rests (§3.5)
    public var rows: [RowID]                     // display order: warm-ups, then working sets, each head followed by its segments
    public var sideOrder: SideOrder?             // copied from the routine slot
    public var sideMatch: Bool?
    public var replaces: String?                 // the lane of the slot whose pending sets it took over (§4.3 replaceExercise)
    public var lane: String { replaces ?? key }  // the member it counts as in a group's rounds (§4.6)
}
public struct SlotRests: Codable, Equatable, Sendable { public var setRest, warmupRest, transition, sideSwitch: Seconds? }
public struct GroupState: Codable, Equatable, Sendable {
    public var key: String
    public var groupKind: GroupKind
    public var pairing: Pairing?
    public var roundRest: Seconds?
    public var rounds: Int?
    public var madeInGym: Bool                   // decides what the session record stores (§3.5)
    public var changedInGym: Bool
}

public struct Row: Codable, Equatable, Sendable {
    public var id: RowID
    public var slot: String
    public var plannedSet: String?               // the routine planned set's key; nil for an added row
    public var role: Role
    public var roleLabel: RoleLabel?
    public var structure: Structure
    public var termination: Termination?
    public var chainHead: RowID?                 // nil on a head
    public var chainPosition: Int                // 1 on a head
    public var side: Side?
    public var target: SetTarget?                // copied from the planned set at row creation (§4.5)
    public var restOverride: Seconds?            // the planned set's `rest`, or one set in the gym
    public var intraSetPause: Seconds?           // the planned set's, for a structured head
    public var timerMode: TimerMode?
    public var entered: SetValues                // typed or copied values, not yet ticked
    public var timerStartedAt: Date?             // a timed, distance or carry row whose timer runs or ran
    public var performed: Performed?             // nil while pending
    public var notes: String?
    public var generated: Bool                   // made by the engine as a chain segment
    public var rpePrefillDeclined: Bool          // the owner cleared the RPE: no pre-fill any more (§4.14)
}
public struct SetValues: Codable, Equatable, Sendable {
    public var load: Mass?; public var assistance: Mass?; public var reps: Int?; public var duration: Seconds?
    public var distance: Length?; public var rpe: Decimal?
    public var rpeMethod: EffortMethod?          // how rpe got there (§4.14); nil beside a value reads as entered
    public var reachedFailure: Bool?; public var failedRepAttempted: Bool?
    public var ratedRPE: Decimal?                // rpe unless pre-filled or copied
}
public struct Performed: Codable, Equatable, Sendable {
    public var recordID: SetID
    public var completedAt: Date
    public var startedAt: Date?
    public var values: SetValues                 // complete for the measurement
    public var restBefore: Seconds?
    public var restPlanned: Seconds?
    public var restSkipped: Bool
    public var edits: Int                        // edits after the first tick
    public var priorRestClock: RestClock?        // restored if the tick is undone
    public var completion: CompletionMethod      // individual for a tick or a Stop, bulk for a row Finish marked done
}
public enum RestSlot: String, Codable, Sendable {
    case override, warmupRest, intraSetPause, sideSwitch, transition, roundRest, blockRest, setRest
}
public struct Countdown: Codable, Equatable, Sendable {
    public enum Kind: Codable, Equatable, Sendable { case rest(RestSlot), timedSet(RowID), manual }
    public var kind: Kind
    public var startedAt: Date
    public var endsAt: Date?                     // nil for a stopwatch
}
public struct RestClock: Codable, Equatable, Sendable {
    public var since: Date                       // the tick of the set before
    public var planned: Seconds                  // 0 when no timer ran
    public var slot: RestSlot?
    public var endedAt: Date?
    public var skipped: Bool
}
```

### 4.2 Events

Every event is wrapped as `EventEnvelope { id: EventID, at: Date, event: SessionEvent }` and appended to the journal
only if `apply` succeeds. Ids the reducer needs (rows, records, groups, slots) are minted by the controller and carried
in the event, so a fold is deterministic.

| Event                       | Payload                                                                                                                                             | Touches the session record (§4.12) |
| --------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------- |
| `start`                     | session id, routine snapshot or nil, exercise snapshots, settings snapshot, body mass, time zone, offset, row ids per planned set and known segment | yes, revision 1                    |
| `touchRow`                  | row                                                                                                                                                 | no                                 |
| `editSet`                   | row, a patch of `SetValues` (each field: unchanged, set, or cleared; a set RPE with its method), optional note                                      | no                                 |
| `completeSet`               | row, record id, the values committed, ids for segment rows the tick may generate                                                                    | no                                 |
| `uncompleteSet`             | row                                                                                                                                                 | no                                 |
| `addSet`                    | slot, new row id, role                                                                                                                              | no                                 |
| `removeSet`                 | row                                                                                                                                                 | no                                 |
| `changeSetKind`             | row, kind (`warmup`, `working`, `failure`, `drop`, `myo`, `myomatch`), ids for generated segment rows                                               | no                                 |
| `setRowRest`                | row, seconds or nil (the per-set override)                                                                                                          | no                                 |
| `addExercise`               | slot key, exercise snapshot, rows (role and count from last time, else one working row), slot rests from last time                                  | yes                                |
| `removeExercise`            | slot                                                                                                                                                | yes                                |
| `reorderExercises`          | slot keys in the new order                                                                                                                          | yes                                |
| `replaceExercise`           | slot, new slot key, exercise snapshot                                                                                                               | yes                                |
| `groupExercises`            | slot, partner slot, group key (new or the partner's)                                                                                                | yes                                |
| `ungroupExercise`           | slot                                                                                                                                                | yes                                |
| `setGroup`                  | group, kind, pairing, round rest (each optional)                                                                                                    | yes                                |
| `setSlotRest`               | slot, which (`setRest`, `warmupRest`, `transition`, `sideSwitch`), seconds or nil (back to the default)                                             | yes                                |
| `setNote`                   | target (session or slot), text                                                                                                                      | yes                                |
| `pauseClock`, `resumeClock` | none                                                                                                                                                | yes                                |
| `startRest`                 | seconds (the toolbar's standalone timer)                                                                                                            | no                                 |
| `adjustRest`                | seconds, positive or negative (±15 from every surface)                                                                                              | no                                 |
| `skipRest`                  | none                                                                                                                                                | no                                 |
| `startTimedSet`             | row, target seconds or nil for a stopwatch                                                                                                          | no                                 |
| `stopTimedSet`              | row, record id, the values committed, segment row ids                                                                                               | no                                 |
| `cancelTimedSet`            | row                                                                                                                                                 | no                                 |
| `finish`                    | start, end, unticked rows' fate (`drop`, or `markDone` with each row's values), session RPE, notes, offset at the end                               | yes                                |
| `discard`                   | none                                                                                                                                                | not recorded                       |

**Resume is not an event.** Relaunching, returning to the foreground or opening the cover changes no data: the
controller loads the journal, folds it, and the coordinator re-applies notification and Live Activity (§6.3).

`EngineError` cases: `noSession`, `sessionActive`, `unknownRow`, `unknownSlot`, `rowNotPending`, `rowNotCompleted`,
`segmentBeforeItsLink` (a segment ticked before the link before it), `incompleteValues(missing:)`, `timerRunning`,
`noTimer`, `invalidOrder`, `groupTooSmall`, `finished`, and the app's own `unknownGroup`, `duplicateID`,
`kindNotAllowed`, `clockPaused`, `clockNotPaused` and `invalidEffort` (an RPE method the event cannot claim, §4.14).
The controller turns an error into a haptic and no change.

An event's payload must keep decoding old journals: a field added to it is optional or decodes with a default (the
nested types' `extra` decodes as empty, `SetValues.rpeMethod` and `SetValuesPatch.rpeMethod` as nil). The state is a
cache of the fold, so its shape may change freely.

### 4.3 Reducer rules

- **`start`.** Fails if a state exists. Builds the header (§3.5 fields known at Start; `revision` 1), the plan
  snapshot, slots from the routine's slots in order (`key` = routine slot key, `routineSlot` = same key), groups from the
  routine's groups (`madeInGym` false), and rows from the planned sets (§4.5). An empty session has no slots.
- **`touchRow`.** For a pending row whose earlier chain links are all completed: `focus = row`, and if
  `restClock.endedAt` is nil and no rest countdown is still running, `restClock.endedAt = at` (§4.8). The standalone
  timer is not the rest's countdown: it never holds the end back. Touching a completed row or a locked segment changes
  nothing.
- **`editSet`.** Pending row: merges into `entered`. Completed row: merges into `performed.values`, `edits += 1`, then
  re-checks its chain: an edit that now triggers a myo stop removes the pending generated segment; an edit that undoes a
  stop generates one (§4.9). A completed row's values must stay complete for the measurement. A set RPE takes the
  patch's method, `entered` when it names none, `copied` for PREVIOUS; `prefilled` and `imported` are refused
  (`invalidEffort`). A cleared RPE goes with its method and sets the row's `rpePrefillDeclined`; a set one clears it
  (§4.14).
- **`completeSet`.** Fails unless the row is pending, every earlier link of its chain is completed, and the values are
  complete for the measurement:

  | Measurement                  | Required                     |
  | ---------------------------- | ---------------------------- |
  | `weightreps`                 | load and reps                |
  | `repsonly`, `bodyweightreps` | reps                         |
  | `weightedbodyweightreps`     | added load (0 allowed), reps |
  | `bodyweightassistedreps`     | assistance and reps          |
  | `duration`                   | duration of at least 1 s     |
  | `weightduration`             | load and duration            |
  | `distanceduration`           | distance and duration        |
  | `shortdistanceweight`        | load and distance            |

  Then the RPE fields are checked (§4.14): a method with no RPE is dropped, an RPE with none is `entered`, `prefilled`
  must be the row's own pre-fill and `imported` is refused (`invalidEffort`). `performed` is set with the record id,
  `completedAt = at`, `startedAt = timerStartedAt`, the values, `completion = .individual`, and the rest fields from
  `restClock` (§4.8; none on the session's first tick); `priorRestClock` keeps the old clock. A row
  whose head was switched to Failure in the gym (its termination `failure`, its copied target's not) commits
  `reachedFailure = true` unless the values say otherwise, so its record reads as a failure set (§5.2); `markDone` at
  Finish does the same. A running
  timed countdown for this row is cleared. Chain segments are generated (§4.9). `lastCompleted = row`, `focus = nil`.
  The rest decision for this row (§4.7) sets `countdown` (when it has seconds) and a new `restClock` (`since = at`,
  `planned` = the decision's seconds or 0, `slot`).
- **`uncompleteSet`.** Only the last completed link of a chain, or a straight set. Its values return to `entered`, but a
  `prefilled` RPE does not: it was never the owner's, so the row shows its pre-fill again. Its rest fields are kept on
  the row and reused if it is ticked again, and generated segments after it are removed. When
  this tick started the running rest (it is `lastCompleted` and the countdown and clock began at its `completedAt`;
  instants are floored to the second, so the instant alone is not enough), that countdown is cancelled and
  `restClock = priorRestClock`; an older set's un-tick leaves the newer rest alone. `lastCompleted` falls back to the
  latest other completed row.
- **`addSet`.** A warm-up row goes after the slot's last warm-up; a working row after the slot's last row. No target.
- **`removeSet`.** Pending: removed. Completed: removed with its whole chain if it is a head, or with the segments after
  it if it is a segment. A countdown that started at a removed tick is cancelled. The rest clock walks back past every
  clock a removed tick started, to the one before the earliest of them, so the next set measures its rest from a set
  that still exists; among removed ticks of one second the one with the earliest prior clock goes first, which keeps
  the fold independent of row order. `removeExercise` does the same.
- **`changeSetKind`.** On a pending row sets `role`, `structure` and `termination` (`failure` sets termination
  `failure`; `working` restores the planned termination). `drop` creates its drop rows (planned `drops` or 1); `myo` and
  `myomatch` create nothing until the head is ticked. On a completed row only `warmup`, `working` and `failure` are
  allowed, and only `failure` also sets `reachedFailure`; a structure change on a completed head is allowed only while
  it has no completed segment, and generates the next segment at once.
- **`setRowRest`.** Sets the row's `restOverride`. Kept on the row only; it is not a session record property.
- **`addExercise`.** Appends a slot (or inserts it after the focused slot's group block when the picker was opened
  from an exercise menu) with the given rows. Rests from last time go into the slot's `rests`.
- **`removeExercise`.** Removes the slot and its rows. If its group falls below two members, the group is removed and
  the remaining member's `group` cleared.
- **`reorderExercises`.** The keys must be a permutation of the slots. After the move, each group's members are gathered
  at the position of its first member, keeping their new relative order, so a group stays contiguous.
- **`replaceExercise`.** If the slot has no completed row: the slot is replaced in place by a slot with the new key and
  exercise, keeping `group`, `routineSlot`, rests and notes; its pending rows keep their targets (03 §13: the new
  exercise uses its own history; a cold start may use the planned load). If it has completed rows: the old slot keeps
  only its completed rows, and a new slot with the pending rows is inserted right after it, in the same group and with
  the same `routineSlot`. The new slot also records the old slot's lane (`replaces`, state only, never a record): in a
  group the two count as one member, so the replacement's sets keep their rounds (§4.6) and a group made in the gym
  keeps its kind. The coverage session's bar pushdown in the rope pushdown's slot is the first case.
- **`groupExercises`.** Joins the slot to the partner's group, or makes a new group (key: the first unused lowercase
  letter, `madeInGym` true). The slot moves to just after the group's last member. The kind follows the member count
  when the group was made in the gym and never set by hand: 2 superset, 3 tri-set, 4 or more giant set, counting a
  replacement and the slot it replaced as one member.
- **`ungroupExercise`.** Clears the slot's group, moves it after the group's block; a group left with one member is
  removed.
- **`setGroup`, `setSlotRest`, `setNote`.** Store the value; mark the group `changedInGym`.
- **`pauseClock`.** Appends a pause `{start: at}`; fails if one is open. **`resumeClock`** closes it. A pause stops the
  session clock and nothing else: a running rest keeps counting, because rest is time since the last set (decision in
  §1.4; the examples count a phone call into `restBefore`).
- **`startRest`.** A standalone countdown (`kind .manual`), replacing any rest countdown. It does not touch `restClock`.
- **`adjustRest`.** On a rest or manual countdown: `endsAt = max(at, endsAt + seconds)`, so −15 never moves the end
  before now. `restClock.planned = endsAt − startedAt` when the countdown is the rest after the last tick.
- **`skipRest`.** Clears a rest or manual countdown; `restClock.endedAt = at` and `skipped = true` if not already ended.
- **`startTimedSet`.** The row must be pending and timed (§7.6 columns). Clears any rest countdown, ends the rest
  measurement (`restClock.endedAt = at` if nil), sets `timerStartedAt = at` and `countdown = .timedSet(row)` with
  `endsAt = at + target` (nil for a stopwatch). Fails if another timed set runs.
- **`stopTimedSet`.** Completes the row exactly as `completeSet`, with `duration = at − timerStartedAt` filled in the
  values unless the owner typed one. Reaching the target does nothing by itself: the count runs on past zero (req 31).
- **`cancelTimedSet`.** Clears `timerStartedAt` and the countdown.
- **`finish`.** Fails while a timed set runs. Clamps start to at most the first performed row's `startedAt` or
  `completedAt`, and end to at least the last `completedAt`. Unticked rows: `drop` removes them; `markDone` completes
  each with the given values at `completedAt = end`, in display order, with no rest fields and `completion = .bulk`
  (rows the controller could not give values to are dropped; an RPE whose method the row cannot claim is dropped, not
  the set). Then slots with no performed row are removed (groups falling below two members go
  too), the open pause is closed at the end, `sessionRPE` and notes are stored, `phase = .finished`, revision + 1.
- **`discard`.** Valid only while active; the controller deletes the journal (the state ceases to exist).

### 4.4 Invariants

Checked by a debug assertion after every `apply`, and by `EngineTests` on every scenario:

1. At most one active session exists (the controller keeps one journal).
2. Slot keys are unique; each row belongs to exactly one slot and appears once in its `rows`.
3. Within a slot, warm-up rows come first; each chain head is followed directly by its segments in `chainPosition`
   order.
4. Group members are contiguous in display order; every group has at least two members; group keys are unique.
5. A segment is completed only after every earlier link of its chain.
6. A chain has at most one pending generated myo, myo-match or rest-pause segment, always the last.
7. At most one countdown exists; a timed countdown names a pending row whose `timerStartedAt` is set.
8. Every completed row has a record id, a `completedAt` no earlier than the session's start, and values complete for
   its measurement.
9. Event instants never go backwards: the controller clamps a new event's `at` to the previous event's.
10. `fold(events) == state`.

### 4.5 Rows from a routine

For each routine slot, each planned set becomes one row, in order, then known segments are added:

- `role`, `roleLabel`, `structure`, `termination`, `restOverride` (`rest`), `intraSetPause` and `timerMode` are copied.
- `target` is copied from the planned set, every field but `key` (which becomes `plannedSet`), `role`, `roleLabel`,
  `structure` and `rest`. Unknown fields stay on the planned set and its session snapshot: `set.target` declares other
  fields, and the server refuses an undeclared one. `termination` = the planned one, else the one its targets imply
  (`reps` when `repsMin == repsMax`, `reprange` when both differ, `amrap` when only `repsMin` is set on an AMRAP, `time`
  when only a duration is set, `distance` when only a distance is set); `repsMin`, `repsMax`, `load`, `loadPercent`,
  `loadReference`, `assistance`, `rpeMin`, `rpeMax`, `rirMin`, `rirMax`, `durationMin`, `durationMax`, `timerMode`,
  `distance`, `tempo`, `miniSetReps`, `miniSets`, `myoStopRule`, `totalReps`, `drops`, `dropPercent`, `dropStep`,
  `dropReps`, `intraSetPause`. A myo-rep match's `totalReps`, when the plan leaves it out, is resolved when its head is
  ticked: the reps of the slot's previous working set (§4.9). The record adds what the engine ran where the plan is
  silent (§3.5).
- **A percentage load** (D5): `target.load` is always the absolute load. A planned `load` wins over `loadPercent`. With
  `loadReference: bodymass` and a body mass at Start, `load` = body mass × `loadPercent` / 100 in the body mass's unit
  and `referenceLoad` = the body mass. V1 keeps no one-rep max or training max, so a percentage of one stays
  unresolved: `loadPercent` and `loadReference` are copied, `load` and `referenceLoad` are absent, and the suggestion
  picks a load (03 §11.4).
- **Known segments are created with their head:** for a drop set, `drops` (default 1) rows with `structure: drop`,
  `chainPosition` 2, 3, and target `{plannedSet, repsMin = repsMax = dropReps}` when `dropReps` is set; for a cluster,
  `miniSets` rows with target `{plannedSet, repsMin = repsMax = miniSetReps}` (the head is the first cluster: with no rep
  range of its own, the suggestion reads its target as `miniSetReps` too, §5.2); for a set on a `unilateral` exercise, one
  second-side row with target `{plannedSet}` (§4.9). Generated myo, myo-match and rest-pause mini-sets get
  `{plannedSet}`, plus `repsMin = repsMax = miniSetReps` when the plan sets it. This is the examples' shape: a chain's
  later records carry the planned set's key and only their own targets.
- An added row (`addSet`, an added exercise) has no `plannedSet` and no target.

### 4.6 Up next

Up next is the one row the session points at: the Live Activity, the notification, the rest bar and the suggestion's
reason line all describe it. It is recomputed after every event.

**The plan sequence.** Lay the session's rows in one sequence:

- Slots in display order. A plain slot contributes its rows in display order (warm-ups, then each working head followed
  by its segments).
- A group contributes one block: first every member's warm-up rows (member order), then round 1, round 2 and so on. Round
  `r` holds, for each member in order that has an `r`-th working head, that head and its segments. A member with
  fewer working sets drops out of the later rounds (02 §3.3; with A 4 sets, B 3, C 2: A, B, C, A, B, C, A, B, A). A
  member is a lane: a slot that replaced another mid-exercise (`replaceExercise` with done sets) continues that slot's
  lane, its working heads numbered after the old slot's, so A1, B1, A2, B2 and then a replacement's set is round 3. A
  row's round is this number; outside a group it is the head's ordinal.

**The rule.**

1. If `focus` is a pending row, it is Up next (the owner chose it).
2. Else, with the anchor `lastCompleted`, Up next is the first pending row after the anchor in the plan sequence,
   wrapping around to the start; with no anchor, the first pending row of the sequence.
3. If no row is pending, there is no Up next and the session shows "All sets done" with Finish.

Because segments follow their head and rounds interleave members, rule 2 gives req 23's order: the rest of the chain,
then the next member of the round (or the first member of the next round), then the next set of the same exercise, then
the next exercise with an unfinished set. A segment whose earlier link is pending is skipped by rule 2 (it cannot be
ticked yet).

**The expected next set** used by the rest rule (§4.7) is rule 2 alone, with the just-completed row as the anchor,
ignoring `focus`.

### 4.7 Rest selection

When a row C is completed, `RestRules.after(C, state) -> RestDecision { slot: RestSlot?, seconds: Seconds, source:
Source, notifies: Bool }` picks one rest, from C and the expected next row N (§4.6). The first matching line wins:

| # | Condition                                                                            | Rest slot       | Value, first present wins                                                                                                                            |
| - | ------------------------------------------------------------------------------------ | --------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| 0 | No row is pending (N is nil)                                                         | none            | 0: no timer; "All sets done"                                                                                                                         |
| 1 | C's chain is complete and C's chain head has a `restOverride`                        | `override`      | the head's `restOverride`                                                                                                                            |
| 2 | N is the next link of C's chain, a side                                              | `sideSwitch`    | session slot `sideSwitch`, routine slot `sideSwitch`, default 0                                                                                      |
| 3 | N is the next link of C's chain, any other structure                                 | `intraSetPause` | the head's `intraSetPause`, else by structure: drop 0, myo 15 s, myomatch 15 s, restpause 20 s, cluster 20 s                                         |
| 4 | C is a warm-up                                                                       | `warmupRest`    | session slot, routine slot, settings warm-up rest (default 45 s)                                                                                     |
| 5 | C and N are in the same group block and N is in the same round as C (another member) | `transition`    | C's session slot, C's routine slot, default transition below                                                                                         |
| 6 | C and N are in the same group block, N in another round                              | `roundRest`     | session group, routine group, default round rest below                                                                                               |
| 7 | N is in another slot or group block than C                                           | `blockRest`     | if the "rest after an exercise's last set" setting is off: 0. Else C's group's round rest (as line 6) if C is grouped, else C's set rest (as line 8) |
| 8 | Otherwise (N is in C's own plain slot)                                               | `setRest`       | session slot, routine slot, settings default for C's exercise's `restClass`, else the settings fallback                                              |

Defaults (02 §4.3 and §4.4; editable ones are in Settings and snapshotted at Start):

| Rest                                  | Default                                                                                                                                                                                        | In Settings |
| ------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------- |
| Warm-up rest                          | 45 s                                                                                                                                                                                           | yes         |
| Set rest by rest class                | heavycompound 3:00, compound 2:00, isolation 1:30, highrep 1:00, timed 1:00, carry 2:00, power 3:00                                                                                            | yes         |
| Set rest, exercise with no rest class | 2:00 (02 §4.2: err long)                                                                                                                                                                       | yes         |
| Transition                            | by `pairing`: samemuscle, preexhaust, mechanicaldrop 0; antagonist, upperlower, unrelated 15 s. No pairing: `paired` 1:00; `triset`, `giantset`, `circuit` 15 s; `superset` 0 (disagreement 9) | no          |
| Round rest                            | `circuit` 2:00; any other group 2:30 when a member's rest class is heavycompound, compound, power or carry, else 1:30                                                                          | no          |
| Intra-set pause                       | drop 0 (no timer), myo and myomatch 15 s, restpause and cluster 20 s                                                                                                                           | no          |
| Side switch                           | 0                                                                                                                                                                                              | no          |
| Rest after an exercise's last set     | on                                                                                                                                                                                             | yes         |

A value of 0 (`PT0S` in a routine, "Off" in the app) means no countdown: Up next simply moves on, and the screen
scrolls to the next member when superset auto-scroll is on (§7.6). A per-set override is a rest after the whole set, so
it applies only once its chain is complete (line 1).

**Notifications by slot** (disagreement 8): `override`, `warmupRest`, `roundRest`, `blockRest`, `setRest` and `manual`
countdowns notify; `transition`, `intraSetPause` and `sideSwitch` notify only when longer than 20 s. Shorter ones end
with a haptic only, since the phone is in the lifter's hand.

The rule reproduces every `restPlanned` in the examples; `RestRuleTests` replays them (§10.5). For instance, in the
27 October session: plank 2 after plank 1 is `setRest` 15 s; crunch 1 after plank 2 is `blockRest` 15 s; bird dog 1
after glute bridge 1 is `transition` 0; glute bridge 2 after bird dog 1 is `roundRest` 1:00; Pallof 1 right after
1 left is `sideSwitch` 10 s; chest press 1 after its warm-up is `warmupRest` 45 s; chest press 2a after 2 is
`intraSetPause` 0.

### 4.8 Rest measurement

Each set record carries `restBefore`, `restPlanned` and `restSkipped` (req 9). The engine keeps one `restClock`:

- It starts at every tick: `since` = the tick, `planned` = the decision's seconds (0 when no timer ran), `slot`.
- It ends (`endedAt`) at the first of: Skip; a timed set's start; a `touchRow` on a pending row once the rest's
  countdown has ended or when none ran (the standalone timer does not count); the next tick. Touches while a rest
  countdown still runs do not end it, because owners type the next load during rest (Hevy's screenshots show it); the
  countdown keeps counting whatever row is touched (req 24).
- The next completed row takes `restBefore = (endedAt ?? its completedAt) − since`, `restPlanned = planned` (after any
  ±15 s), `restSkipped = skipped`.
- The session's first performed row, and rows marked done at Finish, carry no rest fields.

A rep set ticked with no touch first includes its own working time in `restBefore`. That is the documented limit of a
set with no recorded start (the examples README says the same).

### 4.9 Chains

A structured set is a chain of rows, one record each: the head (`chainPosition` 1) and its segments, which name the
head (`continues` on the record). All records of a chain share the head's `structure`, `ordinal`, `plannedSet` and
`role`.

| Structure         | Segments            | Created                        | Segment target and ghost                                                                                                                                                                                           | Ends                                                                                                                                                   |
| ----------------- | ------------------- | ------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `drop`            | `drops` (default 1) | with the head                  | Load from 03 §11.7 on the parent's performed load, or on the load its row shows while the parent is pending (`dropStep` wins over `dropPercent`, then the exercise's, then 20 %); reps `dropReps`, else to failure | After the last drop. If 03 §11.7 finds no lighter load when the parent is ticked, the remaining drop rows are removed ("No lighter load for a drop").  |
| `myo`             | mini-sets           | one at a time, after each tick | 03 §11.6: load the activation's, reps `t1` then `m1`                                                                                                                                                               | 03 §11.6 stop: cap `miniSets` ?? exercise `myoMaxMiniSets` ?? 5, rule planned ?? exercise ?? `repdrop`, `totalReps`                                    |
| `myomatch`        | mini-sets           | one at a time                  | Load the head's; reps `min(miniSetReps ?? t1, T − done)`                                                                                                                                                           | When head reps + mini-sets reach `T`, the reps of the slot's previous working set (resolved into `target.totalReps` at the head's tick), or at the cap |
| `restpause`       | mini-sets           | one at a time                  | Load the head's; reps `miniSetReps`, else open                                                                                                                                                                     | At `miniSets` (default 2) or `totalReps`                                                                                                               |
| `cluster`         | `miniSets` clusters | with the head                  | Load the head's; reps `miniSetReps`                                                                                                                                                                                | After the last cluster                                                                                                                                 |
| side (unilateral) | the second side     | with the head                  | First side by `sideOrder` (left by default; `weakerfirst`: the side with fewer reps last time, else left). Second side: the same load; reps the first side's when `sideMatch`, else the same target                | After the second side                                                                                                                                  |

- A `myo`, `myomatch` or `restpause` head ticked generates the first mini-set row unless the stop rule already holds
  (the cap is 0, or a total is met). Each mini-set tick generates the next one the same way. The rows are generated from
  the event's pre-minted ids.
- Side chains apply to exercises whose `laterality` is `unilateral`; their heads carry `side` and `structure: straight`.
  An `alternating` exercise logs one record per set, its reps counting per side.
- A structured set on a unilateral exercise is not supported in V1: the side chain wins and the structure is ignored for
  the chain (rare; the data cannot nest chains).

### 4.10 Labels, order and ordinals

**Ordinal** (`ordinal` on the record): the row's number among its slot's rows of the same role, in display order, a
chain counted once (its segments share the head's ordinal). During the session it counts every row, so the grid shows
1, 2, 3 as planned; at Finish it is recomputed over performed rows only.

**Order** (`order`): the row's place among the slot's performed rows in display order, from 1, each record of a chain
counting.

**Label** (req 37), one function, `SetLabel.make(row, slot) -> (short: String, spoken: String)`:

| Row                                 | Short      | Spoken (Live Activity, notifications, VoiceOver)           |
| ----------------------------------- | ---------- | ---------------------------------------------------------- |
| Warm-up                             | `W`        | `warm-up 2` (just `warm-up` when the slot has one)         |
| Working head                        | `2`        | `set 2`                                                    |
| Drop, rest-pause or cluster segment | `2a`, `2b` | `set 2, drop 1` / `set 2, mini-set 1` / `set 2, cluster 2` |
| Myo or myo-match mini-set           | `M`        | `set 2, mini-set 1`                                        |
| Side record                         | `1L`, `1R` | `set 1, left side`                                         |

A failure or AMRAP set keeps its number and shows a red `F` badge beside it; a top or back-off set shows `Top` or
`Back-off` in small text under the number.

### 4.11 Counting

- **Sets** (header and summaries): completed working chain heads, labelled "Working sets" in the header and the Finish
  sheet. A chain and a side pair count once. Warm-ups never. At Finish they are counted on the projected state
  (§7.7), so marked-done rows count as History will count them.
- **Volume load**: for each completed record with an effective load (03 §10.4, with the session's body mass),
  `reps × effective load`, times `implements` for a `perimplement` load, times 2 for a `perside` load unless the set
  is `unilateral` (bilateral and alternating sets alike). Reps count as logged: `repsBasis` is not read, so an
  alternating set's per-side count is not doubled (10 a side with a pair of 10 kg dumbbells is 200 kg), and an
  imported alternating set counted in `total` reps counts as given. `loadBasis`, `implements`, `laterality` and
  `bodyShare` are the set's own copies (§3.5), the exercise's only for a set written before they existed, so editing
  an exercise changes no past volume. Warm-ups are left out unless the "Count warm-ups in volume" setting is on (req 32). No-load measurements and
  bodyweight sets without a body mass add nothing. Shown in the default unit, rounded to the nearest whole unit.
- **Working sets per muscle** (session detail): 1 per primary muscle, 0.5 per secondary, per counted set (02 §6.2). A
  derived view, never stored.

### 4.12 Revisions

The session's `revision` is 1 at Start and rises by one with each event that changes a property of the session record
(the right-hand column of §4.2), with Finish, and with the Health link. That is what V2 will send, one write per
change. A set's `revision` is `1 + edits`. V1 renumbering `order` and `ordinal` at Finish does not raise it, because no
earlier version left the phone; V2 will raise it with its PATCH.

### 4.13 Formatting

One formatter module, used everywhere (req 37; 06 §5 item 14):

- **Durations**: `m:ss` under an hour (`0:53`, `1:30`), `h:mm:ss` from an hour (`1:02:05`); `Off` for a rest of 0. The
  same formatter for rests, timed sets, the session clock and summaries.
- **Loads** (03 §12): at most two decimals, trailing zeros removed, a space, the unit: `35 kg`, `92.5 kg`, `190 lb`.
  Bodyweight `BW`, added load `BW + 2.5 kg`, assistance `BW − 30 kg` (U+2212).
- **A set**: `{load} × {reps}` with U+00D7; reps only `× 15`; timed `1:02`, with load `10 kg for 1:02`; distance
  `40 m`, with load `20 kg for 40 m`; RPE added after a space as `@ 9.5` in PREVIOUS and history, a rated RPE only
  (§4.14).
- **RPE**: at most one decimal, trailing zero removed: `9.5`, `8`.
- **Ranges**: en dash, `10–12`. An effort target: `RPE 8`, `RPE 7–8`, `2–3 reps in reserve`, `1 rep in reserve`
  (`Format.effort`). A percentage load: `80% of training max`.

### 4.14 RPE provenance and the pre-fill

The Rows session screen shows the plan's RPE pre-filled on the set being logged, and one tap logs the set with it.
Each set record says how its RPE got there (`rpeMethod`, D1), and nothing decides that by comparing values: the event
says it.

- **Untouched, ticked**: `completeSet` with `state.acceptingRPEPrefill(values, row:)` logs the plan's value as
  `prefilled`.
- **A value tapped**, even the one already shown: `editSet` with `SetValuesPatch.enteringRPE(_:)` (or `rpe: .set`
  with no method) logs `entered`.
- **PREVIOUS tapped**: `editSet` with `previous.copy`, whose rated RPE carries `rpeMethod: .copied`, logs `copied`. A
  tap after it makes it `entered`.
- **Cleared**: `editSet` with `SetValuesPatch.clearingRPE` logs no RPE and no method.
- **The Table's RPE sheet**: Done on a value sends `SetRowPresentation.rpePatch(for:)`, which is `enteringRPE` even on
  the value the sheet opened on (a copied value, or one logged pre-filled in Rows before the layout changed), and
  nothing only when the row already holds that value as `entered`. Clear sends `clearingRPE` when the row holds a
  value, else nothing.

- **The pre-fill.** `state.rpePrefill(ofRow:)` is the top of the row's target effort range (`SetTarget.effortTop`:
  `rpeMax`, else `10 − rirMin`; a second side reads its head's). It is nil for a completed row, a row holding an RPE of
  its own, a row whose RPE the owner cleared (`rpePrefillDeclined`), and a row whose plan names no effort (an added
  set, a drop, a mini-set). `state.rpe(ofRow:)` gives the cell: `.none`, `.prefill(x)` or `.value(x, method)`.
- **Accepting it.** `acceptingRPEPrefill` puts the pre-fill into values that hold no RPE, as `prefilled`; values with
  an RPE pass unchanged. With `PresenterSettings.prefillsRPE`, the presenter does this for every pending row's
  `commitValues` and for "Mark them done as shown", so a tick, the Live Activity's Done and Finish all log what the
  screen showed. Off, nothing is pre-filled (the grid screen, where RPE is never ghosted).
- **Checks.** On a commit (`completeSet`, `stopTimedSet`, a row marked done) an RPE with no method is `entered` (a
  journal from before the field, whose RPE could only be tapped), a method with no RPE is dropped, `prefilled` must
  equal the row's own pre-fill, and `imported` is refused. An edit may say only `entered` or `copied`.
- **Untick.** A `prefilled` RPE goes back to being the pre-fill; an `entered` or `copied` one stays in the row.
- **Clearing** removes `rpe` and `rpeMethod` together, and on a pending row stops the pre-fill until a value is tapped.
- **Reading.** Only `entered` and `imported` values are ratings (`EffortMethod.isRating`; absent beside a value reads
  as one, a method this build does not know as none), and only on an RIR-based scale (`rirbased`, `hevy`, `strong`,
  or none written; not `cr10` or an unknown scale): `PerformedSet.ratedRPE`. The suggestion reads a pre-filled or
  copied RPE as none (§5.2), keeping the method of the field it came from (`unratedRPE`); PREVIOUS and History show
  and copy ratings only; e1RM and the exercise trend take ratings only.

## 5. Suggestions, PREVIOUS and ghost values

### 5.1 The function

WorkoutsCore implements research 03 Part B (§8 to §13) exactly, as one pure function. Its types are 03 §9's, kept apart
from the record model so the engine is testable from 03's vectors alone:

```swift
public enum SuggestKind: Sendable { case warmup, normal, failure, drop, myoActivation, myoMini, timed }
public enum LoadMode03: Sendable { case external, bodyweightAdded, bodyweightAssisted, none }

public struct ExerciseConfig: Sendable {          // 03 §9.1
    public var unit: MassUnit
    public var mode: LoadMode03
    public var bodyShare: Double                  // 1.0
    public var base: Double?                      // nil: no load grid (see §5.3)
    public var step: Double?
    public var maxLoad: Double?
    public var canAddLoad: Bool                   // true
    public var defaultRepsMin = 8, defaultRepsMax = 12
    public var defaultRPE = 8.0
    public var repGain = 1
    public var dropPercent = 0.20
    public var timeStep = 5
    public var myoMaxMiniSets = 5
    public var myoStopRule: MyoStopRule = .repdrop
}
public struct Target03: Sendable {
    public var repsMin, repsMax: Int?
    public var rpe: Double?                       // the top of the plan's effort range: rpeMax, else 10 − rirMin
    public var load: Double?                      // in the exercise unit; added load or assistance in bodyweight modes
    public var durationMin, durationMax: Int?
    public var dropPercent: Double?
    public var dropStep: Double?
    public var myoMiniReps, myoTotalReps: Int?
    public var openTop: Bool                      // AMRAP or failure with no repsMax: hi unbounded (03 §13)
}
public struct DoneSet: Sendable {                 // 03 §9.2
    public var kind: SuggestKind
    public var setup: String?
    public var load: Double?
    public var unit: MassUnit
    public var reps: Int?
    public var repsLeft, repsRight: Int?
    public var duration: Int?
    public var rpe: Double?                       // a rating only
    public var unratedRPE: Double?                // a pre-filled or copied RPE: read as none, and said "assuming"
    public var reachedFailure: Bool
    public var target: Target03
    public var completedAt: Date
    public var position: Int
    public var ordinal: Int?                      // working ordinal; warm-up ordinal for warm-ups
}
public struct PastSession: Sendable { public var start: Date; public var bodyMass: Double?; public var sets: [DoneSet] } // 03 §9.3, kg body mass
public struct PendingSet: Sendable {              // 03 §9.4
    public var kind: SuggestKind
    public var ordinal: Int                       // k, or the warm-up ordinal for a warm-up
    public var target: Target03
    public var parent: DoneSet?                   // drops: the set before; mini-sets: the activation
    public var miniSetsDone: [Int]                // reps of earlier mini-sets in this chain
}
public struct Suggestion: Sendable, Equatable {  // 03 §9.5, verbatim
    public var load: Double?
    public var reps: Int?
    public var duration: Int?
    public var targetRPE: Double?
    public var stop: Bool
    public var reason: Reason
    public var text: String
}
public enum Reason: Sendable { case warmupAsLastTime, historyAddRep, historyLoadUp, historyLoadDown, historyNewRange,
    historyAtMax, sessionKeep, sessionUp, sessionDown, sessionCoarse, sessionAtMax, coldStartRoutineLoad,
    coldStartPickLoad, myoMini, myoStop, drop, dropAtMinimum, timedHistory, timedSession, timedLoadUp, timedColdStart,
    backoff }                                     // `backoff` is the app's (§5.3)

public func suggest(pending: PendingSet, exercise: ExerciseConfig, today: [DoneSet], history: [PastSession],
                    bodyMass: Double?, now: Date, calendar: Calendar) -> Suggestion?
```

The building blocks are 03 §10 to §12 with their constants and tolerances (the percentage curve `P`, `p`, `pInv`, the
effective RPE, effective load, the grid's `floorG`, `next`, `prev`, `repsAt`, evidence selection with the 180-day,
five-session plausibility test, the fatigue allowance, the 10 % raise cap, the long-break cuts, every explanation
template). `calendar` (the owner's time zone) only counts whole local days between a past session's start and `now`.
The engine is re-run after every change and never commits anything (req 29).

### 5.2 Building the inputs

`SuggestionInputs` (in `Present/`) builds the arguments from the session state, the catalogue and the history index:

- **`ExerciseConfig`** from the exercise. `unit` = `loadStep`'s unit, else the default unit setting. `mode`:
  `weightreps`, `weightduration`, `shortdistanceweight` are `external`; `weightedbodyweightreps` is `bodyweightAdded`
  with `canAddLoad` true; `bodyweightreps` is `bodyweightAdded` with `canAddLoad` false; `bodyweightassistedreps` is
  `bodyweightAssisted`; `repsonly`, `duration`, `distanceduration` are `none` (03 §11.5 and §11.8 apply). `base`, `step`,
  `maxLoad` from `loadBase`, `loadStep`, `loadMax` converted to the unit, else the defaults below. `dropPercent / 100`,
  and the other settings with 03 §9.1's defaults.

  | Equipment or measurement                                 | Base, step (kg) | Base, step (lb) |
  | -------------------------------------------------------- | --------------- | --------------- |
  | `barbell`                                                | 20, 2.5         | 45, 5           |
  | `dumbbell`                                               | 0, 2            | 0, 5            |
  | `machine`, `cable`                                       | 0, 5            | 0, 10           |
  | `kettlebell`                                             | 0, 4            | 0, 5            |
  | `plateloaded`                                            | 0, 2.5          | 0, 5            |
  | `weightedbodyweightreps` (any equipment)                 | 0, 2.5          | 0, 5            |
  | `bodyweightassistedreps` (any equipment)                 | 0, 5            | 0, 10           |
  | `none`, `plate`, `resistanceband`, `suspension`, `other` | no grid         | no grid         |

- **Kinds** (package README, "Reading"): `warmup` from the role; a segment of a drop chain is `drop`; a segment of a
  myo, myo-match, rest-pause or cluster chain is `myoMini`; the head of a myo or myo-match chain is `myoActivation`;
  `timed` for `duration` and `weightduration` exercises; `failure` when `reachedFailure`, or the target's termination
  is `failure` or `amrap` (then `openTop` when `repsMax` is absent); else `normal`. A set switched to Failure in the gym
  keeps its copied target, so its tick records `reachedFailure` (§4.3) and history reads it as `failure`; History's F
  badge follows `reachedFailure` too. A pending row with no target (added in the gym) switched to Failure has an open
  top as well.
- **Done sets** fold chains first: a side pair becomes one set with `repsLeft` and `repsRight` and the higher rated
  RPE; drop and mini-set records stay separate sets of their kinds. `rpe` is the set's rating (`ratedRPE`, §4.14):
  `rpe` when its method is `entered`, `imported` or absent, else `10 − rir` when that is a rating and the scale is
  RIR-based (`rirbased`, `hevy`, `strong` or none; never `cr10`). A pre-filled or copied RPE is no rating: the done set
  has no `rpe`, so 03 §10.2 takes the target's top (or 10 when the set fell short), and keeps the value as
  `unratedRPE` for the text. Today's rows read the same way. Values from an imported `rpeScale` are passed unchanged. Loads are converted to the exercise unit (1 lb = 0.45359237 kg, rounded to 0.01;
  03 §13). In bodyweight modes `load` is the added load or the assistance.
- **Target** from the row's copied target: the top of its effort range (`rpeMax`, else `10 − rirMin`; a range of RPE
  7 to 8 aims at 8), converted to the exercise unit. A cluster set's head
  is its first cluster: when its target has neither `repsMin` nor `repsMax`, both read as `miniSetReps`, today and from
  history, so the head is suggested (and committed at) the clusters' reps rather than the exercise's default range,
  and the clusters after it take its load.
- **Today** is this session's completed rows of the same exercise; **history** is the history index's sessions for the
  exercise with the same setup (V1: sets with no `setup`), newest first, excluding the active session, at most the six
  newest within 180 days of now (03 DI 7). Deload sessions are left out of the history and the plausibility median (the
  kind says suggestions discount them; 03 has no finer rule); today's sets count even in a deload session. PREVIOUS
  still shows a deload session when it was the last time, marked "deload", and the e1RM trend draws its point hollow.
- **Body mass**: the session's, in kg.

### 5.3 the app's rules around 03

03 Part B does not cover these; they are small and labelled as the app's:

- **Back-off sets.** A pending working row with `roleLabel: backoff` and a `dropPercent` or `dropStep` in its target,
  after a completed working set of the same slot: load `floorG(parent × (1 − d))` or `floorG(parent − dropStep)`
  (`prev(parent)` if not lighter), reps `min(max(repsAt(L, E, R), lo), hi)` with `E` from 03 §11.2's in-session estimate
  of that parent. Reason `backoff`, text `Back-off {d}% from {parent load}: try {new}` (or `Back-off {step} from
  {parent load}: try {new}`). The coverage session's 110 kg top set then gives 95 kg.
- **Rest-pause and cluster mini-sets** reuse 03 §11.6 with the rule switched off: they stop only at the cap or the total;
  rest-pause reps are `miniSetReps` or open; cluster reps are `miniSetReps`. Mini-sets never feed the e1RM.
- **Myo-rep match** passes `myoTotalReps = T − head reps` and `myoMiniReps = miniSetReps` into 03 §11.6.
- **`rirstop`** (stop at the effort target): the load is suggested as usual; the reps are only a hint, shown as `≈15`, and
  the tick does not commit them: the owner types the reps done.
- **No grid** (equipment with no default step and no `loadStep`): every rule that moves a load keeps the load and works on
  reps as 03 §11.5 does when no heavier or lighter setting exists. A drop is the exception: the app cannot know a
  band's or a suspension trainer's lighter setting, so the drop leaves the load to the owner (`Drop from 20 kg: a
  lighter load, to failure`, no load suggested) and never reports "No lighter load", which would remove the planned
  drops.
- **"Assuming RPE x".** 03 §12's `{prev}` adds `at RPE {e}` only for a rated RPE. A set that carried a pre-filled or
  copied RPE, read as none (§5.2), adds `, assuming RPE {e}` with the effective RPE the rule used, never "at": the 26
  October leg press set 2 reads "Set 1: 105 kg × 9, assuming RPE 8; try 105 kg × 9" (the examples README). A set with
  no RPE at all keeps 03's text.
- **History's body share.** A past set's effective load uses the exercise's current `bodyShare`, as 03 §10.4 does; the
  set's own copy (§3.5) is what volume reads (§4.11).

### 5.4 PREVIOUS

PREVIOUS is the same row last time, wherever the exercise was done (lead decision; req 28):

- The newest finished session in the history index that has a performed set of this exercise with the same setup (V1:
  none), excluding the active session.
- The row is matched by role and ordinal, then chain position and side: today's working set 2 shows last time's working
  set 2; today's `2a` shows last time's `2a`; `1R` shows last time's `1R`; a warm-up shows the warm-up with the same
  warm-up ordinal. When today has two slots of one exercise, the n-th slot matches last time's n-th slot of it.
- Shown in the set grid's PREVIOUS column with RPE (`35 kg × 13 @ 9.5`), a rated RPE only: a set logged with the
  plan's pre-filled RPE shows `105 kg × 9`. Tapping it copies its values into the row's entered values (`editSet`), a
  rated RPE with `rpeMethod: copied`, which a later tap on the RPE makes `entered` (§4.14).

### 5.5 Ghost values

Each field of a pending row shows, in grey, the first of (req 28):

| Field                        | Ghost source, first present wins                                                                   |
| ---------------------------- | -------------------------------------------------------------------------------------------------- |
| load, added load, assistance | the suggestion's load; the target's load or assistance; PREVIOUS                                   |
| reps                         | the suggestion's reps; the target's (`repsMin` when it equals `repsMax`, else `repsMin`); PREVIOUS |
| duration                     | the suggestion's duration; the target's `durationMin`; PREVIOUS                                    |
| distance                     | the target's distance; PREVIOUS                                                                    |
| RPE                          | never ghosted; the Rows screen's pre-fill (§4.14) is the plan's value, not a ghost                 |

A typed value replaces the ghost in black. The tick commits what the row shows, typed or ghost, except a `rirstop` hint
(§5.3). If a required field (§4.3) has neither, the tick is disabled and that field is outlined.

### 5.6 Where the output appears

- **Set rows**: the ghost values (§5.5).
- **Reason line**: one line of secondary text under the Up next row only, with the suggestion's `text` (03 §12). A tap on
  any other pending row's ghost value shows that row's text in a small popover.
- **Rest bar** and **Live Activity**: "Next: Chest Press (Machine), set 1: 40 kg × 11", from the Up next row's ghost
  values.
- **Notification** body: the same line.
- **Chains**: `stop` on a mini-set ends the chain (§4.9); the stop text ("Stop: mini-set 4 got 3, first was 4") shows
  in place of the reason line for a moment after the tick.

### 5.7 Entry validation

Before a working set with an effective load is committed (tick, Stop or Done), the controller runs 03 §10.7's
plausibility test on it: the set's e1RM against the median session-best e1RM of the five newest other sessions within
180 days, when there are at least three. Outside 0.5 to 1.5 times, a dialog asks before the event is sent (req 30):

- Title: "500 kg × 8 is far from your recent sets"; message "Recently about 35 kg × 12."
- If dividing or multiplying the load by 10 lands inside the range, the first button offers it: "Use 50 kg".
- Then "Keep 500 kg" and "Edit".

## 6. The app layer

### 6.1 SessionController

`@MainActor @Observable final class SessionController`, one instance (`SessionController.shared`), created at app
launch or lazily when a Live Activity intent launches the app in the background.

- Holds the catalogue, the history index, the settings, the journal, the active `SessionState?` and its
  `SessionPresentation`.
- `func send(_ command: SessionCommand) async` is the only way anything changes a session. Views, intents and notification
  actions call it. A command is the event minus what the controller adds: ids (UUIDv7), the instant (floored to the
  second, clamped to the last event's), and values that need history (ghost values for a tick or Done, an added
  exercise's rows from last time).
- After a successful `apply`, in this order (04 §2.4): bump the effects revision; write the journal (before any
  `await`); recompute the presentation; `await coordinator.sync(state, presentation, revision)`; `await sync.applied(...)`
  in a detached task.
- `start(routine:)` and `startEmpty()` check for an active session first (the conflict dialog, §7.4), read the body
  mass (§9.5), request notification permission the first time, and start the Live Activity in the foreground.
- `finish(...)` applies the `finish` its `FinishPresenter.projection` builds for the dialog's choice (the one the sheet
  showed, §7.7), builds `SessionRecords`, writes the history file, writes the Health queue entry
  (before the journal goes and before any `await`, so no death in between can lose it), deletes the journal, ends the
  Live Activity, clears the notification, and starts the Health save without awaiting it. The save waits until the
  cover and the Finish sheet have closed (about 0.7 s), because the first Finish may show the permission sheet; over a
  cover that opened again for the next session it saves without asking. Until the cover is gone it keeps drawing the
  session as it was, never "No session in progress".
- `discard()` applies `discard`, deletes the journal, ends the Live Activity at once, clears the notification.

### 6.2 CountdownCoordinator

The only code that touches the notification and the Live Activity (req 25). `sync(state, presentation, revision)`:

1. Remember `mine = revision`.
2. Remove the pending and delivered notification `workouts.countdown`; add a new one if a countdown has a future end and
   notifies (§4.7, §9.2). (V1.1 inserts AlarmKit here: cancel the old alarm before any `await`, schedule the new one
   after.)
3. After each `await`, stop if `mine` is no longer the latest revision.
4. Update the Live Activity with the new content, `staleDate` = the countdown's end, and `timestamp:` = the event's
   instant, so ActivityKit drops a late, older update (04 §2.4).

`restore(state)` runs the same steps at launch and on every return to the foreground, so whatever happened while the
app was suspended or dead, the alert and the Live Activity match the journal again.

### 6.3 Launch, foreground, background

- **Launch**: build the catalogue (bundled plus local exercises), start loading history in the background, load the
  journal and fold it. If a session is active: show the in-progress accessory, `coordinator.restore`, ensure a Live
  Activity exists unless the owner dismissed this session's (`activityStateUpdates` reported `.dismissed`), and present
  the session cover when the launch is in the foreground.
- **Background launch by an intent**: the same load, no UI; the intent's command is handled once the journal is
  folded.
- **`scenePhase == .active`**: `restore`; clear an expired rest without a second haptic (the notification already
  alerted); re-read notification settings so the session screen can say when alerts are off; process the Health queue.
- **`.background`**: nothing; the journal is written and the alerts are with the system.

### 6.4 Settings

Stored in `UserDefaults` under `workouts.*` keys, read through one `Settings` struct:

| Setting                                   | Default |
| ----------------------------------------- | ------- |
| Default unit (`kg`, `lb`)                 | kg      |
| Warm-up rest                              | 45 s    |
| Set rest per rest class, and the fallback | §4.7    |
| Rest after an exercise's last set         | on      |
| Count warm-ups in volume                  | off     |
| Scroll to the next exercise in a superset | on      |
| Session layout (Rows, Table)              | Rows    |
| Keep the screen awake during a session    | on      |
| Body mass, typed                          | none    |

A session snapshots the rest and counting settings at Start (`SessionSettings`), so a fold never depends on settings
changed later. The layout is not snapshotted: switching it presents the session again, with the RPE pre-fill on for Rows
and off for Table (`WorkoutsSettings.sessionPresenter`, §4.14).

## 7. Screens

Wireframes are schematic: widths, glyphs and spacing are the system's. Text in quotes is the copy to use.

### 7.1 Root and tabs

`TabView` with four `Tab`s: **Routines** (`dumbbell`), **History** (`clock.arrow.circlepath`), **Exercises**
(`list.bullet`), **Settings** (`gearshape`); `.tabBarMinimizeBehavior(.onScrollDown)`. While a session is active,
`.tabViewBottomAccessory(isEnabled: true)` shows the in-progress bar (§7.5), and `.fullScreenCover` presents the
session (§7.6) with `.navigationTransition(.zoom(sourceID: sessionID, in: namespace))` from the accessory's
`.matchedTransitionSource`. `RootView` replaces the scaffold's `AppTab` with the four cases. `.updateOffer(updates)`
stays on the root, but an offer that arrives while a session is open is held until Finish or Discard: Install replaces
the running app, and a journal from an older build is the one thing a newer build may not read (§2.3).

### 7.2 Routines

A `NavigationStack` titled "Routines". Toolbar: a trailing "+" button, "Start empty session". Content: a `List` with one
collapsible `Section(isExpanded:)` per folder, folders sorted by name, routines by `position` then name; routines with
no folder last, under "Other routines". The section header is the folder name and its count, as a separate secondary
label.

```text
+--------------------------------------------------+
| Routines                                     (+) |
|                                                  |
| v Full Body 3x Week (Back-Friendly)   4 routines |
| +----------------------------------------------+ |
| | Fully Body Collection #1                     | |
| | Leg Press (Machine), Calf Press (Machine),   | |
| | Seated Leg Curl (Machine), Leg Extension...  | |
| | Last done Tue 27 Oct            [  Start  ]  | |
| +----------------------------------------------+ |
| +----------------------------------------------+ |
| | Day 1 – Strength Foundation                  | |
| | Leg Press (Machine), Iso-Lateral Chest Press | |
| | (Machine), Seated Row (Machine), Lateral...  | |
| | Last done Mon 26 Oct            [  Start  ]  | |
| +----------------------------------------------+ |
|  ...                                             |
+--------------------------------------------------+
| [ ● Day 1 – Strength Foundation      Rest 1:16 ] |  <- accessory, when a session is active
|   Routines    History    Exercises    Settings   |  <- tab bar
+--------------------------------------------------+
```

- **Card** (content): the name (headline), the exercise names joined by commas, at most two lines, "Last done Tue 27 Oct"
  from local history (omitted when never), and a `.borderedProminent` "Start". Tapping the card pushes the preview.
- **States**: loading (a `ProgressView` until the catalogue decodes, normally instant); empty ("No routines yet. Routines
  are written in the substrate console; this build bundles the owner's folder." with a "Start empty session" button);
  catalogue errors (a footer row "2 routines could not be read" linking to Settings).

### 7.3 Routine preview

Pushed from a card. A `List` in the content layer; a bottom toolbar with one `.glassProminent` "Start routine". The page
hides the tab bar (`.toolbar(.hidden, for: .tabBar)`): the floating tab bar would otherwise lie over Start and take its
taps. Back stays in the navigation bar.

```text
+--------------------------------------------------+
| < Routines      Fully Body Collection #1         |
|                                                  |
| Every exercise of the folder in one routine:     |
| open it and do what the day allows...            |
| Last done Tue 27 Oct, 46 min                     |
|                                                  |
| Leg Press (Machine)                              |
|   Warm-up: 40 kg × 12                            |
|   2 sets of 10–15 reps at RPE 8, from 90 kg      |
|   Rest 2:00                                      |
| ...                                              |
| Chest Press (Machine)                            |
|   Warm-up: 25 kg × 15                            |
|   2 sets of 10–15 reps at RPE 9.5, from 35 kg    |
|   Set 2 is a drop set: 1 drop                    |
|   Rest 1:30                                      |
| | Superset A, rest 1:00 after each round         |
| | A1 Glute Bridge   2 sets of 12–20 reps         |
| | A2 Bird Dog       2 sets of 8–10 reps          |
| ...                                              |
|              [   Start routine   ]               |  <- bottom toolbar (glass)
+--------------------------------------------------+
```

Each slot: the exercise name, then one line per kind of planned set (warm-ups listed, working sets summarised as "n sets
of a–b reps at RPE r, from load"), special structures named ("Set 2 is myo-reps", "Set 3 to failure", "Hold 0:45–1:30,
counted down"), the rests that differ from the defaults, tempo, and the slot's notes. Groups show a bracket and A1, A2
labels. Start runs §7.4's check.

### 7.4 Starting while a session is open

A `confirmationDialog` anchored to the Start button that was tapped (req 22):

```text
+--------------------------------------------------+
|        A training session is in progress         |
|  Fully Body Collection #1, started 13:42,        |
|  11 sets done.                                   |
|                                                  |
|  Resume it                                       |
|  Finish it and start this one                    |
|  Discard it and start this one        (red)      |
|  Cancel                                          |
+--------------------------------------------------+
```

"Resume it" opens the cover. "Finish it and start this one" opens the open session's Finish sheet and starts the new
one after Save. "Discard it and start this one" asks once more ("Discard 11 sets?", or "Discard the open session?" when
no set is done) and then starts.

### 7.5 In-progress accessory

`tabViewBottomAccessory(isEnabled:)` content: a plain `Button` filling the bar, with no glass of its own (the bar is
the system's glass). It reads `tabViewBottomAccessoryPlacement`:

```text
expanded:  [ ● Fully Body Collection #1                  24:16 ]
           [   Chest Press (Machine), set 2       Rest 1:16    ]
inline:    [ ● Rest 1:16 ]        or        [ ● 24:16 ]
```

- The dot is green while active, grey while the clock is paused ("Paused" replaces the clock).
- The clock is `Text(timerInterval:)` from `clockStart` (§9.1), the rest `Text(timerInterval:countsDown: true)`; both
  tick with no app code.
- No discard button (06 §5 item 8). Tapping opens the session cover with the zoom transition.

### 7.6 The session cover

The cover draws the session in one of two layouts, chosen in Settings > Session > Layout and from the cover's `…`
menu: **Rows**, the default, one line per set with the set to do now open in place (§7.6a), and **Table**, the grid
this section describes. The toolbar, the sheets, the menus, the dialogs and the rules for ticking, the entry check and
scrolling below are shared; the header, the cards, the grid and the rest bar are the Table's.

A `NavigationStack` inside the `fullScreenCover`, a `ScrollView` with a `LazyVStack` of cards, and the rest bar in a
`safeAreaBar(edge: .bottom)`.

```text
+--------------------------------------------------+
| (v)  Fully Body Collection #1  (≡) (…) [Finish]  |  <- glass toolbar
|--------------------------------------------------|
| Duration         Volume           Working sets   |
| 14:02            375 kg           3 of 40        |
|                                                  |
| Chest Press (Machine)                       (…)  |
| [ Rest 1:30 ] [ Note ]                           |
| Seat on 4, handles level with the chest.         |
| SET  PREVIOUS            KG     REPS   RPE   ✓   |
| W    25 kg × 15          25     15     6    [✓]  |  <- done: tinted, bold
| 1    35 kg × 15          40     11          [ ]  |  <- Up next, ghost values grey
|      Last time 35 kg × 15, top of range; try 40 kg × 11
| 2    35 kg × 13 @ 9.5    35     14          [ ]  |
| 2a   35 kg × 8 @ 9.5     25     to fail.    [ ]  |  <- drop: 20 % off set 2's load; locked until 2 is ticked
| [               + Add set                ]       |
|                                                  |
| | Superset A: 2 rounds, 1:00 after each round (…)|
| | A1 Glute Bridge                           (…)  |
| | SET  PREVIOUS   REPS   RPE   ✓                 |
| | 1    × 15       15           [ ]               |
| | A2 Bird Dog                               (…)  |
| | ...                                            |
|                                                  |
| [            + Add exercise            ]         |
|--------------------------------------------------|
|  (−15)   ( Rest  1:16  ▓▓▓▓▓▓░░░ )  (+15) (Skip) |  <- rest bar (glass)
|  Next: Chest Press (Machine), set 1: 40 kg × 11  |
+--------------------------------------------------+
```

**Toolbar** (system glass): leading "Minimise" (`chevron.down`), which dismisses the cover and keeps the session; the
title inline; trailing a jump menu (`list.bullet`, the exercises with "2 of 3 done", tapping scrolls to one), a `…` menu
(Add exercise, Reorder exercises, Timer…, Pause clock or Resume clock, Session note, "Show as rows" or "Show as table",
Discard session), a
`ToolbarSpacer(.fixed)`, then "Finish" in `.glassProminent` at `ToolbarItemPlacement.topBarPinnedTrailing` with
`visibilityPriority(.high)`. A quiet banner row under the header says "Alerts are off for the app. Rest ends will not
notify." with "Settings" when notifications are refused or shown nowhere (Settings also names a missing time-sensitive
level, §9.6). When the journal could not be written, another says "Changes are not being saved on this iPhone" (§2.3).

**Header** (content): three labelled columns, Duration (tap pauses or resumes the clock), Volume (§4.11) and Working
sets ("3 of 40": working sets done of planned; the label says "Working sets" because ticked warm-ups do not move it,
`SessionHeaderStats.setsTitle`). No muscle map in V1.

**Exercise card** (content): the name (tap pushes the exercise detail, §7.9), a `…` menu, a group label (`A1`) and the
group's colour bracket when grouped, a "Rest 1:30" chip (`.bordered`, opens the rest menu) and a "Note" chip, the slot's
notes in secondary text, the set grid, and a full-width `.bordered` "+ Add set".

**Set grid columns** follow the exercise's `measurement` (req 28; 06 §1.3). Every grid starts with SET and PREVIOUS and
ends with the tick:

| Measurement              | Middle columns                    | RPE column |
| ------------------------ | --------------------------------- | ---------- |
| `weightreps`             | KG (or LB), REPS                  | yes        |
| `repsonly`               | REPS                              | yes        |
| `bodyweightreps`         | REPS                              | yes        |
| `weightedbodyweightreps` | +KG, REPS                         | yes        |
| `bodyweightassistedreps` | −KG (assistance), REPS            | yes        |
| `duration`               | TIME (play button and countdown)  | no         |
| `weightduration`         | KG, TIME                          | no         |
| `distanceduration`       | M, TIME                           | no         |
| `shortdistanceweight`    | KG, M (a stopwatch below the row) | no         |

The unit column header is the exercise's unit. A grid is bounded tabular data, so fixed columns are right here; the
exercise name, notes and reason line are never put in columns.

Every set takes an RPE, timed and distance sets too (the owner's call); the engine stores it on any row and its record.
A grid with no RPE column keeps its room for the timer: the set-kind menu offers "Rate effort", which opens the same RPE
sheet (`editSet`, before or after the tick; its reps-in-reserve line is left out, `RPEScaleText.reserve(_:measurement:)`),
and a rated row shows a small secondary "RPE 8" under it (`SetRowPresentation.canRateEffort`, `effortText`).

**Rows.** States: pending (ghost values grey, fields bordered), Up next (a leading accent bar), locked (a segment whose
earlier link is pending: fields disabled), done (row tinted with a light green background, values black and bold, the
tick filled green), timing (see Timed sets below). A typed value is black, regular weight; a done one black and bold; a
ghost value (§5.5) is regular weight in a grey that reads at arm's length (`GhostValueStyle`: about 5:1 on the light
card, 6:1 on the dark one, darker or lighter again with Increase Contrast), and a hint (`≈15`, "to fail.") is the same
grey in italics. The ghost is drawn over the empty field, not as the text field's prompt: the system draws prompts in
the placeholder grey, under 2:1 and nearly invisible in dark mode. Numbers are typed with the system decimal pad and a keyboard toolbar,
all on the right in the thumb's reach: "−5 kg", "+5 kg" (the exercise's step; ±1 for reps; ±5 s for time), "Next",
"Done". Every tappable cell, the timer button and the `…` menus are at least 44 pt. A drop's or open mini-set's REPS
ghost "to failure" shows as "to fail." in the cell (VoiceOver reads the whole words). Each value field's VoiceOver
label names the exercise, the set and the field with its unit ("Leg Press (Machine), set 1, load in kilograms"; "reps";
"time in minutes and seconds"), and its value is what is typed or done, else "suggested 40", "≈15, a hint", "required,
empty" or "empty", and a value cleared while typing reads as the empty field shows, never the value it replaced
(`GridCell.accessibilityLabel`, `accessibilityValue(draft:)`); the RPE control reads "Leg Press
(Machine), set 1, RPE". The grid's text stops growing at
AX1 and its columns at 16 % wider, so a member card on a 393 pt phone still holds them; at AX1 nothing in a cell is cut
(values shrink to half size at most, a running timer's "left" moves under its time, Top and Back-off shrink). Swipe left on a row: Remove
(`swipeActions` in the `LazyVStack` with `swipeActionsContainer()`, iOS 27); a completed row asks first. Tapping the
filled tick unticks (`uncompleteSet`). A tick sends `completeSet` (after the entry check of §5.7), plays the tick
haptic, and, with superset auto-scroll on and a 0 s transition, scrolls the next member's row into view. No other action
scrolls the list. Focusing a field or opening a row's RPE or set-kind control sends `touchRow`.

**PREVIOUS cell**: §5.4; tap copies.

**Set-kind menu**: tapping the SET label opens a `Menu`: "Warm-up", "Working", "Failure", "Drop set", "Myo-reps",
"Myo-rep match", "About set types…", a divider, "Rate effort…" (rows with no RPE column, `canRateEffort`), "Rest after
this set…", "Note…", "Remove set" (destructive). Items that §4.3 forbids for the row are disabled. "About set types…"
opens a `.medium` sheet, "Set types", with each kind's name over a few plain sentences: what it is, what the app adds
for it, the rest around it and when it stops. Warm-up: lighter, not a working set, left out of volume unless Settings
counts it, warm-up rest after it. Working: counted, the exercise's rest. Failure: to the last clean rep, a red F.
Drop set: a drop row added, 20 % lighter unless the routine says otherwise, to failure, no rest before it, the rest after
the last drop. Myo-reps: one set near failure, then mini-sets with its load 0:15 apart, each tick adding the next,
stopping when one gets fewer reps than the first mini-set or after 5. Myo-rep match: mini-sets 0:15 apart until this set and its
mini-sets reach the previous working set's reps, or after 5 (§4.7, §4.9).

**RPE sheet**: tapping the RPE cell (or "Rate effort…") presents `.sheet` with `.presentationDetents([.height(320)])`
and no custom background:

```text
+--------------------------------------------------+
|  Rest                                            |
|  1:16          (−15) (+15) (Skip)           (?)  |  <- only while a rest runs
|                       8.5                        |
|   (−)                Hard                  (+)   |  <- −0.5 and +0.5, 44 pt
|            Could do 1 more rep, maybe 2          |
|  6  6.5  7  7.5  8  [8.5]  9  9.5  10            |  <- slider, half steps
|  [ Clear ]                            [ Done ]   |
+--------------------------------------------------+
```

Values and text (the RIR-based scale with Hevy's wording; 02 §5.2):

| RPE | Headline        | Reps in reserve text          |
| --- | --------------- | ----------------------------- |
| 10  | Maximum effort  | Could not do more reps        |
| 9.5 | Very hard       | Maybe 1 more rep              |
| 9   | Very hard       | Could do 1 more rep           |
| 8.5 | Hard            | Could do 1 more rep, maybe 2  |
| 8   | Hard            | Could do 2 more reps          |
| 7.5 | Challenging     | Could do 2 more reps, maybe 3 |
| 7   | Challenging     | Could do 3 more reps          |
| 6.5 | Moderate effort | Could do 3 more reps, maybe 4 |
| 6   | Moderate effort | Could do 4 or more reps       |

The empty state reads "Select RPE" and "Slide or tap to rate your effort"; the slider is dimmed and Done disabled until
a value is picked, so Done never saves less than the sheet shows. −0.5 and +0.5 (44 pt circles beside the value) move it
by half a point within 6 to 10; with no value yet they move from the slider's resting 8. A timed or distance set shows
no reps-in-reserve line (`RPEScaleText.reserve(_:measurement:)` is nil). Past the default text size the sheet opens at
`.medium`. The `?` opens a popover explaining the scale. While a rest runs, the top line holds its label and countdown
and a compact −15, +15 and Skip (plain fills, 44 pt tall). At the default text sizes they always sit beside the
countdown and a long label (a superset's "Go: Leg Press (Machine) (A2) in") takes two lines, so the 320 pt sheet never
grows a row; past the default size they go under the countdown when they do not fit beside it. They are there because
the rest bar steps aside while any sheet is up: its tinted glass would show through the sheet's. Done sends `editSet`.

**Timed sets**: the TIME cell holds a play button and the ghost duration.

```text
pending:   1    1:02       [▶]  1:07          [ ]
running:   1    1:02       [■]  0:42 left         <- countdown to the target; the rest bar shows "Plank, set 1  0:42  [Stop]"
past zero: 1    1:02       [■]  +0:05 (orange)    <- keeps counting
stopwatch: 1    0:41       [■]  0:12              <- timerMode stopwatch counts up
done:      1    1:02            1:09          [✓]
```

Play sends `startTimedSet` with the target `row.timer` holds, `.ready(target:)`: the shown duration, typed or else the
ghost (the suggestion's, the plan's `durationMin`, PREVIOUS), or none for a stopwatch (`timerMode` stopwatch, or nothing
to show). Stop or the tick sends `stopTimedSet`; a long press on the stop button offers "Cancel timer"
(`cancelTimedSet`). At the target: a haptic in the foreground, a notification in the background (§9.2), nothing else.
The running cell counts down with `Text(timerInterval:)` ("1:07 left", "left" small under the time when the cell is too
narrow for one line) and turns orange only once `now` passes `endsAt`, then counts on from it ("+0:05"). The rest bar's
timed-set pill does the same and turns from the accent colour to orange at the same second. Both decide the branch in
a `TimelineView(.periodic(from: startedAt, by: 1))`, which renders at the current second and flips at `endsAt`. A
`TimelineView(.explicit([endsAt]))` is not a way to tell: SwiftUI renders an explicit schedule's first entry at once
even when it is in the future, so such a view starts in its overtime branch ("+0:00", never counting down); that was
the frozen orange "+0:00" of the first simulator run.

**Rest bar**: one custom glass element (§8), in a `safeAreaBar(edge: .bottom)` with `scrollEdgeEffectStyle(.soft, for:
.bottom)` on the scroll view. Its forms, morphing with `glassEffectID`:

| State                     | Content                                                                                                                                                                                                          |
| ------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| No countdown              | A compact Up next chip: "Next: Seated Row (Machine), set 1: 50 kg × 9"; tap scrolls to it                                                                                                                        |
| Resting                   | −15, the countdown pill with its `ProgressView(timerInterval:)` and slot name ("Rest", "Warm-up rest", "Round rest", "Rest before next exercise", "Switch sides", "Pause"), +15, Skip; the Up next line under it |
| Short pause or transition | The same, labelled "Pause 0:12" or "Go: Bird Dog (A2) in 0:12"                                                                                                                                                   |
| Rest over (10 s)          | "Rest over. Go: set 2, 35 kg × 14", tinted green, then the Up next chip; when the next set is another exercise's, it names it: "Rest over. Go: Seated Row (Machine), set 1, 50 kg × 9"                           |
| No timer (0 s rest)       | "Go: Bird Dog (A2), round 1"                                                                                                                                                                                     |
| Timed set                 | "Plank, set 1", the countdown or stopwatch, Stop                                                                                                                                                                 |
| All done                  | "All sets done" and a "Finish" button                                                                                                                                                                            |
| Empty session             | "Add an exercise to begin"; tap opens the exercise picker (an empty session is not "done")                                                                                                                       |

The pill's label sits above its countdown (beside it there is no room for "Rest before next exercise"), and everything
in a tinted pill is white. The glass container's spacing is under the row's, so −15, the pill, +15 and Skip stay four
separate shapes. At the accessibility text sizes the bar's text keeps growing and its controls move to a second row:
the pill full width, then −15, +15 and Skip sharing the row below (Stop and Finish likewise under their pill). The
"Next" line under a running rest is a button on glass of its own, part of the bar's container, at least 44 pt tall,
that brings the next set into view; it reads "Next: Leg Press (Machine), warm-up 2" with the values, "70 kg × 6", in
bold, on the same line when they fit and else on a line of their own so the values are never the part cut off. The Up
next chip draws its line the same way. While a sheet is up the bar fades out and takes no taps (the RPE sheet carries
the rest's controls); menus have no presentation callback in SwiftUI, so the bar stays under them. On a page pushed
inside the cover (an exercise's history) a running rest or timed set keeps the same bar.

**Supersets**: a group block has a header row (content): "Superset A: 2 rounds, 1:00 after each round" (the kind's
label, rounds, round rest), with a `…` menu: "Rest after each round…", "Kind" (Superset, Tri-set, Giant set, Circuit,
Paired sets), "Pairing" (§3.3 values), "Ungroup all". Member cards carry `A1`, `A2` (group key upper-cased, member
position) and share a coloured bracket; each group gets the next colour of a fixed palette of four accessible colours:
indigo, teal, orange and pink in shades that read at 4.5:1 or better on the card in light and dark (the system teal,
orange and pink do not on white). The warm-up `W` uses the same orange.
The member card's Rest chip reads "Then 0:00 to A2" (its transition) instead of the set rest; the last member's reads
"Round rest 1:00".

**Exercise menu** (`…` on a card, a system `Menu`): "Reorder exercises", "Replace exercise", "Add to superset…" (a
submenu of the other exercises) or "Remove from superset", "Rest…" (set rest, warm-up rest, transition, side switch, as
apply), "Add warm-up set", "Note…", "Remove exercise" (destructive; asks "Remove Chest Press (Machine) and its 3 done
sets?" when sets are done).

**Rest picker**: the Rest chip opens a `Menu` of presets (Off, 0:30, 1:00, 1:30, 2:00, 2:30, 3:00, "Default (2:00)",
"Custom…"); Custom opens a `.medium` sheet with a wheel in 5 s steps up to 10:00 and Done.

**Add exercise**: "+ Add exercise" at the end of the list and in the `…` menu open the picker sheet (`.large`): a search
field whose tokens match in any order against names and aliases, case and diacritics ignored, with built-in synonyms (db
dumbbell, bb barbell, kb kettlebell, rdl Romanian deadlift, ohp overhead press); "Recent" (done in the last 30 days),
then A to Z. Rows show the name and the equipment label. "New exercise…" at the bottom opens a form: name, measurement
(the nine labels from the kind), equipment, laterality (default both sides), rest class. Its id is the name's slug
(lowercase, every run of non-alphanumerics a `-`, trimmed; `-2`, `-3` if taken); it is marked "On this iPhone".

**Reorder**: a `.large` sheet with a `List` in edit mode and `onMove`, names only; group members show indented under a
"Superset A" row and move as a block unless dragged within it. Done sends `reorderExercises`.

**Discard**: `…` > "Discard session" asks "Discard this session? 11 done sets will be deleted." with "Discard"
(destructive) and "Cancel". It is the only way to discard (req 22).

### 7.6a The Rows layout (the default)

The owner's chosen session screen. Each set is one line that reads like a sentence; the set to do now is open in place
as a small editor; a floating glass capsule at the bottom says what is happening. It is the default; the grid of §7.6
stays as **Table**. Settings > Session > Layout (Rows, Table) picks one, and the session's `…` menu has "Show as table"
or "Show as rows" (§6.4, §7.10). Both read the same `SessionPresentation` and send the same `SessionCommand`s; the
toolbar, the sheets, the menus, the dialogs, Finish, the countdown coordinator, the notifications and the Live Activity
are the same for both. Everything Rows shows comes from `SessionPresentation.rowsLayout` (WorkoutsKit/API.md, "The Rows
layout"); the views hold only view state (`RowsScreenModel`: a fix and its draft, the keypad, what is folded or opened,
the undo offer) and compute no rule. The views are in `the app/Views/Session/Rows/`.

```text
+--------------------------------------------------+
| (v)  Day 1 – Strength Foundation    (≡ …) [Finish]|  <- glass toolbar; the clock under the title
|      ⏱ 12:40                                      |
| Leg Press (Machine)                         (…)  |
| 6–10 reps at RPE 8, rest 2:30                    |
| (✓) Warm-ups 40 kg × 12, 70 kg × 6  as last time |  <- folded once every warm-up is done
| (✓) Set 1  110 kg × 8  @ 8    +5 kg, −2 reps (=) |  <- done: the change and the verdict mark
| +----------------------------------------------+ |
| | (●) Set 2  ✦ reason for today's numbers  (…) | |  <- the open set, tinted
| |     110 kg   last 105            ( −5 | +5 ) | |
| |     8 reps   last 10             ( −1 | +1 ) | |
| | [6  6.5  7  7.5 [8] 8.5  9  9.5  10]         | |  <- RPE strip; the plan's 8 outlined
| | [         ✓ Log 110 kg × 8 @ 8          ]    | |
| +----------------------------------------------+ |
| ( ) Set 3  110 kg × 8              last 105 × 10 |  <- upcoming: muted, last time
|  +  Add set                                      |
|--------------------------------------------------|
| (✓) Set 2 is up                        2:41      |  <- the capsule (glass)
|     Rest 2:30 done                 since set 1   |
+--------------------------------------------------+
```

**Structure.** A `ScrollView` with a `LazyVStack` of blocks (16 pt side margins, 12 pt between blocks), the "Alerts
are off" and "not saving" banners at the top as in §7.6, and "+ Add exercise" at the end. The capsule sits in a
`safeAreaBar(edge: .bottom)` with `scrollEdgeEffectStyle(.soft, for: .bottom)`. Rows has no header row: the toolbar's
principal item shows the session's title over its clock (`stopwatch` and the elapsed time in the accent colour, or
"Paused" in orange); a tap pauses or resumes it.

**Blocks.** One card per `RowsBlock` (`secondarySystemGroupedBackground`, corner radius 24, opaque, no shadow):

- The name (20 pt semibold; for a plain exercise a tap opens its history), the plan in one line under it (15 pt
  secondary, "6–10 reps at RPE 8, rest 2:30"), and the `…` menu: History, then the Table's exercise menu items; for a
  group, the group's menu items and a submenu per member. The routine's and the gym's notes show under the plan only
  on the block holding the open set.
- The block's items in the order they are done, then its "+ Add set" rows (one per member in a group, "Add A1 set").
- A finished block folds to one line, at least 56 pt: a green check, the name, `summary.text` ("110 kg × 8, 7, 8";
  up to two lines, all of it at the accessibility sizes) and the headline change ("+5 kg", green for a gain) with a
  chevron. A tap opens it again; a chevron in the opened header folds
  it. VoiceOver reads `summary.sentence`.

**A set line** (`RowsSetItem`), at least 46 pt tall, 0.5 pt hairlines between neighbouring lines from x = 48:

- A 22 pt ring: done is a filled green check (orange for a warm-up); the open set an accent ring with a dot (a hollow
  inner ring while a rest runs before it, and on the open set drawn as a line while another set is fixed); upcoming a
  grey ring (orange for a warm-up); `later` a grey disc with a pause mark; `locked` a dashed ring, the line dimmed; a
  drop not yet done a hook.
- The title (15 pt secondary, at least 44 pt wide so the values line up; orange semibold for a warm-up or a drop; in a
  group the `A1` badge in the group's colour and the exercise's name at 17 pt), the values (17 pt, monospaced digits:
  primary when done, secondary when pending, the accent when `valuesChanged`), the RPE ("@ 8", 15 pt secondary) and the
  tags ("+ drop" orange, "later" grey, "up next" accent).
- At the end: on a done line the changes, joined by commas, gains green and everything else grey, then the verdict mark
  (a 20 pt disc: `arrow.up.right` green for better, `equal` or `arrow.down.right` grey); on a pending line `last`.
- When it does not fit on one line, the end moves to a second line; at the largest sizes the title, the values and the
  end each take a line. Nothing is cut to fit.
- A tap: a done line opens its fix in place; a later or upcoming line is done now (`touchRow`, out of order); the open
  set drawn as a line goes back to it. VoiceOver reads `sentence` as one element, with a hint for the tap.
- The set just logged is washed green from the left for five seconds.

**Other lines.** The warm-up fold ("Warm-ups 40 kg × 12, 70 kg × 6", "as last time"; a tap unfolds the warm-ups as set
lines, each fixable). A group's "Round 1 of 2" (13 pt semibold, dimmed when done). Connectors, 28 pt: an arrow for the
hand-off ("**0:00,** straight to A2"), a timer in the accent colour for a rest ("**Rest 1:00,** then round 2"), on a
rail that joins the rings, dimmed once passed. A per-set rest on the set before ("Rest after this set…", or a
planned set's `rest`) wins, as it does for the countdown (§4.7 line 1), and reads as a rest ("**Rest 0:45,** then
A2"). The text takes up to two lines, all of it at the accessibility sizes. A chain's header ("Set 2 as myo-reps, 0:15 between mini-sets") and its
done mini-sets as chips on one line ("Mini-sets 4 4", "first time"); a tap on a chip fixes that mini-set; the stop text
in orange once the chain has ended.

**The editor** (`RowsEditor`), open in place on the set to do now: a panel inset 4 pt in the card, corner radius 20,
filled with the accent at 8 % and a 1 pt accent line (orange for a warm-up or a drop; the card's colour with a grey
line when fixing). In order:

- The header, at least 36 pt: the ring, the badge and title (17 pt semibold), the note (a sparkle and the suggestion's
  reason in 13 pt; a status such as "after the rest" or "logged at 12:06" in 15 pt; a red dot and "holding"), and on
  the right "+ RPE" while the strip is folded, `trailingNote` ("no rest", orange), and the set menu `…` (the Table's set
  menu items) or Cancel when fixing. The note sits beside the title when it fits, else on its own line under it. After
  an RPE tap the cell's meaning ("RPE 8.5: could do 1 more rep, maybe 2") shows in the note's place for 2.5 s without
  changing the header's height.
- Each value on a 48 pt row, indented to the title: the number at 26 pt semibold with monospaced digits and a faint
  dotted underline (a tap opens the keypad; the target is 44 pt tall), its unit (15 pt secondary), `last` (15 pt secondary) at the same x on
  every row (the widest number and unit sets it, at least 84 pt), the reset chip ("↺ 8", or "↺ was 8" when fixing), and
  the stepper on the right: one tinted capsule, 44 pt tall, two 56 pt halves ("−5 | +5", "−1 | +1", "−0:05 | +0:05")
  that repeat while held. A changed number is drawn in the accent colour. A value with nothing to log shows its
  placeholder ("to failure", "≈15") in grey italics, outlined in orange when it is required. When the row is narrow the
  chip goes under `last`; at larger sizes `last` goes under the number; at the accessibility sizes the stepper goes
  under them.
- The RPE strip: nine cells from 6 to 10 in half steps across the panel's full inner width, 48 pt tall, on a
  `tertiarySystemFill` track. Each cell is a ninth of the strip: 44 pt wide on a 440 pt iPhone, 40 pt on a 402 pt one,
  39 pt on a 390 pt one. Where nine cells of 38 pt do not fit (a 375 pt iPhone) the strip takes two rows, as at the
  accessibility sizes. Whole numbers are 17 pt semibold, half steps 15 pt regular. The plan's value (and a
  pre-filled or copied value) is an outlined, hollow cell; a rating is filled solid with its fill inset in the cell. A
  tap on any cell rates it (the outlined one too, as `entered`); a tap on the filled cell clears it. Folded behind
  "+ RPE" when there is nothing to show (a warm-up, a timed set, or a set with no planned RPE and none last time).
- A timed set: ready, the duration row with its stepper and, under it, the track with the goal's tick labelled above and
  last time's below. Running, the duration row gives way to the clock (28 pt, counting up, its width reserved so it
  never moves anything) with "0:21 to beat last time" beside it, and the track filling.
- The buttons. Log is a 50 pt capsule in the panel's colour that names exactly what it saves ("Log 110 kg × 8 @ 8",
  "Log A2, 11 per side", "Log drop 25 kg × 12"); disabled while a required value is missing. A timed set has "Log 1:07
  hold" beside Start, then Cancel beside "Stop and log". A pending mini-set has "End myo-reps here" under Log. Fixing
  has "Delete set" in red (it asks first, as the Table's removal of a done set does) and Save, enabled once the draft
  differs.

**Logging.** One tap on Log sends `tick` with the row's `commitValues`, which carry the plan's RPE as `prefilled`
when nothing was tapped (§4.14), through the entry check as in the Table. Once the set is in the journal the phone
plays `.success` (§9.3), the line is washed green, and "Set 2 logged, Undo" floats above the capsule for five seconds
(Undo sends `untick`); when the entry check asks first, that comes once its Keep or Use corrected logs the set, and
not after Edit. The next set opens where it is, and the list makes the least scroll that shows it whole; a scroll
the owner is making is never interrupted: the list waits until the scroll settles. A superset's 0 s hand-off scrolls
the same way: superset auto-scroll is the Table's, and Rows follows its open set instead. A request to show the open
set (the entry check's Edit, Finish's way back) takes the same least scroll.

**Fixing a done set.** A tap opens the fix in place: the draft starts at what was logged (`RowsFix.logged`), every
stepper, chip, keypad entry and RPE cell changes the draft (`SetValues.applying`), and Save sends `RowsFix.save(draft)`,
one edit with only what changed. Cancel drops the draft. While a fix is open the set to do now is drawn as a line with
its "up next" tag, and the rest keeps running.

**Out of order.** A tap on any later set opens it at once. Sets passed over show a grey "later" tag and a pause mark,
and the capsule says which set waits, with Go back.

**Supersets** read top to bottom in the order they are done: Round 1 of 2, A1, "0:00, straight to A2", A2, "Rest 1:00,
then round 2", Round 2, then the rest before the next exercise. **Drops** show as "+ drop" on their set until it is
done, then hang off it with no rest and open at once (orange). **Myo-reps**: the activation, then one mini-set at a time,
with the done ones as chips.

**The keypad.** A tap on a number opens a keypad that floats where the capsule was, on glass (corner radius 34): the
nearby steps of the exercise's grid as six chips, 44 pt tall (a tap sets the value and closes the pad), digits, "." for loads and
distances, delete, Cancel, and "Set 120 kg". The field shows what is typed in a box with a caret; a time is typed as
digits ("107" is 1:07). On a fix it changes the draft.

**The capsule** (`rowsLayout.capsule`) replaces the Table's rest bar. A rounded glass shape, at least 60 pt tall, 16 pt
from the screen's edges:

- Resting: a ring of what is left, the countdown (28 pt) and "of 2:30", then −15, +15 and Skip, and under a hairline
  "After set 3" (accent) with what follows. When the row is narrow "of 2:30" goes first, then the buttons move under the
  time. VoiceOver reads the rest's title as the clock's label and the time left with the total as its value.
- Up next and Go: a 36 pt disc (a green check after a rest, an accent arrow, orange with a flame for a warm-up or an
  arrow for a drop), the title ("Set 2 is up", "A2 Bird Dog now, no rest", "Drop to 25 kg now") and detail, and a clock
  counting up since the last set with its label ("since set 1"). Before the first set it is the session's clock
  ("session"), which stands still while the session clock is paused (`RowsNowCapsule.pausedAt`), as the title's does.
  A tap brings the open set into view.
- Waiting: a grey pause disc, which set waits, and Go back.
- A timed set: what comes after it ("Then rest 0:15", "Set 2, 0:58 hold, last 0:53"); the clock is in the open set.
- Every set done: Finish. No exercise: a tap opens the picker.
- It fades out while a sheet is up, as the rest bar does. On a page pushed inside the cover it stays while a rest or a
  timed set counts; a tap goes back.

**Sizes and accessibility.** System text styles throughout (17, 15 and 13 pt at the default size; the numbers 26 pt,
the clocks 28 pt; nothing larger), with `@ScaledMetric` for the rings, steppers and cells, so everything grows with
Dynamic Type. Every time on screen ticks with `Text(timerInterval:)` or a once-a-second `TimelineView` in monospaced
digits inside a width reserved for its longest form, so nothing beside it moves. Each control is at least 44 pt tall,
and at least 44 pt wide save two: the RPE cells (a ninth of the strip, above) and the done mini-set chips (26 pt wide
or more, 44 pt tall). Glass
is used only on what floats (the toolbar, the capsule, the keypad and the undo offer); cards, editors, steppers and the
strip are opaque. Light and dark use the system colours, the accent, the group palette and an adaptive green for gains
(`RowsStyle`).

**Haptics.** A light tick (`.selection`) on every stepper step and RPE pick; `.success` when a Log is journaled (§9.3).

**Where it differs from the design mockup** (the owner's pick, "rows", revision 2):

- A drop does not pre-fill RPE 10 and shows "to failure" rather than an estimate: the engine pre-fills no drop or
  mini-set (§4.14). Log stays disabled until reps are set; the stepper starts from last time.
- A timed set offers Cancel, not Pause: the engine has no paused timed set.
- The verdict mark follows the presenter's estimated one-rep max, so heavier for fewer reps can read as below.
- Dragging along the RPE strip does not scrub: inside the scrolling list a drag there is a scroll. Each cell is a
  tap target instead.
- The keypad's Log label changes when the typed value is set, not while typing.
- Rows has no Duration, Volume and Working sets header; the clock is under the title and the totals are in Finish.

### 7.7 Finish

Tapping Finish:

1. If rows are unticked, one dialog: "3 sets are not ticked" with "Drop them", "Mark them done as shown" and "Cancel".
   "Mark them done" uses each row's shown values; rows with nothing to commit are dropped, and when some are the
   dialog's message says so ("2 have a value missing and are left out either way.") and it adds "Fill in the missing
   values", which closes the dialog, scrolls to the first unticked set with a value missing and puts the keypad on its
   first empty field (a locked drop is only scrolled to). The dialog's words come from `FinishPresenter.summary` for
   `markDone`, worked out once when Finish is tapped. There is no second "are you sure" (06 §2.8 item 10).
2. The Finish sheet (`.large`, content in a `Form`):

```text
+--------------------------------------------------+
| Cancel           Finish session          [Save]  |
|--------------------------------------------------|
| Fully Body Collection #1                         |
| Started      Fri 30 Oct  13:42                   |
| Ended        Fri 30 Oct  14:29                   |
| Duration     47:12                               |
|                                                  |
| How hard was the session?                        |
| 1  2  3  4  5  6  7  8  9  10        [ Skip ]    |
| Hard                                             |
|                                                  |
| Working sets     24                              |
| Volume           3,120 kg                        |
| Exercises        9 of 21                         |
|                                                  |
| Notes                                            |
| [                                              ] |
|                                                  |
| Saved to this iPhone and to Apple Health.        |
+--------------------------------------------------+
```

- Started and Ended are `DatePicker`s, clamped as §4.3 says; Duration follows them (pauses excluded).
- With unticked sets, a section titled "3 sets are not ticked" holds the dialog's two choices as an inline picker, the
  chosen one ticked and still changeable, and the numbers below follow it. Its footer is the dialog's message; when
  some sets cannot be marked done, "Fill in the missing values" closes the sheet (dropping a start that waited on Save)
  and does what the dialog's button does.
- Session RPE is optional (lead decision): ten buttons for the CR10 scale with its word under the chosen one, the
  scale's anchors carrying over to the unlabelled values: 1 "very, very easy", 2 "easy", 3 "moderate", 4 "somewhat
  hard", 5 and 6 "hard", 7 to 9 "very hard", 10 "maximal". Skip clears it. 0 is not offered: Apple's effort score runs
  from 1.
- The summary rows are labelled values, one per line, and describe what Save will write under the dialog's choice, not
  only what is ticked: `FinishPresenter.projection(state, presentation:, policy:, end:, recordID:)` runs the engine's
  own `finish` on a copy of the state, for `drop` or `markDone`, and counts the result: sets saved, sets marked done,
  sets that cannot be completed because a value is missing, working sets (History's count), volume and exercises
  ("9 of 21"). A footnote under them says what happens to the unticked rows ("11 unticked sets are saved as shown; 2 are
  left out, a value is missing." or "3 unticked sets are left out."). The sheet works the summary out again when the
  dates, the choice or the session change, not on each keystroke in Notes, and keeps the saved one while it closes.
  The last line says where it will be saved, or "Apple Health access is off; saved to this iPhone." when denied.
- Save builds its `finish` from the same projection (`projection.event(...)`, with the record ids it mints), so the
  sheet's numbers and the saved record cannot disagree. Then §6.1's Finish steps; the cover closes, a short `.success`
  haptic plays, and the History tab badge shows the new session until visited. Finish never waits for Health.

### 7.8 History

**List**: a `NavigationStack` titled "History", a `List` of finished sessions newest first in month sections ("October
2026"). Each row: the session name's routine part as the title; under it, as separate secondary lines, the local date
and time ("Tue 27 Oct, 13:30") and a short labelled summary ("46 min, 24 sets, 3,120 kg"); a trailing glyph for Apple
Health: saved (`heart.fill`), pending (`clock`), not saved (`exclamationmark.triangle`). Empty state:
`ContentUnavailableView` "No sessions yet" / "Finished sessions show up here, newest first." Swipe to delete with a
confirmation ("Delete this session? It stays in Apple Health.").

**Session detail**: pushed from a row.

```text
+--------------------------------------------------+
| < History      Fully Body Collection #1          |
| Tue 27 Oct 2026, 13:30 to 14:16 (46 min)         |
| Session RPE 7                                    |
| Apple Health: saved                              |
|                                                  |
| Chest Press (Machine)                            |
|   W    25 kg × 15                                |
|   1    35 kg × 15                    rest 1:02   |
|   2    35 kg × 13 @ 9.5              rest 1:35   |
|   2a   35 kg × 8 @ 9.5               rest 0:06   |
| Superset A                                       |
|   A1 Glute Bridge   1  × 15    2  × 15           |
| ...                                              |
| Working sets per muscle                          |
|   Chest 2, Triceps 1, Shoulders 1 ...            |
| Notes                                            |
+--------------------------------------------------+
```

Health status "not saved" shows a "Try again" button; "pending" shows "Saving to Apple Health". Rows with notes show them
under the set. Each exercise's card is one `NavigationLink` to that exercise's detail (§7.9, the page the Exercises tab
opens), its chevron trailing. History's stack keeps a `NavigationPath`, not a `[SessionID]`: a typed path cannot take
the exercise, so the tap did nothing and left the card highlighted. Read-only in V1, apart from Delete in the toolbar.

### 7.9 Exercises

**List**: "Exercises", a searchable `List` (the picker's matching rules), sections A to Z, each row the name and, under
it, the equipment and the last-done date as secondary text. An exercise with no equipment never shows "None": a
bodyweight measurement says "Bodyweight", anything else shows no equipment line (`Exercise.equipmentText`). Local
exercises carry "On this iPhone".

**Exercise detail**:

```text
+--------------------------------------------------+
| < Exercises     Chest Press (Machine)            |
| Equipment                                        |
|   Machine                                        |
| Records                                          |
|   Weight and reps                                |
| Muscles                                          |
|   Chest; also Triceps, Shoulders                 |
| Load                                             |
|   5 kg steps                                     |
|                                                  |
| Estimated 1RM                       [3M 6M 1Y]   |
|   60 ┤          •    •                           |
|   55 ┤   •  •                                    |
|      └──────────────────────                     |
|                                                  |
| History                                          |
|   Tue 27 Oct, Fully Body Collection #1           |
|     W 25 kg × 15                                 |
|     1 35 kg × 15                                 |
|     2 35 kg × 13 @ 9.5, 2a 35 kg × 8 @ 9.5       |
|   ...                                            |
+--------------------------------------------------+
```

- Facts are stacked label over value, never side-by-side columns (the repository's UI rule). The load's grid reads
  "Load: 5 kg steps", with the start only when it is not 0 ("2.5 kg steps from 20 kg"); a bodyweight-plus exercise
  says "Added load", an assisted one "Assistance". Equipment follows the list's rule.
- **Trend**: Swift Charts `LineMark` plus `PointMark`, one point per session, joined by straight segments
  (`.interpolationMethod(.linear)`): a smoothed curve would draw values between sessions that no session had. Each
  point: the best e1RM of its working sets with `n = reps + 10 − RPE ≤ 12` (03 DI 12), by 03 §10.2 to §10.6 with that
  session's body mass, in the exercise's unit.
  Timed exercises chart the longest hold; reps-only and plain bodyweight without a body mass chart the most reps. Tapping
  a point pushes that session. Under two points: "Not enough sessions for a trend yet."
- **History**: every session with this exercise, newest first, its sets in display order with labels (chains folded per
  §4.10).

### 7.10 Settings

A `Form`, sections in this order:

```text
Units
  Default unit                         [ kg | lb ]
  Loads keep the unit they were entered in; this unit is for new loads and totals.
Rest
  After a warm-up                               0:45 >
  Between sets, by exercise class                    >   (pushes the seven classes and the fallback)
  Rest after an exercise's last set               [on]
Session
  Layout                               [ Rows | Table ]
  Keep the screen awake                           [on]
  Count warm-ups in volume                       [off]
  Scroll to the next exercise in a superset       [on]
Alerts
  Rest alerts: on, time sensitive                      (or "not time sensitive", "not asked yet", or a warning; §9.2)
Apple Health
  Saving workouts: allowed                [Connect]    (states: not asked, allowed, not allowed)
  Body mass: 82.4 kg from Apple Health                 (or a field to type one)
Data
  Routines and exercises come with this build: 4 routines, 30 exercises.   (or a warning when they did not load)
  Sessions are kept on this iPhone. Syncing with substrate comes in a later version.
  Exercises added on this iPhone: 2
  Records that could not be read: none
[BuildSection]                                          (Updater.swift)
```

Rest values use the rest picker of §7.6.

## 8. Liquid Glass

Glass is a functional layer for navigation and controls; content never gets it (HIG Materials; 04 §1.1).

**Glass, provided by the system** (no custom glass, no custom backgrounds):

- the tab bar and the tab bar bottom accessory;
- every navigation bar and toolbar, including the routine preview's bottom toolbar and the keyboard toolbar;
- sheets (RPE, rest wheel, exercise picker, new exercise, reorder, set types, Finish) with no `presentationBackground`;
- menus (set kind, exercise, group, rest presets, toolbar overflow) and confirmation dialogs and alerts.

**Glass, custom, in the Rows layout** (§7.6a): the capsule at the bottom, the keypad that floats in its place, and the
undo offer above it, each `.glassEffect(.regular)` (the capsule `.interactive()`, tinted orange for a warm-up or a drop),
with opaque fills for the buttons on them. Nothing else in Rows is glass.

**Glass, custom, in the Table layout, exactly one element**: the rest bar in the session cover: a `safeAreaBar(edge: .bottom)` holding a
`GlassEffectContainer(spacing: 6)` (under the row's spacing, so the controls stay separate shapes); the countdown pill uses `.glassEffect(.regular.tint(color))` and
`glassEffectID("countdown", in: namespace)` to morph between its forms; −15 and +15 use `.buttonStyle(.glass)`; Skip,
Stop and the bar's Finish use `.buttonStyle(.glassProminent)`. The "Next" line under a running rest is a capsule of
`.regular.interactive()` glass in the same container (`glassEffectID("next")`), so it reads over the rows. The tint is
the accent colour while resting, green when rest is over, orange past a timed set's target. The bar fades out while a
sheet is up, so its tint never shows through the sheet's glass; the RPE sheet's own −15, +15 and Skip are plain fills,
not glass (no glass in a sheet).

**Content, never glass**: routine cards and folder headers, the preview list, the session header stats, exercise cards,
group headers and brackets, the set grid and its cells, PREVIOUS, reason lines, the Rows blocks, set lines, editors,
steppers and RPE strip, history rows and session detail, the
exercise detail and its chart, Settings rows, the Finish sheet's values, empty states. Buttons in content use
`.borderedProminent` (a card's Start) or `.bordered` (Add set, Add exercise, the Rest and Note chips).

**Rules**:

- Never put `.glassEffect` inside the accessory, a sheet, a menu or a toolbar: no glass on glass.
- `.regular` glass only, never `.clear` (no photo or video backgrounds).
- `.scrollEdgeEffectStyle(.soft, for: .bottom)` on the session scroll view, so cards fade under the rest bar.
- "Start" and "Finish" are buttons, never tabs (HIG Tab bars).
- Respect Reduce Transparency and Increase Contrast: the system glass adapts; the rest bar's pill must stay legible with
  them on (checked on a device, §9.10).

## 9. Platform

### 9.1 Live Activity

Shared by the app and the widget (`the app/Shared/WorkoutsActivityAttributes.swift`):

```swift
struct WorkoutsActivityAttributes: ActivityAttributes {
    struct ContentState: Codable, Hashable {
        var clockStart: Date            // session start plus every closed pause, so Text(timerInterval:) shows elapsed time
        var clockPausedAt: Date?        // while paused: Text(timerInterval:pauseTime:)
        var exercise: String            // the Up next row's exercise, "Chest Press (Machine)"
        var groupLabel: String?         // "A1"
        var next: String                // "Set 2: 35 kg × 14", "All sets done"
        var nextRowID: String?          // the row Done completes
        var countdown: Countdown?
        var setsDone: Int
        var setsPlanned: Int
        var finishedSummary: String?    // "Saved: 46 min, 24 sets" after Finish
        var revision: Int
        struct Countdown: Codable, Hashable {
            enum Kind: String, Codable, Hashable { case rest, roundRest, blockRest, transition, pause, sideSwitch, timedSet, manual }
            var kind: Kind
            var label: String           // "Rest", "Round rest", "Plank, set 1"
            var startedAt: Date
            var endsAt: Date?           // nil for a stopwatch
        }
    }
    var sessionID: String
    var title: String                   // the routine name or "Training session"
}
```

The content stays far below the 4 KB limit. `staleDate` is the countdown's end, so at that instant `context.isStale`
flips with no app code running and the view switches to "Rest over" (req 27); the app also pushes a fresh state when it
next runs.

**Layouts** (`SubstrateWorkoutsWidgets/SessionLiveActivity.swift`):

```text
Lock Screen, resting                              Lock Screen, rest over (stale) or working
+--------------------------------------------+    +--------------------------------------------+
| ◆ Fully Body Collection #1          24:16  |    | ◆ Fully Body Collection #1          25:40  |
| Chest Press (Machine)                      |    | Rest over                                  |
| Next: set 2, 35 kg × 14                    |    | Go: Chest Press (Machine), set 2,          |
| ▓▓▓▓▓▓▓▓▓▓▓▓░░░░░░░                        |    | 35 kg × 14                                 |
| [−15s]       1:16       [+15s]   [Skip]    |    |                                  [ Done ]  |
+--------------------------------------------+    +--------------------------------------------+

Timed set                                         After Finish (five minutes, then dismissed)
+--------------------------------------------+    +--------------------------------------------+
| ◆ Fully Body Collection #1          31:02  |    | ◆ Fully Body Collection #1                 |
| Plank, set 1                               |    | Saved: 46 min, 24 sets                     |
| 0:42 left                        [ Stop ]  |    +--------------------------------------------+
+--------------------------------------------+

Dynamic Island
  compact:  [dumbbell.fill or "A1"]  ...  [1:16]          (elapsed time when nothing counts down)
  minimal:  [1:16]
  expanded: leading "Chest Press (Machine)", trailing the countdown,
            bottom "Next: set 2, 35 kg × 14" and the buttons of the Lock Screen state
```

- Counting views are `Text(timerInterval:countsDown:)` and `ProgressView(timerInterval:countsDown:)`, which tick with no
  app code. A stale or finished state never shows a live 0:00 with buttons.
- The next set's values ("Set 2: 105 kg × 10") are the line the owner reads between sets: primary and medium weight on
  the Lock Screen, in the expanded Dynamic Island and on the watch. The word "Next", the routine title and the clock are
  secondary; the countdown and the buttons carry the tint.
- `.supplementalActivityFamilies([.small])` with a watch layout: the countdown large, the Next line, no buttons. It costs
  a few lines and the watch is where the owner looks (06 §1.12).
- `widgetURL(substrate-workouts://session)` opens the session cover scrolled to Up next.

**Buttons and how they reach the engine** (`the app/Shared/SessionIntents.swift`, compiled into both targets; bodies
only in the app, behind `#if WORKOUTS_APP`):

```swift
struct AdjustRestIntent: LiveActivityIntent {           // −15 s and +15 s
    static let title: LocalizedStringResource = "Adjust Rest"
    static let isDiscoverable = false
    @Parameter(title: "Seconds") var seconds: Int
    init() {}
    init(seconds: Int) { self.seconds = seconds }
    @MainActor func perform() async throws -> some IntentResult {
        #if WORKOUTS_APP
        await SessionController.shared.send(.adjustRest(seconds: seconds))
        #endif
        return .result()
    }
}
// SkipRestIntent            -> .skipRest
// CompleteSetIntent(rowID)  -> .completeUpNext(rowID): only if that row is still pending and still Up next; commits its ghost values (§5.5)
// StopTimedSetIntent(rowID) -> .stopTimedSet(rowID)
```

No dialog can ask from the Lock Screen, so Done and Stop run §5.7's entry check: a set it would question (a typed 400
for 40) is not committed and waits for the owner in the app. The presenter's `canComplete` says whether the button can
work, and the widget shows Done, or Stop while a timed set runs, only then: not when a value must be typed first (a drop
to failure, a weighted plank or carry with no load), nor when the entry check would ask. A commit for a row also closes
an entry-check question still open for it.

A `LiveActivityIntent` runs in the app's process; the system launches the app in the background if needed, and the
controller loads the journal (readable while locked thanks to its protection class) before handling the command. Two
quick taps interleave at every `await`; the revision guard and the update timestamp make the newest change win (§6.2).

**Lifecycle**: requested with `Activity.request(attributes:content:pushType: nil)` in the foreground at Start, or at
restore when missing and not dismissed. Ended at Finish with the summary state and `.after(.now + 5 * 60)`, at Discard
with `.immediate`. A `.dismissed` state from `activityStateUpdates` is respected for the rest of that session.

### 9.2 Notifications

- One pending request at a time, identifier `workouts.countdown`, `UNTimeIntervalNotificationTrigger(timeInterval:
  max(1, end − now), repeats: false)`, `interruptionLevel = .timeSensitive`, `sound = .default`, `threadIdentifier` =
  the session id. Replaced on every change (§6.2); removed when the countdown is skipped, when a set is ticked, at Finish
  and at Discard.
- Only countdowns that notify by §4.7 schedule one, plus a timed set's target.
- Copy:

  | Countdown                 | Title        | Body                                                          |
  | ------------------------- | ------------ | ------------------------------------------------------------- |
  | set rest, override        | "Rest over"  | "Next: Chest Press (Machine), set 2: 35 kg × 14"              |
  | warm-up rest              | "Rest over"  | "Next: Chest Press (Machine), set 1: 40 kg × 11"              |
  | round rest                | "Round 2"    | "Next: Glute Bridge (A1), set 2: × 15"                        |
  | rest before next exercise | "Rest over"  | "Next exercise: Seated Row (Machine), set 1: 50 kg × 9"       |
  | long transition           | "Go"         | "Next: Vertical Traction (Machine) (A2), round 1: 45 kg × 12" |
  | timed set target          | "Time"       | "Plank, set 1: 1:07 reached"                                  |
  | standalone timer          | "Timer done" | none                                                          |

- A `UNUserNotificationCenterDelegate` returns no presentation options while the session cover is visible (the haptic
  and the bar already say it) and `[.banner, .sound]` otherwise. Tapping a notification opens the cover.
- Permission (`.alert`, `.sound`) is requested at the first Start. If the time-sensitive entitlement does not provision
  (§9.10), the same requests go out at the default `.active` level with no code change other than the entitlement.
  Settings reads the state back: "Rest alerts: on, time sensitive"; "on, not time sensitive. A Focus may hold them
  back." (the owner switched it off, or the entitlement is missing) with Open Settings; "not asked yet" before the first
  Start; or off.

### 9.3 Haptics and sound

`sensoryFeedback` on the views, triggered by presentation changes:

| Moment                                         | Feedback                   |
| ---------------------------------------------- | -------------------------- |
| A tick (Table)                                 | `.impact(weight: .medium)` |
| A Log journaled (Rows)                         | `.success`                 |
| A stepper step or an RPE pick (Rows)           | `.selection`               |
| A notifying countdown ends in the foreground   | `.success`                 |
| A short pause, side switch or transition ends  | `.impact(weight: .heavy)`  |
| A timed set reaches its target                 | `.success`                 |
| The entry check asks (§5.7), a refused command | `.warning`                 |
| Finish saved                                   | `.success`                 |

No sound in the foreground by default: the owner may be listening to music (04 §4.4).

### 9.4 Keep the screen awake

`UIApplication.shared.isIdleTimerDisabled = settings.keepAwake && sessionCoverVisible && scenePhase == .active`,
re-evaluated when any of the three changes, and set back to `false` when the cover closes.

### 9.5 Apple Health

**Body mass at Start**: if read access to `bodyMass` was granted, the latest sample ending on or before now
(`HKSampleQuery`, sorted by end date, limit 1), converted to kg and rounded to 0.1; else the typed value in Settings;
else none (bodyweight exercises then use 03 §11.5). Start never prompts for Health.

**The workout at Finish** (`HealthExporter.save(records)`, called from the queue):

| HealthKit                        | Value                                                                                                                                                                                                                                                                   |
| -------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Builder                          | `HKWorkoutBuilder(healthStore:configuration:device: .local())`                                                                                                                                                                                                          |
| Activity type                    | `session.activityType` mapped to `.traditionalStrengthTraining`, `.functionalStrengthTraining`, `.coreTraining`, `.highIntensityIntervalTraining`; absent: `.traditionalStrengthTraining`                                                                               |
| Location                         | `.indoor`                                                                                                                                                                                                                                                               |
| Start, end                       | `beginCollection(at: session.at)`, `endCollection(at: session.endsAt)`                                                                                                                                                                                                  |
| Pauses                           | `addWorkoutEvents`: a `.pause` and a `.resume` event per pause                                                                                                                                                                                                          |
| `HKMetadataKeyExternalUUID`      | the session id                                                                                                                                                                                                                                                          |
| `HKMetadataKeySyncIdentifier`    | the session id                                                                                                                                                                                                                                                          |
| `HKMetadataKeySyncVersion`       | `session.revision` at Finish, plus the queue entry's failed attempts (a retry HealthKit would not take as newer replaces the old workout)                                                                                                                               |
| `HKMetadataKeyIndoorWorkout`     | `true`                                                                                                                                                                                                                                                                  |
| `HKMetadataKeyTimeZone`          | `session.timezone`                                                                                                                                                                                                                                                      |
| Effort                           | when `sessionRPE` is 1 or more (`session.healthEffortScore`; a CR10 0 has no effort score and is never sent as 1): a `workoutEffortScore` sample (`.appleEffortScore()` unit) over the session, saved and linked with `relateWorkoutEffortSample(_:with:activity: nil)` |
| Energy, heart rate, per-exercise | none                                                                                                                                                                                                                                                                    |

`finishWorkout()` returns the workout; its `uuid.uuidString` (uppercase) goes into the history file's session record
as `healthKitWorkout`, and the session's `revision` rises by one. The sink's `healthLinked` is called (V2 PATCHes it).
No re-save after any later change. Before building, the exporter looks for a workout it saved earlier with the same
`HKMetadataKeySyncIdentifier` (the app died after `finishWorkout()` and before the history file was told; HealthKit
shows an app its own samples without read access) and links that one instead of writing a second.

**The queue**: `health-queue.json` lists session ids to save. Finish writes the entry right after the history file and
before deleting the journal, with no `await` in between, and starts the save once the cover has closed. It is
processed then, at launch and on each return to the foreground, inside
`UIApplication.beginBackgroundTask(withName:expirationHandler:)`, whose expiry handler ends the task (UIKit ends an app
whose task outlives its time; the queue keeps the session). The permission sheet, which waits on the owner, is asked
before the task begins. At launch and each foreground, every history entry still `pending` with no workout that the
queue has lost is queued again, and a queue file that no longer decodes is set aside first. Outcomes:

- Saved: remove from the queue; History shows "saved".
- `isHealthDataAvailable()` false: remove, record "not available", show nothing.
- Sharing not determined: keep queued; Settings and the session detail (for a session still pending) offer "Connect".
- Sharing denied: keep a local "not saved" status; the session detail offers "Try again" after access is given in the
  Health app; the queue does not retry it on its own.
- Any other error: keep queued with the error; retry next time, at most once per foreground.

**Authorization**: requested in context at the first Finish (or from Settings' "Connect"), with
`requestAuthorization(toShare: [workoutType, workoutEffortScore], read: [bodyMass])`.

### 9.6 Permissions, when each is asked

| Permission                                            | Asked                                  | If refused                                                                |
| ----------------------------------------------------- | -------------------------------------- | ------------------------------------------------------------------------- |
| Notifications                                         | First Start                            | Session banner "Alerts are off"; the Live Activity and haptics still work |
| Live Activities                                       | No prompt; the owner can turn them off | The app works without; the accessory still shows the countdown            |
| Apple Health (share workouts, effort; read body mass) | First Finish, or Settings' Connect     | Sessions save locally; History shows "not saved"                          |

### 9.7 Entitlements and Info.plist

App target (`Support/the app.entitlements`, generated by xcodegen from `project.yml`):

| Key                                                    | Value  | Why                          |
| ------------------------------------------------------ | ------ | ---------------------------- |
| `com.apple.developer.healthkit`                        | `true` | Saving the workout           |
| `com.apple.developer.healthkit.access`                 | `[]`   | No extra-sensitive types     |
| `com.apple.developer.usernotifications.time-sensitive` | `true` | `.timeSensitive` rest alerts |

No `aps-environment` (no push), no background modes, no App Group, no AlarmKit key in V1.

App `Info.plist` additions:

| Key                              | Value                                                                                   |
| -------------------------------- | --------------------------------------------------------------------------------------- |
| `NSSupportsLiveActivities`       | `true`                                                                                  |
| `NSHealthUpdateUsageDescription` | "the app saves each finished training session to Apple Health as one strength workout." |
| `NSHealthShareUsageDescription`  | "the app reads your latest body mass to work out bodyweight and assisted sets."         |
| `CFBundleURLTypes`               | one type, scheme `substrate-workouts`                                                              |

Widget `Info.plist`: `CFBundleDisplayName` "the app", `CFBundleShortVersionString` `$(MARKETING_VERSION)`,
`CFBundleVersion` `$(CURRENT_PROJECT_VERSION)`, `NSExtension` with `NSExtensionPointIdentifier`
`com.apple.widgetkit-extension`. The version keys must use the same variables as the app's: `release.sh` passes them
on the command line for every target, and checks only the app's own plist (04 §6), so a hardcoded value would slip
through. 

### 9.8 project.yml changes

```yaml
settings:
  base:
    # ... existing keys ...
    # Moved up from the app target so the widget extension shares them; release.sh
    # overrides both on the command line.
    MARKETING_VERSION: "0.1.0"
    CURRENT_PROJECT_VERSION: "1"
targets:
  the app:
    # ... existing keys ...
    dependencies:
      - package: WorkoutsKit
        product: WorkoutsCore
      - package: SubstrateKit
        product: SubstrateKit
      # The Live Activity's UI.
      - target: SubstrateWorkoutsWidgets
        embed: true
    entitlements:
      path: Support/the app.entitlements
      properties:
        com.apple.developer.healthkit: true
        com.apple.developer.healthkit.access: []
        com.apple.developer.usernotifications.time-sensitive: true
    info:
      path: Support/Info.plist
      properties:
        # ... existing keys ...
        NSSupportsLiveActivities: true
        NSHealthUpdateUsageDescription: the app saves each finished training session to Apple Health as one strength workout.
        NSHealthShareUsageDescription: the app reads your latest body mass to work out bodyweight and assisted sets.
        CFBundleURLTypes:
          - CFBundleURLName: dev.reamde.substrate.workouts
            CFBundleURLSchemes: [substrate-workouts]
    settings:
      base:
        # ... existing keys, minus MARKETING_VERSION and CURRENT_PROJECT_VERSION ...
        # WORKOUTS_APP gates the LiveActivityIntent bodies that call SessionController;
        # the widget compiles the same Shared/ files without it.
        SWIFT_ACTIVE_COMPILATION_CONDITIONS: "$(inherited) WORKOUTS_APP"
        # Preview-only data (generated example sessions) stays out of archives.
        DEVELOPMENT_ASSET_PATHS: '"the app/Preview Content"'
  SubstrateWorkoutsWidgets:
    type: app-extension
    platform: iOS
    sources:
      - SubstrateWorkoutsWidgets
      - the app/Shared
    info:
      path: SubstrateWorkoutsWidgets/Info.plist
      properties:
        CFBundleDisplayName: the app
        CFBundleShortVersionString: $(MARKETING_VERSION)
        CFBundleVersion: $(CURRENT_PROJECT_VERSION)
        NSExtension:
          NSExtensionPointIdentifier: com.apple.widgetkit-extension
    settings:
      base:
        PRODUCT_BUNDLE_IDENTIFIER: dev.reamde.substrate.workouts.widgets
        GENERATE_INFOPLIST_FILE: "NO"
        TARGETED_DEVICE_FAMILY: "1"
        IPHONEOS_DEPLOYMENT_TARGET: "27.0"
        SKIP_INSTALL: "YES"
        SWIFT_DEFAULT_ACTOR_ISOLATION: MainActor
        SWIFT_APPROACHABLE_CONCURRENCY: "YES"
```

`.gitignore` gains `Support/the app.entitlements` and `SubstrateWorkoutsWidgets/Info.plist`, both generated, as
`Support/Info.plist` already is. The widget's bundle id is registered by `-allowProvisioningUpdates` like the app's;
`release.sh` needs no change.

### 9.9 Package.swift changes

```swift
.target(
    name: "WorkoutsCore",
    // The owner's exercises and routines, generated by scripts/examples.py.
    resources: [.copy("Resources/Bundled")]
),
.testTarget(
    name: "WorkoutsCoreTests",
    dependencies: ["WorkoutsCore"],
    // Every example record and kinds.json, generated by scripts/examples.py.
    resources: [.copy("Fixtures")]
),
```

`WorkoutsCore.schemaVersion` becomes `journalFormat = 1` and `historyFormat = 1`, the `format` numbers of §2.3.

### 9.10 Verify on a device

The simulator cannot prove these. Each needs one pass on the owner's iPhone before V1 is called done (04 DI; the
research README's "Still to check on a device"):

1. **Locked rest**: tick a set, lock the phone; the time-sensitive notification arrives at the end, with Focus on.
2. **Stale Live Activity**: at the rest's end, with the app suspended, the Lock Screen view switches to "Rest over"
   without the app running; Dynamic Island compact and minimal match.
3. **Live Activity buttons**: −15, +15, Skip and Done from the Lock Screen and the Dynamic Island, including two fast
   taps on +15 and after the app was swiped away (a cold background launch).
4. **Crash and reboot**: swipe the app away mid-rest and relaunch; reboot mid-session and unlock; the session, the
   countdown and the alert are all consistent.
5. **Time-sensitive provisioning**: `mise run apple:release:workouts` signs with the
   time-sensitive capability through the App Store Connect key; if not, add it once in the developer portal or ship with
   `.active`.
6. **HealthKit provisioning**: the same dry run registers HealthKit on `dev.reamde.substrate.workouts` (the key may need the Admin
   role, 04 §6).
7. **The workout**: appears in Health and Fitness as traditional strength training, indoor, with the right times and
   pauses; the effort score shows on it; the metadata reads back in the Health app (Browse > Activity > Workouts > Show
   All Data > the workout: External UUID and Sync Identifier are the session id, the time zone is the session's);
   whether it earns Exercise ring minutes is noted. V1 has no in-app readback.
8. **Denied Health**: Finish still saves locally; History shows "not saved"; "Try again" works after access is given.
9. **Watch**: the Live Activity's `.small` layout in the Smart Stack, and the rest notification on the wrist.
10. **Keep awake**: on and off; the screen dims normally after the cover closes.
11. **Glass**: the rest bar and the accessory with Reduce Transparency and Increase Contrast, light and dark.
12. **Body mass**: read at Start when access is granted.

## 10. Test plan

All WorkoutsKit tests run on the Mac with `cd apps/apple/WorkoutsKit && swift test` (Swift Testing, `@Test`),
and from `xcodebuild test` through the scheme's test target.

### 10.1 Unit tests

| File                      | Covers                                                                                                                                                                                                                                                  |
| ------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `CodecTests`              | §10.4                                                                                                                                                                                                                                                   |
| `EngineTests`             | Every event's success and each `EngineError`; invariants (§4.4) after every event; `fold(events) == state` after every event of every test                                                                                                              |
| `RestRuleTests`           | Every line of §4.7, every default, the notify rule; the example replays of §10.5                                                                                                                                                                        |
| `UpNextTests`             | The plan sequence for plain slots, groups with uneven members (A 4, B 3, C 2), warm-ups before round 1, chains, wrap-around, focus override, all done                                                                                                   |
| `ChainTests`              | Drop rows created with the head and removed at the minimum; myo stop by `repdrop`, `belowtarget`, cap and total; myo-match total resolution; rest-pause and cluster; side chains with `sideMatch` and `weakerfirst`; edits that stop or restart a chain |
| `SuggestVectorTests`      | 03 §14, V1 to V19 (§10.2), plus `p`, `pInv` at every published point and the 03 §9.1 rounding checks                                                                                                                                                    |
| `ExampleProgressionTests` | §10.3                                                                                                                                                                                                                                                   |
| `PresenterTests`          | Columns per measurement; labels (§4.10); ghost precedence; PREVIOUS matching by ordinal, chain position and side; the reason line only on Up next; `rirstop` hints                                                                                      |
| `FormatTests`             | Every formatter example in §4.13 and 03 §12                                                                                                                                                                                                             |
| `JournalTests`            | Atomic write and reload; refold mismatch handling, a rewrite that fails; format number; an unreadable journal set aside                                                                                                                                 |
| `HistoryStoreTests`       | Load, index by exercise newest first, skip an undecodable file, delete; the Health queue's lifecycle, lost sessions queued again, an unreadable queue set aside                                                                                         |
| `ReviewFixTests`          | The V1 review's fixes: cluster heads, failure sets switched in the gym, rest clocks around removals and same-second ticks, replacements in a group, no-grid drops, the rest bar's and Lock Screen's forms                                               |
| `UUIDv7Tests`             | Version and variant bits, timestamp equals the instant, lowercase, ordering                                                                                                                                                                             |

### 10.2 Suggestion vectors, verbatim from 03 §14

Each is one `@Test` asserting the load, reps (or duration), `stop`, reason and the exact text; intermediate numbers to
1e-4 where the vector gives them.

Defaults unless a vector says otherwise: `defaultRPE` 8, `repGain` 1, gap 7 days, no other history, body mass constant,
`myoStopRule` repDrop, `myoMaxMiniSets` 5, `dropPercent` 0.20, `timeStep` 5 s. Grids: barbell kg is `base 20, step
2.5`; stack kg is `base 0, step 5`; dumbbell kg is `base 0, step 2`; barbell lb is `base 45, step 5`. "Target" is the
pending set's target; "stored target" is the target recorded on the history set.

**V1. First set from history, top of range reached (load up).** Chest press (machine), stack kg (the exercise in the
owner's screenshot). History: set 1 was 35 × 15, no RPE, stored target 10–15 at RPE 9.5. Pending: working set 1, target
10–15 at RPE 9.5. Nothing done today.

- Effective RPE 9.5 (met the target, so assume the target). `n = 15.5`, `p = 0.586`, `E = 35 / 0.586 = 59.726962`.
- `repsAt(35) = floor(15.5 − 0.5) = 15`; goal `15 + 1 = 16 > 15`, so load up one step to 40.
- `q = 40 / 59.726962 = 0.669714`; `pInv = 12 + 0.5 × (0.680 − 0.669714) / 0.013 = 12.395604`; reps
  `floor(12.395604 − 0.5) = 11`; clamp to 10–15 gives 11.
- Expected: load 40, reps 11, `historyLoadUp`, "Last time 35 kg × 15, top of range; try 40 kg × 11".

**V2. First set from history, add a rep (the owner's example).** Same exercise. History: set 2 was 35 × 13 at RPE 9.5
(logged), stored target 10–15 at RPE 9.5. Pending: working set 2, target 10–15 at RPE 9.5. No working set done today.

- `n = 13.5`, `p = 0.640`, `E = 54.6875`. `repsAt(35) = floor(13.5 − 0.5) = 13`; goal 14, inside 10–15.
- Expected: load 35, reps 14, `historyAddRep`, "Last time 35 kg × 13 at RPE 9.5; try 35 kg × 14".

**V3. In-session, one point easier than the target.** Bench press, barbell kg, sets of 8–12 at RPE 8. Today set 1: 60 ×
10 at RPE 7. Pending: set 2, same target.

- `F = 0`, `n = 13`, `E = 60 / 0.653 = 91.883614`, `r0 = floor(13 − 2) = 11`, inside the range.
- Expected: load 60, reps 11, `sessionKeep`, "Set 1: 60 kg × 10 at RPE 7; try 60 kg × 11".

**V4. In-session, much easier than the target (load up).** Same, but set 1 was 60 × 12 at RPE 6.

- `F = 0`, `n = 16`, `E = 60 / 0.572 = 104.895105`, `r0 = 14 > 12`.
- `E × p(15) = 104.895105 × 0.599 = 62.832168`; `floorG = 62.5`; `next = 65`. `next(60) = 62.5`; cap
  `max(62.5, floorG(66) = 65) = 65`; `L = 65`.
- `q = 65 / 104.895105 = 0.619667`; `pInv = 14 + 0.5 × (0.626 − 0.619667) / 0.013 = 14.243590`; reps
  `floor(12.243590) = 12`, inside 8–12.
- Expected: load 65, reps 12, `sessionUp`, "Set 1: 60 kg × 12 at RPE 6, easier than RPE 8; try 65 kg × 12".

**V5. In-session, harder than the target (load down).** Squat, barbell kg, sets of 5 at RPE 8 (`lo = hi = 5`). Today
set 1: 100 × 5 at RPE 9.5.

- `F = 0.5`; `n = 5.5`, so `n − F = 5.0`; `E = 100 / 0.863 = 115.874855`; `r0 = floor(5 − 2) = 3 < 5`.
- `E × p(7) = 115.874855 × 0.811 = 93.974508`; `floorG = 92.5`.
- `q = 0.798275`; `pInv = 7.5 + 0.5 × (0.799 − 0.798275) / 0.013 = 7.527885`; reps `floor(5.527885) = 5`.
- Expected: load 92.5, reps 5, `sessionDown`, "Set 1: 100 kg × 5 at RPE 9.5, harder than RPE 8; try 92.5 kg × 5".

**V6. In-session fatigue allowance.** Bench press, barbell kg, sets of 8–12 at RPE 8. Today set 1: 60 × 10 at RPE 9.

- `F = 0.5`; `n = 11`, so `n − F = 10.5`; `E = 60 / 0.723 = 82.987552`; `r0 = floor(10.5 − 2) = 8`, inside the range.
- Expected: load 60, reps 8, `sessionKeep`, "Set 1: 60 kg × 10 at RPE 9; try 60 kg × 8".

**V7. Missed reps last time, no RPE.** Barbell row, barbell kg. History: set 1 was 40 × 6, no RPE, stored target 8–12 at
RPE 8. Pending: set 1, target 8–12 at RPE 8.

- Effective RPE 10 (fell short of 8). `n = 6`, `E = 40 / 0.837 = 47.789725`. `repsAt(40) = floor(6 − 2) = 4`; goal
  5 < 8.
- `E × p(10) = 47.789725 × 0.739 = 35.316607`; `floorG = 35`.
- `q = 0.732375`; `pInv = 10 + 0.5 × (0.739 − 0.732375) / 0.016 = 10.207031`; reps `floor(8.207031) = 8`.
- Expected: load 35, reps 8, `historyLoadDown`, "Last time 40 kg × 6, below target; try 35 kg × 8".

**V8. Warm-up sets are ignored.** V1 plus: the history session also had a warm-up 25 × 15 at RPE 6 before its working
sets, and today a warm-up 25 × 15 at RPE 6 is already completed.

- 8a. Pending working set 1: the warm-ups are not `W` and not ordinal 1. Expected: identical to V1 (load 40, reps 11,
  same text).
- 8b. Pending warm-up 1 (before it is done): expected load 25, reps 15, `warmupAsLastTime`, "Warm-up, as last time: 25
  kg × 15".

**V9. Myo-rep mini-sets.** Machine lateral raise, stack kg. Today: activation set 25 × 17 at RPE 9 done. No
`myoMiniReps` in the plan, so `t1 = clamp(round(17 / 4 = 4.25) = 4, 3, 5) = 4`.

- 9a. No mini-sets yet. Expected: load 25, reps 4, `myoMini`, "Mini-set 1: 25 kg × 4".
- 9b. Mini-sets 4, 4, 4 done. Target for `j ≥ 2` is `m1 = 4`. Expected: load 25, reps 4, `myoMini`, "Mini-set 4:
  25 kg × 4".
- 9c. Mini-sets 4, 4, 4, 3 done. Mini-set 4 has 3 < 4. Expected: `stop = true`, `myoStop`, "Stop: mini-set 4 got 3,
  first was 4".
- 9d. Mini-sets 5, 5, 5, 5, 5 done. Expected: `stop = true`, `myoStop`, "Stop: 5 mini-sets done".
- Rounding check: `rA = 18` gives `round(4.5) = 5`; `rA = 13` gives 3; `rA = 21` gives 5.

**V10. Drop set.** Seated row (machine), stack kg. The parent set 35 × 8 at RPE 10 is done.

- 10a. `35 × 0.8 = 28`; `floorG(28) = 25`. Expected: load 25, reps nil, `drop`, "Drop 20% from 35 kg: 25 kg to
  failure".
- 10b. A second drop after 25: `25 × 0.8 = 20`; `floorG(20) = 20`. Expected: load 20, reps nil, "Drop 20% from
  25 kg: 20 kg to failure".

**V11. Timed set.** Plank, bodyweight, no duration range, no RPE. History: set 1 held 62 s, set 2 held 53 s (the owner's
screenshot).

- 11a. Pending set 1: `62 + 5 = 67`. Expected: duration 67, `timedHistory`, "Last time 1:02; try 1:07".
- 11b. Pending set 2, set 1 not yet done: `53 + 5 = 58`. Expected: duration 58, "Last time 0:53; try 0:58".
- 11c. Pending set 2, today set 1 held 55 s: `min(58, 55) = 55`. Expected: duration 55, `timedSession`, "Set 1
  held 0:55; try 0:55".

**V12. Bodyweight with added load.** Pull-up, bodyweightAdded, `bodyShare 1.0`, body mass 80 kg, `canAddLoad`, step 2.5
kg. History: set 1 was BW × 10 at RPE 9 (logged), stored target 6–10 at RPE 9. Pending: set 1, target 6–10 at RPE 9.

- Effective load 80. `n = 11`, `E = 80 / 0.707 = 113.154173`. `repsAt(80) = floor(11 − 1) = 10`; goal 11 > 10, so one
  step to 82.5.
- `q = 82.5 / 113.154173 = 0.729094`; `pInv = 10 + 0.5 × (0.739 − 0.729094) / 0.016 = 10.309570`; reps
  `floor(9.309570) = 9`.
- Expected: added load 2.5 (effective 82.5), reps 9, `historyLoadUp`,
  "Last time BW × 10 at RPE 9, top of range; try BW + 2.5 kg × 9".

**V13. Assisted.** Assisted pull-up machine, bodyweightAssisted, body mass 80 kg, assistance step 5 kg. History: set 1
was assistance 30 × 12 at RPE 8, stored target 8–12 at RPE 8. Pending: set 1, target 8–12 at RPE 8.

- Effective load `80 − 30 = 50`. `n = 14`, `E = 50 / 0.626 = 79.872204`. `repsAt(50) = 12`; goal 13 > 12, so one step
  heavier: assistance 25, effective 55.
- `q = 55 / 79.872204 = 0.688600`; `pInv = 11.5 + 0.5 × (0.694 − 0.6886) / 0.014 = 11.692857`; reps
  `floor(9.692857) = 9`.
- Expected: assistance 25, reps 9, `historyLoadUp`,
  "Last time BW − 30 kg × 12 at RPE 8, top of range; try BW − 25 kg × 9".

**V14. No history.** Bench press, barbell kg, pending set 1, target 8–12 at RPE 8.

- 14a. The routine gives 50 kg. Expected: load 50, reps 8, `coldStartRoutineLoad`, "First time; routine load,
  try 50 kg × 8".
- 14b. No routine load. `floor(8 + 10 − 8) = 10`. Expected: load nil, reps 8, `coldStartPickLoad`, "First time: pick a
  load for about 10 reps; do 8".

**V15. Pounds, with history stored in kg.** Bench press, unit lb, barbell lb. History: set 1 stored as 83.9146 kg × 8 at
RPE 8 (entered as 185 lb), stored target 6–8 at RPE 8. Pending: set 1, target 6–8 at RPE 8.

- `83.9146 / 0.45359237 = 185.0000255`, rounded to 0.01: 185.
- `n = 10`, `E = 185 / 0.739 = 250.338295`. `repsAt(185) = 8`; goal 9 > 8, so one step to 190.
- `q = 190 / 250.338295 = 0.758973`; `pInv = 9 + 0.5 × (0.762 − 0.758973) / 0.011 = 9.137592`; reps
  `floor(7.137592) = 7`.
- Expected: load 190, reps 7, `historyLoadUp`, "Last time 185 lb × 8 at RPE 8, top of range; try 190 lb × 7".

**V16. Long break.** Barbell kg. History: set 1 was 60 × 10 at RPE 8, stored target 8–12 at RPE 8, 45 days ago. Pending:
set 1, target 8–12 at RPE 8.

- `E = 60 / 0.680 × 0.95 = 83.823529`, `g = 0`.
- `q = 60 / 83.823529 = 0.715789`; `pInv = 10.5 + 0.5 × (0.723 − 0.715789) / 0.016 = 10.725329`;
  `repsAt = floor(8.725329) = 8`; goal 8.
- Expected: load 60, reps 8, `historyAddRep`, "Last time 60 kg × 10 at RPE 8 (45 days ago); try 60 kg × 8".

**V17. New rep range.** Barbell kg. History: set 1 was 100 × 5 at RPE 8, stored target 5 at RPE 8. Pending: set 1,
target 10–12 at RPE 8.

- `5 < 10 − 2`, so the scheme changed. `E = 100 / 0.811 = 123.304562`. `mid = 11`.
  `E × p(13) = 123.304562 × 0.653 = 80.517879`; `floorG = 80`.
- `q = 0.648800`; `pInv = 13 + 0.5 × (0.653 − 0.6488) / 0.013 = 13.161538`; reps `floor(11.161538) = 11`.
- Expected: load 80, reps 11, `historyNewRange`, "Last time 100 kg × 5 at RPE 8; for 10–12 reps try 80 kg × 11".

**V18. Unilateral.** Bulgarian split squat, dumbbell kg (load per dumbbell). History: set 1 was 20 kg, left 10, right 9,
at RPE 8, stored target 8–12 at RPE 8. Pending: set 1, same target.

- Reps `min(10, 9) = 9`. `n = 11`, `E = 20 / 0.707 = 28.288543`. `repsAt(20) = floor(11 − 2) = 9`; goal 10.
- Expected: load 20, reps 10 (each side), `historyAddRep`, "Last time 20 kg × 9 at RPE 8; try 20 kg × 10".

**V19. Outlier skipped.** Seated row (machine), stack kg, target 10–15 at RPE 8. History, newest first: S1 (3 days ago)
set 1 = 350 × 12 at RPE 8 (a typo for 35, the only working set); S2 (10 days ago) set 1 = 35 × 12 at RPE 8; S3 (17 days
ago) set 1 = 35 × 11 at RPE 8; S4 (24 days ago) set 1 = 30 × 14 at RPE 8.

- e1RMs: S1 `350 / 0.626 = 559.105431`; S2 `35 / 0.626 = 55.910543`; S3 `35 / 0.653 = 53.598775`; S4
  `30 / 0.572 = 52.447552`.
- For S1's set, the others are S2, S3 and S4: median `53.598775`, upper bound `80.398162`. 559.1 is above it, so S1's set
  is implausible. S1 has no other working set.
- For S2's set, the others are S1, S3 and S4: median `53.598775`; 55.91 lies inside `26.799387 ... 80.398162`. S2 is
  the reference.
- `n = 14`; `repsAt(35) = floor(14 − 2) = 12`; goal 13, inside 10–15.
- Expected: load 35, reps 13, `historyAddRep`, "Last time 35 kg × 12 at RPE 8; try 35 kg × 13".

### 10.3 Example progression vectors

`ExampleProgressionTests` load the generated fixtures and run the whole path (records, history index,
`SuggestionInputs`, `suggest`) over `sessions.json` alone, history newest first, as the examples README computed them.

**Thu 22 Oct (no history):**

- Every first working set: "First time; routine load, try 100 kg × 6" for the leg press, and the same form for each.
- Iso-lateral press set 2: "Set 1: 20 kg × 10 at RPE 7, easier than RPE 8; try 22.5 kg × 8".
- Romanian deadlift set 2: "Set 1: 16 kg × 7, harder than RPE 7; try 12 kg × 10".
- Lateral raise set 2: "Set 1: 6 kg × 13 at RPE 7; try 6 kg × 15".
- The myo set's chain: "Stop: mini-set 4 got 3, first was 4".

**Mon 26 Oct, from 22 Oct:**

| Set                         | Suggested                                                      |
| --------------------------- | -------------------------------------------------------------- |
| Leg press W1, W2            | Warm-up, as last time: 40 kg × 12, then 70 kg × 6              |
| Leg press 1                 | Last time 100 kg × 10 at RPE 7.5, top of range; try 105 kg × 9 |
| Leg press 2                 | Set 1: 105 kg × 9, assuming RPE 8; try 105 kg × 9              |
| Leg press 3                 | Set 2: 105 kg × 9 at RPE 8.5; try 105 kg × 8                   |
| Iso-lateral press 1         | Last time 20 kg × 10 at RPE 7, top of range; try 22.5 kg × 8   |
| Iso-lateral press 2         | Set 1: 22.5 kg × 10 at RPE 8; try 22.5 kg × 10                 |
| Iso-lateral press 3         | Set 2: 22.5 kg × 9 at RPE 8.5; try 22.5 kg × 8                 |
| Seated row 1                | Last time 45 kg × 12 at RPE 8, top of range; try 50 kg × 9     |
| Seated row 2                | Set 1: 50 kg × 10 at RPE 8; try 50 kg × 10                     |
| Seated row 3                | Set 2: 50 kg × 10 at RPE 8.5; try 50 kg × 9                    |
| Lateral raise 1             | Last time 6 kg × 13 at RPE 7; try 6 kg × 15                    |
| Lateral raise 2, activation | Set 1: 6 kg × 14 at RPE 7; try 6 kg × 16                       |
| Lateral raise 2, mini-sets  | Mini-set 1 to 5: 6 kg × 4, then "Stop: 5 mini-sets done"       |
| Romanian deadlift 1         | Last time 16 kg × 7, below target; try 12 kg × 11              |
| Romanian deadlift 2         | Set 1: 12 kg × 12 at RPE 7; try 12 kg × 12                     |
| Romanian deadlift 3         | Set 2: 12 kg × 12 at RPE 7.5; try 12 kg × 11                   |

Leg press set 1 was logged with the plan's RPE 8 pre-filled and untouched (`rpeMethod: prefilled`), so set 2 reads it
as no rating and says "assuming" (§5.3).

**The next Fully Body Collection #1 session (30 Oct), before anything is done**, with body mass 82.4 kg:

| Set                                  | Suggested                                                   |
| ------------------------------------ | ----------------------------------------------------------- |
| Chest press W1                       | Warm-up, as last time: 25 kg × 15                           |
| Chest press 1                        | Last time 35 kg × 15, top of range; try 40 kg × 11          |
| Chest press 2                        | Last time 35 kg × 13 at RPE 9.5; try 35 kg × 14             |
| Chest press 2a, after set 2 at 35 kg | Drop 20% from 35 kg: 25 kg to failure                       |
| Plank 1                              | Last time 1:02; try 1:07                                    |
| Plank 2                              | Last time 0:53; try 0:58                                    |
| Bulgarian split squat 1              | Last time 10 kg × 10 at RPE 8.5; try 10 kg × 10             |
| Bulgarian split squat 2              | Last time 10 kg × 9 at RPE 9; try 10 kg × 9                 |
| Push up 1                            | Last time BW × 14 at RPE 9; try BW × 15 (a hint, `rirstop`) |
| Push up 2                            | Last time BW × 11 at RPE 9; try BW × 12 (a hint, `rirstop`) |

Plus the app's own rule (§5.3): in the coverage session, after the 110 kg × 6 top set, the back-off set suggests 95 kg
("Back-off 10% from 110 kg: try 95 kg × …", reps per §5.3).

### 10.4 Codec

- **Round trip**: for every record in the four fixture files, `encode(decode(r))` equals `r` structurally (§3.4 rule 9),
  and every decoded model's `extra` is empty (the model covers every property the examples use).
- **Drift**: the codec's declared property names and object field names for each kind equal `kinds.json` (plus the
  traits' `at` and `endsAt`). A property added to a kind file without a model field fails here.
- **Types**: decimals refuse JSON numbers; `"9.50"` decodes to 9.5 and encodes `"9.5"`; durations `PT90S` decode to 90
  and encode `PT1M30S`, `PT36H` to `P1DT12H`, zero to `PT0S`; datetimes with an offset decode to the instant and encode
  in UTC with `Z`; references refuse another kind; the `workout` reference keeps the `/` in its id.
- **Defaults and status**: a set with `role: working` written out decodes and re-encodes without it; the coverage
  in-progress session (no `status`) round-trips without one; a finished one keeps `status: finished`.
- **Forward compatibility**: an unknown property and an unknown optional enum value survive a round trip, at the top
  level and inside every nested object; an unknown execution enum (a planned set's `structure` `emom`, a set's
  `role`) and an unknown `measurement` make the record undecodable with `CodecError.unsupported`.
- **Served form**: every example record with its references as substrate serves them, `{"ref": …}` at every level,
  decodes to the same values and round-trips in that form; a decorated reference keeps its link fields while it points
  at the same record.
- **Encode from the engine**: a scripted session reproducing the 27 October Fully Body session's ticks (times, values,
  touches, skips) produces `SessionRecords` equal to `sessions.json`'s records for that session, ids included (the test
  passes the fixture's ids and instants in its events), except `healthKitWorkout`, `workout` and `revision`, which V1
  writes differently (§4.12).

### 10.5 Engine scenarios

- **Rest replay**: for each example session, the scripted session's `restPlanned` on every set equals the fixture's,
  including the transitions, round rests, side switches, warm-up rests, block rests and the drop's `PT0S`.
- **Rest measurement**: the 22 October iso-lateral set 3 (`restSkipped`, 1:38 against 2:00), the 26 October pause inside
  a rest, and the plank's `restBefore` ending at the play tap.
- **Any order**: tick a later set first; Up next and the rest follow §4.6 and §4.7.
- **Edits**: un-tick and re-tick keeps the rest fields; editing a mini-set re-evaluates the chain.
- **Structure**: add, remove, replace (both cases), reorder with a group, group and ungroup; group contiguity holds.
- **Finish**: unticked rows dropped and marked done; empty slots and one-member groups dropped; times clamped; ordinals and
  orders renumbered; session revision counted.
- **Countdowns**: −15 never before now; a timed set replaces a rest; two countdowns never coexist.

### 10.6 Build checks

Run before every commit that touches the app (the repository's `mise run precommit` covers the Markdown):

```bash
cd apps/apple
uv run --with pyyaml scripts/examples.py --check            # generated JSON matches the YAML
(cd WorkoutsKit && swift test)                               # all WorkoutsKit tests on the Mac
mise x xcodegen -- xcodegen generate                        # project from project.yml
xcodebuild -project the app.xcodeproj -scheme the app \
  -destination 'generic/platform=iOS Simulator' -derivedDataPath build/dd build
xcodebuild -project the app.xcodeproj -scheme the app \
  -destination 'platform=iOS Simulator,name=<an installed iPhone simulator>' -derivedDataPath build/dd test
mise run apple:release:workouts   # from the repo root: signing, entitlements, widget embedded
mise x -- dprint check apps/apple/docs/DESIGN.md         # from the repo root
```

A grep check in CI or review: `WorkoutsKit/Sources` imports nothing but Foundation (`grep -rn '^import'` shows only
`import Foundation`).

## 11. Work split and order

Five engineers, each owning disjoint paths. Shared files (`project.yml`, `Package.swift`) have one owner each; others
send changes through them.

| Engineer | Owns                                                                                                                                                                                                                                                           | Delivers                                                           |
| -------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| A        | `WorkoutsKit/Package.swift`, `Sources/WorkoutsCore/{Model,Codec,Store,Seams,Resources}/`, `Tests/WorkoutsCoreTests/{Fixtures/,CodecTests,JournalTests,HistoryStoreTests,UUIDv7Tests}.swift`, `scripts/examples.py`, `the app/Preview Content/PreviewHistory.json` | §2.3, §2.4, §2.5, §3, §9.9; §10.4                                  |
| B        | `Sources/WorkoutsCore/Suggest/`, `Tests/WorkoutsCoreTests/SuggestVectorTests.swift`                                                                                                                                                                              | §5.1 and §5.3's rules inside `suggest`; §10.2                      |
| C        | `Sources/WorkoutsCore/{Engine,Rest,Present,Format}/`, `Tests/WorkoutsCoreTests/{EngineTests,RestRuleTests,UpNextTests,ChainTests,PresenterTests,FormatTests,ExampleProgressionTests}.swift`                                                                      | §4, §5.2, §5.4 to §5.6; §10.1's engine rows, §10.3, §10.5          |
| D        | `the app/Views/`, `the app/Preview Content/PreviewData.swift`, `the app/SubstrateWorkoutsApp.swift`                                                                                                                                                                      | §7, §8 (except the Live Activity)                                  |
| E        | `project.yml`, `Support/`, `.gitignore`, `the app/Platform/`, `the app/Shared/`, `SubstrateWorkoutsWidgets/`                                                                                                                                                             | §6.2, §9.1 to §9.8, §9.10                                          |
| C and D  | `the app/App/` (`SessionController`, `AppModel`, `DeepLinks`)                                                                                                                                                                                                  | §6.1, §6.3, §6.4, §5.7; C writes it, D reviews and wires the views |

**Order:**

1. **Day 0, foundations (A and E, in parallel, small).** A: `Package.swift` with resources, empty module folders that
   build, `examples.py` and its generated files. E: `project.yml` with the widget target, entitlements and Info keys, a
   stub widget bundle and an empty `Shared/`, building on the simulator. Everyone else starts after these land.
2. **Day 1, the model types (A).** A lands `Model/` (the §3.3 structs and value types, no codec yet) first, so C and D
   code against the real types. B needs none of it: its types are 03's own (§5.1).
3. **Parallel build (A, B, C, D, E).**
   - A: codec, journal, history store, seams, bundled catalogue; codec tests green.
   - B: `suggest` and its blocks; V1 to V19 green. B publishes the API early as a stub returning nil.
   - C: engine, rest rules, Up next, chains, labels, presenter on the stub suggester; engine tests green.
   - D: every screen on `PreviewData` (presentations built from the preview fixtures through C's presenter as soon as
     it exists, hand-built before).
   - E: countdown coordinator, notifications, Live Activity views and intents (against a hand-built
     `ContentState`), HealthKit exporter and queue, body mass reader, keep-awake, haptics.
4. **Integration.** C: `SessionController` and `SuggestionInputs` with B's real `suggest`; `ExampleProgressionTests`
   green. D: views bound to the controller. E: coordinator wired to the controller; Health export at Finish wired to
   A's history store.
5. **Device pass (E, then everyone).** §9.10's list on the owner's iPhone; fix what it finds.
6. **Release.** `mise run apple:release:workouts` after a dry run; the app is already listed in
   the internal app list and wired to `SubstrateKit`.

## 12. Questions that still touch V1

These stay open with the owner (research README numbering); V1 ships with the decision in brackets and changes in one
place if the answer differs:

- **1** RPE keypad range [6 to 10, half steps, 6.5 included]: `RPESheet`.
- **6** Warm-ups in volume [no, a setting]: `Settings`.
- **7** Superset rest defaults [§4.7]: `RestDefaults`.
- **8** Rest after an exercise's last set [yes, a setting]: `Settings`.
- **9** Timed sets complete themselves at zero [no]: `SessionEngine`.
- **10** Phone on silent at the gym, AlarmKit [V1.1]: the coordinator's hook.
- **15** Default rep range and RPE [8 to 12 at RPE 8, per exercise in data]: `ExerciseConfig` defaults.
- **21** Gyms and setups [none in V1]: `SuggestionInputs`, `Previous`.
- **22** Body mass source [Health, else typed]: `BodyMassReader`.
- **24** Routine name as the Health brand name [no]: `HealthExporter`.
- **32** Discard leaves no trace; skipped planned sets are not recorded [as decided]: the engine's Finish rules.
