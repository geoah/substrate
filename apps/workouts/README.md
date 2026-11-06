# Substrate Workouts

An iPhone app (iOS 27) for strength sessions in the gym, writing them into a
substrate as records of a training package the app keeps there. It lists the
workout routines, starts a session from one or from nothing, and runs it:
exercises in any order, supersets and warm-up circuits, warm-up, drop and
myo-rep sets, timed sets, reps left and why a set ended, rest timers that
notify when rest ends, a suggested load and rep count for the next set from
this session and earlier ones, an offer of the next load once two sessions
reached the top of the range, and the time the session should end. Each finished
session is also saved to Apple Health as one strength workout. It came to
this repository from an app called Spotter; its spec is
[DESIGN.md](DESIGN.md), and its package is [Package/](package/README.md).
The research, design options, reviews and decisions behind it are in
[docs/](docs/README.md).

The app has five tabs. **Routines** lists the routines by folder, opens
one and starts it (or an empty session). **History** lists finished sessions,
newest first. **Stats** shows progress across every routine, or one. **Exercises** lists every exercise with its history and its
estimated 1RM trend. **Settings** holds units, rest defaults, the session
switches, alerts, Apple Health, the substrate, imports and the update check. A session
runs in a full-screen cover whose default layout, **Rows**, shows each set as
a line, opens the next set in place with steppers, the slot's cue and the
planned reps left filled in (any other set opens with a tap, in any order),
and keeps an island at the
bottom with the rest countdown, the next set once its editor is scrolled away,
and the set after it; **Table** is the grid. The session's Live Activity
(`SubstrateWorkoutsWidgets`) shows the rest on the Lock Screen and in the
Dynamic Island.

```bash
mise run apple:build:workouts     # the app for the simulator
mise run apple:test:workouts      # WorkoutsKit: the model, both engines, the sync, on the Mac
mise run apps:release prerelease workouts   # archived, signed and published under workouts/
.mise/python.sh apps/workouts/scripts/workouts-package.py   # the package's manifests into the app's JSON
.mise/python.sh apps/workouts/scripts/exercise-library.py   # the exercise library into the app's JSON
```

**A routine's page** opens on its folder, notes and what the plan holds, then Plan or
Progress. The plan is the routine as a started session draws it, read only: a card per
exercise or group, each planned set a line with its label, its targets (load, reps or hold,
the reps left), its tags and last time, groups by round, with no glyph to tick and nothing that
answers a tap but an exercise's name, which opens its history. On the signed-in substrate,
Edit, and only Edit, edits the routine in place: the lines take the session's glyphs, a tap
opens a set as a session's panel with steppers, reps left among them, and its rest, each line's and
card's `…` hold the rest, and Save queues one guarded write as before. Progress, and the **Stats** tab for every
routine or one, show the period's sessions and rate, volume, working sets and time per
session, the weeks trained, volume over time, the last session against the one before, each
exercise's best with its trend, personal records, a body map (`BodyMap`) with push/pull and
upper/lower balance, and the sessions, worked out off the main thread and kept until the
history or the catalogue changes. `StatsPresenter` and `RoutinePlanPresenter` work it all out
(`StatsPresenterTests`, `RoutinePlanPresenterTests`); the screenshots are
`docs/screenshots/routine-*.jpg` and `stats-*.jpg`. Every note, a routine's, an
exercise's, a set's or a session's, reads as Markdown (`NotesMarkdown`, `NotesText`) and folds
past five lines behind Show more. The body map's figures are
react-native-body-highlighter's (MIT), written into `Views/Stats/BodyMapShapes.swift` by
`apps/workouts/scripts/body-map.py` at a pinned commit, with its notice.

## Layout

