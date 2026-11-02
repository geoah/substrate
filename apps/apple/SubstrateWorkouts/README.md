# Substrate Workouts

An iPhone app (iOS 27) for strength sessions in the gym, writing them into a
substrate as records of a training package the app keeps there. It lists the
workout routines, starts a session from one or from nothing, and runs it:
exercises in any order, supersets, warm-up, drop and myo-rep sets, timed sets,
RPE, rest timers that notify when rest ends, and a suggested load and rep
count for the next set from this session and earlier ones. Each finished
session is also saved to Apple Health as one strength workout. It came to
this repository from an app called Spotter; its spec is
[DESIGN.md](DESIGN.md), and its package is [Package/](Package/README.md).
The research, design options, reviews and decisions behind it are in
[docs/](docs/README.md).

The app has four tabs. **Routines** lists the routines by folder, previews
one and starts it (or an empty session). **History** lists finished sessions,
newest first. **Exercises** lists every exercise with its history and its
estimated 1RM trend. **Settings** holds units, rest defaults, the session
switches, alerts, Apple Health, the substrate and the update check. A session
runs in a full-screen cover whose default layout, **Rows**, shows each set as
a line, opens the next set in place with steppers and the planned RPE filled
in (any other set opens with a tap, in any order), and keeps an island at the
bottom with the rest countdown, the next set once its editor is scrolled away,
and the set after it; **Table** is the grid. The session's Live Activity
(`SubstrateWorkoutsWidgets`) shows the rest on the Lock Screen and in the
Dynamic Island.

```bash
mise run apple:build:workouts     # the app for the simulator
mise run apple:test:workouts      # WorkoutsKit: the model, both engines, the sync (405 tests, on the Mac)
mise run apple:release:workouts   # archived, signed and published under workouts/
uv run --with pyyaml apps/apple/scripts/workouts-package.py   # the package's manifests into the app's JSON
```

## Layout

| Path | What |
| --- | --- |
| `SubstrateWorkouts/Sources/` | the app: `App/` (the session controller, the model, `SubstrateLink`), `Platform/` (notifications, the Live Activity, HealthKit, haptics), `Shared/` (compiled into the widget too), `Views/` |
| `SubstrateWorkouts/Package/` | the training package's manifests and their long form |
| `SubstrateWorkoutsWidgets/` | the Live Activity extension |
| `WorkoutsKit/Sources/WorkoutsCore` | Foundation only: the model and its record codec, the session engine, the rest rules, the suggestion engine, the presenters, the files on the phone, the sample routine and the package's manifests |
| `WorkoutsKit/Sources/WorkoutsSync` | the app's side of a substrate, over SubstrateKit's client: the package installer, the catalogue, the outbox, the server's history, the manifest |

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
| `declare:~/training` | to install and upgrade the app's own package, which the app then owns ([0203](../../../docs/decisions/0203-an-app-keeps-its-own-package-under-a-declare-scope.md), [0205](../../../docs/decisions/0205-an-app-owns-the-package-it-declares.md)) |
| `write:~/training/*` | to read and write its exercises, routines, sessions and sets |
| `read:substrate.reamde.dev/health/workout`, `read:…/applehealth/account` | to find the Health workout a session was saved as |
| `package` detail `substrate.reamde.dev/health` | the catalogue the package requires, installed on approval |

A server whose metadata lists no `declare` verb is too old, and says so. A
release stamps its client id (`SubstrateWorkoutsClientID`, the
`workouts/client.json` the release publishes); a local build signs in as
`http://127.0.0.1/substrate-workouts`. The session is kept in the Keychain
under `dev.reamde.substrate.workouts`, and Sign Out revokes it as the kit
does for every app.

**A local build is another app.** The server owns the package by the full
client id, so the local build and a release build are two apps: whichever
declares `~/training` in a repository first owns it, and the other is
refused, at the approval page and at its apply. Point a local build at a
separate repository (a throwaway one on `mise run dev` is the usual), or take
the package over in the repository you share and let neither build update it.

**The package is the app's.** At sign-in, at launch and on return to the
foreground, `PackageInstaller` reads
`substrate.reamde.dev/core/package/<authority>/training`:

