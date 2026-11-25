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
| `TodayKit/` | the core, a Swift package with no UI framework that builds and tests on Linux: `TodayCore`, and `TodayGlance`, the day as a widget draws it |
| `apple/Sources/` | the app target: its scenes and the updater |
| `apple/UI/` | `SubstrateTodayUI`: the kit's `AppShell` over the app's root, the design guide's components, the pane and the pages ([its README](apple/README.md)) |
| `apple/Widgets/` | the widget extension, on iPhone and iPad: three home-screen widgets drawn from `TodayGlance` alone ([Widgets](#widgets)) |
| `apple/Tests/` | the gallery: every component, scene, page and widget drawn on the Mac |
| [`dev/`](dev/README.md) | a dev server with the packages and an invented day, to try the app against |

## The pane

From top to bottom: the strip (the projects with a flag, in flag order), the
night, what is done earlier today, what is happening since the last look
(directly above the current item), the current item in full, what comes next
in the order it is acted on, and "Add or ask" fixed at the bottom, whose
words open the day's conversation with the assistant in the chat's own
screen (one thread a day, read from its start each time). What is
typed there goes to the assistant, which adds a task where the words are one
and answers where they ask; while it cannot be reached, the words are
answered in place and can be kept as a task. What is said to the assistant is a conversation it remembers:
the day has one a day, and each task, project, person and meeting has its own,
for as long as it lives ([Asking the assistant](docs/seam.md#asking-the-assistant)). The
priorities, the list of projects, a project, a person, a meeting, a task,
the plan and the suggested tasks are pages; everything else opens in place.
A row of the day that opens a page ends with a right chevron.

A task's and a routine's closed row ends with a tick, and a task's flag is
its colour stripe alone: the flag is changed from the opened row. Finishing
a thing is taken back, not confirmed: the tick, Done, Taken, All taken, All
done and Skip write nothing on the tap. For three seconds the control is a
countdown and the way back, for two more the thing is drawn crossed out
where it stands, and then it is written; leaving the app writes it at once.
What is finished stays in the day under "Earlier today" as a closed,
crossed-out row whose filled tick takes it back, with a meeting that ended
as a dimmed row. That header shows or hides them, and the device keeps the
choice.

The rules are the engine's, and a test pins each:

- **Tasks and suggested tasks, nothing else.** A suggested task is a task in
  `proposed`. They are answered on a page of their own, grouped by project:
  the flagged projects first, and within one what the assistant would make
  a task first. Each project is a save card with its own button, the
  assistant's answer selected and marked Suggested, and one it has not
  answered has nothing selected. A group draws six until asked for the rest,
  and its Save answers only what it draws.
- **Other.** Every suggestion can be answered in the owner's own words:
  what is wrong, or what to do instead. They are kept as a record for
  whoever suggests tasks, and sent to the assistant with the task. That
  suggestion is then with the assistant and is not asked again.
- **Heard means said.** A suggestion says "Heard" only with the time its
  words were said; one whose record does not say is "Suggested", with the
  time it was written. It fades by the first.
- **What yesterday left is part of the plan.** A task that was due and is
  still open is the first section of the plan page, by name, with its own
  list and nothing chosen for the owner. Saved, the plan keeps a row in the
  day that opens it again.
- **A routine yesterday left says what it left.** A routine of several
  things lists the ones not logged, each with a tick of its own, over two
  options that answer what is still open ("Took the rest yesterday",
  "Skipped the rest"). Its answer is read from the logs, so it is the same
  whichever way it was given, on any device. Answered, it is a row of
  "Earlier today" that opens to the same lines and options. One logged on
  its own day, taken or skipped, is never asked after.
- **Skip skips what is not ticked.** In a routine with some things ticked,
  Skip logs the others as skipped and leaves the ticks. The finished row
  says how many of each ("2 of 5 taken, 3 skipped, at 07:31"): it is not
  said to be taken while part of it was skipped.
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
- **A line says where it came from and opens it.** A line of what is happening, of the night or of a
  project's timeline is written from a record (a message, a mail, an issue, an invitation). Under its words
  are the links out: its page at the source ("Slack", "Gmail") once that is read, and the record itself. It
  says its source in words, the channel included: "Slack, #api-reliability".
- **A project's timeline says each day once.** "Coming up", "Today", "Overnight", "Yesterday", then a date,
  "Tuesday 17 November", each over the lines of that day, and every line has its clock time alone. Inside a
  day the lines are grouped by where they came from, each source said once over its lines
  ([the seam](docs/seam.md#a-projects-timeline)).

## Widgets

Three home-screen widgets, on iPhone and iPad, each in the two sizes that take a
row of the home screen or several (`systemMedium`, `systemLarge`). None is small.

A widget is rows and nothing else. It has no name over them, and each thing is
one row: its flag as a stripe, its glyph, a meeting's start and end, its title, and
quietly at the end what the title leaves room for. The type is small, so a medium
widget holds six rows and a large one sixteen at the default text size; a larger
text size draws fewer. Where a line is left under the rows, it says how the day
stands.

| Widget | Kind | Draws |
| --- | --- | --- |
| Now and next | `today.now` | the day in its order: what leads it (the pane's current item), heavier, then what follows, tasks, routines and meetings alike. The next meeting the owner goes to is always among the rows: where it would fall below the last one, the rows before it give way to "and 4 more". Under them, "3 tasks and 2 meetings left" |
| Tasks | `today.tasks` | today's open tasks in the pane's order, each with its flag as a stripe, and at the end of its row why it matters now or its project. Under them, "2 of 6 tasks done" |
| Meetings | `today.meetings` | the meetings of the rest of today, each with its start and its end: the one in progress is heavier and says "now", two that share time and wait for a choice stand on one band, and one the owner does not go to is struck through. Under them, "5 meetings left" |

The count to the next meeting is in minutes, "in 12 min", as the app says it, and
drawn within the hour before it as the app's is. The system counts the minutes
itself, so it moves on the home screen with no new day for each one. A widget cannot
be scrolled or paged: several of them in one place is the system's Smart Stack.

- **The extension reads no record and reaches no server.** The app makes the day
  ahead (`GlancePresenter`, from the pane the engine makes at each instant) and
  writes it into one Keychain item, which the extension reads. The extension links
  `TodayGlance` and nothing else of the app, and draws what it reads.
- **The two share a Keychain access group.** The app and the extension each list
  the app's own identifier as their one Keychain group (`keychain-access-groups`:
  the team's prefix, then `dev.reamde.substrate.today`), as Substrate Chat does for
  its notification extension. A Keychain group is no capability to register with
  Apple, so signing needs nothing set up for it, as Chat's extension ships. The
  item is a generic password, service `dev.reamde.substrate.today.glance`, account
  `day`, readable after the first unlock and never synchronised to iCloud; its
  value is the day's JSON compressed, about six kilobytes for a day of eighteen
  meetings.
- **It stays right while the app is not running.** The item holds the day at each
  instant it changes: now, then each turn of the day (a meeting starts or ends or
  comes near, a routine comes within its hour, the evening begins), up to 24
  instants or the end of tomorrow. Past the last of them a widget says to open the
  app, and never shows a day it does not know.
- **A routine is its group and a count**, as in a reminder: the names and doses of
  what is in it are shown inside the app only
  ([records](docs/records.md)). Titles are marked private, so a locked device hides them.
- **A tap opens the app with a link of its own** (the `substrate-today` scheme):
  `substrate-today://day` anywhere on a widget, and in the large size a row opens
  its page, `substrate-today://task/<id>` or `substrate-today://meeting/<id>` (and
  `plan` and `suggested` for the rows that open those pages). A link the build does
  not know opens the day.
- **Signing out, or clearing what the device kept, takes the day back at once**:
  the item then holds one day that says to open the app.

The widgets' views are in `apple/Widgets/Views/`, compiled into the extension and
into the tests, which draw every widget on the Mac
([the gallery](apple/README.md#the-gallery)). The Keychain item is one definition,
`apple/Widgets/Shared/`, compiled into the extension and into the UI framework.

## TodayKit

`TodayCore`, by folder:

| Folder | What |
| --- | --- |
| `Presentation/` | the values a view draws and the one `TodayCommand` enum; every word shown is here, the labels of each part included |
| `Model/` | the records as values (`Day` and its parts), what the repository holds (`Discovery`), the drafts above each Save, the day's notes the device keeps, and the wording helpers |
| `Engine/` | `DayEngine`: pure functions from a `Day` to the pane and the pages |
| `App/` | the root under the kit's link (`TodayApp`, a `LinkedRecordsApp`): commands to events, effects, the reads beside the linked kinds (two window reads, open requests, sync status), the writes, the assistant's client, Settings |
| `TodayFixtures.swift` | an invented day as records, and one named scene per moment the design draws, each made by running the engine at a fixed instant |
| `GlancePresenter.swift` | the widgets' day at an instant, made from that instant's pane, and the timeline of such days ahead |

`TodayGlance` is the day as a widget draws it (`GlanceDay`, and the `GlanceTimeline`
of them the app writes), over Foundation alone with no dependency, so the widget
extension links it without the kit.

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

Lock Screen widgets, a widget on the Mac, App Intents (a widget's lines open
the app and tick nothing), Live Activities, and messages the assistant drafts
for the owner to send. A reminder's actions (Taken, Skip, Later) work from a
notification.
