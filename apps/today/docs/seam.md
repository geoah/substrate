# The seam between the core and the views

`TodayCore` makes one value, `TodayPresentation`, and takes one enum, `TodayCommand`. A view draws the first
and sends the second, and decides nothing: every word it shows is already in the presentation, in sentence
case and written to "you". The types are in `TodayKit/Sources/TodayCore/Presentation/` and compile on their
own against the kit.

## What a view keeps to itself

- **Navigation.** A path of `TodayRoute` (`priorities`, `projects`, `project(id)`, `person(id)`,
  `meeting(id)`, `task(id)`, `plan`, `suggested`). `TodayPresentation.page(_:)` answers a route with its
  `PagePresentation` each time the view draws; it is cheap and pure. One level: a page opened from a page
  takes its place.
- **What is open.** Which row, door, flag list, group or folded block is open is view state. The row whose
  `state` is `.now` (and the meeting whose `leads` is true, and the night when `leads`) is drawn open with
  no close button.
- **What is typed.** The text of "Add or ask" and of an assistant box's field, until it is sent; and what
  is typed under "Other" on a suggested task (`SuggestedItem.other`), until its Save. A view sends that Save
  as `saveSuggested(ids, words:)` with the words of those ids, whatever `words` the button's own command carries.
- **Ticks under "To raise"** on a meeting.

When a flag list is closed the view sends `settleFlags`, so the order follows the flags only then.

## Presentation types, and what each draws

