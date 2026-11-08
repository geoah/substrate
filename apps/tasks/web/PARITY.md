# Tasks on the web against the native app

Every feature, screen, setting, command, word and behaviour of the Apple and
Android apps (TasksKit's `TasksApp`, `TasksPresenter`, `TasksSettings`,
`TasksMenu`, `Reminders`, `TaskEditor`, `TaskOrder`, `TasksEnrichment`, the
SwiftUI views and the Compose screens), each marked by where the web app
stands:

- **present**: the web does it, in the presenter's words;
- **partial**: the web does some of it, or in other words;
- **missing**: the web does not do it.

The first status is the MVP's (`feat/web-tasks` as first deployed). Where the
web cannot match the native app, the row says why.

## The App menu

| What | Native | Web (MVP) |
| --- | --- | --- |
| Sections | Needs you, Lists, Settings (`TasksMenu`) | partial: Today, Lists, Settings |
| Needs you's badge | the count of open tasks that need you, hidden at 0 | partial: Today's due count |
| Phone | a tab per section, and the round + at the bar's trailing end that opens New Task, seeded with the list shown (and today on Needs you) | partial: no + in the bar; New Task is in each list's bar |
| Tablet, desktop | the sections and the lists in one sidebar, Settings last; the list beside it, the open task in a third column ("No task open", "Choose a task to see it here.") | partial: three columns only from 1180 px; "No Task Selected" |
| Nothing chosen in Lists (wide) | "Choose a list" | missing |

## The lists column (`SidebarPresentation`)

| What | Native | Web (MVP) |
| --- | --- | --- |
| Title | Lists | present |
| Fixed rows | Needs you (wide only) and No List, each with its open count | partial: Today and No List |
| Heading | My Lists | present |
| A list with no name | "Untitled list" | missing |
| No lists | "No lists yet. Add one to sort your tasks." | partial: "No lists yet." |
| New List | the bar's folder-plus button; the question "New List", "A task list's name, such as Groceries or Home.", Name, Cancel, Add | partial: a row; a sheet titled New List with Done |
| A list's own menu (right-click, long press) | Rename…, Delete… | missing |
| Drag to reorder lists | `reorderList`: `Position.moves` over the arranged lists, writing only what moved | missing |
| Count read aloud | "1 open task", "3 open tasks" | missing |

## A list (`ListPresentation`)

| What | Native | Web (MVP) |
| --- | --- | --- |
| Title | the list's name, "Untitled list", No List, Needs you | partial |
| Summary | "3 to do" or "All done", "1 overdue"; on Needs you "2 need you" or "Nothing needs you" and the day ("Sunday, 8 November") | missing |
| Sections | To Do (with its count where Done shows too), Done with its count | partial: an unnamed section and "Completed" |
| Order | Settings' Sort Tasks By: My Order (position, then id) or Due Date (soonest first, undated last, then my order) | partial: always position, then name |
| Order note | "Sorted by due date" | missing |
| Done tasks | newest completion first, under Done, when Show Completed is on | partial |
| List options (⋯) | Sort Tasks By (My Order, Due Date), Show Completed / Hide Completed, Rename List…, Delete List… | partial: "Show/Hide Completed Tasks", Rename List, Delete List |
| Empty, in a list | "No open tasks", "Tap + to add a task." | partial: "No tasks." |
| Empty, in No List | "No tasks", "Tasks in no list show here. Tap + to add one." | partial: "No tasks." |
| Quick add | "Add a task to Groceries", "Add a task" in No List; a task after the list's last | partial: "Add a task to No List" |
| New Task | the round + (phone), the "New Task" capsule (wide), and the empty state's | partial: an icon in the bar |
| Drag to reorder tasks | in My Order only, never on Needs you: `Position.moves` over the open rows | missing |
| Keyboard reordering | the system's (VoiceOver, the Mac's list) | missing |

## Needs you (`TasksPresenter.needsYou`, `TaskBand`)

| What | Native | Web (MVP) |
| --- | --- | --- |
| The home screen | every open task across the lists in its band | missing (Today: open tasks due today or before) |
| Bands | Needs you (late, or high and urgent with no day), Coming up (within seven days, with its count), Later (under a dashed fold, faded) | missing |
| Ranking | late by how late, then undated by priority; coming by when; later by when, undated by priority; ties in my order | missing |
| A late row's due words | "4 days late", "1 day late", "Late since 15:00", "Late" | missing |
| Decisions on a row | Move (Today where it is late, Tomorrow, the day a week on, Pick a Day…), or Pick a Day for an undated task; Won't Do | missing |
| Reschedule | a named day keeps a timed task's time of day; Pick a Day opens the editor | missing |
| Chips | All, No List (while it has open tasks or is chosen), each list; narrow Needs you to one | missing |
| Clear line | "Nothing needs you right now." while the first band is empty and others are not | missing |
| Empty | "All clear", "Nothing is open. Tap + to add a task." or, narrowed, "Nothing is open here. Choose All to see every list." | missing |
| Quick add, New Task | due today, in the list it is narrowed to: "Add a task for today", "Add a task for today to Home" | partial |
| No list options | Needs you has no ⋯ | partial |

## A task's row (`TaskRow`)

