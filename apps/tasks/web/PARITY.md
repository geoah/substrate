# Tasks on the web against the native app

Every feature, screen, setting, command, word and behaviour of the Apple and
Android apps (TasksKit's `TasksApp`, `TasksPresenter`, `TasksSettings`,
`TasksMenu`, `Reminders`, `TaskEditor`, `TaskOrder`, `TasksEnrichment`, the
SwiftUI views and the Compose screens), each with where the web app stood
when it was first deployed (the MVP) and where it stands now:

- **present**: the web does it, in the presenter's words;
- **partial**: the web does some of it, or in other words, and the row says
  what is left;
- **missing**: the web does not do it;
- **n/a**: a browser has no such thing, and the row says why.

The web's words are written once, in `src/present.ts`, `src/model.ts` and
`src/state.ts`, from the same state the presenter reads.

## The App menu

| What                           | Native                                                                                                                                                          | MVP                                 | Now                                                          |
| ------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------- | ------------------------------------------------------------ |
| Sections                       | Needs you, Lists, Settings (`TasksMenu`)                                                                                                                        | partial: Today, Lists, Settings     | present                                                      |
| Needs you's badge              | the count of open tasks that need you, hidden at 0                                                                                                              | partial: Today's due count          | present                                                      |
| Phone                          | a tab per section, and the round + at the bar's trailing end that opens New Task, seeded with the list shown (and today on Needs you)                           | partial                             | present                                                      |
| Tablet, desktop                | the sections and the lists in one sidebar, Settings last; the list beside it, the open task in a third column ("No task open", "Choose a task to see it here.") | partial: three columns from 1180 px | present: three columns from 1000 px, as an iPad in landscape |
| Nothing chosen in Lists (wide) | "Choose a list"                                                                                                                                                 | missing                             | present                                                      |

## The lists column (`SidebarPresentation`)

| What                                    | Native                                                                                                        | MVP     | Now                                        |
| --------------------------------------- | ------------------------------------------------------------------------------------------------------------- | ------- | ------------------------------------------ |
| Title                                   | Lists                                                                                                         | present | present                                    |
| Fixed rows                              | Needs you (wide only) and No List, each with its open count                                                   | partial | present                                    |
| Heading                                 | My Lists                                                                                                      | present | present                                    |
| A list with no name                     | "Untitled list"                                                                                               | missing | present                                    |
| No lists                                | "No lists yet. Add one to sort your tasks."                                                                   | partial | present                                    |
| New List                                | the bar's folder-plus button; "New List", "A task list's name, such as Groceries or Home.", Name, Cancel, Add | partial | present                                    |
| Rename                                  | "Rename List", Cancel, Rename                                                                                 | partial | present                                    |
| A list's menu (right click, long press) | Rename…, Delete…                                                                                              | missing | present                                    |
| Drag to reorder lists                   | `reorderList`: `Position.moves` over the arranged lists                                                       | missing | present: a mouse, a finger's hold, Alt ↑ ↓ |
| Count read aloud                        | "1 open task", "3 open tasks"                                                                                 | missing | present                                    |

## A list (`ListPresentation`)

