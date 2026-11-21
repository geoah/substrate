# Substrate Today

One pane for the day, on iPhone, iPad and Mac: the order of the day under a
strip of the projects you flagged, with the assistant on every item. It is
built on the kit (`apps/kit`) and drawn from records: every screen is a
presentation TodayKit makes, and the SwiftUI views draw it and send commands,
nothing else ([docs/plans/app-kit.md](../../docs/plans/app-kit.md)).

The app asks for `full`, alone, as Substrate Chat does: the pane reads across
packages and talks to the assistant, which reads everything. A `full` request
carries no package details, so the app publishes nothing and sets nothing up.
What it reads comes from packages the owner adds from the catalogue
(`routines`, `health`, `today`) and from `tasks`, which Substrate Tasks
publishes. A part whose package the repository lacks stays empty; the app
never fails because one is missing.

| Where | What |
| --- | --- |
| `app.json` | the app, named once: its ids, its scope (`full`), and that it publishes and asks for no package |
| [`docs/records.md`](docs/records.md) | every kind and property the pane reads and writes, who writes each, and the rules that turn records into the pane |
| [`docs/seam.md`](docs/seam.md) | the presentation types and the commands: what a view draws and sends |
| `TodayKit/` | the core, a Swift package with no UI framework that builds and tests on Linux: `TodayCore` |
| `apple/Sources/` | the app target: its scenes and the updater |
| `apple/UI/` | `SubstrateTodayUI`: the kit's `AppShell` over the app's root, the design guide's components, the pane and the pages ([its README](apple/README.md)) |
| `apple/Tests/` | the gallery: every component, scene and page drawn on the Mac |
| [`dev/`](dev/README.md) | a dev server with the packages and an invented day, to try the app against |

## The pane

From top to bottom: the strip (the projects with a flag, in flag order), the
night, what is done earlier today, what is happening since the last look
(directly above the current item), the current item in full, what comes next
in the order it is acted on, and "Add or ask" fixed at the bottom. The
priorities, the list of projects, a project, a person, a meeting, a task and
the plan are pages; everything else opens in place.

The rules are the engine's, and a test pins each:

- **Tasks and suggested tasks, nothing else.** A suggested task is a task in
  `proposed`; the suggested tasks are answered together and saved with one
  button, with the assistant's answer selected and marked Suggested. The
  card draws twelve at a time, and one the assistant has not answered has
  nothing selected.
- **The assistant suggests and never acts.** Above a Save its option starts
  selected and nothing takes effect until Save; where a tap takes effect at
  once its option only carries the pill. A flag or a project it suggests on
  something that exists is a patch request, accepted by choosing it.
- **A choice hides nothing.** An answered thing stays where it was with its
  choice filled and when it was chosen, and another tap moves the choice.
- **A flag is High, Medium or Low.** Urgent is above them and is never
  written by the app: it is taken off by "Not urgent" or by choosing a flag.
  An urgent mark the assistant checked interrupts; one it holds back waits in
  what is happening; one nobody checked waits two minutes, then interrupts.
- **Routines are one row a group a day.** The occurrences of one local day
  whose series share a group are one row and one tap, in slot order; one
  reminder a day at the first slot, whose words name no medicine.
- **No durations.** Only real times: when something starts, how long until it.

## TodayKit

`TodayCore`, by folder:

| Folder | What |
| --- | --- |
| `Presentation/` | the values a view draws and the one `TodayCommand` enum; every word shown is here, the labels of each part included |
| `Model/` | the records as values (`Day` and its parts), what the repository holds (`Discovery`), the drafts above each Save, the day's notes the device keeps, and the wording helpers |
| `Engine/` | `DayEngine`: pure functions from a `Day` to the pane and the pages |
| `App/` | the root under the kit's link (`TodayApp`, a `LinkedRecordsApp`): commands to events, effects, the reads beside the linked kinds (two window reads, open requests, sync status), the writes, the assistant's client, Settings |
| `TodayFixtures.swift` | an invented day as records, and one named scene per moment the design draws, each made by running the engine at a fixed instant |

The small kinds (lists, tasks, the series kinds, logs, medications, the app's
own, the assistant's instructions) are read whole and followed through the
watch. Routine occurrences and meetings come from window reads, kept on the
device so the pane draws offline. Every write goes through the outbox in the
shape `docs/records.md` gives it.

Settings holds the app's own part above the kit's: **How Moss behaves** (the
assistant's standing instructions, each with a switch and its text to edit,
written as guarded patches) and **Preparing the day** (whether and when the
assistant prepares, and how long an urgent mark nobody checked waits), from
the one `today/preference`.

## Running it

```bash
mise run apple:build:today   # the Mac app and the simulator app, registering nothing with Apple
mise run apple:test:today    # TodayKit's tests, then the gallery drawn on the Mac
```

`apps/today/dev/server.sh up` starts a dev server with the packages and an
invented day, and a debug build opens it without the approval page
([Driving a debug build](apple/README.md#driving-a-debug-build)). A debug
build also draws any fixture scene with no server at all.

## Not built yet

Widgets and the Lock Screen, App Intents, Live Activities, and messages the
assistant drafts for the owner to send. A reminder's actions (Taken, Skip,
Later) work from a notification.