| Type                                                         | Draws                                                                                                   |
| ------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------- |
| `TodayPresentation`                                          | The root: `pane`, `page(_:)`, the kit's notices, problem, settings, storage and about                   |
| `PanePresentation`                                           | The day: `strip`, `items` in the order drawn, `ground`, `composer`, `answer`                            |
| `PaneItem`                                                   | One thing in the timeline: night, earlier, happening, row, meeting, overlap, parked, unfinished, note   |
| `StripPresentation`, `StripEntry`                            | The strip: title, date, the flagged projects, "All projects" and the edit icon button                   |
| `NightPresentation`, `NightItem`, `NightDecision`            | The night: one line folded; open, by project, with "Got it"                                             |
| `EarlierPresentation`                                        | "Earlier today": one line with a count, opening to its lines                                            |
| `HappeningPresentation`, `HappeningGroup`, `HappeningItem`   | What is happening: the closed line; open, a group per project in flag order, the rest as one count      |
| `MarkedBlock`                                                | Something marked urgent and held back: who marked it and its options                                    |
| `Row`, `RowBody`                                             | A row of the day, closed (a line) or open (its body); `Row.Trailing` is what ends the closed line. `.page(route)` is a right chevron, and a tap on the line opens the page |
| `RoutineCard`                                                | A routine row, open: its items, Taken or Done, Skip, a stale note                                       |
| `YesterdayCard`                                              | What was not logged yesterday: each routine with its option list, acting on a tap                       |
| `SuggestedItem`                                              | One suggested task wherever it is drawn: `save` set when drawn alone, `other` while "Other" is the pick, `told` once the owner answered it in words |
| `PageRowCard`                                                | What a row that opens a page holds where it leads: one line and one button ("Open the plan", "Open the suggested tasks") |
| `OverlapCard`, `OverlapBlock`                                | An overlap's options; in the timeline, the block around its meetings                                    |
| `MeetingRow`, `MeetingCard`                                  | A meeting as a line, and in full                                                                        |
| `TaskCard`, `Door`, `TaskLine`                               | A task open (chips, doors for Flag, Project and When, Done, the assistant's box); a task as a list line |
| `UrgentCard`                                                 | What cannot wait, in its three states (open, chosen, unchecked)                                         |
| `UnfinishedCard`                                             | The assistant asks where something flagged High goes                                                    |
| `WrapCard`                                                   | The end of the day save card. It lists no suggested task: `unanswered` is one line that says how many wait, and `answer` opens their page |
| `MossBox`, `AnswerCard`                                      | The assistant's box about one thing, and one exchange with it as it arrives                             |
| `ComposerPresentation`                                       | "Add or ask"                                                                                            |
| `PagePresentation`                                           | One of: `PrioritiesPage`, `ProjectsPage`, `ProjectPage`, `PersonPage`, `MeetingPage`, `TaskPage`, `PlanPage`, `SuggestedPage`, `MissingPage` |
| `SuggestedPage`, `SuggestedPage.Group`                       | The suggested tasks as a page: one save card for each project, six of a group drawn until `more` is pressed |
| `PlanPage.Left`                                              | A task yesterday left, the first section of the plan page: its own list, nothing selected until chosen  |
| `OptionList`, `OptionRow`                                    | The one control for a choice. `mark` round or square; a row's tap sends `command`                       |
| `ButtonPresentation`, `SaveRow`, `ReadyMessage`              | A button (`command`, `opens` or `url`); the end of a save card; a message ready with its own Send        |
| `FlagControl`                                                | A flag where it can change: the stripe, the word, the three options, and `open` when drawn open         |
| `Flag`, `Stripe`, `Glyph`, `Tile`, `Badge`, `Icon`, `Pill`   | The marks: flag stripe (hollow when suggested), kind glyph, project tile, person badge, pill            |
| `Chips`, `ProjectChip`, `PersonChip`, `StaleNote`, `Ground`  | Chips under a title, the stale pill with its sentence, the six grounds                                  |
| `WrapLabels`, `MeetingLabels`, `PageLabels`, `ControlWords`  | The fixed words: the label over each part of a card or page, and each icon button's name                |
| `MossSettings`                                               | Settings, "How Moss behaves": each standing instruction with a switch and its text to edit              |

`Pill.Kind` gives a pill's tone and glyph: `suggested` (sparkles), `urgent` (flame), `stale` (clock),
`caution`, `neutral`. An `OptionRow` with `suggested` carries the Suggested pill, and `reason` is the
assistant's sentence under it.

## Commands

| Command                                                                                     | When                                                                                   |
| ------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `log(row:done:)`, `logOccurrence(_:done:)`, `unlog(_:)`, `later(row:)`                      | A routine row: Taken or Done, Skip, one item, taken back, the notification's Later     |
| `answerYesterday(item:option:)`                                                             | An option of a routine not logged yesterday; takes effect at once                      |
| `readNight`, `gotIt(project:)`, `answerNight(project:option:)`                              | The night and what is happening                                                        |
| `suggest(_:_:)`, `saveSuggested(_:words:)`, `showSuggested(group:)`                         | A draft pick on a suggested task; Save for the ids a group or one item draws, with what was typed under "Other"; the rest of a group |
| `planPick`, `planFlag`, `planOverlap`, `planSend`, `planLeft`, `planReset`, `planSave`      | The plan page: a draft until `planSave`                                                |
| `decideOverlap(_:option:)`                                                                  | An overlap outside the plan; takes effect at once                                      |
| `setFlag(task:_:)`, `settleFlags`, `setWhen(task:option:)`, `setProject(task:project:)`     | A task's doors; each takes effect at once                                              |
| `done(_:)`, `reopen(_:)`, `moveTo(task:option:)`                                            | A task's action; where an unfinished thing goes                                        |
| `urgent(task:_:)`                                                                           | Take it now, leave it here, not urgent                                                 |
| `setPriority(project:_:)`, `togglePriority(project:)`, `removePriority(project:)`           | The priorities page, and a project's own page                                          |
| `carry(task:option:)`, `saveWrap`                                                           | The wrap: a draft until `saveWrap`                                                     |
| `addOrAsk(_:)`, `addUnanswered(_:)`, `ask(_:_:)`, `closeAnswer(_:)`, `stopAnswer`           | The field and the assistant's boxes                                                    |
| `send(_:)`                                                                                  | Send, under a message that is ready                                                    |
| `instruction(_:on:)`, `rewriteInstruction(_:text:)`, `settings(_:)`                         | Settings: an instruction on or off, its text, a row of the app's own                   |
| `looked`, `refresh`, `foreground`, `clearSaved`, `offered(_:)`, `dismissProblem`, `notificationsAsked` | The app's own                                                                |

Every button and option row in a presentation already carries the command it sends, so a view rarely names
one: it sends `button.command` or `row.command`.

## What the shell wires

- **The root**: `TodayApp(cache:outbox:words:build:folder:)`. Pass `folder: parts.folder`: the kinds found at
  the last sign-in are kept there, and without it the pane opens empty until the server answers.
- **Coming forward**: the shell's `refresh` is `.foreground`, which reads again and counts as a look. A view
  that shows the pane without the app coming forward sends `.looked`.
- **Notifications**: `respond: TodayReminders.command(for:)`. The root registers the category (Taken, Skip,
  Later), plans the reminders (`TodayPresentation.reminders`) and reconciles them itself; it asks for
  permission the first time a routine is logged, or on `.notificationsAsked`.
- **The clock**: the root asks to be drawn again at `TodayPresentation.nextChange` (the next minute, or the
  moment an unchecked urgent mark starts to interrupt), so a view keeps no timer.
- **A debug session**: `TodayFixtures.state(at:)` and `TodayFixtures.present(_:at:)` make any moment of the
  invented day without a server.

## Fixtures

`TodayFixtures.all` is one named scene per moment the design draws, each made by running the root's presenter
over the invented day at a fixed time: waking (and waking where logging has stopped), the routine, before the
call, the catch-up with the night leading, the suggested tasks (a dismissal the assistant objects to, "Other"
picked, one answered in words, and a backlog of over a hundred),
the plan's row and a plan draft, working at 11:30, a flag list open, the interrupt, before a meeting, in a
meeting, the gap with something marked urgent and held, the same with the assistant unreachable, the evening,
the wrap, an answer in place, and an empty repository. A gallery draws `fixture.app.pane`, and
`fixture.app.page(route)` for each of `fixture.routes`.