| Path | What |
| --- | --- |
| `apple/Sources/` | the app: `App/` (`WorkoutsRoot`, the composition root: the kit's link with `WorkoutsApp` under it on one runtime, and the device services; `SessionModel`, `AppModel` and `SubstrateLink`, which republish the runtime's presentations for the views and send their commands), `Platform/` (the session screen's presence, the Live Activity's attributes, HealthKit, haptics; the notification delegate is the kit's), `Shared/` (compiled into the widget too), `Views/` |
| `package/` | the training package's manifests and their long form |
| `apple/Widgets/` | the Live Activity extension |
| `WorkoutsKit/Sources/WorkoutsCore` | Foundation and the kit's SubstrateCore, SubstrateRuntime and SubstrateLocal: the model and its record codec, the session engine, the session's controller on the kit's runtime (its journal the kit's `JournalFile`), the rest rules, the suggestion engine, the presenters, the files on the phone, the sample routine and the package's manifests |
| `WorkoutsKit/Sources/WorkoutsSync` | the app's side of a substrate, over the kit's portable libraries: `WorkoutsApp`, the app's root under the kit's `LinkController` (the catalogue, History and the settings, the sync run, the routine edits, the session as its child), the catalogue, the outbox (the kit's, with the routine helpers), the server's history, the manifest |

## Signing in and sync

Without a server the app runs on the one routine it carries, "Sample: every
feature", and keeps every session on the phone, so it can be tried before
anyone signs in. The sample holds every measurement, set structure and way a
set ends the app runs, each exercise with a note saying what it shows
([`docs/examples/sample.yaml`](docs/examples/sample.yaml), decision
[S1](docs/decisions.md#s1-one-sample-routine-that-shows-every-feature-written-once-and-deletable)).

**Signing in** is the substrate's app authorization, through SubstrateKit
under `WorkoutsManifest`: the connect screen probes the address, the system
sheet opens the server's own sign-in and approval page, and the code comes
back to a loopback port and is exchanged with its PKCE verifier. The request
asks for:

| Asks | Why |
| --- | --- |
| `package` details `substrate.reamde.dev/health` (1+), `substrate.reamde.dev/training` and `substrate.reamde.dev/trainingcoach` (each at the version this build was written against, or later) | the catalogue the package requires, the training package, and the workout coach's package with the agent in its closure; the server installs the published two from the authority document (decision record 0214) |
| `write:substrate.reamde.dev/training/*` | to read and write its exercises, routines, sessions and sets |
| `read:substrate.reamde.dev/health/workout`, `read:…/applehealth/account` | to find the Health workout a session was saved as |
| `read:substrate.reamde.dev/health/sleep`, `read:…/observations/*`, `read:…/clinical/*` | what the coach's agent reads beyond training: a package's code reaches no further than the approved scopes (decision record 0215), so the app asks for the coach's reach and `CoachTests` holds the list to the closure |
| `agents:substrate.reamde.dev/trainingcoach/*` | to talk to the coach |

The request carries the build's version (`software_version`), which the
server records with the floors; an upgrade that would break a connected
build waits. A server whose metadata lists no `package` detail or no
`agents` verb is too old, and says so. A release stamps its client id
(`SubstrateWorkoutsClientID`, the `workouts/client.json` the site
publishes); a local build signs in as `http://127.0.0.1/substrate-workouts`.
The session is kept in the Keychain under `dev.reamde.substrate.workouts`,
and Sign Out revokes it as the kit does for every app.

**The package is published.** `substrate.reamde.dev/training` and
`substrate.reamde.dev/trainingcoach` are the same name in every repository,
served from the site's authority document with each version's closure
(`mise run apps:site`, [Package/](package/README.md)). The app applies
nothing and respells nothing: the kinds are `substrate.reamde.dev/training/…`
on the phone and on the wire. A phone updated from a build that spelled them
`~/training/…` (decision [T3](docs/decisions.md#t3-the-package-is-the-apps-own-training))
still holds that spelling in its files: each store reads a file from before
its format moved through `LegacyKinds` once, every kind and reference path,
and writes it back in the published spelling (`history/*.json` at format 3,
`outbox.json`, `catalog.json`, `local-exercises.json` and
`routine-draft.json` at format 2), so nothing is parked on a path the
server never had and nothing is set aside. What the old build knew about
`~/training` says nothing about the published package, which may be empty
or hold the copies the owner made, so the outbox carries none of it over:
the old package's record versions and edit chains go, a queued write of it
goes without its `ifVersion`, and a session's queued writes go altogether,
since the first sync's exchange derives them again from the history and the
tombstones. A session the published package lacks is uploaded whole, one it
holds is taken as the server has it and never sent again, and nothing is
deleted on either side; a routine edit queued before the update lands on
the copy, or waits parked with the server's words where there is none. The owner cannot edit a published package
(the tier's ordinary rule), keeps the bundle's **Pause** and **Resume**, and
may **Take over…** it, after which it stops upgrading and the app keeps
writing records to it. A local build and a release build ask for the same
packages, so they share a repository without trouble.

**Where the repository stands.** At sign-in and at launch the kit's link
reads the training package's row, and again after each set up check, which
carries the build's floor
(`substrate.reamde.dev/core/package/substrate.reamde.dev%2Ftraining`, whose
`shippedVersion` is the publisher's number):

| The repository holds | The app |
| --- | --- |
| none, or a row the token cannot see | reads it as missing; the kit's status check says what the approval installs |
| an older version than this build was written against | writes nothing, asks the server through the set up check (which carries the build's floor; a kinds-only upgrade is applied at once, one that changes code waits for the owner's review on the server), reads the row again, and says "Your substrate has version 4 of the Training package and this app needs version 5 or later" until it lands |
| this version, or a later one | writes; a later version is additive, so this build ignores what it does not know |

A change to the package ships as the next version and is additive only: the
server refuses a narrowing while live records hold the old shape, and a
published version never changes (`scripts/workouts-package.py --check`
refuses a changed closure under an unmoved version, and the site refuses to
publish one). An older build keeps writing validly against a newer version.

**Routines** come from the server once the package is in place, and are
cached for the gym (`catalog.json` in the repository's folder under
Application Support). The first sync on a repository writes the sample
routine, its exercises first, when the repository holds no routine and none of
the sample's exercises. The server is the marker: deleting a routine keeps its
exercises, so a second phone, or this one reinstalled, finds them and writes
nothing. A repository that already has routines gets no sample. `sample.json`
in the repository's folder keeps what this phone settled, and a write it
started, which it finishes at the next sync. A sample write that fails is
shown on the empty Routines screen and tried again at the next sync; it never
stops the history exchange or the outbox. An empty repository says routines
come from the agent, and offers **Add the sample routine** only when the sample
was written there and then deleted; like every write, only into a package
checked at this build's version, and the button says why when it is not.

**A routine can be deleted** from the app: a swipe or the menu in Routines,
or Delete on its preview, after a confirmation, for any routine on the
signed-in substrate while the app may write its package (when it may not,
Delete is not offered and the Routines banner says why). It leaves the cached
catalogue at once and is purged on the server through the outbox
(`DELETE ?purge=true`); a sync that reads the catalogue before the delete
lands, or while it is being made, leaves it out. The exercises it named stay, since past sessions
name them. Signed out there is nothing to delete the sample from, so the
app offers no delete there.

**A routine can be edited** from Edit on its preview, and duplicated from
its menu: name, folder, notes, the order of exercises, groups and sets,
grouping, exercises and sets added and removed, and each set's targets and
rests ([E1](docs/decisions.md#e1-routines-are-edited-on-the-phone-save-and-cancel-a-draft-on-disk-a-conflict-is-the-owners)).
Save queues one guarded `PATCH` of what changed in the outbox; the draft is
kept in `routine-draft.json` on every change, so a killed app resumes it. A
`409` is parked for the owner (**Keep My Edit** or **Use Theirs**), never
merged over. Signed out the sample cannot be edited.

**Each sign-in is one account**: its own folder, outbox, catalogue and
package standing. The kit's link (`LinkController`) owns the connection: the
session and its revocations, the set up check, the packages' versions,
connectivity and the outbox's flush, which follows every write the app
queues. Workouts' root under it (`WorkoutsApp`) runs the sync from the
link's snapshot, each step named for the account it ran for: signing out or
signing in elsewhere stops the run under its key, and a step for the old
account that still arrives changes nothing.

**A finished session goes through the outbox** (`outbox.json`), kept on the
phone until the server takes it, in the package's order, after any exercise it
names that the repository lacks (a session logged signed out on the sample,
for a repository that never got it):

1. the session `PUT` at its UUIDv7, without `status`, so a replay never moves
   a finished session back;
2. each set `PUT`, a chain's head before its drops or mini-sets
   (`set.session` and `set.continues` are `mustExist`);
3. Finish as a `PATCH` under `ifVersion` (the version the create answered)
   with `status: finished`, the end, the session RPE and the plan snapshot,
   raising `revision`;
4. the Health link, `healthKitWorkout` under `ifVersion`, a revision higher.

A `409` reads the record and sends the patch again on top. A `422` naming a
revision below the stored one means a newer write landed: the entry is
dropped. No network, a busy server or a refused token stop the run with
nothing lost; any other refusal (a `404` for a session purged on the server,
a validation error) parks the write with the server's words, holds back the
rest of its session, and lists it in Settings with Retry and Discard. Each
entry is found by its id after every network call, so a delete, a discard or
a sign-out while a write is in flight never removes the wrong one; a purge
asked for while the session's create is in flight goes after it. A change
the outbox cannot keep on disk is refused and said; an unreadable
`outbox.json` is set aside and listed, never replaced by an empty queue.

**History** is the phone's and the server's (`HistoryExchange`). At each sync
the app reads every session and set the repository holds and decides from
the outbox's record versions, which say what this phone sent to or read from
that repository, and that repository's tombstones (`deleted-sessions.json`
in its folder, every session deleted on the phone while signed in to it).
The app works with one server at a time (decision O3), so a delete is purged
on the server and repository it belongs to and nowhere else. A session
deleted while signed out goes in the app's own `deleted-sessions.json`, which
only keeps it from being imported again:

| The server | The phone | The app |
| --- | --- | --- |
| holds a finished session | lacks it, never deleted it | imports it |
| holds a session | deleted it | purges the server's copy, never imports it |
| changed a session (a record at a version the phone did not see, a set added or removed) | holds it, nothing waiting | takes the server's records, keeping its own Health state |
| holds a session unfinished | finished it | sends Finish again above the server's revision |
| lacks a session | sent or read it before | deletes it from the phone: the owner deleted it |
| lacks a session | never sent it | sends it |

A session with a write still waiting is the phone's until the write lands. Nothing is
ever written to `substrate.reamde.dev/health/workout`; the Apple Health save
works as it always did.

## The exercise library

The app seeds an exercise library into the repository's training package:
606 movements (`substrate.reamde.dev/training/movement`: Chest Press, with the muscles it works
and how much) and 807 exercises, one per equipment version (Chest Press
(Machine), Chest Press (Cable), Chest Press (Band)), each with its
measurement, equipment and load conventions (two dumbbells count per
dumbbell, a plate-loaded lever per side). The coach and the console read
them like any record. `apps/workouts/scripts/exercise-library.py` builds it
(`WorkoutsCore/Resources/Bundled/library.json`) from two sources in
[`Library/`](library/):

| Source | What |
| --- | --- |
| `movements.yaml` | curated: 305 movements with muscle shares across 34 muscles, and their exercises, each named as Hevy names it and linked to the free-exercise-db entries that are the same exercise and to the Hevy template ids that map onto it (all 451 of Hevy's built-in exercises) |
| `free-exercise-db.json` | [free-exercise-db](https://github.com/yuhonas/free-exercise-db)'s 678 strength entries (The Unlicense) at a pinned commit; an entry no curated exercise claims is a movement and exercise of its own, with its muscle groups and no shares |
| `library.lock.json` | the library's version, derived from the records' hash, and the version each record first appeared in |

**Seeding** (`LibraryInstall`) runs at the end of each sync, after the
sessions are sent. The repository's `substrate.reamde.dev/training/library/exercises` record is
the marker: a run names the version it writes there before its first write,
and sets the version it holds whole only after a run that left nothing
behind. A run writes the records that first appeared after that version
(each carries the code `substrate-workouts-library` with it), each as a
create that never merges into a record someone wrote meanwhile, so a record
the owner deleted never comes back and a run cut short is finished, on any
phone. A record the repository already holds is the owner's: it is enriched
once, only when it is plainly the library's exercise (its name, an alias or
its Hevy id), and only with its movement, muscles and codes, never a field
that changes how its sets count or run; an exercise's own group lists win
over its movement's muscles when they differ. **Exercises** lists the owner's own (done,
in a routine, or not from the library) until a search, which covers all of
them.

## Importing from Hevy

Settings → Import → Hevy reads a Hevy account in one of two ways, and
`HevyImport` turns what it reads into the package's records as the package
README's [Importing](package/README.md#importing) section asks; nothing is
saved until the owner has seen what it holds and taps Import.

- **An API key** (Hevy Pro: hevy.com → Settings → Developer). `HevyClient`
  reads the account's workouts, routines and routine folders, body
  measurements, and every exercise the account made, used or not; read
  only, and the key is not kept.
- **The workout export** (`workout_data.csv`, from Hevy's Profile → Settings
  → Export & Import Data → Export Data → Export Workouts), chosen in Files and
  read by `HevyCSV`. It holds workouts only, by title, with wall-clock times
  the owner places in a time zone on the preview.

| Hevy holds | The import writes |
| --- | --- |
| a workout | one finished session under an id derived from `hevy-<workout id>` (the API) or `hevy-<yyyyMMdd>-<HHmm>` from its start on the wall (the CSV), so an import run again finds it, as it finds a session an earlier build wrote at that key itself; its `adapter` says which path it came by (`substrate-workouts-hevy-api@…`); the API's instants kept as they are; a workout imported by the other path, at the same time, is skipped as imported; an empty workout too |
| its exercises and sets | slots and set records: every value Hevy keeps (load, reps, distance, time, RPE, warm-up, failure, drop chains, supersets, notes); an exercise with no sets keeps its slot and notes; a stair machine's floors or steps go in the set's notes |
| an exercise | the catalogue's exercise with that Hevy id, else the one of that name that names no other Hevy id, else the library's (movement, muscles and conventions included), else one made from the account's own template, its Hevy id in `codes`; two templates are two exercises |
| a routine | a workout routine under an id derived from `hevy-<routine id>` (folder, order, supersets, sets, rep ranges, rests, a rest of none, drops); one the repository holds, at that id or at `hevy-<routine id>` itself, is left as it is; each session names the routine it was started from |
| body measurements | one `substrate.reamde.dev/training/bodymeasurement` per entry, under an id derived from `hevy-<entry id>`, or at that key where an earlier build wrote it: body mass, lean mass, body fat and the fourteen circumferences; a session takes the latest body mass on or before its day |
| a pair of dumbbells or kettlebells logged together | halved onto one implement, when the owner says so on the preview |

A session deleted from History, here or on another device, is never
imported again. Imported sessions join History, PREVIOUS and the
suggestions, are never saved to Apple Health (Hevy may have saved its own),
and go to the substrate through the history exchange like any session the
server never held. Routines and body measurements need a substrate; signed
out, only the workouts come in.

**Not imported:** the order of Hevy's routine folders and an empty folder
(a routine keeps its folder's name and its place in it), exercise thumbnails,
account preferences and social content. Body measurements live in the
training package rather than as `substrate.reamde.dev/observations`, since
lean mass and circumferences have no metric there yet and writing
observations would ask every user for a new scope.

## Tests

`mise run apple:test:workouts` checks the generated JSON against its YAML,
then runs both suites on the Mac (CI runs the check in the `lint` job, and
both suites on Linux in the `apps linux` job; no workflow builds the Apple
app): WorkoutsCore's (the codec, the engine,
the suggestions against their vectors, the presenters, the stores, the sample
routine's methods and a session through it to Finish, and the Hevy import
against `Tests/WorkoutsCoreTests/Fixtures/hevy`, the API's shape included, and the exercise library, and the files the last release left in the `~/training`
spelling read back once, and the session's controller on the kit's runtime: its journal
order, the Finish that is two events and a crash on either side of its history file,
the open, the ticks, the entry check and a refusal) and WorkoutsSync's:
the sample written once and never again after a delete, on a second phone or a
reinstall, a refused sample write that stops nothing, a session logged signed
out finding its exercises, a routine's delete through the outbox and during a
read, the sample added back while its delete lands, the empty Routines
screen's offer, the codec against answers
recorded from a server built from this tree (`Tests/WorkoutsSyncTests/Fixtures`),
the outbox's order and its answers to `409`, `422`, `401` and the network, a
delete, a discard, a reset and a new write while a write is in flight, a
failed save and an unreadable file, parked writes, the history exchange, the
package check (missing, behind and asked through the set up check, current),
the published manifests, the coach's closure and the scopes held to its reach
(`CoachTests`), an account folder from the `~/training` spelling read and rewritten, its
history exchanged with an empty published package and with one holding the
owner's copies, the sign-in request and status, and the app's
root under the link (`WorkoutsAppTests`: what the link's snapshot starts, a
sync step for another account, a finished session reaching the account's
outbox as one batch, a delete's tombstone).

`LiveServerTests` runs the whole path against a real substrate when told
where (one that reads the publisher's authority document, live or through
`SUBSTRATE_INSECURE_AUTHORITY_DOCS`): the authorization request, approved
through the approval API (which installs health, training and the coach),
the package read as current, the sample
routine written on the first run, deleted through the outbox and not written
again by this phone, a second one or a reinstall, added back, a session logged
signed out whose exercises the repository lacks taken by the server's
`mustExist`, a
finished session and its 20 sets written through the outbox, and all of it
read back as the owner.

```bash
WORKOUTS_E2E_SERVER=http://localhost:8080 WORKOUTS_E2E_OWNER_TOKEN=… WORKOUTS_E2E_REPOSITORY=<fresh repository> \
  swift test --package-path apps/workouts/WorkoutsKit --filter LiveServer
```

`LiveHevyImportTests` seeds the exercise library over an owner's own
exercise before reading an account through Hevy's API, and holds that
nothing the owner wrote changes. It seeds the exercise library and sends a
Hevy export (`WORKOUTS_HEVY_CSV`) the same way, into a repository holding
this build's training package: every session, set and new exercise
through the history exchange and the outbox, read back as the import wrote
them, with nothing left for the phone to send; with `WORKOUTS_HEVY_API_KEY` it reads the
account through Hevy's API instead, routines included. `WORKOUTS_HEVY_CSV` alone runs
the export through `HevyImportTests` without a server.

## Screenshots

Taken on an iPhone 18 Pro Max simulator (iOS 27) on 2026-11-01, signed in to a
throwaway substrate built from this branch (`localhost:18087`, a fresh
repository with neither health nor training installed). The approval set up
Health, the app then installed its Training package, copied the starter
routines, and the finished session and its sets were read back from the server.
They are `docs/screenshots/signed-*.jpg`.

| Server | Approval | Empty server | Routines from the server |
| --- | --- | --- | --- |
| ![Server](docs/screenshots/signed-01-sign-in.jpg) | ![Approval](docs/screenshots/signed-02-approve.jpg) | ![Empty server](docs/screenshots/signed-03-empty-server.jpg) | ![Routines](docs/screenshots/signed-04-routines-from-server.jpg) |

| Set to do now | Resting | Superset | Finish |
| --- | --- | --- | --- |
| ![Set to do now](docs/screenshots/signed-05-rows-set-open.jpg) | ![Resting](docs/screenshots/signed-06-resting.jpg) | ![Superset](docs/screenshots/signed-07-superset.jpg) | ![Finish](docs/screenshots/signed-08-finish.jpg) |

| History | Substrate in Settings |
| --- | --- |
| ![History](docs/screenshots/signed-09-history-session.jpg) | ![Settings](docs/screenshots/signed-10-settings-substrate.jpg) |

The session screen after the owner's first review (2026-11-02, iPhone 18 Pro
Max simulator): sets logged in any order, nothing timed but a timed set, and
the island's three parts, the rest, the next set (docked once its editor is
scrolled away) and the one after it. They are `docs/screenshots/session-after-*.jpg`;
`session-before-*.jpg` are the same moments before.

| Start | Resting | Any order | Three parts |
| --- | --- | --- | --- |
| ![Start](docs/screenshots/session-after-start.jpg) | ![Resting](docs/screenshots/session-after-resting.jpg) | ![Any order](docs/screenshots/session-after-any-order.jpg) | ![Island](docs/screenshots/session-after-island.jpg) |

| Docked next set | Superset | Plank | Minimised |
| --- | --- | --- | --- |
| ![Docked](docs/screenshots/session-after-docked.jpg) | ![Superset](docs/screenshots/session-after-superset.jpg) | ![Plank](docs/screenshots/session-after-plank.jpg) | ![Minimised](docs/screenshots/session-after-minimised.jpg) |

After the second review and the owner's feedback on the pre-release
(2026-11-02, iPhone 17 simulator; "Three parts" and "Docked next set" above
are retaken there too): a done set's `…` with Untick set and Delete set, the
RPE in the island with its stops, a fix with Save and Cancel only, "Then"
taking the list to its set, the labelled RPE stops, and at the largest text
size the island docking only the name and Log, or Stop and its clock (Stop is
measured alone, so a visible Stop above Cancel docks nothing).

| A done set's `…` | RPE in the island | Fixing | "Then" shows the set |
| --- | --- | --- | --- |
| ![Done set menu](docs/screenshots/session-after-done-menu.jpg) | ![Island RPE](docs/screenshots/session-after-island-rpe.jpg) | ![Fix](docs/screenshots/session-after-fix.jpg) | ![Then](docs/screenshots/session-after-then.jpg) |

The docked set keeps its − and + beside the RPE (owner, 2026-11-02): the
values, the RPE and Log share one row where they fit: a load and reps on Plus
and Pro Max phones, one value or a timed set on every phone. On a 402 pt phone
and narrower a load and reps take two rows, the RPE and Log under the values;
larger text puts each value on a line of its own, steppers kept.

| Two values: two rows | One value: one row | Timed set | Larger text |
| --- | --- | --- | --- |
| ![Docked](docs/screenshots/session-after-docked.jpg) | ![One row](docs/screenshots/session-after-island-one-row.jpg) | ![Timed](docs/screenshots/session-after-island-timed.jpg) | ![Larger text](docs/screenshots/session-after-island-large-text.jpg) |

| RPE, labelled | Largest text, docked | Largest text, timed | Largest text, Stop in view |
| --- | --- | --- | --- |
| ![RPE label](docs/screenshots/session-after-rpe-label.jpg) | ![AX docked](docs/screenshots/session-after-accessibility-size.jpg) | ![AX timed](docs/screenshots/session-after-accessibility-timed.jpg) | ![AX Stop](docs/screenshots/session-after-accessibility-stop.jpg) |

The open set's values after the owner's feedback from the gym (2026-11-06,
iPhone 17e simulator, the narrowest phone): the open set named after its
exercise, flush with the panel's edge, and load, reps and reps left built the
same, each with its last time under the number and, beside its steps, the
reset and (on reps left) why the set ended as round buttons; the keypad sets
the field as each key is typed, so Log never takes the old value, with Done
and Cancel; every other set on one line, with no round headers; every menu's
button on Liquid Glass, so its menu grows out of it, the open set's `…` beside
Log; "Next", on one line, for the set after; and the session's name as a large
title over its length, folding into the bar as the list scrolls. They are
`docs/screenshots/rows-input-*.jpg`.

| The open set | Typing 12.75 | Why the set ended | A superset | The top of the list |
| --- | --- | --- | --- | --- |
| ![Open set](docs/screenshots/rows-input-01-set-open.jpg) | ![Keypad](docs/screenshots/rows-input-02-keypad.jpg) | ![Why stop](docs/screenshots/rows-input-03-why-stop.jpg) | ![Superset](docs/screenshots/rows-input-04-superset.jpg) | ![Header](docs/screenshots/rows-input-05-header.jpg) |

A routine's page and its progress after the owner's review of them
(2026-11-03, iPhone 18 Pro simulator, signed in to a throwaway substrate,
with the previews' two sessions of the sample as History: a Debug build
launched with `-WorkoutsSeedPreviewHistory` writes them into an empty
History). Read only, the plan has no glyph to tick and answers no tap but an
exercise's name, so Edit is plainly the way in, and editing looks like it;
a wrapped line's second row starts under the text after its label, with last
time quieter than the target; and the sets per muscle group are rows of
their own, the name beside its bar. They are
`docs/screenshots/review-after-*.jpg`; `review-before-*.jpg` are the same
moments before.

| Read only | A superset, read only | Editing | Muscles |
| --- | --- | --- | --- |
| ![Read only](docs/screenshots/review-after-plan-top.jpg) | ![Superset](docs/screenshots/review-after-plan-superset.jpg) | ![Editing](docs/screenshots/review-after-plan-editing.jpg) | ![Muscles](docs/screenshots/review-after-progress-muscles.jpg) |
| ![Read only, before](docs/screenshots/review-before-plan-top.jpg) | ![Superset, before](docs/screenshots/review-before-plan-superset.jpg) | ![Editing, before](docs/screenshots/review-before-plan-editing.jpg) | ![Muscles, before](docs/screenshots/review-before-progress-muscles.jpg) |

`docs/screenshots/rows-*.jpg` are the Rows layout from the app's build in
the owner's private repository (Spotter, its #118), with example sessions loaded as history: drops, a past set being
fixed, a timed plank and dark mode.
