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
- **The choice between two tied meetings**, open or closed: the key `overlap:<MeetingTie.id>`.
- **What was finished a moment ago.** A command that finishes something (`TodayCommand.finishing` is not
  nil: `done`, `log(row:done:)`, `logOccurrence(_:done:)`) is not sent on the tap. The view keeps it in a
  `Finishing`: for `Finishing.countdown` (3 seconds) the control that was pressed is a countdown and the way
  back, for `Finishing.settle` (2 more) the thing is drawn finished where it stands and a tap on its tick
  still takes it back, then the command is sent. Leaving the foreground, or the screen going away, sends
  everything that waits at once. `Finishing` is a value with no clock: the view says what time it is. A
  command that takes a finish back (`reopen`, `unlog`, `unlogRow`) is sent at once.
  After a command is sent the view goes on drawing the thing finished until the presentation no longer
  holds it as something that can be finished, and while it leaves its place, so it is never seen open again
  between its command and its record.
- **Whether what is done is shown.** The header of "Earlier today" shows or hides its entries. The device
  keeps the choice across launches, and it starts shown.

- **How it moves.** A view draws every change of the presentation as a move and not a cut: an item of the
  day keeps one place whatever it is drawn as (`PaneItem.id`), so the row that becomes the current item is
  the same row growing, and what is finished leaves while the rest closes up. The core says nothing about
  motion but one fact: `TodayPresentation.writing` is true while an answer from the assistant is arriving a
  few words at a time, and the view then draws each change at once.

A task's flag is changed in its opened row (the card's Flag door); a closed row has no flag control. When
that list is closed, or the card is, the view sends `settleFlags`, so the order follows the flags only then.

## Presentation types, and what each draws

| Type                                                         | Draws                                                                                                   |
| ------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------- |
| `TodayPresentation`                                          | The root: `pane`, `page(_:)`, the kit's notices, problem, settings, storage and about, and `writing`    |
| `PanePresentation`                                           | The day: `strip`, `items` in the order drawn, `ground`, `composer`, `answer`                            |
| `PaneItem`                                                   | One thing in the timeline: night, earlier, happening, row, meeting, parked, unfinished, note            |
| `StripPresentation`, `StripEntry`                            | The strip: title, date, the flagged projects, "All projects" and the edit icon button                   |
| `NightPresentation`, `NightItem`, `NightDecision`            | The night: one line folded; open, by project, with "Got it"                                             |
| `EarlierPresentation`                                        | "Earlier today": a header with a count, and its `entries` in one order by when they happened: a `row` for what was finished (`Row.State.done`: crossed out, its filled tick takes it back), a `meeting` that ended (`MeetingRow.ended`: dimmed), a `line` for bookkeeping |
| `HappeningPresentation`, `HappeningGroup`, `HappeningItem`   | What is happening: the closed line; open, every project with its lines, flagged first, nothing folded   |
| `MarkedBlock`                                                | Something marked urgent and held back: who marked it and its options                                    |
| `Row`, `RowBody`                                             | A row of the day, closed (a line) or open (its body); `Row.Trailing` is what ends the closed line. `.tick(button)` ends a task's and a routine's: open it finishes the thing, filled (role `primary`, on a row whose `state` is `.done`) it takes that back. `.page(route)` is a right chevron, and a tap on the line opens the page. A closed row says `line`, or `opening` where it has no line: the opening words of what the open row shows in full, which an open row leaves out |
| `RoutineCard`                                                | A routine row, open: its items each with a tick, All taken or All done, Skip beside it, a line if stale |
| `YesterdayCard`                                              | What was not logged yesterday: each routine with its option list, acting on a tap                       |
| `SuggestedItem`                                              | One suggested task wherever it is drawn: `save` set when drawn alone, `other` while "Other" is the pick, `told` once the owner answered it in words |
| `PageRowCard`                                                | What a row that opens a page holds where it leads: one line and one button ("Open the plan", "Open the suggested tasks") |
| `OverlapCard`, `MeetingTie`                                  | An overlap's options; and what ties the lines of two meetings that share time ([below](#two-meetings-that-share-time)) |
| `MeetingRow`, `MeetingCard`                                  | A meeting as a line, and in full                                                                        |
| `TaskCard`, `Door`, `TaskLine`                               | A task open (chips, `details`, doors for Flag, Project and When, Done, the assistant's box); a task as a list line. `details` is all of the description, in the day as on the task's page; `brief`, set only on the current item with a long one, is its opening words and the two words of the button that shows all of it in place and puts it back |
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

## Two meetings that share time

They are two `PaneItem.meeting` lines like any other, one under the other, each with a `MeetingRow.tie` that
names the overlap and the other meeting. Nothing is drawn around them and nothing between them.

- **No choice yet** (`tie.toChoose`): one band marks both, the second says `tie.ask` ("These two overlap.
  Choose one."), neither says a line of its own, and the pair has one chevron, at the top right of the band.
  A tap on either opens the options (`MeetingRow.card.overlap`) under the second line, in place.
- **A choice** sends `decideOverlap` at once. The chosen option is seen filled, then the view closes the
  options by itself. The meeting the owner does not go to is `off`: struck through, with nothing under it.
- **Afterwards** the line the owner goes to opens its own card, which holds the overlap and its options. A
  tap on the struck line opens the options under the two again.
- Where one of the two leads the day, or is open as its card, that card holds the options and the other is
  a plain line (`tie.otherLeads`, or the view's own open state).

`MeetingRow.until` is set on the next fixed point only within the hour before it: hours ahead, its time says
enough.

## What a thing was made from: its links

`TaskCard.links`, `UrgentCard.links`, `SuggestedItem.links` and `MeetingCard.links` are `SourceLink`s:
buttons that leave the app. A view draws each as a small button with its `title` and the arrow of a link
that opens elsewhere, names it aloud with its `label`, and opens its `url` on a tap. On a task they stand
beside "Done"; on a suggestion under the line that says when it was heard.

- `source`: the record's page where it came from, titled by the place ("Slack", "Gmail", "Linear",
  "GitHub", "Google Docs", else the host). Present once the root has read that page
  ([records](records.md#where-a-task-came-from-its-links)).
- `record`: the record itself in the server's console, which a browser opens. Present whenever the thing
  names what it was made from and the server is known.

A thing made from nothing (a task the owner typed) has no links. A meeting has its event's own page in its
calendar. `MeetingCard.Reading.url` opens what is to be read first the same way: its page at the source
where that is read, else the record.

## Commands

| Command                                                                                     | When                                                                                   |
| ------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `log(row:done:)`, `logOccurrence(_:done:)`, `unlog(_:)`, `unlogRow(_:)`, `later(row:)`      | A routine row: Taken or Done, Skip, one item, an item taken back, the whole row taken back, the notification's Later |
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