| What                  | Native                                                                                             | MVP                          | Now                                                                                               |
| --------------------- | -------------------------------------------------------------------------------------------------- | ---------------------------- | ------------------------------------------------------------------------------------------------- |
| Title                 | the list's name, "Untitled list", No List, Needs you                                               | partial                      | present                                                                                           |
| Summary               | "3 to do" or "All done", "1 overdue"; on Needs you "2 need you" or "Nothing needs you" and the day | missing                      | present: the presenter writes it and neither native UI draws it; the web draws it under the title |
| Sections              | To Do (named, with the order note, where Done shows too), Done with its count                      | partial                      | present                                                                                           |
| Order                 | Sort Tasks By: My Order (position, then id) or Due Date (soonest, undated last, then my order)     | partial: position, then name | present                                                                                           |
| Order note            | "Sorted by due date"                                                                               | missing                      | present                                                                                           |
| Done tasks            | newest completion first, under Done, when shown                                                    | partial                      | present                                                                                           |
| List options (⋯)      | Sort Tasks By ▸ My Order, Due Date; Show Completed / Hide Completed; Rename List…, Delete List…    | partial                      | present                                                                                           |
| Empty, in a list      | "No open tasks", "Tap + to add a task.", New Task                                                  | partial                      | present                                                                                           |
| Empty, in No List     | "No tasks", "Tasks in no list show here. Tap + to add one."                                        | partial                      | present                                                                                           |
| Quick add             | "Add a task to Groceries", "Add a task" in No List; after the list's last                          | partial                      | present (Android's; the Apple app has none)                                                       |
| New Task              | the round + (phone), the "New Task" capsule (wide), the empty state's                              | partial                      | present: a round + where the column is narrow                                                     |
| Drag to reorder tasks | in My Order only, never on Needs you                                                               | missing                      | present                                                                                           |
| Keyboard reordering   | the system's                                                                                       | missing                      | present: Alt ↑ ↓, said aloud                                                                      |

## Needs you (`TasksPresenter.needsYou`, `TaskBand`)

| What                   | Native                                                                                                              | MVP             | Now                              |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------- | --------------- | -------------------------------- |
| The home screen        | every open task across the lists in its band                                                                        | missing (Today) | present; `#/today` links open it |
| Bands                  | Needs you, Coming up (with its count), Later (under a dashed fold, faded)                                           | missing         | present                          |
| Ranking                | late by how late, then undated by priority; coming by when; later by when, undated by priority; ties in my order    | missing         | present                          |
| A late row's due words | "4 days late", "1 day late", "Late since 15:00", "Late"                                                             | missing         | present                          |
| Decisions on a row     | Move ▸ Today (where it is late), Tomorrow, the day a week on, Pick a Day…; Pick a Day for an undated task; Won't Do | missing         | present                          |
| Reschedule             | a named day keeps a timed task's time of day; Pick a Day opens the editor                                           | missing         | present                          |
| Chips                  | All, No List (while it has open tasks or is chosen), each list                                                      | missing         | present                          |
| Clear line             | "Nothing needs you right now."                                                                                      | missing         | present                          |
| Empty                  | "All clear", "Nothing is open. Tap + to add a task." or "Nothing is open here. Choose All to see every list."       | missing         | present                          |
| Quick add, New Task    | due today, in the list narrowed to: "Add a task for today", "Add a task for today to Home"                          | partial         | present                          |
| No list options        | Needs you has no ⋯                                                                                                  | partial         | present                          |

## A task's row (`TaskRow`)

| What                               | Native                                                                                                                                           | MVP     | Now                                               |
| ---------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ | ------- | ------------------------------------------------- |
| Name                               | "Untitled task" for none                                                                                                                         | partial | present                                           |
| Notes                              | their first words on one line, at most 160 characters                                                                                            | partial | present, without markdown's marks                 |
| Due                                | "Today", "Tomorrow, 15:00", "Tue 10 Nov", "Overdue: Yesterday", in the tone's colour, filled where overdue                                       | partial | present                                           |
| Priority                           | flag and its word, orange for High, red for Urgent                                                                                               | partial | present                                           |
| List                               | on Needs you only                                                                                                                                | partial | present                                           |
| Done and Won't Do rows             | struck, faded, no chips                                                                                                                          | partial | present                                           |
| The round mark                     | on the right; done fills it at once with a ring that drains for the 3 s undo window, a second tap takes it back, and only then is the write sent | partial | present; a page closed within the window sends it |
| Read aloud                         | "Buy milk, due Today, priority High, in Home, has notes"                                                                                         | missing | present                                           |
| Row menu (right click, long press) | Mark as Done / Won't Do, Reopen or Undo; Edit; Priority ▸ (checked); Move to List ▸; Delete Task                                                 | missing | present                                           |
| Swipes (phone)                     | leading: what the mark does, all the way across does it; trailing: Delete and Won't Do, never a full swipe                                       | missing | present                                           |
| The open task's row                | washed with the accent                                                                                                                           | present | present                                           |