| What | Native | Web (MVP) |
| --- | --- | --- |
| Name | "Untitled task" for none | partial: "Untitled" |
| Notes | their first words on one line, at most 160 characters | partial: a notes icon |
| Due | "Today", "Tomorrow, 15:00", "Tue 10 Nov", "Overdue: Yesterday", in the tone's colour; filled where overdue | partial: no "Overdue:" |
| Priority | flag and its word, orange for High, red for Urgent | partial: "!" marks |
| List | on Needs you only | partial: on Today |
| Done and Won't Do rows | struck through, faded, no chips | partial |
| The round mark | on the right; done fills it at once with a ring that drains for the 3 s undo window, a second tap takes it back, and only then is the write sent | partial: on the left, written at once, the row kept until the list is left |
| Read aloud | "Buy milk, due Today, priority High, in Home, has notes" | missing |
| Row menu (context menu, long press) | Mark as Done / Won't Do, or Reopen, or Undo; Edit; Priority ▸ (checked); Move to List ▸; Delete Task | missing |
| Swipes (phone) | leading Mark as Done / Reopen / Undo; trailing Delete and Won't Do, never a full swipe | missing |
| The open task's row | washed with the accent | present |

## The open task (`TaskDetailPresentation`)

| What | Native | Web (MVP) |
| --- | --- | --- |
| Title | the name ("Untitled task") beside the round mark, struck when abandoned or pending | partial: no mark |
| Status | "Open", "Done", "Won't do", "Done, tap Undo to keep it open" | partial: "Won't Do" |
| List, Priority | choices that change it in place | present |
| Due | in full ("Friday, 6 November 2026 at 15:00"), red when overdue, "No due date" without | partial: row hidden without |
| Notes | a section of its own | partial: a row, plain text |
| Link | a section with the link | partial |
| Completed | the moment it was done, in full | missing |
| Actions | Mark as Done, Won't Do; Reopen; Undo while pending; Delete Task apart, with no question | partial: Delete asks |
| Edit | the bar's | present |
| Deleted elsewhere | closes | present |

## The editor (`TaskEditor`)

| What | Native | Web (MVP) |
| --- | --- | --- |
| Title | New Task, Edit Task | present |
| Fields | Name ("What to do"), Notes ("Details"), Link ("https://example.com"); List; Due: No Due Date, On a Day, At a Time, with Day or Day and Time; Priority | partial: Title, a Due Date toggle and a Time toggle |
| At a Time | starts at 9:00 on the day chosen | present |
| Save | Add, Save, or Retry after a write that was not kept; Cancel | partial: no Retry |
| A name | "A task needs a name." once the owner has typed | missing |
| Stays open | until the write is kept, with the draft; a write not kept is said in the editor ("A change was not kept") | partial: a toast |
| Saves | a create in the list chosen after its last task; an edit patches only the fields changed, on the version seen | present |
| Pick a Day | the editor on the task | missing |

## Questions

| What | Native | Web (MVP) |
| --- | --- | --- |
| Delete a list | "Delete “Groceries”?" ("Delete this list?" for none); "The list has no tasks.", "Its 1 task is deleted with it.", "Its 3 tasks are deleted with it."; Delete List, Delete List and 1 Task, Delete List and 3 Tasks; Cancel | partial: other words, Delete |
| After it | the list closes: back to the lists on a phone, No List wider; Needs you's chip clears | partial |

## Settings (`TasksSettings`)

| What | Native | Web (MVP) |
| --- | --- | --- |
| Reminders | "Remind Me When Tasks Are Due", off until turned on, which is when the system is asked | missing |
| The hour | "Tasks Due on a Day", "When the reminder comes": 7:00, 8:00, 9:00, 12:00, 18:00, default 9:00 | missing |
| Refused | "Notifications are off for Substrate Tasks", "Turn them on in the system's Settings to be reminded." | missing |
| Count | "Reminders Set on This iPhone" and how many | missing |
| Footers | "A task due at a time reminds you then; a task due on a day, at the hour above." / "Turned on, you are asked once whether Substrate Tasks may send notifications." | missing |
| Lists | "Sort Tasks By" (My Order, Due Date), "Show Completed Tasks" | partial: Show Completed only, under "Tasks" |
| The link's sections | Your Substrate (status, package), Repository, Storage, About with Version and Channel | partial: the kit's Repository, Access, Sign Out; About |

## Reminders (`Reminders.plan`)

| What | Native | Web (MVP) |
| --- | --- | --- |
| What reminds | every open task due ahead: at `dueAt`, or at the chosen hour of its day; the soonest 60 | missing |
| Words | the name ("Untitled task"); "Due at 15:00", "Due today", ", in Home" | missing |
| A tap | opens the task in its list (Lists, the list, the task) | missing |
| Kept in step | planned again with every change to the tasks and the settings | missing |

## Commands and keys

| What | Native | Web (MVP) |
| --- | --- | --- |
| File > New Task | ⌘N on the Mac | missing |
| Settings | ⌘, on the Mac | missing |
| Check for Updates | the Mac's updater | n/a: a page is current when loaded |
| Arrow keys through a list, Escape | the Mac's list and sheets | partial: Escape closes a sheet |
| Pull to refresh | `refresh` | n/a: the change feed keeps the page current |

## Behaviour

| What | Native | Web (MVP) |
| --- | --- | --- |
| Writes | through the outbox, kept offline | the kit's (its outbox is the kit's work) |
| Moving a task | `list` and `position` in one patch, after the new list's last | present |
| Reopen | clears `completedAt` in the same write | present |
| Words at midnight | presented again when "Tomorrow" becomes "Today", and as a timed task falls due | partial: every 30 s |
| A task in a deleted list | hidden until the server's pass | present |
