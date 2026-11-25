# The seam between the core and the views

`TodayCore` makes one value, `TodayPresentation`, and takes one enum, `TodayCommand`. A view draws the first
and sends the second, and decides nothing: every word it shows is already in the presentation, in sentence
case and written to "you". The types are in `TodayKit/Sources/TodayCore/Presentation/` and compile on their
own against the kit.

## What a view keeps to itself

- **Navigation.** A path of `TodayRoute` (`priorities`, `projects`, `project(id)`, `person(id)`,
  `meeting(id)`, `task(id)`, `plan`, `suggested`). `TodayPresentation.page(_:)` answers a route with its
  `PagePresentation` each time the view draws; it is cheap and pure. One level: a page opened from a page
  takes its place. The view says which page is on the screen (`opened(route)`), and nothing else of its
  path.
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
- **Whether what waits is shown.** Within ten minutes of a meeting the rows of the stretch are folded into
  one line, "Morning medication, the vendor check and two more wait until you are out." (`ParkedLine`). The
  line holds the rows it folds (`rows`) and opens in place to them: a view draws them under it as rows of the
  day, each with its tick and its own way to open, so a routine is logged and a task ticked in the minutes
  before the meeting. It starts closed. (The owner, 24 November, with the line circled: "I missed saving my
  skincare and some medication and now I can't see and change them." The rows were folded and the line did
  not open.)

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
| `ParkedLine`                                                 | What waits while a meeting leads: the things in bold (`what`), the rest of the sentence, and the `rows` it folds, which it opens to |
| `StripPresentation`, `StripEntry`                            | The strip: title, date, the flagged projects, "All projects" and the edit icon button                   |
| `NightPresentation`, `NightItem`, `NightDecision`            | The night: one line folded; open, by project, with "Got it"                                             |
| `EarlierPresentation`                                        | "Earlier today": a header with a count, and its `entries` in one order by when they happened: a `row` for what was finished (`Row.State.done`: crossed out, its filled tick takes it back), a `meeting` that ended (`MeetingRow.ended`: dimmed), a `line` for bookkeeping |
| `HappeningPresentation`, `HappeningGroup`, `HappeningItem`   | What is happening: the closed line; open, every project with its lines, flagged first, nothing folded. A line says where it came from (`source`: "Slack, #api-reliability.") and carries its `links` |
| `TimelineLine`                                               | One line of a timeline, the night's or a project's: a clock time (`time`, "08:23", never a day), who, what happened, and the `links` to what it was written from |
| `MarkedBlock`                                                | Something marked urgent and held back: who marked it and its options                                    |
| `Row`, `RowBody`                                             | A row of the day, closed (a line) or open (its body); `Row.Trailing` is what ends the closed line. `.tick(button)` ends a task's and a routine's: open it finishes the thing, filled (role `primary`, on a row whose `state` is `.done`) it takes that back. `.page(route)` is a right chevron, and a tap on the line opens the page. A closed row says `line`, or `opening` where it has no line: the opening words of what the open row shows in full, which an open row leaves out |
| `RoutineCard`                                                | A routine row, open: its items each with a tick, All taken or All done, Skip beside it, a line if stale |
| `YesterdayCard`                                              | What was not logged yesterday: each routine with the lines it left, a tick on each, and its option list |
| `SuggestedItem`                                              | One suggested task wherever it is drawn: `save` set when drawn alone, `other` while "Other" is the pick, `told` once the owner answered it in words |
| `PageRowCard`                                                | What a row that opens a page holds where it leads: one line and one button ("Open the plan", "Open the suggested tasks") |
| `OverlapCard`, `MeetingTie`                                  | An overlap's options; and what ties the lines of two meetings that share time ([below](#two-meetings-that-share-time)) |
| `MeetingRow`, `MeetingCard`                                  | A meeting as a line, and in full                                                                        |
| `TaskCard`, `Door`, `TaskLine`                               | A task open (chips, `details`, doors for Flag, Project and When, Done, the assistant's box); a task as a list line. `details` is all of the description, in the day as on the task's page; `brief`, set only on the current item with a long one, is its opening words and the two words of the button that shows all of it in place and puts it back |
| `UrgentCard`                                                 | What cannot wait, in its three states (open, chosen, unchecked)                                         |
| `UnfinishedCard`                                             | The assistant asks where something flagged High goes                                                    |
| `WrapCard`                                                   | The end of the day save card. It lists no suggested task: `unanswered` is one line that says how many wait, and `answer` opens their page |
| `MossBox`, `AnswerCard`                                      | The assistant's box about one thing, and one exchange with it as it arrives. Its exchanges are the thing's conversation, the ones kept from earlier days included ([Asking the assistant](#asking-the-assistant)) |
| `ComposerPresentation`, `ChatDoor`                           | "Add or ask", and the day's conversation as the chat's own screen opens it ([below](#the-days-conversation)) |
| `PagePresentation`                                           | One of: `PrioritiesPage`, `ProjectsPage`, `ProjectPage`, `PersonPage`, `MeetingPage`, `TaskPage`, `PlanPage`, `SuggestedPage`, `MissingPage` |
| `ProjectPage.TimelineGroup`, `TimelineGroup.Section`         | A project's timeline: one group a day, whose `label` says the day once, and in it one section for each place its lines came from, with that place as its `source` ([below](#a-projects-timeline)) |
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

`TaskCard.links`, `UrgentCard.links`, `SuggestedItem.links`, `MeetingCard.links`, `HappeningItem.links` and
`TimelineLine.links` are `SourceLink`s: buttons that leave the app. A view draws each as a small button with its `title` and the arrow of a link
that opens elsewhere, names it aloud with its `label`, and opens its `url` on a tap. On a task they stand
beside "Done"; on a suggestion under the line that says when it was heard.

- `source`: the record's page where it came from, titled by the place ("Slack", "Gmail", "Linear",
  "GitHub", "Google Docs", else the host). Present once the root has read that page
  ([records](records.md#where-a-task-came-from-its-links)). Where the place is said in words right above
  the buttons (a timeline section's "Slack, #api-reliability", a line's "Gmail."), the title is "Open" and
  the place is not said a second time; the `label` is "Open in Slack" either way.
- `record`: the record itself in the server's console, which a browser opens. Present whenever the thing
  names what it was made from and the server is known.

A thing made from nothing (a task the owner typed) has no links. A meeting has its event's own page in its
calendar. `MeetingCard.Reading.url` opens what is to be read first the same way: its page at the source
where that is read, else the record.

A line written from a digest's item has the same two: `HappeningItem.links` in what is happening, and
`TimelineLine.links` on the night's lines and on a project's timeline. They are the links of the first
record the item names in `sources`, its thread's opener. The record link is there at once and the source
link once that record's page is read, so a line may gain its first button a moment after it is drawn. A view
draws them as one row under the line's words, in the words' column and not under the time, lower and
quieter than a card's (`SourceLinksView(quiet:)`): there is a pair under every line. A line written from no
record (a meeting ahead, a task the owner finished, an item that names no source) has none.

**Where a line came from, in words** (`SourcePages.label`, `Day.source(of:)`): the place, then what the
source is inside it. The place is known from the record's kind before anything is read (Slack, Gmail,
Google Calendar, Linear, GitHub, Notion), else it is the read page's own name (a file in a drive is "Google
Docs" by its address). What it is inside the place is `SourcePage.group`, known once the page is read: a
Slack message's channel as "#name", "direct message" or "group message". So a line says "Slack" first and
"Slack, #api-reliability" once read. It is joined with a comma, and nothing in the app joins facts with a
middot. `HappeningItem.source` is that label as a sentence ("Slack, #api-reliability."), and what something
marked urgent says there stays as it was.

## A project's timeline

`ProjectPage.timeline` is drawn as headings and lines, in this order:

- **One group a day, and its day said once.** "Coming up" (the meetings still ahead that hold a task of the
  project), "Today" (since waking), "Overnight" (the night before it), "Yesterday", and a date for anything
  older, "Tuesday 17 November". No line says a day: `TimelineLine.time` is a clock time alone, and it is
  read under its group's label.
- **Overnight is one night.** It starts at the last look of the day before where that was in its evening
  (`DayEngine.timelineNightFrom`). A last look after midnight leaves it starting at midnight, and one longer
  ago than 16:00 of the day before leaves it starting there, so what an owner who was away finds under
  "Overnight" is one night's lines, and each day before it has its own group.
- **Inside a day, one section for each place its lines came from.** `Section.source` is the label above
  ("Slack, #api-reliability", "Gmail"), said once over the section's lines. All of one source's lines in a
  day are one section, the newest line first. A day with one source still says it.
- **What was written from no record has no label and stands first in its day**, directly under the day's
  name: a task the owner finished, an item that names no source. After a labelled section it would read as
  more of that one. The labelled sections follow in order of each one's newest line.

A view draws the group's label as a heading, each section's `source` as a quiet line, and each line with its
time in the time column and its links under its words (`ProjectTimelineView`). It is one list of headings
and lines whose ids hold: a line is named by when it happened, so one that arrives moves the others, and a
line whose source was just read moves under its new label.

When a page comes onto the screen the view sends `opened(route)` once. For a project's page the root then
reads what its timeline was written from, a few records at a time, the newest lines first
([records](records.md#where-a-task-came-from-its-links)); until a line's record is read it has the record
link alone and its section says the place with no channel.

## Asking the assistant

What the owner says to the assistant is a conversation it remembers, and not one message at a time (the
owner, 24 November: "if I talk to it again, it doesn't know what I'm talking about"). A conversation is a
thread on the server, and the app keeps which thread each one is in.

- **A conversation's scope** (`MossSubject.scope(on:)`). Each thing has one conversation for as long as it
  lives: `task:<id>`, `project:<id>`, `person:<path>`, `meeting:<id>`. So a question about a task a week
  later follows what was said before. The day has one a day, in the owner's time zone: `day:2026-11-24`,
  where "Add or ask" and the plan's sends go. "Other" words on a suggested task go to that task's. An
  exchange keeps the scope it was asked in, so one sent before midnight stays in that day's.
- **Continuing it.** `State.threads` holds the thread of each scope. A question is sent into it
  (`Effect.ask(_:message:context:thread:after:)`), with the thing's record as the turn's context every time,
  so the assistant reads the thing as it is now. The server names the thread in its first line, and that
  is when it is kept: the thread a question opened is the conversation's from then on.
- **A thread that was not heard of.** With no thread kept and something asked in the scope before, the
  question names that exchange (`after`), and `Assistant.thread(of:)` reads the message's own row, which the
  server writes under the id the app chose and which names its thread. So a second question sent before
  the first one's thread arrived, or after a send whose answer was lost, opens no second conversation.
- **A thread that is gone.** The server refuses a thread it does not hold before it writes the message
  (404). The same message is then sent once more with no thread, which opens a new one.
- **One answer at a time in a conversation.** A question asked while another in the same scope is being
  answered closes that one where it stands (the server takes the new message into the same turn, and the
  answer covers both). Conversations do not wait on each other: each is read under its own effect key.
- **Kept on the device** (`MossKept`, `today-moss.json` in the repository's folder): the threads and the
  exchanges, saved when a question is asked, answered or closed. On opening, today's conversation about the
  day is read back and no earlier day's, an exchange about a thing for thirty days, a thing's thread for
  four months after it was last asked in, and an exchange that was being answered when the app closed is
  closed with a sentence that says so. Signing out, or clearing what the device kept, forgets them all.

What this does not do: another device does not find a conversation started on this one (a thread has no
property that says what it is about, so that needs a record of the app's own); a conversation about a thing
is not told what was said about the day, or the reverse (both read the same records and the assistant's
memories); and a thing's thread is not given a name of its own in Chat (the day's is, below).

### The day's conversation

The owner, 25 November: the answer over the field "is too basic. It doesn't correctly stream, it doesn't
correctly show the item", and "when I click on chat, I should see the rest of the discussion that I'm
discussing with the agent for today. So it should be a continuous thread for the whole day". So the day's
conversation is drawn by the chat's own screen, the one Substrate Chat draws a conversation with, from the
kit's `SubstrateChatUI` (`ChatSessionView`; the kit's README, "A conversation in another app").

- **The door** is `ComposerPresentation.chat`, a `ChatDoor`: the day's scope (`day:2026-11-25`), the
  assistant, the thread the scope is kept under where it has one, and the day's name ("Wednesday 25
  November"), which a thread the screen opens is named, so Chat's list says which day it was.
- **The view** (`ComposerBar`, `DayScreen`). What is sent from the field opens the screen over the day with
  those words sent, under everything said earlier in the day; the button before the field opens it with
  nothing sent. What waits to be finished is written first (`Finishing.flush`), so the assistant reads the
  day as it stands.
- **The thread is kept by the core.** The screen says the thread it opened or found with
  `chatThread(scope:thread:)`; the core keeps it in `State.threads` and on the device, as it keeps a thread
  a box's question opened. So the plan's sends, which are about the day too, go into the same thread, and
  the next opening reads the conversation from its start.
- **While the assistant cannot be reached** (`ChatDoor.sends` false: the device is offline, or the last
  question from the core did not get through) what is typed is asked from the core with `addOrAsk` and
  answered in place, where its words can be kept as a task. Nothing typed is left unsent in a screen, and
  the door still opens what was said. The next answer that arrives sends the field's words to the screen
  again.
- **Where there is no such screen** (a drawing, a fixture scene, a test) `PaneActions.chat` is nil: the
  button before the field is Add, and what is sent goes to the assistant from the core with `addOrAsk`, as
  before, and is answered in place.

Not done: the boxes about one thing (a task, a project, a person, a meeting) still answer in place, since a
message sent from the chat's screen carries no record as its context yet; and the app's own session is the
chat's, so the chat reads the list of chats and watches it while only the day's is drawn.

## The widgets' day

A home-screen widget draws a second, much smaller value: `GlanceDay`, the day at one instant, in
`TodayKit/Sources/TodayGlance/` (Foundation alone, so the widget extension links it without the kit).
`GlancePresenter` makes it from the pane the engine makes at that instant, and works nothing out a second
time: what leads is `PanePresentation.lead`, the order is the order of `items`, and a meeting's real instants
are the day's own. Every word a widget shows is in the value, "and 3 more" included.

| Type                             | Holds                                                                                               |
| -------------------------------- | --------------------------------------------------------------------------------------------------- |
| `GlanceTimeline`                 | `writtenAt`, the `entries` in order of `at`, and `until`, the instant past which the day is not known. `days(from:)` is what a widget draws from an instant on |
| `GlanceDay`                      | `at` (from when it holds), the date, `now`, `quiet`, `next`, `left`, `upcoming`, `tasks`, `meetings`, and `message`, which every widget says in place of the day |
| `GlanceItem`                     | One thing on one row: glyph, title, `line` (said at the row's end where there is room), a meeting's start (`time`) and end (`until`), its flag (and whether it is only suggested), its project, a live count's end, and `link` |
| `GlanceMeeting`                  | A meeting with its `start` and `end` as instants: its `time`, its end as words (`until`), its title, `note`, `live`, `off`, its place in a `pair` to choose between, and whether it `counts` |
| `GlanceTasks`, `GlanceMeetings`  | A list: `standing` ("2 of 6 tasks done", "5 meetings left"), `items`, and the sentence for none (`empty`) |
| `GlanceKind`, `GlanceWords`, `GlanceLinks` | The three widgets' kinds, names and descriptions; the fixed words; the link scheme and the day's link |

A widget has no name over its rows and no row taller than one line (the owner, 24 November: "too large
text, too little info, too much wasted space", then "avoid headers and just have single rows with icons and
priorities for each item in order"). A row is a flag's stripe, the kind's glyph, a meeting's start and end,
the title, and what ends it: a count, a note ("now", "Choose one"), or quietly the first of `line` and the
project that the title leaves room for. A widget draws as many rows as fit, six in a medium one and sixteen
in a large one at the default text size, and one quiet line under them where a line is left over.

What each widget draws:

- **Now and next** (`today.now`): `now`, heavier, then `upcoming` in its order. Where `next` would fall below
  the last row, the rows before it give way: one quiet row says how many ("and 4 more") and `next` is the last
  row, so the next fixed point is always in the widget. Under the rows, `left`. With nothing leading it says
  `quiet`.
- **Tasks** (`today.tasks`): each of `tasks.items`, and under them `tasks.standing`; `tasks.empty` when there
  is none.
- **Meetings** (`today.meetings`): each of `meetings.items` with its start and its end in one column, and
  what ends its row: the count where `counts`, else `note`. The one in progress is heavier, `off` is struck
  through, and the two of a `pair` stand on one band. Under them, `meetings.standing`.

What `GlancePresenter` keeps to:

- **`now`** is the item whose id is `PanePresentation.lead`: a row, a meeting, the night. **`next`** is the
  first meeting in the pane that is not on, not `off` and does not lead, so a meeting that leads is never said
  twice. **`upcoming`** is every row and every meeting the owner goes to, in the pane's order, without what
  leads; `next` is among them, in its own place. The saved plan's row is not a thing ahead. The rows a `parked` line folds while a meeting leads are
  still listed, in the engine's order.
- **`tasks.items`** are the pane's task rows, and something urgent, in its order; `standing` counts the tasks
  done today from "Earlier today". A suggested task and a routine are not tasks.
- **A routine** is its group and a count, as a reminder says it (`DayEngine.outsideTitle`): a dose in no group
  is "Medication" and "1 item.", and what was not logged yesterday says no more than that. No name and no dose
  of what is in a routine is in the value ([records](records.md)).
- **A count** is real: `count` and `counts` are set exactly where `MeetingRow.until` is, and the view draws the
  minutes left to the meeting's `start`, "in 12 min": a started minute counted, as `Words.until` counts it,
  and never minutes and seconds, which read as a time of day beside the real ones. On the home screen the
  system counts the minutes; `GlanceWords.count(from:to:)` is the same words at one instant, for a picture.
- **A link** is the route of the page a row opens (`TodayLinks`): `substrate-today://task/<id>`,
  `substrate-today://meeting/<id>`, `substrate-today://plan`, `substrate-today://suggested`, and
  `substrate-today://day` for everything else. `TodayLinks.route(_:)` reads one back, and answers nil, the
  day, for one it does not know.

The timeline is the day at `now`, then at each of the day's turns (`DayEngine.turns`: a run of meetings comes
near, a meeting starts or ends, a routine comes within its hour, a task moved to a time reaches it, an
unchecked urgent mark starts to interrupt, the evening and the closing of the day begin, midnight), up to
`GlancePresenter.limit` instants or the end of tomorrow. A turn that changes nothing a widget draws adds no
entry. The instant a count starts to be said is no turn: it is the day before it with the count on. A test
walks the invented day and holds the timeline to the pane at every instant between the turns, so a rule the
engine gains without a turn fails there.

## Commands

| Command                                                                                     | When                                                                                   |
| ------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `log(row:done:)`, `logOccurrence(_:done:)`, `unlog(_:)`, `unlogRow(_:)`, `later(row:)`      | A routine row: Taken or Done, Skip, one item, an item taken back, the whole row taken back, the notification's Later |
| `answerYesterday(item:option:)`                                                             | An option of a routine not logged yesterday; it answers what is still open, at once    |
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
| `chatThread(scope:thread:)`                                                                 | The chat's screen says the thread a conversation is in: kept under its scope           |
| `send(_:)`                                                                                  | Send, under a message that is ready                                                    |
| `instruction(_:on:)`, `rewriteInstruction(_:text:)`, `settings(_:)`                         | Settings: an instruction on or off, its text, a row of the app's own                   |
| `opened(_:)`                                                                                | A page came onto the screen, by its route: sent once each time one appears. It writes nothing; a project's page has what its timeline was written from read for its links |
| `looked`, `refresh`, `foreground`, `clearSaved`, `offered(_:)`, `dismissProblem`, `notificationsAsked` | The app's own                                                                |

Every button and option row in a presentation already carries the command it sends, so a view rarely names
one: it sends `button.command` or `row.command`.

## What the shell wires

- **The root**: `TodayApp(cache:outbox:words:build:folder:glance:)`. Pass `folder: parts.folder`: the kinds
  found at the last sign-in are kept there, and without it the pane opens empty until the server answers.
- **The widgets' day**: `glance` is where the root hands a `GlanceTimeline`, off the main actor: a moment
  after each change that may draw the pane otherwise, once the records are read, and one that says to open
  the app as soon as the owner signs out or clears what the device kept. The shell writes it into the
  Keychain item the widget extension reads (`GlanceKeychain`, in the Keychain group the app and the extension
  share), unless the item already says the same from now on, and has WidgetKit read it again. Nil, as on a
  Mac, writes none.
- **A link**: the scene's `onOpenURL` hands the URL to `Today.open(_:)`, and the day's screen opens
  `TodayLinks.route(_:)` on its path: that page in place of whatever was open, or the day for nil.
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

The invented day's digests name what each item was written from (`TodayFixtures.written`: messages in
channels with a name, a direct message and a group one, mails with their thread and account, issues, pull
requests, invitations, a document, and a chat that has no page), over two days before the day itself, and
their pages are read already (`writtenPages`, made by `SourcePages` from those records). A state with
`sources` emptied is the day before any page is read.

`TodayFixtures.glances` is the widgets' day at the moments their gallery draws: waking, the morning, the
afternoon in a meeting with two that share time still to choose between, a repository with nothing to draw a
day from, and signed out.