## The open task (`TaskDetailPresentation`)

| What              | Native                                                                                  | MVP     | Now                                                                        |
| ----------------- | --------------------------------------------------------------------------------------- | ------- | -------------------------------------------------------------------------- |
| Title             | the name beside the round mark, struck when abandoned or pending                        | partial | present                                                                    |
| Status            | "Open", "Done", "Won't do", "Done, tap Undo to keep it open" (Android's row)            | partial | present                                                                    |
| List, Priority    | choices that change it in place                                                         | present | present                                                                    |
| Due               | in full, red when overdue, "No due date" without                                        | partial | present                                                                    |
| Notes             | a section of its own                                                                    | partial | present, drawn as markdown (below)                                         |
| Link              | a section with the link                                                                 | partial | present; an address that is not http, https or mailto is shown, not linked |
| Completed         | the moment it was done, in full                                                         | missing | present                                                                    |
| Actions           | Mark as Done, Won't Do; Reopen; Undo while pending; Delete Task apart, with no question | partial | present                                                                    |
| Edit              | the bar's                                                                               | present | present                                                                    |
| Deleted elsewhere | closes                                                                                  | present | present                                                                    |

## The editor (`TaskEditor`)

| What               | Native                                                                                                                                                | MVP                                 | Now                                                      |
| ------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------- | -------------------------------------------------------- |
| Title              | New Task, Edit Task                                                                                                                                   | present                             | present                                                  |
| Fields             | Name ("What to do"), Notes ("Details"), Link ("https://example.com"); List; Due: No Due Date, On a Day, At a Time, with Day or Day and Time; Priority | partial                             | present                                                  |
| At a Time          | starts at 9:00 on the day chosen                                                                                                                      | present                             | present                                                  |
| Save               | Add, Save, or Retry after a write that was not kept; Cancel                                                                                           | partial                             | present: Cancel is the sheet's ✕                         |
| A name             | "A task needs a name." once the owner has typed                                                                                                       | missing                             | present                                                  |
| Stays open         | until the write is kept, with the draft; a write not kept is said in it ("A change was not kept")                                                     | partial                             | present; the kit's words come once its `onNotKept` lands |
| Saves              | a create after the chosen list's last task; an edit patches only the fields changed, on the version seen when it opened                               | partial: on the version as it stood | present                                                  |
| Closed by a delete | a task deleted while edited closes its editor                                                                                                         | missing                             | present                                                  |

## Questions

| What          | Native                                                                                                                                                                                                                     | MVP     | Now                                                                      |
| ------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------- | ------------------------------------------------------------------------ |
| Delete a list | "Delete “Groceries”?" ("Delete this list?" for none); "The list has no tasks.", "Its 1 task is deleted with it.", "Its 3 tasks are deleted with it."; Delete List, Delete List and 1 Task, Delete List and 3 Tasks; Cancel | partial | present, the buttons stacked where one is long, as an iPhone stacks them |
| After it      | back to the lists on a phone, No List wider; Needs you's chip clears                                                                                                                                                       | partial | present                                                                  |

## Settings (`TasksSettings`)

| What                | Native                                                                                                                                                             | MVP     | Now                                                                                                                                                        |
| ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Reminders           | "Remind Me When Tasks Are Due", off until turned on, which is when the system is asked                                                                             | missing | present: the browser is asked                                                                                                                              |
| The hour            | "Tasks Due on a Day", "When the reminder comes": 7:00, 8:00, 9:00, 12:00, 18:00 in the locale's words, default 9:00                                                | missing | present                                                                                                                                                    |
| Refused             | "Notifications are off for Substrate Tasks", "Turn them on in the system's Settings to be reminded."                                                               | missing | partial: "…in your browser's settings for this site…", and a browser with no notifications says so                                                         |
| Count               | "Reminders Set on This iPhone" and how many                                                                                                                        | missing | present: "Reminders Set on This Browser"                                                                                                                   |
| Footers             | "A task due at a time reminds you then; a task due on a day, at the hour above." / "Turned on, you are asked once whether Substrate Tasks may send notifications." | missing | present, with "Reminders show only while Substrate Tasks is open in this browser."                                                                         |
| Lists               | "Sort Tasks By" (My Order, Due Date), "Show Completed Tasks"                                                                                                       | partial | present                                                                                                                                                    |
| Kept                | in the repository's folder on the device                                                                                                                           | partial | present: in this browser's storage                                                                                                                         |
| The link's sections | Your Substrate (status, package), Repository, Storage, About with Version and Channel                                                                              | partial | partial: the kit's Repository, Access, Sign Out, and About with Version; the status, storage and refused writes are the kit's sections, which it is adding |
| Keys                | the Mac's menus show them                                                                                                                                          | n/a     | present: a Keyboard section on a desktop                                                                                                                   |

## Reminders (`Reminders.plan`)

| What                    | Native                                                                                 | MVP     | Now                                                                                                                                  |
| ----------------------- | -------------------------------------------------------------------------------------- | ------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| What reminds            | every open task due ahead, at `dueAt` or at the chosen hour of its day; the soonest 60 | missing | present                                                                                                                              |
| Words                   | the name ("Untitled task"); "Due at 15:00", "Due today", ", in Home"                   | missing | present                                                                                                                              |
| A tap                   | opens the task in its list                                                             | missing | present, also from a closed tab through the worker (`#/reminder/<task>`)                                                             |
| Kept in step            | planned again with every change to the tasks and the settings                          | missing | present                                                                                                                              |
| While the app is closed | the system shows them                                                                  | missing | n/a: a page runs only while it is open; showing one with the page closed needs Web Push from a server, which no part of this app has |

## Commands and keys

| What                      | Native                    | MVP     | Now                                                                     |
| ------------------------- | ------------------------- | ------- | ----------------------------------------------------------------------- |
| File > New Task           | ⌘N on the Mac             | missing | present: N; the browser keeps ⌘N for a new window                       |
| Settings                  | ⌘, on the Mac             | missing | present                                                                 |
| The list's arrows, Escape | the Mac's list and sheets | partial | present: ↑ ↓ open the next task beside the list, E edits, Escape closes |
| Save                      | Return in the Mac's sheet | partial | present: ⌘↵                                                             |
| Check for Updates         | the updater               | n/a     | n/a: a page is current when loaded                                      |
| Pull to refresh           | `refresh`                 | n/a     | n/a: the change feed keeps the page current                             |

## Behaviour

| What                       | Native                                                                         | MVP                 | Now                                                                                                                                                                        |
| -------------------------- | ------------------------------------------------------------------------------ | ------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Writes kept offline        | the outbox                                                                     | missing             | the kit's, which is adding one                                                                                                                                             |
| A waiting or refused write | Status and the problem dialog                                                  | partial: a toast    | the kit's (`usePending`, `RefusedWritesSection`), once it lands                                                                                                            |
| Moving a task              | `list` and `position` in one patch, after the new list's last                  | present             | present                                                                                                                                                                    |
| Reopen                     | clears `completedAt` in the same write                                         | present             | present                                                                                                                                                                    |
| Words at midnight          | presented again when "Tomorrow" becomes "Today", and as a timed task falls due | partial: every 30 s | present: at each minute                                                                                                                                                    |
| A task in a deleted list   | hidden until the server's pass                                                 | present             | present                                                                                                                                                                    |
| Haptics                    | a light tap as a task is marked                                                | missing             | present where the browser vibrates (Android); a hold buzzes too                                                                                                            |
| Notes                      | plain text                                                                     |                     | the web draws them as markdown (`src/markdown.tsx`): paragraphs, headings, lists and their check marks, quotes, code, emphasis and links, as React elements and never HTML |