| The repository holds | The app |
| --- | --- |
| none | applies its manifests, `~` written as the repository's authority |
| an older version, kinds as the app left them | applies them again; the server lands the change at the next version |
| any version it knows, a kind changed outside the app | writes nothing there, keeps sessions on the phone, and says "The Training package on <server> was changed outside Substrate Workouts; it is managed by this app." |
| this version | does nothing |
| a newer version | writes nothing more, keeps sessions on the phone, and says to update the app |
| a package the owner took over, or another app's | never applies to it, keeps sending sessions, and says "Your Training package is managed by you now (taken over); this app no longer updates it", or "…by another app" |

**The server enforces it** (decision
[0205](../../../docs/decisions/0205-an-app-owns-the-package-it-declares.md)):
the package's kinds change only through this app, by its client id. The
owner can't edit them in the console, through `substratectl` or the API, and
there is nothing to override. The owner's ways in are the bundle's
**Pause** and **Resume**, which stay theirs, and **Take over…** on the
package's page, which makes the package theirs for good (there is no
handing it back). After a takeover the server answers the app's apply with
`403`: the app reads the package row (`takenOverFrom`, `declaredBy`,
`declaredByClient`) before every apply, so it stops applying from the first
check on, never in a loop, and keeps flushing the outbox, since the server
still takes the app's record writes. A `403` on an apply it did send, in the
moment between its read and a takeover, ends the same way.

A change to the package ships as the next version and is additive only. The
server refuses the app's token an apply that would remove anything the
package holds, and a lossy change; the app shows that refusal in the
server's words and writes nothing until it is settled. After each apply the
app still records every kind row's version (`package.json` in the
repository's folder) and compares them at every check: with the owner's
edits refused, a moved kind means a package that predates the app's install,
and the app writes nothing there and never applies over it. On the phone the
kinds are spelled
`~/training/…`; `Rehome` swaps `~` for the repository's authority at the
server's edge, both ways, and nowhere else.

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
package check. Signing out or signing in elsewhere replaces it, and a sync
still running for the old one stops at its next step without publishing or
writing anything.

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

## Tests

`mise run apple:test:workouts` checks the generated JSON against its YAML,
then runs both suites on the Mac (CI runs the check, in the `lint` job, and
not the suites: no workflow builds the Apple apps): WorkoutsCore's 392 (the codec, the engine,
the suggestions against their vectors, the presenters, the stores, the sample
routine's methods and a session through it to Finish) and WorkoutsSync's 58:
the sample written once and never again after a delete, on a second phone or a
reinstall, a refused sample write that stops nothing, a session logged signed
out finding its exercises, a routine's delete through the outbox and during a
read, the sample added back while its delete lands, the empty Routines
screen's offer, the codec against answers
recorded from a server built from this tree (`Tests/WorkoutsSyncTests/Fixtures`),
the outbox's order and its answers to `409`, `422`, `401` and the network, a
delete, a discard, a reset and a new write while a write is in flight, a
failed save and an unreadable file, parked writes, the history exchange, the
package installer and an owner's edit, a package managed elsewhere and the
`403` that says so (`OwnershipTests`), the rehoming, and the sign-in request
and status.

`LiveServerTests` runs the whole path against a real substrate when told
where: the authorization request, approved through the approval API (which
installs health), the package declared and upgraded, the owner's edit to
it refused, the package taken over and the app's apply stopped, the sample
routine written on the first run, deleted through the outbox and not written
again by this phone, a second one or a reinstall, added back, a session logged
signed out whose exercises the repository lacks taken by the server's
`mustExist`, a
finished session and its 20 sets written through the outbox, and all of it
read back as the owner.

```bash
WORKOUTS_E2E_SERVER=http://localhost:8080 WORKOUTS_E2E_OWNER_TOKEN=… WORKOUTS_E2E_REPOSITORY=<fresh repository> \
  swift test --package-path apps/apple/WorkoutsKit --filter LiveServer
```

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

`docs/screenshots/rows-*.jpg` are the Rows layout from the app's build in
rxtx.dev (Spotter, geoah/rxtx.dev#118), with example sessions loaded as history: drops, a past set being
fixed, a timed plank and dark mode.
